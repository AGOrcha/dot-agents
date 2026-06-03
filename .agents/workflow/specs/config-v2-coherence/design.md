# Spec: Config-v2 Coherence — Scopes, Sources, Units, and the Lock

**Status:** draft (graduated from proposal `config-v2-coherence-scopes-sources-lock.md`
+ `section-7a-units-lock-wiring.md`).
**Owns:** the coherent scope / source / kind model, the unified `units` lockfile with
content-hash staleness (the "§7A" lock), and the auto-sync (`EnsureResolved`) contract.
**Design philosophy:** agent DX is the primary operator; humans are a secondary,
cross-cutting audience.

This spec is the contract the **`config-v2-coherence`** plan and the remaining
**`config-v2-migration`** finish-line tasks (p4c-rest, p4f, p1c) are accountable to. It
does not contain file paths, function names, or task ordering — those live in the plans.

---

## 1. Problem

Adding a uv-style "detect staleness → auto re-resolve → auto-sync outputs" lockfile to
config-v2 surfaced a deeper incoherence that must be resolved first, so auto-sync is built
on a coherent model rather than bolted onto the current overloaded one. Four questions the
current model answers incoherently:

1. **Config CRUD is half-old/half-new.** `da skills|agents|hooks|rules|mcp|settings`
   author files directly into `~/.agents/` (the "old" model), while the
   extends/packages/lock machinery is the "new" model. The same logical slot (a skill) is
   half authored-file, half resolvable-unit.
2. **The lock is ambiguous.** It is unclear what `.agentsrc.lock` tracks and what makes it
   stale. The live model (§7) locks only resolved `extends` refs, so a project with no
   remote extends gets *no meaningful lock at all* — and flat/local-only projects get no
   lockfile.
3. **Three axes are fused.** `~/.agents/` (`AgentsHome()`, ~40 call sites) simultaneously
   serves a config **scope** (precedence), a canonical **asset store** (source material),
   **scoped content**, genuinely-local operational **state**, and a fetch **cache** — fusing
   *scope* (precedence), *source* (origin/transport), and *kind* (policy vs artifact), plus
   real machine-local state.
4. **Command intents blur.** Overlapping inspection surfaces (`status` / `doctor` /
   `config explain`) with unclear ownership; a `sync` vs `config sync` verb collision.

## 2. Goals

- A single coherent model where every resolvable thing — a config layer *or* an artifact
  like a skill — is addressed uniformly and tracked uniformly in one lock.
- A lockfile that exists for **every** resolved project (including flat/local-only), whose
  staleness is **content-hash driven, never clock-driven**.
- CRUD, sourcing, and versioning that are the same whether a unit is authored locally or
  pulled from a remote registry — differing only by *source*.
- Crisp, non-overlapping command intents (one truth surface for effective config; one for
  fleet health; one for nudges).
- uv-proven ergonomics (`--locked` / `--frozen` / `--no-sync` / `--offline`; explicit-only
  upstream re-check; exact/prune projection).

## 3. Decisions (with rationale)

Each decision was settled in design review; the rejected alternative is noted.

### D1 — Three orthogonal axes, not one overloaded directory
A resolvable unit is described by three independent axes:
- **Scope** (precedence; *merges* into effective policy): product → user-local → org →
  team → repo-imported → repo-local (committed) → **project-local overlay (uncommitted)** →
  runtime. Answers "who gets the last word." (Extends `org-config-resolution` §4 with one new
  scope — see D9.)
- **Source** (origin/transport; *resolves + versions* a unit): `git` / `http` / `local` /
  `oci`. Answers "where a unit comes from and how it is versioned." Orthogonal to scope.
- **Kind** (behavior): `layer` (mergeable policy) vs `artifact` (installable executable).

*Rejected:* keeping scope/source/kind fused in one directory with special-cased paths —
the root cause of the half-old/half-new feeling.

### D2 — Versioning and sourcing are unified via `source:path@version`
Everything resolvable is addressed by the reference syntax already in the base spec
(`config-distribution-model` §5): `source-id : layer-or-artifact-path @ version-spec`. A
skill authored locally and a skill pulled from a registry are the **same kind of resolvable
unit**, differing only by source.

*Rejected:* tier-specific addressing (config refs vs package refs as separate syntaxes).

### D3 — The `config`/`packages` tier wall collapses into one `units` model
With sourcing/versioning unified (D2), the Tier-1 `config` vs Tier-2 `packages` split — two
parallel subsystems with different source constraints, lock sections, caches, and command
trees — is no longer warranted. What remains is a single model of **resolvable units**
carrying a `kind` that drives exactly two behaviors:
- **layer**: a JSON field-bundle that *merges* into effective config by category rules;
  protected-field rules apply; it may *declare* further units.
- **artifact**: a bundle *installed discretely* into the asset store and invoked;
  trust/signing rules apply; it does not merge.

Consequences: one lock `units` section; one source model (any source serves any kind); one
CRUD surface (a `--scope`/`--source` flag, not parallel command trees). The two-pass
resolution survives only as a **dependency order** (resolve mergeable layers to a fixpoint
first — they declare what else to fetch — then resolve/install the full declared artifact
set), **not** a tier wall.

*Rejected:* preserving two subsystems; it duplicates source/lock/cache/command machinery
for no behavioral gain.

### D4 — Staleness is content-hash driver events; TTL becomes a review-nudge
The lock tracks, per resolved unit, a content `digest` and a top-level `inputs_digest` (a
hash of all **local** scopes). A project is stale iff: `inputs_digest` mismatches (a local
manifest changed) **OR** the declared set of refs changed **OR** a recorded digest no longer
matches. Cheap, local, no network, no clock.

`cache_ttl` **no longer auto-invalidates or auto-fetches**. A per-source `last_checked_at`
powers a `doctor`/`config explain` *review-nudge* ("layer X last re-checked 6d ago — run
`da config sync`") — a separate axis from truth. This converges three sources of truth: the
base spec's own "policy can drift vs content-addressed immutable" table, the KG spec's
"domain knowledge does not decay on a clock; staleness = driver events; time = review-nudge,"
and uv (no manifest hash, never auto-upgrades on a timer).

*Rejected:* clock/TTL-driven auto-invalidation (chases upstream on a timer; non-reproducible;
needless git churn).

### D5 — `inputs_digest` covers all local scopes, whole-normalized
The top-level `inputs_digest` hashes user-local + project-local overlay + repo-local
committed manifests together, normalized. This is the deliberate divergence from uv (which
never fingerprints a mutable local source tree): our "local source" is *managed* config
(small, often git-backed), and clock-free local-scope drift detection is worth the hash.

### D6 — The `local` source is git-backed and auto-bootstrapped
For local-authored assets to resolve through the same `source:path@version` path as remote
ones, the `local` source (`~/.agents/`, already a git repo) is initialized + made
version-resolvable on first resolve/init. A local skill/agent/layer then carries a real
version (the local repo ref); the lock records `local:<path>@<ref> → digest` like any other
source. CRUD becomes "author into the `local` source working tree." Its cache-key default is
the resolved commit (like `git`) **plus working-tree content** (so authoring before a commit
still registers as a driver event).

### D7 — One asset dir; sourced units are gitignored from the `local` source
Local-authored and remote-materialized units share one asset dir, but remote/sourced units
are excluded from the `local` source's git tracking, so `da sync` never commits fetched
assets into the user's personal repo. Provenance = the unit's source in the lock;
git-tracked ⇔ local-authored.

### D8 — Artifact sourcing is relaxed; OCI is one source, not the definition
`git` / `local` / `http` / `oci` are all valid for **artifacts** (not just OCI). The base
spec's §4 tier-constraint table updates accordingly.

### D9 — New `project-local overlay` scope (uncommitted, machine-local)
A personal per-project layer — the `.git/config` analog (local > user) — sits just above
repo-local committed and below runtime. Stored as a da-managed, **gitignored**
`.agentsrc.local.json` in the repo (discoverable, lean, repo tree stays clean of *its* own
content because it is gitignored). It is a **local input** → hashed into `inputs_digest`.

### D10 — Auto-sync = lock + outputs parity, via one `EnsureResolved` seam
Config-consuming mutating/setup commands call one seam that: (1) computes current
`inputs_digest` + declared set (cheap, local); (2) if fresh ⇒ no-op (no network, no write);
(3) if stale ⇒ re-resolve local scopes (+ cached layers), rewrite the lock; (4) apply the
**outputs** half — project the asset-store union into the repo, **exact/prune by default**
(delete managed outputs no longer in the resolved set). uv-proven flags: `--locked` (assert;
error non-zero if the lock would change — CI gate), `--frozen` (use lock as-is; skip the
staleness check), `--no-sync` (skip the outputs apply), `--offline` (lock/cache only),
`--refresh` / `da config sync` (the explicit upstream re-check). **Upstream re-check is
explicit-only** — auto-sync never chases upstream on its own.

### D11 — CRUD/sync route by `--scope`/`--source` + an editability check (no per-store trees)
A single `--scope`/`--source` flag routes a CRUD or sync op to a source; an **editability
check** governs writes. This replaces both a `sync`→`config sync` rename and the
package-shadow guard. `local` is always writable (personal); team/org are governed by their
own external policies via a **policy-backend-agnostic interface** (D-open-1).

### D12 — Command intents (agent-DX-first)
- `config explain` — **the** effective-policy truth surface (values, provenance, lock
  freshness, flags); **auto-locks** (writes the lock to stay current, like `uv tree`); does
  not project. **Absorbs `status`'s config inspection fully.**
- `status` — managed projects + projection/link health (the fleet view); read-only; drops
  config-value/freshness reporting.
- `doctor` — health + pending driver events + review-nudges; repair *guidance* only; never
  repairs.
- `refresh` — project the asset-store union → repos (the "sync outputs" half); ensures lock
  fresh first; stays distinct from `config sync`.
- `install` — full setup = resolve + lock + project + setup-contract (the canonical "make
  everything current," ≈ `uv sync`).
- `config sync` — explicit upstream config re-check (≈ `uv --upgrade`).
- `sync` (git) — git-manage a source repo; `--source <id>` selects which (default `local`).

### D13 — Scoped content routes to its scope's source
`proposals/`, `context/`, lessons, and asset units are **scoped** (user/repo/team/org) and
route to the source backing that scope (generalizing proposal-routing's global-vs-project
split to all four scopes). Only the fetch `cache/` and the managed-project registry are
genuinely non-scoped local operational state — never a scope, never a source, never projected.

### D14 — Managed-resource `.gitignore` auto-fill in consuming projects
`da` owns a delimited, idempotent block in each consuming project's `.gitignore`: projected
links, generated platform configs, the `.agentsrc.local.json` overlay, and materialized
asset units are **ignored**; `.agentsrc.json` and `.agentsrc.lock` stay **committed** (the
resolved-state contract, like `uv.lock`). Re-runs converge (regenerated, not appended).

## 4. Requirements (behavioral)

R1. **A lockfile exists for every resolved project**, including flat/local-only, carrying
`lock_version`, `inputs_digest`, and a `units` map keyed by `source:path@version` →
`{kind, digest, fetched_at, last_checked_at}`. The `adapters` section is preserved untouched
(owned by `graph-backend-adapter-contract`).

R2. **Staleness never consults a clock.** A project is fresh ⇔ `inputs_digest` matches, the
declared ref set is unchanged, and recorded digests match. `last_checked_at` only powers
nudges; it never auto-invalidates.

R3. **`config verify` / `config explain` / `doctor` read the `units` section** and compare a
freshly-computed `inputs_digest` against the lock's recorded value to report **local-scope
drift** as a first-class check, and per-unit digest mismatch as a cache/integrity check —
uniformly across git/http/local/oci. A local-only project shows its tracked `inputs_digest`,
not "nothing to verify."

R4. **CRUD (`skills|agents|hooks|rules|mcp|settings`) routes via `--scope`/`--source`** and
passes an editability check before writing; each op registers as a driver event.

R5. **`EnsureResolved` is idempotent and convergent:** fresh ⇒ no-op (no network, no write);
stale ⇒ re-resolve + rewrite lock + project outputs exact/prune; the flags in D10 behave
exactly as specified; re-running yields no diff.

R6. **Per-source cache-keys** follow uv's model with per-kind defaults (D4/D6) and are
overridable per source (file/glob, git commit/tags, env, dir-presence). Force escapes:
per-unit `--refresh` and a config-declared always-revalidate marker.

R7. **One `units` lock section** — no separate `config`/`packages` sections. The §7 extends-
only `config` section is migrated/dual-written during a soak, then dropped per the base
spec's section-ownership rules.

R8. **`.gitignore` auto-fill** (D14) leaves `.agentsrc.json`/`.agentsrc.lock` tracked and
every managed/generated output ignored; the block is idempotent.

## 5. Done criteria

1. A flat local-only project, after `da config sync`, has a `.agentsrc.lock` carrying
   `inputs_digest` + (empty-or-populated) `units` (R1).
2. `da config verify` reports local-scope drift (inputs_digest mismatch) and per-unit cache
   integrity for git/http/local/oci uniformly (R3).
3. No clock-driven staleness anywhere; digests only (R2).
4. `config explain` is the single effective-policy truth surface and `status` no longer
   reports config values/freshness (D12).
5. The lock has one `units` section (config/packages collapsed), `adapters` untouched (R7).
6. CRUD routes via `--scope`/`--source` with an editability check (R4).
7. `EnsureResolved` is convergent and honors `--locked`/`--frozen`/`--no-sync`/`--offline`
   (R5); `--locked` exits non-zero if the lock would change.
8. `.gitignore` auto-fill converges on re-run with `.agentsrc.{json,lock}` committed (R8).

## 6. Open questions

- **D-open-1 — Governance interface shape (team/org editability).** `local` is always
  writable; team/org are governed by highly-variable external policies, so writes go through
  a **policy-backend-agnostic interface** ("can principal P write source S?") with pluggable
  implementations (the `graph-backend-adapter-contract` pattern). *Where the policy plugs in,
  what it returns, and how project editability is derived (team/org-owned → that governance;
  else personal → local-writable) is unresolved.* Must be settled before D11's write path is
  built.
- **Inherited from `config-distribution-model` §14 that this spec touches:** Q3 config-layer
  signing (p5 ships a warn-only verifier; ERROR-by-default deferred) and Q5 workspace-level
  lockfile (ruled either-or like git; not implemented here).

## 7. Deferred / out of scope

- The governance **implementation** (this spec fixes the *interface* requirement only).
- The graph `adapters` lock section (owned by `graph-backend-adapter-contract`; this spec
  only guarantees it is preserved as a peer section).
- OCI artifact transport/signing specifics (owned by `external-agent-sources`).
- v1 deprecation + auto-migration (separate, soak-gated `config-v2-migration` tail).

## 8. Relationship to other specs and plans

- **`config-distribution-model/design.md`** (base contract) — this spec *upgrades* its §4
  (tier-constraint relaxation, D8), §6 (two-pass → layer-fixpoint-then-artifacts dependency
  order, D3), §7 (one `units` section + `inputs_digest`, TTL→nudge, D4/D7), and §8 (caching).
- **`org-config-resolution/design.md`** (scope/merge model) — this spec *adds* the
  project-local overlay scope to its §4 precedence stack (D9).
- **`external-agent-sources/design.md`** (source/transport/OCI) — this spec relaxes its
  artifact-sourcing constraint (D8); transport/auth/signing stay its territory.
- **`graph-backend-adapter-contract`** — peer owner of the lock's `adapters` section; shares
  the schema-agnostic lock writer; this spec must not write that section.
- **Plans:** `config-v2-coherence` (the phased implementation of this spec) and the
  `config-v2-migration` finish-line tasks — **p4f** wires the already-built-but-dormant §7A
  seam (the `units`+`inputs_digest` lock model: implemented and tested in-tree, currently
  with zero production callers) into the write path; **p4c-rest** adds `config sync`/`lint`;
  **p1c** carries the typed verifier-profile migration. The plan's task success criteria must
  trace back to §5 here.

## 9. Implementation status note (context, not contract)

The §7A lock model — `inputs_digest` computation, the `units` lock structure, and the
`EnsureResolved` auto-sync seam — is **already implemented and tested in-tree but unwired**
(no production callers). This spec's near-term work is therefore predominantly *wiring +
reader migration + command reshape*, not greenfield. The `local`-source auto-setup (D6),
the project-local overlay scope (D9), `--scope`/`--source` routing + the editability
interface (D11/D-open-1), `.gitignore` auto-fill (D14), and the command reshape (D12) are the
net-new surfaces.
