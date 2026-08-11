# dot-agents state — extracted from the payout Jul 8–11 mega-session (2026-07-11)

**Why this file exists.** The payout repo's two Jul-10 handoffs
(`~/proj-docs/payout/.agents/active/handoffs/2026-07-10-session-archaeology.md`
and `…-incident-recovery-followups.md`) enumerated a large amount of work that
is actually **dot-agents tooling**, not payout product work — it was merely
*discovered* by passes that ran inside the payout worktree. This file pulls that
tooling state into the dot-agents repo where it belongs. Both payout handoffs now
carry a pointer to this file and a note that the payout agent does **not** need
to handle the items below.

Grounding: every claim marked ✅ was re-verified against the live dot-agents tree
2026-07-11 (cites are `file:line` in `~/proj-docs/dot-agents`). Claims marked
⚠️SESSION come from the payout transcripts / handoffs and should be re-confirmed
on pickup. Where I re-verified a handoff claim and it had drifted, it is marked
🔁CORRECTED.

---

## 0. TOP PRIORITY — recover the cut-off #377 worker (Green377Sonar)

Source transcript: `~/.omp/agent/sessions/-proj-docs-payout/2026-07-08T00-32-22-093Z_019f3f23-da4d-7000-8465-d94a0d720d5b/Green377Sonar.jsonl`
(349 records; worker ran 2026-07-11 00:32–01:06Z, cut off mid-step by a tmux
server restart — a *different* event from the `rm -rf` workspace-deletion incident).

**What the worker did:**
- Assignment: fix the **11 new-code SonarCloud issues** across 5 files on
  `session/config-obs-guard-2026-07-10` (PR #377): `commands/workflow/delegation.go`,
  `commands/workflow/plan_task_test.go`,
  `internal/scaffold/hooks/global/guard-commands/guard.sh`,
  `…/guard-commands/omp/guard-rm.ts`, `internal/adapters/sdk/readsfrom_gate_test.go`.
- ✅ **Committed + pushed:** `054e0a3a fix(sonar): resolve 11 new-code SonarCloud
  issues on PR #377` is HEAD and is on `origin/session/config-obs-guard-2026-07-10`.
  The Sonar **new-code quality gate PASSED** on the 00:53Z run (`new_violations = 0`,
  "QUALITY GATE STATUS: PASSED") — the assigned task's core acceptance was met.
- ⚠️ Then the worker discovered the **same** `Coverage gate (merged multi-OS)` job
  ALSO enforces Go statement coverage, and it was RED because
  `internal/dashboard/handlers/stream.go` sat at **81.25%** (< 95% threshold, not
  on the per-file allowlist). This shortfall is **pre-existing**, unrelated to the
  11 Sonar fixes. The worker wrote new tests to close it, got cut off just before
  committing.

**At-risk work — RECOVERED, verified, still UNCOMMITTED on disk:**
- ✅ `internal/dashboard/handlers/stream_test.go` is `git status`-modified
  (+157 lines): `TestHandleEventsRequiresFlusher`, `TestHandleEventsReturnsWhenPaddingWriteFails`
  (uses `math.Inf(1)` to force a `json.Marshal` failure + a `failingWriteRecorder`),
  and a direct-`handleEvents` call bypassing the always-flushing logged middleware.
- ✅ Verified 2026-07-11: `go test -race -count=1 ./internal/dashboard/handlers/` → PASS,
  `stream.go` now **100.0%** (`handleEvents`/`paddingFrame`/`writeEventFrame` all 100%).
  This is exactly what greens #377's coverage-gate.
- The full diff is captured at `artifact://9` if the working tree is ever cleaned.

**Recovery action (recommended next step — NOT yet performed here):**
```
cd ~/proj-docs/dot-agents           # branch is ALREADY session/config-obs-guard-2026-07-10
git add internal/dashboard/handlers/stream_test.go
git commit -m "test(dashboard): cover stream.go flusher-guard + padding/event write-fail paths (100%)"
git push origin session/config-obs-guard-2026-07-10
# then gh run watch the new #377 run until Coverage gate (merged multi-OS) is green
```
**Gate note (do NOT auto-merge #377):** the incident-recovery handoff requires
omp be restarted (activates `~/.omp/agent/hooks/pre/guard-rm.ts`) and autonomous
multi-project orchestration stay paused until #377 merges + omp restarted. So push
the coverage fix to green the gate, but leave the merge to the owner gate.

**⚠️ The dot-agents working tree is parked on the #377 branch** with this one
uncommitted file. Any `git reset --hard` / `checkout` / stash-drop loses the
recovered work. Preserve it first.

---

## A. Open dot-agents PRs (verified via GitHub 2026-07-11)
- **#377** — `Session config/obs fixes + omp command guard + local master catch-up`
  — **OPEN**. 16 base commits + the salvage of 20 orphaned artifacts into
  `.agents/history/salvaged-active-artifacts-2026-07-10/` + the 11-Sonar fix.
  Coverage gate red only for the uncommitted `stream_test.go` fix above → push it, then owner-merge.
- **#332** — `release: 0.5.0` — **OPEN**, owner-held (docs predecessor merged).
  The `ReleaseDocsRefresh050` pass (19 sub-agents) was prep for this cut.
- **#347** — `refresh commit schema 8→40 chars` — ⚠️SESSION **CLOSED, unmerged**.
  Verify the schema-widening landed another way or is a real gap.

## B. Review-lens recording + merge-back decision (payout plan T1/T2) — DESIGN PICTURE

**These BLOCKERs are dot-agents work.** They were authored as findings F1/F2 →
tasks T1/T2 in the payout plan `config-content-hill-climb` § `review-agents-rules-corpus`,
but every file they touch lives in dot-agents (`commands/workflow/cmd.go`,
`delegation.go`, `review_decision_schema.go`, `internal/scaffold/home/starter/…`).
Payout should hand these to dot-agents (see the pointer note added to that plan region).

### B.1 The two config surfaces (BOTH must survive the fix) ✅
1. **StageProfile** — `internal/config/agentsrc.go:661-671`. `stage_profiles.<stage>.<slug>`
   → exactly three fields (`label`, `prompt_files []PromptFileRef`, `precondition_policy`),
   identical for executor/verifier/reviewer/orchestrator. Confirms the handoff claim
   that only `label`/`prompt_files`/`precondition_policy` are valid — there is no
   `command`/`on_fail`/`applies_when` and no `verdict`/`phase`/`lens` field on the profile.
   Author-owned, preserved across refresh (`cloneStageProfiles` 1110). Container
   `AgentsRC.StageProfiles` at agentsrc.go:317.
2. **ExecutionProfile / AppTypeProfile** — `internal/config/execution_profile.go`.
   `execution_profile.by_app_type.<appType>` → 4 facets: `relevance` (per-stage
   core/situational/noise filter, :36), `topology` (:40 — `executors`,
   `verifiers_per_executor`, `reviewers`, **`verifier_sequence []slug`** :93),
   `lenses` (:43 — **`lens_set []slug`** :100, **`lens_concurrency` parallel|gated|tiered** :103),
   `graph_backend` (:52).

### B.2 What "configurable verification/review per app type" IS — must NOT be lost ✅
- **Verification** = per-app-type `topology.verifier_sequence` → each slug →
  `stage_profiles.verifier.<slug>.precondition_policy` → the top-level
  `precondition_policies` registry. Resolved by
  `config.ResolvePreconditionPolicy(projectPath, appType)`
  (`internal/config/precondition_resolve.go:48-50,108-112`); shipped 2026-06-22
  (`.agents/history/verifier-precondition-policy/`). This gate lives **entirely on
  the verifier profile** — it is independent of the reviewer/lens recording surface,
  so **the T1/T2 fix need not touch it.** This is the "configurable verification"
  the owner does not want to lose; it survives for free.
- **Review** = per-app-type `lenses.lens_set` (which lenses run) + `lens_concurrency`
  (how: parallel/gated/tiered). Live dot-agents go-cli profile:
  `lens_set = [architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial]`,
  `lens_concurrency = gated`. **This is the "configurable review per app type."**
  It must remain the *driver* of whatever recording shape is chosen.
- Canonical owners: `stage-profile-and-routing-consolidation` spec §3 (D1/D2, SHIPPED
  2026-06-22) owns the profile primitive + `verifier_sequence`/`lens_set` reference
  model; `lens-evidence-policy` spec owns `lens_concurrency` dispatch policy.

### B.3 The blockers, with live cites ✅
- **F1/T1 — `verify record --kind review` hard-requires two phases, no lens awareness.**
  `dispatchVerifyRecordReview` (`commands/workflow/cmd.go:750-755`) hard-fails unless
  BOTH `--phase1-decision` and `--phase2-decision` are set (pinned by
  `cmd_test.go:347-350`). There is **no `--lens` flag** anywhere. Reviewers emit only
  `verdict: pass|fail (lens: X)`. The persistence was built for the OLD *two-phase*
  model (`review-agent.project.md` "Two-lens review contract": Phase 1 product/domain,
  Phase 2 architecture) — superseded by the 4-lens `lens_set`. Extra trap:
  `ReviewDecisionDoc` (`review_decision_schema.go:35-48`) has **no lens field** and the
  `review-decision.yaml` artifact is keyed by `task_id` alone
  (`.agents/active/verification/<task_id>/review-decision.yaml`), so N lenses overwrite
  each other's decision doc too — not only merge-back.
- **F2/T2 — merge-back + the whole contract chain is one-file-per-`task_id`.**
  `runWorkflowMergeBack` (`delegation.go:1455-1517`): contract `<taskID>.yaml`,
  merge-back `<taskID>.md`, result `<taskID>/<verifier>.result.yaml` — no lens/sub-task
  scoping anywhere. First lens flips the single contract to `completed`
  (guard `delegation.go:1483-1485`, "delegation for task %s is already %s"); every
  subsequent lens fails outright (pinned by `delegation_test.go:1472-1474`). Parent
  gate `evaluateDelegationGate` reads the single review-decision + merge-back
  (`review_gate.go:113-117`).
- Note: the human-readable review artifact is **already lens-scoped by filename** —
  `reviewer.base.md:54` writes `.agents/active/review/<task_id>-<lens>.md`. Only the
  **typed CLI artifacts** (review-decision.yaml, merge-back.md, delegation.yaml) are
  not lens-scoped. That's precisely the collision.

### B.4 Recommended shape — "same file, lens-labelled blocks, driven by lens_set"
The owner's steer ("same file, different block distinguished by a flag" + "don't
lose the per-app-type configurable verification/review") maps to **T1 option (a) +
T2 lens-accumulation**, NOT the collapse option. Rationale:

- **Reject T1 option (b) / collapse.** Collapsing reviewers into one parent-owned
  two-phase decision (reviewers stop calling `verify record`) discards the per-lens
  verdict surface and re-entrenches the retired product/architecture two-phase model
  — throwing away exactly the `lens_set`-driven configurability the owner wants kept.
  `lens-evidence-policy` §10-11 (open-Q1 + the lens-failure-telemetry follow-up)
  **already wants** `verify record --kind review` to emit **per-lens accept/reject +
  reason**, and `r1-outcome-scoring` consumes per-lens verdicts + merge-back status as
  scoring inputs → the records must stay machine-readable and per-lens-attributable.
- **T1 (a): add `--lens` accumulation.** `verify record --kind review --lens <lens>
  --verdict …` upserts that lens's block into ONE task-scoped
  `review-decision.yaml` (give `ReviewDecisionDoc` a `lenses` map/list, each entry
  `{lens, verdict, decision, findings_ref → <task_id>-<lens>.md, review_engine}`).
  The overall decision is **derived** from the accumulated lens verdicts via the
  existing pessimistic merge (`deriveOverallReviewDecision`: any reject→reject; else
  any escalate→escalate; else accept — `review_decision_schema.go:60-70`). Keep
  `--phase1/--phase2` as the **direct/non-lens** review path (mutually exclusive with
  `--lens`); the CLI derives overall from whichever mode ran.
- **T2 lens-accumulation (preferred over `<task_id>--<lens>` sub-tasks).**
  `merge-back --lens <lens>` accumulates into the single contract; the contract flips
  to `completed` only once **all lenses in the app-type's `lens_set` have merged back**
  (the parent/return-gate checks lens_set membership). Prefer this over sub-task-id
  because sub-task-id fragments the KG/scoring/board task identity and forces the
  parent to reassemble — accumulation keeps ONE task identity, which is exactly the
  "same file" the owner asked for.
- **The seam that preserves per-app-type config:** `execution_profile.by_app_type.<t>.lenses.lens_set`
  is what tells the accumulator *which* lens blocks are expected and *when* the set is
  complete. Different app type → different `lens_set` → different expected blocks →
  same accumulation machinery. `lens_concurrency` governs dispatch order only,
  orthogonal to recording. The verifier precondition gate (B.2) is untouched.
- **This is the already-designed target reached incrementally.**
  `staged-profile-dispatch-and-return-gate.md` Finding #7 + Target Ownership Contract
  (:123-145, status *superseded-by* the *completed* `stage-profile-and-routing-consolidation`)
  already says: each named reviewer owns its typed `<reviewer>.decision.yaml`; a
  **parent-owned deterministic return/aggregation gate** consolidates → one
  `review-decision.yaml` + merge-back; migration rule = the consolidated review stage
  keeps writing until typed artifacts + the return gate land. `--lens` accumulation IS
  that compat path; the parent aggregation gate lands later. Native reviewer *dispatch*
  (independent spawn — the thing that breaks one-per-task ownership) was explicitly
  deferred out of the shipped consolidation (§7:139-140).

### B.5 Open sub-decisions for the owner (surfaced, not decided)
1. Does `--lens` **coexist** with `--phase1/--phase2` (recommended: coexist —
   two-phase = direct non-fanout review per `review-agent.project.md`; `--lens` =
   staged N-lens, mutually exclusive) or **deprecate** two-phase?
2. Cross-lens **findings** consolidation shape under `gated` — `lens-evidence-policy`
   §10 open-Q3 is unspecified (the pessimistic merge answers the *decision*, not the
   *findings-block* format in merge-back).
3. Wire per-lens retry budget `lens_chain_max` now, or leave DEFERRED
   (`lens-evidence-policy` §9:464-467; it's already a `fanout` flag but not consumed)?
4. Where does the contract persist the expected `lens_set` so merge-back accumulation
   knows the completion set? (Fanout already resolves `verifier_sequence` onto the
   contract — `delegation.go:1588-1592`; add the `lens_set` analog.)

### B.6 Downstream prompt/scaffold cleanups (T3–T9 in the payout plan) — dot-agents scaffold
These are gated on T1/T2 landing and all touch `internal/scaffold/home/starter/…`:
- **T3/F4:** migrate `acceptance-invariants`, `adversarial`, `architecture-standards`
  reviewer AGENT.md to `da workflow resolve-prompt --kind reviewer --slug <lens>`
  (mirror `cross-harness-adversarial` Step 4); delete embedded duplicate prose —
  incl. the **shipped bug**: `architecture-standards-reviewer` L37 hardcodes
  dot-agents' own rule filenames into the generic starter template.
- **T4/F5:** ship `stage_profiles.reviewer.*` into the starter `.agentsrc.json` /
  `da init` (scaffold currently registers **zero** stage_profiles →
  `resolve-prompt` reads nonexistent paths); add a `matched:false` branch to
  cross-harness Step 4.
- **T5/F3:** `isp/instructions/staged-runtime.md` L36 says "Three-lens contract" and
  omits `cross-harness-adversarial` (landed 2026-06-24); fix the dead
  `loop-worker.md § "Review lenses"` cross-reference (that section does not exist).
- **T6/F6:** backport "Not this lens" disambiguation (adversarial vs
  acceptance-invariants invariants overlap); fix "3 lenses"→"4 lenses" language.
- **T7:** define an explicit dispatch trigger for `cross-harness-adversarial`
  (security/auth/crypto/PATH-sensitive or BLOCKER-risk diffs); fix
  `orchestrator/AGENT.md` L173/L194 "aggregate all lenses' merge-backs" prose once T1/T2 land.
- **T8/T9:** LOW — refresh `rules/global/rules.mdc`; spot-check
  `orchestrator/AGENT.md` MCP tool names.

Related pre-existing backlog item: `.agents/active/backlog-drain-queue.md:230`
IDEATION queue `lens-evidence-policy (openQ=3, per-lens evidence-form)` — the same theme.

## C. Workflow-store / CLI bugs
- **RMW lost-update race — FIX IS INCOMPLETE.** ✅ Confirmed: only
  `commands/workflow/plan_task.go` takes `agentslock.AcquireFileLock` (`plan_task.go:573`,
  landed as `479530bc`). The other TASKS.yaml/PLAN.yaml writers **`delegation.go`,
  `contract.go`, `eligible_accounting.go` have zero lock usage** → concurrent
  `da workflow advance`/`task update` still silently lose updates there. This is why
  board reconciles must be serialized. Filed follow-up:
  `~/.agents/proposals/workflow-store-concurrency-safe-writes.md` (standard
  concurrency guard for ALL workflow shared-file writes).
- **`plan update --dry-run` mutates PLAN.yaml** — ⚠️SESSION; **GENUINELY UNFILED**
  (adjacent dry-run bugs ARE filed: `obs-da-start-task-dryrun-not-honored.md`,
  `obs-foldback-create-dryrun-has-side-effects.md`). Needs its own artifact.
- **`complete --plan ""` bypasses required-flag validation** (`cmd/workflow/cmd.go:558-559`)
  — ⚠️SESSION; **GENUINELY UNFILED**. Needs an artifact.
- **`plan archive --plan` auto-commits repo-wide** (sweeps unrelated dirty
  canonical-store paths into one commit) — PARTIALLY covered by
  `~/.agents/proposals/obs-da-workflow-commit-scope-safety.md` (commit stages entire
  45-path store) but no plan-archive-specific artifact.
- **archive cleanup swallows bundle-removal errors** — PARTIALLY covered by the
  swallowed-errors audit `~/.agents/proposals/dot-agents-swallowed-errors-loud-atomic.md`
  (destructive-cleanup family) but not named for `plan archive`.

## D. Doc/code gaps — proposals ALREADY FILED (in `~/.agents/proposals/`)
All from the `ReleaseDocsRefresh050` pass; each has a filed artifact — do NOT re-file:
- Claude `message_display` unwired from `internal/platform/hooks.go` claudeEventTable
  → `platform-dirs-claude-message-display-unwired.md`.
- `agents remove` ignores global `--dry-run/--force` →
  `agents-remove-dryrun-force-not-honored.md`.
- raw `fmt.Errorf` bypasses the hint contract in `commands/kg/*` + `projectsync/promote.go`
  → `dot-agents-error-message-contract-raw-errorf-promote-kg.md`.
- KG search is substring `LIKE/ILIKE`, no FTS5/tsvector (docs overstate it) →
  `obs-kg-search-fts5-tsvector-not-implemented.md`.
- `LOOP_ORCHESTRATION_SPEC` §4 hook-authority stale vs shipped two-tier hard/soft block →
  `loop-orchestration-spec-hook-layer-authority-stale.md`.
- ⚠️SESSION still open (not a filed proposal): `journal.go` stale comment
  (`branchMatchesTask`→`strictBranchMatch`); 4 shipped starter skills missing from
  `docs/SKILL_COMMAND_INTEGRATION.md`.

## E. Proposal / observation ledger (user `~/.agents/` vs repo `./.agents/`)
Cross-mapped 2026-07-11. Reconciliation detail: this file's companion research.
- **Only ONE true cross-dir duplicate:** `omp-platform-handling.md` exists in BOTH
  `~/.agents/proposals/` and `./.agents/proposals/`, **byte-identical** (synced).
  No other filename collisions.
- **Handoff §E "unfiled proposals" — mostly ALREADY FILED:** verifier-chain forward
  surface (spread across repo `config-v2-dependency-map.md`, `ui-verifier-taxonomy.md`,
  `pr-ci-verifier-integration-audit.md`, user `python-service-runtime-entrypoint-smoke-verifier.md`);
  platform-config layering (repo `config-v2-coherence-scopes-sources-lock.md`,
  `config-harness-architecture-coherence.md`, `config-distribution-model-coherence-amendments.md`,
  `config-derived-agent-capability-profiles.md`, `config-explain-live-surface.md`);
  kg-ideate loop (repo `load-lessons-into-worker-bundles-2026-06-25.md`,
  `kg-crg-aware-bundle-authoring.md`, `kg-ideate-skill.yaml`);
  skill-corpus-drift-audit (user `config-agentsrc-doc-drift-audit.md`);
  release-docs code gaps (all filed — see §D).
- **Genuinely UNFILED, needs an artifact:** `lesson-index-sync` standalone (only a
  task step inside `kg-ideate-skill.yaml`); `plan update --dry-run` mutation;
  `complete --plan ""` bypass (§C).
- **Net-new dot-agents signal in user dir** not in the handoff: tests pollute real
  `~/.claude`/`~/.agents` (`obs-dot-agents-tests-pollute-real-home.md`); fanout
  write_scope omits sibling `*_test.go` (`obs-da-fanout-auto-test-scope.md`);
  `task update` lacks `--depends-on/--blocks` (`obs-da-task-update-depends-on.md`);
  OpenCode agent dir singular→plural rename untracked (`obs-platform-dirs-opencode-agent-dir-rename.md`).

## F. Lessons corpus state ✅
- **Index drift (concrete):** `.agents/lessons/index.md` has **67** entries for **68**
  dirs — `multi-target-family-needs-shared-core` has a dir but no index line. This is
  the exact case the `lesson-index-sync` proposal (§E, unfiled) would automate.
- **7 of 10 handoff-named lessons DO NOT EXIST yet** (no dir): `config-layer-imports-must-be-transitive`,
  `config-source-scope-is-routing-only`, `config-relevance-must-use-layered-resolver`,
  `real-layer-files-must-carry-version-2`, `recipe-env-vars-must-be-explicit`,
  `dead-error-path-elimination`, `prefer-the-real-matcher-over-the-legacy-comment`
  — prospective/in-flight, need capturing. Present + indexed: `build-tagged-test-import-cycle`,
  `sonar-container-fsmonitor-socket`, `stale-plan-status-vs-reality`.
- **Process fix (root cause of the incident's lesson loss):** commit a lesson in the
  SAME change that adds its index entry — the 12 lost lessons were authored but never committed.

## G. Deferred decisions with a dot-agents surface (owner input needed)
From payout handoff §D — the dot-agents-tooling subset:
- **Platform-config layering:** `orga-agents-config` scope (platform-only vs repo
  catalogs)? require `version:2` on layer files? org base single vs multiple? org
  locks now vs value-only inheritance first? (design set filed under repo `config-*` proposals; board §H).
- **Observability (obs.usepayout.com):** CF Access IdP source? D1 retention window?
  v1 event kinds (include `plan.status_snapshot` or telemetry-only)?
- **Verifier chain:** author CI/CD + GitOps gate wrapper scripts now or keep those
  verifier IDs blocked? Change the `stage_profiles.verifier` schema (only
  `label`/`prompt_files`/`precondition_policy` valid — confirmed §B.1) or keep
  behavior prompt-resident?
- **kg-scope-commit design:** `--signature-change` on fanout vs require
  `.scope.yaml signature_changes`? hard-fail on graph-unavailable signature fanout
  vs `--skip-impact-scope-gate`? `mechanical_signature_carve_out` file-exact vs globs?
  `da workflow commit` no-plan/no-task: compat warning vs require `--all-managed`?
- **Release 0.5.0 docs (#332):** hook-authority section rewrite vs footnote; Claude
  `message_display` as dedicated code change vs hooks-maintenance bucket; Antigravity
  first-class in rosters?

## H. Board / plan reality (dot-agents plans)
- **platform-config-layering** — all 5 tasks pending (resolver/schema gaps real).
- **worker-bundle-authoring** — all 10 tasks pending (bundle-impact/sidecar/lessons-facet
  surfaces don't exist yet).
- **verifier-chain forward surface** — 🔁CORRECTED: the payout handoff said this was
  "blocked on the relevance-resolver commit that is local-only, not pushed." **That is
  now stale.** `61b4930d fix(config): migrate relevance + verify off the flat snapshot
  onto the layered resolver` **IS pushed** — it's on `origin/session/config-obs-guard-2026-07-10`
  (the #377 branch) and all the `fix/*` origin branches. It lands when #377 merges;
  the forward surface is no longer blocked by an unpushed commit.
- **`DaRefreshInLock`** — ✅ landed + merged (`3c23cd3c fix(refresh): write refresh
  metadata to .agentsrc.lock` + merge of `fix/refresh-metadata-to-lock`).
- **`config-content-hill-climb`** 13/15 (the 2 deferred = T1/T2, §B);
  **`da-command-behavior-observations`** 6/6; **`go-auth-token-boundary`** interim done.
- **9 `fix/*` branches** — ✅ confirmed present, pre-consolidation originals superseded
  by #377 (they carry the same commits now on the #377 branch): `agents-fix-4th-lens-orphan`,
  `agents-migrate-reviewers-resolve-prompt`, `fix-fanout-with-tests`, `fix-start-task-dryrun`,
  `fix-task-update-depends-on`, `fix-tests-pollute-home`, `fix-workflow-tasksyaml-race`,
  `skills-disambiguate-wave-picker`, `skills-fix-kg-ideate-molecules` (+ `install-generate-scope`).
  **Delete after #377 merges.**
- ⚠️SESSION **ARCHIVE-BLOCKED:** `da workflow plan archive` moves don't persist (plans
  re-project / not git-committed) — 17 completed plans await archival
  (`backlog-drain-queue.md:245-250`). Needs the auto-commit-scope fix (§C) first.

## I. In-context dot-agents facts (from transcripts) ⚠️SESSION
- **`da run`:** registered `commands/root.go:222`; recipes `scaffold-plan.da`,
  `checkpoint-advance.da`; quote-blind env expansion (undefined → empty string);
  shipped `c421eda4` on master.
- **Skill loader:** one-level scan under `skills/global` → nested `kg-ideate`
  molecules (`kg-brief`/`spec-scaffold`/`plan-scaffold`/`staged-execution-handoff`)
  are NOT discoverable by name despite "Invoke standalone" wording (partly addressed
  by `e2bbbcb5 fix(skills): kg-ideate molecule discoverability`).
- **KG search:** substring `LIKE/ILIKE`, no FTS/tsvector table (§D).
- **Corpus sizes:** ~100 lessons total (65 dot-agents + 31 payout + 4 global) per the
  session; current dot-agents = 68 dirs / 67 index entries (§F).
