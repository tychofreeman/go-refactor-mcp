# Working algebraically

The user's thesis (2026-10-20): source code is a structure to be
manipulated by operations that respect type checking and syntax, much
as an algebraic expression is rewritten by rules, and almost any
desired shape of the code can be reached by such operations, with only
small edits at the leaves. Working that way lets a person think at a
higher level than keywords and syntax, and should let the model do so
too. This page is how we measure how algebraically a run worked.

## The measure

`refactor-tokens` (`internal/tokens`) reports, for the gorefactor calls
that applied:

- **written:** lines of code the model supplied in arguments (`content`,
  `new`, `field`, `value`, `type`, also inside a batch's steps);
- **changed:** lines the results report as changed: for each diff hunk,
  the larger of the lines removed and added; for a list of locations (a
  rename's or a family change's), the lines it names;
- **derived:** changed lines beyond those written, call by call (never
  below 0); the **derived share** is derived over changed;
- by kind of call: refactorings only, edits only, or a batch of both;
  and the steps of each kind.

Approximations:

- Only Go files count: text written into README files and docs is not
  code, though it goes through replace_text.
- A call whose result shows no diff (a batch with `report: "summary"`)
  counts as written but unmeasured, and stays out of the share.
- Written lines are counted from the arguments, so code the model sends
  twice (a step refused and sent again) counts once per applied call
  only; code moved by `replace_statements` (written again elsewhere)
  counts as written, which is what it was.
- Only gorefactor calls are seen. In the hybrid arm, edits made with
  Edit, Write or Bash count as neither, so its share is that of its
  gorefactor calls alone.

## Baseline (Oct 20)

| Run (arm) | written | changed | derived share | steps: refactorings / edits |
|---|---|---|---|---|
| mech-before (mcp) | 549 | 555 | 11% | 2 / 88 |
| mech-now (mcp) | 606 | 585 | 12% | 12 / 69 |
| tp-mcp-refactor-first (mcp) | 428 | 379 | 9% | 1 / 24 |
| tp-mcp-type-errors (mcp) | 813 | 711 | 5% | 0 / 39 |
| ab-features-denied (mcp) | 750 | 789 | 13% | 3 / 60 |
| ab-features-type-errors (mcp) | 797 | 818 | 11% | 4 / 60 |
| tp-first (hybrid, gorefactor calls only) | 58 | 62 | 6% | 0 / 2 |
| ab-chain-fixes (hybrid, gorefactor calls only) | 136 | 268 | 60% | 16 / 19 |
| ab-design-refactor-first (hybrid, gorefactor calls only) | 83 | 432 | 85% | 28 / 11 |
| this project's own sessions (10) | 16942 | 10664 | 33% | 83 / 1055 |

A fresh model in the mcp arm writes nearly all of what changes: about
nine in ten changed lines are lines it wrote, through edits, on every
task set, including the ones whose real PRs refactored. The tools carry
its text and check its types; they rarely derive the change. Where the
design was seeded to be in the way (the shop's design and chain tasks),
the hybrid arm's gorefactor calls were mostly refactorings and derived
60-85% of what they changed; there the model did reach for operations.
This project's own sessions sit between (a third derived), with far
more edits than refactorings. So the gap the thesis points at is real
and measurable: the next step is to see whether a model taught to plan
changes as derivations raises the share, and what that does to cost,
errors and the code it leaves.
