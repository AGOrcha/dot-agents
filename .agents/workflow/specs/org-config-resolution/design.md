# Organization Config Resolution And Repo Identity


> **Consolidation update (2026-06-07) — `stage-profile-and-routing-consolidation`:** `verifier_profiles` + `reviewer_profiles` are now unified into one **typed** `stage_profiles` map (stage `executor`/`verifier`/`reviewer`/`orchestrator` → slug → `{label, prompt_files}`), and `app_type_verifier_map` is **retired** into `execution_profile.by_app_type.<type>.topology.verifier_sequence`. Legacy keys still load (folded, deprecated). Mentions of those keys below describe the pre-consolidation surface — read them as the new model.

**Status:** design artifact

**Purpose:** define how `dot-agents` should resolve shared configuration, verifier policy, and feature rollout for organizations that have many repositories, uneven local checkouts, and no guaranteed shared filesystem root.

**Related design tracks:**
- The canonical `.agentsrc.json` field surface (`sources`, `extends`, `packages`), two-pass
  resolution engine, lockfile format, per-tier caching, audit taxonomy additions, and
  `da config explain` command are specified in [config-distribution-model](../config-distribution-model/design.md).
  This document defines *what* layers mean; config-distribution-model defines *how* they
  are fetched, merged, and locked.
- External source transport, auth, OCI wire protocol, FIPS posture, and package signing live
  in [external-agent-sources](../external-agent-sources/design.md).
- This document focuses on resolution semantics, config layering, repo identity, and operational rollout.
- It does not commit the current `loop-agent-pipeline` plan to implementation work.

> **Field-vocabulary supersession (single-source pointer; verified against shipped code
> 2026-06-25).** This document predates the staged-dispatch consolidation and names
> `verifier_profiles` and `app_type_verifier_map` as live first-class `AgentsRC` fields
> (notably §2.2, §7.2, §7.3, §8). **In the shipped code those are deprecated legacy keys** — read
> and folded on load into the unified `stage_profiles` + `execution_profile` model
> (`internal/config/agentsrc.go` `foldLegacyProfiles`; `internal/config/execution_profile.go`),
> never re-emitted. The canonical successor mapping lives once in
> [config-distribution-model §15](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock)
> and §10 (field-name note): `app_type_verifier_map` → `execution_profile.by_app_type.<type>.topology.verifier_sequence`;
> `verifier_profiles` → `stage_profiles.<stage>.<id>`. The **layering/precedence/merge semantics this
> document defines remain authoritative and accurate** — only the field *names* moved. Where a section
> below says `verifier_profiles`/`app_type_verifier_map`, read the successor field; the merge category
> (map-merge / ordered-replace) is unchanged. This pointer is the single source for the rename so the
> rule is not restated per occurrence.

## 1. Problem statement

The current repo-local `app_type -> verifier_sequence` mechanism is useful, but it is not a sufficient project model for larger organizations.

Today, the real behavior is:

- `workflow fanout` reads `task.app_type`, falling back to `plan.default_app_type`.
- It resolves that string against `.agentsrc.json.app_type_verifier_map`.
- It writes the result into the delegation bundle as `verification.app_type` and `verification.verifier_sequence`.
- An explicit `--verifier-sequence` flag overrides the map.

That is a narrow dispatch feature, not a full configuration hierarchy.

It does not define:

- how a company shares config across many repos
- how a repo opts into shared config without a common parent directory
- how repo identity is determined across local checkout, CI, and ephemeral environments
- how repo-specific setup or validation contracts are carried
- how teams roll out new `da` features gradually across many repos
- how personal aggregate workspaces differ from canonical organizational configuration

The larger-company constraint is the deciding one:

- many developers only clone one repo or a small subset
- repositories may be entirely independent local checkouts
- there may be no monorepo and no submodule root
- a developer’s personal multi-repo workspace must not become the source of truth for the company

Therefore filesystem-local inheritance is the wrong core model.

## 2. Current-state audit

### 2.1 What exists today

The current system already has a few building blocks worth preserving:

- repo-local `.agentsrc.json`
- repo-local verifier profiles
- task-local `app_type`
- plan-level `default_app_type`
- delegation-bundle persistence of resolved verification metadata
- project `kg` configuration and graph-bridge readiness work

This is enough for a single repo to say:

- `go-cli` work should run `unit`
- `api` work should run `unit, api`

That is a useful local dispatch rule.

### 2.2 Important implementation caveat

~~The current config loader does not yet treat `verifier_profiles` and `app_type_verifier_map` as first-class typed `AgentsRC` fields. They are parsed by the workflow fanout path and otherwise preserved as extra JSON.~~ **(RESOLVED by `stage-profile-and-routing-consolidation`, 2026-06-07):** the profile surface is now the **typed** `stage_profiles` field (`map[stage]map[slug]StageProfile`) with the full `AgentsRC` lifecycle, and per-app_type verifier routing is the typed `execution_profile.by_app_type.<type>.topology.verifier_sequence`; `app_type_verifier_map` is retired (folded on load). The earlier "extra JSON / too weak a contract" caveat no longer holds.

### 2.3 Why `payout` is useful but not canonical

A personal workspace like `payout/` is still useful as:

- a convenience root for running one dev binary across many repos
- a coordination surface for migration work
- a place to keep program-level plans if one person wants that view

It is not an acceptable canonical model for enterprise config resolution because:

- other developers may not have the same workspace shape
- CI will not share a developer’s local directory topology
- a repo must be operable when checked out by itself
- organizational policy cannot depend on one developer’s parent directory layout

Workspace-local aggregation is optional convenience, not semantic inheritance.

## 3. Design principles

### 3.1 Explicit, not implicit

Shared configuration must be imported explicitly from declared sources, not discovered by walking parent directories.

### 3.2 Identity-based, not path-based

Resolution must key off stable repository identity, not local filesystem location.

### 3.3 Repo-local operability

A repo checked out by itself must resolve the same effective config as the same repo inside a larger personal workspace, subject only to user-local overrides.

### 3.4 Layered overrides

Configuration should resolve through a small number of clearly ordered layers so operators can reason about where behavior came from.

### 3.5 Optional workspaces

Workspaces may add coordination features, but they must not be required for correct config resolution.

### 3.6 Gradual rollout

New `da` capabilities should be adoptable centrally but enabled per repo or per team, not forced all at once.

## 4. Proposed layer model

Configuration resolves through these layers, from lowest precedence to highest:

1. Product defaults
2. User-local defaults
3. Imported organization layers
4. Imported team layers
5. Imported repo layers
6. Repo-local committed config
7. Plan/task/runtime overrides

### 4.1 Product defaults

Built-in defaults shipped by `dot-agents`.

Examples:

- baseline schema defaults
- built-in verifier contract rules
- default workflow artifact paths
- default disabled state for new feature flags unless enabled elsewhere

### 4.2 User-local defaults

Machine-local preferences that should not be committed to a project.

Examples:

- local auth material
- local cache choices
- preferred tool paths
- user-specific allowed convenience sources

These should not redefine company policy. They exist for local environment fit.

### 4.3 Imported organization layers

Central source-of-truth configuration for the company.

Examples:

- shared prompt packs
- standard verifier profiles
- standard app classes
- policy defaults
- approved source registries
- feature rollout policy
- canonical repo registry metadata

### 4.4 Imported team layers

Optional domain overlays owned by a team such as:

- `payments-platform`
- `frontend`
- `infra`
- `data-pipeline`

These refine org defaults without forcing every repo to duplicate the same conventions.

### 4.5 Imported repo layers

Optional centrally managed repo-specific definitions, used when a company wants to manage repo policy from the config source rather than copy the whole policy into each repo.

Examples:

- repo-specific verifier chains
- repo-specific prerequisite commands
- repo-specific prompt overlays
- repo-specific graph defaults

### 4.6 Repo-local committed config

The actual checked-in `.agentsrc.json` in the repository remains authoritative for that repo’s local declaration.

Its main responsibilities should be:

- identify the repo
- declare which shared layers it imports
- pin or select source refs when necessary
- add the repo’s local exceptions or overlays

### 4.7 Plan/task/runtime overrides

These remain the narrowest and highest-precedence layer.

Examples:

- `plan.default_app_type`
- `task.app_type`
- `--verifier-sequence`
- per-bundle scenario tags
- temporary validation queue

These should never be mistaken for long-lived organization policy.

## 5. Repository identity model

### 5.1 Why repo identity must be explicit

If the system only knows the local path, then the same repository will resolve differently across:

- `/Users/alice/src/service-a`
- `/buildkite/builds/.../service-a`
- `/tmp/checkout/service-a`

That is not acceptable for durable policy.

### 5.2 Proposed identity fields

Each repo should have a stable logical identity.

Recommended fields:

- `repo_id`: canonical organization-level repo identity
- `project`: human-readable short name
- optional `team`
- optional `system`

Illustrative examples:

- `repo_id: github.com/acme/po-core-api-se`
- `repo_id: gitlab.acme.internal/payments/settlement-engine`

### 5.3 Resolution sources for repo identity

Resolution order should be:

1. explicit `repo_id` in repo-local config
2. explicit runtime override for exceptional automation use
3. derived identity from configured git remote normalization

Git remote derivation is a fallback, not the primary contract.

### 5.4 Why `project` is not enough

Short names like `api`, `web`, or `core` collide across organizations and teams. `project` is useful for display. `repo_id` is the stable lookup key.

## 6. Source and import model

### 6.1 Core rule

A repo should opt into shared config explicitly through imports, not through parent-directory discovery.

Illustrative repo-local shape (field surface specified in full in
[config-distribution-model §3](../config-distribution-model/design.md#3-agentsrcjson-field-surface)):

```json
{
  "$schema": "https://agorcha.dev/schemas/agentsrc.schema.json",
  "version": 2,
  "project": "po-core-api-se",
  "repo_id": "github.com/acme/po-core-api-se",
  "sources": [
    {
      "id": "acme",
      "type": "git",
      "url": "git@github.com:acme/da-config.git",
      "ref": "main"
    }
  ],
  "extends": [
    "acme:org/base",
    "acme:team/payments-platform",
    "acme:repo/po-core-api-se"
  ]
}
```

The exact transport and package publication details belong to the external-sources track.
The field surface (`sources`, `extends`, `packages`), source-id reference syntax, and
resolution engine belong to the config-distribution-model track.
The important semantic design point here is:

- `sources` tells the client where shared config may come from
- `extends` tells the client which named layers to import, using `source-id:layer-path` syntax

### 6.2 Import targets

Imported layers should be named, versionable config objects.

Examples:

- `org/base`
- `org/strict-security`
- `team/frontend`
- `team/payments-platform`
- `repo/po-core-api-se`

### 6.3 Import ordering

`extends` should be processed left to right, with later entries able to override earlier ones within the same precedence layer.

That makes order visible and predictable.

### 6.4 Missing import behavior

Missing imported layers must fail loudly and structurally.

Do not silently continue with partial inherited state when:

- an imported layer is not found
- the source is reachable but the named layer is absent
- the fetched artifact is the wrong type

The failure should identify:

- which import failed
- from which source it was expected
- whether the error was transport, auth, content, or schema

### 6.5 Caching and offline

This design assumes shared sources may be cached locally.

Offline behavior should be:

- use cached pinned content when available
- fail deterministically when required imports are missing and cannot be fetched

This aligns with the external-sources design rather than redefining it.

### 6.6 Transitive extends: org-through-team post-order

§6.3 covers ordering **within** a single manifest's `extends` list. This subsection
covers what happens when an imported layer **itself declares** `sources` and `extends`
— the org→team→repo transitive case that lets a consuming repo declare only its team
source and still inherit org/platform policy.

**Dependency expansion, children-first (post-order).** An `extends` entry is resolved
as a graph, not a flat list. When a fetched layer declares its own `extends`, those are
resolved **before** the layer that declared them is admitted to the effective stack. So
if `team/base.json` extends `org/base.json`, the effective imported order is
`[org/base.json, team/base.json]`, followed by repo-local. Org sets baseline vocabulary;
team refines and overrides it; repo-local wins last. Root entry order (§6.3) is preserved
**after** each root's subtree is expanded in place.

**Layer-local source environment.** A layer may declare `sources` of its own. Those
sources are available **only to that layer and its descendants** (a child source
environment extended from the parent's), never leaked back up to siblings or the root.
This is what lets a team layer name a private org source that the consuming repo never
declares.

**Dedupe by ref + resolved digest.** The same canonical layer ref reached through two
paths is admitted **once**, kept at its **first-resolved (baseline, lowest-precedence)** position —
so a shared base layer stays beneath every layer that extends it (org before team). The later
occurrence is recorded as already-satisfied provenance, not merged twice. But if the same
ref resolves to **different digests** (conflicting source refs pinning divergent content),
resolution **fails loudly** rather than silently merging ambiguous policy.

**Cycle detection.** A cycle in the extends graph (`A → B → A`) is detected on the active
recursion stack and fails structurally (an explicit `cycle` import-fail reason), never
silently dropped. The failure names the offending frame chain for provenance.

**Lock the whole transitive stack.** Every resolved layer unit — not only the repo-root
`extends` — is written to the unified `units` lock. A repo that declares only its team
source must still be able to `da config explain` org/platform fields **offline** after a
lock sync, because the org layer was locked as a transitive unit (see §6.5 and
[config-distribution-model §15](../config-distribution-model/design.md#15-coherence-model-v2-scopes-sources-units-and-the-lock)).

**Worked example (org → team → repo).** A repo's manifest declares only its team
source and `extends` `acme:team/base.json`. `team/base.json` itself declares an org
source and `extends` `acme-org:org/base.json`. The resolved effective stack is
`[acme-org:org/base.json, acme:team/base.json, <repo-local>]` — the repo inherits the org
layer transitively without ever naming the org source, and all three units are locked.

**Scope/owner are routing, not authority.** A layer's `sources[].scope`
(`public|org|team|repo`) and `owner` are ROUTING/ownership metadata that seed
scoped-content routing and explain/editability UI. They do **not** grant policy authority:
an imported layer still starts at `public` authority and can only bind locks through the
`authority_grants` pass (config-distribution-model §15 D1a). See §7 for merge/precedence.

## 7. Merge and precedence rules

### 7.1 Why a merge contract matters

Without explicit merge rules, layered config becomes guesswork and teams cannot explain why a repo resolved to a given verifier chain or feature set.

### 7.2 Proposed merge categories

- scalar fields: last writer wins within precedence order
- object maps: merge by key, then apply field-level override
- arrays that represent sets: union with stable order
- arrays that represent ordered execution: replace unless explicitly marked additive

### 7.3 Examples

- `repo_id`: scalar, must not be overridden by imported layers
- `skills`: set-union
- `agents`: set-union
- `rules`: set-union (name-selection; see [workspace-member-aggregation](../workspace-member-aggregation/design.md#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model) for exclusion-token semantics)
- `hooks`: set-union (name-selection; see [workspace-member-aggregation](../workspace-member-aggregation/design.md#d12--hooks-mcp-and-rules-become-a-uniform-name-selection-model) for exclusion-token semantics)
- `mcp`: set-union (name-selection; see [workspace-member-aggregation](../workspace-member-aggregation/design.md#d15--mcp-server-definitions-are-resolvable-config-content) for full-resolution merge semantics)
- `settings`: scalar (see [workspace-member-aggregation](../workspace-member-aggregation/design.md#rd13--settings-stays-scalar-in-v1) for the no-name-space rationale)
- `verifier_profiles`: map merge by profile id
- `app_type_verifier_map`: ordered execution mapping, last writer wins per app type
- `feature_flags`: map merge
- `workflow defaults`: object merge

### 7.4 Protected fields

Some fields should be repo-owned and non-overridable by imported layers once the repo commits them.

Recommended protected fields:

- `repo_id`
- `project`
- repo-owned path overrides that point inside the repo

### 7.5 Explainability requirement

The product should eventually be able to answer:

- which layer set `app_type_verifier_map["go-http-service"]`
- which layer enabled a feature flag
- which layer introduced a verifier profile

If config becomes layered, explanation tooling becomes part of the design, not an optional nicety.

### 7.6 Staged dispatch policy categories (planning input, 2026-05-26)

Staged execution adds policy values that must be classified before they are
merged:

| Config area | Merge behavior | Override boundary |
|---|---|---|
| `app_type_profiles`, `stage_agents`, `reviewer_lenses`, `return_gate_policies` | map merge by stable id | selected refs remain subject to inherited policy and allowlists |
| ordered verifier or stage chains | replace by default | additive composition only when the profile explicitly declares it |
| ordered stage-safe overlay chains | replace by default | append only for overlays declared composable and explainable |
| package source and digest requirements | inherited policy | repo/task/runtime layers cannot silently weaken trust requirements |

An imported organization or team layer may mark `allowed_stage_agents`,
`required_reviewer_lenses`, `return_gate_policy`, and `execution_mode` as
locked policy. A repo or task selection outside those constraints must fail
validation or use an explicit audited exception if the governing policy
permits one.

Repo-local overlay paths remain repo-owned protected paths. Imported layers
may provide imported overlay references, but must not redirect execution to
arbitrary files within a consuming repository.

## 8. App type and verifier policy under the layered model

### 8.1 Keep the current narrow mechanism

The current `app_type -> verifier_sequence` model is still useful and should remain as one part of the larger system.

The mistake would be treating it as the entire project model.

### 8.2 Expand what verifier profiles can describe

Verifier profiles should eventually be able to carry more than:

- `label`
- `prompt_files`

For larger-project setup they likely need room for:

- verifier kind
- prerequisite commands
- scoped command templates
- artifact expectations
- evidence policy defaults
- environment capability requirements

This is required because a large-company repo may need different setup and evidence discipline even when two repos both claim the same broad app type.

### 8.3 Layered mapping model

Under the layered design:

- org defines standard verifier profile vocabulary
- team defines common chains for app classes it owns
- repo refines or replaces the chain where local requirements differ
- task/runtime can still override explicitly

Resolution becomes:

`repo_id + imported layers + repo-local config + plan/task override -> verifier_sequence`

### 8.4 Illustrative mappings

Examples:

- `go-cli -> [unit]`
- `go-http-service -> [unit, api]`
- `realtime-stream -> [unit, streaming, integration]`
- `nextjs-ui -> [unit, ui-e2e, accessibility]`
- `infra-rollout -> [lint, batch, smoke, manual]`

These are policy classes, not hardcoded filesystem types.

### 8.5 Repo-specific setup contract

A repo may need setup that is more involved than a verifier chain name can express.

Examples:

- install or verify repo-owned git hooks
- build a local dev binary
- run a repo-specific doctor or readiness check
- gather changed-file scope for Sonar or similar tools
- assert Docker or service dependencies are available

That setup contract belongs to repo policy, not to parent-directory inheritance.

### 8.6 Staged execution manifest and override boundaries (planning input, 2026-05-26)

Staged profile dispatch extends resolution output from an effective
`verifier_sequence` to a resolved execution manifest. The
parent/orchestrator resolves this manifest before materializing delegation
work and persists selected refs, digests, provenance, and any approved
exception in the bundle or associated evidence.

Organization, team, and imported repository policy may govern:

- app-type profile selection and composition;
- permitted named implementation agents and reviewer lenses;
- permitted stage-safe overlay references;
- verifier chain requirements;
- return-gate and closeout policy;
- permitted execution modes; and
- package allowlist, signature, or digest requirements.

Repository-local configuration may choose within permitted values, define a
repo-owned stage-safe overlay, and supply app-type or task specialization
where inherited policy allows it. It must not silently replace locked stage
instructions, reviewer requirements, return-gate ownership, or trust
requirements.

Task and runtime input supplies execution facts such as task identity,
bounded write scope, context references, scenario tags, and temporary
validation queue entries. Existing compatibility flags such as
`--verifier-sequence` may continue to be accepted by the current runtime, but
in the staged model any bypass of inherited profile policy is an explicit
audited override, never an invisible last-writer replacement.

The resolved manifest must explain both applied values and rejected or
exception-authorized overrides. It is consumed consistently by
`da config explain`, app-type inspection, bundle materialization, and
validation.

## 9. Workspace model

### 9.1 Workspace is optional

The design must treat workspaces as optional containers for convenience features.

Examples:

- one developer’s personal multi-repo checkout
- a temporary migration workspace
- a program-level coordination checkout

### 9.2 What a workspace may add

A workspace may add:

- convenience command routing
- aggregate health views
- personal cross-repo planning
- shared dev binary location
- local orchestration of multiple checked-out repos

### 9.3 What a workspace must not define

A workspace must not be required to determine:

- whether a repo inherits company policy
- which team owns a repo
- which verifier chain a repo uses
- which features a repo has opted into

Those must resolve identically when the repo is checked out alone.

## 10. Cross-repo planning model

### 10.1 Do not require one giant checkout

Cross-repo work should not assume all participating repos are co-located on disk under one root.

### 10.2 Two acceptable coordination models

#### Model A: per-repo canonical plans with links

Each repo keeps its own canonical plans, and a cross-repo initiative links work through `repo_id` references.

Good for:

- decentralized ownership
- independent release cadences
- repositories that are often checked out alone

#### Model B: dedicated orchestration repo

A separate orchestration repo stores program-level plans that reference many repos by `repo_id`.

Good for:

- platform migrations
- coordinated multi-repo releases
- central architecture programs

### 10.3 What not to do

Do not make a single developer’s personal umbrella workspace the canonical control plane for company-wide multi-repo planning.

## 11. Feature rollout model for new `da` capabilities

### 11.1 Central rollout, local opt-in

Organizations need a way to define new feature availability centrally while letting repos opt in intentionally.

Examples of features that fit this model:

- canonical workflow plan/task bundles
- staged fanout and verifier sequencing
- graph bridge integration
- repo-scoped health/readback commands
- newer verifier result contracts

### 11.2 Proposed feature layers

Recommended model:

- org layer declares available features and minimum supported client versions
- team layer may recommend or require features for classes of repos
- repo layer explicitly opts into features when ready

### 11.3 Why repo opt-in matters

Large organizations rarely migrate every repo at once.

Some repos may need:

- conservative defaults
- compatibility with older automation
- different readiness criteria
- temporary exceptions during migration

### 11.4 Illustrative shape

```json
{
  "features": {
    "workflow_canonical_plans": "enabled",
    "staged_fanout": "enabled",
    "graph_bridge": "preview",
    "verifier_contract_v2": "disabled"
  }
}
```

The exact schema can change. The design point is that feature rollout should be explicit and layered.

## 12. Central config repo layout

The company source of truth can live in a dedicated config repo or equivalent published bundle set.

Illustrative logical layout:

```text
org/
  base.json
  strict-security.json
teams/
  payments-platform.json
  frontend.json
repos/
  po-core-api-se.json
  manager-ui.json
verifiers/
  unit.json
  api.json
  streaming.json
  ui-e2e.json
app-types/
  go-http-service.json
  realtime-stream.json
  staged-web-app.json
agents/
  implementation.json
  reviewers/
    scope-reviewer.json
overlays/
  products/
    shared-ui.json
return-gates/
  delegated-review-closeout.json
features/
  rollout.json
registry/
  repos.json
```

This does not require those exact filenames. It does require the source of truth to distinguish:

- organization policy
- team policy
- repo overrides
- reusable verifier definitions
- app-class mappings
- repo registry metadata

## 13. Minimal repo-local config target

For enterprise rollout, the ideal repo-local `.agentsrc.json` should be small.

It should primarily answer:

- who am I
- which shared layers do I import
- what do I override locally

Illustrative target:

```json
{
  "$schema": "https://agorcha.dev/schemas/agentsrc.schema.json",
  "version": 2,
  "project": "manager-ui",
  "repo_id": "github.com/acme/manager-ui",
  "sources": [
    {
      "id": "acme",
      "type": "git",
      "url": "git@github.com:acme/da-config.git",
      "ref": "main"
    }
  ],
  "extends": [
    "acme:org/base",
    "acme:team/frontend",
    "acme:repo/manager-ui"
  ],
  "app_type_verifier_map": {
    "nextjs-ui": ["unit", "ui-e2e", "accessibility"]
  }
}
```

The repo-local file remains durable even if the developer only cloned this one repository.

## 14. Relationship to `payout`-style setups

A `payout`-style workspace can still be a first-class user workflow for:

- running one dev `dot-agents` binary against many repos
- maintaining a migration dashboard
- holding temporary aggregate plans
- performing personal cross-repo readback

But its role is:

- user convenience
- local orchestration

Not:

- organization inheritance root
- required source of verifier policy
- required location for shared company config

## 15. Migration direction

### 15.1 Near-term

Keep the current repo-local `app_type_verifier_map` behavior intact.

Do not break:

- existing `.agentsrc.json`
- current fanout resolution
- current task `app_type` fields

### 15.2 Additive next steps

Likely additive path:

1. promote `verifier_profiles` and `app_type_verifier_map` to first-class `AgentsRC` fields
2. add `repo_id`
3. add `extends`
4. add imported-layer resolution from declared sources
5. add effective-config explanation tooling
6. add feature-rollout fields
7. add resolved staged execution manifests with provenance for agent,
   overlay, verifier, return-gate, and closeout selections
8. add locked-policy and audited-exception validation for repo/task/runtime
   staged overrides

### 15.3 Workspace migration rule

Any personal or team workspace that currently behaves like an inheritance root should be migrated so repos continue to work when copied out and checked out alone.

## 16. Open questions

### Q1: source packaging boundary

Should organization/team/repo config layers be published as dedicated config artifacts, or as package-like bundles reusing the external-source package machinery directly?

### Q2: repo registry authority

Where should the canonical `repo_id -> team/system/ownership metadata` registry live:

- inside the central config source
- in a separate organization service
- partially in both with local cache

### Q3: protected field enforcement

Which fields are hard protected at the repo layer, and which may an imported repo layer override if the repo opts in?

### Q4: verifier profile schema growth

How much execution metadata should verifier profiles own directly versus referencing reusable command/policy blocks?

### Q5: orchestration repo contract

If a dedicated orchestration repo exists, what canonical artifact shape should it use to reference repo-scoped plans without duplicating their execution state?

## 17. Recommended direction

Adopt this rule:

- configuration inheritance is explicit, source-based, and identity-based

Reject this rule:

- configuration inheritance is based on filesystem locality

Keep this distinction:

- company config source is canonical
- workspace is optional convenience

Preserve this narrow feature:

- `app_type -> verifier_sequence` remains useful as a layered dispatch rule

But upgrade the broader model so a larger company can support:

- many repos
- partial local checkouts
- repo-specific setup contracts
- gradual `da` feature rollout
- cross-repo coordination without a mandatory shared root
