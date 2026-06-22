package app

// This file holds the live layer (DESIGN.md's "Review model"):
// fsnotify-driven auto-reload with a fingerprint poll fallback, and the
// state-preserving reload path.
// Recompute never blocks Update; everything runs in commands.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/diffsource"
	"cride/internal/lsp"
)

const pollInterval = 2 * time.Second

type watchStartedMsg struct {
	ch   chan struct{}
	stop func()
	err  error
}

type treeChangedMsg struct{}

type pollTickMsg struct{}

type fingerprintMsg struct {
	generation int
	value      string
	err        error
}

// startWatchCmd tries the source's native watcher; on failure the poll loop
// is the degraded fallback.
func (m Model) startWatchCmd() tea.Cmd {
	watcher, ok := m.source.(diffsource.Watcher)
	if !ok {
		return m.schedulePollCmd()
	}
	return func() tea.Msg {
		ch := make(chan struct{}, 1)
		stop, err := watcher.Watch(func() {
			select {
			case ch <- struct{}{}:
			default: // an event is already pending; coalesce
			}
		})
		return watchStartedMsg{ch: ch, stop: stop, err: err}
	}
}

func waitWatchCmd(ch chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return treeChangedMsg{}
	}
}

func (m Model) schedulePollCmd() tea.Cmd {
	if _, ok := m.source.(diffsource.Fingerprinter); !ok {
		return nil
	}
	return tea.Tick(pollInterval, func(time.Time) tea.Msg {
		return pollTickMsg{}
	})
}

func (m Model) fingerprintCmd() tea.Cmd {
	fp, ok := m.source.(diffsource.Fingerprinter)
	if !ok {
		return nil
	}
	generation := m.loadSeq
	return func() tea.Msg {
		value, err := fp.Fingerprint()
		return fingerprintMsg{generation: generation, value: value, err: err}
	}
}

// reload starts a new diff load without blanking the view; the sequence
// number drops stale results if loads overlap.
func (m *Model) reload(manual bool) tea.Cmd {
	m.saveCurrentFileState()
	m.reloadRequested = manual
	m.loadSeq++
	return m.loadCmdSeq(m.loadSeq)
}

// stopWatching releases the watcher before quitting.
func (m *Model) stopWatching() {
	if m.watchStop != nil {
		m.watchStop()
		m.watchStop = nil
	}
}

// applyReloadedDiff replaces the loaded diff while preserving the reviewer's
// place: selection follows the file path and the cursor re-anchors by source
// line, not row index, so shifted hunks don't move the eye.
func (m *Model) applyReloadedDiff(files []diff.FileDiff) (previousPathLost bool) {
	previousPath := m.currentFilePath()
	prevSrcLine := 0
	if rows := m.currentRows(); m.cursor >= 0 && m.cursor < len(rows) {
		prevSrcLine = sourceLine(rows[m.cursor])
	}
	m.saveCurrentFileState()

	m.files = files
	m.changedPaths = changedPathSet(files)
	m.fileContents = make(map[string]fileContentState)
	m.contentGeneration++
	m.rowsVersion++
	m.projectFiles = nil
	m.projectFilesLoading = false
	m.projectFilesErr = nil
	m.referencePanel = referencePanelState{}
	m.enrichmentPanel = enrichmentPanelState{}
	m.diagnostics = make(map[string][]lsp.Diagnostic)
	m.hasPendingLocation = false

	m.selectedFile = fileIndexByPath(m.files, previousPath)
	m.restoreCurrentFileState()
	if previousPath != "" && m.currentFilePath() == previousPath && prevSrcLine > 0 {
		if cursorRowSourceLine(m) != prevSrcLine {
			m.jumpSourceLine(prevSrcLine)
		}
	}
	return previousPath != "" && findFileIndexByPath(m.files, previousPath) < 0
}

func cursorRowSourceLine(m *Model) int {
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0
	}
	return sourceLine(rows[m.cursor])
}
