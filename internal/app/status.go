package app

// This file holds status feedback: transient toasts, loading spinners, and
// the contextual one-line footer. See DESIGN.md's "Rendering and interaction"
// section.

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/ui"
)

const toastExpiry = 3 * time.Second

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type statusState struct {
	level      ui.ToastLevel
	text       string
	sticky     bool // errors persist until the next keypress
	generation int
}

type toastExpiredMsg struct {
	generation int
}

type spinnerTickMsg struct{}

// notify sets a toast. Info/warn toasts auto-expire; error toasts stay until
// the next keypress. A newer toast supersedes the pending expiry by generation.
func (m *Model) notify(level ui.ToastLevel, text string) tea.Cmd {
	m.statusGeneration++
	m.status = statusState{
		level:      level,
		text:       text,
		sticky:     level == ui.ToastError,
		generation: m.statusGeneration,
	}
	if m.status.sticky {
		return nil
	}
	generation := m.status.generation
	return tea.Tick(toastExpiry, func(time.Time) tea.Msg {
		return toastExpiredMsg{generation: generation}
	})
}

func (m *Model) clearToast() {
	m.status = statusState{}
}

// clearStickyToast drops an error toast on the next keypress.
func (m *Model) clearStickyToast() {
	if m.status.sticky {
		m.clearToast()
	}
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

func (m Model) anythingLoading() bool {
	if m.loading || m.loadInFlight || m.projectFilesLoading || m.overlay.Loading ||
		m.referencePanel.Loading || m.enrichmentPanel.Loading || m.outlineLoading {
		return true
	}
	for _, state := range m.fileContents {
		if state.loading {
			return true
		}
	}
	return false
}

func (m Model) spinnerFrameString() string {
	return spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
}

// footerView assembles the one-line footer: stats + LSP on the left,
// contextual hints or the active toast on the right.
func (m Model) footerView() *ui.Footer {
	baseline := ""
	if m.source != nil {
		baseline = m.source.Baseline()
	}
	stats := ui.FooterStats(m.files, baseline)
	if unread := m.unreadCount(); unread > 0 {
		stats += " · " + strconv.Itoa(unread) + " unread"
	}
	footer := &ui.Footer{
		Stats: stats,
		LSP:   m.semanticStatusLine(),
		Hints: m.contextualHints(),
	}
	if m.treeChanged {
		footer.Notice = "⟳ tree changed — ^R reload"
		if m.mode != modeReview {
			footer.Notice = "⟳ tree changed — reloads when editing ends"
		}
	}
	if m.status.text != "" {
		footer.Toast = &ui.FooterToast{Level: m.status.level, Text: m.status.text}
	}
	if m.search.typing {
		footer.Search = &ui.FooterSearch{Query: m.search.query, Count: m.searchMatchCount()}
	}
	return footer
}

// contextualHints returns 4-6 hints for the current UI mode. The command
// palette remains the complete discoverable list.
func (m Model) contextualHints() []string {
	switch {
	case m.overlay.Kind == OverlayCommandPalette:
		return []string{"`tab/⇧tab`category", "type to filter", "`enter`run", "`j/k`move", "`esc`close"}
	case m.mode == modeInsert:
		return []string{"-- INSERT --", "`arrows`move", "`home/end`line", "`esc`normal"}
	case m.pendingReplace != 0:
		return []string{"-- EDIT --", "type replacement character", "`esc`cancel"}
	case m.pendingOp != 0:
		return []string{"-- EDIT --", "`" + string(m.pendingOp) + string(m.pendingOp) + "`line", "`w/e/b`word", "`$/0/^`line-part"}
	case m.pendingZUpper:
		return []string{"-- EDIT --", "`Z`save+exit", "`Q`discard+exit"}
	case m.mode == modeEdit && m.editDirty:
		return []string{"-- EDIT --", "`i`insert", "`u`undo", "`ZZ`save", "`ZQ`discard"}
	case m.mode == modeEdit:
		return []string{"-- EDIT --", "`i`insert", "`dd`delete", "`o`open-line", "`esc`review"}
	case m.pendingG:
		return []string{"`g`top", "`d`def", "`r`refs", "`s`symbols", "`y`changes", "`e`diag"}
	case m.pendingZ:
		return []string{"`o`expand", "`c`collapse", "`O`all+", "`C`reset", "`s`split", "`f`full"}
	case m.pendingBracket > 0:
		return []string{"`c`next-hunk", "`]`next-file"}
	case m.pendingBracket < 0:
		return []string{"`c`prev-hunk", "`[`prev-file"}
	case m.search.active && !m.search.typing:
		return []string{m.searchMatchCount(), "`n/N`match", "`esc`clear"}
	case m.focus == paneList:
		return []string{"`j/k`move", "`enter`open", "`h/l`fold", "`o`order", "`ctrl+l`diff"}
	case m.overlay.Kind == OverlaySymbolChoice:
		return m.symbolChoiceHints()
	case m.overlay.Kind == OverlaySearch:
		return []string{"type query", "`tab/⇧tab`select", "`enter`open", "`^U`clear", "`esc`close"}
	case m.overlay.Kind != OverlayNone:
		return []string{"type to filter", "`enter`accept", "`^O`order", "`esc`close"}
	case m.enrichmentPanel.Open && m.enrichmentPanel.Kind == enrichmentPanelOutlineDiff:
		return []string{"`J/K`select", "`enter`jump", "`^W`dock", "`s`file/review", "`esc`close"}
	case m.enrichmentPanel.Open || m.referencePanel.Open:
		return []string{"`J/K`select", "`enter`jump", "`^W`dock", "`o`order", "`esc`close"}
	case m.viewMode == ViewFile:
		hints := []string{"`j/k`move"}
		if m.selectedFile >= 0 && m.selectedFile < len(m.files) && m.files[m.selectedFile].Status != diff.FileUnchanged {
			hints = append(hints, "`tab`diff")
		}
		hints = append(hints, "`gd`def", "`gr`refs", "`^P`open")
		if len(m.jumplist) > 0 {
			hints = append(hints, "`^O`back")
		}
		return hints
	default:
		return []string{"`j/k`move", "`n/N`unread", "`]c`hunk", "`}`file", "`tab`full", "`^S`save review"}
	}
}

// diffDelta compares two loaded diffs by path for the reload toast.
func diffDelta(prev, next []diff.FileDiff) (added, removed, changed int) {
	prevByPath := make(map[string]diff.FileDiff, len(prev))
	for _, f := range prev {
		if f.Status == diff.FileUnchanged {
			continue
		}
		prevByPath[f.Path()] = f
	}
	nextPaths := make(map[string]bool, len(next))
	for _, f := range next {
		if f.Status == diff.FileUnchanged {
			continue
		}
		path := f.Path()
		nextPaths[path] = true
		before, ok := prevByPath[path]
		if !ok {
			added++
			continue
		}
		if !reflect.DeepEqual(before.Hunks, f.Hunks) || before.Status != f.Status {
			changed++
		}
	}
	for path := range prevByPath {
		if !nextPaths[path] {
			removed++
		}
	}
	return added, removed, changed
}

func reloadToastText(added, removed, changed int) string {
	if added+removed+changed == 0 {
		return "reloaded — no changes"
	}
	var parts []string
	if changed > 0 {
		parts = append(parts, strconv.Itoa(changed)+" changed")
	}
	if added > 0 {
		parts = append(parts, strconv.Itoa(added)+" added")
	}
	if removed > 0 {
		parts = append(parts, strconv.Itoa(removed)+" removed")
	}
	return "reloaded — " + strings.Join(parts, ", ")
}
