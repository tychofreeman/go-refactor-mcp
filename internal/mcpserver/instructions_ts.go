package mcpserver

// TypeScriptInstructions are Instructions for a project that holds
// TypeScript and no Go: its tools, rules and example are TypeScript's.
const TypeScriptInstructions = `Refactoring and editing tools for the TypeScript in this project: the solution of the nearest tsconfig.json and the projects it references, tests included.

Two kinds of tool, by what they guarantee:
- Refactorings preserve behavior. They find every reference with the type checker (every use and call, in every project of the solution), so there is no need to search for them first; they refuse changes that would alter behavior.
- Edits change behavior on purpose: the step that adds or changes a feature.
Every step, refactoring or edit, must add no type error to the solution (errors it already has are tolerated); a step that would is refused with the errors, and nothing changes. An edit that calls functions or methods, makes classes or reads properties that nothing declares yet gets stubs for them (create_from_usage), so to write a failing test first, just write the test.
Some tools choose for you (create_from_usage chooses a stub's signature and file); their result lists each choice on a "chose:" line. Check them: give the choice explicitly, or undo, to change it.

Which tool, by what you want to do:
- Read the code: inspect.
- Rename or move: rename, move_declaration.
- Split or join code (refactorings): extract_function, inline_function, extract_variable, inline_variable, extract_interface, merge_duplicate_function, function_to_method, method_to_function, move_method; fold replaces code that does what an existing function does with calls to it; parameterize makes functions that differ only in constants calls of one that takes them; union_switch_to_method makes each case of a switch on a union of classes a method of its class; prepare_switch_for_types prepares a switch on a value for its cases to move into classes one at a time.
- Change a signature and every call (refactorings): add_parameter, remove_parameter, move_parameter, introduce_parameter, convert_params_to_object, change_parameter_type, add_result, change_field_type.
- Simplify control flow (refactorings): invert_if, remove_else, move_statement_up, move_statement_down, remove_dead_code.
- Add code (edits): create_file for any new file, TypeScript or not (test fixtures, JSON; several at once in one batch), add_declaration, add_field, add_case, add_element, insert_statements; create_from_usage declares the functions, classes, methods and properties code uses before they exist.
- Change code (edits): replace_statements, replace_expression, replace_declaration (a whole declaration, by name); change_type for what a type alias is, with the fixes it lists in the same batch; replace_text for string literals, comments and files that are not TypeScript.
- Move or delete files and directories (fixtures, docs, data): move_file, delete_file.
- Several steps in one call: batch. Then: check (with run_tests: the project's test script, or node --test), and undo to revert a step.

How to work with them:
1. Call inspect (no arguments) for an overview. Inspect a file to see its declarations without their bodies, with line numbers. Then read what you will change: inspect with function, or start_line/end_line, or several at once with targets; or several declarations by name alone with names; how each use treats a declaration (called, compared, written, read), with uses.
2. Refactor only while tests pass: first to make the change easy, after it to remove duplication. A failing test needs passing code first.
3. Apply ONE tool per call, or, when you can plan several steps from what you have read, send them together with batch: one call, rolled back as a whole if any step is refused. A step's args are that tool's own arguments, so if your client loads tool schemas on demand, first load the schema of every tool its steps use, all in one request, rather than guessing their names. Load schemas in the same response as another call, never in a turn of their own. Each call returns APPLIED with what changed, or REFUSED with the reason; a refusal changes nothing, so read it and choose differently.
4. Line numbers change after edits. Before a single step that needs them, re-inspect the code an earlier step changed. In a batch, give line numbers as they were when the batch began: the batch moves them past the earlier steps. To address what an earlier step of the batch made, give a reference instead of a number: "$prev.end", "$2.start" (see batch). Tools that target a declaration (rename, add_parameter, move_declaration, ...) also accept file and name without line: a top-level declaration of file, or a member (Class.method), which stays valid after edits.
5. Use check (with run_tests) at the end, and undo if a step made the code worse. Every step can be undone.

Example batch, for a feature added test first: the test imports and calls discount, which calc.ts does not export, so it gets a stub there; the next step replaces the stub with the least code that passes; then a rename. One call, one turn:
{"report": "summary", "steps": [
  {"tool": "replace_text", "args": {"file": "src/calc.test.ts", "old": "import { scale }", "new": "import { scale, discount }"}},
  {"tool": "add_declaration", "args": {"file": "src/calc.test.ts", "content": "export const discounted: number = discount(200, 10);"}},
  {"tool": "add_declaration", "args": {"file": "src/calc.ts", "content": "export function discount(price: number, pct: number): number {\n  return price - (price * pct) / 100;\n}"}},
  {"tool": "rename", "args": {"file": "src/calc.ts", "name": "scale", "new_name": "stretch"}}
]}

Prefer these tools over editing files by hand for everything they cover.`
