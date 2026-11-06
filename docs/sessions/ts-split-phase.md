# TypeScript's split by phase

- **Item:** roadmap 52.2, the last kind of TypeScript's split; item 3 of
  "do items 1-3".
- **Date:** 2026-11-06
- **Branch:** ts-split-phase

## Plan

At the highest level: Go's phase is two operations, extract_function of
the second phase and a parameter object of what it takes from the first.
TypeScript has both, but its parameter object bundles every parameter;
an option to bundle some makes phase an expander of the two, as Go's is.

- Red: convert_params_to_object with parameters; TestSplitPhase (applied
  and Go's five refusals).
- Green: `only` through ParamsToObject (add_parameter, then the body);
  `tsapi.PhaseAt` finds the phase and the names to bundle; Split's
  `Expand` returns the two steps.

## Notes

- read: Go's Expand, phaseAt and its goldens; TypeScript's split,
  ParamsToObject, the name helpers (declared, uses, assigns, taken).
- refusal: add_declaration found two packages named changesig; resent
  with imports.
- bulk: ParamsToObject's call loop (25 lines) and selection (23), PhaseAt
  (55), Expand (33): new behavior, beside the existing code.

## Measured

Not measured: a cloud clone with no session log. Two add_parameter
refactorings, about a dozen structured edits.

## Assessment

- **Through the tools:** structured edits and add_parameter; no hand
  edit of Go.
- **Plan:** held; the option was the step that made it a composition.
- **One change that would have helped:** none.

## Follow-ups

- 52.3: inline_struct's counterpart for TypeScript.
