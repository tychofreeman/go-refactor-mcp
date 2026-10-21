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

Each of these reached its goal with at most one line of Go written by hand, and was found by two or more sessions. Write as little Go by hand as you can: when a step seems to need code, look for operations that produce it, and try them (a refusal costs one call and changes nothing; `undo` reverts a step, and one undo reverts a whole batch).

Derivations:

- **A new field or type a test needs costs no code:** write the test first (`create_file`), using it in a keyed literal; the field or the type is declared for you, typed from the literal (a "chose:" line). Do not `add_field` it afterwards.
- **A behavior change inside a nested loop:** `extract_function` the inner loop with the declaration of what it builds (whole statements); it takes the locals as parameters and returns the result. The change is then one guard line: `insert_statements` with `where: "into"` on the loop's line, or `before` its first statement.
- **A helper only one type uses becomes its method:** `add_parameter` with the receiver's type, `before` the first parameter, and as `value` the receiver variable every caller has in scope (rename a parameter that already has that name first: `rename` with `function` and `name`); then `function_to_method` with `new_name`. To drop a parameter every caller passes the same constant: insert `n = <constant>` as the body's first statement (one line); `remove_parameter` then accepts it, since it is assigned before it is read, and leaves `n := <constant>`.
- **A function takes several parameters as one struct:** declare the struct through a test (above). Where the calls pass different expressions, `extract_variable` them at every call with one name, so one `add_parameter` value fits every call (`T{A: a, B: msg}`). `extract_function` the body into a helper; the old body becomes one line, `return helper(b.A, b.B)` (`replace_statements` with `function`); `remove_parameter` the old parameters. A value that must be qualified in other packages cannot be unqualified in its own: move a test that calls it there out of the way for the step, or into the `_test` package.
- **Near-duplicate methods into one function:** in each copy, extract only the statements that differ least (for example the one that writes a field), `method_to_function`, `extract_variable` with `pointer` on the field it assigns (`p := &x.F`, uses become `*p`), `introduce_parameter` for `&x.F` and for each other part that differs, `remove_parameter` what is left unused. `add_parameter` with each copy's own constant where they differ in one. Rename until the copies' bodies and signatures are the same, then `merge_duplicate_function` them into one. A statement the copies share (a log line) is then added once (`insert_statements`, the one line) and deleted from each copy (`replace_statements` with empty `content`). `introduce_parameter` of `&x.F` is refused when a call comes before it in the body, so extract the statement that uses it alone first.
- **Every read of a field through a function:** a getter with no code by hand: `extract_variable` the field read, `extract_function` that one line, `inline_variable` in the new function. `fold` it: every read of the field, in every package, becomes a call. Then a change of the field's representation is a change of the getter's body, one line. Several fields that change: one at a time, each with its own getter, fold and line; a test's literals give the new field (a test that uses it declares it), and the old field goes once nothing reads it.

Using the tools:

- **Address by name, not line:** `function: "Type.Method"`, `rename` with `function` and `name`, `add_declaration` with `before`/`after`. In a batch, a step cannot address a line an earlier step rewrote: use `"$id.start"`, or a later call.
- **End a batch with `{"tool": "check", "args": {"run_tests": true}}`** to run the tests in the same call. Load each tool's schema before calling it.
