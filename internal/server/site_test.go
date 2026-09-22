package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	frontend "github.com/amazonmio/mio-image-hosting/web"
)

func TestDefaultSiteConfig(t *testing.T) {
	a := testApp(t, "")
	cfg := decode[map[string]any](t, request(t, a, "GET", "/api/config", "", 200))
	if cfg["site_name"] != defaultSiteName || cfg["avatar_url"] != brandAvatarURL || cfg["favicon_url"] != brandFaviconURL {
		t.Fatalf("default branding: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(a.dir, "config.yaml")); err != nil {
		t.Fatal("expected default config.yaml")
	}
	for _, name := range []string{defaultAvatarFile, defaultFaviconFile} {
		if _, err := os.Stat(filepath.Join(a.dir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	avatar := request(t, a, "GET", "/branding/avatar", "", 200)
	if avatar.Header().Get("Content-Type") != "image/webp" || !bytes.Equal(avatar.Body.Bytes(), frontend.DefaultAvatar) {
		t.Fatal("default avatar should be the built-in logo")
	}
	icon := request(t, a, "GET", "/branding/favicon", "", 200)
	if !bytes.Equal(icon.Body.Bytes(), frontend.DefaultAvatar) {
		t.Fatal("default favicon should match the built-in logo")
	}
}

func TestReplacingBrandFilesTakesEffect(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	payload := pngBytes(t)
	if err = os.WriteFile(filepath.Join(dir, defaultAvatarFile), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, defaultFaviconFile), payload, 0600); err != nil {
		t.Fatal(err)
	}
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	avatar := request(t, a, "GET", "/branding/avatar", "", 200)
	if !bytes.Equal(avatar.Body.Bytes(), payload) {
		t.Fatal("replaced avatar was ignored")
	}
	icon := request(t, a, "GET", "/branding/favicon", "", 200)
	if !bytes.Equal(icon.Body.Bytes(), payload) {
		t.Fatal("replaced favicon was ignored")
	}
}

func TestCustomSiteBranding(t *testing.T) {
	dir := t.TempDir()
	payload := pngBytes(t)
	if err := os.WriteFile(filepath.Join(dir, "avatar.webp"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "favicon.png"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("site_name: 猫猫图床\navatar: avatar.webp\nfavicon: favicon.png\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(dir, "secret", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	enableFixtureAdmin(t, a)
	cfg := decode[map[string]any](t, request(t, a, "GET", "/api/config", "", 200))
	if cfg["site_name"] != "猫猫图床" {
		t.Fatal(cfg["site_name"])
	}
	avatar := request(t, a, "GET", "/branding/avatar", "", 200)
	if avatar.Header().Get("Content-Type") != "image/png" || !bytes.Equal(avatar.Body.Bytes(), payload) {
		t.Fatalf("avatar: %s %d", avatar.Header().Get("Content-Type"), avatar.Body.Len())
	}
	icon := request(t, a, "GET", "/branding/favicon", "", 200)
	if icon.Header().Get("Content-Type") != "image/png" || !bytes.Equal(icon.Body.Bytes(), payload) {
		t.Fatal("favicon mismatch")
	}
	ico := request(t, a, "GET", "/favicon.ico", "", 200)
	if !bytes.Equal(ico.Body.Bytes(), payload) {
		t.Fatal("favicon.ico mismatch")
	}
}

func TestSiteConfigRejectsUnsafeValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("avatar: ../secret.png\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, "", ""); err == nil {
		t.Fatal("accepted path traversal")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("avatar: missing.png\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, "", ""); err == nil {
		t.Fatal("accepted missing avatar")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("avatar: thumbs/x.jpg\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, "", ""); err == nil {
		t.Fatal("accepted thumbs path")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("site_name: "+strings.Repeat("名", 61)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(dir, "", ""); err == nil {
		t.Fatal("accepted an overlong site_name")
	}
}

func TestInjectSiteTitleAndFavicon(t *testing.T) {
	page := []byte(`<html><head><title>Mio · 轻量图床</title><link rel="icon" type="image/webp" href="/logo.webp"/></head></html>`)
	out := string(injectSite(page, `猫 <床>`))
	if !strings.Contains(out, "<title>猫 &lt;床&gt;</title>") || strings.Contains(out, "Mio · 轻量图床") {
		t.Fatal(out)
	}
	if !strings.Contains(out, `href="/branding/favicon"`) || strings.Contains(out, `href="/logo.webp"`) {
		t.Fatal(out)
	}
}

func TestCustomIndexUsesSiteName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("site_name: 测试站点\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code == http.StatusOK && !strings.Contains(w.Body.String(), "<title>测试站点</title>") {
		t.Fatalf("index title: %d %s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
}

func TestSiteConfigJSONShape(t *testing.T) {
	a := testApp(t, "")
	raw := request(t, a, "GET", "/api/config", "", 200).Body.Bytes()
	var cfg struct {
		SiteName   string `json:"site_name"`
		AvatarURL  string `json:"avatar_url"`
		FaviconURL string `json:"favicon_url"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SiteName == "" || cfg.AvatarURL == "" || cfg.FaviconURL == "" {
		t.Fatal(cfg)
	}
}
