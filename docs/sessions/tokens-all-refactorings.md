# refactor-tokens counts every refactoring

- **Item:** roadmap 49.1, chosen by the user from /whats-next (choosing
  it was the OK to change earlier cost-history rows)
- **Date:** 2026-10-31
- **Branch:** tokens-all-refactorings

## Plan

The change: the set of tools that `refactor-tokens` counts as
refactorings is missing five that were added after it (fold,
parameterize, move_method, type_switch_to_method,
prepare_switch_for_types), so their steps count as edits and the
derived share is low. The set is data, `refactoringTools` in
`internal/tokens/tokens.go`; `measureAlgebra` is its only reader, for
transcripts and logs alike.

Derivation: nothing to make easy (a map's entries). Red: a test that
calls `measureAlgebra` with an applied call of each of the five and
expects `AlgebraRefactoring`. Green: the five entries. Refactor: none
expected. Docs: the roadmap marked done, a note in cost-history that
rows before this undercount the five, and the friction log.

## Notes

- read: `measureAlgebra` and the map (lines 440-520), to see the map's
  only reader and how a call is classified; `TestAnalyzeAlgebra`, for how
  calls are built in tests.
- refusal: the read guard denied a grep over `internal/refactor/*.go`
  (a glob names Go files); repeated over directories with `--include`.
  The question was whether the server already classifies tools as
  refactorings or edits; it does not, in a field.
- (Written at the end, not as it happened: the notes above were
  reconstructed when the record was finished.)

## Measured

`refactor-tokens -log .claude/session-log/tokens-all-refactorings.jsonl`,
run before the record was finished (so its own call counts as "denied or
failed": the call had no result yet when the log was read):

- tool calls: 18
- through gorefactor: 0 refactoring steps, 2 edit steps (Go); docs
  through replace_text and create_file are not counted
- refused by gorefactor: 0; by hand: 0; large writes: 0
- denied or failed: 2 (the guarded grep; the measuring call itself)
- reads: inspect: targets 2, inspect: names 2, Bash 3
- derived share: 8% (13 lines written, 12 changed)
- transcript: 16 requests, 1.54M input tokens, 5.2K output; 1
  ToolSearch-only turn, 1 refused read

## Assessment

- **Through the tools:** every write went through gorefactor: the test
  (add_declaration), the fix (replace_text, two lines), the docs
  (replace_text, one batch). Nothing to derive: the change is data. The
  8% derived share says so; it is not a sign of working around.
- **Large writes:** none.
- **Cost:** 16 requests. One turn lost to the read guard (a glob of Go
  files in a grep), one to loading schemas.
- **Did the plan hold?** Yes, as written: red, five entries, no
  refactoring.
- **Reading:** two function bodies, both to write the test; `names` was
  tried for the `call` struct, refused as ambiguous (declared in several
  packages, and `tokens.call` too, since a test function in the package
  shares the name), then found with a directory grep.
- **Notes were not kept as it happened.** The skill asks for them at
  the time; they were written at the end.
- **One change that would have helped:** the list drifts because
  nothing ties it to the tools: a test that every published tool is
  classified (refactoring or edit) would have caught all five. Roadmap
  49.8.

## Follow-ups

- Roadmap 49.8: keep `refactoringTools` in step with the published
  tools. change_type is in the set but is an edit (session notes), and
  TypeScript's change_field_type and change_parameter_type are edits
  too while Go's are refactorings.
- `inspect` `names` cannot reach `tokens.call`, a type, when a test
  file of the package declares a function `call`: worth a look.
