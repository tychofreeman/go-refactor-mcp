# How this project was built

A record of the practices behind go-refactor-mcp, written to decide which
are worth repeating. It covers 15–18 October 2026: 159 commits, PRs #1–#65.
It draws on the git history, `docs/friction-log.md`, `docs/edits.md` and
the notes kept between sessions; judgments are Claude's.

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

Work went out as small PRs (65 in four days), stacked when two touched
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

## What did not work as well

- **Line numbers.** Several tool calls reused line numbers from before an
  edit and were refused; re-inspecting after every change avoids that.
- **Process leaks.** Long-running background jobs need an explicit check
  that they stopped; the recursive stress tests ran on unnoticed for a
  while.
- **Measuring before building.** The effect summaries were built on the
  strength of a refusal count, without first breaking down *which* calls
  were refused. A ten-minute breakdown first would have pointed at the
  standard library.

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
