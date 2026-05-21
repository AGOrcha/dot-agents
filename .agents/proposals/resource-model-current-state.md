# Resource Model: `.agents/` System — Current State

> Captured: 2026-04-20. Use as the baseline domain model for the plan-archive-command proposal.
> Refreshed: 2026-05-21. Snapshot brought forward against post-#37/#38/#39/#41/#42 master state with explicit tracked-vs-local distinction. Counts verified via `git ls-tree origin/master` and `find` on the working tree.

---

## 1. Top-Level Directory Map

```
.agents/
├── active/              ← LIVE WORKFLOW I/O (active.loop runs read/write here)
│   │  TRACKED ON MASTER:
│   ├── platform-dir-unification.plan.md  ← STALE (history/platform-dir-unification has
│   │                                       no PLAN+TASKS; plan archive never ran. The
│   │                                       canonical plan was retired but this narrative
│   │                                       was left behind in `active/`.)
│   │
│   │  LOCAL WORKING STATE ONLY (not tracked on master; counts vary by worktree):
│   ├── delegation/              [contracts — currently 2 entries after PR #45 archive:
│   │                              cg6b-b3-workflow-helpers.md + its .AUDIT.md;
│   │                              prior 20 entries moved to history/archived-delegations/2026-05-20/]
│   ├── delegation-bundles/      [live worker bundles; populated per active loop session]
│   ├── reviews/                 [PR review note bundles: pr3b/, pr3c/
│   │                              (adversarial.md, architecture.md, test-behavior.md, TRIAGE.md)]
│   └── (fold-back / handoffs / merge-back / verification: present transiently as
│         workers create them; cleared by closeout — no permanent residents)
│
│   GONE since 2026-04-20 (templates that were tracked then but no longer exist):
│     active.loop.md, orchestrator.loop.md, loop-state.md   — loop templates removed
│     isp-prompt-orchestrator.plan.md                       — cleaned up (no longer stale)
│
├── workflow/            ← CANONICAL PLANS (structured registry, TRACKED)
│   ├── plans/           ← 15 plan dirs (all draft/active/in_progress/proposed — see §2)
│   └── specs/           ← 28 spec/design artifacts
│                          (graph-bridge.yaml NO LONGER lives under workflow/; it has been
│                          retired or moved out of this tree.)
│
├── prompts/             ← TRACKED (impl-agent + isp + review-agent overlays; verifiers/)
├── history/             ← IMMUTABLE ARCHIVE, TRACKED (53 entries, +15 since 2026-04-20)
│   ├── [25 entries: PLAN.yaml + TASKS.yaml copied in]
│   ├── [28 entries: impl-results, specs, analysis docs only]
│   └── [DMA subdirs present on 17 entries; see §5]
├── proposals/           ← TRACKED on master (8 files at this PR's merge base; this PR
│                          adds 2 more for a total of 10 at merge)
│
│   LOCAL WORKING STATE ONLY (not tracked on master; per-worktree):
├── lessons/             [durable lesson notes; ~6 entries locally]
├── skills/              [local skills, incl. one `global/` subdir; ~21 entries locally]
├── agents/              [agent definitions: loop-worker, test-runner, verifier — 3 locally]
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

> Note: as of the 2026-05-20 triage session, the prior 21 stale `active/delegation/`
> contracts have been resolved via PR #45 — 20 moved to
> `history/archived-delegations/2026-05-20/` with full per-contract evidence in the
> sibling MANIFEST, and 1 (`cg6b-b3-workflow-helpers`) carved out as an audit
> target with its sibling `.AUDIT.md` documenting the spawn-gate-cleared-but-no-
> bundle-materialized situation. The delegation-vs-canonical-plan parity audit
> that this section formerly recommended as "candidate follow-up" is now mostly
> complete; remaining open item is the cg6b-b3 spawn-or-skip decision.

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
