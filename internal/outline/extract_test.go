package outline

import (
	"strings"
	"testing"

	"cride/internal/diffsource"
	"cride/internal/lsp"
)

func TestLexicalExtractorLanguagesAndRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		content string
		want    []string
	}{
		{"main.go", "package p\n\ntype Server struct {}\n\nfunc (s *Server) Login() {\n}\n", []string{"struct:Server:3-4", "method:Login:5-6"}},
		{"main.py", "class Server:\n    def login(self):\n        return True\n\ndef helper():\n    pass\n", []string{"class:Server:1-4", "method:login:2-4", "function:helper:5-6"}},
		{"main.ts", "class Server {\n  login() {\n    return true\n  }\n}\nfunction helper() {}\n", []string{"class:Server:1-5", "method:login:2-5", "function:helper:6-6"}},
		{"main.rs", "struct Server {}\nimpl Server {\n    fn login(&self) {}\n}\nfn helper() {}\n", []string{"struct:Server:1-1", "class:Server:2-4", "method:login:3-4", "function:helper:5-5"}},
	}
	extractor := LexicalExtractor{}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			symbols, err := extractor.Symbols(tt.path, []byte(tt.content))
			if err != nil {
				t.Fatal(err)
			}
			flat := lsp.FlattenDocumentSymbols(symbols)
			got := make([]string, 0, len(flat))
			for _, symbol := range flat {
				got = append(got, symbol.Kind.String()+":"+symbol.Name+":"+itoa(symbol.Range.Start.Line)+"-"+itoa(symbol.Range.End.Line))
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("symbols = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLexicalExtractorToleratesBrokenAndSkipsUnsafeContent(t *testing.T) {
	t.Parallel()
	extractor := LexicalExtractor{}
	symbols, err := extractor.Symbols("broken.py", []byte("class Open:\n    def useful(\n"))
	if err != nil || len(symbols) != 1 || symbols[0].Name != "Open" {
		t.Fatalf("broken symbols = %#v, err %v", symbols, err)
	}
	oversized := make([]byte, diffsource.MaxContentBytes+1)
	if symbols, err := extractor.Symbols("large.go", oversized); err != nil || len(symbols) != 0 {
		t.Fatalf("oversized symbols = %#v, err %v", symbols, err)
	}
	if symbols, err := extractor.Symbols("binary.go", []byte("func Before() {}\x00func After() {}")); err != nil || len(symbols) != 0 {
		t.Fatalf("binary symbols = %#v, err %v", symbols, err)
	}
}

func TestLexicalExtractorCppCStyleVTable(t *testing.T) {
	t.Parallel()

	content := `typedef struct WidgetDispatch {
    void (*destroy)(Widget *self);
    int (*draw)(Widget *self, int x);
} WidgetDispatch;

struct Widget {
    const WidgetDispatch *vtable;
};

static int widget_draw(Widget *self, int x) { return x; }

static const WidgetDispatch widget_vtable = {
    .destroy = widget_destroy,
    .draw = widget_draw,
};
`
	symbols, err := (LexicalExtractor{}).Symbols("widget.cpp", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	flat := lsp.FlattenDocumentSymbols(symbols)
	want := map[string]string{
		"WidgetDispatch": "struct",
		"destroy":        "method:vtable slot",
		"draw":           "method:vtable slot",
		"Widget":         "struct",
		"widget_draw":    "function",
		"widget_vtable":  "object:WidgetDispatch vtable",
	}
	for name, kindDetail := range want {
		found := false
		for _, symbol := range flat {
			got := symbol.Kind.String()
			if symbol.Detail != "" {
				got += ":" + symbol.Detail
			}
			if symbol.Name == name && got == kindDetail {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s (%s) in %#v", name, kindDetail, flat)
		}
	}

	bindings := map[string]string{"destroy": "→ widget_destroy", "draw": "→ widget_draw"}
	for name, detail := range bindings {
		found := false
		for _, symbol := range flat {
			if symbol.Name == name && symbol.Kind == lsp.SymbolMethod && symbol.Detail == detail {
				found = true
			}
		}
		if !found {
			t.Errorf("missing vtable binding %s %s", name, detail)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for ; n > 0; n /= 10 {
		digits = append(digits, byte('0'+n%10))
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
