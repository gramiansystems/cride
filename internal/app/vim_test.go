package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
)

func TestVimCursorPositionUsesCurrentSideAndByteColumn(t *testing.T) {
	file := testFile("example.go")
	file.Hunks[0].NewStart = 5
	file.Hunks[0].Lines = []diff.Line{
		{Kind: diff.LineDelete, Content: "old", OldLine: 4},
		{Kind: diff.LineAdd, Content: "a界b", NewLine: 5},
		{Kind: diff.LineContext, Content: "next", NewLine: 6},
	}
	m := Model{files: []diff.FileDiff{file}, cursor: 2, col: 2}
	if line, col := m.vimCursorPosition(); line != 5 || col != 5 {
		t.Fatalf("Vim position = %d:%d, want 5:5", line, col)
	}
	m.cursor = 1 // deleted line: land at the next current-side line
	if line, col := m.vimCursorPosition(); line != 5 || col != 1 {
		t.Fatalf("deleted-line Vim position = %d:%d, want 5:1", line, col)
	}
	m.cursor = 0 // hunk header
	if line, col := m.vimCursorPosition(); line != 5 || col != 1 {
		t.Fatalf("header Vim position = %d:%d, want 5:1", line, col)
	}
}

func TestOpenCurrentFileInVimShortcutAndReturn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "example.go"), []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := Model{source: fakeSource{root: root}, files: []diff.FileDiff{testFile("example.go")}, mode: modeReview, width: 80, height: 24}
	next, cmd := m.handleKey(key("V"))
	if cmd == nil {
		t.Fatal("V did not launch Vim")
	}
	got := next.(Model)
	if got.mode != modeReview || got.currentFilePath() != "example.go" {
		t.Fatal("V changed the review position before launching Vim")
	}
	next, reloadCmd := got.Update(vimFinishedMsg{})
	if reloadCmd == nil || !next.(Model).loadInFlight {
		t.Fatal("returning from Vim did not reload the diff")
	}

	deleted := testFile("removed.go")
	deleted.NewPath = "/dev/null"
	deleted.OldPath = "removed.go"
	deleted.Status = diff.FileDeleted
	m.files = []diff.FileDiff{deleted}
	next, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'V'}})
	if cmd == nil || next.(Model).status.text == "" {
		t.Fatal("deleted file should report that Vim cannot open it")
	}
}

func TestVimShortcutUsesChangeListCursor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("hello\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := Model{
		source: fakeSource{root: root},
		files:  []diff.FileDiff{testFile("a.go"), testFile("b.go")},
		focus:  paneList, width: 80, height: 24,
	}
	for i, row := range m.changeListView().Rows {
		if !row.IsDir && row.FileIdx == 1 {
			m.listCursor = i
			break
		}
	}
	next, cmd := m.handleKey(key("V"))
	got := next.(Model)
	if cmd == nil || got.currentFilePath() != "b.go" {
		t.Fatal("V did not open the file under the change-list cursor")
	}
}
