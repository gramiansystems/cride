package outline

import "cride/internal/lsp"

// EnclosingPath returns matching symbols innermost first.
func EnclosingPath(symbols []lsp.DocumentSymbol, line int) []lsp.DocumentSymbol {
	if line < 1 {
		return nil
	}
	var best []lsp.DocumentSymbol
	var walk func([]lsp.DocumentSymbol, []lsp.DocumentSymbol)
	walk = func(items []lsp.DocumentSymbol, parents []lsp.DocumentSymbol) {
		for _, symbol := range items {
			start, end := symbol.Range.Start.Line, symbol.Range.End.Line
			if start < 1 || end < start || line < start || line > end {
				continue
			}
			path := append(append([]lsp.DocumentSymbol(nil), parents...), symbol)
			if len(path) > len(best) {
				best = path
			}
			walk(symbol.Children, path)
		}
	}
	walk(symbols, nil)
	for left, right := 0, len(best)-1; left < right; left, right = left+1, right-1 {
		best[left], best[right] = best[right], best[left]
	}
	return best
}
