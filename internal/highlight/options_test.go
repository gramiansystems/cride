package highlight

import (
	"strings"
	"testing"
)

func TestFormatterSelection(t *testing.T) {
	t.Parallel()

	line := `func main() {}`

	trueColor := NewWithOptions(Options{Dark: true, TrueColor: true}).Line("main.go", line)
	if !strings.Contains(trueColor, "38;2;") {
		t.Fatalf("truecolor output missing 24-bit sequences: %q", trueColor)
	}

	quantized := New().Line("main.go", line)
	if strings.Contains(quantized, "38;2;") {
		t.Fatalf("terminal256 output contains 24-bit sequences: %q", quantized)
	}
	if !strings.Contains(quantized, "38;5;") {
		t.Fatalf("terminal256 output missing 256-color sequences: %q", quantized)
	}

	disabled := NewWithOptions(Options{Disabled: true}).Line("main.go", line)
	if disabled != line {
		t.Fatalf("disabled highlighter altered the line: %q", disabled)
	}
}

func TestStyleSelectionPerScheme(t *testing.T) {
	t.Parallel()

	if got := NewWithOptions(Options{Dark: true}).StyleName(); got != "monokai" {
		t.Fatalf("dark default style = %q, want monokai", got)
	}
	if got := NewWithOptions(Options{Dark: false}).StyleName(); got != "github" {
		t.Fatalf("light default style = %q, want github", got)
	}
	if got := NewWithOptions(Options{Style: "dracula"}).StyleName(); got != "dracula" {
		t.Fatalf("override style = %q, want dracula", got)
	}
	// Unknown style falls back rather than crashing.
	if got := NewWithOptions(Options{Style: "no-such-style"}).StyleName(); got == "" {
		t.Fatal("unknown style produced empty style")
	}
}
