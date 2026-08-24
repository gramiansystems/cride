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

func TestSaveMarkdownReplacesAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ExportName)
	if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	review := sampleReview()
	if err := SaveMarkdown(path, review); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), string(ExportMarkdown(review)); got != want {
		t.Fatalf("saved markdown mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("saved markdown mode = %o, want 644", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary export left behind: %s", entry.Name())
		}
	}
}

func TestMarkdownRoundTripPreservesReview(t *testing.T) {
	t.Parallel()

	existing := sampleReview()
	parsed, err := ParseMarkdown(ExportMarkdown(existing), existing)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(ExportMarkdown(parsed)), string(ExportMarkdown(existing)); got != want {
		t.Fatalf("round-trip mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	ids := make(map[string]bool, len(parsed.Comments))
	for _, comment := range parsed.Comments {
		ids[comment.ID] = true
	}
	for _, comment := range existing.Comments {
		if !ids[comment.ID] {
			t.Fatalf("comment identity %q was not preserved", comment.ID)
		}
	}
}

func TestParseMarkdownAcceptsBodyEditsAndNewComments(t *testing.T) {
	t.Parallel()

	existing := sampleReview()
	edited := strings.Replace(string(ExportMarkdown(existing)), "this can nil-panic", "this is definitely unsafe", 1)
	edited += "\n- **[nit]**\n  remember the benchmark\n"
	parsed, err := ParseMarkdown([]byte(edited), existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Comments) != len(existing.Comments)+1 {
		t.Fatalf("comments = %d, want %d", len(parsed.Comments), len(existing.Comments)+1)
	}
	var changed, added Comment
	for _, comment := range parsed.Comments {
		switch comment.Body {
		case "this is definitely unsafe\nsee the constructor":
			changed = comment
		case "remember the benchmark":
			added = comment
		}
	}
	if changed.ID != "1" {
		t.Fatalf("edited comment ID = %q, want preserved ID 1", changed.ID)
	}
	if added.ID == "" || added.Created.IsZero() {
		t.Fatalf("new comment lacks generated metadata: %+v", added)
	}
}

func TestParseMarkdownRejectsMalformedCommentHeading(t *testing.T) {
	t.Parallel()

	data := []byte("# Review\n\n## a.go\n\n### near line ten [urgent]\nfix this\n")
	if _, err := ParseMarkdown(data, Review{}); err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("malformed heading error = %v", err)
	}
}
