# split: Split Variable and Split Loop in one tool

- **Item:** roadmap 51.1, asked by the user (2026-11-04): Fowler's Split
  Phase, Split Loop and Split Variable, all under a single tool. This PR
  has variable and loop; phase follows in its own PR.
- **Date:** 2026-11-04
- **Branch:** split (from docs-catalog, #542)

## Plan

At the level of the operation: both are a piece of code that holds two
things becoming two pieces. A variable that gets a value not depending
on its old one holds two values, one after the other; a loop whose body
has two independent parts does two things per iteration.

- Split Loop is move_statement_up repeated over iterations: running the
  first part for every iteration, then the second, equals interleaving
  them when every statement of the second part could swap with every
  statement of the first. So it reuses slide's check
  (`slidestmt.Independent`), plus what slide never needed: the header is
  evaluated again (an `analysis.Move` of each header expression past the
  body, `Several` times), the range's operand must give the same values
  in the same order (no map, channel or function), and no part leaves
  the loop.
- Split Variable is a rename of the uses after the assignment, safe when
  no use after it can see a value from before: the assignment is in the
  block that declares the variable (so no value comes round a loop or
  out of a branch), the value does not read the old one, nothing outside
  the code can hold the variable (`Func.Owns`), and no goto. The new
  variable keeps the old one's type (`var y T = e`) unless `e` checked on
  its own already has it.
- Split Phase needs a parameter object, which no tool makes in one step
  (derivations 5 does it in four). It will be an expander: extract the
  second phase, then bundle the new function's parameters the first phase
  computes into a struct, a step that is not a tool.

Derivation, in TDD's order:
1. Green: export what split reuses from slidestmt, by the tools: rename
   `stmtAt`, `span` and `leaves`, add `Independent` and fold it into its
   one use.
2. Red: 27 goldens and the test, by create_file (the test's `New` came as
   a stub, once the package had a non-test file).
3. Green: the code by add_declaration, registered by add_element.

## Notes

- First run: every variable case was refused as "in a block inside the
  one that declares it": a function's body has no scope of its own in
  go/types, its locals are in the signature's scope (`scopeOf`).
- `rate = 2` with `rate float64` became `mist := 2`, an int: go/types
  records an untyped constant at the type it converts to, so a check on
  the recorded type said float64. Fixed by checking the expression on
  its own (`types.CheckExpr`), where it is still untyped; golden
  `constant_of_default_type` for the case where `:=` is right.
- Stress (each module's tests after every applied split): x/sync,
  uritemplate, jsonschema-go and four standard-library packages, 370
  candidates: none applied, no rollback, no failure. Real code rarely
  has either shape where every assignment and loop statement is a
  candidate; most refusals are an assignment inside a branch and a loop
  body that returns or breaks.
- By hand: nothing in the Go code. The goldens' line numbers were
  checked by a script before the batch.
