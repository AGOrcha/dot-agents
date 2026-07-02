// handlers_test.go covers the behavioral surface around the golden contract:
// param validation (§1.3/§1.4 bad_request), error mapping (404/400/500),
// If-None-Match/304 (§1.5), pagination and filter passthrough, the health
// no-5xx guarantee (§3.6), request logging, and serving through the t06
// recompute-decorated store.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
)

// Request targets shared across the behavior tests.
const (
	runsTarget   = basePath + "/runs"
	healthTarget = basePath + "/health"
)

// stubStore satisfies store.Store with canned values and a single injectable
// error shared by every method (each test exercises one endpoint at a time).
type stubStore struct {
	runs   []store.RunSummary
	run    store.RunDetail
	its    []store.IterationSummary
	it     store.IterationDetail
	rubric store.RubricDoc
	health store.Health
	err    error
}

func (s *stubStore) ListRuns(context.Context, store.RunFilter) ([]store.RunSummary, error) {
	return s.runs, s.err
}

func (s *stubStore) GetRun(context.Context, string) (store.RunDetail, error) {
	return s.run, s.err
}

func (s *stubStore) ListIterations(context.Context, string) ([]store.IterationSummary, error) {
	return s.its, s.err
}

func (s *stubStore) GetIteration(context.Context, string, int) (store.IterationDetail, error) {
	return s.it, s.err
}

func (s *stubStore) Rubric(context.Context) (store.RubricDoc, error) {
	return s.rubric, s.err
}

func (s *stubStore) Health(context.Context) (store.Health, error) {
	return s.health, s.err
}

// newStubMount builds a Mount over st, failing the test on construction error.
func newStubMount(t *testing.T, st store.Store) *Mount {
	t.Helper()
	m, err := New(Deps{Store: st})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// decodeError unmarshals the §1.3 error envelope.
func decodeError(t *testing.T, body []byte) (code, message string) {
	t.Helper()
	var eb struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &eb); err != nil {
		t.Fatalf("unmarshal error envelope: %v; body: %s", err, body)
	}
	return eb.Error.Code, eb.Error.Message
}

// wantError asserts one request yields the expected §1.3 error envelope.
func wantError(t *testing.T, m *Mount, target string, status int, code string) string {
	t.Helper()
	rr := get(t, m, target, nil)
	if rr.Code != status {
		t.Fatalf("GET %s = %d, want %d; body: %s", target, rr.Code, status, rr.Body)
	}
	if ct := rr.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("error Content-Type = %q, want %q", ct, contentTypeJSON)
	}
	gotCode, msg := decodeError(t, rr.Body.Bytes())
	if gotCode != code {
		t.Errorf("GET %s error code = %q, want %q", target, gotCode, code)
	}
	return msg
}

func TestNewValidation(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrNilStore) {
		t.Fatalf("New without store = %v, want ErrNilStore", err)
	}
	m := newStubMount(t, &stubStore{})
	if m.Prefix() != "/api" {
		t.Fatalf("Prefix() = %q, want /api (the RegisterMount prefix)", m.Prefix())
	}
}

// TestBadRequestParams sweeps the §1.4/§3.1/§3.4 invalid-input surface.
func TestBadRequestParams(t *testing.T) {
	m := newStubMount(t, &stubStore{})
	targets := []string{
		basePath + "/runs?limit=0",
		basePath + "/runs?limit=-1",
		basePath + "/runs?limit=501",
		basePath + "/runs?limit=abc",
		basePath + "/runs?offset=-1",
		basePath + "/runs?offset=abc",
		basePath + "/runs?sort=nope",
		basePath + "/runs?order=up",
		basePath + "/runs?band=meh",
		basePath + "/runs/sess-a/iterations?limit=0",
		basePath + "/runs/sess-a/iterations?offset=-2",
		basePath + "/iterations/abc",
		basePath + "/iterations/0",
		basePath + "/iterations/-3",
	}
	for _, target := range targets {
		wantError(t, m, target, http.StatusBadRequest, codeBadRequest)
	}
}

// TestNotFoundMapping pins ErrNotFound → 404 with resource-specific messages.
func TestNotFoundMapping(t *testing.T) {
	m := newStubMount(t, &stubStore{err: store.ErrNotFound})
	if msg := wantError(t, m, basePath+"/runs/nope", http.StatusNotFound, codeNotFound); !strings.Contains(msg, `"nope"`) {
		t.Errorf("run 404 message %q should name the session id", msg)
	}
	wantError(t, m, basePath+"/runs/nope/iterations", http.StatusNotFound, codeNotFound)
	if msg := wantError(t, m, basePath+"/iterations/99", http.StatusNotFound, codeNotFound); !strings.Contains(msg, "99") {
		t.Errorf("iteration 404 message %q should name the iteration", msg)
	}
}

// TestRootNotAllowedMapping pins ErrRootNotAllowed → 400 (an iter_log_dir
// outside the resolved roots is invalid input, not a lookup miss).
func TestRootNotAllowedMapping(t *testing.T) {
	m := newStubMount(t, &stubStore{err: store.ErrRootNotAllowed})
	msg := wantError(t, m, basePath+"/iterations/1?iter_log_dir=/etc", http.StatusBadRequest, codeBadRequest)
	if !strings.Contains(msg, "iter_log_dir") {
		t.Errorf("message %q should name iter_log_dir", msg)
	}
}

// TestInternalErrorMapping pins unexpected store failures → 500 with a
// non-leaking message.
func TestInternalErrorMapping(t *testing.T) {
	m := newStubMount(t, &stubStore{err: errors.New("disk exploded: /secret/path")})
	for _, target := range []string{runsTarget, basePath + "/rubric"} {
		msg := wantError(t, m, target, http.StatusInternalServerError, codeInternal)
		if strings.Contains(msg, "secret") {
			t.Errorf("500 message %q must not leak internals", msg)
		}
	}
}

// TestHealthNeverReturns5xx pins §3.6: a store failure degrades health to a
// bare liveness payload, never an error status.
func TestHealthNeverReturns5xx(t *testing.T) {
	m := newStubMount(t, &stubStore{err: errors.New("boom")})
	rr := get(t, m, healthTarget, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("degraded health = %d, want 200", rr.Code)
	}
	var env struct {
		Data store.Health `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Status != "ok" || env.Data.Roots == nil {
		t.Errorf("degraded health payload = %+v, want status ok with empty roots", env.Data)
	}
}

// TestMarshalFailureIs500 drives the respond marshal error branch (a NaN
// score is unrepresentable in JSON).
func TestMarshalFailureIs500(t *testing.T) {
	nan := math.NaN()
	m := newStubMount(t, &stubStore{runs: []store.RunSummary{{SessionID: "x", Score: &nan}}})
	wantError(t, m, runsTarget, http.StatusInternalServerError, codeInternal)
}

// TestNilRunsMarshalsAsEmptyList pins the §1.3 resilience rule shape: a store
// returning a nil slice still yields 200 with data: [] and count 0.
func TestNilRunsMarshalsAsEmptyList(t *testing.T) {
	m := newStubMount(t, &stubStore{})
	rr := get(t, m, runsTarget, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty runs = %d, want 200", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"data":[]`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"count":0`)) {
		t.Errorf("nil runs must marshal as an empty list with count 0, got %s", rr.Body)
	}
}

// brokenWriter simulates a client that hung up mid-write.
type brokenWriter struct {
	http.ResponseWriter
}

// Write always fails, standing in for a dead connection.
func (brokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("client gone")
}

// TestWriteJSONLogsWriteFailure pins that a dead-connection write is logged,
// not panicked or silently dropped.
func TestWriteJSONLogsWriteFailure(t *testing.T) {
	var buf bytes.Buffer
	m, err := New(Deps{Store: &stubStore{}, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	if err != nil {
		t.Fatal(err)
	}
	m.writeJSON(brokenWriter{httptest.NewRecorder()}, http.StatusOK, envelope{Data: []byte(`{}`)})
	if !strings.Contains(buf.String(), "response write failed") {
		t.Errorf("write failure must be logged, got: %s", buf.String())
	}
}

// TestIfNoneMatch covers the §1.5 handshake: matching exact, weak, and
// wildcard candidates 304 with an empty body; a stale etag re-serves 200.
func TestIfNoneMatch(t *testing.T) {
	m := newFixtureMount(t)
	first := get(t, m, runsTarget, nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response must carry an ETag")
	}
	for _, header := range []string{etag, "W/" + etag, `"stale", ` + etag, "*"} {
		rr := get(t, m, runsTarget, map[string]string{"If-None-Match": header})
		if rr.Code != http.StatusNotModified {
			t.Errorf("If-None-Match %q = %d, want 304", header, rr.Code)
		}
		if rr.Body.Len() != 0 {
			t.Errorf("304 body must be empty, got %s", rr.Body)
		}
		if rr.Header().Get("ETag") != etag {
			t.Errorf("304 must echo the ETag header")
		}
	}
	rr := get(t, m, runsTarget, map[string]string{"If-None-Match": `"stale"`})
	if rr.Code != http.StatusOK {
		t.Errorf("stale If-None-Match = %d, want 200", rr.Code)
	}
}

// TestRubricETagIsVersion pins §3.5: the rubric response's etag is the rubric
// version itself.
func TestRubricETagIsVersion(t *testing.T) {
	m := newFixtureMount(t)
	rr := get(t, m, basePath+"/rubric", nil)
	var env struct {
		Data store.RubricDoc `json:"data"`
		Meta struct {
			ETag string `json:"etag"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Meta.ETag != env.Data.Version {
		t.Errorf("rubric etag = %q, want the rubric version %q", env.Meta.ETag, env.Data.Version)
	}
}

// decodeIterations unwraps an iteration-list envelope.
func decodeIterations(t *testing.T, body []byte) (its []store.IterationSummary, count *int) {
	t.Helper()
	var env struct {
		Data []store.IterationSummary `json:"data"`
		Meta struct {
			Count *int `json:"count"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	return env.Data, env.Meta.Count
}

// TestIterationsPagination covers §1.4 on the iteration list: handler-side
// limit/offset slicing and the out-of-range-offset empty page.
func TestIterationsPagination(t *testing.T) {
	m := newFixtureMount(t)
	rr := get(t, m, basePath+"/runs/sess-a/iterations?limit=1&offset=1", nil)
	its, count := decodeIterations(t, rr.Body.Bytes())
	if len(its) != 1 || its[0].Iteration != 2 || count == nil || *count != 1 {
		t.Errorf("limit=1 offset=1 page wrong: %d items, count %v", len(its), count)
	}

	rr = get(t, m, basePath+"/runs/sess-a/iterations?offset=99", nil)
	its, count = decodeIterations(t, rr.Body.Bytes())
	if len(its) != 0 || count == nil || *count != 0 {
		t.Errorf("out-of-range offset must yield an empty page, got %d items", len(its))
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"data":[]`)) {
		t.Errorf("empty page must marshal data as [], got %s", rr.Body)
	}
}

// TestRunsFilterAndSortPassthrough proves the validated §3.1 params reach the
// store: filters narrow the fixture's two sessions, sort flips their order.
func TestRunsFilterAndSortPassthrough(t *testing.T) {
	m := newFixtureMount(t)
	sids := func(target string) []string {
		rr := get(t, m, target, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s = %d; body: %s", target, rr.Code, rr.Body)
		}
		var env struct {
			Data []store.RunSummary `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(env.Data))
		for i, r := range env.Data {
			out[i] = r.SessionID
		}
		return out
	}

	if got := sids(basePath + "/runs?harness=codex"); len(got) != 1 || got[0] != "sess-b" {
		t.Errorf("harness=codex → %v, want [sess-b]", got)
	}
	if got := sids(basePath + "/runs?band=good"); len(got) != 1 || got[0] != "sess-a" {
		t.Errorf("band=good → %v, want [sess-a]", got)
	}
	asc := sids(basePath + "/runs?sort=session_id&order=asc")
	if len(asc) != 2 || asc[0] != "sess-a" || asc[1] != "sess-b" {
		t.Errorf("sort=session_id asc → %v", asc)
	}
}

// TestIterLogDirParam covers §1.6 disambiguation on the real store: naming a
// configured root works, naming anything else is a 400.
func TestIterLogDirParam(t *testing.T) {
	m := newFixtureMount(t)
	rr := get(t, m, basePath+"/iterations/2?iter_log_dir=iterlog", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("configured iter_log_dir = %d, want 200; body: %s", rr.Code, rr.Body)
	}
	wantError(t, m, basePath+"/iterations/2?iter_log_dir=/etc", http.StatusBadRequest, codeBadRequest)
}

// TestUnknownRoutesAndMethods pins mux behavior outside the route table,
// matching the review mount's precedent.
func TestUnknownRoutesAndMethods(t *testing.T) {
	m := newStubMount(t, &stubStore{})
	if rr := get(t, m, basePath+"/nope", nil); rr.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", rr.Code)
	}
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, runsTarget, nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST on a GET route = %d, want 405", rr.Code)
	}
}

// TestRequestLogging asserts the one-structured-line-per-request contract.
func TestRequestLogging(t *testing.T) {
	var buf bytes.Buffer
	m, err := New(Deps{
		Store:  &stubStore{},
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	get(t, m, healthTarget, nil)
	get(t, m, basePath+"/runs?limit=0", nil)
	logs := buf.String()
	for _, want := range []string{"method=GET", "path=" + healthTarget, "status=200", "status=400", "duration_ms="} {
		if !strings.Contains(logs, want) {
			t.Errorf("request log missing %q:\n%s", want, logs)
		}
	}
}

// TestServedThroughRecomputeStore serves the handlers through the t06
// recompute decorator — the production wiring. A fresh sidecar serves the
// plain read; the sidecar-less iteration routes through the recompute path
// (which degrades to the raw read when the scoring pipeline cannot run) and
// must still produce a 200 schema-conformant detail.
func TestServedThroughRecomputeStore(t *testing.T) {
	root := fixtureRoot(t)
	rs := store.NewRecompute(store.New([]string{root}), t.TempDir())
	defer rs.Flush()
	m, err := New(Deps{Store: rs})
	if err != nil {
		t.Fatal(err)
	}

	rr := get(t, m, basePath+"/iterations/2", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("fresh-sidecar detail = %d; body: %s", rr.Code, rr.Body)
	}
	var env struct {
		Data store.IterationDetail `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Data.Scored || env.Data.Score == nil || *env.Data.Score != 0.8 {
		t.Errorf("iteration 2 through recompute store = %+v, want scored 0.8", env.Data)
	}

	rr = get(t, m, basePath+"/iterations/5", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("recompute-path detail = %d; body: %s", rr.Code, rr.Body)
	}
	validateData(t, "dashboard-iteration.schema.json", rr.Body.Bytes())
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Iteration != 5 {
		t.Errorf("iteration = %d, want 5", env.Data.Iteration)
	}
}

// TestEmptyStoreListEndpointsServeEmpty pins the reviewer-verifies checklist
// item: with zero sidecars on disk (a real empty store root, not a stub), the
// runs list serves [] with 200 and health stays 200 — the dashboard renders
// an empty-but-live state, never an error.
func TestEmptyStoreListEndpointsServeEmpty(t *testing.T) {
	m, err := New(Deps{Store: store.New([]string{t.TempDir()})})
	if err != nil {
		t.Fatal(err)
	}
	runs := get(t, m, runsTarget, nil)
	if runs.Code != http.StatusOK {
		t.Fatalf("empty-root runs = %d, want 200", runs.Code)
	}
	if !bytes.Contains(runs.Body.Bytes(), []byte(`"data":[]`)) || !bytes.Contains(runs.Body.Bytes(), []byte(`"count":0`)) {
		t.Errorf("empty root must serve an empty runs list, got %s", runs.Body)
	}
	if health := get(t, m, healthTarget, nil); health.Code != http.StatusOK {
		t.Errorf("empty-root health = %d, want 200", health.Code)
	}
}

// TestETagCacheValidityContract pins the ETag's contractual properties
// implementation-agnostically (the string itself is opaque per API.md §1.5,
// ratified content-derived): identical resource+content yields an identical
// ETag across requests; a content change yields a different ETag; distinct
// resources yield distinct ETags; and a matching If-None-Match round-trips to
// 304.
func TestETagCacheValidityContract(t *testing.T) {
	st := &stubStore{runs: []store.RunSummary{{SessionID: "s1"}}}
	m := newStubMount(t, st)

	first := get(t, m, runsTarget, nil).Header().Get("ETag")
	if first == "" {
		t.Fatal("runs response must carry an ETag")
	}
	// Same resource + unchanged content => identical ETag.
	if again := get(t, m, runsTarget, nil).Header().Get("ETag"); again != first {
		t.Errorf("stable content must yield a stable ETag: %q != %q", again, first)
	}
	// Content change => different ETag.
	st.runs = []store.RunSummary{{SessionID: "s1"}, {SessionID: "s2"}}
	if changed := get(t, m, runsTarget, nil).Header().Get("ETag"); changed == first {
		t.Errorf("changed content must yield a new ETag, still %q", changed)
	}
	// Distinct resource => distinct ETag (health vs runs).
	if healthTag := get(t, m, healthTarget, nil).Header().Get("ETag"); healthTag == first {
		t.Errorf("distinct resources must yield distinct ETags, both %q", first)
	}
	// Matching If-None-Match round-trips to 304.
	st.runs = []store.RunSummary{{SessionID: "s1"}}
	tag := get(t, m, runsTarget, nil).Header().Get("ETag")
	if rr := get(t, m, runsTarget, map[string]string{"If-None-Match": tag}); rr.Code != http.StatusNotModified {
		t.Errorf("matching If-None-Match = %d, want 304", rr.Code)
	}
}
