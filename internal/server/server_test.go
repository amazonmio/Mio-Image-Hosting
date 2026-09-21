package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enableFixtureAdmin(t *testing.T, a *App) {
	t.Helper()
	if a.token == "" {
		a.token = "fixture-api-token"
	}
	if _, err := a.db.Exec("INSERT OR IGNORE INTO administrator(id,username,password_hash,salt,iterations) VALUES(1,'fixture',X'01',X'02',600000)"); err != nil {
		t.Fatal(err)
	}
}
func testApp(t *testing.T, token string) *App {
	t.Helper()
	a, err := New(t.TempDir(), token, "https://img.example.com")
	if err != nil {
		t.Fatal(err)
	}
	enableFixtureAdmin(t, a)
	t.Cleanup(func() { a.Close() })
	return a
}
func request(t *testing.T, a *App, method, path, body string, code int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Mio-Request", "1")
	if code != 401 {
		enableFixtureAdmin(t, a)
		r.Header.Set("Authorization", "Bearer "+a.token)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != code {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, code, w.Body.String())
	}
	return w
}
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	img.Set(1, 1, color.RGBA{255, 128, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func sendUpload(t *testing.T, a *App, body []byte, folder string, code int) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	f, err := m.CreateFormFile("file", "旅行照片.png")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(body)
	if folder != "" {
		m.WriteField("folder_id", folder)
	}
	m.Close()
	r := httptest.NewRequest("POST", "/api/images", &b)
	r.Header.Set("Content-Type", m.FormDataContentType())
	enableFixtureAdmin(t, a)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != code {
		t.Fatalf("upload: got %d want %d: %s", w.Code, code, w.Body.String())
	}
	return w
}
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImageLifecycle(t *testing.T) {
	a := testApp(t, "")
	folder := decode[Folder](t, request(t, a, "POST", "/api/folders", `{"name":"博客配图"}`, 201))
	payload := pngBytes(t)
	p := decode[Picture](t, sendUpload(t, a, payload, fmt.Sprint(folder.ID), 201))
	if p.Width != 8 || p.Height != 6 || p.Size != int64(len(payload)) || p.FolderID == nil || *p.FolderID != folder.ID {
		t.Fatalf("unexpected metadata: %+v", p)
	}
	if p.URL != "https://img.example.com/i/"+p.ID {
		t.Fatal(p.URL)
	}
	raw := request(t, a, "GET", "/i/"+p.ID, "", 200)
	if !bytes.Equal(raw.Body.Bytes(), payload) {
		t.Fatal("image content changed")
	}
	download := request(t, a, "GET", "/download/"+p.ID, "", 200)
	if !strings.Contains(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("missing download disposition")
	}
	if !bytes.Equal(download.Body.Bytes(), payload) {
		t.Fatal("download corrupted")
	}
	list := decode[struct {
		Total int
		Items []Picture
	}](t, request(t, a, "GET", fmt.Sprintf("/api/images?folder=%d&q=旅行", folder.ID), "", 200))
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatal("filter failed")
	}
	request(t, a, "PATCH", fmt.Sprintf("/api/folders/%d", folder.ID), `{"name":"新名称"}`, 200)
	request(t, a, "GET", "/i/"+p.ID, "", 200)
	request(t, a, "PATCH", "/api/images/"+p.ID, `{"folder_id":null}`, 200)
	list = decode[struct {
		Total int
		Items []Picture
	}](t, request(t, a, "GET", "/api/images?folder=0", "", 200))
	if list.Total != 1 || list.Items[0].FolderID != nil {
		t.Fatal("move failed")
	}
	request(t, a, "PATCH", "/api/images/"+p.ID, fmt.Sprintf(`{"folder_id":%d}`, folder.ID), 200)
	request(t, a, "DELETE", fmt.Sprintf("/api/folders/%d", folder.ID), "", 200)
	list = decode[struct {
		Total int
		Items []Picture
	}](t, request(t, a, "GET", "/api/images?folder=0", "", 200))
	if list.Total != 1 {
		t.Fatal("folder delete lost image")
	}
	request(t, a, "GET", "/i/"+p.ID, "", 200)
	request(t, a, "DELETE", "/api/images/"+p.ID, "", 200)
	request(t, a, "GET", "/i/"+p.ID, "", 404)
	request(t, a, "GET", "/download/"+p.ID, "", 404)
	request(t, a, "DELETE", "/api/images/"+p.ID, "", 404)
	entries, err := os.ReadDir(a.uploads)
	if err != nil || len(entries) != 0 {
		t.Fatalf("file cleanup failed: %v %v", entries, err)
	}
}
func TestValidation(t *testing.T) {
	a := testApp(t, "")
	sendUpload(t, a, []byte("<svg onload=alert(1)></svg>"), "", 400)
	sendUpload(t, a, pngBytes(t), "99999", 400)
	sendUpload(t, a, pngBytes(t), "invalid", 400)
	sendUpload(t, a, append(pngBytes(t), make([]byte, maxFileSize)...), "", 400)
	request(t, a, "POST", "/api/folders", `{"name":" "}`, 400)
	request(t, a, "POST", "/api/folders", `{"name":"test"}`, 201)
	request(t, a, "POST", "/api/folders", `{"name":"test"}`, 409)
	request(t, a, "PATCH", "/api/folders/9999", `{"name":"another"}`, 404)
	request(t, a, "DELETE", "/api/folders/9999", "", 404)
	request(t, a, "POST", "/api/folders", `{"name":"x"} {"name":"y"}`, 400)
	request(t, a, "GET", "/api/images?folder=abc", "", 400)
	request(t, a, "GET", "/i/missing.png", "", 404)
	entries, _ := os.ReadDir(a.uploads)
	if len(entries) != 0 {
		t.Fatal("rejected uploads left files")
	}
}
func TestAuthenticationAndOrigin(t *testing.T) {
	a := testApp(t, "secret")
	request(t, a, "GET", "/api/config", "", 200)
	request(t, a, "GET", "/api/images", "", 401)
	r := httptest.NewRequest("GET", "/api/images", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":"x"}`))
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	public := testApp(t, "")
	r = httptest.NewRequest("POST", "/api/folders", strings.NewReader(`{"name":"x"}`))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	public.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestPersistenceAndDeleteRecovery(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	a.Close()
	path := filepath.Join(dir, "uploads", p.ID)
	if err = os.Rename(path, path+".trash"); err != nil {
		t.Fatal(err)
	}
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	request(t, a, "GET", "/i/"+p.ID, "", 200)
	list := decode[struct{ Total int }](t, request(t, a, "GET", "/api/images", "", 200))
	if list.Total != 1 {
		t.Fatal("database did not persist")
	}
}
func TestPagination(t *testing.T) {
	a := testApp(t, "")
	for i := 0; i < 51; i++ {
		_, err := a.db.Exec("INSERT INTO images(id,name,size,width,height,mime,created_at) VALUES(?,?,1,1,1,'image/png',?)", fmt.Sprint(i), fmt.Sprintf("image-%02d.png", i), fmt.Sprintf("2026-01-01T00:00:%02dZ", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	type Result struct {
		Total int
		Items []Picture
	}
	first := decode[Result](t, request(t, a, "GET", "/api/images", "", 200))
	second := decode[Result](t, request(t, a, "GET", "/api/images?page=2", "", 200))
	if first.Total != 51 || len(first.Items) != 48 || len(second.Items) != 3 {
		t.Fatal("pagination failed")
	}
}
func TestRangeAndRevalidation(t *testing.T) {
	a := testApp(t, "")
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/i/"+p.ID, nil)
	req.Header.Set("Range", "bytes=0-7")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || len(body) != 8 {
		t.Fatal("range failed")
	}
	req, _ = http.NewRequest("GET", srv.URL+"/i/"+p.ID, nil)
	req.Header.Set("If-None-Match", fmt.Sprintf("%q", p.ID))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 304 {
		t.Fatal("revalidation failed")
	}
}

func TestMoveToUncategorizedZero(t *testing.T) {
	a := testApp(t, "")
	folder := decode[Folder](t, request(t, a, "POST", "/api/folders", `{"name":"归档"}`, 201))
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), fmt.Sprint(folder.ID), 201))
	request(t, a, "PATCH", "/api/images/"+p.ID, `{"folder_id":0}`, 200)
	list := decode[struct {
		Total int
		Items []Picture
	}](t, request(t, a, "GET", "/api/images?folder=0", "", 200))
	if list.Total != 1 || list.Items[0].FolderID != nil {
		t.Fatal("folder_id 0 should mean uncategorized")
	}
}

func TestOrphanUploadCleanupAndSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err = a.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version: %d %v", version, err)
	}
	p := decode[Picture](t, sendUpload(t, a, pngBytes(t), "", 201))
	orphan := filepath.Join(dir, "uploads", "deadbeef.png")
	if err = os.WriteFile(orphan, []byte("nope"), 0600); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan upload remained")
	}
	request(t, a, "GET", "/i/"+p.ID, "", 200)
}

func TestRejectsNewerSchema(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if _, err = New(dir, "", ""); err == nil {
		t.Fatal("accepted a newer database schema")
	}
}
