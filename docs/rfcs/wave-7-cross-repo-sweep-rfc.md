# RFC: Wave 7 — Cross-Repo Sweep and Drift

**Date:** 2026-04-11
**Status:** Accepted — resolves design questions, implementation may begin
**Depends on:** Wave 6 (delegation + merge-back complete), stable managed-project inventory

---

## Problem Statement

Users operate multiple repos, and the same workflow drift recurs across them without visibility: stale proposal queues, missing checkpoints, inconsistent hook rollout, diverged preferences. There is no mechanism to surface or fix this across repos without manual inspection.

---

## Design Decisions

### 1. Project Scope: Managed Inventory Only

**Decision:** Only projects registered in `~/.agents/config.json` (via `config.Load().Projects`) are sweep targets. No discovery, no path scanning.

**Rationale:** Prevents unintended access to repos the user hasn't explicitly registered. Uses the existing managed-project inventory rather than adding a new discovery mechanism.

---

### 2. Staleness Thresholds

**Decision:**
- Checkpoint staleness: 7 days (same as existing checkpoint retention guidance)
- Proposal staleness: 30 days (proposals older than 30 days without action are stale)

These are hard-coded defaults, overridable via `--stale-days` flag on the drift command.

---

### 3. Sweep Mutations: Confirmation-Gated, Not Proposal-Gated

**Decision:** Sweep mutations (scaffold hooks, create plan structure, clear stale proposals) go through interactive confirmation prompts (or `--yes` flag), NOT through the proposal review queue.

**Rationale:** Sweep fixes are operational, not shared-preference changes. Adding them to the proposal queue creates a feedback loop (sweep proposes a fix, the proposal becomes part of what's swept). The proposal queue is for shared configuration changes that affect other agents/users.

**Exception:** If a sweep action would modify shared preferences (`preferences.yaml` with `scope: shared`), it must go through proposal review.

---

### 4. Aggregation: Both Per-Repo and Aggregate

**Decision:** `workflow drift` produces:
- `[]RepoDriftReport` (per-repo) — printed per repo in tabular format
- `AggregateDriftReport` — summary counts + top warnings
- Written to `~/.agents/context/drift-report.json` for machine consumption

---

### 5. Default Behavior: Read-Only

**Decision:** `workflow drift` is always read-only. `workflow sweep` defaults to `--dry-run`. Mutations require explicit `--apply`. No background execution — all operations are on-demand.

---

## Acceptance Criteria

1. ✅ Only managed projects are checked — no path scanning
2. ✅ Default drift command is read-only
3. ✅ Sweep mutations require `--apply` (not default)
4. ✅ Threshold defaults: 7 days checkpoint, 30 days proposals
5. ✅ Sweep mutations use confirmation prompts (not proposal queue) except for shared preference changes
6. ✅ Drift results written to `~/.agents/context/drift-report.json`

---

## Blocking Risks

1. **Path no longer exists** — managed project paths may have been deleted or moved. `detectRepoDrift` must handle gracefully (mark as `unreachable` with warning).

2. **Sweep idempotency** — running sweep twice should be safe. Scaffold operations must check before writing; clearing stale proposals must not clear proposals that were recently created.

3. **Large project inventories** — sweep is sequential (no concurrency). For >20 projects this may be slow, but simplicity wins over premature parallelism.

---

## Implementation Order

1. Step 1: `loadManagedProjects` + `ManagedProject` struct
2. Step 2: `RepoDriftReport` + `detectRepoDrift`
3. Step 3: `AggregateDriftReport` + `aggregateDrift`
4. Step 4: `workflow drift` subcommand
5. Step 5: `SweepAction` enum + `SweepPlan` + `planSweep`
6. Step 6: `workflow sweep` subcommand + sweep log
7. Step 7: status integration (drift warning in current project)
