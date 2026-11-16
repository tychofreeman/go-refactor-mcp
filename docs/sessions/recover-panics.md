# A panic in a tool is the tool's error

- **Item:** #639, from the code review of 2026-11-14 (bug 4): neither the server nor the MCP
  SDK recovered panics, and the SDK runs each tool call in its own goroutine, so a panic in any
  tool ended the server and its undo history.
- **Date:** 2026-11-14
- **Branch:** recover-panics

## Plan

The change: every tool call goes through `Server.handler`, so that is the one place a panic can
become a tool error. The error names the tool and where it panicked, since the model reports it
and the user fixes it.

Derivation: no refactoring first. Red: `TestPanicIsAToolError` registers a tool whose run
panics (a write to a nil map), calls it over in-memory transports, and lists tools after. Green:
`handler` recovers into a `toolError`; `panicSite` is the first non-runtime frame.

## Notes

- read: `handler`, `arguments`, `toolError` and the test helpers, to see where calls enter and
  how the tests connect.
- refusal: `add_declaration` of the test refused `jsonschema` as not in scope; the CLI's
  `imports=` took no list, so the step went through `batch` with `imports`. Then `go mod tidy
  -diff` showed `jsonschema` becoming a direct dependency, so `replace_expression` put a map in
  its place (the schema field is `any`), and the tool removed the import itself.
- plan: the test first used `connect`, which builds a new SDK server from `s.MCP()`, without
  the tool the test added; it connects to the server it added the tool to.

## Measured

Not measured: a cloud clone with no MCP server and no session-log hook. Every Go change went
through `cmd/refactor`: `add_declaration` (the test, by batch), `replace_statements` (the
test's body), `replace_declaration` of `handler` and `add_declaration` of `panicSite` in one
batch, `replace_expression` (the schema).

## Assessment

Through the tools; no Go by hand. The plan held. One detour: the CLI's `imports=key` form
does not take a list, which cost a refused call; `batch` took it.

What the recovery cannot do: say whether the panicking call wrote part of a change. The error
says to check `git diff` before the next step. Rolling back a half-written change needs #641
(atomic writes).

## Follow-ups

- #641: write a change atomically, so a failure part way leaves nothing to check.
- The CLI's `imports=` could take a JSON list as `content_file` does text; a small friction.
