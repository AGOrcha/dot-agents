# Delegation bundle ↔ contract artifact divergence

**Status:** observation → proposal draft
**Source:** PR #40 (seam-interface-di) loop runs, 2026-05-20
**Type:** project-local — targets `da workflow` CLI commands

## The observed gap

The loop-worker authoring flow produces a delegation **bundle** at:

```
.agents/active/delegation-bundles/<delegation_id>.yaml
```

The closeout CLI commands (`da workflow verify record --task`, `da workflow merge-back --task`, and the iteration-close skill's chained calls) read a delegation **contract** at:

```
.agents/active/delegation/<delegation_id>.yaml
```

These are two distinct files in two distinct directories. **Nothing materializes the contract from the bundle automatically.** Every loop-worker in PR #40 (install, review, add, atomic-convergence) noted the same gap and hand-authored the contract from the bundle's metadata so the closeout CLI could resolve the task. The install worker captured it first; the convergence worker explicitly listed it in their closeout artifacts: *".agents/active/delegation/seam-atomic-convergence.yaml (created — no contract existed yet)"*.

## Why this looks like implementation drift

The two artifact paths share an ID space and serve adjacent purposes. The bundle is the *input* the worker reads; the contract is the *state* the CLI tracks. A reasonable design would either:

- Treat the bundle as the source of truth and have the CLI read from `delegation-bundles/` directly (deleting the parallel `delegation/` directory and re-anchoring `verify record` / `merge-back` on the bundle path), OR
- Have the orchestrator's bundle-creation step (or the `workflow fanout` command) auto-materialize the contract alongside the bundle, deriving it from the bundle's `task`, `write_scope`, `verification_required`, and `merge_back` fields.

Either is a single-place fix. The current state — two artifacts, no link between them, every worker hand-authors the contract — is the failure mode neither design intends.

## Recommendation

Option B (auto-materialize) is the smaller change and preserves both file shapes for whatever downstream tooling depends on them:

1. When `workflow fanout` writes a bundle to `.agents/active/delegation-bundles/<id>.yaml`, it also writes a contract to `.agents/active/delegation/<id>.yaml` derived from the bundle. The derivation is mechanical: copy `task.id`, `task.stage`, `write_scope`, `verification_required`, `merge_back.artifact`; omit the bundle's worker-facing content (`known_gotchas`, `decision_locks`, `required_reads`, `feature_thread`).
2. If a worker creates a bundle without going through `workflow fanout` (as happened in PR #40 — bundles were hand-written by the orchestrator), the contract write should be either:
   - A flag on bundle creation (`da workflow fanout --bundle <path>` materializes the contract from a pre-existing bundle), or
   - A standalone helper (`da workflow delegation contract-from-bundle <bundle-path>`) that the orchestrator or worker can invoke when a contract is missing.

## Why this matters more than the symptom suggests

Every worker hand-authoring the contract is a silent variance surface. The four contracts created during PR #40 are not byte-identical — each worker chose slightly different field shapes based on what felt necessary to make the CLI happy. A canonical derivation removes that variance and makes the contract a true projection of the bundle.

## Open questions

- Is there an existing reason to keep the two artifacts conceptually separate (e.g. one is committed to git, the other is not; one has a different lifetime; one is owned by the worker, the other by the orchestrator)? If so, the proposal should preserve that distinction rather than collapse it.
- Should the contract carry a back-reference to the bundle (`bundle_path: ...`) so closeout commands can resurface the worker-facing fields for debugging?
- Where does this work belong in the workflow plan stack? It feels like a small CLI extension; could land as a single PR against the `loop-runtime-refactor` plan or as a new tiny plan.

## References

- The four PR #40 loop-worker merge-backs at `.agents/active/delegation/seam-*.merge-back.md`.
- `~/.agents/profiles/loop-worker.md` — the global profile that names both paths but does not bridge them.
- `commands/workflow/fanout*.go` — the likely implementation site for the contract-materialization extension.
