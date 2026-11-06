# CLI stdin fixes

- **Item:** three gaps from the friction log of 2026-11-05 (the entries
  on `key_file=-`, test_for and skeleton-only), chosen by the user with
  "what's next?" ("Do all 4", this first).
- **Date:** 2026-11-05
- **Branch:** cli-stdin-fixes

## Plan

At the highest level: what the model sends should mean what it meant.
Three places where it did not:

1. A heredoc ends its text with a newline. Through `new_file=-`, that
   newline split lines that `old` did not end with (three times on
   2026-11-05). The CLI drops one final newline from text it reads from
   stdin, as the shell's `$(...)` does, except for `content`, whose
   final newline is the file's or the declaration's own.
2. An argument the schema types as a string, given as a number or a
   boolean (`expression=1500` through key=value, or a model's JSON), is
   refused as "cannot unmarshal number". The engine converts a scalar to
   the schema's type before decoding, where `aliasArgs` already renames
   arguments: a number or boolean to a string for a string property, a
   string of digits to a number for an integer, "true"/"false" for a
   boolean.
3. An edit refused for its type errors runs again as a batch that adds
   imports; when that retry was refused for another reason (its
   arguments), the result showed only the first refusal. The step
   reports the retry's refusal when it is not about the errors.

Each test first: the CLI's stdin test with a replace_text whose old ends
mid-line; refactor's argument test with a number for a string; the
TypeScript language's Step with an edit whose retry is refused.

## Notes

- read: `aliasArgs` and `fromStrings`, to find where arguments are converted before decoding: string to integer and boolean was there already, so the reverse joined it (and the function was renamed `toSchemaTypes`).
- plan: the third fix (show the import retry's own refusal) is left out. Its cause on 2026-11-05, a `content` argument marked required, is fixed in #568, and on main nothing makes a retry refuse for another reason, so there is no test to write first; it stays a follow-up.

## Measured

Not measured: a cloud session with no session log (it started outside the
checkout). Eleven CLI calls: heredocs for the two tests, the two inserted
statements and the docs; one rename.

## Assessment

- **Through the tools:** every change was an edit or a refactoring
  (add_declaration for the tests, insert_statements for the two fixes,
  rename and replace_text on comments); nothing by hand, no Go read with
  grep or sed.
- **Plan:** two of three fixes held; the third has no trigger on main.
- **One change that would have helped:** none needed; the conversion
  had a place to go (`aliasArgs`), found by reading one function.

## Follow-ups

- When an edit refused for its type errors runs again as a batch and
  that batch is refused for another reason, report that refusal (TS
  `Language.Step`; Go's `RunStep` retries too). Write it when something
  makes it happen on main.
