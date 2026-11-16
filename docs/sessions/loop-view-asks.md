# The loop on prompts that name no code

- **Item:** roadmap 29's untested half after cost-history 28: the
  focused view takes its names from the prompt, so a prompt that
  describes behavior without naming code gets only the index, and
  `-no-read` gives the plan no way to ask for more. Fourth of the four
  items chosen from "what's next?" on 2026-11-16.
- **Date:** 2026-11-16
- **Branch:** loop-view-asks

## Plan

(Written after the code, not before: this item is Python in
`eval-tasks/learning/`, planned from the loop's ask handling, which an
earlier item of the session had read.)

The change: let the plan ask for what the focused view leaves out, as
it asks for tool entries with `-index`, and give it tasks whose prompts
name no code they change, to see whether it asks and still derives.

1. `probe.view_of(module, names)`: the call tree's entries for a
   package, a type with its methods, a function, `Type.Method` or
   `pkg.Name`, and a line for names the module lacks.
2. `loop.py -view-asks N` (with `-view focused`): the head says the plan
   may ask `{"view": [...]}` N times; the answer is `view_of` on the
   module as it is; the row counts `view_asks`.
3. `eval-tasks/derivations-freight-xxl-unnamed.json`: freight-xxl's four
   tasks with prompts that name only new code (and, for run, the types
   its new struct holds), the same hidden tests, copies of the reference
   batches.
4. A run (the user's yes first), then its record.

## Notes

- read: loop.py's ask handling and flags (sed, as before in the
  session), to put the new ask beside the tools ask.

- plan: step 4's run used three views (whole call tree, focused,
  focused with asks), the user having asked for the call tree too.

## Measured

`go run ./cmd/refactor-tokens -log .claude/session-log/loop-view-asks.jsonl`:
35 tool calls; 0 gorefactor steps on Go (this item changed Python and
JSON, through replace_text, create_file and move with copy); 8 Bash
calls the log counts as by hand, none of them writing the repo's code:
the three runs, the summaries, and the merges of #662 and #663 (whose
conflicts were resolved with replace_text). The run: $11.06
(cost-history 29).

## Assessment

- **Through the tools or around them:** through them for every file of
  the repo; the plan was written after the code, which the record says.
- **Did the plan hold?** Yes; the run had a third view.
- **What it showed:** a plan given only an index asks when it may, and
  then does what it does with the names in the prompt; without asks it
  guesses and fails a tenth of the attempts.

## Follow-ups

- The other session's next steps (Claude Code sessions on freight-xxl,
  by hand and with the tools; then a session that works as the loop
  does) are with the user for a yes.
