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
| TS | `inspect` with `pattern` is advertised (since #560) but refused as invalid arguments: `internal/ts/inspect` has no `Pattern` | new, here |
| TS | In a TypeScript-only project `check` and `batch` publish Go's text (`go test`, ./...) though TS runs node tests | new, here |
| TS | extract_function by `case`: no copy of the code after the switch for a value with no case | 41.8 |
| Go, TS | remove_else `wrap` does not re-indent in an unformatted file | 43 |
| Go | introduce_parameter on `(*found)[0]`; extract_function passes a read-only variable by pointer; add_parameter misses a local function literal | 44 |
| Go | inspect `names` refuses a type as declared twice when a test file declares a function of the same name | 49.9 |
| Go | merge_duplicate_function does not see `Exists` and `lang.Exists` as one | 50.6 |
| Go | reflection through interface variables not followed, so rename can miss a JSON field | 42c |
| Go | move_file into another module; move_declaration `new_name` on an embedded type | 39g, 38e |
| Java | replace_text matches `old` inside a line's indentation and writes at the wrong indent, silently | Java friction log 2026-11-05, no row |
| Java | replace_text refuses an `old` that differs only in indentation, without naming the nearest lines (Go does) | Java #106 |
| Java | a stub is not replaced by a declaration with the same erasure | Java #107 |
| Java | a batch checked at the end imports a library class for a name a later step declares | Java #108 |
| Java | friction-log follow-ups still call #105 open | Java friction log |

## 2. One contract: names and server-level behavior

The same operation has different names in Java, so recipes and
`examples/CLAUDE.md` do not transfer. Each server should accept the
others' names as aliases (`refactor.ToolName` here, `Tools` there), and
`docs/tool-contract.md` should record one name per operation.

| Go and TS | Java |
|---|---|
| extract_function | extract_method |
| move_declaration | move_type |
| move_method | move_member |
| move_statement_up / _down | move_statement |
| extract_interface | extract_supertype (interface) |
| convert_params_to_object | extract_struct |
| introduce_parameter | extract_variable `as=parameter` |
| method_to_function | move_member `to` its own class |
| function_to_method | function_to_method |
| remove_dead_code | replace_declaration with empty content (safe delete) |
| delete_file | move_file (delete) |
| change_field_type, change_parameter_type | change_type |

Server-level gaps:
- Java: `inspect` with no arguments lists the project (Java #111); the
  session log has no token counts (Java #18).
- TS: `-no-code-by-text` is Go-only (49.11); Java has no such switch.
- Go and TS: no `preserve_exceptions` (Java's policy on exceptional
  behavior). Go's counterpart is panics, which this repo already rules on
  (session notes); TS's is throws. A decision, not a bug.

## 3. Tools one language lacks

Before any new Go or TS tool, `TestToolBudget` (50) has to be raised or
two tools merged (25); Java's limit is also 50. The session notes' order
applies: a composition, then an option of an existing tool, then a tool.

**TypeScript lacks** (Go has): `split` kind phase (52.2) and the
counterpart of `inline_struct` (52.3).

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
  assertion extract_interface already writes.
- `encapsulate_field`: a fold in Go and TS (derivations 14-16); a
  composite tool only if runs show the composition is missed.
- `convert_lambda`: TS arrow, function expression and method reference;
  Go has no counterpart worth a tool.
- `convert_to_record`: Java-only.

## 4. Options and behavior on shared tools

**TS behind Go:** `imports` on the edits; `line`/`column` addressing on
the signature tools, change_type and move_method; function_to_method's
`receiver`; remove_dead_code's `value`; move_declaration's `new_name`;
invert_if un-nesting at the end of a case or before the final return;
inline_function keeping only the branch a constant condition takes.

**Go behind TS:** inline_function of generic, variadic and other-package
callees (4); rename of interface methods, embedded types, imports and
labels (6); introduce_parameter of every occurrence at once (1).

**Java behind Go and TS:** move_member into another file's class (#109);
fold of instance methods (#103); encapsulate_field's compound writes and
`++`; stubs for a static field read (#107); refusals that name the line
(#110); the remaining `inspect` and argument options the schemas list
(Java has fewer addressing options on most tools).

**Different by design, not to change:** TS's change_type family are edits,
Go's convert at the uses (41.2); add_result returns a tuple in Go and an
object in TS (43); TS's move_method moves down to a subclass, Go's to an
embedding struct (43.10); Java's convert_to_record needs
`value_semantics=true`.

## 5. Proposed order

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

Decisions for the user: whether Java's names or Go's are the contract;
raising the tool budget for steps 3 to 5; whether Go and TS get
`preserve_exceptions`.
