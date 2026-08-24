# Security policy

## Supported versions

cride is currently pre-1.0. Security fixes are applied to the latest code on
the default branch; older snapshots are not maintained as separate supported
release lines.

## Reporting a vulnerability

Please do not open a public issue for a vulnerability that could put users or
their repositories at risk. Use the repository's private vulnerability
reporting feature on GitHub instead. Include:

- the affected revision or release;
- the operating system and relevant tool versions;
- steps to reproduce the issue;
- the expected impact; and
- any suggested mitigation, if known.

If private vulnerability reporting is not enabled yet, contact the repository
owner privately through their published organization contact and avoid sharing
exploit details in public channels.

The maintainers will acknowledge a report, investigate it, and coordinate a
fix and disclosure timeline appropriate to the severity. Please allow time for
a patch to be prepared before publishing details.

## External dependencies

Building cride from source requires Go 1.24.2 or later. Running cride requires
`git` on `PATH`. The following executables are optional and enable the noted
integrations when they are present:

| Executable | Purpose |
| --- | --- |
| `rg` (ripgrep) | Faster project and lexical-symbol searches; cride falls back to `git grep` when it is unavailable. |
| `gopls` | Go language-server features. |
| `rust-analyzer` | Rust language-server features. |
| `clangd` | C and C++ language-server features. |

The source build uses these direct Go modules, pinned in `go.mod`:

| Module | Version |
| --- | --- |
| `github.com/alecthomas/chroma/v2` | `v2.23.1` |
| `github.com/charmbracelet/bubbles` | `v1.0.0` |
| `github.com/charmbracelet/bubbletea` | `v1.3.10` |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` |
| `github.com/fsnotify/fsnotify` | `v1.10.1` |
| `github.com/mattn/go-runewidth` | `v0.0.19` |
| `github.com/muesli/reflow` | `v0.3.0` |
| `github.com/sourcegraph/go-diff` | `v0.8.0` |

The module graph also includes these pinned indirect requirements. Some are
platform-specific and might not be linked into every release binary:

```text
github.com/atotto/clipboard v0.1.4
github.com/aymanbagabas/go-osc52/v2 v2.0.1
github.com/charmbracelet/colorprofile v0.4.1
github.com/charmbracelet/x/ansi v0.11.6
github.com/charmbracelet/x/cellbuf v0.0.15
github.com/charmbracelet/x/term v0.2.2
github.com/clipperhouse/displaywidth v0.9.0
github.com/clipperhouse/stringish v0.1.1
github.com/clipperhouse/uax29/v2 v2.5.0
github.com/dlclark/regexp2 v1.11.5
github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f
github.com/lucasb-eyer/go-colorful v1.3.0
github.com/mattn/go-isatty v0.0.20
github.com/mattn/go-localereader v0.0.1
github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6
github.com/muesli/cancelreader v0.2.2
github.com/muesli/termenv v0.16.0
github.com/rivo/uniseg v0.4.7
github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e
golang.org/x/sys v0.38.0
golang.org/x/text v0.3.8
```

`go.mod` and `go.sum` are the authoritative dependency and checksum records.
The complete resolved module graph, including modules used only by dependency
tests or tools, can be inspected with `go list -m all`. See
[`docs/dependencies.md`](docs/dependencies.md) for dependency purposes,
platform notes, and refresh commands.

## Security boundaries

cride operates on local source repositories and executes `git`, optional `rg`,
and optional language servers found on `PATH`. It does not make
application-level network requests, but those external tools process repository
content and use the current user's permissions. Review untrusted repositories
and executables with the same care you would use for other local developer
tooling.
