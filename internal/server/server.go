package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	frontend "github.com/amazonmio/mio-image-hosting/web"
	_ "golang.org/x/image/webp"
	sqlite "modernc.org/sqlite"
)

const (
	maxFileSize      int64 = 20 << 20
	schemaVersion          = 1
	sqliteConstraint       = 19 // SQLITE_CONSTRAINT
)

type App struct {
	imageLocksMu                         sync.Mutex
	imageLocks                           map[string]*imageLock
	decodeSlots                          chan struct{}
	authMu                               sync.Mutex
	setupKey, setupKeyPath               string
	attempts                             map[string]authAttempt
	db                                   *sql.DB
	dir, uploads, thumbs, token, baseURL string
	siteName                             string
	avatar, favicon                      brandAsset
	mu                                   sync.Mutex
}
type Picture struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MIME      string `json:"mime"`
	FolderID  *int64 `json:"folder_id"`
	CreatedAt string `json:"created_at"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumb_url"`
}
type Folder struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func New(dir, token, baseURL string) (*App, error) {
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("PUBLIC_BASE_URL must be an http(s) origin, e.g. https://images.example.com")
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	uploads := filepath.Join(abs, "uploads")
	thumbs := filepath.Join(abs, "thumbs")
	if err = os.MkdirAll(uploads, 0700); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(thumbs, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "app.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = applySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	app := &App{decodeSlots: make(chan struct{}, 1), db: db, dir: abs, uploads: uploads, thumbs: thumbs, token: token, baseURL: strings.TrimRight(baseURL, "/")}
	if err = app.loadSiteConfig(); err != nil {
		db.Close()
		return nil, err
	}
	if err = app.initAuth(abs); err != nil {
		db.Close()
		return nil, err
	}
	if err = app.recoverUploads(); err != nil {
		db.Close()
		return nil, err
	}
	if err = app.recoverThumbs(); err != nil {
		db.Close()
		return nil, err
	}
	return app, nil
}

func applySchema(db *sql.DB) error {
	_, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS folders (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
 CREATE TABLE IF NOT EXISTS images (id TEXT PRIMARY KEY, name TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL, mime TEXT NOT NULL, folder_id INTEGER REFERENCES folders(id) ON DELETE SET NULL, created_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS images_folder_date ON images(folder_id,created_at DESC);
 CREATE INDEX IF NOT EXISTS images_date ON images(created_at DESC);`)
	if err != nil {
		return err
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema %d is newer than this version (%d)", version, schemaVersion)
	}
	if version == schemaVersion {
		return nil
	}
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version=%d", schemaVersion))
	return err
}

func uniqueConstraint(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code()&0xff == sqliteConstraint
}

func normalizeFolderID(id *int64) *int64 {
	if id == nil || *id == 0 {
		return nil
	}
	return id
}

func (a *App) Close() error { return a.db.Close() }
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func internal(w http.ResponseWriter, err error) {
	log.Print(err)
	fail(w, 500, "服务器处理失败，请重试")
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		bodyReadError(w, err, "请求格式错误")
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		bodyReadError(w, err, "请求格式错误")
		return false
	}
	return true
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", a.authState)
	mux.HandleFunc("POST /api/auth/setup", a.setup)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("POST /api/auth/password", a.changePassword)
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, a.siteConfig())
	})
	mux.HandleFunc("GET /branding/avatar", a.serveAvatar)
	mux.HandleFunc("GET /branding/favicon", a.serveFavicon)
	mux.HandleFunc("GET /favicon.ico", a.serveFavicon)
	mux.Handle("GET /api/images", a.protect(http.HandlerFunc(a.listImages)))
	mux.Handle("POST /api/images", a.protect(http.HandlerFunc(a.upload)))
	mux.Handle("PATCH /api/images/{id}", a.protect(http.HandlerFunc(a.moveImage)))
	mux.Handle("DELETE /api/images/{id}", a.protect(http.HandlerFunc(a.deleteImage)))
	mux.Handle("GET /api/folders", a.protect(http.HandlerFunc(a.listFolders)))
	mux.Handle("POST /api/folders", a.protect(http.HandlerFunc(a.createFolder)))
	mux.Handle("PATCH /api/folders/{id}", a.protect(http.HandlerFunc(a.renameFolder)))
	mux.Handle("DELETE /api/folders/{id}", a.protect(http.HandlerFunc(a.deleteFolder)))
	mux.HandleFunc("GET /i/{id}", a.serveImage)
	mux.HandleFunc("GET /download/{id}", a.serveImage)
	mux.HandleFunc("GET /t/{id}", a.serveThumbnail)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })
	assets, _ := fs.Sub(frontend.Files, "dist")
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			a.serveIndex(w, r, assets)
			return
		}
		http.FileServer(http.FS(assets)).ServeHTTP(w, r)
	}))
	return withBodyDeadlines(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	}), jsonBodyTimeout, uploadBodyTimeout)
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
			fail(w, 401, "请先完成初始化")
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
					fail(w, 401, "上传令牌无效，请检查 ADMIN_TOKEN")
				} else {
					fail(w, 401, "登录已过期，请重新登录")
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (a *App) imageURL(id string) string { return a.baseURL + "/i/" + id }
func (a *App) listFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT f.id,f.name,COUNT(i.id) FROM folders f LEFT JOIN images i ON i.folder_id=f.id GROUP BY f.id ORDER BY f.name COLLATE NOCASE")
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	result := []Folder{}
	for rows.Next() {
		var f Folder
		if err = rows.Scan(&f.ID, &f.Name, &f.Count); err != nil {
			internal(w, err)
			return
		}
		result = append(result, f)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, result)
}
func (a *App) listImages(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	where := " WHERE 1=1"
	args := []any{}
	if f := r.URL.Query().Get("folder"); f != "" {
		if f == "0" {
			where += " AND folder_id IS NULL"
		} else {
			id, err := strconv.ParseInt(f, 10, 64)
			if err != nil || id < 1 {
				fail(w, 400, "文件夹无效")
				return
			}
			where += " AND folder_id=?"
			args = append(args, id)
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		where += " AND instr(lower(name),lower(?))>0"
		args = append(args, q)
	}
	var total, all, uncategorized int
	var size int64
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM images"+where, args...).Scan(&total); err != nil {
		internal(w, err)
		return
	}
	if err := a.db.QueryRowContext(r.Context(), "SELECT COUNT(*),COALESCE(SUM(size),0),COUNT(*) FILTER (WHERE folder_id IS NULL) FROM images").Scan(&all, &size, &uncategorized); err != nil {
		internal(w, err)
		return
	}
	args = append(args, 48, (page-1)*48)
	rows, err := a.db.QueryContext(r.Context(), "SELECT id,name,size,width,height,mime,folder_id,created_at FROM images"+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Picture{}
	for rows.Next() {
		var p Picture
		if err = rows.Scan(&p.ID, &p.Name, &p.Size, &p.Width, &p.Height, &p.MIME, &p.FolderID, &p.CreatedAt); err != nil {
			internal(w, err)
			return
		}
		p.URL = a.imageURL(p.ID)
		p.ThumbURL = a.thumbURL(p.ID)
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	reply(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": 48, "all_count": all, "total_size": size, "uncategorized_count": uncategorized})
}
func (a *App) folderExists(id *int64) bool {
	id = normalizeFolderID(id)
	if id == nil {
		return true
	}
	var v int
	return a.db.QueryRow("SELECT 1 FROM folders WHERE id=?", *id).Scan(&v) == nil
}
func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		bodyReadError(w, err, "上传无效或超过 20 MB 限制")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["file"]) != 1 {
		fail(w, 400, "每次请求请上传一张图片")
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, "请选择图片")
		return
	}
	defer f.Close()
	if header.Size > maxFileSize || header.Size == 0 {
		fail(w, 400, "图片大小须为 1 字节至 20 MB")
		return
	}
	var folderID *int64
	if raw := r.FormValue("folder_id"); raw != "" && raw != "0" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, 400, "文件夹无效")
			return
		}
		folderID = &id
	}
	cfg, format, err := a.validateImage(r.Context(), f)
	if err != nil {
		if errors.Is(err, errImageBusy) {
			w.Header().Set("Retry-After", "1")
			fail(w, http.StatusServiceUnavailable, err.Error())
		} else {
			fail(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	exts := map[string]string{"jpeg": ".jpg", "png": ".png", "gif": ".gif", "webp": ".webp"}
	ext, ok := exts[format]
	if !ok {
		fail(w, 400, "不支持该图片格式")
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		internal(w, err)
		return
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		internal(w, err)
		return
	}
	id := hex.EncodeToString(random) + ext
	tmp, err := os.CreateTemp(a.uploads, ".upload-")
	if err != nil {
		internal(w, err)
		return
	}
	defer os.Remove(tmp.Name())
	size, copyErr := io.Copy(tmp, io.LimitReader(f, maxFileSize+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		internal(w, errors.Join(copyErr, syncErr, closeErr))
		return
	}
	if size > maxFileSize {
		fail(w, 400, "图片超过 20 MB")
		return
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if len([]rune(name)) > 180 {
		name = string([]rune(name)[:180])
	}
	p := Picture{ID: id, Name: name, Size: size, Width: cfg.Width, Height: cfg.Height, MIME: mime.TypeByExtension(ext), FolderID: folderID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), URL: a.imageURL(id), ThumbURL: a.thumbURL(id)}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.folderExists(folderID) {
		fail(w, 400, "文件夹不存在，请刷新后重试")
		return
	}
	dest := filepath.Join(a.uploads, id)
	if err = os.Rename(tmp.Name(), dest); err != nil {
		internal(w, err)
		return
	}
	_, err = a.db.ExecContext(r.Context(), "INSERT INTO images(id,name,size,width,height,mime,folder_id,created_at) VALUES(?,?,?,?,?,?,?,?)", p.ID, p.Name, p.Size, p.Width, p.Height, p.MIME, p.FolderID, p.CreatedAt)
	if err != nil {
		os.Remove(dest)
		internal(w, err)
		return
	}
	if err := a.writeThumbFromOriginal(r.Context(), dest, id); err != nil {
		log.Printf("thumbnail %s: %v", id, err)
	}
	reply(w, 201, p)
}
func (a *App) moveImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FolderID *int64 `json:"folder_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.FolderID = normalizeFolderID(body.FolderID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.folderExists(body.FolderID) {
		fail(w, 400, "文件夹不存在")
		return
	}
	result, err := a.db.ExecContext(r.Context(), "UPDATE images SET folder_id=? WHERE id=?", body.FolderID, r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "图片不存在")
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteImage(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	id := r.PathValue("id")
	release, err := a.lockImage(r.Context(), id)
	if err != nil {
		fail(w, http.StatusRequestTimeout, "删除请求已取消，请重试")
		return
	}
	defer release()
	var stored string
	if err := a.db.QueryRow("SELECT id FROM images WHERE id=?", id).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "图片不存在")
		} else {
			internal(w, err)
		}
		return
	}
	path := filepath.Join(a.uploads, stored)
	trash := path + ".trash"
	moved := false
	if err := os.Rename(path, trash); err != nil && !os.IsNotExist(err) {
		internal(w, err)
		return
	} else if err == nil {
		moved = true
	}
	if _, err := a.db.ExecContext(r.Context(), "DELETE FROM images WHERE id=?", id); err != nil {
		if moved {
			if e := os.Rename(trash, path); e != nil {
				log.Print(e)
			}
		}
		internal(w, err)
		return
	}
	if moved {
		if err := os.Remove(trash); err != nil {
			log.Printf("cleanup %s: %v", id, err)
		}
	}
	a.removeThumb(stored)
	reply(w, 200, map[string]bool{"ok": true})
}
func folderName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var b struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &b) {
		return "", false
	}
	b.Name = strings.TrimSpace(b.Name)
	if len([]rune(b.Name)) < 1 || len([]rune(b.Name)) > 60 {
		fail(w, 400, "文件夹名称须为 1–60 个字符")
		return "", false
	}
	return b.Name, true
}
func (a *App) createFolder(w http.ResponseWriter, r *http.Request) {
	name, ok := folderName(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "INSERT INTO folders(name) VALUES(?)", name)
	if err != nil {
		if uniqueConstraint(err) {
			fail(w, 409, "文件夹名称已存在")
		} else {
			internal(w, err)
		}
		return
	}
	id, _ := result.LastInsertId()
	reply(w, 201, Folder{ID: id, Name: name})
}
func (a *App) renameFolder(w http.ResponseWriter, r *http.Request) {
	name, ok := folderName(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "UPDATE folders SET name=? WHERE id=?", name, r.PathValue("id"))
	if err != nil {
		if uniqueConstraint(err) {
			fail(w, 409, "文件夹名称已存在")
		} else {
			internal(w, err)
		}
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "文件夹不存在")
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteFolder(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM folders WHERE id=?", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, 404, "文件夹不存在")
		return
	}
	reply(w, 200, map[string]bool{"ok": true})
}
func (a *App) serveImage(w http.ResponseWriter, r *http.Request) {
	var id, name, contentType string
	if err := a.db.QueryRowContext(r.Context(), "SELECT id,name,mime FROM images WHERE id=?", r.PathValue("id")).Scan(&id, &name, &contentType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			internal(w, err)
		}
		return
	}
	f, err := os.Open(filepath.Join(a.uploads, id))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.Header().Set("ETag", fmt.Sprintf("%q", id))
	if strings.HasPrefix(r.URL.Path, "/download/") {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}
