package app

import (
	"strings"
	"testing"

	"cride/internal/diff"
	"cride/internal/ui"
)

func searchTestFile(path string) diff.FileDiff {
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,5 +1,4 @@",
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "alpha one", OldLine: 1, NewLine: 1},
				{Kind: diff.LineAdd, Content: "beta two", NewLine: 2},
				{Kind: diff.LineContext, Content: "Alpha three", OldLine: 2, NewLine: 3},
				{Kind: diff.LineDelete, Content: "gamma alpha", OldLine: 3},
				{Kind: diff.LineContext, Content: "delta", OldLine: 4, NewLine: 4},
			},
		}},
	}
}

func typeString(m Model, s string) Model {
	for _, r := range s {
		m = press(m, string(r))
	}
	return m
}

func TestInFileSearchIncrementalNarrowingAndWidening(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{searchTestFile("a.go")}, width: 90, height: 20}
	m = press(m, "/")
	if !m.search.typing || !m.search.active {
		t.Fatalf("/ did not open search prompt: %+v", m.search)
	}

	m = typeString(m, "alpha")
	if len(m.search.matches) != 3 {
		t.Fatalf("alpha matches = %d, want 3 (smart-case folds)", len(m.search.matches))
	}
	if m.cursor != 1 {
		t.Fatalf("cursor after first match = %d, want row 1", m.cursor)
	}

	// Narrow: "alpha o" only matches row 1.
	m = typeString(m, " o")
	if len(m.search.matches) != 1 || m.search.matches[0].rowIdx != 1 {
		t.Fatalf("narrowed matches = %+v, want single row-1 match", m.search.matches)
	}

	// Backspace widens back to 3.
	m = press(m, "backspace")
	m = press(m, "backspace")
	if len(m.search.matches) != 3 {
		t.Fatalf("widened matches = %d, want 3", len(m.search.matches))
	}
}

func TestInFileSearchSmartCase(t *testing.T) {
	t.Parallel()

	rows := ui.FlattenFile([]diff.FileDiff{searchTestFile("a.go")}, 0)

	folded := computeMatches(rows, "alpha")
	if len(folded) != 3 {
		t.Fatalf("lowercase query matches = %d, want 3", len(folded))
	}

	exact := computeMatches(rows, "Alpha")
	if len(exact) != 1 || exact[0].rowIdx != 3 {
		t.Fatalf("uppercase query matches = %+v, want only the Alpha row", exact)
	}

	// Deleted-side content is searchable in diff view.
	deleted := computeMatches(rows, "gamma")
	if len(deleted) != 1 || deleted[0].rowIdx != 4 {
		t.Fatalf("deleted-side match = %+v, want row 4", deleted)
	}
}

func TestInFileSearchMatchColumnsAreDisplayColumns(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines:  []diff.Line{{Kind: diff.LineAdd, Content: "\tx := alpha", NewLine: 1}},
		}},
	}}
	rows := ui.FlattenFile(files, 0)
	matches := computeMatches(rows, "alpha")
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	// Tab expands to 4 spaces: "    x := alpha" places alpha at display col 9.
	if matches[0].startCol != 9 || matches[0].endCol != 14 {
		t.Fatalf("match span = [%d,%d), want [9,14)", matches[0].startCol, matches[0].endCol)
	}
}

func TestInFileSearchNextPrevWrapAndEscape(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{searchTestFile("a.go")}, width: 90, height: 20}
	m = press(m, "/")
	m = typeString(m, "alpha")
	m = press(m, "enter")
	if m.search.typing || !m.search.active {
		t.Fatalf("enter did not close prompt: %+v", m.search)
	}

	m = press(m, "n")
	if m.cursor != 3 {
		t.Fatalf("n moved to row %d, want 3", m.cursor)
	}
	m = press(m, "n")
	if m.cursor != 4 {
		t.Fatalf("second n moved to row %d, want 4", m.cursor)
	}
	m = press(m, "n") // wraps
	if m.cursor != 1 {
		t.Fatalf("wrap n moved to row %d, want 1", m.cursor)
	}
	if !strings.Contains(m.status.text, "wrapped") {
		t.Fatalf("wrap toast = %q, want wrap notice", m.status.text)
	}

	m = press(m, "N") // wraps backward
	if m.cursor != 4 {
		t.Fatalf("wrap N moved to row %d, want 4", m.cursor)
	}

	m = press(m, "esc")
	if m.search.active || len(m.search.matches) != 0 {
		t.Fatalf("esc did not clear search: %+v", m.search)
	}
	// n is hunk navigation again: from row 4 there is no next hunk header,
	// so the cursor stays put rather than wrapping to a match.
	m = press(m, "n")
	if m.cursor != 4 {
		t.Fatalf("n after esc moved to row %d, want hunk-nav no-op at 4", m.cursor)
	}
}

func TestInFileSearchPerFileMemoSurvivesFileSwitch(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{searchTestFile("a.go"), searchTestFile("b.go")}, width: 90, height: 20}
	m = press(m, "/")
	m = typeString(m, "alpha")
	m = press(m, "enter")

	m = press(press(m, "]"), "]") // to b.go: no memo there
	if m.search.active {
		t.Fatalf("search unexpectedly active on b.go: %+v", m.search)
	}

	m = press(press(m, "["), "[") // back to a.go: memo restores query and highlights
	if !m.search.active || m.search.query != "alpha" {
		t.Fatalf("search not restored on a.go: %+v", m.search)
	}
	if len(m.search.matches) != 3 {
		t.Fatalf("restored matches = %d, want 3", len(m.search.matches))
	}

	// Restoring must not move the cursor.
	if m.cursor != 1 {
		t.Fatalf("restore moved cursor to %d, want saved row 1", m.cursor)
	}
}

func TestInFileSearchSurvivesViewModeToggle(t *testing.T) {
	t.Parallel()

	m := Model{
		files:        []diff.FileDiff{searchTestFile("a.go")},
		width:        90,
		height:       20,
		fileContents: map[string]fileContentState{"a.go": {lines: []string{"alpha one", "beta two", "Alpha three", "delta"}, loaded: true}},
	}
	m = press(m, "/")
	m = typeString(m, "alpha")
	m = press(m, "enter")

	m = press(m, "tab")
	if m.viewMode != ViewFile {
		t.Fatalf("tab did not switch to full-file view")
	}
	if !m.search.active || m.search.query != "alpha" {
		t.Fatalf("search lost on view toggle: %+v", m.search)
	}
	if len(m.search.matches) == 0 {
		t.Fatal("no matches recomputed against full-file rows")
	}
}
