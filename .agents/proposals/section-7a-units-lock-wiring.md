# §7A units-lock wiring — make local sources first-class in the lockfile

Status: proposed (next config-v2 task after 0.3.4)
Decision owner: maintainer (sequencing confirmed 2026-06-02: land #7 §7-baseline, then this)

## Problem

A flat / local-only project gets **no `.agentsrc.lock`** today, and `da config
verify` only cross-checks remote `extends` layers. That feels wrong because a
local source is a *managed* config source (often git-backed — we help the user
keep it synced), so it should be tracked in the lock like any other source, and
a lockfile should exist for **every** resolved project, not just ones with
remote extends.

## Root cause: two lock models, the wrong one is live

- **§7 (live):** `resolver.Resolve` → `WriteConfigLock` writes a lockfile
  `config` section keyed by resolved `extends` refs. No extends ⇒ nothing
  meaningful is locked. `da config verify` (#7) reads this section.
- **§7A (built, dormant):** the coherence-redesign model already exists in-tree
  but **no command calls it**:
  - `staleness.go::ComputeInputsDigest` hashes **all local config scopes**
    (user-local, project-local overlay, repo-local) whole-normalized into a
    `sha256:` `inputs_digest`.
  - `lock_units.go::{UnitsLock, LockedUnit, WriteUnitsLock}` — a `units` section
    keyed by `source:path@version` with per-unit `digest`, plus the top-level
    `inputs_digest`. Staleness is content-hash driven, never clock-driven.
  - `ensure_resolved.go::EnsureResolved` — the "§7A.5 auto-sync seam."

`grep` confirms `EnsureResolved` / `ComputeInputsDigest` / `WriteUnitsLock` have
no production callers. The seam is implemented + tested but unwired.

## uv reference (and our deliberate divergence)

uv draws the line at **file vs directory, not local vs remote**: git →
commit-pinned; local *file/archive* → content-hashed; mutable local
*directories* (`directory`/`editable`/`virtual`) → **path only, never hashed**,
read live each sync. uv does not fingerprint a mutable local source tree.

We intentionally diverge: §7A **does** content-hash the local config scopes
(`inputs_digest`). Justified because our "local source" is *managed* config
(small, often git-backed) and we want clock-free drift detection ("your local
config changed since last resolve") — a property uv forgoes for app source
trees. uv's closest analog to "track the managed local source" is its **git**
kind (commit SHA); §7A chose a content-digest, which is source-agnostic.

## Work

1. **Wire `EnsureResolved` into the write path** — `da config sync` and `da
   install` resolve through the §7A seam and write `units` + `inputs_digest`,
   so every resolved project (including flat/local-only) gets a lockfile.
2. **Migrate readers** — `da config verify`, `da config explain`, `da doctor`
   read the `units` section and compare `ComputeInputsDigest` against the lock's
   recorded `inputs_digest` to report **local-scope drift** as a first-class
   check; per-unit digest mismatch as a cache/integrity check.
3. **Deprecate the §7 `config` section** — migrate or dual-write during a soak,
   then drop, per the §7.4 section-ownership rules.
4. **verify follow-up** — replace the §7 `locked-layers` check (extends-only,
   shipped in #7) with the §7A units + inputs_digest checks; a local-only
   project then shows its tracked `inputs_digest`, not "nothing to verify."

## Done criteria

- A flat local-only project, after `da config sync`, has a `.agentsrc.lock`
  carrying `inputs_digest` + (empty-or-populated) `units`.
- `da config verify` reports local-scope drift (inputs_digest mismatch) and
  per-unit cache integrity for git/http/local uniformly.
- No clock-driven staleness anywhere; digests only.

## Relationship

- Source model: config-v2 **coherence redesign** (§7A is its lock model).
- Builds on the 0.3.4 verify baseline (#7): manifest/source/binary checks + the
  §7 extends-layer cache check (incl. the local-layer cache fix `47a3dcf2`).
- Should be folded into the config-v2 plan as the next task.
