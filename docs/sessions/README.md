# Session records

One file per item of work, `<branch>.md`, written by the `whats-next`
skill (`.claude/skills/whats-next`) and committed in the item's PR. A
record says what the item was, the plan made before reading code, what
happened as it happened, and what the session log measured, so the user
can judge whether the work went through the tools or around them.

The measurement comes from the session log: the hook `cmd/session-log`
(`.claude/settings.json`) appends every tool call to
`.claude/session-log/<branch>.jsonl`, which is gitignored, and
`go run ./cmd/refactor-tokens -log FILE` reports on it.

## Template

    # <item, in a few words>

    - **Item:** what was chosen, and from where (roadmap item N, a
      friction-log entry, a measurement)
    - **Date:** YYYY-MM-DD
    - **Branch:** <branch>, PR #N

    ## Plan

    The change at the highest level that captures it, then the
    derivation: the operations, in order, that make it easy, and the one
    small edit that changes behavior. Written before reading bodies.

    ## Notes

    One line each, as it happens: `hand:`, `refusal:`, `read:`, `plan:`.

    ## Measured

    The output of `refactor-tokens -log`.

    ## Assessment

    Through the tools or around them; whether the plan held; reading; one
    change that would have helped, and where it is recorded.

    ## Follow-ups
