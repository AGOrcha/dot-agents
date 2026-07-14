---
title: Agent Model Routing
description: How OMP routes work across Claude, GPT, and future model families.
sidebar:
  order: 12
---

# Agent model routing through OMP

dot-agents uses **OMP as the execution harness** for both Claude- and GPT-family
models. Model selection and harness selection are separate concerns:

- `stage_profiles.<stage>.<slug>.model` selects a concrete OMP model.
- `stage_profiles.<stage>.<slug>.model_family` records the semantic family used
  to enforce reviewer diversity.
- Choosing a GPT-family model through OMP does **not** invoke the Codex CLI and
  does not inherit Codex CLI sandbox, writable-root, or permission-whitelist
  behavior.
- Codex CLI guidance applies only to legacy or explicit code paths that launch
  the `codex` binary directly.

This supersedes the older assumption that model-family diversity required
switching agent harnesses.

## Routing policy

The default balances available capacity with independent review:

- **Claude-family through OMP:** implementation, routine verification, routine
  review lenses, orchestration, and other high-volume stages.
- **GPT-family through OMP:** the blocking cross-family adversarial lens and
  selected independent second-opinion work.
- **Additional families:** allowed without schema changes. `model_family` is an
  open identifier; diversity gates compare family identity rather than a closed
  vendor enum.

Every stage used by the full-loop runtime must resolve a non-empty `model` and
`model_family`. A blocking diversity gate must use a family different from the
implementer/routine-review family. The named
`cross-harness-adversarial` slug is retained for compatibility, but its contract
is now **cross-model-family under OMP**, not cross-CLI dispatch.

## Why route at all

Measured review evidence shows that an independent model family catches defects
that same-family implementation and review can jointly miss. The invariant is
therefore model-family disagreement, not a particular binary, sandbox, or vendor
permission model.

The historical runs below used Codex CLI as the available GPT-family second
brain. Their defect-finding evidence remains valid; their mechanical
Codex-sandbox constraints do not describe GPT-family stages running through OMP.

## Required blocking gate

- Substantive implementation work receives a read-only, cited adversarial review
  from a model family different from the implementation family.
- Any BLOCKER/HIGH finding rejects the task.
- The gate re-runs after every fix batch.
- If the configured cross-family model cannot run, the gate blocks explicitly;
  it does not silently pass or degrade to same-family review.

## Historical cross-model evidence

This section records dated, first-hand routing evidence. It is the empirical basis
for the BLOCKING-gate recommendation above.

### 2026-06-25 graph-chain wave

Codex adversarial review caught a BLOCKER or HIGH in every Claude-impl PR reviewed
in this wave:

- **Tautological parity** — a test that compared an output to itself, proving nothing.
- **Papered lifecycle** — state-machine transitions that were documented but never
  enforced by the code.
- **Unwired gate** — a quality gate wired into the spec but never called at runtime.

Same-model (Claude) review of the same PRs missed all three. Codex also caught
**incomplete fixes** on a re-review pass after the first round of corrections —
establishing that the gate must re-run after every fix batch, not just once.

### 2026-07-04 — PR #341 (agent-ops-hardening p4)

PR #341 implemented a mechanical asserting-test-scope gate for `da workflow fanout`.
Before the Codex adversarial gate ran, the PR had five green CI checks (build and
test on macOS, Ubuntu, and Windows; lint; Sonar) and worker-reported clean metrics
(gocognit max 13, 96.7% coverage, clean vet/gofmt).

The Codex gate returned **NOT-SOUND** with five correctness bugs green CI had not
caught:

1. **File-scoped write_scope misclassified** — same-package EXPAND never fired; the
   test only asserted "no error" so it passed blind.
2. **Directory-scope symbol enumeration non-recursive** — major false negatives on
   nested packages.
3. **Raw-byte regex matching** — identifiers in comments, strings, and common method
   names were flagged incorrectly.
4. **First-match-only recording per test file** — a same-directory EXPAND could mask
   a cross-package REFUSE.
5. **Tests too weak to prove the contract** — coverage numbers were high but
   assertions did not exercise the behavioral boundaries.

The same session also saw Codex catch a TOCTOU creation-safety bug (a store opener
that could create the DB it was supposed to open read-only), an auth false-positive
classifier, and cross-OS error-semantics gaps — all past green CI. These are
corroborating instances of the same pattern.

### Pattern summary

Green CI plus same-family self-review is not sufficient to catch correctness bugs
on substantive implementation work. Independent cross-model-family adversarial
review closes the blind spot. The gate must be:

- **Blocking, not advisory** — the pattern reproduced across multiple waves and
  unrelated features.
- **Re-run after every fix batch** — incomplete fixes are a real failure mode.
- **Explicitly routed** — the resolved stage records both concrete OMP model and
  semantic family.

## Legacy direct-CLI note

The dated evidence above came from Codex CLI runs. Direct Codex invocations still
need explicit read-only scope, sandbox writable-root configuration, and the
platform's permission model. Those constraints are **not applicable** when OMP
runs a GPT-family model. Do not branch orchestration logic on `claude` versus
`codex` binaries for the full-loop runtime.
