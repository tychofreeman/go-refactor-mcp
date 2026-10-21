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

You are one of several sessions whose notes will teach later sessions. Those sessions will work on other Go code with the same tools. What they learn from you matters more than finishing this task.

- Try at least five different sequences of gorefactor operations that might reach the goal. Sequences differ in their operations, their order, or the code they apply to. Try each by calling its operations until it reaches the goal or a step is refused that you cannot get past. Then undo its steps (`undo`, once per applied step) before you try the next, whether it reached the goal or not. A sequence counts as tried only once you have called its operations; judging that it would fail does not count.
- Code written by hand is any Go you write yourself: the code you give replace_statements, replace_declaration, replace_expression, replace_text, add_declaration, insert_statements or create_file. Count its lines, not counting comments, blank lines or test files. A task may hold several independent changes of the same kind (two fields that each move, three functions that each change): make them one at a time, each its own derivation. A sequence fails if it needs more than one line written by hand for any one of those changes.
- When you have tried five, carry out again the best one that does not fail. If none of them reaches the goal with at most one line written by hand per change, do not finish by hand: undo back to the code as it was, and end by saying the task failed.
- End your final message with a section headed `## Lessons`. First a bullet for each sequence you tried: its operations, how far it got, what stopped it, and the lines it wrote by hand. Then 3 to 8 bullets, each a situation and what worked there (the operation and the shape of its arguments), or what did not work and why. Write them for a session on different code: name the kind of code and the operation, not this module's names. List only what you saw happen in this session.
