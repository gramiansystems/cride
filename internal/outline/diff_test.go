package outline

import (
	"testing"

	"cride/internal/diff"
	"cride/internal/lsp"
	"cride/internal/source"
)

func TestDiffOutlinesClassifiesChanges(t *testing.T) {
	t.Parallel()
	beforeContent := []byte("func Same() {\n old()\n}\nfunc OldName() {\n shared()\n shared2()\n}\nfunc Gone() {}\n")
	afterContent := []byte("func Same() {\n new()\n}\nfunc NewName() {\n shared()\n shared2()\n}\nfunc Added() {}\n")
	before := []lsp.DocumentSymbol{
		testSymbol("a.go", "Same", lsp.SymbolFunction, 1, 3),
		testSymbol("a.go", "OldName", lsp.SymbolFunction, 4, 7),
		testSymbol("a.go", "Gone", lsp.SymbolFunction, 8, 8),
	}
	after := []lsp.DocumentSymbol{
		testSymbol("b.go", "Same", lsp.SymbolFunction, 1, 3),
		testSymbol("b.go", "NewName", lsp.SymbolFunction, 4, 7),
		testSymbol("b.go", "Added", lsp.SymbolFunction, 8, 8),
	}
	files := []diff.FileDiff{{
		OldPath: "a.go", NewPath: "b.go", Status: diff.FileRenamed,
		Hunks: []diff.Hunk{{Lines: []diff.Line{
			{Kind: diff.LineDelete, OldLine: 2}, {Kind: diff.LineAdd, NewLine: 2},
			{Kind: diff.LineDelete, OldLine: 8}, {Kind: diff.LineAdd, NewLine: 8},
		}}},
	}}
	changes := DiffOutlines(before, after, beforeContent, afterContent, "a.go", "b.go", diff.NewReviewIndex(files))
	got := map[string]ChangeType{}
	for _, change := range changes {
		got[changeName(change)] = change.Type
		if change.Path != "b.go" {
			t.Fatalf("change path = %q, want b.go", change.Path)
		}
	}
	if got["Same"] != SymbolModified || got["NewName"] != SymbolRenamed || got["Gone"] != SymbolRemoved || got["Added"] != SymbolAdded {
		t.Fatalf("change types = %#v", got)
	}
}

func TestDiffOutlinesRejectsWeakRename(t *testing.T) {
	t.Parallel()
	before := []lsp.DocumentSymbol{testSymbol("a.go", "Old", lsp.SymbolFunction, 1, 2)}
	after := []lsp.DocumentSymbol{testSymbol("a.go", "New", lsp.SymbolFunction, 1, 2)}
	changes := DiffOutlines(before, after, []byte("func Old() {}\nold()\n"), []byte("func New() {}\nnew()\n"), "a.go", "a.go", diff.NewReviewIndex(nil))
	types := map[ChangeType]int{}
	for _, change := range changes {
		types[change.Type]++
	}
	if len(changes) != 2 || types[SymbolRemoved] != 1 || types[SymbolAdded] != 1 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffOutlinesAcceptsRenameAtThreshold(t *testing.T) {
	t.Parallel()
	before := []lsp.DocumentSymbol{testSymbol("a.go", "Old", lsp.SymbolFunction, 1, 5)}
	after := []lsp.DocumentSymbol{testSymbol("a.go", "New", lsp.SymbolFunction, 1, 5)}
	beforeContent := []byte("func Old() {\nsharedOne()\nsharedTwo()\nsharedThree()\noldOnly()\n")
	afterContent := []byte("func New() {\nsharedOne()\nsharedTwo()\nsharedThree()\nnewOnly()\n")
	changes := DiffOutlines(before, after, beforeContent, afterContent, "a.go", "a.go", diff.NewReviewIndex(nil))
	if len(changes) != 1 || changes[0].Type != SymbolRenamed || changes[0].BodySimilarity != 0.6 {
		t.Fatalf("threshold changes = %#v", changes)
	}
}

func testSymbol(path, name string, kind lsp.SymbolKind, start, end int) lsp.DocumentSymbol {
	return lsp.DocumentSymbol{
		Name: name, Kind: kind,
		Range:          source.Range{Start: source.Location{Path: path, Line: start, Column: 1}, End: source.Location{Path: path, Line: end, Column: 1}},
		SelectionRange: source.Range{Start: source.Location{Path: path, Line: start, Column: 1}},
	}
}
