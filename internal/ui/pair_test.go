package ui

import (
	"strings"
	"testing"

	"cride/internal/diff"
)

func pairKinds(pairs []linePair) string {
	var b strings.Builder
	for _, p := range pairs {
		switch {
		case p.Left != nil && p.Right != nil && p.Left == p.Right:
			b.WriteByte('=')
		case p.Left != nil && p.Right != nil:
			b.WriteByte('x')
		case p.Left != nil:
			b.WriteByte('<')
		case p.Right != nil:
			b.WriteByte('>')
		default:
			b.WriteByte('!')
		}
	}
	return b.String()
}

func TestAlignPairsGoldens(t *testing.T) {
	t.Parallel()

	ctx := func(n int) diff.Line { return diff.Line{Kind: diff.LineContext, OldLine: n, NewLine: n} }
	del := func(n int) diff.Line { return diff.Line{Kind: diff.LineDelete, OldLine: n} }
	add := func(n int) diff.Line { return diff.Line{Kind: diff.LineAdd, NewLine: n} }

	tests := []struct {
		name  string
		lines []diff.Line
		want  string
	}{
		{"balanced", []diff.Line{ctx(1), del(2), del(3), add(2), add(3), ctx(4)}, "=xx="},
		{"more deletes", []diff.Line{del(1), del(2), del(3), add(1)}, "x<<"},
		{"adds only", []diff.Line{ctx(1), add(2), add(3)}, "=>>"},
		{"deletes only", []diff.Line{del(1), ctx(2)}, "<="},
		{"interleaved", []diff.Line{del(1), add(1), del(2), add(2), ctx(3)}, "xx="},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := pairKinds(alignPairs(tt.lines))
			if got != tt.want {
				t.Fatalf("alignPairs shape = %q, want %q", got, tt.want)
			}
			// Determinism.
			if again := pairKinds(alignPairs(tt.lines)); again != got {
				t.Fatalf("alignPairs not deterministic: %q vs %q", got, again)
			}
		})
	}
}

func TestPairRowsKeepsHeadersAndPrimaryLine(t *testing.T) {
	t.Parallel()

	files := wrappedTestFiles()
	rows := FlattenFile(files, 0)
	paired := PairRows(rows)

	if paired[0].Kind != RowHunkHeader {
		t.Fatalf("first paired row kind = %v, want hunk header", paired[0].Kind)
	}
	for _, r := range paired[1:] {
		if r.Kind != RowPair {
			t.Fatalf("unexpected row kind %v after pairing", r.Kind)
		}
		if r.Left == nil && r.Right == nil {
			t.Fatal("pair row with both sides nil")
		}
		if r.Right != nil && r.Line != *r.Right {
			t.Fatal("primary line is not the current side")
		}
		if r.Right == nil && r.Line != *r.Left {
			t.Fatal("primary line fallback is not the baseline side")
		}
	}
}

func TestPairRowHeightIsTallerSide(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("pair-content ", 30)
	files := []diff.FileDiff{{
		NewPath: "a.go",
		OldPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -1,2 +1,2 @@",
			Lines: []diff.Line{
				{Kind: diff.LineDelete, Content: "short", OldLine: 1},
				{Kind: diff.LineAdd, Content: long, NewLine: 1},
			},
		}},
	}}
	rows := PairRows(FlattenFile(files, 0))
	width := 110
	lw, rw, ok := PairColumnWidths(width)
	if !ok {
		t.Fatal("width too narrow for split in test")
	}

	var pairIdx int
	for i, r := range rows {
		if r.Kind == RowPair {
			pairIdx = i
			break
		}
	}
	lines := pairRowLines(files, rows[pairIdx], nil, pairIdx, 0, width)
	wantRight := len(wrapLine(strings.ReplaceAll(long, "\t", "    "), rw))
	if len(lines) != wantRight {
		t.Fatalf("pair row height = %d, want taller side %d (lw=%d rw=%d)", len(lines), wantRight, lw, rw)
	}
	// Layout agrees with rendering.
	layout := BuildWrapLayout(files, rows, width)
	if layout.RowHeight(pairIdx) != len(lines) {
		t.Fatalf("layout height %d != rendered height %d", layout.RowHeight(pairIdx), len(lines))
	}
	// Every rendered line is exactly the panel width.
	for k, line := range lines {
		if got := len([]rune(stripANSI(line))); got != width {
			t.Fatalf("pair line %d width = %d, want %d", k, got, width)
		}
	}
}
