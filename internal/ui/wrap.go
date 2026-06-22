package ui

import (
	"sort"

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
// Highlighting never changes printable content, so heights are computed from
// the unhighlighted render of each row; the styled render wraps identically.
func BuildWrapLayout(files []diff.FileDiff, rows []Row, width int) *WrapLayout {
	l := &WrapLayout{
		Width:   width,
		heights: make([]int, len(rows)),
		starts:  make([]int, len(rows)),
	}
	for i, r := range rows {
		h := len(rowScreenLines(files, r, nil, i, 0, width))
		if h < 1 {
			h = 1
		}
		l.starts[i] = l.total
		l.heights[i] = h
		l.total += h
	}
	return l
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
