package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/blame"
	"cride/internal/diff"
	"cride/internal/diffsource"
	"cride/internal/ui"
)

type blameFileState struct {
	file    blame.FileBlame
	err     error
	loading bool
	loaded  bool
}

type blameLoadedMsg struct {
	path string // baseline-side path
	file blame.FileBlame
	err  error
}

// toggleBlameGutter controls a global view preference. Individual file
// results remain cached because every supported source exposes a pinned,
// immutable baseline ref.
func (m *Model) toggleBlameGutter() tea.Cmd {
	m.blameGutter = !m.blameGutter
	m.rowsVersion++
	if !m.blameGutter {
		return m.notify(ui.ToastInfo, "git blame margin off")
	}
	return tea.Batch(
		m.ensureCurrentBlameCmd(),
		m.notify(ui.ToastInfo, "git blame margin on — baseline history"),
	)
}

func (m *Model) ensureCurrentBlameCmd() tea.Cmd {
	if !m.blameGutter || m.source == nil || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return nil
	}
	file := m.files[m.selectedFile]
	if file.Binary || file.Status == diff.FileAdded || file.OldPath == "" || file.OldPath == "/dev/null" {
		return nil
	}
	refSource, ok := m.source.(diffsource.BaselineReferencer)
	if !ok || strings.TrimSpace(refSource.BaselineRef()) == "" {
		return nil
	}
	path := file.OldPath
	if m.blames == nil {
		m.blames = make(map[string]blameFileState)
	}
	if state, exists := m.blames[path]; exists && (state.loading || state.loaded) {
		return nil
	}
	m.blames[path] = blameFileState{loading: true}

	src := m.source
	root := src.Root()
	ref := refSource.BaselineRef()
	return func() tea.Msg {
		// BaselineContent enforces the source's file-size and regular-file
		// bounds before git blame can produce substantially larger output.
		if _, err := src.BaselineContent(path); err != nil {
			return blameLoadedMsg{path: path, err: err}
		}
		loaded, err := blame.Load(root, ref, path)
		return blameLoadedMsg{path: path, file: loaded, err: err}
	}
}

func (m Model) withBlameRows(rows []ui.Row) []ui.Row {
	if !m.blameGutter || len(rows) == 0 || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return rows
	}
	file := m.files[m.selectedFile]
	state := m.blames[file.OldPath]
	out := make([]ui.Row, len(rows))
	copy(out, rows)
	loadingLabelShown := false
	for i := range out {
		out[i].BlameGutter = true
		if !out[i].IsLineRow() {
			continue
		}
		if state.loading && !loadingLabelShown {
			out[i].BlameText = "loading blame…"
			loadingLabelShown = true
			continue
		}
		line := baselineLineForBlame(file, out[i])
		if !state.loaded || state.err != nil || line < 1 || line > len(state.file.Lines) {
			continue
		}
		info := state.file.Lines[line-1]
		if info.SHA == "" {
			continue
		}
		sha := info.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		date := ""
		if !info.AuthorTime.IsZero() {
			date = info.AuthorTime.Local().Format("2006-01-02 15:04")
		}
		out[i].BlameText = strings.TrimSpace(sha + " " + date + " " + info.Author)
	}
	return out
}

// baselineLineForBlame maps a rendered row back to the immutable side. Diff
// rows already carry OldLine. Full-file and locally-expanded rows are rebuilt
// from current content, so unchanged lines are mapped through hunk deltas.
func baselineLineForBlame(file diff.FileDiff, row ui.Row) int {
	if row.Kind == ui.RowPair {
		if row.Left == nil || row.Left.Kind == diff.LineAdd {
			return 0
		}
		return row.Left.OldLine
	}
	if row.Kind != ui.RowLine || row.Line.Kind == diff.LineAdd || row.Changed {
		return 0
	}
	if row.Line.OldLine > 0 {
		return row.Line.OldLine
	}
	return baselineLineForCurrent(file, row.Line.NewLine)
}

func baselineLineForCurrent(file diff.FileDiff, currentLine int) int {
	if currentLine < 1 {
		return 0
	}
	delta := 0
	for _, hunk := range file.Hunks {
		if currentLine < hunk.NewStart {
			return currentLine + delta
		}
		for _, line := range hunk.Lines {
			if line.NewLine != currentLine {
				continue
			}
			if line.Kind == diff.LineAdd {
				return 0
			}
			if line.OldLine > 0 {
				return line.OldLine
			}
		}
		newEnd := hunk.NewStart + hunk.NewLines
		if currentLine < newEnd {
			// A current-side line inside the hunk that was not explicitly
			// matched is an insertion and has no baseline attribution.
			return 0
		}
		delta += hunk.OldLines - hunk.NewLines
	}
	return currentLine + delta
}
