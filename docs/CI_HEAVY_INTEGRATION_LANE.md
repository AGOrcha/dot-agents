# CI Heavy Integration Lane

The default PR gate in [test.yml](../.github/workflows/test.yml) stays intentionally narrow:
fast toolchain checks, isolated-home smokes, workflow control-plane coverage, and release-packaging
parity. Heavier checks live in a separate lane so they stay tracked without destabilizing normal PRs.

## Workflow

- Workflow file: [heavy-integration.yml](../.github/workflows/heavy-integration.yml)
- Triggers:
  - nightly schedule: `0 6 * * *`
  - manual dispatch: `workflow_dispatch`

## Active lane

### `container-sandbox`

- Triggered nightly and manually.
- Builds [tests/Dockerfile.sandbox](../tests/Dockerfile.sandbox).
- Runs:
  - `docker build -t dot-agents-sandbox:<run-id> -f tests/Dockerfile.sandbox .`
  - `docker run --rm dot-agents-sandbox:<run-id> dot-agents --version`
  - `docker run --rm dot-agents-sandbox:<run-id> dot-agents --help`

This is intentionally outside the PR gate because container builds are slower, network-sensitive,
and materially heavier than the isolated built-binary smokes in `test.yml`.

## Escalated and deferred heavy checks

These are explicitly tracked here. Some remain deferred; the unstable graph-warm path has been
escalated back into working scope via fold-back rather than parked here as silent debt.

- `graph-warm-health`
  - Status: escalated into working scope.
  - Fold-back: `graph-warm-build-transaction-defect` on
    `kg-command-surface-readiness / kg-freshness-audit`.
  - Why escalated: a fresh isolated `dot-agents kg build` failed with
    `sqlite3.OperationalError: cannot start a transaction within a transaction`, so this is a live
    product defect, not just heavy-lane backlog.
  - Trigger target: manual or scheduled only after the KG build path is reliable on fresh homes.
  - Required secrets/auth: none, but it does require a CI-safe graph build path.

- `credential-bound-agent-integrations`
  - Status: deferred.
  - Why deferred: these flows would require provider credentials or interactive agent sessions.
  - Trigger target: manual only until a stable non-interactive contract exists.
  - Required secrets/auth: provider API keys or agent auth tokens, depending on the integration.

- `long-soak`
  - Status: deferred.
  - Why deferred: long-running or flaky checks should not block ordinary PR feedback.
  - Trigger target: schedule or manual dispatch if the suite becomes deterministic enough.
  - Required secrets/auth: depends on the final suite.

## Testing-matrix note

`.agents/workflow/testing-matrix.yaml` does not exist yet in this repo. When it is introduced,
mirror these lane names there so scenario coverage stays canonical instead of drifting into workflow
comments and docs:

- `container-sandbox`
- `graph-warm-health`
- `credential-bound-agent-integrations`
- `long-soak`
