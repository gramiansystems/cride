package ui

import (
	"strings"
	"testing"

	"cride/internal/diff"
	"cride/internal/highlight"
)

func wrappedTestFiles() []diff.FileDiff {
	long := strings.Repeat("word ", 60) // ~300 cols, wraps at any sane width
	return []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,4 +1,4 @@",
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "short one", OldLine: 1, NewLine: 1},
				{Kind: diff.LineAdd, Content: long, NewLine: 2},
				{Kind: diff.LineContext, Content: "short two", OldLine: 3, NewLine: 3},
				{Kind: diff.LineDelete, Content: long + "tail", OldLine: 4},
			},
		}},
	}}
}

func TestWrapLayoutRoundTrip(t *testing.T) {
	t.Parallel()

	files := wrappedTestFiles()
	rows := FlattenFile(files, 0)
	l := BuildWrapLayout(files, rows, 60)

	if l.NumRows() != len(rows) {
		t.Fatalf("NumRows = %d, want %d", l.NumRows(), len(rows))
	}
	sum := 0
	for i := range rows {
		if l.RowHeight(i) < 1 {
			t.Fatalf("row %d height = %d, want >= 1", i, l.RowHeight(i))
		}
		if l.RowStart(i) != sum {
			t.Fatalf("row %d start = %d, want %d", i, l.RowStart(i), sum)
		}
		sum += l.RowHeight(i)
	}
	if l.TotalLines() != sum {
		t.Fatalf("TotalLines = %d, want %d", l.TotalLines(), sum)
	}
	if l.TotalLines() <= len(rows) {
		t.Fatalf("expected wrapped rows to produce more screen lines than rows: %d <= %d", l.TotalLines(), len(rows))
	}

	// Every screen line belongs to exactly one row; round trip is exact.
	for idx := 0; idx < l.TotalLines(); idx++ {
		sl := l.LineAt(idx)
		if got := l.RowStart(sl.RowIdx) + sl.WrapIdx; got != idx {
			t.Fatalf("LineAt(%d) round trip = %d", idx, got)
		}
		if sl.WrapIdx < 0 || sl.WrapIdx >= l.RowHeight(sl.RowIdx) {
			t.Fatalf("LineAt(%d) wrap idx %d outside row height %d", idx, sl.WrapIdx, l.RowHeight(sl.RowIdx))
		}
	}

	// Out-of-range indexes clamp instead of panicking.
	if sl := l.LineAt(-5); sl.RowIdx != 0 || sl.WrapIdx != 0 {
		t.Fatalf("LineAt(-5) = %+v, want first line", sl)
	}
	last := l.LineAt(l.TotalLines() + 10)
	if last.RowIdx != len(rows)-1 || last.WrapIdx != l.RowHeight(len(rows)-1)-1 {
		t.Fatalf("LineAt(beyond) = %+v, want last line", last)
	}
}

func TestWrapLayoutMatchesRenderedWrapCount(t *testing.T) {
	t.Parallel()

	files := wrappedTestFiles()
	rows := FlattenFile(files, 0)
	width := 48
	l := BuildWrapLayout(files, rows, width)

	// The styled render must wrap to exactly the same number of screen lines
	// the layout predicts, or scroll math and rendering drift apart.
	hl := highlight.New()
	for i, r := range rows {
		styled := wrapLine(renderRow(files, r, hl, i, 0), width)
		if len(styled) != l.RowHeight(i) {
			t.Fatalf("row %d: styled render wraps to %d lines, layout says %d", i, len(styled), l.RowHeight(i))
		}
	}
}

func TestRowScreenHeightMatchesRenderedRows(t *testing.T) {
	t.Parallel()

	files := wrappedTestFiles()
	files[0].Added = 123
	files[0].Deleted = 45
	rows := append([]Row{{Kind: RowFileHeader, FileIdx: 0}}, FlattenFile(files, 0)...)
	rows = append(rows,
		Row{Kind: RowComment, FileIdx: 0, Text: "comment with\ta tab and 界wide text"},
		Row{Kind: RowLine, FileIdx: 0, Line: diff.Line{Content: "combining e\u0301 and 界 glyphs"}},
		Row{Kind: RowLine, FileIdx: 0, Line: diff.Line{Content: "wide line numbers", OldLine: 12345, NewLine: 67890}},
	)
	rows = append(rows, PairRows(FlattenFile(files, 0))...)

	for _, width := range []int{1, 8, 17, 18, 19, 31, 48, 80} {
		for i, row := range rows {
			want := len(rowScreenLines(files, row, nil, i, 0, width))
			if got := rowScreenHeight(files, row, width); got != want {
				t.Fatalf("width %d row %d kind %d: measured %d lines, rendered %d", width, i, row.Kind, got, want)
			}
		}
	}
}

func TestRowTextPositionAtUnifiedText(t *testing.T) {
	t.Parallel()

	row := Row{
		Kind: RowLine,
		Line: diff.Line{Kind: diff.LineContext, Content: "a\t界z", OldLine: 1, NewLine: 1},
	}
	for _, tt := range []struct {
		name string
		x    int
		want int
	}{
		{name: "gutter", x: 4, want: 0},
		{name: "first rune", x: diffRowPrefixWidth, want: 0},
		{name: "tab first cell", x: diffRowPrefixWidth + 1, want: 1},
		{name: "tab last cell", x: diffRowPrefixWidth + 4, want: 1},
		{name: "wide rune first cell", x: diffRowPrefixWidth + 5, want: 2},
		{name: "wide rune second cell", x: diffRowPrefixWidth + 6, want: 2},
		{name: "following rune", x: diffRowPrefixWidth + 7, want: 3},
		{name: "end of line", x: diffRowPrefixWidth + 8, want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, baseline, ok := RowTextPositionAt(row, tt.x, 0, 40)
			if !ok || baseline || got != tt.want {
				t.Fatalf("RowTextPositionAt(x=%d) = (%d, %v, %v), want (%d, false, true)", tt.x, got, baseline, ok, tt.want)
			}
		})
	}
}

func TestRowTextPositionAtWrappedAndWideGutter(t *testing.T) {
	t.Parallel()

	row := Row{
		Kind: RowLine,
		Line: diff.Line{Kind: diff.LineContext, Content: "0123456789abcdef", OldLine: 12345, NewLine: 67890},
	}
	// Five-digit old and new line numbers widen the normal 18-cell gutter to
	// 20 cells. At width 24, wrapped screen line 1 therefore starts at rune 4.
	got, baseline, ok := RowTextPositionAt(row, 3, 1, 24)
	if !ok || baseline || got != 7 {
		t.Fatalf("wrapped hit = (%d, %v, %v), want (7, false, true)", got, baseline, ok)
	}
}

func TestRowTextPositionAtSplitSides(t *testing.T) {
	t.Parallel()

	left := diff.Line{Kind: diff.LineDelete, Content: "old\t界", OldLine: 1}
	right := diff.Line{Kind: diff.LineAdd, Content: "new value", NewLine: 1}
	row := Row{Kind: RowPair, Line: right, Left: &left, Right: &right}
	width := 80
	lw, rw, ok := PairColumnWidths(width)
	if !ok {
		t.Fatal("test width does not support split view")
	}
	leftStart := PairLeftCellEnd(lw) - lw
	rightStart := width - rw

	got, baseline, hit := RowTextPositionAt(row, leftStart+4, 0, width)
	if !hit || !baseline || got != 3 {
		t.Fatalf("left hit = (%d, %v, %v), want tab at (3, true, true)", got, baseline, hit)
	}
	got, baseline, hit = RowTextPositionAt(row, rightStart+5, 0, width)
	if !hit || baseline || got != 5 {
		t.Fatalf("right hit = (%d, %v, %v), want (5, false, true)", got, baseline, hit)
	}

	// Each split cell wraps independently and repeats its gutter. A click
	// three cells into the second left screen line lands on rune lw+3.
	left.Content = strings.Repeat("x", lw+8)
	got, baseline, hit = RowTextPositionAt(row, leftStart+3, 1, width)
	if !hit || !baseline || got != lw+3 {
		t.Fatalf("wrapped left hit = (%d, %v, %v), want (%d, true, true)", got, baseline, hit, lw+3)
	}
}

func TestRowTextPositionAtSplitBlankCellUsesNearestText(t *testing.T) {
	t.Parallel()

	left := diff.Line{Kind: diff.LineDelete, Content: "left", OldLine: 1}
	right := diff.Line{Kind: diff.LineAdd, Content: "right", NewLine: 1}
	width := 80
	lw, _, ok := PairColumnWidths(width)
	if !ok {
		t.Fatal("test width does not support split view")
	}

	row := Row{Kind: RowPair, Line: right, Right: &right}
	got, baseline, hit := RowTextPositionAt(row, 2, 0, width)
	if !hit || baseline || got != 0 {
		t.Fatalf("blank left hit = (%d, %v, %v), want right start", got, baseline, hit)
	}

	row = Row{Kind: RowPair, Line: left, Left: &left}
	got, baseline, hit = RowTextPositionAt(row, PairLeftCellEnd(lw)+1, 0, width)
	if !hit || !baseline || got != len([]rune(left.Content)) {
		t.Fatalf("blank right hit = (%d, %v, %v), want left end", got, baseline, hit)
	}
}

func TestDiffLinesHonorTopWrap(t *testing.T) {
	t.Parallel()

	files := wrappedTestFiles()
	rows := FlattenFile(files, 0)
	width := 48
	l := BuildWrapLayout(files, rows, width)

	// Render the whole buffer one screen line at a time; consecutive
	// single-line windows must reproduce every screen line exactly once.
	var got []string
	for idx := 0; idx < l.TotalLines(); idx++ {
		sl := l.LineAt(idx)
		lines := diffLines(files, rows, -1, sl.RowIdx, sl.WrapIdx, width, 1, nil)
		if len(lines) != 1 {
			t.Fatalf("window at %d rendered %d lines, want 1", idx, len(lines))
		}
		got = append(got, lines...)
	}

	full := diffLines(files, rows, -1, 0, 0, width, l.TotalLines(), nil)
	if len(full) != l.TotalLines() {
		t.Fatalf("full render = %d lines, want %d", len(full), l.TotalLines())
	}
	for i := range full {
		if got[i] != full[i] {
			t.Fatalf("screen line %d differs between scrolled and full render:\n%q\n%q", i, stripANSI(got[i]), stripANSI(full[i]))
		}
	}
}
