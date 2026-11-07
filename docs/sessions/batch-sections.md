# Code sections after a CLI call's JSON

- **Item:** the user, 2026-11-07, after a look at this session's malformed
  payloads ("Would it help to have a tool that you can give some code to
  and a json name to and which would create the json directly for you?"),
  then "Let's build the refactor batch extension next."
- **Date:** 2026-11-07
- **Branch:** batch-sections

## Plan

At the highest level: JSON read from stdin or a file (`-`, `@path`) may be
followed by named code sections, `@@name` alone on a line and the text
after it; a string value `"@name"` in the JSON is replaced by that text, so
code travels verbatim and nothing is escaped by hand. One call, any tool or
batch.

- Red: a CLI test, a batch from stdin whose two steps take their content
  from sections (quotes and a `\n` in a Go string among them), and one
  where a section is not used.
- Green: `withSections(data)` in a new file of `cmd/refactor`, called by
  `jsonFrom` on what it read: split at the first section line, decode the
  JSON, replace the strings that name a section, encode it again. A
  section no string names is an error (a typo in the name would otherwise
  pass silently); a section's last newline is dropped.
- Docs: the CLI's usage line, CLAUDE.md's and the session notes' cloud
  recipe.

## Notes

- refusal: create_file of sections.go named `mapsKeys`, which does not
  exist (`maps.Keys` was meant); resent the whole file, 79 lines, as
  create_file has no retry.
- bulk: sections.go, 79 lines by create_file: new behavior.
- plan: the README's example was run end to end in a scratch module, and
  this record's sections were sent as sections of one batch, the first
  use. Its heredoc ended early: a line of the text was the delimiter EOF
  itself, so the friction entry's section was never read and its
  placeholder was appended as text. A string naming no given section is
  now an error (test first); the delimiter for sections is BATCH.
- refusal: the fix's first batch used sectionName before the step that
  declared it ran; declared it, then sent a retry with the shifted lines
  and the content again as sections.
## Measured

No session log (a cloud session started outside the checkout). Through
the CLI: add_declaration of the test, create_file of sections.go (twice),
insert_statements of the call in jsonFrom, replace_text on the docs.

## Assessment

Test first, through the tools; the only Go written is the new behavior
and its test. The refusal cost a resend of the whole new file, the waste
this item removes for batches and that create_file keeps for single
calls.

## Follow-ups

- The MCP server needs none of this: its client encodes the arguments.