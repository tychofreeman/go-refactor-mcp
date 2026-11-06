# add_parameter on a local function

- **Item:** roadmap 44's defects, in the defects list and
  `docs/parity.md` section 1: add_parameter does not reach a function
  literal bound to a local (`try := func(...)`).
- **Date:** 2026-11-06
- **Branch:** add-param-local-func

## Plan

At the highest level: a local variable that a `:=` binds to a literal and
that is only called is a function with a narrower scope; adding a
parameter is the same operation, on the literal's signature and the
variable's calls. The target stands for a declared function; let it
stand for a literal too.

- Green first: the target gets `lit`, and the add_parameter checks that
  read `t.decl` (hides, shadowNote) read `fnNode()` and `body()`.
- Red: goldens: applied; refused when the new name hides a captured
  variable; refused when the local is used as a value; refused when not
  bound by `:=`.
- Green: newTarget finds the `:=` literal for a function of no scope that
  stands for the variable; `findLocal` builds it for add_parameter.

## Notes

- read: findTarget, locateFunc, newTarget, collectCalls, hides,
  shadowNote, checkSignatures, paramEdit: what the target needs from a
  declaration.
- refusal: replace_expression of `b && c` in `a && b && c`: not a
  subtree; sent the whole condition.
- refusal: replace_statements over a case clause's header; add_case for
  the new clause, and a one-line replace_statements for the return.
- plan: resolving locals in `locateFunc` reached every signature
  refactoring, most of which read `t.decl` and would panic; `findLocal`
  is add_parameter's alone, and locateFunc went back as it was.
- hand: the goldens' files with the shell (testdata); one refusal
  fragment guessed wrong ("var" for "variable") and corrected.

## Measured

Not measured: a cloud clone with no session log. Four batches through the
CLI (one refused at a step and resent), all structured edits but
replace_text on a comment, the description and docs.

## Assessment

- **Through the tools:** every Go change a structured edit; the
  refactoring before the test went through replace_expression with
  all_occurrences. No hand edit of Go.
- **Large writes:** none over 20 lines; `findLocal` (18) and the newTarget
  case are new behavior.
- **Plan:** held, narrowed to add_parameter.
- **Reading:** bodies read to see what the target needs; the t.decl uses
  across the package found by grep over the directory, to decide the
  scope.
- **One change that would have helped:** none new.

## Follow-ups

- remove_parameter, move_parameter, change_parameter_type and
  introduce_parameter on local functions, if a run or the friction log
  asks for them: each reads `t.decl` and would need `fnNode`/`body`.
