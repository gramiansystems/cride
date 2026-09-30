package app

import (
	"cride/internal/ui"
)

// viewCacheCapacity keeps enough recently visited files hot for normal review
// navigation without retaining row and wrap data for an unbounded project.
const viewCacheCapacity = 8

func (m Model) mainLayout() ui.MainLayout {
	return ui.LayoutWithPanelSizes(m.width, m.height, m.bottomPanelView(), m.showOutlineBreadcrumb(), m.changeListWidth)
}

// wrapCacheKey captures everything the wrap layout depends on. rowsVersion is
// bumped whenever row content changes (reload, file content load, expansion),
// so equal keys guarantee identical row text.
type wrapCacheKey struct {
	selectedFile int
	path         string
	mode         ViewMode
	split        bool
	width        int
	rowCount     int
	rowsVersion  int
}

type wrapCacheState struct {
	entries     map[wrapCacheKey]*ui.WrapLayout
	order       []wrapCacheKey
	rowsVersion int
	versionSet  bool
}

type rowCacheKey struct {
	selectedFile int
	path         string
	mode         ViewMode
	split        bool
	rowsVersion  int
}

type rowCacheState struct {
	renderer          ui.Renderer
	entries           map[rowCacheKey][]ui.Row
	order             []rowCacheKey
	diagnosticEntries map[diagnosticRowCacheKey][]ui.Row
	diagnosticOrder   []diagnosticRowCacheKey
	changeListRows    []ui.ChangeListRow
	changeListVersion int
	changeListValid   bool
	rowsVersion       int
	versionSet        bool
}

type diagnosticRowCacheKey struct {
	rows               rowCacheKey
	diagnosticsVersion int
}

func promoteCacheKey[K comparable](order []K, key K) {
	for i, candidate := range order {
		if candidate != key || i == len(order)-1 {
			continue
		}
		copy(order[i:], order[i+1:])
		order[len(order)-1] = key
		return
	}
}

func (c *rowCacheState) get(key rowCacheKey) ([]ui.Row, bool) {
	if !c.versionSet || c.rowsVersion != key.rowsVersion {
		return nil, false
	}
	rows, ok := c.entries[key]
	if ok {
		promoteCacheKey(c.order, key)
	}
	return rows, ok
}

func (c *rowCacheState) put(key rowCacheKey, rows []ui.Row) {
	if !c.versionSet || c.rowsVersion != key.rowsVersion {
		c.entries = make(map[rowCacheKey][]ui.Row)
		c.order = nil
		c.diagnosticEntries = nil
		c.diagnosticOrder = nil
		c.rowsVersion = key.rowsVersion
		c.versionSet = true
	}
	if _, exists := c.entries[key]; exists {
		c.entries[key] = rows
		promoteCacheKey(c.order, key)
		return
	}
	c.entries[key] = rows
	c.order = append(c.order, key)
	if len(c.order) <= viewCacheCapacity {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}

func (c *rowCacheState) getDiagnostic(key diagnosticRowCacheKey) ([]ui.Row, bool) {
	rows, ok := c.diagnosticEntries[key]
	if ok {
		promoteCacheKey(c.diagnosticOrder, key)
	}
	return rows, ok
}

func (c *rowCacheState) putDiagnostic(key diagnosticRowCacheKey, rows []ui.Row) {
	if c.diagnosticEntries == nil {
		c.diagnosticEntries = make(map[diagnosticRowCacheKey][]ui.Row)
	}
	if _, exists := c.diagnosticEntries[key]; exists {
		c.diagnosticEntries[key] = rows
		promoteCacheKey(c.diagnosticOrder, key)
		return
	}
	c.diagnosticEntries[key] = rows
	c.diagnosticOrder = append(c.diagnosticOrder, key)
	if len(c.diagnosticOrder) <= viewCacheCapacity {
		return
	}
	oldest := c.diagnosticOrder[0]
	c.diagnosticOrder = c.diagnosticOrder[1:]
	delete(c.diagnosticEntries, oldest)
}

func (c *wrapCacheState) get(key wrapCacheKey) (*ui.WrapLayout, bool) {
	if !c.versionSet || c.rowsVersion != key.rowsVersion {
		return nil, false
	}
	layout, ok := c.entries[key]
	if ok {
		promoteCacheKey(c.order, key)
	}
	return layout, ok
}

func (c *wrapCacheState) put(key wrapCacheKey, layout *ui.WrapLayout) {
	if !c.versionSet || c.rowsVersion != key.rowsVersion {
		c.entries = make(map[wrapCacheKey]*ui.WrapLayout)
		c.order = nil
		c.rowsVersion = key.rowsVersion
		c.versionSet = true
	}
	if _, exists := c.entries[key]; exists {
		c.entries[key] = layout
		promoteCacheKey(c.order, key)
		return
	}
	c.entries[key] = layout
	c.order = append(c.order, key)
	if len(c.order) <= viewCacheCapacity {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
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
		path:         m.currentFilePath(),
		mode:         m.viewMode,
		split:        m.splitViewActive(),
		width:        width,
		rowCount:     len(rows),
		rowsVersion:  m.rowsVersion,
	}
	if m.wrap != nil {
		if layout, ok := m.wrap.get(key); ok {
			return layout
		}
	}
	layout := ui.BuildWrapLayout(m.files, rows, width)
	if m.wrap == nil {
		m.wrap = &wrapCacheState{entries: make(map[wrapCacheKey]*ui.WrapLayout)}
	}
	m.wrap.put(key, layout)
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
