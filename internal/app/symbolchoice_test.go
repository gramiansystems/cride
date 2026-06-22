package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

func symbolChoiceModel(content string) Model {
	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineContext,
				Content: content,
				OldLine: 1,
				NewLine: 1,
			}},
		}},
	}}
	return Model{
		source:       fakeSource{},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}
}

func TestSymbolChoiceHighlightsCandidatesInline(t *testing.T) {
	t.Parallel()

	m := symbolChoiceModel("return Alpha(Beta)")
	m = press(m, "g")
	m = press(m, "r")
	if m.overlay.Kind != OverlaySymbolChoice {
		t.Fatalf("overlay kind = %v, want OverlaySymbolChoice", m.overlay.Kind)
	}

	spans := m.symbolChoiceSpans()
	want := []ui.MatchSpan{
		{RowIdx: 1, Start: 7, End: 12, Current: true, Side: ui.MatchSideUnified},   // Alpha
		{RowIdx: 1, Start: 13, End: 17, Current: false, Side: ui.MatchSideUnified}, // Beta
	}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v, want %+v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Fatalf("span[%d] = %+v, want %+v", i, spans[i], want[i])
		}
	}

	hints := strings.Join(m.contextualHints(), " ")
	if !strings.Contains(hints, "1/2 Alpha") || !strings.Contains(hints, "`←/→`symbol") {
		t.Fatalf("hints = %q, want candidate counter and ←/→ hint", hints)
	}

	// No popup: the base view renders unobscured.
	if view := m.View(); strings.Contains(view, "Choose") {
		t.Fatalf("view contains a choice popup:\n%s", view)
	}
}

func TestSymbolChoiceLeftRightWrapAndCancel(t *testing.T) {
	t.Parallel()

	m := symbolChoiceModel("return Alpha(Beta)")
	m = press(m, "g")
	m = press(m, "r")

	step := func(msg tea.KeyMsg) {
		next, _ := m.handleKey(msg)
		m = next.(Model)
	}

	step(tea.KeyMsg{Type: tea.KeyRight})
	if m.overlay.Cursor != 1 {
		t.Fatalf("cursor after right = %d, want 1", m.overlay.Cursor)
	}
	step(tea.KeyMsg{Type: tea.KeyRight})
	if m.overlay.Cursor != 0 {
		t.Fatalf("cursor after wrapping right = %d, want 0", m.overlay.Cursor)
	}
	step(tea.KeyMsg{Type: tea.KeyLeft})
	if m.overlay.Cursor != 1 {
		t.Fatalf("cursor after wrapping left = %d, want 1", m.overlay.Cursor)
	}
	if spans := m.symbolChoiceSpans(); !spans[1].Current || spans[0].Current {
		t.Fatalf("spans after moves = %+v, want Beta current", spans)
	}

	step(tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay.Kind != OverlayNone {
		t.Fatalf("overlay kind after esc = %v, want OverlayNone", m.overlay.Kind)
	}
	if spans := m.symbolChoiceSpans(); spans != nil {
		t.Fatalf("spans after esc = %+v, want nil", spans)
	}
	if m.referencePanel.Open {
		t.Fatal("reference panel opened after cancel")
	}
}

func TestSymbolDisplaySpanExpandsTabs(t *testing.T) {
	t.Parallel()

	// One tab before the symbol: byte column 2, display column 4.
	start, end, ok := symbolDisplaySpan("\tAlpha(Beta)", 2, "Alpha")
	if !ok || start != 4 || end != 9 {
		t.Fatalf("span = %d..%d ok=%v, want 4..9 true", start, end, ok)
	}
	// Content drifted (live reload): the stale column must not highlight.
	if _, _, ok := symbolDisplaySpan("\tGamma(Beta)", 2, "Alpha"); ok {
		t.Fatal("stale symbol column still produced a span")
	}
}
