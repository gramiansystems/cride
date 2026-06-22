# Risk Layers: Baseline Blame and Test Linkage

## Summary

Two independent context layers that change how carefully a reviewer reads a
hunk:

- **Blame**: what history is the agent overwriting? Rewriting three-year-old
  stable code and rewriting last week's security fix are different events;
  the reviewer should feel that difference without leaving the diff.
- **Test linkage**: does anything test what changed, and were those tests
  updated by this change?

Both are pure Go + `git`/`rg` — no LSP, no CGO, fully available in the static
core build.

## Goals

- Blame the **baseline side** of changed files: per-line commit age and
  subject; a popup on demand and an optional gutter age-shading.
- Flag risk patterns: modified lines whose last commit is recent, or whose
  subject matches fix/bug/security/revert patterns.
- Per changed symbol: do test references exist, and are they updated in this
  diff? Surface "changed, untested" and "tested but tests untouched" states.

## Non-goals

- No git-log browsing UI; blame is a per-line enrichment only.
- No coverage-data integration — linkage is reference-based heuristics and
  must be presented as such.
- No blame of the current side: the agent's uncommitted edits have no useful
  blame, and blaming a churning worktree is racy.

## Architecture — blame

New package `internal/blame`:

```go
type LineInfo struct {
    SHA        string
    Author     string
    AuthorTime time.Time
    Summary    string // commit subject line
}

type FileBlame struct {
    Path  string     // baseline-side (old) path
    Lines []LineInfo // index 0 = line 1
}

// Load blames path at the pinned baseline ref:
//   git blame --porcelain -w -M <ref> -- <path>
func Load(repoRoot, ref, path string) (FileBlame, error)
```

- Always blame `<baseline-ref> -- <OldPath>`. The question is the history of
  the code being replaced, and the pinned ref makes results immutable for the
  session. `FileStatus == FileAdded` files have no baseline — skip.
- `-w -M`: ignore whitespace, follow intra-file moves — keeps attribution
  meaningful. Renames deeper in history are git's job; we only pass the
  baseline-side path.
- Porcelain parsing: commit headers appear once per SHA with
  `author`, `author-time`, `summary` fields; subsequent lines reference the
  SHA only — the parser must carry a SHA → header cache.
- Lazy + cached: keyed by `(ref, oldPath)`; since the baseline is pinned,
  **entries never invalidate during a session**. Loaded in a Cmd goroutine:

```go
type blameLoadedMsg struct {
    path  string
    blame blame.FileBlame
    err   error
}
```

(No generation counter needed — results are immutable; keep the msg keyed by
path and ignore duplicates.)

### Blame UI

- **Popup** (suggested chord `gb`; verify against `pendingG`): blame for the
  cursor row's baseline line. Deleted/context rows use `OldLine` directly;
  added rows have no baseline line — show the nearest preceding context
  line's blame labeled "surrounding code:". Contents: short SHA, author,
  relative age, subject. While loading: "blame loading…".
- **Gutter age shading** (suggest `zb` under the `z` chord): 3–4 age buckets
  rendered as gutter intensity on deleted/context lines. Off by default.
- **Risk badge**: deleted lines whose `LineInfo` is younger than a threshold
  (default 30 days) or whose Summary matches
  `(?i)\b(fix|bug|security|cve|regression|revert)\b` get a `▲` marker, with a
  hunk-header rollup: `rewrites 2 lines from "fix auth token leak" (12d)`.

## Architecture — test linkage

Extends `internal/impact` (task 07) rather than adding a package:

```go
type TestState int

const (
    TestUpdated TestState = iota // test refs exist on changed lines
    TestStale                    // test refs exist, none updated
    Untested                     // no test references found
)

type TestLink struct {
    Symbol   string
    Change   outline.SymbolChange
    State    TestState
    TestRefs []search.ReferenceResult
}

func TestLinks(changes []outline.SymbolChange,
    refs func(symbol string) []search.ReferenceResult,
    isTest func(path string) bool) []TestLink
```

- **Shared helper `search.IsTestPath(path string) bool`** — one definition
  consumed by tasks 05, 07, 10, and 11. Heuristics: Go `*_test.go`; JS/TS
  `*.test.*`, `*.spec.*`, `__tests__/`; Python `test_*.py`, `*_test.py`;
  Rust `tests/` (path-level only in the first cut).
- Reference gathering reuses `Source.SearchWord` filtered by `IsTestPath`,
  under task 07's bounding rules: sequential in one Cmd goroutine, capped
  symbol count, partial results labeled truncated.
- Classification: a test ref whose `Review.Changed` marker is set →
  `TestUpdated`; refs exist but none changed → `TestStale`; none → `Untested`.
- Default scope: exported/public symbols only (unexported helpers flood the
  list). First cut: capitalization heuristic for Go; per-language config
  later.

### Test-linkage UI

- Change list badges: `T?` on files containing `Untested` changed exported
  symbols; `T~` for `TestStale`.
- `gt` panel (the chord task 05 reserved for related tests): one row per
  changed symbol with its state; Enter jumps to the first test ref, or to
  the symbol itself when `Untested`. Rows are `enrichmentResult`s.

## Invariants and assumptions

- Blame targets the pinned baseline **only** — never the working tree. This
  is both a correctness rule (uncommitted lines blame to nothing) and what
  makes the aggressive forever-cache safe.
- `git blame` can be slow (big files, deep history): always async; cap file
  size at `diffsource.MaxContentBytes`; skip binaries; a slow blame must
  never delay diff rendering.
- Blame is an enrichment, never load-bearing: on any failure the popup shows
  "unavailable" and gutters/badges render nothing — no errors surface in the
  main flow.
- `TestStale` and `Untested` are hints: integration/e2e suites may cover the
  code without referencing the symbol, and lexical matches carry task 07's
  noise caveats. Badge wording: "no direct test reference found" — never
  "untested" as a verdict, and never presented as coverage.
- Deleted-row coordinates are `(OldPath, OldLine)`; added rows have no
  baseline line — the popup's preceding-context fallback must be explicit in
  its labeling, not silently wrong.
- The risk regex and age threshold are policy, not truth — keep them in one
  place, ready to move into config.

## Important ideas

- **Blame age is a review-attention dial**, not information for its own
  sake: its whole job is making the reviewer slow down on the right hunks.
  The hunk-header rollup matters more than per-line detail.
- **`TestStale` is the sharpest signal in the whole feature**: behavior
  changed, tests still pass untouched — either the tests are weak or the
  change is untested in the way that matters. Rank it above `Untested` in
  attention-worthiness.
- Session-immutability of pinned-ref blame is what makes the feature cheap:
  first touch pays, everything after is a map lookup.

## Testing

- Porcelain parser: captured fixture output covering multi-hunk attribution,
  repeated-commit compaction (header appears once), boundary commits, and
  files without trailing newline.
- `Load` against a real temp repo (worktree test style) with 2–3 commits:
  per-line SHA/subject assertions; `-w` verified via a whitespace-only
  commit; `FileAdded` skip.
- Risk rules: table tests over age thresholds and subject patterns
  (including non-matches like "prefix" containing "fix" — the `\b` matters).
- `IsTestPath`: table across all supported languages, positive and negative.
- `TestLinks`: fixtures for all three states; exported-only filtering;
  truncation labeling.
- App: `gb` on deleted row uses `OldLine`; on added row uses
  preceding-context with the "surrounding code:" label; blame failure is
  invisible outside the popup; `gt` panel rows and jumps; badges appear in
  the change list.

## Open questions

- Show author identity, or only age + subject? (Blame-shaming dynamics in
  team settings argue for age-first presentation.)
- Preload blame for all changed files on idle, or stay strictly on-demand?
- Should the risk patterns be configurable per repo (e.g. ticket-prefix
  conventions)?
