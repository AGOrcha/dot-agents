---
title: Layered Configuration
description: A user-facing guide to dot-agents' layered configuration model — the .agentsrc.json manifest, extends layers, resolution and precedence, the .agentsrc.lock lockfile, and the da config command family.
sidebar:
  order: 1
---

# Layered Configuration

`dot-agents` resolves a project's effective configuration from a **stack of layers** rather
than a single file. A repo's `.agentsrc.json` manifest can `extends` shared layers — an org
baseline, a team overlay — that merge underneath the repo's own settings. The resolved stack is
pinned in a committed lockfile (`.agentsrc.lock`) so every machine and every agent projects the
*same* effective config. This is the same shape `uv`/`cargo`/`npm` use: a declared manifest, a
resolved lockfile, and commands to inspect, re-resolve, and validate the stack.

This guide is the canonical user-facing reference for that model. It covers the manifest, the
layering mechanism, how layers merge and which one wins, the lockfile, the `da config` command
family, and a full worked walkthrough. The authoritative design lives in the
[`config-distribution-model`](../.agents/workflow/specs/config-distribution-model/design.md) spec
(§15 is the current authority); cite this guide for day-to-day use.

> **Scope.** This guide is about the *config layer* — the structured fields in `.agentsrc.json`
> and how they resolve. The execution-profile layer (units / topology / lenses by `app_type`) has
> its own canonical reference in [Config Relevance](./CONFIG_RELEVANCE.md).

---

## Overview

A `dot-agents` project has three artifacts at the root:

| File | Role | Committed? |
|---|---|---|
| `.agentsrc.json` | The **manifest** — declared identity, layers (`extends`), and settings. | Yes |
| `.agentsrc.lock` | The **lockfile** — the resolved stack pinned by content digest. | Yes |
| `.agentsrc.local.json` | Optional machine-local overlay (highest precedence, never committed). | No (gitignored) |

The manifest is what you *declare*; the lockfile is what was *resolved*. You edit the manifest;
`dot-agents` writes the lockfile. Like `uv.lock`, both `.agentsrc.json` and `.agentsrc.lock` are
committed — they are the resolved-state contract that makes a checkout reproducible.

The lifecycle:

```
edit .agentsrc.json
  → da config sync          (re-fetch layers, re-resolve, rewrite .agentsrc.lock)
  → da config explain       (inspect the effective value of any field + its provenance)
  → da config lint          (validate each layer against the schema)
  → da config verify         (offline setup contract check)
  → da refresh / da install  (re-project the locked config into platform outputs)
```

---

## The Model

### The `.agentsrc.json` manifest

The manifest is a JSON object. The v2 (config-v2) surface adds these top-level fields on top of
the original v1 settings (all v2 fields are optional, so a v1 manifest still validates):

| Field | Type | Purpose |
|---|---|---|
| `repo_id` | string | Canonical repository identity, e.g. `"github.com/acme/manager-ui"`. A **protected** field: imported layers cannot override it. |
| `sources` | array | Named sources that layers and packages are fetched from. Each has an `id`. |
| `extends` | array | Config **layers** to merge underneath this repo's settings. |
| `packages` | array | Executable **artifacts** (agents/skills bundles) to install. |
| `features` | object | Feature-flag overrides (`features.*`). |
| `execution_profile` | object | Workflow execution shape by `app_type` (see [Config Relevance](./CONFIG_RELEVANCE.md)). |

A minimal v2 manifest:

```json
{
  "$schema": "https://dot-agents.dev/schemas/agentsrc.json",
  "repo_id": "github.com/acme/manager-ui",
  "sources": [
    { "id": "acme", "type": "git", "url": "git@github.com:acme/agents-config.git", "ref": "main" }
  ],
  "extends": [
    "acme:org/base.json"
  ],
  "kg": { "backend": "sqlite" }
}
```

### Sources

A **source** names *where* layers and packages come from. The `type` determines the transport:

| `type` | Transport | Valid for `extends`? | Valid for `packages`? |
|---|---|---|---|
| `local` | A path on disk | Yes | Yes |
| `git` | A git repository (`url` + `ref`) | Yes | Yes |
| `http` | An HTTP(S) endpoint | Yes | Yes |
| `oci` | An OCI registry (content-addressed) | **No** | Yes |

> **Only `extends` rejects `oci`** — oci cannot supply a config layer. Every other source×kind
> combination is valid: packages/artifacts may be fetched from git, local, http, or oci. Declaring
> an `oci` source in `extends` is rejected at resolution.

Each source carries an `id` (required for v2 refs), an optional `cache_ttl` (a *review nudge*, not a
hard expiry — see [Caching](#caching-and-staleness)), an optional opaque `auth` block, and an
optional `cache_keys` override.

### Layering via `extends`

Each `extends` entry references a layer inside a declared source. The reference syntax is:

```
source-id:layer-path[@version]
```

For example `acme:org/base.json` resolves `org/base.json` from the source whose `id` is `acme`,
at the source's default ref; `acme:org/base.json@v2` pins a specific version. An entry may also be
the object form to mark it non-fatal if the fetch fails:

```json
{ "extends": [ { "ref": "acme:org/base.json", "optional": true } ] }
```

**`kind` — layer vs artifact.** Under the hood every resolved unit (whether it came from `extends`
or `packages`) carries a `kind` that governs how it is treated:

- **`layer`** — a JSON field-bundle that **merges** into effective config by the category rules
  below. Protected-field rules apply. A layer may itself *declare* further units (transitive
  `extends`).
- **`artifact`** — a bundle **installed discretely** into the asset store and invoked. Trust/
  signing rules apply. It does **not** merge into config.

`extends` entries resolve to `layer` units; `packages` entries resolve to `artifact` units.

### Resolution and precedence

Layers merge into an **effective configuration**. The stack, lowest precedence first:

```
[1] product-defaults   (built-in defaults)
[2] user-local         (~/.agents/.agentsrc.json, if present)
[.] imported extends   (your declared layers, reconstructed from the lock at their locked digest)
[n] repo-local         (./.agentsrc.json)
```

**Higher in the stack wins.** Your repo-local `.agentsrc.json` sits at the top, so it overrides
anything an imported layer set; imported layers override user-local and product defaults. (When
present, the uncommitted `.agentsrc.local.json` overlay sits above repo-local.)

Merge is **per-field by category**, not a blunt whole-object replace:

| Category | Behavior | Applies to |
|---|---|---|
| **scalar** | Last (highest) layer with a value wins. | most scalar fields |
| **set-union** | Arrays merged as a deduped set. | `skills`, `agents`, `rules` |
| **map-merge** | Object maps merged by key, recursing into nested maps. | `features`, `kg`, `stage_profiles`, `execution_profile` |
| **ordered-replace** | Array taken wholesale from the winning layer (order matters). | `sources`, `extends`, `packages`, verifier sequences |

So adding a skill in your repo-local manifest *adds* to the layered set (set-union), while changing
`kg.backend` *overrides* just that key (map-merge into a scalar leaf), and replacing `sources`
swaps the whole list (ordered-replace).

`repo_id` is **protected**: an imported layer can never set or change it.

### Seeing provenance

`da config explain <field>` shows the effective value and the **full layer stack** that produced
it — which layers had a value, and which one won. The machine-readable shape (`--json`) is:

```json
{
  "field": "kg.backend",
  "value": "sqlite",
  "active_layer": "repo-local",
  "layers": [
    { "layer": "product-defaults", "value": "sqlite", "active": false },
    { "layer": "acme:org/base.json", "value": "postgres", "active": false },
    { "layer": "repo-local", "value": "sqlite", "active": true }
  ]
}
```

`active_layer` is the winning layer; exactly one entry in `layers[]` has `"active": true`.
`--origin-only` prints just the winning layer identifier; `--value-only` prints just the effective
value (JSON-encoded for non-scalars).

### The lockfile (`.agentsrc.lock`)

The lockfile is the resolved-state contract. It exists for **every** resolved project — including
flat, local-only projects with no remote `extends`. It carries:

- `lock_version` — the lockfile schema version.
- `inputs_digest` — a hash of all local config scopes. This is how local-scope drift is detected.
- `units` — a map keyed by `source:path@version`, each value pinning the resolved unit:

```json
{
  "lock_version": 1,
  "inputs_digest": "sha256:9f2c…",
  "units": {
    "acme:org/base.json@main": {
      "kind": "layer",
      "digest": "sha256:1a4b…",
      "fetched_at": "2026-06-22T18:04:11Z",
      "last_checked_at": "2026-06-22T18:04:11Z",
      "cache_key": "git:7c9e…"
    }
  }
}
```

Per unit, the lock pins the resolved **content `digest`** (a `sha256:…`), the `fetched_at`
timestamp, a `last_checked_at` (used only to power review nudges — it never auto-invalidates), and
the effective `cache_key`. Sibling sections owned by other subsystems (e.g. `adapters`) are
preserved untouched on rewrite.

**There is one unified `units` section** — not separate `config` and `packages` sections. Both
layers and artifacts live in `units`, distinguished by `kind`.

**When the lock is written vs read.** The lock is rewritten when the project is **stale** (see
below) and read-only when it is fresh:

- `da config sync` always re-fetches upstream and rewrites the lock.
- `da config explain` **auto-locks**: if the lock is stale or absent it re-resolves and rewrites
  before rendering; if it is already fresh it reads the lock back read-only (no network, no write).
- `da refresh` and `da install` re-project the locked config into platform outputs and only
  re-resolve when the lock is stale — so routine relinking never reaches the network for an
  unchanged stack.

### Caching and staleness

**Staleness never consults a clock.** A project is **fresh** if and only if all three hold:

1. the freshly-computed `inputs_digest` matches the lock's recorded value (local scopes unchanged),
2. the declared `extends`/`packages` ref set is unchanged, and
3. every recorded per-unit `digest` matches.

If any differs, the project is stale and the next resolving command re-resolves. `cache_ttl` and
`last_checked_at` are **review nudges** only — `doctor`/`explain` may *suggest* you run
`da config sync`, but they never silently re-fetch or invalidate the lock on a timer.

**`cache_keys`** control what content pins a unit's cache key, with per-`type` defaults that follow
uv's model:

| Source `type` | Default cache key |
|---|---|
| `git` | the resolved commit (immutable) |
| `local` | commit + working-tree content (uncommitted edits are a distinct key) |
| `http` | upstream ETag/Last-Modified, falling back to a content digest |
| `oci` | the manifest digest (content-addressed) |

A source may override its cache key via `cache_keys` (tracked file/glob paths, git commit/tags,
environment variables, directory-presence markers) and can force re-validation with an
always-revalidate marker. At runtime, `--refresh` forces a single unit to re-check upstream.

---

## The `da config` Commands

```
da config explain [field]   Show the effective value of a field and where it came from
da config sync              Re-fetch layers and rewrite the lock (explicit upstream re-check)
da config lint              Validate declared layer files against the schema
da config verify            Run offline repo setup contract checks (no layer re-fetch)
da config relevance         Resolve a task's execution profile by app_type
da config migrate           Rewrite a legacy v1 .agentsrc.json into the v2 schema (opt-in)
```

All accept the global `--json` flag for machine-readable output. **All config command errors
exit with code `1`** — there is no differentiated exit code per failure class today.

### `da config explain`

The single **effective-config truth surface**. It prints the effective value of one field plus the
full layer stack that produced it. With no field it is interactive over the config; `--all` prints
the entire effective configuration annotated with provenance; `--flags` prints feature-flag
resolution across layers.

`explain` **auto-locks** (like `uv tree`): it consumes the committed lock, and when that lock is
stale or absent it re-resolves and rewrites it before rendering; when the lock is already fresh it
reads it back read-only. (`da status` no longer inspects config — config introspection moved here.)

```
da config explain kg.backend
da config explain kg.backend --origin-only      # just the winning layer
da config explain skills --value-only            # just the effective value
da config explain --all --json
da config explain --flags
```

### `da config sync`

The explicit **upstream re-check** — the `uv --upgrade` analog. It re-fetches every declared layer
**regardless of TTL**, re-runs resolution, and rewrites the `units` section + `inputs_digest` of
`.agentsrc.lock`. It is intentionally distinct from `da refresh`, which only re-projects local
outputs and re-resolves only when the lock is already stale.

- `--layer source-id:path` scopes the *report* to one declared layer (the full stack is still
  re-resolved so the lock stays internally consistent).
- The global `--dry-run` flag (added in #105) **previews** what sync would re-fetch and which lock
  entries it would rewrite, then exits **without touching** `.agentsrc.lock`.

```
da config sync
da config sync --layer acme:org/base.json
da config sync --dry-run        # preview, no write
da config sync --json
```

### `da config lint`

Validates the repo-local `.agentsrc.json` and each declared `extends` layer against the canonical
AgentsRC layer schema (`schemas/agentsrc.schema.json`). For each layer it reports pass/fail with the
structured validation error on failure. Local-source layers are read straight from disk; remote
layers that aren't locally readable without a fetch are **skipped** (run `da config sync` first,
then re-run lint). It exits non-zero if any layer is invalid; skipped layers do not fail it.

### `da config verify`

An **offline** setup contract check (no layer re-fetch). It confirms:

- **manifest** — `.agentsrc.json` is present and parses;
- **config-layers** — each declared source resolves offline (local source paths exist);
- **locked-layers** — each `extends` layer is pinned in the lock, and for remote (git/http/oci)
  sources its downloaded assets are present in the cache at the locked digest — confirming remote
  layers offline without re-fetching;
- **binary** — optional integrations (e.g. code-review-graph) are ready.

It exits non-zero if any check fails; warnings (absent optional integration, or a remote layer that
can't be confirmed offline) do not fail it. It is narrower than `da doctor`, which audits the full
platform link projection (and is **read-only** — it reports problems and the fix command, never
repairs).

### `da config relevance`

Resolves a task's **execution profile** (units, topology, lenses) by `app_type`. This is a separate
layer with its own canonical reference — see [Config Relevance](./CONFIG_RELEVANCE.md).

### `da config migrate`

An **explicit, opt-in** rewrite of a legacy v1 `.agentsrc.json` into the equivalent v2 manifest. It
is a convenience for moving a repo to v2 on your own schedule — it is **not** a forced cutover.

- **Detection** — a manifest is migratable when it declares an old schema version, or when it
  carries the deprecated keys `verifier_profiles` / `reviewer_profiles` / `app_type_verifier_map`
  (which the loader already folds silently into the unified `stage_profiles` / `execution_profile`
  model on every read).
- **Backup-on-write** — the **original** manifest is copied to `.agentsrc.json.v1.bak` *before* the
  v2 file is written, so the pre-migration manifest is always recoverable.
- **Equivalent rewrite** — the schema `version` is bumped to `2` and the legacy keys are folded
  away (the same fold the loader performs, so nothing is lost). The legacy keys are not re-emitted.
- **Idempotent** — running it on a clean v2 manifest is a no-op with a clear message; no backup is
  written.
- **`--dry-run`** — previews the rewrite (and the planned backup path) and exits **without writing**
  anything.

It operates on the **current repository's** `.agentsrc.json`, so it is repo-agnostic. During the
deprecation soak window maintainers run it **per-repo** (including `payout`) to migrate each repo
when convenient.

> **Soak, not removal.** This command exists during a **two-release deprecation soak**. v1 manifests
> continue to load unchanged and the existing v1 deprecation warning still fires — `da config migrate`
> only lets you opt a repo into v2 early. v1 loading is **not** removed by this change.

```
da config migrate              # rewrite this repo's manifest to v2 (backs up to .v1.bak)
da config migrate --dry-run    # preview the rewrite, write nothing
da config migrate --json       # machine-readable report
```

---

## Worked Example

Start from a flat project and add a layer that overrides a field, then resolve, inspect, edit, and
validate.

### 1. Start a project

```console
$ cat .agentsrc.json
{
  "$schema": "https://dot-agents.dev/schemas/agentsrc.json",
  "repo_id": "github.com/acme/manager-ui",
  "kg": { "backend": "sqlite" }
}
```

`da config explain kg.backend` shows it resolving from `repo-local` over the built-in default:

```console
$ da config explain kg.backend
kg.backend = "sqlite"
  winning layer: repo-local
  [1] product-defaults   "sqlite"
  [n] repo-local          "sqlite"   ← active
```

### 2. Add a local `extends` layer that overrides a field

Create a layer that sets a different `kg.backend`, and a `local` source pointing at it:

```console
$ cat layers/base.json
{ "kg": { "backend": "postgres" }, "skills": ["release-docs-refresh"] }
```

Declare the source and extend it:

```json
{
  "$schema": "https://dot-agents.dev/schemas/agentsrc.json",
  "repo_id": "github.com/acme/manager-ui",
  "sources": [
    { "id": "team", "type": "local", "path": "layers" }
  ],
  "extends": [ "team:base.json" ],
  "kg": { "backend": "sqlite" }
}
```

### 3. Resolve and lock

```console
$ da config sync
resolved 1 layer
  team:base.json  →  sha256:1a4b…  (kind: layer)
wrote .agentsrc.lock (units: 1, inputs_digest updated)
```

The lockfile now pins the resolved layer:

```console
$ cat .agentsrc.lock
{
  "lock_version": 1,
  "inputs_digest": "sha256:9f2c…",
  "units": {
    "team:base.json@-": {
      "kind": "layer",
      "digest": "sha256:1a4b…",
      "fetched_at": "2026-06-22T18:04:11Z",
      "last_checked_at": "2026-06-22T18:04:11Z",
      "cache_key": "local:7c9e…"
    }
  }
}
```

### 4. Inspect the override and the winning layer

The layer set `kg.backend: postgres`, but repo-local still says `sqlite` and **repo-local wins**:

```console
$ da config explain kg.backend
kg.backend = "sqlite"
  winning layer: repo-local
  [1] product-defaults   "sqlite"
  [2] team:base.json      "postgres"
  [n] repo-local          "sqlite"    ← active
```

The layer's `skills` entry *did* take effect (set-union — repo-local declared none, so the layer's
value flows through):

```console
$ da config explain skills --value-only
["release-docs-refresh"]
```

Confirm provenance machine-readably:

```console
$ da config explain skills --origin-only
team:base.json
```

### 5. Edit the layer and re-sync to see the lock update

Change the layer's content:

```console
$ cat layers/base.json
{ "kg": { "backend": "postgres" }, "skills": ["release-docs-refresh", "verify"] }
```

A `--dry-run` previews the change without writing:

```console
$ da config sync --dry-run
would re-fetch:
  team:base.json  digest sha256:1a4b… → sha256:3d8f…  (changed)
would rewrite .agentsrc.lock  (inputs_digest, units[team:base.json].digest)
(dry-run: no files written)
```

Then commit it for real:

```console
$ da config sync
resolved 1 layer
  team:base.json  →  sha256:3d8f…  (kind: layer, digest changed)
wrote .agentsrc.lock
```

The unit's recorded `digest` is now `sha256:3d8f…`. Because staleness is content-driven, the next
`da config explain` sees the new digest matches the lock and reads it back without re-fetching.

### 6. Validate with lint

```console
$ da config lint
PASS  ./.agentsrc.json
PASS  team:base.json
all layers valid
```

Introduce an invalid layer and lint catches it (exit 1):

```console
$ da config lint
PASS  ./.agentsrc.json
FAIL  team:base.json  →  kg.backend: must be one of ["sqlite","postgres"] (got "mysql")
1 layer invalid
$ echo $?
1
```

---

## Reference

### Field surface (v2 additive)

| Field | Type | Notes |
|---|---|---|
| `repo_id` | string | Protected; imported layers cannot override. |
| `sources` | array of `{ id, type, url?, path?, ref?, cache_ttl?, auth?, cache_keys? }` | `type` ∈ `local`/`git`/`http`/`oci`. |
| `extends` | array of `string` or `{ ref, optional? }` | Ref form `source-id:path[@version]`; resolves to `layer` units; rejects `oci`. |
| `packages` | array of `string` | Ref form `source-id:path@version`; resolves to `artifact` units; valid from any source (git/local/http/oci). |
| `features` | object (map-merge) | Feature-flag overrides. |
| `execution_profile` | object (map-merge) | Execution shape by `app_type` — see [Config Relevance](./CONFIG_RELEVANCE.md). |

### Layer stack (lowest → highest precedence)

1. `product-defaults`
2. `user-local` (`~/.agents/.agentsrc.json`)
3. imported `extends` (at locked digest)
4. `repo-local` (`./.agentsrc.json`)
5. `.agentsrc.local.json` (uncommitted overlay, if present)

### Merge categories

`scalar` (last-wins) · `set-union` (`skills`/`agents`/`rules`) · `map-merge` (`features`/`kg`/
`stage_profiles`/`execution_profile`) · `ordered-replace` (`sources`/`extends`/`packages`).

### Lockfile keys

`lock_version` · `inputs_digest` · `units{ <source:path@version>: { kind, digest, fetched_at,
last_checked_at?, cache_key? } }`. Sibling sections (e.g. `adapters`) preserved on rewrite.

### Command quick reference

| Command | Reads/writes lock | Network | Exit on error |
|---|---|---|---|
| `config explain` | auto-locks (write if stale, else read-only) | only if stale | 1 |
| `config sync` | always rewrites | always re-fetches | 1 |
| `config lint` | read-only | none (skips un-fetched remotes) | 1 |
| `config verify` | read-only | none (offline) | 1 |
| `config relevance` | read-only | none | 1 |
| `refresh` / `install` | re-resolves only if stale | only if stale | 1 |

### See also

- [Config Relevance](./CONFIG_RELEVANCE.md) — the execution-profile layer (units / topology / lenses).
- [`config-distribution-model`](../.agents/workflow/specs/config-distribution-model/design.md) — the authoritative spec (§15 is current).
