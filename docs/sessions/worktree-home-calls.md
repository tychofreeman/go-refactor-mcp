# inspect, check and undo from a worktree

- **Item:** roadmap 50's first open point (2026-11-02, the user: "handle
  the open things, one per PR"): `inspect` and `check` without a file,
  and `undo`, act on the server's home even when the caller is in a
  worktree, since `cmd/worktree-paths` has no path to rewrite.
- **Date:** 2026-11-02
- **Branch:** worktree-home-calls

## Plan

The concept: a call's module is decided by the files it names, and a
call that names none falls back to the server's home. The caller's tree
is a second way to name a module, which the hook knows and the server
does not. So the hook adds it, as an argument the server reads and does
not publish (`worktree`, an absolute directory), and `route` uses it
when the call names no file. Undo's stack is the session's, shared by
every agent: from a worktree, undo refuses when the last change is not
in that tree, rather than revert another agent's change.

Derivation, in TDD's order:
1. Green: make the change easy. `undo` takes no arguments anywhere:
   add_parameter `args json.RawMessage` to `lang.Language.Undo` (the
   interface, with Go's and TypeScript's), value nil; add_parameter to
   `Server.undo`; replace_expression so the tool passes its arguments.
   Behavior unchanged.
2. Red: in `golang_test.go`, inspect and check with `worktree` and no
   file work in that module; undo with `worktree` refuses a change made
   in the home module.
3. Green: `route` reads and removes `worktree`; `Undo` checks the last
   change's module.
4. The hook adds `worktree` to every call made from a worktree (test
   first).

## Notes

- Step 1 as planned: add_parameter on the interface method changed Go's,
  TypeScript's and a test fake's `Undo` and their four calls in one
  step; two replace_expression passed the arguments through.
- refusal: check with `packages` "./internal/mcpserver/ ./internal/lang/..."
  is one pattern, not two; reran with ./internal/....
- refusal: extract_function of Go's new block into a helper: it returns
  through `route`'s named result `err`. hand: added `lang.TakeWorktree`
  and replaced the block with a call by replace_statements.
- plan: TypeScript too. Its `route` refused the unknown key, its
  `Inspect` decoded the arguments before routing (the early decode
  dropped, by replace_statements), and its `Check` did not route at
  all. introduce_parameter on `test` was refused (it is passed as a
  value, `Test: l.test`), so extract_function made `testIn(root,
  pattern)`, and `Check` routes and runs the tests there.
- hand: `Check`'s body rewritten with replace_statements (new behavior,
  routing, mixed with the old lines); then a fix of my own: the tests
  had moved from `l.root()` to `l.dir` for the home solution.
- refactor: the two undo checks (Go by module root, TypeScript by
  directory) became `lang.UndoElsewhere`, by directory, with
  add_declaration and two replace_statements calls: extract_function
  cannot take code that returns the function's results.
- hand: the hook's `changed` flag removed with replace_statements (two
  lines): remove_dead_code does not take a local that is assigned but
  never read.

## Measured

`refactor-tokens -log .claude/session-log/worktree-home-calls.jsonl`,
before the docs:

- tool calls 58; through gorefactor 3 refactoring steps, 28 edit steps
  (11 of them text edits of Go); refused by gorefactor 7; by hand 0
- large writes: the two tests (35, 36 lines), TypeScript's `Check` body
  (30), `lang.UndoElsewhere` (23)
- requests 44, input tokens 13.3M, output 27.7K; refused turns 7
  gorefactor, 1 Bash read

## Assessment

- **Through the tools or around them:** no write by hand outside
  gorefactor, but 11 of 28 edit steps were text edits of Go. Most wrote
  new behavior into existing functions (the `route` blocks, the undo
  checks); four replaced code an operation could not move: Go's block
  into `TakeWorktree` and both undo checks into `UndoElsewhere`
  (extract_function refuses code that returns its function's results),
  and the hook's dead `changed` flag (remove_dead_code takes no local).
  The refactorings were add_parameter on an interface method (four
  functions, four calls, one step), extract_function (`testIn`) and
  the earlier undo pass-through.
- **Large writes:** all new behavior; TypeScript's `Check` body
  rewritten whole kept six old lines, which an edit of the parts (route
  first, then the decode) would have kept in place.
- **Cost:** 44 requests; 7 refusals: two from my arguments (a package
  pattern of two, add_element's `type` on a line with two literals),
  one stale `old` text, and four that named a missing operation.
- **Did the plan hold?** For Go, yes. TypeScript was not in the plan;
  its `route`, `Inspect` and `Check` each needed a change.
- **Reading:** bodies read to write the change (`route`, `Undo`,
  `Check`, `test`), none to find uses.
- **One change that would have helped:** extract_function of code that
  returns the enclosing function's results, when the caller can return
  the new function's results (a refusal string here); roadmap 50.5.

## Follow-ups

- Roadmap 50's other open points: users without the hook, and
  `refactor-tokens` with worktree transcripts.
- Not yet tried live: the server needs a reconnect after merge; until
  then the hook (built from the branch) adds `worktree`, which the old
  server refuses.
