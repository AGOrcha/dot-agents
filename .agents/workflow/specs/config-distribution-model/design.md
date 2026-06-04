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

### 13.2 New command subtree: `da packages`

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

#### D1 — Three orthogonal axes, not one overloaded directory
A resolvable unit is described by three independent axes:
- **Scope** (precedence; *merges* into effective policy): product → user-local → org → team →
  repo-imported → repo-local (committed) → **project-local overlay (uncommitted)** → runtime.
  Answers "who gets the last word." (Extends `org-config-resolution` §4 with one new scope — see D9.)
- **Source** (origin/transport; *resolves + versions* a unit): `git` / `http` / `local` / `oci`.
  Answers "where a unit comes from and how it is versioned." Orthogonal to scope.
- **Kind** (behavior): `layer` (mergeable policy) vs `artifact` (installable executable).

*Rejected:* keeping scope/source/kind fused in one directory with special-cased paths — the root
cause of the half-old/half-new feeling.

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

#### D13 — Scoped content routes to its scope's source
`proposals/`, `context/`, lessons, and asset units are **scoped** (user/repo/team/org) and route to
the source backing that scope (generalizing proposal-routing's global-vs-project split to all four
scopes). Only the fetch `cache/` and the managed-project registry are genuinely non-scoped local
operational state — never a scope, never a source, never projected.

#### D14 — Managed-resource `.gitignore` auto-fill in consuming projects
`da` owns a delimited, idempotent block in each consuming project's `.gitignore`: projected links,
generated platform configs, the `.agentsrc.local.json` overlay, and materialized asset units are
**ignored**; `.agentsrc.json` and `.agentsrc.lock` stay **committed** (the resolved-state contract,
like `uv.lock`). Re-runs converge (regenerated, not appended).

### 15.4 Requirements (behavioral)

R1. **A lockfile exists for every resolved project**, including flat/local-only, carrying
`lock_version`, `inputs_digest`, and a `units` map keyed by `source:path@version` →
`{kind, digest, fetched_at, last_checked_at}`. The `adapters` section is preserved untouched (owned
by `graph-backend-adapter-contract`).

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
- v1 deprecation + auto-migration (separate, soak-gated `config-v2-migration` tail).

### 15.8 Implementation status note (context, not contract)

The §7A lock model — `inputs_digest` computation, the `units` lock structure, and the
`EnsureResolved` auto-sync seam — is **already implemented and tested in-tree but unwired** (no
production callers). The near-term work is therefore predominantly *wiring + reader migration +
command reshape*, not greenfield. The editability **interface** (D11/D-open-1) is also already built
(`internal/config/editability.go`); what remains net-new is its **routing consumer**
(`--scope`/`--source` write path), the `local`-source auto-setup (D6), the project-local overlay
scope (D9), `.gitignore` auto-fill (D14), and the command reshape (D12). 
