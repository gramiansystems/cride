# Change Impact: Untouched Callers, Signature Checks, Orphans

## Summary

`gi` already lists references for a changed symbol, review-ranked. Deepen it
into an impact report that answers the reviewer's real questions: which usages
of the changed code were **not** updated, did the symbol's contract change,
and did the change strand any code (orphans / dangling references).

These are classic agent failure modes: updating a function but only some call
sites, and leaving behind helpers nothing calls.

## Goals

- Partition references of a changed symbol into "updated in this change" vs
  "untouched".
- Detect signature changes lexically and flag untouched call sites as needing
  verification.
- Detect orphans: removed definitions with remaining (dangling) references,
  definitions whose last caller was removed, and new definitions nothing
  references.
- A review-wide impact summary panel aggregating all changed symbols.

## Non-goals

- No type-level analysis or proof of behavior — lexical plus LSP hints only.
  All output is phrased as hints ("possible references"), never verdicts.
- No auto-fixing.
- No baseline-side LSP precision (task 08 upgrades signature comparison via
  before/after hover; the lexical path here must stand alone).

## Architecture

New package `internal/impact`: pure functions and report types; the app wires
them via generation-guarded Cmds.

```go
// SymbolImpact is the impact report for one changed symbol.
type SymbolImpact struct {
    Query            search.SymbolQuery
    Change           outline.SymbolChange // from task 06; zero-valued in degraded mode
    SignatureOld     string               // normalized definition line, baseline side
    SignatureNew     string               // normalized definition line, current side
    SignatureChanged bool
    Touched          []search.ReferenceResult // reference line itself is changed
    Untouched        []search.ReferenceResult // everything else
}

type OrphanKind int

const (
    OrphanDanglingRef  OrphanKind = iota // def removed, references remain
    OrphanUnreferenced                   // last caller removed, def remains
    OrphanNewUnused                      // new def, no references
)

type Orphan struct {
    Kind     OrphanKind
    Symbol   string
    Location source.Location // definition (current side) or last-known position
    Evidence []search.ReferenceResult
}
```

### Partitioning

Reference results already carry `diff.ReviewMarkers` (populated via
`MarkersForIndex` / `RankReferenceResultsWithReview`). The rule:

- **Touched** ⇔ `Review.Changed` — the reference line itself is an
  added/deleted line.
- **Untouched** ⇔ everything else, **including references in changed files on
  unchanged lines**. Do not use `IsChanged(path)` for this split: a reference
  sitting near a change but not updated is exactly the suspicious case this
  feature exists to surface.

### Signature comparison (lexical first)

1. Current side: the definition line is found by the existing machinery
   (`search.DefinitionSearchPattern` + `LooksLikeDefinition` over rg results).
2. Baseline side: run the same regexes in-process over
   `Source.BaselineContent(oldPath)` split into lines — do not shell out for
   baseline content; ripgrep only sees the current tree.
3. Normalize both def lines: collapse whitespace runs, strip trailing `{` and
   trailing comments. `SignatureChanged = normOld != normNew`.
4. When changed: bump Score on every Untouched result and prefix its label
   with a `!` marker; panel headline reads e.g.
   `signature changed · 12 possible call sites not updated`.

LSP upgrade path: current-side `Hover` at the definition gives an
authoritative signature; the baseline-side equivalent arrives with task 08.
Keep the lexical comparison as the always-available path.

### Orphan detection

Inputs: outline changes from task 06 (`SymbolRemoved` / `SymbolAdded`), plus —
as the degraded path when outline data is unavailable — identifiers harvested
from deleted lines via `search.NonKeywordIdentifiers`.

- For each **removed** symbol: `Source.SearchWord(name)`. Hits that are not
  definitions (`ClassifyReferenceKind`) → `OrphanDanglingRef` — the change
  likely breaks these.
- For each symbol appearing on **deleted lines** whose definition is *not* in
  the diff: count current-side references excluding the definition line(s);
  zero → `OrphanUnreferenced` ("the agent removed the last caller but left the
  definition").
- For each **added** symbol: current-side references excluding the definition
  and test files (`IsTestPath`, see task 11); zero → `OrphanNewUnused` —
  dead-on-arrival code.

### App integration

- `gi` panel (referenceRequestImpact): first cut keeps one flat ranked list
  but sorts Untouched first (they are the risk) and marks rows
  `○ untouched` / `● updated`. Section-header rows (non-selectable, needing a
  snap-to-selectable cursor rule in the panel) are a follow-up, not the first
  cut.
- New aggregate panel (pick a free `g` chord, e.g. `gA` for "audit"; verify
  against `pendingG`): one row per changed symbol —
  `authn · sig changed · 4/16 refs updated`; Enter re-opens `gi` scoped to
  that symbol. Orphans listed after the symbols with their own kind labels.
- Messages follow the house pattern:

```go
type impactLoadedMsg struct {
    generation int
    report     []impact.SymbolImpact
    orphans    []impact.Orphan
    truncated  bool
    err        error
}
```

- The aggregate run issues one `SearchWord` per symbol: run them
  **sequentially inside a single Cmd goroutine** (no process storm), cap at
  ~50 symbols, and set `truncated` when capped.

## Invariants and assumptions

- Lexical whole-word counts overcount (comments, strings, same-name symbols in
  other scopes) and can undercount (dynamic dispatch, reflection). Every count
  in the UI is "possible references"; **never render "no impact"** — render
  "no references found".
- `SearchWord` (ripgrep) only sees the **current side**. Baseline-side
  evidence comes from in-process scans of `BaselineContent`, bounded by
  `diffsource.MaxContentBytes`.
- Evidence taken from deleted lines carries `(OldPath, OldLine)` coordinates —
  set `Side: search.ResultSideBaseline` so jumps resolve to the deletion row
  in the diff, never to a bogus current-side line.
- Results go stale the moment the agent writes: generation-guard every load;
  recompute only on demand (panel open), not on every tree change.
- Never block `Update`: all searching and content reads happen in Cmd
  goroutines.
- Reuse `search.ReferenceResult` / `enrichmentResult` as row types so the
  existing order toggles (`ResultOrderReview` / `ResultOrderSource`) and
  cursor code keep working.
- Task 06 is a soft dependency: with outline data absent, skip rename-aware
  matching and drive orphan detection purely from deleted-line identifiers.

## Important ideas

- **"Touched" must mean the reference line itself changed.** A reference in a
  changed file on an unchanged line is the most interesting row in the panel,
  not a resolved one.
- Normalized-def-line comparison is crude but catches the highest-value class
  (arity/name/parameter-type changes) with zero semantic tooling, and it works
  on broken code where LSP stalls.
- `OrphanNewUnused` is agent-review gold: agents habitually leave unused
  helpers behind. Cheap to detect, high hit rate.

## Testing

- Partition: fabricated `ReferenceResult`s with markers — Touched/Untouched
  split; a ref in a changed file on an unchanged line lands Untouched.
- Signature: fixture old/new contents — arity change (flagged), rename
  (flagged), whitespace-only reflow (not flagged), trailing-comment move (not
  flagged), method receiver change (flagged).
- Orphans: in-memory fake `diffsource.Source` (reuse/extend the app tests'
  fake) — removed def with a surviving call → DanglingRef; removed last caller
  → Unreferenced; added unused helper → NewUnused; added helper referenced
  only from a test file → not NewUnused (or configurable).
- Aggregate: symbol cap respected; `truncated` set; sequential execution (no
  goroutine-per-symbol).
- App: `gi` sorts untouched-first with markers; `!` flags appear when the
  signature changed; stale `impactLoadedMsg` ignored; Enter from the aggregate
  panel re-opens `gi` with the right `SymbolQuery`.

## Open questions

- Should untouched references in *test* files rank below untouched production
  references, or above (tests encode expectations)?
- Auto-run the aggregate audit once per diff generation and badge the footer,
  or strictly on demand?
- Where should the untouched/updated split live long-term: markers in one
  list, or real sections with a snap-to-selectable cursor?
