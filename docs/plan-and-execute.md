# Plan, then execute: a loop of our own

A plan, written to be run in a later session (the user, 2026-10-25).
The question: should we build an agent loop of our own that makes the
change, in place of a model driving the tools turn by turn in Claude
Code? This revisits a decision: on 2026-10-17 the user set the Ollama
loop (`cmd/refactor-agent`) aside because Claude through MCP is the
product. That decision stands; this is a research instrument, and the
user decides on its result whether it becomes more.

## Why now

- With the tools, both Sonnet and Haiku cost more than editing by hand
  on freight-xl (cost-history 14: Sonnet 67% more, Haiku 41%), though
  every run passed in every arm.
- The tools' context in each request is not the gap: about 1.8K tokens,
  4-8% of a session (cost-history 13). The gap is turns: the mcp arm
  takes about twice the requests a change by hand takes.
- About 38% of an A/B session's cost is Claude Code's first request,
  written fresh in every session (cost-history 12), which no arm can
  avoid inside Claude Code.

So what costs is the conversation: a model steering a dozen small tool
calls, one turn at a time. A loop of our own changes that, not the
engine.

## What a loop of our own can do

1. **Plan once, execute deterministically.** The model gets the task and
   the module's names and signatures (`cmd/calltree`, or the skeleton),
   and answers with the whole derivation as one batch; the engine runs
   it; only a refusal or a failing test goes back, as a short repair
   turn. Two or three requests in place of 7-20.
2. **Its own context.** A short prompt with only what a batch needs (the
   steps' arguments, from the tools' input schemas), and a cache layout
   we choose.
3. **A model per part.** The strongest model for the plan; Haiku, or no
   model, for repairs: many refusals already say what to send.

## The evidence so far

The call-tree probe (`docs/reading-and-deriving.md`, step 2;
`eval-tasks/learning/probe.py`) is the planning half: one turn, no
tools, the answer a batch run by the CLI. On freight-xl, from the call
tree alone, 7 of 16 batches passed the hidden tests (10 of 16 from the
whole module), and the failures were not the plans:

- `rename` with `function` naming the function itself (8 answers, all
  on run), a trap in the tool's interface;
- the steps' argument names (8 answers): the probe gave the tools'
  descriptions without their input schemas;
- test files no view showed (7 answers, routes): `Road` removed while
  tests still set it;
- a `fold` bug, since fixed.

Each is something a repair turn, the input schemas, or the test files'
uses in the view would answer. An answer cost about $0.06 with Sonnet
(48 for $2.73), against $0.10 for Sonnet editing by hand on the same
tasks and $0.16 with the tools (cost-history 14), and the probe still
ran inside `claude -p`, with Claude Code's context.

## Step 1: the probe with repairs (no new code but the script)

Extend `probe.py` into `eval-tasks/learning/loop.py`:

- **The plan:** one request, as the probe asks now, with three changes:
  the steps' input schemas in place of the descriptions alone (the
  server's `tools/list`, or each refactoring's `InputSchema()`; `refactor
  list` prints only the descriptions), the test files' uses of
  what the task changes in the view (`inspect` `uses`), and only the
  call-tree view (the cheapest, and as good for planning).
- **Execute:** the batch on a fresh copy (`apply`, as now), then the
  hidden tests.
- **Repair:** on a refusal, or a test that fails, one request with the
  plan, the refusal or the test output, and the view, asking for the
  batch again; at most two repairs. A run passes when the tests do.
- **Models:** the plan and the repairs with the same model, Sonnet and
  Haiku, `-model` as the probe's `ask` has it (today Sonnet only).

Run on freight-xl (dispatch, routes, tail, run), 4 runs a task and
model, about $2-4:

    python3 eval-tasks/learning/loop.py -tasks eval-tasks/derivations-freight.json \
        -only freight-dispatch,freight-routes,freight-tail,freight-run \
        -models sonnet,haiku -runs 4 -repairs 2 -out eval-runs/loop-freight

Compare with `eval-runs/model-sonnet` and `model-haiku` (cost-history
14), the manual arm with the same model on the same tasks: passed,
cost a run, requests, lines written by hand (`written`), and refusals
by kind. Then lending-xl (set C), whose shapes no move names, the
harder test of planning.

What it tells:

- The loop passes as often as editing by hand and costs less with the
  same model: plan-and-execute pays, and step 2 is worth building.
- It passes but costs about the same: the saving is in the requests,
  but Claude Code's per-session context eats it; step 2's own context
  is the open question.
- It fails where editing by hand passes (routes, where no operation
  fits): the loop needs a fallback to edits it can write itself, or is
  only for derivations the tools can do.

## Step 2, only if step 1 pays: a runner

The API directly (not `claude -p`), with our own prompt and caching:
the step reference and the view as a cached prefix, the task and the
repairs after it. The engine in-process: `cmd/refactor-agent` already
starts the server in-process and connects a client to it (`connect`);
its Ollama model loop is not reused. A manual arm in the same runner
(the model writes whole files or edits; the runner applies them and
runs the tests), so a difference between the arms comes from the
derivation, not from the runner's context.

## What the planning half showed (cost-history 16)

With the input schemas and the file list, Sonnet's call-tree batches
passed 11 of 16 on freight-xl (7 before), and Haiku planned as well as
Sonnet at a tenth of the price (15 of 36 against 19 of 36 over freight
and lending). The failures were content for bodies the view does not
show, add_parameter refusing a parameter that hides the package
variable, and tests using a removed field. Step 1's repairs should send
the bodies a refusal or failing test names.

## Step 1, built (2026-10-25)

`eval-tasks/learning/loop.py`, as above, with what section 16 showed:
the first answer may instead ask to read the declarations it must write
code for (once); a refusal's places are sourced from a copy with the
steps before the refused one applied, since the batch rolled them back;
an answer that does not parse gets a repair turn; `-models ref` answers
with the reference batches and passes all nine tasks, for free. Each
request runs `claude -p --tools "" --strict-mcp-config`: with Claude
Code's tools in the request, Haiku's plan cost $0.0114, without them
$0.0081 (replacing the system prompt too, $0.0070). Of the ~31K tokens
written, most is the prompt's input schemas (75K characters). Two
smoke runs of Haiku on freight-tail asked to read 11-12 bodies and then
rewrote them with replace_text, rather than fold, as section 16 found
for bodies in view; one passed ($0.021, 2 requests, against $0.0099
for Haiku by hand), one answered JSON that did not parse, which now
gets a repair.

## Step 1, run (cost-history 17)

Haiku without reads: 12/16 at the cost of Haiku by hand; Sonnet 14/16
at 1.8 times. Before step 2, three fixes: a shorter tools reference
(the prompt is the cost), a "done?" turn when the module's tests pass
(two Sonnet answers planned in stages and the loop stopped after the
first), and the stub bug (a batch re-declared the fields its own first
step removed). Then lending-xl.

The stub bug is fixed: a batch takes what the module declares when it
begins, and declares no stub for a name that was declared then (a
package-level name by its file, a field or method by its type); the
refusal says that an earlier step removed it.

The "done?" turn: when a batch applies and the module's tests pass, the
loop asks whether the change is complete; if not, the model's next batch
runs on what the kept batches left (`-stages`, default 2 more), and a
repair restarts from there, not from the module at the start.

## Step 1, rerun (cost-history 18)

With the three fixes the loop costs what editing by hand does (Haiku
10% less, Sonnet 10% more), passing 11/16 and 14/16 against 16/16.
What fails is routes, where remove_dead_code's refusal names only the
first use left, so each repair fixes one file. It now names every use
left, sorted, with their count ("Road is used in 3 places: ..."). Then
lending-xl.

## Step 1 on routes again and on lending-xl (cost-history 19)

With the refusal listing every use, Sonnet passed routes 3/4 (2/4
before), one attempt by fixing four uses in one repair; Haiku stayed
at 1/4, its plans rewriting the uses instead. On lending-xl the loop
passed Haiku 11/20 at $0.012 and Sonnet 15/20 at $0.146, against
Sonnet in a session with the tools 20/20 at $0.176. Kinds failed every
attempt: the plans named the derivation but broke on
prepare_switch_for_types after declaring the interface's method
themselves, and on narrowing the field while uses still needed the old
type. The next fixes, if any: prepare_switch_for_types accepting an
interface that already has the method, and a narrowing refusal that
says which steps come first. Whether to go on to step 2 is the user's
call.

Both are done (#338, #339): prepare_switch_for_types uses a method the
interface has when its signature is the one the cases need, and the
narrowing refusal ends with what comes first: "No type implements Kind
yet: make a type for each kind first, ...", or, once some do, "Write
each kind's type (Book, DVD) where these uses have its old value, then
narrow." Section 10 found that a refusal naming the right steps did not
change what the model did next in a session; a rerun of kinds would
show whether it does in a repair.

It did not pass (cost-history 19, kinds again: 0/8): both refusals
fired, and the attempts then stopped on code they could not see. Kinds
needs the cases' bodies; a run without `-no-read`, where the first
answer can ask for them, is the test of that.

With reads, kinds passed 8/8 (Haiku $0.020, Sonnet $0.197, against
$0.25 for Sonnet in a session), but by rewriting with edits, not by
the derivation: no attempt used prepare_switch_for_types. So reads are
what a moved-code task needs, and they seemed to trade derivation for
edits. But a read then came once, before the first batch, of functions
only, and without line numbers, which the line-addressed steps of the
derivation (extract_function, introduce_parameter) need: none of the 8
attempts used one.

With reads numbered, of types and variables too, allowed in any turn
(cost-history 19, kinds with numbered reads): 7/8 at the same cost, and
the same edits. Every attempt read once, at the start; none used a
line-addressed step or prepare_switch_for_types. The read's form was
not the cause: with the bodies in view, the models write the types out.
A derivation here would need a step addressed by name that moves a case
into a type's method, so the plan has no reason to see the body.

The user proposed an address in place of a tool: "the body of the X
case", for extract_function (and replace_statements). Built (README,
roadmap item 32); the kinds reference is now the derivation with it,
without a line number or a case body, and passes the dry run. A run
without reads would show whether the plans take it.

They mostly did not (cost-history 19, kinds with the case address:
0/8). One Sonnet attempt used it for every case and wrote the methods
as calls of the extracted functions, but ran out of stages; Haiku
declared `Label(title string)` from the prompt, which
prepare_switch_for_types refuses for cases that use `it`; the other
Sonnet attempts asked for the bodies or wrote methods that call back
into the switch. Candidates: accept a bare word for a string case
(`book` for `"book"`); say in the plan prompt that steps addressed by
name move code the plan does not see; more stages; prepare taking a
method whose parameters are what the cases read (`it.Title` as
`title`).

With the first two (cost-history 19, again): still 0/8, but 5 of 8
plans now derived by case, none asked for bodies. One refusal rolls
back a batch of 40-69 steps, and two repairs clear two of the four or
five refusals each plan meets: a case of several values not yet split,
a kind with no case, the receiver not first, function_to_method on an
interface's method name, and a bug in the bare word's retry.

With those fixed (cost-history 19, again): 0/8, every attempt stopped
at a kind with no case. A refusal inside a batch gives lines of the
module at the refused step, and a repair resends the batch from the
start, so a line it copies from the refusal points at other code.

With that and a missing case fixed (cost-history 19, again): 0/8, but
the derivation of the cases goes through in every attempt that got
that far; what fails is the edit in Import, which no plan can see
without a read, and the stages run out. The next test is reads and
the case address together: reads where an edit needs the code, the
address where code moves.

But reads in the first answer turned every earlier plan into rewrites
(the user, 2026-10-26: reads make the model default to edits). The
case bodies and Import are needed at different times: the cases move by
name, and Import's edit needs its body only once they have moved, when
the switches hold one-line calls and nothing is left to rewrite. So
`-read-after`: the first batch must derive without reading and leave the
module's tests passing; once a batch applies, the model may read for
the edits that remain. A read asked for before then is answered that
source comes after a batch (and counts against the three reads). The
reference's first 52 steps, the cases moved and the string cases kept,
pass the module's tests, so such a first batch exists.

Run (cost-history 19, reads only after a batch): 7/8 at the cost of
reads at the start, and the same rewrites. Every first batch prepared
the switches and stopped; every second answer read all five switches
and Import, then wrote the methods out. Deferring the reads deferred
the move with them.

With reads not mentioned until the "done?" turn (cost-history 19,
unannounced): 8/8, and two Sonnet attempts moved every case by name
in their first batch, then read Import and little else; the other six
prepared only and rewrote the switches after reading them.

## Recording

Each step in its own PR: the results in `docs/cost-history.md`, the
finding in `docs/session-notes.md`, this file with what was learned,
roadmap item 29 marked.
