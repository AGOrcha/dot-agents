# CRG parity gate soak tracking

The named CI job **CRG parity gate (decommission soak signal)** is the durable
pass/fail history used as the decommission soak-clock evidence for the CRG
parity corpus.

- **Soak-clock start date:** 2026-07-17
- **Requirement:** the gate must pass for 3 consecutive weeks under the design
  `verification_strategy` / §11.6 before t6d may perform final deletion of the
  CRG bridge.
- **Current status:** this change starts the clock. It does not claim that the
  3-consecutive-week soak requirement is satisfied.

## Relationship to the other decommission criteria

This job is §11.4 **criterion 1** evidence (parity-matrix rows green in CI).
It is hermetic by design: a synthetic corpus, both sides driven in-process, no
Python. It therefore cannot speak to criterion 2.

§11.4 **criterion 2** — the behavior-preservation gate over a corpus of REAL
review tasks, compared against the LIVE Python bridge under the same structural
oracles — is the separate `crg-behavior-gate` job, documented in
`testdata/crg-behavior/BEHAVIOR.md`. Criterion 4 (the managed-repo `reads_from:
[crg-bridge]` lockfile sweep) is its own workstream again.

## Hermetic parity tests in the gate

The job runs these exact tests with the in-memory store and pinned corpus; it
requires no Postgres service, Python runtime, or virtual environment:

- `internal/adapters/builtin/crg`
  - `TestPostprocessParity_TenCommitDualRead`
  - `TestPostprocessParity_CatchesDivergence`
- `internal/graphstore`
  - `TestRegression_CrossPathParity_ClampsAreTheSameNumbers`
