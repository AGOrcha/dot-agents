# Package Artifact Install — Design Spec

**Status:** draft (2026-07-15)
**Scope:** project (dot-agents) — closes the unbuilt "install/materialize" half of the config-distribution model so a `da` source can deliver **resource content** (skill/agent/rule/hook files), not just config-layer declarations.
**Written:** 2026-07-15

**Related (canonical owners — this spec REFERENCES, never re-states):**
- [config-distribution-model §15](../config-distribution-model/design.md) — **canonical** sources / scopes / units / lock / `EnsureResolved`. D3 (`kind: artifact` = "installed discretely into the asset store"), D7 (git-tracked ⇔ local-authored; sourced ⇒ gitignored), D9 (scope precedence incl. `repo-imported`), D10 (exact/prune outputs projection), R1 (one `units` lock). This spec consumes those; it does not re-decide the lock, scope precedence, or `inputs_digest`.
- [external-agent-sources §3/§5](../external-agent-sources/design.md) — content layouts (`tree | tarball | registry`), typed artifact media types (`application/vnd.dot-agents.artifact-bundle.v1+tar+gzip`), ref syntax `source-id:artifact-path@version-spec`, and the §5.1 rule: **do not invent new executable package types**. This spec adds the *install target contract* §5 left undefined, not new types.
- [da-project-specifics-source](../da-project-specifics-source/design.md) — the first concrete consumer (dot-agents' own specifics delivered from the `da-agc` source). This spec supplies the artifact-install mechanism that spec's Tier-2 units need. **Correction:** that spec's local-first framing is superseded here — dot-agents fetches its specifics from the external `da-agc` source; the fetched resources land in a **separate, un-tracked split** of `~/.agents`, not in the user's own config-source git.
- [skill-tiering-contract §5](../skill-tiering-contract/design.md) — skill directory layout (`SKILL.md` + `instructions/` + `references/` + …) that a `tree`/`tarball` bundle carries.
- managed-gitignore machinery — `internal/links/gitignore.go`, `internal/config/local_source.go` (the D7 sourced-vs-authored gitignore split this spec relies on).

---

## 1. Problem statement

The v2 config-distribution substrate ships **layers** end-to-end (fetch → merge →
lock → project) but **artifacts** only reach *fetch → bytes*. The package
resolver (pass 2, config-distribution-model §6) is a no-op stub: `resolver.go`
writes an empty packages section, and `SelectPackageFetcher` / `FetchArtifact`
(`internal/config/fetcher_{oci,git,http,local}_artifact.go`) have **zero
non-test, non-fetcher callers**. `unit_kinds.go`'s `IsProjectableKind` comments
`kind: artifact` as "installs", but **nothing installs** — there is no
`MaterializeArtifact`, no unpack-into-store, no `kind:artifact` lock write.

Consequence: a `packages[]` reference resolves to nothing on disk. A source can
version and deliver **config-layer declarations** (the `skills[]`/`agents[]`
*name arrays*, profiles, rules — via `extends`) but **cannot deliver the
resource files themselves**. The linkers only read bodies from the local
`~/.agents/<family>/<scope>/<name>/` store; a fetched artifact never arrives
there.

This blocks the maintainer's goal: dot-agents' own skills/agents should live in
and be **versioned by an external `da` git source (`AGOrcha/da-agc`)**, fetched
onto each machine — not carried inline, and not duplicated into the user's own
`~/.agents` config-source git.

## 2. What "install/materialize" means here

For each resolved `packages[]` reference `<source>:<family>/<name>@<version>`:

1. **Fetch** the artifact bundle from its source (already partly built: returns
   `FetchedArtifact.Data []byte` + digest + posture).
2. **Materialize** the bundle's file tree into the canonical store at
   `~/.agents/<family>/<sourced-scope>/<name>/`.
3. **Record** the unit in `.agentsrc.lock` (`kind: artifact`, keyed
   `source:family/name@version`, with resolved digest) so staleness is
   digest-driven (§15 R1/D4).
4. **Project** the materialized resource into platform dirs via the **shipped**
   exact/prune outputs path (`RunSharedTargetProjectionExact` /
   `internal/platform/resource_plan.go`) — not a parallel linker.

The bundle is an **opaque blob under a typed media-type envelope** (§5.1: no new
package types). Its *content layout* is what the fetch normalizes:
- `tree` — a directory subtree in a git/local source (the ergonomic authoring
  form: `da-agc` holds plain `skill/<name>/SKILL.md` files, git-diffable and
  blame-able). Fetch walks the subtree.
- `tarball` — a single `+tar+gzip` archive blob (http / digest-pinned). Fetch
  reads the blob; materialize untars it.
- `registry` (OCI) — a typed OCI artifact pulled via the OCI distribution
  protocol; the `ociFetcher` (`fetcher_oci.go`) already returns the
  `+tar+gzip` blob, so materialize reuses the `tarball` untar path. In scope
  (D9).

## 3. Decisions

### D1 — Fetched artifacts are a separate, un-tracked split of `~/.agents`

Materialized artifacts land in a **sourced scope** that is **gitignored from the
user's own config-source git** (the `agents-config` remote on `~/.agents`), per
config-distribution-model **D7** (git-tracked ⇔ local-authored; sourced content
is never committed to the local source). Their canonical version lives in the
**external source** (`da-agc`) plus the `.agentsrc.lock` digest — never a
duplicate copy in `agents-config`.

*Why:* this is the maintainer's stated intent and the D7 invariant. A fetched
skill is reproducible from `source@digest`; committing it into `~/.agents`'s own
git would fork it from its source of truth and re-create the half-old/half-new
incoherence §15.1 exists to kill. The managed-gitignore machinery
(`internal/links/gitignore.go`) already auto-ignores sourced content; this spec
routes artifacts through that split.

### D2 — Install target: `~/.agents/<family>/<sourced-scope>/<name>/`, scope namespaced by source id

The `<sourced-scope>` segment is **the source id** (e.g. `da-agc`), yielding
`~/.agents/skills/da-agc/release-docs-refresh/`. Scope is orthogonal to source
and kind (§15 D9), but for *sourced* units the source id is the natural,
collision-free scope key: two sources may each ship a `release-docs-refresh`
without clobbering, and provenance is legible on disk. The resolver maps this
scope into the `repo-imported` precedence rung (§15 D9) when composing effective
config.

*Rejected:* a single flat `repo-imported` scope dir shared by all sources
(collides across sources; loses provenance). *Rejected:* installing into the
project scope (`dot-agents`) alongside local-authored units (violates D1 — the
gitignore split can't cleanly separate authored from fetched in one scope dir).

### D3 — `tree` and `tarball` layouts; honor the media-type envelope; no new types

Fetch supports the `tree` (git subtree), `tarball` (single archive), and
`registry` (OCI) content layouts from external-agent-sources §3 — see D9 for the
OCI specifics. All normalize to the same on-disk result: a resource directory.
The typed media-type envelope (`artifact-bundle.v1+tar+gzip`) is preserved for
validation; **no new package `kind`s or per-file typed schema are introduced**
(external-agent-sources §5.1, §15 D3). `tree` is a *layout*, not a new type.

*Why:* `tree` is what makes `da-agc` ergonomic for a git source — plain files,
no pack/publish step, human review via normal diffs. `tarball` covers http and
digest-pinned reproducibility. The blob-plus-envelope model is what the authors
chose; we implement it, we don't evolve it.

### D4 — Materialize reuses the shipped exact/prune projection

The install writes into the store and then projects via the **existing**
`RunSharedTargetProjectionExact` / `resource_plan.go` path (§15 D10 exact/prune),
so a fetched artifact composes with local-authored units under one projection
with prune semantics — not a second, artifact-only linker.

### D5 — One `units` lock; `kind: artifact`; digest-driven staleness

The materialized artifact is one `units` entry (§15 R1) keyed
`source:family/name@version`, `kind: artifact`, carrying the resolved content
digest and install path. Staleness is digest-driven (§15 D4): a re-resolve with
an unchanged digest is a no-op; a changed upstream digest re-materializes.

### D6 — Pass-2 wiring point

The package resolver (pass 2) runs inside `EnsureResolved` after pass-1 config
resolution, and `da install` / `da refresh` invoke materialize + projection
before the platform linkers read the store. Pass 2 is skipped when the effective
config has no `packages[]` entries (config-distribution-model §6).

### D9 — OCI `registry` is in scope: consume and publish

OCI is not deferred. Both sides are built:

- **Consume.** An `oci` source resolves a `packages` ref to an OCI artifact by
  tag (or `pinned:sha256:` digest), pulls the `+tar+gzip` bundle via the
  `ociFetcher`, **validates the manifest artifact media type against the ref's
  `kind`** (external-agent-sources §5: wrong type in a slot fails before
  execution), then materializes through the same D4 untar path as `tarball`.
  Registry auth uses the external-agent-sources §4 provider model — `bearer` and
  `credential-helper` are the v1 targets (the git-credential-style helper that
  already backs private git sources); `oauth2`/`mtls` are that spec's surface,
  referenced not re-built here.
- **Publish.** `da packages publish` is the producer: it packs a resource tree
  into a `+tar+gzip` artifact-bundle blob under the typed media type and pushes
  it to an OCI registry at `<namespace>/<family>/<name>:<tag>` (external-agent-sources
  §5 namespace form), emitting the digest for `pinned:` reproducibility. This is
  the same bundle format the `tree`/`tarball` fetchers normalize, so producer and
  consumer share one envelope.

*Why now (not deferred):* the media-type envelope and `ociFetcher` already exist;
the missing pieces are the consume-side wiring (shared with the tarball
materialize) and the publish producer. Building both now keeps OCI a first-class
peer of git/local rather than a bolt-on, and gives the enterprise/shared
distribution path (external-agent-sources personas) a working transport. Version
range resolution (`^1.2`) is the one OCI sub-item that stays deferred — tag and
`pinned:sha256:` cover reproducible pulls; SemVer ranges are a follow-on.

## 3A. Hardening decisions (pre-launch cross-harness review, 2026-07-15)

A full-lens Codex (gpt-5.6-sol) review of this spec+plan traced each finding to code and
returned normative fixes. These **supersede** the weaker forms in §3 where they conflict.

- **H1 — Fail-closed bundle-safety contract (BLOCKER: path-traversal/Zip-Slip).** t1 fetch, t2
  materialize, and t8 pack MUST route every bundle through one normalizer: canonical relative
  slash paths only; **reject** empty/`.`/`..`/absolute/drive/UNC/duplicate/case-colliding
  entries and **all** symlink/hardlink/device entries; cap file count + expanded bytes;
  validate the *whole* bundle, extract to a staging dir, then **atomic rename**. Adversarial
  tree+tar tests are a done-criterion. (Refines D3; `ParsePackageRef`/`fetcher_local_artifact.go`
  currently join unvalidated paths.)
- **H2 — Content-addressed immutable store path (BLOCKER: versionless path vs digest lock).**
  The backing store path is **digest-qualified and immutable** (e.g.
  `~/.agents/cache/artifacts/<digest>/…`); the projected view at the sourced scope is a link to
  the locked digest. Two projects on different tags never write one mutable path. (Supersedes
  D2's mutable `<family>/<source-id>/<name>/` as the *storage* path; that name becomes the
  *projection* target only.)
- **H3 — Reserved sourced namespace (HIGH: D2 scope collision).** Materialization targets a
  **reserved** `_sourced` partition (`~/.agents/<family>/_sourced/<source-id>/<name>/`), never a
  segment that can equal a local scope (`global`, the project name). Source IDs colliding with a
  local scope are rejected; materialize **refuses to replace any path lacking sourced
  provenance**. (Supersedes D2's "collision-free by source-id" claim, which the review
  falsified against `internal/platform/resources.go:32-50`.)
- **H4 — Sourced ignore is permanent (HIGH: D1 leak after removal).** The `_sourced` namespace
  is gitignored **permanently** in the local source; the ignore is installed + verified
  **before** the fetched tree is exposed, and is never removed on package removal (only on store
  deletion). The managed-block rewrite is CAS/locked. (Refines D1; fixes the
  `local_source.go:272-294` block-removal leak + the RMW race.)
- **H5 — OCI payload integrity, separate digests (BLOCKER: digest conflation).** `pinned:`
  addresses the **manifest**; the fetcher recomputes SHA-256 over the fetched **payload** and
  compares it to the layer descriptor **before** cache/untar; the verified content digest is
  what the lock records. Manifest-digest ≠ blob-digest is handled explicitly. (Refines D9;
  `fetcher_oci.go:410-420` currently trusts the reported digest.)
- **H6 — Family-specific media-type validation (HIGH).** An OCI pull MUST require both the
  manifest `artifactType` == the artifact-bundle media type **and** the family-specific type,
  rejecting empty/missing/mismatch (remove the empty-type tolerance at
  `fetcher_oci.go:423-434`). Validating "against ref kind" alone is insufficient because the
  lock kind stays `artifact`. No new `UnitKind` is introduced. (Refines D9.)
- **H7 — Post-install integrity (HIGH).** A production artifact-store digest resolver is passed
  to **both** `EnsureResolved` and `config verify` (which currently pass nil unit-digest) so
  store tampering after install prevents projection/invocation — canonical per-unit integrity
  for all source kinds.
- **H8 — Cache temp+rename + verify-on-hit (HIGH concurrency).** The content-addressed cache
  publishes blobs via same-dir temp+rename and **re-verifies every cache hit against its
  digest**, closing the torn-read at `fetcher_oci.go:212-218,245-250`.
- **H9 — Pass-2 hydration ownership (HIGH).** Split **pure resolve/lock-update** from
  **hydrate-from-lock**. Hydration runs once through the real lifecycle
  (`commands/internal/lifecycle/install.go`, `commands/refresh.go`) **before**
  `RunSharedTargetProjectionExact`, and never writes on the frozen/locked-clean no-write
  contract except as the explicit hydrate step. (t3's `commands/install.go` write-scope was
  stale — the file does not exist.)
- **H10 — Lock shape (MEDIUM).** Drop `install_path` from the lock unit (R3); derive it
  deterministically from resolved identity + the H2 storage namespace. The canonical
  `LockedUnit` (`lock_units.go`) is unchanged; no out-of-scope schema edit.
- **H11 — R5 prunes projected output only (MEDIUM).** Removing a package prunes the **projected
  output** via the exact/prune path; store-tree deletion stays deferred GC (safe under H4 —
  the ignore persists).
- **H12 — Credential non-disclosure (t7, HIGH).** Auth config carries **secret references
  only**; resolved env/file/helper secrets never enter `Source.Auth` persistence, locks, cache
  metadata, errors, audit events (`audit.go:204-225` currently logs raw `Err.Error()`), logs,
  generated files, or argv. Helper queries over stdin; tokens in headers only; central error
  redaction; sentinel-secret leak tests across lock/stderr/audit. **t8 depends on t7** and
  reuses its auth path.
- **Publish surface (BLOCKER #4, owner ruling 2026-07-15):** the OCI producer lands on the
  **canonical unified resource CRUD surface** — the retired `da packages` family is **not**
  revived. t8 is re-scoped off `commands/packages`.
- **Confirmed sound (no change):** protected-field override is blocked (`layer_schema.go`); t2
  correctly reuses `RunSharedTargetProjectionExact` (not a parallel linker); lockfile flush is
  atomic + inter-process locked; signing posture fails-closed for `required`; R4/R6 are testable.

### H13–H17 — t2 review corrections: per-project CAS-direct projection (owner ruling 2026-07-15)

A full-lens t2 review found the shared `~/.agents/<family>/_sourced/<source-id>/<name>/` alias
**breaks per-project isolation** — a single global mutable path can't hold each project's resolved
digest, so repo A's package projects into repo B and A@v1/B@v2 collide. These supersede the
projection half of H2/H3/D2.

- **H13 — per-project CAS-direct projection.** Each project's repo output links **directly** to
  the immutable CAS digest path `~/.agents/cache/artifacts/<family>/<digest>/` for the digest
  **that project resolved** (from its lock). There is **no** shared mutable
  `~/.agents/<family>/_sourced/<source-id>/<name>/` alias as the projection authority. The
  projection input set is the **caller's resolved unit list** — each `{family,name,source-id,
  digest,cas-path}` — passed into `RunSharedTargetProjectionExact`; it is **never** discovered by
  scanning the global store. Boundary: **t2 owns the mechanism** (materialize to CAS + project a
  *caller-supplied* set of resolved units to their CAS paths); **t3 drives it** with the project's
  resolved lock. Two projects pinning different digests of the same source/name no longer collide.
- **H14 — CAS gitignored before first write.** `~/.agents/cache/` (covering
  `cache/artifacts/<family>/<digest>`) is permanently gitignored, installed + verified **before
  any CAS byte is written**, checked with git's ignore semantics on the CAS path itself — not only
  the projection. Do not claim the advisory file lock serializes git/user edits.
- **H15 — identity-component containment.** `source-id`, `family`, `name` must each be **one
  canonical path segment** — reject `.`/`..`/separators/absolute/volume/reserved-scope names — and
  the final path is asserted via `filepath.Rel` to remain beneath the exact CAS root.
- **H16 — CAS verify-on-hit.** A pre-existing digest path is trusted only after **re-walking +
  re-verifying its canonical bundle digest and shape** (the H8 discipline applied to the store); a
  corrupt/wrong-type/rename-race mismatch is rejected or quarantined, never trusted on `os.Stat`.
- **H17 — no `RemoveAll` after a separate ownership check.** Replacing a managed link uses a
  non-recursive identity-rechecked unlink or an atomic temp-link swap; never `RemoveAll(path)`
  after a prior ownership check (a race could delete user content).
- **Confirmed sound in t2:** the same-filesystem atomic CAS publish, and that exact/prune does not
  delete plain user files.

## 4. Requirements (behavioral)

- **R1** Given `packages: ["da-agc:skill/release-docs-refresh@main"]` and a
  `git` source `da-agc`, `da install` (or `da refresh`) materializes
  `~/.agents/skills/da-agc/release-docs-refresh/SKILL.md` (+ its subtree) and the
  skill becomes projected/invocable for enabled platforms.
- **R2** The materialized tree is **gitignored** from `~/.agents`'s own git — a
  `git -C ~/.agents status` shows no fetched-artifact files (D1/D7).
- **R3** `.agentsrc.lock` gains a `kind: artifact` unit for the ref with the
  resolved digest and install path (D5).
- **R4** Re-running `da refresh` with an unchanged upstream yields **no diff**
  (idempotent; digest-addressed).
- **R5** A changed upstream (`da-agc` commit that alters the subtree) →
  re-materialize on next resolve; a removed `packages[]` entry → the artifact is
  pruned from the store on projection (D4 exact/prune).
- **R6** Local-authored resources (project/global scopes, `agents-config`-tracked)
  are **unaffected** — no behavior change, byte-parity for existing consumers.
- **R7** `tree` (git subtree), `tarball` (archive blob), and `registry` (OCI)
  layouts all satisfy R1. An OCI pull whose manifest media type does not match
  the ref's `kind` fails resolution before materialize (D9).
- **R8** `da packages publish <family>/<name>` packs a resource tree into a
  typed `+tar+gzip` artifact-bundle, pushes it to the target OCI registry, and
  prints the resulting `sha256:` digest; a subsequent `packages` pull of that
  `pinned:sha256:` digest round-trips byte-identically (D9).

## 5. Done criteria

- **DC1** End-to-end against `AGOrcha/da-agc`: git source + `packages` ref →
  `da install` → materialized + gitignored + locked + projected → the
  `release-docs-refresh` skill and `platform-dirs-change-analyst` agent resolve
  from the fetched store and are invocable.
- **DC2** A `tree`-layout git-source content-install smoke test (extends the
  `verify/git-source-smoke` fixture — commit `fa331cdd` — from layer-only to
  content install) passes in CI.
- **DC2-oci** An OCI round-trip test: `da packages publish` a resource to a
  local registry (`ggcr`/`oras` test registry or `zot`), pull it via an `oci`
  source `packages` ref, and assert materialize + media-type validation + digest
  reproducibility (D9/R8).
- **DC3** R2/R4/R6 proven by test: gitignore split holds; re-refresh is a no-op;
  local-authored units are byte-unchanged.
- **DC4** `go test ./internal/config/... ./commands/... ./internal/platform/...`
  green; `./scripts/verify.sh` smoke green.

## 6. Deferred (explicitly out of scope)

- **OCI SemVer range resolution** (`^1.2`) — tag and `pinned:sha256:` pulls are
  in scope (D9); client-side SemVer range resolution across registry tags is a
  follow-on (external-agent-sources §5).
- **`oauth2`/`mtls` registry auth providers** — `bearer` + `credential-helper`
  are in scope (D9); the browser/PKI providers stay owned by
  external-agent-sources §4.
- **Bundle-manifest** multi-resource pointer doc
  (`application/vnd.dotagents.bundle.v1+json`) — external-agent-sources §5; this
  spec resolves single `family/name` refs only.
- **SemVer range resolution** (`^1.2`) — support `@<ref/tag>` and
  `@pinned:sha256:` first; ranges later (external-agent-sources §5).
- **Garbage collection** of artifacts no longer referenced by any lock unit —
  prune-on-projection (D4/R5) covers the projected copy; store-level GC of the
  fetched tree is a follow-on.
- **`da-agc` authoring/populate + the dot-agents `.agentsrc.json` cutover** — a
  wiring/authoring task in the plan, gated on the mechanism (this spec) landing.

## 7. Relationship to plans

Implemented by plan `package-artifact-install`. Task success criteria trace to
the done criteria above. The `da-agc` populate + `.agentsrc.json` cutover
(consumer wiring) trace additionally to `da-project-specifics-source` §5 parity
discipline (effective config byte-identical pre/post for the *layer* half, while
the *artifact* half newly materializes content).

## 8. Open questions

- **Q1 — sourced-scope segment name.** D2 uses the source id (`da-agc`) as the
  scope dir. Confirm against the §15 D9 `repo-imported` rung: is the on-disk dir
  `<family>/<source-id>/<name>/` with the resolver mapping it to `repo-imported`
  precedence, or a literal `<family>/repo-imported/<name>/` with source in the
  lock only? *Lean:* source-id dir (provenance on disk, collision-free);
  resolve at t1.
- **Q2 — `tree` fetch normalization.** Does the git fetcher assemble the subtree
  in-memory and hand a normalized bundle to a shared materializer, or write
  directly to the store? *Lean:* fetch → normalized in-memory bundle → shared
  materializer (keeps oci/http/git symmetric, testable). Resolve at t1/t2.
- **Q3 — projection ownership of fetched units.** Confirm
  `RunSharedTargetProjectionExact` can treat a sourced scope as an input set
  without a parallel plan; if not, the smallest extension needed. Resolve at t2.
