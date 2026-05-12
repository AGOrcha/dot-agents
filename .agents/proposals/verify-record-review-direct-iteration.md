# Proposal: Materialize Delegation Contracts for Direct Iterations Too

**Type:** workflow CLI behavior change
**Scope:** project-local (dot-agents binary)
**Status:** queued — surfaced via iteration-close tooling-gap on 2026-05-11

## Observation

`da workflow verify record --kind review --task <id> ...` requires an active delegation contract at `.agents/active/delegation/<task-id>.yaml`. When invoked for a direct iteration (no delegation in flight), the command fails:

```
✗ Error: load delegation contract for task "p3-branch-session-finder":
  open .agents/active/delegation/p3-branch-session-finder.yaml: no such file or directory
```

This forces direct workers to hand-write `.agents/active/verification/<task_id>/review-decision.yaml`, bypassing CLI schema validation and skipping the `mergeReviewIterLog` merge step entirely (the iter-log review block stays empty).

## Why a contract still matters for direct iterations

The contract is **not** redundant just because the direct-execution agent already has iteration context loaded in its window. The contract serves a different purpose: it pins structural guardrails that prevent mid-iteration drift —

- **`scope.write_scope`** — declares which files the iteration is allowed to touch, so an agent that gets distracted by an adjacent issue has a hard fence to bounce off.
- **`verification.feedback_goal`** — the one-line success contract; rereading it mid-iteration is the cheapest way to detect "am I still working on what I started?"
- **`verification.scenario_tags` / `regression_artifacts` / `focused_commands`** — pin which tests must pass before claiming done, so verification doesn't degrade to "I think it works."
- **`closeout.worker_must`** — the canonical checklist the close skill enforces (verify → checkpoint → advance), so direct iterations follow the same discipline as delegated ones.

Some of this is redundant with the agent's already-loaded context — but redundancy is the point. Loaded context is mutable (compaction, refocus, skill chains); the on-disk contract is immutable for the iteration's duration. It's a checkpoint the agent can re-anchor on.

The CLI gap is therefore best closed by making **direct iterations also produce a contract**, not by making the contract lookup optional.

## Proposed change

**Always materialize a contract when starting work on a task — delegated or direct.**

1. **New CLI surface:** `da workflow contract create --task <id> [--from-plan]`
   - Reads the task entry from the owning plan's `TASKS.yaml`.
   - Synthesizes a minimal direct-iteration `delegationBundleYAML` at `.agents/active/delegation/<task-id>.yaml`:
     - `worker.profile: direct` (new sentinel — distinct from `loop-worker`)
     - `scope.write_scope`: copied verbatim from the task
     - `verification.feedback_goal`: derived from task notes' first non-empty paragraph (or task title if notes are empty)
     - `verification.scenario_tags / regression_artifacts / focused_commands`: pulled from the plan's `verification_strategy` if present
     - `closeout.worker_must`: standard direct-iteration checklist (verify → self-review → checkpoint → advance)
     - No `selection`, `prompt`, or `context` block needed for direct work
   - Idempotent: if the file already exists, do nothing (or `--force` to overwrite).

2. **Auto-materialize on first verify-record:** when `verify record --kind review` (or `--kind test --task <id>`) runs and no contract exists, auto-call `contract create` before proceeding rather than failing. Surface a one-line note ("synthesized direct-iteration contract for <task>; ~/.../delegation/<task>.yaml") so the user sees what happened.

3. **Auto-materialize at task-advance start:** when `workflow advance <plan> --task <id> --status in_progress` runs without a contract, do the same auto-materialize. This means the contract is in place as soon as work starts, available for the agent to re-read mid-iteration.

4. **Contract lookup stays required.** The verify/checkpoint/merge-back paths continue to assume a contract exists. They simply now have a guarantee that one always will (because step 2/3 ensures it).

## Acceptance criteria

- `da workflow advance <plan> --task <id> --status in_progress` writes `.agents/active/delegation/<id>.yaml` automatically when none exists, with `worker.profile: direct`, write_scope copied from TASKS.yaml, and a derived feedback_goal.
- `da workflow verify record --kind review --task <id> --phase1-decision accept --phase2-decision accept --summary "..."` succeeds for a direct iteration, with the auto-materialized contract serving as the binding.
- `da workflow checkpoint --log-to-iter N --role review` populates the iter-log review block in both delegated and direct iterations.
- The on-disk contract is identical in shape between delegated and direct work — only the `worker.profile` value distinguishes them. Tooling (delegation-lifecycle, merge-back, fold-back) handles `direct` profile gracefully (no merge-back required for direct work; advance is sufficient).
- Existing delegated workflows are unaffected (contracts written by delegation fanout retain their full content).

## Surfaced from

`.agents/active/verification/p3-branch-session-finder/review-decision.yaml` and `.agents/active/verification/f2-platform-scanner-tests/review-decision.yaml` (telemetry envelope, `improvement_signals.tooling_gap`, both iterations).

## User clarification (2026-05-11)

> "even with direct execution and no delegation, the contract still helps provide some structure during direct executions, while it's slightly redundant for some info as the agent would already have the context loaded helps add guidelines so it stays on track" — confirms the contract-as-guardrail framing above; rules out the "make lookup optional" alternative.
