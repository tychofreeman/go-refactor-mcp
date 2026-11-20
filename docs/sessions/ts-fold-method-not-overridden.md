# fold of a method no subclass overrides

- **Item:** issue #730: TypeScript fold refused `Token.is_minus` because
  `Operator extends Token`, though nothing overrides `is_minus`; the
  session went around it by a function, fold, then function_to_method.
- **Date:** 2026-11-20
- **Branch:** ts-fold-method-not-overridden

## Plan

- **The easy change:** in place of `extended` (does any class extend
  this one), `overrider`: does any class extending it, directly or not,
  declare a member of the method's name; the refusal names it.
- **The places today:** one caller of `extended`, in Fold.
- **The refactorings:** none; `extended` is deleted, its one use gone.

## Notes

- plan: the first test folded `new Operator("+").text` into
  `(new Operator("+")).is_minus()`, the parentheses of #717 (an open
  PR from another contributor, #718); the test takes the receiver as a
  parameter instead.

## Measured

One batch of 3 (add_declaration of `overrider`, replace_statements of
the check, replace_declaration deleting `extended`), two add_element
and two replace_text in test strings; nothing by hand; no refusals.

## Assessment

Through the tools. Only the program's classes are seen: a class
outside it extending an exported one is not, as in Java's rule.

## Follow-ups

None.
