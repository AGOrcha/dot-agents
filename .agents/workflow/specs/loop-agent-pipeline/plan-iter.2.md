---
spec: loop-agent-pipeline
iter: 2
purpose: Convert the iter-1 direction into a concrete task graph with explicit write_scope, dependency, and hotspot notes so canonical PLAN/TASKS artifacts can be authored without reopening the architecture.
inputs:
  - plan-iter.1.md
  - decisions.1.md
  - review.1.agent-thoughts.md
  - review.1.human-thoughts.md
status: draft-for-task-authoring
---

# Plan Iteration 2 — loop-agent-pipeline

## Goal

This iteration does three things that iter-1 did not do cleanly:

1. Turns the reviewed architecture into a **task graph with real write scopes**.
2. Incorporates the now-locked decisions from iter-1:
   - D1: `workflow verify record` is the **flag-first canonical writer**
   - D6: external sources move to a **design fork**, not an implementation task in this plan
   - D7: iter-log evolves to **schema v2 with nested role blocks**
3. Resolves the remaining “plan-authoring only” questions inline so the next artifact can be canonical `PLAN.yaml` / `TASKS.yaml`, not another speculative markdown pass.

## Delta From Iter-1

Iter-1 had the right phase model, but the task graph was still missing the details that would make implementation coherent:

- `workflow verify record` needed a concrete CLI contract and owner
- iter-log evolution needed a dedicated task instead of being implied
- fanout/schema tasks were missing required schema files in `write_scope`
- external sources needed to be split out of the main implementation plan
- orchestrator awareness needed to be explicit instead of buried inside fanout
- `commands/workflow.go` and `commands/workflow_test.go` were clearly shared hotspots but not called out as such

Iter-2 fixes those gaps.

## Inline Confirmations Resolved Here

### D2.a — fold-back update identity

Choose **stable human-authored slugs** now. No auto-id round-trip in this plan.

Slug rules:

- task-scoped observation: `<reasoning-path>-<plan-id>-<task-id>`
- plan-scoped observation: `<reasoning-path>-<plan-id>`

Initial reasoning-path vocabulary:

- `coverage-regression`
- `schema-drift`
- `cross-task-conflict`
- `budget-escalation`
- `fold-back-triage`

Examples:

- `coverage-regression-loop-agent-pipeline-p3c-api-verifier`
- `schema-drift-loop-agent-pipeline`

Plan impact:

- `workflow fold-back create` / `update` work in the same task with no extra state store.
- Post-closeout orchestration owns the slug selection; updates refine the same observation instead of creating duplicates.

### D3.a — pre-verifier TDD-fresh gate

Choose the minimal enforceable version now and keep it in the **control plane**, not agent self-reporting.

Gate placement:

- runs in `ralph-pipeline` after the impl-agent handoff exists and before the first verifier is dispatched

Gate passes when either condition is true:

1. at least one test file changed within the task’s touched scope since task-start / task-work commit range
2. `impl-handoff.yaml` includes `tests_unchanged_justified: true`

Initial file heuristics:

- Go: `*_test.go`
- Playwright / JS / TS: `*.spec.ts`, `*.test.ts`, `*.spec.js`, `*.test.js`

Initial retry policy:

- default verifier retry cap: `2`
- config location: `verifier_profiles.<type>.max_retries`

Failure behavior:

- verifier dispatch is blocked
- the failure is surfaced as failed gate `tdd-fresh`
- reviewer / post-closeout flows treat it as a plan-level defect, not an implementation-pass result

Plan impact:

- this behavior lives in the pipeline/control-plane cluster, not inside verifier prompts
- iter-log v2 auto-populates `impl.self_assessment.tdd_refresh_performed`

## Task Graph

### Foundations / independent starts

These can begin immediately once the canonical plan exists:

- `p1-pipeline-control`
- `p2-impl-agent-surface`
- `p3a-result-schema`
- `p8-orchestrator-awareness`
- `p9-sources-design-fork`

### Verifier and reviewer surfaces

These unblock once the base verification contract exists:

- `p3b-unit-verifier` depends on `p3a-result-schema`
- `p3c-api-verifier` depends on `p3a-result-schema`
- `p3d-ui-verifier` depends on `p3a-result-schema`
- `p3e-batch-verifier` depends on `p3a-result-schema`
- `p3f-streaming-verifier` depends on `p3a-result-schema`
- `p4-review-agent` depends on `p3a-result-schema`

### Integration / control-plane convergence

- `p5-iter-log-v2` depends on `p2-impl-agent-surface`, `p3a-result-schema`, `p4-review-agent`
- `p6-fanout-dispatch` depends on `p2-impl-agent-surface`, `p3a-result-schema`, `p3b-unit-verifier`, `p3c-api-verifier`, `p3d-ui-verifier`, `p3e-batch-verifier`, `p3f-streaming-verifier`, `p4-review-agent`, `p8-orchestrator-awareness`
- `p7-post-closeout` depends on `p1-pipeline-control`, `p4-review-agent`, `p5-iter-log-v2`

## Task Definitions

### `p1-pipeline-control`

Title:
`ralph-pipeline` outer loop, plan-scoped break check, verification directory lifecycle, and pre-verifier TDD gate

Depends on:
none

Repo `write_scope`:

- `bin/tests/ralph-pipeline`
- `bin/tests/ralph-orchestrate`
- `commands/workflow.go`
- `commands/workflow_test.go`

Delivers:

- `RALPH_RUN_PLAN` loop mode using `workflow next --json --plan <id>` when supported, otherwise `workflow tasks` fallback
- verification directory creation before verifier dispatch
- pre-verifier TDD-fresh gate and retry-cap wiring
- no Python / narrative output parsing for loop break logic

Verification:

- focused script coverage for no-unblocked-work, one-unblocked-task, and gate-fail branches
- focused Go tests for any `workflow next` / tasks filter change

Notes:

- This is the control-plane entry point for D3.a.
- If `workflow next` lacks plan filtering, the command change lands here.

### `p2-impl-agent-surface`

Title:
Separate repo-side impl-agent surface from loop-worker behavior

Depends on:
none

Repo `write_scope`:

- `bin/tests/ralph-cursor-loop`
- `.agents/prompts/impl-agent.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- repo-local impl-agent overlay / prompt surface with no verifier ownership
- `impl-handoff.yaml` contract clarified to include `write_scope_touched`, `ready_for_verification`, and optional `tests_unchanged_justified`
- loop-worker behavior remains intact for existing Pattern E callers

Verification:

- script smoke for worker invocation selecting the impl-agent surface
- prompt / overlay path checks

Notes:

- The true home-directory agent profile remains an operational artifact; this task only covers repo-owned surfaces and routing.

### `p3a-result-schema`

Title:
Introduce canonical verification-result artifact contract

Depends on:
none

Repo `write_scope`:

- `schemas/verification-result.schema.json` (new)
- `commands/workflow.go`
- `commands/workflow_test.go`

Delivers:

- verifier result schema for `.agents/active/verification/<task_id>/<type>.result.yaml`
- any minimal command-side validation helpers needed by reviewer / merge-back paths

Verification:

- schema fixture coverage
- focused workflow tests for reading / validating result artifacts where applicable

### `p3b-unit-verifier`

Title:
Unit verifier surface and result contract

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `.agents/prompts/verifiers/unit.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- unit verifier role guidance
- explicit `go test ./... -race -count=1 -timeout=300s`
- scoped-test discipline aligned with D12

Verification:

- prompt file lint / path coverage

### `p3c-api-verifier`

Title:
API verifier surface and result contract

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `.agents/prompts/verifiers/api.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- API verifier guidance
- contract/perf artifact expectations
- Playwright API setup expectations

Verification:

- prompt file lint / path coverage

### `p3d-ui-verifier`

Title:
UI E2E verifier surface and result contract

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `.agents/prompts/verifiers/ui-e2e.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- UI E2E verifier guidance
- screenshot diff / accessibility artifact expectations

Verification:

- prompt file lint / path coverage

### `p3e-batch-verifier`

Title:
Batch verifier surface and result contract

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `.agents/prompts/verifiers/batch.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- fixture-driven batch verifier guidance
- expected-vs-actual diff artifact expectations

Verification:

- prompt file lint / path coverage

### `p3f-streaming-verifier`

Title:
Streaming verifier surface and result contract

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `.agents/prompts/verifiers/streaming.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- SSE / WebSocket verifier guidance
- timeout / backpressure / dropped-frame artifact expectations

Verification:

- prompt file lint / path coverage

### `p4-review-agent`

Title:
Review-agent surface plus merged `workflow verify record` decision writer

Depends on:
`p3a-result-schema`

Repo `write_scope`:

- `schemas/verification-decision.schema.json` (new)
- `commands/workflow.go`
- `commands/workflow_test.go`
- `.agents/prompts/review-agent.project.md` (new)
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- review-agent repo-local surface
- `workflow verify record` flag-first contract:
  - `--task-id`
  - `--phase-1-decision`
  - `--phase-2-decision`
  - repeatable `--failed-gate`
  - `--reviewer-notes`
  - `--escalation-reason`
- CLI-owned `review-decision.yaml` output plus lean `verification-log.jsonl` append

Verification:

- focused command tests for valid / invalid enum values and required escalation behavior
- artifact rendering tests for `review-decision.yaml`

### `p5-iter-log-v2`

Title:
Role-owned iteration log schema v2 plus role-aware checkpoint merge

Depends on:

- `p2-impl-agent-surface`
- `p3a-result-schema`
- `p4-review-agent`

Repo `write_scope`:

- `schemas/workflow-iter-log.schema.json`
- `commands/static/workflow-iter-log.schema.json`
- `commands/workflow_iter_log_schema.go`
- `commands/workflow.go`
- `commands/workflow_test.go`

Delivers:

- `schema_version: 2`
- `impl`, `verifiers[]`, `review` nested role blocks
- role-aware `workflow checkpoint --log-to-iter`
- merge validation so each role writes only its own block
- auto-derived fields for review and verifier artifacts

Verification:

- schema sync test
- migration coverage from v1 to v2
- merge / replace / duplicate-role guard coverage

### `p6-fanout-dispatch`

Title:
App-type dispatch and verifier-sequence wiring through plan schema, `.agentsrc`, and delegation bundles

Depends on:

- `p2-impl-agent-surface`
- `p3a-result-schema`
- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p4-review-agent`
- `p8-orchestrator-awareness`

Repo `write_scope`:

- `schemas/workflow-delegation-bundle.schema.json`
- `schemas/workflow-plan.schema.json`
- `schemas/agentsrc.schema.json`
- `src/share/templates/standard/agentsrc.json`
- `commands/workflow.go`
- `commands/workflow_test.go`
- `bin/tests/ralph-orchestrate`

Delivers:

- `app_type` on plan tasks / plan surface
- `verifier_profiles` and `app_type_verifier_map` in `.agentsrc`
- `workflow fanout --verifier-sequence`
- bundle population with verifier sequence resolved from `app_type`

Verification:

- fanout CLI tests
- schema validation tests
- repo-relative prompt / overlay path tests

Notes:

- This is the integration point called out in iter-1 review.
- `p3a` is an explicit dependency because the bundle refers to the verification contract.

### `p7-post-closeout`

Title:
Post-closeout orchestration pass plus `fold-back update`

Depends on:

- `p1-pipeline-control`
- `p4-review-agent`
- `p5-iter-log-v2`

Repo `write_scope`:

- `bin/tests/ralph-closeout`
- `bin/tests/ralph-pipeline`
- `commands/workflow.go`
- `commands/workflow_test.go`

Delivers:

- post-closeout reasoning pass
- `workflow fold-back update`
- stable slug routing for create/update
- grouped task/slice observations rather than noisy duplicates

Verification:

- focused fold-back tests for create/update and plan/task filtering
- script coverage for accept-path closeout plus post-closeout review

### `p8-orchestrator-awareness`

Title:
Make orchestrator prompts and dispatch role-aware

Depends on:
none

Repo `write_scope`:

- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-pipeline`
- `.agents/active/orchestrator.loop.md`
- `docs/LOOP_ORCHESTRATION_SPEC.md`

Delivers:

- orchestrator logic selects impl vs verifier vs review role surfaces deliberately
- `--project-overlay` and `--prompt-file` are used for distinct purposes
- per-task prompt generation replaces the old “same file for both” shortcut

Verification:

- script coverage for bundle generation including distinct overlay/prompt paths

### `p9-sources-design-fork`

Title:
Fork external-sources design doc from the main implementation plan

Depends on:
none

Repo `write_scope`:

- `.agents/workflow/specs/external-agent-sources/design.md` (new)

Delivers:

- design doc only
- no production schema / CLI changes in this plan

Verification:

- design doc completeness against D6.a TOC

## Shared Hotspots And Sequencing Constraints

The graph above is the **logical** dependency graph. It is not the same thing as a safe parallel implementation plan.

### Shared code hotspots

These paths are multi-task hotspots and should not be parallelized naively:

- `commands/workflow.go`
- `commands/workflow_test.go`
- `bin/tests/ralph-orchestrate`
- `bin/tests/ralph-pipeline`
- `docs/LOOP_ORCHESTRATION_SPEC.md`

### Safe parallelism

These are good early parallel candidates:

- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p9-sources-design-fork`

### Forced sequencing cluster

Treat this as a likely sequential or tightly coordinated cluster because it shares command/control-plane code:

1. `p1-pipeline-control`
2. `p4-review-agent`
3. `p5-iter-log-v2`
4. `p6-fanout-dispatch`
5. `p7-post-closeout`

`p8-orchestrator-awareness` can land early, but it also shares `ralph-orchestrate` / `ralph-pipeline`, so it should either land before `p6` or be folded into the same wave.

## Canonical PLAN/TASKS Authoring Notes

When converting this into canonical workflow artifacts:

- keep `p9-sources-design-fork` in the same plan only as a **doc-only placeholder**
- do not reintroduce the old `p7-external-sources` implementation scope
- keep `p4-review-agent` and `p5-iter-log-v2` separate; they touch the same command file but they solve different contracts
- preserve `p8-orchestrator-awareness` as its own task instead of burying it inside fanout
- mark `commands/workflow.go` and `commands/workflow_test.go` as hotspot notes in task `notes`

## Exit Condition For Iter-2

Iter-2 is complete when the canonical `PLAN.yaml` / `TASKS.yaml` can be authored directly from this document without reopening:

- D1 or D7 shape questions
- external-sources scope
- fold-back update identity
- pre-verifier TDD gate ownership
- orchestrator-awareness ownership
