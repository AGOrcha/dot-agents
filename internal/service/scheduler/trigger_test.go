package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// drain reads a trigger channel to completion. Used after context cancel to
// confirm the channel closes (and to avoid leaving an unread, soon-to-close
// channel) without an empty for-range block.
func drain(ch <-chan time.Time) {
	for range ch {
		continue
	}
}

func TestIntervalTriggerFires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := Interval(5 * time.Millisecond).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("interval tick %d did not arrive", i)
		}
	}
}

func TestIntervalTriggerStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Interval(time.Millisecond).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	// Channel must close after cancellation. Drain any in-flight tick first.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // closed — success
			}
		case <-deadline:
			t.Fatal("interval channel did not close after context cancel")
		}
	}
}

// TestIntervalTriggerCancelDuringSend exercises the ctx-cancel branch of the
// delivery select: a tick is ready but no consumer reads, then ctx is cancelled.
func TestIntervalTriggerCancelDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Interval(5 * time.Millisecond).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the ticker time to produce a tick that blocks on the unread out chan.
	time.Sleep(30 * time.Millisecond)
	cancel()
	// Drain until closed; the goroutine must exit via the ctx.Done branch.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("interval goroutine did not exit after cancel during send")
		}
	}
}

// TestFSNotifyTriggerCancelDuringSend exercises the ctx-cancel branch of the
// FSNotify delivery select: the debounce timer fires producing a pending tick,
// but no consumer reads, then ctx is cancelled.
func TestFSNotifyTriggerCancelDuringSend(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: 10 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Write}
	// Let the debounce elapse so a tick is pending on the unread out channel.
	time.Sleep(40 * time.Millisecond)
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("fsnotify goroutine did not exit after cancel during send")
		}
	}
}

// fakeWatcher is an injectable fsWatcher whose event/error streams the test
// drives directly, making fsnotify trigger tests deterministic under -race.
type fakeWatcher struct {
	mu     sync.Mutex
	added  []string
	events chan fsnotify.Event
	errs   chan error
	closed bool
	addErr error
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{events: make(chan fsnotify.Event, 16), errs: make(chan error, 4)}
}

func (f *fakeWatcher) Add(p string) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.mu.Lock()
	f.added = append(f.added, p)
	f.mu.Unlock()
	return nil
}
func (f *fakeWatcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}
func (f *fakeWatcher) Events() <-chan fsnotify.Event { return f.events }
func (f *fakeWatcher) Errors() <-chan error          { return f.errs }

// withFakeWatcher swaps the package newWatcher seam for the duration of a test.
func withFakeWatcher(t *testing.T, fw *fakeWatcher) {
	t.Helper()
	orig := newWatcher
	newWatcher = func() (fsWatcher, error) { return fw, nil }
	t.Cleanup(func() { newWatcher = orig })
}

func TestFSNotifyTriggerCoalescesBurst(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := &FSNotifyTrigger{Paths: []string{"/tmp/x"}, Debounce: 20 * time.Millisecond}
	ch, err := tr.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Fire a rapid burst of relevant events within the debounce window.
	for i := 0; i < 5; i++ {
		fw.events <- fsnotify.Event{Name: "/tmp/x/f.yaml", Op: fsnotify.Write}
	}
	// Exactly one coalesced tick should arrive.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no coalesced tick after burst")
	}
	// No further tick should appear from the same burst.
	select {
	case <-ch:
		t.Fatal("burst produced more than one tick")
	case <-time.After(60 * time.Millisecond):
	}
}

func TestFSNotifyTriggerSeparateBurstsFireSeparately(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: 15 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	for b := 0; b < 2; b++ {
		fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Create}
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("burst %d produced no tick", b)
		}
	}
}

func TestFSNotifyTriggerIgnoresChmod(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: 15 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Chmod}
	select {
	case <-ch:
		t.Fatal("chmod-only event should not produce a tick")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFSNotifyTriggerSwallowsWatcherErrors(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: 15 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	fw.errs <- errors.New("watcher hiccup")
	// An error must not produce a tick, and a subsequent real event still works.
	fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Write}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("trigger stopped working after a watcher error")
	}
}

func TestFSNotifyTriggerStopsOnContextCancel(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			drain(ch)
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close on context cancel")
	}
}

func TestFSNotifyTriggerClosedWatcherChannel(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	fw.Close() // closes the events channel
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected out channel to close when watcher events close")
		}
	case <-time.After(time.Second):
		t.Fatal("out channel did not close when watcher events closed")
	}
}

func TestFSNotifyTriggerAddError(t *testing.T) {
	fw := newFakeWatcher()
	fw.addErr = errors.New("no such path")
	withFakeWatcher(t, fw)
	_, err := (&FSNotifyTrigger{Paths: []string{"/nope"}}).Start(context.Background())
	if err == nil {
		t.Fatal("Start should fail when watcher.Add errors")
	}
}

func TestFSNotifyTriggerNewWatcherError(t *testing.T) {
	orig := newWatcher
	newWatcher = func() (fsWatcher, error) { return nil, errors.New("no watcher") }
	t.Cleanup(func() { newWatcher = orig })
	_, err := FSNotify("/d").Start(context.Background())
	if err == nil {
		t.Fatal("Start should fail when newWatcher errors")
	}
}

// TestFSNotifyTriggerRealFilesystem exercises the production newWatcher /
// realWatcher adapter against a real directory, so the adapter code is covered
// (not just the fake-watcher path).
func TestFSNotifyTriggerRealFilesystem(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{dir}, Debounce: 30 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Write a file; expect a coalesced tick.
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no tick from real filesystem write")
	}
}

// TestFSNotifyTriggerReArmDuringBurst drives events with gaps comparable to the
// debounce window so the internal timer is re-armed (timer.Stop / Reset path)
// rather than freshly created, and the burst still coalesces to one tick.
func TestFSNotifyTriggerReArmDuringBurst(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: 40 * time.Millisecond}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Send several events with sub-debounce spacing so each re-arms the timer.
	for i := 0; i < 6; i++ {
		fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Write}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("no coalesced tick after re-armed burst")
	}
	// And only one tick.
	select {
	case <-ch:
		t.Fatal("re-armed burst produced more than one tick")
	case <-time.After(80 * time.Millisecond):
	}
}

// TestFSNotifyTriggerCancelWithPendingTimer cancels the context while a debounce
// timer is armed, exercising the stopTimer cleanup path on shutdown.
func TestFSNotifyTriggerCancelWithPendingTimer(t *testing.T) {
	fw := newFakeWatcher()
	withFakeWatcher(t, fw)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := (&FSNotifyTrigger{Paths: []string{"/d"}, Debounce: time.Second}).Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Arm the timer, then cancel before it can fire.
	fw.events <- fsnotify.Event{Name: "/d/a", Op: fsnotify.Write}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			drain(ch)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after cancel with pending timer")
	}
}

func TestFSNotifyConstructor(t *testing.T) {
	tr := FSNotify("/some/path")
	if len(tr.Paths) != 1 || tr.Paths[0] != "/some/path" {
		t.Fatalf("FSNotify constructor wrong: %+v", tr)
	}
}
