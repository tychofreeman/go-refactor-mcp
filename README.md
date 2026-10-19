# go-refactor-mcp

Behavior-preserving Go refactorings, and edits that change behavior on
purpose, exposed as tools for an LLM (Claude or a local model through
Ollama). The model decides *what* to change; this tool guarantees *how*
it's changed safely, one step at a time. The aim is to do nearly all coding
through these typed steps instead of editing files by hand.

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
- **Step 3 (in progress):** refactorings that simplify code.
  `extract_variable`, `inline_variable` and `extract_function` are done
  (`extract_function` with parameters and results, `return` statements,
  pointers for shared variables, methods, imports, local constants and
  types, and generics). `invert_if` (with guard clauses) and `remove_else`
  are done. `inline_function` has a first version (see the roadmap).
- **Step 4 (in progress):** refactorings that prepare a feature change.
  `add_parameter`, `remove_parameter`, `move_parameter` and
  `introduce_parameter` are done, for functions and methods, exported or
  not. `method_to_function` turns an unexported method into a function,
  and `function_to_method` a function into a method of its first
  parameter's type. `extract_struct` moves fields into a new, embedded
  struct type, `merge_duplicate_function` replaces a function with an
  identical one, and `remove_dead_code` deletes a declaration nothing
  uses. `move_statement_up` and `move_statement_down` swap a statement
  with its neighbor when the two are independent. `move_declaration` moves a
  declaration (with the helpers it uses, if asked) to another file, of its
  package or another one.
- **Step 5 (in progress):** edits, steps that change behavior on purpose
  (see "Edits"). `create_file`, `replace_statements`, `insert_statements`,
  `add_declaration`, `replace_expression` and `add_field` are done; the
  candidates are in `docs/edits.md`.

## Roadmap

The goal is a workflow for AI-driven development: during a story, Claude
picks the refactorings that make the change easy, the tools perform them
deterministically, and Claude makes the small edit that adds or removes the
feature. The order below follows what that workflow needed in practice;
`docs/friction-log.md` records each time a refactoring had to be done by
hand or a tool got in the way.

1. **Changing a signature** at every call site. The most common by-hand
   step so far. *`add_parameter`, `remove_parameter` and `move_parameter`
   done, for exported methods too (see "Add parameter", "Remove parameter",
   "Move parameter: what it checks" and "Changing a method: interfaces").
   `introduce_parameter` turns an expression of the body into a parameter
   (see "Introduce parameter: what it checks"), also through the receiver
   or a pointer read at entry; next: every occurrence at once, and calls
   on paths that return before the expression.
   Left: methods of generic types that an interface could match, and
   interface methods with all their implementations.*
2. **Moving a declaration** to another file or package, updating its
   references (and renaming it on the way when it changes package).
   *`move_declaration` done, within a package and to another one (see
   "Move declaration: what it checks"), with several declarations at once,
   from any files of the package (`also`), out of a grouped declaration,
   a type with its methods, and into a new package.*
3. **Evidence from real use.** Keep the friction log during normal work.
   Later, a comparison: the same small stories done with and without the
   tools, comparing the size of the hand-written edits, test failures and
   review effort. `refactor-eval`'s metrics and throwaway copies can carry
   over (see "Measuring whether the tools help"); its first runs showed that
   complexity alone rewards bad extractions, and that applied counts must
   subtract undos.
4. **`inline_function`:** the error-exit shape
   (`x, err := f(); if err != nil { return ..., err }`), generic and variadic
   functions, inlining every call at once, and callees from other packages
   (see "Inline function: what it checks").
5. **Statement refactorings**, the behavior-preserving forms of edits in
   `docs/edits.md`: *sliding a statement up or down past an independent
   one is done (see "Move statement up and down: what they check"), and
   it knows which of the module's own functions have no side effects (see
   "Calls of the module's own functions"); next: the other refactorings
   using those summaries too.* Then: introduce a plain block, and move a
   statement into or out of one; and
   statements with no effect for `remove_dead_code` (unused declarations
   are done). Deleting a statement for its
   effect is the edit `delete_statement`.
6. **`rename` gaps:** imports, labels, embedded fields and interface methods
   (see "Rename: what it checks").
7. **Packaging:** tagged releases, so `go install ...@latest` gets a stable
   version, and possibly a Claude Code plugin for a one-step install.
8. **Test run time.** CI's test step grew from about 13 s to 75 s per Go
   version between Oct 16 and Oct 19, in step with the golden cases (132 to
   824): about 9 s per 100 cases under `-race`. Each case loads a module
   with `packages.Load`, so the cost is per case, not per assertion. In
   order:
   - **Measure** per package and per test (`go test -json`), so the rest
     is decided on numbers. *Done: each CI job reports its slowest
     packages, tests and golden cases in the job summary and as a notice
     annotation (`.github/test-times.sh`).*
   - **Next: make the fresh-load comparison cheaper.** `refactortest.Run`
     compares the workspace with a fresh load after applying and after
     undoing each applied case: 1,725 loads for today's cases instead of
     811. It is over half the test time: on Oct 19, CI's package times
     summed to 267 s with it and 122 s with `-short`, which skips it. It
     must keep checking both points, since skipping either could hide an
     incremental-update bug. The aim is the same check for less work, for
     example by not type-checking the standard library again for each
     fresh load. The CI test-times report measures the result.
   - **`-race` costs little,** about 5% of package time, within run-to-run
     noise, so it stays in both CI jobs.
   - **Tests that start `go test`:** `mcpserver.TestRefactoringLoop`
     (`check` with `run_tests`) and `agent.TestRunUndoesWhenTestsBreak`.
   - **Repeated tests.** Each costs a load or two, so these come last:
     `mcpserver` tests that repeat `refactor`'s checks of batches, compact
     diffs and broken input (keep only the wiring); the six
     `refuse_type_error` cases of the edits, which all test
     `refactor.Run`'s check; and undo after an external edit, tested in
     both `workspace` and `mcpserver`.
   - **Not for speed:** the stress tests copy `envInt`, `tailLines` and
     their candidate loop six times (`inlinefunc` has them factored out
     already), `inlinevar`'s `copyDir` is `refactortest.copyTree`, and
     `extractvar.TestTokens` tests `locate`.

Alongside: **edits**, steps that change behavior on purpose, so the
change itself is a tool call too (Step 5 above); `docs/edits.md` lists the
candidates and the evidence for each, gathered from the friction log.

Parked: the Ollama loop (`refactor-agent`). It works and stays, but Claude
through MCP is the priority.

## Layout

```
cmd/refactor/                 CLI: one refactoring step per invocation
cmd/refactor-mcp/             MCP server (stdio) for Claude Code and other MCP clients
cmd/refactor-agent/           a local model (Ollama) refactoring a module with the same tools
cmd/refactor-eval/            runs the agent on a module's most complex functions and measures the results
cmd/refactor-tokens/          where Claude Code sessions spent their tokens, by tool (MCP vs by hand)
cmd/refactor-ab/              runs tasks (eval-tasks/ab.json) with claude -p through the MCP and by hand; compares cost and results
internal/
  agent/                      the model loop: in-process MCP client, Ollama client, transcript
  eval/                       evaluation: pick targets, run the agent on copies, summarize
  ab/                         the A/B harness: fresh copies, claude -p per arm, checks, summary
  tokens/                     per-tool token cost from Claude Code transcripts
  metrics/                    per-function complexity, nesting and length, and summaries
  analysis/                   shared facts: side effects, stable expressions, escaping/modified locals
  edit/                       TextEdit/FileEdit, Apply, unified diff (Myers)
  textutil/                   token scanning and re-indenting for moved source text
  imports/                    a file's import names; adding the imports moved code needs
  inspect/                    module overview, file outline + complexity + side effects, numbered source
  mcpserver/                  MCP tools: one per refactoring, plus inspect, undo, check
  workspace/                  go/packages loading (incl. tests), apply, undo, errors
  locate/                     file+line+name (+column) → *ast.Ident
  refactor/                   Refactoring interface, Refusal, Run (the safety engine)
    refactortest/             golden-test harness shared by all refactorings
  refactorings/
    registry.go               the list of tools
    rename/                   rename + testdata/<case>/{in,out,args.json,refused.txt}
    extractvar/               extract_variable + testdata + opt-in stress test
    inlinevar/                inline_variable + testdata + opt-in stress test
    extractfunc/              extract_function + testdata + opt-in stress test
    inlinefunc/               inline_function + testdata + opt-in stress and round-trip tests
    invertif/                 invert_if, remove_else + testdata + opt-in stress test
    changesig/                add/remove/move_parameter + testdata/<tool>/
    movedecl/                 move_declaration + testdata
    slidestmt/                move_statement_up/down + testdata/{up,down}/ + opt-in stress test
  edits/
    createfile/               create_file + testdata
```

## Getting started

Install the MCP server and the CLI:

```sh
go install github.com/tychofreeman/go-refactor-mcp/cmd/refactor-mcp@latest
go install github.com/tychofreeman/go-refactor-mcp/cmd/refactor@latest
go install github.com/tychofreeman/go-refactor-mcp/cmd/refactor-agent@latest   # optional: local models
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

If `refactor-mcp` is not on the `PATH` Claude Code sees, use its absolute
path as the command. The server works on the module containing `-C`, or
`$CLAUDE_PROJECT_DIR` (which Claude Code sets), or its working directory.

Then, in a Go project, ask something like *"use the gorefactor tools to
simplify calc/calc.go"*. Run `/mcp` in Claude Code to see the server's status.

Tools:

| Tool | What it does |
|---|---|
| `inspect` | No args: module overview. `file`: outline (line ranges, complexity, nesting, side effects) + numbered source. `function` or `start_line`/`end_line` to narrow. |
| `rename`, `extract_variable`, `inline_variable`, `extract_function`, `inline_function`, `invert_if`, `remove_else`, `add_parameter`, `remove_parameter`, `move_parameter`, `move_declaration` (and every future refactoring) | Applies one step; returns `APPLIED` + diff, or `REFUSED` + reason (nothing changed). |
| `create_file` (and every future edit) | Applies one step that may change behavior; like a refactoring, it must leave code that type-checks. Returns `APPLIED` + diff, or `REFUSED` + reason (nothing changed). |
| `undo` | Reverts the last applied step; repeatable. |
| `check` | Type-check status; `run_tests: true` also runs `go test`. |

The server sends instructions to the model at startup (one refactoring per
call, re-inspect after each step, stop when nothing clearly improves). It
keeps one workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it updates instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

## Using a local model (Ollama)

`refactor-agent` lets a model served by [Ollama](https://ollama.com) refactor a
module with the same tools. It starts the MCP server in-process and connects
to it as a client, so the tools, their descriptions and the instructions are
exactly what Claude Code sees.

```sh
ollama pull qwen2.5-coder:14b      # any model with tool calling
refactor-agent -C path/to/module -model qwen2.5-coder:14b -file calc/calc.go -tests
refactor-agent -model qwen2.5-coder:14b -goal "Reduce nesting in parse in calc/calc.go" \
  -revert -transcript run.jsonl   # try it without keeping the changes
```

The model inspects the code, applies one refactoring per turn and stops when
nothing clearly improves (or after `-max-steps` tool calls, or
`-max-failures` failed or refused calls in a row). Every step type-checks and
is undoable. With `-tests`, `go test ./...` runs before and after; if it
passed before and fails after, every step is undone (unless `-keep`).
`-revert` undoes everything at the end, to try a model without keeping its
changes. `-timeout` (default 2h) stops asking the model; local models can
take minutes per turn, and the clean-up (tests, `-revert`) still runs after
it. If an undo fails, the summary says so and the exit status is 1. The run
ends with a summary and the combined diff; `-transcript` writes every model
message (with how long it took) and tool call as JSON lines.

Small models need help with tool calling, so the agent:

- runs only the first tool call of a turn (the others get "NOT RUN": line
  numbers may be stale after a change);
- fixes argument types the schema makes unambiguous (`"12"` for an integer,
  a JSON object sent as a string), and nothing else;
- sets the context window with `-ctx` (default 16384; Ollama's own default is
  too small for `inspect` output).

`-host` defaults to `$OLLAMA_HOST`, then `http://localhost:11434`; `-model`
defaults to `$OLLAMA_MODEL`.

## Measuring whether the tools help

`refactor-eval` runs the agent loop on the most complex functions of a module
and measures what happened. Each run works on a fresh copy of the module, so
the module itself never changes:

```sh
refactor-eval -C path/to/module -model qwen3.5:9b -n 5            # the 5 most complex functions
refactor-eval -model qwen3.5:9b -target internal/eval/eval.go:Markdown -max-steps 10
```

For each target the model is asked to simplify that one function. The
output directory (default `eval-runs/<model>-<time>`) gets:

- `results.jsonl`: one line per run, written as each run ends: the target's
  complexity and nesting before and after, the same summed over the changed
  files, tests before and after (with `-tests`, on by default), applied and
  refused calls per tool, and every refusal or error with its reason;
- `summary.md`: overall outcomes (runs that changed code, tests broken,
  targets improved or made worse, mean changes), a row per run, tool usage,
  and the most common refusal and error reasons (numbers replaced by `N`, so
  reasons that differ only in line numbers group together);
- `NN-<file>-<func>.jsonl` and `.diff`: the run's transcript and its change.

`-timeout` limits each run (default 45m). `refactor-agent` prints the same
before/after numbers for the files it changed.

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
  JSON schema and a description that says *when* to use it. Unknown argument
  fields are rejected so a misspelled parameter isn't silently ignored.
- **Test variants.** Packages are loaded with tests, so a file can appear in
  `p` and `p [p.test]`. Symbols are identified by declaration position, and
  edits are deduplicated, so references in `_test.go` files are updated too.


### Calls known to have no side effects

A call can do anything, so most checks treat one as a side effect. A
vetted list (`analysis.KnownCall`) names functions that have none and whose
result depends only on their arguments (strings, numbers, runes) and on
receiver state that never changes: most of `strings` on strings,
`unicode`, `unicode/utf8`, `math`, the lexical `path` and `path/filepath`
functions, `strconv`'s formatting, the `go/token` predicates, and getters of
`go/types` objects such as `Object.Name` and `Package.Path`. Each says how
it can panic: never (`strings.ToUpper`), only through a nil receiver (the
`go/types` getters), or otherwise (`strings.Repeat` with a negative count).

A known call that cannot panic has no effect and gives the same value
wherever it is evaluated, so `add_parameter` can pass `strings.ToUpper(name)`,
`extract_variable` can replace every `strings.ToLower(s)`, and
`inline_variable` can repeat one. One that can panic counts as a panic, of
the kind it is (so `introduce_parameter` accepts a getter at entry, like a
read through a pointer).

Left out on purpose: functions that return a new error (`strconv.Atoi`:
two calls give errors that are not `==`), that call their arguments'
methods (`fmt`), that read memory other code can change (`bytes`,
`strings.Join`, anything taking a slice; also any call spreading a slice,
`f(xs...)`), and methods of `go/ast` nodes, whose fields can be assigned.

### Calls of the module's own functions

`analysis.Effects` summarizes a call of a function declared in the
workspace from its body, in three parts: whether it has a side effect,
whether it reads what other code can change, and whether it could panic or
not finish. Each part names the first reason, so a refusal can say
"calls `bump`, which assigns to `count`".

- **Side effects:** assigning to a package variable, through a pointer or
  parameter (`xs[0] = 1`, `c.n++` with a pointer receiver), a channel
  operation, `go`, and calls with side effects. Assigning to its own
  locals, value parameters and value receivers is not one; nor is writing
  through a local that only holds memory the function made itself
  (`make`, `new`, a composite literal or its address, an `append` to it),
  so building and returning a slice or map has no side effect.
- **Reads:** package variables, memory through pointers, and elements of
  slices and maps it did not make.
- **Panics:** indexing, slicing, dereferencing, type assertions without
  `ok`, integer division by a variable, `panic`, and `for` loops (which
  might not finish).
- **Calls** count what the callee does, recursively. A call of a function
  value, an interface method, a recursive call, or a standard library
  function not on the lists counts as a side effect. Function literals
  passed to `ast.Inspect`, `sort.Slice` and similar functions that call
  them before returning are summarized as part of the caller.
  `errors.New` and the `strconv` parsers have no side effects (but are not
  known calls: their errors are new values).
- **Standard library** (`internal/analysis/stdlib.go`): a table, by full
  name and including interface methods, of what common functions read,
  write and how they panic. Reading the file system or the environment
  (`os.ReadFile`, `os.Stat`, `filepath.Abs`) is a read, which only a side
  effect can disturb; `strings.Join` and `slices.Contains` read their
  slice; `sort.Strings` writes its argument; `strings.Builder` and
  `bytes.Buffer` methods write their receiver; the `go/types` getters, the
  `go/token` position methods and every `go/ast` node's `Pos` and `End`
  read at most their receiver. `fmt.Sprint*` and `Errorf` have no side
  effect unless an argument is an interface or has a `String`, `Error`,
  `Format` or `GoString` method; `fmt.Fprint*` writing to a local
  `strings.Builder` writes only that. Inside a function, a write to its
  own local (`sb.WriteString` on a local builder, `sort.Strings` on a
  fresh slice) is not a side effect.

For now only `move_statement_up` and `move_statement_down` use these
summaries; the other refactorings still treat every call not on the
known list as a side effect. The function literals passed to
`ast.Inspect` and the like run during their statement, so
`move_statement_*` counts what they do as part of it.

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
embedded), interface methods.

Comments: the doc comment of a renamed declaration (function, method,
type, variable, constant or field) is renamed too when it starts with the
old name, as Go's convention has it (`// helper doubles n.` becomes
`// twice doubles n.`). Other comments mentioning the old name are not
changed; the result notes where they are, so the model can decide, and for
a local variable only the comments in its scope count.

## Extract variable: what it checks

`extract_variable` inserts `name := <expression>` before the statement that
contains the expression and uses `name` in its place. The expression is
identified by `file`, the `line` it starts on and its source text (whitespace
may differ; `column` disambiguates repeats on one line). `all_occurrences`
also replaces identical occurrences later in the same block.

What it produces:

- constant expressions become `const` (so untyped constants stay untyped:
  `n * (1 << 10)` → `const kib = 1 << 10` and `n * kib`);
- when `:=` would infer a different type than the context gave the
  expression, it declares the type: `var m uint8 = 1 << n` →
  `var bit uint8 = 1 << n`, not `bit := 1 << n` (which would be an `int`);
- an extraction from an `if`/`switch` init, `if` condition or `range`
  expression is placed before that statement.

Refused (with an explanation), because hoisting the expression could
change behavior:

- it is evaluated conditionally or repeatedly: right operand of `&&`/`||`,
  `for` condition or post statement, `else if`, `case` expression, `select` case
  (constants are exempt);
- it would move ahead of a call or channel receive earlier in the same
  statement (`read() - read2()`), unless its value depends only on constants
  and local variables that nothing else can reach;
- it would move ahead of an assignment or `++`/`--` earlier in the same
  statement that could change its value (`!ok` in
  `if v, ok = x.(T); !ok`): with only unreachable local variables, when
  one of them is assigned; otherwise always;
- it is assigned to, has its address taken, is sliced (arrays), or has a
  pointer-receiver method called on it: a new variable would be a copy;
- it uses a variable declared by the statement itself (`if v := f(); v > 3`);
- it is a type, package, builtin, `nil`, a void or multi-value call, a whole
  expression statement, a `defer`/`go` call, uses `iota`, or is a generic
  function whose type arguments are inferred;
- the new name is already declared in the block, would hide a name used later
  in the block, or is itself hidden at a replacement site;
- a `goto` jumps forward over the new declaration.

`all_occurrences` additionally requires an expression with no calls (other
than conversions, `len`/`cap`/`min`/`max`, and the known functions without
side effects that cannot panic, such as `strings.ToLower`; see "Calls
known to have no side effects"), no pointer, slice or map
element reads, no globals, no new slices or maps, and no local variable that
escapes (address taken, captured by a closure) or is modified after the
declaration. Without `all_occurrences`, the result notes other occurrences
that could be replaced.

Known limitation: if both the hoisted expression and an earlier part of the
same statement would panic, which panic happens first can change (e.g. a
division by zero instead of a nil dereference). Non-panicking behavior is
preserved.

## Inline variable: what it checks

`inline_variable` replaces every use of a local variable or constant with
its initializer and deletes the declaration (`x := e`, `var x [T] = e`,
`const x [T] = e`, also from an `if`/`switch` header or a grouped `var (...)`).
Identify it by `file`, `line` and `name`, at the declaration or any use.

What it produces:

- parentheses where grouping requires them: `sum := w + h; sum * 2` →
  `(w + h) * 2`; `c - d` with `d := a - b` → `c - (a - b)`; a composite literal
  in an `if` header → `if p == (Pt{0, 0})`;
- a conversion where the variable's type differs from the expression's own:
  `var scale float64 = 1; scale / 3` → `float64(1) / 3` (not integer
  division), `n := 7; n / 2.0` → `int(7) / 2.0`,
  `var r io.Reader = os.Stdin; r == nil` → `io.Reader(os.Stdin) == nil`; no
  conversion where the context already gives the type (`io.Copy(w, os.Stdin)`).

Refused (with an explanation):

- the variable is assigned anywhere, its address is taken, it is sliced, or a
  pointer-receiver method is called on it;
- its initializer is not stable (it has calls, or reads globals, pointers,
  slice/map elements or escaping variables) and the variable is used more than
  once, or its one use is not evaluated exactly once right after the
  declaration: inside a loop or closure, in a nested block, after `&&`/`||`, in
  an `else if`, or with a call, write or `return` in between;
- a variable the initializer reads is modified before a use (or anywhere in a
  loop or closure that contains a use but not the declaration);
- a name in the initializer would refer to something else at a use;
- a constant would land somewhere Go treats constants differently: a
  conversion that overflows (`int8(x)` with `x := 200`) or a division by 0;
- parameters, range and type-switch variables, `for` headers, variables
  declared together (`a, b := ...`), without an initializer, at package level,
  or never used.

A comment directly above the removed declaration is kept, and the result notes
it so the model can decide whether to remove it.

## Extract function: what it checks

`extract_function` moves a run of complete statements into a new function,
declared after the enclosing top-level declaration, and calls it in their
place. Identify the statements by `file`, `start_line` and `end_line`
(inclusive, as `inspect` numbers them); `doc`, if given, becomes the new
function's doc comment (`//` is added to each line that lacks it). The
original text moves, so comments
come along; it is re-indented, except inside multi-line raw strings and
comments. The result notes the parameters and results chosen, and warns
when there are many (more than five parameters or three results): a
selection that cuts through code sharing many variables gives a function
no one would write, and a different range usually reads better.

The lines must cover whole statements of one block (a function body, a `{}`
block, or one `case`); comments and blank lines in the range are allowed.
Selecting all of an `if` extracts the `if`; selecting only its body extracts
the body. A range that cuts a statement or crosses a block boundary is
refused, and the refusal lists the statement lines nearby.

Local variables the statements use become parameters, in order of first
use. Variables they change and that are used afterwards, and variables they
declare and that are used after them, become results, assigned at the call
site:

```go
total := 0                         total := 0
for _, x := range xs {             total = sumDoubled(xs, total)
    total += x             =>      fmt.Println(total)
}                                  ...
total *= 2                         func sumDoubled(xs []int, total int) int {
fmt.Println(total)                     for _, x := range xs { total += x }
                                       total *= 2
                                       return total
                                   }
```

- A variable the statements assign before reading it is declared in the new
  function rather than passed in.
- Results declared by the statements use `:=` at the call site; when some
  results already exist, the new ones are declared with `var` first, since
  `:=` could shadow a variable from an outer scope.
- "Used afterwards" is conservative: any use outside the selection, a named
  result (a bare `return` reads it), or being read by the statements when
  they are in a loop. A result kept only because of the loop is noted.
- A variable that other code can reach while the statements run (its
  address is taken, a pointer method is called on it, an array of it is
  sliced, or a function literal uses it) is passed as `&v`, so both
  functions work on the same variable; it is never a result. Inside the
  new function, `&v` becomes `v`, `v.f` and `v.M()` stay as they are, and
  other uses become `*v`, or `(*v)` where Go does not dereference
  (`(*v)[i]`, `(*v)(args)`, methods of interfaces). For example,
  `json.Unmarshal(data, &cfg)` moves as `json.Unmarshal(data, cfg)`.
- If the statements use the receiver of the method they are in, the new
  function is a method of the same type, called as `c.bump()`; generic
  receivers (`(s *Stack[T])`) keep their type parameters. A value receiver
  whose address is taken gets a pointer receiver. The name must be
  unexported, must not clash with a field or method (including promoted
  ones), and must not make the type, or a type embedding it, implement an
  interface of the package it did not implement before. Statements inside
  a function literal in a method become a function instead.
- A variable that function literals use but nothing ever assigns cannot
  change, so it is passed by value, not by pointer. So is one that only
  synchronous function literals use: ones called on the spot
  (`func() { ... }()`, but not with `go` or `defer`), or passed directly to
  a function known to call them only before it returns and not keep them
  (`sort.Slice`, `ast.Inspect`, `slices.SortFunc`, `strings.FieldsFunc`,
  `filepath.WalkDir`, ...). Such a literal outside the selection runs
  before or after the call, never during it, and one inside it uses the
  new function's copy. `sort.Slice(out, func(i, j int) bool { ... })`
  elsewhere in the function no longer makes `out = addAll(in, out)` a
  pointer parameter.
- Local constants and types the statements use but do not declare (or
  declare and code after them uses, or that appear in the new function's
  signature) move to package level, just above the enclosing declaration,
  so both functions share one declaration: a copied type would be a
  different type, and a constant passed as a parameter would stop being a
  constant. Local constants and types they depend on move with them.
- If the statements (or the variables they pass in and out) use the
  enclosing function's type parameters, the new function declares all of
  them, with their constraints as written, and the call instantiates them
  explicitly: `add[T](xs, total)`. Nothing depends on type inference.
- If a parameter or result type needs a package the file does not import,
  the import is added (with an alias such as `b2` when the package name is
  already used in the file or package), and the result notes it.
- Writes through slices, maps and pointers need no result; arrays and
  structs are values, so a changed one is returned.

`return` statements in the selection would return from the new function,
so the call site passes them on. Two shapes are supported:

- **Every path returns** (or ends in another terminating statement, such as
  `panic`): the new function gets the enclosing function's results, the
  statements move unchanged, and the call site becomes `return f(...)`.
- **Early exits with an error**: the enclosing function's last result is an
  `error`, and every `return` in the selection returns an error known to be
  non-nil, with the same constant-like values for the other results. The
  new function returns its own results plus an `error`:

  ```go
  f, err := open(path)               data, err := openAndRead(path)
  if err != nil {                    if err != nil {
      return nil, err        =>          return nil, err
  }                                  }
  data := read(f)                    return data, nil
  return data, nil
  ```

  Inside it, the early returns become `return <zero values>, err`, and
  falling off the end returns `..., nil`. An error counts as non-nil when
  the return is inside `if v != nil` (possibly `&&` other conditions) with no
  write to `v` in between, when it comes from `errors.New` or `fmt.Errorf`,
  when it is a non-pointer value or `&T{...}`, or when it comes from a
  function in the loaded source (in any package of the module) whose every
  `return` passes such a value, like a `refusef` helper that returns
  `&Refusal{...}`. Such a function must have unnamed results (a deferred
  function could change named ones), and calling itself proves nothing.
  Interface methods and function values never count. With no other
  results, the call site declares the error in the `if` that checks it
  (`if err := f(...); err != nil {`), where it hides nothing; otherwise it
  uses `err`, or `err2` and so on if `err` is taken.

  When the selection has no other results and is followed (in its block,
  with no comment in between) by `return <the same values>, nil`, the call,
  its check and that return fold into one: `return f(...)`, or
  `return 0, f(...)` for exits that return `0, err`. Whatever the new
  function's error, that returns what the old code did.

Other returns are refused: an early return of a success value (`return v,
nil`) looks the same to the caller as falling off the end. So are returns
that use the enclosing function's named results, bare returns in a function
with results, and `return f()` of a multi-value call in the early-exit
shape.

Refused (with an explanation):

- a variable the statements declare has its address taken or is used by a
  function literal that can run later (one stored, deferred, or passed to
  a function not known to call it only before returning), and is used
  after them: returning it would return a copy (some types, like
  `strings.Builder`, also refuse to be copied). A literal that only runs
  during its statement, like an `ast.Inspect` visitor or a `sort.Slice`
  comparison, has finished when the new function returns, so its
  variables are returned like any other;
- a `:=` in the statements redeclares a variable passed as a pointer;
- the function has a type parameter named `_`, or one whose name a local
  type hides at the call site, and the statements use its type parameters;
  or the statements are in a function literal inside a generic method and
  use the receiver's type parameters;
- a local constant or type has to move to package level (see above) but
  cannot: it is part of a grouped declaration, uses a local variable or
  type parameter, its name is taken at package level or by an import in
  any file, or some use of it would then refer to something else;
- a type needs a package that cannot be imported here (an `internal`
  package of another tree, or a `main` package), or a package name the new
  code writes is hidden where it is written (by a parameter, or by a local
  at the call site);
- a statement redeclares a name that is also passed in;
- a `return` that does not fit the two shapes below, or `defer`, `recover()`
  or `fallthrough` in the selection (outside function literals);
- a `break`, `continue` or `goto` that leaves the selection, or a jump from
  outside to a label inside it;
- the selection ends with what makes a function with results end without
  a `return` (a `panic`, a `for` without condition, ...): a call to the new
  function is not a terminating statement, so the code would not compile;
- the name is invalid, `init`, predeclared (`len`), already declared in the
  package (including its test files), an import name, hidden at the call site
  by a local declaration, or in a `_test.go` file looks like a test
  (`TestX`, `BenchmarkX`, ...).

## Inline function: what it checks

`inline_function` replaces a call with the body of the function it calls,
and deletes the function when nothing else uses it. It is the inverse of
`extract_function`. Identify the call by `file`, `line` and the called
function's `name` (`column` only if the name appears more than once on the
line).

What replaces the call depends on how it is used:

- **a statement,** `f(a)`: the body's statements;
- **an assignment or declaration,** `x, err := f(a)`, `x = f(a)`,
  `var x = f(a)`: the body's statements, then the returned values assigned.
  When a returned value is a local the body declares at its top level, that
  local becomes the variable: `a, b := pair()` comes back as the statements
  that declared `a` and `b`, not as `a, b := a, b`. This also works for an
  existing variable (`err, key = f(k)`): the body's `var key T` is dropped
  when the body assigns it before reading it, its `:=` becomes `=`, or, if
  the caller declared `var key T` just for the call, that declaration goes
  and the body's declares it instead;
- **a return,** `return f(a)`, when the result types are identical, or a
  call the caller returns right after: the body as it is, returns included,
  so only here may the body return early;
- **part of an expression,** `g(f(a)) + 1`, when the body is a single
  `return e`: `e`, converted to the result type and parenthesized where
  needed.

Parameters are replaced by their arguments when that cannot change what the
body sees, and otherwise declared at the top with the arguments' values
(`p := arg`, one statement for all, which keeps the evaluation order):

- **replaced:** a constant used as a whole value; an expression that cannot
  change (locals no other code reaches, no calls); a variable read when the
  body changes nothing before using it, or the variable is never assigned;
  `&v` (uses of `*p` and `p.f` become `v` and `v.f`); a parameter the body
  assigns, if its argument is a variable of the same name that the caller
  assigns the result back to (`v = step(v)`) or never reads again; and, in
  an expression, an argument with calls used exactly once, unconditionally,
  in argument order;
- **declared:** everything else, such as an argument with calls, or a
  parameter the body assigns or captures in a closure while the argument
  could still change. Unused parameters drop their argument unless it has
  effects (`_ = arg`).

Refused:

- **the callee:** generic or variadic; from another package; an interface
  method, method expression or promoted method; a function value; using
  `defer` or `recover`, which belong to its own frame; named results;
  returning early outside the return shape; labels that clash with the
  caller's;
- **names:** a name the body uses that would mean something else at the
  call (a package name the caller's file spells differently is renamed, and
  a missing import is added); a name the inlined code declares that the
  calling block already declares, or that would hide a name used later in
  it; any declaration when the caller uses `goto`;
- **the call:** in a `go` or `defer` statement; in an if, for or switch
  header, unless the body is a single return; an argument whose name a
  local of the body would capture, inside an expression.

The function is deleted only if it is unexported, nothing else refers to it
(tests included), it does not call itself, and, for a method, no interface
in the package has a method of that name. Imports that only it used go with
it. `keep_function` keeps it anyway.

## Invert if and remove else: what they check

`invert_if` negates an if statement's condition and swaps its branches,
turning nested code into guard clauses where it can. Identify the if by
`file` and the `line` of its `if` keyword (`column` only if the line has more
than one).

- **if/else:** the branches swap. If the new first branch cannot fall through
  (it ends in `return`, `continue`, `break`, `goto`, `panic` or another
  terminating statement), the else is removed and the other branch follows
  the if, un-nested. `keep_else` only swaps.

  ```go
  if ok {                          if !ok {
      work()                           log("skipped")
  } else {                 =>          return
      log("skipped")               }
      return                       work()
  }
  ```
- **if without else** becomes a guard clause, followed by its former body,
  un-nested, when it is
  - the last statement of a function without results: `if !c { return }`;
  - the last statement of a loop body: `if !c { continue }`;
  - the last statement of a `switch` or `select` case: `if !c { break }`;
  - followed only by the function's final `return`, which the guard repeats
    (`if err != nil { return err }`, then the body, then `return err`):
    every path still evaluates it exactly once.

  An if whose body already ends in an exit is already a guard clause and is
  left alone.

`remove_else` handles the case where the *if* branch already exits:
`if c { return x } else { B }` becomes `if c { return x }` followed by `B`; an
`else if` becomes a separate `if`.

The negated condition is simplified only where that is exact:

- `!x` becomes `x`;
- `==` and `!=` swap (exact for every type, NaN included);
- `<`, `<=`, `>`, `>=` flip only for integers and strings; for floats (and
  type parameters, whose type sets usually include floats) `!(a < b)` is
  kept, because with NaN both `a < b` and `a >= b` are false;
- `&&` and `||` use De Morgan's laws, with parentheses only where precedence
  needs them: `a == 1 || b == 2 && ok` becomes `a != 1 && (b != 2 || !ok)`.
  Operands are still evaluated left to right, the right one only when the
  left one does not decide the result.

Anything else becomes `!x` or `!(...)`. Branch contents move as text, so
comments come along; un-nested statements are re-indented, except inside
multi-line raw strings. An init statement stays on the if, unless the
un-nested statements use its variables: then it moves before the if
(`v, ok := m[k]` then `if !ok { return }` then the code using `v`), after the
same name checks as moved declarations, plus one: a name the enclosing block
already declares would turn `:=` into an assignment to it.

Un-nesting moves the branch's declarations into the enclosing block, so it
is refused (for an if/else, `invert_if` then only swaps and says why in a
note) when:

- the moved statements, or the if's init statement when it has to move,
  declare a name the enclosing block already declares, or that the
  code after the if uses for something else (it would now see the moved
  declaration);
- a `goto` jumps over the if (it would jump over the moved declarations);
- there is a comment between the branches, or the if is an else-if, labeled,
  or does not start its own line.

Also refused: an else-if chain in the else branch of `invert_if` (swapping
it in would add nesting; `remove_else` splits chains instead); an if without
else in any other position (code after it runs either way); a repeated
return that would mean something else inside the if (a name it uses is
hidden by the if's init statement); and `remove_else` when the if branch can
fall through.

## Add parameter: what it checks

`add_parameter` adds a parameter to a function or method and
passes a given value for it at every call in the workspace (test files
included). Identify the function by `file`, `line` and `name`, at its
declaration or any call. `parameter` and `type` give the new parameter;
`value` is the Go expression every call passes, checked at each call, so it
can name something in the caller's scope (`cfg.OutDir`). `before` puts it
ahead of an existing parameter; by default it goes at the end, or just
before a variadic parameter. Next to parameters of the same type it joins
their group (`src, dst, skip string`); inside a group of another type it
splits it (`lo int, label string, hi int`).

The body does not use the new parameter yet; using it is the edit that
follows. So that the step changes nothing, the value must be safe to
evaluate where nothing evaluated before:

- **no side effects:** no calls other than conversions, `len`, `cap`,
  `min`, `max`, `new`, `make` and the like, and the known functions without
  side effects (`strings.ToUpper(name)`; one that can panic, like
  `strings.Repeat`, counts as a panic), and no channel receives;
- **no panics:** no pointer dereference (explicit, or a field read through a
  pointer), no indexing other than of a map, no slicing or type assertion,
  and no division by a non-constant.

Constants are always fine.

Refused:

- **the function:** used other than by calling it (a function value, a
  method expression), since its type would change; a method that would stop
  or start implementing an interface (see "Changing a method: interfaces");
  an interface method;
  `init`, or `main` in package `main`; declared outside the module; without
  a body; unnamed parameters;
- **the name:** already a parameter, result, receiver or type parameter; a
  local declared at the top of the body, which shares the parameters'
  scope; a name the body uses for something declared outside the function,
  which the parameter would hide;
- **the type:** not valid in the declaring file;
- **the value:** not valid at some call, not assignable to the type, or
  unsafe as above;
- **a call** that passes another call's results as its arguments
  (`f(pair())`).

An exported function's calls in other modules are not updated; the result
says so.

## Remove parameter: what it checks

`remove_parameter` is the reverse: it removes a parameter from a function
or method and drops its argument from every call in the
workspace. Identify the function as for `add_parameter`, and the parameter
by `parameter`. Removing a variadic parameter drops all of its arguments
(and a spread `xs...`). The rest of the parameter list keeps its grouping
(`w, h, unused int` becomes `w, h int`).

The body must not use the parameter: change the body first, then remove
it. So that the step changes nothing, each dropped argument must be safe
not to evaluate, by the same rules as an added value: no side effects and
no possible panic.

Refused, besides the function checks `add_parameter` makes (function
values, method expressions, interfaces, and so on):

- **the parameter:** not found, more than one with that name (`_`), used by
  the body, or of a type that mentions one of the function's type
  parameters, since calls may infer it from that argument;
- **an argument:** unsafe as above; a call that passes another call's
  results (`f(pair())`);
- **what it leaves behind:** a local variable or an import whose only use
  is a dropped argument (or, for an import, the removed parameter's type),
  since it would then be unused and not compile. Removing the import too is
  a possible follow-up.

## Move parameter: what it checks

`move_parameter` moves a parameter to another position and moves its
argument to match at every call in the workspace. Identify the function as
for `add_parameter`; `parameter` is the one to move, and `before` the one it
should go just before (by default the end, or just before a variadic
parameter). Next to parameters of the same type it joins their group, as
`add_parameter` does.

Go evaluates a call's arguments left to right, so moving an argument
changes when it is evaluated relative to the arguments it passes over. The
move is refused at a call where that could matter, for the moved argument
and any argument it passes over:

- one has side effects (a call, a channel receive) and the other reads a
  variable, whose value the effect could change;
- both could have side effects or panic, which would then happen in the
  other order.

So `f(next(), 2)` can become `f(2, next())`, and `f(x, *p)` can become
`f(*p, x)`, but `f(next(), count)` cannot change order.

Refused, besides the function checks `add_parameter` makes: a variadic
parameter, which must stay last; a parameter that is already in that
position; a call that passes another call's results; and a moved argument
that contains another call of the same function (`f(1, f(2, 3))`), whose
own edit would have to move with it.

## Introduce parameter: what it checks

`introduce_parameter` replaces an expression in a function's body with a
new parameter, and passes that expression at every call, written in terms
of the call's arguments: introducing `p.x` as `x` in `label(p point)`
turns `label(pts[0])` into `label(pts[0], pts[0].x)`. An argument that is
not a simple operand is parenthesized (`(a+b)*2`). Identify the expression
as for `extract_variable`, by file, line and source text; the parameter
goes at the end (or before a variadic one) with the expression's type.
Followed by `remove_parameter`, it narrows a parameter to the part the
body uses.

The receiver counts as a parameter: introducing `m.decl` in a method of
`m` passes `mv.decl` at the call `mv.check()`. Where the receiver is used
other than through a selector, the address the call takes or the copy it
makes is written out (`(&v)`, `(*q)`).

The expression is now evaluated once, at the call, instead of wherever
(and however often) the body evaluated it. That keeps behavior when its
value is the same at both points and evaluating it does the same:

- it reads only parameters and the receiver, and the body never assigns
  them, changes a field or element of them, or takes their address; a
  variable declared in the body is refused;
- it has no side effects;
- it is not assigned to, addressed, or the receiver of a pointer method,
  since the parameter is a copy;
- at each call, no argument (nor the receiver) has side effects: the new
  argument repeats parts of the others, and Go does not order its
  evaluation against their calls (assign such an argument to a variable
  first);
- every other name it uses (constants, package variables, imports) means
  the same at each call, so a name hidden by a local at the call, or
  unexported in another package, is refused.

An expression that could panic (a read through a pointer, indexing,
slicing, a type assertion, division by a variable), or that reads memory
other code can change (through a pointer, a slice or map element, the
length of a map, a package variable), is allowed only when it is
evaluated *at entry*, which is what `m.decl` at the top of a method
usually is:

- on every call: not inside an `if` or `else`, a `case`, the right
  operand of `&&` or `||`, nor (once per call) a loop, a function
  literal, or a function with `goto`;
- after nothing that could change it or the outcome: no call or channel
  operation before it, no write to anything but a local variable whose
  address is never taken, and no possible panic, unless both could only
  be nil dereferences, which panic alike.

So `switch m.decl {` as a method's first statement can become a
parameter, as can `len(m.decl)` after `k := m.n`; `len(m.decl)` after
`m.n++`, a call, `xs[0]`, or inside `if m.n > 0 { ... }` cannot. The check
is by position, not control flow: a call on a path that returns before
the expression still counts.

Refused besides: a method called through an embedded field (`n.label()`
for a method of `n`'s embedded `point`). The function checks of
`add_parameter` apply too (only called, not an interface method,
interfaces kept, the new name free).

## Method to function: what it checks

`method_to_function` turns a method into a function whose first
parameter is the receiver, and changes every call: `x.m(a)` becomes
`m(x, a)`, `m(&v, a)` where the call took the address of `v` for a pointer
receiver, `m(*p, a)` where it dereferenced `p` for a value receiver, and
`m(&o.inner, a)` for a method promoted from an embedded field. The
function keeps the method's name unless `new_name` is given, and an
unnamed receiver gets a name (the type's initial, or `recv`). It is the
step before moving a method to another package, which `move_declaration`
refuses, and before narrowing what a helper takes (`introduce_parameter`
and `remove_parameter`).

The receiver is evaluated first in both forms, so behavior is the same
for every call; what can change is which types have the method:

- only unexported methods: an exported one could satisfy an interface
  outside this module that a library checks at run time (`fmt` looks for
  `String`, `errors` for `Error`), and losing it would not be a compile
  error;
- no interface in the workspace has a method of that name and package
  (an unexported method can only be in this module's interfaces), since a
  type could implement it through the method, directly or by embedding;
- every use is a call: a method value (`f := x.m`) or method expression
  (`T.m`) is refused;
- the receiver type has no type parameters (the function would have to
  declare them);
- the name is free: not declared in the package, not an import name in
  one of its files, not predeclared, and not hidden by a local at any
  call;
- a call whose receiver contains another call of the method
  (`n.after().after()`) is refused; assign the inner one first.

## Function to method: what it checks

`function_to_method` is the reverse: the first parameter becomes the
receiver, and `m(x, a)` becomes `x.m(a)`. Where the call passed `&v` (for a
pointer receiver) or `*p` (for a value receiver) it is written `v.m(a)` or
`p.m(a)`, since the call takes the address or dereferences itself; an
argument of another type, like `nil`, is converted (`(*T)(nil).m(a)`), and
a composite literal is parenthesized. The method keeps the function's name
unless `new_name` is given.

Adding a method can change what other code means without a compile
error, so:

- the method's name must be unexported: an exported one could make the
  type satisfy an interface checked at run time (`fmt` looks for
  `String`); every call must then be in the function's package;
- no interface in the workspace has a method of that name and package
  (the type would start implementing it, which a type assertion or switch
  could notice);
- neither the type nor any type that embeds it already has a field or
  method of that name: the new method would clash, or, promoted, change
  which one `x.name` means;
- the receiver is the first parameter (move another one there first with
  `move_parameter`, which checks the evaluation order), not variadic, and
  of type `T` or `*T` for a non-generic defined type `T` declared in the
  function's package, which is not a pointer or interface type;
- the function is not generic, and every use is a call; a receiver
  argument containing another call of the function (`f(f(x))`) is
  refused.

## Extract struct: what it checks

`extract_struct` moves some fields of a struct type into a new struct
type, declared just before it, and embeds the new type (by value) where
the first of them was. Every use keeps compiling with the same meaning:
`m.path` now selects the field through the embedded struct. Fields move
with their doc comments, tags and trailing comments. Composite literals of
the type are rewritten: keyed ones group the moved keys
(`mover{ws: w, source: source{path: p}}`), unkeyed ones the moved values
(`point3{xy{1, 2}, 3}`), and a literal inside another is rewritten first.
It is the first step of splitting a struct; `function_to_method` and
`move_declaration` then give the new type its methods and its place.

Embedding keeps uses, but changes the struct's shape, so:

- only unexported struct types: code outside the module could write an
  exported type's literals, or see its fields by reflection;
- no value of the type (or pointer to it) may be passed to an interface
  parameter or converted to an interface (`fmt.Printf("%+v", m)`,
  `json.Marshal(m)`, `any(m)`): reflection would see the new embedded
  field. A value assigned to an interface variable is not checked;
- the moved fields are one level deeper, so another embedded field that
  has a field or method of the same name would make the selector
  ambiguous: refused;
- an unkeyed literal can only be rewritten when the moved fields are next
  to each other (otherwise its values would be evaluated in another
  order); give it keys first;
- `new_type` must be free: not declared in the package, nor a field or
  method of the struct (the embedded field takes its name).

Not supported yet: a generic struct, an embedded field among those to
move, part of a field group (`x, y int`), and a multi-line literal with
comments between its elements (rewriting it would lose them).

## Merge duplicate function: what it checks

`merge_duplicate_function` replaces every use of a function (calls and
function values) with an identical function, qualified and imported where
needed, and deletes it, with the imports only it used. To remove copies of
a helper, move one copy where the others can reach it (`move_declaration`,
which can create the package), then merge each of the others into it.

Identical means a call of one does exactly what a call of the other does:

- the same signature (parameter names aside);
- bodies that match node for node, ignoring comments and layout:
  parameters and locals may be named differently, consistently (so
  `x - y` does not match `y - x` with the parameters swapped), and a call
  of itself matches the other's call of itself;
- every other name means the same object in both: a function, variable,
  type, field or method, or an import of the same package
  (`strings.ToUpper` matches `strings.ToUpper`; one package's `helper`
  does not match another's).

The refusal names the first difference. Also refused: methods and generic
functions; an exported function to delete (code in other modules could
call it); a function to keep that a user cannot see (unexported, in
another package) or could only import by making an import cycle; and a use
where the new name, or its package's name, means something else there.

## Remove dead code: what it checks

`remove_dead_code` deletes a top-level declaration (function, method,
type, variable or constant) that nothing uses, with its doc comment and the
imports only it used; in a parenthesized group, only its spec goes. Being
unused is not enough to be dead, so it must also be something nothing can
reach any other way:

- unexported (other modules could use an exported name), except in a
  `main` package, which nothing imports;
- not `init`, `main`, or a `Test`, `Benchmark`, `Fuzz` or `Example`
  function in a test file, which run without being called;
- a method that no interface in the workspace names (a type could
  implement one through it);
- a variable whose initializer has no side effects;
- not a blank name (`var _ I = (*T)(nil)` checks something at compile
  time), and without a `//go:` or `//export` directive;
- a type without methods (remove them first), and a spec that declares
  only that name.

Uses inside the declaration itself (a recursive call) do not count.

## Move statement up and down: what they check

`move_statement_up` swaps a statement with the one before it in its block
(or case clause), `move_statement_down` with the one after it ("slide
statements"). Identify it by `file` and the `line` it starts on; the
outermost statement starting there moves, so a whole `if` or loop moves as
one. Each statement takes along the comments directly above it (no blank
line between) and a comment ending its last line; blank lines and other
comments between the two stay where they are. A statement that shares a
line with other code is refused.

The two must be independent, which it checks conservatively:

- **names:** neither may use something the other declares (`b := a + 1`
  after `a := 1`; also `y, ok := ...` after `x, ok := ...`, which assigns
  the same `ok`), and the statement moving up may not declare a name the
  other refers to (it would capture that reference);
- **local variables** that only the function's own code can reach (their
  address is not taken and no closure uses them): one may not assign a
  variable the other uses. Fields count separately, so `p.x = 1` and
  `p.y = 2` swap, but `q := p` and `p.x = 1` do not;
- **side effects:** a call not known to have none (see "Calls known to
  have no side effects" and "Calls of the module's own functions"), a
  channel operation, `go`, `defer`, `select`, or
  an assignment to a package variable, through a pointer, or to a
  variable a closure or pointer can reach. A statement with side effects
  may not move past one that has side effects too, reads what other code
  can change (package variables, memory through pointers, slice and map
  elements, variables a closure can change), or could panic or not
  finish;
- **panics:** two statements that could both panic or not finish
  (indexing, dereferencing, slicing, a type assertion without `ok`,
  integer division by a variable, a known call that can panic, a `for`
  loop) are not swapped, since which one fails first would change;
- **leaving:** a statement that returns, `break`s or `continue`s may
  only swap with one that does nothing but declare new variables, with no
  side effects and nothing that could panic or not finish: such a
  statement running, or not, when the other leaves cannot be seen. An
  assignment could be (a bare `return` reads named results; a `break`
  leaves the loop's variables in use). `goto`, `fallthrough`, labeled
  statements and two leaving statements are refused.

Defining a function literal runs nothing, so what its body does does not
count, until something calls it.

## Changing a method: interfaces

Changing a method's signature can change which interfaces its type
implements. Where the program converts the type to an interface, that is a
compile error; but a type assertion (`v.(fmt.Stringer)`, or `errors.As`, or
`fmt` checking for `String() string`) just quietly gives a different answer.
So `add_parameter`, `remove_parameter` and `move_parameter` compute the
method's new signature and refuse the change if any type whose method set
includes the method, the receiver type, a pointer to it, or a type that
embeds it, would

- **stop implementing** an interface it implements now (`T implements
  fmt.Stringer`), or
- **start implementing** one it does not (removing `force` from
  `Close(force bool) error` would make the type an `io.Closer`).

The interfaces checked are every one the program can see that has a method
of that name: the predeclared `error`; every interface type written in the
module's code, named or not (`v.(interface{ Size() int })`); and every
interface declared at package level in any dependency, the standard library
included. A change that keeps the signature (moving a parameter among
parameters of the same type) is allowed even when the type implements an
interface with that method.

Not seen: interfaces declared inside functions of other modules, like the
`interface{ Unwrap() error }` that `errors.Is` checks for; the result of an
exported method's change says so. And code in other modules, which could
declare its own interfaces, is never seen. Methods of generic types are
refused whenever an interface has a method of that name.

## Move declaration: what it checks

`move_declaration` moves a top-level declaration (a function, method, type,
variable or constant, with its doc comment and a comment ending its last
line) to another file, creating the file if it does not exist. This
section covers a file of the same package; "To another package" below
covers the rest. Identify it by `file`, `line` and `name`, at the declaration or
any use; `to_file` is the destination, relative to the module root.

Top-level names live in the package scope, so no reference changes. What
changes is per file:

- **imports:** the destination gets the imports the moved code uses. A new
  file copies them as the source writes them (aliases included); an
  existing one keeps its own name for a package it already imports, and
  the moved code's qualifiers change to match (`str.ToUpper` becomes
  `strings.ToUpper`). Imports that only the moved code used are removed
  from the source. Added imports go where goimports puts them: standard
  library imports and the others in separate runs (separated by a blank
  line), each sorted into a run of its kind or a new one. The same holds
  for every refactoring that adds imports (`extract_function` too);
- **which builds see the code:** both files must be built everywhere (no
  `//go:build` line, no GOOS or GOARCH in the name, no cgo), and both must
  be test files or neither, in the same package clause (not `p` and
  `p_test`);
- **a source file left empty:** when the move leaves a file with nothing
  but its package clause (no declarations, imports or comments), the file
  is deleted, and a note says so; undo writes it back. A file that keeps
  a comment (a copyright line, a package doc) stays.

Refused as well:

- an `init` function, or a variable whose initializer has side effects:
  package initialization follows the order of the files, so moving one
  could change what runs first;
- a `//go:embed` variable;
- code that uses a dot import.

### Out of a group

A declaration grouped with others in parentheses (`var ( a = 1; b = 2 )`)
moves out of the group as a declaration of its own, `var b = 2`, with its
doc comment and trailing comment, one level less indented; the rest of the
group stays. Naming every spec of a group (the name and `also`) moves the
group as it is.

In a `const` group a spec's position can matter: `iota` is its index, and
a spec without values repeats the previous expression (with its own
`iota`). So taking a constant out of its group is refused when it has no
values or uses `iota`, and when a later spec in the group does either,
since its value would shift; move the whole group instead.

### Several declarations at once

`also` names other top-level declarations of the package (functions,
types, variables and constants, not methods), in any of its files, to
move with it, e.g. the helpers it calls. They go to the destination
together, the named declaration's file first, each file's in its order;
each is held to the same checks, and so is each file they come from
(test file or not, build constraints, cgo); they keep their names. To another
package this lets code that uses its neighbours move: references among the
moved declarations stay unqualified. A declaration in `also` that stays
unexported but is still used outside the new package is refused; rename it
first. Refused: a name the package does not declare at top level, one
already in the destination file, and, for a new destination file, a
package that two source files import under different names (the new file
could only use one; rename one first).

### To another package

When `to_file` is in another directory, the declaration moves to the
package there, and every reference changes: uses outside that package
become `q.Name`, importing `q` where needed; uses inside it lose their
`p.` qualifier; and the moved code's own references to `q` lose theirs.
Imports left unused anywhere are removed. `new_name` renames it on the
way, together with the first word of its doc comment; an unexported
declaration used outside its new package needs one, since it must become
exported (`defaultHost` in a command becomes `agent.DefaultHost`).

Refused, besides the checks above:

- **what moves:** a method on its own (it must stay with its type), a
  declaration of several names (`var a, b = 1, 2`), and code
  that uses other top-level declarations of its package: the new package
  would have to import the old one, which imports it (move those first,
  or with it in `also`);
- **where:** a test file on either side, a file with build constraints, a
  name the package already declares;
- **who uses it:** a package that would then import the destination while
  the destination (or the moved code's imports) already depends on it,
  which is an import cycle; a package that cannot import the destination
  (`internal`); a use through a dot import.

A directory without a Go package gets one, created by the move (the
directories too, if missing; undo removes them): it is named after the
directory, with the import path of the module plus the directory, and the
result notes it. Refused: a directory the go command ignores (`testdata`,
or a name starting with `_` or `.`), one whose name is not a valid
package name (`my-text`) or is `main`, and one that has Go files no
loaded package covers (all behind build constraints, say).

A type takes its methods along, since a method must be in its type's
package, from whichever files of the package they are in. A method call (`c.Inc()`) does not change, but what code left
behind can reach does:

- an unexported field or method of the type used outside the destination
  package (by a selector, a key, or an unkeyed composite literal such as
  `T{3}`) is refused: export it first, or move that code too;
- an unexported method belongs to its package, so after the move the type
  no longer implements an interface naming it; if any interface in the
  workspace has such a method, the move is refused, since a type
  assertion would quietly fail.

## Edits

A refactoring keeps behavior; an edit changes it, on purpose: it is the
step that adds or changes a feature. Edits implement `refactor.Edit`, and
the engine holds them to the same rule as refactorings: the code
type-checks before every step and after it. An edit whose result would not
type-check is rolled back and refused, listing the errors (which come from
the code it was given, so the message asks for different code rather than
reporting a bug in the tool). Every edit can be undone.

So writing a failing test first takes two steps: declare what the test
calls (a stub that compiles), then create the test, which compiles and
fails.

### create_file

`create_file` creates a file with the given `content` at `file`, relative
to the module root. A Go file must parse, and its package clause must match
the other Go files in its directory (a test file may use the `_test`
package; any name is fine in a directory without Go files); it is gofmt-ed.
Other files are written as given. Missing directories are created, so a
new package's first file needs no other step. Refused: a file that exists
(change it with the other tools), a path through a file (`sub/b.go` when
`sub` is a file), a path outside the module, or a file whose code does not
type-check (a test that calls something not declared yet). Undo deletes
the file and the directories it created.

### replace_statements

`replace_statements` replaces whole statements of one block, lines
`start_line` to `end_line` (as `inspect` numbers them), with `content`: new
Go statements, without the enclosing braces. It is how a stub's body
becomes the implementation (select the stub's `return` line), and how
statements change what they do. The lines must cover whole statements of
one block, with nothing else on them but comments and blank lines (which
are replaced too); `content` must parse as statements, and is re-indented
to the block (unless a raw string in it spans lines).

Imports follow the code. A package the new statements name that is not in
scope there is imported: the package another file of the package imports
under that name, else a workspace package of that name, else a standard
library one; when several fit (`rand`: `crypto/rand`, `math/rand`,
`math/rand/v2`) the refusal lists them, and `imports` names the one to
use. An import only the replaced statements used, and the new ones do not,
is removed. Refused: lines that cut a statement or cover statements of
different blocks, content that does not parse, and, like every step, a
result that does not type-check.

### insert_statements

`insert_statements` inserts new statements at `line`: `before` the
statement that starts there, `after` the statement that ends there (the
outermost one, so on a closing brace it means after the whole `if` or
loop), or `into` the block whose `{` is on that line (or the `case` clause
whose colon is), at its start. An empty function body written on one line
(`func f() {}`) is opened up. The content is re-indented to the block, and
its imports are added as for `replace_statements`. Refused: a line where no
statement starts or ends (or no block opens), a line with other code
before or after the statement, a non-empty block written on one line (use
`replace_statements`), and a result that does not type-check.

### add_declaration

`add_declaration` adds top-level declarations (functions, methods, types,
variables, constants, with their doc comments) to an existing file: at its
end, or just `after` or `before` the top-level declaration of that name
(`Type.Method` for a method, which `after` alone cannot name when the
method shares a type's name).
The `content` has no package clause and no imports: packages it names are
imported as for `replace_statements`, and an `import` in it is refused. A
name the package already declares is refused by the type check, whose
message names both places.

### replace_expression

`replace_expression` replaces one expression (found by line and source
text, as for `extract_variable`) with `new`, parenthesized where its
context needs it (`b` becomes `(b + 1)` in `a * b`), with the imports it
needs. With `all_occurrences` it replaces every expression in the file
that means the same: the same tokens, with package-level names, imports,
fields and methods referring to the same things and local variables of the
same types, so `c.Body` in another method of the same type matches and
`d.Body` of another type does not. A change in the expression's type is
noted; the result must type-check.

### add_field

`add_field` adds fields to a struct type: one or more, as Go source with
their comments and tags, at the end or just `after` or `before` a named
field. A struct written on one line (`struct{ x, y int }`, or `struct{}`)
is opened up into lines. Packages the fields' types name are imported.
Adding a field changes the struct (its literals, its size, what reflection
sees), so it is an edit; the type check refuses a duplicate name and an
unkeyed literal that would no longer list every field.

## Testing

`go test ./...` runs the unit and golden tests. The golden cases run in
parallel, and after each applied refactoring and each undo they compare the
incrementally updated workspace with a fresh load. `go test -short ./...`
skips that comparison for a faster local loop; CI always runs it.
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
   precondition failure.
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
