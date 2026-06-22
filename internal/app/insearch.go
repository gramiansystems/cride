package app

// This file holds in-file incremental search: `/` prompts for a query over
// the currently rendered rows, n/N step matches while a search is active.
// See DESIGN.md's "Rendering and interaction" section.

import (
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mattn/go-runewidth"

	"cride/internal/ui"
)

type matchSpan struct {
	rowIdx   int
	startCol int // display column within the tab-expanded content
	endCol   int
	side     ui.MatchSpanSide
}

type searchViewState struct {
	typing  bool // prompt open, keys edit the query
	active  bool // highlights live, n/N remapped to match navigation
	query   string
	origin  int // cursor row when the search began; incremental edits anchor here
	matches []matchSpan
	current int
}

// searchMemo persists a file's last search across file switches.
type searchMemo struct {
	query string
}

// searchMatchKey tracks what the current match set was computed against.
type searchMatchKey struct {
	path        string
	mode        ViewMode
	rowsVersion int
	query       string
}

// startInFileSearch opens the search prompt anchored at the cursor.
func (m *Model) startInFileSearch() {
	m.countBuf = ""
	m.pendingG = false
	m.pendingZ = false
	m.search = searchViewState{typing: true, active: true, origin: m.cursor}
	m.searchKey = searchMatchKey{}
	m.refreshSearchMatches(false)
}

// clearInFileSearch drops the search: highlights off, n/N back to hunk nav.
func (m *Model) clearInFileSearch() {
	m.search = searchViewState{}
	m.searchKey = searchMatchKey{}
	path := m.currentFilePath()
	if path != "" && m.fileSearches != nil {
		delete(m.fileSearches, path)
	}
}

func (m Model) handleSearchTypingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.stopWatching()
		return m, tea.Quit
	case "esc":
		m.clearInFileSearch()
		return m, nil
	case "enter":
		if m.search.query == "" {
			m.clearInFileSearch()
			return m, nil
		}
		m.search.typing = false
		m.rememberSearchMemo()
		return m, nil
	case "backspace":
		m.search.query = dropLastRune(m.search.query)
		m.refreshSearchMatches(true)
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.search.query += string(msg.Runes)
		m.refreshSearchMatches(true)
		return m, nil
	}
	return m, nil
}

// refreshSearchMatches recomputes matches for the current rows. When jump is
// true the current match snaps to the first match at or after the search
// origin (incremental typing); otherwise the current index is only clamped
// (row regeneration must not move the reviewer).
func (m *Model) refreshSearchMatches(jump bool) {
	if !m.search.active {
		return
	}
	rows := m.currentRows()
	m.search.matches = computeMatches(rows, m.search.query)
	m.searchKey = searchMatchKey{
		path:        m.currentFilePath(),
		mode:        m.viewMode,
		rowsVersion: m.rowsVersion,
		query:       m.search.query,
	}
	if len(m.search.matches) == 0 {
		m.search.current = 0
		return
	}
	if jump {
		m.search.current = 0
		for i, match := range m.search.matches {
			if match.rowIdx >= m.search.origin {
				m.search.current = i
				break
			}
		}
		m.cursor = m.search.matches[m.search.current].rowIdx
		m.clampScroll()
		return
	}
	m.search.current = min(max(m.search.current, 0), len(m.search.matches)-1)
}

// refreshSearchMatchesIfStale recomputes matches when rows or query changed
// underneath them, e.g. after a reload or view-mode toggle.
func (m *Model) refreshSearchMatchesIfStale() {
	if !m.search.active {
		return
	}
	key := searchMatchKey{
		path:        m.currentFilePath(),
		mode:        m.viewMode,
		rowsVersion: m.rowsVersion,
		query:       m.search.query,
	}
	if key == m.searchKey {
		return
	}
	m.refreshSearchMatches(false)
}

// stepSearchMatch moves to the next/previous match, wrapping with a toast.
func (m *Model) stepSearchMatch(dir, count int) tea.Cmd {
	if len(m.search.matches) == 0 {
		return m.notify(ui.ToastWarn, "no matches for "+m.search.query)
	}
	n := len(m.search.matches)
	next := m.search.current + dir*count
	wrapped := next < 0 || next >= n
	next = ((next % n) + n) % n
	m.search.current = next
	m.cursor = m.search.matches[next].rowIdx
	m.search.origin = m.cursor
	m.clampScroll()
	m.centerCursorInViewport()
	if wrapped {
		if dir > 0 {
			return m.notify(ui.ToastInfo, "search wrapped to top")
		}
		return m.notify(ui.ToastInfo, "search wrapped to bottom")
	}
	return nil
}

// rememberSearchMemo persists the accepted query for the current file.
func (m *Model) rememberSearchMemo() {
	path := m.currentFilePath()
	if path == "" || m.search.query == "" {
		return
	}
	if m.fileSearches == nil {
		m.fileSearches = make(map[string]searchMemo)
	}
	m.fileSearches[path] = searchMemo{query: m.search.query}
}

// restoreSearchForCurrentFile reactivates a remembered search when returning
// to a file, without moving the cursor.
func (m *Model) restoreSearchForCurrentFile() {
	memo, ok := m.fileSearches[m.currentFilePath()]
	if !ok || memo.query == "" {
		m.search = searchViewState{}
		m.searchKey = searchMatchKey{}
		return
	}
	m.search = searchViewState{active: true, query: memo.query, origin: m.cursor}
	m.searchKey = searchMatchKey{}
	m.refreshSearchMatches(false)
}

// searchMatchCount formats the "3/17" prompt counter.
func (m Model) searchMatchCount() string {
	if len(m.search.matches) == 0 {
		return "0/0"
	}
	return strconv.Itoa(m.search.current+1) + "/" + strconv.Itoa(len(m.search.matches))
}

// uiMatchSpans converts matches to render spans for the current file.
func (m Model) uiMatchSpans() []ui.MatchSpan {
	if !m.search.active || len(m.search.matches) == 0 {
		return nil
	}
	spans := make([]ui.MatchSpan, 0, len(m.search.matches))
	for i, match := range m.search.matches {
		spans = append(spans, ui.MatchSpan{
			RowIdx:  match.rowIdx,
			Start:   match.startCol,
			End:     match.endCol,
			Current: i == m.search.current,
			Side:    match.side,
		})
	}
	return spans
}

// computeMatches scans row contents for literal, smart-case matches. Columns
// are display columns over the tab-expanded content, matching the renderer.
func computeMatches(rows []ui.Row, query string) []matchSpan {
	if query == "" {
		return nil
	}
	fold := !strings.ContainsFunc(query, unicode.IsUpper)
	queryRunes := []rune(query)
	if fold {
		for i, r := range queryRunes {
			queryRunes[i] = unicode.ToLower(r)
		}
	}
	var matches []matchSpan
	for rowIdx, row := range rows {
		switch row.Kind {
		case ui.RowLine:
			matches = appendContentMatches(matches, rowIdx, row.Line.Content, ui.MatchSideUnified, queryRunes, fold)
		case ui.RowPair:
			if row.Left != nil && row.Left == row.Right {
				// Context pair: one match rendered in both columns.
				matches = appendContentMatches(matches, rowIdx, row.Left.Content, ui.MatchSideBoth, queryRunes, fold)
				continue
			}
			if row.Left != nil {
				matches = appendContentMatches(matches, rowIdx, row.Left.Content, ui.MatchSideLeft, queryRunes, fold)
			}
			if row.Right != nil {
				matches = appendContentMatches(matches, rowIdx, row.Right.Content, ui.MatchSideRight, queryRunes, fold)
			}
		}
	}
	return matches
}

func appendContentMatches(matches []matchSpan, rowIdx int, content string, side ui.MatchSpanSide, queryRunes []rune, fold bool) []matchSpan {
	content = strings.ReplaceAll(content, "\t", "    ")
	runes := []rune(content)
	// cols[i] is the display column where rune i starts.
	cols := make([]int, len(runes)+1)
	for i, r := range runes {
		cols[i+1] = cols[i] + runewidth.RuneWidth(r)
	}
	for i := 0; i+len(queryRunes) <= len(runes); i++ {
		if !runesMatchAt(runes, i, queryRunes, fold) {
			continue
		}
		matches = append(matches, matchSpan{
			rowIdx:   rowIdx,
			startCol: cols[i],
			endCol:   cols[i+len(queryRunes)],
			side:     side,
		})
		i += len(queryRunes) - 1 // non-overlapping
	}
	return matches
}

func runesMatchAt(runes []rune, at int, query []rune, fold bool) bool {
	for j, q := range query {
		r := runes[at+j]
		if fold {
			r = unicode.ToLower(r)
		}
		if r != q {
			return false
		}
	}
	return true
}
