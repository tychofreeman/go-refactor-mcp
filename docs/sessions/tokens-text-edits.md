# refactor-tokens reports text edits of Go

- **Item:** roadmap 49.10, asked by the user after #532 ("the edit tools
  have been used too much as an impediment")
- **Date:** 2026-10-31
- **Branch:** tokens-text-edits

## Plan

The change: edit steps are one count today, so Go typed out and pasted
with replace_text looks the same as add_element or add_case. Split the
edits into structured edits and text edits of Go (replace_text,
replace_statements, replace_declaration on a `.go` file), and report
the text edits in both reports: the transcript's "steps:" line and the
log's "through gorefactor:" line.

Derivation: the classification is the switch in `measureAlgebra` that
counts `refactorings` and `edits`; `Report` sums them (`add`, the
aggregation of analyses) and `Work` copies them. Nothing to make easy:
a counter beside the others. Red: assert `TextEditSteps` in
`TestAnalyzeAlgebra`, whose transcript already has a replace_statements
on a.go, a replace_text on a.go in a batch, and one on README.md (not
counted). Green: add the field to `algebraCall` and `Report` (add_field),
count it in the switch, sum it. Then the same for `Work` and its line,
with a test on the log's report. Refactor: as the code shows.

## Notes

- refusal: a batch's add_field had guessed arguments (`struct`,
  `comment`): its schema was not loaded. Resent with `name` and `field`.
- read: the four lines that sum and print the step counts, and
  `TestAnalyzeWork`, to write the new code and test.
- hand: the transcript's `steps:` line needs one more argument to an
  `Fprintf` whose format also holds the table header. No tool adds an
  argument to a call: replace_expression takes one expression (`a, b`
  is not), add_parameter changes a function's signature, add_element
  takes composite literals only. Tried none further; the whole call is
  replaced with replace_expression, its long format retyped with `%s`.

## Measured

`refactor-tokens -log .claude/session-log/tokens-text-edits.jsonl`, run
with the new count, before the record was finished:

- tool calls: 20
- through gorefactor: 0 refactoring steps, 15 edit steps (1 of them
  text edits of Go: the doc comment on `Report.TextEditSteps`)
- refused by gorefactor: 1 (add_field's guessed arguments); by hand: 0;
  large writes: 0
- denied or failed: 1 (the measuring call itself, still running)
- reads: inspect: targets 2, Bash 3 (directory greps for line numbers)
- transcript: 14 requests, 1.78M input tokens, 11.4K output; 1 turn
  lost to the refusal

## Assessment

- **Through the tools or around them:** through, this time. The
  changes were additions, and each went through the structured edit
  for it: add_field, insert_statements, add_declaration; the test's
  field was declared as a stub. The one text edit of Go is a doc
  comment, which is replace_text's job. The exception is the
  `Fprintf` given one more argument: replace_expression of the whole
  call retyped its 130-character format, since no tool adds an
  argument (roadmap 49.12). The new count does not see that: it counts
  replace_expression as structured.
- **Large writes:** none.
- **Cost:** 14 requests, one lost to a schema not loaded (add_field):
  CLAUDE.md's first rule, skipped because I loaded the schemas for the
  last item and not this one's.
- **Did the plan hold?** Yes. The plan's add_field went to
  `algebraCall` and `WorkReport`, and the test declared `Report`'s.
  The note's helper, `textEditsNote`, was new: both reports needed the
  same words.
- **Reading:** two small ranges and one test, all to write the change;
  line numbers came from directory greps, where `inspect` of the file
  would have given them too.
- **One change that would have helped:** adding an argument to a call,
  roadmap 49.12.

## Follow-ups

- Roadmap 49.12 (add an argument to a call).
- replace_expression of a large expression is a text edit too; whether
  to count it so (by the size of what it retypes) is open.
