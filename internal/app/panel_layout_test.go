package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"cride/internal/search"
	"cride/internal/source"
	"cride/internal/ui"
)

func resultPanelTestModel() Model {
	results := make([]search.ReferenceResult, 30)
	for i := range results {
		results[i] = search.ReferenceResult{
			Location: source.Location{Path: "a.go", Line: i + 1, Column: 1},
			Preview:  "reference",
		}
	}
	return Model{
		files:                testFiles(),
		width:                160,
		height:               40,
		resultPanelPlacement: ui.PanelRight,
		referencePanel: referencePanelState{
			Open:    true,
			Results: results,
		},
	}
}

func TestResultPanelDefaultsRightAndDockHotkeyTogglesBottom(t *testing.T) {
	t.Parallel()

	if got := New(nil).resultPanelPlacement; got != ui.PanelRight {
		t.Fatalf("new model result panel placement = %v, want right", got)
	}

	m := resultPanelTestModel()
	rightLayout := m.mainLayout()
	rightPage := m.referencePageSize()
	if rightLayout.ResultPanelX <= 0 {
		t.Fatal("result panel did not open on the right")
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if m.resultPanelPlacement != ui.PanelBottom {
		t.Fatal("ctrl+w did not dock the result panel at the bottom")
	}
	bottomLayout := m.mainLayout()
	if bottomLayout.ResultPanelX != 0 || rightLayout.ResultPanelHeight <= bottomLayout.ResultPanelHeight {
		t.Fatalf("right geometry = x%d h%d; bottom h%d",
			rightLayout.ResultPanelX, rightLayout.ResultPanelHeight, bottomLayout.ResultPanelHeight)
	}
	if bottomPage := m.referencePageSize(); rightPage <= bottomPage {
		t.Fatalf("right panel page = %d, bottom page = %d; want more rows", rightPage, bottomPage)
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = next.(Model)
	if m.resultPanelPlacement != ui.PanelRight {
		t.Fatal("second ctrl+w did not return the result panel to the right")
	}
}

func TestMouseDragResizesRightAndBottomResultPanel(t *testing.T) {
	t.Parallel()

	m := resultPanelTestModel()
	m.resultPanelPlacement = ui.PanelRight
	layout := m.mainLayout()
	originalWidth := layout.ResultPanelWidth

	next, _ := m.handleMouse(tea.MouseMsg{X: layout.ResultPanelX, Y: layout.ResultPanelY + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	next, _ = m.handleMouse(tea.MouseMsg{X: layout.ResultPanelX - 10, Y: layout.ResultPanelY + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(Model)
	if got := m.mainLayout().ResultPanelWidth; got != originalWidth+10 {
		t.Fatalf("dragged right panel width = %d, want %d", got, originalWidth+10)
	}
	next, _ = m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease})
	m = next.(Model)
	if m.resizingPane != resizeNone {
		t.Fatal("mouse release did not finish right-panel resize")
	}

	m.resultPanelPlacement = ui.PanelBottom
	layout = m.mainLayout()
	originalHeight := layout.ResultPanelHeight
	next, _ = m.handleMouse(tea.MouseMsg{X: 10, Y: layout.ResultPanelY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 10, Y: layout.ResultPanelY - 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(Model)
	if got := m.mainLayout().ResultPanelHeight; got != originalHeight+5 {
		t.Fatalf("dragged bottom panel height = %d, want %d", got, originalHeight+5)
	}
}

func TestMouseDragResizesChangeList(t *testing.T) {
	t.Parallel()

	m := Model{files: testFiles(), width: 160, height: 40}
	layout := m.mainLayout()
	next, _ := m.handleMouse(tea.MouseMsg{X: layout.LeftOuterWidth - 1, Y: layout.BodyY + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	next, _ = m.handleMouse(tea.MouseMsg{X: 39, Y: layout.BodyY + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(Model)
	if got := m.mainLayout().LeftOuterWidth; got != 40 {
		t.Fatalf("dragged change-list width = %d, want 40", got)
	}
}
