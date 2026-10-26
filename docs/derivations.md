# Derivations

The user's way of changing code (roadmap item 19, `docs/algebraic.md`):
reshape it with operations that keep what it does, until the change is
one small edit, then make that edit. These are the user's derivations
for five small problems on the seeded module `eval-tasks/testdata/notify`
(2026-10-20), each run with the tools on a copy, and what stopped them.
The letters refer to the gaps at the end.

## 1. Two near-duplicates become one

`Email.Send` and `SMS.Send` differ in a channel name, an address, the
text and the slice they append to. Wanted: both call
`record(sent *[]string, channel, to, text string)`.

**Derivation:** extract a variable for each part that differs, move
the variables to the top, so the rest of both bodies is the same;
extract that rest from one, which replaces it in both (or merge the
two extracted functions); inline the variables.

**Run:** `to` and `text` extracted (from the first occurrence: H). The
slice could not be (`e.Sent` is assigned; a variable would be a copy):
A. `"email"` is inside `"email to %s"`: B. In a batch, an expression
on a line an earlier step rewrote could not be addressed: E. Moving
`text := truncate(...)` above `log.Printf` was refused, `truncate` taken
to panic, though its slice is guarded by a length check: F.
`extract_function` made the rest a method of `Email` and moved the
local `const channel` to package level instead of taking it as a
parameter, and replaces only the lines it is given: D.

## 2. Make room inside a loop

`Broadcast` must skip a channel the user opted out of
(`User.OptOut map[string]bool`).

**Derivation a, wrap and guard:** put an `if` around the loop's
statements, with `true` as its condition, then replace `true` with
`!u.OptOut[ch.Name()]`, the one step that changes behavior.

**Derivation b, filter the data:** extract `c.Channels` to a variable
and add one line before the loop that filters it.

**Run:** b went through: `extract_variable`, `insert_statements`
(`channels = withoutOptOuts(channels, u)`, whose stub the engine
declared with the right signature), `add_field`, `replace_statements`
for the filter. The field had to be added by hand: G. a stops at its
second step, as no operation puts statements into a block: C.

## 3. Change the shape of the data

`User.Email` and `User.Phone` become `User.Contacts map[string]string`.

**Derivation:** a method `Contact(channel)` that every read goes
through, a setter every write goes through; then the change is inside
the two methods. Remove the fields.

**Run (by reading the tools):** writing the method is a small edit.
Turning every read of `u.Email` into `u.Contact("email")` has no
operation: I. One method for both fields, rather than `email()` and
`phone()`, has none: J. Literals (`User{Email: "a@x"}`) cannot call a
setter, and must become `User{Contacts: map[string]string{...}}`: K.
Removing a field: L.

## 4. Move behavior where it belongs

`truncate(s, n)`, used only by `SMS`, becomes `(*SMS).fit(text)` with
the 20 inside.

**Derivation:** a method `fit` that calls `truncate(text, 20)`;
redirect the call sites one at a time; inline `truncate`.

**Run:** went through: `add_declaration`, `replace_expression`,
`inline_function` with `all_calls` (which deleted `truncate`). The
inliner kept `n := 20` as a variable, though a constant could be
substituted: M. Redirecting many call sites is I.

## 5. A parameter object

`Broadcast(c, users, m)` becomes `Broadcast(c, b Batch)`.

**Derivation (expand, migrate, contract):** a `Broadcast2(c, b Batch)`
that delegates to `Broadcast`; at each call site, call `Broadcast2` with
a `Batch`; inline `Broadcast`; rename `Broadcast2` to `Broadcast`.

**Run:** went through until the last step: `inline_function` folded
the old body into `Broadcast2` (with `b.Users` and `b.Message`, no
temporaries), but kept `Broadcast`, being exported, and
`remove_dead_code` refuses exported code, so the rename met a clash: N.
The migration at each call site is I.

## 6. A kind into types, one at a time

On `lending-xl` (2026-10-22): `Item.Kind` is a string (`"book"`, `"dvd"`,
`"atlas"`) switched on in five functions; each kind should be a type.

**Derivation (the user's; expand, migrate, contract):** widen the field
to an interface type; give each switch an interface method that a
value implementing it runs first; then for one kind at a time make its
type, move its cases into the type's methods, send the kind's values
through the type, and delete the cases, now dead; at the end narrow
the field. Every step keeps behavior; only sending the values through
the type changes code that runs, and the argument for it comes from
reading the field's uses.

**Recipe, for one kind (`"book"` into `Book`):**

1. `change_field_type` of `Item.Kind` to `any`, then
   `prepare_switch_for_types` on each switch, with one interface
   (`Kind`) and the function's name as the method. A case of several
   values is split into one each. `add_declaration` of
   `type Book struct{}`. One batch.
2. For each switch, the case's body into a method of `Book`, all
   refactorings: `extract_function` of the body (`bookLoanDays`),
   `add_parameter` `b Book` with value `Book{}` (before the others),
   `function_to_method`, `rename` to the interface's method. The case
   now reads `return (Book{}).LoanDays()`. The last rename makes `Book`
   implement `Kind`, allowed since no `Book` value becomes an interface
   value yet. One batch of 20 steps.
3. `inspect` with `uses: ["Item.Kind"]`: what is done with the field
   everywhere. Sending `Book{}` where `"book"` went is safe when no use
   tells them apart: every switch now runs the same code for both, and
   no `==` compares with `"book"`. Then the edits: where the field is
   written from a kind's name read at run time (`Import`), a function
   `kindOf` that returns `Book{}` for `"book"`; `Book{}` for `"book"` in
   the tests.
4. Delete each `case "book":`, now dead: `replace_statements` with
   the clause's lines and empty content. Or leave them: narrowing the
   field after the last kind deletes every dead case.

**Run:** with the CLI on a copy, four batches, the module's tests
passing after each: 27 derived steps (1 widening, 5 preparations,
20 refactorings, 1 check by `uses`); by hand, `type Book struct{}`,
`kindOf` (6 lines), 3 expressions and 5 deletions. `uses` showed what
grep would not: `Import` rejects unknown kinds by `LoanDays(it) == 0`,
which `Book{}` passes.

**What the run met:**
- rename refused the last method, since `Book` would implement `Kind`
  (fixed: refused only where a value may become an interface value).
- `function_to_method` refuses a name an interface has, so the method
  is made under a temporary name and renamed.
- The methods land after each function; `move_declaration` can group
  them with `Book`.
- Deleting a dead case had no tool; `replace_text` did it (now
  `replace_statements` with empty content over the clause).
- Narrowing the field from `any` to `Kind` is now `change_field_type`,
  which deletes the cases no `Kind` matches; before the last kind it
  refuses, listing the writes and comparisons still of other kinds:
  the recipe can leave step 4 to it. The task's `Label(title string)` where the switch gave
  `Label(it Item)`: introduce_parameter of `it.Title` in `Book.Label`
  (the whole interface's methods change) and remove_parameter of `it`
  on `Kind.Label`, both by name.
- `uses` of a type lists receivers and literals as reads: noise.

**The whole task** (`lending-kinds`, on the same copy, after
`change_field_type` narrowing and case deletion were built): `Label`
reshaped first, while only `Book` implemented `Kind` (`add_parameter`
of `title` on `Kind.Label`, `Book.Label` made to use it, then
`remove_parameter` of `it`); `DVD` and `Atlas` declared, atlas's
`Renewable` given the default's body as a case (`add_case`), then the
other two kinds' cases into methods (44 refactorings, two batches);
`kindOf`, the comparisons in `desk` and `notices` and the tests sent
through the types; `Import` keeps the kind names for its message, and
`kindOf` returns `Kind` (nil for an unknown name); narrowing the field
deleted the ten dead cases in one step; the five switches left empty
deleted with `replace_statements`. The hidden test passes. In all, 74
derived steps; by hand about 30 distinct lines (three type
declarations, `kindOf`, 12 expressions, an added case, 3 lines of
`Import`, 5 empty switches deleted), against 375 lines over 4 runs
(about 94 a run) in the algebraic arm of reading-and-deriving step 3.
What it met:
- `replace_expression` of `"atlas"` with `lending.Atlas{}` in an `if`
  condition wrote a composite literal the parser takes for the `if`'s
  block; it must be parenthesized (a bug, fixed: it now is).
- `introduce_parameter` refuses a method that implements an interface;
  through the interface method it would be a proof (every call passes
  the expression), so `Book.Label`'s body was an edit.
- Narrowing leaves a switch with no cases; it could delete one whose
  tag cannot panic. The guard `it.Kind.(Kind)` stays.
- A kind with no case of its own (atlas in `Renewable`) needs one
  equal to the default before its method can be extracted: `add_case`,
  an edit that a proof could make.

**On this project's own code** (2026-10-22): `change_field_type` had
its modes as two flags, `widen` and `narrow` (#283 added the second),
switched on in `write` and in `use`'s reads. The same derivation,
adapted: `write` regrouped by mode (by hand: no tool regroups a switch
of conditions, and `prepare_switch_for_types` takes a switch on a
value); each mode's branch extracted; the reads given one signature;
an interface `fieldMode` and types `converting`, `widening`,
`narrowing`; each method moved onto its type (method_to_function,
add_parameter of the mode value, function_to_method, rename: 24 steps
in one batch, rename allowed by the check of #281); a `mode` field
set where the flags were, dispatch through it, and the flags removed
with remove_dead_code. 39 refactorings; by hand the regrouping, the
declarations, the dispatch and five one-word edits. What it met: no
refactoring replaces a parameter by the argument every call passes
(the inverse of `introduce_parameter`), so `info` became
`p.TypesInfo` by edits, then `remove_parameter`; and `inline_variable`
and `remove_parameter` refused `p.TypesInfo` (a field read through a
pointer may panic, or change past a call), so one call passed `nil`
for the unused parameter before it was removed.

## The user's principle

Not every operation will exist. Use the operations there are; where
none does a step, make the manual edit as small as it can be, and
avoid manual edits entirely when possible. So a gap matters by the
size of the manual edit it forces, not by how often it comes up: the
constant split (B) costs one small replacement, a fold for reads (I)
saved rewriting every use. Measure runs by the size of their manual
edits (largest, median, total) and the share of steps that write
nothing, as well as by the derived share.

## The gaps

| | Gap | Kind |
|---|---|---|
| A | A pointer alias: `e.Sent` becomes `sent := &e.Sent` and its uses `*sent`. *Done:* `extract_variable` with `pointer` | operation |
| B | Rewrite a constant into an equal expression (`"email to %s"` to `"email" + " to %s"`), the engine checking they are equal | operation |
| C | Wrap statements in `if true { ... }` (or a loop, a closure) | operation |
| D | `extract_function` replaces the same statements elsewhere too, and is told what becomes a parameter | operation |
| E | A batch step names an expression on a line an earlier step rewrote | engine |
| F | A slice guarded by a length check is not taken to panic | analysis |
| G | A field stub from an index (`u.OptOut[k]` as a bool: `map[string]bool`) | stubs |
| H | `all_occurrences` reaches back to occurrences before the one named | small |
| I | Fold: replace each occurrence of an expression or call with a call that means the same (`u.Email` to `u.Contact("email")`, `f(a, b)` to `g(T{a, b})`), checked by inlining the call back | operation |
| J | Parameterize: functions differing only in a constant or a field become one with a parameter | operation |
| K | Fold literals through a setter: `User{ID: id, Email: v}` becomes `withEmail(User{ID: id}, v)` for `func withEmail(u User, v string) User { u.Email = v; return u }`, so that a change of representation is made in the setter's body, for any struct and any new representation. (A one-shot `fields_to_map`, fields into a map with literals and reads, was tried in #217 and dropped: it fit one shape and skipped the derivation.) *Done:* `fold` of a setter, which may set several fields to expressions of its parameters; pointer setters are on the roadmap | operation |
| L | Remove a struct field (refused while used). *Done:* `remove_dead_code` of `Type.Field` | operation |
| M | Inlining substitutes a constant argument used more than once | analysis |
| N | Remove exported code the module no longer uses, when asked to. *Done:* `remove_dead_code` with `exported` | operation |
| O | Fold through a pointer parameter: `*sent` in the body matches `s.Sent`, passed as `&s.Sent`, so only one method needs the alias of A | operation |

The user's derivations reshape without changing behavior, then make
one small edit; the reshaping falls in three families: naming and
aliasing (A, B), folding and unfolding (D, I, J), and containers
(C, K). Folding came up in four of the five; `fold` (stage 1: one-return
bodies) now does problem 4's redirect and problem 3's reads through a
getter, (stage 2: struct parameters used by their fields) problem
5's migration to the parameter object, and (stage 3: statement bodies)
problem 1's last step, the second body's statements becoming a call of
the function extracted from the first. Its engine is the
before/after templates with typed holes of `docs/user-transformations.md`
(option 3): one well-made fold covers D, I and J.
