package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"cride/internal/diff"
)

// ToastLevel grades transient status messages.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastWarn
	ToastError
)

// FooterToast is a transient status message shown in the footer's hint region.
type FooterToast struct {
	Level ToastLevel
	Text  string
}

// FooterSearch is the in-file search prompt anchored to the footer line.
type FooterSearch struct {
	Query string
	Count string // "3/17"
}

// Footer is the one-line footer view-model: stats on the left, contextual
// hints (or an active toast, or the search prompt) on the right. Keeping it a
// plain struct lets tests assert content without parsing ANSI.
type Footer struct {
	Stats string
	LSP   string
	// Notice is a persistent warning segment (e.g. "tree changed"), unlike
	// the transient Toast.
	Notice string
	Hints  []string // formatted like "`j/k`move"
	Toast  *FooterToast
	Search *FooterSearch // takes priority over Toast and Hints
}

// FooterStats formats the left footer segment for the current review.
func FooterStats(files []diff.FileDiff, baseline string) string {
	adds, dels := totalChanges(files)
	changedFiles := 0
	for _, file := range files {
		if file.Status != diff.FileUnchanged {
			changedFiles++
		}
	}
	comparison := "baseline " + baseline
	if isCommitComparison(baseline) {
		comparison = "commits " + baseline
	}
	return fmt.Sprintf(" %d files · ", changedFiles) + changeStat(adds, dels) + " · " + comparison
}

// DefaultFooterHints is the fallback hint set when the app supplies none.
func DefaultFooterHints(fullFile bool) []string {
	toggle := "`tab`full"
	if fullFile {
		toggle = "`tab`diff"
	}
	return []string{"`j/k`move", toggle, "`gd`def", "`gr`refs", "`^P`open"}
}

// RenderFooter renders the footer as exactly one screen line. Hints truncate
// before stats do.
func RenderFooter(f Footer, width int) string {
	if width < 1 {
		width = 1
	}
	left := f.Stats
	if f.LSP != "" {
		left += " · " + f.LSP
	}
	leftStyled := footerStyle.Render(left)
	if f.Notice != "" {
		leftStyled += diagWarningStyle.Render(" · " + f.Notice)
		left += " · " + f.Notice
	}

	var right string
	var rightStyled string
	switch {
	case f.Search != nil:
		right = "/" + f.Search.Query + "▌"
		if f.Search.Count != "" {
			right += "  " + f.Search.Count
		}
		right += " "
		rightStyled = normalFileStyle.Render(right)
	case f.Toast != nil && f.Toast.Text != "":
		right = f.Toast.Text + " "
		rightStyled = toastStyle(f.Toast.Level).Render(right)
	default:
		hints := strings.Join(f.Hints, " ")
		if hints != "" {
			hints += " "
		}
		right = hints + "`?`commands "
		rightStyled = footerStyle.Render(right)
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	switch {
	case leftW+rightW+1 <= width:
		gap := width - leftW - rightW
		return leftStyled + strings.Repeat(" ", gap) + rightStyled
	case leftW+1 < width:
		// Truncate the hint/toast region, keep the stats intact.
		budget := max(1, width-leftW-1)
		if f.Toast != nil && f.Toast.Text != "" {
			rightStyled = toastStyle(f.Toast.Level).Render(truncate.String(right, uint(budget)))
		} else {
			rightStyled = footerStyle.Render(truncate.String(right, uint(budget)))
		}
		return leftStyled + " " + rightStyled
	default:
		return footerStyle.Render(truncate.String(left, uint(width)))
	}
}

func toastStyle(level ToastLevel) lipgloss.Style {
	switch level {
	case ToastError:
		return diagErrorStyle
	case ToastWarn:
		return diagWarningStyle
	default:
		return normalFileStyle
	}
}
