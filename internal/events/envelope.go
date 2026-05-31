// Package events implements a unified event-contract core: a typed Envelope,
// a runtime Kind registry, table-driven dispatch, a generic config-driven
// producer engine, and a dependency-free JSONPath subset.
//
// The design is deliberately schema-additive: event kinds are registered at
// runtime (mirroring verifier_profiles) rather than enumerated in Go, so new
// event types can be added without touching this package.
package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// errEmpty is the shared sentinel for missing required fields so callers can
// match with errors.Is and so the message stays consistent across validations.
var errEmpty = errors.New("events: required field is empty")

// Envelope is the canonical wire shape for every event flowing through the
// system. Payload is left as raw JSON so the core never needs to know the
// concrete shape of any particular kind.
type Envelope struct {
	Type           string          `json:"type"`
	Source         string          `json:"source"`
	OccurredAt     time.Time       `json:"occurred_at"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}

// NewEnvelope constructs an Envelope and validates it. OccurredAt defaults to
// the current time when the caller passes the zero value.
func NewEnvelope(typ, source, idempotencyKey string, occurredAt time.Time, payload json.RawMessage) (Envelope, error) {
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	env := Envelope{
		Type:           typ,
		Source:         source,
		OccurredAt:     occurredAt,
		IdempotencyKey: idempotencyKey,
		Payload:        payload,
	}
	if err := env.Validate(); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Validate enforces the invariants required of every Envelope. It is cheap and
// has no side effects so it can be called freely at construction and emit time.
func (e Envelope) Validate() error {
	if err := requireField("type", e.Type); err != nil {
		return err
	}
	if err := requireField("source", e.Source); err != nil {
		return err
	}
	return requireField("idempotency_key", e.IdempotencyKey)
}

// Namespace returns the dotted prefix of the event type up to (but excluding)
// the final segment — e.g. "event.pr.merged" -> "event.pr". When the type has
// no separating dot the whole type is treated as the namespace.
func (e Envelope) Namespace() string {
	return namespaceOf(e.Type)
}

func requireField(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s", errEmpty, name)
	}
	return nil
}

func namespaceOf(typ string) string {
	idx := strings.LastIndex(typ, ".")
	if idx <= 0 {
		return typ
	}
	return typ[:idx]
}
