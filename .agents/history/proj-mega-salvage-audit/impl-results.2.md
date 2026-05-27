# Starter Agent Configuration Consistency Review

**Date:** 2026-05-26
**Scope:** Repo-owned staged prompt/spec surfaces plus the readable
`~/.agents` canonical configuration checkout.

## Findings

1. **High - Staged children can inherit legacy closeout semantics.**
   The dirty `~/.agents/profiles/loop-worker.md` and projected
   `agents/dot-agents/loop-worker/AGENT.md` combine `impl`, `verify`, and
   `review` stages with unconditional `/iteration-close` and merge-back.
   That conflicts with the staged runtime, where implementation and verifier
   stages write typed artifacts and stop.

2. **High - Delegated parent closeout duplicated canonical completion.**
   `workflow delegation closeout --decision accept` already sets the
   canonical task to `completed`; its lifecycle test asserts that contract.
   `bin/tests/ralph-closeout` nevertheless followed it with
   `workflow advance --status completed`, and generated bundle metadata listed
   both as required parent operations.

3. **Medium - One bundle worker closeout shape cannot describe both modes.**
   Generated `closeout.worker_must` still describes the legacy full-slice
   worker (`verify record`, `checkpoint`, `merge-back`). Typed staged agents
   instead have distinct artifact-and-stop boundaries. This needs an
   execution-mode or per-stage bundle contract before the field is injected
   as native staged instruction.
   The bundle's `worker.profile: loop-worker` label also remains compatibility
   metadata until native named stage definitions and a stage-aware schema
   exist; changing it now would invent an unsupported runtime identifier.

4. **Medium - A historical merge-back owner was never implemented.**
   `.agents/workflow/specs/loop-agent-pipeline/decisions.1.md` assigned
   `workflow merge-back` to a verifier aggregate. The actual staged runner
   has the review stage write the merge-back return artifact, and its smoke
   tests encode that behavior.

5. **Medium - The recovered project overlay is not stage-neutral.**
   `.agents/active/active.loop.md` called itself the project overlay while
   carrying implementation-only commands and `/iteration-close`. This pass
   labels it legacy/full-slice; it remains unsafe to inject unchanged into
   typed verifier or reviewer agents.
   Before cleanup `bin/tests/ralph-orchestrate` emitted it as the default
   project overlay in bundle metadata. Staged auto-fanout now omits an
   implicit legacy overlay; a stage-safe replacement still needs to be
   materialized.

6. **Medium - Named stage agents are not yet supplied by the canonical config.**
   The current manifest exposes `loop-worker`, generic `verifier`, and
   `test-runner`; there is no materialized `impl-agent`, typed verifier
   agent, or named reviewer-lens agent set. Tracked repo prompt surfaces
   exist, but native stage definitions have not caught up.

7. **High - The PA-cursor lineage contained durable artifacts still needed by
   master.** `agent-context-resolution-architecture.md` was missing while
   multiple ADRs and histories linked to it. The branch-only
   `verify-record-review-direct-iteration.md` remains referenced from active
   verification evidence, and no direct-contract materialization
   implementation was found.

8. **Medium - “Profile” already has a canonical pipeline meaning.**
   `.agents/workflow/specs/app-type-profiles/design.md` is already present on
   master byte-identically to the PA-cursor source and defines profiles as
   versioned pipeline bundles. Shared worker guardrails therefore belong to
   parent-resolved stage instructions, not a competing new profile kind.

9. **High - Live PA-cursor configuration/review proposals were missing.**
   `config-explain-live-surface.md` remains required because current dispatch
   reads flat local maps without an effective-config/provenance command.
   `scope-routed-da-review.md` remains required because product docs describe
   routed proposal queues while a canonical review contract is not present.

10. **Medium - Merge-back ownership must be designed for named reviewers.**
    Current consolidated review can produce a return artifact, but separate
    reviewer-lens agents cannot each own a single consolidated merge-back.
    The target contract uses typed reviewer evidence, a deterministic
    parent-invoked return/aggregation gate, and parent-owned closeout.

11. **Medium - The app-type draft carried derived-field drift.**
    `impact_radius_kind` is declared derived from the graph adapter but was
    still authored in examples and composite resolution rules. Carrying that
    inconsistency into a plan would create an unnecessary conflicting config
    surface.

12. **Medium - External artifact types were underspecified for staged
    dispatch.** `external-agent-sources` supports executable `agent`, `skill`,
    `verifier`, and registry `bundle` packages but no `profile` package. App
    profiles and overlay selection belong in Tier 1 configuration unless a
    later design deliberately adds a new package type; the runtime delegation
    bundle is not the registry bundle manifest.

13. **Medium - Organization resolution stopped before staged dispatch.**
    `org-config-resolution` defines organization-to-runtime precedence but
    resolves only through verifier selection. The staged manifest needs
    inherited locks and auditable override handling for agents, reviewers,
    overlays, verifier chains, return/closeout behavior, execution mode, and
    package trust rules.

## Repo-Owned Cleanup Applied

- Normalized `docs/LOOP_ORCHESTRATION_SPEC.md` to distinguish staged child
  artifact production from legacy full-slice closeout, and to specify
  parent-owned delegation closeout as the single completion operation for
  contracted delegated work.
- Removed the redundant post-closeout `workflow advance` from contracted
  `ralph-closeout` processing. Orphaned merge-backs now fail explicitly,
  because the parent review gate itself requires a delegation contract;
  direct, non-delegated work retains its separate `workflow advance` path.
- Corrected generated delegation-bundle parent requirements to advertise only
  `workflow_delegation_closeout`.
- Updated staged repo prompt surfaces so they no longer suggest inheriting
  legacy `loop-worker` closeout; they point toward parent-resolved shared
  stage instructions, explicitly distinct from an `app_type` profile.
- Clarified the ISP prompt so staged children do not load legacy
  `/iteration-close` behavior.
- Added a runtime/target clarification to the pipeline decisions record:
  consolidated review currently emits the merge-back return artifact; the
  target named-reviewer design moves consolidation to a parent-invoked return
  gate, while parent closeout reconciles accepted completion.
- Restored `.agents/proposals/agent-context-resolution-architecture.md`
  byte-identically from
  `origin/feature/PA-cursor-projectsync-phase1-extract-293f`, repairing its
  live ADR/history references and preserving its dispatch/system-instruction
  ownership note.
- Restored the still-relevant queued
  `.agents/proposals/verify-record-review-direct-iteration.md` from that
  branch because active verification evidence references it and the described
  direct-contract CLI gap is not implemented.
- Restored `.agents/proposals/config-explain-live-surface.md` and
  `.agents/proposals/scope-routed-da-review.md` from the PA-cursor branch:
  both remain live input to effective config and routed review planning.
- Changed staged `ralph-orchestrate` fanout defaults so typed stages no longer
  receive the legacy `active.loop.md` or a loop-worker closeout prompt by
  default.
- Corrected derived impact-radius drift in the app-type profile draft and
  added explicit config/dispatch/return-gate planning requirements.
- Added `.agents/proposals/staged-profile-dispatch-and-return-gate.md` as the
  handoff for a later canonical `da` spec/plan creation pass.
- Amended `external-agent-sources` and dependent design inputs to map staged
  resources onto existing config/package tiers and prevent runtime bundle
  artifacts from being conflated with OCI bundle manifests.
- Amended `org-config-resolution` and dependent planning inputs to define
  staged merge categories, protected overlay ownership, inherited locks, and
  explicit audit requirements for permitted runtime exceptions.

## Deferred Config-Source Cleanup

`~/.agents` is a separate Git checkout with pre-existing modifications in
the exact profile and loop-worker agent files that need restructuring. This
pass intentionally does not overwrite those changes. A follow-up should:

- create reusable stage instructions from the non-closeout portions of
  `profiles/loop-worker.md`, composed by the parent/orchestrator at dispatch;
- preserve `loop-worker` as legacy/full-slice compatibility only;
- add native `impl-agent`, typed verifier, and named reviewer definitions;
- split bundle closeout metadata into legacy/full-slice and typed staged
  obligations before injecting it into native stage agents;
- update orchestrator and iteration-close guidance so delegated accepted work
  uses parent-owned `delegation closeout` without a second advancement, while
  direct `workflow advance` examples use current flag-based syntax; and
- introduce a stage-safe project overlay before staged dispatch injects
  common repo-local content.

Other PA-cursor branch-only proposals were classified but not revived:
`graph-backend-adapter-contract`, `plan-archive-command`, and
`workflow-app-types-discovery` already graduated into current spec/code
surfaces. `config-explain-live-surface` and `scope-routed-da-review` are now
restored because the unresolved config/proposal-review contracts are required
inputs to the requested planning pass.
