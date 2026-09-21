package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/bmp"
)

type brandAsset struct {
	data        []byte
	contentType string
	modified    time.Time
	etag        string
}

func forbiddenBrandPath(name string) bool {
	name = strings.ToLower(filepath.ToSlash(name))
	return name == "uploads" || strings.HasPrefix(name, "uploads/") || name == "quarantine" || strings.HasPrefix(name, "quarantine/") || name == "app.db" || strings.HasPrefix(name, "app.db-") || name == "config.yaml" || name == "setup-key.txt"
}

func (a *App) readBrandAsset(name, field string) (brandAsset, error) {
	var asset brandAsset
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if !filepath.IsLocal(filepath.FromSlash(name)) || forbiddenBrandPath(filepath.Clean(name)) {
		return asset, fmt.Errorf("%s 路径无效或指向系统文件", field)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := brandMIME[ext]; !ok {
		return asset, fmt.Errorf("%s 仅支持 png、jpg、webp、gif、ico", field)
	}
	root, err := os.OpenRoot(a.dir)
	if err != nil {
		return asset, err
	}
	defer root.Close()
	// Disallow links in any component. Root.Open also prevents escaping the data
	// directory if a component changes between the checks and the actual open.
	part := ""
	for _, component := range strings.Split(filepath.ToSlash(filepath.Clean(name)), "/") {
		part = filepath.Join(part, component)
		info, e := root.Lstat(part)
		if e != nil {
			return asset, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return asset, fmt.Errorf("%s 不能使用符号链接", field)
		}
		if part == filepath.FromSlash(filepath.Clean(name)) && !info.Mode().IsRegular() {
			return asset, fmt.Errorf("%s 必须是普通图片文件", field)
		}
	}
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return asset, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return asset, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBrandFileSize {
		return asset, fmt.Errorf("%s 文件无效或超过 2 MB", field)
	}
	for _, protected := range []string{"app.db", "app.db-wal", "app.db-shm", "app.db-journal", "config.yaml", "setup-key.txt"} {
		target, e := root.Stat(protected)
		if e != nil && !os.IsNotExist(e) {
			return asset, e
		}
		if e == nil && os.SameFile(info, target) {
			return asset, fmt.Errorf("%s 不能链接到系统文件 %s", field, protected)
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBrandFileSize+1))
	if err != nil {
		return asset, err
	}
	if len(raw) == 0 || len(raw) > maxBrandFileSize {
		return asset, fmt.Errorf("%s 文件无效或超过 2 MB", field)
	}
	contentType := ""
	if ext == ".ico" {
		if err = validateICO(raw); err != nil {
			return asset, fmt.Errorf("%s: %w", field, err)
		}
		contentType = "image/x-icon"
	} else {
		_, format, e := a.validateImage(context.Background(), bytes.NewReader(raw))
		if e != nil {
			return asset, fmt.Errorf("%s 不是有效图片: %w", field, e)
		}
		contentType = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	}
	hash := sha256.Sum256(raw)
	return brandAsset{data: raw, contentType: contentType, modified: info.ModTime(), etag: fmt.Sprintf("\"%x\"", hash)}, nil
}

// ICO may contain PNG or Windows DIB frames. Validate every frame, including
// directory bounds and the DIB transparency mask, before serving the container.
func validateICO(raw []byte) error {
	invalid := fmt.Errorf("ICO 数据损坏或格式不受支持")
	if len(raw) < 6 || binary.LittleEndian.Uint16(raw) != 0 || binary.LittleEndian.Uint16(raw[2:]) != 1 {
		return invalid
	}
	count := int(binary.LittleEndian.Uint16(raw[4:]))
	if count < 1 || count > 64 || len(raw) < 6+16*count {
		return invalid
	}
	for i := 0; i < count; i++ {
		entry := raw[6+16*i : 6+16*(i+1)]
		width, height := int(entry[0]), int(entry[1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		size, offset := uint64(binary.LittleEndian.Uint32(entry[8:])), uint64(binary.LittleEndian.Uint32(entry[12:]))
		if size == 0 || offset < uint64(6+16*count) || offset+size > uint64(len(raw)) {
			return invalid
		}
		frame := raw[int(offset):int(offset+size)]
		var decoded image.Image
		var err error
		if bytes.HasPrefix(frame, []byte("\x89PNG\r\n\x1a\n")) {
			cfg, e := png.DecodeConfig(bytes.NewReader(frame))
			if e != nil || cfg.Width != width || cfg.Height != height {
				return invalid
			}
			decoded, err = png.Decode(bytes.NewReader(frame))
		} else {
			if len(frame) < 40 {
				return invalid
			}
			header := int(binary.LittleEndian.Uint32(frame))
			if header != 40 && header != 108 && header != 124 {
				return invalid
			}
			if header > len(frame) {
				return invalid
			}
			if int32(binary.LittleEndian.Uint32(frame[4:])) != int32(width) || int32(binary.LittleEndian.Uint32(frame[8:])) != int32(height*2) {
				return invalid
			}
			bits := int(binary.LittleEndian.Uint16(frame[14:]))
			palette := 0
			switch bits {
			case 1, 2, 4, 8:
				palette = int(binary.LittleEndian.Uint32(frame[32:]))
				if palette == 0 {
					palette = 1 << bits
				}
				if palette > 1<<bits {
					return invalid
				}
			case 24, 32:
			default:
				return invalid
			}
			pixelOffset := header + 4*palette
			pixels := ((width*bits + 31) / 32) * 4 * height
			mask := ((width + 31) / 32) * 4 * height
			if pixelOffset+pixels+mask > len(frame) {
				return invalid
			}
			bitmap := make([]byte, 14+len(frame))
			copy(bitmap, []byte("BM"))
			binary.LittleEndian.PutUint32(bitmap[2:], uint32(len(bitmap)))
			binary.LittleEndian.PutUint32(bitmap[10:], uint32(14+pixelOffset))
			copy(bitmap[14:], frame)
			binary.LittleEndian.PutUint32(bitmap[22:], uint32(height))
			decoded, err = bmp.Decode(bytes.NewReader(bitmap))
		}
		if err != nil || decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
			return invalid
		}
	}
	return nil
}
