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
| ab-features-skeleton | #121: inspect without bodies, targets | $2.62 · 6.27M · 7/8 | | |
| ab-features-read-inspect | mcp arm's CLAUDE.md: read Go code with inspect, not Read, cat or sed | $2.42 · 5.55M · 7/8 | | |
| ab-features-read-questions | the same, and a "Q:" line before each read saying what it is to answer | $2.38 · 5.12M · 7/8 | | |
| ab-features-toolsearch | #128: load schemas alongside another call; shorter definitions | $2.46 · 5.54M · 8/8 | | |
| ab-features-denied | #133–#134: create_file for any file in the instructions; replace_text `files` | $2.34 · 5.21M · 7/8 | | |

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

**#121 (Oct 20).** Where the model used `inspect`, it read less: a
file's view was 3.0-6.6K characters where it had been 9-15K, and
`inspect` calls went from 3 to 10 (one with `targets`), whole-file Reads
from 8-10 to 4. But reading did not get cheaper in total (3.1M
characters carried, weighted by turns, against 3.0M and 4.0M in the two
runs before), and input tokens did not fall (6.27M): the model moved its
reading to Bash, where it reads several files a turn with wide `sed -n`
ranges (`sed -n 80,330p refactor.go; sed -n 1,200p createfile.go`),
often after an `inspect` gave it the lines. The tool works; the habit
does not follow from a description. The mcp arm's CLAUDE.md says nothing
about reading, which a user's CLAUDE.md would.

**Reading through inspect (Oct 20).** With one paragraph in the mcp arm's
CLAUDE.md (`eval-tasks/claude-md-refactor-first-inspect`) saying to read
Go code with `inspect`, not Read, `cat` or `sed`, the model did: 32
`inspect` calls (14 with `targets`), 3 Reads, almost no `cat` or
`sed -n`. But it read as much as before (3.2M characters carried,
weighted by turns), now through wide `inspect` ranges such as lines
66-224 or 125-380. How much a model reads is set by what it wants to
understand, not by the tool it reads with.

What sets input tokens is the number of requests. Across the last four
feature runs, input tokens per request stayed at 38.5-40K (6.03M/151,
4.97M/129, 6.27M/157, 5.55M/141), and the fixed context of a request
(system prompt, tool list, CLAUDE.md) is about 21K of it. Fewer turns,
not smaller reads, is what would cut cost now. In this run about a
fifth of the turns went to things the mcp arm cannot do or need not do:
15 Write attempts and 18 Bash commands that write, which it is denied,
and 16 ToolSearch calls to load schemas.

**What the reads are for (Oct 20).** In `ab-features-read-questions`
the mcp arm's CLAUDE.md (`eval-tasks/claude-md-read-questions`) also
asks for a line "Q: ..." before each read, saying what it is to answer.
The run cost about the same (5.12M input, 130 requests, 39K each), so
asking did not change how it read much. Its 79 reads, sorted by
question, with their cost carried through later turns:

| Question | Reads per run | Share of read cost |
|---|---|---|
| What is in this module, package or file? | 2.8 | 41% |
| How does the code I will change work? | 2.4 | 30% |
| Where is X, who calls it, where is it made? | 2.6 | 22% |
| How are the existing tests written? | 2.1 | 8% |

The module overview (`inspect` with no arguments) alone was 17%: about
8.9K characters, read at the start of 4 of the 8 runs and so carried
through every later turn. Four broad greps (7-12K characters each, such
as `\.Line\b`) were another 12%, all "where is X used" questions.
All reads together are about 0.8M of the 5.1M input tokens, so even
halving them would save under 10%; turns still matter more.

**Schemas loaded alongside another call (#128, Oct 20).** Same arm
CLAUDE.md as `ab-features-read-inspect`. Turns that did nothing but
load schemas fell from 9 to 5 (12 in `ab-features-read-questions`), and
turns that loaded them together with another call rose from 7 to 9; two
of the five were `create_file`. Totals did not move (5.54M input against
5.55M, 8/8 passed), as a change of four turns in 130 would not. The
largest waste left is the denied writes: 17 Write attempts and 15 Bash
commands that write, all refused.

**Denied writes (#133–#134, Oct 20).** Same arm CLAUDE.md. Write
attempts fell from 17 to none: the case-clause runs wrote their
fixtures with create_file after one refused `mkdir` in Bash, and cost
0.43M input per run against 0.76M. Both symbol-addressing runs made
the schema change with one `replace_text` over `files`, after one
refused `sed -i` each. Turns refused fell from 17 to 14 of 120: fixture
4, `sed -i` 4, and reads the arm's Bash allowlist cannot match 6 (a
harness limit). Totals 5.54M → 5.21M, within noise; one
symbol-addressing run failed its hidden tests, as in earlier runs.

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

## 6. Third-party tasks (Oct 20)

The first ten tasks of `eval-tasks/third-party/ab.json` (feature PRs of
cli/cli, Hugo, lazygit, OpenTofu and Gitea; hidden tests are the PR's),
hybrid against manual, one run each, with the "refactor only when
needed" CLAUDE.md, Go 1.27.3.

| Run | hybrid | manual |
|---|---|---|
| tp-first | $1.96 · 3.64M · 8/10 | $1.96 · 3.71M · 9/10 |

No task needed setup beyond the prompt, and none timed out (runs of 1-5
minutes). The hybrid arm refactored nothing: its only gorefactor calls
were two `create_file`, so this run says nothing yet about the tools on
this code, only that the arms cost the same without them. Both arms made
most of their edits in Bash, with Python and `sed -i` scripts (about 25
of 78 Bash calls each), rather than with Edit (5 and 12 calls).

Failures: `tp-gitea-npm-version` failed in both arms because its prompt
was wrong: it said the package document has no top-level `repository`,
but the PR sets it from the latest version and leaves it out only when
that version has none (the integration test checks it). Fixed in
`tasks.json`. The hybrid arm's `tp-tofu-lint-unused` reported unused
locals but not unused variables, a miss of its own.

**The mcp arm (Oct 20).** The four tasks that refactored on the way,
with the refactor-first CLAUDE.md and every edit through gorefactor
(`tp-mcp-refactor-first`, 1 run each, the Gitea prompt fixed):
$0.98 · 2.03M · 4/4, against $0.75 (hybrid) and $0.70 (manual) on the
same four in `tp-first`, 3/4 each before the fix. It edited through
the tools throughout (replace_statements 9, add_declaration 6,
replace_text 7 with `files` once, create_file 4, batch 3) and made
one refactoring, a rename it needed for its edit; none extracted,
moved or changed a signature, as the real PRs did. Turns: 52, of
which 9 had a gorefactor refusal (code that did not type-check, a
range with a signature, content closing its block), 4 a refused Bash,
1 ToolSearch alone. The first `inspect` loads the module: 296 s on
Hugo, 27-37 s on the others; steps after it took at most 12 s. Two
refusals show gaps: a test using a type not declared yet (stubs are
declared for functions and methods only), and a call whose results
could not be inferred, so no stub.

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
