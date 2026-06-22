package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the full UI palette. All rendering styles derive from it in
// applyTheme; no color literals exist outside this file.
type Theme struct {
	Dark bool

	Red    lipgloss.Color
	Green  lipgloss.Color
	Yellow lipgloss.Color
	Blue   lipgloss.Color
	Purple lipgloss.Color
	Dim    lipgloss.Color
	Fg     lipgloss.Color

	HeaderFg lipgloss.Color // text on the purple header bar

	BgLight      lipgloss.Color // selection background
	CursorBg     lipgloss.Color
	CharCursorBg lipgloss.Color // the one-cell character cursor
	HunkBg       lipgloss.Color
	AddBg        lipgloss.Color
	DelBg        lipgloss.Color
	SearchBg     lipgloss.Color
	SearchCurBg  lipgloss.Color
}

// NewTheme returns the built-in palette for a dark or light terminal.
func NewTheme(dark bool) Theme {
	if dark {
		return DarkTheme()
	}
	return LightTheme()
}

// DarkTheme is the original dracula-adjacent palette.
func DarkTheme() Theme {
	return Theme{
		Dark:         true,
		Red:          lipgloss.Color("#ff5555"),
		Green:        lipgloss.Color("#50fa7b"),
		Yellow:       lipgloss.Color("#f1fa8c"),
		Blue:         lipgloss.Color("#8be9fd"),
		Purple:       lipgloss.Color("#bd93f9"),
		Dim:          lipgloss.Color("#6272a4"),
		Fg:           lipgloss.Color("#f8f8f2"),
		HeaderFg:     lipgloss.Color("#000000"),
		BgLight:      lipgloss.Color("#44475a"),
		CursorBg:     lipgloss.Color("#3d3f4b"),
		CharCursorBg: lipgloss.Color("#6f7bce"),
		HunkBg:       lipgloss.Color("#2b3040"),
		AddBg:        lipgloss.Color("#14351f"),
		DelBg:        lipgloss.Color("#3a1818"),
		SearchBg:     lipgloss.Color("#54491d"),
		SearchCurBg:  lipgloss.Color("#8a6d1a"),
	}
}

// LightTheme keeps the same semantic roles readable on a light background:
// add/delete tints, hunk/current-row backgrounds, and diagnostics all stay
// distinguishable from each other.
func LightTheme() Theme {
	return Theme{
		Dark:         false,
		Red:          lipgloss.Color("#b3261e"),
		Green:        lipgloss.Color("#1a7f37"),
		Yellow:       lipgloss.Color("#9a6700"),
		Blue:         lipgloss.Color("#0969da"),
		Purple:       lipgloss.Color("#6639ba"),
		Dim:          lipgloss.Color("#6e7781"),
		Fg:           lipgloss.Color("#1f2328"),
		HeaderFg:     lipgloss.Color("#ffffff"),
		BgLight:      lipgloss.Color("#d0d7de"),
		CursorBg:     lipgloss.Color("#c4ccd4"),
		CharCursorBg: lipgloss.Color("#9ec5fe"),
		HunkBg:       lipgloss.Color("#e4ebf1"),
		AddBg:        lipgloss.Color("#dafbe1"),
		DelBg:        lipgloss.Color("#ffebe9"),
		SearchBg:     lipgloss.Color("#fff8c5"),
		SearchCurBg:  lipgloss.Color("#f2cc60"),
	}
}

var currentTheme Theme

// SetTheme installs the palette and rebuilds every derived style. Call once
// at startup before rendering (styles are package state, not per-frame).
func SetTheme(t Theme) {
	currentTheme = t
	applyTheme(t)
}

// CurrentTheme returns the installed palette.
func CurrentTheme() Theme { return currentTheme }

func applyTheme(t Theme) {
	colorRed = t.Red
	colorGreen = t.Green
	colorYellow = t.Yellow
	colorBlue = t.Blue
	colorPurple = t.Purple
	colorDim = t.Dim
	colorBgLight = t.BgLight
	colorFg = t.Fg
	colorCursor = t.CursorBg
	colorCharCursor = t.CharCursorBg
	colorHunkBg = t.HunkBg
	colorAddBg = t.AddBg
	colorDelBg = t.DelBg
	colorSearchBg = t.SearchBg
	colorSearchCur = t.SearchCurBg

	searchMatchBgSeq = mustBackgroundSequence(colorSearchBg)
	searchCurrentBgSeq = mustBackgroundSequence(colorSearchCur)
	charCursorBgSeq = mustBackgroundSequence(colorCharCursor)

	dimStyle = lipgloss.NewStyle().Foreground(colorDim)
	fileHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	hunkStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPurple)
	addStyle = lipgloss.NewStyle().Foreground(colorGreen)
	delStyle = lipgloss.NewStyle().Foreground(colorRed)
	modStyle = lipgloss.NewStyle().Foreground(colorYellow)
	renStyle = lipgloss.NewStyle().Foreground(colorBlue)
	borderStyle = lipgloss.NewStyle().Foreground(colorDim)
	focusBorderStyle = lipgloss.NewStyle().Foreground(colorBlue)
	headerStyle = lipgloss.NewStyle().Background(colorPurple).Foreground(t.HeaderFg).Bold(true)
	footerStyle = lipgloss.NewStyle().Foreground(colorDim)
	selectedFileStyle = lipgloss.NewStyle().Background(colorBgLight).Foreground(colorBlue).Bold(true)
	normalFileStyle = lipgloss.NewStyle().Foreground(colorFg)
	cursorStyle = lipgloss.NewStyle().Background(colorCursor)
	hunkBgStyle = lipgloss.NewStyle().Background(colorHunkBg)
	addedBgStyle = lipgloss.NewStyle().Background(colorAddBg)
	removedBgStyle = lipgloss.NewStyle().Background(colorDelBg)
	beforeBadgeStyle = lipgloss.NewStyle().Foreground(colorRed).Background(colorDelBg).Bold(true)
	afterBadgeStyle = lipgloss.NewStyle().Foreground(colorGreen).Background(colorAddBg).Bold(true)
	beforeNumStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	afterNumStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	relativeNumStyle = lipgloss.NewStyle().Foreground(colorFg).Background(colorBgLight).Width(3).Align(lipgloss.Right)
	diagErrorStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	diagWarningStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	diagInfoStyle = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	unreadBadgeStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	commentStyle = lipgloss.NewStyle().Foreground(colorPurple)
}

func init() {
	SetTheme(DarkTheme())
}
