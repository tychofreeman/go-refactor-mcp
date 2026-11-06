# extract_function's pointer for a variable it only reads

- **Item:** roadmap 44's defects, in the defects list and
  `docs/parity.md` section 1: extract_function passed `found` by pointer
  because a closure in the caller changes it, though the new function
  only reads it.
- **Date:** 2026-11-06
- **Branch:** extract-read-only-captured

## Plan

At the highest level: a pointer is needed when the variable can change
while the new function runs, or the new function changes it. A literal
that changes it can only change it during the lines if the lines can run
the literal. When the lines do not write the variable and cannot run any
such literal, a copy reads the same values.

- Red: a golden that copies; one that keeps the pointer when the lines
  call the literal.
- Green: `copyReads` lifts the async-capture case of the pointer rule
  when the lines do not write the variable and each literal is held in
  a local that is only called, outside the lines, not by go or defer.

## Notes

- read: the pointer rule in `flow`, `sharedWithCaller`, `writtenIn`, the
  existing pointer goldens: to place the condition.
- plan: the first version let a call of the local inside another literal
  count as outside; the lines could run that literal. A third golden
  (red), then calls inside any literal count as reachable.
- hand: the goldens' files written with the shell (testdata); one
  golden's output made by the current CLI, which was right for that case.

## Measured

Not measured: a cloud clone with no session log. Two batches:
replace_expression and add_declaration (`copyReads`, 40 lines, new), then
a replace_expression. Stress run on jsonschema-go v0.4.3 with its tests (STRESS_N=400, STRESS_TESTS=1): no broken build and no failing test.

## Assessment

- **Through the tools:** structured edits only; no hand edit of Go.
- **Large writes:** `copyReads` (new behavior).
- **Plan:** held, with one hole found by thinking through the rule, not
  by a test; the stress run is the check for others.
- **Reading:** bodies read to place the change.
- **One change that would have helped:** none.

## Follow-ups

- add_parameter on a function literal bound to a local.
