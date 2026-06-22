// Package outline extracts source structure and relates it to a review diff.
// The lexical path deliberately favors useful partial results over rejecting
// files that are temporarily invalid while an agent is editing them.
package outline

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"cride/internal/diffsource"
	"cride/internal/lsp"
	"cride/internal/source"
)

// Extractor produces range-annotated symbols for one version of a file.
type Extractor interface {
	Symbols(path string, content []byte) ([]lsp.DocumentSymbol, error)
}

// LexicalExtractor recognizes common declaration forms without requiring a
// language server. It is intentionally best-effort and safe for broken files.
type LexicalExtractor struct{}

type candidate struct {
	symbol lsp.DocumentSymbol
	line   int
	level  int
}

var (
	goFunc = regexp.MustCompile(`^\s*func\s+(?:\(([^)]*)\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`)
	goType = regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s*(struct|interface)?`)

	pyDef   = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pyClass = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)

	jsFunction = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsType     = regexp.MustCompile(`^\s*(?:export\s+)?(?:default\s+)?(class|interface|enum|type|namespace)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)
	jsArrow    = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=.*=>`)
	jsMethod   = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|abstract|readonly|override|declare)\s+)*(?:async\s+)?(?:get\s+|set\s+)?([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

	rustFn   = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:unsafe\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustType = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(struct|enum|trait|union|type|mod)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustImpl = regexp.MustCompile(`^\s*impl(?:<[^>]*>)?\s+([^\s{]+(?:\s+for\s+[^\s{]+)?)`)

	cType            = regexp.MustCompile(`^\s*(?:typedef\s+)?(struct|class|union|enum(?:\s+class)?)[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)?`)
	cNamespace       = regexp.MustCompile(`^\s*namespace\s+([A-Za-z_][A-Za-z0-9_]*)`)
	cFunctionPointer = regexp.MustCompile(`\([[:space:]]*\*[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\)[[:space:]]*\(`)
	cFunction        = regexp.MustCompile(`^\s*(?:(?:template\s*<[^;{}]*>)\s*)?(?:[A-Za-z_][A-Za-z0-9_:<>,]*[[:space:]*&]+)+((?:[A-Za-z_][A-Za-z0-9_]*::)*~?[A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\(`)
	cMemberFunction  = regexp.MustCompile(`^\s*(?:(?:explicit|constexpr|consteval|inline|static|virtual|friend)\s+)*(~?[A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\(`)
	cTypedefAlias    = regexp.MustCompile(`^\s*}[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*;`)
	cVTableObject    = regexp.MustCompile(`^\s*(?:(?:static|const|constexpr|inline|extern)\s+)*(?:struct\s+)?([A-Za-z_][A-Za-z0-9_:]*)(?:\s+const)?\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*{`)
	cVTableBinding   = regexp.MustCompile(`(?:^|[,{}])\s*\.?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*&?([A-Za-z_][A-Za-z0-9_:]*)`)
)

// Symbols returns a nested, source-ordered outline. Unknown file types simply
// return no symbols.
func (LexicalExtractor) Symbols(path string, content []byte) ([]lsp.DocumentSymbol, error) {
	if len(content) == 0 || len(content) > diffsource.MaxContentBytes || bytes.IndexByte(content, 0) >= 0 {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	ext := strings.ToLower(filepath.Ext(path))
	var candidates []candidate
	switch ext {
	case ".go":
		candidates = goCandidates(path, lines)
	case ".py", ".pyi":
		candidates = pythonCandidates(path, lines)
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		candidates = javascriptCandidates(path, lines)
	case ".rs":
		candidates = rustCandidates(path, lines)
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".c++", ".hh", ".hpp", ".hxx", ".h++", ".inl", ".ipp", ".tpp":
		candidates = cCandidates(path, lines)
	default:
		return nil, nil
	}
	finishRanges(candidates, path, lines)
	return nestCandidates(candidates), nil
}

type cScope struct {
	bodyDepth int
	kind      string
	candidate int
	name      string
}

func cCandidates(path string, lines []string) []candidate {
	var (
		out           []candidate
		scopes        []cScope
		depth         int
		dispatchTypes = make(map[string]bool)
	)
	for i, line := range lines {
		clean := stripLineComment(line, "//")
		delta := braceDelta(clean)

		// Resolve "typedef struct { ... } Name;" once the trailing alias is
		// visible. Keeping the opening range makes breadcrumbs cover the body.
		if match := cTypedefAlias.FindStringSubmatchIndex(clean); match != nil {
			for j := len(scopes) - 1; j >= 0; j-- {
				scope := scopes[j]
				if scope.kind != "anonymous-type" || scope.candidate < 0 || scope.candidate >= len(out) {
					continue
				}
				nameStart, nameEnd := match[2], match[3]
				out[scope.candidate].symbol.Name = clean[nameStart:nameEnd]
				out[scope.candidate].symbol.SelectionRange = source.Range{
					Start: source.Location{Path: path, Line: i + 1, Column: nameStart + 1},
					End:   source.Location{Path: path, Line: i + 1, Column: nameEnd + 1},
				}
				scopes[j].kind = "type"
				scopes[j].name = clean[nameStart:nameEnd]
				break
			}
		}

		inFunction := cHasScope(scopes, "function")
		inType := cHasTypeScope(scopes)
		inVTable := cHasScope(scopes, "vtable")
		declaredTypeName := ""
		vtableObjectLine := false

		if !inFunction && !inVTable {
			if match := cNamespace.FindStringSubmatchIndex(clean); match != nil {
				out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolNamespace, ""))
				if delta > 0 {
					scopes = append(scopes, cScope{bodyDepth: depth + 1, kind: "namespace", candidate: len(out) - 1, name: clean[match[2]:match[3]]})
				}
			} else if match := cType.FindStringSubmatchIndex(clean); match != nil {
				keyword := strings.Join(strings.Fields(clean[match[2]:match[3]]), " ")
				kind := cTypeSymbolKind(keyword)
				nameStart, nameEnd := match[4], match[5]
				scopeKind := "type"
				if nameStart < 0 {
					nameStart, nameEnd = match[2], match[3]
					scopeKind = "anonymous-type"
				}
				out = append(out, makeCandidate(path, lines, i, nameStart, nameEnd, depth, kind, ""))
				if scopeKind == "anonymous-type" {
					out[len(out)-1].symbol.Name = "(anonymous " + keyword + ")"
				}
				declaredTypeName = out[len(out)-1].symbol.Name
				if delta > 0 {
					scopes = append(scopes, cScope{bodyDepth: depth + 1, kind: scopeKind, candidate: len(out) - 1, name: out[len(out)-1].symbol.Name})
				}
			} else if match := cVTableObject.FindStringSubmatchIndex(clean); match != nil {
				typeName := clean[match[2]:match[3]]
				if isVTableType(typeName) || dispatchTypes[cBaseTypeName(typeName)] {
					out = append(out, makeCandidate(path, lines, i, match[4], match[5], depth, lsp.SymbolObject, typeName+" vtable"))
					vtableObjectLine = true
					if delta > 0 {
						scopes = append(scopes, cScope{bodyDepth: depth + 1, kind: "vtable", candidate: len(out) - 1})
					}
				}
			}
		}

		// Function-pointer members are the virtual methods in C-style object
		// systems. Model every such member as a method; this stays useful even
		// when a project calls its dispatch table something other than VTable.
		if (inType || declaredTypeName != "") && !inFunction {
			matches := cFunctionPointer.FindAllStringSubmatchIndex(clean, -1)
			if len(matches) > 0 {
				typeName := declaredTypeName
				if typeName == "" {
					typeName = cNearestTypeName(scopes)
				}
				if typeName != "" {
					dispatchTypes[typeName] = true
				}
			}
			for _, match := range matches {
				out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolMethod, "vtable slot"))
			}
		}

		if inVTable || vtableObjectLine {
			for _, match := range cVTableBinding.FindAllStringSubmatchIndex(clean, -1) {
				detail := "→ " + clean[match[4]:match[5]]
				out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolMethod, detail))
			}
		}

		if !inFunction && !inVTable {
			match := cFunction.FindStringSubmatchIndex(clean)
			if match == nil && inType {
				match = cMemberFunction.FindStringSubmatchIndex(clean)
			}
			if match != nil && !cNonDeclarationPrefix(clean) {
				nameStart, nameEnd, detail := cFunctionName(clean, match[2], match[3])
				kind := lsp.SymbolFunction
				if inType || detail != "" {
					kind = lsp.SymbolMethod
				}
				out = append(out, makeCandidate(path, lines, i, nameStart, nameEnd, depth, kind, detail))
				if delta > 0 {
					scopes = append(scopes, cScope{bodyDepth: depth + 1, kind: "function", candidate: len(out) - 1})
				}
			}
		}

		depth = max(0, depth+delta)
		for len(scopes) > 0 && depth < scopes[len(scopes)-1].bodyDepth {
			scopes = scopes[:len(scopes)-1]
		}
	}
	return out
}

func cHasScope(scopes []cScope, kind string) bool {
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i].kind == kind {
			return true
		}
	}
	return false
}

func cHasTypeScope(scopes []cScope) bool {
	return cHasScope(scopes, "type") || cHasScope(scopes, "anonymous-type")
}

func cNearestTypeName(scopes []cScope) string {
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i].kind == "type" || scopes[i].kind == "anonymous-type" {
			return scopes[i].name
		}
	}
	return ""
}

func cBaseTypeName(name string) string {
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		return name[idx+2:]
	}
	return name
}

func cFunctionName(line string, start, end int) (int, int, string) {
	raw := line[start:end]
	detail := ""
	if idx := strings.LastIndex(raw, "::"); idx >= 0 {
		detail = raw[:idx+2]
		start += idx + 2
	}
	if start < end && line[start] == '~' {
		start++
	}
	return start, end, detail
}

func cNonDeclarationPrefix(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"return ", "co_return ", "throw ", "case ", "new ", "delete "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func cTypeSymbolKind(keyword string) lsp.SymbolKind {
	switch keyword {
	case "struct":
		return lsp.SymbolStruct
	case "enum", "enum class":
		return lsp.SymbolEnum
	case "class":
		return lsp.SymbolClass
	default:
		return lsp.SymbolClass
	}
}

func isVTableType(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, "_", ""))
	return strings.Contains(name, "vtable") || strings.Contains(name, "vtbl") ||
		strings.HasSuffix(name, "ops") || strings.HasSuffix(name, "operations") ||
		strings.HasSuffix(name, "methods") || strings.HasSuffix(name, "iface")
}

func goCandidates(path string, lines []string) []candidate {
	var out []candidate
	for i, line := range lines {
		if match := goFunc.FindStringSubmatchIndex(line); match != nil {
			nameGroup := 2
			kind := lsp.SymbolFunction
			detail := ""
			if match[2] >= 0 {
				kind = lsp.SymbolMethod
				detail = strings.TrimSpace(line[match[2]:match[3]])
			}
			out = append(out, makeCandidate(path, lines, i, match[nameGroup*2], match[nameGroup*2+1], 0, kind, detail))
			continue
		}
		if match := goType.FindStringSubmatchIndex(line); match != nil {
			kind := lsp.SymbolClass
			if match[4] >= 0 {
				switch line[match[4]:match[5]] {
				case "struct":
					kind = lsp.SymbolStruct
				case "interface":
					kind = lsp.SymbolInterface
				}
			}
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], 0, kind, ""))
		}
	}
	return out
}

func pythonCandidates(path string, lines []string) []candidate {
	var out []candidate
	for i, line := range lines {
		indent := indentation(line)
		if match := pyDef.FindStringSubmatchIndex(line); match != nil {
			kind := lsp.SymbolFunction
			if indent > 0 {
				kind = lsp.SymbolMethod
			}
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], indent, kind, ""))
			continue
		}
		if match := pyClass.FindStringSubmatchIndex(line); match != nil {
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], indent, lsp.SymbolClass, ""))
		}
	}
	return out
}

func javascriptCandidates(path string, lines []string) []candidate {
	var out []candidate
	depth := 0
	for i, line := range lines {
		clean := stripLineComment(line, "//")
		if match := jsFunction.FindStringSubmatchIndex(clean); match != nil {
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolFunction, ""))
		} else if match := jsType.FindStringSubmatchIndex(clean); match != nil {
			kind := jsSymbolKind(clean[match[2]:match[3]])
			out = append(out, makeCandidate(path, lines, i, match[4], match[5], depth, kind, ""))
		} else if match := jsArrow.FindStringSubmatchIndex(clean); match != nil {
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolFunction, ""))
		} else if depth > 0 {
			if match := jsMethod.FindStringSubmatchIndex(clean); match != nil {
				name := clean[match[2]:match[3]]
				if !isControlWord(name) {
					kind := lsp.SymbolMethod
					if name == "constructor" {
						kind = lsp.SymbolConstructor
					}
					out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, kind, ""))
				}
			}
		}
		depth = max(0, depth+braceDelta(clean))
	}
	return out
}

func rustCandidates(path string, lines []string) []candidate {
	var out []candidate
	depth := 0
	for i, line := range lines {
		clean := stripLineComment(line, "//")
		if match := rustFn.FindStringSubmatchIndex(clean); match != nil {
			kind := lsp.SymbolFunction
			if depth > 0 {
				kind = lsp.SymbolMethod
			}
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, kind, ""))
		} else if match := rustType.FindStringSubmatchIndex(clean); match != nil {
			kind := rustSymbolKind(clean[match[2]:match[3]])
			out = append(out, makeCandidate(path, lines, i, match[4], match[5], depth, kind, ""))
		} else if match := rustImpl.FindStringSubmatchIndex(clean); match != nil {
			out = append(out, makeCandidate(path, lines, i, match[2], match[3], depth, lsp.SymbolClass, "impl"))
		}
		depth = max(0, depth+braceDelta(clean))
	}
	return out
}

func makeCandidate(path string, lines []string, line, nameStart, nameEnd, level int, kind lsp.SymbolKind, detail string) candidate {
	name := ""
	if line >= 0 && line < len(lines) && nameStart >= 0 && nameEnd <= len(lines[line]) {
		name = lines[line][nameStart:nameEnd]
	}
	loc := source.Location{Path: path, Line: line + 1, Column: nameStart + 1}
	return candidate{
		line:  line,
		level: level,
		symbol: lsp.DocumentSymbol{
			Name:           name,
			Detail:         detail,
			Kind:           kind,
			Range:          source.Range{Start: source.Location{Path: path, Line: line + 1, Column: indentation(lines[line]) + 1}},
			SelectionRange: source.Range{Start: loc, End: source.Location{Path: path, Line: line + 1, Column: nameEnd + 1}},
		},
	}
}

func finishRanges(items []candidate, path string, lines []string) {
	for i := range items {
		end := len(lines)
		for j := i + 1; j < len(items); j++ {
			if items[j].level <= items[i].level {
				end = items[j].line
				break
			}
		}
		if end < items[i].line+1 {
			end = items[i].line + 1
		}
		endCol := 1
		if end > 0 && end <= len(lines) {
			endCol = len(lines[end-1]) + 1
		}
		items[i].symbol.Range.End = source.Location{Path: path, Line: end, Column: endCol}
	}
}

func nestCandidates(items []candidate) []lsp.DocumentSymbol {
	type node struct {
		symbol   lsp.DocumentSymbol
		children []*node
	}
	type entry struct {
		level int
		node  *node
	}
	var roots []*node
	var stack []entry
	for _, item := range items {
		for len(stack) > 0 && stack[len(stack)-1].level >= item.level {
			stack = stack[:len(stack)-1]
		}
		next := &node{symbol: item.symbol}
		if len(stack) == 0 {
			roots = append(roots, next)
		} else {
			parent := stack[len(stack)-1].node
			parent.children = append(parent.children, next)
		}
		stack = append(stack, entry{level: item.level, node: next})
	}
	var convert func(*node) lsp.DocumentSymbol
	convert = func(n *node) lsp.DocumentSymbol {
		symbol := n.symbol
		for _, child := range n.children {
			symbol.Children = append(symbol.Children, convert(child))
		}
		return symbol
	}
	out := make([]lsp.DocumentSymbol, 0, len(roots))
	for _, root := range roots {
		out = append(out, convert(root))
	}
	return out
}

func indentation(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func braceDelta(line string) int {
	delta := 0
	inSingle, inDouble, escaped := false, false, false
	for _, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && (inSingle || inDouble) {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '{':
			if !inSingle && !inDouble {
				delta++
			}
		case '}':
			if !inSingle && !inDouble {
				delta--
			}
		}
	}
	return delta
}

func stripLineComment(line, marker string) string {
	if idx := strings.Index(line, marker); idx >= 0 {
		return line[:idx]
	}
	return line
}

func jsSymbolKind(keyword string) lsp.SymbolKind {
	switch keyword {
	case "class":
		return lsp.SymbolClass
	case "interface":
		return lsp.SymbolInterface
	case "enum":
		return lsp.SymbolEnum
	case "namespace":
		return lsp.SymbolNamespace
	default:
		return lsp.SymbolClass
	}
}

func rustSymbolKind(keyword string) lsp.SymbolKind {
	switch keyword {
	case "struct":
		return lsp.SymbolStruct
	case "enum":
		return lsp.SymbolEnum
	case "trait":
		return lsp.SymbolInterface
	case "mod":
		return lsp.SymbolModule
	default:
		return lsp.SymbolClass
	}
}

func isControlWord(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "catch", "with":
		return true
	default:
		return false
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
