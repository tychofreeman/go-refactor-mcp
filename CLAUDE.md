# go-refactor-mcp

A Go MCP server ("gorefactor") that gives Claude type-checked refactorings
and edits. The aim is to show whether refactoring helps AI-driven
development: Claude chooses refactorings, the deterministic tools perform
them, and Claude then makes the small change the story needs. Judge work by
whether it helps Claude do that during real feature work, and by whether
working through the tools costs fewer tokens than editing by hand. Aim for
about 95% of coding through the tools, this project's own code included.

## How we work

TDD:
* Write the tests we need next first.
* Then make the tests pass with the minimal amount of code.
* Then refactor, keeping in mind the changes we will want to make next.
* Rerun the tests.

"Make the change easy, then make the easy change": through small
refactorings, change the design so that the next change is simpler.

Use the gorefactor MCP for every refactoring and edit it can do. When no
tool covers a change, make it by hand and log it in `docs/friction-log.md`
(one entry per PR: what was done by hand and why). That log is the
evidence for which tool to build next; candidate edit tools are in
`docs/edits.md`.

One branch and one PR per item, never commits to main; the user merges.
Before pushing, run what CI runs (`.github/workflows/test.yml`); hand edits
are not gofmt-ed, so this catches them:

    gofmt -l $(git ls-files '*.go' | grep -v /testdata/)   # must print nothing
    go mod tidy -diff
    go vet ./...
    go test -race ./...

Each PR also updates the docs it affects: the roadmap in `README.md` (mark
an item *Done.* when it is), `docs/friction-log.md`, and
`docs/cost-history.md` for A/B results.

Match the surrounding code: its comment density, naming and idiom. Doc
comments say what a thing is or does, plainly.

What earlier sessions learned (the user's preferences, why past choices
were made, where the work stands) is in `docs/session-notes.md`, imported
here. Keep it current: add what a later session would need, in the same
PR as the work, and fix what has become wrong. Notes saved anywhere else
do not reach sessions on other machines.

@docs/session-notes.md

## Using gorefactor well

The server is started from this checkout (`.mcp.json` runs
`go run ./cmd/refactor-mcp`), so a session's tools are the code as it was
when the session started. After changing the server, test the change with
`go test`, not by calling the tool: the running server is the old build
until the MCP is reconnected.

1. **Load every schema at once.** The tools are deferred: load all the
   gorefactor tools you expect to use in one ToolSearch
   (`select:mcp__gorefactor__batch,mcp__gorefactor__replace_statements,...`).
   Guessed argument names cost a refusal and a turn.
2. **Read through inspect.** `inspect` of a file shows it without function
   bodies, with line numbers, for a third to a half of the whole file;
   then read the functions or line ranges you will change, several in one
   call with `targets`. Avoid reading whole files with Read or `cat`: each
   result is sent again on every later turn, and whole files were over
   half of what reading cost in the A/B runs.
3. **Don't search before a refactoring.** rename, add_parameter,
   move_declaration and the others find every reference with the type
   checker.
4. **Plan from one read, send one batch.** Steps that build on each other
   go in one `batch` call, with line numbers as they were when you read
   them: the batch moves them past earlier steps. Use `"report": "summary"`
   when you don't need the diff, and `"check": "end"` when edits pass
   through code that does not compile yet (a test before the code it
   tests). Independent steps can instead be parallel calls in one turn,
   which costs no extra turn.
5. **Address by name, not line,** where a tool allows it: `file` + `name`
   for declarations (`Type.Method`, `Type.Field`), `replace_statements`
   with `function` to replace a whole body, `add_declaration` with
   `after`/`before`. Names stay valid after earlier steps; a reference such
   as `$prev.end` addresses what an earlier step of the batch made.
6. **Test first through the tools.** Add the test with `add_declaration`
   (or `create_file`). Functions and methods it calls (or passes as
   values) and types it makes with keyed literals that nothing
   declares get stubs (`panic("not implemented")`) automatically, and
   `add_declaration` of the real one replaces the stub. An edit may start
   from code that does not type-check if it fixes some errors and adds
   none (`add_field` for a field the new test uses).
7. **replace_text for everything else:** string literals, comments, and
   files that are not Go (README, docs, JSON fixtures); `files` makes
   the same replacement in several files. `old` must match
   the file exactly, after gofmt's alignment: if it is not found, grep for
   the line as it is.
8. **Read refusals; they say what to send.** A refusal changes nothing. A
   range that cuts a statement is refused with the range to use. Every
   step can be undone with `undo`.

Known gaps (do these by hand and log them): nothing removes a struct
field or restores a file.

## Measuring cost

`cmd/refactor-ab` runs tasks in copies of the repo with a fresh model, per
arm (mcp, hybrid, manual), and reports cost, tokens, turns and pass rates
into `eval-runs/` (gitignored). It needs the `claude` CLI and costs a few
dollars a run. The copies get their CLAUDE.md from `-claude-md DIR`, not
from this file. The usual runs:

    go run ./cmd/refactor-ab -model sonnet -arms mcp -runs 2 \
      -only feat-symbol-addressing,feat-case-clause,feat-no-echo,feat-compact-diffs \
      -claude-md eval-tasks/claude-md-refactor-first-bash -out eval-runs/NAME
    go run ./cmd/refactor-ab -model sonnet -arms hybrid,manual -runs 3 \
      -only chain-duplication,chain-tangled,chain-currency,chain-address \
      -claude-md eval-tasks/claude-md-refactor-when-needed -out eval-runs/NAME

The features run takes about 15 minutes. `cmd/refactor-tokens` measures
session transcripts: tokens by tool, turns that only loaded schemas or
had a call refused, and what reading cost by kind. The `ab-analyze`
skill (`.claude/skills/ab-analyze`) runs it on a run and a baseline and
says what to report. The history and what moved cost are in
`docs/cost-history.md`. Lessons so far:

* Run-to-run noise is 10-25% at 2-3 runs per task; a fix that saves a few
  refusals does not show in totals. Count the waste itself per run
  (refusals, turns lost), or run more.
* Count turns, not calls: parallel calls in one turn are free.
* Weight a read by the turns it stays in context, not its size alone.
* What helped a fresh model most was doing for it what it would get wrong
  (checking arguments up front, suggesting ranges, retrying with check
  "end", declaring stubs) and smaller results. A coaching tip in results
  did not help (#119), nor did more tools or longer descriptions.

On the back burner: a type-aware find/query tool to replace grep (a small
token win; check gopls's MCP server first if it comes back). Not wanted:
work on the Ollama loop (`cmd/refactor-agent`) or standalone batch
evaluation (`cmd/refactor-eval`).
