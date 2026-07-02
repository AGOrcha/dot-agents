package events

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
)

// The t02 read layer must satisfy the broker's eviction wiring point.
var _ Evictor = (*store.DiskStore)(nil)

// waitTimeout bounds every "an event must arrive / a stream must close" wait.
const waitTimeout = 2 * time.Second

// silenceWindow is how long a "nothing must arrive" observation lasts.
const silenceWindow = 100 * time.Millisecond

// noHeartbeat disables the keepalive so timing-sensitive tests see only
// their own publishes.
const noHeartbeat = -1 * time.Millisecond

// recordingEvictor is a concurrency-safe Evictor fake.
type recordingEvictor struct {
	mu       sync.Mutex
	roots    []string
	evictAll int
}

func (r *recordingEvictor) Evict(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roots = append(r.roots, root)
}

func (r *recordingEvictor) EvictAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evictAll++
}

func (r *recordingEvictor) snapshot() ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.roots...), r.evictAll
}

// rootedPayload implements RootScoped for per-root eviction tests.
type rootedPayload struct{ root string }

func (p rootedPayload) IterLogRoot() string { return p.root }

// mustReceive reads one event or fails the test after waitTimeout.
func mustReceive(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("stream closed while an event was expected")
		}
		return ev
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for an event")
	}
	panic("unreachable")
}

// mustClose drains the stream until it closes or fails after waitTimeout.
func mustClose(t *testing.T, ch <-chan Event) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the stream to close")
		}
	}
}

// mustStaySilent asserts no event arrives within silenceWindow.
func mustStaySilent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("unexpected event %q while silence was expected", ev.Type)
		}
	case <-time.After(silenceWindow):
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	b := New(Options{}) // zero value: all defaults, heartbeat running
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()
	if got := cap(ch); got != DefaultBuffer {
		t.Errorf("default subscriber buffer = %d, want %d", got, DefaultBuffer)
	}
	if b.opts.Grace != DefaultGrace || b.opts.Heartbeat != DefaultHeartbeat {
		t.Errorf("defaults = grace %v / heartbeat %v, want %v / %v",
			b.opts.Grace, b.opts.Heartbeat, DefaultGrace, DefaultHeartbeat)
	}
}

func TestPublishFansOutToAllSubscribers(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	chA, cancelA := b.Subscribe(context.Background())
	chB, cancelB := b.Subscribe(context.Background())
	defer cancelA()
	defer cancelB()

	b.Publish(TopicIterationScored, "p0")
	b.Publish(TopicSessionUpdated, "p1")

	for name, ch := range map[string]<-chan Event{"A": chA, "B": chB} {
		first := mustReceive(t, ch)
		second := mustReceive(t, ch)
		if first.Type != TopicIterationScored || first.Payload != "p0" {
			t.Errorf("sub %s first event = %q/%v, want %q/p0", name, first.Type, first.Payload, TopicIterationScored)
		}
		if second.Type != TopicSessionUpdated || second.Payload != "p1" {
			t.Errorf("sub %s second event = %q/%v, want %q/p1", name, second.Type, second.Payload, TopicSessionUpdated)
		}
		if first.Seq != 0 || second.Seq != 1 {
			t.Errorf("sub %s seqs = %d,%d, want 0,1", name, first.Seq, second.Seq)
		}
		if first.TS.Location() != time.UTC || first.TS.Nanosecond() != 0 {
			t.Errorf("sub %s TS %v not whole-second UTC", name, first.TS)
		}
	}
}

func TestSeqIsPerConnection(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	chA, cancelA := b.Subscribe(context.Background())
	defer cancelA()
	b.Publish(TopicIterationScored, nil)
	mustReceive(t, chA)

	chB, cancelB := b.Subscribe(context.Background())
	defer cancelB()
	b.Publish(TopicIterationScored, nil)

	if got := mustReceive(t, chA).Seq; got != 1 {
		t.Errorf("existing connection seq = %d, want 1", got)
	}
	if got := mustReceive(t, chB).Seq; got != 0 {
		t.Errorf("new connection seq = %d, want 0 (resets per connection)", got)
	}
}

func TestOverflowDisconnectsAfterGrace(t *testing.T) {
	b := New(Options{Buffer: 1, Grace: 50 * time.Millisecond, Heartbeat: noHeartbeat})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	b.Publish(TopicIterationScored, "buffered") // fills the buffer
	b.Publish(TopicIterationScored, "dropped")  // full: stall clock starts
	time.Sleep(80 * time.Millisecond)           // exceed the grace window
	b.Publish(TopicIterationScored, "trigger")  // still full: disconnect

	if got := mustReceive(t, ch).Payload; got != "buffered" {
		t.Errorf("buffered payload = %v, want %q", got, "buffered")
	}
	mustClose(t, ch)
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after overflow-disconnect = %d, want 0", n)
	}
}

func TestOverflowWithinGraceDropsWithoutDisconnect(t *testing.T) {
	b := New(Options{Buffer: 1, Grace: time.Hour, Heartbeat: noHeartbeat})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	b.Publish(TopicIterationScored, "kept")
	b.Publish(TopicIterationScored, "lost-1") // stall starts
	b.Publish(TopicIterationScored, "lost-2") // within grace: dropped, no disconnect
	if n := b.SubscriberCount(); n != 1 {
		t.Fatalf("SubscriberCount within grace = %d, want 1", n)
	}

	if got := mustReceive(t, ch).Payload; got != "kept" {
		t.Errorf("first payload = %v, want %q", got, "kept")
	}
	b.Publish(TopicIterationScored, "after-drain")
	got := mustReceive(t, ch)
	if got.Payload != "after-drain" || got.Seq != 1 {
		t.Errorf("post-drain event = %v/seq %d, want after-drain/seq 1", got.Payload, got.Seq)
	}
}

func TestSuccessfulSendResetsStallClock(t *testing.T) {
	b := New(Options{Buffer: 1, Grace: 50 * time.Millisecond, Heartbeat: noHeartbeat})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	b.Publish(TopicIterationScored, "fill")
	b.Publish(TopicIterationScored, "drop") // stall starts
	if got := mustReceive(t, ch).Payload; got != "fill" {
		t.Fatalf("drained payload = %v, want %q", got, "fill")
	}
	time.Sleep(80 * time.Millisecond) // grace elapses, but buffer has room now
	b.Publish(TopicIterationScored, "revived")

	if got := mustReceive(t, ch).Payload; got != "revived" {
		t.Errorf("payload after drain = %v, want %q", got, "revived")
	}
	if n := b.SubscriberCount(); n != 1 {
		t.Errorf("SubscriberCount after recovery = %d, want 1", n)
	}
}

func TestContextCancellationDisconnects(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	ctx, cancelCtx := context.WithCancel(context.Background())
	ch, cancel := b.Subscribe(ctx)
	defer cancel()

	cancelCtx()
	mustClose(t, ch)
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after ctx cancel = %d, want 0", n)
	}
}

func TestCancelIsIdempotent(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())

	cancel()
	cancel() // second call must be a no-op
	mustClose(t, ch)
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after double cancel = %d, want 0", n)
	}
}

func TestSubscribeAfterCloseReturnsClosedStream(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	b.Close()

	ch, cancel := b.Subscribe(context.Background())
	mustClose(t, ch)
	cancel() // no-op cancel must not panic
}

func TestPublishAfterCloseIsCompleteNoOp(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	b.Close()
	b.Close() // idempotent

	b.Publish(TopicIterationScored, rootedPayload{root: "r1"})
	roots, all := evictor.snapshot()
	if len(roots) != 0 || all != 0 {
		t.Errorf("evictor ran after Close: roots=%v evictAll=%d, want none", roots, all)
	}
}

func TestCloseDisconnectsSubscribers(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	b.Close()
	mustClose(t, ch)
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after Close = %d, want 0", n)
	}
}

func TestHeartbeatIsEmittedAndSkipsEviction(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: 10 * time.Millisecond, Evictor: evictor})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	beat := mustReceive(t, ch)
	if beat.Type != TopicHeartbeat {
		t.Errorf("heartbeat type = %q, want %q", beat.Type, TopicHeartbeat)
	}
	if beat.Payload != struct{}{} {
		t.Errorf("heartbeat payload = %#v, want empty struct", beat.Payload)
	}
	roots, all := evictor.snapshot()
	if len(roots) != 0 || all != 0 {
		t.Errorf("heartbeat triggered eviction: roots=%v evictAll=%d, want none", roots, all)
	}
}

func TestHeartbeatDrivesOverflowDisconnectOnIdleStream(t *testing.T) {
	b := New(Options{Buffer: 1, Grace: 20 * time.Millisecond, Heartbeat: 10 * time.Millisecond})
	defer b.Close()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	// Never drain: heartbeats alone must fill the buffer, exceed the
	// grace window, and disconnect the stalled subscriber. Poll the
	// count instead of reading ch — receiving would drain the buffer
	// and defeat the stall.
	deadline := time.Now().Add(waitTimeout)
	for b.SubscriberCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("idle stalled subscriber was not overflow-disconnected")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mustClose(t, ch) // stream must be closed (drains the stranded buffer)
}

func TestEvictionPerRoot(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()

	b.Publish(TopicIterationScored, rootedPayload{root: "roots/a"})
	roots, all := evictor.snapshot()
	if len(roots) != 1 || roots[0] != "roots/a" || all != 0 {
		t.Errorf("per-root eviction: roots=%v evictAll=%d, want [roots/a] and 0", roots, all)
	}
}

func TestEvictionWholeCacheFallback(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()

	b.Publish(TopicSessionUpdated, map[string]string{"session_id": "s1"}) // not root-scoped
	b.Publish(TopicRubricChanged, rootedPayload{root: ""})                // root-scoped but empty
	roots, all := evictor.snapshot()
	if len(roots) != 0 || all != 2 {
		t.Errorf("whole-cache eviction: roots=%v evictAll=%d, want none and 2", roots, all)
	}
}

func TestPublishWithoutEvictorDoesNotPanic(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	b.Publish(TopicScoreRecomputed, rootedPayload{root: "r"})
}

func TestSubscriberCountTracksLiveSubscribers(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	if n := b.SubscriberCount(); n != 0 {
		t.Fatalf("initial SubscriberCount = %d, want 0", n)
	}
	_, cancelA := b.Subscribe(context.Background())
	_, cancelB := b.Subscribe(context.Background())
	if n := b.SubscriberCount(); n != 2 {
		t.Errorf("SubscriberCount with two subscribers = %d, want 2", n)
	}
	cancelA()
	cancelB()
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after cancels = %d, want 0", n)
	}
}

// TestConcurrentPublishSubscribeCancelUnderRace is the -race conformance
// exercise: concurrent publishers, heartbeats, draining subscribers,
// abandoning (never-draining) subscribers, mid-flight cancels, and a
// final Close must produce no data race, no deadlock, and no panic.
func TestConcurrentPublishSubscribeCancelUnderRace(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{
		Buffer:    4,
		Grace:     5 * time.Millisecond,
		Heartbeat: 2 * time.Millisecond,
		Evictor:   evictor,
	})

	const publishers = 4
	const eventsPerPublisher = 150
	const drainers = 6
	const abandoners = 3

	var wg sync.WaitGroup
	for i := 0; i < drainers; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			ctx, cancelCtx := context.WithCancel(context.Background())
			defer cancelCtx()
			ch, cancel := b.Subscribe(ctx)
			var last uint64
			for ev := range ch {
				if ev.Seq < last {
					t.Errorf("seq went backwards: %d after %d", ev.Seq, last)
					break
				}
				last = ev.Seq
				if rng.Intn(200) == 0 {
					cancel()
				}
			}
			cancel()
		}(int64(i))
	}
	for i := 0; i < abandoners; i++ {
		// Subscribe and never drain: must be overflow-disconnected
		// without wedging any publisher.
		_, cancel := b.Subscribe(context.Background())
		defer cancel()
	}
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for n := 0; n < eventsPerPublisher; n++ {
				b.Publish(TopicIterationScored, rootedPayload{root: "stress"})
			}
		}(i)
	}

	wg.Wait()
	b.Close()
	if n := b.SubscriberCount(); n != 0 {
		t.Errorf("SubscriberCount after Close = %d, want 0", n)
	}
	roots, _ := evictor.snapshot()
	if len(roots) != publishers*eventsPerPublisher {
		t.Errorf("evictions = %d, want %d (one per publish)", len(roots), publishers*eventsPerPublisher)
	}
}
