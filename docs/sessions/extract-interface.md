# extract_interface for Go

- **Item:** roadmap 51.2, the first of the remaining suggestions the user
  asked for after split (2026-11-04).
- **Date:** 2026-11-04
- **Branch:** extract-interface (from split-phase, #544)

## Plan

At the level of the operation: name part of a type's behavior so code
can depend on just that part. Go's interfaces are implicit, so the step
is a declaration that changes nothing that runs, plus `var _ I =
(*T)(nil)`, the counterpart of TypeScript's `implements`. From an
interface it is TypeScript's other case: the methods move into the new
interface, which the old one embeds (TypeScript's `extends`). The same
name and arguments as TypeScript's (`fields`, `new_type`), so the merged
schema of a project with both languages stays one shape.

Derivation, in TDD's order: no reshaping; nine goldens and the test by
create_file (`New` stubbed); the code by add_declaration, registered by
add_element.

## Notes

- The shared-line golden could not be written: create_file formats Go,
  and gofmt puts each method of an interface on its own line. The check
  stays, for files gofmt has not formatted.
- Server tests: `Rate` in the example is a name in an eval task (now
  `Spray`); the instructions must list the tool; and the instructions
  went 12 characters over their cap, so two phrases were cut ("an
  existing function", "data" from the files line) rather than raising it.
- Stress (rollbacks only; a declaration and an assertion run nothing):
  x/tools 490 of 499 types applied, uritemplate 13 of 13, jsonschema-go 7
  of 7. Refused: generic types, and one method whose signature needs an
  import the type's file lacks.
- By hand: nothing.
