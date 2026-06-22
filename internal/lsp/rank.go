package lsp

import (
	"sort"
	"strings"

	"cride/internal/diff"
	"cride/internal/source"
)

func RankDiagnostics(results []Diagnostic, changedFiles map[string]bool, changedLines map[string]map[int]bool, limit int) []Diagnostic {
	return RankDiagnosticsWithReview(results, source.Location{}, legacyReviewIndex{changedFiles: changedFiles, changedLines: changedLines}, diff.ResultOrderReview, limit)
}

func RankDiagnosticsWithReview(results []Diagnostic, current source.Location, review diff.ReviewIndex, order diff.ResultOrder, limit int) []Diagnostic {
	ranked := make([]Diagnostic, 0, len(results))
	for i, result := range results {
		loc := result.Location()
		result.Score = max(0, 1000-i)
		result.Review = diff.MarkersForIndex(review, loc.Path, loc.Line)
		result.Score += reviewScore(loc, current, result.Review, review)
		switch result.Severity {
		case DiagnosticError:
			result.Score += 400
		case DiagnosticWarning:
			result.Score += 300
		case DiagnosticInformation:
			result.Score += 200
		case DiagnosticHint:
			result.Score += 100
		}
		ranked = append(ranked, result)
	}
	if order == diff.ResultOrderSource {
		sortDiagnosticsBySource(ranked)
		return trimDiagnostics(ranked, limit)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		a, b := ranked[i].Location(), ranked[j].Location()
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return trimDiagnostics(ranked, limit)
}

func RankWorkspaceSymbols(results []WorkspaceSymbol, changedFiles map[string]bool, limit int) []WorkspaceSymbol {
	return RankWorkspaceSymbolsWithReview(results, source.Location{}, legacyReviewIndex{changedFiles: changedFiles}, diff.ResultOrderReview, limit)
}

func RankWorkspaceSymbolsWithReview(results []WorkspaceSymbol, current source.Location, review diff.ReviewIndex, order diff.ResultOrder, limit int) []WorkspaceSymbol {
	ranked := make([]WorkspaceSymbol, 0, len(results))
	for i, result := range results {
		result.Score = max(0, 1000-i)
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		result.Score += reviewScore(result.Location, current, result.Review, review)
		ranked = append(ranked, result)
	}
	if order == diff.ResultOrderSource {
		sortWorkspaceSymbolsBySource(ranked)
		return trimWorkspaceSymbols(ranked, limit)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].Location.Path != ranked[j].Location.Path {
			return ranked[i].Location.Path < ranked[j].Location.Path
		}
		if ranked[i].Name != ranked[j].Name {
			return ranked[i].Name < ranked[j].Name
		}
		return ranked[i].Location.Line < ranked[j].Location.Line
	})
	return trimWorkspaceSymbols(ranked, limit)
}

func RankCalls(results []CallHierarchyCall, changedFiles map[string]bool, changedLines map[string]map[int]bool, limit int) []CallHierarchyCall {
	return RankCallsWithReview(results, source.Location{}, legacyReviewIndex{changedFiles: changedFiles, changedLines: changedLines}, diff.ResultOrderReview, limit)
}

func RankCallsWithReview(results []CallHierarchyCall, current source.Location, review diff.ReviewIndex, order diff.ResultOrder, limit int) []CallHierarchyCall {
	ranked := make([]CallHierarchyCall, 0, len(results))
	for i, result := range results {
		result.Score = max(0, 1000-i)
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		result.Score += reviewScore(result.Location, current, result.Review, review)
		ranked = append(ranked, result)
	}
	if order == diff.ResultOrderSource {
		sortCallsBySource(ranked)
		return trimCalls(ranked, limit)
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
	return trimCalls(ranked, limit)
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

func reviewScore(loc, current source.Location, markers diff.ReviewMarkers, review diff.ReviewIndex) int {
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
	if markers.Changed {
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
	if isLikelyTest(loc.Path) {
		score += 2500
	}
	return score
}

func sortDiagnosticsBySource(results []Diagnostic) {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].Location(), results[j].Location()
		return locationLess(a, b, results[i].Message, results[j].Message)
	})
}

func sortWorkspaceSymbolsBySource(results []WorkspaceSymbol) {
	sort.SliceStable(results, func(i, j int) bool {
		return locationLess(results[i].Location, results[j].Location, results[i].Name, results[j].Name)
	})
}

func sortCallsBySource(results []CallHierarchyCall) {
	sort.SliceStable(results, func(i, j int) bool {
		return locationLess(results[i].Location, results[j].Location, results[i].Name, results[j].Name)
	})
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

func isLikelyTest(path string) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, "_test.go") {
		return true
	}
	return strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") ||
		strings.Contains(lower, "/tests/")
}

func trimDiagnostics(results []Diagnostic, limit int) []Diagnostic {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func trimWorkspaceSymbols(results []WorkspaceSymbol, limit int) []WorkspaceSymbol {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func trimCalls(results []CallHierarchyCall, limit int) []CallHierarchyCall {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}
