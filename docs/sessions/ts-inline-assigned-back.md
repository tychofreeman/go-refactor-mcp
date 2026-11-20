# inline of x = f(x) without a copy

- **Item:** issue #724: TypeScript `inline` of `calculator =
  enter(calculator, operand)`, where `enter` assigns its parameter and
  returns it, left `let calculator2 = calculator; ...; calculator =
  calculator2;`, not the statement extract_function started from.
- **Date:** 2026-11-20
- **Branch:** ts-inline-assigned-back

## Plan

- **The easy change:** a parameter the body assigns is bound to a copy;
  when the call's statement is `x = f(x, ...)`, the body returns that
  parameter last, and nothing but the body sees `x` while it runs, the
  parameter can be `x` itself, and the final `x = x;` goes. "Nothing
  else sees x": a local of the caller that no function in it captures,
  no `try` around the call (a throw would leave `x` half-written where a
  catch could read it), and no other argument naming `x`.
- **The places today:** the binding decision in `inliner.inline` and
  the last statement it writes; one function, so a new method
  `assignedBack` and two conditions.
- **The refactorings:** none.

## Notes

None.

## Measured

Six edit steps (two add_element of the tests, add_declaration of
`assignedBack`, insert_statements, two replace_expression); nothing by
hand; no refusals. The batch ran with check "end" because `back` was
unused until its third step.

## Assessment

Through the tools. `assignedBack` (about 50 lines) is new behavior.
The stress run on Hono (session notes) was not run; the change applies
only to `x = f(x)` with a body ending `return p`.

## Follow-ups

- #727: fold of such a function against a statement of the same shape.
