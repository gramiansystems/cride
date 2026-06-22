package app

// This file holds side-by-side view state: the per-file zs toggle, the
// active side for symbol lookups, and cursor re-anchoring across toggles.
// See DESIGN.md's "Rendering and interaction" section.

import (
	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/ui"
)

// splitViewActive reports whether the current file renders side-by-side:
// enabled for the file, diff view, and a wide enough panel (narrow windows
// fall back to unified without losing the preference).
func (m *Model) splitViewActive() bool {
	if m.viewMode != ViewDiff {
		return false
	}
	if !m.splitFiles[m.currentFilePath()] {
		return false
	}
	_, _, ok := ui.PairColumnWidths(m.diffContentWidth())
	return ok
}

// toggleSplitView flips side-by-side for the current file, re-anchoring the
// cursor by source line so hunk positions correspond across the toggle.
func (m *Model) toggleSplitView() tea.Cmd {
	if m.viewMode != ViewDiff {
		return m.notify(ui.ToastWarn, "side-by-side applies to the diff view")
	}
	path := m.currentFilePath()
	if path == "" {
		return nil
	}
	enabling := !m.splitFiles[path]
	if enabling {
		if _, _, ok := ui.PairColumnWidths(m.diffContentWidth()); !ok {
			return m.notify(ui.ToastWarn, "window too narrow for side-by-side")
		}
	}

	srcLine := 0
	if rows := m.currentRows(); m.cursor >= 0 && m.cursor < len(rows) {
		srcLine = sourceLine(rows[m.cursor])
	}

	if m.splitFiles == nil {
		m.splitFiles = make(map[string]bool)
	}
	if enabling {
		m.splitFiles[path] = true
	} else {
		delete(m.splitFiles, path)
	}
	m.rowsVersion++

	if srcLine > 0 {
		m.jumpSourceLine(srcLine)
	}
	m.clampScroll()
	if enabling {
		return m.notify(ui.ToastInfo, "side-by-side on — the cursor crosses sides at line edges")
	}
	return m.notify(ui.ToastInfo, "unified view")
}

// setSplitActiveSide records which column gd/gr/hover operate on.
func (m *Model) setSplitActiveSide(left bool) {
	m.splitActiveLeft = left
}

// splitSideForClick resolves which column a diff-content x offset landed in.
func splitSideForClick(xInContent, contentWidth int) (left bool, ok bool) {
	lw, _, valid := ui.PairColumnWidths(contentWidth)
	if !valid {
		return false, false
	}
	return xInContent < ui.PairLeftCellEnd(lw), true
}
