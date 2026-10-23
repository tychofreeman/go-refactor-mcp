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

Treat the code as a structure you reshape with operations, not text you rewrite. Before reading any function body, write the change down as a derivation: operations that keep behavior, ending in the one small edit that changes it. Plan from names and signatures (inspect of a file shows them without bodies); the operations find every use through the type checker, so you need not read or search for the code they change, and a refusal says what is wrong. Read a body only where the behavior changes, or to check a shape you are not sure of. Writing Go with replace_statements, replace_declaration, replace_expression or replace_text is writing by hand: keep it to the fewest distinct lines. An operation counts as tried only once called; undo reverts a step.

## A switch on a kind, into a type per kind

When a field holds a kind as a value (`Shape.Kind`, `"circle"` or `"square"`) and functions switch on it, and each kind should become a type, move one kind at a time, every step keeping behavior:

1. Widen the field to an interface: change_field_type of `Shape.Kind` to `any`. Then prepare_switch_for_types on each switch, with one interface (`Kind`) and the function's name as the method. Declare the first kind's type (`type Circle struct{}`).
2. Give the interface's methods the signatures you want while no more than one type implements it: add_parameter and remove_parameter on the interface method (`Kind.Area`) change every implementation and call with it (add the new parameter, make the one implementation use it, then remove the old).
3. Each switch's case for the kind into a method of its type: extract_function of the case's body (`circleArea`); introduce_parameter and remove_parameter until its parameters are the interface method's; add_parameter of the receiver (`c Circle`, value `Circle{}`, before the others); function_to_method; rename to the interface method's name. A kind without a case of its own runs the default: add_case it with the default's body first. Every switch and kind in one batch.
4. inspect with `uses` of the field: every use, by what it does with it. Where no use tells a kind's old value from its type, send the value through the type where it is written or compared (`Circle{}` for `"circle"`, in tests too, and where a kind is read from text). These are the edits.
5. Narrow the field: change_field_type of `Shape.Kind` to `Kind`. It deletes the cases no `Kind` can match; if it refuses, it lists each use that still holds or compares an old value. Delete a switch left empty with replace_statements of its lines and empty content.
