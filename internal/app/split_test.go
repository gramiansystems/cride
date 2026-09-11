package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	navsearch "cride/internal/search"
	"cride/internal/ui"
)

func splitTestFile(path string) diff.FileDiff {
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{
			{
				Header:   "@@ -1,4 +1,4 @@",
				NewStart: 1, NewLines: 4,
				Lines: []diff.Line{
					{Kind: diff.LineContext, Content: "one", OldLine: 1, NewLine: 1},
					{Kind: diff.LineDelete, Content: "old two", OldLine: 2},
					{Kind: diff.LineAdd, Content: "new two", NewLine: 2},
					{Kind: diff.LineContext, Content: "three", OldLine: 3, NewLine: 3},
				},
			},
			{
				Header:   "@@ -10,2 +10,2 @@",
				NewStart: 10, NewLines: 2,
				Lines: []diff.Line{
					{Kind: diff.LineContext, Content: "ten", OldLine: 10, NewLine: 10},
					{Kind: diff.LineAdd, Content: "eleven", NewLine: 11},
				},
			},
		},
	}
}

func pressZS(m Model) Model {
	m = press(m, "z")
	m = press(m, "s")
	return m
}

func TestSplitToggleRoundTripPreservesCursorLine(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24}
	// Put the cursor on the added line (row 3 unified: header, ctx, del, add).
	m.cursor = 3
	before := cursorSourceLine(m)
	if before != 2 {
		t.Fatalf("setup: cursor source line = %d, want 2", before)
	}

	m = pressZS(m)
	if !m.splitViewActive() {
		t.Fatal("zs did not enable side-by-side")
	}
	rows := m.currentRows()
	if rows[m.cursor].Kind != ui.RowPair {
		t.Fatalf("cursor row kind = %v, want pair", rows[m.cursor].Kind)
	}
	if got := cursorSourceLine(m); got != before {
		t.Fatalf("cursor source line after split = %d, want %d", got, before)
	}

	m = pressZS(m)
	if m.splitViewActive() {
		t.Fatal("second zs did not return to unified")
	}
	if got := cursorSourceLine(m); got != before {
		t.Fatalf("cursor source line after round trip = %d, want %d", got, before)
	}
}

func TestSplitHunkNavigationParity(t *testing.T) {
	t.Parallel()

	unified := Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24}
	split := pressZS(Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24})

	unified = press(unified, "n")
	split = press(split, "n")
	uRows, sRows := unified.currentRows(), split.currentRows()
	if uRows[unified.cursor].Kind != ui.RowHunkHeader || sRows[split.cursor].Kind != ui.RowHunkHeader {
		t.Fatalf("n did not land on hunk headers: %v / %v", uRows[unified.cursor].Kind, sRows[split.cursor].Kind)
	}
	if uRows[unified.cursor].HunkIdx != sRows[split.cursor].HunkIdx {
		t.Fatalf("hunk jump parity broken: unified hunk %d, split hunk %d", uRows[unified.cursor].HunkIdx, sRows[split.cursor].HunkIdx)
	}
}

func TestSplitWidthFallback(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 60, height: 24}
	m = pressZS(m)
	if m.splitViewActive() {
		t.Fatal("split enabled despite narrow window")
	}
	if !strings.Contains(m.status.text, "too narrow") {
		t.Fatalf("no width-fallback toast: %q", m.status.text)
	}

	// Wide window: enabled; shrinking falls back to unified but keeps the
	// preference for when the window grows again.
	m = Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24}
	m = pressZS(m)
	if !m.splitViewActive() {
		t.Fatal("split not enabled at width 140")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = next.(Model)
	if m.splitViewActive() {
		t.Fatal("narrow resize did not fall back to unified rows")
	}
	if !m.splitFiles["a.go"] {
		t.Fatal("narrow resize dropped the split preference")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m = next.(Model)
	if !m.splitViewActive() {
		t.Fatal("regrowing the window did not restore split view")
	}
}

func TestSplitActiveSideSelectsSymbolSide(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24}
	m = pressZS(m)
	// Move to the changed pair (old two | new two).
	rows := m.currentRows()
	for i, r := range rows {
		if r.Kind == ui.RowPair && r.Left != nil && r.Right != nil && r.Left != r.Right {
			m.cursor = i
			break
		}
	}

	queries, ok := m.currentSymbolQueries()
	if !ok || len(queries) == 0 {
		t.Fatal("no symbol queries on pair row")
	}
	if queries[0].Symbol != "new" && !strings.Contains(strings.Join(symbolNames(queries), ","), "new") {
		t.Fatalf("right-side symbols = %v, want new-side content", symbolNames(queries))
	}

	// h at column 0 crosses to the left cell (line end); 0 walks to its start.
	m = press(m, "h")
	m = press(m, "0")
	if !m.splitActiveLeft {
		t.Fatal("h at column 0 did not cross to the left cell")
	}
	queries, ok = m.currentSymbolQueries()
	if !ok || len(queries) == 0 {
		t.Fatal("no symbol queries on left side")
	}
	if queries[0].Side != navsearch.ResultSideBaseline {
		t.Fatalf("left-side query side = %v, want baseline", queries[0].Side)
	}
	if queries[0].Symbol != "old" {
		t.Fatalf("left-side symbol = %q, want old", queries[0].Symbol)
	}

	// l past the left cell's line end crosses back to the right cell.
	m = press(m, "$")
	m = press(m, "l")
	if m.splitActiveLeft {
		t.Fatal("l at the left cell's line end did not cross back")
	}
}

func TestMouseClickSplitTextSelectsSideAndColumn(t *testing.T) {
	t.Parallel()

	m := pressZS(Model{files: []diff.FileDiff{splitTestFile("a.go")}, width: 140, height: 24})
	rows := m.currentRows()
	rowIdx := -1
	for i, row := range rows {
		if row.Kind == ui.RowPair && row.Left != nil && row.Right != nil && row.Left != row.Right {
			rowIdx = i
			break
		}
	}
	if rowIdx < 0 {
		t.Fatal("changed split pair not found")
	}
	layout := m.mainLayout()
	l := m.layoutFor(rows)
	y := layout.DiffRowsY + l.RowStart(rowIdx) - m.topScreenLine(l)
	lw, rw, ok := ui.PairColumnWidths(layout.DiffContentWidth)
	if !ok {
		t.Fatal("test layout does not support split view")
	}
	leftStart := ui.PairLeftCellEnd(lw) - lw
	rightStart := layout.DiffContentWidth - rw

	next, _ := m.handleMouse(tea.MouseMsg{
		X:      layout.DiffContentX + leftStart + 4,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Type:   tea.MouseLeft,
	})
	got := next.(Model)
	if got.cursor != rowIdx || !got.splitActiveLeft || got.col != 4 {
		t.Fatalf("left click = row %d side-left %v col %d, want row %d side-left true col 4", got.cursor, got.splitActiveLeft, got.col, rowIdx)
	}

	next, _ = got.handleMouse(tea.MouseMsg{
		X:      layout.DiffContentX + rightStart + 2,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Type:   tea.MouseLeft,
	})
	got = next.(Model)
	if got.cursor != rowIdx || got.splitActiveLeft || got.col != 2 {
		t.Fatalf("right click = row %d side-left %v col %d, want row %d side-left false col 2", got.cursor, got.splitActiveLeft, got.col, rowIdx)
	}
}

func symbolNames(queries []navsearch.SymbolQuery) []string {
	names := make([]string, 0, len(queries))
	for _, q := range queries {
		names = append(names, q.Symbol)
	}
	return names
}
