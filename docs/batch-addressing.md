# Batch addressing: options

A step of a batch addresses lines as they were when the batch began, and
`RunBatch` moves each to where that line is after the earlier steps
(`translateLines`). A line an earlier step rewrote or removed, and any line
of a file created in the batch, cannot be addressed: the refusal says to
address what that step made by name, or to send the step in a later call
after a new `inspect`.

So the most common follow-up has no address: "after what the previous step
made". The friction log has it twice (2026-10-18: line numbers in a batch,
a test body replaced after a helper was added; 2026-10-19: locals by
function and name, an `inline_variable` that could not follow the
`introduce_parameter` that rewrote its line). Addressing by name
(`function`, `after` in `add_declaration`) covers declarations and locals,
but not statements.

This file collects ways to address what earlier steps of a batch changed,
from the most specific to the most general. Roadmap item 10 in the README
takes option 3, with ranges that follow later steps as option 5's marks
do; `internal/refactor/batchref.go` implements it.

## What a step's result is

Each option needs the range of lines a step left in a file. `RunBatch`
already has what it takes: `ws.LastChange()` gives each file before and
after, and `edit.LineMap` tells which lines are new. A step's range in a
file is its new lines, from the first to the last, as they are now:

- an insertion (`insert_statements`, `add_declaration`, `add_field`): the
  inserted lines;
- a replacement (`replace_statements`, `replace_expression`): the lines
  that replaced the old ones;
- a deletion (`remove_dead_code`, an inlined variable's declaration): no
  lines, only a point, the line after the deleted ones;
- a file it created: the whole file.

A step that changes several places in one file (`rename`, `add_parameter`
at every call) has several ranges, so a reference to it is refused unless
it names which one; most uses follow an edit that changes one place.

## 1. One special address

A reserved value for a step's `line`, such as `"line": "$prev.end"`:
the last line of the previous step's range in the step's `file`.
`insert_statements` with `where: "after"` then inserts right after what
the previous step inserted.

- Small: one case in `translateLines`.
- The next needs come straight after: the start, before rather than
  after, two steps back.

## 2. Back-references with accessors

The previous step's range as a value: `$prev` (the range, for
`start_line`/`end_line`), `$prev.start`, `$prev.end`, with offsets:
`$prev.end+1`, `$prev.start-2`.

- `start_line: "$prev.start", end_line: "$prev.end"` replaces what the
  previous step wrote, so a wrong guess can be fixed in the same batch.
- Reaches one step back only.

## 3. Results by step number or id

Any step may take an `id` (`"id": "helper"`), and a later step may
address its range as `$helper.start` or `$helper.end`, or by position
(`$2.end`, the second step); `$prev` is the step just before. Offsets as
in option 2.

- The most general: several steps can build on one (three insertions
  after the same new helper).
- `id` is a batch field beside `tool` and `args`, so no tool's schema
  changes; `RunBatch` resolves the references before the tool sees its
  arguments, as it translates line numbers now.

## 4. An implicit cursor

As in `ed`, the batch keeps a current line: the end of the last step's
range. A step that gives no line works there; `.` and `.+1` name it.

- The tersest form for a run of insertions.
- State the model has to track in its head; a step that leaves out `line`
  by mistake is no longer refused.

## 5. Marks

A step may set a mark on its range (`"mark": "x"`), and later steps
address `'x` or `$x.end`. The mark moves with the lines, as `lineMap`
moves the batch's starting lines, so it stays valid however later steps
change the file.

- Option 3 with ranges that follow later edits: `$helper.end` still means
  the helper's end after a step inserted above it.
- Within one batch the same as option 3 resolved when used; it differs
  only if marks outlive the batch (across calls, cleared by `inspect`).

## 6. Search from a reference

A reference plus a pattern: the first line after `$prev.end` that starts
a statement matching some text, or the `return` of the function the
previous step changed.

- For when a step's range only comes close: the line after a new `if`,
  inside a function just made.
- Patterns can match more than once or not at all, and each refusal has to
  say which.

## Questions any option must answer

- **When references resolve:** when the step runs, against the files as
  the earlier steps left them, as line numbers are translated now.
- **Which file:** a reference is to the step's range in the file the
  addressing step names; a step that did not change that file has none
  there, and the reference is refused.
- **Steps with several ranges or none:** several are refused, saying how
  many and where. A deletion gives a point, the line after it, so
  `$prev.start` and `$prev.end` are both that line.
- **Refused steps:** a batch stops at the first refusal, so a reference
  only ever names a step that applied.
- **Undo:** a batch is one change on the undo stack, so references never
  outlive it (unless marks do, option 5).
