# TypeScript spike: results

Step 0 of `docs/ts-plan.md` (roadmap item 40): can the Go port of the
TypeScript compiler, imported through `buke/typescript-go-internal`
v7.0.2, load a project, take an edit in memory, re-check, and rename,
fast enough for the engine? The code is `cmd/ts-spike` (throwaway; it
goes when step 3 lands). Run on 2026-10-27 in a 2-core cloud session.

**Answer: yes, with three findings that change the plan.** The engine
must load every project of a solution (a file's default project can
leave its tests out), must re-check through the incremental builder (a
plain re-check of importers is seconds), and should record declaration
signatures at load (the first edit otherwise pays for all of them).

## The project

[Hono](https://github.com/honojs/hono) at `bac5c4a` (2026-10-25): 313
files in `src`, 82,078 lines with tests (26,381 without), strict, a
solution `tsconfig.json` referencing `tsconfig.build.json` (sources,
tests excluded) and `tsconfig.spec.json` (sources and tests), among
others. With its libraries and `node_modules` types, the spec program
has 906 files. Dependencies installed with `pnpm install`.

The TypeScript 7.0.2 CLI, for comparison: `tsc -p tsconfig.build.json
--noEmit` 1.5 s, `tsc -p tsconfig.spec.json --noEmit` 4.1 s (4 errors
in tests under TypeScript 7, which every comparison below carries
unchanged).

## Measurements

| Step | Time |
|---|---|
| Parse the tsconfig and build the Program (906 files) | 0.32–0.34 s |
| Check all 314 project files | 3.6–4.2 s |
| `UpdateProgram` after an in-memory edit of one file (program reused) | 1–2 ms |
| Files that import `src/utils/url.ts`, directly or not | 205 of 314, found in 1–5 ms |
| Re-check those 205 plainly, after any edit | 2.5–5.3 s |
| Re-check through the incremental builder, body edit (first edit / later) | 1.9 s / 26–28 ms |
| Same, an edit that changes `getPath`'s signature (41 errors in callers) | 1.6–2.4 s |
| Open a file in a language-server session (loads its projects) | 0.5–0.7 s |
| `ProvideRename` of `getPath` (47 edits, 5 files) | 6–12 ms |
| Re-check that rename incrementally, 2 cores / 1 core | 1.4–2.6 s / 2.6–3.4 s |

Binary: the spike alone is 30.7 MB (compiler, checker, language
service, project system); today's `refactor-mcp` is 19.4 MB.

## What worked as hoped

- **An overlay file system is enough** for edits in memory:
  `wrapvfs.Wrap` replacing `ReadFile`, under `bundled.WrapFS` for the
  library files. Nothing is written to disk to check a plan.
- **`Program.UpdateProgram(path, host, nil)`** replaces one changed
  file and reuses the rest of the program; it reported reuse every
  time. A step changing several files calls it once per file.
- **Diagnostics compare cleanly** by file name, code, message key and
  arguments (positions left out). The signature change gave exactly 41
  new errors; undoing it removed exactly those 41; redoing it brought
  them back.
- **Rename is editor-grade.** Through `project.Session` (the language
  server's own project system, in process) and `ProvideRename`, the
  rename of `getPath` changed the import and the one reference in
  `hono-base.ts` (`options.getPath ?? getPath`) and kept the `getPath`
  option properties; `getPath: getPath` in a test kept its key. After
  it, the diagnostics were the same.

## What changes the plan

1. **Load every project of the solution.** The session picked
   `tsconfig.build.json` as `url.ts`'s project, which leaves tests out;
   rename then changed only `hono-base.ts` and `url.ts`, and three test
   files kept the old name. The before/after check caught it (4 new
   errors), so the engine would have refused, not broken the code.
   With a test file opened too, the spec project loaded, rename found
   47 edits in 5 files, and the check came back clean. The workspace
   opens one file of each project the root's solution references (or
   loads the project tree through `WithSnapshotLoadingProjectTree`), so
   a refactoring sees every file that can use a name.
2. **Re-check through the incremental builder**
   (`execute/incremental`, what `tsc --watch` uses). It keeps each
   file's diagnostics and re-checks a file's importers only when the
   file's declaration output changed. A plain re-check of the importers
   costs the same whatever the edit, 2.5–5.3 s here, because each new
   program starts with empty checkers.
3. **Record signatures at load.** The incremental builder computes a
   file's declaration signature the first time the file changes, so the
   first edit of a session paid 1.9 s where later ones paid 27 ms.
   Computing them at load, in the background, moves that cost out of
   the first step.

## What the numbers mean for steps

- Edits inside bodies (extract and inline variable, invert_if,
  replace_statements, most of extract_function) re-check in tens of
  milliseconds once signatures are recorded.
- Steps that change an exported shape (rename of an export, add or
  remove a parameter, move_declaration) re-check every importer: 1.4 to
  3.4 s here, for a file 205 of 314 files depend on, which is near the
  worst case in this project. The checker pool uses 4 checkers by
  default, so on 4 or more cores this should come down toward the
  1 s target; measure on the user's machine before tuning
  (`SingleThreaded`, `CheckerPoolOptions`).
- A batch should re-check once at its end or at a checkpoint, not
  after each step, where its steps allow it (as `"check": "end"`
  already does for Go).

## Open for step 3

- Two holders of state: the compiler `Program` (for checks) and the
  language server's `Session` (for rename, references, file moves,
  organize imports), each with its own overlays. Whether the session's
  own snapshots can serve both, with its programs re-checked
  incrementally, decides whether the workspace keeps one or two.
- The language service speaks LSP: positions as line and column in
  UTF-8 (set by `PositionEncoding`), edits as `WorkspaceEdit`. The
  engine's byte-range edits convert at that boundary only.
- `project.Session` wants a `Client` for watching and publishing
  diagnostics; with watching off and no client it worked for this.

## Reproducing

`cmd/ts-spike` was removed once `internal/ts` replaced it (plan step 3,
rename); it is in the history at commit 094bff4. There:

```
go build -o /tmp/ts-spike ./cmd/ts-spike        # from this repo
git clone --depth 1 https://github.com/honojs/hono && cd hono
pnpm install --frozen-lockfile --ignore-scripts
/tmp/ts-spike -p tsconfig.spec.json             # load and check
/tmp/ts-spike -inc -p tsconfig.spec.json -file src/utils/url.ts \
  -find 'export const getPath = (request: Request): string' \
  -replace 'export const getPath = (request: Request, base: string): string'
/tmp/ts-spike -p tsconfig.spec.json -file src/utils/url.ts \
  -find 'getPath = (' -rename pathOf -open src/utils/url.test.ts
```

Without `-inc`, the edit runs re-check every importer plainly; without
`-open`, rename shows finding 1.
