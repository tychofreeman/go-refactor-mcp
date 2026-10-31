# go-refactor-mcp

**You know the discipline (small behavior-preserving steps, make the change
easy then make the easy change, two hats), and gorefactor gives it to an AI
coding assistant.** It's an MCP server with about 45 Go refactorings and
edits, much of Fowler's catalog as type-checked tools: the model picks the
step, and the engine checks preconditions (evaluation order, side effects,
panics, name capture), applies it everywhere with the type checker, and
rolls back anything that doesn't compile. Refactorings refuse anything that
would change behavior, behavior changes go through separate edit tools, and
test-first works because what a failing test calls is stubbed so the code
still compiles. Like junior developers, models don't refactor on their own:
they extend what's there, unless "make the change easy" is a step of its
own, which found the right design in all 15 runs we tried. And because
writing code costs a model almost nothing, refactoring first pays off where
a change reaches many places, not on small local edits, so the open question
is the one you'd ask: can the craft be taught to the machine, not just
handed to it as tools?

```
model ──(tool call: rename {...})──▶ engine: check preconditions
                                             apply byte-range edits
                                             re-type-check, roll back on error
model ◀──(APPLIED + diff | REFUSED + reason)──┘
```

Each step returns a unified diff, or a refusal saying why, which the model
reads before choosing the next step. A `batch` sends several steps as one
change, rolled back as a whole, and every step can be undone.

## Status

- **Refactorings:** rename and move declarations; extract and inline
  functions and variables; add, remove, move and introduce parameters,
  change parameter, result and field types (an interface method with its
  implementations); method to function and back, move a method, extract a
  struct; `fold` (replace code that does what a function does with calls
  to it), `parameterize`, merge duplicate functions; a type switch into
  methods; invert if, remove else, slide statements, remove dead code.
- **Edits**, which change behavior on purpose and are still type-checked
  and undoable: create, move and delete files, add declarations, fields,
  cases and elements, replace statements, expressions, declarations and
  text, and `create_from_usage`, which declares what a test written first
  uses.
- **TypeScript:** most of the same tools under the same names, chosen by the
  file's extension, built on the Go port of the TypeScript compiler.
- **Releases:** tagged by CI from v0.3.0 (`docs/releases/`).
- **Evidence:** A/B runs against editing by hand in
  `docs/cost-history.md`, every hand edit in `docs/friction-log.md`, and
  the derivations found so far in `docs/derivations.md`.
- **Roadmap:** `docs/roadmap.md`.

## Layout

```
cmd/refactor-mcp/     the MCP server (stdio), for Claude Code and other MCP clients
cmd/refactor/         the CLI: a step, a batch, inspect, check or undo per invocation
cmd/refactor-ab/      A/B runs: tasks with claude -p, through the tools and by hand
cmd/refactor-tokens/  where a Claude Code session's tokens went, by tool
cmd/read-guard/       the hook that sends this repo's Go reads through inspect
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

## Getting started

Install the MCP server and the CLI:

```sh
go install github.com/tychofreeman/go-refactor-mcp/cmd/refactor-mcp@latest
go install github.com/tychofreeman/go-refactor-mcp/cmd/refactor@latest
```

Or work from a clone:

```sh
git clone https://github.com/tychofreeman/go-refactor-mcp
cd go-refactor-mcp
go mod tidy          # fetches dependencies and updates go.sum
go test ./...
go install ./cmd/refactor ./cmd/refactor-mcp
```

`go install` puts binaries in `$(go env GOPATH)/bin` (usually `~/go/bin`);
add that to your `PATH`, or install elsewhere with `GOBIN=~/bin go install ...`.

## Using it from Claude Code

Register the server once for all your projects (user scope):

```sh
claude mcp add --scope user gorefactor -- refactor-mcp
```

Or share it with a project by committing a `.mcp.json` at its root (see
`examples/mcp.json`):

```json
{
  "mcpServers": {
    "gorefactor": { "command": "refactor-mcp" }
  }
}
```

For the instructions, `examples/CLAUDE.md` is a CLAUDE.md to copy into
your project: the one that did best in the A/B runs (the user's moves of
`eval-tasks/claude-md-derivations`, cut to what measurably helped;
`docs/cost-history.md` sections 8, 9 and 14). Without a CLAUDE.md that asks
for them, a fresh model does not use the tools.

If `refactor-mcp` is not on the `PATH` Claude Code sees, use its absolute
path as the command. The server works on the module containing `-C`, or
`$CLAUDE_PROJECT_DIR` (which Claude Code sets), or its working directory.

Setup notes:
- Claude Code reads `.mcp.json` when a session starts: one added during a
  session takes effect in the next.
- The tools need Go 1.26 or later. With `GOTOOLCHAIN=auto` (the default)
  `go install` fetches it; with `GOTOOLCHAIN=local` an older Go fails.
- Tool names carry the server's name as Claude Code sees it:
  `mcp__gorefactor__inspect` here, `mcp__remote-devices__gorefactor__inspect`
  through the desktop app's bridge. Instructions copied from this repo's
  CLAUDE.md name the first and speak of "this checkout"; in another
  project, name the tools without the prefix, as `examples/CLAUDE.md` does.
- `refactor-mcp -version` and `refactor -version` say which build runs:
  the release, or the commit it was built from.
- `refactor list [tool...]` prints each tool's description and arguments.

Then, in a Go project, ask something like *"use the gorefactor tools to
simplify calc/calc.go"*. Run `/mcp` in Claude Code to see the server's status.

`refactor list` prints every tool with its description and arguments,
as the model sees them. The server also sends the model instructions at
startup: which tool for what, and how to read and batch. It keeps one
workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it updates instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

## Measuring whether the tools help

`cmd/refactor-ab` runs tasks in fresh copies of a module with a fresh
`claude -p`, per arm (through the tools, by hand, or both), and reports
cost, tokens, turns and pass rates; `cmd/refactor-tokens` says where a
session's tokens went. CLAUDE.md ("Measuring cost") has the usual runs,
and `docs/cost-history.md` the results.

## Using the CLI

The CLI runs one step with the same engine and arguments as the MCP tools:

```sh
refactor list
refactor -C path/to/module -n rename file=main.go line=7 name=t new_name=total   # preview
refactor -C path/to/module    rename file=main.go line=7 name=t new_name=total   # apply
refactor rename '{"file":"main.go","line":7,"name":"t","new_name":"total"}'      # JSON args, as a model sends them
refactor -n extract_variable file=main.go line=12 expression='qty*unit' name=subtotal
refactor -n inline_variable file=main.go line=12 name=subtotal
refactor -n extract_function file=main.go start_line=20 end_line=31 name=printReport
refactor -n inline_function file=main.go line=20 name=printReport
refactor -n invert_if file=main.go line=14
refactor -n remove_else file=main.go line=20
refactor -n add_parameter file=main.go line=9 name=copyTree parameter=skip type=string value=cfg.OutDir
refactor -n remove_parameter file=main.go line=9 name=copyTree parameter=skip
refactor -n move_parameter file=main.go line=9 name=copyTree parameter=dst before=src
refactor -n move_declaration file=main.go line=40 name=copyTree to_file=copy.go
refactor -n create_file file=half_test.go content="$(cat half_test.go.draft)"
```

Exit status: `0` applied/previewed, `2` refused, `1` error.

`-C` can be any directory inside a module: the tool always loads the whole
enclosing module (nearest `go.mod`), so references in every package are
updated. `file` is resolved relative to the module root, as the tools print
paths; a path that names no file there but one under `-C` means that one.
A new file (`create_file`) goes at the module root.
References in *other* modules (e.g. siblings in a `go.work`) are not
updated; exported renames add a note saying so.

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

Seven opt-in tests try the
refactorings on real code; each modifies the module it is pointed at, so use
a copy:

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
