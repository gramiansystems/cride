# cride architecture

This document describes the architecture that exists in the repository today.
Future feature specifications belong in the [roadmap](tasks/README.md), not in
this guide.

## Product boundary

cride is a terminal IDE for reviewing a diff while its current side is still
changing. Its center of gravity is the review loop: notice new work, inspect
it in context, leave comments, and see the response.

The project deliberately does not aim to be:

- a general-purpose text editor;
- a full Git client with staging, rebasing, or conflict resolution; or
- dependent on healthy, compilable source code for basic navigation.

Small, current-side edits are supported because they shorten a review cycle.
The baseline side remains immutable.

## Review model

Every session compares two sides:

| Side | Live review | Commit/range review |
| --- | --- | --- |
| Baseline | A commit resolved and pinned at session start | The selected base commit |
| Current | The working tree, including untracked non-ignored files | The selected head commit |

The main diff is always `baseline -> current`. Pinning a commit SHA rather than
following a branch name means later commits and amendments stay inside the same
review.

Live reviews expose a watcher and a cheap fingerprint. The filesystem watcher
provides low-latency updates; fingerprint polling is the fallback when watching
is unavailable. Static commit and range reviews expose an immutable current
side.

### Unread state

Unread state is derived per file. cride stores the hash of the review diff when
the reviewer marks a file read. A file is unread whenever its current hash does
not match that stored value. This has useful behavior without special cases:

- a newly changed file is unread;
- editing an acknowledged file makes it unread again; and
- reverting back to the acknowledged diff makes it read again.

The session store persists these hashes. Region-level unread tracking is a
roadmap item.

### Review annotations

`internal/annotate` stores comments in the canonical, editable `review.md`.
Startup and explicit reloads parse Markdown edits; matching anchors preserve
in-memory comment identity and timestamps, while new Markdown comments receive
new metadata. A comment can be general or anchored to a baseline/current line
range. The current implementation stores a code snippet with line coordinates,
detects anchor drift, and marks detached comments unresolved rather than
discarding them.

Content-fingerprint re-anchoring and replies remain roadmap work. Markdown
syntax changes should remain backward-compatible with existing review files.

## Data flow

```text
                         background Bubble Tea commands
  git + filesystem  ---> DiffSource ---> diff parser --------+
  git/rg + LSP      ---> search, outline, diagnostics --------+--> messages
  session/comments  ---> load and save commands --------------+       |
                                                                    v
  keyboard + mouse ------------------------------------------> app.Model
                                                                    |
                                                                    v
                                            UI rows, panes, overlays, footer
```

`internal/app.Model` owns interactive state. Bubble Tea messages are the only
way asynchronous results mutate it. Git operations, file reads, search, LSP
requests, and persistence run in commands or watcher goroutines; `Update`
coordinates state and dispatches more work.

Generation counters guard asynchronous search, outline, enrichment, and diff
loads. Late results from an older request are ignored. Per-file view state lets
reloads and file switches preserve a reviewer's source position.

## Package layout

```text
cmd/cride/                    CLI flags and program bootstrap
internal/
  annotate/                   comments and Markdown persistence
  app/                        Bubble Tea model, commands, navigation, editing
  config/                     user config parsing and XDG config path
  diff/                       unified-diff model, parser, review index
  diffsource/                 source interface and optional capabilities
    worktree/                 local Git implementation, watcher, fingerprint
  highlight/                  cached Chroma syntax highlighting
  keymap/                     key binding source and Markdown generator
  lsp/                        lazy stdio JSON-RPC language-server client
  outline/                    lexical outlines and before/after symbol diff
  search/                     fuzzy ranking and lexical symbol search
  session/                    versioned, atomic XDG state persistence
  source/                     shared source-coordinate types
  ui/                         rows, wrapping, pairing, panes, theme, rendering
docs/                         user and dependency reference
tasks/                        active feature specifications
```

All application packages are internal. The supported interface is the `cride`
command, which keeps package refactors possible while the project matures.

## Diff sources

`internal/diffsource.Source` is the seam between review behavior and storage or
transport. It provides:

- the unified review diff and baseline label;
- bounded current- and baseline-side content reads;
- changed paths and project files; and
- project text and whole-word search.

Sources may additionally implement `Watcher` and `Fingerprinter`. The local
working-tree source is the only implementation today. Commit and range modes
reuse it with an immutable current Git object. Remote, container, and hosted
pull-request sources can implement the same contract later.

On-demand content reads are capped at 2 MiB and reject non-regular files.
These limits keep full-file views and navigation from unexpectedly pulling
large or unsafe inputs into the terminal UI.

## Rendering and interaction

The parsed diff is flattened into `ui.Row` values. Cursor motion, comments,
hunk navigation, context expansion, and rendering all consume those rows.
Full-file view is another row projection that keeps diff metadata attached to
source coordinates. Side-by-side mode pairs delete/insert rows and falls back
to unified mode at narrow widths.

Wrapping maintains a screen-line-to-logical-row mapping. Scroll calculations
therefore operate on visible terminal lines while selection and comments stay
attached to source rows.

The change list, main review pane, bottom enrichment panel, command palette,
and search overlays are projections of the same model. The change list may
also include unchanged current-side project files as navigation entries; they
remain excluded from diff statistics, unread state, and review indexing. A
contextual footer combines baseline/change statistics, prompts, progress, and
transient errors.

## Code intelligence

Navigation uses a fallback chain designed for unstable agent worktrees:

1. A language server is started lazily when one is configured for the file and
   its executable is on `PATH`.
2. Definitions and references fall back to whole-word project search and
   language-aware lexical classification.
3. Document outlines fall back to built-in lexical extractors for Go, Python,
   JavaScript/TypeScript, Rust, and C/C++.

The stdio LSP client supports definitions, references, hover, diagnostics,
document/workspace symbols, and incoming/outgoing call hierarchy. Requests
have timeouts and unavailable servers degrade the feature rather than failing
the review.

All current Go dependencies are pure Go; the core build has no intentional CGO
requirement.

## Editing contract

Editing uses source coordinates, never rendered screen indexes. Only rows with
a current-side source location are editable. Entering edit mode expands the
file, loads a buffer, and creates `.cride/editing.json`. While that advisory
lock exists:

- a cooperating coding agent should avoid the named file;
- live reloads are deferred; and
- `ZZ` writes atomically, while `ZQ` discards the buffer.

The reviewer's save wins if another process ignores the advisory lock. After a
save, the file is reloaded and stamped read so the reviewer's own edit does not
appear as unseen agent work. See the [editing guide](docs/review-expansion-and-editing.md)
for the supported vim subset and known simplifications.

## Persistence and repository-local files

Session state is versioned and written atomically beneath
`$XDG_STATE_HOME/cride`. The repository ID incorporates the root and pinned
baseline, so different reviews do not share cursor or unread state. Corrupt or
future-versioned session data is ignored rather than preventing startup.

Repository-local collaboration files have separate roles:

| Path | Role |
| --- | --- |
| `review.md` | Canonical human- and agent-editable review |
| `.cride/editing.json` | Temporary advisory editing lock |

Projects reviewed with cride should normally ignore `review.md` and `.cride/`.

## Invariants for contributors

- Keep blocking I/O out of Bubble Tea `Update` and `View`.
- Treat the current side as mutable and potentially uncompilable.
- Preserve source coordinates across view projections and reloads.
- Do not silently discard comments or persisted review state.
- Prefer useful lexical behavior when semantic tooling is unavailable.
- Keep generated `docs/keymap.md` synchronized with `internal/keymap`.
- Add tests around state transitions, stale asynchronous results, source-line
  anchoring, and terminal-width edge cases.

Run `make test`, `make vet`, and `make docs` before submitting changes. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the complete workflow.
