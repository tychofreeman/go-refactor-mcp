# convert_params_to_object for Go (Go/TypeScript parity, first part)

- **Item:** Go and TypeScript as equal in their tools as makes sense
  (the user, 2026-11-04, after roadmap 51), chosen from "what's next?".
  Of the published tools, TypeScript alone has convert_params_to_object;
  Go alone has split and (in #546) inline_struct. This PR is the first
  part; TypeScript's split and inline counterpart follow, stacked on
  their Go PRs.
- **Date:** 2026-11-04
- **Branch:** go-params-to-object (from main)

## Plan

At the level of the operation: Fowler's Introduce Parameter Object, a
function's parameters become one struct the calls build. In Go it is
derivations 5, delegate, redirect, inline, contract: declare the struct
and a wrapper that takes it and calls the function with its fields;
fold the wrapper, so every call builds the struct; inline the function
into the wrapper; rename the wrapper to the function's name. So the
tool is a composite of four existing steps (an Expander), with the same
name and arguments as TypeScript's (`file`, `name`, `type_name`), and
no new proof.

Up front, what the steps would leave half done: a function used as a
value (fold leaves it, inline keeps the function, the rename clashes),
recursion (inline refuses), a variadic or blank parameter, a call that
passes a multi-value call. Fields are the parameters' names, exported
when a call is in another package (else fold leaves those calls).

Derivation, in TDD's order: no reshaping (a new package of changesig's
kind); goldens (red); the Expander, registered.

## Notes

- plan: the derivation run by hand first (CLI, scratch module): fold left the call in another package while the wrapper was unexported, so the temporary is exported with the function; inlining deletes the function's doc comment, so the wrapper carries it.
- refusal: golden `method`, step 3 (inline_function) refused "no package-level water": the step took the bare name; changed to the qualified name with replace_expression.
- refusal: TestExamplesAvoidEvalTasks, `Args` (the default name's suffix, in the description) is a name in an eval task; the sentence now says "named after the function".
- refusal: TestToolBudget, instructions 12 characters over 6,000 with the tool listed; cut the batch line ("then check, and undo"), not a line the open PRs #543-#546 trimmed, so they do not conflict.
- hand: rebasing this branch from main onto the top of the stack
  (#548), so that its docs can use roadmap item 51: the instructions'
  tool list and the registry's last elements conflicted, both extended
  by each side; kept the stack's lines and added this one's, by a
  script.
- refusal: on the stack the instructions were 6 characters over again;
  cut "a whole declaration" to "a whole one" in replace_declaration's
  entry. The cap is now what each new tool's listing meets first.
- refusal: the stress run on jsonschema-go rolled back ForType and
  Schema.Resolve: fold built the struct in an external test package
  (`package jsonschema_test`) with unexported fields, since checkCalls
  had left `_test` packages out of "another package". Golden
  `exported_external_test` first (red), then replace_expression dropped
  the exclusion; both now apply and the module's tests pass.
- refusal: replace_expression's argument is `expression`, not `old`;
  sent `old`, refused, renamed it in the batch file.

## Measured

No `.claude/session-log/` here: this session ran in a cloud clone, where
the hook's log is not written, and every step went through the `refactor`
CLI with batch files in the scratchpad (counted as Bash). Those files
carried: the goldens (create_file), the expander and its test
(add_declaration), the fixes (replace_expression, replace_statements of
the method case), the docs (replace_text).

Stress (`TestStressParamsToObject`, STRESS_TESTS=1, the modules' tests
after each change): jsonschema-go 28 of 53 applied, uritemplate (v3) 31
of 48, x/tools one rollback class. Every rollback was fold building the
struct in an external test package with unexported fields (2 in
jsonschema-go, 6 in x/tools); after the fix all 8 apply or meet an
ordinary refusal of inline_function, and jsonschema-go's tests pass
after ForType and Schema.Resolve. No test failed after an applied change.

## Assessment

- **Through the tools:** every write to Go went through gorefactor's
  CLI; the tool itself is an expander of four existing ones, so no new
  rewriting code. Hand: only the rebase conflicts (a script).
- **Large writes:** paramobject.go (new behavior: argument checks and
  the wrapper's text) and the goldens (fixtures).
- **Did the plan hold?** Yes: add_declaration, fold, inline_function,
  rename, as derivations 5. Two things the plan missed: fold needs the
  struct's fields exported when any call, an external test's included,
  is in another package; and inline_function of a method needs its
  qualified name.
- **Reading:** none of bodies outside the new file and fold's export rule.
- **One change that would have helped:** the stress run caught the
  external-test case that no golden had; a golden per kind of package a
  call can be in (same, other, internal test, external test) is the
  checklist for any expander that folds.

## Follow-ups

- inline_function's note that the deleted doc comment "went with it" is
  noise here, since the wrapper carries it; the expander could drop it.
- Roadmap 52.2 (split for TypeScript) and 52.3 (inline_struct's
  counterpart).
