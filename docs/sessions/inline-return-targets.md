# inline_function: where an inlined return assigns

- **Item:** two bugs the stress test with module tests found while
  checking the constant-branch change (2026-11-04), both silent behavior
  changes already on main (reproduced with a CLI built from main); fixed
  before roadmap 51.4's second half, as told to the user.
- **Date:** 2026-11-04
- **Branch:** inline-return-targets (from inline-constant-branches)

## Plan

At the level of the operation: an early return of the inlined body
becomes an assignment to the call's variables, which is right only
where those names mean the call's variables and the call assigns with
`=`. Two shapes break that: a block of the body declaring the same name
around the return (jsonschema-go `resolve`, `jsonNumber`; x/tools
`readAll`, `rvToExpr`), and a call assigned with an operator (x/tools
`popcount` under `l +=`). Both have the fix the code already makes for a
variable of another type: a temporary, copied after the body.

Derivation, in TDD's order: two goldens of the two shapes, their
expected outputs over what -update gave (red); tailTargets replaced
(replace_declaration), shadowedAtReturns added with it.

## Notes

- With two temporaries, freshName gave both the name `result`: neither
  is declared yet when the second is named. tailTargets now keeps the
  names it has taken.
- No other golden changed; the five calls from the stress run inline and
  their packages' tests pass.
- By hand: nothing.
