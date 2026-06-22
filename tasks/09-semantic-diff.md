# Semantic Diff Quality: Moves, Format-Noise, Intraline

## Summary

Agents relocate and reformat code constantly. The diff should distinguish
three things that currently all render identically: code that **moved**, code
that was **reformatted**, and code that was **actually edited**. This is a
pure-Go analysis pass over the parsed review diff; tree-sitter can later
upgrade the token layer without changing any interface.

## Goals

- Detect moved blocks (within and across files); render them dimmed with an
  `M` gutter badge; edits *inside* a move highlighted at full intensity.
- Detect format-only hunks and format-only line pairs; a view toggle to dim or
  collapse them.
- Intraline (word-level) change highlighting for modified line pairs.
- Derived, deterministic, recomputed with the diff; fast enough for every
  recompute.

## Non-goals

- No AST diffing in the first pass; normalization is whitespace-only.
- No semantic-equivalence claims beyond that normalization.
- Never hide a change without a visible placeholder — suppression is a view
  policy, not a data policy.

## Architecture

New package `internal/diffx` — a pure post-processing pass over
`[]diff.FileDiff`:

```go
type Options struct {
    MinMoveLines  int     // minimum block length to consider, default 4
    MinSimilarity float64 // accept threshold, default 0.85
    MaxPairs      int     // cap on candidate block pairs, default 4096
}

// BlockRef addresses a run of same-kind lines inside one hunk.
type BlockRef struct {
    Path      string
    HunkIndex int // index into FileDiff.Hunks
    LineStart int // index into Hunk.Lines
    LineCount int
}

type Move struct {
    From, To   BlockRef // From: deleted side; To: inserted side
    Similarity float64  // 1.0 = pure move
    // EditedLines are To-side offsets whose content differs from the paired
    // From line; they render at full intensity.
    EditedLines []int
}

type LineKey struct {
    Path string
    Side search.ResultSide
    Line int // OldLine for baseline side, NewLine for current side
}

// Span is a half-open byte-column range into Line.Content (1-based, no +/-
// prefix), consistent with source.Location column conventions.
type Span struct{ Start, End int }

type Analysis struct {
    Moves           []Move
    FormatOnlyHunks map[BlockRef]bool // BlockRef with LineStart/Count zeroed = whole hunk
    FormatOnlyPairs map[LineKey]bool
    Intraline       map[LineKey][]Span
}

func Analyze(files []diff.FileDiff, opts Options) Analysis
```

### Move detection

1. Collect blocks: maximal runs of ≥ `MinMoveLines` consecutive `LineDelete`
   lines, and separately `LineAdd` lines, across all files.
2. Normalize each line: trim, collapse internal whitespace runs to one space.
   Hash normalized lines (`hash/maphash`). Blank lines don't count toward
   length or similarity.
3. Index deleted-line hashes: hash → [(block, offset)]. For each inserted
   block, **vote**: each line's hash lookup votes for a
   (deletedBlock, alignmentOffset) pair; the best-voted alignment wins.
   `Similarity = matchedLines / max(fromLen, toLen)`.
4. Accept ≥ `MinSimilarity`; consume greedily best-similarity-first so every
   block participates in at most one Move; stop expanding candidates past
   `MaxPairs`.
5. `Similarity < 1.0` → an *edited move*: pair lines by the winning alignment
   and record non-matching To-side offsets in `EditedLines`.

### Format-only detection

- **Hunk-level**: the multiset of normalized non-empty deleted lines equals
  the multiset of normalized non-empty added lines → format-only (catches
  re-indents, brace shuffles). Additionally compare the whitespace-stripped
  *concatenation* of each side — this catches re-wrapped comments and
  parameter lists that line-by-line comparison misses.
- **Line-level**: pair delete+insert lines the way the UI's `alignPairs`
  does; pairs equal after normalization → `FormatOnlyPairs` (dim them even
  inside hunks that aren't wholly format-only).

### Intraline

For paired, non-format lines: tokenize into identifier/word tokens (reuse the
byte-class predicates that already exist in `internal/search/symbol.go`), run
Myers over the token lists (`aymanbagabas/go-udiff` — already the planned
unread-layer dependency — or a small in-package Myers), emit `Span`s for the
changed tokens on each side.

Caps to avoid confetti and cost: skip lines > 400 bytes; if common tokens
< 30% of either side, highlight the whole line instead.

### Rendering (internal/ui)

- `diffRow` gains optional flags: move membership (move id, pure/edited),
  format-only, intraline spans. Rendering composes `Analysis` over rows by
  `(path, hunkIndex, lineIndex)` — `FileDiff` itself is never mutated.
- Pure move: dimmed content, gutter `M`. Edited move: dimmed block, but
  `EditedLines` at full highlight. Format-only: dim + `≈` gutter.
- Toggle `zn` ("noise") cycles: show all → dim noise → collapse format-only
  hunks and pure moves. Collapsed content leaves a one-line placeholder with
  counts (`· 42 moved lines from util/log.go ·`) — never silently omitted.
- `gm` on a moved block jumps between its From/To ends (cross-file allowed).
- Change-list badges show net-of-noise alongside raw counts:
  `+120/−118 · ~110 moved`.

## Invariants and assumptions

- Analysis is **derived data**: recompute on `diffLoadedMsg`; never mutate
  `FileDiff`; drop stale results by generation as usual.
- **Hidden ≠ read.** When unread tracking lands (M2/M4), suppressed-but-
  changed lines must still count as unread. `zn` is presentation only.
- Normalization stays conservative — whitespace only. Case folding or token
  reordering would misclassify real edits as noise; a false "format-only" is
  strictly worse than a false "changed".
- Pairing is exclusive (each block in ≤ 1 Move) and **deterministic**: stable
  ordering (similarity desc, then path/hunk order) so recomputes never flip
  pairings under the reviewer.
- Intraline spans are byte columns into `Line.Content` (no `+`/`-` prefix, no
  newline), matching the `source.Location` byte-column convention.
- Performance: hashing is O(total lines); matching is bounded by `MaxPairs`.
  Budget: a 10k-line diff analyzed well under 50ms — enforce with a benchmark,
  since this runs on every recompute once the live layer exists.
- Side conventions: From blocks live at `(OldPath, OldLine)`, To blocks at
  `(NewPath, NewLine)`; `LineKey.Side` is mandatory, never inferred.

## Important ideas

- **Vote-by-line-hash alignment** finds moves without quadratic block
  comparison and tolerates a few edited lines inside a relocated region —
  which is precisely the dangerous case.
- **Edited moves deserve the most attention, not the least**: an edit
  camouflaged inside a relocation is the sneakiest change class in agent
  diffs. Dimming the moved bulk is what makes the edited lines inside it pop.
- The concatenation rule catches re-wraps cheaply; don't try to be clever
  line-by-line about wrapping.
- Tree-sitter upgrade path: swap the normalizer/tokenizer for an AST-token
  stream per language behind the same `Analyze` signature; rendering,
  toggles, and tests downstream are untouched.

## Testing

- Golden `Analyze` cases: function moved verbatim within a file; moved across
  files; moved with one edited line (similarity value, `EditedLines`
  content); re-indented block (hunk-level format-only); re-wrapped comment
  (concatenation rule); two similar-but-distinct blocks (no false pairing);
  interleaved moves; runs shorter than `MinMoveLines` ignored; blank-line
  padding doesn't inflate similarity.
- Determinism: permuting file order in the input produces identical Moves.
- Intraline: token-rename yields tight spans on both sides; >400-byte line
  skipped; <30% commonality falls back to whole-line.
- Benchmark: synthetic 10k-line diff under the budget.
- UI (render_test.go patterns): flag composition per row; `zn` cycle states;
  placeholder rows carry correct counts; `gm` jump targets resolve across
  files.

## Open questions

- Persist the `zn` state per session alongside other view state?
- Should pure moves become auto-read candidates once unread tracking lands?
- Is 0.85 the right default similarity, or should it scale with block length
  (longer blocks tolerate more edits)?
