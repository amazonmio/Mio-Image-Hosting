package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testPassword = "test-only-strong-password"

func freshAuthApp(t *testing.T) *App {
	t.Helper()
	a, err := New(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}
func authRequest(t *testing.T, a *App, method, path, body string, cookie *http.Cookie, code int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Mio-Request", "1")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != code {
		t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, code, w.Body.String())
	}
	return w
}
func setupAdmin(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	w := authRequest(t, a, "POST", "/api/auth/setup", `{"username":"admin","password":"`+testPassword+`"}`, nil, 201)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	return cookies[0]
}
func TestBootstrapSessionLifecycle(t *testing.T) {
	a := freshAuthApp(t)
	state := decode[authStatus](t, authRequest(t, a, "GET", "/api/auth/status", "", nil, 200))
	if state.Initialized || state.Authenticated || state.SetupKeyRequired {
		t.Fatal(state)
	}
	authRequest(t, a, "GET", "/api/images", "", nil, 401)
	authRequest(t, a, "POST", "/api/auth/setup", `{"username":"admin","password":"short"}`, nil, 400)
	cookie := setupAdmin(t, a)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api" || cookie.MaxAge != 604800 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if _, err := os.Stat(a.setupKeyPath); !os.IsNotExist(err) {
		t.Fatal("setup key was not removed")
	}
	var stored string
	if err := a.db.QueryRow("SELECT token_hash FROM sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == cookie.Value || stored != hashToken(cookie.Value) {
		t.Fatal("session should be hashed")
	}
	var hash []byte
	if err := a.db.QueryRow("SELECT password_hash FROM administrator").Scan(&hash); err != nil || string(hash) == testPassword {
		t.Fatal("password was not hashed")
	}
	authRequest(t, a, "POST", "/api/auth/setup", `{"username":"other","password":"`+testPassword+`"}`, nil, 409)
	authRequest(t, a, "GET", "/api/folders", "", cookie, 200)
	state = decode[authStatus](t, authRequest(t, a, "GET", "/api/auth/status", "", cookie, 200))
	if !state.Initialized || !state.Authenticated || state.Username != "admin" {
		t.Fatal(state)
	}
	authRequest(t, a, "POST", "/api/auth/logout", "", cookie, 200)
	authRequest(t, a, "GET", "/api/folders", "", cookie, 401)
	authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, nil, 401)
	authRequest(t, a, "POST", "/api/auth/login", `{"username":"other","password":"`+testPassword+`"}`, nil, 401)
	login := authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"`+testPassword+`"}`, nil, 200)
	second := login.Result().Cookies()[0]
	if second.Value == cookie.Value {
		t.Fatal("session was reused")
	}
	authRequest(t, a, "GET", "/api/images", "", second, 200)
	if _, err := a.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	authRequest(t, a, "GET", "/api/images", "", second, 401)
}
func TestPasswordChangeRevokesEverySession(t *testing.T) {
	a := freshAuthApp(t)
	first := setupAdmin(t, a)
	second := authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"`+testPassword+`"}`, nil, 200).Result().Cookies()[0]
	authRequest(t, a, "POST", "/api/auth/password", `{"current_password":"wrong","new_password":"another-strong-password"}`, first, 400)
	authRequest(t, a, "POST", "/api/auth/password", `{"current_password":"`+testPassword+`","new_password":"another-strong-password"}`, first, 200)
	authRequest(t, a, "GET", "/api/folders", "", first, 401)
	authRequest(t, a, "GET", "/api/folders", "", second, 401)
	authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"`+testPassword+`"}`, nil, 401)
	authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"another-strong-password"}`, nil, 200)
}
func TestSetupRemoteKeyAndCSRF(t *testing.T) {
	a := freshAuthApp(t)
	r := httptest.NewRequest("POST", "http://images.example/api/auth/setup", strings.NewReader(`{"username":"admin","password":"`+testPassword+`"}`))
	r.Header.Set("X-Mio-Request", "1")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	payload, _ := json.Marshal(map[string]string{"username": "admin", "password": testPassword, "setup_key": a.setupKey})
	r = httptest.NewRequest("POST", "http://images.example/api/auth/setup", strings.NewReader(string(payload)))
	r.Header.Set("X-Mio-Request", "1")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	for _, path := range []string{"/api/folders", "/api/auth/password", "/api/auth/logout", "/api/auth/login"} {
		r = httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(`{}`))
		r.AddCookie(cookie)
		w = httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("missing custom header accepted for %s: %d", path, w.Code)
		}
		r.Header.Set("X-Mio-Request", "1")
		r.Header.Set("Origin", "https://attacker.example")
		w = httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross origin accepted")
		}
	}
	a.baseURL = "https://images.example"
	w = httptest.NewRecorder()
	a.setSessionCookie(w, httptest.NewRequest("GET", "/", nil), "test")
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("missing Secure on HTTPS deployment")
	}
}
func TestLoginRateLimit(t *testing.T) {
	a := freshAuthApp(t)
	setupAdmin(t, a)
	a.attempts = make(map[string]authAttempt)
	for i := 0; i < 10; i++ {
		authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, nil, 401)
	}
	authRequest(t, a, "POST", "/api/auth/login", `{"username":"admin","password":"wrong"}`, nil, 429)
}
func TestConcurrentSetupCreatesOneAdmin(t *testing.T) {
	a := freshAuthApp(t)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("POST", "http://localhost/api/auth/setup", strings.NewReader(`{"username":"admin","password":"`+testPassword+`"}`))
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("X-Mio-Request", "1")
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	ok, conflict := 0, 0
	for code := range codes {
		if code == 201 {
			ok++
		}
		if code == 409 {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("setup race: %d successful %d conflicts", ok, conflict)
	}
}
func TestAuthPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	cookie := setupAdmin(t, a)
	a.Close()
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	state := decode[authStatus](t, authRequest(t, a, "GET", "/api/auth/status", "", cookie, 200))
	if !state.Initialized || !state.Authenticated {
		t.Fatal("session or setup state lost")
	}
	authRequest(t, a, "POST", "/api/auth/setup", `{"username":"admin","password":"`+testPassword+`"}`, nil, 409)
}

func TestExpiredSessionsPurgedOnRestart(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	setupAdmin(t, a)
	if _, err = a.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var n int
	if err = a.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired session not purged: %d %v", n, err)
	}
}

func TestSecureCookieTrustsForwardedHTTPS(t *testing.T) {
	a := freshAuthApp(t)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https, http")
	w := httptest.NewRecorder()
	a.setSessionCookie(w, r, "test")
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("missing Secure behind HTTPS reverse proxy")
	}
}
