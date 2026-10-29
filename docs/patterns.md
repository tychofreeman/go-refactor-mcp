# Patterns sessions used

The refactorings and working patterns that this project's own sessions
worked out, learned or relied on while building it. Each entry says
what the outcome was meant to be, the steps taken (tools in order), and
where it was used. `docs/derivations.md` has the user's derivations run
in full on small problems. Entries here that come from it say so and
are kept short. The rest come from building the tools with the tools
(the friction log has each PR's account).

Written 2026-10-29, at the user's request, from that day's work and the
friction log. Add an entry when a session finds a sequence it would use
again.

## Changing code

### A function per kind of call, then inline the general one

- **Outcome:** a function called many times with one of a few values
  as an argument (`b.set(&b.summary.Panics, w)`, 34 calls) becomes one
  function per value (`b.panics(w)`), and the general one goes.
- **Steps:**
  1. `extract_function` of one call per value. It is already a method
     when the call uses the receiver.
  2. `fold` of each new function, which rewrites every other call with
     that value.
  3. `inline_function` of the general one with `all_calls`.
- **Where:** derivations section 7, the user's. The session had logged
  this as "no tool does it" until the user named the three steps. That
  led to the rule that a gap names the compositions tried (CLAUDE.md).

### Inline a function that returns early

- **Outcome:** a call of a function whose body returns early is
  replaced by the body.
- **Steps:**
  1. Give each if that returns the rest as its else (`remove_else`
     with `wrap`).
  2. Give the function one result variable that each return assigns.
     In a loop or switch, start the variable at the value returned
     after it and break; failing that, set a flag that guards the rest.
  3. `inline_function`, which does steps 1 and 2 itself.

  Or: extract what encloses the call into a method of its own, so that
  nothing runs after the call, then inline.
- **Where:** derivations section 9, the user's. It was built into
  inline_function in #472, #473 and #475, and into TypeScript in #474.

### Pass in what a getter makes on first use

- **Outcome:** a function that calls a caching getter
  (`if r.f == nil { r.f = ... }; return r.f`) takes the value as a
  parameter instead.
- **Steps:** `introduce_parameter` of the getter's call, which the
  engine judges a read (`analysis.cacheOf`). Without that, the
  non-strict way: pass the method value, so the getter still runs
  where it ran.
- **Where:** derivations section 8 (#471).

### A kind into types, one at a time

- **Outcome:** a field holding one of several constants, switched on in
  several places, becomes an interface with one type per kind.
- **Steps:**
  1. `change_field_type` to widen the field to `any`.
  2. `prepare_switch_for_types` to add the interface and a guard that
     calls it.
  3. Per kind: `extract_function` of the case (`function` + `case`)
     into the new type's method; send the constant through the type
     where it flows into the field; delete the dead case with
     `replace_statements`.
  4. `change_field_type` to narrow the field to the interface.
- **Where:** derivations section 6, the user's; lending-kinds end to
  end through the tools (#285).

### Flags that are a mode become types

- **Outcome:** a struct with two booleans that cannot both be true
  (`widen`, `narrow` in change_field_type's `fieldChange`) becomes an
  interface with one implementation per mode, each holding its own
  checks.
- **Steps:** name the concept first ("this holds one of three modes,
  switched on in two places"), then apply the kind-into-types steps
  above to the mode.
- **Where:** #292. The session had added the second flag itself (#283),
  and only saw the type code when the user asked. That is the origin of
  the session note "reason from the highest abstraction first".

### One analysis, each caller picks its threshold

- **Outcome:** three refactorings (rename, extract_struct,
  remove_dead_code) ask "what does reflection see of this value?" but
  care about different answers. A refusal-or-not boolean
  (`PassedAsInterface`) becomes an ordered answer (`analysis.Sight`:
  `Blind` < `Prints` < `PrintsNames` < `Compares` < `SeesNames`), and
  each caller decides where to refuse and where to note.
- **Steps:**
  1. Add the richer function beside the old one (`ReflectedArgs`),
     with the old one written in terms of the shared walk, so its
     callers do not change.
  2. Move one caller to it, with goldens for the new outcomes (#476).
  3. When a second caller needs a level the first did not, split that
     level (`Prints` from `Blind`, `Compares` from `SeesNames`). Rename
     what changed meaning (`namedVerbs` to `formatVerbs`, `verbFlags`
     to `verb`) before replacing its body.
  4. Move the other callers (#477).
- **Where:** #476 and #477.

### Merge one table literal into another

- **Outcome:** the elements of one composite literal (a table of rules)
  move into another, keyed literals merged field by field.
- **Steps:** `add_element` with `from` (and `name` for a keyed element).
  Then `remove_dead_code` for the emptied table.
- **Where:** #467.

### Expand, migrate, contract

- **Outcome:** an API changes shape without a step that breaks its
  callers.
- **Steps:**
  1. Add the new form beside the old (`add_declaration`, or
     `add_parameter` with a value).
  2. Move each caller to it (`fold`, `inline_function` of the old form
     into a wrapper, or `rename`).
  3. Delete the old form (`remove_dead_code`).
- **Where:** the user's move (derivations, intro); a type's change
  through an alias (`change_parameter_type`, then `change_type`) is
  the same shape.

## Working with the tools

### Test first, through stubs

- **Outcome:** a failing test before the code it tests, while every
  step still type-checks.
- **Steps:**
  1. `add_declaration` (or `create_file`) of the test. The functions,
     fields and types it uses that nothing declares get stubs that
     panic.
  2. `add_declaration` of the real one replaces its stub.

  In a batch, put the test before the code, with `"check": "end"`.
- **Where:** every PR since #117.

### Replace a declaration under a new name

- **Outcome:** a declaration's body and name both change
  (`verbFlags` to a struct `verb`, `namedVerbs` to `formatVerbs`).
- **Steps:**
  1. `rename` any local that would capture the new name (a local `verb`
     in the function), since rename refuses a capture.
  2. `rename` the declaration, which updates every use and its doc
     comment.
  3. `replace_declaration` under the new name.

  replace_declaration refuses content that declares another name.
  Renames come first in a batch, because a refactoring runs only on
  code that type-checks.
- **Where:** #477 (three tries, each refusal naming the next step).

### Move a golden case to another name

- **Outcome:** a `refuse_*` golden that now applies becomes an applied
  case under a new name.
- **Steps:**
  1. `create_file` of its args and inputs under the new name, from the
     old files (a script writes the steps).
  2. `delete_file` of the old files. delete_file removes a directory
     left empty.
  3. `go test -update` writes `out/`. Read the diff before committing.

  `move_file` renames a directory in one step when nothing else
  changes.
- **Where:** #476, #477; move_file in the add_field and dead-field PRs.

### A batch built by a script, checked at its end

- **Outcome:** many dependent steps in one call, with line numbers as
  they were when read.
- **Steps:**
  1. Write the new code into scratch files.
  2. A short script loads them into `create_file`, `add_declaration`,
     `replace_declaration` and `replace_text` steps. The texts come from
     files, so quoting cannot break them.
  3. Send the batch with `"check": "end"` and `"report": "summary"`.

  A refusal rolls back the whole batch and names the step: fix that
  step and resend. Give each batch file a name of its own: a stale
  `b9.json` from an earlier session once ran in place of the new one
  (undone with `refactor undo`).
- **Where:** most PRs since Oct 22.

### A description within its size budget

- **Outcome:** a tool's "Refuses" line lists what the code refuses,
  within `TestLoadedToolsStaySmall`'s 1,700 characters.
- **Steps:** add the refusals. Then trim what the schema already says
  (options it describes) and wording that repeats the example. Rerun
  the test.
- **Where:** #478, after an audit found 11 tools behind their code.

### A survey in parallel with the work

- **Outcome:** a broad read-only question (do the descriptions match?
  what does TypeScript lack?) answered while a PR goes on.
- **Steps:**
  1. Start a read-only subagent with the goal, where to look, what is
     already known, and what to leave alone.
  2. Keep working on the PR.
  3. Check each finding against the code before acting on it.
- **Where:** #478 and #479. The findings checked held up.

### Stack a PR on one not yet merged

- **Outcome:** a second item that edits the same lines as an open PR
  goes out without conflicts.
- **Steps:** branch from the open PR's branch. Open the new PR against
  main, saying it builds on the other. The user merges them in order.
- **Where:** #475 on #474, #478 on #477.
