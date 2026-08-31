package app

import (
	"errors"
	"strings"
	"testing"

	"cride/internal/diff"
)

func TestReloadReAnchorsCursorBySourceLine(t *testing.T) {
	t.Parallel()

	// Cursor on source line 42 (single-hunk file around that line).
	before := diff.FileDiff{
		OldPath: "a.go", NewPath: "a.go", Status: diff.FileModified,
		Hunks: []diff.Hunk{{
			Header: "@@ -40,5 +40,5 @@", NewStart: 40, NewLines: 5,
			Lines: []diff.Line{
				{Kind: diff.LineContext, Content: "l40", OldLine: 40, NewLine: 40},
				{Kind: diff.LineContext, Content: "l41", OldLine: 41, NewLine: 41},
				{Kind: diff.LineAdd, Content: "l42", NewLine: 42},
				{Kind: diff.LineContext, Content: "l43", OldLine: 43, NewLine: 43},
			},
		}},
	}
	m := Model{files: []diff.FileDiff{before}, width: 90, height: 24}
	m.cursor = 3 // the added l42 row
	if got := cursorSourceLine(m); got != 42 {
		t.Fatalf("setup cursor source line = %d, want 42", got)
	}

	// The agent added a hunk far above: rows shift, source line must not.
	after := diff.FileDiff{
		OldPath: "a.go", NewPath: "a.go", Status: diff.FileModified,
		Hunks: []diff.Hunk{
			{
				Header: "@@ -1,3 +1,4 @@", NewStart: 1, NewLines: 4,
				Lines: []diff.Line{
					{Kind: diff.LineContext, Content: "l1", OldLine: 1, NewLine: 1},
					{Kind: diff.LineAdd, Content: "new-top", NewLine: 2},
					{Kind: diff.LineContext, Content: "l2", OldLine: 2, NewLine: 3},
				},
			},
			before.Hunks[0],
		},
	}
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: []diff.FileDiff{after}})
	m = next.(Model)
	if got := cursorSourceLine(m); got != 42 {
		t.Fatalf("cursor source line after reload = %d, want 42", got)
	}
	if m.currentFilePath() != "a.go" {
		t.Fatalf("selected file after reload = %q", m.currentFilePath())
	}
}

func TestReloadCoalescesWhileLoadIsInFlight(t *testing.T) {
	t.Parallel()

	m := Model{source: fakeSource{}, files: testFiles(), width: 90, height: 24, outlineGeneration: 2, outlineLoaded: true}
	firstCmd := m.reload(false)
	firstSeq := m.loadSeq
	queuedCmd := m.reload(true)
	_ = m.reload(false)
	if firstCmd == nil || queuedCmd != nil {
		t.Fatalf("reload commands: first=%v queued=%v, want command then nil", firstCmd != nil, queuedCmd != nil)
	}
	if m.loadSeq != firstSeq || !m.loadInFlight || !m.reloadPending || !m.reloadPendingManual {
		t.Fatalf("queued reload state: seq=%d inFlight=%v pending=%v manual=%v", m.loadSeq, m.loadInFlight, m.reloadPending, m.reloadPendingManual)
	}

	// Landing the active result starts exactly one coalesced follow-up. A
	// queued manual request stays manual even if later automatic events arrive.
	next, followupCmd := m.Update(diffLoadedMsg{seq: firstSeq, files: []diff.FileDiff{testFile("intermediate.go")}})
	m = next.(Model)
	if followupCmd == nil || m.loadSeq != firstSeq+1 || !m.loadInFlight || m.reloadPending || !m.reloadRequested {
		t.Fatalf("follow-up state: cmd=%v seq=%d inFlight=%v pending=%v manual=%v", followupCmd != nil, m.loadSeq, m.loadInFlight, m.reloadPending, m.reloadRequested)
	}
	if len(m.files) != 1 || m.files[0].Path() != "intermediate.go" {
		t.Fatalf("intermediate reload not applied: %v", m.files)
	}
	if m.outlineGeneration != 3 || m.outlineLoaded || m.outlineLoading {
		t.Fatalf("intermediate outlines not invalidated: generation=%d loaded=%v loading=%v", m.outlineGeneration, m.outlineLoaded, m.outlineLoading)
	}

	// The follow-up completes without scheduling a third load.
	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: []diff.FileDiff{testFile("latest.go")}})
	m = next.(Model)
	if m.loadInFlight || m.reloadPending || m.reloadRequested {
		t.Fatalf("completed reload state: inFlight=%v pending=%v manual=%v", m.loadInFlight, m.reloadPending, m.reloadRequested)
	}
	if len(m.files) != 1 || m.files[0].Path() != "latest.go" {
		t.Fatalf("latest reload result not applied: %v", m.files)
	}
}

func TestReloadRetriesPendingEventAfterTransientError(t *testing.T) {
	t.Parallel()

	m := Model{source: fakeSource{}, files: testFiles(), width: 90, height: 24}
	_ = m.reload(false)
	seq := m.loadSeq
	_ = m.reload(false)

	next, cmd := m.Update(diffLoadedMsg{seq: seq, err: errors.New("transient reload failure")})
	m = next.(Model)
	if cmd == nil || !m.loadInFlight || m.loadSeq != seq+1 || m.err != nil {
		t.Fatalf("retry after error: cmd=%v inFlight=%v seq=%d err=%v", cmd != nil, m.loadInFlight, m.loadSeq, m.err)
	}
}

func TestPollFingerprintSetsTreeChangedIndicator(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), width: 90, height: 24}
	m.loadedFingerprint = "abc"

	next, _ := m.Update(fingerprintMsg{generation: m.loadSeq, value: "abc"})
	m = next.(Model)
	if m.treeChanged {
		t.Fatal("matching fingerprint set treeChanged")
	}

	next, _ = m.Update(fingerprintMsg{generation: m.loadSeq, value: "xyz"})
	m = next.(Model)
	if !m.treeChanged {
		t.Fatal("differing fingerprint did not set treeChanged")
	}
	footer := m.footerView()
	if footer.Notice == "" || !strings.Contains(footer.Notice, "tree changed") {
		t.Fatalf("footer notice = %q, want tree-changed indicator", footer.Notice)
	}

	// Reload clears the indicator and adopts the new fingerprint.
	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: testFiles(), fingerprint: "xyz"})
	m = next.(Model)
	if m.treeChanged || m.loadedFingerprint != "xyz" {
		t.Fatalf("reload did not clear indicator: changed=%v fp=%q", m.treeChanged, m.loadedFingerprint)
	}
}

func TestUnreadDerivedFromSeenSnapshots(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFile("a.go"), testFile("b.go")}, width: 90, height: 24}
	if got := m.unreadCount(); got != 2 {
		t.Fatalf("initial unread = %d, want 2", got)
	}

	// R marks the current file read and advances to the next file.
	m = press(m, "R")
	if got := m.unreadCount(); got != 1 {
		t.Fatalf("unread after R = %d, want 1", got)
	}
	if m.fileUnread(m.files[0]) {
		t.Fatal("marked file still unread after R")
	}
	if m.currentFilePath() != "b.go" {
		t.Fatalf("current file after R = %q, want b.go", m.currentFilePath())
	}

	// An edit to the file's diff makes it unread again.
	edited := m.files
	edited[0] = testFileWithLines("a.go", 3)
	next, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: edited})
	m = next.(Model)
	if !m.fileUnread(m.files[0]) {
		t.Fatal("edited file not unread")
	}

	// Reverting to the seen content makes it read again — derived, not
	// bookkept.
	reverted := []diff.FileDiff{testFile("a.go"), testFile("b.go")}
	next, _ = m.Update(diffLoadedMsg{seq: m.loadSeq, files: reverted})
	m = next.(Model)
	if m.fileUnread(m.files[0]) {
		t.Fatal("reverted file still unread")
	}

	// A marks everything read.
	m = press(m, "A")
	if got := m.unreadCount(); got != 0 {
		t.Fatalf("unread after A = %d, want 0", got)
	}
}

func TestReadKeyMarksReadAndMovesToNextFile(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFile("b.go"),
		testFile("a.go"),
		testFile("dir/c.go"),
	}
	m := Model{files: files, width: 90, height: 24}
	// Rendered order: dir/c.go (dirs first), a.go, b.go.
	m.selectedFile = 2

	m = press(m, "R")
	if m.fileUnread(files[2]) {
		t.Fatal("R did not mark current file read")
	}
	if m.currentFilePath() != "a.go" {
		t.Fatalf("R moved to %q, want a.go", m.currentFilePath())
	}
	if got := m.unreadCount(); got != 2 {
		t.Fatalf("unread after R = %d, want 2", got)
	}
}

func TestUnreadKeyMarksCurrentFileUnread(t *testing.T) {
	t.Parallel()

	m := Model{files: []diff.FileDiff{testFile("a.go"), testFile("b.go")}, width: 90, height: 24}
	m = press(m, "A")
	if got := m.unreadCount(); got != 0 {
		t.Fatalf("unread after A = %d, want 0", got)
	}

	m = press(m, "U")
	if got := m.unreadCount(); got != 1 {
		t.Fatalf("unread after U = %d, want 1", got)
	}
	if !m.fileUnread(m.files[0]) {
		t.Fatal("current file not unread after U")
	}
	if m.fileUnread(m.files[1]) {
		t.Fatal("non-current file became unread after U")
	}
	if m.currentFilePath() != "a.go" {
		t.Fatalf("current file after U = %q, want a.go", m.currentFilePath())
	}
}

func TestUnreadNavigationFollowsChangeListOrder(t *testing.T) {
	t.Parallel()

	files := []diff.FileDiff{
		testFile("b.go"),
		testFile("a.go"),
		testFile("dir/c.go"),
	}
	m := Model{files: files, width: 90, height: 24}
	// Rendered order: dir/c.go (dirs first), a.go, b.go.
	m.selectedFile = 2          // dir/c.go
	_ = m.markCurrentFileRead() // unread: a.go, b.go

	m = press(m, "n")
	if m.currentFilePath() != "a.go" {
		t.Fatalf("n moved to %q, want a.go", m.currentFilePath())
	}
	m = press(m, "n")
	if m.currentFilePath() != "b.go" {
		t.Fatalf("second n moved to %q, want b.go", m.currentFilePath())
	}
	// Wrap back to the first unread with a toast.
	m = press(m, "n")
	if m.currentFilePath() != "a.go" {
		t.Fatalf("wrap n moved to %q, want a.go", m.currentFilePath())
	}
	if !strings.Contains(m.status.text, "wrapped") {
		t.Fatalf("wrap toast = %q", m.status.text)
	}

	m = press(m, "N")
	if m.currentFilePath() != "b.go" {
		t.Fatalf("N moved to %q, want b.go", m.currentFilePath())
	}
}
