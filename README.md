# gorefactor

Behavior-preserving Go refactorings exposed as tools for an LLM (Claude or a
local model through Ollama). The model decides *what* to change; this tool
guarantees *how* it's changed safely, one step at a time.

```
model ──(tool call: rename {...})──▶ engine: check preconditions
                                             apply byte-range edits
                                             re-type-check, roll back on error
model ◀──(APPLIED + diff | REFUSED + reason)──┘
```

Each step returns a unified diff (or a refusal explaining why), which the
model reads before choosing the next refactoring. That loop is the iteration.

## Status

- **Step 1:** workspace loading, edits, diffs, undo, the safety engine, a
  type-aware `rename`, golden tests and a CLI.
- **Step 2:** MCP server (`cmd/refactor-mcp`) with `rename`, `inspect`,
  `undo` and `check`, usable from Claude Code.

Next: `extract_variable`, `inline_variable`, `invert_if` (guard clauses),
`extract_function`; then an Ollama loop.

## Layout

```
cmd/refactor/                 CLI: one refactoring step per invocation
cmd/refactor-mcp/             MCP server (stdio) for Claude Code and other MCP clients
internal/
  edit/                       TextEdit/FileEdit, Apply, unified diff (Myers)
  inspect/                    module overview, file outline + complexity, numbered source
  mcpserver/                  MCP tools: one per refactoring, plus inspect, undo, check
  workspace/                  go/packages loading (incl. tests), apply, undo, errors
  locate/                     file+line+name (+column) → *ast.Ident
  refactor/                   Refactoring interface, Refusal, Run (the safety engine)
    refactortest/             golden-test harness shared by all refactorings
  refactorings/
    registry.go               the list of tools
    rename/                   rename + testdata/<case>/{in,out,args.json,refused.txt}
```

## Getting started

```sh
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

If `refactor-mcp` is not on the `PATH` Claude Code sees, use its absolute
path as the command. The server works on the module containing `-C`, or
`$CLAUDE_PROJECT_DIR` (which Claude Code sets), or its working directory.

Then, in a Go project, ask something like *"use the gorefactor tools to
simplify calc/calc.go"*. Run `/mcp` in Claude Code to see the server's status.

Tools:

| Tool | What it does |
|---|---|
| `inspect` | No args: module overview. `file`: outline (line ranges, complexity, nesting) + numbered source. `function` or `start_line`/`end_line` to narrow. |
| `rename` (and every future refactoring) | Applies one step; returns `APPLIED` + diff, or `REFUSED` + reason (nothing changed). |
| `undo` | Reverts the last applied step; repeatable. |
| `check` | Type-check status; `run_tests: true` also runs `go test`. |

The server sends instructions to the model at startup (one refactoring per
call, re-inspect after each step, stop when nothing clearly improves). It
keeps one workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it reloads instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

## Using the CLI

The CLI runs one step with the same engine and arguments as the MCP tools:

```sh
refactor list
refactor -C path/to/module -n rename file=main.go line=7 name=t new_name=total   # preview
refactor -C path/to/module    rename file=main.go line=7 name=t new_name=total   # apply
refactor rename '{"file":"main.go","line":7,"name":"t","new_name":"total"}'      # JSON args, as a model sends them
```

Exit status: `0` applied/previewed, `2` refused, `1` error.

`-C` can be any directory inside a module: the tool always loads the whole
enclosing module (nearest `go.mod`), so references in every package are
updated. `file` is resolved relative to `-C` first, then to the module root.
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
  happening.
- **Refusals are results, not errors.** A `*refactor.Refusal` is written for
  the model: what is wrong and what to try instead. Internal failures are
  ordinary errors.
- **Model-friendly addressing.** Symbols are found by `file` + `line` + `name`;
  `column` is only needed when the same name means different things on one
  line, and the refusal says which columns to choose from. No byte offsets.
- **One tool per refactoring** (when the MCP server lands), each with its own
  JSON schema and a description that says *when* to use it. Unknown argument
  fields are rejected so a misspelled parameter isn't silently ignored.
- **Test variants.** Packages are loaded with tests, so a file can appear in
  `p` and `p [p.test]`. Symbols are identified by declaration position, and
  edits are deduplicated, so references in `_test.go` files are updated too.

## Rename: what it checks

Supported: local and package-level variables, constants, functions, types,
type parameters, struct fields, methods, type-switch variables; generic
instantiations; references across packages and in test files.

Refused (with an explanation):

- the new name is already declared in the same scope, or clashes with an import
- a reference would be **shadowed** by an inner declaration of the new name
- the renamed symbol would **capture** an existing use of the new name
  (including builtins like `len` and imported package names)
- a field/method clash on the type, including promoted members via embedding
- a method that makes a type satisfy an interface, or whose name is commonly
  called dynamically (`String`, `Error`, `MarshalJSON`, …)
- unexporting a name that another package uses
- symbols declared outside the workspace (stdlib, dependencies)

Not supported yet: imports, labels, embedded fields (and types that are
embedded), interface methods. Comments mentioning the old name are not
changed; the result includes a note listing them so the model can decide.

## Adding a refactoring

1. Create `internal/refactorings/<name>/` implementing `refactor.Refactoring`.
   `Plan` must not write anything; return `refactor.Refusef(...)` for every
   precondition failure.
2. Add golden cases under `testdata/<case>/`: `in/` (a complete module with
   `go.mod`), `args.json`, and either `out/` or `refused.txt`. Generate `out/`
   with `go test ./internal/refactorings/<name>/ -update` and review the diff.
   The harness also checks that undo restores `in/` exactly.
3. Register it in `internal/refactorings/registry.go`. It then appears as an
   MCP tool (named `Name()`, described by `Description()`) and in the CLI
   automatically.
