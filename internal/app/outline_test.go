package app

import (
	"errors"
	"strings"
	"testing"

	"cride/internal/diff"
	"cride/internal/lsp"
	"cride/internal/outline"
	"cride/internal/source"
	"cride/internal/ui"
)

func TestDocumentSymbolsFallsBackToLexicalExtraction(t *testing.T) {
	t.Parallel()
	lines := []string{"package p", "func Target() {}", "type Server struct{}"}
	files := []diff.FileDiff{testFileWithLines("a.go", len(lines))}
	m := Model{
		source:           fakeSource{contents: map[string][]byte{"a.go": []byte(strings.Join(lines, "\n"))}},
		lsp:              fakeLSP{err: errors.New("server unavailable")},
		outlineExtractor: outline.LexicalExtractor{},
		files:            files,
		changedPaths:     changedPathSet(files),
		selectedFile:     0,
		width:            100,
		height:           24,
		fileContents:     make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("s"))
	if cmd == nil {
		t.Fatal("gs returned nil fallback command")
	}
	next, _ = next.(Model).Update(cmd())
	got := next.(Model)
	if got.enrichmentPanel.Err != nil || len(got.enrichmentPanel.Results) != 2 {
		t.Fatalf("fallback panel = %+v", got.enrichmentPanel)
	}
	if !strings.Contains(got.enrichmentPanel.Results[0].Label, "Target") {
		t.Fatalf("first result = %q", got.enrichmentPanel.Results[0].Label)
	}
}

func TestDocumentSymbolsDistinguishContainedChangesFromChangedFile(t *testing.T) {
	t.Parallel()
	current := "package p\nfunc Changed() {\n\tkeep()\n\tadded()\n}\nfunc Untouched() {\n\tkeep()\n}\n"
	baseline := "package p\nfunc Changed() {\n\tremoved()\n\tkeep()\n}\nfunc Untouched() {\n\tkeep()\n}\n"
	file := diff.FileDiff{
		OldPath: "a.go", NewPath: "a.go", Status: diff.FileModified,
		Hunks: []diff.Hunk{{Lines: []diff.Line{
			{Kind: diff.LineDelete, Content: "\tremoved()", OldLine: 3},
			{Kind: diff.LineContext, Content: "\tkeep()", OldLine: 4, NewLine: 3},
			{Kind: diff.LineAdd, Content: "\tadded()", NewLine: 4},
		}}},
	}
	m := Model{
		source: fakeSource{
			contents:         map[string][]byte{"a.go": []byte(current)},
			baselineContents: map[string][]byte{"a.go": []byte(baseline)},
		},
		lsp: fakeLSP{documentSymbols: []lsp.DocumentSymbol{
			{
				Name: "Changed", Kind: lsp.SymbolFunction,
				Range:          source.Range{Start: source.Location{Path: "a.go", Line: 2, Column: 1}, End: source.Location{Path: "a.go", Line: 5, Column: 2}},
				SelectionRange: source.Range{Start: source.Location{Path: "a.go", Line: 2, Column: 6}},
			},
			{
				Name: "Untouched", Kind: lsp.SymbolFunction,
				Range:          source.Range{Start: source.Location{Path: "a.go", Line: 6, Column: 1}, End: source.Location{Path: "a.go", Line: 8, Column: 2}},
				SelectionRange: source.Range{Start: source.Location{Path: "a.go", Line: 6, Column: 6}},
			},
		}},
		outlineExtractor: outline.LexicalExtractor{},
		files:            []diff.FileDiff{file},
		changedPaths:     changedPathSet([]diff.FileDiff{file}),
		selectedFile:     0,
		width:            100,
		height:           24,
		fileContents:     make(map[string]fileContentState),
	}

	cmd := m.openDocumentSymbolsPanel()
	if cmd == nil {
		t.Fatal("document symbols returned nil command")
	}
	next, _ := m.Update(cmd())
	got := next.(Model)
	panel := got.enrichmentPanelViewValue()
	results := map[string]ui.BottomPanelResult{}
	for i, result := range got.enrichmentPanel.Results {
		results[result.Label] = panel.Results[i]
	}
	changed := results["[function] Changed  2:6"]
	if changed.Tone != ui.ResultToneModified || !changed.ChangeField || strings.Contains(changed.Label, "contains-") {
		t.Fatalf("changed function result = %+v, want symbolic mixed tone", changed)
	}
	untouched := results["[function] Untouched  6:6"]
	if untouched.Tone != ui.ResultToneNone || !untouched.ChangeField || strings.Contains(untouched.Label, "changed-file") {
		t.Fatalf("untouched function result = %+v, want blank change field", untouched)
	}
}

func TestOutlinePanelScopesReviewAndJumpsToRemovedSymbol(t *testing.T) {
	t.Parallel()
	files := []diff.FileDiff{
		{
			OldPath: "a.go", NewPath: "a.go", Status: diff.FileModified,
			Hunks: []diff.Hunk{{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 0, Lines: []diff.Line{{Kind: diff.LineDelete, Content: "func Gone() {}", OldLine: 1}}}},
		},
		{
			OldPath: "/dev/null", NewPath: "b.py", Status: diff.FileAdded,
			Hunks: []diff.Hunk{{OldStart: 0, NewStart: 1, NewLines: 2, Lines: []diff.Line{{Kind: diff.LineAdd, Content: "class Added:", NewLine: 1}, {Kind: diff.LineAdd, Content: "    pass", NewLine: 2}}}},
		},
	}
	m := Model{
		source: fakeSource{
			contents:         map[string][]byte{"a.go": []byte("package p\n"), "b.py": []byte("class Added:\n    pass\n")},
			baselineContents: map[string][]byte{"a.go": []byte("func Gone() {}\n")},
		},
		lsp:              fakeLSP{err: errors.New("server unavailable")},
		outlineExtractor: outline.LexicalExtractor{},
		files:            files,
		changedPaths:     changedPathSet(files),
		selectedFile:     0,
		width:            100,
		height:           24,
		fileContents:     make(map[string]fileContentState),
	}

	m = press(m, "g")
	next, cmd := m.handleKey(key("y"))
	got := next.(Model)
	if cmd == nil || !got.enrichmentPanel.Open || got.enrichmentPanel.Kind != enrichmentPanelOutlineDiff {
		t.Fatalf("gy panel/cmd = %+v/%v", got.enrichmentPanel, cmd != nil)
	}
	next, _ = got.Update(cmd())
	got = next.(Model)
	if len(got.enrichmentPanel.Results) != 1 || got.enrichmentPanel.Results[0].Side.String() != "before" {
		t.Fatalf("current-file results = %#v", got.enrichmentPanel.Results)
	}

	next, _, handled := got.handleEnrichmentPanelKey(key("s"))
	got = next.(Model)
	if !handled || len(got.enrichmentPanel.Results) != 2 {
		t.Fatalf("review scope handled/results = %v/%d", handled, len(got.enrichmentPanel.Results))
	}
	// Source ordering puts the removed a.go declaration first.
	got.enrichmentPanel.Order = diff.ResultOrderSource
	got.enrichmentPanel.Results = got.rankEnrichmentResults(got.enrichmentPanel.RawResults)
	got.enrichmentPanel.Cursor = 0
	if cmd := got.acceptEnrichmentResult(); cmd != nil {
		t.Fatal("removed symbol jump unexpectedly loaded current content")
	}
	if got.viewMode != ViewDiff || got.selectedFile != 0 {
		t.Fatalf("removed jump mode/file = %v/%d", got.viewMode, got.selectedFile)
	}
	rows := got.currentRows()
	if got.cursor < 0 || got.cursor >= len(rows) || rows[got.cursor].Line.OldLine != 1 {
		t.Fatalf("removed jump cursor/rows = %d/%#v", got.cursor, rows)
	}
}

func TestOutlineBreadcrumbTracksCurrentAndBaselineRows(t *testing.T) {
	t.Parallel()
	file := diff.FileDiff{
		OldPath: "a.py", NewPath: "a.py", Status: diff.FileModified,
		Hunks: []diff.Hunk{{Lines: []diff.Line{
			{Kind: diff.LineDelete, Content: "    def old(self):", OldLine: 2},
			{Kind: diff.LineAdd, Content: "    def new(self):", NewLine: 2},
		}}},
	}
	extractor := outline.LexicalExtractor{}
	current, _ := extractor.Symbols("a.py", []byte("class Server:\n    def new(self):\n        pass\n"))
	baseline, _ := extractor.Symbols("a.py", []byte("class Server:\n    def old(self):\n        pass\n"))
	m := Model{
		files:           []diff.FileDiff{file},
		selectedFile:    0,
		cursor:          2,
		outlineLoaded:   true,
		outlineCurrent:  map[string][]lsp.DocumentSymbol{"a.py": current},
		outlineBaseline: map[string][]lsp.DocumentSymbol{"a.py": baseline},
	}
	if got := m.outlineBreadcrumb(); got != "class Server › method new" {
		t.Fatalf("current breadcrumb = %q", got)
	}
	m.cursor = 1
	if got := m.outlineBreadcrumb(); got != "class Server › method old" {
		t.Fatalf("baseline breadcrumb = %q", got)
	}
	if !m.showOutlineBreadcrumb() || m.currentRows()[m.cursor].Kind != ui.RowLine {
		t.Fatal("breadcrumb row was not available")
	}
}

func TestStaleOutlineLoadIsIgnored(t *testing.T) {
	t.Parallel()
	m := Model{outlineGeneration: 3, outlineLoading: true}
	next, _ := m.Update(outlineDiffLoadedMsg{generation: 2, changes: []outline.SymbolChange{{Type: outline.SymbolAdded}}})
	got := next.(Model)
	if !got.outlineLoading || got.outlineLoaded || len(got.outlineChanges) != 0 {
		t.Fatalf("stale outline mutated model: %+v", got)
	}
}
