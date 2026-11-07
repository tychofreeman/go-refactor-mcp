# Tool descriptions without refusal lists

- **Item:** java-refactor-mcp's #256-#258 for Go and TypeScript. The
  user, 2026-11-06: "same as Java (no refusal lists and shared text in
  instructions). Go ahead and overturn the existing rule."
- **Date:** 2026-11-06
- **Branch:** descriptions-without-refusals

## Plan

At the highest level: a description says what a tool does (its
before/after and addressing hints), not what it refuses; each refusal
says why and what to do instead; what every tool shares is said once, in
the instructions. All text, so replace_text on the descriptions' string
literals, in batches checked at the end.

- Remove every "Refuses, saying why: ..." paragraph and the refusal
  sentences elsewhere ("Refusals say which rule...", "The refusal shows
  the first difference", "is refused with the compiler's error", the
  move_statement paragraph that opens "Behavior does not change").
- Remove ": an edit" (the instructions sort the tools) and "packages it
  names are imported" from the edits; the Go instructions say once that
  edits import what they name.
- `preserve_exceptions`: one short line, as java's #258.
- The session note on descriptions replaced.

## Notes

- plan: kept what is behavior, not refusal: inline_function's account of
  early returns, extract_struct's API note, replace_declaration's "fixed
  in the same batch with check end", inline's `elsewhere` option.
- plan: the Go instructions went over their 6,000-character budget with
  a sentence of their own on refusals; folded into step 3's "REFUSED
  with why and what to do instead (descriptions do not list refusals)".

## Measured

No session log (a cloud session started outside the checkout). Through
the CLI: 81 replace_text steps in four batches (one step with `files`
over 15 files), all on string literals and docs; nothing refused.

Published, as JSON (initialize and tools/list against the server):

| | before | after |
|---|---|---|
| Go tools | 93,886 | 85,279 |
| Go instructions | 5,934 | 5,995 |
| TypeScript tools | 80,803 | 73,454 |
| TypeScript instructions | 5,587 | 5,604 |

## Assessment

All through replace_text on text, which is what it is for. Less was cut
than in Java (58,000 to 42,400) because most of the rest is schema
argument text, which this item did not touch. The descriptions now
differ from what the A/B baselines saw.

## Follow-ups

- An A/B check of the new descriptions (ask first).
- The schemas' repeated argument text (`column`, `line`, `file`), if the
  user wants it shorter too.
