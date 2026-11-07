# go-refactor-mcp

**gorefactor gives the discipline of refactoring to an AI
coding assistant.** It's an MCP server with about 45 Go transformations 
(refactorings and edits), much of Fowler's catalog as type-checked tools: 
the model picks the step, and the engine checks preconditions (evaluation
order, side effects, panics, name capture), applies it everywhere with the 
type checker, and rolls back anything that doesn't compile. 

It works on Go and TypeScript: the same tools under the same names, at
near parity (roadmap item 52 tracks what still differs).

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


## What you get today, and what is not proven yet

What you get:
- **Changes that keep compiling.** Every step is type-checked before and
  after it applies; a step that breaks the build is rolled back, and a
  batch rolls back as a whole.
- **Changes that reach every use.** A rename, a signature change or a move
  is carried to every reference in the module, tests included, found by
  the type checker rather than by search.
- **Refusals instead of silent behavior changes.** A refactoring that
  could change what code does (evaluation order, side effects, name
  capture) is refused, with what to send instead; behavior changes go
  through the edit tools on purpose.
- **Undo** for every step and every batch.
- **Better design when you ask for it.** Asked to make the change easy as
  a step of its own, before the feature, a fresh model made the reference
  design in 15 of 15 runs, and 0 of 15 without that step.

What is not proven: that it saves tokens. On the tasks measured so far,
working through the tools has cost more than editing by hand, about 1.5
times on small tasks. Deriving a change through the tools has come out
ahead only where one step carries it to many uses, and behind on local
edits. The runs are in
[docs/cost-history.md](docs/cost-history.md).

Here is a step as the model sees it, applied and refused:

```
APPLIED rename: renamed field "water" to "litres" (2 occurrences in 1 file)

--- a/bed.go
+++ b/bed.go
@@ -1,8 +1,8 @@
 package garden
 
 type Plant struct {
-	Name  string
-	water int
+	Name   string
+	litres int
 }
 
 type Bed struct {
@@ -12,7 +12,7 @@
 func (b *Bed) Need() int {
 	n := 0
 	for _, p := range b.Plants {
-		n += p.water
+		n += p.litres
 	}
 	return n
 }

REFUSED inline_variable: "n" is assigned at bed.go:15:3; only variables that are never changed after their declaration can be inlined
```

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

To upgrade, run the same `go install` again, then reconnect the server in
Claude Code (`/mcp`). Releases publish tags, not prebuilt binaries.

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
you have, and put your project's name in its heading. What it asks for, and
the hooks that hold a session to it, are in "Using the tools well" below.

**3. Try it:** in a Go or TypeScript project, ask something like *"use the
gorefactor tools to simplify calc.go"* (or `calc.ts`). Run `/mcp` in Claude Code to
see the server's status.

Other MCP clients: the server speaks MCP over stdio, so any client that
starts a command can run `refactor-mcp` (with `-C` for the project
directory). The instructions and the hooks below are written for Claude
Code; other clients have not been tried.

`refactor list [tool...]` prints each tool's description and arguments,
as the model sees them. The server also sends the model instructions at
startup: which tool for what, and how to read and batch. It keeps one
workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it updates instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

## Using the tools well

The tools pay off when the model treats a change as a sequence of
operations it can name, and lets the tools carry each one to every place it
reaches. `examples/CLAUDE.md` asks for that in three parts; the hooks below
hold a session to it where the instructions alone did not. Of the
instructions we tried, these got a fresh model to work through the tools,
and derive its changes, at the lowest cost; on the tasks we tried, working
through the tools has not yet cost less than editing by hand
([docs/cost-history.md](docs/cost-history.md)).

**Refactor first, while the tests pass.** Make the change easy, then make
the easy change: refactor (rename to what a thing will mean, extract what
you will change, add the parameter or field the change needs) until the
change is small and local; then write the failing test; then the least code
that passes, even a copy; then refactor the duplication away. A refactoring
is refused while code does not compile, and a test that is red needs code,
not a refactoring. Models extend the code that is there unless told to
reshape it first.

**Work as a derivation.** Before editing, write the change as operations
that keep behaviour, ending in the one small edit that changes it, and send
them as one `batch`. Where no operation does a step, keep the hand edit as
small as it can be. Moves that have worked:
- **Near-duplicates:** extract_function from one copy, then `fold` the
  others onto it. Copies that differ only in constants: `parameterize`.
- **Room in a loop:** extract_function the loop with what it builds; the
  change is then one guard line.
- **Change a representation:** put a getter or setter in front of every
  use (add_declaration, then `fold`), change the representation inside it,
  then remove_dead_code the old field.
- **Move behaviour or change a signature (expand, migrate, contract):**
  add_declaration the new function as a wrapper over the old one, `fold`
  the callers onto it, inline_function the old one with `all_calls`, then
  rename. One parameter or result: add_parameter, remove_parameter,
  change_parameter_type or add_result in one step.
- **A test written first** gets stubs for what it calls and nothing
  declares, so the code still compiles.

More are in `docs/derivations.md` and `docs/patterns.md`.

**Use the tools economically.** Load every schema you will need in one
ToolSearch, since a guessed argument costs a refused call and a turn. Read
code with `inspect`: a file first, without bodies, then only what you will
change, by `names` or `targets`; a whole file read once is sent again on
every later turn. Don't search before a refactoring: it finds every
reference with the type checker. Address declarations by name rather than
line, use `"check": "end"` when a test comes before the code it tests, and
read refusals: they say what to send instead.

**Guards (optional hooks, Go only).** Sessions read and wrote Go by shell
out of habit even when asked to use the tools, in this repo's own work as in
fresh ones. Two PreToolUse hooks turn the habit into a refusal that names
the tool to use:
- `read-guard` refuses Read of a `.go` file, and `sed`, `cat`, `head`,
  `tail`, `awk` or `grep` naming one, and points to `inspect`. In this
  repo it ended reading Go by shell for about 4% of turns refused; for a
  fresh model in the A/B runs it saved nothing, since reads moved to more,
  smaller `inspect` calls (`docs/cost-history.md` sections 21 and 22).
- `write-guard` refuses Edit, Write and MultiEdit of a `.go` file, and
  shell commands that write one (`sed -i`, `perl -i`, `tee`, `cp`, `mv`,
  `rm`, a redirection), and points to the edits. It is an experiment, on
  in this repo since 2026-11-05 (roadmap item 53): with it, every change
  goes through the tools, and what none can make is asked of you and
  logged.

Both allow files under `testdata/`, and `gofmt -w` and git. To use them,
install them and add `examples/guards.json` to your project's
`.claude/settings.json`:

```sh
go install github.com/tychofreeman/go-refactor-mcp/cmd/read-guard@latest
go install github.com/tychofreeman/go-refactor-mcp/cmd/write-guard@latest
```

## Troubleshooting

- **The server is connected but Claude never calls it.** That is the
  default: a model edits files as it always has unless its instructions ask
  for the tools. Add `examples/CLAUDE.md` to your project's CLAUDE.md (step
  2 above); the guards make the habit a refusal.
- **The tools don't show up.** Claude Code reads `.mcp.json` when a
  session starts, so one added during a session takes effect in the next.
  `/mcp` shows whether the server started; if it didn't, check that
  `refactor-mcp` is on the `PATH` Claude Code sees, or use its absolute
  path.
- **Install fails on an older Go.** With `GOTOOLCHAIN=auto` (the default)
  `go install` fetches Go 1.26 if yours is older; with `GOTOOLCHAIN=local`
  an older Go fails.
- **Which build is running?** `refactor-mcp -version` and `refactor
  -version` say: the release, or the commit it was built from.
- **Instructions name tools that don't exist.** Tool names carry the
  server's name as Claude Code sees it (`mcp__gorefactor__inspect`).
  `examples/CLAUDE.md` names the tools without the prefix, so it works
  whatever you call the server; this repo's own CLAUDE.md does not.
- **Git worktrees.** The server stays on the checkout its session started
  in. After `EnterWorktree` it follows the session by asking Claude Code for
  its MCP roots on each call, but a subagent with `isolation: "worktree"`
  gets the parent's roots, so there a relative path would edit the main
  checkout. The hook `cmd/worktree-paths` sends each call to the caller's
  worktree, subagents' too, and refuses paths into the main checkout from
  one: `go install
  github.com/tychofreeman/go-refactor-mcp/cmd/worktree-paths@latest`, then
  add `examples/settings.json` to the project's `.claude/settings.json`.

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
refactor -n move file=main.go line=40 name=copyTree to_file=copy.go
refactor batch @steps.json        # several steps as one change, from a JSON file
refactor undo                     # undo the last step or batch
```

Long text needs no quoting: `content_file=path` (`new_file`, any `key_file`)
gives a text argument from a file, and `content_file=-` from stdin, one per
call, so a heredoc can carry it (a quoted delimiter keeps it as written, and
must not appear in the text; the heredoc's final newline is dropped, except
from `content`):

```sh
refactor add_declaration file=bed.go content_file=- <<'EOF'
func (b *Bed) Size() int { return len(b.plants) }
EOF
refactor batch - <<'EOF'
{"steps": [{"tool": "rename", "args": {"file": "bed.go", "name": "Bed.Size", "new_name": "Count"}}]}
EOF
```

For code in several steps of one batch, JSON from stdin or a file may be
followed by sections, `@@name` alone on a line and the text after it, to
the next section or the end; a string `"@name"` in the JSON is that text,
as written, so nothing in it is escaped. A section no string names, or a
string naming no section, is an error:

```sh
refactor batch - <<'EOF'
{"steps": [
  {"tool": "add_declaration", "args": {"file": "bed.go", "content": "@size"}},
  {"tool": "add_declaration", "args": {"file": "bed_test.go", "content": "@test"}}
]}
@@size
func (b *Bed) Size() int { return len(b.plants) }
@@test
func TestSize(t *testing.T) {
	if n := (&Bed{}).Size(); n != 0 {
		t.Errorf("Size() = %d, want 0", n)
	}
}
EOF
```

Exit status: `0` applied/previewed, `2` refused, `1` error.


`-C` can be any directory inside a module: the tool always loads the whole
enclosing module (nearest `go.mod`), so references in every package are
updated. `file` is resolved relative to the module root, as the tools print
paths; a path that names no file there but one under `-C` means that one.
A new file (`create_file`) goes at the module root.
References in *other* modules (e.g. siblings in a `go.work`) are not
updated; exported renames add a note saying so.

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

## Contributing

How the server is laid out, the design decisions behind it, its tests
and stress tests, how the A/B runs work, and how to add a refactoring are
in [CONTRIBUTING.md](CONTRIBUTING.md). What each refactoring checks before
it applies is in `docs/checks.md`.
