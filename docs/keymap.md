<!-- Generated from internal/keymap; run `make docs` after editing. Do not edit by hand. -->

# cride key bindings

Press `?` (or `F1`, `g?`) inside cride for the categorized, executable command palette; use Tab/Shift-Tab to switch categories.

## Moving around

| Keys | Action |
| --- | --- |
| `j/k, ↑/↓` | Move the cursor by line |
| `h/l, ←/→` | Move the cursor by character |
| `w/b` | Move to the next/previous word |
| `0/^/$` | Line start / first non-blank / line end |
| `f/F/t/T, ;/,` | Find a character on the line; ;/, repeat |
| `%` | Jump to the matching bracket |
| `ctrl+d/ctrl+u` | Scroll half a page |
| `ctrl+f/ctrl+b, pgup/pgdown` | Scroll by one page |
| `H/L` | Move to the top or bottom visible row |
| `gg, home / G, end` | Jump to the start or end of the file |
| `{count}G` | Jump to a source line |

## Files & panes

| Keys | Action |
| --- | --- |
| `{/}, J, ]]/[[` | Move between changed files |
| `ctrl+h / ctrl+l` | Focus the change list / the diff |
| `o` | Toggle file list path/change order |
| `h/l` | Fold/unfold directories when the list has focus |

## Hunks & unread

| Keys | Action |
| --- | --- |
| `]c/[c` | Move between hunks |
| `n/N, shift+tab` | Next/previous unread file (matches while searching) |
| `R/U/A` | Mark current read+next / unread / all read |

## View

| Keys | Action |
| --- | --- |
| `zo/zc` | Expand or collapse context around the current hunk |
| `zO/zC` | Expand or collapse context for all hunks in the file |
| `tab, zf` | Toggle full-file view |
| `zs` | Toggle side-by-side diff |
| `ctrl+r` | Reload the diff and import review.md |

## Editing (vim-style, current-side lines only)

| Keys | Action |
| --- | --- |
| `i/I/a` | Enter insert mode (before cursor / line start / after cursor) |
| `esc` | Insert → edit mode; edit → review when nothing is unsaved |
| `A, o/O` | Edit mode: append at line end, open a line below/above |
| `arrows, home/end` | Insert mode: move the insertion point |
| `x, D/C, d/c/y + motion` | Edit mode: delete, change, yank (counts supported; dd/cc/yy for lines) |
| `r, s/S, J` | Edit mode: replace, substitute, or join lines |
| `p/P` | Paste the register after/before the cursor |
| `u / ctrl+r` | Undo / redo (edit mode) |
| `ZZ / ZQ` | Save to the working tree / discard, back to review |

## Comments & review file

| Keys | Action |
| --- | --- |
| `c/C` | Comment on the current line / general comment |
| `]a/[a` | Next/previous comment |
| `x` | Toggle a comment resolved |
| `ctrl+s / e` | Save review.md without leaving cride |

## Search & open

| Keys | Action |
| --- | --- |
| `ctrl+p` | Open a file by fuzzy name |
| `/` | Search within the current file (n/N step, esc clears) |
| `g/` | Search the project |

## Code intelligence

| Keys | Action |
| --- | --- |
| `gd` | Go to definition |
| `gr/gR` | Find references, or references in changed files |
| `←/→` | Pick the highlighted symbol when a line has several |
| `gi` | Show changed-symbol impact |
| `gs/gS` | Document or workspace symbols |
| `gy` | Changed-symbol outline (s toggles file/review) |
| `ge/gE` | Current-file or workspace diagnostics |
| `gI/gO` | Incoming or outgoing calls |
| `K` | Hover documentation for the symbol under the cursor |
| `ctrl+o / ctrl+]` | Jump back / forward in the jump history |

## Mouse

| Keys | Action |
| --- | --- |
| `wheel` | Scroll the code pane or active result panel |
| `click file list` | Select a changed file |
| `click code row` | Move the cursor to that row |

## App

| Keys | Action |
| --- | --- |
| `?, f1, g?` | Open the categorized command palette |
| `tab / shift+tab` | Switch command-palette category |
| `esc` | Close panels and prompts, clear the in-file search |
| `q, ctrl+c` | Quit |
