package outline

import (
	"sort"
	"strings"

	"cride/internal/diff"
	"cride/internal/lsp"
	"cride/internal/source"
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

	type renamePair struct {
		bi, ai     int
		similarity float64
	}
	var candidates []renamePair
	for bi, b := range beforeFlat {
		if matchedBefore[bi] {
			continue
		}
		for ai, a := range afterFlat {
			if matchedAfter[ai] || a.symbol.Kind != b.symbol.Kind {
				continue
			}
			similarity := bodySimilarity(beforeContent, b.symbol.Range, afterContent, a.symbol.Range)
			if similarity >= 0.6 {
				candidates = append(candidates, renamePair{bi: bi, ai: ai, similarity: similarity})
			}
		}
	}
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

func bodySimilarity(before []byte, beforeRange source.Range, after []byte, afterRange source.Range) float64 {
	beforeLines := normalizedRangeLines(before, beforeRange)
	afterLines := normalizedRangeLines(after, afterRange)
	if len(beforeLines) == 0 || len(afterLines) == 0 {
		return 0
	}
	counts := make(map[string]int, len(beforeLines))
	for _, line := range beforeLines {
		counts[line]++
	}
	overlap := 0
	for _, line := range afterLines {
		if counts[line] > 0 {
			overlap++
			counts[line]--
		}
	}
	denominator := len(beforeLines)
	if len(afterLines) > denominator {
		denominator = len(afterLines)
	}
	return float64(overlap) / float64(denominator)
}

func normalizedRangeLines(content []byte, r source.Range) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start, end := r.Start.Line, r.End.Line
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if start > len(lines) {
		return nil
	}
	if end > len(lines) {
		end = len(lines)
	}
	out := make([]string, 0, end-start+1)
	for _, line := range lines[start-1 : end] {
		normalized := strings.Join(strings.Fields(line), " ")
		if normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
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
