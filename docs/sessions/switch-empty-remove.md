# Remove the switch a move empties

- **Item:** follow-up of roadmap 72.2 (switch-move-case): a switch whose
  last case moved stays as `switch it.Kind {}`.
- **Date:** 2026-11-20
- **Branch:** switch-empty-remove

## Plan

The change: when `move` deletes a switch's only clause, it deletes the
switch with it: the statement after a returning guard, or the guard's
`else` that held it.

- **The easy change:** in `mover.plan`, where a clause is deleted, delete
  the switch (or the else) in its place when the clause is the switch's
  only one.
- **The places today:** one, `mover.plan`'s case that calls
  `deleteClause`; `move` needs the guard's `if` to find the else.
- **The refactorings:** none (one place).

Test first: a golden `move_last_case`, moving "dvd" in move_no_case's
input, where each switch has only that case.

## Notes

- refusal: replace_text of the condition in `deletion` was refused as code; replace_expression did it.
- plan: a switch on `kind(it)` stays, since deleting it would drop a call; prepare refuses such a tag (checked with the CLI), so only a guard written by hand has one. A golden, `move_last_case_call`, holds that.

## Measured

`refactor-tokens -log .claude/session-log/switch-empty-remove.jsonl`:
28 tool calls; 11 edit steps through gorefactor (1 a text edit of Go,
refused and redone as replace_expression), 0 refactorings, 0 by hand;
1 refusal, not followed by a hand edit; 4 large writes, all goldens
under testdata (create_file). 26 requests, 2.69M input tokens (with the
A/B run's wait and its analysis in the same span).

## Assessment

- **Through the tools:** every Go change was an edit through gorefactor
  (add_field, insert_statements, replace_expression, add_declaration);
  nothing by hand. The one text edit of Go was refused and became
  replace_expression, as the refusal said.
- **Large writes:** the four goldens are new fixtures, written with
  create_file as testdata must be.
- **Cost:** one turn lost to the refusal; one to the write guard on a
  scratch module outside testdata (moved under a `testdata/` directory).
- **Did the plan hold?** Yes, one place; `plain` was added on reading
  that deleting a switch drops its tag's evaluation. A CLI check showed
  prepare already refuses such a tag, so the roadmap claim drafted first
  was corrected before commit.
- **Reading:** three inspects, of the code changed.
- **One change that would have helped:** none needed here.

## Follow-ups

- TypeScript's prepare_switch_for_types has neither `lookup` nor `move`
  (roadmap 72).
