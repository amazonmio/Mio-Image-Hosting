package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"io"
)

// A 16-bit RGBA image needs up to 8 bytes per pixel. Bound one decode to
// roughly 256 MB of pixel storage; compressed bytes and decoder overhead are extra.
const maxDecodedPixels int64 = 32_000_000
const maxGIFFrames = 500

var errImageBusy = errors.New("图片校验繁忙，请稍后重试")

func (a *App) validateImage(ctx context.Context, source io.ReadSeeker) (image.Config, string, error) {
	select {
	case a.decodeSlots <- struct{}{}:
		defer func() { <-a.decodeSlots }()
	case <-ctx.Done():
		return image.Config{}, "", ctx.Err()
	default:
		return image.Config{}, "", errImageBusy
	}
	if err := ctx.Err(); err != nil {
		return image.Config{}, "", err
	}
	cfg, format, err := image.DecodeConfig(source)
	if err != nil {
		return cfg, format, errors.New("图片头无效或格式不受支持")
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > maxDecodedPixels {
		return cfg, format, errors.New("图片不得超过 3200 万像素")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return cfg, format, err
	}
	switch format {
	case "gif":
		raw, e := io.ReadAll(io.LimitReader(source, maxFileSize+1))
		if e != nil {
			return cfg, format, e
		}
		if int64(len(raw)) > maxFileSize {
			return cfg, format, errors.New("图片超过 20 MB")
		}
		// DecodeAll retains all frames. Check the total declared frame allocation first.
		if e = checkGIFBudget(raw); e != nil {
			return cfg, format, e
		}
		_, err = gif.DecodeAll(bytes.NewReader(raw))
	case "jpeg", "png", "webp":
		// Decode the actual pixel stream, not only its dimensions. Keep the original
		// file for storage so JPEG quality, metadata and GIF animation are preserved.
		var decoded image.Image
		decoded, _, err = image.Decode(source)
		if err == nil && (decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height) {
			err = errors.New("图片尺寸与头信息不一致")
		}
	default:
		return cfg, format, errors.New("仅支持 JPG、PNG、GIF、WebP")
	}
	if err != nil {
		return cfg, format, fmt.Errorf("图片数据损坏或不完整：%w", err)
	}
	if err = ctx.Err(); err != nil {
		return cfg, format, err
	}
	_, err = source.Seek(0, io.SeekStart)
	return cfg, format, err
}

// Walk GIF container blocks without allocating frame buffers. The standard GIF
// decoder below still verifies palettes, compression and every frame's pixels.
func checkGIFBudget(raw []byte) error {
	invalid := errors.New("GIF 数据损坏或不完整")
	if len(raw) < 13 {
		return invalid
	}
	offset := 13
	if raw[10]&0x80 != 0 {
		offset += 3 * (1 << ((raw[10] & 7) + 1))
	}
	skipBlocks := func() bool {
		for offset < len(raw) {
			size := int(raw[offset])
			offset++
			if size == 0 {
				return true
			}
			if size > len(raw)-offset {
				return false
			}
			offset += size
		}
		return false
	}
	var total int64
	frames := 0
	for offset < len(raw) {
		block := raw[offset]
		offset++
		switch block {
		case 0x3b:
			if frames == 0 {
				return invalid
			}
			return nil
		case 0x21:
			if offset >= len(raw) {
				return invalid
			}
			offset++
			if !skipBlocks() {
				return invalid
			}
		case 0x2c:
			if len(raw)-offset < 9 {
				return invalid
			}
			width := int64(binary.LittleEndian.Uint16(raw[offset+4:]))
			height := int64(binary.LittleEndian.Uint16(raw[offset+6:]))
			packed := raw[offset+8]
			offset += 9
			frames++
			total += width * height
			if width == 0 || height == 0 {
				return invalid
			}
			if frames > maxGIFFrames || total > maxDecodedPixels {
				return errors.New("GIF 最多 500 帧，所有帧累计不得超过 3200 万像素")
			}
			if packed&0x80 != 0 {
				offset += 3 * (1 << ((packed & 7) + 1))
			}
			if offset >= len(raw) {
				return invalid
			}
			offset++ // LZW minimum code size
			if !skipBlocks() {
				return invalid
			}
		default:
			return invalid
		}
	}
	return invalid
}
