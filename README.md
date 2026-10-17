# go-refactor-mcp

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
- **Step 3 (in progress):** refactorings that simplify code.
  `extract_variable` and `inline_variable` are done; `extract_function` is in
  progress (parameters, results and `return` statements done; adding
  imports, local constants and generics next).

Next: `invert_if` (guard clauses); then an Ollama loop.

## Layout

```
cmd/refactor/                 CLI: one refactoring step per invocation
cmd/refactor-mcp/             MCP server (stdio) for Claude Code and other MCP clients
internal/
  analysis/                   shared facts: side effects, stable expressions, escaping/modified locals
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
    extractvar/               extract_variable + testdata + opt-in stress test
    inlinevar/                inline_variable + testdata + opt-in stress test
    extractfunc/              extract_function + testdata + opt-in stress test
```

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

If `refactor-mcp` is not on the `PATH` Claude Code sees, use its absolute
path as the command. The server works on the module containing `-C`, or
`$CLAUDE_PROJECT_DIR` (which Claude Code sets), or its working directory.

Then, in a Go project, ask something like *"use the gorefactor tools to
simplify calc/calc.go"*. Run `/mcp` in Claude Code to see the server's status.

Tools:

| Tool | What it does |
|---|---|
| `inspect` | No args: module overview. `file`: outline (line ranges, complexity, nesting) + numbered source. `function` or `start_line`/`end_line` to narrow. |
| `rename`, `extract_variable`, `inline_variable`, `extract_function` (and every future refactoring) | Applies one step; returns `APPLIED` + diff, or `REFUSED` + reason (nothing changed). |
| `undo` | Reverts the last applied step; repeatable. |
| `check` | Type-check status; `run_tests: true` also runs `go test`. |

The server sends instructions to the model at startup (one refactoring per
call, re-inspect after each step, stop when nothing clearly improves). It
keeps one workspace per session so `undo` works across calls, and it
re-checks the files on disk before every call: if the model or you edit a
file directly in between, it updates instead of applying edits at stale
offsets, and `undo` refuses to clobber those edits.

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
than conversions and `len`/`cap`/`min`/`max`), no pointer, slice or map
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
(inclusive, as `inspect` numbers them). The original text moves, so comments
come along; it is re-indented, except inside multi-line raw strings and
comments.

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
  change, so it is passed by value, not by pointer.
- Local constants and types the statements use but do not declare (or
  declare and code after them uses, or that appear in the new function's
  signature) move to package level, just above the enclosing declaration,
  so both functions share one declaration: a copied type would be a
  different type, and a constant passed as a parameter would stop being a
  constant. Local constants and types they depend on move with them.
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
  or when it is a non-pointer value or `&T{...}`. The call site uses `err`,
  or `err2` and so on if `err` is taken.

Other returns are refused: an early return of a success value (`return v,
nil`) looks the same to the caller as falling off the end. So are returns
that use the enclosing function's named results, bare returns in a function
with results, and `return f()` of a multi-value call in the early-exit
shape.

Refused (with an explanation):

- a variable the statements declare has its address taken or is used by a
  function literal, and is used after them: returning it would return a
  copy (some types, like `strings.Builder`, also refuse to be copied);
- a `:=` in the statements redeclares a variable passed as a pointer;
- a type parameter of the function is used by the statements (directly or
  through a variable's type), unless it belongs to the receiver of a method
  the statements become;
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

Four opt-in tests try the
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
   `go.mod`), `args.json`, and either `out/` or `refused.txt`. Generate `out/`
   with `go test ./internal/refactorings/<name>/ -update` and review the diff.
   The harness also checks that undo restores `in/` exactly.
   Then run a stress test against real code (see Testing).
3. Register it in `internal/refactorings/registry.go`. It then appears as an
   MCP tool (named `Name()`, described by `Description()`) and in the CLI
   automatically.
