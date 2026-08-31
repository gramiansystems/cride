// Package ui renders the review diff. It flattens files/hunks/lines into
// []Row values so scrolling and cursor math operate on row indices. See
// DESIGN.md's "Rendering and interaction" section.
package ui

import "cride/internal/diff"

// RowKind distinguishes the kinds of rows in the flattened diff.
type RowKind int

const (
	RowFileHeader RowKind = iota
	RowHunkHeader
	RowLine
	RowPair    // side-by-side aligned pair; see pair.go
	RowComment // inline review-comment row under its anchor
)

// Row is one rendered row in the flattened diff view.
type Row struct {
	Kind             RowKind
	FileIdx          int       // index into the files slice
	HunkIdx          int       // 1-based hunk index, or 0 when not tied to one
	Line             diff.Line // valid when Kind == RowLine; primary line for RowPair
	Text             string    // header/placeholder text for non-line rows
	Changed          bool      // true for changed current lines in full-file view
	DiagnosticMarker string
	// Left/Right hold the two sides of a RowPair; nil is a blank cell.
	Left  *diff.Line
	Right *diff.Line
	// CommentID ties RowComment rows (and anchored markers) to their comment.
	CommentID string
	// Muted dims the row (e.g. resolved comments).
	Muted bool
}

// IsLineRow reports whether the row carries source content (unified line or
// side-by-side pair).
func (r Row) IsLineRow() bool {
	return r.Kind == RowLine || r.Kind == RowPair
}

// Flatten turns files into a single ordered slice of rows.
func Flatten(files []diff.FileDiff) []Row {
	var rows []Row
	for fi, f := range files {
		rows = append(rows, Row{Kind: RowFileHeader, FileIdx: fi})
		rows = append(rows, flattenFileRows(f, fi)...)
	}
	return rows
}

// FlattenFile returns the rows for one file, excluding the file header because
// the file view already renders the selected path as its panel title.
func FlattenFile(files []diff.FileDiff, fileIdx int) []Row {
	if fileIdx < 0 || fileIdx >= len(files) {
		return nil
	}
	return flattenFileRows(files[fileIdx], fileIdx)
}

// FlattenFullFile turns current file content into source rows, marking current
// line numbers touched by the review diff.
func FlattenFullFile(files []diff.FileDiff, fileIdx int, lines []string) []Row {
	if fileIdx < 0 || fileIdx >= len(files) {
		return nil
	}
	return flattenFullFileRows(files[fileIdx], fileIdx, lines, false)
}

// FlattenReviewFile turns current file content into source rows while keeping
// review metadata inline. In full mode it renders the whole current file with
// hunk headers and deleted baseline-only rows. In local mode it renders compact
// hunks unless that hunk has an expansion entry, in which case the hunk is
// rebuilt from current content with extra context on both sides.
func FlattenReviewFile(files []diff.FileDiff, fileIdx int, lines []string, localExpansion map[int]int, full bool) []Row {
	if fileIdx < 0 || fileIdx >= len(files) {
		return nil
	}
	if full {
		return flattenFullFileRows(files[fileIdx], fileIdx, lines, true)
	}
	if len(localExpansion) == 0 {
		return FlattenFile(files, fileIdx)
	}
	return flattenLocalExpandedFileRows(files[fileIdx], fileIdx, lines, localExpansion)
}

func flattenFullFileRows(f diff.FileDiff, fileIdx int, lines []string, reviewInline bool) []Row {
	changed := changedCurrentLineHunks(f)
	rows := make([]Row, 0, len(lines))
	events := map[int][]Row{}
	if reviewInline {
		events = reviewEvents(f, fileIdx, len(lines))
	}
	for i, content := range lines {
		lineNum := i + 1
		hunkIdx, lineChanged := changed[lineNum]
		rows = append(rows, events[lineNum]...)
		rows = append(rows, Row{
			Kind:    RowLine,
			FileIdx: fileIdx,
			Line: diff.Line{
				Kind:    diff.LineContext,
				Content: content,
				NewLine: lineNum,
			},
			Changed: lineChanged,
			HunkIdx: hunkIdx,
		})
	}
	rows = append(rows, events[len(lines)+1]...)
	return rows
}

// MessageRows returns a non-source placeholder for the selected file panel.
func MessageRows(fileIdx int, text string) []Row {
	if fileIdx < 0 {
		fileIdx = 0
	}
	return []Row{{Kind: RowHunkHeader, FileIdx: fileIdx, Text: text}}
}

func flattenFileRows(f diff.FileDiff, fileIdx int) []Row {
	var rows []Row
	switch {
	case f.Binary:
		rows = append(rows, Row{Kind: RowHunkHeader, FileIdx: fileIdx, Text: "(binary file)"})
	case len(f.Hunks) == 0:
		rows = append(rows, Row{Kind: RowHunkHeader, FileIdx: fileIdx, Text: "(no textual changes)"})
	default:
		for hi, h := range f.Hunks {
			hunkIdx := hi + 1
			rows = append(rows, Row{Kind: RowHunkHeader, FileIdx: fileIdx, HunkIdx: hunkIdx, Text: h.Header})
			for _, ln := range h.Lines {
				rows = append(rows, Row{Kind: RowLine, FileIdx: fileIdx, HunkIdx: hunkIdx, Line: ln})
			}
		}
	}
	return rows
}

func flattenLocalExpandedFileRows(f diff.FileDiff, fileIdx int, lines []string, localExpansion map[int]int) []Row {
	var rows []Row
	for hi, h := range f.Hunks {
		extra := localExpansion[hi]
		if extra <= 0 {
			hunkIdx := hi + 1
			rows = append(rows, Row{Kind: RowHunkHeader, FileIdx: fileIdx, HunkIdx: hunkIdx, Text: h.Header})
			for _, ln := range h.Lines {
				rows = append(rows, Row{Kind: RowLine, FileIdx: fileIdx, HunkIdx: hunkIdx, Line: ln})
			}
			continue
		}
		rows = append(rows, expandedHunkRows(f, fileIdx, hi, lines, extra)...)
	}
	return rows
}

func expandedHunkRows(f diff.FileDiff, fileIdx, hunkIdxZero int, lines []string, extra int) []Row {
	h := f.Hunks[hunkIdxZero]
	hunkIdx := hunkIdxZero + 1
	rows := []Row{{Kind: RowHunkHeader, FileIdx: fileIdx, HunkIdx: hunkIdx, Text: h.Header}}

	start, end := expandedCurrentRange(h, len(lines), extra)
	events := deletionEventsForHunk(h, fileIdx, hunkIdx, len(lines))
	emitted := map[int]bool{}
	changed := changedCurrentLineHunks(f)
	for lineNum := start; lineNum <= end; lineNum++ {
		if lineNum < 1 || lineNum > len(lines) {
			continue
		}
		rows = append(rows, events[lineNum]...)
		if len(events[lineNum]) > 0 {
			emitted[lineNum] = true
		}
		_, lineChanged := changed[lineNum]
		rows = append(rows, Row{
			Kind:    RowLine,
			FileIdx: fileIdx,
			HunkIdx: hunkIdx,
			Line: diff.Line{
				Kind:    diff.LineContext,
				Content: lines[lineNum-1],
				NewLine: lineNum,
			},
			Changed: lineChanged,
		})
	}
	for _, anchor := range sortedEventAnchors(events) {
		if emitted[anchor] {
			continue
		}
		rows = append(rows, events[anchor]...)
	}
	return rows
}

func expandedCurrentRange(h diff.Hunk, lineCount, extra int) (int, int) {
	if lineCount <= 0 {
		return 1, 0
	}
	start := h.NewStart
	end := h.NewStart + h.NewLines - 1
	if h.NewLines == 0 {
		end = h.NewStart - 1
	}
	start = max(1, start-extra)
	end = min(lineCount, end+extra)
	if h.NewLines == 0 {
		end = min(lineCount, h.NewStart+extra-1)
	}
	return start, end
}

func reviewEvents(f diff.FileDiff, fileIdx, lineCount int) map[int][]Row {
	events := map[int][]Row{}
	for hi, h := range f.Hunks {
		hunkIdx := hi + 1
		headerAnchor := clampAnchor(h.NewStart, lineCount)
		events[headerAnchor] = append(events[headerAnchor], Row{
			Kind:    RowHunkHeader,
			FileIdx: fileIdx,
			HunkIdx: hunkIdx,
			Text:    h.Header,
		})
		for anchor, rows := range deletionEventsForHunk(h, fileIdx, hunkIdx, lineCount) {
			events[anchor] = append(events[anchor], rows...)
		}
	}
	return events
}

func deletionEventsForHunk(h diff.Hunk, fileIdx, hunkIdx, lineCount int) map[int][]Row {
	events := map[int][]Row{}
	nextNewLine := h.NewStart
	for _, ln := range h.Lines {
		switch ln.Kind {
		case diff.LineDelete:
			anchor := clampAnchor(nextNewLine, lineCount)
			events[anchor] = append(events[anchor], Row{
				Kind:    RowLine,
				FileIdx: fileIdx,
				HunkIdx: hunkIdx,
				Line:    ln,
			})
		case diff.LineAdd, diff.LineContext:
			if ln.NewLine > 0 {
				nextNewLine = ln.NewLine + 1
			} else {
				nextNewLine++
			}
		}
	}
	return events
}

func clampAnchor(anchor, lineCount int) int {
	if anchor < 1 {
		return 1
	}
	if anchor > lineCount+1 {
		return lineCount + 1
	}
	return anchor
}

func sortedEventAnchors(events map[int][]Row) []int {
	anchors := make([]int, 0, len(events))
	for anchor := range events {
		anchors = append(anchors, anchor)
	}
	for i := 1; i < len(anchors); i++ {
		for j := i; j > 0 && anchors[j] < anchors[j-1]; j-- {
			anchors[j], anchors[j-1] = anchors[j-1], anchors[j]
		}
	}
	return anchors
}

// changedCurrentLineHunks indexes changed current-side lines in one diff pass.
// Full-file flattening can then classify every source line in O(1), instead of
// rescanning every hunk and diff line for each source line.
func changedCurrentLineHunks(f diff.FileDiff) map[int]int {
	changed := map[int]int{}
	for hi, h := range f.Hunks {
		for _, ln := range h.Lines {
			if ln.Kind != diff.LineAdd || ln.NewLine < 1 {
				continue
			}
			// Preserve the old first-match behavior if malformed input repeats a
			// current line across multiple hunks.
			if _, exists := changed[ln.NewLine]; !exists {
				changed[ln.NewLine] = hi + 1
			}
		}
	}
	return changed
}
