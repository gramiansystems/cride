package app

// This file holds the inline reference-symbol choice: when gr/gd/gi land on a
// row with several candidate identifiers, the candidates are highlighted in
// place on the row and ←/→ move the selection; enter runs the pending lookup.
// No popup is shown — the overlay kind only marks the modal key state.

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mattn/go-runewidth"

	navsearch "cride/internal/search"
	"cride/internal/ui"
)

// openReferenceSymbolChoice enters the inline choice mode for the cursor row.
func (m *Model) openReferenceSymbolChoice(kind referenceRequestKind, changedOnly bool, queries []navsearch.SymbolQuery) {
	m.referencePanel = referencePanelState{}
	m.overlay = overlayState{
		Kind:                    OverlaySymbolChoice,
		SymbolQueries:           queries,
		PendingReferenceKind:    kind,
		PendingReferenceChanged: changedOnly,
	}
}

// handleSymbolChoiceKey drives the inline choice: ←/→ (or h/l) move the
// highlight along the row's candidates, enter runs the pending lookup.
func (m Model) handleSymbolChoiceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopWatching()
		m.saveSessionNow()
		return m, tea.Quit
	case "esc":
		m.closeOverlay()
	case "enter":
		return m, m.acceptReferenceSymbolChoice()
	case "left", "h":
		m.moveSymbolChoiceCursor(-1)
	case "right", "l":
		m.moveSymbolChoiceCursor(1)
	}
	return m, nil
}

// moveSymbolChoiceCursor cycles the highlighted candidate, wrapping at ends.
func (m *Model) moveSymbolChoiceCursor(delta int) {
	n := len(m.overlay.SymbolQueries)
	if n == 0 {
		m.overlay.Cursor = 0
		return
	}
	m.overlay.Cursor = ((m.overlay.Cursor+delta)%n + n) % n
}

func (m *Model) acceptReferenceSymbolChoice() tea.Cmd {
	if m.overlay.Cursor < 0 || m.overlay.Cursor >= len(m.overlay.SymbolQueries) {
		return nil
	}
	query := m.overlay.SymbolQueries[m.overlay.Cursor]
	kind := m.overlay.PendingReferenceKind
	changedOnly := m.overlay.PendingReferenceChanged
	m.closeOverlay()
	return m.openReferencesPanelForQuery(kind, changedOnly, query)
}

// symbolChoiceSpans marks the candidate identifiers on the cursor row while
// the choice is active; the selected one gets the current-match style. Spans
// are re-derived from the live row so a reload that rewrites the line drops
// stale highlights instead of marking unrelated text.
func (m Model) symbolChoiceSpans() []ui.MatchSpan {
	if m.overlay.Kind != OverlaySymbolChoice {
		return nil
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return nil
	}
	row := rows[m.cursor]
	spans := make([]ui.MatchSpan, 0, len(m.overlay.SymbolQueries))
	for i, query := range m.overlay.SymbolQueries {
		baseline := query.Side == navsearch.ResultSideBaseline
		line, ok := rowSideLine(row, baseline)
		if !ok {
			continue
		}
		start, end, ok := symbolDisplaySpan(line.Content, query.Location.Column, query.Symbol)
		if !ok {
			continue
		}
		side := ui.MatchSideUnified
		if row.Kind == ui.RowPair {
			side = ui.MatchSideRight
			if baseline {
				side = ui.MatchSideLeft
			}
		}
		spans = append(spans, ui.MatchSpan{
			RowIdx:  m.cursor,
			Start:   start,
			End:     end,
			Current: i == m.overlay.Cursor,
			Side:    side,
		})
	}
	return spans
}

// symbolDisplaySpan converts a 1-based byte column into display columns over
// the tab-expanded content, matching computeMatches. ok is false when the
// content no longer holds the symbol at that column.
func symbolDisplaySpan(content string, column int, symbol string) (start, end int, ok bool) {
	idx := column - 1
	if symbol == "" || idx < 0 || idx+len(symbol) > len(content) || content[idx:idx+len(symbol)] != symbol {
		return 0, 0, false
	}
	start = tabExpandedWidth(content[:idx])
	// Identifiers are ASCII, so byte length equals display width.
	return start, start + len(symbol), true
}

// tabExpandedWidth measures display columns like computeMatches: tabs count 4.
func tabExpandedWidth(s string) int {
	width := 0
	for _, r := range s {
		if r == '\t' {
			width += 4
			continue
		}
		width += runewidth.RuneWidth(r)
	}
	return width
}

// symbolChoiceHints is the footer strip for the inline choice, e.g.
// "2/3 Beta `←/→`symbol `enter`refs `esc`cancel".
func (m Model) symbolChoiceHints() []string {
	action := "refs"
	switch m.overlay.PendingReferenceKind {
	case referenceRequestDefinition:
		action = "def"
	case referenceRequestImpact:
		action = "impact"
	}
	if m.overlay.PendingReferenceChanged {
		action += "·changed"
	}
	hints := make([]string, 0, 4)
	queries := m.overlay.SymbolQueries
	if m.overlay.Cursor >= 0 && m.overlay.Cursor < len(queries) {
		hints = append(hints, strconv.Itoa(m.overlay.Cursor+1)+"/"+strconv.Itoa(len(queries))+" "+queries[m.overlay.Cursor].Symbol)
	}
	return append(hints, "`←/→`symbol", "`enter`"+action, "`esc`cancel")
}
