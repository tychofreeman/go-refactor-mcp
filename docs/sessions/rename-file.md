# rename of a file path

- **Item:** roadmap 54.1's remainder, `rename` of a file path (the
  user, 2026-11-05), chosen with "what's next?" ("Do all 4", the third).
- **Date:** 2026-11-06
- **Branch:** rename-file

## Plan

rename with `file` and `new_name`, and no `name`, `line` or `function`,
renames the file in its directory: its Plan returns move_file's plan with
`to` the new name beside the file (`refactor.FileRename`, shared by both
languages). A new_name with a slash is refused, naming move. `name`
becomes optional in both schemas.

## Notes

- refusal: add_declaration into `internal/refactor/onlyargs.go`, which is
  on the unmerged one-inline branch; the helper went into `move.go`.
- refusal: replace_expression on the wrong line (192 for 193); the
  refusal named the line.
- hand: none.

## Measured

Not measured: a cloud session with no session log; nine CLI calls.

## Assessment

- **Through the tools:** inserts, a new helper by add_declaration, text
  edits on struct tags and descriptions only.
- **Plan:** held.

## Follow-ups

- Java: `rename` of a file path, with its `move` (row 109).
