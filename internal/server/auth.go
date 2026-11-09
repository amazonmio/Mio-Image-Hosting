package server

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const sessionCookie = "mio_session"
const sessionTTL = 7 * 24 * time.Hour
const passwordIterations = 600000

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

type authAttempt struct {
	Count int
	Until time.Time
}

type authStatus struct {
	Initialized      bool   `json:"initialized"`
	Authenticated    bool   `json:"authenticated"`
	Username         string `json:"username,omitempty"`
	SetupKeyRequired bool   `json:"setup_key_required"`
}

func (a *App) initAuth(dir string) error {
	_, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS administrator (
			id INTEGER PRIMARY KEY CHECK(id=1),
			username TEXT NOT NULL,
			password_hash BLOB NOT NULL,
			salt BLOB NOT NULL,
			iterations INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			expires_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
	`)
	if err != nil {
		return err
	}
	if err = a.purgeExpiredSessions(); err != nil {
		return err
	}
	a.attempts = make(map[string]authAttempt)
	a.setupKeyPath = filepath.Join(dir, "setup-key.txt")
	ready, err := a.initialized()
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	key, err := os.ReadFile(a.setupKeyPath)
	if err == nil {
		a.setupKey = strings.TrimSpace(string(key))
		if len(a.setupKey) < 32 {
			return errors.New("invalid setup-key.txt")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	a.setupKey, err = randomToken()
	if err != nil {
		return err
	}
	return os.WriteFile(a.setupKeyPath, []byte(a.setupKey+"\n"), 0600)
}

func (a *App) initialized() (bool, error) {
	var count int
	err := a.db.QueryRow("SELECT COUNT(*) FROM administrator").Scan(&count)
	return count == 1, err
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func sameSecret(got, want string) bool {
	g := sha256.Sum256([]byte(got))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

func (a *App) apiTokenOK(r *http.Request) bool {
	return a.token != "" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && sameSecret(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), a.token)
}

func localSetup(r *http.Request) bool {
	// A loopback reverse proxy is not a local administrator. Any proxy hint
	// requires the setup key, even when the proxy rewrites Host to localhost.
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(peer).IsLoopback() {
		return false
	}
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (a *App) browserRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, http.StatusForbidden, "不允许跨站请求")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "https" && u.Scheme != "http") {
			fail(w, http.StatusForbidden, "不允许跨站请求")
			return false
		}
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-Mio-Request") != "1" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		fail(w, http.StatusForbidden, "请求校验失败，请刷新页面重试")
		return false
	}
	return true
}

func (a *App) sessionUser(r *http.Request) (string, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return "", nil
	}
	var name string
	err = a.db.QueryRowContext(r.Context(), "SELECT username FROM administrator WHERE id=1 AND EXISTS(SELECT 1 FROM sessions WHERE token_hash=? AND expires_at>?)", hashToken(cookie.Value), time.Now().Unix()).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

func (a *App) purgeExpiredSessions() error {
	_, err := a.db.Exec("DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix())
	return err
}

func forwardedHTTPS(r *http.Request) bool {
	proto := r.Header.Get("X-Forwarded-Proto")
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = proto[:i]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func (a *App) secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(a.baseURL, "https://") || forwardedHTTPS(r)
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/api", HttpOnly: true, Secure: a.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: int(sessionTTL.Seconds()), Expires: time.Now().Add(sessionTTL)})
}

func (a *App) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/api", HttpOnly: true, Secure: a.secureCookie(r), SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}

func addSession(tx *sql.Tx, token string) error {
	if _, err := tx.Exec("DELETE FROM sessions WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	_, err := tx.Exec("INSERT INTO sessions(token_hash,expires_at) VALUES(?,?)", hashToken(token), time.Now().Add(sessionTTL).Unix())
	return err
}

func (a *App) authState(w http.ResponseWriter, r *http.Request) {
	if err := a.purgeExpiredSessions(); err != nil {
		internal(w, err)
		return
	}
	ready, err := a.initialized()
	if err != nil {
		internal(w, err)
		return
	}
	name, err := a.sessionUser(r)
	if err != nil {
		internal(w, err)
		return
	}
	reply(w, http.StatusOK, authStatus{Initialized: ready, Authenticated: name != "", Username: name, SetupKeyRequired: !ready && (!localSetup(r) || a.token != "")})
}

// Called while holding authMu. Limiting by socket peer avoids trusting spoofed forwarded headers.
func (a *App) allowAttempt(w http.ResponseWriter, r *http.Request) bool {
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.Until) {
			delete(a.attempts, k)
		}
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}
	attempt := a.attempts[peer]
	if attempt.Count >= 10 || (attempt.Count == 0 && len(a.attempts) >= 4096) {
		w.Header().Set("Retry-After", "900")
		fail(w, http.StatusTooManyRequests, "尝试次数过多，请 15 分钟后重试")
		return false
	}
	if attempt.Count == 0 {
		attempt.Until = now.Add(15 * time.Minute)
	}
	attempt.Count++
	a.attempts[peer] = attempt
	return true
}

func passwordValid(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 10 && len(password) <= 256 && strings.TrimSpace(password) != ""
}

func passwordRecord(password string) ([]byte, []byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	return hash, salt, err
}

func verifyPassword(password string, hash, salt []byte, iterations int) bool {
	if len(password) > 256 || iterations < 1 || iterations > 2000000 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	return err == nil && subtle.ConstantTimeCompare(got, hash) == 1
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if !a.browserRequest(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		SetupKey string `json:"setup_key"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	ready, err := a.initialized()
	if err != nil {
		internal(w, err)
		return
	}
	if ready {
		fail(w, http.StatusConflict, "初始化已完成，请登录")
		return
	}
	if !a.allowAttempt(w, r) {
		return
	}
	if (!localSetup(r) || a.token != "") && !sameSecret(body.SetupKey, a.setupKey) {
		fail(w, http.StatusForbidden, "初始化密钥不正确，请查看数据目录中的 setup-key.txt")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if !usernamePattern.MatchString(body.Username) || !passwordValid(body.Password) {
		fail(w, http.StatusBadRequest, "账号须为 3–32 位字母、数字、下划线或短横线；密码至少 10 个字符且最多 256 字节")
		return
	}
	hash, salt, err := passwordRecord(body.Password)
	if err != nil {
		internal(w, err)
		return
	}
	token, err := randomToken()
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO administrator(id,username,password_hash,salt,iterations) VALUES(1,?,?,?,?)", body.Username, hash, salt, passwordIterations); err != nil {
		internal(w, err)
		return
	}
	if err = addSession(tx, token); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	// The database, not the presence of this file, determines setup completion.
	_ = os.Remove(a.setupKeyPath)
	a.setSessionCookie(w, r, token)
	reply(w, http.StatusCreated, authStatus{Initialized: true, Authenticated: true, Username: body.Username})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.browserRequest(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if !a.allowAttempt(w, r) {
		return
	}
	var name string
	var hash, salt []byte
	var iterations int
	err := a.db.QueryRow("SELECT username,password_hash,salt,iterations FROM administrator WHERE id=1").Scan(&name, &hash, &salt, &iterations)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusConflict, "请先完成初始化")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	valid := verifyPassword(body.Password, hash, salt, iterations)
	if !valid || strings.TrimSpace(body.Username) != name {
		fail(w, http.StatusUnauthorized, "账号或密码不正确")
		return
	}
	token, err := randomToken()
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	if old, e := r.Cookie(sessionCookie); e == nil {
		if _, err = tx.Exec("DELETE FROM sessions WHERE token_hash=?", hashToken(old.Value)); err != nil {
			internal(w, err)
			return
		}
	}
	if err = addSession(tx, token); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	delete(a.attempts, peer)
	a.setSessionCookie(w, r, token)
	reply(w, http.StatusOK, authStatus{Initialized: true, Authenticated: true, Username: name})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if !a.browserRequest(w, r) {
		return
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if c, err := r.Cookie(sessionCookie); err == nil {
		if _, err = a.db.Exec("DELETE FROM sessions WHERE token_hash=?", hashToken(c.Value)); err != nil {
			internal(w, err)
			return
		}
	}
	a.clearSessionCookie(w, r)
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	if !a.browserRequest(w, r) {
		return
	}
	var body struct {
		Current  string `json:"current_password"`
		Password string `json:"new_password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	user, err := a.sessionUser(r)
	if err != nil {
		internal(w, err)
		return
	}
	if user == "" {
		fail(w, http.StatusUnauthorized, "登录已过期，请重新登录")
		return
	}
	if !a.allowAttempt(w, r) {
		return
	}
	var hash, salt []byte
	var iterations int
	if err = a.db.QueryRow("SELECT password_hash,salt,iterations FROM administrator WHERE id=1").Scan(&hash, &salt, &iterations); err != nil {
		internal(w, err)
		return
	}
	if !verifyPassword(body.Current, hash, salt, iterations) {
		fail(w, http.StatusBadRequest, "当前密码不正确")
		return
	}
	if !passwordValid(body.Password) {
		fail(w, http.StatusBadRequest, "新密码至少 10 个字符且最多 256 字节")
		return
	}
	hash, salt, err = passwordRecord(body.Password)
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE administrator SET password_hash=?,salt=?,iterations=? WHERE id=1", hash, salt, passwordIterations); err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec("DELETE FROM sessions"); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		internal(w, err)
		return
	}
	a.clearSessionCookie(w, r)
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.browserRequest(w, r) {
			return
		}
		ready, err := a.initialized()
		if err != nil {
			internal(w, err)
			return
		}
		if !ready {
			fail(w, http.StatusUnauthorized, "请先完成初始化")
			return
		}
		if !a.apiTokenOK(r) {
			name, err := a.sessionUser(r)
			if err != nil {
				internal(w, err)
				return
			}
			if name == "" {
				if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
					fail(w, http.StatusUnauthorized, "上传令牌无效，请检查 ADMIN_TOKEN")
				} else {
					fail(w, http.StatusUnauthorized, "登录已过期，请重新登录")
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
