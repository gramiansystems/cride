package outline

import (
	"bytes"
	"sort"
	"strings"

	"cride/internal/diff"
	"cride/internal/lsp"
	"cride/internal/source"
)

const (
	renameSimilarityThreshold = 0.6
	// Fuzzy rename matching is inherently pairwise. Keep the optional
	// convenience bounded so generated files and broad refactors degrade to
	// added/removed symbols instead of consuming memory in proportion to the
	// Cartesian product.
	maxRenameComparisons   = 4096
	maxRenameComparedLines = 1 << 20
)

// ChangeType classifies how a declaration participates in the review.
type ChangeType int

const (
	SymbolUnchanged ChangeType = iota
	SymbolAdded
	SymbolRemoved
	SymbolModified
	SymbolRenamed
)

func (t ChangeType) String() string {
	switch t {
	case SymbolAdded:
		return "added"
	case SymbolRemoved:
		return "removed"
	case SymbolModified:
		return "modified"
	case SymbolRenamed:
		return "renamed"
	default:
		return "unchanged"
	}
}

// SymbolChange is one before/current declaration pairing.
type SymbolChange struct {
	Type             ChangeType
	Path             string
	Before           *lsp.DocumentSymbol
	After            *lsp.DocumentSymbol
	ContainsAddition bool
	ContainsDeletion bool
	BodySimilarity   float64
}

type qualifiedSymbol struct {
	symbol lsp.DocumentSymbol
	key    string
}

type renamePair struct {
	bi, ai     int
	similarity float64
}

// DiffOutlines matches declarations by qualified name and marks same-name
// symbols modified when their ranges intersect review lines.
func DiffOutlines(before, after []lsp.DocumentSymbol, beforeContent, afterContent []byte, oldPath, newPath string, files []diff.FileDiff) []SymbolChange {
	beforeFlat := flattenQualified(before)
	afterFlat := flattenQualified(after)
	displayPath := newPath
	if displayPath == "" || displayPath == "/dev/null" {
		displayPath = oldPath
	}

	afterByKey := make(map[string][]int)
	for i, item := range afterFlat {
		afterByKey[item.key] = append(afterByKey[item.key], i)
	}
	matchedAfter := make([]bool, len(afterFlat))
	matchedBefore := make([]bool, len(beforeFlat))
	var changes []SymbolChange
	for bi, item := range beforeFlat {
		indices := afterByKey[item.key]
		ai := -1
		for _, candidate := range indices {
			if !matchedAfter[candidate] {
				ai = candidate
				break
			}
		}
		if ai < 0 {
			continue
		}
		matchedBefore[bi], matchedAfter[ai] = true, true
		b, a := item.symbol, afterFlat[ai].symbol
		containsAddition := rangeHasKind(files, newPath, a.Range, diff.ChangeAdded)
		containsDeletion := rangeHasKind(files, oldPath, b.Range, diff.ChangeDeleted)
		changeType := SymbolUnchanged
		if containsAddition || containsDeletion {
			changeType = SymbolModified
		}
		changes = append(changes, SymbolChange{
			Type:             changeType,
			Path:             displayPath,
			Before:           &b,
			After:            &a,
			ContainsAddition: containsAddition,
			ContainsDeletion: containsDeletion,
		})
	}

	candidates := fuzzyRenameCandidates(beforeFlat, afterFlat, matchedBefore, matchedAfter, beforeContent, afterContent)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].similarity > candidates[j].similarity })
	for _, pair := range candidates {
		if matchedBefore[pair.bi] || matchedAfter[pair.ai] {
			continue
		}
		matchedBefore[pair.bi], matchedAfter[pair.ai] = true, true
		b, a := beforeFlat[pair.bi].symbol, afterFlat[pair.ai].symbol
		changes = append(changes, SymbolChange{
			Type:             SymbolRenamed,
			Path:             displayPath,
			Before:           &b,
			After:            &a,
			ContainsAddition: rangeHasKind(files, newPath, a.Range, diff.ChangeAdded),
			ContainsDeletion: rangeHasKind(files, oldPath, b.Range, diff.ChangeDeleted),
			BodySimilarity:   pair.similarity,
		})
	}
	for i, item := range beforeFlat {
		if !matchedBefore[i] {
			b := item.symbol
			changes = append(changes, SymbolChange{
				Type:             SymbolRemoved,
				Path:             displayPath,
				Before:           &b,
				ContainsDeletion: rangeHasKind(files, oldPath, b.Range, diff.ChangeDeleted),
			})
		}
	}
	for i, item := range afterFlat {
		if !matchedAfter[i] {
			a := item.symbol
			changes = append(changes, SymbolChange{
				Type:             SymbolAdded,
				Path:             displayPath,
				After:            &a,
				ContainsAddition: rangeHasKind(files, newPath, a.Range, diff.ChangeAdded),
			})
		}
	}

	sort.SliceStable(changes, func(i, j int) bool {
		li, lj := changeLine(changes[i]), changeLine(changes[j])
		if li != lj {
			return li < lj
		}
		return changeName(changes[i]) < changeName(changes[j])
	})
	return changes
}

func flattenQualified(symbols []lsp.DocumentSymbol) []qualifiedSymbol {
	var out []qualifiedSymbol
	var walk func([]lsp.DocumentSymbol, []string)
	walk = func(items []lsp.DocumentSymbol, parents []string) {
		for _, item := range items {
			path := append(append([]string(nil), parents...), item.Name)
			children := item.Children
			item.Children = nil
			out = append(out, qualifiedSymbol{symbol: item, key: strings.Join(path, "/") + "\x00" + item.Kind.String()})
			walk(children, path)
		}
	}
	walk(symbols, nil)
	return out
}

// renameComparisonCount returns the number of compatible unmatched pairs,
// stopping once the configured budget is exceeded. Counting by symbol kind
// avoids disabling useful rename detection merely because the two sides have
// many symbols that could never match.
func renameComparisonCount(before, after []qualifiedSymbol, matchedBefore, matchedAfter []bool) int {
	afterByKind := make(map[lsp.SymbolKind]int)
	for i, item := range after {
		if !matchedAfter[i] {
			afterByKind[item.symbol.Kind]++
		}
	}
	comparisons := 0
	for i, item := range before {
		if matchedBefore[i] {
			continue
		}
		comparisons += afterByKind[item.symbol.Kind]
		if comparisons > maxRenameComparisons {
			return comparisons
		}
	}
	return comparisons
}

func fuzzyRenameCandidates(before, after []qualifiedSymbol, matchedBefore, matchedAfter []bool, beforeContent, afterContent []byte) []renamePair {
	comparisons := renameComparisonCount(before, after, matchedBefore, matchedAfter)
	if comparisons == 0 || comparisons > maxRenameComparisons {
		return nil
	}

	beforeLineCount := contentLineCount(beforeContent)
	afterLineCount := contentLineCount(afterContent)
	if renameComparisonWork(before, after, matchedBefore, matchedAfter, beforeLineCount, afterLineCount) > maxRenameComparedLines {
		return nil
	}

	// Normalizing once avoids splitting and rewriting both complete files for
	// every candidate pair. Per-pair work below is limited to symbol ranges and
	// the aggregate range scan is bounded above.
	beforeLines := normalizedContentLines(beforeContent)
	afterLines := normalizedContentLines(afterContent)
	candidates := make([]renamePair, 0, min(comparisons, 64))
	for bi, b := range before {
		if matchedBefore[bi] {
			continue
		}
		for ai, a := range after {
			if matchedAfter[ai] || a.symbol.Kind != b.symbol.Kind {
				continue
			}
			similarity := bodySimilarity(beforeLines, b.symbol.Range, afterLines, a.symbol.Range)
			if similarity >= renameSimilarityThreshold {
				candidates = append(candidates, renamePair{bi: bi, ai: ai, similarity: similarity})
			}
		}
	}
	return candidates
}

// renameComparisonWork bounds the number of normalized source lines visited
// across all compatible pairs. A small Cartesian product can still be
// expensive when every symbol range spans a generated or malformed file.
func renameComparisonWork(before, after []qualifiedSymbol, matchedBefore, matchedAfter []bool, beforeLineCount, afterLineCount int) int {
	beforeByKind := make(map[lsp.SymbolKind]int)
	afterByKind := make(map[lsp.SymbolKind]int)
	for i, item := range before {
		if !matchedBefore[i] {
			beforeByKind[item.symbol.Kind]++
		}
	}
	for i, item := range after {
		if !matchedAfter[i] {
			afterByKind[item.symbol.Kind]++
		}
	}

	work := 0
	for i, item := range before {
		if matchedBefore[i] {
			continue
		}
		work = addRenameComparisonWork(work, normalizedRangeLineCount(beforeLineCount, item.symbol.Range), afterByKind[item.symbol.Kind])
		if work > maxRenameComparedLines {
			return work
		}
	}
	for i, item := range after {
		if matchedAfter[i] {
			continue
		}
		work = addRenameComparisonWork(work, normalizedRangeLineCount(afterLineCount, item.symbol.Range), beforeByKind[item.symbol.Kind])
		if work > maxRenameComparedLines {
			return work
		}
	}
	return work
}

func addRenameComparisonWork(total, lines, pairs int) int {
	if lines <= 0 || pairs <= 0 {
		return total
	}
	remaining := maxRenameComparedLines - total
	if remaining < 0 || lines > remaining/pairs {
		return maxRenameComparedLines + 1
	}
	return total + lines*pairs
}

func contentLineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

func rangeHasKind(files []diff.FileDiff, path string, r source.Range, kind diff.ChangeKind) bool {
	if path == "" || path == "/dev/null" {
		return false
	}
	start, end := r.Start.Line, r.End.Line
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	for _, file := range files {
		if kind == diff.ChangeAdded && file.NewPath != path {
			continue
		}
		if kind == diff.ChangeDeleted && file.OldPath != path {
			continue
		}
		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				lineNumber := 0
				switch kind {
				case diff.ChangeAdded:
					if line.Kind == diff.LineAdd {
						lineNumber = line.NewLine
					}
				case diff.ChangeDeleted:
					if line.Kind == diff.LineDelete {
						lineNumber = line.OldLine
					}
				}
				if lineNumber >= start && lineNumber <= end {
					return true
				}
			}
		}
	}
	return false
}

func normalizedContentLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return lines
}

func bodySimilarity(before []string, beforeRange source.Range, after []string, afterRange source.Range) float64 {
	beforeStart, beforeEnd := normalizedRangeBounds(len(before), beforeRange)
	afterStart, afterEnd := normalizedRangeBounds(len(after), afterRange)
	if beforeStart >= beforeEnd || afterStart >= afterEnd {
		return 0
	}

	counts := make(map[string]int, min(beforeEnd-beforeStart, 256))
	beforeCount := 0
	for _, line := range before[beforeStart:beforeEnd] {
		if line != "" {
			counts[line]++
			beforeCount++
		}
	}
	overlap, afterCount := 0, 0
	for _, line := range after[afterStart:afterEnd] {
		if line == "" {
			continue
		}
		afterCount++
		if counts[line] > 0 {
			overlap++
			counts[line]--
		}
	}
	if beforeCount == 0 || afterCount == 0 {
		return 0
	}
	denominator := max(beforeCount, afterCount)
	return float64(overlap) / float64(denominator)
}

// normalizedRangeBounds converts a 1-based inclusive source range into a
// clamped 0-based half-open slice range.
func normalizedRangeBounds(lineCount int, r source.Range) (int, int) {
	start, end := r.Start.Line, r.End.Line
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if start > lineCount {
		return lineCount, lineCount
	}
	if end > lineCount {
		end = lineCount
	}
	return start - 1, end
}

func normalizedRangeLineCount(lineCount int, r source.Range) int {
	start, end := normalizedRangeBounds(lineCount, r)
	return end - start
}

func changeLine(change SymbolChange) int {
	if change.After != nil {
		return change.After.Range.Start.Line
	}
	if change.Before != nil {
		return change.Before.Range.Start.Line
	}
	return 0
}

func changeName(change SymbolChange) string {
	if change.After != nil {
		return change.After.Name
	}
	if change.Before != nil {
		return change.Before.Name
	}
	return ""
}
