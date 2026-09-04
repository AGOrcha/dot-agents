package config

import (
	"archive/tar"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	gogitssh "github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/go-git/go-git/v6/storage/memory"
	cryptossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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

// TestSelectFetcherAcceptsAllSourceTypes asserts full source/kind orthogonality
// (config-distribution-model §15 D13): every source type — including oci — is
// valid for extends (config layers), mirroring SelectPackageFetcher for
// packages. There is no remaining source/kind asymmetry.
func TestSelectFetcherAcceptsAllSourceTypes(t *testing.T) {
	cases := map[string]any{
		"git":   &gitFetcher{},
		"http":  &httpFetcher{},
		"local": &localFetcher{},
		"oci":   &ociLayerFetcher{},
	}
	for typ, want := range cases {
		got, err := SelectFetcher(typ)
		if err != nil {
			t.Errorf("SelectFetcher(%q) = %v, want fetcher", typ, err)
			continue
		}
		if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", want) {
			t.Errorf("SelectFetcher(%q) = %T, want %T", typ, got, want)
		}
	}
	if _, err := SelectFetcher("bogus"); err == nil {
		t.Error("SelectFetcher(bogus) = nil, want unsupported error")
	}
}

// TestOCIFetcherRejectsLayerMediaType is the mirror of the layer fetcher's
// guard: a `packages` pull that resolves to a config-layer media type must fail
// with a schema error so a layer blob is never installed as an artifact, even
// though oci now serves both kinds (config-distribution-model §15 D13).
func TestOCIFetcherRejectsLayerMediaType(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("layer-doc-served-to-packages")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: "sha256:" + sha256Hex(blob), MediaType: ociLayerMediaType}, nil
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema media-type error, got %v", err)
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

// --- H8: packages cache temp+rename + verify-on-hit -------------------------

// TestWriteCachedArtifactIsAtomic asserts the packages cache blob is
// published via a same-dir temp+rename (fsops.WriteFileAtomic), not a plain
// truncate-and-write: the destination file never exists in a partially
// written state a concurrent reader could observe mid-write.
func TestWriteCachedArtifactIsAtomic(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("atomic-write-blob")
	digest := "sha256:" + sha256Hex(blob)
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatalf("writeCachedArtifact: %v", err)
	}
	dir := filepath.Dir(cachedArtifactPath(digest))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "artifact.blob" {
			t.Fatalf("expected only the final blob in %s, found stray entry %q (temp file leaked)", dir, e.Name())
		}
	}
	data, ok := readCachedArtifact(digest)
	if !ok || string(data) != string(blob) {
		t.Fatalf("readCachedArtifact after write = (%q, %v), want (%q, true)", data, ok, blob)
	}
}

// TestReadCachedArtifactRejectsTornOrCorruptBlob is H8's core property: a
// cache blob whose on-disk bytes do not match the digest it is stored under
// (a torn write, or on-disk tampering) is rejected on read rather than
// trusted — the caller falls through to a fresh fetch instead of ever being
// handed bytes that do not match what was asked for.
func TestReadCachedArtifactRejectsTornOrCorruptBlob(t *testing.T) {
	withPackagesCache(t)
	real := []byte("the real artifact bytes")
	digest := "sha256:" + sha256Hex(real)
	// Simulate a torn/tampered cache entry: the file exists under the
	// digest's directory, but its content does not hash to that digest.
	if err := writeCachedArtifact(digest, []byte("corrupted-different-bytes")); err != nil {
		t.Fatalf("writeCachedArtifact: %v", err)
	}
	if _, ok := readCachedArtifact(digest); ok {
		t.Fatal("expected a digest-mismatched cache entry to be rejected, not trusted")
	}
}

// TestGitArtifactFetcherPinnedCacheHitRejectsCorruptBlob proves the H8
// verify-on-hit property end-to-end through a real fetcher: a corrupt cache
// entry under a pinned digest is never returned as a cache hit — the
// fetcher instead falls through to a real clone.
func TestGitArtifactFetcherPinnedCacheHitRejectsCorruptBlob(t *testing.T) {
	withPackagesCache(t)
	real := []byte("pinned-git-blob")
	digest := "sha256:" + sha256Hex(real)
	if err := writeCachedArtifact(digest, []byte("torn-bytes-do-not-match-digest")); err != nil {
		t.Fatalf("writeCachedArtifact: %v", err)
	}
	cloned := false
	f := &gitArtifactFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		cloned = true
		return nil, nil, errors.New("expected: falls through to a real clone")
	}}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "pinned:" + digest})
	if err == nil {
		t.Fatal("expected an error once the corrupt cache entry is rejected and the fallback clone fails")
	}
	if !cloned {
		t.Fatal("expected the corrupt cache entry to be rejected, falling through to a real clone")
	}
}

// TestCacheDigestPathTraversalGuard is the audit-preempt regression: a
// malformed, attacker-influenced digest (e.g. from a `pinned:sha256:../...`
// version spec) must never be turned into a cache-path component. writes are
// refused and reads miss, so no file is created or read outside the cache root.
func TestCacheDigestPathTraversalGuard(t *testing.T) {
	withPackagesCache(t)
	for _, bad := range []string{
		"sha256:../../../../etc/passwd",
		"sha256:..",
		"sha256:" + strings.Repeat("z", 64), // right length, non-hex
		"sha256:deadbeef",                   // valid hex, wrong length
		"notadigest",
	} {
		if err := writeCachedArtifact(bad, []byte("x")); err == nil {
			t.Fatalf("writeCachedArtifact(%q) must refuse a malformed digest", bad)
		}
		if _, ok := readCachedArtifact(bad); ok {
			t.Fatalf("readCachedArtifact(%q) must miss on a malformed digest", bad)
		}
	}
	// A well-formed digest still round-trips.
	good := "sha256:" + sha256Hex([]byte("real"))
	if err := writeCachedArtifact(good, []byte("real")); err != nil {
		t.Fatalf("well-formed digest must still cache: %v", err)
	}
	if _, ok := readCachedArtifact(good); !ok {
		t.Fatal("well-formed digest must read back")
	}
}

// TestReadConfinedCacheBlobRejectsOversized is the cache-read bound regression:
// a cache blob larger than the cap is rejected (a miss) before the full read,
// while an in-cap blob reads back — the local-path discipline applied to the
// packages cache.
func TestReadConfinedCacheBlobRejectsOversized(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artifact.blob"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	if _, ok := readConfinedCacheBlob(root, "artifact.blob", 1024); ok {
		t.Fatal("expected an over-cap cache blob to be rejected before the full read")
	}
	if data, ok := readConfinedCacheBlob(root, "artifact.blob", 1<<20); !ok || len(data) != 4096 {
		t.Fatalf("expected an in-cap cache blob to read back, ok=%v len=%d", ok, len(data))
	}
}

// TestReadCachedArtifactRejectsSymlinkedCacheEntry is the cache-read identity
// regression: the cache blob is a symlink whose target's content DOES hash to
// the requested digest. The old symlink-following os.ReadFile would return that
// target as a hit; the confined no-follow read rejects it (a miss) — an
// attacker cannot substitute cache content via a symlink.
func TestReadCachedArtifactRejectsSymlinkedCacheEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	withPackagesCache(t)
	content := []byte("cached-artifact-body")
	digest := "sha256:" + sha256Hex(content)
	blobPath := cachedArtifactPath(digest)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// The real content lives outside the cache blob path; the cache "blob" is a
	// symlink to it, and its target hashes to the requested digest.
	targetDir := t.TempDir()
	target := filepath.Join(targetDir, "real")
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, blobPath); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCachedArtifact(digest); ok {
		t.Fatal("a symlinked cache entry must be rejected even when its target hashes to the digest")
	}
}

// --- oci fetcher -----------------------------------------------------------

// seedOCIArtifactCache writes both the content-addressed blob AND its OCI type
// sidecar, mirroring what a validated fresh OCI artifact pull leaves on disk.
// A digest-pinned cache hit is only trusted when the sidecar is present
// (Finding 2 / H6-on-cache-hit), so a test that wants to exercise the cache
// fast path must seed both — seeding the blob alone now (correctly) reads back
// as a miss.
func seedOCIArtifactCache(t *testing.T, digest string, blob []byte) {
	t.Helper()
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	if err := writeOCITypeSidecar(digest, ociArtifactMediaType, ociArtifactMediaType); err != nil {
		t.Fatal(err)
	}
}

func TestOCIFetcherPullsAndCaches(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	digest := "sha256:" + sha256Hex(blob)
	pulls := 0
	f := &ociFetcher{puller: func(_ context.Context, ref ociRef, _ []byte) (ociBlob, error) {
		pulls++
		if ref.Registry != "reg.example" {
			t.Fatalf("registry = %q", ref.Registry)
		}
		return ociBlob{Data: blob, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
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
	seedOCIArtifactCache(t, digest, blob)
	pulled := false
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		pulled = true
		return ociBlob{}, errors.New("should not pull")
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
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: served, MediaType: ociArtifactMediaType}, nil
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

// TestOCIFetcherRejectsMissingLayerDescriptorDigest is the round-3 item 1
// regression test: a fresh packages/artifact TAG pull whose manifest OMITS the
// layer-descriptor digest must be rejected before untar/cache, even with
// correct media labels. A well-formed OCI manifest always declares the layer
// descriptor digest, so an omission is malformed/malicious — and, critically,
// leaving it fail-open would let a MITM bypass the integrity comparison simply
// by not sending the digest. (Supersedes the old fail-OPEN
// "computes digest when registry omits" assertion.)
func TestOCIFetcherRejectsMissingLayerDescriptorDigest(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("no-digest-from-reg")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil // registry omits the layer descriptor digest
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting a manifest with no layer descriptor digest, got %v", err)
	}
	if _, ok := readCachedArtifact("sha256:" + sha256Hex(blob)); ok {
		t.Fatal("a pull with no integrity anchor must never be cached")
	}
}

func TestOCIFetcherPullError(t *testing.T) {
	withPackagesCache(t)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{}, errors.New("registry down")
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
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: "sha256:" + sha256Hex(blob), MediaType: ociArtifactMediaType}, nil
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
	seedOCIArtifactCache(t, digest, blob)
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
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: "sha256:" + sha256Hex(blob), MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestOCIPullNotWired(t *testing.T) {
	// The default (real) puller is a deterministic transport error until wired.
	_, err := ociPull(context.Background(), ociRef{Registry: "r", Repository: "x"}, nil)
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

// TestOCIFetcherPopulatesBundleFromFreshPull is the core t6 verification bar
// (CRITICAL, t3 review #5): a fresh OCI pull whose payload is a `+tar+gzip`
// artifact bundle must populate FetchedArtifact.Bundle through the H1
// fail-closed normalizer, exactly like the git/local/http fetchers, so the
// pass-2 driver's "not a directory-shaped bundle" rejection no longer fires
// for an OCI ref.
func TestOCIFetcherPopulatesBundleFromFreshPull(t *testing.T) {
	withPackagesCache(t)
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "SKILL.md", 0o644, []byte("skill body"))
		tarAddDir(t, tw, "instructions", 0o755)
		tarAddFile(t, tw, "instructions/x.md", 0o644, []byte("nested body"))
	})
	digest := "sha256:" + sha256Hex(blob)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	src := Source{Type: "oci", URL: "oci://reg.example/base"}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle == nil {
		t.Fatal("expected a Bundle for a gzip-sniffed OCI artifact payload")
	}
	if string(got.Data) != string(blob) {
		t.Fatal("expected Data to still carry the raw compressed bytes alongside Bundle")
	}
	byPath := make(map[string]BundleEntry, len(got.Bundle.Entries))
	for _, e := range got.Bundle.Entries {
		byPath[e.Path] = e
	}
	nested, ok := byPath["instructions/x.md"]
	if !ok || string(nested.Data) != "nested body" {
		t.Fatalf("expected nested instructions/x.md relative to the resource root, got paths %v", bundlePaths(*got.Bundle))
	}
}

// TestOCIFetcherPopulatesBundleFromCachedPin proves a digest-pinned OCI pull
// served from the local packages cache (no pull, no manifest to re-read) is
// still decoded into a Bundle — the cache-hit path must not silently drop the
// H1 normalization a fresh pull gets.
func TestOCIFetcherPopulatesBundleFromCachedPin(t *testing.T) {
	withPackagesCache(t)
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "SKILL.md", 0o644, []byte("cached body"))
	})
	digest := "sha256:" + sha256Hex(blob)
	seedOCIArtifactCache(t, digest, blob)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{}, errors.New("should not pull on a pinned cache hit")
	}}
	src := Source{Type: "oci", URL: "oci://reg.example"}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if !got.CacheHit {
		t.Fatal("expected a cache hit")
	}
	if got.Bundle == nil {
		t.Fatal("expected a Bundle decoded from the cached pinned payload")
	}
	if len(got.Bundle.Entries) == 0 || got.Bundle.Entries[0].Path != "SKILL.md" {
		t.Fatalf("unexpected bundle entries %+v", got.Bundle.Entries)
	}
}

// TestOCIFetcherNonGzipBodyHasNoBundle mirrors the http fetcher's legacy
// opaque-blob contract: a payload that does not sniff as gzip is not an
// error, it simply carries no Bundle (spec D3: a plain single-file artifact
// pull is not installable via packages, but the fetcher itself does not
// decide that — the pass-2 driver does).
func TestOCIFetcherNonGzipBodyHasNoBundle(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("plain single-file blob")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: artifactDigest(blob), MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	got, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle != nil {
		t.Fatalf("expected no Bundle for a non-gzip payload, got %+v", got.Bundle)
	}
}

// TestOCIFetcherRejectsAdversarialBundle is the OCI half of the H1 adversarial
// requirement, exercised through the REAL fetch path: a `../escape` entry in
// the tar payload is rejected before FetchArtifact returns, and critically,
// before the payload is ever written to the packages cache.
func TestOCIFetcherRejectsAdversarialBundle(t *testing.T) {
	withPackagesCache(t)
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
		tarAddFile(t, tw, "../escape", 0o644, []byte("evil"))
	})
	digest := "sha256:" + sha256Hex(blob)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting the adversarial tarball, got %v", err)
	}
	if _, ok := readCachedArtifact(digest); ok {
		t.Fatal("a rejected OCI bundle must not be written to the packages cache")
	}
}

// TestOCIFetcherRejectsSpoofedDigestLabel is the H5 regression test (BLOCKER:
// digest conflation): a registry (or a tampering man-in-the-middle) that
// serves DIFFERENT bytes than a pin addresses, while LABELING the blob with
// the pinned digest, must be rejected. The old "trust the reported digest
// when non-empty" shape compared the spoofed label against the pin and let
// this through; the fix recomputes SHA-256 over the actual payload, which
// never matches the pin here.
func TestOCIFetcherRejectsSpoofedDigestLabel(t *testing.T) {
	withPackagesCache(t)
	real := []byte("real-content")
	tampered := []byte("tampered-content")
	pin := "sha256:" + sha256Hex(real)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: tampered, Digest: pin, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	src := Source{URL: "oci://reg.example"}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "pinned:" + pin})
	if err == nil {
		t.Fatal("expected a digest-mismatch error from the recomputed payload hash, not the spoofed label")
	}
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error, got %v", err)
	}
	if _, ok := readCachedArtifact(pin); ok {
		t.Fatal("a payload whose bytes don't match the pin must never be cached under that pin")
	}
}

// TestOCIFetcherRejectsTamperedTagPull is the Finding 1 (BLOCKER) regression
// test: a TAG pull (no pin) whose manifest layer-descriptor declares digest D
// but whose registry serves bytes that hash to D' must be rejected BEFORE the
// bytes reach the untar decoder or the cache. The earlier shape discarded the
// declared layer-descriptor digest and only compared against a pin, so a tag
// pull had NO integrity check at all — a MITM serving tampered bytes under a
// valid tag + correct media labels flowed straight through. TestOCIFetcher-
// RejectsSpoofedDigestLabel only covered the pinned path.
func TestOCIFetcherRejectsTamperedTagPull(t *testing.T) {
	withPackagesCache(t)
	declared := []byte("what the manifest was signed for")
	tampered := []byte("what the MITM actually served")
	declaredLayerDigest := "sha256:" + sha256Hex(declared)
	tamperedDigest := "sha256:" + sha256Hex(tampered)
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		// Correct media labels, correct declared layer digest — but the bytes
		// don't match the declared digest.
		return ociBlob{Data: tampered, Digest: declaredLayerDigest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	// A plain tag version-spec: ref.Digest is empty, so there is no pin.
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting the tampered tag pull, got %v", err)
	}
	// Rejected before cache/untar: neither the tampered nor the declared digest
	// was written to the cache.
	if _, ok := readCachedArtifact(tamperedDigest); ok {
		t.Fatal("tampered tag-pull bytes must never be cached")
	}
	if _, ok := readCachedArtifact(declaredLayerDigest); ok {
		t.Fatal("nothing must be cached under the declared layer digest for a rejected pull")
	}
}

// TestWriteConfinedPackagesCacheFileRejectsSymlinkedDigestDir is the Finding 3
// regression test: a symlink pre-created at the digest dir inside the packages
// cache (an attacker-planted redirect) must not let a cache write escape the
// cache root. The confined writer rejects a symlinked digest-dir component
// outright.
func TestWriteConfinedPackagesCacheFileRejectsSymlinkedDigestDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics; junction coverage is exercised via os.Root confinement")
	}
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	digest := "sha256:" + sha256Hex([]byte("x"))
	hexDir := digest[len("sha256:"):]
	cacheRoot := filepath.Join(home, "cache", "packages")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a symlink at <cache>/<hexDir> pointing at an attacker-chosen dir
	// OUTSIDE the cache.
	victimDir := filepath.Join(home, "victim")
	if err := os.MkdirAll(victimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victimDir, filepath.Join(cacheRoot, hexDir)); err != nil {
		t.Fatal(err)
	}
	if err := writeCachedArtifact(digest, []byte("payload")); err == nil {
		t.Fatal("expected a symlinked digest dir to be rejected")
	}
	// The write must NOT have landed in the symlink target outside the cache.
	if _, err := os.Stat(filepath.Join(victimDir, "artifact.blob")); err == nil {
		t.Fatal("cache write escaped the cache root through a symlinked digest dir")
	}
}

// TestOCIFetcherRejectsMissingManifestArtifactType is an H6 negative test: a
// registry that omits the manifest-level artifactType must fail closed for a
// packages/artifact pull — the old guard tolerated an empty served media type;
// H6 removes that tolerance.
func TestOCIFetcherRejectsMissingManifestArtifactType(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: artifactDigest(blob), MediaType: ociArtifactMediaType}, nil // ArtifactType omitted
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error for a missing manifest artifactType, got %v", err)
	}
}

// TestOCIFetcherRejectsMismatchedManifestArtifactType is an H6 negative test:
// a manifest declaring a different artifactType (e.g. the config-layer type,
// which some other OCI blob in the same registry legitimately carries) must
// be rejected before it is ever treated as an artifact bundle.
func TestOCIFetcherRejectsMismatchedManifestArtifactType(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: artifactDigest(blob), MediaType: ociArtifactMediaType, ArtifactType: ociLayerMediaType}, nil
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error for a mismatched manifest artifactType, got %v", err)
	}
}

// TestOCIFetcherRejectsMissingLayerMediaType is an H6 negative test: the
// blob/descriptor's own media type must also be present — a manifest with a
// correct artifactType but a registry that omits the layer descriptor's media
// type must still fail (H6 requires BOTH checks, independently).
func TestOCIFetcherRejectsMissingLayerMediaType(t *testing.T) {
	withPackagesCache(t)
	blob := []byte("artifact-bytes")
	f := &ociFetcher{puller: func(context.Context, ociRef, []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: artifactDigest(blob), ArtifactType: ociArtifactMediaType}, nil // MediaType omitted
	}}
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error for a missing layer media type, got %v", err)
	}
}

// TestOCIFetcherCacheHitWithoutSidecarIsNotTrusted is the Finding 2 regression
// test: a valid tar seeded into the shared packages cache WITHOUT the OCI type
// sidecar (as another source type — git/local/http — or a direct seed would
// leave it) must NOT be trusted as an OCI artifact on a digest-pinned hit. The
// blob alone carries no manifest artifactType / layer media type, so the fetcher
// must treat the entry as a MISS and re-resolve the manifest. Here the puller is
// the unwired stub, so the fall-through surfaces as a transport error rather
// than silently materializing the unvalidated bytes (fail closed).
func TestOCIFetcherCacheHitWithoutSidecarIsNotTrusted(t *testing.T) {
	withPackagesCache(t)
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "SKILL.md", 0o644, []byte("seeded by another route"))
	})
	digest := "sha256:" + sha256Hex(blob)
	// Blob present, but NO sidecar — mimics a non-OCI seed of the shared cache.
	if err := writeCachedArtifact(digest, blob); err != nil {
		t.Fatal(err)
	}
	f := &ociFetcher{} // unwired puller -> any fall-through is a transport error
	_, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "pinned:" + digest})
	if err == nil {
		t.Fatal("a sidecar-less cached blob must not be trusted as an OCI artifact; expected a fall-through error")
	}
	// Corrupting the sidecar's declared type must likewise NOT be trusted.
	if err := writeOCITypeSidecar(digest, ociLayerMediaType, ociLayerMediaType); err != nil {
		t.Fatal(err)
	}
	if _, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "pinned:" + digest}); err == nil {
		t.Fatal("a cached blob whose sidecar declares the wrong type must not be trusted")
	}
	// With the correct sidecar the SAME cached blob is trusted — proving the
	// gate keys on the sidecar, not on some incidental difference.
	if err := writeOCITypeSidecar(digest, ociArtifactMediaType, ociArtifactMediaType); err != nil {
		t.Fatal(err)
	}
	got, err := f.FetchArtifact(Source{URL: "oci://reg.example"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "pinned:" + digest})
	if err != nil {
		t.Fatalf("FetchArtifact with valid sidecar: %v", err)
	}
	if !got.CacheHit || got.Bundle == nil {
		t.Fatalf("expected a trusted cache hit with a Bundle, got %+v", got)
	}
}

// TestParseOCIRefRejectsSemVerRange proves a SemVer range version-spec is
// rejected up front (spec §6: ranges stay deferred) rather than silently
// forwarded to the registry as a literal (and almost certainly wrong) tag.
func TestParseOCIRefRejectsSemVerRange(t *testing.T) {
	cases := []string{"^1.2", "~1.2.3", ">=1.0", "1.x", "1.2.0 - 1.3.0", "*"}
	src := Source{URL: "oci://reg.example/base"}
	for _, spec := range cases {
		t.Run(spec, func(t *testing.T) {
			_, err := parseOCIRef(src, PackageRefParts{ArtifactPath: "skill/x", VersionSpec: spec})
			if err == nil {
				t.Fatalf("expected a deferred-range error for version-spec %q", spec)
			}
		})
	}
}

// TestParseOCIRefAcceptsLiteralTags proves ordinary tag-shaped version specs
// (unaffected by the SemVer-range guard) still resolve as literal tags.
func TestParseOCIRefAcceptsLiteralTags(t *testing.T) {
	cases := []string{"1.2.3", "latest", "main", "v2", ""}
	src := Source{URL: "oci://reg.example/base"}
	for _, spec := range cases {
		t.Run(spec, func(t *testing.T) {
			ref, err := parseOCIRef(src, PackageRefParts{ArtifactPath: "skill/x", VersionSpec: spec})
			if err != nil {
				t.Fatalf("parseOCIRef(%q): unexpected error %v", spec, err)
			}
			if ref.Tag != spec || ref.Digest != "" {
				t.Fatalf("parseOCIRef(%q) = %+v, want Tag=%q Digest=\"\"", spec, ref, spec)
			}
		})
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

// --- http artifact fetcher: tarball layout (H1) -----------------------------

func httpTestServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHTTPArtifactFetcherTarballSurvivesStructure is the tarball half of the
// task's core verification bar: a gzip-sniffed http artifact body is
// normalized into a Bundle whose paths are relative to the resource root,
// with dirs, modes, and dotfiles surviving.
func TestHTTPArtifactFetcherTarballSurvivesStructure(t *testing.T) {
	withPackagesCache(t)
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "SKILL.md", 0o644, []byte("skill body"))
		tarAddDir(t, tw, "instructions", 0o755)
		tarAddFile(t, tw, "instructions/x.md", 0o644, []byte("nested body"))
		tarAddFile(t, tw, ".env", 0o600, []byte(""))
	})
	srv := httpTestServer(t, blob)

	f := &httpArtifactFetcher{client: srv.Client()}
	got, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle == nil {
		t.Fatal("expected a Bundle for a gzip-sniffed tarball body")
	}
	if string(got.Data) != string(blob) {
		t.Fatal("expected Data to still carry the raw compressed bytes alongside Bundle")
	}

	byPath := make(map[string]BundleEntry, len(got.Bundle.Entries))
	for _, e := range got.Bundle.Entries {
		byPath[e.Path] = e
	}
	nested, ok := byPath["instructions/x.md"]
	if !ok || string(nested.Data) != "nested body" {
		t.Fatalf("expected nested instructions/x.md relative to the resource root, got paths %v", bundlePaths(*got.Bundle))
	}
	if dir, ok := byPath["instructions"]; !ok || !dir.IsDir {
		t.Fatalf("expected instructions dir entry to survive, got %+v", dir)
	}
	if _, ok := byPath[".env"]; !ok {
		t.Fatalf("expected dotfile .env to survive, paths %v", bundlePaths(*got.Bundle))
	}
}

// TestHTTPArtifactFetcherTarballRejectsAdversarialEntries is the http half of
// the task's adversarial requirement, exercised through the REAL fetch path
// (not just UntarBundle directly): a `../escape` entry, an absolute path, and
// a symlink leaving the resource dir are each rejected before FetchArtifact
// returns — and critically, before the fetched blob is ever written to the
// packages cache, so a failed validation leaves no trace on disk.
func TestHTTPArtifactFetcherTarballRejectsAdversarialEntries(t *testing.T) {
	cases := []struct {
		name string
		add  func(tw *tar.Writer)
	}{
		{
			name: "path traversal escape",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddFile(t, tw, "../escape", 0o644, []byte("evil"))
			},
		},
		{
			name: "absolute path",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddFile(t, tw, "/etc/passwd", 0o644, []byte("evil"))
			},
		},
		{
			name: "symlink leaving the resource dir",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddSymlink(t, tw, "escape-link", "../../../etc/passwd")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withPackagesCache(t)
			blob := buildTarGz(t, tc.add)
			digest := "sha256:" + sha256Hex(blob)
			srv := httpTestServer(t, blob)

			f := &httpArtifactFetcher{client: srv.Client()}
			_, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
			var ie *ImportError
			if !errors.As(err, &ie) || ie.Reason != ReasonContent {
				t.Fatalf("want content error rejecting the adversarial tarball, got %v", err)
			}
			if _, ok := readCachedArtifact(digest); ok {
				t.Fatal("a rejected tarball must not be written to the packages cache")
			}
		})
	}
}

func TestHTTPArtifactFetcherNonGzipBodyHasNoBundle(t *testing.T) {
	withPackagesCache(t)
	srv := httpTestServer(t, []byte("plain single-file blob"))
	f := &httpArtifactFetcher{client: srv.Client()}
	got, err := f.FetchArtifact(Source{URL: srv.URL}, PackageRefParts{SourceID: "s", ArtifactPath: "a", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle != nil {
		t.Fatalf("expected no Bundle for a non-gzip legacy blob, got %+v", got.Bundle)
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

// TestSelectFetcherSourceTypes asserts every known source type — including oci
// after D13 — selects a fetcher, and an unknown type is rejected. Acceptance of
// each concrete fetcher type is asserted by TestSelectFetcherAcceptsAllSourceTypes.
func TestSelectFetcherSourceTypes(t *testing.T) {
	for _, typ := range []string{"git", "http", "local", "oci"} {
		if _, err := SelectFetcher(typ); err != nil {
			t.Errorf("SelectFetcher(%q) = error %v, want fetcher", typ, err)
		}
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

	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
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
	if _, ok := readCachedUnit(cacheDir, got.ResolvedSHA); !ok {
		t.Fatal("expected layer written to cache")
	}
}

func TestLocalFetcherMissing(t *testing.T) {
	f := &localFetcher{}
	_, err := f.Fetch(Source{Type: "local", Path: t.TempDir()}, LayerRefParts{LayerPath: "nope.json"}, FetchTarget{Dir: t.TempDir()})
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

	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
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
	_, err := f.Fetch(Source{Type: "http", URL: "http://insecure.example/"}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: t.TempDir()})
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
	_, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: t.TempDir()})
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

	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
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
	got, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: t.TempDir()})
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
	_, err := f.Fetch(Source{Type: "git", URL: "ht!tp://%zz"}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for malformed git url")
	}
}

func TestGitFetcherMissingRef(t *testing.T) {
	url, _, _ := makeGitFixture(t, "base.json", []byte("{}"))
	f := &gitFetcher{}
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: "no-such-branch"}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error cloning a non-existent ref")
	}
}

func TestGitFetcherMissingLayerPath(t *testing.T) {
	url, branch, _ := makeGitFixture(t, "base.json", []byte("{}"))
	f := &gitFetcher{}
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "nope.json"}, FetchTarget{Dir: t.TempDir()})
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
	_, err := f.Fetch(Source{Type: "git", URL: url, Ref: branch}, LayerRefParts{LayerPath: "big.json"}, FetchTarget{Dir: t.TempDir()})
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
	_, err := f.Fetch(Source{Type: "git", URL: "file:///x", Ref: "main"}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error when HEAD cannot be resolved")
	}
}

func TestGitFetcherCloneError(t *testing.T) {
	f := &gitFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		return nil, nil, errors.New("clone boom")
	}}
	_, err := f.Fetch(Source{Type: "git", URL: "file:///x", Ref: "main"}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: t.TempDir()})
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
	if err := writeCachedUnit(FetchTarget{Dir: filepath.Join(f, "sub")}, "sha", []byte("{}")); err == nil {
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
	_, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: filepath.Join(blocker, "cache")})
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
	_, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: filepath.Join(blocker, "cache")})
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
	_, err := f.Fetch(Source{Type: "git", URL: "file:///r", Ref: "main"}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: filepath.Join(blocker, "cache")})
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
	_, err := f.Fetch(Source{Type: "git", URL: "file:///r", Ref: "main"}, LayerRefParts{LayerPath: "missing.json"}, FetchTarget{Dir: t.TempDir()})
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
	got, err := f.Fetch(Source{Type: "http", URL: srv.URL}, LayerRefParts{LayerPath: "org/base.json"}, FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")})
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
	got, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")})
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
	got2, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: filepath.Join(t.TempDir(), "cache2")})
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
	f := &ociFetcher{puller: func(_ context.Context, _ ociRef, _ []byte) (ociBlob, error) {
		return ociBlob{Data: blob, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
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

func (f *refreshFakeFetcher) Fetch(src Source, parts LayerRefParts, target FetchTarget) (FetchedLayer, error) {
	return f.FetchRefresh(src, parts, target, false)
}

func (f *refreshFakeFetcher) FetchRefresh(_ Source, parts LayerRefParts, target FetchTarget, forceRefresh bool) (FetchedLayer, error) {
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
		if cached, ok := readCachedUnit(target, sha); ok {
			return FetchedLayer{Data: cached, ResolvedSHA: sha, CacheHit: true, KeyInputs: CacheKeyInputs{ResolvedCommit: sha}}, nil
		}
	}
	if err := writeCachedUnit(target, sha, []byte(body)); err != nil {
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
	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
	src := Source{Type: "git", URL: url, Ref: branch}
	parts := LayerRefParts{LayerPath: "org/base.json"}
	// Pre-seed the cache at the resolved SHA with STALE bytes the fetcher would
	// serve on a normal fetch.
	stale := []byte(`{"skills":["stale"]}`)
	if err := writeCachedUnit(cacheDir, sha, stale); err != nil {
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
	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
	sha := contentHash([]byte(body))
	if err := writeCachedUnit(cacheDir, sha, []byte(body)); err != nil {
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
	cacheDir := FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")}
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

// --- git artifact fetcher: tree layout (H1) ---------------------------------

// populatedRepoFS opens the on-disk fixture at dir (for HEAD resolution, as
// committedRepoFS does) and returns an independent memfs the test populates
// directly via setup, so a test can build an arbitrary worktree tree
// (nested dirs, dotfiles, symlinks) without needing real git plumbing for
// exotic entry types.
func populatedRepoFS(t *testing.T, dir string, setup func(fs billy.Filesystem)) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.PlainOpen(dir)
		if err != nil {
			return nil, nil, err
		}
		wfs := memfs.New()
		setup(wfs)
		return repo, wfs, nil
	}
}

// memfsWriteFile creates name (and its implicit parent dirs) in fs with body.
func memfsWriteFile(t *testing.T, fs billy.Filesystem, name string, body []byte) {
	t.Helper()
	fh, err := fs.Create(name)
	if err != nil {
		t.Fatalf("memfs Create(%s): %v", name, err)
	}
	if _, err := fh.Write(body); err != nil {
		t.Fatalf("memfs Write(%s): %v", name, err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("memfs Close(%s): %v", name, err)
	}
}

// TestGitArtifactFetcherTreeLayoutSurvivesStructure is the tree-layout half
// of the task's core verification bar: a git-subtree pull yields a Bundle
// with paths relative to the resource root (nested instructions/x.md
// appears as instructions/x.md), and dirs + modes + dotfiles survive.
func TestGitArtifactFetcherTreeLayoutSurvivesStructure(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: gitTreeFixtureCloner(t, map[string][]byte{
		"skill/SKILL.md":          []byte("skill body"),
		"skill/instructions/x.md": []byte("nested body"),
		"skill/.env":              []byte(""),
	}, nil)}

	got, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle == nil {
		t.Fatal("expected a Bundle for a directory artifact path")
	}
	if got.Data != nil {
		t.Fatalf("expected no single-blob Data for a tree-layout pull, got %q", got.Data)
	}

	byPath := make(map[string]BundleEntry, len(got.Bundle.Entries))
	for _, e := range got.Bundle.Entries {
		byPath[e.Path] = e
	}
	nested, ok := byPath["instructions/x.md"]
	if !ok || string(nested.Data) != "nested body" {
		t.Fatalf("expected nested instructions/x.md relative to the resource root, got paths %v", bundlePaths(*got.Bundle))
	}
	if dir, ok := byPath["instructions"]; !ok || !dir.IsDir {
		t.Fatalf("expected instructions dir entry to survive, got %+v", dir)
	}
	if _, ok := byPath[".env"]; !ok {
		t.Fatalf("expected dotfile .env to survive, paths %v", bundlePaths(*got.Bundle))
	}
	if skill, ok := byPath["SKILL.md"]; !ok || string(skill.Data) != "skill body" {
		t.Fatalf("expected SKILL.md content to survive, got %+v", skill)
	}
	if got.Digest != BundleDigest(*got.Bundle) {
		t.Fatalf("digest = %q, want BundleDigest(%+v)", got.Digest, got.Bundle)
	}
}

// TestGitArtifactFetcherTreeLayoutRejectsSymlinkEscape is the git half of
// the task's adversarial requirement: a symlink anywhere in the subtree
// (here pointing outside the resource dir) is rejected before the pull
// succeeds, and no Bundle is returned.
func TestGitArtifactFetcherTreeLayoutRejectsSymlinkEscape(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: gitTreeFixtureCloner(t, map[string][]byte{
		"skill/SKILL.md": []byte("skill body"),
	}, map[string]string{
		"skill/escape-link": "../../../etc/passwd",
	})}

	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting the symlink, got %v", err)
	}
}

// TestGitArtifactFetcherTreeLayoutPinnedDigestMismatch asserts a
// "pinned:sha256:..." version spec pins the WHOLE subtree's BundleDigest: a
// pull whose subtree hashes to something else is a content error, not a
// silently-served mismatch.
func TestGitArtifactFetcherTreeLayoutPinnedDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "skill/SKILL.md", []byte("root"))
	f := &gitArtifactFetcher{cloner: populatedRepoFS(t, dir, func(fs billy.Filesystem) {
		memfsWriteFile(t, fs, "skill/SKILL.md", []byte("skill body"))
	})}

	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "pinned:sha256:deadbeef"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error for a pinned tree-layout digest mismatch, got %v", err)
	}
}

// TestGitArtifactFetcherTreeLayoutRequiredPostureFails asserts the signing
// posture stub applies to tree-layout pulls exactly as it does to a
// single-file pull.
func TestGitArtifactFetcherTreeLayoutRequiredPostureFails(t *testing.T) {
	withPackagesCache(t)
	dir := t.TempDir()
	makeGitFixtureAt(t, dir, "skill/SKILL.md", []byte("root"))
	f := &gitArtifactFetcher{cloner: populatedRepoFS(t, dir, func(fs billy.Filesystem) {
		memfsWriteFile(t, fs, "skill/SKILL.md", []byte("skill body"))
	})}

	src := Source{Type: "git", URL: "file:///r", Ref: "main", Auth: json.RawMessage(`{"signing":"required"}`)}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	if err == nil {
		t.Fatal("required posture must fail an unsigned tree-layout pull")
	}
}

// submoduleCloner builds an in-memory repo whose committed tree at subPath
// contains a gitlink (filemode.Submodule) entry, paired with a worktree memfs
// that mimics go-git flattening that submodule into an empty directory — the
// exact shape defect #4 exploits. The returned cloner drives the real
// gitArtifactFetcher path (cloneAndResolve loads the commit tree from the same
// storer).
func submoduleCloner(t *testing.T) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	st := memory.NewStorage()

	blob := st.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("skill body")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	blobHash, err := st.SetEncodedObject(blob)
	if err != nil {
		t.Fatal(err)
	}

	// A gitlink points at some commit OID that need not exist in this store.
	subCommit := plumbing.NewHash("1111111111111111111111111111111111111111")

	skillTree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "SKILL.md", Mode: filemode.Regular, Hash: blobHash},
		{Name: "vendored", Mode: filemode.Submodule, Hash: subCommit},
	}}
	skillHash := encodeTree(t, st, skillTree)

	rootTree := &object.Tree{Entries: []object.TreeEntry{
		{Name: "skill", Mode: filemode.Dir, Hash: skillHash},
	}}
	rootHash := encodeTree(t, st, rootTree)

	sig := object.Signature{Name: "t", Email: "t@example"}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "with submodule", TreeHash: rootHash}
	commitObj := st.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := st.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commitHash)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}

	wfs := memfs.New()
	memfsWriteFile(t, wfs, "skill/SKILL.md", []byte("skill body"))
	// go-git flattens the gitlink into an empty directory in the checkout.
	if err := wfs.MkdirAll("skill/vendored", 0o755); err != nil {
		t.Fatal(err)
	}

	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.Open(st, wfs)
		if err != nil {
			return nil, nil, err
		}
		return repo, wfs, nil
	}
}

func encodeTree(t *testing.T, st *memory.Storage, tree *object.Tree) plumbing.Hash {
	t.Helper()
	obj := st.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		t.Fatal(err)
	}
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// treeFixtureNode is one node (file, symlink, or dir) in the tree a
// buildCommittedTree call assembles. A dir node's Children key is the child's
// path segment; a leaf node (Data non-nil or Symlink set) has no children.
type treeFixtureNode struct {
	Data     []byte // non-nil for a regular file (may be empty)
	Symlink  string // non-empty target for a symlink leaf
	Children map[string]*treeFixtureNode
}

// buildCommittedTree writes files (path -> content) and symlinks (path ->
// target) directly into st's object graph as real committed blob/tree
// objects — no memfs, no real disk I/O — and returns the root tree's hash.
// It is the t1b test counterpart to the git-ingestion path's switch to
// committed-tree/blob reads (gitCommittedTreeWalker): unlike the worktree-fs
// fixtures used elsewhere in this file (populatedRepoFS et al.), every path a
// test declares here is REAL committed content, so a tree-based content
// reader sees exactly the shape the test intends.
func buildCommittedTree(t *testing.T, st *memory.Storage, files map[string][]byte, symlinks map[string]string) plumbing.Hash {
	t.Helper()
	root := &treeFixtureNode{Children: map[string]*treeFixtureNode{}}
	for p, body := range files {
		insertTreeFixtureNode(root, p, &treeFixtureNode{Data: body})
	}
	for p, target := range symlinks {
		insertTreeFixtureNode(root, p, &treeFixtureNode{Symlink: target})
	}
	return encodeFixtureTree(t, st, root)
}

// insertTreeFixtureNode places leaf at path p (slash-separated) under root,
// creating intermediate directory nodes as needed.
func insertTreeFixtureNode(root *treeFixtureNode, p string, leaf *treeFixtureNode) {
	parts := strings.Split(p, "/")
	cur := root
	for i, part := range parts {
		if i == len(parts)-1 {
			cur.Children[part] = leaf
			return
		}
		next, ok := cur.Children[part]
		if !ok || next.Children == nil {
			next = &treeFixtureNode{Children: map[string]*treeFixtureNode{}}
			cur.Children[part] = next
		}
		cur = next
	}
}

// writeFixtureBlob stores body as a git blob object and returns its hash.
func writeFixtureBlob(t *testing.T, st *memory.Storage, body []byte) plumbing.Hash {
	blob := st.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	h, err := st.SetEncodedObject(blob)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fixtureTreeSortKey returns name with a trailing slash for directory nodes so
// entries sort as Git's tree encoding requires (treeEntrySortName).
func fixtureTreeSortKey(n *treeFixtureNode, name string) string {
	if n.Symlink == "" && n.Data == nil {
		return name + "/"
	}
	return name
}

// encodeFixtureTree recursively encodes n and its children into git tree/blob
// objects in st, returning the root tree hash.
func encodeFixtureTree(t *testing.T, st *memory.Storage, n *treeFixtureNode) plumbing.Hash {
	names := make([]string, 0, len(n.Children))
	for name := range n.Children {
		names = append(names, name)
	}
	// Git's tree encoding requires entries sorted as if a directory name
	// carried a trailing slash (treeEntrySortName) — sort accordingly so
	// Tree.Validate (called by Encode) accepts the constructed tree.
	sort.Slice(names, func(i, j int) bool {
		return fixtureTreeSortKey(n.Children[names[i]], names[i]) < fixtureTreeSortKey(n.Children[names[j]], names[j])
	})
	tree := &object.Tree{}
	for _, name := range names {
		child := n.Children[name]
		switch {
		case child.Symlink != "":
			tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: filemode.Symlink, Hash: writeFixtureBlob(t, st, []byte(child.Symlink))})
		case child.Data != nil:
			tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: filemode.Regular, Hash: writeFixtureBlob(t, st, child.Data)})
		default:
			tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: encodeFixtureTree(t, st, child)})
		}
	}
	return encodeTree(t, st, tree)
}

// gitTreeFixtureCloner returns a cloner (the gitArtifactFetcher.cloner test
// seam) whose repo's HEAD resolves to a commit over a tree built from files
// and symlinks by buildCommittedTree. No memfs is populated: with content
// reads sourced from the committed tree/blob graph (t1b), the worktree
// filesystem is never consulted for a tree-layout pull, so an empty memfs is
// sufficient here (FetchArtifact's directory-vs-file Lstat classification
// still needs the artifact root to exist somewhere in the worktree fs for a
// single-file pull, but every caller of this helper exercises a tree-layout
// pull, which classifies via the committed tree once inside fetchTreeBundle).
func gitTreeFixtureCloner(t *testing.T, files map[string][]byte, symlinks map[string]string) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	st := memory.NewStorage()
	rootHash := buildCommittedTree(t, st, files, symlinks)

	sig := object.Signature{Name: "t", Email: "t@example"}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "fixture", TreeHash: rootHash}
	commitObj := st.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := st.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commitHash)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}

	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		wfs := memfs.New()
		// The top-level artifact path must exist in the worktree fs as a
		// directory for FetchArtifact's initial Lstat classification — content
		// underneath it is irrelevant since fetchTreeBundle reads the committed
		// tree, not this fs.
		if err := wfs.MkdirAll("skill", 0o755); err != nil {
			return nil, nil, err
		}
		repo, err := gogit.Open(st, wfs)
		if err != nil {
			return nil, nil, err
		}
		return repo, wfs, nil
	}
}

// TestGitArtifactFetcherTreeLayoutRejectsSubmodule is the defect #4
// regression: the committed subtree carries a gitlink (mode 160000) that
// go-git flattened into an empty worktree directory. A worktree-only walk
// would silently drop the gitlink OID (letting two different referenced
// commits yield the same BundleDigest); inspecting the committed tree modes
// catches and rejects it. The two cases cover a submodule NESTED under the
// artifact dir (the recursive tree-walk branch) and a submodule that IS the
// artifact path (the FindEntry branch).
func TestGitArtifactFetcherTreeLayoutRejectsSubmodule(t *testing.T) {
	cases := map[string]string{
		"submodule nested under the artifact dir": "skill",
		"submodule is the artifact path":          "skill/vendored",
		// Defect #3: noncanonical artifact paths resolve to the gitlink in the
		// worktree memfs but must not slip past the committed-tree lookup.
		"noncanonical dot-slit path":     "./skill/vendored",
		"noncanonical dotdot round-trip": "skill/../skill/vendored",
		"noncanonical repeated-sep path": "skill//vendored",
	}
	for name, artifactPath := range cases {
		t.Run(name, func(t *testing.T) {
			withPackagesCache(t)
			f := &gitArtifactFetcher{cloner: submoduleCloner(t)}
			_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: artifactPath, VersionSpec: "1"})
			var ie *ImportError
			if !errors.As(err, &ie) || ie.Reason != ReasonContent {
				t.Fatalf("want content error rejecting a git submodule (gitlink) entry, got %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), "submodule") {
				t.Fatalf("error should name the submodule defect, got %v", err)
			}
		})
	}
}

// TestGitCommittedTreeWalkerEnforcesPerFileCap is the defect #4 regression
// (t1b re-shape: the walker now reads from the committed tree, not the
// worktree): the per-file cap must hold on the git ingestion path too — an
// oversized committed blob is rejected AT the walker (before the full read,
// via the blob's declared Size), not only later by the accumulator. The
// error naming "git tree file" proves the pre-read walker-level rejection.
func TestGitCommittedTreeWalkerEnforcesPerFileCap(t *testing.T) {
	st := memory.NewStorage()
	rootHash := buildCommittedTree(t, st, map[string][]byte{"big.txt": make([]byte, 4096)}, nil)
	limits := BundleLimits{MaxEntries: 10, MaxFiles: 10, MaxFileBytes: 1024, MaxBytes: 1 << 20, MaxStreamBytes: 1 << 20, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20}
	_, err := NormalizeBundle(gitCommittedTreeWalker(st, rootHash, limits), limits)
	if err == nil {
		t.Fatal("expected rejection of an oversized committed file on the git path")
	}
	if !strings.Contains(err.Error(), "git tree file") || !strings.Contains(err.Error(), "per-file cap") {
		t.Fatalf("expected a walker-level per-file-cap rejection, got %v", err)
	}
}

// TestGitArtifactFetcherTreeLayoutRejectsOversizedBlob is the t1b end-to-end
// adversarial test: a committed blob larger than BundleLimits.MaxFileBytes
// inside a tree-layout pull is rejected by FetchArtifact itself (real
// DefaultBundleLimits, not an injected tiny cap), proving the walker-level
// per-file bound (TestGitCommittedTreeWalkerEnforcesPerFileCap) holds through
// the full FetchArtifact -> fetchTreeBundle path, and that the blob's
// declared Size is checked before its content is read (readCommittedBlobFile)
// rather than after an unbounded read.
func TestGitArtifactFetcherTreeLayoutRejectsOversizedBlob(t *testing.T) {
	withPackagesCache(t)
	big := make([]byte, DefaultBundleLimits().MaxFileBytes+1)
	f := &gitArtifactFetcher{cloner: gitTreeFixtureCloner(t, map[string][]byte{
		"skill/big.bin": big,
	}, nil)}

	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting an oversized committed blob, got %v", err)
	}
	if !strings.Contains(err.Error(), "per-file cap") {
		t.Fatalf("expected a per-file-cap rejection, got %v", err)
	}
}

// TestGitCommittedTreeWalkerRejectsEntryFlood is the git half of the t1b
// directory-flood adversarial test: the committed-tree walk streams entries
// one at a time as it recurses, so the accumulator's MaxEntries cap trips
// within one entry's worth of over-read rather than after the whole subtree
// is buffered. A small actual entry count against a tiny injected cap
// exercises the SAME mechanism a real multi-million-entry flood would hit, at
// test speed.
func TestGitCommittedTreeWalkerRejectsEntryFlood(t *testing.T) {
	st := memory.NewStorage()
	files := make(map[string][]byte, 50)
	for i := 0; i < 50; i++ {
		files[fmt.Sprintf("f%03d.txt", i)] = []byte("x")
	}
	rootHash := buildCommittedTree(t, st, files, nil)
	limits := BundleLimits{MaxEntries: 10, MaxFiles: 100, MaxFileBytes: 1 << 20, MaxBytes: 1 << 20, MaxStreamBytes: 1 << 20, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20}
	_, err := NormalizeBundle(gitCommittedTreeWalker(st, rootHash, limits), limits)
	if err == nil {
		t.Fatal("expected rejection of a git tree entry-count flood")
	}
	if !strings.Contains(err.Error(), "entry-count cap") {
		t.Fatalf("expected an entry-count-cap rejection, got %v", err)
	}
}

// TestGitCommittedTreeWalkerRejectsFlatTreeBeforeDecode is the t1b-review
// flat-tree-decode bound: object.GetTree materializes a whole tree object's
// entry slice into memory, so the walker MUST reject an oversized (flooded)
// tree OBJECT via its pre-decode encoded-size gate before GetTree ever runs —
// not merely via the post-decode MaxEntries cap. A directory with many entries
// is built so its single tree object's encoded size exceeds the derived
// tree-object cap; the walker rejects it with the "tree-object cap" reason,
// proving the gate fired before decode. (Encoded size is checked via
// EncodedObjectSize, which reads the object header without inflating it.)
func TestGitCommittedTreeWalkerRejectsFlatTreeBeforeDecode(t *testing.T) {
	st := memory.NewStorage()
	// Enough entries that the tree object's encoded size (~28+ bytes/entry)
	// clears the tiny tree-object cap derived below (MaxEntries*32 +
	// MaxTotalPathBytes = 4*32 + 256 = 384 bytes).
	files := make(map[string][]byte, 200)
	for i := 0; i < 200; i++ {
		files[fmt.Sprintf("f%04d.txt", i)] = []byte("x")
	}
	rootHash := buildCommittedTree(t, st, files, nil)
	limits := BundleLimits{MaxEntries: 4, MaxFiles: 1000, MaxFileBytes: 1 << 20, MaxBytes: 1 << 20, MaxStreamBytes: 1 << 20, MaxPathBytes: 4096, MaxTotalPathBytes: 256}
	_, err := NormalizeBundle(gitCommittedTreeWalker(st, rootHash, limits), limits)
	if err == nil {
		t.Fatal("expected pre-decode rejection of an oversized flat tree object")
	}
	if !strings.Contains(err.Error(), "tree-object cap") {
		t.Fatalf("expected a pre-decode tree-object-size rejection, got %v", err)
	}
}

// unresolvableTreeCloner builds a repo whose HEAD resolves to a commit hash
// that has NO commit object in storage — so repo.Head() succeeds but
// CommitObject fails, leaving the committed tree unresolvable. The worktree
// memfs still shows a "skill" directory, reproducing the gitlink error-path
// bypass: a directory artifact whose committed tree cannot be resolved must
// fail closed rather than skip the submodule check.
func unresolvableTreeCloner(t *testing.T) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	st := memory.NewStorage()
	dangling := plumbing.NewHash("2222222222222222222222222222222222222222")
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", dangling)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}
	wfs := memfs.New()
	memfsWriteFile(t, wfs, "skill/SKILL.md", []byte("skill body"))
	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.Open(st, wfs)
		if err != nil {
			return nil, nil, err
		}
		return repo, wfs, nil
	}
}

// TestGitArtifactFetcherDirectoryRejectsUnresolvableTree is the gitlink
// error-path regression: when the committed-tree lookup fails, a directory
// artifact must fail closed (the submodule check cannot run without the tree)
// rather than silently walk the worktree and succeed.
func TestGitArtifactFetcherDirectoryRejectsUnresolvableTree(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: unresolvableTreeCloner(t)}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error failing closed on an unresolvable committed tree, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "committed tree") {
		t.Fatalf("error should name the unresolvable committed tree, got %v", err)
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
	if runtime.GOOS == "windows" {
		t.Skip("owner-permission bits do not restrict reads the same way on windows")
	}
	withPackagesCache(t)
	// A permission-denied file (not a missing one) makes os.ReadFile fail
	// with a non-IsNotExist error, covering the content-read error branch.
	srcDir := t.TempDir()
	blocked := filepath.Join(srcDir, "blocked.json")
	if err := os.WriteFile(blocked, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o644) })
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "blocked.json", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error reading a permission-denied file, got %v", err)
	}
}

// --- local artifact fetcher: tree layout (H1, mirrors git) -----------------

// TestLocalArtifactFetcherTreeLayoutSurvivesStructure asserts a local-source
// directory subtree mirrors the git tree-layout contract: nested paths
// relative to the resource root, dirs, modes, and dotfiles all survive.
func TestLocalArtifactFetcherTreeLayoutSurvivesStructure(t *testing.T) {
	withPackagesCache(t)
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(filepath.Join(skillDir, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("skill body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "instructions", "x.md"), []byte("nested body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	got, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	if got.Bundle == nil {
		t.Fatal("expected a Bundle for a directory artifact path")
	}

	byPath := make(map[string]BundleEntry, len(got.Bundle.Entries))
	for _, e := range got.Bundle.Entries {
		byPath[e.Path] = e
	}
	nested, ok := byPath["instructions/x.md"]
	if !ok || string(nested.Data) != "nested body" {
		t.Fatalf("expected nested instructions/x.md relative to the resource root, got paths %v", bundlePaths(*got.Bundle))
	}
	if dir, ok := byPath["instructions"]; !ok || !dir.IsDir {
		t.Fatalf("expected instructions dir entry to survive, got %+v", dir)
	}
	if _, ok := byPath[".env"]; !ok {
		t.Fatalf("expected dotfile .env to survive, paths %v", bundlePaths(*got.Bundle))
	}
}

// TestLocalArtifactFetcherTreeLayoutRejectsSymlinkEscape mirrors the git
// adversarial case: a symlink anywhere in the local subtree is rejected
// before the pull succeeds, regardless of what it points to.
func TestLocalArtifactFetcherTreeLayoutRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	withPackagesCache(t)
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("skill body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(skillDir, "escape-link")); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting the symlink, got %v", err)
	}
}

// TestLocalArtifactFetcherRejectsParentEscape is the defect #1 regression:
// Source.Path is a real root and ArtifactPath walks OUT of it via "..".
// The subpath is rejected before any filesystem join, so the secret that
// lives outside the root is never opened or read.
func TestLocalArtifactFetcherRejectsParentEscape(t *testing.T) {
	withPackagesCache(t)
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "hostname"), []byte("SECRET-OUTSIDE-ROOT"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(outer, "root")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	for _, escape := range []string{"../hostname", "../../etc/hostname"} {
		got, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: escape, VersionSpec: "1"})
		var ie *ImportError
		if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
			t.Fatalf("ArtifactPath %q: want schema error rejecting the parent escape before any join, got %v", escape, err)
		}
		if got.Data != nil || got.Bundle != nil {
			t.Fatalf("ArtifactPath %q: nothing outside the root may be read", escape)
		}
		if strings.Contains(string(got.Data), "SECRET") {
			t.Fatalf("ArtifactPath %q: leaked out-of-root content", escape)
		}
	}
}

// TestLocalArtifactFetcherRejectsSymlinkComponentEscape is the defect #1
// intermediate-symlink half: a symlink INSIDE the root points OUT of it, and
// an artifact path threaded through that component is refused by the os.Root
// confinement — an Lstat that only checked the final component would have
// followed it.
func TestLocalArtifactFetcherRejectsSymlinkComponentEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	withPackagesCache(t)
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "hostname"), []byte("SECRET-OUTSIDE-ROOT"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(outer, "root")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A symlink inside the root that escapes to the parent directory.
	if err := os.Symlink(outer, filepath.Join(srcDir, "out")); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	got, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "out/hostname", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || (ie.Reason != ReasonContent && ie.Reason != ReasonNotFound) {
		t.Fatalf("want content/not-found rejection of an escape via an intermediate symlink, got %v", err)
	}
	if strings.Contains(string(got.Data), "SECRET") || got.Bundle != nil {
		t.Fatal("nothing reached through the escaping symlink may be read")
	}
}

// TestLocalArtifactFetcherRejectsSymlinkRoot is the defect #1 symlink-root
// half: the artifact path IS a symlink; it is rejected outright (H1 admits no
// symlink), even though os.Root's Lstat of the final component does not follow
// it.
func TestLocalArtifactFetcherRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	withPackagesCache(t)
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, "secretdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(outer, "root")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outer, "secretdir"), filepath.Join(srcDir, "linkdir")); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "linkdir", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting a symlink at the artifact root, got %v", err)
	}
}

// TestReadRootFileRejectsInRootSymlinkSwap is the defect #2 regression: after
// the walker classifies a path as a regular file, the path is swapped for an
// in-root symlink pointing at a DIFFERENT in-root file. os.Root would follow
// that symlink, but the os.SameFile identity check between the pre-open Lstat
// and the fstat of the opened fd catches the swap and fails closed — the
// swapped-in decoy content is never returned.
func TestReadRootFileRejectsInRootSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "realfile"), []byte("REAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "decoy"), []byte("DECOY-CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	// Classify "realfile" as a regular file, then swap it for an in-root symlink
	// pointing at the decoy (the post-classify TOCTOU).
	expected, err := root.Lstat("realfile")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(srcDir, "realfile")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoy", filepath.Join(srcDir, "realfile")); err != nil {
		t.Fatal(err)
	}

	_, _, data, err := readRootFile(root, "realfile", expected, DefaultBundleLimits())
	if err == nil {
		t.Fatal("expected identity-mismatch rejection of the swapped-in symlink")
	}
	if strings.Contains(string(data), "DECOY") {
		t.Fatal("the swapped-in decoy content must never be returned")
	}
}

// TestReadConfinedDirRejectsInRootSymlinkSwap is the directory half of defect
// #2: a directory swapped for an in-root symlink to another in-root directory
// after classification is caught by the same os.SameFile identity check.
func TestReadConfinedDirRejectsInRootSymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on windows")
	}
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "decoydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "decoydir", "secret.txt"), []byte("DECOY"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	expected, err := root.Lstat("realdir")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(srcDir, "realdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("decoydir", filepath.Join(srcDir, "realdir")); err != nil {
		t.Fatal(err)
	}

	if err := streamConfinedDir(root, "realdir", expected, func(os.DirEntry) error { return nil }); err == nil {
		t.Fatal("expected identity-mismatch rejection of the swapped-in directory symlink")
	}
}

// TestLocalRootWalkerEnforcesPerFileCap covers the confined local read path
// (readRootFile): a file larger than the per-file cap is rejected during the
// bounded, fstat-revalidated read — the local-tree half of the defect #2/#3
// budget enforcement, exercised directly so a small cap can be injected.
func TestLocalRootWalkerEnforcesPerFileCap(t *testing.T) {
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "big.txt"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	rootInfo, err := root.Lstat("skill")
	if err != nil {
		t.Fatal(err)
	}

	limits := BundleLimits{MaxEntries: 10, MaxFiles: 10, MaxFileBytes: 1024, MaxBytes: 1 << 20}
	if _, err := NormalizeBundle(localRootWalker(root, "skill", rootInfo, limits), limits); err == nil {
		t.Fatal("expected rejection of a local tree file exceeding the per-file cap")
	}
}

// TestLocalRootWalkerRejectsEntryFlood is the t1b directory-flood adversarial
// test: os.ReadDir(-1) (and (*os.File).ReadDir(-1)) would materialize a
// directory's ENTIRE listing — a flood of millions of flat entries — into one
// slice before MaxEntries ever got a chance to reject it. streamConfinedDir
// instead reads in dirReadBatchSize batches, so the accumulator's MaxEntries
// cap trips within one batch's worth of over-read rather than after the
// whole directory is buffered. A small actual entry count against a tiny
// injected cap exercises the SAME mechanism a real multi-million-entry flood
// would hit, at test speed.
func TestLocalRootWalkerRejectsEntryFlood(t *testing.T) {
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		name := filepath.Join(skillDir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	rootInfo, err := root.Lstat("skill")
	if err != nil {
		t.Fatal(err)
	}

	limits := BundleLimits{MaxEntries: 10, MaxFiles: 100, MaxFileBytes: 1 << 20, MaxBytes: 1 << 20, MaxStreamBytes: 1 << 20, MaxPathBytes: 4096, MaxTotalPathBytes: 1 << 20}
	_, err = NormalizeBundle(localRootWalker(root, "skill", rootInfo, limits), limits)
	if err == nil {
		t.Fatal("expected rejection of a local directory entry-count flood")
	}
	if !strings.Contains(err.Error(), "entry-count cap") {
		t.Fatalf("expected an entry-count-cap rejection, got %v", err)
	}
}

// TestLocalRootWalkerStreamsAcrossBatchBoundary proves streamConfinedDir's
// batched ReadDir loop enumerates EVERY entry across more than one
// dirReadBatchSize batch, not just the first — a directory bigger than one
// batch must still fully succeed under a generous cap (regression guard for
// an off-by-one in the batch loop introduced by t1b's move off ReadDir(-1)).
func TestLocalRootWalkerStreamsAcrossBatchBoundary(t *testing.T) {
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const n = dirReadBatchSize + 25
	for i := 0; i < n; i++ {
		name := filepath.Join(skillDir, fmt.Sprintf("f%05d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	rootInfo, err := root.Lstat("skill")
	if err != nil {
		t.Fatal(err)
	}

	limits := DefaultBundleLimits()
	bundle, err := NormalizeBundle(localRootWalker(root, "skill", rootInfo, limits), limits)
	if err != nil {
		t.Fatalf("NormalizeBundle: %v", err)
	}
	fileCount := 0
	for _, e := range bundle.Entries {
		if !e.IsDir {
			fileCount++
		}
	}
	if fileCount != n {
		t.Fatalf("expected all %d files to survive a multi-batch read, got %d", n, fileCount)
	}
}

// TestQuotaFilesystemRejectsOverBudgetWrite is the t1b unit test for the
// clone-quota mechanism gitCloneShallowQuotaBounded relies on: a write whose
// declared bytes would drive the shared budget negative fails closed BEFORE
// it reaches disk, instead of the unbounded write gitCloneShallow's default
// in-memory storer+memfs would allow for an oversized upstream repository.
// This proves the quota wrapper's mechanism directly at a small injected
// budget — the same mechanism the real 256 MiB gitArtifactCloneQuotaBytes
// ceiling applies against an oversized clone.
func TestQuotaFilesystemRejectsOverBudgetWrite(t *testing.T) {
	dir := t.TempDir()
	qfs := newQuotaFilesystem(osfs.New(dir), 10)

	f, err := qfs.Create("x.bin")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(make([]byte, 5)); err != nil {
		t.Fatalf("first write within budget: %v", err)
	}
	if _, err := f.Write(make([]byte, 10)); !errors.Is(err, errCloneQuotaExceeded) {
		t.Fatalf("expected errCloneQuotaExceeded once a write exceeds the remaining budget, got %v", err)
	}
}

// TestLocalArtifactFetcherTreeLayoutPinnedDigestMismatch mirrors the git
// pinned-tree-digest-mismatch case for a local source.
func TestLocalArtifactFetcherTreeLayoutPinnedDigestMismatch(t *testing.T) {
	withPackagesCache(t)
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("skill body"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "pinned:sha256:deadbeef"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error for a pinned tree-layout digest mismatch, got %v", err)
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

func TestValidateGitSourceURL(t *testing.T) {
	parts := PackageRefParts{SourceID: "s", ArtifactPath: "a.json"}
	// A well-formed remote URL parses cleanly (the err==nil branch).
	if err := validateGitSourceURL("https://github.com/acme/repo.git", parts); err != nil {
		t.Fatalf("remote url: %v", err)
	}
	// A file:// path classifies as ErrNotRemote, which is allowed (local fixture).
	if err := validateGitSourceURL("file:///tmp/repo", parts); err != nil {
		t.Fatalf("file url: %v", err)
	}
	// A malformed URL is a hard parse failure -> schema ImportError.
	err := validateGitSourceURL("ht!tp://%zz", parts)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error for malformed url, got %v", err)
	}
}

// --- exported locked-layer read helpers ------------------------------------

// TestReadCachedLayerBytes covers the exported offline cache read used by lint:
// a hit returns the seeded bytes, and an absent SHA returns ok=false.
func TestReadCachedLayerBytes(t *testing.T) {
	withPackagesCache(t)
	body := []byte(`{"version":2,"skills":["s"]}`)
	if err := writeCachedUnit(layerTarget("acme", "org/base.json"), "deadbeef", body); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	got, ok := ReadCachedLayerBytes("acme", "org/base.json", "deadbeef")
	if !ok || string(got) != string(body) {
		t.Fatalf("hit = %q (ok=%v), want %q", got, ok, body)
	}
	if _, ok := ReadCachedLayerBytes("acme", "org/base.json", "nope"); ok {
		t.Fatalf("absent SHA should miss")
	}
}

// TestLockedRemoteLayerBytes drives the locked-layer cache resolution: a locked
// ref whose bytes are cached returns them; an unlocked ref, a lock with an empty
// SHA, and a locked-but-uncached ref all miss (ok=false) so lint skips them.
func TestLockedRemoteLayerBytes(t *testing.T) {
	withPackagesCache(t)
	parts, err := ParseLayerRef("acme:org/base.json")
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	body := []byte(`{"version":2}`)
	if err := writeCachedUnit(layerTarget("acme", "org/base.json"), "sha1", body); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	locked := map[string]LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "sha1"},
		"acme:org/empty":     {ResolvedSHA: ""},
		"acme:org/uncached":  {ResolvedSHA: "sha-missing"},
	}

	if got, ok := LockedRemoteLayerBytes(parts, "acme:org/base.json", locked); !ok || string(got) != string(body) {
		t.Fatalf("locked+cached = %q (ok=%v), want %q", got, ok, body)
	}
	if _, ok := LockedRemoteLayerBytes(parts, "acme:org/unlocked", locked); ok {
		t.Errorf("unlocked ref should miss")
	}
	if _, ok := LockedRemoteLayerBytes(parts, "acme:org/empty", locked); ok {
		t.Errorf("empty-SHA lock should miss")
	}
	uncachedParts, _ := ParseLayerRef("acme:org/uncached")
	if _, ok := LockedRemoteLayerBytes(uncachedParts, "acme:org/uncached", locked); ok {
		t.Errorf("locked-but-uncached ref should miss")
	}
}

// TestReadLockedLayers projects a written lock (legacy config section, migrated on
// read) into the LockedLayer set keyed by extends ref.
func TestReadLockedLayers(t *testing.T) {
	withPackagesCache(t)
	project := t.TempDir()
	if err := WriteConfigLock(project, map[string]LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "abc123", FetchedAt: "2026-06-02T00:00:00Z"},
	}); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	got, err := ReadLockedLayers(project)
	if err != nil {
		t.Fatalf("ReadLockedLayers: %v", err)
	}
	if l, ok := got["acme:org/base.json"]; !ok || l.ResolvedSHA != "abc123" {
		t.Fatalf("locked layer = %+v (ok=%v), want resolved_sha abc123", l, ok)
	}
}

// --- git ssh auth (layered agent -> default-key fallback) ------------------
//
// go-git v6 leaves ClientOptions unset -> ssh.NewSSHAgentAuth unconditionally,
// which hard-fails with "SSH agent requested but SSH_AUTH_SOCK not-specified"
// the instant SSH_AUTH_SOCK is unset, even when the user has a perfectly
// usable unencrypted default key and their own `git` works fine. gitSSHAuth
// (consumed by gitCloneShallow, shared by gitFetcher and gitArtifactFetcher)
// layers: SSH agent when present, else the OpenSSH default identity files
// (honoring ~/.ssh/config IdentityFile first), else a clear actionable error.

// setTestHomeDir points os.UserHomeDir() at dir for the duration of the
// test. Go's UserHomeDir reads $HOME on unix/darwin but %USERPROFILE% on
// Windows, so a plain t.Setenv("HOME", dir) is silently ignored on the
// windows-latest CI runner — set both so the fixture is honored everywhere.
func setTestHomeDir(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// writeTestSSHKey writes an OpenSSH-format ed25519 private key to
// <sshDir>/<name>, optionally passphrase-protected, and returns its path.
// Generated entirely via golang.org/x/crypto/ssh (no ssh-keygen subprocess),
// so the fixture is hermetic and needs no external binary.
func writeTestSSHKey(t *testing.T, sshDir, name, passphrase string) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = cryptossh.MarshalPrivateKey(priv, "")
	} else {
		block, err = cryptossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll sshDir: %v", err)
	}
	path := filepath.Join(sshDir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile key: %v", err)
	}
	return path
}

// TestGitSSHAuthNonSSHURLIsNoop proves https/file sources are completely
// unaffected by this fetcher's auth building: no auth, no error.
func TestGitSSHAuthNonSSHURLIsNoop(t *testing.T) {
	for _, url := range []string{"https://github.com/acme/repo.git", "file:///tmp/repo", "http://example.com/x"} {
		primary, fallback, err := gitSSHAuth(url)
		if err != nil || primary != nil || fallback != nil {
			t.Fatalf("gitSSHAuth(%q) = (%v, %v, %v), want (nil, nil, nil)", url, primary, fallback, err)
		}
	}
}

// TestCloneWithFallback covers the shared retry policy directly with stub clone
// funcs (no network): primary success skips the fallback; a primary error with a
// nil fallback surfaces that error unretried; a primary error with a non-nil
// fallback retries with the fallback auth, and the fallback result wins.
func TestCloneWithFallback(t *testing.T) {
	okRepo := &gogit.Repository{}
	dummy := &gogitssh.PublicKeys{} // any non-nil client.SSHAuth

	calls := 0
	repo, _, err := cloneWithFallback(nil, dummy, func(client.SSHAuth) (*gogit.Repository, billy.Filesystem, error) {
		calls++
		return okRepo, nil, nil
	})
	if err != nil || repo != okRepo || calls != 1 {
		t.Fatalf("primary-success: repo=%v err=%v calls=%d, want (okRepo, nil, 1)", repo, err, calls)
	}

	calls = 0
	_, _, err = cloneWithFallback(nil, nil, func(client.SSHAuth) (*gogit.Repository, billy.Filesystem, error) {
		calls++
		return nil, nil, errors.New("boom")
	})
	if err == nil || calls != 1 {
		t.Fatalf("primary-fail-no-fallback: err=%v calls=%d, want (err, 1)", err, calls)
	}

	calls = 0
	var gotAuth []client.SSHAuth
	repo, _, err = cloneWithFallback(nil, dummy, func(a client.SSHAuth) (*gogit.Repository, billy.Filesystem, error) {
		calls++
		gotAuth = append(gotAuth, a)
		if calls == 1 {
			return nil, nil, errors.New("primary boom")
		}
		return okRepo, nil, nil
	})
	if err != nil || repo != okRepo || calls != 2 {
		t.Fatalf("fallback-retry: repo=%v err=%v calls=%d, want (okRepo, nil, 2)", repo, err, calls)
	}
	if gotAuth[0] != nil || gotAuth[1] != client.SSHAuth(dummy) {
		t.Fatalf("fallback-retry: auth order = %v, want [nil, dummy]", gotAuth)
	}
}

// serveAgent serves keyring on a short-path unix socket (macOS caps sun_path
// ~104 bytes, so t.TempDir()'s long prefix is avoided) and points SSH_AUTH_SOCK
// at it for the duration of the test.
func serveAgent(t *testing.T, keyring agent.Agent) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "da-agent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "a.sock"))
	if err != nil {
		t.Fatalf("listen agent sock: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, conn) }()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(dir, "a.sock"))
}

// TestGitSSHAuthAgentWithIdentityUsesAgentOnly: an agent holding a key is the
// sole primary (no file fallback), preserving agent-first behavior.
func TestGitSSHAuthAgentWithIdentityUsesAgentOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket agent probe; Windows uses Pageant")
	}
	keyring := agent.NewKeyring()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("keyring.Add: %v", err)
	}
	serveAgent(t, keyring)
	// A key file exists too, but a keyed agent must win with no fallback.
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, fallback, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if _, ok := primary.(*gogitssh.PublicKeysCallback); !ok {
		t.Fatalf("expected agent primary (*PublicKeysCallback), got %T", primary)
	}
	if fallback != nil {
		t.Fatalf("keyed agent should have no fallback, got %T", fallback)
	}
}

// TestGitSSHAuthUnprobeableAgentKeepsFileFallback: SSH_AUTH_SOCK points at a
// socket that accepts but is not a real agent (List fails -> probe unknown), so
// gitSSHAuth returns the agent as primary AND an on-disk key as fallback for
// gitCloneShallow to retry with on any agent-path clone error.
func TestGitSSHAuthUnprobeableAgentKeepsFileFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket agent probe; Windows uses Pageant")
	}
	dir, err := os.MkdirTemp("/tmp", "da-agent")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "a.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// Accept then immediately close: dial succeeds (NewSSHAgentAuth builds) but
	// the agent List() gets EOF -> probeSSHAgent returns unknown.
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(dir, "a.sock"))

	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, fallback, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if _, ok := primary.(*gogitssh.PublicKeysCallback); !ok {
		t.Fatalf("expected agent primary (*PublicKeysCallback), got %T", primary)
	}
	if _, ok := fallback.(*gogitssh.PublicKeys); !ok {
		t.Fatalf("expected on-disk key fallback (*PublicKeys), got %T", fallback)
	}
}

// TestGitSSHAuthDeadAgentSocketUsesKeyFile: a dead SSH_AUTH_SOCK (dial fails, so
// NewSSHAgentAuth cannot even build) drops straight to the on-disk key primary.
func TestGitSSHAuthDeadAgentSocketUsesKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket agent probe; Windows uses Pageant")
	}
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "not-a-real-agent.sock"))
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, fallback, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if _, ok := primary.(*gogitssh.PublicKeys); !ok {
		t.Fatalf("dead socket: expected on-disk key primary (*PublicKeys), got %T", primary)
	}
	if fallback != nil {
		t.Fatalf("dead socket: expected no fallback, got %T", fallback)
	}
}

// TestGitSSHAuthFallsBackToDefaultKeyFile: no agent, but an unencrypted default
// key exists — the plain-shell scenario.
func TestGitSSHAuthFallsBackToDefaultKeyFile(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, _, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if _, ok := primary.(*gogitssh.PublicKeys); !ok {
		t.Fatalf("expected on-disk key auth, got %T", primary)
	}
}

// TestGitSSHAuthEmptyAgentUsesKeyFile is the omp-session fix: SSH_AUTH_SOCK
// points at a real but EMPTY agent (the macOS launchd default). The empty agent
// must NOT shadow the key file — gitSSHAuth builds file-based auth as primary.
func TestGitSSHAuthEmptyAgentUsesKeyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket agent probe; Windows uses Pageant")
	}
	serveAgent(t, agent.NewKeyring()) // zero identities
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, fallback, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth with empty agent: %v", err)
	}
	if _, ok := primary.(*gogitssh.PublicKeys); !ok {
		t.Fatalf("expected file-based primary (empty agent skipped), got %T", primary)
	}
	if fallback != nil {
		t.Fatalf("empty agent -> file primary should have no fallback, got %T", fallback)
	}
}

// TestProbeSSHAgent covers the tri-state probe: SSH_AUTH_SOCK unset -> unknown,
// a real but empty agent -> empty, a non-empty agent -> hasIdentity.
func TestProbeSSHAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket agent probe; Windows uses Pageant")
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	if got := probeSSHAgent(); got != sshAgentUnknown {
		t.Fatalf("unset SSH_AUTH_SOCK: got %d, want sshAgentUnknown", got)
	}
	keyring := agent.NewKeyring()
	serveAgent(t, keyring)
	if got := probeSSHAgent(); got != sshAgentEmpty {
		t.Fatalf("empty agent: got %d, want sshAgentEmpty", got)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("keyring.Add: %v", err)
	}
	if got := probeSSHAgent(); got != sshAgentHasIdentity {
		t.Fatalf("non-empty agent: got %d, want sshAgentHasIdentity", got)
	}
}

// TestGitSSHAuthUsesURLUserOrDefault proves the ssh user comes from the URL
// when present, else falls back to git.DefaultUsername ("git").
func TestGitSSHAuthUsesURLUserOrDefault(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "")

	primary, _, err := gitSSHAuth("ssh://custom-user@example.com/acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	pk, ok := primary.(*gogitssh.PublicKeys)
	if !ok {
		t.Fatalf("auth type = %T, want *ssh.PublicKeys", primary)
	}
	if pk.User != "custom-user" {
		t.Fatalf("User = %q, want custom-user", pk.User)
	}

	primary2, _, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if pk2 := primary2.(*gogitssh.PublicKeys); pk2.User != "git" {
		t.Fatalf("User = %q, want git (DefaultUsername)", pk2.User)
	}
}

// TestGitSSHAuthPassphraseProtectedKeyErrorsClearly proves a passphrase-
// protected key with no agent surfaces an actionable error instead of
// go-git's raw "SSH_AUTH_SOCK not-specified".
func TestGitSSHAuthPassphraseProtectedKeyErrorsClearly(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)
	writeTestSSHKey(t, filepath.Join(home, ".ssh"), "id_ed25519", "s3cret")

	_, _, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err == nil {
		t.Fatal("expected an error for a passphrase-protected key with no agent")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase-specific error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ssh-agent") {
		t.Fatalf("expected actionable ssh-agent guidance, got: %v", err)
	}
}

// TestGitSSHAuthNoKeyFoundErrorsClearly proves the "nothing at all to try"
// case is also a clear, actionable error rather than a silent auth failure.
func TestGitSSHAuthNoKeyFoundErrorsClearly(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)

	_, _, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err == nil {
		t.Fatal("expected an error when no agent and no default key exist")
	}
	if !strings.Contains(err.Error(), "no default SSH key found") {
		t.Fatalf("expected a 'no default SSH key' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ssh-agent") {
		t.Fatalf("expected actionable ssh-agent guidance, got: %v", err)
	}
}

// TestGitSSHAuthHonorsSSHConfigIdentityFile proves a ~/.ssh/config
// IdentityFile for the host is tried even when none of the OpenSSH default
// filenames (id_ed25519/id_rsa/id_ecdsa) exist.
func TestGitSSHAuthHonorsSSHConfigIdentityFile(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)
	sshDir := filepath.Join(home, ".ssh")
	writeTestSSHKey(t, sshDir, "custom_key", "")
	cfg := "Host github.com\n  IdentityFile ~/.ssh/custom_key\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	primary, _, err := gitSSHAuth("git@github.com:acme/repo.git")
	if err != nil {
		t.Fatalf("gitSSHAuth: %v", err)
	}
	if primary == nil {
		t.Fatal("expected auth built from the ssh_config IdentityFile")
	}
}

// TestSSHConfigIdentityFiles unit-tests the ssh_config(5) subset the fetcher
// relies on: literal/glob Host matching, multiple IdentityFile lines in file
// order, tilde expansion, and non-matching hosts yielding nothing.
func TestSSHConfigIdentityFiles(t *testing.T) {
	home := t.TempDir()
	setTestHomeDir(t, home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfg := strings.Join([]string{
		"# a comment, and a blank line below",
		"",
		"Host bitbucket.org",
		"  IdentityFile ~/.ssh/bb_key",
		"",
		"Host *.github.com github.com",
		"  IdentityFile ~/.ssh/gh_key1",
		"  IdentityFile /abs/gh_key2",
		"",
		"Host gitlab.com",
		"  IdentityFile ~/.ssh/gl_key",
	}, "\n")
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := sshConfigIdentityFiles(sshDir, "github.com")
	wantGh1 := filepath.Join(home, ".ssh", "gh_key1")
	if len(got) != 2 || got[0] != wantGh1 || got[1] != "/abs/gh_key2" {
		t.Fatalf("sshConfigIdentityFiles(github.com) = %v", got)
	}

	if got := sshConfigIdentityFiles(sshDir, "sub.github.com"); len(got) != 2 || got[0] != wantGh1 || got[1] != "/abs/gh_key2" {
		t.Fatalf("wildcard Host match: got %v", got)
	}

	if got := sshConfigIdentityFiles(sshDir, "unknown.example.com"); got != nil {
		t.Fatalf("expected no match for an unconfigured host, got %v", got)
	}
}

// TestSSHConfigIdentityFilesMissingFile proves a missing ~/.ssh/config is not
// an error — it just yields nothing to add to the default identity list.
func TestSSHConfigIdentityFilesMissingFile(t *testing.T) {
	if got := sshConfigIdentityFiles(filepath.Join(t.TempDir(), ".ssh"), "github.com"); got != nil {
		t.Fatalf("expected nil for a missing config file, got %v", got)
	}
}

// TestGitFetcherSSHNoAgentNoKeyErrorsClearly is the end-to-end proof: the
// same actionable error surfaces through the real gitFetcher.Fetch path
// (FetchRefresh -> clone -> gitCloneShallow -> gitSSHAuth), not just the
// unit-level gitSSHAuth call, and fails before any network clone attempt.
func TestGitFetcherSSHNoAgentNoKeyErrorsClearly(t *testing.T) {
	withPackagesCache(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	home := t.TempDir()
	setTestHomeDir(t, home)

	f := &gitFetcher{}
	_, err := f.Fetch(
		Source{Type: "git", URL: "git@github.com:acme/does-not-matter.git", Ref: "main"},
		LayerRefParts{LayerPath: "layer.json"},
		FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")},
	)
	if err == nil {
		t.Fatal("expected an ssh auth error")
	}
	if !strings.Contains(err.Error(), "no default SSH key found") {
		t.Fatalf("expected the actionable ssh auth error, got: %v", err)
	}
}

// --- H12 OCI auth seam wiring (t7) ------------------------------------------

// TestOCIAuthHeaderForRefWiredThroughPuller proves the fetcher_oci.go wiring
// point (ociAuthHeaderForRef) resolves the exact Authorization header value
// a real puller (t8) needs from a source's reference-only auth block, and
// that using it exactly as intended never lets the resolved secret leak into
// FetchedArtifact — the structure downstream lock/cache-metadata recording
// reads from.
func TestOCIAuthHeaderForRefWiredThroughPuller(t *testing.T) {
	withPackagesCache(t)
	sentinel := "sentinel-fetcher-wiring-3d4e5f"
	t.Setenv("OCI_TEST_WIRING_TOKEN", sentinel)

	blob := []byte("artifact-bytes")
	digest := "sha256:" + sha256Hex(blob)
	var observedHeader string
	f := &ociFetcher{puller: func(ctx context.Context, ref ociRef, auth []byte) (ociBlob, error) {
		// This is exactly the pattern t8's real puller uses: resolve the
		// Authorization header via the seam, use it only on the (here,
		// simulated) outgoing request, and never fold it into the returned
		// ociBlob.
		header, err := ociAuthHeaderForRef(ctx, auth, ref, "")
		if err != nil {
			t.Fatalf("ociAuthHeaderForRef: %v", err)
		}
		observedHeader = header
		return ociBlob{Data: blob, Digest: digest, MediaType: ociArtifactMediaType, ArtifactType: ociArtifactMediaType}, nil
	}}
	src := Source{
		Type: "oci",
		URL:  "oci://reg.example/base",
		Auth: json.RawMessage(`{"provider":"bearer","token_env":"OCI_TEST_WIRING_TOKEN"}`),
	}
	got, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	if err != nil {
		t.Fatalf("FetchArtifact: %v", err)
	}
	wantHeader := "Bearer " + sentinel
	if observedHeader != wantHeader {
		t.Fatalf("observed Authorization header = %q, want %q", observedHeader, wantHeader)
	}

	// H12: the resolved secret must never appear anywhere in the
	// FetchedArtifact the caller receives (this is what downstream lock/
	// cache-metadata recording reads from).
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), sentinel) {
		t.Fatalf("FetchedArtifact leaked the resolved secret: %s", encoded)
	}
	// And src.Auth itself — the persisted reference — never mutates to carry
	// the resolved value.
	if strings.Contains(string(src.Auth), sentinel) {
		t.Fatalf("Source.Auth mutated to carry the resolved secret: %s", src.Auth)
	}
}

// TestOCIAuthHeaderForRefForcedErrorNoLeak forces a downstream transport
// failure whose error message DELIBERATELY embeds the resolved Authorization
// header (simulating a future puller that is careless with the credential),
// and confirms the error-boundary redaction (newRedactedError, Codex round-2
// HIGH #3) scrubs it from BOTH the caller-visible error string AND the audit
// event. This is the non-vacuous replacement for the prior version, which
// returned a constant error the secret was never interpolated into — proving
// nothing. With the boundary redaction removed, this test FAILS (the sentinel
// survives in err.Error()), so it now guards the fix for the right reason.
func TestOCIAuthHeaderForRefForcedErrorNoLeak(t *testing.T) {
	withPackagesCache(t)
	sentinel := "sentinel-forced-error-6f7e8d"
	t.Setenv("OCI_TEST_FORCED_ERROR_TOKEN", sentinel)

	var resolvedHeader string
	f := &ociFetcher{puller: func(ctx context.Context, ref ociRef, auth []byte) (ociBlob, error) {
		header, err := ociAuthHeaderForRef(ctx, auth, ref, "")
		if err != nil {
			t.Fatalf("ociAuthHeaderForRef: %v", err)
		}
		resolvedHeader = header
		// Embed the resolved header (which carries the sentinel token) in the
		// transport error, exactly the careless-puller case the boundary
		// redaction must defend against.
		return ociBlob{}, fmt.Errorf("registry probe failed after presenting %s", header)
	}}
	src := Source{
		Type: "oci",
		URL:  "oci://reg.example/base",
		Auth: json.RawMessage(`{"provider":"bearer","token_env":"OCI_TEST_FORCED_ERROR_TOKEN"}`),
	}
	_, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill/x", VersionSpec: "1.0"})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	// Guard against a vacuous pass: the header the puller embedded must
	// actually contain the sentinel, otherwise the assertions below are
	// true-by-construction.
	if !strings.Contains(resolvedHeader, sentinel) {
		t.Fatalf("test setup invalid: resolved header %q did not carry the sentinel", resolvedHeader)
	}
	// Caller-visible error string (CLI stderr / logs) is scrubbed.
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("caller-visible error leaked the sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected the caller-visible error to show a redaction marker: %v", err)
	}
	// Audit event detail is scrubbed too.
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatal("expected an *ImportError")
	}
	ev := importFailedEvent(ie, false)
	detail, _ := ev.Fields["detail"].(string)
	if strings.Contains(detail, sentinel) {
		t.Fatalf("audit detail leaked the sentinel: %q", detail)
	}
}
