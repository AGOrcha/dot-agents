package events

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBuiltinBackendRef(t *testing.T) {
	// Spec D4.3/D4.4: the builtin's adapter reference under the synthetic
	// dotagents-builtin source.
	want := "dotagents-builtin:eventbus/inproc@^1.0"
	if BuiltinBackendRef != want {
		t.Fatalf("BuiltinBackendRef = %q, want %q", BuiltinBackendRef, want)
	}
}

func TestInProcBusRoundtrip(t *testing.T) {
	bus := NewInProcBus()
	defer func() { _ = bus.Close() }()

	ch, unsubscribe, err := bus.Subscribe(TopicIterationScored)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	err = bus.Publish(TopicIterationScored, "payload-1")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Topic != TopicIterationScored {
			t.Errorf("topic = %q, want %q", evt.Topic, TopicIterationScored)
		}
		if evt.Payload != "payload-1" {
			t.Errorf("payload = %v, want payload-1", evt.Payload)
		}
		if evt.Timestamp.IsZero() {
			t.Errorf("timestamp not set")
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}

	unsubscribe()
	unsubscribe() // idempotent
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed stream after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("stream not closed after unsubscribe")
	}
}

func TestInProcBusErrClosed(t *testing.T) {
	bus := NewInProcBus()
	ch, _, err := bus.Subscribe(TopicRescoreDone)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	err = bus.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed stream after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber stream not closed by Close")
	}

	err = bus.Publish(TopicRescoreDone, "late")
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Publish after Close: err = %v, want ErrClosed", err)
	}
	_, _, err = bus.Subscribe(TopicRescoreDone)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close: err = %v, want ErrClosed", err)
	}
	err = bus.Close()
	if err != nil {
		t.Fatalf("second Close: err = %v, want nil (idempotent)", err)
	}
}

func TestInProcBusDroppedPassthrough(t *testing.T) {
	bus := NewInProcBus()
	defer func() { _ = bus.Close() }()

	_, _, err := bus.Subscribe(TopicTaskError)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// defaultBuffer is 16: publishing 20 without draining drops exactly 4.
	for i := 0; i < defaultBuffer+4; i++ {
		perr := bus.Publish(TopicTaskError, i)
		if perr != nil {
			t.Fatalf("Publish %d: %v", i, perr)
		}
	}
	if got := bus.Dropped(); got != 4 {
		t.Fatalf("Dropped = %d, want 4", got)
	}
}

// TestInProcBusConcurrentPublishClose exercises the adapter's closed-state
// gate under the race detector: concurrent publishers observe either a nil
// error (before Close) or ErrClosed (after), never anything else.
func TestInProcBusConcurrentPublishClose(t *testing.T) {
	bus := NewInProcBus()
	_, _, err := bus.Subscribe(TopicIterationScored)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				perr := bus.Publish(TopicIterationScored, i)
				if perr != nil && !errors.Is(perr, ErrClosed) {
					t.Errorf("Publish: unexpected error %v", perr)
					return
				}
			}
		}()
	}
	cerr := bus.Close()
	if cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
	wg.Wait()

	perr := bus.Publish(TopicIterationScored, "after")
	if !errors.Is(perr, ErrClosed) {
		t.Fatalf("Publish after Close: err = %v, want ErrClosed", perr)
	}
}
