# Tokens and large writes in session records

- **Item:** the user asked (2026-10-31) for the whats-next records to use
  `refactor-tokens`' transcript data, and to show whether any write was
  made because writing lines was easier than the edits or refactorings.
  Not chosen through "what's next?"; recorded here as the skill would.
- **Date:** 2026-10-31
- **Branch:** tokens-in-records

## Plan

At the level of the design: the log already names each call's
tool_use id; the transcript holds the tokens of those same calls. So the
report needs (1) the log to keep the transcript's path, and (2)
`Analyze` restricted to a set of call ids. And a "large write" is a
fact about one call (lines of Go it carried), which every route of
writing has in its arguments.

The derivation, written before reading bodies: extract `Analyze`'s body
into `analyze` (every path returns, so the call is `return analyze(r)`),
add a parameter `ids` passing nil, while green; then the test; then the
edits: a call keeps its id, `analyze` slices its requests when `ids` is
set, `AnalyzeOnly` calls it. For large writes: a field and one helper
per route, small replace_text edits in `AnalyzeWork`, nothing rewritten.

## Notes

Written at the end of the item, not as it happened: the item did not
start through the skill. A cloud session, so no MCP and no hook: every
step went through the `refactor` CLI, with batches written as JSON by
Python in the scratchpad (quoting Go in a shell is the reason).

- `plan:` held. extract_function and add_parameter did the
  restructuring in one batch, green before the test.
- `refusal:` the first test batch was refused: `AnalyzeOnly`'s results
  could not be told from its use, and `ToolUseIDs`' type neither. The
  refusal named the steps (create_from_usage with `results`, add_field);
  added them and resent. create_from_usage typed the stub's parameter
  from the test's argument (`*strings.Reader`); replace_declaration
  gave it `io.Reader`.
- `bulk:` I first wrote the whole of `AnalyzeWork` again in a scratch
  file (84 lines) to replace the declaration, because writing it out
  was easier than finding the five places it changed. It would have
  typed out again most of its 74 lines to change about 12. Not used: five
  replace_text steps scoped with `function` made the change instead.
- `bulk:` the helpers for large writes (96 lines) and
  `transcriptTokens` (29 lines) went in with add_declaration: new code,
  nothing to move.
- `refusal:` a replace_text on the hook's doc comment was refused
  (`old` not found): I had guessed the comment's line breaks. Read it
  with `inspect` and resent.
- `read:` `Analyze` and `Report.Markdown`, through `inspect`, to place
  the slicing and to see how a sliced report reads; `main` of
  refactor-tokens, to place the new call.

The same question for the previous item (#527, the skill itself):
`internal/tokens/work.go` (217 lines) and `cmd/session-log/main.go`
(138 lines) were written whole in scratch files with Write, then put in
with create_file `overwrite` over the stubs the tests had made. The
code was new, so no operation could have moved it; but overwriting the
file rather than replacing each stub with add_declaration (which
CLAUDE.md names) was taken because writing the file out was easier.
The skill and README were new documents, written whole.

## Measured

No session log: the hook runs only in sessions started in the
checkout. From the CLI's results: 7 applied batches, 2 refusals (both answered
through the tools, neither followed by a hand edit), 1 refactoring
batch (extract_function, add_parameter), the rest edits. Large writes
of Go: helpers 96 lines (add_declaration, new), `transcriptTokens` 29
(add_declaration, new), tests 38 and 23 (add_declaration, new); no
existing code written again.

## Assessment

- **Through the tools or around them:** through them. Every change went
  through the CLI; the restructuring of `Analyze` was derived, not
  rewritten. The large writes were all new code.
- **Large writes:** one was started for the reason the user asked
  about, rewriting `AnalyzeWork` whole because that was easier, and
  dropped for five targeted edits. In #527 the same reason led to
  overwriting two files instead of replacing their stubs.
- **Cost:** not measured: no transcript is reachable from this session.
- **One change that would have helped:** the record's `bulk:` note and
  the "Large writes" question exist now because of this. A tool change:
  `create_from_usage` could type a parameter from an argument's
  interface when the test passes a concrete reader (`*strings.Reader`
  for `io.Reader`); not on the roadmap, since one occurrence is not
  evidence.

## Follow-ups

- Run the skill from a local session, with the hook, so a record has a
  real log and transcript (roadmap 49.5).
