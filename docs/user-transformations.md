# User-defined transformations: options

A person who does the same few steps often (add a parameter and thread
it through, narrow a receiver, add a table test) should be able to name
that sequence once and call it, as IDE users call templates. Ideally the
definition is a text file in the repository; a definition in code is
worth considering too.

The options below go from the cheapest to the most powerful. Roadmap item
11 in the README takes option 1; it builds on item 10, references to
earlier steps of a batch (`docs/batch-addressing.md`).

## 1. Saved batches with parameters ("recipes")

A file holds a batch's `steps`, `check` and `report`, with placeholders
such as `${type}` and `${field}` in the step arguments. A `run_recipe`
tool, or `refactor recipe narrow.json type=Workspace field=root`, fills
them in and calls `RunBatch` as it is.

- **For:** almost nothing to build. A recipe is rolled back as a whole,
  undone in one step, and may pass through code that does not compile
  with `check: "end"`, as any batch.
- **Against:** steps are literal: no loops, no conditions, and no way to
  use what a step chose (the parameters `extract_function` picked, the
  file `create_from_usage` chose).
- **Against:** portable only for steps addressed by name (`rename`,
  `inline_variable` with `function`, `add_declaration` with `after`,
  `move_declaration`, `replace_text`). A line in a recipe means nothing
  in another file; references such as `$prev.end` (roadmap item 10)
  address the lines the recipe's own steps made.

Covers fixed sequences: "add a parameter, then pass it on through these
two functions".

## 2. Recipes with bindings and queries

Option 1, plus outputs and queries. Each step exposes what it made
(`$s1.new_name`, its ranges), and a recipe can begin with queries
("every call of F", "the methods of T", "the fields T's methods use")
and run steps `for_each` result.

- **For:** covers most of roadmap item 9's intent tools ("move whatever
  a move needs", "narrow a parameter to the fields it uses").
- **Against:** this is where a configuration format becomes a
  programming language. Decide whether to design one (option 6) before
  adding conditions one at a time.

## 3. Before/after pattern templates

As in IntelliJ's Structural Search and Replace, whose `$x$` holes take
constraints (type, count, text), or Go's `gofmt -r` and `eg`
(golang.org/x/tools/cmd/eg). An `eg` template is a Go file with `before`
and `after` functions; their parameters are the holes, so the template
type-checks and the holes are typed.

- **For:** written in Go, not in a new language, and checked by the
  compiler. The same engine is roadmap item 9's "every occurrence of a
  pattern".
- **Against:** rewrites expressions and statements only; it cannot move
  a declaration or change a signature.

## 4. Templates as inlinable functions

The new form is written as a function, and the old one is marked to be
inlined, as `//go:fix inline` does for gopls. `inline_function` at
every call (roadmap item 4) does the rest.

- **For:** reuses a tool that exists; the template is ordinary code with
  its own tests.
- **Against:** only call-shaped changes.

## 5. Live templates (snippets)

As VS Code's `${1:name}` snippets and IntelliJ's live templates, whose
`$VAR$` holes take functions such as `suggestVariableName()`. Here: the
`content` of an edit (`add_declaration`, `insert_statements`) with holes,
such as a table test for a named function.

- **For:** simple, and saves the model retyping boilerplate.
- **Against:** generates code rather than transforming it, so it
  complements the other options.

## 6. An embedded script

Starlark, Lua or JavaScript calling the tools and queries, with real
control flow and each step's outputs.

- **For:** full power and still deterministic. Starlark is sandboxed and
  hermetic.
- **Against:** a new runtime, API and documentation, and a failure deep
  in a script is harder to explain than a refusal.

## 7. In Go

A person implements `refactor.Refactoring` in their own module and
builds a server with it, as `multichecker` does for analyzers; or an
external process serves steps over a small protocol.

- **For:** anything is possible, and the golden-test harness comes with
  it.
- **Against:** the internal packages become a public API. Go's `plugin`
  package is too fragile to rely on.

## 8. Prose recipes for the model

A skill or a markdown file describes the transformation, and the model
expands it into a batch each time.

- **For:** no code at all.
- **Against:** not deterministic, and it costs tokens at every use.
  Useful for trying out a recipe before writing it as an option 1 file.

## What today's batches already allow

- **Works:** a file of name-addressed steps with string substitution.
- **Also works:** steps addressing lines that earlier steps of the
  recipe made, through roadmap item 10's references (`$prev.end`).
- **Does not work yet:** lines of the code the recipe is run on, steps
  that depend on what an earlier step chose (option 2's outputs), and
  "for every X" (option 2's queries).

So option 1 alone is a usable first version, and option 2's queries are
the step after it.
