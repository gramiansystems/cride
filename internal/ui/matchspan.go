package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// MatchSpanSide locates a span within a side-by-side pair row. Unified rows
// always use MatchSideUnified; MatchSideBoth marks context pairs whose text
// appears in both columns.
type MatchSpanSide int

const (
	MatchSideUnified MatchSpanSide = iota
	MatchSideLeft
	MatchSideRight
	MatchSideBoth
)

// MatchSpan marks a highlighted range on one diff row, e.g. an in-file search
// match. Start/End are display columns within the row's tab-expanded content
// (not counting the line-number gutter).
type MatchSpan struct {
	RowIdx  int
	Start   int
	End     int
	Current bool
	Cursor  bool // character-cursor cell; its style wins over match styling
	Side    MatchSpanSide
}

// diffRowPrefixWidth is the visible width of everything renderRow places
// before a line row's content: relative number, both line-number gutters,
// diagnostic marker, sign, and separators.
const diffRowPrefixWidth = 18

func matchSpansByRow(spans []MatchSpan) map[int][]MatchSpan {
	if len(spans) == 0 {
		return nil
	}
	byRow := make(map[int][]MatchSpan)
	for _, span := range spans {
		byRow[span.RowIdx] = append(byRow[span.RowIdx], span)
	}
	return byRow
}

// applyMatchSpans overlays match backgrounds on one wrapped screen line of a
// row. wrapIdx selects which slice of the row's columns this line shows; spans
// use content columns, so the gutter prefix is added here. baseBg (may be
// empty) is re-asserted after each span so cursor/change-row backgrounds
// survive the overlay.
func applyMatchSpans(line string, spans []MatchSpan, wrapIdx, width int, baseBg lipgloss.Color) string {
	if len(spans) == 0 || width <= 0 {
		return line
	}
	restore := "\x1b[49m"
	if bg, ok := backgroundSequence(baseBg); ok {
		restore = bg
	}
	lineStart := wrapIdx * width
	for _, span := range spans {
		absStart := span.Start + diffRowPrefixWidth
		absEnd := span.End + diffRowPrefixWidth
		from := max(absStart-lineStart, 0)
		to := min(absEnd-lineStart, width)
		if to <= from || from >= width {
			continue
		}
		line = forceBackgroundRange(line, spanBackground(span), restore, from, to)
	}
	return line
}

// spanBackground picks the escape sequence for one span's role.
func spanBackground(span MatchSpan) string {
	switch {
	case span.Cursor:
		return charCursorBgSeq
	case span.Current:
		return searchCurrentBgSeq
	default:
		return searchMatchBgSeq
	}
}

// applyPairMatchSpans overlays match backgrounds on one screen line of a
// side-by-side pair row. Each side's spans map into that side's column
// window; MatchSideBoth spans render in both columns.
func applyPairMatchSpans(line string, spans []MatchSpan, wrapIdx, width int, baseBg lipgloss.Color) string {
	lw, rw, ok := PairColumnWidths(width)
	if !ok {
		return applyMatchSpans(line, spans, wrapIdx, width, baseBg)
	}
	restore := "\x1b[49m"
	if bg, ok := backgroundSequence(baseBg); ok {
		restore = bg
	}
	leftOffset := pairRelWidth + pairColPrefix
	rightOffset := pairRelWidth + pairColPrefix + lw + pairDividerWidth + pairColPrefix

	apply := func(line string, span MatchSpan, cellOffset, cellWidth int) string {
		lineStart := wrapIdx * cellWidth
		from := max(span.Start-lineStart, 0)
		to := min(span.End-lineStart, cellWidth)
		if to <= from || from >= cellWidth {
			return line
		}
		return forceBackgroundRange(line, spanBackground(span), restore, cellOffset+from, cellOffset+to)
	}

	for _, span := range spans {
		switch span.Side {
		case MatchSideLeft:
			line = apply(line, span, leftOffset, lw)
		case MatchSideRight:
			line = apply(line, span, rightOffset, rw)
		case MatchSideBoth:
			line = apply(line, span, leftOffset, lw)
			line = apply(line, span, rightOffset, rw)
		}
	}
	return line
}

// forceBackgroundRange re-asserts bg over the [from, to) visible-column range
// of a styled line, re-injecting it after any SGR sequence inside the range so
// syntax highlighting cannot reset it, and restoring the previous background
// after the range.
func forceBackgroundRange(s, bg, restore string, from, to int) string {
	if from >= to || bg == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4*(len(bg)+len(restore)))
	col := 0
	inSpan := false
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			end := i + 2
			for end < len(s) && !isANSICommandByte(s[end]) {
				end++
			}
			if end < len(s) {
				end++
			}
			b.WriteString(s[i:end])
			if inSpan && s[end-1] == 'm' {
				b.WriteString(bg)
			}
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !inSpan && col >= from && col < to {
			b.WriteString(bg)
			inSpan = true
		}
		if inSpan && col >= to {
			b.WriteString(restore)
			inSpan = false
		}
		b.WriteString(s[i : i+size])
		col += runewidth.RuneWidth(r)
		i += size
	}
	if inSpan {
		b.WriteString(restore)
	}
	return b.String()
}
