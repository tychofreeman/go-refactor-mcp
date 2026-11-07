# batch retry

- **Item:** `docs/parity.md` section 5, item 7: java-refactor-mcp's
  `batch` `retry` (#247) for Go and TypeScript. Asked for by the user
  (2026-11-06: "go ahead and do the batch retry").
- **Date:** 2026-11-06
- **Branch:** batch-retry

## Plan

At the highest level: the server remembers the last batch that was
refused with nothing changed, and a batch call with `retry: true` is
that batch with only the changed steps sent: `{"step": N, ...}` replaces
step N (its tool kept when left out), `{"after": N, ...}` adds a step
after it (0 for first), numbered as in the refused batch; `check` and
`report` kept unless given. The merged batch runs whole, as any batch.
Every batch refused with nothing changed ends by saying how to retry; a
batch that applies (or keeps steps through a checkpoint) forgets it.

The memory belongs above the languages, as the undo stack does: a retry
may name no file, so the language is chosen from the merged batch. The
server needs to know whether a batch was refused with nothing changed,
which `Language.Batch` does not say.

- Green, first: `add_result` of a bool `refused` to `Language.Batch`
  (both implementations, the interface), as `Step` already reports.
- Red: `refactor.Retry` tests (merge, refusals: no kept batch, both or
  neither of step and after, a number out of range) and a server test
  (a refused batch, then a retry that applies).
- Green: `refactor.Retry` (`Args`, `Keep`) in a new file by create_file
  (new behavior); the server's `batch` calls it; the batch schema gains
  `retry` and the step fields, `tool` no longer required; the
  description a sentence.
- The CLI: the same `Retry`, its kept batch in a file beside the undo
  stack's.

## Notes

- refusal: create_file of retry_test.go: no stub for `Retry`, used as `var r Retry` (a type is stubbed only when made with a literal); declared the type with stub methods in retry.go first.
- bulk: Retry.Args and Keep, 70 lines by add_declaration over the stubs: new behavior.
- refusal: the wiring batch of ten steps: add_field takes `field`, not `fields` (twice); resent whole, which this item is for.
- plan: add_result first put the bool after the error, and no tool moves a result; fixed add_result in its own PR first (#592, `docs/sessions/add-result-before-error.md`), this branch stacked on it.
- plan: the batch definition was at its budget (2,500 characters): the description says nothing of retry; the refusal's last line carries the rules, and the schema names retry, step and after without descriptions. Three phrases were shortened to fit, one of them made language-neutral ("run_tests runs the tests too").
- hand: the CLI's runBatch body rewritten by replace_statements (47 lines), most of it the old body: the workspace now loads before the batch is parsed, and its last lines keep or forget the batch. move_statement would have refused moving the load above the parse (both print and return); writing the body out was easier than several moves and inserts.

## Measured

No session log (a cloud session started outside the checkout). Through
the CLI: add_result (with a replace_declaration naming the fake's
parameter), create_file of the test and of retry.go's stubs,
add_declaration of the behavior, one batch of ten structured steps for
the wiring (replace_expression, add_field, insert_statements,
replace_statements), replace_text on the definition's literals, and the
CLI's body.

## Assessment

Through the tools but for the CLI's body, written out (above). The
signature change was a refactoring, after a detour that fixed add_result
for every later use. Two refusals each cost a resend of a whole batch,
the waste this item removes. The plan held, with the add_result PR
added before it and the description left to the refusal.

## Follow-ups

- Mark `docs/parity.md` section 5 item 7 done once #591 is merged (this
  branch is cut from before it).
- An A/B run would show whether a fresh model finds retry from the
  refusal alone, as Java's did (#250).
