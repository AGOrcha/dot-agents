# di-refactor-rollout — SUPERSEDED

**Archived:** 2026-05-23 (orchestrator session-start triage)
**Superseded by:** `seam-interface-di-migration` (live at `.agents/workflow/plans/seam-interface-di-migration/`)

## Why archived

This plan (created 2026-05-14) described a "Deps struct + `WithXxx` test
helpers" rollout across 6 packages (`commands`, `commands/kg`,
`commands/agents`, `internal/platform`, `internal/graphstore`,
`internal/projectsync`). All 6 tasks had `write_scope: []` and pointed at
"the workflow-di-refactor reference implementation".

That reference implementation was explicitly rejected during the
[[prefer-interface-di-over-funcvar-seams]] lesson (`.agents/lessons/`).
The newer `seam-interface-di-migration` plan (created 2026-05-19)
delivers the *per-file interface-DI* shape instead — narrow collaborator
interfaces, prod struct impl, constructor/parameter injection — which
landed for `commands/` end-to-end in PR #40 (merged 2026-05-21).

## Task overlap

| di-refactor-rollout task | Status in seam-interface-di-migration |
|---|---|
| `commands` | Completed (PR #40) |
| `commands-kg` | Active as `kg-pkg` (pending) |
| `commands-agents` | Active as `agents-pkg` (pending) |
| `internal-platform` | Active as `platform-pkg` (pending) |
| `internal-graphstore` | **Not in newer plan** — see follow-up below |
| `internal-projectsync` | **Not in newer plan** — see follow-up below |

## Non-overlapping bits (potential follow-ups)

`internal/graphstore` and `internal/projectsync` are NOT covered by
`seam-interface-di-migration`. Before assuming they need the same
conversion, audit whether they actually have func-var seams:

- `internal/graphstore` already uses constructor injection (`NewHandle`
  per the canonical seam doc) — likely already on the right shape, no
  conversion needed.
- `internal/projectsync` — needs a quick grep for `var .* = func` /
  `var .*Fn = ` package-var seam patterns before deciding.

If either does carry func-var seams, surface them as new tasks on
`seam-interface-di-migration` rather than reviving this plan.

## Cross-references

- Live plan: `.agents/workflow/plans/seam-interface-di-migration/`
- Lesson: `.agents/lessons/prefer-interface-di-over-funcvar-seams/`
- Memory: `[[seam-interface-di-migration]]`
