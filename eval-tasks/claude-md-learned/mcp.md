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

Earlier sessions with the same tools, on other Go code, found these. Each derivation reached its goal with at most one distinct line of Go written by hand for each change the task makes (one per field that moves, say), and was found by two or more sessions. Write as little Go by hand as you can: when a step seems to need code, look for operations that produce it, and call them (a refusal costs one call and changes nothing; `undo` reverts a step).

Derivations:

- **A new field or type a test needs costs no code:** write the test first with a keyed literal that uses it; the field or the type is declared for you, typed from the literal (a "chose:" line).
- **A behavior change inside a nested loop:** `extract_function` the inner loop with the declaration of what it builds (whole statements); it takes the locals and returns the result. The change is one guard line, `insert_statements` with `where: "before"` on the loop body's first statement. Put a guard before any call with side effects, and re-read line numbers after the extraction: a guard one line too late type-checks, and only the test catches it.
- **A helper becomes a method, or a function takes a new signature (expand, migrate, contract):** `add_declaration` the new one as a one-line wrapper over the old (`func (s *T) fit(x string) string { return trunc(x, 20) }`, or `func SendAll(c Config, b Batch) []string { return Send(c, b.Users, b.Msg) }`), `fold` it: every call of the old one, in every package, becomes a call of the wrapper, building a struct argument from the old ones where the wrapper takes one; `inline_function` the old one with `all_calls`; `rename` the wrapper to the old name if the name should stay. The wrapper is the one line.
- **A parameter every caller passes the same constant:** insert `n = <constant>` as the body's first statement (the one line); `remove_parameter` then accepts it and leaves `n := <constant>`.
- **Parameters bundled into a struct, other ways:** extract the body into a helper, so the old body is one line, `return helper(users, c, m)`; write that line with the struct literal, `return helper(Batch{Users: users, Message: m}.Users, ...)`, `extract_variable` the literal with `all_occurrences`, and `introduce_parameter` it: the literal is written at every call, from that call's arguments. Or `add_parameter` with the literal as its value, once each call names what it passes (`extract_variable` at each call, one name). A value or literal that names a type of the function's package is qualified at calls in other packages for you.
- **Near-duplicate methods into one function:** in each copy, name what differs (`extract_variable`; for a field the copy assigns, `extract_variable` with `pointer`, so `x.F = append(x.F, ...)` becomes `p := &x.F; *p = append(*p, ...)`), `extract_function` only the statements after any call with side effects (here the append, not the log call before it), `method_to_function`, `introduce_parameter` for the pointer and each part that differs, `remove_parameter` the rest. Give each copy's constant with `add_parameter` (a method of the receiver works as the value, `n.Name()`, once every receiver has the same name). When the copies are the same text, `merge_duplicate_function` them into one. Add a statement they share (the log line) once with `insert_statements`, the one line, and delete it from each copy with `replace_statements` and empty `content`.
- **A field's representation changes:** one field at a time. A getter with no code by hand: `extract_variable` a read of the field, `extract_function` that line, `inline_variable` inside the new function. Give it an exported name and `move_declaration` it to the type's package, so `fold` reaches every package; `fold` it: every read becomes a call. A test's literal declares the new field. Then change the getter's body, the one line; `remove_dead_code` the old field (`Type.Field`) once nothing uses it.

Dead ends:

- `introduce_parameter`, `add_parameter`, `move_statement_up` and `move_statement_down` refuse to move a read through a pointer (`&x.F`) or a call past an earlier call with side effects (a log line): extract the statements after it instead.
- `parameterize` takes functions or methods of one receiver type only; `fold` matches only code of the same shape as the function's body, with its parameters as the holes; `inline_function` inlines only calls in the function's own package.
- In a `batch`, a step cannot address a line an earlier step rewrote: use `"$id.start"`, a name (`function`), or a separate call.
