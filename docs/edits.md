# Edits: candidates

Refactorings keep behavior; edits change it on purpose, as the step that
adds or changes a feature does. The aim, later, is to do nearly all coding
through these typed steps instead of editing files by hand. For now the
focus stays on refactorings; this file collects the edits we may want, and
the evidence for each, so the order is decided by real use when we get to
them.

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

## Candidates

Smallest first within each level. Each is placed by file, line and name
like the refactorings, and checked to parse.

Some are refactorings when the tool can prove behavior does not change,
and edits otherwise; those are marked *refactoring when …*. The refactoring
form can come first, while the focus is on refactorings.

**Expressions**

- `replace_expression`: replace one expression (found as
  `extract_variable` finds one) with new Go source, reporting a change of
  type.
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
- `delete_statement`: delete whole statements, or a whole declaration (a
  function, type, variable or constant), as long as the result
  type-checks. Deleting a call for its side effect is often the point, so
  this is an edit, even though some deletions keep behavior. The
  refactoring that deletes only what cannot matter is a different tool,
  `remove_dead_code` (on the roadmap): nothing uses what it declares, and
  it has no effect.
- `move_statement_up` / `move_statement_down`: swap a statement with the
  one before or after it ("slide statements"). *Refactoring when* the two
  are independent: neither writes what the other reads or writes, their
  side effects cannot be reordered observably, and no declaration moves
  past a use of it.
  - 2026-10-17, `extract_function` returning `f(...)`: `at :=
    x.insertOffset(decl)` slid above the call-site code so the code could
    be extracted together with its `site.String()`.
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

- `add_declaration`, `replace_declaration`: a whole top-level function,
  type, variable or constant (deleting one is `delete_statement`).
  - 2026-10-17, adding `remove_parameter`: a stub declared so a test written
    first compiled.
  - 2026-10-17, adding `introduce_parameter`: the stub's `Plan` replaced by
    the real one, and the new checks added beside it.
- `add_field` (struct) and `add_method` (a method on a type, or an
  interface method).
  - 2026-10-18, one interface check instead of two: `Workspace.TypesInfos`.

## Suggested, without evidence yet

`delete_statement` (including declarations), `move_statement_down`,
`move_statement_in/out`, `introduce_block` and `introduce_if` were
suggested on 2026-10-17 and have no friction-log
entries yet. More will be added here as they come up.

## Not yet logged

So far every refactoring in this repository was itself written by editing
files directly: new functions, new files, changed statements. Those edits
were not logged one by one; from now on, hand edits made during feature
work are logged with the candidate that would have covered them.
