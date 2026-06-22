package app

// Session persistence (DESIGN.md's "Persistence and repository-local files"):
// view state is captured as source coordinates whenever file state is saved,
// flushed on a debounce and on quit, and re-applied best-effort after the first
// diff load.

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/session"
	"cride/internal/ui"
)

const sessionSaveDebounce = 5 * time.Second

type sessionLoadedMsg struct {
	state session.State
	err   error
}

type sessionSaveTickMsg struct{}

type sessionSavedMsg struct {
	err error
}

func (m Model) sessionRepoID() string {
	if m.source == nil || m.freshSession {
		return ""
	}
	return session.RepoID(m.source.Root(), m.source.Baseline())
}

func (m Model) loadSessionCmd() tea.Cmd {
	repoID := m.sessionRepoID()
	if repoID == "" {
		return nil
	}
	return func() tea.Msg {
		session.CleanOld()
		state, err := session.Load(repoID)
		return sessionLoadedMsg{state: state, err: err}
	}
}

// captureSessionFileState records the current file's position in source
// coordinates. Called from saveCurrentFileState so the session mirror is
// always as fresh as the in-memory row state.
func (m *Model) captureSessionFileState() {
	path := m.currentFilePath()
	if path == "" {
		return
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return
	}
	line := sourceLine(rows[m.cursor])
	if line <= 0 {
		return
	}
	if m.sessionFiles == nil {
		m.sessionFiles = make(map[string]session.FileState)
	}
	m.sessionFiles[path] = session.FileState{
		CursorLine: line,
		CursorCol:  m.col,
		ScreenRow:  m.cursorScreenRow(),
	}
}

// applySessionFileState positions a freshly opened file from the stored
// session, re-anchoring by source line (the tree may have changed since).
func (m *Model) applySessionFileState() {
	path := m.currentFilePath()
	state, ok := m.sessionFiles[path]
	if !ok || state.CursorLine <= 0 {
		return
	}
	m.jumpSourceLine(state.CursorLine)
	m.setCursorCol(state.CursorCol)
	m.clampScroll()
	m.scrollCursorToScreenRowAllowingEOFSpace(state.ScreenRow)
}

// applySession restores global session state once both the session and the
// first diff are loaded. Restore is best-effort and silent. It reports
// whether the stored file selection was applied, so the caller knows if a
// default selection is still needed.
func (m *Model) applySession(state session.State) bool {
	if state.FullFileView {
		m.viewMode = ViewFile
	}
	if len(state.CollapsedDirs) > 0 {
		m.collapsedDirs = make(map[string]bool, len(state.CollapsedDirs))
		for _, dir := range state.CollapsedDirs {
			m.collapsedDirs[dir] = true
		}
	}
	if len(state.SplitFiles) > 0 {
		m.splitFiles = make(map[string]bool, len(state.SplitFiles))
		for _, path := range state.SplitFiles {
			m.splitFiles[path] = true
		}
	}
	if order, ok := ui.ParseChangeListOrder(state.ChangeOrder); ok {
		m.changeOrder = order
	}
	if len(state.ChangeOrdinal) > 0 {
		m.changeOrdinal = make(map[string]int, len(state.ChangeOrdinal))
		for path, ordinal := range state.ChangeOrdinal {
			m.changeOrdinal[path] = ordinal
			if ordinal > m.changeClock {
				m.changeClock = ordinal
			}
		}
	}
	if len(state.ChangeHashes) > 0 {
		m.changeHashes = make(map[string]string, len(state.ChangeHashes))
		for path, hash := range state.ChangeHashes {
			m.changeHashes[path] = hash
		}
	}
	if state.ChangeClock > m.changeClock {
		m.changeClock = state.ChangeClock
	}
	if len(state.Seen) > 0 {
		m.seen = make(map[string]string, len(state.Seen))
		for path, hash := range state.Seen {
			m.seen[path] = hash
		}
	}
	if len(state.Searches) > 0 {
		m.fileSearches = make(map[string]searchMemo, len(state.Searches))
		for path, query := range state.Searches {
			m.fileSearches[path] = searchMemo{query: query}
		}
	}
	if len(state.Files) > 0 {
		m.sessionFiles = make(map[string]session.FileState, len(state.Files))
		for path, fs := range state.Files {
			m.sessionFiles[path] = fs
		}
		for path, fs := range state.Files {
			if len(fs.Expansions) == 0 {
				continue
			}
			if m.localExpansions == nil {
				m.localExpansions = make(map[string]map[int]int)
			}
			expansions := make(map[int]int, len(fs.Expansions))
			for key, extra := range fs.Expansions {
				if idx, err := strconv.Atoi(key); err == nil && extra > 0 {
					expansions[idx] = extra
				}
			}
			if len(expansions) > 0 {
				m.localExpansions[path] = expansions
			}
		}
	}
	m.rowsVersion++

	restored := false
	if idx := findFileIndexByPath(m.files, state.SelectedFile); idx >= 0 {
		m.selectedFile = idx
		// restoreCurrentFileState falls through to applySessionFileState,
		// which clamps and applies the stored screen row itself; a trailing
		// clampScroll here would undo the EOF-space scroll.
		m.restoreCurrentFileState()
		restored = true
	} else {
		m.clampScroll()
	}
	m.restoreSearchForCurrentFile()
	return restored
}

// buildSessionState assembles the serialization view-model from the model.
func (m *Model) buildSessionState() session.State {
	m.captureSessionFileState()
	state := session.State{
		FormatVersion: session.FormatVersion,
		SelectedFile:  m.currentFilePath(),
		FullFileView:  m.viewMode == ViewFile,
	}
	if m.source != nil {
		state.Baseline = m.source.Baseline()
	}
	for dir := range m.collapsedDirs {
		state.CollapsedDirs = append(state.CollapsedDirs, dir)
	}
	for path, on := range m.splitFiles {
		if on {
			state.SplitFiles = append(state.SplitFiles, path)
		}
	}
	if m.changeOrder != ui.DefaultChangeListOrder {
		state.ChangeOrder = m.changeOrder.ID()
	}
	if m.changeClock > 0 {
		state.ChangeClock = m.changeClock
	}
	if len(m.changeOrdinal) > 0 {
		state.ChangeOrdinal = make(map[string]int, len(m.changeOrdinal))
		for path, ordinal := range m.changeOrdinal {
			state.ChangeOrdinal[path] = ordinal
		}
	}
	if len(m.changeHashes) > 0 {
		state.ChangeHashes = make(map[string]string, len(m.changeHashes))
		for path, hash := range m.changeHashes {
			state.ChangeHashes[path] = hash
		}
	}
	if len(m.seen) > 0 {
		state.Seen = make(map[string]string, len(m.seen))
		for path, hash := range m.seen {
			state.Seen[path] = hash
		}
	}
	if len(m.fileSearches) > 0 {
		state.Searches = make(map[string]string, len(m.fileSearches))
		for path, memo := range m.fileSearches {
			state.Searches[path] = memo.query
		}
	}
	if len(m.sessionFiles) > 0 || len(m.localExpansions) > 0 {
		state.Files = make(map[string]session.FileState)
		for path, fs := range m.sessionFiles {
			state.Files[path] = fs
		}
		for path, expansions := range m.localExpansions {
			fs := state.Files[path]
			fs.Expansions = make(map[string]int, len(expansions))
			for idx, extra := range expansions {
				if extra > 0 {
					fs.Expansions[strconv.Itoa(idx)] = extra
				}
			}
			state.Files[path] = fs
		}
	}
	return state
}

// markSessionDirty schedules a debounced background save.
func (m *Model) markSessionDirty() tea.Cmd {
	if m.sessionRepoID() == "" {
		return nil
	}
	m.sessionDirty = true
	if m.sessionSavePending {
		return nil
	}
	m.sessionSavePending = true
	return tea.Tick(sessionSaveDebounce, func(time.Time) tea.Msg {
		return sessionSaveTickMsg{}
	})
}

func (m *Model) saveSessionCmd() tea.Cmd {
	repoID := m.sessionRepoID()
	if repoID == "" {
		return nil
	}
	state := m.buildSessionState()
	return func() tea.Msg {
		return sessionSavedMsg{err: session.Save(repoID, state)}
	}
}

// saveSessionNow flushes synchronously; used on quit so nothing is lost.
func (m *Model) saveSessionNow() {
	repoID := m.sessionRepoID()
	if repoID == "" {
		return
	}
	_ = session.Save(repoID, m.buildSessionState())
}
