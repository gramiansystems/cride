package ui

import (
	"strconv"
	"strings"
	"testing"

	"cride/internal/diff"
)

func manyFiles(n int) []diff.FileDiff {
	files := make([]diff.FileDiff, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, diff.FileDiff{
			NewPath: "pkg/file_" + strconv.Itoa(i) + ".go",
			Status:  diff.FileModified,
			Added:   1,
		})
	}
	return files
}

func TestChangeListScrollIndicatorAppearsOnOverflow(t *testing.T) {
	t.Parallel()

	view := BuildChangeListView(manyFiles(40), nil, nil, 0, -1, 0, 10, false)
	lines := changeListLines(view, manyFiles(40), 28)
	if len(lines) != 10 {
		t.Fatalf("rendered %d lines, want 10", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "█") {
		t.Fatalf("overflowing list missing scrollbar thumb:\n%s", stripANSI(joined))
	}

	// A short list has no scrollbar.
	small := BuildChangeListView(manyFiles(3), nil, nil, 0, -1, 0, 10, false)
	joined = strings.Join(changeListLines(small, manyFiles(3), 28), "\n")
	if strings.Contains(joined, "█") || strings.Contains(joined, "░") {
		t.Fatalf("short list unexpectedly has scrollbar:\n%s", stripANSI(joined))
	}
}

func TestBuildChangeListViewKeepsAnchorVisible(t *testing.T) {
	t.Parallel()

	files := manyFiles(40)
	// Unfocused: selection far down the list scrolls into view.
	view := BuildChangeListView(files, nil, nil, 30, -1, 0, 10, false)
	if view.Selected < view.Top || view.Selected >= view.Top+view.Height {
		t.Fatalf("selected row %d outside window [%d,%d)", view.Selected, view.Top, view.Top+view.Height)
	}

	// Focused: the cursor is the anchor instead.
	view = BuildChangeListView(files, nil, nil, 2, 35, 0, 10, true)
	if view.Cursor < view.Top || view.Cursor >= view.Top+view.Height {
		t.Fatalf("cursor row %d outside window [%d,%d)", view.Cursor, view.Top, view.Top+view.Height)
	}
}

func TestChangeListRowAtMatchesRendering(t *testing.T) {
	t.Parallel()

	files := manyFiles(40)
	view := BuildChangeListView(files, nil, nil, 30, -1, 0, 10, false)
	for line := 0; line < view.Height; line++ {
		idx := view.RowAt(line)
		if idx != view.Top+line {
			t.Fatalf("RowAt(%d) = %d, want %d", line, idx, view.Top+line)
		}
	}
	if view.RowAt(-1) != -1 || view.RowAt(view.Height) != -1 {
		t.Fatal("out-of-range lines must return -1")
	}
}

func TestChangeListChangeOrderFloatsRecentFilesAndDirs(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		{NewPath: "a.go", Status: diff.FileModified},
		{NewPath: "internal/app.go", Status: diff.FileModified},
		{NewPath: "b.go", Status: diff.FileModified},
	}
	opts := ChangeListOptions{
		Order: ChangeListOrderChanged,
		ChangeOrdinal: map[string]int{
			"a.go":            1,
			"internal/app.go": 3,
			"b.go":            2,
		},
	}

	order := ChangeListFileOrderWithOptions(files, opts)
	want := []int{1, 2, 0}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}

	rows := ChangeListRowsWithOptions(files, nil, nil, opts)
	if len(rows) < 2 || !rows[0].IsDir || rows[0].Path != "internal" || rows[1].Path != "internal/app.go" {
		t.Fatalf("recent directory did not float to the top: %+v", rows[:min(len(rows), 2)])
	}
}

func TestChangeListOrderDefaultAndParse(t *testing.T) {
	t.Parallel()

	if DefaultChangeListOrder != ChangeListOrderChanged {
		t.Fatalf("default order = %v, want changed", DefaultChangeListOrder)
	}
	if order, ok := ParseChangeListOrder(""); ok || order != DefaultChangeListOrder {
		t.Fatalf("empty order parse = %v/%v, want default/false", order, ok)
	}
	if order, ok := ParseChangeListOrder("path"); !ok || order != ChangeListOrderPath {
		t.Fatalf("path order parse = %v/%v, want path/true", order, ok)
	}
}
