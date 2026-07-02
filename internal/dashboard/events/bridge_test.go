package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
)

// fakeBus is a hand-driven EventBus for bridge tests: it exposes the
// subscription channels so tests can inject events with crafted
// timestamps and observe unsubscribes.
type fakeBus struct {
	mu        sync.Mutex
	streams   map[string]chan svcevents.Event
	failTopic string
	unsubbed  []string
}

var _ svcevents.EventBus = (*fakeBus)(nil)

func newFakeBus() *fakeBus {
	return &fakeBus{streams: make(map[string]chan svcevents.Event)}
}

func (f *fakeBus) Publish(topic string, payload any) error {
	f.mu.Lock()
	stream := f.streams[topic]
	f.mu.Unlock()
	if stream != nil {
		stream <- svcevents.Event{Topic: topic, Timestamp: time.Now(), Payload: payload}
	}
	return nil
}

func (f *fakeBus) inject(ev svcevents.Event) {
	f.mu.Lock()
	stream := f.streams[ev.Topic]
	f.mu.Unlock()
	stream <- ev
}

func (f *fakeBus) Subscribe(topic string) (<-chan svcevents.Event, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if topic == f.failTopic {
		return nil, nil, errors.New("fake bus: subscribe refused")
	}
	stream := make(chan svcevents.Event, 16)
	f.streams[topic] = stream
	var once sync.Once
	unsub := func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			delete(f.streams, topic)
			close(stream)
			f.unsubbed = append(f.unsubbed, topic)
		})
	}
	return stream, unsub, nil
}

func (f *fakeBus) Close() error { return nil }

func (f *fakeBus) unsubscribed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.unsubbed...)
}

func TestAttachBusForwardsEventsAndEvicts(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachBus(bus, TopicIterationScored)
	if err != nil {
		t.Fatalf("AttachBus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	if err := bus.Publish(TopicIterationScored, rootedPayload{root: "roots/x"}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}
	ev := mustReceive(t, ch)
	if ev.Type != TopicIterationScored {
		t.Errorf("forwarded type = %q, want %q", ev.Type, TopicIterationScored)
	}
	if ev.Payload != (rootedPayload{root: "roots/x"}) {
		t.Errorf("forwarded payload = %#v, want the bus payload", ev.Payload)
	}
	roots, all := evictor.snapshot()
	if len(roots) != 1 || roots[0] != "roots/x" || all != 0 {
		t.Errorf("bridged eviction: roots=%v evictAll=%d, want [roots/x] and 0", roots, all)
	}
}

func TestAttachBusMultipleTopics(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachBus(bus, TopicIterationScored, TopicSessionUpdated)
	if err != nil {
		t.Fatalf("AttachBus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	_ = bus.Publish(TopicIterationScored, nil)
	_ = bus.Publish(TopicSessionUpdated, nil)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		seen[mustReceive(t, ch).Type] = true
	}
	if !seen[TopicIterationScored] || !seen[TopicSessionUpdated] {
		t.Errorf("forwarded topics = %v, want both attached topics", seen)
	}
}

func TestAttachBusPreservesBusTimestamp(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := newFakeBus()

	detach, err := b.AttachBus(bus, TopicScoreRecomputed)
	if err != nil {
		t.Fatalf("AttachBus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	stamp := time.Date(2026, 5, 28, 14, 3, 11, 987654321, time.FixedZone("PDT", -7*3600))
	bus.inject(svcevents.Event{Topic: TopicScoreRecomputed, Timestamp: stamp, Payload: nil})

	ev := mustReceive(t, ch)
	want := stamp.UTC().Truncate(time.Second)
	if !ev.TS.Equal(want) || ev.TS.Location() != time.UTC || ev.TS.Nanosecond() != 0 {
		t.Errorf("bridged TS = %v, want %v (whole-second UTC)", ev.TS, want)
	}
}

func TestDetachStopsForwardingAndIsIdempotent(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachBus(bus, TopicIterationScored)
	if err != nil {
		t.Fatalf("AttachBus: %v", err)
	}
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	detach()
	detach() // idempotent
	_ = bus.Publish(TopicIterationScored, "after-detach")
	mustStaySilent(t, ch)
}

func TestAttachBusSubscribeErrorUnwindsPartialAttach(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := newFakeBus()
	bus.failTopic = TopicSessionUpdated

	if _, err := b.AttachBus(bus, TopicIterationScored, TopicSessionUpdated); err == nil {
		t.Fatalf("AttachBus with a refusing bus: want error, got nil")
	}
	unsubbed := bus.unsubscribed()
	if len(unsubbed) != 1 || unsubbed[0] != TopicIterationScored {
		t.Errorf("unwound subscriptions = %v, want [%s]", unsubbed, TopicIterationScored)
	}
}

func TestAttachBusOnClosedBrokerReturnsErrClosed(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	b.Close()
	bus := newFakeBus()

	if _, err := b.AttachBus(bus, TopicIterationScored); !errors.Is(err, ErrClosed) {
		t.Fatalf("AttachBus on closed broker: err = %v, want ErrClosed", err)
	}
	unsubbed := bus.unsubscribed()
	if len(unsubbed) != 1 || unsubbed[0] != TopicIterationScored {
		t.Errorf("unwound subscriptions = %v, want [%s]", unsubbed, TopicIterationScored)
	}
}

func TestCloseDetachesAttachedBus(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	if _, err := b.AttachBus(bus, TopicIterationScored); err != nil {
		t.Fatalf("AttachBus: %v", err)
	}
	// Close must run the detach itself; if it did not, wg.Wait would
	// deadlock on the still-open forward goroutine and this test would
	// time out.
	b.Close()
	if err := bus.Publish(TopicIterationScored, nil); err != nil {
		t.Errorf("bus.Publish after broker Close: %v, want nil (bus unaffected)", err)
	}
}

// TestBridgedStressUnderRace drives concurrent bus publishes, local
// publishes, and subscriber churn through the bridge under -race.
func TestBridgedStressUnderRace(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{
		Buffer:    4,
		Heartbeat: 2 * time.Millisecond,
		Evictor:   evictor,
	})
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()
	if _, err := b.AttachBus(bus, TopicIterationScored, TopicSessionUpdated); err != nil {
		t.Fatalf("AttachBus: %v", err)
	}

	const producers = 3
	const perProducer = 100
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancelCtx := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancelCtx()
			ch, cancel := b.Subscribe(ctx)
			defer cancel()
			for range ch {
				// Drain until ctx cancel closes the stream.
			}
		}()
	}
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < perProducer; n++ {
				_ = bus.Publish(TopicIterationScored, rootedPayload{root: "bridged"})
				b.Publish(TopicSessionUpdated, nil)
			}
		}()
	}

	wg.Wait()
	b.Close()
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after Close = %d, want 0", n)
	}
}
