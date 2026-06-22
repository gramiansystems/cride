package diff

import "testing"

func TestReviewIndexMarkersForAddedContextDeletedRows(t *testing.T) {
	t.Parallel()

	files := []FileDiff{{
		OldPath: "a.go",
		NewPath: "a.go",
		Status:  FileModified,
		Hunks: []Hunk{{
			Header: "@@ -1,3 +1,3 @@",
			Lines: []Line{
				{Kind: LineContext, OldLine: 1, NewLine: 1},
				{Kind: LineDelete, OldLine: 2},
				{Kind: LineAdd, NewLine: 3},
			},
		}},
	}}

	idx := NewReviewIndex(files)
	tests := []struct {
		name        string
		line        int
		wantKind    ChangeKind
		wantChanged bool
	}{
		{name: "context", line: 1, wantKind: ChangeContext},
		{name: "deleted", line: 2, wantKind: ChangeDeleted, wantChanged: true},
		{name: "added", line: 3, wantKind: ChangeAdded, wantChanged: true},
		{name: "outside", line: 4, wantKind: ChangeNone},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			markers := idx.Markers("a.go", tt.line)
			if markers.ChangeKind != tt.wantKind {
				t.Fatalf("ChangeKind = %v, want %v", markers.ChangeKind, tt.wantKind)
			}
			if markers.Changed != tt.wantChanged {
				t.Fatalf("Changed = %v, want %v", markers.Changed, tt.wantChanged)
			}
		})
	}
}

func TestReviewIndexDelegatesUnreadAndAnnotations(t *testing.T) {
	t.Parallel()

	idx := NewReviewIndex([]FileDiff{{
		NewPath: "a.go",
		Status:  FileModified,
		Hunks: []Hunk{{
			Lines: []Line{{Kind: LineAdd, NewLine: 2}},
		}},
	}},
		WithUnreadIndex(LineSet{"a.go": {2: true}}),
		WithAnnotationIndex(AnnotationMap{"a.go": {2: AnnotationQuestion}}),
	)

	markers := idx.Markers("a.go", 2)
	if !markers.Unread {
		t.Fatal("Unread = false, want true")
	}
	if !markers.Annotated || markers.Annotation != AnnotationQuestion {
		t.Fatalf("annotation markers = %v/%v, want question", markers.Annotated, markers.Annotation)
	}
}
