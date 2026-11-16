# add_declaration keeps template literals

- **Item:** #637, from the code review of 2026-11-14 (bug 2): TypeScript `add_declaration`
  re-indents the lines of a multi-line template literal in the content it places.
- **Date:** 2026-11-14
- **Branch:** add-declaration-template

## Plan

The change: `place.go`'s `reindent` must know which lines of a member are part of a string's
value. `lined.reindent` (simplify.go) already finds them from the AST; the rule should be one.

Derivation: green, `extract_function` of `lined.reindent`'s loop into `lined.literalLines`.
Red: a test in `adddecl_test.go` placing a method whose template has an indented second line.
Green: `reindent` parses the members it is given (wrapped in a class, as `PlaceDeclarations`
does) and skips `literalLines`, both when finding the shared indentation and when changing it.

## Notes

- read: `lined.reindent` and `PlaceDeclarations`, to find where content is re-indented and what
  already protects literals.
- read: `adddecl_test.go`'s helpers and `TestPlacesByKind`, to write the new test like them.

## Measured

Not measured: a cloud clone with no MCP server and no session-log hook. Every Go change went
through `cmd/refactor`: `extract_function` (`literalLines`), `add_declaration` (the test),
`replace_statements` (`reindent`), and `replace_text` for two doc comments.

## Assessment

Through the tools: no Go was written by hand. The plan held, refactor first while green, then
red, then green. Reading was for writing the new behavior and the test.

The new `reindent` parses the segment a second time; `PlaceDeclarations` already has it parsed
(`cf`). Passing `cf`'s lines down would avoid that, but the offsets would need mapping per
segment; left as is, since a member is small.

## Follow-ups

- #638: `ReindentSteps`, the third copy, still converts steps inside strings.
- #642: one rule for re-indenting per language; `literalLines` is the TypeScript half.
