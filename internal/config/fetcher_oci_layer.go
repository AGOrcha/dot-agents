package config

import (
	"fmt"
)

// This file completes the source/kind orthogonality (config-distribution-model
// §15 D13): a config layer (`extends`) may be sourced from OCI exactly as an
// artifact (`packages`) is. ociLayerFetcher implements the extends-side Fetcher
// by reusing the shared OCI pull (pullOCIContent in fetcher_oci.go) and
// returning the pulled blob as the layer.json document — a normal FetchedLayer
// the resolver merges with no special-casing. The media-type guard requires the
// pulled blob to carry the config-layer media type (ociLayerMediaType), the
// mirror of the artifact fetcher's guard, so `kind` stays meaningful even though
// the source is now unrestricted.

// ociLayerFetcher fetches a config layer from an OCI source. It is the extends
// counterpart to ociFetcher (packages): both pull a single content-addressed
// blob over the same OCI plumbing, then diverge only on which media type they
// require and which cache/result shape they produce. A layer blob carries the
// layer.json document and resolves to a FetchedLayer.
type ociLayerFetcher struct {
	// puller is the shared OCI pull seam (test seam). Nil uses the real, not-yet-
	// wired registry pull, identical to ociFetcher.
	puller ociPuller
}

// ociLayerRefToOCIRef adapts a parsed extends ref to an ociRef. The layer-path
// is the repository and the optional @version pins a tag or digest, mirroring
// parseOCIRef for packages but sourced from a LayerRefParts (extends has an
// optional version where packages require one). An empty version leaves the ref
// tag/digest unset so the registry's default (e.g. "latest") applies.
func ociLayerRefToOCIRef(src Source, parts LayerRefParts) (ociRef, error) {
	pkg := PackageRefParts{
		SourceID:     parts.SourceID,
		ArtifactPath: parts.LayerPath,
		VersionSpec:  parts.Version,
	}
	return parseOCIRef(src, pkg)
}

// Fetch pulls the layer blob and returns it as a FetchedLayer (no force-refresh).
func (f *ociLayerFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	return f.FetchRefresh(src, parts, cacheDir, false)
}

// FetchRefresh pulls the config-layer blob over the shared OCI plumbing and
// returns it as a FetchedLayer. The pulled content is digest-addressed, so the
// content digest is the resolved SHA / cache key and the OCI cache-key default
// (config-distribution-model §7A.4) keys on the manifest digest. forceRefresh is
// accepted to satisfy the refreshingFetcher contract; the OCI pull is already
// content-addressed (a changed upstream yields a new digest), so it has no
// separate SHA-fast-path to bypass.
func (f *ociLayerFetcher) FetchRefresh(src Source, parts LayerRefParts, cacheDir string, _ bool) (FetchedLayer, error) {
	importRef := parts.SourceID + ":" + parts.LayerPath
	ref, err := ociLayerRefToOCIRef(src, parts)
	if err != nil {
		return FetchedLayer{}, &ImportError{Ref: importRef, SourceID: parts.SourceID, Reason: ReasonSchema, Err: err}
	}
	pulled, err := pullOCIContent(f.puller, src, ref, importRef, parts.SourceID, ociLayerMediaType)
	if err != nil {
		return FetchedLayer{}, err
	}
	// Seed the shared content-addressed packages cache with an OCI-layer type
	// sidecar (round-3 item 2) so a later digest-pinned OCI `extends` ref can be
	// served from pullOCIContent's fast path WITH OCI provenance — and, crucially,
	// so that path trusts ONLY blobs an OCI-layer pull actually validated. This is
	// best-effort: the SHA-addressed config-layer cache below is the source of
	// truth for offline layer serves, so a failure here only forgoes the fast-path
	// optimization, never correctness. Skipped on a cache hit (the entry, and its
	// sidecar, already exist and must not be rewritten with stale metadata).
	if !pulled.CacheHit {
		_ = writeCachedArtifact(pulled.Digest, pulled.Data)
		_ = writeOCITypeSidecar(pulled.Digest, "", ociLayerMediaType)
	}
	// Persist under the SHA-addressed config layer cache so the resolver's offline
	// serve and lockfile round-trip work like any other layer source.
	if err := writeCachedLayer(cacheDir, pulled.Digest, pulled.Data); err != nil {
		return FetchedLayer{}, fmt.Errorf("caching oci layer %s: %w", importRef, err)
	}
	return FetchedLayer{
		Data:        pulled.Data,
		ResolvedSHA: pulled.Digest,
		CacheHit:    pulled.CacheHit,
		KeyInputs:   CacheKeyInputs{OCIDigest: pulled.Digest},
	}, nil
}
