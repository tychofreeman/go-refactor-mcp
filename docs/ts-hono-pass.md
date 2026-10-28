# The TypeScript tools on Hono

Before measuring with A/B runs (`docs/ts-plan.md` step 11), the
TypeScript tools were run on a real repository: Hono at ed1d5f7 (291
source files under `src`, a solution of seven tsconfigs), with its
dependencies installed (`pnpm install --ignore-scripts`; without them
the solution has 11,136 errors, with them 4, all in `src/hono.test.ts`).
The tools ran through the MCP server, driven over stdio by a small
script, 50 calls on code chosen from `src/utils`, `src/middleware`,
`src/router` and `src/http-exception.ts`, each applied one then undone.

## What broke, now fixed

All four were style, not behavior; the type check passed every time.

- Statements written by the tools ended with `;` in a file without
  semicolons (extract_variable, extract_function, inline_function's
  declared arguments, parameterize, fold, union_switch_to_method,
  add_field's members). They follow the file now: `semicolons` looks
  at the first statement at any depth, not only the top level, where a
  file of functions has none that does not end in a brace.
- New code was indented by one space under a JSDoc comment
  (extract_function, extract_interface): the file's indentation step
  was the fewest spaces any line starts with, and ` * Sizes.` starts
  with one. A comment's continuation lines no longer count.
- An import added to a file was written in the quotes of the file the
  declaration came from (move_declaration, merge_duplicate_function,
  fold), or in `"` when that file had no imports. Each file's imports
  are now written in its own quotes, else those of its first string.
- inline_variable named on a line was refused as ambiguous when the
  line also had a property of the same name (`const url = request.url`).
  Property names no longer count.

Also: `String.replace` and `replaceAll` with a string or a regular
expression written there and a string for what replaces it are known
to change nothing (a function there would be called).

## What worked

rename (a function, a class member: 3 files), extract_variable (one,
all occurrences), extract_function (a tail that returns), inline_variable
(a `new` used once by a return), inline_function (an expression body,
with its argument declared before the statement because it may change
state), introduce_parameter (6 calls in tests), convert_params_to_object,
move_parameter, move_declaration (to a new file, tests' import split),
move_file (5 importers updated), remove_else (an else-if chain),
move_statement_up, extract_interface (an interface, a class with an
accessor and a method), parameterize (two bigint predicates), and the
edits (replace_expression, insert_statements, replace_statements by
function, add_field).

## Refusals, each with its reason

Correct and clearly worded: an unused parameter where
`noUnusedParameters` is on (name it `_x`), add_result on a function
used as a value, delete_file of an imported file (13 importers),
remove_dead_code of something tests use, merge of two functions
reading different variables of one name, inline_variable whose value
reads state a call in a loop may change, moving a statement past the
declaration it uses, extract_interface of a generic class.

Gaps the pass found, for later (`docs/ts-plan.md`):

- extract_function refuses lines with an early return; Go's makes
  them return a result the call checks.
- invert_if of an if that returns, followed by the function's last
  statements (`if (x) { ...; return y } return z`), is refused; Go's
  un-nests that shape.
- inline_function refuses when a parameter's name is taken where it
  would be declared (`const generator = initializeGenerator(generator)`);
  it could choose another name.
- No shape in Hono for union_switch_to_method or merge_duplicate_function
  to apply to; both rest on their unit tests.

## Time

The first call waits for the load (13-15 s); a refactoring then takes
4-9 s, most of it the type check of the projects the change touches,
and a refused one under a second.
