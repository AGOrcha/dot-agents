# Lesson: Symbol-Only References in Workflow Artifacts

## Rule

In any workflow artifact (`PLAN.yaml`, `TASKS.yaml`, `*.plan.md`, `design.md`, `merge-back.md`, lessons), reference code locations by **symbol** — function name, struct name, file path, or relational anchor ("after preferences resolution", "before mergeIterLogTopLevelGit returns") — never by **line number**.

## Why

Line numbers are the most fragile reference an artifact can carry. They drift the moment any sibling task lands a write to the same file, even when the referenced symbol is untouched. A loop-worker reading TASKS.yaml from a cold start will follow a stale `state.go:785` reference and edit the wrong place — or waste a turn searching for what moved.

Symbols are stable across edits to other parts of the file, survive refactors that don't rename, and are greppable. A reader without context can resolve `enrichWorkflowState (after preferences resolution)` even if the function moved 200 lines. They cannot resolve `state.go:270` if 80 lines were inserted above.

This came up in self-review of the platform-session-integration plan: TASKS.yaml referenced `enrichWorkflowState (line 270)` and `state.go:785`. After P2 landed, the actual locations were 271 and 834 — drift inside the same plan, before any cross-plan churn.

## How to apply

When writing a TASKS note, plan, or spec that points at code:

- **Functions/methods:** name them. `In renderWorkflowOrientMarkdown after renderOrientRecentCommitsSection` — not `at state.go:785`.
- **Insertion points inside a function:** describe the relational anchor. `In enrichWorkflowState, after preferences resolution` — not `state.go line 270`.
- **Struct fields:** name the struct + field. `Add RecentSessions to workflowOrientState` — not `add a field at types.go:73`.
- **File-scope additions:** name the file only. `New file: commands/session_stats.go` — line numbers don't apply.
- **Schema sync points:** name the test that enforces parity. `TestWorkflowIterLogEmbeddedSchemaMatchesCanonical` — not `at static/workflow-iter-log.schema.json:42`.

If you must call out a region too granular for a symbol, use surrounding code as the anchor: "between the `RecentCommits` and `Preferences` blocks in workflowOrientState".

The only place line numbers belong is in tool output — `git blame`, `go build` errors, lint findings — where the producer guarantees they're current.
