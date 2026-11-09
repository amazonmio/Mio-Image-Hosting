package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const imagePageSize = 48

type imageListResponse struct {
	Items              []Picture `json:"items"`
	Total              int       `json:"total"`
	Page               int       `json:"page"`
	PageSize           int       `json:"page_size"`
	AllCount           int       `json:"all_count"`
	TotalSize          int64     `json:"total_size"`
	UncategorizedCount int       `json:"uncategorized_count"`
}

func (a *App) imageURL(id string) string { return a.baseURL + "/i/" + id }

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
				fail(w, http.StatusBadRequest, "文件夹无效")
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
	args = append(args, imagePageSize, (page-1)*imagePageSize)
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
	reply(w, http.StatusOK, imageListResponse{
		Items:              items,
		Total:              total,
		Page:               page,
		PageSize:           imagePageSize,
		AllCount:           all,
		TotalSize:          size,
		UncategorizedCount: uncategorized,
	})
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
		fail(w, http.StatusBadRequest, "每次请求请上传一张图片")
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, "请选择图片")
		return
	}
	defer f.Close()
	if header.Size > maxFileSize || header.Size == 0 {
		fail(w, http.StatusBadRequest, "图片大小须为 1 字节至 20 MB")
		return
	}
	var folderID *int64
	if raw := r.FormValue("folder_id"); raw != "" && raw != "0" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			fail(w, http.StatusBadRequest, "文件夹无效")
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
		fail(w, http.StatusBadRequest, "不支持该图片格式")
		return
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		internal(w, err)
		return
	}
	random := make([]byte, 8)
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
		fail(w, http.StatusBadRequest, "图片超过 20 MB")
		return
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if len([]rune(name)) > 180 {
		name = string([]rune(name)[:180])
	}
	name, err = parseImageName(name)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	p := Picture{ID: id, Name: name, Size: size, Width: cfg.Width, Height: cfg.Height, MIME: mime.TypeByExtension(ext), FolderID: folderID, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), URL: a.imageURL(id), ThumbURL: a.thumbURL(id)}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	if !a.folderExists(folderID) {
		fail(w, http.StatusBadRequest, "文件夹不存在，请刷新后重试")
		return
	}
	var dest string
	available := false
	for attempt := 0; attempt < 8; attempt++ {
		dest = filepath.Join(a.uploads, id)
		if _, checkErr := os.Lstat(dest); os.IsNotExist(checkErr) {
			available = true
			break
		} else if checkErr != nil {
			internal(w, checkErr)
			return
		}
		if _, err = rand.Read(random); err != nil {
			internal(w, err)
			return
		}
		id = hex.EncodeToString(random) + ext
	}
	if !available {
		fail(w, http.StatusConflict, "图片编号冲突，请重试上传")
		return
	}
	p.ID, p.URL, p.ThumbURL = id, a.imageURL(id), a.thumbURL(id)
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
	reply(w, http.StatusCreated, p)
}

func parseImageName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 180 {
		return "", errors.New("图片名称须为 1–180 个字符")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", errors.New("图片名称不能包含路径字符")
	}
	for _, r := range name {
		if r == '\uFFFD' || unicode.IsControl(r) {
			return "", errors.New("图片名称不能包含控制字符")
		}
	}
	return name, nil
}

func (a *App) updateImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FolderID json.RawMessage `json:"folder_id"`
		Name     json.RawMessage `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	hasFolder := len(body.FolderID) > 0
	hasName := len(body.Name) > 0
	if !hasFolder && !hasName {
		fail(w, http.StatusBadRequest, "请提供名称或文件夹")
		return
	}
	var folderID *int64
	if hasFolder {
		if string(body.FolderID) != "null" {
			var id int64
			if err := json.Unmarshal(body.FolderID, &id); err != nil {
				fail(w, http.StatusBadRequest, "文件夹无效")
				return
			}
			folderID = &id
		}
		folderID = normalizeFolderID(folderID)
	}
	var name string
	if hasName {
		if err := json.Unmarshal(body.Name, &name); err != nil {
			fail(w, http.StatusBadRequest, "名称无效")
			return
		}
		parsed, err := parseImageName(name)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		name = parsed
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	if hasFolder && !a.folderExists(folderID) {
		fail(w, http.StatusBadRequest, "文件夹不存在")
		return
	}
	sets := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if hasFolder {
		sets = append(sets, "folder_id=?")
		args = append(args, folderID)
	}
	if hasName {
		sets = append(sets, "name=?")
		args = append(args, name)
	}
	args = append(args, r.PathValue("id"))
	result, err := a.db.ExecContext(r.Context(), "UPDATE images SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, http.StatusNotFound, "图片不存在")
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteImage(w http.ResponseWriter, r *http.Request) {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
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
			fail(w, http.StatusNotFound, "图片不存在")
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
	reply(w, http.StatusOK, map[string]bool{"ok": true})
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
