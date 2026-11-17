# replace_expression everywhere, with holes

- **Item:** the typed counterpart of `sed -i` over the files: cost-history
  30's Claude Code sessions on freight-xxl made every wide change by hand
  with one `sed -i` or `perl -pi` ($0.09-0.12 a run), while with the
  tools routes took 628 edit steps, one use at a time, and 53 turns lost
  to shell edits the arm may not run ($0.38). Chosen from "what's next?"
  on 2026-11-17.
- **Date:** 2026-11-17
- **Branch:** replace-expression-everywhere

## Plan

The change, at its level: "rewrite every use of this, the same way,
across the module" as one typed step. replace_expression has it for one
file and one spelling (`all_occurrences`: the same tokens, names that
mean the same, locals of the same type). Two things keep it from doing
what sed did on routes: it stays in one file, and `s.Road`, `a.Road` and
`xs[i].Road` are different tokens.

As an option of the existing edit (the tool budget), not a new tool:

1. Test first: goldens in `internal/edits/replaceexpr/testdata`:
   - `everywhere_holes`: `$s.Road` to `$s.Routes["road"]` with
     `everywhere`, from a line in package a, rewrites the reads of
     `S.Road` in packages a and b (`s.Road`, `x.Road == ""`,
     `xs[0].Road`) and leaves `T.Road` of another type alone;
   - `refuse_hole_twice`: a hole that `new` uses twice (its expression
     would be evaluated twice) is refused.
2. Holes: `$name` in `expression` and `new` stand for any expression;
   the occurrence at `line` binds each hole's type and each other name's
   object, and an occurrence elsewhere matches when its shape is the
   same, its names refer to the same objects (locals: the same type, as
   `all_occurrences` has it), and each hole's expression has the bound
   type. The replacement is `new` with each hole's source.
3. `everywhere`: the occurrences in every file of the module's
   packages, tests included, each with the imports `new` needs.
4. Each hole appears in `new` exactly once (so its expression is
   evaluated as often as before), in the same order as in `expression`.
5. Description: one sentence and the example; checks.md.
6. An A/B on freight-xxl's routes (the user's yes first) to see whether
   a session uses it in place of per-use edits.

Bodies to read before writing: `Plan`, `sameMeaning`, `sameNames`.

## Notes

(Written late, at the user's question "Are you using the tools well?",
not as each happened.)

- read: Plan, sameMeaning and sameNames, to write the new path beside
  them.
- refusal (of a kind): extract_function of Plan's per-file part (lines
  85-103) applied, but with 9 parameters and 6 results (`err` twice,
  `args` by pointer), since `a` and `err` cross the cut. Undone; a
  preparing step (naming `strings.TrimSpace(a.New)` first, so the loop
  no longer reads `a`) was not tried.
- hand: the goldens' fixtures through `refactor batch` create_file
  (#663 in the CLI built from main; this session's server predates it):
  through the tools, unlike #653 and #661.
- refusal: insert_statements of the dispatch before holeRE was declared;
  this server's retry has no move, so the step was removed and added
  again after the declaration.
- bulk: add_declaration of the pattern path, 180 lines in one call.
  New behavior, except about 25 lines that are existing code typed
  again: the per-target loop of Plan (parentheses, composites, the edit)
  and sameNames' object comparison, now in sameAs. Writing them was
  easier than shaping the extraction (point 2 above); TDD's least-code
  step allows the copy, and the refactoring that removes it follows.
- hand: the duplicated object comparison (a switch whose cases
  `return false`, in sameNames and sameAs) became one predicate,
  sameObject, by add_declaration and two replace_statements. Tried
  first: extract_function of the switch (its early exits would become a
  `done, v` pair, not a predicate) and fold (it matches a body that is
  one return, or statements without results). Gap: extract a block whose
  exits all return false as a bool predicate called `if !p() { return
  false }`.
- plan: the per-target fitting (parentheses, composites) is now one
  function, `fitted`, by extract_function from Plan's copy, which had
  the names it needs (`file`, `t`); the first extraction, from
  planPattern's copy, took `opened` and a `*match` and was undone. The
  copy in planPattern became a call by replace_statements (5 lines to
  1): fold does not match a function with statements and a result.

## Measured

`go run ./cmd/refactor-tokens -log .claude/session-log/replace-expression-everywhere.jsonl`:
38 tool calls; 3 refactoring steps and 10 edit steps through gorefactor
(6 text edits of Go), 2 CLI batches (the fixtures and expected output);
2 refusals; 0 by hand; "large writes: 0" and "derived share 98%".

Both of the last two are wrong for this item. The 180-line
add_declaration went inside a batch, which the log does not count as a
large write, and the 62 lines of the first extract_function, which was
undone, count as derived. In fact: about 205 lines of Go written through
edits (the pattern path, sameObject, the dispatch, the field), of which
about 25 were existing code typed again and later replaced by
sameObject and fitted; 1 extract_function kept (fitted, 5 lines).
Requests 30; input 18.0M tokens (cached or not), the long session's
context.

## Assessment

- **Through the tools or around them:** no Go outside gorefactor, and
  the fixtures went through create_file, which they could not before
  #663. But the main write was one 180-line add_declaration that
  included about 25 lines of existing code typed again, because shaping
  the first extract_function (a preparing step that names
  `strings.TrimSpace(a.New)`) was not tried. The copies were then
  removed: one by extract_function (after a second preparing attempt
  was undone for its parameter types), one by a predicate written by
  hand, where no operation fits.
- **Large writes:** the pattern path is new behavior but for the two
  copies above; sameObject is the copy, made one.
- **Did the plan hold?** Steps 1-5 as written; step 6 (the A/B) waits
  for the user.
- **Reading:** three bodies to write beside; nothing to find uses.
- **One change that would have helped:** an extraction of a block whose
  exits all return false as a bool predicate (extract_function could
  offer it when every exit returns the same constant), and fold of a
  function with statements and one result. Both in the friction log;
  and refactor-tokens should count an add_declaration inside a batch
  as a large write, and leave undone steps out of the derived share.

## Follow-ups

- The A/B on freight-xxl (sessions with the tools) to see whether a
  session uses `everywhere` with holes in place of per-use edits.
- refactor-tokens: large writes inside batches; undone steps.
