# Organization Config Resolution And Repo Identity

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

The current config loader does not yet treat `verifier_profiles` and `app_type_verifier_map` as first-class typed `AgentsRC` fields. They are parsed by the workflow fanout path and otherwise preserved as extra JSON.

That is acceptable for a local dispatch experiment, but it is too weak a contract for organization-wide layering and inheritance.

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
  "$schema": "https://dot-agents.dev/schemas/agentsrc.json",
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
  "$schema": "https://dot-agents.dev/schemas/agentsrc.json",
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
