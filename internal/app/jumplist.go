// Vim-style jump history: positions you jump away from (gd/gr/panels/
// overlays) are recorded so ctrl+o walks back and ctrl+] walks forward.
// Both are bare control chords (not letter keys) because the references/
// enrichment panel deliberately stays open after a jump and claims j/k/o
// for its own list navigation — any letter-based binding gets swallowed by
// the still-open panel. g;/g, were also rejected: those are vim's
// changelist keys, a different concept.
package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/source"
	"cride/internal/ui"
)

const jumplistMax = 100

// jumpEntry is one remembered position. line is the source line under the
// cursor when recorded (0 when the cursor sat on a header or message row);
// cursor is the raw row index, used to restore exactly when the rows are
// unchanged and as a clamped fallback when line positioning fails.
type jumpEntry struct {
	path   string
	mode   ViewMode
	line   int
	cursor int
	col    int // character-cursor column, restored on top of the row position
}

func (m *Model) captureJumpEntry() jumpEntry {
	e := jumpEntry{path: m.currentFilePath(), mode: m.viewMode, cursor: m.cursor, col: m.col}
	if rows := m.currentRows(); m.cursor >= 0 && m.cursor < len(rows) {
		e.line = sourceLine(rows[m.cursor])
	}
	return e
}

// pushJump records the position being jumped away from. Entries forward of
// the current index are dropped, mirroring vim: jumping back and then
// jumping somewhere new starts a fresh branch of history.
func (m *Model) pushJump() {
	e := m.captureJumpEntry()
	if e.path == "" {
		return
	}
	m.jumplist = m.jumplist[:m.jumpIndex]
	if n := len(m.jumplist); n > 0 && m.jumplist[n-1] == e {
		return
	}
	m.jumplist = append(m.jumplist, e)
	if len(m.jumplist) > jumplistMax {
		m.jumplist = m.jumplist[len(m.jumplist)-jumplistMax:]
	}
	m.jumpIndex = len(m.jumplist)
}

func (m *Model) jumpBack() tea.Cmd {
	if m.jumpIndex == 0 {
		return m.notify(ui.ToastInfo, "Already at the oldest jump position")
	}
	if m.jumpIndex == len(m.jumplist) {
		// Standing past the newest entry: remember the live position so
		// g, can return here.
		if e := m.captureJumpEntry(); e != m.jumplist[m.jumpIndex-1] {
			m.jumplist = append(m.jumplist, e)
		}
	}
	m.jumpIndex--
	return m.gotoJumpEntry(m.jumplist[m.jumpIndex])
}

func (m *Model) jumpForward() tea.Cmd {
	if m.jumpIndex >= len(m.jumplist)-1 {
		return m.notify(ui.ToastInfo, "Already at the newest jump position")
	}
	m.jumpIndex++
	return m.gotoJumpEntry(m.jumplist[m.jumpIndex])
}

// gotoJumpEntry is jumpToLocation's history-navigating sibling: it restores
// the recorded view mode instead of forcing ViewFile and never pushes.
func (m *Model) gotoJumpEntry(e jumpEntry) tea.Cmd {
	m.saveCurrentFileState()
	idx := findFileIndexByPath(m.files, e.path)
	mode := e.mode
	if idx < 0 {
		// The file left the diff (e.g. after a reload); full-file view
		// still works through the synthetic entry.
		mode = ViewFile
		idx = m.ensureFileIndex(e.path)
	}
	m.selectedFile = idx
	m.viewMode = mode
	m.restoreCurrentFileState()
	m.rememberPath(e.path)

	m.hasPendingLocation = false
	loc := source.Location{Path: e.path, Line: e.line, Column: 1}
	rows := m.currentRows()
	switch {
	case e.cursor >= 0 && e.cursor < len(rows) && (e.line == 0 || sourceLine(rows[e.cursor]) == e.line):
		m.cursor = e.cursor
	case e.line > 0 && m.positionCursorAtLocation(loc):
	case e.line > 0 && m.currentFileNeedsContent():
		m.pendingLocation = loc
		m.hasPendingLocation = true
	case len(rows) > 0:
		m.cursor = max(0, min(e.cursor, len(rows)-1))
	}
	m.setCursorCol(e.col)
	cmd := m.ensureCurrentFileContentCmd()
	m.clampScroll()
	m.centerCursorInViewport()
	return cmd
}
