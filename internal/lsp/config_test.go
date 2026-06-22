package lsp

import "testing"

func TestParseConfigLanguageCommands(t *testing.T) {
	t.Parallel()

	cfg, err := ParseConfig([]byte(`
[languages.go]
extensions = [".go"]
command = ["gopls"]

[languages.rust]
extensions = [".rs"]
command = ["rust-analyzer"]
`))
	if err != nil {
		t.Fatal(err)
	}

	goLang, ok := cfg.LanguageForPath("internal/app/app.go")
	if !ok {
		t.Fatal("go language not matched")
	}
	if goLang.Name != "go" || len(goLang.Command) != 1 || goLang.Command[0] != "gopls" {
		t.Fatalf("go language = %+v, want gopls command", goLang)
	}

	rustLang, ok := cfg.LanguageForPath("src/lib.rs")
	if !ok {
		t.Fatal("rust language not matched")
	}
	if rustLang.Name != "rust" || len(rustLang.Command) != 1 || rustLang.Command[0] != "rust-analyzer" {
		t.Fatalf("rust language = %+v, want rust-analyzer command", rustLang)
	}
}

func TestDefaultConfigIncludesClangdForCAndCPP(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	for _, tt := range []struct {
		path string
		name string
	}{
		{"src/widget.cpp", "cpp"},
		{"include/widget.hpp", "cpp"},
		{"include/widget.h", "cpp"},
		{"src/widget.c", "cpp"},
	} {
		lang, ok := cfg.LanguageForPath(tt.path)
		if !ok {
			t.Fatalf("%s did not match a default language", tt.path)
		}
		if lang.Name != tt.name || len(lang.Command) != 1 || lang.Command[0] != "clangd" {
			t.Fatalf("language for %s = %+v, want %s via clangd", tt.path, lang, tt.name)
		}
	}
}

func TestClangdUsesCProtocolLanguageForDotC(t *testing.T) {
	t.Parallel()

	lang, ok := DefaultConfig().LanguageForPath("src/widget.c")
	if !ok || lang.languageID("src/widget.c") != "c" || lang.languageID("src/widget.C") != "cpp" || lang.languageID("include/widget.h") != "cpp" {
		t.Fatalf("clangd language ids for C/C++ = %+v", lang)
	}
}
