package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- oci layer fetcher (extends, config-distribution-model §15 D13) ---------

// TestOCILayerFetcherPullsLayer asserts an OCI-served layer blob carrying the
// config-layer media type resolves through Fetch into a FetchedLayer the
// resolver can merge, content-addressed by the registry digest.
func TestOCILayerFetcherPullsLayer(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"skills":["from-oci"]}`)
	digest := "sha256:" + sha256Hex(body)
	pulls := 0
	f := &ociLayerFetcher{puller: func(_ context.Context, ref ociRef, _ []byte) (ociBlob, error) {
		pulls++
		if ref.Registry != "reg.example" || ref.Repository != "base/org/base.json" {
			t.Fatalf("unexpected ref %+v", ref)
		}
		return ociBlob{Data: body, Digest: digest, MediaType: ociLayerMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example/base"}
	cacheDir := layerTarget("acme", "org/base.json")
	got, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got.Data) != string(body) || got.ResolvedSHA != digest || got.CacheHit {
		t.Fatalf("unexpected layer %+v", got)
	}
	if got.KeyInputs.OCIDigest != digest {
		t.Fatalf("key inputs = %+v, want oci digest %s", got.KeyInputs, digest)
	}
	// Persisted under the SHA-addressed config cache for offline serve.
	if _, ok := readCachedUnit(cacheDir, digest); !ok {
		t.Fatal("expected layer cached")
	}
	if pulls != 1 {
		t.Fatalf("pulls = %d", pulls)
	}
}

// TestOCILayerFetcherVersionPin asserts a pinned @version becomes a digest pin
// (the layer-ref → oci-ref adaptation) and that a matching pinned digest pull
// resolves.
func TestOCILayerFetcherVersionPin(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"skills":["pinned"]}`)
	digest := "sha256:" + sha256Hex(body)
	f := &ociLayerFetcher{puller: func(_ context.Context, ref ociRef, _ []byte) (ociBlob, error) {
		if ref.Digest != digest {
			t.Fatalf("ref.Digest = %q, want %q", ref.Digest, digest)
		}
		return ociBlob{Data: body, Digest: digest, MediaType: ociLayerMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	cacheDir := layerTarget("acme", "org/base.json")
	got, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json", Version: "pinned:" + digest}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.ResolvedSHA != digest {
		t.Fatalf("resolved sha = %q, want %q", got.ResolvedSHA, digest)
	}
}

// TestOCILayerFetcherRejectsArtifactMediaType is the core kind guard: an
// `extends` pull that resolves to the artifact-bundle media type must fail with
// a schema error so a packages bundle is never merged as a config layer.
func TestOCILayerFetcherRejectsArtifactMediaType(t *testing.T) {
	withPackagesCache(t)
	body := []byte("artifact-bundle-bytes")
	f := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: body, Digest: "sha256:" + sha256Hex(body), MediaType: ociArtifactMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	cacheDir := layerTarget("acme", "org/base.json")
	_, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, cacheDir)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema media-type error, got %v", err)
	}
}

// TestOCILayerFetcherPullError maps a transport failure to a transport-reason
// ImportError.
func TestOCILayerFetcherPullError(t *testing.T) {
	withPackagesCache(t)
	f := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{}, errors.New("registry down")
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	_, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerTarget("acme", "org/base.json"))
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("want transport error, got %v", err)
	}
}

// TestOCILayerFetcherParseError rejects a non-oci source url with a schema
// error before any pull.
func TestOCILayerFetcherParseError(t *testing.T) {
	withPackagesCache(t)
	f := &ociLayerFetcher{}
	_, err := f.Fetch(Source{Type: "oci", URL: "https://reg.example/x"}, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerTarget("acme", "org/base.json"))
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema parse error, got %v", err)
	}
}

// TestOCILayerFetcherEmptyMediaTypeTolerated asserts a registry that omits the
// descriptor media type still resolves an extends pull (the guard tolerates an
// empty served media type as the requested kind).
func TestOCILayerFetcherEmptyMediaTypeTolerated(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"skills":["no-media-type"]}`)
	f := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: body}, nil // registry omits digest and media type
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	got, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerTarget("acme", "org/base.json"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.ResolvedSHA != "sha256:"+sha256Hex(body) {
		t.Fatalf("computed digest = %q", got.ResolvedSHA)
	}
}

// TestOCILayerFetcherCacheWriteError surfaces a cache-write failure (the config
// cache parent is a regular file, blocking MkdirAll).
func TestOCILayerFetcherCacheWriteError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	// Create ~/.agents/cache as a regular file so MkdirAll of cache/config fails.
	if err := os.WriteFile(filepath.Join(home, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"skills":["x"]}`)
	f := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: body, Digest: "sha256:" + sha256Hex(body), MediaType: ociLayerMediaType}, nil
	}}
	_, err := f.Fetch(Source{Type: "oci", URL: "oci://reg.example"}, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerTarget("acme", "org/base.json"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

// TestOCILayerFetcherCacheHitWithoutSidecarIsNotTrusted is the round-3 item 2
// regression test (the config-layer mirror of
// TestOCIFetcherCacheHitWithoutSidecarIsNotTrusted): a blob seeded into the
// shared, digest-keyed packages cache by a NON-OCI `packages` fetcher
// (git/local/http) — carrying no OCI type sidecar — must NOT be trusted as a
// validated OCI config layer when an OCI `extends` ref is pinned to that
// digest. Without OCI provenance the pinned hit is a MISS and the manifest is
// re-resolved; a correct OCI-layer sidecar (as a fresh OCI-layer pull writes)
// makes the SAME blob a trusted cache hit — proving the gate keys on
// provenance, not on incidental bytes.
func TestOCILayerFetcherCacheHitWithoutSidecarIsNotTrusted(t *testing.T) {
	withPackagesCache(t)
	// A layer.json blob a git/http `packages` fetch already wrote to the shared
	// packages cache (no OCI sidecar).
	body := []byte(`{"skills":["seeded-by-non-oci-packages-fetch"]}`)
	digest := "sha256:" + sha256Hex(body)
	if err := writeCachedArtifact(digest, body); err != nil {
		t.Fatal(err)
	}
	cacheDir := layerTarget("acme", "org/base.json")
	parts := LayerRefParts{SourceID: "acme", LayerPath: "org/base.json", Version: "pinned:" + digest}
	src := Source{Type: "oci", URL: "oci://reg.example"}

	// Negative: no OCI sidecar -> the seeded blob is NOT trusted, so the fetch
	// falls through to a fresh pull. The unwired puller makes that surface as a
	// transport error rather than silently returning the foreign bytes.
	fMiss := &ociLayerFetcher{} // nil puller -> ociPull transport error on fall-through
	if _, err := fMiss.Fetch(src, parts, cacheDir); err == nil {
		t.Fatal("a sidecar-less seeded blob must not be trusted as an OCI config layer")
	}

	// An OCI sidecar recording the ARTIFACT type (wrong kind) must likewise not
	// be trusted for a layer pin.
	if err := writeOCITypeSidecar(digest, ociArtifactMediaType, ociArtifactMediaType); err != nil {
		t.Fatal(err)
	}
	if _, err := fMiss.Fetch(src, parts, cacheDir); err == nil {
		t.Fatal("a blob whose sidecar records the artifact type must not be trusted as a config layer")
	}

	// Positive: the correct OCI-layer sidecar makes the SAME cached blob a
	// trusted hit, served without pulling.
	if err := writeOCITypeSidecar(digest, "", ociLayerMediaType); err != nil {
		t.Fatal(err)
	}
	fHit := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		t.Fatal("a validated OCI-layer cache hit must not pull")
		return ociBlob{}, nil
	}}
	got, err := fHit.Fetch(src, parts, cacheDir)
	if err != nil {
		t.Fatalf("Fetch with valid OCI-layer sidecar: %v", err)
	}
	if !got.CacheHit || string(got.Data) != string(body) || got.ResolvedSHA != digest {
		t.Fatalf("expected a trusted layer cache hit for the seeded body, got %+v", got)
	}
}

// TestOCILayerFetcherFreshPullSeedsOCISidecar proves a fresh OCI-layer pull
// writes the OCI-layer type sidecar into the shared packages cache, so a
// subsequent digest-pinned OCI `extends` ref is served from the fast path WITH
// provenance (round-3 item 2, positive direction end-to-end).
func TestOCILayerFetcherFreshPullSeedsOCISidecar(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"skills":["from-fresh-oci-layer-pull"]}`)
	digest := "sha256:" + sha256Hex(body)
	pulls := 0
	f := &ociLayerFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		pulls++
		return ociBlob{Data: body, Digest: digest, MediaType: ociLayerMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	cacheDir := layerTarget("acme", "org/base.json")
	// Fresh pull at a tag: seeds the packages cache + OCI-layer sidecar.
	if _, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, cacheDir); err != nil {
		t.Fatalf("fresh Fetch: %v", err)
	}
	// A later pinned ref is now a validated cache hit (no second pull).
	got, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json", Version: "pinned:" + digest}, cacheDir)
	if err != nil {
		t.Fatalf("pinned Fetch: %v", err)
	}
	if !got.CacheHit {
		t.Fatalf("expected the pinned ref to be a validated cache hit, got %+v", got)
	}
	if pulls != 1 {
		t.Fatalf("expected exactly one pull (the fresh one), got %d", pulls)
	}
}
