# fold of a small body without line

- **Item:** issue #714: TypeScript fold of `return "";` or `return
  operator + held;` without `line` rewrote every literal or string
  concatenation of the file, each keeping behavior, so nothing said so.
- **Date:** 2026-11-20
- **Branch:** ts-fold-small-body

## Plan

Of the issue's three suggestions (list the matches, an `in_function`
argument, a sentence in the description), the one that needs no new
argument and that a model cannot miss: refuse, listing the matches.
- **The easy change:** before matches become calls, when no `line` was
  given, the body is an expression `slight` (a literal or name, or one
  operator or index on those), and it matched more than once, refuse
  with each match's place and code.
- **The places today:** one, in Fold, and the new `slight`.
- **The refactorings:** none.

## Notes

- plan: the behavior probe "fold over expressions with calls" folded
  `n * 2` everywhere; its body became `n * 2 + 1`, which keeps the
  probe's point (arguments with calls) and is not slight.

## Measured

One batch of 2 edits (the check, `slight`), one add_element and two
replace_text in test strings; nothing by hand; no refusals.

## Assessment

Through the tools. A model that wants every match of a small body now
sends one fold per line in a batch, which the refusal says.

## Follow-ups

- Go's fold may match a small body as widely; not checked here.
- An `in_function` limit (the issue's second suggestion) if sessions
  find one fold per line costly.
