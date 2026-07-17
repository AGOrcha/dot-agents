package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Audit event taxonomy for config-tier resolution (config-distribution-model
// §9, §11). Every event the resolver emits during a Resolve carries the base
// schema { Timestamp, Actor, Principal, Action, Target, Outcome, TraceID } plus
// an action-specific Fields map. The emission seam (AuditEmitter) is injected so
// resolution stays observable without coupling the config package to any
// concrete sink; the default is a no-op so resolution is unchanged when no sink
// is wired (e.g. FlatResolver, or a LayeredResolver constructed without
// WithEmitter).

// Config-tier audit action identifiers. These are the canonical `action`
// strings of the base event schema, matching the taxonomy table in the spec.
const (
	// ActionSourceFetch fires once per layer fetch attempt that contacts (or
	// would have contacted) a source. Target is the source id; Fields carry
	// resolved_sha and cache_hit.
	ActionSourceFetch = "config.source.fetch"
	// ActionLayerResolve fires once per imported layer successfully resolved
	// and validated. Target is the "source_id:layer_path" ref; Fields carry
	// field_count and sha.
	ActionLayerResolve = "config.layer.resolve"
	// ActionFieldOverridden fires once per effective field that more than one
	// layer set (the higher-precedence layer overrides the lower). Target is the
	// field path; Fields carry from_layer, to_layer, value_summary.
	ActionFieldOverridden = "config.field.overridden"
	// ActionFieldProtectionViolation fires when a lower-precedence (imported /
	// user-local) layer attempts to set a repo-protected field and is dropped.
	// Target is the field path; Fields carry attempted_by_layer; Outcome=dropped.
	ActionFieldProtectionViolation = "config.field.protection_violation"
	// ActionImportFailed fires when an extends import cannot be resolved. Target
	// is the failing ref; Fields carry reason (transport|auth|content|schema|
	// not_found) and whether the entry was optional (and thus skipped).
	ActionImportFailed = "config.import.failed"
	// ActionEffectiveProduced fires once at the end of a successful resolve.
	// Target is the repo_id (or "" when unset); Fields carry layer_count.
	ActionEffectiveProduced = "config.effective.produced"
)

// Audit outcome values used in the base event schema.
const (
	// OutcomeSuccess marks an event for an operation that completed normally.
	OutcomeSuccess = "success"
	// OutcomeDropped marks a protected-field override that was discarded.
	OutcomeDropped = "dropped"
	// OutcomeFailure marks an import that failed (non-optional).
	OutcomeFailure = "failure"
	// OutcomeSkipped marks an optional import that failed and was skipped.
	OutcomeSkipped = "skipped"
)

// auditActor is the constant actor string for config-tier resolution events:
// the component that produced the event, not the human principal.
const auditActor = "config-resolver"

// AuditEvent is a single config-tier audit record. It is the Go form of the
// base event schema shared by every config.* action (spec §9). Fields holds the
// action-specific structured attributes named in the taxonomy table.
type AuditEvent struct {
	// Timestamp is when the event was produced (UTC).
	Timestamp time.Time `json:"timestamp"`
	// Actor is the emitting component (always auditActor for this package).
	Actor string `json:"actor"`
	// Principal is the human/service identity on whose behalf resolution ran,
	// or "" when not attributed.
	Principal string `json:"principal,omitempty"`
	// Action is one of the config.* action constants.
	Action string `json:"action"`
	// Target is the subject of the action (source id, layer ref, field path, or
	// repo id depending on Action).
	Target string `json:"target"`
	// Outcome is the disposition (success, dropped, failure, skipped).
	Outcome string `json:"outcome"`
	// TraceID correlates every event emitted within a single Resolve call.
	TraceID string `json:"trace_id"`
	// Fields holds the action-specific structured attributes.
	Fields map[string]any `json:"fields,omitempty"`
}

// AuditEmitter receives config-tier audit events. It is the injectable seam the
// LayeredResolver emits through (WithEmitter). Implementations must be safe for
// concurrent use: the resolver fetches extends layers in parallel, so Emit may
// be called from multiple goroutines. The default emitter is noopEmitter.
type AuditEmitter interface {
	// Emit records one audit event. Implementations must not block resolution.
	Emit(AuditEvent)
}

// noopEmitter discards every event. It is the default so resolution behavior is
// unchanged when no sink is wired.
type noopEmitter struct{}

// Emit discards the event.
func (noopEmitter) Emit(AuditEvent) {
	// Intentionally empty: noopEmitter is the default sink used when no audit
	// emitter is wired, so resolution behavior is unchanged and events are
	// silently dropped.
}

// NoopEmitter returns the shared no-op AuditEmitter. Callers that want auditing
// off use it explicitly; the resolver also falls back to it when none is set.
func NoopEmitter() AuditEmitter { return noopEmitter{} }

// auditTrace carries the per-Resolve correlation context: the emitter to send
// to and the trace id every event in this resolve shares. A zero auditTrace
// (emitter nil) emits nowhere, so emission call sites need no nil-guard of their
// own — newAuditTrace always returns a usable trace.
type auditTrace struct {
	emitter AuditEmitter
	traceID string
}

// newAuditTrace builds a trace for one Resolve call, normalizing a nil emitter
// to the no-op sink and minting a fresh trace id.
func newAuditTrace(emitter AuditEmitter) auditTrace {
	if emitter == nil {
		emitter = noopEmitter{}
	}
	return auditTrace{emitter: emitter, traceID: newTraceID()}
}

// emit stamps the shared base fields (timestamp, actor, trace id) onto evt and
// forwards it to the emitter. Action/Target/Outcome/Fields are set by the
// caller via the event constructors below.
func (t auditTrace) emit(evt AuditEvent) {
	evt.Timestamp = time.Now().UTC()
	evt.Actor = auditActor
	evt.TraceID = t.traceID
	t.emitter.Emit(evt)
}

// newTraceID returns a random 128-bit hex trace id. On the (practically
// impossible) failure of the system RNG it falls back to a timestamp-derived id
// so a resolve never fails for want of a trace id.
func newTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// sourceFetchEvent builds a config.source.fetch event for a layer fetch.
func sourceFetchEvent(sourceID, resolvedSHA string, cacheHit bool) AuditEvent {
	return AuditEvent{
		Action:  ActionSourceFetch,
		Target:  sourceID,
		Outcome: OutcomeSuccess,
		Fields: map[string]any{
			"resolved_sha": resolvedSHA,
			"cache_hit":    cacheHit,
		},
	}
}

// layerResolveEvent builds a config.layer.resolve event for a validated layer.
func layerResolveEvent(ref, sha string, fieldCount int) AuditEvent {
	return AuditEvent{
		Action:  ActionLayerResolve,
		Target:  ref,
		Outcome: OutcomeSuccess,
		Fields: map[string]any{
			"field_count": fieldCount,
			"sha":         sha,
		},
	}
}

// fieldOverriddenEvent builds a config.field.overridden event recording that
// toLayer's value for fieldPath superseded fromLayer's.
func fieldOverriddenEvent(fieldPath, fromLayer, toLayer string, value any) AuditEvent {
	return AuditEvent{
		Action:  ActionFieldOverridden,
		Target:  fieldPath,
		Outcome: OutcomeSuccess,
		Fields: map[string]any{
			"from_layer":    fromLayer,
			"to_layer":      toLayer,
			"value_summary": summarizeValue(value),
		},
	}
}

// protectionViolationEvent builds a config.field.protection_violation event for
// a dropped protected-field override.
func protectionViolationEvent(fieldPath, attemptedByLayer string) AuditEvent {
	return AuditEvent{
		Action:  ActionFieldProtectionViolation,
		Target:  fieldPath,
		Outcome: OutcomeDropped,
		Fields: map[string]any{
			"attempted_by_layer": attemptedByLayer,
		},
	}
}

// importFailedEvent builds a config.import.failed event from an ImportError.
// optional reports whether the entry was optional (skipped) vs. fatal (failure).
func importFailedEvent(ie *ImportError, optional bool) AuditEvent {
	outcome := OutcomeFailure
	if optional {
		outcome = OutcomeSkipped
	}
	fields := map[string]any{
		"reason":   string(ie.Reason),
		"optional": optional,
	}
	if ie.SourceID != "" {
		fields["source_id"] = ie.SourceID
	}
	if ie.Err != nil {
		fields["detail"] = redactSecrets(ie.Err.Error())
	}
	return AuditEvent{
		Action:  ActionImportFailed,
		Target:  ie.Ref,
		Outcome: outcome,
		Fields:  fields,
	}
}

// effectiveProducedEvent builds the terminal config.effective.produced event.
func effectiveProducedEvent(repoID string, layerCount int) AuditEvent {
	return AuditEvent{
		Action:  ActionEffectiveProduced,
		Target:  repoID,
		Outcome: OutcomeSuccess,
		Fields: map[string]any{
			"layer_count": layerCount,
		},
	}
}

// summarizeValue produces a compact, bounded value_summary for an overridden
// field so the event stays small even when the field is a large array/object.
// Scalars render directly; composites render as a type+length descriptor rather
// than their full contents.
func summarizeValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return truncate(t, 64)
	case bool:
		return fmt.Sprintf("%t", t)
	case float64:
		return fmt.Sprintf("%v", t)
	case []any:
		return fmt.Sprintf("[array len=%d]", len(t))
	case map[string]any:
		return fmt.Sprintf("{object keys=%d}", len(t))
	default:
		return truncate(fmt.Sprintf("%v", t), 64)
	}
}

// truncate caps s at max runes, appending an ellipsis marker when it was cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// --- H12 central error redaction --------------------------------------------
//
// registerSecret / redactSecrets are the config package's central credential-
// non-disclosure backstop (package-artifact-install spec H12). The OCI auth
// providers (oci_auth.go) register every resolved secret value the instant
// they resolve it; every surface that formats an error string for a human or
// an audit sink — importFailedEvent above — routes the text through
// redactSecrets first. This is defense in depth: the auth providers are ALSO
// written to never embed a resolved secret in an error message in the first
// place, so redactSecrets is the safety net for a path this package did not
// anticipate, not the primary control.

var (
	secretRedactorMu  sync.Mutex
	registeredSecrets []string
)

// registerSecret records secret as material that must never appear verbatim
// in a subsequently-formatted error, audit, or log string for the remainder
// of this process. It is a no-op for an empty string so a caller can
// register unconditionally right after a credential resolution without a
// blank guard. A `da` invocation is a single short-lived process, so the
// registry is intentionally never cleared: a secret must stay non-
// disclosable for the rest of the run, not just the call that resolved it.
func registerSecret(secret string) {
	if secret == "" {
		return
	}
	secretRedactorMu.Lock()
	defer secretRedactorMu.Unlock()
	for _, s := range registeredSecrets {
		if s == secret {
			return
		}
	}
	registeredSecrets = append(registeredSecrets, secret)
}

// redactSecrets replaces every registered secret substring in s with a fixed
// marker. Safe to call on any string, including one with no registered
// secrets present (returned unchanged).
//
// Matching is single-pass and interval-merged (Codex round-2 HIGH, hardened
// round-3 HIGH — equal-length partial overlap). The original round-2 fix
// sorted secrets longest-first and ran a sequential ReplaceAll per secret;
// that closes strict CONTAINMENT (a short secret that is a substring of a
// longer one) but not EQUAL-LENGTH partial overlap, where neither secret
// contains the other but their occurrences share characters — sort.Slice has
// no equal-length tiebreak, and whichever secret's ReplaceAll pass runs
// first consumes its span, leaving the other secret's non-overlapping tail
// exposed. Concretely: registering "sekrit-AAAA-1" and "AAAA-1-sekrit" (both
// 13 chars) and redacting "sekrit-AAAA-1-sekrit" produced
// "[REDACTED]-sekrit" — a verbatim 7-char fragment of the second secret.
//
// The fix finds every match span for every registered secret against the
// ORIGINAL (unmodified) string, merges overlapping/adjacent spans, and
// replaces each merged span exactly once. This closes containment,
// equal-length overlap, and any other overlap shape uniformly, because
// merging (not sequential substitution order) decides what gets redacted.
func redactSecrets(s string) string {
	secretRedactorMu.Lock()
	secrets := make([]string, len(registeredSecrets))
	copy(secrets, registeredSecrets)
	secretRedactorMu.Unlock()
	if len(secrets) == 0 {
		return s
	}

	spans := collectSecretSpans(s, secrets)
	if len(spans) == 0 {
		return s
	}
	merged := mergeSecretSpans(spans)

	var b strings.Builder
	b.Grow(len(s))
	prev := 0
	for _, m := range merged {
		b.WriteString(s[prev:m.start])
		b.WriteString("[REDACTED]")
		prev = m.end
	}
	b.WriteString(s[prev:])
	return b.String()
}

// secretSpan is a [start,end) byte range within a string that matches a
// registered secret and must be redacted.
type secretSpan struct{ start, end int }

// collectSecretSpans returns every match span of every registered secret
// against the ORIGINAL string s. It advances by one rune of progress (not
// len(secret)) so overlapping occurrences of the SAME secret are also found,
// matching the exhaustive-match requirement the merge strategy depends on.
func collectSecretSpans(s string, secrets []string) []secretSpan {
	var spans []secretSpan
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for from := 0; from <= len(s)-len(secret); {
			idx := strings.Index(s[from:], secret)
			if idx < 0 {
				break
			}
			start := from + idx
			spans = append(spans, secretSpan{start: start, end: start + len(secret)})
			from = start + 1
		}
	}
	return spans
}

// mergeSecretSpans sorts spans by start (end as tiebreak) and coalesces every
// overlapping or adjacent span so each redacted region is emitted exactly once
// regardless of substitution order.
func mergeSecretSpans(spans []secretSpan) []secretSpan {
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	merged := make([]secretSpan, 0, len(spans))
	merged = append(merged, spans[0])
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start <= last.end {
			if sp.end > last.end {
				last.end = sp.end
			}
			continue
		}
		merged = append(merged, sp)
	}
	return merged
}

// redactedError wraps a cause so its rendered message is scrubbed of every
// registered secret — the H12 error-BOUNDARY control (Codex round-2 HIGH
// #3, hardened round-3 MEDIUM — Unwrap exposed the raw cause). redactSecrets
// was previously wired only into the audit event's copied detail field, so
// the error VALUE returned to callers (printed to stderr, logged) was never
// scrubbed. Wrapping a cause in a redactedError before it is placed on an
// ImportError.Err means every consumer of the resulting error —
// ImportError.Error(), a CLI, a log line — sees the redacted text, not just
// the audit detail.
//
// The redacted rendering is computed ONCE, at construction, and stored: msg
// is what Error() always returns. Unwrap is deliberately NOT implemented —
// exposing the raw cause via errors.Unwrap/errors.As would let a caller walk
// past redactedError to the concrete cause and call ITS .Error() directly,
// recovering the unredacted secret (a caller stopping only at *ImportError
// today is safe by convention, not by construction — this closes that gap
// so it holds even once a real caller unwraps further, e.g. once t8's live
// puller is wired). Is IS implemented so errors.Is sentinel matching
// (os.ErrNotExist, context.DeadlineExceeded, etc.) against the wrapped cause
// keeps working: Is only returns a bool, so unlike As/Unwrap it cannot itself
// hand the caller an object whose Error() leaks the raw cause.
type redactedError struct {
	msg   string
	cause error
}

// newRedactedError wraps err, computing its redacted rendering immediately
// so the returned value's Error() output can never surface a secret
// regardless of what a caller later does with it. A nil cause stays nil so
// callers can wrap unconditionally.
func newRedactedError(err error) error {
	if err == nil {
		return nil
	}
	return &redactedError{msg: redactSecrets(err.Error()), cause: err}
}

// Error returns the pre-redacted rendering computed at construction.
func (e *redactedError) Error() string { return e.msg }

// Is delegates to errors.Is against the wrapped cause so sentinel-error
// matching keeps working without exposing the cause itself (see the type
// doc for why Unwrap/As are intentionally absent).
func (e *redactedError) Is(target error) bool { return errors.Is(e.cause, target) }
