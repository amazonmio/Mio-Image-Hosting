package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImageNameUnicodeValidation(t *testing.T) {
	for r := rune(0); r <= 0x9f; r++ {
		if r >= 0x20 && r < 0x7f {
			continue
		}
		if _, err := parseImageName("a" + string(r) + "b.png"); err == nil {
			t.Fatalf("accepted control U+%04X", r)
		}
	}
	if _, err := parseImageName("a\ufffdb.png"); err == nil {
		t.Fatal("accepted replacement character")
	}
	for _, raw := range []string{`"a\ud800b.png"`, `"a\udfffb.png"`} {
		var name string
		if err := json.Unmarshal([]byte(raw), &name); err != nil {
			t.Fatal(err)
		}
		if _, err := parseImageName(name); err == nil {
			t.Fatal("accepted invalid surrogate from JSON")
		}
	}
	for _, name := range []string{" 封面😀.png ", "a\u007e\u00a0\ufffc\U00010000b.png", strings.Repeat("😀", 180)} {
		if _, err := parseImageName(name); err != nil {
			t.Fatalf("valid name rejected: %v", err)
		}
	}
	if _, err := parseImageName(strings.Repeat("😀", 181)); err == nil {
		t.Fatal("accepted too many code points")
	}
}
