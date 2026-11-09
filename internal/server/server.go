package server

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	sqlite "modernc.org/sqlite"
)

const (
	maxFileSize      int64 = 20 << 20
	schemaVersion          = 1
	sqliteConstraint       = 19 // SQLITE_CONSTRAINT
)

type App struct {
	db              *sql.DB
	dir             string
	uploads         string
	thumbs          string
	token           string
	baseURL         string
	siteName        string
	avatar, favicon brandAsset

	// Serialize database/file mutations; readers use database or per-image locks.
	mutationMu   sync.Mutex
	imageLocksMu sync.Mutex
	imageLocks   map[string]*imageLock
	decodeSlots  chan struct{}

	// Protect setup, password changes, login throttling and session creation.
	authMu                 sync.Mutex
	setupKey, setupKeyPath string
	attempts               map[string]authAttempt
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
	// Recovery and runtime writes must use real directories, never linked storage.
	for _, path := range []string{uploads, thumbs} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("storage directory must not be a link: %s", path)
		}
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
	app := &App{
		db:          db,
		dir:         abs,
		uploads:     uploads,
		thumbs:      thumbs,
		token:       token,
		baseURL:     strings.TrimRight(baseURL, "/"),
		decodeSlots: make(chan struct{}, 1),
	}
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
	_, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		PRAGMA foreign_keys=ON;
		PRAGMA busy_timeout=5000;
		CREATE TABLE IF NOT EXISTS folders (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS images (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			size INTEGER NOT NULL,
			width INTEGER NOT NULL,
			height INTEGER NOT NULL,
			mime TEXT NOT NULL,
			folder_id INTEGER REFERENCES folders(id) ON DELETE SET NULL,
			created_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS images_folder_date ON images(folder_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS images_date ON images(created_at DESC);
	`)
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
