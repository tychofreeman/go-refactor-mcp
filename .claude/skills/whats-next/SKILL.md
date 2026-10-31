---
name: whats-next
description: When the user asks "what's next?" (or /whats-next), offer the next items of work with their evidence, let the user choose, then do the chosen item while keeping a session record in docs/sessions/ that shows whether the tools were used or worked around.
---

# What's next

Two halves: choosing the item with the user, then doing it with a record
that lets the user judge afterwards whether the work went through the
tools as this project intends, or around them.

## 1. Find the candidates

Quietly, without reading Go:

- **Open PRs** (`gh api repos/tychofreeman/go-refactor-mcp/pulls`, or
  `git branch -r` when the API is closed): work waiting for the user to
  merge is not a candidate, and a later item that builds on one waits too.
- **The roadmap** (`docs/roadmap.md`): the newest items (the last ten or
  so) and their numbered sub-items without *Done*; older items whose text
  says "next" (`grep -n -i "next" docs/roadmap.md`).
- **The newest "Next"** in `docs/cost-history.md` (its last sections) and
  in `docs/session-notes.md`.
- **Follow-ups** of the latest records in `docs/sessions/`.
- **The last few entries of `docs/friction-log.md`**: a gap named there
  and not yet on the roadmap.

Skip anything CLAUDE.md lists as not wanted.

## 2. Offer them

Ask with AskUserQuestion: two to four options, the one the evidence
favours first, marked "(Recommended)". Each option's label is a few words;
its description is one line with the evidence (the roadmap item, friction
entry or measurement it comes from) and what it costs: an A/B run costs a
few dollars, and per the session notes needs the user's yes anyway. If the
user picks "Other", take their words as the item.

## 3. Start the record

Before reading any code for the item:

1. Branch from an up-to-date main, one branch per item.
2. Create `docs/sessions/<branch>.md` with `create_file`, from the template
   in `docs/sessions/README.md`: the item, where it came from, the date,
   and **the plan**. Name the change at the highest level that captures it
   (session notes, "Reason from the highest abstraction"), then the
   derivation you intend: the operations, in order, that make the change
   easy, and the one small edit that changes behavior. Write the plan from
   names and signatures (`inspect` without bodies, `names`, `uses`); it is
   what the work is judged against, so write it before the bodies.

The hook `cmd/session-log` records every tool call of the session on its
own, to `.claude/session-log/<branch>.jsonl` (gitignored), so do not log
calls by hand.

## 4. Work, and note what the log cannot see

Work as CLAUDE.md says (TDD's order, every edit through gorefactor). The
log shows what happened; it cannot show why. Append one line to the
record's "Notes" with `replace_text` `append` whenever one of these
happens, at the time, not reconstructed at the end:

- `hand:` Go written or changed outside gorefactor, or with a text edit
  (replace_text, replace_statements) on existing code that an operation
  could have moved: what it was, and the compositions of two or three
  tools you tried first and why each failed.
- `refusal:` a gorefactor refusal, and what you did next: another
  operation, a preparing step, undo and reorder, or by hand. Say which.
- `read:` a body you read, and the question it answered. Reading to
  write the new behavior is expected; reading to find uses or check that
  a refactoring is safe is what the tools do for you.
- `plan:` the plan changed, and why.

A line is a sentence. Write it as it is, including when the tools were
skipped because writing it out felt quicker: that is the evidence the
record exists for.

## 5. Finish the record

Before pushing:

1. Append the measurement under "Measured":
   `go run ./cmd/refactor-tokens -log .claude/session-log/<branch>.jsonl`.
   If the log is missing (a session started outside the checkout, or a
   cloud clone using the `refactor` CLI, whose steps it counts but cannot
   measure), say so there; then measure the session's transcript with
   `refactor-tokens` instead, if you can find it.
2. Write "Assessment", a few lines, plainly, including what went badly:
   - **Through the tools or around them:** derived share; writes by hand;
     refusals followed by hand edits; edit steps that rewrote existing Go
     where a refactoring could have carried it.
   - **Did the plan hold?** Which operations it named were used, and what
     replaced the others.
   - **Reading:** denied reads, and bodies read for what the tools would
     have answered.
   - **One change that would have helped:** a tool, an option, a refusal
     that names the next step, or an instruction. Put it on the roadmap or
     in `docs/derivations.md` (if it turned out to be a composition), as
     CLAUDE.md says, and link it here.
3. "Follow-ups": what is left of the item, for the next "what's next?".
4. Commit the record with the work, in the same PR. The friction-log entry
   is still written as CLAUDE.md asks; it may point to the record.

## What "working around the tools" looks like

Use these when writing notes and the assessment; they are what the user
asked the record to show.

- **Embracing:** the change was planned as operations before bodies were
  read; refactorings carried it to every use; the only text written was
  the new behavior; a refusal was answered with another operation or a
  preparing step, or with undo and a different order.
- **Working around:** Edit, Write or a shell command changing files; a
  refusal followed by a hand edit that makes the same change; replace_text
  or replace_statements rewriting existing code an operation could have
  moved (the same edit at many sites is the clearest sign); reading whole
  files or many bodies before planning; repeated attempts at reads the
  hook denies.
