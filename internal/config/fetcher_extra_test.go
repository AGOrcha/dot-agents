package config

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// errReader always fails, exercising readAllLimited's io.ReadAll error branch.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestReadAllLimitedReaderError(t *testing.T) {
	if _, err := readAllLimited(errReader{}); err == nil {
		t.Fatal("expected error from failing reader")
	}
}

// errRoundTripper fails every request so httpFetcher's client.Do error path runs.
type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport down")
}

func TestHTTPFetcherTransportError(t *testing.T) {
	f := &httpFetcher{client: &http.Client{Transport: errRoundTripper{}}}
	_, err := f.Fetch(Source{Type: "http", URL: "https://example.test"}, LayerRefParts{LayerPath: "x.json"}, FetchTarget{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected transport error")
	}
}

// TestLocalFetcherURLFallback exercises the base = src.URL fallback when Path is empty.
func TestLocalFetcherURLFallback(t *testing.T) {
	srcDir := t.TempDir()
	body := []byte(`{"skills":["x"]}`)
	if err := os.WriteFile(filepath.Join(srcDir, "base.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localFetcher{}
	got, err := f.Fetch(Source{Type: "local", URL: srcDir}, LayerRefParts{LayerPath: "base.json"}, FetchTarget{Dir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got.Data) != string(body) {
		t.Fatalf("data = %q, want %q", got.Data, body)
	}
}

// TestLocalFetcherReadDirError points the layer path at a directory so os.ReadFile
// returns a non-NotExist error, exercising the generic read-error branch.
func TestLocalFetcherReadDirError(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &localFetcher{}
	_, err := f.Fetch(Source{Type: "local", Path: srcDir}, LayerRefParts{LayerPath: "adir"}, FetchTarget{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected read error for directory layer path")
	}
}
