package search

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"cride/internal/diff"
	"cride/internal/source"
)

type SymbolQuery struct {
	Symbol   string
	Location source.Location
	Side     ResultSide
}

type ReferenceKind int

const (
	ReferenceUnknown ReferenceKind = iota
	ReferenceDefinition
	ReferenceReference
)

type ResultSource int

const (
	ResultSourceLSP ResultSource = iota
	ResultSourceTreeSitter
	ResultSourceLexical
)

func (s ResultSource) String() string {
	switch s {
	case ResultSourceLSP:
		return "lsp"
	case ResultSourceTreeSitter:
		return "tree-sitter"
	case ResultSourceLexical:
		return "lexical"
	default:
		return "unknown"
	}
}

type ReferenceResult struct {
	Location source.Location
	Preview  string
	Kind     ReferenceKind
	Source   ResultSource
	Score    int
	Review   diff.ReviewMarkers
	Side     ResultSide
}

type Identifier struct {
	Symbol string
	Column int
}

// ExtractIdentifier expands around a 1-based byte column and returns the
// identifier at that position. If the column is on a selector dot, the
// right-hand identifier is preferred.
func ExtractIdentifier(line string, column int) (symbol string, startColumn int, ok bool) {
	if line == "" {
		return "", 0, false
	}
	if column < 1 {
		column = 1
	}
	idx := column - 1
	if idx >= len(line) {
		idx = len(line) - 1
	}

	if isIdentByte(line[idx]) {
		return identifierAt(line, idx)
	}
	if line[idx] == '.' {
		if idx+1 < len(line) && isIdentByte(line[idx+1]) {
			return identifierAt(line, idx+1)
		}
		if idx > 0 && isIdentByte(line[idx-1]) {
			return identifierAt(line, idx-1)
		}
	}
	return "", 0, false
}

// ExtractNonKeywordIdentifier is ExtractIdentifier restricted to symbols
// worth looking up: common source keywords report !ok.
func ExtractNonKeywordIdentifier(line string, column int) (symbol string, startColumn int, ok bool) {
	symbol, startColumn, ok = ExtractIdentifier(line, column)
	if !ok || commonKeyword(symbol) {
		return "", 0, false
	}
	return symbol, startColumn, true
}

// NonKeywordIdentifiers returns the distinct identifier-looking tokens in line,
// excluding common source keywords.
func NonKeywordIdentifiers(line string) []Identifier {
	identifiers := []Identifier{}
	seen := map[string]bool{}
	for i := 0; i < len(line); i++ {
		if !isIdentStartByte(line[i]) {
			continue
		}
		symbol, column, ok := identifierAt(line, i)
		if !ok {
			continue
		}
		if !commonKeyword(symbol) && !seen[symbol] {
			identifiers = append(identifiers, Identifier{Symbol: symbol, Column: column})
			seen[symbol] = true
		}
		i += len(symbol) - 1
	}
	return identifiers
}

// FirstIdentifier returns the first identifier-looking token in line.
func FirstIdentifier(line string) (symbol string, startColumn int, ok bool) {
	for i := 0; i < len(line); i++ {
		if isIdentStartByte(line[i]) {
			return identifierAt(line, i)
		}
	}
	return "", 0, false
}

// FirstNonKeywordIdentifier returns the first identifier that is not a common
// source keyword. It is used when the UI only has a row cursor and no precise
// column to expand from.
func FirstNonKeywordIdentifier(line string) (symbol string, startColumn int, ok bool) {
	for i := 0; i < len(line); i++ {
		if !isIdentStartByte(line[i]) {
			continue
		}
		symbol, startColumn, ok = identifierAt(line, i)
		if ok && !commonKeyword(symbol) {
			return symbol, startColumn, true
		}
		i += len(symbol) - 1
	}
	return "", 0, false
}

func identifierAt(line string, idx int) (symbol string, startColumn int, ok bool) {
	start := idx
	for start > 0 && isIdentByte(line[start-1]) {
		start--
	}
	end := idx + 1
	for end < len(line) && isIdentByte(line[end]) {
		end++
	}
	if start == end || !isIdentStartByte(line[start]) {
		return "", 0, false
	}
	return line[start:end], start + 1, true
}

func isIdentStartByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isIdentByte(b byte) bool {
	return isIdentStartByte(b) || (b >= '0' && b <= '9')
}

func commonKeyword(s string) bool {
	switch s {
	case "any", "bigint", "bool", "boolean", "break", "byte", "bytes",
		"case", "chan", "class", "comparable", "complex", "complex64",
		"complex128", "const", "continue", "default", "defer", "def",
		"dict", "else", "enum", "error", "fallthrough", "false", "float",
		"float32", "float64", "for", "frozenset", "func", "function", "go",
		"goto", "if", "import", "int", "int8", "int16", "int32", "int64",
		"interface", "let", "list", "map", "never", "nil", "none", "null",
		"number", "object", "package", "range", "return", "rune", "select",
		"set", "str", "string", "struct", "switch", "symbol", "true",
		"tuple", "type", "uint", "uint8", "uint16", "uint32", "uint64",
		"uintptr", "undefined", "unknown", "var", "void":
		return true
	default:
		return false
	}
}

func DefinitionSearchPattern(symbol, path string) string {
	quoted := regexp.QuoteMeta(symbol)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return `\b(func[[:space:]]+(\([^)]*\)[[:space:]]*)?` + quoted + `[[:space:]]*\(|type[[:space:]]+` + quoted + `\b|var[[:space:]]+` + quoted + `\b|const[[:space:]]+` + quoted + `\b|` + quoted + `[[:space:]]*:=)`
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".inl", ".ipp", ".tpp":
		return cDefinitionSearchPattern(quoted)
	default:
		return `\b(class|function|def|interface|struct|enum|const|let|var|type)[[:space:]]+` + quoted + `\b|` + quoted + `[[:space:]]*:=`
	}
}

func cDefinitionSearchPattern(quoted string) string {
	typeDecl := `\b(class|struct|union|enum|namespace)[[:space:]]+` + quoted + `\b`
	aliasDecl := `\b(typedef|using)[^;{}]*\b` + quoted + `\b`
	functionPointer := `\([[:space:]]*\*[[:space:]]*` + quoted + `[[:space:]]*\)[[:space:]]*\(`
	qualified := `([A-Za-z_][A-Za-z0-9_]*::)*`
	typedName := `(^[[:space:]]*|[;{}][[:space:]]*)([A-Za-z_][A-Za-z0-9_:<>,]*[[:space:]*&]+)+` + qualified + quoted
	functionDecl := typedName + `[[:space:]]*\(`
	variableDecl := typedName + `[[:space:]]*(=|;|\[)`
	return `(` + typeDecl + `|` + aliasDecl + `|` + functionPointer + `|` + functionDecl + `|` + variableDecl + `)`
}

func ReferenceResultsFromTextResults(symbol string, results []Result, source ResultSource) []ReferenceResult {
	out := make([]ReferenceResult, 0, len(results))
	for _, result := range results {
		out = append(out, ReferenceResult{
			Location: result.Location,
			Preview:  result.Preview,
			Kind:     ClassifyReferenceKind(result.Preview, result.Location.Path, symbol),
			Source:   source,
			Side:     result.Side,
		})
	}
	return out
}

func DefinitionResultsFromTextResults(symbol string, results []Result, source ResultSource) []ReferenceResult {
	out := make([]ReferenceResult, 0, len(results))
	for _, result := range results {
		if !LooksLikeDefinition(result.Preview, result.Location.Path, symbol) {
			continue
		}
		out = append(out, ReferenceResult{
			Location: result.Location,
			Preview:  result.Preview,
			Kind:     ReferenceDefinition,
			Source:   source,
			Side:     result.Side,
		})
	}
	return out
}

// VTableSlotsForImplementation finds designated C/C++ dispatch-table
// bindings such as ".draw = widget_draw" among references to widget_draw.
// Callers can then include possible references to draw, covering indirect
// calls that language-server call graphs generally cannot connect back to the
// concrete implementation.
func VTableSlotsForImplementation(symbol string, results []ReferenceResult) []string {
	if symbol == "" {
		return nil
	}
	pattern := regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=[[:space:]]*&?(?:[A-Za-z_][A-Za-z0-9_]*::)*` + regexp.QuoteMeta(symbol) + `\b`)
	seen := make(map[string]bool)
	var slots []string
	for _, result := range results {
		for _, match := range pattern.FindAllStringSubmatch(result.Preview, -1) {
			if len(match) < 2 || seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			slots = append(slots, match[1])
		}
	}
	sort.Strings(slots)
	return slots
}

func ClassifyReferenceKind(preview, path, symbol string) ReferenceKind {
	if LooksLikeDefinition(preview, path, symbol) {
		return ReferenceDefinition
	}
	return ReferenceReference
}

func LooksLikeDefinition(preview, path, symbol string) bool {
	if symbol == "" {
		return false
	}
	if isCFamilyPath(path) {
		trimmed := strings.TrimSpace(preview)
		for _, prefix := range []string{"return ", "co_return ", "throw ", "case ", "new ", "delete "} {
			if strings.HasPrefix(trimmed, prefix) {
				return false
			}
		}
	}
	pattern := DefinitionSearchPattern(symbol, path)
	ok, err := regexp.MatchString(pattern, preview)
	return err == nil && ok
}

func isCFamilyPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".inl", ".ipp", ".tpp":
		return true
	default:
		return false
	}
}

func RankReferenceResults(results []ReferenceResult, currentPath string, changedFiles map[string]bool, changedLines map[string]map[int]bool, limit int) []ReferenceResult {
	return RankReferenceResultsWithReview(results, source.Location{Path: currentPath}, legacyReviewIndex{changedFiles: changedFiles, changedLines: changedLines}, diff.ResultOrderReview, limit)
}

func RankReferenceResultsWithReview(results []ReferenceResult, current source.Location, review diff.ReviewIndex, order diff.ResultOrder, limit int) []ReferenceResult {
	ranked := make([]ReferenceResult, 0, len(results))
	for i, result := range results {
		result.Score = max(0, 1000-i)
		result.Review = diff.MarkersForIndex(review, result.Location.Path, result.Location.Line)
		result.Score += reviewScore(result.Location, current, result.Review, review, result.Kind == ReferenceDefinition)
		ranked = append(ranked, result)
	}

	if order == diff.ResultOrderSource {
		sortReferenceResultsBySource(ranked)
		return trimReferenceResults(ranked, limit)
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
	return trimReferenceResults(ranked, limit)
}

func sortReferenceResultsBySource(results []ReferenceResult) {
	sort.SliceStable(results, func(i, j int) bool {
		return locationLess(results[i].Location, results[j].Location, ReferenceResultLabel(results[i]), ReferenceResultLabel(results[j]))
	})
}

func ReferenceResultLabel(result ReferenceResult) string {
	return fmt.Sprintf("%s:%d:%d", result.Location.Path, result.Location.Line, result.Location.Column)
}

func trimReferenceResults(results []ReferenceResult, limit int) []ReferenceResult {
	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}
