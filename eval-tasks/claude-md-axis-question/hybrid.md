Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Before making a change, look at the code it touches. Refactor first only when the change needs it: when it would otherwise have to be made in several places (duplicated code), or inside code that is hard to follow or to extend. Then refactor, without changing what the code does, until the change is in one place and small, and make the change. When the code already makes the change easy, make it directly; do not refactor for its own sake.

Before you plan a change, say what it adds or changes in terms of the concepts the code represents, not its lines: what it adds a new instance of (a new kind, case, channel, ...), or which rule it changes. Write that down, and every place that already knows about the existing instances or the rule (inspect's `names` and `uses` find them). The change is easy when the new instance is one new place (one new type, function, case or entry that holds everything about it), or the rule is in one place. If it would not be, first refactor, keeping the tests passing, until it would; then make the change there. If it already would be, make the change directly.

Do refactorings with the gorefactor MCP tools (rename, extract_function, merge_duplicate_function, add_parameter, extract_struct, move_declaration, ...); make the change itself by editing the code.
