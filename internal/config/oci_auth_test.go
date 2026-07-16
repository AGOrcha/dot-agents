package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
	cred, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
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
	cred, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
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
	_, err := ociCredentialHelperRunner(helper, []byte(`{"registry":"`+sentinelRegistry+`","repository":"acme/skill"}`))
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
	_, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
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
	_, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err == nil {
		t.Fatal("expected error for a failing helper process")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatalf("process-failure error leaked the sentinel: %v", err)
	}
}

func TestResolveCredentialHelperCredential_NoUsableCredential(t *testing.T) {
	helper := writeHelperScript(t, `cat >/dev/null
printf '{}'
`)
	_, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper, Helper: helper}, "registry.example.com", "acme/skill")
	if err == nil {
		t.Fatal("expected error for an empty credential response")
	}
}

func TestResolveCredentialHelperCredential_MissingHelper(t *testing.T) {
	if _, err := resolveCredentialHelperCredential(ociAuthConfig{Provider: ociAuthProviderCredentialHelper}, "r", "repo"); err == nil {
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		if r.URL.Query().Get("service") != "registry.example.com" || r.URL.Query().Get("scope") != "repository:acme/skill:pull" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + sentinelToken + `"}`))
	}))
	defer srv.Close()

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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A registry error page that (mis)echoes request material — the
		// exchange must never surface this body verbatim in its error.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials for " + sentinel))
	}))
	defer srv.Close()

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

func TestExchangeBearerToken_MalformedRealm(t *testing.T) {
	_, err := exchangeBearerToken(context.Background(), ociAuthChallenge{Realm: "://not-a-url"}, resolvedOCICredential{Token: "x"})
	if err == nil {
		t.Fatal("expected error for a malformed realm URL")
	}
}

func TestExchangeBearerToken_NoTokenInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
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

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pass, ok := r.BasicAuth(); !ok || pass != sentinelSecret {
			t.Errorf("token endpoint did not receive the resolved secret via basic auth")
		}
		_, _ = w.Write([]byte(`{"token":"` + sentinelToken + `"}`))
	}))
	defer tokenSrv.Close()

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
