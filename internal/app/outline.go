package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/diff"
	"cride/internal/diffsource"
	"cride/internal/lsp"
	"cride/internal/outline"
	navsearch "cride/internal/search"
	"cride/internal/ui"
)

type outlineDiffLoadedMsg struct {
	generation int
	changes    []outline.SymbolChange
	current    map[string][]lsp.DocumentSymbol
	baseline   map[string][]lsp.DocumentSymbol
	statuses   []lsp.Status
}

// loadOutlinesCmd refreshes all changed-file outlines. It is called after a
// diff generation lands so breadcrumbs are ready without first opening the
// symbol panel.
func (m *Model) loadOutlinesCmd() tea.Cmd {
	m.invalidateOutlines()
	if m.source == nil || len(m.files) == 0 {
		m.outlineLoaded = true
		return nil
	}
	m.outlineLoading = true
	return outlineDiffCmd(m.source, m.lsp, m.outlineExtractor, m.outlineGeneration, append([]diff.FileDiff(nil), m.files...))
}

// invalidateOutlines makes any older asynchronous result stale and clears the
// projections derived from the previous diff. Coalesced reloads use this
// without immediately starting enrichment for an intermediate tree.
func (m *Model) invalidateOutlines() {
	m.outlineGeneration++
	m.outlineLoading = false
	m.outlineLoaded = false
	m.outlineChanges = nil
	m.outlineCurrent = nil
	m.outlineBaseline = nil
}

func outlineDiffCmd(src diffsource.Source, client lsp.Client, extractor outline.Extractor, generation int, files []diff.FileDiff) tea.Cmd {
	if extractor == nil {
		extractor = outline.LexicalExtractor{}
	}
	if client == nil {
		client = lsp.NewUnavailableClient(lsp.Config{})
	}
	return func() tea.Msg {
		msg := outlineDiffLoadedMsg{
			generation: generation,
			current:    make(map[string][]lsp.DocumentSymbol),
			baseline:   make(map[string][]lsp.DocumentSymbol),
		}
		for _, file := range files {
			if file.Binary || file.Status == diff.FileUnchanged {
				continue
			}
			oldPath, newPath := reviewSidePaths(file)
			var beforeContent, afterContent []byte
			if oldPath != "" {
				beforeContent, _ = src.BaselineContent(oldPath)
			}
			if newPath != "" {
				afterContent, _ = src.CurrentContent(newPath)
			}

			var before, after []lsp.DocumentSymbol
			if len(beforeContent) > 0 {
				before, _ = extractor.Symbols(oldPath, beforeContent)
				setSymbolPath(before, oldPath)
				msg.baseline[oldPath] = before
			}
			if len(afterContent) > 0 {
				var status lsp.Status
				var err error
				after, status, err = client.DocumentSymbols(newPath)
				if status.Enabled() {
					msg.statuses = append(msg.statuses, status)
				}
				if err != nil || len(after) == 0 {
					after, _ = extractor.Symbols(newPath, afterContent)
				}
				setSymbolPath(after, newPath)
				msg.current[newPath] = after
			}
			msg.changes = append(msg.changes, outline.DiffOutlines(before, after, beforeContent, afterContent, oldPath, newPath, []diff.FileDiff{file})...)
		}
		return msg
	}
}

func reviewSidePaths(file diff.FileDiff) (oldPath, newPath string) {
	if file.OldPath != "" && file.OldPath != "/dev/null" {
		oldPath = file.OldPath
	}
	if file.NewPath != "" && file.NewPath != "/dev/null" {
		newPath = file.NewPath
	}
	return oldPath, newPath
}

func setSymbolPath(symbols []lsp.DocumentSymbol, path string) {
	for i := range symbols {
		symbols[i].Range.Start.Path = path
		symbols[i].Range.End.Path = path
		symbols[i].SelectionRange.Start.Path = path
		symbols[i].SelectionRange.End.Path = path
		setSymbolPath(symbols[i].Children, path)
	}
}

func (m *Model) openOutlinePanel() tea.Cmd {
	m.countBuf = ""
	m.pendingG = false
	m.referencePanel = referencePanelState{}
	m.outlineWholeReview = false
	m.enrichmentPanel = enrichmentPanelState{
		Open:       true,
		Kind:       enrichmentPanelOutlineDiff,
		Title:      "Symbol changes",
		Loading:    m.outlineLoading,
		Generation: m.outlineGeneration,
		Order:      diff.ResultOrderReview,
	}
	if m.outlineLoaded {
		m.refreshOutlinePanel()
		m.clampScroll()
		return nil
	}
	if !m.outlineLoading {
		cmd := m.loadOutlinesCmd()
		m.enrichmentPanel.Generation = m.outlineGeneration
		m.enrichmentPanel.Loading = m.outlineLoading
		m.clampScroll()
		return cmd
	}
	m.clampScroll()
	return nil
}

func (m *Model) refreshOutlinePanel() {
	if !m.enrichmentPanel.Open || m.enrichmentPanel.Kind != enrichmentPanelOutlineDiff {
		return
	}
	path := m.currentFilePath()
	title := "Symbol changes"
	if m.outlineWholeReview {
		title += ": review"
	} else if path != "" {
		title += ": " + path
	}
	raw := make([]enrichmentResult, 0, len(m.outlineChanges))
	for _, change := range m.outlineChanges {
		if change.Type == outline.SymbolUnchanged || (!m.outlineWholeReview && change.Path != path) {
			continue
		}
		raw = append(raw, outlineEnrichmentResult(change))
	}
	m.enrichmentPanel.Title = title
	m.enrichmentPanel.Loading = m.outlineLoading
	m.enrichmentPanel.Err = nil
	m.enrichmentPanel.RawResults = raw
	m.enrichmentPanel.Results = m.rankEnrichmentResults(raw)
	m.clampEnrichmentCursor()
}

func outlineEnrichmentResult(change outline.SymbolChange) enrichmentResult {
	var symbol lsp.DocumentSymbol
	result := enrichmentResult{Review: diff.ReviewMarkers{
		ContainsAddition: change.ContainsAddition,
		ContainsDeletion: change.ContainsDeletion,
		EntireAddition:   change.Type == outline.SymbolAdded && change.ContainsAddition,
		EntireDeletion:   change.Type == outline.SymbolRemoved && change.ContainsDeletion,
	}}
	if change.After != nil {
		symbol = *change.After
		result.Location = symbol.SelectionRange.Start
		if result.Location.Line < 1 {
			result.Location = symbol.Range.Start
		}
		result.Side = navsearch.ResultSideCurrent
	} else if change.Before != nil {
		symbol = *change.Before
		result.Location = symbol.SelectionRange.Start
		if result.Location.Line < 1 {
			result.Location = symbol.Range.Start
		}
		result.Side = navsearch.ResultSideBaseline
	}
	name := symbol.Name
	if change.Type == outline.SymbolRenamed && change.Before != nil && change.After != nil {
		name = change.Before.Name + " → " + change.After.Name
	}
	line := max(1, result.Location.Line)
	result.Label = "[" + symbol.Kind.String() + "] " + name + "  " + change.Path + ":" + strconv.Itoa(line)
	if symbol.Detail != "" {
		result.Preview = symbol.Detail
	}
	return result
}

func (m Model) showOutlineBreadcrumb() bool {
	if !m.outlineLoaded || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return false
	}
	oldPath, newPath := reviewSidePaths(m.files[m.selectedFile])
	return len(m.outlineCurrent[newPath]) > 0 || len(m.outlineBaseline[oldPath]) > 0
}

func (m Model) outlineBreadcrumb() string {
	if !m.showOutlineBreadcrumb() || m.selectedFile < 0 || m.selectedFile >= len(m.files) {
		return ""
	}
	rows := m.currentRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return ""
	}
	file := m.files[m.selectedFile]
	oldPath, newPath := reviewSidePaths(file)
	row := rows[m.cursor]
	line, symbols := 0, []lsp.DocumentSymbol(nil)
	if row.Kind == ui.RowPair {
		if row.Right != nil && row.Right.NewLine > 0 {
			line, symbols = row.Right.NewLine, m.outlineCurrent[newPath]
		} else if row.Left != nil {
			line, symbols = row.Left.OldLine, m.outlineBaseline[oldPath]
		}
	} else if row.IsLineRow() {
		if row.Line.NewLine > 0 {
			line, symbols = row.Line.NewLine, m.outlineCurrent[newPath]
		} else {
			line, symbols = row.Line.OldLine, m.outlineBaseline[oldPath]
		}
	}
	path := outline.EnclosingPath(symbols, line)
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, 0, len(path))
	for i := len(path) - 1; i >= 0; i-- {
		label := path[i].Kind.String() + " " + path[i].Name
		if path[i].Detail != "" && path[i].Kind == lsp.SymbolMethod {
			label = "method (" + path[i].Detail + ") " + path[i].Name
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " › ")
}
