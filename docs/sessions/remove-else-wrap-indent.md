# remove_else's wrap in unformatted files

- **Item:** roadmap 43 (found 2026-10-29), listed under defects in
  roadmap item 6 and `docs/parity.md` section 1: remove_else's `wrap`
  does not re-indent the moved statements in a file that is not
  formatted, in Go and TypeScript.
- **Date:** 2026-11-06
- **Branch:** remove-else-wrap-indent

## Plan

At the highest level: wrap moves the statements after an if into a new
block, one level deeper, and it relies on the formatter to indent them;
a file that was not formatted is not formatted after the edit, so the
statements stay at the if's depth. The edit should indent them itself, by
the file's own step (the if body's indentation less the if's), as
remove_else's other direction already dedents through `textutil.Reindent`.

- Red: a golden with a space-indented file, Go first, then TypeScript.
- Green: wrapElse replaces the text from the if's closing brace through
  the last statement, reindented by the step; TypeScript's WrapElse the
  same.

## Notes
- read: wrapElse, dedented and textutil.Reindent, to reuse the reindent remove_else's other direction uses; TypeScript's WrapElse, lined.unit and early.indentLines, to write the fix.
- plan: TypeScript already indented by the file's step, so its defect was another: blank lines got trailing spaces and a template literal's continuation lines were indented, changing the string; inline_function's early returns had the same loop, so both use the new `lined.indentable`.
- hand: the Go golden's files written with printf (testdata, outside the guards).

## Measured

Not measured: a cloud clone with no session log. Through the CLI: two
replace_statements in Go, one add_declaration, two add_element test
cases, replace_text on a comment and the docs.

## Assessment

- **Through the tools:** every change to Go code was a structured edit;
  replace_text touched only a doc comment and docs. No hand edit of Go.
- **Large writes:** `indentable` (21 lines, new behavior); the two test
  cases (new).
- **Plan:** held for Go; for TypeScript the defect was not the one the
  roadmap named, and the fix covered a second caller.
- **Reading:** bodies read to write the fix, none to check safety.
- **One change that would have helped:** none new; a TypeScript
  counterpart of `textutil.Reindent` would let both early.go and WrapElse
  rebuild text the way Go does, if a third caller appears.

## Follow-ups

- The remaining parity defects: TypeScript 41.8, Go's 44.
