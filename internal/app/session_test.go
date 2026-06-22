package app

import (
	"testing"

	"cride/internal/diff"
	"cride/internal/session"
	"cride/internal/ui"
)

func TestSessionRestoreReanchorsAndRestoresState(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFileWithHunks("a.go", 8),
		testFileWithHunks("dir/b.go", 60),
	}
	m := Model{files: files, width: 100, height: 30, loading: false}

	state := session.State{
		SelectedFile:  "dir/b.go",
		CollapsedDirs: []string{"vendor"},
		SplitFiles:    []string{"a.go"},
		ChangeOrder:   "path",
		ChangeClock:   4,
		ChangeOrdinal: map[string]int{"a.go": 2, "dir/b.go": 4},
		ChangeHashes:  map[string]string{"a.go": fileDiffHash(files[0]), "dir/b.go": fileDiffHash(files[1])},
		Seen:          map[string]string{"a.go": fileDiffHash(files[0])},
		Searches:      map[string]string{"dir/b.go": "line"},
		Files: map[string]session.FileState{
			"dir/b.go": {CursorLine: 12, ScreenRow: 4},
		},
	}
	next, _ := m.Update(sessionLoadedMsg{state: state})
	m = next.(Model)

	if m.currentFilePath() != "dir/b.go" {
		t.Fatalf("selected file = %q, want dir/b.go", m.currentFilePath())
	}
	if got := cursorSourceLine(m); got != 12 {
		t.Fatalf("cursor source line = %d, want 12", got)
	}
	if got := m.cursorScreenRow(); got != 4 {
		t.Fatalf("cursor screen row = %d, want 4", got)
	}
	if !m.collapsedDirs["vendor"] || !m.splitFiles["a.go"] {
		t.Fatalf("collapse/split not restored: %v %v", m.collapsedDirs, m.splitFiles)
	}
	if m.changeOrder != ui.ChangeListOrderPath || m.changeClock != 4 || m.changeOrdinal["dir/b.go"] != 4 || m.changeHashes["a.go"] == "" {
		t.Fatalf("change order not restored: order=%v clock=%d ord=%v hashes=%v", m.changeOrder, m.changeClock, m.changeOrdinal, m.changeHashes)
	}
	if m.fileUnread(files[0]) {
		t.Fatal("seen snapshot not restored: a.go should be read")
	}
	if !m.search.active || m.search.query != "line" {
		t.Fatalf("search memo not restored for current file: %+v", m.search)
	}
}

func TestSessionRestoreSurvivesDrift(t *testing.T) {
	t.Parallel()

	// The saved cursor line is beyond the shrunken file: clamp, don't crash.
	m := Model{files: []diff.FileDiff{testFileWithHunks("a.go", 5)}, width: 100, height: 24}
	state := session.State{
		SelectedFile: "a.go",
		Files:        map[string]session.FileState{"a.go": {CursorLine: 400, ScreenRow: 2}},
	}
	next, _ := m.Update(sessionLoadedMsg{state: state})
	m = next.(Model)
	if got := cursorSourceLine(m); got != 5 {
		t.Fatalf("drifted cursor source line = %d, want clamp to 5", got)
	}

	// A deleted file: selection falls back without crashing.
	m2 := Model{files: []diff.FileDiff{testFileWithHunks("other.go", 5)}, width: 100, height: 24}
	state2 := session.State{SelectedFile: "gone.go"}
	next, _ = m2.Update(sessionLoadedMsg{state: state2})
	m2 = next.(Model)
	if m2.currentFilePath() != "other.go" {
		t.Fatalf("deleted-file fallback selected %q", m2.currentFilePath())
	}
}

func TestSessionPendingUntilFirstDiffLoad(t *testing.T) {
	t.Parallel()

	m := Model{width: 100, height: 24, loading: true}
	state := session.State{SelectedFile: "a.go", Files: map[string]session.FileState{"a.go": {CursorLine: 1}}}
	next, _ := m.Update(sessionLoadedMsg{state: state})
	m = next.(Model)
	if m.pendingSession == nil {
		t.Fatal("session applied before the diff loaded")
	}

	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: []diff.FileDiff{testFileWithHunks("a.go", 5)}})
	m = next.(Model)
	if m.pendingSession != nil || !m.sessionApplied {
		t.Fatal("pending session not applied on first diff load")
	}
	if m.currentFilePath() != "a.go" {
		t.Fatalf("selected file = %q", m.currentFilePath())
	}
}

func TestBuildSessionStateMirrorsModel(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithHunks("a.go", 8)}, width: 100, height: 24}
	m.cursor = 3 // source line 3
	m.viewMode = ViewDiff
	m.collapsedDirs = map[string]bool{"vendor": true}
	m.splitFiles = map[string]bool{"a.go": true}
	m.changeOrder = ui.ChangeListOrderPath
	m.changeClock = 7
	m.changeOrdinal = map[string]int{"a.go": 7}
	m.changeHashes = map[string]string{"a.go": "hash"}
	m.seen = map[string]string{"a.go": "x"}
	m.fileSearches = map[string]searchMemo{"a.go": {query: "q"}}
	// Expansions on a non-selected file: expanding the current file would
	// switch its rows to the content-loading placeholder in this fixture.
	m.localExpansions = map[string]map[int]int{"b.go": {0: 10}}

	state := m.buildSessionState()
	if state.SelectedFile != "a.go" || state.FullFileView {
		t.Fatalf("state basics = %+v", state)
	}
	if state.Files["a.go"].CursorLine != 3 {
		t.Fatalf("cursor line = %d, want 3", state.Files["a.go"].CursorLine)
	}
	if state.Files["b.go"].Expansions["0"] != 10 {
		t.Fatalf("expansions = %+v", state.Files["b.go"].Expansions)
	}
	if len(state.CollapsedDirs) != 1 || len(state.SplitFiles) != 1 {
		t.Fatalf("collapse/split = %+v", state)
	}
	if state.ChangeOrder != "path" || state.ChangeClock != 7 || state.ChangeOrdinal["a.go"] != 7 || state.ChangeHashes["a.go"] != "hash" {
		t.Fatalf("change order state = %+v", state)
	}
	if state.Seen["a.go"] != "x" || state.Searches["a.go"] != "q" {
		t.Fatalf("seen/search = %+v", state)
	}
}

func TestBuildSessionStateOmitsDefaultChangeOrder(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithHunks("a.go", 8)}, width: 100, height: 24}
	state := m.buildSessionState()
	if state.ChangeOrder != "" {
		t.Fatalf("default change order was persisted as %q", state.ChangeOrder)
	}
}
