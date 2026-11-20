# fold of a function that assigns its parameter and returns it

- **Item:** issue #727: after extract_function wrote `rpn = press(rpn,
  text)` and `function press(rpn, text) { rpn += text; return rpn; }`,
  fold refused the next `rpn += ...;` ("press assigns its parameter").
- **Date:** 2026-11-20
- **Branch:** ts-fold-assigned-back, stacked on ts-inline-assigned-back
  (#734), whose rule it shares.

## Plan

At the highest level: the inverse of #724. A body whose only return,
its last, returns a parameter it assigns matches a copy of the
statements before that return, the parameter standing for a variable,
which becomes `y = f(...)` where nothing but the copy sees y.
- **The easy change:** in Fold, accept that one assigned parameter;
  try only the copy without the return for it; mark the match `back`;
  check y (a variable, `unseen`, named by no other argument, of a type
  that takes f's result); write `y = ` before the call.
- **The places today:** Fold (the parameter check, the window loop,
  the per-match checks, the text), and `assignedBack`'s visibility rule
  in inlinefunc.go, to share.
- **The refactorings:** split `unseen` out of `assignedBack`.

## Notes

- hand: `unseen` was written by add_declaration and its lines in
  `assignedBack` replaced by a call (replace_statements), not
  extracted: the lines return -1 early, so extract_function would give
  the `done, v` form, not a predicate. The code was this session's own,
  from #734.
- refusal: the batch's first try declared `returned` and `back`
  unused (check each); retried with check "end".
- refusal: replace_statements over part of the window loop, and over a
  case clause with its header; retried over the whole loop and the
  whole switch.
- refusal: replace_declaration of `found.goesOn` (a type local to Fold);
  add_field with its line did it.

## Measured

One batch of 8 steps (after four retries), one of 3 for the
refactoring, two add_element for the tests; nothing by hand outside the
tools; five refusals, all of mine (addressing).

## Assessment

- **Through the tools or around them:** through them; the `unseen`
  split was an edit, not a refactoring (above).
- **Did the plan hold?** Yes.
- **One change that would have helped:** replace_declaration (or
  add_field's sibling) addressing a field of a type local to a
  function by `Type.Field` with `function`.

## Follow-ups

- The result type check is now written twice in Fold (goesOn and
  back); a helper would hold it once.
