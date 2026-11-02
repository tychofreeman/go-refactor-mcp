# Following a session into a worktree, without the hook

- **Item:** roadmap 50's open point "a user without the hook keeps the
  hazard" (2026-11-02, the user: "handle the open things, one per PR").
  MCP roots are deprecated as of protocol 2026-07-28 (SEP-2577; they
  keep working for at least twelve months); asked, the user chose to
  build on them anyway.
- **Date:** 2026-11-02
- **Branch:** worktree-roots

## Plan

The concept: a call's worktree, which the hook adds as `worktree`, can
also come from the client's roots, for a session that entered a
worktree (not for a worktree subagent, whose roots are the parent's).
So the server supplies the same argument when the hook did not, and
everything after `route` stays as it is.

1. Green: the tool handlers take the call's arguments, not the request;
   the roots step needs the request (its session's capabilities, the
   input responses). Wrap the handlers in one function that turns the
   request into arguments, unchanged for now.
2. Red: `route` resolves a relative path against `worktree` when the
   call carries one (Go and TypeScript tests); a server test with an
   in-memory client whose root is a worktree: a replace_text by
   relative path edits the worktree's file.
3. Green: `route`'s base; the wrapper asks for roots by an input
   request (when the client advertises roots and the call has no
   `worktree`), and on the retry adds `worktree` when the root is a
   git working tree other than the server's home.

## Notes

- Asked whether to build on deprecated roots (SEP-2577); the user chose
  to. The probe (rebuilt, re-added by the user, a restart) showed Claude
  Code advertises roots at initialization and in each request's `_meta`
  (`"roots":{"listChanged":true}`), so the server asks only a client
  that offers them.
- Step 1 as planned, by add_declaration and five replace_statements
  (one line each, the handlers' bodies): no tool replaces a call's
  argument with a function of the request, so each body was rewritten.
- refusal: replace_text of the TypeScript test's request string
  crossed a `+tree+` concatenation, which `-no-code-by-text` refused;
  replace_expression did it.
- hand: `arguments`' body written with replace_statements (new
  behavior over a one-line stub).
- fix of my own: `home` from a relative directory would have differed
  from every (absolute) root; `filepath.Abs` first.
- refactor: the working-tree lookup in the hook and the server: the
  hook's moved to `lang.WorkingTree` (move_declaration);
  merge_duplicate_function refused the server's copy, `lang.Exists`
  against `Exists` counting as different names for the same function,
  so its body became a call (replace_statements) and inline_function
  removed it. The five handler closures became `s.handler(...)` by
  replace_expression each.

## Measured

`refactor-tokens -log .claude/session-log/worktree-roots.jsonl`, before
the docs:

- tool calls 39; through gorefactor 2 refactoring steps, 27 edit steps
  (11 of them text edits of Go); refused 3; by hand 1 (the Bash call
  that merged #539 and made the branch)
- large writes: the server test (43 lines), `arguments`' body (25)
- requests 27, input tokens 9.9M, output 22.4K; one ToolSearch-only
  turn, 3 refused

## Assessment

- **Through the tools or around them:** of the 11 text edits of Go, 3
  were comments and a test's string (allowed); 8 changed code: the five
  one-line handler bodies (step 1), `arguments`' new body, the `Abs`
  fix, and the server's lookup made a call. The refactorings were
  move_declaration (to `lang`) and inline_function; replace_expression
  folded the five closures into `s.handler`.
- **Large writes:** both new behavior.
- **Cost:** 27 requests; three refusals, one each from
  `-no-code-by-text` (right), replace_expression leaving `err` unused,
  and merge_duplicate_function's names.
- **Did the plan hold?** Yes; the refactor step (the shared lookup and
  the wrapper) was added after green.
- **Reading:** the SDK's types and the handlers, to write the change.
- **One change that would have helped:** merge_duplicate_function
  matching a package's name and its qualified use as the same
  (`Exists` in `lang`, `lang.Exists` outside): roadmap 50.6.

## Follow-ups

- Live check after the merge and a reconnect: EnterWorktree, then a
  relative-path edit with the hook removed from settings.
- Roots are deprecated (SEP-2577); when Claude Code drops them, the
  server stops asking (it checks the capability) and only the hook
  remains.
