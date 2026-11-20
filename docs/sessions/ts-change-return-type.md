# change_type of a TypeScript function's return type

- **Item:** issue #713: `change_type` changed a parameter's, a
  property's or an alias's type, not a function's return type, so
  changing a representation behind a function ended in
  replace_declaration of the whole function.
- **Date:** 2026-11-20
- **Branch:** ts-change-return-type

## Plan

The issue suggested a `return_type` argument, as Java's. Without one:
`name` a function and no `parameter` names nothing else today (it was
refused, "no type alias"), so that call changes the return type.
- **The easy change:** `ChangeReturnType` in retype.go, beside
  `ChangeParameterType`: annotate (replace, or add after the `)`), and
  the same for the method's family.
- **The places today:** `ChangeType` (a name that is a function), and
  `ChangeFieldType`, where `Class.method` is routed and was refused as
  "not a property".
- **The refactorings:** none; the new function sits beside the old.

## Notes

- plan: the test's first return type, `string | number`, broke a `+`
  in the hoses fixture; `NonNullable<string>` changes the annotation
  without changing what it means.
- refusal (mine, a Bash command): `git checkout -b` shared a command
  with a grep of Go files the read guard refused, so the branch was
  not made; made alone, as the session notes say.

## Measured

One batch of 3 edits (add_declaration of two functions, two
replace_statements), the test by add_declaration, three replace_text
in strings (the test's type, the description); nothing by hand.

## Assessment

Through the tools. The published description of change_type gained
one line (a new use, with its example).

## Follow-ups

- Go's change_type with a function's name could do the same, for
  parity; Go has add_result for a new result, not a changed one.
