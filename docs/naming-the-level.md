# Naming the level

Whether telling a fresh model at which level to look for what makes a
change hard makes it refactor at the level of the design, not of the
lines the change passes through (session notes, "Would a fresh model
recognize it?"). Asked "what would make the next change hard?" about a
file alone, Sonnet named the type code in `fieldChange`; asked the same
on the way to a feature, it extracted the condition the feature passed
through and kept the flags (#297).

## The question

Written before the tasks were chosen, and not changed per task. It
names a level (a concept and where it is known), not a smell, so that
it fits a type code, a repeated rule and data with no type alike:

> Before you plan a change, say what it does to the concepts the code
> represents, not to its lines: a new kind of something, a new rule, a
> new piece of data. Write down that concept and every place that
> already knows about its existing instances (inspect's `names` and
> `uses` find them). If the concept has no single home, because the
> code tells its instances apart or repeats its rule in several places,
> first give it one with refactorings, keeping the tests passing; then
> make the change there. If it already has a home, make the change
> directly.

It asks for written output (the concept, the places) rather than
reflection: asking explorers to consider ways did nothing
(session notes, "Experience for a fresh model").

## Arms

All three are the hybrid arm (refactorings through gorefactor, the
change by Edit), each with a CLAUDE.md that adds to
`claude-md-refactor-when-needed`:

- `control`: `eval-tasks/claude-md-refactor-when-needed`, as is.
- `design`: `eval-tasks/claude-md-design-question`, plus the question
  of #297 ("ask what about the current design would make it hard").
- `level`: `eval-tasks/claude-md-level-question`, plus the question
  above.

## Tasks and what each run is graded against

`eval-tasks/level-question.json`. The reference concept for each was
written here before the runs.

- `design-tangled` (shop): adds a customer kind. The concept is the
  customer kind, told apart by `o.CustomerKind == "..."` chains in two
  places of `pricing.Total` (unit price and discounts, then tax). Its
  home: one place per kind that holds its rules (a type per kind, or a
  table of rules keyed by kind). Extracting each chain into a function
  keeps two places that know every kind: function level.
- `design-duplication` (shop): changes which orders are exported and
  their order. The concept is the rule for which orders an export
  lists and in what order, repeated in CSV, JSON and Markdown
  (`Status == order.Pending` and `sort.Slice` by ID in each). Its home:
  one function the three call. Here function level is design level.
- `notify-optout` (notify): the negative control. Adds data and one
  condition in `Broadcast`, the one place that sends; nothing to give
  a home. A refactoring here is a false positive.
- `tp-lazygit-stack-order` (lazygit at the PR's parent, code no run
  has seen): a new rule for ordering refs, for local and remote
  branches. The PR (#6024) made the change easy first: it moved the
  ahead-behind `for-each-ref` helpers to their own file, let the
  caller choose the refs, gave `RemoteLoader` its `GitCommon`, loaded
  remote branches as one list grouped by remote afterwards, and then
  added one ordering (`ref_ancestry.go`) used by both loaders. Design
  level: one ordering both loaders use; below it, the rule written
  into each loader.

## Measures

Per run: the concept the run names (from its text before the first
edit), whether it refactored first and at which level (design,
function, none), a refactoring on the control, passed, cost.

## Results

Pending.
