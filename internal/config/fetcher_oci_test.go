package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// This file covers t8 (OCI publish producer + round-trip byte-parity smoke,
// package-artifact-install spec D9/R8/DC2-oci): PackTree (the producer
// counterpart of UntarBundle), the real OCI Distribution push/pull wire
// protocol (artifact_bundle.go), and the fetcher_oci.go wiring changes that
// activate on a live pull (ManifestDigest population, the sidecar schema
// version gate, verifyOCIPin's dual-digest acceptance). fakeOCIRegistry is a
// minimal in-process OCI Distribution server — not a conformance-tested
// implementation of the spec, just enough of the wire surface (blob
// upload/fetch, manifest push/fetch) to prove OUR client round-trips
// correctly, so this suite needs no docker/network dependency.

// fakeOCIRegistry is a minimal in-memory OCI Distribution v2 server used as
// the "local test registry" for the round-trip smoke tests below.
type fakeOCIRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte
	manifests map[string][]byte
	uploads   int

	// requireAuth, when non-empty, is the exact Authorization header value
	// every request must carry; anything else gets a 401 + challenge.
	requireAuth string
	challenge   string

	// tamperBlobDigest + tamperedData: when a GET blob request asks for this
	// digest, the tampered bytes are served instead of the real stored ones
	// (H5 regression coverage — the client must recompute and reject).
	tamperBlobDigest string
	tamperedData     []byte

	// corruptManifestType, when true, rewrites a served manifest's
	// artifactType to a wrong value before responding (H6 regression
	// coverage).
	corruptManifestType bool

	// pullCount tracks GET manifest/blob requests, so a test can assert the
	// registry was NOT hit again (the frozen-lock no-op requirement).
	pullCount int

	// uploadLocationOverride, when non-empty, is returned verbatim as the
	// blob-upload Location header instead of the normal same-origin relative
	// one — used to simulate a malicious/compromised registry pointing the
	// credentialed PUT at an attacker origin (H12 cross-origin guard coverage).
	uploadLocationOverride string
}

func newFakeOCIRegistry(t *testing.T) (*httptest.Server, *fakeOCIRegistry) {
	t.Helper()
	reg := &fakeOCIRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(reg.handle))
	t.Cleanup(srv.Close)
	return srv, reg
}

func (r *fakeOCIRegistry) handle(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.requireAuth != "" && req.Header.Get("Authorization") != r.requireAuth {
		w.Header().Set("Www-Authenticate", r.challenge)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(req.URL.Path, "/v2/")

	switch {
	case strings.HasSuffix(path, "/blobs/uploads/") && req.Method == http.MethodPost:
		r.handleBlobUploadStart(w, path)
	case strings.Contains(path, "/blobs/uploads/") && req.Method == http.MethodPut:
		r.handleBlobUploadPut(w, req)
	case strings.Contains(path, "/blobs/") && req.Method == http.MethodGet:
		r.handleBlobGet(w, path)
	case strings.Contains(path, "/manifests/") && req.Method == http.MethodPut:
		r.handleManifestPut(w, req, path)
	case strings.Contains(path, "/manifests/") && req.Method == http.MethodGet:
		r.handleManifestGet(w, path)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (r *fakeOCIRegistry) handleBlobUploadStart(w http.ResponseWriter, path string) {
	r.uploads++
	loc := fmt.Sprintf("/v2/%suploads/sess-%d", strings.TrimSuffix(path, "uploads/"), r.uploads)
	if r.uploadLocationOverride != "" {
		loc = r.uploadLocationOverride
	}
	w.Header().Set("Location", loc)
	w.WriteHeader(http.StatusAccepted)
}

func (r *fakeOCIRegistry) handleBlobUploadPut(w http.ResponseWriter, req *http.Request) {
	digest := req.URL.Query().Get("digest")
	data, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.blobs[digest] = data
	w.WriteHeader(http.StatusCreated)
}

func (r *fakeOCIRegistry) handleBlobGet(w http.ResponseWriter, path string) {
	r.pullCount++
	digest := path[strings.LastIndex(path, "/blobs/")+len("/blobs/"):]
	data, ok := r.blobs[digest]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.tamperBlobDigest != "" && digest == r.tamperBlobDigest {
		data = r.tamperedData
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func (r *fakeOCIRegistry) handleManifestPut(w http.ResponseWriter, req *http.Request, path string) {
	ref := path[strings.LastIndex(path, "/manifests/")+len("/manifests/"):]
	data, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.manifests[ref] = data
	sum := sha256.Sum256(data)
	r.manifests["sha256:"+hex.EncodeToString(sum[:])] = data
	w.WriteHeader(http.StatusCreated)
}

func (r *fakeOCIRegistry) handleManifestGet(w http.ResponseWriter, path string) {
	r.pullCount++
	ref := path[strings.LastIndex(path, "/manifests/")+len("/manifests/"):]
	data, ok := r.manifests[ref]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.corruptManifestType {
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err == nil {
			doc["artifactType"] = "application/vnd.wrong.type"
			if rewritten, err := json.Marshal(doc); err == nil {
				data = rewritten
			}
		}
	}
	w.Header().Set("Content-Type", ociManifestMediaType)
	_, _ = w.Write(data)
}

// ociTestSource builds the oci Source pointing at srv, with basePath "".
func ociTestSource(srv *httptest.Server) Source {
	return Source{Type: "oci", URL: "oci://" + strings.TrimPrefix(srv.URL, "http://")}
}

// buildFixtureTree writes a small, multi-level resource tree (SKILL.md at
// the root plus a nested instructions/nested/deep.md) under a fresh temp dir
// and returns its path — the source tree PackTree packs and the round-trip
// tests compare the materialized pull against, RECURSIVELY.
func buildFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "SKILL.md"), "skill body\n")
	mustMkdirAll(t, filepath.Join(root, "instructions", "nested"))
	mustWriteFile(t, filepath.Join(root, "instructions", "x.md"), "instructions body\n")
	mustWriteFile(t, filepath.Join(root, "instructions", "nested", "deep.md"), "deeply nested body\n")
	return root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing fixture file %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating fixture dir %s: %v", path, err)
	}
}

// bundleEntryMap indexes a Bundle's entries by path for content comparison.
func bundleEntryMap(b Bundle) map[string]BundleEntry {
	m := make(map[string]BundleEntry, len(b.Entries))
	for _, e := range b.Entries {
		m[e.Path] = e
	}
	return m
}

// assertRecursiveByteParity fails t unless got and want contain the exact
// same set of paths with byte-identical content and dir/file kind at EVERY
// entry, at any depth — the R8 requirement, not just a top-level check.
func assertRecursiveByteParity(t *testing.T, got, want Bundle) {
	t.Helper()
	gotByPath := bundleEntryMap(got)
	wantByPath := bundleEntryMap(want)
	if len(gotByPath) != len(wantByPath) {
		t.Fatalf("entry count mismatch: got %d %v, want %d %v", len(gotByPath), bundlePaths(got), len(wantByPath), bundlePaths(want))
	}
	for path, wantEntry := range wantByPath {
		gotEntry, ok := gotByPath[path]
		if !ok {
			t.Fatalf("materialized tree missing source entry %q", path)
		}
		if gotEntry.IsDir != wantEntry.IsDir {
			t.Fatalf("entry %q: IsDir mismatch got %v want %v", path, gotEntry.IsDir, wantEntry.IsDir)
		}
		if string(gotEntry.Data) != string(wantEntry.Data) {
			t.Fatalf("entry %q: content mismatch got %q want %q", path, gotEntry.Data, wantEntry.Data)
		}
	}
}

// --- PackTree (producer) -----------------------------------------------------

func TestPackTreePacksAndDecodesRecursively(t *testing.T) {
	src := buildFixtureTree(t)
	bundle, blob, err := PackTree(src, DefaultBundleLimits())
	if err != nil {
		t.Fatalf("PackTree: %v", err)
	}
	if len(bundle.Entries) == 0 {
		t.Fatal("expected a non-empty bundle")
	}
	// The producer's own output must decode via the SAME H1 normalizer the
	// consumer (UntarBundle) uses — one shared format (H1).
	decoded, err := UntarBundle(blob, DefaultBundleLimits())
	if err != nil {
		t.Fatalf("UntarBundle(PackTree output): %v", err)
	}
	assertRecursiveByteParity(t, decoded, bundle)
	byPath := bundleEntryMap(decoded)
	nested, ok := byPath["instructions/nested/deep.md"]
	if !ok || string(nested.Data) != "deeply nested body\n" {
		t.Fatalf("expected deeply nested entry to round-trip, got %v", bundlePaths(decoded))
	}
}

func TestPackTreeRejectsSymlinkInSourceTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	src := t.TempDir()
	mustWriteFile(t, filepath.Join(src, "real.md"), "real\n")
	if err := os.Symlink(filepath.Join(src, "real.md"), filepath.Join(src, "link.md")); err != nil {
		t.Fatalf("creating symlink fixture: %v", err)
	}
	if _, _, err := PackTree(src, DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to reject a source tree containing a symlink (H1)")
	}
}

func TestPublishTreeRejectsNonOCISource(t *testing.T) {
	withPackagesCache(t)
	src := buildFixtureTree(t)
	_, err := PublishTree(context.Background(), Source{Type: "git"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "v1"}, src)
	if err == nil {
		t.Fatal("expected an error publishing to a non-oci source")
	}
}

func TestPublishTreeRejectsPinnedTarget(t *testing.T) {
	withPackagesCache(t)
	src := buildFixtureTree(t)
	oci := Source{Type: "oci", URL: "oci://reg.example"}
	_, err := PublishTree(context.Background(), oci, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:sha256:" + strings.Repeat("a", 64)}, src)
	if err == nil {
		t.Fatal("expected an error publishing to a pinned:sha256: target")
	}
}

// --- verifyOCIPin unit coverage ---------------------------------------------

// TestVerifyOCIPinAcceptsManifestDigest exercises the manifest-digest branch
// directly (no network): before t8 wired ociPullLive, manifestDigest was
// always empty and this branch was unreachable dead code (the tracked
// oci-pin-manifest-digest-deadcode residual). A pin equal to a DISTINCT,
// non-empty manifest digest must now be accepted.
func TestVerifyOCIPinAcceptsManifestDigest(t *testing.T) {
	payload := "sha256:" + strings.Repeat("1", 64)
	manifest := "sha256:" + strings.Repeat("2", 64)
	if payload == manifest {
		t.Fatal("test fixture bug: digests must be distinct")
	}
	if err := verifyOCIPin(manifest, manifest, payload, "ref", "src"); err != nil {
		t.Fatalf("pin matching the manifest digest must be accepted, got %v", err)
	}
	if err := verifyOCIPin(payload, manifest, payload, "ref", "src"); err != nil {
		t.Fatalf("pin matching the payload digest must still be accepted, got %v", err)
	}
	other := "sha256:" + strings.Repeat("3", 64)
	if err := verifyOCIPin(other, manifest, payload, "ref", "src"); err == nil {
		t.Fatal("a pin matching NEITHER digest must be rejected")
	}
	// Empty pin (no pin requested) is always a no-op, regardless of digests.
	if err := verifyOCIPin("", manifest, payload, "ref", "src"); err != nil {
		t.Fatalf("empty pin must be a no-op, got %v", err)
	}
}

// --- round trip: publish -> tag pull -> pinned re-pull (frozen no-op) ------

// TestResolveOCILocationOriginGuard is the H12 unit proof that a
// registry-controlled blob-upload Location is pinned to the configured
// registry origin before any credential is attached (t8 round-2 HIGH).
func TestResolveOCILocationOriginGuard(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		loc     string
		wantErr bool
	}{
		{"relative-same-origin", "https://reg.example:5000", "/v2/x/blobs/uploads/sess-1", false},
		{"absolute-same-origin", "https://reg.example:5000", "https://reg.example:5000/v2/x/uploads/sess-1", false},
		{"absolute-same-host-default-port", "https://reg.example", "https://reg.example:443/v2/x/uploads/1", false},
		{"cross-host", "https://reg.example:5000", "https://attacker.evil/steal", true},
		{"same-host-different-port", "https://reg.example:5000", "https://reg.example:9999/v2/x/uploads/1", true},
		{"https-to-http-downgrade", "https://reg.example", "http://reg.example/v2/x/uploads/1", true},
		{"embedded-userinfo", "https://reg.example", "https://user:pass@reg.example/v2/x/uploads/1", true},
		{"scheme-relative-network-path", "https://reg.example", "//attacker.evil/steal", true},
		{"ipv6-same-origin", "https://[2001:db8::1]:5000", "https://[2001:db8::1]:5000/v2/x/uploads/1", false},
		{"ipv6-cross-host", "https://[2001:db8::1]:5000", "https://[2001:db8::2]:5000/steal", true},
		{"opaque-non-web-scheme", "https://reg.example", "mailto:evil@attacker.evil", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveOCILocation(tc.base, tc.loc)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected rejection, got resolved %q", got)
				}
				// The error must not echo a credential-bearing query/userinfo.
				if strings.Contains(err.Error(), "user:pass") {
					t.Fatalf("error leaked userinfo: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

// TestGuardOCIRegistryRedirect is the H12 proof for the shared registry
// client's redirect policy: a credentialed PUSH (non-GET) refuses any redirect;
// a PULL (GET) refuses a non-https (cleartext) target but may follow a
// cross-origin https CDN redirect (Go strips Authorization cross-domain).
func TestGuardOCIRegistryRedirect(t *testing.T) {
	mkReq := func(method, rawurl string) *http.Request {
		r, err := http.NewRequest(method, rawurl, nil)
		if err != nil {
			t.Fatalf("building %s %s: %v", method, rawurl, err)
		}
		return r
	}
	cases := []struct {
		name       string
		origMethod string
		redirectTo string
		wantErr    bool
	}{
		{"push-post-redirect-refused", http.MethodPost, "https://reg.example/other", true},
		{"push-put-redirect-refused", http.MethodPut, "https://reg.example/other", true},
		{"push-put-cross-origin-refused", http.MethodPut, "https://attacker.evil/steal", true},
		{"pull-get-https-cdn-allowed", http.MethodGet, "https://cdn.example/blob", false},
		{"pull-get-http-downgrade-refused", http.MethodGet, "http://reg.example/blob", true},
		{"pull-get-same-host-https-allowed", http.MethodGet, "https://reg.example/blob", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := mkReq(tc.origMethod, "https://reg.example/v2/x/blobs/uploads/")
			next := mkReq(http.MethodGet, tc.redirectTo)
			err := guardOCIRegistryRedirect(next, []*http.Request{orig})
			if tc.wantErr && err == nil {
				t.Fatalf("expected the redirect to be refused")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected the redirect to be allowed, got: %v", err)
			}
		})
	}
}

// TestPublishTreeRefusesCrossOriginBlobUploadLocation proves end-to-end that a
// malicious registry returning an attacker-origin blob-upload Location cannot
// harvest the push credential: the credentialed PUT is never sent to the
// attacker, the publish fails closed, and the attacker server records zero
// requests (t8 round-2 HIGH, H12 credential exfiltration).
func TestPublishTreeRefusesCrossOriginBlobUploadLocation(t *testing.T) {
	withPackagesCache(t)
	var attackerHits int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&attackerHits, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(attacker.Close)

	srv, reg := newFakeOCIRegistry(t)
	reg.uploadLocationOverride = attacker.URL + "/v2/evil/blobs/uploads/sess-steal"
	src := ociTestSource(srv)
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v1.0.0"}

	_, err := PublishTree(context.Background(), src, parts, srcTree)
	if err == nil {
		t.Fatal("expected PublishTree to refuse the cross-origin blob-upload Location")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected a cross-origin rejection, got: %v", err)
	}
	if got := atomic.LoadInt32(&attackerHits); got != 0 {
		t.Fatalf("credentialed request reached the attacker origin %d time(s) — credential exfiltrated", got)
	}
}

func TestPublishAndPullRoundTripByteParity(t *testing.T) {
	withPackagesCache(t)
	srv, _ := newFakeOCIRegistry(t)
	src := ociTestSource(srv)
	srcTree := buildFixtureTree(t)
	sourceBundle, _, err := PackTree(srcTree, DefaultBundleLimits())
	if err != nil {
		t.Fatalf("PackTree(source): %v", err)
	}

	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v1.0.0"}
	result, err := PublishTree(context.Background(), src, parts, srcTree)
	if err != nil {
		t.Fatalf("PublishTree: %v", err)
	}
	if result.ManifestDigest == "" || result.LayerDigest == "" {
		t.Fatalf("expected both digests populated, got %+v", result)
	}
	if result.ManifestDigest == result.LayerDigest {
		t.Fatalf("manifest digest and layer digest must be distinct objects (H5), got the same value %q", result.ManifestDigest)
	}

	// A tag pull through the REAL wired path (nil puller -> ociPull ->
	// ociPullLive) is how a first-time resolve actually happens.
	f := &ociFetcher{}
	got, err := f.FetchArtifact(src, parts)
	if err != nil {
		t.Fatalf("FetchArtifact (tag pull): %v", err)
	}
	if got.CacheHit {
		t.Fatal("first pull must not be a cache hit")
	}
	if got.Bundle == nil {
		t.Fatal("expected a decoded Bundle from the pulled artifact")
	}
	if got.Digest != result.LayerDigest {
		t.Fatalf("pulled content digest %s does not match published layer digest %s", got.Digest, result.LayerDigest)
	}
	// R8 — RECURSIVE byte-parity: every entry, at every depth, byte-identical.
	assertRecursiveByteParity(t, *got.Bundle, sourceBundle)

	// A second, PINNED pull addressed at the digest the first pull cached
	// under must be a frozen no-op: no network call at all (a puller that
	// errors if invoked proves it).
	pinnedParts := PackageRefParts{SourceID: parts.SourceID, ArtifactPath: parts.ArtifactPath, VersionSpec: "pinned:" + got.Digest}
	frozen := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{}, errors.New("must not pull: lock present, this must be a cache hit")
	}}
	got2, err := frozen.FetchArtifact(src, pinnedParts)
	if err != nil {
		t.Fatalf("FetchArtifact (pinned re-pull): %v", err)
	}
	if !got2.CacheHit {
		t.Fatal("expected the pinned re-pull to be a cache hit (frozen no-op)")
	}
	if got2.Bundle == nil {
		t.Fatal("expected the cached hit to still carry a decoded Bundle")
	}
	assertRecursiveByteParity(t, *got2.Bundle, sourceBundle)
}

// TestOCIPullLiveResolvesByManifestDigest proves the live wire client can
// resolve a manifest addressed by ITS OWN digest (not just by tag) — the
// standard OCI Distribution "GET manifest by digest" capability a truly
// cold pin (no prior local cache at all, e.g. a fresh CI runner) depends on
// — and that the resolved ManifestDigest/Digest are the distinct objects H5
// requires (never conflated).
func TestOCIPullLiveResolvesByManifestDigest(t *testing.T) {
	withPackagesCache(t)
	srv, _ := newFakeOCIRegistry(t)
	src := ociTestSource(srv)
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v2.0.0"}
	result, err := PublishTree(context.Background(), src, parts, srcTree)
	if err != nil {
		t.Fatalf("PublishTree: %v", err)
	}

	ref, err := parseOCIRef(src, PackageRefParts{ArtifactPath: parts.ArtifactPath, VersionSpec: "pinned:" + result.ManifestDigest})
	if err != nil {
		t.Fatalf("parseOCIRef: %v", err)
	}
	blob, err := ociPullLive(context.Background(), ref, nil)
	if err != nil {
		t.Fatalf("ociPullLive by manifest digest: %v", err)
	}
	if blob.ManifestDigest != result.ManifestDigest {
		t.Fatalf("resolved manifest digest %s != published %s", blob.ManifestDigest, result.ManifestDigest)
	}
	if blob.Digest != result.LayerDigest {
		t.Fatalf("resolved layer digest %s != published %s", blob.Digest, result.LayerDigest)
	}
	if blob.ManifestDigest == blob.Digest {
		t.Fatal("manifest digest and layer digest must never be conflated")
	}
}

// TestOCIPullLiveRejectsTamperedBlob is the H5 regression test: a registry
// that serves tampered bytes for the layer blob (correct manifest, correct
// declared descriptor digest, wrong actual content) must be rejected by the
// recomputed-digest check before the payload is trusted.
func TestOCIPullLiveRejectsTamperedBlob(t *testing.T) {
	withPackagesCache(t)
	srv, reg := newFakeOCIRegistry(t)
	src := ociTestSource(srv)
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v1.0.0"}
	result, err := PublishTree(context.Background(), src, parts, srcTree)
	if err != nil {
		t.Fatalf("PublishTree: %v", err)
	}

	reg.tamperBlobDigest = result.LayerDigest
	reg.tamperedData = []byte("this is not the artifact you published")

	f := &ociFetcher{}
	_, err = f.FetchArtifact(src, parts)
	if err == nil {
		t.Fatal("expected a digest-mismatch rejection for a tampered blob")
	}
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want a content-integrity error, got %v", err)
	}
	if _, ok := readCachedArtifact(result.LayerDigest); ok {
		t.Fatal("tampered bytes must never be cached under the declared digest")
	}
}

// TestOCIPullLiveRejectsWrongMediaType is the H6 regression test: a registry
// whose manifest declares the wrong artifactType must be rejected before the
// payload is cached or untarred, even though the layer bytes and their
// declared digest are otherwise perfectly valid.
func TestOCIPullLiveRejectsWrongMediaType(t *testing.T) {
	withPackagesCache(t)
	srv, reg := newFakeOCIRegistry(t)
	src := ociTestSource(srv)
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v1.0.0"}
	if _, err := PublishTree(context.Background(), src, parts, srcTree); err != nil {
		t.Fatalf("PublishTree: %v", err)
	}

	reg.corruptManifestType = true

	f := &ociFetcher{}
	_, err := f.FetchArtifact(src, parts)
	if err == nil {
		t.Fatal("expected a media-type rejection for a manifest declaring the wrong artifactType")
	}
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want a schema error, got %v", err)
	}
}

// TestPublishAndPullRoundTripWithBearerAuth proves the OCI producer/consumer
// wire client reuses t7's ociAuthHeaderForRef end to end: the fake registry
// demands a specific bearer Authorization header on every request; a source
// with no auth block configured is rejected, one with a matching bearer
// token_env succeeds.
func TestPublishAndPullRoundTripWithBearerAuth(t *testing.T) {
	withPackagesCache(t)
	t.Setenv("DA_TEST_OCI_TOKEN", "s3cr3t-token")
	srv, reg := newFakeOCIRegistry(t)
	reg.requireAuth = "Bearer s3cr3t-token"
	reg.challenge = `Bearer realm="unused",service="test"`

	authed := Source{
		Type: "oci",
		URL:  "oci://" + strings.TrimPrefix(srv.URL, "http://"),
		Auth: json.RawMessage(`{"provider":"bearer","token_env":"DA_TEST_OCI_TOKEN"}`),
	}
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/review-pr", VersionSpec: "v1.0.0"}

	// No auth configured at all -> every request is rejected (negative path).
	unauthed := authed
	unauthed.Auth = nil
	if _, err := PublishTree(context.Background(), unauthed, parts, srcTree); err == nil {
		t.Fatal("expected publish without credentials to fail against an auth-requiring registry")
	}

	result, err := PublishTree(context.Background(), authed, parts, srcTree)
	if err != nil {
		t.Fatalf("PublishTree with bearer auth: %v", err)
	}

	f := &ociFetcher{}
	got, err := f.FetchArtifact(authed, parts)
	if err != nil {
		t.Fatalf("FetchArtifact with bearer auth: %v", err)
	}
	if got.Digest != result.LayerDigest {
		t.Fatalf("pulled digest %s != published %s", got.Digest, result.LayerDigest)
	}
}

// --- sidecar schema version --------------------------------------------------

// TestOCITypeSidecarSchemaVersionGatesCacheHit is the oci-sidecar-schema-
// versioning regression test. Two shapes are exercised:
//   - a sidecar with NO schema_version field at all (the zero value) is
//     "legacy" — every sidecar written before this field existed already only
//     ever came from a fresh pull that passed the mandatory
//     verifyOCILayerDescriptorDigest check, so it is trusted (this is also
//     what oci_pass2_e2e_test.go's hand-written external-package fixture
//     looks like, and it must keep working unmodified);
//   - a sidecar carrying an EXPLICIT, non-current version — one this binary
//     never wrote — must not be trusted as a validated cache entry, even
//     though its declared types are otherwise correct.
func TestOCITypeSidecarSchemaVersionGatesCacheHit(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("some artifact bytes")
	digest := artifactDigest(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}

	// Legacy shape: no schema_version key at all (zero value) — trusted.
	legacy := `{"artifactType":"` + ociArtifactMediaType + `","mediaType":"` + ociArtifactMediaType + `"}`
	if err := writeConfinedPackagesCacheFile(digestDir(digest), ociTypeSidecarName, []byte(legacy)); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedOCIArtifact(digest); !ok {
		t.Fatal("a legacy sidecar with no schema_version field must still be trusted")
	}

	// An explicit, non-current schema version — this binary never wrote it —
	// must NOT be trusted.
	future, err := json.Marshal(ociTypeSidecar{SchemaVersion: ociTypeSidecarSchemaVersion + 1, ArtifactType: ociArtifactMediaType, MediaType: ociArtifactMediaType})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConfinedPackagesCacheFile(digestDir(digest), ociTypeSidecarName, future); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedOCIArtifact(digest); ok {
		t.Fatal("a sidecar declaring a schema version this binary never wrote must not be trusted")
	}

	// The CURRENT writer stamps the current schema version, and that IS trusted.
	if err := writeOCITypeSidecar(digest, ociArtifactMediaType, ociArtifactMediaType); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedOCIArtifact(digest); !ok {
		t.Fatal("a sidecar written by the current binary must be trusted")
	}
}
