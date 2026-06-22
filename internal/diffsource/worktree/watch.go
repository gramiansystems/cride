package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const watchDebounce = 200 * time.Millisecond

// Fingerprint returns a snapshot of everything that can move the review
// diff: HEAD plus the porcelain status of tracked and untracked files.
// Immutable comparisons (commit/range mode) return a constant.
func (s *Source) Fingerprint() (string, error) {
	if s.head != "" {
		return "immutable", nil
	}
	head, _, err := s.git.runCode("rev-parse", "HEAD")
	if err != nil {
		head = ""
	}
	status, err := s.git.run("status", "--porcelain", "-z")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(head + "\x00" + status))
	return hex.EncodeToString(sum[:]), nil
}

// Watch registers fsnotify watches on the working tree (from the project
// file list) and on .git metadata (HEAD, index, refs) so commits, amends,
// and branch switches fire too. Events are coalesced with a debounce before
// onChange runs. Immutable sources never fire and return a no-op stop.
func (s *Source) Watch(onChange func()) (func(), error) {
	if s.head != "" {
		return func() {}, nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dirs := s.watchDirs()
	registered := 0
	for _, dir := range dirs {
		if err := watcher.Add(dir); err == nil {
			registered++
		}
	}
	if registered == 0 {
		watcher.Close()
		return nil, err
	}

	done := make(chan struct{})
	var closeOnce sync.Once
	stop := func() {
		closeOnce.Do(func() {
			close(done)
			watcher.Close()
		})
	}

	go func() {
		var timer *time.Timer
		var timerC <-chan time.Time
		for {
			select {
			case <-done:
				if timer != nil {
					timer.Stop()
				}
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !s.relevantWatchEvent(event) {
					continue
				}
				// New directories get watched so fresh files keep firing.
				if event.Op.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = watcher.Add(event.Name)
					}
				}
				if timer == nil {
					timer = time.NewTimer(watchDebounce)
					timerC = timer.C
				} else {
					timer.Reset(watchDebounce)
				}
			case <-timerC:
				timer = nil
				timerC = nil
				onChange()
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return stop, nil
}

// watchDirs derives the directory set to watch: the repo root, every
// directory containing project files, and .git metadata locations.
func (s *Source) watchDirs() []string {
	root := s.git.root
	seen := map[string]bool{root: true}
	dirs := []string{root}
	add := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}

	if files, err := s.ProjectFiles(); err == nil {
		for _, f := range files {
			dir := filepath.Join(root, filepath.Dir(filepath.FromSlash(f)))
			for dir != root && !seen[dir] {
				add(dir)
				dir = filepath.Dir(dir)
			}
		}
	}

	gitDir := filepath.Join(root, ".git")
	add(gitDir)
	add(filepath.Join(gitDir, "refs"))
	add(filepath.Join(gitDir, "refs", "heads"))
	return dirs
}

// relevantWatchEvent filters chaff: chmod-only events and .git churn that
// cannot change the review (lock files, object writes).
func (s *Source) relevantWatchEvent(event fsnotify.Event) bool {
	if event.Op == fsnotify.Chmod {
		return false
	}
	name := filepath.ToSlash(event.Name)
	if idx := strings.Index(name, "/.git/"); idx >= 0 {
		rest := name[idx+len("/.git/"):]
		if strings.HasSuffix(rest, ".lock") {
			return false
		}
		switch {
		case rest == "HEAD", rest == "index":
			return true
		case strings.HasPrefix(rest, "refs/"):
			return true
		default:
			return false
		}
	}
	if strings.HasSuffix(name, "/.git") {
		return true
	}
	return true
}
