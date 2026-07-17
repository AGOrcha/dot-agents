package config

// This file drives the OCI publish/pull producer (artifact_bundle.go), the
// confined package cache reader/writer (fetcher_oci.go), and the auth seam
// (oci_auth.go) to 100% line coverage. It reaches the defensive TOCTOU legs and
// the genuinely-never-fail legs (json.Marshal of a fixed struct, crypto/rand
// read, a stat/read/write/close on an already-validated healthy fd, a gzip/tar
// writer over a bytes.Buffer) that the behaviour suites cannot: where a real
// filesystem fault is deterministic (a 0000 cache dir, a truncated TLS body) it
// is used directly; otherwise the minimal production seams added alongside these
// tests (jsonMarshal, randRead, rootLstatFn, fileStatFn/fileWriteFn/fileCloseFn,
// readAllFn, newGzipWriter/newTarWriter, appendDigestQuery, httpNewRequest,
// ociResolveCredentialHelper) are fault-injected. Every helper/type is prefixed
// oci100 so it never collides with the fakeOCIRegistry / ociCov* harnesses reused
// for the happy paths. All seam-mutating tests are non-parallel and restore via
// t.Cleanup, so they never run concurrently with the package's parallel tests.

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var oci100Boom = errors.New("oci100: injected fault")

// oci100Swap sets *p to v for the duration of the test, restoring the original
// on cleanup — the non-parallel seam-override discipline this file relies on.
func oci100Swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	orig := *p
	*p = v
	t.Cleanup(func() { *p = orig })
}

// oci100FakeInfo is a fs.FileInfo whose mode and size are fully controlled, so a
// fault-injected fileStatFn/rootLstatFn can report a symlink, a non-directory,
// or an over-cap size a real just-created entry never would.
type oci100FakeInfo struct {
	mode os.FileMode
	size int64
}

func (f oci100FakeInfo) Name() string       { return "oci100-fake" }
func (f oci100FakeInfo) Size() int64        { return f.size }
func (f oci100FakeInfo) Mode() os.FileMode  { return f.mode }
func (f oci100FakeInfo) ModTime() time.Time { return time.Time{} }
func (f oci100FakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f oci100FakeInfo) Sys() any           { return nil }

// oci100FakeTar is a tarGzWriter whose WriteHeader/Write/Close each fail on
// demand, isolating tarGzBundle's three tar-writer error legs.
type oci100FakeTar struct {
	headerErr error
	writeErr  error
	closeErr  error
}

func (f *oci100FakeTar) WriteHeader(*tar.Header) error { return f.headerErr }
func (f *oci100FakeTar) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}
func (f *oci100FakeTar) Close() error { return f.closeErr }

// oci100FakeGzip is a tarGzGzip that accepts all writes but fails to Close,
// isolating tarGzBundle's gzip-Close error leg.
type oci100FakeGzip struct{ closeErr error }

func (f *oci100FakeGzip) Write(p []byte) (int, error) { return len(p), nil }
func (f *oci100FakeGzip) Close() error                { return f.closeErr }

// ===================== artifact_bundle.go: PackTree =====================

func TestOCI100PackTreeLstatError(t *testing.T) {
	oci100Swap(t, &rootLstatFn, func(*os.Root, string) (os.FileInfo, error) { return nil, oci100Boom })
	if _, _, err := PackTree(t.TempDir(), DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to surface a root Lstat error")
	}
}

func TestOCI100PackTreeSymlinkRoot(t *testing.T) {
	oci100Swap(t, &rootLstatFn, func(*os.Root, string) (os.FileInfo, error) {
		return oci100FakeInfo{mode: os.ModeSymlink}, nil
	})
	if _, _, err := PackTree(t.TempDir(), DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to reject a symlink resource-tree root")
	}
}

func TestOCI100PackTreeNotADirectory(t *testing.T) {
	oci100Swap(t, &rootLstatFn, func(*os.Root, string) (os.FileInfo, error) {
		return oci100FakeInfo{mode: 0}, nil // regular file: not symlink, not dir
	})
	if _, _, err := PackTree(t.TempDir(), DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to reject a non-directory resource-tree root")
	}
}

func TestOCI100PackTreeSerializeError(t *testing.T) {
	oci100Swap(t, &newGzipWriter, func(io.Writer) (tarGzGzip, error) { return nil, oci100Boom })
	if _, _, err := PackTree(buildFixtureTree(t), DefaultBundleLimits()); err == nil {
		t.Fatal("expected PackTree to surface a tarGzBundle serialization error")
	}
}

// ===================== artifact_bundle.go: tarGzBundle =====================

func TestOCI100TarGzGzipConstructorError(t *testing.T) {
	oci100Swap(t, &newGzipWriter, func(io.Writer) (tarGzGzip, error) { return nil, oci100Boom })
	if _, err := tarGzBundle(Bundle{}); err == nil {
		t.Fatal("expected a gzip-constructor error")
	}
}

func TestOCI100TarGzGzipCloseError(t *testing.T) {
	oci100Swap(t, &newGzipWriter, func(io.Writer) (tarGzGzip, error) {
		return &oci100FakeGzip{closeErr: oci100Boom}, nil
	})
	if _, err := tarGzBundle(Bundle{}); err == nil {
		t.Fatal("expected a gzip-Close error")
	}
}

func TestOCI100TarGzWriteHeaderError(t *testing.T) {
	oci100Swap(t, &newTarWriter, func(io.Writer) tarGzWriter { return &oci100FakeTar{headerErr: oci100Boom} })
	if _, err := tarGzBundle(testBundle(t, map[string]string{"f": "x"})); err == nil {
		t.Fatal("expected a tar WriteHeader error")
	}
}

func TestOCI100TarGzWriteContentError(t *testing.T) {
	oci100Swap(t, &newTarWriter, func(io.Writer) tarGzWriter { return &oci100FakeTar{writeErr: oci100Boom} })
	if _, err := tarGzBundle(testBundle(t, map[string]string{"f": "x"})); err == nil {
		t.Fatal("expected a tar Write error for a file entry")
	}
}

func TestOCI100TarGzCloseError(t *testing.T) {
	oci100Swap(t, &newTarWriter, func(io.Writer) tarGzWriter { return &oci100FakeTar{closeErr: oci100Boom} })
	if _, err := tarGzBundle(Bundle{}); err == nil {
		t.Fatal("expected a tar Close error")
	}
}

// ===================== artifact_bundle.go: push legs =====================

func TestOCI100PushBlobAppendDigestError(t *testing.T) {
	oci100Swap(t, &appendDigestQuery, func(string, string) (string, error) { return "", oci100Boom })
	srv, _ := newOCICovFaultRegistry(t)
	ref := ociRef{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "x/y"}
	if _, err := ociPushBlob(context.Background(), ref, nil, []byte("blob")); err == nil {
		t.Fatal("expected ociPushBlob to fail building the completion url")
	}
}

func TestOCI100PushLiveManifestMarshalError(t *testing.T) {
	oci100Swap(t, &jsonMarshal, func(any) ([]byte, error) { return nil, oci100Boom })
	srv, _ := newOCICovFaultRegistry(t)
	ref := ociRef{Registry: strings.TrimPrefix(srv.URL, "http://"), Repository: "x/y", Tag: "v1.0.0"}
	_, err := ociPushLive(context.Background(), ref, nil, []byte("layer"), ociArtifactMediaType, ociArtifactMediaType)
	if err == nil || !strings.Contains(err.Error(), "encoding manifest") {
		t.Fatalf("expected a manifest-encoding error, got %v", err)
	}
}

// ===================== fetcher_oci.go: readConfinedCacheBlob =====================

// oci100SeededRoot returns an os.Root over a temp dir seeded with a small
// regular "blob" file, the fixture the readConfinedCacheBlob legs read.
func oci100SeededRoot(t *testing.T) *os.Root {
	t.Helper()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "blob"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestOCI100ReadConfinedFhStatError(t *testing.T) {
	oci100Swap(t, &fileStatFn, func(*os.File) (os.FileInfo, error) { return nil, oci100Boom })
	if _, ok := readConfinedCacheBlob(oci100SeededRoot(t), "blob", 1024); ok {
		t.Fatal("a post-open fstat error must be a cache miss")
	}
}

func TestOCI100ReadConfinedFhStatMismatch(t *testing.T) {
	oci100Swap(t, &fileStatFn, func(*os.File) (os.FileInfo, error) { return oci100FakeInfo{mode: 0, size: 5}, nil })
	if _, ok := readConfinedCacheBlob(oci100SeededRoot(t), "blob", 1024); ok {
		t.Fatal("a fd whose fstat identity does not match the Lstat must be a miss")
	}
}

func TestOCI100ReadConfinedReadAllError(t *testing.T) {
	oci100Swap(t, &readAllFn, func(io.Reader) ([]byte, error) { return nil, oci100Boom })
	if _, ok := readConfinedCacheBlob(oci100SeededRoot(t), "blob", 1024); ok {
		t.Fatal("a read error must be a cache miss")
	}
}

func TestOCI100ReadConfinedReadOverCap(t *testing.T) {
	oci100Swap(t, &readAllFn, func(io.Reader) ([]byte, error) { return make([]byte, 2000), nil })
	if _, ok := readConfinedCacheBlob(oci100SeededRoot(t), "blob", 1024); ok {
		t.Fatal("a read that overruns the byte cap must be a miss")
	}
}

// ===================== fetcher_oci.go: sidecar + cache writer =====================

func TestOCI100WriteOCITypeSidecarMarshalError(t *testing.T) {
	oci100Swap(t, &jsonMarshal, func(any) ([]byte, error) { return nil, oci100Boom })
	digest := artifactDigest([]byte("x"))
	if err := writeOCITypeSidecar(digest, ociArtifactMediaType, ociArtifactMediaType); err == nil {
		t.Fatal("expected a sidecar marshal error")
	}
}

func TestOCI100WriteConfinedOpenRootError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("relies on POSIX directory-permission enforcement for a non-root user")
	}
	withPackagesCache(t)
	// Pre-create the cache root as an un-openable 0000 directory: fsops.MkdirAll
	// on an existing dir returns nil, then os.OpenRoot cannot open it.
	root := packagesCacheRoot()
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected OpenRoot of a 0000 cache root to fail")
	}
}

func TestOCI100WriteConfinedDirLstatError(t *testing.T) {
	withPackagesCache(t)
	oci100Swap(t, &rootLstatFn, func(*os.Root, string) (os.FileInfo, error) { return nil, oci100Boom })
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected a cache-dir Lstat error to be surfaced")
	}
}

func TestOCI100WriteConfinedDirSymlink(t *testing.T) {
	withPackagesCache(t)
	oci100Swap(t, &rootLstatFn, func(*os.Root, string) (os.FileInfo, error) {
		return oci100FakeInfo{mode: os.ModeSymlink | os.ModeDir}, nil
	})
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected a refusal to write through a symlinked cache dir")
	}
}

func TestOCI100WriteConfinedTempNameError(t *testing.T) {
	withPackagesCache(t)
	oci100Swap(t, &randRead, func([]byte) (int, error) { return 0, oci100Boom })
	// Covers writeConfinedPackagesCacheFile's randomTempName error propagation.
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected the temp-name RNG failure to be surfaced")
	}
	// Covers randomTempName's own rand.Read error leg directly.
	if _, err := randomTempName("blob"); err == nil {
		t.Fatal("expected randomTempName to fail when the RNG errors")
	}
}

func TestOCI100WriteConfinedWriteError(t *testing.T) {
	withPackagesCache(t)
	oci100Swap(t, &fileWriteFn, func(*os.File, []byte) (int, error) { return 0, oci100Boom })
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected the cache-temp write error to be surfaced")
	}
}

func TestOCI100WriteConfinedCloseError(t *testing.T) {
	withPackagesCache(t)
	// Return an error but still perform the real Close so the fd never leaks.
	oci100Swap(t, &fileCloseFn, func(f *os.File) error { _ = f.Close(); return oci100Boom })
	if err := writeCachedArtifact(artifactDigest([]byte("x")), []byte("x")); err == nil {
		t.Fatal("expected the cache-temp close error to be surfaced")
	}
}

// ===================== oci_auth.go =====================

func TestOCI100HelperRequestMarshalError(t *testing.T) {
	oci100Swap(t, &jsonMarshal, func(any) ([]byte, error) { return nil, oci100Boom })
	cfg := ociAuthConfig{Helper: "da-oci100-helper"}
	if _, err := resolveCredentialHelperCredential(context.Background(), cfg, "reg.example", "x/y"); err == nil {
		t.Fatal("expected the helper-request marshal error to be surfaced")
	}
}

func TestOCI100ExchangeBearerTokenRequestBuildError(t *testing.T) {
	oci100Swap(t, &httpNewRequest, func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, oci100Boom
	})
	challenge := ociAuthChallenge{Realm: "https://auth.example/token", Service: "reg"}
	if _, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "t"}); err == nil {
		t.Fatal("expected a token-endpoint request-build error")
	}
}

// oci100TruncatedTLSServer answers over TLS with a body shorter than its
// declared Content-Length, then closes — so the client's io.ReadAll of the
// token-endpoint response fails with an unexpected EOF.
func oci100TruncatedTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4096\r\n\r\nshort"))
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOCI100ExchangeBearerTokenBodyReadError(t *testing.T) {
	srv := oci100TruncatedTLSServer(t)
	oci100Swap(t, &ociTokenHTTPClient, srv.Client())
	challenge := ociAuthChallenge{Realm: srv.URL, Service: "reg", Scope: "repository:x:pull"}
	_, err := exchangeBearerToken(context.Background(), challenge, resolvedOCICredential{Token: "t"})
	if err == nil || !strings.Contains(err.Error(), "reading token endpoint response") {
		t.Fatalf("expected a token-endpoint body read error, got %v", err)
	}
}

func TestOCI100ResolveAuthorizationHeaderEmptyCredential(t *testing.T) {
	// A resolver that returns a partial (username-only) credential with no error
	// is a defensive case the real providers never produce; inject it to reach
	// the empty-credential guard that sends the request unauthenticated.
	oci100Swap(t, &ociResolveCredentialHelper, func(context.Context, ociAuthConfig, string, string) (resolvedOCICredential, error) {
		return resolvedOCICredential{Username: "u"}, nil
	})
	auth := json.RawMessage(`{"provider":"credential-helper","helper":"da-oci100-helper"}`)
	hdr, err := resolveOCIAuthorizationHeader(context.Background(), auth, "reg.example", "x/y", "")
	if err != nil {
		t.Fatalf("an empty credential must resolve to an unauthenticated send, got err %v", err)
	}
	if hdr != "" {
		t.Fatalf("expected an empty Authorization header, got %q", hdr)
	}
}
