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

## Recording

Each step in its own PR: the results in `docs/cost-history.md`, the
finding in `docs/session-notes.md`, this file with what was learned,
roadmap item 29 marked.
