# One step to move a value's cases into its type

- **Item:** roadmap 72.2, the second of the two PRs the user chose.
- **Date:** 2026-11-19
- **Branch:** switch-move-case

## Plan

Today, once the switches on a value are prepared with a lookup (72.1),
moving one value into its type takes three refactorings per switch
(extract_function of the case, add_parameter of the type's value,
function_to_method), then add_element of the map's entry. The dead
cases then stay, since no tool can prove the map now routes the value
elsewhere: about 16 steps for lending's five switches.

The change: prepare_switch_for_types with `move` (the value as written,
`"book"`) and `type` (a new type, `Book`), with `interface` and
`lookup` naming what the switches were prepared with. In one plan:
- every function of the package whose guard indexes the lookup and
  calls a method of the interface gives its case for the value, as
  `func (Book) M(params) results { body }`. The parameters are the
  guard call's arguments, with the interface method's types;
- with no case, the default's body, or the code after a switch whose
  cases return, which ends the function;
- the case is deleted;
- `type Book struct{}` and the methods go after the map, and the entry
  `"book": Book{}` goes into it.

Behavior is kept, because the lookup now runs for "book" exactly the
code its cases ran. The refusals:
- an interface method with no prepared switch, or with two;
- a case of several values (split it first);
- a body that leaves other than by return, or uses a variable that is
  not a parameter;
- a type name that is taken;
- the value already in the map.

- **The easy change:** `if a.Move != "" { return planMove(ws, a) }` in
  Plan, and the new code beside it.
- **The places today:** `prepareArgs` (two fields; `method` becomes
  optional), and `Plan`'s dispatch. The rest is new: finding the
  prepared switches, the methods' text, the edits. It reuses
  switcher's `body`, `checkLeaves`, `freeVars` and `checkWrites`.
- **The refactorings:** none before the test: one place.
- **Red:** goldens `move_lookup` (two switches, one with a default),
  `move_no_case` (a value that falls through to the code after the
  switch) and `refuse_move_unprepared`.
- **Green:** add_field, then add_declaration of the new functions
  (in a new file `move.go` by create_file with a declaration, then
  add_declaration).

## Notes

- refusal: replace_text of `prepareArgs`' Method tag was refused as code; replace_declaration of `prepareArgs.Method` and add_field of Move and Type did it.
- plan: insert_statements of the move's dispatch went in at line 77 from a stale read, inside the identifier loop; replace_statements of the loop put it before (a misplaced step of mine, not the tool's).
- bulk: move.go, about 400 lines by replace_declaration and add_declaration: new behavior (finding prepared switches, the methods' text, the edits); it reuses switcher's body, returns, checkLeaves, freeVars and checkWrites.
- refusal: replace_statements of the methods' loop body refused `mv`, not yet in scope; the loop then went over m.moves by replace_expression of its range and replace_statements of its head, with a new `mover.method`.
- refusal: a batch's check found `it` unused after that; a retry added a step deleting it.
- refusal: fold of withResults (extracted from switcher.signature) matched nothing: its returns are in a switch's cases (roadmap 74); mover.signature's copy became the call by replace_statements, then inline of `s`.
- read: switchmethod.go's find, checkBodies, freeVars, returns, checkLeaves, checkWrites, signature and body: what the move could reuse.
- hand: none outside testdata.

## Measured

The branch was not created when planned: `git checkout -b` shared a
command with a grep the read guard refused, so the whole command did
not run, and the calls were logged to `main.jsonl` until the branch
was made just before committing. Measured from that log's lines after
`gh pr merge 707`, with `refactor-tokens -log`:

- turns with tool calls 28; gorefactor refused 4; one Bash refused
  (that command);
- inspect 10 calls; batch 8; add_declaration 1 (5.3K input);
- steps: 2 refactorings, 15 edits (7 text edits of Go);
- derived share 2% (6 of 327 changed lines; 459 written).

## Assessment

- **Through the tools or around them:** every change to Go went
  through gorefactor, but it is almost all written: the item is a new
  plan of about 400 lines, which no refactoring derives. The text edits
  of Go are:
  - the dispatch (insert_statements, then replace_statements to move
    it out of the loop it went into by my stale line);
  - the methods' loop (replace_expression, replace_statements);
  - two deletions;
  - the copy of withResults replaced by its call, where fold did not
    match (roadmap 74).
- **Large writes:** move.go by replace_declaration and add_declaration
  is new behavior. It reuses switcher's checks rather than writing them
  again. The goldens are heredocs under testdata.
- **Cost:** 28 turns; 4 refusals, one of them the tool's
  (-no-code-by-text, for a struct tag), and three of mine (scope, an
  unused variable, a stale line).
- **Did the plan hold?** Yes: the dispatch, fields, and new code beside
  them. The order of the methods came from the interface (sorted by
  name), so the loop was changed to follow the source.
- **Reading:** switchmethod.go's helpers, to reuse them; no reads to
  find uses.
- **One change that would have helped:** fold of returns inside a
  switch (roadmap 74). And for the session: a branch made in a command
  of its own, so that a guard's refusal of another part cannot skip it.

## Follow-ups

- The emptied switches stay (`switch it.Kind {}` before the default
  code): deleting one when its last case moves, where the tag has no
  effects, would finish the shape.
- TypeScript's prepare_switch_for_types has neither `lookup` nor `move`.
- An A/B rerun of lending-magazine with the routine, to see whether a
  fresh model finds `lookup` and `move` from the description.
