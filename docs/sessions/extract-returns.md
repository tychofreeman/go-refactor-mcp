# extract_function of code that returns early with a value

- **Item:** roadmap 50.5, chosen by the user from "what's next?"
  (2026-11-02). In #538 two blocks could not be extracted and became
  helpers by text edits: `lang.UndoElsewhere`'s block returned
  `fmt.Sprintf(...), nil` early from `Undo` (a success value, not an
  error exit), and `TakeWorktree`'s returned through `route`'s named
  result `err`.
- **Date:** 2026-11-02
- **Branch:** extract-returns

## Plan

At the level of the operation: extract_function passes returns on in
two shapes, terminal (every path returns) and error exit (every early
return carries a non-nil error or non-empty string, so the call site can
tell a return from falling through by that value). The missing shape is
an early return of any values: the call site cannot tell from the values,
so the new function says so itself, with a bool. `returns.go`'s comment
says this is refused because "the caller could not tell"; a flag makes
it tell.

The new mode, `valueExit`:
- when the returns are not an error exit, every return writes out all of
  the enclosing function's results, none uses a named result, and the
  selection has no results of its own (a first version; with its own
  results, refused as now);
- the new function returns `(bool, <enclosing results>)`; each return
  `return a, b` becomes `return true, a, b`, and falling off the end
  `return false, <zeros>`;
- the call site is `if done, r1, r2 := f(...); done { return r1, r2 }`.

Derivation, in TDD's order:
1. Green: nothing to reshape first; `decideReturns` ends in
   `checkErrorExits`, and the emission switches on `x.mode` with a
   default.
2. Red: the golden `refuse_success_early_return` becomes `value_exit`
   (move_file, then its `out/` and args), and a golden of `Undo`'s shape
   (a loop that returns a refusal string and nil); a refusal for a
   selection with results of its own.
3. Green: `checkValueExits` and the mode in the results, body edits,
   call site and note.
4. Refactor what the copy leaves.

The named-result case (`TakeWorktree`) is left for after: it needs the
named result treated as a local of the selection.

## Notes

- hand: `valueExit` declared as `const valueExit = errorExit + 1` by
  add_declaration, since no tool adds a spec to a const group (add_element
  takes literals; replace_declaration of one spec refuses a second).
- The least code: the mode, `checkValueExits`, `exitNames`, a case in
  the results switch and the notes (add_case), the call site
  (insert_statements); `decideReturns`' last line by replace_statements.
- plan: ten goldens that were refusals (an early return the error or
  string could not mark: a nil error, a possibly empty string, a helper
  not proven non-nil, a void-like int return) now extract with the
  flag. Each output read and correct; renamed `value_exit_*` by
  move_file, `refused.txt` deleted, `out/` copied from `in/`, and the
  outputs written by the golden test's `-update` flag (the test writing
  files, not gorefactor). `refuse_different_values` stays refused, by
  the new rule (it also sets `a`).
- refusal: my message said "the selected lines return ... and sets";
  the golden caught it, fixed to "set".
- refusal (twice this day): check's `packages` is one pattern; I gave two.
  Roadmap 50.7.
- Not refactored: `exitNames` spells the error type twice, in two
  blocks extract_variable's all_occurrences does not reach.

## Measured

`refactor-tokens -log .claude/session-log/extract-returns.jsonl`, before
the docs:

- tool calls 40; through gorefactor 14 edit steps (5 of them text edits
  of Go), no refactoring; refused 1 in the log's count; by hand 1, a
  misreading: an `awk | grep` that read `docs/checks.md` counts as a
  write
- large writes: two golden outputs (20, 22 lines), `checkValueExits`
  with `exitNames` (42 lines)
- requests 27, input tokens 11.8M, output 23.6K

## Assessment

- **Through the tools or around them:** all writes through gorefactor
  but the ten outputs the golden test's `-update` wrote. Of the 5 text
  edits of Go, 4 were the description, comments and a message (strings
  and comments), one code: `decideReturns`' last line, replaced by the
  new fallback (replace_statements; new behavior where one line stood).
  No refactoring: the change added a mode beside two others.
- **Large writes:** new behavior and its expected outputs.
- **Cost:** 27 requests; two refusals of my own (the package pattern,
  the message's grammar) and the earlier wrong line range in a golden's
  `args.json`.
- **Did the plan hold?** The mode as planned. Not foreseen: ten old
  refusals now extract, which the plan's "the caller could not tell"
  reason had covered; each was read before its golden changed.
- **Reading:** `returns.go`'s decision functions and the emission in
  `extractfunc.go`, to add the mode.
- **One change that would have helped:** adding a spec to a const group
  by a tool (add_element for a declaration group); and roadmap 50.7.

## Follow-ups

- Roadmap 50.5's open part: value exits with results of their own, and
  returns through a named result.
- TypeScript's extract_function has its own return shapes
  (`tsapi/extractfunc.go`); the same mode there, if a session meets it.
