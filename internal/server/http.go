package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"

	frontend "github.com/amazonmio/mio-image-hosting/web"
)

const maxJSONBodySize = 4 << 10

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Headers have already been sent; only logging is safe at this point.
		log.Printf("write JSON response: %v", err)
	}
}

func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}

func internal(w http.ResponseWriter, err error) {
	log.Print(err)
	fail(w, http.StatusInternalServerError, "服务器处理失败，请重试")
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		bodyReadError(w, err, "请求格式错误")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
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
		reply(w, http.StatusOK, a.siteConfig())
	})
	mux.HandleFunc("GET /branding/avatar", a.serveAvatar)
	mux.HandleFunc("GET /branding/favicon", a.serveFavicon)
	mux.HandleFunc("GET /favicon.ico", a.serveFavicon)
	mux.Handle("GET /api/images", a.protect(http.HandlerFunc(a.listImages)))
	mux.Handle("POST /api/images", a.protect(http.HandlerFunc(a.upload)))
	mux.Handle("PATCH /api/images/{id}", a.protect(http.HandlerFunc(a.updateImage)))
	mux.Handle("DELETE /api/images/{id}", a.protect(http.HandlerFunc(a.deleteImage)))
	mux.Handle("GET /api/folders", a.protect(http.HandlerFunc(a.listFolders)))
	mux.Handle("POST /api/folders", a.protect(http.HandlerFunc(a.createFolder)))
	mux.Handle("PATCH /api/folders/{id}", a.protect(http.HandlerFunc(a.renameFolder)))
	mux.Handle("DELETE /api/folders/{id}", a.protect(http.HandlerFunc(a.deleteFolder)))
	mux.HandleFunc("GET /i/{id}", a.serveImage)
	mux.HandleFunc("GET /download/{id}", a.serveImage)
	mux.HandleFunc("GET /t/{id}", a.serveThumbnail)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "接口不存在") })
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
