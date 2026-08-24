package lsp

import (
	"testing"

	"cride/internal/source"
)

func TestFlattenDocumentSymbolsKeepsEnclosingClassForFunctions(t *testing.T) {
	t.Parallel()

	symbols := []DocumentSymbol{{
		Name: "Outer",
		Kind: SymbolClass,
		Children: []DocumentSymbol{{
			Name: "Inner",
			Kind: SymbolClass,
			Children: []DocumentSymbol{{
				Name:           "run",
				Kind:           SymbolFunction,
				SelectionRange: source.Range{Start: source.Location{Line: 3, Column: 5}},
			}},
		}},
	}}

	flat := FlattenDocumentSymbols(symbols)
	if len(flat) != 3 {
		t.Fatalf("flattened symbols = %d, want 3", len(flat))
	}
	if got := flat[2].ContainerName; got != "Inner" {
		t.Fatalf("function container = %q, want Inner", got)
	}
	if got := DocumentSymbolLabel(flat[2]); got != "[function] run · Inner  3:5" {
		t.Fatalf("function label = %q", got)
	}
}
