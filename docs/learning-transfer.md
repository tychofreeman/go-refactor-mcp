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

The learned lessons (`claude-md-learned`) are the explore run's, less
Contacts, with lesson 3 from the shared-record trial. The learner runs
on B have not been run.
