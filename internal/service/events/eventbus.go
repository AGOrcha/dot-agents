package events

import "errors"

// ErrClosed is returned by Publish and Subscribe after an EventBus has been
// closed. Backends must return an error satisfying errors.Is(err, ErrClosed)
// for post-Close operations so callers can detect shutdown uniformly.
var ErrClosed = errors.New("events: bus closed")

// BuiltinBackendRef is the adapter reference of the in-process builtin
// backend (spec D4.3). It is the default when no external event_bus adapter
// is configured; external adapters (D4.4) are post-v1 and demand-gated.
const BuiltinBackendRef = "dotagents-builtin:eventbus/inproc@^1.0"

// EventBus is the transport seam for service events (spec D4.1). Publishers
// and subscribers — the scheduler tasks, the transport layer, R2's SSE
// fan-out, R5's collection endpoint — bind to this interface, never to the
// concrete channel implementation, so an external backend (Kafka, NATS,
// Redis) can later be swapped in as a config-selected adapter without a
// rewrite of any publisher or subscriber.
//
// The interface promises ONLY the D4.2 floor; callers MUST NOT assume any
// stronger guarantee a particular backend happens to provide:
//
//   - G1 — at-most-once, best-effort delivery. Events are "wake up and look
//     at the new state on disk"; a missed event is recovered by re-reading
//     the canonical sidecar, never from the bus.
//   - G2 — per-topic ordering is best-effort, not guaranteed. Code that
//     needs strict order re-derives it from sidecar state.
//   - G3 — backpressure is a bounded buffer with drop-oldest; publishers
//     never block on a slow subscriber.
//   - G4 — no cross-publish atomicity; every Publish is an independent
//     fire-and-forget.
//
// Backends are free to be stronger; the floor is what app code may assume.
// Conformance is proven mechanically by the eventbustest suite (spec D4.6).
type EventBus interface {
	// Publish delivers payload to all current subscribers of topic.
	// Non-blocking for the publisher (G3); MAY drop events for a slow
	// subscriber per the bounded-buffer / drop-oldest policy. Publishing
	// on a topic with no subscribers succeeds. Returns an error
	// satisfying errors.Is(err, ErrClosed) after Close.
	Publish(topic string, payload any) error
	// Subscribe returns a receive-only stream for topic plus an
	// unsubscribe func. The buffer is bounded; drop-oldest on overflow.
	// The unsubscribe func closes the stream and is safe to call more
	// than once. Returns an error satisfying errors.Is(err, ErrClosed)
	// after Close.
	Subscribe(topic string) (<-chan Event, func(), error)
	// Close drains and releases backend resources, closing every
	// subscriber stream. Close is idempotent.
	Close() error
}
