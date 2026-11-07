# add_result before a final error

- **Item:** found on the way to batch retry (`docs/sessions/batch-retry.md`):
  adding `bool` to `Language.Batch (string, error)` gave
  `(string, error, bool)`, with no tool to reorder results. Done first,
  in its own PR, to make that change easy.
- **Date:** 2026-11-06
- **Branch:** add-result-before-error

## Plan

At the highest level: a function's results end in an error by Go's
convention, so a new result goes before it, in the declaration, every
return and every call's left side. A method `errorLast` on the target,
asked by the three places that write those edits; goldens first.

## Notes

- refusal: replace_expression of `"return " + list + ", " + value`: not
  a node of the left-associative `+` chain; replaced the whole chain.
- refusal: the same batch resent through an unquoted heredoc turned
  `\\n` into `\n`; resent with a quoted one (seven steps sent three
  times, the case batch retry is for).
- hand: the old golden `return_of_call` pinned `(int, error, bool)`; its
  out file changed by sed (testdata).

## Measured

No session log (a cloud session started outside the checkout). Through
the CLI: one batch of seven structured steps (add_declaration,
insert_statements, replace_expression), two replace_text on text.

## Assessment

Through the tools; the only text written is the new behavior. The cost
was the two refusals: each resent the whole batch.

## Follow-ups

- TypeScript's add_result returns an object, so it has no order to keep.
