# fold of a function with early returns

- **Item:** roadmap 67, second step: early returns in the body.
- **Date:** 2026-11-19
- **Branch:** fold-early-returns

## Plan

A copy that ends in the function's last return returns the same values
as a call of the function on every path, early returns included, when
each return carries all the results: it becomes `return f(args)` as in
#694. So the first case needs no new matching, only folder.pattern
accepting returns anywhere in the body. Derivation: add_parameter
`results int` (value -1) to statementsOnly, while green; red goldens
`statements_early_return` and `refuse_statements_bare_return`; then the
edit: statementsOnly's return case refuses only a return without
`results` results, and the third branch passes the whole body with
sig.Results().Len(). A copy that does not end its function (`if v, err
:= f(args); err != nil`) is a later PR.

## Notes

- read: folder.pattern's if chain and statementsOnly, to place the change (both read last PR).
- plan: refuse_not_one_return was removed: its early return now folds; refuse_statements_bare_return carries its advice text.

## Measured

`refactor-tokens -log .claude/session-log/fold-early-returns.jsonl`:
10 tool calls; 1 refactoring step and 5 edit steps through gorefactor
(4 text edits of Go: the replace_expression and replace_statements of
the change, counted as text, and two string literals); no refusals;
1 write by hand, the goldens' fixtures (59 lines of testdata, new).
8 requests, 1.11M input tokens, 6.9K output.

## Assessment

- **Through the tools or around them:** the preparing step was a
  refactoring (add_parameter, both calls); the change was one
  replace_expression, one replace_statements of one line into an if,
  and string literals. Goldens' fixtures by heredoc under testdata, as
  the guards allow (create_file refuses a fixture module's bodies).
- **Did the plan hold?** Yes, as written.
- **One change that would have helped:** create_file writing a golden's
  fixture in a nested module under testdata (friction log of #694).

## Follow-ups

- Roadmap 67: a copy that does not end its function.
- TypeScript's fold refuses both shapes.
