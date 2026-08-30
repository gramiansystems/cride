package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
)

type Overlay struct {
	Title         string
	Prompt        string
	Query         string
	Tabs          []string
	ActiveTab     int
	Cursor        int
	Top           int
	Results       []OverlayResult
	LabelWidth    int
	FullHeight    bool
	Loading       bool
	Error         string
	Empty         string
	Match         string
	MatchFold     bool
	QuerySelected bool
}

type OverlayResult struct {
	Label       string
	Preview     string
	Tone        ResultTone
	ChangeField bool
}

func RenderOverlay(base string, overlay Overlay, width, height int) string {
	if width <= 0 || height <= 0 {
		return base
	}

	panelWidth := min(width-2, max(48, width*4/5))
	if panelWidth < 12 {
		panelWidth = max(2, width)
	}
	panelHeight := overlayPanelHeight(overlay, height)

	panel := box(panelWidth, max(1, panelHeight-2), overlayLines(overlay, panelWidth-2, max(1, panelHeight-2)))
	return fitToTerminal(placeOverlay(base, panel, width, height), width, height)
}

func OverlayResultHeight(overlay Overlay, width, height int) int {
	if width <= 0 || height <= 0 {
		return 1
	}
	panelHeight := overlayPanelHeight(overlay, height)
	contentHeight := max(1, panelHeight-2)
	used := 2
	if len(overlay.Tabs) > 0 {
		used++
	}
	if overlayStatus(overlay) != "" {
		used++
	}
	return max(1, contentHeight-used)
}

func overlayLines(overlay Overlay, width, height int) []string {
	if height <= 0 {
		return nil
	}

	lines := []string{fileHeaderStyle.Render(truncate.String(overlay.Title, uint(max(1, width))))}
	if len(overlay.Tabs) > 0 {
		lines = append(lines, overlayTabLine(overlay, width))
	}
	query := overlay.Query
	if overlay.QuerySelected && query != "" {
		query = selectedFileStyle.Render(query)
	}
	lines = append(lines, padRight(overlay.Prompt+" "+query+"▌", width))

	status := overlayStatus(overlay)
	if status != "" {
		lines = append(lines, status)
	}

	available := max(0, height-len(lines))
	start := min(max(overlay.Top, 0), max(0, len(overlay.Results)-available))
	for i := start; i < len(overlay.Results) && i < start+available; i++ {
		line := overlayResultLineWithLabelWidthAndMatch(overlay.Results[i], width, overlay.LabelWidth, overlay.Match, overlay.MatchFold)
		line = renderResultRow(line, width, i == overlay.Cursor, overlay.Results[i].Tone)
		lines = append(lines, line)
	}
	return lines
}

func overlayPanelHeight(overlay Overlay, height int) int {
	panelHeight := min(height-2, max(12, height*4/5))
	if overlay.FullHeight {
		panelHeight = height - 2
	}
	if panelHeight < 3 {
		panelHeight = max(1, height)
	}
	return panelHeight
}

func overlayTabLine(overlay Overlay, width int) string {
	parts := make([]string, 0, len(overlay.Tabs))
	for i, tab := range overlay.Tabs {
		if i == overlay.ActiveTab {
			parts = append(parts, fileHeaderStyle.Render("▸ "+tab))
		} else {
			parts = append(parts, dimStyle.Render("  "+tab))
		}
	}
	return truncate.String(strings.Join(parts, "  "), uint(max(1, width)))
}

func overlayStatus(overlay Overlay) string {
	switch {
	case overlay.Error != "":
		return delStyle.Render(truncate.String("error: "+overlay.Error, 500))
	case overlay.Loading:
		return dimStyle.Render("loading...")
	case len(overlay.Results) == 0 && overlay.Query != "":
		return dimStyle.Render(overlay.Empty)
	case len(overlay.Results) == 0 && (overlay.Prompt == "/" || overlay.Prompt == "g/"):
		return dimStyle.Render("Type to search")
	default:
		return ""
	}
}

func overlayResultLine(result OverlayResult, width int) string {
	return overlayResultLineWithLabelWidth(result, width, 0)
}

func overlayResultLineWithLabelWidth(result OverlayResult, width, labelWidth int) string {
	return overlayResultLineWithLabelWidthAndMatch(result, width, labelWidth, "", false)
}

func overlayResultLineWithLabelWidthAndMatch(result OverlayResult, width, labelWidth int, match string, fold bool) string {
	return resultLineWithMatch(result.Label, result.Preview, width, result.Tone, labelWidth, result.ChangeField, match, fold)
}

// OverlayResultIndexAt maps a screen click to an overlay result index,
// mirroring RenderOverlay/placeOverlay geometry. Returns -1 outside the
// result rows.
func OverlayResultIndexAt(overlay Overlay, width, height, x, y int) int {
	if width <= 0 || height <= 0 {
		return -1
	}
	panelWidth := min(width-2, max(48, width*4/5))
	if panelWidth < 12 {
		panelWidth = max(2, width)
	}
	panelHeight := overlayPanelHeight(overlay, height)
	contentHeight := max(1, panelHeight-2)
	x0 := max(0, (width-panelWidth)/2)
	y0 := max(0, (height-(contentHeight+2))/3)
	if x < x0 || x >= x0+panelWidth {
		return -1
	}
	innerY := y - y0 - 1 // top border
	if innerY < 0 || innerY >= contentHeight {
		return -1
	}
	used := 2 // title + prompt
	if len(overlay.Tabs) > 0 {
		used++
	}
	if overlayStatus(overlay) != "" {
		used++
	}
	if innerY < used {
		return -1
	}
	available := max(0, contentHeight-used)
	start := min(max(overlay.Top, 0), max(0, len(overlay.Results)-available))
	idx := start + innerY - used
	if idx < 0 || idx >= len(overlay.Results) || idx >= start+available {
		return -1
	}
	return idx
}

// OverlayTabIndexAt maps a click on the tab row to a tab index. Returns -1
// when the overlay has no tabs or the click is outside a visible tab label.
func OverlayTabIndexAt(overlay Overlay, width, height, x, y int) int {
	if width <= 0 || height <= 0 || len(overlay.Tabs) == 0 {
		return -1
	}
	panelWidth := min(width-2, max(48, width*4/5))
	if panelWidth < 12 {
		panelWidth = max(2, width)
	}
	panelHeight := overlayPanelHeight(overlay, height)
	contentHeight := max(1, panelHeight-2)
	x0 := max(0, (width-panelWidth)/2)
	y0 := max(0, (height-(contentHeight+2))/3)
	if y != y0+2 { // top border, then title
		return -1
	}
	innerX := x - x0 - 1 // left border
	if innerX < 0 || innerX >= panelWidth-2 {
		return -1
	}
	start := 0
	for i, tab := range overlay.Tabs {
		end := start + len([]rune(tab)) + 2 // active marker or indentation
		if innerX >= start && innerX < end {
			return i
		}
		start = end + 2 // separating spaces
	}
	return -1
}

func placeOverlay(base, panel string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	if len(baseLines) < height {
		for len(baseLines) < height {
			baseLines = append(baseLines, "")
		}
	}
	if len(baseLines) > height {
		baseLines = baseLines[:height]
	}

	panelLines := strings.Split(panel, "\n")
	panelWidth := 0
	for _, line := range panelLines {
		panelWidth = max(panelWidth, lipgloss.Width(line))
	}
	x := max(0, (width-panelWidth)/2)
	y := max(0, (height-len(panelLines))/3)

	out := make([]string, len(baseLines))
	copy(out, baseLines)
	for i, line := range panelLines {
		row := y + i
		if row < 0 || row >= len(out) {
			continue
		}
		suffix := max(0, width-x-lipgloss.Width(line))
		out[row] = strings.Repeat(" ", x) + line + strings.Repeat(" ", suffix)
	}
	return strings.Join(out, "\n")
}
