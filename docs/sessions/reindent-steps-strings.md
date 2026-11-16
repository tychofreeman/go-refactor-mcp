# ReindentSteps keeps multi-line strings

- **Item:** #638, from the code review of 2026-11-14 (bug 3): `textutil.ReindentSteps`
  converted indent steps on lines inside a multi-line string, which TypeScript `move` of a
  method reaches when the two files indent by different steps.
- **Date:** 2026-11-14
- **Branch:** reindent-steps-strings

## Plan

The change: which lines start inside a multi-line string is one fact of the text, and both of
`textutil`'s re-indenters should ask it. `Reindent` computed it as offsets inside its own body;
`ReindentSteps` did not ask at all.

Derivation: red, `TestReindentStepsKeepsStrings` (a template's second line, 4-space file to
2-space). Green with the least code: a helper, `stringLines`, giving the indexes of lines that
start inside a multi-line string or comment, and `ReindentSteps` skipping them. Then, green,
`Reindent` uses `stringLines` in place of its own spans.

## Notes

- read: `ReindentSteps` and `TestReindentSteps`, to write the test like it.
- plan: the last step rewrote `Reindent`'s body with `replace_statements` rather than a
  refactoring. Turning offset spans into line indexes is a change of representation; no
  composition of extract, inline or fold makes it. The tests held it.

## Measured

Not measured: a cloud clone with no MCP server and no session-log hook. Every Go change went
through `cmd/refactor`: `add_declaration` (the test, the helper), `replace_statements`
(`ReindentSteps`, `Reindent`), in one batch for the green step.

## Assessment

Through the tools; no Go by hand. The plan held: red, the least code (the helper duplicated
`Reindent`'s scan), then the duplication removed while green. Reading was for the change itself.

## Follow-ups

- #642: with this, `textutil` has one rule for Go text (`stringLines`) and `tsapi` one for
  TypeScript (`literalLines`, #646). `ReindentSteps` is called on TypeScript text from
  `movemethod.go`; it uses Go's scanner there, which the review lists as suspected.
