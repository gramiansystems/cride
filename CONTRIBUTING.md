# Contributing to cride

Thanks for helping improve cride. The project welcomes focused bug fixes,
tests, documentation, and features that reinforce its review-first workflow.

## Before starting

For a substantial feature, open an issue before investing heavily so the user
experience and package boundary can be agreed first. The active design work in
[`tasks/`](tasks/README.md) is useful context, but a task document is not a
promise that every proposed detail will be accepted unchanged.

Please keep changes focused. Refactors are easiest to review when they are
separate from behavior changes.

## Development setup

You need:

- Go 1.24 or newer;
- Git.

Ripgrep is optional. When installed, it provides the fast path for live
worktree search; tests also cover the Git-based fallback.

Optional language servers are listed in [the dependency guide](docs/dependencies.md).

Fork and clone the repository using your hosting provider, then initialize the
development environment from the checkout:

```sh
go mod download
make test
```

## Repository map

- `cmd/cride` contains the command-line entry point.
- `internal/app` owns the Bubble Tea state machine and feature coordination.
- `internal/ui` turns review state into terminal rows, panes, and overlays.
- `internal/diffsource` separates review behavior from Git/worktree access.
- `internal/diff`, `annotate`, `search`, `outline`, `lsp`, and `session` hold
  focused domain logic.
- `docs` contains user-facing reference material.
- `tasks` contains active feature specifications and open design work.

See [DESIGN.md](DESIGN.md) for package responsibilities and invariants.

## Making a change

1. Add or update tests with the behavior change.
2. Keep filesystem, Git, search, and language-server work out of Bubble Tea's
   synchronous `Update` and `View` paths.
3. Run formatting and regenerate the keymap when bindings change:

   ```sh
   make fmt
   make docs
   ```

4. Run the same core checks as CI:

   ```sh
   make test
   make vet
   make build
   go mod tidy -diff
   ```

`docs/keymap.md` is generated from `internal/keymap`; edit the Go source rather
than the Markdown file.

## Tests

Prefer small state-machine tests for interaction behavior and table-driven
tests for parsing/ranking logic. Worktree tests should use temporary Git
repositories and must not depend on a contributor's global Git configuration.

When changing rendering, cover narrow terminal widths, wrapped rows, both
diff sides, and source-coordinate preservation where relevant.

## Pull requests

A pull request should:

- explain the user-visible problem and the chosen behavior;
- include tests or explain why no test is practical;
- update user and architecture docs when behavior or boundaries change;
- keep generated documentation and `go.mod`/`go.sum` tidy; and
- pass formatting, tests, vet, and build checks.

By contributing, you agree that your contribution is licensed under the
project's [MIT License](LICENSE).
