package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This file is the OCI registry auth SEAM (package-artifact-install spec
// H12, t7). It resolves the two v1 auth providers named by the external-
// agent-sources spec §4 — `bearer` and `credential-helper` — into the exact
// value a real registry request sets on its Authorization header.
// oauth2/mtls stay owned by external-agent-sources and are not built here.
//
// H12 (credential non-disclosure) is the load-bearing contract: a Source's
// `auth` block persists a secret REFERENCE only (an env var name, a file
// path, or a helper binary name — see ociAuthConfig). The live secret value
// this file resolves from that reference exists only for the duration of
// one authorization-header build: it is never written back into a Source,
// a lock, cache metadata, a log line, or argv, and every error this file
// constructs is written WITHOUT embedding the resolved value. registerSecret
// (audit.go) is also called on every resolved value as a defense-in-depth
// backstop so any surface that formats an error or audit string — even one
// this file did not anticipate — scrubs it via redactSecrets before it is
// ever shown to a human or written to a sink.
//
// The live OCI Distribution wire protocol (manifest/blob pull) stays stubbed
// in fetcher_oci.go's ociPull; this seam is what that real implementation
// (t8) calls per request via ociAuthHeaderForRef to get its Authorization
// header value.

// ociAuthConfig is the parsed, secret-reference-only shape of a Source.Auth
// block for the bearer / credential-helper providers. Only a REFERENCE to a
// secret is ever recorded here — never a resolved value (H12).
type ociAuthConfig struct {
	// Provider selects the auth mechanism. "" means no auth. Any value other
	// than the two below (including "oauth2", "mtls") is a schema error for
	// this package — those providers are external-agent-sources' surface.
	Provider string `json:"provider"`
	// TokenEnv names an environment variable holding a static bearer token
	// (bearer provider). Takes precedence over TokenFile when both are set.
	TokenEnv string `json:"token_env,omitempty"`
	// TokenFile names a file whose trimmed contents are a static bearer
	// token (bearer provider).
	TokenFile string `json:"token_file,omitempty"`
	// Helper names the credential-helper binary to invoke (credential-helper
	// provider), git-credential-style. Looked up on PATH at invocation time;
	// it is never handed the secret request as an argv element — only via
	// stdin (H12).
	Helper string `json:"helper,omitempty"`
}

const (
	// ociAuthProviderBearer selects a static token from an env var or file.
	ociAuthProviderBearer = "bearer"
	// ociAuthProviderCredentialHelper selects an external helper binary
	// queried over stdin, git-credential-style.
	ociAuthProviderCredentialHelper = "credential-helper"
)

// parseOCIAuthConfig decodes a source's opaque auth block into its typed,
// reference-only shape. An empty block parses to the zero value (no auth); a
// malformed block is a schema error. The error text never echoes the raw
// bytes: a malformed auth block could itself be an accidental secret paste.
func parseOCIAuthConfig(auth json.RawMessage) (ociAuthConfig, error) {
	var cfg ociAuthConfig
	if len(auth) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(auth, &cfg); err != nil {
		return ociAuthConfig{}, fmt.Errorf("oci auth: malformed auth block (expected a JSON object)")
	}
	return cfg, nil
}

// resolvedOCICredential is the in-memory-only result of resolving an
// ociAuthConfig's secret reference. It never crosses into any persisted or
// logged surface (H12): callers use it to build exactly one Authorization
// header value and then let it fall out of scope.
type resolvedOCICredential struct {
	// Token is a bearer token presented directly as "Bearer <Token>" —
	// either the bearer provider's static token, or a credential-helper
	// response that returned a ready-made token rather than username+secret.
	Token string
	// Username/Secret are basic-auth-style credentials (credential-helper
	// provider) used to authenticate the registry token-endpoint exchange.
	Username string
	Secret   string
}

// resolveBearerCredential resolves the bearer provider's static token from
// TokenEnv (preferred) or TokenFile. Every error path names the reference
// (env var name / file path) only — never a value, since none has been read
// yet at the point of a missing/unset reference, and a read value is never
// echoed once it has been.
func resolveBearerCredential(cfg ociAuthConfig) (resolvedOCICredential, error) {
	if cfg.TokenEnv != "" {
		v, ok := os.LookupEnv(cfg.TokenEnv)
		v = strings.TrimSpace(v)
		if !ok || v == "" {
			return resolvedOCICredential{}, fmt.Errorf("oci bearer auth: environment variable %q is not set", cfg.TokenEnv)
		}
		registerSecret(v)
		return resolvedOCICredential{Token: v}, nil
	}
	if cfg.TokenFile != "" {
		data, err := os.ReadFile(cfg.TokenFile)
		if err != nil {
			return resolvedOCICredential{}, fmt.Errorf("oci bearer auth: reading token file %q: %w", cfg.TokenFile, err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return resolvedOCICredential{}, fmt.Errorf("oci bearer auth: token file %q is empty", cfg.TokenFile)
		}
		registerSecret(token)
		return resolvedOCICredential{Token: token}, nil
	}
	return resolvedOCICredential{}, fmt.Errorf("oci bearer auth: neither token_env nor token_file is set")
}

// credentialHelperRequest is the JSON document written to the helper's
// stdin — never argv (H12) — naming the registry/repository being
// authenticated so a helper can scope its lookup (git-credential-style).
type credentialHelperRequest struct {
	Registry   string `json:"registry"`
	Repository string `json:"repository,omitempty"`
}

// credentialHelperResponse is the JSON document a credential helper prints
// to stdout on success. Either Token, or Username+Secret, must be set.
type credentialHelperResponse struct {
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

// ociCredentialHelperRunner is the process-execution seam for
// resolveCredentialHelperCredential, letting tests substitute a fake helper
// invocation without asserting on a real PATH lookup.
var ociCredentialHelperRunner = runOCICredentialHelper

// resolveCredentialHelperCredential resolves the credential-helper
// provider's secret by invoking cfg.Helper with the request JSON on stdin
// and parsing its stdout as JSON. Both the argv (only "get", never the
// request or a secret) and stdin-only discipline are enforced by
// runOCICredentialHelper. ctx is threaded through so a hung helper is
// cancelled when the caller's OCI pull deadline fires. Every error path
// deliberately omits the helper's raw stdout/stderr from the error text: a
// misbehaving helper's output may itself carry partial secret material, so
// it is never echoed (H12).
func resolveCredentialHelperCredential(ctx context.Context, cfg ociAuthConfig, registry, repository string) (resolvedOCICredential, error) {
	if cfg.Helper == "" {
		return resolvedOCICredential{}, fmt.Errorf("oci credential-helper auth: helper is not set")
	}
	reqBody, err := json.Marshal(credentialHelperRequest{Registry: registry, Repository: repository})
	if err != nil {
		return resolvedOCICredential{}, fmt.Errorf("oci credential-helper auth: encoding request: %w", err)
	}
	out, err := ociCredentialHelperRunner(ctx, cfg.Helper, reqBody)
	if err != nil {
		return resolvedOCICredential{}, fmt.Errorf("oci credential-helper auth: helper %q failed", cfg.Helper)
	}
	var resp credentialHelperResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return resolvedOCICredential{}, fmt.Errorf("oci credential-helper auth: helper %q returned invalid JSON credential output", cfg.Helper)
	}
	switch {
	case resp.Token != "":
		registerSecret(resp.Token)
		return resolvedOCICredential{Token: resp.Token}, nil
	case resp.Username != "" && resp.Secret != "":
		registerSecret(resp.Secret)
		return resolvedOCICredential{Username: resp.Username, Secret: resp.Secret}, nil
	default:
		return resolvedOCICredential{}, fmt.Errorf("oci credential-helper auth: helper %q returned no usable credential", cfg.Helper)
	}
}

// credentialHelperEnvAllowlist is the fixed set of environment variable NAMES
// a credential helper subprocess is allowed to inherit. cmd.Env is built from
// only these (runOCICredentialHelper) rather than the full parent environment:
// the parent process may hold OTHER sources' bearer tokens in their own
// TokenEnv variables, and a misconfigured or compromised helper must not be
// handed credentials beyond the stdin request scoped to its own registry
// (Codex round-2 HIGH — helper env inheritance). PATH is required so the
// helper's own external commands resolve; the Windows-specific names keep the
// helper runnable there without widening the allowlist to arbitrary secrets.
var credentialHelperEnvAllowlist = []string{
	"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL",
	"SystemRoot", "USERPROFILE", "TEMP", "TMP", "PATHEXT", "ComSpec",
}

// allowlistedHelperEnv returns the current process's values for exactly the
// allowlisted variable names, in "KEY=VALUE" form for exec.Cmd.Env. A name
// that is unset in the parent is simply omitted.
func allowlistedHelperEnv() []string {
	env := make([]string, 0, len(credentialHelperEnvAllowlist))
	for _, k := range credentialHelperEnvAllowlist {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// runOCICredentialHelper invokes name as a subprocess, writing reqBody to
// its stdin and returning its stdout. name is invoked with a single fixed
// "get" argument (git-credential-style) — the request document and any
// secret the helper resolves NEVER cross the process boundary via argv,
// only via the stdin/stdout pipes (H12). Hardening (Codex round-2):
//   - ctx is honored via exec.CommandContext so a hung helper is cancelled
//     when the caller's OCI pull deadline fires.
//   - cmd.Stderr is set to io.Discard so the helper's stderr is genuinely
//     dropped: with a nil Stderr, cmd.Output() would instead capture up to
//     ~64 KiB of it into *exec.ExitError.Stderr, placing any credential the
//     helper printed to stderr into a returned error object.
//   - cmd.Env is the allowlist only, so the helper cannot read other
//     sources' token env vars out of the inherited environment.
//
// On failure only the raw (already secret-free by construction) exec error is
// returned to the caller, which itself never echoes it.
func runOCICredentialHelper(ctx context.Context, name string, reqBody []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, "get")
	cmd.Stdin = bytes.NewReader(reqBody)
	cmd.Stderr = io.Discard
	cmd.Env = allowlistedHelperEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ociAuthChallenge is the parsed form of a registry's WWW-Authenticate
// challenge header for the OCI Distribution bearer scheme (docker/
// distribution auth spec): `Bearer realm="...",service="...",scope="..."`.
type ociAuthChallenge struct {
	Realm   string
	Service string
	Scope   string
}

// parseWWWAuthenticateChallenge parses a WWW-Authenticate header value into
// its Bearer realm/service/scope components. Only the "Bearer" scheme is
// understood; a Basic/Digest challenge or malformed header reports ok=false.
func parseWWWAuthenticateChallenge(header string) (ociAuthChallenge, bool) {
	const scheme = "Bearer "
	if !strings.HasPrefix(header, scheme) {
		return ociAuthChallenge{}, false
	}
	var ch ociAuthChallenge
	for _, part := range splitAuthParams(header[len(scheme):]) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		switch key {
		case "realm":
			ch.Realm = val
		case "service":
			ch.Service = val
		case "scope":
			ch.Scope = val
		}
	}
	if ch.Realm == "" {
		return ociAuthChallenge{}, false
	}
	return ch, true
}

// splitAuthParams splits a comma-separated WWW-Authenticate parameter list,
// respecting double-quoted values that may themselves contain commas.
func splitAuthParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case r == ',' && !inQuotes:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// rejectNonHTTPSRedirect is the token client's redirect policy (Codex round-2
// HIGH — credential downgrade). Go's default redirect handling strips the
// Authorization header only across DIFFERENT hosts; a same-host (or
// subdomain) redirect from https to http, or to a different port, STILL
// forwards the credential — the stdlib copy check keys on host, not scheme.
// So the token exchange refuses to follow ANY redirect whose target is not
// https: a malicious/MITM token endpoint cannot bounce a Basic credential or
// bearer token to a cleartext http URL. The 10-redirect cap mirrors the
// stdlib default the custom policy replaces.
func rejectNonHTTPSRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("oci auth: refusing redirect to non-https token endpoint (scheme %q)", req.URL.Scheme)
	}
	if len(via) >= 10 {
		return fmt.Errorf("oci auth: stopped after 10 redirects")
	}
	return nil
}

// ociTokenHTTPClient is the HTTP client seam for the token-endpoint
// exchange, overridable in tests so no test touches the network. It carries
// rejectNonHTTPSRedirect so a credential is never forwarded across an
// https→http downgrade redirect.
var ociTokenHTTPClient = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: rejectNonHTTPSRedirect,
}

// tokenEndpointResponse is the subset of the OCI Distribution token
// endpoint response this package reads: "token" or the legacy "access_token"
// alias carries the short-lived bearer token.
type tokenEndpointResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// exchangeBearerToken performs the standard OCI registry token flow's
// second leg (WWW-Authenticate → token endpoint → Bearer header): GET
// challenge.Realm with service/scope query params, presenting cred as HTTP
// Basic auth when it carries a username+secret (credential-helper) or as a
// bearer Authorization header when it already carries a token, and returns
// the short-lived token from the JSON response. Every error path omits the
// response body from its text: a registry error page can echo request
// parameters or, on a misconfigured deployment, other sensitive material
// (H12 defense in depth). The resolved token is registered for redaction
// before this function returns it.
func exchangeBearerToken(ctx context.Context, challenge ociAuthChallenge, cred resolvedOCICredential) (string, error) {
	u, err := url.Parse(challenge.Realm)
	if err != nil {
		return "", fmt.Errorf("oci auth: malformed token endpoint realm %q: %w", challenge.Realm, err)
	}
	// The realm comes verbatim from the registry's WWW-Authenticate header —
	// attacker-influenceable. Refuse to present the credential to anything but
	// an https endpoint so a MITM/malicious registry cannot name an http (or
	// otherwise cleartext) realm and harvest it (Codex round-2 HIGH). This is
	// checked BEFORE any credential is attached to a request.
	if u.Scheme != "https" {
		return "", fmt.Errorf("oci auth: refusing to send credential to non-https token endpoint (realm scheme %q; must be https)", u.Scheme)
	}
	q := u.Query()
	if challenge.Service != "" {
		q.Set("service", challenge.Service)
	}
	if challenge.Scope != "" {
		q.Set("scope", challenge.Scope)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("oci auth: building token endpoint request: %w", err)
	}
	switch {
	case cred.Username != "" && cred.Secret != "":
		req.SetBasicAuth(cred.Username, cred.Secret)
	case cred.Token != "":
		req.Header.Set("Authorization", "Bearer "+cred.Token)
	}

	resp, err := ociTokenHTTPClient.Do(req)
	if err != nil {
		// A transport error (dial/TLS failure, timeout) never contains
		// request header content, so it is safe to wrap as-is.
		return "", fmt.Errorf("oci auth: token endpoint request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("oci auth: reading token endpoint response")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oci auth: token endpoint returned status %d", resp.StatusCode)
	}
	var tr tokenEndpointResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("oci auth: token endpoint returned invalid JSON")
	}
	token := tr.Token
	if token == "" {
		token = tr.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("oci auth: token endpoint response carries no token")
	}
	registerSecret(token)
	return token, nil
}

// resolveOCIAuthorizationHeader is the auth SEAM t8's live registry pull
// wires into: given a source's opaque, reference-only auth block, the
// registry host + repository, and (once one is known) the WWW-Authenticate
// challenge header from a prior anonymous probe, it resolves the live
// credential and returns the exact value the caller sets on the outgoing
// request's Authorization header.
//
// An empty return (nil error) means "send this request unauthenticated" —
// either no provider is configured, or (credential-helper with no challenge
// yet) the caller should send an anonymous probe first to discover the
// challenge, per the normal two-round-trip OCI Distribution auth pattern.
//
// The resolved secret material exists only for the duration of this call:
// it is never returned in any form other than the header string, never
// cached, logged, or persisted, and every error this function (or the
// resolvers it calls) constructs is written without embedding it (H12).
func resolveOCIAuthorizationHeader(ctx context.Context, auth json.RawMessage, registry, repository, challengeHeader string) (string, error) {
	cfg, err := parseOCIAuthConfig(auth)
	if err != nil {
		return "", err
	}
	var cred resolvedOCICredential
	switch cfg.Provider {
	case "":
		return "", nil
	case ociAuthProviderBearer:
		cred, err = resolveBearerCredential(cfg)
	case ociAuthProviderCredentialHelper:
		cred, err = resolveCredentialHelperCredential(ctx, cfg, registry, repository)
	default:
		return "", fmt.Errorf("oci auth: unsupported provider %q (bearer/credential-helper only; oauth2/mtls are external-agent-sources)", cfg.Provider)
	}
	if err != nil {
		return "", err
	}
	// A static bearer token (bearer provider, or a credential-helper that
	// returned a ready-made token) is presented directly — no token-endpoint
	// round trip needed.
	if cred.Token != "" {
		return "Bearer " + cred.Token, nil
	}
	if cred.Username == "" || cred.Secret == "" {
		return "", nil
	}
	challenge, ok := parseWWWAuthenticateChallenge(challengeHeader)
	if !ok {
		// No (or no Bearer) challenge known yet — the caller has not made
		// its anonymous probe request, or the registry does not use the
		// bearer scheme. Either way there is nothing to exchange yet.
		return "", nil
	}
	token, err := exchangeBearerToken(ctx, challenge, cred)
	if err != nil {
		return "", err
	}
	return "Bearer " + token, nil
}
