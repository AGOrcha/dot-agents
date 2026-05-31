package config

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// recordingEmitter is a concurrency-safe AuditEmitter test double that captures
// every event for assertion. Resolution fetches extends layers in parallel, so
// Emit must be safe under concurrent calls.
type recordingEmitter struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (e *recordingEmitter) Emit(evt AuditEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, evt)
}

func (e *recordingEmitter) snapshot() []AuditEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]AuditEvent, len(e.events))
	copy(out, e.events)
	return out
}

// byAction returns the events for a given action in capture order.
func (e *recordingEmitter) byAction(action string) []AuditEvent {
	var out []AuditEvent
	for _, ev := range e.snapshot() {
		if ev.Action == action {
			out = append(out, ev)
		}
	}
	return out
}

// --- emitter seam unit tests -----------------------------------------------

func TestNoopEmitterDiscards(t *testing.T) {
	// The no-op emitter must accept events without panicking and is the shared
	// default; two calls return the same value-typed sink.
	e := NoopEmitter()
	e.Emit(AuditEvent{Action: ActionSourceFetch})
	if NoopEmitter() != e {
		t.Errorf("NoopEmitter not stable: %v vs %v", NoopEmitter(), e)
	}
}

func TestNewAuditTraceNormalizesNilEmitter(t *testing.T) {
	// A nil emitter must normalize to the no-op sink so emission sites never
	// nil-panic, and the trace id must be a fresh non-empty hex string.
	tr := newAuditTrace(nil)
	if tr.emitter == nil {
		t.Fatal("nil emitter was not normalized")
	}
	tr.emit(AuditEvent{Action: ActionEffectiveProduced}) // must not panic
	if tr.traceID == "" {
		t.Error("trace id is empty")
	}
}

func TestAuditTraceStampsBaseFields(t *testing.T) {
	rec := &recordingEmitter{}
	tr := newAuditTrace(rec)
	tr.emit(AuditEvent{Action: ActionLayerResolve, Target: "acme:org/base.json", Outcome: OutcomeSuccess})

	got := rec.snapshot()
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	ev := got[0]
	if ev.Actor != auditActor {
		t.Errorf("actor = %q, want %q", ev.Actor, auditActor)
	}
	if ev.TraceID != tr.traceID || ev.TraceID == "" {
		t.Errorf("trace id = %q, want %q", ev.TraceID, tr.traceID)
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp not stamped")
	}
}

func TestNewTraceIDUnique(t *testing.T) {
	a, b := newTraceID(), newTraceID()
	if a == b {
		t.Errorf("trace ids collided: %q", a)
	}
	if len(a) != 32 {
		t.Errorf("trace id len = %d, want 32 hex chars", len(a))
	}
}

func TestImportFailedEventOptionalVsFatal(t *testing.T) {
	ie := &ImportError{Ref: "acme:org/base.json", SourceID: "acme", Reason: ReasonTransport, Err: errors.New("boom")}

	fatal := importFailedEvent(ie, false)
	if fatal.Outcome != OutcomeFailure {
		t.Errorf("fatal outcome = %q, want %q", fatal.Outcome, OutcomeFailure)
	}
	if fatal.Fields["reason"] != string(ReasonTransport) {
		t.Errorf("reason = %v, want transport", fatal.Fields["reason"])
	}
	if fatal.Fields["source_id"] != "acme" {
		t.Errorf("source_id = %v, want acme", fatal.Fields["source_id"])
	}
	if fatal.Fields["detail"] != "boom" {
		t.Errorf("detail = %v, want boom", fatal.Fields["detail"])
	}

	opt := importFailedEvent(ie, true)
	if opt.Outcome != OutcomeSkipped {
		t.Errorf("optional outcome = %q, want %q", opt.Outcome, OutcomeSkipped)
	}
	if opt.Fields["optional"] != true {
		t.Errorf("optional flag = %v, want true", opt.Fields["optional"])
	}
}

func TestImportFailedEventOmitsEmptyFields(t *testing.T) {
	// A bare ImportError (no source id, no underlying error) must not carry
	// empty source_id/detail keys.
	ie := &ImportError{Ref: "x:y", Reason: ReasonSchema}
	ev := importFailedEvent(ie, false)
	if _, ok := ev.Fields["source_id"]; ok {
		t.Error("source_id present for empty SourceID")
	}
	if _, ok := ev.Fields["detail"]; ok {
		t.Error("detail present for nil Err")
	}
}

func TestEventConstructors(t *testing.T) {
	sf := sourceFetchEvent("acme", "deadbeef", true)
	if sf.Action != ActionSourceFetch || sf.Target != "acme" || sf.Fields["cache_hit"] != true || sf.Fields["resolved_sha"] != "deadbeef" {
		t.Errorf("sourceFetchEvent wrong: %+v", sf)
	}

	lr := layerResolveEvent("acme:org/base.json", "sha123", 3)
	if lr.Action != ActionLayerResolve || lr.Fields["field_count"] != 3 || lr.Fields["sha"] != "sha123" {
		t.Errorf("layerResolveEvent wrong: %+v", lr)
	}

	fo := fieldOverriddenEvent("skills", "user-local", "repo-local", []any{"a", "b"})
	if fo.Action != ActionFieldOverridden || fo.Fields["from_layer"] != "user-local" || fo.Fields["to_layer"] != "repo-local" {
		t.Errorf("fieldOverriddenEvent wrong: %+v", fo)
	}
	if fo.Fields["value_summary"] != "[array len=2]" {
		t.Errorf("value_summary = %v, want [array len=2]", fo.Fields["value_summary"])
	}

	pv := protectionViolationEvent("repo_id", "acme:org/base.json")
	if pv.Action != ActionFieldProtectionViolation || pv.Outcome != OutcomeDropped || pv.Fields["attempted_by_layer"] != "acme:org/base.json" {
		t.Errorf("protectionViolationEvent wrong: %+v", pv)
	}

	ep := effectiveProducedEvent("github.com/acme/app", 4)
	if ep.Action != ActionEffectiveProduced || ep.Target != "github.com/acme/app" || ep.Fields["layer_count"] != 4 {
		t.Errorf("effectiveProducedEvent wrong: %+v", ep)
	}
}

func TestSummarizeValue(t *testing.T) {
	long := strings.Repeat("x", 80)
	cases := []struct {
		in   any
		want string
	}{
		{nil, "null"},
		{"short", "short"},
		{long, strings.Repeat("x", 64) + "…"},
		{true, "true"},
		{float64(3), "3"},
		{[]any{1, 2, 3}, "[array len=3]"},
		{map[string]any{"a": 1, "b": 2}, "{object keys=2}"},
		{42, "42"}, // default branch: non-JSON type
	}
	for _, c := range cases {
		if got := summarizeValue(c.in); got != c.want {
			t.Errorf("summarizeValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("truncate long = %q", got)
	}
}

func TestAsImportError(t *testing.T) {
	// Already an ImportError (possibly wrapped): unwrapped through.
	ie := &ImportError{Ref: "x:y", Reason: ReasonAuth}
	if got := asImportError("x:y", ie); got != ie {
		t.Errorf("asImportError did not pass through: %+v", got)
	}
	wrapped := asImportError("a:b", errors.New("plain"))
	if wrapped.Reason != ReasonContent || wrapped.Ref != "a:b" || wrapped.Err == nil {
		t.Errorf("asImportError wrap wrong: %+v", wrapped)
	}
}

// --- end-to-end emission through the resolver ------------------------------

func TestResolveEmitsLayerAndSourceAndEffectiveEvents(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["from-git"],"agents":["a1"]}`},
		sha:   "deadbeefcafe0000000000000000000000000000",
	}
	rec := &recordingEmitter{}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).WithEmitter(rec).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	_ = snap

	// config.source.fetch — one per fetched layer, carrying the resolved sha.
	sf := rec.byAction(ActionSourceFetch)
	if len(sf) != 1 {
		t.Fatalf("source.fetch events = %d, want 1", len(sf))
	}
	if sf[0].Target != "acme" || sf[0].Fields["resolved_sha"] != fake.sha || sf[0].Fields["cache_hit"] != false {
		t.Errorf("source.fetch fields wrong: %+v", sf[0])
	}

	// config.layer.resolve — one per validated layer with field_count + sha.
	lr := rec.byAction(ActionLayerResolve)
	if len(lr) != 1 {
		t.Fatalf("layer.resolve events = %d, want 1", len(lr))
	}
	if lr[0].Target != "acme:org/base.json" || lr[0].Fields["sha"] != fake.sha {
		t.Errorf("layer.resolve target/sha wrong: %+v", lr[0])
	}
	if lr[0].Fields["field_count"] != 2 {
		t.Errorf("field_count = %v, want 2", lr[0].Fields["field_count"])
	}

	// config.effective.produced — exactly one terminal event with repo_id.
	ep := rec.byAction(ActionEffectiveProduced)
	if len(ep) != 1 {
		t.Fatalf("effective.produced events = %d, want 1", len(ep))
	}
	if ep[0].Target != "github.com/acme/app" {
		t.Errorf("effective.produced target = %q, want repo_id", ep[0].Target)
	}
	if ep[0].Fields["layer_count"] != len(snap.Layers) {
		t.Errorf("layer_count = %v, want %d", ep[0].Fields["layer_count"], len(snap.Layers))
	}

	// All events in a resolve share one trace id.
	all := rec.snapshot()
	trace := all[0].TraceID
	for _, ev := range all {
		if ev.TraceID != trace {
			t.Errorf("trace id drift: %q vs %q", ev.TraceID, trace)
		}
	}
}

func TestResolveEmitsFieldOverridden(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	// The imported layer and the repo-local layer both set "skills" as a scalar
	// override target — wait, skills is set-union; use a scalar field instead.
	// "features" map-merges; pick a plain scalar key that overrides wholesale.
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"],
		"default_branch": "main"
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"default_branch":"trunk"}`},
		sha:   "abc123",
	}
	rec := &recordingEmitter{}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).WithEmitter(rec).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	fo := rec.byAction(ActionFieldOverridden)
	var found *AuditEvent
	for i := range fo {
		if fo[i].Target == "default_branch" {
			found = &fo[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no field.overridden for default_branch; got %+v", fo)
	}
	if found.Fields["from_layer"] != "acme:org/base.json" || found.Fields["to_layer"] != LayerRepoLocal {
		t.Errorf("override layers wrong: %+v", found.Fields)
	}
	if found.Fields["value_summary"] != "main" {
		t.Errorf("value_summary = %v, want main", found.Fields["value_summary"])
	}
}

func TestResolveEmitsProtectionViolation(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/real",
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{
		"org/base.json": `{"repo_id":"github.com/evil/override","skills":["x"]}`,
	}}
	rec := &recordingEmitter{}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).WithEmitter(rec).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	pv := rec.byAction(ActionFieldProtectionViolation)
	if len(pv) != 1 {
		t.Fatalf("protection_violation events = %d, want 1", len(pv))
	}
	if pv[0].Target != "repo_id" || pv[0].Outcome != OutcomeDropped {
		t.Errorf("protection violation wrong: %+v", pv[0])
	}
	if pv[0].Fields["attempted_by_layer"] != "acme:org/base.json" {
		t.Errorf("attempted_by_layer = %v", pv[0].Fields["attempted_by_layer"])
	}
}

func TestResolveEmitsImportFailedFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/missing.json"]
	}`)
	fake := &fakeFetcher{fetchErr: errors.New("network down")}
	rec := &recordingEmitter{}
	_, err := NewLayeredResolver().WithFetcher("git", fake).WithEmitter(rec).Resolve(repo)
	if err == nil {
		t.Fatal("expected fatal import error")
	}
	fail := rec.byAction(ActionImportFailed)
	if len(fail) != 1 {
		t.Fatalf("import.failed events = %d, want 1", len(fail))
	}
	if fail[0].Target != "acme:org/missing.json" || fail[0].Outcome != OutcomeFailure {
		t.Errorf("import.failed wrong: %+v", fail[0])
	}
	if fail[0].Fields["reason"] != string(ReasonTransport) {
		t.Errorf("reason = %v, want transport", fail[0].Fields["reason"])
	}
	// No effective.produced event when resolution fails fatally.
	if got := rec.byAction(ActionEffectiveProduced); len(got) != 0 {
		t.Errorf("effective.produced emitted on fatal failure: %+v", got)
	}
}

func TestResolveEmitsImportFailedSkippedForOptional(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": [{"ref": "acme:org/missing.json", "optional": true}]
	}`)
	fake := &fakeFetcher{fetchErr: errors.New("network down")}
	rec := &recordingEmitter{}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).WithEmitter(rec).Resolve(repo); err != nil {
		t.Fatalf("optional failure should not be fatal: %v", err)
	}
	fail := rec.byAction(ActionImportFailed)
	if len(fail) != 1 {
		t.Fatalf("import.failed events = %d, want 1", len(fail))
	}
	if fail[0].Outcome != OutcomeSkipped || fail[0].Fields["optional"] != true {
		t.Errorf("optional import.failed wrong: %+v", fail[0])
	}
}

func TestResolveOfflineEmitsCacheHitSourceFetch(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "cache_ttl": "1h"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "feedface000000000000000000000000000000aa",
	}
	// Online resolve to populate the lockfile + cache.
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}
	// Offline resolve: source.fetch must report cache_hit=true with the cached SHA.
	rec := &recordingEmitter{}
	offline := &fakeFetcher{fetchErr: errors.New("network down")}
	if _, err := NewLayeredResolver().WithFetcher("git", offline).WithOffline(true).WithEmitter(rec).Resolve(repo); err != nil {
		t.Fatalf("offline Resolve: %v", err)
	}
	sf := rec.byAction(ActionSourceFetch)
	if len(sf) != 1 {
		t.Fatalf("source.fetch events = %d, want 1", len(sf))
	}
	if sf[0].Fields["cache_hit"] != true {
		t.Errorf("offline cache_hit = %v, want true", sf[0].Fields["cache_hit"])
	}
	if sf[0].Fields["resolved_sha"] != fake.sha {
		t.Errorf("offline resolved_sha = %v, want %q", sf[0].Fields["resolved_sha"], fake.sha)
	}
}

func TestResolveDefaultEmitterIsNoop(t *testing.T) {
	// A LayeredResolver with no WithEmitter must resolve identically — the
	// no-op fallback means resolution behavior is unchanged.
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{"org/base.json": `{"skills":["x"]}`}}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := activeValue(findProvenance(snap, "skills")); len(got.([]any)) != 1 {
		t.Errorf("skills = %v, want one element", got)
	}
}
