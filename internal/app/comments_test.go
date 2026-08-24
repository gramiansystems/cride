package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCtrlSSavesCanonicalReviewWithoutQuitting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m := commentTestModel()
	m.source = fakeSource{root: root}
	m.review.Comments = []annotate.Comment{{
		ID:       "c1",
		Body:     "please simplify this",
		Severity: annotate.SeverityMustFix,
		Created:  time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		Status:   annotate.StatusOpen,
	}}

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("ctrl+s returned no save command")
	}
	if m.mode != modeReview {
		t.Fatalf("ctrl+s changed mode to %v", m.mode)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if !strings.Contains(m.status.text, "saved") {
		t.Fatalf("save toast = %q", m.status.text)
	}

	markdown, err := os.ReadFile(filepath.Join(root, annotate.ExportName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "please simplify this") || !strings.Contains(string(markdown), "Baseline: HEAD") {
		t.Fatalf("review.md = %q", markdown)
	}
}

func TestSavingCommentRefreshesMarkdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m := commentTestModel()
	m.source = fakeSource{root: root}
	m.cursor = 2
	next, _ := m.handleKey(key("c"))
	m = next.(Model)
	m = typeComposer(m, "keep the guard clause")
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("saving a comment returned no persistence command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatal("comment save did not batch persistence")
	}
	next, _ = m.Update(batch[0]())
	m = next.(Model)
	if m.status.sticky {
		t.Fatalf("comment save failed: %s", m.status.text)
	}
	markdown, err := os.ReadFile(filepath.Join(root, annotate.ExportName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "keep the guard clause") {
		t.Fatalf("review.md was not refreshed: %q", markdown)
	}
}

func TestCtrlRLoadsEmptyReviewWhenMarkdownIsMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m := commentTestModel()
	m.source = fakeSource{root: root}
	m.review.Comments = []annotate.Comment{{ID: "stale", Body: "stale"}}
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("ctrl+r returned no reload command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("ctrl+r did not batch the diff and comment reloads")
	}
	for _, sub := range batch {
		msg := sub()
		next, _ = m.Update(msg)
		m = next.(Model)
	}
	if len(m.review.Comments) != 0 {
		t.Fatalf("missing review.md loaded comments = %+v", m.review.Comments)
	}
	if _, err := os.Stat(filepath.Join(root, annotate.ExportName)); !os.IsNotExist(err) {
		t.Fatalf("reload unexpectedly created a missing review.md: %v", err)
	}
}

func TestCtrlRImportsEditedReviewMarkdown(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	created := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	existing := annotate.Review{Comments: []annotate.Comment{{
		ID:       "keep-me",
		Body:     "original wording",
		Severity: annotate.SeverityQuestion,
		Created:  created,
		Status:   annotate.StatusOpen,
	}}}
	markdown := strings.Replace(string(annotate.ExportMarkdown(existing)), "original wording", "edited wording", 1)
	if err := os.WriteFile(filepath.Join(root, annotate.ExportName), []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}

	m := commentTestModel()
	m.source = fakeSource{root: root}
	m.review = existing
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("ctrl+r did not batch the diff and review.md reloads")
	}
	for _, sub := range batch {
		msg := sub()
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	if len(m.review.Comments) != 1 || m.review.Comments[0].Body != "edited wording" {
		t.Fatalf("imported review = %+v", m.review)
	}
	if m.review.Comments[0].ID != "keep-me" || !m.review.Comments[0].Created.Equal(created) {
		t.Fatalf("import lost comment identity: %+v", m.review.Comments[0])
	}
}

func TestMalformedReviewMarkdownDoesNotReplaceCurrentReview(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, annotate.ExportName), []byte("# Review\n\n## a.go\n\n### not an anchor\ntext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := commentTestModel()
	m.source = fakeSource{root: root}
	m.review.Comments = []annotate.Comment{{ID: "current", Body: "keep this", Status: annotate.StatusOpen}}
	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	batch := cmd().(tea.BatchMsg)
	var reviewMsg, diffMsg tea.Msg
	for _, sub := range batch {
		msg := sub()
		switch msg.(type) {
		case reviewLoadedMsg:
			reviewMsg = msg
		case diffLoadedMsg:
			diffMsg = msg
		}
	}
	if reviewMsg == nil || diffMsg == nil {
		t.Fatal("reload did not produce both review and diff messages")
	}
	// Apply the error first to verify the slower diff result cannot hide it.
	next, _ = m.Update(reviewMsg)
	m = next.(Model)
	next, _ = m.Update(diffMsg)
	m = next.(Model)
	if len(m.review.Comments) != 1 || m.review.Comments[0].ID != "current" {
		t.Fatalf("malformed import replaced current review: %+v", m.review.Comments)
	}
	if !m.status.sticky || !strings.Contains(m.status.text, "invalid comment heading") {
		t.Fatalf("malformed import status = %+v", m.status)
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
