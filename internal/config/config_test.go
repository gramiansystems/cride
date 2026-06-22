package config

import (
	"strings"
	"testing"
)

func TestParseAndWantsDark(t *testing.T) {
	t.Parallel()

	cfg := Parse(strings.NewReader(`
# cride config
theme = Light
chroma_style = dracula
unknown_key = ignored
not a key value line
`))
	if cfg.Theme != "light" || cfg.ChromaStyle != "dracula" {
		t.Fatalf("parsed config = %+v", cfg)
	}
	if cfg.WantsDark(true) {
		t.Fatal("theme=light must force light regardless of detection")
	}

	if !Parse(strings.NewReader("theme = dark")).WantsDark(false) {
		t.Fatal("theme=dark must force dark")
	}
	for _, v := range []string{"auto", "", "sparkly"} {
		cfg := Parse(strings.NewReader("theme = " + v))
		if cfg.WantsDark(true) != true || cfg.WantsDark(false) != false {
			t.Fatalf("theme=%q must follow detection", v)
		}
	}
}
