---
name: ab-analyze
description: Analyze an A/B run of cmd/refactor-ab (eval-runs/NAME): where its turns and tokens went, what was refused, what reading cost, compared with a baseline run. Use after an A/B run, or to compare two runs.
---

# Analyze an A/B run

Arguments: the run directory (`eval-runs/NAME`), and optionally a
baseline run to compare with. Without a baseline, take the latest earlier
row of the same task set in `docs/cost-history.md`, if its directory is
still in `eval-runs/`.

## 1. Totals

Read `eval-runs/NAME/summary.md`: passed, cost, input tokens and turns
per task and arm, means per run. Runs marked unfinished (cut off by the
timeout) are left out of the means; say how many there were.

## 2. Turns, refusals and reads

Run, for the run and the baseline, once per arm if the run has several
(the transcripts are `TASK-ARM-N.jsonl`; pass them by glob):

    go run ./cmd/refactor-tokens eval-runs/NAME/*-mcp-*.jsonl

Its tables, summed over the transcripts given:

- **tool:** each tool's calls and tokens; `context` counts a call's
  tokens once per later request that sent them again.
- **turns:** turns with tool calls; turns that only loaded schemas
  (ToolSearch); turns with a refused call, by the kind of the first:
  Write or Edit disabled (the mcp arm), Bash that writes or reads
  refused (the arm's allowlist: in `claude -p`, Bash with loops,
  variables or brace expansion is refused, where a user would get a
  prompt; count it apart from the tools' waste), gorefactor refused.
- **read:** what reading cost, by kind: `inspect` (module overview, file,
  part, targets), Read, Grep, Glob, and Bash other than go and git.

Divide by the number of transcripts for per-run figures.

## 3. Report

A table of the run against the baseline: cost, input tokens, requests,
passed; per run: turns, ToolSearch-only turns, refused turns by kind, and
the largest read kinds by context. Then what changed and why, from the
transcripts where a number moved: read the turns around a refusal to see
what the model was trying to do.

Keep in mind (CLAUDE.md, "Measuring cost"):

- Noise is 10-25% at 2-3 runs per task; a change smaller than that in
  the totals is not evidence. The waste a fix targets, counted per run,
  is.
- Count turns, not calls: parallel calls in one turn cost no extra turn.
- Input tokens are about requests times the context of a request (about
  39K in the feature runs), so fewer turns is what cuts cost.

## 4. Record

In the PR of the work being measured, or one of its own: a row in
`docs/cost-history.md` (run, change, cost · input · passed) with a
paragraph on what moved, and the finding in `docs/session-notes.md` under
"Where the work stands" if a later session needs it.
