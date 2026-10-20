# Third-party A/B candidates

Feature PRs from five large Go repositories, each a candidate A/B task: the
run starts at the PR's parent commit, the prompt describes the feature, and
the PR's own tests are the hidden check. They were merged after July 1,
2026, after the models' training cutoff, so no arm can recall the real
change.

- `candidates.jsonl`: every candidate (one JSON object per line), with its
  verification where it has one, and for one with black-box tests the
  hidden check: the test files and the `go test` command.
- `candidates.py`, `verify.py`, `report.py`: how the list was made (below).

## Summary

As of each repository's history on Oct 17–20, 2026:

| Repository | Non-test Go lines | Feature candidates | Verified | Black-box | Recommended | Needs API | Not fail-to-pass | Broken |
|---|---|---|---|---|---|---|---|---|
| cli/cli | 128k | 14 | 14 | 9 | 5 | 5 | 0 | 0 |
| gohugoio/hugo | 129k | 18 | 18 | 11 | 11 | 5 | 1 | 1 |
| jesseduffield/lazygit | 135k | 49 | 15 | 9 | 8 | 6 | 0 | 0 |
| opentofu/opentofu | 244k | 31 | 12 | 5 | 4 | 4 | 2 | 1 |
| go-gitea/gitea | 341k | 60 | 15 | 8 | 7 | 7 | 0 | 0 |
| **Total** | | **172** | **74** | **42** | **35** | **27** | **3** | **2** |

All of cli/cli's and Hugo's feature candidates were verified; for the
other three, up to five per size (below), those that refactored first.
Of the 35 recommended, 16 refactored on the way and 19 did not, which
gives both strata of the A/B: tasks where the tools should help, and a
control.

## How the list was made

**Candidates** (`candidates.py`): first-parent commits (merged PRs) since
July 1, 2026, whose title is not a fix or maintenance, that change non-test
Go code in two or more packages and change tests. Each records:

- **Size** by packages of non-test Go code: S (2), M (3–5), L (6 or more).
- **Refactored on the way:** a function's signature changed and its calls
  in other files changed with it, or a function moved between files. These
  are the tasks where the tools should help most.
- **Kind:** `internal` for titles that name no behavior a prompt could ask
  for (performance, `go fix`, reworks, scaffolding, test and tooling work,
  hardening); the rest are `feature`. Internal candidates stay in the file
  but were not verified and are not in the tables.
- **Issues** the commit says it closes, which are where prompts can come
  from.

**Verification** (`verify.py`): the PR's test files are put on its parent
commit, and the tests the PR added or changed are compiled and run there,
then run again at the PR's commit. Each test package gets one of:

- **black-box:** compiles against the old code, fails there, passes after
  the PR. These tests check behavior, so the prompt can describe the
  feature in its own words and any design that delivers it passes.
- **needs API:** does not compile against the old code: the tests call
  names the PR added (listed per candidate). A prompt must name them, which
  fixes part of the design.
- **not fail-to-pass:** passes before the PR, so it does not check it.
- **failing after:** fails at the PR's commit too.

A candidate is **black-box** if any package is, **recommended** if its
black-box packages are at least half of the packages whose tests check the
PR (black-box plus needs API): its hidden check then covers most of what the
PR's own tests do. Otherwise it is **needs API** if any package is,
**not fail-to-pass**, or **broken** (its tests fail at the PR's commit, or it
added no test functions).

## Turning a candidate into a task

A task for `cmd/refactor-ab` (see `../ab.json`) needs a clone of the
repository with history back to the candidate's parent:

```json
{
  "name": "tp-hugo-render-context",
  "dir": "/path/to/clones/gohugoio_hugo",
  "rev": "<parent>",
  "prompt": "<the feature, from the issue>",
  "tests": "eval-tasks/third-party/tests/tp-hugo-render-context",
  "check": "<hidden_check.command>"
}
```

- `tests` is a directory holding the candidate's `hidden_check.files` at
  their paths, from the PR's commit:
  `git -C CLONE archive COMMIT -- FILES | tar -x -C DIR`. Only the black-box
  packages' test files go there: a file that uses a name the PR added would
  fail to compile against any design that named it differently.
- `check` is `hidden_check.command`, which runs exactly the tests the PR
  added or changed in those packages. Add `go vet` of the changed packages
  if the run should also leave clean code.
- The prompt comes from the issue (the `issues` field, or the PR), written
  as a request, not as the diff. For a needs-API candidate it must also
  name what the tests call.

## Environment

What running these needs, found while verifying them:

- **Go 1.27.** cli/cli, Hugo, OpenTofu and Gitea require it; lazygit 1.26.
- **lazygit's integration tests** (most of its black-box checks) run headless
  through `go test ./pkg/integration/clients/ -run '^TestIntegration$/...'`,
  as its CI does; its `cmd/integration_test cli` runner needs a real
  terminal. They need `git`.
- **Gitea's tests refuse to run as root**, and its test setup ignores the
  environment variable meant to allow it. Its `tests/integration` package
  runs with the default SQLite setup; three PRs' integration tests failed
  at their own commit (listed as failing after).
- **Disk:** each candidate builds test binaries for two commits; with
  several large repositories the Go build cache reached 22 GB.
  `verify.py --clean-cache` empties it after each candidate.

## Load time and memory of the workspace

Whether gorefactor copes with modules this size (roadmap item 14): the
`refactor` CLI, built from this repository at 190d1f4 with Go 1.27.3, on a
machine with 2 CPUs and 7 GB of memory. "Load" is a dry-run rename refused
right after loading and type-checking the whole module, test variants
included; "rename" is a dry-run rename of a widely used function, which also
finds its references in every package. Each is a fresh process with a warm
Go build cache; the first load of a module, which compiles its
dependencies' export data, took 18 s (lazygit) to 90 s (Hugo), and 5.6
minutes for Gitea.

| Module | Non-test Go lines | Load | Peak memory | Rename (references) | Peak memory |
|---|---|---|---|---|---|
| this repository | 30k | 0.8 s | 111 MB | 0.8 s (26 in 19 files) | 118 MB |
| lazygit | 135k | 1.5 s | 308 MB | 1.9 s (3 in 3 files) | 326 MB |
| cli/cli | 128k | 4.1 s | 408 MB | 4.1 s (659 in 224 files) | 551 MB |
| Hugo | 129k | 4.4 s | 972 MB | 4.9 s (42 in 20 files) | 978 MB |
| OpenTofu | 244k | 4.8 s | 825 MB | 6.8 s (1,347) | 1,321 MB |
| Gitea | 341k | not measured warm | 1,266 MB (first load) | 8.1 s (648 in 206 files) | 1,141 MB |

All five load and type-check, and stay under 1.4 GB. Memory grows with the
module, between 2 and 8 MB per thousand lines of non-test Go; a rename
across a thousand references costs seconds. The rename column includes the
load, so Gitea's warm load is at most 8 s. The MCP server keeps one
workspace for the session, so these costs come once per session plus each
step's incremental update; how long those take on these modules is not
measured yet.

## Regenerating

```sh
python3 candidates.py --rev origin/HEAD CLONE OWNER/REPO >> candidates.raw.jsonl
python3 verify.py CLONE candidates.raw.jsonl > verified.jsonl          # needs Go on PATH
python3 report.py candidates.raw.jsonl verified*.jsonl --out candidates.jsonl > tables.md
```

`verify.py --only COMMIT,...` verifies some; `--as-user USER` runs git and
go as another user (for Gitea); `--clean-cache` empties the build cache
after each candidate. Clones need history back past `--since`; a shallow
clone with `--depth 2500` reached July for all five.

## The candidates

Verified feature candidates by repository, recommended first, then by
outcome and size. "Black-box of checking" counts the test packages whose
tests check the PR (black-box or needs API), and how many of those are
black-box.

### cli/cli

| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box of checking test packages | Names a needs_api prompt must give |
|---|---|---|---|---|---|---|---|
| [#14072](https://github.com/cli/cli/pull/14072) |  | Set GH_EXTENSION=1 when gh invokes an extension | S (2) |  | **recommended** | 1 of 1 |  |
| [#14007](https://github.com/cli/cli/pull/14007) |  | Guard `pr merge --delete-branch` against worktree conflicts | S (2) |  | **recommended** | 1 of 2 |  |
| [#14332](https://github.com/cli/cli/pull/14332) |  | Expose api_host through gh config | S (2) |  | **recommended** | 1 of 2 |  |
| [#14215](https://github.com/cli/cli/pull/14215) |  | Reject --comments with --json | M (3) |  | **recommended** | 3 of 3 |  |
| [#14720](https://github.com/cli/cli/pull/14720) |  | Support HTML documents in `gh issue artifact` | L (8) |  | **recommended** | 5 of 6 |  |
| [#14136](https://github.com/cli/cli/pull/14136) |  | Add worktree checkout to `gh issue develop` | M (3) | moved `resolveWorktreeTarget`, moved `revParseFacts` | black_box | 1 of 3 |  |
| [#14060](https://github.com/cli/cli/pull/14060) |  | Prevent repo sync from corrupting linked worktrees | M (3) |  | black_box | 1 of 3 |  |
| [#14104](https://github.com/cli/cli/pull/14104) |  | Honour api_host across the CLI, and give api.Client a flexible requ... | L (27) | `GraphQL` signature (1 calling files), `httpRequest` signature (1 calling files) | black_box | 2 of 9 |  |
| [#14345](https://github.com/cli/cli/pull/14345) |  | Add clipboard preference for auth flows | L (7) |  | black_box | 1 of 6 |  |
| [#13807](https://github.com/cli/cli/pull/13807) |  | Add name-based resolution to gh project item-edit (#13807) | S (2) |  | needs_api |  | `FieldAmbiguousError`, `FieldIDNotFoundError`, `FieldNotFoundError`, `ResolveFieldByID` ... |
| [#13823](https://github.com/cli/cli/pull/13823) |  | Add named field columns to gh project item-list (#13823) | S (2) |  | needs_api |  | `DisplayValue`, `fieldIDs`, `listOpts.fieldIDs`, `fields` ... |
| [#14198](https://github.com/cli/cli/pull/14198) |  | Print full help on command misuse for invoking agents | S (2) |  | needs_api |  | `WriteHelp`, `printError (arguments)` |
| [#14006](https://github.com/cli/cli/pull/14006) |  | Add --search-type flag for semantic and hybrid issue search | M (3) |  | needs_api |  | `SearchFeatures.HybridSearch`, `Query.IssueSearchType`, `search.Query.IssueSearchType`, `SearchFeatures.SemanticSearch` ... |
| [#14289](https://github.com/cli/cli/pull/14289) |  | Limit attachment batches to 50 files | L (7) |  | needs_api |  | `maxAttachments` |

### gohugoio/hugo

| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box of checking test packages | Names a needs_api prompt must give |
|---|---|---|---|---|---|---|---|
| [`d83ce27ae`](https://github.com/gohugoio/hugo/commit/d83ce27ae01f) | [#15056](https://github.com/gohugoio/hugo/issues/15056) | tpl/tplimpl: Support sub paths in layouts passed to .Render | S (2) |  | **recommended** | 1 of 1 |  |
| [`33d1f2c85`](https://github.com/gohugoio/hugo/commit/33d1f2c853ec) | [#15167](https://github.com/gohugoio/hugo/issues/15167) | css: Add classDark and classLight options to css.ChromaStyles and g... | S (2) |  | **recommended** | 1 of 1 |  |
| [`bcde80634`](https://github.com/gohugoio/hugo/commit/bcde80634079) | [#15077](https://github.com/gohugoio/hugo/issues/15077) | hugolib: Make .Render take an optional context argument | S (2) | `Render` signature (3 calling files) | **recommended** | 1 of 1 |  |
| [`6a2a955d2`](https://github.com/gohugoio/hugo/commit/6a2a955d252b) | [#15301](https://github.com/gohugoio/hugo/issues/15301), [#15302](https://github.com/gohugoio/hugo/issues/15302) | config/security: Add http.proxyFromEnvironment and document the res... | S (2) |  | **recommended** | 1 of 2 |  |
| [#15376](https://github.com/gohugoio/hugo/pull/15376) | [#15373](https://github.com/gohugoio/hugo/issues/15373) | tpl/partials: Support relative partial paths from within partials (... | S (2) |  | **recommended** | 1 of 1 |  |
| [`c9f6e4d4a`](https://github.com/gohugoio/hugo/commit/c9f6e4d4abc3) | [#15430](https://github.com/gohugoio/hugo/issues/15430) | Add ImageResource.FrameCount | S (2) |  | **recommended** | 1 of 1 |  |
| [`44da08608`](https://github.com/gohugoio/hugo/commit/44da086082a1) | [#15173](https://github.com/gohugoio/hugo/issues/15173) | Add Data.Artifacts to css.Build and js.Build | M (3) |  | **recommended** | 2 of 3 |  |
| [`8405b802c`](https://github.com/gohugoio/hugo/commit/8405b802cf64) | [#15212](https://github.com/gohugoio/hugo/issues/15212) | tpl: Improve the return keyword in templates | M (4) | moved `walkTemplate` | **recommended** | 2 of 2 |  |
| [`df8badea2`](https://github.com/gohugoio/hugo/commit/df8badea25f9) | [#7844](https://github.com/gohugoio/hugo/issues/7844) | Add Translate and Format* methods to Language | M (4) | `New` signature (1 calling files) | **recommended** | 1 of 2 |  |
| [`13ee7e975`](https://github.com/gohugoio/hugo/commit/13ee7e9751a6) | [#15433](https://github.com/gohugoio/hugo/issues/15433) | Add imaging.firstFrameOnly and some animation performance improvements | M (3) | `Decode` signature (2 calling files), `DecodeFormat` signature (1 calling files) | **recommended** | 1 of 1 |  |
| [`70db201ed`](https://github.com/gohugoio/hugo/commit/70db201ed4f7) | [#15103](https://github.com/gohugoio/hugo/issues/15103) | Add importContext option to css.Build, js.Build, css.Sass and css.T... | L (14) |  | **recommended** | 1 of 2 |  |
| [`861ede6d1`](https://github.com/gohugoio/hugo/commit/861ede6d1031) | [#15101](https://github.com/gohugoio/hugo/issues/15101) | cache/filecache: Don't prune used cache entries with mixed-case dir... | S (2) |  | needs_api |  | `htesting.IsCaseInsensitiveFs` |
| [`aaacd12c5`](https://github.com/gohugoio/hugo/commit/aaacd12c5c08) | [#15332](https://github.com/gohugoio/hugo/issues/15332) | tpl/strings: Add a strings.FirstLower function | S (2) |  | needs_api |  | `helpers.FirstLower` |
| [`c6acc246a`](https://github.com/gohugoio/hugo/commit/c6acc246ab10) | [#5349](https://github.com/gohugoio/hugo/issues/5349) | Add markup.rst.syntaxHighlight option | M (3) |  | needs_api |  | `rst.SupportsPygments` |
| [`615e45d6e`](https://github.com/gohugoio/hugo/commit/615e45d6e822) | [#15112](https://github.com/gohugoio/hugo/issues/15112) | Add css.ChromaStyles | M (3) | moved `chromaCSSOverrides`, moved `chromaLeafToken` | needs_api |  | `ChromaStyles` |
| [`d1024ae6b`](https://github.com/gohugoio/hugo/commit/d1024ae6b895) | [#15425](https://github.com/gohugoio/hugo/issues/15425) | Add basic support for OSC 7501 program status | M (4) | `ReportProgress` signature (2 calling files) | needs_api |  | `ProgressDone`, `maxStatusMsgLen`, `statusMsg`, `terminal.StripANSI` ... |
| [`a36bd2745`](https://github.com/gohugoio/hugo/commit/a36bd2745010) |  | Add internalExternal.IgnoreTailwindCSSSecurityError config option | M (3) |  | not_f2p |  |  |
| [`5d43ab700`](https://github.com/gohugoio/hugo/commit/5d43ab70005f) | [#14352](https://github.com/gohugoio/hugo/issues/14352) | Add slug support for section, taxonomy and term pages | S (2) |  | broken |  | tests fail at the PR's commit |

### jesseduffield/lazygit

| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box of checking test packages | Names a needs_api prompt must give |
|---|---|---|---|---|---|---|---|
| [#5924](https://github.com/jesseduffield/lazygit/pull/5924) |  | Show renames when selecting a directory that a file was moved into ... | S (2) | `WorktreeFileDiffCmdObj` signature (2 calling files) | **recommended** | 1 of 2 |  |
| [#6024](https://github.com/jesseduffield/lazygit/pull/6024) |  | List stacked branches in stack order (#6024) | S (2) | `NewRemoteLoader` signature (1 calling files), moved `parseAheadBehindForEachRefOutput`, moved `parseAheadBehindField` | **recommended** | 1 of 2 |  |
| [#6069](https://github.com/jesseduffield/lazygit/pull/6069) |  | Auto-forward branches even if checked out in another worktree (#6069) | S (2) |  | **recommended** | 1 of 1 |  |
| [#6090](https://github.com/jesseduffield/lazygit/pull/6090) |  | Keep copied commits when switching to another worktree or repo (#6090) | S (2) |  | **recommended** | 1 of 1 |  |
| [#6100](https://github.com/jesseduffield/lazygit/pull/6100) |  | Show errors for unresolvable diff lines (#6100) | S (2) |  | **recommended** | 1 of 1 |  |
| [#5748](https://github.com/jesseduffield/lazygit/pull/5748) | [#5205](https://github.com/jesseduffield/lazygit/issues/5205) | Improve deleting worktrees and their branches (#5748) | M (3) | `Remove` signature (2 calling files), `Detach` signature (1 calling files) | **recommended** | 1 of 1 |  |
| [#6035](https://github.com/jesseduffield/lazygit/pull/6035) |  | Stage, unstage and discard changes directly from the focused main v... | L (10) | `ShowCmdObj` signature (2 calling files), `DiffCmdObj` signature (2 calling files), moved `SelectedHunkBounds` | **recommended** | 2 of 4 |  |
| [#6104](https://github.com/jesseduffield/lazygit/pull/6104) | [#6102](https://github.com/jesseduffield/lazygit/issues/6102) | Open files of a branch's diff from the worktree that has it checked... | L (7) | `NewDiffHelper` signature (1 calling files) | **recommended** | 1 of 1 |  |
| [#6123](https://github.com/jesseduffield/lazygit/pull/6123) |  | Make double-click detection more precise (#6123) | M (4) |  | black_box | 1 of 3 |  |
| [#5740](https://github.com/jesseduffield/lazygit/pull/5740) | [#1453](https://github.com/jesseduffield/lazygit/issues/1453), [#5525](https://github.com/jesseduffield/lazygit/issues/5525) | Support custom pagers and passphrase prompts on Windows (#5740) | M (4) | `TerminateProcessGracefully` signature (2 calling files) | needs_api |  | `Cmd`, `ExecCmd`, `cursorDown`, `cursorForward` ... |
| [#6063](https://github.com/jesseduffield/lazygit/pull/6063) |  | Allow telling diff renderers whether the terminal is dark or light ... | M (4) | `GetStdinFilterCommand` signature (1 calling files), `GetExternalDiffCommand` signature (1 calling files) | needs_api |  | `DiffRendererValues`, `config.GetExternalDiffCommand (arguments)` |
| [#6041](https://github.com/jesseduffield/lazygit/pull/6041) |  | Alt- or shift-click a diff line to open it in your editor (#6041) | M (5) | `ForceFlushViewsContentOnly` signature (1 calling files) | needs_api |  | `AltClick`, `ClearLineFlash`, `ViewMouseBinding.HandleWhenPopupPanelFocused`, `SetLineFlash` ... |
| [#6067](https://github.com/jesseduffield/lazygit/pull/6067) |  | Use `f` to "fast-forward" branches whose upstream branch was rewrit... | L (7) | `UpdateBranchRefs` signature (1 calling files) | needs_api |  | `HasLocalOnlyCommits`, `UpstreamRewritten`, `checkForRewrittenUpstreams`, `BranchLoader.hasLocalOnlyCommits` ... |
| [#6077](https://github.com/jesseduffield/lazygit/pull/6077) |  | Improve the rendering of the commit graph for terminals that suppor... | L (6) | `render` signature (1 calling files), `setUp` signature (1 calling files) | needs_api |  | `BoxDrawingSymbols`, `BranchDrawingSymbols`, `TerminalDrawsBranchDrawingSymbols`, `branchDrawingCommitSymbols` ... |
| [#6032](https://github.com/jesseduffield/lazygit/pull/6032) |  | Keep your scroll position in the diff when the diff is rendered aga... | L (6) | `NewDiffHelper` signature (1 calling files) | needs_api |  | `RenderRestore`, `SelectedViewLineRange`, `SetKeepScrollPositionForNextTask`, `SetRestoreForNextTask` ... |

### opentofu/opentofu

| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box of checking test packages | Names a needs_api prompt must give |
|---|---|---|---|---|---|---|---|
| [#4466](https://github.com/opentofu/opentofu/pull/4466) |  | Linting rule: unused variables and locals for root modules (#4466) | S (2) |  | **recommended** | 1 of 2 |  |
| [`4ac35bd5d`](https://github.com/opentofu/opentofu/commit/4ac35bd5d9da) |  | tracing: Honor standard OTEL_RESOURCE_ATTRIBUTES env var | S (2) | `NewResource` signature (1 calling files) | **recommended** | 1 of 1 |  |
| [#4680](https://github.com/opentofu/opentofu/pull/4680) |  | Restore autocompletion for the workspace commands (#4680) | S (2) | `completePredictWorkspaceName` signature (2 calling files) | **recommended** | 1 of 1 |  |
| [#4067](https://github.com/opentofu/opentofu/pull/4067) |  | Add support for instances in test resource overrides (#4067) | M (3) | `parseResourceInstanceUnderModule` signature (2 calling files) | **recommended** | 1 of 2 |  |
| [#4645](https://github.com/opentofu/opentofu/pull/4645) |  | Support adding type annotations for module outputs (#4645) | M (3) |  | black_box | 1 of 3 |  |
| [`8535390a1`](https://github.com/opentofu/opentofu/commit/8535390a14ef) |  | Implement refresh in the new runtime | S (2) |  | needs_api |  |  |
| [#4407](https://github.com/opentofu/opentofu/pull/4407) |  | Improve integration of view arguments (#4407) | M (3) | `NewApply` signature (1 calling files), `NewConsole` signature (1 calling files) | needs_api |  | `View.InputEnabled`, `Apply.View`, `View.ViewType`, `arguments.View.ViewType` ... |
| [`b9e49ccee`](https://github.com/opentofu/opentofu/commit/b9e49ccee38a) |  | Implement target and exclude within the new engine | M (5) |  | needs_api |  |  |
| [`6d6cdf04c`](https://github.com/opentofu/opentofu/commit/6d6cdf04c4e1) |  | Add support for root module outputs in new engine | L (6) |  | needs_api |  | `OutputValues` |
| [`d15085149`](https://github.com/opentofu/opentofu/commit/d15085149943) |  | Implement force replace in the new engine | S (2) | `newPlanContext` signature (2 calling files) | not_f2p |  |  |
| [`f74f330fd`](https://github.com/opentofu/opentofu/commit/f74f330fdbe0) |  | Support Provisioners in the new runtime | L (9) |  | not_f2p |  |  |
| [`1692aaddb`](https://github.com/opentofu/opentofu/commit/1692aaddb4b4) |  | eval: ConfiguredResourceInstanceObjectMeta from ancestor modules too | M (5) | `ResourceInstanceObjectMeta` signature (5 calling files) | broken |  | no added or changed test functions |

### go-gitea/gitea

| PR | Issue | Title | Size (packages) | Refactored on the way | Outcome | Black-box of checking test packages | Names a needs_api prompt must give |
|---|---|---|---|---|---|---|---|
| [#39267](https://github.com/go-gitea/gitea/pull/39267) |  | enhance(packages): implement npm single-version API and add per-ver... | S (2) | `createPackageSearchResponse` signature (1 calling files) | **recommended** | 2 of 2 |  |
| [#39289](https://github.com/go-gitea/gitea/pull/39289) |  | enhance: support `ETag` on streamed repository archives, support `I... | S (2) |  | **recommended** | 2 of 2 |  |
| [#39373](https://github.com/go-gitea/gitea/pull/39373) |  | enhance(citation): render citations server-side (#39373) | S (2) |  | **recommended** | 1 of 2 |  |
| [#39785](https://github.com/go-gitea/gitea/pull/39785) |  | enhance(oauth): pre-register a builtin OAuth2 application for gitea... | S (2) |  | **recommended** | 1 of 1 |  |
| [#38756](https://github.com/go-gitea/gitea/pull/38756) |  | feat(actions): add force-cancel workflow run API (#38756) | M (4) | `CancelJobs` signature (3 calling files) | **recommended** | 1 of 2 |  |
| [#39413](https://github.com/go-gitea/gitea/pull/39413) |  | feat(user): allow renaming security keys (webauthn/passkey) (#39413) | M (5) | `GetWebAuthnCredentialByID` signature (1 calling files), `DeleteCredential` signature (1 calling files) | **recommended** | 1 of 2 |  |
| [#39816](https://github.com/go-gitea/gitea/pull/39816) |  | enhance(pull)!: add per-repository default squash merge commit mess... | L (8) | `deprecatedSetting` signature (7 calling files) | **recommended** | 1 of 2 |  |
| [#39358](https://github.com/go-gitea/gitea/pull/39358) |  | feat(actions): update actionslib, support `self:`, misc fixes (#39358) | L (8) | `EvaluateJobIfExpression` signature (1 calling files), `FindTaskNeeds` signature (2 calling files) | black_box | 1 of 5 |  |
| [#39804](https://github.com/go-gitea/gitea/pull/39804) |  | enhance(repo): prefill new issue and pull request sidebars from que... | S (2) |  | needs_api |  | `base.JoinInt64s`, `retrieveRepoIssueMetaDataWithPrefill` |
| [#38371](https://github.com/go-gitea/gitea/pull/38371) |  | enhance(actions): only create filtered-out workflow commit status f... | M (3) | `CreateSkippedCommitStatusForFilteredWorkflow` signature (1 calling files), `EffectiveRequiredContexts` signature (1 calling files) | needs_api |  | `EffectiveRequiredContexts (arguments)` |
| [#39720](https://github.com/go-gitea/gitea/pull/39720) |  | enhance(actions): show the steps the runner reports (#39720) | M (5) |  | needs_api |  | `IsReported`, `runnerv1.StepState.Name`, `runnerv1.TaskState.ReportedSteps`, `runnerv1.StepState.Stage` ... |
| [#39652](https://github.com/go-gitea/gitea/pull/39652) |  | enhance: allow custom watches to bypass on-mention email restrictio... | M (4) |  | needs_api |  | `repo_model.GetRepoCustomWatchersIDs`, `repo_model.WatchModeCustom` |
| [#38189](https://github.com/go-gitea/gitea/pull/38189) |  | feat: Add audit logging (#38189) | L (45) | `GetOAuthAccessTokenScopeAndUserID` signature (2 calling files), `DeleteSecretByID` signature (1 calling files) | needs_api |  | `AddAuditEventTable`, `Audit`, `AuditRecordEnabled`, `AuditRecordOutput` ... |
| [#39243](https://github.com/go-gitea/gitea/pull/39243) |  | feat(repo): allow deleting protected branches (#39243) | L (9) | `LoadBranches` signature (1 calling files) | needs_api |  | `CanDelete`, `ProtectedBranchConfig.CanDelete`, `DeletionAllowlistTeamIDs`, `ProtectedBranchConfig.DeletionAllowlistTeamIDs` ... |
| [#38415](https://github.com/go-gitea/gitea/pull/38415) |  | feat: add release reactions and a GitHub-like reaction bar (#38415) | L (14) | `FindCommentReactions` signature (1 calling files), `CreateReaction` signature (2 calling files) | needs_api |  | `AllowedReleaseReactions`, `CreateReleaseReaction`, `issues.FindReactionsOptions.IssueIDOnly`, `issues.Reaction.ReleaseID` |

