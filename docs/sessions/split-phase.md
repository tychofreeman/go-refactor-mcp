# split: kind phase (Split Phase)

- **Item:** roadmap 51.1, its last part (2026-11-04): Fowler's Split
  Phase under the same tool as Split Variable and Split Loop (#543).
- **Date:** 2026-11-04
- **Branch:** split-phase (from split, #543)

## Plan

At the level of the operation: a function that computes in two phases
becomes two functions joined by a struct of what the first computes.
That is extract_function of the second phase, then a parameter object
for the parameters it takes from the first (derivations 5 makes one in
four steps). So kind phase is an expander, the parameter object a step
that is not a tool (`refactor.Inner`, as remove_parameter's value is).

Derivation, in TDD's order:
1. Green: nothing to reshape; Split gains `Expand`.
2. Red: seven goldens by create_file, `TestPhase` by add_declaration.
3. Green: `Expand`, `phaseAt` and the step's plan by add_declaration.

## Notes

- First order tried: the struct first (its declaration and the uses of
  its fields), then extract_function by the original lines. Refused: the
  batch cannot address a line an earlier step rewrote, and the uses made
  every line of the second phase rewritten. Swapped: extract first, by
  the original lines, then the struct step finds the new function by
  name, and its arguments leave out `line` (translated too, so refused
  the same way).
- The step's arguments are checked against its schema before it runs,
  so it needs its own (`dataArgs`): with args' schema, `line` was
  required.
- That refusal crashed `runExpanded`: a batch refused before any step
  reports step 0, and it indexed `steps[-1]`. Fixed there; the goldens
  hit it until `dataArgs`.
- A comment directly above the second phase stays above the call, as
  extract_function leaves a comment above its selection.
- By hand: the refusal for a phase that shares no data lost its `line`
  argument by replace_text on code (`, a.Line)`), since no tool removes
  an argument of one call.
