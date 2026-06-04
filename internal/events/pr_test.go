package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDeriveRollupState(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   string
	}{
		{"empty is green", nil, RollupGreen},
		{"all completed success is green", []Check{
			{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "test", Status: "COMPLETED", Conclusion: "SUCCESS"},
		}, RollupGreen},
		{"any failure is failing", []Check{
			{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "lint", Status: "COMPLETED", Conclusion: "FAILURE"},
		}, RollupFailing},
		{"timed out is failing", []Check{
			{Name: "e2e", Status: "COMPLETED", Conclusion: "TIMED_OUT"},
		}, RollupFailing},
		{"cancelled is failing", []Check{
			{Name: "deploy", Status: "COMPLETED", Conclusion: "CANCELLED"},
		}, RollupFailing},
		{"failure beats pending", []Check{
			{Name: "queued", Status: "QUEUED", Conclusion: ""},
			{Name: "lint", Status: "COMPLETED", Conclusion: "FAILURE"},
		}, RollupFailing},
		{"in-progress is pending", []Check{
			{Name: "build", Status: "IN_PROGRESS", Conclusion: ""},
		}, RollupPending},
		{"queued is pending", []Check{
			{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "test", Status: "QUEUED", Conclusion: ""},
		}, RollupPending},
		{"case-insensitive failure", []Check{
			{Name: "lint", Status: "completed", Conclusion: "failure"},
		}, RollupFailing},
		{"case-insensitive completed is green", []Check{
			{Name: "build", Status: "completed", Conclusion: "success"},
		}, RollupGreen},
		{"whitespace trimmed", []Check{
			{Name: "build", Status: " COMPLETED ", Conclusion: " SUCCESS "},
		}, RollupGreen},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveRollupState(tc.checks); got != tc.want {
				t.Errorf("DeriveRollupState(%v) = %q, want %q", tc.checks, got, tc.want)
			}
		})
	}
}

func TestPRKindsCoverDesignKinds(t *testing.T) {
	got := PRKinds()
	want := []string{
		"event.pr.opened", "event.pr.ci_green", "event.pr.ci_failed",
		"event.pr.merged", "event.pr.closed", "event.pr.force_rebased",
		"event.pr.review_requested_change", "event.pr.comment_posted",
	}
	if len(got) != len(want) {
		t.Fatalf("PRKinds count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("PRKinds[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRegisterPRKinds(t *testing.T) {
	r := NewRegistry()
	if err := RegisterPRKinds(r); err != nil {
		t.Fatalf("RegisterPRKinds: %v", err)
	}
	// Every kind is registered with reject (control-plane) disposition.
	for _, name := range PRKinds() {
		k, ok := r.Lookup(name)
		if !ok {
			t.Errorf("kind %q not registered", name)
			continue
		}
		if k.Disposition != DispositionReject {
			t.Errorf("kind %q disposition = %v, want reject", name, k.Disposition)
		}
	}
	// The namespace itself rejects unknown event.pr.* types at emit time.
	if d := r.DispositionFor("event.pr.totally_unknown"); d != DispositionReject {
		t.Errorf("unknown event.pr.* disposition = %v, want reject", d)
	}
}

func TestRegisterPRKindsRejectsViaDispatcher(t *testing.T) {
	r := NewRegistry()
	if err := RegisterPRKinds(r); err != nil {
		t.Fatalf("RegisterPRKinds: %v", err)
	}
	d := NewDispatcher(r, nil)
	// An unhandled but mistyped control-plane PR event must fail loudly.
	env, err := NewEnvelope("event.pr.mrged", "github", "k1", time.Time{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if err := d.Dispatch(env); err == nil {
		t.Fatal("expected reject for mistyped event.pr.* type, got nil")
	}
}

func TestRegisterPRKindsNilRegistry(t *testing.T) {
	if err := RegisterPRKinds(nil); err == nil {
		t.Fatal("expected error for nil registry")
	}
}

func TestRegisterPRKindsIdempotent(t *testing.T) {
	r := NewRegistry()
	if err := RegisterPRKinds(r); err != nil {
		t.Fatalf("first RegisterPRKinds: %v", err)
	}
	if err := RegisterPRKinds(r); err != nil {
		t.Fatalf("second RegisterPRKinds: %v", err)
	}
	if got := len(r.Names()); got != len(PRKinds()) {
		t.Errorf("after double-register, kind count = %d, want %d", got, len(PRKinds()))
	}
}

func TestDefaultGHPRSource(t *testing.T) {
	c := DefaultGHPRSource()
	if c.Producer != "gh" {
		t.Errorf("Producer = %q, want gh", c.Producer)
	}
	if len(c.List.Argv) == 0 || c.List.Argv[0] != "gh" {
		t.Errorf("List.Argv = %v, want gh ...", c.List.Argv)
	}
	if c.List.Map["number"] != ".number" {
		t.Errorf("List.Map[number] = %q, want .number", c.List.Map["number"])
	}
	if c.List.Map["branch"] != ".headRefName" {
		t.Errorf("List.Map[branch] = %q, want .headRefName", c.List.Map["branch"])
	}
	if c.Comments.Each != ".comments" {
		t.Errorf("Comments.Each = %q, want .comments", c.Comments.Each)
	}
	if c.PollIntervalS != 270 {
		t.Errorf("PollIntervalS = %d, want 270", c.PollIntervalS)
	}
}

func TestProducerConfigForList(t *testing.T) {
	c := DefaultGHPRSource()
	cfg, err := c.ProducerConfigForList("event.pr.merged", "github")
	if err != nil {
		t.Fatalf("ProducerConfigForList: %v", err)
	}
	if cfg.Type != "event.pr.merged" {
		t.Errorf("Type = %q", cfg.Type)
	}
	if cfg.Source != "github" {
		t.Errorf("Source = %q", cfg.Source)
	}
	if cfg.KeyBy != "number" {
		t.Errorf("KeyBy = %q, want number", cfg.KeyBy)
	}
	// The gh list block uses identity Each ("."), so the engine config points
	// at the synthetic wrapper key.
	if cfg.Each != "."+identityEachKey {
		t.Errorf("Each = %q, want identity-wrapped %q", cfg.Each, "."+identityEachKey)
	}
	if len(cfg.Fetch.Argv) == 0 {
		t.Error("Fetch.Argv empty")
	}
}

func TestProducerConfigForListExplicitEachPreserved(t *testing.T) {
	c := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"x"},
			Each: ".pulls",
			Map:  map[string]string{"number": ".number"},
		},
	}
	cfg, err := c.ProducerConfigForList("event.pr.opened", "x")
	if err != nil {
		t.Fatalf("ProducerConfigForList: %v", err)
	}
	if cfg.Each != ".pulls" {
		t.Errorf("Each = %q, want .pulls (explicit, not identity-wrapped)", cfg.Each)
	}
}

func TestProducerConfigForListSourceDefaultsToProducer(t *testing.T) {
	c := DefaultGHPRSource()
	cfg, err := c.ProducerConfigForList("event.pr.merged", "")
	if err != nil {
		t.Fatalf("ProducerConfigForList: %v", err)
	}
	if cfg.Source != "gh" {
		t.Errorf("Source = %q, want gh (defaulted from producer)", cfg.Source)
	}
}

func TestProducerConfigForListDefaultsEach(t *testing.T) {
	c := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"echo", "[]"},
			Map:  map[string]string{"number": ".number"},
		},
	}
	cfg, err := c.ProducerConfigForList("event.pr.opened", "x")
	if err != nil {
		t.Fatalf("ProducerConfigForList: %v", err)
	}
	// Empty Each is identity, so it resolves to the synthetic wrapper key.
	if cfg.Each != "."+identityEachKey {
		t.Errorf("Each = %q, want identity-wrapped %q", cfg.Each, "."+identityEachKey)
	}
}

func TestProducerConfigForListErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  PRSourceConfig
		typ  string
	}{
		{"empty type", DefaultGHPRSource(), ""},
		{"empty map", PRSourceConfig{List: FetchBlock{Argv: []string{"x"}}}, "event.pr.merged"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.cfg.ProducerConfigForList(tc.typ, "src"); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestNewListProducer(t *testing.T) {
	c := DefaultGHPRSource()
	// A complete gh `pr list --json` shape: every mapped field is present,
	// including the statusCheckRollup the rollup derive step consumes.
	doc := `[{"number":1,"title":"t","headRefName":"feat","baseRefName":"main","state":"OPEN","mergeable":"MERGEABLE","url":"https://x/1","statusCheckRollup":[{"name":"build","status":"COMPLETED","conclusion":"SUCCESS"}]}]`
	p, err := c.NewListProducer("event.pr.opened", "github", &fakeFetcher{out: []byte(doc)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	if envs[0].Type != "event.pr.opened" {
		t.Errorf("Type = %q", envs[0].Type)
	}
	// number is the snapshot key.
	if envs[0].IdempotencyKey != "1" {
		t.Errorf("IdempotencyKey = %q, want 1", envs[0].IdempotencyKey)
	}
	// The default gh source path derives the rollup: the raw statusCheckRollup
	// array is replaced in the payload by a canonical Rollup whose State came
	// from DeriveRollupState (one COMPLETED/SUCCESS check -> GREEN).
	pr := decodePR(t, envs[0].Payload)
	if pr.Rollup.State != RollupGreen {
		t.Errorf("derived Rollup.State = %q, want %q", pr.Rollup.State, RollupGreen)
	}
	if len(pr.Rollup.Checks) != 1 || pr.Rollup.Checks[0].Name != "build" {
		t.Errorf("Rollup.Checks = %+v, want one 'build' check", pr.Rollup.Checks)
	}
}

func TestNewListProducerInvalidConfig(t *testing.T) {
	c := PRSourceConfig{List: FetchBlock{Argv: []string{"x"}}} // empty map
	if _, err := c.NewListProducer("event.pr.merged", "src", &fakeFetcher{}); err == nil {
		t.Fatal("expected error from invalid config")
	}
}

func TestNewListProducerNilFetcherIdentityWraps(t *testing.T) {
	// A nil fetcher on an identity-Each block defaults to the real fetcher then
	// wraps it. Constructing the producer must succeed; we do not run Cycle (it
	// would exec). This exercises the nil-fetcher default + wrap branch.
	c := DefaultGHPRSource()
	p, err := c.NewListProducer("event.pr.opened", "github", nil)
	if err != nil {
		t.Fatalf("NewListProducer(nil fetcher): %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil producer")
	}
}

func TestIdentityFetcherPassesThroughNonArray(t *testing.T) {
	// A non-array document is returned unchanged so the engine raises its own
	// "each is not a list" error rather than the wrapper masking it.
	c := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"x"},
			Each: ".", // identity -> wrapped
			Map:  map[string]string{"number": ".number"},
		},
	}
	p, err := c.NewListProducer("event.pr.opened", "x", &fakeFetcher{out: []byte(`{"not":"an array"}`)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	if _, err := p.Cycle(context.Background()); err == nil {
		t.Fatal("expected engine error for non-array identity document")
	}
}

func TestIdentityFetcherPropagatesInnerError(t *testing.T) {
	c := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"x"},
			Each: ".",
			Map:  map[string]string{"number": ".number"},
		},
	}
	p, err := c.NewListProducer("event.pr.opened", "x", &fakeFetcher{err: errors.New("boom")})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	if _, err := p.Cycle(context.Background()); err == nil {
		t.Fatal("expected inner fetcher error to propagate")
	}
}

// TestNonGitHubShapeMapsToPREvents proves the done-criteria: adding a non-GitHub
// platform is config only. A GitLab-shaped JSON document maps onto event.pr.*
// through a different field map with zero Go change.
func TestNonGitHubShapeMapsToPREvents(t *testing.T) {
	gitlab := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"glab", "mr", "list", "--output", "json"},
			Each: ".",
			Map: map[string]string{
				"number":    ".iid",
				"branch":    ".source_branch",
				"mergeable": ".merge_status",
				"state":     ".state",
			},
		},
	}
	doc := `[{"iid":42,"source_branch":"feature/x","merge_status":"can_be_merged","state":"opened"}]`
	p, err := gitlab.NewListProducer("event.pr.opened", "gitlab", &fakeFetcher{out: []byte(doc)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	var rec map[string]any
	if err := json.Unmarshal(envs[0].Payload, &rec); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if rec["number"] != float64(42) {
		t.Errorf("number = %v, want 42", rec["number"])
	}
	if rec["branch"] != "feature/x" {
		t.Errorf("branch = %v, want feature/x", rec["branch"])
	}
	// keyed by the canonical "number" field even though the source field is .iid.
	if envs[0].IdempotencyKey != "42" {
		t.Errorf("IdempotencyKey = %q, want 42", envs[0].IdempotencyKey)
	}
}

// TestDefaultGHSourceDerivesRollupEndToEnd is the finding-2 regression: the
// D3/R2 DeriveRollupState rule must be reachable and exercised by a default
// (gh) source fetch — not dead code. A gh-shaped document with a failing check
// must produce an event.pr.* envelope whose payload rollup is derived to FAILING.
func TestDefaultGHSourceDerivesRollupEndToEnd(t *testing.T) {
	tests := []struct {
		name      string
		rollup    string
		wantState string
		wantLen   int
	}{
		{
			name:      "failure derives FAILING",
			rollup:    `[{"name":"build","status":"COMPLETED","conclusion":"SUCCESS"},{"name":"lint","status":"COMPLETED","conclusion":"FAILURE"}]`,
			wantState: RollupFailing,
			wantLen:   2,
		},
		{
			name:      "in-progress derives PENDING",
			rollup:    `[{"name":"build","status":"IN_PROGRESS","conclusion":""}]`,
			wantState: RollupPending,
			wantLen:   1,
		},
		{
			name:      "all-success derives GREEN",
			rollup:    `[{"name":"build","status":"COMPLETED","conclusion":"SUCCESS"}]`,
			wantState: RollupGreen,
			wantLen:   1,
		},
		{
			// Silent-zero guard: gh returns [] when a PR has no checks. That is
			// an explicit empty-check rollup (GREEN), not a dropped field.
			name:      "empty checks derive GREEN not dropped",
			rollup:    `[]`,
			wantState: RollupGreen,
			wantLen:   0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertGHRollupCase(t, tc.rollup, tc.wantState, tc.wantLen)
		})
	}
}

// assertGHRollupCase runs one default-gh-source rollup case end-to-end: it builds
// the gh list document, cycles the producer, and asserts the derived rollup state
// and check count. Extracted from the table loop so the assertions stay flat
// instead of nesting inside the t.Run closure.
func assertGHRollupCase(t *testing.T, rollup, wantState string, wantLen int) {
	t.Helper()
	doc := `[{"number":7,"title":"t","headRefName":"f","baseRefName":"main","state":"OPEN","mergeable":"MERGEABLE","url":"u","statusCheckRollup":` + rollup + `}]`
	p, err := DefaultGHPRSource().NewListProducer("event.pr.ci_failed", "github", &fakeFetcher{out: []byte(doc)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	pr := decodePR(t, envs[0].Payload)
	if pr.Rollup.State != wantState {
		t.Errorf("derived Rollup.State = %q, want %q", pr.Rollup.State, wantState)
	}
	if len(pr.Rollup.Checks) != wantLen {
		t.Errorf("Rollup.Checks len = %d, want %d", len(pr.Rollup.Checks), wantLen)
	}
}

// TestDeriveRollupPreservesOtherFields guards round-trip loss: deriving the
// rollup must not drop the other mapped PR fields from the payload.
func TestDeriveRollupPreservesOtherFields(t *testing.T) {
	doc := `[{"number":9,"title":"hello","headRefName":"feat/x","baseRefName":"main","state":"OPEN","mergeable":"MERGEABLE","url":"https://x/9","statusCheckRollup":[{"name":"t","status":"COMPLETED","conclusion":"SUCCESS"}]}]`
	p, err := DefaultGHPRSource().NewListProducer("event.pr.opened", "github", &fakeFetcher{out: []byte(doc)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	pr := decodePR(t, envs[0].Payload)
	if pr.Number != 9 || pr.Title != "hello" || pr.Branch != "feat/x" || pr.BaseRef != "main" || pr.URL != "https://x/9" {
		t.Errorf("non-rollup fields lost in derive: %+v", pr)
	}
}

// TestPRProducerCycleNoRollupPassThrough proves a source without a rollup field
// (e.g. a comments-style map) is emitted unchanged — the derive is a no-op and
// does not invent a rollup.
func TestPRProducerCycleNoRollupPassThrough(t *testing.T) {
	c := PRSourceConfig{
		Producer: "exec",
		List: FetchBlock{
			Argv: []string{"x"},
			Each: ".",
			Map:  map[string]string{"number": ".number", "title": ".title"},
		},
	}
	doc := `[{"number":3,"title":"no-rollup"}]`
	p, err := c.NewListProducer("event.pr.opened", "x", &fakeFetcher{out: []byte(doc)})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	envs, err := p.Cycle(context.Background())
	if err != nil {
		t.Fatalf("Cycle: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d envelopes, want 1", len(envs))
	}
	var rec map[string]any
	if err := json.Unmarshal(envs[0].Payload, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := rec[rollupField]; ok {
		t.Errorf("payload gained a rollup field for a no-rollup source: %v", rec)
	}
}

// TestPRProducerCyclePropagatesInnerError ensures a fetch/map error from the
// inner engine surfaces through the PR wrapper rather than being swallowed.
func TestPRProducerCyclePropagatesInnerError(t *testing.T) {
	p, err := DefaultGHPRSource().NewListProducer("event.pr.opened", "github", &fakeFetcher{err: errors.New("boom")})
	if err != nil {
		t.Fatalf("NewListProducer: %v", err)
	}
	if _, err := p.Cycle(context.Background()); err == nil {
		t.Fatal("expected inner fetch error to propagate")
	}
}

// TestDeriveEnvelopeRollupDecodeError covers the decode-failure branch: an
// envelope whose payload is not a JSON object surfaces an error rather than a
// silent pass-through.
func TestDeriveEnvelopeRollupDecodeError(t *testing.T) {
	env := Envelope{
		Type:           "event.pr.opened",
		Source:         "github",
		IdempotencyKey: "1",
		OccurredAt:     time.Now().UTC(),
		Payload:        json.RawMessage(`not-json`),
	}
	if _, err := deriveEnvelopeRollup(env); err == nil {
		t.Fatal("expected decode error for malformed payload")
	}
}

// TestDeriveEnvelopeRollupNoRollupUnchanged covers the no-rollup-field branch at
// the function level: the envelope is returned byte-identical.
func TestDeriveEnvelopeRollupNoRollupUnchanged(t *testing.T) {
	in := Envelope{
		Type:           "event.pr.opened",
		Source:         "github",
		IdempotencyKey: "1",
		OccurredAt:     time.Now().UTC(),
		Payload:        json.RawMessage(`{"number":1}`),
	}
	out, err := deriveEnvelopeRollup(in)
	if err != nil {
		t.Fatalf("deriveEnvelopeRollup: %v", err)
	}
	if string(out.Payload) != string(in.Payload) {
		t.Errorf("payload changed for no-rollup envelope: %s", out.Payload)
	}
}

// TestChecksFromRawTolerant covers the tolerant projection: non-array and
// non-object elements yield no/partial checks rather than panicking.
func TestChecksFromRawTolerant(t *testing.T) {
	if got := checksFromRaw("not-an-array"); got != nil {
		t.Errorf("checksFromRaw(non-array) = %v, want nil", got)
	}
	if got := checksFromRaw(nil); got != nil {
		t.Errorf("checksFromRaw(nil) = %v, want nil", got)
	}
	mixed := []any{
		"skip-me",
		map[string]any{"name": "build", "status": "COMPLETED", "conclusion": "SUCCESS", "detailsUrl": "http://ci/1"},
	}
	checks := checksFromRaw(mixed)
	if len(checks) != 1 {
		t.Fatalf("got %d checks, want 1 (non-object skipped)", len(checks))
	}
	if checks[0].Link != "http://ci/1" {
		t.Errorf("Link = %q, want detailsUrl fallback", checks[0].Link)
	}
}

// TestAuthRoundTripperProxyPreservesBodyMetadata is the finding-3 regression: the
// proxy path must forward request body metadata (ContentLength, GetBody, Host)
// rather than dropping it when it rebuilds the request.
func TestAuthRoundTripperProxyPreservesBodyMetadata(t *testing.T) {
	var gotLen int64
	var gotBody string
	injector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer injector.Close()

	rt := &AuthRoundTripper{ProxyBase: injector.URL}
	body := `{"title":"x"}`
	// http.NewRequest sets ContentLength + GetBody for a strings.Reader body.
	req, err := http.NewRequest(http.MethodPost, "http://api.github.com/repos/x/pulls", strings.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "api.github.com"

	out, err := rt.decorate(req)
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}
	if out.ContentLength != int64(len(body)) {
		t.Errorf("ContentLength = %d, want %d", out.ContentLength, len(body))
	}
	if out.GetBody == nil {
		t.Error("GetBody dropped on proxy rewrite (breaks redirect/retry replay)")
	}
	if out.Host != "api.github.com" {
		t.Errorf("Host = %q, want api.github.com preserved", out.Host)
	}

	// End-to-end: the body actually reaches the injector with the right length.
	resp, err := (&http.Client{Transport: rt}).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if gotLen != int64(len(body)) {
		t.Errorf("injector saw ContentLength %d, want %d", gotLen, len(body))
	}
	if gotBody != body {
		t.Errorf("injector saw body %q, want %q", gotBody, body)
	}
}

func TestAuthRoundTripperDirectLoadAttachesBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &AuthRoundTripper{
		Loader: func(host string) (string, error) { return "tok-123", nil },
	}
	client := &http.Client{Transport: rt}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want 'Bearer tok-123'", gotAuth)
	}
}

func TestAuthRoundTripperDirectLoadNoLoaderPassesThrough(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &AuthRoundTripper{} // no proxy, no loader
	client := &http.Client{Transport: rt}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (no loader)", gotAuth)
	}
}

func TestAuthRoundTripperDirectLoadEmptyTokenNoHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rt := &AuthRoundTripper{
		Loader: func(host string) (string, error) { return "   ", nil },
	}
	client := &http.Client{Transport: rt}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (blank token)", gotAuth)
	}
}

func TestAuthRoundTripperDirectLoadLoaderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wantErr := errors.New("vault unreachable")
	rt := &AuthRoundTripper{
		Loader: func(host string) (string, error) { return "", wantErr },
	}
	client := &http.Client{Transport: rt}
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error from loader, got nil")
	}
	if !strings.Contains(err.Error(), "vault unreachable") {
		t.Errorf("error = %v, want it to wrap loader error", err)
	}
}

func TestAuthRoundTripperProxyRewritesURL(t *testing.T) {
	var gotPath string
	var gotHost string
	injector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer injector.Close()

	rt := &AuthRoundTripper{ProxyBase: injector.URL}
	client := &http.Client{Transport: rt}
	resp, err := client.Get("http://api.github.com/repos/x/pulls")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	// The original host is preserved in the proxy path so the injector can
	// attach the right credential.
	if !strings.HasPrefix(gotPath, "/proxy/api.github.com") {
		t.Errorf("proxy path = %q, want prefix /proxy/api.github.com", gotPath)
	}
	if !strings.Contains(gotPath, "/repos/x/pulls") {
		t.Errorf("proxy path = %q, want to carry original request URI", gotPath)
	}
	if gotHost == "" {
		t.Error("expected injector to receive a host")
	}
}

func TestAuthRoundTripperProxyNoHost(t *testing.T) {
	rt := &AuthRoundTripper{ProxyBase: "http://localhost:8765"}
	// A request with an empty URL host is rejected before any network call.
	req := &http.Request{Method: http.MethodGet, URL: mustParseRelative(t, "/no-host"), Header: http.Header{}}
	_, err := rt.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error for request with no host")
	}
	if !strings.Contains(err.Error(), "no host") {
		t.Errorf("error = %v, want 'no host'", err)
	}
}

func TestAuthRoundTripperProxyTrimsTrailingSlash(t *testing.T) {
	var gotPath string
	injector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer injector.Close()

	rt := &AuthRoundTripper{ProxyBase: injector.URL + "/"}
	client := &http.Client{Transport: rt}
	resp, err := client.Get("http://example.com/x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	if strings.Contains(gotPath, "//proxy") {
		t.Errorf("proxy path = %q, trailing slash not trimmed", gotPath)
	}
}

func TestAuthRoundTripperUsesCustomBase(t *testing.T) {
	called := false
	rt := &AuthRoundTripper{
		Base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: 204, Body: http.NoBody, Header: http.Header{}}, nil
		}),
	}
	req := mustGetRequest(t, "http://example.com/a")
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()
	if !called {
		t.Error("custom Base transport was not used")
	}
}

func TestAuthRoundTripperProxyBuildError(t *testing.T) {
	rt := &AuthRoundTripper{ProxyBase: "://bad base"}
	req := mustGetRequest(t, "http://example.com/a")
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("expected build-request error for malformed proxy base")
	}
}

// --- test helpers ---

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustGetRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func mustParseRelative(t *testing.T, path string) *url.URL {
	t.Helper()
	u, err := url.Parse(path)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}

// decodePR unmarshals an envelope payload into the canonical PR shape so a test
// can assert on the derived rollup. Kept trivial (single decode) so it stays well
// under the cognitive-complexity gate.
func decodePR(t *testing.T, payload json.RawMessage) PR {
	t.Helper()
	var pr PR
	if err := json.Unmarshal(payload, &pr); err != nil {
		t.Fatalf("decode PR payload: %v", err)
	}
	return pr
}
