# The tools against Fowler's catalog and IntelliJ

How gorefactor's Go tools (41 at #541, plus `batch`, `inspect`,
`check` and `undo`) compare with the 61 refactorings of Fowler's
*Refactoring* (2nd edition) and with IntelliJ IDEA's and GoLand's
refactorings (2026-11-04, the user asked). For each entry: the tool
that does it, the composition that does it (with its section in
`docs/derivations.md` when one has been run), or what is missing.
"Run" marks a composition tried with the CLI; others are by reading
the tools and unverified. What is missing and worth doing is roadmap
item 51.

Fowler's catalog is from the book as remembered, not checked against
it; IntelliJ's and GoLand's lists are from JetBrains' help pages
(refactoring-source-code.html for each IDE).

## Fowler's catalog

### A first set

| Refactoring | gorefactor |
|---|---|
| Extract Function | `extract_function` |
| Inline Function | `inline_function` |
| Extract Variable | `extract_variable` |
| Inline Variable | `inline_variable` |
| Change Function Declaration | `rename`, `add_parameter`, `remove_parameter`, `move_parameter`, `add_result`, `change_parameter_type` |
| Encapsulate Variable | composition, run: `add_declaration` of a getter and a setter, `fold` each (derivations 14). Compound assignments (`x++`) are left: gap P |
| Rename Variable | `rename` |
| Introduce Parameter Object | composition, run: derivations 5 (a wrapper taking the struct, `fold`, `inline_function`) |
| Combine Functions into Class | `function_to_method` per function, `move_method` |
| Combine Functions into Transform | missing |
| Split Phase | missing; `split` (roadmap 51.1) |

### Encapsulation

| Refactoring | gorefactor |
|---|---|
| Encapsulate Record | composition, run: per field, a getter method, a pointer setter and a literal setter, each folded (derivations 15); gap P as above |
| Encapsulate Collection | missing (a getter that returns a copy changes what callers see) |
| Replace Primitive with Object | `change_field_type`, `change_parameter_type`, `change_type` |
| Replace Temp with Query | composition: `extract_function` of the temp's computation, `inline_variable` |
| Extract Class | `extract_struct` |
| Inline Class | `inline_struct` |
| Hide Delegate | composition by fold's one-return shape: a method `func (a A) c() C { return a.b.c }`, folded |
| Remove Middle Man | `inline_function` of each delegating method |
| Substitute Algorithm | an edit: `replace_statements`, checked by the tests |

### Moving features

| Refactoring | gorefactor |
|---|---|
| Move Function | `move_declaration`, `move_method`, `method_to_function`, `function_to_method` |
| Move Field | `extract_struct` with `into`; within a struct, derivations 13 |
| Move Statements into Function | composition: `extract_function` of the call and the statements at one caller, `fold` the others, `inline_function` the old function |
| Move Statements to Callers | composition: `inline_function`, then `extract_function` of what stays shared |
| Replace Inline Code with Function Call | `fold` |
| Slide Statements | `move_statement_up`, `move_statement_down` |
| Split Loop | missing; `split` (roadmap 51.1) |
| Replace Loop with Pipeline | missing (roadmap 20, loops) |
| Remove Dead Code | `remove_dead_code` |

### Organizing data

| Refactoring | gorefactor |
|---|---|
| Split Variable | missing; `split` (roadmap 51.1) |
| Rename Field | `rename` |
| Replace Derived Variable with Query | not a refactoring: reads of the field equal the query only if every write, literal and decode keeps them equal, an invariant of the program, not an equality of code. An edit after an assertion the tests check (`insert_statements`, then `replace_expression`) |
| Change Reference to Value | missing |
| Change Value to Reference | missing |

### Simplifying conditional logic

| Refactoring | gorefactor |
|---|---|
| Decompose Conditional | composition: `extract_variable` of the condition, `extract_function` of that declaration, `inline_variable`; `extract_function` of each branch |
| Consolidate Conditional Expression | missing; roadmap 51.5 |
| Replace Nested Conditional with Guard Clauses | `invert_if`, `remove_else` |
| Replace Conditional with Polymorphism | `prepare_switch_for_types`, `type_switch_to_method` |
| Introduce Special Case | missing |
| Introduce Assertion | an edit (`insert_statements`) |

### Refactoring APIs

| Refactoring | gorefactor |
|---|---|
| Separate Query from Modifier | missing; roadmap 51.4 |
| Parameterize Function | `parameterize` |
| Remove Flag Argument | composition, run: a wrapper per value, `fold`, `inline_function` (derivations 17) |
| Preserve Whole Object | composition: a wrapper taking the whole value, `fold` (stage 2 of fold); unverified whether it passes `r` or rebuilds `Range{Low: r.Low, ...}` |
| Replace Parameter with Query | `remove_parameter` with `value` |
| Replace Query with Parameter | `introduce_parameter`; `add_parameter` with `replace_variable` |
| Remove Setting Method | composition: `inline_function` of the setter's calls, `remove_dead_code` |
| Replace Constructor with Factory Function | Go has none (constructors are functions already) |
| Replace Function with Command | composition, run: a struct of the parameters and a function taking it, `fold`, `inline_function` of the old function, `function_to_method` (derivations 16). A local cannot then become a field, which is what lets a command's body split into methods: gap Q |
| Replace Command with Function | composition: `method_to_function`, `inline_function` at the calls that build the struct |

### Dealing with inheritance

Go has embedding, not inheritance; the entries map as far as embedding
allows.

| Refactoring | gorefactor |
|---|---|
| Pull Up Method | missing (`move_method` moves down only) |
| Pull Up Field | `extract_struct` with `into` an embedded struct |
| Pull Up Constructor Body | not applicable |
| Push Down Method | `move_method` to a struct that embeds the receiver |
| Push Down Field | missing |
| Replace Type Code with Subclasses | `change_field_type` to `any`, `prepare_switch_for_types`, `type_switch_to_method` (derivations 6) |
| Remove Subclass | missing |
| Extract Superclass | `extract_struct` (its new type is embedded) |
| Collapse Hierarchy | `inline_struct` of an embedded struct |
| Replace Subclass with Delegate | `rename` of an embedded field to a named one |
| Replace Superclass with Delegate | the same |

## IntelliJ IDEA and GoLand

GoLand's Go refactorings are Rename, Change Signature, Extract
(Function, Variable, Constant, Interface), Inline, Move, Copy and Safe
Delete; IntelliJ adds, for Java, the entries below Safe Delete.

| IntelliJ / GoLand | gorefactor |
|---|---|
| Rename | `rename` |
| Change Signature | five tools (see Change Function Declaration); roadmap 25 and 28 weigh merging them |
| Extract Method / Function | `extract_function` |
| Extract Variable | `extract_variable` |
| Extract Constant | local `const` only, through `extract_variable`; not to package level |
| Extract Interface | `extract_interface`, in both languages |
| Inline | `inline_variable`, `inline_function`; no inline of a constant or type |
| Move | `move_declaration`, `move_method`, `move_file` |
| Copy | `move_file` with `copy`; not a declaration |
| Safe Delete | `remove_dead_code` |
| Type Migration | `change_parameter_type`, `change_field_type`, `change_type` |
| Convert to Instance Method / Make Static | `function_to_method` / `method_to_function` |
| Find and Replace Code Duplicates | `fold`, `merge_duplicate_function` |
| Extract Parameter | `introduce_parameter` |
| Extract Parameter Object | derivations 5 |
| Extract Delegate, Extract Superclass | `extract_struct` |
| Replace Temp with Query | composition (Fowler's, above) |
| Remove Middleman | `inline_function` |
| Use Interface Where Possible | missing; close to roadmap 9's narrowing a parameter |
| Invert Boolean | missing |
| Encapsulate Fields | derivations 15 |
| Extract Field, Introduce Type Parameter, Wrap Method Return Value, Replace Constructor with Builder or Factory, Generify, Pull Members Up, Push Members Down, Replace Inheritance with Delegation, Convert Anonymous to Inner | missing or not applicable to Go |

## What neither has

`fold` (any code that does what a function does, with typed holes),
`parameterize`, `prepare_switch_for_types` and `type_switch_to_method`,
statement slides that prove the two statements independent (IntelliJ's
Move Statement moves text), `create_from_usage` as a type-checked step
(a quick-fix in IntelliJ), the edits, and `batch` and `undo` over all of
them, each step type-checked and rolled back on error.
