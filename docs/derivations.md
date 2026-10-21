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
| A | A pointer alias: `e.Sent` becomes `sent := &e.Sent` and its uses `*sent` | operation |
| B | Rewrite a constant into an equal expression (`"email to %s"` to `"email" + " to %s"`), the engine checking they are equal | operation |
| C | Wrap statements in `if true { ... }` (or a loop, a closure) | operation |
| D | `extract_function` replaces the same statements elsewhere too, and is told what becomes a parameter | operation |
| E | A batch step names an expression on a line an earlier step rewrote | engine |
| F | A slice guarded by a length check is not taken to panic | analysis |
| G | A field stub from an index (`u.OptOut[k]` as a bool: `map[string]bool`) | stubs |
| H | `all_occurrences` reaches back to occurrences before the one named | small |
| I | Fold: replace each occurrence of an expression or call with a call that means the same (`u.Email` to `u.Contact("email")`, `f(a, b)` to `g(T{a, b})`), checked by inlining the call back | operation |
| J | Parameterize: functions differing only in a constant or a field become one with a parameter | operation |
| K | Rewrite literals when fields move into another (`User{Email: v}` to `User{Contacts: ...}`). *Done* for a map: `fields_to_map` moves the fields, literals and reads in one step (the Contacts task); writes are refused | operation |
| L | Remove a struct field (refused while used) | operation |
| M | Inlining substitutes a constant argument used more than once | analysis |
| N | Remove exported code the module no longer uses, when asked to | operation |

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
