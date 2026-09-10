package ui

import (
	"sort"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"cride/internal/diff"
)

// ScreenLine identifies one terminal line of the soft-wrapped diff view.
type ScreenLine struct {
	RowIdx  int // index into []Row
	WrapIdx int // 0 = first screen line of the row
}

// WrapLayout maps logical rows to screen lines and back for one (rows, width)
// pair. Cursor and motion semantics stay row-based while scroll math and mouse
// hit-testing operate on screen lines. See DESIGN.md's "Rendering and
// interaction" section.
type WrapLayout struct {
	Width int

	heights []int // screen lines per row, always >= 1
	starts  []int // starts[rowIdx] = index of the row's first screen line
	total   int
}

// BuildWrapLayout computes the wrap layout for rows rendered at width.
// Measuring raw printable widths avoids constructing and styling every
// off-screen row. Highlighting does not change printable content, so the
// resulting heights still match the styled render.
func BuildWrapLayout(files []diff.FileDiff, rows []Row, width int) *WrapLayout {
	l := &WrapLayout{
		Width:   width,
		heights: make([]int, len(rows)),
		starts:  make([]int, len(rows)),
	}
	for i, r := range rows {
		h := rowScreenHeight(files, r, width)
		if h < 1 {
			h = 1
		}
		l.starts[i] = l.total
		l.heights[i] = h
		l.total += h
	}
	return l
}

// rowScreenHeight mirrors rowScreenLines without allocating rendered text.
// Prefix and suffix widths are fixed terminal columns; the row text itself
// is counted rune-by-rune to preserve hard-wrap behavior around wide glyphs.
func rowScreenHeight(files []diff.FileDiff, row Row, width int) int {
	if row.Kind == RowPair {
		lw, rw, ok := PairColumnWidths(width)
		if ok {
			left, right := 0, 0
			if row.Left != nil {
				left = wrappedTextHeight(row.Left.Content, lw, 0, 0)
			}
			if row.Right != nil {
				right = wrappedTextHeight(row.Right.Content, rw, 0, 0)
			}
			return max(1, max(left, right))
		}
		// Narrow split views use the unified fallback renderer.
		return wrappedTextHeight(row.Line.Content, width, unifiedRowPrefixWidth(row), 0)
	}

	switch row.Kind {
	case RowFileHeader:
		if row.FileIdx < 0 || row.FileIdx >= len(files) {
			return 1
		}
		file := files[row.FileIdx]
		// "    M " + path + "  +<adds> -<deletes>"
		suffix := 5 + decimalWidth(file.Added) + decimalWidth(file.Deleted)
		return wrappedTextHeight(file.Path(), width, 6, suffix)
	case RowHunkHeader:
		return wrappedTextHeight(row.Text, width, 4, 0)
	case RowComment:
		// Comment rows use fourteen spaces followed by "┃ ".
		return wrappedTextHeight(row.Text, width, diffRowPrefixWidth-2, 0)
	default:
		return wrappedTextHeight(row.Line.Content, width, unifiedRowPrefixWidth(row), 0)
	}
}

func unifiedRowPrefixWidth(row Row) int {
	width := diffRowPrefixWidth
	line := row.Line
	if line.Kind != diff.LineAdd {
		width += max(0, decimalWidth(line.OldLine)-4)
	}
	if line.Kind != diff.LineDelete {
		width += max(0, decimalWidth(line.NewLine)-4)
	}
	return width
}

func decimalWidth(n int) int {
	if n == 0 {
		return 1
	}
	width := 0
	if n < 0 {
		width++
		// Avoid overflowing on the minimum int. The loop below also works on
		// negative values, so there is no need to negate it.
	}
	for n != 0 {
		width++
		n /= 10
	}
	return width
}

// wrappedTextHeight matches wrapLine's forceful wrapping with PreserveSpace.
// Tabs are four spaces, and suffixWidth represents trailing ASCII columns.
func wrappedTextHeight(text string, limit, prefixWidth, suffixWidth int) int {
	if limit <= 0 {
		return 1
	}
	lines, lineWidth := 1, 0
	addWidth := func(width int) {
		if lineWidth+width > limit {
			lines++
			lineWidth = 0
		}
		lineWidth += width
	}
	for i := 0; i < prefixWidth; i++ {
		addWidth(1)
	}
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		switch r {
		case '\t':
			for i := 0; i < 4; i++ {
				addWidth(1)
			}
		case '\n':
			lines++
			lineWidth = 0
		default:
			addWidth(runewidth.RuneWidth(r))
		}
	}
	for i := 0; i < suffixWidth; i++ {
		addWidth(1)
	}
	return lines
}

// NumRows returns the number of logical rows in the layout.
func (l *WrapLayout) NumRows() int { return len(l.heights) }

// TotalLines returns the total number of screen lines.
func (l *WrapLayout) TotalLines() int { return l.total }

// RowStart returns the screen line index of the row's first line.
func (l *WrapLayout) RowStart(rowIdx int) int {
	if len(l.starts) == 0 {
		return 0
	}
	rowIdx = min(max(rowIdx, 0), len(l.starts)-1)
	return l.starts[rowIdx]
}

// RowHeight returns the number of screen lines the row occupies.
func (l *WrapLayout) RowHeight(rowIdx int) int {
	if len(l.heights) == 0 {
		return 1
	}
	rowIdx = min(max(rowIdx, 0), len(l.heights)-1)
	return l.heights[rowIdx]
}

// LineAt resolves a screen line index to its owning row and wrap offset,
// clamping out-of-range indexes to the first or last screen line.
func (l *WrapLayout) LineAt(screenIdx int) ScreenLine {
	if len(l.starts) == 0 {
		return ScreenLine{}
	}
	if screenIdx < 0 {
		screenIdx = 0
	}
	if screenIdx >= l.total {
		screenIdx = l.total - 1
	}
	// First row whose start is beyond screenIdx, minus one.
	row := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > screenIdx }) - 1
	if row < 0 {
		row = 0
	}
	return ScreenLine{RowIdx: row, WrapIdx: screenIdx - l.starts[row]}
}
