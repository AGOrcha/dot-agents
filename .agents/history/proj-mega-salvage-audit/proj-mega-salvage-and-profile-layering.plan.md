# Proj Mega Salvage And Profile Layering Plan

**Status:** Active
**Started:** 2026-05-25

## Goal

Recover missing durable runtime overlays from the proj-mega lineage without
reviving stale live workflow state, then make role dispatch consistently apply
parent-resolved stage instructions and a stage-safe project overlay.

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
  file, so shared stage-instruction/overlay policy is not guaranteed at runtime.
- `~/.agents/profiles/loop-worker.md` is a useful donor but currently mixes
  three different concerns: reusable bounded-worker discipline, named review
  lens instructions, and full-worker closeout (`verify` / `checkpoint` /
  `merge-back`) behavior.
- The projected native `loop-worker` agent repeats unconditional
  `/iteration-close` completion behavior, while the staged ISP contract
  requires implementation and verifier workers to emit their typed artifacts
  and stop before merge-back.
- The old `origin/feature/PA-cursor-projectsync-phase1-extract-293f` lineage
  supplies four still-live proposal inputs:
  `agent-context-resolution-architecture.md`,
  `config-explain-live-surface.md`, `scope-routed-da-review.md`, and
  `verify-record-review-direct-iteration.md`; all are restored here.
  The branch graph-adapter, app-type-discovery, and plan-archive proposals
  have canonical spec or implemented replacements and are not revived as
  pending queue entries.
- `.agents/workflow/specs/app-type-profiles/design.md` was already present
  byte-identically on `master`: its `profile` means a named, versioned
  pipeline configuration (`verifier_chain`, `review_kind`, `graph_backend`),
  not the reusable instructions injected into a stage agent.
- Parent closeout instructions were not canonical: the global loop-worker
  profile says advance then delegation closeout, while the staged ISP prompt
  formerly described closeout then advancement. Neither two-command sequence
  matches the CLI contract.
- `workflow delegation closeout --decision accept` already reconciles
  canonical task completion. The scripted parent path redundantly followed it
  with `workflow advance`; this pass removes that duplicate mutation for
  contracted delegated work.
- `.agents/active/active.loop.md` was named as a general project overlay but
  contains implementation-only and `/iteration-close` procedure; this pass
  labels it legacy/full-slice and prevents default injection into typed staged
  agents.
- Before this pass `bin/tests/ralph-orchestrate` defaulted generated bundles
  to that legacy overlay. Staged auto-fanout now emits a stage-neutral prompt
  with no implicit legacy overlay; a materialized stage-safe overlay remains
  future work.
- `~/.agents` is the canonical `agents-config` checkout and already has dirty
  edits in `profiles/loop-worker.md` and the projected `loop-worker` agent.
  Normalize that checkout in a separate, conflict-aware pass rather than
  overwriting in-progress configuration work here.
- Historical pipeline decision D9 assigned merge-back to a proposed verifier
  aggregate that was not implemented. The current staged runtime has its
  consolidated review stage emit the return artifact as compatibility
  behavior. The target named-reviewer design has independent reviewers emit
  evidence and a deterministic parent-invoked return gate emit the
  consolidated decision and merge-back packet; parent-owned closeout alone
  reconciles accepted completion.
- Generated bundle `closeout.worker_must` still describes legacy full-slice
  worker duties. A staged contract needs per-stage closeout metadata or an
  execution-mode split before typed children can treat that field as native
  instructions.
- The app-type profile draft contradicted its graph-adapter contract by
  continuing to author derived `impact_radius_kind` values in examples and
  composite rules. This pass corrects that drift and adds planning inputs for
  an effective resolved execution manifest.
- `external-agent-sources` defines OCI media types for agent, skill,
  verifier, and registry bundle artifacts, but not for an app-type profile.
  Profiles and overlay selection should remain Tier 1 config; executable
  stage components use existing Tier 2 types, and registry bundles must not be
  confused with task-local delegation bundles.
- `org-config-resolution` defines organization-to-runtime precedence but does
  not yet bound staged dispatch fields. Stage-agent, reviewer, overlay,
  verifier, return-gate, execution-mode, and trust selections need inherited
  locks and auditable exception handling in the resolved manifest.

## Work

- [x] Restore the worker and orchestrator overlay files from the latest branch
  lineage version, adapting only optional `loop-state.md` reads for current
  `master` where that stale live snapshot is intentionally not restored, and
  updating recovered closeout examples to the current flag-based CLI.
- [x] Audit other branch-only `.agents/` artifacts and classify which are
  recoverable versus stale active state.
- [x] Define the dispatch contract: the parent resolves the app-type pipeline
  profile separately, then injects shared stage instructions, a stage-safe
  project overlay, named stage/reviewer instructions, and the delegation
  bundle.
- [x] Restore the PA-cursor branch artifacts still required by master:
  `agent-context-resolution-architecture.md`,
  `config-explain-live-surface.md`, `scope-routed-da-review.md`, and
  `verify-record-review-direct-iteration.md`.
- [ ] Update staged prompt assembly or native sub-agent registration to apply
  that contract for `impl`, `verifier`, and `review`.
- [ ] Add tests proving each stage receives required shared guardrails without
  duplicating role-specific instructions.
- [x] Lock the repo-owned staged parent contract to a single parent-run
  delegation `closeout` operation for contracted work; retain `advance` only
  for direct, non-delegated work and reject orphaned merge-backs.
- [ ] Normalize the dirty `agents-config` source checkout to shared stage
  instructions and the parent-owned closeout contract after reconciling its
  existing loop-worker edits.
- [ ] Split `active.loop.md` into a legacy/full-slice overlay and a safe
  staged project overlay before injecting project overlay content into typed
  stage agents.
- [x] Label the existing `active.loop.md` as legacy/full-slice and remove its
  implicit injection from staged auto-fanout defaults.
- [x] Set the target ownership decision: named reviewers emit evidence; a
  deterministic parent-invoked return/aggregation gate emits the consolidated
  decision and merge-back packet; parent closeout mutates canonical state.
- [x] Write a proposal/spec input for a later `da` planning pass:
  `.agents/proposals/staged-profile-dispatch-and-return-gate.md`.
- [x] Reconcile that planning input with `external-agent-sources`: classify
  profile/overlay policy as Tier 1, executable stage resources as existing
  Tier 2 types, and separate registry bundle manifests from runtime bundles.
- [x] Reconcile that planning input with `org-config-resolution`: define
  staged precedence, protected overlay ownership, policy locks, and audited
  exception requirements.
- [ ] Reconcile the installed `iteration-close` skill examples that still show
  positional `workflow advance` syntax against the current CLI contract.

## Stage Instruction Transformation Boundary

Use `~/.agents/profiles/loop-worker.md` as source material, not as the direct
system prompt/profile for every stage. Do not confuse the extracted
instruction base with the versioned `app_type` pipeline profile:

| Existing donor content | Target ownership |
| --- | --- |
| Write scope, canonical task authority, focused evidence, classification metadata, and sandbox discipline | Shared stage instruction base, parent-resolved at dispatch |
| Architecture, acceptance, and adversarial review lenses | Separate named reviewer agent definitions |
| Worker `verify` / `checkpoint` / `merge-back` closeout and `/iteration-close` behavior | Legacy/full-slice `loop-worker` only |
| Delegation closeout and accepted canonical completion | Orchestrator or parent-gate instructions only |

## Native Stage Shape

- `impl-agent`: shared stage instruction base plus project overlay plus
  implementation artifact contract; emits `impl-handoff.yaml` and stops.
- Typed verifier agents such as `verifier-unit`, `verifier-api`, and
  `verifier-ui-e2e`: shared instructions plus project overlay plus their specific
  verification contract; emit typed result artifacts and stop.
- Named reviewer agents such as `reviewer-acceptance-invariants`,
  `reviewer-architecture-standards`, and `reviewer-adversarial`: shared instructions
  plus project overlay plus only their lens instructions; emit typed review
  evidence and stop. A deterministic parent-invoked return/aggregation gate
  owns consolidated `review-decision.yaml` and merge-back in the target
  design. Until that contract lands, the implemented consolidated review
  stage remains the compatibility producer.
- `loop-worker` remains the compatibility/full-slice worker until legacy
  dispatch is explicitly retired; it may retain iteration closeout.

## Materialization Contract

Dot-agents should have the parent/orchestrator materialize platform-native
agent definitions by composing the shared stage instruction base, a repo-local
stage-safe project overlay, and the named stage definition during dispatch.
Separately, the resolved `app_type` profile selects the pipeline behavior and
is persisted with provenance in a resolved bundle/config execution manifest
that `da config explain` can inspect. The prompt at spawn time should then
contain only the delegation bundle reference and task-specific runtime
requirements. Do not fold project paths or individual stage procedure into
the reusable instruction base.

Until a stage-safe project overlay exists, staged dispatch must not inject
`.agents/active/active.loop.md` unchanged: that file still includes
implementation and `/iteration-close` procedure intended for the
compatibility/full-slice path.
