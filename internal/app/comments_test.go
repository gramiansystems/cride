package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/annotate"
	"cride/internal/diff"
	"cride/internal/ui"
)

func typeComposer(m Model, s string) Model {
	for _, r := range s {
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	return m
}

func commentTestModel() Model {
	return Model{
		files:  []diff.FileDiff{searchTestFile("a.go"), searchTestFile("b.go")},
		width:  100,
		height: 30,
	}
}

func TestComposeSaveRendersInlineComment(t *testing.T) {
	t.Parallel()

	m := commentTestModel()
	m.cursor = 2 // the added "beta two" row (line 2, current side)

	next, _ := m.handleKey(key("c"))
	m = next.(Model)
	if !m.composer.open {
		t.Fatal("c did not open the composer")
	}
	if m.composer.anchor == nil || m.composer.anchor.Path != "a.go" || m.composer.anchor.LineStart != 2 {
		t.Fatalf("composer anchor = %+v", m.composer.anchor)
	}
	if m.composer.snippet != "beta two" {
		t.Fatalf("composer snippet = %q", m.composer.snippet)
	}

	// Severity cycles.
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = next.(Model)
	if m.composer.severity != annotate.SeverityQuestion {
		t.Fatalf("severity after ctrl+t = %q", m.composer.severity)
	}

	m = typeComposer(m, "why two?")
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if m.composer.open {
		t.Fatal("ctrl+s did not close the composer")
	}
	if len(m.review.Comments) != 1 {
		t.Fatalf("comments = %d, want 1", len(m.review.Comments))
	}
	c := m.review.Comments[0]
	if c.Body != "why two?" || c.Severity != annotate.SeverityQuestion || c.Status != annotate.StatusOpen {
		t.Fatalf("saved comment = %+v", c)
	}

	// The comment renders inline under its anchor with a gutter marker.
	rows := m.currentRows()
	var commentRowIdx int
	found := false
	for i, r := range rows {
		if r.Kind == ui.RowComment && strings.Contains(r.Text, "why two?") {
			commentRowIdx = i
			found = true
		}
	}
	if !found {
		t.Fatal("comment body row not rendered")
	}
	anchorRow := rows[commentRowIdx-2] // header row sits between
	if !anchorRow.IsLineRow() || anchorRow.CommentID != c.ID {
		t.Fatalf("anchor row not marked: %+v", anchorRow)
	}
}

func TestComposeCancelAndEmptyDiscard(t *testing.T) {
	t.Parallel()

	m := commentTestModel()
	m.cursor = 1
	next, _ := m.handleKey(key("c"))
	m = next.(Model)
	m = typeComposer(m, "draft")
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.composer.open || len(m.review.Comments) != 0 {
		t.Fatalf("esc did not cancel: open=%v comments=%d", m.composer.open, len(m.review.Comments))
	}

	// Saving an empty body discards.
	next, _ = m.handleKey(key("C"))
	m = next.(Model)
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if len(m.review.Comments) != 0 {
		t.Fatal("empty comment was saved")
	}
	if !strings.Contains(m.status.text, "discarded") {
		t.Fatalf("no discard toast: %q", m.status.text)
	}
}

func TestGeneralCommentHasNoAnchor(t *testing.T) {
	t.Parallel()

	m := commentTestModel()
	next, _ := m.handleKey(key("C"))
	m = next.(Model)
	if m.composer.anchor != nil {
		t.Fatalf("general comment has anchor: %+v", m.composer.anchor)
	}
	m = typeComposer(m, "overall fine")
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if len(m.review.Comments) != 1 || m.review.Comments[0].Anchor != nil {
		t.Fatalf("general comment = %+v", m.review.Comments)
	}
}

func TestResolveToggleAndAnnotationNavigation(t *testing.T) {
	t.Parallel()

	m := commentTestModel()
	m.review.Comments = []annotate.Comment{
		{ID: "c1", Body: "first", Severity: annotate.SeverityNit, Status: annotate.StatusOpen,
			Anchor: &annotate.Anchor{Path: "a.go", Side: annotate.SideCurrent, LineStart: 1, LineEnd: 1}},
		{ID: "c2", Body: "second", Severity: annotate.SeverityMustFix, Status: annotate.StatusOpen,
			Anchor: &annotate.Anchor{Path: "b.go", Side: annotate.SideCurrent, LineStart: 3, LineEnd: 3}},
	}

	// ]a jumps to the first comment at/after the cursor; from row 0 in a.go
	// that's c1's anchor (line 1).
	m = press(press(m, "]"), "a")
	if m.currentFilePath() != "a.go" || cursorSourceLine(m) != 1 {
		t.Fatalf("]a landed at %s:%d, want a.go:1", m.currentFilePath(), cursorSourceLine(m))
	}
	// Next ]a crosses into b.go.
	m = press(press(m, "]"), "a")
	if m.currentFilePath() != "b.go" || cursorSourceLine(m) != 3 {
		t.Fatalf("second ]a landed at %s:%d, want b.go:3", m.currentFilePath(), cursorSourceLine(m))
	}
	// [a goes back.
	m = press(press(m, "["), "a")
	if m.currentFilePath() != "a.go" {
		t.Fatalf("[a landed in %s, want a.go", m.currentFilePath())
	}

	// x on the anchored row toggles resolution.
	m = press(m, "x")
	if m.review.Comments[0].Status != annotate.StatusResolved {
		t.Fatalf("x did not resolve: %+v", m.review.Comments[0])
	}
	m = press(m, "x")
	if m.review.Comments[0].Status != annotate.StatusOpen {
		t.Fatalf("second x did not reopen: %+v", m.review.Comments[0])
	}
}

func TestCommentAnchorsDetachOnDriftAndHeal(t *testing.T) {
	t.Parallel()

	m := commentTestModel()
	m.review.Comments = []annotate.Comment{{
		ID: "c1", Body: "check this", Severity: annotate.SeverityNit, Status: annotate.StatusOpen,
		Anchor:  &annotate.Anchor{Path: "a.go", Side: annotate.SideCurrent, LineStart: 2, LineEnd: 2},
		Snippet: "beta two",
	}}

	// Reload with changed content at the anchor: detached, never dropped.
	changed := []diff.FileDiff{searchTestFile("a.go"), searchTestFile("b.go")}
	changed[0].Hunks[0].Lines[1].Content = "beta 2 (edited)"
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: changed})
	m = next.(Model)
	m.refreshCommentAnchors()
	if m.review.Comments[0].Status != annotate.StatusUnresolved {
		t.Fatalf("drifted comment status = %q, want unresolved", m.review.Comments[0].Status)
	}
	if len(m.review.Comments) != 1 {
		t.Fatal("detached comment was dropped")
	}

	// Reverting the edit heals the anchor.
	reverted := []diff.FileDiff{searchTestFile("a.go"), searchTestFile("b.go")}
	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: reverted})
	m = next.(Model)
	m.refreshCommentAnchors()
	if m.review.Comments[0].Status != annotate.StatusOpen {
		t.Fatalf("healed comment status = %q, want open", m.review.Comments[0].Status)
	}
}
