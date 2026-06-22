# cride Active Feature Tasks

This directory tracks roadmap tasks that are not fully implemented. Completed
implementation notes live in the [architecture guide](../DESIGN.md).

## Active tasks

1. [`03-find-usages-and-definition.md`](03-find-usages-and-definition.md)
   - LSP and lexical `rg` lookup exist. Keep this task for any future
     syntax-aware fallback between those tiers.
2. [`05-review-aware-navigation.md`](05-review-aware-navigation.md)
   - Review-ranked results exist. Keep this task for deeper unread/comment
     marker integration, introduced-diagnostics behavior, and related-test
     discovery.
3. [`07-change-impact.md`](07-change-impact.md)
   - Untouched/updated reference partitioning, signature-change hints, and
     orphan/dangling-reference detection.
4. [`08-baseline-workspace.md`](08-baseline-workspace.md)
   - Read-only baseline worktree, baseline LSP, introduced/fixed diagnostics,
     and before-side hover/definition.
5. [`09-semantic-diff.md`](09-semantic-diff.md)
   - Move detection, format-only suppression, and intraline highlighting.
6. [`10-review-graph.md`](10-review-graph.md)
   - Cross-hunk relations and suggested callee-first review order.
7. [`11-blame-and-tests.md`](11-blame-and-tests.md)
   - Baseline blame risk layer and per-symbol test linkage.
8. [`19-review-comments.md`](19-review-comments.md)
   - Comment v0 is implemented. Keep this task for content fingerprints,
     exact/shifted/unresolved remapping, and full v1 drift handling.

## Completed specs removed

The completed task specs for navigation foundation, search/open, LSP
enrichments, render foundation, status feedback, theming, in-file search,
panel interaction, side-by-side diff, live watch/unread, session persistence,
and symbol outline diff/breadcrumbs were removed after their durable decisions
were folded into the [architecture guide](../DESIGN.md).

## Design principles

- Preserve the core static binary path. Semantic navigation remains optional.
- Keep the UI responsive. Background workers send messages into Bubble Tea.
- Treat the working tree as unstable. Agent edits can leave code temporarily
  broken.
- Prefer degraded useful behavior over all-or-nothing precision.
- Keep review context first. IDE features should help evaluate a diff, not turn
  `cride` into a general editor.
