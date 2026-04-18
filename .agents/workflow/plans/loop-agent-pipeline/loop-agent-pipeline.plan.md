# Loop Agent Pipeline Plan

Status: Active

## Outcome

Implement the role-pure loop-agent pipeline so the repo can:

- dispatch separate impl, verifier, and review agent surfaces
- persist typed verification artifacts and reviewer decisions
- enforce the pre-verifier TDD-fresh gate in the control plane
- upgrade iteration logging to schema v2 with nested role blocks
- run a post-closeout pass that can create or update fold-back observations

This document is the human-readable view of the canonical plan in [`PLAN.yaml`](./PLAN.yaml) and [`TASKS.yaml`](./TASKS.yaml). The exploratory design work is already done in `specs/loop-agent-pipeline`; this plan should be executed, not reopened.

## Locked Inputs

The plan carries these decisions forward as settled:

- `D1`: `workflow verify record` is the flag-first canonical writer for review decisions.
- `D2.a`: fold-back updates use stable human-authored slugs rather than an auto-id store.
- `D3.a`: the TDD-fresh gate is enforced in the control plane before verifier dispatch.
- `D6`: external sources are forked into a design-doc task, not part of the implementation path here.
- `D7`: iter-log v2 uses nested `impl`, `verifiers[]`, and `review` role blocks.

## Execution Shape

### Foundations

These tasks can start immediately after plan creation:

- `p1-pipeline-control`
- `p2-impl-agent-surface`
- `p3a-result-schema`
- `p8-orchestrator-awareness`
- `p9-sources-design-fork`

### Role Surfaces

These depend on the shared verification result contract in `p3a-result-schema`:

- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p4-review-agent`

### Integration Cluster

These converge the pipeline after the role surfaces exist:

- `p5-iter-log-v2`
- `p6-fanout-dispatch`
- `p7-post-closeout`

## Task Catalog

### `p1-pipeline-control`

Own the outer `ralph-pipeline` loop, plan-scoped break checks, verification directory lifecycle, and the pre-verifier `tdd-fresh` gate. This is where the pipeline stops relying on narrative parsing and starts using workflow-native plan filtering or a typed fallback.

### `p2-impl-agent-surface`

Split the repo-owned impl-agent prompt surface from existing `loop-worker` behavior. Clarify the `impl-handoff.yaml` contract so verification can reason about touched scope, readiness, and justified no-test-change cases.

### `p3a-result-schema`

Create the canonical verification-result schema used by all verifier agents. This task is the contract anchor for every verifier and for reviewer ingestion.

### `p3b` through `p3f`

Define the five verifier role surfaces:

- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`

Each task adds repo-local prompt guidance and artifact expectations for its verifier type while reusing the shared result schema from `p3a`.

### `p4-review-agent`

Define the repo-local review-agent surface and implement the merged `workflow verify record` path. The CLI must validate structured flags, derive the overall decision, write `review-decision.yaml`, and append the lean global verification log entry.

### `p5-iter-log-v2`

Upgrade the iteration log schema to version 2. Logging becomes role-owned, with explicit `impl`, `verifiers[]`, and `review` blocks and role-aware merge semantics in `workflow checkpoint --log-to-iter`.

### `p6-fanout-dispatch`

Wire `app_type`, `verifier_profiles`, `app_type_verifier_map`, and `workflow fanout --verifier-sequence` through plan schema, `.agentsrc`, delegation bundles, and orchestrator dispatch. This is the main plan-schema and bundle integration hotspot.

### `p7-post-closeout`

Add the post-closeout reasoning pass plus `workflow fold-back update`. This task turns stable slugs into create-or-update behavior and prevents noisy duplicate observations during convergence. The closeout lane also needs a post-archive checkpoint commit so archived merge-back artifacts, canonical `PLAN.yaml` updates, and verification records do not leave the repo dirty after `ralph-closeout` completes.

### `p8-orchestrator-awareness`

Make orchestrator dispatch explicitly role-aware. `--project-overlay` and `--prompt-file` stay distinct, and the old shortcut of passing the same file for both must be removed.

### `p9-sources-design-fork`

Keep external sources alive only as a doc-only design fork. This task writes the design artifact and deliberately does not pull registry or source-package implementation work back into this plan.

## Hotspots And Sequencing

The logical dependency graph is wider than the safe implementation graph.

Shared hotspots:

- `commands/workflow.go`
- `commands/workflow_test.go`
- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-pipeline`
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Good early parallel candidates:

- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p9-sources-design-fork`

Forced sequencing cluster:

1. `p1-pipeline-control`
2. `p4-review-agent`
3. `p5-iter-log-v2`
4. `p6-fanout-dispatch`
5. `p7-post-closeout`

`p8-orchestrator-awareness` should land before or alongside `p6-fanout-dispatch`, not after it.

## Verification Expectations

Execution is not complete until the plan has:

- focused command tests for workflow CLI changes
- schema fixtures, sync checks, and migration coverage where schemas move
- `ralph-*` script coverage for control-plane branches
- prompt or path checks for repo-local agent surfaces
- a final `go test ./...` pass before merge

## Exit Condition

The plan is complete when the loop-agent pipeline can run end-to-end with typed verifier and reviewer artifacts, role-aware dispatch, iter-log v2 persistence, and post-closeout fold-back updates without reopening the architectural questions already settled in the spec set.
