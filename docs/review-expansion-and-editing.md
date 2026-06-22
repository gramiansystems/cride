# Review Expansion and Editing Vision

CRIDE should stay review-centered as code editing is added. The main pane is
still the review surface: compact diff, expanded context, and full-file context
are display policies over the same file, not separate panes with different
rules.

## Expansion Model

There are two expansion policies.

### Local Expansion

Local expansion is the default review flow. A file starts as a compact diff.
The user can expand context around individual hunks while CRIDE remembers those
per-file, per-hunk choices.

- `zo` expands the current hunk by one context step.
- `zc` collapses the current hunk by one context step.
- `zO` expands every hunk in the current file by one context step.
- `zC` clears local expansion for the current file.

Local expansion state is independent from full expansion. If a user expands
hunk A and leaves hunk B compact, toggling full expansion must not destroy that
local shape.

### Full Expansion

Full expansion is a toggle for the selected file in the same review pane. It
shows the whole current file while keeping review metadata inline:

- current-side changed lines keep their review highlight,
- hunk headers remain as anchors,
- deleted baseline-only lines are shown as read-only inline blocks,
- diagnostics, unread state, and future annotations remain attached to source
  coordinates.

`tab` and `zf` toggle full expansion. Toggling back restores the remembered
local expansion state instead of recalculating or clearing it. Expanding
re-anchors the cursor by current-file line while preserving its viewport row.
Collapsing remembers the full-file cursor: reopening without moving in the
diff resumes it, while a moved diff cursor becomes the next full-file anchor.
Hunk headers and deleted-only rows use the nearest current insertion point.

## Editing Rules

Editing follows source coordinates, not rendered row indexes.

- Editable rows are rows with a current-side source location.
- Read-only rows are baseline-only deleted rows, hunk headers, binary/error
  placeholders, and any future immutable baseline content.
- Insertions happen into the current file at the nearest valid current-side
  position (`o`/`O` on a read-only row use the nearest editable line).
- Deleted `before` blocks are evidence. They can be selected, navigated, and
  commented on, but never edited.

This keeps the review diff trustworthy while allowing the reviewer to patch the
current working tree from the same review surface.

## Editing Model (implemented)

Editing is modal, like vim, so no review binding is ever repurposed:

- **REVIEW** — the default. All review keys keep their meanings; character
  motions (`h`/`l`/arrows, `w`/`b`, `0`/`^`/`$`, `f`/`t`/`;`/`,`, `%`) are
  always available for reading.
- **EDIT** — vim normal mode. Entered via `i`/`I`/`a` (which continue into
  INSERT); `Esc` from INSERT lands here. Review bindings are suspended, so
  `x`, `o`/`O`, `A`, `D`/`C`, `dd`/`cc`/`yy`, `d`/`c`/`y`+motion, `p`/`P`,
  `r`, `s`/`S`, `J`, `e`, `u` and `ctrl+r` (redo) all carry their canonical
  vim meanings. Counts compose across operators (`2d3w` acts on six words).
  Operators take same-line motion targets (`w`/`e`/`b`/`$`/`0`/`^`/`h`/`l`)
  or the doubled key for lines; `f`/`t` targets and multi-line operator
  motions are not in the subset.
- **INSERT** — literal typing; Enter splits, Backspace joins, paste works, and
  arrow/Home/End keys move the insertion point.

Entering EDIT switches the file to full expansion (the only view where every
rendered row maps to the buffer) and restores the previous view on exit.
`ZZ` saves — an atomic temp-file-plus-rename write, after which the file is
re-stamped as read so your own save never flashes unread — and `ZQ` discards.
`Esc` on a dirty buffer warns instead of dropping edits. Undo history is
in-memory and per edit session.

## Pair-Programming Contract

The agent yields to the reviewer:

- Entering EDIT writes an advisory lock, `.cride/editing.json`
  (`{"path", "since"}`), removed on save/discard/quit and cleared at session
  start. Agent instructions should include: *"Before writing a file, check
  `.cride/editing.json` at the repo root; if it names that file, wait until
  it disappears."* Add `.cride/` to `.gitignore` alongside `.crreview`.
- While EDIT/INSERT is active, tree reloads are deferred (the footer shows
  the pending reload) and run when editing ends — an external write can
  never wipe an open buffer.
- If the agent writes the same file despite the lock, the reviewer's `ZZ`
  whole-file save wins. That is the chosen policy, and the lock exists to
  make the case rare.

Known simplifications: saving always writes a trailing newline, and review
markers (hunk anchors, deleted blocks) can drift visually while a buffer has
unsaved line insertions/deletions — the save-triggered re-diff re-syncs them.
