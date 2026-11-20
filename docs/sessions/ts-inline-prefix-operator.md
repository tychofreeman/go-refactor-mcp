# inline of a const read under a prefix operator

- **Item:** issue #710: TypeScript's `inline` refused `const empty = ...;
  if (!empty)` as "empty is assigned".
- **Date:** 2026-11-20
- **Branch:** ts-inline-prefix-operator

## Plan

- **The easy change:** `Lined.Assigns` counts a prefix unary expression
  as a write only for `++` and `--` (`IsUpdate`), as `isAssigned`,
  the effects analysis and convertloop already do.
- **The places today:** one (`Assigns`; the other prefix checks were
  read and already ask `IsUpdate`).
- **The refactorings:** none.

## Notes

- refusal: replace_statements of the case header was refused (a case
  header cannot be replaced); replace_expression of its condition did it.

## Measured

Three edit steps (replace_expression, add_element of the test); no
writes by hand; one refusal, above.

## Assessment

Through the tools; the change is one condition. `Assigns` also serves
extract_variable's parameter form and introduce_parameter, which were
refused the same way for `!x` and `-x`.

## Follow-ups

None.
