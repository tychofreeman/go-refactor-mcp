# Parity review: gorefactor (Go, TypeScript) and java-refactor-mcp

2026-11-13. Repos: go-refactor-mcp main at a3e1061 (PR #632),
java-refactor-mcp main at 63c9cde (PR #365). A point-in-time review, as
`docs/code-review-2026-10-20.md` is; `docs/parity.md` stays the plan, and
its sections 3 and 6 are behind what is here.

How it was checked: Go's and TypeScript's tool lists come from the
`refactor` CLI built at that commit. Java's come from `Tools.ALL` in its
source and its `docs/tool-contract.md`; the Java server was not built or
run. Language coverage is from a review of Java's goldens made the same
day (the count of tree-sitter syntax kinds a tool writes), which is not
in either repo yet.

## Summary

- **Tool counts:** Go and TypeScript publish 42 each (38 plus inspect,
  check, batch, undo). Java publishes 40. Both budgets are 50, so the
  budget no longer blocks anything.
- **The earlier parity plan is finished.** Every bug in `parity.md`
  section 1 is marked done, and the shared names (`move`, `inline`,
  `move_statement`, `change_type`, `extract_function`, `extract_type`,
  `extract_supertype`) are in all three.
- **Java has moved ahead since.** Three Java tools are not in `parity.md`
  at all: `loop_to_stream`, `subclass_to_strategy`, `change_signature`.
- **The largest gap is shared, not between the servers.** No server builds
  statements or expressions from names, introduces a type parameter, or
  converts between conditional forms. Java has measured this (a tool can
  write 70 of 139 syntax kinds, 50%); Go and TypeScript have not been
  measured.

## 1. Java has it, Go and TypeScript do not

| Tool or option | TypeScript | Go | State |
|---|---|---|---|
| `pull_up`, `push_down` | Fits classes | Embedding, at most | Was waiting on the budget review; the budget now has room |
| `add_supertype` | `implements` / `extends` | The assertion `extract_supertype` writes | Not started |
| `convert_lambda` | Arrow, function expression, method reference | No counterpart | Not started |
| `loop_to_stream` | A loop to `map` / `filter` / `reduce` / `some` | No counterpart worth a tool | **Not in parity.md** |
| `subclass_to_strategy` | Fits classes | Possibly an interface field; undecided | **Not in parity.md** |
| `change_signature` | Fits | Fits; the roadmap names it twice as "later" (items 1 and 28) | **Not in parity.md** |
| `add_declaration` `implement`, `constructor`; `replace_statements` `surround` | Fits | `surround` only | Waiting for evidence: unused in Java's A/B |
| `encapsulate_field` | A fold | A fold | By decision, a composition |
| `convert_to_record` | n/a | n/a | Java only |

## 2. Go and TypeScript have it, Java does not

Issues in java-refactor-mcp, filed with this review: #367 (`check`),
#368 (per-call `inline`), #369 (`inspect`), #370 (`split`), #371 (the
options), and #372 for the names in section 3.

| Tool or option | State in Java |
|---|---|
| `check` (type-check, `run_tests`) and a `check` step inside a batch | Missing. A batch checks each step or the end; nothing runs tests, and there are no checkpoints |
| `inline` of one call (`line`) | Missing: a method inlines every call |
| `inspect` `function`, `targets`, `uses`; each body's complexity, nesting and side effects | Missing ("not yet" in its contract) |
| `split` | Deferred: no friction entry |
| `replace_text` `files`, `function`; `replace_declaration` `line` / `column` | Missing |
| `remove_dead_code` | Done another way: `replace_declaration` with empty content is a safe delete |
| `create_from_usage` | By decision: Java declares stubs itself |

## 3. Names that still differ

The rule is one name per operation, the generic one, others as aliases.
These are left:

| Operation | Go and TypeScript publish | Java |
|---|---|---|
| An expression made a parameter | `introduce_parameter` | `extract_variable` `as=parameter` |
| A parameter object | `convert_params_to_object` | `extract_type` `members` of a method |
| A constant made a parameter | `parameterize` | `extract_variable` `as=parameter` |
| A field added and filled everywhere | `add_field` | `add_parameter` on a record |
| Two identical functions made one | `merge_duplicate_function` | `fold`, then `inline` |
| Delete what nothing uses | `remove_dead_code` | `replace_declaration`, empty |
| A type switch to methods | `prepare_switch_for_types` + `type_switch_to_method` | `type_switch_to_method` alone |

`parity.md` section 2 planned to make `introduce_parameter` and
`convert_params_to_object` options of `extract_variable` and
`extract_type` here; both are still published. Java's aliases
(`Tools.ALIASES`) are `extract_method`, `method_to_function`, the old
move and inline names, `extract_struct` and `extract_interface`, not the
names in this table, so a recipe written for one server does not run on
the other for these seven operations.

## 4. Language coverage: the gaps all three share

From the Java coverage review's list of abstractions that are missing,
with where Go and TypeScript stand:

| Gap | Java | Go and TypeScript |
|---|---|---|
| Statement and expression generators (a loop, call or assignment from names) | Missing; design sketches started 2026-11-13 | Missing; roadmap 13 has `create_loop` and `replace_parameter`, unbuilt |
| Generics (introduce a type parameter) | Missing; `extract_supertype` refuses generic classes | Missing; Go's `inline` also refuses generic callees |
| Conditionals (if and switch, if and ternary, merge or split conditions) | Missing | Missing; roadmap 51.5 has Consolidate Conditional open |
| Enums | Only `add_element` | Go has none (typed constants); TypeScript enums and unions untested |
| Annotations | Missing | Counterparts are Go struct tags and TypeScript decorators; neither has a tool |
| Exceptions (try-with-resources, wrap or translate) | `surround` only | Go's counterpart is `defer` and error wrapping; no tool |
| Class boilerplate (`equals`, builders, delegates) | Missing | TypeScript partly applies; Go mostly not |

Two points follow:

- **Generators, generics and enums are being designed for Java now.** If
  they are designed as Java tools, Go and TypeScript will need ports with
  the same names later. Deciding the names and arguments for all three in
  the sketches costs little and keeps recipes shared. `create_loop`
  (`over`, `op`, `field`) already exists as a design here.
- **There is no coverage number for Go or TypeScript.** The Java method
  (tree-sitter kinds counted in each golden's `in` and `out` trees, the
  call's own arguments subtracted) would run on this repo's goldens
  unchanged. Without it, parity of language coverage cannot be judged.

## 5. Documents that are out of date

- `docs/parity.md`: section 3 omits `loop_to_stream`,
  `subclass_to_strategy` and `change_signature`; section 6 counts Go 49,
  TypeScript 46, Java 37.
- `docs/roadmap.md`, "Where it stands (2026-11-06)": says item 54 waits
  on the budget review.
- java-refactor-mcp's `docs/design.md`, "Next, in order (2026-11-06)":
  every item on it is done.
- java-refactor-mcp's `docs/tool-contract.md`: `inspect` lists the
  no-argument overview as "not yet" after describing it as built; the
  "Planned" list is all built.

## 6. Suggested order

1. Bring `parity.md` up to date (sections 3 and 6), since both repos plan
   from it.
2. Java: `check` with `run_tests` and batch checkpoints, then per-call
   `inline`. These change how every session works, not one refactoring.
3. Decide the seven names in section 3: merge here, or add aliases in
   Java.
4. TypeScript: `pull_up` / `push_down`, `add_supertype`, `convert_lambda`,
   `loop_to_stream`; `change_signature` for Go and TypeScript.
5. Generators, generics, enums: one design for three languages, Java
   built first.
6. Measure Go's and TypeScript's syntax coverage the way Java's was.
7. Java's `inspect` `uses` and the body summaries; the small option gaps
   as friction logs raise them.
