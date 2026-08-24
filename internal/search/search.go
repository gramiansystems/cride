// Package search contains project-navigation result types plus pure parsing and
// ranking helpers for file open and project text search.
package search

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"cride/internal/diff"
	"cride/internal/source"
)

type ResultKind int

const (
	ResultFile ResultKind = iota
	ResultText
)

type ResultSide int

const (
	ResultSideUnknown ResultSide = iota
	ResultSideCurrent
	ResultSideBaseline
	ResultSideBoth
)

func (s ResultSide) String() string {
	switch s {
	case ResultSideCurrent:
		return "after"
	case ResultSideBaseline:
		return "before"
	case ResultSideBoth:
		return "before+after"
	default:
		return ""
	}
}

type Result struct {
	Kind     ResultKind
	Location source.Location
	Label    string
	Preview  string
	Score    int
	Review   diff.ReviewMarkers
	Side     ResultSide
}

// ParseRipgrepOutput parses `rg --line-number --column --no-heading` output.
func ParseRipgrepOutput(out []byte) []Result {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	results := make([]Result, 0, len(lines))
	for _, line := range lines {
		if result, ok := ParseRipgrepLine(line); ok {
			results = append(results, result)
		}
	}
	return results
}

// ParseRipgrepLine parses one `path:line:column:text` result line.
func ParseRipgrepLine(line string) (Result, bool) {
	for first := 0; first < len(line); first++ {
		if line[first] != ':' {
			continue
		}
		secondRel := strings.IndexByte(line[first+1:], ':')
		if secondRel < 0 {
			return Result{}, false
		}
		second := first + 1 + secondRel
		ln, ok := positiveInt(line[first+1 : second])
		if !ok {
			continue
		}

		thirdRel := strings.IndexByte(line[second+1:], ':')
		if thirdRel < 0 {
			return Result{}, false
		}
		third := second + 1 + thirdRel
		col, ok := positiveInt(line[second+1 : third])
		if !ok {
			continue
		}

		path := strings.TrimPrefix(line[:first], "./")
		if path == "" {
			return Result{}, false
		}
		preview := line[third+1:]
		return Result{
			Kind: ResultText,
			Location: source.Location{
				Path:   path,
				Line:   ln,
				Column: col,
			},
			Label:   path + ":" + strconv.Itoa(ln) + ":" + strconv.Itoa(col),
			Preview: preview,
		}, true
	}
	return Result{}, false
}

func positiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// RankFiles filters project paths by fuzzy query and sorts changed and recently
// visited files ahead of otherwise similar fuzzy matches.
func RankFiles(paths []string, query string, changed map[string]bool, recent map[string]int, limit int) []Result {
	results := make([]Result, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimPrefix(path, "./")
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		score, ok := FuzzyScore(path, query)
		if !ok {
			continue
		}
		if changed[path] {
			score += 1000
		}
		if idx, ok := recent[path]; ok {
			score += max(0, 500-idx*50)
		}
		results = append(results, Result{
			Kind:     ResultFile,
			Location: source.Location{Path: path, Line: 1, Column: 1},
			Label:    path,
			Score:    score,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if len(results[i].Label) != len(results[j].Label) {
			return len(results[i].Label) < len(results[j].Label)
		}
		return strings.ToLower(results[i].Label) < strings.ToLower(results[j].Label)
	})
	return trimResults(results, limit)
}

// FuzzyScore returns a positive score when query runes appear in order in path.
func FuzzyScore(path, query string) (int, bool) {
	pathLower := strings.ToLower(path)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" {
		return max(1, 200-len(pathLower)), true
	}

	score := 0
	last := -2
	queryRunes := []rune(queryLower)
	pathRunes := []rune(pathLower)
	pi := 0
	for _, qr := range queryRunes {
		found := -1
		for pi < len(pathRunes) {
			if pathRunes[pi] == qr {
				found = pi
				pi++
				break
			}
			pi++
		}
		if found < 0 {
			return 0, false
		}

		score += 20
		if found == last+1 {
			score += 35
		}
		if found == 0 || isBoundary(pathRunes[found-1]) {
			score += 25
		}
		last = found
	}

	if idx := strings.Index(pathLower, queryLower); idx >= 0 {
		score += 250
		if idx == 0 || strings.LastIndex(pathLower[:idx], "/") >= 0 {
			score += 100
		}
	}
	base := pathLower
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	if strings.Contains(base, queryLower) {
		score += 150
	}

	score += max(0, 120-len(pathLower))
	return score, true
}

func isBoundary(r rune) bool {
	return r == '/' || r == '_' || r == '-' || r == '.' || unicode.IsSpace(r)
}

// RankTextResults scores text search results using review context.
func RankTextResults(results []Result, changedFiles map[string]bool, changedLines map[string]map[int]bool, limit int) []Result {
	return RankTextResultsWithReview(results, source.Location{}, legacyReviewIndex{changedFiles: changedFiles, changedLines: changedLines}, diff.ResultOrderReview, limit)
}

// RankTextResultsWithReview scores text search results using review metadata.
func RankTextResultsWithReview(results []Result, current source.Location, review diff.ReviewIndex, order diff.ResultOrder, limit int) []Result {
	ranked := make([]Result, 0, len(results))
	for i, result := range results {
		result.Score = max(0, 1000-i)
		containsAddition, containsDeletion := result.Review.ContainsAddition, result.Review.ContainsDeletion
		entireAddition, entireDeletion := result.Review.EntireAddition, result.Review.EntireDeletion
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		result.Review.ContainsAddition = containsAddition
		result.Review.ContainsDeletion = containsDeletion
		result.Review.EntireAddition = entireAddition
		result.Review.EntireDeletion = entireDeletion
		result.Score += reviewScore(result.Location, current, result.Review, review, false)
		ranked = append(ranked, result)
	}

	if order == diff.ResultOrderSource {
		sortTextResultsBySource(ranked)
		return trimResults(ranked, limit)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].Location.Path != ranked[j].Location.Path {
			return ranked[i].Location.Path < ranked[j].Location.Path
		}
		if ranked[i].Location.Line != ranked[j].Location.Line {
			return ranked[i].Location.Line < ranked[j].Location.Line
		}
		return ranked[i].Location.Column < ranked[j].Location.Column
	})
	return trimResults(ranked, limit)
}

func sortTextResultsBySource(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		return locationLess(results[i].Location, results[j].Location, results[i].Label, results[j].Label)
	})
}

func trimResults(results []Result, limit int) []Result {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

type legacyReviewIndex struct {
	changedFiles map[string]bool
	changedLines map[string]map[int]bool
}

func (idx legacyReviewIndex) IsChanged(path string) bool {
	return idx.changedFiles[path]
}

func (idx legacyReviewIndex) LineChangeKind(path string, line int) diff.ChangeKind {
	if idx.changedLines[path][line] {
		return diff.ChangeAdded
	}
	return diff.ChangeNone
}

func (idx legacyReviewIndex) IsUnread(path string, line int) bool {
	return false
}

func (idx legacyReviewIndex) AnnotationStatus(path string, line int) diff.AnnotationStatus {
	return diff.AnnotationNone
}

func reviewScore(loc, current source.Location, markers diff.ReviewMarkers, review diff.ReviewIndex, definition bool) int {
	score := 0
	if loc.Path != "" && loc.Path == current.Path {
		score += 10000
		if current.Line > 0 && loc.Line > 0 {
			delta := loc.Line - current.Line
			if delta < 0 {
				delta = -delta
			}
			score += max(0, 1000-delta)
		}
	}
	if markers.Changed || markers.ContainsAddition || markers.ContainsDeletion {
		score += 8000
		if markers.Unread {
			score += 1500
		}
	}
	if markers.Annotated {
		if markers.Annotation.Open() {
			score += 7000
		} else {
			score += 1200
		}
	}
	if review != nil && review.IsChanged(loc.Path) {
		score += 5000
	}
	if IsLikelyTest(loc.Path) {
		score += 2500
	}
	if definition {
		score += 2000
	}
	return score
}

// IsLikelyTest applies lightweight test-file heuristics used for review ranking.
func IsLikelyTest(path string) bool {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return true
	case strings.Contains(lower, ".test."):
		return true
	case strings.Contains(lower, ".spec."):
		return true
	case strings.HasSuffix(lower, "/tests.rs"), strings.Contains(lower, "/tests/"):
		return true
	default:
		return false
	}
}

func locationLess(a, b source.Location, aLabel, bLabel string) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Column != b.Column {
		return a.Column < b.Column
	}
	return aLabel < bLabel
}
