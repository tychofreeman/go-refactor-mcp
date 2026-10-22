# Session notes

What sessions working on this project have learned that the code, the
README and the other docs do not record: the user's preferences, why
past choices were made, and where the work stands. Every session reads
this (CLAUDE.md imports it). When you learn something a later session
would need, add it here in the same PR as the work, and correct or remove
what has become wrong. Keep it short: facts, with the reason, not a diary.
The longer history is in `docs/how-this-project-was-built.md`, and A/B
results are in `docs/cost-history.md`.

## Working with the user

- One item per branch and PR. The user merges, then reconnects the MCP so
  the next item uses the new server; wait for that before relying on a
  change to the tools.
- Ask before starting an A/B run unless the user asked for one: each costs
  a few dollars.
- Append friction log entries at the end of `docs/friction-log.md`.
  Inserting them before an older entry put every PR's entry at the
  same place, so parallel PRs conflicted there one after another
  (2026-10-22, #263-#267).
- When the user says "do X, then Y", do them in order, each in its own PR.
- Report results plainly, including when a change did not help (#119 was
  reverted in #120 after its rerun showed nothing).
- Stacked PRs: if one fails CI, fix the lowest branch and merge upward;
  do not force-push. (#41 failed on gofmt after an import added with sed.)
- Record findings, open issues and decisions in the repo, in the PR of
  the work: the roadmap in README for anything to do later, these notes
  for context, the friction log and docs/ for the rest. Not in claude.ai
  Project docs or memory: the user works in Claude Code, whose sessions
  read only the repo (2026-10-20, after review follow-ups had reached
  only these notes and the friction log, not the roadmap).

## Decisions and their reasons

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
  written against what the refactoring makes); the user agreed.
- **Claude via MCP is the product**, not the Ollama loop or standalone
  evaluation (2026-10-17): the user said those were not what they need.
- **Choose the next tool by evidence:** the friction log for hand edits,
  A/B transcripts for a fresh model's refusals and wasted turns.
- **Query tool deferred** (2026-10-18): about 55% of greps were symbol
  lookups, but grep was about 5% of an arm's input tokens. Revisit only
  with new evidence, and check gopls's MCP server first.

## A/B findings to remember

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

## Where the work stands (2026-10-20)

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
  edits get stubs as through the server (#160). Write long JSON to a
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
