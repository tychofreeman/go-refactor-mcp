# A technical writer's review of the tool descriptions

- **Item:** the user asked for a review of gorefactor's and jrefactor's
  descriptions "for unnecessary words and phrases, repetition, ambiguity,
  and other issues" (2026-11-07), then "go ahead" with the fixes. This
  repo's half; jrefactor's is its own PR.
- **Date:** 2026-11-07
- **Branch:** descriptions-review

## Plan

Read the published text (`tools/list` against the server, Go and
TypeScript), then fix, all on description literals and schema tags:

- stale tool names (inline_function, change_field_type, move_declaration;
  refusals too) and leftovers of #595;
- Go text in TypeScript's (inspect's examples, undo, check);
- repetition: "Applies the change and returns what changed; undo reverts
  it." on every tool, and the name-addressing sentence on 16 Go tools
  (both said in the instructions); instruction lines that repeat inspect's
  description; two lines offering move;
- one path anchor in Go (the workspace root);
- extract_supertype's `fields` published as `members` (`fields` an
  alias), as Java names them; batch's `retry` and `report` described;
  remove_dead_code's dead `exported` no longer published;
- examples that dropped their verb ("x.water(x.depth) x.water()") made
  "X becomes Y"; TypeScript extract_function's garbled "Returns:".

## Notes

- refusal: rename of extract_supertype's `Fields` to `Members` was refused
  (the args struct reaches `DecodeArgs`, where reflection may find fields
  by name), though the field has an explicit json tag, which makes the Go
  name irrelevant to encoding/json: a false refusal, on the roadmap. The
  field stays `Fields` with tag `members`; the refused batch was retried
  with only step 2 replaced (batch retry's first use here).
- hand: the file list of one replace_text step (11 files) was built with
  a line of Python, against the rule for building JSON.
- plan: TestDescriptionsAreCurrent pinned the removed sentences on each
  tool; it now checks the instructions say them once.
- plan: shared tools (move, move_statement) keep "relative to the root",
  right for both languages; Go's own say "workspace root", TypeScript's
  "root".

## Measured

No session log (a cloud session started outside the checkout); no Go
behavior changed. Published JSON: Go tools 89,401 to 83,908 characters,
TypeScript 79,233 to 76,602; Go instructions 5,995 to 5,748.

## Assessment

All through replace_text on literals and tags, replace_expression,
remove_dead_code and replace_statements on one test. Left as it was, with
reasons: `name` for the new thing in the extract tools (a `new_name`
alias would touch five tools and the A/B reference batches), and the
schema's repeated `column` text.

A/B, the same eight tasks against cost-history 25's runs: 16/16 passed
both times, cost -8%, refusals 14 of 140 calls against 17 of 184
(cost-history 26).

## Follow-ups

- rename of a struct field with an explicit json tag should not refuse
  for encoding/json.
- `name` against `new_name` across the extract tools.
