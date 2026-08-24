package app

// Review comments v0 (DESIGN.md's "Review annotations"): compose on a diff row, render
// inline under the anchor, and persist to the editable review.md.
// Anchors are plain line ranges; drift marks a comment detached (never
// discarded) until the v1 fingerprint remap lands.

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/annotate"
	"cride/internal/diff"
	"cride/internal/ui"
)

// nowFunc is a test seam for comment timestamps.
var nowFunc = time.Now

type composerState struct {
	open     bool
	input    textarea.Model
	severity annotate.Severity
	anchor   *annotate.Anchor // nil for general comments
	snippet  string
}

type reviewLoadedMsg struct {
	review annotate.Review
	err    error
}

type reviewSavedMsg struct {
	err error
}

type reviewExportedMsg struct {
	path string
	err  error
}

func (m Model) reviewMarkdownPath() string {
	if m.source == nil {
		return ""
	}
	return filepath.Join(m.source.Root(), annotate.ExportName)
}

// loadReviewCmd reads the canonical review.md. A missing file is an empty
// review; parse and I/O errors leave the current in-memory review untouched.
func (m Model) loadReviewCmd() tea.Cmd {
	markdownPath := m.reviewMarkdownPath()
	if markdownPath == "" {
		return nil
	}
	existing := m.reviewSnapshot()
	return func() tea.Msg {
		review, err := annotate.LoadMarkdown(markdownPath, existing)
		if errors.Is(err, fs.ErrNotExist) {
			return reviewLoadedMsg{review: annotate.Review{
				Baseline: existing.Baseline,
				Comments: []annotate.Comment{},
			}}
		}
		return reviewLoadedMsg{review: review, err: err}
	}
}

func (m Model) reviewSnapshot() annotate.Review {
	review := m.review
	review.Comments = append([]annotate.Comment(nil), m.review.Comments...)
	if review.Baseline == "" && m.source != nil {
		review.Baseline = m.source.Baseline()
	}
	return review
}

// saveReviewCmd writes through on every change so a crash loses nothing and
// review.md stays ready for an agent to consume while cride remains open.
func (m Model) saveReviewCmd() tea.Cmd {
	markdownPath := m.reviewMarkdownPath()
	if markdownPath == "" {
		return nil
	}
	review := m.reviewSnapshot()
	return func() tea.Msg {
		return reviewSavedMsg{err: annotate.SaveMarkdown(markdownPath, review)}
	}
}

func (m Model) exportReviewCmd() tea.Cmd {
	path := m.reviewMarkdownPath()
	if path == "" {
		return nil
	}
	review := m.reviewSnapshot()
	return func() tea.Msg {
		err := annotate.SaveMarkdown(path, review)
		return reviewExportedMsg{path: path, err: err}
	}
}

// openComposer starts a comment on the current row (or a general comment).
func (m *Model) openComposer(general bool) tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.pendingZ = false
	m.referencePanel = referencePanelState{}
	m.enrichmentPanel = enrichmentPanelState{}

	var anchor *annotate.Anchor
	snippet := ""
	if !general {
		a, snip, err := m.anchorForCursor()
		if err != nil {
			return m.notify(ui.ToastWarn, err.Error())
		}
		anchor = &a
		snippet = snip
	}

	input := textarea.New()
	input.Placeholder = "comment…"
	input.Prompt = "│ "
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.SetWidth(max(20, m.width-6))
	input.SetHeight(composerInputHeight)
	input.Focus()

	m.composer = composerState{
		open:     true,
		input:    input,
		severity: annotate.SeverityNit,
		anchor:   anchor,
		snippet:  snippet,
	}
	m.clampScroll()
	return textarea.Blink
}

const composerInputHeight = 4

// anchorForCursor derives the comment anchor from the cursor row, honoring
// the active side in split view.
func (m *Model) anchorForCursor() (annotate.Anchor, string, error) {
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) || !rows[m.cursor].IsLineRow() {
		return annotate.Anchor{}, "", errors.New("no commentable line under the cursor")
	}
	row := rows[m.cursor]
	preferBaseline := row.Kind == ui.RowPair && m.splitActiveLeft
	line, ok := rowSideLine(row, preferBaseline)
	if !ok || (preferBaseline && line.OldLine == 0) {
		line, ok = rowSideLine(row, !preferBaseline)
		preferBaseline = !preferBaseline
	}
	if !ok {
		return annotate.Anchor{}, "", errors.New("no commentable line under the cursor")
	}

	side := annotate.SideCurrent
	lineNum := line.NewLine
	if line.NewLine == 0 || preferBaseline && line.OldLine > 0 {
		side = annotate.SideBaseline
		lineNum = line.OldLine
	}
	if lineNum <= 0 {
		return annotate.Anchor{}, "", errors.New("no commentable line under the cursor")
	}
	return annotate.Anchor{
		Path:      m.currentFilePath(),
		Side:      side,
		LineStart: lineNum,
		LineEnd:   lineNum,
	}, line.Content, nil
}

// handleComposerKey owns all keys while composing. esc cancels, ctrl+s or
// ctrl+d saves, ctrl+t cycles severity; everything else edits.
func (m Model) handleComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.composer = composerState{}
		return m, nil
	case "ctrl+s", "ctrl+d":
		return m, m.saveComposedComment()
	case "ctrl+t":
		m.composer.severity = annotate.NextSeverity(m.composer.severity)
		return m, nil
	case "ctrl+c":
		m.stopWatching()
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.composer.input, cmd = m.composer.input.Update(msg)
	return m, cmd
}

func (m *Model) saveComposedComment() tea.Cmd {
	body := strings.TrimRight(m.composer.input.Value(), "\n ")
	if strings.TrimSpace(body) == "" {
		m.composer = composerState{}
		return m.notify(ui.ToastInfo, "empty comment discarded")
	}
	comment := annotate.Comment{
		ID:       annotate.NewID(),
		Body:     body,
		Severity: m.composer.severity,
		Created:  nowFunc(),
		Anchor:   m.composer.anchor,
		Status:   annotate.StatusOpen,
		Snippet:  m.composer.snippet,
	}
	m.review.Comments = append(m.review.Comments, comment)
	m.composer = composerState{}
	m.rowsVersion++
	m.clampScroll()
	return tea.Batch(m.saveReviewCmd(), m.notify(ui.ToastInfo, "comment saved"))
}

// withCommentRows interleaves comment rows under their anchors, the same
// pattern hunk headers use. The anchored row gets a gutter marker.
func (m *Model) withCommentRows(rows []ui.Row) []ui.Row {
	path := m.currentFilePath()
	if path == "" || len(m.review.Comments) == 0 {
		return rows
	}
	type key struct {
		baseline bool
		line     int
	}
	byLine := map[key][]annotate.Comment{}
	for _, c := range m.review.Comments {
		if c.Anchor == nil || c.Anchor.Path != path {
			continue
		}
		k := key{baseline: c.Anchor.Side == annotate.SideBaseline, line: c.Anchor.LineStart}
		byLine[k] = append(byLine[k], c)
	}
	if len(byLine) == 0 {
		return rows
	}

	out := make([]ui.Row, 0, len(rows)+4)
	for _, row := range rows {
		if !row.IsLineRow() {
			out = append(out, row)
			continue
		}
		var comments []annotate.Comment
		if line, ok := rowSideLine(row, false); ok && line.NewLine > 0 {
			comments = append(comments, byLine[key{baseline: false, line: line.NewLine}]...)
		}
		if line, ok := rowSideLine(row, true); ok && line.OldLine > 0 {
			comments = append(comments, byLine[key{baseline: true, line: line.OldLine}]...)
		}
		if len(comments) > 0 {
			row.CommentID = comments[0].ID
		}
		out = append(out, row)
		for _, c := range comments {
			out = append(out, commentRows(row.FileIdx, c)...)
		}
	}
	return out
}

// commentRows renders one comment as inline rows: a header line and the body.
func commentRows(fileIdx int, c annotate.Comment) []ui.Row {
	muted := c.Resolved()
	header := "[" + string(c.Severity) + "]"
	switch c.Status {
	case annotate.StatusResolved:
		header += " ✓ resolved"
	case annotate.StatusUnresolved:
		header += " (detached)"
	}
	rows := []ui.Row{{
		Kind:      ui.RowComment,
		FileIdx:   fileIdx,
		Text:      header,
		CommentID: c.ID,
		Muted:     muted,
	}}
	for _, line := range strings.Split(strings.TrimRight(c.Body, "\n"), "\n") {
		rows = append(rows, ui.Row{
			Kind:      ui.RowComment,
			FileIdx:   fileIdx,
			Text:      line,
			CommentID: c.ID,
			Muted:     muted,
		})
	}
	return rows
}

// commentAtCursor finds the comment the cursor is on (its rows or its anchor).
func (m *Model) commentAtCursor() (int, bool) {
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return 0, false
	}
	id := rows[m.cursor].CommentID
	if id == "" {
		return 0, false
	}
	for i := range m.review.Comments {
		if m.review.Comments[i].ID == id {
			return i, true
		}
	}
	return 0, false
}

// toggleCommentResolved flips resolved on the comment under the cursor (x).
func (m *Model) toggleCommentResolved() tea.Cmd {
	idx, ok := m.commentAtCursor()
	if !ok {
		return m.notify(ui.ToastWarn, "no comment under the cursor")
	}
	c := &m.review.Comments[idx]
	if c.Status == annotate.StatusResolved {
		c.Status = annotate.StatusOpen
	} else {
		c.Status = annotate.StatusResolved
	}
	m.rowsVersion++
	m.clampScroll()
	return m.saveReviewCmd()
}

// stepAnnotation jumps to the next/previous comment across files (]a / [a).
func (m *Model) stepAnnotation(dir int) tea.Cmd {
	type target struct {
		fileIdx  int
		orderPos int
		line     int
		id       string
	}
	order := ui.ChangeListFileOrderWithOptions(m.files, m.changeListOptions())
	orderPos := map[int]int{}
	for pos, fileIdx := range order {
		orderPos[fileIdx] = pos
	}
	pathIdx := map[string]int{}
	for i, f := range m.files {
		pathIdx[f.Path()] = i
	}

	var targets []target
	for _, c := range m.review.Comments {
		if c.Anchor == nil {
			continue
		}
		fileIdx, ok := pathIdx[c.Anchor.Path]
		if !ok {
			continue
		}
		targets = append(targets, target{fileIdx: fileIdx, orderPos: orderPos[fileIdx], line: c.Anchor.LineStart, id: c.ID})
	}
	if len(targets) == 0 {
		return m.notify(ui.ToastInfo, "no comments yet")
	}
	sortTargets := func(a, b target) bool {
		if a.orderPos != b.orderPos {
			return a.orderPos < b.orderPos
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.id < b.id
	}
	for i := 1; i < len(targets); i++ {
		for j := i; j > 0 && sortTargets(targets[j], targets[j-1]); j-- {
			targets[j], targets[j-1] = targets[j-1], targets[j]
		}
	}

	curPos := orderPos[m.selectedFile]
	curLine := cursorRowSourceLine(m)
	pick := -1
	if dir > 0 {
		for i, t := range targets {
			if t.orderPos > curPos || (t.orderPos == curPos && t.line > curLine) {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = 0 // wrap
		}
	} else {
		for i := len(targets) - 1; i >= 0; i-- {
			t := targets[i]
			if t.orderPos < curPos || (t.orderPos == curPos && t.line < curLine) {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = len(targets) - 1 // wrap
		}
	}

	t := targets[pick]
	if t.fileIdx != m.selectedFile {
		m.saveCurrentFileState()
		m.selectedFile = t.fileIdx
		m.restoreCurrentFileState()
		m.rememberPath(m.currentFilePath())
	}
	m.jumpSourceLine(t.line)
	m.clampScroll()
	m.centerCursorInViewport()
	return m.ensureCurrentFileContentCmd()
}

// refreshCommentAnchors re-checks snippet drift after a (re)load: an anchored
// line whose content changed marks the comment detached; drift healing (an
// agent reverting) reopens it. Comments are never dropped.
func (m *Model) refreshCommentAnchors() {
	changed := false
	for i := range m.review.Comments {
		c := &m.review.Comments[i]
		if c.Anchor == nil || c.Snippet == "" || c.Status == annotate.StatusResolved {
			continue
		}
		content, found := m.diffLineContent(c.Anchor)
		switch {
		case !found:
			continue // line not visible in the diff; leave status alone
		case content != c.Snippet && c.Status == annotate.StatusOpen:
			c.Status = annotate.StatusUnresolved
			changed = true
		case content == c.Snippet && c.Status == annotate.StatusUnresolved:
			c.Status = annotate.StatusOpen
			changed = true
		}
	}
	if changed {
		m.rowsVersion++
	}
}

// diffLineContent looks the anchor line up in the loaded diff.
func (m *Model) diffLineContent(anchor *annotate.Anchor) (string, bool) {
	for _, f := range m.files {
		if f.Path() != anchor.Path {
			continue
		}
		for _, h := range f.Hunks {
			for _, ln := range h.Lines {
				if anchor.Side == annotate.SideBaseline {
					if ln.OldLine == anchor.LineStart && ln.Kind != diff.LineAdd {
						return ln.Content, true
					}
				} else if ln.NewLine == anchor.LineStart && ln.Kind != diff.LineDelete {
					return ln.Content, true
				}
			}
		}
	}
	return "", false
}

// composerView builds the ui composer panel.
func (m Model) composerView() *ui.Composer {
	if !m.composer.open {
		return nil
	}
	title := "New comment"
	if m.composer.anchor != nil {
		title = "Comment on " + m.composer.anchor.Path + ":" + strconv.Itoa(m.composer.anchor.LineStart)
		if m.composer.anchor.Side == annotate.SideBaseline {
			title += " (baseline)"
		}
	} else {
		title = "General comment"
	}
	title += " · [" + string(m.composer.severity) + "]"
	return &ui.Composer{
		Title: title,
		Body:  m.composer.input.View(),
		Hints: "`ctrl+s`save  `ctrl+t`severity  `esc`cancel",
	}
}
