# Cross-Hunk Relations and Suggested Review Order

## Summary

Connect the hunks of a review to each other through the symbols they define
and use, then exploit that graph twice: a **related-hunks** lookup ("the
definition changed here — these other hunks are the adoption sites"), and a
**suggested review order** (callees before callers, types before uses) that
replaces alphabetical file order with an order that minimizes
"what is this function?" moments.

Depends on task 06 (outline diff) for the changed-symbol list.

## Non-goals

- No whole-repo call graph — the graph is scoped to changed code only.
- No persistent graph storage; rebuilt whole on each diff reload.
- Never reorder the change list without explicit user opt-in.

## Architecture

New package `internal/relate`:

```go
type HunkID struct {
    Path  string
    Index int // index into FileDiff.Hunks — valid within one diff generation
}

type EdgeKind int

const (
    EdgeCallerUpdate EdgeKind = iota // a hunk adopts a changed symbol
    EdgeTestUpdate                   // same, but the using file is a test
    EdgeSameSymbol                   // two hunks touch the same symbol's def
)

type Edge struct {
    From   HunkID // hunk whose added lines mention the symbol
    To     HunkID // hunk containing the symbol's definition change
    Symbol string
    Kind   EdgeKind
}

type Graph struct {
    Defs  map[string][]HunkID // changed-symbol name → definition hunks
    Uses  map[string][]HunkID // changed-symbol name → hunks using it
    Edges []Edge
}

func Build(files []diff.FileDiff, changes []outline.SymbolChange) Graph
```

Build steps:

1. **Defs**: for each Modified/Added/Renamed `SymbolChange`, the hunks whose
   new-side range intersects the symbol's `Range`.
2. **Uses**: for every *added* line in every hunk, harvest identifiers with
   `search.NonKeywordIdentifiers` and keep **only names of changed symbols**.
   This restriction is what keeps a lexical approach precise enough — we
   never scan for arbitrary identifiers, only the handful this review
   defines or modifies. Additional noise filters: identifier length ≥ 3 and
   not `commonKeyword` (both helpers exist in `internal/search`).
3. **Edges**: `Uses ∩ Defs`, excluding self-hunk. `EdgeTestUpdate` when the
   using file is a test file (`IsTestPath`, shared helper — see task 11),
   else `EdgeCallerUpdate`.

LSP upgrade path: replace name matching with `references` results filtered to
changed lines — precise but slow; lexical stays the default, LSP on demand.

### Related-hunks command

Suggested chord `gh` (verify against the `pendingG` block):

- Resolve the symbol under the cursor via `search.ExtractIdentifier`, exactly
  like `gd`/`gr`; ambiguity reuses the existing symbol-choice overlay. With
  no identifier under the cursor, default to the enclosing changed symbol
  (task 06's `EnclosingPath`).
- Panel rows (reuse `enrichmentResult`): definition hunks first, then caller
  updates, then test updates. Label = hunk header preview + relationship;
  each row carries `ReviewMarkers`; Enter jumps via the existing
  `pendingLocation` machinery.

### Suggested review order

```go
// Order returns changed files in suggested reading order: definitions of
// used symbols before their adopters. Deterministic for identical inputs.
func Order(g Graph, files []diff.FileDiff) []string
```

- Nodes: changed files. Edge file A → file B when A's changes define a symbol
  that B's changes use (A reads first).
- Topological sort with deterministic tie-breaks, in priority order:
  types/interfaces before functions, non-test before test, then path order.
- Cycles must never fail the sort: break by (in-degree, path) and continue.
- Surface: an ordering toggle in the change list (suggested ⇄ path order),
  mirroring the `ResultOrder` naming, plus a small rank badge per file when
  the suggested order is active. Hunk-level ordering and a
  "next suggested" navigation key are follow-ups, not the first cut.

### Recompute and staleness

The graph is derived: rebuild on `diffLoadedMsg` only (cost is O(added lines)
identifier scanning — cheap). `HunkID.Index` is only meaningful within one
diff generation; panels built from a previous generation must re-resolve
jumps by `(path, hunk header text)` or degrade to a file-level jump.

## Invariants and assumptions

- Lexical edges are noisy (shadowing, common names, strings/comments). The UI
  vocabulary is "related", never "callers of". This is a navigation aid, not
  an analysis result.
- Only **added** lines create Uses edges. Deleted-line identifiers describe
  the past — they belong to task 07's orphan analysis, not this graph.
- **Determinism and stability**: identical inputs must produce an identical
  graph and order (stable sorts everywhere). The suggested order must not
  fluctuate while the reviewer reads: recompute only on diff reload, and when
  the order does change, preserve the user's current file selection by
  *path*, never by list index.
- The graph never owns review state; it composes `FileDiff` + `SymbolChange`
  + `ReviewIndex` markers, all read-only.
- All graph construction happens in a Cmd goroutine with the standard
  generation guard; `Update` only stores the finished graph.

## Important ideas

- **Callee-first is the natural reading order** for a change: read the new
  helper's definition hunk once, then its ten adoption sites make sense on
  sight. That is exactly reverse-topological order over `EdgeCallerUpdate`.
- Restricting the lexical scan to changed-symbol names converts an
  imprecise technique into a usable one — the candidate vocabulary is tiny
  and highly distinctive.
- `gh` is the navigation complement of task 07: 07 answers "which usages did
  the agent forget", `gh` answers "where is the rest of this change".

## Testing

- `Build`: fixture diff + changes — helper added in `a.go`, used in `b.go`
  and `c_test.go` → edges b→a (`EdgeCallerUpdate`) and c_test→a
  (`EdgeTestUpdate`); self-hunk references excluded; a changed symbol named
  like a common word filtered by the noise rules; identifiers on deleted
  lines produce no edges.
- `Order`: chain (a→b→c) reads a,b,c; diamond; cycle broken
  deterministically; type-vs-function and test-vs-non-test tie-breaks;
  stability under permutation of input file order.
- App: `gh` on a changed symbol opens the panel grouped
  definition/caller/test; `gh` on empty space uses the enclosing changed
  symbol; Enter jumps; a stale-generation panel falls back to file-level
  jump; toggling order preserves selection by path.

## Open questions

- Promote the suggested order to default once it proves trustworthy?
- Weight edges by use-count to order "most-adopted first" among siblings?
- Should `EdgeSameSymbol` (two hunks touching one symbol's definition, e.g.
  signature + body in separate hunks) get its own panel section?
