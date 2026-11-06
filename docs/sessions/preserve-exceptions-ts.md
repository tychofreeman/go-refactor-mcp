# preserve_exceptions for TypeScript

- **Item:** roadmap 54.4, after Go (#586); stacked on that branch.
- **Date:** 2026-11-06
- **Branch:** preserve-exceptions-ts

## Plan

At the highest level: TypeScript's one allowance, a read through what may
be null or undefined moving past code that writes, lives in
`tsapi.conflict`; judge there strictly and as before, as Go's Move does.
The analysis is made afresh at each use with no path to the Project, so
the call's setting is package state, which the workspace sets for Run.

- Red: TestNullishReadsPreserved (noted; refused with the flag).
- Green: the strict check in conflict, the setting and its changes, the
  workspace's two methods; then the published schemas.
- Refactor: the wording of a conflict extracted to `described`.

## Notes

- read: conflict and its callers, newEffects, lined, Project: to find
  where a per-call setting can live.
- plan: package state rather than a field, since no caller of newEffects
  has the Project; the server runs one call at a time and no test runs
  in parallel.

## Measured

Not measured: a cloud clone with no session log. Three batches of
structured edits, one extract_function, one move_parameter.

## Assessment

- **Through the tools:** structured edits and two refactorings; no hand
  edit of Go.
- **Large writes:** none of existing code.
- **Plan:** held.
- **One change that would have helped:** none.

## Follow-ups

- Item 3: TypeScript's split by phase (52.2) and inline_struct's
  counterpart (52.3).
