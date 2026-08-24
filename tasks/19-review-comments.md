# Review comment anchoring and threads

> Status: comment creation, severity, persistence, drift detection, resolution,
> navigation, and Markdown export are implemented. This task tracks stronger
> re-anchoring and richer thread metadata.

## Current behavior

- `c` creates a line-anchored comment and `C` creates a general comment.
- Comments carry `nit`, `question`, or `must-fix` severity.
- Changes write through atomically to the canonical, editable `review.md`.
- `ctrl+s`/`e` saves immediately; `ctrl+r` imports Markdown edits and reloads
  the diff without restarting cride.
- `]a`/`[a` navigate comments and `x` toggles resolution.
- Anchors store side and line ranges plus the original snippet.
- When anchored code drifts, the comment becomes unresolved and is never
  silently discarded.

The [architecture guide](../DESIGN.md) describes the current annotation model.
Editing remains separate and is covered by the [editing guide](../docs/review-expansion-and-editing.md).

## Remaining goals

### Content fingerprints

Extend an anchor with a compact fingerprint built from:

- hashes of the selected lines;
- a bounded window of surrounding line hashes; and
- enough path/side metadata to distinguish baseline from current content.

On every relevant diff reload, classify the anchor:

- **exact:** selected content remains at the recorded lines;
- **shifted:** a unique fingerprint match moved to another line range; or
- **unresolved:** no safe unique match exists.

Exact and shifted anchors stay attached. Unresolved anchors remain visible in
a detached bucket with their original path, range, snippet, and body. Ambiguous
matches must never be guessed or discarded.

### Provenance and threads

Consider adding backward-compatible fields for:

- tool version and repository fingerprint;
- replies or agent acknowledgements;
- explicit resolution author/time; and
- a reason for unresolved status.

The coding agent's source edit should remain the primary response; thread
features should not turn cride into a hosted discussion system.

## Format constraints

- Keep `review.md` saves atomic and its visible syntax backward-compatible.
- Treat Markdown reloads as lossy: preserve IDs and timestamps when anchors
  match, and reject malformed structural headings without replacing in-memory
  state.
- Load unknown prose safely and reject unsupported structural headings without
  destroying data.
- Preserve comments across file rename when the diff provides a reliable old
  and new path mapping.

## Tests

- Exact anchor remains attached after unrelated edits.
- Insertions above a comment produce a unique shifted match and updated range.
- Duplicate candidate windows become unresolved rather than choosing one.
- Deletion and later restoration detach and then heal an anchor.
- Baseline and current anchors never cross-match.
- File rename preserves a uniquely matched comment.
- A missing `review.md` loads as an empty review without creating a file.
- Export clearly distinguishes open, resolved, and unresolved comments.

## Open questions

- How large should the context window be?
- Should whitespace-only changes preserve an exact match or count as shifted?
- Are replies useful before agent acknowledgements can be imported reliably?
- Should resolving a `must-fix` comment require confirmation?
