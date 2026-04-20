# CI Smoke Suite Hardening Reconciliation

Status: Resolved Historical Note

## Goal

Record why this reconciliation note existed and what later workflow updates appear to have
resolved, so the bundle no longer carries a live `In Progress` note that overstates current CI
drift.

## Historical Reason This Existed

- The handoff was created before commit `7c733c8`, when the first three tasks were still marked
  completed and the next focus was `release-packaging-and-parity`.
- Commit `7c733c8` intentionally rewound those tasks to `pending` and expanded the plan to require:
  built-binary parity language, scoped workflow-complete semantics, testing-matrix traceability
  when present, and clearer graph-vs-text smoke intent.
- Current CI already covers most of the original work, but it still needs small alignment fixes
  before the first three tasks can honestly return to `completed`.

## Original Reconciliation Targets

1. Keep the existing built-binary/toolchain checks, but align commands and comments with the
   revised plan language.
2. Keep HOME / AGENTS_HOME isolation, but document the scenario intent and avoid stale handoff
   assumptions about `runner.temp`.
3. Extend command-surface smoke coverage to include scoped completion semantics
   (`workflow complete --json --plan ...`) and annotate matrix traceability expectations.
4. Verify the updated workflow file plus the affected CLI commands locally.
5. Restore canonical task state only after the revised requirements are satisfied.

## Current Readback

Later workflow and audit readback indicates the main implementation targets above are now present:

- PR CI validates a built Go binary, runs `gofmt`, `go vet`, and `go test $GO_TEST_FLAGS ./...`
- PR CI uses isolated `HOME` and `AGENTS_HOME` across the smoke steps
- PR CI includes scoped `workflow complete --json --plan ...` coverage
- PR CI runs `goreleaser check` and snapshot packaging validation
- release CI uses the built binary, shared `GO_TEST_FLAGS`, and isolated built-binary smokes
- a named heavy-integration lane exists outside the PR gate

The remaining drift identified by the completed-bundle audit is doc/process drift, not a clear
implementation gap:

- this reconciliation note itself was left live after the workflows caught up
- `docs/CI_HEAVY_INTEGRATION_LANE.md` still says the heavy lane runs nightly, while the current
  cron trigger in `.github/workflows/heavy-integration.yml` is commented out

## How To Use This Note Now

- Treat this file as historical context for why the bundle briefly looked over-claimed.
- Do not treat it as evidence that `ci-smoke-suite-hardening` implementation is still incomplete.
- Use the completed-bundle audit for the current disposition and remaining doc-level follow-up.
