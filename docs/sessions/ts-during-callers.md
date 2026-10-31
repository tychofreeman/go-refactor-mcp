# TypeScript: callbacks run during the call, for every refactoring

- **Item:** the open part of roadmap 46.3 (TypeScript), left by #525:
  only inline_variable asks whether a function literal runs during its
  call; extract_variable, fold and inline_function pass nil to
  `timesIn`. And a local that such a literal assigns counts as state.
- **Date:** 2026-10-31
- **Branch:** ts-during-callers

## Plan

The change, at its level: "does this literal run during the call" is one
fact, which `effects.runsDuring` knows; every question of how often code
runs where it is written (`timesIn`) should get it, not one refactoring.
And a second fact follows from it: a local assigned only in literals that
run during the call stays a local, as one assigned in a loop does.

Derivation:

1. Green, to make it easy: each caller of `timesIn` needs an `effects` at
   the call. Check from the outlines which do; where one does not, move
   its `newEffects` up (move_statement_up) or extract it, while green.
2. Per caller, test first (a use in a literal passed to a module function
   that runs it only during the call), red, then the one edit: `nil`
   becomes `ef.runsDuring` at that call.
3. If every caller then passes `ef.runsDuring`, `during` is always the
   same expression of an `effects`: refactor so timesIn gets it from
   there (a parameter that is one expression of another: the inverse of
   introduce_parameter), or leave it if the `lined` differs (inline_function
   uses another file's).
4. The local: test the shape #525 set aside (`let t = 0` before
   `each(xs, x => { t += x })`), red, then the edit in `assignedInside`:
   a literal for which `runsDuring` holds is not "nested".

## Notes

- read: the five `timesIn` calls (simplify.go 330-380, fold.go 255-312, inlinefunc.go 505-565), to see which have an `effects` in scope: all do (`ef`, `in.ef`), so step 1 needs no refactoring.
- plan: extract_variable's two calls and inline_function's second cannot change: each `stmt` comes from `stmtOf`, which stops at a function, so `timesIn` never crosses a literal there. Only fold (a use of a parameter in the folded body) and inline_function's first call (a parameter used in the callee's body) can; a test for each, then the local.
- read: `reorder.Times` and `Repeat`, to learn whether fold's mapping of anything but Once to Maybe is right for a literal: Maybe lets a read repeat, Several does not; fold maps a loop's use to Maybe too and checks the matched code as `Past`, so a literal run during the call is the same case.

## Measured

`refactor-tokens -log .claude/session-log/ts-during-callers.jsonl`, before
this record's last edits:

    - tool calls: 22
    - through gorefactor: 0 refactoring steps, 11 edit steps, and 1 calls of the CLI (not counted in steps)
    - refused by gorefactor: 0
    - by hand: 0
    - refused, then done by hand: 0
    - denied or failed (no result): 2
      - Bash: for f in simplify fold inlinefunc; do go run ./cmd/refactor inspect file=... (exit 1)
      - Bash: go run ./cmd/refactor-tokens -log ... (this measurement, still running)
    - reads: inspect: file 1, inspect: module overview 2, inspect: targets 1

The two "module overview" reads were `names` (`reorder.Times`,
`effects.isLocal`): refactor-tokens misclassifies them (roadmap 49.6).

## Assessment

- **Through the tools:** every write went through gorefactor (11 edit
  steps, none refused, nothing by hand). No refactoring was needed:
  the change was two arguments (`nil` to `ef.runsDuring`) and one
  condition, so a derived share of 0 is what the change was, not
  work around the tools.
- **The plan held in shape, not in size.** Step 1 needed no
  refactoring (every call had an `effects`). Step 2 shrank from five
  calls to two: three can never cross a literal, which the outline
  could not show and reading the calls did. Step 3 was dropped (three
  `nil`s stay, and inline_function's `lined` is another file's). Step 4
  went as written.
- **Tests from the plan did not discriminate:** the first fold and
  inline_function cases passed before the change. Reading `Repeat` (what
  `Several` refuses that `Maybe` allows) gave the shapes that do. A test
  that passes at red is the sign; the record caught it.
- **Reading:** one read of the three call sites (scope, which names
  alone do not show), two `names` reads to write the edit. No read
  denied; one CLI loop to list outlines failed and was replaced by an
  `inspect` of the file.
- **One change that would have helped:** refactor-tokens counting
  `names` reads as such (roadmap 49.6), so the record's reading line is
  right without a correction.

## Follow-ups

- Roadmap 49.6, refactor-tokens and `names`.
- extract_variable and inline_function's call-site `timesIn` pass `nil`
  and can stay so while `stmtOf` stops at functions; if a statement
  inside a literal ever becomes a target, they need the predicate.
- plan: the first fold and inline_function cases passed before any change (a hole that only reads, a parameter never reassigned, judge the same either way); recast on what `Several` refuses and `Maybe` or a bound argument does not: an array literal fold makes once, a `let` assigned twice that inline_function bound at once. Then all three failed as expected.
