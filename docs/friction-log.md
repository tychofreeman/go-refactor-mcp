# Friction log

The point of this project is a workflow: during a normal story, Claude
picks refactorings that make the change easy, the tools perform them
deterministically, and Claude then makes the small hand edit the story
needs. This log records, from real work, every time that broke down: a
refactoring done by hand because no tool covers it, a refusal that got in
the way, or a tool result that was correct but awkward. It is the evidence
for what to build next, and for whether the workflow helps.

Each entry: the story, the refactoring wanted, what happened, and the gap.

## 2026-10-17: adding `refactor-eval`

**Add a parameter at every call site.** `copyModule(src, dst)` needed a
directory to skip. Done by hand: the signature and its one call. Gap:
`change_signature` (add a parameter).

**Extract a function that returns a value instead of changing a pointer.**
`extract_function` on the diff-line counting loop in `runOne` produced
`countDiffLines(rep, &r)`. That is correct: a deferred closure captures `r`,
so the tool shares it through a pointer. But the function wanted was
`r.DiffLines = diffLines(rep.Diff)`. Finished by hand. Gap:
`change_signature` (narrow a parameter to the field the body uses, turn a
pointer that is only written into a result), or a choice in
`extract_function` to return the new value.

**Move a declaration to another package.** `defaultHost` in
`cmd/refactor-agent` was needed by the new command too, so it moved to
`internal/agent` as `DefaultHost`. Done by hand. Gap: a move-declaration
refactoring (with the rename `rename` can already do).

**A poor extraction gets no warning.** A local model (qwen3.5:9b) extracted
31 lines from the middle of a block of shared counters into a function with
13 parameters and 10 results. The tool applied it without comment; the
model's next step was to undo it, then apply it again. Gap: no signal in the
result that the new function's signature is very wide. A note in the
`APPLIED` result ("the new function takes 13 parameters and returns 10
results") would cost little and help any model judge its own step.
