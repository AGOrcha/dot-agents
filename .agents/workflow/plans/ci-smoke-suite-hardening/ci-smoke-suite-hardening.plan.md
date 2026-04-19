# CI Smoke Suite Hardening

Status: Active

## Related specs

Execution should stay consistent with the broader workflow system contracts:

- [LOOP_ORCHESTRATION_SPEC](../../../../docs/LOOP_ORCHESTRATION_SPEC.md) — additive artifact `.agents/workflow/testing-matrix.yaml` as canonical verification targets and scenario coverage; Phase 8 verification metadata (`scenario_tags`, `regression_artifacts`, evidence classification); plan lifecycle and archive preconditions when this bundle eventually completes.
- [WORKFLOW_AUTOMATION_PRODUCT_SPEC](../../../../docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md) — canonical plan layout under `.agents/workflow/plans/<plan-id>/` and repo-local vs user-local split (`HOME` / `AGENTS_HOME` isolation in CI matches that model for automation runs).

Adjacent capability plans (do not block this plan, but CI should stay easy to extend when they land):

- [Graph Bridge Command Readiness](../graph-bridge-command-readiness/graph-bridge-command-readiness.plan.md) — future optional smokes such as `workflow graph query` with real graph fixtures once bridge readiness is satisfied.

## Outcome

Turn CI from a mostly compile-and-basic-smoke gate into a layered verification pipeline that:

- validates the built CLI binary instead of launcher shims
- runs with isolated `HOME` and `AGENTS_HOME`
- covers meaningful workflow, KG, and resource-lifecycle command paths
- validates release packaging before release time
- keeps heavier environment-sensitive integration checks in an intentional lane
- stays alignable with the orchestration spec’s **testing matrix** and **scenario** model so smoke coverage does not drift as untracked one-off steps

## Audited gap

The current workflows and smoke scripts prove some baseline behavior, but they still leave major gaps around:

- formatting, vet, and build hygiene
- built-binary authenticity
- runner-state isolation
- broader CLI surface coverage
- workflow and KG smoke confidence
- release and PR parity (for example `auto-release.yml` still exercises `src/bin/dot-agents` and `go test ./...` without the PR workflow’s `GO_TEST_FLAGS`)

## Canonical alignment (from LOOP_ORCHESTRATION_SPEC)

- **Testing matrix:** When `.agents/workflow/testing-matrix.yaml` exists, treat it as the durable map of verification targets; new PR smoke steps should either reference matrix rows (in comments or task notes) or fold additions back into the matrix via normal fold-back / proposal flow so CI does not become a second hidden source of truth.
- **Orchestrator primitives:** Smokes already touch surfaces such as `workflow orient` and `workflow next`; keep them consistent with `workflow next` / scoped completion semantics (`RALPH_RUN_PLAN`, `workflow complete --json`) so CI does not encode stale selection assumptions.
- **Graph vs text:** Prefer graph-backed commands where the spec routes planner-style questions (`kg bridge`, `workflow graph query`); keep `grep`-only checks out of the default PR gate unless the graph is absent by design for that step.

## Scope

This plan turns the older Markdown-only hardening note into a canonical executable bundle. The waves are:

1. establish the baseline built-binary and toolchain checks
2. isolate smoke environments
3. expand command-surface smokes for workflow, KG, and resource lifecycle
4. align release packaging checks with PR CI (built binary, shared `go test` flags, GoReleaser config validation on the PR path)
5. explicitly place heavy environment-sensitive tests into a separate lane (graph-warm-dependent checks, containers, long soak, vendor credentials) instead of leaving them as forgotten debt

## Concrete parity targets (wave 4)

For `release-packaging-and-parity`, align at minimum:

| Concern | PR (`test.yml`) direction | Release (`auto-release.yml`) gap to fix |
|--------|---------------------------|----------------------------------------|
| CLI under test | Built Go binary after `go build` | Uses `src/bin/dot-agents` shim for version/smoke |
| Go tests | `go test $GO_TEST_FLAGS ./...` with `-race -count=1 -timeout=300s` | Plain `go test ./...` |
| Packaging | Add or ensure `goreleaser check` / snapshot validation so drift is caught pre-merge | GoReleaser only runs at release |

## Heavy integration lane (wave 5)

Candidates to document and optionally wire to `workflow_dispatch` / scheduled workflows:

- Commands that require a warmed or prebuilt graph (for example full `kg health` semantics vs isolated HOME)
- Environment-heavy or credential-bound flows (containers, cloud agents, interactive tools)
- Long-running or flaky suites better suited to nightly than PR gates

Hooks remain non-authoritative per the orchestration spec; the lane is for **tracked** automation, not silent skips.

## Exit condition

The plan is complete when the repo’s main CI paths validate the shipped binary in isolated homes, cover the highest-risk command families, catch packaging drift before `auto-release.yml` is the first place it fails, and remaining verification intent is either in `.agents/workflow/testing-matrix.yaml` or explicitly deferred to the heavy lane with a documented trigger.

## Plan closeout (when all tasks are terminal)

Per LOOP_ORCHESTRATION_SPEC *Plan lifecycle*: reconcile `PLAN.yaml` to `status: completed`, clear `current_focus_task`, archive the bundle under `.agents/history/ci-smoke-suite-hardening/plan-archive/<date>/`, and remove the live bundle from `.agents/workflow/plans/ci-smoke-suite-hardening/` once no active delegations or merge-backs reference this plan.
