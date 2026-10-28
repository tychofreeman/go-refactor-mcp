# Friction log

The point of this project is a workflow: during a normal story, Claude
picks refactorings that make the change easy, the tools perform them
deterministically, and Claude then makes the small hand edit the story
needs. This log records, from real work, every time that broke down: a
refactoring done by hand because no tool covers it, a refusal that got in
the way, or a tool result that was correct but awkward. It is the evidence
for what to build next, and for whether the workflow helps.

Each entry: the story, the refactoring wanted, what happened, and the gap.
A hand edit that changed behavior on purpose (new code, a changed
statement) is logged too, with the edit that would have covered it; the
candidates and their evidence are collected in `docs/edits.md`.

Entries are in the order the work was done, oldest first (by the commit
that added each); add a new one at the end.

## 2026-10-17: adding `refactor-eval`

**Add a parameter at every call site.** `copyModule(src, dst)` needed a
directory to skip. Done by hand: the signature and its one call. Gap:
`change_signature` (add a parameter).

**Extract a function that returns a value instead of changing a pointer.**
`extract_function` on the diff-line counting loop in `runOne` produced
`countDiffLines(rep, &r)`. That is correct: a deferred closure captures `r`,
so the tool shares it through a pointer. But the function wanted was
`r.DiffLines = diffLines(rep.Diff)`. Finished by hand. Gap:
`change_signature` (narrow a parameter to the field the body uses, turn a
pointer that is only written into a result), or a choice in
`extract_function` to return the new value.

**Move a declaration to another package.** `defaultHost` in
`cmd/refactor-agent` was needed by the new command too, so it moved to
`internal/agent` as `DefaultHost`. Done by hand. Gap: a move-declaration
refactoring (with the rename `rename` can already do).

**A poor extraction gets no warning.** A local model (qwen3.5:9b) extracted
31 lines from the middle of a block of shared counters into a function with
13 parameters and 10 results. The tool applied it without comment; the
model's next step was to undo it, then apply it again. Gap: no signal in the
result that the new function's signature is very wide. A note in the
`APPLIED` result ("the new function takes 13 parameters and returns 10
results") would cost little and help any model judge its own step.
*Follow-up: done; more than five parameters or three results gets a
note.*

**Follow-up:** `add_parameter` now covers the first entry: the `copyModule`
change is one call, `parameter=skip type=string value=cfg.OutDir`, and the
new parameter joins the group (`src, dst, skip string`).

## 2026-10-17: adding `remove_parameter`

**A red test blocks every refactoring.** Under TDD the new test referred to
`NewRemoveParameter` before it existed, so the module did not type-check
and `extract_function` refused to run. A stub that refuses everything made
the code compile while the tests stayed red. Gap: none in the tools (they
must verify against compiling code), but the workflow needs the stub step:
write the test, add a compiling stub, then refactor, then implement.

**`extract_function` leaves an `err2`.** Extracting the "passes another
call's results" check from `checkValue` gave `err2 := checkNoSpread(c,
where); if err2 != nil { return err2 }`, because `err` is declared later in
the function. Folded into `if err := ...; err != nil` by hand. Gap: the
call site could use the `if` initializer form when the error is used only
by the check. *Follow-up: done; with no other results the call site is
`if err := f(...); err != nil {`.*

**Move a declaration to another file.** The extracted `checkNoSpread` was
shared by both refactorings, so it moved from `addparam.go` to `common.go`
by hand. Gap: the move-declaration refactoring (roadmap item 2) within a
package, too.

## 2026-10-17: adding `move_parameter`

**Merging two similar function endings.** `add_parameter` and
`remove_parameter` both ended `Plan` the same way (count the files, write
the summary, merge the edits, note an exported function), differing only
in the summary's first words. Moving that into a shared `newPlan(what,
edits)` was done by hand: `extract_function` takes one run of statements,
and the file count was computed inline from data each built differently.
Gap: none of the tools parameterize and merge near-duplicate code; an
extract that turns a differing expression into a parameter would cover it.

**`move_parameter` is mostly the other two.** The cut of the old parameter
and argument came from `remove_parameter`, the insertion from
`add_parameter`; the new code is the evaluation-order check. The earlier
refactorings (`checkNoSpread`, `paramCut`/`argCut`, `newPlan`) made the
third tool cheap, which is the workflow working as intended.

## 2026-10-17: adding `move_declaration`

**The workspace could not create a file.** Every edit was to an existing
file, so moving a declaration to a new file first needed `Preview` to accept
a missing file, `Apply` to create it and `Undo` to delete it. A small
workspace change, test first; the incremental update already fell back to a
full reload for a new or deleted file.

**First use on this repo.** `move_declaration` previews the move the
`remove_parameter` work did by hand (`checkNoSpread` to `common.go`), and
also shows one still waiting: `unsafeValue`, now used by all three parameter
refactorings, lives in `addparam.go`. Moving it carries its `analysis`
import along and drops it from `addparam.go`.

## 2026-10-17: moving a declaration to another package

**First real `move_declaration`.** `unsafeValue`, shared by the three
parameter refactorings, moved from `addparam.go` to `common.go` in one MCP
call, its import with it. No by-hand work.

**Import removal moved into `imports.Adder` by hand.** A cross-package move
edits imports in many files, and one file can lose an import and gain
another in the same block, so removal had to join addition in one place
(`Adder.Remove`, with `Edits` producing both). That turned a function of
`movedecl` into a method of a type in another package. Gap: no tool turns
a function into a method, and `move_declaration` does not yet move code
that uses its neighbours. *Follow-up: `function_to_method` (in the type's
package, so here `move_declaration` to `imports` first).*

**Three statements replaced by a call, by hand.** After adding `docOf`,
`checkDecl` still computed the same doc comment in three places. Replacing
them was a hand edit. Gap: `replace_statements`, proposed among the edits.

## 2026-10-17: moving several declarations at once (`also`)

**Splitting a struct in two, by hand.** `mover` held one declaration's
fields (`decl`, `obj`, `start`, `end`, `newName`) next to the move's. They
moved into a new `moved` type, embedded in `mover` so every `m.obj` kept
compiling, with `decls []*moved` beside it. Gap: no tool extracts fields
into a new type (an "extract struct" / introduce parameter object
refactoring); embedding made it a two-line hand edit here, but in general
it touches every use. *Follow-up: `extract_struct`.*

**`add_parameter` refused a value the body reads anyway.** Giving
`checkDecl`, `deleteDecl` and `code` a `d *moved` parameter, passing
`m.moved`, was refused: `m.moved` reads through a pointer and could panic.
True, but each body dereferences `m` at once, so the panic would happen
anyway. Done by hand, along with the body's switch from `m.decl` to
`d.decl`. Gaps: `add_parameter` could accept a field read through a
pointer the body dereferences before any effect; and no tool turns a
use of state (`m.decl`) into a parameter that is passed that state
(introduce parameter).

**Wrapping code in a loop, by hand.** Each check that handled the one
declaration became a `for _, d := range m.decls` loop around the same
statements. Gap: `introduce_loop`, among the edits (as a refactoring it
would need to prove the loop runs once).

**What the tools did.** `extract_function` pulled the per-declaration
checks out of `checkOtherPackage` into `checkCanLeave`, which then gained
the parameter and the loop by hand.

## 2026-10-17: adding `introduce_parameter`

**Methods turned into functions, by hand, so they could move.** Finding an
expression by its source text lived in `extract_variable` as methods of
its `extraction` (`find`, `matches`, `lineHint`), and so did
`usedAsVariable`, though each used only a field or two of the receiver.
Each was rewritten as a function taking those fields, by hand; then
`move_declaration` moved them to `locate` and `analysis`, five moves with
no by-hand work. Gap: a refactoring that turns a method into a function
(the receiver, or just the fields it uses, become parameters), the
reverse of the function-into-method gap logged earlier. *Follow-up:
`method_to_function` (the receiver becomes the first parameter; narrow it
with `introduce_parameter` and `remove_parameter`).*

**`move_declaration` put module imports in the standard library group.**
Moving code into `locate/expr.go` added `internal/refactor` and
`internal/workspace` among `fmt`, `go/ast` and the rest, not in a group of
their own. Regrouped by hand. Also: `equalTokens`, which had no doc
comment, became the exported `EqualTokens` without one. Gaps: new imports
should go in the group they belong to; a move that exports a name could
note that it has no doc comment. *Follow-up: imports done (`imports.Adder`
keeps standard library imports and the others in separate runs).*

**What the tools did.** `create_file` wrote the stub and the test file,
`move_declaration` shared six declarations between the two refactorings.
The refactoring itself was written by hand (no `replace_declaration`
edit yet), mostly from `add_parameter`'s pieces: `findTarget`,
`insertIndex`, `checkNewName`, `checkInterfaces`, `paramEdit`, `argEdit`,
`unsafeValue` and `newPlan`.

## 2026-10-17: `introduce_parameter` through the receiver and pointers

**`extract_function` refused a variable used by a synchronous closure.**
Extracting the `effect := false; ast.Inspect(arg, func ... { effect = ...
})` lines from `checkCall` was refused: `effect` is used by a function
literal and after the lines, so returning it "would return a copy that the
function literal no longer shares". But `ast.Inspect` calls the literal
only before it returns (`analysis.SyncLit` already knows this), so the
copy is the final value. Extracted the whole loop instead. Gap: treat
variables of synchronous closures that finish inside the selection as
plain results. *Follow-up: done.*

**`extract_function` at the end of a function.** Extracting the last
statements before `return nil` gave `err := f(); if err != nil { return
err }; return nil`, where `return f()` would do. Folded by hand. Gap:
recognize the trailing `return nil` (see the `err2` entry above too). *Follow-up: done; the call site is now `return f(...)`, or
`return 0, f(...)`, when the selection is followed by the matching
success return.*

**Building the new call site needed a slide, by hand.** To pull the
call-site code out of `extract_function`'s 160-line `plan`, the
`strings.Builder` it fills had to stay inside the extracted lines (copying
one is refused), so `site.String()` was named with `extract_variable` and
included. That needed `at := x.insertOffset(decl)`, which sat between them,
moved above the call-site code first: a hand edit. Gap: `move_statement_up`
(in `docs/edits.md`; a refactoring here, since the two are independent).

**First real use: the case that motivated the tool.** On this repo,
`introduce_parameter` with `d.decl` at the top of `movedecl`'s
`checkDecl` gives `checkDecl(d, d.decl)`, the step the earlier entry did
by hand. The later `d.decl` uses are refused, correctly by the rule but
only because a `refactor.Refusef(...)` call comes earlier, on a path that
returns before reaching them. Gaps: a flow-aware "before" (calls on paths
that return first cannot change anything), and introducing every
occurrence at once, which for the later uses needs to know no call in
between writes `d.decl`.

**Found by the real use:** a type switch guard (`decl := d.decl.(type)`)
was counted as a write and a possible panic. Fixed, test first.

## 2026-10-17: adding `method_to_function`

**Found by a test: chained calls of a method were half updated.**
`collectCalls`, shared by the signature refactorings, keyed each call by
its start offset to skip the same call seen through another package
variant. In `n.after().after()` both calls start at the same offset, so
the outer one was skipped: `add_parameter` on such a method gave code that
did not compile (the engine rolled it back). Calls are now keyed by their
range.

**Real code, dry run.** On this repo, `method_to_function` with
`imports.Adder.nextLine` gives `nextLine(a, last.End())`, no by-hand work.

## 2026-10-17: adding `function_to_method`

**`introduce_parameter` refused a getter.** Making `checkNoInterface`
take the method name, instead of reading `t.obj.Name()`, was a job for
`introduce_parameter`, but any call counts as a side effect, even
`types.Object.Name()`. Done by hand. Gap: a list of calls known to be
pure (getters of `go/types` objects, `strings` functions, `len`-like
helpers) for `analysis.HasEffect`.

**A test variant has its own types.** Comparing an argument's type with
the parameter's by `types.Identical` failed for calls in the test file
(the test variant re-checks the package, with distinct `*types.Named`), so
`size(&mover{...})` came out converted, `(*mover)(&mover{...})`. Compared
by name, as `add_parameter` already does.

**Real code.** `extractvar`'s `needsExplicitType(x *extraction, ...)`, the
one function there that takes the extraction as a parameter, became the
method `x.needsExplicitType(expr, tv)` in one call. A round trip
(`method_to_function` then `function_to_method` on `Adder.nextLine`)
gives back the file byte for byte.

## 2026-10-17: story, `move_declaration` out of a grouped declaration

**What the tools did.** `extract_function` pulled the text-range code out
of `checkDecl` into `setRange`, which then learned to find a spec's range.
The rest was the feature itself, by hand: a `spec` (and `whole`) on each
moved declaration, `node()` for what the inspections walk, and the
`const` group checks.

**By hand that a tool could have done.** Six `ast.Inspect(d.decl, ...)`
became `ast.Inspect(d.node(), ...)`: replacing one expression by another
at several places is `replace_expression` (in `docs/edits.md`), with an
all-occurrences option. And `docOf` took an `ast.Decl` and now takes an
`ast.Node`: no tool changes a parameter's type (a `change_signature`
gap; widening to an interface the arguments satisfy keeps behavior).

**A test of mine was wrong, and the tool said so.** The first test moved
`b = strings.ToUpper("x")` out of a `var` group; the existing rule refused
it, rightly: a call in an initializer could run in another order once in
another file.

## 2026-10-17: story, a type with its methods to another package

**A crash found by thinking, then a test.** The unkeyed-literal check
read the literal's type name from the syntax, `lit.Type.(*ast.Ident)`,
which is nil for the inner literals of `[]T{{4}}`. A test reproduced the
panic before the fix (the type's name now comes from the type).

**Real code, dry runs.** Moving `movedecl`'s `fileEdits` (with its method
`dropUnused`) to `internal/edit` is refused because `otherpkg.go` uses its
unexported `edits` field; moving `changesig`'s `call` is refused because
its methods `recv` and `recvSelection` are in `introparam.go`, with the
next step named (move them into `common.go` first). Both are the refusal a
reviewer would give.

**By hand.** The new checks reuse the shape of `changesig`'s
`checkNoInterface` (an interface with a given method of a package), now in
two packages. Gap: once more, a shared helper wants to move to `analysis`,
and the two versions differ only in their messages: an extract with a
parameter for the differing part would merge them (the near-duplicate
gap logged before).

## 2026-10-17: story, moving declarations from several files

**A struct split, by hand.** `mover` held one source file (`path`,
`file`, `src`, `imports`); they became a `source` type, embedded in
`mover` so every `m.path` kept compiling, with `sources` beside it and a
`from` on each moved declaration. The same embedding trick as the
`moved` split before. Gap, again: extract fields into a new type (seen
twice now). *Follow-up: `extract_struct`.*

**A receiver type changed, by hand.** `lineStart` and `nextLine` read
only `m.src`; their receiver became `*source` (calls on `m` still compile
through the embedding). No tool changes a receiver's type; the closest,
`method_to_function` then `function_to_method`, needs a parameter of the
new type in between. Gap: change a receiver to a field's type it reads
only through ("move method to a field's type").

**Ordering across files.** Sorting moved declarations by `token.Pos`
followed the order the files were loaded, not the request; they are now
ordered by source (the named declaration's file first), then position.

**Real code, dry run.** `changesig`'s `call`, refused last round because
its methods are in `introparam.go`, now gets the next real refusal when
moved to `internal/analysis`: its unexported `expr` field is used by code
left behind. Within the package it moves (its methods stay).

## 2026-10-17: story, moving into a new package

**The workspace could not create a directory.** As with new files before,
the first step was in `workspace`: `Preview` records the directories a
new file needs (`Change.Dirs`), `Apply` creates them, and undo removes
them. A test first, at that level.

**A stand-in package.** The rest of the move needed only the destination's
path, name and an empty scope, so a new package is a `packages.Package`
with an empty `types.Package`; no code downstream changed.

**Real code, on a copy.** Moving `imports.PathBase` and `IsStd` to a new
`internal/importpath` updated five references and built. Gap, small: the
CLI's `key=value` form cannot pass a list (`also`); the JSON form works.

## 2026-10-18: adding `extract_struct`

**The engine caught a bug before the tests did.** Inside a `type ( ... )`
group the new spec was written with the `type` keyword; the result did not
parse, so the change was rolled back with the errors, and the golden case
failed with a clear message. Fixed.

**Real code: the split this tool replaces.** On a copy of the repo at the
commit before the `source` split, `extract_struct` with `mover`'s `file`,
`path`, `src` and `imports` gave the first step of that change in one
call (the struct, the embedding, and the one literal, in `find`), and
`movedecl`'s tests passed. It embeds by value; the hand version embedded
`*source`, which changes what copying a `mover` shares.

## 2026-10-18: story, one interface check instead of two

`changesig.checkNoInterface` and `movedecl.checkNoInterface` searched the
same thing (an interface in the workspace with a given method of a given
package) and differed in their receivers and messages. Now both call
`analysis.InterfaceWithMethod`, which returns what it found; each keeps
its own message.

**What the tools did.** `create_file` wrote the shared function's stub
and its unit test (test first). `rename` gave `extractstruct`'s different
check (values passed as an interface) its own name,
`checkNotPassedAsInterface`.

**By hand, and why no tool fit.** The plan was `method_to_function`, then
`introduce_parameter` for what the receiver supplied, then
`move_declaration` to `analysis`. It would have stalled twice: the body
reads `t.obj.Pkg().Path()`, a call (and calls count as side effects, the
pure-call gap logged before), and its messages use `t.ws.Position`, which
`analysis` must not depend on. The real change was a change of shape, from
returning a refusal to returning a finding, which is an edit, not a
refactoring. So by hand: the shared body (copied from one of the two),
the two callers' new bodies, and `Workspace.TypesInfos`. Gaps:
`replace_statements` / replacing a function body (in `docs/edits.md`),
`add_method`, and the pure-call list.

**Small ones.** `rename` noted that the doc comment still began with the
old name, and left it; a doc comment that starts with the symbol's name
(the Go convention) could be renamed with it, and re-wrapped.

**More duplicates, counted.** `plural` is declared in 10 packages,
`kindOf`/`kind` in 6, `keyOf` in 3, each with the same body. Each could
be replaced by one shared copy, but no tool replaces a function by an
identical one elsewhere and redirects its uses. Gap: a "use the identical
function" refactoring (the body tokens match and resolve to the same
objects, so every call can call the other one, and the copy goes).

## 2026-10-18: adding `merge_duplicate_function`, and using it

**Ten copies of `plural` became one, by the tools.** `move_declaration`
moved `changesig`'s copy to `textutil` as `Plural`, and
`merge_duplicate_function` merged each of the other nine into it (one of
them the copy this tool's own package had), one call each: their uses
switched to `textutil.Plural`, the import was added in the module group,
and the copies went. By hand: only `Plural`'s doc comment.

**Two more helpers, the same way.** `keyOf` (`movedecl`, and the copy
here) became `analysis.ObjectKey`, and `kindOf` (`inlinefunc`,
`inlinevar`) became `analysis.KindOf`, each with one move and one merge.
`move_declaration` renamed `keyOf`'s doc comment with it; `kindOf` had
none, and was given one by hand (a move that exports a name without a doc
comment could note it, logged before).

**The copies that differ were refused, with the difference.**
`extractvar`'s `kindOf` says "predeclared" where `KindOf` says
"predeclared nil"; `changesig`'s `keyOf` starts with an `if` where
`ObjectKey` starts with an assignment. Those are near-duplicates, not
duplicates: merging them would change a message or a result. Small gap:
the refusal names node types (`*ast.IfStmt`) where words would read
better ("an if statement"). *Follow-up: done; `analysis.Describe` names
syntax in words, and the refusal adds the start of the code: "an if
statement (if obj == nil {) ... where ObjectKey has a short variable
declaration (p := fset.Position(o.Pos()))". A body with one statement
more no longer gives an empty "where a has  at -": the other side is "the
end of a block".*

## 2026-10-18: calls known to have no side effects

**What it unblocks, tested.** `add_parameter` passing
`strings.ToUpper(name)`, `introduce_parameter` of `strings.TrimSpace(s)`,
`extract_variable` replacing every `strings.ToLower(s)`, and
`inline_variable` repeating `strings.ToUpper(s)` were all refused before
(any call counted as a side effect); now applied. `strings.Repeat` is
refused as "could panic", not as a side effect.

**The two cases that prompted it are still refused, for the right
reason.** `t.obj.Name()` and `t.obj.Pkg().Path()` (in the
`function_to_method` work and the dedupe story) are known getters now, but
both sit inside a closure assigned to a variable, which can run any number
of times, so `introduce_parameter` refuses them as "evaluated more than
once per call". Gap: an expression that reads only state that cannot
change (a parameter never assigned, then immutable getters) has the same
value however often it is evaluated, so repetition need not matter.

**An existing test changed meaning.** `introduce_parameter/refuse_call`
used `strings.ToUpper` as its example of a call with side effects; it now
uses `fmt.Sprint`.

## 2026-10-18: adding `replace_statements`, the first edit after `create_file`

**Built by hand, of course.** Its own stub could not be replaced by
itself; the implementation was written directly.

**Replaying a hand edit with the tools.** On a copy of the repo at the
commit before the dedupe story, `create_file` wrote
`analysis.InterfaceWithMethod`, and one `replace_statements` call turned
`changesig`'s `checkNoInterface` body (26 lines) into the call and its two
messages: the story's main hand edit, in two tool calls, with
`changesig`'s tests passing.

**Imports: our resolver, not goimports.** goimports could pick any
package in the module cache; this one looks, in order, at what another
file of the package imports under the name, the workspace's packages, and
the standard library, and refuses with the candidates when the name is
ambiguous (`rand`), so the result never depends on what happens to be
downloaded.

## 2026-10-18: adding `insert_statements`, mostly with the edits

**Prep with the tools.** `replace_statements`' parsing, re-indenting and
import code moved to a new `internal/edits/gocode` with seven `rename`s and
one `move_declaration` (from two files, into a new package). The move was
refused at first: `versionSuffix = regexp.MustCompile(...)` is a package
variable initialized by a call. So `create_file` wrote an `isVersion`
function and `replace_statements` switched to it; the unused variable was
deleted by hand (gap: `delete_statement`). *Follow-up:
`remove_dead_code` covers it.* Small gaps: a constant pattern
for `regexp.MustCompile` could count as having no effect; and the move
left `version.go` with only its package clause (a move that empties a file
could delete it). *Follow-up: done; `move_declaration` deletes a source
file it leaves with only a package clause.*

**A bug the move showed.** `move_declaration` wrote a *new* file's imports
in one unsorted group; the earlier grouping fix had been in `imports.Adder`
only. Fixed, test first, with one `replace_statements` call: the first
attempt, three separate replacements, was refused because the first one
alone did not type-check, which is the rule working (dependent changes
are one step).

**The edit itself, with the edits.** `create_file` wrote the stub, the
test and the helpers file; `replace_statements` turned the stub's
`return` into `Plan`, adding six imports (four of them workspace
packages) by itself; another `replace_statements` merged two redundant
branches. By hand: none of the new code.

**`rename` keeps leaving doc comments behind.** Seven renames, seven
stale doc comments, fixed by hand; and renaming a local `a` noted 34
comments mentioning "a" across the file. Gap: rename a doc comment that
starts with the name, and only note comments in the symbol's scope. *Follow-up: done; `rename` renames a doc
comment that starts with the old name, and for a local notes only the
comments in its scope.*

## 2026-10-18: adding `add_declaration`

**With the edits again.** `create_file` wrote `gocode.ParseDeclarations`,
the stub, the test and the placement helpers; `replace_statements` wrote
`Plan`, adding its seven imports. By hand: generalizing `gocode.Code` (a
new field, gap `add_field`; and `c.Body` to `c.node` inside two
`ast.Inspect` calls, gap `replace_expression`).

**A small inconsistency left.** `insert_statements` and `add_declaration`
note added imports as `["fmt" "os"]`, `replace_statements` as `"fmt",
"os"`: three copies of note formatting that a shared helper in `gocode`
would make one. *Fixed in the same PR, with the new tool:
`add_declaration` wrote `gocode.ImportNote`, and four `replace_statements`
switched the callers; the now unused `quoted` was deleted by hand (gap:
`remove_dead_code`, since done).*

## 2026-10-18: adding `replace_expression`

**What "the same expression" means.** The first rule (each name the same
object) would not have replaced `c.Body` in two methods: each method's
receiver `c` is its own variable. It now asks that package-level names,
imports, fields and methods be the same objects, and local variables have
the same types: `c.Body` in another method of `*code` matches, `d.Body` of
another type does not. Found by thinking it through against the test
before running it.

**Its first use was on itself**, through the CLI: `off + (pos(t.End()) -
off)` became `pos(t.End())` in its own `Plan`.

## 2026-10-18: adding `add_field`

**Built with the edits, and cleaned with them.** `create_file` and
`replace_statements` as before. Then a wart in my own helpers (checking a
single field by wrapping it in a throwaway `StructType`) was fixed with
the tools: `add_declaration` added `fieldDeclares`, two
`replace_expression` calls used it, and a `replace_statements` made
`fieldNamed` use it too. No hand edits in the new package after the
first draft.

**A false note found by that use.** `replace_expression` said "the
expression's type changes from untyped bool to bool" for
`f(...) != nil` → `g(...)`: a comparison's untyped bool becomes `bool`
wherever it is used, so that is no change. Fixed in `replace_expression`.

## 2026-10-18: `move_declaration` deletes a file it empties

**All with the tools.** `add_field` gave `edit.FileEdit` a `Delete` flag
(so the workspace test written first compiled) and `workspace.Change` a
`Deleted` set; `insert_statements`, `replace_statements`,
`replace_expression` and `add_declaration` wrote the rest. By hand: only
the testdata, the docs, and one line of the tool's description (a string
literal; `replace_expression` could have done it, but a long raw string
is awkward to restate).

**`after` cannot name a method.** `add_declaration` with `after:
"fileEdits"` put the new methods after the *type* `fileEdits`, not the
method `mover.fileEdits` beside which they belonged. Gap: accept
`Type.Method` in `after`/`before`, as `inspect` does. *Follow-up: done.*

## 2026-10-18: adding `move_statement_up` / `move_statement_down`

**With the tools, mostly.** `create_file` wrote the stub, the test, the
analysis (`facts.go`) and the stress test; `add_declaration` and
`replace_statements` wrote `Plan` and its helpers; `extract_function`,
`rename`, `replace_expression` and `add_field` cleaned up after. By hand:

- **a new directory:** `create_file` refuses a file in a directory that
  does not exist ("creating directories is not supported yet"), though
  the workspace can create them (`move_declaration` does). Gap:
  `create_file` creating missing directories. *Follow-up: done.*
- **an element in a list:** registering the tools meant adding two
  elements to the composite literal in `registry.go`. `replace_expression`
  takes one expression, so the whole `return` was restated with
  `replace_statements`. Gap: insert an element into a composite literal
  or argument list.
- **a second result:** `leaves` had to return whether other statements
  may cross its jump. No tool adds a result to a function; changed by
  hand (and the one call site restored with `sed` so the tools would
  accept the code again). Gap: `add_result`, the counterpart of
  `add_parameter`.
- **a doc comment:** `extract_function` writes none, and no tool adds
  one to a declaration. Gap: a `doc` argument for `extract_function`, or
  an edit that sets a declaration's doc comment. *Follow-up:
  `extract_function` takes `doc`.*

**Line numbers again.** Two `replace_statements` were refused because I
reused line numbers from before an insertion; the refusals ("cut the
return statement", "cut the block") said exactly what was wrong.

**What the stress test showed.** The first run (3000 statements of this
module): no rollbacks, but 56% of refusals were "the statement returns",
nearly all `if err != nil { return ... }` guards. Moving a statement that
only declares variables, with no side effects or panics, past such a
guard is safe, so that is now allowed; assignments are still refused (a
bare `return` reads named results). The next largest group is calls not
known to be free of side effects: recognizing the module's own functions
that have none would help most. After the change, with each package's
tests run after every move: 286 of 3000 applied, no rollbacks, no test
failures; "returns" is still the largest refusal (1490), now because the
statement to move calls something or assigns.

**A stress harness that recursed.** With `STRESS_TESTS=1`, the stress
test runs each changed package's tests with `go test`, which inherited
`STRESS_DIR` and `STRESS_TESTS`: a package with its own stress test then
ran it on the same copy, which ran more tests, and so on. Dozens of
processes kept editing the copy (inlining variables, extracting
functions) under the running workspace, which then panicked on a stale
syntax tree. `slidestmt`'s stress test now leaves `STRESS_*` out of the
child's environment; the other stress tests (`extractvar`, `inlinevar`,
`extractfunc`, `inlinefunc`, `invertif`) have the same flaw when run with
`STRESS_TESTS=1`. *Follow-up: all six now use one helper,
`refactortest.GoTestCommand`, that leaves them out.*

## 2026-10-18: summaries of the module's own functions

**Built with the tools, three hand edits.** `create_file` wrote the stub
and the test, `add_declaration` the analysis, `add_parameter` threaded
`*analysis.Effects` through `factsOf` and `addEffects` (passing `nil`,
then the real value with `replace_expression`). By hand:

- **a one-line function body:** the stub's `func (e *Effects) Func(...)
  *Summary { return nil }` could not be replaced: `replace_statements`
  needs whole statements on their own lines. Split by hand. Gap:
  `replace_declaration` (its entries predate `replace_statements`; this one
  does not).
- **a case clause's body:** replacing every statement of a `case` clause
  was refused ("cut the case clause"), since the last statement ends on
  the clause's last line; replacing the whole clause is refused too
  (`case` is not a statement). A bug in `replace_statements`. *Follow-up:
  fixed; a clause's body can be selected to its end, and selecting the
  `case` line itself is refused as a cut.*
- **an elided composite literal:** `replace_expression` cannot take
  `{true, false, true}` (a map value with its type left out), which does
  not parse alone. Gap: parse the expression in its context.

**Measured, and it barely helped.** On the same 5,569 statements of this
module, `move_statement_up` applied 521 times before and 526 after. The
remaining side-effect refusals: about 500 are calls into the standard
library, interface methods or function values; about 275 are module
functions that call such code; about 390 are writes through pointers or to
package variables. The module's own functions were not the bottleneck;
the standard library is. A breakdown of the refusals by callee before
building would have shown it.

## 2026-10-18: standard library knowledge for the effect summaries

**Measured first this time.** Before writing any code, the remaining
side-effect refusals were ranked by the innermost unknown callee:
`append` (120), `ast.Inspect` (96), `fmt.Sprintf`/`Fprintf` (148), file
system reads (105), `go/token` and `go/types` getters (~250), `n.Pos`
(48), sorts (75), `strings.Builder` (35). That list became the table in
`analysis/stdlib.go`, and `append`, which needs alias tracking, was left
for its own change. On the same 5,569 statements: 526 applied before,
560 after (521 on `main`).

**The tests caught an unsound intermediate step.** Once `ast.Inspect` no
longer counted as a side effect, a statement could slide past an
`ast.Inspect` whose visitor wrote a variable the other statement read:
`move_statement_*` skipped literal bodies. The refusal case written first
(`refuse_visitor_writes_used`) applied, and failed. Fixed in the same
change by walking the bodies of literals that run during the statement.

**A bug the measurement showed.** `o.Pos()` on a `types.Object` was on the
known list, but `Effects.Call` rejected interface methods before looking
there.

**By hand:**

- **dependent edits:** declaring `walk` and using it in two places of one
  100-line statement had to be one step (each step must type-check, so a
  declaration alone is "declared and not used"). Gap: several edits
  applied, and checked, as one step.
- **elements of a map literal** (the standard library table's last
  entries): the composite-literal gap again.
- The running MCP server predates the `doc` argument and `Type.Method`
  placement from #65, so those were not available; restarting it after
  a merge would have made them so.

## 2026-10-18: story, side effects in `inspect`'s outline

The first feature story after a run of tool work: each function's line
in the outline now ends with `no side effects` or `side effects: <why>`.

**The workflow held.** One refactoring made the change easy
(`extract_function` pulled the outline loop out of `File`, complexity 18);
then `add_field` (`Decl.Effects`), `add_declaration` (`addEffects`), two
`replace_statements` and two `replace_expression`s made the change, test
first.

**Reading the feature's output on real code found a bug.** `Outline`,
`Metrics` and `recvTypeName` were reported as "assigns to d": the symbolic
variable of a type switch (`switch d := x.(type)`) has a nil `Defs`
entry, and both `analysis.Effects` and `move_statement_*` took a nil
entry for an assignment. Fixed with `analysis.Defines` (an entry at all);
a variable `:=` redeclares is in `Uses`, which was checked first. Also
missing: `ast.NewIdent` (allocation only).

**By hand:**

- **import grouping:** `add_declaration` put `golang.org/x/tools/go/packages`
  in the module's own group; this repository keeps other modules in a group
  of their own. Gap: the import adder knows standard library versus the
  rest, not third-party versus this module.
- **a long doc comment:** `extract_function`'s new `doc` was written on one
  110-column line. Gap: wrap it like the rest of the comments (about 75
  columns).
- **a map entry** in the standard library table (the composite-literal
  gap), and prose in tool descriptions and the README.

**My mistake, again:** two edits sent in parallel to the same file, the
second with line numbers from before the first. The refusal said what was
cut; edits to one file should be sent one at a time.

## 2026-10-18: addressing declarations by name

**Twelve JSON schemas edited by hand.** Making `line` optional and
documenting the symbol form changed text inside the raw string literal each
`InputSchema` returns. `replace_expression` would have to restate the whole
literal, which costs more than the edit. Gap: an edit of text within a
string literal (or schemas built from shared pieces, so one edit covers
every tool).

**The lookup itself was written by hand.** The `symbol` function and the
branch in `locate.Ident` that calls it were a hand edit; `add_declaration`
and `insert_statements` would have covered them, and should have been used.

## 2026-10-18: a token baseline (`refactor-tokens`)

**New files written with Write.** `internal/tokens` and its test were
written by hand; `create_file` covers both and was used for the command.

**First baseline (10 sessions of this project).** Bash is most of the
by-hand cost (reading code with cat/sed/grep as well as running tests).
Among the MCP tools, `create_file` and `add_declaration` return about as
many tokens as they were sent: the diff echoes the new code back. Gap: an
edit's result could summarize code the model just wrote instead of
repeating it.

## 2026-10-18: not echoing code the model wrote

**All through the MCP.** Stub, tests, helpers and implementation went in
with `add_declaration`, `insert_statements`, `replace_statements`,
`replace_expression` and `add_field` (addressed by name, without a line).

**Adding a key to a composite literal.** Giving two `refactor.Plan`
literals a `Supplied` field meant restating each whole statement with
`replace_statements`. Cheap for a four-line literal. Gap: an edit that
adds an element or key to a composite literal (like `add_field` for a
type).

## 2026-10-18: the A/B harness (`refactor-ab`)

Written through the MCP: `create_file` for the stub and test, then
`add_declaration`, `replace_statements`, `replace_expression`, `add_field`.

**A new local and its use cannot be separate steps.** Adding `cost` and
`turns` to `tokens.Analyze` (declared before the loop, set in it, used
after it) was refused in each order: Go rejects a local that is declared
and not used, and a use of one not yet declared. Done as one
`replace_statements` over the whole loop (50 lines restated). Gap: a batch
of steps type-checked as a whole; or an edit that inserts in several
places at once.

**Added imports shift the line numbers below them.** Filling three stubs
bottom-up is meant to keep earlier line numbers valid, but an edit that
adds an import moves everything after the import block; one
`replace_statements` was refused for it. Gap: name-based addressing for
statements (1c), or a result that says how far lines moved.

**A case clause cannot be added to a switch.** The `case "result":` went
in with the same whole-loop replacement. Gap: an edit that adds a case
clause.

## 2026-10-18: compact results

Done through the MCP: `extract_function` pulled `Unified`'s body out as
`UnifiedContext`, `move_parameter` (by name) put `name` first, and
`introduce_parameter` turned the 3 lines of context into `ctx`, with
`Unified` passing 3.

**`extract_function` output needed tidying by hand.** The doc comment was
one 120-character line, not wrapped like the file's comments, and the
parameters came out as `before []byte, after []byte` (and in the order
first used, so `name` last). Gap: wrap a given doc comment; group
adjacent parameters of the same type.

**`all_occurrences` replaced the new function's own body.** Replacing
`c.Diff` with `diffOf(ws, c, opts)` everywhere in the file also replaced
the `c.Diff` inside `diffOf`, which made it call itself forever. It
type-checked, so it was applied; undone and redone one call at a time.
Gap: `replace_expression` could refuse (or skip) an occurrence inside a
function the new expression calls.

**A flag cannot be added as a local.** `flag.Bool` in `main` is unused
until the line that reads it, so it was declared as a package-level
variable instead (allowed while unused). Same gap as the A/B harness:
a declaration and its use in one step.

## 2026-10-18: the batch tool

Built through the MCP (stub, test, body for `workspace.Squash`,
`refactor.RunBatch` and the `batch` tool); the server instructions, a
string literal, were edited by hand as before.

**Inserted code is echoed back.** `insert_statements` returned the whole
32-line tool registration it was given; only `create_file` and
`add_declaration` leave out code the model supplied. Gap: the other edits
(`insert_statements`, `replace_statements`, `add_field`) set
`Plan.Supplied` too.

**A parallel edit used a line number the other edits then moved.** Sent
together, a `replace_expression` at line 227 was refused (the text was on
226 by then) while the others applied. The refusal named the right line,
so the retry was cheap; a batch addressed by name would not have had the
problem.

## 2026-10-18: the mcp arm's overhead on feature work

The A/B feature runs (mcp arm: every edit through gorefactor) cost about
twice the manual arm. The causes, by count: Bash edits the arm was
denied (sed/python on JSON schemas in string literals, fixture
directories made with mkdir and cp), arguments the tools do not take
(add_field called with type/struct), and read-only Bash outside the
allowed list. Fixed:

- `replace_text`: exact old/new text in any file of the module (string
  literals, comments, go.mod, JSON fixtures), type-checked like every
  edit; addressing text, it stays valid in a batch.
- A refusal for arguments a tool does not take lists the tool's
  arguments with their descriptions.
- `create_file` says it creates any kind of file, and that several (a
  fixture directory) go in one batch. Its 29-file use here, one call and
  one undo, is how the replace_text test fixtures were made.
- The harness lets the mcp arm's Bash cd, cat, head, tail and wc
  (redirects and tee are still denied).

**A one-line function body cannot be replaced with replace_statements.**
`func (*T) Description() string { return "" }` shares its line with the
declaration, so the statement range is refused; `replace_expression` on
the returned value works. Registering the new tool in the `All()` list
(an element of a composite literal) and two description edits were done
by hand: exactly what `replace_text` now covers.

## 2026-10-18: line numbers in a batch

**Built with the batch tool's own limitation in the way.** Adding a test
helper and then replacing a test body by its line numbers could not go in
one batch: the helper changed the file, so the line-addressed step after
it was refused as stale (the rule this change replaces). Done as two
calls; an added import then shifted the lines once more.

**A test function could not be renamed.** `rename` of
`TestBatchRefusesStaleLines` was refused: "a reference ... is in
~/.cache/go-build/..., outside the workspace". The workspace loaded the
main package go generates to run the tests (`p.test`), whose source in
the build cache refers to every test by name; after a rename by hand it
went stale and made every tool refuse with a type error in that file.
Fixed: the workspace leaves those generated packages out. The running
server still had the bug, so the rename was done by hand at the end.

**The running server blocked its own fix.** After the test rename by
hand, the session's server (built before the fix) refused every tool
with the stale test-main error, so the batch-argument help below was
written with Edit. Reconnecting to a server built from this branch
removes the problem.

**Batch arguments put beside the steps.** In the A/B runs a batch was
refused three times with `unknown field "file"`: a step's argument sent
at the top level. The refusal now says what batch takes and that a
tool's arguments go in each step's args.

## 2026-10-18: chained tasks in the A/B harness

Restructured `runOne` through the MCP before adding stages: extract
`runStage`, give it a `Stage`, make the stream path a parameter, then add
the multi-stage version beside the old one, switch the caller, remove the
old one as dead code and rename (one batch of four steps).

**Over-cautious refusals.** `extract_function` rightly refused a range
with `defer cancel()`. Three refusals were wrong for this code:
`remove_parameter` refused an `err` parameter whose value the body never
reads (every use is `x, err := ...`, which overwrites it);
`add_parameter` refused the value `t.first()`, a method of the package
that only builds a struct (the effect summaries `inspect` shows know
that); `introduce_parameter` refused `fmt.Sprintf` of strings and ints as
having side effects. Gaps: dead-on-entry parameters; module functions
without side effects as values; `fmt.Sprintf` with arguments that cannot
be Stringers. Worked around with a composite literal and one
`replace_text` over contiguous text; the `err` removal (signature plus
call, which only type-check together) was done with Edit.

**Extracted functions take parameters in an odd order** (`ctx` in the
middle); `move_parameter` fixes it, by name, in the same batch.

## 2026-10-18: follow-ups from the backlog

- **Edits no longer echo the model's code:** `insert_statements`,
  `replace_statements` and `add_field` mark their content as supplied,
  like `create_file` and `add_declaration`.
- **Signature changes list locations:** `add_parameter` and the other
  `changesig` tools report where calls changed, as `rename` does.
- **Calls without side effects are judged by what they do:**
  `add_parameter`'s value, `remove_parameter`'s dropped arguments and
  `introduce_parameter`'s expression now use the effect summaries
  (`analysis.Effects`): the module's own functions by their bodies, and
  `fmt.Sprintf` of values that cannot have methods. Two old fixtures
  turned out to be false positives (`fmt.Sprint` of a string, a
  `next()` that only returns a literal) and now test real side effects.
- **A parameter whose value is never read can be removed:** when the
  first statement that mentions it assigns it (`n, err := ...` or
  `err = ...`), `remove_parameter` removes it, turning `=` into `:=`.

Still open: adding a case clause, an element of a composite literal, a
one-line function body; a declaration and its first use in one step.

## 2026-10-18: a batch checked at its end

Follow-up to "a declaration and its use in one step" (hit four times):
`batch` takes `check: "end"`, so edits may pass through code that does
not compile yet and the whole batch is type-checked once, at its end.
Its test could not be added in one step by the server it was being
built for: the test used the new `Options.Unchecked` field before it
existed, so the field went in first.

## 2026-10-18: the rest of the tool backlog

- **A one-line function body:** `replace_statements` takes `function`
  (a name, or `Type.Method`) in place of start_line/end_line and
  replaces the whole body.
- **A case clause:** `add_case` adds clauses at the end of a switch or
  type switch, or before/after one of its clauses (a comment above the
  clause stays with it).
- **An element of a composite literal:** `add_element` adds a field,
  map entry or value at the end of a literal or before/after a key,
  on its own line or inline, as the literal is written.
- **A declaration and its first use:** covered by `check: "end"`.

Registering `add_case` and `add_element` was done by hand: the
running server predated `check: "end"`, so the import and its use
could not be separate steps.

## 2026-10-19: quality measures in the A/B harness, all through the MCP

Done entirely with gorefactor, no hand edits: extract_function plus
move_declaration (ReadModule from eval to metrics), tests and stubs
created together, then bodies filled by name, in batches with
`check: "end"`. `check: "end"` removed the usual stub-first dance: a
test file and the stubs it calls went in one call. One refusal:
create_file has no `imports` argument (the imports went in content).

## 2026-10-19: create_from_usage, the first intention tool

All through gorefactor. Notes:
- A test that calls the code it tests could not be added on its own
  (`unknown field Choices`): the field and the test went in one batch
  with `check: "end"`.
- Registering the new tool was one `add_element` call, which added
  the import too; add_case and add_element had to be registered by
  hand.
- `batch` input with literal tab characters inside JSON strings was
  rejected by the client as invalid JSON; escaping them as `\t` worked.
- `extract_function` named a parameter `p` after the case variable it
  came from; a rename followed.

## 2026-10-19: create_from_usage for methods and interfaces

All through gorefactor. Notes:
- Threading a set of reserved names into `parameters` was
  introduce_parameter (the `map[string]bool{}` literal), inline_variable,
  then rename. The inline could not share a batch with the introduce:
  it addresses a line the introduce rewrote. Addressing a local by
  function and name would let it.
- One batch was refused at its end on a single wrong API guess
  (`types.WriteSignature` takes a *bytes.Buffer); with `check: "end"`
  the whole 8-step batch had to be resent. A way to resend with one
  step changed would save that.

## 2026-10-19: locals by function and name

Follow-up to "the inline could not share a batch with the introduce":
inline_variable and rename take `function` in place of `line`, so a
local, parameter or result is found by name in its function, which
stays valid after earlier steps of a batch. Twice while building it I
sent a batch whose steps only type-check together without
`check: "end"`; a step refused for type errors in a batch now says to
send it that way.

## 2026-10-19: create_from_usage for generic types and one-line interfaces

All through gorefactor. Changing a parameter's type (qual
types.Qualifier to write func(types.Type) string, in three functions
and their calls) has no refactoring: it took 13 replace_text steps in
one batch checked at its end. A change_parameter_type refactoring, or
add_parameter followed by remove_parameter in one batch, would fit.

## 2026-10-19: change_parameter_type

All through gorefactor; the new tool was written with create_file in
one batch with its test, its registration (add_element) and its place
in the instructions. The batch hint for check "end" fired on its first
real use: a rename of err2 to err in two replace_text steps that only
compile together. The tool does not cover the case that prompted it
(a Qualifier parameter becoming a func(types.Type) string): that is
not a type change but a replacement of one parameter by another,
computed from it at each call.

## 2026-10-19: a cheaper fresh-load comparison

All through gorefactor, by hand only in that the tests and new file
were written whole with create_file. extract_function refused to
extract the dependency ordering out of updateIncrementally: "order is
used by a function literal that can run later, and after the selected
lines". The literal (visit) is declared in the selected lines and not
used after them, so it cannot run later; the refusal should only apply
when the literal escapes the selection. Done instead with
replace_statements and add_declaration. extract_function also passed
errs as a pointer because a closure appends to it, though the caller
never reads errs again; four replace_text steps made it a value. A
doc comment given to extract_function was not wrapped (see "Finish
code as the file does" in roadmap item 9).

## 2026-10-19: extract_function and a local recursive closure

Fixed the refusal above: a literal the selected lines assign to a
variable they declare, and only call directly (not by go or defer, not
from another of their literals), no longer counts as one that can run
later. All through gorefactor. Two frictions: the first batch was sent
without check "end" (the hint caught it), and add_declaration did not
find astutil by name although the module already imports it elsewhere;
it only looks in the standard library and the workspace's packages, so
a dependency of the module needs imports.

## 2026-10-19: references to what earlier steps of a batch made

All through gorefactor. An end-to-end test caught that a step adding
its first import changes two places of the file (the import and the
code), so a reference to it was refused; edits that write an import
declaration now count as none. replace_text does not add imports, as it
works on text, so a strings.HasPrefix it wrote needed a second step for
the import. extract_function's doc comment was again not wrapped.

## 2026-10-19: finishing touches

Fixed three frictions from today: extract_function wraps its doc
comment at the file's width (gocode.DocComment, for the other tools
later); imports for code an edit adds also come from what other
packages of the module import by that name (astutil); and a variable
only a literal of the selection changes, which nothing outside it sees
again, is passed as a copy rather than a pointer. All through
gorefactor. I sent a batch without check "end" for the third time
today; the hint is clear, but the step that needs it is usually the
first, which refers to a function a later step declares. A batch whose
first steps fail only on names its later steps declare could retry
itself with check "end" and say so.

## 2026-10-19: replace_text missed on indentation

A batch was refused because a replace_text step's `old` had one tab of
indentation where the file had two (the lines were inside a for loop in
gocode.DocComment); the batch rolled back the steps before it. I had not
read the file first. replace_statements with start_line/end_line would
have fit better: it addresses lines, not text, and re-indents its
content. Gap: when the exact match fails, replace_text could try a match
that ignores leading whitespace (if only one place matches), or show the
closest match in its refusal.

## 2026-10-19: move_declaration with_dependencies

All through gorefactor. Dry runs on real code found two more needs of a
move, both refused clearly: exporting fields the source package still
reads (changesig.call), and regexp.MustCompile of a constant counting as
a side effect (refactor.stepRef).

## 2026-10-19: fixes from the fresh-model A/B

A batch now checks every step's argument names against its tool's schema
before running any step, and refuses with all the problems and each
tool's arguments at once. A replace_statements range that cuts a
statement now suggests the whole-statement range to send. The server
instructions say to load every batch tool's schema in one request. All
through gorefactor, but two frictions on the way:
- A step after extract_function that addressed a line below the
  extracted code was refused as "rewritten by step 1". The batch follows
  lines with a line diff. extract_function moves the selected lines
  below the function, and the diff pairs the old lines with that moved
  block (41 lines), not with the rest of the function (33 lines), so the
  rest counts as rewritten. Following lines through the step's own edits
  instead of a diff would fix it. I used replace_text, which addresses
  text, instead.
- I sent a batch without check "end" for the fourth time; the
  auto-retry proposed above would have saved the turn.

## 2026-10-19: defaults that teach a fresh model

All code through gorefactor. The tests were appended with a heredoc, a
slip; add_declaration would have done it. Frictions:
- rename refused RunBatch → runBatch, rightly (mcpserver uses it). I
  extracted the body into runBatch instead, but extract_function put
  steps before ws in the parameters (order of first use). A
  move_parameter step fixed it; extract_function could keep the order
  of the enclosing function's parameters.
- The new auto-declare found a real limit: create_from_usage refuses a
  whole file when one name in names is not a call (an undefined
  variable), and when one call's results can't be told. The batch now
  drops names that are not called and passes on any other refusal as a
  note.

## 2026-10-19: following lines through a step's edits

All through gorefactor. The batch's line diff did worse than refuse: in
a test, a step after an extract that moved code below the function was
put into the moved copy, silently. Lines are now followed through the
step's own edits, with a line diff only for what gofmt changed after
them. Frictions:
- create_from_usage gave the stub LineMapOf a result of type S: the
  result was compared with slices.Equal, and it took that generic
  function's type parameter as the type. Passing results fixed it; it
  should not infer a type from a generic parameter.
- Its parameters were named p and p2 (after []byte(...) conversions);
  two renames fixed them.
- This server still ran the build before #114, so I sent
  create_from_usage myself.
- Replacing a parameter (before []byte by moved []int) took
  replace_text on the signature; replace_parameter (item 13) would do.

## 2026-10-19: stubs replaced, edits on code they fix

From the A/B rerun (docs/cost-history.md). All through gorefactor, but
one fixture line edited with sed. Found on the way: an incremental update
after undoing an edit that broke a file's package clause panicked (the
file's syntax tree has no position); it now reloads instead. The golden
harness let only fixes start from code that does not type-check; edits
may now too.

## 2026-10-19: add_field takes the type separately

All through gorefactor. replace_text still does not add imports
(strings.TrimSpace in a replaced block); I dropped the call rather than
add the import in another step.

## 2026-10-20: a tip to batch single steps

All through gorefactor: the test with add_declaration, the server change
in one batch of 8 steps (add_field, add_declaration, replace_statements,
insert_statements) with line numbers from one sed read, and these notes
with replace_text.

## 2026-10-20: the tip to batch single steps, removed

The code was reverted with git checkout of the two files from before
#119, a hand edit: gorefactor has no tool to remove a struct field or
to restore a file. The notes went through replace_text.

## 2026-10-20: inspect without bodies, and targets

All through gorefactor except two size checks: a temporary test file
written with a heredoc and removed, to print inspect's output on real
files (the running server is the old binary). replace_text refused a
field comment I gave as I wrote it, before gofmt aligned it; I grepped
for the aligned text.

## 2026-10-20: ranges that start at the `func` line

All through gorefactor, with the CLI (`refactor`) built from the branch:
the session worked in a cloud clone, and the MCP it had ran on another
checkout. extract_function and inline_variable pulled `locate.FuncName` out
of `locate.Func`, add_declaration added `locate.SignatureAt` and the two
refusals, insert_statements the checks, and replace_text these notes. By
hand: the golden cases. The CLI has no `batch`, so steps went one at a
time; an insert sent before the function it calls was refused, and an
import add_declaration added moved the next line number by one.

## 2026-10-20: schemas loaded alongside another call, shorter definitions

All through gorefactor's CLI: the test with add_declaration, the
add_declaration and replace_statements definitions with
replace_statements (`function` addressing their Description and
InputSchema methods), and the batch definition, the instructions and
these notes with replace_text. The batch definition is a literal inside
`New`, so only replace_text could reach it; its old text, about 2.6K
characters, had to be sent whole as `old`.

## 2026-10-20: create_file for files that are not Go, in the instructions

All through gorefactor: the test with insert_statements, the
instructions and these notes with replace_text.

## 2026-10-20: replace_text across several files

All through gorefactor: the fixtures with a batch of create_file, the
helper with add_declaration, Plan's new body with replace_statements
(`function`), the schema, description and docs with replace_text.

## 2026-10-20: expressions that can panic, item 8 of the review

All through gorefactor's CLI: `analysis.MayPanic`, its helpers and its
test with add_declaration, the four call sites and the docs with
replace_text. By hand: the golden cases (`in/` and `args.json`; `out/`
from `-update`).

## 2026-10-20: third-party A/B candidates

All by hand, and none of it a gorefactor gap: the new files are Python
scripts, a README and JSON data, and this session's gorefactor server ran
on the linked computer, not on this checkout. The roadmap and these notes
were edited with Edit for the same reason; replace_text would have done
them in a session with the server on the checkout.

## 2026-10-20: introduce_parameter after a guard, item 2 of the review

All through gorefactor's CLI: the two changes to `checkEvaluation` and
`riskOf`, and these notes, with replace_text. By hand: the golden cases
(`in/`, `args.json`, `refused.txt`; `out/` from `-update`).

## 2026-10-20: a step that breaks go.mod, items 6-7 of the review

All through gorefactor's CLI: `Apply`'s body with replace_statements
(`function`), the error, the tests and `Run`'s refusal with
add_declaration and replace_text. The workspace test could not go first:
it names a variable, `ErrBreaksLoading`, and only functions get stubs,
so the variable was declared before it (an edit that does not
type-check is refused).

## 2026-10-20: undo while the module does not load

All through gorefactor's CLI: the test and `notLoading` with
add_declaration, the handler with replace_statements (`function`), the
tool description and docs with replace_text. An edit through the CLI
gets no stubs for functions it calls (the server passes `Declare`; the
CLI does not), so the handler that called `notLoading` was refused until
`notLoading` was declared.

## 2026-10-20: the first third-party A/B tasks

By hand, and not a gorefactor gap: `tasks.py`, the fixes to the other
scripts, `tasks.json` and the docs are Python, JSON and Markdown, and no
Go changed. As in the PR before, the gorefactor server of this cloud
session ran on the linked computer, not on this checkout.

## 2026-10-20: remove_dead_code in a const group, item 1 of the review

All through gorefactor's CLI except the golden cases. Moving
move_declaration's `positional` closure into `analysis` took three
steps, and the last had to be one replace_text over the function's
middle: removing the closure and replacing its last use are refused
each alone (one leaves it used, the other unused). A batch would have
done both; the CLI has none. inline_function refuses a function value.

## 2026-10-20: merge_duplicate_function and optional parts, item 5 of the review

All through gorefactor's CLI except the golden cases: the key and its doc
comment with replace_text.

## 2026-10-20: rename that starts implementing an interface, item 3 of the review

All through gorefactor's CLI except the golden cases: `implementsRenamed`
with add_declaration, `checkInterfaces` with replace_statements
(`function`), and the docs with replace_text.

## 2026-10-20: rename and dependencies' interfaces, item 4 of the review

All through gorefactor's CLI except the golden cases: the dependency walk
and the doc comment with replace_text.

## 2026-10-20: inline_function and the left of an assignment, item 9 of the review

All through gorefactor's CLI except the golden cases: `lhsEvaluatedFirst`
with add_declaration, the shape decision and docs with replace_text.

## 2026-10-20: inline_function and goto, item 10 of the review

All through gorefactor's CLI except the golden case: `deadAfter` and its
doc comment with replace_text.

## 2026-10-20: inline_function and conditional uses, item 11 of the review

All through gorefactor's CLI except the golden cases: the two cases and
the doc comment with replace_text.

## 2026-10-20: changesig and synchronous closures, item 12 of the review

All through gorefactor's CLI except the golden cases: the helpers with
add_declaration, the three walks with replace_text. Two of the walks had
to be one replace_text each from `ast.Inspect(` to its closing `})`,
since wrapping the visitor changes both ends.

## 2026-10-20: extract_function and errors that can be nil, item 13 of the review

All through gorefactor's CLI except the golden cases. The loop fix
called `hasGoto` before it existed, which a CLI edit refuses (no stubs):
declared it first with add_declaration.

## 2026-10-20: remove_dead_code and methods called dynamically, item 14 of the review

All through gorefactor's CLI except the golden cases:
`dependencyInterface` with add_declaration, the refusal and docs with
replace_text. The dependency walk is now in rename and remove_dead_code
(changesig has its own `visible`); a shared one in analysis would do.

## 2026-10-20: rename and fields reached by reflection, item 15 of the review

All through gorefactor's CLI except the golden cases: the helpers with
create_file, `checkReflected` with add_declaration (before the call to it,
which a CLI edit cannot stub), the call and docs with replace_text.

## 2026-10-20: statement content that closes its block, item 16 of the review

All through gorefactor's CLI except the golden cases: both checks and
the docs with replace_text.

## 2026-10-20: undo and a created directory that is not empty, item 17 of the review

All through gorefactor's CLI: the test with add_declaration, `restore`'s
body with replace_statements (`function`), the docs with replace_text.

## 2026-10-20: relative paths at the module root first, item 18 of the review

All through gorefactor's CLI: the test with add_declaration, `Abs`'s body
with replace_statements (`function`), the comments and docs with
replace_text.

## 2026-10-20: introduce_parameter and a literal's own names

All through gorefactor's CLI except the golden case: both checks and the
docs with replace_text.

## 2026-10-20: extract_struct on the shared reflection helpers

All through gorefactor's CLI except the golden cases: the check's body
with replace_statements (`function`), its description and docs with
replace_text.

## 2026-10-20: one walk over dependencies' interfaces

All through gorefactor's CLI: the new file with create_file, changesig's
call with replace_expression (replace_text was refused: it does not add
the `analysis` import, the known gap), its old `dependencies` deleted
with remove_dead_code, and the two other walks with replace_text.

## 2026-10-20: one list of functions that run a closure

All through gorefactor's CLI with replace_text: the test, the merged
table, and the old list deleted together with its one use in `SyncLit`
(deleting the list alone was refused, since `SyncLit` still used it).
Ranges this long were built as JSON with a script, since the CLI takes
its arguments on the command line.

## 2026-10-20: the CLI runs batches

All through gorefactor's CLI: the test with create_file, `declarer` renamed
and moved to the registry with rename and move_declaration, `RunStep`
and `ParseBatch` added with add_declaration, the server's two bodies
replaced with replace_statements, and the CLI's `run` with replace_text
(one range, built as JSON with a script).

## 2026-10-20: introduce_parameter and a variadic parameter

The check through gorefactor's CLI (replace_text). The two golden
fixtures by hand, as usual, and `out/` from `-update`.

## 2026-10-20: create_file formats only Go files

The fix through gorefactor's CLI (replace_text); the golden fixture by
hand, since its `out/` is the unformatted file `-update` could not make.

## 2026-10-20: replace_expression removes imports it leaves unused

All through `refactor batch`, the CLI's new batch (#160): RemoveImports's
signature and its three callers in one batch with check "end", then
replace_expression's change. Its two new imports (`go/token`, `slices`)
went in as a replace_text of the import block, since replace_text does
not add imports (the known gap). Fixtures by hand, `out/` from `-update`.

## 2026-10-20: unfinished A/B runs

All through `refactor batch`: the two fields with add_field ahead of the
tests that use them, the test with add_declaration and replace_text,
then the fix in one batch of replace_text.

## 2026-10-20: lines gofmt realigned keep their place

All through `refactor batch`: the test with replace_text, the fix with
replace_text and add_declaration in one batch.

## 2026-10-20: argument aliases in a batch

All through `refactor batch`. The test's first try used `slices`, which
the test file does not import, and replace_text does not add imports;
`strings` instead.

## 2026-10-20: the review's item 25

All through `refactor batch`: each of the three fixes with replace_text
and add_declaration in one batch, imports (`unicode`, `unicode/utf8`,
`go/version`) as replace_text of the import block (the known gap).
By hand: the golden fixtures, and moving `builder_func_past_effect` to
`refuse_builder_func_past_effect` with git mv, since nothing renames a
fixture directory.

## 2026-10-20: one check for terminating statements

All through `refactor batch`. remove_dead_code could not delete
extract_function's four methods one at a time, since they call each
other (a refusal per step: "terminatingList is used at ..."); they went
as one replace_text of their range. A dead-code removal of a group that
only uses itself would have done it in one step.

## 2026-10-20: a smaller module overview

All through `refactor batch`; `path` added as a replace_text of the
import block (the known gap). Measuring the old overview's size took a
throwaway command, built and deleted by hand.

## 2026-10-20: refactor-tokens counts turns, refusals and reads; ab-analyze skill

All through gorefactor: the test with add_declaration (refused alone
until the fields existed, so in a batch with check "end" after
add_field), the fields with add_field, the new functions with
add_declaration, report, Merge and sortTools with replace_statements
(`function`), the rest and the skill with replace_text and create_file.

## 2026-10-20: replace_text adds imports

All through `refactor batch`, and for the last time with the import
block replaced by hand-written replace_text (`go/ast`, `go/parser`,
`go/token`, `slices`, `gocode`, `imports`): that is the gap this fixes.

## 2026-10-20: a worked batch in the instructions

All through `refactor batch` and replace_text; nothing by hand.

## 2026-10-20: the mcp arm's CLAUDE.md on Bash

All through `refactor batch` (create_file for the three files,
replace_text for CLAUDE.md); nothing by hand.

## 2026-10-20: create_from_usage's names and generic results

All through `refactor batch`; the golden fixtures and the one expected
choice by hand.

## 2026-10-20: inline_function all_calls

All through gorefactor: the fixtures with a batch of create_file, the
engine's Expander, runExpanded and the inliner's Expand and callsOf
with add_declaration, the arguments with add_field, the schema,
description and docs with replace_text.

## 2026-10-20: batches follow columns

Through gorefactor, but for one thing: the fixture of the same-line
refusal, which became a success case, was removed with `git rm`;
nothing removes a file. PosMapOf began as a stub in a batch with its
test, so the test could fail first.

## 2026-10-20: stubs for types, values and aliased imports

All through gorefactor: fixtures with a batch of create_file, the new
functions with add_declaration, fields with add_field, declareIn with
replace_statements, the rest with replace_text; `qualifier`, left
unused, went with remove_dead_code.

## 2026-10-20: stubs for fields

All through gorefactor: fixtures with create_file, the new functions
with add_declaration, fields with add_field, editDistance with
replace_statements, the rest with replace_text.

## 2026-10-20: imports fixed after every edit

All through gorefactor: the package and its fixtures with create_file,
the Plan with replace_statements, the engine's wiring with add_field,
add_declaration and replace_text. A fixture's expected import grouping
was wrong (the module's import goes in its own group), fixed with
replace_text.

## 2026-10-20: signature tools change interface families

Through gorefactor but for one thing: four fixtures that expected a
refusal now apply (the interface changes with its implementations), so
they were renamed with `git mv` and their refused.txt removed with `git
rm`; nothing renames or removes a file. Their expected output was
written with create_file, the descriptions of three tools with one
replace_text over `files`.

## 2026-10-20: add_result

All through gorefactor: fixtures with create_file (the test's call of
NewAddResult got a stub, replaced in the same batch that created the
file), the tool with create_file, the rest with replace_text. Three
tools' descriptions had a sentence inserted mid-list by the last PR,
fixed with one replace_text over `files`.

## 2026-10-20: a signature changed by text points to the tools

All through gorefactor: the test with add_declaration, the note and its
use in Run with add_declaration and replace_text.

## 2026-10-20: change_field_type

All through gorefactor: fixtures and the tool with create_file (the
test's call of NewChangeFieldType got a stub, removed in the batch that
created the file), a fixture's column with replace_text.

## 2026-10-20: refactorings toward a test written first

All through gorefactor: the test with add_declaration, the rule in Run
with replace_text and add_declaration.

## 2026-10-20: the workspace loads when the server starts

All through gorefactor. The test receives from what Preload returns
(`<-s.Preload()`), from which create_from_usage cannot tell the
result's type; it was given in `results` in the same batch.

## 2026-10-20: A/B quality numbers when a file does not parse

All through `refactor batch`. The test used `Quality.Unmeasured` before
it existed, and the new field stubs declared it (#183), which
replace_text then gave its tag and comment.

## 2026-10-20: change_parameter_type writes a parameter with an alias

All through `refactor batch`; the golden fixtures by hand, their `out/`
written by hand first and matched.

## 2026-10-20: change_type

All through `refactor batch`: the package and its test with create_file
in one batch, the engine's note with replace_text and add_declaration,
the workflow test with add_declaration and its imports. One refusal: a
call of `plural`, which the refactor package names `textutil.Plural`;
the stub could not be declared, since its result was unknown. The
golden fixtures by hand, their `out/` written by hand first.

## 2026-10-20: loading with -trimpath

One line through replace_text. Timed by hand in the scratchpad: fresh
copies of Hugo with and without GOFLAGS=-trimpath.

## 2026-10-20: stubs for a call used as a case

All through gorefactor: the fixture with create_file, the case in
`expected` with replace_text.

## 2026-10-20: stubs declared before a refactoring

All through `refactor batch`: the tests with add_declaration, the
engine's change with replace_text. One wrong test, mine: it addressed a
line the first step had rewritten, which the batch rightly refused.

## 2026-10-20: checkpoints in a batch

All through `refactor batch`, the code and its tests in one batch of
nine steps. Two hitches. Stubs were not declared for what the test, in
the external package `refactor_test`, used before it existed
(`refactor.NewCheckpoint`, the fields `Kept` and `Options.Test`): the
batch was refused with the errors instead, so the code went in the same
batch. And Go string escapes (`\n`) in a batch built with an unquoted
shell heredoc became newlines, so one replace_text did not match; the
heredoc must be quoted.

## 2026-10-20: stubs past a name that cannot be typed

All through `refactor batch`; the golden fixture's `out/` by hand first.
One refusal in the way: a replace_text whose new code used
`untypeable.FindStringSubmatch`, a variable the batch's next step
declared, was refused as an unknown package to import; the steps went
in the other order. On the roadmap (item 18).

## 2026-10-20: replace_text leaves a later step's name alone

All through gorefactor: the test with add_declaration, the error type
with add_declaration, the rest with replace_text.

## 2026-10-20: fold

All through gorefactor: the fixtures and a stub of the tool with
create_file (the test alone was refused, its package having no other
file, which the refusal said), the tool with add_declaration and
replace_statements in one batch checked at its end.

## 2026-10-20: tasks that call for the type-error help

All through gorefactor: the seeded module, the hidden tests and the
task file with create_file, the hidden tests' renames with replace_text.
The reference solutions that validate the tasks are a scratch script,
not committed.

## 2026-10-20: move_file and delete_file

All through `refactor batch`; the golden fixtures by hand, as files for
these tools to move and delete. The tools' own fixtures are the first
that could have been made, and later renamed, with them.

## 2026-10-20: declarations replaced and removed by name

All through `refactor batch`. remove_dead_code's `also`: the fields with
add_field, the helpers with add_declaration, Plan's body with
replace_statements (`function`), the checks with replace_text.
replace_declaration with create_file. Golden fixtures by hand, `out/`
written by hand first; one expected line number of mine was off by
one, which the refusal showed.

## 2026-10-20: the CLI reads JSON from a file or stdin

All through `refactor batch`; this entry and the docs went in with
`@path`, the new form, from a file written by a script.

## 2026-10-20: the friction log in order

By a script, a hand edit: each entry was dated by the commit that first
added its heading (`git log -S`), and the entries were written back in
that order, every line kept. No tool moves blocks of text within a
file; replace_text would have had to restate the whole log.

## 2026-10-20: the algebraic measure

All through the `refactor` CLI built from the branch (the session's MCP
served the main checkout, and the work was in a worktree), with batches
of add_declaration, add_field, replace_statements and replace_text sent
as JSON files. A local type unmarshalled from JSON first had unexported
fields, so its arguments were silently empty; the test caught it.

## 2026-10-21: fold, stage 2: struct parameters used by their fields

All through gorefactor: fixtures with create_file; the struct and its
helpers with add_declaration; try and first replaced whole with
replace_declaration (#204), the rest with replace_text, in one batch
checked at its end.

## 2026-10-21: fold, stage 3: statement bodies

All through gorefactor: fixtures with create_file, the statement
matching and the shared call builder with add_declaration, the rest
with replace_text in one batch checked at its end. A fixture was wrong:
`a+": "+b+"!"` is not the tree of `a+": "+text`, which the fixture now
reaches the way the derivation does, through a variable.

## 2026-10-21: parameterize

All through gorefactor: the package's stub, test and fixtures with
create_file, the tool with replace_declaration and add_declaration in
one batch checked at its end.

## 2026-10-21: roadmap item 20, loops and containers

All through `refactor batch` (replace_text in README, the session
notes and this log), from one JSON file.

## 2026-10-21: the derivation experiment

The arm's CLAUDE.md files were copied with `cp` (nothing copies a
file), then the derivation section added with replace_text; the tasks,
hidden tests and docs through gorefactor. The reference solutions that
validate the tasks are a scratch script, not committed.

## 2026-10-21: the derivation experiment on code with many uses

The seeded module `notify-large` (388 lines of test data) was written
by a generator script in the scratchpad, not through gorefactor; the
tasks, hidden tests and docs through gorefactor. Validating against
reference solutions caught a wrong expected value in a hidden test,
fixed with replace_text.

## 2026-10-21: extract_variable with pointer

All through gorefactor: fixtures with create_file, the option with
add_field, replace_statements, replace_text and add_declaration in two
batches; fold's message for statement bodies in one more. The
round-trip test (extract, then inline back) asked for the inverse:
inline_variable now inlines a pointer alias, with the address check
moved to `analysis`, in one batch with remove_dead_code. Tried on a
copy of notify-large with `cmd/refactor`: the shared-record derivation
now needs the alias in every Send method before fold matches them
(gap O).

## 2026-10-21: gap K as fold through a setter

Docs only, all with replace_text.

## 2026-10-21: six runs per task on notify-large

Docs only, all with replace_text. The counts of lines written and
steps writing nothing are a scratch script, not committed.

## 2026-10-21: fold literals through a setter (gap K)

All through gorefactor: the fixtures with create_file in one batch,
the setter with add_field, add_declaration and replace_text in another
(the first refused: `after` named a method without its type, so it
wanted `folder.assignedHole`), the docs with replace_text.

## 2026-10-21: the project history through 21 October

All through `refactor batch`: replace_text steps in
`docs/how-this-project-was-built.md` and this log, from JSON files.

## 2026-10-21: remove a struct field (gaps L and N)

All through gorefactor: fixtures with create_file (a struct written on
one line with two fields was gofmt-ed onto lines, so that fixture
became a one-field struct, by replace_text), the field support with
replace_text and add_declaration in one batch checked at its end, the
docs with replace_text. A batch of docs was refused once: its README
text was from the K branch, not main.

## 2026-10-21: the setter fold qualifies elided types

All through gorefactor: the fixture with create_file, the change with
replace_text, the results with replace_text. The run analysis was a
scratch script, as before.

## 2026-10-21: Contacts rerun

Docs only, all with replace_text.

## 2026-10-21: fold onto a function getter

All through `refactor batch`: the two golden cases with create_file,
the fix with replace_text and add_declaration, the docs with
replace_text. Found by reading the `contacts-q-guided` transcripts:
two runs lost turns to the bug.

## 2026-10-21: roadmap item 21, experience for a fresh model

Docs only, all through `refactor batch` with replace_text.

## 2026-10-21: exploration runs set up

By hand: the `billing-large` module, its five hidden tests and the
explorer's CLAUDE.md, written with heredocs in Bash, and the task file
and the lesson script with Python; the docs too. create_file would have
done the fixtures, one call per file; I wrote them in the shell to check
each hidden test against a reference solution in scratch copies as I
went, which no tool does (roadmap item 21's `try` would).

## 2026-10-21: exploration results and Contacts one field at a time

The results docs of #228-#230 were written with Python in Bash, before
the MCP was reconnected in this session; since then, all edits with
replace_text, and the explorer copied with `cp` before its edits (no
tool copies a file). The Contacts derivation was run with the
`refactor` CLI on a scratch copy of the module: no hand edits but the
two getter lines it counts, and the field deletions by replace_text
after `remove_dead_code` refused falsely.

## 2026-10-21: move_declaration sees cycles through test files

All through gorefactor: the golden case with create_file and
replace_text, the fix with replace_text, the docs with replace_text.
Reproduced by replaying batch run 3's calls with the `refactor` CLI
(a script pulled them from the transcript).

## 2026-10-21: add_parameter and introduce_parameter qualify per call

All through gorefactor: the golden cases with create_file and
replace_text, the code with add_declaration and replace_text in two
batches, the docs with replace_text. Checked on a copy of notify-large
with the `refactor` CLI.

## 2026-10-21: extract_struct qualifies the new type in other packages

All through gorefactor: the golden cases with create_file, the fix with
replace_text and add_declaration in one batch, the docs with
replace_text. Checked on a copy of notify-large with the `refactor`
CLI.

## 2026-10-21: exported methods made, moved and dropped

All through gorefactor: the golden cases with move_file, delete_file,
create_file and replace_text, the code with add_declaration and
replace_text, rename's old list with remove_dead_code, the docs with
replace_text.

## 2026-10-21: extract_struct of an exported struct

All through gorefactor: the golden case with move_file, delete_file,
replace_text and create_file, the code and docs with replace_text.

## 2026-10-21: exported functions merged and inlined away

All through gorefactor: the golden cases with move_file, delete_file,
create_file and replace_text (one batch refused: a case name already
taken), the code and docs with replace_text.

## 2026-10-21: exported code, the API note

All through gorefactor: the golden cases with move_file, delete_file
and create_file, the code with create_file, add_declaration,
replace_declaration and replace_text in batches, the docs with
replace_text.

## 2026-10-21: a nil dereference past calls that return

The code, the golden cases and the docs through gorefactor (one batch
refused: insert_statements cannot add a case clause, so the switch was
replaced whole; a case header is changed with replace_expression).
By hand: three one-line `refused.txt` files written with printf, since
what the refusal would say was settled only once the code ran.

## 2026-10-21: fold through a pointer parameter

All through gorefactor: the golden cases with create_file, the code
with add_field, replace_statements, replace_expression,
insert_statements and add_declaration in one batch (refused once:
`analysis.MayPanic` returns a node, not an expression), the docs with
replace_text.

## 2026-10-21: fold matches text of a format

All through gorefactor: the golden cases with create_file, the code
with add_field, replace_statements, insert_statements and
add_declaration in one batch, the docs with replace_text.

## 2026-10-21: fold through string concatenation

All through gorefactor: the golden case with create_file, the code in
one batch (refused once: a range given by line that the batch placed
on bindText's signature; addressed by `function` instead), the docs
with replace_text.

## 2026-10-21: channel operations might not finish

All through gorefactor: the code with replace_statements (one body
refused: the content lacked its closing lines), the golden cases
with create_file, the docs with replace_text.

## 2026-10-21: fold evaluates a pure argument once

All through gorefactor: the golden cases with create_file, the code
with replace_text and add_declaration (one step refused: `range b` is
not an expression for replace_expression), the docs with
replace_text.

## 2026-10-21: a nil dereference before another panic

All through gorefactor: the code with add_field, replace_statements,
insert_statements, replace_text and replace_expression in batches, the
golden case with create_file, the docs with replace_text. A merge
conflict in this log (two entries at the same place) resolved with
replace_text. A second conflict there, merging main, resolved
the same way.

## 2026-10-21: fold and arguments that read shared memory

All through gorefactor: the golden cases with create_file, the code
with insert_statements and add_declaration in one batch, the docs with
replace_text.

## 2026-10-21: freight-xl

The module, its hidden tests and the task file were written by a
generator script (`eval-tasks/learning/gen_freight.py`), not through
gorefactor: about 1,100 lines of fixtures, which create_file would
have taken as one large content argument each. The docs with
replace_text.

## 2026-10-21: shorter descriptions, signature tools

All through gorefactor: each description with replace_statements and
`function`, eight in one batch; each example checked with the CLI.

## 2026-10-21: shorter descriptions, split and join

All through gorefactor: nine descriptions with replace_statements and
`function` in one batch, a phrase TestDescriptionsAreCurrent requires
put back with replace_text; each example checked with the CLI.

## 2026-10-21: shorter descriptions, moving and control flow

All through gorefactor: seven descriptions (six functions) with
replace_statements and `function` in one batch; each example checked
with the CLI.

## 2026-10-21: shorter descriptions, edits

All through gorefactor: thirteen descriptions with replace_statements
and `function` in one batch; the stub example checked with the CLI.

## 2026-10-22: cmd/calltree

All through gorefactor: the test and main with create_file (the stub of
`tree` declared by the batch), the code with replace_statements,
add_declaration and replace_text (two batches refused: a helper named
`types` clashed with the go/types import; a variable used by steps
before the one declaring it), the docs with replace_text.

## 2026-10-22: reading in the freight runs

The two scripts (`eval-tasks/learning/reads.py` and
`eval-tasks/learning/funcs/main.go`) were written in the scratchpad
while measuring and copied in with cp; the docs with replace_text.

## 2026-10-22: the call-tree probe

`eval-tasks/learning/probe.py` written by hand (a Python driver; one
edit to it with a Python string replace), the references' batches
copied from the scratchpad. `eval-tasks/learning/views` through
gorefactor (create_file, the stub of `view` declared by the batch,
rename of the stub's parameter, replace_statements, add_declaration;
one batch refused for using the parameter before the rename; one
replace_text refused because gofmt had realigned the map's keys, fixed
with the text as it was, and two assertions changed with sed).

## 2026-10-22: fold removes the imports it leaves unused

All through gorefactor: the golden case with create_file, the code with
replace_statements, add_declaration, add_field and insert_statements
in one batch, the docs with replace_text.

## 2026-10-22: function naming the function itself

All through gorefactor: the test cases with replace_text, the fix with
replace_statements and replace_text, the docs with replace_text.

## 2026-10-21: sort.Slice is not reflection on fields

All through gorefactor: the golden cases with create_file, the fix
with replace_text and add_declaration in one batch, the docs with
replace_text.

## 2026-10-22: a plan for deriving and reading less

All through gorefactor: the plan with create_file, the roadmap, the
session notes and this entry with replace_text, in one batch.

## 2026-10-22: set C, lending-xl

The module, its hidden tests and the task file were written by a
generator script (`eval-tasks/learning/gen_lending.py`), edited with
Python string replaces twice (feeds one line too long for gofmt, a
count in a test, then `Import` as three loops). The references are
batches run with the CLI, written by a Python script into
`eval-tasks/learning/refs/` (one fixed with sed: `check`'s argument
order). The algebraic arm copied from the baseline with cp, its section
added with replace_text; the docs with replace_text. Entries are
appended at the end of this file from now on.

## 2026-10-22: set C's runs

No code: the results in the docs with replace_text. From the runs, what
the models did by hand for want of an operation: on address, 10 of 12
runs called `rename` on the embedded field `Address` (refused, "not
supported yet") and then replaced `.Street`, `.City` and `.Zip` by
text; on kinds, every run wrote the types and methods and rewrote the
five switches by hand.

## 2026-10-22: inspect by name

All through gorefactor: the tests with add_declaration (the field
`Query.Names` declared from the test as a stub) and replace_text, the
code with add_declaration and replace_text in one batch, the docs with
replace_text. Trying it on lending-xl took a throwaway test file,
written with a heredoc and deleted: the CLI has no inspect.

## 2026-10-22: inspect's names grouped by file

All through gorefactor: the test with add_declaration, then one batch:
`rename` of `declaration` to `resolve`, `replace_declaration` and
`add_declaration` for the grouping, replace_text to make `File` call
the shared `declarations`. Trying it on lending-xl took a throwaway
test file again (the CLI has no inspect).

## 2026-10-22: inspect in the CLI

All through gorefactor: the test and the `stdout` variable in one batch
checked at its end, then the command with add_declaration (its import
added by the tool) and replace_text; the new code read back with
`refactor inspect` itself. Until now cloud sessions read code with
sed, grep and Read, since the CLI had no inspect.

## 2026-10-22: rename of an embedded field

All through gorefactor: the golden cases with create_file, the code
with replace_text and add_declaration in batches, the docs with
replace_text. One refusal cost a call: add_declaration's `after`
needs `renamer.checkReflected` for a method, not the bare name. Tried
on lending-xl and the extract_struct example with the CLI, in scratch
copies made with cp and printf.

## 2026-10-22: type_switch_to_method

Through gorefactor: the golden cases and the first version of the
package with create_file in CLI batches (written by a Python script
from files in the scratchpad: 46 steps, too long to send inline), a
stub file first replaced with delete_file and create_file; the fixes
with replace_declaration, add_declaration and replace_text. One
refusal: replace_declaration takes one declaration, so a new helper
went in a second step with add_declaration. Tried with the CLI on
scratch modules written with heredocs.

## 2026-10-22: change_field_type widened to any

All through gorefactor: the golden cases with create_file, the code
with replace_declaration, add_declaration and replace_text in one
batch, the docs with replace_text. Tried on a copy of lending-xl (cp)
with the CLI. The branch is stacked on #274 (merged into it with
git), whose docs it extends.

## 2026-10-22: add_interface_guard

Through gorefactor: first a refactoring of switchmethod (a switch of
either kind, the method's signature as one helper) in one batch, then
the golden cases, the moves of the type switch cases into their own
directory (move_file), the new file and the shared checks in one CLI
batch of 39 steps written by a Python script (too long to send
inline). One mistake there: move_file's argument is `file`, not
`from`, fixed in the JSON before sending. Tried on a copy of
lending-xl with the CLI.

## 2026-10-22: prepare_switch_for_types, the name

All through gorefactor: rename for the types and the test, move_file
for the file and its golden cases, replace_text for the strings and
docs.

## 2026-10-22: the tool budget

All through gorefactor: add_declaration for `TestToolBudget`,
replace_text for its limits and the docs.

## 2026-10-22: inspect uses

All through the CLI: add_declaration for the test first (its
`Query.Uses` stub declared by the batch), create_file for `uses.go`,
insert_statements into `Run`, replace_text for the field's tag and
comment, the tool's description and the docs. Two refusals on the
way: insert_statements takes `line`/`where`, not `function`, and
`into` needs the line the block opens on, which the stub field had
moved by one.

## 2026-10-22: prepare_switch_for_types splits shared cases

All through the CLI: create_file for the golden cases (written
first), add_declaration for `checkSplit`, `values` and `split`,
replace_text for the edits in `Plan` and `plan`, the description and
the docs. Hosting: an option of `add_case` was the first idea, but
`add_case` is an edit and the split is a refactoring; preparing the
switch for each value to move alone is the intent it serves, so it
needs no option at all.

## 2026-10-22: rename to a method that implements an interface

Through the CLI: create_file for `convert.go`, its test (first) and
the golden cases, add_declaration and replace_text for rename. By
hand: one `sed` adding `Instances` to the test's `types.Info`, a
field of a composite literal on a long line (add_element would have
done it).

## 2026-10-22: the recipe for a kind into types

Docs only, through replace_text. In the lending-xl run itself (a
copy, through the CLI), by hand: `type Book struct{}`, `kindOf`, three
expressions (`"book"` to `Book{}`), and five case deletions by
replace_text, for which there is no tool.

## 2026-10-22: change_field_type narrows to an interface

Through the MCP: create_file for the golden cases (written first),
rename of `analysis.generic` to `Generic`, add_field, add_declaration
for the new methods, replace_statements, insert_statements and
replace_text for the rest. Nothing by hand. Trying it on the lending-xl
copy found a bug the goldens missed: a test variant of a package has
its own `Item`, so `Book` did not implement the `Kind` the field was
changed to there (`newIn`); a golden case now has a method taking
`Item`.

## 2026-10-22: replace_statements deletes whole case clauses

Through the MCP: create_file for the golden cases (written first),
add_declaration for `caseClauses` and `deleteClauses`, replace_text
for the rest. Nothing by hand. `TestLoadedToolsStaySmall` refused the
first description by 24 characters; it lost its parenthesis.

## 2026-10-22: lending-kinds end to end, and the recipe arm

In this repo: create_file for the arm, replace_text for the docs;
nothing by hand. In the lending-xl copy (through the CLI), by hand:
the `DVD` and `Atlas` types, `kindOf`, 12 expressions (`Book.Label`'s
body, 3 comparisons, 8 test literals), `case "atlas": return false`,
3 lines of `Import`, and 5 empty switches deleted. One refusal from a
bug: `replace_expression` left `lending.Atlas{}` unparenthesized in
an `if` condition, which does not parse.

## 2026-10-22: replace_expression parenthesizes a composite literal in a header

Through the MCP: create_file for the golden case (written first),
replace_text for the fix (a call of `analysis.InControlHeader`, which
`inline_variable` already used) and the docs. Nothing by hand.

## 2026-10-22: the kind A/B results

Docs only, through replace_text. Nothing by hand.

## 2026-10-22: composite literals in a header, inside an expression

Through the MCP: create_file for the golden cases of both tools
(written first), add_declaration for `BareComposites`,
`BracketComposites` and `bracketed`, replace_text for the rest.
Nothing by hand.

## 2026-10-22: the steps for a signature changed as text

Through the MCP: replace_declaration of the server test (first) and
of `signatureNote`, add_declaration of `signature`, `signatures` and
`paramSteps`, replace_text for the docs. Nothing by hand.

## 2026-10-22: the kind A/B rerun

Docs only, through replace_text. Nothing by hand.

## 2026-10-22: change_field_type's modes into types

Through the MCP, read with inspect only: 39 refactorings in five
batches. As edits: `write` regrouped by mode (no tool regroups a
switch of conditions), the interface and types, the dispatch, the
`mode` field and its setup, two `IsInterface` checks, `info` to
`p.TypesInfo` in three places and a `nil` argument (no inverse of
`introduce_parameter`; `p.TypesInfo` refused by `inline_variable` and
`remove_parameter` as a pointer read), and the doc comments. One
stale line number of mine after a batch that added declarations
above it.

## 2026-10-22: reasoning from the highest abstraction

Docs only, through replace_text. Nothing by hand.

## 2026-10-22: examples from no eval task

Through the MCP: add_declaration for the test (first), replace_text
for the descriptions and the docs. Nothing by hand. The new examples
were run with the CLI on a scratch garden module.

## 2026-10-23: naming the level

The arms' instructions, the task file and the doc through create_file,
the results through replace_text. By hand: two one-line corrections
of a run count with sed. Seen in the runs:
`fold` did not reach a copy whose filter is written the other way
round (`!= Pending` around the append against `== Pending` with
`continue`), so all six runs replaced the copies with replace_statements.

## 2026-10-23: the axis, and the concept as a step

The arm's instructions and the two-session task file through
create_file, the results through replace_text. Nothing by hand. Seen
in the runs: no refactoring turns if-chains on a kind into a table of
rules per kind; both preparing sessions did it with
replace_declaration and the tests.

## 2026-10-23: the next steps for the design step

README and session notes through replace_text. Nothing by hand.

## 2026-10-23: the design step at 5 runs and on two more shapes

The hidden tests through create_file; the two task files through the
CLI's create_file with arguments written by Python; the docs through
replace_text. Nothing by hand. The reference solutions that validated
the tasks were made in a scratch copy, not the repo.

## 2026-10-23: does the design step pay back?

The task file (`eval-tasks/level-payback.json`, two existing tasks
joined) written by Python, and the question appended to
`docs/naming-the-level.md` with a heredoc before the run, by hand;
the results through replace_text. No Go changed.

## 2026-10-23: the second change on lending and freight

The hidden tests through create_file and replace_text. By hand: the
task file joined from the two existing ones by Python, the question
appended to `docs/naming-the-level.md` with a heredoc, and the plain
magazine/map and Air/Sea code that validated the tests, in a scratch
copy only.

## 2026-10-23: longer chains

The nine hidden tests through create_file. By hand: the task file
built by Python from the two pay-back files, the question appended to
`docs/naming-the-level.md` with a heredoc, and the plain
implementations that validated the tests, in scratch copies only.
The results through replace_text.

## 2026-10-23: bodies read and not used

The script and the docs through gorefactor (create_file, replace_text).
The transcripts were staged from the user's computer, and the script
written and tried in the scratchpad with heredocs and Python before
create_file put it in the repo.

## 2026-10-23: reads by name only

Through gorefactor: both tests first (`Options` declared with the
test, `RunWith`'s stub from create_from_usage with `results`, the
server's field stubbed from its test), the option threaded through
`Module`, `File` and `targets` with add_parameter, `RunWith` and `Run`
with replace_declaration, the hints and the server's texts with
replace_text. One line by hand: `cs := connect(t, s)` in the tests'
`setup` (replace_statements). `extract_function` refused the lines
that connect a client, twice: "cs ... is used by a function literal
that can run later, and after the selected lines; returning it would
return a copy", with and without the `t.Cleanup(func() { cs.Close() })`
line in the selection, though nothing assigns `cs` again, so the copy
is the same value. A false refusal. `fold` then could not take
`connect` (it has a result and statements before it), so `connect` was
declared and the one call written.

## 2026-10-23: the names arm

Through gorefactor: the test first (add_declaration), then the arms as
a table (`armTools`, add_declaration) with `claudeArgs` reading its
arm's row (replace_statements over the switch, checked by the existing
tests: no refactoring makes a switch on a kind into a table, the gap
the design-step runs met), then the names row and its Bash
(add_declaration, replace_text). The instruction files with
create_file, the docs with replace_text. The session note that #305
put inside an earlier note was moved to that note's end with
replace_text.

## 2026-10-23: where a session's tokens and cost go

The script with create_file and the docs with replace_text, through
gorefactor. Written and tried in the scratchpad first, with heredocs
and Python, on transcripts staged from the user's computer.

## 2026-10-24: folder.match's cases into matchers

Nothing by hand. `folder.match` (complexity 89) is a type switch on
go/ast node types, which belong to another package, so
type_switch_to_method does not apply; extract_function made its five
largest cases methods in one batch (match: 89 to 34; the largest new
one, matchIdent, 25), and move_parameter put their parameters in the
order of matchAddress and the others. inline_variable refused to inline
`pinfo := f.pkg.TypesInfo` first, rightly (methods called in between
could change `f.pkg`), so the matchers take `pinfo`, and the `sub`
closure, as parameters.

The user asked whether the change could be planned without reading
`match` first. From its signature, metrics and `uses` alone it could
not: extract_function needs whole statements, and probing with ranges
only gets refusals that quote the statements near the range, which is
reading the source a few lines at a time. Reading the one function
being refactored, and nothing else, was enough.

## 2026-10-24: roadmap item 27, one matcher for code with holes

Nothing by hand: the item with replace_text.

## 2026-10-24: fold's match state into `matcher` (roadmap item 27)

extract_struct moved the fields the matchers use into `matcher`,
embedded in `folder`, and rename named the receivers. By hand, with
replace_text in the same batch (check "end"): the receivers of the
twelve matchers, `(f *folder)` to `(f *matcher)`, one line repeated.
No operation moves a method to a struct its receiver embeds; through
the tools it would be method_to_function, an alias of folder and
change_parameter_type to it, change_type to matcher with a fix at each
call from folder's code, then function_to_method: four steps per
method for what is one line of text. A candidate: function_to_method
(or a move of a method) onto an embedded field, when the body uses
only what that field provides.

## 2026-10-24: fold's nearest miss (roadmap item 27)

Nothing by hand. The goldens with create_file and replace_text, first;
extract_function made `match` and `matchStmt` wrappers of
`compareExpr` and `compareStmt`; then the change with add_declaration,
add_field, replace_statements (the wrappers' bodies by `function`, the
call in `try` by lines) and replace_text for the loop in `foldStmts` and
the refusal in `Plan`, whose lines had moved. The roadmap planned a miss
*result* through add_result; state on the matcher needed no change at
the matchers' forty-odd returns.

## 2026-10-24: fold's matcher into `internal/pattern` (roadmap item 27)

Through the tools: method_to_function for the six relaxations,
extract_struct for their state (`relaxer`), add_parameter to pass it
(`&m.relaxer` was refused, rightly: it panics on a nil `m`; `nil` went
in and the calls were replaced in the same batch), add_declaration for
the `Relaxations` interface and `relaxer`'s methods, rename to export
what fold uses, and one move_declaration with `with_dependencies` for
the package. The test of the package with create_file. By hand, with
replace_text: the three writes from `m.addressOf` to `rel.addressOf`
and the like, the three calls of the relaxations through `m.Relax`
(behind a nil check), `relaxer` out of the matcher and into `folder`
(no tool moves a field between structs), and the docs that named fn.
Two gaps the move showed:
- rename refuses a type that is embedded (`folder` embedded `matcher`);
  renaming the field `folder.matcher` to `m` first made it a named
  field, after which the type could be renamed.
- move_declaration moved `HoleKey` to another package and left fold's
  unkeyed literals of it, `pattern.HoleKey{o, ""}`, which type-check
  but fail `go vet` (composites). Keyed with replace_expression, twelve
  sites. A move to another package should key them.

## 2026-10-24: parameterize on `pattern.Matcher` (roadmap item 27)

Through the tools: the test first (extract_function for its `check`
helper, add_declaration for the test, whose keyed literal declared
`Structural` and `Literal` as stubs), then add_declaration,
insert_statements and replace_statements for the matcher's structural
walk and the literal hook, replace_statements for parameterize's loop,
remove_dead_code for `comparer` and its methods. By hand, with
replace_text: the docs of the two new fields, two one-line changes in
the matcher (the literal hook in the `BasicLit` case, names without an
object), and `var posType = reflect.TypeOf(token.NoPos)`, which
remove_dead_code refused because it cannot tell `reflect.TypeOf` has no
side effects. A list of calls known to be pure would cover it.

## 2026-10-24: roadmap item 27's follow-ups

Docs only: replace_text for the roadmap entry. Nothing by hand.

## 2026-10-24: the attempt, `pattern.Attempt` (roadmap item 27)

Through the tools: the tests first (replace_declaration and
add_declaration), add_declaration for `Attempt`, `At`, `sub` and fold's
`matchIn`, replace_declaration for `Matcher`, `Relaxations` and
`keepNearest`, rename for the receivers and parameters `m` to `a` (22
steps), remove_parameter for `bindRun`'s unused matcher. By hand, as
twenty-odd replace_text steps (all_occurrences): turning the parameters
`info` and `b` of thirteen methods into the receiver's fields, that is
dropping them from every signature and call and writing `m.Info` and
`m.Bound` for each use, and the receivers from `*Matcher` to
`*Attempt`. No refactoring does this: it is introduce_parameter's
inverse (a parameter every call passes as the same expression of the
receiver becomes that expression), or a parameter object. Text
replacement worked because the names were distinct (`info` but not
`pinfo`, `b[` only for the bindings); on other code it would not be safe.

## 2026-10-24: two comparers in place of `Structural` (roadmap item 27)

Through the tools, as a derivation: extract_function for each compare
function's rules (past the `Structural` test), method_to_function,
add_parameter `k ByKind` and function_to_method to make them
`ByKind.Expr` and `ByKind.Stmt`; add_declaration for `Comparer`,
`Exactly` and `comparer`, add_field for `Compare`, replace_statements
for the two dispatching bodies, remove_dead_code for `Structural` and
`Literal`, inline_function for the one-line `compareExpr`. By hand,
with replace_text: deleting the literal hook's branch from
`ByKind.Expr`, and the two literals of `Matcher` that set `Structural`
and `Literal` (parameterize's and the test's) to set `Compare`. Moving a
method to another receiver type took three refactorings and a
parameter of an empty struct; a `move_method` would be one.

## 2026-10-24: what the pattern's names are, decided once (roadmap item 27)

Through the tools: extract_function for `bindHole`, add_declaration for
`name`, `nameKind` and `nameOf`, add_field for the cache,
replace_statements for `matchIdent`'s body. By hand, with replace_text:
the one line of `matchSelector` that looks up its operand's object, and
doc comments.

## 2026-10-24: calls known not to have side effects (roadmap item 27)

Through the tools: the tests and goldens first (replace_text,
add_declaration, create_file; the test's call declared `ReadOnlyCall`
as a stub), add_declaration for `readOnlyFuncs` and `ReadOnlyCall`.
By hand, with replace_text: the `reflect.TypeOf` entry of the table and
the one-line condition in remove_dead_code's check.

## 2026-10-24: roadmap item 28

Docs only: replace_text for the roadmap entry. Nothing by hand.

## 2026-10-24: what the tools add to a session (cost-history 13)

Docs only: replace_text for cost-history and the session notes, and
this entry appended with a heredoc. The measuring scripts were one-offs
in the scratchpad (transcript usage by arm; the server's tools/list).

## 2026-10-24: a cheaper model with the tools (cost-history 14)

Docs only: replace_text for cost-history and the session notes; one
figure corrected with sed, and this entry appended with a heredoc.

## 2026-10-25: a plan for a loop of our own

The plan with create_file, the roadmap, the session note and this entry
with replace_text, in one batch. Nothing by hand.

## 2026-10-25: tasks a model might get wrong by hand (cost-history 15)

The module, hidden tests and task file were made with create_file
steps in one CLI batch, and names the tool examples use (Move, Check,
Water, Discount) renamed with rename; prompts and tests followed with
replace_text. One replace_text of `Move` also changed `l.Moves` in a
hidden test, undone with a second replace_text: all_occurrences matches
text, not names. In a batch, `$1.end` addresses the line of the call
after an extract_variable, not the line after it; `$1.end+1` was
refused as the wrong line. This entry appended with a heredoc. Merging
main (#324, roadmap item 29 and its friction entry) conflicted in the
README and this log; resolved by hand with a script, this item
renumbered 30.

## 2026-10-25: move_parameter and private locals (roadmap item 30)

Golden cases with create_file; the change with add_parameter (readsVar's
`private`), replace_declaration, add_declaration and replace_text, in one
batch; the nested-order case's input and the roadmap with replace_text.
This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: Haiku's plans (cost-history 16)

probe.py's `-model`, `-schemas`, `-plan` and the file list with
replace_text (a Python file, which no Go tool edits); the docs with
replace_text. This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: the plans with bodies (cost-history 16)

Docs with replace_text; this entry appended with a heredoc. The
test-literal fixes added to the answers' batches were a one-off script
in the scratchpad. Nothing by hand in the repo.

## 2026-10-25: add_parameter's replace_variable (roadmap item 31)

Golden cases with create_file (one args.json corrected with replace_text);
the option with replace_text, replace_declaration (checkNewName onto
`hides`) and add_declaration in one batch; docs with replace_text. This
entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: the plan-and-execute loop (roadmap item 29, step 1)

loop.py with create_file (written in the scratchpad first), then its
fixes with replace_text (one refused for a docstring quoted wrongly, and
resent); the plan doc with replace_text. This entry appended with a
heredoc. Nothing else by hand.

## 2026-10-25: the loop without reads (cost-history 17)

`-no-read` in loop.py and the docs with replace_text. This entry appended
with a heredoc. Nothing else by hand.

## 2026-10-25: a batch's stubs and what a step removed

The test with add_declaration and replace_text; the fix with
add_declaration, replace_declaration and replace_text in two batches
(the first keyed fields by the using file's package, which missed fields
of another package's type); the plan doc with replace_text. This entry
appended with a heredoc. Nothing else by hand.

## 2026-10-25: the loop's "done?" turn

loop.py's attempt rewritten with one replace_text (Python; the new
function generated in the scratchpad), the prompts and flags with
replace_text; the plan doc with replace_text. This entry appended with a
heredoc. Nothing else by hand.

## 2026-10-25: the loop's compact tools reference

`compact` and `-reference` in loop.py, and probe.py's `tool_list`, with
replace_text (Python files). This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: the loop's cost accounting (cost-history 18)

loop.py's usage record, the session's cost from its last total, and a
guard for a line past a file's end, with replace_text; docs with
replace_text. This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: remove_dead_code names every use left

The golden case with create_file in one batch; `checkUnused` with
replace_statements (`function`) and replace_text; the plan doc with
replace_text. One replace_statements left a placeholder line, replaced
by the next. This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: the loop on routes and lending-xl (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: prepare_switch_for_types uses a method the interface has

Golden cases with create_file in one batch; `methodSig` by
extract_function (which made it a method on its own: a function_to_method
step after it was refused as redundant); the field, the check and the
plan's branch with add_field, replace_statements, add_declaration and
replace_text. This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: the narrowing refusal says what comes first

Golden cases with create_file and replace_text; the hint and
`narrowHint` with replace_text and add_declaration in one batch; the plan
doc with replace_text. This entry appended with a heredoc. Nothing else
by hand.

## 2026-10-25: the kinds rerun (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: kinds with reads (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: numbered reads in the loop (cost-history 19)

`eval-tasks/learning/loop.py` edited by hand (Python, outside what the
tools check). Docs with replace_text. This entry appended with a
heredoc. Nothing else by hand.

## 2026-10-25: a case addressed by its value

Golden cases with create_file in batches; `locate.CaseBody` with
add_declaration; extract_function's args and Plan, `caseLines`, and
replace_statements' case branch with add_field, replace_statements and
add_declaration; schemas, descriptions and docs with replace_text. By
hand: the kinds reference batch (`refs/lending-kinds.json`), generated
by a Python script and tried with the CLI, since no tool writes a
whole non-Go file over an existing one. This entry appended with a
heredoc.

## 2026-10-25: kinds with the case address (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: a bare word for a string case

The retry in `locate.CaseBody` and the roadmap with replace_text; the
golden cases with create_file. This entry appended with a heredoc.
Nothing else by hand.

## 2026-10-25: name-addressed steps in the plan prompt (cost-history 19)

The prompt line in `loop.py` and the docs with replace_text. This entry
appended with a heredoc. Nothing else by hand.

## 2026-10-25: the bare word keeps the case's own refusal

The fix with replace_text, the golden case with create_file. This entry
appended with a heredoc. Nothing else by hand.

## 2026-10-25: extract_function splits a case of several values

`split` with add_parameter (on `locate.CaseBody`), add_field,
add_declaration (`splitCase`) and replace_text; golden cases with
create_file. This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: a missing case's refusal says what runs instead

`instead` with add_declaration, the refusal and goldens with
replace_text and create_file. This entry appended with a heredoc.
Nothing else by hand.

## 2026-10-25: function_to_method takes an interface's method name

`InterfacesWithMethod`, `checkImplements`, `implementsWith` and
`becomesInterface` with add_declaration; the call sites, description and
goldens with replace_text and create_file. By hand: the kinds reference
without its renames (`refs/lending-kinds.json`), regenerated by a
Python script. This entry appended with a heredoc.

## 2026-10-25: function_to_method takes any parameter as the receiver

The receiver index, `methodDeclEdits` (replace_declaration), the call
edits and `receiverHint` (add_declaration) with replace_text; goldens
with create_file and delete_file. By hand: the kinds reference's
receiver arguments, by a Python script. This entry appended with a
heredoc.

## 2026-10-25: prepare_switch_for_types takes fields as the declared parameters

`supply`, `supplies` and the field with add_declaration and add_field;
checkDeclared with replace_statements; the plan's changes, description
and roadmap with replace_text; goldens with create_file. This entry
appended with a heredoc. Nothing else by hand.

## 2026-10-25: one note per interface a type starts to implement

The fix with replace_text, the golden's no_notes.txt with create_file.
This entry appended with a heredoc. Nothing else by hand.

## 2026-10-25: add_case adds branches to an if

`ifchain.go` with create_file, the hook, description and schema with
replace_text; goldens with create_file. This entry appended with a
heredoc. Nothing else by hand.

## 2026-10-25: roadmap item 33, output holes in the pattern

Nothing by hand: the item and this entry with replace_text.

## 2026-10-25: extract_function adds the case for a value with none

`missingCase` with add_field and add_declaration, the hook and the
plan's case edit with replace_text; goldens with create_file and
delete_file. By hand: the kinds reference (atlas's Renewable by case),
by a Python script. This entry appended with a heredoc.

## 2026-10-25: kinds with the refusals fixed (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: a batch's refusal gives lines as the batch began

`batchlines.go` and its test with create_file, `stepFile` with
add_declaration, the three call sites and the roadmap with replace_text.
By hand: the merge conflict in this file (main's entries and this one
kept). This entry appended with a heredoc.

## 2026-10-25: kinds with a missing case extracted (cost-history 19)

Docs with replace_text. This entry appended with a heredoc. Nothing
else by hand.

## 2026-10-25: one table of standard-library facts

The four tables merged into `stdFuncs` with replace_declaration, whose
content (the elements of knownFuncs, effectFreeFuncs and readOnlyFuncs
with their values rewritten) a Python script made from the old
literals: no tool moves the elements of one composite literal into
another. Lookups with replace_statements and replace_text, the old
tables with remove_dead_code; tests, the golden and the roadmap with
replace_text. This entry appended with a heredoc.

## 2026-10-25: a summary's findings say where and how it panics

`Where`, `Summary.at` and the body's four recorders with
add_declaration, `Summary` and `Summary.add` with replace_declaration,
the test with add_declaration, the panic kinds where summaries are
built with replace_text, `body.set` with remove_dead_code. By hand:
`body.visit`'s 34 calls of set rewritten to the recorders by a Python
script, sent as replace_statements: no tool replaces each call of a
function with a call of another chosen by its arguments (here, which
field the pointer names). A first try sent several declarations in one
replace_declaration, which takes one. This entry appended with a
heredoc.

## 2026-10-25: a summary of one expression

`Effects.Of` and its test with add_declaration. By hand, with
replace_text: `freshLocals` taking the function's body instead of its
declaration, since introduce_parameter refused `fd.Body` falsely
("reads memory that info := f.Info could change before it": a local
being defined counts as a write that could change what the expression
reads). That check is one of the analyses roadmap item 34 merges.
This entry appended with a heredoc.

## 2026-10-25: the callers onto the summaries

Through the tools: the call sites with replace_text (one batch of 38
steps), the helpers (`panics`, `movable`, `risky`, `Effects.Acts`,
`FirstActBefore`, `StableAddress`, `Summary.Stable`, `Outside`,
`Workspace.Effects`) with add_declaration, the workspace field with
add_field and its resets with insert_statements, the harness with
create_file and replace_declaration. By hand:
- The walker's new cases, written into `body.visit` by a script and
  sent as replace_statements.
- `Where.Panic` renamed to `OtherPanic` with replace_text: rename
  refused because a test prints a Summary with %+v, where reflection
  sees field names. The refusal is right in general; a test's message
  is not behavior worth keeping.
- add_parameter refused `m.ws.Effects()` and `t.ws.Effects()` as
  values ("has side effects": it makes the Effects on first use), so
  the parameters were added with `nil` and the calls then changed with
  replace_text. Step 5, which sees into the module's functions, would
  still refuse it: the lazy assignment is an effect.
This entry appended with a heredoc.

## 2026-10-25: summaries see into the module's functions

Seven goldens with create_file, the workspace's packages added with
replace_text, changesig's and fold's own Effects replaced by the
workspace's with replace_statements, remove_dead_code and
inline_function (all_calls), slide's and inspect's with replace_text.
By hand, by script through replace_text: fourteen goldens whose stand-in
functions were pure (`func load() int { return 3 }`) given a `println`,
and their `line` arguments and one refusal's position moved with the
lines that grew. This entry appended with a heredoc.

## 2026-10-25: the old analyses deleted

The five functions with remove_dead_code (one call, with also), the
differential test with delete_file, TestMayPanic ported and the walker's
division fixed with replace_text, the package comment and the roadmap
with replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: emptying a case the field may still reach is refused

The goldens with create_file in one batch, `analysis.FieldWrites` with
create_file, the check and its message with replace_text and
add_declaration (check "end"), the refusals' expected text and the
roadmap with replace_text. Nothing by hand. This entry appended with a
heredoc.

## 2026-10-26: loop.py reads only after a batch applied

`-read-after` and its two prompts with replace_text in one batch, then
the reply to a read asked for too early the same way. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: cost-history 19, kinds with reads after a batch

The results in cost-history, plan-and-execute and the session notes
with replace_text in one batch. Nothing by hand. This entry appended
with a heredoc.

## 2026-10-26: loop.py says nothing of reads before they open

The first prompt's sentence and the prompt that opens reads with
replace_text in one batch. Nothing by hand. This entry appended with a
heredoc.
The run's results in cost-history, plan-and-execute and the session
notes with replace_text in one batch, by hand none.

## 2026-10-26: introduce_parameter through an interface method

The goldens with create_file, the family path, `function` and the
description with replace_text and add_declaration (check "end"), the
kinds reference's two new steps and the docs with replace_text. By
script, then reverted: inserting the reference's steps with Python's
json reformatted the whole file; replace_text did it instead. This
entry appended with a heredoc.

## 2026-10-26: function_to_method's receiver hint for an undeclared type

The golden with create_file in one batch, the refusal's hint and the
roadmap with replace_text. Nothing by hand. This entry appended with a
heredoc.

## 2026-10-26: loop.py's first batch moves existing code

The new sentence and the run's results in cost-history,
plan-and-execute and the session notes with replace_text. Nothing by
hand. This entry appended with a heredoc.

## 2026-10-26: cost-history 19, kinds after #367, #371 and #372

The two runs' results in cost-history, plan-and-execute and the
session notes with replace_text in one batch. Nothing by hand. This
entry appended with a heredoc.

## 2026-10-26: extract_function splits a shared case by default

The default golden copied from case_split with cp (no tool copies a
directory of fixtures), its args and the other goldens' args and
refusal with replace_text; `split` a pointer, `splits` and the
messages with replace_text and add_declaration (check "end"); the
roadmap with replace_text. This entry appended with a heredoc.

## 2026-10-26: the signature note reaches a batch's refusal

The test with add_declaration, the note's changes with replace_text
and add_declaration (check "end"), the tally in cost-history with
replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: narrowing writes the values given for old constants

The goldens with create_file in one batch, `values` with replace_text
and add_declaration (check "end"), the hint, description, golden and
roadmap with replace_text. By script: the kinds reference's eight
replace_text steps removed and `values` added with Python's json, at
the file's own indent this time. This entry appended with a heredoc.

## 2026-10-26: analysis.Move, with extract_variable and inline_variable

`move.go` and its test with create_file; `walker`, `addressedIn`, the
callers' `move`, `checkMove` and `between` with add_declaration; the
old checks with remove_dead_code; `body.assigns` given its statement
with add_parameter (value `n`, right at all three calls); the rest with
replace_text. By hand: `Move.Check` rewritten twice (the obstacle's
kind, then the rule order) as a whole declaration through
replace_declaration, with a Python script editing the text; and a
replace_declaration that renamed a function was refused ("to rename
it, use rename"), so add_declaration then remove_dead_code. This entry
appended with a heredoc.

## 2026-10-26: a value not of the narrowed type is refused with why

The goldens with replace_text and create_file, `notImplements` with
add_declaration and replace_text (check "end"), the roadmap with
replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: cost-history 19, kinds without reads after #374-#376

The run's results in cost-history with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: refusals say which types lack an interface's methods

The test with add_declaration, the hint with add_declaration and
replace_text (check "end"; the first try reused the name
`missingMethod`, which batchdeclare.go has, and was refused), the
roadmap with replace_text. Nothing by hand. This entry appended with a
heredoc.

## 2026-10-26: cost-history 19, kinds without reads after #378 and #381

The run's results in cost-history with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: changesig onto Move

`checkEvaluation`, `unsafeValue`, `checkArgEffects` and `checkOrder`
with replace_declaration, `funcAround` with add_declaration, the
twelve helpers they made dead with three remove_dead_code calls,
goldens' wording and the walker's `partial` with replace_text. By
hand: `Move.Check` and `Obstacle` rewritten as whole declarations
(the obstacle's `Does` and `PastDoes`), sent through
replace_declaration. This entry appended with a heredoc.

## 2026-10-26: inline_function onto Move

`classify` and `effectBefore` with replace_declaration, `EvaluatedAfter`
with add_declaration, `PastFunc` with add_field, the two other sites
with replace_text, `pureRead` with remove_dead_code. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: slide and fold onto Move

slide's `checkIndependent` and `checkJumps` with replace_declaration,
its `stmt` field with add_field, the four path helpers to
`analysis/paths.go` with move_declaration (`new_name` to export them,
`with_dependencies` brought `isFieldOfValue` along as
`IsFieldOfValue`), the dead walker and fields with remove_dead_code
(eight declarations in two calls); fold's `effectful` with
replace_declaration and `evaluatedFirst`'s loop with replace_text;
`LocalsRead` and `useOf` widened to `ast.Node` with
change_parameter_type; eight slide goldens' wording with replace_text
from a script that read the test's `got:` lines. By hand:
`writtenLocal` rewritten whole through replace_declaration. This entry
appended with a heredoc.

## 2026-10-26: the case for and against step 2

The section in plan-and-execute with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: cost-history 19, kinds without reads with four repairs

The run's results in cost-history and the session notes with
replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: Reach, the path conditions

`Reach` and its test with add_declaration, `evaluatedOnce`,
`firstUseUnconditional` and `notOnEveryCall` with replace_declaration,
the two `conditionalOrRepeated` with remove_dead_code, the condition
cut out of extract_variable's `place` with replace_text (five edits,
one with all_occurrences for the result's removed value), two goldens'
wording with replace_text. Nothing by hand. This entry appended with a
heredoc.

## 2026-10-26: Owner, whose memory it is

`owner.go` and its test with create_file, `Func.freshVars` with
add_field, `private` and `pointsPrivate` with replace_declaration, the
body's `fresh` and `partial` fields and the two constructors with
replace_text, slide's `escapes` with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: loop.py repairs may patch the failed batch

`-patch`, its prompt and helpers with replace_text in batches, then
the fix that a patch is taken only in reply to a failure (a scripted
check had patched a kept batch) the same way, the docs with
replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: cost-history 19, kinds with patch repairs

The run's results in cost-history and plan-and-execute with
replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: site.Site for three refactorings

The derivation per refactoring: extract_struct of the six fields into
`Site` (embedded; the one keyed literal rewritten), rename of each
field to its exported name, then move_declaration to
`internal/site/site.go` for the first (it created the package), and for
the next two, whose target type now existed, replace_declaration of the
local type to an alias `type Site = site.Site`, two replace_text for
the embedded field and the literal, remove_dead_code of the alias.
The helpers with add_declaration on Site, the calls with replace_text,
the old helpers with remove_dead_code; a parameter named `site` that
shadowed the package renamed with rename (by line). Refused on the
way: move_declaration with `new_name` on the extracted type ("rolled
back ... a bug in the precondition checks": the keyed literal keeps the
old field name), so the type was extracted under its final name; and
`string(x.text(` became `(x.Text(` by text, with the parentheses then
dropped by a scripted replace_text. Nothing by hand. This entry
appended with a heredoc.

## 2026-10-26: stopsFuncs into stdFuncs

The field and the entries with replace_text (the entries rewritten from
the old literal by a script: still no tool moves the elements of one
composite literal into another), the table with remove_dead_code, the
test case with replace_text. Nothing by hand. This entry appended with
a heredoc.

## 2026-10-26: loop.py's manual arm

The arm's texts, `run_edits`, `Arm` and the rewiring of `attempt` and
`main` with replace_text in batches, then two wording fixes the
scripted check showed; the docs with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: cost-history 19, the manual arm on kinds

The results in cost-history, plan-and-execute and the session notes
with replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: cost-history 19, both arms on freight-xl

The results in cost-history, plan-and-execute and the session notes
with replace_text. Nothing by hand. This entry appended with a heredoc.

## 2026-10-26: site.Site for the other refactorings

The same derivation as the first three, run per struct by a script
(`port.py`: extract_struct and the renames in one batch, then the
alias, the literals, the helper calls and remove_dead_code of the
helpers in a second). Each `Site:  Site{` literal is aligned by gofmt,
so the script found them by pattern rather than with one fixed text.
remove_dead_code refused while a receiver other than the struct's
usual one still called a helper (`g.` in prepare.go, `first.` in
parameterize); those receivers were added to the replacements.
inline_function's declaring file was extracted to a struct and
renamed to the named field `callee`. The openings moved onto
`site.At` with replace_text, the imports with fix_imports. Nothing by
hand. This entry appended with replace_text.

## 2026-10-26: how this session used the tools

The analysis in the session notes with replace_text. Nothing by hand.
This entry appended with a heredoc.

## 2026-10-26: loop.py's tool index

`-index`, the index and entries split out of `compact`, the asks in
`attempt` and the flag with replace_text in batches; the docs and this
entry with replace_text, anchored on the log's last lines. Nothing by
hand.

## 2026-10-26: cost-history 19, the tool index

The run's results in cost-history and plan-and-execute, and this entry,
with replace_text. Nothing by hand.

## 2026-10-26: fold and inline_function on moves (item 35, step 6)

The new `evaluatedFirst` and `staleRead` with replace_declaration, the
expression, literal and statement cases and `lhsEvaluatedFirst`'s
check with replace_text, `first`, `risky`, `nilAddress`, `effectful`
and `movable` with remove_dead_code, all in one batch; the three
goldens it broke then fixed with replace_text (an address argument
judged on its own, field holes in order, a refusal's wording). New
goldens with create_file, each with its go.mod in a second batch (the
first ran without them: a golden's module needs its own). Nothing by
hand. This entry appended with replace_text.

## 2026-10-26: owned, and the function around fold's code (item 36)

`owned` with extract_struct of body's `f` and `own`; its three methods
by replace_text of their receivers (the embedding promotes them, so
every call kept working), the callback's parameters and uses with
replace_text over both files; `FuncAround` with add_declaration; fold's
three judgments' parameter with replace_text (`all_occurrences` for
`analysis.Outside(info)` and the calls). The new golden with
create_file, from a case written first in a scratch module and run with
both branches' CLIs. Nothing by hand. This entry appended with
replace_text.

## 2026-10-26: loop.py gives the carrying tools in full with the index

`-full-tools`, `FULL`, `CARRIERS` and index's skip with replace_text in
one batch; the docs and this entry with replace_text. Nothing by hand.

## 2026-10-26: results of the index with the carrying tools in full

The results in cost-history 19, plan-and-execute, the session notes
and this entry with replace_text in one batch. Nothing by hand.
## 2026-10-26: the file openings onto site.At (item 37)

Each opening found by a pattern in a script and replaced with
replace_text in one batch (the imports followed). Refused twice on the
way, with nothing changed: the local `at` clashed with an `at` already
in four functions, and the `ws.Fset.File(file.Pos())` replaced with
`all_occurrences` also stood in other functions of two files; renamed
to `opened` and those two replacements dropped. inline_function's and
create_from_usage's openings, of other shapes, with replace_text.
Nothing by hand. This entry appended with replace_text.


## 2026-10-26: seven tools in full beside the index

`CARRIERS` in loop.py, the results in cost-history 19,
plan-and-execute, the session notes and this entry with replace_text
in one batch. Nothing by hand.

## 2026-10-26: move_declaration's files and the declaration edits on sites (item 37)

source's `file` and `src` with extract_struct into a `Site`, renamed to
`File` and `Src`, then the alias recipe (replace_declaration to `type
Site = site.Site`, replace_text of the embedded field and the literals,
remove_dead_code of the alias). rename of `mover.ws` to `WS` was
refused, since the embedded source's Site now has a field `WS`; so the
uses `m.ws`, `m.pkg` and `m.off(` were replaced with replace_text
(`all_occurrences`, one step per character that follows, after a first
batch missed `{m.pkg}` and was rolled back), the fields and their
initializer with replace_text, `mover.off` with remove_dead_code.
`site.Declaring` with add_declaration, its uses with replace_text.
Nothing by hand. This entry appended with replace_text.

## 2026-10-26: a review of the gaps against the roadmap

Docs only: roadmap item 38 with replace_text, from the friction log's
entries and the session notes read for gaps no item held. Nothing by
hand. This entry appended with replace_text.

## 2026-10-27: check in the CLI

The test first with add_declaration; the check tool's report moved
into `refactor.CheckReport`, which the server and the CLI share, with
add_declaration, the server's body with replace_statements (`function`),
its own `tail` and `testTimeout` with remove_dead_code (its go test is
now `refactor.GoTest`, the checkpoint's), `runCheck` with
add_declaration, its dispatch and the usage with replace_text. Nothing
by hand. This entry appended with replace_text.

## 2026-10-27: undo in the CLI

The tests first with add_declaration (the batch declared `LoadUndo` and
`SaveUndo` as stubs), then the two with add_declaration (refused once:
`json` could be `encoding/json` or `encoding/json/v2`, so `imports`
named it), `undoFile`, `keepUndo` and `runUndo` with add_declaration,
their calls, the dispatch and the usage with replace_text; `TestMain`
with add_declaration, keeping the tests' stacks out of the user's
cache and go test on its build cache. Nothing by hand. This entry
appended with replace_text.

## 2026-10-27: version 0.3.0, the first release

The version with replace_text, the roadmap with replace_text. Nothing
by hand. This entry appended with replace_text.

## 2026-10-27: releases made by CI

This session may not push tags or create releases (HTTP 403 for both),
so a workflow does it: `.github/workflows/release.yml` and the notes of
v0.3.0 with create_file, the docs with replace_text. Nothing by hand.
This entry appended with replace_text.

## 2026-10-27: add_case before a default clause

Reported by the user: given the switch's line, add_case put the new
clause after the `default:` that ended the switch. The golden first
with create_file (it failed as reported), then the fix with replace_text
(the comment lines above a clause, now `above`, shared by "before" and
the default) and add_declaration (`trailingDefault`), the description
with replace_text. Then, asked by the user, fallthrough: three goldens
with create_file (each failed first), `fallsThrough` with
add_declaration, the placement and the refusal with replace_text, the
version 0.3.1 and its notes with replace_text and create_file. Nothing
by hand. This entry appended with replace_text.

## 2026-10-27: glisp's friction log into the roadmap

Docs only: roadmap item 39 with replace_text, from glisp's `FRICTION.md`
read against main (each item checked to be still open). Nothing by hand.
This entry appended with replace_text.

## 2026-10-27: TypeScript plan

Docs only: `docs/ts-plan.md`, roadmap item 40 and a session note, from a
claude.ai session without the gorefactor tools (no checkout of its own to
run them from), so all three by hand with plain file edits. No Go
changed. Not evidence for a tool: replace_text and create_file cover
these edits.

## 2026-10-27: CI skips changes to documentation alone

The Test workflow's triggers with replace_text. Nothing by hand. This
entry appended with replace_text.

## 2026-10-27: TypeScript spike

`cmd/ts-spike` (throwaway code, step 0 of `docs/ts-plan.md`) and
`docs/ts-spike.md`, with the plan, README item 40 and a session note
updated, all by hand with plain file edits from a claude.ai session
without the gorefactor tools; the spike was rewritten often while
measuring, which no tool would have shortened. Not evidence for a
tool.

## 2026-10-27: the undo stack into internal/history

TypeScript plan step 2, first part. `Change` and its file operations
moved with rename (three methods exported, as move_declaration's
refusal said) and move_declaration with_dependencies, one batch; the
workspace's wiring with replace_text and replace_statements; the docs
with replace_text. By hand: `history.Stack` (`internal/history/stack.go`,
153 lines, sent with create_file), the bodies of the workspace's undo
methods moved onto a type held in its field. No tool moves a method's
body to the type of a field it uses (Fowler's Move Function); by the
tools it is method_to_function, introduce_parameter, remove_parameter
and function_to_method for each of seven methods, and the callers in
other packages cannot name the unexported field the parameter needs.
A candidate operation: move a method to the type of one of its
receiver's fields, leaving a delegating method behind.
This entry appended with a heredoc.

## 2026-10-27: the engine generic over its workspace

TypeScript plan step 2, second part. The test first
(`internal/refactor/anylang_test.go`, a workspace of text files) with
create_file, in one batch checked at its end with 49 replace_text steps
that made the engine generic: no tool adds a type parameter to a set of
types and functions, or turns a type into an alias of an instance of a
new generic one, so each signature was rewritten as text. The batch's
end check named the one function missed (afterRefusal), added on the
rerun. Two parameters named `any` renamed with rename. The docs with
replace_text. A candidate operation: make a type generic over one of
the types its methods take, leaving the old name as the instance.
This entry appended with a heredoc.

## 2026-10-27: CI on the newest Go only, for now

The matrix with replace_text. Nothing by hand. This entry appended
with replace_text.

## 2026-10-27: the dispatcher, with Go its only language

TypeScript plan step 2, third part. The dispatcher's test and code
(`internal/lang`) with create_file, written before the server changed;
the server's wiring in one batch: replace_statements for each method
now calling through the dispatcher, remove_dead_code for the two
helpers that left, replace_text for the struct and the test that read
its fields. The batch was refused once: remove_dead_code is a
refactoring and needs code that type-checks, so the old fields had to
stay until after it, and go last. By hand: `internal/lang/golang`
(sent with create_file), the server's method bodies moved onto a new
type, the same missing operation as the undo stack's (move a method to
another type, leaving a delegating one).
This entry appended with a heredoc.

## 2026-10-27: the TypeScript workspace

TypeScript plan step 3, first part. The fixture, `tsapi`, the
workspace and its tests with create_file in one batch checked at its
end, written in full before it (new packages: nothing to derive them
from); the docs with replace_text and create_file. Nothing else by
hand. This entry appended with a heredoc.

## 2026-10-27: rename for TypeScript, and its language

TypeScript plan step 3, second part. New packages and tests with
create_file, written before (`tsapi`'s rename and host, the workspace's
rename across projects, the refactoring, the language), and the server's
wiring with replace_text, replace_statements and add_declaration, in
batches checked at their end. One batch declared a stub for symbolNote,
which Go's language called before it had moved there: replace_declaration
put the real body in from the server's copy and remove_dead_code took the
server's out. A move between packages of a function the old package then
no longer uses would have been move_declaration, had the order been
right. `cmd/ts-spike` deleted with delete_file. A test that expected a
TypeScript-only server to refuse a Go tool was wrong (the tool is not
published there) and moved to the mixed test. This entry appended with a
heredoc.

## 2026-10-27: errors already in TypeScript code

The user's choice to relax "every step must type-check" for TypeScript.
The test first with add_declaration; `addedErrors` out of `fixRefusal`
with extract_function, the rest with add_declaration and replace_text,
in one batch checked at its end; the docs with replace_text. Nothing by
hand. This entry appended with a heredoc.

## 2026-10-27: inspect for TypeScript

TypeScript plan step 4, inspect. New packages, fixture and tests with
create_file, written before; the language's Inspect, the server's
description and a server test with replace_statements and replace_text,
in batches checked at their end. One batch refused: replace_statements
could not tell which `inspect` package the new body meant (Go's or
TypeScript's) and asked for `imports`, which settled it. A fixture file
replaced with replace_text, create_file refusing to overwrite. Trying it
on Hono found a panic (a computed member name has no text), fixed with
a test case. Nothing by hand. This entry appended with a heredoc.

## 2026-10-27: TypeScript's first edits

create_file, add_declaration and replace_text for TypeScript. The new
packages and their tests with create_file, written before; registering
them, and the server test, with replace_text in a batch checked at its
end, refused once until `imports` named the TypeScript packages over
Go's of the same names; a test of test-first in a batch with
add_declaration; the plan with replace_text. Nothing by hand. This
entry appended with a heredoc.

## 2026-10-27: TypeScript's statement and expression edits

replace_statements, insert_statements and replace_expression for
TypeScript. The new files with create_file and the outline's brace
offsets, the registration and the server test with replace_text, in one
batch checked at its end. One fix to a file in the scratchpad before it
was created, with sed and then Python (the sed mangled the line). A smoke
test on Hono written with a heredoc and deleted, not committed. This
entry appended with a heredoc.

## 2026-10-27: Roadmap item 39, more from glisp

What a second round of glisp work (its PRs #35-#49) added to item 39,
in README.md with replace_text. By hand: this entry, appended with a
heredoc, and the merge conflict with #426's entry beside it, resolved
with Python (both entries kept, #426's first).

## 2026-10-27: TypeScript imports

The engine's importer generalized (Go's regex into Go's fix_imports, the
retry recognizing TypeScript's refusal, a second pass) and TypeScript's
fix_imports added, all with replace_text and create_file in batches.
Two batches refused and fixed by naming the import: `fiximports` is now
a package name in both languages. One batch failed on a full disk (the
Go build cache, 27 GB); `go clean -cache`. This entry appended with a
heredoc.

## 2026-10-27: a reload that compiles none of the module

The new loader, its test and the changes to Reload, Metadata.Load and
typeCheck with create_file and replace_text in batches; the timings
with throwaway tests written with heredocs and deleted. An `undo` meant
for a timing step that never ran (no /usr/bin/time) undid the last
change on the CLI's stack instead, a committed doc edit, which `git
checkout` restored: check that the step ran before undoing it. This
entry appended with a heredoc.

## 2026-10-27: replace_declaration takes declarations to add after it

Through the CLI built from the branch, one batch: `move_file` turned the
`refuse_two` golden into `adds_after` (a directory, moved whole),
`delete_file` its `refused.txt`, `create_file` the new expectations and
two refusal goldens, and `replace_text` the code, description and schema.
The goldens failed on the old code (checked by stashing replacedecl.go).
One batch refused before applying: `move_file` takes `file`, not `from`.
This entry appended with `replace_text` anchored on the last entry's end,
not a heredoc. The merge conflict with the loader's entry, appended at
the same place, resolved by hand with Edit, both kept.

## 2026-10-27: add_field for TypeScript, and tool aliases

add_field for TypeScript and the add_property alias, with create_file
and replace_text in two batches. Two fixes to a file in the scratchpad
before it was created, with Python. Nothing by hand in the repo. This
entry appended with a heredoc.

## 2026-10-27: TypeScript stubs

The engine's declarer generalized (Go's regexes and declaredNames moved
into Go's create_from_usage as Missing and Declared; the test declarers
given Go's reading) and TypeScript's create_from_usage added, with
replace_text and create_file in batches. The move was done as text, not
with move_declaration: the engine called what was to move, so moving it
first would have made the engine import Go's create_from_usage, a
cycle. A test run failed on a full disk (the build cache again); `go
clean -cache`. This entry appended with a heredoc.
