package ui

// Side-by-side pairing per DESIGN.md's "Rendering and interaction" section:
// consecutive delete+insert runs pair line by line; unpaired lines get a blank
// on the other side. Pure functions of the flattened rows, so unified rendering
// is untouched.

import "cride/internal/diff"

// linePair holds one aligned row of the split view. Nil means a blank cell.
type linePair struct {
	Left  *diff.Line // baseline side: context or delete
	Right *diff.Line // current side: context or add
}

// alignPairs pairs a hunk's lines cr-style: a run of deletes followed by a
// run of adds zips index-by-index; the longer run's tail is unpaired.
// Context lines pair with themselves. Deterministic by construction.
func alignPairs(lines []diff.Line) []linePair {
	var pairs []linePair
	var deletes, adds []*diff.Line

	flush := func() {
		n := max(len(deletes), len(adds))
		for i := 0; i < n; i++ {
			var pair linePair
			if i < len(deletes) {
				pair.Left = deletes[i]
			}
			if i < len(adds) {
				pair.Right = adds[i]
			}
			pairs = append(pairs, pair)
		}
		deletes, adds = nil, nil
	}

	for i := range lines {
		ln := &lines[i]
		switch ln.Kind {
		case diff.LineDelete:
			// A new delete run after adds means the previous run is closed.
			if len(adds) > 0 {
				flush()
			}
			deletes = append(deletes, ln)
		case diff.LineAdd:
			adds = append(adds, ln)
		default:
			flush()
			pairs = append(pairs, linePair{Left: ln, Right: ln})
		}
	}
	flush()
	return pairs
}

// PairRows converts unified rows into split-view rows: line-row runs become
// RowPair rows; header and placeholder rows pass through. Row.Line is set to
// the primary (current-side, else baseline) line so row-based helpers keep
// working.
func PairRows(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	var run []Row

	flushRun := func() {
		if len(run) == 0 {
			return
		}
		lines := make([]diff.Line, len(run))
		for i, r := range run {
			lines[i] = r.Line
		}
		template := run[0]
		for _, pair := range alignPairs(lines) {
			row := Row{
				Kind:    RowPair,
				FileIdx: template.FileIdx,
				HunkIdx: template.HunkIdx,
				Left:    pair.Left,
				Right:   pair.Right,
				Changed: pairChanged(pair),
			}
			if pair.Right != nil {
				row.Line = *pair.Right
			} else if pair.Left != nil {
				row.Line = *pair.Left
			}
			out = append(out, row)
		}
		run = nil
	}

	for _, r := range rows {
		if r.Kind == RowLine {
			run = append(run, r)
			continue
		}
		flushRun()
		out = append(out, r)
	}
	flushRun()
	return out
}

func pairChanged(pair linePair) bool {
	if pair.Left != nil && pair.Left.Kind == diff.LineDelete {
		return true
	}
	if pair.Right != nil && pair.Right.Kind == diff.LineAdd {
		return true
	}
	return false
}

// PairRowHasChange reports whether either side of a pair row is a change.
func PairRowHasChange(r Row) bool {
	if r.Kind != RowPair {
		return false
	}
	return pairChanged(linePair{Left: r.Left, Right: r.Right})
}
