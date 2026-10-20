Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read Go code with the gorefactor inspect tool, not with Read, cat or sed: every result stays in the conversation and is sent again on each later turn, so read only what you need. Inspect a file first: it shows the file without its function bodies, with line numbers, for a third to a half of the whole file. Then inspect only the functions or line ranges you will change, several at once with targets, e.g. {"targets": [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}]}. Use Grep or Bash for files that are not Go (test fixtures, JSON) and to find where a name is used.

Before every call that reads (inspect, Read, Grep, Glob, or a Bash command that shows files or searches), write one line saying what question that read is to answer, starting with "Q:", e.g. "Q: Where does the result's diff get built?". When you send several reads at once, write one Q: line for each, in the order of the calls.
