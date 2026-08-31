package app

// Per-file unread state (DESIGN.md's "Unread state"): a file is unread when its current diff
// differs from the snapshot taken at mark-read. Unread is derived, never
// bookkept — reverting an edit makes a file read again with no special-casing.

import (
	"hash/fnv"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

// fileDiffHash fingerprints one file's diff content.
func fileDiffHash(f diff.FileDiff) string {
	h := fnv.New64a()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write(f.OldPath)
	write(f.NewPath)
	write(strconv.Itoa(int(f.Status)))
	for _, hunk := range f.Hunks {
		write(hunk.Header)
		for _, ln := range hunk.Lines {
			write(strconv.Itoa(int(ln.Kind)))
			write(strconv.Itoa(ln.OldLine))
			write(strconv.Itoa(ln.NewLine))
			write(ln.Content)
		}
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// trackedFileDiffHash returns the hash captured when the current diff was
// loaded. Models assembled directly in tests or by embedders may not have
// passed through updateChangeOrder, so retain the exact-computation fallback.
func (m Model) trackedFileDiffHash(f diff.FileDiff) string {
	if hash, ok := m.changeHashes[f.Path()]; ok {
		return hash
	}
	return fileDiffHash(f)
}

// fileUnread reports whether a file's diff differs from its seen snapshot.
func (m Model) fileUnread(f diff.FileDiff) bool {
	if f.Status == diff.FileUnchanged {
		return false
	}
	return m.seen[f.Path()] != m.trackedFileDiffHash(f)
}

// unreadFileSet maps file indexes to unread state for the change list.
func (m Model) unreadFileSet() map[int]bool {
	if len(m.files) == 0 {
		return nil
	}
	unread := make(map[int]bool, len(m.files))
	for i, f := range m.files {
		if m.fileUnread(f) {
			unread[i] = true
		}
	}
	return unread
}

func (m Model) unreadCount() int {
	count := 0
	for _, f := range m.files {
		if m.fileUnread(f) {
			count++
		}
	}
	return count
}

// markCurrentFileRead snapshots the current file.
func (m *Model) markCurrentFileRead() tea.Cmd {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	f := m.files[m.selectedFile]
	if m.seen == nil {
		m.seen = make(map[string]string)
	}
	m.seen[f.Path()] = m.trackedFileDiffHash(f)
	return m.notify(ui.ToastInfo, "marked read — "+strconv.Itoa(m.unreadCount())+" unread left")
}

// markCurrentFileReadAndAdvance snapshots the current file, then moves to the
// next file in the same order used by file navigation (R).
func (m *Model) markCurrentFileReadAndAdvance() tea.Cmd {
	readCmd := m.markCurrentFileRead()
	before := m.selectedFile
	m.switchFileN(1, 1)
	if m.selectedFile == before {
		return readCmd
	}
	return tea.Batch(readCmd, m.ensureCurrentFileContentCmd())
}

// markCurrentFileUnread clears the current file's seen snapshot (U).
func (m *Model) markCurrentFileUnread() tea.Cmd {
	if m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	if m.seen != nil {
		delete(m.seen, m.files[m.selectedFile].Path())
	}
	return m.notify(ui.ToastInfo, "marked unread — "+strconv.Itoa(m.unreadCount())+" unread")
}

// markAllRead snapshots every file (A).
func (m *Model) markAllRead() tea.Cmd {
	if m.seen == nil {
		m.seen = make(map[string]string)
	}
	for _, f := range m.files {
		m.seen[f.Path()] = m.trackedFileDiffHash(f)
	}
	return m.notify(ui.ToastInfo, "all files marked read")
}

// stepUnreadFile moves to the next/previous unread file in change-list
// order (n/N), wrapping with a toast.
func (m *Model) stepUnreadFile(dir, count int) tea.Cmd {
	order := ui.ChangeListFileOrderWithOptions(m.files, m.changeListOptions())
	if len(order) == 0 {
		return nil
	}
	var unreadPositions []int
	current := -1
	for pos, fileIdx := range order {
		if fileIdx == m.selectedFile {
			current = pos
		}
		if m.fileUnread(m.files[fileIdx]) {
			unreadPositions = append(unreadPositions, pos)
		}
	}
	if len(unreadPositions) == 0 {
		return m.notify(ui.ToastInfo, "no unread files")
	}

	target := -1
	wrapped := false
	for step := 0; step < count; step++ {
		next := -1
		if dir > 0 {
			for _, pos := range unreadPositions {
				if pos > current {
					next = pos
					break
				}
			}
			if next < 0 {
				next = unreadPositions[0]
				wrapped = true
			}
		} else {
			for i := len(unreadPositions) - 1; i >= 0; i-- {
				if unreadPositions[i] < current {
					next = unreadPositions[i]
					break
				}
			}
			if next < 0 {
				next = unreadPositions[len(unreadPositions)-1]
				wrapped = true
			}
		}
		current = next
		target = next
	}
	if target < 0 || order[target] == m.selectedFile {
		return m.notify(ui.ToastInfo, "no other unread files")
	}

	m.saveCurrentFileState()
	m.selectedFile = order[target]
	m.restoreCurrentFileState()
	m.rememberPath(m.currentFilePath())
	m.clampScroll()
	cmd := m.ensureCurrentFileContentCmd()
	if wrapped {
		return tea.Batch(cmd, m.notify(ui.ToastInfo, "wrapped to first unread"))
	}
	return cmd
}
