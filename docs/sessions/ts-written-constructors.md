# Constructors written for the model, in TypeScript

- **Item:** roadmap 54.10, the first of its two PRs: java's #272 and #275 (the user,
  2026-11-07: "Why should the model have to generate that code at all?"), "start the ts changes".
- **Date:** 2026-11-07
- **Branch:** ts-written-constructors

## Plan

Java's rule, in TypeScript's terms. Under `strictPropertyInitialization` (on with `strict`), a
class field with no initializer, no `?` and no `!` must be set in the constructor (TS2564). When
an edit leaves a class with such fields, one of them new in the edit, and no constructor but a
stub, the edit also writes the constructor: a parameter per field, in order, of the field's
type as written, assigning each, placed by kind (after the fields). The result says so. Without
the option nothing is written: a field may stay unset there, and a constructor would break
`new Pot()`.

The edits that add fields are `add_declaration` with `type` and `add_field` on a class; both call
one function, `tsapi.WriteConstructors(before, after)`, through the workspace (which knows the
project's options). #275: a constructor `add_declaration` adds replaces one whose body only
assigns fields (`this.size = size;`), as it replaces a stub: `tsapi.Functions` lists
constructors, and `replaceStubs` takes either body.

Derivation: red tests (add_declaration of two fields writes the constructor; a constructor added
after replaces it; add_field writes one; without strict nothing); then `WriteConstructors` in a
new `tsapi/construct.go`, `Workspace.Construct`, the two Plans calling it, and the two changes
for #275.

## Notes

- read: `addfield.Plan`, `adddecl.Plan` and `replaceStubs`, `tsapi.Functions`, `Workspace.Project`
  and `Abs`, `gocode.Reindent`, and the compiler's `PropertyDeclaration`, `ModifierFlags`,
  `GetStrictOptionValue`, to find where both edits end and what a field without a value is.
- refusal: add_element at line 50 (the slice starts at 40); a retry with the line applied.
- red: the non-strict test failed for TS2564: TypeScript 7 turns `strict` on by default; the test
  says `"strict": false`.
- refusal: `add_parameter` of the source file to `unsetFields` was sent with wrong argument names,
  and would have passed one value to calls that need two; the type's text is trimmed instead.
  A retry's step replaces the step's arguments whole (a partial one was refused).
- red: `Functions` named a constructor by `Name()`, nil for one (a panic); and the replaced
  constructor kept the model's indentation; `gocode.Reindent` re-indents it to the stub's place.
- bulk: `tsapi/construct.go` (109 lines), new behavior.
- hand: none.
## Measured

Not measured: a cloud clone, started outside the checkout, has no session log; the CLI's steps
are in the notes.

## Assessment

Through the tools: every change through the CLI; no hand edits. The large write was new behavior.
The plan held: one function both edits call, through the workspace, and #275 as a stub rule. The
cost was in small misses (a line number, argument names, a nil name), each one resend; the batch
retry carried most of them. What would have helped: a retry step that merges arguments into the
kept step's, as most resends changed one argument (roadmap 54.11 is the same family).

## Follow-ups

- #273: a stub constructor from `new Pot(3)` assigns arguments that each fit one field; with it,
  a class with a stub constructor gets the written one too.