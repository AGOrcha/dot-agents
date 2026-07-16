---
schema_version: 1
task_id: t6-oci-consume
parent_plan_id: package-artifact-install
title: Consume OCI artifacts (fetch, tag/digest resolve, media-type validate)
summary: 'Wired OCI package consume into t2''s materialize path within write_scope (fetcher_oci.go, oci_resolve.go; resolver.go needed no changes). Fixed the CRITICAL t3-flagged gap: FetchArtifact now runs fresh and digest-pinned-cache-hit OCI payloads through MaybeUntarBundle so FetchedArtifact.Bundle is populated exactly like git/local/http. H5: digest is always recomputed over the payload (never trusts blob.Digest label), closing a spoofed-digest bypass — regression test TestOCIFetcherRejectsSpoofedDigestLabel. H6: a fresh packages/artifact pull now requires the manifest artifactType AND the blob media type to both be non-empty and equal the artifact-bundle type (no empty-type tolerance); the extends/config-layer OCI path (fetcher_oci_layer.go) is untouched and keeps its existing tolerant contract — its tests still pass unmodified. SemVer-range version-specs are rejected up front (oci_resolve.go) instead of silently becoming a literal tag. New e2e test TestOCIArtifactMaterializesAndProjectsLikeAnyOtherSource proves an OCI-fetched Bundle flows through platform.MaterializeArtifact + ProjectResolvedUnits with zero OCI-aware code in either function. Had to touch fetcher_test.go (not in the listed write_scope) to add ArtifactType to existing OCI test fixtures — unavoidable once H6 made that field mandatory; no other out-of-scope files were edited. commands/internal/lifecycle/packages_pass2.go:246-248 still hard-rejects src.Type==oci ahead of any of this (a t3 stopgap, out of scope) — recorded as fold-back oci-consume-pass2-guard-blocks-e2e; deleting those 3 lines is the only remaining step to unblock real da install/refresh against an OCI source.'
files_changed: []
verification_result:
    status: pass
    summary: Delete the OCI early-reject block in commands/internal/lifecycle/packages_pass2.go (fetchAndMaterializePackage, ~lines 246-248) to actually unblock end-to-end pass-2 OCI consume; everything downstream (fetch, Bundle, H5, H6, materialize, project) is implemented and tested. TASKS.yaml tracker shows t3/t1b as pending despite their code being present on feat/package-artifact-install per the launcher's explicit base-branch instruction; worth reconciling tracker state when closing out.
integration_notes: Delete the OCI early-reject block in commands/internal/lifecycle/packages_pass2.go (fetchAndMaterializePackage, ~lines 246-248) to actually unblock end-to-end pass-2 OCI consume; everything downstream (fetch, Bundle, H5, H6, materialize, project) is implemented and tested. TASKS.yaml tracker shows t3/t1b as pending despite their code being present on feat/package-artifact-install per the launcher's explicit base-branch instruction; worth reconciling tracker state when closing out.
created_at: "2026-07-16T01:51:19Z"
---

## Summary

Wired OCI package consume into t2's materialize path within write_scope (fetcher_oci.go, oci_resolve.go; resolver.go needed no changes). Fixed the CRITICAL t3-flagged gap: FetchArtifact now runs fresh and digest-pinned-cache-hit OCI payloads through MaybeUntarBundle so FetchedArtifact.Bundle is populated exactly like git/local/http. H5: digest is always recomputed over the payload (never trusts blob.Digest label), closing a spoofed-digest bypass — regression test TestOCIFetcherRejectsSpoofedDigestLabel. H6: a fresh packages/artifact pull now requires the manifest artifactType AND the blob media type to both be non-empty and equal the artifact-bundle type (no empty-type tolerance); the extends/config-layer OCI path (fetcher_oci_layer.go) is untouched and keeps its existing tolerant contract — its tests still pass unmodified. SemVer-range version-specs are rejected up front (oci_resolve.go) instead of silently becoming a literal tag. New e2e test TestOCIArtifactMaterializesAndProjectsLikeAnyOtherSource proves an OCI-fetched Bundle flows through platform.MaterializeArtifact + ProjectResolvedUnits with zero OCI-aware code in either function. Had to touch fetcher_test.go (not in the listed write_scope) to add ArtifactType to existing OCI test fixtures — unavoidable once H6 made that field mandatory; no other out-of-scope files were edited. commands/internal/lifecycle/packages_pass2.go:246-248 still hard-rejects src.Type==oci ahead of any of this (a t3 stopgap, out of scope) — recorded as fold-back oci-consume-pass2-guard-blocks-e2e; deleting those 3 lines is the only remaining step to unblock real da install/refresh against an OCI source.

## Integration Notes

Delete the OCI early-reject block in commands/internal/lifecycle/packages_pass2.go (fetchAndMaterializePackage, ~lines 246-248) to actually unblock end-to-end pass-2 OCI consume; everything downstream (fetch, Bundle, H5, H6, materialize, project) is implemented and tested. TASKS.yaml tracker shows t3/t1b as pending despite their code being present on feat/package-artifact-install per the launcher's explicit base-branch instruction; worth reconciling tracker state when closing out.

## Round-2 addendum (commit a62c1e8c) — cross-harness adversarial re-gate

The round-1 delegation was already closed by the parent, so this round-2 work
could not be recorded via `workflow merge-back` (delegation completed). Evidence
is captured in `workflow verify record` (test/pass) + checkpoint iter-71. The
coordinator closes out from main.

A Codex (gpt-5.6-sol) adversarial pass returned **fail** with three reachable
findings the round-1 green suite missed; all three are now fixed within
write_scope (internal/config/fetcher_oci.go + OCI test files):

1. **BLOCKER — tag pull had no integrity check.** A tag pull (empty `ref.Digest`)
   recomputed the payload digest but never compared it to anything; the
   manifest's declared layer-descriptor digest was discarded, so a MITM serving
   tampered bytes under a valid tag + correct media labels flowed straight to
   untar/cache. Fix: `ociBlob` now splits the layer-descriptor `Digest` from a
   new `ManifestDigest`; `pullOCIContent` ALWAYS compares the recomputed payload
   digest to the declared layer-descriptor digest before type-check/untar/cache
   (both tag and pinned), and `verifyOCIPin` validates a pin against the manifest
   digest (payload-digest fallback until the manifest-fetch wire protocol lands).
   Test: `TestOCIFetcherRejectsTamperedTagPull`.

2. **HIGH — cache hit skipped OCI type validation.** The shared digest-keyed
   packages cache stores only bytes+digest; a valid tar seeded by another source
   (or directly) materialized under an OCI pin with no OCI type ever validated.
   Fix: fresh OCI pulls persist an `oci-type.json` sidecar (artifactType + layer
   media type); a pinned cache hit is trusted only when `readCachedOCIArtifact`
   finds a matching sidecar, else it's a MISS and the manifest is re-resolved.
   Test: `TestOCIFetcherCacheHitWithoutSidecarIsNotTrusted`.

3. **MEDIUM — cache writes weren't confinement-checked.** `writeCachedArtifact`
   used path-based MkdirAll + atomic write; a symlink/junction planted at the
   digest dir could redirect the write outside the cache. Fix:
   `writeConfinedPackagesCacheFile` does MkdirAll + temp create + same-dir rename
   entirely through an `os.Root` beneath the packages cache root and rejects a
   symlinked digest dir. Test:
   `TestWriteConfinedPackagesCacheFileRejectsSymlinkedDigestDir`.

Verification: `go test ./internal/config/... -race` green; platform + lifecycle
green. The extends/config-layer OCI path (fetcher_oci_layer.go, out of scope) is
untouched and keeps its tolerant media-type contract; its tests are unmodified
and pass. The `ociBlob.Digest` field name was kept (as the layer-descriptor
digest) and only `ManifestDigest` was added, so no out-of-scope
resolver_test.go / fetcher_oci_layer.go edits were needed.

Still open (round-1): delete the OCI early-reject in
commands/internal/lifecycle/packages_pass2.go (~lines 246-248) to unblock real
`da install`/`refresh` against an OCI source. New on-disk artifact:
`~/.agents/cache/packages/<digest>/oci-type.json` (cache is gitignored; note for
any cache-GC/inspection tooling).

## Round-3 addendum (commit 7ef8e8ba) — cross-harness re-review, two remaining items

The delegation is still marked completed (parent closed it after round-1), so
`workflow merge-back` again declined to record; evidence is in
`workflow verify record` (test/pass) + checkpoint iter-72. Coordinator closes out
from main. Write_scope was expanded this round to include
`internal/config/fetcher_oci_layer.go`.

The Codex re-review confirmed the round-2 fixes hold and are non-vacuously
tested, and flagged two remaining reachable items — both now fixed:

1. **MEDIUM — artifact integrity anchor made mandatory** (fetcher_oci.go). The
   layer-descriptor integrity check was conditional (`if blob.Digest != ""`), so
   a MITM could OMIT the descriptor digest on a fresh artifact tag pull and
   bypass the comparison entirely. `verifyOCILayerDescriptorDigest` now REQUIRES
   a well-formed layer-descriptor digest on the packages/artifact path (empty or
   malformed → rejected before type-check/untar/cache). The extends/config-layer
   path keeps its D13 fresh-pull tolerance but still rejects a declared-but-
   mismatched digest. Test: `TestOCIFetcherRejectsMissingLayerDescriptorDigest`
   (supersedes the old fail-open `computes-digest-when-omits` assertion).

2. **HIGH — config-layer cache-provenance confused deputy closed**
   (fetcher_oci_layer.go + readCachedPinnedOCIBlob). The layer path fell through
   to a plain read of the SAME packages cache the git/local/http `packages`
   fetchers write into, so an OCI `extends` pin to a non-OCI-seeded digest was
   trusted as a validated config layer with no manifest resolution. Now
   `readCachedPinnedOCIBlob` gates BOTH OCI kinds through OCI sidecars — artifacts
   via `readCachedOCIArtifact`, config layers via new `readCachedOCILayer`
   (requires an OCI-layer sidecar, mediaType == config-layer type). Fresh
   OCI-layer pulls seed the packages cache + layer sidecar (best-effort; the
   SHA-addressed config-layer cache stays the offline source of truth). A
   non-OCI-seeded blob is a MISS and re-resolves. Tests:
   `TestOCILayerFetcherCacheHitWithoutSidecarIsNotTrusted` (negative: missing and
   wrong-kind sidecar rejected; positive: correct sidecar trusts the same blob)
   and `TestOCILayerFetcherFreshPullSeedsOCISidecar` (end-to-end seed → pinned
   hit, exactly one pull).

Verification: `go test ./internal/config/... -race` green; platform + lifecycle
green; gofmt + vet clean. Per instruction, `verifyOCIPin`'s manifest-digest
fallback and the two LOW defense-in-depth items were left untouched (tracked
separately). Behavior note for cache tooling: fresh OCI-LAYER pulls now also
write `~/.agents/cache/packages/<digest>/{artifact.blob,oci-type.json}`
(mediaType=config-layer) in addition to the existing SHA-addressed config-layer
cache — content-addressed and gitignored.
