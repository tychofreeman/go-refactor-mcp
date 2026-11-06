# TypeScript's extract_function for a value with no case

- **Item:** roadmap 41.8, in the defects list and `docs/parity.md`
  section 1: Go's extract_function by `case` adds, for a value with no
  case, a case that calls a copy of the code after the switch;
  TypeScript refused.
- **Date:** 2026-11-06
- **Branch:** ts-case-tail

## Plan

At the highest level: a value with no case runs the code after the
switch, so extracting "its case" means extracting that code, keeping it,
and calling the new function from a new case. Go does this by
rewriting the call site into an insertion at the switch's closing brace;
TypeScript's extraction returns edits, so the same rewrite is done to
them after it.

- Red: an applied case and two refusals (a default; a case that falls
  through).
- Green: `tsapi.MissingCase` answers CaseBody's "no case" refusal under
  Go's conditions and gives the lines; `Place` moves the call edit into
  the new case.

## Notes

- read: Go's missingCase and its use, the goldens case_missing and
  refuse_case_missing, TypeScript's CaseBody and extract_function's Plan:
  to copy the conditions and see where the lines enter.
- refusal: none from the tools. One older test ("no such case") expected
  the refusal the new rule replaces; it was given a default so it still
  pins the refusal.

## Measured

Not measured: a cloud clone with no session log. Through the CLI: one
add_declaration (the type and two functions, about 90 lines, new
behavior), two replace_statements in the Plan, one add_declaration of
the test, replace_text on two string literals and the docs.

## Assessment

- **Through the tools:** every change was a structured edit or
  replace_text on a literal; no hand edit of Go.
- **Large writes:** MissingCase and Place (new behavior), the test (new).
- **Plan:** held.
- **Reading:** bodies read to copy Go's rule and to place the change.
- **One change that would have helped:** none.

## Follow-ups

- Go's three item-44 defects.
