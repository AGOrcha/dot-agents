# Proj Mega Salvage And Profile Layering Plan

**Status:** Active
**Started:** 2026-05-25

## Goal

Recover missing durable runtime overlays from the proj-mega lineage without
reviving stale live workflow state, then make role dispatch consistently apply
the shared worker profile and project overlay.

## Findings

- `proj-mega-branch` tip is an ancestor of `master`; the missing loop overlay
  files survive on the descendant
  `feature/windows-da-init-fixes-for-demo-from-proj-mega-branch`.
- `bin/tests/ralph-worker`, `bin/tests/ralph-orchestrate`, and
  `docs/LOOP_ORCHESTRATION_SPEC.md` reference
  `.agents/active/active.loop.md` and `.agents/active/orchestrator.loop.md`,
  but both were absent from `master`.
- Legacy `ralph-worker` prompt assembly includes `active.loop.md` and
  `~/.agents/profiles/loop-worker.md`. Explicit staged paths
  (`impl`, `verifier`, `review`) currently include only their role prompt
  file, so shared profile/overlay policy is not guaranteed at runtime.
- `~/.agents/profiles/loop-worker.md` is a useful donor but currently mixes
  three different concerns: reusable bounded-worker discipline, named review
  lens instructions, and full-worker closeout (`verify` / `checkpoint` /
  `merge-back`) behavior.
- The projected native `loop-worker` agent repeats unconditional
  `/iteration-close` completion behavior, while the staged ISP contract
  requires implementation and verifier workers to emit their typed artifacts
  and stop before merge-back.
- Parent closeout ordering is not yet canonical: the global loop-worker
  profile says advance then delegation closeout, while the staged ISP prompt
  says delegation closeout then canonical advancement.

## Work

- [x] Restore the worker and orchestrator overlay files from the latest branch
  lineage version, adapting only optional `loop-state.md` reads for current
  `master` where that stale live snapshot is intentionally not restored, and
  updating recovered closeout examples to the current flag-based CLI.
- [x] Audit other branch-only `.agents/` artifacts and classify which are
  recoverable versus stale active state.
- [x] Define the dispatch contract: reusable bounded-stage base plus project
  overlay plus named stage/reviewer agent instructions plus delegation bundle.
- [ ] Update staged prompt assembly or native sub-agent registration to apply
  that contract for `impl`, `verifier`, and `review`.
- [ ] Add tests proving each stage receives required shared guardrails without
  duplicating role-specific instructions.
- [ ] Resolve parent acceptance ordering (`advance` versus delegation
  `closeout`) in one canonical contract and its tests.
- [ ] Reconcile the installed `iteration-close` skill examples that still show
  positional `workflow advance` syntax against the current CLI contract.

## Profile Transformation Boundary

Use `~/.agents/profiles/loop-worker.md` as source material, not as the direct
system profile for every stage:

| Existing donor content | Target ownership |
| --- | --- |
| Write scope, canonical task authority, focused evidence, classification metadata, and sandbox discipline | Shared `bounded-stage-worker` base profile |
| Architecture, acceptance, and adversarial review lenses | Separate named reviewer agent definitions |
| Worker `verify` / `checkpoint` / `merge-back` closeout and `/iteration-close` behavior | Legacy/full-slice `loop-worker` only |
| Delegation closeout and canonical advancement | Orchestrator or parent-gate instructions only |

## Native Stage Shape

- `impl-agent`: shared bounded-stage base plus project overlay plus
  implementation artifact contract; emits `impl-handoff.yaml` and stops.
- Typed verifier agents such as `verifier-unit`, `verifier-api`, and
  `verifier-ui-e2e`: shared base plus project overlay plus their specific
  verification contract; emit typed result artifacts and stop.
- Named reviewer agents such as `reviewer-acceptance-invariants`,
  `reviewer-architecture-standards`, and `reviewer-adversarial`: shared base
  plus project overlay plus only their lens instructions; emit review evidence
  and stop. A separate aggregation decision is required for consolidated
  `review-decision.yaml` and merge-back ownership.
- `loop-worker` remains the compatibility/full-slice worker until legacy
  dispatch is explicitly retired; it may retain iteration closeout.

## Materialization Contract

Dot-agents should materialize platform-native agent definitions by composing
the shared bounded-stage base, the repo-local project overlay, and the named
stage definition during refresh/install. The prompt at spawn time should then
contain only the delegation bundle reference and task-specific runtime
requirements. Do not fold project paths or individual stage procedure into
the reusable shared base.
