package config

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// useTLSTokenClient points ociTokenHTTPClient at a client that trusts srv's
// test TLS certificate for the duration of the test, preserving the
// production rejectTokenEndpointRedirect policy. The token exchange now
// refuses a non-https realm, so token-endpoint fixtures must be TLS servers;
// this swaps in a client that can talk to httptest's self-signed cert while
// still exercising the real redirect guard. The previous client is restored
// on cleanup.
func useTLSTokenClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := ociTokenHTTPClient
	c := srv.Client()
	c.Timeout = 15 * time.Second
	c.CheckRedirect = rejectTokenEndpointRedirect
	ociTokenHTTPClient = c
	t.Cleanup(func() { ociTokenHTTPClient = prev })
}

// --- ociAuthConfig parsing ---------------------------------------------------

func TestParseOCIAuthConfig(t *testing.T) {
	// Empty block -> zero value, no error.
	cfg, err := parseOCIAuthConfig(nil)
	if err != nil || cfg.Provider != "" {
		t.Fatalf("parseOCIAuthConfig(nil) = %+v, %v", cfg, err)
	}

	cfg, err = parseOCIAuthConfig(json.RawMessage(`{"provider":"bearer","token_env":"REGISTRY_TOKEN"}`))
	if err != nil {
		t.Fatalf("parseOCIAuthConfig: %v", err)
	}
	if cfg.Provider != "bearer" || cfg.TokenEnv != "REGISTRY_TOKEN" {
		t.Errorf("parsed cfg = %+v", cfg)
	}

	// Malformed JSON is a schema error whose text never echoes the raw bytes
	// (they could themselves be an accidental secret paste).
	_, err = parseOCIAuthConfig(json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for malformed auth block")
	}
	if strings.Contains(err.Error(), "not json") {
		t.Errorf("parseOCIAuthConfig error echoed raw input: %v", err)
	}
}

// --- bearer provider ---------------------------------------------------------

func TestResolveBearerCredential_TokenEnv(t *testing.T) {
	sentinel := "sentinel-bearer-env-7c3f9a"
	t.Setenv("OCI_TEST_TOKEN", sentinel)

	cred, err := resolveBearerCredential(ociAuthConfig{Provider: ociAuthProviderBearer, TokenEnv: "OCI_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("resolveBearerCredential: %v", err)
	}
	if cred.Token != sentinel {
		t.Errorf("Token = %q, want %q", cred.Token, sentinel)
	}
}

func TestResolveBearerCredential_TokenFile(t *testing.T) {
	sentinel := "sentinel-bearer-file-1a2b3c"
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(sentinel+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cred, err := resolveBearerCredential(ociAuthConfig{Provider: ociAuthProviderBearer, TokenFile: path})
	if err != nil {
		t.Fatalf("resolveBearerCredential: %v", err)
	}
	if cred.Token != sentinel {
		t.Errorf("Token = %q, want %q", cred.Token, sentinel)
	}
}

func TestResolveBearerCredential_MissingEnv_NoLeak(t *testing.T) {
	t.Setenv("OCI_TEST_TOKEN_UNSET_XYZ", "")
	os.Unsetenv("OCI_TEST_TOKEN_UNSET_XYZ")
	_, err := resolveBearerCredential(ociAuthConfig{Provider: ociAuthProviderBearer, TokenEnv: "OCI_TEST_TOKEN_UNSET_XYZ"})
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
	if !strings.Contains(err.Error(), "OCI_TEST_TOKEN_UNSET_XYZ") {
		t.Errorf("error should name the env var reference: %v", err)
	}
}

func TestResolveBearerCredential_NeitherSet(t *testing.T) {
	if _, err := resolveBearerCredential(ociAuthConfig{Provider: ociAuthProviderBearer}); err == nil {
		t.Fatal("expected error when neither token_env nor token_file is set")
	}
}

// --- credential-helper provider ----------------------------------------------

// writeHelperScript writes a POSIX shell script fixture and returns its path.
// Windows cannot execute a shebang script directly (exec.Command has no shell
// interpretation without PATHEXT support), so credential-helper subprocess
// tests are POSIX-only — mirrors the existing PATH-shim skip pattern used
// elsewhere in this repo (internal/graphstore/discoverbin_test.go).
func writeHelperScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("credential-helper subprocess fixture is POSIX-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveCredentialHelperCredential_TokenViaStdout(t *testing.T) {
	sentinel := "sentinel-helper-token-9d8e7f"
	// Reads (and discards) the stdin request, then prints a JSON token
	// response on stdout — the ONLY channel the secret should cross.
	helper := writeHelperScript(t, `cat >/dev/null
printf '{"token":"`+sentinel+`"}'
`)
	cred, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err != nil {
		t.Fatalf("resolveCredentialHelperCredential: %v", err)
	}
	if cred.Token != sentinel {
		t.Errorf("Token = %q, want %q", cred.Token, sentinel)
	}
}

func TestResolveCredentialHelperCredential_UsernameSecretViaStdout(t *testing.T) {
	sentinel := "sentinel-helper-secret-4e5f6a"
	helper := writeHelperScript(t, `cat >/dev/null
printf '{"username":"svc-account","secret":"`+sentinel+`"}'
`)
	cred, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err != nil {
		t.Fatalf("resolveCredentialHelperCredential: %v", err)
	}
	if cred.Username != "svc-account" || cred.Secret != sentinel {
		t.Errorf("cred = %+v", cred)
	}
}

// TestRunOCICredentialHelper_ArgvNeverCarriesRequest is the direct H12 "never
// argv" assertion: the helper dumps its own argv to a file (never inspecting
// stdin for this), and the test confirms the request document — including
// the registry name a real caller passes — never appears there. The request
// only ever crosses via stdin.
func TestRunOCICredentialHelper_ArgvNeverCarriesRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential-helper subprocess fixture is POSIX-only")
	}
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv.txt")
	helper := filepath.Join(dir, "helper.sh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argvFile + "\ncat >/dev/null\nprintf '{\"token\":\"unused\"}'\n"
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	sentinelRegistry := "sentinel-registry.example.com"
	_, err := ociCredentialHelperRunner(context.Background(), helper, []byte(`{"registry":"`+sentinelRegistry+`","repository":"acme/skill"}`), nil)
	if err != nil {
		t.Fatalf("ociCredentialHelperRunner: %v", err)
	}

	argv, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("reading argv capture: %v", err)
	}
	got := strings.TrimSpace(string(argv))
	if got != "get" {
		t.Errorf("helper argv = %q, want exactly \"get\" (request must cross via stdin only)", got)
	}
	if strings.Contains(got, sentinelRegistry) {
		t.Fatalf("registry leaked into argv: %q", got)
	}
}

func TestResolveCredentialHelperCredential_MalformedOutput_NoLeak(t *testing.T) {
	sentinel := "sentinel-malformed-8c7b6a"
	// Not valid JSON — a misbehaving helper that echoed a fragment of its
	// resolved secret into garbage output.
	helper := writeHelperScript(t, `cat >/dev/null
printf 'oops `+sentinel+` not json'
`)
	_, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err == nil {
		t.Fatal("expected error for malformed helper output")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("malformed-output error leaked the sentinel: %v", err)
	}
}

func TestResolveCredentialHelperCredential_ProcessFailure_NoLeak(t *testing.T) {
	sentinel := "sentinel-stderr-2b3c4d"
	// Exits non-zero after writing the sentinel to STDERR — proving the
	// error path never surfaces captured stderr text.
	helper := writeHelperScript(t, `cat >/dev/null
echo "`+sentinel+`" 1>&2
exit 1
`)
	_, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err == nil {
		t.Fatal("expected error for a failing helper process")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("process-failure error leaked the sentinel: %v", err)
	}
}

// TestRunOCICredentialHelper_StderrNeverCaptured proves the io.Discard fix:
// a helper that writes a secret to stderr and exits non-zero must NOT have
// that stderr captured into the returned *exec.ExitError.Stderr (which
// cmd.Output() populates up to ~64 KiB when Stderr is nil), and the returned
// error text must not contain the sentinel.
func TestRunOCICredentialHelper_StderrNeverCaptured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential-helper subprocess fixture is POSIX-only")
	}
	sentinel := "sentinel-exiterr-stderr-7a8b9c"
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ncat >/dev/null\necho \""+sentinel+"\" 1>&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ociCredentialHelperRunner(context.Background(), helper, []byte(`{"registry":"r"}`), nil)
	if err == nil {
		t.Fatal("expected a non-zero-exit error")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if len(exitErr.Stderr) != 0 {
			t.Fatalf("ExitError.Stderr captured helper stderr (%q); io.Discard should prevent this", exitErr.Stderr)
		}
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("runner error leaked the sentinel from stderr: %v", err)
	}
}

// TestRunOCICredentialHelper_EnvAllowlist proves the helper does NOT inherit
// an unrelated source's bearer-token env var: a sentinel is exported into the
// parent process env under a non-allowlisted name, and the helper (which dumps
// its own environment) never sees it. PATH, being allowlisted, IS visible so
// the helper's own external commands still resolve.
func TestRunOCICredentialHelper_EnvAllowlist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential-helper subprocess fixture is POSIX-only")
	}
	sentinel := "sentinel-other-source-token-2d3e4f"
	t.Setenv("SOME_OTHER_SOURCE_TOKEN", sentinel)

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	helper := filepath.Join(dir, "helper.sh")
	// Dump the environment to a file, discard stdin, print an empty JSON
	// credential so the runner returns cleanly.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nenv > "+envFile+"\ncat >/dev/null\nprintf '{\"token\":\"unused\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ociCredentialHelperRunner(context.Background(), helper, []byte(`{"registry":"r"}`), nil); err != nil {
		t.Fatalf("ociCredentialHelperRunner: %v", err)
	}
	dumped, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading env dump: %v", err)
	}
	if strings.Contains(string(dumped), sentinel) {
		t.Fatalf("helper inherited an unrelated source's token env var: %s", dumped)
	}
	if strings.Contains(string(dumped), "SOME_OTHER_SOURCE_TOKEN") {
		t.Fatalf("helper inherited the non-allowlisted env var name: %s", dumped)
	}
}

// TestRunOCICredentialHelper_StdoutBounded is the round-3 MEDIUM regression:
// a helper that writes far more than a credential JSON document to stdout
// must not have that output buffered without bound. The fixture writes 5
// MiB in one burst (well over helperOutputLimit's 4 MiB); the runner must
// never return that much data and must fail promptly rather than hang.
//
// The returned error is NOT asserted to be errHelperOutputTooLarge verbatim:
// once capLimitedWriter's Write returns an error, the exec package's copy
// goroutine stops draining the child's stdout pipe, so a fast writer like dd
// gets SIGPIPE and exits abnormally — and (*exec.Cmd).Wait deliberately
// prefers "the program exited abnormally" over the copy goroutine's error in
// that case (see awaitGoroutines). What matters for the bound is proven
// directly: the call fails, and no multi-megabyte buffer is ever returned.
func TestRunOCICredentialHelper_StdoutBounded(t *testing.T) {
	helper := writeHelperScript(t, `cat >/dev/null
dd if=/dev/zero bs=1048576 count=5 2>/dev/null
`)
	out, err := ociCredentialHelperRunner(context.Background(), helper, []byte(`{"registry":"r"}`), nil)
	if err == nil {
		t.Fatal("expected an error when helper stdout exceeds the size limit")
	}
	if len(out) > helperOutputLimit {
		t.Fatalf("runner returned %d bytes, exceeding helperOutputLimit (%d)", len(out), helperOutputLimit)
	}
}

// TestRunOCICredentialHelper_HelperEnvOptIn is the round-3 LOW regression:
// the fixed base allowlist breaks real ssh-agent/GPG/secretservice-backed
// helpers that need e.g. SSH_AUTH_SOCK. A Source can opt a helper into an
// additional env var NAME (value still resolved from the process env, never
// from config); a name NOT opted in stays fail-closed by default even
// though it is set in the parent process.
func TestRunOCICredentialHelper_HelperEnvOptIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential-helper subprocess fixture is POSIX-only")
	}
	optedIn := "sentinel-ssh-auth-sock-7c8d9e"
	notOptedIn := "sentinel-not-opted-in-1a2b3c"
	t.Setenv("SSH_AUTH_SOCK", optedIn)
	t.Setenv("GNUPGHOME", notOptedIn)

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	helper := filepath.Join(dir, "helper.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nenv > "+envFile+"\ncat >/dev/null\nprintf '{\"token\":\"unused\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ociCredentialHelperRunner(context.Background(), helper, []byte(`{"registry":"r"}`), []string{"SSH_AUTH_SOCK"}); err != nil {
		t.Fatalf("ociCredentialHelperRunner: %v", err)
	}
	dumped, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("reading env dump: %v", err)
	}
	if !strings.Contains(string(dumped), "SSH_AUTH_SOCK="+optedIn) {
		t.Fatalf("opted-in SSH_AUTH_SOCK was not forwarded: %s", dumped)
	}
	if strings.Contains(string(dumped), notOptedIn) || strings.Contains(string(dumped), "GNUPGHOME") {
		t.Fatalf("non-opted-in GNUPGHOME leaked despite fail-closed default: %s", dumped)
	}
}

func TestResolveCredentialHelperCredential_NoUsableCredential(t *testing.T) {
	helper := writeHelperScript(t, `cat >/dev/null
printf '{}'
`)
	_, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err == nil {
		t.Fatal("expected error for an empty credential response")
	}
}

func TestResolveCredentialHelperCredential_MissingHelper(t *testing.T) {
	if _, err := resolveCredentialHelperCredential(context.Background(), ociAuthConfig{Provider: ociAuthProviderCredentialHelper}, "r", "repo"); err == nil {
		t.Fatal("expected error when helper is unset")
	}
}

// --- WWW-Authenticate challenge parsing --------------------------------------

func TestParseWWWAuthenticateChallenge(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   ociAuthChallenge
		ok     bool
	}{
		{
			name:   "well-formed bearer challenge",
			header: `Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:acme/skill:pull"`,
			want:   ociAuthChallenge{Realm: "https://auth.example.com/token", Service: "registry.example.com", Scope: "repository:acme/skill:pull"},
			ok:     true,
		},
		{
			name:   "scope with embedded comma stays intact",
			header: `Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:acme/skill:pull,push"`,
			want:   ociAuthChallenge{Realm: "https://auth.example.com/token", Service: "registry.example.com", Scope: "repository:acme/skill:pull,push"},
			ok:     true,
		},
		{name: "basic scheme rejected", header: `Basic realm="registry"`, ok: false},
		{name: "missing realm rejected", header: `Bearer service="registry.example.com"`, ok: false},
		{name: "empty header rejected", header: "", ok: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseWWWAuthenticateChallenge(c.header)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// --- token endpoint exchange --------------------------------------------------

func TestExchangeBearerToken_BasicAuthSuccess(t *testing.T) {
	sentinelSecret := "sentinel-basic-secret-5f6e7d"
	sentinelToken := "sentinel-exchanged-token-1a2b3c"
	var gotUser, gotPass string
	var gotOK bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		if r.URL.Query().Get("service") != "registry.example.com" || r.URL.Query().Get("scope") != "repository:acme/skill:pull" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + sentinelToken + `"}`))
	}))
	defer srv.Close()
	useTLSTokenClient(t, srv)

	challenge := ociAuthChallenge{Realm: srv.URL, Service: "registry.example.com", Scope: "repository:acme/skill:pull"}
	cred := resolvedOCICredential{Username: "svc-account", Secret: sentinelSecret}
	token, err := exchangeBearerToken(context.Background(), challenge, cred)
	if err != nil {
		t.Fatalf("exchangeBearerToken: %v", err)
	}
	if token != sentinelToken {
		t.Errorf("token = %q, want %q", token, sentinelToken)
	}
	// The secret's ONLY intended appearance: the outgoing HTTP Basic auth
	// header presented to the token endpoint.
	if !gotOK || gotUser != "svc-account" || gotPass != sentinelSecret {
		t.Errorf("basic auth on request = (%q,%q,%v), want (svc-account,%q,true)", gotUser, gotPass, gotOK, sentinelSecret)
	}
}

func TestExchangeBearerToken_ErrorResponseBodyNeverLeaked(t *testing.T) {
	sentinel := "sentinel-error-body-3c4d5e"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A registry error page that (mis)echoes request material — the
		// exchange must never surface this body verbatim in its error.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials for " + sentinel))
	}))
	defer srv.Close()
	useTLSTokenClient(t, srv)

	challenge := ociAuthChallenge{Realm: srv.URL}
	cred := resolvedOCICredential{Username: "svc-account", Secret: "whatever"}
	_, err := exchangeBearerToken(context.Background(), challenge, cred)
	if err == nil {
		t.Fatal("expected error for a non-200 token endpoint response")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("token endpoint error leaked the response body: %v", err)
	}
}

// TestExchangeBearerToken_RejectsHTTPRealm is the HIGH-#1 direct assertion:
// an attacker-influenced WWW-Authenticate realm naming an http:// endpoint is
// refused BEFORE any credential is attached, so the credential is never sent
// in cleartext. No server is contacted at all.
func TestExchangeBearerToken_RejectsHTTPRealm(t *testing.T) {
	var hit atomic.Int32
	steal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
	}))
	defer steal.Close()

	challenge := ociAuthChallenge{Realm: steal.URL} // http:// (NewServer, not TLS)
	cred := resolvedOCICredential{Username: "svc-account", Secret: "sentinel-http-realm-1f2e3d"}
	_, err := exchangeBearerToken(context.Background(), challenge, cred)
	if err == nil {
		t.Fatal("expected exchangeBearerToken to reject an http:// realm")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error should explain the https requirement: %v", err)
	}
	if hit.Load() != 0 {
		t.Fatalf("credential endpoint was contacted despite the http realm (%d hits)", hit.Load())
	}
}

// TestExchangeBearerToken_RejectsDowngradeRedirect is the HIGH-#1 redirect
// half: an https token endpoint that 307-redirects to a same-host http URL
// must NOT have the credential forwarded to the cleartext target. The token
// server issues the downgrade; a separate plain-http "steal" server records
// whether it was ever contacted — it must not be.
func TestExchangeBearerToken_RejectsDowngradeRedirect(t *testing.T) {
	var stealHits atomic.Int32
	steal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stealHits.Add(1)
		_, _ = w.Write([]byte(`{"token":"leaked"}`))
	}))
	defer steal.Close()

	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, steal.URL+"/downgrade", http.StatusTemporaryRedirect)
	}))
	defer tokenSrv.Close()
	useTLSTokenClient(t, tokenSrv)

	cred := resolvedOCICredential{Username: "svc-account", Secret: "sentinel-downgrade-4c5d6e"}
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: tokenSrv.URL}, cred)
	if err == nil {
		t.Fatal("expected an error when the token endpoint redirects to http")
	}
	if stealHits.Load() != 0 {
		t.Fatalf("credential was forwarded across an https->http downgrade redirect (%d hits)", stealHits.Load())
	}
}

// TestExchangeBearerToken_RejectsSameHostDifferentPortRedirect is a round-3
// HIGH-#1 negative case: an https token endpoint that 307-redirects to a
// DIFFERENT PORT on the same loopback host must not have the credential
// forwarded there. Go's stdlib Authorization-forwarding check
// (shouldCopyHeaderOnRedirect) keys on url.Hostname(), which strips the
// port, so a same-host different-port redirect looks like "no host change"
// to the default policy even though it is a different listening service.
// steal and tokenSrv are both httptest TLS servers on 127.0.0.1 with
// distinct random ports, giving a real same-host different-port authority
// change.
func TestExchangeBearerToken_RejectsSameHostDifferentPortRedirect(t *testing.T) {
	var stealHits atomic.Int32
	steal := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stealHits.Add(1)
		_, _ = w.Write([]byte(`{"token":"leaked"}`))
	}))
	defer steal.Close()

	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, steal.URL+"/different-port", http.StatusTemporaryRedirect)
	}))
	defer tokenSrv.Close()
	useTLSTokenClient(t, tokenSrv)

	cred := resolvedOCICredential{Username: "svc-account", Secret: "sentinel-diffport-9f8e7d"}
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: tokenSrv.URL}, cred)
	if err == nil {
		t.Fatal("expected an error when the token endpoint redirects to a different port on the same host")
	}
	if stealHits.Load() != 0 {
		t.Fatalf("credential was forwarded across a same-host different-port redirect (%d hits)", stealHits.Load())
	}
}

// TestExchangeBearerToken_RejectsSubdomainRedirect is a round-3 HIGH-#1
// negative case: an https token endpoint that 307-redirects to a SUBDOMAIN
// of its own host must not have the credential forwarded there. Go's
// stdlib Authorization-forwarding check permits this hop directly
// (isDomainOrSubdomain treats foo.com -> sub.foo.com as safe to carry the
// header). A custom DialContext resolves the synthetic subdomain hostname
// straight to the steal listener's real loopback address, so this test
// proves the policy at the network layer: if rejectTokenEndpointRedirect
// ever let the redirect through, the dial WOULD reach steal (not fail on
// DNS), and stealHits would be nonzero.
func TestExchangeBearerToken_RejectsSubdomainRedirect(t *testing.T) {
	var stealHits atomic.Int32
	steal := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stealHits.Add(1)
		_, _ = w.Write([]byte(`{"token":"leaked"}`))
	}))
	defer steal.Close()
	stealAddr := steal.Listener.Addr().String()

	const subdomainHost = "sub.oci-auth-redirect-test.invalid"
	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+subdomainHost+"/subdomain", http.StatusTemporaryRedirect)
	}))
	defer tokenSrv.Close()

	prev := ociTokenHTTPClient
	ociTokenHTTPClient = &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: rejectTokenEndpointRedirect,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr == subdomainHost+":443" {
					addr = stealAddr
				}
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
			// steal's and tokenSrv's self-signed httptest certificates are
			// issued for 127.0.0.1, not the synthetic subdomain hostname;
			// this test proves the redirect-authority policy, not
			// certificate validation, so hostname verification is skipped.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only: proves redirect-authority rejection, not TLS validation
		},
	}
	t.Cleanup(func() { ociTokenHTTPClient = prev })

	cred := resolvedOCICredential{Username: "svc-account", Secret: "sentinel-subdomain-2b3c4d"}
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: tokenSrv.URL}, cred)
	if err == nil {
		t.Fatal("expected an error when the token endpoint redirects to a subdomain")
	}
	if stealHits.Load() != 0 {
		t.Fatalf("credential was forwarded across a subdomain redirect (%d hits)", stealHits.Load())
	}
}

func TestExchangeBearerToken_MalformedRealm(t *testing.T) {
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: "://not-a-url"}, resolvedOCICredential{Token: "x"})
	if err == nil {
		t.Fatal("expected error for a malformed realm URL")
	}
}

func TestExchangeBearerToken_NoTokenInResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	useTLSTokenClient(t, srv)
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: srv.URL}, resolvedOCICredential{Token: "x"})
	if err == nil {
		t.Fatal("expected error when the token endpoint response carries no token")
	}
}

// --- resolveOCIAuthorizationHeader (the seam) --------------------------------

func TestResolveOCIAuthorizationHeader_Bearer(t *testing.T) {
	sentinel := "sentinel-header-bearer-6a7b8c"
	t.Setenv("OCI_TEST_HEADER_TOKEN", sentinel)
	auth := json.RawMessage(`{"provider":"bearer","token_env":"OCI_TEST_HEADER_TOKEN"}`)

	header, err := resolveOCIAuthorizationHeader(context.Background(), auth, "registry.example.com", "acme/skill", "")
	if err != nil {
		t.Fatalf("resolveOCIAuthorizationHeader: %v", err)
	}
	want := "Bearer " + sentinel
	if header != want {
		t.Errorf("header = %q, want %q", header, want)
	}
}

func TestResolveOCIAuthorizationHeader_NoProviderConfigured(t *testing.T) {
	header, err := resolveOCIAuthorizationHeader(context.Background(), nil, "registry.example.com", "acme/skill", "")
	if err != nil || header != "" {
		t.Errorf("header = %q, err = %v, want empty/nil (unauthenticated)", header, err)
	}
}

func TestResolveOCIAuthorizationHeader_UnsupportedProvider(t *testing.T) {
	auth := json.RawMessage(`{"provider":"oauth2"}`)
	_, err := resolveOCIAuthorizationHeader(context.Background(), auth, "registry.example.com", "acme/skill", "")
	if err == nil {
		t.Fatal("expected error for an unsupported provider (oauth2/mtls are external-agent-sources' surface)")
	}
}

func TestResolveOCIAuthorizationHeader_CredentialHelperAwaitingChallenge(t *testing.T) {
	sentinel := "sentinel-await-challenge-9f8e7d"
	helper := writeHelperScript(t, `cat >/dev/null
printf '{"username":"svc","secret":"`+sentinel+`"}'
`)
	auth, err := json.Marshal(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper})
	if err != nil {
		t.Fatal(err)
	}
	// No challenge known yet: the seam must return "" (send an anonymous
	// probe first) rather than fabricate a request with no exchange target.
	header, err := resolveOCIAuthorizationHeader(context.Background(), auth, "registry.example.com", "acme/skill", "")
	if err != nil {
		t.Fatalf("resolveOCIAuthorizationHeader: %v", err)
	}
	if header != "" {
		t.Errorf("header = %q, want empty pending a discovered challenge", header)
	}
}

func TestResolveOCIAuthorizationHeader_CredentialHelperFullFlow(t *testing.T) {
	sentinelSecret := "sentinel-full-flow-secret-2e3f4a"
	sentinelToken := "sentinel-full-flow-token-5b6c7d"

	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pass, ok := r.BasicAuth(); !ok || pass != sentinelSecret {
			t.Errorf("token endpoint did not receive the resolved secret via basic auth")
		}
		_, _ = w.Write([]byte(`{"token":"` + sentinelToken + `"}`))
	}))
	defer tokenSrv.Close()
	useTLSTokenClient(t, tokenSrv)

	helper := writeHelperScript(t, `cat >/dev/null
printf '{"username":"svc-account","secret":"`+sentinelSecret+`"}'
`)
	auth, err := json.Marshal(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper})
	if err != nil {
		t.Fatal(err)
	}
	challengeHeader := `Bearer realm="` + tokenSrv.URL + `",service="registry.example.com",scope="repository:acme/skill:pull"`

	header, err := resolveOCIAuthorizationHeader(context.Background(), auth, "registry.example.com", "acme/skill", challengeHeader)
	if err != nil {
		t.Fatalf("resolveOCIAuthorizationHeader: %v", err)
	}
	want := "Bearer " + sentinelToken
	if header != want {
		t.Errorf("header = %q, want %q", header, want)
	}
	// The intermediate credential-helper secret must not itself appear in
	// the final header value — only the exchanged short-lived token does.
	if strings.Contains(header, sentinelSecret) {
		t.Fatalf("intermediate secret leaked into the final header: %q", header)
	}
}

// TestResolveOCIAuthorizationHeader_MalformedAuthBlock_NoLeak forces a schema
// error and confirms the error text never echoes a sentinel embedded in the
// (malformed) raw auth bytes.
func TestResolveOCIAuthorizationHeader_MalformedAuthBlock_NoLeak(t *testing.T) {
	sentinel := "sentinel-malformed-block-8a9b0c"
	auth := json.RawMessage(`{not json ` + sentinel)
	_, err := resolveOCIAuthorizationHeader(context.Background(), auth, "registry.example.com", "acme/skill", "")
	if err == nil {
		t.Fatal("expected error for malformed auth block")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("malformed-auth-block error leaked the sentinel: %v", err)
	}
}
