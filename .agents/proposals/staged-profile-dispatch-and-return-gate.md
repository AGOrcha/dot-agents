# Proposal: Staged Profile Dispatch And Return-Gate Contract

- type: project-local scoping and design input
- status: PARTIALLY REALIZED 2026-06-08 — the StageProfile primitive (executor/verifier/reviewer/orchestrator) shipped via #45 (`stage_profiles`); native-stage dispatch + return-gate is now the `multi-provider-wave-dispatch` spec (draft). Was: draft / ready for canonical spec-and-plan authoring
- date: 2026-05-26
- prompted by: PA-cursor salvage, starter agent-config consistency review,
  and the named-stage profile discussion

## Goal

Define the contract that lets a parent/orchestrator spawn native stage agents
from resolved configuration without injecting legacy full-slice instructions
into typed stages, and assign the final merge-back return packet to the right
owner once reviewers are independently spawnable.

This proposal is deliberately a planning input. The next step is a canonical
workflow spec and plan authored with `da`, not ad hoc runtime edits.

## Verified Current State

- `app-type-profiles/design.md` already defines `profile` as a versioned
  pipeline bundle selecting verifier, review, and graph behavior.
- `.agentsrc.json` exposes local `app_type_verifier_map` and
  `verifier_profiles`, but no resolved profile, native named stage agents,
  instruction base, return gate, or staged closeout policy.
- `workflow fanout` persists app type and verifier sequence only; generated
  `closeout.worker_must` still describes the legacy full-slice worker.
- `bin/tests/ralph-worker --stage review` currently writes both
  `review-decision.yaml` and the merge-back artifact.
- `workflow delegation closeout --decision accept` is the parent-owned
  operation that changes canonical task state and archives artifacts.
- The embedded `da init` starter now includes `loop-worker`, ISP, and
  iteration-close instruction surfaces. This pass aligns new starter seeds
  with the legacy-versus-staged boundary and parent-only delegated closeout.
- `CopyMissingStarterAssets` preserves existing destination files, so updated
  embedded defaults do not remediate an already customized
  `~/.agents/profiles/loop-worker.md` installation.
- The live `config explain` and scope-routed `da review` proposals from the
  PA-cursor branch remain needed and are restored beside this proposal.

## Findings

### 1. Profile and system-instruction concerns were being conflated

A pipeline profile answers what stages, verifiers, review kind, and graph
adapter apply to work. It should not embed the invariant instructions that
make an agent bounded or the product-specific overlay it must read.

The parent/orchestrator should compose native stage instruction context from:

1. stable bounded-stage instructions;
2. named stage or reviewer-lens instructions;
3. a stage-safe project/product overlay; and
4. task/bundle context.

The resolved app-type profile independently selects and versions the pipeline
and is persisted in the bundle.

### 2. The draft profile spec had derived-field drift

The graph adapter contract already makes `impact_radius_kind` derived, while
the app-type spec still authored it in examples and required a composite to
declare it on conflict. Those contradictions should not be carried into a
plan; the draft spec is corrected in this pass.

### 3. Current config surfaces cannot explain a dispatched stage

Flat verifier maps answer only which verifier names run. They cannot answer:

- which profile version and digest was resolved;
- which stage agents, reviewer lenses, instruction base, or overlays were
  injected;
- whether a legacy/full-slice or typed staged closeout policy applies;
- which return/aggregation gate owns consolidation; or
- which layer/source supplied each decision.

The restored `config-explain-live-surface` proposal is necessary, but its
effective snapshot must expand from single values to a resolved execution
manifest for stage dispatch.

### 4. External source packaging needs an explicit boundary

`external-agent-sources` already provides OCI media types for executable
`agent`, `skill`, `verifier`, and registry `bundle` pointer manifests. It
does not provide a `profile` package type. The target design should therefore
keep app-type profile documents and overlay selection in Tier 1 configuration,
while resolving executable stage agents, verifier implementations, and
skill-backed gates through the existing Tier 2 types.

The registry bundle manifest must also remain distinct from the repo-local
delegation bundle: one groups reusable published package refs; the other
records a concrete task dispatch and its provenance.

### 5. Organization layering requires staged policy locks and override audit

`org-config-resolution` already defines organization, team, repository, task,
and runtime precedence, but its effective resolved contract stops at verifier
selection. In a staged model, inherited policy must also constrain allowed
implementation agents, reviewer lenses, stage-safe overlays, verifier
chains, return-gate and closeout policy, execution modes, and package trust
requirements.

The parent may select repo/task specialization within those constraints. A
runtime bundle must not quietly replace preloaded stage instructions, the
return owner, or a locked verifier chain. Compatibility overrides such as an
explicit verifier sequence remain viable only when recorded as policy-bypass
evidence and allowed by the applicable policy.

### 6. Starter defaults and installed configuration need separate migration lanes

The shipped starter can be made internally consistent for new installs, but
the current installer deliberately does not overwrite existing shared-home
assets. That is the right safety default for user-modified prompts and
profiles; it also means the installed `~/.agents/profiles/loop-worker.md`
cannot be treated as repaired merely because the embedded seed is corrected.

The planning pass needs seed provenance and an explicit non-destructive
upgrade flow: identify unchanged shipped assets that can be refreshed,
surface local divergence for review, and migrate customized legacy
loop-worker material only through an explicit reviewed action.

### 7. Individual reviewers are the wrong merge-back owner

When review is a single compatibility stage, writing the return packet there
is workable. Once `reviewer-acceptance-invariants`,
`reviewer-architecture-standards`, or `reviewer-adversarial` can spawn
independently, no lens can reliably own one consolidated merge-back result.
The last reviewer to run is not a semantic aggregation boundary.

## Target Ownership Contract

| Artifact or mutation | Target owner |
|---|---|
| `impl-handoff.yaml` | named implementation stage |
| `<verifier>.result.yaml` | each named verifier stage |
| `<reviewer>.decision.yaml` or equivalent typed review evidence | each named reviewer stage |
| consolidated `review-decision.yaml` and merge-back return packet | deterministic return/aggregation gate, resolved and invoked by parent |
| accepted completion, rejected/blocked status, archive, cleanup | parent/orchestrator delegation closeout |
| direct non-delegated task completion | direct-work `workflow advance` path |

Migration rule: the current consolidated review stage may continue writing
the decision and merge-back packet until typed reviewer artifacts and the
return/aggregation gate land. That is compatibility behavior, not target
ownership.

## Required Resolved Execution Manifest

The bundle/config contract should carry a single resolved manifest with:

| Group | Required information |
|---|---|
| Profile | selected ref, resolved version, digest, composition expansion |
| Pipeline | scope/output kinds, verifier chain, review kind/skill, graph adapter and derived impact contract |
| Dispatch | named stage agent refs, shared bounded-instruction ref/digest, stage-safe overlay refs/digests |
| Protocol | stage order, each stage's accepted inputs and required output artifact, retry/gate policy |
| Return | aggregation/return-gate ref and closeout owner |
| Mode | `staged` versus `legacy_full_slice`, preventing legacy closeout from being injected into typed stages |
| Provenance | effective-config layer/source and lock digest for every resolved reference |
| Packaging tier | Tier 1 profile/overlay values versus Tier 2 `agent`, `verifier`, or `skill` refs; runtime bundle versus registry manifest |
| Inherited policy | allowed or locked stage-agent, reviewer, overlay, verifier, return-gate, execution-mode, and trust rules plus any audited exception |

`da config explain app_type --json`, `workflow app-types --verbose`, bundle
materialization, and config validation should consume the same effective
snapshot API.

## Missed Config Opportunities

The canonical spec and plan should address:

1. `stage_agents` and reviewer-lens references as config-resolved names,
   rather than hardcoded shell-stage behavior.
2. A reusable bounded-stage instruction reference plus stage-safe overlay
   selection, separate from `app_type`.
3. `execution_mode` and typed stage protocol in the bundle so
   `closeout.worker_must` cannot lie about staged workers.
4. Return-gate and closeout ownership as resolved policy, so dispatch and
   parent gate agree on who consolidates evidence and mutates state.
5. Effective-config provenance, validation, and digest/lock readback for all
   of the above.
6. A task-level profile selection or controlled composition path for a repo
   containing multiple pipelines or mixed artifact types.
7. Non-regression migration from `app_type_verifier_map` and
   `verifier_profiles`, including byte-identical verifier invocation checks
   before deprecating flat maps.
8. Artifact-type validation against `external-agent-sources`: avoid a new
   `profile` OCI type unless explicitly justified, and reject using a
   registry `bundle` pointer manifest as a runtime delegation artifact.
9. Resolution-boundary validation against `org-config-resolution`: expose
   inherited staged-policy locks and require audited authorization for
   permitted task/runtime deviations.
10. Non-destructive starter migration: version or digest shipped starter
    assets, distinguish new-install defaults from locally edited installed
    config, and provide an explicit reviewed upgrade path for legacy
    `loop-worker` installations.

## Relationship To Other Live Proposals

| Proposal | Relationship |
|---|---|
| `agent-context-resolution-architecture.md` | Parent-owned resolution and instruction/overlay injection foundation |
| `config-explain-live-surface.md` | Operator-visible effective manifest and provenance surface |
| `scope-routed-da-review.md` | Review/approval routing for durable configuration and proposal changes |
| `verify-record-review-direct-iteration.md` | Direct-work contract gap; remains distinct from staged delegated ownership |
| `delegation-bundle-contract-divergence-scoping.md` | Adjacent contract projection/schema work; coordinate bundle fields rather than duplicate them |
| `../workflow/specs/config-distribution-model/design.md` | Effective snapshot plus installed starter-seed provenance and non-destructive migration rules |
| `../workflow/specs/external-agent-sources/design.md` | OCI media types, digest pinning, registry bundle meaning, and transport/auth contract |
| `../workflow/specs/org-config-resolution/design.md` | Staged precedence, protected fields, locked policy, and audited override boundaries |

## Required Canonical Follow-On

Create a canonical workflow spec and implementation plan, through the `da`
workflow surface, for a focused feature such as
`staged-profile-dispatch-return-gate`. The spec should settle:

- manifest and bundle schema fields;
- artifact-tier and media-type mapping, including Tier 1 profiles/overlays and
  Tier 2 executable refs;
- organization/repository/task/runtime precedence, locked-policy, and
  audited-exception semantics;
- native named-stage materialization and project overlay rules;
- reviewer evidence and deterministic aggregation artifact schemas;
- return-gate versus parent-closeout command ownership;
- shipped-starter versus installed-config provenance and reviewed upgrade
  semantics;
- compatibility and migration sequencing; and
- validation, explain, and non-regression acceptance tests.

The plan should explicitly sequence this work with
`delegation-bundle-contract-bridge` if that proposal is promoted first, since
both touch bundle/contract ownership and schema.

## Done Criteria For The Planning Pass

- One canonical spec under `.agents/workflow/specs/<id>/` cites the restored
  proposals and relevant existing design sections.
- One canonical plan under `.agents/workflow/plans/<id>/` carries bounded
  tasks, dependencies, write scopes, and verification strategy.
- No implementation task treats `loop-worker` full-slice closeout as the
  native instruction base for typed stages.
- No implementation task assigns the consolidated merge-back packet to an
  individual named reviewer.
- No migration task silently overwrites customized installed starter assets
  when bringing legacy `loop-worker` configuration forward.
