# cride

**A terminal review IDE for code that is still being written.**

Mostly vibe-coded with Codex and Claude Code. Still in beta.

[![Go 1.24+](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

cride pins a Git baseline, watches the working tree, and keeps an unread queue
as a person or coding agent changes the code. Review the diff, leave anchored
comments, save or reload `review.md`, and watch the next revision arrive without
starting over.

```text
agent edits  ->  cride marks files unread  ->  you review and comment
     ^                                              |
     +--------------- review.md --------------------+
```

cride is review-first. It includes focused, vim-style editing for small fixes,
but it is not trying to replace your editor or Git client.

> [!NOTE]
> cride is under active development. The local working-tree workflow is ready
> to use; remote and pull-request diff sources remain roadmap items.

## Highlights

- **Live review queue:** changed files become unread again when they are edited.
- **Persistent sessions:** the baseline, cursor, view state, and read state
  survive restarts.
- **Review comments:** attach comments to either side of a diff, resolve them,
  and export an agent-friendly Markdown review.
- **Review-aware navigation:** fuzzy file open, project search, definitions,
  references, changed-symbol outlines, diagnostics, hover, and call hierarchy.
- **Graceful fallbacks:** lexical navigation continues to work when a language
  server is missing or the worktree is temporarily broken.
- **A capable diff view:** syntax highlighting, local context expansion,
  full-file context, side-by-side mode, soft wrapping, mouse support, and
  adaptive light/dark themes.

## Install

Building from source requires [Go 1.24+](https://go.dev/dl/) and
[Git](https://git-scm.com/downloads). Install
[ripgrep](https://github.com/BurntSushi/ripgrep) as well for project search and
lexical definition/reference lookup.

After cloning this repository:

```sh
make install
```

This runs `go install ./cmd/cride`; ensure your Go binary directory is on
`PATH`. You can build a repository-local `./cride` binary instead with
`make build`.

## Quick start

```sh
cd path/to/a/git-repository
cride
```

On first launch, cride pins `HEAD` and reviews everything that changes after
it, including uncommitted work and later commits. Reopening the same review
restores its state. Use `cride --fresh` when you want to ignore that state.

The core loop is:

1. Use `n`/`N` to move through unread files and `]c`/`[c` to move through
   hunks.
2. Press `R` to mark the current file read and advance, or `A` to mark all
   files read.
3. Press `c` for a line comment or `C` for a general comment.
4. Press `ctrl+s` (or `e`) to save the review to `review.md`.
5. Keep cride open while the code changes; edited files return to the unread
   queue automatically.

A useful instruction for a coding agent is:

> Read `review.md` at the repository root and address its open review
> comments. Before writing a file, check `.cride/editing.json`; if it names
> that file, wait until it disappears.

Add cride's repository-local files to the reviewed project's `.gitignore`:

```gitignore
.cride/
review.md
```

## Review targets

| Command | Review |
| --- | --- |
| `cride` | Live changes since `HEAD` was pinned |
| `cride --baseline main` | Live changes since a named ref |
| `cride --range BASE..HEAD` | An immutable two-dot commit range |
| `cride --range BASE...HEAD` | An immutable merge-base range |
| `cride --commit REF` | One commit against its first parent |
| `cride --fresh` | Start without saved session state |
| `cride path/to/repo` | Review a repository from elsewhere |

Run `cride --help` for the complete command-line reference.

## Essential keys

| Keys | Action |
| --- | --- |
| `n` / `N` | Next / previous unread file |
| `R` / `U` / `A` | Mark read and advance / unread / all read |
| `]c` / `[c` | Next / previous hunk |
| `}` / `{` | Next / previous changed file |
| `c` / `C` | Line comment / general comment |
| `ctrl+s` / `e` | Save `review.md` without leaving cride |
| `ctrl+r` | Reload the diff and import edits from `review.md` |
| `tab` | Toggle full-file context |
| `zo` / `zc` / `zs` | Expand / collapse context / toggle side-by-side |
| `ctrl+p` / `/` / `g/` | Open file / search file / search project |
| `gd` / `gr` / `gy` | Definition / references / changed symbols |
| `?` | Open the categorized command palette |

See the [complete keymap](docs/keymap.md) for navigation, editing, panels, and
mouse controls.

## Editing and code intelligence

Press `i`, `I`, or `a` on a current-side line to enter the vim-style editing
mode. `ZZ` saves, `ZQ` discards, and baseline-only rows remain read-only.
While editing, cride creates `.cride/editing.json` as an advisory lock and
defers worktree reloads. The [editing guide](docs/review-expansion-and-editing.md)
documents the supported commands and conflict behavior.

Definitions and references work lexically through ripgrep out of the box.
cride starts these language servers lazily when their executable is available:

| Executable | Languages |
| --- | --- |
| `gopls` | Go |
| `rust-analyzer` | Rust |
| `clangd` | C and C++ |

The built-in outline fallback understands Go, Python, JavaScript/TypeScript,
Rust, and C/C++. `clangd` works best with a `compile_commands.json` database.

## Configuration and state

Optional configuration lives at `$XDG_CONFIG_HOME/cride/config` (normally
`~/.config/cride/config`):

```ini
theme = auto
chroma_style = monokai
```

`theme` accepts `auto`, `dark`, or `light`. `NO_COLOR` disables syntax
highlighting, and truecolor is detected through `COLORTERM`.

| Path | Purpose |
| --- | --- |
| `$XDG_STATE_HOME/cride/<repo-id>/session.json` | Per-review session state |
| `review.md` | Canonical editable review for a person or agent |
| `.cride/editing.json` | Temporary advisory edit lock |

Comment changes update `review.md` directly and `ctrl+s` forces an immediate
atomic save. You can also edit comment text, headings, severity, status, or
anchors in the file; `ctrl+r` reloads it, so cride does not need to be restarted
between review passes. Matching anchors retain their in-memory IDs and
timestamps. A missing file means an empty review. Malformed comment headings
are rejected without replacing the review currently in memory.

## Project documentation

| Document | Contents |
| --- | --- |
| [Architecture](DESIGN.md) | Current design, package boundaries, and invariants |
| [Keymap](docs/keymap.md) | Generated shortcut reference |
| [Editing guide](docs/review-expansion-and-editing.md) | Context expansion and editing behavior |
| [Dependencies](docs/dependencies.md) | Runtime, build, and optional dependencies |
| [Roadmap](tasks/README.md) | Active feature specifications |
| [Contributing](CONTRIBUTING.md) | Development workflow and pull-request checklist |
| [Security](SECURITY.md) | Vulnerability reporting policy |

## Development

```sh
make test     # go test ./...
make vet      # go vet ./...
make fmt      # format Go source
make docs     # regenerate docs/keymap.md
```

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request.

## License

cride is available under the [MIT License](LICENSE).
