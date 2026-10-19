# Smaller results: a summary instead of the diff

Every applied step returns `APPLIED` and its diff. Results are already
smaller than a plain unified diff: compact diffs have no context lines
(`-full-diffs` turns them back on), a change that is the same at every
site (`rename`) lists where it happened instead of the diff (`Locations`),
and `create_file` and `add_declaration` leave out code the model just
sent (`Supplied`). `batch` takes `report: "summary"`.

The question here is whether to go further, sending a short summary in
place of the diff for most steps, and how to find out whether that saves
anything. Roadmap item 12 in the README is the test.

## Why the size of a result matters

A tool result stays in the context for the rest of the session, so a
2,000-token diff is sent again with every later request. With prompt
caching those repeats are cheaper, but not free, and a long session of
small steps pays for each diff many times. `refactor-tokens` already
counts this: a tool's `ContextTokens` are its arguments and results once
per later request that sent them.

## What the model uses the diff for

A summary is only cheaper if the model does not have to get the same
facts another way. The diff tells it:

- **What the tool chose.** `extract_function` picks parameters, results
  and their order; `inline_function` picks which parameters to declare;
  `rename` also renames doc comments; edits add and remove imports. The
  `chose:` lines and notes say some of this; the diff shows the rest.
- **Where things are now.** The new lines and names the next step
  addresses. Without them the model calls `inspect` again, which often
  costs more than the diff did.
- **How wide the change was.** Call sites changed in other files, which
  matter for the next step and for the commit message.

## The proposal: a diff only where the result is not predictable

Send a summary when the result follows from the arguments, and the diff
when the tool chose something:

- **Summary:** steps whose result the model could write itself:
  `move_parameter`, `remove_parameter`, `replace_text`, and edits whose
  content the model supplied (`replace_statements`, `insert_statements`,
  `add_field`, `replace_expression`). For example: "replaced lines
  40–52 of calc/calc.go with 9 lines; imports added: strings".
- **Diff:** steps that choose: `extract_function`, `inline_function`,
  `invert_if`, `create_from_usage`, any result with `chose:` lines, and
  any step that changed more places than the model named (an
  `add_parameter` updating calls in other files).
- **Always:** where the result landed ("new function at calc.go:88–104",
  "now lines 40–48"), so the next step needs no `inspect`, and every
  note, refusal and type error, as now.

A step can say which it is: a `Plan` field, like `Supplied` and
`Locations`, that marks its result as predictable from its arguments.

## The test

Add a server flag, `-results summary`, beside `-full-diffs`, and an A/B
arm that runs the mcp arm with it ("mcp-summary"). Run the same tasks in
both arms, several runs each, and compare:

- **Cost:** total tokens, and the tool results' share of them
  (`ResultTokens`, `ContextTokens` by tool);
- **The hidden cost of a shorter result:** `inspect` calls after an
  applied step, and Bash reads of the files it changed;
- **Missed changes:** undos, refused steps, failed checks and tests at the
  end, and changes in the final diff the task did not ask for;
- **Review size:** the size of the final diff, which should not change.

Before building the flag, measure how much there is to win: run
`refactor-tokens` on recent sessions of this project and see what share
of the tokens are results of applied steps. If it is small (a few
percent), the test is not worth running, and that number goes in the
friction log instead.

## What would count as a result

- **Worth keeping:** total tokens down, with no more `inspect` calls,
  undos or failures than the mcp arm.
- **Not worth it:** tokens down but `inspect` calls up by about as much,
  or more undos and failed checks: the model was relying on the diff.
- **Mixed:** keep the summary for the steps where follow-up reads did not
  rise, and the diff for the others; the per-step `Plan` field allows
  that.
