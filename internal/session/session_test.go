package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testState() State {
	return State{
		Baseline:      "abc123",
		SelectedFile:  "internal/app/app.go",
		FullFileView:  true,
		CollapsedDirs: []string{"vendor"},
		SplitFiles:    []string{"internal/app/app.go"},
		ChangeOrder:   "change",
		ChangeClock:   3,
		ChangeOrdinal: map[string]int{"a.go": 3},
		ChangeHashes:  map[string]string{"a.go": "hash"},
		Seen:          map[string]string{"a.go": "deadbeef"},
		Searches:      map[string]string{"a.go": "alpha"},
		Files: map[string]FileState{
			"internal/app/app.go": {CursorLine: 120, ScreenRow: 5, Expansions: map[string]int{"0": 10}},
		},
	}
}

func TestSessionRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	repoID := RepoID("/repo", "abc123")
	if err := Save(repoID, testState()); err != nil {
		t.Fatal(err)
	}
	got, err := Load(repoID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SelectedFile != "internal/app/app.go" || !got.FullFileView {
		t.Fatalf("round trip lost view state: %+v", got)
	}
	fs := got.Files["internal/app/app.go"]
	if fs.CursorLine != 120 || fs.ScreenRow != 5 || fs.Expansions["0"] != 10 {
		t.Fatalf("round trip lost file state: %+v", fs)
	}
	if got.Seen["a.go"] != "deadbeef" || got.Searches["a.go"] != "alpha" {
		t.Fatalf("round trip lost seen/search: %+v", got)
	}
	if got.ChangeOrder != "change" || got.ChangeClock != 3 || got.ChangeOrdinal["a.go"] != 3 || got.ChangeHashes["a.go"] != "hash" {
		t.Fatalf("round trip lost change order: %+v", got)
	}
	if got.SavedAt.IsZero() {
		t.Fatal("SavedAt not stamped")
	}

	// Different baseline → different repo id → fresh session.
	other, err := Load(RepoID("/repo", "other"))
	if err != nil || other.SelectedFile != "" {
		t.Fatalf("different baseline shares session: %+v err=%v", other, err)
	}
}

func TestSessionCorruptAndFutureVersionsStartFresh(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	repoID := RepoID("/repo", "abc")
	path := filepath.Join(Dir(repoID), "session.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	// Missing file: fresh, no error.
	state, err := Load(repoID)
	if err != nil || state.SelectedFile != "" {
		t.Fatalf("missing session: %+v err=%v", state, err)
	}

	// Corrupt file: fresh usable state plus an error to surface.
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Load(repoID)
	if err == nil {
		t.Fatal("corrupt session did not report an error")
	}
	if state.FormatVersion != FormatVersion {
		t.Fatalf("corrupt fallback = %+v", state)
	}

	// Future version: fresh + error, never a crash.
	if err := os.WriteFile(path, []byte(`{"format_version": 99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(repoID); err == nil {
		t.Fatal("future session version did not report an error")
	}

	// Unknown fields are ignored.
	if err := os.WriteFile(path, []byte(`{"format_version":1,"selected_file":"x.go","later_feature":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Load(repoID)
	if err != nil || state.SelectedFile != "x.go" {
		t.Fatalf("unknown fields broke load: %+v err=%v", state, err)
	}
}

func TestSessionSaveLeavesNoTempLitter(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	repoID := RepoID("/repo", "abc")
	if err := Save(repoID, testState()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(Dir(repoID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", entry.Name())
		}
	}
}
