# How this project was built

A record of the practices behind go-refactor-mcp, written to decide which
are worth repeating. It covers 15–29 October 2026: PRs #1–#479. It was
written on 18 October for the first four days (#1–#65), extended on 21
October (to #218) and on 29 October. It draws on the git history,
`docs/friction-log.md`, `docs/edits.md`, `docs/cost-history.md` and
`docs/session-notes.md`; judgments are Claude's. The appendix holds the
session notes as they were until 2026-11-04.

## The arc in brief

1. **Day 1 (15–16 Oct): a safe core.** A workspace loader, byte-range
   edits, an engine that refuses anything not type-checking before and
   after, `rename`, and an MCP server. CI (gofmt, tidy, vet, race tests) came
   the same day, before most features.
2. **Days 1–2: refactorings that simplify code.** `extract_variable`,
   `inline_variable`, `extract_function` (in phases), `invert_if`,
   `inline_function`, plus an Ollama agent loop and `refactor-eval`, a
   harness that let a local model simplify the most complex functions and
   measured the result.
3. **Day 2 (17 Oct): a change of goal.** The user decided the Ollama loop
   and batch "simplify" evaluation were not what mattered. The real goal:
   Claude, during ordinary feature work, picks refactorings, the tools
   perform them deterministically, and Claude makes the small edit the
   story needs ("make the change easy, then make the easy change"). The
   roadmap was reordered around that, and the friction log started.
4. **Days 2–3: refactorings that prepare a change.** `add_parameter`,
   `remove_parameter`, `move_parameter`, `introduce_parameter`,
   `move_declaration`, `extract_struct`, `merge_duplicate_function`,
   `remove_dead_code`, each chosen from friction-log evidence.
5. **Day 3 (17–18 Oct): edits.** Tools that change behavior on purpose
   (`create_file`, `replace_statements`, `insert_statements`,
   `add_declaration`, `replace_expression`, `add_field`), held to the same
   type-checking rule, with the aim of doing ~95% of coding through tools.
6. **Day 4 (18 Oct): statement refactorings and effect analysis.**
   `move_statement_up/down`, opt-in stress tests driving their design, and
   summaries of which of the module's own functions have side effects.
7. **Day 4 (18 Oct): measuring cost.** `refactor-tokens` (#72) counted
   what sessions spent by tool, and `refactor-ab` (#74) gave the same
   task to fresh models through the MCP and by hand, checked by hidden
   tests. The first findings shaped the next PRs: results were most of
   the cost, so they shrank (#73, #75, #76), and dependent steps cost a
   turn each, so `batch` came (#77), with line numbers as of the start
   of the batch (#82) and a check at its end (#87).
8. **Day 5 (19 Oct): what a fresh model gets wrong.** Tools listed by
   intention, `create_from_usage`, locals addressed by name, references
   to what an earlier step made (#90–#104), then the fresh-model fixes:
   arguments checked up front, ranges suggested in refusals, stubs that
   `add_declaration` replaces, edits allowed on code they fix
   (#113–#117). In this project's own sessions, input tokens per edit
   step fell from 1.2–1.9M (Oct 17) to 170–190k.
9. **Day 6 (20 Oct): turns, not reads.** A coaching tip in results did
   not help and was reverted (#119, #120). Reading through `inspect`
   without bodies (#121) changed how the model read, not how much. The
   finding that input tokens track requests (about 39K each) moved the
   work to wasted turns: schema loads (#128), denied writes (#133,
   #134).
10. **Day 6: a full code review** (#130) found 25 problems, 7 of them
    silent behavior changes. All were fixed, one PR each, chained and
    merged in order (#136–#170).
11. **Day 6: other people's code, and type reasoning off the model.**
    172 candidate PRs from five large repositories became A/B tasks
    (#137, #168). The transcripts showed type-check refusals of the
    model's own code, which led to more stubs, imports fixed after every
    edit, signature changes across interface families (#181–#190), the
    user's alias workflow for a parameter's type (#193, #194), lazy
    stubs and checkpoints in a batch (#197, #198), and edits for files
    and whole declarations (#203, #204).
12. **Days 6–7 (20–21 Oct): working algebraically.** The user's thesis:
    code is a structure reached by type-safe operations, with small
    edits at the leaves. A measure of it (#207), five problems the user
    solved as derivations (#208), the operations they lacked (`fold`,
    `parameterize`, #209–#212), and an A/B of guidance in the user's
    moves (#214, #215): no saving on a small module, but on code with
    many uses 14% less cost and 39% fewer lines written by hand.
13. **Days 7–8 (21–22 Oct): what a fresh model can learn, and fold.**
    Exploration runs asked models to try several derivations and write
    lessons for later ones (#227–#244). Lessons halved the lines written
    by hand but not the cost, and a lesson that named a workaround went
    stale once the tools improved. `fold` grew until it folded
    near-duplicates in one call (#245–#253), on the user's decision to
    let a nil dereference move past calls that return. Descriptions
    became shorter, with one example per use (#255–#261). Their examples
    moved to a garden domain that no eval task uses (#294).
14. **Days 8–9 (22–23 Oct): reading, deriving and the design question.**
    From names and signatures alone the model planned the reference
    derivations (#262–#269), so what failed was the tools' interface.
    The kind-into-types derivation was built and run end to end
    (#272–#285). Probes showed that a fresh model sees a type code when
    asked about design, but extends it when asked for a feature. A
    preparing session of its own made the design in 15 of 15 runs,
    against 0 of 15 without it. It did not pay back on small modules
    (#291–#304).
15. **Days 10–11 (24–25 Oct): cost, a shared matcher and a loop of our
    own.** Where a session's cost goes (#308): cache writes 69%, and the
    first request is 38% of the total. fold's matcher became
    `internal/pattern`, shared with `parameterize` (#311–#319). Cheaper
    models and trap tasks found no case where the tools beat editing by
    hand on tasks every arm passes (#322–#328). A plan-then-execute loop
    (`loop.py`) followed (#324 on), and was tuned over a long chain of
    runs (#330–#405).
16. **Day 12 (26 Oct): one analysis of what code does.** Six PRs merged
    Stable, MayPanic and the effect summaries into one, with
    standard-library facts in one table (#360–#365). On top came
    `analysis.Move`, one judgment of moving an evaluation (#379–#401),
    `Reach`, `Owner` and `site.Site`. Addressing a switch case by its
    value (#343–#378) let a plan that cannot read bodies still move
    cases.
17. **Day 13 (27 Oct): releases, speed and TypeScript.** The CLI gained
    `check` and `undo` (#408, #409). Releases are made by CI when the
    version changes (#410–#412). Reloads stopped compiling the module
    (#429): a 13-step batch went from 546 s to 21 s. The user chose
    TypeScript in this repo, behind one server. A plan, a spike on Hono,
    a language-neutral engine and a dispatcher came first (#414–#420),
    then the TypeScript tools (#421–#433).
18. **Day 14 (28 Oct): TypeScript's gaps.** Roadmap item 41 closed the
    gaps against Go one by one (#434–#456), with TypeScript eval tasks.
    `examples/CLAUDE.md` became the instruction set for users to copy
    (#439). CI's cache was fixed (#445).
19. **Day 15 (29 Oct): the user's derivations into the engine.**
    A parameter into a field, fields between structs, replace_statements'
    `wrap` and move_method (#459–#462); merging literals
    (#467); the recipe for calls split by an argument (#468); routing to
    other modules (#469); the rule that a gap names the compositions
    tried (#470); cached getters as reads (#471); inlining early returns,
    in loops and switches, with the default value hoisted, in Go and
    TypeScript (#472–#475); fields that fmt prints (#476, #477). Then an
    audit of every tool description against its code (#478), and of what
    TypeScript still lacks (#479).

## Practices and what they did for decision-making

### 1. A safety engine before features

Every step must start from code that type-checks and leave code that
type-checks, or it is rolled back and reported as a refusal. Refusals are
written for the model: what is wrong and what to do instead.

*Effect.* This made it safe to let a model (or Claude) apply tools freely:
a bad step costs one refusal, never a broken tree. It also made refusals a
design signal. When the rule was extended to edits, the user rejected an
exception for edits that leave type errors, and that forced a workflow
discovery recorded in the friction log: under TDD, write a compiling stub
before the failing test, so the tools can still run.

*Worth repeating:* yes. It is cheap early and hard to retrofit.

### 2. CI on day one, plus a local pre-push rule

CI ran format, tidy, vet and race tests from PR #1, and reported failures
as PR annotations. PR #41 (and the PRs stacked on it) still failed on
gofmt because a hand edit made with `sed` left an unsorted import;
`go test` had passed locally. The fix was a standing rule: run exactly
what CI runs before pushing.

*Effect.* Few broken merges, and the one class that slipped through
became a rule rather than a recurring cost. Tool-made edits are gofmt-ed;
hand edits are not, which is one more argument for doing edits through
tools.

*Worth repeating:* yes, including "write the lesson down as a rule".

### 3. Golden tests with a fresh-load check

Each tool has `testdata/<case>/{in,out,args.json}` (or `refused.txt`),
run in parallel. After every applied case the incrementally updated
workspace is compared with a fresh load, and the step is undone and
compared with the input again. Round-trip tests check that
`inline_variable` undoes `extract_variable`.

*Effect.* Fast, precise regression tests, and an always-on check of the
incremental reloader and undo, which no single feature test covers. Adding
a case is a few files, so refusals got tests as readily as successes.

*Worth repeating:* yes. The fresh-load comparison in particular caught
reloader bugs early (test variants of dependencies, PR #5).

### 4. Pivoting away from the evaluation harness

`refactor-eval` produced real findings: complexity alone rewards bad
extractions (a model extracted 31 lines into a function with 13 parameters
and 10 results), and applied counts must subtract undos. That led to a
"wide signature" note in `extract_function`. But it measured the wrong
thing for the actual goal, and the user stopped investing in it.

*Effect.* Ending it early freed most of days 2–4 for the workflow that
mattered. The harness stayed in the repo, parked rather than deleted.

*Worth repeating:* stating the goal in terms of the real use ("Claude
during a feature story"), and dropping work that measures something else,
even when that work is producing data.

### 5. The friction log

`docs/friction-log.md` records, from real work, every time the workflow
broke down: a refactoring done by hand, a refusal that got in the way, a
correct but awkward result. Each entry names the gap; later entries are
marked *Follow-up: done* when a tool closes them. `docs/edits.md` lists
candidate edits with the log entries that are their evidence, and states
the rule: a candidate without evidence stays at the bottom.

*Effect on decisions.* This was the most important practice. It turned
"what next?" from opinion into a ranking:

- The first entries (adding `refactor-eval`) named `change_signature` and
  a move-declaration tool; both became the next PRs.
- `rename` leaving stale doc comments showed up seven times in one
  session; it was fixed within the day.
- Hand deletions of unused declarations pointed at `remove_dead_code`.
- Today, small gaps (no way to create a directory, to name a method in
  `after`, to add a doc comment) were batched into one cleanup PR because
  each had a dated entry.

It also exposed its own bias: most entries come from building this tool
with itself, so the log keeps pointing at whatever was just built. That
is a reason to schedule ordinary feature stories, not more tool work.

*Worth repeating:* strongly. The cost is a paragraph per session.

### 6. Building the tools with the tools

From day 3, new features were written with the MCP edits and refactorings
(`create_file` for stubs and tests, `replace_statements` for bodies,
`add_field`, `add_parameter`, `extract_function` for cleanup), and every
hand edit was logged with the tool that would have covered it.

*Effect.* Constant, concrete evidence about usability: refusal messages
that pointed at reused line numbers, a `replace_statements` bug with case
clauses, the lack of `add_result` or a way to add a list element. It also
showed the tools work: some PRs had no hand edits in new code after the
first draft.

*Caveat:* the evidence is skewed toward a Go AST-manipulation codebase.

### 7. TDD as a project rule

`CLAUDE.md` asks for tests first, minimal code, then refactoring toward
the next change, and for the refactoring MCP to do refactorings. In
practice: a stub, a failing golden case, the implementation, then cleanup
with the tools.

*Effect.* Each PR arrived with its tests, and refusal wording was decided
up front. Thinking through expected outputs caught design errors
before running anything (e.g. field-level tracking was added because a
planned test, `p.x = 1; p.y = 2`, would otherwise have been refused).

*Worth repeating:* yes; add "compiling stub before failing test" when the
tooling requires compiling code.

### 8. Opt-in stress tests

Each refactoring has a stress test that applies it to every candidate in a
real module (this one, copied), counts applied and refused results by
reason, and, with `STRESS_TESTS=1`, runs the package's tests after every
applied change.

*Effect on decisions.* Three examples from the last day:

- The first `move_statement_up` run: no rollbacks, but 56% of refusals
  were "the statement returns", nearly all `if err != nil { return }`
  guards. That produced the rule letting a pure declaration move past a
  `return`, `break` or `continue`. Without the counts, a different
  limitation would likely have been chosen.
- The second-largest group was calls with unknown effects, which led to
  summarizing the module's own functions. A controlled comparison on
  the same 5,569 statements then showed the summaries raise applied moves
  only from 521 to 526. A breakdown of the remaining refusals showed why:
  about 500 are calls into the standard library, methods or function
  values, about 275 are module functions that call such code, and about
  390 are direct writes through pointers. The analysis is correct but
  aimed at the wrong bottleneck; the next lever is standard-library
  knowledge. Only the stress test could have shown this.
- A first comparison was invalid: adding files to the copied module changed
  the random sample. Comparing two tool versions needs the same input, and
  the cheap no-tests mode makes that fast.

They also found an infrastructure bug: child `go test` runs inherited
`STRESS_*`, so packages' own stress tests ran recursively on the same
copy, dozens of processes editing files under a live workspace. The fix
went into a shared helper used by all six stress tests.

So far the stress tests found no wrong move by `move_statement_*` (0
rollbacks, 0 test failures across ~300 moves with tests). Their value here
was measurement, not bug-finding. They are slow, so they stay out of CI.

*Worth repeating:* yes, for any tool whose rules are heuristics. Keep a
fast counts-only mode, and compare versions on identical input.

### 9. Small, stacked PRs and a written roadmap

Work went out as small PRs (65 in four days, 218 in seven), stacked when two touched
the same files, each with a summary, follow-ups and a test plan. The
README carries status, a roadmap ordered by what the workflow needed, and
a "what it checks" section per tool.

*Effect.* Easy review and course correction: the day-2 change of goal was
one roadmap PR. The "what it checks" sections double as the spec.
Stale docs were a real cost, though: one PR today was partly cleaning
text that still called finished work "on the roadmap".

*Worth repeating:* yes; treat doc staleness as a checklist item per PR.

### 10. Persistent notes between sessions

Short notes recorded decisions that would otherwise be lost between
sessions: the project's real goal, the rule that edits must type-check,
the pre-push checks. They are pointers to decisions, not a copy of the
code or history.

*Effect.* New sessions started with the goal and the constraints, and did
not re-argue settled questions (e.g. returning to the Ollama loop).

*Worth repeating:* yes, limited to decisions and their reasons.

*Later.* The notes became `docs/session-notes.md`, imported by
CLAUDE.md, with a rule to update them in the PR of the work. Two lessons
shaped that. Notes kept in a claude.ai Project and in memory did not
reach the user's Claude Code sessions, which read only the repo; review
follow-ups recorded only there and in the notes never reached the
roadmap. And a note pushed as a second commit after the user had seen
the PR was lost when the PR was merged without it (#122): later work
goes in a new PR.

### 11. A/B runs with fresh models

`cmd/refactor-ab` runs each task in a fresh copy with a fresh model per
arm (mcp: gorefactor only; hybrid: both; manual: built-in tools only),
checks the result with hidden tests, and records cost, tokens, turns and
the quality of the code left behind. `docs/cost-history.md` keeps every
run with what changed before it.

*Effect on decisions.* It replaced opinion about the tools with numbers,
and many of the numbers were unwelcome:

- Without a CLAUDE.md asking for it, a fresh model never used gorefactor.
- With the design in the way, the hybrid arm refactored with the right
  tool every time, but on a small module refactoring first did not make
  the second change cheaper, and plain editing stayed cheapest.
- Editing only through the MCP cost about 1.5 times editing by hand.
  What made this project's own sessions cheap (batches, checking at the
  end) came from knowing the tools, not from the tools alone.
- More tools, longer descriptions and a coaching tip did not help; doing
  for the model what it would get wrong did.

These findings set the work of days 5 and 6. The harness also needed
care of its own: a run cut off by a timeout first counted as costing
nothing (#164), and a module that no longer parsed produced a complexity
change of −10699 (#191). Both are marked now and left out of the means.

*Worth repeating:* yes, with hidden tests rather than the model's own,
and with tasks chosen to need the thing measured: additive features gave
the model "nothing to refactor first".

### 12. Counting waste, not totals

Run-to-run noise was 10–25% at two or three runs per task, so a fix that
saved a few refusals did not show in totals. The analyses moved to what
each turn was for: group a transcript's calls by assistant message to
count turns; weight each result by the turns it stays in context. That
showed input tokens track the number of requests, so reading less mattered
less than wasting fewer turns. `refactor-tokens` now does this, and the
`ab-analyze` skill says how to compare a run with a baseline.

*Worth repeating:* yes. Decide what a change is meant to remove, and
count that per run.

### 13. A code review as a backlog

A full review of the code at one commit (`docs/code-review-2026-10-20.md`)
listed 25 findings with a severity and a suggested fix order. The user
chose the order (the panic class first), and each finding became one PR,
branched from the one before and merged in order, with the review
document marking each fixed.

*Effect.* In one day every finding was fixed, and the shared analyses
they exposed were unified on the way (one dependency walk, one list of
functions that run a closure, one check for terminating statements). The
review found what the golden tests could not: rules that held for the
cases written but not for expressions that can panic or methods reached
by reflection.

*Worth repeating:* yes, once the tools had grown fast: a review written
down is a backlog that a session can take item by item.

### 14. Parallel sessions on one repository

From day 6, more than one session worked at once (one on third-party
prompts or A/B runs, another on tools), each with its own branches.

*Effect.* More got done, at the cost of conflicts in the shared docs: PR
numbers written into docs before the PR existed were taken by another
session's PR, and the friction log, appended to by every PR, conflicted
on most merges. The fixes: check the next PR number before writing it,
and order the friction log by the commit that added each entry, so a
merge places an entry rather than guesses.

*Worth repeating:* yes, with docs that tolerate concurrent appends.

### 15. Derivations from the user

Asked for examples of refactoring-first changes, the user could not think
of many off the top of their head. Posing small problems worked better:
the user answered with moves ("extract variables for what differs, move
them to the top, extract the rest", "delegate, redirect, inline",
"expand, migrate, contract"). Each was run with the tools, and what no
tool could do became a lettered gap (`docs/derivations.md`).

*Effect on decisions.* The gaps chose the next tools (`fold`,
`parameterize`, `extract_variable` with `pointer`), and the user steered
their shape: a tool that did one task in one call (`fields_to_map`, #217)
was closed in favor of a general operation that composes with the others
(fold literals through a setter, #218). The A/B of guidance in these
moves gave the first measured support for the thesis.

*Worth repeating:* yes. Ask how the user would do a concrete change, not
for a list; prefer options on existing tools and general operations to
task-shaped tools.

### 16. Probes before A/B runs

A probe asks a fresh model one question in isolation (`claude -p` in a
copy of the repo, read-only tools) for a few cents to two dollars, where
an A/B run costs several dollars and hours. Examples: would it recognise
the type code (three framings), can it plan from a call tree, does it
find a composition with the tools in view (`docs/composition-probe.md`).

*Effect on decisions.* Probes said which A/B was worth running and what
to change first. The composition probe found that Sonnet composed the
user's derivation in 8 of 9 answers, and was stopped only by one stale
clause in inline_function's description. That one clause was the fix.

*Worth repeating:* yes. Isolate the copy at a path that does not name
the checkout: a probe that found the real repo reviewed the wrong code.

### 17. Options put to the user, decisions recorded with them

When a rule could reasonably go more than one way, the session set out
two or three options with what each costs (keep refusing, recognise the
shape, allow with a note), and the user chose. The choice went into the
session notes with the alternatives and the reason.

*Effect.* Decisions held across sessions and were not re-argued: the nil
dereference past a log line, cached getters, fields that fmt prints,
TypeScript tolerating its existing errors. Where the user stayed unsure
(the nil dereference), the note says so, and the relaxation was kept
narrow and easy to revert.

*Worth repeating:* yes, with the losing options written down.

### 18. A budget of tools

The user worried that tools would multiply until their context
outweighed the gain. A test holds the count and the instructions' size,
raised only with the user. A new operation goes, in order, into a proof
in the engine, an option of an existing tool, or a composite. Only then
does it become a tool.

*Effect.* Most of day 15's features went into the engine (inline_function
rewrites early returns itself) or became options (`wrap`, `from`,
`to_file`). The budget was raised once, by the user, for move_method.

*Worth repeating:* yes, for any tool set a model chooses from.

### 19. A second language behind the same server

TypeScript came in through a written plan with open decisions, a spike
on a real project, and then a refactor of the engine into
language-neutral code before any TypeScript tool. The user chose one
server, routing by file extension, so that the shared code (diffs,
batches, the MCP server, the A/B harness) lands once.

*Effect.* About 40 TypeScript tools in two days, sharing batches,
checkpoints, undo and refusal handling with Go. The cost was gaps that
only showed when looked for: a checkpoint ran `go test` in a TypeScript
project until #479.

*Worth repeating:* yes. Plan and spike first, generalise the engine
before adding the language, then survey for gaps against the first
language.

### 20. Read-only surveys in parallel

Broad questions (do descriptions match the code? what does TypeScript
lack?) went to read-only subagents with a narrow brief, while the main
session went on with a PR. Their findings were checked against the code
before anything changed.

*Effect.* The description audit found eleven tools whose refusal lines
had fallen behind. The TypeScript survey found a real bug and ranked the
gaps. Both finished within one PR's test run.

*Worth repeating:* yes, for questions that would mean reading across
many files. Verify before acting.

### 21. The tool decides what does not change behavior

On 2026-11-07 java-refactor-mcp's user set the rule that the model
should not be concerned with layout (its #279): a rerun had shown a model
asking for a constructor `before` the getters, and the honest answer to
why that mattered was that it did not. Go took the same rule the same
day: `add_declaration` places a method after its type's methods, a
stub's replacement where the stub was, and the rest at the end, and
`after` and `before` were retired (accepted and ignored, so a model that
still sends them is not refused). TypeScript followed: `type` now names
the class members go in, which `after: "Bed.plant"` had done, and a
top-level `const` an existing statement uses goes before it, since
placed after it would throw at load. In the A/B transcripts at hand, 8 of 26
`add_declaration` calls had chosen a place. The rule is narrow on
purpose: where a position changes what code does (a struct's field
order, a slice's elements, a parameter's place, a statement's), the
choice stays the model's.

*Effect.* One fewer decision per new declaration, and one fewer name to
get right; not yet measured.

*Worth repeating:* yes, as a test for every option: if no answer changes
behavior, the tool should pick it.

The same day the rule's other half came over from Java (its #272 and
#275): code the language makes the model write, the tool writes. A
TypeScript class whose fields have no value needs a constructor that sets
them (TS2564), so `add_declaration` and `add_field` write it, and a
constructor the model adds later replaces it.

## What did not work as well

- **Line numbers.** Several tool calls reused line numbers from before an
  edit and were refused; re-inspecting after every change avoided that
  until batches translated lines through earlier steps (#82, #115).
- **Process leaks.** Long-running background jobs need an explicit check
  that they stopped; the recursive stress tests ran on unnoticed for a
  while.
- **Measuring before building.** The effect summaries were built on the
  strength of a refusal count, without first breaking down *which* calls
  were refused. A ten-minute breakdown first would have pointed at the
  standard library.
- **Tips and guidance in place of mechanisms.** A coaching tip in results
  (#119) and a reading paragraph in CLAUDE.md changed nothing measurable;
  checking arguments, suggesting ranges and declaring stubs did.
- **Tasks too small for the thesis.** Refactoring first, and later
  working as a derivation, showed no saving on modules with two or three
  uses of anything. Choosing tasks where the measured effect can appear
  took a second round each time.
- **Doc numbers and stale state.** PR numbers guessed in docs, roadmap
  items left open after the work, and notes kept outside the repo each
  cost a follow-up PR. Tool descriptions drifted in the same way: eleven
  "Refuses" lines were behind their code by 29 October.
- **Gaps logged that were compositions.** "No tool does this" was
  written for changes that two or three existing tools composed to. The
  user found the composition, and the session had not, with the
  descriptions in view.
- **This project's own habits.** Sessions read with `sed` and `grep`
  far more than with `inspect`. They refactored on the way to their own
  changes only where the derivation was the task. And they extended a
  type code while building the tool that removes one.
- **Long tuning chains.** The plan-then-execute loop went through about
  thirty runs on one task (lending kinds), each fixing what the last one
  showed. It ended with writing by hand cheaper on local changes and
  deriving cheaper where one step carries a change to many uses: a
  result a smaller design of the experiment might have shown sooner.
- **Scratch files reused by name.** A batch file named like one from an
  earlier session ran in its place. Undo caught it.

## Recommendations for future projects

In rough order of value for the cost:

1. State the goal as the real use, and reorder work when it changes.
2. Keep a friction log from the first day of real use, and pick work from
   it; mark entries done when closed.
3. A safety rule for every automated step (here: type-checks before and
   after, or nothing changes), with refusals written for the reader.
4. CI from the first PR, and the same checks locally before pushing.
5. Golden tests per tool, plus a consistency check of shared machinery
   (fresh load, undo) on every case.
6. Opt-in stress tests on real code for heuristic tools, with a fast
   counts-only mode; break counts down by cause before deciding what to
   build, and compare versions on identical input.
7. TDD, with a compiling stub before the failing test.
8. Small PRs with test plans; per-tool docs that serve as the spec; check
   for stale docs in each PR.
9. Short notes between sessions for decisions only.
10. Use your own tools to build the project, and log every manual step,
    but balance that with ordinary feature work so the evidence is not
    only about building the tools.
11. Measure with fresh models and hidden tests, on tasks that need what
    is measured; count the waste a change targets per run, since totals
    are noisy.
12. Keep notes and findings in the repository, the one place every
    session reads, and make docs that many sessions append to safe to
    merge.
13. Take a full code review as a backlog, one PR per finding, in an
    order the user chooses.
14. Collect the user's own derivations by posing problems; build general
    operations from their gaps rather than tools shaped to one task.
15. Before logging a gap, look for a sequence of existing operations
    that composes to the change, and write down the sequences tried.
16. Answer narrow questions with cheap probes before paying for an A/B
    run.
17. Put real choices to the user as options with their costs, and record
    the choice with the options it beat.
18. Budget the tools; prefer a proof in the engine or an option to a new
    tool.
19. For a second language, plan, spike and make the engine neutral
    first; then survey for gaps against the first language.
20. Change a tool's description in the PR that changes what it refuses,
    and audit descriptions against code from time to time.
## Appendix: the session notes until 2026-11-04

`docs/session-notes.md` grew to 1,175 lines, all of it imported into every
session. On 2026-11-05 the user asked for it to be cleaned up as the Java
project's docs had been: the notes kept the preferences, decisions and
facts a session works by, and their history moved here, as it was.

What sessions working on this project have learned that the code, the
README and the other docs do not record: the user's preferences, why
past choices were made, and where the work stands. Every session reads
this (CLAUDE.md imports it). When you learn something a later session
would need, add it here in the same PR as the work, and correct or remove
what has become wrong. Keep it short: facts, with the reason, not a diary.
The longer history is in `docs/how-this-project-was-built.md`, A/B
results are in `docs/cost-history.md`, and the refactorings and working
patterns sessions use, with their steps, in `docs/patterns.md`.

### Working with the user

- One item per branch and PR. The user merges, then reconnects the MCP so
  the next item uses the new server; wait for that before relying on a
  change to the tools.
- Ask before starting an A/B run unless the user asked for one: each costs
  a few dollars.
- Append friction log entries at the end of `docs/friction-log.md`,
  with replace_text's `append` (since 2026-10-28; before, by heredoc).
  Inserting them before an older entry put every PR's entry at the
  same place, so parallel PRs conflicted there one after another
  (2026-10-22, #263-#267).
- When the user says "do X, then Y", do them in order, each in its own PR.
- **"What's next?"** (2026-10-31, the user): the `whats-next` skill
  offers the next items and keeps a record of each in
  `docs/sessions/<branch>.md`, so the user can assess whether a session
  used the tools or worked around them. The user chose both an objective
  log (the hook `cmd/session-log`, every call, read by `refactor-tokens
  -log`) and the model's own notes (why a hand edit, what followed a
  refusal, what a read answered), and a file per item in docs/ over a
  summary in the friction log. A hook, because a habit named in the
  instructions did not change (the read guard's story, below).
  The user then asked for the transcript's tokens in the record, and to
  know whether any write was made because writing lines was easier than
  the edits or refactorings: the report lists large writes, and the
  record answers for each. The record `tokens-in-records` found
  that reason twice: a stub overwritten with a whole file (#527) and a
  function about to be written out again (dropped for five edits).
- **Look for a composition before logging a gap** (2026-10-29, the
  user). The 34 calls of `set` (Oct 25) were logged as "no tool
  replaces each call with a call chosen by its argument"; the user saw
  extract one call per field, fold it, inline `set`. This session had
  missed it too, with the descriptions in view: it took the target as
  one transformation and checked tools one at a time. So a gap needs
  the sequences tried (CLAUDE.md, the friction log's header). A probe
  (`docs/composition-probe.md`, $1.11): posed with the tools in view,
  Sonnet found wrappers + fold in 8 of 9 answers, cold, and stopped
  before inlining `set` only because inline_function's description
  listed "an early return" (it inlines one at a caller's last
  statement; the description now says so). Haiku wrote the recorders
  out and moved calls by text, taking the recipe 1 in 3 when named.
- **Reason from the highest abstraction that captures the problem**
  (2026-10-22, the user). Before planning code, name the change at
  that level: "this field holds one of three modes, switched on in
  two places", "this parameter is always the same expression of
  another", not "add a `narrow` flag" or "replace `info` on line
  314". Choose the design and the operation there (a type per mode,
  the inverse of introduce_parameter), and only then descend to tools,
  names and lines. Asked after #283, where reasoning at the level of
  lines added a second flag to what was already a type code.
- **TDD's order: refactor only while green** (2026-10-31, the user:
  "not the only session to make this mistake"). Sessions read TDD and
  "make the change easy" as test, refactor, change: in 47.4 the test
  was written, then the code refactored while it did not compile (the
  test deleted to get past the refusal). The order is: refactor to make
  the change easy while green, before the test; red; the least code
  that passes, a copy included; then refactor (fold the copy). A
  refactoring refused because a new test does not compile is right.
  CLAUDE.md, the README, `examples/CLAUDE.md`, the server's
  instructions (both languages, published text: A/B baselines before
  this saw the old wording) and `docs/patterns.md` now say so.
  When red reveals a missing step (the least code needs a refactoring
  first), roll back to green, take the step there, and redo what
  followed: the user prefers that to refactoring while red and does
  not mind the extra turns (2026-10-31; #518 did it with undo).
- **Worktrees** (2026-11-02, the user asked; roadmap 50): the server
  and the hooks belong to the session's starting checkout, and a
  subagent with `isolation: "worktree"` shares them; its MCP roots are
  the parent's, but a hook's `cwd` is its own. `cmd/worktree-paths`
  (PreToolUse on `mcp__gorefactor__.*`) makes relative paths absolute
  in the caller's worktree and refuses paths into the main checkout from
  one: a subagent never needs the main checkout (the user: isolation is
  the point). `updatedInput` without a permission decision worked for an
  MCP tool, unprompted. Without the hook, the server asks for roots on
  each call (roots are deprecated by SEP-2577 as of 2026-07-28; the
  user chose to build on them while they last), which covers a session
  that entered a worktree, not a subagent. To try a new MCP server in a session: `claude
  mcp add` (local scope), then restart; `/mcp` reconnects one already
  loaded.
- **Edits as a paste** (2026-10-31, the user: "the edit tools have
  been used too much as an impediment"): sessions write the Go
  themselves and have replace_text paste it, counting that as through
  the tools. #524, #528, #529 and #532 each made their change with
  replace_text on Go where add_case, add_element, replace_expression
  or add_parameter fit; #532's record first called it "through
  gorefactor". replace_text is for literals, comments and other files;
  on code, find the structured edit, and note `hand:` when there is
  none. Roadmap 49.10-49.11 measure and check it: `refactor-tokens`
  reports how many edit steps were text edits of Go. This repo's
  server runs with `-no-code-by-text` (`.mcp.json`, to try): a
  replace_text that changes Go code, not a string or comment, is
  refused with the structured edits to use. If it costs more turns than
  it saves, say so and take it out of `.mcp.json`.
- Report results plainly, including when a change did not help (#119 was
  reverted in #120 after its rerun showed nothing).
- Stacked PRs: if one fails CI, fix the lowest branch and merge upward;
  do not force-push. (#41 failed on gofmt after an import added with sed.)
- Record findings, open issues and decisions in the repo, in the PR of
  the work: the roadmap (`docs/roadmap.md`) for anything to do later, these notes
  for context, the friction log and docs/ for the rest. Not in claude.ai
  Project docs or memory: the user works in Claude Code, whose sessions
  read only the repo (2026-10-20, after review follow-ups had reached
  only these notes and the friction log, not the roadmap).
- The README (2026-10-30, the user): it opens with the pitch for
  developers who know TDD and refactoring, five sentences the user
  chose, then Status. The roadmap is `docs/roadmap.md`, numbering
  unchanged. On 2026-10-31 the user asked for repetition cut: "How to
  work" and "Lessons from earlier sessions" (#310, an A/B arm's
  instructions) went, since the README reaches no session (CLAUDE.md
  does) and `examples/CLAUDE.md` is what a user copies; the derivations
  are in `docs/derivations.md`. What each tool checks is
  `docs/checks.md`. Keep the README to what a developer needs to judge
  and install the tools; instructions for Claude go in CLAUDE.md.
- Releases (2026-10-27, the user): CI makes them. To release, raise
  `mcpserver.Version` in a PR, with the notes in
  `docs/releases/v<version>.md`; once Test passes on main, the Release
  workflow tags the commit and creates the release. Sessions may not
  push tags or create releases themselves (HTTP 403).
- CI minutes (2026-10-27, the user): the account has 5,000 Actions
  minutes a month, so Test skips changes to docs alone. It ran on the
  newest Go only from 2026-10-27; the user put the minimum in go.mod
  (1.26.x) back on 2026-10-30. Keep CI runs few: batch small fixes into
  one PR where they belong together.
- CI's time (2026-10-28, the user asked why it had grown from 2 to
  9-12 minutes since ts-spike): every package that reaches the
  TypeScript compiler (300k lines) compiles it, and each of their 24
  test binaries links it. setup-go's cache is saved once per key (Go
  version and the hash of `cache-dependency-path`), by the first job to
  finish: the vulnerability job saved the modules alone (147 MB), so
  vet compiled the compiler every run (145 s). That job no longer
  caches, the test job's key includes test.yml (edit it to start a new
  cache), and `internal/ts/...` runs without -race.

### Decisions and their reasons

- **TypeScript in this repo, from one server** (2026-10-27, the
  user): asked how much would overlap, the user chose this repo and
  then one server whose tools route by file extension, over a
  separate repo or a second server. The language-neutral code
  (diffs, MCP server, batch, A/B harness, about 4,500 lines) is what
  A/B results keep tuning, so it should land once. To keep Go's cost
  and baselines unchanged, descriptions follow what the repo holds
  (Go only: today's text). The plan and its open decisions are
  `docs/ts-plan.md` (roadmap item 40); the user asked for the plan
  before any code.
  The spike (step 0) chose its own project when asked to "pick a
  mid-sized open-source TS repo": Hono, for being TypeScript only,
  strict, a solution of several tsconfigs, and about the 50k lines the
  plan's target named. Results in `docs/ts-spike.md`; `cmd/ts-spike`
  stays until step 3 replaces it. Measure timings again on the user's
  machine (more cores) before tuning the checker pool.
  Errors already in TypeScript code (2026-10-27, the user: "relax the
  rule for now; we might want to revisit"): Hono has 4 under TS 7, so
  every step was refused. A workspace may now tolerate errors
  (`refactor.ToleratesErrors`); TypeScript's does, Go's does not. There
  a step must add no error, rather than start from none. The cost: an
  error already in a file can hide what a step breaks there (an `any`
  where a type was lost). Revisit with evidence from real repos.
  Asked again at the end of item 41 (2026-10-28): keep accepting them
  for now; the user had no other repository in mind to try.

- **Manual edits as small as possible** (2026-10-21, the user's
  principle): not every operation will exist; use the ones there are,
  keep manual edits to the smallest size, avoid them when possible.
  Judge runs and gaps by the size of the manual edits, not only the
  derived share. The experiment of cost-history section 8: guidance in
  the user's moves changed how the model worked where a recipe fit,
  but not cost, on a module too small for rewriting by hand to cost
  much. On code with many uses (`notify-large`, cost-history section
  8) the guided arm cost 14% less and wrote 39% fewer lines by hand,
  the gap on fit and Batch, where a derivation replaces rewriting each
  use: the first measured support for the user's thesis. At six runs
  per task (cost-history section 8) it held but smaller: 4% less
  cost, 31% fewer lines by hand, 55% of steps writing nothing against
  11%; fit is the steady win. A new option the model is not led to
  (`pointer`) went unused: options surface through refusals and
  recipes, so a gap's fix needs one of them to be used.
  In `contacts-q-guided` two runs went textual because no one tool did
  the change (`change_field_type` cannot make two fields a map) and
  never loaded fold; a pointer to the recipe from that refusal is a
  candidate. The user wants to think before rerunning Contacts
  (2026-10-21).

- **Derivations from the user** (2026-10-20): asked how they would
  reach five shapes with operations, the user answered with moves such
  as "extract variables for what differs, move them to the top, extract
  the rest", "wrap in an if and move the statements in", "encapsulate,
  then change the representation", "delegate, redirect, inline",
  "expand, migrate, contract". `docs/derivations.md` has them, run with
  the tools, and the gaps. Pose problems rather than ask for examples:
  the user said they could not think of many off the top of their head.

- **Exported code is changed, with a note** (2026-10-21, the user):
  refactorings no longer refuse exported code because other modules
  might use it. As gopls does, they update this module and say the
  module's API changed, so the model can mark a breaking change. The
  alternatives were a per-call option (as `exported` was), a
  workspace setting that the module is closed, or inferring it only
  for main and internal packages. Checks that guard behavior inside
  the module (reflection, interfaces) stay. Roadmap item 22.

- **A nil dereference may move past calls that return** (2026-10-21,
  the user): folding near-duplicates into `book(&w.Charged, ...)`
  takes the address before a log line, so a nil `w` panics without
  logging. The user chose to allow it: it panics either way, only
  what is printed before differs. Still refused past calls that
  might not return (os.Exit, log.Fatal, a function value or
  interface method) and past other panics. Roadmap item 23, whose
  other parts let `fold` do the near-duplicates task in one call.
  The user is still undecided whether relaxing it is right on
  technical grounds (a crash that printed less, or a different
  panic, is still a different behavior), but it is plainly the
  better fit for the tooling: without it, the most common
  near-duplicates (methods on pointer receivers that log, then
  write a field) cannot be folded at all. Keep the relaxation
  narrow and easy to revert, and say where a refactoring relies on
  it. `fold` extends it (its own PR, to be dropped if the user
  decides against it): `&x` may also come before code or an
  argument that could panic another way, which billing's card copy
  needs; never before code that might not finish or return
  (`Summary.Stops`).

- **One analysis of what code does** (2026-10-25, the user): Stable,
  MayPanic and Effects' summaries merge into one, in six PRs (README,
  roadmap item 34). Facts about the standard library live in one
  table, `stdFuncs`, and verdicts derive from the facts; a function
  added there is known to every analysis. When the module's own
  functions are judged by their summaries (step 5), a call that may
  dereference can move with the call, as above; the user waived
  that question for now.
  Since step 5 a call of a module function counts by its body, so a
  golden that needs an effect must have one (`println`), not a
  function returning a constant; and verdicts change only with a
  reason `TestDifferential` accepts.

- **Moving an evaluation is one question** (2026-10-26, the user asked
  for the missing abstractions and then to build them): `analysis.Move`
  (roadmap item 35) judges whether an expression can be
  evaluated elsewhere or another number of times, from its summary and
  the summary of the code it passes. Refactorings state the move (the
  expression, `Past`, `Times`, `Dereferenced`) and word the refusal
  from the `Obstacle`; they do not restate the rules. When a check in a
  refactoring asks "can this run before/after/more often", express it
  as a Move; where a use sits on its path decides `Times` through
  `analysis.Reach`. All six steps are done: past a call of a module
  function is past its body, by its summary, so "past a function" is
  `Past: []ast.Node{call}` (roadmap item 35, step 6).
  Ownership is `analysis.Owner` (item 36): a summary's notion of its
  own memory is `body.own`, `ThisFunc` for a function and `ThisCode`
  for an expression judged out of context. `site.Site` (item 37) is
  the file a refactoring works in; a new refactoring embeds it and
  opens its file with `site.At` (`site.Declaring` for the file of a
  declaration). rename refuses a field name that a field promoted
  from an embedded struct already has; replace the uses as text
  instead, then delete the field.

- **Tool descriptions: an example per use, the rules in refusals**
  (2026-10-21): cutting `fold`'s description from 668 to about 260
  words lost its use on near-duplicates (2/4 runs to 0/4) when the
  worked example of that shape went; with one example per kind of
  match (about 300 words) it was used in 4/4. A fresh model decides
  whether a tool fits from its before/after examples; conditions and
  evaluation-order rules can live in the refusals, which say why a
  match was left. The user then asked for every description to be
  shortened and given an example or two (#256-#259; 8,700 words to
  about 5,200 over 38 tools, each example run with the CLI). Write new
  ones the same way: what it does with a before/after, the addressing
  hints the schema does not carry, and one line of what it refuses
  ("Refuses, saying why: ..."). Measured on freight-xl (cost-history
  9): about the same cost, half the refused calls, no tool lost.

- **Examples come from no eval task** (2026-10-22): the tool
  descriptions and `inspect`'s examples had grown out of the eval
  tasks' code (lending's `"book"`/`Book{}` and `Member.Address` to
  `Home`, notify's `e.Sent` and `Batch{Users, Message}`, `UserID`), so
  an A/B run's model could find a task's answer in a tool's example
  (cost-history 10: the control arm took the kind derivation the
  descriptions named). They now use one garden domain (`Plant`,
  `Species`, `Bed`, `Sprinkler`), each checked with the CLI, and
  `TestExamplesAvoidEvalTasks` fails when a name or string of a seeded
  eval module appears in what the server publishes. Runs before this
  saw the old examples. The arms' own instruction files are
  deliberate (`claude-md-learned` and `-derivations` cite notify's
  `Batch`).

- **Names and results say the intent** (2026-10-22, the user, on
  `add_interface_guard`, renamed `prepare_switch_for_types`): a
  tool's name, description and result say what the step is for, not
  how it does it (the guard is the mechanism; preparing a switch for
  its cases to move into types is the intent). The mechanism comes
  after, in the example.

- **A budget of tools** (2026-10-22, the user): the concern is that
  tools multiply until their context outweighs the gain over editing
  by hand. Measured: about 5% of a request today, 30-40 tokens a
  request per new tool; the risk that grows is choosing. The order for
  a new operation: a proof in the engine (an edit to code that cannot
  run is a refactoring), an option of an existing tool, a composite
  named by intent built from primitives that are not tools, and only
  then a new tool, which at the budget merges or replaces one. `TestToolBudget` holds
  the count (50: 45 tools today, raised from 44 by the user "for now"; move_method was the 45th, 2026-10-29)
  and the instructions' size; raise it only with the user. Roadmap item 25.

- **Renaming a field that fmt prints** (2026-10-29, the user chose
  the third of three: keep refusing, allow in tests only, or allow with
  a note): printed text is allowed to change, and a note lists the
  calls. What may find a field by name (encoding/json, text/template, a
  function not known) still refuses an exported field. Roadmap item 42.
  extract_struct and remove_dead_code then followed (the user asked
  them "up to par"): any printing is a note, since their changes show
  in `%v` too; remove_dead_code also refuses `reflect.DeepEqual`.
- **Descriptions drift behind refusals** (2026-10-29, the user asked
  whether they match): an audit found 11 Go tools whose "Refuses" line
  missed refusals the code makes or kept one it no longer makes
  (inline_function's goto and early return). When a PR adds or removes
  a refusal, change the line in the same PR; a definition must stay
  under `TestLoadedToolsStaySmall`'s 1,700 characters, so trim to fit.
- **A getter that caches its result reads, it does not write**
  (2026-10-29, the user chose this of three: keep refusing, recognise
  the shape, or allow with a note). `analysis.cacheOf` recognises
  `if r.f == nil { ... }; return r.f`: its writes to `r.f` and what it
  holds are its own, and the new values it makes do not count, since
  later calls return the first. Only that shape: a getter that also
  counts calls or writes anything else keeps its effect. The user also
  asked for the non-strict way (`docs/derivations.md` section 8): pass
  the method value, so the getter runs where it ran.
- **Early returns inline** (2026-10-29, the user's derivation): wrap the
  statements after an if that returns in its else, then give the
  function one result variable that each return assigns, and the body
  has one return at its end. inline_function does both itself
  (`exits`, `tailResults`, by the order of the tool budget: a proof
  in the engine first), assigning the call's variables directly, or a
  temporary of the result's type when a variable's type differs (a nil
  `*T` returned into an `error` must stay a typed nil). A return in a
  loop or switch sets a flag that guards the statements after it, and
  in a loop breaks out of the outermost one, labelled (`exits`); goto
  is still refused. The user then asked to expose the first step:
  remove_else's `wrap` (its inverse). Assigning the returns to one
  variable stays inside inline_function until a second use appears
  (extract_function of code that returns is the candidate). The
  user's other way, extract what encloses the call so nothing runs
  after it, now inlines with the returns kept (`returnsRightAfter`
  sees through ifs, switches and blocks); derivations section 9.
  The user's simpler form comes first (`hoistable`, both languages):
  when what follows the loop or switch is only `return E` and E can
  be evaluated before it (an analysis.Move past it, Maybe times), the
  variable starts as E and each return assigns and breaks, with no
  flag; the flag stays for statements after the loop, or an E the
  loop changes.
  TypeScript has the same (`tsapi/early.go`, with remove_else's
  `wrap`), loops and switches included, but a return converted in a
  switch also breaks out of it (a case falls through), the variable is
  declared `let v!: T` when a flag hides from the checker that every
  path assigns it, and a call at a module's top level is still refused
  (its variable would be the module's). Guards with a last return
  still inline as a conditional expression, which comes first.
- **TypeScript's rename and names found as strings** (2026-10-29,
  item 43.9): the same rule as Go's for fields that fmt prints. Code
  that finds properties by name (JSON.stringify, Object.keys and the
  like, for...in, `"name" in x`) refuses; what console prints is a note.
  A function not known that takes `unknown` is not checked, unlike Go's
  interface parameters: in TypeScript that would refuse every value a
  test's `equal(got: unknown, ...)` sees.
- **TypeScript's counterparts of Go-only shapes** (2026-10-29, item
  43.10, chosen in the session, not by the user): move_method moves a
  method down to a class that extends its class (TypeScript has no
  embedding; a subclass is what the outer struct is), every use reaching
  it through that class. A getter is summarized, and a cached one read
  as a read, only when static or private, as a method is: a public one
  could be overridden by a subclass the analysis cannot see. Fold's
  setter is `{ ...p, f: v }`, matching a literal of p's type that ends
  with f, since key order shows in JSON. Other solutions are routed as
  Go's other modules: a file in no project of the solution, whose
  nearest tsconfig.json is not one of its projects either. Revisit any
  of these if the user prefers another reading.
- **A string can carry an early exit** (2026-10-30, item 44, chosen in
  the session): extract_function treats a string last result as an
  error, "" for nil, because this repo's functions return refusals that
  way and all four hand rewrites of 43.10 hit the refusal. Numbers were
  left out on purpose (an early `return 1` is a value, not an exit).
- **Reads between occurrences** (2026-10-30, the user, item 45.4):
  extract_variable's all_occurrences judges what the expression reads
  against the code between its occurrences (`analysis.Move.Between`),
  as TypeScript does, so `b.depth*2 + b.depth` extracts; and
  inline_variable for several uses, since `TestGoldenRoundTrip` inlines
  what every applying extraction made. The code between is whole
  statements (`analysis.Spanning`), so a loop's other iterations count,
  and nothing when a use is in a closure. fold and changesig's
  evaluation still refuse any read, since they pass no code between;
  give them `Between` with that code to relax them. fold followed
  (the user, same day), judged over the code it replaces. The other
  two disagreements were then decided by the user: TypeScript takes
  Go's nil dereference (a nullish read may pass code that writes but
  does not throw otherwise or leave), Go takes TypeScript's leaving
  (an effect does not move past a return, break or continue). The
  rules then became one: `internal/reorder` (`Repeat`, `Conflict`),
  which both languages call (item 46.2); change a rule there, not in
  `analysis.Move.Check` or tsapi's `Check`.
- **A module function that calls its callback only during the call**
  (2026-10-30, the user, item 46.3): known from its body
  (`Effects.RunsOnlyDuring`), per parameter, recursion assumed to
  qualify while judged, interface methods never. TypeScript followed
  (2026-10-31, `effects.runsOnlyDuring` in tsapi), the same answers. A new
  caller of `AnalyzeFunc` passes its workspace's Effects, or nil for
  the table of known functions alone.
- **Schemas come from argument structs** (2026-10-30, item 45.1):
  `refactor.Schema[T]()`; a property is a field with a `desc` tag,
  optional with omitempty. Published schemas did not change, so the A/B
  baselines hold. The languages keep separate structs, since most of
  their descriptions differ; unifying them would change published text,
  so ask before doing it. To check a change publishes the same
  schemas, dump `golang.Language{}.Tools()` and
  `typescript.Language{}.Tools()` before and after.
- **Instructions by language** (2026-10-29, item 43.4): a project
  without Go gets `TypeScriptInstructions` (internal/mcpserver), whose
  example batch `TestTypeScriptInstructions` runs and whose tool list
  must name every tool published; Go's text is unchanged, so Go's A/B
  baselines hold, and names TypeScript only when both are there.
- **A parameter may take a package variable's place** (2026-10-25, the
  user): add_parameter's `replace_variable` lets the new parameter hide
  the package variable the body reads. Where the engine cannot prove a
  call passes the variable's value, the user chose to allow it and list
  those calls as edits in a note, over refusing them: the plans that
  wanted it pass `DefaultPolicy()` from other packages, which no proof
  reaches. Roadmap item 31.
- **TypeScript's gaps before its cost** (2026-10-28, the user): "not
  too worried about the cost with TypeScript at the moment", so A/B
  reruns and more TypeScript tasks wait; the gaps against Go are roadmap
  item 41, done in its order. The user then asked for fewer
  PRs for the rest (2026-10-28): items 6-8 in one, 9-10 in another.
- **TypeScript's type changes are edits** (2026-10-28, item 41.2):
  change_field_type and change_parameter_type only write the
  annotation, since types are erased and no value can change; but a
  refactoring in a batch with check "end" must still type-check on its
  own, so they are edits, as Go's change_type is, letting the fixes to
  the uses follow them in the batch.
- **One tool per operation, other names as aliases** (2026-10-27, the
  user): TypeScript's "add a property" is `add_field`, Go's name, by
  the plan's rule (same names where the operation is the same, so
  recipes and eval tasks transfer), not `add_property` as its table
  had it. The user then asked for an alias: `refactor.ToolName` maps
  other names to a tool (`add_property` to `add_field`, and
  `change_return_type`, the plan's name, to `add_result`) wherever the
  server resolves a name, in batch steps and the CLI, for either
  language. Aliases are not published tools, so they cost nothing
  against the budget; a direct MCP call can only use a published name.
  Argument names work the same way (`aliasArgs`): a word of the given
  name, `contentAliases`, and `nameAliases` (2026-10-31: `path` and
  `from` for file, `struct` for name, the refusals the read-guard A/B
  runs counted most: create_file's `path` 9 times in 16 runs), and a
  number or boolean sent as a string is converted. Add a synonym only
  when it can mean one argument and a run shows it refused; `query`
  (replace_text, once) could be old or a search, so it stays refused.
- **Options over new tools** (2026-10-21): the pointer alias (gap A)
  is `pointer` on `extract_variable`, not a tool of its own: the model
  already reaches for extract_variable, and its refusal for a field
  that is assigned to now names the option. More tools did not help a
  fresh model (CLAUDE.md lessons).

- **General operations, not task-shaped tools** (2026-10-21, the
  user): `fields_to_map` (#217, closed) did the Contacts task in one
  call, but fit one shape (fields into a `map[string]T`) and went
  around the derivation the experiment measures. A gap becomes an
  operation that composes with the others, for any type and target:
  K is now fold literals through a setter, then L removes the fields.

- **Experience for a fresh model** (2026-10-21): the user tries several
  approaches when refactoring and builds a library from them; they
  want a fresh model to get that benefit even at a short-term cost.
  Roadmap item 21, in the order the user accepted: lessons in
  refusals, `try`, a recipe library in the repo, exploration runs,
  search in the engine. The user then asked for the exploration
  experiment first (whether a model told to try several approaches can
  teach later ones): `docs/learning-transfer.md`. Its later tasks are
  on a second module of the same shapes, so lessons carry technique,
  not answers; keep set A and set B apart. What made explorers explore
  (2026-10-21): an instruction to make calls ("only a call counts as
  trying"), the user's limit of one distinct line by hand per change
  (a line repeated at many sites counts once; a change is one fact,
  not one edit call), and "try all five even when one passes"; asking
  them to consider ways did not, they reasoned the ways away. The
  learner runs: the model-taught lessons halved the lines by hand but
  not the cost; the user's moves were cheapest (cost-history 9).
  Once `fold` could do near-duplicates (#245-#251), every arm used it
  for `book`, even the baseline; the learned arm's long recipe for it
  then cost turns. A lesson that names a workaround goes stale when
  the tools improve: prefer moves that name general operations.
  On a larger module (`freight-xl`, cost-history 9) the user's moves
  cost 25% less than the baseline and wrote 43% fewer lines by hand
  (13% on billing): the first sign that the saving grows with the
  number of uses a change has. The A/B harness copies the module
  from the working tree for each run: stay on the branch that has
  it until the run ends (a checkout of main mid-run broke 8 runs).

- **Teaching to derive, and reading less** (2026-10-22, the user):
  "teach the model to refactor better" means mostly deriving on shapes
  it was not taught, not applying given moves. The user believes,
  without data yet, that thinking algebraically lets the model read
  much less, perhaps planning from a call tree's names, and that this
  goes hand in hand with deriving. The plan, to run in a later session:
  `docs/reading-and-deriving.md`, roadmap item 24. Step 1 (freight-xl
  transcripts): both arms read alike and little, and about a quarter
  of input is the output of "where is X used" searches, which a
  derivation does not need; the model searches by habit before it
  plans. Step 2 (the call-tree probe): from names and signatures
  alone the model found where the change goes and planned the
  reference derivations; what failed was the tools' interface
  (rename's `function`, argument names) and a fold bug, not the lack
  of bodies. Step 3 (`lending-xl`, shapes no move names; three arms,
  60 runs): the algebraic arm derived a little more where an
  operation fits, at the same cost; where none fits no arm derived,
  and reading was alike in every arm. The method transfers only
  through operations that exist; the runs name the missing ones
  (rename of an embedded field, a switch on a kind into methods).
  The first is done: rename makes an embedded field a named one.
  For the second the user chose two general operations over one
  shaped to the task: a type code into types, then a type switch into
  methods (`type_switch_to_method`, built first). Then the user
  proposed a better derivation, one kind at a time with every step
  preserving behavior: widen the field to `any`, add the interface and
  a guard that calls it, then per kind make the type from the case,
  replace the constant where it flows into the field, and delete the
  dead case; narrow the field at the end (roadmap item 24).
  Asked for alternatives to a tool for one kind's migration, the
  existing refactorings turned out to do the case bodies; what is left
  (sending the values through the type, deleting the case) is an
  argued edit either way, so it becomes a recipe with `inspect`
  `uses` for the argument, and a tool only if a fresh model fails it.
  Narrowing the field and deleting a case were then built (#283,
  #284), and the whole task ran end to end through the tools (74
  derived steps, about 30 lines by hand). The recipe arm for a fresh
  model is `eval-tasks/claude-md-kind-recipe`; its example is shapes,
  not lending, so the task stays out of the instructions. Run
  (cost-history 10): the control arm without the recipe found the
  same tools (widen, prepare, narrow) in 3 of 4 runs, and no run took
  the recipe's extraction step (one-line bodies are cheaper to
  write). So no move_case_to_type: the tools' descriptions carry
  the path. Derived share doubled and lines by hand fell a third
  against set C, but cost rose a quarter with the turns. After the
  two fixes the runs showed (#288, #289), a rerun: the parse refusals
  went, but a refusal that names the right steps did not change what
  the model did next; it fixed the text it had broken, then batched.
  A note helps less than a tool that makes the wrong move work.
  Bodies read and not used (2026-10-23, `level-long-5`,
  `eval-tasks/learning/bodies.py`): about three quarters of the body
  text read reached no edit, most of it with whole files, but all Go
  reads were 3.7% of input and the unused bodies under 1%: at these
  sizes reading is not where the tokens go.
  The user then asked for a server option that refuses reads of
  lines without a name, default off, for experiments: `-read-by-name`
  (roadmap item 24). A range is the only view that shows source
  without naming it (a file alone is its skeleton), so that is what
  it refuses.
  The `names` arm of `cmd/refactor-ab` runs it: the mcp arm with the
  server started with `-read-by-name` and no other way to read Go (no
  Read, Grep, Glob, cat, head, tail or grep);
  `eval-tasks/claude-md-names` has `mcp.md` and `names.md`, the same
  but for how to read.

- **How this project's own sessions work** (2026-10-22, the user
  asked; `refactor-tokens` on one multi-day session, 2,796 requests):
  it read by `sed`/`cat`/`head` 702 times and `grep` 568, against 69
  `inspect` calls, though CLAUDE.md and the user ask for `inspect`:
  the habit of the fresh models (search, then a range, then edit).
  55 of 1,576 steps were refactorings; it derived only where the
  derivation was the task (lending-xl, 74 steps), not on the way to
  its own changes. It wrote a type code while building the tool that
  removes one: `fieldChange` got a second flag, `narrow` next to
  `widen` (#283), and did not see it until asked to look. Batches
  sent through the CLI with JSON written by Python count as Bash in
  `refactor-tokens`. So: read with `inspect` (`names`, `targets`,
  `uses`), not `grep -n` for anchors (a name addresses a
  declaration); before adding a flag or a case, ask whether the
  change is made easy first.
  Measured again on the session of 2026-10-18 to 10-26 (one
  transcript, tool calls classified by day): of about 4,150 writes to
  the repo, 92% went through gorefactor (3,822 steps), 95% leaving out
  the friction log's entries, appended with a heredoc by habit (126,
  one a PR); Edit and Write 47, none after Oct 22; Bash writes about
  150 (Python rewriting JSON or a merge conflict, `sed -i`, `cp` of a
  golden). The share rose from 78% on Oct 18 to 97-99% of writes other
  than the log's on Oct 25-26. But what went through the tools was
  mostly text: replace_text 1,659 steps (1,009 on Go source), create_file
  1,214 (mostly goldens), add_declaration 292, replace_statements 227;
  refactorings 198 (5%). And reads did not move: Go source read with
  `sed -n`, `cat` or `grep` 492 times against 82 `inspect` calls,
  though since Oct 24 a third of Go reads were `inspect` (34 to 80).
  On 2026-10-30 (#508, items 46.4-46.5, after promising the user to
  read by inspect and derive): every write went through the tools (41
  steps, 14 of them refactorings: rename, move_declaration, inline),
  but no read did: 7 `sed -n`, 11 grep, 1 Read, 0 `inspect`. The
  habit holds even when named; a reading rule needs a check, not a
  resolution. Turns lost to guessed argument names (3 of 13 batches)
  are what loading the schemas first avoids. The transcript keeps only
  what follows the last compaction, so measure before one.
  So the user asked for the check: `cmd/read-guard`, a PreToolUse hook
  in `.claude/settings.json` (2026-10-30), refuses Read of a `.go` file
  and sed, cat, head, tail, less, more, nl, awk or grep naming one,
  outside testdata/. It denies with a JSON decision and exit 0, since
  `go run` turns an exit status of 2 into 1, which Claude Code takes
  for the hook failing and lets the call through. grep over
  directories (a search, not a read) and git diff stay allowed. It
  applies to sessions started in the checkout, from the next one.
  Measured on a fresh model (cost-history 21, the `guarded` arm of
  refactor-ab): no Go file read whole, but reading moved to more and
  smaller inspect and Grep calls: 20% more requests and 9% more cost
  on the feature tasks from a cloud session, the same cost and 4% fewer
  requests on the user's workstation. The workstation's pass rates
  there were void: its Go's `$GOROOT/src` is a symlink, which the
  lookup of standard-library packages did not follow, so every golden
  that adds such an import failed (fixed 2026-10-31, item 47.4; it also
  failed `go test` on main there). On a machine where an import of
  `strings` is refused, suspect the GOROOT layout first.
  It is not a saving for a fresh model; its case is this repo's own
  sessions: there it ended reading Go by shell (20 reads per 100
  requests before, none after) for 4% of turns refused (cost-history
  22, 2026-10-31), so it stays. Two of its refusals were wrong (a fixture
  read after `cd` into testdata/), and its advice named a CLI the A/B
  arms lack; both fixed 2026-10-31 (roadmap 47.1-47.2: paths resolved
  from the hook's `cwd`, the `cd`s and `NAME=value`s before them; the
  CLI named only when on the PATH).
  On 2026-11-05, prompted by java-refactor-mcp's port of this guard, it
  was relaxed to allow a grep whose flags print no text of the file
  (`-c`, `-l`, `-L`, `-q`; #559), and that relaxation was reverted the
  same day: the user asked whether `inspect` should cover the search
  instead, and chose to give it `pattern` (the lines that match a
  regular expression, in a file or the module, each file's count first)
  and point the guard's advice at it. The rule stays as it was: Go is
  read only through `inspect`, and a search that needs a tool gets one
  there. The full sequence is in java-refactor-mcp's
  `docs/how-this-project-was-built.md`.

- **Would a fresh model recognize it?** (2026-10-22, a probe, $0.23):
  three Sonnet sessions, `claude -p` in a copy of the repo at the
  commit before #292 with no CLAUDE.md, docs, README, `.claude`, MCP
  or eval tasks, read-only tools, asked "What refactorings, if any,
  would you make to `fieldtype.go`? Your top three." All three put
  first splitting `use` (the longest function): size, not design.
  Two named the `widen`/`narrow` flags inside that item, as "a single
  `mode` enum" (one: "or a strategy per mode"), since they cannot both
  be true; one did not name them. None proposed types with the
  behavior moved onto them, the derivation #292 did. So the type code
  is seen, but as a representation to tidy, below length and
  duplication; seeing it as behavior that belongs on types is what
  the move adds. Answers and costs: the scratchpad's `probe/`, not
  kept.
  Two more framings, the same isolation ($1.87 in all):
  - "What about this design would make the next change hard?" (3
    valid runs): all three put the two flags first and proposed a
    strategy interface with one implementation per mode, the design
    of #292. Asked about design, the model sees the types.
  - A feature, narrowing an interface field to a concrete type (3
    runs, each passing its tests, $0.38-0.53): none made the change
    easy first. Two added a third flag, both named `concrete bool //
    narrow, and new is not an interface`, checked in 4-5 places; one
    hid the mode in inline `!types.IsInterface(c.new)` checks. The
    same move as #283.
  So the gap is recognition while making a change, not knowledge of
  the move: the model, like this project's sessions, extends what is
  there. A prompt or step that asks the design question before a
  feature is the candidate to test.
  Tested (3 runs, $0.33-0.49, each passing): the same feature prompt
  ending "Before you plan this change, ask what about the current
  design would make it hard. If something would, first refactor so
  that the change becomes easy, keeping the tests passing, and only
  then make the change." All three refactored first, with tests green
  between, where none had without the question, but what they saw was
  the narrowing condition inline in `Plan`: each extracted predicates
  (`narrows(old, new)`, and `fits` or `mayEqual` for the read check)
  and kept both flags. None saw the flags as a mode; the third mode
  went in as `!types.IsInterface(c.new)` checks in 2-4 places. So the
  question makes the model refactor first, at the scale of a function
  (extract what the change touches), not of the design: asked "what
  would make the next change hard?" about the file alone it names the
  type code; asked it on the way to a feature, it names the line the
  feature goes through. Not tried: naming the level ("which concept
  does this code represent with flags or conditions?"), or asking the
  design question in a step of its own before the feature is given.
  Isolating a probe: start `claude -p` inside the copy, at a path
  that does not name the checkout. Two design runs started from the
  copy's parent globbed for the file, and the scratchpad's path
  (`-home-...-go-refactor-mcp`) led them to the real checkout, so
  they reviewed #292's code instead; they were rerun from
  `/tmp/probe-*`. Read as reviews of #292, they found its leaks: `use`
  still asks `types.IsInterface(c.new)` itself, each mode re-parses a
  use's parent node, and the mode is chosen from
  `checkConversion`'s error.
  Naming the level (2026-10-23, A/B, `docs/naming-the-level.md`,
  cost-history 11): an instruction to name the concept a change
  touches and every place that knows it made the model state the
  concept and refactor where the generic question did not (a new
  customer kind), but it gave each rule a home, not each kind: the
  axis the code is already cut along. No false refactoring on an
  additive task; on lazygit the instruction was skipped. Next to
  try: name the axis (the new instance should be one new place), and
  make the concept a step of its own.
  Both tried (same day, $3.06): naming the axis in CLAUDE.md did
  less (an `else if` passed as "one new case"). The same words as a
  session of its own (told the next change and not to make it)
  reached the design: both runs made a table of rules per kind, and
  the next session added the partner as one entry. No false
  refactoring; lazygit was again judged "one rule, already easy".
  So make-the-change-easy works for a fresh model when it is the
  whole task of a session, not a clause of the instructions.
  At 5 runs, and on two shapes chosen after (a lending kind in five
  switches, a freight route with eight helpers per route): the
  preparing session made the reference design in 15 of 15 runs, the
  control in 0 of 15, none refactored where nothing was needed, and
  the feature session then changed one place. The designs were made
  with edits and the tests, not refactorings: the tools lack "switches
  on a kind into a table" and "functions differing in a field into one
  taking a selector". What comes next: roadmap item 26.
  Over a second change (student, 8 runs): after the step it was one
  table entry, 11% cheaper; without it, a branch and a shared tax
  condition. The preparing session costs twelve times that saving, so
  on a small module the step buys design, not tokens. The same on
  lending (map) and freight (Sea): after the step one row or accessor,
  without it a case per switch or eight copies, at the same cost; a
  fresh model copies the nearest instance as cheaply as it adds a
  row. The user then asked for longer chains per project.
  Five changes per project (`level-long-5`): no pay back either; the
  feature sessions cost the same with or without the step. Lending's
  table held for every later change; pricing's was bypassed by a new
  rule in 4 of 5 runs; freight's control extracted a helper on its
  own once a change cut across the copies. The harness twice lost
  sessions to `claude` missing from PATH (the CLI updating mid-run):
  leave such chains out, since later stages fail the lost one's tests.

- **Loops and containers** (2026-10-20): the user asked for
  refactorings that convert between kinds of loop, and edits that make
  a block the body of a loop, an `if` and the like. Roadmap item 20.
- **The model reasons about intent, the engine about types**
  (2026-10-20). The user's concern with type-check refusals for the
  model's own code: the project is to take type-safe code off the
  model, so it reasons at a higher level. Roadmap item 18 and
  `docs/type-errors.md` rank what to take over.
- **A parameter's type changes through an alias** (2026-10-20): the
  user's way by hand is to declare an alias of the old type, write the
  parameter with it, then change the alias and fix what follows. So
  change_parameter_type re-spells to an identical type (#193), and
  `change_type` (#194), which retargets the alias, is an **edit**, so
  that a batch with check "end" checks it together with the fixes its
  result lists (the user's decision; a refactoring there would have to
  compile on its own).

- **Stubs are declared lazily, and batches get checkpoints**
  (2026-10-20, the user's decision). In a batch checked at its end,
  stubs and imports for earlier edits are added just before a
  refactoring (#197), not only after the last step: there was no
  recorded reason for the end only, and it made a refactoring after a
  test written first fail. Checkpoints (#198): a `check` step
  type-checks, and may run tests; a later refusal keeps the steps
  through the last passing one, as one change. The user left open
  whether a failure should keep what passed or stop; keeping is what
  was built, since that is what a checkpoint is for.

- **Every step must type-check** (2026-10-17). The user rejected letting
  edits leave type errors. Test-first still works: stubs for what a new
  test calls are declared automatically, and an edit may start from
  broken code if it fixes some errors and adds none. Since 2026-10-20 a
  refactoring may too, when every error is in a test file (a test
  written against what the refactoring makes); the user agreed. That
  is for a refactoring before the test or after it passes, not one
  between red and green (2026-10-31, below).
- **Claude via MCP is the product**, not the Ollama loop or standalone
  evaluation (2026-10-17): the user said those were not what they need.
  Revisited (2026-10-25): after cost-history 14, the user asked
  whether a loop of our own should make the change. It stands; a
  plan-and-execute loop is to be tried as an experiment
  (`docs/plan-and-execute.md`, roadmap item 29), and the user decides
  on its result.
- **Choose the next tool by evidence:** the friction log for hand edits,
  A/B transcripts for a fresh model's refusals and wasted turns.
- **A reload compiles none of the module** (2026-10-27, the user asked
  to fix the tools' slowness): packages.Load with types has go list
  compile every package to export data, the module's own too, which
  the workspace then type-checks from source anyway. Once the
  TypeScript compiler became a dependency, a new file in `tsapi`
  recompiled its 18 dependents: 2 min 10 s, and a 13-step batch 546 s.
  `loadPackages` (internal/workspace/load.go) asks go list for metadata
  only, loads the standard library's and other modules' types from
  export data (with NeedExportFile: with NeedDeps alone go/packages
  checks them from source), and type-checks the module's packages here
  (`check`, shared with `Metadata.Load`). The same batch took 20.7 s, a
  reload about 2 s; `TestLoadMatchesTypedLoad` compares it with
  go/packages' load. Type errors are no longer loader errors, so edits
  of a package with errors stay incremental. The user asked whether
  splitting into a server and commands would help: the MCP server
  already is one and paid the same reloads; a CLI that talks to a
  running server is still an option, mainly for TypeScript's warm state.
- **Query tool deferred** (2026-10-18): about 55% of greps were symbol
  lookups, but grep was about 5% of an arm's input tokens. Revisit only
  with new evidence, and check gopls's MCP server first.

- **Fowler's catalog and IntelliJ** (2026-11-04, the user asked for
  the comparison): `docs/catalog.md`, entry by entry. Run a
  composition before calling an entry missing: Encapsulate Variable and
  Record and Replace Function with Command first looked missing and are
  folds (derivations 14-16). What is left is roadmap 51, in the user's
  order: `split` (variable, loop and phase in one tool), Extract
  Interface for Go, Inline Struct (discuss first), query/modifier and
  flag arguments as options, then Go and TypeScript at parity.

- **Go and TypeScript at parity** (2026-11-04, the user, roadmap 52):
  one tool per PR, Go first where Go lacks it. Separate Query from
  Modifier is shelved (51.4): the user found the example copies too
  much code and drives toward no design. An expander that folds must
  export the struct's fields when any call is in another package, an
  external test package (`x_test`) included; the stress run, not a
  golden, found that.

- **Run the stress tests with the modules' tests** (`STRESS_TESTS=1`)
  when a refactoring changes what it writes (2026-11-04): that run found
  two silent behavior changes in inline_function on main, a return in a
  block that declares the call's variable's name, and a call assigned
  with `+=`, which no golden had. jsonschema-go, uritemplate and x/tools
  from the module cache, copied to a scratch directory, are enough; in
  x/tools, `go/analysis/passes/modernize` fails without a Go toolchain
  binary, whatever the change.

### A/B findings to remember

- TypeScript tasks (2026-10-28, cost-history 20): a seeded module is a
  tsconfig with `"types": []` (no node_modules), `.ts` imports
  (`allowImportingTsExtensions`, `erasableSyntaxOnly`) so Node 22 runs
  it from source, and tests that throw (`src/testing.ts`'s `equal`),
  run by `node --test`; logging goes to a module's `logged` array,
  since `console` needs types the config leaves out. The task's check
  is `tsc -p . && node --test`; the quality columns measure TypeScript
  too since item 41.10 (they were empty in cost-history 20).
  Validate each hidden test: it fails on the module and passes after
  a reference change (a scratch script did this; not kept).
- `examples/CLAUDE.md` is the instruction set that did best, for users
  to copy (2026-10-28, the user asked): the user's moves cut to what
  measurably helped. When a run finds better instructions, update it.
- Without a CLAUDE.md asking for it, a fresh model never used gorefactor.
  With one, it edits through it, but for additive features finds "nothing
  to refactor first"; tasks need the design in the way to see refactoring.
- On the small seeded module, refactoring first did not make a second
  change cheaper; plain editing stayed cheapest. Larger code or more
  changes are untested.
- MCP-only editing costs about 1.5x editing by hand; refactorings through
  the MCP cost about the same as by hand.
- In this project's own sessions, input tokens per edit step fell from
  1.2-1.9M (Oct 17, 10-20% through the tools) to 170-190k (Oct 19, ~100%
  through the tools, with batches). A fresh model does not get that from
  the tools alone; it comes from how they are used (CLAUDE.md, "Using
  gorefactor well").
- Input tokens are about requests × 39K in the feature runs, whatever
  the reads; count requests and what each turn was for.
- What the tools add to every request (cost-history 13): about 1.8K
  tokens (instructions 1.46K, tool names 0.37K), about 8% of a hybrid
  session; a loaded schema 500-800 more. The mcp arm's gap over manual
  is turns (twice the requests), not that context.
- Haiku and Sonnet, with and without the tools, on freight-xl
  (cost-history 14): all 64 runs passed; with the tools both cost more
  than by hand (Sonnet 67%, Haiku 41%), and Haiku by hand cost an eighth
  of Sonnet by hand. The tools neither saved cost nor enabled the
  cheaper model on tasks every arm gets right. Section 9's 25% was tools
  against tools, not against by hand. Next evidence would come from
  tasks a model gets wrong by hand.
- Tasks built so that the obvious hand edit silently changes behavior
  (cost-history 15, `eval-tasks/traps-warehouse.json`: argument order,
  short-circuit and double evaluation when inlining, a rename captured
  by a parameter, one near-duplicate that differs): 64 of 64 runs passed,
  Haiku and Sonnet by hand included. On a small module with "must not
  change" in the prompt, the model reads the trap and writes a test or
  a variable for it; the tools cost about 20% more. Untested: the hazard
  as one site among many, or a prompt that does not ask to keep behavior.
- Haiku plans derivations about as well as Sonnet (cost-history 16, the
  call-tree probe with `-model haiku -schemas -plan`): from names and
  signatures alone it named the reference's shape on most tasks, the
  user's widen-then-narrow kind derivation in 4 of 4, at $0.0075 an
  answer against Sonnet's $0.072. It missed fold for near-duplicates
  (dispatch, 0 of 4). Batches fail mostly on content written for bodies
  the view does not show, so a planner needs bodies where it writes code.
  With the whole module and the test files' literals, 28 of 29 parsed
  answers on those tasks passed; with bodies in view both models wrote
  more by hand (Sonnet dropped fold on dispatch).
- The plan-then-execute loop (cost-history 17, `loop.py -no-read`): Haiku
  12/16 on freight-xl at $0.0126 a task in 1.2 requests, the same cost as
  Haiku by hand (16/16, 9.6 requests); Sonnet 14/16 at 1.8 times Sonnet
  by hand. Letting the plan read bodies doubled Haiku's cost and turned
  folds into rewrites. The cost is the prompt (31K tokens, mostly input
  schemas). Open: a shorter tools reference, asking "done?" when tests
  pass, and a batch's stubs re-declaring fields an earlier step removed.
  All three done (cost-history 18): Haiku's loop 11/16 at $0.0111 (by
  hand $0.0124), Sonnet's 14/16 at $0.107 (by hand $0.0973). A resumed
  `claude -p` session reports its running total cost, not the request's:
  take the last total, do not sum them (section 17's costs were summed).
  With remove_dead_code naming every use left (cost-history 19), Sonnet
  passed routes 3/4. On lending-xl the loop passed Haiku 11/20 ($0.012)
  and Sonnet 15/20 ($0.146, against $0.176 and 20/20 for Sonnet in a
  session with the tools); kinds failed every attempt on
  prepare_switch_for_types and narrowing refusals. With both fixed
  (#338, #339) kinds still failed 0/8: the refusals fired, and the
  attempts stopped on case bodies the no-read plan cannot see (Haiku
  said so and sent no batch). Kinds needs reads: with them 8/8 (Haiku
  $0.020, Sonnet $0.197, a session $0.25), rewritten with edits, no
  prepare_switch_for_types. Those reads came once, before the first
  batch, functions only, without line numbers (not in the "done?"
  turn, as first recorded). With numbered reads of any declaration in
  any turn: 7/8, same cost, same edits, every read at the start. The
  read's form was not the cause; with bodies in view the models write
  the code out. Only a name-addressed step that moves a case into a
  type would remove the reason to read. The user chose an address
  over a tool: `case` with `function` on extract_function and
  replace_statements (roadmap item 32); splitting a case of several
  values on request is for later. Run without reads: 0/8. Only one
  attempt used it (and ran out of stages); Haiku stuck on the
  prompt's `Label(title string)`, which prepare refuses, and Sonnet
  asked for bodies it did not need. A line in the plan prompt (steps
  addressed by name carry code you cannot see) changed that: 5 of 8
  plans then derived by case, but each batch (40-69 steps) met four
  or five refusals of detail, more than two repairs clear: 0/8.
  With those fixed, still 0/8: every attempt stopped at Renewable's
  `atlas`, which has no case. A batch's refusal gives lines of the
  module at the refused step; a repair resends the batch from the
  start, so those lines point at other code. With both fixed (#356,
  #358): still 0/8, but the cases' derivation goes through; what fails
  is Import's edit, which needs its body, and the stages run out.
  Reads make the model default to edits (the user, 2026-10-26), so
  `-read-after` opens them only once a batch has applied: derive
  first, read for the edit that remains. Run: 7/8, but every first
  batch only prepared, and every attempt then read the five switches
  and wrote the methods out: a plan that can read later defers the
  move to after the read. With reads unannounced until they open,
  8/8, and 2 of 8 (Sonnet) derived first and then read only Import
  and a few uses; the six that only prepared still read the switches
  and wrote them out. Worded as "every line is existing code in a new
  place or new code; the first batch places the existing code" (the
  user, replacing "make it easy"): 6/8, 3 of 8 derived first.
  After #367, #371 and #372: with reads after the first batch 7/8
  (2 Sonnet derived, 41-57 lines); without reads 2/8, the first
  passes without a read (Sonnet, Import found through the narrowing
  refusal). Haiku derives but does not finish. After `values`
  (#374), the signature note (#375), split by default (#376) and
  refusals saying why (#378, #381), with four repairs: 6/8 without
  reads (Sonnet 4/4 $0.347, Haiku 2/4 $0.031), every pass derived.
  Import's edit needs its body, and a failing test brings it: a
  repair sends the source of what a failure names. What the runs
  lacked was a repair to spend on it. Repairs that patch the batch
  (`-patch`) cut Sonnet's cost a quarter, to $0.261. The manual arm
  in the same loop (`-arm manual`: reads, then text edits) passed
  kinds 8/8 at $0.0146 (Haiku) and $0.186 (Sonnet), cheaper than
  deriving, mostly by not sending the tools reference (12K tokens);
  it wrote twice the lines. On freight-xl the refactor arm won:
  Sonnet 33% cheaper, Haiku 23% (14/16 against 16/16), a fifth to a
  third of the lines. Writing wins on local changes (dispatch, tail,
  kinds), deriving where one step carries a change to many uses
  (routes, run); the tools reference is the derivation's fixed cost.
  An index of the tools (a line each, full entries on request) halved
  the first request but lost the derivations: plans asked for the
  tools the task's words name, not fold. With the five tools that
  carry a change given in full beside it (`-full-tools`, 29% of the
  reference), freight-xl came back to 16/16 for Sonnet at 23% below
  the reference, Haiku 13/16 at 19% below; kinds cost 15% more, in
  asks for the other tools its derivation uses. A plan chooses a
  derivation from the examples it sees. With prepare_switch_for_types
  and function_to_method in full too (seven), kinds cost 20% below the
  reference (Sonnet 3/4, Haiku 2/4), within 11% of writing it by hand.
- Where a session's cost goes (cost-history 12,
  `eval-tasks/learning/cost.py`): cache writes 69%, output 18%, cache
  reads 13%, though cache reads are 88% of input tokens. About 38% of
  cost is the first request writing ~9.4K tokens of context fresh in
  every session, so arms differ only in the rest; count savings
  against that, not the total.
- How the analyses were done: group a transcript's tool calls by
  assistant message id to count turns; weight each result's size by the
  number of turns after it to estimate what it cost in context.
  `refactor-tokens` now does this (turns, ToolSearch-only turns, refused
  turns by kind, reads by kind); the `ab-analyze` skill says how to use
  it on a run and a baseline. Pairing "Q:" lines with reads was a
  one-off and is not in it.
- Harness details: the MCP binary is built into the output directory at
  the start, so checking out another branch during a run is safe. The mcp
  arm cannot use Edit or Write, and its Bash may not change files: in
  `claude -p`, an allow rule such as `Bash(cat:*)` does not cover a
  redirect that writes a file (checked 2026-10-20). A run cut off before
  its result (a timeout) is marked `unfinished` and left out of the
  means of cost, tokens and turns (#164); before that it counted as 0.
  Likewise a run whose module has a Go file that does not parse gets
  `quality.unmeasured` (the parse error) and is left out of the quality
  table's function columns; before, its numbers came out as zero and
  minus the module's whole complexity (lazygit in
  `tp-mcp-refactor-first`: 0 changed functions, Δ complexity -10699).

### Where the work stands (2026-10-20)

- #121 (inspect without bodies, `targets`) was measured in
  `ab-features-skeleton`: more `inspect`, fewer whole-file Reads, but the
  same read cost, since the model read bodies with wide `sed -n` ranges
  in Bash. With a reading paragraph in the mcp arm's CLAUDE.md
  (`eval-tasks/claude-md-refactor-first-inspect`, run
  `ab-features-read-inspect`), it read through `inspect` but just as
  much. Input tokens track the number of requests (~39K each), so the
  next lever is turns: the mcp arm's denied Write and Bash-write
  attempts and ToolSearch calls (together about a fifth of its turns).
- ToolSearch in `ab-features-read-inspect`: 9 of 141 turns loaded only
  a schema (6.5% of input), mostly `batch`, `create_file` and
  `replace_text` at the edit phase. A turn-0 preload barely pays (best:
  8 tools, 7 turns saved, under 1% of tokens), since a loaded schema is
  resent every request. #128 asks for schemas to be loaded alongside
  another call and shortens the most-loaded definitions (8 tools: 15.7K
  to 12.5K characters). Measured in `ab-features-toolsearch`:
  ToolSearch-only turns 9 → 5, totals unchanged (5.54M, 8/8). Next by
  size: the denied Write and Bash-write turns (32 in that run).
- Denied turns, by what they tried (50 of 385 turns in the last three
  feature runs): fixtures 20, one `sed -i` over several files 14, reads
  the mcp arm's Bash allowlist cannot match 11 (a harness limit: in
  `-p` mode a command with loops, variables or brace expansion is
  refused, where a user would get a prompt). #133 names fixtures in
  the instructions under create_file; #134 lets replace_text take
  `files`. Measured in `ab-features-denied`: no Write attempts (17
  before), `files` used in both symbol-addressing runs, refused turns
  17 → 14 of 120; the model still tries Bash once before the tool.
- Line ranges in that run: of 30 inspect ranges, 18 spanned several
  functions (80% of range lines), 8 were exactly one function; a
  `function` list in inspect would change little.
- What reads are for (`ab-features-read-questions`, a "Q:" line before
  each read): orientation 41% of read cost (the 8.9K module overview
  alone 17%), how the code to change works 30%, where X is used 22%
  (broad greps), how tests are written 8%. Cheap wins: a smaller module
  overview; the find tool on the back burner now has evidence (2.6
  where-is-X reads per run). Reads are ~15% of input, so turns matter
  more.
- PR #122 was merged without its second commit (this file), which was
  pushed after the user had seen the PR. Put later work in a new PR
  rather than adding commits to one the user may already be merging.
- Roadmap item 15: refusals for ranges that start at the `func` line are
  done (#124); they give the body's lines and, for `replace_statements`,
  `function`. The worked example of a batch is in the instructions
  (#174), and `TestInstructionsExampleRuns` runs it, so keep it valid.
- Third-party A/B (roadmap item 16): `eval-tasks/third-party/` has 172
  candidate PRs from five large repos, and `ab.json` there the first ten
  tasks, built by `tasks.py` from `tasks.json` (prompts by hand) and
  validated: at the parent the code builds and the hidden tests fail, at
  the PR all pass. The check starts with `go build ./...`, not `go vet
  ./...` (20 minutes and 18 GB cold on Hugo). First run (`tp-first`,
  hybrid vs manual, 1 run each, Go 1.27.3 via `GOTOOLCHAIN=go1.27.3`
  for the harness so the server is built with it too): $1.96 each arm,
  8/10 and 9/10; the hybrid arm never refactored. A failure in both arms
  can be the prompt: read `check_output` in results.jsonl against the
  hidden test first. The mcp arm on the four refactor-on-the-way tasks
  (`tp-mcp-refactor-first`): 4/4, $0.98 against ~$0.72 by hand; it
  edits through the tools fine on large code but still does not
  refactor structurally, even told to. Its first `inspect` waits for
  the module to load (296 s on Hugo). Rerun after #181-#190
  (`tp-mcp-type-errors`): type-check refusals 2.0 to 0.5 per run, 8/8,
  same cost; the new stubs, fix_imports and family changes rarely
  fired, so most of the drop is how the runs went. Preload did not
  help Hugo's first call (the load itself is slow in a fresh copy).
  Fixed by loading with `-trimpath` (#195): Go's build cache keyed the
  module's packages by directory, so every A/B copy compiled Hugo again
  (288 s); with it, a second copy loads in 2.2 s. The first load after
  the change still fills the cache, so time a run's second task, not
  its first. What making them taught:
  - Read each hidden test before writing a prompt. "Black-box" only
    means it compiles against the old code: one compared a function's
    result with `reflect.DeepEqual` (pins a `[]string` return), one only
    checked a default list. Prompts must give every exact string the
    tests check (error messages, config names).
  - Hidden test files are the PR's whole files, some internal; every
    prompt says hidden tests use the existing names and signatures, or a
    refactoring run would fail for refactoring.
  - Fixtures under `testdata/` belong with the tests (fixed in
    verify.py on Oct 20; four candidates re-verified).
  - The GitHub API is closed to repos outside the session; read PRs and
    issues with WebFetch. Gitea's tests refuse to run as root (use
    `--as-user`), and verifying fills the disk (`--clean-cache`).
- The type-error help, measured on tasks that call for it
  (`eval-tasks/mechanisms.json`, mech-now vs mech-before): it fires and
  removes the refusals it targets, but cost and turns stay the same.
  Hidden tests there are named TestAccept... so that a model's own test
  does not clash with them.
- **Working algebraically** (the user's aim, 2026-10-20): the user sees
  code as a structure manipulated by type-safe operations, like algebra,
  and believes 99% of changes can be reached so, which lets one think
  above syntax. They want to know whether that has a measurable benefit
  for the model. `docs/algebraic.md` defines the measure (the derived
  share, in refactor-tokens) and the baseline: fresh mcp runs derive
  about a tenth. Derivations are being collected from the user by
  posing small problems; they become recipes. Roadmap item 19.
- Sessions in a cloud clone have no MCP on that clone (the user's MCP
  runs on their own checkout); build `cmd/refactor` from the branch and
  use it for every step: `refactor batch '<json>'` runs a batch, and
  edits get stubs as through the server (#160). Read with
  `refactor inspect` (`'{"names":["F","T.M"]}'`, `file=a.go`), not
  sed, grep or Read: it is what the server's inspect shows, so a
  session reads as the tools intend and tries new read features as it
  builds them (2026-10-22, the user). `refactor check run_tests=true`
  is the check tool, and `refactor undo` reverts the last step or batch
  of any earlier invocation: the CLI keeps the undo stack in the user's
  cache directory, a file per module, its last 50 changes (2026-10-27).
  Every test binary that reaches the TypeScript checker links the whole
  compiler, so the Go build cache grows to about 27 GB and fills the
  session's disk: on "no space left on device", `go clean -cache`. Write long JSON to a
  file in the scratchpad and pass `@path` (or `-` for stdin, #205):
  Go escapes such as `\n` then survive, which an unquoted heredoc
  turned into newlines.
- The smaller known bugs are fixed: `replace_text` adds imports (#173);
  `create_from_usage` names parameters by type and does not take a
  result type from an uninstantiated generic callee (#176).
- A full code review at a641b61 is in `docs/code-review-2026-10-20.md`:
  25 findings, 7 of them silent behavior changes or broken guarantees,
  most from panicking expressions treated as stable and methods reached
  by reflection. Its fix order: batch rollback and undo after a step
  breaks `go.mod` (items 6-7), then `analysis.Stable` (8, with 2).
  The user chose the panic class first (8, then 2, then 6-7). Item 8 is
  fixed (#136): `analysis.MayPanic` names what can panic in an
  expression `Stable` accepts; inline_variable, extract_variable's order
  check and inline_function's substitution and dropped results use it,
  while `all_occurrences` (one shared value) does not need it. Item 2 is
  fixed (#138): introduce_parameter refuses an expression that can panic
  after a `return`, and counts `s[0]` on a string as able to panic.
  Items 6-7 (#139): `workspace.Apply` reloads before pushing the change,
  and when the module no longer loads it puts the files back and returns
  `ErrBreaksLoading`, which `Run` refuses; a batch then rolls back its
  earlier steps. The `undo` tool (#140) goes ahead when the refresh
  before it fails, since it needs only the files, and says the module
  does not load. The review's fix order is done; the user then asked
  for the rest of the high items, one PR each, branched one from the
  next and merged in order: 1 (#141, `analysis.DependsOnPosition`, now
  shared with move_declaration), 5 (#142: merge_duplicate_function's
  key counts the optional parts of slices, for loops and `...` calls),
  3 (#143: rename compares, for each type with the method, which
  interfaces with the old or new name it implements before and after,
  by method set), and 4 (#144: rename also searches every package-level
  interface of every dependency, as changesig does). All high items are
  done; the medium ones (8-18) remain, 8 already fixed. The user asked
  for those too, in the same chained way: 9 (#145, inline_function
  checks the operands on the left of an assignment), 10 (#146, no
  variable is dead after the call in a caller with goto), 11 (#147, an
  else if and a for post count as conditional uses; analysis.PathTo
  runs from the node outward), 12 (#148, changesig walks the literals a
  call runs, `syncWalk`), 13 (#149,
  extract_function's non-nil error: writes later in a loop count, and a
  helper with defer proves nothing), 14 (#150, remove_dead_code keeps a
  method through which its type implements a dependency's interface or
  error), 15 (#151, rename refuses an exported field of a struct passed
  as an interface: `analysis.PassedAsInterface` and `Holds`, now used
  by extract_struct too), 16 (#152, statement content may not
  close its block: `gocode.ParseStatements` and add_case), 17 (#153,
  undo removes a created directory last and only if empty), 18 (#154,
  paths resolve at the module root first). All high and medium items
  are done; what was found on the way, and the low items 19-25, are
  roadmap item 17, done one PR each, chained the same way: the false
  refusal for a literal's own parameters (#156), extract_struct on the
  shared reflection helpers (#157), one dependency walk
  (`analysis.Dependencies`, #158), one list of functions that run a
  closure during the call (`analysis.syncCalls`, #159), the CLI matching the server (#160), then the
  low items: 19 (#161, introduce_parameter and a variadic parameter),
  20 (#162, create_file formats only `.go` files), 21 (#163,
  replace_expression removes imports it leaves unused;
  `gocode.RemoveImports` takes a position test, `gocode.Between`), 22
  (#164, unfinished A/B runs are marked and left out of the means), 23
  (#165, `edit.LineMapOf` compares lines without their alignment), 24
  (#166, a batch applies `aliasArgs` before line translation), 25 (#167:
  rename refuses changing whether go test runs a function, `testKind`;
  `make` of a slice or channel sized other than by a constant, len or
  cap can panic; extract_function's `sharedLoopVar` before Go 1.22, by
  `info.FileVersions`). All 25 review items are done, and item 17 with
  them: one check for terminating statements, `analysis.TerminatingList`
  (#170). Then roadmap item 15's open bullets, one PR each, chained from
  #170: a smaller module overview (#171, one line per package),
  replace_text adds imports (#173, `gocode.ImportNames`), a worked batch
  in the instructions (#174), an mcp-arm CLAUDE.md saying its Bash
  cannot change files (#175, `eval-tasks/claude-md-refactor-first-bash`,
  now the features run's; the earlier dirs stay as baselines; not yet
  measured), create_from_usage's names and generic results (#176). Counting refused Bash reads apart is done
  in #172 (refactor-tokens), not here.
  Change only
  this repo copy: the Claude Doc linked from #130 is not kept up to date.
