# pr-ci-verifier Integration Audit

**Date:** 2026-05-27
**Audit scope:** Existing `verify` / `verifier` / `verification` infrastructure in `dot-agents`, and how a new `pr-ci-verifier` subagent (proposed by `[[verifier-owns-ci-watch-shift-left]]`, PR #123) should integrate without colliding.
**Method:** Read-only investigation across CLI surface, schemas, starter agents, profiles, skills, hooks, and proposals.

---

## 1. Executive summary

The term "verifier" is **heavily overloaded** in this repo at three distinct semantic levels: (a) a CLI verb (`workflow verify record`), (b) an ISP-pipeline stage and the typed verifier-class workers that fill it (`verifier_sequence: [unit, api, ui-e2e, batch, streaming]`), and (c) **a real shipped subagent at `~/.agents/agents/global/verifier/AGENT.md`** registered as `verifier` in `.agentsrc.json`. The proposed name `pr-ci-verifier` therefore enters a crowded namespace and — more importantly — describes a workflow that is *structurally different* from the existing pipeline verifiers (post-merge PR-watch loop vs pre-merge handoff-driven scoped tests). The cleanest path is (i) **do not pick a name beginning with `verifier-` or ending in `-verifier`** to avoid being mistaken for an ISP verifier-class profile or for the shipped `verifier` agent; (ii) place the new agent in the dot-agents project overlay at `~/.agents/agents/dot-agents/pr-ci-watch/AGENT.md` (recommended name **`pr-ci-watch`**); (iii) reuse the existing `.agents/active/verification/<task_id>/` artifact family for its terminal `pr-readiness.result.yaml` so `verify record` and the iter-log readers continue to work; and (iv) introduce no new bundle `stage:` enum value — the watcher is spawned *after* `merge-back` and therefore lives outside the ISP staged-runtime chain.

---

## 2. Terminology census

| # | Term | Where used (file:line) | What it means | Concrete artifact / output |
|---|---|---|---|---|
| 1 | `da workflow verify record` (CLI) | `commands/workflow/verification.go:16` (`verifyRecordedByLabel`); `internal/scaffold/.../iteration-close/instructions/workflow.md:127` | Append a verification record to `.agents/active/verification-log.jsonl` and (when `--task` + `--verifier-type` given) write a typed result YAML | `.agents/active/verification-log.jsonl` + `.agents/active/verification/<task_id>/<verifier_type>.result.yaml` |
| 2 | `da workflow verify log` (CLI) | `commands/workflow/cmd.go` (subcommand registration); help text above | Tail the verification log | Reads `verification-log.jsonl` |
| 3 | `--kind` values for `verify record` | `commands/workflow/verification.go:22` (`isValidVerificationKind`) | Enum: `test \| lint \| build \| format \| custom \| review` | Drives schema branch in `verification-result.schema.json` |
| 4 | `--verifier-type` flag (for `verify record`) | `commands/workflow/verification.go` + help text | Profile id used as filename stem for the typed result artifact (`unit`, `api`, `batch`, `merge-back`, …) | `<task_id>/<verifier_type>.result.yaml` |
| 5 | `VerifierTypeMergeBack` const = `"merge-back"` | `commands/workflow/verification_result_schema.go:15` | Sentinel verifier_type used for worker merge-back results | `<task_id>/merge-back.result.yaml` |
| 6 | `stage: verify` (delegation bundle, legacy `role:`) | `internal/scaffold/.../agents/global/loop-worker/AGENT.md:18`, `:37` | One of three bundle stages a `loop-worker` may execute: `impl \| verify \| review` (defaults to `impl`) | Worker runs/authors tests, writes verification trace, closes out via `verify record --kind test` |
| 7 | `verifier_sequence` (bundle field) | `schemas/workflow-delegation-bundle.schema.json:192-198`; `staged-runtime.md:22` | Ordered list of verifier-profile ids the ISP stage chain will run after `impl-handoff.yaml` lands | Per-verifier subagent sessions, each writing `<verifier>.result.yaml` |
| 8 | `app_type` + `app_type_verifier_map` | `schemas/workflow-delegation-bundle.schema.json:188-191`; `.agentsrc.json:8` | Resolves which verifier sequence applies for an app type (e.g. `go-cli` → `[unit]`) | Hydrates `verifier_sequence` at fanout |
| 9 | `verifier_profiles` (planned) | `app-type-profiles/design.md`; `org-config-resolution/design.md:71` | First-class typed verifier-profile definitions (versioned, composable) | Future config layer |
| 10 | `.agents/active/verification/<task_id>/` (artifact dir) | `iteration-close/SKILL.md:10-15`; live samples e.g. `t03-move-install/unit.result.yaml`, `agents-pkg/merge-back.result.yaml` | Per-task verification artifact family (handoff + per-verifier results + review-decision) | `impl-handoff.yaml`, `<verifier>.result.yaml`, `review-decision.yaml`, `merge-back.result.yaml` |
| 11 | `--role` checkpoint flag values | `iteration-close/instructions/workflow.md:96, 146`; `iteration-close/SKILL.md:15` | Enum: `impl \| verifier \| review` — selects which iter-log block to merge into `iter-N.yaml` | Iter-log v2 `impl:` / `verifier:` / `review:` blocks |
| 12 | `verification-decision.schema.json` | `schemas/verification-decision.schema.json`; `iteration-close/instructions/workflow.md:84` | Two-phase review decision schema (phase_1/phase_2 + consolidation) | `<task_id>/review-decision.yaml` |
| 13 | `verification-result.schema.json` | `schemas/verification-result.schema.json`; `verification_result_schema.go:18` | Per-verifier typed result payload | `<task_id>/<verifier_type>.result.yaml` |
| 14 | Shipped `verifier` agent | `~/.agents/agents/global/verifier/AGENT.md` (real file); registered in `.agentsrc.json:3` `"verifier"` | A general-purpose subagent that runs tests + smoke flows after work is "marked done" and produces a `## Verification report` markdown block | No structured artifact — free-form markdown response |
| 15 | Shipped `test-runner` agent | `~/.agents/agents/global/test-runner/AGENT.md`; `.agentsrc.json:3` | Proactive subagent that runs tests on detected code changes and patches failures without weakening intent | Free-form markdown |
| 16 | `review_type` lenses (loop-worker review stage) | `internal/scaffold/.../profiles/loop-worker.md:18-44`; `agents/global/loop-worker/AGENT.md:18, :38` | Required field on `stage: review` bundles; enum: `architecture-standards \| acceptance-invariants \| adversarial`. Each lens is one worker, no production edits | Findings list (BLOCKER/HIGH/MEDIUM/LOW + file:line) feeding `review-decision.yaml` |
| 17 | (proposed) `pr-ci-verifier` | NEW — referenced only in `.agents/lessons/verifier-owns-ci-watch-shift-left/LESSON.md` (PR #123) | Post-merge-back PR-watch loop: polls `gh pr checks` + SonarCloud quality-gate API, auto-fixes mechanical issues, escalates non-mechanical to impl, exits on terminal `READY`/`FOLD-BACK` | Currently undefined — lesson sketches markdown handoff, no schema yet |

---

## 3. Conceptual flow + where the new agent slots in

```
PRE-MERGE (ISP staged runtime, owned by loop-worker bundle stages)
  ┌──────────────────────────────────────────────────────────────────────┐
  │  impl  ──>  verifier(s) ──>  review-lens(es) ──> parent gate         │
  │  (one)      (unit|api|     (architecture-     (delegation closeout) │
  │             ui-e2e|batch    standards |                              │
  │             |streaming)    acceptance-invariants                     │
  │                            | adversarial)                            │
  │                                                                      │
  │   ↓ writes ↓                ↓ writes ↓        ↓ writes ↓             │
  │   impl-handoff.yaml         <verifier>        review-decision.yaml   │
  │                             .result.yaml                             │
  │                                                                      │
  │   All inside .agents/active/verification/<task_id>/                  │
  │   All recorded via `workflow verify record --kind {test|review}`    │
  │   All merged into iter-N.yaml via `workflow checkpoint --role …`    │
  └──────────────────────────────────────────────────────────────────────┘
                              │
                              │  worker exits at merge-back
                              ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │  MERGE-BACK + PR CREATION (impl loop-worker's last step)             │
  │  writes .agents/active/merge-back/<task>.md                          │
  │  pushes branch, opens PR via gh                                      │
  └──────────────────────────────────────────────────────────────────────┘
                              │
                              ▼  ← NEW AGENT SLOTS IN HERE (post-merge-back)
  ┌──────────────────────────────────────────────────────────────────────┐
  │  POST-MERGE-BACK PR-CI WATCH  (NEW: dot-agents project overlay only) │
  │                                                                      │
  │  pr-ci-watch (proposed name) loop:                                   │
  │    poll gh pr checks <n>                                             │
  │    poll SonarCloud /api/qualitygates/project_status                  │
  │    classify surfaced issues                                          │
  │      └─ mechanical → auto-fix in-loop                                │
  │      └─ non-mechanical → fix-up brief → impl agent                   │
  │    terminal: READY (clean) or FOLD-BACK (hard-blocked)               │
  │                                                                      │
  │   ↓ should write ↓                                                   │
  │   .agents/active/verification/<task_id>/pr-readiness.result.yaml     │
  │   (reusing existing artifact family + verification-result schema)    │
  └──────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
  ┌──────────────────────────────────────────────────────────────────────┐
  │  PARENT ORCHESTRATOR: workflow delegation closeout --decision accept │
  │  (triggered by READY signal — fixes the [[verify-task-status-vs-     │
  │  pr-history]] stale-eligibility leak)                                │
  └──────────────────────────────────────────────────────────────────────┘
```

### Overlap surfaces (must be addressed)
- **Name collision** with shipped `verifier` agent (#14) at `~/.agents/agents/global/verifier/`.
- **Stage-name collision** with bundle `stage: verify` (#6) and `verifier_type` profiles (#4, #7).
- **Artifact-family overlap**: new agent should *reuse* `.agents/active/verification/<task_id>/` (#10) rather than introduce a parallel `.agents/active/pr-readiness/` tree.

### Genuine gap (no existing infra)
- No existing agent owns *post-merge-back PR-CI watch*. Today this is bundled into impl per `[[worker-owns-pr-readiness-loop]]`. The new agent fills a real hole.

---

## 4. Integration decisions

### 4.1 Name

`pr-ci-verifier` **clashes** with shipped `~/.agents/agents/global/verifier/` (#14) and with the planned per-type verifier-profile agents (#7, #9) that the codex `staged-profile-dispatch-and-return-gate` track promises. A loop-worker reading "verifier" expects an ISP pipeline stage, not a post-merge watch loop.

**Recommended name: `pr-ci-watch`.**

Rationale: "watch" accurately describes the polling-loop nature of the work; "pr-ci" scopes it to the post-PR CI surface; the name avoids the `verifier-` / `-verifier` namespace entirely. Spawn line reads cleanly: `Agent({subagent_type: "pr-ci-watch", input: {pr: 123, ...}})`.

**Alternatives (less preferred):**
- `pr-readiness-monitor` — accurate but longer; "monitor" implies passive observation, which is wrong (it auto-fixes)
- `ci-gate-warden` — vivid but overloads "gate" with `iteration-close-gate` / `loop-worker-gate` hooks (§7)
- `pr-watchdog` — same critique as `pr-readiness-monitor`; less specific about scope

### 4.2 Storage path

**Recommendation:** `~/.agents/agents/dot-agents/pr-ci-watch/AGENT.md` (canonical), linked into the repo at `.agents/agents/pr-ci-watch/AGENT.md` via the existing `da agents import` / `promote` flow.

Per `[[starter-vs-project-overlay]]`: this agent depends on `gh`, SonarCloud, and `coverage-exceptions.txt` — none of which the generic starter knows about. It is **dot-agents-dev overlay material**. Other `da` consumers running on GitLab/Jenkins would write their own `<project>/<name>` agent.

The user's prompt suggests `.agents/agents/dot-agents/pr-ci-verifier/AGENT.md` — that path under the *repo-local* `.agents/agents/` would not match the current scope layout (per `commands/agents/new.go:34-39`, scope `dot-agents` would translate to `~/.agents/agents/dot-agents/pr-ci-watch/`). Use `da agents new pr-ci-watch dot-agents` to author it correctly, then `da agents promote pr-ci-watch` to register in `.agentsrc.json.agents`.

### 4.3 Bundle stage

**Do not** add a new `stage:` enum value. The watch loop runs *after* the loop-worker's merge-back closeout, outside the ISP staged-runtime chain. The bundle schema's `stage` enum (`impl | verify | review`) describes pre-merge work; expanding it would invite confusion with stage 6.

Instead: the parent orchestrator spawns `pr-ci-watch` as a standalone subagent (no bundle) with prompt-passed inputs `{pr_number, task_id, plan_id, impl_agent_id, feedback_channel_path}`. The watcher reads the existing merge-back artifact for context. No bundle schema change required.

If a contract-style handoff is needed later, model it as a *post-bundle* artifact — e.g. `.agents/active/pr-ci-watch/<task_id>.input.yaml` — without touching `workflow-delegation-bundle.schema.json`.

### 4.4 Verification-artifact reuse

**Reuse the existing artifact family.** The watcher's terminal output should be:

```
.agents/active/verification/<task_id>/pr-readiness.result.yaml
```

…written via `da workflow verify record --kind custom --task <task_id> --verifier-type pr-readiness --status pass|fail|partial --summary "READY"|"FOLD-BACK: <reason>"`. This:
1. Validates against the existing `verification-result.schema.json` (no schema change).
2. Lands in the same dir other verifiers write to (#10).
3. Appears in `verification-log.jsonl`, making `da workflow verify log` and downstream graphs observe PR-CI as just another verifier kind.
4. Surfaces in iter-log v2 via `workflow checkpoint --role verifier --verifier-type pr-readiness` (the `--role verifier` enum already supports this per #11).

The `verifier_type: pr-readiness` value reads as "a kind of verifier that checks PR readiness" — semantically honest and avoids inventing a new artifact category.

### 4.5 Tools list

```yaml
tools: Bash, Read, Grep, Glob, Edit, Write
```

- `Bash` — `gh`, `curl`, `git -C`, `go test`, `coverage-gate.sh`
- `Read`, `Grep`, `Glob` — read merge-back, SonarCloud responses, allowlist files
- `Edit`, `Write` — auto-fix mechanical issues (cog complexity refactors, dup-literal consts, focused tests)
- **No `Task`** — the watcher is itself a leaf subagent; auto-fixes are in-process, escalations are markdown handoffs back to the parent, not subagent spawns. Adding `Task` would let it recursively spawn loop-workers, breaking the bounded contract.

### 4.6 Spawning mechanism

**Orchestrator-spawned via `Agent({subagent_type: "pr-ci-watch", ...})`** as a regular Claude Code subagent.

Rationale:
- Hook-triggered (e.g. on `merge-back` artifact creation) would conflate transport (hook) with subagent identity and bypass the orchestrator's batch control.
- CLI-invocation (`da workflow pr-ci-watch ...`) would force the loop into a synchronous shell call. Polling + auto-fix needs the subagent runtime's tool loop.
- Agent-spawn is consistent with `[[loop-worker-vs-general-purpose]]` — `pr-ci-watch` is a NAMED subagent type for a bounded scope, not a free-form worker.

### 4.7 Relationship to existing CLI

- Calls `da workflow verify record --kind custom --verifier-type pr-readiness ...` exactly once at terminal state (READY or FOLD-BACK).
- Does **NOT** call `da workflow checkpoint` — that's a closeout step owned by the impl worker before merge-back.
- Does **NOT** call `da workflow merge-back` — already done by the impl worker before this agent spawns.
- **DOES** call `da workflow delegation closeout --plan <id> --task <id> --decision accept` on terminal READY — this is the missing step that `[[verify-task-status-vs-pr-history]]` identifies as the root cause of stale eligibility. The watcher closing the loop after PR merge is exactly the intervention that lesson calls for.

### 4.8 Relationship to lens reviewers (per PR #122 / current loop-worker)

The three lens reviewers (`architecture-standards`, `acceptance-invariants`, `adversarial`) are **executed as `stage: review` invocations of the existing `loop-worker` agent**, parameterized by `review_type`. They are not separate top-level agents in starter — see `internal/scaffold/.../profiles/loop-worker.md:18-44` and `agents/global/loop-worker/AGENT.md:18,:38`. The user's prompt cites `architecture-standards-reviewer/AGENT.md` etc. as separate agents in PR #122; those files **do not currently exist** in `internal/scaffold/home/starter/agents/global/` (only `loop-worker/AGENT.md` does). PR #122 may have landed differently than the prompt assumes — confirm before authoring.

Regardless of how the lenses are packaged, the **conceptual distinction** is clear and should be encoded in `pr-ci-watch`'s AGENT.md:

| Aspect | Lens reviewers | `pr-ci-watch` |
|---|---|---|
| When | Pre-merge (after impl, before merge-back) | Post-merge-back (after PR opens) |
| Input | Code diff + write_scope | PR number + merge-back artifact |
| Output | `review-decision.yaml` (accept/reject/escalate) | `pr-readiness.result.yaml` (READY/FOLD-BACK) |
| Loop | One-shot per lens | Polling loop until terminal |
| Mutates code? | Never (findings only) | Yes (mechanical auto-fix); escalates non-mechanical |
| Concurrent? | N lenses × 1 worker each (parallel ok) | One per PR (serial within a PR) |
| Audience | Parent orchestrator (review gate) | Parent orchestrator (closeout gate) |

They are siblings in the ownership tree, not the same family.

---

## 5. Recommended AGENT.md outline

```yaml
---
name: pr-ci-watch
description: Post-merge-back PR-CI watcher (dot-agents project overlay). Polls gh pr checks + SonarCloud quality-gate, auto-fixes mechanical issues, escalates non-mechanical to impl, exits on terminal READY or FOLD-BACK. Spawned after impl worker writes merge-back artifact.
tools: Bash, Read, Grep, Glob, Edit, Write
---
```

Section list:

1. **Role** — single-paragraph mandate; explicitly contrast against `verifier` (#14), ISP verifier stages (#7), and lens reviewers
2. **Inputs** (prompt-passed): `pr_number`, `task_id`, `plan_id`, `impl_agent_id` (for fix-up briefs), optional `feedback_channel_path`
3. **Startup** (3 steps): read merge-back artifact; confirm PR open; confirm task status pending/in_progress
4. **Watch loop** — polling cadence (60-90s), `gh pr checks <n>` schema, SonarCloud QG endpoint, terminal-state detection
5. **Classification table** — verbatim from lesson §3 (S3776, S1192, coverage, allowlist, security hotspot vs vulnerability, CPD-tabular, test failures, merge conflicts)
6. **Auto-fix patterns** — for each mechanical class, the canonical refactor + cross-link to lessons (`[[const-extraction-triggers-cpd-on-tables]]`, `[[no-lazy-allowlist-tech-debt]]`)
7. **Escalation format** — markdown brief schema written to `.agents/active/pr-ci-watch/<task_id>/escalation.md`; one entry per escalation
8. **Closeout** — single `verify record --kind custom --verifier-type pr-readiness`; on READY, also run `delegation closeout --decision accept` (the missing step from `[[verify-task-status-vs-pr-history]]`)
9. **Guardrails** — no orchestrator commands (`workflow orient`, `next`, `status`); no merge-back rewrite; never widens write_scope beyond auto-fix patterns; tabular-CPD check before any S1192 fix

---

## 6. Pre-flight tasks (file before authoring AGENT.md)

| # | Task | Why it must land first |
|---|---|---|
| P1 | Confirm PR #122 landing state for lens reviewers (separate AGENT.md files vs `loop-worker` `review_type` parameterization). | The AGENT.md's §1 "Relationship to lens reviewers" depends on which packaging is live. |
| P2 | Add `pr-readiness` to the documented set of `verifier_type` values in `verification_result_schema.go`'s godoc + `verification-result.schema.json` examples. No schema change needed (the field is free-form `string`) but document the convention. | Prevents the watcher's artifacts from looking ad-hoc to readers familiar with `unit`/`api`/`batch`. |
| P3 | Trim `[[worker-owns-pr-readiness-loop]]` to clarify dot-agents-dev supersession path (per lesson §"How to apply" item 3). Lesson currently only exists in worktrees, not master — confirm canonical location before edit. | Avoids leaving two contradictory lessons live. |
| P4 | Decide whether `delegation closeout --decision accept` from the watcher requires owner override. Today `workflow delegation closeout` is "parent-only" per `closeout.parent_must` in the bundle schema (§ schema line 242-247). Watcher acting on behalf of parent is a contract change. | If not addressed, the watcher's terminal step fails the bundle contract and the loop never closes. |
| P5 | Add a test fixture under `internal/testutil/` modeling a sample `pr-readiness.result.yaml` so future schema tooling has a reference. | Keeps the new verifier_type honest against existing schema validators. |

---

## 7. Risk register

| # | Risk | Probability | Mitigation |
|---|---|---|---|
| R1 | Authoring `pr-ci-verifier` (the name in the lesson) creates a real subagent that collides with shipped `verifier` in `.agentsrc.json`. Spawn dispatch by `subagent_type` matches on suffix in some Claude Code search UIs — both would appear in autocomplete. | High | Rename to `pr-ci-watch` per §4.1. Update lesson cross-references before authoring agent. |
| R2 | Watcher writes artifacts under a new top-level dir (`.agents/active/pr-readiness/`) instead of reusing `.agents/active/verification/<task_id>/`. Downstream iter-log mergers + `verify log` ignore them. | Medium | Encode artifact path in §4.4. Make `verify record --verifier-type pr-readiness` the only sanctioned closeout. |
| R3 | Watcher adds a new bundle `stage: pr-watch` or `stage: pr-ci-watch` to "fit in". Breaks the ISP staged-runtime invariant that all stages run pre-merge-back; breaks `loop-worker.AGENT.md` stage enum. | Medium | Explicitly forbid in AGENT.md §"Guardrails"; document spawn-outside-bundle pattern in §4.3. |
| R4 | Watcher auto-fix accidentally widens write_scope beyond impl worker's bundle (e.g. S1192 fix touches a shared constants file outside the original scope). Trips `[[parallel-worker-branch-drift]]`. | Medium | Constrain auto-fix to files already touched in the merge-back commit; anything outside that set must escalate. Document in §"Guardrails". |
| R5 | Watcher polls indefinitely on a stuck PR (CI infra outage). Burns subagent runtime + tokens. | Low-Medium | Add `--max-iterations N` and `--max-wall-clock 30m` envelope; on exceed, write `FOLD-BACK: timeout` and exit. Document in §"Watch loop". |

---

## 8. Open questions for the user

1. **Did PR #122 land separate `architecture-standards-reviewer`, `acceptance-invariants-reviewer`, `adversarial-reviewer` AGENT.md files** in `internal/scaffold/home/starter/agents/global/`, or did it consolidate them under `loop-worker`'s `review_type` parameterization (which is the only thing visible on `master` today)? The audit assumes the latter based on filesystem evidence; the lesson's §"Cross-references" wording assumes the former.
2. **Is `pr-ci-watch` an acceptable rename**, or do you want to keep `pr-ci-verifier` for continuity with the lesson title? If kept, the lesson should be renamed too to keep cross-references stable, AND the shipped `verifier` agent's description should be tightened so the two don't both appear in subagent-picker UIs.
3. **Should `pr-ci-watch` be allowed to run `da workflow delegation closeout`** on the parent's behalf (per §4.7), or should it only emit a `READY` signal and let the orchestrator close out? Affects whether the bundle schema's `closeout.parent_must` contract needs to be relaxed or whether a new "delegated-closeout-from-watcher" exception is documented.
4. **Promote to starter or keep in dev overlay only?** The lesson is explicit (dev overlay only), but the auto-fix patterns for S3776/S1192/CPD are *generic Go+Sonar wisdom*. Splitting "generic auto-fix knowledge" (potential starter skill) from "dot-agents specific CI surface" (overlay-only AGENT.md) may be worthwhile.
5. **Concurrency model:** if a wave ships 5 PRs in parallel, do we spawn 5 concurrent `pr-ci-watch` subagents, or one watcher that fans out internally? The first matches `[[loop-worker-vs-general-purpose]]` discipline (one bounded subagent per scope); the second amortizes API knowledge across PRs (the original motivation in the lesson).

---

## Cross-references

- `[[verifier-owns-ci-watch-shift-left]]` — origin lesson (PR #123)
- `[[worker-owns-pr-readiness-loop]]` — prior pattern this work supersedes for dot-agents-dev
- `[[starter-vs-project-overlay]]` — placement rule applied in §4.2
- `[[loop-worker-vs-general-purpose]]` — bounded-subagent-type discipline applied in §4.5
- `[[verify-task-status-vs-pr-history]]` — the stale-eligibility gap that §4.7 closes
- `[[const-extraction-triggers-cpd-on-tables]]` — auto-fix guard cited in §5 §9
- `[[no-lazy-allowlist-tech-debt]]` — auto-fix guard cited in §5 §9
- `[[parallel-worker-branch-drift]]` — risk R4 mitigation rationale
- `staged-profile-dispatch-and-return-gate` (codex track) — future verifier-profile namespace this audit avoids colliding with
- `app-type-profiles/design.md` — `verifier_profiles` taxonomy the audit's naming respects
