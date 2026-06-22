package outline

import (
	"testing"

	"cride/internal/lsp"
)

func TestEnclosingPathIsInnerFirstAndInclusive(t *testing.T) {
	t.Parallel()
	method := testSymbol("a.py", "login", lsp.SymbolMethod, 2, 4)
	class := testSymbol("a.py", "Server", lsp.SymbolClass, 1, 5)
	class.Children = []lsp.DocumentSymbol{method}
	for _, line := range []int{2, 4} {
		got := EnclosingPath([]lsp.DocumentSymbol{class}, line)
		if len(got) != 2 || got[0].Name != "login" || got[1].Name != "Server" {
			t.Fatalf("line %d path = %#v", line, got)
		}
	}
	if got := EnclosingPath([]lsp.DocumentSymbol{class}, 6); len(got) != 0 {
		t.Fatalf("outside path = %#v", got)
	}
	if got := EnclosingPath([]lsp.DocumentSymbol{class}, 1); len(got) != 1 || got[0].Name != "Server" {
		t.Fatalf("class boundary path = %#v", got)
	}
}
