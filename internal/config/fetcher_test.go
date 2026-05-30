package config

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func TestGitFetcherResolvesAndCaches(t *testing.T) {
	const sha = "a3f9c2d1e8b4000000000000000000000000aaaa"
	body := []byte(`{"agents":["claude"]}`)
	calls := 0
	f := &gitFetcher{runner: func(args ...string) ([]byte, error) {
		calls++
		switch args[0] {
		case "ls-remote":
			return []byte(sha + "\trefs/heads/main\n"), nil
		case "archive":
			return body, nil
		}
		return nil, errors.New("unexpected git args: " + strings.Join(args, " "))
	}}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	got, err := f.Fetch(Source{Type: "git", URL: "https://example/repo.git", Ref: "main"}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.ResolvedSHA != sha {
		t.Fatalf("sha = %q, want %q", got.ResolvedSHA, sha)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q", got.Data)
	}

	// Second fetch: ls-remote returns same SHA, content served from cache (no archive).
	calls = 0
	got2, err := f.Fetch(Source{Type: "git", URL: "https://example/repo.git", Ref: "main"}, LayerRefParts{LayerPath: "org/base.json"}, cacheDir)
	if err != nil {
		t.Fatalf("Fetch (2nd): %v", err)
	}
	if !got2.CacheHit {
		t.Fatal("second fetch should hit the SHA cache")
	}
	if calls != 1 {
		t.Fatalf("expected only ls-remote on cache hit, got %d git calls", calls)
	}
}

func TestGitFetcherRealRunErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Real run() path (no runner seam) against a bogus local URL: git ls-remote
	// fails fast, exercising the real exec branch + its error handling.
	f := &gitFetcher{}
	_, err := f.Fetch(
		Source{Type: "git", URL: filepath.Join(t.TempDir(), "does-not-exist.git"), Ref: "main"},
		LayerRefParts{LayerPath: "x.json"},
		t.TempDir(),
	)
	if err == nil {
		t.Fatal("expected error from real git against a bogus URL")
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
	f := &gitFetcher{runner: func(args ...string) ([]byte, error) {
		if args[0] == "ls-remote" {
			return []byte("abc123\trefs/heads/main\n"), nil
		}
		return []byte("{}"), nil
	}}
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.Fetch(Source{Type: "git", URL: "https://example/r.git"}, LayerRefParts{LayerPath: "x.json"}, filepath.Join(blocker, "cache"))
	if err == nil {
		t.Fatal("expected cache-write error")
	}
}

func TestGitFetcherArchiveError(t *testing.T) {
	f := &gitFetcher{runner: func(args ...string) ([]byte, error) {
		if args[0] == "ls-remote" {
			return []byte("abc123\trefs/heads/main\n"), nil
		}
		return nil, errors.New("archive failed")
	}}
	_, err := f.Fetch(Source{Type: "git", URL: "https://example/r.git"}, LayerRefParts{LayerPath: "x.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected archive error")
	}
}

func TestGitFetcherRefNotFound(t *testing.T) {
	f := &gitFetcher{runner: func(args ...string) ([]byte, error) {
		return []byte(""), nil // empty ls-remote = ref not found
	}}
	_, err := f.Fetch(Source{Type: "git", URL: "https://example/repo.git", Ref: "nope"}, LayerRefParts{LayerPath: "x.json"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for unresolvable git ref")
	}
}
