# go-refactor-mcp

It works on Go and TypeScript: the same tools under the same names, at
near parity (roadmap item 52 tracks what still differs).

**gorefactor gives the discipline of refactoring to an AI
coding assistant.** It's an MCP server with about 45 Go transformations 
(refactorings and edits), much of Fowler's catalog as type-checked tools: 
the model picks the step, and the engine checks preconditions (evaluation
order, side effects, panics, name capture), applies it everywhere with the 
type checker, and rolls back anything that doesn't compile. 

**Why?** Because thinking in terms of transformations is thinking at a higher
level of abstraction, and I believe that makes programming faster and better.
I believe that a refactoring editor like IntelliJ or Eclipse is a force
multiplier. And I suspect that AI models will get the same effect.

**Refactorings specifically**
refuse anything that would change behavior; behavior changes go through
**separate edit tools**, and test-first works because what a failing test calls 
is stubbed so the code still compiles. Like junior developers, models don't
refactor on their own: they extend what's there, unless "make the change easy"
is a step of its own, which found the right design in all 15 runs we tried. 
And because writing code costs a model almost nothing, refactoring first pays
off where a change reaches many places, not on small local edits, so the open
question is the one you'd ask: can the craft be taught to the machine, not just
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
  bundle them into a struct,
  change parameter, result and field types (an interface method with its
  implementations); method to function and back, move a method, extract a
  struct or an interface, inline a struct; `fold` (replace code that does what a function does with calls
  to it), `parameterize`, merge duplicate functions; a type switch into
  methods; invert if, remove else, slide statements, split a variable, a
  loop or a phase, remove dead code.
- **Edits**, which change behavior on purpose and are still type-checked
  and undoable: create, move and delete files, add declarations, fields,
  cases and elements, replace statements, expressions, declarations and
  text, and `create_from_usage`, which declares what a test written first
  uses.
- **TypeScript:** the same tools under the same names, chosen by the
  file's extension, built on the Go port of the TypeScript compiler; a
  few are still Go's alone (roadmap item 52).
- **Releases:** every change to main that passes the tests is tagged and
  released by CI, `go install ...@latest` gets the newest, and
  `refactor-mcp -version` says which you have.
- **Evidence:** A/B runs against editing by hand in
  `docs/cost-history.md`, every hand edit in `docs/friction-log.md`, and
  the derivations found so far in `docs/derivations.md`.
- **Against Fowler and IntelliJ:** `docs/catalog.md`, entry by entry.
- **Roadmap:** `docs/roadmap.md`.

## Layout

```
cmd/refactor-mcp/     the MCP server (stdio), for Claude Code and other MCP clients
cmd/refactor/         the CLI: a step, a batch, inspect, check or undo per invocation
cmd/refactor-ab/      A/B runs: tasks with claude -p, through the tools and by hand
cmd/refactor-tokens/  where a Claude Code session's tokens went, by tool
cmd/read-guard/       the hook that sends this repo's Go reads through inspect
cmd/write-guard/      the hook that sends this repo's Go writes through the tools
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

## Getting started

The server is written in Go, so installing it needs Go 1.26 or later, for a
TypeScript project too. Install the MCP server and the CLI:

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

**1. Register the server,** once for all your projects (user scope):

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

If `refactor-mcp` is not on the `PATH` Claude Code sees, use its absolute
path as the command. The server works on the Go module (the nearest
`go.mod`) and the TypeScript solution (the nearest `tsconfig.json` and the
projects it references) at or above `-C`, `$CLAUDE_PROJECT_DIR` (which
Claude Code sets), or its working directory.

**2. Tell Claude to use it.** Without instructions that ask for them, a
fresh model never calls the tools: it edits files as it always has. Copy
`examples/CLAUDE.md` into your project's CLAUDE.md, or merge it into the one
you have, and put your project's name in its heading. It asks for three
things:
- **TDD with refactoring first:** make the change easy while the tests
  pass, then write the failing test, then the least code, then remove the
  duplication. Models extend the code that is there unless told to
  reshape it first.
- **Work as a derivation:** plan a change as operations that keep
  behaviour, ending in the one edit that changes it, sent as one `batch`,
  with the moves that have worked (near-duplicates folded onto one copy, a
  wrapper to change a signature, a getter before a change of
  representation).
- **Use the tools well:** load the schemas at once, read through
  `inspect`, address declarations by name, read refusals.

Of the instructions we tried, these got a fresh model to work through the
tools, and derive its changes, at the lowest cost; on the tasks we tried,
working through the tools has not yet cost less than editing by hand
([docs/cost-history.md](docs/cost-history.md)).

**3. Try it:** in a Go or TypeScript project, ask something like *"use the
gorefactor tools to simplify calc.go"* (or `calc.ts`). Run `/mcp` in Claude Code to
see the server's status.

Setup notes:
- Claude Code reads `.mcp.json` when a session starts: one added during a
  session takes effect in the next.
- Git worktrees: the server stays on the checkout its session started in.
  After `EnterWorktree` it follows the session by asking Claude Code for
  its MCP roots on each call; a subagent with `isolation: "worktree"`
  gets the parent's roots, so there a relative path would edit the main
  checkout. The hook `cmd/worktree-paths` sends each call to the
  caller's worktree, subagents' too, and refuses paths into the main
  checkout from one: `go install
  github.com/tychofreeman/go-refactor-mcp/cmd/worktree-paths@latest`,
  then add `examples/settings.json` to the project's
  `.claude/settings.json`.
- With `GOTOOLCHAIN=auto` (the default) `go install` fetches Go 1.26 if
  yours is older; with `GOTOOLCHAIN=local` an older Go fails.
- Tool names carry the server's name as Claude Code sees it:
  `mcp__gorefactor__inspect` here, `mcp__remote-devices__gorefactor__inspect`
  through the desktop app's bridge. Instructions copied from this repo's
  CLAUDE.md name the first and speak of "this checkout"; in another
  project, name the tools without the prefix, as `examples/CLAUDE.md` does.
- `refactor-mcp -version` and `refactor -version` say which build runs:
  the release, or the commit it was built from.

`refactor list [tool...]` prints each tool's description and arguments,
as the model sees them. The server also sends the model instructions at
startup: which tool for what, and how to read and batch. It keeps one
workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it updates instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

## Help improve the tools

Which tool to build next is chosen from evidence: each time a change had to
be made by hand because no tool, or no sequence of two or three tools, made
it. If you would like to help, have Claude keep that record in your project
too, by adding this to your CLAUDE.md:

```markdown
## Friction log

When no gorefactor tool covers a step, make it by hand, as small as it can
be, and log it in `docs/friction-log.md`. Append one entry per change at
the end of the file: what you did by hand, why no tool did it (none exists,
one refused and why, or an argument was missing), and the operation that
would have done it. Log refusals that cost a turn too. Edits to files that
are not code, made with replace_text, need no entry.
```

Then open an issue with the entries, or a link to them. This repository's
own log is `docs/friction-log.md`.

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
