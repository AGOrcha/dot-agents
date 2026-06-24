# loop-worker (global profile)

**Location:** `~/.agents/profiles/loop-worker.md`  
**Bundle label:** `workflow fanout --delegate-profile loop-worker` stores this name in the delegation bundle; the CLI does not auto-load this file — agents should read it when acting as a bounded worker.
**Execution mode:** legacy/full-slice compatibility only. Do not inject this profile into typed ISP `impl`, verifier, or reviewer stages.

This is the **global** layer of the legacy three-layer model (`docs/LOOP_ORCHESTRATION_SPEC.md`): stable across repos. For the legacy worker, **repo-specific** plans, matrices, hooks, and command cheat sheets may be provided through a full-slice **project overlay** (e.g. `.agents/active/active.loop.md`) passed via `--project-overlay`. **Per-delegation** prompts and context belong in the bundle under `.agents/active/delegation-bundles/<delegation_id>.yaml`.

Typed staged dispatch is separate: the parent/orchestrator resolves shared bounded-stage instructions, a stage-safe product/project overlay, and a named stage-agent or reviewer definition. Typed stages must not load this profile or the legacy full-slice overlay.

## Discipline

- Honor `write_scope`; do not mutate canonical plan state outside the delegated task unless the parent contract allows it.
- Trust canonical `PLAN.yaml` / `TASKS.yaml` / `workflow next` over stale checkpoint prose when they disagree.
- Run focused tests first; broaden only when justified.
- Record a concrete **feedback_goal** per iteration; use **scenario_tags** and classify evidence (e.g. ok, ok-warning, impl-bug, tool-bug, blocked).
- Require **negative-path** tests when the change introduces new failure modes.
- Re-derive the coverage-delta against your diff before push: `write_scope` may omit the tests a change breaks. If a broken asserter sits outside scope, escalate rather than silently expanding it.
- Validate cross-platform before push (reason about GOOS, never hand-join paths, treat skipped/build-tagged platform tests as unverified) and against fresh `origin/master` — green-in-isolation can go red once combined. Run the project's quality gate locally; a green forge check is not proof.

## Staged dispatch boundary

This profile is not a typed stage definition or reviewer lens. In staged
execution:

- named implementation and verifier agents emit their typed handoff/result
  artifacts and stop;
- named reviewer agents emit typed review evidence and stop;
- a deterministic parent-invoked return/aggregation gate produces the
  consolidated decision and merge-back return packet; and
- the parent/orchestrator owns canonical closeout.

Until native staged dispatch is available, this legacy profile may implement
one complete bounded slice and return one merge-back artifact.

## Worker closeout (delegated slice)

In order:

1. `dot-agents workflow verify record …`
2. `dot-agents workflow checkpoint …`
3. `dot-agents workflow merge-back …`

Do **not** run `workflow delegation closeout` as the worker; the **parent**
owns that operation after reviewing merge-back. Do not run `workflow advance`
for delegated work; it is the direct, non-delegated completion path.

## Parent closeout (orchestrator)

After accepting delegate output: `workflow delegation closeout --plan <id> --task <id> --decision accept|reject` — an accepted closeout already advances the canonical task to `completed` (see `commands/workflow/delegation.go` `applyCloseoutDecisionToTasks`), so a separate `workflow advance` call is redundant. `workflow advance` remains the direct, non-delegated completion path.

## Reusable verification metadata (bundle / flags)

Prefer setting these via fanout flags or the delegation bundle when used: `feedback_goal`, `scenario_tags`, regression matrix / artifact paths, higher-layer validation queue path, evidence classification expectations, and sandbox policy for mutating checks — see **Phase 8** in `LOOP_ORCHESTRATION_SPEC.md` in the dot-agents repo.
