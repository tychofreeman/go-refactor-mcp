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

- **Every step must type-check** (2026-10-17). The user rejected letting
  edits leave type errors. Test-first still works: stubs for what a new
  test calls are declared automatically, and an edit may start from
  broken code if it fixes some errors and adds none.
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
- Harness details: the MCP binary is built into the output directory at
  the start, so checking out another branch during a run is safe. The mcp
  arm cannot use Edit or Write, and its Bash may not change files: in
  `claude -p`, an allow rule such as `Bash(cat:*)` does not cover a
  redirect that writes a file (checked 2026-10-20). A run cut off before
  its result (a timeout) is marked `unfinished` and left out of the
  means of cost, tokens and turns (#164); before that it counted as 0.

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
  `function`. Still open: one worked example of a batch in the
  instructions.
- Third-party A/B (roadmap item 16): `eval-tasks/third-party/` has 172
  candidate PRs from five large repos, and `ab.json` there the first ten
  tasks, built by `tasks.py` from `tasks.json` (prompts by hand) and
  validated: at the parent the code builds and the hidden tests fail, at
  the PR all pass. The check starts with `go build ./...`, not `go vet
  ./...` (20 minutes and 18 GB cold on Hugo). Not yet run. What making them taught:
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
- Sessions in a cloud clone have no MCP on that clone (the user's MCP
  runs on their own checkout); build `cmd/refactor` from the branch and
  use it for every step: `refactor batch '<json>'` runs a batch, and
  edits get stubs as through the server (#160).
- Smaller known bugs: `create_from_usage` infers odd result types for
  generic calls and names parameters p, p2; `replace_text` does not add
  imports.
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
  `info.FileVersions`). All 25 review items are done; left on item 17:
  one check for terminating statements.
  Change only
  this repo copy: the Claude Doc linked from #130 is not kept up to date.
