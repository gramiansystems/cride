package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestForceBackgroundRangeComposesWithInnerSGR(t *testing.T) {
	t.Parallel()

	bg := searchMatchBgSeq
	if bg == "" {
		t.Fatal("no match background sequence")
	}
	// "hello world" with an SGR reset in the middle of the span.
	line := "he\x1b[0mllo world"
	got := forceBackgroundRange(line, bg, "\x1b[49m", 0, 5)

	if !strings.HasPrefix(got, bg) {
		t.Fatalf("span start missing bg: %q", got)
	}
	if !strings.Contains(got, "\x1b[0m"+bg) {
		t.Fatalf("bg not re-asserted after inner SGR: %q", got)
	}
	if !strings.Contains(got, "\x1b[49m") {
		t.Fatalf("bg not restored after span: %q", got)
	}
	if stripANSI(got) != "hello world" {
		t.Fatalf("printable content changed: %q", stripANSI(got))
	}
}

func TestApplyMatchSpansRestoresRowBackground(t *testing.T) {
	t.Parallel()

	// A cursor-row line has a persistent background; the match overlay must
	// restore it (not default) after the span.
	cursorBg, _ := backgroundSequence(colorCursor)
	line := withPersistentBackground(padRight("some content here", 40), colorCursor)
	got := applyMatchSpans(line, []MatchSpan{{RowIdx: 0, Start: 0, End: 4, Current: true}}, 0, 40, colorCursor)

	if !strings.Contains(got, searchCurrentBgSeq) {
		t.Fatalf("current-match bg missing: %q", got)
	}
	if !strings.Contains(got, searchCurrentBgSeq) || !strings.Contains(got[strings.Index(got, searchCurrentBgSeq):], cursorBg) {
		t.Fatalf("cursor bg not restored after span: %q", got)
	}
	if stripANSI(got) != stripANSI(line) {
		t.Fatalf("printable content changed:\n%q\n%q", stripANSI(got), stripANSI(line))
	}
}

func TestApplyMatchSpansOffsetsByGutterAndWrap(t *testing.T) {
	t.Parallel()

	width := 30
	// Content column 0 sits at diffRowPrefixWidth on the first wrapped line.
	line := padRight(strings.Repeat("x", width), width)
	first := applyMatchSpans(line, []MatchSpan{{Start: 0, End: 2}}, 0, width, lipgloss.Color(""))
	if !strings.Contains(first, searchMatchBgSeq) {
		t.Fatalf("span on first wrap line missing: %q", first)
	}
	idx := strings.Index(first, searchMatchBgSeq)
	if got := len([]rune(stripANSI(first[:idx]))); got != diffRowPrefixWidth {
		t.Fatalf("span begins at col %d, want %d", got, diffRowPrefixWidth)
	}

	// The same span on the second wrapped line is off-screen: no overlay.
	second := applyMatchSpans(line, []MatchSpan{{Start: 0, End: 2}}, 1, width, lipgloss.Color(""))
	if strings.Contains(second, searchMatchBgSeq) {
		t.Fatalf("span leaked onto later wrap line: %q", second)
	}

	// A span past the first line lands on the second at the shifted offset.
	span := MatchSpan{Start: width - diffRowPrefixWidth + 3, End: width - diffRowPrefixWidth + 6}
	wrapped := applyMatchSpans(line, []MatchSpan{span}, 1, width, lipgloss.Color(""))
	if !strings.Contains(wrapped, searchMatchBgSeq) {
		t.Fatalf("span missing on second wrap line: %q", wrapped)
	}
	idx = strings.Index(wrapped, searchMatchBgSeq)
	if got := len([]rune(stripANSI(wrapped[:idx]))); got != 3 {
		t.Fatalf("wrapped span begins at col %d, want 3", got)
	}
}
