package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"testing"
)

func animationBytes(t *testing.T) []byte {
	t.Helper()
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	frame.Pix[0] = 1
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{2, 3}}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestCompleteImageValidation(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile("testdata/gopher-doc.lossless.webp")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{"PNG": pngBytes(t), "JPEG": jpg.Bytes(), "GIF": animationBytes(t), "WebP": webp}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			a := testApp(t, "")
			p := decode[Picture](t, sendUpload(t, a, raw, "", 201))
			response := request(t, a, "GET", "/i/"+p.ID, "", 200)
			if !bytes.Equal(raw, response.Body.Bytes()) {
				t.Fatal("validation changed original bytes")
			}
			truncated := raw[:len(raw)-2]
			if name == "PNG" {
				truncated = raw[:33]
			}
			if name == "WebP" {
				truncated = raw[:len(raw)-10]
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(truncated)); err != nil {
				t.Fatalf("fixture must have a valid image header: %v", err)
			}
			sendUpload(t, a, truncated, "", 400)
			var count int
			if err := a.db.QueryRow("SELECT COUNT(*) FROM images").Scan(&count); err != nil || count != 1 {
				t.Fatal("invalid image persisted")
			}
			entries, err := os.ReadDir(a.uploads)
			if err != nil || len(entries) != 1 {
				t.Fatal("invalid upload left files")
			}
		})
	}
}
func TestRejectsCorruptPNGChecksum(t *testing.T) {
	a := testApp(t, "")
	raw := pngBytes(t)
	offset := bytes.Index(raw, []byte("IDAT"))
	if offset < 0 {
		t.Fatal("missing IDAT")
	}
	raw[offset+4] ^= 0xff
	sendUpload(t, a, raw, "", 400)
}
func TestGIFValidFirstFrameDoesNotHideTruncatedAnimation(t *testing.T) {
	raw := animationBytes(t)
	raw = raw[:len(raw)-2]
	if _, err := gif.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("first frame should decode: %v", err)
	}
	sendUpload(t, testApp(t, ""), raw, "", 400)
}
func declaredGIF(frames, width, height int) []byte {
	raw := []byte{'G', 'I', 'F', '8', '9', 'a', 1, 0, 1, 0, 0, 0, 0}
	for i := 0; i < frames; i++ {
		desc := []byte{0x2c, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(desc[5:], uint16(width))
		binary.LittleEndian.PutUint16(desc[7:], uint16(height))
		raw = append(raw, desc...)
		raw = append(raw, 2, 1, 0, 0)
	}
	return append(raw, 0x3b)
}
func TestDecodeBudgets(t *testing.T) {
	if err := checkGIFBudget(declaredGIF(2, 5000, 4000)); err == nil {
		t.Fatal("GIF aggregate pixel limit not enforced")
	}
	if err := checkGIFBudget(declaredGIF(maxGIFFrames+1, 1, 1)); err == nil {
		t.Fatal("GIF frame limit not enforced")
	}
	raw := pngBytes(t)[:33]
	binary.BigEndian.PutUint32(raw[16:], 10000)
	binary.BigEndian.PutUint32(raw[20:], 10000)
	binary.BigEndian.PutUint32(raw[29:], crc32.ChecksumIEEE(raw[12:29]))
	sendUpload(t, testApp(t, ""), raw, "", 400)
	a := testApp(t, "")
	a.decodeSlots <- struct{}{}
	sendUpload(t, a, pngBytes(t), "", 503)
	<-a.decodeSlots
	sendUpload(t, a, pngBytes(t), "", 201)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.validateImage(ctx, bytes.NewReader(pngBytes(t))); err == nil {
		t.Fatal("cancelled validation accepted")
	}
}
