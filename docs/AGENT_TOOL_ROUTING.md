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

The routing is evidence-based, not stylistic:

- The 2026-06-25 graph-chain wave produced new **cross-harness** evidence:
  Codex adversarial review found a `BLOCKER` or `HIGH` issue in every reviewed
  Claude implementation PR in that wave, and it also caught incomplete fixes on
  re-review. The misses were not cosmetic; they included tautological parity,
  papered lifecycle, and unwired gate defects that same-model Claude review did
  not catch.
- In the same wave, Codex implementation work itself was clean, but the harness
  still had friction around self-publish and repo-faithful gate replay. Claude
  was the practical rescue path for publish, per-file gate reruns, and cross-OS
  repro when the delegated Codex slice needed a full local toolchain handoff.

Routing therefore optimizes for the **second brain** where it pays off most
(review) and the **more friction-free execution harness** where it still matters
(implementation closeout, publish, and gate repro).

## Route to Codex first

- **Cross-harness adversarial review on substantive work.** Treat this as the
  recommended blocking gate for non-trivial changes: production Go logic,
  workflow/runtime changes, test or gate semantics, plan/skill/refinement work
  that changes how the system operates, and any fix whose correctness depends on
  more than "tests passed". If Codex returns `BLOCKER` or `HIGH`, do not merge
  until the finding is resolved and re-reviewed.
- **Read-only second-opinion review generally.** Codex is the preferred cited
  reviewer when the goal is to find what the implementing agent missed. Keep the
  read-only boundary explicit in the brief and require file/line citations.
- **Cross-tool resume of rate-limited sessions.** When a Claude session hits a
  rate-limit cap mid-flight, Codex can pick up and continue the work rather than
  stalling the pipeline.
- **Bounded, staged delegation.** Well-scoped tasks with explicit write-scopes and
  a clear stage boundary (impl / verify / review), where Codex's native
  spawn/wait/close delegation fits.

## Keep Claude as primary implementation + rescue

- **Implementation-heavy loops.** Tight edit/`go test`/re-edit cycles, especially
  when the change needs repeated local command execution or branch surgery, still
  fit Claude best.
- **Publish and gate-running rescue.** When a Codex-authored change needs branch
  publish, PR updates, per-file coverage reruns, sonar/native gate replay, or
  cross-OS verification, hand off to Claude rather than pretending the Codex
  harness already proved those steps.
- **Cross-OS / CI-parity repro.** Use Claude for the repro path that depends on
  the repo's real gate machinery (`make gate`, `make gate-cross`, Windows/runtime
  checks, or other local/CI parity work). The 2026-06-25 wave showed that this
  friction is operationally real even when the code change itself is sound.

## Rescue pattern

Use the tools together, in this order:

1. Implement in Claude or Codex.
2. Run Codex cross-harness review as the independent second brain on substantive
   work.
3. If the change was authored in Codex and needs publish or repo-faithful gate
   replay, hand the branch to Claude for gate-running and closeout.
4. Re-run Codex review after any non-trivial fix prompted by that review.

## Notes

- Same-model review is not an acceptable substitute for the cross-harness pass
  when the task is substantive; the graph-chain wave showed that it can miss
  defects that the independent harness catches immediately.
- Keep the read-only boundary explicit in any review or plan task brief routed to
  Codex; front-loading it remains one of the brief-template hardening rules.
- This is still a routing policy, not an ownership silo. If the harness friction
  changes materially, update the policy to match the new measured behavior.
