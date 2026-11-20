# fold moved an argument past a read of what it changes

- **Item:** issue #726: extract_variable refused moving
  `pending.pop().text` before the read of `calculator.keys` in
  `calculator.keys += pending.pop().text`, while fold made the same move
  (`calculator.press(pending.pop().text)`). The issue asked which one
  was wrong.
- **Date:** 2026-11-20
- **Branch:** ts-fold-read-before-argument

## Finding

Fold was unsound. With `bump() { this.keys = "x"; return "y"; }`,
`a.keys += a.bump()` leaves "y" and `a.press(a.bump())` leaves "xy"
(node 22); fold applied it. Cause: `parts`, what else runs in a match,
broke `calculator.keys` up because its object is a hole, keeping the
bare name `keys`, so the property read was lost and the argument's
move checked against nothing.

Go's fold makes the same move, but there gc gives "xy" both ways and
the spec leaves the order of a read and a call in one expression
unspecified, so it is not a behavior change Go promises.

## Plan

- **The easy change:** in `parts`, a property or element read whose
  object is skipped (a hole) is kept whole: it reads state. Not as the
  target of a plain `=`, which is not read.
- **The places today:** one, `parts` (shared by extract_variable, fold
  and inline; all ask what runs, so all want this).
- **The refactorings:** none.

## Notes

None.

## Measured

Two edit steps (replace_statements of the switch, replace_text of the
doc comment), one add_element for the test; nothing by hand; no
refusals.

## Assessment

Through the tools. Both tools now refuse the issue's case under one
rule (`analysis` of the move). The session's fold of
`calculator.keys += pending.pop().text` is refused now too.

## Follow-ups

- A finer rule would let both apply in the issue's case: pop() writes
  `this.held` and an array's elements, which cannot be `Calculator.keys`
  (another class's string property). The effects analysis says only
  "may change state" for the call.
