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

- **Adversarial / second-opinion review.** Read-only, cited code review where the
  goal is to find what the implementing agent missed. Codex must stay read-only
  on these tasks and cite the file and line for every finding.
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

Once the environment taxes are fixed at the source (Codex sandbox `GOCACHE`/
`GOTMPDIR` writable; the `~/Documents` TCC move; Sonar scanner exclusions), this
build-test work is no longer Claude-only — route it to **either** tool by
availability and load.

## Notes

- Keep the read-only boundary explicit in any review or plan task brief routed to
  Codex; front-loading it is one of the brief-template hardening rules in the same
  design (§3, P2).
- This routing is a policy, not a hard gate; it should track the environment-tax
  state above and be revisited when those fixes land.
