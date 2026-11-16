# TypeScript: inline at a declaration means every call

- **Item:** roadmap 56, TypeScript's counterpart of #653 and #658;
  second of the four items chosen from "what's next?" on 2026-11-16.
- **Date:** 2026-11-16
- **Branch:** ts-inline-declaration

## Plan

The change: as in Go, inline given a function's declaration where a
call goes (the declaration's line, or a name with no line) runs every
call and says so on a `chose:` line, instead of refusing ("no call of f
on line N", "give the line of the call, or all_calls", and inline's
"give line ..." for a name alone).

Derivation, from the outlines of `simplify/inline.go`,
`simplify/simplify.go` and `tsapi/inlinefunc.go`:

1. Test first: cases in TestInlineFunction (declaration line, name with
   no line) and one through `inline` for a name with no line, with the
   choice checked.
2. `add_result` to `tsapi.Project.InlineFunction`: a bool that says
   every call ran because the declaration was given (default false),
   carried to its one caller, `InlineFunction.Plan`.
3. In tsapi, the two refusals become `all = true` with that result
   set (a replace of the two cases).
4. In `InlineFunction.Plan`, the summary and the choice from it.
5. In `Inline.Plan`, the default case (no line, all_calls, function or
   field) runs inline_function, which refuses what is not a function.

## Notes

- read: tsapi.InlineFunction's line handling (lines 60-130) and
  Inline.Plan, InlineFunction.Plan, to write the change; the test file's
  inline cases, to copy a case's source.
- refusal: add_case at line 103, which step 2 of the same batch had
  rewritten; retried the batch with the step at `$2.start`.
- plan: add_result put the new result last, after the `why` string
  (`([]Edit, bool, string, bool)`), not before it; left there, since
  the one caller names it.

- bulk: add_declaration of TestInlineDeclaration, 39 lines, new (the
  test).

## Measured

`go run ./cmd/refactor-tokens -log .claude/session-log/ts-inline-declaration.jsonl`:

- tool calls: 25; through gorefactor 1 refactoring step (add_result)
  and 16 edit steps (8 text edits of Go: descriptions and doc comments
  by replace_text, and the small replace_statements and
  replace_expression edits); 1 refusal; 0 by hand.
- large writes: the test, 39 lines.
- derived share: 25% of the Go lines changed (add_result's 15 lines
  changed by writing 2).
- requests: 19; input 8.04M tokens (cached or not), output 13.0K; the
  first request's context was 405K, this long session's.

## Assessment

- **Through the tools or around them:** through them: nothing by hand.
  add_result carried the signature to its caller; the behavior went in
  as six edits of one or two lines each (narrow a case, add a case, swap
  two returns, the result's value, the choice), not a rewrite of the
  switch. The text edits of Go were descriptions and comments.
- **Large writes:** only the test, which is new.
- **Cost:** 19 requests; one turn lost to the add_case refusal, answered
  with `$2.start` as the refusal said.
- **Did the plan hold?** Yes, steps 1-5 as written, with a tidy-up after
  green (`if a.All || every` in place of rewriting the summary), and the
  new result last rather than before `why`.
- **Reading:** the line handling of tsapi.InlineFunction and the two
  Plans, to write into them; nothing read to find uses.
- **One change that would have helped:** add_result could put a new
  result before a final `string` that carries a refusal, as it does
  before a final error (session notes: "A string last result counts as
  an error for an early exit"); not put on the roadmap, since one caller
  named it as easily either way.

## Follow-ups

- None for this item; Go and TypeScript agree.
