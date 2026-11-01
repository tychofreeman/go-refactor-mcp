# Fields and receivers by edit

- **Item:** roadmap 49.13: no tool deletes or moves a struct field, and
  none names an unnamed receiver. `-no-code-by-text` (49.11) now refuses
  the replace_text that did both in `refuse-code-by-text`.
- **Date:** 2026-11-01
- **Branch:** fields-by-edit

## Plan

At the level of the tools: a struct's field is a declaration addressed
by name (`Type.Field`), as a method is; replace_declaration already
takes such names but finds only top-level declarations. So the change
is to widen what replace_declaration's name reaches, not a new tool:

- `Type.Field` with empty content deletes the field, its doc and line
  comment; with content, replaces it (content declares the same field).
  A field declared with others (`a, b int`) is refused for now.
- Moving a field is then a composition: delete it, then add_field with
  `after` (into `docs/derivations.md`, not a tool).
- A receiver: rename with `function` (`Type.Method`) and `name` `_`
  names a blank or unnamed receiver; second, in the same PR if small.

Derivation, in TDD's order:
1. Green: make the change easy. In `Plan`, the found declaration is
   `declOf(file, pos)`; a field is a different node, so the lookup and
   the two paths (deletion, replacement) branch on it. Extract the
   top-level path from `Plan` first if the branch would be large.
2. Red: goldens `delete_field` and `replace_field` (and a refusal for
   `a, b int`).
3. Green: the least code: a `fieldOf` lookup and a deletion of its
   lines, a replacement of its span.
4. Refactor: share the line-extent logic with `deletion`.

## Notes

- plan: step 1 needed no refactoring: the field branch is one `if`
  before `declOf`, so the change was already small and local.
- bulk: `fieldOf` and `fieldPlan`, 57 lines, new behavior written as
  the least code; its line extent copies `deletion`'s, folded next.
- refusal: fold of the extracted `wholeLines` (statements with results);
  its refusal named the route, extract each copy and merge_duplicate_function.
- refusal: the copy in `fieldPlan` reads `field.End()` after `from`, the
  original's `end` before it; move_statement_up and introduce_parameter
  both refused, since both calls could panic and would swap order.
  Undo, then the next.
- hand: replace_statements put `wholeLines(tf, start, field.End(), src)`
  in place of the copy: a text edit of Go. It reorders only which of two
  calls on a non-nil field would panic first, which the analysis cannot
  know; the composition (extract, introduce_parameter, remove_parameter,
  move_parameter, merge) failed at introduce_parameter for that reason.

- bulk: `nameReceiver`, 34 lines, new behavior, the least code.
- plan: gap L in `docs/derivations.md` says remove_dead_code already
  deletes an unused `Type.Field`; `refuse-code-by-text`'s stub field
  could have moved by remove_dead_code + add_field. Kept the deletion
  in replace_declaration anyway: its description already claimed
  `Type.Field` and refused it, and as an edit it deletes where
  remove_dead_code's checks of printing would refuse.

## Measured

`refactor-tokens -log .claude/session-log/fields-by-edit.jsonl`, before
the docs and this record:

- tool calls: 57
- through gorefactor: 3 refactoring steps, 22 edit steps (5 of them text edits of Go)
- refused by gorefactor: 5
- by hand: 0; refused, then done by hand: 0
- large writes: add_declaration replacedecl.go 56 lines, rename.go 34 lines
- denied or failed: 4 (3 read-guard refusals of greps naming a
  `_test.go` file, 1 a `sed` the guard took for a Go read)
- reads: inspect targets 4, part of a file 4, file 1, names 1; Bash 9
  (ls and cat of testdata, greps of directories)
- derived share: 19% of Go lines changed
- requests 52, input tokens 6.69M, output 26.8K; ToolSearch-only turns
  4, gorefactor refused 5, Bash reads refused 3

## Assessment

- **Through the tools or around them:** every write went through
  gorefactor. Of the 5 text edits of Go, 4 were replace_text of string
  literals (the three descriptions and a `desc` tag), which
  `-no-code-by-text` allows and `refactor-tokens` still counts as text
  edits; one was a replace_statements over code, the `wholeLines` call
  (a `hand:` note above): the composition failed on evaluation order.
- **Large writes:** both new behavior (`fieldOf`/`fieldPlan`,
  `nameReceiver`), written as the least code, not existing code typed
  again. `fieldPlan`'s copy of the line extent was folded by
  extract_function plus that one replace_statements.
- **Cost:** 52 requests; 5 refusals: fold of a function with results
  (its refusal named the route), a batch with a guessed `function`
  argument for fold (schema loaded but misread), a `$id.end` reference
  off by the extracted variable's placement, move_statement_up and
  introduce_parameter on order of evaluation. 4 schema-only turns.
- **Did the plan hold?** Step 1 needed no refactoring. Fields and the
  receiver as planned; the refactor step (share the line extent) took
  three tries and an undo. The plan missed that remove_dead_code
  already deleted an unused field.
- **Reading:** bodies read: replace_declaration's `Plan` and
  `deletion` (to write the field path), locate's `local` and rename's
  `Plan` and capture check (to write and word `nameReceiver`). None to
  find uses. Three greps refused by the read guard for naming a
  `_test.go` file.
- **One change that would have helped:** `refactor-tokens` counting
  replace_text inside string literals and comments apart from text
  edits of code, so the record's count says what `-no-code-by-text`
  checks (roadmap 49.14).

## Follow-ups

- Roadmap 49.14: text edits of Go split into literals/comments and code.
- A field declared with others (`Width, Depth int`) is refused; splitting
  it would be its own step.
