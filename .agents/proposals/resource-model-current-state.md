# Resource Model: `.agents/` System — Current State

> Captured: 2026-04-20. Use as the baseline domain model for the plan-archive-command proposal.
> Refreshed: 2026-05-20. Snapshot brought forward against post-#37/#39/#40/#41/#42 disk state.

---

## 1. Top-Level Directory Map

```
.agents/
├── active/              ← TRANSIENT (live workflow I/O)
│   ├── platform-dir-unification.plan.md  ← STALE (history/platform-dir-unification has
│   │                                       no PLAN+TASKS; plan archive never ran. The
│   │                                       canonical plan was retired but this narrative
│   │                                       was left behind in `active/`.)
│   ├── delegation/              [21 live delegation contracts — see §2 note]
│   └── reviews/                 [PR review note bundles: pr3b/, pr3c/
│                                  (adversarial.md, architecture.md, test-behavior.md,
│                                  TRIAGE.md)]
│
│   GONE since 2026-04-20:
│     active.loop.md, orchestrator.loop.md, loop-state.md   — loop templates removed
│     isp-prompt-orchestrator.plan.md                       — cleaned up (no longer stale)
│     delegation-bundles/, merge-back/, verification/       — directories no longer present
│                                                             at top of active/ (live workers
│                                                             write into worktree subdirs;
│                                                             see checkpoint message in
│                                                             orient JSON for evidence)
│     fold-back/                                             — gone (fold-back is now created
│                                                             via `workflow fold-back create`
│                                                             directly from staged notes)
│     handoffs/                                              — gone (handoff docs now produced
│                                                             on demand by agent-handoff skill)
│
├── workflow/            ← CANONICAL PLANS (structured registry)
│   ├── plans/           ← 15 plan dirs (all draft/active/in_progress/proposed — see §2)
│   └── specs/           ← 22 spec/design artifacts
│                          (graph-bridge.yaml NO LONGER lives under workflow/; it has been
│                          retired or moved out of this tree.)
│
├── history/             ← IMMUTABLE ARCHIVE (53 entries, +15 since 2026-04-20)
│   ├── [25 entries: PLAN.yaml + TASKS.yaml copied in]
│   ├── [28 entries: impl-results, specs, analysis docs only]
│   └── [DMA subdirs present on 17 entries; see §5]
│
├── lessons/             [6 durable lessons + index.md]
├── proposals/           [6 project-local proposals incl. this file]
├── skills/              [21 local skills incl. one `global/` subdir]
├── agents/              [3 agent definitions: loop-worker, test-runner, verifier]
├── prompts/             [4 reusable prompts: impl-agent, isp.prompt,
│                          review-agent + verifiers/ subdir]
└── worktrees/           [1 active worktree: proj-mega-branch — new since 2026-04-20]
```

---

## 2. Plan Lifecycle State — Snapshot (2026-05-20)

`da workflow orient --json` returns 15 canonical plans. No plan currently reports
status `completed` — the prior "noise" of completed-but-unarchived plans has been
cleared. Most plans are still in pre-execution states (`draft`, `proposed`,
`active`, `in_progress`).

```
.agents/workflow/plans/  (15 total)

IN_PROGRESS / ACTIVE (5)
┌──────────────────────────────────────────────────────────────────────────────────┐
│ go-test-fixture-extraction           active        2/8 done  spec ✓              │
│ graphstore-concurrency-contract      in_progress   2/5 done                      │
│ pr10-branch-split                    active        7/11 done                     │
│ refresh-skill-relink                 active        0/1 done                      │
│ shared-target-projection-wiring      in_progress   4/6 done                      │
│ sonarqube-pr10                       active        4/5 done (1 blocked)          │
└──────────────────────────────────────────────────────────────────────────────────┘

DRAFT / PROPOSED (9)
┌──────────────────────────────────────────────────────────────────────────────────┐
│ coverage-95-staged                   draft        28/28 done  (no pending — TBA) │
│ coverage-gate-per-file               draft         6/8 done                      │
│ di-refactor-rollout                  draft         0/6 done                      │
│ graph-backend-adapter-contract       draft         0/6 done   spec ✓             │
│ production-code-helper-extraction    proposed      2/6 done   spec ✓             │
│ root-command-decomposition           draft         0/0 done                      │
│ seam-interface-di-migration          draft         2/7 done   spec ✓             │
│ workflow-commit-command              draft         0/5 done                      │
│ worktree-platform                    draft         0/7 done                      │
└──────────────────────────────────────────────────────────────────────────────────┘

NO COMPLETED-BUT-UNARCHIVED PLANS
  (Plans with all tasks done are absent from workflow/plans/ in this snapshot —
   either archived to history/ or never reached the canonical registry.)
```

> Note: `active/delegation/` carries 21 live delegation contracts whose plan IDs
> don't all map to the 15 canonical plans above. Several contracts (e.g.
> `cg4-...`, `cg6b-...`, `docs-0.3.x`, `stp-...`, `pr5-scaffold-rehome`,
> `gcc1/gcc2-...`) appear to belong to in-flight wave work whose canonical
> homes are either inside the listed plans (e.g. `shared-target-projection-
> wiring`, `pr10-branch-split`) or live in branch worktrees (`proj-mega-branch`).
> An audit of delegation-vs-canonical-plan parity is a candidate follow-up.

### Plan ↔ spec ↔ history correlation

| Plan ID                           | Spec dir | History dir |
|-----------------------------------|----------|-------------|
| coverage-95-staged                | —        | —           |
| coverage-gate-per-file            | —        | —           |
| di-refactor-rollout               | —        | —           |
| go-test-fixture-extraction        | ✓        | —           |
| graph-backend-adapter-contract    | ✓        | —           |
| graphstore-concurrency-contract   | —        | —           |
| pr10-branch-split                 | —        | ✓ (DMA)     |
| production-code-helper-extraction | ✓        | —           |
| refresh-skill-relink              | —        | —           |
| root-command-decomposition        | —        | —           |
| seam-interface-di-migration       | ✓        | —           |
| shared-target-projection-wiring   | —        | —           |
| sonarqube-pr10                    | —        | ✓ (DMA)     |
| workflow-commit-command           | —        | —           |
| worktree-platform                 | —        | —           |

`pr10-branch-split` and `sonarqube-pr10` already have `history/` dirs with DMA
subtrees but their canonical plan dirs remain live — same plan-archive gap
described in §3.

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

> 2026-05-20 note: the orient snapshot now exposes statuses `in_progress` and
> `proposed` in addition to the schema's `draft|active|paused|completed|archived`
> — confirm whether those are alias values, schema extensions, or workflow-CLI
> render labels before this proposal's archive-command design lands.

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
  → active/delegation/<task>.md         active/delegation/<task>.md   ──►
  → (bundle path now resolved by         active/merge-back/<task>.md  ──►
     ISP runtime; no fixed                active/verification/<task>/ ──►
     active/delegation-bundles/ dir)         history/<plan-id>/
workflow merge-back                          delegate-merge-back-archive/
  → active/merge-back/<task>.md                <date>/<task-id>/
workflow fold-back create                       delegation.yaml
  → routes obs note into                        merge-back.md
    workflow/plans/<plan>/notes/                closeout.yaml
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

> 2026-05-20 note: `active/delegation-bundles/`, `active/merge-back/`,
> `active/verification/`, and `active/fold-back/` no longer exist at the
> repo-root `.agents/active/` tree. Live workers operate inside worktrees
> (see `.agents/worktrees/proj-mega-branch/` and `.claude/worktrees/seam-di/`
> referenced in the orient checkpoint message). The command → resource mapping
> above describes the conceptual contract; the literal repo-root subdir set
> has thinned. Verify the closeout path still archives into history/<id>/DMA/
> from worktree contexts.

---

## 5. History Directory — Anatomy of a Fully-Closed Plan

```
.agents/history/<plan-id>/
├── PLAN.yaml              ← should be copied at archive time
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

### Current history completeness (53 entries; PLAN+TASKS presence shown)

Plans WITH both PLAN.yaml + TASKS.yaml in history (25 entries):

| Plan ID                                 | DMA |
|-----------------------------------------|-----|
| active-artifact-cleanup                 | —   |
| agent-resource-lifecycle                | ✓   |
| binary-rename-da-sweep                  | ✓   |
| command-surface-decomposition           | ✓   |
| crg-kg-integration                      | ✓   |
| global-flag-compliance                  | ✓   |
| graph-bridge-command-readiness          | —   |
| kg-command-surface-readiness            | ✓   |
| legacy-shell-prune-share-rehome         | —   |
| loop-agent-pipeline                     | ✓   |
| loop-orchestrator-layer                 | ✓   |
| loop-runtime-refactor                   | ✓   |
| plan-archive-command                    | ✓   |
| platform-session-integration            | —   |
| platform-session-integration-followup   | —   |
| plugin-resource-salvage                 | ✓   |
| resource-command-parity                 | ✓   |
| resource-intent-centralization          | ✓   |
| self-review-iteration-close-wiring      | ✓   |
| skill-import-streamline                 | —   |
| test-file-structure                     | —   |
| test-file-structure-wave2               | —   |
| typescript-port                         | ✓   |
| (plus 2 more — see git log post-2026-04-20 for new arrivals)            |

Plans in history WITHOUT PLAN+TASKS (28 entries; archived via impl-results
only — no canonical plan was ever created, OR plan archive ran before this
contract was formalized):

agentsrc-local-schema, ci-smoke-suite-hardening*, delegation-merge-back-archive,
error-message-compliance*, go-rewrite, import-command, isp-scoped-runtime-pass,
knowledge-graph-subproject-spec, loop-improvements-review, macos-ci-pipefail-fix,
managed-resource-cleanup, planner-evidence-backed-write-scope*,
planner-resource-write-safety, platform-dir-unification,
pr10-branch-split* (DMA-only), project-diagrams,
ralph-fanout-and-runtime-overrides* (DMA-only),
ralph-runtime-permissions-and-error-handling, repository-guidelines,
repository-guidelines-restore, research-evaluation-kg-adjacent-enrichment,
resource-sync-architecture-analysis, skill-architect-transform-all-local-skills,
skill-architect-transform-skills, skill-import-promotion,
sonarqube-pr10* (DMA-only),
workflow-automation-follow-on-spec, workflow-automation-product-spec,
workflow-dogfood-loop-improvements, workflow-parallel-orchestration*

`*` = has DMA dir (closeout ran) but missing PLAN+TASKS in history. For the
three marked `(DMA-only)`, the canonical plan dir ALSO still lives in
workflow/plans/ — delegation closeout has executed but plan archive has not.
These are the present-day concrete callers for the proposed `workflow plan
archive` command.

---

## 6. Key Architectural Invariants

1. `listCanonicalPlanIDs` returns ALL plans in `workflow/plans/` regardless of status — no filter.
2. `selectNextCanonicalTask` skips any plan where `status != "active"` (plan_task.go:874).
3. `delegation closeout` writes to `history/<id>/delegate-merge-back-archive/` — this dir may
   exist before a plan is ever archived, so archive must merge, not clobber.
4. `copyWorkflowDir` + `copyWorkflowArtifact` exist in `delegation.go` and are reusable.
5. `plansBaseDir()` = `.agents/workflow/plans/` — no equivalent `historyBaseDir()` helper exists yet.
6. Plan statuses defined: `draft | active | paused | completed | archived` (plan_task.go:122).
   2026-05-20 caveat: orient JSON additionally surfaces `in_progress` and `proposed` for live
   plans — confirm canonicalization (schema vs render-label) before archive logic branches on
   status.
7. `archived` status has no behavioral effect today — it is a dormant stub.
