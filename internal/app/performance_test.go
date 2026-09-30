package app

import (
	"strconv"
	"testing"

	"cride/internal/diff"
	"cride/internal/highlight"
	"cride/internal/lsp"
	"cride/internal/source"
)

func benchmarkFiles(fileCount, lineCount int) []diff.FileDiff {
	files := make([]diff.FileDiff, fileCount)
	for fileIdx := range files {
		lines := make([]diff.Line, lineCount)
		for lineIdx := range lines {
			line := lineIdx + 1
			lines[lineIdx] = diff.Line{
				Kind:    diff.LineContext,
				Content: "value" + strconv.Itoa(fileIdx) + " := source + " + strconv.Itoa(line),
				OldLine: line,
				NewLine: line,
			}
		}
		path := "file" + strconv.Itoa(fileIdx) + ".go"
		files[fileIdx] = diff.FileDiff{
			OldPath: path,
			NewPath: path,
			Status:  diff.FileModified,
			Hunks:   []diff.Hunk{{Header: "@@ -1 +1 @@", Lines: lines}},
		}
	}
	return files
}

func BenchmarkScrollLargeFile(b *testing.B) {
	m := Model{files: benchmarkFiles(1, 20_000), width: 120, height: 40}
	m.clampScroll()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.windowScroll(3)
		m.clampScroll()
	}
}

func BenchmarkSwitchLargeFiles(b *testing.B) {
	m := Model{files: benchmarkFiles(8, 20_000), width: 120, height: 40}
	m.updateChangeOrder(m.files)
	m.clampScroll()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dir := 1
		if m.selectedFile == 1 {
			dir = -1
		}
		m.switchFile(dir)
		m.clampScroll()
	}
}

func BenchmarkScrollAndViewHighlighted(b *testing.B) {
	benchmarkScrollAndView(b, false)
}

func BenchmarkScrollAndViewWithSearch(b *testing.B) {
	benchmarkScrollAndView(b, true)
}

func benchmarkScrollAndView(b *testing.B, search bool) {
	m := Model{
		source: fakeSource{},
		files:  benchmarkFiles(1, 20_000),
		width:  120,
		height: 40,
		hl:     highlight.New(),
	}
	m.updateChangeOrder(m.files)
	m.clampScroll()
	if search {
		m.search = searchViewState{active: true, query: "value"}
		m.refreshSearchMatches(false)
	}
	maxTop := m.currentLayout().TotalLines() - m.viewHeight()
	direction := 3
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if top := m.topScreenLine(m.currentLayout()); top >= maxTop {
			direction = -3
		} else if top == 0 {
			direction = 3
		}
		m.windowScroll(direction)
		m.clampScroll()
		_ = m.View()
	}
}

func BenchmarkViewLargeFile(b *testing.B) {
	m := Model{
		source: fakeSource{},
		files:  benchmarkFiles(1, 20_000),
		width:  120,
		height: 40,
		hl:     highlight.NewWithOptions(highlight.Options{Disabled: true}),
	}
	m.updateChangeOrder(m.files)
	m.clampScroll()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkViewManyFiles(b *testing.B) {
	m := Model{
		source: fakeSource{},
		files:  benchmarkFiles(5_000, 1),
		width:  160,
		height: 50,
		hl:     highlight.NewWithOptions(highlight.Options{Disabled: true}),
	}
	m.updateChangeOrder(m.files)
	m.clampScroll()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func TestCurrentRowsCachesRecentFilesAndInvalidatesByVersion(t *testing.T) {
	t.Parallel()

	m := Model{files: benchmarkFiles(2, 20), width: 120, height: 40}
	first := m.currentRows()
	m.selectedFile = 1
	_ = m.currentRows()
	m.selectedFile = 0
	again := m.currentRows()
	if &first[0] != &again[0] {
		t.Fatal("returning to a recent file rebuilt its rows")
	}

	m.rowsVersion++
	changed := m.currentRows()
	if &first[0] == &changed[0] {
		t.Fatal("row version change reused stale rows")
	}
}

func TestWrapLayoutCachesRecentFiles(t *testing.T) {
	t.Parallel()

	m := Model{files: benchmarkFiles(2, 20), width: 120, height: 40}
	first := m.currentLayout()
	m.selectedFile = 1
	_ = m.currentLayout()
	m.selectedFile = 0
	if again := m.currentLayout(); first != again {
		t.Fatal("returning to a recent file rebuilt its wrap layout")
	}
}

func TestRenderRowsCachesDiagnosticsAndInvalidatesUpdates(t *testing.T) {
	t.Parallel()

	path := "file0.go"
	warning := lsp.Diagnostic{
		Range: source.Range{
			Start: source.Location{Path: path, Line: 2, Column: 1},
			End:   source.Location{Path: path, Line: 2, Column: 2},
		},
		Severity: lsp.DiagnosticWarning,
	}
	m := Model{
		files:              benchmarkFiles(1, 20),
		width:              120,
		height:             40,
		diagnostics:        map[string][]lsp.Diagnostic{path: {warning}},
		diagnosticsVersion: 1,
	}
	first := m.renderRows()
	again := m.renderRows()
	if &first[0] != &again[0] {
		t.Fatal("unchanged diagnostics rebuilt decorated rows")
	}

	errorDiagnostic := warning
	errorDiagnostic.Severity = lsp.DiagnosticError
	m.updateDiagnostics(enrichmentLoadedMsg{
		kind:        enrichmentPanelDiagnosticsCurrent,
		path:        path,
		diagnostics: []lsp.Diagnostic{errorDiagnostic},
	})
	changed := m.renderRows()
	if &first[0] == &changed[0] {
		t.Fatal("diagnostic update reused stale decorated rows")
	}
	for _, row := range changed {
		if row.Line.NewLine == 2 && row.DiagnosticMarker != "E" {
			t.Fatalf("updated marker = %q, want E", row.DiagnosticMarker)
		}
	}
}

func TestChangeListCachesRowsAndInvalidatesReadState(t *testing.T) {
	t.Parallel()

	m := Model{files: benchmarkFiles(2, 20), width: 120, height: 40}
	m.updateChangeOrder(m.files)
	_ = m.currentRows()
	first := m.changeListView().Rows
	again := m.changeListView().Rows
	if &first[0] != &again[0] {
		t.Fatal("unchanged file-list state rebuilt the tree")
	}

	_ = m.markCurrentFileRead()
	changed := m.changeListView().Rows
	if &first[0] == &changed[0] {
		t.Fatal("read-state change reused stale file-list rows")
	}
	for _, row := range changed {
		if row.FileIdx == m.selectedFile && row.Unread {
			t.Fatal("selected file stayed unread after mark-read")
		}
	}
}
