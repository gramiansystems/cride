package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"cride/internal/diff"
	"cride/internal/highlight"
)

// Renderer reuses the wrapping and styling of rows retained in the viewport.
// Its zero value is ready to use. One instance belongs to one view; rendering
// is serial, as are changes to the package theme.
//
// Cache identity and invalidation stay here: callers only supply their normal
// render inputs. Entries outside the current viewport are discarded each frame.
type Renderer struct {
	key     renderKey
	entries map[int]*renderedRow
}

type renderKey struct {
	rows  *Row
	count int
	width int
	hl    *highlight.Highlighter
	theme Theme
}

type rowFile struct {
	path           string
	status         diff.FileStatus
	added, deleted int
}

type renderedRow struct {
	row         Row
	left, right diff.Line
	file        rowFile
	relative    int // checked only when the gutter itself wraps

	base   []string
	styled []string
	bg     lipgloss.Color

	replaceRelative bool
	prefix, suffix  string
}

func (r *Renderer) begin(rows []Row, width int, hl *highlight.Highlighter) {
	if r == nil {
		return
	}
	key := renderKey{count: len(rows), width: width, hl: hl, theme: CurrentTheme()}
	if len(rows) > 0 {
		key.rows = &rows[0]
	}
	if r.entries == nil || r.key != key {
		r.key = key
		r.entries = make(map[int]*renderedRow)
	}
}

func (r *Renderer) finish(first, end int) {
	if r == nil {
		return
	}
	for index := range r.entries {
		if index < first || index >= end {
			delete(r.entries, index)
		}
	}
}

func (r *Renderer) row(files []diff.FileDiff, row Row, hl *highlight.Highlighter, index, cursor, width int, bg lipgloss.Color) *renderedRow {
	var file rowFile
	if row.FileIdx >= 0 && row.FileIdx < len(files) {
		file.path = files[row.FileIdx].Path()
	}
	if row.Kind == RowFileHeader {
		f := files[row.FileIdx]
		file.status, file.added, file.deleted = f.Status, f.Added, f.Deleted
	}
	var left, right diff.Line
	if row.Left != nil {
		left = *row.Left
	}
	if row.Right != nil {
		right = *row.Right
	}
	// Keep the changing relative number out of the cached row. On very narrow
	// panes the gutter itself wraps, so use the regular rendering path instead.
	replaceRelative := row.IsLineRow() && width >= blameGutterWidth(row)+pairRelWidth
	relative := index - cursor
	if relative < 0 {
		relative = -relative
	}
	relative = min(999, relative)
	var cached *renderedRow
	if r != nil {
		cached = r.entries[index]
	}
	if cached == nil || cached.row != row || cached.left != left || cached.right != right || cached.file != file || (row.IsLineRow() && !cached.replaceRelative && cached.relative != relative) {
		baseIndex, baseCursor := index, cursor
		if replaceRelative {
			baseIndex, baseCursor = 0, 0
		}
		cached = &renderedRow{
			row: row, left: left, right: right, file: file, relative: relative,
			base:            rowScreenLines(files, row, hl, baseIndex, baseCursor, width),
			replaceRelative: replaceRelative,
		}
	}
	if !cached.style(width, bg) {
		// Unicode blame text can make the wrapping library split even a gutter
		// that fits its measured width. Preserve the regular renderer's output
		// whenever there is no intact relative-number slot on the first line.
		cached.replaceRelative = false
		cached.base = rowScreenLines(files, row, hl, index, cursor, width)
		cached.styled = nil
		cached.style(width, bg)
	}
	if r != nil {
		r.entries[index] = cached
	}
	return cached
}

func (r *renderedRow) style(width int, bg lipgloss.Color) bool {
	if r.styled != nil && r.bg == bg {
		return true
	}
	r.bg = bg
	r.styled = make([]string, len(r.base))
	for i, line := range r.base {
		line = padRight(line, width)
		if bg != "" {
			line = withPersistentBackground(line, bg)
			line = lipgloss.NewStyle().Background(bg).Width(width).MaxWidth(width).Render(line)
		}
		r.styled[i] = line
	}
	if r.replaceRelative {
		var ok bool
		r.prefix, r.suffix, ok = splitRelativeGutter(r.styled[0], blameGutterWidth(r.row))
		return ok
	}
	return true
}

func (r *renderedRow) line(wrapIndex, rowIndex, cursor int) string {
	if wrapIndex != 0 || !r.replaceRelative {
		return r.styled[wrapIndex]
	}
	return r.prefix + relativeNumberText(rowIndex, cursor) + r.suffix
}

// The relative gutter is three contiguous ASCII cells inside the row's SGR
// styling. Locate those cells once, then change just their digits each frame;
// retaining the surrounding escape sequences also avoids extra terminal I/O.
func splitRelativeGutter(line string, startColumn int) (prefix, suffix string, ok bool) {
	column := 0
	var state byte
	for offset := 0; offset < len(line); {
		_, width, size, next := ansi.DecodeSequence(line[offset:], state, nil)
		if width > 0 && column == startColumn {
			end := offset + relativeNumWidth
			if end <= len(line) && line[offset:end] == "  0" {
				return line[:offset], line[end:], true
			}
			return "", "", false
		}
		column += width
		offset += size
		state = next
	}
	return "", "", false
}

func rowBackground(row Row, selected, activeHunk bool) lipgloss.Color {
	if selected {
		return colorCursor
	}
	if bg, ok := changeBgColor(row); ok {
		return bg
	}
	if activeHunk && !(row.Kind == RowPair && PairRowHasChange(row)) {
		return colorHunkBg
	}
	return ""
}
