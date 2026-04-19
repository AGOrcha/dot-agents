# CI Smoke Suite Hardening Reconciliation

Status: In Progress

## Goal

Reconcile the revised `ci-smoke-suite-hardening` plan with the already-landed CI changes so the
canonical task state matches the actual implementation quality before continuing the ISP run.

## Why This Reopened

- The handoff was created before commit `7c733c8`, when the first three tasks were still marked
  completed and the next focus was `release-packaging-and-parity`.
- Commit `7c733c8` intentionally rewound those tasks to `pending` and expanded the plan to require:
  built-binary parity language, scoped workflow-complete semantics, testing-matrix traceability
  when present, and clearer graph-vs-text smoke intent.
- Current CI already covers most of the original work, but it still needs small alignment fixes
  before the first three tasks can honestly return to `completed`.

## Reconciliation Targets

1. Keep the existing built-binary/toolchain checks, but align commands and comments with the
   revised plan language.
2. Keep HOME / AGENTS_HOME isolation, but document the scenario intent and avoid stale handoff
   assumptions about `runner.temp`.
3. Extend command-surface smoke coverage to include scoped completion semantics
   (`workflow complete --json --plan ...`) and annotate matrix traceability expectations.
4. Verify the updated workflow file plus the affected CLI commands locally.
5. Restore canonical task state only after the revised requirements are satisfied.
