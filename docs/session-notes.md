# Session notes

What sessions working on this project have learned that the code, the
README and the other docs do not record: the user's preferences, why
past choices were made, and where the work stands. Every session reads
this (CLAUDE.md imports it). When you learn something a later session
would need, add it here in the same PR as the work, and correct or remove
what has become wrong. Keep it short: facts, with the reason, not a diary.
The longer history is in `docs/how-this-project-was-built.md`, and A/B
results are in `docs/cost-history.md`.

## Working with the user

- One item per branch and PR. The user merges, then reconnects the MCP so
  the next item uses the new server; wait for that before relying on a
  change to the tools.
- Ask before starting an A/B run unless the user asked for one: each costs
  a few dollars.
- When the user says "do X, then Y", do them in order, each in its own PR.
- Report results plainly, including when a change did not help (#119 was
  reverted in #120 after its rerun showed nothing).
- Stacked PRs: if one fails CI, fix the lowest branch and merge upward;
  do not force-push. (#41 failed on gofmt after an import added with sed.)

## Decisions and their reasons

- **Every step must type-check** (2026-10-17). The user rejected letting
  edits leave type errors. Test-first still works: stubs for what a new
  test calls are declared automatically, and an edit may start from
  broken code if it fixes some errors and adds none.
- **Claude via MCP is the product**, not the Ollama loop or standalone
  evaluation (2026-10-17): the user said those were not what they need.
- **Choose the next tool by evidence:** the friction log for hand edits,
  A/B transcripts for a fresh model's refusals and wasted turns.
- **Query tool deferred** (2026-10-18): about 55% of greps were symbol
  lookups, but grep was about 5% of an arm's input tokens. Revisit only
  with new evidence, and check gopls's MCP server first.

## A/B findings to remember

- Without a CLAUDE.md asking for it, a fresh model never used gorefactor.
  With one, it edits through it, but for additive features finds "nothing
  to refactor first"; tasks need the design in the way to see refactoring.
- On the small seeded module, refactoring first did not make a second
  change cheaper; plain editing stayed cheapest. Larger code or more
  changes are untested.
- MCP-only editing costs about 1.5x editing by hand; refactorings through
  the MCP cost about the same as by hand.
- In this project's own sessions, input tokens per edit step fell from
  1.2-1.9M (Oct 17, 10-20% through the tools) to 170-190k (Oct 19, ~100%
  through the tools, with batches). A fresh model does not get that from
  the tools alone; it comes from how they are used (CLAUDE.md, "Using
  gorefactor well").
- Input tokens are about requests × 39K in the feature runs, whatever
  the reads; count requests and what each turn was for.
- How the analyses were done: group a transcript's tool calls by
  assistant message id to count turns; weight each result's size by the
  number of turns after it to estimate what it cost in context.
- Harness details: the MCP binary is built into the output directory at
  the start, so checking out another branch during a run is safe. The mcp
  arm cannot use Edit or Write, and its Bash may not change files.

## Where the work stands (2026-10-20)

- #121 (inspect without bodies, `targets`) was measured in
  `ab-features-skeleton`: more `inspect`, fewer whole-file Reads, but the
  same read cost, since the model read bodies with wide `sed -n` ranges
  in Bash. With a reading paragraph in the mcp arm's CLAUDE.md
  (`eval-tasks/claude-md-refactor-first-inspect`, run
  `ab-features-read-inspect`), it read through `inspect` but just as
  much. Input tokens track the number of requests (~39K each), so the
  next lever is turns: the mcp arm's denied Write and Bash-write
  attempts and ToolSearch calls (together about a fifth of its turns).
- ToolSearch in `ab-features-read-inspect`: 9 of 141 turns loaded only
  a schema (6.5% of input), mostly `batch`, `create_file` and
  `replace_text` at the edit phase. A turn-0 preload barely pays (best:
  8 tools, 7 turns saved, under 1% of tokens), since a loaded schema is
  resent every request. #128 asks for schemas to be loaded alongside
  another call and shortens the most-loaded definitions (8 tools: 15.7K
  to 12.5K characters); not yet measured. Count ToolSearch-only turns.
- Line ranges in that run: of 30 inspect ranges, 18 spanned several
  functions (80% of range lines), 8 were exactly one function; a
  `function` list in inspect would change little.
- What reads are for (`ab-features-read-questions`, a "Q:" line before
  each read): orientation 41% of read cost (the 8.9K module overview
  alone 17%), how the code to change works 30%, where X is used 22%
  (broad greps), how tests are written 8%. Cheap wins: a smaller module
  overview; the find tool on the back burner now has evidence (2.6
  where-is-X reads per run). Reads are ~15% of input, so turns matter
  more.
- PR #122 was merged without its second commit (this file), which was
  pushed after the user had seen the PR. Put later work in a new PR
  rather than adding commits to one the user may already be merging.
- Roadmap item 15: refusals for ranges that start at the `func` line are
  done (#124); they give the body's lines and, for `replace_statements`,
  `function`. Still open: one worked example of a batch in the
  instructions.
- Sessions in a cloud clone have no MCP on that clone (the user's MCP
  runs on their own checkout); build `cmd/refactor` from the branch and
  use it for every step. It has no `batch`, so steps go one at a time.
- Smaller known bugs: `create_from_usage` infers odd result types for
  generic calls and names parameters p, p2; `replace_text` does not add
  imports.
- A full code review at a641b61 is in `docs/code-review-2026-10-20.md`:
  25 findings, 7 of them silent behavior changes or broken guarantees,
  most from panicking expressions treated as stable and methods reached
  by reflection. Its fix order: batch rollback and undo after a step
  breaks `go.mod` (items 6-7), then `analysis.Stable` (8, with 2);
  the order is an open question at the end of the review. Change only
  this repo copy: the Claude Doc linked from #130 is not kept up to date.
