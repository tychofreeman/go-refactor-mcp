Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Make every change in two phases: "make the change easy, then make the easy change".

1. Make the change easy. Before changing any behaviour, refactor: change the design of the code, without changing what it does, until the change you need is small and local. Typical refactorings: rename something to what it will mean, extract the code you will change into its own function, add the parameter or field the new behaviour will need, move a declaration to where the change belongs. The tests keep passing after every refactoring.
2. Make the easy change. Then make the behaviour change itself, which should now touch little code.

Do the refactorings in phase 1 with the gorefactor MCP tools (rename, extract_function, add_parameter, introduce_parameter, move_declaration, ...). Make the change in phase 2 with the gorefactor edit tools (replace_statements, insert_statements, add_declaration, create_file, ...).

Read Go code with the gorefactor inspect tool, not with Read, cat or sed: every result stays in the conversation and is sent again on each later turn, so read only what you need. Inspect a file first: it shows the file without its function bodies, with line numbers, for a third to a half of the whole file. Then inspect only the functions or line ranges you will change, several at once with targets, e.g. {"targets": [{"file": "a.go", "function": "F"}, {"file": "b.go", "start_line": 10, "end_line": 30}]}. Use Grep or Bash for files that are not Go (test fixtures, JSON) and to find where a name is used.

## Explore, then teach

You are one of several sessions whose notes will teach later sessions. Those sessions will work on other Go code with the same tools. What they learn from you matters more than how cheaply you finish.

- Reach the change through the gorefactor refactorings, the operations that keep behavior, and keep the code you write by hand (replace_statements, replace_declaration, replace_expression, replace_text on Go code) as small as you can.
- When a refactoring is refused, or does not do what you wanted, do not fall back to writing the code by hand straight away. Read the refusal and try another way: a refactoring first that would let it go through, a different operation, or the same one on a different part of the code. Try at least two other ways before you write the code by hand.
- Before you write Go code by hand for a step, even where no refusal stopped you, try at least two operations that might do that step instead, including ones you expect to be refused: a refusal costs one call, changes nothing, and often names what would work. Say in your lessons which ones you tried.
- End your final message with a section headed `## Lessons`: 3 to 8 bullets, each a situation and what worked there (the operation and the shape of its arguments), or what did not work and why. Write them for a session on different code: name the kind of code and the operation, not this module's names. List only what you saw happen in this session.
