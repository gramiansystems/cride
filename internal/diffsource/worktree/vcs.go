package worktree

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// emptyTree is git's well-known empty tree object, used as a baseline for a
// repository that has no commits yet.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// git is a thin wrapper around the git CLI rooted at a repository.
type git struct {
	root string
}

func newGit(dir string) (*git, error) {
	root, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository (%s): %w", dir, err)
	}
	return &git{root: strings.TrimSpace(root)}, nil
}

func (g *git) run(args ...string) (string, error) {
	out, _, err := runGitCode(g.root, args...)
	return out, err
}

func (g *git) runCode(args ...string) (string, int, error) {
	return runGitCode(g.root, args...)
}

func runGit(dir string, args ...string) (string, error) {
	out, _, err := runGitCode(dir, args...)
	return out, err
}

// runGitCode runs git and returns stdout, the exit code, and an error. Exit
// code 1 is treated as success because `git diff --no-index` uses it to mean
// "differences found"; codes >1 are real failures.
func runGitCode(dir string, args ...string) (string, int, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	err := cmd.Run()
	if err == nil {
		return outb.String(), 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code == 1 {
			return outb.String(), 1, nil
		}
		return outb.String(), code, fmt.Errorf("git %s: exit %d: %s",
			strings.Join(args, " "), code, strings.TrimSpace(errb.String()))
	}
	return "", -1, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}
