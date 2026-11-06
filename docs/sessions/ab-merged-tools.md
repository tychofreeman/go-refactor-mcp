# A/B of the merged tools

- **Item:** the follow-up every record from #583 to #589 left: an A/B
  check of the merged descriptions (`move` with delete, `inline` with
  `field`, `move_statement` with `direction`, `change_type` covering the
  field and parameter tools), and `preserve_exceptions` on every
  refactoring's schema. The user chose it from /whats-next (2026-11-06).
- **Date:** 2026-11-06
- **Branch:** ab-merged-tools

## Plan

At the highest level: the published list changed (fewer tools, longer
merged descriptions, one more argument on every refactoring), and the
question is whether a fresh model still reaches the operation a task
needs, at no more refusals or turns. No code change unless a run shows
a cause.

- Baseline: the server from `c7bf1579` (main before #583), built with
  `-mcp`; current: main at `a3ee5870`.
- Tasks, each needing a merged tool: `move-to-package` and
  `multi-move-rename` (ab.json: move), `notify-userid` (mechanisms:
  change_type of a field), `warehouse-inline` (traps: inline and the
  delete of the helpers).
- Sonnet, arm mcp, 2 runs per task and server, CLAUDE.md
  `eval-tasks/claude-md-refactor-first-bash`: 16 sessions.
- Compare per run: pass, cost, turns, refusals, and which tool names
  the model called (old names through aliases count as a miss of the
  merged description).

## Notes

## Measured

## Assessment

## Follow-ups
