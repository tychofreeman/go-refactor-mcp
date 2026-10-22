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

## Recording

Each step in its own PR: the results in `docs/cost-history.md`
(section 9 or a new section), the finding in `docs/session-notes.md`,
this file updated with what was learned, and roadmap item 24 marked.
