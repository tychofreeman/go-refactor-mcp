# Code review: go-refactor-mcp

Nov 14, 2026

Reviewed: `main` at 75438f1 (after #635). 356 Go files outside `testdata/`: about 74,400 lines of
source and 19,900 of tests. The Go refactorings and edits are about 33,500 lines in one package
each; the TypeScript side is about 20,300 lines, 16,000 of them in one package, `tsapi`. There
are 965 Go golden cases and about 281 TypeScript cases written inline in tests. The previous
review is `docs/code-review-2026-10-20.md`.

**Issues.** The findings are filed as #636 to #643, one per bug and two for the structure
(section 2 and section 3).

**How it was done.** One reviewer, so this covers less ground than the Java review of 2026-11-13,
which had five. First the analyzers: `go vet`, `staticcheck`, `dupl` (copy-paste, 50 tokens) and
`gocyclo`, plus a count of function lengths. Then reading: the batch runner, the gate, history,
workspace containment, the server's concurrency, the reindent and type-spelling helpers. Then
about twenty behavior probes, run through the CLI on small Go and TypeScript projects, aimed at
what a type checker cannot catch.

**Confidence.** Bugs 1 to 3 and 5 were reproduced: two end to end, one in the helper alone, one
by writing through a symlink. Bug 4 was read from the code and the MCP SDK, not triggered. Items
marked *suspected* need a test before any fix.

## Overall

This codebase is in better shape than java-refactor-mcp, mostly because more of it has been
stress tested.

- **Probes found little.** Every behavior probe on a refactoring was refused correctly, with a
  refusal that says why and what to do instead. The cases:
  - Go: `remove_else` capturing an outer variable through an if's init statement; `defer` in
    `extract_function` and `inline`; a loop variable captured by a closure (Go 1.22 per-iteration
    variables); a value receiver (inlined as a copy); a slice the loop shrinks.
  - TypeScript: a `let` loop variable captured by a closure; a block-scoped `let` that would
    shadow; inlining a variable past a call that changes it; a getter that would be evaluated
    twice; cutting an optional chain.
  - `rename` checks interface satisfaction before and after, including standard-library
    interfaces and where a value becomes an interface.
- **Two Java bugs don't recur.** `replace_text` refuses an empty `old`. Each language serializes
  its tool calls behind a mutex, so the race the Java server had cannot happen.
- **Little exact duplication.** `dupl` finds 36 clone groups, about 660 lines, none long.
- **No TODOs, and almost no dead code.** `staticcheck` reports four unused functions.

The debt is in two places. First, a few text helpers exist in several drifting versions, and
three of the confirmed bugs come from them. Second, the TypeScript side is written very
differently from the Go side: one large package of long functions, with far fewer tests.

## 1. Bugs to fix first

Ordered by harm. Each is small (S) unless marked. "Silent" means the result type-checks and runs
differently, and nothing refuses or reports it.

| # | Tool | What goes wrong | Evidence |
|---|---|---|---|
| 1 | `inline` (Go), call in an expression | A multi-line raw string in the result is re-indented. `reindentTail` re-indents all but the first line, and `textutil.Reindent` then scans from the middle of the string, so it cannot see the string. Inlining a function that returns a two-line raw string into an expression two levels deeper adds two tabs inside the string. Silent. | Reproduced end to end. `inlinefunc/text.go:70`. #636 |
| 2 | `add_declaration` (TypeScript) | The model's content is re-indented by `place.go`'s `reindent`, which does not skip template literals. A template line indented by four spaces is written with two. Silent. Anything placed through `PlaceDeclarations` is affected. | Reproduced end to end. `tsapi/place.go:258`. #637 |
| 3 | `move` of a method (TypeScript) into a file with another indent step | `textutil.ReindentSteps` protects multi-line strings in its first pass, then converts indent steps on every line, including lines inside a template literal. From a 4-space file to a 2-space file, a template line loses two spaces. Silent. | Reproduced in the helper. `textutil.go:256`, from `tsapi/movemethod.go:160`. #638 |
| 4 | Every tool | No panic is recovered, either in the server or in the MCP SDK, which runs each call in its own goroutine. One panic ends the server process and loses the undo history, which the server keeps only in memory (the CLI saves its own). There are about 286 type assertions without `, ok`, and the TypeScript compiler port can panic on its own. java-refactor-mcp turns compiler exceptions into refusals (`Jdt.compilerFailed`). | Read, not triggered. `mcpserver/server.go` `handler`. #639 |
| 5 | Every edit | Containment is checked on the path's text only (`filepath.Rel`). A symlink inside the module lets `replace_text` write a file outside it. This matters little on a developer's own machine, but it would for a hosted server. `inspect` already refuses paths outside the module's packages. | Reproduced. `workspace.Contains`, and TypeScript's. #640 |
| 6 | Undo and delete | `history.removeEmpty` climbs with no bound: it removes each parent left empty, past the workspace root if that is empty too. `writeAll` writes files one at a time, so a failure part way leaves a change half written, with no rollback. Both match java-refactor-mcp's bug 18. | Read. `history.go:180` and `:197`. #641 |

Smaller ones, all *suspected*:
- `tsapi/wrap.go` re-indents TypeScript with `textutil.Reindent`, which uses Go's scanner. A
  single-quoted string or a regex literal that holds a backtick starts a Go raw string, so later
  lines are mis-detected as inside a string. It looks like this misplaces indentation rather
  than changing a string, but that needs a test to confirm.
- Type spelling has three implementations with different checks. Only `inline` refuses an
  unqualified type name that something else hides at the target. `extract_variable` (through
  `analysis.TypeString`) and `extract_function` don't check, though a wrong type usually fails to
  compile.
- The gate compares errors without file or position. An error fixed in one file and the same
  text added in another counts as no change. This is the designed tolerance for errors a workspace
  already has, but it can hide a move of an error between files.

## 2. Duplicated abstractions to consolidate (#642)

Ranked by how much harm the drift already causes. Proposed homes reuse existing packages.

| Job | Copies | Drift already causing harm | Proposed home |
|---|---|---|---|
| Re-indenting text | `textutil.Reindent`, `ReindentSteps`, `reindentTail`, TypeScript's `lined.reindent` (AST-based, correct) and `place.go`'s `reindent` (unguarded) | Bugs 1 to 3 | One rule per language: Go's in `textutil` (protect strings from the whole text, never a slice of it); TypeScript's `lined.reindent` everywhere in `tsapi`, with `ReindentSteps`' step pass skipping protected lines |
| Line start and indent of a position | `lineStart` ×7 (one TypeScript), `indentOf` ×5, plus TypeScript's `lined.indent` | None seen yet | `textutil.LineStart`/`IndentAt`, beside `NextLine`, `LineEnd` and `Indent` |
| Spelling a type for a file | `analysis.TypeString`, the `qualifier` copies in `extractfunc` and `inlinefunc`, `typeString` in `rename` and `workspace` | The hidden-name check exists in one only | `analysis.TypeString` with a scope argument, so each caller gets both checks |
| Checking a new name | `checkNewName` ×4 (`changesig`, `extractfunc`, `extractvar`, `rename`) | Not compared yet (*suspected* drift) | One `analysis.NameFree(scope, pos, name)` with each caller's own refusal |
| Small helpers | `plural` ×4 (`textutil.Plural` exists), `lowerFirst` ×3, `fileExists` ×3, `embeddedName` ×3, `objectOf` ×2, `isLocal` ×2, `firstLine` ×3 | Wording differences only | `textutil`, `analysis` |
| Effects analysis for Go and TypeScript | `internal/analysis` (6,079 lines) and `tsapi/effects.go` (1,141); only `internal/reorder` (194 lines) is shared | Inherent, since the ASTs differ. The rules the user chose, though, are kept in two places by hand | Move more of the decisions, not just the ordering, into `reorder` as language-free facts and verdicts. Keep the refusal wording in one place |

## 3. Other smells

- **The TypeScript package's shape (#643).**
  - `tsapi` is 44 files and 16,000 lines in one package, with 93 methods on `Project`.
  - 12 of the repo's 16 functions over 150 lines are there: `MoveDeclaration` (448),
    `Fold` (404), `inliner.inline` (349), `ExtractFunction` (334), `UnionSwitchToMethod` (323),
    `PrepareSwitchForTypes` (233), `ParamsToObject` (229), and others.
  - Go has a package per refactoring with smaller functions. The rule in `internal/ts/CLAUDE.md`
    that `tsapi` alone imports the compiler is good. The refactorings above it could still be
    split into packages, as Go's are, with `tsapi` exposing the AST helpers.
- **Long or complex units elsewhere.** `runBatch` is 199 lines (complexity 52), `Run` has
  complexity 45, `extraction.analyzeDataFlow` is 198 lines, and `extraction.plan` 156.
  `extractfunc.go` is 2,445 lines in one file.
- **The tests are uneven between the languages (#643).**
  - Go has 965 golden cases and opt-in stress tests that run real modules' tests.
  - TypeScript has about 281 cases inside test functions, no golden directories and no stress
    tests.
  - Behavior probes, which run the code before and after a change, exist only for
    `convert_loop` in both languages, plus an `inline` round trip in Go.
  - Bugs 1 to 3 change string values and type-check, so only probes or goldens with
    multi-line strings would catch them.
  - No test covers a multi-line string through `ReindentSteps`, `reindentTail` or `place.go`.
- **Unused code** (`staticcheck`, #642): `analysis.isInteger`, `changesig.plainValue`,
  `extraction.isString`, `inversion.returnFrom`. Also a dead assignment in `refactor_test.go:673`,
  and two simplifications in `tsapi`.
- **Deprecated API.** The roots feature (SEP-2577) is used on purpose, as recorded in the session
  notes. Expect to rework it when the SDK removes roots.

## 4. Suggested order

1. **Bugs 1 to 3** (#636, #637, #638), each with a test holding a multi-line raw string or
   template literal, and a probe where one fits. The fix is one rule for re-indenting per
   language (section 2, first row). Those can go in parallel.
2. **Bug 4** (#639). Recover in `handler` (or around `Step`, `Batch`, `Check` and `Inspect`),
   turning a panic into a tool error that names the tool and the location. Add a test that
   panics.
3. **Bugs 5 and 6** (#640, #641). Resolve symlinks before the containment check, bound
   `removeEmpty` by the workspace root, and write a change through temporary files and renames.
   Each is S with a unit test. Bug 5 should be fixed before any hosted deployment.
4. **Shared helpers** (#642): line and indent, type spelling with the scope check, name checks,
   then the small ones. Do them through the project's own tools, as CLAUDE.md asks.
5. **TypeScript tests and structure** (#643): probes for the refactorings that move code, a
   stress run on a real project as Go has, then split `tsapi`'s refactorings into packages when
   one of them next changes.

One item needs the user's choice first: whether the gate should compare errors with their file
(the last suspected item). That would tighten a tolerance chosen on purpose for TypeScript
workspaces that already have errors.
