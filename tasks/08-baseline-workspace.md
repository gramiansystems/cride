# Baseline Workspace: Introduced Diagnostics and Before-Side Semantics

## Summary

Materialize the pinned review baseline as a real, read-only directory with its
own LSP instance. This is one infrastructure investment that unlocks a whole
before/after family:

- **Introduced diagnostics** — the delta between baseline and current
  diagnostics, which turns LSP noise on an agent's churning tree into review
  signal ("this change introduced 2 errors, fixed 1").
- **Baseline-side hover / definition** — answer "what did this code do
  before?" directly from deleted rows.
- **Before/after hover** for changed symbols (consumed by task 07 as its
  signature-comparison precision upgrade, and by task 06 for baseline
  symbols via LSP instead of lexical extraction).

## Goals

- `internal/baseline`: materialize the baseline SHA as a detached git
  worktree, idempotently, under the XDG cache; clean lifecycle.
- A second `lsp.Client` rooted in that worktree, independent of the current
  one.
- Diagnostics delta (introduced / fixed / preexisting) with position mapping
  through the diff.
- Route hover/def on baseline-only rows to the baseline client.

## Non-goals

- Never write to the baseline workspace. It is read-only by convention.
- No remote transports (the seam stays `diffsource.Source`).
- No persistent index service; the worktree + a stock LSP server is the whole
  mechanism.

## Architecture

New package `internal/baseline`:

```go
// Workspace is a materialized, read-only checkout of the review baseline.
type Workspace struct {
    Root string // absolute path of the detached worktree
    Ref  string // pinned baseline commit SHA
}

// Materialize creates (or reuses) a detached worktree for ref under dir.
// Idempotent: an existing clean worktree already at ref is reused as-is.
func Materialize(repoRoot, ref, dir string) (*Workspace, error)

// Close removes the worktree registration and directory.
func (w *Workspace) Close() error
```

- **Placement**: `$XDG_CACHE_HOME/cride/worktrees/<repo-id>/<short-ref>`
  (repo-id = hash of the repo root path). Never inside the repo's own
  worktree.
- **Creation**: `git -C <repoRoot> worktree add --detach <dir> <ref>`.
- **Reuse check**: dir exists ∧ `git -C <dir> rev-parse HEAD` == ref ∧
  `git -C <dir> status --porcelain` is empty; otherwise remove and recreate.
- **Close**: `git -C <repoRoot> worktree remove --force <dir>`. Do not run
  repo-wide `git worktree prune` (it has no path filter and the user/agent may
  have their own worktrees); only fall back to prune + manual dir removal if
  `remove` fails.
- **Tree-snapshot baselines** (the "no commit yet" case from DESIGN §2):
  `git worktree add` requires a commit — synthesize one with
  `git commit-tree <tree-sha> -m cride-baseline`. This creates an unreferenced
  commit object; no ref is updated, and the detached worktree pins it against
  GC.

### Lifecycle in the app

Lazy and async: nothing materializes at startup. The first feature needing the
baseline dispatches a Cmd; the footer shows `baseline ◐ preparing` through the
same status mechanism as `lspStatuses`.

```go
type baselineReadyMsg struct {
    workspace *baseline.Workspace
    err       error
}
```

The Model gains `baselineLSP lsp.Client`, initialized to
`lsp.NewUnavailableClient(...)` — every call site works unchanged when
materialization is disabled, pending, or failed. On `baselineReadyMsg`,
construct the same ProcessClient implementation used for the current side,
rooted at `workspace.Root` with the same `lsp.Config`.

**Path mapping boundary**: results from `baselineLSP` are relative to
`Workspace.Root` and use *old* paths. Translate at the app boundary: re-root
to repo-relative, set `Side: search.ResultSideBaseline`, and map
OldPath→NewPath via the rename table derived from `[]diff.FileDiff` for
display and jumps.

### Line mapping (in `internal/diff`)

```go
// MapOldLineToNew maps a baseline line number to the current file. exact is
// false when the line falls inside a replaced region; the result is then the
// nearest following current-side line.
func MapOldLineToNew(file FileDiff, oldLine int) (newLine int, exact bool)

// MapNewLineToOld is the symmetric inverse.
func MapNewLineToOld(file FileDiff, newLine int) (oldLine int, exact bool)
```

Walk `Hunks` in order accumulating `(NewStart − OldStart)` offsets; inside a
hunk, walk `Lines` pairing `OldLine`/`NewLine` (context lines carry both).

### Introduced diagnostics

```go
type DiagnosticsDelta struct {
    Introduced  []lsp.Diagnostic // current-side coordinates
    Fixed       []lsp.Diagnostic // baseline diagnostics with no current match
    Preexisting []lsp.Diagnostic // matched pairs, current-side coordinates
}

func Delta(baselineDiags, currentDiags []lsp.Diagnostic,
    files []diff.FileDiff) DiagnosticsDelta
```

Matching rule — a baseline diagnostic matches a current one when:

1. same file after rename mapping (OldPath→NewPath), and
2. same `Code`, or, when Code is empty, same `Source` + message prefix
   (first ~60 bytes), and
3. same `Severity`, and
4. the baseline line mapped via `MapOldLineToNew` lands within ±2 of the
   current line, **or** both fall inside the same hunk's replaced region
   (`exact == false` for both).

Unmatched current → Introduced. Unmatched baseline → Fixed. Position is a
tiebreaker, not the primary key — diagnostics drift lines constantly.

### UI

- Extend the diagnostics enrichment panel with a **delta mode**: a key inside
  the panel (e.g. `d`) cycles all → introduced → fixed. Default to
  *introduced* whenever a baseline workspace is ready. Rows remain
  `lsp.Diagnostic` + `ReviewMarkers`, so existing ranking works.
- **Baseline-side hover/def**: the diff view knows a row's side (deleted rows
  have only `OldLine`). When the cursor row is baseline-only, route `K` / `gd`
  to `baselineLSP` with `source.Location{Path: OldPath, Line: OldLine, ...}`.
  Definition results map back through the rename table; if the target still
  exists on the current side, jump there; otherwise show the baseline content
  read-only (extend the `fileContents` cache with a side dimension, fed by
  `BaselineContent`).
- **Before/after hover**: cursor on a changed symbol → issue `Hover` to both
  clients (current location, and the old location via `MapNewLineToOld`);
  render two sections `baseline:` / `current:` in the hover panel.

## Invariants and assumptions

- The baseline workspace is **immutable by convention**: cride never writes
  there. LSP servers may write their own caches elsewhere (gopls does);
  that's theirs.
- The baseline ref is always a real commit (HEAD at session start), except
  the tree-snapshot case handled via `commit-tree`. The detached worktree
  pins the commit against GC for its lifetime.
- Baseline diagnostics are **stable for the whole session** (the ref is
  pinned): compute once per language/file set, cache in the Model. Current
  diagnostics churn with every agent write and stay generation-guarded.
- Two clients, two independent statuses, independent failure: a crashed or
  absent baseline server must never degrade the current-side experience.
  `UnavailableClient` is the permanent fallback shape.
- Disk cost is a full checkout. Add a config flag
  (e.g. `--baseline-workspace=off`) and a size guard: skip auto-materialize
  past a threshold, still allow explicit activation.
- Never place the worktree inside the user's repo; never touch worktrees
  cride didn't create.
- All git invocations and server warmup are async Cmds; `Update` never waits
  on them.

## Important ideas

- **The delta is the product.** Raw diagnostics on an agent's mid-edit tree
  are unusable noise; "what did this change introduce/fix" is the question
  the reviewer actually has. When the current tree is temporarily broken the
  delta will spike — show a "tree is churning" hint when current counts are
  wildly above baseline rather than pretending precision. Only refresh the
  delta after a debounced settle.
- **One seam, many features**: 06 can upgrade baseline symbol extraction to
  LSP, 07 gets authoritative before/after signatures, deleted-row navigation
  becomes real — all through the same `baselineLSP lsp.Client` field with no
  new abstractions.
- **Reuse-not-recreate matters**: LSP warmup on a big repo is expensive. A
  persistent per-(repo, ref) cache directory makes the second session
  instant, which is why Materialize is idempotent rather than temp-dir-based.

## Testing

- `Materialize` (real temp git repos, following the worktree
  `source_test.go` style): fresh create matches ref content; reuse when
  clean-and-at-ref (assert no re-checkout via dir mtime or a sentinel);
  recreate when dirty; tree-snapshot path via `commit-tree`; `Close` removes
  registration and directory; repo with existing unrelated worktrees is left
  untouched.
- `MapOldLineToNew` / `MapNewLineToOld`: table tests — line before all hunks,
  between hunks, inside pure-insert and pure-delete hunks, inside a replaced
  region (`exact == false`), after the last hunk; multi-hunk cumulative
  offsets; symmetric round-trips on context lines.
- `Delta`: fabricated diagnostics — pure position drift (preexisting), fixed,
  introduced, renamed file, empty-Code message-prefix matching, severity
  mismatch (no match).
- App: a fake baseline client returning canned results — panel delta mode
  cycles and defaults to introduced; hover/def on a deleted row routes to the
  baseline client (fake records the call) with old-path coordinates; before/
  after hover renders both sections; `baselineReadyMsg` after failure leaves
  `UnavailableClient` semantics intact.
- Manual: a repo with one known baseline warning; have the agent introduce an
  error — introduced shows exactly one; hover a deleted line and see the old
  signature.

## Open questions

- Auto-materialize at session start behind the size guard, or strictly on
  first use?
- Surface Fixed prominently (positive signal) or tucked behind the cycle key?
- Garbage-collect stale cache worktrees from previous sessions on startup, or
  leave them for reuse until a `cride --gc`?
