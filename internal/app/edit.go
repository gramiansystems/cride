package app

// Vim-style editing of the current working tree, scoped to current-side
// lines (docs/review-expansion-and-editing.md). Three modes:
//
//	REVIEW — the default; every review binding keeps its meaning.
//	EDIT   — vim normal mode, entered via i/a/I (which continue to INSERT);
//	         review bindings are suspended so x/o/A/dd/cc/e carry their
//	         canonical vim meanings without collisions.
//	INSERT — literal typing into the buffer; esc returns to EDIT.
//
// The buffer is fileContents[path].lines — the same slice full-file view
// renders from — so editing is entering full expansion and mutating it.
// Mutations always build fresh slices (never in place), which makes undo
// snapshots free slice references. ZZ writes the buffer to the working tree
// and ZQ discards it, both returning to REVIEW. While EDIT/INSERT is active
// an advisory lock at .cride/editing.json tells the agent to make way, and
// tree reloads are deferred so an external write cannot wipe the buffer
// mid-edit; they run on exit.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

type editorMode int

const (
	modeReview editorMode = iota
	modeEdit
	modeInsert
)

const editUndoMax = 200

// editSnapshot is one undo/redo state: the whole line slice (strings are
// shared; mutations never edit slices in place) plus the cursor position.
type editSnapshot struct {
	lines []string
	line  int // 1-based buffer line under the cursor
	col   int
}

// editRegister is the single unnamed register d/c/x/y fill and p/P read.
type editRegister struct {
	lines    []string
	linewise bool
}

// --- mode entry -------------------------------------------------------------

// enterEditMode handles i/a/I from REVIEW: full expansion on, buffer loaded
// (async if needed), lock acquired, then INSERT at the cursor's source line.
func (m *Model) enterEditMode(kind byte) tea.Cmd {
	if m.focus == paneList || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	f := m.files[m.selectedFile]
	switch {
	case f.Binary:
		return m.notify(ui.ToastWarn, "cannot edit a binary file")
	case f.Status == diff.FileDeleted:
		return m.notify(ui.ToastWarn, "cannot edit a deleted file")
	case f.Path() == "":
		return nil
	}
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only row — move to a current-side line to edit")
	}
	col := m.col
	screenRow := m.cursorScreenRow()
	if m.viewMode != ViewFile {
		m.editPrevView = m.viewMode
		m.saveCurrentFileState()
		m.viewMode = ViewFile
		// The cursor re-anchors to srcLine below instead of restoring the
		// file's remembered full-view position.
	} else {
		m.editPrevView = ViewFile
	}
	cmd := m.ensureCurrentFileContentCmd()
	if _, ok := m.editBufferLines(); !ok {
		m.pendingEditKind = kind
		m.pendingEditLine = srcLine
		m.pendingEditCol = col
		m.pendingEditRow = screenRow
		return cmd
	}
	return tea.Batch(cmd, m.completeEditEntry(kind, srcLine, col, screenRow))
}

// completeEditEntry finishes mode entry once the buffer is available.
func (m *Model) completeEditEntry(kind byte, srcLine, col, screenRow int) tea.Cmd {
	rows := m.currentRows()
	lines, _ := m.editBufferLines()
	if idx, ok := rowIndexForNewLine(rows, srcLine); ok {
		m.cursor = idx
	}
	m.setCursorCol(col)
	m.mode = modeEdit
	m.editOriginal = lines
	m.editDirty = false
	m.editUndo, m.editRedo = nil, nil
	m.acquireEditLock(m.currentFilePath())
	var cmd tea.Cmd
	switch kind {
	case 'i':
		cmd = m.startInsert(insertAtCursor)
	case 'I':
		m.cursorFirstNonBlank()
		cmd = m.startInsert(insertAtCursor)
	case 'a':
		cmd = m.startInsert(insertAfterCursor)
	}
	// Switching from compact diff rows to full-file rows can move the same
	// source line a long way down the row slice. Preserve its visual position
	// instead of letting clampScroll put it at the bottom of the viewport.
	m.clampScroll()
	m.scrollCursorToScreenRowAllowingEOFSpace(screenRow)
	return cmd
}

const (
	insertAtCursor = iota
	insertAfterCursor
	insertAtLineEnd
)

// startInsert switches EDIT → INSERT, adjusting the column for a/A. The undo
// snapshot taken here groups the whole insert session as one undo unit.
func (m *Model) startInsert(adjust int) tea.Cmd {
	rows := m.currentRows()
	if _, ok := m.editableCursorLine(rows); !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	m.pushEditUndo()
	runes, _ := m.cursorContentWithRows(rows)
	switch adjust {
	case insertAfterCursor:
		m.col = min(m.col+1, len(runes))
		m.desiredCol = m.col
	case insertAtLineEnd:
		m.col = len(runes)
		m.desiredCol = m.col
	}
	m.mode = modeInsert
	return nil
}

// exitEditMode returns to REVIEW, restoring the pre-edit view and running any
// reload deferred while the buffer was open.
func (m *Model) exitEditMode() tea.Cmd {
	m.mode = modeReview
	m.pendingOp, m.pendingFind = 0, 0
	m.pendingOpCount, m.pendingReplace = 0, 0
	m.pendingZUpper = false
	m.editUndo, m.editRedo = nil, nil
	m.editOriginal = nil
	m.releaseEditLock()
	if m.editPrevView != ViewFile {
		m.saveCurrentFileState()
		m.viewMode = m.editPrevView
		m.restoreCurrentFileState()
	}
	var cmds []tea.Cmd
	if cmd := m.ensureCurrentFileContentCmd(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if m.treeChanged {
		cmds = append(cmds, m.reload(false))
	}
	m.clampScroll()
	return tea.Batch(cmds...)
}

// --- key handling -----------------------------------------------------------

// handleEditKey is vim normal mode over the edit buffer. It works from an
// explicit whitelist: review bindings never leak in.
func (m Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	m.clearStickyToast()

	if m.pendingFind != 0 {
		kind := m.pendingFind
		m.pendingFind = 0
		if target, ok := findTargetRune(msg); ok {
			count, _ := m.consumeCount()
			m.findChar(kind, target, count)
			m.clampScroll()
		} else {
			m.countBuf = ""
		}
		return m, nil
	}

	if m.pendingReplace != 0 {
		count := m.pendingReplace
		m.pendingReplace = 0
		if target, ok := replacementTargetRune(msg); ok {
			cmd := m.replaceCharacters(target, count)
			m.clampScroll()
			return m, cmd
		}
		return m, nil
	}

	if m.pendingZUpper {
		m.pendingZUpper = false
		switch k {
		case "Z":
			return m, m.executeCommand(commandSaveEdits, 1, false)
		case "Q":
			return m, m.executeCommand(commandDiscardEdits, 1, false)
		}
		return m, nil
	}

	if m.pendingOp != 0 {
		if m.captureCount(k) {
			return m, nil
		}
		op := m.pendingOp
		m.pendingOp = 0
		motionCount, _ := m.consumeCount()
		count := multiplyEditCounts(max(1, m.pendingOpCount), motionCount)
		m.pendingOpCount = 0
		if k == string(op) {
			id := map[byte]string{
				'c': commandChangeLine,
				'd': commandDeleteLine,
				'y': commandYankLine,
			}[op]
			return m, m.executeCommand(id, count, true)
		}
		cmd := m.applyOperator(op, k, count)
		m.clampScroll()
		return m, cmd
	}

	if m.captureCount(k) {
		return m, nil
	}
	count, hasCount := m.consumeCount()

	switch k {
	case "Z":
		m.pendingZUpper = true
		return m, nil
	}
	id := map[string]string{
		"ctrl+c": commandQuit,
		"esc":    commandExitEditMode,
		"i":      commandEditInsert,
		"I":      commandEditInsertLineStart,
		"a":      commandEditAppend,
		"A":      commandEditAppendLineEnd,
		"o":      commandEditOpenLineBelow,
		"O":      commandEditOpenLineAbove,
		"d":      commandEditDelete,
		"c":      commandEditChange,
		"y":      commandEditYank,
		"r":      commandEditReplace,
		"s":      commandEditSubstitute,
		"S":      commandEditSubstituteLine,
		"x":      commandDeleteCharacter,
		"D":      commandDeleteToLineEnd,
		"C":      commandChangeToLineEnd,
		"p":      commandEditPasteAfter,
		"P":      commandEditPasteBefore,
		"J":      commandJoinLines,
		"u":      commandEditUndo,
		"ctrl+r": commandEditRedo,
		"j":      commandCursorDown,
		"down":   commandCursorDown,
		"k":      commandCursorUp,
		"up":     commandCursorUp,
		"h":      commandCursorLeft,
		"left":   commandCursorLeft,
		"l":      commandCursorRight,
		"right":  commandCursorRight,
		"w":      commandCursorWordForward,
		"b":      commandCursorWordBackward,
		"e":      commandCursorWordEnd,
		"0":      commandCursorLineStart,
		"^":      commandCursorFirstNonBlank,
		"$":      commandCursorLineEnd,
		"%":      commandJumpMatchingBracket,
		"f":      commandFindForward,
		"F":      commandFindBackward,
		"t":      commandTillForward,
		"T":      commandTillBackward,
		";":      commandRepeatFindForward,
		",":      commandRepeatFindBackward,
		"ctrl+d": commandScrollHalfPageDown,
		"ctrl+u": commandScrollHalfPageUp,
		"pgdown": commandScrollPageDown,
		"ctrl+f": commandScrollPageDown,
		"pgup":   commandScrollPageUp,
		"ctrl+b": commandScrollPageUp,
		"H":      commandMoveViewportTop,
		"L":      commandMoveViewportBottom,
		"home":   commandGoToFileTop,
		"G":      commandGoToFileBottom,
		"end":    commandGoToFileBottom,
	}[k]
	if k == "G" && hasCount {
		id = commandJumpSourceLine
	}
	return m, m.executeCommand(id, count, hasCount)
}

// handleInsertKey types literally into the buffer.
func (m Model) handleInsertKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeEdit
		m.setCursorCol(max(0, m.col-1)) // vim steps back onto the last typed rune
		m.clampScroll()
		return m, nil
	case tea.KeyEnter:
		return m, m.insertLineBreak()
	case tea.KeyBackspace:
		return m, m.insertBackspace()
	case tea.KeyDelete:
		return m, m.insertDelete()
	case tea.KeyLeft:
		m.moveInsertHorizontal(-1)
		return m, nil
	case tea.KeyRight:
		m.moveInsertHorizontal(1)
		return m, nil
	case tea.KeyUp:
		m.moveInsertVertical(-1)
		return m, nil
	case tea.KeyDown:
		m.moveInsertVertical(1)
		return m, nil
	case tea.KeyHome:
		m.setCursorCol(0)
		return m, nil
	case tea.KeyEnd:
		if runes, ok := m.cursorContentWithRows(m.currentRows()); ok {
			m.setCursorCol(len(runes))
		}
		return m, nil
	case tea.KeyTab:
		return m, m.insertText("\t")
	case tea.KeySpace:
		return m, m.insertText(" ")
	case tea.KeyRunes:
		return m, m.insertText(string(msg.Runes))
	}
	return m, nil
}

// --- buffer access ----------------------------------------------------------

// editBufferLines returns the loaded current-file buffer.
func (m *Model) editBufferLines() ([]string, bool) {
	state, ok := m.fileContents[m.currentFilePath()]
	if !ok || !state.loaded || state.err != nil {
		return nil, false
	}
	return state.lines, true
}

// setEditBufferLines stores a fresh line slice and marks the buffer dirty.
func (m *Model) setEditBufferLines(lines []string) {
	path := m.currentFilePath()
	state := m.fileContents[path]
	state.lines = lines
	m.fileContents[path] = state
	m.editDirty = !slices.Equal(lines, m.editOriginal)
	m.rowsVersion++
}

// editableCursorLine returns the 1-based buffer line the cursor row maps to;
// ok is false on read-only rows (baseline deletions, headers, comments).
func (m *Model) editableCursorLine(rows []ui.Row) (int, bool) {
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0, false
	}
	row := rows[m.cursor]
	if row.Kind != ui.RowLine || row.Line.Kind == diff.LineDelete || row.Line.NewLine <= 0 {
		return 0, false
	}
	return row.Line.NewLine, true
}

// nearestEditableLine finds the closest current-side line to the cursor, for
// o/O started on a read-only row (vision doc: insert at the nearest valid
// current-side position).
func (m *Model) nearestEditableLine(rows []ui.Row) (int, bool) {
	for delta := 1; delta < len(rows); delta++ {
		for _, idx := range [2]int{m.cursor + delta, m.cursor - delta} {
			if idx < 0 || idx >= len(rows) {
				continue
			}
			row := rows[idx]
			if row.Kind == ui.RowLine && row.Line.Kind != diff.LineDelete && row.Line.NewLine > 0 {
				return row.Line.NewLine, true
			}
		}
	}
	return 0, false
}

// rowIndexForNewLine finds the row rendering buffer line n.
func rowIndexForNewLine(rows []ui.Row, n int) (int, bool) {
	for i, row := range rows {
		if row.Kind == ui.RowLine && row.Line.Kind != diff.LineDelete && row.Line.NewLine == n {
			return i, true
		}
	}
	return 0, false
}

// positionEditCursor moves the cursor to buffer line n (1-based) column col.
func (m *Model) positionEditCursor(n, col int) {
	rows := m.currentRows()
	if idx, ok := rowIndexForNewLine(rows, n); ok {
		m.cursor = idx
	}
	m.setCursorCol(col)
}

// --- undo / redo ------------------------------------------------------------

func (m *Model) currentEditSnapshot() (editSnapshot, bool) {
	lines, ok := m.editBufferLines()
	if !ok {
		return editSnapshot{}, false
	}
	line, _ := m.editableCursorLine(m.currentRows())
	return editSnapshot{lines: lines, line: max(1, line), col: m.col}, true
}

// pushEditUndo records the pre-mutation state and invalidates redo.
func (m *Model) pushEditUndo() {
	snap, ok := m.currentEditSnapshot()
	if !ok {
		return
	}
	m.editUndo = append(m.editUndo, snap)
	if len(m.editUndo) > editUndoMax {
		m.editUndo = m.editUndo[len(m.editUndo)-editUndoMax:]
	}
	m.editRedo = nil
}

func (m *Model) popEditUndo(count int) tea.Cmd {
	for ; count > 0; count-- {
		if len(m.editUndo) == 0 {
			return m.notify(ui.ToastInfo, "already at the oldest change")
		}
		if cur, ok := m.currentEditSnapshot(); ok {
			m.editRedo = append(m.editRedo, cur)
		}
		snap := m.editUndo[len(m.editUndo)-1]
		m.editUndo = m.editUndo[:len(m.editUndo)-1]
		m.restoreEditSnapshot(snap)
	}
	return nil
}

func (m *Model) popEditRedo(count int) tea.Cmd {
	for ; count > 0; count-- {
		if len(m.editRedo) == 0 {
			return m.notify(ui.ToastInfo, "already at the newest change")
		}
		if cur, ok := m.currentEditSnapshot(); ok {
			m.editUndo = append(m.editUndo, cur)
		}
		snap := m.editRedo[len(m.editRedo)-1]
		m.editRedo = m.editRedo[:len(m.editRedo)-1]
		m.restoreEditSnapshot(snap)
	}
	return nil
}

func (m *Model) restoreEditSnapshot(snap editSnapshot) {
	path := m.currentFilePath()
	state := m.fileContents[path]
	state.lines = snap.lines
	m.fileContents[path] = state
	m.editDirty = !slices.Equal(snap.lines, m.editOriginal)
	m.rowsVersion++
	m.positionEditCursor(min(snap.line, max(1, len(snap.lines))), snap.col)
}

// multiplyEditCounts composes counts before and after an operator (2d3w),
// saturating on the extremely unlikely integer overflow instead of wrapping.
func multiplyEditCounts(operator, motion int) int {
	maxInt := int(^uint(0) >> 1)
	if motion > 0 && operator > maxInt/motion {
		return maxInt
	}
	return operator * motion
}

// --- operators --------------------------------------------------------------

// applyOperator runs d/c/y over a motion target: the doubled key is linewise
// (dd/cc/yy), everything else resolves to a same-line character range.
func (m *Model) applyOperator(op byte, motion string, count int) tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	if idx < 0 || idx >= len(lines) {
		return nil
	}

	if motion == string(op) {
		return m.applyLinewiseOperator(op, lines, idx, count)
	}

	runes := []rune(lines[idx])
	from, to, ok := charwiseRange(runes, m.col, motion, count)
	if !ok {
		return nil
	}
	removed := string(runes[from:to])
	m.editRegister = editRegister{lines: []string{removed}, linewise: false}
	if op == 'y' {
		m.setCursorCol(from)
		return nil
	}
	m.pushEditUndo()
	rest := string(runes[:from]) + string(runes[to:])
	m.setEditBufferLines(replaceLine(lines, idx, rest))
	if op == 'c' {
		m.positionEditCursor(srcLine, from)
		m.col = from // may equal the new line length; insert mode allows it
		m.desiredCol = m.col
		m.mode = modeInsert
		return nil
	}
	m.positionEditCursor(srcLine, min(from, max(0, len([]rune(rest))-1)))
	return nil
}

func (m *Model) applyLinewiseOperator(op byte, lines []string, idx, count int) tea.Cmd {
	n := min(count, len(lines)-idx)
	removed := append([]string(nil), lines[idx:idx+n]...)
	m.editRegister = editRegister{lines: removed, linewise: true}
	if op == 'y' {
		return nil
	}
	m.pushEditUndo()
	rest := make([]string, 0, len(lines)-n)
	rest = append(rest, lines[:idx]...)
	rest = append(rest, lines[idx+n:]...)
	if op == 'c' {
		rest = insertLineSlice(rest, idx, []string{""})
		m.setEditBufferLines(rest)
		m.positionEditCursor(idx+1, 0)
		m.mode = modeInsert
		return nil
	}
	if len(rest) == 0 {
		rest = []string{""}
	}
	m.setEditBufferLines(rest)
	target := min(idx+1, len(rest))
	m.positionEditCursor(target, 0)
	m.cursorFirstNonBlank()
	return nil
}

// substituteCharacters implements s/S's characterwise half. On an empty
// line, s still enters INSERT, matching the useful vim behavior.
func (m *Model) substituteCharacters(count int) tea.Cmd {
	runes, ok := m.cursorContentWithRows(m.currentRows())
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	if len(runes) == 0 || m.col >= len(runes) {
		return m.startInsert(insertAtCursor)
	}
	return m.applyOperator('c', "l", count)
}

// replaceCharacters implements r{char}, replacing up to count runes without
// changing the line length or entering INSERT.
func (m *Model) replaceCharacters(target rune, count int) tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	runes := []rune(lines[idx])
	if m.col < 0 || m.col >= len(runes) {
		return nil
	}
	n := min(max(1, count), len(runes)-m.col)
	m.pushEditUndo()
	for i := 0; i < n; i++ {
		runes[m.col+i] = target
	}
	m.setEditBufferLines(replaceLine(lines, idx, string(runes)))
	m.positionEditCursor(srcLine, m.col+n-1)
	return nil
}

func replacementTargetRune(msg tea.KeyMsg) (rune, bool) {
	if msg.Type == tea.KeyTab {
		return '\t', true
	}
	return findTargetRune(msg)
}

// charwiseRange maps a motion key to the [from, to) rune range it covers on
// one line. Same-line motions only; unknown motions report !ok.
func charwiseRange(runes []rune, col int, motion string, count int) (from, to int, ok bool) {
	n := len(runes)
	col = min(max(col, 0), n)
	switch motion {
	case "l", "right", " ":
		return col, min(col+count, n), col < n
	case "h", "left":
		return max(0, col-count), col, col > 0
	case "w":
		end := col
		for i := 0; i < count; i++ {
			next, found := nextWordStart(runes, end)
			if !found {
				end = n
				break
			}
			end = next
		}
		return col, end, end > col
	case "e":
		end := col
		for i := 0; i < count; i++ {
			next, found := wordEndAfter(runes, end)
			if !found {
				return 0, 0, false
			}
			end = next
		}
		return col, end + 1, true
	case "b":
		start := col
		for i := 0; i < count; i++ {
			prev, found := prevWordStart(runes, start)
			if !found {
				break
			}
			start = prev
		}
		return start, col, start < col
	case "$":
		return col, n, col < n
	case "0":
		return 0, col, col > 0
	case "^":
		fnb := firstNonBlank(runes)
		return min(fnb, col), max(fnb, col), fnb != col
	default:
		return 0, 0, false
	}
}

// --- line open, paste -------------------------------------------------------

// openLine implements o/O: a new empty line below/above the cursor line (or
// the nearest editable line when the cursor sits on a read-only row).
func (m *Model) openLine(above bool) tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		srcLine, ok = m.nearestEditableLine(rows)
		if !ok {
			return m.notify(ui.ToastWarn, "no editable line nearby")
		}
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	m.pushEditUndo()
	at := srcLine // 0-based insertion index == below srcLine
	if above {
		at = srcLine - 1
	}
	m.setEditBufferLines(insertLineSlice(lines, at, []string{""}))
	m.positionEditCursor(at+1, 0)
	m.mode = modeInsert
	return nil
}

// pasteRegister implements p/P.
func (m *Model) pasteRegister(before bool, count int) tea.Cmd {
	if len(m.editRegister.lines) == 0 {
		return m.notify(ui.ToastInfo, "nothing yanked")
	}
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	m.pushEditUndo()
	if m.editRegister.linewise {
		var block []string
		for i := 0; i < count; i++ {
			block = append(block, m.editRegister.lines...)
		}
		at := idx + 1
		if before {
			at = idx
		}
		m.setEditBufferLines(insertLineSlice(lines, at, block))
		m.positionEditCursor(at+1, 0)
		m.cursorFirstNonBlank()
		return nil
	}
	text := strings.Repeat(m.editRegister.lines[0], count)
	if text == "" {
		return nil
	}
	runes := []rune(lines[idx])
	at := min(m.col+1, len(runes))
	if before {
		at = min(m.col, len(runes))
	}
	next := string(runes[:at]) + text + string(runes[at:])
	m.setEditBufferLines(replaceLine(lines, idx, next))
	m.positionEditCursor(srcLine, at+len([]rune(text))-1)
	return nil
}

// joinLines implements J: join the current line with count-1 following lines,
// trimming boundary whitespace and inserting one separating space.
func (m *Model) joinLines(count int) tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	n := min(max(2, count), len(lines)-idx)
	if n < 2 {
		return m.notify(ui.ToastInfo, "already at the last line")
	}
	joined := strings.TrimRight(lines[idx], " \t")
	joinCol := len([]rune(joined))
	for _, line := range lines[idx+1 : idx+n] {
		next := strings.TrimLeft(line, " \t")
		if joined != "" && next != "" {
			joined += " "
		}
		joined += next
	}
	m.pushEditUndo()
	next := make([]string, 0, len(lines)-n+1)
	next = append(next, lines[:idx]...)
	next = append(next, joined)
	next = append(next, lines[idx+n:]...)
	m.setEditBufferLines(next)
	m.positionEditCursor(srcLine, max(0, joinCol))
	return nil
}

// --- insert-mode mutations ----------------------------------------------------

// Insert-mode cursor motions use insertion-point columns (0..line length),
// unlike normal-mode motions whose maximum is the last rune.
func (m *Model) moveInsertHorizontal(delta int) {
	if runes, ok := m.cursorContentWithRows(m.currentRows()); ok {
		m.setCursorCol(min(max(m.col+delta, 0), len(runes)))
	}
}

func (m *Model) moveInsertVertical(delta int) {
	line, ok := m.editableCursorLine(m.currentRows())
	lines, loaded := m.editBufferLines()
	if !ok || !loaded || len(lines) == 0 {
		return
	}
	target := min(max(line+delta, 1), len(lines))
	desired := m.desiredCol
	m.positionEditCursor(target, desired)
	m.desiredCol = desired
	m.clampScroll()
}

// insertText types text at the cursor, honoring embedded line breaks (paste).
func (m *Model) insertText(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, part := range parts {
		if i > 0 {
			if cmd := m.insertLineBreak(); cmd != nil {
				return cmd
			}
		}
		if part == "" {
			continue
		}
		if cmd := m.insertPlainText(strings.ReplaceAll(part, "\r", "")); cmd != nil {
			return cmd
		}
	}
	return nil
}

func (m *Model) insertPlainText(text string) tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	runes := []rune(lines[idx])
	at := min(max(m.col, 0), len(runes))
	next := string(runes[:at]) + text + string(runes[at:])
	m.setEditBufferLines(replaceLine(lines, idx, next))
	m.col = at + len([]rune(text))
	m.desiredCol = m.col
	m.clampScroll()
	return nil
}

func (m *Model) insertLineBreak() tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return m.notify(ui.ToastWarn, "read-only line")
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	runes := []rune(lines[idx])
	at := min(max(m.col, 0), len(runes))
	next := replaceLine(lines, idx, string(runes[:at]))
	next = insertLineSlice(next, idx+1, []string{string(runes[at:])})
	m.setEditBufferLines(next)
	m.positionEditCursor(srcLine+1, 0)
	m.clampScroll()
	return nil
}

func (m *Model) insertBackspace() tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return nil
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	runes := []rune(lines[idx])
	if m.col > 0 {
		at := min(m.col, len(runes))
		next := string(runes[:at-1]) + string(runes[at:])
		m.setEditBufferLines(replaceLine(lines, idx, next))
		m.col = at - 1
		m.desiredCol = m.col
		m.clampScroll()
		return nil
	}
	if idx == 0 {
		return nil
	}
	prev := []rune(lines[idx-1])
	merged := string(prev) + string(runes)
	next := replaceLine(lines, idx-1, merged)
	next = append(next[:idx], next[idx+1:]...)
	m.setEditBufferLines(next)
	m.positionEditCursor(srcLine-1, len(prev))
	m.col = len(prev) // may equal line length in insert mode
	m.desiredCol = m.col
	m.clampScroll()
	return nil
}

func (m *Model) insertDelete() tea.Cmd {
	rows := m.currentRows()
	srcLine, ok := m.editableCursorLine(rows)
	if !ok {
		return nil
	}
	lines, ok := m.editBufferLines()
	if !ok {
		return nil
	}
	idx := srcLine - 1
	runes := []rune(lines[idx])
	if m.col < len(runes) {
		next := string(runes[:m.col]) + string(runes[m.col+1:])
		m.setEditBufferLines(replaceLine(lines, idx, next))
		return nil
	}
	if idx+1 >= len(lines) {
		return nil
	}
	merged := string(runes) + lines[idx+1]
	next := replaceLine(lines, idx, merged)
	next = append(next[:idx+1], next[idx+2:]...)
	m.setEditBufferLines(next)
	return nil
}

// --- slice primitives (always fresh slices; undo depends on it) --------------

func replaceLine(lines []string, idx int, text string) []string {
	next := make([]string, len(lines))
	copy(next, lines)
	next[idx] = text
	return next
}

func insertLineSlice(lines []string, at int, texts []string) []string {
	at = min(max(at, 0), len(lines))
	next := make([]string, 0, len(lines)+len(texts))
	next = append(next, lines[:at]...)
	next = append(next, texts...)
	next = append(next, lines[at:]...)
	return next
}

// --- save / discard ----------------------------------------------------------

// saveEditBufferAndExit is ZZ: atomic write, unread re-stamp after the
// reload, back to REVIEW.
func (m *Model) saveEditBufferAndExit() tea.Cmd {
	path := m.currentFilePath()
	lines, ok := m.editBufferLines()
	if !ok || !m.editDirty {
		return m.exitEditMode()
	}
	if err := writeWorkingFile(m.source.Root(), path, lines); err != nil {
		return m.notify(ui.ToastError, "save failed: "+err.Error())
	}
	m.editDirty = false
	// Re-stamp seen only when the file was read before the edit: your own
	// save must not flash it unread, but it also must not swallow agent
	// changes you hadn't acknowledged yet.
	if m.selectedFile >= 0 && m.selectedFile < len(m.files) && !m.fileUnread(m.files[m.selectedFile]) {
		m.pendingSeenPath = path
	}
	m.treeChanged = true // force the exit reload even on the poll fallback
	return tea.Batch(m.notify(ui.ToastInfo, "saved "+path), m.exitEditMode())
}

// discardEditBufferAndExit is ZQ: drop the buffer so the next render re-reads
// the file from disk.
func (m *Model) discardEditBufferAndExit() tea.Cmd {
	if m.editDirty {
		delete(m.fileContents, m.currentFilePath())
		m.editDirty = false
		m.rowsVersion++
	}
	return m.exitEditMode()
}

// writeWorkingFile atomically replaces root/rel (temp file + rename in the
// same directory), preserving the file's permission bits. A trailing newline
// is always written.
func writeWorkingFile(root, rel string, lines []string) error {
	if root == "" || rel == "" {
		return errors.New("no file to save")
	}
	path := filepath.Join(root, rel)
	mode := fs.FileMode(0o644)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to replace a symlink")
		}
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cride-save-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	data := []byte(strings.Join(lines, "\n") + "\n")
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// --- advisory edit lock -------------------------------------------------------

// The lock is advisory: it cannot stop an external writer, but a cooperating
// agent can check it before touching the locked path (README documents the
// suggested instruction snippet).

const (
	editLockDir  = ".cride"
	editLockName = "editing.json"
)

type editLockPayload struct {
	Path  string    `json:"path"`
	Since time.Time `json:"since"`
}

func editLockFilePath(root string) string {
	return filepath.Join(root, editLockDir, editLockName)
}

func (m *Model) acquireEditLock(path string) {
	if m.source == nil || path == "" {
		return
	}
	root := m.source.Root()
	if root == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(root, editLockDir), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(editLockPayload{Path: path, Since: time.Now().UTC()}, "", "  ")
	if err != nil {
		return
	}
	if os.WriteFile(editLockFilePath(root), append(data, '\n'), 0o644) == nil {
		m.editLockHeld = true
	}
}

func (m *Model) releaseEditLock() {
	if !m.editLockHeld || m.source == nil {
		return
	}
	_ = os.Remove(editLockFilePath(m.source.Root()))
	m.editLockHeld = false
}

// clearStaleEditLock removes a lock left behind by a crashed session.
func clearStaleEditLock(root string) {
	if root == "" {
		return
	}
	_ = os.Remove(editLockFilePath(root))
}
