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

## Measured

## Assessment

## Follow-ups
