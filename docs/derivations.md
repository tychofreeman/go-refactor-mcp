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

## 7. Calls of one function split by an argument

`b.set(&b.summary.Panics, w)` is called 34 times with one of a few
fields as its first argument. Wanted: one recorder per field,
`b.panics(w)`, and no `set` (Oct 25: done then by a script).

**Derivation** (the user's, 2026-10-29): for each field, extract one
of its calls into a function, then fold that function, which turns
every other call with that field into a call of it; then inline `set`
into the recorders.

**Run** (a module of the same shape: three fields, five calls; 8
steps, nothing by hand):
- `extract_function` of one call per field, whose second argument is
  a variable: the call uses the receiver, so the new function is
  already a method of `body`, `func (b *body) panics(w Where)`. Where
  a field has only calls with an expression there (`Where{n.line +
  1}`), `extract_variable` first, or the method takes what the
  expression reads and no longer matches the other calls.
- `fold` of each recorder: a parameter matches any expression of its
  type, so `b.set(&b.summary.Panics, n.Pos())` becomes
  `b.panics(n.Pos())`.
- `inline_variable` of the variables extracted for it, and
  `inline_function` of `set` with `all_calls`, which deletes it.

One function per call instead would leave several identical methods
per field, which `merge_duplicate_function` refuses to merge
(methods); extracting one per field and folding makes none. The
recorders keep `field := &b.summary.Reads` from the inlined pointer
parameter.

A body that returns early (set's duplicate check) inlines too: each
recorder's call of `set` is its last statement. Declaring the
recorders as one-line wrappers over `set` instead of extracting them
works the same, and is what fresh models chose
(`docs/composition-probe.md`).

## 8. A value a getter makes on first use, passed in

A getter that makes its result on first use and caches it in a field:

```go
// index returns the beds by name, making the index on first use.
func (g *Garden) index() map[string]*Bed {
	if g.byName == nil {
		g.byName = map[string]*Bed{}
		for i := range g.beds {
			g.byName[g.beds[i].Name] = &g.beds[i]
		}
	}
	return g.byName
}

type planner struct{ g *Garden }

// place puts pl in its bed.
func (p *planner) place(pl Plant) error {
	if pl.Bed == "" {
		return errors.New("no bed")
	}
	b, ok := p.g.index()[pl.Bed]
	if !ok {
		return fmt.Errorf("no bed %q", pl.Bed)
	}
	b.Plants = append(b.Plants, pl)
	return nil
}
```

Wanted: `place` takes the index instead of reaching into the garden,
so a test can pass a map (Oct 25: `ws.Effects()`, done then with `nil`
and replace_text).

**Strictly, one step.** introduce_parameter of `p.g.index()`: the
callers pass it, evaluated at the call. Its first call assigns
`g.byName`, which was a side effect, so it was refused. Since
2026-10-29 a getter of this shape (`if r.f == nil { ... }` then
`return r.f`) counts as reading `r.f` and what the block reads, its
writes to the cache as its own: called earlier or again, it returns
the same. Where the body evaluates the getter first (`find` in
`introduce_parameter/cache_getter`) the step now applies. Here it is
still refused, rightly: `p.g` is read at the call, before the guard,
so a nil `p` with an empty `Bed` would panic where it returned an
error.

**Split at the guard, then strictly.** extract_function of the four
statements after the guard (`placeIn`: `place` keeps the guard and
`return p.placeIn(pl)`), then introduce_parameter of `p.g.index()` in
`placeIn`, which evaluates it first on every call:

```go
func (p *planner) place(pl Plant) error {
	if pl.Bed == "" {
		return errors.New("no bed")
	}
	return p.placeIn(pl, p.g.index())
}

func (p *planner) placeIn(pl Plant, beds map[string]*Bed) error {
	b, ok := beds[pl.Bed]
	...
```

**Non-strict, with the tools before the change.** The same split, then
introduce_parameter of the method value `p.g.index` instead of the
call: the parameter is a function that makes the value when called, so
the getter still runs where it ran, and nothing about when it runs has
to be proved:

```go
	return p.placeIn(pl, p.g.index)
}

func (p *planner) placeIn(pl Plant, index func() map[string]*Bed) error {
	b, ok := index()[pl.Bed]
```

Both ran with the CLI on a scratch module, the non-strict one on the
tools before the change. Then the wrapper folds away, by the user's
next derivation (2026-10-29): a function that returns early inlines
once the statements after each if that returns become its else, and
each return assigns a variable of the result's type instead, so the
body runs to its end. inline_function does this itself. The call in
`PlaceAll` is an if statement's header, so extract_variable moves it
out first (`placed := p.place(pl)`), then inline_function of `place`
gives

```go
		var placed error
		if pl.Bed == "" {
			placed = errors.New("no bed")
		} else {
			placed = p.placeIn(pl, p.g.index())
		}
		if err := placed; err != nil {
```

and rename `placeIn` to `place`. A return in a loop, a switch or a
block is still refused: the statements after it cannot become an else.

## 9. Inlining a function that returns early

The user's derivation (2026-10-29): give each if that returns the
statements after it as its else branch, then give the function one
variable of its result type that each return assigns, and return it at
the end. With one return, the inline is trivial. remove_else's `wrap`
is the first step on its own; inline_function does both itself, never
changing the callee:

```go
func sign(n int) string {          s := sign(n)   becomes
	if n < 0 {                     var s string
		return "neg"               if n < 0 {
	}                                  s = "neg"
	return "pos"                   } else {
}                                      s = "pos"
                                   }
```

A return in a loop or a switch has no else to take the rest. When the
rest is only a return of a value that can be evaluated first (it does
nothing, cannot panic, and the loop changes nothing it reads), the
user's way: the variable starts with that value, each return in the
loop assigns it and breaks out:

```go
	v := -1
loop:
	for _, x := range xs {
		if x > 0 {
			v = x
			break loop
		}
	}
```

Otherwise (a statement before the last return, or a value the loop
changes, `return total`), each return sets a flag the rest is guarded
by:

```go
	var v int
	var returned bool
loop:
	for _, x := range xs {
		if x > 0 {
			v = x
			returned = true
			break loop
		}
	}
	if !returned {
		println("none")
		v = -1
	}
```

Another way, the user's too, when the call sits in a setter's if:
extract the if (or whatever encloses the call) into a method of its
own. There nothing runs after the call, so the callee's returns stay
returns, and the inline only copies the body in:

```go
func (p *planner) placeIf(force bool, bed string) {
	if force {
		if bed == "" {
			return
		}
		p.placed = append(p.placed, bed)
	}
}
```

It keeps a method, where the first way rewrites the returns in the
caller; which reads better depends on the code around the call.

## 10. Sharing code that returns a refusal early: extract the tail, pass what follows

Found running the compositions item 43.10's rewrites skipped
(2026-10-29, roadmap item 44). extract_function refuses lines that
return on some paths unless the function returns an `error`, but lines
that return on every path extract, and the end of a function always
does. So extract from the start of the shared part to the end of the
function, then make the part that differs between callers a parameter:

1. extract_function the part that differs, at the end (the code that
   inserts each call's argument; what runs after the per-method edits).
2. extract_function from the start of the shared part to the end of the
   function: every path returns, so it extracts, and the call becomes
   `return shared(...)`.
3. introduce_parameter of the function step 1 made, in `shared`: it
   becomes a parameter `then` of function type, and the call passes it.
4. Where step 2 made a method of the wrong type, method_to_function,
   then function_to_method with `receiver`.

Another caller then calls `shared` with its own `then`. What it gives
is code in continuation-passing style, not a function that returns the
shared result: CaseLines' lookup became `lookup(..., then)`, not
`caseClause(...) *ast.Node`. Run on three of the four rewrites:
add_result's per-method edits (A5, 5 steps, `t.forFn(p, fn, l, ...,
then)`), CaseLines' lookup (B2, 5 steps with one replace_expression),
introduce_parameter's per-call loop (C1, 3 steps; a fourth that would
include the checks is refused for a `:=` redeclaring a captured
variable). What none does: move a statement that may return past
another that may (`findCalls` above the result-type check), which
changes which refusal wins.

## 11. A mode through a closure: make it a function that builds the closure

add_parameter does not reach a function literal bound to a local
(`try := func(...) {...}`), and its body's returns keep extract_function
from taking the body out. The statement that binds it extracts whole:

1. extract_function the `try := func(...) {...}` statement: it becomes
   `try := makeTry(...)`, whose result is the closure, its captured
   locals parameters (pointers where it changes them).
2. add_parameter to `makeTry` (`fields`, value `nil` at the one call).
3. insert_statements in the closure's body to use it
   (`m.fields = fields`), or carry it on with add_parameter to what the
   closure calls (`newMatcher`, with add_field and add_element for the
   struct it fills).

A second mode is then a second `makeTry(..., fields)` call. Run on
fold's object-parameter mode (D2, 4 steps; D5, 3 steps through
`newMatcher`); what is left is the new behaviour itself.

## 12. Reusing a function whose statements compute its result

fold matches a body that is one return, a setter, or statements without
results. A function that computes a variable and returns it
(`textutil.LineEnd`: an if that moves `at`, then `return at`) is none of
those, so its copies elsewhere do not fold. Two steps per copy:

1. extract_function the copied statements: what they set and is used
   after becomes the result, what they read the parameters, so the new
   function has the same body as the one to reuse, names aside.
2. merge_duplicate_function the new function into the one to reuse
   (`keep_file`, `keep_name`), in another package too: the call becomes
   `off = textutil.LineEnd(src, off)` and the copy is gone.

Found after 46.4, which had made two copies one-line edits by hand and
logged fold's refusal as a gap; fold's refusal now names these steps.
It fails when the copy evaluates its parts in another order than the
function (49.13: `field.End()` read after a call the function makes
first): extract_function then gives a different body, and
introduce_parameter or move_statement_up refuses to swap two calls
that could panic. The call then goes in by replace_statements.

## 13. Moving a struct field: delete it, add it back where it belongs

No tool moves a field within its struct (extract_struct moves fields
into a new one). Two steps:

1. Delete it: remove_dead_code of `Type.Field` when nothing uses it
   (gap L), or replace_declaration of `Type.Field` with empty content,
   an edit, which also deletes one whose struct is printed.
2. add_field with `after` (or `before`) and the field as it was, its
   doc and line comment included.

A field in use cannot be deleted first (the code no longer
type-checks); a stub field that create_from_usage put at the end, which
nothing uses yet, can. Found as a gap in `refuse-code-by-text` (49.13),
which had missed that remove_dead_code takes a field.

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
