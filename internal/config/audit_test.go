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

// --- H12 central error redaction --------------------------------------------

// TestRedactSecretsScrubsRegisteredSecret plants a sentinel secret via
// registerSecret and asserts redactSecrets removes every occurrence from an
// arbitrary string, leaving non-secret text untouched.
func TestRedactSecretsScrubsRegisteredSecret(t *testing.T) {
	sentinel := "sentinel-secret-tHiS-iS-tOp-sEcReT-9f8e7d"
	registerSecret(sentinel)

	in := "request failed: token=" + sentinel + " (status 401)"
	got := redactSecrets(in)
	if strings.Contains(got, sentinel) {
		t.Fatalf("redactSecrets left the sentinel in place: %q", got)
	}
	want := "request failed: token=[REDACTED] (status 401)"
	if got != want {
		t.Errorf("redactSecrets = %q, want %q", got, want)
	}

	// Text with no registered secret is returned unchanged.
	if got := redactSecrets("nothing sensitive here"); got != "nothing sensitive here" {
		t.Errorf("redactSecrets mutated a clean string: %q", got)
	}
}

// TestRedactSecretsLongestFirstNoFragment is the HIGH-#2 regression: a
// shorter secret registered BEFORE a longer one that contains it must not
// fragment the longer one. Registering "abc" then "abcdef" and redacting a
// string containing "abcdef" must leave NO fragment of "abcdef" behind —
// order-dependent replacement would have produced "[REDACTED]def", leaking
// the "def" tail of the real credential.
func TestRedactSecretsLongestFirstNoFragment(t *testing.T) {
	short := "sfrag-abc-" + "u1"
	long := short + "-longer-tail-secret"
	registerSecret(short) // shorter, registered first
	registerSecret(long)  // longer, contains short as a prefix

	got := redactSecrets("prefix " + long + " suffix")
	if strings.Contains(got, "longer-tail-secret") {
		t.Fatalf("redactSecrets left a fragment of the longer secret: %q", got)
	}
	if strings.Contains(got, short) {
		t.Fatalf("redactSecrets left the shorter secret verbatim: %q", got)
	}
	if got != "prefix [REDACTED] suffix" {
		t.Errorf("redactSecrets = %q, want %q", got, "prefix [REDACTED] suffix")
	}
}

// TestRedactSecretsEqualLengthOverlapNoFragment is the round-3 HIGH-#2
// regression: descending-length sort (the round-2 fix) closes strict
// CONTAINMENT but not EQUAL-LENGTH partial overlap, where neither secret
// contains the other but their occurrences share characters. Registering
// "sekrit-AAAA-1" and "AAAA-1-sekrit" (both 13 chars, live repro) and
// redacting "sekrit-AAAA-1-sekrit" must leave no fragment of either secret —
// the sequential-ReplaceAll implementation produced "[REDACTED]-sekrit",
// leaking the 7-char "-sekrit" tail verbatim. The strict-containment case
// from the round-2 test is re-asserted alongside it so both overlap shapes
// stay covered by the single merged-interval implementation.
func TestRedactSecretsEqualLengthOverlapNoFragment(t *testing.T) {
	secretA := "sekrit-AAAA-1"
	secretB := "AAAA-1-sekrit"
	if len(secretA) != len(secretB) {
		t.Fatalf("test fixture broken: secrets must be equal length, got %d and %d", len(secretA), len(secretB))
	}
	registerSecret(secretA)
	registerSecret(secretB)

	got := redactSecrets("sekrit-AAAA-1-sekrit")
	for _, frag := range []string{secretA, secretB, "-sekrit", "sekrit-", "AAAA-1"} {
		if strings.Contains(got, frag) {
			t.Fatalf("redactSecrets left a fragment %q of an overlapping secret: %q", frag, got)
		}
	}
	if got != "[REDACTED]" {
		t.Errorf("redactSecrets = %q, want %q", got, "[REDACTED]")
	}

	// Strict containment (round-2 case) stays closed under the same
	// merged-interval implementation.
	short := "sfrag-eqlen-abc-u1"
	long := short + "-longer-tail-secret"
	registerSecret(short)
	registerSecret(long)
	got = redactSecrets("prefix " + long + " suffix")
	if strings.Contains(got, "longer-tail-secret") || strings.Contains(got, short) {
		t.Fatalf("redactSecrets left a fragment of the containment case: %q", got)
	}
	if got != "prefix [REDACTED] suffix" {
		t.Errorf("redactSecrets = %q, want %q", got, "prefix [REDACTED] suffix")
	}
}

// TestRegisterSecretDeduplicates confirms registering the same secret value
// twice does not grow the registry (bounding its process-lifetime size when
// a credential is re-resolved across repeated fetches).
func TestRegisterSecretDeduplicates(t *testing.T) {
	sentinel := "sentinel-dedupe-check-0a1b2c"
	registerSecret(sentinel)
	before := len(registeredSecrets)
	registerSecret(sentinel)
	if len(registeredSecrets) != before {
		t.Fatalf("registerSecret grew the registry on a duplicate: %d -> %d", before, len(registeredSecrets))
	}
}

// TestRedactSecretsEmptyStringNoOp confirms registerSecret("") never
// registers a wildcard that would scrub every string to empty.
func TestRedactSecretsEmptyStringNoOp(t *testing.T) {
	before := len(registeredSecrets)
	registerSecret("")
	if len(registeredSecrets) != before {
		t.Fatalf("registerSecret(\"\") grew the registry: %d -> %d", before, len(registeredSecrets))
	}
	if got := redactSecrets("some text"); got != "some text" {
		t.Errorf("redactSecrets corrupted unrelated text: %q", got)
	}
}

// TestNewRedactedErrorScrubsErrorString is the HIGH-#3 error-boundary test:
// a cause whose message embeds a registered secret must render scrubbed via
// the wrapper's Error().
func TestNewRedactedErrorScrubsErrorString(t *testing.T) {
	sentinel := "sentinel-boundary-err-1c2d3e"
	registerSecret(sentinel)

	cause := &ImportError{Ref: "acme:x@1", Reason: ReasonTransport, Err: errors.New("sent Bearer " + sentinel)}
	wrapped := newRedactedError(cause)
	if strings.Contains(wrapped.Error(), sentinel) {
		t.Fatalf("redactedError leaked the sentinel in its rendered string: %q", wrapped.Error())
	}
	if !strings.Contains(wrapped.Error(), "[REDACTED]") {
		t.Errorf("redactedError did not redact the sentinel: %q", wrapped.Error())
	}
	// A nil cause wraps to nil so callers can wrap unconditionally.
	if newRedactedError(nil) != nil {
		t.Error("newRedactedError(nil) should be nil")
	}
}

// TestNewRedactedErrorUnwrapCannotRecoverSecret is the round-3 MEDIUM
// regression: redactedError must not expose the raw, unredacted cause via
// errors.Unwrap/errors.As. errors.As previously reached the underlying
// *ImportError, and calling ITS .Error() bypassed redaction entirely — the
// exact leak this test proves is now closed: nothing reachable by walking
// the wrapped error's chain (errors.Unwrap, errors.As) ever renders the raw
// secret.
func TestNewRedactedErrorUnwrapCannotRecoverSecret(t *testing.T) {
	sentinel := "sentinel-unwrap-leak-4d5e6f"
	registerSecret(sentinel)

	cause := &ImportError{Ref: "acme:x@1", Reason: ReasonTransport, Err: errors.New("sent Bearer " + sentinel)}
	wrapped := newRedactedError(cause)

	// errors.Unwrap must not hand back a value at all: redactedError
	// implements neither Unwrap() error nor Unwrap() []error.
	if u := errors.Unwrap(wrapped); u != nil {
		t.Fatalf("errors.Unwrap(wrapped) = %v, want nil (Unwrap must not be implemented)", u)
	}

	// errors.As must not be able to recover the concrete *ImportError cause
	// (which would let the caller call ie.Error() directly, bypassing
	// redaction and leaking the sentinel verbatim).
	var ie *ImportError
	if errors.As(wrapped, &ie) {
		t.Fatalf("errors.As reached the raw cause %v; its Error() (%q) is not redaction-scrubbed", ie, ie.Error())
	}

	// errors.Is against the wrapped cause's sentinel still works (Is is
	// implemented deliberately, unlike Unwrap/As) without exposing it.
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is(wrapped, cause) = false, want true (Is passthrough should still work)")
	}

	// Belt-and-suspenders: no reachable value in the chain renders the raw
	// sentinel when .Error() is called on it.
	if strings.Contains(wrapped.Error(), sentinel) {
		t.Fatalf("wrapped.Error() leaked the sentinel: %q", wrapped.Error())
	}
}

// TestImportFailedEventRedactsSentinelInDetail is the H12 sentinel-leak test
// named directly by the spec: audit.go's importFailedEvent used to log
// ie.Err.Error() raw. A sentinel secret embedded in a wrapped error (as a
// naive implementation elsewhere in the auth path might do) must never reach
// the emitted audit event's Fields["detail"].
func TestImportFailedEventRedactsSentinelInDetail(t *testing.T) {
	sentinel := "sentinel-audit-leak-4b2c1a"
	registerSecret(sentinel)

	ie := &ImportError{
		Ref:      "acme-pkgs:skill/x@1.0",
		SourceID: "acme-pkgs",
		Reason:   ReasonAuth,
		Err:      errors.New("oci token exchange failed for token " + sentinel),
	}
	ev := importFailedEvent(ie, false)
	detail, _ := ev.Fields["detail"].(string)
	if strings.Contains(detail, sentinel) {
		t.Fatalf("importFailedEvent leaked the sentinel into the audit detail field: %q", detail)
	}
	if !strings.Contains(detail, "[REDACTED]") {
		t.Errorf("importFailedEvent did not redact the sentinel: %q", detail)
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
