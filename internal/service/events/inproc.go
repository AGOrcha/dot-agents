package events

import "sync"

// InProcBus adapts the channel-based Bus into the builtin EventBus backend
// (spec D4.3, dotagents-builtin:eventbus/inproc). It is the default — and
// in v1 the only — backend: bounded buffer per subscriber, drop-oldest on
// slow consumers, no durable storage, no cross-process IPC.
//
// The adapter adds the error-reporting shape the D4.1 seam requires on top
// of the concrete Bus: after Close, Publish and Subscribe return ErrClosed
// instead of silently no-opping.
type InProcBus struct {
	mu     sync.Mutex
	bus    *Bus
	closed bool
}

// Compile-time proof that the builtin satisfies the D4.1 seam.
var _ EventBus = (*InProcBus)(nil)

// NewInProcBus returns a ready-to-use in-process builtin backend.
func NewInProcBus() *InProcBus {
	return &InProcBus{bus: NewBus()}
}

// Publish implements EventBus. Delivery semantics are those of Bus.Publish:
// non-blocking, drop-oldest per slow subscriber, no-subscriber publishes
// succeed. Returns ErrClosed after Close.
func (p *InProcBus) Publish(topic string, payload any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	p.bus.Publish(topic, payload)
	return nil
}

// Subscribe implements EventBus. The stream uses the package default buffer
// capacity; the unsubscribe func closes the stream and is idempotent.
// Returns ErrClosed after Close.
func (p *InProcBus) Subscribe(topic string) (<-chan Event, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, nil, ErrClosed
	}
	ch, unsubscribe := p.bus.Subscribe(topic)
	return ch, unsubscribe, nil
}

// Close implements EventBus: closes every subscriber stream and marks the
// backend closed. Idempotent; always returns nil.
func (p *InProcBus) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.bus.Close()
	return nil
}

// Dropped reports the total number of events discarded across current
// subscribers due to drop-oldest back-pressure (observability passthrough
// to Bus.Dropped; not part of the EventBus contract).
func (p *InProcBus) Dropped() int64 {
	return p.bus.Dropped()
}
