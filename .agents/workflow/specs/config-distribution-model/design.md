# Config Distribution Model

**Status:** design artifact — canonical reference

> **v2 coherence layer — read §15 first.** §15 ("Coherence model") folds in the former
> `config-v2-coherence` spec and is the current authority. It **supersedes the two-tier
> framing** of §1–§2 (the `config`/`packages` tiers collapse into one `units` model with a
> `kind`) and upgrades §4, §6, §7, and §8. §1–§14 retain their rationale and history; where
> they conflict with §15, §15 wins.

**Purpose:** define the model that governs how `dot-agents` fetches, resolves, and
distributes configuration. Originally a two-tier model (§1–§2); §15 unifies the tiers into
one `units` model. This spec is the authoritative source on:

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
15. [Coherence model (v2): scopes, sources, units, and the lock](#15-coherence-model-v2-scopes-sources-units-and-the-lock) — **current authority**

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

> **Superseded by §15 (D3).** The Tier-1/Tier-2 wall below is retained for history. In the v2
> model the two tiers collapse into one `units` model where the former tiers become a per-unit
> `kind` (`layer` vs `artifact`); the two-pass engine survives only as a dependency order, not a
> tier boundary.

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

> **Superseded by §15 (D3/D8/D15).** The source/kind matrix below is retained for history. D8
> relaxed `packages` to all four sources; **D15 (extends-oci-relax) removes the last asymmetry —
> `oci` is now valid for `extends` too**, so every source serves every kind. The constraint is no
> longer schema-enforced: source no longer constrains kind. What stays enforced (at fetch time) is
> the unit's **media type** matching its declared kind — a config layer carries
> `application/vnd.dot-agents.config-layer.v1+json`, an artifact the artifact-bundle media type.

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

> **Superseded by §15 (D4/D5/D7, R1/R7).** The `config` + `packages` two-section layout below is
> retained for history. In the v2 model the lock carries one `units` section (keyed by
> `source:path@version`) plus a top-level content-hash `inputs_digest`; `adapters` stays a peer
> section. Staleness is digest-driven, never TTL/clock-driven (TTL becomes a review-nudge). The §7
> extends-only `config` section is dual-written during a soak, then dropped.

`.agentsrc.lock` is a committed JSON file — the single resolved-state companion to
`.agentsrc.json`. It carries three sections, each owned by a distinct writer:

| Section | Owner | Contents |
|---|---|---|
| `config` | config resolver (this spec, two-pass engine §6) | resolved config-layer SHAs + TTL |
| `packages` | package resolver (this spec, pass 2 §6) | resolved OCI tags + content digests |
| `adapters` | graph-backend adapter (graph-backend-adapter-contract §10.1) | activated adapter source/schema digests + per-materialized-view state machine |

There is exactly one lockfile. The adapter lockfile defined in
graph-backend-adapter-contract §10.1 is **not** a separate file — it is the `adapters`
section of this document. See [§7.4](#74-section-ownership-and-concurrent-writes) for the
read-modify-write discipline that lets independent writers share one file.

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
  },
  "adapters": {
    "kuzu": {
      "source_digest": "sha256:aa11bb22...",
      "schema_digest": "sha256:cc33dd44...",
      "activated_at": "2026-04-19T14:00:00Z",
      "materialized_views": {
        "decision_index": {
          "view_digest": "sha256:ee55ff66...",
          "view_status": "ready",
          "depends_on": [
            { "adapter": "kuzu", "schema_digest": "sha256:cc33dd44...", "version": "1" }
          ],
          "last_rebuilt_at": "2026-04-19T14:00:00Z"
        }
      }
    }
  }
}
```

The `adapters` section schema (per-adapter `source_digest`/`schema_digest`/`activated_at`,
the per-view `view_status` four-value enum, `depends_on`, and the bounded `state_history`
audit log) is normative in graph-backend-adapter-contract §10.1.1–§10.1.3; this spec owns
only that it lives here, as a peer section of `config` and `packages`.

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

### Adapters section semantics

- Owned and mutated exclusively by the graph-backend adapter lifecycle
  (graph-backend-adapter-contract §10.1). The config/package resolver never reads or writes it.
- Written on adapter activation (init state machine) and on fail-closed reconcile; the
  four-value `view_status` enum and per-view transitions are normative in §10.1.1–§10.1.3.
- Absent `adapters` section ≡ no adapter activated (built-in `none`); a fresh `.agentsrc.lock`
  written by config resolution before any adapter activates simply omits the key.

### 7.4 Section ownership and the shared lockfile writer

`.agentsrc.lock` has three section writers (config resolver, package resolver, adapter
lifecycle) that run at different times and may run while another section is already populated.
They do **not** each open and rewrite the file independently. Instead they share a single
**lockfile writer** (a small dedicated package, e.g. `internal/agentslock`, that both config-v2
and the graph adapter depend on — neither imports the other):

- **Schema-agnostic section buffer.** The writer owns the whole document and treats sections as
  **opaque** values: `Section(name, into)` reads a section, `SetSection(name, raw)` stages one.
  It never knows the `config`/`packages`/`adapters` shapes — each subsystem marshals its own
  section and hands the bytes over. This is what keeps the layering clean: the writer is the
  only shared surface, and adding a fourth section later needs no change to it.
- **Load once, flush once.** A command opens the writer (which loads the current file, so any
  section another subsystem already wrote is in hand), each active subsystem stages its section
  into the in-memory buffer, and the command flushes **once**. This collapses what would
  otherwise be N separate read-whole/mutate/write-whole cycles (e.g. `da install` touching
  `config` *and* `packages`) into a single atomic write — one `fsync`+`rename`, no intra-process
  double-read. Throughput is not the point (the lockfile is written a handful of times per
  invocation, never in a loop); the point is fewer writes and no partially-updated intermediate
  states.
- **Parallel resolution, serialized write.** Where throughput *does* matter is the **resolver
  stage**, and that stays maximally parallel: pass-1 config layers and pass-2 packages are
  fetched/resolved concurrently (network-bound — see §6), each producing its section content.
  The shared writer is the safe convergence point for those parallel producers: `SetSection` is
  concurrency-safe (in-process mutex), so resolver goroutines stage their results into the buffer
  without racing, and the single flush is the one serialized write. Producers fan out to the
  degree the resolver can; only the write is serial. The writer never throttles resolution — it
  just guarantees that however parallel the producers are, the on-disk lockfile is always written
  safely and whole.
- **Flush preserves untouched sections (this is the RMW guarantee).** Because the writer loaded
  the current document and only replaces staged sections, a flush writes the whole document back
  with sibling sections verbatim. This holds the cross-invocation contract too: a later, separate
  `da` process opens a fresh writer, loads the file written by the earlier process, and stages
  only its own section. The read-modify-write discipline lives **inside** the writer's
  load/flush, not in three hand-rolled copies.
- **Atomic replace.** Flush writes to a temp file in the same directory and `rename(2)`s over the
  target, so a concurrent reader sees either the old or new whole document, never a partial.
- **Flush is callable more than once.** The single-flush case is the optimization, not a
  constraint: a command may flush `config` before a slow adapter activation (crash-safety) and
  flush `adapters` after — each flush is atomic and section-preserving.
- **Locking.** The single writer guards concurrent `SetSection` calls with an in-process mutex
  (what makes parallel resolution above safe), and is the natural home for a future cross-process
  file-lock if the background service ever writes sections from separate processes concurrently
  (tracked in r3). v1 needs no cross-process file lock — within one invocation a single
  mutex-guarded writer instance serves all goroutines, and across invocations the
  load+atomic-flush discipline tolerates the interleavings.
- **`lock_version`** is shared across all sections; bumping it is a coordinated migration, not
  a per-section concern.

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

> **Field-name note (verified against shipped code).** The examples below name
> `app_type_verifier_map`/`verifier_profiles` for historical continuity, but those are
> **deprecated legacy keys** — read and folded into the unified
> `execution_profile` / `stage_profiles` model on load (`internal/config/agentsrc.go`
> `foldLegacyProfiles`), never re-emitted. The live field a real `explain` walk reports is
> `execution_profile.by_app_type.<type>.topology.verifier_sequence` (the
> `app_type_verifier_map` successor, `internal/config/execution_profile.go`) and the
> `stage_profiles.<stage>.<id>` map (the `verifier_profiles` successor). The provenance/layer-stack
> mechanic shown here is unchanged; only the field names moved.

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

### Resolved execution manifest visibility (planning input, 2026-05-26)

When `app_type` profiles replace or supplement `app_type_verifier_map`, config
explain must expose the whole resolved pipeline contract, not only the
selected profile name. `da config explain app_type --json` should include:

- selected and resolved profile ref, resolved version, content digest, and
  composition expansion;
- ordered verifier chain, review kind/skill, graph adapter, and the adapter's
  derived impact-radius contract;
- selected named stage-agent and reviewer-lens refs, shared stage-instruction
  ref, and stage-safe product/project overlay refs;
- staged versus legacy/full-slice execution mode and return-gate/closeout
  policy; and
- field-level provenance and source/lock digests for each resolved component.

Per external-agent-sources §5.1, the selected profile and overlay policy are
Tier 1 config-layer values; executable agent, verifier, or skill refs are
Tier 2 package values. The effective snapshot must preserve that distinction,
and must not confuse a registry bundle pointer manifest with the repo-local
delegation bundle written for a task.

Per org-config-resolution §8.6, the snapshot must also expose inherited
constraints and override outcomes for stage agents, reviewer lenses,
overlays, verifier chains, return/closeout policy, execution mode, and
package trust rules. Rejected or explicitly authorized task/runtime
exceptions are explain output, not hidden runtime behavior.

`workflow app-types --verbose`, bundle materialization, config validation, and
`config explain` should consume one effective-config snapshot API. Otherwise
the authoring surface can advertise a valid profile that dispatch resolves
differently, and review cannot reproduce which instructions a worker
received.

### Installed starter-seed upgrade boundary (planning input, 2026-05-27)

The embedded shared-home starter and an already-installed user configuration
are separate update lanes. Today `da init` copies starter assets only when a
destination file is missing; this correctly protects customized local
profiles, agents, and skills, but it also means corrected shipped defaults do
not update an existing `~/.agents` installation.

Once staged instruction selection and profile resolution become config-backed,
the canonical implementation plan must define non-destructive seed migration:

- version or digest the shipped starter baseline for profiles, agents, and
  workflow skills;
- expose shipped-versus-installed provenance and drift for starter-owned
  surfaces through config explain or an adjacent inspect command;
- allow automatic refresh only for an installation proven unchanged from a
  prior shipped seed, or require an explicit reviewed apply operation; and
- provide a migration path that converts installed legacy/full-slice
  `loop-worker` material into the stage-safe model without overwriting local
  overlays or user customizations.

Updating embedded defaults is therefore necessary for new installs, but is
not evidence that installed configuration has been remediated.

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
| `da config relevance` | Report the resolved relevance/topology/lens facets for the current `app_type`×stage (the `execution_profile` working-set view; `--filter units\|topology\|lenses\|all`, `--json`). Shipped, `commands/config/relevance.go` (skill-relevance-filter). |
| `da config migrate` | Opt-in v1→v2 `.agentsrc.json` migrator: backs the original up to `.agentsrc.json.v1.bak`, folds legacy keys into `stage_profiles`/`execution_profile`, bumps `version`, idempotent, `--dry-run`. Shipped 0.4.1, `commands/config/migrate.go` (#138). |

> **Shipped surface (verified 2026-06-25).** The full `da config` subtree on master is
> `explain` / `sync` / `lint` / `verify` / `relevance` / `migrate` (`commands/config/`).
> `relevance` and `migrate` are the two new verbs that surfaced after the original §13.1
> table; they are folded in above rather than tracked in a sibling doc.

### 13.2 New command subtree: `da packages`

> **Superseded by §15 (D3) — not shipped.** The collapse of the config/packages tier wall into
> one `units` model retired the parallel `da packages` command tree: artifacts are `kind:artifact`
> units resolved by the same resolver and CRUD'd via `--scope`/`--source`, not a separate verb
> family. No `commands/packages/` package exists on master (verified 2026-06-25); the migration
> task that would have built it (`config-v2-migration/p6`) is **cancelled**, its surviving
> artifact-resolution mechanic folded into `config-v2-coherence/ce-unified-artifact-sourcing`. The
> table below is retained for history. `publish` is not implemented; OCI artifact publish remains a
> v2 roadmap item owned by external-agent-sources.

All package-tier operations live under a new `packages` subcommand.

| Command | Description |
|---|---|
| `da packages install` | Install all packages declared in effective config; write lock packages section |
| `da packages update [ref]` | Re-resolve semver ranges; write new digests to lock |
| `da packages list` | Show installed packages with resolved version and digest |
| `da packages publish <type> <path>` | Publish an agent, skill, verifier, or bundle to a declared OCI source |

`publish` replaces the conceptual `da publish agent|verifier|skill|bundle`
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

> **Superseded by §15 (D12) — shipped reshape differs.** The "repair surface" framing below is
> historical. As shipped (`commands/internal/lifecycle/{status,doctor}.go`, verified 2026-06-25):
> **`da doctor` is read-only and never repairs** — it surfaces health, driver-event drift, and
> per-source review-nudges only; and **`da status` is fleet/link-health only** — it dropped all
> config-value/freshness reporting, which now lives solely in `da config explain` (the single
> effective-config truth surface, which auto-locks). The staleness model the surfaces report is the
> §15 D4 content-hash/`inputs_digest` driver-event model, not the §13.4 TTL/clock model below (TTL
> is demoted to a review-nudge). Read the §13.4 bullets as the original intent; §15 D12 is the
> shipped contract.

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
exception — requiring earlier signing than skill/agent packages? | Human Addendum 2026-05-10T12:57 Yeah, let's bring in signing earlier then. |

### Q4: `da config explain` output format for CI

The explain command output above is human-readable. CI pipelines may need a
machine-readable form (`--format json`). Define the JSON schema before v1.5 ships
so tooling can depend on it.

### Q5: Lockfile for workspace-level installs

For a developer running one `dot-agents` binary across multiple repos (the `payout`-
style workspace), should there be a workspace-level lockfile that aggregates resolved
SHAs across repos, or does each repo own its own lockfile exclusively? | Human Addendum 2026-05-10T12:56 (EST) So like git where the gitdir is just a pointer to the parent's .git/.. we should have it be either-or so that if by it's self then it has it, if in a workspace like payout then workspace manages the aggregate. |

---

## 15. Coherence model (v2): scopes, sources, units, and the lock

**Status:** canonical (folded in from the former `config-v2-coherence` spec, which graduated
from proposals `config-v2-coherence-scopes-sources-lock.md` + `section-7a-units-lock-wiring.md`).
**Coherence amendments folded (2026-06-27):** the three owner-ratified amendments in
`.agents/proposals/config-distribution-model-coherence-amendments.md` (rev 2, all 8 forks
OWNER-RATIFIED 2026-06-27; merged to master via PR #188) are now folded into this section:
**A1** — D1 rewritten as **D1a** (the scope axis splits into AUTHORITY-RANK vs VALUE-PRECEDENCE,
plus the source-authority registry); **A2** — D13 split (portable project IDENTITY becomes the
first-class `kind: project-set` unit; machine-local BINDING stays non-scoped); **A3** — D3 gains a
**conditional** fourth resolver behavior `kind: descriptor`. These amendments are substrate
(contract) changes; the resolver-code work they obligate is itemized in **§15.9** and shipped by a
separate follow-on PR.
**Design philosophy:** agent DX is the primary operator; humans are a secondary, cross-cutting
audience.

This section is the v2 refinement of the model defined in §1–§13. It **supersedes the
two-tier framing** (§1–§2): the Tier-1 `config` / Tier-2 `packages` wall collapses into one
`units` model carrying a `kind` (see D3). It also upgrades §4 (tier-constraint relaxation, D8),
§6 (two-pass → layer-fixpoint-then-artifacts *dependency order*, not a tier wall, D3), §7 (one
`units` lock section + `inputs_digest`, TTL→nudge, D4/D7), and §8 (per-source caching, D4/D6).
Earlier sections retain their rationale and history; where they conflict with §15, §15 is
authoritative.

The remaining `config-v2-migration` finish-line tasks (**p4f** units-lock wiring, **p4c-rest**
`config sync`/`lint`, **p1c** verifier-profile migration) are accountable to this section; their
success criteria trace back to §15.5.

### 15.1 Problem

Adding a uv-style "detect staleness → auto re-resolve → auto-sync outputs" lockfile to config-v2
surfaced a deeper incoherence that must be resolved first, so auto-sync is built on a coherent
model rather than bolted onto the current overloaded one. Four questions the pre-§15 model
answers incoherently:

1. **Config CRUD is half-old/half-new.** `da skills|agents|hooks|rules|mcp|settings` author files
   directly into `~/.agents/` (the "old" model), while the extends/packages/lock machinery is the
   "new" model. The same logical slot (a skill) is half authored-file, half resolvable-unit.
2. **The lock is ambiguous.** It is unclear what `.agentsrc.lock` tracks and what makes it stale.
   The §7 live model locks only resolved `extends` refs, so a project with no remote extends gets
   *no meaningful lock at all* — and flat/local-only projects get no lockfile.
3. **Three axes are fused.** `~/.agents/` (`AgentsHome()`, ~40 call sites) simultaneously serves a
   config **scope** (precedence), a canonical **asset store** (source material), genuinely-local
   operational **state**, and a fetch **cache** — fusing *scope*, *source*, and *kind*.
4. **Command intents blur.** Overlapping inspection surfaces (`status` / `doctor` / `config
   explain`) with unclear ownership; a `sync` vs `config sync` verb collision.

### 15.2 Goals

- A single coherent model where every resolvable thing — a config layer *or* an artifact like a
  skill — is addressed uniformly and tracked uniformly in one lock.
- A lockfile that exists for **every** resolved project (including flat/local-only), whose
  staleness is **content-hash driven, never clock-driven**.
- CRUD, sourcing, and versioning that are the same whether a unit is authored locally or pulled
  from a remote registry — differing only by *source*.
- Crisp, non-overlapping command intents (one truth surface for effective config; one for fleet
  health; one for nudges).
- uv-proven ergonomics (`--locked` / `--frozen` / `--no-sync` / `--offline`; explicit-only
  upstream re-check; exact/prune projection).

### 15.3 Decisions (with rationale)

Each decision was settled in design review; the rejected alternative is noted.

#### D1 — Three orthogonal axes, not one overloaded directory  *(amended → D1a)*

> **Superseded by D1a (coherence fold, 2026-06-27, Amendment 1).** The original D1 below is
> retained for history. It named the scope axis as a **single** ordering and said it "*merges*
> into effective **policy**" — silently conflating two orthogonal things (who may *lock/cap* vs
> whose *value* wins) under one chain. D1a splits them into two explicitly-named orderings and adds
> the source-authority registry. The original value-precedence chain is preserved **verbatim** as
> D1a's VALUE-PRECEDENCE ordering, so no shipped value-merge behavior changes.

> A resolvable unit is described by three independent axes:
> - **Scope** (precedence; *merges* into effective policy): product → user-local → org → team →
>   repo-imported → repo-local (committed) → **project-local overlay (uncommitted)** → runtime.
>   Answers "who gets the last word." (Extends `org-config-resolution` §4 with one new scope — see D9.)
> - **Source** (origin/transport; *resolves + versions* a unit): `git` / `http` / `local` / `oci`.
>   Answers "where a unit comes from and how it is versioned." Orthogonal to scope.
> - **Kind** (behavior): `layer` (mergeable policy) vs `artifact` (installable executable).
>
> *Rejected:* keeping scope/source/kind fused in one directory with special-cased paths — the root
> cause of the half-old/half-new feeling.

#### D1a — Scope carries TWO orderings (authority-rank ≠ value-precedence); plus source and kind
A resolvable unit is still described by three axes (**scope**, **source**, **kind**), but the
**scope** axis is not one ordering — it carries **two distinct, co-existing orderings** that the
original D1 collapsed into one. They do not contradict; they govern different questions and run in
two resolver phases (a policy-authority pass, then a value-merge pass).

- **AUTHORITY-RANK — who may emit locks / value-locks / override-caps.** A **single total order**,
  **`org > team > repo > user`**, **deny-overrides** (any sufficiently-authoritative `deny` wins;
  there is **no force-allow** — a lower scope can never punch a capability *through* a higher deny),
  **higher binds lower** (a higher scope's locks/caps are absolute over every lower one). The
  **user is the LOWEST authority rung** — on the chain, beneath repo — and may emit locks/caps
  **only at its own scope**; **every higher scope, including repo, overrides it** (this is why a
  personal/user scope can never constrain a shared repo). **repo MAY set its own local guardrails**
  (lock down *its own* surface) but ranks **below team**, so a team lock still binds the repo.
  *Grounding:* AWS SCP/IAM, VS Code admin policy, and AD **Enforced** GPO all rank an individual
  beneath the shared resource — no security-grade system lets a user out-rank a shared resource.
- **VALUE-PRECEDENCE — whose VALUE wins within the locks the authority pass leaves surviving.**
  **`user / repo / runtime`**, **most-local-wins** — the **user still wins on value** for its own
  preferences. This is the **verbatim preservation** of the original D1 chain (product floor →
  user-local → org → team → repo-imported → repo-local → project-local overlay → runtime) as the
  value-merge ordering. It applies **only** to fields the authority pass left **unlocked and
  within-cap**; a locked field never reaches this pass.
- **LOCKED-FIELD COLLISION.** When a lower scope sets a value a higher scope holds **value-locked**,
  the **lock wins** and the lower write is **rejected/ignored** — and the rejection is
  provenance-visible: `da config explain` reports the **attempted value**, the **winning (locked)
  value**, and the **owning scope** (e.g. "repo-local `model=Y` rejected by org value-lock
  `model=X` ⇒ effective `model=X`"). Cross-authority subtraction is legitimate **only** via a
  higher-scope deny-lock; a lower deny can never erase a higher allow.
- **SPECIAL scopes.** `product` = the **floor** (zero authority, ships defaults, everything
  overrides it). `public` = **VALUE-ONLY** (may supply values at the lowest precedence; **any
  authority/lock claim it ships is IGNORED unless co-signed by a trusted root** — the
  supply-chain-signing guard, so a foreign/public source can never bind via an unsigned lock).
  `runtime` and the project-local overlay = **value-only, zero authority** (highest value-precedence,
  set values but never locks).

**SOURCE-AUTHORITY REGISTRY (net-new substrate concept).** A scope's authority is **never
self-declared** by a unit — it derives from **`ref.source → source-authority registry → scope`**.
The registry carries an optional **`authority_grants`** block: a per-source allowlist of the form
*"source `<S>` may carry the authority of scope `<O>`."* Two invariants gate it:
- **(a) Write-authority.** An `authority_grants` entry **MAY be written only by a strictly-higher
  authority scope.** An org-scope registry may bless a team source to carry org authority; a team
  source may not bless itself or a peer to carry org authority.
- **(b) No self-blessing.** A lower **repo / user / public** layer **CANNOT** grant authority to
  itself or to a source it controls — self-elevation is a **resolve-time rejection**, not a silent
  no-op. This closes the public-source injection vector: a foreign/public source shipping its own
  `authority_grants` claiming org authority is **inert**, because the grant is honored only when
  written by a strictly-higher scope's registry.

**Compatibility (explicit — shipped behavior is unchanged).** (i) The existing value-precedence
chain is preserved **verbatim** as the VALUE-PRECEDENCE ordering; no shipped value-merge behavior
changes. (ii) Any downstream reference to "the D1 scope chain" resolves, by default, to the
VALUE-PRECEDENCE ordering (back-compat). (iii) AUTHORITY-RANK and the source-authority registry are
**net-new**; nothing that exists today silently re-binds. This is the §15-side home for
`unified-config-profiles` Q1 (the one canonical scope ordering + its authority ranks) and Q6 (the
source-authority-registry grant shape); downstream specs **reference** D1a rather than redefining
either ordering.

> **Resolver-code obligation (NOT yet implemented — follow-on PR; see §15.9).** D1a obligates a
> **policy-authority pass** (Phase 1: apply the AUTHORITY-RANK total order, evaluate locks /
> value-locks / override-caps / cross-authority deny-overrides) ahead of the existing value-merge
> (Phase 2), and a **registry write-guard** enforcing invariants (a)+(b) at resolve time. The
> current resolver implements value-precedence only.

*Rejected:* keeping scope/source/kind fused in one directory (the original D1 root cause); and
keeping the scope axis as a **single** ordering — that conflation is exactly what blocked a single
canonical scope ordering downstream.

#### D2 — Versioning and sourcing are unified via `source:path@version`
Everything resolvable is addressed by the reference syntax in §5: `source-id : layer-or-artifact-path
@ version-spec`. A skill authored locally and a skill pulled from a registry are the **same kind of
resolvable unit**, differing only by source.

*Rejected:* tier-specific addressing (config refs vs package refs as separate syntaxes).

#### D3 — The `config`/`packages` tier wall collapses into one `units` model
With sourcing/versioning unified (D2), the §2 Tier-1 `config` vs Tier-2 `packages` split — two
parallel subsystems with different source constraints, lock sections, caches, and command trees —
is no longer warranted. What remains is a single model of **resolvable units** carrying a `kind`
that drives exactly two behaviors:
- **layer**: a JSON field-bundle that *merges* into effective config by category rules;
  protected-field rules apply; it may *declare* further units.
- **artifact**: a bundle *installed discretely* into the asset store and invoked; trust/signing
  rules apply; it does not merge.

**Amended (2026-06-27, Amendment 3) — a CONDITIONAL fourth behavior `kind: descriptor`.** Beyond
mergeable `layer` and installable `artifact`, the substrate reserves a **fourth resolver behavior**
for **descriptors** — declarative, **non-merging, non-installing** projection data (consumed by the
projector to drive per-harness output). Its provenance is **conditional**:
- **Default (until the F4 probe completes):** descriptors stay **internal / probe artifacts** —
  Go-internal declarative data, **NOT** a §15 unit (no `kind`, no lock entry, not a member of
  `inputs_digest`). This holds through `multi-harness-extensibility`'s F4 hand-add-one-harness probe
  (the DC0 experiment that ratifies/refutes the descriptor schema). This amendment supplies only the
  substrate *position*; it does **not** unblock the multi-harness descriptor schema — the F4 probe
  still gates that independently.
- **Only if a descriptor becomes source-shipped** (`multi-harness` §8 "a future external source could
  ship descriptors") does it become a **full §15 unit** — at which point `kind: descriptor` must
  define, before it ships: a **media type** distinct from config-layer/artifact-bundle (mirroring
  D15's media-type guard, so a descriptor blob is never mis-resolved); its **resolver order** (where
  the fourth behavior sits relative to layer-merge and artifact-install); **validation** rules; a
  **lock entry** shape; and its **local `inputs_digest`** participation (in `inputs_digest` when
  authored locally, only in `units` when sourced). Until that source-shipping need is real, none of
  that substrate surface is added.

The **irreducible Go renderer / procedural core is NOT a unit in any case** — it is code shipped
with the binary (`CreateLinks`/`RemoveLinks`, source-priority selection, user-home fanout,
stale-file pruning, semantic hook rendering), per `multi-harness` D2. The descriptor owns the
declarative projection; the named, audited Go core owns the rest.

> **Resolver-code obligation (CONDITIONAL — only fires on source-shipping; see §15.9).** No resolver
> change is owed today: the default keeps descriptors Go-internal. *If* the source-shipping condition
> fires, the follow-on must add the fourth resolver behavior (media type, resolver order, validation,
> lock entry, `inputs_digest` participation).

Consequences: one lock `units` section; one source model (any source serves any kind); one CRUD
surface (a `--scope`/`--source` flag, not parallel command trees). The §6 two-pass resolution
survives only as a **dependency order** (resolve mergeable layers to a fixpoint first — they declare
what else to fetch — then resolve/install the full declared artifact set), **not** a tier wall.

*Rejected:* preserving two subsystems; it duplicates source/lock/cache/command machinery for no
behavioral gain.

#### D4 — Staleness is content-hash driver events; TTL becomes a review-nudge
The lock tracks, per resolved unit, a content `digest` and a top-level `inputs_digest` (a hash of
all **local** scopes). A project is stale iff: `inputs_digest` mismatches (a local manifest changed)
**OR** the declared set of refs changed **OR** a recorded digest no longer matches. Cheap, local, no
network, no clock.

`cache_ttl` **no longer auto-invalidates or auto-fetches**. A per-source `last_checked_at` powers a
`doctor`/`config explain` *review-nudge* ("layer X last re-checked 6d ago — run `da config sync`") —
a separate axis from truth. This converges three sources of truth: §8's "policy can drift vs
content-addressed immutable" table, the KG spec's "staleness = driver events; time = review-nudge,"
and uv (no manifest hash, never auto-upgrades on a timer).

*Rejected:* clock/TTL-driven auto-invalidation (chases upstream on a timer; non-reproducible;
needless git churn).

#### D5 — `inputs_digest` covers all local scopes, whole-normalized
The top-level `inputs_digest` hashes user-local + project-local overlay + repo-local committed
manifests together, normalized. This is the deliberate divergence from uv (which never fingerprints
a mutable local source tree): our "local source" is *managed* config (small, often git-backed), and
clock-free local-scope drift detection is worth the hash.

#### D6 — The `local` source is git-backed and auto-bootstrapped
For local-authored assets to resolve through the same `source:path@version` path as remote ones, the
`local` source (`~/.agents/`, already a git repo) is initialized + made version-resolvable on first
resolve/init. A local skill/agent/layer then carries a real version (the local repo ref); the lock
records `local:<path>@<ref> → digest` like any other source. CRUD becomes "author into the `local`
source working tree." Its cache-key default is the resolved commit (like `git`) **plus working-tree
content** (so authoring before a commit still registers as a driver event).

#### D7 — One asset dir; sourced units are gitignored from the `local` source
Local-authored and remote-materialized units share one asset dir, but remote/sourced units are
excluded from the `local` source's git tracking, so `da sync` never commits fetched assets into the
user's personal repo. Provenance = the unit's source in the lock; git-tracked ⇔ local-authored.

#### D8 — Artifact sourcing is relaxed; OCI is one source, not the definition
`git` / `local` / `http` / `oci` are all valid for **artifacts** (not just OCI). The §4
tier-constraint table updates accordingly.

#### D9 — New `project-local overlay` scope (uncommitted, machine-local)
A personal per-project layer — the `.git/config` analog (local > user) — sits just above repo-local
committed and below runtime. Stored as a da-managed, **gitignored** `.agentsrc.local.json` in the
repo (discoverable, lean, repo tree stays clean of *its* own content because it is gitignored). It is
a **local input** → hashed into `inputs_digest`.

#### D10 — Auto-sync = lock + outputs parity, via one `EnsureResolved` seam
Config-consuming mutating/setup commands call one seam that: (1) computes current `inputs_digest` +
declared set (cheap, local); (2) if fresh ⇒ no-op (no network, no write); (3) if stale ⇒ re-resolve
local scopes (+ cached layers), rewrite the lock; (4) apply the **outputs** half — project the
asset-store union into the repo, **exact/prune by default** (delete managed outputs no longer in the
resolved set). uv-proven flags: `--locked` (assert; error non-zero if the lock would change — CI
gate), `--frozen` (use lock as-is; skip the staleness check), `--no-sync` (skip the outputs apply),
`--offline` (lock/cache only), `--refresh` / `da config sync` (the explicit upstream re-check).
**Upstream re-check is explicit-only** — auto-sync never chases upstream on its own.

#### D11 — CRUD/sync route by `--scope`/`--source` + an editability check (no per-store trees)
A single `--scope`/`--source` flag routes a CRUD or sync op to a source; an **editability check**
governs writes. This replaces both a `sync`→`config sync` rename and the package-shadow guard.
`local` is always writable (personal); team/org are governed by their own external policies via a
**policy-backend-agnostic interface** — now **implemented and resolved** (was D-open-1; see
"Resolved" under §15.6): `WriteAuthorizer` + `Checker` in `internal/config/editability.go`.

#### D12 — Command intents (agent-DX-first)
- `config explain` — **the** effective-policy truth surface (values, provenance, lock freshness,
  flags); **auto-locks** (writes the lock to stay current, like `uv tree`); does not project.
  **Absorbs `status`'s config inspection fully.**
- `status` — managed projects + projection/link health (the fleet view); read-only; drops
  config-value/freshness reporting.
- `doctor` — health + pending driver events + review-nudges; repair *guidance* only; never repairs.
- `refresh` — project the asset-store union → repos (the "sync outputs" half); ensures lock fresh
  first; stays distinct from `config sync`.
- `install` — full setup = resolve + lock + project + setup-contract (the canonical "make everything
  current," ≈ `uv sync`).
- `config sync` — explicit upstream config re-check (≈ `uv --upgrade`).
- `sync` (git) — git-manage a source repo; `--source <id>` selects which (default `local`).

#### D13 — Scoped content routes to its scope's source  *(amended: registry split, 2026-06-27)*
`proposals/`, `context/`, lessons, and asset units are **scoped** (user/repo/team/org) and route to
the source backing that scope (generalizing proposal-routing's global-vs-project split to all four
scopes). Only the fetch `cache/` and the **machine-local binding table** are genuinely non-scoped
local operational state — never a scope, never a source, never projected.

**Amended (2026-06-27, Amendment 2) — the managed-project registry is NOT wholesale
machine-local; it splits into two surfaces.** The original D13 classified the *entire* registry as
non-scoped machine-local state. That is too coarse: portable project identity must travel between
machines while machine paths must not. The split:
- **(a) portable project IDENTITY** (`id` + a portable key, **no path**) is **synced** config,
  promoted to a **first-class `kind: project-set` unit** (a.k.a. identity-registry): scope- and
  manifest-referenceable, layered under the **same selector-merge law** as every other unit. It
  rides the user-local layer like any other portable config; it is **not** non-scoped operational
  state. A single neutral `project-set` unit is the **one owner** that **both** `home-config`
  portability (referencing it at personal scope) **and** a team manifest (referencing it at team
  scope) point at — neither owns the other's surface, so standalone portability does not depend on a
  manifest and team distribution does not reach down into a personal spec.
- **(b) machine-local BINDING** (`id → absolute-path`, plus `added` bookkeeping) **stays exactly as
  D13 said**: machine-local, **never synced, never a scope, never projected**. Caches stay
  machine-local too; credentials remain always-machine-local.

D13's "never a scope" clause now narrows from "the registry" to "the **binding table** (id→path) +
`added` + caches." **Two distinct registries — do not conflate.** The **identity registry** here
(which projects exist + their portable keys) is a **different registry** from the
**source-authority registry** in D1a (which source may carry which scope's authority). They serve
different purposes and are named distinctly.

> **Resolver-code obligation (NOT yet implemented — follow-on PR; see §15.9).** Amendment 2
> obligates a new `kind: project-set` resolver behavior (a synced identity-registry unit with its own
> lock entry + `inputs_digest` participation when authored locally), and a clean separation of the
> machine-local binding table out of any synced/scoped path. The current registry is wholesale
> machine-local.

#### D14 — Managed-resource `.gitignore` auto-fill in consuming projects
`da` owns a delimited, idempotent block in each consuming project's `.gitignore`: projected links,
generated platform configs, the `.agentsrc.local.json` overlay, and materialized asset units are
**ignored**; `.agentsrc.json` and `.agentsrc.lock` stay **committed** (the resolved-state contract,
like `uv.lock`). Re-runs converge (regenerated, not appended).

#### D15 — `extends` accepts OCI; source/kind asymmetry removed (extends D8)
*(the `extends-oci-relax` spec decision; tracked there as "D13" relative to that spec, recorded
here as D15 to avoid the §15 numbering collision with the scoped-content decision above.)*

D8 relaxed **artifact** sourcing to all four source types but left **`extends` (config layers)**
rejecting `oci` — the one remaining source/kind asymmetry, enforced at resolve time by
`SelectFetcher`. D15 removes it: **`oci` is now valid for `extends`** exactly as it is for
`packages`, so **every** source (`local`/`git`/`http`/`oci`) serves **every** kind (layer or
artifact). A config layer is published to OCI as a single blob carrying the layer document under a
dedicated media type **`application/vnd.dot-agents.config-layer.v1+json`** (distinct from the
artifact-bundle media type). The OCI layer fetcher reuses the existing OCI pull plumbing
(digest-addressing, auth, cache, posture) — the shared pull is factored, not duplicated — and
returns the blob as a `FetchedLayer` the resolver merges with no special-casing. A **media-type
guard** keeps `kind` meaningful even though source is now unrestricted: an `extends` pull must
carry the config-layer media type and a `packages` pull the artifact-bundle media type; a mismatch
is a clear schema error, so a layer blob is never installed as an artifact (and vice-versa).
`kind` (not source) continues to govern merge/trust. The user-facing doc flips (the §15 guide
matrix/callout, README, and the `SelectFetcher` error string narrative) follow once #110/#111 land,
per the extends-oci-relax sequencing.

### 15.4 Requirements (behavioral)

R1. **A lockfile exists for every resolved project**, including flat/local-only, carrying
`lock_version`, `inputs_digest`, and a `units` map keyed by `source:path@version` →
`{kind, digest, fetched_at, last_checked_at}`. The `adapters` section is preserved untouched (owned
by `graph-backend-adapter-contract`). Per the coherence fold, `kind` ranges over `layer` /
`artifact` / **`project-set`** (the synced identity-registry unit, D13/A2) and **conditionally**
`descriptor` (only once source-shipped, D3/A3); the **machine-local binding table** (id→path) is
**not** a unit and never appears in the lock. A synced `project-set` (and a locally-authored
descriptor, if/when that condition fires) participates in `inputs_digest` like any other local unit.

R2. **Staleness never consults a clock.** A project is fresh ⇔ `inputs_digest` matches, the declared
ref set is unchanged, and recorded digests match. `last_checked_at` only powers nudges; it never
auto-invalidates.

R3. **`config verify` / `config explain` / `doctor` read the `units` section** and compare a
freshly-computed `inputs_digest` against the lock's recorded value to report **local-scope drift** as
a first-class check, and per-unit digest mismatch as a cache/integrity check — uniformly across
git/http/local/oci. A local-only project shows its tracked `inputs_digest`, not "nothing to verify."

R4. **CRUD (`skills|agents|hooks|rules|mcp|settings`) routes via `--scope`/`--source`** and passes an
editability check before writing; each op registers as a driver event.

R5. **`EnsureResolved` is idempotent and convergent:** fresh ⇒ no-op (no network, no write); stale ⇒
re-resolve + rewrite lock + project outputs exact/prune; the flags in D10 behave exactly as
specified; re-running yields no diff.

R6. **Per-source cache-keys** follow uv's model with per-kind defaults (D4/D6) and are overridable
per source (file/glob, git commit/tags, env, dir-presence). Force escapes: per-unit `--refresh` and a
config-declared always-revalidate marker.

R7. **One `units` lock section** — no separate `config`/`packages` sections. The §7 extends-only
`config` section is migrated/dual-written during a soak, then dropped per §7's section-ownership
rules.

R8. **`.gitignore` auto-fill** (D14) leaves `.agentsrc.json`/`.agentsrc.lock` tracked and every
managed/generated output ignored; the block is idempotent.

### 15.5 Done criteria

1. A flat local-only project, after `da config sync`, has a `.agentsrc.lock` carrying `inputs_digest`
   + (empty-or-populated) `units` (R1).
2. `da config verify` reports local-scope drift (inputs_digest mismatch) and per-unit cache integrity
   for git/http/local/oci uniformly (R3).
3. No clock-driven staleness anywhere; digests only (R2).
4. `config explain` is the single effective-policy truth surface and `status` no longer reports
   config values/freshness (D12).
5. The lock has one `units` section (config/packages collapsed), `adapters` untouched (R7).
6. CRUD routes via `--scope`/`--source` with an editability check (R4).
7. `EnsureResolved` is convergent and honors `--locked`/`--frozen`/`--no-sync`/`--offline` (R5);
   `--locked` exits non-zero if the lock would change.
8. `.gitignore` auto-fill converges on re-run with `.agentsrc.{json,lock}` committed (R8).

### 15.6 Resolved & open questions

**Resolved — D-open-1: Governance interface shape (team/org editability).** Settled and
implemented as the `WriteAuthorizer` seam + `Checker` default in `internal/config/editability.go`
(task `c0-governance-interface`, PR #210; interface + nil-safe default only, 100% per-file
coverage). The three previously-open sub-questions are answered:

- **Where the policy plugs in** — `WriteAuthorizer.Authorize(p Principal, s WriteTarget) (Verdict,
  error)`, a policy-backend-agnostic seam (the `graph-backend-adapter-contract` pattern). The
  `Checker` calls a backend **only** for governed scopes; `local` and personal-project writes never
  reach a backend.
- **What it returns** — a `Verdict{Decision, Reason, Scope}` where `Decision ∈ {DecisionAllow,
  DecisionDeny, DecisionPrompt}`. A non-nil backend error is treated as a safe **fail-closed
  `DecisionPrompt`** (deny-then-confirm), never a silent allow.
- **How project editability is derived** (`Checker.CanWrite`): `local` → always allow (personal);
  `project` with no `Owner` → allow (personal project, derives to local); `project` with an `Owner`
  + `team`/`org` → governed, delegate to the backend; **no backend wired or backend error** →
  `DecisionPrompt` (safe default); unknown scope → `DecisionDeny`.

Still deferred (see §15.7): the concrete team/org policy backend implementation and its
registration/selection — the contract and nil-safe default are in place; no production backend is
wired yet.

**Open — inherited from §14 that this section touches:** Q3 config-layer signing (p5 ships a
warn-only verifier; ERROR-by-default deferred) and Q5 workspace-level lockfile (ruled either-or like
git; not implemented here).

### 15.7 Deferred / out of scope

- The governance **backend implementation** — the interface + nil-safe default are done (D-open-1
  resolved); a concrete team/org `WriteAuthorizer` and its registration/selection are deferred.
- The graph `adapters` lock section (owned by `graph-backend-adapter-contract`; this section only
  guarantees it is preserved as a peer section).
- OCI artifact transport/signing specifics (owned by `external-agent-sources`).
- v1 loading **removal** (deferred to 0.5.0+ per the 2-release soak; the opt-in `da config migrate`
  shipped in 0.4.1, but v1 manifests still load with a deprecation warning).
- **Doc + prompt-overlay surfacing decision (deferred):** whether shipped-vs-installed provenance and
  drift for starter-owned profiles/agents/skills is surfaced through `da config explain` or an
  adjacent `inspect` command (the §10 "Installed starter-seed upgrade boundary" addendum). Non-blocking
  for the units substrate; an owner-facing UX decision, not a substrate gap. Tracked here so it is not
  re-litigated as a §15 open question.

### 15.8 Implementation status note (context, not contract)

**SHIPPED in 0.4.0 / 0.4.1 (verified against master 2026-06-25).** The §7A model is no longer
"implemented but unwired" — the full §15 surface is wired, released, and has production callers:

- **Units lock + `inputs_digest`** (D3/D4/D7, R1/R7): one `units` section keyed by
  `source:path@version` with per-unit `kind`/`digest`/`last_checked_at`, plus a top-level
  `inputs_digest`; `adapters` preserved as a peer section. `internal/config/lock_units.go`,
  `internal/agentslock/lockfile.go`.
- **Content-hash staleness, TTL→nudge** (D4): `internal/config/staleness.go`,
  `internal/config/lockstatus.go`. No clock-driven invalidation anywhere.
- **`EnsureResolved` auto-sync seam** + `--locked`/`--frozen`/`--no-sync`/`--offline`:
  `internal/config/ensure_resolved.go`; production callers in `da config explain`
  (`commands/config/explain.go`, auto-locks), `da install`, and `da refresh`.
- **Editability seam** (D11/D-open-1): `WriteAuthorizer` + `Checker` in
  `internal/config/editability.go`; `--scope`/`--source` routing in
  `commands/internal/cmdutil/source_routing.go`.
- **Local-source auto-bootstrap** (D6): `internal/config/local_source.go`.
- **Project-local overlay** (D9): `internal/config/overlay.go` (`.agentsrc.local.json`, gitignored,
  hashed into `inputs_digest`).
- **Managed `.gitignore` auto-fill** (D14): `internal/links/gitignore.go`.
- **Exact/prune outputs projection** (D10, cj): `internal/platform/resource_plan.go`,
  `commands/refresh.go` (`--inexact` opt-out); `da install` reuses `RunSharedTargetProjectionExact`.
- **Command reshape** (D12): `da doctor` read-only/never-repairs and `da status` fleet/link-health
  only (`commands/internal/lifecycle/{doctor,status}.go`); `da config explain` is the single
  effective-config truth surface.
- **`extends`-accepts-oci** (D15, extends-oci-relax): `internal/config/fetcher_oci_layer.go` (the
  `application/vnd.dot-agents.config-layer.v1+json` media-type-guarded layer fetcher); `SelectFetcher`
  in `internal/config/fetcher.go` returns it for `oci` — the source/kind asymmetry is removed in code,
  satisfying the "doc flips once #110/#111 land" caveat in D15.
- **Unified artifact sourcing** (D8, ce): git/local artifact fetchers added
  (`internal/config/fetcher_git_artifact.go`, `internal/config/fetcher_local_artifact.go`); any source
  serves any kind.
- **`da config migrate`** (0.4.1, #138): opt-in v1→v2, folds legacy keys into
  `stage_profiles`/`execution_profile`, `.agentsrc.json.v1.bak` backup, idempotent.
  `commands/config/migrate.go`, `internal/config/migrate.go`.
- **`agentslock` interprocess lost-update protection** (p4h, #148 Windows-parent fix in 0.4.1):
  `internal/agentslock/lockfile.go`.

**Source-content-gap learning folded in (cc → cl).** `cache_keys` was parsed but **inert** when
first shipped (`config-v2-coherence/cc`): `EffectiveCacheKey`/`DefaultCacheKey` had zero non-test
callers, so setting `cache_keys` in `.agentsrc.json` was a silent no-op. The follow-up
`config-v2-coherence/cl-cache-keys-consume` wired the parsed primitive into the http/oci fetchers and
the resolver/staleness path (`internal/config/cache_keys.go` consumed by `fetcher_http.go`,
`fetcher_oci.go`, `resolver.go`, `staleness.go`). Likewise `da config lint` originally blanket-skipped
all non-local layers; the `lint-validate-locked-remote-layers` fast-follow (#127) made it validate
locked+cached remote layers at their recorded digest. **Lesson recorded here so future "spec says X is
configurable" claims are checked against a real consumer, not just a parser.**

**Net-new remaining (small):** none of the §15 substrate as it stood at 0.4.1. The coherence-fold
amendments (D1a, D13/A2, D3/A3) introduce **net-new resolver obligations** that are **NOT yet
implemented** — see §15.9.

### 15.9 Coherence-fold resolver obligations

The three amendments folded above are **contract** changes. This is the checklist the resolver-code
work is accountable to (the contract is §15; this itemizes the deltas it obligates).

**Implementation status (PR #193, `impl/15-fold-amendments`).** Items 1-5 (the D1a authority core +
security) and item 8 (the descriptor kind guard) are **IMPLEMENTED** here; items 6-7 (the
`project-set` unit) are **PARTIAL** (kind enum + binding-separation guards landed; the full
synced-unit resolver behavior is a tracked follow-on). The authority pass is **additive**: a layer
set that declares no `locks`/`authority_grants` resolves exactly as before, so no shipped
value-merge behavior changed. Code: `internal/config/authority.go`,
`internal/config/authority_apply.go`, `internal/config/unit_kinds.go`; wired into the shared
`resolveSnapshot` so both the flat and layered resolvers honor it; negative-control suite in
`internal/config/authority_test.go` + `authority_apply_test.go`.

**Security-audit hardening (round 2).** A cross-brain audit found the first cut tested only the
SAFE direction; these exploit-direction holes are now closed and covered by exploit-direction
negative controls: (A) grants require a **strictly-higher** granter (`g > c`) — a peer cannot
confer its own rank, and the grant table rejects same-rank/downgrade **overwrite** of an incumbent
grant; (B) a **lower** deny can no longer erase a **higher/peer** allow — deny application is now
provenance-aware (a member survives when its highest contributor outranks the deny owner); (D)
`value_locks` traverse the **dot field-path** (copy-on-write) so a lock on `features.flag` pins the
nested value, not a literal top-level key; and malformed `locks`/`authority_grants` now **fail
closed** (a resolve-time validation error), never a silent skip. Conferring org-level authority
onto an org source remains the deferred trusted-root/governance bootstrap (§15.7) — it does not
flow through the peer guard.

**Round-3 hardening.** (D) Lock decoding is **strict**: `extractLocks` uses `DisallowUnknownFields`,
so any unknown/typo'd key in a `locks` block (`deny_lock` missing the `s`, `force_alow`, …) is a
fail-closed "malformed/unknown lock field" error rather than a silently-ignored no-op policy — a
mistyped admin deny can never silently bind nothing. (C) Overlapping value-lock paths are
**ambiguous and rejected** fail-closed: if one effective lock path is a strict segment-wise prefix
of another (e.g. `features` and `features.graph_bridge`), the resolve aborts rather than applying
them in nondeterministic Go map order; disjoint sibling paths (`features.a`, `features.b`) are fine.
**Array-index path segments are unsupported in v1** — an all-digit segment (e.g. `skills.0`) in a
value_lock or deny_lock path is a validation error, not silently treated as a map key.

**Round-4 hardening (strict lock grammar).** The claim "a mistyped admin deny can never silently
bind nothing" now holds for the token grammar too, not just unknown keys. A value_lock path segment
and a deny_lock `category:member` token must be a **clean identifier** — letters, digits, `_`, `-`
(the alphabet real config keys use: app types like `go-cli`, feature-flag names, profile slugs) —
with **no whitespace, brackets, colons-in-token, dots-in-segment, or control characters**, and a
deny_lock must have **exactly one** `:` separating two non-empty valid tokens. Any token that fails
the grammar (`skills: risky`, `skills :risky`, `" model"`, `model `, `skills[0]`, `:risky`,
`skills:`, `skills:risky:extra`) is a **fail-closed resolve error** — never silently trimmed,
normalized, or no-op'd. So a mistyped lock aborts the resolve instead of binding nothing.

**From D1a (Amendment 1 — authority/value two-axis + source-authority registry):**
1. **[DONE] Policy-authority pass (Phase 1).** A resolve phase ahead of the existing value-merge
   applies the **AUTHORITY-RANK** total order (`org > team > repo > user`, deny-overrides, higher
   binds lower) and evaluates locks / value-locks. `runAuthorityPass` + `applyAuthority`.
2. **[DONE] Locked-field collision + provenance.** A lower-scope write a higher scope value-locked is
   rejected (lock wins) and surfaced as a `LockCollision` (attempted + winning + owner) through
   `da config explain` (`printLockCollisions`).
3. **[DONE] Cross-authority deny / no force-allow.** A lower deny cannot erase a higher allow
   (deny-locks bind only lower scopes); `force_allow` is a fatal validation error.
4. **[DONE] Source-authority registry + `authority_grants`.** Authority derives from
   `ref.source → registry → scope`; the `authority_grants` block is honored only when written by a
   scope whose authority is at least the conferred scope. Self-elevation by a scoped lower layer is a
   **fatal** resolve-time rejection; a value-only/public source's claim is **inert**. The bootstrap
   of the first org/team-scope registry is the trusted-root/governance-backend concern (deferred,
   §15.7) — so org/team authority does not yet enter via a built-in local layer, but the guards and
   the resolver path are in place and tested. `resolveAuthorityGrants` / `evaluateGrant`.
5. **[DONE] Schema fields.** Two explicitly-named orderings (`authority_rank`, `value_precedence`)
   in `ScopeOrdering`/`CanonicalScopeOrdering` (NOT one reused `scope_chain`, F1.1); manifest-side
   `locks` + `authority_grants` typed fields + `schemas/agentsrc.schema.json`.

**From D13 (Amendment 2 — registry split):**
6. **[PARTIAL] `kind: project-set` unit.** The `UnitKindProjectSet` kind + `IsSyncedUnitKind`/
   `IsProjectableKind` guards landed; the full synced identity-registry unit (lock entry +
   `inputs_digest` participation, `home-config`/team-manifest referencing under selector-merge) is a
   tracked follow-on.
7. **[PARTIAL] Binding-table separation.** The guard surface (`IsSyncedUnitKind` excludes the
   binding table) is in place; physically relocating the machine-local `id → absolute-path` table out
   of the registry is the same follow-on as item 6.

**From D3 (Amendment 3 — conditional descriptor):**
8. **[DONE — guard only, by design] CONDITIONAL.** Descriptors stay Go-internal through the
   `multi-harness` F4 probe: the `UnitKindDescriptor` kind is reserved and `ValidateUnitKind` /
   `IsProjectableKind` fail-closed on it (`descriptorsSourceShipped = false`), so the resolver/lock
   recognize but never mis-resolve a descriptor today. The full fourth behavior (media type, resolver
   order, validation, lock entry, `inputs_digest`) fires only if a descriptor becomes source-shipped;
   the F4 probe still gates the schema independently of this fold.
