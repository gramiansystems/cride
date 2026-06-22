package annotate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleReview() Review {
	t0 := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	return Review{
		Baseline: "abc123",
		Comments: []Comment{
			{
				ID: "2", Body: "prefer a map here", Severity: SeverityNit,
				Created: t0.Add(time.Minute),
				Anchor:  &Anchor{Path: "b.go", Side: SideCurrent, LineStart: 7, LineEnd: 7},
				Status:  StatusOpen, Snippet: "for i := range xs {",
			},
			{
				ID: "1", Body: "this can nil-panic\nsee the constructor", Severity: SeverityMustFix,
				Created: t0,
				Anchor:  &Anchor{Path: "a.go", Side: SideCurrent, LineStart: 42, LineEnd: 42},
				Status:  StatusOpen, Snippet: "x := y.Value()",
			},
			{
				ID: "3", Body: "overall direction looks right", Severity: SeverityQuestion,
				Created: t0.Add(2 * time.Minute), Status: StatusResolved,
			},
			{
				ID: "4", Body: "old name was clearer", Severity: SeverityNit,
				Created: t0.Add(3 * time.Minute),
				Anchor:  &Anchor{Path: "a.go", Side: SideBaseline, LineStart: 10, LineEnd: 10},
				Status:  StatusUnresolved, Snippet: "func OldName() {",
			},
		},
	}
}

func TestStoreRoundTripAndAtomicity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, StoreName)

	// Missing file is an empty review, not an error.
	empty, err := Load(path)
	if err != nil || len(empty.Comments) != 0 {
		t.Fatalf("missing file: %v %v", empty, err)
	}

	review := sampleReview()
	if err := Save(path, review); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FormatVersion != FormatVersion {
		t.Fatalf("format version = %d", loaded.FormatVersion)
	}
	if len(loaded.Comments) != 4 {
		t.Fatalf("comments = %d, want 4", len(loaded.Comments))
	}
	// Stored sorted by file/line; general comments last.
	if loaded.Comments[0].Anchor.Path != "a.go" || loaded.Comments[0].Anchor.LineStart != 10 {
		t.Fatalf("first stored comment = %+v", loaded.Comments[0].Anchor)
	}
	if loaded.Comments[3].Anchor != nil {
		t.Fatal("general comment not last")
	}

	// No temp litter after save.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}

	// Corrupt file errors but yields a usable empty review.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err == nil {
		t.Fatal("corrupt file did not error")
	}
	if got.FormatVersion != FormatVersion || len(got.Comments) != 0 {
		t.Fatalf("corrupt load fallback = %+v", got)
	}

	// Future-versioned file refuses rather than clobbering.
	if err := os.WriteFile(path, []byte(`{"format_version": 99, "comments": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("future format version did not error")
	}
}

func TestExportMarkdownGolden(t *testing.T) {
	t.Parallel()

	got := string(ExportMarkdown(sampleReview()))
	want := `# Review

Baseline: abc123

## a.go

### L10 [nit] (baseline) (detached)
> func OldName() {
old name was clearer

### L42 [must-fix]
> x := y.Value()
this can nil-panic
see the constructor

## b.go

### L7 [nit]
> for i := range xs {
prefer a map here

## General Comments

- **[question]** ✓ resolved
  overall direction looks right
`
	if got != want {
		t.Fatalf("export mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
