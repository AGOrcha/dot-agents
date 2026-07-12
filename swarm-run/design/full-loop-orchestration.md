# Full-loop orchestration contract — N plans, lifecycle slots, and meta-loop fold-back

**Status:** integration design. This wraps the existing `profile-driven.swarm.yaml`; it does not replace its per-task verifier/reviewer profiles.

## 1. Boundary: da is the outer loop; swarm is one task slot

The swarm DSL only defines `mode`, `target_count`, and an agent dependency graph (`swarm-extension/src/swarm/schema.ts:14-21`). It has no plan store, cross-plan dependency resolver, worker-slot ledger, delegation lifecycle, or proposal/fold-back store. Those are da responsibilities.

Therefore:

- **Outer loop (Main + da):** drives every active plan, selects conflict-free eligible tasks, enforces the live slot budget, creates delegations/fanout lineage, launches inner runs, reconciles lifecycle state, routes fold-backs, and records/scorers outcomes.
- **Inner loop (one `profile-driven.swarm.yaml` instance):** owns one `<plan>/<task>` while that task holds a workflow slot; resolves `app_type`, executes implementation, runs the configured verifier sequence and reviewer lenses, and returns READY or FOLD-BACK artifacts.
- **Workers produce artifacts; Main mutates canonical workflow state.** This preserves the repository's single-writer constraint until all WorkStore writers are concurrency-safe.

The existing `target_count: 3` remains the bounded implementation/review correction loop *inside one task*. It is not the number of plans, tasks, waves, or global iterations.

## 2. Outer scheduler tick over N plans

Each scheduler tick is a fresh-state transaction:

1. **Discover active scope.** Enumerate active plans; do not pin the loop to one plan unless explicitly scoped.
2. **Read pending refinement work.** `da workflow orient` exposes pending proposals; `da workflow fold-back list --json` exposes staged fold-backs. Refinement does not interrupt an active task mid-stage.
3. **Read capacity.** `da --json workflow slots --plan <comma-separated-scope>` provides `max_parallel`, `occupied`, `available`, and blocked counts. Start no more than `available` tasks.
4. **Read eligible tasks.** When the current wave has no open fanout/delegation setup, run `da --json workflow eligible --plan <scope> --limit <available>`. Use `eligible_tasks` and the conflict-free `max_batch`, not `workflow next`, as the primary selection surface. `next` is only a cross-check.
5. **Revalidate.** Re-read the canonical task row, `depends_on`, `write_scope`, current PR/delegation state, and base lineage immediately before fanout. Eligibility is advisory under stale-state failure modes.
6. **Construct one wave.** Select at most `available` tasks from `max_batch`. A task with missing `write_scope` runs alone. Never combine tasks that overlap by directory-aware prefix.
7. **Fan out with lineage.** Use `da workflow fanout --plan <P> --task <T>` for every selected task. For a downstream with one unmerged upstream, da automatically resolves the dependency's PR head and records `scope.base_branch`, `base_pr`, and `base_task` in the bundle. Distinct unmerged multi-dependency bases must be explicitly sequenced or supplied an unambiguous `--base-branch`; da must refuse ambiguity. There is no `--base-task` CLI flag.
8. **Launch one inner pipeline per selected task.** Export `TASK=<plan>/<task>` and give each run its own code worktree and coordination namespace.
9. **Wave barrier.** Do not query the next `max_batch` while the current wave is still establishing/running delegations. Wait until every task reaches READY, FOLD-BACK, blocked, or a delegation lifecycle boundary that frees its slot.
10. **Serialize reconciliation.** Main processes completed inner runs one at a time: merge-back readback, owner gate, delegation closeout, fold-back routing, checkpoint/score, and fresh `eligible`/`slots` read.
11. **Repeat until quiescent.** Quiescence means no eligible task, no occupied slot that can progress, no lifecycle transition awaiting Main, and no approved refinement action ready to enter a plan. Distinguish true completion from all-blocked or owner-review backlog.

Cross-plan dependencies use qualified IDs (`<plan>/<task>`). The downstream-satisfaction rule is `completed || awaiting_owner_review`, so implementation can continue before an upstream PR merges. The downstream branch must inherit the upstream PR head; a rejected/closed upstream cascades a block and fold-back to dependants.

## 3. Workflow slots manage the whole in-flight lifespan

The configured preference is currently named `execution.max_parallel_workers`, although the design concept is `max_parallel_tasks`:

```sh
da workflow prefs set-local execution.max_parallel_workers <1..32>
```

Set it explicitly. When unset, the slot ledger derives capacity as `clamp(NumCPU-2, 2, 16)`, while the eligible batch resolver can still default to one; explicit configuration keeps both surfaces aligned.

A slot is task-scoped, not agent-stage-scoped:

| Task state | Slot | Meaning |
|---|---:|---|
| `in_progress` | held | implementation, verification, or correction is active |
| `awaiting_review/awaiting_agent_review` | held | agent/reviewer work is active |
| `awaiting_review/awaiting_owner_review` | free | PR waits on the owner; next layered task may start |
| `blocked-on:*` | free | external wait must not consume execution capacity |
| `completed` / terminal blocked | free | task is no longer executing |

A reviewer request-changes, lens rejection, or force-rebase bounce re-acquires a slot. If capacity is full, the task queues rather than exceeding the budget. This lets implementation throughput proceed independently of owner merge cadence while preserving the task's lifecycle identity.

`max_parallel_tasks` and per-task stage concurrency are independent. The slot budget controls concurrent tasks. Each task's resolved execution profile controls verifier/lens ordering and concurrency inside that task. Never multiply the task budget by the number of verifier/reviewer agents.

## 4. Per-task lifecycle through da

The production lifecycle is:

```text
eligible -> fanout -> delegated worker -> inner profile pipeline
         -> merge-back artifact -> agent/owner gate -> delegation closeout
         -> completed | blocked/fold-back | awaiting_owner_review
```

Required invariants:

- Fanout-time `write_scope` conflict checking is the hard backstop after selection-time conflict filtering.
- One writer per code worktree. Shared coordination commits use CAS on `refs/agents/state`.
- Inner workers never run canonical `advance`, `task update`, `merge-back`, `closeout`, or plan mutations concurrently.
- The inner `gate` writes READY/FOLD-BACK evidence and a merge-back draft; it never merges the PR or marks the task complete.
- Main performs delegation readback and closeout after the owner decision, then re-reads slots and eligibility.
- Owner rejection closes/cascade-blocks descendants based on their recorded lineage; it is not treated as ordinary implementation fold-back.

## 5. Fold-back is active-plan input, not a terminal output

Every inner stage may report a refinement signal in its coordination artifact. Only the inner `gate`/Main turns confirmed signals into durable workflow state.

### Task- and plan-scoped observations

First occurrence or local concern:

```sh
da workflow fold-back create --plan <P> --task <T> \
  --observation "<specific observed failure and evidence>" --slug <stable-slug>
```

This creates `classification: small`, routes to `task_note:<P>/<T>`, and updates the canonical task row. Omitting `--task` routes to `plan_summary:<P>`. A stable `--slug` makes retries idempotent; `fold-back update` refines the same observation.

Because the target is a running plan/task, the next scheduler tick sees the note during eligibility and execution-context assembly. On rejection, the task returns to `in_progress`, re-acquires a slot when available, and the inner executor addresses the fold-back reasons first. The fold-back is not a dead-letter or terminal swarm result.

### Cross-cutting observations and proposals

A recurring/cross-project concern enters the human-reviewed meta loop:

```sh
da workflow fold-back create --plan <P> [--task <T>] \
  --observation "<recurring concern + evidence>" --propose --slug <stable-slug>
```

This creates `classification: proposal` and routes to `proposal:obs-<slug>.md` under `~/.agents/proposals/` (observation track). Shared preference changes use the structured proposal track:

```sh
da workflow prefs set-shared <key> <value>
da review
da review show <id>
da review approve <id>        # human-gated; applies + refreshes
da review reject <id> --reason "<reason>"
```

These tracks stay distinct:

- structured YAML proposals: `da review` lifecycle and explicit human approval;
- `obs-*.md` observations: evidence/refinement queue, later promoted through the proposal workflow.

No active worker self-applies a workflow/config refinement discovered during its own run. The outer loop schedules refinement work out-of-band, capped independently (default: at most two refinement tasks in a wave), so product work cannot be starved by self-improvement.

## 6. Meta-loop outputs on every iteration

The loop produces two kinds of output simultaneously:

1. **Product/task output:** code, tests, proof artifacts, PR, merge-back, lifecycle transition.
2. **Workflow-refinement output:** fold-backs, observations, structured proposals, hook outcomes, iteration score, and—after confirmed recurrence—a lesson or refinement task.

At inner-iteration close, Main records observable outcomes through da:

```sh
da workflow checkpoint --log-to-iter <N>
da workflow hook-outcome write <...>
da score iteration <N> --recompute --json
```

`close-task` may compose checkpoint, score, advance, and commit only when its preconditions match the current lifecycle. Do not bypass the canonical `iteration-close` sequence or duplicate its record in prose. Hook/sentinel outcomes are scoring evidence, not optional logs.

Outcome scoring closes the feedback loop: repeated low-scoring or remediated outcomes become evidence for a fold-back/proposal; accepted refinements then change future profiles, prompts, rules, or workflow implementation. The meta loop consumes the same run evidence the product loop produced.

## 7. Profile-driven inner pipeline changes

Keep the existing stages and app-type profile resolution, with these boundary corrections:

- Rename/document the YAML as the **per-task inner pipeline**, not the full loop.
- `profile_resolve` consumes a delegation bundle/task identity and lineage prepared by da; it does not independently choose work.
- `executor`, verifier, and reviewer stages emit evidence only.
- `gate` collates READY/FOLD-BACK, merge-back, proof, and candidate refinement signals. It does not directly mutate the board unless running under Main's serialized reconciliation step.
- A FOLD-BACK causes a bounded re-entry into the same task pipeline. A cross-cutting proposal is emitted in parallel as meta-loop output but does not replace the task correction.
- Dynamic `execution_profile.by_app_type` remains authoritative for verifier sequences, lens sets, lens concurrency, and delivery proof. Outer scheduling never hardcodes app types or stage counts.

## 8. Safety and version constraints

- Build and invoke da from repository HEAD for orchestration. Installed 0.4.2 lacks or diverges on parts of fanout, slot, and base-resolution behavior.
- Treat 0.4.2 `--dry-run` as unsafe for fold-back/review writes. `da review approve` is intentionally side-effectful and runs refresh.
- Until WorkStore concurrency-safe writes land, Main is the only canonical-board writer. CAS protects `refs/agents/state`, but it does not make arbitrary `PLAN.yaml`/`TASKS.yaml` writes safe.
- Never launch write-scope-overlapping tasks in the same wave. Empty scope means serialize, not "no conflict."
- Preserve the iteration-close archived-mergeback sentinel workaround at parent closeout; do not let an archived worker merge-back deadlock the outer loop.

## 9. Implementation split

The full system is two reusable components:

1. **Outer da loop runner:** scheduler ticks, active-plan discovery, `slots`/`eligible`, conflict-free wave launch, delegation lifecycle, serialized reconciliation, fold-back/proposal routing, checkpoint/score, quiescence detection.
2. **Inner profile pipeline:** existing `profile-driven.swarm.yaml`, launched once per selected `<plan>/<task>` and bounded for task-local fold-back.

This split uses da's existing plan store and lifecycle semantics as the source of truth while retaining the dynamic execution profiles already authored for different application types.

## Sources

- `.agents/workflow/specs/workflow-parallel-orchestration/design.md`
- `.agents/workflow/specs/loop-agent-pipeline/decisions.1.md` and `plan-iter.*.md`
- `.agents/workflow/specs/layered-pr-fanout/design.md` §§2.5, 2.8, 3
- `.agents/workflow/specs/meta-loop-operating-model/design.md`
- `.agents/workflow/specs/loop-discipline-stop-hooks/design.md`
- `.agents/workflow/specs/orchestration-companion-stop-hooks/design.md`
- `commands/workflow/eligible_accounting.go`, `delegation.go`, `types.go`, `cmd.go`, `prefs.go`
- `swarm-run/research/loop-slots-nplan.md`
- `swarm-run/research/loop-metaloop-foldback.md`

## Implementation status — LANDED (#389, master b83189a5)
- Explicit per-stage model routing (`model`/`model_family` on StageProfile + schema + resolve-prompt); Claude-family volume, GPT-family blocking cross-family lens; docs de-Codex-harnessed.
- Runtime under `.agents/workflow/runtime/full-loop/`: per-task `profile-driven.swarm.yaml` (7 verifier + 4 routine + 1 GPT cross-family slots) + sequential `reconcile.swarm.yaml`.
- N-plan driver `bin/tests/omp-full-loop` (+ test): slots/eligible/max_batch/fanout waves, barrier, reconcile; crash/stale-lock/fanout-refusal recovery; no vendor CLI.
- Canonical plan `full-loop-orchestration-runtime`: 6/7 tasks complete; `migrate-workflow-state-ref` stays pending, cross-plan-gated on `git-ref-work-backend/document-and-default-git-ref`.
- Temporary `.agents/active/state-ref-transition.md`: worktree state canonical, ref coordination-only until git-ref backend ships.
