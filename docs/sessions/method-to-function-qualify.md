# method_to_function: calls from other packages

- **Item:** roadmap 57, found by a plan-and-execute loop attempt on
  freight-xxl (cost-history 28); chosen from "what's next?" with items
  56, the fixtures gap and the loop's next view, all four in order.
- **Date:** 2026-11-16
- **Branch:** method-to-function-qualify

## Plan

The change: method_to_function makes a method a function of the
method's package, so a call in another package must name it through
that package (`freight.Dispatch(c, ...)`), with an import when the
calling file has none (it may hold the receiver without naming its
package: `b.Make().Dispatch(2)`). Today every call is written
`Dispatch(c, ...)`, which the post-check refuses as a bug.

Derivation: the call edits are made in `target.funcCallEdits(c call,
name string, csrc []byte)`, called once per call from `Plan`.
`imports.Adder.Name(pkg)` gives the qualifier a file uses for a
package, adding an import if it has none, and `Adder.Edits()` writes
the imports. So:

1. Test first: a golden `cross_package` with a call in a package that
   imports the method's package, and one in a package that does not.
2. In `Plan`, an Adder per calling file outside the method's package;
   pass the qualifier into funcCallEdits by giving it `name` already
   qualified (`a.Dispatch`), so its body does not change; add each
   Adder's edits to the file's edits.
3. checkNameAtCall checks the name at the call; with a qualifier, what
   must not be hidden there is the qualifier. Look at its body only to
   decide this.
4. A `new_name` that is not exported while a call is in another package
   is refused (a second golden).

## Notes

- hand: the goldens' fixture files (go.mod and three 7-9 line Go files,
  copied for the refusal case) with printf: create_file refuses function
  bodies under -skeleton-only, and no edit reaches a fixture under
  testdata/ (the gap this session's third item is about).
- read: Plan and checkNameAtCall, to write the change into them.

- bulk: replace_statements of Plan's call loop, 29 lines, of which about
  20 are the loop as it was, typed out again around the 9 new ones
  (the adders map, the if/else on the call's package, the imports after
  the loop). Four smaller edits would have carried it: insert_statements
  of `adders` before the loop and of the imports loop after it,
  replace_statements of the checkNameAtCall block alone, and
  replace_expression of `name` with `callName` at funcCallEdits. Writing
  the loop out was easier than working out those four lines.
- bulk: add_declaration of qualifiedName, 30 lines of new behavior.
- plan: step 3 changed: checkNameAtCall stays as it was for calls in the
  method's package; the qualifier's own check (a local must not hide
  the package's name) is in qualifiedName, since a package name is not
  a package-level object and the old check would refuse it.

## Measured

`go run ./cmd/refactor-tokens -log .claude/session-log/method-to-function-qualify.jsonl`:

- tool calls: 24; through gorefactor 2 edit steps (1 a text edit of
  Go: the replace_statements above) and 1 CLI call; 0 refusals.
- by hand: 1 (the fixtures, with printf); refused then by hand: 0.
- large writes: add_declaration 30 lines, replace_statements 29 lines.
- denied or failed: 3 (a grep of a test file, which the read guard
  refused with the fixture commands in the same call; the measuring
  command itself).
- requests: 20; input tokens 7.70M (cached or not), output 13.2K; the
  first request's context was 370K, this long session's, so the input
  says more about the session than about the item.
- derived share: none; 59 lines written, all through edits.

## Assessment

- **Through the tools or around them:** through them for the Go (two
  edits, no hand edits of non-fixture Go); no refactoring fit, since
  the change is new behavior in one function. The fixtures were by
  hand, the known gap. The one text edit of existing Go is the loop
  rewritten whole: around the tools in the small sense the record
  exists for, since four targeted edits would have changed only the
  lines that changed.
- **Large writes:** qualifiedName is new behavior; the loop is 9 new
  lines and 20 old ones typed again, because writing it out was easier.
- **Cost:** 20 requests, none lost to a refusal or a schema load; one
  turn lost to the read guard (grep of a _test.go in a shell chain).
- **Did the plan hold?** Yes, steps 1, 2 and 4 as written; step 3 moved
  into the new function (see plan: above).
- **Reading:** outlines of four files, then two bodies (Plan,
  checkNameAtCall) to write into; nothing read to find uses.
- **One change that would have helped:** writing a golden's fixture
  through the tools: this session's third item (friction log for #653
  and this entry).

## Follow-ups

- move_method and function_to_method may have the same gap for calls
  from other packages; not checked here.
