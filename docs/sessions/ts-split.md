# split for TypeScript: variable and loop

- **Item:** roadmap 52.2, `split` for TypeScript (Go/TypeScript parity),
  chosen from "what's next?". This PR does kinds variable and loop, as
  Go's #543 did; phase follows in its own PR, as Go's #544.
- **Date:** 2026-11-05
- **Branch:** ts-split

## Plan

The change: TypeScript gets the operation Go's `split` is, under the same
name and arguments (`kind`, `file`, `line`, `name`, `new_name`, `at`), so a
recipe or a batch that splits works in either language and the merged
schema stays one shape. Two kinds now:

- **variable:** `n = expr` that does not read `n`, in the block that
  declares `n`, becomes `const new_name = expr` (`let` when the new
  variable is assigned again later), and the uses of `n` after it read
  `new_name`. Refused: the right side reads `n`; the assignment is inside
  a nested block; a closure refers to `n`; `new_name` is already a name
  there.
- **loop:** a `for`, `for...of` (or `for...in`) whose body is two
  independent parts becomes two loops with the same header, the second
  holding the statements from line `at` on. Refused: a part that leaves
  the loop (break, continue, return, throw at its level); parts that use
  what the other declares or whose effects conflict; a header the body
  changes, or one with effects, or a `for...of` over something not known
  to be an array (an iterator runs once).

Derivation, in TDD's order: no refactoring is needed first: a new package
`internal/ts/refactorings/split` beside `simplify`, calling two new
`tsapi.Project` methods built from what is there (`lined`, `declared`,
`uses`, `symbols`, `taken`, `newEffects(...).Of`, `conflict`,
`lineRange`). Tests first (a table in the package, as simplify's),
stubbed, then the code. The tool goes in the TypeScript registry and the
TypeScript instructions' list. Phase answers "not yet" until its PR.

## Notes

- plan: put `Split` in the `simplify` package rather than a new one: it
  simplifies in place like its neighbours, and reuses their `plan`
  helper and test table.
- read: tsapi's MoveStatement, the name helpers (`taken`, `uses`,
  `declared`, `symbolOf`) and the effects analysis (`Effects`,
  `conflict`, `ofNodes`, `walk.declare`), to build from them.
- bulk: `tsapi/split.go` (230 lines), `simplify/split.go` (57) and
  `TestSplit` (150): new behavior and its tests, over stubs the batch
  declared (`create_from_usage` made `SplitVariable` and `SplitLoop`).
- refusal: the loop cases failed on `a.Of` of a statement (it judges
  expressions; statements are "the expression ... may change state");
  replaced with `ofNodes`, and a `for`'s declaration with
  `walk.declare`, by replace_statements. The first try cut a loop;
  resent with the range the refusal named.
- hand: a placeholder `var _ = checker...` written into the new file and
  then removed with replace_text, a text edit of Go (no structured edit
  removes a package-level `_` var by name); no reason to write it.
- refusal: add_element on the line of an element; resent with the
  literal's first line.

## Measured

Not measured: this cloud clone has no session log. Every step went
through the `refactor` CLI, with batch files in the scratchpad carrying:
the record (create_file), the wrapper and its tests (create_file,
add_declaration; stubs by the batch), the analysis (create_file over
the stubs, replace_statements for the two fixes), the registry
(add_element), and the docs (replace_text).

## Assessment

- **Through the tools:** every write went through gorefactor; one text
  edit of Go (the placeholder above).
- **Large writes:** all new behavior and tests; no existing code was
  written again.
- **Did the plan hold?** Yes, with the package change noted; the one
  surprise was that `Of` does not judge statements.
- **Reading:** bodies read were the helpers built on, read to use them.
- **One change that would have helped:** `effects.Of` refusing a
  statement, or naming `ofNodes`, rather than calling it an expression
  that may change state.

## Follow-ups

- Kind phase for TypeScript (roadmap 52.2): extract_function of the
  second phase, then the parameters from the first into an object type.
- 52.3, inline_struct's counterpart.
