package events

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// PR is the canonical, platform-independent pull-request payload (pr-event-source
// design D3). A producer maps any platform's JSON shape onto these fields via the
// pr_source field map, so consumers never branch on the source platform.
type PR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Branch    string `json:"branch"`
	BaseRef   string `json:"base_ref"`
	State     string `json:"state"`
	Mergeable string `json:"mergeable"`
	HeadSHA   string `json:"head_sha"`
	URL       string `json:"url"`
	Rollup    Rollup `json:"rollup"`
}

// Rollup is the aggregate CI status of a PR. State is one of the RollupState
// constants and is always derived by DeriveRollupState — never set ad hoc.
type Rollup struct {
	State  string  `json:"state"`
	Checks []Check `json:"checks"`
}

// Check is a single CI check on a PR.
type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Link       string `json:"link"`
}

// Comment is a single PR review or issue comment (pr-event-source D3).
type Comment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	CreatedAt string `json:"created_at"`
}

// Rollup state constants. These are the only legal values of Rollup.State.
const (
	RollupGreen   = "GREEN"
	RollupFailing = "FAILING"
	RollupPending = "PENDING"
)

// failingConclusions are the check conclusions that force the whole rollup to
// FAILING. Matching is case-insensitive so platform casing differences (gh emits
// upper-case, some APIs lower-case) resolve to the same state.
var failingConclusions = map[string]bool{
	"FAILURE":   true,
	"TIMED_OUT": true,
	"CANCELLED": true,
}

// rollupField is the canonical record/payload key carrying a PR's CI rollup. The
// gh list map projects .statusCheckRollup onto it (raw check array); the producer
// path then replaces it in place with a canonical Rollup whose State is derived.
const rollupField = "rollup"

// DeriveRollupState applies the one shared derive rule (pr-event-source D3):
//
//   - any check whose conclusion is FAILURE/TIMED_OUT/CANCELLED -> FAILING
//   - else any check not yet COMPLETED                          -> PENDING
//   - else                                                      -> GREEN
//
// An empty check set is GREEN (nothing failing, nothing pending). The rule is
// case-insensitive on both Status and Conclusion.
func DeriveRollupState(checks []Check) string {
	pending := false
	for _, c := range checks {
		if failingConclusions[strings.ToUpper(strings.TrimSpace(c.Conclusion))] {
			return RollupFailing
		}
		if strings.ToUpper(strings.TrimSpace(c.Status)) != "COMPLETED" {
			pending = true
		}
	}
	if pending {
		return RollupPending
	}
	return RollupGreen
}

// PR event kind names (pr-event-source D1). These are the first registered kinds
// on the event.pr namespace; each maps 1:1 onto a layered-pr-fanout poll-detector
// transition and the monitor-pr-review-comment-routing §4 events.
const (
	KindPROpened                = "event.pr.opened"
	KindPRCIGreen               = "event.pr.ci_green"
	KindPRCIFailed              = "event.pr.ci_failed"
	KindPRMerged                = "event.pr.merged"
	KindPRClosed                = "event.pr.closed"
	KindPRForceRebased          = "event.pr.force_rebased"
	KindPRReviewRequestedChange = "event.pr.review_requested_change"
	KindPRCommentPosted         = "event.pr.comment_posted"

	// PRNamespace is the namespace all PR kinds share.
	PRNamespace = "event.pr"
)

// PRKinds is the ordered set of canonical PR event kinds. It is the single source
// of truth for which kinds RegisterPRKinds installs.
func PRKinds() []string {
	return []string{
		KindPROpened,
		KindPRCIGreen,
		KindPRCIFailed,
		KindPRMerged,
		KindPRClosed,
		KindPRForceRebased,
		KindPRReviewRequestedChange,
		KindPRCommentPosted,
	}
}

// RegisterPRKinds installs every event.pr.* kind on the registry with
// control-plane (reject-on-unknown) disposition (pr-event-source D1/R1). It also
// sets the namespace disposition to reject so a mistyped event.pr.* type fails
// loudly at emit time rather than silently routing to a soft handler.
//
// Registration is idempotent: re-registering replaces with the same values.
func RegisterPRKinds(r *Registry) error {
	if r == nil {
		return fmt.Errorf("events: RegisterPRKinds: nil registry")
	}
	if err := r.SetNamespaceDisposition(PRNamespace, DispositionReject); err != nil {
		return err
	}
	for _, name := range PRKinds() {
		if err := r.Register(name, DispositionReject); err != nil {
			return err
		}
	}
	return nil
}

// FetchBlock is one named fetch in a pr_source config (the "list" or "comments"
// block). It maps directly onto the generic engine: Argv/URL drive the FetchSpec,
// Each selects the item list, and Map projects each item onto canonical fields.
type FetchBlock struct {
	Argv    []string          `json:"argv,omitempty"`
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Each    string            `json:"each,omitempty"`
	Map     map[string]string `json:"map,omitempty"`
}

// fetchSpec converts a FetchBlock to the engine FetchSpec.
func (b FetchBlock) fetchSpec() FetchSpec {
	return FetchSpec{
		Argv:    b.Argv,
		URL:     b.URL,
		Method:  b.Method,
		Headers: b.Headers,
	}
}

// PRSourceConfig is the engine-facing form of the .agentsrc.json pr_source field
// (pr-event-source D4). It is producer-agnostic: a gh, exec, http, or GitLab
// source differs only by the Argv/URL and Map in its blocks — zero Go.
type PRSourceConfig struct {
	// Producer selects the named producer: "gh" (the zero-config default),
	// "exec", "http", or any registered code producer.
	Producer string `json:"producer,omitempty"`
	// List fetches the open PR list and maps it onto canonical PR fields.
	List FetchBlock `json:"list,omitempty"`
	// Comments fetches one PR's comments (the "{number}" placeholder in Argv is
	// substituted per PR by the caller). Optional.
	Comments FetchBlock `json:"comments,omitempty"`
	// PollIntervalS is the producer poll cadence in seconds.
	PollIntervalS int `json:"poll_interval_s,omitempty"`
}

// DefaultGHPRSource returns the built-in `gh` pr_source config (pr-event-source
// D4 / proposal §3.1). It is the zero-config default: it shells out to the GitHub
// CLI and maps gh's JSON onto the canonical PR fields. A new platform overrides
// this block in .agentsrc.json — no Go change (pr-event-source done-criteria 1).
func DefaultGHPRSource() PRSourceConfig {
	return PRSourceConfig{
		Producer: "gh",
		List: FetchBlock{
			Argv: []string{
				"gh", "pr", "list", "--state", "open", "--json",
				"number,title,headRefName,baseRefName,state,mergeable,statusCheckRollup,url",
			},
			Each: ".",
			Map: map[string]string{
				"number":    ".number",
				"title":     ".title",
				"branch":    ".headRefName",
				"base_ref":  ".baseRefName",
				"state":     ".state",
				"mergeable": ".mergeable",
				"url":       ".url",
				// The raw gh check array is projected onto the canonical rollup
				// field; the producer derive step then replaces it in place with
				// a canonical Rollup whose State comes from DeriveRollupState.
				rollupField: ".statusCheckRollup",
			},
		},
		Comments: FetchBlock{
			Argv: []string{"gh", "pr", "view", "{number}", "--json", "comments"},
			Each: ".comments",
			Map: map[string]string{
				"author": ".author.login",
				"body":   ".body",
				"path":   ".path",
				"line":   ".line",
			},
		},
		PollIntervalS: 270,
	}
}

// identityEachKey is the synthetic wrapper key used when a list block's Each is
// the identity path ("." or empty) — i.e. the fetched document is itself the
// array (the gh `pr list --json` shape). The engine's JSONPath subset has no
// root-identity operator, so identityFetcher wraps the document under this key
// and ProducerConfigForList points Each at it. This keeps the foundational
// engine untouched while supporting a top-level-array source.
const identityEachKey = "__pr_items__"

// isIdentityEach reports whether an Each expression selects the document itself.
func isIdentityEach(each string) bool {
	e := strings.TrimSpace(each)
	return e == "" || e == "." || e == "$" || e == "$."
}

// ProducerConfigForList builds the engine ProducerConfig that emits envelopes of
// the given typ from this source's List block. The source/Each/Map all flow from
// config, so this is the entire "PR support" producer wiring — no per-platform
// code (pr-event-source D2).
func (c PRSourceConfig) ProducerConfigForList(typ, source string) (ProducerConfig, error) {
	if strings.TrimSpace(typ) == "" {
		return ProducerConfig{}, fmt.Errorf("events: pr_source: empty event type")
	}
	if strings.TrimSpace(source) == "" {
		source = c.Producer
	}
	if len(c.List.Map) == 0 {
		return ProducerConfig{}, fmt.Errorf("events: pr_source: list block has empty map")
	}
	each := c.List.Each
	if isIdentityEach(each) {
		each = "." + identityEachKey
	}
	return ProducerConfig{
		Type:   typ,
		Source: source,
		Fetch:  c.List.fetchSpec(),
		Each:   each,
		Map:    cloneStringMap(c.List.Map),
		KeyBy:  "number",
	}, nil
}

// NewListProducer constructs the PR list producer for this source. A nil fetcher
// uses the default exec/http fetcher; tests inject a fake. When the list block's
// Each is the document-identity path, the fetcher is wrapped so the engine sees
// the array under a synthetic key. The returned *PRProducer wraps the generic
// engine and derives each PR's canonical Rollup.State on every cycle, so a
// default-source fetch emits event.pr.* payloads carrying a derived rollup state
// (pr-event-source D3) — DeriveRollupState is on this live path, not dead code.
func (c PRSourceConfig) NewListProducer(typ, source string, fetcher Fetcher) (*PRProducer, error) {
	cfg, err := c.ProducerConfigForList(typ, source)
	if err != nil {
		return nil, err
	}
	if isIdentityEach(c.List.Each) {
		if fetcher == nil {
			fetcher = DefaultFetcher{}
		}
		fetcher = identityFetcher{inner: fetcher}
	}
	inner, err := NewProducer(cfg, fetcher)
	if err != nil {
		return nil, err
	}
	return &PRProducer{inner: inner}, nil
}

// PRProducer wraps the generic engine Producer with the PR-specific rollup
// derive step (pr-event-source D3). Keeping the derive here — not in the engine —
// honours D2 (no per-platform producer files in the generic engine): the engine
// stays config-only, while this reaction-side wrapper applies the one shared
// Rollup.State rule. A platform whose source has no checks simply maps no rollup
// field and the derive is a no-op.
type PRProducer struct {
	inner *Producer
}

// Cycle runs one engine cycle, then rewrites each emitted envelope's payload so
// its rollup field is the canonical Rollup{State, Checks} with State derived by
// DeriveRollupState. Envelopes without a rollup field pass through unchanged so
// non-CI sources (e.g. a comments source) are unaffected.
func (p *PRProducer) Cycle(ctx context.Context) ([]Envelope, error) {
	envs, err := p.inner.Cycle(ctx)
	if err != nil {
		return nil, err
	}
	for i := range envs {
		derived, derr := deriveEnvelopeRollup(envs[i])
		if derr != nil {
			return nil, derr
		}
		envs[i] = derived
	}
	return envs, nil
}

// deriveEnvelopeRollup decodes one envelope payload, replaces its raw rollup
// field with a canonical derived Rollup, and re-marshals — preserving every
// other mapped field (no round-trip loss). When the payload has no rollup field
// the envelope is returned unchanged.
func deriveEnvelopeRollup(env Envelope) (Envelope, error) {
	var rec map[string]any
	if err := json.Unmarshal(env.Payload, &rec); err != nil {
		return Envelope{}, fmt.Errorf("events: pr producer %q: decode payload: %w", env.Type, err)
	}
	raw, ok := rec[rollupField]
	if !ok {
		return env, nil
	}
	rec[rollupField] = deriveRollup(raw)
	payload, err := json.Marshal(rec)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: pr producer %q: marshal payload: %w", env.Type, err)
	}
	return NewEnvelope(env.Type, env.Source, env.IdempotencyKey, env.OccurredAt, payload)
}

// deriveRollup converts a raw mapped rollup value (a platform's check array, or
// null when the source reported no rollup) into the canonical Rollup with its
// State derived by the one shared rule. A nil/absent value yields an explicit
// empty-check rollup (GREEN) — the same as a source reporting zero checks —
// rather than dropping the field.
func deriveRollup(raw any) Rollup {
	checks := checksFromRaw(raw)
	return Rollup{State: DeriveRollupState(checks), Checks: checks}
}

// checksFromRaw projects a platform's raw check array onto canonical Checks. It
// is tolerant: a non-array (null, missing nested fields) yields no checks rather
// than erroring, so a PR with no CI configured derives to GREEN deterministically.
func checksFromRaw(raw any) []Check {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	checks := make([]Check, 0, len(arr))
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		checks = append(checks, Check{
			Name:       stringField(m, "name"),
			Status:     stringField(m, "status"),
			Conclusion: stringField(m, "conclusion"),
			Link:       firstStringField(m, "link", "detailsUrl"),
		})
	}
	return checks
}

// stringField returns m[key] as a string, or "" when absent or non-string.
func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// firstStringField returns the first present non-empty string among keys — used
// for fields a platform may name differently (gh uses detailsUrl, others link).
func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := stringField(m, k); v != "" {
			return v
		}
	}
	return ""
}

// identityFetcher wraps a fetched top-level JSON array as {identityEachKey: [...]}
// so the engine's JSONPath subset (which has no root-identity operator) can
// select it. A non-array document is passed through unchanged so an upstream
// shape error surfaces from the engine with its own message.
type identityFetcher struct {
	inner Fetcher
}

func (f identityFetcher) Fetch(ctx context.Context, spec FetchSpec) ([]byte, error) {
	raw, err := f.inner.Fetch(ctx, spec)
	if err != nil {
		return nil, err
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Not a top-level array — leave it for the engine to reject with a
		// precise "each is not a list" error.
		return raw, nil
	}
	wrapped := map[string][]json.RawMessage{identityEachKey: arr}
	return json.Marshal(wrapped)
}

func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for _, k := range sortedKeys(m) {
		out[k] = m[k]
	}
	return out
}

// CredentialLoader resolves a per-host credential for the direct-load fallback
// path (pr-event-source D5). It is the seam the external-agent-sources cred-store
// loader plugs into; the producer never sees the resolution strategy. Returning
// an empty token with a nil error means "no credential for this host" — the
// request proceeds unauthenticated rather than failing.
type CredentialLoader func(host string) (token string, err error)

// AuthRoundTripper is the auth seam the producer receives (pr-event-source D5).
// When ProxyBase is set it rewrites outbound requests to the da service injector
// at localhost:<port>/proxy/<host>, which attaches the credential and owns
// refresh — the producer stays credential-unaware. When ProxyBase is empty it
// falls back to the direct-load path: it calls Loader for the request host and
// sets a bearer Authorization header inline.
type AuthRoundTripper struct {
	// ProxyBase is the da service injector base, e.g. "http://localhost:8765".
	// Empty selects the direct-load fallback.
	ProxyBase string
	// Loader resolves a credential for a host in the fallback path. Nil in the
	// fallback path means "no credential available" and requests pass through
	// unauthenticated.
	Loader CredentialLoader
	// Base is the underlying RoundTripper. Nil uses http.DefaultTransport.
	Base http.RoundTripper
}

// usingProxy reports whether the proxy path is selected.
func (a *AuthRoundTripper) usingProxy() bool {
	return strings.TrimSpace(a.ProxyBase) != ""
}

// RoundTrip implements http.RoundTripper. It never mutates the caller's request;
// it clones before rewriting URL or headers.
func (a *AuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := a.Base
	if base == nil {
		base = http.DefaultTransport
	}
	out, err := a.decorate(req)
	if err != nil {
		return nil, err
	}
	return base.RoundTrip(out)
}

// decorate returns a cloned request routed for the active path.
func (a *AuthRoundTripper) decorate(req *http.Request) (*http.Request, error) {
	if a.usingProxy() {
		return a.viaProxy(req)
	}
	return a.viaDirectLoad(req)
}

// viaProxy rewrites the request to target localhost:<port>/proxy/<host>, leaving
// the credential to the injector. The original host is preserved in the path so
// the injector can attach the right credential. Body metadata that
// http.NewRequestWithContext cannot infer from a generic io.ReadCloser
// (ContentLength, GetBody for retries, chunked TransferEncoding, Trailer, and the
// original Host header) is copied across so a POST/PATCH body is forwarded intact
// — no round-trip loss on the proxy path.
func (a *AuthRoundTripper) viaProxy(req *http.Request) (*http.Request, error) {
	host := req.URL.Host
	if host == "" {
		return nil, fmt.Errorf("events: auth proxy: request has no host")
	}
	target := strings.TrimRight(a.ProxyBase, "/") + "/proxy/" + host + req.URL.RequestURI()
	out, err := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
	if err != nil {
		return nil, fmt.Errorf("events: auth proxy: build request: %w", err)
	}
	out.Header = req.Header.Clone()
	// Preserve body metadata the constructor drops for an opaque body reader.
	out.ContentLength = req.ContentLength
	out.GetBody = req.GetBody
	out.TransferEncoding = req.TransferEncoding
	out.Trailer = req.Trailer
	if req.Host != "" {
		out.Host = req.Host
	}
	return out, nil
}

// viaDirectLoad clones the request and, when a Loader yields a token for the
// host, sets a bearer Authorization header inline.
func (a *AuthRoundTripper) viaDirectLoad(req *http.Request) (*http.Request, error) {
	out := req.Clone(req.Context())
	if a.Loader == nil {
		return out, nil
	}
	token, err := a.Loader(req.URL.Host)
	if err != nil {
		return nil, fmt.Errorf("events: auth direct-load for %q: %w", req.URL.Host, err)
	}
	if strings.TrimSpace(token) != "" {
		out.Header.Set("Authorization", "Bearer "+token)
	}
	return out, nil
}
