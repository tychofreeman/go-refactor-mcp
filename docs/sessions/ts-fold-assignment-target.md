# fold does not match an assignment's target

- **Item:** issue #712: TypeScript `fold` of a function that returns its
  parameter (`return held;`) matched `out` in `out += names` too, and
  the whole fold was refused for `written(out) += ...`.
- **Date:** 2026-11-20
- **Branch:** ts-fold-assignment-target

## Plan

- **The easy change:** in Fold's loop over matches, a match that is
  written (`isAssigned`: the left of an assignment, the operand of `++`
  or `--`) is no match: a call can stand for a value, not a target.
- **The places today:** one.
- **The refactorings:** none.

## Notes

None.

## Measured

Two edit steps (add_element of the test, insert_statements of the
check); nothing by hand; no refusals.

## Assessment

Through the tools; three lines. A target inside a destructuring
assignment or a for-of's head is not checked by `isAssigned`; no
body that is a lone parameter has met one yet.

## Follow-ups

- #714: a small body matches widely without `line`.
