package docsaccess

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/credstore"
)

// fakeResolver is a mock CredResolver. canned maps id -> secret; an id absent
// from the map resolves to ErrCredentialNotFound (mirroring the real loader's
// clean-miss terminal). err, when set, overrides every lookup with a hard error.
type fakeResolver struct {
	canned map[string]string
	err    error
}

func (f fakeResolver) Resolve(id string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if v, ok := f.canned[id]; ok {
		return v, nil
	}
	return "", credstore.ErrCredentialNotFound
}

const (
	testID     = "id-XXXX"
	testSecret = "secret-YYYY"
)

func bothCreds() map[string]string {
	return map[string]string{
		CredClientID:     testID,
		CredClientSecret: testSecret,
	}
}

// newRequest builds a GET request for the given absolute URL, failing the test
// on a malformed URL rather than returning an error to each table row.
func newRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("building request for %q: %v", rawURL, err)
	}
	return req
}

func TestDecorateAttachMatrix(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		canned      map[string]string
		wantHeaders bool
		wantErr     error
	}{
		{
			name:        "internal path on docs host attaches both headers",
			url:         "https://agorcha.dev/internal/runbook",
			canned:      bothCreds(),
			wantHeaders: true,
		},
		{
			name:        "internal root exactly is gated",
			url:         "https://agorcha.dev/internal",
			canned:      bothCreds(),
			wantHeaders: true,
		},
		{
			name:        "docs host with port still gated",
			url:         "https://agorcha.dev:8443/internal/x",
			canned:      bothCreds(),
			wantHeaders: true,
		},
		{
			name:        "host match is case-insensitive",
			url:         "https://AGORCHA.DEV/internal/x",
			canned:      bothCreds(),
			wantHeaders: true,
		},
		{
			name:        "public root gets no headers",
			url:         "https://agorcha.dev/",
			canned:      bothCreds(),
			wantHeaders: false,
		},
		{
			name:        "public guides path gets no headers",
			url:         "https://agorcha.dev/guides/getting-started",
			canned:      bothCreds(),
			wantHeaders: false,
		},
		{
			name:        "internal path on a different host gets no headers",
			url:         "https://example.com/internal/x",
			canned:      bothCreds(),
			wantHeaders: false,
		},
		{
			name:        "internalfoo boundary is NOT gated",
			url:         "https://agorcha.dev/internalfoo",
			canned:      bothCreds(),
			wantHeaders: false,
		},
		{
			name:        "internal path with both creds missing errors, no partial header",
			url:         "https://agorcha.dev/internal/x",
			canned:      map[string]string{},
			wantHeaders: false,
			wantErr:     ErrMissingCredential,
		},
		{
			name:        "internal path with only id present errors, no partial header",
			url:         "https://agorcha.dev/internal/x",
			canned:      map[string]string{CredClientID: testID},
			wantHeaders: false,
			wantErr:     ErrMissingCredential,
		},
		{
			name:        "internal path with empty secret value errors",
			url:         "https://agorcha.dev/internal/x",
			canned:      map[string]string{CredClientID: testID, CredClientSecret: ""},
			wantHeaders: false,
			wantErr:     ErrMissingCredential,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(WithResolver(fakeResolver{canned: tc.canned}))
			req := newRequest(t, tc.url)

			err := c.Decorate(req)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Decorate err = %v, want errors.Is %v", err, tc.wantErr)
				}
				assertNoHeaders(t, req)
				return
			}
			if err != nil {
				t.Fatalf("Decorate returned unexpected error: %v", err)
			}
			if tc.wantHeaders {
				assertHeaders(t, req, testID, testSecret)
			} else {
				assertNoHeaders(t, req)
			}
		})
	}
}

// TestDecoratePropagatesHardResolverError proves a non-miss resolver failure is
// surfaced (not swallowed as a clean miss) on a gated request.
func TestDecoratePropagatesHardResolverError(t *testing.T) {
	boom := errors.New("keyring locked")
	c := New(WithResolver(fakeResolver{err: boom}))
	req := newRequest(t, "https://agorcha.dev/internal/x")

	err := c.Decorate(req)
	if !errors.Is(err, boom) {
		t.Fatalf("Decorate err = %v, want errors.Is %v", err, boom)
	}
	if errors.Is(err, ErrMissingCredential) {
		t.Fatalf("hard error must not be reported as ErrMissingCredential: %v", err)
	}
	assertNoHeaders(t, req)
}

// TestWithDocsHostOverride proves a custom docs host gates a non-default host
// while the default host no longer matches.
func TestWithDocsHostOverride(t *testing.T) {
	c := New(WithResolver(fakeResolver{canned: bothCreds()}), WithDocsHost("docs.staging.internal"))

	gated := newRequest(t, "https://docs.staging.internal/internal/x")
	if err := c.Decorate(gated); err != nil {
		t.Fatalf("Decorate gated host: %v", err)
	}
	assertHeaders(t, gated, testID, testSecret)

	notGated := newRequest(t, "https://agorcha.dev/internal/x")
	if err := c.Decorate(notGated); err != nil {
		t.Fatalf("Decorate non-gated host: %v", err)
	}
	assertNoHeaders(t, notGated)
}

// TestDocsHostFromEnv proves DA_DOCS_HOST overrides the default at construction.
func TestDocsHostFromEnv(t *testing.T) {
	t.Setenv(envDocsHost, "docs.env.example")
	c := New(WithResolver(fakeResolver{canned: bothCreds()}))

	req := newRequest(t, "https://docs.env.example/internal/x")
	if err := c.Decorate(req); err != nil {
		t.Fatalf("Decorate: %v", err)
	}
	assertHeaders(t, req, testID, testSecret)
}

// TestDecorateWithRealLoader proves the real credstore.NewLoader path resolves
// the CF Access creds out of a plaintext credentials file (DA_CREDENTIALS_FILE)
// and the client attaches them — exercising the actual production token source,
// not just the mock. HOME/XDG are redirected so the real ~/.config is never hit.
func TestDecorateWithRealLoader(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	credsPath := filepath.Join(tmp, "creds.json")
	writeCredsFile(t, credsPath, map[string]string{
		CredClientID:     testID,
		CredClientSecret: testSecret,
	})
	t.Setenv("DA_CREDENTIALS_FILE", credsPath)

	// WithKeyring(nil) disables the encrypted-store step so resolution is driven
	// purely by the hermetic plaintext file (docs/TEST_SEAMS.md style seam).
	loader := credstore.NewLoader(credstore.WithKeyring(nil))
	c := New(WithResolver(loader))

	req := newRequest(t, "https://agorcha.dev/internal/x")
	if err := c.Decorate(req); err != nil {
		t.Fatalf("Decorate with real loader: %v", err)
	}
	assertHeaders(t, req, testID, testSecret)
}

// TestDecorateWithRealLoaderMissingCreds proves the real loader's clean-miss
// terminal (ErrCredentialNotFound) surfaces as ErrMissingCredential on a gated
// request when the credentials file lacks the CF Access ids.
func TestDecorateWithRealLoaderMissingCreds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))

	credsPath := filepath.Join(tmp, "creds.json")
	writeCredsFile(t, credsPath, map[string]string{"unrelated": "value"})
	t.Setenv("DA_CREDENTIALS_FILE", credsPath)

	loader := credstore.NewLoader(credstore.WithKeyring(nil))
	c := New(WithResolver(loader))

	req := newRequest(t, "https://agorcha.dev/internal/x")
	err := c.Decorate(req)
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("Decorate err = %v, want errors.Is ErrMissingCredential", err)
	}
	assertNoHeaders(t, req)
}

// TestTransportRoundTrip proves the RoundTripper wrapper attaches the headers to
// the request actually sent to the server (and leaves the caller's request
// unmutated, honoring the RoundTripper contract).
func TestTransportRoundTrip(t *testing.T) {
	var gotID, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get(headerClientID)
		gotSecret = r.Header.Get(headerClientSecret)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Gate the test server's own host so the real round trip is decorated.
	srvHost := newRequest(t, srv.URL).URL.Hostname()
	c := New(WithResolver(fakeResolver{canned: bothCreds()}), WithDocsHost(srvHost))

	client := c.HTTPClient(nil)
	req := newRequest(t, srv.URL+"/internal/ping")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	_ = resp.Body.Close()

	if gotID != testID || gotSecret != testSecret {
		t.Fatalf("server saw id=%q secret=%q, want id=%q secret=%q", gotID, gotSecret, testID, testSecret)
	}
	// The caller's original request must not have been mutated by the transport.
	if h := req.Header.Get(headerClientID); h != "" {
		t.Fatalf("transport mutated caller request: got %s=%q", headerClientID, h)
	}
}

// TestTransportRoundTripMissingCredAborts proves a gated round trip with no
// credential fails loudly instead of sending an unauthenticated request.
func TestTransportRoundTripMissingCredAborts(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	srvHost := newRequest(t, srv.URL).URL.Hostname()
	c := New(WithResolver(fakeResolver{canned: map[string]string{}}), WithDocsHost(srvHost))

	client := c.HTTPClient(nil)
	req := newRequest(t, srv.URL+"/internal/ping")
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected error from gated round trip with missing credential, got nil")
	}
	if reached {
		t.Fatal("unauthenticated request reached the server; client must fail loudly instead")
	}
}

// --- helpers ---

func assertHeaders(t *testing.T, req *http.Request, wantID, wantSecret string) {
	t.Helper()
	if got := req.Header.Get(headerClientID); got != wantID {
		t.Errorf("%s = %q, want %q", headerClientID, got, wantID)
	}
	if got := req.Header.Get(headerClientSecret); got != wantSecret {
		t.Errorf("%s = %q, want %q", headerClientSecret, got, wantSecret)
	}
}

func assertNoHeaders(t *testing.T, req *http.Request) {
	t.Helper()
	if got := req.Header.Get(headerClientID); got != "" {
		t.Errorf("expected no %s, got %q", headerClientID, got)
	}
	if got := req.Header.Get(headerClientSecret); got != "" {
		t.Errorf("expected no %s, got %q", headerClientSecret, got)
	}
}

// writeCredsFile writes a 0600 JSON id->secret map the real credstore loader
// reads via DA_CREDENTIALS_FILE. 0600 satisfies the loader's secure-perm check.
func writeCredsFile(t *testing.T, path string, creds map[string]string) {
	t.Helper()
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshaling creds: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing creds file: %v", err)
	}
}

// TestNewNilResolverFallsBackToLoader proves New replaces a nil resolver
// (an Option may explicitly clear it) with a real credstore loader rather than
// leaving a nil that would panic on the first Resolve.
//
// We assert the fallback's TYPE rather than driving a Resolve through it: a real
// *credstore.Loader on macOS is wired to the OS Keychain, and exercising it here
// would block on the Security Service (the CI mac runner hangs to the test
// timeout). The fallback wiring is what this test owns; the resolution chain is
// covered hermetically by the fakeResolver and DA_CREDENTIALS_FILE tests.
func TestNewNilResolverFallsBackToLoader(t *testing.T) {
	c := New(WithResolver(nil), WithDocsHost("docs.example.test"))
	if c.resolver == nil {
		t.Fatal("New(WithResolver(nil)) left a nil resolver; expected the default loader fallback")
	}
	if _, ok := c.resolver.(*credstore.Loader); !ok {
		t.Fatalf("expected the fallback resolver to be a *credstore.Loader, got %T", c.resolver)
	}
}

// TestNewEmptyDocsHostFallsBackToEnvDefault proves New replaces an empty
// docs-host (an Option may clear it) with the env/default host.
func TestNewEmptyDocsHostFallsBackToEnvDefault(t *testing.T) {
	t.Setenv("DA_DOCS_HOST", "fallback.docs.test")
	c := New(WithResolver(fakeResolver{canned: bothCreds()}), WithDocsHost(""))
	if c.docsHost != "fallback.docs.test" {
		t.Fatalf("empty WithDocsHost should fall back to DA_DOCS_HOST; got %q", c.docsHost)
	}
}

// TestDecorateNilRequestIsNoop proves Decorate tolerates a nil request / nil URL
// without panicking and attaches nothing.
func TestDecorateNilRequestIsNoop(t *testing.T) {
	c := New(WithResolver(fakeResolver{canned: bothCreds()}))
	if err := c.Decorate(nil); err != nil {
		t.Fatalf("Decorate(nil) should be a no-op, got error: %v", err)
	}
	// A request with a nil URL is likewise a no-op.
	if err := c.Decorate(&http.Request{Header: http.Header{}}); err != nil {
		t.Fatalf("Decorate(req with nil URL) should be a no-op, got error: %v", err)
	}
}
