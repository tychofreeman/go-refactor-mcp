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

## Results (Oct 23)

`eval-runs/level-{control,design,level}`, Sonnet, 2 runs per task and
arm, $3.41 in all (about $0.14 a run). 23 of 24 passed; the one that
did not (design, notify-optout run 2) never started: the `claude`
binary failed to exec ("exec format error"), a harness failure.

| task | control | design | level |
|---|---|---|---|
| design-tangled | 0/2 refactored: one `else if` | 0/2: "the existing structure already makes this easy" | 2/2 refactored: `Total`'s two chains extracted into `linePrice` and `tax`/`taxFor`, then the branch |
| design-duplication | 2/2 extracted the shared filter and sort | 2/2 the same | 2/2 the same, the concept named first in writing ("which orders get exported, and in what order") |
| notify-optout | 0/2 refactored | 0/1 | 0/2; one said first "Broadcast is the only place that decides which channels a user gets, so the change goes there directly" |
| tp-lazygit-stack-order | 2/2 one new `stack_order.go` used by both loaders, nothing refactored | 2/2 the same | 2/2 the same; neither wrote a concept before editing |

- **The level question moved the one task the others left alone.** On
  design-tangled it was the only arm to refactor, and one run wrote
  the concept as asked: "a customer kind drives two rules, unit price
  and tax, each in its own if-chain in `Total`". It gave each **rule**
  a home, not each **kind**: two functions that still tell every kind
  apart, and the partner went in as one more branch of one of them.
  Function level by the reference. The concept was named right; the
  axis chosen was the one the code is already cut along, as in the
  `fieldtype.go` probe (#297), where runs extracted `narrows` and
  kept the flags.
- **Where function level is design level, every arm got there.**
  design-duplication's rule had no home and all six runs gave it one;
  the question only made the concept explicit.
- **No false positives.** No arm refactored on the control.
- **On code no run had seen, the question was skipped.** Both level
  runs on lazygit wrote no concept and refactored nothing first; one
  also skipped writing a test first. All six runs found one home for
  the new rule on their own (a file both loaders call), which is
  the PR's end state without its preparatory moves. The instruction
  competes with the size of the task: on 300 lines the model followed
  it, on lazygit it went straight to the change.
- **Cost:** the same in every arm ($1.01-1.21 an arm), within noise
  at this size.

So naming the level makes the model state the concept and refactor
where the generic question did not, but not choose the axis the
change runs along: "a new kind" was in the question, and the runs
still homed the rules. What could be tried next: name the axis ("the
change adds an instance of something: give *that* a home, so that the
new instance is one new place"), and make the concept a step of its
own, so that a large task cannot skip it.

A tool gap the runs showed: in design-duplication `fold` would not
fold JSON's copy onto the extracted function (it writes the filter as
`!= order.Pending` around the append, CSV as `== ... continue`), so
all six runs replaced the copies with `replace_statements`, an
edit, where a refactoring should reach.
