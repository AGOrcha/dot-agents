// Package events provides an in-process publish/subscribe bus that lets
// background tasks notify subscribers about state changes without coupling
// to them. Events are ephemeral: consumers that miss an event are expected
// to re-read canonical state from disk sidecars. There is no durable
// storage and no cross-process IPC (see R3 design decision D4).
//
// The cross-plan contract that R2's sse-broker and R5's collection endpoint
// bind to is the EventBus interface (spec D4.1) — never this concrete
// channel implementation. Bus is the channel engine behind InProcBus, the
// builtin backend (spec D4.3); external backends are post-v1 config-selected
// adapters behind the same interface (spec D4.4). Conformance of any backend
// is proven mechanically by the eventbustest suite (spec D4.6).
package events

import (
	"sync"
	"time"
)

// Topic identifiers published by R3's background tasks. Consumers match on
// these constant strings.
const (
	// TopicIterationScored is published after an iteration log entry has
	// been scored and its sidecar written.
	TopicIterationScored = "iteration.scored"
	// TopicRescoreDone is published after a rubric-version rescore pass
	// completes.
	TopicRescoreDone = "rescore.done"
	// TopicTaskError is published when a scheduled task records a failure.
	TopicTaskError = "task.error"
)

// defaultBuffer is the per-subscriber channel buffer used when Subscribe is
// called without an explicit capacity. It is large enough to absorb normal
// publish bursts while keeping memory bounded.
const defaultBuffer = 16

// Event is a single message delivered to subscribers of a topic. Payload is
// topic-specific and opaque to the bus; subscribers type-assert it.
type Event struct {
	Topic     string
	Timestamp time.Time
	Payload   any
}

// subscriber is one registered consumer channel for a topic.
type subscriber struct {
	ch      chan Event
	dropped int64
}

// Bus is a concurrency-safe in-process pub/sub bus. The zero value is not
// usable; construct with NewBus.
type Bus struct {
	mu          sync.Mutex
	subscribers map[string][]*subscriber
	closed      bool
	now         func() time.Time
}

// NewBus returns a ready-to-use Bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[string][]*subscriber),
		now:         time.Now,
	}
}

// Publish delivers an event on the given topic to every current subscriber.
//
// Delivery is non-blocking: if a subscriber's buffer is full (a slow
// consumer), the oldest buffered event for that subscriber is discarded to
// make room for the new one, and the subscriber's dropped counter is
// incremented. This guarantees Publish never blocks on a slow consumer.
//
// Publishing on a topic with no subscribers, or after Close, is a no-op.
func (b *Bus) Publish(topic string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	subs := b.subscribers[topic]
	if len(subs) == 0 {
		return
	}
	evt := Event{Topic: topic, Timestamp: b.now(), Payload: payload}
	for _, s := range subs {
		deliver(s, evt)
	}
}

// deliver places evt on the subscriber channel without blocking, applying
// drop-oldest back-pressure semantics. The caller must hold b.mu, which
// also guarantees no concurrent close of s.ch.
func deliver(s *subscriber, evt Event) {
	for {
		select {
		case s.ch <- evt:
			return
		default:
			// Buffer full: drop the oldest queued event and retry.
			select {
			case <-s.ch:
				s.dropped++
			default:
				// Raced empty (subscriber drained between sends);
				// loop and try to send again.
			}
		}
	}
}

// Subscribe registers a consumer for topic and returns a receive-only
// channel of events plus an unsubscribe function. The channel uses
// defaultBuffer capacity. Use SubscribeBuffered to choose the capacity.
//
// The returned unsubscribe function removes the subscription and closes the
// channel. It is safe to call more than once; subsequent calls are no-ops.
func (b *Bus) Subscribe(topic string) (<-chan Event, func()) {
	return b.SubscribeBuffered(topic, defaultBuffer)
}

// SubscribeBuffered is Subscribe with an explicit per-subscriber buffer
// capacity. A capacity below 1 is clamped to 1.
func (b *Bus) SubscribeBuffered(topic string, buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	s := &subscriber{ch: make(chan Event, buffer)}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		// Bus already closed: hand back a closed channel and a no-op
		// unsubscribe so callers never block on receive.
		close(s.ch)
		return s.ch, func() {
			// Intentionally empty: unsubscribe after bus close is a no-op.
		}
	}
	b.subscribers[topic] = append(b.subscribers[topic], s)
	b.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() { b.remove(topic, s) })
	}
	return s.ch, unsubscribe
}

// remove detaches s from topic and closes its channel. Closing under b.mu
// serializes against Publish, so no send can happen on a closed channel.
func (b *Bus) remove(topic string, target *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subscribers[topic]
	for i, s := range subs {
		if s != target {
			continue
		}
		b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
		if len(b.subscribers[topic]) == 0 {
			delete(b.subscribers, topic)
		}
		close(s.ch)
		return
	}
}

// Dropped reports the total number of events discarded across all current
// subscribers due to drop-oldest back-pressure. Useful for observability.
func (b *Bus) Dropped() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var total int64
	for _, subs := range b.subscribers {
		for _, s := range subs {
			total += s.dropped
		}
	}
	return total
}

// Close detaches and closes every subscriber channel and marks the bus
// closed. Subsequent Publish calls are no-ops and subsequent Subscribe
// calls return an already-closed channel. Close is idempotent.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for topic, subs := range b.subscribers {
		for _, s := range subs {
			close(s.ch)
		}
		delete(b.subscribers, topic)
	}
}
