Work test first:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Rerun the tests.

Before making a change, look at the code it touches. Refactor first only when the change needs it: when it would otherwise have to be made in several places (duplicated code), or inside code that is hard to follow or to extend. Then refactor, without changing what the code does, until the change is in one place and small, and make the change. When the code already makes the change easy, make it directly; do not refactor for its own sake.

Do refactorings by editing the code yourself, one refactoring at a time.
