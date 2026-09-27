package app

import (
	"strings"
	"testing"
	"time"

	"cride/internal/blame"
	"cride/internal/diff"
	"cride/internal/ui"
)

func blameMappingFile() diff.FileDiff {
	return diff.FileDiff{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			OldStart: 2,
			OldLines: 2,
			NewStart: 2,
			NewLines: 3,
			Lines: []diff.Line{
				{Kind: diff.LineDelete, OldLine: 2, Content: "old"},
				{Kind: diff.LineAdd, NewLine: 2, Content: "new"},
				{Kind: diff.LineContext, OldLine: 3, NewLine: 3, Content: "keep"},
				{Kind: diff.LineAdd, NewLine: 4, Content: "more"},
			},
		}},
	}
}

func TestBaselineLineForCurrent(t *testing.T) {
	file := blameMappingFile()
	tests := []struct {
		current int
		want    int
	}{
		{1, 1},
		{2, 0},
		{3, 3},
		{4, 0},
		{5, 4},
	}
	for _, tt := range tests {
		if got := baselineLineForCurrent(file, tt.current); got != tt.want {
			t.Errorf("current line %d maps to %d, want %d", tt.current, got, tt.want)
		}
	}
}

func TestWithBlameRowsDecoratesBaselineLinesAndReservesMargin(t *testing.T) {
	file := blameMappingFile()
	authorTime := time.Date(2026, 9, 23, 12, 34, 56, 0, time.Local)
	infos := make([]blame.LineInfo, 4)
	for i := range infos {
		infos[i] = blame.LineInfo{
			SHA:        "abcdef0123456789",
			Author:     "Ada",
			AuthorTime: authorTime,
		}
	}
	m := Model{
		files:        []diff.FileDiff{file},
		selectedFile: 0,
		blameGutter:  true,
		blames: map[string]blameFileState{
			"a.go": {file: blame.FileBlame{Path: "a.go", Lines: infos}, loaded: true},
		},
	}
	rows := []ui.Row{
		{Kind: ui.RowHunkHeader, Text: "@@"},
		{Kind: ui.RowLine, Line: diff.Line{Kind: diff.LineContext, NewLine: 1, Content: "one"}},
		{Kind: ui.RowLine, Changed: true, Line: diff.Line{Kind: diff.LineContext, NewLine: 2, Content: "new"}},
		{Kind: ui.RowLine, Line: diff.Line{Kind: diff.LineContext, NewLine: 5, Content: "four"}},
	}
	got := m.withBlameRows(rows)
	for i, row := range got {
		if !row.BlameGutter {
			t.Fatalf("row %d did not reserve blame gutter", i)
		}
	}
	if got[1].BlameText != "abcdef0 2026-09-23 12:34 Ada" {
		t.Fatalf("line 1 blame = %q", got[1].BlameText)
	}
	if got[2].BlameText != "" {
		t.Fatalf("added line blame = %q, want blank", got[2].BlameText)
	}
	if !strings.Contains(got[3].BlameText, "abcdef0") {
		t.Fatalf("post-hunk blame = %q", got[3].BlameText)
	}
}

func TestGBAndZBToggleBlameGutter(t *testing.T) {
	for _, keys := range [][]string{{"g", "b"}, {"z", "b"}} {
		m := Model{files: []diff.FileDiff{testFile("a.go")}, width: 100, height: 24}
		for _, keyName := range keys {
			next, _ := m.handleKey(key(keyName))
			m = next.(Model)
		}
		if !m.blameGutter {
			t.Fatalf("%s did not enable blame gutter", strings.Join(keys, ""))
		}
	}
}
