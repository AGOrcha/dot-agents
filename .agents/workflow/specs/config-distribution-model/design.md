# Config Distribution Model

**Status:** design artifact — canonical reference

**Purpose:** define the two-tier model that governs how `dot-agents` fetches, resolves,
and distributes configuration. This spec is the authoritative source on:

- the `sources` / `extends` / `packages` field surface in `.agentsrc.json`
- the distinction between config layers (policy) and executable packages (artifacts)
- the two-pass resolution engine
- the unified lockfile format
- per-tier caching semantics
- the audit event taxonomy additions that cover config resolution
- the `da config explain` command contract

**Upstream context:**
- Config layer semantics (precedence order, merge rules, repo identity, feature rollout,
  workspace model) live in [org-config-resolution](../org-config-resolution/design.md).
- Transport details, auth providers, OCI wire protocol, FIPS posture, and package signing
  live in [external-agent-sources](../external-agent-sources/design.md).
- This spec defines the interface where those two tracks meet.

---

## Table of contents

1. [Why two tiers](#1-why-two-tiers)
2. [Tier definitions](#2-tier-definitions)
3. [`.agentsrc.json` field surface](#3-agentsrcjson-field-surface)
4. [Source types and tier constraints](#4-source-types-and-tier-constraints)
5. [Reference syntax](#5-reference-syntax)
6. [Two-pass resolution engine](#6-two-pass-resolution-engine)
7. [Lockfile format](#7-lockfile-format)
8. [Caching semantics](#8-caching-semantics)
9. [Audit event taxonomy](#9-audit-event-taxonomy)
10. [Effective-config explain command](#10-effective-config-explain-command)
11. [Error contract](#11-error-contract)
12. [Scope boundaries](#12-scope-boundaries)
13. [Command surface migration plan](#13-command-surface-migration-plan)
14. [Open questions](#14-open-questions)

---

## 1. Why two tiers

Industry converges on two distinct distribution patterns for developer tooling config:

- **Config-as-source** (Renovate presets, ESLint shareable configs, Kustomize bases,
  Terraform modules from git): raw structured files fetched from a versioned source,
  merged by the consumer, policy-oriented, changes at human pace.
- **Config-as-package** (Helm OCI charts, Buf modules, npm packages, OCI images):
  versioned artifacts with media types, digest-pinnable, code-executable, changes
  at software release pace.

Conflating them into one mechanism produces the wrong tradeoffs for both:

| Concern | Config layers need | Executable packages need |
|---|---|---|
| Versioning overhead | low (git ref or tag) | full (semver + digest) |
| Signing urgency | lower (pure data) | high (executes on host) |
| Cache invalidation | TTL-based (policy can drift) | content-addressed (immutable) |
| Distribution primitive | git / HTTP / local | OCI registry |
| Update cadence | operator-driven (`da config sync`) | explicit (`da packages update`) |
| Blast radius if tampered | policy injection | code execution |

The two-tier model assigns each concern to the right primitive without forcing either
into an unnatural fit.

---

## 2. Tier definitions

### Tier 1 — Config layers (policy)

Config layers are structured JSON objects that carry organization, team, or repo policy.
They are fetched as raw files from declared `git`, `http`, or `local` sources.

Examples of what a config layer carries:

- verifier profile vocabulary
- `app_type_verifier_map` entries
- feature flag defaults
- approved source registry list
- repo-specific prompt overlays
- agent and skill declarations

Config layers are **not** executable artifacts. They have no binary payload, no OCI
media type, and no code surface.

Named examples: `org/base`, `team/payments-platform`, `repo/po-core-api-se`.

### Tier 2 — Executable packages (artifacts)

Executable packages are versioned OCI artifacts with typed media types. They contain
runnable code or prompt bundles that the tool loads and executes.

Examples: `skill/review-pr`, `verifier/playwright-api`, `agent/impl-agent`.

Packages may be declared in repo-local `.agentsrc.json` directly, or inherited through
a config layer. An org or team layer may inject package declarations that every repo
inheriting that layer automatically receives.

---

## 3. `.agentsrc.json` field surface

Three new top-level fields:

```json
{
  "version": 2,
  "repo_id": "github.com/acme/manager-ui",
  "project": "manager-ui",

  "sources": [
    {
      "id": "acme",
      "type": "git",
      "url": "git@github.com:acme/da-config.git",
      "ref": "main",
      "cache_ttl": "4h"
    },
    {
      "id": "acme-pkgs",
      "type": "oci",
      "url": "oci://registry.acme.internal/dot-agents",
      "auth": { "provider": "credential-helper" }
    }
  ],

  "extends": [
    "acme:org/base",
    "acme:team/frontend"
  ],

  "packages": [
    "acme-pkgs:skill/review-pr@^1.2",
    "acme-pkgs:verifier/playwright-api@pinned:sha256:abc123"
  ]
}
```

### `sources`

An ordered array of source declarations. Each source has:

| Field | Required | Description |
|---|---|---|
| `id` | yes | Stable local identifier used in `extends` and `packages` refs |
| `type` | yes | `git \| http \| local \| oci` |
| `url` | yes | Source location |
| `ref` | no | Git branch/tag, or OCI tag (default: `main` for git) |
| `cache_ttl` | no | Duration string for tier-1 TTL (e.g. `"4h"`); ignored for `oci` sources |
| `auth` | no | Auth block; detail delegated to [external-agent-sources](../external-agent-sources/design.md) |

### `extends`

An ordered array of config layer references in the form `source-id:layer-path`. Layers
are applied left-to-right per the precedence rules in
[org-config-resolution §7](../org-config-resolution/design.md#7-merge-and-precedence-rules).

`extends` entries **must** reference `git`, `http`, or `local` sources. Referencing an
`oci` source in `extends` is a schema validation error.

### `packages`

An ordered array of executable package references in the form
`source-id:artifact-path@version-spec`. Version spec follows the format defined in
[external-agent-sources §5](../external-agent-sources/design.md#5-registry-content-model).

`packages` entries **must** reference `oci` or `http` sources. Referencing a `git` or
`local` source in `packages` is a schema validation error.

---

## 4. Source types and tier constraints

| Source type | Valid for `extends` | Valid for `packages` | Notes |
|---|---|---|---|
| `git` | yes | no | Fetches raw JSON layer files by path |
| `http` | yes | yes | Layer files or OCI-compatible HTTP endpoint |
| `local` | yes | no | Filesystem path; dev/test only |
| `oci` | no | yes | OCI Distribution wire protocol |

This constraint is enforced at schema validation time, not at fetch time, so errors
surface before any network call.

---

## 5. Reference syntax

```
source-id : layer-or-artifact-path @ version-spec
```

- `source-id` — must match an `id` in the `sources` array
- `layer-or-artifact-path` — relative path within the source (for git/http) or
  repository path (for OCI)
- `@version-spec` — optional for `extends`; required for `packages`

Version spec forms for packages (from external-agent-sources §5):

| Form | Example | Meaning |
|---|---|---|
| semver range | `@^1.2` | resolve highest compatible release |
| exact tag | `@1.2.3` | resolve exact OCI tag |
| digest pin | `@pinned:sha256:abc...` | immutable content address |

For `extends`, the version spec is the source `ref` (git SHA, tag, or branch). When
omitted, the declared `ref` on the source is used.

---

## 6. Two-pass resolution engine

Resolution always runs pass 1 before pass 2. Pass 2 reads the effective config
produced by pass 1, so packages declared in inherited layers are resolved.

### Pass 1 — Config resolution (policy)

```
for each entry in "extends" (left to right):
  1. identify source by id
  2. fetch layer file from source (cache check first, then network)
  3. validate layer JSON against AgentsRC layer schema
  4. merge into accumulator per category rules

after all extends entries:
  5. merge repo-local .agentsrc.json fields over accumulator
  6. apply plan / task / runtime overrides (highest precedence)

result: effective config object
```

Merge category rules are defined in
[org-config-resolution §7.2](../org-config-resolution/design.md#72-proposed-merge-categories).

Protected fields (`repo_id`, `project`, repo-owned path overrides) are enforced during
step 5: if an imported layer attempts to set a protected field, the field is dropped
and a `config.field.protection_violation` event is emitted (non-fatal warning).

### Pass 2 — Package resolution (artifacts)

```
read effective config "packages" field
for each package ref:
  1. identify source by id
  2. resolve version spec against OCI registry
  3. check local content-addressed cache by digest
  4. if cache miss: fetch blob from registry
  5. write resolved digest to .agentsrc.lock packages section

result: local artifact store ready for tool invocation
```

Pass 2 is skipped if the effective config has no `packages` entries.

---

## 7. Lockfile format

`.agentsrc.lock` is a committed JSON file with two sections:

```json
{
  "lock_version": 1,
  "config": {
    "acme:org/base": {
      "resolved_sha": "a3f9c2d1e8b4...",
      "fetched_at": "2026-04-19T14:00:00Z",
      "ttl_expires_at": "2026-04-19T18:00:00Z"
    },
    "acme:team/frontend": {
      "resolved_sha": "d87b41f0c3a2...",
      "fetched_at": "2026-04-19T14:00:00Z",
      "ttl_expires_at": "2026-04-19T18:00:00Z"
    }
  },
  "packages": {
    "acme-pkgs:skill/review-pr": {
      "resolved_tag": "1.2.3",
      "digest": "sha256:abc123def456...",
      "fetched_at": "2026-04-19T14:00:00Z"
    },
    "acme-pkgs:verifier/playwright-api": {
      "resolved_tag": "pinned",
      "digest": "sha256:def456abc123...",
      "fetched_at": "2026-04-19T14:00:00Z"
    }
  }
}
```

### Config section semantics

- `resolved_sha` is the git commit SHA or content hash at fetch time
- `ttl_expires_at` is derived from the source `cache_ttl`; absent means never re-check
  automatically (requires explicit `da config sync`)
- On TTL expiry: re-fetch and update SHA; emit `config.source.fetch` event
- On re-fetch: if SHA changed, re-run pass 1 and re-write lockfile

### Package section semantics

- `digest` is the OCI content digest; immutable once written
- No TTL; packages do not expire automatically
- Update via `da packages update [package-ref]` which re-resolves the semver range

### Update commands

| Command | Effect |
|---|---|
| `da config sync` | Re-fetch all config layers regardless of TTL; re-run pass 1 |
| `da config sync --layer acme:org/base` | Re-fetch one layer |
| `da packages update` | Re-resolve all semver package ranges; write new digests |
| `da packages update acme-pkgs:skill/review-pr` | Re-resolve one package |

---

## 8. Caching semantics

### Tier 1 — Config layer cache

Location: `~/.agents/cache/config/<source-id>/<layer-path>/<sha>/layer.json`

- Content-addressed by git SHA (stable once fetched for that SHA)
- TTL governs when to check for a new SHA, not when to evict the cached content
- Offline behavior: use last resolved SHA from lockfile; emit `config.source.fetch`
  with `outcome: cache_hit_offline`; proceed if SHA is present in cache

### Tier 2 — Package artifact cache

Location: `~/.agents/cache/packages/<digest>/`

- Strictly content-addressed; never expires
- Eviction only via explicit `da cache prune` with age or size threshold
- Offline behavior: use cached content if digest is present; fail deterministically
  with `registry.blob.fetch` error if digest is absent

---

## 9. Audit event taxonomy

These events extend the taxonomy defined in
[external-agent-sources §8](../external-agent-sources/design.md#8-audit-logging-cmmc-au-2--au-3).

All events share the base schema `{ timestamp, actor, principal, action, target, outcome, trace_id }`.

### Config tier events

| Action | Target | Notes |
|---|---|---|
| `config.source.fetch` | `source_id` | includes `resolved_sha`, `cache_hit: bool` |
| `config.layer.resolve` | `source_id:layer_path` | includes `field_count`, `sha` |
| `config.field.overridden` | `field_path` | includes `from_layer`, `to_layer`, `value_summary` |
| `config.field.protection_violation` | `field_path` | includes `attempted_by_layer`; outcome: `dropped` |
| `config.import.failed` | `source_id:layer_path` | includes `reason: transport\|auth\|content\|schema` |
| `config.effective.produced` | `repo_id` | includes `layer_count`, `package_count`, `trace_id` |

### Package tier events (supplements external-agent-sources §8)

The existing `registry.*` events cover package fetching. Add:

| Action | Target | Notes |
|---|---|---|
| `packages.resolve.start` | `repo_id` | begins pass 2 |
| `packages.lock.updated` | `repo_id` | includes changed package refs |

---

## 10. Effective-config explain command

`da config explain [field-path]`

Walks the resolved layer stack and reports where each field value originated.

### Single field

```
$ da config explain app_type_verifier_map.go-http-service

Field:   app_type_verifier_map["go-http-service"]
Value:   ["unit", "api", "integration"]

Layer stack:
  [1] product defaults             → not set
  [2] user-local                   → not set
  [3] acme:org/base  @ a3f9c2d    → ["unit"]
  [4] acme:team/frontend           → not set
  [5] repo-local .agentsrc.json   → ["unit", "api", "integration"]   ← active
  [6] plan / task override         → not set
```

### Full effective config

```
$ da config explain --all
```

Outputs the effective config object with each field annotated by its winning layer.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Resolution succeeded |
| 1 | One or more layers failed to fetch (details in stderr) |
| 2 | Schema validation error in a fetched layer |
| 3 | Auth failure fetching a required source |

---

## 11. Error contract

All import failures must identify:

- which `extends` or `packages` entry failed
- which source it was expected from
- the failure category: `transport | auth | content | schema | not_found`

Missing required layers fail loudly and halt the resolution pass. There is no partial
resolution fallback. This aligns with the rule in
[org-config-resolution §6.4](../org-config-resolution/design.md#64-missing-import-behavior).

### Distinguishing required vs. optional imports

A layer entry may be marked optional:

```json
"extends": [
  "acme:org/base",
  { "ref": "acme:team/experimental", "optional": true }
]
```

Optional entries that fail to fetch are skipped with a `config.import.failed` warning
event. Non-optional entries that fail halt resolution.

---

## 12. Scope boundaries

This spec owns: the field surface, the two-pass resolution engine, the lockfile format,
the caching semantics per tier, the audit taxonomy additions, and the explain command.

This spec defers to:

- **[org-config-resolution](../org-config-resolution/design.md)** for: layer precedence
  order (§4), merge category rules (§7.2), protected field list (§7.4), repo identity
  model (§5), feature rollout model (§11), workspace semantics (§9), cross-repo
  planning (§10).

- **[external-agent-sources](../external-agent-sources/design.md)** for: auth provider
  model (§4), OCI artifact media types (§5), OCI wire protocol details (§6), FIPS
  posture (§7), full audit event base schema (§8), trust and attestation roadmap (§9),
  package version semantics (§5), migration from git-only sources (§12).

---

## 13. Command surface migration plan

The two-tier model introduces new lifecycle verbs (`config sync`, `packages update`,
`config explain`) and new persistent state (`.agentsrc.lock`, tiered cache). Before
implementation, the following decisions must be locked.

### 13.1 New command subtree: `da config`

All config-tier operations live under a new `config` subcommand. This avoids
colliding with the existing `da sync` (git ops on `~/.agents`) and `da explain`
(human documentation) roots.

| Command | Description |
|---|---|
| `da config sync` | Re-fetch all config layers; re-run pass 1; update lock config section |
| `da config sync --layer source-id:path` | Re-fetch one layer only |
| `da config explain [field-path]` | Show effective config and layer provenance |
| `da config explain --flags` | Show feature flag resolution across all layers |
| `da config lint` | Validate all declared layer files against the AgentsRC layer schema |
| `da config verify` | Run repo setup contract checks (hooks, binary readiness, doctor) |

### 13.2 New command subtree: `da packages`

All package-tier operations live under a new `packages` subcommand.

| Command | Description |
|---|---|
| `da packages install` | Install all packages declared in effective config; write lock packages section |
| `da packages update [ref]` | Re-resolve semver ranges; write new digests to lock |
| `da packages list` | Show installed packages with resolved version and digest |
| `da packages publish <type> <path>` | Publish an agent, skill, verifier, or bundle to a declared OCI source |

`publish` replaces the conceptual `dot-agents publish agent|verifier|skill|bundle`
from [external-agent-sources §5](../external-agent-sources/design.md#5-registry-content-model).
It is scoped under `packages` to make the source-of-truth (OCI registry) explicit and
to keep the local authoring flow (`da agents add`, `da skills promote`) separate from
the distribution flow.

### 13.3 Disposition of existing commands

| Existing command | Current purpose | Decision |
|---|---|---|
| `da sync` | Git ops on `~/.agents` (pull, push) | **Keep as-is.** Do not steal for config layer refresh. |
| `da explain` | Human documentation lookup | **Keep as-is.** Do not steal for config explain. |
| `da install` | `~/.agents` projection and platform link setup | **Repurpose.** New behavior: runs `da config sync` then `da packages install` as a combined setup step. Old projection behavior deprecated with a warning until removed. |
| `da refresh` | `~/.agents` projection refresh | **Alias.** Becomes a thin alias for `da config sync` with a deprecation notice. Remove after one release cycle. |
| `da import` | Ad-hoc local agent/skill import | **Scope-reduce.** Retained for local-first authoring only (`local` source type). Package refs from `oci` sources must use `da packages install`. Add deprecation path for OCI-capable use cases. |

### 13.4 Health surfaces: `da status` and `da doctor`

New persistent state introduced by this spec must have an explicit inspection and
repair surface.

**`da doctor` additions:**
- Config layer staleness: warn if any layer TTL has expired and `da config sync` has
  not been run
- Lock drift: error if `.agentsrc.json` declares a layer or package not present in
  `.agentsrc.lock` (indicates `da install` or `da packages install` has not been run
  since the last edit)
- Missing package digests: error if a package in the lock section has no local cache
  entry and the network is available
- Optional import failures: warn on any `config.import.failed` events in the last run
  that were optional (non-fatal but visible)

**`da status` additions:**
- Config layer freshness: show each declared layer with its resolved SHA and TTL
  expiry time
- Package install status: show each package with resolved tag, digest, and cache
  presence

### 13.5 Repo setup contract entry point

Setup contract checks defined in
[org-config-resolution §8.5](../org-config-resolution/design.md#85-repo-specific-setup-contract)
(hooks, local binary builds, readiness checks) run at two entry points:

- `da install` — always runs setup contract as part of combined setup
- `da config verify` — standalone verification without re-fetching layers

`da doctor` detects if setup contract checks have never run (no recorded evidence)
and directs the user to `da install` or `da config verify`.

### 13.6 Feature rollout and command gating

Feature flags resolved in pass 1 (see
[org-config-resolution §11](../org-config-resolution/design.md#11-feature-rollout-model-for-new-da-capabilities))
gate command availability:

- If a feature is `"disabled"`, the corresponding command exits with a clear message:
  `feature 'graph_bridge' is not enabled for this repo — see da config explain --flags`
- If a feature is `"preview"`, the command runs with a visible preview banner
- `da config explain --flags` always runs regardless of feature state

Feature gating is enforced at command entry, not buried in flag checks.

---

## 14. Open questions

### Q1: Layer file schema validation at publish time

Should config layers in a git source repo be validated against the `AgentsRC` layer
schema at publish time (e.g. a CI check on the config repo), or only at client fetch
time? Publish-time validation catches errors before they reach any repo. Fetch-time
validation is the minimum viable guarantee.

Recommendation: both — client always validates on fetch; config repo CI should run
`da config lint` on every push. The lint command spec is out of scope for this doc.

### Q2: Team-owned source declarations in inherited layers

Can a team layer file itself declare new `sources` entries that repos inherit? This
would let a team layer say "also pull from our team registry" without requiring every
repo to duplicate that source declaration.

Risk: a compromised team layer could inject a malicious source. Needs an org-level
allowlist of permitted source URLs before this is safe to enable.

### Q3: Config layer signing timeline

Org config layers carry policy with high blast radius (affects verifier chains across
all repos). External-agent-sources flags signing as v2. Should config layers be an
exception — requiring earlier signing than skill/agent packages?

### Q4: `da config explain` output format for CI

The explain command output above is human-readable. CI pipelines may need a
machine-readable form (`--format json`). Define the JSON schema before v1.5 ships
so tooling can depend on it.

### Q5: Lockfile for workspace-level installs

For a developer running one `dot-agents` binary across multiple repos (the `payout`-
style workspace), should there be a workspace-level lockfile that aggregates resolved
SHAs across repos, or does each repo own its own lockfile exclusively?
