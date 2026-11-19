# A batch step finds its anchor by its text

- **Item:** roadmap 65, chosen from "what's next?". In 6 of 8 runs of
  cost-history 34, a batch had `Road: $v` and then `Rail: $v`
  (replace_expression, `everywhere`), both anchored on one literal's
  line. The first step rewrote the line, so the second was refused
  ("rewritten by step N") and sent again alone.
- **Date:** 2026-11-18
- **Branch:** batch-anchor-by-text

## Plan

The change, at its level: a batch already follows a line with a column
as a position, which stays addressable while what is at it survives,
even on a line a step changed (`translateLines`, `lineMap.position`).
A step that names its target by text (`expression`) and gives no column
has, in that text, what it needs to find the column on the line as it
was when the batch began. So the rule is to derive the column from the
text and then follow the position as today. When a step changed only
part of the line, a later step's expression elsewhere on that line can
still be addressed.

Derivation (TDD):
1. Red: `TestBatchFindsAnExpressionOnARewrittenLine` in mcpserver's
   tests. A literal `{Road: "nw", Rail: "y7"}` and a batch of two
   replace_expression steps on its line, `Road: $v` and `Rail: $v` to
   `Routes: ...` with `everywhere`, applies and leaves `{Routes:
   map[string]string{"road": "nw", "rail": "y7"}}`.
2. Green: in translateLines, when `line` has no column, the line was
   rewritten, and the step has `expression`, take the text up to the
   first `$` hole. If it occurs once on the original line, its column
   goes to the position path. Otherwise the refusal stays as it is.
3. Refactor: extract the column lookup into a function, if it reads
   better there.
4. Docs: the roadmap, checks.md (batch section, if it has one), the
   record.

Bodies read: translateLines and lineMap.position (to place the change),
and TestBatchLinesFromBeforeTheBatch (the test's shape).

## Notes

- read: translateLines, lineMap.position and the batch tests' shape, to
  write beside them.
- refusal: insert_statements "before" line 1150 was refused, since 1150
  is a comment and no statement starts there. The retry put the block
  "after" line 1149 (the `if` that ends there), so the column branch's
  comment stays above the code it describes.
- plan: step 3 wasn't needed. textColumn was a function from the
  start.

## Measured

`refactor-tokens -log .claude/session-log/batch-anchor-by-text.jsonl`,
before the docs steps: 20 tool calls, 3 edit steps through gorefactor
(none of them text edits of Go), 1 refused (the insert_statements
above), nothing by hand, no large writes. Two Bash calls were denied:
the read guard refused a grep that named a Go test file, and a pipeline
that ran with it. Derived share 6%: this is new behavior. The
transcript part: 15 requests, 2.56M input tokens (cached or not).

## Assessment

- **Through the tools:** all of it. The test came in with
  add_declaration, the call with insert_statements, and textColumn with
  add_declaration. A replace_text changed the doc comment of
  translateLines.
- **Large writes:** none.
- **Cost:** 15 requests. The refused insert_statements cost one retry,
  and the denied grep cost one turn: I used grep for a Go file out of
  habit, where inspect's `pattern` was the tool.
- **Plan:** it held, except that step 3 wasn't needed.
- **Reading:** translateLines, position and one test, to place the
  change. That is reading to write.
- **One change that would have helped:** insert_statements "before" a
  comment line could take the statement the comment heads. But that
  places the new code between the comment and its statement, which is
  wrong here. The refusal was right, and "after" the statement before
  is the move. No roadmap entry.

## Follow-ups

- A pattern that starts with a hole (`$s.Road`) has no text before it
  to find, so it is still refused on a rewritten line. The text after
  the hole (`.Road`) could find it instead.
