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
| add_field | **add_property** | to interface, type literal or class |
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
   delegate to it.
3. **TypeScript engine:** `tsapi`, workspace, edits, diffs, safety rule,
   formatting of inserted text, `rename`, golden tests (`testdata/*.ts`
   in and out), wired into the dispatcher.
4. **Read and check:** `inspect`, `check`, `undo`, `batch` across
   languages, `replace_text`; descriptions chosen by what the root holds.
5. **Edits first:** `create_file`, `add_declaration`,
   `replace_statements`, `insert_statements`, `replace_expression`,
   `add_property`, `organize_imports`, with stubs from usage. These made
   the "test first through the tools" workflow possible for Go.
6. **Signatures** (Go's most common by-hand step): add / remove / move /
   introduce parameter, change_return_type, convert_params_to_object.
7. **Effects analysis** (section 4) as one `analysis` package with a
   `Move` question, built before the refactorings that need it.
8. **Simplify:** extract / inline variable and function, invert_if,
   remove_else, move statements.
9. **Move:** move_declaration, move_file, delete_file.
10. **Shapes:** fold, parameterize, merge_duplicate_function,
    remove_dead_code, extract_interface, union_switch_to_method.
11. **Measuring:** `refactor-ab` with TypeScript tasks: translate four
    or five (warehouse, notify, billing) with tests, and the CLAUDE.md
    variants. First A/B: mcp vs manual on the features run. Then a
    mixed task (a Go handler and its TypeScript client).

## 7. Decisions open

1. **Option A** (Go + typescript-go-internal), with the official Go API
   as the planned swap; confirmed or not by the spike.
2. **Test runner for TypeScript eval testdata:** `node:test` (no
   install) or vitest (what most TypeScript projects use).
3. **Formatting:** format inserted text with the compiler's formatter
   and project settings, not Prettier; or run the project's Prettier on
   changed ranges when it is installed.
4. **Nullish dereference and throwing past a call:** take Go's rule
   (allow moving it past calls that return) or start strict.
5. **Strictness:** refuse refactorings in files compiled without
   `strict`, or apply them with a note (proposed: note).

## Sources

- [Announcing TypeScript 7.0](https://devblogs.microsoft.com/typescript/?p=5246)
- [typescript-go discussion #481: a public Go API for embedding](https://github.com/microsoft/typescript-go/discussions/481)
- [buke/typescript-go-internal](https://github.com/buke/typescript-go-internal)
- [typescript-go #2244: port update imports on file rename](https://github.com/microsoft/typescript-go/issues/2244)
