# Cost history

What it costs a model to make changes with gorefactor, against making them
by hand, and how that changed as the tools did. The goal (README, "Status")
is that the MCP costs fewer tokens than editing by hand.

## How it is measured

`cmd/refactor-ab` gives the same task to fresh Claude sessions (Sonnet) in
a copy of a module and checks the result with hidden tests. Each task runs
in up to three arms:

- **manual:** built-in tools only (Read, Edit, Bash, ...), no gorefactor.
- **mcp:** gorefactor only; Edit and Write are disabled and Bash may not
  change files.
- **hybrid:** both, with a CLAUDE.md that says to use gorefactor for
  refactorings.

Results are in `eval-runs/<name>/` (gitignored): one transcript per run
and `results.jsonl`. Totals below are sums over all runs of an arm.

**Noise.** The same arm on the same tasks moved by about ±10% in cost
between runs (manual: $1.73 → $1.50 between two runs with no change that
affected it). Input tokens are steadier than cost, which also depends on
cache writes. Read differences under about 10% as noise.

## 1. Named refactorings (Oct 18)

Eight tasks that name a refactoring (rename a field, add a parameter,
move to a package, ...), on this repo at 508d107, 3 runs each.

| Run | Change | mcp | manual |
|---|---|---|---|
| ab-full-1 | first baseline | $1.87 · 2.22M | $1.66 · 2.03M |
| ab-compact-1 | diffs without context lines (#75) | $1.77 · 2.16M | $1.67 · 2.08M |
| ab-locations-1 | renames list locations; "no need to search first" | $1.60 · 2.08M | $1.73 · 2.07M |
| ab-batch-1 | batch tool (#77) | $1.52 · 2.12M | $1.50 · 2.05M |

All 192 runs passed. Results were the main cost: a rename returned ~6.9k
tokens per call at first, 4.4k with locations instead of diffs. The MCP
reached parity in input tokens. The model never used batch: every task was
one or two steps.

## 2. Multi-step tasks (Oct 18)

Three tasks of dependent steps, mcp arm only, 5 runs each.

| Run | Change | mcp |
|---|---|---|
| ab-multi-prebatch | before batch | $1.18 · 1.95M · 102 turns |
| ab-multi-batch | batch tool | $1.10 · 1.66M · 92 turns |

Batch cut input tokens by 15%; the model used it in every run of the
dependent tasks.

## 3. Feature tasks (Oct 18–19)

Four features of this repo, with hidden acceptance tests, 2 runs each
(1 for the first two rows). The mcp arm must make every change, not only
refactorings, through gorefactor.

| Run | Change | mcp | hybrid | manual |
|---|---|---|---|---|
| ab-features-calibrate | no CLAUDE.md (1 run) | $0.72 · 1.42M | $0.73 · 1.30M | $0.65 · 1.17M |
| ab-features-guided-1 | CLAUDE.md per arm (1 run) | $1.38 · 4.34M | $0.78 · 1.74M | $0.72 · 1.50M |
| ab-features-refactor-first | refactor-first CLAUDE.md | $2.50 · 6.91M · 7/8 | $1.46 · 3.01M · 8/8 | $1.41 · 2.71M · 7/8 |
| ab-features-mcp-overhead | replace_text, argument lists in refusals (#80) | $2.07 · 5.53M · 7/8 | | |
| ab-features-2026-10-19 | batch refs, check "end", more tools | $2.83 · 6.77M · 8/8 | | |
| ab-features-defaults | #113–#115: see below | $2.31 · 4.91M · 7/8 | | |
| ab-features-fixes | #116–#117: stubs replaced, edits on code they fix, add_field type | $2.56 · 6.03M · 6/8 | | |
| ab-features-coaching | #119: a tip to batch on the 2nd single step in a row (since removed) | $2.42 · 4.97M · 7/8 | | |

Without a CLAUDE.md no arm used gorefactor at all: all three edited with
Bash. Doing everything through gorefactor cost about 1.8× by hand at first,
1.5× after #80. The Oct 19 tools made a fresh model's runs dearer: it
guessed the argument names of tools it only used inside batches, sent line
ranges that cut a statement, and sent calls of functions not yet written
without `create_from_usage`. The fixes for those (#113–#115) brought input
tokens 27% down, below the earlier baseline:

- a batch checks every step's argument names before running any (#113);
- a range that cuts a statement is refused with the range to send (#113);
- a batch whose edit does not type-check runs again with check "end"
  (#114);
- functions and methods an edit calls that nothing declares get stubs
  (#114);
- an unknown argument name that can only mean one argument is taken as it
  (#114);
- lines are followed through each step's own edits (#115).

The tip to batch (#119) was removed after one run. It fired 9 times and
the model never batched after it; the 18% drop is noise. Its premise was
wrong: counted by calls, single steps came in runs of 3 to 10, but most
of those were sent in parallel in one turn, which costs nothing extra.
Counted by turns, a turn of steps right after another one (which a batch
could have saved) happened about once per run.

## 4. Design in the way, and chains (Oct 18–19)

Four tasks on a seeded module (`eval-tasks/testdata/shop`, ~300 lines)
whose design gets in the way of the change, 3 runs each. Chains add a
second change to the same code, to see whether refactoring first pays
back.

| Run | Change | hybrid | manual | plain manual |
|---|---|---|---|---|
| ab-design-refactor-first | one change, refactor first | $1.46 · 2.48M | $1.12 · 1.54M | $1.03 · 1.39M |
| ab-design-hybrid-translate | batch line translation (#82) | $1.43 · 2.61M | | |
| ab-chain-refactor-first | two changes, refactor first | $2.47 · 4.42M | $2.18 · 3.43M | $1.73 · 2.83M |
| ab-chain-when-needed | refactor only when needed | $2.24 · 3.48M | $1.95 · 2.85M | $1.79 · 3.00M |
| ab-chain-when-needed-2026-10-19 | Oct 19 tools | $2.71 · 3.91M | $2.13 · 2.82M | |
| ab-chain-defaults | #113–#115 | $2.68 · 3.85M | $2.11 · 2.86M | |
| ab-chain-fixes | #116–#117 | $2.84 · 4.10M | $2.15 · 2.86M | |

With the design in the way, the hybrid arm refactored in every run, with
the fitting tool. But on a module this small, refactoring first did not
make the second change cheaper, and plain editing stayed cheapest. The
hybrid arm makes its edits with Edit and uses gorefactor only to refactor,
so #113–#115 did not change it.

**#116–#117 (Oct 19).** Each fix did what it was for: in the features,
stubs were replaced 10 times and no `add_declaration` was refused as
redeclared, and `add_field` took the type separately. But both sets cost
more than the run before: features 4.91M → 6.03M input, with more
reading (Read 9 → 15, Grep 8 → 12), more `check` calls (9 → 16) and more
`Write` attempts (6 → 16), none of which the fixes touch; chains' hybrid
3.85M → 4.10M, manual flat. Both failed features were the interface
method case that fails in earlier runs too. At 2–3 runs per task, swings
of this size come from how a run goes, not from the tools: a fix that
removes a few refusals per run is below what these sets can show.
Fewer refusals and turns lost per run measure it better than the total.

## 5. Sessions building this project (Oct 17–19)

`refactor-tokens` over the transcripts of the sessions that built
gorefactor, per edit step (a batch step, a single tool call, or an Edit or
Write):

| Period | Share of edits through gorefactor | Requests per step | Input tokens per step |
|---|---|---|---|
| Oct 17 | 10–20% | ~10 | 1.2–1.9M |
| Oct 18 (before batch) | 80–99% | 2–4 | 0.5–1.8M |
| Oct 19 (batch, check "end", references) | 99–100% | 0.7–0.9 | 170–190k |

A model that knows the tools well spends six to ten times less per step.
A fresh model gets little of that. Sections 3 and 4 measure it, and the
tools now do more of what that knowledge did (#113–#115).

## What made the difference

- **Smaller results** (#75, rename locations): the largest single win when
  results were most of the cost.
- **Batching dependent steps** (#77, #82): fewer turns, but only on tasks
  with several dependent steps.
- **Doing for the model what it would otherwise get wrong** (#113–#115):
  checking arguments up front, suggesting ranges, retrying with check
  "end", declaring stubs.
- **Not:** more tools or longer descriptions. Each adds to every request's
  context, and a fresh model uses few of them.

Next (README, roadmap item 15): ranges that start at a function's `func`
line (5 refusals in the last chain run) answered with the body's lines,
the reading before the first edit (Bash, Grep and Read were about two
thirds of the tool-call input in the last feature run), and more runs per
task so that effects of 10% can be seen. Count waste by turns, not calls:
parallel calls in one turn are not waste.
