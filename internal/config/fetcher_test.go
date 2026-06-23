package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

// withPackagesCache points the tier-2 packages cache root (~/.agents/cache) at a
// temp dir for the duration of a test by overriding AGENTS_HOME, so artifact
// caching is hermetic and never touches the real cache.
func withPackagesCache(t *testing.T) {
	t.Helper()
	t.Setenv("AGENTS_HOME", t.TempDir())
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// --- signing posture stub --------------------------------------------------

func TestSigningPostureValid(t *testing.T) {
	for _, p := range []SigningPosture{PostureUnsigned, PostureOptional, PostureRequired} {
		if !p.Valid() {
			t.Errorf("posture %q should be valid", p)
		}
	}
	if SigningPosture("bogus").Valid() {
		t.Error("bogus posture should be invalid")
	}
}

func TestPostureFromSource(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want SigningPosture
	}{
		{name: "absent", auth: "", want: PostureUnsigned},
		{name: "empty object", auth: `{}`, want: PostureUnsigned},
		{name: "required", auth: `{"signing":"required"}`, want: PostureRequired},
		{name: "optional", auth: `{"signing":"optional"}`, want: PostureOptional},
		{name: "unsigned explicit", auth: `{"signing":"unsigned"}`, want: PostureUnsigned},
		{name: "unrecognized falls back", auth: `{"signing":"weird"}`, want: PostureUnsigned},
		{name: "non-string value", auth: `{"signing":3}`, want: PostureUnsigned},
		{name: "malformed json", auth: `{not json`, want: PostureUnsigned},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.auth != "" {
				raw = json.RawMessage(tc.auth)
			}
			if got := PostureFromSource(Source{Auth: raw}); got != tc.want {
				t.Fatalf("PostureFromSource(%q) = %q, want %q", tc.auth, got, tc.want)
			}
		})
	}
}

func TestAuthString(t *testing.T) {
	if got := authString(nil, "token"); got != "" {
		t.Fatalf("nil auth = %q", got)
	}
	if got := authString(json.RawMessage(`["not","obj"]`), "token"); got != "" {
		t.Fatalf("array auth = %q", got)
	}
	if got := authString(json.RawMessage(`{"token":"abc"}`), "token"); got != "abc" {
		t.Fatalf("token = %q, want abc", got)
	}
	if got := authString(json.RawMessage(`{"token":"abc"}`), "missing"); got != "" {
		t.Fatalf("missing key = %q", got)
	}
}

func TestVerifySignatureStub(t *testing.T) {
	// Unsigned/optional always pass; required without a verified signature fails.
	if err := verifySignature(PostureUnsigned, "sha256:x", false); err != nil {
		t.Fatalf("unsigned should pass: %v", err)
	}
	if err := verifySignature(PostureOptional, "sha256:x", false); err != nil {
		t.Fatalf("optional should pass: %v", err)
	}
	if err := verifySignature(PostureRequired, "sha256:x", true); err != nil {
		t.Fatalf("required+signed should pass: %v", err)
	}
	err := verifySignature(PostureRequired, "sha256:x", false)
	if err == nil {
		t.Fatal("required+unsigned should fail")
	}
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want ImportError reason=auth, got %v", err)
	}
}

// --- package ref parsing ---------------------------------------------------

func TestParsePackageRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    PackageRefParts
		wantErr bool
	}{
		{name: "tag", ref: "acme:skill/review@1.2.3", want: PackageRefParts{SourceID: "acme", ArtifactPath: "skill/review", VersionSpec: "1.2.3"}},
		{name: "range", ref: "acme:skill/review@^1.2", want: PackageRefParts{SourceID: "acme", ArtifactPath: "skill/review", VersionSpec: "^1.2"}},
		{name: "digest pin", ref: "acme:v/api@pinned:sha256:abc", want: PackageRefParts{SourceID: "acme", ArtifactPath: "v/api", VersionSpec: "pinned:sha256:abc"}},
		{name: "no colon", ref: "acmeskill@1", wantErr: true},
		{name: "empty source", ref: ":skill@1", wantErr: true},
		{name: "no version", ref: "acme:skill", wantErr: true},
		{name: "empty artifact", ref: "acme:@1.2", wantErr: true},
		{name: "empty version", ref: "acme:skill@", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePackageRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// --- package fetcher selection (tier constraint) ---------------------------

func TestSelectPackageFetcherAcceptsAllSourceTypes(t *testing.T) {
	// config-distribution-model §4 (relaxed) / §15 D3+D8: any source type may
	// serve any artifact kind. All four source types must select a fetcher.
	cases := map[string]any{
		"oci":   &ociFetcher{},
		"http":  &httpArtifactFetcher{},
		"git":   &gitArtifactFetcher{},
		"local": &localArtifactFetcher{},
	}
	for typ, want := range cases {
		got, err := SelectPackageFetcher(typ)
		if err != nil {
			t.Errorf("SelectPackageFetcher(%q) = %v, want fetcher", typ, err)
			continue
		}
		if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Errorf("SelectPackageFetcher(%q) = %T, want %T", typ, got, want)
		}
	}
	if _, err := SelectPackageFetcher("bogus"); err == nil {
		t.Error("SelectPackageFetcher(bogus) = nil, want unsupported error")
	}
}

// TestSelectFetcherExtendsStillRejectsOCI guards the remaining tier asymmetry:
// the unified artifact sourcing relaxation applies to packages/artifacts only.
// extends (the layer tier) must still reject oci (config-distribution-model §4).
func TestSelectFetcherExtendsStillRejectsOCI(t *testing.T) {
	if _, err := SelectFetcher("oci"); err == nil {
		t.Error("SelectFetcher(oci) = nil, want extends-tier rejection")
	}
	for _, typ := range []string{"git", "http", "local"} {
		if _, err := SelectFetcher(typ); err != nil {
			t.Errorf("SelectFetcher(%q) = %v, want fetcher", typ, err)
		}
	}
}

// --- oci ref parsing -------------------------------------------------------

func TestParseOCIRef(t *testing.T) {
	src := Source{Type: "oci", URL: "oci://registry.acme.internal/dot-agents"}
	ref, err := parseOCIRef(src, PackageRefParts{ArtifactPath: "skill/review", VersionSpec: "1.2.3"})
	if err != nil {
		t.Fatalf("parseOCIRef: %v", err)
	}
	if ref.Registry != "registry.acme.internal" || ref.Repository != "dot-agents/skill/review" || ref.Tag != "1.2.3" || ref.Digest != "" {
		t.Fatalf("unexpected ref %+v", ref)
	}

	// Digest-pinned version becomes a Digest, not a Tag.
	ref2, err := parseOCIRef(src, PackageRefParts{ArtifactPath: "v/api", VersionSpec: "pinned:sha256:abc"})
	if err != nil {
		t.Fatalf("parseOCIRef pin: %v", err)
	}
	if ref2.Digest != "sha256:abc" || ref2.Tag != "" {
		t.Fatalf("pin ref %+v", ref2)
	}

	// No base path: registry only.
	ref3, _ := parseOCIRef(Source{URL: "oci://reg.example"}, PackageRefParts{ArtifactPath: "a/b", VersionSpec: "1"})
	if ref3.Registry != "reg.example" || ref3.Repository != "a/b" {
		t.Fatalf("no-base ref %+v", ref3)
	}
}

func TestParseOCIRefErrors(t *testing.T) {
	if _, err := parseOCIRef(Source{URL: ""}, PackageRefParts{ArtifactPath: "a", VersionSpec: "1"}); err == nil {
		t.Fatal("empty url should error")
	}
	if _, err := parseOCIRef(Source{URL: "https://reg.example/x"}, PackageRefParts{ArtifactPath: "a", VersionSpec: "1"}); err == nil {
		t.Fatal("non-oci scheme should error")
	}
	if _, err := parseOCIRef(Source{URL: "oci:///"}, PackageRefParts{ArtifactPath: "a", VersionSpec: "1"}); err == nil {
		t.Fatal("missing registry host should error")
	}
}

func TestDigestFromVersionSpec(t *testing.T) {
	if d, ok := digestFromVersionSpec("pinned:sha256:abc"); !ok || d != "sha256:abc" {
		t.Fatalf("pinned digest = %q,%v", d, ok)
	}
	if _, ok := digestFromVersionSpec("1.2.3"); ok {
		t.Fatal("tag should not be a digest")
	}
	if _, ok := digestFromVersionSpec("pinned:md5:abc"); ok {
		t.Fatal("non-sha256 pin should not be a digest")
	}
}

func TestDigestDirAndPath(t *testing.T) {
	if got := digestDir("sha256:deadbeef"); got != "deadbeef" {
		t.Fatalf("digestDir prefixed = %q", got)
	}
	if got := digestDir("deadbeef"); got != "deadbeef" {
		t.Fatalf("digestDir bare = %q", got)
	}
	if !strings.HasSuffix(cachedArtifactPath("sha256:abc"), filepath.Join("abc", "artifact.blob")) {
		t.Fatalf("cachedArtifactPath = %q", cachedArtifactPath("sha256:abc"))
	}
}

// --- oci fetcher -----------------------------------------------------------

func TestOCIFetcherPullsAndCaches(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	digest := "sha256:" + sha256Hex(blob)
	pulls := 0
	f := &ociFetcher{puller: func(_ context.Context, ref ociRef, _ []byte) ([]byte, string, error) {
		pulls++
		if ref.Registry != "reg.example" {
			t.Fatalf("registry = %q", ref.Registry)
		}
		return blob, digest, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example/base"}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if string(got.Data) != string(blob) || got.Digest != digest || got.CacheHit {
		t.Fatalf("unexpected result %+v", got)
	}
	if got.Posture != PostureUnsigned {
		t.Fatalf("posture = %q", got.Posture)
	}
	// Cached on disk.
	if _, ok := readCachedArtifact(digest); !ok {
		t.Fatal("expected artifact cached")
	}
	if pulls != 1 {
		t.Fatalf("pulls = %d", pulls)
	}
}

func TestOCIFetcherDigestPinCacheHit(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-blob")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	pulled := false
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		pulled = true
		return nil, "", errors.New("should not pull")
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if !got.CacheHit || got.Digest != digest {
		t.Fatalf("expected cache hit, got %+v", got)
	}
	if pulled {
		t.Fatal("pinned cache hit must not pull")
	}
}

func TestOCIFetcherDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("served")
	served := "sha256:" + sha256Hex(blob)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		return blob, served, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:sha256:deadbeef"})
	if err == nil {
		t.Fatal("expected digest-mismatch error")
	}
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error, got %v", err)
	}
}

func TestOCIFetcherComputesDigestWhenRegistryOmits(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("no-digest-from-reg")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		return blob, "", nil // registry omits digest
	}}
	got, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Digest != "sha256:"+sha256Hex(blob) {
		t.Fatalf("computed digest = %q", got.Digest)
	}
}

func TestOCIFetcherPullError(t *testing.T) {
	withPackagesCache(t)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		return nil, "", errors.New("registry down")
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("want transport error, got %v", err)
	}
}

func TestOCIFetcherParseError(t *testing.T) {
	withPackagesCache(t)
	f := &ociFetcher{}
	_, err := f.FetchArtifact(Source{URL: "https://reg.example/x"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error, got %v", err)
	}
}

func TestOCIFetcherRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("blob")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		return blob, "sha256:" + sha256Hex(blob), nil
	}}
	src := Source{URL: "oci://reg.example", Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error for required posture, got %v", err)
	}
}

func TestOCIFetcherRequiredPostureCacheHitFails(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinblob")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	f := &ociFetcher{}
	src := Source{URL: "oci://reg.example", Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:" + digest})
	if err == nil {
		t.Fatal("required posture must fail even on cache hit")
	}
}

func TestOCIFetcherCacheWriteError(t *testing.T) {
	// Point AGENTS_HOME at a path whose cache parent is a regular file so
	// writeCachedArtifact's MkdirAll fails.
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	// Create ~/.agents/cache as a file to block MkdirAll of cache/packages.
	if err := os.WriteFile(filepath.Join(home, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := []byte("b")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) ([]byte, string, error) {
		return blob, "sha256:" + sha256Hex(blob), nil
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestOCIPullNotWired(t *testing.T) {
	// The default (real) puller is a deterministic transport error in p5.
	_, _, err := ociPull(context.Background(), ociRef{Registry: "r", Repository: "x"}, nil)
	if err == nil {
		t.Fatal("expected not-wired error from default oci puller")
	}
}

func TestOCIFetcherDefaultPullerErrors(t *testing.T) {
	withPackagesCache(t)
	f := &ociFetcher{} // nil puller -> ociPull -> transport error
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected error from unwired default puller")
	}
}

// --- http artifact fetcher -------------------------------------------------

func TestHTTPArtifactFetcherPullsAndCaches(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("http-artifact")
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/skill/x/1.0" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	f := &httpArtifactFetcher{client: srv.Client()}
	src := Source{Type: "http", URL: srv.URL, Auth: json.RawMessage(`{"token":"secret"}`)}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if string(got.Data) != string(blob) || got.CacheHit {
		t.Fatalf("result %+v", got)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if _, ok := readCachedArtifact(got.Digest); !ok {
		t.Fatal("expected cached")
	}
}

func TestHTTPArtifactFetcherRejectsNonHTTPS(t *testing.T) {
	f := &httpArtifactFetcher{}
	_, err := f.FetchArtifact(Source{URL: "http://insecure.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error, got %v", err)
	}
}

func TestHTTPArtifactFetcherStatusCodes(t *testing.T) {
	cases := []struct {
		status int
		reason ImportFailReason
	}{
		{http.StatusUnauthorized, ReasonAuth},
		{http.StatusForbidden, ReasonAuth},
		{http.StatusNotFound, ReasonNotFound},
		{http.StatusInternalServerError, ReasonTransport},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			withPackagesCache(t)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			f := &httpArtifactFetcher{client: srv.Client()}
			_, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
			var ie *ImportError
			if !errors.As(err, &ie) || ie.Reason != tc.reason {
				t.Fatalf("status %d: want reason %s, got %v", tc.status, tc.reason, err)
			}
		})
	}
}

func TestHTTPArtifactFetcherDigestPinCacheHit(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-http")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	// Server would 500 if hit; cache hit must short-circuit before any request.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := &httpArtifactFetcher{client: srv.Client()}
	got, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if !got.CacheHit {
		t.Fatal("expected pinned cache hit")
	}
}

func TestHTTPArtifactFetcherDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("served-bytes")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	f := &httpArtifactFetcher{client: srv.Client()}
	_, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:sha256:deadbeef"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error, got %v", err)
	}
}

func TestHTTPArtifactFetcherRequiredPosture(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("blob")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	f := &httpArtifactFetcher{client: srv.Client()}
	src := Source{URL: srv.URL, Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error, got %v", err)
	}
}

func TestHTTPArtifactFetcherRequestBuildError(t *testing.T) {
	// A control char in the URL passes the https:// prefix check but makes
	// http.NewRequestWithContext fail (the request-build error branch).
	f := &httpArtifactFetcher{}
	src := Source{URL: "https://reg.example/\x7f"}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("want transport error from request build, got %v", err)
	}
	if !strings.Contains(err.Error(), "building request") {
		t.Fatalf("expected request-build error, got %v", err)
	}
}

func TestHTTPArtifactFetcherTransportError(t *testing.T) {
	f := &httpArtifactFetcher{client: &http.Client{Transport: errRoundTripper{}}}
	_, err := f.FetchArtifact(Source{URL: "https://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("want transport error, got %v", err)
	}
}

func TestHTTPArtifactFetcherCacheWriteError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob := []byte("b")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer srv.Close()
	f := &httpArtifactFetcher{client: srv.Client()}
	_, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestArtifactURL(t *testing.T) {
	got := artifactURL(Source{URL: "https://reg.example/base/"}, PackageRefParts{ArtifactPath: "/skill/x/", VersionSpec: "/1.0"})
	if got != "https://reg.example/base/skill/x/1.0" {
		t.Fatalf("artifactURL = %q", got)
	}
}

func TestParseLayerRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		want    LayerRefParts
		wantErr bool
	}{
		{name: "bare", ref: "acme:org/base", want: LayerRefParts{SourceID: "acme", LayerPath: "org/base"}},
		{name: "with version", ref: "acme:org/base@v1.2.3", want: LayerRefParts{SourceID: "acme", LayerPath: "org/base", Version: "v1.2.3"}},
		{name: "version with at in sha", ref: "acme:team/frontend@abc123", want: LayerRefParts{SourceID: "acme", LayerPath: "team/frontend", Version: "abc123"}},
		{name: "no colon", ref: "acmeorgbase", wantErr: true},
		{name: "empty source", ref: ":org/base", wantErr: true},
		{name: "empty layer path", ref: "acme:", wantErr: true},
		{name: "empty layer path with version", ref: "acme:@v1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLayerRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSelectFetcherTierConstraint(t *testing.T) {
	for _, typ := range []string{"git", "http", "local"} {
		if _, err := SelectFetcher(typ); err != nil {
			t.Errorf("SelectFetcher(%q) = error %v, want fetcher", typ, err)
		}
	}
	if _, err := SelectFetcher("oci"); err == nil {
		t.Error("SelectFetcher(\"oci\") = nil error, want schema rejection (oci is packages-only)")
	}
	if _, err := SelectFetcher("bogus"); err == nil {
		t.Error("SelectFetcher(\"bogus\") = nil error, want unsupported-type error")
	}
}

func TestLocalFetcher(t *testing.T) {
	srcDir := t.TempDir()
	layerPath := "org/base.json"
	full := filepath.Join(srcDir, "org", "base.json")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"skills":["x"]}`)
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")
	f := &localFetcher{}
	got, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: layerPath}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q, want %q", got.Data, body)
	}
	if got.ResolvedSHA != contentHash(body) {
		t.Fatalf("sha = %q, want content hash", got.ResolvedSHA)
	}
	// Cache file is written content-addressed by SHA.
	if _, ok := readCachedLayer(cacheDir, got.ResolvedSHA); !ok {
		t.Fatal("expected layer written to cache")
	}
}

func TestLocalFetcherMissing(t *testing.T) {
	f := &localFetcher{}
	_, err := f.Fetch(Source{Type: "local", Path: t.TempDir()}, LayerRefParts{LayerPath: "nope.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing local layer")
	}
}

func TestHTTPFetcher(t *testing.T) {
	body := `{"rules":["r"]}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/base.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	cacheDir := filepath.Join(t.TempDir(), "cache")
	f := &httpFetcher{client: srv.Client()}
	got, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got.Data) != body {
		t.Fatalf("data = %q, want %q", got.Data, body)
	}
	if got.CacheHit {
		t.Fatal("first fetch should not be a cache hit")
	}

	// Second fetch with same content hash hits the cache.
	got2, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch (2nd): %v", err)
	}
	if !got2.CacheHit {
		t.Fatal("second fetch should be a cache hit")
	}
}

func TestHTTPFetcherRejectsNonHTTPS(t *testing.T) {
	f := &httpFetcher{}
	_, err := f.Fetch(Source{Type: "http", URL: "http://insecure.example/"}, LayerRefParts{LayerPath: "x.json"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https-enforcement error, got %v", err)
	}
}

func TestHTTPFetcherNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	f := &httpFetcher{client: srv.Client()}
	_, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "x.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

// makeGitFixtureAt inits a real on-disk git repo at dir, commits a single layer
// file at layerPath with body, and returns the branch name and committed SHA.
func makeGitFixtureAt(t *testing.T, dir, layerPath string, body []byte) (branch, sha string) {
	t.Helper()
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	full := filepath.Join(dir, filepath.FromSlash(layerPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatalf("write fixture layer: %v", err)
	}
	if _, err := wt.Add(layerPath); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, err := wt.Commit("add layer", &gogit.CommitOptions{
		Author: &object.Signature{Name: "t", Email: "t@example"},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return head.Name().Short(), h.String()
}

// makeGitFixture inits a real on-disk git repo, commits a single layer file, and
// returns the repo's file:// URL plus the branch name and committed SHA. This
// exercises the real go-git clone-and-read path hermetically (no network, no git
// binary).
func makeGitFixture(t *testing.T, layerPath string, body []byte) (url, branch, sha string) {
	t.Helper()
	dir := t.TempDir()
	branch, sha = makeGitFixtureAt(t, dir, layerPath, body)
	return "file://" + dir, branch, sha
}

func TestGitFetcherClonesAndCaches(t *testing.T) {
	body := []byte(`{"agents":["claude"]}`)
	url, branch, wantSHA := makeGitFixture(t, "org/base.json", body)

	cacheDir := filepath.Join(t.TempDir(), "cache")
	f := &gitFetcher{}
	got, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.ResolvedSHA != wantSHA {
		t.Fatalf("sha = %q, want %q", got.ResolvedSHA, wantSHA)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q, want %q", got.Data, body)
	}
	if got.CacheHit {
		t.Fatal("first fetch should not be a cache hit")
	}

	// Second fetch resolves the same SHA and serves the layer from cache.
	got2, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch (2nd): %v", err)
	}
	if !got2.CacheHit {
		t.Fatal("second fetch should hit the SHA cache")
	}
	if got2.ResolvedSHA != wantSHA {
		t.Fatalf("2nd sha = %q, want %q", got2.ResolvedSHA, wantSHA)
	}
}

func TestGitFetcherDefaultsRefToSourceRefThenMain(t *testing.T) {
	body := []byte(`{"x":1}`)
	url, branch, _ := makeGitFixture(t, "base.json", body)
	// Source.Ref empty + parts.Version empty -> falls back to the source ref
	// (here we pass it via Source.Ref to exercise that branch), and a fixture
	// whose default branch is "main"/"master" exercises the final fallback when
	// both are empty only if the fixture branch matches; assert the source-ref
	// branch resolves regardless.
	f := &gitFetcher{}
	got, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "base.json"}, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q", got.Data)
	}
}

func TestGitFetcherBadURL(t *testing.T) {
	f := &gitFetcher{}
	// A URL that transport.ParseURL rejects outright (control char) hits the
	// gitremote.ParseRemoteURL hard-error branch before any clone.
	_, err := f.Fetch(Source{Type: "git", URL: "ht!tp://%zz"}, LayerRefParts{LayerPath: "x.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for malformed git url")
	}
}

func TestGitFetcherMissingRef(t *testing.T) {
	url, _, _ := makeGitFixture(t, "base.json", []byte("{}"))
	f := &gitFetcher{}
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: "no-such-branch"}, LayerRefParts{LayerPath: "base.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error cloning a non-existent ref")
	}
}

func TestGitFetcherMissingLayerPath(t *testing.T) {
	url, branch, _ := makeGitFixture(t, "base.json", []byte("{}"))
	f := &gitFetcher{}
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "nope.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error reading a missing layer path from the clone")
	}
}

func TestGitFetcherOversizedLayer(t *testing.T) {
	// A layer file larger than maxLayerBytes must be rejected by readAllLimited.
	big := make([]byte, maxLayerBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	url, branch, _ := makeGitFixture(t, "big.json", big)
	f := &gitFetcher{}
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "big.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for oversized layer")
	}
}

// committedRepoFS opens the on-disk fixture at dir and returns its repository
// (which has a valid HEAD) paired with an independent memfs the test populates.
// This lets the fakeCloner exercise HEAD resolution and worktree-Open branches
// without depending on go-git's WithWorkTree init option.
func committedRepoFS(t *testing.T, dir string, files map[string][]byte) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.PlainOpen(dir)
		if err != nil {
			return nil, nil, err
		}
		fs := memfs.New()
		for name, body := range files {
			fh, err := fs.Create(name)
			if err != nil {
				return nil, nil, err
			}
			if _, err := fh.Write(body); err != nil {
				return nil, nil, err
			}
			if err := fh.Close(); err != nil {
				return nil, nil, err
			}
		}
		return repo, fs, nil
	}
}

// emptyRepoCloner returns a freshly-initialized in-memory repo (no commits, so
// Head() errors) and an empty memfs.
func emptyRepoCloner() func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.Init(memory.NewStorage())
		if err != nil {
			return nil, nil, err
		}
		return repo, memfs.New(), nil
	}
}

func TestGitFetcherHeadError(t *testing.T) {
	// Cloner returns a repo with no commits -> repo.Head() errors.
	f := &gitFetcher{cloner: emptyRepoCloner()}
	_, err := f.Fetch(Source{Type: "git", URL: "file:///x", Ref: "main"}, LayerRefParts{LayerPath: "base.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error when HEAD cannot be resolved")
	}
}

func TestGitFetcherCloneError(t *testing.T) {
	f := &gitFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		return nil, nil, errors.New("clone boom")
	}}
	_, err := f.Fetch(Source{Type: "git", URL: "file:///x", Ref: "main"}, LayerRefParts{LayerPath: "base.json"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("expected wrapped clone error, got %v", err)
	}
}

func TestGitFullRef(t *testing.T) {
	if got := gitFullRef("main"); got != "refs/heads/main" {
		t.Fatalf("gitFullRef(main) = %q", got)
	}
	if got := gitFullRef("refs/tags/v1"); got != "refs/tags/v1" {
		t.Fatalf("gitFullRef(refs/tags/v1) = %q", got)
	}
}

func TestWriteCachedLayerMkdirError(t *testing.T) {
	// Point the cache dir at a path whose parent is a regular file so MkdirAll
	// fails, covering writeCachedLayer's error branch.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedLayer(filepath.Join(f, "sub"), "sha", []byte("{}")); err == nil {
		t.Fatal("expected mkdir error when parent is a file")
	}
}

func TestLocalFetcherCacheWriteError(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "x.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cacheDir's parent is a regular file -> writeCachedLayer fails.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localFetcher{}
	_, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "x.json"}, filepath.Join(blocker, "cache"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestHTTPFetcherCacheWriteError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &httpFetcher{client: srv.Client()}
	_, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "x.json"}, filepath.Join(blocker, "cache"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestGitFetcherCacheWriteError(t *testing.T) {
	// Cloner returns a committed repo (valid HEAD) plus a memfs holding the
	// layer, but the cache dir's parent is a regular file so writeCachedLayer
	// fails after a successful read.
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "x.json", []byte("{}"))
	f := &gitFetcher{cloner: committedRepoFS(t, dir, map[string][]byte{"x.json": []byte("{}")})}
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.Fetch(Source{Type: "git", URL: "file:///r", Ref: "main"}, LayerRefParts{LayerPath: "x.json"}, filepath.Join(blocker, "cache"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestGitFetcherReadError(t *testing.T) {
	// Cloner returns a committed repo whose worktree memfs lacks the requested
	// layer path -> Open fails (the read-error branch).
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "x.json", []byte("{}"))
	f := &gitFetcher{cloner: committedRepoFS(t, dir, nil)}
	_, err := f.Fetch(Source{Type: "git", URL: "file:///r", Ref: "main"}, LayerRefParts{LayerPath: "missing.json"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git read") {
		t.Fatalf("expected git read error, got %v", err)
	}
}

// --- cache_keys consumption (cl-cache-keys-consume) ------------------------
//
// These tests prove cache_keys / the per-kind cache-key default is no longer a
// silent no-op: the fetchers now surface the facts the key derives from, and the
// resolver records EffectiveCacheKey in the lock and lets a force escape change
// the offline-serve decision.

// TestHTTPFetcherCapturesCacheKeyValidators proves the http layer fetcher now
// surfaces the upstream ETag / Last-Modified validators (and the content digest)
// that the §7A.4 http cache-key default keys on — previously dropped on the floor.
func TestHTTPFetcherCapturesCacheKeyValidators(t *testing.T) {
	body := `{"rules":["r"]}`
	const etag = `"abc123"`
	const lastMod = "Wed, 21 Oct 2026 07:28:00 GMT"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", lastMod)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := &httpFetcher{client: srv.Client()}
	got, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.KeyInputs.ETag != etag {
		t.Errorf("KeyInputs.ETag = %q, want %q", got.KeyInputs.ETag, etag)
	}
	if got.KeyInputs.LastModified != lastMod {
		t.Errorf("KeyInputs.LastModified = %q, want %q", got.KeyInputs.LastModified, lastMod)
	}
	if got.KeyInputs.ContentDigest != contentHash([]byte(body)) {
		t.Errorf("KeyInputs.ContentDigest = %q, want content hash", got.KeyInputs.ContentDigest)
	}
	// The default http key prefers the ETag validator (strong validator wins).
	if k := DefaultCacheKey(SourceKindHTTP, got.KeyInputs); k != cacheKeyPrefix+"http:etag="+etag {
		t.Errorf("DefaultCacheKey = %q, want etag-keyed", k)
	}
}

// TestLocalFetcherCapturesWorktreeCacheKey proves the local fetcher marks the
// worktree dirty and supplies its content hash, so a local source's effective
// key tracks uncommitted authoring (§7A.4 / D6) instead of the kind default
// collapsing to nothing.
func TestLocalFetcherCapturesWorktreeCacheKey(t *testing.T) {
	srcDir := t.TempDir()
	full := filepath.Join(srcDir, "base.json")
	body := []byte(`{"skills":["x"]}`)
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localFetcher{}
	got, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "base.json"}, filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !got.KeyInputs.WorktreeDirty {
		t.Error("local fetch should mark worktree dirty")
	}
	if got.KeyInputs.WorktreeContentHash != contentHash(body) {
		t.Errorf("WorktreeContentHash = %q, want content hash", got.KeyInputs.WorktreeContentHash)
	}
	// Editing the working tree changes the local default key (negative -> positive).
	keyA := DefaultCacheKey(SourceKindLocal, got.KeyInputs)
	if err := os.WriteFile(full, []byte(`{"skills":["x","y"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got2, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "base.json"}, filepath.Join(t.TempDir(), "cache2"))
	if err != nil {
		t.Fatalf("Fetch (2nd): %v", err)
	}
	if keyA == DefaultCacheKey(SourceKindLocal, got2.KeyInputs) {
		t.Errorf("local key did not change after worktree edit: both %q", keyA)
	}
}

// TestOCIFetcherCapturesDigestCacheKey proves the oci fetcher surfaces the
// manifest digest as the cache-key fact, so the content-addressed oci default
// keys on it.
func TestOCIFetcherCapturesDigestCacheKey(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	digest := artifactDigest(blob)
	f := &ociFetcher{puller: func(_ context.Context, _ ociRef, _ []byte) ([]byte, string, error) {
		return blob, digest, nil
	}}
	got, err := f.FetchArtifact(Source{Type: "oci", URL: "oci://reg.test/base"}, PackageRefParts{SourceID: "acme", ArtifactPath: "skill/x", VersionSpec: "1.0.0"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.KeyInputs.OCIDigest != digest {
		t.Errorf("KeyInputs.OCIDigest = %q, want %q", got.KeyInputs.OCIDigest, digest)
	}
	if k := DefaultCacheKey(SourceKindOCI, got.KeyInputs); k != cacheKeyPrefix+"oci:"+digest {
		t.Errorf("DefaultCacheKey = %q, want digest-keyed", k)
	}
}

// TestLayeredResolverRecordsCacheKeyInLock proves the resolver derives and
// records EffectiveCacheKey in the lock — the primitive is consumed, not inert.
func TestLayeredResolverRecordsCacheKeyInLock(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["from-git"]}`},
		sha:   "deadbeefcafe0000000000000000000000000000",
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := locked["acme:org/base.json"].CacheKey
	// The git default keys on the resolved commit the fake reported.
	want := cacheKeyPrefix + "git:" + fake.sha
	if got != want {
		t.Errorf("recorded cache_key = %q, want %q", got, want)
	}
}

// TestLayeredResolverAlwaysRevalidateRecordsSentinel proves a source declaring
// cache_keys.always_revalidate records the AlwaysRevalidate sentinel instead of
// the kind default — a non-default cache_keys demonstrably changes the lock.
func TestLayeredResolverAlwaysRevalidateRecordsSentinel(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main", "cache_keys": {"always_revalidate": true}}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["from-git"]}`},
		sha:   "deadbeefcafe0000000000000000000000000000",
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := locked["acme:org/base.json"].CacheKey; got != AlwaysRevalidate {
		t.Errorf("recorded cache_key = %q, want AlwaysRevalidate sentinel %q", got, AlwaysRevalidate)
	}
}

// TestLayeredResolverCacheKeyOffline drives the headline behavior change: with
// the same lock + cache, a default source serves offline (negative: cache_keys
// inert path) while always_revalidate / --refresh force a revalidation failure
// offline (positive: cache_keys now changes behavior).
func TestLayeredResolverCacheKeyOffline(t *testing.T) {
	seed := func(t *testing.T, manifest string) (repo string, sha string) {
		t.Helper()
		t.Setenv("AGENTS_HOME", t.TempDir())
		repo = t.TempDir()
		writeManifest(t, repo, manifest)
		fake := &fakeFetcher{
			files: map[string]string{"org/base.json": `{"skills":["online"]}`},
			sha:   "feedface000000000000000000000000000000aa",
		}
		if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
			t.Fatalf("online seed Resolve: %v", err)
		}
		return repo, fake.sha
	}

	const defaultManifest = `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main"}],
		"extends": ["acme:org/base.json"]
	}`
	const revalManifest = `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main", "cache_keys": {"always_revalidate": true}}],
		"extends": ["acme:org/base.json"]
	}`

	// Negative: default cache_keys -> offline serves the cached layer.
	t.Run("default serves offline", func(t *testing.T) {
		repo, _ := seed(t, defaultManifest)
		offline := &fakeFetcher{fetchErr: errors.New("network down")}
		snap, err := NewLayeredResolver().WithFetcher("git", offline).WithOffline(true).Resolve(repo)
		if err != nil {
			t.Fatalf("offline Resolve: %v", err)
		}
		if offline.calls != 0 {
			t.Errorf("offline called fetcher %d times, want 0", offline.calls)
		}
		if !hasWarning(snap.Warnings, "acme:org/base.json", "cache_hit_offline") {
			t.Errorf("expected cache_hit_offline warning, got %+v", snap.Warnings)
		}
	})

	// Positive: always_revalidate -> offline refuses to serve and fails loudly.
	t.Run("always_revalidate blocks offline serve", func(t *testing.T) {
		repo, _ := seed(t, revalManifest)
		offline := &fakeFetcher{fetchErr: errors.New("network down")}
		_, err := NewLayeredResolver().WithFetcher("git", offline).WithOffline(true).Resolve(repo)
		if err == nil || !strings.Contains(err.Error(), "revalidation required") {
			t.Fatalf("expected offline revalidation-required error, got %v", err)
		}
	})

	// Positive: --refresh force escape blocks offline serve even with a default
	// source (the runtime half of the R6 force escape).
	t.Run("refresh blocks offline serve", func(t *testing.T) {
		repo, _ := seed(t, defaultManifest)
		offline := &fakeFetcher{fetchErr: errors.New("network down")}
		_, err := NewLayeredResolver().WithFetcher("git", offline).WithOffline(true).WithRefresh(true).Resolve(repo)
		if err == nil || !strings.Contains(err.Error(), "revalidation required") {
			t.Fatalf("expected --refresh offline revalidation error, got %v", err)
		}
	})
}

// refreshFakeFetcher is a refresh-aware Fetcher that records whether the resolver
// asked it to bypass its cache (forceRefresh). It serves a cached SHA on a
// repeat fetch unless forceRefresh is set, mirroring the real git/http fetchers,
// so a test can assert the online resolve path consults the cache key.
type refreshFakeFetcher struct {
	files       map[string]string
	sha         string
	calls       int
	refreshSeen []bool // forceRefresh value per call
}

func (f *refreshFakeFetcher) Fetch(src Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	return f.FetchRefresh(src, parts, cacheDir, false)
}

func (f *refreshFakeFetcher) FetchRefresh(_ Source, parts LayerRefParts, cacheDir string, forceRefresh bool) (FetchedLayer, error) {
	f.calls++
	f.refreshSeen = append(f.refreshSeen, forceRefresh)
	body, ok := f.files[parts.LayerPath]
	if !ok {
		return FetchedLayer{}, errors.New("fake: no such layer " + parts.LayerPath)
	}
	sha := f.sha
	if sha == "" {
		sha = contentHash([]byte(body))
	}
	if !forceRefresh {
		if cached, ok := readCachedLayer(cacheDir, sha); ok {
			return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}}, nil
		}
	}
	if err := writeCachedLayer(cacheDir, sha, []byte(body)); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: []byte(body), ResolvedSHA: sha, CacheHit: false, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}}, nil
}

// cacheKeyManifest is a git source manifest with the given cache_keys JSON
// fragment spliced into the source ("" for none), used to drive the online
// re-resolve cases below from a table without repeating the JSON literal.
func cacheKeyManifest(cacheKeys string) string {
	src := `{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main"`
	if cacheKeys != "" {
		src += `, "cache_keys": ` + cacheKeys
	}
	src += `}`
	return `{"version": 2, "sources": [` + src + `], "extends": ["acme:org/base.json"]}`
}

// newCacheKeyFetcher returns a refresh-aware fake whose resolved SHA is fixed, so
// a re-resolve derives the same kind-default key and only a force escape / an
// override edit can make the recorded cache key stale.
func newCacheKeyFetcher() *refreshFakeFetcher {
	return &refreshFakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "feedface000000000000000000000000000000aa",
	}
}

// resolveOnce writes the manifest, resolves once with a fresh fake fetcher, and
// returns the forceRefresh flag the fetcher observed on its single call.
func resolveOnce(t *testing.T, repo, manifest string, refresh bool) bool {
	t.Helper()
	writeManifest(t, repo, manifest)
	fake := newCacheKeyFetcher()
	if _, err := NewLayeredResolver().WithFetcher("git", fake).WithRefresh(refresh).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(fake.refreshSeen) != 1 {
		t.Fatalf("fetcher calls = %d, want 1 (refreshSeen=%v)", len(fake.refreshSeen), fake.refreshSeen)
	}
	return fake.refreshSeen[0]
}

// TestLayeredResolverCacheKeyForcesOnlineRefetch is the headline online behavior
// change: with a prior lock + cache, a default source serves the cache on the
// second online resolve (negative: cache_keys inert path), while a force escape
// (always_revalidate or --refresh) or a cache_keys override edit forces the
// fetcher to bypass its cache and re-validate upstream (positive: cache_keys now
// changes the online resolve). Reverting the fetchLayer wiring flips every
// positive case's observed forceRefresh to false, so the test fails.
func TestLayeredResolverCacheKeyForcesOnlineRefetch(t *testing.T) {
	cases := []struct {
		name      string
		seed      string // manifest the first (lock-recording) resolve uses
		reresolve string // manifest the second resolve uses (== seed unless an edit)
		refresh   bool   // --refresh force escape on the second resolve
		want      bool   // forceRefresh the fetcher must observe on the second resolve
	}{
		{
			name: "default serves cache on re-resolve",
			seed: cacheKeyManifest(""),
			want: false, // kind-default key still matches the recorded key
		},
		{
			name: "always_revalidate forces refresh",
			seed: cacheKeyManifest(`{"always_revalidate": true}`),
			want: true, // sentinel never matches the recorded key
		},
		{
			name:      "override edit forces refresh",
			seed:      cacheKeyManifest(""),
			reresolve: cacheKeyManifest(`{"env": ["MY_TOKEN"]}`),
			want:      true, // adding an {env} selector changes the key shape
		},
		{
			name:    "refresh flag forces refresh",
			seed:    cacheKeyManifest(""),
			refresh: true, // runtime half of the R6 force escape
			want:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENTS_HOME", t.TempDir())
			repo := t.TempDir()
			// First resolve records the lock + cache (forceRefresh always false:
			// no prior lock to compare against).
			resolveOnce(t, repo, tc.seed, false)
			reresolve := tc.reresolve
			if reresolve == "" {
				reresolve = tc.seed
			}
			if got := resolveOnce(t, repo, reresolve, tc.refresh); got != tc.want {
				t.Fatalf("re-resolve forceRefresh = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGitFetcherRefreshBypassesCache proves the real git fetcher's FetchRefresh
// bypasses the SHA-addressed cache serve when forceRefresh is set: a cached layer
// whose on-disk bytes differ from the worktree is re-read on a forced refresh and
// served from cache otherwise.
func TestGitFetcherRefreshBypassesCache(t *testing.T) {
	fresh := []byte(`{"skills":["fresh"]}`)
	url, branch, sha := makeGitFixture(t, "org/base.json", fresh)
	f := &gitFetcher{}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	src := Source{Type: "git", URL: url, Ref: branch}
	parts := LayerRefParts{LayerPath: "org/base.json"}
	// Pre-seed the cache at the resolved SHA with STALE bytes the fetcher would
	// serve on a normal fetch.
	stale := []byte(`{"skills":["stale"]}`)
	if err := writeCachedLayer(cacheDir, sha, stale); err != nil {
		t.Fatal(err)
	}

	// Without forceRefresh: serves the cached (stale) bytes.
	got, err := f.FetchRefresh(src, parts, cacheDir, false)
	if err != nil {
		t.Fatalf("FetchRefresh(false): %v", err)
	}
	if !got.CacheHit || string(got.Data) != string(stale) {
		t.Fatalf("FetchRefresh(false) = hit %v data %q, want cached stale bytes", got.CacheHit, got.Data)
	}

	// With forceRefresh: bypasses the cache and re-reads the fresh worktree bytes.
	got2, err := f.FetchRefresh(src, parts, cacheDir, true)
	if err != nil {
		t.Fatalf("FetchRefresh(true): %v", err)
	}
	if got2.CacheHit || string(got2.Data) != string(fresh) {
		t.Fatalf("FetchRefresh(true) = hit %v data %q, want fresh bytes", got2.CacheHit, got2.Data)
	}
}

// TestHTTPFetcherRefreshBypassesCache proves the real http layer fetcher's
// FetchRefresh rewrites the cache from the upstream response when forceRefresh is
// set, instead of returning the cached-SHA fast path.
func TestHTTPFetcherRefreshBypassesCache(t *testing.T) {
	body := `{"rules":["r"]}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	f := &httpFetcher{client: srv.Client()}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	sha := contentHash([]byte(body))
	if err := writeCachedLayer(cacheDir, sha, []byte(body)); err != nil {
		t.Fatal(err)
	}

	// Without forceRefresh: the matching SHA serves from cache (CacheHit true).
	got, err := f.FetchRefresh(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir, false)
	if err != nil {
		t.Fatalf("FetchRefresh(false): %v", err)
	}
	if !got.CacheHit {
		t.Fatalf("FetchRefresh(false) CacheHit = false, want cache serve")
	}

	// With forceRefresh: bypasses the cache serve and reports a fresh fetch.
	got2, err := f.FetchRefresh(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir, true)
	if err != nil {
		t.Fatalf("FetchRefresh(true): %v", err)
	}
	if got2.CacheHit {
		t.Fatalf("FetchRefresh(true) CacheHit = true, want cache bypass")
	}
}

// TestFetchWithRefreshFallsBackToFetch proves fetchWithRefresh routes a plain
// (non-refresh-aware) Fetcher through Fetch and ignores the forceRefresh signal,
// so legacy fetchers and the cache-less local fetcher keep working.
func TestFetchWithRefreshFallsBackToFetch(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	plain := &fakeFetcher{files: map[string]string{"org/base.json": `{"skills":["x"]}`}, sha: "abc0000000000000000000000000000000000000"}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	got, err := fetchWithRefresh(plain, Source{Type: "git"}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir, true)
	if err != nil {
		t.Fatalf("fetchWithRefresh: %v", err)
	}
	if got.ResolvedSHA != plain.sha {
		t.Fatalf("ResolvedSHA = %q, want %q", got.ResolvedSHA, plain.sha)
	}
	if plain.calls != 1 {
		t.Fatalf("plain fetcher calls = %d, want 1", plain.calls)
	}
}

// --- git artifact fetcher --------------------------------------------------

func TestGitArtifactFetcherClonesAndCaches(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"skill":"review-pr"}`)
	url, branch, _ := makeGitFixture(t, "skill/review-pr.json", body)
	wantDigest := "sha256:" + sha256Hex(body)

	f := &gitArtifactFetcher{}
	got, err := f.FetchArtifact(Source{Type: "git", URL: url, Ref: branch}, PackageRefParts{SourceID: "acme", ArtifactPath: "skill/review-pr.json", VersionSpec: branch})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Digest != wantDigest {
		t.Fatalf("digest = %q, want %q", got.Digest, wantDigest)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q, want %q", got.Data, body)
	}
	if got.CacheHit {
		t.Fatal("first fetch should not be a cache hit")
	}
	if got.Posture != PostureUnsigned {
		t.Fatalf("posture = %q", got.Posture)
	}
	if got.KeyInputs.ResolvedCommit == "" {
		t.Fatal("expected resolved commit in key inputs")
	}
	if _, ok := readCachedArtifact(wantDigest); !ok {
		t.Fatal("expected artifact cached by digest")
	}
}

func TestGitArtifactFetcherDigestPinCacheHit(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-git-blob")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	cloned := false
	f := &gitArtifactFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		cloned = true
		return nil, nil, errors.New("should not clone")
	}}
	got, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if !got.CacheHit || got.Digest != digest {
		t.Fatalf("expected cache hit, got %+v", got)
	}
	if cloned {
		t.Fatal("pinned cache hit must not clone")
	}
}

func TestGitArtifactFetcherDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	body := []byte("served-git")
	url, branch, _ := makeGitFixture(t, "a.json", body)
	f := &gitArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "git", URL: url, Ref: branch}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "pinned:sha256:deadbeef"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error, got %v", err)
	}
}

func TestGitArtifactFetcherCloneError(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		return nil, nil, errors.New("clone boom")
	}}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///x", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonTransport {
		t.Fatalf("want transport error, got %v", err)
	}
}

func TestGitArtifactFetcherBadURL(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "ht!tp://%zz"}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error, got %v", err)
	}
}

func TestGitArtifactFetcherHeadError(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: emptyRepoCloner()}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///x", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("want not_found error, got %v", err)
	}
}

func TestGitArtifactFetcherMissingPath(t *testing.T) {
	withPackagesCache(t)
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "x.json", []byte("{}"))
	f := &gitArtifactFetcher{cloner: committedRepoFS(t, dir, nil)}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "missing.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("want not_found error, got %v", err)
	}
}

func TestGitArtifactFetcherOversized(t *testing.T) {
	withPackagesCache(t)
	big := make([]byte, maxLayerBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "big.json", big)
	f := &gitArtifactFetcher{cloner: committedRepoFS(t, dir, map[string][]byte{"big.json": big})}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "big.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content (oversized) error, got %v", err)
	}
}

func TestGitArtifactFetcherCacheWriteError(t *testing.T) {
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "x.json", []byte("{}"))
	// Point AGENTS_HOME at a path whose cache parent is a regular file so
	// writeCachedArtifact's MkdirAll fails after a successful read.
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", blocker)
	f := &gitArtifactFetcher{cloner: committedRepoFS(t, dir, map[string][]byte{"x.json": []byte("{}")})}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "x.json", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestGitArtifactFetcherRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	url, branch, _ := makeGitFixture(t, "a.json", []byte("blob"))
	f := &gitArtifactFetcher{}
	src := Source{Type: "git", URL: url, Ref: branch, Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: branch})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error for required posture, got %v", err)
	}
}

func TestGitArtifactRefResolution(t *testing.T) {
	if got := gitArtifactRef(Source{Ref: "src-ref"}, PackageRefParts{VersionSpec: "1.2.3"}); got != "1.2.3" {
		t.Fatalf("version spec ref = %q, want 1.2.3", got)
	}
	if got := gitArtifactRef(Source{Ref: "src-ref"}, PackageRefParts{VersionSpec: "pinned:sha256:abc"}); got != "src-ref" {
		t.Fatalf("pinned -> source ref = %q, want src-ref", got)
	}
	if got := gitArtifactRef(Source{}, PackageRefParts{VersionSpec: "pinned:sha256:abc"}); got != "main" {
		t.Fatalf("pinned -> main fallback = %q, want main", got)
	}
}

// --- local artifact fetcher ------------------------------------------------

func TestLocalArtifactFetcherReadsAndCaches(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"agent":"local"}`)
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skill", "x.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	wantDigest := "sha256:" + sha256Hex(body)

	f := &localArtifactFetcher{}
	got, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "dev", ArtifactPath: "skill/x.json", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Digest != wantDigest || string(got.Data) != string(body) || got.CacheHit {
		t.Fatalf("unexpected result %+v", got)
	}
	if !got.KeyInputs.WorktreeDirty || got.KeyInputs.WorktreeContentHash != wantDigest {
		t.Fatalf("expected dirty worktree key inputs, got %+v", got.KeyInputs)
	}
	if _, ok := readCachedArtifact(wantDigest); !ok {
		t.Fatal("expected artifact cached by digest")
	}
}

func TestLocalArtifactFetcherURLFallback(t *testing.T) {
	withPackagesCache(t)
	body := []byte("via-url")
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	got, err := f.FetchArtifact(Source{Type: "local", URL: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q", got.Data)
	}
}

func TestLocalArtifactFetcherNotFound(t *testing.T) {
	withPackagesCache(t)
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: t.TempDir()}, PackageRefParts{SourceID: "s", ArtifactPath: "nope.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("want not_found error, got %v", err)
	}
}

func TestLocalArtifactFetcherDigestPinCacheHit(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-local")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	// A non-existent path proves the pinned cache fast path runs before any read.
	got, err := f.FetchArtifact(Source{Type: "local", Path: "/no/such/dir"}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if !got.CacheHit || got.Digest != digest {
		t.Fatalf("expected cache hit, got %+v", got)
	}
}

func TestLocalArtifactFetcherDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	body := []byte("served-local")
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "pinned:sha256:deadbeef"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error, got %v", err)
	}
}

func TestLocalArtifactFetcherRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.json"), []byte("blob"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	src := Source{Type: "local", Path: srcDir, Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error for required posture, got %v", err)
	}
}

func TestLocalArtifactFetcherCacheWriteError(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", blocker)
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestLocalArtifactFetcherPinnedCacheHitRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-required")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	src := Source{Type: "local", Path: t.TempDir(), Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "pinned:" + digest})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error on pinned cache hit, got %v", err)
	}
}

func TestLocalArtifactFetcherReadError(t *testing.T) {
	withPackagesCache(t)
	// Pointing ArtifactPath at a directory makes os.ReadFile fail with a
	// non-IsNotExist error, covering the content-read error branch.
	srcDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(srcDir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "adir", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error reading a directory, got %v", err)
	}
}

func TestGitArtifactFetcherPinnedCacheHitRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("pinned-git-required")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	f := &gitArtifactFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		return nil, nil, errors.New("should not clone")
	}}
	src := Source{Type: "git", URL: "file:///r", Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "a.json", VersionSpec: "pinned:" + digest})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonAuth {
		t.Fatalf("want auth error on pinned cache hit, got %v", err)
	}
}
