# Can an exploring model teach later ones?

Roadmap item 21's exploration runs. The question (the user, 2026-10-21):
if a model is told to try several approaches when one fails, can what it
learns make later models more effective, even if it costs more itself?
A later model gets nothing from an earlier run unless something carries
it across, so the test is of what the explorer writes down: lessons,
distilled into recipes, given to fresh models on other code.

What earlier runs suggest: recipes tied to a situation changed how the
model worked where they fit (cost-history section 8, the user's moves in
`eval-tasks/claude-md-derivations`); general advice did not (#119, longer
descriptions). So lessons are kept only when they name a situation and
an operation, and when a transcript shows them working.

## Two task sets with the same shapes

- **A, the explorer's:** `eval-tasks/derivations-large.json`, the five
  derivation problems on `testdata/notify-large`.
- **B, the learners':** `eval-tasks/derivations-billing.json`, the same
  five shapes on `testdata/billing-large` (accounts charged through
  payment methods): merge four near-duplicate methods into `book`
  (shared-record), skip blocked methods in a loop (optout), two fields
  become a map (contacts), a helper used by one type becomes its method
  `tail` (fit), a parameter object `Run` (batch). Names, domain and
  details differ (`last` keeps the end of a string where `truncate` keeps
  the start; a bank charge appends the amount), so a recipe can carry the
  technique across but not the answer. Each hidden test fails on the
  module as given and passes with a reference solution (checked when the
  set was made).

## The runs

1. **Explore** on A, with `eval-tasks/claude-md-explorer`: the base
   (`claude-md-refactor-first-inspect`) and a section asking the model
   to try two other ways before writing code by hand after a refusal,
   and to end with a `## Lessons` section for sessions on other code.
   It is not given the user's moves, so what it teaches is its own.

       go run ./cmd/refactor-ab -tasks eval-tasks/derivations-large.json \
         -model sonnet -arms mcp -runs 6 \
         -claude-md eval-tasks/claude-md-explorer -out eval-runs/explore

2. **Distill:** `python3 eval-tasks/learning/lessons.py eval-runs/explore`
   prints each run's lessons with its result and cost. Keep a lesson when
   it comes from a passing run and its transcript shows the operation
   going through (`refactor-tokens` lists the derived steps); merge
   duplicates; drop notify's names; at most about a dozen bullets. They
   go into `eval-tasks/claude-md-learned/mcp.md`: the base and a
   `## Lessons from earlier sessions` section.

3. **Learn** on B, three arms of 6 runs per task:
   - `claude-md-refactor-first-inspect`, the baseline;
   - `claude-md-learned`, the baseline and the distilled lessons;
   - `claude-md-derivations`, the user's moves (taught by a person).

       go run ./cmd/refactor-ab -tasks eval-tasks/derivations-billing.json \
         -model sonnet -arms mcp -runs 6 \
         -claude-md eval-tasks/claude-md-learned -out eval-runs/learn-learned

Measure: pass rate, cost, turns, the derived share and the size of the
manual edits (`docs/derivations.md`, the user's principle), per task and
in total. The explorer's extra cost is a one-time cost: the question is
whether the learned arm beats the baseline on B, and how it compares with
the user's moves.

Cost, from `large6-*` (about $3.60 for 30 runs): the explore run perhaps
twice that, the three learner arms about $11; about $18 in all.

## Results

Not run yet.
