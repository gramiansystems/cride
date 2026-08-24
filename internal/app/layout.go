package app

import (
	"cride/internal/ui"
)

func (m Model) mainLayout() ui.MainLayout {
	return ui.LayoutWithPanelSizes(m.width, m.height, m.bottomPanelView(), m.showOutlineBreadcrumb(), m.changeListWidth)
}

// wrapCacheKey captures everything the wrap layout depends on. rowsVersion is
// bumped whenever row content changes (reload, file content load, expansion),
// so equal keys guarantee identical row text.
type wrapCacheKey struct {
	selectedFile int
	mode         ViewMode
	width        int
	rowCount     int
	rowsVersion  int
}

type wrapCacheState struct {
	key    wrapCacheKey
	layout *ui.WrapLayout
}

// currentLayout returns the wrap layout for the current rows, memoized until
// width or row content changes.
func (m *Model) currentLayout() *ui.WrapLayout {
	rows := m.currentRows()
	return m.layoutFor(rows)
}

func (m *Model) layoutFor(rows []ui.Row) *ui.WrapLayout {
	width := m.diffContentWidth()
	key := wrapCacheKey{
		selectedFile: m.selectedFile,
		mode:         m.viewMode,
		width:        width,
		rowCount:     len(rows),
		rowsVersion:  m.rowsVersion,
	}
	if m.wrap != nil && m.wrap.key == key {
		return m.wrap.layout
	}
	layout := ui.BuildWrapLayout(m.files, rows, width)
	m.wrap = &wrapCacheState{key: key, layout: layout}
	return layout
}

func (m *Model) diffContentWidth() int {
	return m.mainLayout().DiffContentWidth
}

// topScreenLine converts the (top row, wrap offset) scroll state into an
// absolute screen line index.
func (m *Model) topScreenLine(l *ui.WrapLayout) int {
	if l.NumRows() == 0 {
		return 0
	}
	top := min(max(m.top, 0), l.NumRows()-1)
	wrapIdx := min(max(m.topWrap, 0), l.RowHeight(top)-1)
	return l.RowStart(top) + wrapIdx
}

// setTopScreenLine stores an absolute screen line index as (row, wrap) scroll
// state, which survives width changes better than a raw line index.
func (m *Model) setTopScreenLine(l *ui.WrapLayout, screenIdx int) {
	if l.NumRows() == 0 {
		m.top, m.topWrap = 0, 0
		return
	}
	sl := l.LineAt(screenIdx)
	m.top, m.topWrap = sl.RowIdx, sl.WrapIdx
}
