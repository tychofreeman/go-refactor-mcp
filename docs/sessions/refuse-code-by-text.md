# Refuse Go code changed by replace_text

- **Item:** roadmap 49.11, asked by the user ("I'd like to play with
  3") after #532's fix turned out to be Go pasted with replace_text
- **Date:** 2026-10-31
- **Branch:** refuse-code-by-text

## Plan

The change: under a server option, replace_text may change a Go file
only inside string literals and comments; anything else is refused,
naming the structured edits. The rule, stated on the file: the Go
tokens other than strings and comments are the same before and after
the edit. That one comparison covers old in code, new breaking out of
a string, append, and all_occurrences, with no case for each.

Where it goes: `ReplaceText` gains a field, `TextOnly`; `Plan` compares
the token streams of each Go file before and after its edit (go/scanner)
and refuses when they differ. `golang.Language` gains `NoCodeByText`,
set by `refactor-mcp -no-code-by-text` as `ReadByName` is by
`-read-by-name`, and passes the strict replace_text in `Step` and to a
batch's steps. `.mcp.json` turns it on for this repo's sessions, after
the merge and a reconnect. Published text does not change, so the A/B
baselines hold; TypeScript is left alone for now.

Derivation:
1. Green, to make it easy: the golden cases move to
   `testdata/replace_text/` (move_file), as other packages keep a
   directory per variant, so `testdata/text_only/` can sit beside it.
2. Red: golden cases under `testdata/text_only/` run with
   `&ReplaceText{..., TextOnly: true}`: a string literal changed
   (applies), a comment changed (applies), code changed (refused), new
   breaking out of a string (refused).
3. Green: the field and the comparison, the least code.
4. Then the language and the flag: a test that `Step` with the option
   refuses code, the field, the swap.

## Notes

- read: replace_text's `Plan` and `fileEdit`, `edit.Apply` and the
  golden harness's `Run`, to write the check and find where tests of a
  variant go.
- plan: the 18 cases moved with one batch of move_file; the test
  declared `TextOnly` as a stub.
- hand: `func (*ReplaceText) Plan` given the receiver `r` with
  replace_text. No tool names an unnamed receiver: rename needs a name,
  replace_expression an expression, and a receiver is neither.

## Measured

`refactor-tokens -log .claude/session-log/refuse-code-by-text.jsonl`,
before the record was finished:

- tool calls: 53
- through gorefactor: 0 refactoring steps, 23 edit steps (4 of them
  text edits of Go: the test's path string, the `TextOnly` comment, the
  receiver, the field's line deleted)
- refused by gorefactor: 4 (two ambiguous imports, a batch's call
  before its declaration, replace_declaration of a field)
- by hand: 1 (a Python script that wrote the move batch's JSON to the
  scratchpad, then unused: the batch went inline)
- large writes: 1 (add_declaration of `codeChanged`, `codeToken` and
  `codeTokens`, 57 lines)
- denied: 1 (a leftover `sed` of a Go file in a grep command)
- reads: inspect: targets 5, names 3, file 2; Bash 12 (testdata, CI)
- transcript: 39 requests, 6.67M input tokens, 27.8K output; 4 turns
  lost to refusals, 1 to the guard, 1 to a schema load

## Assessment

- **Through the tools or around them:** through, with two exceptions,
  both where no tool reached: the receiver and the field's deletion
  (roadmap 49.13). Both would now be refused by the option this item
  adds, which is the point: the session would have had to stop and say
  so. The other two text edits of Go were a string literal and a
  comment, which the option allows.
- **Large writes:** the 57 lines are new behavior (the token
  comparison), not existing code typed again.
- **Cost:** 39 requests. Two refusals were ambiguous package names
  (scanner, replacetext), the kind `imports` answers; one was a call
  placed before its declaration in a batch without check "end".
- **Did the plan hold?** Yes, in its order: move the goldens while
  green, red cases, the field and the comparison, then the server
  test, the language's `strict` and the flag.
- **Reading:** the bodies read were to write the check (Plan,
  fileEdit, edit.Apply, the harness's Run) and the server's option
  plumbing; none to find uses.
- **One change that would have helped:** an edit that deletes or moves
  a field, and one that names a receiver (roadmap 49.13).

## Follow-ups

- Try it: from the next session (after a reconnect), replace_text on
  Go code is refused here. Watch whether sessions then reach for the
  structured edits or for replace_statements, which is still allowed.
- Not yet: the `refactor` CLI, TypeScript, an A/B arm (costs money).
- Roadmap 49.12 (an argument to a call) and 49.13 (fields, receivers)
  are the gaps the option will meet first.
- refusal: replace_declaration of `Server.NoCodeByText` (to move the
  stub field beside `ReadByName`): "not a top-level declaration".
- hand: so the stub field's line was deleted with replace_text, then
  add_field put it after `ReadByName` with its doc. No tool deletes or
  moves a field (extract_struct moves fields into a new type).
- refusal: twice, a package name in new code was ambiguous (go/scanner
  or text/scanner; Go's or TypeScript's replacetext); resent with
  `imports`, as the refusal said.
