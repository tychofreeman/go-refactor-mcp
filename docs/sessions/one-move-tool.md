# One move tool

- **Item:** roadmap 54.1, one `move` for declarations, members and files
  (the user, 2026-11-05: "easier for the model to reach for"), Go and
  TypeScript first. From `docs/parity.md`, section 2.
- **Date:** 2026-11-05
- **Branch:** one-move-tool

## Plan

At the highest level: three tools that do one operation (take a named
thing elsewhere, update its references) are published as one, which
stands for the three and chooses among them by its arguments; the three
stay as they are, unpublished, so their markers (edit, compile-checked)
and refusals are unchanged and their names still work.

- A `refactor.Router`: a tool whose `Route(args)` names the tool its
  arguments ask for, or refuses them, and `Covers()` the tools it
  publishes in place of. Language-neutral, since Go's and TypeScript's
  three move tools take the same argument names.
- `move` (`refactor.Move`): `to_file` moves a declaration
  (move_declaration), `name` with `to` a method (move_method), `to`
  without `name` a file or directory (move_file); anything else is
  refused, saying which to give.
- Where a tool is looked up by name to run it (Go's and TypeScript's
  `Step`, a batch step, the CLI), a Router is replaced by the tool it
  routes to. `Tools()` leaves out the tools a Router covers.
- Test first: Route's cases; a Go and a TypeScript `move` of each kind
  through the language; a batch step; `Tools()` without the three.
- Then the published text: move's description per language (one
  sentence, one example per case, under 1,700 characters), the
  instructions and examples that name the old tools.

The old names keep working, so recipes and the baselines' calls still
run; the published list changes, so this waits for an A/B check before it
is merged (the user decides on the run).

## Notes

- read: `refactor inspect` for every Go read, after the user caught
  grep and sed on Go files in the previous item.
- read: the four argument structs, `RefactoringOf`, `ParseBatch`, the
  registry, each language's `Step` and `Tools`, and the CLI's lookup: to
  find where a tool is looked up by name (four places) and which markers a
  wrapper would lose (`Edit`, `ChecksByCompiling`, `LocalsOnly`).
- plan: a wrapper whose Plan calls the chosen tool's was ruled out by
  those markers; routing at lookup keeps them.
- refusal: a test calling `NewMove[...]` got no stub (instantiation not
  seen as a call); the stub went in by create_file in the same batch.
- refusal: insert_statements after a blank line; the closing brace's line
  was the one.
- bulk: create_file of `internal/refactorings/move.go` and
  `internal/lang/typescript/move.go` (19 lines each, mostly the new
  descriptions), the routing functions by replace_declaration (new
  behavior), and three tests by add_declaration (new).

## Measured

Not measured: a cloud clone with no session log; 16 batches through the
CLI, one inspect per read.

## Assessment

- **Through the tools:** every Go change was a batch: create_file for new
  files, replace_declaration for the stubs, add_element to the registries,
  insert_statements and replace_expression for the four lookups, and
  replace_text only on string literals and docs. No hand edit of Go.
- **Large writes:** the two description files and the routing code are
  new behavior; no existing code was typed again.
- **Plan:** held, after the change from a wrapper to routing at lookup.
- **Reading:** bodies were read to find the lookup sites and markers;
  nothing was read to check a refactoring.
- **One change that would have helped:** stubs for a generic function
  called with type arguments (friction log).

## Follow-ups

- The A/B check of the published list (ask the user first).
- `rename` of a file path (54.1).
- Java's move_type and move_member as `move` (its row 109).
