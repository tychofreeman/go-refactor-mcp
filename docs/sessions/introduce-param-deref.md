# introduce_parameter of a dereferenced parameter

- **Item:** roadmap 44's defects, in the defects list and
  `docs/parity.md` section 1: introduce_parameter of `(*found)[0]`
  wrote `(*(&found))[0]` at the call, and replaced the first occurrence.
- **Date:** 2026-11-06
- **Branch:** introduce-param-deref

## Plan

At the highest level: substituting an argument `&x` for a parameter that
the expression dereferences gives `*&x`, which is `x`; the substitution
should write that. Probe all three item-44 defects first, in a scratch
module, to see which still happen.

- Red: a golden, `deref_of_address`.
- Green: in `substitute`, when the argument is `&x` and the parameter is
  dereferenced, `x` replaces the dereference and its parentheses.

## Notes

- read: `substitute`, `selected` and `simpleOperand`, to place the case.
- plan: the probe showed the first-occurrence half now refuses (the
  occurrence after the if is not evaluated on every call); only the
  written text was left.

## Measured

Not measured: a cloud clone with no session log. One batch: a
replace_statements and an add_declaration (`derefOf`, 26 lines, new).

## Assessment

- **Through the tools:** both changes structured edits; no hand edit of
  Go.
- **Large writes:** `derefOf` (new behavior).
- **Plan:** held; smaller than the roadmap said.
- **Reading:** bodies read to place the change.
- **One change that would have helped:** none.

## Follow-ups

- extract_function's pointer for a variable it only reads; add_parameter
  on a function literal bound to a local. Each its own PR.
