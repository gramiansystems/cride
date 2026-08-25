package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"cride/internal/diff"
	"cride/internal/highlight"
)

func TestRenderBrowsingShape(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		{
			OldPath: "internal/ui/render.go",
			NewPath: "internal/ui/render.go",
			Status:  diff.FileModified,
			Added:   1,
			Deleted: 1,
			Hunks: []diff.Hunk{{
				Header: "@@ -1,1 +1,1 @@",
				Lines: []diff.Line{
					{Kind: diff.LineDelete, Content: strings.Repeat("old ", 40), OldLine: 1},
					{Kind: diff.LineAdd, Content: strings.Repeat("new ", 40), NewLine: 1},
				},
			}},
		},
		{
			OldPath: "/dev/null",
			NewPath: "cmd/cride/main.go",
			Status:  diff.FileAdded,
			Added:   1,
			Hunks: []diff.Hunk{{
				Header: "@@ -0,0 +1,1 @@",
				Lines:  []diff.Line{{Kind: diff.LineAdd, Content: "package main", NewLine: 1}},
			}},
		},
	}

	out := Render(files, FlattenFile(files, 0), 0, 0, 0, 96, 18, highlight.New(), "HEAD", false)
	if got := lipgloss.Height(out); got != 18 {
		t.Fatalf("height = %d, want 18\n%s", got, stripANSI(out))
	}

	plain := stripANSI(out)
	for _, want := range []string{
		"[working tree] baseline HEAD",
		"╭",
		"▾ cmd/",
		"▾ internal/",
		"render.go",
		"    @@",
		"`j/k`move",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"before", "after"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("rendered output has redundant %q line label:\n%s", unwanted, plain)
		}
	}
	if strings.Contains(plain, ">   @@") {
		t.Fatalf("rendered output still uses cursor marker:\n%s", plain)
	}
	if !strings.Contains(plain, "new new") {
		t.Fatalf("wrapped long line tail not visible:\n%s", plain)
	}
}

func TestRenderStaysWithinTerminalBounds(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a/really/long/path/to/a/file.go",
		NewPath: "a/really/long/path/to/a/file.go",
		Status:  diff.FileModified,
		Added:   1,
		Deleted: 1,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []diff.Line{
				{Kind: diff.LineDelete, Content: strings.Repeat("old content ", 20), OldLine: 1},
				{Kind: diff.LineAdd, Content: strings.Repeat("new content ", 20), NewLine: 1},
			},
		}},
	}}
	rows := FlattenFile(files, 0)
	hl := highlight.NewWithOptions(highlight.Options{Disabled: true})
	panels := []*BottomPanel{
		nil,
		{Open: true, Title: "Results", Placement: PanelBottom, Results: []BottomPanelResult{{Label: strings.Repeat("result ", 20)}}},
		{Open: true, Title: "Results", Placement: PanelRight, Results: []BottomPanelResult{{Label: strings.Repeat("result ", 20)}}},
	}
	widths := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 16, 20, 24, 28, 32, 40, 48, 60, 80, 100, 120}
	heights := []int{1, 2, 3, 4, 5, 8, 12, 20, 24, 40}
	for _, width := range widths {
		for _, height := range heights {
			for _, panel := range panels {
				out := RenderWithPanel(files, rows, 0, 1, 0, width, height, hl, "HEAD", false, panel)
				if got := lipgloss.Width(out); got > width {
					t.Fatalf("rendered width = %d, terminal width = %d (height %d, panel %+v)", got, width, height, panel)
				}
				if got := lipgloss.Height(out); got > height {
					t.Fatalf("rendered height = %d, terminal height = %d (width %d, panel %+v)", got, height, width, panel)
				}
				overlay := Overlay{Title: strings.Repeat("Commands ", 10), Prompt: "?", Results: []OverlayResult{{Label: strings.Repeat("result ", 20)}}}
				out = RenderOverlay(out, overlay, width, height)
				if got := lipgloss.Width(out); got > width {
					t.Fatalf("overlay width = %d, terminal width = %d (height %d, panel %+v)", got, width, height, panel)
				}
				if got := lipgloss.Height(out); got > height {
					t.Fatalf("overlay height = %d, terminal height = %d (width %d, panel %+v)", got, height, width, panel)
				}
			}
		}
	}
}

func TestRenderShowsStickySymbolBreadcrumb(t *testing.T) {
	t.Parallel()
	files := []diff.FileDiff{{
		OldPath: "server.py", NewPath: "server.py", Status: diff.FileModified,
		Hunks: []diff.Hunk{{Lines: []diff.Line{{Kind: diff.LineAdd, Content: "    def login(self):", NewLine: 2}}}},
	}}
	rows := FlattenFile(files, 0)
	options := RenderOptions{Breadcrumb: "class Server › method login", ShowBreadcrumb: true}
	out := RenderWithOptions(files, rows, 0, 1, 0, 80, 14, highlight.New(), "HEAD", false, nil, options)
	plain := stripANSI(out)
	if !strings.Contains(plain, "class Server › method login") {
		t.Fatalf("rendered output missing breadcrumb:\n%s", plain)
	}
	without := Layout(80, 14, nil)
	with := LayoutWithBreadcrumb(80, 14, nil, true)
	if with.DiffRowsY != without.DiffRowsY+1 || with.DiffRowsHeight != without.DiffRowsHeight-1 {
		t.Fatalf("breadcrumb layout = y %d height %d; base y %d height %d", with.DiffRowsY, with.DiffRowsHeight, without.DiffRowsY, without.DiffRowsHeight)
	}
}

func TestPersistentCursorBackgroundRestoresAfterANSIReset(t *testing.T) {
	t.Parallel()

	bg, ok := backgroundSequence(colorCursor)
	if !ok {
		t.Fatal("cursor background did not produce an ANSI sequence")
	}

	got := withPersistentBackground("left\x1b[0mright", colorCursor)
	if !strings.HasPrefix(got, bg) {
		t.Fatalf("highlighted row does not start with cursor background: %q", got)
	}
	if !strings.Contains(got, "\x1b[0m"+bg) {
		t.Fatalf("cursor background was not restored after ANSI reset: %q", got)
	}
}

func TestActiveHunkRangeTracksCursorHunk(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{
			{
				Header: "@@ -1,2 +1,2 @@",
				Lines: []diff.Line{
					{Kind: diff.LineContext, Content: "one", OldLine: 1, NewLine: 1},
					{Kind: diff.LineAdd, Content: "two", NewLine: 2},
				},
			},
			{
				Header: "@@ -10,1 +10,1 @@",
				Lines: []diff.Line{
					{Kind: diff.LineDelete, Content: "ten", OldLine: 10},
				},
			},
		},
	}}
	rows := FlattenFile(files, 0)

	start, end, ok := activeHunkRange(rows, 1)
	if !ok || start != 0 || end != 2 {
		t.Fatalf("first hunk range = %d..%d ok=%v, want 0..2 true", start, end, ok)
	}

	start, end, ok = activeHunkRange(rows, 3)
	if !ok || start != 3 || end != 4 {
		t.Fatalf("second hunk range = %d..%d ok=%v, want 3..4 true", start, end, ok)
	}

	_, _, ok = activeHunkRange(MessageRows(0, "(binary file)"), 0)
	if ok {
		t.Fatal("placeholder row unexpectedly counted as an active hunk")
	}
}

func TestResultToneUsesGitDiffSigns(t *testing.T) {
	t.Parallel()

	lines := []string{
		bottomPanelResultLine(BottomPanelResult{Label: "a.go:1:1", Preview: "old line", Tone: ResultToneDeleted}, 80),
		overlayResultLine(OverlayResult{Label: "b.go:2:1", Preview: "new line", Tone: ResultToneAdded}, 80),
		bottomPanelResultLine(BottomPanelResult{Label: "Gone", Tone: ResultToneDeletedEntire, ChangeField: true}, 80),
		overlayResultLine(OverlayResult{Label: "New", Tone: ResultToneAddedEntire, ChangeField: true}, 80),
		bottomPanelResultLine(BottomPanelResult{Label: "Changed", Tone: ResultToneModified, ChangeField: true}, 80),
		bottomPanelResultLine(BottomPanelResult{Label: "Partial", Tone: ResultToneAdded, ChangeField: true}, 80),
		overlayResultLine(OverlayResult{Label: "Untouched", ChangeField: true}, 80),
		overlayResultLine(OverlayResult{Label: "c.go:3:1"}, 80),
	}
	plain := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{
		"- a.go:1:1",
		"+ b.go:2:1",
		"--- Gone",
		"+++ New",
		"+,- Changed",
		"+   Partial",
		"    Untouched",
		"c.go:3:1",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("result tone missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"before", "after", "deleted", "added"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("result tone used redundant text %q:\n%s", unwanted, plain)
		}
	}
}

func TestOverlayLinesRenderScrolledWindow(t *testing.T) {
	t.Parallel()

	results := make([]OverlayResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, OverlayResult{Label: "result-" + strconv.Itoa(i)})
	}
	lines := overlayLines(Overlay{
		Title:   "Search project",
		Prompt:  "/",
		Query:   "result",
		Cursor:  12,
		Top:     10,
		Results: results,
	}, 60, 6)
	plain := stripANSI(strings.Join(lines, "\n"))

	for _, want := range []string{"result-10", "result-11", "result-12", "result-13"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("scrolled overlay missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"result-9", "result-14"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("scrolled overlay unexpectedly contains %q:\n%s", unwanted, plain)
		}
	}
}

func TestOverlayLinesCanAlignPreviewColumn(t *testing.T) {
	t.Parallel()

	lines := overlayLines(Overlay{
		Title:      "Help",
		Prompt:     "?",
		Query:      "esc/q/? closes",
		LabelWidth: 12,
		Results: []OverlayResult{
			{Label: "j/k", Preview: "Move the cursor"},
			{Label: "ctrl+f", Preview: "Scroll by one page"},
		},
	}, 60, 5)
	plainLines := strings.Split(stripANSI(strings.Join(lines, "\n")), "\n")

	firstColumn := strings.Index(plainLines[2], "Move the cursor")
	secondColumn := strings.Index(plainLines[3], "Scroll by one page")
	if firstColumn < 0 || secondColumn < 0 {
		t.Fatalf("overlay lines missing previews:\n%s", strings.Join(plainLines, "\n"))
	}
	if firstColumn != secondColumn {
		t.Fatalf("preview columns = %d and %d, want aligned:\n%s", firstColumn, secondColumn, strings.Join(plainLines, "\n"))
	}
}

func TestOverlayLinesRenderCategoryTabs(t *testing.T) {
	t.Parallel()

	overlay := Overlay{
		Title:     "Commands",
		Prompt:    "?",
		Tabs:      []string{"Review", "Files", "Code"},
		ActiveTab: 1,
		Results:   []OverlayResult{{Label: "Open file"}},
	}
	lines := overlayLines(overlay, 60, 5)
	plain := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "▸ Files") || !strings.Contains(plain, "Review") || !strings.Contains(plain, "Code") {
		t.Fatalf("overlay tabs missing:\n%s", plain)
	}
	if !strings.Contains(plain, "Open file") {
		t.Fatalf("overlay result missing below tabs:\n%s", plain)
	}

	found := -1
	for y := 0; y < 24 && found < 0; y++ {
		for x := 0; x < 100; x++ {
			if OverlayTabIndexAt(overlay, 100, 24, x, y) == 1 {
				found = 1
				break
			}
		}
	}
	if found != 1 {
		t.Fatal("could not map a click to the Files tab")
	}
}

func TestBottomPanelLinesRenderStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		panel BottomPanel
		want  string
	}{
		{
			name:  "empty",
			panel: BottomPanel{Open: true, Title: "References: Target", Summary: "0 results · lexical", Empty: "No references"},
			want:  "No references",
		},
		{
			name:  "loading",
			panel: BottomPanel{Open: true, Title: "References: Target", Summary: "lexical", Loading: true},
			want:  "loading...",
		},
		{
			name:  "error",
			panel: BottomPanel{Open: true, Title: "References: Target", Error: "boom"},
			want:  "error: boom",
		},
		{
			name: "populated",
			panel: BottomPanel{
				Open:    true,
				Title:   "References: Target",
				Summary: "1 result · lexical",
				Results: []BottomPanelResult{{Label: "[def] a.go:2:1", Preview: "func Target() {}"}},
			},
			want: "[def] a.go:2:1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plain := stripANSI(strings.Join(bottomPanelLines(tt.panel, 72, 4), "\n"))
			if !strings.Contains(plain, tt.want) {
				t.Fatalf("bottom panel missing %q:\n%s", tt.want, plain)
			}
		})
	}
}

func TestRightDockedResultPanelUsesWideFullHeightLayout(t *testing.T) {
	t.Parallel()

	panel := BottomPanel{Open: true, Placement: PanelRight, Title: "References"}
	layout := LayoutWithPanelSizes(160, 40, &panel, false, 0)
	if layout.ResultPanelWidth != 64 {
		t.Fatalf("right panel width = %d, want 64", layout.ResultPanelWidth)
	}
	if layout.ResultPanelX != 96 || layout.ResultPanelY != 1 || layout.ResultPanelHeight != 38 {
		t.Fatalf("right panel geometry = x%d y%d %dx%d, want x96 y1 64x38",
			layout.ResultPanelX, layout.ResultPanelY, layout.ResultPanelWidth, layout.ResultPanelHeight)
	}
	if layout.BottomPanelHeight != 0 {
		t.Fatalf("right dock reserved bottom height %d", layout.BottomPanelHeight)
	}

	bottom := panel
	bottom.Placement = PanelBottom
	if right, below := BottomPanelResultHeight(panel, 160, 40), BottomPanelResultHeight(bottom, 160, 40); right <= below {
		t.Fatalf("right panel page = %d, bottom page = %d; want more rows on right", right, below)
	}
}

func TestLayoutHonorsDraggedPanelSizes(t *testing.T) {
	t.Parallel()

	bottom := BottomPanel{Open: true, Size: 14}
	if got := LayoutWithPanelSizes(160, 40, &bottom, false, 42); got.BottomPanelHeight != 14 || got.LeftOuterWidth != 42 {
		t.Fatalf("bottom/list sizes = %d/%d, want 14/42", got.BottomPanelHeight, got.LeftOuterWidth)
	}

	right := BottomPanel{Open: true, Placement: PanelRight, Size: 55}
	if got := LayoutWithPanelSizes(160, 40, &right, false, 42); got.ResultPanelWidth != 55 || got.LeftOuterWidth != 42 {
		t.Fatalf("right/list sizes = %d/%d, want 55/42", got.ResultPanelWidth, got.LeftOuterWidth)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}
