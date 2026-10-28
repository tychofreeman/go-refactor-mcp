Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Type-check with `tsc -p .` and run the tests with `node --test` (`npm test` runs both).

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read TypeScript code with the gorefactor inspect tool, not with Read, cat or sed: every result stays in the conversation and is sent again on each later turn, so read only what you need. Inspect a file first: it shows the file without its function bodies, with line numbers, for a third to a half of the whole file. Then inspect only the functions or line ranges you will change, several at once with targets, e.g. {"targets": [{"file": "src/a.ts", "function": "f"}, {"file": "src/b.ts", "start_line": 10, "end_line": 30}]}. Use Grep or Bash for files that are not TypeScript (JSON, config) and to find where a name is used.

Your Bash can type-check, test and look, but it cannot change files: a command that writes (mkdir, sed -i, cat > file, cp, mv, rm) is refused, and the turn is lost. Make every new file with create_file (any file, TypeScript or not; it makes the directories too) and every change with the edit tools; for the same change in several files, replace_text with files.
