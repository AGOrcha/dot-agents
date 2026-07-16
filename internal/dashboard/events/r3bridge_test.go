// r3bridge_test.go proves AttachR3Bus TRANSLATES the R3 background-worker
// service's events (internal/service/events taxonomy) into the dashboard's
// API.md §3.7 taxonomy — topic remap plus schema-shaped payload — while
// keeping the eviction, fan-out, timestamp, and lifecycle contracts the
// generic AttachBus bridge holds. It reuses this package's fakeBus /
// recordingEvictor / mustReceive helpers (bridge_test.go, broker_test.go).
package events

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
)

func TestAttachR3BusTranslatesIterationScored(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	var gotDir string
	var gotIter int
	detach, err := b.AttachR3Bus(bus, WithR3SessionResolver(func(dir string, n int) string {
		gotDir, gotIter = dir, n
		return "sess-9"
	}))
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	if err := bus.Publish(svcevents.TopicIterationScored, svcevents.IterationScored{
		Iteration:   3,
		Score:       0.82,
		Band:        "good",
		SidecarPath: "roots/x/iter-3.score.yaml",
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	ev := mustReceive(t, ch)
	if ev.Type != TopicIterationScored {
		t.Errorf("translated type = %q, want %q", ev.Type, TopicIterationScored)
	}
	got, ok := ev.Payload.(r3IterationScored)
	if !ok {
		t.Fatalf("payload = %#v, want r3IterationScored", ev.Payload)
	}
	if got.SessionID != "sess-9" || got.Iteration != 3 || got.Band != "good" {
		t.Errorf("payload = %+v, want {SessionID:sess-9 Iteration:3 Band:good}", got)
	}
	if gotDir != "roots/x" || gotIter != 3 {
		t.Errorf("resolver args = (%q, %d), want (roots/x, 3)", gotDir, gotIter)
	}
	// Per-root eviction keyed on the sidecar's directory.
	roots, all := evictor.snapshot()
	if len(roots) != 1 || roots[0] != "roots/x" || all != 0 {
		t.Errorf("eviction: roots=%v evictAll=%d, want [roots/x] and 0", roots, all)
	}
}

// TestAttachR3BusRootKeyIsForwardSlash pins the OS-independence invariant that
// regressed on Windows CI: the logical iteration.scored root — fed to BOTH the
// session resolver and the per-root cache evictor — must be forward-slash on
// every OS. filepath.Dir cleans a slash-style sidecar to the backslash OS-sep
// on Windows; without filepath.ToSlash the resolver arg and eviction key carry
// a backslash there, so the evict key stops matching the store's normalized
// cache key and per-root eviction silently no-ops. The assertion holds on all
// platforms (a no-op on POSIX, a real guard on Windows).
func TestAttachR3BusRootKeyIsForwardSlash(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	var gotDir string
	detach, err := b.AttachR3Bus(bus, WithR3SessionResolver(func(dir string, _ int) string {
		gotDir = dir
		return ""
	}))
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	if err := bus.Publish(svcevents.TopicIterationScored, svcevents.IterationScored{
		Iteration:   7,
		SidecarPath: "roots/nested/dir/iter-7.score.yaml",
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}
	mustReceive(t, ch)

	if strings.ContainsRune(gotDir, '\\') {
		t.Errorf("resolver root %q contains an OS separator; want forward-slash only", gotDir)
	}
	roots, _ := evictor.snapshot()
	if len(roots) != 1 {
		t.Fatalf("eviction roots = %v, want exactly one", roots)
	}
	if strings.ContainsRune(roots[0], '\\') {
		t.Errorf("eviction root %q contains an OS separator; want forward-slash only", roots[0])
	}
	if roots[0] != gotDir {
		t.Errorf("resolver root %q and eviction root %q disagree; keys must align", gotDir, roots[0])
	}
}

func TestAttachR3BusTranslatesRescoreDone(t *testing.T) {
	evictor := &recordingEvictor{}
	b := New(Options{Heartbeat: noHeartbeat, Evictor: evictor})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachR3Bus(bus)
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	if err := bus.Publish(svcevents.TopicRescoreDone, svcevents.RescoreDone{
		FromVersion: "1.0.0",
		ToVersion:   "2.0.0",
		IterCount:   7,
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	ev := mustReceive(t, ch)
	if ev.Type != TopicRubricChanged {
		t.Errorf("translated type = %q, want %q", ev.Type, TopicRubricChanged)
	}
	got, ok := ev.Payload.(r3RubricChanged)
	if !ok {
		t.Fatalf("payload = %#v, want r3RubricChanged", ev.Payload)
	}
	if got.RubricVersion != "2.0.0" {
		t.Errorf("payload = %+v, want {RubricVersion:2.0.0}", got)
	}
	// A rubric bump rescores every root → whole-cache eviction.
	roots, all := evictor.snapshot()
	if len(roots) != 0 || all != 1 {
		t.Errorf("eviction: roots=%v evictAll=%d, want none and 1", roots, all)
	}
}

func TestAttachR3BusDefaultResolverYieldsEmptySession(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachR3Bus(bus) // no resolver option
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	_ = bus.Publish(svcevents.TopicIterationScored, svcevents.IterationScored{
		Iteration:   1,
		SidecarPath: "roots/y/iter-1.score.yaml",
	})
	got, ok := mustReceive(t, ch).Payload.(r3IterationScored)
	if !ok {
		t.Fatalf("payload not r3IterationScored")
	}
	if got.SessionID != "" {
		t.Errorf("default-resolver session_id = %q, want empty", got.SessionID)
	}
}

func TestAttachR3BusPreservesBusTimestamp(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := newFakeBus()

	detach, err := b.AttachR3Bus(bus)
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	stamp := time.Date(2026, 5, 28, 14, 3, 11, 987654321, time.FixedZone("PDT", -7*3600))
	bus.inject(svcevents.Event{
		Topic:     svcevents.TopicRescoreDone,
		Timestamp: stamp,
		Payload:   svcevents.RescoreDone{ToVersion: "3.0.0"},
	})

	ev := mustReceive(t, ch)
	want := stamp.UTC().Truncate(time.Second)
	if !ev.TS.Equal(want) || ev.TS.Location() != time.UTC || ev.TS.Nanosecond() != 0 {
		t.Errorf("bridged TS = %v, want %v (whole-second UTC)", ev.TS, want)
	}
}

func TestAttachR3BusDropsUnexpectedPayload(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachR3Bus(bus)
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	defer detach()
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	// A bridged topic carrying an off-contract payload is dropped, never
	// forwarded off-schema.
	_ = bus.Publish(svcevents.TopicIterationScored, "not-a-payload")
	mustStaySilent(t, ch)
}

func TestAttachR3BusDetachStopsForwardingAndIsIdempotent(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	detach, err := b.AttachR3Bus(bus)
	if err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	ch, cancel := b.Subscribe(context.Background())
	defer cancel()

	detach()
	detach() // idempotent
	_ = bus.Publish(svcevents.TopicRescoreDone, svcevents.RescoreDone{ToVersion: "9.9.9"})
	mustStaySilent(t, ch)
}

func TestAttachR3BusOnClosedBrokerReturnsErrClosed(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	b.Close()
	bus := newFakeBus()

	if _, err := b.AttachR3Bus(bus); !errors.Is(err, ErrClosed) {
		t.Fatalf("AttachR3Bus on closed broker: err = %v, want ErrClosed", err)
	}
	// Every subscription made during the aborted attach is unwound.
	unsubbed := bus.unsubscribed()
	if len(unsubbed) != len(r3BridgedTopics) {
		t.Errorf("unwound subscriptions = %v, want all %v", unsubbed, r3BridgedTopics)
	}
}

func TestAttachR3BusSubscribeErrorUnwindsPartialAttach(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	defer b.Close()
	bus := newFakeBus()
	bus.failTopic = svcevents.TopicRescoreDone

	if _, err := b.AttachR3Bus(bus); err == nil {
		t.Fatalf("AttachR3Bus with a refusing bus: want error, got nil")
	}
	// iteration.scored subscribed first, then rescore.done refused → the
	// first subscription is unwound so no goroutine leaks.
	unsubbed := bus.unsubscribed()
	if len(unsubbed) != 1 || unsubbed[0] != svcevents.TopicIterationScored {
		t.Errorf("unwound subscriptions = %v, want [%s]", unsubbed, svcevents.TopicIterationScored)
	}
}

func TestCloseDetachesAttachedR3Bus(t *testing.T) {
	b := New(Options{Heartbeat: noHeartbeat})
	bus := svcevents.NewInProcBus()
	defer func() { _ = bus.Close() }()

	if _, err := b.AttachR3Bus(bus); err != nil {
		t.Fatalf("AttachR3Bus: %v", err)
	}
	// Close must run the detach itself; otherwise wg.Wait deadlocks on the
	// still-open forward goroutines and this test times out.
	b.Close()
	if err := bus.Publish(svcevents.TopicIterationScored, svcevents.IterationScored{Iteration: 1}); err != nil {
		t.Errorf("bus.Publish after broker Close: %v, want nil (bus unaffected)", err)
	}
}
