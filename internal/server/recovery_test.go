package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryKeepsOriginalAfterMetadataRollback(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	raw := pngBytes(t)
	p := decode[Picture](t, sendUpload(t, a, raw, "", 201))
	if _, err = a.db.Exec("DELETE FROM images WHERE id=?", p.ID); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "quarantine", "*", p.ID))
	if err != nil || len(files) != 1 {
		t.Fatal("missing quarantine", files, err)
	}
	preserved, err := os.ReadFile(files[0])
	if err != nil || !bytes.Equal(preserved, raw) {
		t.Fatal("original was not preserved")
	}
	request(t, a, "GET", "/i/"+p.ID, "", 404)
	request(t, a, "GET", "/"+filepath.ToSlash(mustRel(t, dir, files[0])), "", 404)
	a.Close()
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	again, _ := filepath.Glob(filepath.Join(dir, "quarantine", "*", p.ID))
	if len(again) != 1 {
		t.Fatal("restart duplicated or removed quarantined image")
	}
}
func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestRecoveryNeverOverwritesConflictingOriginal(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	original := pngBytes(t)
	p := decode[Picture](t, sendUpload(t, a, original, "", 201))
	a.Close()
	trash := filepath.Join(dir, "uploads", p.ID+".trash")
	backup := []byte("conflicting backup kept for recovery")
	if err = os.WriteFile(trash, backup, 0600); err != nil {
		t.Fatal(err)
	}
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	raw := request(t, a, "GET", "/i/"+p.ID, "", 200).Body.Bytes()
	if !bytes.Equal(raw, original) {
		t.Fatal("original overwritten")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "quarantine", "*", p.ID+".trash"))
	if len(files) != 1 {
		t.Fatal("conflict was lost")
	}
	raw, err = os.ReadFile(files[0])
	if err != nil || !bytes.Equal(raw, backup) {
		t.Fatal("conflict bytes changed")
	}
}
func TestRecoveryPreservesTempAndUnknownTrash(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	for _, name := range []string{".upload-interrupted", "unknown.png.trash", "manual-original.png"} {
		if err = os.WriteFile(filepath.Join(dir, "uploads", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err = New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, name := range []string{".upload-interrupted", "unknown.png.trash", "manual-original.png"} {
		files, _ := filepath.Glob(filepath.Join(dir, "quarantine", "*", name))
		if len(files) != 1 {
			t.Fatal("missing", name)
		}
		raw, _ := os.ReadFile(files[0])
		if string(raw) != name {
			t.Fatal("content changed")
		}
	}
}
func TestFailedQuarantineKeepsSource(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	source := filepath.Join(dir, "uploads", "keep.png")
	if err = os.WriteFile(source, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "quarantine"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if a, err = New(dir, "", ""); err == nil {
		a.Close()
		t.Fatal("invalid quarantine should fail safely")
	}
	raw, err := os.ReadFile(source)
	if err != nil || string(raw) != "keep" {
		t.Fatal("source lost on quarantine failure")
	}
}
