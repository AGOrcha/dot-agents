// Package eventbustest ships the backend-conformance suite for the
// events.EventBus transport seam (spec D4.6). Any EventBus implementation —
// the in-process builtin today, config-selected external adapters (Kafka,
// NATS, Redis) post-v1 — must pass this identical battery: it exercises the
// D4.1 interface (Publish/Subscribe semantics, topic routing, the
// unsubscribe func, Close drain) and asserts the D4.2 G1–G4 floor. A new
// backend is validated mechanically by re-running the suite against its
// constructor, not by a hand-written re-proof.
package eventbustest

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
)

// Factory constructs a fresh backend under test. The suite calls it once
// per check so state never leaks between checks.
type Factory func() events.EventBus

// Options tunes the suite for backend capabilities that exceed the floor.
type Options struct {
	// OrderedPerTopic additionally asserts publish-order delivery per
	// topic. The G2 floor is best-effort only (partitioned backends MAY
	// reorder), so this is opt-in for backends — like the in-process
	// builtin — that do guarantee per-topic order.
	OrderedPerTopic bool
}

// TB is the narrow testing surface the checks report through. *testing.T
// satisfies it; a fake can drive the checks against deliberately broken
// backends to prove the suite itself has teeth.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// waitTimeout bounds every "an event must arrive / a stream must close"
// wait. A var so the suite's own negative tests can shrink it.
var waitTimeout = 2 * time.Second

const (
	// quietWindow is how long a drain waits with no traffic before
	// concluding the backend has delivered everything it intends to.
	quietWindow = 200 * time.Millisecond
	// silenceWindow is how long a "nothing must arrive" observation lasts.
	silenceWindow = 100 * time.Millisecond

	topicAlpha = "conformance.alpha"
	topicBeta  = "conformance.beta"
)

// check is one named conformance assertion run against a fresh backend.
type check struct {
	name string
	fn   func(tb TB, bus events.EventBus)
}

// Run executes the full conformance battery against the backend produced
// by factory, asserting only the D4.2 G1–G4 floor.
func Run(t *testing.T, factory Factory) {
	RunWithOptions(t, factory, Options{})
}

// RunWithOptions is Run with capability opt-ins enabled per opts.
func RunWithOptions(t *testing.T, factory Factory, opts Options) {
	checks := []check{
		{"PublishWithoutSubscribers", checkPublishWithoutSubscribers},
		{"TopicRouting", checkTopicRouting},
		{"FanOut", checkFanOut},
		{"G1AtMostOnceDelivery", checkAtMostOnce},
		{"Unsubscribe", checkUnsubscribe},
		{"G3NonBlockingPublish", checkNonBlockingPublish},
		{"G3DropOldestKeepsNewest", checkDropOldest},
		{"G4IndependentPublishes", checkIndependentPublishes},
		{"CloseSemantics", checkClose},
	}
	if opts.OrderedPerTopic {
		checks = append(checks, check{"OrderedPerTopic", checkOrderedPerTopic})
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			bus := factory()
			defer func() { _ = bus.Close() }()
			c.fn(t, bus)
		})
	}
}

// --- shared helpers -------------------------------------------------------

// subscribe wraps bus.Subscribe and fails the check on error.
func subscribe(tb TB, bus events.EventBus, topic string) (<-chan events.Event, func()) {
	tb.Helper()
	ch, unsub, err := bus.Subscribe(topic)
	if err != nil {
		tb.Fatalf("Subscribe(%q): unexpected error: %v", topic, err)
		return nil, nil
	}
	return ch, unsub
}

// publish wraps bus.Publish and fails the check on error.
func publish(tb TB, bus events.EventBus, topic string, payload any) {
	tb.Helper()
	err := bus.Publish(topic, payload)
	if err != nil {
		tb.Fatalf("Publish(%q, %v): unexpected error: %v", topic, payload, err)
	}
}

// recvEvent waits for one event on ch, failing the check on close or on a
// waitTimeout expiry.
func recvEvent(tb TB, ch <-chan events.Event, what string) events.Event {
	tb.Helper()
	select {
	case evt, ok := <-ch:
		if !ok {
			tb.Fatalf("%s: stream closed unexpectedly", what)
			return events.Event{}
		}
		return evt
	case <-time.After(waitTimeout):
		tb.Fatalf("%s: no event within %v", what, waitTimeout)
		return events.Event{}
	}
}

// expectNoEvent asserts nothing arrives on ch for silenceWindow and that the
// stream stays open.
func expectNoEvent(tb TB, ch <-chan events.Event, what string) {
	tb.Helper()
	select {
	case evt, ok := <-ch:
		if ok {
			tb.Fatalf("%s: unexpected event %+v", what, evt)
		} else {
			tb.Fatalf("%s: stream closed unexpectedly", what)
		}
	case <-time.After(silenceWindow):
	}
}

// expectClosed drains any residual buffered events off ch and asserts the
// stream closes within waitTimeout.
func expectClosed(tb TB, ch <-chan events.Event, what string) {
	tb.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			tb.Fatalf("%s: stream not closed within %v", what, waitTimeout)
			return
		}
	}
}

// drainStrings collects string payloads off ch until the stream goes quiet,
// counting deliveries per payload. Fails the check if the stream closes or
// a payload is not the published string type.
func drainStrings(tb TB, ch <-chan events.Event) map[string]int {
	tb.Helper()
	seen := make(map[string]int)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				tb.Fatalf("drain: stream closed unexpectedly")
				return seen
			}
			payload, isString := evt.Payload.(string)
			if !isString {
				tb.Fatalf("drain: payload %v (%T) is not the published string", evt.Payload, evt.Payload)
				return seen
			}
			seen[payload]++
		case <-time.After(quietWindow):
			return seen
		}
	}
}

// --- checks ----------------------------------------------------------------

// checkPublishWithoutSubscribers: publishing on a topic nobody subscribed to
// succeeds (fire-and-forget, G1/G4).
func checkPublishWithoutSubscribers(tb TB, bus events.EventBus) {
	tb.Helper()
	publish(tb, bus, topicAlpha, "nobody-listening")
}

// checkTopicRouting: events reach only subscribers of the published topic.
func checkTopicRouting(tb TB, bus events.EventBus) {
	tb.Helper()
	chAlpha, unsubAlpha := subscribe(tb, bus, topicAlpha)
	defer unsubAlpha()
	chBeta, unsubBeta := subscribe(tb, bus, topicBeta)
	defer unsubBeta()

	publish(tb, bus, topicAlpha, "routed")
	expectNoEvent(tb, chBeta, "beta subscriber on an alpha publish")
	evt := recvEvent(tb, chAlpha, "alpha subscriber")
	if evt.Payload != "routed" {
		tb.Errorf("alpha payload = %v, want %q", evt.Payload, "routed")
	}
}

// checkFanOut: every subscriber of a topic receives a published event.
func checkFanOut(tb TB, bus events.EventBus) {
	tb.Helper()
	const n = 3
	streams := make([]<-chan events.Event, 0, n)
	for i := 0; i < n; i++ {
		ch, unsub := subscribe(tb, bus, topicAlpha)
		defer unsub()
		streams = append(streams, ch)
	}
	publish(tb, bus, topicAlpha, "fan-out")
	for i, ch := range streams {
		evt := recvEvent(tb, ch, fmt.Sprintf("fan-out subscriber %d", i))
		if evt.Payload != "fan-out" {
			tb.Errorf("subscriber %d payload = %v, want %q", i, evt.Payload, "fan-out")
		}
	}
}

// checkAtMostOnce (G1): no event is delivered more than once to a
// subscriber and no event is fabricated; drops are permitted.
func checkAtMostOnce(tb TB, bus events.EventBus) {
	tb.Helper()
	ch, unsub := subscribe(tb, bus, topicAlpha)
	defer unsub()

	const n = 20
	published := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		payload := fmt.Sprintf("evt-%d", i)
		published[payload] = true
		publish(tb, bus, topicAlpha, payload)
	}

	seen := drainStrings(tb, ch)
	if len(seen) == 0 {
		tb.Fatalf("G1: no events delivered at all for %d publishes", n)
	}
	for payload, count := range seen {
		if count > 1 {
			tb.Errorf("G1 violated: %q delivered %d times, want at most once", payload, count)
		}
		if !published[payload] {
			tb.Errorf("G1 violated: %q delivered but never published", payload)
		}
	}
}

// checkUnsubscribe: the unsubscribe func closes the stream, is safe to call
// twice, and leaves sibling subscribers receiving.
func checkUnsubscribe(tb TB, bus events.EventBus) {
	tb.Helper()
	chFirst, unsubFirst := subscribe(tb, bus, topicAlpha)
	chSecond, unsubSecond := subscribe(tb, bus, topicAlpha)
	defer unsubSecond()

	publish(tb, bus, topicAlpha, "before-unsubscribe")
	recvEvent(tb, chFirst, "first subscriber before unsubscribe")
	recvEvent(tb, chSecond, "second subscriber before unsubscribe")

	unsubFirst()
	unsubFirst() // idempotent: second call must be a safe no-op

	publish(tb, bus, topicAlpha, "after-unsubscribe")
	recvEvent(tb, chSecond, "second subscriber after sibling unsubscribed")
	expectClosed(tb, chFirst, "unsubscribed stream")
}

// checkNonBlockingPublish (G3): a publisher is never blocked by a slow
// subscriber that never drains its stream.
func checkNonBlockingPublish(tb TB, bus events.EventBus) {
	tb.Helper()
	_, unsub := subscribe(tb, bus, topicAlpha) // deliberately never drained
	defer unsub()

	done := make(chan error, 1)
	go func() {
		for i := 0; i < 512; i++ {
			err := bus.Publish(topicAlpha, fmt.Sprintf("burst-%d", i))
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			tb.Fatalf("Publish during burst: unexpected error: %v", err)
		}
	case <-time.After(waitTimeout):
		tb.Fatalf("G3 violated: publisher blocked on a slow subscriber")
	}
}

// checkDropOldest (G3): under overflow the backend drops the OLDEST events
// for the slow subscriber — the newest published event survives, and
// nothing is duplicated.
func checkDropOldest(tb TB, bus events.EventBus) {
	tb.Helper()
	ch, unsub := subscribe(tb, bus, topicAlpha)
	defer unsub()

	const n = 256
	newest := fmt.Sprintf("evt-%d", n-1)
	for i := 0; i < n; i++ {
		publish(tb, bus, topicAlpha, fmt.Sprintf("evt-%d", i))
	}

	seen := drainStrings(tb, ch)
	if seen[newest] == 0 {
		tb.Errorf("G3 violated: newest event %q was dropped; drop-oldest must keep it", newest)
	}
	for payload, count := range seen {
		if count > 1 {
			tb.Errorf("G1 violated during overflow: %q delivered %d times", payload, count)
		}
	}
}

// checkIndependentPublishes (G4): every Publish is an independent
// fire-and-forget — drops forced on one topic's slow subscriber never
// affect delivery on another topic.
func checkIndependentPublishes(tb TB, bus events.EventBus) {
	tb.Helper()
	_, unsubAlpha := subscribe(tb, bus, topicAlpha) // never drained: forces drops
	defer unsubAlpha()
	chBeta, unsubBeta := subscribe(tb, bus, topicBeta)
	defer unsubBeta()

	const rounds = 8
	for i := 0; i < rounds; i++ {
		for j := 0; j < 64; j++ {
			publish(tb, bus, topicAlpha, fmt.Sprintf("flood-%d-%d", i, j))
		}
		want := fmt.Sprintf("beta-%d", i)
		publish(tb, bus, topicBeta, want)
		evt := recvEvent(tb, chBeta, fmt.Sprintf("beta subscriber round %d", i))
		if evt.Payload != want {
			tb.Fatalf("G4 violated: beta round %d got %v, want %q", i, evt.Payload, want)
		}
	}
}

// checkClose: Close succeeds, closes subscriber streams, gates Publish and
// Subscribe behind events.ErrClosed, and is idempotent.
func checkClose(tb TB, bus events.EventBus) {
	tb.Helper()
	ch, _ := subscribe(tb, bus, topicAlpha)
	publish(tb, bus, topicAlpha, "pre-close")

	err := bus.Close()
	if err != nil {
		tb.Fatalf("Close: unexpected error: %v", err)
	}
	expectClosed(tb, ch, "subscriber stream after Close")

	err = bus.Publish(topicAlpha, "post-close")
	if !errors.Is(err, events.ErrClosed) {
		tb.Fatalf("Publish after Close: error = %v, want events.ErrClosed", err)
	}
	_, _, err = bus.Subscribe(topicAlpha)
	if !errors.Is(err, events.ErrClosed) {
		tb.Fatalf("Subscribe after Close: error = %v, want events.ErrClosed", err)
	}
	err = bus.Close()
	if err != nil {
		tb.Fatalf("second Close: unexpected error: %v, want idempotent nil", err)
	}
}

// checkOrderedPerTopic (opt-in, beyond the G2 floor): a backend that claims
// per-topic ordering delivers a small burst in exact publish order.
func checkOrderedPerTopic(tb TB, bus events.EventBus) {
	tb.Helper()
	ch, unsub := subscribe(tb, bus, topicAlpha)
	defer unsub()

	const n = 10 // small burst: stays under any sane bounded buffer
	for i := 0; i < n; i++ {
		publish(tb, bus, topicAlpha, fmt.Sprintf("ord-%d", i))
	}
	for i := 0; i < n; i++ {
		want := fmt.Sprintf("ord-%d", i)
		evt := recvEvent(tb, ch, fmt.Sprintf("ordered event %d", i))
		if evt.Payload != want {
			tb.Fatalf("per-topic order violated at %d: got %v, want %q", i, evt.Payload, want)
		}
	}
}
