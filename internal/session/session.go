// Package session persists view state across cride restarts, per DESIGN.md
// §9: $XDG_STATE_HOME/cride/<repo-id>/session.json, where repo-id hashes the
// repo path and baseline so different baselines are distinct sessions. State
// stores source coordinates (path + line), never row indexes, so restores
// survive diff drift.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const FormatVersion = 1

// maxSessionAge prunes session dirs untouched for this long.
const maxSessionAge = 30 * 24 * time.Hour

// FileState is one file's remembered view state.
type FileState struct {
	CursorLine int            `json:"cursor_line,omitempty"` // source line, not row index
	CursorCol  int            `json:"cursor_col,omitempty"`  // rune index within the line
	ScreenRow  int            `json:"screen_row,omitempty"`  // cursor's viewport offset
	Expansions map[string]int `json:"expansions,omitempty"`  // hunk index → extra context lines
}

// State is the serialization view-model assembled from the app model.
type State struct {
	FormatVersion int                  `json:"format_version"`
	Baseline      string               `json:"baseline,omitempty"`
	SelectedFile  string               `json:"selected_file,omitempty"`
	FullFileView  bool                 `json:"full_file_view,omitempty"`
	CollapsedDirs []string             `json:"collapsed_dirs,omitempty"`
	SplitFiles    []string             `json:"split_files,omitempty"`
	ChangeOrder   string               `json:"change_order,omitempty"`
	ChangeClock   int                  `json:"change_clock,omitempty"`
	ChangeOrdinal map[string]int       `json:"change_ordinal,omitempty"`
	ChangeHashes  map[string]string    `json:"change_hashes,omitempty"`
	Seen          map[string]string    `json:"seen,omitempty"`
	Searches      map[string]string    `json:"searches,omitempty"`
	Files         map[string]FileState `json:"files,omitempty"`
	SavedAt       time.Time            `json:"saved_at"`
}

// RepoID derives the session identity from repo path + baseline ref.
func RepoID(root, baseline string) string {
	sum := sha256.Sum256([]byte(root + "\x00" + baseline))
	return hex.EncodeToString(sum[:8])
}

// Dir returns the session directory for a repo id, honoring XDG_STATE_HOME.
func Dir(repoID string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "cride", repoID)
}

func statePath(repoID string) string {
	dir := Dir(repoID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "session.json")
}

// Load reads the stored session. A missing file yields a zero state with no
// error; corrupt or future-versioned files return an error alongside a
// usable fresh state — never a fatal condition.
func Load(repoID string) (State, error) {
	path := statePath(repoID)
	if path == "" {
		return State{FormatVersion: FormatVersion}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{FormatVersion: FormatVersion}, nil
	}
	if err != nil {
		return State{FormatVersion: FormatVersion}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{FormatVersion: FormatVersion}, fmt.Errorf("parse session: %w", err)
	}
	if state.FormatVersion > FormatVersion {
		return State{FormatVersion: FormatVersion}, fmt.Errorf("session format %d newer than this cride", state.FormatVersion)
	}
	if state.FormatVersion == 0 {
		state.FormatVersion = FormatVersion
	}
	return state, nil
}

// Save writes the session atomically (temp + rename).
func Save(repoID string, state State) error {
	path := statePath(repoID)
	if path == "" {
		return errors.New("no session directory available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	state.FormatVersion = FormatVersion
	state.SavedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "session.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// CleanOld removes session directories untouched for ~30 days. Best effort.
func CleanOld() {
	base := Dir("")
	if base == "" {
		return
	}
	base = filepath.Dir(base) // .../cride
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxSessionAge)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		os.RemoveAll(filepath.Join(base, entry.Name()))
	}
}
