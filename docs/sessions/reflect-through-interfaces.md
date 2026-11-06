# Values reaching reflection through interfaces

- **Item:** roadmap 42c, in the defects list and `docs/parity.md`
  section 1: rename's check of what reflection may see looked only at
  arguments passed as an interface, so `var x any = r; json.Marshal(x)`
  let it rename a JSON field.
- **Date:** 2026-11-06
- **Branch:** reflect-through-interfaces

## Plan

At the highest level: the check asks "does a value holding the type
reach an interface parameter"; it answered only for values written at
the call. An argument that is itself an interface was reached by other
values earlier: follow it to them, within the package's files.

- Red: three rename goldens refused (a variable, a function's return, a
  composite literal's element).
- Green: `analysis.reaches` indexes assignments to interface variables
  and the package's functions; `holder` follows an argument through
  them, conversions and literals, and `passedAsInterface` reports the
  type it finds.

## Notes

- read: passedAsInterface, reflectedArgs, PassedAsInterface, the json
  refusal golden.
- refusal: batch step using `r` before the step that declares it; the
  steps reordered.

## Measured

Not measured: a cloud clone with no session log. One batch of six steps
(after the refused one): `reaches` (about 95 lines, new behavior) by
add_declaration, the rest replace_statements, insert_statements and
replace_expression.

## Assessment

- **Through the tools:** structured edits only; no hand edit of Go.
- **Large writes:** `reaches`, `newReaches`, `holder` (new behavior).
- **Plan:** held.
- **Reading:** bodies read to place the change.
- **One change that would have helped:** none.
- **Trade-off:** more values are now followed into callees the rule
  counts as able to find fields by name (an error value passed to
  `errors.Is` through a variable, say), so rename, extract_struct and
  remove_dead_code refuse a little more often on exported fields.

## Follow-ups

- If the extra refusals show in a run, a table of standard functions
  that do not reflect (`errors.Is`, `errors.As`) would narrow them.
