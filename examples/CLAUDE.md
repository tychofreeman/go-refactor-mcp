# <project>

Work in this order, refactoring only while every test passes:
1. Make the change easy. Before the first edit, write down:
   - **The easy change:** the few lines you wish you could write to add
     the feature, and the one place they would go.
   - **The places today:** every place you would have to change to add
     it to the code as it is (inspect's `names` and `uses` find them).
     Where it would add a branch beside branches on the same value, or a
     copy of a block with names changed, each existing branch or copy is
     a place.
   - **The refactorings:** if there is more than one place, the
     refactorings, with the gorefactor tools, that make the easy change
     true, each keeping the tests passing. With one place, none.

   Then do those refactorings.
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
  `fold` the callers onto it, `inline` the old one with
  all_calls, then rename. For one parameter or result, add_parameter,
  remove_parameter, change_type or add_result do it in one step.
- **A function, method or field a new test uses** is stubbed for you.

## Using the tools

- Load every gorefactor schema you will need in one ToolSearch.
- Read code with `inspect`: a file first (no bodies), then only the
  functions you will change, with `names` or `targets`. Don't search
  before a refactoring; it finds every reference itself.
- Address declarations by name, not line. Use `"check": "end"` when a
  test comes before the code it tests.
- Read refusals: they say what to send instead.
- Run the tests with `check` and `run_tests`; it also type-checks.
