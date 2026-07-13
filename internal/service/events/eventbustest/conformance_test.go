package eventbustest

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
)

// --- anchor: the in-process builtin is the seam's integration-test backend --

func inProcFactory() events.EventBus {
	return events.NewInProcBus()
}

// TestInProcBusConformanceOrdered proves the D4.3 builtin passes the full
// battery including the opt-in per-topic-order capability it guarantees.
func TestInProcBusConformanceOrdered(t *testing.T) {
	RunWithOptions(t, inProcFactory, Options{OrderedPerTopic: true})
}

// TestInProcBusConformanceFloor proves the builtin under the plain G1-G4
// floor entrypoint external adapters will use.
func TestInProcBusConformanceFloor(t *testing.T) {
	Run(t, inProcFactory)
}

// --- fake TB: drives checks against broken backends -------------------------

var errFailNow = errors.New("eventbustest: Fatalf called")

type fakeTB struct {
	fatals []string
	errs   []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatals = append(f.fatals, fmt.Sprintf(format, args...))
	panic(errFailNow)
}

// shrinkWaitTimeout keeps the deliberately-hanging negative cases fast.
func shrinkWaitTimeout(t *testing.T) {
	t.Helper()
	old := waitTimeout
	waitTimeout = 75 * time.Millisecond
	t.Cleanup(func() { waitTimeout = old })
}

// runCheck executes one conformance check against a broken backend,
// swallowing the fake's fail-now panic, and returns the fake for asserts.
func runCheck(bus events.EventBus, fn func(TB, events.EventBus)) *fakeTB {
	f := &fakeTB{}
	func() {
		defer func() {
			r := recover()
			if r != nil && r != any(errFailNow) {
				panic(r)
			}
		}()
		fn(f, bus)
	}()
	return f
}

func expectFatalContaining(t *testing.T, f *fakeTB, want string) {
	t.Helper()
	if len(f.fatals) == 0 {
		t.Fatalf("expected a Fatalf containing %q, got none (errors: %v)", want, f.errs)
	}
	if !strings.Contains(f.fatals[0], want) {
		t.Fatalf("Fatalf %q does not contain %q", f.fatals[0], want)
	}
}

func expectErrorContaining(t *testing.T, f *fakeTB, want string) {
	t.Helper()
	if len(f.fatals) != 0 {
		t.Fatalf("expected only Errorf, got Fatalf %q", f.fatals[0])
	}
	for _, msg := range f.errs {
		if strings.Contains(msg, want) {
			return
		}
	}
	t.Fatalf("no Errorf containing %q in %v", want, f.errs)
}

// --- broken backends ---------------------------------------------------------

// errBus returns configured errors; its zero value is a silent backend that
// accepts everything and never delivers.
type errBus struct {
	pubErr   error
	subErr   error
	closeErr error
}

func (b *errBus) Publish(string, any) error { return b.pubErr }

func (b *errBus) Subscribe(string) (<-chan events.Event, func(), error) {
	if b.subErr != nil {
		return nil, nil, b.subErr
	}
	return make(chan events.Event), func() {}, nil
}

func (b *errBus) Close() error { return b.closeErr }

// closedChanBus hands out already-closed streams.
type closedChanBus struct{ errBus }

func (b *closedChanBus) Subscribe(string) (<-chan events.Event, func(), error) {
	ch := make(chan events.Event)
	close(ch)
	return ch, func() {}, nil
}

// blockingBus blocks every Publish on a gate (a G3 violation).
type blockingBus struct {
	errBus
	gate chan struct{}
}

func (b *blockingBus) Publish(string, any) error {
	<-b.gate
	return nil
}

// echoMode selects which contract violation echoBus commits.
type echoMode int

const (
	echoExact       echoMode = iota // honest delivery, but unsub/Close never close streams
	echoLeaky                       // delivers to every subscriber regardless of topic
	echoDup                         // delivers every event twice
	echoBogusString                 // replaces payloads with a fabricated string
	echoIntPayload                  // replaces payloads with a non-string value
	echoFirstOnly                   // delivers only the first publish, drops the rest
)

// echoBus delivers synchronously per its mode; streams are buffered and
// sends drop when full so broken-backend runs can never deadlock the suite.
type echoBus struct {
	mode  echoMode
	mu    sync.Mutex
	subs  map[string][]chan events.Event
	count int
}

func newEchoBus(mode echoMode) *echoBus {
	return &echoBus{mode: mode, subs: make(map[string][]chan events.Event)}
}

func (b *echoBus) Subscribe(topic string) (<-chan events.Event, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan events.Event, 512)
	b.subs[topic] = append(b.subs[topic], ch)
	return ch, func() {}, nil // never closes the stream
}

func (b *echoBus) Publish(topic string, payload any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if b.mode == echoFirstOnly && b.count > 1 {
		return nil
	}
	out := payload
	switch b.mode {
	case echoBogusString:
		out = "bogus"
	case echoIntPayload:
		out = 42
	}
	copies := 1
	if b.mode == echoDup {
		copies = 2
	}
	for _, ch := range b.targets(topic) {
		for i := 0; i < copies; i++ {
			select {
			case ch <- events.Event{Topic: topic, Timestamp: time.Now(), Payload: out}:
			default:
			}
		}
	}
	return nil
}

func (b *echoBus) targets(topic string) []chan events.Event {
	if b.mode != echoLeaky {
		return b.subs[topic]
	}
	var all []chan events.Event
	for _, subs := range b.subs {
		all = append(all, subs...)
	}
	return all
}

func (b *echoBus) Close() error { return nil } // never closes the streams

// noErrAfterCloseBus wraps the builtin but hides ErrClosed from Publish.
type noErrAfterCloseBus struct{ events.EventBus }

func (b *noErrAfterCloseBus) Publish(topic string, payload any) error {
	err := b.EventBus.Publish(topic, payload)
	if errors.Is(err, events.ErrClosed) {
		return nil
	}
	return err
}

// subOKAfterCloseBus wraps the builtin but hides ErrClosed from Subscribe.
type subOKAfterCloseBus struct{ events.EventBus }

func (b *subOKAfterCloseBus) Subscribe(topic string) (<-chan events.Event, func(), error) {
	ch, unsub, err := b.EventBus.Subscribe(topic)
	if errors.Is(err, events.ErrClosed) {
		closed := make(chan events.Event)
		close(closed)
		return closed, func() {}, nil
	}
	return ch, unsub, err
}

// secondCloseErrBus wraps the builtin but fails the idempotent re-Close.
type secondCloseErrBus struct {
	events.EventBus
	closes int
}

func (b *secondCloseErrBus) Close() error {
	b.closes++
	if b.closes > 1 {
		return errors.New("second close failed")
	}
	return b.EventBus.Close()
}

// --- the suite has teeth: every violation is caught --------------------------

func TestSuiteCatchesFatalViolations(t *testing.T) {
	shrinkWaitTimeout(t)
	boom := errors.New("boom")
	blocker := &blockingBus{gate: make(chan struct{})}
	t.Cleanup(func() { close(blocker.gate) })

	tests := []struct {
		name string
		bus  events.EventBus
		fn   func(TB, events.EventBus)
		want string
	}{
		{"subscribe error", &errBus{subErr: boom}, checkFanOut, "Subscribe("},
		{"publish error", &errBus{pubErr: boom}, checkPublishWithoutSubscribers, "Publish("},
		{"no delivery times out", &errBus{}, checkFanOut, "no event within"},
		{"closed stream on receive", &closedChanBus{}, checkFanOut, "stream closed unexpectedly"},
		{"leaky topic routing", newEchoBus(echoLeaky), checkTopicRouting, "unexpected event"},
		{"closed stream during silence watch", &closedChanBus{}, checkTopicRouting, "stream closed unexpectedly"},
		{"closed stream during drain", &closedChanBus{}, checkAtMostOnce, "drain: stream closed"},
		{"non-string payload fabricated", newEchoBus(echoIntPayload), checkAtMostOnce, "is not the published string"},
		{"nothing delivered at all", &errBus{}, checkAtMostOnce, "no events delivered"},
		{"publish error mid-burst", &errBus{pubErr: boom}, checkNonBlockingPublish, "Publish during burst"},
		{"blocked publisher", blocker, checkNonBlockingPublish, "publisher blocked"},
		{"cross-topic interference", newEchoBus(echoBogusString), checkIndependentPublishes, "G4 violated"},
		{"unsubscribe leaves stream open", newEchoBus(echoExact), checkUnsubscribe, "not closed within"},
		{"close returns error", &errBus{closeErr: boom}, checkClose, "Close: unexpected error"},
		{"close leaves streams open", newEchoBus(echoExact), checkClose, "not closed within"},
		{"publish after close not gated", &noErrAfterCloseBus{events.NewInProcBus()}, checkClose, "Publish after Close"},
		{"subscribe after close not gated", &subOKAfterCloseBus{events.NewInProcBus()}, checkClose, "Subscribe after Close"},
		{"second close fails", &secondCloseErrBus{EventBus: events.NewInProcBus()}, checkClose, "second Close"},
		{"out-of-order delivery", newEchoBus(echoBogusString), checkOrderedPerTopic, "order violated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := runCheck(tc.bus, tc.fn)
			expectFatalContaining(t, f, tc.want)
		})
	}
}

func TestSuiteCatchesNonFatalViolations(t *testing.T) {
	tests := []struct {
		name string
		bus  events.EventBus
		fn   func(TB, events.EventBus)
		want string
	}{
		{"duplicate delivery", newEchoBus(echoDup), checkAtMostOnce, "delivered 2 times"},
		{"fabricated payload", newEchoBus(echoBogusString), checkAtMostOnce, "never published"},
		{"newest event dropped", newEchoBus(echoFirstOnly), checkDropOldest, "newest event"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := runCheck(tc.bus, tc.fn)
			expectErrorContaining(t, f, tc.want)
		})
	}
}
