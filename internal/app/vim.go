package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/ui"
)

type vimFinishedMsg struct{ err error }

// openCurrentFileInVim temporarily gives Vim the terminal, then lets the
// normal diff reload reconcile changes made while cride was suspended.
func (m *Model) openCurrentFileInVim() tea.Cmd {
	fileIdx := m.selectedFile
	if m.focus == paneList {
		row, ok := m.selectedChangeListRow()
		if !ok || row.IsDir {
			return m.notify(ui.ToastWarn, "select a file to open in Vim")
		}
		fileIdx = row.FileIdx
	}
	if m.source == nil || fileIdx < 0 || fileIdx >= len(m.files) {
		return m.notify(ui.ToastWarn, "no current file to open in Vim")
	}
	file := m.files[fileIdx]
	if file.NewPath == "" || file.NewPath == "/dev/null" {
		return m.notify(ui.ToastWarn, "deleted file has no working-tree copy to open")
	}
	root := m.source.Root()
	path := file.NewPath
	info, err := os.Stat(filepath.Join(root, path))
	if err != nil {
		return m.notify(ui.ToastError, "cannot open in Vim: "+err.Error())
	}
	if !info.Mode().IsRegular() {
		return m.notify(ui.ToastWarn, "current file is not a regular file")
	}
	if fileIdx != m.selectedFile {
		// The change-list cursor can point to a file other than the diff pane's
		// current one. Keep that pick selected when cride resumes.
		_ = m.openFileFromList(fileIdx)
	}

	line, column := m.vimCursorPosition()
	cmd := exec.Command("vim", fmt.Sprintf("+call cursor(%d,%d)", line, column), "--", path)
	cmd.Dir = root
	return tea.Exec(&vimCommand{Cmd: cmd, pauseKeyboard: m.pauseKeyboard}, func(err error) tea.Msg { return vimFinishedMsg{err: err} })
}

// Bubble Tea releases the terminal before Run and restores it after Run.
// Pause Kitty key reporting within that interval so Vim receives normal keys.
type vimCommand struct {
	*exec.Cmd
	pauseKeyboard func() func()
}

func (c *vimCommand) Run() error {
	if c.pauseKeyboard != nil {
		resumeKeyboard := c.pauseKeyboard()
		defer resumeKeyboard()
	}
	return c.Cmd.Run()
}

// Bubble Tea may supply a translating keyboard reader. Vim needs the real
// terminal file, which also lets exec.Wait finish as soon as Vim exits.
func (c *vimCommand) SetStdin(io.Reader)    { c.Stdin = os.Stdin }
func (c *vimCommand) SetStdout(w io.Writer) { c.Stdout = w }
func (c *vimCommand) SetStderr(w io.Writer) { c.Stderr = w }

// Vim's column is a 1-based byte offset; cride's is a 0-based rune index.
// Diff headers and deleted lines use a current-side line in their hunk, or
// the hunk's current-side start when it has no visible current-side lines.
func (m *Model) vimCursorPosition() (line, column int) {
	rows := m.currentRows()
	line, column = 1, 1
	if m.cursor < 0 || m.cursor >= len(rows) {
		return line, column
	}
	row := rows[m.cursor]
	if currentLine := rowLineNumberForSide(row, false); currentLine > 0 {
		line = currentLine
		if !cursorRowSide(row, m.splitActiveLeft) {
			if content, ok := rowContentForSide(row, false); ok {
				if byteCol, ok := byteColumnAtRune(content, m.col); ok {
					column = byteCol
				} else {
					column = len(content) + 1
				}
			}
		}
		return line, column
	}
	for i := m.cursor + 1; i < len(rows) && rows[i].HunkIdx == row.HunkIdx; i++ {
		if currentLine := rowLineNumberForSide(rows[i], false); currentLine > 0 {
			return currentLine, column
		}
	}
	for i := m.cursor - 1; i >= 0 && rows[i].HunkIdx == row.HunkIdx; i-- {
		if currentLine := rowLineNumberForSide(rows[i], false); currentLine > 0 {
			return currentLine, column
		}
	}
	if row.HunkIdx > 0 && row.HunkIdx <= len(m.files[m.selectedFile].Hunks) {
		return max(1, m.files[m.selectedFile].Hunks[row.HunkIdx-1].NewStart), column
	}
	return line, column
}
