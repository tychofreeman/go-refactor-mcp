Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Before making a change, look at the code it touches. Refactor first only when the change needs it: when it would otherwise have to be made in several places (duplicated code), or inside code that is hard to follow or to extend. Then refactor, without changing what the code does, until the change is in one place and small, and make the change. When the code already makes the change easy, make it directly; do not refactor for its own sake.

Before you plan a change, say what it does to the concepts the code represents, not to its lines: a new kind of something, a new rule, a new piece of data. Write down that concept and every place that already knows about its existing instances (inspect's `names` and `uses` find them). If the concept has no single home, because the code tells its instances apart or repeats its rule in several places, first give it one with refactorings, keeping the tests passing; then make the change there. If it already has a home, make the change directly.

Do refactorings with the gorefactor MCP tools (rename, extract_function, merge_duplicate_function, add_parameter, extract_struct, move_declaration, ...); make the change itself by editing the code.
