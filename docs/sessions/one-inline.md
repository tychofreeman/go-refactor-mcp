# One inline

- **Item:** roadmap 54.2, one `inline` for functions, variables and a
  held struct, chosen with "what's next?" ("Do all 4", the second).
- **Date:** 2026-11-06
- **Branch:** one-inline

## Plan

Published in place of inline_function, inline_variable and (Go)
inline_struct, as `move` is, the three kept unpublished under their
names. Unlike move, the arguments alone do not say which: `file`, `line`
and `name` may be a call or a local. So `inline` is a `refactor.Covering`
tool (the part of `Router` that publishes in place of others) whose Plan
decides, by `field`, `all_calls`/`keep_function`, `function`, or else what
the name on the line is (`locate.Ident` in Go, a new `tsapi.Callable` in
TypeScript), and runs that tool with only the arguments it takes.

## Notes

- read: the three tools' arguments and markers: only inline_variable has
  one (`LocalsOnly`, which only leaves out the symbol note), so a tool
  that plans through another loses nothing.
- plan: `Router` split into `Covering` (published in place of others) and
  `Router` (Covering that routes by arguments), so `Published` hides what
  inline covers without inline routing at lookup.
- refusal: the Go test's lines went stale after the first inline (a
  deleted function); the steps were reordered.
- refusal: inline_struct broke a one-line `type Bed struct{ Soil }`, a
  defect of its own, put on the roadmap; the test uses multi-line
  structs.
- hand: none. `only`, the argument filter, was moved to
  `refactor.OnlyArgs` with `move` when TypeScript needed it.
- TDD's order was broken for TypeScript: its inline was written before
  its test, which then passed at once.
- bulk: `internal/refactorings/inline/inline.go` (106 lines) and the TS
  inline (67 lines) by create_file: new behavior, mostly the descriptions
  and argument lists.

## Measured

Not measured: a cloud session with no session log; about twenty CLI calls.

## Assessment

- **Through the tools:** every Go change was an edit or a refactoring;
  `move` carried the shared filter to its package. Text edits only on
  literals, comments and docs.
- **Plan:** held, with the Covering split found on the way.
- **One change that would have helped:** none new; the inline_struct
  defect is on the roadmap.

## Follow-ups

- Java's `inline` (with inline class, when built).
- inline_struct on one-line struct declarations (roadmap defects).
- hand: merging main, both sides had appended whole test functions to the two language test files; the conflicts were resolved with a short Python script over the lines (main's functions, then TestInline), since no tool edits a file with conflict markers in it.
