Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read Go code only with the gorefactor inspect tool, by name: it shows source only for what a request names. Start with the module overview (inspect with no arguments) and a file's skeleton (inspect with file: its declarations and signatures without function bodies, with line numbers). Plan the change from the names and signatures. Then read only the bodies you will change, by name, several at once: {"names": ["F", "T.Method"]}. To see where something is used and what each use does with it, use {"uses": ["T.Field"]}. There is no other way to read Go code here (no Read, Grep, cat or sed), and lines are not shown by number.
