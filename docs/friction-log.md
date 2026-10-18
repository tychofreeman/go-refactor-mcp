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
