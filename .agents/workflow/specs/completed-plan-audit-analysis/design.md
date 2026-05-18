# Completed Workflow Bundle Audit Analysis

**Status:** analysis artifact for auditing live workflow bundles whose canonical `PLAN.yaml`
currently says `completed`, but whose surrounding markdown narratives, archived audits, or
follow-on notes suggest residual spec-vs-implementation drift.

**Written:** 2026-04-19

**Purpose:** define one canonical, risk-ordered audit method for the current set of completed
workflow bundles before any of them are treated as fully closed or archived.

**Current audit set:**

- [loop-agent-pipeline](../../plans/loop-agent-pipeline/PLAN.yaml)
- [ci-smoke-suite-hardening](../../plans/ci-smoke-suite-hardening/PLAN.yaml)
- [kg-command-surface-readiness](../../plans/kg-command-surface-readiness/PLAN.yaml)
- [error-message-compliance](../../plans/error-message-compliance/PLAN.yaml)

---

## 1. Why this analysis exists

The live workflow tree currently has multiple bundles whose canonical state is `completed`
in `PLAN.yaml`, while nearby evidence still signals incomplete, reopened, or only partially
validated work.

This is not automatically a contradiction. In some cases the canonical state may be correct
and the markdown narrative is merely stale. In other cases the bundle may have been closed
too early, with a follow-on note or archived audit already describing the remaining gap.

The repo therefore needs a consistent audit method that answers:

1. does the shipped implementation satisfy the bundle's spec and task-level done criteria
2. is the bundle's historical evidence trustworthy enough to justify the closeout
3. should the bundle remain completed, be reconciled, or be reopened into a follow-on plan

---

## 2. Audit goals

Each bundle audit should answer five questions in this order:

1. **Canonical state:** what does `PLAN.yaml` claim is complete
2. **Spec contract:** what exact behaviors, invariants, and verification strategy does the
   linked spec or contract require
3. **Task truth:** do the completed tasks in `TASKS.yaml` correspond to shipped behavior, not
   just landed artifacts or prose
4. **Evidence quality:** do merge-back archives, impl-results, CI artifacts, tests, and docs
   provide enough proof to trust the completion state
5. **Disposition:** keep completed, reconcile docs/status drift, or reopen/fork follow-on work

This analysis is intentionally stricter than a normal plan read-through. It is a
spec-vs-implementation audit, not a summary of what the plan says happened.

---

## 3. Evidence precedence

Use evidence in this order. Lower-precedence sources may signal drift, but should not override
higher-precedence sources without concrete proof.

1. **Canonical status and success criteria**
   - `PLAN.yaml`
   - `TASKS.yaml`
2. **Canonical spec / contract**
   - linked `design.md`
   - linked product docs or contracts
3. **Direct implementation evidence**
   - code, tests, workflows, prompt surfaces, schemas
4. **Verification evidence**
   - focused tests
   - CI workflows
   - impl-results artifacts
   - archived merge-back artifacts
5. **Narrative / historical context**
   - `*.plan.md`
   - reconciliation notes
   - archived audits and handoffs

Interpretation rule:

- `PLAN.yaml` is the canonical status source.
- `*.plan.md` status text is not authoritative, but it is a strong drift signal.
- Archived audits that explicitly call a task partial or over-claimed are stronger than stale
  markdown and should be treated as real contradiction evidence until disproven.

---

## 4. Audit output shape

Each completed bundle audit should produce the same output sections:

1. **Verdict**
   - `verified-complete`
   - `completed-with-doc-drift`
   - `completed-with-evidence-gaps`
   - `reopen-recommended`
2. **Spec anchors**
3. **Implementation anchors**
4. **Verification anchors**
5. **Confirmed drift points**
6. **Open questions**
7. **Required follow-up**

If reopened work is needed, separate:

- **status drift only** from
- **behavioral drift** from
- **evidence/provenance drift**

Those are different problems and should not be collapsed into one verdict.

---

## 5. Risk-ordered audit queue

The current completed bundles should be audited in this order:

1. `loop-agent-pipeline`
2. `ci-smoke-suite-hardening`
3. `kg-command-surface-readiness`
4. `error-message-compliance`

This ordering is based on two factors:

- direct evidence that the bundle may have been marked complete too early
- blast radius of failure if downstream workflow or planning surfaces trust the bundle blindly

---

## 6. Bundle analyses

### 6.1 `loop-agent-pipeline`

**Why first**

This is the highest-risk completed bundle because it sits on the control plane for staged
delegation, closeout, review gating, and plan-scoped completion. Its own canonical summary
still embeds unimplemented follow-on behavior, and its archived implementation audit already
reported one task as partial and one as not actually complete.

**Canonical anchors**

- [PLAN.yaml](../../plans/loop-agent-pipeline/PLAN.yaml)
- [TASKS.yaml](../../plans/loop-agent-pipeline/TASKS.yaml)

**Spec anchors**

- [docs/LOOP_ORCHESTRATION_SPEC.md](../../../../docs/LOOP_ORCHESTRATION_SPEC.md)
- [loop-agent-pipeline decisions.1.md](../loop-agent-pipeline/decisions.1.md)

**Implementation / evidence anchors**

- [loop-agent-pipeline.plan.md](../../plans/loop-agent-pipeline/loop-agent-pipeline.plan.md)
- [loop-agent-pipeline-implementation-audit.md](../../../history/loop-agent-pipeline/loop-agent-pipeline-implementation-audit.md)
- [delegate-merge-back-archive](../../../history/loop-agent-pipeline/delegate-merge-back-archive)

**Known drift hypotheses**

1. `p7-post-closeout` may be canonically completed but only partially implemented in real
   runtime behavior.
2. `p11-plan-completion-mode` may have shipped the scoped completion surface while still
   missing the replacement-agent retry behavior described in its own done criteria.
3. `PLAN.yaml` may be using a completed status even though its summary still acknowledges
   remaining runtime gaps.
4. Merge-back provenance for several tasks may be too noisy to support a high-confidence
   closeout.

**Audit procedure**

1. Validate that post-task orchestrator review behavior matches the spec's expected
   judgment and pause semantics, not just artifact-presence gating.
2. Validate that scoped completion uses deterministic plan scoping and respects lock/pause
   states.
3. Validate whether replacement-agent fallback for usage/rate-limit failures exists in real
   runtime behavior or only in notes and environment knobs.
4. Reconcile `TASKS.yaml` done means against the archived implementation audit and current
   shell/runtime code.
5. Classify archive noise separately from behavioral incompleteness.

**Canonical follow-on tasks**

- `p12-review-gate-hardening` — replace heuristic auto-accept with real post-task
  orchestrator review application
- `p13-replacement-worker-retry` — add resumable stage retry with fallback runtime selection
  after terminal provider failures

**Reopen trigger**

Recommend reopen if any of these are true:

- post-task review is still heuristic rather than a real orchestrator judgment pass
- replacement-agent retry remains unimplemented despite being part of completed-task done means
- a task marked completed is still explicitly partial in current runtime behavior

---

### 6.2 `ci-smoke-suite-hardening`

**Why second**

This bundle directly controls confidence in repo-wide CI. It is marked completed, but also
has a live reconciliation note still marked `In Progress`, which is a strong signal that the
bundle needs verification against current workflow semantics rather than trusting status alone.

**Canonical anchors**

- [PLAN.yaml](../../plans/ci-smoke-suite-hardening/PLAN.yaml)
- [TASKS.yaml](../../plans/ci-smoke-suite-hardening/TASKS.yaml)

**Spec anchors**

- [docs/LOOP_ORCHESTRATION_SPEC.md](../../../../docs/LOOP_ORCHESTRATION_SPEC.md)
- [docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md](../../../../docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md)

**Implementation / evidence anchors**

- [ci-smoke-suite-hardening.plan.md](../../plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening.plan.md)
- [ci-smoke-suite-hardening-reconcile.plan.md](../../plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md)
- [.github/workflows/test.yml](../../../../.github/workflows/test.yml)
- [.github/workflows/auto-release.yml](../../../../.github/workflows/auto-release.yml)
- [.github/workflows/heavy-integration.yml](../../../../.github/workflows/heavy-integration.yml)
- [docs/CI_HEAVY_INTEGRATION_LANE.md](../../../../docs/CI_HEAVY_INTEGRATION_LANE.md)

**Known drift hypotheses**

1. The original completion may have predated stricter requirements around built-binary parity,
   scoped completion semantics, and testing-matrix traceability.
2. The implementation may now satisfy most requirements, but the bundle may still carry stale
   plan/task history that was never fully reconciled.
3. Traceability against `.agents/workflow/testing-matrix.yaml` may be comment-based rather than
   backed by a canonical artifact, which is acceptable only if the docs are explicit.

**Audit procedure**

1. Compare `test.yml` and `auto-release.yml` for built-binary parity and `go test` parity.
2. Verify every smoke lane actually runs under isolated `HOME` and `AGENTS_HOME`.
3. Verify the scoped completion smoke exists and matches the orchestration spec's probe shape.
4. Verify heavy integration is intentionally separated and documented.
5. Decide whether the reconciliation note reflects stale history or a still-open gap.

**Reopen trigger**

Recommend reopen if any of these are true:

- PR and release workflows still test materially different binaries or test flags
- scoped completion semantics are not actually covered
- the reconciliation note still describes real unmet targets rather than stale state

---

### 6.3 `kg-command-surface-readiness`

**Why third**

This bundle affects planner evidence quality, graph-backed commands, and MCP consumers, but
its current contradiction signals are weaker than the first two bundles. The main risk is not
obvious early closeout; it is operational drift in environment-sensitive graph behavior.

**Canonical anchors**

- [PLAN.yaml](../../plans/kg-command-surface-readiness/PLAN.yaml)
- [TASKS.yaml](../../plans/kg-command-surface-readiness/TASKS.yaml)

**Spec anchors**

- [kg-command-surface-readiness design.md](../kg-command-surface-readiness/design.md)
- [graph-bridge-command-readiness](../../plans/graph-bridge-command-readiness/PLAN.yaml)

**Implementation / evidence anchors**

- [kg-command-surface-readiness.plan.md](../../plans/kg-command-surface-readiness/kg-command-surface-readiness.plan.md)
- [impl-results.md](../../../history/kg-command-surface-readiness/impl-results.md)
- [delegate-merge-back-archive](../../../history/kg-command-surface-readiness/delegate-merge-back-archive)
- docs/research artifacts referenced from `TASKS.yaml`

**Known drift hypotheses**

1. Clean-checkout behavior may still differ from live-repo behavior, especially around graph
   freshness, lock/busy states, and code-status trustworthiness.
2. The completed status may be correct while the markdown plan simply remained `active`.
3. MCP parity or advanced-surface decisions may be documented but insufficiently verified under
   realistic fixtures.

**Audit procedure**

1. Re-run the freshness contract from clean state and compare to task notes.
2. Re-run `kg changes` and `kg impact` with stale/unbuilt and fresh-graph scenarios.
3. Confirm help-text and docs match the agent-ready vs expert-only decisions for advanced
   surfaces.
4. Confirm only the approved MCP parity changes landed and external tool names did not drift.

**Reopen trigger**

Recommend reopen if any of these are true:

- stale/unbuilt graph still looks indistinguishable from no-impact in command behavior
- code-status does not behave as the authoritative readiness probe
- MCP parity decisions are documented but not actually enforced in implementation

---

### 6.4 `error-message-compliance`

**Why fourth**

This bundle has the clearest contract and the lowest immediate control-plane risk. The most
likely outcome is "completed with doc drift" unless spot checks show later regressions in
prioritized command families.

**Canonical anchors**

- [PLAN.yaml](../../plans/error-message-compliance/PLAN.yaml)
- [TASKS.yaml](../../plans/error-message-compliance/TASKS.yaml)

**Spec anchors**

- [docs/ERROR_MESSAGE_CONTRACT.md](../../../../docs/ERROR_MESSAGE_CONTRACT.md)
- [docs/GLOBAL_FLAG_CONTRACT.md](../../../../docs/GLOBAL_FLAG_CONTRACT.md)

**Implementation / evidence anchors**

- [error-message-compliance.plan.md](../../plans/error-message-compliance/error-message-compliance.plan.md)
- [impl-results.md](../../../history/error-message-compliance/impl-results.md)
- [docs/research/error-message-inventory.md](../../../../docs/research/error-message-inventory.md)

**Known drift hypotheses**

1. The bundle may be functionally complete even though the markdown plan still says `Proposed`.
2. Residual drift is most likely in the exact command families called out by the inventory:
   `commands/kg/*`, `commands/agents/*`, and setup-validation paths.
3. Later code changes may have reintroduced hand-authored usage strings or non-enumerated
   finite-domain errors outside the originally normalized paths.

**Audit procedure**

1. Spot-check the priority command families named in the inventory and task notes.
2. Confirm the helper decision order in the contract still matches live command behavior.
3. Confirm regression coverage exists for root parse, finite-domain validation, and recovery
   hints.
4. Separate intentional human-first failure rendering from true contract drift.

**Reopen trigger**

Recommend reopen if any of these are true:

- user-correctable failures in priority command families still bypass the shared CLI error path
- finite-domain validation still omits valid values in representative commands
- later code changes visibly regressed the contract after the bundle was marked completed

---

## 7. Cross-cutting drift classes

The audits should classify findings into these buckets:

### A. Status drift

Examples:

- `PLAN.yaml` says `completed`, but `*.plan.md` still says `Active` or `Proposed`
- a reconciliation note remains live after completion

This usually requires doc reconciliation, not necessarily plan reopening.

### B. Behavioral drift

Examples:

- done criteria were accepted, but runtime behavior still misses core requirements
- a feature exists as a surface or schema but not as working control-plane behavior

This is the strongest reopen signal.

### C. Verification drift

Examples:

- tests do not cover the exact success criteria
- CI or smoke verification only partially exercises the promised behavior

This may require follow-on verification work even if implementation is mostly correct.

### D. Provenance drift

Examples:

- merge-back artifacts contain dirty-state noise
- archive evidence is too weak to support a high-confidence closeout

This does not automatically invalidate the implementation, but it weakens trust in the
historical record and may justify archive cleanup or a provenance-specific follow-up.

---

## 8. Recommended next step after this analysis

Run the first full spec-vs-implementation audit against `loop-agent-pipeline` using this
document as the playbook, then fold back the result into one of:

- a completed verification memo if the bundle is truly done
- a reconciliation note if the main gap is status/doc drift
- a follow-on canonical plan or reopened task if behavioral drift is confirmed

Do not audit the remaining bundles in parallel until the first audit calibrates the standard
for what counts as "verified complete" versus "completed with residual drift."
