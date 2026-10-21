# Type errors the model makes

Part of the aim of gorefactor is that the model reasons about what it
wants the code to do, and the tools work out what compiles. Every step
is type-checked, so the type errors a model makes show up as refusals:
each costs a turn and is type reasoning the model did and got wrong.
This is a count of them, to choose what the engine should take over.

## How it was counted

Every gorefactor refusal containing "does not type-check" in all A/B
transcripts in `eval-runs/` as of Oct 20 (19 runs: the feature, chain,
design and third-party sets, Oct 18-20), sorted by the most specific
error each lists. 66 refusals. Three of them were refactorings refused
because the code did not type-check before (a test written first
against the refactored API), not because of the step; they are a kind
of their own (below).

## What they were

| Kind | Refusals | Example | What the model wanted |
|---|---|---|---|
| A name used before it exists | 38 (17 since stubs, #114-#116) | `w.DiffContext undefined`, `unknown field Compact in struct literal`, `undefined: Supplied` as a field's type | a helper, field or type it would write next |
| Callers or implementers left behind by a signature or type change | 10 | `too many arguments in call to Unified`, `too many return values`, `*pageContentOutput does not implement OutputFormatContentProvider (wrong type for method Render)`, `cannot use Repository{...} as *Repository` | change a signature or a field's type, everywhere |
| Imports | 10 | `"fmt" imported and not used` after replacing its last use; `undefined: strings` from replace_text | none: bookkeeping |
| Declared twice | 5 | `identBySymbol redeclared in this block` | replace a declaration, or did not know it existed |
| Other mechanics | 3 | `no new variables on left side of :=`, `declared and not used`, an unexported method of another package | assign; finish the edit; use that helper |

Of the names used before they exist, most (21) are from runs before
stubs were declared for functions and methods (#114). Since then they
are mostly what stubs did not cover: fields (`w.DiffContext`, `unknown
field Compact`), a type named only as a field's type, and the three
that #181 covers (a type from keyed literals, a method passed as a
value, an aliased import).

## What the engine could own, ranked

By refusals removed per unit of work:

1. **Stubs for fields.** A field used before it exists, `x.F` or
   `T{F: v}`, gets added to the struct, typed by its use as stubs type
   results (compared with 3: an int; assigned a string: a string). A
   type named only as a field's or parameter's type gets a stub
   `struct{}`. Same mechanism as stubs, so cheap; the largest kind left.
2. **Imports as bookkeeping in every edit.** `replace_text` adds the
   imports new code needs, and every edit removes those it leaves
   unused (replace_expression does since #163). Cheap; 10 refusals.
3. **Signature and type changes that carry their consequences.**
   `change_signature`: parameters and results of a function or method,
   updating every call and, for a method, the interface it implements
   and that interface's other implementers; `change_field_type`, with
   each use adjusted where the change is mechanical (`&x`, `*x`), else
   refused naming the use. add_parameter and change_parameter_type do
   part of this, but the model changes signatures by text; when an edit
   changes a signature, the refusal could point to the tool. Larger
   work; 10 refusals, and the most type reasoning per refusal.
4. **Mechanical repairs with a note.** `:=` with no new variables
   becomes `=`; a call of an unexported function of another package
   offers to export it; a declaration that exists is refused with where
   it is and how to replace it (replace_statements with `function`).
   Cheap but rare.
5. **Refactorings toward a test written first.** A test written against
   the API a refactoring would create (`order.Address`, which
   extract_struct makes) blocks that refactoring, since the code does
   not type-check. A refactoring could run while the only errors are in
   test files and it fixes them, checked on the rest of the module.

To measure: count these refusals per run before and after, by kind.

## Measured after #181-#190 (Oct 20)

The mcp arm, the four third-party tasks that refactored on the way
(`tp-mcp-type-errors`, 2 runs each, against `tp-mcp-refactor-first`, 1
each) and the four feature tasks (`ab-features-type-errors`, against
`ab-features-denied`, 2 each):

| Set | Type-check refusals per run | Passed | Cost per run |
|---|---|---|---|
| third-party, before | 2.0 (8 in 4) | 4/4 | $0.25 |
| third-party, after | 0.5 (4 in 8) | 8/8 | $0.24 |
| features, before | 0.1 (1 in 8) | 7/8 | $0.29 |
| features, after | 0.25 (2 in 8) | 6/8 | $0.28 |

The third-party drop is real but is mostly how the runs went, not the
new help: type and field stubs, fix_imports and family changes fired
not once in the third-party runs (function stubs did, twice). The
models did not again change a signature by text or use a type before
writing it. What was left:

- **Stubs that could not tell the results** (3): a test calling a
  generic helper the model meant to write (`orderStacksByAncestry(refs,
  ...)` returning a slice of refs; the results only show in how the
  test ranges over them), and `case isClause(s):` in a switch without
  a tag, whose context needs a bool (now read as one: a case of a
  switch needs a bool, or its tag's type).
- **The model's own** (3): a field made a pointer again (Gitea), a
  field added twice, a member of the module's own `json` package that
  does not exist.

The features' failures (symbol-addressing, 0/2) were both the same
hidden test, an interface method by name, not a refusal.
