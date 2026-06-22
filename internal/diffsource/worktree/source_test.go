package worktree

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cride/internal/diff"
	"cride/internal/diffsource"
)

func TestWorkingTreeSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-q")
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	gitRun("add", "a.txt")
	gitRun("commit", "-qm", "init")

	// Modify a tracked file and add an untracked one.
	writeFile(t, dir, "a.txt", "one\nTWO\nthree\n")
	writeFile(t, dir, "b.txt", "brand new\n")

	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := src.Diff()
	if err != nil {
		t.Fatal(err)
	}
	files, err := diff.ParseReview(raw)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, raw)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d:\n%s", len(files), raw)
	}

	byPath := map[string]diff.FileDiff{}
	for _, f := range files {
		byPath[f.Path()] = f
	}
	if a, ok := byPath["a.txt"]; !ok || a.Status != diff.FileModified {
		t.Errorf("a.txt: status=%v ok=%v", a.Status, ok)
	}
	if b, ok := byPath["b.txt"]; !ok || b.Status != diff.FileAdded {
		t.Errorf("b.txt: status=%v ok=%v", b.Status, ok)
	}
}

func TestWorkingTreeContentAPIs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-q")
	writeFile(t, dir, "a.txt", "base\n")
	writeFile(t, dir, "deleted.txt", "gone\n")
	gitRun("add", "a.txt", "deleted.txt")
	gitRun("commit", "-qm", "init")

	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "a.txt", "current\n")
	if err := os.Remove(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	gitRun("add", "a.txt", "deleted.txt")
	gitRun("commit", "-qm", "move head")
	writeFile(t, dir, "added.txt", "new\n")
	writeFile(t, dir, "huge.txt", strings.Repeat("x", diffsource.MaxContentBytes+1))

	current, err := src.CurrentContent("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "current\n" {
		t.Fatalf("CurrentContent(a.txt) = %q", current)
	}

	baseline, err := src.BaselineContent("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(baseline) != "base\n" {
		t.Fatalf("BaselineContent(a.txt) = %q, want pinned base", baseline)
	}

	if _, err := src.BaselineContent("added.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("BaselineContent(added.txt) err = %v, want fs.ErrNotExist", err)
	}
	if _, err := src.CurrentContent("deleted.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("CurrentContent(deleted.txt) err = %v, want fs.ErrNotExist", err)
	}
	if _, err := src.CurrentContent("huge.txt"); !errors.Is(err, diffsource.ErrFileTooLarge) {
		t.Fatalf("CurrentContent(huge.txt) err = %v, want ErrFileTooLarge", err)
	}

	paths, err := src.ChangedPaths()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range paths {
		got[p] = true
	}
	for _, want := range []string{"a.txt", "added.txt", "deleted.txt", "huge.txt"} {
		if !got[want] {
			t.Fatalf("ChangedPaths missing %q: %v", want, paths)
		}
	}
}

func TestProjectFilesExcludesIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-q")
	writeFile(t, dir, ".gitignore", "*.log\n")
	writeFile(t, dir, "tracked.txt", "tracked\n")
	gitRun("add", ".gitignore", "tracked.txt")
	gitRun("commit", "-qm", "init")

	writeFile(t, dir, "untracked.txt", "untracked\n")
	writeFile(t, dir, "ignored.log", "ignored\n")

	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := src.ProjectFiles()
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]bool{}
	for _, path := range paths {
		got[path] = true
	}
	for _, want := range []string{".gitignore", "tracked.txt", "untracked.txt"} {
		if !got[want] {
			t.Fatalf("ProjectFiles missing %q: %v", want, paths)
		}
	}
	if got["ignored.log"] {
		t.Fatalf("ProjectFiles included ignored.log: %v", paths)
	}
}

func TestCommitSourceUsesCommitSnapshot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, gitRun, gitOutput := newTestGitRepo(t)
	gitRun("init", "-q")
	writeFile(t, dir, "a.txt", "base\nneedle old\n")
	gitRun("add", "a.txt")
	gitRun("commit", "-qm", "base")

	writeFile(t, dir, "a.txt", "commit\nneedle new\n")
	writeFile(t, dir, "b.txt", "added\n")
	gitRun("add", "a.txt", "b.txt")
	gitRun("commit", "-qm", "target")
	target := gitOutput("rev-parse", "HEAD")

	writeFile(t, dir, "a.txt", "dirty worktree\n")
	writeFile(t, dir, "c.txt", "untracked\n")

	src, err := OpenCommit(dir, target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(src.Baseline(), "..") {
		t.Fatalf("Baseline() = %q, want commit range display", src.Baseline())
	}

	files := parseSourceDiff(t, src)
	byPath := map[string]diff.FileDiff{}
	for _, f := range files {
		byPath[f.Path()] = f
	}
	if a, ok := byPath["a.txt"]; !ok || a.Status != diff.FileModified {
		t.Fatalf("a.txt status = %v ok=%v", a.Status, ok)
	}
	if b, ok := byPath["b.txt"]; !ok || b.Status != diff.FileAdded {
		t.Fatalf("b.txt status = %v ok=%v", b.Status, ok)
	}
	if _, ok := byPath["c.txt"]; ok {
		t.Fatalf("commit diff included untracked worktree file: %+v", files)
	}

	current, err := src.CurrentContent("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "commit\nneedle new\n" {
		t.Fatalf("CurrentContent(a.txt) = %q, want committed content", current)
	}
	baseline, err := src.BaselineContent("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(baseline) != "base\nneedle old\n" {
		t.Fatalf("BaselineContent(a.txt) = %q, want parent content", baseline)
	}

	paths, err := src.ChangedPaths()
	if err != nil {
		t.Fatal(err)
	}
	gotPaths := pathSet(paths)
	for _, want := range []string{"a.txt", "b.txt"} {
		if !gotPaths[want] {
			t.Fatalf("ChangedPaths missing %q: %v", want, paths)
		}
	}
	if gotPaths["c.txt"] {
		t.Fatalf("ChangedPaths included untracked worktree file: %v", paths)
	}

	projectFiles, err := src.ProjectFiles()
	if err != nil {
		t.Fatal(err)
	}
	gotProject := pathSet(projectFiles)
	for _, want := range []string{"a.txt", "b.txt"} {
		if !gotProject[want] {
			t.Fatalf("ProjectFiles missing %q: %v", want, projectFiles)
		}
	}
	if gotProject["c.txt"] {
		t.Fatalf("ProjectFiles included untracked worktree file: %v", projectFiles)
	}

	results, err := src.SearchWord("needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("SearchWord results = %d, want before and after hits: %+v", len(results), results)
	}
	gotSides := map[diffsourceSearchSide]bool{}
	for _, result := range results {
		if result.Location.Path != "a.txt" || result.Location.Line != 2 {
			t.Fatalf("SearchWord result location = %+v, want a.txt:2", result.Location)
		}
		gotSides[diffsourceSearchSide(result.Side.String())] = true
	}
	for _, want := range []diffsourceSearchSide{"before", "after"} {
		if !gotSides[want] {
			t.Fatalf("SearchWord missing %s result: %+v", want, results)
		}
	}
}

func TestRangeSourceUsesRangeEndpointSnapshot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, gitRun, gitOutput := newTestGitRepo(t)
	gitRun("init", "-q")
	writeFile(t, dir, "r.txt", "base\n")
	gitRun("add", "r.txt")
	gitRun("commit", "-qm", "base")
	base := gitOutput("rev-parse", "HEAD")

	writeFile(t, dir, "r.txt", "mid\n")
	gitRun("add", "r.txt")
	gitRun("commit", "-qm", "mid")

	writeFile(t, dir, "r.txt", "head\nrangeword\n")
	gitRun("add", "r.txt")
	gitRun("commit", "-qm", "head")
	head := gitOutput("rev-parse", "HEAD")

	writeFile(t, dir, "r.txt", "dirty worktree\n")

	src, err := OpenRange(dir, base+".."+head)
	if err != nil {
		t.Fatal(err)
	}
	files := parseSourceDiff(t, src)
	if len(files) != 1 || files[0].Path() != "r.txt" || files[0].Status != diff.FileModified {
		t.Fatalf("range files = %+v, want one modified r.txt", files)
	}

	current, err := src.CurrentContent("r.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "head\nrangeword\n" {
		t.Fatalf("CurrentContent(r.txt) = %q, want range head content", current)
	}
	baseline, err := src.BaselineContent("r.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(baseline) != "base\n" {
		t.Fatalf("BaselineContent(r.txt) = %q, want range base content", baseline)
	}

	results, err := src.SearchWord("rangeword")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Location.Path != "r.txt" || results[0].Location.Line != 2 {
		t.Fatalf("SearchWord results = %+v, want r.txt:2", results)
	}

	threeDot, err := OpenRange(dir, base+"..."+head)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(threeDot.Baseline(), "...") {
		t.Fatalf("Baseline() = %q, want three-dot display", threeDot.Baseline())
	}
}

func TestWorkingTreeSearchWordUsesWholeWordMatches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-q")
	writeFile(t, dir, "a.go", "Targeted()\nTarget()\nnotTarget()\n")

	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := src.SearchWord("Target")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1: %+v", len(results), results)
	}
	if results[0].Location.Path != "a.go" || results[0].Location.Line != 2 || results[0].Location.Column != 1 {
		t.Fatalf("result location = %+v, want a.go:2:1", results[0].Location)
	}
}

func parseSourceDiff(t *testing.T, src *Source) []diff.FileDiff {
	t.Helper()
	raw, err := src.Diff()
	if err != nil {
		t.Fatal(err)
	}
	files, err := diff.ParseReview(raw)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, raw)
	}
	return files
}

func pathSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, path := range paths {
		out[path] = true
	}
	return out
}

type diffsourceSearchSide string

func newTestGitRepo(t *testing.T) (string, func(...string), func(...string) string) {
	t.Helper()
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitOutput := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	return dir, gitRun, gitOutput
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
