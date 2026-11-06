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

- plan: the six sets first ran at once and four were killed (the
  machine ran out of memory compiling the TypeScript checker six times);
  rerun one at a time with both servers built once.

## Measured

No session log (a cloud session started outside the checkout). No Go
was changed: the CLI's calls were replace_text and create_file on docs.
The runs, means per run (`docs/cost-history.md`, section 24):

| task | before: cost | turns | after: cost | turns |
|---|---|---|---|---|
| move-to-package, multi-move-rename | $0.113 | 8.0 | $0.098 | 6.0 |
| notify-userid | $0.236 | 14.5 | $0.169 | 14.0 |
| warehouse-inline | $0.148 | 11.0 | $0.157 | 13.5 |

16/16 passed; $2.26 in all.

## Assessment

The merged list holds: every run reached the operation through its
merged name (`change_type` for a field, where the baseline called
`change_field_type`), none called an old name, and nothing was refused.
Cost moved within the noise of 2 runs. The tasks did not exercise
`move` with `delete`, `inline` with `field`, `move_statement` or
`preserve_exceptions`; no task in the sets needs them.

## Follow-ups

- A task that needs `move` with `delete` or `inline` with `field`, if a
  later change to those descriptions needs measuring.
