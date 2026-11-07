# Session notes

What sessions working on this project have learned that the code, the
README and the other docs do not record: the user's preferences, why
past choices were made, and facts a session needs to work here. Every
session reads this (CLAUDE.md imports it), so keep it short: facts with
their reason, not a diary. Add what a later session would need in the same
PR as the work, and correct or remove what has become wrong. The history
behind these notes, with the measurements, is in
`docs/how-this-project-was-built.md` (its appendix holds the notes as they
were until 2026-11-04) and `docs/cost-history.md`; the refactorings and
working patterns, with their steps, are in `docs/patterns.md`.

## Working with the user

- **One item per branch and PR; the user merges** (or asks a session to),
  then pulls main and reconnects the MCP, so the next item uses the new
  server. Wait for that before relying on a change to the tools. When the
  user says "do X, then Y", do them in order, each in its own PR.
- **Stacked PRs:** if one fails CI, fix the lowest branch and merge
  upward; do not force-push. Put later work in a new PR rather than adding
  commits to one the user may already be merging (#122 was merged without
  its second commit).
- **Ask before an A/B run** unless the user asked for one: each costs a few
  dollars.
- **Report results plainly**, including when a change did not help (#119
  was reverted in #120 after its rerun showed nothing).
- **Record findings in the repo,** in the PR of the work: the roadmap for
  anything to do later, these notes for context, the friction log and
  `docs/` for the rest. Not in claude.ai Project docs or memory: the user
  works in Claude Code, whose sessions read only the repo (2026-10-20).
- **Friction log entries go at the end** of `docs/friction-log.md`, with
  replace_text's `append`: inserting them before an older entry made
  parallel PRs conflict there (#263-#267).
- **"What's next?"** (2026-10-31): the `whats-next` skill offers items and
  keeps a record per item in `docs/sessions/<branch>.md`, so the user can
  judge whether a session used the tools or worked around them. The user
  chose both an objective log (the hook `cmd/session-log`, read by
  `refactor-tokens -log`) and the model's own notes (why a hand edit, what
  followed a refusal, what a read answered); a hook, because a habit named
  in the instructions did not change. The record answers, for each large
  write, whether lines were written because that was easier than the
  tools.
- **Reason from the highest abstraction that captures the problem**
  (2026-10-22): name the change at that level ("this field holds one of
  three modes, switched on in two places"), choose the design and the
  operation there, and only then descend to tools, names and lines. Asked
  after #283, where reasoning at the level of lines added a second flag to
  what was already a type code.
- **TDD's order: refactor only while green** (2026-10-31: "not the only
  session to make this mistake"). Refactor to make the change easy before
  the test; red; the least code that passes, a copy included; then
  refactor. A refactoring refused because a new test does not compile is
  right. When red reveals a missing step, roll back to green (undo), take
  the step there, and redo what followed; the user prefers the extra turns
  to refactoring while red.
- **Look for a composition before logging a gap** (2026-10-29): the 34
  calls of `set` were logged as "no tool does this"; the user saw extract
  one call per field, fold, inline `set`. Sessions take the target as one
  transformation and check tools one at a time, so a gap names the
  sequences tried.
- **replace_text is not a way to paste code** (2026-10-31: "the edit tools
  have been used too much as an impediment"): #524, #528, #529 and #532
  made changes with replace_text on Go where add_case, add_element,
  replace_expression or add_parameter fit. This repo's server runs with
  `-no-code-by-text`, which refuses that; if it costs more turns than it
  saves, say so and take it out of `.mcp.json`.
- **The README** (2026-10-30): it opens with the pitch for developers who
  know TDD and refactoring, five sentences the user chose, then Status.
  Keep it to what a developer needs to judge and install the tools;
  instructions for Claude go in CLAUDE.md, and `examples/CLAUDE.md` is what
  a user copies. The README is for Go and TypeScript alike, with one
  sentence at the top on their parity, not a version for each; the README
  says what the example asks for and why, with one honest sentence on the
  evidence and a link, and the friction log is a "Help improve the tools"
  section, not part of the example (2026-11-05, the user). "Using the
  tools well" (the user, same day) carries the example's structure and the
  guards, which users install from `examples/guards.json`; keep it and
  `examples/CLAUDE.md` saying the same. Order (2026-11-05, the user asked
  for a review for programmers who let models do most of the work): the
  pitch, then "What you get today, and what is not proven yet" with a real
  applied and refused step, Status, install, Claude Code, using the tools
  well, troubleshooting, the CLI, helping. What a contributor needs
  (layout, design decisions, testing, adding a refactoring, measuring) is
  in `CONTRIBUTING.md`. Keep the evidence sentences in step with
  `docs/cost-history.md`.
- **Releases** (2026-11-05, the user): every push to main that passes Test
  is released by `.github/workflows/release.yml`, with notes generated
  from the merged PRs. The PR's label picks the version: `major` or
  `minor` bumps that part, anything else the patch; label a PR that adds
  a tool `minor`. There is no version constant to raise: the binary
  reports the tag it was installed at, or "devel" and a commit
  (`mcpserver.Version()`). `docs/releases/` holds the notes of v0.3.0 to
  v0.5.0, written by hand. Sessions may not push tags or create releases
  (HTTP 403); the workflow's token can.
- **CI minutes** (2026-10-27): 5,000 Actions minutes a month, so Test
  skips changes to docs alone; it runs on go.mod's minimum (1.26.x) and the
  newest Go. Keep CI runs few: batch small fixes that belong together.
  CI is slow because every package that reaches the TypeScript compiler
  compiles and links it: the test job's cache key includes test.yml (edit
  it to start a new cache), the vulnerability job does not cache, and
  `internal/ts/...` runs without -race.

## Working in this repo

- **Cloud clones have no MCP** (the user's server runs on their checkout):
  build `cmd/refactor` from the branch and send every step through it,
  `refactor batch - <<'EOF'` with the JSON (quote the delimiter: an
  unquoted heredoc turns `\n` into newlines), and code itself as text with
  `content_file=- <<'GO'` (the user, 2026-11-05: no scratch files or
  Python to build the JSON; the delimiter must not occur in the text,
  and its final newline is dropped except from `content`),
  `refactor inspect` to read, `refactor check run_tests=true`, `refactor
  undo` (its stack is kept per module in the cache directory). Read
  through inspect there too, so the
  session reads as the tools intend.
- **A session started outside the checkout has no guards** (2026-11-05):
  hooks load from where the session starts, so a cloud session that clones
  the repo later can grep and sed Go files unrefused; one did, and the user
  caught it. Read through `refactor inspect` (with `pattern` for a line)
  all the same.
- **Disk:** every test binary that reaches the TypeScript checker links
  the whole compiler, so the build cache grows to about 27 GB; on "no space
  left on device", `go clean -cache`.
- **The read guard** (`cmd/read-guard`, 2026-10-30): the user asked for a
  check after sessions kept reading Go by `sed` and `cat` though asked to
  use inspect. It denies with a JSON decision and exit 0 (`go run` turns
  exit 2 into 1, which Claude Code takes for a failed hook). For a fresh
  model it saved nothing (cost-history 21); it stays for this repo's own
  sessions, where it ended shell reads for 4% of turns refused
  (cost-history 22).
- **The write guard** (`cmd/write-guard`, 2026-11-05, the user: "I would
  like to experiment with a write guard"): the read guard's counterpart for
  changes, on in this repo. It refuses Edit, Write and MultiEdit of a `.go`
  file outside testdata/, and Bash commands that write one (`sed -i`,
  `perl -i`, `tee`, `cp`/`install`/`ln` into one, `mv`, `rm`, a
  redirection); `gofmt -w` and git are allowed. Its refusal names the
  edits and says to log a change no tool makes and ask the user. The
  guards share `internal/hook` (the call, the denial, `Commands`, `Each`).
  What to watch: roadmap 53.
- **Worktrees** (2026-11-02, roadmap 50): the server and hooks belong to
  the session's starting checkout; a subagent with `isolation: "worktree"`
  shares them, with the parent's MCP roots but its own `cwd` in hooks.
  `cmd/worktree-paths` makes relative paths absolute in the caller's
  worktree and refuses paths into the main checkout from one (the user:
  isolation is the point). Without the hook, the server asks for roots on
  each call; roots are deprecated by SEP-2577, and the user chose to build
  on them while they last. To try a new MCP server: `claude mcp add`, then
  restart; `/mcp` reconnects one already loaded.
- **Stress tests with the modules' tests** (`STRESS_TESTS=1`) when a
  refactoring changes what it writes (2026-11-04): that run found silent
  behavior changes no golden had (inline_function's shadowed returns and
  `+=`; convert_params_to_object in external test packages).
  jsonschema-go, uritemplate and x/tools from the module cache, copied to
  a scratch directory, are enough; x/tools'
  `go/analysis/passes/modernize` fails without a Go toolchain binary,
  whatever the change.
- **A GOROOT whose `src` is a symlink** made every golden that imports the
  standard library fail on the user's workstation until 2026-10-31 (item
  47.4): where an import of `strings` is refused, suspect that first.
- **Measuring a session:** the transcript keeps only what follows the last
  compaction, so measure before one; batches sent through the CLI count as
  Bash in `refactor-tokens`.

## Decisions and their reasons

- **Claude via MCP is the product** (2026-10-17), not the Ollama loop or
  standalone evaluation. A plan-and-execute loop of our own was tried as an
  experiment (`docs/plan-and-execute.md`, roadmap 29); the user decides on
  its result.
- **Every step must type-check** (2026-10-17). Test-first still works:
  stubs are declared for what a new test calls, and an edit may start from
  broken code if it fixes some errors and adds none. A refactoring may
  too when every error is in a test file written against what it makes
  (2026-10-20), but not between red and green.
- **A refused batch is kept for a retry** (2026-11-06, as java's #247):
  `refactor.Retry`, in the server (above the languages: a retry may name
  no file) and in the CLI (`<undo file>.retry`); a batch kept through a
  checkpoint is not kept, since its first steps applied.
- **Stubs are declared lazily, and batches get checkpoints** (2026-10-20):
  in a batch checked at its end, stubs for earlier edits are added just
  before a refactoring; a `check` step type-checks (and may run tests), and
  a later refusal keeps the steps through the last passing one.
- **The model reasons about intent, the engine about types**
  (2026-10-20): take type-safe code off the model so it reasons at a
  higher level (roadmap 18, `docs/type-errors.md`). A parameter's type
  changes through an alias, the user's way by hand: `change_type`
  retargets it, and is an edit so a batch checks it with its fixes.
- **Manual edits as small as possible** (2026-10-21): not every operation
  will exist; use the ones there are and judge runs and gaps by the size of
  the hand edits, not only the derived share. On code with many uses the
  user's moves cost less and wrote far fewer lines by hand
  (cost-history 8 and 9); on small modules, writing it out is as cheap.
- **Work algebraically** (2026-10-20, the user's thesis): code is a
  structure manipulated by type-safe operations, and 99% of changes can be
  reached so. `docs/algebraic.md` defines the measure (the derived share,
  in refactor-tokens). Derivations come from the user by posing small
  problems, not by asking for examples (`docs/derivations.md`).
- **General operations, not task-shaped tools** (2026-10-21):
  `fields_to_map` (#217, closed) fit one task. A gap becomes an operation
  that composes with the others, for any type and target.
- **Options over new tools, and a budget of tools** (2026-10-21,
  2026-10-22): more tools did not help a fresh model, and choosing gets
  harder as they multiply. The order for a new operation: a proof in the
  engine, an option of an existing tool, a composite named by intent, and
  only then a new tool. `TestToolBudget` holds the count (50) and the
  instructions' size (6,000 characters); raise them only with the user.
  An option surfaces through a refusal or a recipe, or it goes unused.
- **One tool per operation, other names as aliases** (2026-10-27): the
  same names in both languages where the operation is the same, so
  recipes transfer; `refactor.ToolName` maps other names (`add_property`,
  `change_return_type`), and `aliasArgs` other argument names. Add a
  synonym only when it can mean one argument and a run shows it refused.
- **Tool descriptions: an example per use, the rules in refusals**
  (2026-10-21): cutting `fold`'s worked example of near-duplicates lost
  its use there (4/4 runs to 0/4); a fresh model decides whether a tool
  fits from its before/after examples. Write: what it does with a
  before/after and the addressing hints the schema lacks; not what it
  refuses, nor what every tool shares (the user, 2026-11-06, as java's
  #256: "refusals should explain themselves"; this replaced "one line of
  what it refuses"). Each refusal says why and what to do instead;
  `docs/checks.md` keeps the lists, and the instructions say once that
  edits import what they name. A definition stays under
  `TestLoadedToolsStaySmall`'s 1,700 characters. Checked by A/B the same
  day (cost-history 25): no more refusals, cost within the noise.
- **Examples come from no eval task** (2026-10-22): they use one garden
  domain (`Plant`, `Bed`, `Sprinkler`), and `TestExamplesAvoidEvalTasks`
  fails on a name or string of a seeded eval module in published text.
- **Names and results say the intent** (2026-10-22, on
  `prepare_switch_for_types`): what the step is for, not how; the
  mechanism comes after, in the example.
- **Published text and the baselines:** a change to the instructions or a
  description changes what A/B baselines saw. Schemas come from argument
  structs (`refactor.Schema[T]`); dump `golang.Language{}.Tools()` and
  `typescript.Language{}.Tools()` before and after to check a refactoring
  of them publishes the same. A project without Go gets
  `TypeScriptInstructions`.
- **Exported code is changed, with a note** (2026-10-21): as gopls does,
  refactorings update this module and say the module's API changed.
  Checks that guard behavior inside the module (reflection, interfaces)
  stay.
- **A nil dereference may move past calls that return** (2026-10-21): it
  panics either way, only what is printed before differs. Not past calls
  that might not return (os.Exit, log.Fatal, a function value or
  interface method) or other panics. The user is undecided on technical
  grounds but chose it as the better fit for the tools; keep it narrow and
  easy to revert. `fold` extends it (its own PR, to drop if the user
  decides against it).
- **One analysis of what code does** (2026-10-25 to 10-30): facts about
  the standard library live in one table, `stdFuncs`, and verdicts derive
  from the facts; a module function counts by its body, so a golden that
  needs an effect must have one (`println`). Whether an expression can be
  evaluated elsewhere or another number of times is one question,
  `analysis.Move`; refactorings state the move and word the refusal from
  the `Obstacle`. The ordering rules both languages share are
  `internal/reorder`: change a rule there. Ownership is `analysis.Owner`;
  a refactoring's file is a `site.Site` (`site.At`).
- **Rules the user chose among options** (2026-10-29 to 10-30): fields
  that fmt prints may be renamed, with a note listing the calls, while
  what finds a field by name (encoding/json, text/template, an unknown
  function) still refuses an exported field; a getter that caches its
  result (`if r.f == nil {...}; return r.f`) reads, it does not write; a
  parameter may take a package variable's place (`replace_variable`), the
  unproved calls listed in a note; extract_variable judges what it reads
  against the code between its occurrences; a module function that calls
  its callback only during the call is known from its body. TypeScript
  follows each, with names found as strings (`JSON.stringify`,
  `Object.keys`, `in`) where Go has reflection.
- **Early returns inline** (2026-10-29, the user's derivation): wrap what
  follows an if that returns in its else, give the function one result
  variable each return assigns. inline_function does both itself, with a
  flag and a labelled break for a return inside a loop or switch, and the
  simpler form first when only `return E` follows. remove_else's `wrap`
  exposes the first step. A string last result counts as an error for an
  early exit (this repo's refusals return that way); numbers do not.
- **TypeScript in this repo, from one server** (2026-10-27): one server
  whose tools route by file extension, so the language-neutral code
  (diffs, server, batch, A/B harness) lands once. Descriptions follow what
  the repo holds, so Go's text and baselines are unchanged. A TypeScript
  workspace tolerates errors already there (Hono has 4 under TS 7): a step
  must add none ("relax the rule for now; we might want to revisit").
  TypeScript's type changes are edits, as Go's change_type is.
  Counterparts chosen in the session, not by the user (revisit if the user
  prefers another reading): move_method moves down to a subclass; getters
  and methods are summarized only when static or private; fold's setter
  is `{ ...p, f: v }`.
- **Go and TypeScript at parity** (2026-11-04, roadmap 52): one tool per
  PR, Go first where Go lacks it. Separate Query from Modifier is shelved
  (51.4): the user found the example copies too much code and drives
  toward no design. An expander that folds must export the struct's fields
  when any call is in another package, external tests included.
- **Parity of Go, TypeScript and Java** (2026-11-05, roadmap 54,
  `docs/parity.md`): one name per operation across the servers, the more
  generic one (extract_function, move_statement, change_type,
  extract_supertype), the others kept as aliases; one `move` for
  declarations, members and files (the user: easier for the model to reach
  for), one `inline` for functions, variables and a held struct, and
  `rename` of a file path renames the file; `preserve_exceptions`
  comes to Go and TypeScript, per call on every refactoring, with a note
  for each such move made without it (the user, 2026-11-06, over a server
  flag; `docs/checks.md`). The tool budget waits for the user's review
  of the list in parity.md (they expect to raise it). Parity is kept where it
  makes sense, not for its own sake (the user, 2026-11-06): extract_struct
  became extract_type in all three servers, since "struct" is Go's word and
  the new type in Java is a class or a record; extract_struct stays an alias
  (`toolAliases`); the A/B instruction files and reference batches use the
  new name too. extract_interface became extract_supertype in Go and
  TypeScript the same day (the user), the name shared with Java: it makes a
  supertype (members stay; the type embeds, extends or implements the new
  interface), not a held type; extract_interface stays an alias. extract_type
  on a TypeScript file runs TypeScript's own since 2026-11-06 (roadmap 52.4).
- **Constructive transformations** (2026-11-05, roadmap 55, as
  java-refactor-mcp's #212-#215): `create_file` with `declaration`, then
  `test_for`, then a skeleton-only flag, each in its own PR; the user's
  choices for Go and TypeScript are in the roadmap item. `-skeleton-only`
  is a server flag like `-no-code-by-text`, on in this repo's
  `.mcp.json` (the user, 2026-11-05): new Go and TypeScript files come
  from declaration and test_for, and functions from add_declaration.
- **Fowler's catalog and IntelliJ** (2026-11-04): `docs/catalog.md`.
  Run a composition before calling an entry missing: Encapsulate Variable
  and Record and Replace Function with Command first looked missing and
  are folds (derivations 14-16).
- **A reload compiles none of the module** (2026-10-27): `loadPackages`
  asks go list for metadata only, loads other modules' types from export
  data, and type-checks the module here; a 13-step batch went from 546 s
  to 21 s. `TestLoadMatchesTypedLoad` compares it with go/packages.
- **Choose the next tool by evidence:** the friction log for hand edits,
  A/B transcripts for a fresh model's refusals and wasted turns. The
  query tool is deferred (grep was about 5% of input); revisit only with
  new evidence, and check gopls's MCP server first.

## What the A/B runs and probes found

The numbers are in `docs/cost-history.md`; these are the conclusions a
session plans by.

- Without a CLAUDE.md asking for it, a fresh model never used gorefactor;
  `examples/CLAUDE.md` is the instruction set that did best, for users to
  copy. Update it when a run finds better instructions.
- With the tools, a fresh model edits through them but finds "nothing to
  refactor first" on additive features, like this project's own sessions:
  it extends what is there (a third flag where a mode was). Asked the
  design question in a session of its own, before the feature, it made the
  reference design in 15 of 15 runs (0 of 15 without); as a clause of the
  instructions it refactored only at the scale of a function. On small
  modules the step bought design, not tokens.
- Cost follows turns: input is about requests × 39K tokens, and about 38%
  of a session's cost is the first request's fixed context. The tools add
  about 1.8K tokens a request, a loaded schema 500-800 more. In sessions,
  editing through the tools cost more than by hand (about 1.5x on small
  tasks, 41-67% on freight-xl); in the plan-and-execute loop, deriving won
  where one step carries a change to many uses and writing won on local
  changes.
- Tasks every arm gets right do not show the tools' value: Haiku and
  Sonnet by hand passed every freight-xl and trap task. Evidence would
  come from tasks a model gets wrong by hand.
- A plan chooses a derivation from the examples it sees, and reads make
  the model write code out instead of deriving: with bodies in view both
  Haiku and Sonnet wrote more by hand.
- Reading is not where the tokens go at these sizes (Go reads were under
  4% of input); a quarter of input was "where is X used" searches, which a
  derivation does not need.
- Harness: the MCP binary is built into the output directory at the
  start, but the module is copied from the working tree per run, so stay
  on the branch until a run ends. A run cut off before its result is
  `unfinished` and left out of the means. A resumed `claude -p` session
  reports a running total cost: take the last one.
- Third-party tasks (`eval-tasks/third-party/`): read each hidden test
  before writing a prompt, give every exact string it checks, and say that
  hidden tests use the existing names. Read `check_output` before blaming
  an arm.
