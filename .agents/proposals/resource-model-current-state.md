# Resource Model: `.agents/` System — Current State

> Captured: 2026-04-20. Use as the baseline domain model for the plan-archive-command proposal.

---

## 1. Top-Level Directory Map

```
.agents/
├── active/              ← TRANSIENT (live workflow I/O)
│   ├── active.loop.md           [worker loop template]
│   ├── orchestrator.loop.md     [orchestrator loop template]
│   ├── loop-state.md            [current loop context]
│   ├── isp-prompt-orchestrator.plan.md  ← STALE (no matching plan dir)
│   ├── delegation/              [live contracts — currently empty]
│   ├── delegation-bundles/      [live bundles  — currently empty]
│   ├── merge-back/              [live merge-backs — currently empty]
│   ├── verification/            [per-task verification artifacts]
│   │   └── p3b-unit-verifier/   ← orphaned? (no live delegation)
│   ├── fold-back/               [3 loop observations pending routing]
│   └── handoffs/                [2 agent handoff docs + README]
│
├── workflow/            ← CANONICAL PLANS (structured registry)
│   ├── plans/           ← 11 plan dirs, 7 are COMPLETED (see §2)
│   ├── specs/           [9 research/spec artifacts]
│   └── graph-bridge.yaml
│
├── history/             ← IMMUTABLE ARCHIVE (38 entries)
│   ├── [13 entries: PLAN.yaml + TASKS.yaml copied in]
│   ├── [25 entries: impl-results, specs, analysis docs only]
│   └── [most entries have delegate-merge-back-archive/ subdirs]
│
├── lessons/             [16 durable lessons]
├── proposals/           [this file lives here]
├── skills/              [19 local skills]
├── agents/              [agent definitions]
└── prompts/             [6 reusable prompts]
```

---

## 2. Plan Lifecycle State — Snapshot

```
.agents/workflow/plans/  (11 total)

ACTIVE (2)
┌─────────────────────────────────────────────┐
│ planner-evidence-backed-write-scope         │  0/6 tasks   no history dir
│ ralph-fanout-and-runtime-overrides          │  0/3 tasks   no history dir
└─────────────────────────────────────────────┘

PAUSED (1)
┌─────────────────────────────────────────────┐
│ refresh-skill-relink                        │  0/1 tasks   no history dir
└─────────────────────────────────────────────┘

COMPLETED — awaiting archive (7)  ← THE NOISE
┌─────────────────────────────────────────────┬───────────────────────────────┐
│ ci-smoke-suite-hardening        5/5 tasks   │ history/ exists  (DMA ✓)      │
│ error-message-compliance        4/4 tasks   │ history/ exists  (DMA ✓)      │
│ graph-bridge-command-readiness  4/4 tasks   │ history/ exists  (no DMA)     │
│ kg-command-surface-readiness    8/8 tasks   │ history/ exists  (DMA ✓)      │
│ loop-agent-pipeline            19/19 tasks  │ history/ exists  (DMA ✓)      │
│ platform-dir-unification        2/2 tasks   │ history/ exists  (DMA ✓)      │
│ plugin-resource-salvage         5/5 tasks   │ history/ exists  (DMA ✓)      │
└─────────────────────────────────────────────┴───────────────────────────────┘
DMA = delegate-merge-back-archive subdir pre-created by delegation closeout

NO STATUS (1) — drift
┌─────────────────────────────────────────────┐
│ typescript-port                 0/0 tasks   │  history/ exists  (DMA ✓)    │
└─────────────────────────────────────────────┘
```

---

## 3. Plan Status State Machine (with the gap highlighted)

```
   [draft] ──plan create──► [active] ──all tasks done──► [completed]
                                │                              │
                             paused                    status field exists
                                │                      but NO COMMAND here
                            [paused]                          │
                                                    MANUAL git commit
                                                    (done 3× in git history:
                                                     98c719e, b0828cd, 87bce37)
                                                              │
                                                              ▼
                                                        [archived]
                                                  status value exists in schema
                                                  (draft|active|paused|completed|archived)
                                                  but setting it via plan update
                                                  does NOT move any files
                                                              │
                                                              ▼
                                            .agents/history/<id>/  (immutable)
                                            PLAN.yaml  TASKS.yaml  *.plan.md
```

---

## 4. Command → Resource Map

```
READS from workflow/plans/            WRITES to workflow/plans/
──────────────────────────────        ──────────────────────────────────
workflow orient                       workflow plan create
workflow plan (list)                    → creates dir + PLAN.yaml + TASKS.yaml
workflow health                       workflow plan update
workflow next                           → edits PLAN.yaml in-place
workflow complete --plan <id>         workflow advance
workflow tasks --plan <id>              → edits TASKS.yaml task status

WRITES to active/                     ARCHIVES to history/
──────────────────────────────        ──────────────────────────────────
workflow fanout                       workflow delegation closeout
  → active/delegation/<task>.yaml       active/delegation/<task>.yaml  ──►
  → active/delegation-bundles/<id>      active/merge-back/<task>.md    ──►
workflow merge-back                    active/verification/<task>/     ──►
  → active/merge-back/<task>.md              history/<plan-id>/
workflow fold-back create                  delegate-merge-back-archive/
  → active/fold-back/<slug>.yaml              <date>/<task-id>/
                                               delegation.yaml
                                               merge-back.md
                                               closeout.yaml
                                               verification/

MISSING                               MISSING
──────────────────────────────        ──────────────────────────────────
drift: completed plan detection       workflow plan archive  ← PROPOSED
sweep: archive action type              workflow/plans/<id>/  ──────────►
                                        history/<id>/
                                        (stamp archived, merge dir,
                                         skip DMA, overwrite PLAN+TASKS,
                                         remove source)
```

---

## 5. History Directory — Anatomy of a Fully-Closed Plan

```
.agents/history/<plan-id>/
├── PLAN.yaml              ← should be copied at archive time
│                             (7 completed plans are missing this step)
├── TASKS.yaml             ← same
├── <id>.plan.md           ← narrative spec (when one existed)
├── impl-results.md        ← authored by agents during execution
└── delegate-merge-back-archive/
    └── <date>/
        └── <task-id>/
            ├── delegation.yaml   ← moved here by `delegation closeout`
            ├── merge-back.md     ← moved here by `delegation closeout`
            ├── closeout.yaml     ← written by `delegation closeout`
            └── verification/     ← moved here by `delegation closeout`
```

### Current history completeness (plans that have both PLAN+TASKS in history)

| Plan ID                        | PLAN.yaml | TASKS.yaml | impl-results | DMA closeouts |
|--------------------------------|-----------|------------|--------------|---------------|
| active-artifact-cleanup        | ✓         | ✓          | 2            | 0             |
| agent-resource-lifecycle       | ✓         | ✓          | 0            | 4             |
| command-surface-decomposition  | ✓         | ✓          | 0            | 6             |
| crg-kg-integration             | ✓         | ✓          | 2            | 0             |
| global-flag-compliance         | ✓         | ✓          | 0            | 1             |
| graph-bridge-command-readiness | ✓         | ✓          | 1            | 0             |
| loop-agent-pipeline            | ✓         | ✓          | 1            | 18            |
| loop-orchestrator-layer        | ✓         | ✓          | 0            | 0             |
| loop-runtime-refactor          | ✓         | ✓          | 0            | 2             |
| platform-dir-unification       | ✓         | ✓          | 0            | 0             |
| plugin-resource-salvage        | ✓         | ✓          | 0            | 0             |
| resource-command-parity        | ✓         | ✓          | 0            | 5             |
| resource-intent-centralization | ✓         | ✓          | 0            | 0             |
| skill-import-streamline        | ✓         | ✓          | 0            | 0             |
| typescript-port                | ✓         | ✓          | 0            | 7             |

Plans in history WITHOUT PLAN+TASKS (archived via impl-results only, no canonical plan was created):
agentsrc-local-schema, ci-smoke-suite-hardening*, delegation-merge-back-archive,
error-message-compliance*, go-rewrite, import-command, isp-scoped-runtime-pass,
kg-command-surface-readiness*, knowledge-graph-subproject-spec, loop-improvements-review,
managed-resource-cleanup, planner-resource-write-safety, project-diagrams,
ralph-runtime-permissions-and-error-handling, repository-guidelines,
repository-guidelines-restore, resource-sync-architecture-analysis, skill-architect-transform-*,
skill-import-promotion, workflow-automation-*, workflow-dogfood-loop-improvements

* These have PLAN+TASKS still in workflow/plans/ — delegation closeout ran but plan archive has not.

---

## 6. Key Architectural Invariants

1. `listCanonicalPlanIDs` returns ALL plans in `workflow/plans/` regardless of status — no filter.
2. `selectNextCanonicalTask` skips any plan where `status != "active"` (plan_task.go:874).
3. `delegation closeout` writes to `history/<id>/delegate-merge-back-archive/` — this dir may
   exist before a plan is ever archived, so archive must merge, not clobber.
4. `copyWorkflowDir` + `copyWorkflowArtifact` exist in `delegation.go` and are reusable.
5. `plansBaseDir()` = `.agents/workflow/plans/` — no equivalent `historyBaseDir()` helper exists yet.
6. Plan statuses defined: `draft | active | paused | completed | archived` (plan_task.go:122).
7. `archived` status has no behavioral effect today — it is a dormant stub.
