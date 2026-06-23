# Relax `extends` to accept OCI — design (spec)

**Spec id:** `extends-oci-relax`
**Status:** design artifact (spec tier). Amends `config-distribution-model` §15 (a new decision **D13**, extending D8). Plan: `workflow/plans/extends-oci-relax/`.

## 1. Problem & why

§15 unified config into one `units` model where **source ⟂ kind** ("any source serves any
kind; OCI is one source, not the definition"). D8 delivered that for **artifacts** (`packages`
accept git/local/http/oci). But **`extends` (config layers) still rejects `oci`** — the one
remaining asymmetry, enforced at resolve time by `SelectFetcher` (`internal/config/fetcher.go`).

The original rationale (§4): "config layers are not executable artifacts — no binary payload,
no OCI media type, no code surface." True that a layer *is* a plain mergeable config document,
but OCI registries can store **arbitrary blobs** (the OCI-artifacts spec allows any media type),
so "a layer has no OCI representation" is a stance, not a limit.

**Why relax it (the decisive use case):** an org that distributes its agent config via an OCI
registry — signed, versioned, content-addressed — wants its **entire** config there as the
single source of truth: not just `skill`/`agent` artifacts, but its `org/base` **config layer**
too. Today they must stand up a separate git/http endpoint just for the layer. Completing the
orthogonality lets OCI be the SOT for both kinds.

## 2. Decision (D13 — amends config-distribution-model §15, extends D8)

**`oci` becomes valid for `extends`.** A config layer may be fetched from an OCI source exactly
as artifacts are. After this, **there is NO source/kind asymmetry** — all of `local`/`git`/`http`/`oci`
are valid for both `extends` (layers) and `packages` (artifacts).

**OCI-layer representation (the one sub-decision):** a config layer is published to OCI as a
single blob carrying the layer document (the same `layer.json` shape a git/http/local layer
serves), under a **dedicated layer media type** `application/vnd.dot-agents.config-layer.v1+json`
(distinct from the artifact-bundle media type). The puller:
- reuses the existing OCI pull plumbing (digest-addressing, auth, cache, posture/signing) that
  `ociFetcher.FetchArtifact` already implements — factor the shared pull, don't duplicate it;
- returns the blob content as a `FetchedLayer` (the `layer.json`) for the resolver to merge,
  NOT a `FetchedArtifact` (which installs a bundle);
- validates the media type: an `extends` pull MUST be a config-layer media type — if the OCI
  ref points at an artifact-bundle media type, fail with a clear error (and vice-versa for
  `packages`), so `kind` stays meaningful even though source is now unrestricted.

`kind` (layer vs artifact) continues to govern merge/trust; **source no longer constrains kind.**

## 3. Done criteria

1. `SelectFetcher("oci")` returns a layer fetcher (no longer a schema-violation error).
2. A config layer published to an OCI source resolves through `extends` and is merged like any
   layer (verified by a test pulling an OCI-served layer blob → `FetchedLayer` → resolver merge).
3. Media-type guard: an `extends` ref to an artifact-bundle media type fails clearly; a `packages`
   ref to a layer media type fails clearly (kind/source orthogonal, but the unit's media type
   must match its declared kind).
4. The old `TestSelectFetcherTierConstraint` (asserts oci rejected for extends) is flipped to
   assert acceptance; existing git/http/local extends behavior unchanged.
5. Spec `config-distribution-model` §15 carries D13; §4's banner/matrix updated to "no asymmetry";
   the user-facing docs (#110 guide matrix + callout, README, the `SelectFetcher` error string +
   the stale comments) flip from "extends rejects oci" to "all sources serve all kinds."
6. No regression to artifact (`packages`) sourcing.

## 4. Sequencing

The docs/comment/error-string edits (D13 task t3) touch the SAME lines PRs #110 (guide) and #111
(README + comments + `SelectFetcher` error string) just corrected to today's "extends rejects oci"
behavior. **Land #110 + #111 first** (they're correct for current shipped code), THEN this plan's
t3 flips them to the post-D13 behavior — so the docs are never wrong at any point: #110/#111 fix
the *packages* lies now; t3 flips the *extends* statement when the code actually changes (t2).

## 5. Out of scope / deferred

- A `da` command to PUBLISH a layer to OCI (this spec is the consume/`extends` side; publishing
  is the producer's concern — `oras`/registry tooling — until/unless we add a `da` publish path).
- Post-0.4.0: this is a **follow-up feature**, not a 0.4.0 release gate (0.4.0 ships with the
  asymmetry; D13 removes it next). Flag for the maintainer if they want it folded into 0.4.0.

## 6. Relationship to other artifacts

- Amends `config-distribution-model` §15 (D13 extends D8) and §4 (the source-type matrix).
- Builds on `ce`/#100 (the artifact-side relaxation + the OCI pull plumbing it generalized).
- Flips the framing that `extends-oci-relax` docs task inherits from #110/#111.
