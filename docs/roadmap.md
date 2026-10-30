# Roadmap

Moved out of the README on 2026-10-30, numbering unchanged, so "roadmap
item N" anywhere in the repository still means item N here. Sections it
names in quotes, such as "Rename: what it checks", are in the README.

The goal is a workflow for AI-driven development: during a story, Claude
picks the refactorings that make the change easy, the tools perform them
deterministically, and Claude makes the small edit that adds or removes the
feature. The order below follows what that workflow needed in practice;
`docs/friction-log.md` records each time a refactoring had to be done by
hand or a tool got in the way.

1. **Changing a signature** at every call site. The most common by-hand
   step so far. *`add_parameter`, `remove_parameter` and `move_parameter`
   done, for exported methods too (see "Add parameter", "Remove parameter",
   "Move parameter: what it checks" and "Changing a method: interfaces").
   `introduce_parameter` turns an expression of the body into a parameter
   (see "Introduce parameter: what it checks"), also through the receiver
   or a pointer read at entry; next: every occurrence at once, and calls
   on paths that return before the expression.
   Left: methods of generic types that an interface could match, and
   interface methods with all their implementations.*
2. **Moving a declaration** to another file or package, updating its
   references (and renaming it on the way when it changes package).
   *`move_declaration` done, within a package and to another one (see
   "Move declaration: what it checks"), with several declarations at once,
   from any files of the package (`also`), out of a grouped declaration,
   a type with its methods, and into a new package.*
3. **Evidence from real use.** Keep the friction log during normal work.
   Later, a comparison: the same small stories done with and without the
   tools, comparing the size of the hand-written edits, test failures and
   review effort. `refactor-eval`'s metrics and throwaway copies can carry
   over (see "Measuring whether the tools help"); its first runs showed that
   complexity alone rewards bad extractions, and that applied counts must
   subtract undos.
4. **`inline_function`:** the error-exit shape
   (`x, err := f(); if err != nil { return ..., err }`), generic and variadic
   functions, inlining every call at once, and callees from other packages
   (see "Inline function: what it checks").
5. **Statement refactorings**, the behavior-preserving forms of edits in
   `docs/edits.md`: *sliding a statement up or down past an independent
   one is done (see "Move statement up and down: what they check"), and
   it knows which of the module's own functions have no side effects (see
   "Calls of the module's own functions"); next: the other refactorings
   using those summaries too.* Then: introduce a plain block, and move a
   statement into or out of one; and
   statements with no effect for `remove_dead_code` (unused declarations
   are done). Deleting a statement for its
   effect is the edit `delete_statement`.
6. **`rename` gaps:** imports, labels, types that are embedded, and
   interface methods (see "Rename: what it checks"). Embedded fields:
   *Done* (renamed into named fields).
7. **Packaging:** tagged releases, so `go install ...@latest` gets a stable
   version, and possibly a Claude Code plugin for a one-step install.
   *Done: a release follows a change of `mcpserver.Version` on main
   (`.github/workflows/release.yml`, notes from `docs/releases/`); the
   first is v0.3.0 (2026-10-27). Left: the plugin.*
8. **Test run time.** CI's test step grew from about 13 s to 75 s per Go
   version between Oct 16 and Oct 19, in step with the golden cases (132 to
   824): about 9 s per 100 cases under `-race`. Each case loads a module
   with `packages.Load`, so the cost is per case, not per assertion. In
   order:
   - **Measure** per package and per test (`go test -json`), so the rest
     is decided on numbers. *Done: each CI job reports its slowest
     packages, tests and golden cases in the job summary and as a notice
     annotation (`.github/test-times.sh`).*
   - **Make the fresh-load comparison cheaper.** `refactortest.Run`
     compares the workspace with a fresh load after applying and after
     undoing each applied case, and that was over half the test time (on
     Oct 19, CI's package times summed to 267 s with it and 122 s with
     `-short`, which skips it). *Done: about 40 ms of a 47 ms load is
     `go list`, which only reports metadata (packages, their files and
     imports). A fresh load now takes the metadata of an earlier load of
     the case when `workspace.Fingerprint` (each Go file up to the end of
     its imports, go.mod, go.sum) shows go list would report the same, as
     it always does after undoing, and only parses and type-checks. Each
     case checks such a load against its first, real one. Locally the sum
     of package times went from 164 s to 109 s, against 90 s with
     `-short`.*
   - **`-race` costs little,** about 5% of package time, within run-to-run
     noise, so it stays in both CI jobs.
   - **Tests that start `go test`:** `mcpserver.TestRefactoringLoop`
     (`check` with `run_tests`) and `agent.TestRunUndoesWhenTestsBreak`.
   - **Repeated tests.** Each costs a load or two, so these come last:
     `mcpserver` tests that repeat `refactor`'s checks of batches, compact
     diffs and broken input (keep only the wiring); the six
     `refuse_type_error` cases of the edits, which all test
     `refactor.Run`'s check; and undo after an external edit, tested in
     both `workspace` and `mcpserver`.
   - **Not for speed:** the stress tests copy `envInt`, `tailLines` and
     their candidate loop six times (`inlinefunc` has them factored out
     already), `inlinevar`'s `copyDir` is `refactortest.copyTree`, and
     `extractvar.TestTokens` tests `locate`.
9. **Intent tools.** Grouped by what the person wanted, the friction log's
   hand edits fall into a few intentions, each of which took several tools
   or a step no tool covers. An intent tool takes the intention and works
   out the steps, often from a plan another tool already prints in its
   refusal. By evidence (dates are friction-log entries):
   - **Move whatever a move needs.** `move_declaration`'s refusals name
     the step to take first: move the methods it needs, turn methods into
     functions, replace a variable initialized by a call. This tool takes
     those steps itself, as one batch. Five stories (Oct 17: adding
     `introduce_parameter`, a type with its methods, moving from several
     files; Oct 18: one interface check, adding `insert_statements`).
     *First part done: `move_declaration` with `with_dependencies` moves
     what the moved code uses of its package, exporting what the package
     still uses. Left: exporting fields and methods other code of the
     package still uses, and the steps that change code rather than move
     it (methods into functions, a variable initialized by a call).*
   - **Merge near-duplicate functions.** `merge_duplicate_function`
     refuses copies that differ, and names the difference; this tool makes
     each difference a parameter instead. Also `extract_function` over
     several ranges that differ only in expressions. Five stories (Oct 17:
     adding `move_parameter`, a type with its methods; Oct 18: one
     interface check, adding `merge_duplicate_function`, adding
     `add_declaration`).
   - **Narrow a parameter or receiver to the fields it uses.** Today this
     is `method_to_function`, `introduce_parameter` for each field, then
     `remove_parameter` and `function_to_method`, and it stalls at the
     first getter in a closure. Narrowing the receiver to one field is
     "move a method to a field's type". Four stories (Oct 17: adding
     `refactor-eval`, adding `introduce_parameter`, moving from several
     files; Oct 18: one interface check).
   - **One to many.** A field that held one item becomes a slice, and each
     use a loop over it (`introduce_loop` in `docs/edits.md`).
     `extract_struct` does the first step. Three stories, each the largest
     hand edit of its story (Oct 17: several declarations at once, moving
     from several files; Oct 18: chained tasks). Generating code from a
     description, such as the sum of a field over a list, belongs here
     and with `create_from_usage`; the log has two such loops written by
     hand (Oct 17: adding `refactor-eval`, adding `move_parameter`).
   - **Results, and a value through several calls.** `add_result`,
     `remove_result` and `change_result_type`, the counterparts of the
     parameter tools (`change_parameter_type` is done), with a pointer
     that is only written becoming a result; and a parameter added along
     every call path from one function to another. Five stories (Oct 17:
     adding `refactor-eval`, several declarations at once; Oct 18: one
     interface check, `move_statement_up`, summaries of the module's own
     functions).
   - **Replace a parameter by one computed from it.** `replace_parameter`:
     a new parameter whose argument at each call is computed from the old
     one (`qual types.Qualifier` becoming `write func(types.Type) string`,
     passed `func(t types.Type) string { return types.TypeString(t,
     qual) }`), the body's uses rewritten in terms of it, and the old
     parameter removed, as one step. It is not a type change, since no
     conversion joins the two, so `change_parameter_type` refuses it. One
     story (Oct 19: generic types, 13 `replace_text` steps; noted again
     under change_parameter_type).
   - **Every occurrence of a pattern.** One edit taking `gofmt -r`-style
     patterns, such as `ast.Inspect($d.decl, $f)` to
     `ast.Inspect($d.node(), $f)`, across the module and checked as one
     step, in place of an all-occurrences option in each tool. It skips an
     occurrence inside a function the replacement calls, which
     `all_occurrences` once rewrote into endless recursion. Four stories
     (Oct 17: out of a grouped declaration; Oct 18: addressing
     declarations by name, compact results; Oct 19: generic types), and
     the "every occurrence at once" left in items 1 and 4.
   - **Finish code as the file does.** Not an intention, but the most
     small hand edits, about eight: wrap doc comments at the file's width,
     group adjacent parameters of one type, put third-party imports apart
     from the module's, note an exported name without a doc comment. One
     shared pass after every tool.
10. **Addressing what earlier steps of a batch made.** A step's lines are
    as of the start of the batch, so a line an earlier step rewrote, or
    any line of a file it created, cannot be addressed, and "after what
    the previous step inserted" has no address at all (friction log,
    Oct 18: line numbers in a batch; Oct 19: locals by function and
    name). *Done: a step may take an `id`, and a later step's `line`,
    `start_line` or `end_line` may be a reference to an earlier step's
    range of new lines in that file: `$helper.end`, `$2.start` (by
    position), `$prev.end` (the step before), with offsets such as
    `$prev.end+1`. `RunBatch` resolves them when the step runs, before
    the tool sees its arguments, so no tool's schema changes. A range
    follows the later steps, as marks would (option 5), and leaves out
    the step's imports. A step that changed several places in the file,
    or whose lines a later step rewrote, is refused as a reference.* The
    other options (one special address, a cursor, search from a
    reference) are in `docs/batch-addressing.md`.
11. **Recipes: saved batches with parameters.** Not urgent; later. A
    file holds a batch's steps with placeholders (`${type}`,
    `${field}`), and a `run_recipe` tool or `refactor recipe` fills them
    in and runs them with `RunBatch`, so a recipe rolls back and undoes
    as one step. Portable only for steps addressed by name, and for
    lines the recipe's own steps made (item 10's references). No loops,
    conditions or step outputs; those, and the other ways to define a
    transformation (pattern templates, inlinable functions, snippets,
    scripts, Go code), are in `docs/user-transformations.md`.
12. **Smaller results: test a summary in place of the diff.** A result
    stays in the context, so every later request sends it again. Steps
    whose result follows from their arguments (`move_parameter`,
    `replace_text`, edits whose content the model supplied) could return
    a summary and where the result landed; steps that choose
    (`extract_function`, `create_from_usage`) keep the diff. In order:
    - **Measure the possible win:** `refactor-tokens` on recent sessions,
      for the share of tokens that are results of applied steps. A few
      percent ends the item here, with the number in the friction log.
    - **Build:** a `Plan` field marking a result as predictable, and a
      server flag `-results summary` beside `-full-diffs`.
    - **Test:** an A/B arm "mcp-summary" against "mcp" on the same tasks,
      comparing total tokens, `inspect` calls and file reads after
      applied steps, undos, refusals and failed checks, and the final
      diff. The proposal and what would count as a result are in
      `docs/result-size.md`.
13. **Generators: less code written by the model.** The edits take new
    code as `content`, so the model still writes every loop, test and
    changed call. Generators take a description and write the code from
    the types, so a batch carries names and expressions rather than
    statements. First the two with the most evidence (see "Generators"
    in `docs/edits.md`):
    - **`create_loop`:** a function or statements computing a sum,
      count, min or max, filter, map, any/all or index over a slice or
      map, from `over`, `op` and `field`. A summing loop was written by
      hand twice (Oct 17), and "one to many" in item 9 was the largest
      hand edit of three stories.
    - **`replace_parameter`:** a parameter replaced by one computed from
      it, rewriting the signature, the body's uses and every call. It
      took 13 `replace_text` steps on Oct 19, a case
      `change_parameter_type` does not cover.

    Then `create_test` (table-driven tests from cases as data), and the
    suggestions without evidence yet (a call with its error check, the
    IDE "Generate" set). Measure progress by the code in edit arguments
    per story: `refactor-tokens`' input tokens for `create_file`,
    `replace_*`, `insert_statements` and `add_declaration`.
14. **Large modules: measure memory and time.** The workspace keeps the
    syntax and type information of every package of the module (test
    variants too) for the whole session; dependencies, the standard
    library included, come as types only, from export data. So memory
    grows with the module's own lines, likely tens to hundreds of times
    their size, and time with how much imports what: an edit to a
    package most of the module imports re-type-checks most of it, and
    some tools (`rename`'s references, interface checks, the effect
    summaries) look at every package. Nothing has measured this on a
    large module yet; `BenchmarkApplyUndo` and `BenchmarkLoad` use a
    generated module of 20 small packages. In order:
    - **Measure** peak memory (`/usr/bin/time -v`) and time of a full
      load, `check`, an edit with its undo in a package most of the
      module imports, and a `rename` of a widely used name, on real
      modules of growing size: this one, golang.org/x/tools, and
      kubernetes. Record the numbers here, and per line of Go. *First
      numbers (Oct 20, `eval-tasks/third-party/README.md`): five modules
      of 128k to 341k lines of non-test Go all load and type-check, at
      300 MB to 1.3 GB (2 to 8 MB per thousand lines). With a warm build
      cache four load in under 5 s, and a rename preview across 650 to
      1,350 references, its load included, takes 4 to 8 s; a first load
      compiles the dependencies (18 s to 5.6 min, Gitea). Left: an edit
      with its undo, and `check`, on these modules.*
    - **Decide on the numbers.** If memory or time is a problem at the
      sizes people work in, the usual remedies: load only the packages a
      step needs and those that import them, or keep the syntax only of
      packages being edited and cache the types of the rest, as gopls
      does since its 2023 redesign.
15. **Teach a fresh model through the tools, not the prompt.** On Oct 19
    a fresh model with the MCP cost more than one without it: it guessed
    argument names, cut statements with its line ranges, and sent steps
    that called functions not written yet without `create_from_usage`.
    In order:
    - **Refusals that say what to send:** a batch checks every step's
      argument names first, and a range that cuts a statement gets the
      whole-statement range to send. *Done (#113).*
    - **Defaults that do it for the model:** a batch whose edit does not
      type-check runs again with check "end"; functions and methods the
      steps call but nothing declares get stubs (`create_from_usage`), in
      a batch or a single edit, except a name close to a declared one;
      an unknown argument name that can only mean one missing argument
      is taken as it (`old_text` as `old`). *Done.*
    - **Follow lines through a step's own edits,** not a line diff, so a
      step after `extract_function` can address the lines below it. *Done:
      a line diff could pair a line with the moved copy and put the
      next step there, not only refuse it.*
    - **Fixes from the rerun** (`docs/cost-history.md`): the features'
      mcp arm fell from 6.77M to 4.91M input tokens; chains did not
      change. In order:
      - **`add_declaration` replaces a stub.** The model writes a call,
        then declares the helper; the stub the call already got made that
        declaration a "redeclared" refusal (4 in 8 runs). A function whose
        body is only `panic("not implemented")` is replaced, with a note.
        *Done.*
      - **Edits may start from code they fix.** A test written first used
        a field that `add_field` was then refused to add, since the code
        did not type-check. Like `create_from_usage`, an edit may run on
        code that does not type-check if it leaves fewer errors and no
        new one. *Done.*
      - **`add_field` takes the type separately** (`field` and `type`, as
        the model sent twice) and joins them. *Done.*
    - **Ranges that start at the `func` line.** In the last chain run the
      model selected a whole function, signature included, for
      `extract_function` (3) and `replace_statements` (2). The refusal
      could give the body's lines, and for `replace_statements` point to
      `function`, which replaces the whole body. *Done: both refuse with
      the function's name and the lines of its statements, and
      `replace_statements` names `function`. Before, the refusal named a
      `func` token and, for a range that cut the body, suggested the whole
      function, which was refused in turn. Still a refusal, not a guess:
      a model that selected a whole function may have written a new
      signature in `content`.*
    - **Coaching in results,** when a model does something a tool could
      have done (three single steps that could have been one batch).
      Tried in #119 (a tip on the second single step in a row) and
      removed: the model never batched after it, and most single steps
      in a row were parallel calls in one turn, which cost nothing
      extra. Serial steps a batch would save came about once per run.
    - **Reading through `inspect`, not whole files.** In the feature runs,
      whole Go files read with Read, `cat` or `inspect` were over half of
      what the reading cost (counting each result once per later turn).
      `inspect` of a file now shows it without function bodies, a third to
      a half of the file; a function or range shows only that, without the
      outline; and `targets` reads several in one call, as `cat a; cat b`
      did. *Done.* Measured: the model used it more (3 → 10 calls) and
      read fewer whole files, but read the bodies with wide `sed -n`
      ranges in Bash instead, so reading cost the same.
    - **Turns spent only on loading schemas.** The tools' definitions are
      about 77K characters, so Claude Code defers them and a model loads
      each schema with ToolSearch. In `ab-features-read-inspect` it loaded
      the first ones alongside its first read, for free, but 9 of 141
      turns (6.5% of input tokens) did nothing but load a schema it then
      needed: `batch` (4), `create_file` (3), `replace_text` (3).
      Loading more up front barely pays, since a loaded schema is sent
      again with every later request (the best set, 8 tools, saved 7
      turns but under 1% of tokens). *Done: the instructions say to load
      schemas in the same response as another call, and the definitions
      of `batch`, `replace_statements` and `add_declaration`, and the
      sentence every refactoring and edit ends with, are shorter: the 8
      tools a model loads most went from 15.7K to 12.5K characters.
      Measured: turns of only ToolSearch fell from 9 to 5 in 130; totals
      did not move.*
    - **Turns denied to the mcp arm.** In `ab-features-read-inspect`, 15
      Write attempts and 18 Bash commands that write (often test
      fixtures) were refused, about a tenth of the turns. Find what each
      was for and give the tools a way to do it: perhaps `create_file`
      for several files, or for files that are not Go. Counted by turns
      over the last three feature runs, 50 of 385 turns were refused:
      test fixtures 20 (every case-clause run: heredocs, then Write,
      then a ToolSearch for `create_file`), one `sed -i` across several
      files 14 (symbol-addressing), reads the arm's Bash allowlist cannot
      match 11 (loops, variables, brace expansion; a harness limit, which
      a user would get as a prompt), new files and one-file `sed -i` 5.
      *Done for fixtures: the instructions say `create_file` makes any
      new file, Go or not, several in one batch; and for one change in
      several files: `replace_text` takes `files` in place of `file`.
      Measured (`ab-features-denied`): no Write attempts (17 before),
      `files` used in both symbol-addressing runs, refused turns 17 to
      14 of 120.* Left:
      - **One Bash try before the tool.** The model still tries `mkdir`
        or `sed -i` once (8 turns in that run) before `create_file` or
        `replace_text`. The mcp arm's CLAUDE.md could say its Bash
        cannot change files; that changes the arm, not the tools.
        *Done (#175): `eval-tasks/claude-md-refactor-first-bash`, the
        `-inspect` one with a paragraph saying so; not yet measured.*
      - **Reads the allowlist refuses** (6 turns in that run): in
        `claude -p`, Bash with loops, variables or brace expansion is
        refused where a user would get a prompt. Count them apart from
        the tools' waste in the analyses, or allow them in the arm if
        that can be done without letting writes through.
    - **A smaller module overview.** `inspect` with no arguments lists
      every file with its line count, about 8.9K characters for this
      repo. In `ab-features-read-questions` it was read first in 4 of 8
      runs and was 17% of what reading cost. Packages with their file
      names on one line could be a quarter of it. *Done (#171): one line
      per package, its files with their line counts, the package name
      only where it is not the directory's; 10.7K to 4.8K characters for
      this repo as it is now.*
    - **"Where is X used" without broad greps.** The same run asked it
      2.6 times per run, 22% of read cost; four greps of 7-12K
      characters (such as `\.Line\b`) were 12%. Evidence for the
      type-aware find tool on the back burner (check gopls's MCP server
      first). Reads are about 15% of input, so a small win in total.
    - **Smaller bugs:** `create_from_usage` infers odd result types for
      generic calls and names parameters p, p2; `replace_text` does not
      add the imports new code needs (hit again in #128). *Done for
      imports (#173): in a Go file, `replace_text` imports the packages
      its new text names, as the other edits do. And for
      `create_from_usage` (#176): a parameter is named after its type when
      its argument has no name (`configs`, `m`, `ch`, `fn`, `err`, `id`
      for `ids[0]`), and a generic callee whose type arguments could not
      be inferred tells nothing about a result, so it is no longer
      written as its type parameter (`S`).*
    - **One worked example** of a whole batch in the instructions.
      *Done (#174): a test first, the real function replacing its stub,
      and a rename, in one batch; a test runs it as written.*
    - **Measure** each with the A/B harness's feature and chain tasks,
      with more runs per task (noise is 10-25% at 2-3 runs), and count
      the waste each fix targets per run (turns refused, turns spent on
      ToolSearch alone), not only the totals.
16. **A/B on other people's code.** The harness's tasks are all in this
    repository, which the tools were built on and for. Real feature PRs of
    large third-party modules test whether the tools help where nobody
    shaped the code for them. `eval-tasks/third-party/` lists 172 feature
    PRs merged after July 1, 2026 in cli/cli, Hugo, lazygit, OpenTofu and
    Gitea, and verifies 74: 42 have tests that check the feature's
    behavior against the code before the PR (black-box), 35 of them
    enough to be the hidden check (recommended), 16 of those refactoring
    on the way. In order:
    - **Prompts** for the recommended candidates, from their issues,
      written as requests; then `ab.json` tasks with their hidden tests.
      *First ten done* (`eval-tasks/third-party/ab.json`, two per
      repository, four that refactored on the way), each validated: it
      fails at the PR's parent and passes at the PR. Two candidates were
      set aside whose hidden tests pinned internal design or checked too
      little; the other 23 recommended still need their tests read and a
      prompt.
    - **A first run** on a few per repository, manual against hybrid, to
      see whether a task needs more than the prompt (setup, a running
      service) before running all of them, with at least five runs per
      task and arm. *Done (`tp-first`, `docs/cost-history.md` section
      6): no task needed setup; one prompt was wrong and is fixed. The
      hybrid arm never refactored, so the tools were not exercised.*
    - **Get the tools used on this code.** With "refactor only when
      needed", the hybrid arm judged no refactoring needed in any of the
      ten, the four that refactored on the way included. Try the
      refactor-first CLAUDE.md, or the mcp arm, on the four, before
      running all tasks at five runs each. *Mcp arm done
      (`tp-mcp-refactor-first`): 4/4 at about 1.35x by hand, every edit
      through the tools, but one rename and no structural refactoring.*
    - **Load the module before the first call.** The first `inspect` of
      a run waited 296 s for Hugo to load (27-37 s elsewhere). The
      server could start loading when it starts, so the model's
      reading overlaps it. *Done: refactor-mcp calls `Preload` at
      start; calls wait for it as they waited for the first load.
      Measured: no shorter on Hugo (328 s), since the model calls
      `inspect` at once. Then: a fresh copy of Hugo loaded in 288 s
      however warm the cache, as Go's build cache keys a package by
      its directory; with `-trimpath` in the load's build flags, the
      second copy loads in 2.2 s (the first fills the cache, 302 s).
      Done; a cross-package rename finds the same 24 occurrences.*
    - **Stubs for types.** A test written first that uses a type not
      declared yet (`stackEntry` in lazygit) is refused; stubs are
      declared only for functions and methods. *Done: a type made with
      keyed literals gets a struct with those fields; and, from the same
      run, a function or method passed as a value gets the signature its
      place needs, a stub's types use the names the file imports their
      packages as (Gitea's `packages_model`), and a name with no stub
      because it is not called is now said so.*
17. **Follow-ups from the code review** (`docs/code-review-2026-10-20.md`).
    Its high and medium items are fixed (#136-#154); these were found on
    the way, or are its low items:
    - **A false refusal in `introduce_parameter`:** an expression whose
      function literal names its own parameters
      (`strings.FieldsFunc(s, func(r rune) bool { return r == 32 })`) is
      refused with "uses r, declared in the body of f"; the literal's
      parameters are its own, not the body's. *Done (#156): names the
      expression declares itself go with it, in both the body check and
      the call-site check.*
    - **`extract_struct` reaching reflection:** its own check
      (`checkNotPassedAsInterface`) finds the type passed as an interface
      only by itself or through a pointer, so `fmt.Printf("%+v", ms)` with
      `ms []mover`, or a struct holding a `mover` field, is accepted and
      the printed shape changes. `analysis.PassedAsInterface` and `Holds`
      (#151) follow slices, maps, pointers and fields; switch to them.
      *Done (#157).*
    - **One walk over dependencies' interfaces:** `rename`
      (`interfacesWithMethod`), `remove_dead_code`
      (`dependencyInterface`) and the signature changes (`visible`) each
      walk every package-level interface of every dependency. One
      function in `analysis` would do. *Done (#158): `analysis.Dependencies`
      and `PackageInterfaces`. merge_duplicate_function keeps its own
      `dependencies`, which finds import cycles.*
    - **One list of functions that call a closure during the call:**
      `analysis.go`'s `syncCallees` and `effects.go`'s `syncCalls` have
      drifted apart: 22 of the first's are not in the second, among them
      the `bytes` functions, `filepath.Walk`, `slices.DeleteFunc` and
      `sort.Search`, so the effect summaries treat them as calls of a
      function value. *Done (#159): one table, `syncCalls`, says for each
      what it does with its other arguments (reads or changes the first,
      reads all, walks the file system, panics on an empty slice).*
    - **The CLI matches the server:** `cmd/refactor` has no `batch`, and
      its edits get no stubs for the functions they call (the server
      passes `Declare`). Sessions in a cloud clone use the CLI, since the
      MCP runs on the user's own checkout, so they order steps by hand.
      *Done (#160): `refactor batch '{"steps": [...]}'` takes the batch
      tool's arguments, and a single edit is retried with stubs as the
      server does (`refactorings.RunStep`, `ParseBatch`, `Declarer`, which
      both now use).*
    - **The review's low items, 19-25:** a crash in `introduce_parameter`
      on a variadic parameter with no variadic arguments; `create_file`
      gofmt-ing any file that parses as Go; `replace_expression` not
      removing an import it leaves unused; the A/B harness counting timed
      out runs at cost 0, and maybe letting the mcp arm write with
      `cat >`; a line gofmt realigned, and argument aliases, in batch line
      translation; and smaller gaps (`rename` and `Test` functions,
      `make(n)` able to panic in `move_statement`, loop variables before
      Go 1.22). Each is described, with where, in the review.
      *Done so far: 19 (#161, introduce_parameter refuses an expression
      using the variadic parameter unless the call passes a slice with
      `...`), 20 (#162, `create_file` formats only `.go` files), 21
      (#163, `replace_expression` removes an import only the replaced
      expression used), 22 (#164, the A/B summary leaves runs cut off
      before their result out of the means and lists them; `Bash(cat:*)`
      does not let `cat > file` through, checked with `claude -p`), 23
      (#165, a line gofmt only realigned keeps its place in batch line
      translation), 24 (#166, a batch takes argument aliases such as
      `line_no` before translating lines), 25 (#167: `rename` and test
      functions, `make` with a size that may be negative, loop variables
      before Go 1.22). All the low items are done.*
    - **One check for terminating statements:** `extract_function`
      duplicates the logic of `analysis/flow.go` (the review's code
      quality note). *Done (#170): `analysis.TerminatingList` takes the
      statement that becomes the call, and `extract_function` uses it.*
18. **Take type reasoning off the model.** The model should say what it
    wants and the tools work out what compiles. `docs/type-errors.md`
    counts the 66 type-check refusals in all A/B runs so far by kind, and
    ranks what the engine could take over, in order:
    - **Stubs for fields** used before they exist (`x.F`, `T{F: v}`),
      typed by their use, and `struct{}` for a type named only as a
      field's or parameter's type. *Done; a name one letter or one swap
      from a field or method the type has is refused as a likely
      misspelling.*
    - **Imports in every edit:** `replace_text` adds those new code needs,
      and every edit removes those it leaves unused. *Done: replace_text
      adds them (#173), and when an edit leaves code that does not
      type-check, the engine runs `fix_imports` (not a tool of its own)
      on the files with import errors before it declares stubs: it adds
      the imports names unambiguously need and removes those nothing
      uses.*
    - **Signature and type changes that carry their consequences.** The
      user chose to extend the parameter tools first (2026-10-20):
      - **Interface families.** *Done:* add_parameter, remove_parameter,
        move_parameter and change_parameter_type change an interface
        method of the module with every method of the module's types
        that implements it (and other module interfaces those satisfy
        through it), and every call through any of them; name any
        member. Interfaces outside the module are still checked.
      - **Results:** a tool to change a function's results, with every
        call. *Done: add_result adds one, last, with the value every
        return gives it (callers `v, _ := f()`), for families too.
        Removing or retyping a result is still to do.*
      - **Edits that change a signature by text** are refused with the
        tool to use. *Done: such an edit still applies if everything
        compiles; when it does not, the refusal notes which signature it
        changed and the tools that change one with every call.*
      - **`change_field_type`,** adjusting uses only where safe and
        refusing each other use by name. *Done: to a type with the same
        underlying type, a wider number type or an interface the old one
        implements; writes are converted, reads converted back where the
        type matters, and a use that cannot follow (its address taken,
        a read needing the old type under an interface) is listed by
        position in the refusal. T to *T is not among them: every read
        could then dereference nil.*
      - **A parameter's type changed through an alias**, the way the
        user changes one by hand (2026-10-20): declare an alias of the
        old type (`type renderArg = string`, add_declaration), write the
        parameter with it, change what the alias is, then fix what no
        longer fits. Hugo's `layout ...string` to `args ...any` goes so.
        - **Write a parameter with an identical type.** *Done (#193):
          change_parameter_type with an alias of the parameter's type
          changes only the declarations, every member of an interface
          family's, variadic parameters too.*
        - **`change_type`, an edit:** retarget an alias (or change a
          defined type's underlying type), and list where values come
          in and where they go out needing the old type, spread calls
          (`f(xs...)`) among them, for the model to fix in the same
          batch. An edit, not a refactoring, so that a batch with check
          "end" checks it with those fixes (the user's decision).
          *Done (#194): change_type changes an alias's target or a
          defined type's underlying type, with the imports that follow;
          in a batch with check "end" its result lists the type errors
          it left (`refactor.BreakageNoter`). The list is the compiler's
          errors, not a classification of uses; that can come if the
          errors prove hard to act on.*
      - **Later: one `change_signature`** taking the new signature, which
        works out the parameters added, removed, moved and retyped and
        the results, and needs a value at each call for a new one.
        More general than the separate tools; harder to keep safe.
    - **Measured on tasks that call for it** (`eval-tasks/mechanisms.json`,
      cost-history section 7): the help fires and removes the type-check
      refusals it targets, but totals do not move; those refusals are a
      few turns of many. Left from those runs:
      - **A stub for a type used only in conversions:** `UserID("u1")` in
        a test written first, or `map[UserID]bool`, gets none; the
        conversion's argument gives the underlying type (`type UserID
        string`).
    - **Mechanical repairs with a note:** `:=` to `=`, exporting a helper
      another package calls, "already declared at ..." with how to
      replace it. *Undecided (2026-10-20). The user's direction: rather
      than repairing an assignment the model wrote, let it ask for one
      (assign these values to these names) and have the engine write the
      right form: `:=`, `=`, `var`, `_` for results it does not want.*
    - **Refactorings toward a test written first:** run while the only
      errors are in tests it fixes. *Done: a refactoring runs on code
      whose only errors are in `_test.go` files, and like a fix must
      fix some and add none. Stubs, declared for what a test calls, can
      get there first; then the refactoring meets the stub.*
    - **Stubs declared lazily** (the user's decision, 2026-10-20). In a
      batch checked at its end, stubs and imports were added only after
      the last step, so a refactoring after a test written first met
      code that did not type-check and was refused. *Done (#197): just
      before a refactoring, the engine adds the imports and stubs the
      earlier edits need, and later steps' lines follow what it added;
      the end still adds whatever is left.*
    - **Checkpoints in a batch** (the user's decision, 2026-10-20): a
      step that type-checks, and optionally runs tests, at that point,
      so a batch refused later keeps what passed before it. *Done
      (#198): a step `{"tool": "check"}`, with `run_tests` and
      `packages` as the check tool takes them, first adds the stubs and
      imports earlier edits need (in a batch checked at its end). If a
      later step is refused, or the batch's end does not type-check, the
      steps through the last checkpoint that passed stay applied as one
      change (one undo), and the result says so; a batch with a kept
      part is not run again with check "end".*
    - **One name that cannot be typed blocked every stub of its file.**
      Found writing #198's test: `NewCheckpoint()`'s results could not
      be told, so the fields the test also used got no stubs. *Done
      (#199): the engine drops such a name, as it did a name not called,
      declares the rest, and says why the dropped one has none; and a
      call used as an element of a composite literal (a keyed struct
      field, a map value, a slice or array element) takes that
      element's type as its result.*
    - **replace_text refuses a selector on a name a later step
      declares** (`untypeable.FindStringSubmatch` before the step adding
      `var untypeable`): it takes the name for a package to import, and
      refuses when there is none, though the batch is checked at its
      end. Leave such a name alone instead, for the type check to judge.
      *Done: a name no package has is left to the type check
      (`gocode.NoPackageError`); an ambiguous one still asks for
      `imports`.*
    - **From the friction log's review (2026-10-20)**, by hits since the
      tools covered most edits:
      - **Move and delete files** (5 hits, `git mv` and `git rm` of
        golden cases whose expected outcome changed). *Done (#203):
        `move_file` and `delete_file`, for a file or a directory with
        everything in it; a moved file is written byte for byte
        (`edit.FileEdit.Verbatim`), and a directory a delete leaves
        empty goes with it (undo puts it back).*
      - **Replace or delete declarations by name, without restating
        their text** (6 hits of a long `old` in replace_text: a 2.6K
        literal, a 124-line range of methods), and **remove several
        declarations at once** (3 hits: functions that only call each
        other, a list and its last use). *Done (#204):
        `replace_declaration` replaces a whole top-level declaration (a
        function with its signature, a method, a type, a variable such as
        a table, one spec of a group) by name, keeping the old doc comment
        unless the new source has one; and `remove_dead_code` takes
        `also`, more declarations to remove with the first, uses among
        them not counting (a cycle of helpers, a type and its methods).
        A literal inside a function, like the 2.6K batch definition,
        still needs replace_text or replace_statements.*
      - **`refactor batch` reads its JSON from a file or stdin** (14
        hits of batches built in the shell, one broken by an unquoted
        heredoc; cloud sessions use the CLI). *Done (#205): an only
        argument `@path` is read from the file, and `-` from stdin, for
        a batch or a single step.*
    - **Measure** by counting these refusals per run, by kind.
19. **Work algebraically** (the user's thesis, `docs/algebraic.md`): code
    as a structure reached by type-safe operations, with small edits at
    the leaves, so the model thinks above keywords and syntax. In order:
    - **Measure it.** *Done:* `refactor-tokens` separates the code the
      model wrote from what the tools derived (the derived share). A
      fresh model's mcp runs derive about a tenth of what changes.
    - **Collect derivations** from the user, by posing small problems,
      as recipes: the shape before, the operations, the shape after.
      *Five done* (`docs/derivations.md`), each run with the tools.
    - **Catalog the missing operations**: what the code the model writes
      through edits does, which no operation does yet. *From the five:
      14 gaps (A-N in `docs/derivations.md`); first, fold (I, with D
      and J): replace occurrences of an expression or call with a call
      that means the same, checked by inlining back.*
    - **Fold.** *Stage 1 done:* `fold` replaces every match of a
      function's one-return body (its parameters are typed holes) with a
      call, a method's receiver bound from the match or from the method
      the match is in, one line at a time with `line`. *Stage 2 done:* a
      struct parameter the body uses only by its fields makes each field
      a hole, and the call passes a literal (`Send(p, us, m)` to
      `Send2(p, Batch{Users: us, Message: m})`, problem 5), also across
      packages. *Stage 3 done:* a function without results whose body
      is statements folds runs of the same statements in any block (its
      locals renamed freely), so extract_function and then fold replaces
      duplicates (problem 1). Matching is by syntax tree: `a+b+c` does
      not match `a+(b+c)`. Then parameterize (J). *Literals through a
      setter (K) done:* a function that sets fields of a struct
      parameter and returns it folds the literals that give those
      fields (`User{ID: id, Email: v}` to `withEmail(User{ID: id}, v)`),
      for any struct, field and value. Next: **pointer setters**
      (`func (u *User) SetEmail(v string)`, asked for by the user), for
      a literal that initializes a variable only (`u := User{...}` to
      `u := User{...}` and `u.SetEmail(v)`): elsewhere the literal would
      have to be hoisted out of its expression into a new variable, which
      changes evaluation order and is impossible in a condition, a loop's
      header or a package-level initializer.
    - **Parameterize (J).** *Done for constants:* `parameterize` makes
      functions that are the same but for constants calls of one new
      function that takes them (`logEmail`, `logSMS` to `logTo(to,
      format)`); `inline_function` with `all_calls` then retires them.
      Next: functions that differ in an expression, such as problem 3's
      `u.Email` and `u.Phone`, by a switch on a new parameter.
    - **Teach and measure:** recipes and planning a change as a
      derivation, against the current instructions, on tasks with a
      derivation as the reference solution. *First run done*
      (cost-history section 8): the guidance made the model use the
      recipes where they fit (37% of steps writing nothing, against
      22%), at the same cost; on the seeded module manual edits are
      small either way. *On code with many uses* (notify-large): the
      guided arm cost 14% less, wrote 39% fewer lines by hand and 31%
      of its steps wrote nothing (9% without), the gap on the tasks
      where a derivation replaces rewriting each use (fit, Batch). Next:
      the gaps those runs hit (pointer alias A for the shared record,
      *done* as `extract_variable` with `pointer`; fold through a
      pointer parameter, O; for Contacts, fold literals through a setter,
      K, then remove the fields, L). *Six runs per task done*: 4% less
      cost, 31% fewer lines by hand, 55% of steps writing nothing
      against 11% (Contacts left out); no run used `pointer`, since
      none tried to extract `e.Sent`. Next: K (*done*) and L, then Contacts
      again, and a recipe or gap O so the shared record is derived.
      *L done:* `remove_dead_code` of `Type.Field`; its `exported`
      option also does N. *Contacts rerun* (cost-history section 8):
      same cost; the guided arm folded reads and setters, but the
      setter fold refused literals with an elided type in another
      package, the test literals; now qualified. *Rerun done:* three
      of six guided runs derived the whole change (reads and all
      literals folded, representation changed in four functions),
      guided $0.175 against base $0.208 a run. Next: gap O or a
      recipe so the shared record is derived.

20. **Loops and containers** (asked for by the user, 2026-10-20): the
    structural moves on control flow that derivations need and no tool
    does, so the model writes `for` and `if` by hand. Each is a
    refactoring where it can be proved not to change behavior, and an
    edit otherwise.
    - **Convert between kinds of loop.** A three-clause `for i := 0; i <
      n; i++` to `for i := range n` (Go 1.22) or `for i := range s`, and
      back; `range` by index to `range` by value (`s[i]` uses become `v`)
      and back; a condition loop `for c { }` to `for { if !c { break } }`
      and back; a loop that appends, sums or finds to the `slices` or
      `maps` call that does it, and back; a loop over a function's
      results to range-over-func (Go 1.23). What each must check: the
      loop variable is not assigned in the body; the bound or the slice
      is evaluated once in a `range` but every iteration in a condition
      (it must be stable and not written in the body); per-iteration
      variables before Go 1.22 (`info.FileVersions`, as in
      `sharedLoopVar`); `break`, `continue`, `goto` and labels keep
      their targets; the post statement and condition have no effects
      the new form reorders; a value copy where the body wrote `s[i]`.
    - **Wrap statements in a container** (gap C in
      `docs/derivations.md`; `introduce_block`, `introduce_if` and
      `introduce_loop` in `docs/edits.md`): make a range of statements
      the body of a block, an `if`, a loop, a closure called at once, or
      a `switch` case. A refactoring for `{ }` when nothing declared is
      used after it, `if true`, `for range 1`, and `func() { ... }()`
      without `return`, `defer`, `break` or `continue` that would change
      targets; an edit with a real condition or clause, which the model
      then gives. The inverse, unwrapping a body into its parent, is the
      same checks backward, and with `move_statement_in`/`_out`
      (`docs/edits.md`) gives "wrap in an if and move the statements
      in", one of the user's derivations. *Done* as an edit, for any
      container: replace_statements' `wrap` keeps the statements and
      puts them, re-indented, at the line `$body` of the code it gives
      (`if verbose {\n$body\n}`, `once(func() {\n$body\n})`, by lines,
      `case` or `function`). It refuses statements that declare a name
      used after them, and notes a return, defer, break or continue
      whose meaning the container changes. Not yet: the refactoring
      forms that prove nothing changes (`{ }`, `if true`, `for range
      1`, a closure called at once), and unwrapping.
    - **A branch of an if** (the user, 2026-10-25): add_case adds
      `else if` and `else` branches to an if, as it adds cases to a
      switch: at the line of the if, at the end of its chain (before
      its else); at the line of a branch, before or after it. *Done.*
      Not yet: a `branch` address (`"branch": "p.Height > 10"`) for
      extract_function and replace_statements, as `case` is.

21. **Experience for a fresh model** (asked for by the user, 2026-10-21):
    refactoring by hand, the user tries several approaches before one
    works, and each try adds to a library for later. A fresh model has
    neither the habit nor the library. Short-term cost may rise; the
    measure is whether later tasks get cheaper. Context is costly (about
    39K tokens a request) and longer instructions did not help, so what
    is learned must reach the model where it is stuck: in a refusal, or
    a recipe it is pointed to. In order:
    - **Lessons in refusals.** Each known dead end becomes a line in the
      refusal the model meets there, naming the way through: e.g.
      `change_field_type` asked to make two fields one map points to
      getters, fold, then the representation. Evidence: two runs in
      `contacts-q-guided` went textual at that refusal.
    - **`try`: several candidates at once.** Run two or more candidate
      batches on scratch copies of the same code, apply none, and report
      for each whether it type-checks, whether the tests pass, and the
      size of the manual edits left; the model applies the one it
      picks. One turn in place of apply, undo and retry per approach.
    - **A recipe library in the repository** (with item 11's saved
      batches with parameters as the format): derivations that worked,
      the shape before, the operations, the shape after, with
      parameters; and what did not work, and why. Taken from runs
      (`refactor-tokens` already separates derived steps) and from this
      project's own PRs, each of which adds what it used, as the
      friction log collects hand edits. Served as a short index or a
      skill loaded when it fits, not in the instructions.
    - **Exploration runs.** A/B runs told to try two or three approaches
      and say which worked and why, distilled into the library; then a
      "with library" arm on later tasks, counting the exploration as a
      one-time cost. *Set up:* `docs/learning-transfer.md`, with a
      second module of the same five shapes (`derivations-billing.json`)
      for the later tasks. Found by the runs, to fix: `fold`'s
      refusal names no nearest miss; `introduce_parameter`'s refusal
      of `&e.Sent` after a call gives the wrong reason (the nil
      receiver's panic moves, which the target itself does: a
      decision for the user, shared by gap O); `remove_dead_code`,
      `rename` and `extract_struct` took `sort.Slice`/`SliceStable` of
      a struct slice for reflection on its fields (*Done:* those
      arguments are not looked at); `inline_variable` refuses to inline a call of a function
      without side effects, such as a getter, used twice.
    - **Search in the engine** (the long shot): given the target shape
      (a struct's new declaration, a function's new signature), search
      sequences of operations that reach it and return the shortest.
22. **Exported code** (the user, 2026-10-21): refactorings change
    exported code as gopls does, update this module, and note that the
    module's API changed, so the model can mark a breaking change (a
    new major version). Not API: a main package, or one under an
    internal directory. Refusals for exported code met in the
    exploration runs: `extract_struct` 12, `remove_dead_code` 4,
    `function_to_method` 1. In order, one PR each:
    - **The note, and `remove_dead_code`.** *Done:* `refactor.APINote`,
      `analysis.PublicAPI`; rename, the signature tools,
      `change_field_type` and `move_declaration` use the note;
      `remove_dead_code` no longer refuses exported code (its
      `exported` option is no longer needed).
    - `merge_duplicate_function` and `inline_function` delete an
      exported function (inline_function must then look for uses in
      every package, not only its own). *Done:* and for a method,
      interfaces in every package (`analysis.InterfaceWithMethod`).
    - `extract_struct` of an exported struct. *Done:* the note adds
      that other modules' literals cannot name the moved fields.
      Literals in other packages of this module wrote the new type
      unqualified (found by explorer 7, three Contacts runs); *fixed:*
      qualified, and an unexported new type is refused there.
    - Next, from explorer 7 on all five tasks: `add_parameter` and
      `introduce_parameter` write one value for every call, so a value
      of a type of the callee's package cannot be both `notify.Batch`
      in other packages and `Batch` in its own (batch failed 2 of 3);
      write it qualified per call. *Done:* each call's value names the
      function's package as its file imports it (importing it if
      needed), and drops the qualifier inside that package. And a `move_declaration` that left
      code that did not compile (batch run 3). *Fixed:* its cycle
      check missed the destination's test files (`campaigns_test.go`
      imports `notify`, which would import `campaigns`).
    - The method guards (`extract_function` from a method,
      `function_to_method`, `method_to_function`): in place of an
      unexported name, refuse only a method that changes which
      interfaces of the module and its dependencies the type
      implements, as rename checks; note it for other modules.
      *Done:* `analysis.MethodInterface` (rename's list of methods
      called at run time, now `analysis.DynamicMethods`, and the
      package-level interfaces of the module and its dependencies
      whose other methods the type has); `function_to_method` takes
      calls in other packages when the name is exported.

23. **Near-duplicates by `fold`** (the user, 2026-10-21): in every
    learner run on billing (24 of 24), the near-duplicate task was
    written by hand: the explorers' route (extract the append,
    `method_to_function`, `introduce_parameter`,
    `merge_duplicate_function`) took 26 to 60 calls. The short route,
    the user's "delegate" move, is to declare the shared function once
    and `fold` it into the copies; on billing it is refused for four
    reasons. One PR each:
    - **A nil dereference may move past calls that return** (the
      user's decision). `book(&w.Charged, ...)` takes the address
      before the log line it replaces runs, so a nil `w` panics
      without logging. *Done:* `&p.f` counts as reading only `p`
      (`analysis.Addressed`), and `introduce_parameter`,
      `add_parameter` and the statement moves let a panic that is
      only a nil dereference move past side effects, unless they
      might not return: `os.Exit`, `log.Fatal` and the like, or a
      call of a function value or interface method (`Summary.Unsafe`
      now says so). *Fixed after:* channel operations (send,
      receive, select, range over a channel or a function) count as
      might not finish too; they had let a nil dereference move ahead
      of a receive that could block forever.
    - `fold`: a pattern's `*p`, for a pointer parameter `p`, matches
      any addressable expression `e`, passing `&e`. *Done:* a
      variable or field; left when `e` goes through a package
      variable, or through a pointer that could be nil while code
      before `e` might not return (`log.Fatal`) or could panic
      otherwise.
    - `fold`: string `+` is associative, so a hole matches a run of
      a concatenation (`a.Bank+": "+inv.Ref+fmt.Sprintf(...)`).
      *Done:* a `string` parameter matches a run of operands however
      they are grouped. Whether an argument may be evaluated before the
      statements is now judged by what the calls in it do
      (`analysis.Effects`), all of them, not only the outermost: so
      `fmt.Sprintf` in a run is fine, and `x+g()`, with `g` having
      effects, is no longer folded. Billing's bank copy folds too:
      three of four in one call.
    - `fold`: in a printf-style call, a `%s` hole matches constant
      text of the format that holds no `%` (`"wallet charge to %s"`).
      *Done:* `%s` or `%v` with a `string` parameter, in a call of a
      function named `...f` whose last parameters are a format string
      and `...any`. With this and the pointer hole, billing's wallet
      and voucher copies fold into `book`.
    - Found on the way: the card copy uses `last(a.Card, 4)` twice;
      `fold` leaves an argument that could panic (`last` slices) when
      the body uses it twice, though it is pure and the first thing
      the statements evaluate, so evaluating it once first is the
      same. *Done:* an argument that could panic is evaluated once
      before the statements when it has no side effects, reads
      nothing other code can change if used more than once, nothing
      that matters comes before its first use, and no argument
      evaluated before it in the call could panic. The card copy is
      still left: `&c.Charged` comes before it in the call, so a nil
      `c` would panic before `last` could; allowing that is the next
      part, an extension of the nil-dereference decision.
    - **A nil dereference may also come before another possible
      panic** (an extension of the first part, in its own PR so it
      can be dropped). In `fold`, `&x` may be evaluated ahead of code
      that could panic another way, and ahead of an argument that
      could: a crash either way, only with another message. Still
      not ahead of code that might not finish or return
      (`Summary.Stops`: loops, channel operations, `os.Exit`, unknown
      code). *Done:* billing's near-duplicate task is now
      `add_declaration` of `book` and one `fold`, all four copies,
      and passes its hidden test.
    - *Measured* (`learn2-*`, `docs/learning-transfer.md`): learners
      folded `book` in 6/6 runs under the user's moves and the
      combined set, 4/6 baseline, 3/6 learned (whose old recipe now
      costs turns); lines by hand on the task halved, cost unchanged.
    - Found on the way: an argument that reads memory other code can
      change (`c.count` through a pointer) was evaluated before the
      code it replaced, past calls that could change it:
      `bump(c); fmt.Println(c.count)` folded into `rec(c, c.count)`.
      *Fixed:* such an argument is left when code before its last use
      has side effects or assigns.
    - *Measured on a larger module* (`freight-xl`, 5-10 times the
      uses): the user's moves 25% cheaper than the baseline, 43% fewer
      lines by hand (`docs/learning-transfer.md`).
    - Found on the way, in `cmd/refactor-ab`: a run whose hidden tests
      could not be copied in ("copying the tests in" error) was
      counted as passed; it should count as failed or unfinished.
    - Open (2026-10-23, `docs/naming-the-level.md`): `fold` does not
      fold a copy whose condition is written the other way round
      (`if o.Status != P { list = append(list, o) }` onto
      `if o.Status == P { continue }; list = append(list, o)`), so
      every run replaced such copies by an edit.
24. **Does deriving let the model read less?** (the user, 2026-10-22):
    the user's hypothesis that a model thinking algebraically needs to
    read much less, perhaps only a call tree's names, and that this goes
    with deriving on shapes no move names. Every learner run so far was
    on the five shapes the moves were written for. A plan in three
    steps, `docs/reading-and-deriving.md`: reads in the freight runs (no
    new runs); a one-turn probe, planning from a call tree, the skeleton
    or the whole module, scored by running the derivation; a set C of
    new shapes on a large module, with an arm that teaches the method
    and names no shapes.
    - Step 1, reads in the freight runs. *Done:* both arms read alike
      and little; about a quarter of input is "where is X used"
      searches, which a derivation does not need
      (`eval-tasks/learning/reads.py`).
    - Found by the step 2 probe: `rename` (and every tool that finds
      a local by `function`) refused `function` set to the name of the
      function itself (8 answers: "Dispatch2 declares no Dispatch2").
      *Fixed:* when the function declares no local of that name, it is
      the function (or method: Run or T.Run in T.Run).
    - Step 2, the call tree: `cmd/calltree` prints each package's types
      and each function's signature, doc line and the module functions
      it calls (through an interface as the interface's method), with
      no bodies. *Done* (13.4K characters for freight-xl, against
      22.3K for its code).
    - Step 2, the probe. *Done* (`eval-tasks/learning/probe.py`, 48
      answers, $2.73): from the call tree alone the answers named the
      functions to change (precision 1.0, recall 0.97) and planned the
      reference's derivations; the batches failed on the tools'
      interface. Found: `rename` with `function` equal to the name it
      renames is refused (8 answers); `fold` leaves imports its folds
      make unused (a bug); field removal blocked by test files no view
      shows.
    - `fold` removes the imports its folds leave unused (found by the
      probe: a `dispatch` in a file of its own, ten carriers folded,
      left `log` unused). *Fixed*; that answer now applies and passes.
    - Step 3, set C: `lending-xl` (`eval-tasks/learning/gen_lending.py`,
      748 lines; `eval-tasks/derivations-lending.json`), five tasks of
      shapes no move, lesson or tool example names (phases, kinds,
      policy, address, dryrun), hidden tests, a reference for each made
      by the tools (`eval-tasks/learning/refs/lending-*.json`), and the
      algebraic arm (`eval-tasks/claude-md-algebraic`). *Built*; the runs
      are next. Found while making the references: no operation turns a
      switch on a kind into methods, or makes an embedded field a named
      one (73 replacements by hand); `remove_dead_code` takes
      `var policy = DefaultPolicy()`'s initializer as having side
      effects though `DefaultPolicy` only returns a literal.
    - Step 3, the runs. *Done* (60 runs, $10.68, all passed): the
      algebraic arm derived a little more where an operation fits
      (policy: 77 lines by hand against 182, 69% derived against 43%)
      at the same cost; where none fits (kinds, phases) no arm
      derived; reading was alike in every arm. Next, the gaps the runs
      name: `rename` of an embedded field (10 of 12 address runs tried
      it), and an operation that turns a switch on a kind into
      methods.
    - **`rename` of an embedded field.** *Done*: it becomes a named
      field, and every selector that reached through it names it.
      The address task is now `extract_struct`, then `rename` of
      `Member.Address` to `Home` (73 selectors, the hidden tests
      pass), where the reference needed five text replacements.
    - **A switch on a kind into methods** (the user chose two
      general operations, 2026-10-22): `type_switch_to_method` turns
      a type switch into a method of each type it switches on.
      *Done.* Then the user proposed a derivation by kinds, one at a
      time (expand, migrate, contract), which replaced "a type code
      into types" as the next work: widen the field to `any`; add the
      interface and a guard before the switch that calls it for a
      value that implements it (nothing does yet); for each kind,
      make its type with the case's body as the method, replace its
      constant where it flows into the field, and delete the case,
      now dead; at the end narrow the field to the interface.
      Not for a switch with fallthrough.
      - Widening: `change_field_type` to `any` keeps a switch on
        the field, `==` with it and passing it as an interface, and
        converts an untyped constant compared with or written to it
        when its default type is not the old one (`kind("dvd")`).
        *Done* (lending-xl's `Item.Kind`: 17 uses).
      - Preparing the switch: `prepare_switch_for_types` makes a
        value that implements the interface run its method, and the
        switch the rest: it declares the interface (or adds the
        method to it) and puts before the switch
        `if k, ok := it.Kind.(Kind); ok { return k.Days() }`.
        *Done* (lending-xl: widening and the five guards in one
        batch, its tests pass).
      - One kind's migration (the user, 2026-10-22: alternatives to a
        tool first). With existing refactorings each case body becomes
        a method of the kind's type: `extract_function` of the body,
        `add_parameter` of `b Book` with `Book{}`, `function_to_method`,
        `rename` to the interface method (tried on lending-xl's
        `LoanDays`). What is not a refactoring is sending the kind's
        values through the type and deleting the dead case; neither can
        be proved where kinds are read at run time (lending-xl's
        `Import`), by a tool or not. So the plan is a recipe of general
        operations, not `move_case_to_type`: `inspect` `uses` to see
        how each use treats the field (*Done*), splitting a shared
        clause (`case "book", "dvd":`; *Done*, by
        `prepare_switch_for_types`, whose intent it is: each value's
        case can then move alone; no new tool or option), rename to
        the interface's method when that makes the type implement it
        (*Done*: refused only where a value of the type may become an
        interface value, `analysis.BecomesInterface`; the first proof in
        the engine of item 25), the recipe in
        `docs/derivations.md` run on lending-xl with the CLI (*Done*
        for one kind: section 6, 27 derived steps, by hand one type,
        a 6-line function, 3 expressions and 5 deletions), and a
        tool only if a fresh model fails the recipe.
      - Narrowing: `change_field_type` from `any` to the interface
        when every value written to the field is one already; a
        switch on it then loses the cases no such value matches,
        a proof that deletes them all at the end. A write that need
        not be one (`kindOf(...)` returning `any`), a constant
        written, an `==` with a constant, a case reached by
        fallthrough or a generic call is refused, each listed, so on
        lending-xl after `"book"` the refusal lists what is left for
        `"dvd"` and `"atlas"` (9 uses). *Done.* With `values`
        (`{"book": "Book{}"}`), narrowing writes each value where the
        field is given or compared with its constant, qualified in
        other packages, so a plan need not see those uses: the
        refusal listing them cost one of two repairs in 13 of 16
        kinds attempts (cost-history 19), and the kinds reference
        drops its eight replace_text steps for it. An edit, said in a
        note; values that call a function are refused. *Done.* A
        value not of the new type is refused with why: the method its
        type lacks (move the cases first), or has with another
        signature (`Label(title string)` where `Kind` has `Label(it
        Item)`), which stopped three plans without reads. *Done.*
      - Deleting one kind's dead case before the end:
        `replace_statements` with empty content over whole case
        clauses deletes them, refused when the case before falls
        through into them. An edit (whether the case is dead is the
        argument from `uses`), an option of an existing tool, no new
        tool. *Done.* Emptying or deleting a case of a switch on a
        field (by lines or by `case`) is refused while some write may
        still give the field a value equal to it, listing those writes
        (`analysis.FieldWrites`): on lending-xl, Haiku emptied the
        string cases while `Import` still wrote strings, and the
        failing tests that came back named the tests' literals, not
        Import. *Done.*
      - The whole task end to end with the CLI: 74 derived steps,
        about 30 lines by hand, hidden test passing
        (`docs/derivations.md` section 6). Gaps it met: parentheses
        for a composite literal in an `if` (`replace_expression`;
        *Done*: it uses `analysis.InControlHeader`, as
        `inline_variable` did),
        `introduce_parameter` through an interface method (*Done*:
        each method of the interface's types gets the parameter for
        the same expression of its own parameters, found with
        `pattern`, and `function` addresses the expression by name;
        so `Label(it Item)` becomes the prompt's `Label(title string)`
        in two steps, introduce_parameter of `it.Title` in
        `Book.Label` and remove_parameter of `it`, and the kinds
        reference ends with them), narrowing leaving empty switches,
        a case equal to the default.
      - A fresh model on the recipe: `eval-tasks/claude-md-kind-recipe`
        (the algebraic arm and the recipe, with shapes for its
        example), `lending-kinds`, 4 runs, against the algebraic arm
        of step 3 (375 lines by hand over 4 runs, 19% derived). Run
        (cost-history section 10): with or without the recipe, 3 to
        4 runs of 4 widened, prepared, wrote the types and narrowed;
        250-281 lines by hand, 39-48% derived, $0.25 a run against
        $0.20. No tool is needed for a kind's migration. Next, the
        waste it showed: parentheses for a composite literal inside
        a comparison in an `if` (6 refusals in 8 runs, 5 of them
        `replace_text`; *Done*: both tools bracket each literal an
        `if`, `for` or `switch` header would take for its block,
        `analysis.BareComposites`), and a
        refusal of `replace_text` on an interface method's signature
        that points to `add_parameter` on the interface method
        (*Done*: the note on an edit that changes one signature's
        parameters now gives the steps, add_parameter, the edits,
        remove_parameter; before, a list of tools the runs ignored).
        Rerun (cost-history 10): parse refusals 6 to 0; the steps
        did not change what the control runs did (two still edited
        the interface, then its call, before a batch with check
        "end"); cost within noise.
      - The derivation on this project's own code: `change_field_type`'s
        flags into mode types (`docs/derivations.md` section 6), 39
        refactorings. Gaps: an inverse of `introduce_parameter`
        (replace a parameter with the argument every call passes),
        and `p.TypesInfo` treated as able to panic or change by
        `inline_variable` and `remove_parameter`.
    - **`inspect` by name** (the user, 2026-10-22): `names` reads
      several declarations in one call, each found in the module by
      its name, without file (`Name`, `Type.Method`, `pkg.Name` where
      a name is in several packages), to cut round trips, grouped
      under one header per file. *Done.* Not
      yet measured; a step toward reads that need a name (a read mode
      without file or line reads, and a question on each read, are
      the next ideas).
    - **Bodies read and not used** (`level-long-5`, no new runs;
      `docs/reading-and-deriving.md`): about three quarters of the
      body text read reached no edit, most of it with whole files,
      though reads were under 4% of input.
    - **Reads by name only** (the user, 2026-10-23): `refactor-mcp
      -read-by-name` makes `inspect` show source only for what a
      request names (`function`, `names`, `uses`); a line range is
      refused, and the tool's description, schema and the server's
      instructions stop offering ranges. Off by default. *Done.* The
      A/B arm `names` runs it: the mcp arm with the server started so
      and no other way to read Go (no Read, Grep, Glob, cat, head, tail
      or grep). *Done.* Next, the run: the mcp arm against `names`, the
      same instructions but for how to read
      (`eval-tasks/claude-md-names`), on freight-xl:

          go run ./cmd/refactor-ab -tasks eval-tasks/derivations-freight.json \
            -model sonnet -arms mcp,names -runs 4 \
            -only freight-dispatch,freight-routes,freight-tail,freight-run \
            -claude-md eval-tasks/claude-md-names -out eval-runs/names-freight

      About $5 (freight-xl was $0.15-0.20 a run). Measure passes, cost,
      turns, refused reads and bodies read (`eval-tasks/learning/bodies.py`).
25. **A budget of tools** (the user, 2026-10-22): more tools must not
    eat the gain over editing by hand. Today the tools cost about 5%
    of a request (the instructions, 5.8K characters, and the deferred
    names; a run's eight loaded schemas, about 12.5K characters, from
    when they load), a new tool 30-40 tokens a request; what grows
    faster is choosing among them. `TestToolBudget` holds the count
    at 50 tools (45 today, with move_method; the user raised it from 44 for now) and the
    instructions at 6,000 characters. *Done.* A new
    operation is, in this order:
    - a proof in the engine, not a tool: an edit to code the engine
      proves cannot run (behind `x.(Kind)` that no type implements)
      counts as preserving behavior, so `replace_statements` there is
      a refactoring;
    - an option of a tool the model already uses (`pointer` on
      extract_variable; wrapping statements in an `if` as an option
      of `insert_statements`);
    - a composite named by its intent, built inside the engine from
      primitives that are not tools (`prepare_switch_for_types` is a
      declaration, an empty guard, and a fill of the dead branch);
    - a new tool; at the budget, it merges or replaces one.
      Candidates to merge: `move_statement_up`/`move_statement_down`,
      `function_to_method`/`method_to_function`, the parameter tools
      into one; measure each, since cutting `fold`'s examples lost
      its use.
    Next: `refactor-tokens` reports the tools' overhead per run
    (schema characters loaded, ToolSearch turns), so a run shows when
    it starts to eat the gain.
26. **The design question as a step of its own** (the user, 2026-10-23):
    a fresh model extends what is there while making a change, though
    asked about the design alone it sees what is in the way (session
    notes, "Would a fresh model recognize it?"). Of the ways tried,
    only a session whose whole task is the question (told the next
    change, not to make it, and to refactor until the change would be
    one new place) refactored at the design level: on `design-tangled`
    both runs built a table of rules per kind, and the feature session
    then added one entry (`docs/naming-the-level.md`, cost-history
    11, PRs #297-#299). Next, in order:
    - *Repeat it with more runs*: *Done* (5 runs, and the two shapes
      below): the reference design in 15 of 15 preparing sessions
      where it was in the way, 0 of 15 in the control. The commands:

          GOTOOLCHAIN=go1.27.3 go run ./cmd/refactor-ab -model sonnet \
            -arms hybrid -runs 5 -tasks eval-tasks/level-step.json \
            -claude-md eval-tasks/claude-md-refactor-when-needed \
            -out eval-runs/level-step-5
          (the same with -tasks eval-tasks/level-question.json for
          the control)

      About $0.25 a task for the step's two sessions (Oct 23).
    - *Other shapes*: *Done*: `lending-magazine` and `freight-air`
      (`eval-tasks/level-question-2.json`, `level-step-2.json`).
    - *Does it pay back?* *Done* on design-tangled
      (`eval-tasks/level-payback.json`, 8 runs): the second change
      (student) was one table entry in 7 of 7 step runs and cost 11%
      less than without the step, but the preparing session cost
      twelve times that saving. Not in tokens on a module this
      small. On `lending-magazine` and `freight-air` too
      (`level-payback-2.json`): one row or accessor after the step,
      a case per switch or eight copies without, at the same cost.
      Five changes per project (`level-long.json`): *Done*, no pay
      back in tokens; lending's table held, pricing's was bypassed
      at a new rule, freight's control refactored on its own when a
      change cut across the copies (`docs/naming-the-level.md`).
      Open: whether larger code changes this, and measures other
      than tokens (missed copies, how far kind knowledge spreads).
    - *How the rule is computed*: on lazygit the step judged the
      change "one rule, already easy", while the PR first reshaped how
      the loaders query refs. Try a step wording that also asks where
      the new rule's inputs come from, and keep a task where the
      honest answer is "already easy" (`notify-optout`) to catch
      false refactoring.
    - *Where the step lives*, once it holds: in how a task is given
      (two prompts), in the instructions as "prepare in a subagent
      first", or in the server's instructions. Decide with the user.
    - Gaps the runs showed: no refactoring makes switches or
      if-chains on a kind into a table of rules per kind, nor
      functions that differ in the field they read into one that
      takes a selector (`parameterize` takes constants); the
      preparing sessions wrote both with edits and the tests. `fold`
      does not reach a copy whose condition is inverted (item 23).
27. **One matcher for code with holes** (the user, 2026-10-24): three
    refactorings decide, each its own way, whether two pieces of Go are
    the same and what a name in them is (a hole, a local, a plain name):
    fold's `folder.match` and `matchStmt` (a pattern with fn's
    parameters as holes), parameterize's `comparer` (two bodies the same
    but for constants, with `mismatch` saying where they first differ),
    and merge_duplicate_function's `canon` (a token stream per function,
    no holes). A package for Go code with holes, compiled once and
    matched by type, would answer "what is this name?" in one place,
    give fold the nearest miss for free (in the exploration runs,
    "nothing matches the body" was where models gave up), and be the
    search the back-burner query tool and item 21's last step need.
    fold's relaxations (a `%s` matching constant text, `+` matching a
    run of operands, `*p` matching an addressable expression, setters)
    stay fold's, as options: parameterize must not treat `"a"+"b"` as
    `"ab"`. Built inside fold first, moved out when parameterize is the
    second user; merge's `canon` compares tokens, not trees, so it would
    be a rewrite and is no test of the abstraction. One PR each:
    - The match state (`locals`, `bodyLocals`, `holes`, `fields`,
      `recv`) into a type of its own, with the `match*` methods on it
      (`extract_struct`). #309 made `match`'s cases methods first.
      *Done:* `matcher`, embedded in `folder`, also holds what the
      relaxations make (`addressOf`, `made`, `runs`).
    - A miss result from `match` and `matchStmt` (`add_result`), used in
      fold's "nothing matches" refusal: the nearest copy and where it
      differs. *Done,* as state rather than a result: `match` and
      `matchStmt` wrap `compareExpr` and `compareStmt` and keep the
      innermost failed comparison in `matcher.miss`; fold keeps the
      miss that got furthest (statements, then depth) and the refusal
      says "the nearest, m.go:5:28, has x where the body has 2".
    - The matcher into a package of its own, with fold's relaxations
      behind options. *Done:* `internal/pattern`'s `Matcher`, with
      `Relaxations` (Call, Concat, Address), which fold's `relaxer`
      implements and a nil `Relax` leaves off.
    - parameterize's `comparer` on it: the test that the abstraction is
      general, not fold's shape. *Done,* and what it showed: what the
      two share is the answer to "what is this name?" (`matchIdent`:
      locals by declaration, parameters through a seeded `Locals`) and
      where a match failed (`Miss`, with the expressions around a
      name). What they do not share is the comparison of nodes: fold's
      rules per kind cover the shapes fold folds, parameterize needs
      every shape compared exactly, so `Structural` brings the
      comparer's field-by-field walk into the package and `Literal`
      makes constants of one kind its holes. Old and new parameterize
      gave the same results on closures, labels, type assertions and
      parentheses (a scratch module, both CLIs). merge's `canon` is
      left as it is.

    An evaluation of the four by another session (2026-10-24) found
    half the design in: the package and its two users, but not the
    attempt or the compiled pattern, and a flag for a mode. The site's
    `info` and the bindings `b` are still threaded through thirteen
    signatures and the relaxations; `Locals` has two lifetimes (fold
    sets and clears it around each statement match, parameterize seeds
    it once with its parameters); `compareExpr`, `compareStmt` and
    `matchIdent` work out what each of the pattern's names is again at
    every site; and `Structural` is a bool for one of two ways of
    comparing nodes, tested in two places. One PR each, in this order,
    so that each makes the next smaller:
    - The attempt: one type for matching one site (the matcher, the
      site's info, the bindings, the code's locals and the miss), so
      the compare functions and the relaxations take `(p, x)`.
      *Done:* `Matcher.At(info)` starts a `pattern.Attempt`, which
      embeds the `Matcher` and holds `Info`, `Bound`, `Locals` and
      `Miss`; the matching methods are its own, without `info`, `b`,
      `pinfo` or `sub`. fold's backtracking relaxations try a copy of
      the bindings with `matchIn`.
    - Two comparers behind one interface, rules per kind of node
      (fold's) and field by field (parameterize's, with its literal
      hook), sharing name resolution and the miss, in place of
      `Structural` and `Literal`. *Done:* `pattern.Comparer` (`Expr`,
      `Stmt`) with `ByKind`, the default, and `Exactly{Literal}`, which
      parameterize uses; both call back into the attempt for names
      (`matchIdent`) and for the nodes inside, so the miss stays shared.
    - The compiled pattern: what each of the pattern's names is (a
      hole, a field of a struct hole, a local, a plain name), decided
      once when the pattern is made, not at every site. *Done* for
      identifiers: `Matcher.nameOf` decides what each of the pattern's
      identifiers is (`declared`, `bodyLocal`, `hole`, `plain`) the
      first time it is matched and keeps it; `matchIdent` switches on
      that (complexity 27 to 18, with `bindHole` apart) and
      `matchSelector` uses its object. What is left of compiling is
      fold's: `Plan` works out `Holes`, `Fields` and `BodyLocals` from
      fn, which `pattern` could do from a function declaration.
    - A list of standard-library calls without side effects
      (`reflect.TypeOf`, `json.Valid`), so that `analysis` stops
      refusing them; remove_dead_code refused `posType` for it.
      *Done,* in two lists, since the two calls differ: `reflect.TypeOf`
      is stable (a value's dynamic type never changes) and joins
      `knownFuncs`, which every refactoring uses; `json.Valid` reads a
      slice that other code can change, so it is not stable, and
      `HasEffect` still stops a refactoring from moving it past code
      that writes through a local. It goes in `readOnlyFuncs`
      (`analysis.ReadOnlyCall`: no side effects, never panics), which
      remove_dead_code uses, since deleting a read drops nothing.

28. **A parameter into what the body can work out, and methods onto
    another type** (the user, 2026-10-24): the largest hand edit of
    item 27 (#317) turned `info` and `b`, parameters of thirteen
    methods that the bodies read and passed down, into the receiver's
    `m.Info` and `m.Bound`: twenty-odd replace_text steps, safe only
    because the names were distinct. remove_parameter refuses a
    parameter the body reads, rightly. Two operations, in this order:
    - `value` on remove_parameter (Fowler's Replace Parameter with
      Query, IntelliJ's Inline Parameter; the inverse of
      introduce_parameter): every use of the parameter becomes value,
      an expression the body can evaluate (`m.Info`), and every call
      drops its argument. An option, not a tool (the budget, item 25);
      plain remove_parameter's refusal for a parameter the body reads
      names it. What the engine proves: each call passes the value in
      the caller's terms (`a.Info` at `a.Match(p, x, a.Info, b)`), or
      forwards the caller's own parameter in a call on the same
      receiver, which by induction is the value (the recursive calls);
      evaluating the value at each use gives what the argument gave at
      the call (nothing between writes `m.Info`, and it cannot panic:
      `analysis.Stable` and `MayPanic`, as introduce_parameter uses);
      and the body never assigns the parameter (writing through it, as
      `b[k] = x` does, is fine: it is the same map).
    - `move_method`: a method moves to another receiver type that
      holds the old receiver (`*Matcher` to `*Attempt`, which embeds
      it) or to one of its parameters' types, and every call follows.
      #317 changed thirteen receivers by hand; #318 moved two methods
      to `ByKind` in three refactorings each, through a parameter of
      an empty struct (method_to_function, add_parameter,
      function_to_method). Whether it is a tool or an option of
      function_to_method (a receiver from a parameter of any type) is
      for the budget to decide.
    With both, #317's derivation would have been: declare `Attempt`
    (an edit), move the methods, then remove_parameter with `value`
    for `info` and `b`. IntelliJ's other route, Introduce Parameter
    Object then Convert to Instance Method, needs the same two. Not
    this: one `change_signature` in place of add_parameter,
    remove_parameter and move_parameter (IntelliJ's Change Signature)
    would free two places of the budget and make several signature
    changes in one call, but would not have done #317: Change
    Signature does not delete a parameter the body uses either.
    *Done* (2026-10-29): `value` (#459), with `also` for functions
    that pass the parameter to each other: every body first gets the
    value written for its uses, then each loses the parameter, so a
    call among them that passed its own parameter on now passes the
    value, and the check that each call passes it holds; and plain
    remove_parameter's refusal for a parameter the body reads names
    `value`. Left: a function that only passes the parameter on and
    never evaluates it, when the value can panic (`m.info` through a
    pointer): dropping its arguments is refused. `move_method` is a
    tool (the budget had room: 45 of 50): it moves methods to a struct
    type that embeds their receiver's type, with `also` for those that
    call each other; the receiver's uses go through the embedded field
    where they must (as a value, or a selector the new type resolves
    elsewhere), `y.Soil.m()` becomes `y.m()`, and a call on a value no
    such struct holds is refused. To a parameter's type, the move is
    method_to_function then function_to_method with `receiver` (#318
    did it in three steps before `receiver` existed). #317's
    derivation is now: declare the struct (an edit), move_method with
    `also`, then remove_parameter with `value` and `also`.

29. **Plan, then execute: a loop of our own** (the user, 2026-10-25):
    with the tools, Sonnet and Haiku both cost more than editing by
    hand (cost-history 14), and the gap is turns, not the tools'
    context (13). A loop of our own could plan the whole derivation in
    one request from names and signatures, run it with the engine, and
    send only refusals or failing tests back, in two or three requests
    instead of 7-20. The call-tree probe is its planning half (7 of 16
    batches passed from the call tree, the failures the tools'
    interface, not the plans). Plan: `docs/plan-and-execute.md`.
    Step 1, the probe with repairs and the input schemas, against the
    manual arm with the same model (about $2-4); step 2, a runner on
    the API with its own context, only if step 1 pays. A research
    instrument: the decision that Claude through MCP is the product
    stands, and the user decides on the result.

30. **move_parameter counts a local as observable** (2026-10-25, the
    trap run, cost-history 15): with `from := p.Bin()` extracted,
    `l.Transfer(from, p.Bin(), p.Qty())` is refused ("moving p.Bin()
    past from would change the order"), though a local whose address
    is not taken and that no closure captures cannot be read or
    written by the call. Two of four Haiku runs hit it and had to
    extract the second argument too. The order check should treat
    such a local as stable. *Done:* a local of the function around
    the call, which no pointer or closure reaches and whose value holds
    no pointers (`privateLocals`), no longer counts as a read a call
    could change.

31. **A parameter in place of a package variable** (the user,
    2026-10-25, from cost-history 16): 5 of 8 plans for lending-policy
    named the new parameter like the package variable the body reads, so
    the body would read the parameter with no edit; add_parameter
    refused the hiding. *Done:* `replace_variable` on add_parameter. It
    refuses a parameter of another type than the variable, and a body
    that writes it. A call passing the variable itself, in a module that
    never assigns it, keeps behavior; any other call (`DefaultPolicy()`
    from another package) is an edit, which the result's note lists, as
    the user chose. Without a variable to hide, the option does nothing.
    The six plans, replayed with it: five pass the hidden tests.

32. **A case addressed by its value** (the user, 2026-10-25, from
    cost-history 19): kinds failed without reads because moving a
    case into a type starts with extract_function of its body, by
    lines the call-tree plan cannot see. *Done:* `function` and `case`
    (the value as written, or `default`) in place of the lines, on
    extract_function and replace_statements; a case of several values
    is refused, pointing to prepare_switch_for_types, which splits
    them. The kinds reference is now that derivation, 74 steps without
    a line number or a case body (widen, prepare each switch, then per
    case extract, add the receiver, function_to_method, rename;
    narrow), and passes the loop's free dry run. Open, found on the
    way:
    - a case of several values: split them on request (an option),
      except a case that falls through (the user: an extension for
      later);
    - function_to_method refuses a method named like one of an
      interface's ("a type could implement through it"), where rename
      proves no value of the type becomes an interface value: the
      reference makes `bookLoanDays` a method and renames it;
    - the receiver goes first: add_parameter needs `before` naming the
      first parameter, which a plan knows only from the signature;
    - narrowing leaves `switch it.Kind {}` once every case is gone; it
      could remove the empty switch.

    After the run without reads (cost-history 19), a bare word names
    the string case of that text (`book` for `"book"`) when no case is
    written so: one of Sonnet's repairs went to that refusal. *Done.*

    `split` on extract_function (the user: a case of several values
    split on request, except with fallthrough): the value gets a case
    of its own and each other value a case before it with a copy of
    the body. Refused when a value is not a constant (the order of
    comparison would change), the case before falls through into it,
    or the body declares a label or a type. In the rerun 5 of 8
    attempts met the shared case of Renewable. *Done.* Since then the
    default (the user, 2026-10-26, after two Sonnet attempts without
    reads still sent it without `split`): `split: false` refuses a
    shared case as before. *Done.*

    A value no case has: the refusal says what runs for it, the default
    case, or the code after the switch, with add_case to make it a case
    or writing it where it goes. In the rerun 5 of 8 attempts asked for
    Renewable's `atlas`. *Done.*

    function_to_method takes an interface's method name when, as
    rename proves, no value of the type becomes an interface value:
    the type's new interfaces are noted, not refused (and so for an
    interface checked at run time, String or Error). A type embedding
    the receiver type still refuses any such name. The kinds reference
    drops its renames: 60 steps. *Done.*

    The receiver need not be first: function_to_method's `receiver`
    names any parameter, and each call's argument for it moves to the
    operand, refused where move_parameter would refuse the move (order
    of evaluation). When the first parameter's type already has the
    name, the refusal names a parameter whose type could take it
    (`give receiver "k" to make it a method of Book`). The kinds
    reference adds the receiver last and names it. *Done.* The same
    hint ends the refusal of a first parameter whose type is not
    declared in the package (`title string` before `b Book`, after
    add_parameter appended the receiver): Haiku's derived kinds attempt
    stopped there and dropped `Label`. *Done.*

    Types used as an interface before they have its methods (`kindOf`
    returning `Book{}` as a `Kind` before the cases became methods, 3
    attempts in each of two runs without reads): the refusal of the
    edit, of a refactoring after it in a batch, and of a batch at its
    end says which types lack which interface's methods and to give
    them the methods in earlier steps. *Done.*

    A value with no case (Renewable's `atlas`, which stopped 7 of 8
    attempts in the next rerun): extract_function by case adds the
    case, by default, when the code after the switch is what runs for
    it (no default, every case returns, the code after it ends in a
    return): the new case calls a copy of that code, which stays. The
    kinds reference extracts atlas's Renewable so. *Done.*

    A batch's refusal gives lines as the batch began: the lines the
    model read and sends again, not those of the module at the refused
    step, which a repair that resent the batch copied into an early
    step ("line 168 on" pointed at other code). File-qualified lines
    are mapped in any file, bare ones (`line 12`, `lines 12-14`, a
    statement listed as `150-152` and its text) in the step's file; a
    line an earlier step wrote keeps its number, marked `*`. *Done.*

    The prompt's `Label(title string)`: prepare_switch_for_types takes
    a method the interface declares when the cases read their
    variables only as fields (`it.Title`) of the parameters' types, in
    order: it declares `title := it.Title` before the switch and the
    cases read `title`, the user's "extract variables for what
    differs, move them to the top". The kinds reference with the
    interface declared first, as Haiku planned it, passes. *Done.*

33. **Output holes in the pattern** (the user, 2026-10-25): `fold`, and so
    `pattern`, takes a function whose body returns one value, sets
    fields of a struct parameter and returns it, or has no results.
    Most code worth extracting is statements whose outputs are used
    after them (`x, err := ...`, then `x`), so a copy of such a run
    cannot be folded. An output hole is a variable the pattern's
    statements assign and the code after the call uses, bound to the
    site's own variable, so that the run becomes `x, err := f(...)`, or
    `x, err = f(...)` where the site's variables already exist. The
    rules mirror extract_function's flow analysis: the site's run
    produces the same outputs, of the same types, used after it; and it
    assigns nothing else that the code after it reads. The outputs of
    a run could be computed in one place, for extract_function at the
    selection and for the matcher at each site. What it would buy:
    - `fold` of statement runs with results: the mcpserver tests'
      `connect` helper, which fold refused (friction log, "reads by
      name only", 2026-10-23), and the near-duplicates the design-step
      runs wrote by hand (item 26).
    - `extract_function` with `all_occurrences`, as extract_variable
      has: a composite, extract then fold what it made, so the other
      copies become calls in the same step, with the nearest miss when
      a copy differs.
    One PR each: output holes in `pattern`, and fold taking such
    functions; then the extract_function option.
34. **One analysis of what code does** (the user, 2026-10-25): three
    analyses answer overlapping questions about an expression:
    `Func.Stable` (is its value the same if computed again or
    elsewhere), `MayPanic` (can it panic), and `Effects`' summaries
    (what a call or body writes, reads, can panic on, or stops), each
    with its own facts about the standard library, which drifted apart
    (`json.Valid` known to one, `reflect.TypeOf` to another). The
    merge, one PR each:
    1. *Done.* One table of standard-library facts, `stdFuncs`: what a
       function reads, writes, how it panics, and whether its result is
       new each call. `KnownCall` and `ReadOnlyCall` derive from it.
       Verdicts that changed with it: the go/types and io/fs getters
       Effects knew are now known to Stable; `os.Getenv` and the other
       functions that only read can be deleted; `strings.Join` can
       panic (a result too long), as ReadOnlyCall already said.
    2. *Done.* A summary's findings carry the node (`Summary.At`: the
       node, or the call through which it happens, and the first that
       can panic other than by a nil dereference) and the panic kind
       (`Summary.Panics`), so that callers can ask what Stable and
       MayPanic ask. Sends and selects count as able to panic (a
       closed channel).
    3. *Done.* A summary of one expression (`Effects.Of(f, x)`), the
       body walker applied to it, with the memory f made itself as
       f's own.
    4. *Done.* The callers of MayPanic, Stable, HasEffect and
       FirstEffectBefore moved to the summaries: `Summary.Stable`,
       `At.Panic` (any panic) and `At.OtherPanic` (other than a nil
       dereference), `Effects.StableAddress`, `Effects.Acts` and
       `FirstActBefore`, through `Workspace.Effects()`, which knows no
       module function yet. The walker learned what only Stable and
       MayPanic knew: shifts, conversions to arrays, which slices and
       string indexes can panic, the size of a map read through `len`,
       locals a pointer or closure can change, and `Makes` (a new
       reference value: `&`, a literal, a function value, a fresh
       result). `TestDifferential` compares the old and new verdicts on
       every expression of the goldens and this module (and of
       `DIFF_DIR`), and fails on any difference without a reason. The
       reasons, all looser where facts allow or stricter where the old
       check missed something: fmt calls of values without methods do
       nothing else; without sharing a new reference value is stable;
       a call that can panic is stable (callers ask At.Panic); a call
       that only makes a new value does not act; `make` of a size not
       known in range can panic; fold looked at an expression's own node
       only, missing calls inside it, and now also counts a new
       reference value when an expression is evaluated another number
       of times. Two goldens used fmt as their example of an effect and
       now use a call of `Error` and `os.Getenv`.
    7. *Done* in item 35, step 2: changesig's `riskOf` and
       `unsafeValue`, the last copies of the panic rules, became moves.
    8. *Done.* `stopsFuncs` is a `stops` fact in `stdFuncs` (a function
       with it is neither known nor read-only). `syncCalls` stays its
       own table: it says how a function treats its function-valued
       arguments (runs them before returning, reads or writes its first
       argument), a different kind of fact with its own summary.
    5. *Done.* Calls of the module's own functions judged by their
       summaries, made once for each state of the workspace
       (`Workspace.Effects()`, which changesig, fold, slide and inspect
       now share): by value, as stable as the arguments; reading
       through a pointer, a package variable, or caching into a field,
       not stable; an effect (logging), not stable; a panic, by the
       panic rules; a function value, a wall. A callee that makes a new
       reference value makes its call one. inline_variable's goldens
       hold a case of each (`module_call_by_value`, `refuse_module_*`,
       `refuse_function_value_call`). Fourteen goldens had pure
       functions stand in for effects (`func load() int { return 3 }`);
       they now print. A loop in a callee still counts as something
       that might not finish, so such calls stay walls. The user waived
       the question of a nil dereference moving with a call (2026-10-25).
    6. *Done.* The old analyses deleted (Func.Stable,
       Func.StableAddress, MayPanic, HasEffect, FirstEffectBefore), with
       the differential test that compared them; TestMayPanic became
       TestEffectsPanic, which found that the walker missed a division
       of a type parameter.
35. **Moving an evaluation** (2026-10-26): nearly every refactoring asks
    whether an expression can be evaluated somewhere else, some other
    number of times, and mean the same; each answered it with its own
    machinery (inline_variable's checkEvaluation and firstEffectBetween,
    extract_variable's checkOrder and checkWritesBefore,
    inline_function's movable and lhsEvaluatedFirst, changesig's
    checkEvaluation, riskOf and unsafeValue, fold's first, risky and
    impure, move_parameter's checkOrder, slide's checkIndependent). One
    abstraction consumes the summaries of item 34: `analysis.Move`, an
    expression of a function, the code it would move past (`Past`), how
    often it would then be evaluated (`Times`: Once, Maybe, Several,
    Shared), and whether every use dereferences it (`Dereferenced`, for
    a pointer alias). `Check` returns an `Obstacle` (what kind of thing
    the expression does, where in it, what it cannot pass, a sentence)
    or nil, by one set of rules: evaluated another number of times, an
    effect, a panic or a read of shared memory changes behavior (and
    with Shared, a new reference value); past code that writes what it
    reads (a local too) or acts, it may not read; past an effect, a
    panic or something that might not return, it may not panic, except
    that a nil dereference may pass an effect that returns (item 23).
    `EvaluatedBefore` gives the code of a statement evaluated before a
    position. One PR each:
    1. *Done.* `Move`, with extract_variable and inline_variable on it
       (their stable, panics, checkOrder, checkWritesBefore and
       firstEffectBetween deleted). Verdicts loosened with a reason: a
       read may now move past a call that only reads (`read(p)`), and
       `&v` of a variable or field is the same pointer each time, so it
       may be shared; stricter: a write to a local the expression reads,
       between declaration and use, is found by the move as well.
    2. *Done.* changesig: `checkEvaluation` is one move (to the call,
       past everything the body does first, Several or Maybe by the
       path), `unsafeValue` a move with `Unused` times, `checkOrder` two
       moves of each argument past the other, and `checkArgEffects` a
       summary; `risk`, `riskOf`, `risks`, `firstRisk`, `syncWalk`,
       `assignsOutside`, `readsVar` and `privateLocals` are deleted,
       and item 34's step 7 with them. introduce_parameter keeps its
       own policy that the value has no side effects at all (its own
       check, not a move rule). Verdicts loosened with a reason: two
       nil dereferences may swap as arguments; a local that holds a
       reference, passed as an argument, is not a read of what it
       points to. Out of its context, a write to a local a pointer or
       closure can reach is an effect (the walker's `partial`).
    3. *Done.* inline_function: `classify` sorts an argument by its
       summary (an effect, a panic, a read, or none), `effectBefore` is
       one move of the argument into the body (`PastFunc`, the body's
       function, since the argument's summary is the caller's), past
       what the body evaluates before the last use and the rest of each
       use's statement (`EvaluatedAfter`), `droppable` a move with
       `Unused` times, and the result's own effects a summary;
       `pureRead` is deleted. `lhsEvaluatedFirst` keeps `movable` (stable
       and no panic), since what it moves past is the whole inlined
       body. Loosened with a reason: a call that only reads classes as
       a read, not an effect; a dropped result that reads may be dropped.
    4. *Done.* slide: `checkIndependent` is two moves, each statement
       past the other (`Move.Expr` takes a statement), and `checkJumps`
       a move with `Maybe` times; its own walker (`addEffects`, 100
       lines, with `effect`, `shared`, `unsafe`, `nilDeref`) is deleted,
       and its field-path helpers moved to `analysis` (`PathOf`,
       `PathAbove`, `ParentsIn`, `Overlaps`), where the move uses them:
       a write to `p.y` does not touch a read of `p.x`, and a method
       with a pointer receiver called on a local writes it. fold:
       `evaluatedFirst` is a move of the argument past what the matched
       statements evaluate before its first use, `Shared` when the body
       uses the parameter several times; `effectful` reads the summary;
       `matters` is deleted. Found on the way: `Shared` need not refuse a
       panic (it panics the first time either way), `Several` must also
       refuse a new reference value (two allocations are two pointers),
       and inline_variable's several uses are `Several`, not `Shared`.
       fold's own hole judgments became moves in step 6.
    5. *Done.* `analysis.Reach(path)`: how often code is evaluated for
       each time the body at the path's end runs (Once, Maybe, Several)
       and why, as a phrase. It replaces six walks: inline_variable's
       `evaluatedOnce` and `conditionalOrRepeated`, inline_function's
       `firstUseUnconditional` and its own `conditionalOrRepeated`,
       changesig's `notOnEveryCall` (now a one-line wrapper) and the
       condition extract_variable's `place` computed on the way to the
       container. One wording per position (the right operand of &&,
       "which is only evaluated when the left operand is true"; an
       else-if; a case expression; a select case; a loop's condition,
       post and body; a function literal). Left as they are:
       inline_variable's `repeatingAncestor`, which needs the loop node,
       not the count.
    6. *Done.* The last judgments of their own. A function's summary
       stands as `Past` through its call: since item 34's step 5 a call
       of a module function is summarized by its body, so moving past
       the call is moving past what it does. inline_function's
       `lhsEvaluatedFirst` moves each operand on the left past the call
       (its arguments and body), and `movable` is deleted. fold judges
       an argument by where the code it replaces evaluated it: one that
       acts, panics or takes an address moves past what that code
       evaluates before its first use (`Shared` when the body uses it
       several times), and each argument the call evaluates before it
       but that code after it moves past it; one that only reads moves
       past what that code evaluates before its last use, since one
       value now stands for every use (`staleRead`). The expression
       case uses the same judgment as the statement case; a literal's
       fields set move past the elements after them and the other
       fields set. `first`, `risky`, `nilAddress` and `effectful` are
       deleted; `impure` stays as what decides which arguments move
       like an effect. Loosened with a reason: an operand on the left
       that reads through a pointer may stay first when the body
       writes nothing it reads (`lhs_pointer_read_body_writes_nothing`);
       a field of a struct argument may be evaluated first in a fold
       of statements; an argument that only panics may be used several
       times when it is evaluated first. Stricter: a field set that
       acts may not move past one that reads through a pointer
       (`refuse_setter_past_read`). Address arguments keep fold's own
       relaxation (`movedAddress`, item 23).
36. **Ownership of memory** (2026-10-26). *Done* in its first form:
    `analysis.Owner` is three-valued (`Anyone`, `ThisFunc`, `ThisCode`),
    and `Func.Owns(v)`, `Func.Owner(x)` and `Func.Pointee(x)` answer it
    for a variable, an assignable expression and what a pointer, slice
    or map points to, with the fresh locals computed once per `Func` on
    first use. The walker's `private` and `pointsPrivate` are one
    comparison each against `body.own`, the owner whose memory counts as
    the summarized code's: `ThisFunc` for a whole function, `ThisCode`
    for part of one out of its context, which replaces the `partial`
    flag; slide's `escapes` is `Owns(v) != ThisCode`. Then (2026-10-26) the
    `private` callback became a value: `owned`, the `Func` and `Owner`
    of a body, which `body` embeds and `call`, `stdCall`, `fmtCall` and
    `syncCallSummary` take (the zero `owned`, for `Effects.Call`, owns
    nothing). And `Outside(info)` is now only for code in no function
    or parsed apart (a value given for a new argument): fold and the
    signature changes' call sites take the function they are in,
    `analysis.FuncAround`. That fixed a fold that read a local after a
    write through a pointer to it (`refuse_stale_read_of_escaped_local`:
    out of its function, nothing showed the local escaped). Left:
    `Func.Escapes`
    (which counts a closure that runs during its statement, where
    `Owns` does not; its callers in inline_function and extract_function
    mean the stricter test) and inline_function's `neverChanges`.
37. **A site** (2026-10-26): fifteen refactorings carry `ws, pkg, info,
    file, src` and a target, with six copies each of `quote` and
    `declaredAt`, nine of `text`, ten of `off`, and seven `find`
    functions from a `locate.IdentQuery`. `internal/site.Site` is that
    file of the workspace (`WS`, `Pkg`, `Info`, `File`, `TF`, `Src`)
    with `Off`, `Text`, `Quote`, `Position` and `DeclaredAt`, and
    `site.At(ws, path)` makes one. Done so far: inline_variable,
    extract_variable and inline_function embed it (extract_struct,
    rename of each field to its exported name, then move_declaration
    for the first and an alias replaced by the package's type for the
    others, since move_declaration cannot merge into a type that
    exists). Found: move_declaration with `new_name` on an embedded
    type does not rename the field in keyed literals (`inliner{site:
    site{...}}` stays; it rolled back as a bug in its checks).
    Part 2: extract_function, invert_if (with flatten and remove_else),
    the switch refactorings, the signature changes' `target`,
    extract_struct, parameterize and create_from_usage embed it too;
    inline_function's declaring file is a second, named `Site`
    (`callee`), and rename uses `site.DeclaredAt`. `Site.Off` now
    goes through the file set, so it holds for a position in any
    file (`target` reads other files). `site.At` refuses a file
    outside the module's packages, and extract_function, invert_if
    and extract_variable open their file with it. Then (2026-10-26)
    eleven more openings: the edits add_declaration, replace_expression,
    insert_statements, fix_imports, add_case, replace_statements and
    add_element, slide, introduce_parameter, inline_function's caller
    file and create_from_usage's interface file, each now one call of
    `site.At` (an edit's refusal for a file outside the packages now
    reads "module's packages", as the refactorings' did). Left as they
    are, because they are not the same opening: locate and the switch
    refactorings need no source and name the patterns in their refusal;
    replace_text's imports skip files that are not Go quietly;
    merge_duplicate_function and create_from_usage's destinations pick
    the file among a package's syntax or write files that may not be
    loaded. Then the last two shapes: the declaration edits
    (replace_declaration, change_type, add_field) open the file a
    declaration is in with `site.Declaring(ws, o)`, which refuses a
    declaration outside the module's packages by its name and file; and
    move_declaration's files are sites. A move has one package and
    several files: each `source` embeds the `site.Site` of its file
    (its package the variant whose syntax holds it), the mover reaches
    the workspace, package and `Off` through the source it embeds (its
    own `ws`, `pkg` and `off` are gone), and the file a move edits is
    opened with `site.At`. Left: the seven `find`s, which look for
    different kinds of thing and so are not one function. Item 37 is
    *Done.*
38. **Gaps the friction log and session notes record but no item held**
    (a review, 2026-10-26). Dates are friction-log entries.
    - **Elements of a composite literal.** `add_element` adds one;
      nothing moves the elements of one literal into another or rewrites
      each element, so tables were merged by a script and sent as
      replace_declaration (Oct 18 twice, map entries; Oct 25, `stdFuncs`;
      Oct 26, `stopsFuncs`). *Done:* add_element's `from` adds every
      element of another variable's literal, written by a template of
      `$key` and `$value`; a key the literal has already merges when
      both values are keyed literals with no field in common, and `from`
      the literal itself rewrites each element. The literal can be named
      by its variable (`name`). Left: TypeScript's add_element, and
      merging into a value that is not a literal (`"os.Exit": stops`).
    - **Parameters into the receiver's state, and back.** Thirteen
      methods' `info` and `b` parameters became fields of their receiver
      as twenty-odd replace_text steps (Oct 24, `pattern.Attempt`). The
      inverse of `introduce_parameter`, replacing a parameter with the
      argument every call passes, is named only inside item 24 (Oct 22,
      `change_field_type`'s modes). *Done* for one function at a time:
      remove_parameter's `value` (the expression every call passes, in
      the body's terms: `b.info`) writes it for the body's uses and
      drops the parameter, once each call is shown to pass it and each
      place the body evaluates it to get what the call passed (the
      checks of introduce_parameter, run the other way); a recursive
      call passing the parameter on counts as passing it. Methods that
      pass the parameter to each other (the thirteen) go together with
      `also` (item 28).
    - **A field between structs.** `relaxer` moved from the matcher to
      `folder` by text (Oct 24): no tool moves a field to another struct
      that every use can reach. *Done:* extract_struct's `into` moves
      fields to the end of a struct that already exists, one the struct
      embeds (uses unchanged) or holds in a named field (`b.depth`
      becomes `b.soil.depth`), by value; literals carry the fields into
      the holding field's literal, or one made for them. Refused: a
      pointer (values sharing one would share the fields), a name the
      struct already has, an unkeyed literal of either struct, a
      struct passed as an interface (reflection). Across packages and
      the other direction (out of a held struct) are left.
    - **A field the embedded struct already provides.** `rename` rightly
      refuses `mover.ws` to `WS` when the embedded Site promotes a `WS`;
      what was wanted is to drop the field for the promoted one when
      every use sees the same value (Oct 26, done as text). *Done*
      (2026-10-30) as remove_dead_code's `value`, by the order of the
      tool budget (an option, a proof in the engine): `value WS` removes
      `mover.ws` when every literal of mover sets it to the `WS` of the
      `Site` it embeds (`ws: s.WS` beside `Site: s`), the literal's
      element for that field being a name or a path of fields, and
      nothing writes `ws`, `Site` or `WS` outside literals; its reads
      become `m.WS`, its elements go, then the field. A path through a
      pointer is refused (a literal would dereference it).
    - **A bug:** `move_declaration` with `new_name` on an embedded type
      leaves the field name in keyed literals (`inliner{site: site{...}}`)
      and rolls back as a bug in its checks (Oct 26; noted in item 37,
      now done).
    - **Refusals still too strict.** Each was worked around and none is
      on the roadmap: `extract_function` refuses a variable that a
      literal which can run later uses, though nothing assigns it again
      (Oct 19, `order`; Oct 23, `cs`); `add_parameter` counts a getter
      that makes its value on first use (`ws.Effects()`) as a side effect
      (Oct 25); `inline_variable` and `remove_parameter` treat
      `p.TypesInfo` as able to panic or change (Oct 22); `rename` refuses
      a field because a test prints its struct with `%+v` (Oct 25: a
      test's output is not behavior to keep, but that is the user's
      call); `introduce_parameter` counted defining a local as a write
      that could change what the expression reads (Oct 25, `freshLocals`;
      check whether item 35 fixed it). Rechecked (2026-10-29), each
      reproduced on a scratch module: extract_function's is *done*, a
      variable nothing writes after its declaration may be returned though
      a literal that runs later keeps it, since the copy holds the same
      (`writtenAgain`; still refused once anything writes it again);
      introduce_parameter's local and inline_variable's `p.TypesInfo`
      past a module function no longer refuse (items 34-35 fixed them);
      remove_parameter's dropped `p.TypesInfo` is right to refuse alone
      (a pointer read that can panic) and is now `value`'s case (item
      28). Left, as decisions rather than bugs: add_parameter's lazy
      getter, whose first call assigns a field (an effect, though
      repeating it changes nothing a caller can see), and rename's test
      that prints a struct with `%+v` (the user's call; *done* as item
      42's rule: printing is a note, not a refusal). The lazy getter
      is *done* (the user chose to recognise its shape): a method whose
      body is `if r.f == nil { ... }` then `return r.f` counts as
      reading `r.f` and what its block reads, and its writes to the
      cache as its own (`analysis.cacheOf`), so moving or repeating its
      call is judged as for any read. `docs/derivations.md` section 8
      has the example, a split that makes it apply past an early
      return, and the non-strict way with the tools before (pass the
      method value).
    - **Each call of a function by another chosen by its arguments.**
      Thirty-four calls of `set` became calls of the recorder the
      argument named, by a script (Oct 25): one more story for item 9's
      "every occurrence of a pattern". *Done* as a derivation, no tool:
      extract one call per value, fold it, inline the function
      (`docs/derivations.md` section 7).
    - **replace_text after gofmt's alignment.** `old` written as the
      model wrote it, before gofmt aligned it, is not found (Oct 19, a
      loop's indentation; Oct 20, a field comment; Oct 22, a map's
      keys). CLAUDE.md says to grep for the line; the tool could match
      ignoring whitespace when one place matches, or show the nearest
      match in its refusal. *Done:* when `old` is not found exactly,
      replace_text matches it with the spacing within lines ignored (a
      note says so), and a refusal names the lines where the first line
      of `old` the file has is (`textutil.Locate`, both languages).
    - **Files that are not Go.** No tool copies a file or a directory of
      fixtures (Oct 21, Oct 26: `cp`), replaces a whole non-Go file (Oct
      25, a reference batch rewritten by Python), or moves a block of
      text within a file (Oct 20, the friction log reordered by a
      script). Replacing a whole file is *done* (create_file's
      `overwrite`), and so is copying one through the CLI: create_file
      with `content_file` naming the file to copy. A directory of
      fixtures, and moving a block of text, are not.
    - **Experiments the session notes leave open:** a hazard as one site
      among many, or a prompt that does not ask to keep behavior
      (cost-history 15); and tasks a model gets wrong by hand, named as
      the next evidence for the tools (cost-history 14).
39. **From use on another project** (glisp, 2026-10-27, its `FRICTION.md`:
    seventeen PRs through the CLI on a small Lisp interpreter). Already
    done from it: `refactor check` and `refactor undo` (#408, #409),
    `add_case` before a trailing default (#412), tagged releases (v0.3.0).
    Left, roughly by what it cost:
    - **One indentation rule for edits.** On files indented with spaces
      (not gofmt'd), the edits disagreed: `replace_statements` by
      `function` re-indented with tabs, by lines kept the file's spaces;
      `add_declaration` and `insert_statements` kept the content as
      written; `add_case` put the content's own tab after the file's
      spaces; added imports went in with a tab among space-indented ones
      (four times). Fixed in the end by gofmt'ing the project. Either
      every edit gofmts what it writes, or every edit follows the file's
      indentation; and `check` could list files that are not gofmt'd, so
      the mismatch shows on the first edit. *Done* the second way: in a
      file indented with spaces, each leading tab of what any edit writes
      at a line's start becomes the file's unit of indentation (the
      shortest run of leading spaces; `workspace.followIndent`, one place
      for every edit and the imports they add; text with a raw string is
      left as written). gofmt-ed files are formatted after each step, as
      before. `check` lists the module's files that are not gofmt-ed.
    - **The CLI's `list` gives no argument names.** It prints the
      descriptions, not the schemas, so `replace_expression`'s old text
      (`expression`) and `remove_dead_code`'s `also` were guessed or
      avoided. `refactor list` (or `refactor help <tool>`) could print each
      tool's arguments. *Done:* `refactor list [tool...]` prints each
      tool's description and its arguments, required ones marked.
    - **A stub in the wrong file, and quietly.** A test's undeclared
      matcher got a stub (`panic("not implemented")`) in the production
      file, though only `_test.go` used it, and the result said only
      "APPLIED batch: 2 steps". A stub for a name only test files use
      belongs in the test file, and the result should name each stub.
      The result names each stub and its file on a "chose:" line (it did
      by the time of this item; glisp's run printed the batch's summary
      alone). Where a stub goes stays as it is: a name only a test calls is
      also what test-first work calls before writing it, which belongs in
      the production file, and nothing tells the two apart. A stub for a
      test's call now says so on its "chose:" line, with the to_file that
      puts it in the test file instead.
    - **A refusal that names another tool's argument.** `add_declaration`
      of a test using `_, err := RunFile(...)` was refused with "give them
      in results", an argument of `create_from_usage`, not of
      `add_declaration`. The hint should name `create_from_usage` (with
      `results`), or `add_declaration` should take `results` and pass it
      on. *Done* the first way: the refusal names create_from_usage and,
      in a batch, a step of it before the step that needs it.
    - **A line only renamed stays addressable.** In a batch, a step that
      addressed a line an earlier `rename` had changed (only the
      identifier) was refused as rewritten; the session split the batch in
      two. A line whose only change is a rename could keep its number.
      *Done:* after a step that makes the same change at every site (a
      rename), a line it changed within, adding no line break, keeps its
      number (`edit.LineMapOfInPlace`).
    - **Smaller:** `add_parameter` could note a local in the body that
      shadows the new parameter (*done*: a declaration in an inner scope is
      noted; one at the body's top level was already refused); `replace_statements`' description could
      say that empty `content` deletes the statements (used three times,
      found by trying); `check` could take `race` or test flags (`go test
      -race` was run by hand; *done*: check's `race`, with `run_tests`); a tool or a `remove_dead_code` option to key
      an unkeyed literal (three literals restated by hand to remove a
      field; with item 38's literal elements; *done* for what that was
      for: remove_dead_code deletes a field's element from unkeyed
      literals when it is a constant or nil, and refuses one it would
      have to evaluate).
    - **One server, one module.** The server reads its module once at
      start, so a session on one module cannot use it on another; a tool
      or `inspect` argument to open another module, or a server serving
      several roots. *Done* for Go: a call that names a file of another
      module (a directory with its own go.mod, beside or inside this one;
      `../tools/a.go`, or an absolute path) works in that module, loaded
      on first use; its result starts by saying which module, with paths
      relative to its root, and undo reverts it there. A call or batch
      naming files of two modules is refused. Left: TypeScript (a
      program per tsconfig.json), and a `to` of move_file in another
      module, which stays relative to that module's root.
    - **Versions:** `refactor` has no `-version`, and both binaries report
      only the constant; reporting the module version or commit from the
      build information (`debug.ReadBuildInfo`) would tell which build is
      running. *Done:* both take `-version` and print the release with the
      module version they were installed at, or the commit they were built
      from (`mcpserver.BuildVersion`).
    - **Setup notes for the README:** an `.mcp.json` added during a
      session loads only in a new one; the tools need Go 1.26, which
      `GOTOOLCHAIN=auto` fetches but `GOTOOLCHAIN=local` does not; and the
      "Using gorefactor well" section, copied into another project, names
      `mcp__gorefactor__` tools and "this checkout", which do not hold
      through the desktop bridge (`mcp__remote-devices__gorefactor__`) or
      with `go run ...@latest`. *Done* ("Using it from Claude Code",
      setup notes).
    - **Later the same day** (glisp PRs #35-#49: a WebAssembly backend,
      a garbage collector, strings, line numbers in errors; ~4,000 lines,
      nearly all through the CLI's `batch`). What it added, by what it
      cost:
      - **`create_file` from a file.** Every new file (eleven, up to 900
        lines) was written in a scratch directory and then put into a
        batch's JSON by a script, since `create_file` takes its content
        only inline. A `content_file` argument naming a local file would
        remove the script; the same for `add_declaration`'s and
        `replace_statements`' `content`. *Done,* for every tool and
        both languages: `content_file` gives `content` from a file
        (`new_file` gives `new`, and so on: any `X_file` the schema
        lacks, for a text argument `X` it has), a path absolute or
        relative to the root (`refactor.readFileArgs`).
      - **`replace_declaration` takes exactly one declaration.** Twice the
        replacement came with a new helper beside it; the refusal ("it has
        2") rolled the batch back, and the fix was `replace_declaration`
        plus `add_declaration` with `after`. Taking the extra declarations
        and placing them after the replaced one would save the retry.
        *Done:* `content` may hold the new declaration and then
        declarations to add after it (refused if another comes first, or
        for one spec of a group); the note names what was added. It came
        up a third and fourth time in glisp's next round (#58, `Lambda`'s
        `String` with two new methods; #54's `slotStore` and `pushCall`
        earlier), worked around by a splitting script.
      - **A new name's clash shows only at the end.** New files used names
        the package already had (`str`, `field`); a batch with `check:
        "end"` caught it, but only after all steps, rolling back eleven. A
        way to ask whether a name is free in a package (an `inspect`
        option) would let a new file be checked before it is sent.
        *Done:* `inspect` `names` with `pkg.name` says of a name nothing
        declares that it is free to declare, or that a file of the
        package imports a package by that name (`strings`), or that it is
        predeclared (`min`).
      - **Where a type is asserted.** Giving lambdas a type of their own
        meant finding every `x.(Function)` and type switch on it; `inspect`
        `uses` lists a type's every reference (conversions, signatures),
        so this was a `grep`. A `uses` filter for type assertions and
        switch cases would find exactly those. *Done:* `uses` of a type
        groups them as their own: asserted, a type switch case (with its
        switch), converted to, made by a literal; the rest (signatures,
        receivers) stay under read.
      - **Smaller:** `add_field` inside a batch with `check: "end"` let
        unkeyed literals through until the last step, whose refusal then
        named them; alone it refuses at once, which is clearer.
        `create_from_usage`'s `to_file` must already exist ("is not a file
        of package"); creating it would save a `create_file` with only a
        package clause. *Done:* add_field gives every unkeyed literal of
        the struct the new fields' zero values in their place (refusing,
        with the literal, only when a zero value names a package its file
        does not import), so it no longer refuses them at all; and a
        `to_file` that does not exist, in the target package's directory,
        is created with the stubs. Experiments (a constant changed for a timing
        probe, then restored) went around the tools; `undo` covers them
        only when the change went through a tool.
    - **The third round** (2026-10-28, glisp's entries from "direct
      calls to builtins" to the language spec: the wasm generator
      restructured, builtins and value kinds made tables, rest
      parameters, vectors, CHAMP maps). Most PRs were one batch that
      passed first time; `replace_declaration` taking extra
      declarations (above) settled the most repeated refusal. What is
      left, by what it cost:
      - **Wrap statements in a closure passed to a call.** Most of the
        `wasmBody` PR (nine batches, about 120 steps) turned `If ...
        Else ... End` and `Block Loop ... End` runs into `ifElse(func()
        {...}, ...)`/`when`/`until` calls, by Python-computed
        `replace_text`. Item 20's containers stop at a closure called at
        once; this is the same edit with the literal passed to a named
        function, its free variables staying captured. *Done:*
        replace_statements' `wrap` (item 20).
      - **Delete a declaration by name, whatever its uses.** Old
        versions of helpers whose new versions live in another file went
        by `replace_text` of their whole text, found by fragile regexes:
        `remove_dead_code` needs them unused, `move_declaration` moves
        them unchanged. An edit that deletes a named declaration (its
        comment included), checked with the batch, or
        `replace_declaration` with `to_file`. *Done:*
        replace_declaration with empty `content` deletes the declaration
        (a spec of a group alone), its doc comment and the blank line
        that set it apart, and the imports only it used (Go and
        TypeScript); what it breaks is refused, or fixed in the batch.
      - **Replace text within a function.** `compileLambda` was changed
        by substring replacements computed outside and sent as a whole
        `replace_declaration`. `replace_text` scoped by `function` would
        send only the changed parts, and make `old` unique there.
        *Done:* replace_text's `function` (Go and TypeScript).
      - **Imports tidied at the end of a batch checked at its end.** A
        step replacing the last user of an import removed it, and the
        next step, which needed it, was refused; with `check: "end"`
        unused imports could wait for the end, as stubs do. *Done:* in a
        batch checked at its end, edits leave the imports they make
        unused (`refactor.ImportKeeper`), so a later step can still use
        one by a name no search would find (an alias, or a package
        nothing else imports); the end's fix_imports removes those
        still unused, as a step of the result.
      - **Append to a file, and replace a whole file.** Friction entries
        went in by heredoc until the session found it could anchor
        `replace_text` on the last entry's final lines (this project's
        sessions did the same, 126 times); an `append` (or `at: "end"`)
        has no anchor to find. `create_file` refuses an existing file, so
        a new `wasm_maps.go` took create, delete and move; an
        `overwrite` option would be one step. *Done:* replace_text's
        `append` adds `new` at the end of the file, and create_file's
        `overwrite` replaces one that exists (both languages).
      - **Asked again:** whether a name is free in a package (twice more:
        `field`, `strings`), and `create_file` from a local file (a batch
        was lost to an unquoted heredoc running the backticks in Go
        content).
      - **Not a tool gap, but worth a look:** splitting `builtin`'s
        `min`, `max` into an embedded `arity` took 29 `replace_text`
        steps generated from `inspect`'s listing (the first script
        misread its line-number prefix), where `extract_struct` should do
      it in one (it rewrites unkeyed literals whose moved fields are
      adjacent; untried on glisp). The session did not find the
      tool; its description says "split a struct that holds two
      things", not "group fields" (*done*: it now says both). The type check twice let through a
        behavior change in a hand-written edit (a case's dropped
        `return`, an inverted test); output hashed before and after
        caught both, which says what a check for a pure refactoring
        could compare.
40. **TypeScript** (2026-10-27, the user): the same tools for TypeScript,
    in this repo and from this server, under the same names; each tool
    picks the language from its file's extension. Built on the Go port
    of the TypeScript compiler (TypeScript 7), imported through
    `buke/typescript-go-internal` until there is an official Go API; its
    language service has rename, references and file moves but none of
    the refactorings, so those are ours, as for Go. The plan, the tool
    mapping, what TypeScript needs checked that Go did not, and the
    steps are in `docs/ts-plan.md`: first a spike that proves in-memory
    edits and re-checking are fast enough, then the CLAUDE.md split and
    CI path filters, then making the engine language-neutral (the undo
    stack out of the Go workspace, a dispatcher with Go as its only
    language), and only then TypeScript itself. A workspace per root
    (a Go workspace per `go.mod`, a program per `tsconfig.json`) also
    answers item 39's "one server, one module" for Go.
    Step 0, the spike, is *done* (`docs/ts-spike.md`): on Hono (82k
    lines with tests) loading takes 0.33 s, a body edit re-checks in
    27 ms through the incremental builder, an exported change in 1.4-3.4
    s on 1-2 cores, and rename through the language server's session is
    correct in 10 ms; it found that every project of a solution must be
    loaded (a file's default project left its tests out of a rename).

41. **TypeScript's gaps against Go** (2026-10-28, the user asked for
    them on the roadmap and to work through them in this order, one PR
    each; the cost of the TypeScript tools against editing by hand is on
    the back burner meanwhile, so these are judged by what they let the
    model do, not by A/B cost):
    1. Stubs for a test written first. *Done*: a call passed where any
       value goes (`equal(got: unknown, ...)`) gets a stub returning
       `unknown`, and add_declaration of a function or method replaces
       its stub in the file or the class, as Go's does.
    2. `change_field_type`, `change_parameter_type` and `change_type`.
       *Done*, as edits: each writes or adds the annotation (a property
       of a class, interface or object type; a parameter; a type
       alias's target). Types are erased, so no value changes, and the
       uses are not adapted: a batch with check "end" changes those
       that no longer type-check after it. Go converts at the uses; an
       assertion (`as`) would be TypeScript's counterpart, for later if
       runs show the model needs it.
    3. `function_to_method` and `method_to_function`. *Done*: a
       function becomes a method of the class its first parameter is
       typed with, declared in the same file, the parameter becoming
       `this`; a method becomes a function after its class taking the
       instance first, imported where it is called. Calls keep their
       evaluation order. Refused: dynamic dispatch (a member that
       overrides, implements or is overridden), `this` or the parameter
       in a nested function, `super`, `#` names, `?.` calls, recursion
       and uses as a value.
    4. `prepare_switch_for_types` (only `union_switch_to_method`, its
       last step, exists). *Done*: as Go's, an interface gets the
       method (the locals the cases use its parameters) and a guard
       before the switch hands it a subject that has the method, as
       `typeof s === "object" && s !== null && "m" in s`, then
       `(s as I).m(...)`; the subject must be able to hold an object
       (widen it to `unknown` first, as Go to `any`). The rest of the
       one-case-at-a-time path needs item 8 (extract_function by
       `case`) and narrowing with values, which change_field_type does
       not do in TypeScript yet.
    5. `extract_struct` (TypeScript has extract_interface and
       convert_params_to_object). *Done* as a name: Go embeds the new
       struct so every use keeps working, and an interface that extends
       the new one is TypeScript's counterpart, which extract_interface
       makes; so extract_struct on a TypeScript file runs it, as
       type_switch_to_method runs union_switch_to_method (the same
       arguments). Go's next step, a named field (`m.path` to
       `m.src.path`), would change the objects' shape at run time in
       TypeScript; not done.
    6. The edits `add_case`, `add_element` and `replace_declaration`.
       *Done*: clauses at a switch's end (before a final default and
       the cases falling into it) or next to a clause, `else if`
       branches at a chain's end or next to one; an element in an object
       or array literal, at its end or by key, in the literal's layout;
       a declaration replaced by name, its doc comment kept unless the
       content has one. With these every Go tool has a TypeScript
       counterpart.
    7. inline_function's refusals where a sound form exists: an early
       return first, then rest or destructured parameters, overloads,
       generics, async functions. *Done* but async: guards and a last
       return become a conditional expression, inlined anywhere
       (`truncate`'s shape in the TypeScript A/B run); a body that
       returns early is inlined where the call is a return's whole
       value; a rest parameter is declared as the array of the rest, a
       destructured one as its pattern; of overloads the implementation
       is inlined, and the signatures go with it; a generic function
       whose body does not name its type parameters. Async stays
       refused: inlining an await changes when the rest runs. Go's
       inline_function then still refused an early return (it inlines
       them since #472).
    8. extract_function addressed by `function` and `case` (a switch
       case), as Go's. *Done*, for replace_statements too: the case's
       value as written (`"fern"`, `Kind.Fern`) or `default`; a last
       `break` stays in the switch. Lines that end with a return now
       keep their returns wherever they are (the call becomes
       `return f(...)`), as a case's body that returns needs; before,
       only lines that ended the function's body did. Go's copy of the
       code after the switch for a value with no case is not done.
    9. The plan's smaller leftovers: all_calls across files, declaring a
       generic function in extract_function, a moved declaration the
       destination imports under an alias, deleting an old file left
       empty, fold of near-duplicates that differ in constants, and a
       base class in union_switch_to_method. *Done*: all_calls inlines
       every file's calls and drops their imports; extract_function
       declares the type parameters the lines use; the destination's
       uses of an alias take the declaration's name; a file left empty
       is deleted; a string parameter of fold's pattern stands for
       constant text of a template and a run of a + chain; an abstract
       class's direct subclasses take a union's place, the base getting
       the method as abstract (one level of subclasses).
    10. Tooling: TypeScript in the `refactor` CLI (inspect and batches),
        and the A/B quality columns for TypeScript. *Done*: a CLI call
        naming TypeScript files runs through the server's languages
        (`mcpserver.Server.Call`), and `refactor undo` reverts its
        steps as Go's: both languages' invocations load and save one
        stack file per module, and undo goes to the language whose
        files the last change wrote. `metrics` measures TypeScript functions
        (`tsapi.Measure`: complexity and nesting counted as Go's), so
        refactor-ab's quality table covers TypeScript runs.
    11. With the user: whether to keep tolerating errors already in the
        code (a step may add none), and real repositories beyond Hono,
        where union_switch_to_method and merge_duplicate_function found
        nothing to apply to. *Decided* (2026-10-28, the user): keep
        tolerating them for now; no other repository in mind, so those
        two tools rest on their unit tests until one comes up.

42. **Renaming a field that fmt prints** (2026-10-29, the user): rename
    refused an exported field whenever a value holding its struct was
    passed as an interface, so a test's `t.Errorf("%+v", got)` blocked
    it (friction log, 2026-10-25). `analysis.ReflectedArgs` now says what
    each callee sees (`Sight`): fmt's, log's and testing's printers see
    field names only through `%+v` and `%#v`, and not in a value that
    prints itself (`String`, `Error`, `Format`, `GoString`);
    `reflect.DeepEqual` sees none; any other function, or a conversion, may
    find fields by name. rename refuses only the last; for printers it
    goes ahead and lists in a note the calls whose output changes, for
    unexported fields too (they were not checked, though `%+v` prints
    them). *Done.* Open:
    - **Wrappers:** a `logf(format string, args ...any)` that passes to
      fmt is a function not known, so it still refuses. vet's printf
      check finds wrappers by facts; a summary could carry "this
      parameter reaches a printer". *Done* (2026-10-30,
      `analysis.wrappersIn`): a function of the module whose last,
      variadic `...any` parameter only goes, spread, to a printer in its
      arguments' place (with its string parameter before it as the
      format, unchanged, when the printer is formatted) counts as that
      printer, methods too, and so does one that passes on to such a
      function; the call's own format then decides what is printed. One
      that also uses its arguments another way (`json.Marshal(args)`), or
      changes the format, is still not known.
    - **Other users:** extract_struct (the printed shape changes) and
      remove_dead_code still used `PassedAsInterface`, which counts every
      callee. *Done:* both go ahead where fmt prints the value, at any
      verb (`Prints` as well as `PrintsNames`), and note the calls;
      remove_dead_code still refuses `reflect.DeepEqual` (`Compares`: two
      values that differed only in the field would compare equal), and
      both refuse what may find fields by name. `ReflectedIn` runs it
      over a workspace's packages, once per argument.
    - **Values not followed:** assignments to interface variables and
      returns are not looked at (`var x any = v; json.Marshal(x)`).

43. **TypeScript against Go, what still differs** (2026-10-29, the user
    asked where TypeScript does not match Go; read from the code and
    run on scratch projects, after item 41 closed). Go has move_method,
    which TypeScript refuses; TypeScript alone has
    convert_params_to_object. In order of how often feature work in
    TypeScript would meet them:
    1. **Tests at a checkpoint.** A batch's `check` step with
       `run_tests` ran `go test` in the TypeScript project, and failed
       the batch; `check` ran none. *Done:* both run package.json's test
       script, or `node --test`, with `packages` naming test files.
    2. **create_from_usage stubs functions and methods only.** A test
       written first that does `new Cart()` or reads `x.total` gets no
       class or property; no `to_file`, no `results`
       (`tsapi/stubs.go`). *Done:* `new Pot(3)` declares
       `class Pot { constructor(n: number) {} }`, before the statement
       that first makes it (classes are not hoisted) or exported from the
       module the file imports it from; a property read or written on a
       class or interface becomes `size!: number;` (`size: number;` in
       an interface), typed from its place or what is assigned;
       `results` gives types the uses do not tell; `to_file` puts the
       file's own stubs in another file, exported and imported.
    3. **rename by `Class.member`, and locals by `function`.** A member
       needs a line ("declares no top-level", `ts/refactorings/rename`);
       the lookup other tools use for members could serve it. *Done:*
       without line, `Class.member` names a member of a class, interface
       or object type; `function` (name or `Class.method`) finds a
       parameter, variable or constant of that function, and refuses one
       declared twice in it, giving the lines.
    4. **The server's instructions are Go's** in a TypeScript-only
       project ("tools for the Go module"); only inspect's text adapts.
       *Done:* `TypeScriptInstructions` for a project without Go, with
       TypeScript's tools, rules (errors already there are tolerated) and
       a worked batch that `TestTypeScriptInstructions` runs; with both
       languages, Go's text names TypeScript in its first line. Go alone
       is unchanged, for its baselines.
    5. **Method families.** add_parameter and the other signature tools
       on a method that implements an interface or overrides one are
       refused by the type check; Go changes the family together
       (`changesig` `findFamily`). *Done* for add_parameter,
       remove_parameter and move_parameter (`tsapi` `family`): the same
       method of every type it implements or extends, through types that
       do not declare it, and of every type that implements or extends one
       of those, changes with it, and calls through any of them; an
       interface's method (`Iface.method`) can be the one named. Only
       types related by `implements` and `extends` count: a class that
       fits an interface without naming it is left to the type check.
       add_result, change_parameter_type and convert_params_to_object
       change the family too (item 43.10): each method of it binds the
       object's keys to its own names (`{ bed, liters: amount }`).
    6. **inspect `uses`** is refused. *Done:* each name (`Name` or
       `Class.member`) lists its uses in the solution by role (switch on
       it, compared, written, called, made with new, used as a type,
       read), each with its line; a name declared in two files is refused
       with both.
    7. **add_field does not fill object literals** of the type that lack
       the property; they are refused. Go fills unkeyed literals. *Done:*
       add_field's `value` gives one new property to every object literal
       of the type that lacks it (a literal with a spread is left to the
       type check), in each literal's layout.
    8. **One schema per tool in a project with both languages**
       (`lang.Dispatcher.Tools` keeps the first language's): TypeScript's
       own options (`add_element`'s `column`, `add_result`'s `field` and
       `kept`) are not advertised, and Go's schema forbids unknown
       properties. Merge the properties of the two. *Done:*
       `lang.mergeSchemas` publishes the properties of both, requiring
       only what both require; each language still refuses arguments it
       does not take.
    9. **rename through string keys.** Nothing refuses or notes a type
       whose values reach `JSON.stringify`, `Object.keys`, `in` or an
       index by a string, as Go's reflection check does for
       encoding/json and fmt. *Done* (`tsapi.KeyedUses`): renaming a
       member of a class, interface or object type is refused where a
       value holding the type (itself, in an array, a union or a property)
       reaches `JSON.stringify`, `Object.keys`/`values`/`entries`/
       `assign`/`getOwnPropertyNames`, `Reflect.ownKeys`, `for...in`,
       or `"name" in x`; where `console` prints one, it goes ahead with a
       note, as Go does for fmt. An index by a string literal is renamed
       by the language service itself.
    10. Rarer, and most compose from existing tools: remove_parameter's
        `value` and `also`; add_element's `name` and `from`; fold's
        setter and parameter-object matches; replace_statements' `wrap`;
        add_parameter's `replace_variable`; change_field_type's `values`;
        extract_function's `split`; other solutions than the nearest
        tsconfig's; a cached getter read as a read (`analysis.cacheOf`);
        move_method. *Done:* replace_statements' `wrap`
        (`tsapi.Project.Wrap`: the statements as they are at a `$body`
        line, which TypeScript parses as an identifier, so no placeholder;
        refused where a name they declare is used after them; notes for a
        return, break or continue that now acts on the new function, loop
        or switch); add_element's `name` (a top-level variable of the file,
        through `as` and `satisfies`) and `from` (`$key`, `$value`);
        extract_function's `split`: a case sharing its body (`case "fern":
        case "moss":`) gets a case of its own, the others a copy before it
        (`tsapi.Project.CaseBody`), refused for a body that does not end
        in a jump, declares a name, or cases that are not constants.
        remove_parameter's `value` and `also` (`tsapi` `WriteValue`, then
        `target.shown`, two steps per function as Go's `Expand`; the
        per-call check is introduce_parameter's own, `target.introduced`,
        so the inverse refuses where introducing would); add_parameter's
        `replace_variable` (a variable at a module's top level, of the
        parameter's type after widening a literal type, that no body
        assigns; a note when the variable is assigned anywhere or the
        value is not it). A family's method is still refused for `value`.
        change_field_type's `values` (`tsapi` `valueEdits`: each old
        constant becomes its value where the property is given in a
        literal of its type, assigned, compared or switched on); a cached
        getter read as a read (`tsapi` `cacheOf`: `if (this.f ===
        undefined) { ... } return this.f;`, or `== null`, `!this.f`; a
        getter is summarized at all only when static or private, as a
        method is, since a subclass could override a public one); fold's
        parameter-object match (tried after the plain match fails, so
        `p.height * 2` still folds to `tall(p, ...)`) and setter match (an
        object literal of the setter's type ending with its property, so
        the keys keep their order); move_method, down to a class that
        extends the method's (TypeScript has no embedding: the subclass is
        what the outer struct is in Go, and every use must reach the method
        through it); other solutions, routed as Go's other modules: a call
        naming a file in none of the solution's projects, whose nearest
        tsconfig.json is not one of them either, works in that tsconfig's
        solution, says so, and undo follows it. All of 43.10 is *Done.*
    Deliberate differences, not gaps: errors already in the code are
    tolerated (item 41.11), type changes are edits (41.2), add_result
    returns an object (`field`, `kept`), extract_struct runs
    extract_interface (41.5), async functions are not inlined (41.7).
    Also found: remove_else's `wrap` does not re-indent the moved
    statements in a file that is not formatted (both languages' output
    was right on formatted files).
44. **Extracting code that returns a refusal** (2026-10-29, found by
    running the compositions item 43.10's rewrites skipped): Go's
    extract_function refuses lines that return on some paths unless the
    function returns an `error`, and this repo's own functions return a
    refusal `string` (`([]Edit, string)`, `(int, int, string)`). All four
    rewrites hit it first: add_result's per-method edits (the old result
    type and the result annotation each refuse early), introduce_parameter's
    per-call text, and CaseLines' clause lookup. TypeScript's
    extract_function already handles lines that return early (the call
    returns what they returned, or whether they did). Candidate: take the
    last result's zero value as "did not return" for any comparable type,
    not only a nil `error`: `if a, b, why := f(...); why != "" { return
    a, b, why }`. This is also the second use that session notes name for
    giving a function one result variable that each return assigns.
    Found on the way: introduce_parameter of `(*found)[0]` replaced the
    first of two occurrences in caseBody, not the one in the refusal
    after an `if`, and wrote `(*(&found))[0]` at the call; extract_function
    passed `found` by pointer because a closure in the caller uses it,
    though the new function only reads it; add_parameter does not reach a
    function literal bound to a local (`try := func(...)`).
    Not the only way round: extracting the tail and passing what follows
    as a parameter (`docs/derivations.md` section 10), and making a
    closure a function that builds it (section 11), compose today; run
    five chains per rewrite, they got add_result's per-method edits,
    CaseLines' lookup and the per-call loop into shared functions in
    continuation-passing style, and threaded fold's mode. The chains
    found four more defects: introduce_parameter refuses the method value
    `t.findCalls` because `fn.Type()` "could change memory" it reads;
    extract_variable's `all_occurrences` takes `found[0]` to change
    between two reads with nothing in between; extract_function refuses
    lines where `more, why := ...` redeclares a variable a closure
    captures (it would assign the pointer); add_result refuses a return
    of another call's results rather than splitting it into variables.
    *Done* (2026-10-30): extract_function treats a string last result as
    it treats an error, "" for nil (`exitKind`): early returns whose
    string is known not to be empty (a constant, fmt.Sprintf of a format
    with text besides its verbs, a variable checked against "", or a
    call of a function whose every return passes such a string) become
    the new function's, and the call site checks `why != ""`, named after
    the enclosing function's last result when it is named. Numbers were
    left out: an int result returned early reads as a value, not an exit.
    Run on the old code: introduce_parameter's per-call text now
    extracts; add_result's per-method part and CaseLines' lookup stop at
    the next obstacle, a variable that `visit`, a closure passed to the
    compiler's `ForEachChild`, uses after the lines (no known function
    that runs it only during the call). Of the four defects: a local a
    closure captures counts as the function's own memory only when
    something writes it after its declaration (`Func.WrittenAgain`), so
    `t.findCalls` introduces; a `:=` that redeclares a variable passed as
    a pointer becomes `var a T` and `a, *v = f()` (`redeclareEdits`);
    add_result splits `return f()` into `r1, r2 := f()` and `return r1,
    r2, value` (`freshNames`). `all_occurrences` on `found[0]` was right
    to refuse: `found` is captured by a closure the compiler's
    `ForEachChild` receives, which could keep it and run it later.

45. **Abstractions the code lacks, and the duplication they would
    remove** (2026-10-30, the user asked which abstractions the code has
    and which it lacks). Measured at 748e6b7: 64,500 lines outside
    tests; exact clones of 40 tokens or more (`dupl`) 1,194 lines, 1.9%.
    Most duplication is not textual: 38 of the 42 tools exist for both
    languages (about 34k lines of Go against 17k of TypeScript), and a
    few idioms repeat around them. Cheap removal is about 2,500-3,500
    lines (4-5%); the larger gain is that the languages and the schemas
    can no longer drift. In order of payoff:
    1. **Schemas from argument structs** (about 1,000-1,300 lines): 83
       JSON schemas written by hand (about 970 property lines, half
       repeated between the languages, which is why `mergeSchemas`
       exists), 92 argument decodings, 20 "file must exist" checks.
       One struct per operation with field tags, its schema generated,
       shared by Go and TypeScript for the common tools.
       *Done* (2026-10-30), but for the sharing: `refactor.Schema[T]()`
       makes each tool's schema from its arguments struct (`desc` tags,
       omitempty for optional, `enum`, `schema:"-"`, and omit for a tool
       that shares another's struct), and every published schema came
       out the same, in the same order, as the text it replaces. About
       740 lines fewer, not 1,000: the descriptions stay, in the tags.
       Sharing a struct between the languages was not done: of the 177
       properties the two give the same tool, 127 are described
       differently, so one struct would change one language's published
       text and the A/B baselines with it. Within TypeScript's changesig
       the file and name every tool takes are one embedded struct
       (`function`). The server's own inspect, check and batch keep
       their written schemas (nested objects).
    2. **TypeScript workspace forwarding** (about 300-400 lines): most
       of the 33 functions in `ts/workspace` find the project and call
       the same function on it; one generic helper, as `ws.Edits` is.
       *Done* (2026-10-30), not by a generic helper: the forwarders
       return different tuples, and each had one caller. They are gone;
       `ws.Project(file)` gives the path and the project that has it, or
       the refusal (no such file, in none of the projects), and callers
       call `tsapi` themselves, which also folded their `os.Stat`
       checks in. Rename reuses `merged`. 128 lines fewer.
    3. **Walking a TypeScript project** (about 200 lines, and one place
       for the filter): helpers for the project's own files (12 loops
       repeat the `.d.ts` and `node_modules` filter), for taking a
       checker (29 times) and for walking a tree (36 visit closures);
       an error type in `tsapi` for the `(x, why string)` convention
       (35 `Refusef("%s", why)`).
       *Done in part* (2026-10-30): `inspect(n, f)` walks a tree as
       go/ast's Inspect does, and replaced 19 of the 36 visit closures
       (those whose only exit from the walk is skipping a node's
       children; the rest stop early, carry a second argument, or start
       from a node's children); `p.sources()` is the project's own
       files, for the six loops that skipped declaration files and
       node_modules. Not done: the checker helper (its two lines would
       stay two) and the error type, which would touch every function
       of tsapi for no fewer lines; extract_function already treats the
       string as an error. About 40 lines fewer with the new file.
    4. **One decision of whether an evaluation can move** (about 200
       lines): `analysis.Move` and tsapi's `Check`, `conflict` and
       `canReorder` decide the same question; a shared core over a
       small summary interface, so the languages cannot disagree.
       *Not built yet* (2026-10-30): compared rule by rule, the two
       already agree where their models overlap (an effect may not
       happen another number of times; a panic or throw may not move
       under a condition; one shared value may throw at its first use;
       a write and a read of the same state keep their order), and
       sharing that part would save a few lines, since each words its
       own refusals. They disagree in three places, each a choice of
       meaning rather than of code:
       - **Reads under Shared or Several.** Go refuses any expression
         that reads state another evaluation could see changed, whatever
         runs between; TypeScript refuses only when the code between
         (`Past`) writes it. So `extract_variable` with
         all_occurrences of `b.depth` in `b.depth*2 + b.depth` (a
         pointer) is refused in Go and done in TypeScript. Go's rule is
         needed while its callers pass no `Past` for Shared
         (extract_variable, fold) or Several (inline_variable,
         changesig's evaluation); taking TypeScript's would mean
         passing the code between the occurrences. *Done for
         extract_variable* (2026-10-30, the user): it passes the code
         between the declaration and the last occurrence, with
         `Move.Between`, which judges reads against that code only, as
         TypeScript does, and inline_variable the same for several uses
         (its round trip with extract_variable needs it). fold and
         changesig's evaluation keep Go's rule.
       - **Nil dereferences.** Go lets a nil dereference move past calls
         that return (session notes, 2026-10-21); TypeScript has no
         such case and refuses a throw past a write.
       - **Leaving.** TypeScript counts a return, break or continue as
         acting, so nothing that acts moves past it; Go asks this only
         of code that may panic (`transfers`), and of an effect only
         when the code may not finish (`Stops`).
       Which way each should go is the user's to choose; the shared
       core follows from that.
       *Decided and done* (2026-10-30, the user): TypeScript takes Go's
       nil dereference: a read through what may be null or undefined
       may be evaluated on the other side of code that neither throws
       another way nor leaves (`Effects.OtherThrows`, `mayPass`), so two
       such reads swap. Go takes TypeScript's leaving: an effect does
       not move past a return, break or continue (`transfers` in the
       effect case of `Move.Check`). fold judges a hole used several
       times in the body by `Move.Between` over the code it replaces,
       so `c.Get() + c.Get()` folds into `double(c.Get())` when nothing
       there writes what `Get` reads (strict again when that code holds
       a function literal). With extract_variable and inline_variable
       before, only changesig's evaluation still refuses any read under
       Several; the shared core is still to build.
    5. **One "always equals E" proof** (about 200-300 lines):
       remove_parameter's `value` (both languages), remove_dead_code's
       `value` and add_parameter's `replace_variable` each prove that
       a value equals an expression and substitute it; three are the
       same write-then-remove expander (`refactor.Then`).
       *Done in part* (2026-10-30): the steps an Expand makes that are
       not tools are `refactor.Inner` and `InnerEdit` values (name,
       schema, a plan function) instead of a type each with four
       methods, and both languages' remove_parameter with value build
       their steps with `refactor.WriteThenRemove`. The proofs were not
       merged: each shows a different thing (a call passes the value, in
       the call's terms; every literal sets the field to a path of its
       others; a call passes the variable's value), and what they have
       in common is already shared (`locate.EqualTokens`,
       `checkIntroduced` and `substitute`, which introduce_parameter
       uses too). 47 lines fewer in the steps, about as many in the new
       file.
    6. **A golden harness for TypeScript**, as `refactortest` is for Go
       (about 300 test lines): `load`, `run`, `check` and
       `mustContain` are redefined 37 times in 17 files.
       *Done* (2026-10-30): `internal/ts/tstest` writes and loads a
       solution (`Load`), runs a refactoring (`Run`) and reads a file
       back (`Read`); the packages' own helpers now call it, 34 bodies
       in 16 test files, about 200 lines fewer. Not a golden directory
       harness: the TypeScript tests keep their cases inline.
    7. **The exact clones** (about 500 lines removable): Go's and
       TypeScript's inspect, the two workspaces, the two language
       front ends, and code shared by extract_function and
       inline_function.
       *Done in part* (2026-10-30): the language front ends' path
       helpers are `lang.Exists`, `ExistingDir` and `RelTo`; both
       inspects number source with `textutil.Numbered`; both workspaces
       take a change's paths from `history.Paths`. Left: inspect's
       `targets`, the workspaces' per-file part of `Apply` and
       `Preload`, each a dozen lines over types that differ, and
       extract_function's and inline_function's shared code.
    Not proposed: one representation of code that both syntax trees map
    onto, which would remove the parallel implementations but means
    rewriting most of the engine; sharing at the decision layer (4, 5)
    is the step short of it.

Alongside: **edits**, steps that change behavior on purpose, so the
change itself is a tool call too (Step 5 above); `docs/edits.md` lists the
candidates and the evidence for each, gathered from the friction log.

Parked: the Ollama loop (`refactor-agent`). It works and stays, but Claude
through MCP is the priority.
