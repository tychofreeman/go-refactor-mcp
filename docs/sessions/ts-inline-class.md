# inline_struct for TypeScript

- **Item:** roadmap 52.3, the last Go-only tool; item 3 of "do items
  1-3".
- **Date:** 2026-11-06
- **Branch:** ts-inline-class

## Plan

At the highest level: a class held in a property is a struct held in a
field; inlining it moves its members into the holder, cuts the
property from every use, and deletes the class. A first version takes
only the case with nothing to translate: a property made with `new S()`
and no arguments, S plain (no constructor, static members or heritage),
used nowhere else, and the property used only to reach S's members.

- Red: TestInlineClass (applied; four refusals).
- Green: `tsapi.InlineClass`; the `inline_struct` tool; `inline` routes
  `field` to it.

## Notes

- read: Go's inline_struct golden, TypeScript's inline, the class and
  property AST names tsapi already uses.
- refusal: insert_statements on a case clause's line; add_case instead.
- bulk: `tsapi/inlineclass.go` (125 lines), new behavior.

## Measured

Not measured: a cloud clone with no session log. One create_file, two
batches of structured edits.

## Assessment

- **Through the tools:** structured edits; no hand edit of Go.
- **Plan:** held.
- **One change that would have helped:** none.

## Follow-ups

- Go's `elsewhere` (keep the class when used elsewhere) and `prefer`
  (members both have), and constructors with no arguments whose body
  only assigns fields, if a run asks for them.
