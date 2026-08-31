package ui

import (
	"testing"

	"cride/internal/diff"
)

func TestFlattenFullFileMarksChangedCurrentLines(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Lines: []diff.Line{
				{Kind: diff.LineContext, OldLine: 1, NewLine: 1, Content: "one"},
				{Kind: diff.LineDelete, OldLine: 2, Content: "old"},
				{Kind: diff.LineAdd, NewLine: 2, Content: "new"},
				{Kind: diff.LineContext, OldLine: 3, NewLine: 3, Content: "three"},
			},
		}},
	}}

	rows := FlattenFullFile(files, 0, []string{"one", "new", "three"})
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].Changed {
		t.Fatal("line 1 marked changed")
	}
	if !rows[1].Changed {
		t.Fatal("line 2 not marked changed")
	}
	if rows[2].Changed {
		t.Fatal("line 3 marked changed")
	}
	if rows[1].Line.NewLine != 2 || rows[1].Line.Content != "new" {
		t.Fatalf("line 2 row = %+v", rows[1].Line)
	}
}

func TestFlattenFullFileIndexesChangedLinesByHunk(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{
			{Lines: []diff.Line{{Kind: diff.LineAdd, NewLine: 2, Content: "two"}}},
			{Lines: []diff.Line{{Kind: diff.LineAdd, NewLine: 5, Content: "five"}}},
		},
	}}

	rows := FlattenFullFile(files, 0, []string{"one", "two", "three", "four", "five", "six"})
	for _, tc := range []struct {
		line    int
		changed bool
		hunk    int
	}{
		{line: 1},
		{line: 2, changed: true, hunk: 1},
		{line: 4},
		{line: 5, changed: true, hunk: 2},
	} {
		row := rows[tc.line-1]
		if row.Changed != tc.changed || row.HunkIdx != tc.hunk {
			t.Fatalf("line %d changed/hunk = %v/%d, want %v/%d", tc.line, row.Changed, row.HunkIdx, tc.changed, tc.hunk)
		}
	}
}

func TestFlattenReviewFileFullKeepsDeletedRowsInline(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -1,3 +1,3 @@",
			NewStart: 1,
			NewLines: 3,
			Lines: []diff.Line{
				{Kind: diff.LineContext, OldLine: 1, NewLine: 1, Content: "one"},
				{Kind: diff.LineDelete, OldLine: 2, Content: "old"},
				{Kind: diff.LineAdd, NewLine: 2, Content: "new"},
				{Kind: diff.LineContext, OldLine: 3, NewLine: 3, Content: "three"},
			},
		}},
	}}

	rows := FlattenReviewFile(files, 0, []string{"one", "new", "three"}, nil, true)
	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5: %+v", len(rows), rows)
	}
	if rows[0].Kind != RowHunkHeader {
		t.Fatalf("first row kind = %v, want hunk header", rows[0].Kind)
	}
	if rows[2].Line.Kind != diff.LineDelete || rows[2].Line.Content != "old" {
		t.Fatalf("deleted row = %+v, want old delete", rows[2].Line)
	}
	if !rows[3].Changed || rows[3].Line.NewLine != 2 || rows[3].Line.Content != "new" {
		t.Fatalf("changed current row = %+v changed=%v, want new line 2", rows[3].Line, rows[3].Changed)
	}
}

func TestFlattenReviewFileLocalExpansionAddsCurrentContext(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Header:   "@@ -3,1 +3,1 @@",
			NewStart: 3,
			NewLines: 1,
			Lines: []diff.Line{{
				Kind:    diff.LineAdd,
				Content: "three",
				NewLine: 3,
			}},
		}},
	}}

	rows := FlattenReviewFile(files, 0, []string{"one", "two", "three", "four", "five"}, map[int]int{0: 1}, false)
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want header plus three current rows: %+v", len(rows), rows)
	}
	if rows[1].Line.NewLine != 2 || rows[3].Line.NewLine != 4 {
		t.Fatalf("expanded context lines = %d..%d, want 2..4", rows[1].Line.NewLine, rows[3].Line.NewLine)
	}
	if !rows[2].Changed || rows[2].HunkIdx != 1 {
		t.Fatalf("expanded changed row changed=%v hunk=%d, want changed hunk 1", rows[2].Changed, rows[2].HunkIdx)
	}
}
