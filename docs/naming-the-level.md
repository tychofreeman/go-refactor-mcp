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

## The axis, and the concept as a step of its own (Oct 23)

The question again, naming the axis: what the change adds a new
instance of (a kind, case, channel) or which rule it changes, and
"the change is easy when the new instance is one new place (one new
type, function, case or entry that holds everything about it), or the
rule is in one place". The same words two ways:

- `axis`: in CLAUDE.md (`eval-tasks/claude-md-axis-question`), on
  `level-question.json`, as the level arm was.
- `step`: the control's CLAUDE.md, and each task in two sessions
  (`eval-tasks/level-step.json`). The first is told the next change
  and not to make it, but to write down the concept and its places
  and refactor until the change would be one new place; its check is
  the existing tests. The second, a new session in the code the first
  left, gets the feature prompt alone and the hidden tests.

`eval-runs/level-axis` ($1.17, 8/8) and `eval-runs/level-step` ($1.89
for 16 sessions, 16/16), 2 runs each.

| task | axis | step: preparing session | step: feature session |
|---|---|---|---|
| design-tangled | 0/2 refactored: "one new branch in the price chain" | **2/2 a table of rules per kind** ("a kind is defined by two things, a unit price after discounts and a tax rule"), `Total` looks up the kind's rule | 2/2 "already easy": one price function and one map entry |
| design-duplication | 2/2 the shared function | 2/2 the shared function, "it changes one rule" | 2/2 changed the function |
| notify-optout | 0/2 refactored | 0/2 changed: "Broadcast is the only place" | 2/2 one `if` |
| tp-lazygit-stack-order | 0/2, a new file both loaders call | 0/2 changed: "it changes one rule", "already easy" | 2/2 a new file both loaders call |

- **As its own step, the question reached the design.** Both
  preparing sessions on design-tangled made what the reference names:
  one home per kind, holding its price and its tax. Nothing else
  run in this experiment did; the feature session that followed found
  the change "already easy" and added one entry. This is "make the
  change easy, then make the easy change" with the two halves in
  separate sessions, and it is the first time a fresh model made the
  change easy at the design level here.
- **In CLAUDE.md, naming the axis did less than naming the level.**
  The axis arm did not refactor on design-tangled: a new `else if` is
  "one new case" by the question's own words, and the tax rule already
  sent partners to retail. The wording let the cheapest reading pass.
  The level arm had at least extracted the two rules.
- **No false positives, and lazygit is still read as one rule.** The
  step's preparing sessions changed nothing where nothing was needed,
  and said so with the places they had looked at. On lazygit they
  judged the change one new rule and the code already easy; the PR's
  preparatory moves (the ahead-behind helpers, loading remote
  branches as one list) serve how the rule is computed, which the
  question does not ask about.
- **Cost:** the step arm's two sessions cost $0.11-0.30 a task
  together, about one and a half single sessions; the lazygit
  preparing session cost $0.10 and changed nothing.
- The preparing sessions on design-tangled rewrote `Total` with
  `replace_declaration`, an edit, with the tests as the proof: no
  refactoring makes if-chains on a kind into a table of rules.

What the step does that an instruction does not: the session has
nothing to do but the design question, so it cannot be skipped and
is not answered at the level of the cheapest edit. The next thing to
learn is whether it holds over more runs and other shapes (the
lending kinds, the freight carriers), and whether a step like it
belongs in the tools' instructions or in how a task is given.

## More runs, and two more shapes (Oct 23)

`level-step` and the control again at 5 runs per task, and two new
tasks in `eval-tasks/level-question-2.json` (control) and
`eval-tasks/level-step-2.json` (step), each validated: the hidden
tests fail at the start and pass with a plain solution (a case in
each switch; the rail helpers copied for air). The reference designs,
written before the runs:

- `lending-magazine` (lending-xl): adds a kind. The kind is told apart
  by switches in five functions of `lending` (`LoanDays`, `DailyFee`,
  `Label`, `Shelf`, `Renewable`), and `Import` accepts a kind through
  `LoanDays`; `notices` and `desk` compare with `"atlas"` and `"dvd"`.
  Its home: one place per kind holding all five (a table of kind
  rules, or a type per kind behind an interface), the five functions
  looking it up. Below that: a case in each switch.
- `freight-air` (freight-xl): adds a route to `audit`, which has
  eight helpers written once for Road and again for Rail
  (`LackingRoad`/`LackingRail`, `RoadZone`/`RailZone`, ...). The new
  instance is the route. Its home: one helper per operation that
  takes the route (its code, or a function that reads it), the Road,
  Rail and Air helpers one-line calls of it. Below that: eight
  functions copied for Air.

Results: `eval-runs/level-{step,control}-5` (the first four tasks)
and `eval-runs/level-{step2,control2}-5` (the two new ones), 5 runs
each, Sonnet, 90 sessions, all passed, $11.40.

| task | control: refactored first | step: preparing session | step: feature session |
|---|---|---|---|
| design-tangled | 0/5 (an `else if`) | **5/5 a table of rules per kind** | 5/5 one entry, "no refactoring needed" |
| lending-magazine | 0/5 (a case in each switch) | **5/5 a table of kind rules** the five functions look up | 5/5 one row, "Import accepts it because it checks against the table" |
| freight-air | 0/5 (eight copied functions; one: "there are now three near-identical copies of each check") | **5/5 shared helpers that take a function reading the route's code** | 5/5 one `airCode` selector and eight one-line calls |
| design-duplication | 5/5 the shared function | 5/5 the shared function | 5/5 changed it |
| notify-optout | 0/5 | 0/5 changed nothing | 5/5 one `if` |
| tp-lazygit-stack-order | 0/5 | 0/5 changed nothing, "already easy" | 5/5 one new file both loaders call |

- **It holds, and on shapes it was not tuned to.** On every task
  whose design was in the way at more than one function, the
  preparing session made the reference design in 15 of 15 runs and
  the control in 0 of 15. The feature session then found one place
  to change in all 15, and said so.
- **No false refactoring:** 0 of 10 preparing sessions changed code
  on notify-optout and lazygit.
- **Lazygit stays "already easy"** (10 of 10 preparing sessions).
  Either the question does not reach how a rule is computed, or the
  PR's preparation was a choice, not a need; the hidden tests pass
  either way.
- **The design was made by edits, not refactorings.** Of the 15
  preparing sessions that changed the design, the kind tables were
  written with `replace_declaration`, Edit or Bash (four of the five
  on design-tangled first extracted the two chains with
  `extract_function`), and freight's helpers likewise; only the
  exports were done mostly by refactorings (`extract_function`,
  `fold`, `merge_duplicate_function`, with `replace_statements` where
  `fold` did not reach). The tests were the proof that nothing
  changed. The tools have no operation for "switches on a
  kind into a table" or "functions that differ in a field into one
  that takes a selector" (`parameterize` takes constants).
- **Cost:** the feature session alone costs what the control does
  ($0.09-0.11 against $0.10-0.11 on the three design tasks); the
  preparing session is on top, so a task costs 1.5-2.6 times as
  much. It can only pay back over later changes to the same code,
  which roadmap item 26 measures next.

## Does it pay back? (Oct 23)

Written before the run. The preparing session costs 1.5-2.6 times a
task; the question is whether a second change to the same code is
cheaper, or better made, after it. `eval-tasks/level-payback.json`,
8 runs per chain, Sonnet, the hybrid arm with
`claude-md-refactor-when-needed`:

- `payback-control`: `chain-tangled` as before (partner, then
  student, each a session).
- `payback-step`: the preparing session of `design-tangled`, then
  partner, then student. Student is given plainly, with no second
  preparing session: what is measured is whether the first one
  carries over.

What counts: the student session's cost and input tokens in each
chain, whether it passes, and where it changes the code (one entry,
or a branch in each place). The chain's total says whether the
preparation is paid back by its second change; at 8 runs, a
difference under about 15% is noise.

Results: `eval-runs/level-payback-8`, 40 sessions, $4.08. Step run 1
is left out: its preparing and partner sessions failed in the harness
(`exec: "claude": executable file not found`, the CLI was being
updated), so its student session ran on the original code without
partner and failed. Every other session passed.

| session | control (8 runs) | step (7 runs) |
|---|---|---|
| preparing | - | $0.148 · 253K |
| partner | $0.097 · 141K | $0.096 · 161K |
| student | $0.106 · 202K, 6-18 turns | $0.094 · 158K, 9-11 turns |
| chain | $0.203 | $0.338 |

- **The second change is better made, a little cheaper, and steadier.**
  Every step run added student as one entry in the per-kind table
  (with a `studentPrice` helper over the retail price in some). The
  control runs added an `if` inside the retail branch and put student
  into the staff tax condition (`"staff" || "student"`), 7 of 8 with
  no refactoring; one extracted `retailPrice` first. The step's
  student session cost 11% less and read 22% fewer input tokens, and
  its spread was a third of the control's (sd $0.007 against $0.020).
  The cost difference is under the 15% set above as noise; the
  tokens and the spread are over it.
- **It does not pay back over one more change on this module.** The
  preparing session costs $0.148; the student session saved $0.012.
  At that rate it would take about a dozen later changes to the same
  code. The module is small: the control's if-chain is cheap to
  extend, so what the step buys here is the design (each kind in one
  place, no kind sharing another's tax branch), not tokens.
- So the case for the step rests on code where extending the tangle
  costs more per change (more places, more uses), as `notify-large`
  and `freight-xl` showed for derivations, or on the design itself.
  Not measured: a second change on `lending-magazine` (five switches)
  or `freight-air` (eight helpers), where the control's copy-and-edit
  grows with each new instance.

## The second change on lending and freight (Oct 23)

Written before the run. On design-tangled the control's if-chain was
cheap to extend, so the step bought the design but not tokens. Here
the control's way grows with each instance: a case in each of five
switches (lending), eight copied functions (freight).
`eval-tasks/level-payback-2.json`, 8 runs per chain:

- `lending-magazine`, then a "map" kind (7 days, 25 cents, "Map: "
  label, the "reference" shelf, renewable; hidden test
  `testdata/lending-map`).
- `freight-air`, then the same eight checks for the Sea code (hidden
  test `testdata/freight-sea`).

Each shape as a control chain (magazine or air, then the second
change) and a step chain (the preparing session, then the two). Both
hidden tests were checked first in a scratch copy: they fail after a
plain first change and pass after a plain second one.

What counts, as before: the second session's cost, input tokens and
turns in each chain, and where it changes the code (one entry or
selector, or a case or copy at each place). For the step to pay back
in tokens, the second change's saving must exceed the preparing
session's cost; noise is about 15%.

Results: `eval-runs/level-payback2-8`, 80 sessions, all passed, $8.83.
Means per session, cost · input tokens · lines changed:

| session | lending control | lending step | freight control | freight step |
|---|---|---|---|---|
| preparing | - | $0.134 · 191K · 69 | - | $0.169 · 205K · 124 |
| first (magazine / Air) | $0.106 · 139K · 21 | $0.100 · 178K · 18 | $0.108 · 118K · 89 | $0.102 · 112K · 57 |
| second (map / Sea) | $0.087 · 116K · 31 | $0.097 · 158K · 23 | $0.100 · 130K · 96 | $0.103 · 146K · 64 |
| chain | $0.193 | $0.331 | $0.208 | $0.374 |

- **The design carries over.** After the step, every second session
  added one row to the kind table (lending, 8/8) or one accessor and
  eight one-line calls to the shared helpers (freight, 8/8), and said
  the code already made it one place. Without it, every second
  session added a case to each switch or copied the eight Air
  functions with `Sea` for `Air`, with no refactoring (16/16).
- **It costs no less.** The second change cost 11% more after the
  step on lending and the same on freight, with 15-36% more input
  tokens: the sessions after the step read the table or helpers
  before adding to them, where a session in the copies' code copied
  the nearest instance. It wrote a quarter to a third fewer lines.
- **So it does not pay back in tokens over two changes, on any of the
  three shapes.** Writing the fifth case of a switch or the third
  copy of eight functions is cheap for the model, as cheap as adding
  a row; what grows with each instance is the code, not the cost of
  extending it. What the step buys is the design: one place per
  instance, and fewer lines. Whether that turns into cost needs
  changes that cut across the instances (a new rule for every kind,
  a bug in every copy), where the copies must each be changed and the
  table or helper once.

## Longer chains (Oct 23)

Written before the run. Two changes did not pay back on any shape,
and the cost of extending the copies did not grow with the
instances. The user asked for longer chains per project. Each chain
now has five changes, mixing new instances with changes that cut
across them (a new rule for every kind, a fix in every copy), where
the copies must each be changed and a table or helper once.
`eval-tasks/level-long.json`, 5 runs per chain:

| project | 1 | 2 | 3 | 4 | 5 |
|---|---|---|---|---|---|
| pricing (`shop`) | partner kind | student kind | `Shipping`, a rule per kind that needs the subtotal | reseller kind | `Invoiced`, a rule per kind |
| lending | magazine kind | map kind | `MaxRenewals` per kind, `Renewable` follows it | ebook kind | `Deposit` per kind |
| freight | Air checks | Sea checks | zones trim the code, in every copy | a `Valid` check per code | lines trim the code, in every copy |

The control chain makes the five changes, each in its own session;
the step chain has the preparing session for change 1 first, and no
other. Each new hidden test (`testdata/shop-*`, `lending-renewals`,
`-ebook`, `-deposit`, `freight-zone-trim`, `-valid`, `-line-trim`)
was checked in a scratch copy: it fails before its change and passes
after a plain one, with every earlier stage's tests still passing.

What counts: each change's cost in both chains, the chain's total,
and whether the step chain's later sessions still find one place to
change (and whether a cross-cutting change is one place there and
several in the control).

Results: `eval-runs/level-long-5`, 165 sessions, $16.23. Two
tangled-control runs are left out: the harness lost sessions to
`exec: "claude": executable file not found` (as in
`level-payback-8`), and the later sessions of a chain that lost one
fail the earlier stage's tests. One real failure: freight's `Valid`
prompt did not name package `audit`, and one control session put the
four functions in `manifests`, which has its own per-code copies
(the prompt's fault).

Mean cost per change (pricing control: 3 runs; the rest 5):

| change | pricing control | pricing step | lending control | lending step | freight control | freight step |
|---|---|---|---|---|---|---|
| preparing | - | $0.136 | - | $0.124 | - | $0.181 |
| 1 | $0.094 | $0.095 | $0.098 | $0.088 | $0.111 | $0.105 |
| 2 | $0.112 | $0.089 | $0.098 | $0.091 | $0.100 | $0.095 |
| 3 | $0.108 | $0.114 | $0.082 | $0.097 | $0.099 | $0.087 |
| 4 | $0.073 | $0.099 | $0.090 | $0.084 | $0.105 | $0.112 |
| 5 | $0.069 | $0.087 | $0.084 | $0.092 | $0.120 | $0.114 |
| changes 1-5 | $0.455 | $0.484 | $0.451 | $0.452 | $0.535 | $0.513 |
| chain | $0.455 | $0.620 | $0.451 | $0.577 | $0.535 | $0.694 |

Where changes 3-5 went (a subagent graded every session from its
edit calls; two spot-checked):

- **Lending: the table held, 15 of 15.** A new rule per kind
  (`MaxRenewals`, `Deposit`) was a table field after the step, and a
  new switch in the control (10 of 10: the kind knowledge spreads);
  the ebook kind was one row against a case in each of five switches.
- **Pricing: the table did not hold.** At `Shipping`, 4 of 5 step
  sessions coded the kinds outside the table (a switch, or table
  membership `&& kind != "student"`); two of them then changed two
  places for reseller, and at `Invoiced` one more bypassed it and
  said a new kind would miss it. One run kept the design through all
  five. The control added new functions with their own kind tests,
  and reseller as a branch in both if-chains.
- **Freight: the control refactored on its own.** At both trims, 10
  of 10 control sessions first extracted a shared helper over the
  four copies and then changed it once; after the step, one change
  in the existing helper. For `Valid` both arms wrote a helper and
  four wrappers. Neither design reached `manifests`' own
  `RoadLine`/`RailLine`, which 3 of 10 line-trim sessions changed one
  by one.
- **Cost over five changes: no pay back.** After the step, changes
  1-5 cost what the control's did (-4% to +6%); the preparing session
  is never recovered (chains 26-36% dearer). Lines changed are fewer
  after the step on lending and freight.

So the step buys a design a fresh model then mostly keeps (lending),
or partly erodes (pricing), and where copies line up the model
refactors without being asked once a change cuts across them
(freight). In tokens, a fresh model extends copies or switches as
cheaply as a table, at these sizes. What it would take to pay: code
where reading and changing each copy costs much more than here, or
a measure other than one session's tokens (defects such as the
`manifests` copies, how far kind knowledge spreads).
