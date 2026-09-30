package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"cride/internal/diff"
	"cride/internal/highlight"
)

func TestRendererPreservesRowRendering(t *testing.T) {
	t.Parallel()
	files := wrappedTestFiles()
	rows := append([]Row{{Kind: RowFileHeader, FileIdx: 0}}, FlattenFile(files, 0)...)
	rows = append(rows, PairRows(FlattenFile(files, 0))...)
	rows = append(rows,
		Row{Kind: RowComment, Text: CommentHeaderText("warning", false, false, false), CommentHeader: true},
		Row{Kind: RowComment, Text: "done comment 界\ttext", Muted: true},
	)
	for i := range rows {
		rows[i].BlameGutter = true
		rows[i].BlameText = "abc123 界 👩‍💻 author"
		if rows[i].IsLineRow() {
			rows[i].DiagnosticMarker = "W"
		}
	}
	hl := highlight.New()
	for _, blame := range []bool{false, true} {
		for i := range rows {
			rows[i].BlameGutter = blame
		}
		for _, width := range []int{1, 3, 4, 17, 34, 35, 38, 48, 80} {
			var renderer Renderer
			renderer.begin(rows, width, hl)
			for _, cursor := range []int{0, 1, 4, 1004, 1} {
				for i, row := range rows {
					for _, bg := range []lipgloss.Color{"", colorCursor, colorHunkBg} {
						cached := renderer.row(files, row, hl, i, cursor, width, bg)
						want := rowScreenLines(files, row, hl, i, cursor, width)
						for k, line := range want {
							line = padRight(line, width)
							if bg != "" {
								line = withPersistentBackground(line, bg)
								line = lipgloss.NewStyle().Background(bg).Width(width).MaxWidth(width).Render(line)
							}
							if got := cached.line(k, i, cursor); got != line {
								t.Fatalf("blame=%v width=%d cursor=%d row=%d wrap=%d bg=%q:\ngot  %q\nwant %q", blame, width, cursor, i, k, bg, got, line)
							}
						}
					}
				}
			}
		}
	}
}

func TestRendererReusesRetainedRowsAndBoundsCacheToViewport(t *testing.T) {
	t.Parallel()
	files := []diff.FileDiff{{NewPath: "a.go"}}
	rows := make([]Row, 500)
	for i := range rows {
		rows[i] = Row{Kind: RowLine, Line: diff.Line{Content: fmt.Sprintf("value := %d", i), NewLine: i + 1}}
	}
	var renderer Renderer
	options := RenderOptions{Renderer: &renderer}
	_ = renderDiffRows(files, rows, 0, 0, 0, 80, 7, nil, options)
	retained := renderer.entries[2]
	base := &retained.base[0]
	_ = renderDiffRows(files, rows, 2, 2, 0, 80, 7, nil, options)
	if renderer.entries[2] != retained || &renderer.entries[2].base[0] != base {
		t.Fatal("cursor movement or scrolling rebuilt an overlapping row")
	}
	for top := 4; top < len(rows); top += 2 {
		_ = renderDiffRows(files, rows, top, top, 0, 80, 7, nil, options)
		if len(renderer.entries) > 7 {
			t.Fatalf("cache retained %d rows for a seven-line viewport", len(renderer.entries))
		}
		for index := range renderer.entries {
			if index < top || index >= top+7 {
				t.Fatalf("cache retained off-screen row %d at top %d", index, top)
			}
		}
	}
	_ = diffLinesWithOptions(nil, nil, 0, 0, 0, 80, 7, nil, false, options)
	if len(renderer.entries) != 0 || renderer.key.rows != nil {
		t.Fatal("empty view retained the previous file's render data")
	}
}

func TestRendererInvalidatesChangedInputs(t *testing.T) {
	t.Parallel()
	files := wrappedTestFiles()
	rows := append([]Row{{Kind: RowFileHeader}}, PairRows(FlattenFile(files, 0))...)
	hl := highlight.New()
	width := 80
	var renderer Renderer
	check := func() {
		t.Helper()
		cached := renderDiffRows(files, rows, 1, 0, 0, width, 8, hl, RenderOptions{Renderer: &renderer})
		fresh := renderDiffRows(files, rows, 1, 0, 0, width, 8, hl, RenderOptions{})
		if !reflect.DeepEqual(cached, fresh) {
			t.Fatalf("cached frame differs from fresh rendering:\n%q\n%q", cached, fresh)
		}
	}
	check()
	files[0].NewPath = "renamed.py"
	files[0].Added = 42
	check()
	rows[2].DiagnosticMarker = "E"
	rows[2].BlameGutter, rows[2].BlameText = true, "abc123 author"
	check()
	if rows[2].Left != nil {
		rows[2].Left.Content = "changed in place"
	}
	if rows[2].Right != nil {
		rows[2].Right.Content = "also changed in place"
	}
	check()
	width = 30 // unified fallback for paired rows
	check()
	hl = highlight.NewWithOptions(highlight.Options{Disabled: true})
	check()
	rows = append([]Row(nil), rows...)
	check()
	rows = append(rows, Row{Kind: RowComment, Text: "a newly inserted comment"})
	check()
	rows = FlattenFile(files, 0)
	check()
	rows[1].Line.Content = "edited unified content 界"
	rows[1].Changed = true
	check()
	rows[1] = Row{Kind: RowComment, Text: "edited comment", CommentHeader: true}
	check()
	rows[1].Muted = true
	check()
}

// Theme changes mutate package styles, so this test cannot run in parallel.
func TestRendererInvalidatesTheme(t *testing.T) {
	defer SetTheme(CurrentTheme())
	files := wrappedTestFiles()
	rows := FlattenFile(files, 0)
	var renderer Renderer
	for _, theme := range []Theme{DarkTheme(), LightTheme(), DarkTheme()} {
		SetTheme(theme)
		cached := renderDiffRows(files, rows, 1, 0, 0, 80, 8, nil, RenderOptions{Renderer: &renderer})
		fresh := renderDiffRows(files, rows, 1, 0, 0, 80, 8, nil, RenderOptions{})
		if !reflect.DeepEqual(cached, fresh) {
			t.Fatal("theme change reused stale row styling")
		}
	}
}

func TestMatchSourceReceivesExactVisibleRowRange(t *testing.T) {
	t.Parallel()
	files := []diff.FileDiff{{NewPath: "a.go"}}
	rows := []Row{
		{Kind: RowLine, Line: diff.Line{Content: "above"}},
		{Kind: RowLine, Line: diff.Line{Content: strings.Repeat("x", 120)}},
		{Kind: RowLine, Line: diff.Line{Content: "below"}},
	}
	search := MatchSpan{RowIdx: 1, Start: 20, End: 40, Current: true}
	cursor := MatchSpan{RowIdx: 1, Start: 25, End: 26, Cursor: true}
	var renderer Renderer
	for _, height := range []int{2, 6, 2} {
		calls := 0
		got := renderDiffRows(files, rows, 1, 1, 1, 30, height, nil, RenderOptions{
			Renderer: &renderer,
			Matches:  []MatchSpan{cursor},
			MatchSource: func(first, end int) []MatchSpan {
				calls++
				wantEnd := 2
				if height == 6 {
					wantEnd = 3
				}
				if first != 1 || end != wantEnd {
					t.Fatalf("height %d requested [%d,%d), want [1,%d)", height, first, end, wantEnd)
				}
				return []MatchSpan{search}
			},
		})
		want := renderDiffRows(files, rows, 1, 1, 1, 30, height, nil, RenderOptions{Matches: []MatchSpan{search, cursor}})
		if calls != 1 || !reflect.DeepEqual(got, want) {
			t.Fatal("match source changed highlight order or was queried more than once")
		}
		search.Current = !search.Current
	}
}
