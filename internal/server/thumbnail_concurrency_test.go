package server

import (
	"context"
	"image"
	"image/color"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A test-only decoder gives deterministic control of a real image.Decode call
// without introducing hooks into the production code or decoding large images.
func controlledThumbnail(t *testing.T, a *App, decodeFrame func() image.Image) string {
	t.Helper()
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	magic := "MIO_THUMB_TEST_" + p.ID
	image.RegisterFormat(magic, magic, func(io.Reader) (image.Image, error) { return decodeFrame(), nil }, func(io.Reader) (image.Config, error) {
		return image.Config{ColorModel: color.RGBAModel, Width: 8, Height: 6}, nil
	})
	if err := os.WriteFile(filepath.Join(a.uploads, p.ID), []byte(magic), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.thumbPath(p.ID)); err != nil {
		t.Fatal(err)
	}
	return p.ID
}
func waitImageReferences(t *testing.T, a *App, id string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.imageLocksMu.Lock()
		lock := a.imageLocks[id]
		got := 0
		if lock != nil {
			got = lock.refs
		}
		a.imageLocksMu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("image operation did not reach expected lock state")
}
func thumbnailResponse(a *App, id string) int {
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/t/"+id, nil))
	return w.Code
}

func TestConcurrentThumbnailRequestsDecodeOnce(t *testing.T) {
	a := testApp(t, "")
	var calls atomic.Int32
	id := controlledThumbnail(t, a, func() image.Image { calls.Add(1); return image.NewRGBA(image.Rect(0, 0, 8, 6)) })
	a.decodeSlots <- struct{}{}
	var wg sync.WaitGroup
	codes := make(chan int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- thumbnailResponse(a, id) }()
	}
	// All requests have reached the per-image lock before decoding is released.
	waitImageReferences(t, a, id, 4)
	<-a.decodeSlots
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatal(code)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("decoded %d times for the same thumbnail", calls.Load())
	}
	a.imageLocksMu.Lock()
	remaining := len(a.imageLocks)
	a.imageLocksMu.Unlock()
	if remaining != 0 {
		t.Fatal("unused image locks leaked")
	}
}
func TestDeletionWaitsForThumbnailAndLeavesNoCache(t *testing.T) {
	a := testApp(t, "")
	entered, release := make(chan struct{}), make(chan struct{})
	id := controlledThumbnail(t, a, func() image.Image { close(entered); <-release; return image.NewRGBA(image.Rect(0, 0, 8, 6)) })
	thumb := make(chan int, 1)
	go func() { thumb <- thumbnailResponse(a, id) }()
	<-entered
	deleted := make(chan int, 1)
	go func() {
		r := httptest.NewRequest("DELETE", "/api/images/"+id, nil)
		r.Header.Set("Authorization", "Bearer "+a.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		deleted <- w.Code
	}()
	waitImageReferences(t, a, id, 2)
	select {
	case code := <-deleted:
		close(release)
		t.Fatalf("delete ran during decode: %d", code)
	default:
	}
	close(release)
	if code := <-thumb; code != 200 {
		t.Fatal(code)
	}
	if code := <-deleted; code != 200 {
		t.Fatal(code)
	}
	if _, err := os.Stat(a.thumbPath(id)); !os.IsNotExist(err) {
		t.Fatal("thumbnail survived successful deletion", err)
	}
	if code := thumbnailResponse(a, id); code != 404 {
		t.Fatal("deleted thumbnail accessible", code)
	}
	if err := a.writeThumbFromOriginal(context.Background(), filepath.Join(a.uploads, id), id); err == nil {
		t.Fatal("deleted image regenerated")
	}
	entries, err := os.ReadDir(a.thumbs)
	if err != nil || len(entries) != 0 {
		t.Fatal("cache/temp files remain", entries, err)
	}
}
func TestCancelledImageLockWaiterDoesNotLeak(t *testing.T) {
	a := testApp(t, "")
	release, err := a.lockImage(context.Background(), "image")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		unlock, e := a.lockImage(ctx, "image")
		if e == nil {
			unlock()
		}
		result <- e
	}()
	waitImageReferences(t, a, "image", 2)
	cancel()
	if err = <-result; err != context.Canceled {
		t.Fatal(err)
	}
	release()
	waitImageReferences(t, a, "image", 0)
}
