package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTopicConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"iteration scored", TopicIterationScored, "iteration.scored"},
		{"rescore done", TopicRescoreDone, "rescore.done"},
		{"task error", TopicTaskError, "task.error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestPublishToMultipleSubscribers(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"single subscriber", 1},
		{"three subscribers", 3},
		{"ten subscribers", 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBus()
			defer b.Close()

			chans := make([]<-chan Event, tc.n)
			for i := 0; i < tc.n; i++ {
				ch, _ := b.Subscribe(TopicIterationScored)
				chans[i] = ch
			}

			b.Publish(TopicIterationScored, "payload-1")

			for i, ch := range chans {
				select {
				case evt := <-ch:
					if evt.Topic != TopicIterationScored {
						t.Errorf("subscriber %d: topic %q, want %q", i, evt.Topic, TopicIterationScored)
					}
					if evt.Payload != "payload-1" {
						t.Errorf("subscriber %d: payload %v, want payload-1", i, evt.Payload)
					}
					if evt.Timestamp.IsZero() {
						t.Errorf("subscriber %d: timestamp not set", i)
					}
				case <-time.After(time.Second):
					t.Fatalf("subscriber %d: no event received", i)
				}
			}
		})
	}
}

func TestPublishTopicIsolation(t *testing.T) {
	b := NewBus()
	defer b.Close()

	scored, _ := b.Subscribe(TopicIterationScored)
	rescored, _ := b.Subscribe(TopicRescoreDone)

	b.Publish(TopicIterationScored, "only-scored")

	select {
	case evt := <-scored:
		if evt.Payload != "only-scored" {
			t.Fatalf("payload %v, want only-scored", evt.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("scored subscriber received no event")
	}

	select {
	case evt := <-rescored:
		t.Fatalf("rescore subscriber should not have received %v", evt)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	b := NewBus()
	defer b.Close()
	// Must not panic or block.
	b.Publish(TopicTaskError, "lost")
	b.Publish("never-subscribed", 42)
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	b := NewBus()
	defer b.Close()

	ch, unsubscribe := b.Subscribe(TopicRescoreDone)
	unsubscribe()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after unsubscribe")
	}

	// Publish after unsubscribe must not panic (no send on closed chan).
	b.Publish(TopicRescoreDone, "after-unsub")
}

func TestUnsubscribeIdempotent(t *testing.T) {
	b := NewBus()
	defer b.Close()

	_, unsubscribe := b.Subscribe(TopicTaskError)
	unsubscribe()
	unsubscribe() // second call must be a safe no-op
}

func TestUnsubscribeOneOfMany(t *testing.T) {
	b := NewBus()
	defer b.Close()

	a, unsubA := b.Subscribe(TopicIterationScored)
	bch, _ := b.Subscribe(TopicIterationScored)

	unsubA()

	b.Publish(TopicIterationScored, "x")

	// a is closed and drained.
	if _, ok := <-a; ok {
		t.Fatal("unsubscribed channel a should be closed")
	}
	// b still receives.
	select {
	case evt := <-bch:
		if evt.Payload != "x" {
			t.Fatalf("payload %v, want x", evt.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("remaining subscriber received no event")
	}
}

func TestDropOldestOnSlowConsumer(t *testing.T) {
	b := NewBus()
	defer b.Close()

	// Buffer of 2; never drain. Publishing 5 events forces 3 drops and
	// leaves the 2 newest queued.
	ch, _ := b.SubscribeBuffered(TopicIterationScored, 2)

	for i := 0; i < 5; i++ {
		b.Publish(TopicIterationScored, i)
	}

	if got := b.Dropped(); got != 3 {
		t.Fatalf("dropped count = %d, want 3", got)
	}

	// The two retained events must be the newest (3 and 4).
	got := []any{}
	for i := 0; i < 2; i++ {
		select {
		case evt := <-ch:
			got = append(got, evt.Payload)
		default:
			t.Fatalf("expected 2 buffered events, got %d", len(got))
		}
	}
	if got[0] != 3 || got[1] != 4 {
		t.Fatalf("retained payloads = %v, want [3 4]", got)
	}
}

func TestDroppedZeroWhenKeepingUp(t *testing.T) {
	b := NewBus()
	defer b.Close()

	ch, _ := b.SubscribeBuffered(TopicRescoreDone, 4)
	for i := 0; i < 3; i++ {
		b.Publish(TopicRescoreDone, i)
		<-ch // drain immediately
	}
	if got := b.Dropped(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
}

func TestSubscribeBufferClamped(t *testing.T) {
	tests := []struct {
		name   string
		buffer int
	}{
		{"zero clamps to one", 0},
		{"negative clamps to one", -5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBus()
			defer b.Close()
			ch, _ := b.SubscribeBuffered(TopicTaskError, tc.buffer)
			if c := cap(ch); c != 1 {
				t.Fatalf("cap = %d, want 1", c)
			}
		})
	}
}

func TestCloseClosesAllChannels(t *testing.T) {
	b := NewBus()

	a, _ := b.Subscribe(TopicIterationScored)
	bch, _ := b.Subscribe(TopicRescoreDone)

	b.Close()

	for name, ch := range map[string]<-chan Event{"a": a, "b": bch} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Fatalf("channel %s should be closed after Close", name)
			}
		case <-time.After(time.Second):
			t.Fatalf("channel %s not closed after Close", name)
		}
	}
}

func TestCloseIdempotent(t *testing.T) {
	b := NewBus()
	b.Subscribe(TopicIterationScored)
	b.Close()
	b.Close() // must not panic
}

func TestPublishAfterClose(t *testing.T) {
	b := NewBus()
	b.Close()
	b.Publish(TopicIterationScored, "ignored") // no-op, no panic
	if got := b.Dropped(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
}

func TestSubscribeAfterClose(t *testing.T) {
	b := NewBus()
	b.Close()

	ch, unsubscribe := b.Subscribe(TopicRescoreDone)
	// Channel must already be closed so receivers never block.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel from closed bus should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("channel from closed bus not closed")
	}
	unsubscribe() // no-op, must not panic
}

func TestConcurrentPublish(t *testing.T) {
	b := NewBus()
	defer b.Close()

	const subscribers = 4
	const publishers = 8
	const perPublisher = 200

	var received [subscribers]int64
	var wg sync.WaitGroup

	for i := 0; i < subscribers; i++ {
		// Large buffer so we count deliveries rather than exercise drops.
		ch, _ := b.SubscribeBuffered(TopicIterationScored, publishers*perPublisher)
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range ch { // ranges until Close closes the channel
				atomic.AddInt64(&received[idx], 1)
			}
		}()
	}

	var pubWg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		pubWg.Add(1)
		go func(base int) {
			defer pubWg.Done()
			for j := 0; j < perPublisher; j++ {
				b.Publish(TopicIterationScored, base+j)
			}
		}(p * perPublisher)
	}
	pubWg.Wait()

	// Closing the bus closes subscriber channels, terminating the readers.
	b.Close()
	wg.Wait()

	want := int64(publishers * perPublisher)
	for i := 0; i < subscribers; i++ {
		if got := atomic.LoadInt64(&received[i]); got != want {
			t.Fatalf("subscriber %d received %d, want %d", i, got, want)
		}
	}
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	b := NewBus()
	defer b.Close()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Background publisher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				b.Publish(TopicTaskError, "tick")
			}
		}
	}()

	// Churn subscriptions concurrently.
	var churn sync.WaitGroup
	for i := 0; i < 16; i++ {
		churn.Add(1)
		go func() {
			defer churn.Done()
			for j := 0; j < 50; j++ {
				ch, unsub := b.Subscribe(TopicTaskError)
				select {
				case <-ch:
				default:
				}
				unsub()
			}
		}()
	}

	// Once the churners are done, stop the publisher and join.
	churn.Wait()
	close(stop)
	wg.Wait()
}

func TestDeliverRetriesAfterRacedDrain(t *testing.T) {
	// Exercises the drop-oldest loop where the buffer is full: the deliver
	// helper must drop the oldest and successfully enqueue the new event.
	b := NewBus()
	defer b.Close()

	ch, _ := b.SubscribeBuffered(TopicRescoreDone, 1)
	b.Publish(TopicRescoreDone, "first")
	b.Publish(TopicRescoreDone, "second") // drops "first"

	if got := b.Dropped(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if evt := <-ch; evt.Payload != "second" {
		t.Fatalf("payload = %v, want second", evt.Payload)
	}
}

func TestInjectableClock(t *testing.T) {
	b := NewBus()
	defer b.Close()
	fixed := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	b.now = func() time.Time { return fixed }

	ch, _ := b.Subscribe(TopicIterationScored)
	b.Publish(TopicIterationScored, nil)
	evt := <-ch
	if !evt.Timestamp.Equal(fixed) {
		t.Fatalf("timestamp = %v, want %v", evt.Timestamp, fixed)
	}
}
