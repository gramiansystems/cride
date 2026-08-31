package outline

import (
	"strconv"
	"strings"
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
	changes := DiffOutlines(before, after, beforeContent, afterContent, "a.go", "b.go", files)
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
	for _, change := range changes {
		switch changeName(change) {
		case "Same":
			if !change.ContainsAddition || !change.ContainsDeletion {
				t.Fatalf("Same range markers = %+v, want addition and deletion", change)
			}
		case "Gone":
			if !change.ContainsDeletion || change.ContainsAddition {
				t.Fatalf("Gone range markers = %+v, want deletion only", change)
			}
		case "Added":
			if !change.ContainsAddition || change.ContainsDeletion {
				t.Fatalf("Added range markers = %+v, want addition only", change)
			}
		}
	}
}

func TestDiffOutlinesRejectsWeakRename(t *testing.T) {
	t.Parallel()
	before := []lsp.DocumentSymbol{testSymbol("a.go", "Old", lsp.SymbolFunction, 1, 2)}
	after := []lsp.DocumentSymbol{testSymbol("a.go", "New", lsp.SymbolFunction, 1, 2)}
	changes := DiffOutlines(before, after, []byte("func Old() {}\nold()\n"), []byte("func New() {}\nnew()\n"), "a.go", "a.go", nil)
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
	changes := DiffOutlines(before, after, beforeContent, afterContent, "a.go", "a.go", nil)
	if len(changes) != 1 || changes[0].Type != SymbolRenamed || changes[0].BodySimilarity != 0.6 {
		t.Fatalf("threshold changes = %#v", changes)
	}
}

func TestDiffOutlinesSkipsFuzzyRenamesBeyondComparisonBudget(t *testing.T) {
	t.Parallel()

	// Use the smallest square set that exceeds the pair budget. All bodies are
	// identical, so an unbounded matcher would retain every pair as a rename
	// candidate.
	side := 1
	for side*side <= maxRenameComparisons {
		side++
	}
	before := make([]lsp.DocumentSymbol, 0, side)
	after := make([]lsp.DocumentSymbol, 0, side)
	for i := 0; i < side; i++ {
		before = append(before, testSymbol("a.go", "Old"+strconv.Itoa(i), lsp.SymbolFunction, 1, 1))
		after = append(after, testSymbol("a.go", "New"+strconv.Itoa(i), lsp.SymbolFunction, 1, 1))
	}

	changes := DiffOutlines(before, after, []byte("shared()\n"), []byte("shared()\n"), "a.go", "a.go", nil)
	types := map[ChangeType]int{}
	for _, change := range changes {
		types[change.Type]++
	}
	if len(changes) != side*2 || types[SymbolRemoved] != side || types[SymbolAdded] != side || types[SymbolRenamed] != 0 {
		t.Fatalf("budgeted changes: total=%d types=%v, want %d added and removed", len(changes), types, side)
	}
}

func TestDiffOutlinesSkipsFuzzyRenamesBeyondLineWorkBudget(t *testing.T) {
	t.Parallel()

	// Stay exactly within the pair-count budget while making the aggregate
	// symbol ranges too expensive to scan pairwise.
	side := 64
	bodyLines := maxRenameComparedLines/(2*side*side) + 1
	before := make([]lsp.DocumentSymbol, 0, side)
	after := make([]lsp.DocumentSymbol, 0, side)
	for i := 0; i < side; i++ {
		before = append(before, testSymbol("a.go", "Old"+strconv.Itoa(i), lsp.SymbolFunction, 1, bodyLines))
		after = append(after, testSymbol("a.go", "New"+strconv.Itoa(i), lsp.SymbolFunction, 1, bodyLines))
	}
	content := []byte(strings.Repeat("shared()\n", bodyLines))

	changes := DiffOutlines(before, after, content, content, "a.go", "a.go", nil)
	types := map[ChangeType]int{}
	for _, change := range changes {
		types[change.Type]++
	}
	if len(changes) != side*2 || types[SymbolRemoved] != side || types[SymbolAdded] != side || types[SymbolRenamed] != 0 {
		t.Fatalf("work-budgeted changes: total=%d types=%v, want %d added and removed", len(changes), types, side)
	}
}

func testSymbol(path, name string, kind lsp.SymbolKind, start, end int) lsp.DocumentSymbol {
	return lsp.DocumentSymbol{
		Name: name, Kind: kind,
		Range:          source.Range{Start: source.Location{Path: path, Line: start, Column: 1}, End: source.Location{Path: path, Line: end, Column: 1}},
		SelectionRange: source.Range{Start: source.Location{Path: path, Line: start, Column: 1}},
	}
}
