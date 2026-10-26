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
| ab-features-type-errors | #181–#190: type-error help (`claude-md-refactor-first-bash`) | $2.22 · 4.93M · 6/8 | | |
| ab-features-descriptions | #255–#259: every description shorter (8,700 words to 5,200), with examples | $2.09 · 4.51M · 7/8 | | |

The shorter descriptions, against `ab-features-type-errors` (same
CLAUDE.md): cost 6% lower, input 9% lower, 111 turns with tool calls
against 118; refused Bash writes 11 to 0, refused Bash reads 7 to 4,
refused gorefactor calls 5 to 6, ToolSearch-only turns 4 and 4. The
failed run is `feat-symbol-addressing` on an interface method
(`Shape.Area`), which failed in both runs of the baseline too. Within
the noise of 2 runs a task, but in the direction of the freight-xl
measurement: no tool lost, fewer wasted turns.

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

**After #181-#190 (Oct 20).** The same four with the type-error work
(stubs for types, fields and values, fix_imports, families,
add_result, change_field_type, Preload; `tp-mcp-type-errors`, 2 runs
each): $1.88 · 3.89M · 8/8, the same cost per run; type-check refusals
fell from 2.0 to 0.5 per run, though the new help itself rarely fired
(`docs/type-errors.md`). Preload did not shorten Hugo's first call
(328 and 331 s): the model calls `inspect` at once, and the load
itself is slow in a fresh copy.

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
Hugo, 27-37 s on the others; steps after it took at most 12 s. (Its quality row
for lazygit, 0 changed functions and Δ complexity -10699, is not a
measurement: a Go file of the copy did not parse, and the harness read
that as an empty module; fixed to say so.) Two
refusals show gaps: a test using a type not declared yet (stubs are
declared for functions and methods only), and a call whose results
could not be inferred, so no stub.

## 7. Tasks that call for the type-error help (Oct 20)

`eval-tasks/mechanisms.json`: six small features of a seeded module
(`testdata/notify`, a Notifier interface with two implementations),
each with hidden tests, each asking for a change whose natural path
needs one piece of the type-error help: a field a test sets first, an
error result and a parameter on an interface method, a field's type
changed to a named type, a helper passed as a value, the last use of
a package removed. The mcp arm, 3 runs each, with the current server
and with the one from e0682c2 (just before #181):

| Run | Server | mcp | turns | gorefactor refusals |
|---|---|---|---|---|
| mech-before | e0682c2 | $2.31 · 4.16M · 16/18 | 135 | 14 |
| mech-now | main after #199 | $2.25 · 4.26M · 17/18 | 138 | 11 |

The help fired where the tasks called for it (field stubs in 3 of 3
quiet runs, add_result on the interface family in 3 of 3, add_parameter
on the family, change_field_type in 3 of 3), and the type-check
refusals it targets went: send-error had 4 (a call of the old Send
used as a value, too many return values, an import left unused) and
now none; priority 1 and now none. But totals did not move: cost and
turns are the same within noise. A type-check refusal costs a turn,
and these runs had a few each; most turns read code. What is left:
`UserID("u1")` in a test written first got no stub in every userid
run of both arms (a type used only in conversions), and line ranges
that cut a loop (6, both arms). The three failures were the task's:
the model wrote its own TestSendErrors, which the hidden test's name
clashed with; the hidden tests are now named TestAccept....

## 8. Working as a derivation (Oct 21)

The user's five derivations (`docs/derivations.md`) as tasks with
hidden tests (`eval-tasks/derivations.json`: shared record, opt-out,
Contacts map, fit method, Batch parameter) and the six mechanism tasks
as a control, the mcp arm, 3 runs each, with the current instructions
and with `eval-tasks/claude-md-derivations`: the same, plus "work as a
derivation" (operations that keep behavior, then one small edit; use
the operations you have, and keep manual edits as small as they can
be) and the user's moves as recipes. One server for all, with fold and
parameterize. Every run passed.

| Run | Tasks | mcp | turns per run | derived share | steps writing nothing | production lines written per run |
|---|---|---|---|---|---|---|
| deriv-base | derivations | $1.77 · 2.88M | 10.0 | 35% | 22% | 10.6 |
| deriv-guided | derivations | $1.89 · 3.15M | 9.7 | 41% | 37% | 9.7 |
| mech-base2 | mechanisms | $2.13 · 3.96M | 11.7 | 12% | 6% | 13.0 |
| mech-guided | mechanisms | $2.22 · 3.82M | 11.3 | 9% | 1% | 14.2 |

On the derivation tasks the guidance changed how the model worked
where a recipe fit: for the Batch parameter it expanded, migrated and
contracted (fold, inline_function, rename), 14 of 19 steps writing
nothing (2 of 10 without); for fit and Contacts it reached for
inline_function and fold. It did not cut cost or turns, and on the
feature tasks it changed nothing. On a module this small the plain
model's manual edits are already small (a median of one or two
lines): what a derivation saves, the rewriting of every use by hand,
grows with the code, and these tasks have two or three uses of
anything. The test of the thesis needs the same derivations on code
with many uses.

**On code with many uses (Oct 21).** The same five problems on a
larger seeded module (`eval-tasks/testdata/notify-large`, 388 lines in
three packages: four near-duplicate senders, five SMS methods that
truncate, a report package reading `u.Email` and `u.Phone` about 30
times, ten campaign functions calling `Broadcast`;
`eval-tasks/derivations-large.json`), the same two arms, 3 runs each.
Every run passed.

| Run | mcp | turns per run | derived share | steps writing nothing | production lines written per run |
|---|---|---|---|---|---|
| large-base | $2.40 · 3.78M | 11.0 | 34% | 9% | 15.6 |
| large-guided | $2.07 · 3.37M | 9.7 | 50% | 31% | 9.5 |

Here the guidance paid: 14% less cost, 11% fewer input tokens, 39%
fewer lines written by hand. The gap is where a derivation replaces
rewriting each use: fit (6.3 lines written per run against 17.3,
$0.11 against $0.14, 7.3 turns against 11: fold the five call sites
onto `fit`, inline `truncate`) and Batch (15.7 against 32.0, 12 of 17
steps writing nothing: expand, fold the ten callers, inline, rename).
The control, opt-out, did not move (7.3 against 7.7), nor did the
shared record, where neither arm used fold (the derivation needs a
pointer alias, gap A). Contacts was cheaper ($0.18 against $0.23) but
still mostly rewritten by text: the literals (gap K) and a getter for
each of two fields, which fold took only twice. Three runs per task
is within the noise for the totals; the written lines and the
operations used are the clearer signal, and they point the same way.

**Six runs per task** (`large6-base`, `large6-guided`), with a server
that had `extract_variable`'s `pointer` (#216) and `fields_to_map`
(#217, since closed). Every run passed. Contacts is left out: both arms
did it in one `fields_to_map` call ($0.08, 5 turns), which measures
that tool, not the guidance. The earlier runs are recounted the same
way (production lines in what each step wrote, test files left out),
which counts more than the table above did:

| Run | runs | cost | input | turns per run | steps writing nothing | production lines written per run |
|---|---|---|---|---|---|---|
| large-base | 12 | $1.68 | 2.77M | 10.8 | 17% | 22.9 |
| large-guided | 12 | $1.54 | 2.41M | 8.9 | 40% | 15.1 |
| large6-base | 24 | $3.18 | 5.30M | 10.2 | 11% | 15.7 |
| large6-guided | 24 | $3.05 | 4.81M | 9.1 | 55% | 10.9 |

With twice the runs the direction holds and the cost gap shrinks: 4%
less cost (8% at three runs), 9% fewer input tokens, 11% fewer turns,
31% fewer lines written by hand, and over half the guided arm's steps
write nothing. By task: fit is the steady win (3.3 lines against 13.5,
$0.12 against $0.14, 8.0 turns against 11.8; fold and inline_function
in every guided run); Batch is derived (56 of 70 steps write nothing:
rename, fold, inline_function) but costs the same, and its lines
written are close (21.7 against 24.2), since declaring Batch and the
delegate is most of them; opt-out 9.3 against 13.5; the shared record
9.3 against 11.5. The same arm varies a lot between the two runs
(base Batch 48.7 then 24.2 lines), so per-task numbers at this size
are rough. No run used `pointer`: both arms wrote the shared helper
with replace_statements and never tried to extract `e.Sent`, so the
refusal that names the option never came up.

**Contacts after K and L** (`contacts-kl-base`, `contacts-kl-guided`,
six runs each, server at main after #222): 6/6 each, $0.185 a run in
both arms, 14.3 turns against 10.7. The guided arm took the recipe in
three runs: getters and `WithEmail`/`WithPhone` setters, folds of all
13 reads of Email and 10 of Phone, then the setter fold, which folded
one literal and left the rest: they were `[]notify.User{{...}}` in
test files of other packages, and an elided type there was refused
(not qualified). One run wrote the types out with replace_text and
folded them, changing the representation in the four functions only;
two rewrote the literals by hand, so the guided arm wrote more lines
(35.0 a run against 25.5) and 17% of its steps wrote nothing (5%).
No run used remove_dead_code on the fields: with no uses left, they
replaced the `User` declaration whole. The base arm never folded.
Fixed next: the setter fold qualifies an elided type in another
package.

**Contacts after the qualified setter fold** (`contacts-q-base`,
`contacts-q-guided`, six runs each, main after #223): 6/6 each;
$0.208 a run against $0.175, 13.7 turns against 10.7. Three guided
runs went the whole derivation through the tools: getters and
setters, folds of the 23 reads and of all 7 literals (4 for Email, 3
for Phone), then the representation changed in the four functions
and the struct ($0.140, $0.147, $0.200). Two other guided runs and
three base runs did it in one textual batch: replace_text of
`u.Email` with `u.Contacts["email"]` over several files, then the
literals ($0.14-0.16); the remaining base runs rewrote each use with
replace_expression ($0.22-0.31). The two guided runs say why: they
loaded `change_field_type`, found it could not turn two fields into a
map, and never loaded fold. The last guided run took the recipe with a
function getter, `GetEmail(u User)`, and fold failed with overlapping
edits: a body that is only a field of a struct parameter made that
field a hole of its own, which matched every string. It fell back to
replace_expression per file. The derived run at $0.200 hit the same
error and switched to methods. Fixed since: such a body keeps the
parameter one hole, and a body that is only a parameter is refused. Lines written by hand mislead here:
the derived runs restate whole declarations with replace_declaration
(58-66 lines for a change of a few), and a textual replacement over
every file counts as one line.

## 9. Lessons taught by models (Oct 21)

`docs/learning-transfer.md`. Explorers on notify-large, told to try
five sequences and to write at most one distinct line by hand per
change, wrote lessons; distilled, they went to fresh models on
billing-large, the same five shapes under other names. 6 runs a task:

| arm | passed | cost a run | turns | written by hand | derived share |
|---|---|---|---|---|---|
| baseline | 30/30 | $0.146 | 11.0 | 711 | 40% |
| learned | 30/30 | $0.142 | 10.1 | 386 | 60% |
| user's moves | 30/30 | $0.127 | 8.4 | 529 | 48% |

The learned lessons changed how the model worked (fold and inline where
the baseline rewrote; 46% fewer lines by hand) but not what it cost;
the user's shorter moves were cheapest. A short set combining the two
(`claude-md-combined`, about 310 words): $0.132, 8.7 turns, 493 lines by
hand, 51% derived, like the user's moves within the noise.

After `fold` learned near-duplicates (#245-#251; `eval-runs/learn2-*`,
$17.09, 120/120): on `book` the user's moves and the combined set
folded all four copies in 6/6 runs, the baseline 4/6, learned 3/6
(its long recipe now costs turns: 18.8 against 10.2). Lines written by
hand on `book` under the user's moves halved (74 to 37), cost did not
change (the task was already two or three calls). Totals per arm
moved within the noise: $0.152, $0.154, $0.134, $0.128 a run.

On a larger module of the same shapes (`freight-xl`, 959 lines, 5-10
times the uses; 4 tasks, 4 runs, $5.73): the user's moves $0.148 a run
against $0.198 for the baseline (25% less), 9.5 turns against 12.8,
251 lines by hand against 439. The parameter object went through
wrapper, `fold`, `inline_function` and `rename` in one batch in every
run of the moves arm, the baseline through a `replace_text` per call.

A tighter `fold` description (668 words to about 260, branch
`fold-description`; the user's moves on freight-xl again, 4 tasks, 4
runs, $2.67, `eval-runs/freight-human-desc`): `fold` was used in 0 of
4 near-duplicates runs (2 of 4 before), although every run loaded its
schema; tail and run as before (3/4, 4/4). Cost a run $0.168 against
$0.148, mostly the Contacts-shaped routes task, which used no `fold`
in either. The shorter text dropped the worked example of the
near-duplicates shape (`w.Sent = append(w.Sent, s)` into
`record(&w.Sent, s)`); the model reads the examples, not the rules, to
decide whether a tool fits. With one worked near-duplicates example
added back (about 300 words; `eval-runs/freight-human-desc2`, the
near-duplicates task only, 4 runs, $0.57): `fold` in 4 of 4 runs, $0.14
a run, 10.2 turns, 53 lines by hand (61 with the long description, 88
with the tighter one without the example).

| description | words | near-duplicates runs with `fold` | cost | turns | by hand |
|---|---|---|---|---|---|
| long | 668 | 2/4 | $0.15 | 11.2 | 61 |
| tighter | 260 | 0/4 | $0.15 | 10.0 | 88 |
| tighter, one example per kind | 300 | 4/4 | $0.14 | 10.2 | 53 |

Reading in the freight-xl runs (`docs/reading-and-deriving.md`, step
1; no new runs): both arms made two to four read calls a run and read
almost none of the bodies they changed. Reading was about a quarter
of input in both (14% for the moves arm on tail, 27% for the baseline),
mostly the output of "where is X used" searches resent on later turns:
on `run` 20% of the moves arm's input, for a `fold` that found all 30
calls itself.

The call-tree probe (`docs/reading-and-deriving.md`, step 2; 48
one-turn answers, $2.73): from the call tree, the skeleton or the whole
module, the answers named the functions to change alike (precision
1.0, recall 0.97, 0.96, 0.99) and planned the reference derivations;
their batches passed the hidden tests in 7, 6 and 10 of 16, failing on
the tools' interface (`rename`'s `function`, argument names the probe
did not give), test files no view showed, and a `fold` bug.

Set C, shapes no move names (`lending-xl`, `docs/reading-and-deriving.md`
step 3; 5 tasks, 4 runs, $10.68, all passed): the baseline $0.176 a
run, 10.7 turns, 1,285 lines by hand, 31% derived; the user's moves
$0.186, 13.1, 1,215, 33%; the algebraic arm $0.173, 12.6, 1,141, 37%.
The difference is policy (77 lines by hand against 182), a shape the
signature tools fit; the shapes no operation fits were written by hand
in every arm, and reading was alike.

All 38 descriptions shortened, each with an example (#255-#259, 8,700
words to about 5,200); the user's moves on freight-xl, 4 tasks, 4 runs,
$2.34 (`eval-runs/freight-human-alldesc`, against `freight-human`):

| | old descriptions | new |
|---|---|---|
| cost (16 runs) | $2.42 | $2.34 |
| turns with tool calls | 99 | 98 |
| ToolSearch-only turns | 9 | 6 |
| gorefactor calls refused | 11 | 5 |
| Bash writes refused | 1 | 7 |
| `fold` used: dispatch, routes, tail, run | 2, 2, 3, 4 of 4 | 4, 2, 2, 4 of 4 |
| lines by hand | 251 | 230 |

About the same cost, half the refusals, no tool used less but `fold`
on tail (one run, within noise). The Bash writes were `sed -i` and
`perl -pi` regex replacements across files on the routes task, four in
one run; replace_text takes exact text only, so a model that wants a
pattern across files still reaches for sed.

## 10. A kind into types, with and without the recipe (Oct 22)

`lending-kinds`, 4 runs an arm, $2.00 in all, after narrowing (#283),
case deletion (#284) and the header parentheses (#286):
`eval-runs/lending-kinds-recipe` (the algebraic arm and the recipe,
`eval-tasks/claude-md-kind-recipe`) and `lending-kinds-algebraic2`
(the algebraic arm alone), against the algebraic arm's kinds runs of
section 9's set C (`lending-algebraic`, before these tools).

| arm | passed | cost a run | turns | written by hand (4 runs) | derived share | gorefactor refused |
|---|---|---|---|---|---|---|
| algebraic, set C | 4/4 | $0.20 | 9.8 | 375 | 19% | 4 |
| algebraic, today's tools | 4/4 | $0.25 | 19.0 | 250 | 39% | 8 |
| recipe | 4/4 | $0.25 | 16.8 | 281 | 48% | 10 |

The tools were found without the recipe: 3 of 4 control runs widened
the field, ran `prepare_switch_for_types` on the five switches, wrote
the types, and narrowed (the fourth rewrote by hand). Every recipe
run took that path too, and none took step 3: they wrote the fifteen
one-line methods with `add_declaration` instead of extracting each
case, which is cheaper when the bodies are one line. Narrowing
deleted the dead cases in all 7 runs that used it. Against set C,
about a third less written by hand and twice the derived share, at a
quarter more cost and nearly twice the turns: the derivation's steps
cost turns that one rewrite did not, on a module this small.

The waste, per arm (refused turns): a comparison replaced whole
(`l.Item.Kind == lending.Atlas{}` in an `if`) did not parse, 6
refusals in 8 runs, since #286 parenthesizes only a new expression
that is itself a composite literal, and `replace_text` none;
`Label(it Item)` to `Label(title string)` by `replace_text` of the
interface, refused until the implementation and call changed with
it (2 runs an arm), where `add_parameter` on the interface method
does all three; `replace_text` old found twice (4 runs); empty
switches deleted with Bash `perl`/`python` (refused or not counted)
or `replace_statements`.

Rerun after #288 (composite literals bracketed inside an expression
in a header, by replace_text too) and #289 (the refusal of a
signature changed as text gives the steps), 4 runs an arm, $2.00
(`lending-kinds-recipe2`, `lending-kinds-algebraic3`):

| | recipe | rerun | control | rerun |
|---|---|---|---|---|
| cost a run | $0.25 | $0.27 | $0.25 | $0.23 |
| turns with tool calls | 49 | 49 | 51 | 46 |
| gorefactor refused | 10 | 6 | 8 | 7 |
| composite literal in a header | 3 | 0 | 3 | 0 |
| `Label`'s signature by text | 2 | 0 | 2 | 4 |
| derived share | 48% | 47% | 39% | 22% |

The parse refusals are gone. The signature note did not change what
the control runs did: two still edited the interface and then the
call as text, each refused, before sending both with check "end";
one had the new note with the steps, the other changed `Label` and
`Shelf` in one edit and got the general note. One recipe run used
`add_parameter` and `remove_parameter` on `Kind.Label` (its step 2);
the others edited both places in one batch, unrefused. The control's
derived share fell with one run that wrote the types in a new file and
replaced the switches whole, without the field tools. Cost moved
within noise. What is left is mostly `replace_text` old found twice
(`Kind: "dvd"` on two test lines, 5 refusals in 8 runs) and
malformed batch JSON (5), neither the tools' doing.

## 11. Naming the level (Oct 23)

Whether an instruction that names the level of the design (a concept
and every place that knows it) makes a fresh model refactor at that
level before a feature. Hybrid arm, three CLAUDE.md variants on
`claude-md-refactor-when-needed`, four tasks (`level-question.json`),
2 runs each; `docs/naming-the-level.md` has the question, the
references written before the run, and the transcripts graded.

| Run | Instruction | cost · input · passed |
|---|---|---|
| level-control | refactor when needed | $1.19 · 2.12M · 8/8 |
| level-design | + "what would make it hard?" | $1.01 · 1.63M · 7/8 (1 harness failure) |
| level-level | + name the concept and where it is known | $1.21 · 1.99M · 8/8 |
| level-axis | + name the axis: the new instance one new place | $1.17 · 1.91M · 8/8 |
| level-step | the same words as a session of its own before the feature | $1.89 · 2.85M · 16/16 sessions |
| level-step-5 | level-step, 5 runs | $5.04 · 7.87M · 40/40 sessions |
| level-control-5 | level-control, 5 runs | $2.82 · 4.42M · 20/20 |
| level-step2-5 | the step on lending-magazine and freight-air | $2.49 · 3.48M · 20/20 sessions |
| level-control2-5 | the control on them | $1.05 · 1.23M · 10/10 |
| level-payback-8 | a second change (student) after the step, against the chain without it (8 runs; one step run lost to the harness) | $4.08 · 6.87M · 37/40 (the 3 lost sessions of step run 1) |
| level-payback2-8 | the same on lending (map after magazine) and freight (Sea after Air) | $8.83 · 11.93M · 80/80 |
| level-long-5 | five changes per chain on pricing, lending and freight, with and without the step (5 runs; two pricing-control runs lost to the harness) | $16.23 · 24.07M · 154/165 |

Only the level arm refactored before adding a customer kind, but it
gave each pricing rule a home, not each kind; where the right home was
a function (the exports' shared filter) every arm found it; no arm
refactored on the control; on lazygit the question was skipped.
Naming the axis in CLAUDE.md did less (no refactoring on the kind:
a new `else if` passed as "one new case"). The same words as a
session of their own, before a new session makes the feature, made
both runs build a table of rules per kind, the reference's design;
the feature session then added one entry. At 5 runs and on two new
shapes (a lending kind, a freight route) it held: the preparing
session made the reference design in 15 of 15 runs where it was in
the way, the control in 0 of 15, with no false refactoring. The
feature session costs what the control does; the preparing session
is on top (1.5-2.6 times a task). On a second change (student, after
partner) the step's session added one table entry in 7 of 7 runs and
cost 11% less than the control's (22% fewer input tokens), whose runs
added a branch and shared staff's tax condition; the preparing
session ($0.148) is not paid back by that ($0.012) on this small
module. On lending (map) and freight (Sea) the second change was one
row or one accessor after the step and a case per switch or eight
copies without it, at the same cost or 11% more (more input read
before adding to a table): no shape paid back over two changes.
Over five changes (`level-long-5`) neither: after the step changes
1-5 cost what the control's did; lending's table held for every
later change, pricing's was bypassed at a new rule in 4 of 5 runs,
and freight's control extracted the shared helper itself once a
change cut across the copies.

## 12. Where a session's tokens and cost go (Oct 23, no new runs)

The user's question after reads came to under 4% of input: where do
the rest go? `eval-tasks/learning/cost.py` on `level-long-5` (160
sessions, 5.6 requests and 150K input tokens a session):

| input tokens | share |
|---|---|
| the first request's context (system prompt, tool definitions, CLAUDE.md, the task), about 22K tokens, sent again on every request | 82% |
| reads of code | 3% |
| searches | 1% |
| the model's own calls and text | 1% |
| edits and other command output | 1% |
| not in the transcripts' text: mostly tool schemas loaded by ToolSearch (about 2.8K tokens each), and chars/4 undercounting code | 12% |

Tokens are not cost. A least-squares fit to the reported costs gives
$0.10 per million cache-read tokens, $4 per million written to the
cache and $10 per million of output, and reproduces the run's $16.23
exactly. Cache writes are 69% of cost (17.4K tokens a session), output
18% (1.9K), cache reads 13% (133K). Of the writes, the first request's
are 38% of cost: it reads about 12.8K tokens from the cache and writes
about 9.4K fresh, and so does a chain's later stage, in the same
directory a minute after the one before, so the directory is not why
(it accounts for about 1.7K). What follows the cached part, the
session's first message with Claude Code's context and the task at its
end, is written anew each session. The later requests' writes, the
conversation's growth, are the other 30%.

So about $0.04 of a $0.10 session is the price of starting one, the
same in every arm: a difference between arms is a third larger against
what the arms control than against the total. An interactive session
pays it once over many requests, so the harness weighs it far more
than real use does. Reading less can cut at most part of the 30%;
fewer requests save mostly cache reads, the cheapest part.

## 13. What the tools add to a session (Oct 24, no new runs)

The user asked whether the context the tools add to every request is
where the arms' gap is. From the transcripts of the last runs with a
manual arm (Oct 18-20) and the newest, means per session, prices as in
section 12 ($0.10 per million read from the cache, $4 written, $10
output):

| run | arm | first request | of it written | requests | ToolSearch | cost |
|---|---|---|---|---|---|---|
| ab-chain-fixes | manual | 17.7K | 7.8K | 5.6 | 0 | $0.089 |
| ab-chain-fixes | hybrid | 19.5K | 9.6K | 6.7 | 1.0 | $0.118 |
| ab-features-refactor-first | manual | 17.8K | 8.8K | 10.0 | 0 | $0.177 |
| ab-features-refactor-first | mcp | 18.5K | 10.0K | 20.6 | 1.9 | $0.313 |
| level-long-5 | hybrid | 22.2K | 9.7K | 5.6 | 0.8 | $0.101 |

Today's server (44 tools) puts about 1.8K tokens in every request: its
instructions (5,831 characters, about 1,460 tokens) and the deferred
tools' names (about 370). That is the first request's gap between the
arms. A schema loaded by ToolSearch adds 500-800 tokens (move_declaration
803, inspect 699, fold 679, batch 637; all 44 together about 20K).

Where the gap goes:
- hybrid against manual (chains, +$0.029 a session): the always-on 1.8K
  is about $0.009 of it (written once, then read on each request), the
  rest the schema loaded and the extra request with what it adds. On
  level-long-5 the always-on part is about 8% of a session.
- mcp against manual (features, +$0.136): twice the requests (20.6
  against 10.0), each larger (41.9K against 33.9K); the always-on part
  is about 4% of the gap.

So the context the tools add to every request is not the gap. Halving
the instructions would save about 4% of a hybrid session; where the
mcp arm loses is the number of turns a change takes through the tools.
What is left to measure is whether the tools change what a session
costs in another way: a cheaper model doing with them what a larger
one does by hand (section 14).

## 14. A cheaper model with the tools (Oct 24)

The question: does Haiku with gorefactor do what Sonnet does by hand,
at a fraction of the price? freight-xl (dispatch, routes, tail, run),
the user's-moves instructions (`claude-md-derivations`), mcp and manual
arms, 4 runs each, one run per model (`eval-runs/model-haiku`,
`eval-runs/model-sonnet`; $4.63 in all, $0.48 of it Haiku, as the CLI
reports cost):

| model | arm | passed | cost a run | requests | first request | written to the cache | ToolSearch | gorefactor refused (16 runs) |
|---|---|---|---|---|---|---|---|---|
| Haiku | manual | 16/16 | $0.0124 | 9.6 | 20.9K | 29.4K | 0 | - |
| Haiku | mcp | 16/16 | $0.0175 | 9.9 | 23.3K | 40.9K | 1.1 | 15 |
| Sonnet | manual | 16/16 | $0.0973 | 5.3 | 20.1K | 15.8K | 0 | - |
| Sonnet | mcp | 16/16 | $0.1621 | 7.3 | 22.5K | 27.0K | 1.6 | 4 |

By task, cost a run, mcp against manual: Sonnet dispatch $0.13 against
$0.12, routes $0.27 against $0.10, tail $0.12 against $0.09, run $0.13
against $0.08; Haiku about $0.02 against $0.01 on every task. The code
left behind scored alike in every cell.

What it shows:
- Every run passed. Haiku did these tasks by hand, so the tools did not
  enable the cheaper model. Haiku by hand cost an eighth of Sonnet by
  hand: the model is the largest cost lever measured, and it does not
  need the tools here.
- With the tools, both models cost more than by hand: Sonnet 67% more,
  Haiku 41% more. Sonnet took two more requests a session, and every
  mcp session writes more to the cache (the instructions, the larger
  CLAUDE.md, loaded schemas, larger results).
- The 25% saving on freight-xl in section 9 was between two arms that
  both used the tools (the user's moves against the baseline
  instructions), never against editing by hand. Against by hand there
  is no saving on these tasks.
- Routes is most of Sonnet's gap: no operation does what it needs (the
  Contacts shape, fields into a map; section 8), so the runs made 157
  edits through the tools, 21 of them replace_expression, and no
  refactoring. Where an operation fits (tail, run), Sonnet derived 8-9
  steps and still cost a third more than by hand.
- The derived share fell with the smaller model: Haiku made 8
  refactorings in 16 runs against Sonnet's 17, and 15 refused calls
  against 4.

So on code of this size, with changes a model gets right by hand, the
tools do not pay for themselves in cost, and do not stand in for a
larger model. Where they could still show worth: changes that a model
gets wrong by hand (a failure rate, not a cost), which these tasks do
not have, since every arm passed.

## 15. Changes a model might get wrong by hand (Oct 25)

Section 14 left one place for the tools to show worth: changes a model
gets wrong by hand, where a refactoring's checks keep behavior and a
hand edit silently changes it. `eval-tasks/traps-warehouse.json` has
four, on a new module (`testdata/warehouse`, four small packages). Each
compiles and passes the module's own tests when done the obvious way,
and fails hidden tests:

- **reorder**: `Ledger.Transfer(from, to, qty)` to `(to, from, qty)`;
  one call is `l.Transfer(p.Bin(), p.Bin(), p.Qty())`, where swapping
  the arguments leaves the text the same and reverses every pick-list
  move. move_parameter refuses it until the arguments are variables.
- **inline**: inline `both(a, b) = a && b`, `either` and `within(v,
  lo, hi)`; the calls pass `y.charge(truck)` second (written `a && b`,
  a truck is no longer charged when no door is free) and
  `y.nextSlot()` as `v` (written out, it takes two slots).
  inline_function declares the arguments first.
- **rename**: package variable `margin` to `markup`, where `Bulk` has a
  parameter `markup` and reads `margin`; a textual rename compiles and
  adds the fee in place of the margin. rename refuses until the
  parameter is renamed.
- **dedupe**: ten sensors' `Probe` methods into one function, where
  `Gas` alerts above its limit (`>`) and the other nine at it (`>=`).
  fold folds the nine and leaves Gas.

Each was checked before the run: the seeded module fails the hidden
tests, the tool path passes them, and the obvious hand edit fails them.
The run: `-arms mcp,manual -runs 4 -claude-md
eval-tasks/claude-md-derivations` for Haiku and Sonnet
(`eval-runs/traps-haiku`, `traps-sonnet`), $3.66 in all.

| model, arm | passed | cost/run | turns |
|---|---|---|---|
| Haiku manual | 16/16 | $0.0087 | 13.9 |
| Haiku mcp | 16/16 | $0.0109 | 12.6 |
| Sonnet manual | 16/16 | $0.0954 | 10.6 |
| Sonnet mcp | 16/16 | $0.1137 | 9.5 |

No hand run fell into a trap. The hand runs saw them: Haiku, told by
the arm's CLAUDE.md to work test first, wrote a test in all 16 hand
runs (Sonnet in 6), most of them pinning the hazard before the change
(`TestAdmitChargesEvenWithoutDoor`); Haiku's first reorder message
named it ("There's one trap: Apply calls l.Transfer(p.Bin(), p.Bin(),
p.Qty())") and extracted the arguments into variables before swapping,
the same derivation the tool's refusal asks for. Every hand inline
declared the arguments first, as inline_function does. With the tools
the refusals did their job (move_parameter in 7 of 8 reorder runs,
rename in 3 of 4 Sonnet runs) and cost about 20% more than by hand, as
in section 14.

Two of Haiku's reorder runs were refused by move_parameter after
extracting only one argument (`moving p.Bin() past from`): a local
that nothing else can reach cannot observe the call, so that refusal is
false (README, roadmap item 30).

So on a module this small, with "must not change" in the prompt, the
traps are the only thing of interest in the code, and both models find
them by reading. Whether a model misses one where it is one call among
many, in a larger module, or where the prompt does not say behavior
must be kept, is untested; this run gives no evidence that the tools
lower the failure rate.

## 16. Can Haiku plan the derivation? (Oct 25)

The user asked how well Haiku thinks about the general shape of the
code: given a change, can it say which steps reach it? The call-tree
probe (`eval-tasks/learning/probe.py`, section 9's setting, roadmap item
29's planning half) asks that in one turn with no tools. It has three
new options for this run:
- `-model`;
- `-schemas`: the tools' input schemas, as the server publishes them, in
  place of the CLI's descriptions;
- `-plan`: the derivation in words, one entry per step, before the
  batch.

The view is the call tree (names, signatures, doc comments; no bodies),
now headed by the module's file list: in a trial, Haiku guessed
`freight/truck.go` and the batch failed on the path, not the plan.
Tasks: freight-xl's four and lending-xl's five (set C, shapes no move
names). Four answers per task and model, the same prompt for Sonnet
(`eval-runs/plan-probe`, $2.86 in all).

| | batches passing the hidden tests | cost per answer |
|---|---|---|
| Haiku | 15/36 (freight 6/16, lending 9/20) | $0.0075 |
| Sonnet | 19/36 (freight 11/16, lending 8/20) | $0.072 |

Sonnet's freight score was 7/16 in section 9's probe without the
schemas and file list.

The plans, read against each task's reference derivation
(`eval-tasks/learning/refs`). A plan counts when it names a sound
derivation of the reference's shape, whether or not its batch passed:

| task | reference shape | Haiku | Sonnet |
|---|---|---|---|
| dispatch | write `dispatch`, fold the ten bodies | 0/4: replaced each body by hand | 4/4 |
| routes | accessors, fold the uses, change what they read, remove the fields | 1/4 | 0/4: rewrote the uses |
| run | wrapper, fold the callers, inline the old one | 3/4 (renamed the old one first) | 4/4 |
| tail | wrapper, fold, inline `last` | 4/4 (3 by function_to_method and remove_parameter, also sound) | 4/4 |
| address | extract_struct, rename the embedded field | 4/4 | 4/4 |
| kinds | widen the field to `any`, types with the cases' bodies, rewrite the writes, narrow | 4/4 | 2/4 narrowed; 2 kept `any` or rewrote the type |
| policy | thread a parameter up from the leaves | 4/4 | 4/4 |
| phases, dryrun | written by hand in the reference too | wrote them (phases); reused Import on a copy (dryrun) | the same |

So Haiku sees the shapes about as well as Sonnet: it named the
derivations for run, tail, address, policy and kinds, the last one
being the user's widen-then-narrow derivation (README item 24),
unprompted in every run, more consistently than Sonnet. Where it
differed, it went both ways. On dispatch it never used fold and
rewrote ten bodies it could not see. On routes it found
encapsulate-then-change-representation once, which Sonnet never did.

Why batches failed, for both models: content written for bodies the
call tree does not show (`<body of case "book">` placeholders, guessed
log formats; all of phases and kinds, Haiku's dispatch); add_parameter
refusing a parameter named `policy` that would hide the package
variable (5 of 8 policy plans relied on that shadowing, so the bodies
need no rewrite); tests that still use a removed field (routes). The
first is the view; the second a choice to make (README, roadmap item
29's repairs, or an option); the third is in step 1's plan already
(the test files' uses in the view).

**With the bodies** (same day, `-views whole`, `eval-runs/plan-probe-whole`,
$1.68): the four tasks whose batches failed on content, rerun with the
whole module in the view. That view has every file but the tests.

| task | Haiku, call tree → whole | Sonnet, call tree → whole |
|---|---|---|
| phases | 0/4 → 4/4 | 0/4 → 4/4 |
| dispatch | 0/4 → 4/4 | 4/4 → 2/2 (2 answers refused by the API's safeguards) |
| kinds | 0/4 → 0/3 (1 unparsed) | 0/4 → 0/4 |
| routes | 0/4 → 0/4 | 0/4 → 0/4 |

Kinds and routes now failed only in test files, which the view does
not show: test literals still wrote `Kind: "book"`, or set `Road` and
`Rail`. Adding those literal fixes to each answer's batch (5 to 8
replace_text steps, the same in every answer), they pass the hidden
tests: routes 8 of 8, kinds 6 of 7 (Haiku 3 of 3; one Sonnet answer
refused by change_field_type). So with the bodies and the tests in view,
the plans of 28 of 29 parsed answers on these tasks would have
succeeded; the plans were right, and what they lacked was what they
could not read. Seeing bodies also made the models write more by hand:
on dispatch both replaced the ten bodies with replace_statements, and
Sonnet stopped using fold, which it used in 4 of 4 from the call tree.

For roadmap item 29: the planning half works with Haiku at a tenth of
Sonnet's price, and what is missing is bodies where content must be
written, not the plan. A repair turn with the bodies it names would
answer most of the failures.

## 17. Plan, then execute: the loop (Oct 25)

Roadmap item 29, step 1: `eval-tasks/learning/loop.py` on freight-xl's four
tasks, 4 attempts each, up to two repairs, the module's own tests deciding
when the loop stops and the hidden tests judging the result. Two variants
for Haiku: the plan may first ask to read declarations (`read`), or source
comes only with a repair (`-no-read`); then Sonnet with the one that won.
Compared with the manual arm of section 14 (same tasks and model, Claude
Code editing by hand). $3.44 in all. The loop's costs here are upper
bounds: it summed a resumed session's running totals (section 18).

| | passed | cost per task | requests |
|---|---|---|---|
| Haiku by hand (section 14) | 16/16 | $0.0124 | 9.6 |
| Haiku loop, read | 12/16 | $0.0286 | 2.3 |
| Haiku loop, no read | 12/16 | $0.0126 | 1.2 |
| Sonnet by hand (section 14) | 16/16 | $0.0973 | 5.3 |
| Sonnet loop, no read | 14/16 | $0.174 | 1.2 |

- **Reading costs and rewrites.** With `read`, Haiku asked for 8-60
  declarations a task and rewrote them with replace_statements and
  replace_text; dispatch wrote 15 lines by hand an attempt. Without it,
  the same model folded (dispatch 5 lines, run by fold and inline) at
  less than half the cost, with the same passes. Section 16's finding,
  now in cost.
- **dispatch, tail and run pass in every attempt of every variant**, mostly
  in one request: the plan from the call tree is the change.
- **routes fails, for three reasons, none of them the plan's shape:**
  - A batch that replaced `Shipment` without `Road` and `Rail` was
    applied with those fields declared again as stubs: the batch's
    create_from_usage declares what the steps use and nothing declares,
    including what an earlier step of the same batch removed. The module's
    tests then passed on unchanged code. An engine bug, which a session
    through the MCP can hit too.
  - Two Sonnet answers planned in stages ("I'll rewrite the uses in a
    follow-up") and sent only add_field; it passed the module's tests, and
    the loop stopped. The loop takes passing tests for done.
  - replace_text on test files whose text the plan guessed.
- **The loop costs what its prompt costs.** One planning request writes
  about 31K tokens to the cache, most of it the tools' input schemas (75K
  characters); Claude Code's own context, with its tools turned off
  (`--tools ""`), is a few thousand. That is about Haiku's whole cost by
  hand, and at Sonnet's prices 1.8 times Sonnet's by hand, though the loop
  takes 1.2 requests against 5-10.

So the loop does in one or two requests what the mcp arm did in 7-20,
and at Haiku's price matches editing by hand. What stands between it and
a saving: a shorter tools reference (the schemas of the steps a plan uses
are a fraction of 44), asking the model whether it is done when the tests
pass instead of stopping, and the stub bug.

## 18. The loop with its fixes (Oct 25)

After section 17: a batch no longer stubs what an earlier step removed
(#332), the loop asks whether the change is done when the module's tests
pass (#333), and the tools reference is compact (#334: 38K characters
in place of 76K; the planning request writes about 20K tokens to the
cache, not 31K). `loop.py -no-read` on freight-xl's four tasks, 4
attempts each, $1.89 (`eval-runs/loop-haiku-fixed`, `loop-sonnet-fixed`).

| | passed | cost per task | requests |
|---|---|---|---|
| Haiku by hand (section 14) | 16/16 | $0.0124 | 9.6 |
| Haiku loop | 11/16 | $0.0111 | 3.0 |
| Sonnet by hand (section 14) | 16/16 | $0.0973 | 5.3 |
| Sonnet loop | 14/16 | $0.107 | 2.2 |

- **Cost is now at par with editing by hand:** Haiku's loop 10% cheaper,
  Sonnet's 10% dearer. The planning request is most of it (Sonnet: $0.090
  of an attempt's ~$0.10); the "done?" turn reads the cache and writes
  about 1K tokens.
- **dispatch and run pass in every attempt with both models**, by fold
  and by wrapper, fold, inline and rename. Sonnet passes tail 4/4.
- **What fails, both models: routes** (Haiku 1/4, Sonnet 2/4). The
  plan was right, but remove_dead_code's refusal names only the first
  use left ("Road is used at audit/audit_test.go:11:42"): each repair
  fixed that file's test literals and was refused at the next file, until
  the repairs ran out. A refusal listing every use would let one repair
  fix them all.
- **Haiku's tail** (2/4): it wrote `tail` by hand and asked
  inline_function to inline `last` everywhere, which is refused for a
  two-statement body inside expressions; it repeated that in its
  repairs. The derivation that works (wrapper, fold, inline) is the
  reference's; Haiku found it in sections 16 and 17 in some attempts.

**A correction to section 17:** a resumed `claude -p` session reports
its cost so far, not the request's, and loop.py summed those totals, so
an attempt of several requests was counted more than once. The loop now
takes the last total. Section 17's means are upper bounds; since the
first request is most of an attempt, the true values are near the lower
bounds (each attempt's total over its requests): Haiku with reads
$0.011-0.029 (not $0.0286), Haiku without $0.010-0.013, Sonnet without
$0.144-0.174.

## 19. The loop on routes again, and on lending-xl (Oct 25)

remove_dead_code's refusal now names every use left, sorted, with
their count (#336). `loop.py -no-read`, 4 attempts per task and model,
the engine built from that branch; $0.60 for routes, $3.15 for
lending-xl.

**Routes** (`eval-runs/loop-{haiku,sonnet}-routes`): Sonnet 3/4 (2/4 in
section 18), Haiku 1/4 (1/4). The new refusal fired in three attempts
("Road is used in 20 places: ..."); Sonnet's third attempt got "RailOld
is used in 4 places", fixed all four in one repair, and passed.
Sonnet's failure was tests failing after a batch, not a refusal.
Haiku's failures were its plans: two replaced the fields with a map
and rewrote each use from bodies it had not seen, and one asked
remove_dead_code to remove `Road` while 63 uses were left.

**Lending-xl** (set C, shapes no move names; `eval-runs/loop-{haiku,sonnet}-lending`):

| task | Haiku loop | Sonnet loop |
|---|---|---|
| phases | 0/4 | 3/4 |
| kinds | 0/4 | 0/4 |
| policy | 3/4 | 4/4 |
| address | 4/4 | 4/4 |
| dryrun | 4/4 | 4/4 |
| all | 11/20, $0.0121, 2.6 requests | 15/20, $0.146, 2.9 requests |

No arm by hand ran lending-xl with either model; the nearest is section
9's Sonnet with the tools in a session, 20/20 at $0.176 a run (10.7
turns): the loop is 17% cheaper and fails a quarter of the tasks.

- **Kinds fails every attempt, both models.** It is the kind-into-types
  derivation (74 steps through the tools by hand, README item 24). The
  plans named it (widen, prepare, types, narrow) but broke on two
  refusals: prepare_switch_for_types after the plan had declared the
  interface's method itself ("interface Kind already has a method
  LoanDays", 2 attempts), and change_field_type narrowing the field
  while uses still needed the old type ("these uses of Kind cannot
  follow its new type Kind", 6 attempts); the repairs then wrote the
  types by hand with replace_text that did not type-check.
- **Haiku's phases** (0/4): written by hand in the reference too; its
  batches applied and the tests failed, from code written for bodies the
  plan had not seen, as in section 16.
- **Address, dryrun and policy** pass in 23 of 24 attempts, in two
  requests (address, dryrun) or three (policy, through
  `replace_variable`).

**Kinds again, with both fixes** (#338, #339;
`eval-runs/loop-{haiku,sonnet}-kinds2`, $1.12): 0/4 for each model
still. Both new refusals fired: Haiku's plans declared `Label(title
string)` where the switch needs `Label(it Item)`, and
prepare_switch_for_types said so (3 attempts); the narrowing refusal
named the missing middle. What stopped the attempts then was that the
plan sees no bodies: asked whether it was done, Haiku answered that it
could not write the next batch without the case bodies and Import's
source, and sent none; Sonnet made the types call the old switches with
string literals and guessed replace_text targets in code it had not
seen. Moving each case into its type needs the cases, which `-no-read`
withholds from the plan and the "done?" turn.

**Kinds with reads** (the first answer may ask for functions' source, once,
without line numbers; not the "done?" turn, as first written here;
`eval-runs/loop-{haiku,sonnet}-kinds-read`, $0.87): 8/8. Haiku $0.020 a
task in 4.5 requests (8.5 reads), Sonnet $0.197 in 4.5 (11), against
Sonnet in a session with today's tools (section 10) at $0.25 and 19
turns. But none derived: no attempt used prepare_switch_for_types.
Haiku widened and narrowed with change_field_type (three attempts
after a narrowing refusal) and wrote the types and the writes with
replace_statements and replace_text; Sonnet wrote all of it with edits.
As in sections 16 and 17, with the bodies in view both models rewrite;
the tools still check each step and the tests decide.

**Kinds with numbered reads** (reads show `inspect`'s numbered source,
of types and variables too, and may come in place of any batch, the
"done?" turn and repairs included, at most three; repair excerpts are
numbered; `eval-runs/loop-{haiku,sonnet}-kinds-numbered`, $0.85): 7/8.
Haiku 4/4 at $0.021 in 5 requests (old reads: $0.020, 4.5), Sonnet 3/4
at $0.192 in 4.25 ($0.197, 4.5). Nothing else moved: again no
prepare_switch_for_types, extract_function or introduce_parameter; the
same tools (add_declaration, replace_declaration, replace_statements,
replace_text; Haiku also change_field_type), 102 and 66 lines written
per passing attempt (101 and 68 before). Every attempt read once, at
the start, the six switching functions and their callers; no read came
later. The Sonnet failure spent its first repair on an answer with two
JSON objects, read the same names twice, and ran out of repairs on
type errors. So the read's form was not what kept the models from the
derivation: with the cases' bodies in view, writing the types out is
what both models choose, at the same cost as before.

**Kinds with the case address, without reads** (extract_function by
`function` and `case`, roadmap item 32;
`eval-runs/loop-{haiku,sonnet}-kinds-case`, $0.92): 0/8. Haiku $0.014
a task in 4.75 requests, Sonnet $0.217 in 3.5. One attempt in eight
used the address: Sonnet's first extracted every case (29 calls), wrote
the types' methods by hand as one-line calls of the extracted
functions, a route that needs no body, and ran out of stages before
Import, the comparisons and narrowing. On the way two refusals cost it
repairs: `"case": "book"` for the string case `"book"`, and Renewable's
`"atlas"`, which has no case. The others did not take it:

- Haiku (3 of 4) declared Kind with `Label(title string)`, as the
  prompt gives it, and prepare_switch_for_types refused: the cases need
  `Label(it Item)`. No repair got past that; no Haiku attempt
  extracted.
- Sonnet (2 of 4) did not see that a step addressed by name moves code
  it cannot see: one sent the steps it could and asked for the five
  functions' source, one wrote each method as a call back into the
  switch (`func (Book) LoanDays() int { return LoanDays(Item{Kind:
  Book{}}) }`), which recurses once the guard dispatches.

So the address works where it is used, but a plan has to know that it
moves code unseen, and the task's own signature for Label is one
prepare_switch_for_types cannot reach from `it`.

**Again, with a bare word for a string case and a line in the plan
prompt** ("Code you cannot see here need not be written: steps that
address it by name carry it with them ..., so move code rather than ask
for it or call back into what you move";
`eval-runs/loop-{haiku,sonnet}-kinds-case2`, $1.06): 0/8 still, but
the plans changed. Every Sonnet attempt and one Haiku attempt planned
the derivation by case (39-45 extract_function steps, 40-69 steps a
batch); none asked for bodies or wrote a method that calls back into
the switch. Haiku $0.031 a task, Sonnet $0.233, 3 requests each: no
batch applied, each repair cleared one refusal and met the next. The
refusals, by what a fix would be:

- a case of several values (Renewable's `case "book", "dvd":`),
  extracted before prepare_switch_for_types split it (5 attempts);
- `atlas` in Renewable, which has no case: atlas runs the code after
  the switch (5);
- the bare word's retry hid the real refusal: `book` found the shared
  case, whose refusal was dropped for "no case book" (3); a bug of the
  last PR;
- function_to_method making a method on `Item` (the first parameter)
  where the receiver was not added before `it` (2), and refusing an
  interface's method name (1);
- Haiku's `Label(title string)` from the prompt (1).

The prompt line is what moved the plans; what fails now is batches of
40-69 steps rolled back whole by one refusal, with two repairs.

**Again, with the refusals fixed** (the bare word's own refusal, `split`,
the missing case saying what runs, function_to_method on an
interface's method name and with any receiver, prepare taking fields
as the declared parameters, #347-#352;
`eval-runs/loop-{haiku,sonnet}-kinds-case3`, $1.03): 0/8. Haiku $0.022
a task in 4 requests, Sonnet $0.235 in 3.25. The plans now name the
receiver (`receiver: "b"`) and get through prepare with the prompt's
`Label(title string)`; two attempts went through three stages. What
stopped 7 of 8 is Renewable's `atlas`, which has no case: the refusal
says the code after the switch runs for it, "line 168 on", a line of
the module as it was at the refused step. The repairs resend the whole
batch from the start and put `extract_function` of `start_line: 168`
first, where line 168 is other code ("contain no statements inside a
function body"). Two Sonnet attempts also extracted a case of several
values without `split`, after the refusal named it.

**Again, with a missing case extracted and refusals' lines as the batch
began** (#356, #358; `eval-runs/loop-{haiku,sonnet}-kinds-case4`,
$0.98): 0/8. Haiku $0.029 a task in 3.5 requests, Sonnet $0.216 in
3.25. The derivation of the cases now goes through: the refusals come
at steps 54-75 of batches of 55-81, at narrowing the field
("these uses of Kind cannot follow") and at tests that fail after it,
and three attempts (two Sonnet, one Haiku) used all three stages
without a refusal at the end, Sonnet's second applying every batch.
What is left is the edit, not the derivation: Import must turn the
kind's name into a type and still report an unknown one by name, and
no plan can see Import without a read; plans guessed its body (one
replaced lines 236-238 of a 230-line file) or left it, and narrowing
then refused the string Import writes.

**Reads only after a batch has applied** (#366 `-read-after`, with
#367's refusal for emptying a live case; `-stages 3`;
`eval-runs/loop-{haiku,sonnet}-kinds-after`, $1.03): 7/8. Haiku 3/4 at
$0.023 a task in 6.5 requests, Sonnet 4/4 at $0.236 in 6.25; against
reads at the start (8/8, $0.020 and $0.197) about the same. But no
attempt derived: every first batch was the preparation alone
(`change_field_type` to `any`, `prepare_switch_for_types` five times,
sometimes the types), 6-10 steps, and stopped there; then every
attempt read all five switches and Import, and wrote the methods out
with `add_declaration` and `replace_statements` (64-127 lines by hand,
against none for the cases in the no-read runs). No
`extract_function` or `function_to_method` in any attempt. The prompt
said to "leave for later what needs code you cannot see", and the
plans counted moving the cases as that. So when reads come does not
matter: a plan that knows it can read later defers the move and
reads; reads, whenever they open, make the model write the code out.

**Reads not announced until they open** (the first prompt says only
that the first batch must leave the tests passing, and that the next is
asked for once it applies; the "done?" turn is the first word of
reads; `eval-runs/loop-{haiku,sonnet}-kinds-unannounced`, $1.14): 8/8.
Haiku 4/4 at $0.028 a task in 6.75 requests, Sonnet 4/4 at $0.259 in
6. Two Sonnet attempts derived in their first batch: one the
reference's way (extract each case by `case`, add_parameter,
function_to_method: 15 of each), then read Import, LoanDays and two
functions of notices, and wrote 41 lines; the other its own way
(extract each case, declare a method that calls it, inline the
function into it: 15 of each), 55 lines. The other six prepared only,
then read the five switches and wrote them out (65-124 lines). So not
announcing reads let a derivation happen, where announcing them
stopped every one, but the plans that stop at preparing still read
the switches when they can. Haiku never derived with reads possible.

**The first batch moves existing code, later ones add what is new**
(the user thought "make it easy" confused the model; the first prompt
now says every line of the change is existing code in a new place or
new code, the first batch places the existing code by name and writes
none the module has; `eval-runs/loop-{haiku,sonnet}-kinds-move`,
$1.26): 6/8. Haiku 2/4 at $0.027 a task in 5.75 requests, Sonnet 4/4
at $0.288 in 7.25. Three first batches moved the cases by name (two
Sonnet the reference's way, 15 extractions, parameters and methods;
one Haiku its 15 extractions), against two before; five still only
prepared and then read and wrote the switches out. The derived Sonnet
attempts still wrote 63 lines: they prepared `Label` without first
declaring the task's `Label(title string)`, so the moved methods took
the item, and after reading them they wrote the three out again with
the task's signature, besides Import and the literals. Haiku's derived
attempt failed at `function_to_method` of a `Label` helper whose first
parameter is the title ("the type of title, string, is not a type
declared in package lending"). The wording moved a little toward
deriving at no gain in passes; a refusal at a function_to_method whose
first parameter is not the receiver, naming `receiver`, is the next
fix it shows.

**Again, after #367, #371 and #372** (the refusal for emptying a live
case, the receiver hint, introduce_parameter through an interface
method and by `function`). Two runs on main, Haiku and Sonnet, 4
each, `-stages 3` ($2.49 in all):

- With reads after the first batch
  (`eval-runs/loop-{haiku,sonnet}-kinds-fixes`): 7/8. Haiku 3/4 at
  $0.025 a task in 5.5 requests, Sonnet 4/4 at $0.278 in 6. Two
  Sonnet attempts derived (15 extractions in the first batch, then
  add_parameter, function_to_method and introduce_parameter by
  `function` on the three Label helpers, then remove_parameter) and
  wrote 41 and 57 lines; the other two Sonnet attempts and three
  Haiku ones prepared only and rewrote the switches (66-106 lines).
  Haiku's derived attempt moved every case and reached `Label(title
  string)` with the new introduce_parameter on `Book.Label`, then
  failed at narrowing (13 lines written).
- Without reads (`eval-runs/loop-{haiku,sonnet}-kinds-noread`): 2/8,
  the first passes of kinds without a read. Haiku 0/4 at $0.025 in 4
  requests, Sonnet 2/4 at $0.297 in 3.75. One Sonnet attempt
  declared `Kind` with `Label(title string)` first (so prepare
  supplied `title`), moved every case by `case`, add_parameter and
  function_to_method, and found Import's write from the narrowing
  refusal: `Kind: kindOf(strings.TrimSpace(f[2]))`, with an
  `unknownKind string` type implementing Kind so the error still
  names the kind (49 lines, none of them a case body). The other
  extracted by case, declared methods that call the helpers and
  inlined them (56 lines). The failures were refusals of detail
  (a case shared by two values without `split`, introduce_parameter
  on a helper that already read `title`, function_to_method on a
  helper with no parameter) more than repairs could clear.

So with the tools fixed, the derivation carries the task: without
any read Sonnet passes half its attempts by moving code, and with
reads half its attempts still derive first; Haiku derives as often
but does not finish. Reading still turns most plans into rewrites
(5 of 8 here).

The refusals of both runs, tallied from the attempts replayed for
their full text (31, and one answer without a batch): narrowing refused
while old constants were still written or compared (13, in 13 of 16
attempts; it cost one of two repairs each time, and four of the seven
failures ran out of repairs after it); the interface's method retyped
by hand while the guard still called `k.Label(it)` (5); `kindOf`
returning the types as `Kind` before they had the methods (3); a case
shared by two values without `split` (2); eight of one each. What
followed: narrowing takes `values` for the old constants and writes
them itself (#374); the note that names the steps for a signature
changed as text reaches a batch's refusal, where it had stayed in the
notes of a step the batch dropped, and names only the methods whose
signature changed, with introduce_parameter for a parameter that
stands for an expression of the old ones; `split` became the default.

**Without reads, after #374-#376**
(`eval-runs/loop-{haiku,sonnet}-kinds-noread2`, $1.08): 3/8, against
2/8 before. Haiku 1/4 at $0.026 a task in 3 requests, its first pass
of kinds without a read (73 lines); Sonnet 2/4 at $0.245 in 3.5 (53
and 37 lines). `values` did its work: every narrowing refusal after it
listed only Import's `strings.TrimSpace(f[2])`, where before it listed
ten literals. No case was refused as shared. What stopped the five
failures: narrowing with `values` before the types implemented `Kind`
(3: one Haiku plan narrowed right after preparing, two Sonnet plans had
written the methods with `Label(title string)` while prepare had
declared `Label(it Item)`), refused as "Atlas{} is not a Kind" without
saying why; `kindOf` returning the types before they had the methods
(3, as before); Import's error for an unknown kind and a feed test,
written without seeing Import (2); introduce_parameter on a helper
that already read `title`, since the plan's own `Kind` had made
prepare supply it (1).

**Again, with both refusals saying why** (#378, #381;
`eval-runs/loop-{haiku,sonnet}-kinds-noread3`, $1.24): 2/8, Haiku 0/4
at $0.033 in 3 requests, Sonnet 2/4 at $0.276 in 3.5 (53 and 46
lines); at four attempts a model, the same as 3/8. The hints were
followed: all four Sonnet attempts met the narrowing refusal naming
Import's write, added `kindOf` at the start of the next batch, met
the new missing-methods hint, and in the repair after moved `kindOf`
after the methods, so their batches applied. Then two failed on
Import itself: `kindOf` returns nil for an unknown kind and Import
calls `LoanDays(it)` to detect one, which a plan without Import's
body cannot know, so TestImport panicked. Haiku met the signature
refusal (`Label(it Item)` against `Label(title string)`, now saying
so) and still failed its repairs, and twice ran introduce_parameter
on a helper that already read `title`. What remains is the edit in
Import: the derivation's refusals are no longer what stops the plans.

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
