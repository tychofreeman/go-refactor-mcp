# go-refactor-mcp

A Go MCP server ("gorefactor") that gives Claude type-checked refactorings
and edits, for Go and TypeScript. The aim is to show whether refactoring
helps AI-driven development: Claude chooses refactorings, the deterministic
tools perform them, and Claude then makes the small change the story needs.
Judge work by whether it helps Claude do that during real feature work, and
by whether working through the tools costs fewer tokens than editing by
hand. Aim for about 95% of coding through the tools, this project's own
code included. The roadmap is `docs/roadmap.md`.

## How we work

TDD, in this order; refactor only while every test passes:
1. Green: make the change easy. If the change would be hard, refactor
   first, before writing the test, until it is small and local.
2. Red: write the next test and see it fail.
3. Green: make it pass with the least code, even code that duplicates
   what is there.
4. Green: refactor, removing that duplication and shaping the code for
   the changes we will want next. Rerun the tests.

When a test is failing or does not compile, the next step is code that
makes it pass, not a refactoring. If you find you need one, undo the
test (or set it aside), refactor while green, and add the test again.

Use the gorefactor MCP for every refactoring and edit it can do. Before
deciding that no tool covers a change, name what the result is made of in
operations (each new function is the old one with an argument fixed:
extract, fold, inline) and look for a sequence of two or three tools that
composes to it; `docs/derivations.md` has those found so far, and a gap
that turns out to be one goes there, not on the roadmap. A change no tool
or composition makes is made by hand and logged in `docs/friction-log.md`
(one entry per PR: what was done by hand, the sequences tried, why each
failed); candidate edit tools are in `docs/edits.md`.

One branch and one PR per item, never commits to main; the user merges.
When the user says "do X, then Y", do them in order, each in its own PR.
Before pushing, run what CI runs (`.github/workflows/test.yml`); hand
edits are not gofmt-ed, so this catches them:

    gofmt -l $(git ls-files '*.go' | grep -v /testdata/)   # must print nothing
    go mod tidy -diff
    go vet ./...
    go test -race $(go list ./... | grep -v /internal/ts/)
    go test ./internal/ts/...

Each PR also updates the docs it affects: the roadmap (mark an item
*Done.* when it is), `docs/friction-log.md`, `docs/checks.md` for what a
tool checks, and `docs/cost-history.md` for A/B results.

Match the surrounding code: its comment density, naming and idiom. Doc
comments say what a thing is or does, plainly.

What earlier sessions learned (the user's preferences, why past choices
were made) is in `docs/session-notes.md`, imported here. Keep it current
in the same PR as the work; notes saved anywhere else do not reach
sessions on other machines.

@docs/session-notes.md

When the user asks "what's next?", use the `whats-next` skill: it offers
the next items and keeps a record of the chosen one in
`docs/sessions/<branch>.md`, measured from the session log
(`refactor-tokens -log`).

## Using gorefactor well

`.mcp.json` runs `go run ./cmd/refactor-mcp` from this checkout, so a
session's tools are the code as it was when the session started: test a
server change with `go test`, not by calling the tool. After the user
merges, they pull main and reconnect the server (`/mcp`). In a cloud clone
with no MCP, build `cmd/refactor` and send the same steps through it
(session notes).

1. **Load the schemas you will use at once,** in one ToolSearch
   (`select:mcp__gorefactor__batch,mcp__gorefactor__replace_statements,...`).
   Guessed argument names cost a refusal and a turn.
2. **Read through inspect.** `inspect` of a file shows it without function
   bodies, with line numbers; then read the functions or ranges you will
   change, several in one call with `targets`, or by name with `names`
   (`["Settle", "Ledger.Post"]`, no file). A hook (`cmd/read-guard`)
   refuses Read of a `.go` file, and sed, cat, head, tail, awk or grep
   naming one, outside testdata/; grep over directories is allowed.
3. **Don't search before a refactoring.** The refactorings find every
   reference with the type checker.
4. **Plan from one read, send one batch.** Steps that build on each other
   go in one `batch`, with line numbers as they were when you read them:
   the batch moves them past earlier steps. `"report": "summary"` when you
   don't need the diff, `"check": "end"` when edits pass through code
   that does not compile yet (a test before the code it tests).
5. **Address by name, not line,** where a tool allows it: `file` + `name`
   (`Type.Method`, `Type.Field`), `replace_statements` with `function`,
   `add_declaration` with `after`/`before`, `replace_declaration` with
   empty `content` to delete. `$prev.end` addresses what an earlier step
   of the batch made.
6. **Test first through the tools.** Add the test with `add_declaration`
   or `create_file`; what it calls and nothing declares gets a stub
   (`panic("not implemented")`), which `add_declaration` of the real one
   replaces. An edit may start from code that does not type-check if it
   fixes some errors and adds none.
7. **replace_text is for text:** string literals, comments, and files
   that are not Go (docs, JSON fixtures; `files` for several, `append` for
   the friction log's entry). The server runs with `-no-code-by-text`:
   replace_text that changes Go code is refused with the structured edits
   to use instead.
8. **Read refusals; they say what to send.** A refusal changes nothing,
   and every step can be undone with `undo`. To restore a file to an
   earlier commit's version, use create_file with `overwrite`.

## Measuring cost

`cmd/refactor-ab` runs tasks in copies of the repo with a fresh model, per
arm (mcp, hybrid, manual), and reports cost, tokens, turns and pass rates
into `eval-runs/` (gitignored). It needs the `claude` CLI and costs a few
dollars a run, so ask first. The copies get their CLAUDE.md from
`-claude-md DIR`, not from this file. The usual runs:

    go run ./cmd/refactor-ab -model sonnet -arms mcp -runs 2 \
      -only feat-symbol-addressing,feat-case-clause,feat-no-echo,feat-compact-diffs \
      -claude-md eval-tasks/claude-md-refactor-first-bash -out eval-runs/NAME
    go run ./cmd/refactor-ab -model sonnet -arms hybrid,manual -runs 3 \
      -only chain-duplication,chain-tangled,chain-currency,chain-address \
      -claude-md eval-tasks/claude-md-refactor-when-needed -out eval-runs/NAME

`-tasks eval-tasks/mechanisms.json` runs the tasks that call for the
type-error help; `-mcp BINARY` runs an older server. `cmd/refactor-tokens`
measures a session's transcript, and the `ab-analyze` skill says how to
compare a run with a baseline. Results are in `docs/cost-history.md`.
Lessons so far:

* Run-to-run noise is 10-25% at 2-3 runs per task; a fix that saves a few
  refusals does not show in totals. Count the waste itself per run
  (refusals, turns lost), or run more.
* Count turns, not calls: parallel calls in one turn are free.
* Weight a read by the turns it stays in context, not its size alone.
* What helped a fresh model most was doing for it what it would get wrong
  (checking arguments up front, suggesting ranges, retrying with check
  "end", declaring stubs) and smaller results. A coaching tip in results
  did not help (#119), nor did more tools or longer descriptions.

Not wanted: work on the Ollama loop (`cmd/refactor-agent`) or standalone
batch evaluation (`cmd/refactor-eval`). On the back burner: a type-aware
find tool to replace grep (check gopls's MCP server first).
