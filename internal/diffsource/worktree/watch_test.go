package worktree

import (
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func initWatchRepo(t *testing.T) (string, func(args ...string)) {
	t.Helper()
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
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	gitRun("add", "a.txt")
	gitRun("commit", "-qm", "init")
	return dir, gitRun
}

func TestFingerprintTracksTreeAndCommits(t *testing.T) {
	dir, gitRun := initWatchRepo(t)
	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	base, err := src.Fingerprint()
	if err != nil || base == "" {
		t.Fatalf("fingerprint: %q err=%v", base, err)
	}
	same, _ := src.Fingerprint()
	if same != base {
		t.Fatal("fingerprint unstable without changes")
	}

	// Worktree edit.
	writeFile(t, dir, "a.txt", "one\nTWO\n")
	edited, _ := src.Fingerprint()
	if edited == base {
		t.Fatal("worktree edit did not change fingerprint")
	}

	// Staging changes it again.
	gitRun("add", "a.txt")
	staged, _ := src.Fingerprint()
	if staged == edited {
		t.Fatal("staging did not change fingerprint")
	}

	// Commit moves HEAD.
	gitRun("commit", "-qm", "edit")
	committed, _ := src.Fingerprint()
	if committed == staged {
		t.Fatal("commit did not change fingerprint")
	}

	// Immutable mode is constant.
	immutable, err := OpenCommit(dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	fp1, _ := immutable.Fingerprint()
	writeFile(t, dir, "a.txt", "unrelated\n")
	fp2, _ := immutable.Fingerprint()
	if fp1 != fp2 {
		t.Fatal("immutable fingerprint changed")
	}
}

func TestWatchFiresOnEditAndCommitDebounced(t *testing.T) {
	dir, gitRun := initWatchRepo(t)
	src, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}

	var fired atomic.Int32
	stop, err := src.Watch(func() { fired.Add(1) })
	if err != nil {
		t.Skipf("watch unavailable: %v", err)
	}
	defer stop()

	waitFor := func(want int32, what string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if fired.Load() >= want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s: onChange fired %d times, want >= %d", what, fired.Load(), want)
	}

	// A burst of writes coalesces into one debounced callback.
	writeFile(t, dir, "a.txt", "one\nTWO\n")
	writeFile(t, dir, "a.txt", "one\nTWO!\n")
	writeFile(t, dir, "b.txt", "new\n")
	waitFor(1, "worktree burst")
	time.Sleep(400 * time.Millisecond)
	burst := fired.Load()
	if burst > 2 {
		t.Fatalf("burst produced %d callbacks, want coalesced (<=2)", burst)
	}

	// Commits (via .git metadata) fire too.
	gitRun("add", ".")
	gitRun("commit", "-qm", "edit")
	waitFor(burst+1, "commit")
}
