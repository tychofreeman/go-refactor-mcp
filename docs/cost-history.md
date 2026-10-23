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
module.

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
