package app

// The character cursor: a rune-index column on the cursor row, always
// available while reading. It drives symbol-under-cursor lookups, renders as
// a one-cell span through the same MatchSpan pipeline as search highlights,
// and in side-by-side view crossing a line edge hops between the two cells
// (which is what keeps splitActiveLeft meaningful for symbol lookups and
// comment anchors).

import (
	"github.com/mattn/go-runewidth"

	"cride/internal/diff"
	"cride/internal/source"
	"cride/internal/ui"
)

// desiredEOL pins the cursor to line ends across vertical motion ($).
const desiredEOL = 1 << 30

// findMotion remembers the last f/F/t/T so ;/, can repeat it.
type findMotion struct {
	kind   byte
	target rune
}

// rowContentForSide returns the text a row shows on one side; ok is false
// when the row has no content there (headers, wrong-side lines, blank cells).
func rowContentForSide(row ui.Row, baseline bool) (string, bool) {
	switch row.Kind {
	case ui.RowLine:
		if baseline && row.Line.Kind == diff.LineAdd {
			return "", false
		}
		if !baseline && row.Line.Kind == diff.LineDelete {
			return "", false
		}
		return row.Line.Content, true
	case ui.RowPair:
		if baseline {
			if row.Left == nil {
				return "", false
			}
			return row.Left.Content, true
		}
		if row.Right == nil {
			return "", false
		}
		return row.Right.Content, true
	default:
		return "", false
	}
}

// rowLineNumberForSide returns the row's 1-based source line on one side, or
// 0 when the row has none there.
func rowLineNumberForSide(row ui.Row, baseline bool) int {
	line, ok := rowSideLine(row, baseline)
	if !ok {
		return 0
	}
	if baseline {
		return line.OldLine
	}
	return line.NewLine
}

// cursorRowSide reports which side of a row the character cursor lives on,
// snapping to the side that has content when the preferred one is blank.
// Unified delete rows are baseline-side; everything else is current-side.
func cursorRowSide(row ui.Row, preferLeft bool) (baseline bool) {
	switch row.Kind {
	case ui.RowPair:
		if preferLeft {
			return row.Left != nil
		}
		return row.Right == nil
	case ui.RowLine:
		return row.Line.Kind == diff.LineDelete
	default:
		return false
	}
}

// cursorContentWithRows returns the cursor row's active-side content.
func (m *Model) cursorContentWithRows(rows []ui.Row) ([]rune, bool) {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil, false
	}
	row := rows[m.cursor]
	content, ok := rowContentForSide(row, cursorRowSide(row, m.splitActiveLeft))
	if !ok {
		return nil, false
	}
	return []rune(content), true
}

// setCursorCol places the column and re-anchors the sticky desired column.
func (m *Model) setCursorCol(col int) {
	m.col = max(col, 0)
	m.desiredCol = m.col
}

// clampCursorColWithRows re-derives col from desiredCol for the cursor row.
// Called from clampScroll so every motion and jump lands on a valid column.
// Insert mode allows the one-past-end column, like vim.
func (m *Model) clampCursorColWithRows(rows []ui.Row) {
	runes, ok := m.cursorContentWithRows(rows)
	if !ok {
		m.col = 0
		return
	}
	maxCol := max(0, len(runes)-1)
	if m.mode == modeInsert {
		maxCol = len(runes)
	}
	target := m.desiredCol
	if target >= desiredEOL {
		target = maxCol
	}
	m.col = min(max(target, 0), maxCol)
}

// moveCursorCol moves the cursor delta runes within the row; in side-by-side
// view the cursor crosses between the two cells at the line edges.
func (m *Model) moveCursorCol(delta int) {
	rows := m.currentRows()
	runes, ok := m.cursorContentWithRows(rows)
	if !ok {
		return
	}
	next := m.col + delta
	if next < 0 {
		if m.crossPairSide(rows, true) {
			return
		}
		next = 0
	}
	maxCol := max(0, len(runes)-1)
	if next > maxCol {
		if m.crossPairSide(rows, false) {
			return
		}
		next = maxCol
	}
	m.setCursorCol(next)
}

// crossPairSide hops the cursor to the other cell of a pair row: leftward
// from column 0 lands on the left cell's line end, rightward from the line
// end lands on the right cell's start. Reports whether it moved.
func (m *Model) crossPairSide(rows []ui.Row, toLeft bool) bool {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return false
	}
	row := rows[m.cursor]
	if row.Kind != ui.RowPair {
		return false
	}
	baseline := cursorRowSide(row, m.splitActiveLeft)
	if toLeft {
		if baseline || row.Left == nil {
			return false
		}
		m.splitActiveLeft = true
		m.setCursorCol(max(0, len([]rune(row.Left.Content))-1))
		return true
	}
	if !baseline || row.Right == nil {
		return false
	}
	m.splitActiveLeft = false
	m.setCursorCol(0)
	return true
}

// cursorWordForward implements w: next word start, crossing rows.
func (m *Model) cursorWordForward(count int) {
	rows := m.currentRows()
	for ; count > 0; count-- {
		if !m.wordForwardOnce(rows) {
			return
		}
	}
}

func (m *Model) wordForwardOnce(rows []ui.Row) bool {
	if runes, ok := m.cursorContentWithRows(rows); ok {
		if next, found := nextWordStart(runes, m.col); found {
			m.setCursorCol(next)
			return true
		}
	}
	for i := m.cursor + 1; i < len(rows); i++ {
		if !rows[i].IsLineRow() {
			continue
		}
		m.cursor = i
		if runes, ok := m.cursorContentWithRows(rows); ok {
			m.setCursorCol(firstNonBlank(runes))
		} else {
			m.setCursorCol(0)
		}
		return true
	}
	return false
}

// cursorWordBackward implements b: previous word start, crossing rows.
func (m *Model) cursorWordBackward(count int) {
	rows := m.currentRows()
	for ; count > 0; count-- {
		if !m.wordBackwardOnce(rows) {
			return
		}
	}
}

func (m *Model) wordBackwardOnce(rows []ui.Row) bool {
	if runes, ok := m.cursorContentWithRows(rows); ok {
		if prev, found := prevWordStart(runes, m.col); found {
			m.setCursorCol(prev)
			return true
		}
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if !rows[i].IsLineRow() {
			continue
		}
		m.cursor = i
		if runes, ok := m.cursorContentWithRows(rows); ok {
			if prev, found := prevWordStart(runes, len(runes)); found {
				m.setCursorCol(prev)
				return true
			}
		}
		m.setCursorCol(0)
		return true
	}
	return false
}

// cursorWordEnd implements e (EDIT mode): next word end, crossing rows.
func (m *Model) cursorWordEnd(count int) {
	rows := m.currentRows()
	for ; count > 0; count-- {
		if runes, ok := m.cursorContentWithRows(rows); ok {
			if next, found := wordEndAfter(runes, m.col); found {
				m.setCursorCol(next)
				continue
			}
		}
		moved := false
		for i := m.cursor + 1; i < len(rows); i++ {
			if !rows[i].IsLineRow() {
				continue
			}
			m.cursor = i
			if rs, ok := m.cursorContentWithRows(rows); ok {
				if end, found := wordEndAfter(rs, -1); found {
					m.setCursorCol(end)
				} else {
					m.setCursorCol(0)
				}
			}
			moved = true
			break
		}
		if !moved {
			return
		}
	}
}

func (m *Model) cursorLineStart() {
	m.setCursorCol(0)
}

func (m *Model) cursorFirstNonBlank() {
	if runes, ok := m.cursorContentWithRows(m.currentRows()); ok {
		m.setCursorCol(firstNonBlank(runes))
	}
}

func (m *Model) cursorLineEnd() {
	runes, ok := m.cursorContentWithRows(m.currentRows())
	if !ok {
		return
	}
	m.col = max(0, len(runes)-1)
	m.desiredCol = desiredEOL
}

// findChar runs f/F/t/T and remembers it for ;/, repeats.
func (m *Model) findChar(kind byte, target rune, count int) {
	m.lastFind = findMotion{kind: kind, target: target}
	m.findCharMove(kind, target, count)
}

// repeatFindChar repeats the last f/F/t/T; reverse flips its direction (,).
func (m *Model) repeatFindChar(count int, reverse bool) {
	if m.lastFind.kind == 0 {
		return
	}
	kind := m.lastFind.kind
	if reverse {
		kind = reverseFindKind(kind)
	}
	m.findCharMove(kind, m.lastFind.target, count)
}

func reverseFindKind(kind byte) byte {
	switch kind {
	case 'f':
		return 'F'
	case 'F':
		return 'f'
	case 't':
		return 'T'
	case 'T':
		return 't'
	}
	return kind
}

// findCharMove is atomic like vim: a count that overruns the matches on the
// line moves nothing.
func (m *Model) findCharMove(kind byte, target rune, count int) {
	runes, ok := m.cursorContentWithRows(m.currentRows())
	if !ok {
		return
	}
	dir := 1
	if kind == 'F' || kind == 'T' {
		dir = -1
	}
	till := kind == 't' || kind == 'T'
	col := m.col
	for ; count > 0; count-- {
		next, found := findCharOnLine(runes, col, target, dir, till)
		if !found {
			return
		}
		col = next
	}
	m.setCursorCol(col)
}

// cursorMatchBracket implements %: find the bracket at or after the cursor on
// its row, then jump to the partner. Cross-row jumps land in the jumplist.
func (m *Model) cursorMatchBracket() {
	rows := m.currentRows()
	targetRow, targetCol, ok := m.bracketTarget(rows)
	if !ok {
		return
	}
	if targetRow != m.cursor {
		m.pushJump()
	}
	m.cursor = targetRow
	m.setCursorCol(targetCol)
}

// bracketTarget scans same-side rows for the partner bracket, stopping at
// source-line gaps (unexpanded context) so a match never silently skips
// hidden code.
func (m *Model) bracketTarget(rows []ui.Row) (rowIdx, col int, ok bool) {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0, 0, false
	}
	row := rows[m.cursor]
	baseline := cursorRowSide(row, m.splitActiveLeft)
	content, hasContent := rowContentForSide(row, baseline)
	if !hasContent {
		return 0, 0, false
	}
	runes := []rune(content)
	start, found := firstBracketFrom(runes, m.col)
	if !found {
		return 0, 0, false
	}
	open := runes[start]
	match, forward, _ := bracketFor(open)
	dir := 1
	if !forward {
		dir = -1
	}

	depth := 0
	i := m.cursor
	pos := start
	prevLine := rowLineNumberForSide(row, baseline)
	for {
		for ; pos >= 0 && pos < len(runes); pos += dir {
			switch runes[pos] {
			case open:
				depth++
			case match:
				depth--
				if depth == 0 {
					return i, pos, true
				}
			}
		}
		next := i + dir
		for ; next >= 0 && next < len(rows); next += dir {
			content, hasContent := rowContentForSide(rows[next], baseline)
			if !hasContent {
				continue
			}
			line := rowLineNumberForSide(rows[next], baseline)
			if line != 0 && prevLine != 0 && line != prevLine+dir {
				return 0, 0, false
			}
			i, prevLine = next, line
			runes = []rune(content)
			pos = 0
			if dir < 0 {
				pos = len(runes) - 1
			}
			break
		}
		if next < 0 || next >= len(rows) {
			return 0, 0, false
		}
	}
}

// cursorSpan renders the character cursor as a one-cell span on the cursor
// row. It is appended after search/symbol spans so it stays visible on top,
// and hidden while the change list has focus or the inline symbol choice
// owns the row highlight.
func (m Model) cursorSpan() []ui.MatchSpan {
	if m.focus != paneDiff || m.overlay.Kind == OverlaySymbolChoice {
		return nil
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	row := rows[m.cursor]
	if !row.IsLineRow() {
		return nil
	}
	baseline := cursorRowSide(row, m.splitActiveLeft)
	content, ok := rowContentForSide(row, baseline)
	if !ok {
		return nil
	}
	runes := []rune(content)
	col := min(max(m.col, 0), max(0, len(runes)-1))
	start := tabExpandedWidth(string(runes[:min(col, len(runes))]))
	cellWidth := 1
	if col < len(runes) {
		if runes[col] == '\t' {
			cellWidth = 4
		} else {
			cellWidth = max(1, runewidth.RuneWidth(runes[col]))
		}
	}
	side := ui.MatchSideUnified
	if row.Kind == ui.RowPair {
		side = ui.MatchSideRight
		if baseline {
			side = ui.MatchSideLeft
		}
	}
	return []ui.MatchSpan{{RowIdx: m.cursor, Start: start, End: start + cellWidth, Cursor: true, Side: side}}
}

// alignCursorColToLocation places the column after a jump. Only exact line
// landings adopt the location's column; nearest-line fallbacks reset to 0.
func (m *Model) alignCursorColToLocation(row ui.Row, loc source.Location, baseline, exact bool) {
	if exact && loc.Column > 1 {
		if content, ok := rowContentForSide(row, baseline); ok {
			if idx, ok := runeIndexAtByteColumn(content, loc.Column); ok {
				m.setCursorCol(idx)
				return
			}
		}
	}
	m.setCursorCol(0)
}

// runeIndexAtByteColumn converts a 1-based byte column into the rune index of
// the rune covering that byte; ok is false when the column is out of range.
func runeIndexAtByteColumn(content string, column int) (int, bool) {
	offset := column - 1
	if offset < 0 || offset >= len(content) {
		return 0, false
	}
	idx := 0
	for i := range content {
		if i == offset {
			return idx, true
		}
		if i > offset {
			// offset points into the previous rune's continuation bytes.
			return max(0, idx-1), true
		}
		idx++
	}
	return max(0, idx-1), true
}

// byteColumnAtRune converts a rune index into the 1-based byte column where
// that rune starts; ok is false when the index is past the content.
func byteColumnAtRune(content string, runeIdx int) (int, bool) {
	if runeIdx < 0 {
		return 0, false
	}
	idx := 0
	for i := range content {
		if idx == runeIdx {
			return i + 1, true
		}
		idx++
	}
	return 0, false
}
