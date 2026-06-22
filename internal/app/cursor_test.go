package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

// cursorTestFile builds one file whose single hunk carries multi-word content
// with brackets so motions have something to bite on.
func cursorTestFile() diff.FileDiff {
	return diff.FileDiff{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -1,4 +1,4 @@",
			NewStart: 1, NewLines: 4,
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "func alpha(beta int) {", OldLine: 1, NewLine: 1},
				{Kind: diff.LineAdd, Content: "\treturn beta + gamma", NewLine: 2},
				{Kind: diff.LineContext, Content: "}", OldLine: 3, NewLine: 3},
				{Kind: diff.LineContext, Content: "var delta = epsilon", OldLine: 4, NewLine: 4},
			},
		}},
	}
}

func cursorTestModel() Model {
	m := Model{files: []diff.FileDiff{cursorTestFile()}, width: 100, height: 24}
	rows := m.currentRows()
	for i, r := range rows {
		if r.IsLineRow() {
			m.cursor = i
			break
		}
	}
	return m
}

func TestCharMotionMovesAndClamps(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	m = press(m, "l")
	m = press(m, "l")
	if m.col != 2 {
		t.Fatalf("col after ll = %d, want 2", m.col)
	}
	m = press(m, "h")
	if m.col != 1 {
		t.Fatalf("col after h = %d, want 1", m.col)
	}
	// h at column 0 stays put in unified view.
	m = press(m, "h")
	m = press(m, "h")
	if m.col != 0 {
		t.Fatalf("col after hh at start = %d, want 0", m.col)
	}
	// $ pins to the line end; l cannot pass it.
	m = press(m, "$")
	want := len([]rune("func alpha(beta int) {")) - 1
	if m.col != want {
		t.Fatalf("col after $ = %d, want %d", m.col, want)
	}
	m = press(m, "l")
	if m.col != want {
		t.Fatalf("col after l at line end = %d, want %d", m.col, want)
	}
}

func TestDesiredColumnSticksAcrossVerticalMotion(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	m = press(m, "$") // end of "func alpha(beta int) {"
	m = press(m, "j") // "\treturn beta + gamma" is shorter
	long := len([]rune("func alpha(beta int) {")) - 1
	short := len([]rune("\treturn beta + gamma")) - 1
	if m.col != short {
		t.Fatalf("col on shorter line = %d, want %d", m.col, short)
	}
	m = press(m, "k")
	if m.col != long {
		t.Fatalf("$ did not stick across jk: col = %d, want %d", m.col, long)
	}

	m = press(m, "0")
	for i := 0; i < 5; i++ {
		m = press(m, "l")
	}
	m = press(m, "j")
	m = press(m, "j") // "}" has only one column
	if m.col != 0 {
		t.Fatalf("col on \"}\" line = %d, want 0", m.col)
	}
	m = press(m, "k")
	m = press(m, "k")
	if m.col != 5 {
		t.Fatalf("desired column lost across vertical motion: col = %d, want 5", m.col)
	}
}

func TestWordMotionCrossesRows(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	m = press(m, "w") // func -> alpha
	if m.col != 5 {
		t.Fatalf("col after w = %d, want 5", m.col)
	}
	firstRow := m.cursor
	for i := 0; i < 8; i++ {
		m = press(m, "w")
	}
	if m.cursor <= firstRow {
		t.Fatalf("w never crossed to the next row (row %d)", m.cursor)
	}
	for i := 0; i < 12; i++ {
		m = press(m, "b")
	}
	if m.cursor != firstRow || m.col != 0 {
		t.Fatalf("b did not return to start: row %d col %d, want row %d col 0", m.cursor, m.col, firstRow)
	}
}

func TestFindCharAndRepeat(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	// f a on "func alpha(beta int) {" -> the a of alpha (col 5).
	m = press(m, "f")
	if m.pendingFind != 'f' {
		t.Fatalf("pendingFind = %q, want f", m.pendingFind)
	}
	m = press(m, "a")
	if m.col != 5 {
		t.Fatalf("col after fa = %d, want 5", m.col)
	}
	m = press(m, ";") // next a: alph_a_ (col 9)
	if m.col != 9 {
		t.Fatalf("col after ; = %d, want 9", m.col)
	}
	m = press(m, ",") // back to col 5
	if m.col != 5 {
		t.Fatalf("col after , = %d, want 5", m.col)
	}
	// t i stops one short of "int" (space at col 15).
	m = press(m, "0")
	m = press(m, "t")
	m = press(m, "i")
	if m.col != 15 {
		t.Fatalf("col after ti = %d, want 15", m.col)
	}
	// esc cancels a pending find without moving.
	before := m.col
	m = press(m, "f")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.pendingFind != 0 || m.col != before {
		t.Fatalf("esc did not cancel pending find (pending=%q col=%d)", m.pendingFind, m.col)
	}
}

func TestQuestionMarkRemainsAFindTarget(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	m.files[0].Hunks[0].Lines[0].Content = "a?b"
	m = press(m, "f")
	m = press(m, "?")
	if m.overlay.Kind != OverlayNone {
		t.Fatalf("f? opened overlay kind %v, want no overlay", m.overlay.Kind)
	}
	if m.col != 1 {
		t.Fatalf("column after f? = %d, want 1", m.col)
	}
}

func TestMatchBracketSameRowAndAcrossRows(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	// % from column 0 uses the first bracket on the row: ( -> ).
	m = press(m, "%")
	content := []rune("func alpha(beta int) {")
	wantClose := 0
	for i, r := range content {
		if r == ')' {
			wantClose = i
		}
	}
	if m.col != wantClose {
		t.Fatalf("col after %% = %d, want %d", m.col, wantClose)
	}
	// % again bounces back to the opening paren.
	m = press(m, "%")
	if m.col != 10 {
		t.Fatalf("col after %%%% = %d, want 10", m.col)
	}
	// From the trailing { the match is } two rows down.
	m = press(m, "$")
	startRow := m.cursor
	m = press(m, "%")
	if m.cursor == startRow {
		t.Fatal("% did not cross rows for the brace")
	}
	rows := m.currentRows()
	if got := rows[m.cursor].Line.Content; got != "}" {
		t.Fatalf("%% landed on %q, want }", got)
	}
}

func TestMatchBracketStopsAtHiddenContext(t *testing.T) {
	t.Parallel()

	// Two hunks with a gap: the { in hunk one has no visible partner.
	file := diff.FileDiff{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{
			{
				Header:   "@@ -1,2 +1,2 @@",
				NewStart: 1, NewLines: 2,
				Lines: []diff.Line{
					{Kind: diff.LineAdd, Content: "func f() {", NewLine: 1},
					{Kind: diff.LineContext, Content: "\tx()", OldLine: 1, NewLine: 2},
				},
			},
			{
				Header:   "@@ -10,1 +10,1 @@",
				NewStart: 10, NewLines: 1,
				Lines: []diff.Line{
					{Kind: diff.LineContext, Content: "}", OldLine: 10, NewLine: 10},
				},
			},
		},
	}
	m := Model{files: []diff.FileDiff{file}, width: 100, height: 24}
	rows := m.currentRows()
	for i, r := range rows {
		if r.IsLineRow() {
			m.cursor = i
			break
		}
	}
	m = press(m, "$") // on the {
	startRow, startCol := m.cursor, m.col
	m = press(m, "%")
	if m.cursor != startRow || m.col != startCol {
		t.Fatalf("%% crossed a hidden-context gap: row %d col %d", m.cursor, m.col)
	}
}

func TestCursorSpanGeometry(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	m = press(m, "j") // "\treturn beta + gamma": tab expands to 4 columns
	m = press(m, "l") // col 1, the r of return
	spans := m.cursorSpan()
	if len(spans) != 1 {
		t.Fatalf("cursorSpan count = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.RowIdx != m.cursor || !s.Cursor {
		t.Fatalf("span row/kind = %+v", s)
	}
	if s.Start != 4 || s.End != 5 {
		t.Fatalf("span after tab = [%d,%d), want [4,5)", s.Start, s.End)
	}
	// On the tab itself the cell covers the full expansion.
	m = press(m, "0")
	spans = m.cursorSpan()
	if spans[0].Start != 0 || spans[0].End != 4 {
		t.Fatalf("tab span = [%d,%d), want [0,4)", spans[0].Start, spans[0].End)
	}
	// No span while the change list has focus.
	m.focus = paneList
	if got := m.cursorSpan(); got != nil {
		t.Fatalf("cursorSpan with list focus = %v, want nil", got)
	}
}

func TestSymbolUnderCursorSkipsChooser(t *testing.T) {
	t.Parallel()

	m := cursorTestModel()
	// Cursor at col 0 sits on "func" (keyword): the chooser fallback applies,
	// so multiple candidates come back.
	queries, ok := m.currentSymbolQueries()
	if !ok || len(queries) < 2 {
		t.Fatalf("keyword position queries = %v ok=%v, want multiple candidates", queries, ok)
	}
	// On beta, the lookup resolves directly to one query.
	m = press(m, "f")
	m = press(m, "b")
	queries, ok = m.currentSymbolQueries()
	if !ok || len(queries) != 1 || queries[0].Symbol != "beta" {
		t.Fatalf("under-cursor queries = %v ok=%v, want [beta]", queries, ok)
	}
}

func TestCursorColPersistsAcrossFileSwitch(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{cursorTestFile(), testFile("b.go")}, width: 100, height: 24}
	rows := m.currentRows()
	for i, r := range rows {
		if r.IsLineRow() {
			m.cursor = i
			break
		}
	}
	m = press(m, "l")
	m = press(m, "l")
	wantRow, wantCol := m.cursor, m.col
	m = press(m, "}")
	m = press(m, "{")
	if m.cursor != wantRow || m.col != wantCol {
		t.Fatalf("position after file round-trip = row %d col %d, want row %d col %d", m.cursor, m.col, wantRow, wantCol)
	}
}

func TestCursorRowSideSnapsToContent(t *testing.T) {
	t.Parallel()

	left := diff.Line{Kind: diff.LineDelete, Content: "old", OldLine: 1}
	right := diff.Line{Kind: diff.LineAdd, Content: "new", NewLine: 1}
	pair := ui.Row{Kind: ui.RowPair, Left: &left, Right: &right}
	if !cursorRowSide(pair, true) || cursorRowSide(pair, false) {
		t.Fatal("full pair did not respect the preferred side")
	}
	leftOnly := ui.Row{Kind: ui.RowPair, Left: &left}
	if !cursorRowSide(leftOnly, false) {
		t.Fatal("right preference did not snap to the only (left) cell")
	}
	rightOnly := ui.Row{Kind: ui.RowPair, Right: &right}
	if cursorRowSide(rightOnly, true) {
		t.Fatal("left preference did not snap to the only (right) cell")
	}
	del := ui.Row{Kind: ui.RowLine, Line: left}
	if !cursorRowSide(del, false) {
		t.Fatal("unified delete row is not baseline-side")
	}
}
