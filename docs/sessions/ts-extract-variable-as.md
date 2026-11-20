# extract_variable's as and a missing line

- **Item:** issues #716 (an unknown `as` ignored) and #715 (a missing
  `line` refused for line 0, or a line the call never gave), filed by
  a session that built a TypeScript toy through the tools.
- **Date:** 2026-11-20
- **Branch:** ts-extract-variable-as

## Plan

- **The easy change:** refuse an `as` other than "parameter", and a
  missing `line`, saying what to send, at the top of
  `ExtractVariable.Plan`; and in the batch's renumbering of refusals
  (`startLines`), leave line 0 alone: it matched an original line a
  step deleted, whose number now is 0.
- **The places today:** TypeScript's `ExtractVariable.Plan`, Go's
  (the same silent `as`), and `startLines`: one place each.
- **The refactorings:** none.

## Notes

- refusal: add_element with `type` as written in the source matched
  nothing; retried without `type`.

## Measured

`refactor-tokens -log .claude/session-log/ts-extract-variable-as.jsonl`
(it includes reading the 14 issues before the branch was chosen):

- 29 tool calls, 29 requests, 3.33M input tokens;
- 8 edit steps (2 text edits of Go), 0 refactorings; gorefactor
  refused 2; one grep of Go files denied by the read guard;
- by hand: the two Go goldens under testdata (shell).

## Assessment

- **Through the tools or around them:** through them: the checks are
  new behavior (replace_statements, insert_statements), and the tests
  went in by add_element. No refactoring was needed: one place each.
- **Large writes:** none.
- **Cost:** most of the input is the start of the session (reading
  the issues); two refusals, both of add_element's `type`.
- **Did the plan hold?** Yes; Go's extract_variable had the same
  silent `as`, found by `inspect` with a pattern, and was fixed too.
- **Reading:** startLines and the TypeScript and Go Plan functions,
  to write the checks.
- **One change that would have helped:** add_element accepting a
  type as written in the source (spacing), not only as go/types
  prints it.

## Follow-ups

- add_element's `type` could compare without spaces.
