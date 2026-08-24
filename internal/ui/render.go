package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/muesli/reflow/wrap"

	"cride/internal/diff"
	"cride/internal/highlight"
)

const (
	changeListWidth = 30
	headerHeight    = 1
	footerHeight    = 1
	panelBorderH    = 2
	diffPathHeight  = 1
)

// The palette and every derived style are owned by theme.go: applyTheme
// assigns them all at startup (and whenever SetTheme installs a new palette).
var (
	colorRed        lipgloss.Color
	colorGreen      lipgloss.Color
	colorYellow     lipgloss.Color
	colorBlue       lipgloss.Color
	colorPurple     lipgloss.Color
	colorDim        lipgloss.Color
	colorBgLight    lipgloss.Color
	colorFg         lipgloss.Color
	colorCursor     lipgloss.Color
	colorCharCursor lipgloss.Color
	colorHunkBg     lipgloss.Color
	colorAddBg      lipgloss.Color
	colorDelBg      lipgloss.Color
	colorSearchBg   lipgloss.Color
	colorSearchCur  lipgloss.Color

	searchMatchBgSeq   string
	searchCurrentBgSeq string
	charCursorBgSeq    string

	dimStyle          lipgloss.Style
	fileHeaderStyle   lipgloss.Style
	hunkStyle         lipgloss.Style
	addStyle          lipgloss.Style
	delStyle          lipgloss.Style
	modStyle          lipgloss.Style
	renStyle          lipgloss.Style
	borderStyle       lipgloss.Style
	focusBorderStyle  lipgloss.Style
	headerStyle       lipgloss.Style
	footerStyle       lipgloss.Style
	selectedFileStyle lipgloss.Style
	normalFileStyle   lipgloss.Style
	cursorStyle       lipgloss.Style
	hunkBgStyle       lipgloss.Style
	addedBgStyle      lipgloss.Style
	removedBgStyle    lipgloss.Style
	beforeBadgeStyle  lipgloss.Style
	afterBadgeStyle   lipgloss.Style
	beforeNumStyle    lipgloss.Style
	afterNumStyle     lipgloss.Style
	relativeNumStyle  lipgloss.Style
	diagErrorStyle    lipgloss.Style
	diagWarningStyle  lipgloss.Style
	diagInfoStyle     lipgloss.Style
	unreadBadgeStyle  lipgloss.Style
	commentStyle      lipgloss.Style
)

type BottomPanel struct {
	Open    bool
	Title   string
	Summary string
	// Placement controls whether this result panel is docked below the review
	// or beside it. Size is the outer height for bottom panels and the outer
	// width for right panels; zero selects the responsive default.
	Placement PanelPlacement
	Size      int
	Cursor    int
	Top       int
	Results   []BottomPanelResult
	Loading   bool
	// Spinner is the current spinner frame, shown in the title while loading.
	Spinner string
	Error   string
	Empty   string
}

type PanelPlacement int

const (
	PanelBottom PanelPlacement = iota
	PanelRight
)

type BottomPanelResult struct {
	Label       string
	Preview     string
	Tone        ResultTone
	ChangeField bool
}

type ResultTone int

const (
	ResultToneNone ResultTone = iota
	ResultToneAdded
	ResultToneDeleted
	ResultToneAddedEntire
	ResultToneDeletedEntire
	ResultToneModified
)

type RenderOptions struct {
	LSPStatus string
	// Breadcrumb is the enclosing symbol chain displayed below the file path.
	Breadcrumb string
	// ShowBreadcrumb reserves the sticky breadcrumb row even between symbols.
	ShowBreadcrumb bool
	// TopWrap is the wrap offset within the top row: the first TopWrap screen
	// lines of that row are scrolled off above the viewport.
	TopWrap int
	// Footer overrides the default footer content when non-nil.
	Footer *Footer
	// Matches are in-file search spans to highlight over the diff rows.
	Matches []MatchSpan
	// ChangeList overrides the default (unfocused, fully expanded) list view.
	ChangeList *ChangeListView
	// ChangeListWidth is the requested outer width of the change-list pane.
	// Zero keeps the responsive default.
	ChangeListWidth int
	// Composer renders the comment input in the bottom-panel slot.
	Composer *Composer
}

// Composer is the comment-input view-model, rendered in the bottom-panel
// slot so the commented code stays visible above it.
type Composer struct {
	Title string
	Body  string // pre-rendered multi-line input (textarea view)
	Hints string
}

func composerLines(c Composer, width, height int) []string {
	lines := []string{fileHeaderStyle.Render(truncate.String(c.Title, uint(max(1, width))))}
	for _, line := range strings.Split(strings.TrimRight(c.Body, "\n"), "\n") {
		if len(lines) >= height {
			break
		}
		lines = append(lines, truncate.String(line, uint(max(1, width))))
	}
	if len(lines) < height && c.Hints != "" {
		lines = append(lines, footerStyle.Render(truncate.String(c.Hints, uint(max(1, width)))))
	}
	return lines
}

type MainLayout struct {
	HeaderHeight      int
	BodyY             int
	BodyHeight        int
	ContentY          int
	ContentHeight     int
	LeftOuterWidth    int
	RightOuterWidth   int
	DiffContentX      int
	DiffContentWidth  int
	DiffRowsY         int
	DiffRowsHeight    int
	BottomPanelY      int
	BottomPanelHeight int
	ResultPanelX      int
	ResultPanelY      int
	ResultPanelWidth  int
	ResultPanelHeight int
}

func Layout(width, height int, panel *BottomPanel) MainLayout {
	return LayoutWithBreadcrumb(width, height, panel, false)
}

// LayoutWithBreadcrumb accounts for the optional sticky symbol row.
func LayoutWithBreadcrumb(width, height int, panel *BottomPanel, showBreadcrumb bool) MainLayout {
	return LayoutWithPanelSizes(width, height, panel, showBreadcrumb, 0)
}

// LayoutWithPanelSizes calculates review and result-panel geometry while
// honoring a user-resized change list. The simpler Layout helpers retain the
// original responsive defaults for callers that do not manage pane sizes.
func LayoutWithPanelSizes(width, height int, panel *BottomPanel, showBreadcrumb bool, changeListSize int) MainLayout {
	headerH := headerHeight
	footerH := footerHeight
	panelHeight := BottomPanelHeight(panel, height)
	bodyHeight := height - headerH - footerH - panelHeight
	if bodyHeight < panelBorderH+1 {
		bodyHeight = panelBorderH + 1
	}
	contentHeight := bodyHeight - panelBorderH

	mainWidth := width
	rightPanelWidth := 0
	if panel != nil && panel.Open && panel.Placement == PanelRight {
		rightPanelWidth = RightPanelWidth(panel, width)
		mainWidth = width - rightPanelWidth
	}

	leftOuterWidth := changeListSize
	if leftOuterWidth <= 0 {
		leftOuterWidth = changeListWidth
	}
	if mainWidth < leftOuterWidth+28 {
		leftOuterWidth = max(18, mainWidth/3)
	}
	if leftOuterWidth > mainWidth-12 {
		leftOuterWidth = max(8, mainWidth-12)
	}
	leftOuterWidth = min(max(2, leftOuterWidth), max(2, mainWidth-2))
	rightOuterWidth := mainWidth - leftOuterWidth
	if rightOuterWidth < 8 {
		rightOuterWidth = 8
	}

	diffHeaderHeight := diffPathHeight
	if showBreadcrumb {
		diffHeaderHeight++
	}
	contentY := headerH + 1
	layout := MainLayout{
		HeaderHeight:      headerH,
		BodyY:             headerH,
		BodyHeight:        bodyHeight,
		ContentY:          contentY,
		ContentHeight:     contentHeight,
		LeftOuterWidth:    leftOuterWidth,
		RightOuterWidth:   rightOuterWidth,
		DiffContentX:      leftOuterWidth + 1,
		DiffContentWidth:  max(1, rightOuterWidth-2),
		DiffRowsY:         contentY + diffHeaderHeight,
		DiffRowsHeight:    max(0, contentHeight-diffHeaderHeight),
		BottomPanelY:      headerH + bodyHeight,
		BottomPanelHeight: panelHeight,
	}
	if panel != nil && panel.Open {
		if panel.Placement == PanelRight {
			layout.ResultPanelX = mainWidth
			layout.ResultPanelY = headerH
			layout.ResultPanelWidth = rightPanelWidth
			layout.ResultPanelHeight = bodyHeight
		} else {
			layout.ResultPanelY = layout.BottomPanelY
			layout.ResultPanelWidth = width
			layout.ResultPanelHeight = panelHeight
		}
	}
	return layout
}

// Render produces the full screen for the static review view.
func Render(files []diff.FileDiff, rows []Row, selectedFile, cursor, top, width, height int, hl *highlight.Highlighter, baseline string, fullFile bool) string {
	return RenderWithPanel(files, rows, selectedFile, cursor, top, width, height, hl, baseline, fullFile, nil)
}

// RenderWithPanel produces the full screen with an optional docked result
// panel.
func RenderWithPanel(files []diff.FileDiff, rows []Row, selectedFile, cursor, top, width, height int, hl *highlight.Highlighter, baseline string, fullFile bool, panel *BottomPanel) string {
	return RenderWithOptions(files, rows, selectedFile, cursor, top, width, height, hl, baseline, fullFile, panel, RenderOptions{})
}

// RenderWithOptions produces the full screen with an optional result panel and
// status metadata.
func RenderWithOptions(files []diff.FileDiff, rows []Row, selectedFile, cursor, top, width, height int, hl *highlight.Highlighter, baseline string, fullFile bool, panel *BottomPanel, options RenderOptions) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	header := renderHeader(files, baseline, width)
	footerModel := options.Footer
	if footerModel == nil {
		footerModel = &Footer{
			Stats: FooterStats(files, baseline),
			LSP:   options.LSPStatus,
			Hints: DefaultFooterHints(fullFile),
		}
	}
	footer := RenderFooter(*footerModel, width)
	panelHeight := BottomPanelHeight(panel, height)
	layout := LayoutWithPanelSizes(width, height, panel, options.ShowBreadcrumb, options.ChangeListWidth)
	contentHeight := layout.ContentHeight
	leftOuterWidth := layout.LeftOuterWidth
	rightOuterWidth := layout.RightOuterWidth

	listView := options.ChangeList
	if listView == nil {
		view := BuildChangeListView(files, nil, nil, selectedFile, -1, 0, contentHeight, false)
		listView = &view
	}
	listFocused := listView.Focused
	left := boxWithBorder(leftOuterWidth, contentHeight, changeListLines(*listView, files, leftOuterWidth-2), paneBorderStyle(listFocused))
	center := boxWithBorder(rightOuterWidth, contentHeight, diffPanelLines(files, rows, selectedFile, cursor, top, options.TopWrap, rightOuterWidth-2, contentHeight, hl, fullFile, options.Matches, !isCommitComparison(baseline), options.Breadcrumb, options.ShowBreadcrumb), paneBorderStyle(!listFocused))
	bodyParts := []string{left, center}
	if panel != nil && panel.Open && panel.Placement == PanelRight && layout.ResultPanelWidth > 0 {
		content := max(1, layout.ResultPanelHeight-2)
		bodyParts = append(bodyParts, box(layout.ResultPanelWidth, content, bottomPanelLines(*panel, layout.ResultPanelWidth-2, content)))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, bodyParts...)

	parts := []string{header, body}
	if panelHeight > 0 {
		content := max(1, panelHeight-2)
		if options.Composer != nil {
			parts = append(parts, box(width, content, composerLines(*options.Composer, width-2, content)))
		} else {
			parts = append(parts, box(width, content, bottomPanelLines(*panel, width-2, content)))
		}
	}
	parts = append(parts, footer)
	out := lipgloss.JoinVertical(lipgloss.Left, parts...)
	if lipgloss.Height(out) > height {
		return strings.Join(strings.Split(out, "\n")[:height], "\n")
	}
	return out
}

func BottomPanelHeight(panel *BottomPanel, totalHeight int) int {
	if panel == nil || !panel.Open || panel.Placement == PanelRight || totalHeight <= 0 {
		return 0
	}
	available := max(0, totalHeight-headerHeight-footerHeight)
	if panel.Size > 0 {
		return min(max(3, panel.Size), max(3, available-3))
	}
	height := min(8, max(5, totalHeight/3))
	if height > totalHeight/2 {
		height = max(3, totalHeight/2)
	}
	return height
}

// RightPanelWidth returns the clamped outer width of a right-docked result
// panel. The default deliberately favors a wide-screen layout while leaving
// enough room for the change list and a useful diff pane.
func RightPanelWidth(panel *BottomPanel, totalWidth int) int {
	if panel == nil || !panel.Open || panel.Placement != PanelRight || totalWidth <= 0 {
		return 0
	}
	desired := panel.Size
	if desired <= 0 {
		desired = max(40, totalWidth*2/5)
	}
	maxWidth := max(2, totalWidth-28)
	minWidth := min(24, maxWidth)
	return min(max(minWidth, desired), maxWidth)
}

func BottomPanelResultHeight(panel BottomPanel, width, height int) int {
	panelHeight := BottomPanelHeight(&panel, height)
	if panel.Open && panel.Placement == PanelRight {
		panelHeight = max(3, height-headerHeight-footerHeight)
	}
	if panelHeight == 0 {
		return 1
	}
	contentHeight := max(1, panelHeight-2)
	used := 1
	if bottomPanelStatus(panel) != "" {
		used++
	}
	return max(1, contentHeight-used)
}

// BottomPanelResultIndexAt maps a zero-based line inside the docked panel's
// content box to a result index, mirroring bottomPanelLines. Returns -1 for
// the title/status lines or empty space.
func BottomPanelResultIndexAt(panel BottomPanel, totalHeight, innerY int) int {
	panelHeight := BottomPanelHeight(&panel, totalHeight)
	if panel.Open && panel.Placement == PanelRight {
		panelHeight = max(3, totalHeight-headerHeight-footerHeight)
	}
	if panelHeight == 0 {
		return -1
	}
	contentHeight := max(1, panelHeight-2)
	if innerY < 0 || innerY >= contentHeight {
		return -1
	}
	used := 1
	if bottomPanelStatus(panel) != "" {
		used++
	}
	if innerY < used {
		return -1
	}
	available := max(0, contentHeight-used)
	start := min(max(panel.Top, 0), max(0, len(panel.Results)-available))
	idx := start + innerY - used
	if idx < 0 || idx >= len(panel.Results) || idx >= start+available {
		return -1
	}
	return idx
}

func renderHeader(files []diff.FileDiff, baseline string, width int) string {
	label := " [working tree] uncommitted changes"
	if baseline != "" {
		label = fmt.Sprintf(" [working tree] baseline %s", baseline)
		if isCommitComparison(baseline) {
			label = fmt.Sprintf(" [commits] %s", baseline)
		}
	}
	if len(files) == 0 {
		label += " (clean)"
	}
	return headerStyle.Width(width).Render(truncate.String(label, uint(max(1, width))))
}

func isCommitComparison(label string) bool {
	return strings.Contains(label, "..")
}

func bottomPanelLines(panel BottomPanel, width, height int) []string {
	if height <= 0 {
		return nil
	}

	title := panel.Title
	if panel.Loading && panel.Spinner != "" {
		title = panel.Spinner + " " + title
	}
	if panel.Summary != "" {
		title += dimStyle.Render("  " + panel.Summary)
	}
	lines := []string{fileHeaderStyle.Render(truncate.String(title, uint(max(1, width))))}

	status := bottomPanelStatus(panel)
	if status != "" {
		lines = append(lines, status)
	}

	available := max(0, height-len(lines))
	start := min(max(panel.Top, 0), max(0, len(panel.Results)-available))
	for i := start; i < len(panel.Results) && i < start+available; i++ {
		line := bottomPanelResultLine(panel.Results[i], width)
		line = renderResultRow(line, width, i == panel.Cursor, panel.Results[i].Tone)
		lines = append(lines, line)
	}
	return lines
}

func bottomPanelStatus(panel BottomPanel) string {
	switch {
	case panel.Error != "":
		return delStyle.Render(truncate.String("error: "+panel.Error, 500))
	case panel.Loading:
		return dimStyle.Render("loading...")
	case len(panel.Results) == 0:
		if panel.Empty != "" {
			return dimStyle.Render(panel.Empty)
		}
		return dimStyle.Render("No results")
	default:
		return ""
	}
}

func bottomPanelResultLine(result BottomPanelResult, width int) string {
	return resultLine(result.Label, result.Preview, width, result.Tone, 0, result.ChangeField)
}

func diffPanelLines(files []diff.FileDiff, rows []Row, selectedFile, cursor, top, topWrap, width, height int, hl *highlight.Highlighter, fullFile bool, matches []MatchSpan, live bool, breadcrumb string, showBreadcrumb bool) []string {
	if height <= 0 {
		return nil
	}
	path := " "
	if selectedFile >= 0 && selectedFile < len(files) {
		path = " " + files[selectedFile].Path()
		if fullFile {
			path += " [full]"
		}
	}
	lines := []string{dimStyle.Render(truncate.String(path, uint(max(1, width))))}
	diffHeaderHeight := diffPathHeight
	if showBreadcrumb {
		diffHeaderHeight++
		crumb := " "
		if breadcrumb != "" {
			crumb = "  " + breadcrumb
		}
		lines = append(lines, hunkStyle.Render(truncate.String(crumb, uint(max(1, width)))))
	}
	lines = append(lines, diffLinesWithMatches(files, rows, cursor, top, topWrap, width, max(0, height-diffHeaderHeight), hl, matches, live)...)
	return lines
}

func box(width, contentHeight int, lines []string) string {
	return boxWithBorder(width, contentHeight, lines, borderStyle)
}

// paneBorderStyle returns the accent border for the focused pane.
func paneBorderStyle(focused bool) lipgloss.Style {
	if focused {
		return focusBorderStyle
	}
	return borderStyle
}

func boxWithBorder(width, contentHeight int, lines []string, border lipgloss.Style) string {
	if width < 2 {
		width = 2
	}
	if contentHeight < 1 {
		contentHeight = 1
	}
	inner := width - 2
	var b strings.Builder
	b.WriteString(border.Render("╭" + strings.Repeat("─", inner) + "╮"))
	b.WriteByte('\n')
	for i := 0; i < contentHeight; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(border.Render("│"))
		b.WriteString(padRight(line, inner))
		b.WriteString(border.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(border.Render("╰" + strings.Repeat("─", inner) + "╯"))
	return b.String()
}

func diffLines(files []diff.FileDiff, rows []Row, cursor, top, topWrap, width, height int, hl *highlight.Highlighter) []string {
	return diffLinesWithMatches(files, rows, cursor, top, topWrap, width, height, hl, nil, true)
}

func diffLinesWithMatches(files []diff.FileDiff, rows []Row, cursor, top, topWrap, width, height int, hl *highlight.Highlighter, matches []MatchSpan, live bool) []string {
	if height <= 0 {
		return nil
	}
	if len(rows) == 0 {
		if len(files) == 0 {
			return emptyReviewLines(live, width)
		}
		return []string{dimStyle.Render("No file selected.")}
	}
	matchesByRow := matchSpansByRow(matches)
	return renderDiffRows(files, rows, cursor, top, topWrap, width, height, hl, matchesByRow)
}

// emptyReviewLines fills the diff pane when the review has no changes. For a
// live worktree review this is the reviewer's first screen — often before the
// agent has edited anything — so it doubles as a primer for the review loop.
func emptyReviewLines(live bool, width int) []string {
	if !live {
		return []string{dimStyle.Render(truncate.String("No changes in this comparison.", uint(max(1, width))))}
	}
	lines := []string{
		"No changes against baseline yet.",
		"",
		"cride is watching the working tree; when files change, the",
		"diff appears here marked unread. The review loop:",
		"",
		"  n / N  jump between unread files",
		"  R      mark the current file read and advance",
		"  c      comment on the current line (C for a general comment)",
		"  ^S / e save review.md for the agent to act on",
		"",
		"Changes reload automatically; ^R reloads the diff and review.md.",
		"`?` opens the command palette.",
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = dimStyle.Render(truncate.String(line, uint(max(1, width))))
	}
	return out
}

func renderDiffRows(files []diff.FileDiff, rows []Row, cursor, top, topWrap, width, height int, hl *highlight.Highlighter, matchesByRow map[int][]MatchSpan) []string {
	out := make([]string, 0, height)
	hunkStart, hunkEnd, hasActiveHunk := activeHunkRange(rows, cursor)
	for i := top; i < len(rows) && len(out) < height; i++ {
		selected := i == cursor
		activeHunk := hasActiveHunk && i >= hunkStart && i <= hunkEnd
		isPair := rows[i].Kind == RowPair
		wrapped := rowScreenLines(files, rows[i], hl, i, cursor, width)
		wrapOffset := 0
		if i == top && topWrap > 0 {
			wrapOffset = min(topWrap, len(wrapped)-1)
			wrapped = wrapped[wrapOffset:]
		}
		rowSpans := matchesByRow[i]
		for k, line := range wrapped {
			if len(out) >= height {
				break
			}
			line = padRight(line, width)
			var baseBg lipgloss.Color
			if selected {
				baseBg = colorCursor
				line = withPersistentBackground(line, colorCursor)
				line = cursorStyle.Width(width).MaxWidth(width).Render(line)
			} else if bg, ok := changeBgColor(rows[i]); ok {
				baseBg = bg
				line = withPersistentBackground(line, bg)
				line = changeBgStyle(rows[i]).Width(width).MaxWidth(width).Render(line)
			} else if activeHunk && !(isPair && PairRowHasChange(rows[i])) {
				// Pair rows embed per-column change tints; only tint pure
				// context pairs with the active-hunk background.
				baseBg = colorHunkBg
				line = withPersistentBackground(line, colorHunkBg)
				line = hunkBgStyle.Width(width).MaxWidth(width).Render(line)
			} else if !isPair {
				line = changeBgStyle(rows[i]).Width(width).MaxWidth(width).Render(line)
			}
			if len(rowSpans) > 0 {
				switch rows[i].Kind {
				case RowLine:
					line = applyMatchSpans(line, rowSpans, wrapOffset+k, width, baseBg)
				case RowPair:
					line = applyPairMatchSpans(line, rowSpans, wrapOffset+k, width, baseBg)
				}
			}
			out = append(out, line)
		}
	}
	return out
}

func activeHunkRange(rows []Row, cursor int) (start, end int, ok bool) {
	if cursor < 0 || cursor >= len(rows) || rows[cursor].Kind == RowFileHeader {
		return 0, 0, false
	}

	start = cursor
	for start >= 0 && (rows[start].IsLineRow() || rows[start].Kind == RowComment) {
		start--
	}
	if start < 0 || rows[start].Kind != RowHunkHeader {
		return 0, 0, false
	}

	end = start
	fileIdx := rows[start].FileIdx
	for end+1 < len(rows) && (rows[end+1].IsLineRow() || rows[end+1].Kind == RowComment) && rows[end+1].FileIdx == fileIdx {
		end++
	}
	if end == start {
		return 0, 0, false
	}
	return start, end, true
}

// Side-by-side geometry: relative number, then two columns each with a
// 7-cell gutter (line number, space, sign, space), split by a 1-cell divider.
const (
	pairRelWidth      = 4 // relativeNum(3) + space
	pairColPrefix     = 7 // num(4) + space + sign + space
	pairDividerWidth  = 1
	pairMinCellWidth  = 8
	pairChromeWidth   = pairRelWidth + 2*pairColPrefix + pairDividerWidth
	MinSplitViewWidth = pairChromeWidth + 2*pairMinCellWidth
)

// PairLeftCellEnd returns the first content column past the left cell, for
// click side resolution.
func PairLeftCellEnd(lw int) int {
	return pairRelWidth + pairColPrefix + lw
}

// PairColumnWidths splits a diff content width into left/right cell widths.
// ok is false when the width cannot host a usable split view.
func PairColumnWidths(width int) (lw, rw int, ok bool) {
	usable := width - pairChromeWidth
	if usable < 2*pairMinCellWidth {
		return 0, 0, false
	}
	lw = usable / 2
	rw = usable - lw
	return lw, rw, true
}

// rowScreenLines renders any row into its wrapped screen lines. This is the
// single wrapping path shared by rendering and BuildWrapLayout, so scroll
// math and pixels cannot disagree.
func rowScreenLines(files []diff.FileDiff, r Row, hl *highlight.Highlighter, rowIdx, cursor, width int) []string {
	if r.Kind == RowPair {
		return pairRowLines(files, r, hl, rowIdx, cursor, width)
	}
	return wrapLine(renderRow(files, r, hl, rowIdx, cursor), width)
}

// pairRowLines renders one side-by-side pair row: both cells soft-wrap
// independently and the row's height is the taller side. Column backgrounds
// are embedded here; whole-row (cursor/hunk) backgrounds layer on top in
// diffLines.
func pairRowLines(files []diff.FileDiff, r Row, hl *highlight.Highlighter, rowIdx, cursor, width int) []string {
	lw, rw, ok := PairColumnWidths(width)
	if !ok {
		return wrapLine(renderRow(files, r, hl, rowIdx, cursor), width)
	}
	path := files[r.FileIdx].Path()
	left := pairCellLines(path, r.Left, true, hl, lw)
	right := pairCellLines(path, r.Right, false, hl, rw)
	divider := borderStyle.Render("│")

	n := max(len(left), len(right))
	out := make([]string, 0, n)
	for k := 0; k < n; k++ {
		rel := relativeNumStyle.Render("")
		if k == 0 {
			rel = relativeNum(rowIdx, cursor)
		}
		leftCell := blankPairCell(lw)
		if k < len(left) {
			leftCell = left[k]
		}
		rightCell := blankPairCell(rw)
		if k < len(right) {
			rightCell = right[k]
		}
		out = append(out, rel+" "+leftCell+divider+rightCell)
	}
	return out
}

func blankPairCell(w int) string {
	return strings.Repeat(" ", pairColPrefix+w)
}

// pairCellLines renders one side of a pair row as wrapped, gutter-prefixed,
// background-tinted cell lines of exactly pairColPrefix+w columns each.
func pairCellLines(path string, ln *diff.Line, leftSide bool, hl *highlight.Highlighter, w int) []string {
	if ln == nil {
		return nil
	}
	content := strings.ReplaceAll(ln.Content, "\t", "    ")
	if hl != nil {
		content = hl.Line(path, content)
	}

	sign := " "
	var num string
	var bg lipgloss.Color
	hasBg := false
	switch ln.Kind {
	case diff.LineDelete:
		sign = delStyle.Render("-")
		num = beforeNumStyle.Render(numCol(ln.OldLine, true))
		bg, hasBg = colorDelBg, true
	case diff.LineAdd:
		sign = addStyle.Render("+")
		num = afterNumStyle.Render(numCol(ln.NewLine, true))
		bg, hasBg = colorAddBg, true
	default:
		lineNo := ln.NewLine
		if leftSide {
			lineNo = ln.OldLine
		}
		num = dimStyle.Render(numCol(lineNo, true))
	}

	wrapped := wrapLine(content, w)
	out := make([]string, 0, len(wrapped))
	for k, contentLine := range wrapped {
		prefix := strings.Repeat(" ", pairColPrefix)
		if k == 0 {
			prefix = num + " " + sign + " "
		}
		cell := prefix + padRight(contentLine, w)
		if hasBg {
			cell = withPersistentBackground(cell, bg) + "\x1b[49m"
		}
		out = append(out, cell)
	}
	return out
}

func renderRow(files []diff.FileDiff, r Row, hl *highlight.Highlighter, rowIdx, cursor int) string {
	switch r.Kind {
	case RowFileHeader:
		f := files[r.FileIdx]
		return "    " + statusLetter(f.Status) + " " + fileHeaderStyle.Render(f.Path()) +
			"  " + changeStat(f.Added, f.Deleted)
	case RowHunkHeader:
		return "    " + hunkStyle.Render(r.Text)
	case RowComment:
		style := commentStyle
		if r.Muted {
			style = dimStyle
		}
		return strings.Repeat(" ", diffRowPrefixWidth-4) + style.Render("┃ "+r.Text)
	default:
		f := files[r.FileIdx]
		ln := r.Line
		old := oldLineNumber(ln)
		nw := newLineNumber(r)
		sign, content := signAndContent(f.Path(), r, hl)
		marker := diagnosticMarker(r.DiagnosticMarker)
		if r.DiagnosticMarker == "" && r.CommentID != "" {
			marker = commentStyle.Render("●")
		}
		return relativeNum(rowIdx, cursor) + " " + old + " " + nw + " " + marker + " " + sign + " " + content
	}
}

func diagnosticMarker(marker string) string {
	switch marker {
	case "E":
		return diagErrorStyle.Render("E")
	case "W":
		return diagWarningStyle.Render("W")
	case "I":
		return diagInfoStyle.Render("I")
	case "H":
		return dimStyle.Render("H")
	case "!":
		return diagWarningStyle.Render("!")
	default:
		return " "
	}
}

func relativeNum(rowIdx, cursor int) string {
	if rowIdx == cursor {
		return relativeNumStyle.Render("0")
	}
	dist := rowIdx - cursor
	if dist < 0 {
		dist = -dist
	}
	if dist > 999 {
		dist = 999
	}
	return relativeNumStyle.Render(strconv.Itoa(dist))
}

func changeBgStyle(r Row) lipgloss.Style {
	if color, ok := changeBgColor(r); ok {
		switch color {
		case colorAddBg:
			return addedBgStyle
		case colorDelBg:
			return removedBgStyle
		default:
			return lipgloss.NewStyle().Background(color)
		}
	}
	return lipgloss.NewStyle()
}

func changeBgColor(r Row) (lipgloss.Color, bool) {
	if r.Kind != RowLine {
		return "", false
	}
	if r.Changed {
		return colorAddBg, true
	}
	switch r.Line.Kind {
	case diff.LineAdd:
		return colorAddBg, true
	case diff.LineDelete:
		return colorDelBg, true
	default:
		return "", false
	}
}

func signAndContent(path string, r Row, hl *highlight.Highlighter) (sign, content string) {
	ln := r.Line
	content = strings.ReplaceAll(ln.Content, "\t", "    ")
	if hl != nil {
		content = hl.Line(path, content)
	}
	if r.Changed {
		return addStyle.Render("+"), content
	}
	switch ln.Kind {
	case diff.LineAdd:
		return addStyle.Render("+"), content
	case diff.LineDelete:
		return delStyle.Render("-"), content
	default:
		return " ", content
	}
}

func oldLineNumber(ln diff.Line) string {
	n := numCol(ln.OldLine, ln.Kind != diff.LineAdd)
	if ln.Kind == diff.LineDelete {
		return beforeNumStyle.Render(n)
	}
	return dimStyle.Render(n)
}

func newLineNumber(r Row) string {
	ln := r.Line
	n := numCol(ln.NewLine, ln.Kind != diff.LineDelete)
	if r.Changed || ln.Kind == diff.LineAdd {
		return afterNumStyle.Render(n)
	}
	return dimStyle.Render(n)
}

func withPersistentBackground(s string, color lipgloss.Color) string {
	bg, ok := backgroundSequence(color)
	if !ok {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + len(bg)*4)
	b.WriteString(bg)
	for i := 0; i < len(s); {
		if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
			b.WriteByte(s[i])
			i++
			continue
		}

		end := i + 2
		for end < len(s) && !isANSICommandByte(s[end]) {
			end++
		}
		if end >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}

		end++
		b.WriteString(s[i:end])
		if s[end-1] == 'm' {
			b.WriteString(bg)
		}
		i = end
	}
	return b.String()
}

func backgroundSequence(color lipgloss.Color) (string, bool) {
	hex := strings.TrimPrefix(string(color), "#")
	if len(hex) != 6 {
		return "", false
	}
	r, ok := parseHexByte(hex[0:2])
	if !ok {
		return "", false
	}
	g, ok := parseHexByte(hex[2:4])
	if !ok {
		return "", false
	}
	b, ok := parseHexByte(hex[4:6])
	if !ok {
		return "", false
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b), true
}

func mustBackgroundSequence(color lipgloss.Color) string {
	seq, _ := backgroundSequence(color)
	return seq
}

func parseHexByte(s string) (uint64, bool) {
	n, err := strconv.ParseUint(s, 16, 8)
	if err != nil {
		return 0, false
	}
	return n, true
}

func isANSICommandByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func totalChanges(files []diff.FileDiff) (adds, dels int) {
	for _, f := range files {
		adds += f.Added
		dels += f.Deleted
	}
	return adds, dels
}

func statusLetter(s diff.FileStatus) string {
	switch s {
	case diff.FileAdded:
		return addStyle.Render("A")
	case diff.FileDeleted:
		return delStyle.Render("D")
	case diff.FileRenamed:
		return renStyle.Render("R")
	default:
		return modStyle.Render("M")
	}
}

func changeStat(adds, dels int) string {
	return addStyle.Render(fmt.Sprintf("+%d", adds)) + " " + delStyle.Render(fmt.Sprintf("-%d", dels))
}

func numCol(n int, show bool) string {
	if !show || n == 0 {
		return "    "
	}
	s := strconv.Itoa(n)
	if len(s) >= 4 {
		return s
	}
	return strings.Repeat(" ", 4-len(s)) + s
}

func wrapLine(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	w := wrap.NewWriter(width)
	w.PreserveSpace = true
	_, _ = w.Write([]byte(s))
	wrapped := strings.TrimRight(w.String(), "\n")
	if wrapped == "" {
		return []string{""}
	}
	return strings.Split(wrapped, "\n")
}

// padRight pads s with spaces to w visible columns (ANSI-aware), truncating if
// it is already wider.
func padRight(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis >= w {
		return truncate.String(s, uint(w))
	}
	return s + strings.Repeat(" ", w-vis)
}

func resultLine(label, preview string, width int, tone ResultTone, labelWidth int, changeField bool) string {
	out := styleLeadingResultMarkers(label)
	if labelWidth > 0 {
		out = padRight(out, labelWidth)
	}
	if changeField {
		out = resultToneChangeField(tone) + " " + out
	} else if sign := resultToneSign(tone); sign != "" {
		out = sign + " " + out
	}
	if preview != "" {
		out += dimStyle.Render("  " + strings.TrimSpace(preview))
	}
	return truncate.String(out, uint(max(1, width)))
}

func resultToneChangeField(tone ResultTone) string {
	switch tone {
	case ResultToneAdded:
		return afterBadgeStyle.Render("+") + "  "
	case ResultToneDeleted:
		return beforeBadgeStyle.Render("-") + "  "
	case ResultToneAddedEntire:
		return afterBadgeStyle.Render("+++")
	case ResultToneDeletedEntire:
		return beforeBadgeStyle.Render("---")
	case ResultToneModified:
		return afterBadgeStyle.Render("+") + dimStyle.Render(",") + beforeBadgeStyle.Render("-")
	default:
		return "   "
	}
}

func renderResultRow(line string, width int, selected bool, tone ResultTone) string {
	line = padRight(line, width)
	if selected {
		line = withPersistentBackground(line, colorBgLight)
		return selectedFileStyle.Width(width).MaxWidth(width).Render(line)
	}
	if bg, ok := resultToneBg(tone); ok {
		line = withPersistentBackground(line, bg)
		return resultToneStyle(tone).Width(width).MaxWidth(width).Render(line)
	}
	return normalFileStyle.Width(width).MaxWidth(width).Render(line)
}

func resultToneStyle(tone ResultTone) lipgloss.Style {
	switch tone {
	case ResultToneAdded, ResultToneAddedEntire:
		return addedBgStyle
	case ResultToneDeleted, ResultToneDeletedEntire:
		return removedBgStyle
	default:
		return lipgloss.NewStyle()
	}
}

func resultToneBg(tone ResultTone) (lipgloss.Color, bool) {
	switch tone {
	case ResultToneAdded, ResultToneAddedEntire:
		return colorAddBg, true
	case ResultToneDeleted, ResultToneDeletedEntire:
		return colorDelBg, true
	default:
		return "", false
	}
}

func resultToneSign(tone ResultTone) string {
	switch tone {
	case ResultToneAdded:
		return afterBadgeStyle.Render("+")
	case ResultToneDeleted:
		return beforeBadgeStyle.Render("-")
	case ResultToneAddedEntire:
		return afterBadgeStyle.Render("+++")
	case ResultToneDeletedEntire:
		return beforeBadgeStyle.Render("---")
	case ResultToneModified:
		return afterBadgeStyle.Render("+") + dimStyle.Render(",") + beforeBadgeStyle.Render("-")
	default:
		return ""
	}
}

func styleLeadingResultMarkers(label string) string {
	if !strings.HasPrefix(label, "[") {
		return label
	}
	end := strings.Index(label, "]")
	if end < 0 {
		return label
	}
	markerText := label[1:end]
	if markerText == "" {
		return label
	}
	markers := strings.Split(markerText, ",")
	styled := make([]string, 0, len(markers))
	for _, marker := range markers {
		if resultToneMarker(marker) {
			continue
		}
		styled = append(styled, marker)
	}
	if len(styled) == 0 {
		return strings.TrimLeft(label[end+1:], " ")
	}
	return "[" + strings.Join(styled, dimStyle.Render(",")) + "]" + label[end+1:]
}

func resultToneMarker(marker string) bool {
	switch marker {
	case "before", "after", "before+after", "deleted", "added":
		return true
	default:
		return false
	}
}
