// Package search contains project-navigation result types plus pure parsing and
// ranking helpers for file, symbol, and project text search.
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

type ResultGroup int

const (
	ResultGroupNone ResultGroup = iota
	ResultGroupSymbol
	ResultGroupGrep
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

// SymbolCategory is the coarse semantic group used by Search Everywhere.
// The ordering is intentional: larger values are preferred during symbol
// ranking, while zero keeps non-symbol search results neutral.
type SymbolCategory int

const (
	SymbolCategoryOther SymbolCategory = iota
	SymbolCategoryVariable
	SymbolCategoryFunction
	SymbolCategoryType
)

type Result struct {
	Kind     ResultKind
	Group    ResultGroup
	Location source.Location
	Label    string
	// SearchText is the unadorned name used for fuzzy matching. Results whose
	// labels include presentation metadata (notably workspace symbols) set it
	// so kind names, containers, and paths do not influence relevance.
	SearchText string
	Preview    string
	Score      int
	Review     diff.ReviewMarkers
	Side       ResultSide
	// SymbolCategory and Reference are populated for Search Everywhere symbol
	// rows. They keep presentation labels out of semantic result ordering.
	SymbolCategory SymbolCategory
	Reference      ReferenceKind
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

// FuzzyScore returns a positive score when the significant query runes appear
// in order in path. Separators are insignificant in the query, allowing human
// input such as "xose gateway" to match xose_Gateway and xoseGateway.
func FuzzyScore(path, query string) (int, bool) {
	pathLower := strings.ToLower(path)
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" {
		return max(1, 200-len(pathLower)), true
	}
	queryRunes := compactFuzzyRunes(queryLower)
	if len(queryRunes) == 0 {
		return max(1, 200-len(pathLower)), true
	}

	score := 0
	last := -2
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
	compactQuery := string(queryRunes)
	compactPath := string(compactFuzzyRunes(pathLower))
	if idx := strings.Index(compactPath, compactQuery); idx >= 0 {
		score += 180
		if idx == 0 {
			score += 75
		}
	}
	base := pathLower
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	if strings.Contains(base, queryLower) || strings.Contains(string(compactFuzzyRunes(base)), compactQuery) {
		score += 150
	}

	score += max(0, 120-len(pathLower))
	return score, true
}

func isBoundary(r rune) bool {
	return r == '/' || r == '_' || r == '-' || r == '.' || unicode.IsSpace(r)
}

func compactFuzzyRunes(s string) []rune {
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		if isBoundary(r) {
			continue
		}
		runes = append(runes, r)
	}
	return runes
}

// CompactQuery removes identifier and path separators from a fuzzy query. It
// is useful when a backend performs its own filtering before local ranking.
func CompactQuery(query string) string {
	return string(compactFuzzyRunes(strings.TrimSpace(query)))
}

// RankSymbols fuzzy-filters workspace-symbol results without letting their
// decorated display labels affect relevance.
func RankSymbols(results []Result, query string, limit int) []Result {
	ranked := make([]Result, 0, len(results))
	for _, result := range results {
		candidate := result.SearchText
		if candidate == "" {
			candidate = result.Label
		}
		fuzzyScore, ok := SymbolScore(candidate, query)
		if !ok {
			continue
		}
		// Keep a small contribution from the backend while local fuzzy
		// relevance remains the dominant signal.
		result.Score = fuzzyScore + max(0, min(result.Score, 100))
		ranked = append(ranked, result)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].SymbolCategory != ranked[j].SymbolCategory {
			return ranked[i].SymbolCategory > ranked[j].SymbolCategory
		}
		if referencePriority(ranked[i].Reference) != referencePriority(ranked[j].Reference) {
			return referencePriority(ranked[i].Reference) > referencePriority(ranked[j].Reference)
		}
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].SearchText != ranked[j].SearchText {
			return strings.ToLower(ranked[i].SearchText) < strings.ToLower(ranked[j].SearchText)
		}
		if ranked[i].Location.Path != ranked[j].Location.Path {
			return ranked[i].Location.Path < ranked[j].Location.Path
		}
		return ranked[i].Location.Line < ranked[j].Location.Line
	})
	return trimResults(ranked, limit)
}

// SymbolScore is case-insensitive for eligibility and adds a bounded bonus
// when the candidate preserves the query's casing. Thus xoseGateway ranks
// ahead of XoseGateway for "xose gateway", while both remain valid matches.
func SymbolScore(candidate, query string) (int, bool) {
	score, ok := FuzzyScore(candidate, query)
	if !ok {
		return 0, false
	}
	queryRunes := compactFuzzyRunes(strings.TrimSpace(query))
	if len(queryRunes) == 0 {
		return score, true
	}

	candidateRunes := []rune(candidate)
	ci := 0
	caseMatches := 0
	for _, qr := range queryRunes {
		for ci < len(candidateRunes) {
			cr := candidateRunes[ci]
			ci++
			if unicode.ToLower(cr) != unicode.ToLower(qr) {
				continue
			}
			if cr == qr {
				caseMatches++
			}
			break
		}
	}
	score += caseMatches * 4
	compactCandidate := string(compactFuzzyRunes(candidate))
	compactQuery := string(queryRunes)
	if strings.Contains(compactCandidate, compactQuery) {
		score += 160
	}
	if trimmed := strings.TrimSpace(query); trimmed != "" && strings.Contains(candidate, trimmed) {
		score += 240
	}
	return score, true
}

// QuerySeed chooses the strongest literal token for a backend query. The full
// fuzzy query is still applied locally; using one token for retrieval lets
// "xose gateway" find xose_Gateway in both symbol and grep backends.
func QuerySeed(query string) string {
	terms := strings.FieldsFunc(strings.TrimSpace(query), func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seed := ""
	for _, term := range terms {
		if len([]rune(term)) > len([]rune(seed)) {
			seed = term
		}
	}
	return seed
}

// GrepRelevanceScore measures query proximity in the matching source line and
// path. Literal and separator-insensitive contiguous matches outrank loose
// subsequence matches.
func GrepRelevanceScore(result Result, query string) (int, bool) {
	lineScore, lineOK := FuzzyScore(result.Preview, query)
	pathScore, pathOK := FuzzyScore(result.Location.Path, query)
	if !lineOK && !pathOK {
		return 0, false
	}

	score := 0
	if lineOK {
		score += lineScore
	}
	if pathOK {
		score += pathScore / 2
	}
	needle := strings.TrimSpace(query)
	haystack := result.Preview
	if !strings.ContainsFunc(needle, unicode.IsUpper) {
		needle = strings.ToLower(needle)
		haystack = strings.ToLower(haystack)
	}
	if needle != "" && strings.Contains(haystack, needle) {
		score += 1000
	}
	compactNeedle := strings.ToLower(string(compactFuzzyRunes(query)))
	compactLine := strings.ToLower(string(compactFuzzyRunes(result.Preview)))
	if compactNeedle != "" && strings.Contains(compactLine, compactNeedle) {
		score += 600
	}
	return score, true
}

// RankGrepResults places the most textually relevant current-side source
// matches first, with small review/current-file/recency tie-break bonuses.
func RankGrepResults(results []Result, query string, current source.Location, review diff.ReviewIndex, recent map[string]int, limit int) []Result {
	ranked := make([]Result, 0, len(results))
	seen := make(map[string]bool)
	for _, result := range results {
		if result.Side == ResultSideBaseline {
			continue
		}
		score, ok := GrepRelevanceScore(result, query)
		if !ok {
			continue
		}
		key := result.Location.Path + "\x00" + strconv.Itoa(result.Location.Line) + "\x00" + result.Preview
		if seen[key] {
			continue
		}
		seen[key] = true
		result.Kind = ResultText
		result.Group = ResultGroupGrep
		result.Label = "[grep] " + result.Location.Path + ":" + strconv.Itoa(max(1, result.Location.Line)) + ":" + strconv.Itoa(max(1, result.Location.Column))
		result.Score = score
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		if result.Location.Path == current.Path {
			result.Score += 100
		}
		if result.Review.Changed || result.Review.ContainsAddition || result.Review.ContainsDeletion {
			result.Score += 250
		} else if review != nil && review.IsChanged(result.Location.Path) {
			result.Score += 100
		}
		if idx, ok := recent[result.Location.Path]; ok {
			result.Score += max(0, 80-idx*8)
		}
		ranked = append(ranked, result)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return locationLess(ranked[i].Location, ranked[j].Location, ranked[i].Label, ranked[j].Label)
	})
	return trimResults(ranked, limit)
}

func referencePriority(kind ReferenceKind) int {
	switch kind {
	case ReferenceDefinition:
		return 2
	case ReferenceReference:
		return 1
	default:
		return 0
	}
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
