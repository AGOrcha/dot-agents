---
name: "iteration-close"
description: "Use after completing a loop iteration's code changes and tests to persist workflow state via dot-agents CLI. Fixes the recurring 'persisted_via_workflow_commands: no' anti-pattern. Delegated workers: verify → checkpoint → merge-back (not advance). Direct work: verify → checkpoint → `da workflow close-task` (scores, advances, and commits in one call)."
argument-hint: "[--message <summary>] [--task <plan-id>/<task-id>] [--status pass|fail|partial]"
tier: T2
contract:
  reads:
    - "active delegation contract under `.agents/active/delegation/<task_id>.yaml` — to decide merge-back vs advance and resolve <task_id> for verify-record / checkpoint."
    - "test outcome from the iteration (focused + regression tier exit codes / summaries) — feeds `verify record --kind test`."
    - ".agents/active/verification/<task_id>/review-decision.yaml — produced by `/self-review` (per ADR-0002); read by `verify record --kind review` and `mergeReviewIterLog` when `workflow checkpoint --role review` fires."
    - "instructions/{workflow,gotchas,proposal-criteria}.md — per-step rules and failure modes."
    - "templates/self-assessment-line.md — final loop-state self-assessment lines."
  writes:
    - ".agents/active/verification/log.jsonl — appended by `da workflow verify record --kind test` and `--kind review`."
    - ".agents/active/iteration-log/iter-N.yaml — written/merged by `da workflow checkpoint --log-to-iter <N> --role <impl|verifier|review>`. The `--role review` invocation is what populates the iter-log review block from the self-review artifact (closes the dead-coded path that motivated this skill's T2 contract)."
    - ".agents/active/iteration-log/iter-N.score.yaml — written by `da workflow close-task` on the direct path (checkpoint → score → advance → focus → commit); the returned score value + band is rendered back to the operator."
    - ".agents/active/merge-back/<task_id>.md — produced by `da workflow merge-back` on the delegated path."
    - "loop-state.md self-assessment lines (`persisted_via_workflow_commands`, `proposal_queued`) — emitted to the chat narrative for the parent / loop owner to append."
  escape_hatches:
    - "Halt closeout when `/self-review` returns `overall_decision: reject` (see ADR-0003). Do NOT continue to `workflow checkpoint --role review` or merge-back/advance — the rejected work must be fixed and the chain restarted from the failing step. Quote the rejected `reviewer_notes` into the next iteration's loop-state entry."
    - "Route to `workflow fold-back create` when `/self-review` returns `overall_decision: escalate` (`escalation_reason` is required and present in the review-decision.yaml). The chain still records the review verdict via `verify record --kind review --phase1-decision escalate --escalation-reason \"<reason>\"` (the CLI derives the consolidated `escalate` value from the phase decisions), but instead of merge-back/advance the worker emits a fold-back observation citing the escalation_reason verbatim and stops."
    - "Skip `make build-prod` (Step 5) on every iteration — only run it after a stable section / feature is complete and verification is already green. Treat it as a stability checkpoint, not a closeout default."
    - "Skip `verify record --kind review` + `checkpoint --role review` ONLY when no `/self-review` ran this iteration (e.g., a docs-only edit smaller than the self-review heuristic in instructions/proposal-criteria.md). Document the skip in the checkpoint message; the iter-log review block stays at zero values, which is the documented signal for 'review not applicable this iteration'."
---

# Iteration Close

Closes out an **implementation** iteration by persisting workflow state through the dot-agents CLI. Typical chain: `verify record --kind test`, then `/self-review`, then `verify record --kind review`, then `workflow checkpoint --log-to-iter N --role review`, then `workflow merge-back` (delegated) or `da workflow close-task` (direct — one call that scores the iteration, advances the task, refocuses the plan, and commits). Sets `persisted_via_workflow_commands: yes` in the loop-state self-assessment.

**Not this skill:** routing loose orchestrator observations into plan notes or proposals — use `workflow fold-back create` (see `.agents/workflow/specs/meta-loop-operating-model/design.md`). **Delegation handoff** details live in `.agents/active/delegation-bundles/<delegation_id>.yaml` — use `delegation-lifecycle` for fanout through merge-back and parent closeout.

## Workflow

1. **Detect environment**
   Load → `instructions/workflow.md` § Detect Environment
   Determine binary path and project name (differs for dot-agents vs payout). Resolve direct vs delegated mode and the `<task_id>` (from the active delegation contract `.agents/active/delegation/<task_id>.yaml`, or `workflow status` when no delegation is active).

2. **Write the stop-gate sentinel** *(required — before any verify/checkpoint/advance/merge-back action)*
   Load → `instructions/workflow.md` § Write Stop-Gate Sentinel
   Run `da workflow hook-sentinel write iteration-close` once, declaring the plan, task, run ID, agent type, and the closeout artifacts this iteration must produce. The sentinel is what the Stop/SubagentStop gate reads to confirm the closeout contract held — no sentinel means no enforcement.

3. **Record verification (test)**
   Load → `instructions/workflow.md` § Record Verification (test)
   Run `workflow verify record --kind test` with test outcome and summary.

4. **Self-review the change set** *(per ADR-0003 — fires AFTER test verify, BEFORE checkpoint)*
   Load → `instructions/workflow.md` § Invoke Self-Review
   Invoke `/self-review` so it writes `.agents/active/verification/<task_id>/review-decision.yaml` (per [ADR-0002](../../../docs/adr/0002-self-review-output-schema.md)). Then call `workflow verify record --kind review …` so the existing reader path picks up the YAML; then call `workflow checkpoint --log-to-iter <N> --role review` to populate the iter-log review block via `mergeReviewIterLog`. Document the failure modes (reject / escalate / accept) and respect the gating: a rejected review halts the chain.

5. **Write the narrative checkpoint**
   Load → `instructions/workflow.md` § Write the narrative checkpoint
   Run `workflow checkpoint --message "<summary>" --verification-status <pass|partial>` so `workflow log` / `status` show the current iteration outcome. The iter-log `impl` block is written in Step 6 — by `close-task` on the direct path, or an explicit `--log-to-iter N --role impl` on the delegated path.

6. **Close the iteration** *(pick one path)*
   Load → `instructions/workflow.md` § Delegation vs direct closeout, then § Close the iteration (direct) or § Merge-back (delegated)
   - **Direct worker:** run `da workflow close-task <plan-id> --task <task-id> --json` — one call that writes the iter-log `impl` block, scores the iteration, advances the task to `completed`, refocuses the plan to the next eligible task, and commits the workflow state. Render the returned score (value + band) back to the operator. Only run it when the iteration fully completed that YAML task and you are not under an active parent delegation.
   - **Delegated worker:** run `workflow checkpoint --log-to-iter <N> --role impl`, then `workflow merge-back` — do **not** run `close-task`/`advance`; moving the canonical task to `completed` is the parent's call after review.

7. **Refresh the production binary** *(stable section/feature only)*
   Load → `instructions/workflow.md` § Refresh Production Binary
   Run `make build-prod` after a major section or feature is complete and verification is already green.

8. **Queue improvement proposals** *(if worthy candidate found)*
   Load → `instructions/proposal-criteria.md`
   If the iteration produced a gotcha, rule gap, or hook improvement — use the proposal/review loop to queue the canonical proposal artifact and process it with `da review`.

9. **Confirm self-assessment**
   Load → `templates/self-assessment-line.md`
   Output the `persisted_via_workflow_commands` and `proposal_queued` lines for the loop-state block.

> Before running, load → `instructions/gotchas.md` to avoid common failure modes.
