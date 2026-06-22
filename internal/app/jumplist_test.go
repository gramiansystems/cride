package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	navsearch "cride/internal/search"
	"cride/internal/source"
)

func jumpTestModel() Model {
	lines := numberedLines(20)
	files := []diff.FileDiff{testFileWithLines("a.go", 20), testFileWithLines("b.go", 20)}
	return Model{
		source:       fakeSource{},
		files:        files,
		changedPaths: changedPathSet(files),
		viewMode:     ViewDiff,
		selectedFile: 0,
		width:        80,
		height:       30,
		fileContents: map[string]fileContentState{
			"a.go": {lines: lines, loaded: true},
			"b.go": {lines: lines, loaded: true},
		},
	}
}

func referenceTo(path string, line int) navsearch.ReferenceResult {
	return navsearch.ReferenceResult{
		Location: source.Location{Path: path, Line: line, Column: 1},
		Source:   navsearch.ResultSourceRG,
	}
}

func TestJumpBackAndForwardAcrossFiles(t *testing.T) {
	t.Parallel()

	m := jumpTestModel()
	if !m.positionCursorAtLocation(source.Location{Path: "a.go", Line: 5, Column: 1}) {
		t.Fatal("failed to position cursor at a.go:5")
	}
	startCursor := m.cursor

	m.jumpToReferenceResult(referenceTo("b.go", 15))
	if got := m.currentFilePath(); got != "b.go" {
		t.Fatalf("after jump path = %q, want b.go", got)
	}
	if got := cursorSourceLine(m); got != 15 {
		t.Fatalf("after jump source line = %d, want 15", got)
	}

	m.jumpBack()
	if got := m.currentFilePath(); got != "a.go" {
		t.Fatalf("after back path = %q, want a.go", got)
	}
	if m.viewMode != ViewDiff {
		t.Fatalf("after back viewMode = %v, want ViewDiff", m.viewMode)
	}
	if got := cursorSourceLine(m); got != 5 {
		t.Fatalf("after back source line = %d, want 5", got)
	}
	if m.cursor != startCursor {
		t.Fatalf("after back cursor = %d, want %d", m.cursor, startCursor)
	}

	m.jumpForward()
	if got := m.currentFilePath(); got != "b.go" {
		t.Fatalf("after forward path = %q, want b.go", got)
	}
	if m.viewMode != ViewFile {
		t.Fatalf("after forward viewMode = %v, want ViewFile", m.viewMode)
	}
	if got := cursorSourceLine(m); got != 15 {
		t.Fatalf("after forward source line = %d, want 15", got)
	}
}

func TestJumpBackAtOldestKeepsPositionAndNotifies(t *testing.T) {
	t.Parallel()

	m := jumpTestModel()
	m.cursor = 3

	cmd := m.jumpBack()
	if cmd == nil {
		t.Fatal("expected a toast command at the oldest position")
	}
	if m.status.text == "" {
		t.Fatal("expected a toast message at the oldest position")
	}
	if got := m.currentFilePath(); got != "a.go" {
		t.Fatalf("path changed to %q, want a.go", got)
	}
	if m.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", m.cursor)
	}
}

func TestNewJumpTruncatesForwardHistory(t *testing.T) {
	t.Parallel()

	m := jumpTestModel()
	if !m.positionCursorAtLocation(source.Location{Path: "a.go", Line: 2, Column: 1}) {
		t.Fatal("failed to position cursor at a.go:2")
	}

	m.jumpToReferenceResult(referenceTo("b.go", 10))
	m.jumpBack()
	if got := cursorSourceLine(m); got != 2 {
		t.Fatalf("after back source line = %d, want 2", got)
	}

	// A fresh jump from here should drop the b.go:10 forward entry.
	m.jumpToReferenceResult(referenceTo("b.go", 18))
	if m.status.text != "" {
		t.Fatalf("unexpected toast %q after a fresh jump", m.status.text)
	}
	m.jumpForward()
	if m.status.text == "" {
		t.Fatal("expected a toast at the newest position")
	}
	if got := cursorSourceLine(m); got != 18 {
		t.Fatalf("after forward at newest, source line = %d, want 18", got)
	}

	m.jumpBack()
	if got := m.currentFilePath(); got != "a.go" {
		t.Fatalf("after back path = %q, want a.go", got)
	}
	if got := cursorSourceLine(m); got != 2 {
		t.Fatalf("after back source line = %d, want 2", got)
	}
	m.jumpForward()
	if got := cursorSourceLine(m); got != 18 {
		t.Fatalf("after forward source line = %d, want 18", got)
	}
}

func TestJumpKeysRouteThroughCtrlOAndCtrlCloseBracket(t *testing.T) {
	t.Parallel()

	m := jumpTestModel()
	if !m.positionCursorAtLocation(source.Location{Path: "a.go", Line: 5, Column: 1}) {
		t.Fatal("failed to position cursor at a.go:5")
	}
	m.jumpToReferenceResult(referenceTo("b.go", 15))

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	got := next.(Model)
	if got.currentFilePath() != "a.go" {
		t.Fatalf("after ctrl+o path = %q, want a.go", got.currentFilePath())
	}

	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	got = next.(Model)
	if got.currentFilePath() != "b.go" {
		t.Fatalf("after ctrl+] path = %q, want b.go", got.currentFilePath())
	}
}

// The references/enrichment panel deliberately stays open after a jump
// (TestReferenceResultJumpKeepsCursorAboveBottomPanel) and claims j/k/o for
// its own list navigation. A letter-based jump binding would get swallowed
// by the still-open panel right when it's most wanted; the control chords
// must keep working regardless of panel state.
func TestJumpKeysWorkWithReferencePanelStillOpen(t *testing.T) {
	t.Parallel()

	m := jumpTestModel()
	if !m.positionCursorAtLocation(source.Location{Path: "a.go", Line: 5, Column: 1}) {
		t.Fatal("failed to position cursor at a.go:5")
	}
	m.jumpToReferenceResult(referenceTo("b.go", 15))
	m.referencePanel = referencePanelState{
		Open:   true,
		Query:  navsearch.SymbolQuery{Symbol: "Target"},
		Source: navsearch.ResultSourceRG,
		Results: []navsearch.ReferenceResult{
			referenceTo("b.go", 15),
			referenceTo("a.go", 1),
		},
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	got := next.(Model)
	if got.currentFilePath() != "a.go" {
		t.Fatalf("after ctrl+o with panel open, path = %q, want a.go", got.currentFilePath())
	}
	if !got.referencePanel.Open {
		t.Fatal("expected the reference panel to remain open across ctrl+o")
	}

	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	got = next.(Model)
	if got.currentFilePath() != "b.go" {
		t.Fatalf("after ctrl+] with panel open, path = %q, want b.go", got.currentFilePath())
	}
}
