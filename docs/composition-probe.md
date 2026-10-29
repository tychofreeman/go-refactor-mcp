# Composition probe: calls split by an argument

Asked after the 34 calls of `set` (`docs/derivations.md` section 7):
would a fresh model find "extract (or declare) one function per field,
fold it, inline `set`" on its own? 2026-10-29, $1.11 for 18 answers.

**Setup.** `claude -p` in `/tmp/probe-set`, no tools, no CLAUDE.md or
MCP; the prompt held a module shaped like `body.set` (four fields of
`Summary`, eleven calls of `b.set(&b.summary.F, w)` in `visit`, set
with a duplicate check that returns early), the task (one recorder per
field, keeping the check, every call of set a call of its recorder),
and the server's tool descriptions (`refactor list`, 60 KB). It asked
for a plan as tool calls, saying where no tool does a step. Three
prompts, three answers each from Sonnet and Haiku:

- A: as above.
- B: plus "before concluding that no tool does a step, look for a
  sequence of two or three of the tools that does it together".
- C: plus four named compositions, one of them this recipe
  (specialize: extract one call per value, fold it, inline the
  general function).

**Results.**

| | wrappers + fold | then inline set | recorders written out, calls moved by text or replace_expression |
|---|---|---|---|
| Sonnet A | 3/3 | 0/3 | 0/3 |
| Sonnet B | 2/3 | 0/3 | 1/3 |
| Sonnet C | 3/3 | 1/3 | 0/3 |
| Haiku A | 0/3 | 0/3 | 3/3 |
| Haiku B | 0/3 | 0/3 | 3/3 |
| Haiku C | 1/3 | 1/3 | 2/3 |

- Sonnet found the composition cold: declare four one-line wrappers
  over `set` (`func (b *body) panics(w Where) { b.set(&b.summary.Panics,
  w) }`, a small edit), fold each, and every call moves over. None
  extracted the wrappers from a call; declaring them is as cheap. The
  nudge (B) changed nothing.
- What stopped the last step was the tools' text, not the idea: 8 of 9
  Sonnet answers wrote the recorders' bodies by hand with
  replace_statements because inline_function's description listed "an
  early return" among its refusals, and set returns early. The tool
  inlines it: a call that is its caller's last statement (a wrapper's
  one statement) may return early, and the run of the plan
  (wrappers, four folds, inline_function `all_calls`) applied in one
  batch. The description now says so.
- Haiku mostly did not compose: it wrote the four recorders out and
  moved the calls with replace_text or replace_expression, or said
  fold "goes the other way". With the named compositions (C) one of
  three took the recipe.

**Reading.** The gap differs by model. Sonnet composes when the problem
is posed with the tools in view, and was blocked by an over-broad
refusal line; the probe found a description bug, not a missing idea.
Haiku needs the composition named, and even then took it once in three.
A plan from the problem statement is not the same as recognising the
composition mid-feature, which is what this project's own session
missed (the friction log's gate is for that); a probe that poses the
change inside a feature is the next to try.
