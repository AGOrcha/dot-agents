# payout-wrk and dot-agents agent-behavior evidence

**Date:** 2026-07-10  
**Purpose:** capture cross-repo evidence about agent behavior over time, workflow maturity, and verification discipline from `C:\Users\nprakash1\Documents\Pers\payout-wrk` and `C:\Users\nprakash1\Documents\Pers\dot-agents`.

This memo is intentionally separate from the Provider Admin / Roosevelt report because the repo-specific implementation context is different, even though the behavioral lessons are reusable.

## Evidence and time-anchor method

### Evidence sources used

- `payout-wrk\.agents\workflow\...`
- `payout-wrk\.agents\history\...`
- `payout-wrk\.agents\lessons\...`
- `dot-agents\.agents\workflow\specs\...`
- `dot-agents\.agents\workflow\plans\...`
- `dot-agents\.agents\history\...`
- recovered VS Code chat transcripts from `C:\Users\nprakash1\AppData\Roaming\Code - Insiders\User\workspaceStorage\*\chatSessions\*.jsonl`

### Transcript coverage note

The current local Copilot session store did **not** return matching sessions for:

- `C:\Users\nprakash1\Documents\Pers\payout-wrk`
- `C:\Users\nprakash1\Documents\Pers\dot-agents`

However, a later inventory of VS Code storage found hidden workspace transcripts for:

- local `dot-agents`
- devcontainer / workspace-file variants of `dot-agents`
- WSL `payout-wrk`
- local `payout-wrk`

Those recovered transcripts were not as rich as the Provider Admin CLI/session-state evidence, but they did confirm that some of the work was happening through:

- devcontainer-backed sessions
- workspace-file sessions
- provider-admin ↔ payout planning crossover chats
- agents-config / payout-rule readback

So this memo is built from:

1. workflow/history artifacts
2. recovered VS Code workspace transcript JSONLs where useful
3. embedded timestamps (`created_at`, `updated_at`)
4. git commit times
5. file creation/modify times as fallback

## Representative time anchors

| Case | Anchor used | Evidence |
|---|---|---|
| `dot-agents` planner evidence-backed write scope | git commit + file time | design last commit `2026-07-04T18:01:01-04:00` |
| `dot-agents` workflow parallel orchestration `p3-eligible-cmd` | merge-back created_at + git commit | `created_at 2026-04-21T07:00:00Z`; archived by commit `2026-05-19T16:02:04-04:00` |
| `dot-agents` loop-discipline-stop-hooks | workflow timestamps + git commit | `created_at 2026-05-25T15:16:58Z`, `updated_at 2026-07-09T20:38:07Z`; closeout commit `2026-07-09T16:38:23-04:00` |
| `payout-wrk` websocket-hub-test-suite | workflow timestamps + git commit | `created_at 2026-05-16T03:35:16Z`, `updated_at 2026-05-17T01:22:20Z`; archive commit `2026-05-16T21:25:09-04:00` |
| `payout-wrk` po-core-api-se-acceptance-coverage | workflow timestamps + git commit | `created_at 2026-05-16T03:35:16Z`, `updated_at 2026-05-16T15:23:40Z`; closeout commit `2026-05-16T11:29:25-04:00` |
| `payout-wrk` loop-discipline-stop-hooks-backfill | workflow timestamps + git commit | `created_at 2026-07-07T15:01:24Z`, `updated_at 2026-07-09T20:17:06Z`; migration commit `2026-07-09T16:17:20-04:00` |

## Recovered hidden transcript signals

The recovered VS Code workspace transcripts did not replace the artifact analysis, but they did add a few useful time-anchored behavior signals:

1. `2026-05-12` hidden session title: **"Create payout plan for provider admin"**
   - evidence that payout workflow thinking was already being applied back onto Provider Admin delivery planning
2. `2026-05-19` hidden transcript references to `agents-config/rules/payout/agents.md`
   - confirms active rule-layer readback, not just static files on disk
3. `2026-05-29` hidden transcript references to:
   - shared agent config/runtime overlay for Provider Admin repositories
   - the same split direction planned for payout
   - layered reusable config rather than repo-local policy duplication
4. `2026-04-21` hidden transcripts around `dot-agents` devcontainer files
   - support the earlier conclusion that some work happened through devcontainer/WSL contexts rather than the later Windows-first flow

Behavior signal:

- payout/dot-agents influenced Provider Admin process design earlier and more directly than the artifact-only pass could prove
- the recovered transcripts also reinforce the user's note that environment mode matters when comparing agent behavior over time

## dot-agents: evidence of workflow-model evolution

## 1. Planning evolved from prose plans toward execution contracts

The clearest design evidence is in:

- `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`
- `.agents/workflow/plans/loop-discipline-stop-hooks/loop-discipline-stop-hooks.plan.md`

The write-scope spec explicitly identifies the gap:

- `write_scope` was canonical, but still intuition-driven
- later workers could not see why paths were selected
- good plans still lacked enough operational evidence

The proposed response was not just "better notes," but a real execution-contract model:

- task goal
- locked decisions/invariants
- required reads
- verification target
- explicit exclusions/stop conditions

**Behavior signal:** the system started recognizing that planning quality problems were not only "no plan existed," but "the plan did not encode enough operational evidence."

## 2. The workflow started auditing itself

`.agents/workflow/specs/completed-plan-audit-analysis/design.md` is important because it treats completed bundles as potentially untrustworthy until audited.

That analysis introduces:

- evidence precedence
- risk-ordered bundle audit queue
- explicit verdict classes such as:
  - `verified-complete`
  - `completed-with-doc-drift`
  - `completed-with-evidence-gaps`
  - `reopen-recommended`

**Behavior signal:** the workflow matured from "completed means done" into "completed is a claim that still needs reconciliation against code, tests, and evidence."

## 3. The meta-loop became an explicit operating model

`.agents/workflow/specs/meta-loop-operating-model/design.md` makes the process shift explicit:

- separate **WORK loop** from **REFINEMENT loop**
- treat refinement as tracked work, not side effects in feature branches
- use orchestrator-managed task state across all plans
- feed lessons/proposals back as future work

This is strong evidence that the repo is not only improving artifacts; it is improving how agents are expected to improve themselves.

## 4. Companion task contracts reduced ambiguity

`loop-discipline-stop-hooks.plan.md` relies on task-specific contract files such as:

- `p0-sentinel-cli.contract.md`
- `p3b-companion-discipline-skills.contract.md`
- `p6-payout-backfill.contract.md`

These contracts pin:

- interfaces
- acceptance checks
- exclusions
- source maps
- sequencing rationale

**Behavior signal:** when task notes are too dense for `TASKS.yaml`, separate contract files are a scalable way to keep front-loaded task content strong.

## 5. Merge-back artifacts became resilient to worker failure

In `.agents/history/workflow-parallel-orchestration/.../p3-eligible-cmd/merge-back.md`:

- implementation was committed
- the worker environment became inaccessible before closeout
- the parent wrote the merge-back after confirming commit and verification

This is important because it shows the workflow can still preserve truth even when the sub-agent lifecycle fails late.

**Behavior signal:** merge-back/closeout is durable enough to survive partial worker failure if the parent can reconcile the code and verification evidence.

## 6. Loop/process reviews became first-class artifacts

`.agents/history/loop-improvements-review/impl-results.1.md` is not product work. It reviews:

- double-commit per iteration noise
- stale coverage summaries
- append-only next-iteration playbooks
- overused health/status commands

Then it updates:

- prompt guidance
- loop-state structure
- evidence-budget rules

**Behavior signal:** the repo uses historical evidence to tune agent workflow mechanics, not only product code.

## payout-wrk: downstream proof of workflow behavior under real delivery pressure

## 1. Delegated acceptance work can be tightly bounded and measurably verified

In `po-core-api-se-acceptance-coverage`:

- delegated task: `stripe-webhook-acceptance-sync-row`
- write scope: `po-core-api-se/internal/sync/`
- verification result recorded exact commands:
  - `go test ./internal/sync`
  - `./scripts/acceptance_coverage.sh`
- pass condition included a threshold:
  - `acceptance_coverage.sh 51.30% >= 51% threshold exit 0`

**Behavior signal:** good acceptance delegation does not stop at "tests pass"; it records the exact threshold and command surface that proved the slice.

## 2. Merge-back records can be honest about deviations without losing the slice

In `websocket-hub-test-suite` task `xadd-field-assertions`, the merge-back result is notable because it preserves a pass while explicitly documenting three deviations:

1. acceptance coverage failure due to **pre-existing untouched-package baseline skew**
2. SonarQube verification blocked by external service issues
3. `gofmt -l` Bash invocation blocked by sandbox, with formatting validated transitively through pre-commit

It also explicitly notes an unrelated untracked file from a prior task was left untouched.

**Behavior signal:** strong workflow evidence does not pretend the environment is clean; it separates:

- slice-local correctness
- environment/tooling failures
- pre-existing baseline skew

That is a better audit shape than collapsing all of that into "pass" or "fail."

## 3. Dirty live branches can be handled safely with isolated re-derivation

`loop-discipline-stop-hooks-backfill.plan.md` is a strong migration-pattern example.

It documents:

- earlier direct work against a live dirty branch
- why that proved the migration in reality but was not safely committable
- a clean re-derivation in an isolated worktree on top of actual mainline

It also preserved:

- exact config migration delta
- skill-resolution fixes
- known residual gaps
- hook dry runs
- preservation checks proving other active state was untouched

**Behavior signal:** the workflow can support "prove on live state, then re-derive cleanly" rather than forcing either unsafe branch pollution or purely theoretical migration work.

## 4. Negative controls were used to prove enforcement was real

The same backfill artifact included three hook dry-run cycles, including an explicit **negative control**:

- `isp-p6negtest.json`
- expected artifact path intentionally missing
- gate returned `{"decision":"block", ...}` and exit `2`

This is high-quality evidence because it proves the gate does more than silently pass.

**Behavior signal:** gates/hook enforcement should include at least one intentional failure case wherever possible.

## 5. Lessons were promoted into reusable guardrails

`payout-wrk\.agents\lessons\commit-scope-hygiene\LESSON.md` records a concrete mistake:

- a task commit swept in unrelated pre-existing edits

The lesson then turns that into a reusable rule:

- stage only task-specific paths/hunks
- do not rationalize unrelated fixes into the task commit just because they are beneficial
- separate quality fixes into their own commit

**Behavior signal:** repeated workflow mistakes become reusable operator guidance instead of staying as one-off corrections.

## Cross-repo agent-behavior lessons

Across both repos, the most important behavior changes are:

1. **From prose planning to execution contracts**
   - task queues alone were not enough
   - required reads, locked decisions, verification targets, and exclusions matter

2. **From "completed" to "auditable completed"**
   - status alone is insufficient
   - completed bundles may still require evidence audits or reopen decisions

3. **From happy-path verification to adversarial verification**
   - negative controls and honest deviation recording are stronger than green-only reporting

4. **From fragile worker sessions to durable parent reconciliation**
   - if a worker fails late, the parent can still salvage closeout when commit and verification evidence exist

5. **From dirty-branch improvisation to isolated clean derivation**
   - especially valuable for migration/config/backfill work

6. **From repeated friction to promoted guidance**
   - lessons, task contracts, and plan audits are evidence of real workflow self-improvement over time

## Practical guidance extracted from these repos

1. For high-risk tasks, prefer a **task contract** in addition to `TASKS.yaml`.
2. Require **exact verification commands and thresholds** in acceptance-oriented work.
3. Include at least one **negative control** when validating gates, hooks, or enforcement logic.
4. Record **deviations structurally** so later audits can separate slice-local truth from tool/environment issues.
5. When the live branch is dirty, prove behavior there if needed, then **re-derive the minimal delta in a clean worktree**.
6. Treat any repeated correction as a candidate for a **lesson, rule, or planner upgrade**.
