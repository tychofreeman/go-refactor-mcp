# Does deriving let the model read less?

A plan, written to be run in a later session. The user's hypothesis
(2026-10-22), not yet supported by data: a model that thinks
algebraically, deriving a change as operations rather than rewriting
text, needs to read the code much less; perhaps the names of the
functions in a call tree are enough to decide where a change goes; and
this goes hand in hand with the model being able to derive on its own,
on shapes no move or example names.

Two questions, then:

1. **Reading:** does a derivation let the model read less, and can it
   plan from names and signatures alone?
2. **Deriving:** can it find a derivation for a shape it was not taught?

So far every learner run (`docs/learning-transfer.md`) used the five
shapes the user's moves were written for: billing and freight-xl are
notify under other names. They show the model applying moves it was
given, not deriving.

## Why it is plausible

A refactoring finds its own sites: `fold`, `inline_function` with
`all_calls`, `rename` and `add_parameter` reach every use through the
type checker, and a refusal says what is wrong. To send one, the model
needs names and signatures, not the bodies of the sites. Bodies are
needed where the behavior changes (the one edit) and to see that a shape
is there at all (that four methods are near-duplicates). Rewriting by
hand needs every site read; a derivation needs the shape and the edit
site.

On freight-xl the moves arm folded all 30 calls of `run` in one batch,
while the baseline rewrote each with `replace_text`. Whether it also
read less has not been measured.

## Step 1: reads in the freight runs (no new runs)

The transcripts are in `eval-runs/` on the user's computer:
`freight-human` (the user's moves), `freight-base`, and `freight-base2`
(the baseline's tail and run, rerun). For each arm and task:

    go run ./cmd/refactor-tokens eval-runs/freight-human/freight-run-mcp-*.jsonl
    go run ./cmd/refactor-tokens eval-runs/freight-base2/freight-run-mcp-*.jsonl

Report, per run, per task and arm: read calls; turns with a read; read
tokens by kind (`inspect` module overview, file, part, targets; Read;
Grep; Bash) both as sent once and as `context` (resent on later turns);
and Grep calls, which stand for "where is X used". Add one measure
`refactor-tokens` does not have, by a short script over the transcripts
(or in `internal/tokens`, if it is to stay): **bodies read**, the
functions whose bodies an `inspect` of a function or range, a Read or a
`sed -n` covered, against the functions whose bodies the run changed.

What it tells:

- On run and tail (the derivation did the uses) the moves arm reading
  clearly less, mostly in "where is X" and in bodies of call sites:
  derivations already save reading; go on to step 2.
- The same reading in both arms: the model reads by habit, before it
  plans; deriving does not save reading unless something lets it skip
  the read (the instructions of step 3, or the call tree of step 2).

Record the table in `docs/cost-history.md` (section 9) and the finding
here.

**Done** (2026-10-22; `eval-tasks/learning/reads.py`, with the
functions' line ranges from `go run ./eval-tasks/learning/funcs
eval-tasks/testdata/freight-xl`). Means per run, 4 runs each:

| task | arm | read calls | turns with a read / turns | searches | bodies read | bodies changed | reading's share of input |
|---|---|---|---|---|---|---|---|
| dispatch | baseline | 4.0 | 3.5 / 9.0 | 1.5 | 3.8 | 11 | 25% |
| dispatch | moves | 4.0 | 3.0 / 7.0 | 1.2 | 1.0 | 11 | 30% |
| routes | baseline | 3.2 | 3.0 / 9.2 | 2.2 | 0 | 63 | 24% |
| routes | moves | 2.5 | 2.5 / 7.8 | 1.5 | 0 | 65 | 25% |
| tail | baseline | 4.0 | 3.8 / 8.5 | 2.0 | 15.8 | 12 | 27% |
| tail | moves | 2.0 | 2.0 / 5.5 | 1.0 | 3.0 | 12 | 14% |
| run | baseline | 2.8 | 2.8 / 6.0 | 1.2 | 4.0 | 31 | 24% |
| run | moves | 2.5 | 2.5 / 4.5 | 1.0 | 2.2 | 31 | 23% |

Reading's share is the context of the reads (each result times the
turns it stays) over the run's input. Bodies read are those an
`inspect` of a function or range, a Read or a `cat`/`sed -n` covered,
by the module's line numbers as given (approximate once edits move
them); bodies changed are the harness's changed functions.

What it showed:

- **Both arms read the same way, and little:** two to four read calls
  a run, one to two of them searches, and almost no bodies of the
  code they change (routes changed about 63 bodies having read none;
  run 31, having read two to four). Neither arm reads bodies to find
  sites; both find them with a search.
- **The searches are most of reading's cost,** about a quarter of each
  run's input in both arms: a grep's output, sent once, is resent on
  every later turn. On run the moves arm spent 20% of its input on
  grep output (140K of 688K), for a derivation (wrapper, `fold`) that
  found all 30 calls itself.
- Only tail differs: the moves arm read fewer bodies (3 against 16)
  and half as much (14% against 27%), in fewer turns.

So, the plan's second outcome: the model reads by habit, before it
plans, and deriving does not save reading by itself; the moves arm
saves turns, not reads. The reading that a derivation makes needless is
the "where is X used" search, which CLAUDE.md's "don't search before a
refactoring" asks it to skip and it does not. Step 2 tests whether a
plan can be made without it.

## Step 2: plan from a call tree (a probe, one turn a question)

Whether names and signatures are enough to plan, without bodies or a
tool loop.

**Make the call tree.** A small command, `cmd/calltree` (or an option
of `inspect`, if it is to become a tool): for each package, each
function and method with its signature and doc comment's first line,
and the functions it calls in the module, indented; no bodies. Static
calls from `golang.org/x/tools/go/callgraph/static` over `go/ssa`
(x/tools is already a dependency); calls through interfaces listed as
the interface method. Check its size on freight-xl against the
`inspect` skeleton of every file and against the whole module.

**Ask.** For each freight task (dispatch, routes, tail, run) and, once
step 3 has them, each task of set C, give a fresh model (`claude -p`,
sonnet, no tools) the task's prompt, the tool descriptions of the
refactorings (as the server sends them), and one of three views of the
module:

- the call tree;
- the `inspect` skeleton of every file (no bodies, no calls);
- the whole module.

Ask for two things:

- **A. Where:** the functions whose code must change, as a JSON list.
- **B. The derivation:** the change as a `batch` of gorefactor steps,
  in the CLI's JSON, with any line written by hand as an edit step.

Four answers per task and view: 3 views x 4 tasks x 4 = 48 calls, about
a dollar or two (the whole module is about 12K tokens).

**Score.** A against the functions the reference solution changes
(precision and recall; write the reference by the tools on a copy and
keep it, `eval-tasks/testdata/<task>-ref`, since none is stored now).
B by running it: `refactor batch @answer.json` on a fresh copy, then
the task's hidden test; score passed, first refused step, and lines
written by hand. A derivation that passes from the call tree alone is
the direct evidence the hypothesis needs. Keep the answers and scores
under `eval-runs/calltree-probe`.

What it tells: if the call tree scores close to the whole module on A
and B, names and signatures are enough to plan; a call-tree tool and
the instructions of step 3 are worth building. If the skeleton scores
as well as the call tree, the calls add nothing over `inspect`. If only
the whole module plans well, bodies carry what the plan needs, and the
hypothesis holds at most for the shapes the tools already find.

**Done** (2026-10-22; `eval-tasks/learning/probe.py`, the views by
`cmd/calltree` and `eval-tasks/learning/views`, the references in
`eval-tasks/learning/refs/`, each made by the tools and passing its
hidden test; 48 answers, $2.73, `eval-runs/calltree-probe`). The views
of freight-xl: call tree 13.4K characters, skeleton 27.7K (larger than
the code: its functions are so short that inspect's metrics outweigh
the bodies it leaves out), whole module 22.4K.

| task | view | A precision | A recall | B passed | B lines by hand |
|---|---|---|---|---|---|
| dispatch | call tree / skeleton / whole | 1.00 / 1.00 / 1.00 | 1.00 / 1.00 / 1.00 | 3 / 4 / 4 of 4 | 6 / 5 / 10 |
| routes | call tree / skeleton / whole | 0.98 / 1.00 / 1.00 | 0.95 / 0.94 / 1.00 | 0 / 0 / 0 of 4 | 10 / 6 / 25 |
| tail | call tree / skeleton / whole | 1.00 / 1.00 / 1.00 | 0.94 / 0.90 / 0.98 | 3 / 2 / 4 of 4 | 6 / 9 / 16 |
| run | call tree / skeleton / whole | 1.00 / 1.00 / 1.00 | 1.00 / 1.00 / 1.00 | 1 / 0 / 2 of 4 | 8 / 8 / 8 |
| all | call tree / skeleton / whole | 1.00 / 1.00 / 1.00 | 0.97 / 0.96 / 0.99 | 7 / 6 / 10 of 16 | |

What it showed:

- **Where: names are enough.** From the call tree alone the answers
  named the functions to change with precision 1.0 and recall 0.97,
  as well as from the whole module (0.99).
- **The derivation: the plans were right; the steps failed on the
  tools' interface.** Every answer for run planned wrapper, `fold`,
  `inline_function`, `rename`, the reference's chain, from every view;
  dispatch planned `fold` of a declared `dispatch`; tail `fold` and
  `inline_function`. The refusals:
  - `rename` with `function` set to the function's own name (8
    answers, all on run): `function` names the function a local is
    in, so `rename` of `Dispatch2` refused ("Dispatch2 declares no
    Dispatch2"). A trap in the tool's interface. One more run answer
    left code that did not type-check.
  - The tools' argument names (8 answers: `expr` or `old` for
    replace_expression's `expression`, `struct` for add_field's
    `name`, replace_statements without `file`): the probe gave the
    descriptions without the input schemas a session loads, so this
    part is the probe's, not the model's.
  - Removing `Road` while test files still set it in literals (7
    answers, routes): no view shows the tests.
  - `fold` itself (1 answer, dispatch): with the new `dispatch` in a
    file of its own, folding all ten carriers left `log` unused in
    freight.go, and fold does not remove an import its folds leave
    unused. A bug.
- The whole module passed more (10 against 7 of 16), mostly by chance
  in which answers fell into the `rename` trap; bodies did not change
  the plans.

So names and signatures are enough to plan both where the change goes
and the derivation that makes it, on these shapes; the calls add little
over the skeleton for that (they agree), and the failures to apply are
fixable in the tools: rename by name where `function` repeats it, fold
removing the imports it leaves unused, and the test files' uses shown
or reported. Step 3, on shapes no move names, is the open test.

## Step 3: shapes no move names, on a large module

**Set C.** A new module the size of freight-xl, with many uses of
everything, made by a generator as `gen_freight.py` is
(`eval-tasks/learning/gen_<name>.py`), in a domain unlike notify,
billing and freight. Five tasks whose shapes are in no move, no lesson
and no tool description's example, for instance:

- **split a function into phases:** a long function that parses,
  validates and applies becomes three, called in turn, each used
  elsewhere (one caller needs only the parse);
- **a switch on a kind becomes methods:** a `switch x.Kind` repeated in
  several functions becomes a method per kind on an interface;
- **a global becomes a parameter:** a package variable read deep in a
  call chain is passed down from the top, through every function on the
  way;
- **split a type:** a struct whose fields fall into two groups used by
  different code becomes two types, the second a field of the first;
- **an additive feature that needs one of the above first** (like
  freight-blocked), so that deriving is a means, not the task.

Each task as in `derivations-freight.json`: a prompt that names every
exact string and signature the hidden test checks, hidden tests named
`TestAccept...` that fail on the module as given and pass with a
reference solution, made by the tools and kept (`<task>-ref`). Before
using a shape, check that `docs/derivations.md`, the instruction sets
and the tool descriptions have no example of it; keep the tasks out of
every instruction set later, as set A and B were kept apart.

**Arms,** the mcp arm, 4 runs a task:

- `claude-md-refactor-first-inspect`, the baseline;
- `claude-md-derivations`, the user's moves;
- `claude-md-algebraic`, new: the method and no shapes. A draft:

  > Treat the code as a structure you reshape with operations, not
  > text you rewrite. Before reading any function body, write the
  > change down as a derivation: operations that keep behavior, ending
  > in the one small edit that changes it. Plan from names and
  > signatures (`inspect` of a file shows them without bodies); the
  > operations find every use through the type checker, so you need
  > not read the code they change, and a refusal says what is wrong.
  > Read a body only where the behavior changes, or to check a shape
  > you are not sure of. Writing Go with replace_statements,
  > replace_declaration, replace_expression or replace_text is writing
  > by hand: keep it to the fewest distinct lines. An operation counts
  > as tried only once called; `undo` reverts a step.

- if step 2 showed the call tree helps: the algebraic arm with the
  call tree as a tool (`inspect` with `calls`, say), as a fourth arm.

    go run ./cmd/refactor-ab -tasks eval-tasks/derivations-<name>.json \
      -model sonnet -arms mcp -runs 4 \
      -claude-md eval-tasks/claude-md-algebraic -out eval-runs/c-algebraic

Cost: freight-xl was about $0.15-0.20 a run, so 5 tasks x 4 runs is
about $3-4 an arm, $10-16 for three or four arms. Ask the user before
each run. Stay on the branch with the module until the runs end (the
harness copies it from the working tree).

**Measure,** per task and arm: passed, cost, turns, lines written by
hand (distinct), derived share; reads as in step 1, with bodies read
against bodies changed; and whether the run derived: read each
transcript for the sequence it took and whether it planned before
reading bodies.

What it tells:

- The algebraic arm derives on set C (high derived share, few lines by
  hand) where the moves arm does not: the method transfers to new
  shapes, moves do not.
- It also reads less: both parts of the hypothesis, and that they go
  together.
- It derives but reads as much: deriving does not by itself save
  reading; a call-tree tool is the next test.
- Neither arm derives: the shapes need operations that are missing;
  each run's hand-written steps name them (the friction log's way).

**Built** (2026-10-22). `lending-xl`, a library's lending
(`eval-tasks/learning/gen_lending.py`, 748 lines in five packages),
`eval-tasks/derivations-lending.json`, hidden tests in
`testdata/lending-*`, and a reference for each task made by the tools
and passing its hidden test (`eval-tasks/learning/refs/lending-*.json`):

| task | the shape | uses | the reference |
|---|---|---|---|
| phases | `Import` parses, checks and adds in three loops; split into `ParseRecords`, `ValidateRecords`, `ApplyRecords` over a new `Record` | one function | edits: the three functions and `Import` calling them |
| kinds | a `switch it.Kind` in five functions; an interface `Kind` with a type per kind | 5 switches, 3 comparisons in two other packages | edits: the types, five bodies, `Import`, the comparisons and test literals |
| policy | a package variable read four calls deep; passed down from `Settle` and `Warn` | 17 calls of `Settle`, 2 of `Warn` | derivation: `add_parameter` up the chain (19 calls of Settle updated), the reads pointed at the parameter, `remove_dead_code` |
| address | `Street`, `City`, `Zip` of `Member` into `Address`, held as a named field `Home` | about 55 reads in three packages | `extract_struct` (embedded), then 73 replacements by hand to name the field |
| dryrun | an additive feature: check without adding | | derivation: `extract_function` of the two checking loops, then a 10-line `DryRun` |

`Import` was written as three loops, parse, check and add, so that
splitting it does not change which error a text with two bad lines
reports. Checked before using the shapes: `docs/derivations.md`, the
instruction sets and the tool descriptions name none of them ("phase"
there is the two-phase rule; add_case's example adds a clause).

The algebraic arm, `eval-tasks/claude-md-algebraic`: the baseline
(`claude-md-refactor-first-inspect`) and the method of the draft above
under "Work as a derivation", with "or search for" added, since step 1
found the searches are most of what reading costs.

Found while making the references: no operation turns a switch on a
kind into methods, or makes an embedded field a named one;
`remove_dead_code` refused `var policy = DefaultPolicy()` as an
initializer with side effects, though `DefaultPolicy` only returns a
literal.

**Run** (2026-10-22; `eval-runs/lending-base`, `lending-moves`,
`lending-algebraic`; 5 tasks, 4 runs each, $10.68). All 60 runs passed.

| arm | cost a run | turns | lines by hand | derived share |
|---|---|---|---|---|
| baseline | $0.176 | 10.7 | 1,285 | 31% |
| the user's moves | $0.186 | 13.1 | 1,215 | 33% |
| algebraic | $0.173 | 12.6 | 1,141 | 37% |

By task, lines written by hand (derived share), over 4 runs:

| task | baseline | moves | algebraic | operations used |
|---|---|---|---|---|
| phases | 500 (6%) | 469 (12%) | 474 (11%) | none, but one `extract_function` |
| kinds | 396 (7%) | 394 (4%) | 375 (19%) | none in any run |
| policy | 182 (43%) | 141 (52%) | 77 (69%) | `introduce_parameter` and `add_parameter` up the chain, every run |
| address | 51 (86%) | 58 (86%) | 48 (91%) | `extract_struct`, then `rename` of the embedded field (10 of 12 runs), refused: "renaming embedded fields is not supported yet"; then `replace_text` |
| dryrun | 156 (43%) | 153 (45%) | 167 (42%) | `extract_function`, every run |

Reading (`eval-tasks/learning/reads.py`): alike in every arm, 1 to 4
read calls a run with 1 to 2 searches, and about the same bodies read;
the algebraic arm's instruction not to search or read bodies did not
change it.

What it showed, by the plan's outcomes:

- **Where an operation fits, the arms derive, and the method helps a
  little:** on policy every run of every arm passed the parameter
  down with `introduce_parameter` and `add_parameter`, and the
  algebraic arm wrote the fewest lines by hand (77 against 182 for
  the baseline, 69% derived against 43%); on dryrun every run
  extracted the checking loops. These are the shapes the tools
  already name, not new derivations.
- **Where none fits, no arm derives:** kinds (a switch becomes
  methods) and phases (a function split around a new type) were
  written by hand in every run, without an attempt at an operation.
  The method did not lead to a derivation from smaller operations;
  it needs an operation that is there.
- **The runs name the missing operation:** on address, 10 of 12 runs
  called `rename` on the embedded field `Address`, which is refused.
- **Reading:** deriving or not, every arm read the same way; the
  instruction did not make the model read or search less.

So the hypothesis's two parts did not show on new shapes: the method
transfers only where an operation already fits (and there it saves
lines, not turns or cost), and it does not reduce reading. The gaps the
runs name are the next work: `rename` of an embedded field to make it
a named one, and an operation for a switch on a kind (methods per kind
from the cases). The first is done: the address task is now
`extract_struct`, then `rename` of `Member.Address` to `Home`, which
names the field in every selector that reached through it. For the
second the user chose two general operations: `type_switch_to_method`
(done), and a type code into types (next).

## Bodies read and not used (Oct 23, no new runs)

The user's question after the long chains: how many reads were
unnecessary, bodies read where names and signatures would do? Measured
over the 160 sessions of `eval-runs/level-long-5` (hybrid arm) with
`eval-tasks/learning/bodies.py`: each function body a read showed
(a skeleton's elided bodies and one-line functions do not count), by
what the session did with it, matched against the text of every edit
it made (Edit, Write, Python and shell scripts, gorefactor edits and
batches). A heuristic; a strict and a loose rule (`MINLEN=15`, `6`)
bracket it, and four sessions checked by hand agreed.

| sessions | bodies read | edited | copied from | unused, asked for | unused, came with a file | unused share of body text |
|---|---|---|---|---|---|---|
| all 160 | 9.1 | 0.6 | 1.2-1.6 | 1.8-1.9 | 5.1-5.5 | 71-75% |
| control features, lending and freight | 17 | 0.3-0.5 | 3-5 | 2-4 | 8-12 | 66-72% |
| features after the step | 6-10 | 0-0.4 | 0.2 | 0.1-3.4 | 2-7 | 87-92% |
| preparing, freight | 15 | 10 | 2 | 0.4-0.6 | 3 | 22-26% |

- About three quarters of the body text read reached no edit, and most
  of it came with a whole file (`lending.go` read to change one switch
  or add one row). Those are the reads where names and signatures
  would have done; at most the model checked that nothing else needed
  the change, which `inspect`'s skeleton and `uses` answer without
  bodies.
- A body copied from (the nearest case, copied for a new kind) was
  needed, unless an operation did the copying.
- After the step the change was a table row, yet the feature sessions
  still read 6-10 bodies and used almost none: the habit of reading a
  file before planning.
- In tokens it is small here: all Go reads, each counted once per
  request it stays in context, were 3.7% of a session's input (about
  150K), the unused bodies 0.7-0.8%. Most of a request is the fixed
  context (instructions, tool schemas, the conversation). On code where
  a file is thousands of lines the same habit would cost much more.

Next: a server option that lets `inspect` read source only by name
(roadmap item 24), so a run can test planning from names and
signatures with the bodies a name asks for. Built: `refactor-mcp
-read-by-name`; the run is still to do.

## Recording

Each step in its own PR: the results in `docs/cost-history.md`
(section 9 or a new section), the finding in `docs/session-notes.md`,
this file updated with what was learned, and roadmap item 24 marked.
