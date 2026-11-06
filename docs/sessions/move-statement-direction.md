# move_statement with a direction

- **Item:** `docs/parity.md` section 2, the shared names: Java's
  move_statement takes a direction; Go and TypeScript published
  move_statement_up and move_statement_down. The user: "do items 1-3"
  (1 the merges, 2 preserve_exceptions, 3 TypeScript 52.2/52.3).
- **Date:** 2026-11-06
- **Branch:** move-statement-direction

## Plan

At the highest level: two tools that are one operation with a flag are
published as one with the flag as an argument, as `move` and `inline`
were; the two stay, unpublished, so old names and recipes run.

- Red: a language test each: move_statement down and up, a bad direction
  refused, the published list.
- Green: `refactor.MoveStatementOf`, a Covering tool whose Plan picks up
  or down and passes only the arguments that tool takes (`OnlyArgs`); a
  constructor per language with the merged description; registered.

## Notes

- read: move.go's Router and Routed, inline's Plan, the up/down tools:
  Routed passes the arguments unchanged, and `direction` would be
  unknown to the up/down tools, so a Covering Plan rather than a Router.
- refusal: add_element with `after` on a slice literal (keys only); sent
  without it.
- found: `inline` missing from refactor-tokens' `refactoringTools`.

## Measured

Not measured: a cloud clone with no session log. create_file (48 lines,
new), two add_declaration (constructors with descriptions), add_element
for the registries and the tokens table, replace_statements in a test,
replace_text on the instructions.

## Assessment

- **Through the tools:** structured edits; no hand edit of Go.
- **Large writes:** movestatement.go (new behavior), the two tests (new).
- **Plan:** held.
- **Reading:** bodies read to choose Router or Covering.
- **One change that would have helped:** a test that the tokens table
  names every published refactoring would have caught `inline`
  (roadmap 48.8 asks for it).

## Follow-ups

- change_type for change_field_type and change_parameter_type;
  delete_file into move. The A/B check of the merged descriptions.
