package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/diffsource"
	navsearch "cride/internal/search"
	"cride/internal/ui"
)

// editTestSource is a minimal in-memory diff source for edit-mode tests.
type editTestSource struct {
	root    string
	content map[string]string
}

func (s *editTestSource) Diff() ([]byte, error) { return nil, nil }
func (s *editTestSource) CurrentContent(path string) ([]byte, error) {
	return []byte(s.content[path]), nil
}
func (s *editTestSource) BaselineContent(path string) ([]byte, error) { return nil, nil }
func (s *editTestSource) ChangedPaths() ([]string, error)             { return nil, nil }
func (s *editTestSource) ProjectFiles() ([]string, error)             { return nil, nil }
func (s *editTestSource) Search(string) ([]navsearch.Result, error)   { return nil, nil }
func (s *editTestSource) SearchWord(string) ([]navsearch.Result, error) {
	return nil, nil
}
func (s *editTestSource) Baseline() string { return "HEAD" }
func (s *editTestSource) Root() string     { return s.root }

var _ diffsource.Source = (*editTestSource)(nil)

// editTestModel builds a model in ViewFile mode with a loaded buffer so mode
// entry is synchronous.
func editTestModel(t *testing.T, lines ...string) Model {
	t.Helper()
	root := t.TempDir()
	src := &editTestSource{root: root, content: map[string]string{"a.go": strings.Join(lines, "\n") + "\n"}}
	file := diff.FileDiff{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -1,1 +1,1 @@",
			NewStart: 1, NewLines: 1,
			Lines: []diff.Line{{Kind: diff.LineAdd, Content: lines[0], NewLine: 1}},
		}},
	}
	m := Model{
		source:       src,
		files:        []diff.FileDiff{file},
		width:        100,
		height:       24,
		viewMode:     ViewFile,
		fileContents: map[string]fileContentState{"a.go": {lines: append([]string(nil), lines...), loaded: true}},
		fileStates:   make(map[fileStateKey]fileState),
	}
	rows := m.currentRows()
	for i, r := range rows {
		if r.IsLineRow() && r.Line.NewLine == 1 {
			m.cursor = i
			break
		}
	}
	return m
}

func bufferLines(t *testing.T, m Model) []string {
	t.Helper()
	lines, ok := m.editBufferLines()
	if !ok {
		t.Fatal("edit buffer unavailable")
	}
	return lines
}

func pressEsc(m Model) Model {
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	return next.(Model)
}

func typeText(m Model, s string) Model {
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func TestInsertModeTypesAndSaves(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "hello world", "second line")
	// Mark the file read first: the seen re-stamp only applies to files the
	// reviewer had already acknowledged.
	m.seen = map[string]string{"a.go": fileDiffHash(m.files[0])}
	m = press(m, "i")
	if m.mode != modeInsert {
		t.Fatalf("mode after i = %v, want insert", m.mode)
	}
	if !m.editLockHeld {
		t.Fatal("edit lock not acquired on entry")
	}
	lockPath := editLockFilePath(m.source.Root())
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}

	m = typeText(m, "XY")
	if got := bufferLines(t, m)[0]; got != "XYhello world" {
		t.Fatalf("line after typing = %q", got)
	}
	if !m.editDirty {
		t.Fatal("buffer not dirty after typing")
	}

	m = pressEsc(m)
	if m.mode != modeEdit {
		t.Fatalf("mode after esc = %v, want edit", m.mode)
	}

	// ZZ writes the file and returns to review.
	m = press(m, "Z")
	m = press(m, "Z")
	if m.mode != modeReview {
		t.Fatalf("mode after ZZ = %v, want review", m.mode)
	}
	if m.editLockHeld {
		t.Fatal("edit lock still held after save")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(m.source.Root(), "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "XYhello world\nsecond line\n" {
		t.Fatalf("saved content = %q", data)
	}
	if m.pendingSeenPath != "a.go" {
		t.Fatalf("pendingSeenPath = %q, want a.go", m.pendingSeenPath)
	}
}

func TestEnteringEditPreservesCursorScreenRow(t *testing.T) {
	t.Parallel()

	lines := numberedLines(100)
	root := t.TempDir()
	src := &editTestSource{root: root, content: map[string]string{"a.go": strings.Join(lines, "\n") + "\n"}}
	m := Model{
		source:       src,
		files:        []diff.FileDiff{testSingleLineHunk("a.go", 60)},
		width:        100,
		height:       24,
		viewMode:     ViewDiff,
		fileContents: map[string]fileContentState{"a.go": {lines: lines, loaded: true}},
		fileStates:   make(map[fileStateKey]fileState),
		cursor:       1, // changed line below the compact diff's hunk header
	}
	wantScreenRow := m.cursorScreenRow()

	m = press(m, "i")
	if m.mode != modeInsert || m.viewMode != ViewFile {
		t.Fatalf("entry mode/view = %v/%v, want insert/full-file", m.mode, m.viewMode)
	}
	if got := cursorSourceLine(m); got != 60 {
		t.Fatalf("source line after entry = %d, want 60", got)
	}
	if got := m.cursorScreenRow(); got != wantScreenRow {
		t.Fatalf("cursor screen row after entry = %d, want preserved row %d", got, wantScreenRow)
	}
}

func TestInsertEnterBackspaceAndJoin(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "abcdef")
	m = press(m, "l")
	m = press(m, "l")
	m = press(m, "l") // col 3
	m = press(m, "i")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if got := bufferLines(t, m); len(got) != 2 || got[0] != "abc" || got[1] != "def" {
		t.Fatalf("lines after enter = %v", got)
	}
	// Backspace at column 0 joins back.
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	if got := bufferLines(t, m); len(got) != 1 || got[0] != "abcdef" {
		t.Fatalf("lines after join = %v", got)
	}
	if m.col != 3 {
		t.Fatalf("col after join = %d, want 3", m.col)
	}
}

func TestEditOperatorsAndUndoRedo(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "one two three", "four five")
	m = press(m, "i")
	m = pressEsc(m) // EDIT mode, clean

	// dw deletes "one " and the register holds it.
	m = press(m, "d")
	m = press(m, "w")
	if got := bufferLines(t, m)[0]; got != "two three" {
		t.Fatalf("line after dw = %q", got)
	}
	if m.editRegister.linewise || len(m.editRegister.lines) != 1 || m.editRegister.lines[0] != "one " {
		t.Fatalf("register after dw = %+v", m.editRegister)
	}

	// x deletes the character under the cursor.
	m = press(m, "x")
	if got := bufferLines(t, m)[0]; got != "wo three" {
		t.Fatalf("line after x = %q", got)
	}

	// dd removes the whole line, linewise.
	m = press(m, "d")
	m = press(m, "d")
	if got := bufferLines(t, m); len(got) != 1 || got[0] != "four five" {
		t.Fatalf("lines after dd = %v", got)
	}
	if !m.editRegister.linewise {
		t.Fatal("dd register not linewise")
	}

	// u unwinds all three edits.
	m = press(m, "u")
	m = press(m, "u")
	m = press(m, "u")
	if got := bufferLines(t, m); got[0] != "one two three" || len(got) != 2 {
		t.Fatalf("lines after 3x undo = %v", got)
	}
	// ctrl+r reapplies one.
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if got := bufferLines(t, m)[0]; got != "two three" {
		t.Fatalf("line after redo = %q", got)
	}
}

func TestEditOperatorCountsCompose(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "one two three four five", "second", "third")
	m = press(m, "i")
	m = pressEsc(m)

	// Counts before and after an operator multiply, like vim.
	m = press(m, "2")
	m = press(m, "d")
	m = press(m, "2")
	m = press(m, "w")
	if got := bufferLines(t, m)[0]; got != "five" {
		t.Fatalf("line after 2d2w = %q, want five", got)
	}

	m = press(m, "u")
	m = press(m, "2")
	m = press(m, "d")
	m = press(m, "d")
	if got := bufferLines(t, m); len(got) != 1 || got[0] != "third" {
		t.Fatalf("lines after 2dd = %v, want [third]", got)
	}
}

func TestUndoBackToOriginalClearsDirtyState(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "original")
	m = press(m, "i")
	m = typeText(m, "X")
	m = pressEsc(m)
	if !m.editDirty {
		t.Fatal("typed edit was not dirty")
	}
	m = press(m, "u")
	if m.editDirty {
		t.Fatal("undo to entry-state buffer remained dirty")
	}
	if got := bufferLines(t, m)[0]; got != "original" {
		t.Fatalf("line after undo = %q", got)
	}

	// A clean edit session can return to review with Esc.
	m = pressEsc(m)
	if m.mode != modeReview {
		t.Fatalf("mode after clean esc = %v, want review", m.mode)
	}
}

func TestInsertModeCursorNavigation(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "abc", "de")
	m = press(m, "i")

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	m = typeText(m, "X")
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	m = typeText(m, "Y")
	if got := bufferLines(t, m)[0]; got != "aXbcY" {
		t.Fatalf("first line after right/end edits = %q", got)
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(Model)
	m = typeText(m, "Q")
	if got := bufferLines(t, m)[1]; got != "Qde" {
		t.Fatalf("second line after down/home edit = %q", got)
	}
}

func TestReplaceSubstituteAndJoin(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "abc", "  def", "ghi")
	m = press(m, "i")
	m = pressEsc(m)

	m = press(m, "2")
	m = press(m, "r")
	m = press(m, "X")
	if got := bufferLines(t, m)[0]; got != "XXc" {
		t.Fatalf("line after 2rX = %q", got)
	}

	m = press(m, "0")
	m = press(m, "s")
	m = typeText(m, "A")
	m = pressEsc(m)
	if got := bufferLines(t, m)[0]; got != "AXc" {
		t.Fatalf("line after substitute = %q", got)
	}

	m = press(m, "J")
	if got := bufferLines(t, m); len(got) != 2 || got[0] != "AXc def" {
		t.Fatalf("lines after J = %v", got)
	}
	m = press(m, "u")
	if got := bufferLines(t, m); len(got) != 3 || got[1] != "  def" {
		t.Fatalf("lines after undoing J = %v", got)
	}
}

func TestLinewisePasteAndOpenLine(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "alpha", "beta")
	m = press(m, "i")
	m = pressEsc(m)

	m = press(m, "y")
	m = press(m, "y")
	if !m.editRegister.linewise || m.editRegister.lines[0] != "alpha" {
		t.Fatalf("register after yy = %+v", m.editRegister)
	}
	m = press(m, "p")
	if got := bufferLines(t, m); len(got) != 3 || got[1] != "alpha" {
		t.Fatalf("lines after p = %v", got)
	}

	// o opens a line below and lands in insert mode.
	m = press(m, "o")
	if m.mode != modeInsert {
		t.Fatalf("mode after o = %v, want insert", m.mode)
	}
	m = typeText(m, "new")
	m = pressEsc(m)
	if got := bufferLines(t, m); got[2] != "new" {
		t.Fatalf("lines after o typing = %v", got)
	}
}

func TestChangeOperatorEntersInsert(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "one two")
	m = press(m, "i")
	m = pressEsc(m)
	m = press(m, "c")
	m = press(m, "w")
	if m.mode != modeInsert {
		t.Fatalf("mode after cw = %v, want insert", m.mode)
	}
	m = typeText(m, "uno ")
	m = pressEsc(m)
	if got := bufferLines(t, m)[0]; got != "uno two" {
		t.Fatalf("line after cw = %q", got)
	}
}

func TestEscWithDirtyBufferWarnsAndZQDiscards(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "content")
	m = press(m, "i")
	m = typeText(m, "zz")
	m = pressEsc(m) // insert -> edit
	m = pressEsc(m) // dirty: stays in edit with a warning
	if m.mode != modeEdit {
		t.Fatalf("esc with dirty buffer left edit mode (mode=%v)", m.mode)
	}
	if m.status.text == "" || !strings.Contains(m.status.text, "ZZ") {
		t.Fatalf("no unsaved-edits hint shown (toast=%q)", m.status.text)
	}
	m = press(m, "Z")
	m = press(m, "Q")
	if m.mode != modeReview {
		t.Fatalf("mode after ZQ = %v, want review", m.mode)
	}
	if m.editLockHeld {
		t.Fatal("lock held after discard")
	}
	// The dirty buffer was dropped; exit immediately kicks a re-read from
	// disk, so the entry is either gone or back in the loading state.
	if state, ok := m.fileContents["a.go"]; ok && state.loaded {
		t.Fatalf("dirty buffer survived discard: %+v", state)
	}
	// Nothing was written.
	data, err := os.ReadFile(filepath.Join(m.source.Root(), "a.go"))
	if err == nil && strings.Contains(string(data), "zz") {
		t.Fatalf("discarded edit reached disk: %q", data)
	}
}

func TestReviewKeysSuspendedInEditMode(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "one", "two")
	m = press(m, "i")
	m = pressEsc(m)
	// R (mark read) and e (export) must not fire their review actions.
	m = press(m, "R")
	if len(m.seen) != 0 {
		t.Fatal("R marked a file read while in edit mode")
	}
	m = press(m, "e") // word-end motion, not export
	if m.mode != modeEdit {
		t.Fatalf("e changed mode: %v", m.mode)
	}
	// q must not quit: it is not in the edit whitelist.
	next, cmd := m.handleKey(key("q"))
	m = next.(Model)
	if cmd != nil {
		t.Fatal("q produced a command in edit mode")
	}
	if m.mode != modeEdit {
		t.Fatalf("q changed mode: %v", m.mode)
	}
}

func TestTreeChangeDeferredWhileEditing(t *testing.T) {
	t.Parallel()

	m := editTestModel(t, "one")
	m.watchCh = make(chan struct{}, 1)
	m = press(m, "i")
	next, _ := m.Update(treeChangedMsg{})
	m = next.(Model)
	if !m.treeChanged {
		t.Fatal("treeChanged not flagged while editing")
	}
	if m.loadSeq != 0 {
		t.Fatal("reload started while an edit buffer was open")
	}
	// Exiting a clean buffer runs the deferred reload.
	m = pressEsc(m) // insert -> edit (buffer clean: nothing typed)
	m = pressEsc(m) // edit -> review
	if m.mode != modeReview {
		t.Fatalf("mode = %v, want review", m.mode)
	}
	if m.loadSeq == 0 {
		t.Fatal("deferred reload did not start on exit")
	}
}

func TestEditingGuardsReadOnlyRows(t *testing.T) {
	t.Parallel()

	// A file with a baseline-deleted block: the delete row is read-only.
	file := diff.FileDiff{
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -1,2 +1,1 @@",
			NewStart: 1, NewLines: 1,
			Lines: []diff.Line{
				{Kind: diff.LineDelete, Content: "gone", OldLine: 1},
				{Kind: diff.LineContext, Content: "kept", OldLine: 2, NewLine: 1},
			},
		}},
	}
	root := t.TempDir()
	src := &editTestSource{root: root, content: map[string]string{"a.go": "kept\n"}}
	m := Model{
		source:       src,
		files:        []diff.FileDiff{file},
		width:        100,
		height:       24,
		viewMode:     ViewFile,
		fileContents: map[string]fileContentState{"a.go": {lines: []string{"kept"}, loaded: true}},
		fileStates:   make(map[fileStateKey]fileState),
	}
	rows := m.currentRows()
	deleteRow := -1
	for i, r := range rows {
		if r.Kind == ui.RowLine && r.Line.Kind == diff.LineDelete {
			deleteRow = i
			break
		}
	}
	if deleteRow < 0 {
		t.Fatal("no delete row rendered")
	}
	m.cursor = deleteRow
	m = press(m, "i")
	if m.mode != modeReview {
		t.Fatalf("i on a deleted baseline row entered mode %v", m.mode)
	}
	if m.status.text == "" {
		t.Fatal("no read-only hint shown")
	}
}
