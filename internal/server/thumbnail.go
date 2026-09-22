package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

const (
	thumbMaxEdge = 480
	thumbQuality = 82
)

func (a *App) thumbPath(id string) string {
	return filepath.Join(a.thumbs, id+".jpg")
}

func (a *App) thumbURL(id string) string {
	return a.baseURL + "/t/" + id
}

func (a *App) writeThumbFromOriginal(ctx context.Context, src, id string) error {
	release, err := a.lockImage(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	return a.writeThumbLocked(ctx, src, id)
}

// The per-image lock is held through the database check, decode and atomic rename.
// Deletion uses the same lock; cached reads never hold it during network writes.
func (a *App) writeThumbLocked(ctx context.Context, src, id string) error {
	var exists int
	if err := a.db.QueryRowContext(ctx, "SELECT 1 FROM images WHERE id=?", id).Scan(&exists); err != nil {
		return err
	}
	if info, err := os.Stat(a.thumbPath(id)); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("invalid thumbnail cache")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case a.decodeSlots <- struct{}{}:
		defer func() { <-a.decodeSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		f.Close()
		return err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > maxDecodedPixels {
		f.Close()
		return errors.New("thumbnail source exceeds pixel limit")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return err
	}
	img, _, err := image.Decode(f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.encodeThumb(ctx, img, id)
}

func thumbSize(w, h int) (int, int) {
	if w < 1 || h < 1 {
		return 0, 0
	}
	tw, th := w, h
	if w >= h {
		if w > thumbMaxEdge {
			th = h * thumbMaxEdge / w
			tw = thumbMaxEdge
		}
	} else if h > thumbMaxEdge {
		tw = w * thumbMaxEdge / h
		th = thumbMaxEdge
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	return tw, th
}

func (a *App) encodeThumb(ctx context.Context, img image.Image, id string) error {
	src := img.Bounds()
	tw, th := thumbSize(src.Dx(), src.Dy())
	if tw < 1 || th < 1 {
		return errors.New("invalid image size")
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, src, draw.Over, nil)
	if err := os.MkdirAll(a.thumbs, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(a.thumbs, ".thumb-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()
	if err = jpeg.Encode(tmp, dst, &jpeg.Options{Quality: thumbQuality}); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	dest := a.thumbPath(id)
	if err = os.Rename(tmpName, dest); err != nil {
		if _, e := os.Stat(dest); e == nil {
			return nil
		}
		return err
	}
	cleanup = false
	return nil
}

func (a *App) serveThumbnail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	release, err := a.lockImage(r.Context(), id)
	if err != nil {
		return
	}
	// Copy the small JPEG while holding the image lock. This also lets Windows
	// remove a thumbnail during an ongoing response without an open-file conflict.
	var raw []byte
	var info os.FileInfo
	err = func() error {
		defer release()
		if e := a.writeThumbLocked(r.Context(), filepath.Join(a.uploads, id), id); e != nil {
			return e
		}
		f, e := os.Open(a.thumbPath(id))
		if e != nil {
			return e
		}
		defer f.Close()
		info, e = f.Stat()
		if e != nil {
			return e
		}
		if info.Size() <= 0 || info.Size() > 2<<20 {
			return errors.New("invalid thumbnail cache size")
		}
		raw, e = io.ReadAll(io.LimitReader(f, 2<<20))
		return e
	}()
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		log.Printf("thumbnail %s: %v", id, err)
		a.serveImage(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", fmt.Sprintf("%q", id+"-t"))
	http.ServeContent(w, r, id+".jpg", info.ModTime(), bytes.NewReader(raw))
}

func (a *App) removeThumb(id string) {
	if err := os.Remove(a.thumbPath(id)); err != nil && !os.IsNotExist(err) {
		log.Printf("cleanup thumb %s: %v", id, err)
	}
}

func (a *App) recoverThumbs() error {
	known := map[string]struct{}{}
	rows, err := a.db.Query("SELECT id FROM images")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = struct{}{}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(a.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.Mkdir("thumbs", 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := root.Lstat("thumbs")
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("thumbs 必须是数据目录内的真实文件夹")
	}
	dir, err := root.Open("thumbs")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".jpg")
		_, ok := known[id]
		if ok && entry.Type().IsRegular() && name == id+".jpg" {
			continue
		}
		if err = root.RemoveAll(filepath.Join("thumbs", name)); err != nil {
			return err
		}
	}
	return nil
}
