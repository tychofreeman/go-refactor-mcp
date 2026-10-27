# TypeScript

The TypeScript side of the server: `docs/ts-plan.md` is the plan (step 3
is under way), `docs/ts-spike.md` what the spike measured. Claude Code
loads this file when a session reads files here; the root CLAUDE.md
still applies.

- `tsapi` is the only package that imports the compiler
  (`buke/typescript-go-internal`, the Go port of TypeScript 7, pinned
  in go.mod). Everything else uses its types, so that an official Go
  API, when there is one, changes only `tsapi`. Upgrade the mirror in
  its own PR, with the tests as the gate.
- `workspace` is the engine's `refactor.Workspace` for TypeScript: a
  solution, the projects its root tsconfig.json references, each kept
  up to date incrementally. Changes are written to disk and the
  programs updated, as for Go; errors are compared as
  "file:line:col: message", each error once even when two projects
  report it.
- What the spike found and the code must keep: load every project of
  the solution (a file's default project can leave its tests out);
  re-check through the incremental builder (a plain re-check of a
  file's importers costs seconds); a created or deleted file rebuilds
  the projects that take it.
- Fixtures are small solutions under `testdata/` in the garden domain
  of the tool examples (`Plant`, `Bed`, `water`), not the eval tasks'.
  They need no `node_modules`: `"types": []` in the config.
  `tsc` 7 (`npm i typescript@7`) checks that a fixture compiles.
- Run `go test ./internal/ts/...`; the rest of what CI runs is in the
  root CLAUDE.md.
