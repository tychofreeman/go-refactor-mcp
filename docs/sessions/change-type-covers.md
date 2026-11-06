# change_type for field and parameter types

- **Item:** `docs/parity.md` section 2: Java has one change_type; Go and
  TypeScript published change_type, change_field_type and
  change_parameter_type. Second of the merges (the user: "do items
  1-3").
- **Date:** 2026-11-06
- **Branch:** change-type-covers

## Plan

At the highest level: three tools that change a declared type, told
apart by what is named, published as one. change_type is an edit and the
other two refactorings, so the choice is made at lookup (a Router), not
in a Plan, so that each keeps its markers; change_type routes to itself
for its own case.

- Red: a language test each: change_type on Type.Field and with
  parameter; the published list.
- Green: `refactor.ChangeTypeRoute` (parameter, values or a name with a
  dot); Route and Covers on both change_type tools; their arguments gain
  parameter and values for the schema; merged descriptions.

## Notes

- read: the three tools' arguments and descriptions in both languages,
  and the instructions' lines.

## Measured

Not measured: a cloud clone with no session log. One batch of five
structured steps, then replace_text on descriptions and instructions.

## Assessment

- **Through the tools:** structured edits; no hand edit of Go.
- **Large writes:** the two tests (new).
- **Plan:** held.
- **Reading:** arguments and descriptions, to merge them.
- **One change that would have helped:** none.

## Follow-ups

- delete_file into move; the A/B check of the merged descriptions.
