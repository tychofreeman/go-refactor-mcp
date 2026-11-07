# add_declaration places by kind

- **Item:** jrefactor's #279 (the user, 2026-11-07: "the model should not be concerned with
  layout"), brought to Go: `add_declaration` decides where declarations go, and `after` and
  `before` are retired, accepted and ignored.
- **Date:** 2026-11-07
- **Branch:** place-by-kind

## Plan

Where a top-level declaration sits does not change what a Go program does (package
initialization follows dependencies; only independent package variables with side effects, and
several `init` functions in one file, run in the order written). So placement is the tool's:

- a function or method that replaces a stub in the same file goes where the stub was;
- a method goes after the last method of its receiver type in the file, else after the type;
- anything else at the end of the file.

Content with several declarations is split by declaration and each placed; neighbours that go to
one place stay together, in their order. `after` and `before` leave the schema (`schema:"-"`) and
are ignored. Constants and variables stay at the end: grouping them with the others would move a
variable with side effects ahead of independent ones.

Derivation: red goldens first (the after/before goldens reworked: a method after its type, after
its type's methods, a retired `after` ignored, a stub replaced in its place); then a `places`
function in place.go that `Plan` calls in place of `offset`; then `offset`, `declNamed` and
`kindOf` deleted as unused. The description, checks.md, CLAUDE.md, parity.md, the session notes
and how-this-project-was-built.md follow.

## Notes

- plan: the goldens of `after`/`before` that only placed by name (after_name, before_method,
  before_name, refuse_unknown_method) are deleted; testdata moved with git mv.
- hand: the testdata moves (git mv, git rm, mkdir, cp of a go.mod); friction log.
- read: `adddecl.Plan`, `place.go` and `stub.go`, to place the new code where `offset` was and
  to reuse `isStub`, `funcKey` and `recvTypeName`; `batchargs.argsProblem`, after a server test
  sent `before` in a batch and the argument check refused it (the struct still decoded it).
- plan: the batch argument check refuses arguments the schema lacks, so the retired names go in
  `refactor.unlistedArgs`, accepted wherever a schema no longer has them.
- bulk: `places`, `place` and `declaresType` (90 lines), new behavior.
- plan: `convert_params_to_object` composes add_declaration `before` the function, so five of
  its goldens moved the struct to the end; that placement is the tool's, so it passes an
  unpublished `place_before` (`unlistedArgs`, renamed from `retiredArgs`) and keeps them. The
  first try was refused by the batch argument check, as the retired names had been.
- bulk: `declNamed` (36 lines) restored from main with add_declaration from a file (`git show`
  into the scratchpad), existing code written again: the batch had stubbed it, and no tool brings
  back a deleted declaration from a commit.
## Measured

Not measured: a cloud clone, started outside the checkout, has no session log; the CLI's steps
are in the notes.

## Assessment

Through the tools: every Go change went through the CLI (add_declaration, replace_statements,
replace_expression, replace_declaration, rename); the testdata moves were by hand (friction log).
The large writes were the new placement (new behavior) and `declNamed`, restored from main after
the plan changed: deleting it, then needing it for composites, cost one stubbed batch and a
re-add. The plan held for the model-facing part; it missed that the batch argument check refuses
arguments a schema lacks, and that convert_params_to_object composes `before`, both found by the
CI run, not by reading. One change that would have helped: a search for a tool's own arguments
in composites (`grep` of `"before"` in `Expand`s) before retiring one; noted in the session notes.

## Follow-ups

- TypeScript's add_declaration (roadmap 54.9).
- Constructors written for the model, TypeScript only (roadmap 54.10).