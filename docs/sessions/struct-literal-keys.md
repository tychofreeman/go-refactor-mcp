# A struct literal's key, everywhere

- **Item:** roadmap 62, chosen from "what's next?". On freight-xxl's
  routes (cost-history 32), replace_expression's `everywhere` with holes
  rewrote every read of `Road` and `Rail` in one step each. The struct
  literals (`Shipment{Road: "x"}`) are not expressions it addresses, and
  sessions spent 16 refused shell turns, then replace_declaration, on
  them.
- **Date:** 2026-11-18
- **Branch:** struct-literal-keys

## Plan

The change, at its level: a field's element in a literal is one more
place where the field is used, so the same "rewrite every use of this"
step should reach it. A pattern that is a key-value element, `Road: $v`,
matches the elements of literals whose key is the same field (the key
resolves to the field object, so `T{Road: ...}` of another type and a map
key `Road` don't match). The replacement is another element, `Routes:
map[string]string{"road": $v}`. This is an option of replace_expression,
not a new tool: the expression and new text are parsed as an element
when they are not expressions.

Freight's literals set both fields (`{Road: "nw-0042", Rail: "Y7"}`), so
the second step (`Rail: $v` to `Routes: ...{"rail": $v}`) finds a
literal that already has `Routes`. A key twice doesn't type-check, so in
that case the replacement's value is merged into the existing one: when
both are composite literals, the new elements are added to it and the
old element is removed.

Derivation (TDD):
1. Red: golden `key_everywhere`. `Road: $v` to `Routes:
   map[string]string{"road": $v}` with `everywhere`, from a literal in
   package a. It rewrites literals of S in packages a and b (one with the
   type elided in a slice), and leaves `T{Road: ...}` alone.
2. Green: a pattern parser that falls back to an element, by parsing
   `_{text}` and taking its one KeyValueExpr. It is used for expression
   and new in planPattern. The text that checks imports is wrapped as an
   element when new is one. Everything else (shapeOf, sameAs, fitted)
   already takes any ast.Expr, and a KeyValueExpr is one.
3. Red: golden `key_merges`. `Rail: $v` into a literal that already has
   `Routes: map[string]string{"road": "x"}`.
4. Green: in planPattern, an element target whose literal has new's key
   is merged: its value's elements go after the existing value's last
   element, and the target element is deleted with the separator before
   it.
5. Refactor: extract the merge into a function.
6. Description: one sentence ("an element `Road: $v` matches literals'
   fields"), within the size budget; checks.md; roadmap 62 *Done*.

Bodies read before writing: planPattern, Plan, fitted (to see where an
element would pass through).

## Notes

- read: planPattern, Plan and fitted, to see where an element parses and
  where its text is built.
- bulk: the four golden fixtures (20-21 lines each) are new test data,
  written with create_file as testdata takes them.
- bulk: parsePattern and parseLit (24 lines) and merged (49 lines) are
  new behavior, added with add_declaration. Their callers changed with
  two replace_expression steps, one replace_statements of a single line
  (the import check's text) and one insert_statements (the call to
  merged).
- plan: step 5 (extract the merge) wasn't needed. merged was written as
  a function from the start, so the green step left no duplication to
  remove.
- check: on a copy of freight-xxl, through `cmd/refactor` built from
  this branch: add_field Routes, then `Road: $v` and `Rail: $v`
  everywhere, rewrote 17 and 30 elements in 16 and 11 files. No
  `Road:` or `Rail:` was left, and the literals that set both became
  `Routes: map[string]string{"road": ..., "rail": ...}`.
- refusal (a test's): TestExamplesAvoidEvalTasks failed on the
  description's first example (`Road`, `Routes`, `"road"` are freight's).
  It became the garden's `Height: $h` to `Sizes: map[string]int{...}`.

## Measured

`refactor-tokens -log .claude/session-log/struct-literal-keys.jsonl`:
33 tool calls, 14 edit steps through gorefactor (3 of them text edits
of Go: the description's string literal twice, and the one-line
replace_statements), 0 refactorings, 0 refused by gorefactor. One call
listed as by hand: a python3 read of an eval task's JSON prompt, which
wrote nothing. Six large writes, all listed above. Derived share 0%:
this is new behavior, not a change of what was there. One Bash grep of
a Go file in the scratch copy was denied by the read guard. The
transcript part: 30 requests, 3.52M input tokens (cached or not).

## Assessment

- **Through the tools:** every Go change went through gorefactor. The
  two replace_expression steps and the insert_statements changed only
  the lines the new behavior needed. The text edits of Go were the
  description, a string literal.
- **Large writes:** all were new behavior or new fixtures. None
  restated existing code.
- **Cost:** 30 requests. No turns were lost to refusals. One read was
  denied (the scratch copy's grep), which cost one turn.
- **Plan:** it held, except that step 5 wasn't needed.
- **Reading:** planPattern, Plan and fitted, to write beside them. The
  rest of the reading was the eval task and freight's literals, to
  choose the design.
- **One change that would have helped:** none for the tools. The item
  was small once `KeyValueExpr` turned out to be an `ast.Expr` that
  shapeOf already handled.

## Follow-ups

- An A/B on freight-xxl's routes (sessions with the tools), to see
  whether a session uses the element pattern. This needs the user's yes.
- An element pattern without holes or `everywhere` still goes through
  the plain path (`locate.ExprTokens`), which doesn't parse an element.
  Routing it to planPattern is a one-line change, with a golden.
  *Done* in the next PR: replace_expression of Plan's condition, isElement
  added with add_declaration, golden `key_single`.
- The A/B: done (cost-history 34). It went 8/8 at $0.27 a run, against
  $0.39. Six refusals came from batching both fields on one line
  (roadmap 65).
- A merge drops a comment that sits between the deleted element and the
  one before it.
