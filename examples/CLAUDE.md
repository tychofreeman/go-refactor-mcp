# <project>

<!-- An example CLAUDE.md for a Go project that uses the gorefactor MCP.
It is the instruction set that did best in this repo's A/B runs (the
user's moves, eval-tasks/claude-md-derivations), cut to what measurably
helped: docs/cost-history.md sections 8, 9 and 14. Keep it short:
longer recipes cost turns. -->

Work in this order, refactoring only while every test passes:
1. Make the change easy: if it would be hard, refactor first, with the
   gorefactor tools, until it is small and local (rename to what a thing
   will mean, extract what you will change, add the parameter or field
   the change needs).
2. Write the next test and see it fail.
3. Make it pass with the least code, with the edit tools
   (replace_statements, insert_statements, add_declaration, create_file).
4. Refactor away the duplication, and rerun the tests.

## Work as a derivation

Treat the code as a structure you reshape with operations. Before
editing, write the change as a list of operations that keep behaviour,
ending in the one small edit that changes it, and send it as one
`batch`. Where no operation does a step, keep the hand edit as small as
it can be.

Moves that work:
- **Near-duplicates:** extract_function from one copy, then `fold` the
  others onto it. Copies that differ only in constants: `parameterize`.
- **Room in a loop:** extract_function the loop together with what it
  builds; the change is then one guard line.
- **Change a representation:** put a getter or setter in front of every
  use (add_declaration, then `fold`), change the representation inside
  it, then remove_dead_code the old field.
- **Move behaviour or change a signature (expand, migrate, contract):**
  add_declaration the new function as a wrapper over the old one,
  `fold` the callers onto it, inline_function the old one with
  all_calls, then rename. For one parameter or result, add_parameter,
  remove_parameter, change_parameter_type or add_result do it in one
  step.
- **A field or function a new test uses** is stubbed for you.

## Friction log

When no gorefactor tool covers a step, make it by hand, as small as it
can be, and log it in `docs/friction-log.md`. Append one entry per
change (a PR, or a task) at the end of the file, never between older
entries: what you did by hand, why no tool did it (none exists, one
refused and why, or its argument was missing), and the operation that
would have done it. Log refusals that cost a turn too. Edits to files
that are not Go, made with replace_text, need no entry. The log is the
evidence for which tool to build next, so record the gap, not a
workaround.

## Using the tools

- Load every gorefactor schema you will need in one ToolSearch.
- Read Go with `inspect`: a file first (no bodies), then only the
  functions you will change, with `names` or `targets`. Don't search
  before a refactoring; it finds every reference itself.
- Address declarations by name, not line. Use `"check": "end"` when a
  test comes before the code it tests.
- Read refusals: they say what to send instead.
