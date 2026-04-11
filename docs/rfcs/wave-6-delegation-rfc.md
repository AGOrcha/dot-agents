# RFC: Wave 6 — Delegation and Merge-Back

**Date:** 2026-04-10
**Status:** Draft — required before any Wave 6 implementation
**Depends on:** Wave 5 (graph bridge stable), Wave 2 (canonical plan/task artifacts)

---

## Problem Statement

When a parent agent delegates a task to a sub-agent, there is no canonical mechanism to:
1. Declare which files the sub-agent is permitted to write (write scope)
2. Detect overlapping write scopes between concurrent delegations
3. Produce a structured return artifact that the parent can consume without re-reading all changed files

Ad-hoc delegation (via freeform conversation) creates write-scope conflicts, missed merge integrations, and invisible delegation state. This RFC resolves the design questions required before implementing Wave 6.

---

## Design Decisions

### 1. Concurrency Model: Reservation-Based

**Decision:** Reservation-based, not lock-based.

**Rationale:**
- Claude Code sessions are not long-running processes — there is no lock holder to release a lock on crash.
- Lock-based models require a lock server or filesystem lock (both fragile in agent contexts).
- Reservation-based: a delegation contract *declares* write scope. Overlap detection runs at creation time. If two delegations overlap, the second is rejected at creation, not at first write.

**Implication:** The reservation is a contract, not a runtime enforcement. Agents are expected to honor write scope. Lint (or orient) can detect violations after the fact.

**Alternatives rejected:**
- Filesystem locks (fragile, no expiry)
- Optimistic concurrency (allows conflicts, requires merge)
- Sequential queuing (too restrictive for genuinely independent tasks)

---

### 2. Conflict Detection Strategy: Glob Pattern Overlap

**Decision:** Use `filepath.Match`-style glob patterns for write scope. Detect overlap at delegation creation.

**Overlap algorithm:**
- Two delegations overlap if any file path that matches pattern A also matches pattern B
- Check is bidirectional: `A ⊆ B || B ⊆ A || A ∩ B ≠ ∅`
- For glob patterns, exact containment is not decidable in general — use a set of representative test paths derived from the patterns (listing actual files under each pattern, if present)
- If representative set overlap is detected, reject the second delegation

**Practical simplification:** In the common case, write scopes are disjoint directory trees (e.g., `commands/` vs `internal/config/`). String-prefix matching on non-wildcard patterns handles 90% of cases. Full glob overlap for wildcard patterns is deferred to a later implementation.

**Acceptance criteria:**
- `commands/` and `commands/workflow.go` overlap (prefix containment)
- `commands/*.go` and `internal/**` do not overlap
- Two delegations with identical scope are always detected as conflicting

---

### 3. Interaction with Wave 2 Plan/Task Artifacts

**Decision:** Delegation contracts reference canonical plan/task IDs from Wave 2. A delegation can only be created for a task that exists in a canonical plan and is in `pending` or `in_progress` status.

**Why:** This prevents creating phantom delegations for nonexistent work. The canonical plan is the authoritative source of truth for what work exists.

**Fanout flow:**
1. Parent agent loads canonical plan → identifies task to delegate
2. Runs `workflow fanout --plan <plan-id> --task <task-id> --write-scope <globs>`
3. System validates: plan exists, task exists, task not already delegated, no write-scope overlap
4. Creates `.agents/active/delegation/<task-id>.yaml`
5. Advances task status to `in_progress` in canonical plan (Wave 2 advance operation)

**Merge-back flow:**
1. Sub-agent completes work
2. Runs `workflow merge-back --task <task-id> --summary "..." --verification-status pass`
3. System: creates `.agents/active/merge-back/<task-id>.md`, updates delegation status to `completed`
4. Parent agent reads merge-back artifact; decides whether to advance task to `completed` in canonical plan

**Key separation:** The merge-back artifact is produced by the delegate and consumed by the parent. The parent — not the system — decides whether the work is acceptable.

---

### 4. Coordination Intents: No Transport Syntax

**Decision:** Coordination intents (`status_request`, `review_request`, `escalation_notice`, `ack`) are stored as enum fields in the delegation contract, never as chat syntax or `@mention` markers.

**Why:** Chat syntax in canonical storage creates a tight coupling to a specific agent communication protocol. When protocols change (from Claude Code to a different agent runtime), the canonical storage should not need updating.

**Implication:** Agents check the `pending_intent` field on delegation contracts as part of their orient cycle. The intent is cleared (set to empty) once acknowledged.

---

### 5. RFC Acceptance Criteria

Wave 6 implementation may begin only after this RFC is acknowledged and the following are confirmed:

1. ✅ Reservation-based concurrency (no locks)
2. ✅ Glob-based overlap detection (prefix-first, full glob deferred)
3. ✅ Delegation requires canonical plan/task reference
4. ✅ Coordination intents as enum fields, no chat syntax
5. ✅ Merge-back is produced by delegate, consumed by parent — system does not auto-close tasks

---

## Blocking Risks

1. **Glob overlap is undecidable in general** — the practical simplification (prefix matching) covers common cases but misses adversarial or complex wildcard overlaps. Accept this limitation; document it.

2. **Sub-agent ignores write scope** — the system cannot prevent a sub-agent from writing outside its declared scope. Lint can detect violations post-hoc. This is a social contract, not a technical enforcement.

3. **Orphaned delegations** — if a sub-agent session ends without merge-back, the delegation contract stays active indefinitely. `workflow status` should surface orphaned delegations (no activity for > N hours) as warnings.

---

## Implementation Order (once RFC accepted)

1. Step 1: `DelegationContract` types + CRUD
2. Step 2: Write-scope overlap detection
3. Step 3: `MergeBackSummary` types + CRUD
4. Step 4: `CoordinationIntent` enum + lifecycle
5. Step 5: `workflow fanout` subcommand
6. Step 6: `workflow merge-back` subcommand
7. Step 7: Orient/status integration

**Estimated effort:** 1-2 implementation sessions (~4-6 hours total code + tests).

---

## Status

**Awaiting acknowledgment.** Tag this RFC as `accepted` and set Wave 6 plan status to `Ready for implementation` before starting Step 1.
