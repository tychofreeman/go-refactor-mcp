# A stub constructor sets the fields it fits, in TypeScript

- **Item:** roadmap 54.10, its second PR: java's #273 (the user's option 2 of 2026-11-07, "a
  constructor stub whose arguments each fit one field assigns them").
- **Date:** 2026-11-07
- **Branch:** ts-assigning-stub (on ts-written-constructors, #610)

## Plan

TypeScript's test-first flow differs from Java's: `create_from_usage` declares an undeclared
`Pot` from `new Pot(3)` as `class Pot { constructor(n: number) {} }`, with no fields yet. The
fields come after (`add_declaration` with `type`), and the empty constructor then blocks the
constructor #610 writes, so TS2564 refuses the edit. So the rule lands where the fields arrive:
in `tsapi.WriteConstructors`, a class whose one constructor has an empty or stub body and whose
parameters each fit, by type as written, exactly one of the fields with no value, all of them
covered, gets that constructor's body set to assign them (`this.size = n;`); its signature, and
so every `new Pot(3)`, stays. Otherwise the edit is left to the type check, as now.

Derivation: a red test (a class with `constructor(n: number) {}` gets a `rows: number` field and
the body `this.rows = n;`; two fields of one type are refused); then the case in
`WriteConstructors`.

## Notes

- read: `tsapi.Stubs`' `stub` (how `new Pot(3)` is declared: a class with an empty constructor,
  only for an undeclared class), which moved the rule from the stub to where fields arrive.
- plan: the case went into `WriteConstructors`, rewritten with `replace_declaration` (its insertions
  became replacements), plus `emptyBody` and `fitting`.
- refusal: the batch replaced `WriteConstructors` before declaring what it uses; a retry inserted a
  step before it (`after: 0`) with the helpers, its stub of `fitting` replaced by the next step.
- bulk: `WriteConstructors` again (75 lines: 45 of them as they were, written out because its loop
  changed shape; no refactoring turns an insertion list into a replacement list) and `fitting`
  (28 lines, new).
- hand: none.
## Measured

Not measured: a cloud clone, started outside the checkout, has no session log; the CLI's steps
are in the notes.

## Assessment

Through the tools: every change through the CLI; no hand edits. The plan changed once, before any
code, after reading how TypeScript's stubs are made. One function body was written out again in
part; the loop's shape changed, and the edits would have been several replace_statements at lines
inside one function. Cost: one refused batch and two retries, the second because the first
inserted the helpers while the kept step declared them too.

## Follow-ups

- None for roadmap 54.10. A Java-style constructor stub for an existing class (`new Pot(3)` where
  Pot is declared without one) is not declared by `create_from_usage` at all; evidence would come
  from a run.