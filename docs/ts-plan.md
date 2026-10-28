# TypeScript: plan

TypeScript support for this server: type-checked refactorings and edits
for TypeScript, from the same server and under the same tool names as
the Go ones. The tools pick the language from the file they are given.
Same aim, same loop: Claude chooses the step, the tool performs it
deterministically, re-type-checks, and returns a diff or a refusal that
says what to send instead.

```
model ──(tool call: rename {...})──▶ engine: check preconditions
                                             apply byte-range edits
                                             re-type-check, roll back on error
model ◀──(APPLIED + diff | REFUSED + reason)──┘
```

This document is the plan only: what to build on, how the Go and
TypeScript sides share one server, what is new for TypeScript, and the
order of work. Status as of 2026-10-27; README roadmap item 40.

## 1. What to build on

### Can the Go TypeScript compiler be used as a library?

Yes, unofficially today, and probably officially soon.

- **TypeScript 7.0** (the Go port) shipped on 2026-07-08. It ships no
  programmatic API; the team expects "a new (and different) API" in 7.1.
- Upstream keeps every Go package under `internal/`, so it cannot be
  imported directly. On the question of a public Go API, the team said
  (July 2026) they will do it when the code moves back into
  microsoft/TypeScript, possibly before 7.1. Their recommended path until
  then is the out-of-process IPC API, which they expect to be usable
  from Go. A C ABI is not planned.
- **buke/typescript-go-internal** republishes those packages under
  importable paths (`github.com/buke/typescript-go-internal/v7/pkg/...`),
  synced from upstream release tags by a script and a scheduled workflow;
  current release v7.0.2, Apache-2.0, needs Go 1.26. It is not affiliated
  with Microsoft and has a handful of users. It exposes what we need:
  `ast`, `parser`, `binder`, `checker`, `compiler` (Program,
  `GetTypeChecker`), `tsoptions` (tsconfig), `vfs`, `project`, `ls`,
  `format`, `printer`, `scanner`, `astnav`.

### What the Go language service has, and lacks

Checked in the source (`pkg/ls`, v7.0.2):

| Has | Lacks |
|---|---|
| rename, find all references, implementations | every refactor provider: extract function / constant / type, inline, move to file, convert parameters to object, convert function ↔ arrow, … |
| call hierarchy (incoming / outgoing) | `getEditsForRefactor` as a whole |
| update imports on file rename (`file_rename.go`) | |
| organize imports, sort / remove unused imports | |
| format document / range | |
| code actions: import fixes, missing members, class-implements-interface, missing type annotation | |
| diagnostics, hover, definition, symbols, selection ranges | |

So on Go, the refactorings themselves are ours to write, as they were in
gorefactor. Rename, references, file moves, imports and formatting come
free.

### The options

| | A. Go + typescript-go-internal | B. Node + TypeScript 6 JS API | C. Go + tsgo out of process (LSP / IPC) |
|---|---|---|---|
| Built-in refactors | none | about 20, editor-grade | none in 7.0 |
| Reuse of gorefactor | high: MCP server, batch and `$refs`, edit / diff, undo, checkpoints, A/B harness, refactor-tokens | low: rewrite in TS | high |
| Checker access | full, in process | full, in process | only what the protocol exposes |
| Speed | native, shared-memory | 8–12x slower on full builds | native, plus IPC cost per query |
| Future | 6.0 is the last JS version | swap import path to the official Go API when it lands | 7.1 API |
| Risk | unofficial mirror; `internal` APIs change at every sync | frozen compiler; TS 7 features unparsed eventually | refactorings need AST + types, a protocol is too narrow |

**Recommendation: A.** The value of gorefactor is not the transforms an
editor already offers but the checks, the refusals written for a model,
batch addressing, small results and the measuring harness, and all of
that is language-neutral Go we can lift. TS 6's built-in refactors are
editor actions (they pick names like `newFunction`, ask nothing, and
refuse nothing); wrapping them would still mean writing our own checks,
on a compiler that is now frozen.

Containing the risk of A:

- One adapter package, `internal/tsapi`, is the only importer of
  `buke/typescript-go-internal`. Everything else uses its types. When the
  official Go API appears, the swap is in one package.
- Pin the mirror's version in `go.mod`; upgrade in its own PR with the
  golden tests as the gate.
- Step 0 below is a spike that proves the pieces we depend on before
  anything else is built.

Fallback if the spike fails (for example the checker cannot be re-run
cheaply after an edit): C for type-checking, keeping our own parser use
for edits, or B as a last resort.

## 2. One repo, one server

Decided 2026-10-27 (the user): TypeScript goes in this repo and module,
and one server serves both languages under the same tool names.

**Why one repo.** About 4,500 of the roughly 42,000 non-test lines are
language-neutral: `edit` (diffs), `mcpserver`, `ab`, `tokens`,
`refactortest`, about half of `refactor` (batch, `$refs`,
checkpoints) and the commands around them. They are the part still being
tuned from A/B results (batch addressing, result size, refusal wording).
In one module the TypeScript side uses them as they are, and an
improvement lands once. The other 37,000 lines are Go-specific but carry
the design that TypeScript repeats: the `Refactoring` / `Edit` / `Fix`
interfaces, refusals that say what to send, name addressing, stubs from
usage, the `Move` question, the recipes, the eval tasks.

**Layout.**

```
internal/ts/tsapi/          the only importer of buke/typescript-go-internal
internal/ts/workspace/      tsconfig, Program over a vfs overlay, refresh
internal/ts/inspect/        a file without bodies; names
internal/ts/analysis/       effects and the Move question, TypeScript's rules
internal/ts/refactorings/   one package per refactoring, as for Go
internal/ts/edits/          one package per edit, as for Go
internal/lang/              the dispatcher: one tool per name, routed by file
```

The Go packages stay where they are; a later PR may move them under
`internal/golang/` if the symmetry pays for the churn.

**Dispatch.** Two registries with the same tool names, and a dispatcher
in front of them. Today `Plan` takes a `*workspace.Workspace`; wrapping
each registry leaves the Go refactorings as they are.

- A tool with a `file` goes by its extension: `.go` to Go; `.ts`, `.tsx`,
  `.mts`, `.cts` to TypeScript. Contents are never needed.
- Workspaces load on first use: a Go workspace per `go.mod`, a program
  per `tsconfig.json`. A Go-only repo never loads the TypeScript checker.
- `check` checks what was touched. `undo` is one stack for both
  languages: changes are file bytes either way, so the undo stack moves
  out of the Go workspace into the engine.
- `inspect` with `names` and no file looks in both languages, and
  refuses when a name exists in both, asking for a `file`.
- A `batch` may change Go and TypeScript files (a handler and the client
  that calls it): every workspace it touched is checked, and a failure
  rolls back all of them.
- A panic in either side is recovered per call, so it does not take down
  the other.
- A tool that exists in one language only (`add_result`,
  `convert_params_to_object`) refuses on the other, saying so; a concept
  both have keeps one name where the operation is the same.

**Descriptions by what the repo holds.** A fresh model is sensitive to
descriptions (cutting `fold`'s lost its use, session notes 2026-10-21),
and examples for two languages would double what every schema load
costs. So at start the server looks at the root: only `go.mod`, today's
descriptions word for word; only `tsconfig.json`, TypeScript examples;
both, both. Arguments for one language (`pointer` on
`extract_variable`) appear only where that language is present. The
tool list can change mid-session (`listChanged`) if a tsconfig appears.
Single-language repos, the usual case, cost nothing extra, and Go's A/B
baselines stay valid; one Go A/B run confirms it.

**Costs to manage.**

- Context: the root `CLAUDE.md` imports `docs/session-notes.md` (51 KB)
  into every session. Split it: shared practices at the root, Go notes
  in a CLAUDE.md under the Go code, TypeScript notes under
  `internal/ts/`, so each is loaded where the work is.
- CI: path filters, so a TypeScript-only PR does not run the Go golden
  tests and the reverse; a change to shared packages runs both.
- Releases: one version for the one server, as now; the notes say which
  language a change is for.
- Name: the repo stays `go-refactor-mcp` (renaming changes the module
  path and every `go install` path). The server's MCP name can stay
  `gorefactor`.

## 3. The engine for TypeScript

**Workspace.** Root is the directory of a `tsconfig.json`, and when it
is a solution (project references), every project it references is
loaded: a file's default project can leave its tests out, and a rename
there misses them (spike, finding 1). Load with `tsoptions` and build a
`compiler.Program` over a `vfs` overlay (`wrapvfs` replacing
`ReadFile`), so a plan can be type-checked before anything is written
to disk; a changed file goes in with `Program.UpdateProgram`. Refresh
on every call when any `.ts/.tsx/.mts/.cts` file, tsconfig or
package.json changed (stamps, as now). Edit only files under the root
that the program owns; never `node_modules` or `.d.ts` from packages.

**Safety rule.** Same as gorefactor: semantic and syntactic diagnostics
before and after; a refactoring must add none; an edit marked `Fix` must
remove some and add none. Compare diagnostics by file, code, message key
and arguments, not position. Re-check through the incremental builder
(`execute/incremental`, `tsc --watch`'s): it keeps each file's
diagnostics and re-checks importers only when a file's declaration
output changed, 27 ms for a body edit against seconds for a plain
re-check of importers. Record every file's signature at load, in the
background, or the first edit pays for them (spike, findings 2-3).

**Formatting.** There is no gofmt. Format only the text a step inserts,
with `pkg/format` and the project's settings (tsconfig / `.editorconfig`;
read `.prettierrc` for indent and quotes if present). Never reformat code
the step did not write. `replace_text` matches the file as it is.

**Inspect.** A file without bodies, with line numbers, where "body"
covers function and method bodies, arrow functions bound to a `const`,
object literal methods, and getters / setters; classes keep their member
signatures. Addressing by name: `fn`, `Class.method`, `Class.prop`,
`Interface.member`, `ns.fn`, and `default` for a default export.

**Imports.** After any step that moves or adds references: add missing
imports with the LS import fixes, and remove imports the step left
unused (only those, not others already unused). Path style follows the
file's existing imports (relative vs `paths` alias, extension or not).

**Exported code** is changed, with the API note, as decided for
gorefactor; "exported" means reachable from the package's `exports` /
`main` / `types` entry, or simply `export` when there is no package.json.

## 4. Where TypeScript differs (the checks to write)

These are the rules the Go analysis encoded that do not carry over, and
the new ones. Each becomes a refusal with a reason, and a golden test.

- **Reading a property can run code.** Getters, setters, Proxies, and
  `valueOf` / `toString` through operators and template literals. Start
  conservative: effect-free means literals, `const`/`let` locals,
  parameters, `typeof`, arithmetic on values typed `number` / `bigint`
  / `string` / `boolean`, and a table of known-pure library calls
  (`Math.*`, string and array methods that do not mutate), the
  counterpart of `stdFuncs`. A property read on a type with a declared
  accessor counts as a call. Widen later with evidence.
- **Anything may throw.** There is no `err` result; every call and
  property read on a possibly-nullish value may throw. `Move` must treat
  "may throw" like gorefactor's "may panic", and the decision about
  nil dereferences past calls (session notes, 2026-10-21) needs to be
  made again for TS.
- **`this`.** Extracting code that uses `this` into a function makes a
  method, or passes `this`, or refuses; an arrow function keeps `this`,
  a `function` does not. `function_to_method` / `method_to_function`
  apply to classes only.
- **Hoisting and the temporal dead zone.** Moving a statement or
  extracting a variable must not read a `let`/`const`/`class` before its
  declaration; `var` and function declarations hoist.
- **Closures in loops.** `let` in a `for` head is per-iteration;
  extracting the loop body must preserve which binding a closure sees.
- **`await`, `yield`, `return`, `break`, `continue`, `arguments`,
  `super`.** Extracting code with `await` makes an async function and
  awaits the call; `yield` and `arguments` refuse; jumps follow
  gorefactor's rules for `return` and `break`.
- **Structural typing.** A parameter or property change can break
  assignability far away (an object literal passed to an interface). The
  re-type-check catches it; refusals should name the type that no longer
  matches.
- **Declaration merging, overloads, namespaces, `declare`.** rename and
  change signature must handle all overload signatures and merged
  declarations, or refuse.
- **Weak types.** With `strict` off, or with `any` around, the type
  checker catches less. The result notes when a changed file has
  `noImplicitAny` off or the change touches `any`-typed values.
- **TSX.** Parse and edit `.tsx`; components are functions; JSX
  attributes are arguments for change-signature purposes (props object).
- **JS files** (`allowJs`): read-only at first.

## 5. Tools: gorefactor's list mapped to TypeScript

Same names where the operation is the same, so prompts, recipes and
eval tasks transfer.

| gorefactor | TypeScript | Notes |
|---|---|---|
| inspect, check, undo, batch | same | |
| rename | same | LS rename, plus our checks: shadowing, merged declarations, string-keyed access (`obj["name"]`) and computed keys reported |
| extract_variable / inline_variable | same | effects and TDZ rules above |
| extract_function / inline_function | same | `this`, `await`, closures |
| invert_if, remove_else | same | |
| add_parameter, remove_parameter, move_parameter, introduce_parameter | same | overloads; optional and default parameters; rest parameters |
| change_parameter_type, change_type, change_field_type | same (`change_property_type`) | |
| add_result | **change_return_type** | no multiple results in TS; widen to an object or tuple |
| method_to_function, function_to_method | same | classes only |
| extract_struct | **extract_interface** / **extract_type** | name a type from an object type or a class's members |
| add_field | same, also called `add_property` | to interface, type literal or class |
| fold, parameterize, merge_duplicate_function | same | |
| remove_dead_code | same | plus unused exports, unused properties |
| move_statement_up / down | same | |
| move_declaration | same | to another file, imports updated; with `also` |
| move_file, delete_file, create_file | same | LS file_rename updates imports |
| type_switch_to_method, prepare_switch_for_types | **union_switch_to_method** | `switch (x.kind)` on a discriminated union, or an `instanceof` chain, to a method per class |
| add_case | same | for a union-typed switch |
| fix_imports | **organize_imports** | from the LS |
| replace_statements, insert_statements, replace_expression, add_declaration, replace_declaration, replace_text, add_element, create_from_usage | same | stubs throw `new Error("not implemented")` |
| — | **convert_params_to_object** | the bundled-parameters recipe as one step |
| — | **function_to_arrow / arrow_to_function** | `this` checks |
| — | **add_export / remove_export** | |

## 6. Roadmap

TDD throughout, one item per branch and PR, the friction log kept as for
Go. Steps 1 and 2 change shared code and come before any TypeScript.

0. **Spike.** *Done* (`docs/ts-spike.md`, `cmd/ts-spike`): on Hono
   (82k lines with tests), loading takes 0.33 s, an edit's program
   update 1-2 ms, a re-check after a body edit 27 ms and after an
   exported change 1.4-3.4 s on 1-2 cores; rename through the language
   server's session is correct and takes 10 ms. Option A holds. It
   added three requirements to step 3: load every project of the
   solution, re-check through the incremental builder, record
   signatures at load.
1. **Split CLAUDE.md and the session notes** by language, and add CI
   path filters. *Folded into step 3* (2026-10-27, the user): with no
   TypeScript code yet there is nothing to filter, and the root
   CLAUDE.md is the one Claude Code always loads, while the Go code has
   no directory of its own for a nested one. So the root stays as it
   is, and step 3 adds `internal/ts/CLAUDE.md` (loaded when a session
   reads files there) and the CI filters with the first TypeScript
   code.
2. **Make the engine language-neutral:** the undo stack out of the Go
   workspace, a workspace interface the engine and batch use, the
   dispatcher in `internal/lang` with Go as its only language. No
   behavior change; the Go golden tests and one Go A/B run are the gate.
   First part *done*: `internal/history` holds `Change` (moved with
   move_declaration) and `Stack`, the undo stack, which a workspace
   reaches through its `History` field; the workspace's undo methods
   delegate to it. Second part *done*: the engine is generic over a
   `refactor.Workspace` (root, paths, errors, preview, apply, undo,
   squash, last change). `RefactoringOf[W]`, `StepOf[W]`,
   `OptionsOf[W]`, `ExpanderOf[W]`, `CheckpointOf[W]`, `Run[W]` and
   `RunBatch[W]` take the workspace type; `Refactoring`, `Step`,
   `Options`, `Expander` and `Checkpoint` are their instances for Go,
   so no refactoring changed. A TypeScript refactoring is
   `RefactoringOf[*ts.Workspace]`, and gets the safety rule, batches,
   line tracking, `$ref`s, checkpoints and compact diffs as they are.
   What stays Go's: the signature note after an edit and the stubs of a
   batch run only when the workspace is `*workspace.Workspace`; a
   checkpoint's tests are `go test` unless `Options.Test` says
   otherwise, which the TypeScript side will set. A batch is one
   workspace type, so a batch over Go and TypeScript files is the
   dispatcher's to split (the third part).
   Third part *done*, and with it step 2: `internal/lang` has the
   `Language` interface (name, the files it owns, step, batch, inspect,
   check, undo) and the `Dispatcher`, which gives a call to the
   language of the source files it names (`file`, `files`, `to_file`,
   in steps and targets too), to the first language when it names
   none, and refuses a call that names files of two languages (send
   them as separate calls; a batch over both is for later, if wanted).
   Undo goes to the language of the change on top of the one stack all
   languages share. `internal/lang/golang` is Go, with what the server
   did before; the server registers the same tools and calls through
   the dispatcher. The CLI (`cmd/refactor`) still uses the Go workspace
   directly. Not yet run: the Go A/B gate.
3. **TypeScript engine:** `tsapi`, workspace, edits, diffs, safety rule,
   formatting of inserted text, `rename`, golden tests (`testdata/*.ts`
   in and out), wired into the dispatcher.
   First part *done*: `internal/ts/tsapi` (projects from a tsconfig,
   updated in place or rebuilt for a created or deleted file, their
   diagnostics as "file:line:col: message") and `internal/ts/workspace`,
   the engine's `Workspace` for a solution: every project the root
   tsconfig.json references, errors once each, preview, apply, undo,
   refresh from disk. Tested on a solution fixture whose test file only
   the spec project has, and through `refactor.Run`: a step that breaks
   a test file's types is refused and undone. `internal/ts/CLAUDE.md`
   is step 1's nested instructions. Open for the engine: its "errors
   only in tests" rule knows `_test.go` only, so a TypeScript test
   written first needs the workspace to say which files are tests;
   signatures are not yet recorded at load.
   Second part *done*: `rename` for TypeScript, and the TypeScript
   language in the dispatcher. Rename runs the language service on the
   workspace's own programs (a small host in `tsapi`, not the language
   server's session, which would keep a second copy of every program),
   in each project that has the file, and merges the edits: on Hono it
   made the spike's 47 edits in 5 files in 40 ms, the `getPath` option
   properties kept, no new error. It refuses a new name a changed file
   already uses (conservative: a name in another scope refuses too).
   The server detects its languages at start: `go.mod` at or above the
   directory gives Go (also when neither is there), `tsconfig.json`
   gives TypeScript. Tools are the union; a tool both have keeps Go's
   schema and adds "In TypeScript files: ..." to its description. In a
   Go-only repo the tools and instructions are byte for byte what they
   were. A TypeScript file given to a tool TypeScript does not have yet
   is refused, naming the ones it has. The engine's `ParseBatch` is
   generic, so batches of TypeScript steps work. Projects now read the
   sources of the projects they reference (`UseSourceOfProjectReference`,
   as the language server does): without it, Hono's runtime-test
   projects reported 67 errors about output not built.
   Hono has 4 errors under TypeScript 7 (in tests, code written for
   6), so the engine refused every step there. The user chose to relax
   the rule for TypeScript, for now: a workspace may tolerate errors
   (`refactor.ToleratesErrors`, which TypeScript's does), and a step
   there must add none, compared by message without positions, so an
   error that only moved is not new; a batch must end, and pass each
   checkpoint, with none it did not begin with. Go keeps its rule. On
   Hono the rename then applies (47 edits, 5 files, 5 s with the
   re-check on 2 cores).
4. **Read and check:** `inspect`, `check`, `undo`, `batch` across
   languages, `replace_text`; descriptions chosen by what the root holds.
   `check` (without tests), `undo` and `batch` came with step 3; a batch
   over both languages is refused. `inspect` *done*: Go's query and
   layout (a file with its function bodies elided, a function, lines,
   targets, names across the solution, an overview by directory), from
   `tsapi.Outline` (functions, classes and their members, interfaces,
   types, enums, variables, arrow functions bound to a const). A body's
   facts are its length only; `uses` is refused; a name several files
   declare is qualified by file (`src/shed.ts:prune`). Its description
   names TypeScript where the repo has it. On Hono: 324 files, 1,504
   declarations; `hono-base.ts` shows 362 of its 564 lines without
   bodies, its types and doc comments being much of it.
5. **Edits first:** `create_file`, `add_declaration`,
   `replace_statements`, `insert_statements`, `replace_expression`,
   `add_property`, `organize_imports`, with stubs from usage. These made
   the "test first through the tools" workflow possible for Go.
   First part *done*: `create_file`, `add_declaration` (at the end, or
   after or before a declaration by name, a class member included) and
   `replace_text` (any file under the root, `files`, `all_occurrences`),
   in `internal/ts/edits`, with Go's arguments. Test first works in a
   batch checked at its end: the test and what it calls go together.
   Second part *done*: `replace_statements` (lines of whole statements
   of one block, or `function` for a whole body, an empty one or an
   arrow function's too), `insert_statements` (before, after, into) and
   `replace_expression` (parenthesized where needed; `all_occurrences`
   by tokens and symbols). The syntax is in `tsapi/stmts.go` and
   `expr.go`. Without Go's `imports` and `case` arguments yet; checked
   on Hono (a body, a block, an expression in 25-170 ms after the
   first step, which pays 4.6 s for the declaration signatures, as
   the spike found).
   Imports *done*: as for Go, the engine fixes a step's imports, through
   an internal `fix_imports` (`internal/ts/edits/fiximports`), not a
   tool: a name that one file of the solution exports is imported, into
   an import of its module or a new one in the file's quotes and
   semicolons; with `noUnusedLocals`, imports left unused are removed
   (the language service's organize imports). An edit refused for its
   errors runs again as a batch, which adds them. The engine's importer
   names the errors it clears (`refactor.ImportFixer`), gets only those
   the steps added where errors are tolerated, and makes a second pass
   for what the first left. Sorting imports, the rest of organize
   imports, is not offered: no step needed it.
   `add_field` *done*, under Go's name (the user's choice, by the rule
   above), with `add_property` as another name a batch step or the CLI
   may use (`refactor.ToolName`): properties into an interface, a class
   or a type alias of an object type, after or before a member or at the
   end, a type on one line opened up into lines.
   Stubs *done*, so step 5 is: as for Go, a batch checked at its end
   declares what its steps call and nothing declares, through
   `create_from_usage` (`internal/ts/edits/createfromusage`), typed from
   the first call (parameters by the arguments, the result by what the
   call's place expects, `void` for a statement): an exported function
   in the module a test imports it from, a function in the file, a
   method in the class (a signature in the interface) it is called on.
   The engine's declarer now reads its own errors
   (`refactor.DeclarerOf`: `Missing`, `Declared`); Go's regexes and
   `declaredNames` moved into Go's `create_from_usage`. A name the
   compiler suggests another for ("Did you mean") is left alone.
6. **Signatures** (Go's most common by-hand step): add / remove / move /
   introduce parameter, change_return_type, convert_params_to_object.
   First part *done*: `add_parameter`, `remove_parameter`,
   `move_parameter` (`internal/ts/refactorings/changesig`, the core in
   `tsapi/signature.go`): a function, an arrow function bound to a
   const, or a class method, by name, at its declaration and every call
   in every project, merged as rename merges. Refused: a reference that
   is not a call (a value: `xs.map(f)` would pass an index), a spread
   argument, overloads, constructors, a body that reads the removed
   parameter or `arguments`, a value whose names do not mean the same at
   every call (`ResolveName` at each call), and a dropped or reordered argument
   with effects (judged by step 7's analysis). Where
   `noUnusedParameters` is on, a new parameter must start with `_` (the
   refusal says so; Hono has it on). On Hono the reference search takes
   about 45 ms; the step's time is the engine's diagnostics (8.7 s the
   first time on a clone without `node_modules`, 11k errors).
   Second part *done*: constructors (`Class.constructor`: the news of
   the class and the super calls of classes extending it; refused: a
   subclass without a constructor of its own, the class used as a value,
   `new C` without parentheses, removing a parameter property);
   `introduce_parameter` (by function and expression text; the
   expression in each call's own terms, parameters becoming arguments
   and `this` the receiver; refused: a local, a name a call sees
   otherwise, an argument that could have effects, state the body may
   change before, a loop or closure); `convert_params_to_object` (a new
   tool, no Go counterpart: the parameters destructured in one object,
   `type_name` for an interface, `= {}` when all are optional). The
   lookup and call search are `tsapi`'s `target`, shared by all four.
   `add_result` *done*, so step 6 is but for overloads:
   `change_return_type` is its alias, by the rule for names. The result
   becomes an object holding the old result (`kept`, default `value`)
   and the new field; every return gives `value`; every call that uses
   the result reads `.value`, which works inside any expression, so
   unlike Go no call is refused for where its result goes; a call as a
   statement stays; an expression-bodied arrow gets `({ ... })`, and a
   recursive call inside a return reads its own `.value`. Refused: a
   function returning nothing, async functions, generators.
   Overloads *done*, so step 6 is: every one of these tools changes the
   overload signatures with the implementation, at the implementation's
   positions (overloads may name parameters otherwise): add and
   introduce insert into each overload, refusing one too short to take
   the parameter where calls matching it would pass it; remove takes it
   out of each overload that has it; move reorders each that has the
   whole range, refusing one with part of it; add_result wraps each
   overload's result type (refusing one with none written);
   convert_params_to_object gives each overload an object of its own
   types keyed by the implementation's names (no `type_name` then). The
   overloads' names are skipped by the call search.
7. **Effects analysis** (section 4) as one `analysis` package with a
   `Move` question, built before the refactorings that need it.
   First part *done*: `tsapi.Effects` (in `tsapi`, not a package of its
   own, since it asks the checker) says what evaluating an expression
   may do: write (a call or assignment not known, a getter, a spread,
   `valueOf` through an operator on what is not a number, bigint,
   string, boolean or enum), throw (a property or method of what may be
   null or undefined, a non-null assertion included; `in` and
   `instanceof`), read (a `let`, `var`, parameter or property) and make
   a new value. Known: arithmetic and templates on those types, `Math`
   but `random`, string and array methods that change nothing, and
   `map`, `filter` and the like with an arrow callback judged by its
   body. The signature tools ask it in place of the syntactic check:
   remove may drop an argument that neither writes nor throws, move may
   reorder arguments of which at most one writes or throws and none
   reads while another writes, and introduce_parameter may move an
   expression that may be dropped and duplicate an argument that may
   be evaluated twice. Refusals name the part: "removing the argument
   next() of the call at use.ts:4 would drop the call next() at
   use.ts:4". Decision 4 taken strict for now.
   Second part *done*, so step 7 is: the module's own functions are
   judged by their bodies (a function, a const bound to one, a static
   or private method, which nothing overrides; not async, generators,
   overloads or a recursive call), without their own locals and
   returns, and refusals name the call. Locals are tracked apart: a
   variable or parameter that no function inside its own assigns, so
   that a call cannot change it; what assigns or reads one says which.
   Statements have effects too, with a return, or a break or continue
   that jumps out of them, as leaving. `Move{Expr, Past, Times}` and
   `Check` are Go's `analysis.Move` for TypeScript: whether code may be
   evaluated past other code (they must not both write, throw or
   leave, one write what the other reads, or one assign a local the
   other uses) and another number of times (`Maybe`, `Several`,
   `Shared`, `Unused`, as in Go). The tools' own checks (drop, repeat,
   reorder) are the same rules; step 8's refactorings ask `Check`.
8. **Simplify:** extract / inline variable and function, invert_if,
   remove_else, move statements.
   *Done* (`internal/ts/refactorings/simplify`, the logic in `tsapi`'s
   simplify.go, extractfunc.go and inlinefunc.go), each asking step
   7's `Check`:
   - `extract_variable` inserts `const name = e;` before the
     statement: `e` moves past what runs before it there (`Maybe` after
     `&&`, `?.` or in a branch; `Shared`, with the whole loop as what
     runs between, in a loop's condition); its names must mean the same
     there. all_occurrences takes the same expression later in the
     block where nothing between could change it. Refused: an
     assignment's target, a method called through it (its `this`), part
     of an optional chain, an expression in no block. `pointer` is
     Go's and refused.
   - `inline_variable` replaces each use of a `const` or never-assigned
     `let` (one declarator of several keeps the others): its value
     moves past what lies between the declaration and the use, the
     whole loop when the use is in one; several uses are `Several`. A
     use inside a function takes only a constant value.
   - `move_statement_up` / `_down` swap two statements with their
     comments: refused when one uses what the other declares (a
     function declaration aside, as it is hoisted) or when `Check`
     finds the order matters.
   - `invert_if` negates (exactly: `===`/`!==`, `!`, De Morgan, `<`
     and `>=` only for strings and bigints, as a number may be NaN) and
     swaps; a new first branch that cannot fall through un-nests the
     other unless keep_else; an if without else becomes a guard as the
     last statement of a function's body (`return;`) or a loop's
     (`continue;`), and one whose branch leaves takes what follows it
     as its new branch (`if (c) { T } R` to `if (!c) { R } T`, with a
     `return` or `continue` after R when R can fall through). `remove_else` un-nests the else after a branch that
     cannot fall through, an else-if becoming a separate if. Both
     refuse names that un-nesting would make clash, and re-indent
     without touching lines inside a multi-line string or template.
   - `extract_function` makes a function after the top-level
     statement (a `private` method after the member when the lines use
     `this` or `super`, `static` in a static one; `async` with `await`):
     locals read become parameters, typed at their first use (declared
     type when the lines assign them); those declared or assigned and
     used outside the lines come back as the result, one as itself,
     several as an object (`const { a, b } = f(...)`, or
     `({ a, b } = f(...))`). Lines that end the function's body keep
     their returns (`return f(...)`). Lines that return earlier return
     a value the call returns unless it is undefined (`const result =
     f(...)`, then `if (result !== undefined) { return result }`), when
     no return's value may be undefined, or, when every return is bare,
     whether they returned (`if (f(...)) { return }`). Refused: returns
     of what may be undefined, bare and valued returns mixed, a jump
     out, `yield`, `arguments`, a type parameter or local type, mixing
     declared and assigned results, declarations at the module's top
     level. Go's `function`/`case` address is refused for now.
   - `inline_function` inlines functions, consts bound to one, and
     static or private methods (`this` becomes the receiver): a body
     that is one expression anywhere, statements where the call is a
     statement, a declaration's or assignment's value, or a return.
     An argument replaces its parameter when `Check` allows it (others
     are passed before it, the body after it; `Unused`, `Maybe` or
     `Several` by its uses), else it is declared before the statement
     (`const p: T = arg;`, as `p2` to `p9` when `p` is taken there). all_calls takes the file's calls; the
     function goes when nothing else uses it, unless exported.
     Refused: overridable methods, async and generators, generics, rest
     or destructured parameters, overloads, early returns, `super`,
     recursion, names the body sees otherwise at the call.
   Left for later: extract_function on a switch case, calls of
   inline_function in other files (they are inlined, but all_calls
   takes only the file's), and declaring a generic function.
9. **Move:** move_declaration, move_file, delete_file.
   *Done* (`internal/ts/refactorings/move`, the logic in `tsapi`'s
   move.go):
   - `move_file` moves a file or directory through the language
     service's file rename: the module specifiers that refer to it, its
     own relative imports and the tsconfig's file lists change; other
     files move byte for byte.
   - `delete_file` refuses a file another file still imports (for a
     directory, from outside it), naming the imports.
   - `move_declaration` moves top-level declarations (a function with
     its overloads, class, interface, type, enum, a one-variable
     statement), with the comments above them, to a file it creates
     when needed: exported when anything outside uses them; the old
     file's imports they use follow them (re-specified for the new
     place), and those only they used go; the old file imports what it
     still uses of them; other files' imports of them from the old file
     change to the new one (the whole specifier when all its names
     move, else split), and the destination's own import of them goes.
     `also` and `with_dependencies` as in Go. Refused: a declaration
     that stays and is not exported but is used by what moves, a name
     the destination declares, a default export, a namespace, several
     variables in one statement, a namespace import of the old file
     that uses what moves, `export *` of it, `import()` or `require()`
     of it. Go's `new_name` is refused: rename before or after.
   Left for later: a moved declaration that the destination reaches
   through a name of its own (an alias), and deleting an old file left
   empty.
10. **Shapes:** fold, parameterize, merge_duplicate_function,
    remove_dead_code, extract_interface, union_switch_to_method.
    *Done* (`internal/ts/refactorings/shapes`, the logic in `tsapi`'s
    shapes.go, fold.go and types.go). One matcher compares code with a
    pattern node for node: names mean the same but for the pattern's
    own locals, paired one to one with the code's, and its holes
    (parameters, and `this` for a method), which match any expression.
    - `fold`: a body that is one expression (or one return) matches
      expressions anywhere in the solution; a body of statements
      without a result matches runs of statements in any block. Each
      match becomes a call where `Check` allows each argument's move to
      the call (evaluated once, before the body), its type is
      assignable, the function's name means it there (or the file can
      import it, which it then does), and a local the statements
      declare is not used after them; the result lists the matches
      left and why. Methods: private ones, or of a class nothing
      extends.
    - `parameterize`: functions (or methods of one class) the same but
      for literals become calls of a new function taking them, after
      the last of them.
    - `merge_duplicate_function`: the same signature and bodies;
      every use, imports included, turns to the one kept, imported
      where needed (it must be exported to be seen elsewhere).
    - `remove_dead_code`: top-level declarations and class or
      interface members nothing uses (imports and re-exports count),
      with the imports only they used; not a member that implements
      or overrides one of a base type, nor a value whose evaluation
      does something. Exported code goes with a note.
    - `extract_interface` (Go's extract_struct): an interface's
      members move into a new one it extends; a type alias of an object
      type becomes `New & { rest }`; a class gets an interface of the
      members' signatures and implements it.
    - `union_switch_to_method` (Go's type_switch_to_method): a switch
      on a discriminant property, or an instanceof chain, over a union
      of the solution's classes, becomes a method of each class; the
      subject becomes `this`, the locals used parameters; every class
      needs a case, and no default or final else.
    Left for later: fold of near-duplicates that differ in constants
    (Go's `%s` and concatenation matches), prepare_switch_for_types'
    one-case-at-a-time path, and a class hierarchy (a base class) in
    union_switch_to_method.
11. **Measuring:** `refactor-ab` with TypeScript tasks: translate four
    or five (warehouse, notify, billing) with tests, and the CLAUDE.md
    variants. First A/B: mcp vs manual on the features run. Then a
    mixed task (a Go handler and its TypeScript client).
    Before it, every tool was run on Hono (`docs/ts-hono-pass.md`): the
    four style bugs it found are fixed, and so are its three gaps
    (extract_function with an early return, invert_if of a returning
    if before the last statements, inline_function choosing another
    name for a parameter whose name is taken).

## 7. Decisions open

1. **Option A** (Go + typescript-go-internal), with the official Go API
   as the planned swap; confirmed or not by the spike.
2. **Test runner for TypeScript eval testdata:** `node:test` (no
   install) or vitest (what most TypeScript projects use).
3. **Formatting:** format inserted text with the compiler's formatter
   and project settings, not Prettier; or run the project's Prettier on
   changed ranges when it is installed.
4. **Nullish dereference and throwing past a call:** take Go's rule
   (allow moving it past calls that return) or start strict. Started
   strict (step 7): what may throw is not dropped, duplicated or
   reordered with another that writes or throws. Revisit with refusals
   from real repos.
5. **Strictness:** refuse refactorings in files compiled without
   `strict`, or apply them with a note (proposed: note).

## Sources

- [Announcing TypeScript 7.0](https://devblogs.microsoft.com/typescript/?p=5246)
- [typescript-go discussion #481: a public Go API for embedding](https://github.com/microsoft/typescript-go/discussions/481)
- [buke/typescript-go-internal](https://github.com/buke/typescript-go-internal)
- [typescript-go #2244: port update imports on file rename](https://github.com/microsoft/typescript-go/issues/2244)
