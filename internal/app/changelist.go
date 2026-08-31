package app

// This file holds the focusable change list: focus switching, list cursor
// motion, directory collapse, and the shared view used by rendering and
// mouse hit-testing. See DESIGN.md's "Rendering and interaction" section.

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

type paneID int

const (
	paneDiff paneID = iota
	paneList
)

// changeListView builds the list view all consumers share: rendering, key
// motion, and click hit-testing see the same rows and scroll.
func (m *Model) changeListView() ui.ChangeListView {
	height := m.mainLayout().ContentHeight
	return ui.BuildChangeListViewWithOptions(m.files, m.collapsedDirs, m.unreadFileSet(), m.selectedFile, m.listCursor, m.listTop, height, m.focus == paneList, m.changeListOptions())
}

func (m Model) changeListOptions() ui.ChangeListOptions {
	return ui.ChangeListOptions{
		Order:         m.changeOrder,
		ChangeOrdinal: m.changeOrdinal,
	}
}

func (m *Model) updateChangeOrder(files []diff.FileDiff) {
	if m.changeHashes == nil {
		m.changeHashes = make(map[string]string)
	}
	if m.changeOrdinal == nil {
		m.changeOrdinal = make(map[string]int)
	}

	nextHashes := make(map[string]string, len(files))
	var changed []string
	for _, f := range files {
		path := f.Path()
		if path == "" {
			continue
		}
		hash := fileDiffHash(f)
		nextHashes[path] = hash
		if m.changeHashes[path] != hash {
			changed = append(changed, path)
		}
	}

	for path := range m.changeHashes {
		if _, ok := nextHashes[path]; !ok {
			delete(m.changeOrdinal, path)
		}
	}
	if len(changed) > 0 {
		m.stampChangeOrdinals(changed)
	}
	m.changeHashes = nextHashes
}

// stampChangeOrdinals assigns fresh ordinals to a batch of changed paths,
// ranked by workdir mtime. Without this a first-sight batch (every file at
// once, e.g. session start) would share one ordinal and change order would
// collapse into path order.
func (m *Model) stampChangeOrdinals(changed []string) {
	mtimes := m.workdirMTimes(changed)
	// Paths without a readable mtime (deletions, non-local sources) sort
	// first on the zero time and share one tick — the pre-mtime behavior.
	sort.SliceStable(changed, func(i, j int) bool {
		return mtimes[changed[i]].Before(mtimes[changed[j]])
	})
	shared := 0
	for _, path := range changed {
		if mtimes[path].IsZero() {
			if shared == 0 {
				m.changeClock++
				shared = m.changeClock
			}
			m.changeOrdinal[path] = shared
			continue
		}
		m.changeClock++
		m.changeOrdinal[path] = m.changeClock
	}
}

func (m *Model) workdirMTimes(paths []string) map[string]time.Time {
	out := make(map[string]time.Time, len(paths))
	if m.source == nil {
		return out
	}
	root := m.source.Root()
	for _, path := range paths {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			out[path] = info.ModTime()
		}
	}
	return out
}

// selectFirstDisplayedFile points the selection at the first file the change
// list renders. Diff order can differ arbitrarily from display order (the
// tree floats directories and recent changes up), so falling back to
// files[0] would land the selection mid-list.
func (m *Model) selectFirstDisplayedFile() {
	idx := -1
	for _, row := range ui.ChangeListRowsWithOptions(m.files, m.collapsedDirs, nil, m.changeListOptions()) {
		if !row.IsDir {
			idx = row.FileIdx
			break
		}
	}
	if idx < 0 {
		// Every file sits under a collapsed directory; fall through to the
		// collapse-blind order and reveal the pick.
		if order := ui.ChangeListFileOrderWithOptions(m.files, m.changeListOptions()); len(order) > 0 {
			idx = order[0]
		}
	}
	if idx < 0 || idx == m.selectedFile {
		return
	}
	m.selectedFile = idx
	m.restoreCurrentFileState()
	m.revealSelectedFile()
}

func (m *Model) toggleChangeListOrder() tea.Cmd {
	if m.changeOrder == ui.ChangeListOrderChanged {
		m.changeOrder = ui.ChangeListOrderPath
	} else {
		m.changeOrder = ui.ChangeListOrderChanged
	}
	if m.focus == paneList {
		if selected := m.changeListView().Selected; selected >= 0 {
			m.listCursor = selected
		}
	}
	m.syncChangeListScroll()
	return m.notify(ui.ToastInfo, "file list: "+m.changeOrder.String())
}

func (m *Model) focusChangeList() {
	if m.focus == paneList {
		return
	}
	m.focus = paneList
	view := m.changeListView()
	// Start the cursor on the selected file so focus lands where the eye is.
	if view.Selected >= 0 {
		m.listCursor = view.Selected
	} else {
		m.listCursor = min(max(m.listCursor, 0), max(0, len(view.Rows)-1))
	}
	m.syncChangeListScroll()
}

func (m *Model) focusDiff() {
	m.focus = paneDiff
}

// syncChangeListScroll re-clamps listTop after cursor motion or collapse.
func (m *Model) syncChangeListScroll() {
	view := m.changeListView()
	m.listCursor = view.Cursor
	m.listTop = view.Top
}

// handleChangeListKey processes keys while the change list has focus.
// Unhandled keys fall through to their global meaning so focus never traps.
func (m Model) handleChangeListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "j", "down":
		count, _ := m.consumeCount()
		return m, m.executeCommand(commandCursorDown, count, true), true
	case "k", "up":
		count, _ := m.consumeCount()
		return m, m.executeCommand(commandCursorUp, count, true), true
	case "enter":
		return m, m.executeCommand(commandOpenListSelection, 1, false), true
	case "h", "left":
		return m, m.executeCommand(commandCollapseDirectory, 1, false), true
	case "l", "right":
		return m, m.executeCommand(commandExpandDirectory, 1, false), true
	case "esc", "ctrl+l":
		return m, m.executeCommand(commandFocusDiff, 1, false), true
	default:
		return m, nil, false
	}
}

func (m *Model) moveChangeListCursor(delta int) {
	view := m.changeListView()
	m.listCursor = min(max(m.listCursor+delta, 0), max(0, len(view.Rows)-1))
	m.syncChangeListScroll()
}

func (m *Model) selectedChangeListRow() (ui.ChangeListRow, bool) {
	view := m.changeListView()
	if m.listCursor < 0 || m.listCursor >= len(view.Rows) {
		return ui.ChangeListRow{}, false
	}
	return view.Rows[m.listCursor], true
}

func (m *Model) collapseSelectedDirectory() tea.Cmd {
	if m.focus != paneList {
		return m.notify(ui.ToastInfo, "focus the change list to collapse a directory")
	}
	row, ok := m.selectedChangeListRow()
	if !ok {
		return nil
	}
	if row.IsDir && !row.Collapsed {
		m.setDirCollapsed(row.Path, true)
		m.syncChangeListScroll()
		return nil
	}
	if parent := parentDirRowIndex(m.changeListView().Rows, m.listCursor); parent >= 0 {
		m.listCursor = parent
		m.syncChangeListScroll()
	}
	return nil
}

func (m *Model) expandSelectedDirectory() tea.Cmd {
	if m.focus != paneList {
		return m.notify(ui.ToastInfo, "focus the change list to expand a directory")
	}
	row, ok := m.selectedChangeListRow()
	if !ok {
		return nil
	}
	if row.IsDir && row.Collapsed {
		m.setDirCollapsed(row.Path, false)
		m.syncChangeListScroll()
	} else if row.IsDir && m.listCursor+1 < len(m.changeListView().Rows) {
		m.listCursor++
		m.syncChangeListScroll()
	}
	return nil
}

func (m *Model) openSelectedChangeListItem() tea.Cmd {
	if m.focus != paneList {
		return m.notify(ui.ToastInfo, "focus the change list to open its selection")
	}
	row, ok := m.selectedChangeListRow()
	if !ok {
		return nil
	}
	if row.IsDir {
		m.toggleDirCollapsed(row.Path)
		m.syncChangeListScroll()
		return nil
	}
	cmd := m.openFileFromList(row.FileIdx)
	m.focusDiff()
	return cmd
}

func parentDirRowIndex(rows []ui.ChangeListRow, from int) int {
	if from < 0 || from >= len(rows) {
		return -1
	}
	depth := rows[from].Depth
	for i := from - 1; i >= 0; i-- {
		if rows[i].IsDir && rows[i].Depth < depth {
			return i
		}
	}
	return -1
}

// openFileFromList commits the list cursor to the selected file.
func (m *Model) openFileFromList(fileIdx int) tea.Cmd {
	if fileIdx < 0 || fileIdx >= len(m.files) || fileIdx == m.selectedFile {
		return nil
	}
	m.saveCurrentFileState()
	m.selectedFile = fileIdx
	if m.files[fileIdx].Status == diff.FileUnchanged {
		m.viewMode = ViewFile
	}
	m.restoreCurrentFileState()
	m.rememberPath(m.currentFilePath())
	m.clampScroll()
	return m.ensureCurrentFileContentCmd()
}

func (m *Model) toggleDirCollapsed(path string) {
	if m.collapsedDirs == nil {
		m.collapsedDirs = make(map[string]bool)
	}
	if m.collapsedDirs[path] {
		delete(m.collapsedDirs, path)
	} else {
		m.collapsedDirs[path] = true
	}
}

func (m *Model) setDirCollapsed(path string, collapsed bool) {
	if collapsed {
		if m.collapsedDirs == nil {
			m.collapsedDirs = make(map[string]bool)
		}
		m.collapsedDirs[path] = true
		return
	}
	delete(m.collapsedDirs, path)
}

// revealSelectedFile expands any collapsed ancestor of the selected file so
// the selection is never hidden; file cycling into a collapsed directory
// auto-expands it.
func (m *Model) revealSelectedFile() {
	if len(m.collapsedDirs) == 0 {
		return
	}
	path := m.currentFilePath()
	if path == "" {
		return
	}
	for _, dir := range ui.ChangeListAncestorDirs(path) {
		delete(m.collapsedDirs, dir)
	}
}
