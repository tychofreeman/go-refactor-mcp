We should take the TDD approach:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Then refactor, keeping in mind the changes we will want to make next.
* Rerun the tests.

When we refactor, we want to "make the change easy, and then make the easy change." That is, through small refactorings, slowly change the design of the system so that the next change we want to make is simpler and requires less work.

For any refactoring that can be done with the gorefactor MCP, we should use the MCP.
