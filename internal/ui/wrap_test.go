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
