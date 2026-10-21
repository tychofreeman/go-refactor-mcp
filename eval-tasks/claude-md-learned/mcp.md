Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read Go code with the gorefactor inspect tool, not with Read, cat or sed: every result stays in the conversation and is sent again on each later turn, so read only what you need. Inspect a file first: it shows the file without its function bodies, with line numbers, for a third to a half of the whole file. Then inspect only the functions or line ranges you will change, several at once with targets, e.g. {"targets": [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}]}. Use Grep or Bash for files that are not Go (test fixtures, JSON) and to find where a name is used.

## Lessons from earlier sessions

Earlier sessions with the same tools, on other Go code, found these.

Derivations that worked:

- **A helper only one type uses becomes its method.** `function_to_method` takes the first parameter as the receiver, so first `add_parameter` with the receiver's type, `before` the current first parameter, and as `value` the receiver variable every caller already has in scope. If a parameter already has that name, rename it first (`rename` with `function` and `name`, no line). Then `function_to_method` with `new_name`. A parameter every caller passes the same constant: replace its uses in the body with the constant, then `remove_parameter` drops it at every call (it refuses while the body still reads it).
- **A behavior change inside a nested loop.** `extract_function` the inner loop together with the declaration of what it builds (whole statements). It takes the locals as parameters and returns the result, and the change becomes a guard in a small function: `replace_statements` with `function`, or `insert_statements` before the loop body's first statement.
- **Near-duplicate methods into one shared function.** `extract_function` the shared statements of one method (it becomes a method of that type), then `method_to_function`. Then `introduce_parameter` for each part that differs (`file`, `line`, the exact `expression`, `parameter`); an expression used twice gets `extract_variable` with `all_occurrences` first. For a field the copies assign to, `extract_variable` with `pointer` (`sent := &x.F`, its uses become `*sent`). `introduce_parameter` on `&x.F` is refused while a call comes before it: move the alias line above the call by hand, then retry. A literal inside a format string cannot be lifted: rewrite `"x to %s"` as `"%s to %s", "x"` by hand, then `introduce_parameter` on `"x"`. Finish with `remove_parameter` for what is no longer used, `move_parameter` and `rename`. `fold` does not match the other copies while they differ in literals or in a field against a pointer: replace their bodies with a call, `replace_statements` with `function: "Type.Method"`, one step per copy.
- **A signature change whose new argument differs at each call** (existing parameters bundled into a struct, callers in other packages): expand, migrate, contract. Add the new function under a temporary name beside the old one, move the callers over (`replace_expression` per call, or `replace_text` with `all_occurrences` and `files` when they have one shape), `remove_dead_code` the old one with `exported: true`, `rename` the new one to the old name, all in one batch. `add_parameter` and `introduce_parameter` refuse a value whose type the callers' package would have to qualify, and `inline_function` refuses callers in another package.

Using the tools:

- **Test first with a new field:** `create_file` of a test that uses it declares the field (a "chose:" line); `add_field` for it afterwards is refused as a redeclaration.
- **Address by name, not line:** `function: "Type.Method"` in `replace_statements` and `inspect`, `rename` with `function` and `name`, `add_declaration` with `before`/`after`. After an edit shifts lines, re-inspect before a step that needs line numbers.
- **In a batch, a step cannot address a line an earlier step rewrote:** give the earlier step an `id` and use `"line": "$id.start"`, or touch each line once.
- **A signature change and its callers go in one batch** with `check: "end"`; `replace_declaration` alone is refused while the callers do not compile. End the batch with `{"tool": "check", "args": {"run_tests": true}}` to run the tests in the same call.
- **`replace_expression` takes a whole Go expression:** not a `Key: value` element or an elided `{...}`; use the enclosing literal, or `replace_text`.
- **Argument names:** `replace_statements` and `replace_declaration` take `content`; `insert_statements` needs `where`; `add_parameter` takes `value` and `before`. Load each tool's schema before calling it.
