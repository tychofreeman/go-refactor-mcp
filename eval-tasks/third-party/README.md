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

LOAD_TABLE

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
outcome and size.

TABLES
