# A/B of the descriptions without refusal lists

- **Item:** the A/B check #595 left open. The user, 2026-11-06: "Go ahead
  with the a/b check".
- **Date:** 2026-11-06
- **Branch:** ab-descriptions

## Plan

The question: with refusal lists gone from the descriptions, does a fresh
model refuse more, or cost more? The server at #596's head (all of
#592-#596) against main, on the four feature tasks of the usual mcp run
and the four tasks of `ab-merged-tools.md`, whose main runs serve as the
baseline. Sonnet, arm mcp, 2 runs each, one set at a time.

## Notes

- plan: the container restarted during main's feature runs; the five
  finished results were kept and the three missing runs rerun into
  `desc-main-feat-b` and `-c`.

## Measured

No session log (a cloud session started outside the checkout); no Go was
changed. Results in `docs/cost-history.md`, section 25: 16/16 passed
against 15/16, refusals 17 of 184 calls against 19 of 209, cost +6%.

## Assessment

The change holds: a model told less in advance did not refuse more, and
the refusals it met told it what to do. The cost difference is within
the noise of 2 runs a task. The descriptions' published size fell by
about 9%, a small part of a session's input, so no saving shows either.

## Follow-ups

- The schemas' repeated argument text, if the user wants it shorter.
