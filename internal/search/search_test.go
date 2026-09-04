package search

import (
	"strings"
	"testing"

	"cride/internal/diff"
	"cride/internal/source"
)

func TestParseRipgrepLine(t *testing.T) {
	t.Parallel()

	got, ok := ParseRipgrepLine("internal/app/app.go:42:7:return needle")
	if !ok {
		t.Fatal("ParseRipgrepLine returned false")
	}
	if got.Kind != ResultText {
		t.Fatalf("Kind = %v, want ResultText", got.Kind)
	}
	if got.Location.Path != "internal/app/app.go" || got.Location.Line != 42 || got.Location.Column != 7 {
		t.Fatalf("Location = %+v", got.Location)
	}
	if got.Label != "internal/app/app.go:42:7" {
		t.Fatalf("Label = %q", got.Label)
	}
	if got.Preview != "return needle" {
		t.Fatalf("Preview = %q", got.Preview)
	}
}

func TestParseRipgrepLineAllowsColonsInPreview(t *testing.T) {
	t.Parallel()

	got, ok := ParseRipgrepLine("./README.md:3:12:value: still preview")
	if !ok {
		t.Fatal("ParseRipgrepLine returned false")
	}
	if got.Location.Path != "README.md" {
		t.Fatalf("Path = %q, want README.md", got.Location.Path)
	}
	if got.Preview != "value: still preview" {
		t.Fatalf("Preview = %q", got.Preview)
	}
}

func TestRankFilesPrefersChangedFilesWhenFuzzyScoreIsSimilar(t *testing.T) {
	t.Parallel()

	results := RankFiles(
		[]string{"internal/search.go", "internal/source.go"},
		"sr",
		map[string]bool{"internal/source.go": true},
		nil,
		10,
	)
	if len(results) < 2 {
		t.Fatalf("len(results) = %d, want at least 2", len(results))
	}
	if results[0].Label != "internal/source.go" {
		t.Fatalf("top result = %q, want changed internal/source.go; all=%+v", results[0].Label, results)
	}
}

func TestFuzzyScoreIgnoresIdentifierSeparators(t *testing.T) {
	t.Parallel()

	for _, candidate := range []string{"xose_Gateway.go", "xoseGateway.go", "xose-gateway.go"} {
		if _, ok := FuzzyScore(candidate, "xose gateway"); !ok {
			t.Fatalf("FuzzyScore(%q, %q) did not match", candidate, "xose gateway")
		}
	}
}

func TestCompactQueryPreservesCaseWhileRemovingSeparators(t *testing.T) {
	t.Parallel()

	if got := CompactQuery(" Xose_Gateway.go "); got != "XoseGatewaygo" {
		t.Fatalf("CompactQuery = %q, want XoseGatewaygo", got)
	}
}

func TestRankSymbolsUsesUnadornedSearchText(t *testing.T) {
	t.Parallel()

	results := RankSymbols([]Result{
		{Label: "[function] Other  xose_gateway.go:2:1", SearchText: "Other"},
		{Label: "[function] xose_Gateway  other.go:2:1", SearchText: "xose_Gateway"},
	}, "xose gateway", 10)
	if len(results) != 1 || results[0].SearchText != "xose_Gateway" {
		t.Fatalf("RankSymbols = %+v, want only xose_Gateway", results)
	}
}

func TestRankSymbolsPrefersSemanticKindThenDefinitions(t *testing.T) {
	t.Parallel()

	results := RankSymbols([]Result{
		{SearchText: "TargetVariable", SymbolCategory: SymbolCategoryVariable, Reference: ReferenceDefinition},
		{SearchText: "TargetFunctionUsage", SymbolCategory: SymbolCategoryFunction, Reference: ReferenceReference},
		{SearchText: "TargetTypeUsage", SymbolCategory: SymbolCategoryType, Reference: ReferenceReference},
		{SearchText: "TargetFunction", SymbolCategory: SymbolCategoryFunction, Reference: ReferenceDefinition},
		{SearchText: "TargetType", SymbolCategory: SymbolCategoryType, Reference: ReferenceDefinition},
	}, "target", 10)

	want := []string{"TargetType", "TargetTypeUsage", "TargetFunction", "TargetFunctionUsage", "TargetVariable"}
	if len(results) != len(want) {
		t.Fatalf("RankSymbols returned %d results, want %d: %+v", len(results), len(want), results)
	}
	for i, name := range want {
		if results[i].SearchText != name {
			t.Fatalf("RankSymbols[%d] = %q, want %q; all=%+v", i, results[i].SearchText, name, results)
		}
	}
}

func TestSymbolScoreIsCaseInsensitiveButPrefersMatchingCase(t *testing.T) {
	t.Parallel()

	upperScore, ok := SymbolScore("XoseGateway", "xose gateway")
	if !ok {
		t.Fatal("xose gateway did not match XoseGateway")
	}
	matchingScore, ok := SymbolScore("xoseGateway", "xose gateway")
	if !ok {
		t.Fatal("xose gateway did not match xoseGateway")
	}
	if matchingScore <= upperScore {
		t.Fatalf("case-aligned score %d <= case-insensitive score %d", matchingScore, upperScore)
	}
}

func TestQuerySeedUsesLongestIdentifierTerm(t *testing.T) {
	t.Parallel()

	if got := QuerySeed("xose gateway"); got != "gateway" {
		t.Fatalf("QuerySeed = %q, want gateway", got)
	}
	if got := QuerySeed("xose_Gateway"); got != "xose_Gateway" {
		t.Fatalf("underscored QuerySeed = %q, want xose_Gateway", got)
	}
}

func TestRankGrepResultsScoresRelevanceAndDropsBaselineOnlyRows(t *testing.T) {
	t.Parallel()

	results := RankGrepResults([]Result{
		{Location: source.Location{Path: "loose.go", Line: 4, Column: 1}, Preview: "t x a x r x g x e x t", Side: ResultSideCurrent},
		{Location: source.Location{Path: "exact.go", Line: 2, Column: 6}, Preview: "func Target() {}", Side: ResultSideCurrent},
		{Location: source.Location{Path: "deleted.go", Line: 1, Column: 1}, Preview: "Target", Side: ResultSideBaseline},
	}, "target", source.Location{}, nil, nil, 10)

	if len(results) != 2 {
		t.Fatalf("RankGrepResults = %+v, want two current-side rows", results)
	}
	if results[0].Location.Path != "exact.go" || results[0].Group != ResultGroupGrep {
		t.Fatalf("top grep result = %+v, want exact.go tagged as grep", results[0])
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("grep scores = %d, %d; want exact match first", results[0].Score, results[1].Score)
	}
}

func TestRankTextResultsPrefersChangedHunkThenChangedFile(t *testing.T) {
	t.Parallel()

	results := []Result{
		textResult("other.go", 2),
		textResult("changed.go", 8),
		textResult("changed.go", 3),
	}
	ranked := RankTextResults(results, map[string]bool{"changed.go": true}, map[string]map[int]bool{
		"changed.go": {3: true},
	}, 10)

	if ranked[0].Location.Path != "changed.go" || ranked[0].Location.Line != 3 {
		t.Fatalf("top result = %+v, want changed hunk line", ranked[0].Location)
	}
	if ranked[1].Location.Path != "changed.go" || ranked[1].Location.Line != 8 {
		t.Fatalf("second result = %+v, want changed file match", ranked[1].Location)
	}
}

func TestRankTextResultsCanUseSourceOrder(t *testing.T) {
	t.Parallel()

	results := []Result{
		textResult("z.go", 20),
		textResult("a.go", 8),
		textResult("a.go", 3),
	}
	ranked := RankTextResultsWithReview(results, source.Location{}, diff.NewReviewIndex(nil), diff.ResultOrderSource, 10)

	if ranked[0].Location.Path != "a.go" || ranked[0].Location.Line != 3 {
		t.Fatalf("top source result = %+v, want a.go:3", ranked[0].Location)
	}
	if ranked[1].Location.Path != "a.go" || ranked[1].Location.Line != 8 {
		t.Fatalf("second source result = %+v, want a.go:8", ranked[1].Location)
	}
}

func TestRankTextResultsAttachesReviewMarkers(t *testing.T) {
	t.Parallel()

	review := diff.NewReviewIndex([]diff.FileDiff{{
		NewPath: "changed.go",
		Status:  diff.FileModified,
		Hunks: []diff.Hunk{{
			Lines: []diff.Line{{Kind: diff.LineAdd, NewLine: 3}},
		}},
	}})
	ranked := RankTextResultsWithReview([]Result{textResult("changed.go", 3)}, source.Location{}, review, diff.ResultOrderReview, 10)
	if len(ranked) != 1 {
		t.Fatalf("ranked results = %d, want 1", len(ranked))
	}
	if ranked[0].Review.ChangeKind != diff.ChangeAdded || !ranked[0].Review.Changed {
		t.Fatalf("review markers = %+v, want added changed line", ranked[0].Review)
	}
}

func TestExtractIdentifierHandlesGoIdentifiersAndSelectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		line       string
		column     int
		wantSymbol string
		wantColumn int
	}{
		{
			name:       "identifier",
			line:       "value_name := call()",
			column:     4,
			wantSymbol: "value_name",
			wantColumn: 1,
		},
		{
			name:       "selector right side",
			line:       "pkg.Foo(bar)",
			column:     5,
			wantSymbol: "Foo",
			wantColumn: 5,
		},
		{
			name:       "selector dot prefers right side",
			line:       "pkg.Foo(bar)",
			column:     4,
			wantSymbol: "Foo",
			wantColumn: 5,
		},
		{
			name:       "digits after first character",
			line:       "value2 := 1",
			column:     6,
			wantSymbol: "value2",
			wantColumn: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, col, ok := ExtractIdentifier(tt.line, tt.column)
			if !ok {
				t.Fatal("ExtractIdentifier returned false")
			}
			if got != tt.wantSymbol || col != tt.wantColumn {
				t.Fatalf("ExtractIdentifier = %q, %d; want %q, %d", got, col, tt.wantSymbol, tt.wantColumn)
			}
		})
	}
}

func TestNonKeywordIdentifiersReturnsDistinctSourceTokens(t *testing.T) {
	t.Parallel()

	got := NonKeywordIdentifiers("return Alpha(Beta, Alpha)")
	if len(got) != 2 {
		t.Fatalf("len(identifiers) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Symbol != "Alpha" || got[0].Column != 8 {
		t.Fatalf("first identifier = %+v, want Alpha at column 8", got[0])
	}
	if got[1].Symbol != "Beta" || got[1].Column != 14 {
		t.Fatalf("second identifier = %+v, want Beta at column 14", got[1])
	}
}

func TestNonKeywordIdentifiersSkipsBuiltInTypes(t *testing.T) {
	t.Parallel()

	got := NonKeywordIdentifiers("var name string = int(count)")
	if len(got) != 2 {
		t.Fatalf("len(identifiers) = %d, want 2: %+v", len(got), got)
	}
	if got[0].Symbol != "name" || got[0].Column != 5 {
		t.Fatalf("first identifier = %+v, want name at column 5", got[0])
	}
	if got[1].Symbol != "count" || got[1].Column != 23 {
		t.Fatalf("second identifier = %+v, want count at column 23", got[1])
	}
}

func TestFirstNonKeywordIdentifierSkipsBuiltInTypes(t *testing.T) {
	t.Parallel()

	got, col, ok := FirstNonKeywordIdentifier("string int Target")
	if !ok {
		t.Fatal("FirstNonKeywordIdentifier returned false")
	}
	if got != "Target" || col != 12 {
		t.Fatalf("FirstNonKeywordIdentifier = %q, %d; want Target, 12", got, col)
	}
}

func TestReferenceResultsFromLexicalSearchClassifiesDefinitions(t *testing.T) {
	t.Parallel()

	results := ReferenceResultsFromTextResults("Target", []Result{
		{
			Location: source.Location{Path: "a.go", Line: 3, Column: 6},
			Preview:  "func Target() {}",
		},
		{
			Location: source.Location{Path: "a.go", Line: 8, Column: 9},
			Preview:  "return Target()",
		},
	}, ResultSourceLexical)

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Kind != ReferenceDefinition {
		t.Fatalf("first kind = %v, want ReferenceDefinition", results[0].Kind)
	}
	if results[1].Kind != ReferenceReference {
		t.Fatalf("second kind = %v, want ReferenceReference", results[1].Kind)
	}
	if results[0].Source != ResultSourceLexical {
		t.Fatalf("source = %v, want ResultSourceLexical", results[0].Source)
	}
}

func TestCppDefinitionsIncludeFunctionsStructsAndVTableSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		symbol  string
		preview string
		want    bool
	}{
		{"Widget", "struct Widget {", true},
		{"draw", "    int (*draw)(Widget *, int);", true},
		{"widget_draw", "static int widget_draw(Widget *self, int x) {", true},
		{"widget_draw", "    static int widget_draw(Widget *self, int x) {", true},
		{"widget_vtable", "static const WidgetVTable widget_vtable = {", true},
		{"widget_draw", "    .draw = widget_draw,", false},
		{"widget_draw", "return widget_draw(self, 1);", false},
	}
	for _, tt := range tests {
		if got := LooksLikeDefinition(tt.preview, "widget.cpp", tt.symbol); got != tt.want {
			t.Errorf("LooksLikeDefinition(%q, %q) = %v, want %v; pattern %q", tt.preview, tt.symbol, got, tt.want, DefinitionSearchPattern(tt.symbol, "widget.cpp"))
		}
	}
}

func TestVTableSlotsForImplementation(t *testing.T) {
	t.Parallel()

	results := []ReferenceResult{
		{Preview: "    .draw = widget_draw,"},
		{Preview: "    .destroy = &detail::widget_draw,"},
		{Preview: "return widget_draw(self, 1);"},
		{Preview: "    .draw = widget_draw,"},
	}
	got := VTableSlotsForImplementation("widget_draw", results)
	if strings.Join(got, ",") != "destroy,draw" {
		t.Fatalf("slots = %v, want destroy,draw", got)
	}
}

func TestRankReferenceResultsPrefersCurrentAndChangedFiles(t *testing.T) {
	t.Parallel()

	results := []ReferenceResult{
		{Location: source.Location{Path: "other.go", Line: 1, Column: 1}},
		{Location: source.Location{Path: "changed.go", Line: 8, Column: 1}},
		{Location: source.Location{Path: "current.go", Line: 3, Column: 1}},
	}
	ranked := RankReferenceResults(results, "current.go", map[string]bool{"changed.go": true}, nil, 10)

	if ranked[0].Location.Path != "current.go" {
		t.Fatalf("top result = %+v, want current.go", ranked[0].Location)
	}
	if ranked[1].Location.Path != "changed.go" {
		t.Fatalf("second result = %+v, want changed.go", ranked[1].Location)
	}
}

func textResult(path string, line int) Result {
	return Result{
		Kind:     ResultText,
		Location: source.Location{Path: path, Line: line, Column: 1},
		Label:    path,
	}
}
