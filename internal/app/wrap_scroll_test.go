package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

func wrappedTestFile(path string, lineCount, longEvery int) diff.FileDiff {
	lines := make([]diff.Line, 0, lineCount)
	for i := 1; i <= lineCount; i++ {
		content := "line"
		if longEvery > 0 && i%longEvery == 0 {
			content = strings.Repeat("wrapped-content ", 20) // ~320 cols
		}
		lines = append(lines, diff.Line{Kind: diff.LineContext, Content: content, OldLine: i, NewLine: i})
	}
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks:   []diff.Hunk{{Header: "@@ -1,1 +1,1 @@", Lines: lines}},
	}
}

func cursorFullyVisible(t *testing.T, m Model) {
	t.Helper()
	rows := m.currentRows()
	if len(rows) == 0 {
		return
	}
	l := m.layoutFor(rows)
	vh := m.viewHeight()
	topSL := m.topScreenLine(l)
	start := l.RowStart(m.cursor)
	end := start + l.RowHeight(m.cursor)
	if l.RowHeight(m.cursor) > vh {
		if start != topSL {
			t.Fatalf("oversized cursor row not pinned: start=%d top=%d", start, topSL)
		}
		return
	}
	if start < topSL || end > topSL+vh {
		t.Fatalf("cursor row [%d,%d) outside viewport [%d,%d)", start, end, topSL, topSL+vh)
	}
}

func TestWrappedScrollKeepsCursorVisible(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{wrappedTestFile("a.go", 30, 3)}, width: 80, height: 20}

	// Walk down through every row: the cursor row must stay fully visible and
	// j must always advance exactly one logical row.
	for i := 0; i < len(m.currentRows())-1; i++ {
		before := m.cursor
		m = press(m, "j")
		if m.cursor != before+1 {
			t.Fatalf("j moved cursor %d -> %d, want +1", before, m.cursor)
		}
		cursorFullyVisible(t, m)
	}
	// And back up.
	for m.cursor > 0 {
		m = press(m, "k")
		cursorFullyVisible(t, m)
	}
}

func TestWrappedPageScrollMovesByScreenLines(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{wrappedTestFile("a.go", 40, 2)}, width: 80, height: 20}
	l := m.layoutFor(m.currentRows())
	vh := m.viewHeight()

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = next.(Model)
	if got := m.topScreenLine(m.layoutFor(m.currentRows())); got != vh {
		t.Fatalf("ctrl+f scrolled to screen line %d, want %d (total=%d)", got, vh, l.TotalLines())
	}
	cursorFullyVisible(t, m)

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = next.(Model)
	if got := m.topScreenLine(m.layoutFor(m.currentRows())); got != 0 {
		t.Fatalf("ctrl+b returned to screen line %d, want 0", got)
	}
	cursorFullyVisible(t, m)
}

func TestWrappedMouseClickSelectsOwningRow(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{wrappedTestFile("a.go", 12, 2)}, width: 80, height: 24}
	m.clampScroll()

	rows := m.currentRows()
	l := m.layoutFor(rows)
	layout := ui.Layout(m.width, m.height, nil)
	topSL := m.topScreenLine(l)

	for y := layout.DiffRowsY; y < layout.DiffRowsY+layout.DiffRowsHeight; y++ {
		screenIdx := topSL + y - layout.DiffRowsY
		if screenIdx >= l.TotalLines() {
			break
		}
		want := l.LineAt(screenIdx).RowIdx
		next, _ := m.handleMouse(tea.MouseMsg{
			X:      layout.DiffContentX + 2,
			Y:      y,
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
		})
		got := next.(Model)
		if got.cursor != want {
			t.Fatalf("click at y=%d selected row %d, want %d (screen line %d)", y, got.cursor, want, screenIdx)
		}
	}
}

func TestResizePreservesCursorVisibility(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{wrappedTestFile("a.go", 40, 2)}, width: 120, height: 30}
	for i := 0; i < 20; i++ {
		m = press(m, "j")
	}
	cursorFullyVisible(t, m)
	cursorBefore := m.cursor

	// Narrower width makes long lines wrap taller; the cursor must stay put
	// logically and remain visible.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m = next.(Model)
	if m.cursor != cursorBefore {
		t.Fatalf("resize moved cursor %d -> %d", cursorBefore, m.cursor)
	}
	cursorFullyVisible(t, m)

	next, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = next.(Model)
	cursorFullyVisible(t, m)
}
