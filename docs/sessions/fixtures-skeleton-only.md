# Golden fixtures under -skeleton-only

- **Item:** the friction-log gap of #653 and #661: under `-skeleton-only`
  create_file refuses a golden fixture with a function body, and no edit
  reaches a fixture (it is in no loaded package), so fixtures were
  written by hand with printf. Third of the four items chosen from
  "what's next?" on 2026-11-16.
- **Date:** 2026-11-16
- **Branch:** fixtures-skeleton-only

## Plan

The change: `-skeleton-only` is about the module's code, which is
built by structure; a file under a `testdata/` directory is data a test
loads as a module of its own, which the go tool never builds. So
create_file under `-skeleton-only` takes any content for a file under
`testdata/`, in Go and TypeScript alike, and changing a fixture is
create_file with `overwrite` (no edit tool needs to reach it).

Derivation, from the outlines of `createfile.Skeleton` (Go,
`internal/edits/createfile/declaration.go`), TypeScript's counterpart
(`internal/ts/edits/createfile/declaration.go`) and the two
TestSkeletonOnly tests:

1. Test first: in each TestSkeletonOnly, create_file of
   `testdata/case/in/m.go` (or `.ts`) with a function body is applied.
2. In each Skeleton's Plan, skip the body check for a path with a
   `testdata` element: one condition.
3. The refusal and `-skeleton-only`'s flag text say fixtures are exempt;
   checks.md and session notes.

## Notes

- read: the two Skeleton.Plan bodies and the two TestSkeletonOnly
  tests, to write into them.

## Measured

`go run ./cmd/refactor-tokens -log .claude/session-log/fixtures-skeleton-only.jsonl`:
13 tool calls; 8 edit steps through gorefactor (3 text edits of Go:
the flag's help and two doc comments, by replace_text); 0 refusals; 0
by hand; no large writes; 22 Go lines written (the two test cases and
the helper). Requests and tokens are this long session's, as for the
two items before it.

## Assessment

- **Through the tools or around them:** through them, nothing by hand:
  the tests by insert_statements, the helper by add_declaration, the
  rule by two replace_expression edits.
- **Large writes:** none.
- **Did the plan hold?** Yes, as written.
- **Reading:** the two Plans and the two tests, to write into them.
- **One change that would have helped:** this one; with it, the goldens
  of #653 and #661 would have gone through create_file.

## Follow-ups

- The record of #661 names a follow-up: move_method and
  function_to_method may write calls from other packages unqualified
  too.
