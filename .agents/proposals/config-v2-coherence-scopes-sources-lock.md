# Proposal: Config-v2 Coherence — Scopes, Sources, the Asset Store, and the Lock

Status: draft for review (design analysis; not yet a canonical spec upgrade or plan)
Created: 2026-05-30
Context: emerged from designing a uv-style auto-syncing lockfile (`.agentsrc.lock`).
The auto-sync work surfaced a deeper incoherence that must be resolved first, so that
auto-sync is built on a coherent scope/source/lock model rather than bolted onto the
current overloaded one. Design philosophy: **agent DX is the primary operator** — humans
are a secondary, cross-cutting audience.

---

## 1. Problem

Adding uv-style "detect staleness → auto re-resolve → auto-sync outputs" forced four
questions the current model answers incoherently:

1. **Config CRUD** — `da skills/agents/hooks/rules/mcp/settings` author files directly into
   `~/.agents/` (the "old" model), while the lock/extends/packages machinery is the "new"
   model. The same logical slot (a skill) is half-authored-file, half-resolvable-unit.
2. **The lock** — what does `.agentsrc.lock` actually track, and what makes it stale?
3. **Scopes** — the precedence layers (product/user/org/team/repo/runtime) are not cleanly
   separated from *where* config comes from or *what kind* of thing it is.
4. **UX** — overlapping inspection commands (`status` / `doctor` / `config explain`) with
   blurred intents; a verb collision (`sync` vs `config sync`).

## 2. Root cause — `~/.agents/` is overloaded with four fused roles

`AgentsHome()` (one path, ~40 call sites) simultaneously serves:

| Role | Paths | What it actually is |
|---|---|---|
| User-local config **layer** | `.agentsrc.json` | a **scope** (precedence #2, org-config-resolution §4.2) |
| Canonical **asset store** | `skills/ agents/ hooks/ rules/ mcp/ settings/` | **source material**, projected into repos by `refresh` |
| Scoped **content** | `proposals/`, `context/`, lessons, and the asset units themselves | **scoped** — belongs to a scope (user/repo/team/org) and lives in that scope's source; not flat local state |
| Local operational **state** | `config.json` (managed-project registry), backup + promote journals | genuinely machine-local — never a scope, never projected |
| Fetch **cache** | `cache/config/<src>/<sha>/` | **cache** of fetched units |

…and `da sync` runs git over the whole directory. The directory fuses three orthogonal
axes — **scope** (precedence), **source** (origin/transport), and **kind** (policy vs
artifact) — plus genuinely local machine state. That fusion is the root cause of the
"half old, half new" feeling in the CRUD commands.

## 3. The coherent model — three orthogonal axes + local state

The key correction (from review): **versioning/sourcing is unified, not tier-specific.**
Everything resolvable — a config layer *or* an artifact like a skill — is addressed by the
reference syntax that already exists in the spec (config §5):

```
source-id : layer-or-artifact-path @ version-spec
```

So the model is three independent axes, not one overloaded directory:

### Axis A — Scope (precedence; merges into effective policy)
product → user-local → org → team → repo-imported → repo-local (committed) →
**project-local overlay (uncommitted)** → runtime (extends org-config-resolution §4 with one new
scope). Scopes **merge** by category rules. This axis answers "who gets the last word."

The **project-local overlay** is the user's personal, machine-local settings *for one specific
project* — not committed to the repo, not the global user config. It is the `git`-`.git/config`
analog (local > user). Feasible and low-cost: a gitignored `.agentsrc.local.json` in the repo
(discoverable; lean) or a machine-keyed `~/.agents/projects/<repo-id>/overlay.json` (keeps the
repo tree clean). It sits just above repo-local committed (a personal take wins over the shared
default) and below runtime, and is a **local input** → hashed into `inputs_digest`.

### Axis B — Source (origin/transport; resolves + versions a unit)
`git` / `http` / `local` / `oci` (config §4). A source is **where** a unit comes from and
**how it is versioned**, orthogonal to which scope it lands in:

- `git` — ref = tag/branch/SHA
- `http` — layer file or OCI-over-HTTP
- `oci` — tag / semver / digest
- `local` — `~/.agents/`, **git-backed and auto-bootstrapped** so its units version by git
  ref exactly like a remote `git` source

This is the unification: **a skill authored locally and a skill pulled from a registry are
the same kind of resolvable unit differing only by source.** The `local` source is not a
special "old" path — it is a first-class source whose backing repo we set up automatically.

### Axis C — Kind (mergeable layer vs installable artifact)
With sourcing/versioning/lock unified (Axis B), the old **Tier-1 `config` vs Tier-2 `packages`**
split largely **collapses**. They were two parallel subsystems — different source constraints,
different lock sections, different caches, different command trees. None of that needs to differ
anymore. What remains is a single model of **resolvable units** (`source:path@version`) carrying a
`kind` that drives just two genuine behaviors:

- **layer** (mergeable policy): a JSON field-bundle that **merges** into effective config by
  category rules; protected-field rules apply; it may **declare** further units (more layers, and
  artifacts).
- **artifact** (installable executable: skill/agent/verifier/hook): a bundle **installed
  discretely** into the asset store and invoked; trust/signing rules apply; it does not merge.

So "config vs packages" reduces to **merge-vs-install + trust** — a per-unit attribute, not two
worlds. The two-pass resolution survives only as a **dependency order**, not a tier wall: resolve
mergeable layers to a fixpoint first (they declare what else to fetch), then resolve/install the
full declared artifact set. Consequences:

- **One lock model**: a single `units` map keyed by `source:path@version` →
  `{kind, digest, fetched_at, last_checked_at}`, replacing the separate `config`/`packages`
  sections. `adapters` stays its own section (genuinely different owner — the graph lifecycle).
- **One source model**: any source serves any kind (Axis B); the §4 tier-constraint table relaxes
  so `git`/`local` are valid for artifacts, OCI is just one artifact source.
- **One CRUD surface**: `da <unit-type> …` with a `--scope`/`--source` flag (§7), not parallel
  `config`/`packages` command trees.

### Scoped content vs local operational state
Most of what looks like "machine-local state" is actually **scoped content** that belongs to a
scope and lives in **that scope's source**, not flat in `~/.agents`:

- `proposals/`, `context/`, lessons, and the asset units (skills/agents/…) are **scoped** —
  user/local, project/repo, team, or org — and route to the source backing that scope. This
  already exists in embryo as proposal-routing's global (`~/.agents`) vs project-local
  (`.agents/`) split; generalize it to all four scopes. A repo-scoped proposal lives in the repo
  source (`.agents/`); a user-scoped one in the `local` source (`~/.agents`); team/org ones in
  their respective sources.
- Only the fetch `cache/` and the managed-project **registry** (`config.json`) are genuinely
  non-scoped local operational state — never a scope, never a source, never projected.

## 4. The `local` source auto-setup

For local-authored assets to resolve through the same `source:path@version` path as remote
ones, the `local` source must be bootstrapped so it is version-resolvable:

- `~/.agents/` is already a git repo (`da sync` manages it) → the `local` source is git-backed.
- Auto-setup: on first resolve (or `init`), ensure the `local` source repo is initialized and
  resolvable, so a local skill/agent/layer carries a real version (the local repo's ref) and
  the resolver/lock treat it uniformly.
- Consequence: `da skills new` (etc.) author into the `local` source working tree; the unit's
  version is the local repo ref; the lock records `local:<path>@<ref> → digest` just like any
  other source.
- **One asset dir, provenance-gitignored**: local-authored and remote-materialized units share
  one asset dir, but remote/sourced units are **excluded from the `local` source's git tracking**
  (gitignored), so `da sync` push never commits fetched assets into the user's personal repo.
  Provenance = the unit's source in the lock; git-tracked ⇔ local-authored.

This removes the "half old/half new" split: CRUD is just authoring into the `local` source.

## 5. The lock under the new model (`.agentsrc.lock`)

Staleness moves from **clock (TTL)** to **content-hash driver events** — converging three
independent sources of truth: the config spec's own table (§61: TTL "policy can drift" vs
content-addressed immutable), KG spec.2 §0 ("domain knowledge does not decay on a clock;
staleness = driver events; time = review-nudge"), and uv (no manifest hash, never
auto-upgrades on a timer).

The lock tracks, per resolved unit, uniformly across sources:

```jsonc
{
  "lock_version": 1,
  "inputs_digest": "sha256:…",   // hash of LOCAL scopes (user-local + repo-local manifests)
  "units": {
    "<source>:<path>@<resolved-version>": {
      "kind": "layer",            // or "artifact"
      "digest": "sha256:…",
      "fetched_at": "…",
      "last_checked_at": "…"      // nudge basis only — never auto-invalidates
    }
  },
  "adapters": { /* owned by graph-backend-adapter-contract §10.1, separate writer */ }
}
```

- **Staleness = driver event, by hash**: stale ⇔ `inputs_digest` mismatch (a local manifest
  changed) OR the declared set of `extends`/`packages` refs changed OR a recorded digest no
  longer matches. Cheap, local, no network, no clock. Minimal git churn — the committed lock
  only changes when an input actually changed.
- **TTL → review-nudge** (chosen): `cache_ttl` no longer auto-invalidates or auto-fetches.
  `last_checked_at` powers a `doctor`/`config explain` nudge ("layer X last re-checked 6d ago
  — `da config sync`"). It is a separate axis from truth.
- **Upstream re-check is explicit-only**: when local inputs are unchanged but an upstream ref
  may have moved, only `da config sync` / `--refresh` re-checks. Auto-sync never chases
  upstream on its own (uv "never auto-upgrade").
- Writer unchanged in spirit: shared schema-agnostic `internal/agentslock` writer, atomic
  flush, sibling-section-preserving (config §7.4). Adds `inputs_digest` (top-level), collapses
  `config`+`packages` into one `units` section keyed by `source:path@version` with a `kind`, and
  swaps per-unit `resolved_sha`+`ttl_expires_at` for `digest`+`last_checked_at`.

### 5.1 Per-source staleness keys (configurable, uv `cache-keys` model)

Default staleness per source kind mirrors uv's automatic, per-kind cache keys:

- `git` → resolved commit (a branch/tag ref resolves to a commit; the commit is the key)
- `local` → **git-backed** (the `local` source is the user's git-synced `~/.agents` repo) → keyed
  by resolved commit like `git`, **plus working-tree content** for in-progress authoring not yet
  committed (so `da skills new` before a commit still registers as a driver event)
- `http` → HTTP validators (ETag / Last-Modified) when present, else content digest
- `oci` → digest

A source may **override** its key set, exactly like uv's `tool.uv.cache-keys`, to declare what
drives re-check for its units:

| Key | uv form | Meaning here |
|---|---|---|
| file / glob | `{ file = "**/*.json" }` | re-check when matching files change |
| git commit / tags | `{ git = { commit = true, tags = true } }` | re-check when the ref's commit (or tags) move |
| env | `{ env = "VAR" }` | re-check when an env var changes |
| dir presence | `{ dir = "skills" }` | re-check when a dir is added/removed |

Two force escapes mirror uv: a per-unit **`--refresh`** (revalidate one unit/source now — the
explicit upstream re-check) and a config-declared **always-revalidate** marker (uv's
`reinstall-package`) for a unit whose metadata is dynamic and must not be trusted from cache.
The **cache-keys decide what *content* staleness means**; `last_checked_at` (recorded **per
source**) is the orthogonal *time-since* axis that powers the review-nudge.

## 6. Auto-sync behavior (the uv-style half)

A single seam — `EnsureResolved(project, opts)` — that config-consuming commands call:

1. Compute current `inputs_digest` + declared-set (cheap, local).
2. Compare to lock. Fresh ⇒ no-op (no network, no write).
3. Stale (driver event) ⇒ re-resolve local scopes (+ cached layers), rewrite lock.
4. Apply the **sync (outputs) half**: project the asset-store union → repos, **exact/prune by
   default** (delete managed outputs no longer in the resolved set; `--inexact` opts out).

Flags (uv-proven, three-way):
- `--locked` — assert; if the lock would change, **error (non-zero exit)**, write nothing. CI gate.
- `--frozen` — use lock as-is; skip the staleness check entirely.
- `--no-sync` — skip the outputs apply step (lock may still update unless also `--frozen`).
- `--offline` — resolve from lock/cache only, never touch network (already exists).
- `--refresh` / `da config sync` — the explicit upstream re-check (the only non-auto fetch).

**Scope of auto-sync = lock + outputs (full parity)** for mutating/setup commands. Read-feeling
commands are reshaped below rather than given a blanket rule.

### 6.1 Managed-resource `.gitignore` auto-fill (commands are the source of truth)

Projecting outputs into a consuming project also **maintains that project's `.gitignore`** so every
managed resource is ignored — the projection-side mirror of gitignoring sourced units from the
`local` source (§4). `da` owns a delimited, idempotent block in the project `.gitignore`:

- **Ignored** (generated / machine-local): projected links, generated platform configs, the
  `.agentsrc.local.json` project-local overlay (§3, Axis A), and any materialized asset units.
- **Committed** (authoritative declarations): `.agentsrc.json` and `.agentsrc.lock` stay tracked —
  they are the resolved-state contract, like `uv.lock`.

This guarantees the **source of truth is the lock + commands, never leaked git state**: managed
artifacts cannot accidentally be committed into a consuming repo and drift from what the resolver
would produce. Re-runs converge (the block is regenerated, not appended).

## 7. Command surface re-evaluation (agent-DX-first)

Each command gets one crisp intent against the model. Decisions taken in review are marked ✅.

| Command | Intent (post-model) | Lock interaction | Change |
|---|---|---|---|
| `config explain` | **the** effective-policy truth surface: values, provenance, lock freshness, flags | **auto-locks** (writes to stay current, like `uv tree`); does NOT project | reshape: ✅ **absorbs status's config inspection fully** |
| `status` | managed projects + projection/link health (the fleet view) | read-only | ✅ drop config-value/freshness reporting (→ `config explain`) |
| `doctor` | health + pending driver events + TTL review-nudges; repair *guidance* | read-only; never repairs | single staleness/nudge surface |
| `refresh` | project the asset-store union → repos (the "sync outputs" half) | ensure lock fresh, then project | gains pre-resolve; stays distinct from `config sync` (spec §13.3) |
| `install` | full setup = resolve + lock + project + setup-contract | full sync (≈ `uv sync`) | the canonical "make everything current" entry |
| `config sync` | explicit **upstream** config re-check (uv `--upgrade`) | resolve + write, ignores freshness | new (p4c) |
| `sync` (git) | git-manage a source repo; **`--source <id>`** selects which (default `local`) | n/a (git) | ✅ flag-routed; `config sync` collision is nominal (different op; `config` namespace disambiguates) |
| `skills/agents/hooks/rules/mcp/settings` | CRUD a unit into a target source; **`--scope`/`--source`** routes + **editability check** | each op is a driver event | ✅ flag-routed + permission check (subsumes the package-shadow guard) |
| `import` | author repo/global config INTO the `local` source (reverse projection) | driver event | clarify as local-source authoring, lock-aware |
| `add` / `remove` | (un)register a project in the local registry | resolve + initial lock on add | unchanged intent |

## 8. Open decisions

- **Governance interface (team/org editability).** `local` = always writable (personal); team/org
  = governed by **their** external policies, which are highly variable — so we need a
  **policy-backend-agnostic interface** ("can principal P write source S?") with pluggable
  implementations (the graph-backend-adapter-contract pattern), not a hardcoded mechanism. Project
  editability derives: team/org-owned → that governance; else personal → local-writable. Interface
  shape (where the policy plugs in, what it returns) is TBD.
_(Overlay storage and per-source cache-key defaults are now resolved — see §9.)_

## 9. Locked decisions (this thread)

- Staleness = content-hash driver events; **TTL → review-nudge** (never auto-invalidates).
- Auto-sync scope = **lock + outputs** (full parity) for mutating/setup commands.
- `config explain` **auto-locks** (writes lock) but does not project; **absorbs status's
  config inspection**; `status` becomes fleet/link-health only.
- `--frozen` / `--no-sync` are **opt-in** escape hatches; `--locked` = CI assertion (non-zero exit).
- Upstream re-check is **explicit-only** (`config sync` / `--refresh`); never on a clock.
- Versioning/sourcing is **unified** via `source:path@version`; the `local` source is
  git-backed and **auto-bootstrapped** so local assets resolve like remote ones.
- **`config`/`packages` tiers collapse** into one `units` model with a `kind` (layer|artifact):
  one lock `units` section, one source model, one CRUD surface. `adapters` stays separate.
- **Artifact sourcing relaxed**: `git`/`local`/`http`/`oci` are all valid for artifacts; the §4
  tier-constraint table is updated (OCI is one artifact source, not the definition).
- **No per-store command trees**: a `--scope`/`--source` flag routes CRUD/sync to a source and an
  **editability check** governs writes (replaces the `sync` rename and the package-shadow guard).
- **Scoped content routes to its scope's source** (proposals/context/lessons/assets across
  user/repo/team/org); only the cache + managed-project registry are non-scoped local state.
- **One asset dir; sourced units gitignored** from the `local` source so `da sync` never tracks
  fetched assets (provenance = the unit's source in the lock; git-tracked ⇔ local-authored).
- **Editability tiers**: `local` always writable; team/org governed via a policy-backend-**agnostic
  interface**; project = team/org governance if owned, else personal/local-writable.
- **New `project-local overlay` scope** (uncommitted, machine-local) just above repo-local
  committed; a `.git/config`-style personal per-project layer; hashed into `inputs_digest`. Stored
  as a **da-managed, gitignored `.agentsrc.local.json`** in the repo (like other managed resources).
- **`local` source is git-backed** (the git-synced `~/.agents` repo): its cache-key default is the
  resolved commit (like `git`) plus working-tree content for uncommitted authoring — not a plain
  directory content/mtime.
- **`inputs_digest` covers all local scopes** (user-local + project-local overlay + repo-local
  committed), whole-normalized.
- **Per-source staleness via uv-style `cache-keys`** (§5.1) with per-kind defaults;
  `last_checked_at` recorded **per source** for the review-nudge; `--refresh` + a declared
  always-revalidate marker as the force escapes.
- **Managed-resource `.gitignore` auto-fill** in consuming projects (da-owned idempotent block,
  §6.1): projected outputs + the `.agentsrc.local.json` overlay are ignored; `.agentsrc.json` /
  `.agentsrc.lock` stay committed. Source of truth = lock + commands, never leaked git state.

## 10. Next steps

Per proposal-routing, this project-local design doc must **graduate** — it should not rot as a
proposal:

1. Fold §3–§6 into a **spec upgrade** of `config-distribution-model/design.md`: rewrite §4
   (tier-constraint relaxation), §6 (two-pass → layer-fixpoint-then-artifacts dependency order),
   §7 (one `units` lock section + `inputs_digest`, TTL→nudge), §8 (caching), and add an
   "auto-sync & `EnsureResolved`" section. Reconcile the `config`/`packages` collapse with
   org-config-resolution §4 and external-agent-sources.
2. Settle the four open decisions (§8).
3. Create the **canonical plan** (`da workflow plan create`) — phased tasks: (p-a) lock schema
   migration (`units` + `inputs_digest` + `digest`/`last_checked_at`, TTL demotion); (p-b)
   `EnsureResolved` seam + flags (`--locked`/`--frozen`/`--no-sync`); (p-c) `local` source
   auto-setup + unified artifact sourcing; (p-d) `--scope`/`--source` routing + editability
   interface; (p-e) command reshape (explain absorbs status, doctor nudges, refresh pre-resolve);
   (p-f) outputs exact/prune projection + `.gitignore` auto-fill (§6.1); (p-g) project-local
   overlay scope.

### Land-it-once strategy (graduation)

Rather than merge the six in-flight config-v2 PRs as-is and rework later, **push the corrected
implementation onto the existing PR branches** so they land already implementing the coherent
model — implement once, correctly. Map each design change onto its PR:

| PR | Branch | Folds in |
|---|---|---|
| #205 | p1d ResolveLocked | `EnsureResolved` seam + content-hash staleness |
| #206 | p2 lockstatus | driver-event drift + TTL→nudge + per-source `last_checked_at` |
| #208 | p3 audit events | `config.*` taxonomy for the resolve/sync/units events |
| #209 | p5 source types | tier-constraint relaxation (git/local artifacts) + `cache-keys` |
| #207 | p4b app-types | consume the unified `units` snapshot |
| #178 | none adapter | unaffected — `adapters` lock section stays separate |

Net-new surfaces with no existing PR (the single `units` lock schema + `inputs_digest`, the
project-local overlay scope, `--scope`/`--source` routing + editability interface, `.gitignore`
auto-fill, command reshape, exact/prune projection) become fresh tasks in the canonical plan,
branched off master and PR'd normally. **USER remains the merger** for every branch.
