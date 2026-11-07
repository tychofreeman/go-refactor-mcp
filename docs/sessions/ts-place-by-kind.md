# TypeScript's add_declaration places by kind

- **Item:** roadmap 54.9, TypeScript's half: java's #279 and gorefactor's #607 (the user,
  2026-11-07: "the model should not be concerned with layout").
- **Date:** 2026-11-07
- **Branch:** ts-place-by-kind (on place-by-kind, #607, for `refactor.unlistedArgs`)

## Plan

TypeScript's `add_declaration` took `after` and `before` both to place and to say which class a
member goes in (`after: "Bed.plant"`). Retiring them needs a way to name the class: `type`, the
name Java's add_declaration uses. Then placement is the tool's:

- members of `type`: a field after the class's last field, a constructor after its last
  constructor or field, either before its first member when it has none of those, anything else
  after its last member; members going to one place go fields, constructors, then the rest, and
  take the class's indentation and its spacing (a blank line between members when it has one);
- top-level: a `const`, `let`, `var`, `class` or `enum` that an existing top-level statement
  names goes before the first such statement (used before it is declared, it would throw at
  load); anything else at the end;
- a function or method that replaces a stub keeps the stub's place (as now).

An old call that sends `after: "Bed.plant"` and no `type` still names the class by its prefix;
the position is ignored.

Derivation: red tests in `adddecl_test.go` (members by kind with `type`, a const before its use,
a retired `after` ignored); `tsapi.Place` in a new `place.go` (it needs the parser); `Plan` calls
it in place of its switch on `after`/`before`. Then the description, checks.md, parity.md,
roadmap 54.9, the session notes and how-this-project-was-built.md.

## Notes

- read: TS `adddecl.Plan` and `replaceStubs`, `tsapi.Outline` (it lists no plain fields, so
  placement parses the file itself), `tsapi.Functions` (how content is parsed as members).
- bulk: `tsapi/place.go` (275 lines), new behavior.
- refusal: the first batch did not type-check: `Place` is already a type in tsapi, and the
  replace_statements range in `Plan` was off (75-119 for 76-116). A retry adding a rename was
  refused (a refactoring needs code that type-checks); the retry with place.go resent under
  `PlaceDeclarations` was refused at the kept rename step, and a retry replacing that step with a
  `check` applied. Three resends of a 275-line file: retry cannot drop a step.
- hand: none.
## Measured

Not measured: a cloud clone, started outside the checkout, has no session log; the CLI's steps
are in the notes.

## Assessment

Through the tools: every change went through the CLI's batches; nothing by hand. The one large
write was the new placement (new behavior). The plan held; `type` was added as planned, and the
old `after: "Class.member"` form still names the class. Cost: three resends of the 275-line file
after one wrong name and one wrong range, because a retry can replace a step but not drop one
(the kept rename had to become a `check`). One change that would have helped: a retry step that
drops a step (roadmap 54.11).

## Follow-ups

- Constructors written for the model, TypeScript only (roadmap 54.10).