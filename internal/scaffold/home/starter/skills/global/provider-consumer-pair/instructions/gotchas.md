# Gotchas: Provider Consumer Pair

Common failure points:

## Layer Boundaries

- Do not collapse provider and consumer into one implementation unit just because they live in the same package. The architectural boundary still matters.
- Similar-looking structs across layers can be intentional. Avoid forcing a shared type when the two layers have different responsibilities.

## Import And Coupling Risks

- Avoid introducing circular imports or command-level coupling when the consumer can proceed against a local adapter first.
- A flat package layout may let imports compile even when the design boundary is getting muddled. Preserve the mental separation explicitly.

## Contract Artifacts

- If machine-readable contract files (e.g. `bridge-contract.yaml`, OpenAPI specs, generated stubs) are part of the pattern, generate them early so both sides can rely on the same artifact.
- Treat the contract artifact as the source of truth for the boundary — not the prose in either plan markdown. If the plans disagree with the artifact, reconcile by editing the plans, not by reinterpreting the artifact.
- Do not skip the final integration test. Provider and consumer unit tests passing separately is not enough for a paired-wave slice.

## Stale Bundle Scope

- Pre-validate every file in each side's proposed `--write-scope` against current HEAD before fanout. A bundle authored from a stale task snapshot either spawns a worker on already-merged work or ships against files that no longer exist.
- Use a code-graph or `grep` pass to catch callers the plan's static write_scope list missed; consumer-side callers of provider symbols are the most common omission.
- If the provider task is marked `pending` but a recent merged PR mentions the contract, run `gh pr list --state merged --search '<task-id>'` (or your platform's equivalent) before fanout. Status drifts faster than the orchestrator can closeout.

## Canonical Paths

- Plans live under `.agents/workflow/plans/<plan-id>/` (`<plan-id>.plan.md` + `PLAN.yaml` + `TASKS.yaml`). They are NOT under `.agents/active/`, which holds only transient runtime artifacts (delegation bundles, merge-back, fold-back, iteration logs).
- Scope-evidence sidecars live at `.agents/workflow/plans/<plan-id>/evidence/<task-id>.scope.yaml` — the consumer fanout's `--prompt` should reference the provider sidecar's `decision_locks`.

## Sequencing And Closeout

- Serialize provider before consumer unless the contract artifact is already merged and the two `--write-scope` values are disjoint.
- After the provider's worker runs `workflow merge-back`, the parent must run `workflow delegation closeout --decision accept` to advance task status — `merge-back` alone leaves the task `in_progress`. Otherwise the consumer fanout will see stale eligibility.
