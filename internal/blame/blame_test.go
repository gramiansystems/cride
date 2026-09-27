package blame

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCarriesCompactedCommitMetadata(t *testing.T) {
	const shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fixture := strings.Join([]string{
		shaA + " 1 1 1",
		"author Ada Lovelace",
		"author-time 1700000000",
		"summary initial implementation",
		"filename sample.go",
		"\tfirst",
		shaB + " 2 2 1",
		"author Grace Hopper",
		"author-time 1700100000",
		"summary fix parser",
		"filename sample.go",
		"\tsecond",
		shaA + " 3 3 1",
		"\tthird",
	}, "\n")

	got, err := Parse(strings.NewReader(fixture), "sample.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("line count = %d, want 3", len(got.Lines))
	}
	if got.Lines[2].Author != "Ada Lovelace" || got.Lines[2].Summary != "initial implementation" {
		t.Fatalf("compacted metadata = %+v", got.Lines[2])
	}
	if got.Lines[1].SHA != shaB || got.Lines[1].AuthorTime.Unix() != 1700100000 {
		t.Fatalf("second line = %+v", got.Lines[1])
	}
}

func TestParseRejectsTruncatedRecord(t *testing.T) {
	_, err := Parse(strings.NewReader("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1 1 1\nauthor Ada\n"), "a.go")
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("error = %v, want truncated record", err)
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{3 * time.Hour, "3h"},
		{12 * 24 * time.Hour, "12d"},
		{120 * 24 * time.Hour, "4mo"},
		{3 * 365 * 24 * time.Hour, "3y"},
	}
	for _, tt := range tests {
		if got := CompactAge(now, now.Add(-tt.age)); got != tt.want {
			t.Errorf("CompactAge(%s) = %q, want %q", tt.age, got, tt.want)
		}
	}
}

func TestLoadUsesPinnedRefAndIgnoresWhitespace(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("config", "user.name", "Test Author")
	git("config", "user.email", "test@example.com")
	path := filepath.Join(repo, "sample.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "sample.txt")
	git("commit", "-q", "-m", "initial subject")
	initial := git("rev-parse", "HEAD")

	if err := os.WriteFile(path, []byte("alpha  \nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "sample.txt")
	git("commit", "-q", "-m", "whitespace only")
	baseline := git("rev-parse", "HEAD")

	got, err := Load(repo, baseline, "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(got.Lines))
	}
	for i, line := range got.Lines {
		if line.SHA != initial || line.Summary != "initial subject" {
			t.Errorf("line %d = %+v, want initial commit", i+1, line)
		}
	}
}
