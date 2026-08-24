# External Dependencies

This inventory covers dependencies outside this repository: Go modules,
runtime executables, optional language-server integrations, and terminal/OS
surfaces. It was prepared from `go.mod`, `go list`, `go version -m`, and
searches for `exec.Command`, `exec.LookPath`, and environment variable use.

cride itself does not make network requests at runtime. Network access is
normally needed only to download the Go toolchain or modules, or to install
optional executables.

## How to Refresh

Run these after adding imports, runtime commands, or release artifacts:

```sh
go mod tidy -diff
go list -m all
go list -deps -f '{{with .Module}}{{if ne .Path "cride"}}{{.Path}} {{.Version}}{{end}}{{end}}' ./cmd/cride | sort -u
rg -n 'exec\.Command|LookPath|os\.Getenv|XDG_|NO_COLOR|COLORTERM' internal cmd
go version -m ./cride
```

`go list -deps` is platform-sensitive. For release builds, run it for each
target `GOOS`/`GOARCH` pair that will be shipped.

## Runtime and Toolchain Dependencies

| Dependency | Required for | Code path | Failure behavior |
| --- | --- | --- | --- |
| Go 1.24+ | Building, testing, and installing from source | `go.mod`, `Makefile` | Not needed when running a prebuilt binary. |
| `git` | All review sources and repository discovery | `internal/diffsource/worktree/vcs.go` | Hard requirement. Startup/source opening fails if `git` is unavailable or the target is not a Git repository. |
| `rg` / ripgrep | Project search on the live worktree side, plus lexical symbol lookups that route through project search | `internal/diffsource/worktree/source.go` | Review still works. Search returns `ripgrep not found: install rg to use project search`. Baseline/ref searches use `git grep`. |
| `gopls` | Go LSP enrichments: diagnostics, hover, symbols, and call hierarchy | `internal/lsp/config.go`, `internal/lsp/process.go` | Optional. The LSP client reports the server unavailable and the app keeps working with lexical fallbacks. |
| `rust-analyzer` | Rust LSP enrichments for `.rs` files | `internal/lsp/config.go`, `internal/lsp/process.go` | Optional, same unavailable/degraded behavior as `gopls`. |
| `clangd` | C/C++ definitions, references, diagnostics, hover, symbols, and call hierarchy | `internal/lsp/config.go`, `internal/lsp/process.go` | Optional. Uses the project's compilation database when available; C/C++ lexical navigation and vtable-aware outlines remain available without it. |
| Filesystem event backend | Low-latency live reloads | `github.com/fsnotify/fsnotify`, `internal/diffsource/worktree/watch.go` | If watcher registration fails, the app falls back to the fingerprint poll loop in `internal/app/livewatch.go`. |
| Terminal capabilities | TUI alternate screen, mouse cell motion, colors, width calculations | Bubble Tea, Lip Gloss, Termenv, Chroma | Poor terminal support affects rendering/input quality, not diff correctness. |

Application runtime does not call HTTP APIs or remote services. Network access
is only a build/development concern when the Go tool downloads modules.

## Environment and Filesystem Interfaces

| Surface | Purpose |
| --- | --- |
| `$XDG_CONFIG_HOME/cride/config` or `~/.config/cride/config` | Optional config for `theme` and `chroma_style`. |
| `$XDG_STATE_HOME/cride/<repo-id>/session.json` or `~/.local/state/cride/<repo-id>/session.json` | Per-review session state. |
| `NO_COLOR` | Disables syntax highlighting. |
| `COLORTERM` | Enables truecolor output when it contains `truecolor` or `24bit`. |
| `review.md` | Repository-local canonical review, editable by an agent or reviewer. |

## Direct Go Modules

These are imported by CRIDE code and should remain direct requirements in
`go.mod`.

| Module | Version | Primary use | Notes |
| --- | --- | --- | --- |
| `github.com/alecthomas/chroma/v2` | `v2.23.1` | Pure-Go syntax highlighting in `internal/highlight`. | Chroma parses source text for display only. `NO_COLOR` disables highlighting. |
| `github.com/charmbracelet/bubbles` | `v1.0.0` | Comment composer textarea. | Imported directly by `internal/app/comments.go`; keep it direct rather than `// indirect`. |
| `github.com/charmbracelet/bubbletea` | `v1.3.10` | TUI program loop, commands, messages, key/mouse events. | Core app architecture depends on Bubble Tea message flow. |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` | Styling, layout, color, and terminal width helpers. | Also drives terminal background detection at startup. |
| `github.com/fsnotify/fsnotify` | `v1.10.1` | Recursive worktree and `.git` metadata watching. | Watch limits vary by OS; CRIDE has a polling fallback. |
| `github.com/mattn/go-runewidth` | `v0.0.19` | Display-width calculations for search/symbol UI. | Important for wide runes and aligned terminal rendering. |
| `github.com/muesli/reflow` | `v0.3.0` | Wrapping and truncating terminal text. | Used in the main renderer, footer, overlays, and change list. |
| `github.com/sourcegraph/go-diff` | `v0.8.0` | Parse unified `git diff` output into review model hunks. | Coupled to the diff shape produced by the `git` CLI. |

## Modules Compiled Into the Current CLI

For the current Linux `./cmd/cride` build, `go list -deps` resolves these
external modules into the binary package set:

```text
github.com/alecthomas/chroma/v2 v2.23.1
github.com/atotto/clipboard v0.1.4
github.com/aymanbagabas/go-osc52/v2 v2.0.1
github.com/charmbracelet/bubbles v1.0.0
github.com/charmbracelet/bubbletea v1.3.10
github.com/charmbracelet/colorprofile v0.4.1
github.com/charmbracelet/lipgloss v1.1.0
github.com/charmbracelet/x/ansi v0.11.6
github.com/charmbracelet/x/cellbuf v0.0.15
github.com/charmbracelet/x/term v0.2.2
github.com/clipperhouse/displaywidth v0.9.0
github.com/clipperhouse/stringish v0.1.1
github.com/clipperhouse/uax29/v2 v2.5.0
github.com/dlclark/regexp2 v1.11.5
github.com/fsnotify/fsnotify v1.10.1
github.com/lucasb-eyer/go-colorful v1.3.0
github.com/mattn/go-isatty v0.0.20
github.com/mattn/go-runewidth v0.0.19
github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6
github.com/muesli/cancelreader v0.2.2
github.com/muesli/reflow v0.3.0
github.com/muesli/termenv v0.16.0
github.com/rivo/uniseg v0.4.7
github.com/sourcegraph/go-diff v0.8.0
github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e
golang.org/x/sys v0.38.0
```

Most compiled transitive modules come from the terminal stack: ANSI parsing,
termios, Unicode/grapheme width, terminal color profiles, clipboard/OSC52, and
cancellable input readers. `github.com/dlclark/regexp2` is pulled in by
Chroma.

## Full Module Graph Notes

`go list -m all` is larger than the binary package set because it includes
dependency test modules, optional dependency modules, and MVS-selected module
requirements. At the time of this inventory, graph-only modules include:

```text
github.com/MakeNowJust/heredoc v1.0.0
github.com/alecthomas/assert/v2 v2.11.0
github.com/alecthomas/repr v0.5.2
github.com/aymanbagabas/go-udiff v0.3.1
github.com/bits-and-blooms/bitset v1.24.4
github.com/charmbracelet/harmonica v0.2.0
github.com/charmbracelet/x/exp/golden v0.0.0-20241011142426-46044092ad91
github.com/dustin/go-humanize v1.0.1
github.com/google/go-cmp v0.5.2
github.com/hexops/gotextdiff v1.0.3
github.com/kylelemons/godebug v1.1.0
github.com/sahilm/fuzzy v0.1.1
golang.org/x/exp v0.0.0-20231006140011-7918f672742d
golang.org/x/mod v0.6.0-dev.0.20220419223038-86c51ed26bb4
golang.org/x/text v0.3.8
golang.org/x/tools v0.1.12
```

Do not treat graph-only modules as CRIDE feature dependencies without checking
`go list -deps` for the relevant target platform.

## Dependency Risk Notes

- The runtime trust boundary is local: CRIDE executes `git`, `rg`, and optional
  language-server commands from `PATH` against user workspaces.
- The application currently has no runtime network dependency.
- The source tree has no explicit `cgo` imports. A release binary should still
  be checked with `go version -m` because the final `CGO_ENABLED` value is set
  by the build environment.
- Before a release, run `go mod tidy -diff`, `go test ./...`, and a vulnerability
  scan such as `govulncheck ./...`. No vulnerability or license scanner is
  wired into this repository today.
