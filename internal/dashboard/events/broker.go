// Package events implements the R2 dashboard's SSE broker (plan task
// t04-sse-broker): an in-process fan-out from event sources to per-client
// bounded buffers.
//
// Sources are (a) the local Publisher seam — t06's filesystem watcher
// publishes here pre-R3 — and (b) an R3 EventBus attached via
// Broker.AttachBus (bridge.go), which binds ONLY to the D4.1 EventBus
// interface, never to a concrete backend, and assumes only the D4.2 G1–G4
// floor (at-most-once best-effort delivery, best-effort per-topic order,
// bounded-buffer non-blocking publish, no cross-publish atomicity).
//
// Backpressure implements spec OQ5's recommendation ("bounded buffer with
// disconnect-on-overflow, client refetches on reconnect"), which is also
// API.md §3.7's pinned resolution: each subscriber holds one bounded
// buffered channel, and EVERY drop is terminal — the first event that
// cannot be enqueued disconnects that subscriber (channel closed), so the
// SSE client reconnects and refetches. A live stream therefore never
// contains a silent hole: drop ⇒ disconnect ⇒ refetch, with no window in
// which a client keeps consuming past a lost event. Publishers never
// block (G3-compatible).
//
// The broker also owns the store-cache eviction wiring point: on every
// pushed event (never on heartbeats) it invalidates the t02 read cache via
// the Evictor hooks (store.DiskStore's per-root Evict / whole-cache
// EvictAll) BEFORE fan-out, so a client refetching in reaction to the
// event always sees post-event state.
//
// Anti-scope: no HTTP (t05 owns the SSE endpoint), no event production
// (t06 fswatch / R3 bus via t13), no replay (spec D2.2: reconnect+refetch).
package events

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Dashboard SSE topics — the API.md §3.7 event taxonomy carried in the
// SSE `event:` field and validated by schemas/dashboard-event.schema.json.
const (
	// TopicIterationScored — a new iter-N.score.yaml sidecar appeared (spec R5).
	TopicIterationScored = "iteration.scored"
	// TopicSessionUpdated — a session score sidecar was rewritten (spec R6).
	TopicSessionUpdated = "session.updated"
	// TopicScoreRecomputed — an existing iteration score changed in place.
	TopicScoreRecomputed = "score.recomputed"
	// TopicRubricChanged — rubric recomputed under a NEW version (spec R7).
	TopicRubricChanged = "rubric.changed"
	// TopicHeartbeat — keepalive emitted by the broker itself.
	TopicHeartbeat = "heartbeat"
)

// Defaults applied by New when the corresponding Options field is zero.
const (
	// DefaultBuffer is the per-subscriber bounded channel capacity.
	DefaultBuffer = 64
	// DefaultHeartbeat is the keepalive interval (API.md §3.7: 15s).
	DefaultHeartbeat = 15 * time.Second
)

// ErrClosed is returned by AttachBus after the broker has been closed.
var ErrClosed = errors.New("dashboard/events: broker closed")

// Event is one message delivered to a broker subscriber. Field names and
// JSON tags mirror schemas/dashboard-event.schema.json — t05 marshals this
// struct into the SSE frame's data: field (`event:` = Type, `id:` = Seq).
type Event struct {
	// Type is the topic (event taxonomy constant).
	Type string `json:"type"`
	// Seq is the monotonic per-connection sequence, starting at 0 and
	// resetting on reconnect. It is NOT a durable cursor (no replay).
	Seq uint64 `json:"seq"`
	// TS is the publish time, stamped in UTC truncated to whole seconds
	// so json.Marshal emits the schema's RFC3339 "Z" pattern verbatim.
	TS time.Time `json:"ts"`
	// Payload is the topic-specific thin key set; opaque to the broker.
	Payload any `json:"payload"`
}

// Publisher is the broker's local publish seam. Pre-R3 producers (t06's
// filesystem watcher) hold this minimal shape; the r3-facing seam is the
// events.EventBus interface, bridged via AttachBus (wired in t13).
type Publisher interface {
	Publish(topic string, payload any)
}

// Evictor is the t02 store-cache invalidation hook. *store.DiskStore
// satisfies it (per-root Evict + whole-cache EvictAll push hooks).
type Evictor interface {
	Evict(root string)
	EvictAll()
}

// RootScoped is optionally implemented by event payloads that identify
// the iter-log root they concern. When a payload is root-scoped the
// broker evicts only that root's cache snapshot; otherwise it falls back
// to whole-cache eviction (correct but coarser).
type RootScoped interface {
	IterLogRoot() string
}

// Options configures a Broker. The zero value yields the defaults.
type Options struct {
	// Buffer is the per-subscriber channel capacity; <=0 → DefaultBuffer.
	Buffer int
	// Heartbeat is the keepalive interval; 0 → DefaultHeartbeat,
	// negative → heartbeats disabled (tests, embedded use).
	Heartbeat time.Duration
	// Evictor, when non-nil, receives cache invalidations before fan-out.
	Evictor Evictor
}

// subscriber is one connected client's bounded buffer.
type subscriber struct {
	ch chan Event
	// seq is the next sequence number to assign on enqueue. Because any
	// drop disconnects the subscriber, a delivered stream is always both
	// gap-free and hole-free: seq runs contiguously from 0 until close.
	seq uint64
	// done unblocks the context watcher when the subscriber is removed.
	done chan struct{}
}

// Broker fans events out to per-client bounded buffers. It implements
// Publisher. Construct with New; the zero value is not usable.
type Broker struct {
	opts Options
	// now is the stamp/stall clock; injectable in tests.
	now func() time.Time

	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool
	// detaches are AttachBus unsubscribers, run on Close so forwarding
	// goroutines terminate before Close returns.
	detaches []func()

	stopHeartbeat chan struct{}
	wg            sync.WaitGroup
}

// Compile-time proof of the t04 contract shape.
var _ Publisher = (*Broker)(nil)

// New returns a running Broker (heartbeat active unless disabled).
func New(opts Options) *Broker {
	if opts.Buffer <= 0 {
		opts.Buffer = DefaultBuffer
	}
	if opts.Heartbeat == 0 {
		opts.Heartbeat = DefaultHeartbeat
	}
	b := &Broker{
		opts:          opts,
		now:           func() time.Time { return time.Now().UTC().Truncate(time.Second) },
		subs:          make(map[*subscriber]struct{}),
		stopHeartbeat: make(chan struct{}),
	}
	if opts.Heartbeat > 0 {
		b.wg.Add(1)
		go b.heartbeatLoop(opts.Heartbeat)
	}
	return b
}

// Publish implements Publisher: evicts the store cache for the event,
// then fans it out to every subscriber. Non-blocking for the publisher;
// delivery to any individual subscriber is at-most-once (G1). Publishing
// on a closed broker is a complete no-op (no eviction, no fan-out).
func (b *Broker) Publish(topic string, payload any) {
	if b.isClosed() {
		return
	}
	b.evict(payload)
	b.dispatch(topic, payload, b.now())
}

// isClosed reports whether Close has run.
func (b *Broker) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// Subscribe registers a client and returns its bounded event stream plus
// an idempotent cancel func. The stream closes on cancel, on context
// cancellation, on broker Close, or the moment the client's buffer
// overflows (OQ5 disconnect-on-overflow — the client must then reconnect
// and refetch; no event is ever silently skipped on a live stream).
// Subscribing on a closed broker returns an already-closed stream and a
// no-op cancel.
func (b *Broker) Subscribe(ctx context.Context) (<-chan Event, func()) {
	s := &subscriber{
		ch:   make(chan Event, b.opts.Buffer),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(s.ch)
		return s.ch, func() {
			// Intentionally empty: cancel after broker close is a no-op.
		}
	}
	b.subs[s] = struct{}{}
	b.wg.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.wg.Done()
		select {
		case <-ctx.Done():
			b.remove(s)
		case <-s.done:
			// Removed by cancel, overflow-disconnect, or Close.
		}
	}()
	return s.ch, func() { b.remove(s) }
}

// SubscriberCount reports the live subscriber count (the health
// endpoint's subscriber_count field, API.md §3.6).
func (b *Broker) SubscriberCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

// Close disconnects every subscriber, detaches any attached buses, stops
// the heartbeat, and waits for all broker goroutines to exit. Idempotent.
// Subsequent Publish calls are no-ops.
func (b *Broker) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.stopHeartbeat)
	detaches := b.detaches
	b.detaches = nil
	for s := range b.subs {
		b.removeLocked(s)
	}
	b.mu.Unlock()

	// Outside b.mu: detach funcs take the attached bus's own locks.
	for _, detach := range detaches {
		detach()
	}
	b.wg.Wait()
}

// evict runs the store-cache invalidation hook for a pushed event:
// per-root when the payload is root-scoped, whole-cache otherwise.
// Called before fan-out so refetching clients see post-event state.
func (b *Broker) evict(payload any) {
	if b.opts.Evictor == nil {
		return
	}
	if scoped, ok := payload.(RootScoped); ok {
		if root := scoped.IterLogRoot(); root != "" {
			b.opts.Evictor.Evict(root)
			return
		}
	}
	b.opts.Evictor.EvictAll()
}

// dispatch fans one event out to every current subscriber. ts is the
// event stamp (local publish time, or the source bus's timestamp for
// bridged events).
func (b *Broker) dispatch(topic string, payload any, ts time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for s := range b.subs {
		b.offerLocked(s, topic, payload, ts)
	}
}

// offerLocked enqueues an event on one subscriber without ever blocking
// the fan-out. Every drop is terminal (spec OQ5: bounded buffer with
// disconnect-on-overflow, client refetches on reconnect): the FIRST
// event that cannot be enqueued disconnects the subscriber immediately.
// That preserves the OQ5 invariant drop ⇒ disconnect ⇒ refetch — a live
// stream can never carry a silent hole the client would consume past.
// Events already buffered before the drop remain deliverable (they
// precede the hole); nothing can be delivered after it because the
// stream is closed here, under the same lock every send takes.
// Heartbeats are treated uniformly: a buffer with no room for a
// heartbeat has no room for the next data event either, so keeping the
// invariant unconditional is both simplest and provably hole-free.
// Caller must hold b.mu.
func (b *Broker) offerLocked(s *subscriber, topic string, payload any, ts time.Time) {
	select {
	case s.ch <- Event{Type: topic, Seq: s.seq, TS: ts, Payload: payload}:
		s.seq++
	default:
		b.removeLocked(s)
	}
}

// remove detaches one subscriber and closes its stream. Idempotent.
func (b *Broker) remove(s *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeLocked(s)
}

// removeLocked is remove with b.mu held. Closing s.ch under b.mu
// serializes against offerLocked, so no send races the close.
func (b *Broker) removeLocked(s *subscriber) {
	if _, ok := b.subs[s]; !ok {
		return
	}
	delete(b.subs, s)
	close(s.ch)
	close(s.done)
}

// heartbeatLoop emits a typed heartbeat event to every subscriber on the
// configured interval (API.md §3.7). Heartbeats never trigger cache
// eviction; like any event, an undeliverable heartbeat disconnects the
// overflowing subscriber, so even idle-stream stalls surface within one
// heartbeat interval.
func (b *Broker) heartbeatLoop(interval time.Duration) {
	defer b.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopHeartbeat:
			return
		case <-ticker.C:
			b.dispatch(TopicHeartbeat, struct{}{}, b.now())
		}
	}
}
