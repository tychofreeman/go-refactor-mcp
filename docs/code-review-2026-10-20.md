# Code review: go-refactor-mcp

Oct 20, 2026 · @Chris

The core design holds up, but the refactorings have 7 high-severity ways to change behavior silently, mostly through expressions that can panic and methods reached by reflection.

## Scope and method

The review covers main at a641b61 (PR #128): about 30K lines of non-test Go in 44 packages, split four ways: the core engine, `extract_function` and `inline_function` with the shared analysis, the other refactorings, and the edits plus the A/B harness and commands.

Almost every finding was reproduced by running the tool on a small test module. Four were re-run independently: the `iota` deletion, `introduce_parameter` past a nil guard, `rename` to `String`, and the batch left half-applied after `go.mod` broke. Findings not reproduced are marked *(not reproduced)*.

## What is solid

The core design held up under every attempt to break it:

- **Preview, apply and undo:** edits are computed from fresh bytes, and undo checks that the files haven't changed since. The fresh-load comparison in the golden tests is strong evidence that the incremental reload is right.
- **Batch line following:** it uses the actual edits rather than a diff, and refuses an ambiguous line rather than guessing.
- **Analysis the reviewers couldn't break:**
  - `extract_function` decides correctly when to pass a pointer and when a copy;
  - `inline_function` handles names and receivers correctly;
  - `invert_if` negates exactly;
  - `move_statement`'s aliasing analysis held;
  - imports are handled conservatively;
  - the A/B harness is fair between arms.

The weak spots form a pattern. Most real bugs come from **expressions that can panic** being treated as effect-free, and from **behavior reached through reflection or dynamic dispatch** (`String`, `WriteTo`, JSON), which the type checker can't see.

## High: silent behavior changes

Each of these is accepted by the tool and changes what the program does, or breaks the promise that every step type-checks and can be undone.

| # | Finding | Where | Example |
| --- | --- | --- | --- |
| 1 | `remove_dead_code` deletes a member of an `iota` group. *Fixed in #141.* | `deadcode/checks.go:112` | In `const ( low = iota; mid; high )`, removing an unused `mid` changes `high` from 2 to 1. `move_declaration` already refuses this case. |
| 2 | `introduce_parameter` moves an expression past an early-return guard. *Fixed in #138, with a constant index into a string counted as able to panic.* | `changesig/evaluation.go:147` | From `if n == nil { return "<nil>" }; return n.name` it makes `label(n, n.name)`, so `label(nil)` now panics. Same for `xs[0]` after a `len(xs) == 0` check. |
| 3 | `rename` can make a method start implementing an interface. *Fixed in #143.* | `rename/rename.go:423` | Renaming `text` to `String` is accepted, and `fmt.Println(p)` output changes. Only the old name is checked; the signature-change tools check both directions. |
| 4 | `rename` misses interfaces in dependencies the module never names. *Fixed in #144.* | `rename/rename.go:488` | Renaming `WriteTo` silently stops `io.Copy` from using it. The signature-change tools already search every dependency. |
| 5 | `merge_duplicate_function` treats `s[:n]` and `s[n:]` as identical. *Fixed in #142, with a `for` loop's init and post.* | `mergefunc/mergefunc.go:189` | The comparison ignores which slice bounds are given and whether a call spreads with `...`, so `append(xs, ys...)` matches `append(xs, ys)`. |
| 6 | A batch isn't rolled back when a step breaks the module's loading. *Fixed in #139.* | `refactor/refactor.go:361`, `workspace/workspace.go:409` | A `replace_text` that breaks `go.mod` as step 2: rollback undoes the wrong entry, so `go.mod` is restored but step 1 stays applied, unreported. |
| 7 | An edit that breaks `go.mod` is applied and can't be undone through the server. *Fixed: the edit is refused and put back (#139), and `undo` works while the module doesn't load (#140).* | `workspace/workspace.go:420`, `mcpserver/server.go:291` | The write happens, then `undo`, like every other tool, reloads the module first and fails. Every tool is stuck until the file is fixed by hand. |

## Medium

Item 8 is the root of several others: one fix in `Stable` covers both of its consequences.

| # | Finding | Where | Example |
| --- | --- | --- | --- |
| 8 | Panicking expressions (`a/b`, `arr[i]`, `s[0]`, type assertions) are treated as stable. *Fixed in #136: `analysis.MayPanic`.* | `analysis/analysis.go:239` | `inline_variable` moves `q := a/b` past `if b == 0 { return }`, so a panic disappears; `inline_function` substitutes `arr[i]` into `flag && ...`. |
| 9 | `inline_function` changes evaluation order on the left of an assignment. *Fixed in #145.* | `inlinefunc/inlinefunc.go:390` | In `m[k()] = f()`, `f`'s body now runs before `k()`. |
| 10 | `inline_function` assumes a parameter is dead after the call despite a backward `goto`. *Fixed in #146.* | `inlinefunc/merge.go:245` | The repro's result changed from 27 to 34. `extract_function` already checks for `goto`. |
| 11 | `inline_function` misses `else if` conditions and `for` post statements as conditional positions. *Fixed in #147.* | `inlinefunc/params.go:493` | A `p.n` substituted into `else if p.n > 0` no longer panics for `p == nil` when the first branch is taken. |
| 12 | Effect summaries skip the bodies of synchronous closures. *Fixed in #148, in changesig, the caller that skipped them.* | `analysis/effects.go:143` | `introduce_parameter` moved a `FieldsFunc` call whose closure increments a counter: it now runs once per call instead of once per loop pass. |
| 13 | `extract_function`'s error-exit shape misses a later write to `err` in a loop, and its non-nil helper check ignores `defer`/`recover`. *Fixed in #149.* | `extractfunc/returns.go:328`, `:250` | A helper that recovers from a panic returns a nil error, though every `return` passes a non-nil one. |
| 14 | `remove_dead_code` deletes `String` or `WriteTo` in package main. *Fixed in #150.* | `deadcode/checks.go:95` | They're unused in code, but `fmt` and `io` call them dynamically. |
| 15 | `rename` of a struct field silently changes its JSON key | `rename/rename.go:261` | Renaming `Name` to `name` drops it from `json.Marshal` output. |
| 16 | Statement content can contain a `}` that closes the block and adds top-level declarations | `edits/gocode/gocode.go:35` | The result still says "replaced the body of helper". A model pasting a range that runs past the function's end would trigger it. |
| 17 | `undo` of a step that created a directory fails halfway if another file appeared there | `workspace/workspace.go:541` | Files are restored only partly, and the undo entry stays on the stack. |
| 18 | A relative path resolves differently for new files | `workspace/workspace.go:288` | With the server in a subdirectory, `create_file` puts a new file at the module root, while other tools resolve the same name in the subdirectory. |

## Low

Mostly false refusals, misleading results and one crash; none changes behavior except the gaps in item 25.

| # | Finding | Where | Example |
| --- | --- | --- | --- |
| 19 | Crash: `introduce_parameter` on an expression using a variadic parameter | `changesig/introparam.go:316` | Panics with index out of range when a call passes no variadic arguments. |
| 20 | `create_file` gofmts any content that parses as Go, including non-`.go` golden fixtures | `workspace/workspace.go:388` | The result still says "added as written". |
| 21 | `replace_expression` never removes an import it leaves unused | `edits/replaceexpr/replaceexpr.go:97` | The refusal blames the model's code: "strings imported and not used". |
| 22 | A/B harness: runs that time out count as cost 0 in the per-arm means; the `Bash(cat:*)` rule may allow `cat > file` in the mcp arm *(not reproduced)* | `ab/ab.go:330`, `:59` | An arm that times out more often looks cheaper. |
| 23 | A line gofmt realigned after an earlier batch step is read as rewritten | `edit/diff.go:331` | A later step addressing that line is refused as "rewritten by step 1". |
| 24 | Argument aliases in a batch skip line translation | `refactor/refactor.go:373` | A step sent with `line_no` instead of `line` uses the untranslated number. |
| 25 | Smaller behavior gaps | `rename`, `slidestmt`, `extractfunc` | `rename` can stop a `Test…` function from running or start running a helper; `move_statement` doesn't count `make(n)` with a variable size as able to panic; pre-1.22 loop variables captured by goroutines. |

**Code quality.** Two whitelists of synchronous-callee functions have drifted apart (`analysis.go:45`, `effects.go:143`). `extract_function` duplicates the terminating-statement logic in `analysis/flow.go`. The README said only `move_statement` uses the effect summaries, but the signature-change tools do too (corrected in #148).

## What to fix first

Each is a separate PR under the repo's one-item-per-PR rule.

1. **Items 6 and 7:** the guarantees that every step type-checks and every step undoes. On a failed reload, restore the files and refuse the step; let `undo` run without reloading first.
2. **Item 8, by fixing `Stable`, together with item 2.** These cover most of the panic-class bugs.
3. **Items 1 and 3–5:** each is a small, well-understood check.

**Order decided (2026-10-20): the panic class first,** items 8 and 2, since they are likelier in real use; then items 6 and 7, which need an unusual step (an edit that breaks `go.mod`). Items 8 (#136), 2 (#138), 6 (#139) and 7 (#139, #140) are done, then the rest of the high ones: 1 (#141), 5 (#142), 3 (#143) and 4 (#144). All high items are done.
