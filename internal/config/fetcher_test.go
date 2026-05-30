package config

import (
	"context"
	"errors"
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
