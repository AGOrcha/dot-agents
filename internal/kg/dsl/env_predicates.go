package dsl

import (
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// EnvPredicate is a declared environmental-driver predicate on a note type
// (§7.1, §7.2). When an external trigger fires, matching notes are tagged
// `stale: { reason: "environmental" }`. v1 supports the time_after and webhook
// kinds; module_version and custom are declared-but-deferred.
type EnvPredicate struct {
	// NoteType is the note type the predicate is declared on.
	NoteType string
	// Kind is one of time_after | webhook | module_version | custom (§7.2).
	Kind string
	// Field is the date field a time_after predicate observes.
	Field string
	// Endpoint is the webhook predicate's endpoint name.
	Endpoint string
}

// Predicate kind constants (§7.2).
const (
	KindTimeAfter = "time_after"
	KindWebhook   = "webhook"
)

// EnvTrigger is one fired environmental observation: a clock crossing
// (time_after) or a webhook POST. It is the input to ApplyEnvTrigger, which
// tags matching notes stale.
type EnvTrigger struct {
	// Kind selects which predicates fire (time_after | webhook).
	Kind string
	// Now is the clock observation a time_after trigger compares against. A
	// note is tagged when its predicate field is a date <= Now.
	Now time.Time
	// Endpoint is the webhook endpoint that fired (webhook kind). All notes
	// whose webhook predicate names this endpoint are tagged.
	Endpoint string
	// Targets optionally restricts the fire to specific note ids. When empty,
	// every note whose predicate matches the trigger is tagged; when set, only
	// the listed notes are tagged (a webhook fired for one policy, say). This
	// models the §8.4.1 declare_predicate_fired arg surface where a receiver
	// fires for a specific resource.
	Targets []string
	// TriggerID is recorded in the stale tag's `because` provenance list.
	TriggerID string
}

// targets reports whether a note id is in the trigger's target set. An empty
// target set matches every note.
func (t EnvTrigger) matchesTarget(id string) bool {
	if len(t.Targets) == 0 {
		return true
	}
	for _, want := range t.Targets {
		if want == id {
			return true
		}
	}
	return false
}

// ApplyEnvTrigger evaluates a fired trigger against the declared predicates and
// returns the notes that should be tagged stale, each carrying the §7.3
// structured stale payload `{ reason: "environmental", because, fired_at }`.
// It does not mutate the store — the caller (a bootstrap skill via the SDK, or
// the scoped-KG driver bus in production) persists the tagged notes. This keeps
// the env-predicate logic a pure function the conformance tests pin directly.
func ApplyEnvTrigger(preds []EnvPredicate, notes []sdk.Note, trig EnvTrigger) ([]sdk.Note, error) {
	matching, err := predicatesForKind(preds, trig.Kind)
	if err != nil {
		return nil, err
	}
	var out []sdk.Note
	for _, n := range notes {
		pred, ok := matchPredicate(matching, n)
		if !ok || !trig.matchesTarget(n.ID) {
			continue
		}
		if fires(pred, n, trig) {
			out = append(out, tagStale(n, trig))
		}
	}
	return out, nil
}

// predicatesForKind filters declared predicates to the trigger's kind, erroring
// on an unsupported kind so a typo fails loud at fire time.
func predicatesForKind(preds []EnvPredicate, kind string) ([]EnvPredicate, error) {
	if kind != KindTimeAfter && kind != KindWebhook {
		return nil, fmt.Errorf("dsl: unsupported env trigger kind %q (v1 supports %s, %s)", kind, KindTimeAfter, KindWebhook)
	}
	var out []EnvPredicate
	for _, p := range preds {
		if p.Kind == kind {
			out = append(out, p)
		}
	}
	return out, nil
}

// matchPredicate finds the predicate declared on a note's type, if any.
func matchPredicate(preds []EnvPredicate, n sdk.Note) (EnvPredicate, bool) {
	for _, p := range preds {
		if p.NoteType == n.Type {
			return p, true
		}
	}
	return EnvPredicate{}, false
}

// fires reports whether a note satisfies the predicate under the trigger. A
// time_after predicate fires when the note's field date is at or before the
// observation clock; a webhook predicate fires when the trigger's endpoint
// matches the predicate's endpoint.
func fires(pred EnvPredicate, n sdk.Note, trig EnvTrigger) bool {
	switch pred.Kind {
	case KindTimeAfter:
		return timeAfterFires(pred, n, trig.Now)
	case KindWebhook:
		return pred.Endpoint == trig.Endpoint
	default:
		return false
	}
}

// timeAfterFires reports whether the note's predicate date field is at or before
// the clock observation (the date has been crossed, §7.2).
func timeAfterFires(pred EnvPredicate, n sdk.Note, now time.Time) bool {
	raw, ok := fieldValue(&n, pred.Field)
	if !ok {
		return false
	}
	s, ok := raw.(string)
	if !ok {
		return false
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return false
	}
	return !d.After(now)
}

// tagStale returns a copy of n with the §7.3 environmental stale payload set.
func tagStale(n sdk.Note, trig EnvTrigger) sdk.Note {
	fields := make(map[string]any, len(n.Fields)+1)
	for k, v := range n.Fields {
		fields[k] = v
	}
	fields[staleKey] = map[string]any{
		"reason":   "environmental",
		"because":  []any{trig.TriggerID},
		"fired_at": trig.Now.Format(time.RFC3339),
	}
	return sdk.Note{ID: n.ID, Type: n.Type, Fields: fields}
}
