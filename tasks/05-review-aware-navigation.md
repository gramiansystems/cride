# Review-Aware Navigation

## Summary

Make IDE features understand review state. `cride` is not a general editor; its
navigation should prioritize the work needed to evaluate a live diff.

## Goals

- Rank IDE results by review relevance.
- Show whether a reference, diagnostic, or symbol is inside the review diff.
- Connect results to unread regions and review annotations.
- Provide impact-oriented commands for changed symbols.
- Help the reviewer answer "what does this change affect?"

## Non-goals

- No automatic proof of behavioral correctness.
- No global dependency graph service in the first pass.
- No code editing/refactoring.

## Review metadata

Expose a query layer over review state:

```go
type ReviewIndex interface {
    IsChanged(path string) bool
    LineChangeKind(path string, line int) ChangeKind
    IsUnread(path string, line int) bool
    AnnotationStatus(path string, line int) AnnotationStatus
}
```

This should be derived from existing diff, unread, and annotation state. It
should not duplicate ownership of that state.

## Result decoration

Every navigation result can carry review markers:

```go
type ReviewMarkers struct {
    Changed     bool
    ChangeKind  ChangeKind
    Unread      bool
    Annotated   bool
    Annotation  AnnotationStatus
}
```

Use these markers in:

- References panel.
- Search results.
- Diagnostics panel.
- Symbol/workspace symbol panels.
- Call hierarchy.

## Ranking policy

Default rank order:

1. Current file and nearby lines.
2. Changed lines.
3. Unread changed lines.
4. Open annotations.
5. Changed files.
6. Tests related to changed code.
7. Definitions.
8. Other results.

Commands should allow toggling between review-ranked and natural/source order.

## Review-specific commands

Potential commands:

- `gr`: find usages, review-ranked.
- `gR`: find usages only in changed files.
- `gi`: show impact for symbol under cursor.
- `gt`: show likely related tests.
- `ge`: show diagnostics introduced by this change.

These can be phased in after basic navigation exists.

## Changed-symbol impact

When the cursor is on a changed function/type:

1. Identify the changed symbol.
2. Find references.
3. Show direct callers and tests first.
4. Highlight references outside the diff, because those are potential hidden
   behavioral impacts.

This should use LSP when available and degrade to tree-sitter/rg.

## Test discovery

Start with heuristics:

- Go: `*_test.go`, `TestXxx`, `BenchmarkXxx`, `ExampleXxx`.
- JS/TS: common `*.test.*`, `*.spec.*`.
- Rust: `#[test]` and tests modules.

Later this can become language-configurable.

## Annotation integration

Navigation results should show when they intersect comments:

- Open must-fix comment.
- Open question.
- Resolved comment.
- Unresolved/stale anchor.

This helps the reviewer check whether the agent changed all relevant call sites
or only the line that had a comment.

## Tests

- Review markers are computed correctly for added/context/deleted rows.
- Ranking prefers changed and unread results.
- Results can be displayed in source order as an alternate sort.
- Annotation markers survive shifted anchors.
- Impact view handles no references, lexical-only references, and LSP results.

## Open questions

- Should outside-diff references be emphasized more than inside-diff references?
- Should unresolved annotations be treated as highest priority?
- Should related test detection run automatically after every change or only on
  demand?
