// Package docsaccess is the client half of CF Access integration for the
// internal docs surface. It lets `da` reach pages under /internal/ on the docs
// host (e.g. the maintainer-only Starlight build) by attaching a developer's
// Cloudflare Access service-token headers — CF-Access-Client-Id and
// CF-Access-Client-Secret — sourced from the local credential store.
//
// Scope boundary: this package only ATTACHES an already-issued service token to
// outbound requests. The per-user minting endpoint (Cloudflare API
// POST .../access/service_tokens), which would create those tokens, is the
// remaining piece and is intentionally not implemented here — it needs a
// maintainer-provided scoped CF API token to call. Until that lands a developer
// stores their issued id/secret under the cf-access-client-id /
// cf-access-client-secret credential ids and this client uses them.
package docsaccess

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/credstore"
)

// Credential ids the resolver is queried with. They match the ids a developer
// registers their issued CF Access service token under in the credential store.
const (
	CredClientID     = "cf-access-client-id"
	CredClientSecret = "cf-access-client-secret"
)

// CF Access reads these two request headers to authenticate a service token.
const (
	headerClientID     = "CF-Access-Client-Id"
	headerClientSecret = "CF-Access-Client-Secret"
)

// defaultDocsHost is the host whose /internal/* surface is gated behind CF
// Access. It is overridable per-Client and via the DA_DOCS_HOST env var so the
// same client works against staging or a local preview host.
const defaultDocsHost = "agorcha.dev"

// envDocsHost overrides the docs host without code changes (e.g. staging).
const envDocsHost = "DA_DOCS_HOST"

// internalPrefix is the path surface gated behind CF Access. A request matches
// when its path is exactly "/internal" or lives under "/internal/" — "/internalfoo"
// is a different surface and must NOT pick up the access headers.
const internalPrefix = "/internal"

// ErrMissingCredential signals that a request targeting the gated /internal/*
// surface could not be authenticated because a required CF Access credential is
// absent. The caller asked for protected access; sending an unauthenticated
// request would silently fail at the edge, so the client fails loudly instead.
var ErrMissingCredential = errors.New("docsaccess: missing CF Access service-token credential")

// CredResolver is the narrow token source the client depends on (interface-DI,
// docs/TEST_SEAMS.md). *credstore.Loader satisfies it directly; tests inject a
// fake to drive the present / missing / partial-credential branches.
type CredResolver interface {
	Resolve(id string) (string, error)
}

// Client decorates outbound HTTP requests with CF Access service-token headers
// when, and only when, they target the gated internal docs surface.
type Client struct {
	// resolver is the credential source (the credstore loader in production).
	resolver CredResolver
	// docsHost is the host whose /internal/* surface is gated. Compared
	// case-insensitively against the request URL host (port stripped).
	docsHost string
}

// Option customizes a Client at construction.
type Option func(*Client)

// WithResolver sets the credential source. Production passes a
// *credstore.Loader; tests pass a fake CredResolver.
func WithResolver(r CredResolver) Option {
	return func(c *Client) { c.resolver = r }
}

// WithDocsHost overrides the gated docs host (defaults to DA_DOCS_HOST, else
// agorcha.dev). The value is matched host-only; any port on the request URL is
// ignored.
func WithDocsHost(host string) Option {
	return func(c *Client) { c.docsHost = host }
}

// New builds a Client. By default it resolves credentials through a freshly
// constructed credstore.NewLoader() and gates the host from DA_DOCS_HOST (or
// agorcha.dev). Options override either collaborator.
func New(opts ...Option) *Client {
	c := &Client{
		resolver: credstore.NewLoader(),
		docsHost: docsHostFromEnv(),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.resolver == nil {
		c.resolver = credstore.NewLoader()
	}
	if c.docsHost == "" {
		c.docsHost = docsHostFromEnv()
	}
	return c
}

// docsHostFromEnv reads DA_DOCS_HOST, falling back to the default docs host.
func docsHostFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(envDocsHost)); v != "" {
		return v
	}
	return defaultDocsHost
}

// Decorate attaches the CF Access service-token headers to req IFF it targets
// the gated internal docs surface (host == docsHost AND path under /internal/).
//
// Behavior:
//   - Non-matching request (public path, or a different host): nothing is set
//     and nil is returned — the request is left exactly as given.
//   - Matching request with both credentials available: both CF-Access headers
//     are set and nil is returned.
//   - Matching request with EITHER credential missing/empty: NO header is set
//     (no partial auth) and ErrMissingCredential is returned, so the caller
//     fails loudly rather than firing an unauthenticated request at the edge.
//
// The secret value is never logged; only the credential id and the gating
// decision are ever surfaced.
func (c *Client) Decorate(req *http.Request) error {
	if req == nil || req.URL == nil {
		return nil
	}
	if !c.gates(req.URL.Hostname(), req.URL.Path) {
		return nil
	}

	id, err := c.resolveRequired(CredClientID)
	if err != nil {
		return err
	}
	secret, err := c.resolveRequired(CredClientSecret)
	if err != nil {
		return err
	}

	// Both resolved: attach together so a request never carries a half-set pair.
	req.Header.Set(headerClientID, id)
	req.Header.Set(headerClientSecret, secret)
	return nil
}

// resolveRequired fetches a credential that the gated surface requires. A miss
// (ErrCredentialNotFound) or an empty value is reported as ErrMissingCredential
// naming the id; any other resolver error is propagated. The secret itself is
// never included in the error.
func (c *Client) resolveRequired(id string) (string, error) {
	val, err := c.resolver.Resolve(id)
	if err != nil {
		if errors.Is(err, credstore.ErrCredentialNotFound) {
			return "", fmt.Errorf("%w: %q", ErrMissingCredential, id)
		}
		return "", fmt.Errorf("docsaccess: resolving %q: %w", id, err)
	}
	if val == "" {
		return "", fmt.Errorf("%w: %q", ErrMissingCredential, id)
	}
	return val, nil
}

// gates reports whether a request to host+path targets the gated internal docs
// surface. The host match is case-insensitive; path matching treats exactly
// "/internal" and anything under "/internal/" as gated, but not "/internalfoo".
func (c *Client) gates(host, path string) bool {
	if !strings.EqualFold(host, c.docsHost) {
		return false
	}
	return isInternalPath(path)
}

// isInternalPath reports whether path is the /internal surface: exactly
// "/internal" or a descendant under "/internal/". "/internalfoo" is excluded.
func isInternalPath(path string) bool {
	return path == internalPrefix || strings.HasPrefix(path, internalPrefix+"/")
}

// Transport returns an http.RoundTripper that auto-attaches the CF Access
// headers (via Decorate) before delegating to base. base may be nil, in which
// case http.DefaultTransport is used. A decoration error (a gated request with
// no credential) aborts the round trip with that error rather than sending an
// unauthenticated request.
func (c *Client) Transport(base http.RoundTripper) http.RoundTripper {
	return &accessTransport{client: c, base: base}
}

// HTTPClient returns an *http.Client whose transport auto-attaches the CF Access
// headers for gated requests. base may be nil (http.DefaultTransport).
func (c *Client) HTTPClient(base http.RoundTripper) *http.Client {
	return &http.Client{Transport: c.Transport(base)}
}

// accessTransport is the RoundTripper wrapper that decorates each gated request
// before delegating to the underlying transport.
type accessTransport struct {
	client *Client
	base   http.RoundTripper
}

// RoundTrip decorates req (adding CF Access headers when it targets the gated
// surface) then delegates. To honor RoundTripper's contract of not mutating the
// caller's request, the headers are applied to a shallow clone.
func (t *accessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if err := t.client.Decorate(clone); err != nil {
		return nil, err
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
