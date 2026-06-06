# Acceptance-invariants lens — dot-agents repo overlay

Repo-local committed layer. Composes **after** `reviewers/reviewer.base.md` (the contract) and
`reviewers/acceptance-invariants.md` (the lens: intent + platform invariants). This file adds **only**
the dot-agents invariants in force.

## dot-agents platform invariants to check survived

- **Managed-link / link-model:** generated platform outputs (Claude/Cursor/Codex/Copilot dirs) are
  managed links projected from the asset store — a task that touches a managed path must use the
  managed generation flow, not write the final path directly.
- **Config-v2 coherence:** scope precedence (product → user → org → team → repo-imported → repo-local
  → project-local overlay → runtime), the lock's `inputs_digest` / outputs parity, and protected-field
  rules hold; new `AgentsRC` fields follow the schema-usage lifecycle (struct + core mirror +
  Unmarshal + Marshal + `agentsRCKnown` + `schemas/agentsrc.schema.json`).
- **Cross-OS contracts:** behavior is identical on POSIX and Windows, routed through `internal/fsops`
  / `internal/testutil` rather than inline `runtime.GOOS` (see lesson
  `leverage-cross-platform-fs-helpers`).
- **Workflow artifact model:** spec → plan → tasks → history tiers are not collapsed; done criteria
  trace back to the spec.

Acceptance criteria come from the originating task/spec/plan. Verdict line
`(lens: acceptance-invariants)`; `fail` on any BLOCKER/HIGH.
