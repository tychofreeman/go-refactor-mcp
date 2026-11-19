# add_case and a type switch's variable

- **Item:** roadmap item 68, found in the fold-returns session
- **Date:** 2026-11-19
- **Branch:** add-case-typeswitch-var

## Plan

The change: add_case resolves the names its content uses as selector
operands (`pp.Results`) against the scope at the switch's `{`, to import
the packages among them. A type switch's symbol is declared in each
clause's scope, not the switch's, so `pp` looked like a package nobody
imports. The clause add_case writes declares its own `pp`, so the symbol
is to be taken out of the names that may be packages.

Derivation: green first, export `packageRefs` (rename to `PackageRefs`)
so add_case can filter the names before `ImportNames`; red, a golden
`type_switch_symbol` adding a clause that uses `v.Field`; green, add_case
drops the symbol of `switch v := x.(type)` from the names.

## Notes

- read: outlines of gocode.go and addcase.go, then AddCase.Plan and
  gocode's PackageRefs/ImportNames in one targets call; the refusal is
  ImportNames looking names up at `body.Lbrace`. The existing
  `type_switch` golden passed only because its `len(v)` is no selector.
- plan: held. rename, then the golden (red: the same refusal), then one
  replace_statements in Plan.
- read: TypeScript's add_case does no scope or import check of its
  content (inspect pattern), and TypeScript has no type switch: no
  counterpart.
- refusal: none from the tools. The worktree hook rewrites inspect's
  arguments, which auto mode then asked to resend once with the absolute
  path and `worktree`.

## Measured

Measured afterwards (2026-11-19, #701): the hook had logged this
subagent's calls to `main.jsonl` (roadmap 70); they were split out by
their tool_use ids in its transcript and measured against that
transcript.
`refactor-tokens -log .claude/session-log/add-case-typeswitch-var.jsonl`:
29 tool calls; 1 refactoring step (the rename) and 3 edit steps through
gorefactor (1 a text edit of Go: the replace_statements in Plan); no
refusals; 2 by hand (Write and Edit of this record); no large writes;
2 shell reads of Go refused by read-guard. 24 requests, 1.88M input
tokens, 11.7K output; 14 Bash calls, 5 of them reads other than go and
git.

## Assessment

Through the tools: no Go written by hand. The plan held as written.
Reading was two outlines and one targets call.

## Follow-ups

None.
