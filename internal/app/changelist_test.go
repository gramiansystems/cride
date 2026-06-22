package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	navsearch "cride/internal/search"
	"cride/internal/session"
	"cride/internal/source"
	"cride/internal/ui"
)

func nestedTestFiles() []diff.FileDiff {
	files := []diff.FileDiff{
		testFile("a.go"),
		testFile("internal/ui/render.go"),
		testFile("internal/ui/row.go"),
		testFile("internal/app/app.go"),
	}
	for i := range files {
		files[i].Added = 1
	}
	return files
}

func ctrlKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func TestFocusRoundTripAndGlobalsStillWork(t *testing.T) {
	t.Parallel()

	m := Model{files: nestedTestFiles(), width: 90, height: 24}

	next, _ := m.handleKey(ctrlKey(tea.KeyCtrlH))
	m = next.(Model)
	if m.focus != paneList {
		t.Fatalf("ctrl+h focus = %v, want list", m.focus)
	}

	// The cursor starts on the selected file's row.
	view := m.changeListView()
	if view.Cursor != view.Selected {
		t.Fatalf("list cursor = %d, want selected row %d", view.Cursor, view.Selected)
	}

	// j moves the list cursor, not the diff cursor.
	diffCursor := m.cursor
	m = press(m, "j")
	if m.cursor != diffCursor {
		t.Fatal("j while list focused moved the diff cursor")
	}

	// Globals fall through: ? opens the command palette from the list.
	m = press(m, "?")
	if m.overlay.Kind != OverlayCommandPalette {
		t.Fatal("? did not fall through to the command palette from the list")
	}
	m = press(m, "esc")

	next, _ = m.handleKey(ctrlKey(tea.KeyCtrlL))
	m = next.(Model)
	if m.focus != paneDiff {
		t.Fatalf("ctrl+l focus = %v, want diff", m.focus)
	}
}

func TestChangeListCollapseAggregatesAndHides(t *testing.T) {
	t.Parallel()

	files := nestedTestFiles()
	rows := ui.ChangeListRows(files, map[string]bool{"internal": true}, nil)
	var dirRow *ui.ChangeListRow
	for i := range rows {
		if rows[i].IsDir && rows[i].Path == "internal" {
			dirRow = &rows[i]
		}
		if strings.HasPrefix(rows[i].Path, "internal/") {
			t.Fatalf("collapsed subtree leaked row %q", rows[i].Path)
		}
	}
	if dirRow == nil {
		t.Fatal("collapsed dir row missing")
	}
	if !dirRow.Collapsed || dirRow.Files != 3 {
		t.Fatalf("collapsed dir stats = %+v, want 3 files", dirRow)
	}
	if dirRow.Added != 3 || dirRow.Deleted != 0 {
		t.Fatalf("aggregate +%d -%d, want +3 -0", dirRow.Added, dirRow.Deleted)
	}
}

func TestChangeListEnterOpensFileAndReturnsFocus(t *testing.T) {
	t.Parallel()

	m := Model{files: nestedTestFiles(), width: 90, height: 24}
	next, _ := m.handleKey(ctrlKey(tea.KeyCtrlH))
	m = next.(Model)

	// Walk down to a different file row and open it.
	view := m.changeListView()
	target := -1
	for i, row := range view.Rows {
		if !row.IsDir && row.FileIdx != m.selectedFile {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no other file row found")
	}
	for m.listCursor != target {
		before := m.listCursor
		if m.listCursor < target {
			m = press(m, "j")
		} else {
			m = press(m, "k")
		}
		if m.listCursor == before {
			t.Fatalf("list cursor stuck at %d aiming for %d", m.listCursor, target)
		}
	}
	m = press(m, "enter")
	if m.focus != paneDiff {
		t.Fatal("enter did not return focus to the diff")
	}
	if got := m.changeListView().Rows[target].FileIdx; m.selectedFile != got {
		t.Fatalf("selectedFile = %d, want %d", m.selectedFile, got)
	}
}

func TestChangeListHCollapsesAndFileCyclingAutoExpands(t *testing.T) {
	t.Parallel()

	m := Model{files: nestedTestFiles(), selectedFile: 0, width: 90, height: 24}
	next, _ := m.handleKey(ctrlKey(tea.KeyCtrlH))
	m = next.(Model)

	// Move to the internal/ dir row and collapse it with h.
	view := m.changeListView()
	dirIdx := -1
	for i, row := range view.Rows {
		if row.IsDir && row.Path == "internal" {
			dirIdx = i
			break
		}
	}
	if dirIdx < 0 {
		t.Fatal("internal dir row not found")
	}
	m.listCursor = dirIdx
	m = press(m, "h")
	if !m.collapsedDirs["internal"] {
		t.Fatal("h did not collapse the dir")
	}

	// File cycling reaches the hidden file and auto-expands its ancestors.
	// Directories sort first, so from a.go the collapsed files are behind us.
	next2, _ := m.handleKey(ctrlKey(tea.KeyCtrlL))
	m = next2.(Model)
	m = press(m, "[")
	m = press(m, "[")
	if m.currentFilePath() == "a.go" {
		t.Fatal("[[ did not move into the collapsed directory")
	}
	if m.collapsedDirs["internal"] {
		t.Fatal("arriving in a collapsed dir did not auto-expand it")
	}
}

func TestBottomPanelClickSelectsAndJumps(t *testing.T) {
	t.Parallel()

	m := Model{files: nestedTestFiles(), width: 90, height: 30, fileContents: map[string]fileContentState{}}
	m.referencePanel = referencePanelState{
		Open: true,
		Results: []navsearch.ReferenceResult{
			{Location: source.Location{Path: "a.go", Line: 1, Column: 1}},
			{Location: source.Location{Path: "internal/ui/render.go", Line: 1, Column: 1}},
		},
	}

	layout := ui.Layout(m.width, m.height, m.bottomPanelView())
	if layout.BottomPanelHeight == 0 {
		t.Fatal("bottom panel not open in layout")
	}
	// First result row: border(1) + title(1) → innerY 1.
	next, _ := m.handleMouse(tea.MouseMsg{
		X:      4,
		Y:      layout.BottomPanelY + 1 + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	got := next.(Model)
	if got.referencePanel.Cursor != 0 {
		t.Fatalf("click selected result %d, want 0", got.referencePanel.Cursor)
	}
	if got.viewMode != ViewFile || got.currentFilePath() != "a.go" {
		t.Fatalf("bottom-panel click did not jump: mode=%v path=%q", got.viewMode, got.currentFilePath())
	}
	// Second result row.
	next, _ = got.handleMouse(tea.MouseMsg{
		X:      4,
		Y:      layout.BottomPanelY + 1 + 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if got := next.(Model); got.referencePanel.Cursor != 1 {
		t.Fatalf("click selected result %d, want 1", got.referencePanel.Cursor)
	}
}

func TestChangeListOrderToggleAndNavigation(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFile("a.go"),
		testFile("b.go"),
		testFile("dir/c.go"),
	}
	m := Model{
		files:         files,
		selectedFile:  0,
		width:         90,
		height:        24,
		changeOrdinal: map[string]int{"a.go": 1, "b.go": 3, "dir/c.go": 2},
	}

	if m.changeOrder != ui.ChangeListOrderChanged {
		t.Fatalf("default changeOrder = %v, want changed", m.changeOrder)
	}

	m = press(press(m, "["), "[")
	if m.currentFilePath() != "dir/c.go" {
		t.Fatalf("[[ in change order selected %q, want dir/c.go", m.currentFilePath())
	}

	m = press(m, "o")
	if m.changeOrder != ui.ChangeListOrderPath {
		t.Fatalf("changeOrder = %v, want path", m.changeOrder)
	}
	if !strings.Contains(m.status.text, "path order") {
		t.Fatalf("toggle toast = %q", m.status.text)
	}
}

func TestChangeOrderTracksReloadedDiffs(t *testing.T) {
	t.Parallel()

	initial := []diff.FileDiff{testFile("a.go"), testFile("b.go")}
	m := Model{files: initial, selectedFile: 0, width: 90, height: 24}
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: initial})
	m = next.(Model)

	edited := []diff.FileDiff{testFile("a.go"), testFileWithLines("b.go", 3)}
	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: edited})
	m = next.(Model)
	m.changeOrder = ui.ChangeListOrderChanged

	order := ui.ChangeListFileOrderWithOptions(m.files, m.changeListOptions())
	if len(order) != 2 || order[0] != 1 || order[1] != 0 {
		t.Fatalf("change order = %v, want b.go before a.go", order)
	}
}

func TestInitialLoadSelectsFirstDisplayedFile(t *testing.T) {
	t.Parallel()

	// Diff order puts the top-level file first, but the rendered tree floats
	// directories above it; the startup selection must follow the tree.
	files := []diff.FileDiff{testFile("zz.go"), testFile("dir/a.go")}
	m := Model{width: 90, height: 24}
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: files})
	m = next.(Model)

	if got := m.currentFilePath(); got != "dir/a.go" {
		t.Fatalf("initial selection = %q, want first displayed file dir/a.go", got)
	}
}

func TestInitialLoadKeepsSessionSelection(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFile("zz.go"), testFile("dir/a.go")}
	state := session.State{SelectedFile: "zz.go"}
	m := Model{width: 90, height: 24, pendingSession: &state}
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: files})
	m = next.(Model)

	if got := m.currentFilePath(); got != "zz.go" {
		t.Fatalf("selection = %q, want session-restored zz.go", got)
	}
}

func TestChangeOrdinalsSeededByMTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Now()
	writeAged := func(name string, age time.Duration) {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	writeAged("old.go", 2*time.Hour)
	writeAged("new.go", 0)

	m := Model{source: fakeSource{root: root}}
	files := []diff.FileDiff{testFile("old.go"), testFile("new.go"), testFile("gone.go")}
	m.updateChangeOrder(files)

	if m.changeOrdinal["new.go"] <= m.changeOrdinal["old.go"] {
		t.Fatalf("ordinals new.go=%d old.go=%d, want newer mtime ranked higher",
			m.changeOrdinal["new.go"], m.changeOrdinal["old.go"])
	}
	if m.changeOrdinal["gone.go"] >= m.changeOrdinal["old.go"] {
		t.Fatalf("ordinals gone.go=%d old.go=%d, want unreadable mtime ranked lowest",
			m.changeOrdinal["gone.go"], m.changeOrdinal["old.go"])
	}

	// A later edit still floats the file above the whole seeded batch.
	top := m.changeOrdinal["new.go"]
	m.updateChangeOrder([]diff.FileDiff{testFileWithLines("old.go", 3), testFile("new.go"), testFile("gone.go")})
	if m.changeOrdinal["old.go"] <= top {
		t.Fatalf("re-edited old.go ordinal = %d, want above %d", m.changeOrdinal["old.go"], top)
	}
}

func TestOverlayClickSelectsThenAccepts(t *testing.T) {
	t.Parallel()

	m := Model{files: nestedTestFiles(), width: 100, height: 30}
	m.overlay = overlayState{Kind: OverlayCommandPalette, Results: numberedResults(10)}

	overlay := m.overlayView()
	// Find the y of the third result row via the shared geometry helper.
	found := -1
	var clickY int
	for y := 0; y < m.height; y++ {
		if idx := ui.OverlayResultIndexAt(overlay, m.width, m.height, m.width/2, y); idx == 2 {
			found = idx
			clickY = y
			break
		}
	}
	if found != 2 {
		t.Fatal("could not locate result row 2 in overlay geometry")
	}

	next, _ := m.handleMouse(tea.MouseMsg{X: m.width / 2, Y: clickY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := next.(Model)
	if got.overlay.Cursor != 2 {
		t.Fatalf("overlay click cursor = %d, want 2", got.overlay.Cursor)
	}

	// A second click accepts the selected command-palette row. These fixture
	// rows have no command ID, so accepting simply closes the palette.
	next, _ = got.handleMouse(tea.MouseMsg{X: m.width / 2, Y: clickY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if got := next.(Model); got.overlay.Kind != OverlayNone {
		t.Fatalf("second overlay click kind = %v, want palette closed", got.overlay.Kind)
	}
}
