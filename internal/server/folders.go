package server

import (
	"net/http"
	"strings"
)

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
	reply(w, http.StatusOK, result)
}

func (a *App) folderExists(id *int64) bool {
	id = normalizeFolderID(id)
	if id == nil {
		return true
	}
	var v int
	return a.db.QueryRow("SELECT 1 FROM folders WHERE id=?", *id).Scan(&v) == nil
}

func folderName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return "", false
	}
	body.Name = strings.TrimSpace(body.Name)
	if len([]rune(body.Name)) < 1 || len([]rune(body.Name)) > 60 {
		fail(w, http.StatusBadRequest, "文件夹名称须为 1–60 个字符")
		return "", false
	}
	return body.Name, true
}

func (a *App) createFolder(w http.ResponseWriter, r *http.Request) {
	name, ok := folderName(w, r)
	if !ok {
		return
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "INSERT INTO folders(name) VALUES(?)", name)
	if err != nil {
		if uniqueConstraint(err) {
			fail(w, http.StatusConflict, "文件夹名称已存在")
		} else {
			internal(w, err)
		}
		return
	}
	id, _ := result.LastInsertId()
	reply(w, http.StatusCreated, Folder{ID: id, Name: name})
}

func (a *App) renameFolder(w http.ResponseWriter, r *http.Request) {
	name, ok := folderName(w, r)
	if !ok {
		return
	}
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "UPDATE folders SET name=? WHERE id=?", name, r.PathValue("id"))
	if err != nil {
		if uniqueConstraint(err) {
			fail(w, http.StatusConflict, "文件夹名称已存在")
		} else {
			internal(w, err)
		}
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, http.StatusNotFound, "文件夹不存在")
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteFolder(w http.ResponseWriter, r *http.Request) {
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	result, err := a.db.ExecContext(r.Context(), "DELETE FROM folders WHERE id=?", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		fail(w, http.StatusNotFound, "文件夹不存在")
		return
	}
	reply(w, http.StatusOK, map[string]bool{"ok": true})
}
