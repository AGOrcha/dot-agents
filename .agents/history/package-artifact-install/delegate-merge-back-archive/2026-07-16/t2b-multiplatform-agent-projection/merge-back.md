---
schema_version: 1
task_id: t2b-multiplatform-agent-projection
parent_plan_id: package-artifact-install
title: 'Extend CAS-aware sourced-unit projection to Codex/OpenCode/Copilot (file-shaped renders)'
summary: 'Extended per-project CAS-direct projection to the file-shaped platforms (Codex TOML renders, OpenCode, Copilot), then closed four review defects in round 2 — Codex user-file overwrite (durable managed-provenance marker + fail-closed ResourceReplaceIfManaged), a verify->unlink TOCTOU (OS-atomic path exchange RENAME_EXCHANGE/RENAME_SWAP with reverse-on-race restore, fail-closed where unavailable), Codex omission from the exact-prune pass (ManagedRenderProjector interface), and a lock-error-collapse-to-empty (surfaced via errors.Join). -race green; verified in a real Linux container.'
files_changed:
    - internal/platform/codex.go
    - internal/platform/opencode.go
    - internal/platform/copilot.go
    - internal/platform/resource_plan.go
    - internal/platform/resource_plan_swap_linux.go
    - internal/platform/resource_plan_swap_darwin.go
    - internal/platform/resource_plan_swap_other.go
verification_result:
    status: pass
    summary: 'go test ./internal/... -race + ./commands/... green; cross-builds darwin/linux/windows clean; gofmt clean; RENAME_EXCHANGE repoint + raced-user-file restore verified in an orbstack golang:1.26 container.'
integration_notes: 'One-time upgrade friction — a pre-existing UNMARKED .codex/agents/*.toml is now treated as user-authored and refused (actionable error, no data loss) on first refresh; new installs write the marker from the start. Tracked as a release-note/migration follow-up. Cherry-pick f126ae66 onto feat.'
created_at: "2026-07-15T00:00:00Z"
---

## Summary

t2b file-shaped CAS-aware projection (Codex/OpenCode/Copilot) plus a round-2 hardening pass closing four review defects. See summary frontmatter. Fixes committed as f126ae66 on the isolated worktree; state checkpoint 1237532e.
