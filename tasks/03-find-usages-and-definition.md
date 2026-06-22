# Syntax-aware navigation fallback

> Status: LSP and lexical ripgrep definition/reference lookup are implemented.
> This task tracks a possible syntax-aware tier between them.

## Problem

`gd` and `gr` already prefer a language server and fall back to whole-word
project search with language-aware definition classification. That fallback is
fast and works on broken source, but it cannot reliably distinguish symbols
that share a spelling.

A lightweight syntax index could improve results when a language server is
missing or unhealthy without making basic review depend on semantic tooling.

## Current behavior

- The symbol comes from the source row and visible character cursor.
- LSP definitions/references are requested when a configured server is
  available.
- Failures fall back to `rg -n -w` through the active `DiffSource`.
- Lexical classifiers identify likely definitions for supported languages.
- Results are ranked by current location and review relevance, rendered in the
  reusable bottom panel, and can be opened in full-file context.

The current path must remain the fallback even if this task is implemented.

## Proposed tier

Add an optional, pure-Go syntax-aware index between LSP and ripgrep:

1. LSP `textDocument/definition` or `textDocument/references`.
2. Syntax-aware definitions/references when a parser is available.
3. Existing lexical search and classification.

The index should reuse the existing result model:

```go
type SymbolIndex struct {
    Definitions map[string][]source.Location
    References  map[string][]source.Location
}
```

Requirements:

- tolerate incomplete and temporarily invalid files;
- update only affected files after the live-reload debounce;
- never block Bubble Tea `Update` or rendering;
- identify its result source in the references panel;
- preserve existing review-aware ranking and source-order toggles; and
- add no required runtime executable.

Tree-sitter is one option, but it is not a settled dependency. Any proposal
must account for grammar distribution, CGO/static-build impact, binary size,
and behavior for languages without a bundled grammar.

## Non-goals

- Type-accurate rename or refactoring.
- Replacing LSP when a healthy server can answer precisely.
- Removing the lexical fallback.
- Indexing generated or ignored content by default.

## Tests

- A same-spelling symbol in two scopes ranks the structurally relevant result
  ahead of a lexical false positive.
- Broken source still produces best-effort results without panic or blocking.
- An edit invalidates only the affected file's index entry.
- Missing grammars and parser failures fall through to lexical search.
- Definition and reference panels label and rank syntax-index results
  consistently with LSP and ripgrep results.

## Open questions

- Is the precision gain large enough to justify bundled parsers?
- Which languages should be supported initially?
- Can the implementation remain pure Go and keep release binaries small?
- Should generated/vendor directories ever be opt-in index targets?
