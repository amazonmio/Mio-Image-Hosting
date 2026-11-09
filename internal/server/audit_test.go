package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProxyHintsRequireSetupKey(t *testing.T) {
	a := freshAuthApp(t)
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port"} {
		t.Run(header, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://localhost/api/auth/status", nil)
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set(header, "proxy")
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if !decode[authStatus](t, w).SetupKeyRequired {
				t.Fatal("proxied localhost request bypassed setup key requirement")
			}
		})
	}
}

func TestUploadRejectsInvalidDisplayName(t *testing.T) {
	a := testApp(t, "")
	for _, name := range []string{"a\x01b.png", "a\ufffdb.png", "   "} {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		file, err := form.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		file.Write(pngBytes(t))
		form.Close()
		r := httptest.NewRequest(http.MethodPost, "/api/images", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Authorization", "Bearer "+a.token)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("name %q: %d %s", name, w.Code, w.Body.String())
		}
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM images").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid uploads persisted: %d, %v", count, err)
	}
}

func TestStorageDirectoryLinksRejected(t *testing.T) {
	for _, name := range []string{"uploads", "thumbs"} {
		t.Run(name, func(t *testing.T) {
			dir, target := t.TempDir(), t.TempDir()
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Skipf("symlink privilege unavailable: %v", err)
			}
			app, err := New(dir, "", "")
			if err == nil {
				app.Close()
				t.Fatal("accepted linked storage")
			}
		})
	}
}
