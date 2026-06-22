package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

func TestToastLifecycle(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), width: 80, height: 20}

	// Info toast expires by generation-matched tick.
	cmd := m.notify(ui.ToastInfo, "marked read")
	if cmd == nil {
		t.Fatal("info toast returned no expiry command")
	}
	if m.status.text != "marked read" || m.status.sticky {
		t.Fatalf("info toast state = %+v", m.status)
	}
	staleGen := m.status.generation

	// A newer toast supersedes; the stale tick must not clear it.
	if cmd := m.notify(ui.ToastInfo, "reloaded — no changes"); cmd == nil {
		t.Fatal("second toast returned no expiry command")
	}
	next, _ := m.Update(toastExpiredMsg{generation: staleGen})
	m = next.(Model)
	if m.status.text != "reloaded — no changes" {
		t.Fatalf("stale tick cleared newer toast: %+v", m.status)
	}
	next, _ = m.Update(toastExpiredMsg{generation: m.status.generation})
	m = next.(Model)
	if m.status.text != "" {
		t.Fatalf("matching tick did not clear toast: %+v", m.status)
	}

	// Error toasts are sticky: ticks don't clear them, the next key does.
	if cmd := m.notify(ui.ToastError, "boom"); cmd != nil {
		t.Fatal("error toast scheduled an expiry")
	}
	next, _ = m.Update(toastExpiredMsg{generation: m.status.generation})
	m = next.(Model)
	if m.status.text != "boom" {
		t.Fatal("tick cleared sticky error toast")
	}
	m = press(m, "j")
	if m.status.text != "" {
		t.Fatalf("keypress did not clear sticky toast: %+v", m.status)
	}
}

func TestFooterRendersOneLineAndTruncatesHintsFirst(t *testing.T) {
	t.Parallel()

	footer := ui.Footer{
		Stats: " 3 files · +10 -2 · baseline abc123",
		Hints: []string{"`j/k`move", "`n/N`hunk", "`]`file", "`tab`full", "`gd`def"},
	}
	for _, width := range []int{40, 60, 80, 120} {
		line := ui.RenderFooter(footer, width)
		if strings.Contains(line, "\n") {
			t.Fatalf("footer at width %d is not one line: %q", width, line)
		}
		plain := stripANSIStr(line)
		if width >= 60 && !strings.Contains(plain, "3 files") {
			t.Fatalf("footer at width %d lost stats: %q", width, plain)
		}
	}

	// At a narrow width the stats survive while hints are dropped.
	plain := stripANSIStr(ui.RenderFooter(footer, 45))
	if !strings.Contains(plain, "3 files") {
		t.Fatalf("stats truncated before hints: %q", plain)
	}
	if strings.Contains(plain, "`gd`def") {
		t.Fatalf("hints not truncated at narrow width: %q", plain)
	}
}

func TestFooterToastReplacesHints(t *testing.T) {
	t.Parallel()

	footer := ui.Footer{
		Stats: " 3 files · +10 -2 · baseline abc123",
		Hints: []string{"`j/k`move"},
		Toast: &ui.FooterToast{Level: ui.ToastInfo, Text: "reloaded — 3 changed"},
	}
	plain := stripANSIStr(ui.RenderFooter(footer, 100))
	if !strings.Contains(plain, "reloaded — 3 changed") {
		t.Fatalf("toast missing from footer: %q", plain)
	}
	if strings.Contains(plain, "`j/k`move") {
		t.Fatalf("toast did not replace hints: %q", plain)
	}
	if !strings.Contains(plain, "3 files") {
		t.Fatalf("toast displaced stats: %q", plain)
	}
}

func TestContextualHintsSwapByMode(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), width: 100, height: 24}
	base := strings.Join(m.contextualHints(), " ")
	if !strings.Contains(base, "`n/N`") {
		t.Fatalf("normal-mode hints = %q, want hunk hint", base)
	}

	m.pendingG = true
	pending := strings.Join(m.contextualHints(), " ")
	if !strings.Contains(pending, "`d`def") || pending == base {
		t.Fatalf("pending-g hints = %q, want completions", pending)
	}
	m.pendingG = false

	m.pendingZ = true
	if got := strings.Join(m.contextualHints(), " "); !strings.Contains(got, "`o`expand") {
		t.Fatalf("pending-z hints = %q", got)
	}
	m.pendingZ = false

	m.enrichmentPanel.Open = true
	if got := strings.Join(m.contextualHints(), " "); !strings.Contains(got, "`enter`jump") {
		t.Fatalf("panel hints = %q", got)
	}
	m.enrichmentPanel.Open = false

	m.overlay.Kind = OverlaySearch
	if got := strings.Join(m.contextualHints(), " "); !strings.Contains(got, "`esc`close") {
		t.Fatalf("overlay hints = %q", got)
	}
}

func TestReloadToastWording(t *testing.T) {
	t.Parallel()

	prev := []diff.FileDiff{testFile("a.go"), testFile("b.go"), testFile("c.go")}
	next := []diff.FileDiff{prev[0], testFileWithLines("b.go", 4), testFile("d.go")}

	added, removed, changed := diffDelta(prev, next)
	if added != 1 || removed != 1 || changed != 1 {
		t.Fatalf("diffDelta = %d/%d/%d, want 1/1/1", added, removed, changed)
	}
	if got := reloadToastText(added, removed, changed); got != "reloaded — 1 changed, 1 added, 1 removed" {
		t.Fatalf("toast text = %q", got)
	}
	if got := reloadToastText(0, 0, 0); got != "reloaded — no changes" {
		t.Fatalf("no-change toast = %q", got)
	}
}

func TestReloadReportsOutcomeToast(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), width: 80, height: 20}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if !m.reloadRequested {
		t.Fatalf("ctrl+r did not mark reload: requested=%v", m.reloadRequested)
	}
	if m.loading {
		t.Fatal("reload must not blank the view with the loading screen")
	}

	upd, _ := m.Update(diffLoadedMsg{seq: m.loadSeq, files: testFiles()})
	m = upd.(Model)
	if m.status.text != "reloaded — no changes" {
		t.Fatalf("reload toast = %q, want no-changes wording", m.status.text)
	}

	// An initial (non-requested) load must not toast.
	m2 := Model{width: 80, height: 20, loading: true}
	upd, _ = m2.Update(diffLoadedMsg{files: testFiles()})
	m2 = upd.(Model)
	if m2.status.text != "" {
		t.Fatalf("initial load unexpectedly toasted: %q", m2.status.text)
	}
}

func stripANSIStr(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEsc {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEsc = false
			}
			continue
		}
		if c == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
