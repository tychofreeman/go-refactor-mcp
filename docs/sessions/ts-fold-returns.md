# TypeScript's fold of bodies that return

- **Item:** roadmap 67, TypeScript's part: parity with Go's fold of a
  function with results (#694-#696), asked by the user's session.
- **Date:** 2026-11-19
- **Branch:** ts-fold-returns

## Plan

The change: fold's pattern is one expression, or statements that do not
return; it becomes statements that may return, when every return gives
a value and the last statement is one. The copy then ends in the same
return (the pattern's last statement) and becomes `return f(args);`.
Its early returns return from the copy's function what the call
returns, so nothing else changes about matching. What is new is a check
per copy: the function it is in must take f's return type (Go's
`resultsTaken`), and a copy in an async function or a generator is left
(its return is not the value returned).

Steps: red cases in shapes_test.go's TestFold (a copy ending in the
return; one with an early return; a return type refused; a bare
`return;` refused, in place of the `two` case, whose text changes);
then in tsapi's Fold, the returns visit refuses only a bare return or a
body not ending in a return, and the edit for statements writes
`return ` before the call; then the per-copy check. The error form:
decide whether TypeScript has a counterpart before writing any of it.

## Notes

- read: fold.go's outline, then Fold whole (lines 20-423), once; TestFold's
  cases by pattern then lines. No other Go read.
- refusal: none by the tools. The first inspect and the first batch
  were stopped by auto mode because the worktree hook rewrote relative
  paths to absolute ones; sent again as rewritten, then absolute paths
  throughout.
- plan: red cases by add_element (5) and replace_text on the `two`
  case's strings; green in one batch (replace_statements of the returns
  visit, insert_statements for the per-copy check, add_declaration of
  `returnsTaken`), with check "end" chosen by the server.
- plan: the error form. TypeScript signals failure by throwing, so
  Go's `v, err := f(); if err != nil { return ..., err }` has no
  counterpart at the call. What the Go form is for, a copy of a lookup
  that goes on using what it found, is: `const h = get(); if (h ===
  undefined) throw ...; ...h...` becomes `const h = f(...)`. Built that,
  narrow: f's only return is its last, `return x`, x declared alone with
  a value by one of f's statements.
- red to green: one batch of 10 steps (a field on `found`, the window
  loop trying the shorter pattern, the leak check exempting x, the type
  check, the edit's text, `onlyDeclaration`).
- The first case's `report` (a copy of total's loop that goes on to
  `return String(sum)`) now folds too, to `let sum = total(b);`; the
  expectation was changed by replace_text. Go leaves it, since its
  error form needs an error result.
- hand: this record (Write/Edit of Markdown); no Go by hand.

## Measured

Measured afterwards (2026-11-19, #701): the hook had logged this
subagent's calls to `main.jsonl` (roadmap 70); they were split out by
their tool_use ids in its transcript and measured against that
transcript.
`refactor-tokens -log .claude/session-log/ts-fold-returns.jsonl`:
49 tool calls; 0 refactoring steps and 25 edit steps through gorefactor,
13 of them text edits of Go (replace_statements and replace_text on
fold.go and shapes_test.go); no refusals; 4 by hand (this record by
Write and Edit, two shell commands); 3 large writes, all to
`internal/ts/tsapi/fold.go` (replace_statements of 21 and 24 lines,
add_declaration of 21); 5 shell reads of Go refused by read-guard, 18
shell reads in all (grep of the TypeScript compiler's sources in the
module cache among them). 48 requests, 5.05M input tokens, 39.9K
output; 2 turns of ToolSearch alone.

## Assessment

Through the tools throughout; the plan held for the first two shapes,
and the third became the throw-form counterpart after the decision noted
above. TypeScript's fold had no goldens, only shapes_test.go's cases, so
the Go goldens were mirrored as cases there. One change that would have
helped: the worktree hook's rewrite costing a turn under auto mode
(friction log).

Added with the measurement (#701): "through the tools" holds (no Go
by hand), but no step was a refactoring, and 13 of 25 edit steps wrote
Go as text: the change went in as edits of fold.go's statements rather
than as refactorings that make the change easy and a small edit. The
three large writes are the new matching and checks (new behavior). The
shell reads, 5 refused, cost turns that inspect would not have.

## Follow-ups

- No stress run: simplify alone has a TypeScript stress test (Hono).
  A fold stress test would check the new shapes against Hono's tests.
- A copy that goes on, with early returns in f (Go's error form proper)
  is not folded in TypeScript; a `T | undefined` finder whose callers
  return undefined on a miss would be its counterpart if one is seen.
