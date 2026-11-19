# fold of a copy that continues after it

- **Item:** roadmap 67, last case: early returns where the copy does not
  end its function.
- **Date:** 2026-11-19
- **Branch:** fold-error-returns

## Plan

fn returns (T..., error), ends in `return x..., nil` with x its top-level
locals, and every other return gives an error known not to be nil (a
call of errors.New or fmt.Errorf, or the sole statement of
`if e != nil { return ..., e }`). A copy of fn's statements but the last
return, whose early returns match fn's in their error and return the
same other values at each, becomes

    x..., err := f(args)
    if err != nil {
        return <its other values>, err
    }

where the function around it takes `error` last and those values use
none of the copy's locals. Derivation: add_parameter `stmts []ast.Stmt`
(value f.stmts) to folder.foldStmts and replace its reads of f.stmts,
while green; red golden `statements_error_return`; then the edits: a
Returns hook on pattern.Attempt that the ReturnStmt case calls;
folder.pattern noting the error form; find calling foldStmts again with
the prefix; foldStmts' checks and the text of the call for that form.

## Notes

- plan: the red golden was written before the preparing refactoring; set aside (moved to the scratchpad) while refactoring, then restored.
- refusal: add_parameter of `stmts` with value f.stmts (reads through a pointer); extract_variable as parameter instead.
- refusal: extract_variable as parameter: the call passed append(stack, n), which has side effects; extract_variable named it `inner` first. all_occurrences replaced only the first read of f.stmts; the two in the loop went by replace_expression in the change's batch.
- bulk: add_declaration of errorForm, nonNil, errReturn, continues and checked (156 lines): new behavior.
- refusal: replace_statements of continues' signature loop with a call of the extracted `around` gave its arguments in the wrong order; retried.
- hand: continues' copy of the loop became `_, sig := around(stack, p)` by replace_statements: fold of around did not fit, since the copy declares only sig.

## Measured

`refactor-tokens -log .claude/session-log/fold-error-returns.jsonl`,
before this record: 26 tool calls; 4 refactoring steps and 16 edit
steps through gorefactor (3 text edits of Go); 3 refusals, none
followed by a hand edit; 4 writes by hand, all under testdata (the
goldens, and moving the red one aside and back). Large writes: the
156-line add_declaration (new) and two fixture heredocs (54 and 70
lines, new). 21 requests, 3.68M input tokens, 28.6K output; 2 turns of
ToolSearch alone, 3 refused.

## Assessment

- **Through the tools or around them:** through them. The preparing
  steps were refactorings (extract_variable twice, add_parameter); the
  change was one batch of 15 steps (hooks placed by insert_statements,
  reads redirected by replace_expression, the new functions by
  add_declaration); the clean-up was extract_function plus one
  replace_statements.
- **Large writes:** the 156 lines are the feature itself.
- **Cost:** 3 refusals; each named its fix.
- **Did the plan hold?** Yes; add_parameter became extract_variable as
  parameter, and usedAfter needed a `keep` set the plan missed.
- **Reading:** foldStmts, find, call, resultsTaken and pattern.go's
  outline, to place the hooks.
- **One change that would have helped:** add_parameter suggesting
  extract_variable as parameter when its value reads through a pointer
  (it says only what to avoid).

## Follow-ups

- TypeScript's fold: all three shapes of roadmap 67.
- nonNil knows errors.New, fmt.Errorf and `if e != nil`; a module
  function that never returns nil is not known.
