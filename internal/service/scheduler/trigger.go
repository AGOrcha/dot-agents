package scheduler

import (
	"context"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Trigger emits ticks that drive a Task's RunFn. A trigger owns its own
// goroutine lifecycle: Start spins up the source and returns a channel that
// receives one value per fire; closing the returned context (or calling the
// returned stop func) tears it down. Implementations must be safe to Start
// at most once.
//
// Triggers do not run the task themselves — the scheduler reads from the
// channel and dispatches. This keeps trigger logic (timing, file watching)
// independent of task lifecycle (panic recovery, bookkeeping, drop-on-overrun).
type Trigger interface {
	// Start begins emitting ticks until ctx is cancelled. The returned channel
	// is closed when the trigger has fully stopped (after ctx cancellation).
	// An error is returned only for setup failures (e.g. a watch path that
	// cannot be registered); a nil error means the channel is live.
	Start(ctx context.Context) (<-chan time.Time, error)
}

// IntervalTrigger fires once per Every duration. The first tick is emitted one
// interval after Start (it does not fire immediately).
type IntervalTrigger struct {
	Every time.Duration
}

// Interval is a convenience constructor for an IntervalTrigger.
func Interval(d time.Duration) *IntervalTrigger { return &IntervalTrigger{Every: d} }

// Start implements Trigger.
func (t *IntervalTrigger) Start(ctx context.Context) (<-chan time.Time, error) {
	out := make(chan time.Time)
	ticker := time.NewTicker(t.Every)
	go func() {
		defer close(out)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				// Deliver, but never block shutdown: prefer ctx cancellation.
				select {
				case out <- now:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

// fsWatcher is the subset of *fsnotify.Watcher the trigger depends on. It is an
// interface so tests can inject a fake event source without touching the real
// filesystem watcher (which is racy to drive deterministically under -race).
type fsWatcher interface {
	Add(path string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

// realWatcher adapts *fsnotify.Watcher to fsWatcher (the concrete type exposes
// Events/Errors as struct fields, not methods).
type realWatcher struct{ w *fsnotify.Watcher }

func (r *realWatcher) Add(path string) error         { return r.w.Add(path) }
func (r *realWatcher) Close() error                  { return r.w.Close() }
func (r *realWatcher) Events() <-chan fsnotify.Event { return r.w.Events }
func (r *realWatcher) Errors() <-chan error          { return r.w.Errors }

// newWatcher is a seam so tests can substitute a fake watcher. It returns a
// realWatcher wrapping a live fsnotify.Watcher by default. The fsnotify.Watcher
// is returned with its error verbatim (no extra branching) so the seam has no
// untestable conditional of its own.
var newWatcher = func() (fsWatcher, error) {
	w, err := fsnotify.NewWatcher()
	return &realWatcher{w: w}, err
}

// FSNotifyTrigger fires when any watched path emits a write/create/rename
// event. Rapid bursts are coalesced: while a debounce window is open, further
// events extend it instead of producing a tick, so a flurry of writes to one
// file yields a single fire after the burst settles.
type FSNotifyTrigger struct {
	// Paths are the files or directories to watch. At least one is required.
	Paths []string
	// Debounce is the quiet period that must elapse after the last raw event
	// before a coalesced tick is emitted. Zero defaults to 50ms.
	Debounce time.Duration
}

// FSNotify is a convenience constructor for an FSNotifyTrigger over one path.
func FSNotify(path string) *FSNotifyTrigger {
	return &FSNotifyTrigger{Paths: []string{path}}
}

const defaultDebounce = 50 * time.Millisecond

// relevantOp reports whether an fsnotify op should produce a tick. Chmod-only
// events are ignored (they don't change content); writes/creates/renames/
// removes are content-relevant for a watcher that wants to re-scan a dir.
func relevantOp(op fsnotify.Op) bool {
	return op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0
}

// debouncer coalesces a burst of raw events into a single tick. timer is nil
// when no burst is pending; arm (re)starts the quiet window and C exposes the
// fire channel for the select loop (nil when disarmed, so it never fires).
type debouncer struct {
	window time.Duration
	timer  *time.Timer
	C      <-chan time.Time
}

// arm (re)starts the debounce window. A pending timer is reset; a fresh one is
// created on the first event of a burst.
func (d *debouncer) arm() {
	if d.timer == nil {
		d.timer = time.NewTimer(d.window)
		d.C = d.timer.C
		return
	}
	if !d.timer.Stop() {
		// Drain a possibly-already-fired timer so Reset is clean.
		select {
		case <-d.timer.C:
		default:
		}
	}
	d.timer.Reset(d.window)
}

// disarm clears the pending window so C stops selecting. Called after a tick is
// emitted and on shutdown.
func (d *debouncer) disarm() {
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
		d.C = nil
	}
}

// Start implements Trigger. It registers every path on a single watcher and
// coalesces bursts using a debounce timer.
func (t *FSNotifyTrigger) Start(ctx context.Context) (<-chan time.Time, error) {
	w, err := newWatcher()
	if err != nil {
		return nil, err
	}
	for _, p := range t.Paths {
		if err := w.Add(p); err != nil {
			_ = w.Close()
			return nil, err
		}
	}
	debounce := t.Debounce
	if debounce <= 0 {
		debounce = defaultDebounce
	}

	out := make(chan time.Time)
	go t.watchLoop(ctx, w, &debouncer{window: debounce}, out)
	return out, nil
}

// watchLoop reads watcher events, coalesces them via d, and emits one tick per
// settled burst on out. It returns (closing out) when ctx is cancelled or the
// watcher's event channel closes.
func (t *FSNotifyTrigger) watchLoop(ctx context.Context, w fsWatcher, d *debouncer, out chan<- time.Time) {
	defer close(out)
	defer func() { _ = w.Close() }()
	defer d.disarm()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.Events():
			if !ok {
				return
			}
			if relevantOp(ev.Op) {
				d.arm()
			}
		case <-w.Errors():
			// Swallow watcher errors here; surfacing them is the scheduler's job
			// via task last-error, and a watcher error does not by itself mean we
			// should stop watching.
		case <-d.C:
			d.disarm()
			if !emit(ctx, out) {
				return
			}
		}
	}
}

// emit delivers a coalesced tick, preferring ctx cancellation over blocking on a
// full channel. It reports whether the loop should continue (false on cancel).
func emit(ctx context.Context, out chan<- time.Time) bool {
	select {
	case out <- time.Now():
		return true
	case <-ctx.Done():
		return false
	}
}
