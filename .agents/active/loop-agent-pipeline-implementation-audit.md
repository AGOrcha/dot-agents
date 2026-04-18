# Loop Agent Pipeline Implementation Audit

Date: 2026-04-18

Scope: audit the canonical `loop-agent-pipeline` tasks against the current source tree, canonical task state, and archived merge-back artifacts. Goal: distinguish work that is substantively implemented from work that is only marked complete in plan state.

## Executive Summary

- Total canonical tasks: `14`
- Substantively implemented and complete: `12`
- Partially implemented but marked complete too early: `1`
- In progress and not complete: `1`
- Pure paper-complete tasks with no corresponding implementation found: `0`

Important caveat:

- Several merge-back artifacts have contaminated `files_changed` / `artifact_paths` lists caused by dirty-state carryover during delegation and closeout. This weakens archive provenance for some tasks, but it does not by itself prove the underlying implementation is missing.

## Verdict Key

- `implemented-complete`: intended task work is present in the repo and canonical completion is justified
- `implemented-partial`: meaningful task work landed, but the current runtime still fails important parts of the intended outcome
- `marked-complete-with-weak-evidence`: likely implemented, but archive evidence is noisy enough that completion confidence is lower than normal
- `in-progress`: not complete yet

## Task Audit

| Task | Canonical status | Audit verdict | Notes |
| --- | --- | --- | --- |
| `p1-pipeline-control` | completed | `implemented-complete` | Plan-scoped fanout/next, verification dir lifecycle, TDD gate, verifier retry wiring are present in `commands/workflow.go`, `bin/tests/ralph-orchestrate`, and `bin/tests/ralph-pipeline`. |
| `p2-impl-agent-surface` | completed | `implemented-complete` | `impl-agent.project.md` exists, spec documents impl handoff semantics, and `ralph-cursor-loop` explicitly treats impl-agent as separate from loop-worker. Archive file lists are noisy. |
| `p3a-result-schema` | completed | `implemented-complete` | Canonical verification-result schema, embedded validation, and merge-back result writing are present. |
| `p3b-unit-verifier` | completed | `implemented-complete` | Unit verifier prompt and spec role text landed directly in target files. |
| `p3c-api-verifier` | completed | `implemented-complete` | API verifier prompt and spec role text are present, but merge-back artifact paths are contaminated by dirty state. |
| `p3d-ui-verifier` | completed | `implemented-complete` | UI E2E verifier prompt and spec routing text are present in the intended files. |
| `p3e-batch-verifier` | completed | `implemented-complete` | Batch verifier prompt and spec role text are present, but archive provenance is noisy. |
| `p3f-streaming-verifier` | completed | `implemented-complete` | Streaming verifier prompt and spec role text are present, but archive provenance is noisy. |
| `p4-review-agent` | completed | `implemented-complete` | Review decision schema, `workflow verify record --kind review`, review prompt, and review-decision artifact path are present. |
| `p5-iter-log-v2` | completed | `implemented-complete` | Iter-log schema v2, embedded schema copy, and role-aware checkpoint merge behavior are present. |
| `p6-fanout-dispatch` | completed | `implemented-complete` | `app_type`, `verifier_sequence`, `.agentsrc` mapping, bundle schema, and fanout resolution all landed. Dependency ordering in TASKS was violated historically, but the implementation exists. |
| `p7-post-closeout` | completed | `implemented-partial` | Fold-back create/update and post-closeout audit wiring landed, but the runtime still fails the “clean closeout / safe rerun” outcome in practice. This task is marked complete too early. |
| `p8-orchestrator-awareness` | in_progress | `in-progress` | Still not complete. `ralph-orchestrate` still passes the same file as both `--project-overlay` and `--prompt-file`. |
| `p9-sources-design-fork` | completed | `implemented-complete` | The design doc exists and matches the intended doc-only fork. Merge-back summary includes unrelated iter-log work, but the design task itself is done. |

## Evidence By Task

### `p1-pipeline-control`

Verdict: `implemented-complete`

Direct evidence:

- `commands/workflow.go` includes `workflow next --plan`, `fanout --verifier-retry-max`, and `fanout --skip-tdd-gate`
- `commands/workflow.go` contains `ensureTaskVerificationDir(...)`
- `commands/workflow.go` contains the pre-verifier TDD gate error path
- `bin/tests/ralph-orchestrate` forwards `--plan`
- `bin/tests/ralph-pipeline` documents verification dir creation before dispatch

### `p2-impl-agent-surface`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/impl-agent.project.md` exists and defines `impl-handoff.yaml`
- `impl-agent.project.md` defines `write_scope_touched`, `ready_for_verification`, and `tests_unchanged_justified`
- `docs/LOOP_ORCHESTRATION_SPEC.md` documents the impl-agent role and impl handoff contract
- `bin/tests/ralph-cursor-loop` explicitly states it does not load `impl-agent.project.md` and logs the impl prompt path separately

Audit note:

- Merge-back artifact lists for this task do not cleanly show the intended files, so archive provenance is weaker than normal.

### `p3a-result-schema`

Verdict: `implemented-complete`

Direct evidence:

- `schemas/verification-result.schema.json`
- `commands/static/verification-result.schema.json`
- `commands/verification_result_schema.go`
- `commands/workflow.go` writes merge-back verification artifacts under `.agents/active/verification/<task_id>/merge-back.result.yaml`

### `p3b-unit-verifier`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/verifiers/unit.project.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md` unit verifier role section

### `p3c-api-verifier`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/verifiers/api.project.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md` API verifier role section

Audit note:

- The archived merge-back lists only dirty-state files, not the actual prompt/spec files. The implementation is present, but the archive trail is noisy.

### `p3d-ui-verifier`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/verifiers/ui-e2e.project.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md` UI E2E verifier role section

### `p3e-batch-verifier`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/verifiers/batch.project.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md` batch verifier role section

Audit note:

- Merge-back archive provenance is noisy, but the intended prompt/spec work is present.

### `p3f-streaming-verifier`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/verifiers/streaming.project.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md` streaming verifier role section

Audit note:

- Merge-back archive provenance is noisy, but the intended prompt/spec work is present.

### `p4-review-agent`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/prompts/review-agent.project.md`
- `schemas/verification-decision.schema.json`
- `commands/static/verification-decision.schema.json`
- `commands/review_decision_schema.go`
- `commands/workflow.go` exposes `workflow verify record --kind review`, phase decision flags, failed-gate flags, escalation reason enforcement, and writes `review-decision.yaml`

### `p5-iter-log-v2`

Verdict: `implemented-complete`

Direct evidence:

- `schemas/workflow-iter-log.schema.json`
- `commands/static/workflow-iter-log.schema.json`
- `commands/workflow_iter_log_schema.go`
- `commands/workflow.go` documents and enforces schema v2 nested role blocks

### `p6-fanout-dispatch`

Verdict: `implemented-complete`

Direct evidence:

- `schemas/workflow-plan.schema.json` includes `default_app_type`
- `schemas/workflow-delegation-bundle.schema.json` includes `app_type` and `verifier_sequence`
- `schemas/agentsrc.schema.json` includes `verifier_profiles` and `app_type_verifier_map`
- `commands/workflow.go` resolves `app_type` and `verifier_sequence`
- `commands/workflow_test.go` contains explicit `app_type` / `verifier_sequence` tests

Audit note:

- This task is substantively implemented, but the recorded dependency ordering is historically inconsistent because `p6` was accepted before `p8` was closed.

### `p7-post-closeout`

Verdict: `implemented-partial`

What is implemented:

- `commands/workflow.go` has `workflow fold-back update`
- `commands/workflow.go` enforces `--slug` for update and supports slugged create/upsert behavior
- `bin/tests/ralph-closeout` includes `RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT`
- `bin/tests/ralph-pipeline` forwards `RALPH_POST_CLOSEOUT_FOLD_BACK_AUDIT`

Why this is not fully complete:

- The live runtime still reuses stale delegation bundles on no-op orchestrate passes
- Closeout staging still commits broader active delegation state than the accepted task should own
- The intended “pipeline finishes cleanly and safely reruns” outcome is not yet reliably true in practice

Recommendation:

- Reopen `p7-post-closeout`, or split a follow-up canonical task for stale-bundle reuse and task-scoped closeout staging.

### `p8-orchestrator-awareness`

Verdict: `in-progress`

Evidence it is not done:

- `bin/tests/ralph-orchestrate` still passes the same repo file as both `--project-overlay` and `--prompt-file`
- The intended role-aware orchestrator separation is therefore still incomplete

### `p9-sources-design-fork`

Verdict: `implemented-complete`

Direct evidence:

- `.agents/workflow/specs/external-agent-sources/design.md` exists and serves as the design-only fork

Audit note:

- The archived merge-back summary includes unrelated iter-log sync work from dirty-state carryover. The design-doc outcome itself is still present and complete.

## Cross-Cutting Audit Findings

### 1. Completion state is mostly real

Most completed tasks correspond to real landed implementation or documentation. The plan is not dominated by paper-only completions.

### 2. Archive provenance is noisy

Several merge-back archives do not cleanly describe the true task delta. This affects confidence in auditability, not necessarily in implementation existence.

Most affected tasks:

- `p2-impl-agent-surface`
- `p3c-api-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p4-review-agent`
- `p5-iter-log-v2`
- `p9-sources-design-fork`

### 3. `p7` is the one canonical completion I would not trust as final

The code landed meaningful `fold-back` and post-closeout behavior, but the real pipeline still exhibits exactly the kind of closeout/rerun hygiene failures that task was meant to eliminate.

## Recommended Canonical Follow-Up

1. Reopen `p7-post-closeout` or add a follow-up task for:
   - task-scoped closeout staging
   - no stale-bundle fallback on no-op orchestration
   - hard-stop behavior for repeated fatal worker deaths
2. Keep `p8-orchestrator-awareness` open until `ralph-orchestrate` stops using the same file for both overlay and prompt-file.
3. Treat merge-back `files_changed` as advisory for this plan, not authoritative audit evidence.
