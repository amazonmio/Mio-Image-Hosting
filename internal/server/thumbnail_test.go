package server

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func largePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{10, 20, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestThumbnailLifecycle(t *testing.T) {
	a := testApp(t, "")
	payload := largePNG(t, 800, 600)
	p := decode[Picture](t, sendUpload(t, a, payload, "", 201))
	if p.ThumbURL != "https://img.example.com/t/"+p.ID {
		t.Fatal(p.ThumbURL)
	}
	list := decode[struct{ Items []Picture }](t, request(t, a, "GET", "/api/images", "", 200))
	if len(list.Items) != 1 || list.Items[0].ThumbURL != p.ThumbURL {
		t.Fatalf("list thumb_url: %+v", list.Items)
	}
	thumb := request(t, a, "GET", "/t/"+p.ID, "", 200)
	if !strings.HasPrefix(thumb.Header().Get("Content-Type"), "image/jpeg") {
		t.Fatal(thumb.Header().Get("Content-Type"))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil || format != "jpeg" {
		t.Fatalf("thumb decode: %s %v", format, err)
	}
	if cfg.Width != 480 || cfg.Height != 360 {
		t.Fatalf("thumb size %dx%d", cfg.Width, cfg.Height)
	}
	if err = os.Remove(a.thumbPath(p.ID)); err != nil {
		t.Fatal(err)
	}
	again := request(t, a, "GET", "/t/"+p.ID, "", 200)
	if !strings.HasPrefix(again.Header().Get("Content-Type"), "image/jpeg") {
		t.Fatal("lazy regenerate failed")
	}
	if _, err = os.Stat(a.thumbPath(p.ID)); err != nil {
		t.Fatal("lazy thumb not written")
	}
	request(t, a, "GET", "/t/missing.png", "", 404)
	request(t, a, "DELETE", "/api/images/"+p.ID, "", 200)
	request(t, a, "GET", "/t/"+p.ID, "", 404)
	if _, err = os.Stat(a.thumbPath(p.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted image left a thumb")
	}
}

func TestSmallImageStillGetsJpegThumb(t *testing.T) {
	a := testApp(t, "")
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	thumb := request(t, a, "GET", "/t/"+p.ID, "", 200)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
	if err != nil || format != "jpeg" || cfg.Width != 8 || cfg.Height != 6 {
		t.Fatalf("%s %dx%d %v", format, cfg.Width, cfg.Height, err)
	}
}

func TestRecoverThumbsRemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "https://img.example.com")
	if err != nil {
		t.Fatal(err)
	}
	enableFixtureAdmin(t, a)
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	orphan := filepath.Join(dir, "thumbs", "orphan.png.jpg")
	if err = os.WriteFile(orphan, []byte("junk"), 0600); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "thumbs", ".thumb-stale")
	if err = os.WriteFile(tmp, []byte("tmp"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = New(dir, "", "https://img.example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan thumb survived restart")
	}
	if _, err = os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("stale temp thumb survived restart")
	}
	if _, err = os.Stat(a.thumbPath(p.ID)); err != nil {
		t.Fatal("registered thumb was removed")
	}
}
