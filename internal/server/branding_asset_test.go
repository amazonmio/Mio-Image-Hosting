package server

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBrandingRejectsLinkedSystemFiles(t *testing.T) {
	a := testApp(t, "")
	for _, protected := range []string{"app.db", "config.yaml", "setup-key.txt", "app.db-journal"} {
		t.Run(protected, func(t *testing.T) {
			target := filepath.Join(a.dir, protected)
			if protected == "setup-key.txt" || protected == "app.db-journal" {
				if err := os.WriteFile(target, pngBytes(t), 0600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(a.dir, protected+".png")
			if err := os.Link(target, alias); err != nil {
				t.Fatal(err)
			}
			if _, err := a.readBrandAsset(filepath.Base(alias), "avatar"); err == nil {
				t.Fatal("accepted hard link to", protected)
			}
		})
	}
}
func TestBrandingRejectsSymlinks(t *testing.T) {
	a := testApp(t, "")
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, pngBytes(t), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(a.dir, "linked.png")); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err := a.readBrandAsset("linked.png", "avatar"); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestBrandingRejectsInvalidContent(t *testing.T) {
	a := testApp(t, "")
	for name, raw := range map[string][]byte{"plain.webp": []byte("private text is not an image"), "truncated.png": pngBytes(t)[:33], "fake.ico": []byte{0, 0, 1, 0, 1, 0}} {
		if err := os.WriteFile(filepath.Join(a.dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := a.readBrandAsset(name, "avatar"); err == nil {
			t.Fatal("accepted", name)
		}
	}
}
func TestBrandingSnapshotIgnoresLaterFileReplacement(t *testing.T) {
	a := testApp(t, "")
	initial := request(t, a, "GET", "/branding/avatar", "", 200)
	if err := os.WriteFile(filepath.Join(a.dir, defaultAvatarFile), []byte("SYNTHETIC-PRIVATE-DATA"), 0600); err != nil {
		t.Fatal(err)
	}
	after := request(t, a, "GET", "/branding/avatar", "", 200)
	if !bytes.Equal(initial.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("served unchecked replacement")
	}
	if after.Header().Get("ETag") == "" {
		t.Fatal("missing snapshot ETag")
	}
	if _, err := a.readBrandAsset(defaultAvatarFile, "avatar"); err == nil {
		t.Fatal("replacement should fail the next validation")
	}
}
func iconContainer(frame []byte) []byte {
	raw := make([]byte, 22)
	binary.LittleEndian.PutUint16(raw[2:], 1)
	binary.LittleEndian.PutUint16(raw[4:], 1)
	raw[6] = 1
	raw[7] = 1
	binary.LittleEndian.PutUint16(raw[10:], 1)
	binary.LittleEndian.PutUint16(raw[12:], 32)
	binary.LittleEndian.PutUint32(raw[14:], uint32(len(frame)))
	binary.LittleEndian.PutUint32(raw[18:], 22)
	return append(raw, frame...)
}
func TestValidPNGAndDIBIcons(t *testing.T) {
	var b bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	dib := make([]byte, 48)
	binary.LittleEndian.PutUint32(dib, 40)
	binary.LittleEndian.PutUint32(dib[4:], 1)
	binary.LittleEndian.PutUint32(dib[8:], 2)
	binary.LittleEndian.PutUint16(dib[12:], 1)
	binary.LittleEndian.PutUint16(dib[14:], 32)
	dib[42] = 255
	dib[43] = 255
	for _, frame := range [][]byte{b.Bytes(), dib} {
		raw := iconContainer(frame)
		if err := validateICO(raw); err != nil {
			t.Fatal("valid icon rejected", err)
		}
		a := testApp(t, "")
		if err := os.WriteFile(filepath.Join(a.dir, "valid.ico"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		asset, err := a.readBrandAsset("valid.ico", "favicon")
		if err != nil || asset.contentType != "image/x-icon" {
			t.Fatal(err, asset.contentType)
		}
		if err = validateICO(raw[:len(raw)-1]); err == nil {
			t.Fatal("truncated icon accepted")
		}
	}
}
func TestICORejectsInvalidDirectoryAndDimensions(t *testing.T) {
	raw := iconContainer(make([]byte, 40))
	binary.LittleEndian.PutUint32(raw[18:], 0xffffffff)
	if err := validateICO(raw); err == nil {
		t.Fatal("out-of-range icon accepted")
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := validateICO(iconContainer(b.Bytes())); err == nil {
		t.Fatal("mismatched icon dimensions accepted")
	}
}
