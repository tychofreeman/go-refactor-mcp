# Contributing to go-refactor-mcp

How the server is built, tested and measured. For installing and using it, see the [README](README.md); for how sessions work in this repo, see [CLAUDE.md](CLAUDE.md).

## Layout

```
cmd/refactor-mcp/     the MCP server (stdio), for Claude Code and other MCP clients
cmd/refactor/         the CLI: a step, a batch, inspect, check or undo per invocation
cmd/refactor-ab/      A/B runs: tasks with claude -p, through the tools and by hand
cmd/refactor-tokens/  where a Claude Code session's tokens went, by tool
cmd/read-guard/       the hook that sends Go reads through inspect
cmd/write-guard/      the hook that sends Go writes through the tools (an experiment)
cmd/worktree-paths/   the hook that sends a call made from a git worktree to it
internal/
  refactor/           the engine: Refactoring, Edit, Refusal, Run, batch
  refactorings/       one package per Go refactoring, with golden testdata/
  edits/              one package per Go edit, with golden testdata/
  analysis/, reorder/ what code does (effects, panics) and whether it can move
  workspace/, history/  loading and type-checking a module; changes and undo
  inspect/            what inspect shows
  mcpserver/, lang/   the tools as MCP, routed by language
  ts/                 TypeScript (internal/ts/CLAUDE.md)
  ab/, tokens/        the A/B harness and the token measures
docs/                 roadmap, cost history, friction log, derivations, checks
eval-tasks/           A/B tasks and the instruction sets they compare
```

`cmd/refactor-agent` and `cmd/refactor-eval` run a local model (Ollama)
with the same tools; they work but are no longer developed.

## Measuring whether the tools help

`cmd/refactor-ab` runs tasks in fresh copies of a module with a fresh
`claude -p`, per arm (through the tools, by hand, or both), and reports
cost, tokens, turns and pass rates; `cmd/refactor-tokens` says where a
session's tokens went. CLAUDE.md ("Measuring cost") has the usual runs,
and `docs/cost-history.md` the results.

## Design decisions

- **Text edits, not AST reprinting.** Refactorings compute byte-range
  replacements against the original source, so comments and formatting
  survive. Changed files are run through gofmt only if they were gofmt-clean
  before (keeps diffs minimal; fixes alignment after a rename).
- **The engine is the safety net.** `refactor.Run` refuses to start on code
  that doesn't type-check, and after applying a step it re-type-checks and
  rolls back if anything broke. A rollback is reported as a bug in that
  refactoring's preconditions; the golden tests exist to keep that from
  happening. A step after which the module no longer loads at all (a
  broken `go.mod`) is put back and refused too, and a batch with such a
  step rolls back its earlier steps. `undo` needs only the files, so it
  works while the module does not load (a `go.mod` broken by hand), and
  says that the module does not load.
- **Incremental updates.** After an edit, only the changed files are
  re-parsed, and only the packages containing them and the workspace packages
  that import those (including test variants such as `h [a.test]`, which
  go/packages builds when `a_test` imports `h` and `h` imports `a`) are
  re-type-checked, in process, configured exactly as
  `go/packages` does it. A full load (`go list` plus type-checking everything)
  happens only when package metadata could change: files added or removed,
  `go.mod`/`go.sum`/`go.work` edited, or a file's package name, imports or build
  constraints changed. An edit and its undo take about 10 ms on a 20-package
  module, where two full reloads took about 370 ms. The golden tests check
  that every incremental update matches a fresh load exactly
  (`workspace.Compare`).
- **Refusals are results, not errors.** A `*refactor.Refusal` is written for
  the model: what is wrong and what to try instead. Internal failures are
  ordinary errors.
- **Model-friendly addressing.** Symbols are found by `file` + `line` + `name`;
  `column` is only needed when the same name means different things on one
  line, and the refusal says which columns to choose from. No byte offsets.
- **One tool per refactoring**, each with its own
  JSON schema, made from its arguments struct (`refactor.Schema`), and a
  description that says *when* to use it. Unknown argument
  fields are rejected so a misspelled parameter isn't silently ignored.
- **Test variants.** Packages are loaded with tests, so a file can appear in
  `p` and `p [p.test]`. Symbols are identified by declaration position, and
  edits are deduplicated, so references in `_test.go` files are updated too.


What each refactoring checks before it applies, and what the edits do:
`docs/checks.md`.

## Testing

`go test ./...` runs the unit and golden tests. The golden cases run in
parallel, and after each applied refactoring and each undo they compare the
incrementally updated workspace with a fresh load (one that reuses the
metadata of an earlier `go list` when the files' headers are unchanged;
see roadmap item 8). `go test -short ./...` skips that comparison for a faster local loop; CI always runs it.
`TestGoldenRoundTrip` checks that `inline_variable` undoes `extract_variable`:
for every applying `extract_variable` golden case, extracting and then
inlining the new variable must give back the original code (after gofmt).
`go test -bench . ./internal/workspace` measures an edit-and-undo cycle against
a full load.

Opt-in stress tests (`TestStress` and `TestRoundTrip` in a refactoring's
package; `grep -rl STRESS_DIR internal` lists them) try the refactorings on
real code. Each modifies the module it is pointed at, so use a copy. For
example:

```sh
cp -r path/to/module /tmp/target
# extract_variable on 400 random expressions:
STRESS_DIR=/tmp/target go test ./internal/refactorings/extractvar -run TestStress -v -timeout 1h
# inline_variable on every local variable:
STRESS_DIR=/tmp/target STRESS_N=100000 go test ./internal/refactorings/inlinevar -run TestStress -v -timeout 1h
# extract then inline back; the file must come back unchanged:
STRESS_DIR=/tmp/target go test ./internal/refactorings/inlinevar -run TestRoundTrip -v -timeout 1h
# extract_function on runs of 1-4 statements and random line ranges:
STRESS_DIR=/tmp/target go test ./internal/refactorings/extractfunc -run TestStress -v -timeout 1h
# invert_if and remove_else on every if statement:
STRESS_DIR=/tmp/target STRESS_N=100000 go test ./internal/refactorings/invertif -run TestStress -v -timeout 1h
# inline_function on calls to the module's own functions (STRESS_DIFFS=n logs n diffs to read):
STRESS_DIR=/tmp/target go test ./internal/refactorings/inlinefunc -run TestStress -v -timeout 1h
# extract_function then inline_function back; the file must come back unchanged:
STRESS_DIR=/tmp/target go test ./internal/refactorings/inlinefunc -run TestRoundTrip -v -timeout 1h
```

Any rolled-back change is reported as a bug (a missing precondition). With
`STRESS_TESTS=1` each applied change is also checked with the package's own
`go test`, which catches behavior changes. `STRESS_N` and `STRESS_SEED` pick
the sample; `STRESS_WORK` sets `GOWORK` for loading the target (`off` for a
standalone module).

## Adding a refactoring

1. Create `internal/refactorings/<name>/` implementing `refactor.Refactoring`.
   `Plan` must not write anything; return `refactor.Refusef(...)` for every
   precondition failure. Embed `refactor.Named[args]`, set by `New` to
   `refactor.Called[args]("name")`: it gives the tool its `Name` and its
   `InputSchema`, made from the struct `Plan` decodes (a field per
   argument with a `desc` tag, omitempty on the optional ones).
2. Add golden cases under `testdata/<case>/`: `in/` (a complete module with
   `go.mod`), `args.json`, and either `out/` or `refused.txt`; with `out/`,
   an optional `notes.txt` lists substrings the result's notes must contain,
   one per line. Generate `out/`
   with `go test ./internal/refactorings/<name>/ -update` and review the diff.
   The harness also checks that undo restores `in/` exactly.
   Then run a stress test against real code (see Testing).
3. Register it in `internal/refactorings/registry.go`. It then appears as an
   MCP tool (named `Name()`, described by `Description()`) and in the CLI
   automatically.
