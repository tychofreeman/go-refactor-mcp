# inline keeps multi-line raw strings

- **Item:** #636, from the code review of 2026-11-14 (bug 1): `inline` of a call inside an
  expression re-indents the continuation lines of a multi-line raw string in the result.
- **Date:** 2026-11-14
- **Branch:** inline-raw-string

## Plan

The change: re-indenting all but the first line of a text must find the strings it protects
on the whole text, never on a slice of it. `reindentTail` passes `text[i+1:]` to
`textutil.Reindent`, which scans what it is given and so cannot see a string that started on
the first line.

Derivation: no refactoring first; the change is already small and local. Red: a golden case,
`expr_raw_string`, a function returning a two-line raw string inlined into an expression two
levels deeper. Green: `reindentTail` re-indents the whole text and keeps the first line as it
was (`Reindent` works line by line, and the first line starts the text, so it is never inside a
protected span). One `replace_statements` of `reindentTail`.

## Notes

- read: `textutil.Reindent`, to learn that it re-indents lines that do not start with `from`
  too, so the first line cannot simply be passed through it.
- hand: the golden case's `in/` and `out/` files, written with a heredoc; testdata is outside
  the guards and no tool writes a golden case.

## Measured

Not measured: a cloud clone with no MCP server and no session-log hook. Steps went through
`cmd/refactor` (see the notes).

## Assessment

Through the tools: the fix was one `replace_statements` of `reindentTail` and one
`replace_text` of its doc comment, both through `cmd/refactor`. The only text written by hand
was the golden case (testdata). The plan held: no preparing refactoring was needed. One body
was read, `textutil.Reindent`, to write the new behavior, not to check safety.

One change that would have helped: none for the tools. The bug came from a helper taking a slice
of text; #642 (one rule for re-indenting per language) is where that is fixed for good.

## Follow-ups

- #637 and #638, the TypeScript counterparts, are the next items.
