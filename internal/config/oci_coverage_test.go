package config

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// This file raises per-file line coverage on the OCI publish/pull wire client
// (artifact_bundle.go), the auth seam (oci_auth.go), the version-spec
// classifier (oci_resolve.go), and the shared fetcher plumbing (fetcher_oci.go)
// to the >=95% ratchet the coverage gate enforces. It exercises the error and
// defensive branches the happy-path round-trip suite in fetcher_oci_test.go
// does not reach: bad URLs, refused/short-circuited redirects, cross-origin and
// userinfo location rejection, non-https realms, digest/media-type gates, the
// live push/pull fault legs (transport failures, wrong statuses, oversize and
// truncated bodies), and the credential-safe URL helpers. All helpers are
// prefixed `ociCov` so they never collide with the existing package test
// harness (fakeOCIRegistry et al.), which is reused for the happy paths.

// --- oci_resolve.go ---------------------------------------------------------

// TestOCICovLooksLikeSemVerRangeEmpty covers the empty-spec early return of
// looksLikeSemVerRange, which classifyOCIVersionSpec short-circuits before
// ever calling (so it is only reachable by a direct call).
func TestOCICovLooksLikeSemVerRangeEmpty(t *testing.T) {
	if looksLikeSemVerRange("") {
		t.Fatal("an empty spec is not a SemVer range")
	}
}

// --- artifact_bundle.go: pure helpers ---------------------------------------

// TestOCICovFindSource exercises both the match and the no-match legs of
// FindSource (the exported source-id resolver commands/config/publish.go uses).
func TestOCICovFindSource(t *testing.T) {
	sources := []Source{{ID: "a", Type: "oci"}, {ID: "b", Type: "git"}}
	if got, ok := FindSource(sources, "b"); !ok || got.Type != "git" {
		t.Fatalf("expected to find source b, got %+v ok=%v", got, ok)
	}
	if got, ok := FindSource(sources, "missing"); ok || got.ID != "" {
		t.Fatalf("expected a miss for an absent id, got %+v ok=%v", got, ok)
	}
	if _, ok := FindSource(nil, "x"); ok {
		t.Fatal("expected a miss against a nil source list")
	}
}

// TestOCICovURLWithoutQuery covers urlWithoutQuery's parse-success path (query
// and userinfo stripped) and its substring fallbacks on an unparseable URL.
func TestOCICovURLWithoutQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strips-query", "https://reg.example/v2/x/uploads/1?_state=tok&digest=sha256:ab", "https://reg.example/v2/x/uploads/1"},
		{"strips-userinfo", "https://user:pass@reg.example/v2/x", "https://reg.example/v2/x"},
		{"no-query-unchanged", "https://reg.example/v2/x", "https://reg.example/v2/x"},
		// A control byte makes url.Parse fail, forcing the substring fallback.
		{"unparseable-with-query", "http://\x7f/bad?secret=1", "http://\x7f/bad"},
		{"unparseable-no-query", "http://\x7f/bad", "http://\x7f/bad"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := urlWithoutQuery(tc.in); got != tc.want {
				t.Fatalf("urlWithoutQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(urlWithoutQuery(tc.in), "pass") {
				t.Fatalf("userinfo leaked: %q", urlWithoutQuery(tc.in))
			}
		})
	}
}

// TestOCICovOCIBaseURL covers every scheme branch of ociBaseURL: an explicit
// http/https prefix used as-is, a loopback host defaulting to http, and a
// remote host defaulting to https.
func TestOCICovOCIBaseURL(t *testing.T) {
	cases := []struct {
		registry string
		want     string
	}{
		{"http://127.0.0.1:5000/", "http://127.0.0.1:5000"},
		{"https://reg.example", "https://reg.example"},
		{"localhost:5000", "http://localhost:5000"},
		{"127.0.0.1", "http://127.0.0.1"},
		{"reg.example.com", "https://reg.example.com"},
	}
	for _, tc := range cases {
		if got := ociBaseURL(tc.registry); got != tc.want {
			t.Fatalf("ociBaseURL(%q) = %q, want %q", tc.registry, got, tc.want)
		}
	}
}

// TestOCICovGuardOCIRegistryRedirectNoVia covers guardOCIRegistryRedirect's
// initial-request short-circuit (an empty via chain is not a redirect).
func TestOCICovGuardOCIRegistryRedirectNoVia(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://reg.example/v2/x", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if err := guardOCIRegistryRedirect(req, nil); err != nil {
		t.Fatalf("an empty via chain must be allowed, got %v", err)
	}
}

// TestOCICovResolveOCILocationParseErrors covers resolveOCILocation's two
// url.Parse failure legs (a malformed Location and a malformed base).
func TestOCICovResolveOCILocationParseErrors(t *testing.T) {
	if _, err := resolveOCILocation("https://reg.example", "http://%zz"); err == nil {
		t.Fatal("expected a parse error on a malformed location")
	}
	if _, err := resolveOCILocation("http://%zz", "/v2/x/uploads/1"); err == nil {
		t.Fatal("expected a parse error on a malformed base")
	}
}

// TestOCICovAppendDigestQueryParseError covers appendDigestQuery's url.Parse
// failure leg.
func TestOCICovAppendDigestQueryParseError(t *testing.T) {
	if _, err := appendDigestQuery("http://%zz", "sha256:ab"); err == nil {
		t.Fatal("expected a parse error on a malformed url")
	}
}

// --- artifact_bundle.go: PackTree / PublishTree error legs ------------------

// TestOCICovPackTreeOpenRootError covers PackTree's os.OpenRoot failure on a
// nonexistent source tree.
func TestOCICovPackTreeOpenRootError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	if _, _, err := PackTree(missing, DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to fail opening a nonexistent tree")
	}
}

// TestOCICovPublishTreeErrorLegs covers PublishTree's ref-resolution failure
// (a deferred SemVer range), its empty-tag guard, and its PackTree failure leg.
func TestOCICovPublishTreeErrorLegs(t *testing.T) {
	withPackagesCache(t)
	oci := Source{Type: "oci", URL: "oci://reg.example"}

	// A SemVer range version-spec is rejected by parseOCIRef -> resolving fails.
	if _, err := PublishTree(context.Background(), oci, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "^1.2.0"}, t.TempDir()); err == nil {
		t.Fatal("expected a ref-resolution error for a SemVer range target")
	}

	// An empty version-spec leaves both tag and digest empty -> empty-tag guard.
	if _, err := PublishTree(context.Background(), oci, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: ""}, t.TempDir()); err == nil {
		t.Fatal("expected an empty-tag rejection")
	}

	// A valid tag but a nonexistent source tree -> PackTree failure leg.
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := PublishTree(context.Background(), oci, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "v1.0.0"}, missing); err == nil {
		t.Fatal("expected a packing error for a nonexistent tree")
	}
}

// --- artifact_bundle.go: ociAuthenticatedRequest error legs -----------------

// TestOCICovAuthenticatedRequestBuildError covers the http.NewRequestWithContext
// failure leg (a URL carrying a control byte cannot be turned into a request).
func TestOCICovAuthenticatedRequestBuildError(t *testing.T) {
	ref := ociRef{Registry: "reg.example", Repository: "x"}
	if _, err := ociAuthenticatedRequest(context.Background(), http.MethodGet, "http://reg.example/\x7f\n", nil, nil, nil, ref); err == nil {
		t.Fatal("expected a request-build error for a malformed url")
	}
}

// TestOCICovAuthenticatedRequestAuthError covers the first-round auth resolution
// failure leg (an unsupported provider errors before any request is sent).
func TestOCICovAuthenticatedRequestAuthError(t *testing.T) {
	ref := ociRef{Registry: "reg.example", Repository: "x"}
	auth := json.RawMessage(`{"provider":"nope"}`)
	if _, err := ociAuthenticatedRequest(context.Background(), http.MethodGet, "https://reg.example/v2/x", nil, nil, auth, ref); err == nil {
		t.Fatal("expected an auth-resolution error for an unsupported provider")
	}
}

// --- artifact_bundle.go: ociPushLive tag/digest guards ----------------------

// TestOCICovPushLiveMissingTagAndDigest drives ociPushLive against a working
// registry (so both blob pushes succeed) with a ref carrying neither a tag nor
// a digest — reaching the manifest-target resolution that falls back from tag
// to digest and then errors when both are empty.
func TestOCICovPushLiveMissingTagAndDigest(t *testing.T) {
	srv, _ := newFakeOCIRegistry(t)
	ref := ociRef{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "x/y"}
	_, err := ociPushLive(context.Background(), ref, nil, []byte("layer"), ociArtifactMediaType, ociArtifactMediaType)
	if err == nil {
		t.Fatal("expected an error pushing a manifest with neither tag nor digest")
	}
	if !strings.Contains(err.Error(), "neither tag nor digest") {
		t.Fatalf("expected a tag/digest error, got %v", err)
	}
}

// --- artifact_bundle.go: a configurable fault-injecting registry ------------

// ociCovFaultRegistry is a minimal OCI Distribution v2 server whose per-request
// behavior is driven by injectable faults, so a single harness can exercise
// every wire-error leg of ociPushLive/ociPullLive (bad statuses, missing
// headers, transport resets, oversize and truncated bodies) that the honest
// round-trip registry in fetcher_oci_test.go never produces.
type ociCovFaultRegistry struct {
	mu           sync.Mutex
	blobs        map[string][]byte
	manifests    map[string][]byte
	uploads      int
	blobPutCount int

	// auth
	always401 bool
	challenge string

	// push faults
	postStatus        int // override POST blobs/uploads status (0 = 202)
	omitLocation      bool
	blobPutStatus     int  // override blob PUT status (0 = 201)
	failSecondBlobPut bool // non-201 only on the 2nd blob PUT (the layer)
	hijackBlobPut     bool // reset the connection on a blob PUT
	manifestPutStatus int  // override manifest PUT status (0 = 201)
	hijackManifestPut bool

	// pull faults
	manifestGetStatus int    // override manifest GET status (0 = 200)
	manifestGetBody   []byte // override manifest GET body verbatim
	hugeManifest      bool   // serve a manifest body over the 4 MiB cap
	truncManifestGet  bool   // declare a long Content-Length then close early
	blobGetStatus     int    // override blob GET status (0 = 200)
	hugeBlob          bool   // serve a blob body over the 64 MiB cap
	truncBlobGet      bool   // declare a long Content-Length then close early
	hijackBlobGet     bool   // reset the connection on a blob GET
}

func newOCICovFaultRegistry(t *testing.T) (*httptest.Server, *ociCovFaultRegistry) {
	t.Helper()
	reg := &ociCovFaultRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(reg.handle))
	t.Cleanup(srv.Close)
	return srv, reg
}

func (r *ociCovFaultRegistry) handle(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.always401 {
		if r.challenge != "" {
			w.Header().Set("Www-Authenticate", r.challenge)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(req.URL.Path, "/v2/")
	switch {
	case strings.HasSuffix(path, "/blobs/uploads/") && req.Method == http.MethodPost:
		r.uploads++
		if r.postStatus != 0 {
			w.WriteHeader(r.postStatus)
			return
		}
		if !r.omitLocation {
			w.Header().Set("Location", fmt.Sprintf("/v2/%suploads/sess-%d", strings.TrimSuffix(path, "uploads/"), r.uploads))
		}
		w.WriteHeader(http.StatusAccepted)
	case strings.Contains(path, "/blobs/uploads/") && req.Method == http.MethodPut:
		r.blobPutCount++
		if r.hijackBlobPut {
			ociCovResetConn(w)
			return
		}
		if r.blobPutStatus != 0 || (r.failSecondBlobPut && r.blobPutCount == 2) {
			st := r.blobPutStatus
			if st == 0 {
				st = http.StatusInternalServerError
			}
			w.WriteHeader(st)
			return
		}
		digest := req.URL.Query().Get("digest")
		body := ociCovReadAll(req)
		r.blobs[digest] = body
		w.WriteHeader(http.StatusCreated)
	case strings.Contains(path, "/blobs/") && req.Method == http.MethodGet:
		r.handleBlobGet(w, path)
	case strings.Contains(path, "/manifests/") && req.Method == http.MethodPut:
		if r.hijackManifestPut {
			ociCovResetConn(w)
			return
		}
		if r.manifestPutStatus != 0 {
			w.WriteHeader(r.manifestPutStatus)
			return
		}
		ref := path[strings.LastIndex(path, "/manifests/")+len("/manifests/"):]
		r.manifests[ref] = ociCovReadAll(req)
		w.WriteHeader(http.StatusCreated)
	case strings.Contains(path, "/manifests/") && req.Method == http.MethodGet:
		r.handleManifestGet(w)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (r *ociCovFaultRegistry) handleManifestGet(w http.ResponseWriter) {
	if r.manifestGetStatus != 0 {
		w.WriteHeader(r.manifestGetStatus)
		return
	}
	if r.truncManifestGet {
		ociCovTruncatedBody(w)
		return
	}
	if r.hugeManifest {
		w.Header().Set("Content-Type", ociManifestMediaType)
		_, _ = w.Write(make([]byte, ociMaxManifestBytes+1))
		return
	}
	w.Header().Set("Content-Type", ociManifestMediaType)
	_, _ = w.Write(r.manifestGetBody)
}

func (r *ociCovFaultRegistry) handleBlobGet(w http.ResponseWriter, path string) {
	if r.hijackBlobGet {
		ociCovResetConn(w)
		return
	}
	if r.blobGetStatus != 0 {
		w.WriteHeader(r.blobGetStatus)
		return
	}
	if r.truncBlobGet {
		ociCovTruncatedBody(w)
		return
	}
	if r.hugeBlob {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(make([]byte, (DefaultBundleLimits().MaxBytes)+1))
		return
	}
	digest := path[strings.LastIndex(path, "/blobs/")+len("/blobs/"):]
	data, ok := r.blobs[digest]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func ociCovReadAll(req *http.Request) []byte {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := req.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return []byte(b.String())
}

// ociCovResetConn hijacks and immediately closes the connection so the client's
// in-flight request fails at the transport layer (no HTTP response at all).
func ociCovResetConn(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

// ociCovTruncatedBody writes an HTTP response declaring a longer Content-Length
// than the body actually sent, then closes the connection — so the client's
// io.ReadAll of the response body fails with an unexpected EOF.
func ociCovTruncatedBody(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	bw := bufio.NewWriter(conn)
	_, _ = bw.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: 4096\r\n\r\n")
	_, _ = bw.WriteString("short")
	_ = bw.Flush()
	_ = conn.Close()
}

func (r *ociCovFaultRegistry) source(srv *httptest.Server) Source {
	return Source{Type: "oci", URL: "oci://" + strings.TrimPrefix(srv.URL, "http://")}
}

// ociCovValidManifestBytes returns a well-formed single-layer manifest whose
// declared layer digest matches layerData, so ociPullLive reaches the blob GET.
func ociCovValidManifestBytes(t *testing.T, layerData []byte) []byte {
	t.Helper()
	doc := ociManifestDoc{
		SchemaVersion: 2,
		MediaType:     ociManifestMediaType,
		ArtifactType:  ociArtifactMediaType,
		Config:        ociManifestDescriptor{MediaType: ociEmptyConfigMediaType, Digest: artifactDigest([]byte("{}")), Size: 2},
		Layers:        []ociManifestDescriptor{{MediaType: ociArtifactMediaType, Digest: artifactDigest(layerData), Size: int64(len(layerData))}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshaling fixture manifest: %v", err)
	}
	return b
}

// --- artifact_bundle.go: ociPushLive fault legs -----------------------------

func TestOCICovPushFaultLegs(t *testing.T) {
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "v1.0.0"}

	cases := []struct {
		name    string
		mutate  func(*ociCovFaultRegistry)
		wantSub string
	}{
		{"post-wrong-status", func(r *ociCovFaultRegistry) { r.postStatus = http.StatusInternalServerError }, "unexpected status"},
		{"post-missing-location", func(r *ociCovFaultRegistry) { r.omitLocation = true }, "missing Location"},
		{"blob-put-wrong-status", func(r *ociCovFaultRegistry) { r.blobPutStatus = http.StatusBadRequest }, "unexpected status"},
		{"blob-put-conn-reset", func(r *ociCovFaultRegistry) { r.hijackBlobPut = true }, ""},
		{"second-blob-put-fails", func(r *ociCovFaultRegistry) { r.failSecondBlobPut = true }, ""},
		{"manifest-put-wrong-status", func(r *ociCovFaultRegistry) { r.manifestPutStatus = http.StatusBadRequest }, "unexpected status"},
		{"manifest-put-conn-reset", func(r *ociCovFaultRegistry) { r.hijackManifestPut = true }, ""},
		{"always-401-no-challenge", func(r *ociCovFaultRegistry) { r.always401 = true }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withPackagesCache(t)
			srv, reg := newOCICovFaultRegistry(t)
			tc.mutate(reg)
			_, err := PublishTree(context.Background(), reg.source(srv), parts, srcTree)
			if err == nil {
				t.Fatalf("expected a publish failure for %s", tc.name)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error to contain %q, got %v", tc.wantSub, err)
			}
		})
	}
}

// TestOCICovPushSecondBlobConnResetOnLayer forces the layer (second) blob push
// to reset, so ociPushLive's layer-blob error leg (distinct from the config
// blob) is exercised.
func TestOCICovPushSecondBlobConnResetOnLayer(t *testing.T) {
	withPackagesCache(t)
	srv, reg := newOCICovFaultRegistry(t)
	reg.failSecondBlobPut = true
	srcTree := buildFixtureTree(t)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "v1.0.0"}
	if _, err := PublishTree(context.Background(), reg.source(srv), parts, srcTree); err == nil {
		t.Fatal("expected the layer blob push to fail")
	}
}

// TestOCICovPushDeadServer covers the transport-error leg of the very first
// blob upload POST (connection refused against a closed listener).
func TestOCICovPushDeadServer(t *testing.T) {
	withPackagesCache(t)
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close() // close it so the port refuses connections
	src := Source{Type: "oci", URL: "oci://" + strings.TrimPrefix(url, "http://")}
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "v1.0.0"}
	if _, err := PublishTree(context.Background(), src, parts, buildFixtureTree(t)); err == nil {
		t.Fatal("expected a transport failure against a dead registry")
	}
}

// TestOCICovPushSecondAuthResolveFails covers ociAuthenticatedRequest's retry
// leg where the registry answers the anonymous probe with a 401 + challenge and
// the auth resolution then fails on the challenged retry. The credential helper
// returns a username+secret so the FIRST (challenge-less) resolve succeeds as an
// anonymous send; the registry's 401 carries a Bearer challenge whose realm is
// non-https, so the SECOND resolve's token exchange refuses and the error
// propagates out of the retry leg.
func TestOCICovPushSecondAuthResolveFails(t *testing.T) {
	withPackagesCache(t)
	orig := ociCredentialHelperRunner
	t.Cleanup(func() { ociCredentialHelperRunner = orig })
	ociCredentialHelperRunner = func(context.Context, string, []byte, []string) ([]byte, error) {
		return []byte(`{"username":"u","secret":"s"}`), nil
	}
	srv, reg := newOCICovFaultRegistry(t)
	reg.always401 = true
	reg.challenge = `Bearer realm="http://auth.example/token",service="reg"`
	src := reg.source(srv)
	src.Auth = json.RawMessage(`{"provider":"credential-helper","helper":"da-cov-helper"}`)
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "v1.0.0"}
	if _, err := PublishTree(context.Background(), src, parts, buildFixtureTree(t)); err == nil {
		t.Fatal("expected the challenged auth retry to fail")
	}
}

// --- artifact_bundle.go: ociPullLive fault legs -----------------------------

func TestOCICovPullFaultLegs(t *testing.T) {
	layer := []byte("layer-bytes")
	validManifest := ociCovValidManifestBytes(t, layer)

	cases := []struct {
		name   string
		mutate func(*ociCovFaultRegistry)
	}{
		{"manifest-404", func(r *ociCovFaultRegistry) { r.manifestGetStatus = http.StatusNotFound }},
		{"manifest-truncated", func(r *ociCovFaultRegistry) { r.truncManifestGet = true }},
		{"manifest-too-big", func(r *ociCovFaultRegistry) { r.hugeManifest = true }},
		{"manifest-invalid-json", func(r *ociCovFaultRegistry) { r.manifestGetBody = []byte("not json at all") }},
		{"manifest-no-layers", func(r *ociCovFaultRegistry) {
			r.manifestGetBody = []byte(`{"schemaVersion":2,"mediaType":"` + ociManifestMediaType + `","layers":[]}`)
		}},
		{"blob-404", func(r *ociCovFaultRegistry) { r.manifestGetBody = validManifest; r.blobGetStatus = http.StatusNotFound }},
		{"blob-conn-reset", func(r *ociCovFaultRegistry) { r.manifestGetBody = validManifest; r.hijackBlobGet = true }},
		{"blob-truncated", func(r *ociCovFaultRegistry) { r.manifestGetBody = validManifest; r.truncBlobGet = true }},
		{"blob-too-big", func(r *ociCovFaultRegistry) { r.manifestGetBody = validManifest; r.hugeBlob = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reg := newOCICovFaultRegistry(t)
			tc.mutate(reg)
			ref := ociRef{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "skill/x", Tag: "v1.0.0"}
			if _, err := ociPullLive(context.Background(), ref, nil); err == nil {
				t.Fatalf("expected ociPullLive to fail for %s", tc.name)
			}
		})
	}
}

// TestOCICovPullLiveMissingTagAndDigest covers ociPullLive's guard when a ref
// carries neither a tag nor a digest.
func TestOCICovPullLiveMissingTagAndDigest(t *testing.T) {
	ref := ociRef{Registry: "reg.example", Repository: "x"}
	if _, err := ociPullLive(context.Background(), ref, nil); err == nil {
		t.Fatal("expected a failure pulling a ref with neither tag nor digest")
	}
}

// --- fetcher_oci.go: direct guards and cache helpers ------------------------

// TestOCICovGuardArtifactTypeMediaMismatch covers guardOCIArtifactType's
// layer-media-type mismatch leg. pullOCIContent already rejects a non-empty
// mismatched media type before the guard runs, so this leg is only reachable by
// a direct call with a correct artifactType but a wrong descriptor media type.
func TestOCICovGuardArtifactTypeMediaMismatch(t *testing.T) {
	pulled := ociContent{ArtifactType: ociArtifactMediaType, MediaType: "application/vnd.wrong"}
	err := guardOCIArtifactType(pulled, "s:skill/x", "s")
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected a schema error for a mismatched layer media type, got %v", err)
	}
}

// TestOCICovVerifyLayerDescriptorDigestLayerMismatch covers the config-layer
// (non-artifact) branch of verifyOCILayerDescriptorDigest where a declared
// descriptor digest is present but does not match the payload.
func TestOCICovVerifyLayerDescriptorDigestLayerMismatch(t *testing.T) {
	payload := artifactDigest([]byte("payload"))
	declared := artifactDigest([]byte("something else"))
	err := verifyOCILayerDescriptorDigest(payload, declared, ociLayerMediaType, "s:x", "s")
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("expected a content mismatch error, got %v", err)
	}
	// A layer pull with an omitted descriptor digest stays tolerant (no error).
	if err := verifyOCILayerDescriptorDigest(payload, "", ociLayerMediaType, "s:x", "s"); err != nil {
		t.Fatalf("an empty descriptor digest must be tolerated on the layer path, got %v", err)
	}
}

// TestOCICovReadCachedPinnedOCIBlobDefault covers the default branch of
// readCachedPinnedOCIBlob (a media type that is neither the artifact-bundle nor
// the config-layer type falls back to the plain content-addressed read).
func TestOCICovReadCachedPinnedOCIBlobDefault(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("plain cached bytes")
	digest := artifactDigest(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatalf("seeding cache: %v", err)
	}
	got, ok := readCachedPinnedOCIBlob(digest, "application/vnd.some.other")
	if !ok || string(got) != string(blob) {
		t.Fatalf("expected a plain cache hit on the default branch, got ok=%v", ok)
	}
}

// TestOCICovWriteOCITypeSidecarMalformedDigest covers writeOCITypeSidecar's
// malformed-digest guard.
func TestOCICovWriteOCITypeSidecarMalformedDigest(t *testing.T) {
	withPackagesCache(t)
	if err := writeOCITypeSidecar("not-a-digest", ociArtifactMediaType, ociArtifactMediaType); err == nil {
		t.Fatal("expected a refusal to write a sidecar under a malformed digest")
	}
}

// TestOCICovReadOCITypeSidecarMisses covers readOCITypeSidecar's three miss
// legs: a malformed digest, an absent cache root, and a malformed JSON sidecar.
func TestOCICovReadOCITypeSidecarMisses(t *testing.T) {
	// Malformed digest — never becomes a path.
	if _, ok := readOCITypeSidecar("bad-digest"); ok {
		t.Fatal("a malformed digest must miss")
	}

	// Absent cache root — os.OpenRoot fails before any file is read.
	withPackagesCache(t)
	digest := artifactDigest([]byte("x"))
	if _, ok := readOCITypeSidecar(digest); ok {
		t.Fatal("an absent cache root must miss")
	}

	// A present but malformed-JSON sidecar is a miss, not a hard error.
	if err := writeCachedArtifact(digest, []byte("x")); err != nil {
		t.Fatalf("seeding blob: %v", err)
	}
	if err := writeConfinedPackagesCacheFile(digestDir(digest), ociTypeSidecarName, []byte("{not json")); err != nil {
		t.Fatalf("seeding sidecar: %v", err)
	}
	if _, ok := readOCITypeSidecar(digest); ok {
		t.Fatal("a malformed sidecar must miss")
	}
}

// TestOCICovWriteConfinedRenameFails covers writeConfinedPackagesCacheFile's
// rename-failure leg by pre-creating the final target name as a directory, so
// the atomic temp->name rename cannot succeed.
func TestOCICovWriteConfinedRenameFails(t *testing.T) {
	withPackagesCache(t)
	digest := artifactDigest([]byte("blob"))
	// Pre-create <cacheRoot>/<hex>/artifact.blob as a NON-empty directory.
	dirAsName := filepath.Join(packagesCacheRoot(), digestDir(digest), artifactBlobName)
	if err := os.MkdirAll(filepath.Join(dirAsName, "child"), 0o755); err != nil {
		t.Fatalf("seeding target directory: %v", err)
	}
	if err := writeCachedArtifact(digest, []byte("blob")); err == nil {
		t.Fatal("expected the rename onto an existing directory to fail")
	}
}

// TestOCICovFetchArtifactSidecarWriteFails covers FetchArtifact's leg where the
// blob write succeeds but the OCI type sidecar write fails: the oci-type.json
// target is pre-created as a directory so its atomic rename cannot complete.
func TestOCICovFetchArtifactSidecarWriteFails(t *testing.T) {
	withPackagesCache(t)
	layer := []byte("fresh oci layer bytes that are not a tarball")
	digest := artifactDigest(layer)
	// Block the sidecar write only (leave artifact.blob free to be written).
	sidecarAsDir := filepath.Join(packagesCacheRoot(), digestDir(digest), ociTypeSidecarName)
	if err := os.MkdirAll(filepath.Join(sidecarAsDir, "child"), 0o755); err != nil {
		t.Fatalf("seeding sidecar directory: %v", err)
	}
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: layer, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "v1.0.0"}
	if _, err := f.FetchArtifact(src, parts); err == nil {
		t.Fatal("expected FetchArtifact to fail when the sidecar write cannot complete")
	}
}

// TestOCICovReadConfinedCacheBlobOpenDenied covers readConfinedCacheBlob's
// os.Root.Open failure leg: a cache blob that passes the no-follow Lstat + size
// checks but is mode 0000 cannot be opened for reading (POSIX only; on Windows
// the mode does not block the owner, so the branch is simply not asserted).
func TestOCICovReadConfinedCacheBlobOpenDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode read-denial semantics")
	}
	withPackagesCache(t)
	blob := []byte("secret cache bytes")
	digest := artifactDigest(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatalf("seeding blob: %v", err)
	}
	path := cachedArtifactPath(digest)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, ok := readCachedArtifact(digest); ok {
		t.Fatal("an unreadable cache blob must be reported as a miss")
	}
}

// TestOCICovWriteConfinedOpenFileDenied covers writeConfinedPackagesCacheFile's
// temp-create failure leg: when the digest directory already exists but is not
// writable, creating the same-dir temp file is denied (POSIX only).
func TestOCICovWriteConfinedOpenFileDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix directory-write-permission semantics")
	}
	withPackagesCache(t)
	digest := artifactDigest([]byte("blob"))
	dir := filepath.Join(packagesCacheRoot(), digestDir(digest))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seeding digest dir: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := writeCachedArtifact(digest, []byte("blob")); err == nil {
		t.Fatal("expected the temp create in a read-only digest dir to fail")
	}
}

// --- oci_auth.go ------------------------------------------------------------

// TestOCICovResolveBearerCredentialTokenFile covers the TokenFile read-error and
// empty-file legs of resolveBearerCredential.
func TestOCICovResolveBearerCredentialTokenFile(t *testing.T) {
	if _, err := resolveBearerCredential(ociAuthConfig{TokenFile: filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Fatal("expected an error reading a nonexistent token file")
	}
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("writing empty token file: %v", err)
	}
	if _, err := resolveBearerCredential(ociAuthConfig{TokenFile: empty}); err == nil {
		t.Fatal("expected an error for an empty token file")
	}
	// A populated token file resolves cleanly.
	good := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(good, []byte("  a-token\n"), 0o600); err != nil {
		t.Fatalf("writing token file: %v", err)
	}
	cred, err := resolveBearerCredential(ociAuthConfig{TokenFile: good})
	if err != nil || cred.Token != "a-token" {
		t.Fatalf("expected the trimmed token, got %q err=%v", cred.Token, err)
	}
}

// TestOCICovAllowlistedHelperEnvDedup covers allowlistedHelperEnv's skip leg for
// an empty name and a name already emitted by the base allowlist.
func TestOCICovAllowlistedHelperEnvDedup(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	env := allowlistedHelperEnv([]string{"", "PATH"})
	count := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			count++
		}
		if strings.HasPrefix(kv, "=") {
			t.Fatalf("an empty name must never be forwarded, got %q", kv)
		}
	}
	if count != 1 {
		t.Fatalf("PATH must appear exactly once, got %d", count)
	}
}

// TestOCICovParseWWWAuthenticateChallengeMalformedParam covers the parser's
// skip leg for a challenge parameter that carries no '=' separator.
func TestOCICovParseWWWAuthenticateChallengeMalformedParam(t *testing.T) {
	ch, ok := parseWWWAuthenticateChallenge(`Bearer realm="https://auth.example/token",bareflag,service="reg"`)
	if !ok {
		t.Fatal("a challenge with a valid realm must still parse")
	}
	if ch.Realm != "https://auth.example/token" || ch.Service != "reg" {
		t.Fatalf("unexpected parse result: %+v", ch)
	}
}

// TestOCICovResolveAuthorizationHeaderBearerError covers the credential-resolve
// failure propagation in resolveOCIAuthorizationHeader (a bearer provider whose
// referenced env var is unset).
func TestOCICovResolveAuthorizationHeaderBearerError(t *testing.T) {
	auth := json.RawMessage(`{"provider":"bearer","token_env":"DA_COV_DEFINITELY_UNSET"}`)
	if _, err := resolveOCIAuthorizationHeader(context.Background(), auth, "reg.example", "x/y", ""); err == nil {
		t.Fatal("expected an error when the bearer token env var is unset")
	}
}

// TestOCICovExchangeBearerTokenOverTLS covers exchangeBearerToken's response
// handling legs — a successful token, a non-200 status, and an invalid JSON
// body — against an in-process TLS token endpoint (the realm must be https).
func TestOCICovExchangeBearerTokenOverTLS(t *testing.T) {
	var mode string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch mode {
		case "ok":
			_, _ = w.Write([]byte(`{"token":"issued-token"}`))
		case "access-token-alias":
			_, _ = w.Write([]byte(`{"access_token":"aliased-token"}`))
		case "status":
			w.WriteHeader(http.StatusForbidden)
		case "invalid-json":
			_, _ = w.Write([]byte("not json"))
		case "no-token":
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)

	orig := ociTokenHTTPClient
	ociTokenHTTPClient = srv.Client()
	t.Cleanup(func() { ociTokenHTTPClient = orig })

	challenge := ociAuthChallenge{Realm: srv.URL, Service: "reg", Scope: "repository:x:pull"}

	mode = "ok"
	tok, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "static"})
	if err != nil || tok != "issued-token" {
		t.Fatalf("expected issued-token, got %q err=%v", tok, err)
	}

	// The username+secret path presents HTTP Basic auth to the token endpoint.
	mode = "access-token-alias"
	tok, err = exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Username: "u", Secret: "s"})
	if err != nil || tok != "aliased-token" {
		t.Fatalf("expected the access_token alias, got %q err=%v", tok, err)
	}

	mode = "status"
	if _, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "static"}); err == nil {
		t.Fatal("expected a non-200 status to be an error")
	}

	mode = "invalid-json"
	if _, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "static"}); err == nil {
		t.Fatal("expected invalid JSON to be an error")
	}

	mode = "no-token"
	if _, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "static"}); err == nil {
		t.Fatal("expected a token-less response to be an error")
	}
}

// TestOCICovResolveAuthorizationHeaderExchangeError covers the exchange-error
// propagation leg of resolveOCIAuthorizationHeader: a credential-helper that
// returns a username+secret pair plus a Bearer challenge whose realm is not
// https, so exchangeBearerToken refuses and the error bubbles up.
func TestOCICovResolveAuthorizationHeaderExchangeError(t *testing.T) {
	orig := ociCredentialHelperRunner
	t.Cleanup(func() { ociCredentialHelperRunner = orig })
	ociCredentialHelperRunner = func(context.Context, string, []byte, []string) ([]byte, error) {
		return []byte(`{"username":"u","secret":"s"}`), nil
	}
	auth := json.RawMessage(`{"provider":"credential-helper","helper":"da-cov-helper"}`)
	// A non-https realm makes exchangeBearerToken fail after the credential
	// resolves, exercising the exchange-error return path.
	challenge := `Bearer realm="http://auth.example/token",service="reg"`
	if _, err := resolveOCIAuthorizationHeader(context.Background(), auth, "reg.example", "x/y", challenge); err == nil {
		t.Fatal("expected the non-https realm exchange to fail")
	}
}
