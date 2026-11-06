# move_file's to in another module

- **Item:** roadmap 39g, in the defects list and `docs/parity.md`
  section 1: a call that names a file of another module works there, its
  paths made relative to that module's root, but move_file's `to` was not
  among the paths, so it stayed relative to the home module.
- **Date:** 2026-11-06
- **Branch:** move-file-other-module

## Plan

At the highest level: the routing knows which arguments are paths by
name (`file`, `to_file`, `files`); `to` is a path for move_file and for
`move` without a name, and a type otherwise. Name it a path when no
`name` is beside it, in both the root detection and the mapping.

- Red: a Language test: move_file in a nested module; move_file into
  another module refused as two modules.
- Green: `to` beside no `name` in NamedFiles and MapFiles.

## Notes

- read: route, NamedFiles, MapFiles, TestOtherModules; `move`'s schema
  for when `to` is a type.
- plan: the first test, with a sibling module, passed before the fix;
  `../other/c.go` means the same from either root. A nested module
  showed the defect.

## Measured

Not measured: a cloud clone with no session log. One add_declaration
(the test), two replace_text on its literals, then one batch of two
replace_expression steps and two comment edits.

## Assessment

- **Through the tools:** structured edits; no hand edit of Go.
- **Large writes:** the test (new).
- **Plan:** held.
- **Reading:** bodies read to place the change.
- **One change that would have helped:** none.

## Follow-ups

- TypeScript's routing to other projects (the rest of 39g).
