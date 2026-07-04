---
title: Agent Tool Routing
description: When to route work to Claude versus Codex across the agent toolchain.
sidebar:
  order: 12
---

# Agent tool routing (Claude vs Codex)

This document records how work is routed between the two agent toolchains used on
dot-agents: **Claude** (the primary implementation agent) and **Codex** (the
secondary review agent). It is the concise operational form of the routing
decision in the agent-ops-hardening design (§3, P3.12); see
[`.agents/workflow/specs/agent-ops-hardening/design.md`](../.agents/workflow/specs/agent-ops-hardening/design.md)
§1 and §3 for the source framing and the session-transcript evidence behind it.

## Why route at all

The two toolchains have measured, complementary strengths and weaknesses, not
just preferences:

- **Codex** showed clean cross-tool resume of rate-limited Claude sessions,
  impeccable read-only adversarial review (it caught a `sync.go` lock-writer and
  a `docsaccess/client.go` HTTP-token leak), and native spawn/wait/close staged
  delegation. Its dominant mechanical tax was its sandbox: it could not write the
  default Go build cache, producing more than a thousand `GOCACHE=/tmp`
  workarounds, and rate-limit caps with no failover once killed a pipeline.
- **Claude** carried the heavy iterative build-test loops in this run, but paid
  its own environment taxes (the macOS `~/Documents` TCC lock; the Sonar pre-push
  scanner flaking on `dist/`/`.scannerwork`).

Routing is therefore about playing to each tool's strength and away from its tax,
until the environment fixes (agent-ops-hardening P0) remove the taxes — after
which either tool can take either kind of work.

## Route to Codex

- **Adversarial / second-opinion review — recommended as a BLOCKING gate on
  substantive impl work.** Read-only, cited code review where the goal is to find
  what the implementing agent missed. Codex must stay read-only on these tasks and
  cite the file and line for every finding. The gate must re-run after every fix
  batch; catching incomplete fixes on re-review is part of its measured value (see
  [Cross-harness evidence](#cross-harness-evidence) below).
- **Cross-tool resume of rate-limited sessions.** When a Claude session hits a
  rate-limit cap mid-flight, Codex can pick up and continue the work rather than
  stalling the pipeline.
- **Bounded, staged delegation.** Well-scoped tasks with explicit write-scopes and
  a clear stage boundary (impl / verify / review), where Codex's native
  spawn/wait/close delegation fits.

## Keep on Claude

- **Heavy iterative Go build-test loops.** Tight edit/`go test`/re-edit cycles —
  the work that depends on a fast, writable local toolchain — stay on Claude
  while Codex's sandbox cannot write the default Go cache and Claude's own env
  taxes are being fixed.
- **Publishing and running the per-file coverage gate.** Even when Codex implements
  a slice cleanly, Codex's sandbox cannot self-publish (push, open PRs, run the
  per-file gate tool). Claude handles the land-it step. This is the current sandbox
  tax, not a capability judgment — route this step to either tool once the tax is
  removed.

Once the environment taxes are fixed at the source (Codex sandbox `GOCACHE`/
`GOTMPDIR` writable; the `~/Documents` TCC move; Sonar scanner exclusions), the
build-test and publish steps are no longer Claude-only — route them to **either**
tool by availability and load.

## Cross-harness evidence

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

Green CI plus same-model self-review is not sufficient to catch correctness bugs on
substantive impl work. Independent cross-model adversarial review (Codex) closes
the blind spot. The gate must be:

- **Blocking, not advisory** — the pattern has reproduced across multiple waves and
  PRs on unrelated features.
- **Re-run after every fix batch** — incomplete fixes are a real failure mode.

## Notes

- Keep the read-only boundary explicit in any review or plan task brief routed to
  Codex; front-loading it is one of the brief-template hardening rules in the same
  design (§3, P2).
- The adversarial-review gate for substantive impl work is now a **blocking-gate
  recommendation**, not a soft preference. The environment-tax routing (Codex
  sandbox / Claude TCC lock) remains conditional and should be revisited when those
  taxes are removed.
