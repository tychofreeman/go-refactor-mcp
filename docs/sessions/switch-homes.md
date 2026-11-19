# Switches on one value into a lookup of types

- **Item:** roadmap 72, the first of two PRs the user chose
  ("Lookup + one-step move"): prepare_switch_for_types with a lookup
  map, so the switched value keeps its type. The second PR moves one
  value's case out of every prepared switch in one step.
- **Date:** 2026-11-19
- **Branch:** switch-homes

## Plan

First, the check the roadmap asked for. On a scratch copy of
lending-xl, through the CLI, the type-per-kind form composes today:
- change_type of `Item.Kind` to `any`;
- prepare_switch_for_types on each of the five switches;
- add_declaration of `Book`;
- then for each switch, extract_function of the `"book"` case,
  add_parameter of `b Book` and function_to_method with new_name.

That is 6 steps, then 15 per kind, and the tests pass. But it ends
with `Kind` as an interface, and the lending task keeps `Kind` a
string (hidden tests write `Kind: "magazine"`). The sessions' table,
`kindRules[it.Kind]`, keeps the string. So the home a value that is
data needs is a lookup from it, not a narrowed field.

The change: prepare_switch_for_types takes `lookup`, the name of a
map from the tag's type to the interface. With it, the tag need not
be an interface. The guard becomes
`if k, ok := kinds[it.Kind]; ok { return k.LoanDays() }`, and the map
is declared empty, `var kinds = map[string]Kind{}`, or reused when it
exists (its type must match; it must be empty, or a value in it would
route before its case moved). A kind then moves with the same steps,
plus one add_element of `"book": Book{}`.

- **The easy change:** the guard's text with a map index in place of
  the type assertion, in one place.
- **The places today:**
  - `prepareArgs`: a field;
  - `checkTag`: the interface requirement;
  - `checkUnimplemented`: an implementer only routes through the map;
  - `plan`'s `head`: the guard;
  - `plan`'s declarations: the map.
- **The refactorings:** extract_variable of the guard's Sprintf in
  `plan`, then extract_function of it into a method `guard(v, ok)`,
  so that the guard is one place. The rest are new checks and a new
  declaration, each a branch on the new field.
- **Red:** a golden `lookup_new` (the map and interface declared, the
  tag a string) and `refuse_lookup_entries`.
- **Green:** an add_field of `Lookup` to `prepareArgs` and of `lookup`
  to `preparer`, then edits in the places above.

## Notes

- refusal: the batch of extract_variable then extract_function addressed line 399, which step 1 rewrote; retried with `$1.start`.
- refusal: a retry's `{"step": 8, "args": {"line": 452}}` replaced all of the step's args, not only line, so the step was refused for missing file, where and content; resent whole. A merge of the changed args would have saved the turn.
- bulk: checkLookup (about 55 lines) by add_declaration: new behavior. The goldens' fixtures by heredoc under testdata.
- refusal: fold of the extracted declStart matched nothing: the copy goes on after `start` is set (the goes-on form TypeScript's fold has, #698, which Go's lacks); replaced the copy with `start := g.declStart()` by replace_statements.
- hand: none outside testdata.
- plan: the summary's "that implements Kind" became "with an entry in kinds" for a lookup (insert_statements, replace_text of the format, replace_expression of its argument).

## Measured

`refactor-tokens -log .claude/session-log/switch-homes.jsonl`, before
the docs were finished:

- gorefactor 18 calls; by hand (Bash) 28; first request's context 89.5K;
- turns with tool calls 44; ToolSearch only 2; Bash reads refused 2;
  gorefactor refused 4;
- steps: 4 refactorings, 14 edits (4 text edits of Go);
- derived share 56% (9 of 16 changed lines derived).

The Bash calls are mostly the check the roadmap asked for: reading
lending-xl and the routine's transcripts (testdata and eval-runs), and
the CLI's batches on scratch copies (the composition, then the
lookup end to end), which the log counts as Bash.

## Assessment

- **Through the tools or around them:** every change to Go went
  through gorefactor; none by hand outside testdata. The text edits of
  Go: the guard's new body and the map's declaration
  (replace_statements, insert_statements: new behavior), the summary's
  format string (replace_text, a string literal), and the copy of
  declStart's lines replaced by its call, where fold did not match
  (roadmap 74).
- **Large writes:** checkLookup, about 55 lines by add_declaration:
  new behavior. The goldens' fixtures by heredoc under testdata.
- **Cost:** 44 turns, a third of them the check on lending; 4
  refusals, two of them the batch's line addressing (a line step 1
  rewrote; a retry's args replacing rather than merging).
- **Did the plan hold?** Yes: extract_variable and extract_function made
  the guard one place first; then add_field, the edits and one new
  check. The summary's wording was added (plan note).
- **Reading:** the bodies read were those changed (Plan, checkTag,
  checkInterface, checkUnimplemented, plan); no read to find uses.
- **One change that would have helped:** Go's fold of a copy that goes
  on (roadmap 74). And a batch retry whose `{"step": N, "args"}` merges
  the args it gives into the step's.

## Follow-ups

- Roadmap 72.2: one step to move a value's cases out of every prepared
  switch into its type, its entry added and the dead cases deleted.
- Roadmap 74: Go's fold of a copy that goes on.
- TypeScript's prepare_switch_for_types has no `lookup` yet (parity,
  after 72.2 settles the shape).
