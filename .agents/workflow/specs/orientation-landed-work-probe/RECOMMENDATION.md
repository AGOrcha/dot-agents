# Recommendation: MERGE into `full-loop-orchestration-runtime`

**Lead:** `.agents/active/ideation-inbox/obs-eligible-probe-landed-work-at-orientation.md`
(fold-back, `plan_id: full-loop-orchestration-runtime`, observed 2026-07-16 wave-1)
**Artifact id:** `orientation-landed-work-probe`
**kg-ideate result:** Phase 1 (kg-brief) → **MERGE** (no new spec/plan; no design.md / plan-draft.md).

## Decision

Do NOT create a competing spec+plan. The concern — `da workflow eligible` /
orient has no signal for a candidate whose `write_scope` already landed on
master, so it re-surfaces done work as fresh implementation — is already owned by
the **active** `full-loop-orchestration-runtime` plan. That plan's Goal is to
"select cross-plan waves through `da workflow slots` and `da workflow eligible`"
and its acceptance criteria include "drives eligible tasks across all active
plans without exceeding the slot budget" and "selection-time … write-scope
conflict guards remain effective." A landed-work probe is an **additive
eligibility annotation** on that exact selection surface plus a **consumption
policy** in the driver — a coherent extension of the runtime's mission, not a
separate concern. The observation was authored as a fold-back to this plan.

**Coordination flag (single-source-of-truth):** the Go accounting predicate
lands in `commands/workflow/eligible_accounting.go` / `plan_task.go`, which is the
implementation surface of the **also-active** `layered-pr-fanout` plan (that plan
owns eligibility/slot/dep accounting; `eligible_accounting.go` cites its
`design.md` §2.8/§3.4). Keep the probe predicate co-located with the other
eligibility predicates there and reference it from the runtime consumer — do not
duplicate. The runtime plan owns the *policy* (route landed work to reconcile
instead of fanout); the accounting file is shared code. Sequence T-a (Go probe)
before T-b (driver/skill consumption).

## Phase 1 — KG Briefing (grounding)

### KG Briefing: eligible/orient landed-work probe
Generated: 2026-07-22

**Prior Decisions (2 found)**
- `layered-pr-fanout/design.md` §2.8/§3.4.6 — eligibility & slot/dep accounting is
  centralized in `commands/workflow/eligible_accounting.go` (dep-satisfies,
  slot-holds, blocked-on predicates). The new landed-work predicate belongs in
  this same file, alongside `depSatisfiesDownstream`/`countsAgainstParallelTasks`.
- `full-loop-orchestration-runtime.plan.md` "da owns workflow state" — Main
  serializes canonical mutations; workers emit artifacts only. A landed-work
  reconcile must go through `da` (advance/closeout), never a hand edit.

**Research Findings (0 relevant)** — [none] (operational orchestration gap, not
literature-driven).

**Contradictions** — [adapter-absent] (`contradicting_claims` not needed).

**Applicable Lessons (6 found)**
- `reconcile-task-status-on-pr-merge` — **root cause**: a merged PR does not
  auto-advance task status; closeout must fire the moment the merge is observed.
  The observation explicitly asks to *escalate this lesson from PR-merge-time to
  orientation-time*. (Daemon gap tracked in `pr-merge-auto-reconcile.md`.)
- `verify-task-status-vs-pr-history` — pre-fanout, cross-check merged PRs vs task
  status because "TASKS.yaml lies after parallel waves"; this probe automates the
  write_scope half of that check.
- `stale-plan-status-vs-reality` — a task's status is a claim, not ground truth;
  diff stated artifacts against the repo before treating status as authoritative.
- `additive-state-fields` — extend `AnnotatedTask` with a NEW field; never reuse
  an existing one (keeps the JSON contract stable for the driver's parser).
- `tests-must-drive-the-production-path` — drive `runWorkflowEligible` / the
  driver selection, not just the predicate; mutation-verify the routing change.
- `symbol-only-references-in-artifacts` — reference code by symbol, never line no.

**Gaps (2 identified)**
- No task-level "scope already on master" signal exists. `detectRepoDrift`
  (`commands/workflow/drift.go`) surfaces only PLAN-level `CompletedPlanIDs`
  (completed-but-not-archived hygiene) — a different, complementary signal.
- `annotateEligibleTasks` already annotates evidence/conflict/scope-declared but
  performs no vcs probe; the driver (`n-plan-omp-driver`) fans out on eligible
  output with no landed-work gate.

**Prior Spec / Plan Overlap**
- `full-loop-orchestration-runtime` (active) — **merge target**; owns cross-plan
  eligible-wave selection + driver + reconcile swarm.
- `layered-pr-fanout` (active) — owns the `eligible_accounting.go` predicate
  surface; coordinate the Go change here (do not fork the accounting logic).

**Impact Radius (seeds write-scopes)**
- `commands/workflow/plan_task.go` — `AnnotatedTask` (embeds
  `workflowNextTaskSuggestion`), `annotateEligibleTasks`, `eligibleAnnotatedWithConflicts`,
  `renderEligibleTask`, `eligibleOutput`, `runWorkflowEligible`.
- `commands/workflow/eligible_accounting.go` — new probe predicate (sibling of
  `depSatisfiesDownstream`).
- Reuse: `stateRefTreeContains(projectPath, commit, relPaths) map[string]bool`
  (plan_task.go) — the existing batched `cat-file -e commit:relPath` primitive
  (one go-git open, error⇒absent); ideal for computing true/partial/false over a
  task's write_scope paths in a single spawn. `gitOutput` / `trackGitSpawn`
  (state.go) available for the merge-base resolution.
- `bin/tests/omp-full-loop`, `.agents/workflow/runtime/full-loop/reconcile.swarm.yaml`,
  `internal/scaffold/home/starter/skills/global/{orchestrator-session-start,isp}`
  — driver + orientation consumers.

## Drafted tasks to add to `full-loop-orchestration-runtime`

### Task A — `eligible-landed-work-probe`
- **Title:** Add a write_scope-vs-master landed-work probe to `da workflow eligible`
- **depends_on:** [] (Go-only; coordinate with layered-pr-fanout on the shared file)
- **app_type:** go-cli
- **write_scope:**
  - `commands/workflow/eligible_accounting.go`
  - `commands/workflow/eligible_accounting_test.go`
  - `commands/workflow/plan_task.go`
  - `commands/workflow/plan_task_test.go`
- **Behavior / change:** Add a deterministic read-only predicate that, per eligible
  candidate, resolves the current merge base (`master`, fall back to
  `origin/master`) and probes the candidate's `write_scope` paths via the existing
  batched `stateRefTreeContains` (no per-path subprocess fan-out). Reduce the
  per-path presence map to a task-level `scope_already_present`:
  `true` (every declared path resolves on master) | `partial` (some) | `false`
  (none, or no `write_scope` declared). Add it as a NEW additive field on
  `AnnotatedTask` (`scope_already_present` JSON tag), populate it inside
  `annotateEligibleTasks`, and surface it in `renderEligibleTask` (text) and the
  `eligibleOutput` JSON. Tasks are annotated, never filtered out. Any git error
  maps to `false`/absent (mirrors the existing error⇒absent tolerance); count the
  spawn via `trackGitSpawn`.
- **Acceptance:** `da workflow eligible --json` carries `scope_already_present`
  per task = `true` when all write_scope paths exist on the merge base, `partial`
  when a strict subset do, `false` when none/undeclared; the probe is read-only
  and adds ≤1 git spawn per candidate (batched); a fully-landed task still appears
  in the eligible list (signal only). Unit tests fixture a committed-on-master
  write_scope (true), a mixed set (partial), and an unlanded/undeclared set
  (false), asserting through `runWorkflowEligible`'s JSON — not the predicate in
  isolation.

### Task B — `route-landed-work-to-reconcile`
- **Title:** Route landed-work candidates to status-reconcile closeout instead of a fresh fanout bundle
- **depends_on:** [`eligible-landed-work-probe`]
- **app_type:** meta
- **write_scope:**
  - `bin/tests/omp-full-loop`
  - `bin/tests/omp-full-loop_test.sh`
  - `.agents/workflow/runtime/full-loop/reconcile.swarm.yaml`
  - `internal/scaffold/home/starter/skills/global/orchestrator-session-start/SKILL.md`
  - `internal/scaffold/home/starter/skills/global/orchestrator-session-start/instructions/eligible-orientation.md`
  - `internal/scaffold/home/starter/skills/global/isp/instructions/task-selection.md`
- **Behavior / change:** Make the outer-loop driver and orient/ISP task-selection
  consume `scope_already_present`. A `true` candidate is routed to a status-reconcile
  closeout (advance/closeout to `completed` through `da`, per
  `reconcile-task-status-on-pr-merge`) instead of being fanned out as a fresh
  implementation worker — saving a worker dispatch per stale task. A `partial`
  candidate is surfaced as a caution and NOT auto-dispatched (needs lead
  confirmation; may be genuinely resumable). Reconcile stays serialized in Main
  (workers emit artifacts only). Document the orientation-time behavior in the
  orchestrator + ISP skills as the escalation of the existing PR-merge-time lesson.
- **Acceptance:** Driver self-test (fake `omp-swarm` + fixture plan) with a pending
  task whose `write_scope` is fully committed on master routes it to
  reconcile-closeout (task ends `completed`, zero workers spawned for it); a
  partial-landed task is flagged, not auto-dispatched; both behaviors are asserted
  in `omp-full-loop_test.sh` and described in the orchestrator-session-start /
  isp skill instructions.

## 3-line grounded summary
1. **Brief found:** the eligible/orient surface (`runWorkflowEligible` →
   `annotateEligibleTasks` → `AnnotatedTask`/`eligibleOutput`) has evidence,
   conflict, and scope-declared annotations but no vcs landed-work signal, and a
   reusable batched `cat-file -e master:path` primitive (`stateRefTreeContains`)
   already exists; the root lesson is `reconcile-task-status-on-pr-merge`.
2. **Decision:** MERGE as two tasks into the active `full-loop-orchestration-runtime`
   plan (probe annotation → driver/skill consumption), coordinating the shared
   `eligible_accounting.go` change with the active `layered-pr-fanout` plan rather
   than forking a competing spec/plan.
3. **Key open question:** what canonical ref is authoritative for "landed" —
   local `master`, `origin/master`, or the configured work-tracking merge base —
   given the `stale-local-master-ref` / `stale-local-checkout-mass-drift` lessons
   (a stale local master would mis-signal); the probe must resolve against the
   same ref the reconcile lesson trusts.
