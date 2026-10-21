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

Treat the code as a structure you reshape with operations, not text you rewrite. Before changing anything, write the change down as a derivation: a list of operations that keep behavior, ending in the one small edit that changes it. Then send it, in one batch where you can. Use the operations you have; where none does a step, make the manual edit as small as it can be, and avoid it entirely when you can.

Moves that work:

- **Merge near-duplicates.** Name what differs in each copy (extract_variable; with all_occurrences, from the first occurrence), so the rest of the copies is the same. extract_function the rest from one copy, then fold the other copies onto it: fold replaces code that does what an existing function does with calls to it. Functions that differ only in constants: parameterize makes them calls of one function that takes the constants.
- **Make room for a feature in a loop.** Change the data before the loop, not the loop: extract_variable the collection, insert one line that filters or maps it (its helper is declared for you as a stub), then write the helper.
- **Change a representation.** Put a function in front of every use (add_declaration a getter or setter, then fold the uses onto it), then change the representation inside that function only.
- **Move behavior.** Write the new function or method as a call of the old one (add_declaration), fold the callers onto it, then inline_function the old one with all_calls.
- **Change a signature or a parameter's shape: expand, migrate, contract.** Write the new function delegating to the old one; fold the callers onto it (a struct parameter used by its fields makes fold pass a literal, Batch{Users: u, Message: m}); inline_function the old one with all_calls; rename the new one. For one parameter or result, add_parameter, remove_parameter, move_parameter, change_parameter_type, add_result and change_field_type do it in one step, interface implementations included.
- **Name before you move.** rename, extract_variable and extract_function give the parts names; the later operations then address them by name, which stays valid after earlier steps.
