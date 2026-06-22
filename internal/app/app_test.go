package app

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/lsp"
	navsearch "cride/internal/search"
	"cride/internal/source"
	"cride/internal/ui"
)

func TestFileNavigationSwitchesFilesAndRestoresState(t *testing.T) {
	t.Parallel()

	files := testFiles()
	m := Model{files: files, selectedFile: 0, cursor: 1, width: 80, height: 20}

	next, cmd := m.handleKey(key("}"))
	if cmd != nil {
		t.Fatal("file navigation returned unexpected command")
	}
	got := next.(Model)
	if got.selectedFile != 1 {
		t.Fatalf("} selectedFile = %d, want 1", got.selectedFile)
	}
	if got.cursor != 0 {
		t.Fatalf("new file cursor = %d, want 0", got.cursor)
	}

	got.cursor = 1

	prev, cmd := got.handleKey(key("{"))
	if cmd != nil {
		t.Fatal("file navigation returned unexpected command")
	}
	got = prev.(Model)
	if got.selectedFile != 0 {
		t.Fatalf("{ selectedFile = %d, want 0", got.selectedFile)
	}
	if got.cursor != 1 {
		t.Fatalf("restored first file cursor = %d, want 1", got.cursor)
	}

	again, _ := got.handleKey(key("}"))
	got = again.(Model)
	if got.selectedFile != 1 {
		t.Fatalf("second } selectedFile = %d, want 1", got.selectedFile)
	}
	if got.cursor != 1 {
		t.Fatalf("restored second file cursor = %d, want 1", got.cursor)
	}
}

func TestFileNavigationDoesNotWrap(t *testing.T) {
	t.Parallel()

	files := testFiles()
	m := Model{files: files, selectedFile: len(files) - 1, cursor: 1, width: 80, height: 20}

	next, _ := m.handleKey(key("}"))
	if got := next.(Model).selectedFile; got != len(files)-1 {
		t.Fatalf("} from last file selectedFile = %d, want %d", got, len(files)-1)
	}

	m.selectedFile = 0
	prev, _ := m.handleKey(key("{"))
	if got := prev.(Model).selectedFile; got != 0 {
		t.Fatalf("{ from first file selectedFile = %d, want 0", got)
	}
}

func TestFileNavigationShiftJAliasAndKHovers(t *testing.T) {
	t.Parallel()

	files := testFiles()
	m := Model{files: files, selectedFile: 0, width: 80, height: 20}

	next, _ := m.handleKey(key("J"))
	if got := next.(Model).selectedFile; got != 1 {
		t.Fatalf("J selectedFile = %d, want 1", got)
	}

	// K always hovers, even when a previous file exists.
	m.selectedFile = 1
	prev, _ := m.handleKey(key("K"))
	got := prev.(Model)
	if got.selectedFile != 1 {
		t.Fatalf("K changed selectedFile to %d, want hover with no navigation", got.selectedFile)
	}
	if !got.enrichmentPanel.Open || got.enrichmentPanel.Kind != enrichmentPanelHover {
		t.Fatalf("K did not open the hover panel: %+v", got.enrichmentPanel)
	}
}

func TestFileNavigationUsesRenderedChangeListOrder(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFile("a.go"),
		testFile("b.go"),
		testFile("dir/c.go"),
	}

	m := Model{files: files, selectedFile: 0, width: 80, height: 20}
	got := press(press(m, "["), "[")
	if got.selectedFile != 2 {
		t.Fatalf("[[ from a.go selectedFile = %d, want 2 for dir/c.go", got.selectedFile)
	}

	got = press(press(got, "]"), "]")
	if got.selectedFile != 0 {
		t.Fatalf("]] from dir/c.go selectedFile = %d, want 0 for a.go", got.selectedFile)
	}

	m = Model{files: files, selectedFile: 1, width: 80, height: 20}
	got = press(press(m, "]"), "]")
	if got.selectedFile != 1 {
		t.Fatalf("]] from b.go selectedFile = %d, want 1", got.selectedFile)
	}
}

func TestCountPrefixedNavigation(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFileWithLines("a.go", 8),
		testFileWithLines("b.go", 8),
		testFileWithLines("c.go", 8),
	}
	m := Model{files: files, selectedFile: 0, width: 80, height: 20}

	m = press(m, "3")
	m = press(m, "j")
	if m.cursor != 3 {
		t.Fatalf("cursor after 3j = %d, want 3", m.cursor)
	}

	m = press(m, "2")
	m = press(m, "k")
	if m.cursor != 1 {
		t.Fatalf("cursor after 2k = %d, want 1", m.cursor)
	}

	m = press(m, "2")
	m = press(m, "]")
	m = press(m, "]")
	if m.selectedFile != 2 {
		t.Fatalf("selectedFile after 2]] = %d, want 2", m.selectedFile)
	}

	m = press(m, "3")
	m = press(m, "[")
	m = press(m, "[")
	if m.selectedFile != 0 {
		t.Fatalf("selectedFile after 3[[ = %d, want 0", m.selectedFile)
	}
}

func TestVimTopBottomAndSourceLineJumps(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithLines("a.go", 6)}, cursor: 4, width: 80, height: 20}

	m = press(m, "g")
	if m.cursor != 4 {
		t.Fatalf("cursor after first g = %d, want 4", m.cursor)
	}
	m = press(m, "g")
	if m.cursor != 0 {
		t.Fatalf("cursor after gg = %d, want 0", m.cursor)
	}

	m = press(m, "G")
	if m.cursor != len(m.currentRows())-1 {
		t.Fatalf("cursor after G = %d, want %d", m.cursor, len(m.currentRows())-1)
	}

	m = press(m, "3")
	m = press(m, "G")
	if got := cursorSourceLine(m); got != 3 {
		t.Fatalf("source line after 3G = %d, want 3", got)
	}

	m = press(m, "5")
	m = press(m, "g")
	m = press(m, "g")
	if got := cursorSourceLine(m); got != 5 {
		t.Fatalf("source line after 5gg = %d, want 5", got)
	}
}

func TestViewportEdgeNavigation(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithLines("a.go", 12)}, top: 3, cursor: 5, width: 80, height: 12}

	m = press(m, "H")
	if m.cursor != 3 {
		t.Fatalf("cursor after H = %d, want 3", m.cursor)
	}

	m = press(m, "L")
	if m.cursor != 9 {
		t.Fatalf("cursor after L = %d, want 9", m.cursor)
	}
}

func TestWindowPageScrollPreservesCursorOffset(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithLines("a.go", 20)}, top: 2, cursor: 4, width: 80, height: 12}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = next.(Model)
	if m.top != 9 || m.cursor != 11 {
		t.Fatalf("after ctrl+f top/cursor = %d/%d, want 9/11", m.top, m.cursor)
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = next.(Model)
	if m.top != 2 || m.cursor != 4 {
		t.Fatalf("after ctrl+b top/cursor = %d/%d, want 2/4", m.top, m.cursor)
	}
}

func TestHunkNavigationPreservesCursorScreenRow(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithHunks("a.go", 6, 6, 6)}, top: 1, cursor: 3, width: 80, height: 12}

	m = press(press(m, "]"), "c")
	if m.cursor != 7 || m.top != 5 {
		t.Fatalf("after ]c top/cursor = %d/%d, want 5/7", m.top, m.cursor)
	}

	m = Model{files: []diff.FileDiff{testFileWithHunks("a.go", 6, 6, 6)}, top: 10, cursor: 12, width: 80, height: 12}

	m = press(press(m, "["), "c")
	if m.cursor != 7 || m.top != 5 {
		t.Fatalf("after [c top/cursor = %d/%d, want 5/7", m.top, m.cursor)
	}

	m = Model{files: []diff.FileDiff{testFileWithHunks("a.go", 6, 1)}, top: 0, cursor: 4, width: 80, height: 16}

	m = press(press(m, "]"), "c")
	if m.cursor != 7 || m.top != 3 {
		t.Fatalf("after ]c to bottom hunk top/cursor = %d/%d, want 3/7", m.top, m.cursor)
	}
	maxTop := max(0, len(m.currentRows())-m.viewHeight())
	if m.top <= maxTop {
		t.Fatalf("bottom hunk top = %d, want past EOF clamp %d", m.top, maxTop)
	}
}

func TestDiffReloadKeepsSelectedFileByPath(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), selectedFile: 1, cursor: 1, width: 80, height: 20}
	next, _ := m.Update(diffLoadedMsg{
		files: []diff.FileDiff{
			testFile("z.go"),
			testFile("b.go"),
			testFile("a.go"),
		},
	})

	got := next.(Model)
	if got.selectedFile != 1 {
		t.Fatalf("selectedFile = %d, want 1 for b.go", got.selectedFile)
	}
	if got.files[got.selectedFile].Path() != "b.go" {
		t.Fatalf("selected path = %q, want b.go", got.files[got.selectedFile].Path())
	}
	if got.cursor != 1 {
		t.Fatalf("cursor = %d, want restored 1", got.cursor)
	}
}

func TestRowLocationMapping(t *testing.T) {
	t.Parallel()

	file := diff.FileDiff{OldPath: "old.go", NewPath: "new.go", Status: diff.FileModified}

	addRow := uiRow(diff.Line{Kind: diff.LineAdd, NewLine: 7})
	got, ok := currentLocationForRow(file, addRow)
	if !ok || got.Path != "new.go" || got.Line != 7 || got.Column != 1 {
		t.Fatalf("current add location = %+v, %v", got, ok)
	}
	if got, ok := baselineLocationForRow(file, addRow); ok {
		t.Fatalf("baseline add location = %+v, want none", got)
	}

	contextRow := uiRow(diff.Line{Kind: diff.LineContext, OldLine: 4, NewLine: 5})
	got, ok = currentLocationForRow(file, contextRow)
	if !ok || got.Path != "new.go" || got.Line != 5 || got.Column != 1 {
		t.Fatalf("current context location = %+v, %v", got, ok)
	}
	got, ok = baselineLocationForRow(file, contextRow)
	if !ok || got.Path != "old.go" || got.Line != 4 || got.Column != 1 {
		t.Fatalf("baseline context location = %+v, %v", got, ok)
	}

	deleteRow := uiRow(diff.Line{Kind: diff.LineDelete, OldLine: 8})
	if got, ok := currentLocationForRow(file, deleteRow); ok {
		t.Fatalf("current delete location = %+v, want none", got)
	}
	got, ok = baselineLocationForRow(file, deleteRow)
	if !ok || got.Path != "old.go" || got.Line != 8 || got.Column != 1 {
		t.Fatalf("baseline delete location = %+v, %v", got, ok)
	}
}

func TestViewModeTogglePreservesSeparateCursorState(t *testing.T) {
	t.Parallel()

	m := Model{
		files:        []diff.FileDiff{testFileWithLines("a.go", 3)},
		selectedFile: 0,
		cursor:       1,
		width:        80,
		height:       20,
		fileContents: map[string]fileContentState{
			"a.go": {lines: []string{"one", "two", "three"}, loaded: true},
		},
	}

	m = press(m, "tab")
	if m.viewMode != ViewFile {
		t.Fatalf("viewMode = %v, want ViewFile", m.viewMode)
	}
	m.cursor = 2

	m = press(m, "tab")
	if m.viewMode != ViewDiff {
		t.Fatalf("viewMode = %v, want ViewDiff", m.viewMode)
	}
	if m.cursor != 1 {
		t.Fatalf("diff cursor = %d, want 1", m.cursor)
	}

	m = press(m, "tab")
	if m.viewMode != ViewFile {
		t.Fatalf("viewMode = %v, want ViewFile", m.viewMode)
	}
	if m.cursor != 2 {
		t.Fatalf("file cursor = %d, want 2", m.cursor)
	}
}

func TestViewModeFirstExpansionAnchorsSourceLineAndScreenRow(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{viewToggleTestFile("a.go")}
	m := Model{
		files:        files,
		selectedFile: 0,
		cursor:       3, // second hunk's changed line: source line 90
		col:          3,
		desiredCol:   3,
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: numberedLines(100), loaded: true},
		},
	}
	wantScreenRow := m.cursorScreenRow()

	m = press(m, "tab")
	if m.viewMode != ViewFile {
		t.Fatalf("viewMode = %v, want ViewFile", m.viewMode)
	}
	if got := cursorSourceLine(m); got != 90 {
		t.Fatalf("file cursor source line = %d, want 90", got)
	}
	if m.col != 3 {
		t.Fatalf("file cursor column = %d, want 3", m.col)
	}
	if got := m.cursorScreenRow(); got != wantScreenRow {
		t.Fatalf("file cursor screen row = %d, want stable row %d", got, wantScreenRow)
	}
}

func TestViewModeMovedDiffCursorOverridesRememberedFileCursor(t *testing.T) {
	t.Parallel()

	m := Model{
		files:        []diff.FileDiff{viewToggleTestFile("a.go")},
		selectedFile: 0,
		cursor:       1, // source line 10
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: numberedLines(100), loaded: true},
		},
	}

	m = press(m, "tab")
	if idx, ok := rowIndexForNewLine(m.currentRows(), 50); ok {
		m.cursor = idx
	} else {
		t.Fatal("full-file row 50 missing")
	}
	m = press(m, "tab")
	if got := cursorSourceLine(m); got != 10 {
		t.Fatalf("restored diff source line = %d, want 10", got)
	}

	// With no diff movement, expanding resumes the full-file exploration.
	m = press(m, "tab")
	if got := cursorSourceLine(m); got != 50 {
		t.Fatalf("resumed file source line = %d, want 50", got)
	}
	m = press(m, "tab")

	// Moving onto another current-side diff line is an explicit new anchor.
	m = press(m, "j") // hunk header for line 90
	m = press(m, "j") // changed line 90
	m = press(m, "tab")
	if got := cursorSourceLine(m); got != 90 {
		t.Fatalf("re-anchored file source line = %d, want 90", got)
	}
}

func TestViewModeHunkHeaderAnchorsAtCurrentHunkStart(t *testing.T) {
	t.Parallel()

	m := Model{
		files:        []diff.FileDiff{viewToggleTestFile("a.go")},
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: numberedLines(100), loaded: true},
		},
	}

	m = press(m, "tab")
	if idx, ok := rowIndexForNewLine(m.currentRows(), 50); ok {
		m.cursor = idx
	} else {
		t.Fatal("full-file row 50 missing")
	}
	m = press(m, "tab")
	m = press(m, "j") // the second hunk header has no source row of its own
	m = press(m, "tab")
	if got := cursorSourceLine(m); got != 90 {
		t.Fatalf("file source line from hunk header = %d, want 90", got)
	}
}

func TestViewModeDeletedRowAnchorsAtCurrentInsertionLine(t *testing.T) {
	t.Parallel()

	file := diff.FileDiff{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -10,3 +10,2 @@",
			OldStart: 10,
			OldLines: 3,
			NewStart: 10,
			NewLines: 2,
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "ten", OldLine: 10, NewLine: 10},
				{Kind: diff.LineDelete, Content: "deleted eleven", OldLine: 11},
				{Kind: diff.LineContext, Content: "twelve", OldLine: 12, NewLine: 11},
			},
		}},
	}
	m := Model{
		files:        []diff.FileDiff{file},
		selectedFile: 0,
		cursor:       2, // deleted old-side line 11
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: numberedLines(20), loaded: true},
		},
	}

	m = press(m, "tab")
	if got := cursorSourceLine(m); got != 11 {
		t.Fatalf("file source line from deleted row = %d, want insertion line 11", got)
	}
}

func TestViewModeAsyncExpansionAppliesDiffAnchorAfterLoad(t *testing.T) {
	t.Parallel()

	content := numberedLines(100)
	m := Model{
		source:          fakeSource{contents: map[string][]byte{"a.go": []byte(strings.Join(content, "\n") + "\n")}},
		files:           []diff.FileDiff{viewToggleTestFile("a.go")},
		changedPaths:    map[string]bool{"a.go": true},
		selectedFile:    0,
		cursor:          3, // source line 90
		width:           100,
		height:          24,
		fileContents:    make(map[string]fileContentState),
		fileViewAnchors: make(map[string]fileViewAnchor),
	}

	next, cmd := m.handleKey(key("tab"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("full-file expansion returned nil load command")
	}
	if m.viewMode != ViewFile {
		t.Fatalf("viewMode before load = %v, want ViewFile", m.viewMode)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if got := cursorSourceLine(m); got != 90 {
		t.Fatalf("file cursor source line after load = %d, want 90", got)
	}
}

func TestLocalExpansionLoadsContentAndStaysInDiffMode(t *testing.T) {
	t.Parallel()

	content := numberedLines(20)
	files := []diff.FileDiff{testSingleLineHunk("a.go", 10)}
	m := Model{
		source:          fakeSource{contents: map[string][]byte{"a.go": []byte(strings.Join(content, "\n") + "\n")}},
		files:           files,
		changedPaths:    changedPathSet(files),
		selectedFile:    0,
		cursor:          1,
		width:           100,
		height:          24,
		fileContents:    make(map[string]fileContentState),
		localExpansions: make(map[string]map[int]int),
	}

	m = press(m, "z")
	next, cmd := m.handleKey(key("o"))
	got := next.(Model)
	if got.viewMode != ViewDiff {
		t.Fatalf("viewMode = %v, want ViewDiff after local expansion", got.viewMode)
	}
	if cmd == nil {
		t.Fatal("local expansion without content returned nil load command")
	}

	next, _ = got.Update(cmd())
	got = next.(Model)
	rows := got.currentRows()
	if len(rows) <= len(ui.FlattenFile(files, 0)) {
		t.Fatalf("expanded rows = %d, want more than compact %d", len(rows), len(ui.FlattenFile(files, 0)))
	}
	if rows[1].Line.NewLine != 1 {
		t.Fatalf("first expanded source row = %d, want line 1 from loaded content", rows[1].Line.NewLine)
	}
}

func TestFullExpansionTogglePreservesLocalExpansionState(t *testing.T) {
	t.Parallel()

	lines := numberedLines(8)
	files := []diff.FileDiff{testSingleLineHunk("a.go", 4)}
	m := Model{
		files:        files,
		selectedFile: 0,
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: lines, loaded: true},
		},
		localExpansions: map[string]map[int]int{
			"a.go": {0: 1},
		},
	}
	localRows := m.currentRows()
	if len(localRows) != 4 {
		t.Fatalf("local rows = %d, want header plus three rows", len(localRows))
	}

	m = press(m, "z")
	m = press(m, "f")
	if m.viewMode != ViewFile {
		t.Fatalf("viewMode = %v, want full expansion", m.viewMode)
	}
	fullRows := m.currentRows()
	if len(fullRows) != 9 {
		t.Fatalf("full rows = %d, want hunk header plus full file", len(fullRows))
	}

	m = press(m, "z")
	m = press(m, "f")
	if m.viewMode != ViewDiff {
		t.Fatalf("viewMode = %v, want diff/local expansion", m.viewMode)
	}
	restoredRows := m.currentRows()
	if len(restoredRows) != len(localRows) {
		t.Fatalf("restored local rows = %d, want %d", len(restoredRows), len(localRows))
	}
	if m.localExpansions["a.go"][0] != 1 {
		t.Fatalf("local expansion = %d, want preserved 1", m.localExpansions["a.go"][0])
	}
}

func TestCommandPaletteOpensAndCloses(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFile("a.go")}, selectedFile: 0, width: 100, height: 24}

	m = press(m, "?")
	if m.overlay.Kind != OverlayCommandPalette {
		t.Fatalf("? overlay kind = %v, want command palette", m.overlay.Kind)
	}
	if len(m.overlay.Results) == 0 {
		t.Fatal("command palette has no results")
	}
	if m.overlay.CommandCategory != CommandCategoryCode {
		t.Fatalf("initial command category = %q, want code", m.overlay.CommandCategory)
	}
	if got := m.overlayView().Tabs; len(got) != len(commandPaletteCategories) {
		t.Fatalf("command tabs = %v, want %d categories", got, len(commandPaletteCategories))
	} else if got[0] != string(CommandCategoryCode) {
		t.Fatalf("first command tab = %q, want code", got[0])
	}
	if got := m.overlayView().LabelWidth; got != commandNameWidth {
		t.Fatalf("command name width = %d, want %d", got, commandNameWidth)
	}
	for i := 1; i < len(m.overlay.Results); i++ {
		if m.overlay.Results[i-1].Label > m.overlay.Results[i].Label {
			t.Fatalf("commands are not alphabetical: %q before %q", m.overlay.Results[i-1].Label, m.overlay.Results[i].Label)
		}
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(Model)
	if m.overlay.CommandCategory != CommandCategoryReview {
		t.Fatalf("tab category = %q, want review", m.overlay.CommandCategory)
	}
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(Model)
	if m.overlay.CommandCategory != CommandCategoryCode {
		t.Fatalf("shift-tab category = %q, want code", m.overlay.CommandCategory)
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay.Kind != OverlayNone {
		t.Fatalf("esc overlay kind = %v, want none", m.overlay.Kind)
	}

	m = press(m, "g")
	m = press(m, "?")
	if m.overlay.Kind != OverlayCommandPalette {
		t.Fatalf("g? overlay kind = %v, want command palette", m.overlay.Kind)
	}
}

func TestCommandPaletteFiltersByTypedQuery(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFile("a.go")}, selectedFile: 0, width: 100, height: 24}
	m = press(m, "?")
	total := len(m.overlay.Results)

	for _, r := range "jump" {
		m = press(m, string(r))
	}
	if m.overlay.Query != "jump" {
		t.Fatalf("palette query = %q, want jump", m.overlay.Query)
	}
	if got := m.overlayView().Query; got != "jump" {
		t.Fatalf("command palette view query = %q, want jump", got)
	}
	if len(m.overlay.Results) == 0 || len(m.overlay.Results) >= total {
		t.Fatalf("filtered commands = %d, want narrower non-empty set from %d", len(m.overlay.Results), total)
	}
	for _, result := range m.overlay.Results {
		haystack := strings.ToLower(result.Label + " " + result.Preview)
		if !strings.Contains(haystack, "jump") {
			t.Fatalf("filtered command %q / %q does not match jump", result.Label, result.Preview)
		}
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	if m.overlay.Query != "jum" {
		t.Fatalf("palette query after backspace = %q, want jum", m.overlay.Query)
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = next.(Model)
	if m.overlay.Query != "" {
		t.Fatalf("palette query after ctrl+u = %q, want empty", m.overlay.Query)
	}
	if len(m.overlay.Results) != total {
		t.Fatalf("cleared palette results = %d, want %d", len(m.overlay.Results), total)
	}
}

func TestCommandPaletteDefaultsToEditCategoryInEditMode(t *testing.T) {
	t.Parallel()

	m := Model{mode: modeEdit, files: []diff.FileDiff{testFile("a.go")}, selectedFile: 0, width: 100, height: 24}
	m = press(m, "?")
	if m.overlay.CommandCategory != CommandCategoryEdit {
		t.Fatalf("edit-mode command category = %q, want edit", m.overlay.CommandCategory)
	}
	for _, result := range m.overlay.Results {
		if commandByID[result.Location.Path].Category != CommandCategoryEdit {
			t.Fatalf("edit tab contains %q from category %q", result.Label, commandByID[result.Location.Path].Category)
		}
	}
}

func TestCommandPaletteExecutesSelectedCommand(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFile("a.go")}, selectedFile: 0, width: 100, height: 24}
	m = press(m, "?")
	m.setCommandPaletteCategory(3) // View
	for i, result := range m.overlay.Results {
		if result.Location.Path == commandToggleFullFile {
			m.overlay.Cursor = i
			break
		}
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.overlay.Kind != OverlayNone {
		t.Fatalf("overlay kind after command = %v, want none", m.overlay.Kind)
	}
	if m.viewMode != ViewFile {
		t.Fatalf("view mode after palette command = %v, want full file", m.viewMode)
	}
}

func TestCategorizedCommandsHaveOnePaletteEntry(t *testing.T) {
	t.Parallel()

	commands := Commands()
	seen := make(map[string]bool, len(commands))
	categorySeen := make(map[CommandCategory]bool, len(commandPaletteCategories))
	for _, category := range commandPaletteCategories {
		if categorySeen[category] {
			t.Fatalf("duplicate command category %q", category)
		}
		categorySeen[category] = true
		results := commandPaletteResults(category, "")
		if len(results) == 0 {
			t.Fatalf("category %q has no commands", category)
		}
		m := Model{width: 100, height: 24, overlay: overlayState{Kind: OverlayCommandPalette, CommandCategory: category, Results: results}}
		if len(results) > m.overlayPageSize() {
			t.Fatalf("category %q has %d commands but page holds %d", category, len(results), m.overlayPageSize())
		}
		for i, result := range results {
			if seen[result.Location.Path] {
				t.Fatalf("duplicate palette command id %q", result.Location.Path)
			}
			seen[result.Location.Path] = true
			if i > 0 && results[i-1].Label > result.Label {
				t.Fatalf("category %q is not alphabetical: %q before %q", category, results[i-1].Label, result.Label)
			}
		}
	}

	visible := 0
	for i, command := range commands {
		if command.ID == "" || command.Name == "" || command.Execute == nil {
			t.Fatalf("incomplete command at %d: %+v", i, command)
		}
		if command.Category == "" {
			if seen[command.ID] {
				t.Fatalf("uncategorized command %q appeared in palette", command.ID)
			}
			continue
		}
		visible++
		if !categorySeen[command.Category] {
			t.Fatalf("command %q has unknown category %q", command.ID, command.Category)
		}
		if !seen[command.ID] {
			t.Fatalf("categorized command %q is missing from palette", command.ID)
		}
	}
	if len(seen) != visible {
		t.Fatalf("palette entries = %d, categorized commands = %d", len(seen), visible)
	}
	for _, trivial := range []string{commandCursorUp, commandCursorDown, commandCursorLeft, commandCursorRight} {
		if seen[trivial] {
			t.Fatalf("trivial motion %q should not appear in palette", trivial)
		}
	}
}

func TestMouseWheelScrollsMainRows(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithLines("a.go", 20)}, selectedFile: 0, width: 100, height: 12}
	next, _ := m.handleMouse(tea.MouseMsg{
		X:      50,
		Y:      5,
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
		Type:   tea.MouseWheelDown,
	})
	got := next.(Model)
	if got.top == 0 || got.cursor == 0 {
		t.Fatalf("mouse wheel top/cursor = %d/%d, want scrolled", got.top, got.cursor)
	}
}

func TestMouseClickChangeListSelectsFile(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFile("a.go"), testFile("b.go")}
	m := Model{files: files, selectedFile: 0, width: 100, height: 18}
	layout := ui.Layout(m.width, m.height, m.bottomPanelView())

	next, cmd := m.handleMouse(tea.MouseMsg{
		X:      2,
		Y:      layout.ContentY + 1,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Type:   tea.MouseLeft,
	})
	if cmd != nil {
		t.Fatal("clicking diff-mode file list returned content load command")
	}
	got := next.(Model)
	if got.selectedFile != 1 {
		t.Fatalf("selectedFile = %d, want 1", got.selectedFile)
	}
}

func TestMouseClickDiffRowsMovesCursor(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFileWithLines("a.go", 8)}, selectedFile: 0, width: 100, height: 18}
	layout := ui.Layout(m.width, m.height, m.bottomPanelView())

	next, _ := m.handleMouse(tea.MouseMsg{
		X:      layout.DiffContentX + 4,
		Y:      layout.DiffRowsY + 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Type:   tea.MouseLeft,
	})
	got := next.(Model)
	if got.cursor != 3 {
		t.Fatalf("cursor = %d, want clicked row 3", got.cursor)
	}
}

func TestSearchResultJumpLoadsFullFileAndPositionsCursor(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFileWithLines("a.go", 3)}
	m := Model{
		source:       fakeSource{contents: map[string][]byte{"a.go": []byte("one\ntwo\nthree\n")}},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		width:        80,
		height:       20,
		fileContents: make(map[string]fileContentState),
		overlay: overlayState{
			Kind: OverlaySearch,
			Results: []navsearch.Result{{
				Kind:     navsearch.ResultText,
				Location: source.Location{Path: "a.go", Line: 2, Column: 1},
				Label:    "a.go:2:1",
			}},
		},
	}

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.overlay.Kind != OverlayNone {
		t.Fatalf("overlay = %v, want closed", got.overlay.Kind)
	}
	if got.viewMode != ViewFile {
		t.Fatalf("viewMode = %v, want ViewFile", got.viewMode)
	}
	if cmd == nil {
		t.Fatal("accepting unloaded file returned nil command")
	}

	next, _ = got.Update(cmd())
	got = next.(Model)
	if got.viewMode != ViewFile {
		t.Fatalf("viewMode after load = %v, want ViewFile", got.viewMode)
	}
	if got.currentFilePath() != "a.go" {
		t.Fatalf("current path = %q, want a.go", got.currentFilePath())
	}
	if got := cursorSourceLine(got); got != 2 {
		t.Fatalf("cursor source line = %d, want 2", got)
	}
}

func TestGoToDefinitionCommandJumpsToDefinitionResult(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineContext,
				Content: "return Target()",
				OldLine: 1,
				NewLine: 1,
			}},
		}},
	}}
	m := Model{
		source: fakeSource{
			contents: map[string][]byte{
				"target.go": []byte("package p\nfunc Target() {}\n"),
			},
			searchResults: []navsearch.Result{{
				Kind:     navsearch.ResultText,
				Location: source.Location{Path: "target.go", Line: 2, Column: 6},
				Label:    "target.go:2:6",
				Preview:  "func Target() {}",
			}},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        80,
		height:       20,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("d"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gd returned nil command")
	}
	if !got.referencePanel.Open || !got.referencePanel.Loading {
		t.Fatalf("reference panel open/loading = %v/%v, want true/true", got.referencePanel.Open, got.referencePanel.Loading)
	}

	next, loadCmd := got.Update(cmd())
	got = next.(Model)
	if got.currentFilePath() != "target.go" {
		t.Fatalf("current path after definition result = %q, want target.go; query=%+v results=%+v err=%v", got.currentFilePath(), got.referencePanel.Query, got.referencePanel.Results, got.referencePanel.Err)
	}
	if got.viewMode != ViewFile {
		t.Fatalf("viewMode after definition result = %v, want ViewFile", got.viewMode)
	}
	if loadCmd == nil {
		t.Fatal("definition jump returned nil content load command")
	}

	got = applyCmd(got, loadCmd)
	if got := cursorSourceLine(got); got != 2 {
		t.Fatalf("cursor source line = %d, want 2", got)
	}
}

func TestGoToDefinitionUsesLSPBeforeLexicalFallback(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "widget.cpp",
		NewPath: "widget.cpp",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{Lines: []diff.Line{{
			Kind:    diff.LineContext,
			Content: "widget_draw(widget, 1);",
			OldLine: 1,
			NewLine: 1,
		}}}},
	}}
	m := Model{
		source: fakeSource{contents: map[string][]byte{
			"widget_impl.cpp": []byte("static int widget_draw(Widget *self, int x) {\n    return x;\n}\n"),
		}},
		lsp: fakeLSP{
			status:      lsp.Status{Language: "cpp", Command: []string{"clangd"}, State: lsp.StateRunning, Message: "running"},
			definitions: []source.Location{{Path: "widget_impl.cpp", Line: 1, Column: 12}},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("d"))
	if cmd == nil {
		t.Fatal("gd returned nil command")
	}
	next, loadCmd := next.(Model).Update(cmd())
	got := next.(Model)
	if got.currentFilePath() != "widget_impl.cpp" || got.referencePanel.Source != navsearch.ResultSourceLSP {
		t.Fatalf("LSP definition jump = path %q, source %v", got.currentFilePath(), got.referencePanel.Source)
	}
	if got.lspStatuses["cpp"].State != lsp.StateRunning {
		t.Fatalf("recorded LSP status = %+v", got.lspStatuses)
	}
	got = applyCmd(got, loadCmd)
	if line := cursorSourceLine(got); line != 1 {
		t.Fatalf("cursor line = %d, want 1", line)
	}
}

func TestVTableImplementationReferencesIncludeIndirectSlotCalls(t *testing.T) {
	t.Parallel()

	var searched []string
	src := fakeSource{
		searchWords: &searched,
		searchResultsByWord: map[string][]navsearch.Result{
			"draw": {
				{Location: source.Location{Path: "widget.hpp", Line: 4, Column: 11}, Preview: "    int (*draw)(Widget *, int);"},
				{Location: source.Location{Path: "widget.cpp", Line: 12, Column: 6}, Preview: "    .draw = widget_draw,"},
				{Location: source.Location{Path: "canvas.cpp", Line: 20, Column: 18}, Preview: "    widget->vtable->draw(widget, 1);"},
			},
		},
	}
	initial := []navsearch.ReferenceResult{{
		Location: source.Location{Path: "widget.cpp", Line: 12, Column: 13},
		Preview:  "    .draw = widget_draw,",
		Kind:     navsearch.ReferenceReference,
		Source:   navsearch.ResultSourceLSP,
		Side:     navsearch.ResultSideCurrent,
	}}
	got := expandVTableReferences(src, "widget_draw", initial)
	if len(got) != 3 {
		t.Fatalf("expanded references = %#v, want binding, slot, and indirect call", got)
	}
	if strings.Join(searched, ",") != "draw" {
		t.Fatalf("searched slots = %v, want draw", searched)
	}
	foundCall := false
	for _, result := range got {
		if result.Location.Path == "canvas.cpp" && result.Location.Line == 20 {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatalf("indirect call missing from %#v", got)
	}
}

// applyCmd executes a command tree (following batches) and feeds every
// resulting message through Update, mirroring what the bubbletea runtime does.
func applyCmd(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			m = applyCmd(m, sub)
		}
		return m
	}
	if _, isTick := msg.(spinnerTickMsg); isTick {
		return m // don't loop forever on the self-rescheduling spinner
	}
	next, nextCmd := m.Update(msg)
	m = next.(Model)
	return applyCmd(m, nextCmd)
}

func TestChangedFileReferencesCommandFiltersResults(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "Target()",
				NewLine: 1,
			}},
		}},
	}}
	m := Model{
		source: fakeSource{
			searchResults: []navsearch.Result{
				{Kind: navsearch.ResultText, Location: source.Location{Path: "other.go", Line: 2, Column: 1}, Preview: "Target()"},
				{Kind: navsearch.ResultText, Location: source.Location{Path: "a.go", Line: 1, Column: 1}, Preview: "Target()"},
			},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("R"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gR returned nil command")
	}
	if !got.referencePanel.ChangedOnly {
		t.Fatal("referencePanel.ChangedOnly = false, want true")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if len(got.referencePanel.Results) != 1 {
		t.Fatalf("reference results = %d, want 1 changed-file result", len(got.referencePanel.Results))
	}
	if got.referencePanel.Results[0].Location.Path != "a.go" {
		t.Fatalf("reference path = %q, want a.go", got.referencePanel.Results[0].Location.Path)
	}
}

func TestReferencesPromptWhenLineHasMultipleSymbols(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineContext,
				Content: "return Alpha(Beta)",
				OldLine: 1,
				NewLine: 1,
			}},
		}},
	}}
	var searched []string
	m := Model{
		source: fakeSource{
			searchWords: &searched,
			searchResults: []navsearch.Result{{
				Kind:     navsearch.ResultText,
				Location: source.Location{Path: "a.go", Line: 1, Column: 14},
				Preview:  "return Alpha(Beta)",
			}},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("r"))
	got := next.(Model)
	if cmd != nil {
		t.Fatal("gr with multiple symbols returned search command before choosing")
	}
	if got.overlay.Kind != OverlaySymbolChoice {
		t.Fatalf("overlay kind = %v, want OverlaySymbolChoice", got.overlay.Kind)
	}
	if got.referencePanel.Open {
		t.Fatal("reference panel opened before choosing a symbol")
	}
	if len(got.overlay.SymbolQueries) != 2 {
		t.Fatalf("symbol queries = %d, want 2", len(got.overlay.SymbolQueries))
	}
	if got.overlay.SymbolQueries[0].Symbol != "Alpha" || got.overlay.SymbolQueries[1].Symbol != "Beta" {
		t.Fatalf("symbol choices = %+v, want Alpha then Beta", got.overlay.SymbolQueries)
	}

	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got = next.(Model)
	next, cmd = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(Model)
	if cmd == nil {
		t.Fatal("accepting symbol choice returned nil command")
	}
	if got.overlay.Kind != OverlayNone {
		t.Fatalf("overlay kind after choice = %v, want OverlayNone", got.overlay.Kind)
	}
	if !got.referencePanel.Open || !got.referencePanel.Loading {
		t.Fatalf("reference panel open/loading = %v/%v, want true/true", got.referencePanel.Open, got.referencePanel.Loading)
	}
	if got.referencePanel.Query.Symbol != "Beta" {
		t.Fatalf("reference query symbol = %q, want Beta", got.referencePanel.Query.Symbol)
	}

	next, _ = got.Update(cmd())
	got = next.(Model)
	if len(searched) != 1 || searched[0] != "Beta" {
		t.Fatalf("searched words = %+v, want [Beta]", searched)
	}
	if got.referencePanel.Err != nil {
		t.Fatalf("reference panel err = %v", got.referencePanel.Err)
	}
}

func TestFindReferencesCanStartFromDeletedLine(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -2,1 +2,0 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineDelete,
				Content: "return Target()",
				OldLine: 2,
			}},
		}},
	}}
	m := Model{
		source:       fakeSource{},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("r"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gr from deleted line returned nil command")
	}
	if got.referencePanel.Query.Symbol != "Target" {
		t.Fatalf("query symbol = %q, want Target", got.referencePanel.Query.Symbol)
	}
	if got.referencePanel.Query.Side != navsearch.ResultSideBaseline {
		t.Fatalf("query side = %v, want baseline", got.referencePanel.Query.Side)
	}
	if got.referencePanel.Query.Location.Path != "a.go" || got.referencePanel.Query.Location.Line != 2 {
		t.Fatalf("query location = %+v, want a.go:2", got.referencePanel.Query.Location)
	}
}

func TestBaselineReferenceResultJumpsToOldSideDiffRow(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -3,1 +3,0 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineDelete,
				Content: "return Target()",
				OldLine: 3,
			}},
		}},
	}}
	m := Model{
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
		referencePanel: referencePanelState{
			Open:   true,
			Query:  navsearch.SymbolQuery{Symbol: "Target"},
			Source: navsearch.ResultSourceRG,
			Results: []navsearch.ReferenceResult{{
				Location: source.Location{Path: "a.go", Line: 3, Column: 8},
				Side:     navsearch.ResultSideBaseline,
				Source:   navsearch.ResultSourceRG,
			}},
		},
	}

	if cmd := m.acceptReferenceResult(); cmd != nil {
		t.Fatal("jumping to baseline diff row returned unexpected command")
	}
	if m.viewMode != ViewDiff {
		t.Fatalf("viewMode = %v, want ViewDiff", m.viewMode)
	}
	if got := cursorSourceLine(m); got != 3 {
		t.Fatalf("cursor source line = %d, want 3", got)
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) || rows[m.cursor].Line.Kind != diff.LineDelete {
		t.Fatalf("cursor row = %+v, want deleted line", rows[m.cursor])
	}
	panel := m.referencePanelViewValue()
	if len(panel.Results) != 1 {
		t.Fatalf("panel results = %+v, want one result", panel.Results)
	}
	if panel.Results[0].Tone != ui.ResultToneDeleted {
		t.Fatalf("panel result tone = %v, want deleted", panel.Results[0].Tone)
	}
	if strings.Contains(panel.Results[0].Label, "before") || strings.Contains(panel.Results[0].Label, "deleted") {
		t.Fatalf("panel result label has redundant side/change text: %+v", panel.Results[0])
	}
}

func TestSearchResultRowsUseResultSideTone(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -2,1 +2,1 @@",
			Lines: []diff.Line{
				{Kind: diff.LineDelete, Content: "old Target()", OldLine: 2},
				{Kind: diff.LineAdd, Content: "new Target()", NewLine: 2},
			},
		}},
	}}
	m := Model{files: files, changedPaths: changedPathSet(files)}
	review := m.reviewIndex()

	before := navsearch.Result{
		Kind:     navsearch.ResultText,
		Location: source.Location{Path: "a.go", Line: 2, Column: 5},
		Label:    "a.go:2:5",
		Side:     navsearch.ResultSideBaseline,
		Review:   diff.MarkersForIndex(review, "a.go", 2),
	}
	beforeLabel := m.overlayResultLabel(before)
	if beforeLabel != "a.go:2:5" {
		t.Fatalf("baseline label = %q, want compact source label", beforeLabel)
	}
	if got := m.resultTone(before.Kind, before.Location, before.Side, before.Review); got != ui.ResultToneDeleted {
		t.Fatalf("baseline tone = %v, want deleted", got)
	}

	after := navsearch.Result{
		Kind:     navsearch.ResultText,
		Location: source.Location{Path: "a.go", Line: 2, Column: 5},
		Label:    "a.go:2:5",
		Side:     navsearch.ResultSideCurrent,
		Review:   diff.MarkersForIndex(review, "a.go", 2),
	}
	afterLabel := m.overlayResultLabel(after)
	if afterLabel != "a.go:2:5" {
		t.Fatalf("current label = %q, want compact source label", afterLabel)
	}
	if got := m.resultTone(after.Kind, after.Location, after.Side, after.Review); got != ui.ResultToneAdded {
		t.Fatalf("current tone = %v, want added", got)
	}
}

func TestImpactCommandMarksOutsideDiffReferences(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "Target()",
				NewLine: 1,
			}},
		}},
	}}
	m := Model{
		source: fakeSource{
			searchResults: []navsearch.Result{
				{Kind: navsearch.ResultText, Location: source.Location{Path: "a.go", Line: 1, Column: 1}, Preview: "Target()"},
				{Kind: navsearch.ResultText, Location: source.Location{Path: "other.go", Line: 2, Column: 1}, Preview: "Target()"},
			},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("i"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gi returned nil command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if got.referencePanel.Kind != referenceRequestImpact {
		t.Fatalf("reference kind = %v, want impact", got.referencePanel.Kind)
	}
	panel := got.referencePanelViewValue()
	foundOutside := false
	for _, result := range panel.Results {
		if strings.Contains(result.Label, "outside-diff") {
			foundOutside = true
			break
		}
	}
	if !foundOutside {
		t.Fatalf("impact panel missing outside-diff marker: %+v", panel.Results)
	}
}

func TestReferencePanelOrderToggleUsesSourceOrder(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFileWithAddedLine("z.go", 1)}
	m := Model{
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		width:        100,
		height:       24,
		referencePanel: referencePanelState{
			Open: true,
			Query: navsearch.SymbolQuery{
				Symbol: "Target",
			},
			Source: navsearch.ResultSourceRG,
			RawResults: []navsearch.ReferenceResult{
				{Location: source.Location{Path: "z.go", Line: 1, Column: 1}},
				{Location: source.Location{Path: "a.go", Line: 5, Column: 1}},
			},
		},
	}
	m.referencePanel.Results = m.rankReferenceResults(m.referencePanel.RawResults)
	if m.referencePanel.Results[0].Location.Path != "z.go" {
		t.Fatalf("review-ranked top = %q, want changed z.go", m.referencePanel.Results[0].Location.Path)
	}

	next, _ := m.handleKey(key("o"))
	got := next.(Model)
	if got.referencePanel.Order != diff.ResultOrderSource {
		t.Fatalf("reference order = %v, want source", got.referencePanel.Order)
	}
	if got.referencePanel.Results[0].Location.Path != "a.go" {
		t.Fatalf("source-ordered top = %q, want a.go", got.referencePanel.Results[0].Location.Path)
	}
}

func TestReferenceResultJumpKeepsCursorAboveBottomPanel(t *testing.T) {
	t.Parallel()

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i+1)
	}
	files := []diff.FileDiff{testFileWithLines("a.go", len(lines))}
	m := Model{
		source:       fakeSource{},
		files:        files,
		changedPaths: changedPathSet(files),
		viewMode:     ViewFile,
		selectedFile: 0,
		width:        80,
		height:       20,
		fileContents: map[string]fileContentState{
			"a.go": {lines: lines, loaded: true},
		},
		referencePanel: referencePanelState{
			Open:   true,
			Query:  navsearch.SymbolQuery{Symbol: "Target"},
			Source: navsearch.ResultSourceRG,
			Results: []navsearch.ReferenceResult{{
				Location: source.Location{Path: "a.go", Line: 20, Column: 1},
				Source:   navsearch.ResultSourceRG,
			}},
		},
	}

	if cmd := m.acceptReferenceResult(); cmd != nil {
		t.Fatal("jumping to already-loaded reference returned unexpected command")
	}
	if got := cursorSourceLine(m); got != 20 {
		t.Fatalf("cursor source line = %d, want 20", got)
	}
	vh := m.viewHeight()
	if m.cursor >= m.top+vh {
		t.Fatalf("cursor hidden below panel viewport: cursor=%d top=%d viewHeight=%d", m.cursor, m.top, vh)
	}
	wantTop := m.cursor - vh/2
	if m.top != wantTop {
		t.Fatalf("top = %d, want %d for centered panel-sized viewport", m.top, wantTop)
	}
}

func TestDeferredResultJumpCentersAfterFileLoad(t *testing.T) {
	t.Parallel()

	contentLines := make([]string, 20)
	for i := range contentLines {
		contentLines[i] = "line " + strconv.Itoa(i+1)
	}
	files := []diff.FileDiff{testFileWithLines("a.go", 20)}
	m := Model{
		source: fakeSource{
			contents: map[string][]byte{
				"a.go": []byte(strings.Join(contentLines, "\n") + "\n"),
			},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		viewMode:     ViewFile,
		selectedFile: 0,
		width:        80,
		height:       20,
		fileContents: make(map[string]fileContentState),
		referencePanel: referencePanelState{
			Open:   true,
			Query:  navsearch.SymbolQuery{Symbol: "Target"},
			Source: navsearch.ResultSourceRG,
		},
	}

	cmd := m.jumpToLocation(source.Location{Path: "a.go", Line: 20, Column: 1})
	if cmd == nil {
		t.Fatal("jumping to unloaded file returned nil command")
	}
	next, _ := m.Update(cmd())
	got := next.(Model)
	if got := cursorSourceLine(got); got != 20 {
		t.Fatalf("cursor source line = %d, want 20", got)
	}
	wantTop := got.cursor - got.viewHeight()/2
	if got.top != wantTop {
		t.Fatalf("top = %d, want %d after deferred centered jump", got.top, wantTop)
	}
}

func TestSearchCommandErrorRendersWithoutCrash(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFile("a.go")}
	m := Model{
		source:       fakeSource{},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		width:        80,
		height:       20,
		fileContents: make(map[string]fileContentState),
		overlay: overlayState{
			Kind:       OverlaySearch,
			Query:      "needle",
			Loading:    true,
			Generation: 1,
		},
	}

	next, _ := m.Update(searchLoadedMsg{generation: 1, query: "needle", err: errors.New("boom")})
	out := next.(Model).View()
	if !strings.Contains(out, "boom") {
		t.Fatalf("rendered output missing search error:\n%s", out)
	}
}

func TestOverlayPagingScrollsByVisiblePage(t *testing.T) {
	t.Parallel()

	m := Model{
		width:  80,
		height: 20,
		overlay: overlayState{
			Kind:    OverlaySearch,
			Results: numberedResults(30),
		},
	}
	page := m.overlayPageSize()
	if page < 2 {
		t.Fatalf("overlay page size = %d, want at least 2", page)
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	got := next.(Model)
	if got.overlay.Top != page || got.overlay.Cursor != page {
		t.Fatalf("after pgdown top/cursor = %d/%d, want %d/%d", got.overlay.Top, got.overlay.Cursor, page, page)
	}

	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	got = next.(Model)
	if got.overlay.Top != 0 || got.overlay.Cursor != 0 {
		t.Fatalf("after pgup top/cursor = %d/%d, want 0/0", got.overlay.Top, got.overlay.Cursor)
	}
}

func TestOverlayArrowScrollsSelectedResultIntoView(t *testing.T) {
	t.Parallel()

	m := Model{
		width:  80,
		height: 20,
		overlay: overlayState{
			Kind:    OverlaySearch,
			Results: numberedResults(30),
		},
	}
	page := m.overlayPageSize()
	for i := 0; i < page; i++ {
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	if m.overlay.Cursor != page {
		t.Fatalf("cursor = %d, want %d", m.overlay.Cursor, page)
	}
	if m.overlay.Top != 1 {
		t.Fatalf("top = %d, want 1 after cursor moves past first page", m.overlay.Top)
	}
}

func TestLSPStatusRendersInFooter(t *testing.T) {
	t.Parallel()

	m := Model{
		source:       fakeSource{},
		lsp:          fakeLSP{status: lsp.Status{Language: "go", State: lsp.StateRunning}},
		files:        []diff.FileDiff{testFile("a.go")},
		changedPaths: map[string]bool{"a.go": true},
		selectedFile: 0,
		width:        100,
		height:       20,
		fileContents: make(map[string]fileContentState),
	}

	out := m.View()
	if !strings.Contains(out, "go") || !strings.Contains(out, "running") {
		t.Fatalf("footer missing lsp status:\n%s", out)
	}
}

func TestDiagnosticsUpdateMarkersAndRankChangedLines(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFileWithAddedLine("a.go", 2), testFileWithAddedLine("b.go", 1)}
	diagnostics := []lsp.Diagnostic{
		{
			Range:    source.Range{Start: source.Location{Path: "b.go", Line: 1, Column: 1}},
			Severity: lsp.DiagnosticError,
			Message:  "b changed file",
		},
		{
			Range:    source.Range{Start: source.Location{Path: "a.go", Line: 2, Column: 1}},
			Severity: lsp.DiagnosticWarning,
			Message:  "a changed line",
		},
	}
	m := Model{
		source:       fakeSource{},
		lsp:          fakeLSP{status: lsp.Status{Language: "go", State: lsp.StateRunning}, workspaceDiagnostics: diagnostics},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	cmd := m.openDiagnosticsPanel(true)
	if cmd == nil {
		t.Fatal("workspace diagnostics returned nil command")
	}
	next, _ := m.Update(cmd())
	got := next.(Model)
	if len(got.enrichmentPanel.Results) != 2 {
		t.Fatalf("diagnostic results = %d, want 2", len(got.enrichmentPanel.Results))
	}
	panel := got.enrichmentPanelViewValue()
	if panel.Results[0].Tone != ui.ResultToneAdded {
		t.Fatalf("first diagnostic tone = %v, want added", panel.Results[0].Tone)
	}
	if strings.Contains(got.enrichmentPanel.Results[0].Label, "added") {
		t.Fatalf("first diagnostic label has redundant added marker: %q", got.enrichmentPanel.Results[0].Label)
	}
	rows := got.renderRows()
	foundMarker := false
	for _, row := range rows {
		if row.Kind == ui.RowLine && row.Line.NewLine == 2 {
			foundMarker = row.DiagnosticMarker == "W"
			break
		}
	}
	if !foundMarker {
		t.Fatalf("line 2 did not receive warning marker: %+v", rows)
	}
}

func TestHoverPopupFormatsAndDismissesOnMovement(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "return Target()", OldLine: 1, NewLine: 1},
				{Kind: diff.LineContext, Content: "return Other()", OldLine: 2, NewLine: 2},
			},
		}},
	}}
	m := Model{
		source:       fakeSource{},
		lsp:          fakeLSP{status: lsp.Status{Language: "go", State: lsp.StateRunning}, hover: lsp.Hover{Contents: "**Target**\n```go\nfunc Target()\n```"}},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	next, cmd := m.handleKey(key("K"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("K returned nil hover command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if !got.enrichmentPanel.Open || got.enrichmentPanel.Kind != enrichmentPanelHover {
		t.Fatalf("hover panel open/kind = %v/%v", got.enrichmentPanel.Open, got.enrichmentPanel.Kind)
	}
	out := got.View()
	if strings.Contains(out, "```") || !strings.Contains(out, "func Target()") {
		t.Fatalf("hover output not formatted:\n%s", out)
	}

	next, _ = got.handleKey(key("j"))
	got = next.(Model)
	if got.enrichmentPanel.Open {
		t.Fatal("hover panel remained open after movement")
	}
	if got.cursor != 2 {
		t.Fatalf("cursor = %d, want movement to continue to row 2", got.cursor)
	}
}

func TestDocumentSymbolsJumpToLine(t *testing.T) {
	t.Parallel()

	lines := []string{"package p", "func Target() {}", "func Other() {}"}
	files := []diff.FileDiff{testFileWithLines("a.go", len(lines))}
	m := Model{
		source: fakeSource{contents: map[string][]byte{
			"a.go": []byte(strings.Join(lines, "\n") + "\n"),
		}},
		lsp: fakeLSP{
			status: lsp.Status{Language: "go", State: lsp.StateRunning},
			documentSymbols: []lsp.DocumentSymbol{{
				Name:           "Target",
				Kind:           lsp.SymbolFunction,
				SelectionRange: source.Range{Start: source.Location{Path: "a.go", Line: 2, Column: 6}},
			}},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		viewMode:     ViewFile,
		selectedFile: 0,
		width:        100,
		height:       24,
		fileContents: map[string]fileContentState{
			"a.go": {lines: lines, loaded: true},
		},
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("s"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gs returned nil command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if len(got.enrichmentPanel.Results) != 1 {
		t.Fatalf("symbol results = %d, want 1", len(got.enrichmentPanel.Results))
	}
	if cmd := got.acceptEnrichmentResult(); cmd != nil {
		t.Fatal("jumping to loaded file returned unexpected command")
	}
	if got := cursorSourceLine(got); got != 2 {
		t.Fatalf("cursor source line = %d, want 2", got)
	}
}

func TestWorkspaceSymbolsRankChangedFilesAndJump(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{testFileWithLines("changed.go", 3)}
	m := Model{
		source: fakeSource{contents: map[string][]byte{
			"changed.go": []byte("package p\nfunc Changed() {}\n"),
			"other.go":   []byte("package p\nfunc Other() {}\n"),
		}},
		lsp: fakeLSP{
			status: lsp.Status{Language: "go", State: lsp.StateRunning},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}
	m.openWorkspaceSymbolOverlay()
	m.overlay.Query = "Target"

	next, _ := m.Update(workspaceSymbolsLoadedMsg{
		generation: m.overlay.Generation,
		query:      "Target",
		status:     lsp.Status{Language: "go", State: lsp.StateRunning},
		results: []lsp.WorkspaceSymbol{
			{Name: "Other", Kind: lsp.SymbolFunction, Location: source.Location{Path: "other.go", Line: 2, Column: 6}},
			{Name: "Changed", Kind: lsp.SymbolFunction, Location: source.Location{Path: "changed.go", Line: 2, Column: 6}},
		},
	})
	got := next.(Model)
	if len(got.overlay.Results) != 2 {
		t.Fatalf("workspace symbol results = %d, want 2", len(got.overlay.Results))
	}
	if got.overlay.Results[0].Location.Path != "changed.go" {
		t.Fatalf("first workspace symbol path = %q, want changed.go", got.overlay.Results[0].Location.Path)
	}
	cmd := got.acceptOverlayResult()
	if cmd == nil {
		t.Fatal("accepting unloaded workspace symbol returned nil command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if got.currentFilePath() != "changed.go" || got.viewMode != ViewFile {
		t.Fatalf("jump path/mode = %q/%v, want changed.go/ViewFile", got.currentFilePath(), got.viewMode)
	}
}

func TestCallHierarchyPanelShowsCalls(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "Target()",
				NewLine: 1,
			}},
		}},
	}}
	m := Model{
		source: fakeSource{},
		lsp: fakeLSP{
			status: lsp.Status{Language: "go", State: lsp.StateRunning},
			calls: []lsp.CallHierarchyCall{{
				Name:     "Caller",
				Kind:     lsp.SymbolFunction,
				Location: source.Location{Path: "caller.go", Line: 3, Column: 6},
				Preview:  "func Caller()",
			}},
		},
		files:        files,
		changedPaths: changedPathSet(files),
		selectedFile: 0,
		cursor:       1,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("I"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gI returned nil command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if len(got.enrichmentPanel.Results) != 1 {
		t.Fatalf("call results = %d, want 1", len(got.enrichmentPanel.Results))
	}
	if !strings.Contains(got.enrichmentPanel.Title, "Incoming calls") || !strings.Contains(got.enrichmentPanel.Results[0].Label, "Caller") {
		t.Fatalf("call hierarchy panel = %q %+v", got.enrichmentPanel.Title, got.enrichmentPanel.Results)
	}
}

func TestMissingLanguageServerReportsUnavailable(t *testing.T) {
	t.Parallel()

	m := Model{
		source:       fakeSource{},
		files:        []diff.FileDiff{testFile("a.go")},
		changedPaths: map[string]bool{"a.go": true},
		selectedFile: 0,
		width:        100,
		height:       24,
		fileContents: make(map[string]fileContentState),
	}
	m = press(m, "g")
	next, cmd := m.handleKey(key("s"))
	got := next.(Model)
	if cmd == nil {
		t.Fatal("gs with missing server returned nil command")
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if got.enrichmentPanel.Err == nil || !strings.Contains(got.enrichmentPanel.Err.Error(), "no language server configured") {
		t.Fatalf("missing server err = %v, want no language server configured", got.enrichmentPanel.Err)
	}
}

func key(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m Model, s string) Model {
	next, _ := m.handleKey(key(s))
	return next.(Model)
}

func testFiles() []diff.FileDiff {
	return []diff.FileDiff{
		testFile("a.go"),
		testFile("b.go"),
		testFile("c.go"),
	}
}

func testFile(path string) diff.FileDiff {
	return diff.FileDiff{
		NewPath: path,
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "line",
				NewLine: 1,
			}},
		}},
	}
}

func testFileWithLines(path string, lineCount int) diff.FileDiff {
	lines := make([]diff.Line, 0, lineCount)
	for i := 1; i <= lineCount; i++ {
		lines = append(lines, diff.Line{
			Kind:    diff.LineContext,
			Content: "line",
			OldLine: i,
			NewLine: i,
		})
	}
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines:  lines,
		}},
	}
}

func testFileWithHunks(path string, lineCounts ...int) diff.FileDiff {
	hunks := make([]diff.Hunk, 0, len(lineCounts))
	lineNum := 1
	for _, lineCount := range lineCounts {
		lines := make([]diff.Line, 0, lineCount)
		start := lineNum
		for i := 0; i < lineCount; i++ {
			lines = append(lines, diff.Line{
				Kind:    diff.LineContext,
				Content: "line",
				OldLine: lineNum,
				NewLine: lineNum,
			})
			lineNum++
		}
		hunks = append(hunks, diff.Hunk{
			Header: "@@ -" + strconv.Itoa(start) + "," + strconv.Itoa(lineCount) + " +" + strconv.Itoa(start) + "," + strconv.Itoa(lineCount) + " @@",
			Lines:  lines,
		})
	}
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks:   hunks,
	}
}

func testFileWithAddedLine(path string, addedLine int) diff.FileDiff {
	lines := []diff.Line{}
	for i := 1; i <= addedLine; i++ {
		kind := diff.LineContext
		if i == addedLine {
			kind = diff.LineAdd
		}
		line := diff.Line{
			Kind:    kind,
			Content: "line",
			NewLine: i,
		}
		if kind != diff.LineAdd {
			line.OldLine = i
		}
		lines = append(lines, line)
	}
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Added:   1,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,1 +1,1 @@",
			Lines:  lines,
		}},
	}
}

func testSingleLineHunk(path string, lineNum int) diff.FileDiff {
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Added:   1,
		Hunks: []diff.Hunk{{
			Header:   "@@ -" + strconv.Itoa(lineNum) + ",1 +" + strconv.Itoa(lineNum) + ",1 @@",
			NewStart: lineNum,
			NewLines: 1,
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "line " + strconv.Itoa(lineNum),
				NewLine: lineNum,
			}},
		}},
	}
}

func viewToggleTestFile(path string) diff.FileDiff {
	return diff.FileDiff{
		OldPath: path,
		NewPath: path,
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{
			{
				Header:   "@@ -10,1 +10,1 @@",
				OldStart: 10,
				OldLines: 1,
				NewStart: 10,
				NewLines: 1,
				Lines: []diff.Line{{
					Kind: diff.LineAdd, Content: "line 10", NewLine: 10,
				}},
			},
			{
				Header:   "@@ -90,1 +90,1 @@",
				OldStart: 90,
				OldLines: 1,
				NewStart: 90,
				NewLines: 1,
				Lines: []diff.Line{{
					Kind: diff.LineAdd, Content: "line 90", NewLine: 90,
				}},
			},
		},
	}
}

func numberedLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i+1)
	}
	return lines
}

func cursorSourceLine(m Model) int {
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0
	}
	return sourceLine(rows[m.cursor])
}

func uiRow(line diff.Line) ui.Row {
	return ui.Row{Kind: ui.RowLine, Line: line}
}

func numberedResults(n int) []navsearch.Result {
	results := make([]navsearch.Result, 0, n)
	for i := 0; i < n; i++ {
		results = append(results, navsearch.Result{
			Kind:     navsearch.ResultText,
			Location: source.Location{Path: "a.go", Line: i + 1, Column: 1},
			Label:    "result-" + strconv.Itoa(i),
		})
	}
	return results
}

type fakeSource struct {
	contents            map[string][]byte
	baselineContents    map[string][]byte
	projectFiles        []string
	searchResults       []navsearch.Result
	searchResultsByWord map[string][]navsearch.Result
	searchErr           error
	searchWords         *[]string
	root                string
}

func (s fakeSource) Diff() ([]byte, error) { return nil, nil }

func (s fakeSource) Baseline() string { return "HEAD" }

func (s fakeSource) Root() string {
	if s.root != "" {
		return s.root
	}
	return "."
}

func (s fakeSource) CurrentContent(path string) ([]byte, error) {
	if s.contents != nil {
		if content, ok := s.contents[path]; ok {
			return content, nil
		}
	}
	return nil, errors.New("missing content")
}

func (s fakeSource) BaselineContent(path string) ([]byte, error) {
	if s.baselineContents != nil {
		if content, ok := s.baselineContents[path]; ok {
			return content, nil
		}
	}
	return nil, errors.New("missing content")
}

func (s fakeSource) ChangedPaths() ([]string, error) { return nil, nil }

func (s fakeSource) ProjectFiles() ([]string, error) { return s.projectFiles, nil }

func (s fakeSource) Search(query string) ([]navsearch.Result, error) {
	return s.searchResults, s.searchErr
}

func (s fakeSource) SearchWord(word string) ([]navsearch.Result, error) {
	if s.searchWords != nil {
		*s.searchWords = append(*s.searchWords, word)
	}
	if results, ok := s.searchResultsByWord[word]; ok {
		return results, s.searchErr
	}
	return s.searchResults, s.searchErr
}

type fakeLSP struct {
	status               lsp.Status
	definitions          []source.Location
	references           []source.Location
	diagnostics          []lsp.Diagnostic
	workspaceDiagnostics []lsp.Diagnostic
	hover                lsp.Hover
	documentSymbols      []lsp.DocumentSymbol
	workspaceSymbols     []lsp.WorkspaceSymbol
	calls                []lsp.CallHierarchyCall
	err                  error
}

func (c fakeLSP) Status(path string) lsp.Status {
	if c.status.Enabled() {
		return c.status
	}
	return lsp.Status{Language: "go", State: lsp.StateRunning}
}

func (c fakeLSP) Definition(loc source.Location) ([]source.Location, lsp.Status, error) {
	return c.definitions, c.Status(loc.Path), c.err
}

func (c fakeLSP) References(loc source.Location, _ bool) ([]source.Location, lsp.Status, error) {
	return c.references, c.Status(loc.Path), c.err
}

func (c fakeLSP) Diagnostics(path string) ([]lsp.Diagnostic, lsp.Status, error) {
	return c.diagnostics, c.Status(path), c.err
}

func (c fakeLSP) WorkspaceDiagnostics(paths []string) ([]lsp.Diagnostic, lsp.Status, error) {
	return c.workspaceDiagnostics, c.Status(""), c.err
}

func (c fakeLSP) Hover(symbol string, loc source.Location) (lsp.Hover, lsp.Status, error) {
	hover := c.hover
	hover.Location = loc
	return hover, c.Status(loc.Path), c.err
}

func (c fakeLSP) DocumentSymbols(path string) ([]lsp.DocumentSymbol, lsp.Status, error) {
	return c.documentSymbols, c.Status(path), c.err
}

func (c fakeLSP) WorkspaceSymbols(query string) ([]lsp.WorkspaceSymbol, lsp.Status, error) {
	return c.workspaceSymbols, c.Status(""), c.err
}

func (c fakeLSP) CallHierarchy(symbol string, loc source.Location, direction lsp.CallDirection) ([]lsp.CallHierarchyCall, lsp.Status, error) {
	return c.calls, c.Status(loc.Path), c.err
}
