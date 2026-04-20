# CI Smoke Suite Hardening Completed-Bundle Audit

Date: 2026-04-19

Scope: spec-vs-implementation audit of the live `ci-smoke-suite-hardening` workflow bundle after
it was marked `completed` in canonical state. This audit follows the method in
[`completed-plan-audit-analysis`](../../workflow/specs/completed-plan-audit-analysis/design.md).

## Verdict

`completed-with-doc-drift`

The canonical task set looks substantially implemented in current workflows:

- PR CI validates a built Go binary
- PR CI uses isolated `HOME` and `AGENTS_HOME`
- PR CI covers scoped `workflow complete --json --plan ...`
- PR CI runs packaging validation through GoReleaser checks
- release CI uses the built binary and shared `GO_TEST_FLAGS`
- a named heavy-integration lane exists outside the PR gate

The remaining drift is primarily documentation and reconciliation state, not a missing core CI
capability. Two doc-level contradictions remain:

1. the bundle still carries a live reconciliation note marked `In Progress` even though its cited
   targets now largely appear satisfied by the workflows
2. the heavy-lane doc claims a nightly schedule exists, but the actual `heavy-integration.yml`
   schedule is currently commented out

## Spec Anchors

- [PLAN.yaml](../../workflow/plans/ci-smoke-suite-hardening/PLAN.yaml)
- [TASKS.yaml](../../workflow/plans/ci-smoke-suite-hardening/TASKS.yaml)
- [LOOP_ORCHESTRATION_SPEC.md](../../../docs/LOOP_ORCHESTRATION_SPEC.md)
- [WORKFLOW_AUTOMATION_PRODUCT_SPEC.md](../../../docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md)

## Implementation Anchors

- [test.yml](../../../.github/workflows/test.yml)
- [auto-release.yml](../../../.github/workflows/auto-release.yml)
- [heavy-integration.yml](../../../.github/workflows/heavy-integration.yml)
- [CI_HEAVY_INTEGRATION_LANE.md](../../../docs/CI_HEAVY_INTEGRATION_LANE.md)

## Verification Anchors

- [ci-smoke-suite-hardening.plan.md](../../workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening.plan.md)
- [ci-smoke-suite-hardening-reconcile.plan.md](../../workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md)

## Confirmed Findings

### 1. Built-binary and toolchain checks are present in PR CI

The PR workflow now includes:

- `gofmt` enforcement
- `go vet`
- `go test $GO_TEST_FLAGS ./...`
- `go build -o ./bin/dot-agents`
- a file-type check to confirm the built binary is a real Go binary rather than a shim

Direct evidence:

- [test.yml:78](../../../.github/workflows/test.yml:78)
- [test.yml:87](../../../.github/workflows/test.yml:87)
- [test.yml:90](../../../.github/workflows/test.yml:90)
- [test.yml:93](../../../.github/workflows/test.yml:93)
- [test.yml:96](../../../.github/workflows/test.yml:96)

This satisfies the core intent of `establish-toolchain-and-built-binary-baseline`.

### 2. HOME / AGENTS_HOME isolation is present and consistently documented in PR CI

The PR workflow defines isolated smoke roots and explicitly documents that all smoke steps below
override `HOME` and `AGENTS_HOME`.

Direct evidence:

- [test.yml:54](../../../.github/workflows/test.yml:54)
- [test.yml:116](../../../.github/workflows/test.yml:116)
- [test.yml:128](../../../.github/workflows/test.yml:128)

This satisfies the main intent of `isolate-home-based-smoke-harness`.

### 3. Scoped completion semantics are covered in PR CI

The reconciliation note said the revised plan needed scoped completion coverage via
`workflow complete --json --plan ...`.

That smoke is now present:

- [test.yml:251](../../../.github/workflows/test.yml:251)

This directly addresses one of the reconciliation note's named reopen targets.

### 4. Release packaging and PR parity are mostly aligned

Release CI now:

- builds `./bin/dot-agents`
- verifies the built binary version
- runs `go test $GO_TEST_FLAGS ./...`
- runs built-binary smoke tests under isolated `HOME` / `AGENTS_HOME`

Direct evidence:

- [auto-release.yml:16](../../../.github/workflows/auto-release.yml:16)
- [auto-release.yml:65](../../../.github/workflows/auto-release.yml:65)
- [auto-release.yml:83](../../../.github/workflows/auto-release.yml:83)
- [auto-release.yml:87](../../../.github/workflows/auto-release.yml:87)

PR CI additionally runs `goreleaser check` and snapshot packaging validation:

- [test.yml:108](../../../.github/workflows/test.yml:108)
- [test.yml:112](../../../.github/workflows/test.yml:112)

This is materially consistent with the plan's packaging/parity goal.

### 5. A named heavy lane exists outside the PR gate

The repo has a dedicated `heavy-integration.yml` workflow with a container-sandbox lane, and the
doc tracks additional deferred heavy checks separately from the PR gate.

Direct evidence:

- [heavy-integration.yml:1](../../../.github/workflows/heavy-integration.yml:1)
- [heavy-integration.yml:25](../../../.github/workflows/heavy-integration.yml:25)
- [CI_HEAVY_INTEGRATION_LANE.md:28](../../../docs/CI_HEAVY_INTEGRATION_LANE.md:28)

This satisfies the plan's requirement that heavy/environment-sensitive work live in a named lane
rather than as silent debt in the PR gate.

## Confirmed Drift Points

### 1. The reconciliation note appears stale relative to current workflows

The live reconciliation note still says the bundle is `In Progress` and frames several targets as
not yet restored:

- built-binary parity language
- scoped completion semantics
- testing-matrix traceability expectations

Direct evidence:

- [ci-smoke-suite-hardening-reconcile.plan.md:3](../../workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md:3)
- [ci-smoke-suite-hardening-reconcile.plan.md:14](../../workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md:14)
- [ci-smoke-suite-hardening-reconcile.plan.md:26](../../workflow/plans/ci-smoke-suite-hardening/ci-smoke-suite-hardening-reconcile.plan.md:26)

Current workflows now appear to satisfy most of those points. The reconciliation note therefore
looks more like stale process state than current implementation truth.

### 2. Heavy-lane docs overstate the trigger surface

`CI_HEAVY_INTEGRATION_LANE.md` says the heavy lane is triggered nightly and manually:

- [CI_HEAVY_INTEGRATION_LANE.md:10](../../../docs/CI_HEAVY_INTEGRATION_LANE.md:10)

But the actual workflow has the schedule commented out, so only manual dispatch is live today:

- [heavy-integration.yml:11](../../../.github/workflows/heavy-integration.yml:11)

This is real doc drift and should be reconciled one way or the other:

- either re-enable the schedule
- or update the doc to say the nightly schedule is currently disabled

### 3. Testing-matrix traceability is still interim rather than artifact-backed

The PR workflow tracks scenario families in comments until `.agents/workflow/testing-matrix.yaml`
exists:

- [test.yml:120](../../../.github/workflows/test.yml:120)

The plan allows this interim mode when the matrix does not yet exist, provided the intent is
explicit:

- [PLAN.yaml:10](../../workflow/plans/ci-smoke-suite-hardening/PLAN.yaml:10)
- [PLAN.yaml:12](../../workflow/plans/ci-smoke-suite-hardening/PLAN.yaml:12)
- [CI_HEAVY_INTEGRATION_LANE.md:55](../../../docs/CI_HEAVY_INTEGRATION_LANE.md:55)

So this is not a reopen trigger. It is a documented interim state that should remain explicit.

## Open Questions

1. Should the reconciliation note be archived or rewritten as a resolved historical note now that
   its concrete targets mostly appear satisfied?
2. Does the repo want the heavy lane schedule re-enabled, or should the docs be changed to reflect
   manual-dispatch-only reality?

## Required Follow-Up

1. Reconcile the stale reconciliation note for `ci-smoke-suite-hardening`.
2. Reconcile the heavy-lane trigger docs vs workflow reality:
   - enable the schedule in `heavy-integration.yml`, or
   - update `CI_HEAVY_INTEGRATION_LANE.md` to stop claiming nightly execution.
3. Keep the bundle `completed` unless new evidence shows missing behavior beyond the doc/process
   drift identified here.
