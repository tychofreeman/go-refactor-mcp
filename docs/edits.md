# Edits: candidates

Refactorings keep behavior; edits change it on purpose, as the step that
adds or changes a feature does. The aim, later, is to do nearly all coding
through these typed steps instead of editing files by hand. This file
collects the edits we may want, and the evidence for each, so the order is
decided by real use: refactorings and edits alike are picked by what the
friction log shows was missing.

Every edit follows the rule refactorings do: the code type-checks before
and after each step, or the step is refused and nothing changes. New code
that names a package gets its import added, and imports left unused are
removed, so no edit needs a separate import step.

## Evidence

The friction log (`docs/friction-log.md`) records each change made by hand.
When no edit covered it, the entry names the candidate below (`Gap:` an
edit), and the candidate lists the entry. Only edits from real work count:
a candidate without evidence stays at the bottom.

## Done

- **`create_file`:** a new file, checked to parse, in the directory's
  package, and type-checking.
- **`replace_statements`:** whole statements of one block replaced by new
  ones, re-indented, with the imports they need added and those only the
  old ones used removed.
- **`insert_statements`:** new statements before or after a statement, or
  into a block (an empty body too), with the imports they need.
- **`add_declaration`:** new top-level declarations in a file, at its end
  or next to a named one, with the imports they need.
- **`replace_expression`:** one expression, or every one in the file that
  means the same, replaced by a new one, parenthesized as needed.
- **`add_field`:** new fields in a struct type, opening up a one-line
  struct, with the imports their types need.

## Candidates

Smallest first within each level. Each is placed by file, line and name
like the refactorings, and checked to parse.

Some are refactorings when the tool can prove behavior does not change,
and edits otherwise; those are marked *refactoring when …*. The refactoring
form can come first, while the focus is on refactorings.

**Expressions**

- `replace_expression` is done (see Done).
  - 2026-10-17, moving a declaration out of a group: six
    `ast.Inspect(d.decl, ...)` became `ast.Inspect(d.node(), ...)`; an
    all-occurrences option would have done them in one call.
- `introduce_constant` / `introduce_variable`: declare a new package
  constant or variable, or a local `name := value` before a statement.

**Statements**

- `insert_statements` is done (see Done).
  - 2026-10-17, moving a declaration to another package: three statements
    computing a doc comment replaced by a call to the new `docOf`.
  - 2026-10-18, one interface check instead of two: the bodies of
    `changesig` and `movedecl`'s `checkNoInterface` replaced by a call of
    `analysis.InterfaceWithMethod` and their messages.
- `delete_statement`: delete whole statements, or a declaration that is
  still used, as long as the result type-checks. Deleting a call for its
  side effect is often the point, so this is an edit, even though some
  deletions keep behavior. A declaration nothing uses is deleted by the
  refactoring `remove_dead_code` (done); its gap, deleting statements
  with no effect, is on the roadmap.
  - 2026-10-18, `versionSuffix` and `quoted` were deleted by hand; both
    were unused, so `remove_dead_code` now covers them.
- `move_statement_up` / `move_statement_down`: swap a statement with the
  one before or after it ("slide statements"). The refactoring form is
  done (the two must be independent; see the README); the edit form,
  moving a statement past one it is not independent of, is left.
  - 2026-10-17, `extract_function` returning `f(...)`: `at :=
    x.insertOffset(decl)` slid above the call-site code so the code could
    be extracted together with its `site.String()`. (The refactoring would
    refuse this one: `insertOffset` is a method call, so it counts as a
    side effect.)
- `move_statement_in` / `move_statement_out`: move a statement into the
  block that follows it, or out of the block it ends or starts.
  *Refactoring when* the block is a plain `{ }` block and scoping allows it
  (nothing it declares is used outside); into or out of an `if`, `for` or
  `switch` it changes how often the statement runs, so it is an edit.
- `introduce_block`: wrap statements in `{ }`. *Refactoring when* nothing
  they declare is used after the block.
- `introduce_if`: wrap statements in `if cond { }`, with a given condition.
- `introduce_loop`: wrap statements in a `for` loop, with a given clause.
  - 2026-10-17, moving several declarations at once: the checks on one
    declaration became loops over all of them.

**Declarations**

- `replace_declaration`: a whole top-level function, type, variable or
  constant (`add_declaration` is done; deleting an unused one is
  `remove_dead_code`). Since `replace_statements`, a stub's body is
  replaced without it; the entries below predate that.
  - 2026-10-17, adding `remove_parameter`: a stub declared so a test written
    first compiled.
  - 2026-10-17, adding `introduce_parameter`: the stub's `Plan` replaced by
    the real one, and the new checks added beside it.
- `add_method` (an interface method; a method on a type is
  `add_declaration`). `add_field` is done (see Done).
  - 2026-10-18, one interface check instead of two: `Workspace.TypesInfos`.

**Generators**

The edits above take the new code as `content`, so the model still writes
it. A generator takes a description instead (names, a field, an
operation, an expression) and writes the code itself, using the types
the type checker knows. The model then writes only names, conditions and
values; the tool writes loops, checks, tests and signatures.

- `create_loop`: a function, or statements, that loop over a slice or map
  and compute something from its elements, described by `over`, `op` and
  `field`: `create_loop function=totalCost over=items op=sum field=Cost`
  declares `func totalCost(items []Item) float64` with the range and the
  accumulator. Operations: sum, count, min and max, filter, map (collect
  a field), any and all (with a condition), and index by key (a map from
  a field to the element). The element, field and result types come from
  the type checker. Roadmap item 13.
  - 2026-10-17, adding `refactor-eval`, and adding `move_parameter`: a
    loop summing a field over a list, written by hand each time.
  - 2026-10-17, moving several declarations at once; moving from several
    files; 2026-10-18, chained tasks: one item became many, and each use
    a loop over them, the largest hand edit of each story (roadmap item
    9, "one to many"; `introduce_loop` above wraps existing statements,
    this writes new ones).
- `replace_parameter`: replace a parameter with a new one computed from
  it, given the new parameter, its type and the expression that computes
  it from the old one at each call (`write` of type
  `func(types.Type) string` from `qual` as
  `func(t types.Type) string { return types.TypeString(t, qual) }`). The
  tool rewrites the signature, the body's uses (which must all be ones
  the new parameter can serve, such as `types.TypeString(x, qual)`
  becoming `write(x)`) and every call. `change_parameter_type` (done)
  covers a conversion of the same value; this covers a different value.
  Roadmap item 13.
  - 2026-10-19, create_from_usage for generic types: a
    `types.Qualifier` parameter became a `func(types.Type) string` in
    three functions and their calls, in 13 `replace_text` steps.
  - 2026-10-19, change_parameter_type: the new tool did not cover that
    case, since it is not a type change.
- `create_test`: a table-driven test of a named function from its cases
  as data (arguments, wanted results, a wanted error), written with the
  table, the `t.Run` loop, the comparisons and the error checks. In this
  repository, `add_golden_case` would do the same for a refactoring's
  `testdata/<case>/` (`in/`, `args.json`, `out/` or `refused.txt`).
  - 2026-10-19, a cheaper fresh-load comparison; change_parameter_type:
    the tests were written whole with `create_file`, the largest code
    the model still sent that day.

Repeated sequences of steps rather than new code (scaffolding a new
refactoring: its package, `Plan`, golden-test harness and registry
entry) are recipes, roadmap item 11.

## Suggested, without evidence yet

`move_statement_in/out`, `introduce_block` and
`introduce_if` were suggested on 2026-10-17 and have no friction-log
entries yet. More will be added here as they come up.

Suggested on 2026-10-19, generators with no friction-log entry yet:

- `call_checked`: a call with its error check,
  `call function=parse args=[src] results=[file, err]` writing
  `file, err := parse(src)` and `if err != nil { return <zero values>,
  err }` with the enclosing function's zero values. The commonest Go
  boilerplate; it is also the error-exit shape `inline_function` lacks
  (roadmap item 4).
- The IDE "Generate" set: a constructor from a struct's fields, a method
  that forwards to a field (delegation), a `String` method, and an
  exhaustive `switch` over a type's constants or an interface's
  implementations (`create_from_usage` already writes the methods a type
  lacks for an interface).

## Not yet logged

Until the first edits (2026-10-18), every refactoring in this repository
was itself written by editing files directly: new functions, new files,
changed statements. Those edits were not logged one by one; since then,
hand edits are logged with the candidate that would have covered them.
