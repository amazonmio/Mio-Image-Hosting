package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Only registered originals are returned to the active uploads directory.
// Unknown files are preserved outside the public image paths for manual recovery.
func (a *App) recoverUploads() error {
	known := map[string]struct{}{}
	rows, err := a.db.Query("SELECT id FROM images")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = struct{}{}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(a.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open("uploads")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	batch := ""
	quarantine := func(name, reason string) error {
		if batch == "" {
			if err := root.Mkdir("quarantine", 0700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err := root.Lstat("quarantine")
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("quarantine 必须是数据目录内的真实文件夹")
			}
			token, err := randomToken()
			if err != nil {
				return err
			}
			batch = filepath.Join("quarantine", time.Now().UTC().Format("20060102T150405Z")+"-"+token[:16])
			if err = root.Mkdir(batch, 0700); err != nil {
				return err
			}
		}
		dest := filepath.Join(batch, name)
		if err := root.Rename(filepath.Join("uploads", name), dest); err != nil {
			return fmt.Errorf("隔离 %q 失败，原文件未主动删除: %w", name, err)
		}
		log.Printf("已隔离文件 %q 至 %q（%s），请人工核对", name, dest, reason)
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			if err = quarantine(name, "上传目录中的文件链接"); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(name, ".trash") {
			id := strings.TrimSuffix(name, ".trash")
			if _, ok := known[id]; ok {
				_, e := root.Lstat(filepath.Join("uploads", id))
				if os.IsNotExist(e) {
					err = root.Rename(filepath.Join("uploads", name), filepath.Join("uploads", id))
				} else if e != nil {
					err = e
				} else {
					err = quarantine(name, "原文件已存在，保留冲突副本")
				}
			} else {
				err = quarantine(name, "删除中断或缺少数据库记录")
			}
		} else if _, ok := known[name]; !ok {
			err = quarantine(name, "数据库未登记的文件")
		}
		if err != nil {
			return err
		}
	}
	return nil
}
