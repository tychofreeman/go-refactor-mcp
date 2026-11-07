# Parity of Go, TypeScript and Java

What it would take for gorefactor's Go and TypeScript tools and
java-refactor-mcp's Java tools to be at parity: the same operations under
the same names, the same options where the languages allow, and no
language's known bugs left behind the others. Surveyed on 2026-11-05 from
both servers' `tools/list`, this repo's roadmap, `docs/checks.md`,
`docs/ts-plan.md` and friction log, and the Java repo's `docs/design.md`
and friction log. Roadmap item 54 tracks it; the Java repo's design.md
points here.

## 1. Bugs to fix first

Cheap, and each one costs a fresh model a refusal or a wrong result today.

| Language | Bug | Where it is recorded |
|---|---|---|
| TS | `inspect` with `pattern` is advertised (since #560) but refused as invalid arguments: `internal/ts/inspect` has no `Pattern` | *Done* (`textutil.Matches`, shared with Go) |
| TS | In a TypeScript-only project `check` and `batch` publish Go's text (`go test`, ./...) though TS runs node tests | *Done* (`typescriptText`, also inspect's "Go file of the module") |
| TS | extract_function by `case`: no copy of the code after the switch for a value with no case | *Done* (`tsapi.MissingCase`) |
| Go, TS | remove_else `wrap` does not re-indent in an unformatted file | *Done* (TS also kept template literals' text) |
| Go | introduce_parameter on `(*found)[0]` | *Done* (`derefOf`) |
| Go | extract_function passes a read-only variable by pointer | *Done* (`copyReads`) |
| Go | add_parameter misses a local function literal | *Done* (`findLocal`; the other signature tools still refuse one) |
| Go | inspect `names` refuses a type as declared twice when a test file declares a function of the same name | 49.9 |
| Go | merge_duplicate_function does not see `Exists` and `lang.Exists` as one | 50.6 |
| Go | reflection through interface variables not followed, so rename can miss a JSON field | *Done* (`analysis.reaches`) |
| Go | move_declaration `new_name` on an embedded type | *Done* (`embeddedField`) |
| Go | move_file into another module | *Done* (`to` routed as a path) |
| Go | inline_struct breaks one-line struct declarations (`type Bed struct{ Soil }`) | *Done* (#573) |
| Java | replace_text matches `old` inside a line's indentation and writes at the wrong indent, silently | *Done* (Java #119, #223: shifted, or refused naming the lines) |
| Java | replace_text refuses an `old` that differs only in indentation, without naming the nearest lines (Go does) | *Done* (Java #106, #221: a uniform indentation shift, `new` shifted with it; the nearest lines named on a miss) |
| Java | a stub is not replaced by a declaration with the same erasure | *Done* (Java #107, #224; also a static field stub for `T.NAME`) |
| Java | a batch checked at the end imports a library class for a name a later step declares | *Done* (Java #108, #225) |
| Java | friction-log follow-ups still call #105 open | *Done* (Java #222) |

## 2. One contract: names and server-level behavior

The same operation has different names in Java, so recipes and
`examples/CLAUDE.md` do not transfer. The user's rule (2026-11-05): **the
more generic name wins**, and the other names stay as aliases
(`refactor.ToolName` here, `Tools` there), so old recipes and the A/B
baselines keep working. `docs/tool-contract.md` records the result.

| Operation | Go and TS today | Java today | Shared name |
|---|---|---|---|
| extract a function | extract_function | extract_method | **extract_function** (a method is a function) |
| move a declaration | move_declaration | move_type | **move** |
| move a method or field | move_method | move_member | **move** |
| move a statement | move_statement_up, _down | move_statement | **move_statement**, with a direction |
| extract an interface or base | extract_supertype (extract_interface until 2026-11-06, still an alias) | extract_supertype | **extract_supertype** |
| a parameter object, extract class | convert_params_to_object, (Go) extract_type | extract_type (`members` of a method, class or record) | **extract_type** (renamed from extract_struct on 2026-11-06, the user: "struct" is Go's word; extract_struct stays an alias); TypeScript's since 2026-11-06 (a class's members into a held class, an interface's or object type's into a held interface), with `as` for the holding property as in Java |
| an expression made a parameter | introduce_parameter | extract_variable `as=parameter` | **extract_variable** `as` |
| change a type | change_type, change_field_type, change_parameter_type | change_type | **change_type** |
| move, copy, delete a file | move_file, delete_file | move_file | **move** (a path); a file renamed in place is also **rename** |
| a type switch to methods | type_switch_to_method (TS: union_switch_to_method) | none yet | **type_switch_to_method** |
| a method to a function and back | method_to_function, function_to_method | move_member to its own class, function_to_method | keep the pair: function_to_method is in all three, and Go's method_to_function is not a move; Java adds method_to_function as an alias |

**One `move`** (2026-11-05, the user: "easier for the model to reach
for"; worth trying). Everything that takes a named thing to another place
and updates its references is one tool, `move`, which works out the case
from what it is given, with no `kind` argument:

- a top-level name (`file` + `name`): a declaration, to `to_file` or a
  `package` (Go's and TypeScript's move_declaration, Java's move_type);
- `Type.member`: a method or field, to `to` (a type) in any file (Go's and
  TypeScript's move_method, Java's move_member and its row 109, which adds
  static fields and constants);
- a path with no name: the file or directory, to `to` (move_file;
  `copy` and delete as options, for delete_file).

**One `inline`** (2026-11-05, the user, the same reasoning). The three
inverses of extract are one tool, `inline`, which works out the case from
what the name resolves to: a function or `Type.Method` inlines its calls
(the one at `line`, or every call with `all_calls`; Go's and TypeScript's
inline_function, Java's); a local variable or constant inlines its uses
(inline_variable); `Holder.field` whose field holds a struct or class
inlines that type into the holder (Go's inline_struct, TypeScript's 52.3,
Java's inline class). Unlike `extract`, nothing is ambiguous: an
extracted expression could become a variable or a function, so extract
stays several tools (or takes `as`) until a run says otherwise. Go goes
from 41 to 39 published tools.

`rename` given a file path and a new name renames the file in place,
the same as `move` within its directory. Not part of `move`: move_statement
(order within a block) and move_parameter (order of a signature), which
reorder rather than relocate. The description stays short by trusting
"move" to be read generically: one sentence of what it moves, one example
per case. The old names stay aliases, and the merged description gets an
A/B check against the baselines before it replaces them (ask first).

Merging under one name also frees budget in Go and TypeScript: six
published tools become options of others (move_statement_down,
convert_params_to_object, introduce_parameter, change_field_type,
change_parameter_type, delete_file), taking Go from 49 to 43, and one
`move` for move_declaration, move_method and move_file takes it to 41. Each merged
schema must stay under `TestLoadedToolsStaySmall`'s 1,700 characters, and
a merge changes what the baselines saw, so it waits for an A/B check of
the merged descriptions (ask before running it).

Server-level gaps:
- Java: `inspect` with no arguments lists the project (Java #111); the
  session log has no token counts (Java #18).
- TS: `-no-code-by-text` is Go-only (49.11); Java has no such switch.
- Go and TS: `preserve_exceptions` *done* (2026-11-06; `docs/checks.md`).
  The user chose (2026-11-05) to
  bring it to both: one flag on every refactoring that makes it refuse a
  change to exceptional behavior (Go: a panic, including the nil
  dereference that may now move past calls that return, per the session
  notes; TS: a throw) instead of making the change and naming it. The
  changes become a separate part of the result, as Java's `Exceptional`
  does.

## 3. Tools one language lacks

Before any new Go or TS tool, `TestToolBudget` (50) has to be raised or
two tools merged (25); Java's limit is also 50. The session notes' order
applies: a composition, then an option of an existing tool, then a tool.

**TypeScript lacks** (Go has): nothing now; `split` kind phase (52.2) and
the counterpart of `inline_struct` (52.3, without Go's `elsewhere` and
`prefer`) are *done*.

**Java lacks** (Go and TS have):
- `add_result`, `parameterize`, `merge_duplicate_function`,
  `prepare_switch_for_types`, `type_switch_to_method` (an `instanceof`
  chain or a switch on a sealed type to a method), `split`, and
  inline class (`inline_struct`).
- `add_field` (Java's add_declaration declares a field but does not fill
  constructors or literals).
- `create_from_usage` is not missing: Java declares stubs automatically,
  by the user's decision.

**Go and TS lack** (Java has):
- `pull_up` / `push_down`: natural for TS classes; for Go, embedding (Java
  #25 says worth a look).
- `extract_supertype` as a superclass: TS only (Go has none).
- `add_supertype`: TS `implements` / `extends`; Go the `var _ I = (*T)(nil)`
  assertion extract_supertype already writes.
- `encapsulate_field`: a fold in Go and TS (derivations 14-16); a
  composite tool only if runs show the composition is missed.
- `convert_lambda`: TS arrow, function expression and method reference;
  Go has no counterpart worth a tool.
- `convert_to_record`: Java-only.
- `batch` `retry` (java #247): after a refused batch, send only the
  changed steps (`{"step": N, ...}` replaces one, `{"after": N, ...}`
  adds one), numbered as in the refused batch; the batch still runs
  whole. Go and TypeScript have checkpoints (a `check` step) and the
  automatic rerun with check "end", not this.
- The constructive options (java #123): `add_declaration` with
  `implement` (`*` stubs every abstract method left, a name adds one stub
  or a `super` override) and `constructor` (fields in, a constructor that
  assigns each); `replace_statements` with `surround` (`try`, `if`,
  `for`, `while`). Java's skeleton A/B (#250) found all three unused on
  its shop tasks, so these wait for evidence.

## 4. Options and behavior on shared tools

**TS behind Go:** `imports` on the edits; `line`/`column` addressing on
the signature tools, change_type and move_method; function_to_method's
`receiver`; remove_dead_code's `value`; move_declaration's `new_name`;
invert_if un-nesting at the end of a case or before the final return;
inline_function keeping only the branch a constant condition takes.

**Go and TS behind Java (2026-11-06, java #243-#250):** `move` takes
`imports` (java #244, after its A/B found a refusal asking for `imports`
that `move` then refused). Checked 2026-11-06, nothing to fix: Go's
and TypeScript's `move` import what moved code uses from its type
information (`imports.Adder` by package path in `movedecl`), not by
name, so no refusal of theirs asks for `imports`; only edits resolve
names (`gocode`), and they take it.
TypeScript's `type_switch_to_method` refuses a `default` or a final
`else`; Java's makes it the supertype's own method (java #249), which
TypeScript could do when the union's classes share a base class. It
already takes switch statements and `instanceof` chains. Go's keeps
`default` in the nil branch by design: an interface has no method body.

**Go behind TS:** inline_function of generic, variadic and other-package
callees (4); rename of interface methods, embedded types, imports and
labels (6); introduce_parameter of every occurrence at once (1).

**Java behind Go and TS:** replace_text's `files` and `function`;
replace_declaration's `line`/`column` and `imports`; move_member into another file's class (#109);
fold of instance methods (#103); encapsulate_field's compound writes and
`++`; stubs for a static field read (#107); refusals that name the line
(#110); the remaining `inspect` and argument options the schemas list
(Java has fewer addressing options on most tools).

**Different by design, not to change:** TS's change_type family are edits,
Go's convert at the uses (41.2); add_result returns a tuple in Go and an
object in TS (43); TS's move_method moves down to a subclass, Go's to an
embedding struct (43.10); Java's convert_to_record needs
`value_semantics=true`. replace_text's indentation: Java takes an `old`
off by a uniform indentation and shifts `new` with it (java #106, #119,
the user's choice over Go's rule, any run of spaces for any other).
**Go and TS behind Java (2026-11-07, java #272-#279):** placement by
kind (#279): done in Go (`docs/sessions/place-by-kind.md`) and
TypeScript (`docs/sessions/ts-place-by-kind.md`), where `type` now names
the class members go in, as Java's does, and a top-level `const`, `let`
or class an existing statement uses goes before it (a temporal dead zone
otherwise).
The constructors written for the model (#272: an edit that leaves fields
unset writes their constructor; #273: a constructor stub assigns the
fields its arguments each fit; #275: an added constructor replaces such a
one) fit TypeScript, where a field with no initializer is TS2564 under
`strictPropertyInitialization`, and `new Pot(3)` stubs an empty
constructor today; Go has no constructors or final fields, and a test
builds a struct with a literal. Roadmap 54.9 and 54.10.

Java-only, nothing to port: an ambiguous import decided by what the
package's other files import (java #245), and `after` by an overload
label (java #248); Go imports name packages. Checked, nothing to do:
inline leaves an argument without parentheses where the parameter
stands alone (java #248) in Go and TypeScript too (`g(n + 1)`, `v := n +
1`; `(n + 1)` only inside an operator).

## 5. Proposed order

Java's rows for these items are #119-#130 in its `docs/design.md`, ordered there under
"Next, in order" (2026-11-06) with its own open items.

1. The bugs in section 1, one PR each per repo (TS inspect `pattern` and
   the TS descriptions first; Java's silent indent match first there).
2. The contract: aliases both ways and `docs/tool-contract.md`; Java #111.
3. TS 52.2 phase and 52.3 inline_struct (the last Go-only tools).
4. Java's missing tools, ordered by its friction log: add_result,
   parameterize, merge_duplicate_function, the type switch pair, split,
   inline class.
5. Go and TS from Java: pull_up / push_down for TS, then whatever the
   budget allows, after the user rules on it.
6. Option gaps in section 4, as the friction logs raise them.
7. From Java's #243-#250 (the user, 2026-11-06): `batch` `retry` for Go
   and TypeScript (`move` with `imports` was checked and needs nothing).

Decided (2026-11-05, the user): the more generic name wins (section 2);
`preserve_exceptions` comes to Go and TypeScript. The tool budget: the
user will review the list below first, and expects to raise it.


**Java's status (2026-11-06, java-refactor-mcp #223-#242):** the section 1 bugs are fixed. The
shared names are in: `extract_function` (alias `extract_method`), `method_to_function` (an alias
of `move_member` in place), one `move` over `move_type`, `move_member` and `move_file`, and one
`inline` over `inline_variable` and `inline_function` (and inline class with `field`), all old
names kept as aliases; `inspect` with no arguments lists the project (#111). Of section 3:
`add_result` is built (a record result); `parameterize` is `extract_variable` `as=parameter` over
the method's parameters; `merge_duplicate_function` is a composition (`fold`, then `inline`);
the type switch pair is one tool, `type_switch_to_method` (a switch expression of type patterns
first); `add_field` is `add_parameter` on a record; `split` is deferred (no friction entry, and
`extract_function` of a tail splits a method). Of section 4: `move_member` into another file's
class and of static fields (#109), and refusals that say where (#110), are done; the rest wait
for friction entries. The A/B check of Java's merged list (`move`, `inline`) passed (#243); since then #244-#250
added `imports` on `move`, the import rules, `batch` `retry`, `implement`, `constructor`,
`surround` and more shapes for `type_switch_to_method` (sections 3 and 4).

## 6. Every tool, for the budget review

Go publishes 49 tools (TypeScript 46, Java 37); the budget is 50 in both
repos. After the merges in section 2, Go publishes 39.

| Kind | Go | TS | Java |
|---|---|---|---|
| Server | inspect, check, batch, undo | the same | inspect, batch, undo |
| Edits | add_case, add_declaration, add_element, add_field, create_file, create_from_usage, insert_statements, replace_declaration, replace_expression, replace_statements, replace_text | the same | the same but add_field and create_from_usage |
| Signature | add_parameter, remove_parameter, move_parameter, introduce_parameter, add_result, change_type, change_field_type, change_parameter_type, convert_params_to_object | the same | add_parameter, remove_parameter, move_parameter, change_type, extract_type |
| Extract, inline | extract_function, extract_variable, inline_function, inline_variable, split, fold, parameterize, merge_duplicate_function | the same but split | extract_method, extract_variable, inline_function, inline_variable, fold |
| Conditionals | invert_if, remove_else, prepare_switch_for_types, type_switch_to_method | the same, union_switch_to_method | invert_if, remove_else |
| Types and moves | rename, move_declaration, move_method, method_to_function, function_to_method, extract_supertype, extract_type, inline_struct, remove_dead_code, move_statement_up, move_statement_down, move_file, delete_file | the same but inline_struct | rename, move_type, move_member, function_to_method, extract_supertype, add_supertype, pull_up, push_down, encapsulate_field, convert_lambda, convert_to_record, move_statement, move_file |
