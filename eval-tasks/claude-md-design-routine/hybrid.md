Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Before your first edit, write these four things down, in order:
1. The easy change: the few lines you wish you could write to add this feature, and the one place they would go.
2. The places today: every place in the current code you would have to change to add the feature as the code is now (inspect's `names` and `uses` find them). Where the feature would add a branch beside branches on the same value, or a copy of a block with names changed, each existing branch or copy is a place.
3. The refactorings: if step 2 lists more than one place, the refactorings that make step 1 true, each keeping the tests passing and changing nothing the code does. If it lists one place, none.
4. Then do the refactorings, rerun the tests, and add the feature as step 1 says.

Do refactorings with the gorefactor MCP tools (rename, extract_function, merge_duplicate_function, add_parameter, extract_type, move_declaration, ...); make the change itself by editing the code.
