# Sessions that move into a git worktree

- **Item:** roadmap 50, asked for by the user after "does the MCP work
  with worktrees?" (2026-11-02). This PR: step 1 (reproduce, and find
  what the server can know), then step 2 if step 1 allows.
- **Date:** 2026-11-02
- **Branch:** worktree-sessions

## Plan

At the highest level: the server has one notion of "where the session
works", fixed at start (`$CLAUDE_PROJECT_DIR`), and the session's own
notion can change under it. The change is to give the server the
session's current tree, or, failing that, to refuse what it would get
wrong.

1. Experiment, no code: EnterWorktree in this session, then a
   gorefactor edit of a docs file by relative path, and see which tree
   it changed; undo, ExitWorktree.
2. Find whether Claude Code offers MCP roots: the go-sdk has
   `ServerSession.ListRoots` and `ServerOptions.RootsListChangedHandler`.
   A server change only shows after a reconnect, so this needs the user:
   log the roots at initialize and on each change, reconnect, move into
   a worktree, read the log.
3. Then, by what 2 finds: re-bind `golang.Language`'s home to the root
   (the existing `others` map holds workspaces by root), or refuse
   relative paths when the roots name another tree.

## Notes

- Experiment (step 1): EnterWorktree made
  `.claude/worktrees/wt-probe` (inside the checkout, on a new branch
  from origin/main). A replace_text of `docs/sessions/README.md` by
  relative path then changed the main checkout's file, not the
  worktree's, unrefused; `git diff` in the worktree was empty. The same
  edit by absolute path was routed to the worktree as "the module at
  .claude/worktrees/wt-probe". Both undone; worktree removed.
- Experiment (step 2), a probe server outside the repo (the
  scratchpad's `rootsprobe`, added by the user with `claude mcp add`,
  local scope; a running session loads a new server only after a
  restart). Claude Code speaks MCP 2026-07-28 to it: no `initialized`
  notification reached the server, and `ListRoots` during a call is
  refused by the go-sdk ("return an InputRequests map instead", SEP-2322,
  multi round-trip). A tool result with `InputRequests: {"roots":
  &ListRootsParams{}}` is answered at once (18 ms): the call comes back
  with the roots in `InputResponses`. Roots were the checkout before
  EnterWorktree and `.claude/worktrees/wt-probe` after it. No
  `roots/list_changed` arrived: the server learns of a move only by
  asking, on a call.
- Experiment (step 3): a Haiku subagent with `isolation: "worktree"`
  (its `pwd` `.claude/worktrees/agent-…`) called the probe: the roots
  it got were the main checkout's. So roots follow EnterWorktree but not
  a worktree subagent, which shares the parent's MCP connection.
- But the hooks see it: `cmd/session-log` recorded each call's `cwd`
  from the hook input, and it was the worktree for both the moved
  session and the subagent (`.claude/worktrees/agent-…` for the
  subagent's probe call). A PreToolUse hook is the one place that knows
  the calling agent's directory in both cases.
- plan: the user chose a hook (2026-11-02) after asking whether a
  subagent ever needs the main checkout: no, isolation is the point, and
  Claude Code already refuses `git -C` into it from a worktree. So the
  hook rewrites relative paths to the caller's worktree and refuses
  absolute ones into the main checkout; the server is unchanged, and
  roots (which a subagent does not get) are not used.
- refusal: create_file of the test alone: no stubs, since the package
  had only test files; then the batch with `main.go` first: no result
  types for `place` from its use. A create_from_usage step with
  `results` declared the stub. Both refusals named the next step.
- refusal: replace_statements of `main`: `json` ambiguous between
  encoding/json and encoding/json/v2; `imports` fixed it.
- Live, in a worktree (`wt-hook`): replace_text of a relative path went
  to the worktree, with no permission prompt added (so `updatedInput`
  without a permission decision works for an MCP tool, which the docs
  leave open), and inspect of an absolute path in the main checkout was
  denied with the hook's reason. Undone; worktree removed.

- Live, a Haiku subagent with `isolation: "worktree"`: replace_text of
  a relative path landed in its own worktree (`git status` there showed
  the file); the main checkout was untouched. Its worktree removed.

## Measured

`refactor-tokens -log .claude/session-log/worktree-sessions.jsonl`,
before the docs:

- tool calls: 86
- through gorefactor: 0 refactoring steps, 6 edit steps (1 of them a
  text edit of Go: replace_statements writing `main`'s new body)
- refused by gorefactor: 3
- by hand: 4, none in the repo: building the scratch probe (twice),
  reading the session log with Python, and the hook's smoke test
  (`git worktree add` and `remove` of a scratch worktree)
- large writes: `main_test.go` 71 lines, `main.go`'s `place` and
  helpers 49 lines
- requests 61, input tokens 13.4M, output 35.7K; ToolSearch-only turns
  2, gorefactor refused 3, Bash reads refused 3, a Bash write refused
  1 (the probe's heredoc, by the read guard)
- Two transcripts were not read: the log names a transcript under
  the worktree's project directory for the calls made while in a
  worktree, and none exists there.

## Assessment

- **Through the tools or around them:** every write to the repo went
  through gorefactor (create_file, create_from_usage, add_declaration,
  replace_statements, replace_text on JSON and docs). The probe server
  was written with Write and Edit in the scratchpad: an experiment, not
  this module's code. No refactoring: a new command, nothing to
  reshape.
- **Large writes:** the test (new) and `place`/`worktree`/`inside`
  (new behavior, the least code); no existing code typed out again.
- **Cost:** 61 requests, most spent on the experiments, which needed
  the user twice (adding the probe, a restart, a reconnect). Three
  refusals, each naming the next step.
- **Did the plan hold?** Steps 1 and 2 as planned; step 3 changed: the
  subagent experiment showed roots do not reach a worktree subagent, so
  the user chose a hook over the server, and the server is unchanged.
- **Reading:** the go-sdk read through inspect by absolute path (it
  worked as "another module"); three greps refused for naming Go files.
- **One change that would have helped:** `refactor-tokens -log`
  finding the transcript of a session that entered a worktree (the log
  records a transcript path under the worktree's project directory):
  roadmap 50, in its open line.

## Follow-ups

- `inspect` and `check` without a file, and `undo`, act on the
  server's home, not the caller's worktree.
- The hook is not part of what the server publishes: a user without it
  keeps the hazard. The server could refuse a relative path when MCP
  roots name another tree (the moved-session case only).
- `refactor-tokens` and transcripts of calls made in a worktree.
