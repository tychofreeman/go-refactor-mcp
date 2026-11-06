# move_declaration's new_name on an embedded type

- **Item:** roadmap 38e, in the defects list and `docs/parity.md`
  section 1: move_declaration with `new_name` left an embedded field's
  old name in keyed literals and selectors, and rolled back as a bug.
- **Date:** 2026-11-06
- **Branch:** move-embedded-new-name

## Plan

At the highest level: an embedded field takes its name from its type, so
renaming the type renames the field; every use of the field (a literal's
key, a selector) must take the new name. Probe first, since the roadmap
said "if it still fails".

- Red: golden `pkg_embedded_renamed` (moved to another package as
  `Place`, used as `inliner{site: ...}` and `i.site.Path`).
- Green: in updateReferences, a use of an embedded field whose type the
  move renames is rewritten to the new name.

## Notes

- read: updateReferences, to see how uses are rewritten; the existing
  new_name golden for the output's shape.

## Measured

Not measured: a cloud clone with no session log. One batch: an
insert_statements (13 lines) and `embeddedField` by add_declaration (20
lines, new behavior).

## Assessment

- **Through the tools:** structured edits only; no hand edit of Go.
- **Large writes:** `embeddedField` (new behavior).
- **Plan:** held.
- **Reading:** one body, to place the change.
- **One change that would have helped:** none.

## Follow-ups

- 39g (move_file's `to` in another module), 42c (values reaching
  reflection through interface variables).
