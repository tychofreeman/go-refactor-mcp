# inline_function: constant conditions (Remove Flag Argument)

- **Item:** roadmap 51.4, its first half (2026-11-04): Fowler's Remove
  Flag Argument, by the order of the tool budget a proof in the engine
  rather than a tool.
- **Date:** 2026-11-04
- **Branch:** inline-constant-branches (from inline-struct, #546)

## Plan

At the level of the operation: a function per flag value is the function
with the flag fixed, which is delegate, redirect, inline (wrappers, fold,
inline_function). What stopped it was the dead branch left by inlining a
constant: `if true { A } else { B }`. A constant condition runs one
branch and evaluating it does nothing, so dropping the other is a proof
the engine can make while it inlines.

Derivation, in TDD's order: no reshaping; six goldens by create_file,
their expected outputs written over what -update gave (red); the step by
add_declaration and replace_statements of the plan's list.

## Notes

- Two expected outputs were wrong: a body left with one statement stays
  on the caller's line, as the inliner already does.
- An `else if` whose condition becomes constant under a condition that
  does not is replaced by a block, not by bare statements.
- No other golden changed.
- By hand: nothing.
