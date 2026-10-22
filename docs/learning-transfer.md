# Can an exploring model teach later ones?

Roadmap item 21's exploration runs. The question (the user, 2026-10-21):
if a model is told to try several approaches when one fails, can what it
learns make later models more effective, even if it costs more itself?
A later model gets nothing from an earlier run unless something carries
it across, so the test is of what the explorer writes down: lessons,
distilled into recipes, given to fresh models on other code.

What earlier runs suggest: recipes tied to a situation changed how the
model worked where they fit (cost-history section 8, the user's moves in
`eval-tasks/claude-md-derivations`); general advice did not (#119, longer
descriptions). So lessons are kept only when they name a situation and
an operation, and when a transcript shows them working.

## Two task sets with the same shapes

- **A, the explorer's:** `eval-tasks/derivations-large.json`, the five
  derivation problems on `testdata/notify-large`.
- **B, the learners':** `eval-tasks/derivations-billing.json`, the same
  five shapes on `testdata/billing-large` (accounts charged through
  payment methods): merge four near-duplicate methods into `book`
  (shared-record), skip blocked methods in a loop (optout), two fields
  become a map (contacts), a helper used by one type becomes its method
  `tail` (fit), a parameter object `Run` (batch). Names, domain and
  details differ (`last` keeps the end of a string where `truncate` keeps
  the start; a bank charge appends the amount), so a recipe can carry the
  technique across but not the answer. Each hidden test fails on the
  module as given and passes with a reference solution (checked when the
  set was made).

## The runs

1. **Explore** on A, with `eval-tasks/claude-md-explorer`: the base
   (`claude-md-refactor-first-inspect`) and a section asking the model
   to try two other ways before writing code by hand after a refusal,
   and to end with a `## Lessons` section for sessions on other code.
   It is not given the user's moves, so what it teaches is its own.

       go run ./cmd/refactor-ab -tasks eval-tasks/derivations-large.json \
         -model sonnet -arms mcp -runs 6 \
         -claude-md eval-tasks/claude-md-explorer -out eval-runs/explore

2. **Distill:** `python3 eval-tasks/learning/lessons.py eval-runs/explore`
   prints each run's lessons with its result and cost. Keep a lesson when
   it comes from a passing run and its transcript shows the operation
   going through (`refactor-tokens` lists the derived steps); merge
   duplicates; drop notify's names; at most about a dozen bullets. They
   go into `eval-tasks/claude-md-learned/mcp.md`: the base and a
   `## Lessons from earlier sessions` section.

3. **Learn** on B, three arms of 6 runs per task:
   - `claude-md-refactor-first-inspect`, the baseline;
   - `claude-md-learned`, the baseline and the distilled lessons;
   - `claude-md-derivations`, the user's moves (taught by a person).

       go run ./cmd/refactor-ab -tasks eval-tasks/derivations-billing.json \
         -model sonnet -arms mcp -runs 6 \
         -claude-md eval-tasks/claude-md-learned -out eval-runs/learn-learned

Measure: pass rate, cost, turns, the derived share and the size of the
manual edits (`docs/derivations.md`, the user's principle), per task and
in total. The explorer's extra cost is a one-time cost: the question is
whether the learned arm beats the baseline on B, and how it compares with
the user's moves.

Cost, from `large6-*` (about $3.60 for 30 runs): the explore run perhaps
twice that, the three learner arms about $11; about $18 in all.

## Results

**Explore on A** (`eval-runs/explore`, 30 runs, $5.55 against $3.65
for the baseline `large6-base`; 30/30). Over the 30 runs, refactoring
calls 56 (baseline 5, the user's moves 7), lines written by hand 486
(513, 384), derived share 59% (46%, 66%). Exploring made the model use
the operations it knew, but did not find the less obvious ones: no run
used `fold`, `pointer`, or `remove_dead_code` of a field (two wrote
that no tool removes one). Derivations several runs reached on their
own: a helper to a method (fit, 6/6: `add_parameter` of the receiver,
`function_to_method` with `new_name`, the constant into the body,
`remove_parameter`, which the user's moves do not have), extract the
inner loop and guard it (optout, 6/6), extract, `method_to_function`
and `introduce_parameter` (shared-record, 5/6), expand, migrate,
contract (batch, 4/6). Contacts went textually in all six. Two lessons
were false ("the tools don't run tests", "no remove-field tool"), and
many were tool mechanics.

The user then dropped the Contacts lesson and asked for focused trials.

**Shared record, told to try two operations before writing by hand,
even ones expected to be refused** (`claude-md-explorer-2`,
`eval-runs/explore-record`, 6 runs, $1.49, 6/6). All six called `fold`,
three `extract_variable` with `pointer`; four derived the shared
function from one method by operations. Three steps were written by
hand in every run, and they say what the engine lacks:

- the alias moved above the log call: `introduce_parameter` of
  `&e.Sent` and both move_statement tools refuse it. The refusal says
  `log.Printf` could change what `&e.Sent` reads, which is wrong; the
  real difference is that `&e.Sent` panics on a nil receiver, now
  before the log line. The target `record(&e.Sent, ...)` makes that
  change itself, so no derivation that keeps behavior strictly
  reaches it (a decision for the user, which gap O shares);
- `"email to %s"` to `"%s to %s", "email"` (gap B);
- the other three methods: `fold` said only "nothing in the module
  matches the body of record"; they differ in the format literal (B)
  and in `s.Sent` against `*sent` (O), and a refusal naming that
  nearest miss is what each run needed there (item 21, lessons in
  refusals).

**Contacts, told to find two or three ways to make the change without
writing Go by hand and carry out the best** (`claude-md-explorer-3`,
`eval-runs/explore-contacts`, 6 runs, $1.26, 6/6). It did not work:
every run named two or three ways (`change_field_type`, accessors, the
textual route) and carried out the textual one, judging the others
without a call ("more work for the same result"); one said it explored
less than asked. No run called `fold`, `undo` or `remove_dead_code`;
four wrote that no tool removes a field. What made the model explore
was an instruction to make calls (shared record), not one to consider
ways: it can satisfy the second by reasoning. And it did not count
`replace_expression` and `replace_text` as writing code by hand,
though the instructions name them.

**Contacts again, with the wording that worked and two sentences
more** (`claude-md-explorer-4`: an operation counts as tried only once
called; replace_statements, replace_declaration, replace_expression and
replace_text on Go code are writing by hand; `eval-runs/explore-contacts-calls`,
6 runs, $1.32, 6/6). Now the model made calls: five called
`change_field_type` first (refused: a string cannot become a map) and
then went textual; run 2 called `remove_dead_code` on the old fields;
run 6 took the user's derivation unprompted: getters `EmailAddr` and
`PhoneNumber` by `add_declaration`, `fold` on each (13 and 10 reads in
every package, two calls), `add_field`, the getters' bodies changed
with `replace_declaration`. It still wrote the keyed literals with
`replace_text` (it did not know a setter folds them) and deleted the
fields with `replace_text`. Found on the way: `remove_dead_code`
refused run 2's fields because `SortUsers` passes `[]User` to
`sort.SliceStable`, which only swaps elements by reflection and never
sees a field: a false refusal, where `replace_text` then deleted the
same fields with no check of reflection at all.

**All five tasks, told to try at least five sequences and to fail
rather than write more than one line by hand** (the user's wording,
`claude-md-explorer-5`: a sequence counts only once its operations are
called, each is undone before the next, the best is carried out again;
test files and comments do not count; `eval-runs/explore-five`, 3 runs
per task, $7.62). 12 of 15 reached the goal within one line: opt-out
3/3, fit 3/3, batch 3/3, shared record 2/3, Contacts 0/3. The runs
made 170 refactoring calls (97% derived) and up to 30 undos each. New
derivations, each found by two or more runs:

- *Shared record, one line:* per copy, extract only the append,
  `method_to_function`, `extract_variable` with `pointer`,
  `introduce_parameter` for `&x.Sent`, `to` and `text` (the pointer now
  comes first, so nothing refuses it), `remove_parameter` the rest;
  `add_parameter` `channel` with each copy's value; `merge_duplicate_function`
  the copies into one; `insert_statements` the log line once (the one
  line); delete the old log lines (`replace_statements` with no content).
  It goes around the nil-receiver refusal rather than through it.
- *Fit, one line:* insert `n = 20` as the body's first statement;
  `remove_parameter` then takes `n` (assigned first) and leaves
  `n := 20`; `add_parameter` of the receiver, `function_to_method`.
- *Batch, one line:* declare `Batch` by a test that uses it
  (`create_from_usage`); `extract_variable` the message at every call
  with one name, so one `add_parameter` value (`Batch{Users: users,
  Message: msg}`) fits every call; `extract_function` the body; the
  wrapper's body becomes one line; `remove_parameter` the old ones.
- *A getter with no code by hand:* `extract_variable` the field read,
  `extract_function` that line, `inline_variable` in the new function;
  `fold` it (Contacts, two runs).

Contacts' best was two or three lines: the two getter bodies must
change, so one line is out of reach with these tools. No run found the
setter fold or `remove_dead_code` of a field (deleting the field lines
with `replace_text` and no content counted as no code).

Limits the runs met, to check: `parameterize` refuses methods of
different receivers; `extract_struct` refuses an exported struct;
`remove_parameter` takes `n = c` but not `a, b = x, y` as assigned
first; `rename` of a field refused over `sort.SliceStable` (as
`remove_dead_code` was); `introduce_parameter` of the literal
`"email"` reportedly made code that did not compile; a `replace_text`
that deleted a field still in use was followed by a stub that declared
it again, reported as applied; `add_parameter`'s value cannot be both
qualified and unqualified, so a test in the declaring package blocks a
value of the new type.

**Contacts one field at a time** (the user, 2026-10-21: "I would do each field separately"). The one-line limit was on the whole task, and Contacts holds two independent changes, each a fact the code does not have (Email lives at `"email"`, Phone at `"sms"`), so it failed by the rule, not for want of tools. Run by hand with the CLI on a copy of notify-large, each field is one line: `extract_variable u.Email` with `all_occurrences`, `extract_function` that line into `EmailAddr`, `inline_variable` in it (a getter with no code by hand); `fold EmailAddr` (11 reads); one batch of the test literals (`Contacts: map[string]string{"email": ...}`, which declares the field) and the getter's body, `return u.Contacts["email"]`, the one line; delete the field. Phone the same (8 reads), `return u.Contacts["sms"]`. The hidden test passes. Two snags: `remove_dead_code User.Email` refused over `sort.SliceStable` (the field went by an empty `replace_text`), and `inline_variable` refused `email := EmailAddr(u)` as if a call of a getter could vary. `claude-md-explorer-6` counts the limit per change: "a sequence fails if it needs more than one line written by hand for any one of those changes". Run on Contacts (`eval-runs/explore-per-field`, 6 runs, $1.55, 6/6) it backfired: every run took each `replace_expression` as its own change, so `u.Contacts["email"]` written at six reads was "one line per change", and the textual route passed. None took the getter route, and exploring collapsed: three runs tried one sequence, with 0-3 undos a run against up to 30 under explorer 5. A limit per change must count every line written for that change, wherever it goes. Two more runs met the false `sort.SliceStable` refusal (`remove_dead_code`, `rename`), and one saw `replace_declaration` of the struct followed by stubs that declared the removed fields again.

The user accepted that reading (the same line at many call sites is one edit: what matters is how complex the manual edit is) but not a chain of different one-line edits. `claude-md-explorer-7` counts distinct lines, defines a change as one fact the task adds, not one edit call, and says to try all five sequences even when an early one passes (`eval-runs/explore-seven`, 6 runs, $3.09, the server before #233). Exploring came back: every run called `fold`, with 3-14 undos a run. Four of six stayed within one distinct line per change. Runs 1, 3 and 5 found the user's derivation themselves: the getter derived with no code (`extract_variable`, `extract_function`, `inline_variable`), renamed to an exported name and moved to the type's package with `move_declaration` (fold leaves other packages alone for an unexported helper, and says which uses it left), `fold`, the new field from a test literal, one getter body per field. Run 2 wrote its getters by hand and counted them as part of the one change. The textual route failed in every run that tried it: `u.Contacts["email"]`, `a.Contacts["email"]` and `b.Contacts["email"]` count as distinct lines. Runs 2 and 4 found the setter fold for literals and set it aside: a setter whose body must make the map and keep the other key is more than one line. Run 4 (getters and setters written by hand, about 8 lines) and run 6 (getters by hand, two lines a field) failed honestly. `remove_dead_code` still refused the fields over `sort.SliceStable`, fixed in #233 after this run started.

**Explorer 7 on all five tasks** (`eval-runs/explore-seven-all`, 3 runs a task, $8.12, after #233-#238 changed exported code), against explorer 5 (`explore-five`, $7.62): shared record 3/3 (2/3), opt-out 3/3 (3/3), Contacts 3/3 (0/3), fit 3/3 (3/3), batch 1/3 (3/3); 13 of 15 against 11. Batch's two failures followed the rule: five sequences, none within one line, all undone. Every route met one gap: `add_parameter` and `introduce_parameter` write one value at every call, and a value of the new type is `notify.Batch{...}` in `campaigns` but `Batch{...}` in `notify`'s test; an alias, a qualified value and moving the callers in and back (an import cycle) all failed. The passing run pointed the test at an extracted helper so that one package was left calling it. The refusals for exported code were gone, but the change had a bug: `extract_struct` of the exported `User` wrote the new type unqualified in other packages' literals, caught after the fact as a bug in its checks, in all three Contacts runs (fixed: qualified, and an unexported new type refused there). A `move_declaration` in batch run 3 also left code that did not compile.

**Batch again after the fixes** (#239-#241: values qualified per call, `extract_struct` qualified, the cycle check through tests; `eval-runs/explore-seven-batch`, explorer 7, 3 runs, $2.20): 3/3, each within one line per change, by three different routes:

- `add_parameter b Batch` with `Batch{Users: users, Message: m}` after `extract_variable` named the message `m` at each of the 11 calls; written `notify.Batch{...}` in `campaigns` and `Batch{...}` in `notify`'s test. Then `extract_function` the body, its wrapper's one line `return send(b.Users, c, b.Message)`, `remove_parameter` twice.
- `introduce_parameter` of a literal written once in the body: `extract_function` the body, its wrapper's one line using `Batch{Users: users, Message: m}`, `extract_variable` with `all_occurrences` to merge its two uses, `introduce_parameter` (which writes the literal at every call, translated and qualified), `inline_variable`, `remove_parameter` twice, `inline_function` the helper. No `extract_variable` per call.
- The user's expand, migrate, contract: a one-line wrapper `BroadcastBatch(c, b)` over the old function, `fold` it (every call in every package becomes `BroadcastBatch(c, notify.Batch{...})`), `inline_function` the old one with `all_calls`, `rename`. Two lines, the type and the wrapper, one per fact.

The type came from a test literal (`create_from_usage`) in two runs.

The learned lessons (`claude-md-learned`) were first the explore run's,
less Contacts, with lesson 3 from the shared-record trial, then the
five-sequence runs'. For the learner runs they are rebuilt from the
explorer 7 runs (`explore-seven`, `explore-seven-all`,
`explore-seven-batch`): six derivations, each found by two or more runs
within one distinct line per change, and three dead ends; about 740
words on the base, against about 350 for the user's moves. One step is
mine, not the runs': the Contacts lesson ends with `remove_dead_code` of
the old field, which the runs did with `replace_text` while
`remove_dead_code` refused falsely (fixed in #233).

## The learner runs on B

`derivations-billing.json`, 6 runs a task, three arms in parallel
(`eval-runs/learn-base`, `learn-learned`, `learn-human`), $12.47 in all:

| arm | passed | cost a run | turns | written by hand | derived share |
|---|---|---|---|---|---|
| baseline | 30/30 | $0.146 | 11.0 | 711 | 40% |
| learned (model-taught) | 30/30 | $0.142 | 10.1 | 386 | 60% |
| the user's moves | 30/30 | $0.127 | 8.4 | 529 | 48% |

Lines written and derived share are over the 30 runs of each arm.

What the lessons carried across, by task:

- `billing-tail` (the helper becomes a method): learned 6/6 by wrapper, `fold`, `inline_function`; the user's moves half by that, half by `function_to_method`; the baseline wrote it by hand but for one run.
- `billing-run` (parameter object): learned and the user's moves both 6/6 by wrapper, `fold`, `inline_function`; the baseline by `replace_expression` at each call.
- `billing-methods` (two fields to a map): learned folded getters and removed the fields with `remove_dead_code` in most runs; the user's moves folded getters too; the baseline went textual (123 replacements).
- `billing-blocked`: small either way; extract then guard in about half the runs of every arm.
- `billing-book` (near-duplicates into `book`): every arm wrote it by hand (`replace_statements` per method). The near-duplicates lesson, the longest and most fragile, did not transfer.

So a model can teach later models: the learned arm derived where the baseline rewrote, and wrote 46% fewer lines by hand than the baseline and 27% fewer than the user's moves, the highest derived share of the three. It did not make them cheaper: $0.142 against $0.146 is within the noise (10-25% at this many runs), while the user's shorter moves cost 13% less with a quarter fewer turns. The explorers' own cost ($5.55 + $7.62 + $8.12 + $2.20 and the trials) is a one-time cost these runs did not repay on a module this small. Not yet tried: a larger module, where rewriting by hand costs more.

**A short combined set** (`claude-md-combined`, about 310 words on the base: the user's framing and phrasing, one proven operation chain per move, nothing for near-duplicates; `eval-runs/learn-combined`, $3.97):

| arm | passed | cost a run | turns | written by hand | derived share |
|---|---|---|---|---|---|
| baseline | 30/30 | $0.146 | 11.0 | 711 | 40% |
| learned (about 740 words) | 30/30 | $0.142 | 10.1 | 386 | 60% |
| the user's moves (about 350) | 30/30 | $0.127 | 8.4 | 529 | 48% |
| combined (about 310) | 30/30 | $0.132 | 8.7 | 493 | 51% |

Shortening gave back the turns (8.7 against 10.1) and some of the derived share (51% against 60%): the combined set performs like the user's moves, within the noise of each other. On Contacts it folded getters and removed the fields with `remove_dead_code` in most runs, more than either; on tail most runs used `function_to_method` rather than the wrapper. Concrete chains added to the user's moves did not measurably improve on them; the learned section's detail bought derived code with turns. Near-duplicates (`book`) stayed by hand in every arm.

## After `fold` learned near-duplicates (#245-#251)

The four arms again on B, 6 runs a task, after `fold` could fold
billing's four `Charge` methods into a `book` the model declares (roadmap
item 23): `eval-runs/learn2-*`, $17.09 in all. All 120 runs passed.

| arm | cost a run | turns | written by hand | derived share |
|---|---|---|---|---|
| baseline | $0.146 → $0.152 | 11.0 → 11.1 | 711 → 627 | 40% → 38% |
| learned | $0.142 → $0.154 | 10.1 → 11.4 | 386 → 375 | 60% → 64% |
| the user's moves | $0.127 → $0.134 | 8.4 → 9.4 | 529 → 546 | 48% → 49% |
| combined | $0.132 → $0.128 | 8.7 → 9.0 | 493 → 446 | 51% → 53% |

The near-duplicates task alone (`book`), runs that folded all four
copies with `add_declaration` and one `fold`, and lines written by
hand over its 6 runs:

| arm | folded | written by hand | derived share | cost a run | turns |
|---|---|---|---|---|---|
| baseline | 4/6 (0 before) | 159 → 118 | 27% → 36% | $0.14 → $0.13 | 11.2 → 9.3 |
| learned | 3/6 (0) | 72 → 69 | 38% → 63% | $0.16 → $0.20 | 10.2 → 18.8 |
| the user's moves | 6/6 (0) | 74 → 37 | 37% → 62% | $0.12 → $0.12 | 7.8 → 8.7 |
| combined | 6/6 (0) | 85 → 60 | 32% → 52% | $0.13 → $0.12 | 8.7 → 8.7 |

- With a general operation for the move, every arm used it, the
  baseline too (from the tool's description): "delegate" became
  declare the function once, then `fold`. Under the user's moves
  and the combined set it was the route in every run, and halved the
  lines written by hand on the task.
- It did not change cost: writing `book` and the four bodies by hand
  was already two or three calls; `fold` replaces the bodies, not a
  turn. On a module this small the gain is in what is written, as
  before.
- The learned arm got worse on this task (18.8 turns): its lesson is
  the explorers' long chain (extract variables, extract, add
  parameters, `merge_duplicate_function`), which three runs followed
  with many refusals, while the other three found `fold`. A recipe
  becomes a cost when the tools improve and it does not. The lesson is
  left as it was measured; a new explorer pass would replace it.
- Totals moved within the noise, except where `book` moved them.
