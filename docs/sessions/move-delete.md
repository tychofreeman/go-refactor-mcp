# delete_file into move

- **Item:** `docs/parity.md` section 2: move, copy and delete a file are
  one `move` with options. Third of the merges (the user: "do items
  1-3").
- **Date:** 2026-11-06
- **Branch:** move-delete

## Plan

At the highest level: deleting a file is a move to nowhere; `move` with
`delete` routes to delete_file, which stays as it is, unpublished.

- Red: TestMove in both languages: `move` with delete removes a file;
  delete with a destination is refused; delete_file is not published.
- Green: `delete` in moveArgs, a case in Route, delete_file in Covers.

## Notes

- plan: Routed passes the router's arguments to the tool unchanged, and
  `delete` is not delete_file's; Routed now drops the router's own
  arguments that the target lacks (`withoutOwn`), keeping unknown ones
  for the tool to refuse. This was done with the new test red, out of
  TDD's order: it should have been a green refactoring first (Routed
  returning its arguments unchanged), then the test.

## Measured

Not measured: a cloud clone with no session log. Two batches of
structured steps (Routed's declaration replaced, its four callers, then
the move changes) and replace_text on descriptions and instructions.

## Assessment

- **Through the tools:** structured edits; no hand edit of Go.
- **Large writes:** `withoutOwn` (28 lines, new behavior).
- **Plan:** changed once (Routed), as noted; TDD's order broken there.
- **Reading:** the four call sites, to change them.
- **One change that would have helped:** add_result on Routed would
  have carried the new result to its callers; it was not tried.

## Follow-ups

- The A/B check of the merged descriptions (move, inline,
  move_statement, change_type).
