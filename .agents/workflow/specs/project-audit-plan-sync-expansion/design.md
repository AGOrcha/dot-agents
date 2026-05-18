# Project Audit + Plan-Sync Expansion

**Status:** analysis artifact for extending the completed-bundle audit + canonical plan-sync
workflow to additional project areas after the first wave of audits.

**Written:** 2026-04-21

**Purpose:** define a structured second-wave audit program so the repo can keep reconciling
spec, implementation, evidence, and canonical workflow state without treating every completed
plan as the same kind of problem.

---

## 1. Report structure

Before starting each new area, the report for that area should answer the same seven things:

1. **Cohort**
   - carry-over audit
   - dependency-coupled completed bundle
   - doc/status drift bundle
   - metadata-repair bundle
2. **Canonical anchors**
   - `PLAN.yaml`
   - `TASKS.yaml`
3. **Spec / contract anchors**
   - `design.md`
   - product docs / contracts
4. **Implementation anchors**
   - code
   - tests
   - prompts / skills / workflows
5. **Drift signals**
   - stale markdown status
   - reopened dependency
   - active fold-back
   - weak verification
   - underspecified canonical metadata
6. **Expected sync action**
   - keep completed
   - rewrite stale note
   - reopen narrowly with follow-on task(s)
   - repair plan metadata
7. **Output artifact**
   - completed-bundle audit memo under `.agents/history/<plan>/`
   - if needed, canonical `PLAN.yaml` / `TASKS.yaml` sync immediately after the audit

This keeps the work comparable across bundles and avoids mixing implementation drift with simple
documentation cleanup.

---

## 2. Current expansion inventory

The remaining relevant canonical plan areas currently look like this:

- `error-message-compliance`
  - `PLAN.yaml`: `completed`
  - markdown status: `Proposed`
  - already in first-wave audit scope, but not yet closed out with a completed-bundle audit memo
- `graph-bridge-command-readiness`
  - `PLAN.yaml`: `completed`
  - markdown status: `Active`
  - directly coupled to reopened KG / planner graph trustworthiness
- `planner-evidence-backed-write-scope`
  - `PLAN.yaml`: `completed`
  - no obvious markdown drift
  - newly completed and downstream of graph/planner surfaces
- `workflow-parallel-orchestration`
  - `PLAN.yaml`: `completed`
  - no markdown narrative in the bundle
  - newly completed control-plane surface
- `ralph-fanout-and-runtime-overrides`
  - `PLAN.yaml`: `completed`
  - no markdown narrative in the bundle
  - newly completed Ralph/runtime control-plane surface
- `plugin-resource-salvage`
  - `PLAN.yaml`: `completed`
  - markdown status: `In Progress`
- `platform-dir-unification`
  - `PLAN.yaml`: `completed`
  - markdown status: `Completed`
  - lower drift signal than the others
- `test-archive-p2`
  - `PLAN.yaml`: `completed`
  - empty summary / success criteria / verification strategy
  - `TASKS.yaml` is empty

---

## 3. Expansion cohorts

### 3.1 Carry-over audits from wave 1

These are already inside the original audit method and should be finished before the repo
declares the broader program complete.

- `error-message-compliance`

Why this is a carry-over:

- it was in the original completed-bundle audit analysis
- it still has obvious status drift (`Proposed` markdown vs `completed` canonical plan)
- it has a concrete contract doc (`docs/ERROR_MESSAGE_CONTRACT.md`)

Expected output:

- full completed-bundle audit memo
- likely either `completed-with-doc-drift` or narrow reopen if representative command families
  still violate the contract

---

### 3.2 Dependency-coupled completed bundles

These sit on paths that feed other active or recently reopened surfaces. Even if they are
completed, downstream trust depends on them being genuinely settled.

#### `graph-bridge-command-readiness`

Why this is high priority:

- canonical plan says `completed`, but markdown still says `Active`
- it is the near-term dependency for graph-backed planner trust
- later specs already describe it as the prerequisite for planner evidence and bridge-backed
  command stability

Canonical anchors:

- `.agents/workflow/plans/graph-bridge-command-readiness/PLAN.yaml`
- `.agents/workflow/plans/graph-bridge-command-readiness/TASKS.yaml`

Spec anchors:

- `.agents/workflow/specs/graph-bridge-contract/design.md`
- `.agents/workflow/specs/kg-command-surface-readiness/design.md`
- `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`

Likely drift points:

- bridge query reliability may be over-claimed relative to later planner expectations
- downstream plans may have absorbed compensating logic, masking whether bridge readiness itself
  is truly settled
- old active markdown may simply be stale, but that must be proven against current query behavior

Expected sync action:

- likely audit first, then either doc reconciliation or narrow reopen if bridge-backed query
  trust is still softer than planner specs assume

#### `workflow-parallel-orchestration`

Why this is high priority:

- newly completed control-plane surface with no narrative reconciliation layer
- directly changes orchestration semantics (`workflow eligible`, conflict detection,
  max-parallel ceiling, ISP fanout path)
- blast radius is high if completion was claimed from tests alone without end-to-end fanout
  confidence

Canonical anchors:

- `.agents/workflow/plans/workflow-parallel-orchestration/PLAN.yaml`
- `.agents/workflow/plans/workflow-parallel-orchestration/TASKS.yaml`

Spec anchors:

- `.agents/workflow/specs/workflow-parallel-orchestration/design.md`
- `.agents/prompts/isp.prompt.md`
- `.agents/skills/orchestrator-session-start/`
- `.agents/skills/plan-wave-picker/`

Likely drift points:

- `workflow eligible` may be correct in unit tests but still thin in live multi-plan orchestration
- ISP / skill prompt readbacks may lag the actual command semantics
- parallelism ceiling and conflict annotations may exist in code but not yet be trustworthy enough
  for genuine multi-task fanout decisions

Expected sync action:

- completed-bundle audit, then either keep completed or reopen narrowly around live fanout
  semantics if prompt/runtime behavior diverges from command behavior

#### `ralph-fanout-and-runtime-overrides`

Why this is high priority:

- recent completion on the same control-plane neighborhood as `loop-agent-pipeline` and
  `workflow-parallel-orchestration`
- override precedence and overlap protection are the kind of features that often look complete in
  focused tests while still drifting in integrated runtime behavior

Canonical anchors:

- `.agents/workflow/plans/ralph-fanout-and-runtime-overrides/PLAN.yaml`
- `.agents/workflow/plans/ralph-fanout-and-runtime-overrides/TASKS.yaml`

Spec / runtime anchors:

- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-pipeline`
- `bin/tests/ralph-worker`
- `bin/tests/ralph-closeout`

Likely drift points:

- per-role override precedence may be correct in isolated tests but ambiguous when multiple env
  sources are present
- overlap detection may cover active delegation conflicts but still miss same-pass or scoped-mode
  edge cases
- cross-plan / parallel-orchestration changes may have introduced follow-on risk after the bundle
  was marked complete

Expected sync action:

- likely audit as a runtime-behavior bundle rather than a doc-only cleanup bundle

#### `planner-evidence-backed-write-scope`

Why this is coupled:

- it is newly completed and depends directly on trustworthy graph-backed evidence and fanout
  behavior
- it may still be semantically complete, but its claims should be checked against the now-reopened
  KG / graph trust surfaces

Canonical anchors:

- `.agents/workflow/plans/planner-evidence-backed-write-scope/PLAN.yaml`
- `.agents/workflow/plans/planner-evidence-backed-write-scope/TASKS.yaml`

Spec anchors:

- `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`
- `.agents/workflow/specs/workflow-parallel-orchestration/design.md`

Likely drift points:

- sidecar derivation may be complete as a command surface but not yet trustworthy enough as a
  planning contract if graph inputs remain environment-sensitive
- fanout warnings and evidence confidence may be wired in command output without being fully
  consumed in orchestrator flows
- manual validation requirements in the plan may have been only partially satisfied

Expected sync action:

- likely audit after `graph-bridge-command-readiness` and the workflow control-plane bundles, so
  the dependency picture is settled first

---

### 3.3 Status-drift and doc-sync bundles

These have clearer signs of stale narrative state, but lower immediate runtime blast radius than
the control-plane and graph bundles above.

#### `plugin-resource-salvage`

Why it belongs here:

- canonical plan is `completed`
- markdown plan still says `In Progress`
- it has substantial closeout lineage in merge-back artifacts, which may be enough to reconcile
  without reopening unless plugin readback / emit paths are still behaviorally soft

Canonical anchors:

- `.agents/workflow/plans/plugin-resource-salvage/PLAN.yaml`
- `.agents/workflow/plans/plugin-resource-salvage/TASKS.yaml`

Spec / doc anchors:

- `docs/PLUGIN_CONTRACT.md`
- `docs/SCHEMA_FOLLOWUPS.md`

Likely drift points:

- phase-5 closeout may have updated docs and plan state without enough current-product verification
- plugin readback and import paths may be landed, but stage-2 follow-ons may still be implicitly
  bundled into the completed plan narrative

Expected sync action:

- likely `completed-with-doc-drift` unless live plugin command behavior shows missing contract work

#### `platform-dir-unification`

Why it is lower priority:

- canonical and markdown state are aligned as `Completed`
- visible drift signals are weak compared with other bundles

Canonical anchors:

- `.agents/workflow/plans/platform-dir-unification/PLAN.yaml`
- `.agents/workflow/plans/platform-dir-unification/TASKS.yaml`

Spec / doc anchors:

- `docs/PLATFORM_DIRS_DOCS.md`
- `docs/CANONICAL_HOOKS_DESIGN.md`
- related RFC / follow-up docs in `docs/rfcs/`

Likely drift points:

- summary may over-compress which phases were actually completed vs deferred into later work
- archived lineage may still refer to earlier active markdown as source of truth

Expected sync action:

- spot-check audit or metadata/doc sync, not likely an immediate reopen candidate

---

### 3.4 Metadata-repair bundles

These are not normal spec-vs-implementation audits first. They need canonical metadata repair so
the repo can even reason about them coherently.

#### `test-archive-p2`

Why it is special:

- `PLAN.yaml` is `completed`
- summary, success criteria, and verification strategy are empty
- `TASKS.yaml` contains no tasks

This is not trustworthy enough to treat as a normal completed implementation bundle. The first
question is not "is the code complete?" but "what canonical artifact is this bundle even supposed
to represent?"

Expected sync action:

- metadata/lineage audit first
- then either:
  - repair the canonical plan metadata from history, or
  - archive/remove the bundle if it is only a placeholder and not a real executable plan

---

## 4. Recommended audit order

Use this order for the next wave:

1. `error-message-compliance`
   - finish the carry-over audit set cleanly
2. `graph-bridge-command-readiness`
   - highest dependency value for planner + graph trust
3. `workflow-parallel-orchestration`
   - high blast-radius control-plane surface
4. `ralph-fanout-and-runtime-overrides`
   - coupled Ralph runtime control-plane surface
5. `planner-evidence-backed-write-scope`
   - depends on graph + fanout trust
6. `plugin-resource-salvage`
   - likely doc/status drift, lower runtime blast radius
7. `test-archive-p2`
   - metadata repair / lineage clarification
8. `platform-dir-unification`
   - low visible drift, likely spot-check only

Rationale:

- finish the original audit queue before expanding indefinitely
- settle the graph / orchestration dependency spine next
- defer lower-risk doc-sync and metadata-only bundles until the higher-blast-radius control-plane
  bundles are understood

---

## 5. Plan-sync rules for wave 2

After each audit, sync canonical state immediately instead of leaving contradictions around.

### If verdict is `verified-complete`

- keep `PLAN.yaml` completed
- rewrite or archive stale narrative notes
- record the audit memo in `.agents/history/<plan>/`

### If verdict is `completed-with-doc-drift`

- keep `PLAN.yaml` completed
- rewrite stale plan/reconcile notes so they stop signaling live incomplete work
- update docs only; do not invent follow-on tasks unless behavior is actually missing

### If verdict is `completed-with-evidence-gaps`

- decide whether the repo is willing to carry the gap temporarily
- if yes, record the evidence gap explicitly in the audit memo
- if not, reopen narrowly instead of leaving the bundle "soft complete"

### If verdict is `reopen-recommended`

- add narrow follow-on task(s) in canonical `TASKS.yaml`
- update `PLAN.yaml` status / focus to the reopened scope
- reroute any active fold-backs to the new canonical follow-on task(s)
- update the audit memo so it records the reopen trigger and the new narrow scope

### If the bundle is metadata-incoherent

- do metadata repair before any implementation judgment
- fill summary / success criteria / verification strategy from durable history if possible
- if no durable history exists, mark the bundle for archive or explicit replacement rather than
  pretending it is a normal completed plan

---

## 6. Output conventions

For each new audit, produce:

- `.agents/history/<plan-id>/<plan-id>-completed-bundle-audit-YYYY-MM-DD.md`

And if the audit triggers sync work, update in the same pass:

- `.agents/workflow/plans/<plan-id>/PLAN.yaml`
- `.agents/workflow/plans/<plan-id>/TASKS.yaml`
- stale `*.plan.md` or reconciliation note in that bundle
- relevant fold-back under `.agents/active/fold-back/` when the follow-on scope changes

This keeps the audit program durable and prevents the repo from collecting a second layer of
"audit notes that themselves drift from canonical plan state."
