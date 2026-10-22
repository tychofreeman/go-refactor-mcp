Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read Go code with the gorefactor inspect tool, not with Read, cat or sed: every result stays in the conversation and is sent again on each later turn, so read only what you need. Inspect a file first: it shows the file without its function bodies, with line numbers, for a third to a half of the whole file. Then inspect only the functions or line ranges you will change, several at once with targets, e.g. {"targets": [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}]}. Use Grep or Bash for files that are not Go (test fixtures, JSON) and to find where a name is used.

## Work as a derivation

Treat the code as a structure you reshape with operations, not text you rewrite. Before changing anything, write the change down as a derivation: operations that keep behavior, ending in the one small edit that changes it, about one line per change the task makes. Then send it, in one batch where you can. Use the operations you have; where none does a step, make the manual edit as small as it can be, and avoid it entirely when you can.

Moves that work:

- **Make room in a loop.** `extract_function` the inner loop with the declaration of what it builds; the change is then one guard line (`insert_statements`, `where: "before"` the body's first statement, ahead of any call with side effects).
- **Change a representation, one field at a time.** Put a function in front of every use: derive a getter (`extract_variable` a read, `extract_function` that line, `inline_variable` in it), give it an exported name in the type's package, and `fold` it. Then change the representation inside the getter, and `remove_dead_code` the old field (`Type.Field`).
- **Move behavior or change a signature: expand, migrate, contract.** `add_declaration` the new function as a one-line wrapper over the old one; `fold` it (every call, in every package, becomes a call of the wrapper, a struct argument built from the old arguments); `inline_function` the old one with `all_calls`; `rename`.
- **A parameter every caller passes the same constant:** insert `n = <constant>` as the body's first statement, then `remove_parameter`.
- **A field or type a new test uses** is declared for you from its keyed literal.

These refuse: moving a read through a pointer, or a call, past an earlier call with side effects (extract what comes after it instead); `fold` of code not shaped like the function's body; `inline_function` of calls in another package.
