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
	cacheDir := layerCacheDir("acme", "org/base.json")
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
	if _, ok := readCachedLayer(cacheDir, digest); !ok {
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
	cacheDir := layerCacheDir("acme", "org/base.json")
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
	cacheDir := layerCacheDir("acme", "org/base.json")
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
	_, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerCacheDir("acme", "org/base.json"))
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
	_, err := f.Fetch(Source{Type: "oci", URL: "https://reg.example/x"}, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerCacheDir("acme", "org/base.json"))
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
	got, err := f.Fetch(src, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerCacheDir("acme", "org/base.json"))
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
	_, err := f.Fetch(Source{Type: "oci", URL: "oci://reg.example"}, LayerRefParts{SourceID: "acme", LayerPath: "org/base.json"}, layerCacheDir("acme", "org/base.json"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}
