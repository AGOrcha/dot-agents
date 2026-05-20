# Tracking: interim `.venv` code-review-graph CI install

Status: tracked debt (user-directed 2026-05-17, from PR #16 review Low-3)
Scope: project-local

## What

`.github/workflows/test.yml` installs `code-review-graph` into a
repo-root `.venv` as **"interim coverage until the in-process CRG graph
adaptor lands"**. Self-documented intentional debt; needs tracking so it
gets removed on schedule rather than ossifying.

## Status of the two Low-3 temporary hacks

- `auto-release.yml` `workflow_dispatch` — **DONE / removed** via PR #23
  (added in #22 only to recover the failed v0.3.0 release).
- `.venv` CRG install in `test.yml` — **STILL PRESENT** (this item).
  The only remaining temporary CI hack.

## Removal condition

Remove the `.venv` install step once the **in-process CRG graph
adaptor** lands (the graphstore CRG bridge replacing the subprocess /
external-venv dependency for coverage). Until then the step is required
for the graph-bridge tests to run real CRG in CI.

## Action

No code change now (the step is load-bearing until the adaptor exists).
This file is the durable tracker; graduate to a plan/task when the
in-process adaptor work is scheduled. Cross-ref:
`testcontainers-separate-module.md` (same "CI heaviness" theme, distinct
lifecycle).
