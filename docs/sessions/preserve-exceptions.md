# preserve_exceptions for Go

- **Item:** `docs/parity.md` section 2, the user's decision of 2026-11-05;
  item 2 of "do items 1-3". Asked today: per call on every refactoring
  (over a server flag), and a note for each change made without it.
- **Date:** 2026-11-06
- **Branch:** preserve-exceptions

## Plan

At the highest level: the tools allow one change to panics, a nil
dereference moving past code that returns; the flag turns that
allowance off, and without the flag each use of it is named. The rule
lives in `analysis.Move` (and a copy in fold), so judging each move
twice, strictly and as before, finds both the refusal and the note in
one place; the flag travels per call through `refactor.Run` to the
workspace's `Effects`.

- Red: move_statement goldens (noted; refused with the flag).
- Green first: extract Check's body to `check`; a `strict` field.
- Green: Check judges strictly, refuses or notes; Effects carries the
  flag and the changes; Run strips the argument and adds the notes; Go's
  published refactoring schemas gain the argument.
- Then fold's `movedAddress`, the same way.

## Notes

- read: analysis.Move, reorder.Conflict, refactor.Run around Plan, the
  workspace's Effects, fold's mayStop and movedAddress: where the rule
  lives and how a call reaches it.
- plan: the goldens were set aside (moved out of testdata) for the two
  refactorings, as TDD's order asks, and put back after.
- plan: a refusal in fold leaves an occurrence rather than refusing the
  call; the golden was corrected to expect the remainder folded and a
  note.
- hand: goldens' files with the shell and a short script (testdata).
- bulk: the strict-check block in Check (14 lines), `takePreserve`,
  `WithPreserveExceptions` (30 lines): new behavior.

## Measured

Not measured: a cloud clone with no session log. Through the CLI: one
extract_function, about twenty structured edits in five batches.

## Assessment

- **Through the tools:** structured edits and one refactoring
  (extract_function); no hand edit of Go.
- **Large writes:** new behavior only.
- **Plan:** held; fold's case needed its own place, as expected.
- **Reading:** bodies read to find the rule's two homes.
- **Trade-off:** results now carry a note wherever the allowance is used,
  which they did silently before; Check runs twice per move.

## Follow-ups

- TypeScript (tsapi's reads of what may be null or undefined).
- Batch: the flag is per step; a batch-level default is not offered.
