package watch

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
)

const waitTimeout = 3 * time.Second

// --- fakes -------------------------------------------------------------------

// recordingPublisher captures Publish calls and signals each arrival.
type recordingPublisher struct {
	mu     sync.Mutex
	events []publishedEvent
	notify chan struct{}
}

type publishedEvent struct {
	topic   string
	payload any
}

func newRecordingPublisher() *recordingPublisher {
	return &recordingPublisher{notify: make(chan struct{}, 64)}
}

func (p *recordingPublisher) Publish(topic string, payload any) {
	p.mu.Lock()
	p.events = append(p.events, publishedEvent{topic: topic, payload: payload})
	p.mu.Unlock()
	p.notify <- struct{}{}
}

// waitFor blocks until pred matches a captured event or the timeout expires.
func (p *recordingPublisher) waitFor(t *testing.T, pred func(publishedEvent) bool) publishedEvent {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		p.mu.Lock()
		for _, ev := range p.events {
			if pred(ev) {
				p.mu.Unlock()
				return ev
			}
		}
		p.mu.Unlock()
		select {
		case <-p.notify:
		case <-deadline:
			t.Fatalf("no matching event within %v; got %+v", waitTimeout, p.snapshot())
		}
	}
}

func (p *recordingPublisher) snapshot() []publishedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]publishedEvent(nil), p.events...)
}

// fakeFSWatcher is a deterministic fsWatcher: tests push events/errors on the
// channels directly.
type fakeFSWatcher struct {
	events chan fsnotify.Event
	errs   chan error
	added  []string
	addErr error
	// addOKPrefix, when non-empty, exempts matching paths from addErr so a
	// test can mix registrable and unregistrable roots.
	addOKPrefix string
	closed      bool
	mu          sync.Mutex
}

func newFakeFSWatcher() *fakeFSWatcher {
	return &fakeFSWatcher{events: make(chan fsnotify.Event, 16), errs: make(chan error, 16)}
}

func (f *fakeFSWatcher) Add(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil && (f.addOKPrefix == "" || !strings.HasPrefix(path, f.addOKPrefix)) {
		return f.addErr
	}
	f.added = append(f.added, path)
	return nil
}

func (f *fakeFSWatcher) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
		close(f.errs)
	}
	return nil
}

func (f *fakeFSWatcher) Events() <-chan fsnotify.Event { return f.events }
func (f *fakeFSWatcher) Errors() <-chan error          { return f.errs }

// stubWatcherFactory rebinds the newWatcher seam for one test.
func stubWatcherFactory(t *testing.T, fn func() (fsWatcher, error)) {
	t.Helper()
	orig := newWatcher
	newWatcher = fn
	t.Cleanup(func() { newWatcher = orig })
}

// --- fixture helpers ------------------------------------------------------------

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func writeIterRecord(t *testing.T, dir string, n int, sid string) {
	t.Helper()
	content := fmt.Sprintf("schema_version: 2\niteration: %d\ndate: \"2026-05-01\"\ncommit: \"c%d\"\nagent:\n  session_id: %q\n  harness: h\n  model: m\n", n, n, sid)
	writeFile(t, dir, fmt.Sprintf("iter-%d.yaml", n), content)
}

func writeIterSidecar(t *testing.T, dir string, n int, band string) {
	t.Helper()
	content := fmt.Sprintf("iteration: %d\nrubric_version: \"2.1.0\"\nscored: true\nvalue: 0.8\nband: %q\nbreakdown: []\n", n, band)
	writeFile(t, dir, fmt.Sprintf("iter-%d.score.yaml", n), content)
}

// startWatcher builds and starts a Watcher wired to a fake fs source, with
// the poll fallback disabled so each test drives exactly one source.
func startWatcher(t *testing.T, root string, pub events.Publisher, opts ...Option) (*Watcher, *fakeFSWatcher) {
	t.Helper()
	fake := newFakeFSWatcher()
	stubWatcherFactory(t, func() (fsWatcher, error) { return fake, nil })
	w := New([]string{root}, pub, append([]Option{WithPollInterval(-1)}, opts...)...)
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Close)
	return w, fake
}

func fsEvent(root, name string, op fsnotify.Op) fsnotify.Event {
	return fsnotify.Event{Name: filepath.Join(root, name), Op: op}
}

// --- fsnotify path ----------------------------------------------------------------

// A new iter-N.score.yaml publishes iteration.scored with session_id resolved
// from the adjacent record, band from the sidecar itself, and the payload
// reporting its iter-log root for the broker's per-root eviction.
func TestFSEventNewScoreSidecarPublishesIterationScored(t *testing.T) {
	root := t.TempDir()
	writeIterRecord(t, root, 2, "sess-a")
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	writeIterSidecar(t, root, 2, "good")
	fake.events <- fsEvent(root, "iter-2.score.yaml", fsnotify.Create)

	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicIterationScored })
	payload, ok := ev.payload.(IterationScored)
	if !ok {
		t.Fatalf("payload type %T, want IterationScored", ev.payload)
	}
	if payload.SessionID != "sess-a" || payload.Iteration != 2 || payload.Band != "good" {
		t.Errorf("payload = %+v", payload)
	}
	if payload.IterLogRoot() != root {
		t.Errorf("IterLogRoot() = %q, want %q (per-root eviction contract)", payload.IterLogRoot(), root)
	}
}

// Rewriting a sidecar the watcher has already seen publishes score.recomputed
// (an existing score changed in place), not iteration.scored.
func TestFSEventRewrittenSidecarPublishesScoreRecomputed(t *testing.T) {
	root := t.TempDir()
	writeIterRecord(t, root, 3, "sess-b")
	writeIterSidecar(t, root, 3, "fair") // pre-existing: baselined, not published
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	// Rewrite with a bumped mtime so the dedupe table sees a change.
	writeIterSidecar(t, root, 3, "excellent")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "iter-3.score.yaml"), future, future); err != nil {
		t.Fatal(err)
	}
	fake.events <- fsEvent(root, "iter-3.score.yaml", fsnotify.Write)

	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicScoreRecomputed })
	if p := ev.payload.(IterationScored); p.Band != "excellent" || p.Iteration != 3 {
		t.Errorf("payload = %+v", p)
	}
}

// A session sidecar change publishes session.updated keyed by the session id
// embedded in the filename.
func TestFSEventSessionSidecarPublishesSessionUpdated(t *testing.T) {
	root := t.TempDir()
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	writeFile(t, root, "session-sess-z.score.yaml", "session_id: sess-z\nscored: true\n")
	fake.events <- fsEvent(root, "session-sess-z.score.yaml", fsnotify.Create)

	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
	payload := ev.payload.(SessionUpdated)
	if payload.SessionID != "sess-z" || payload.IterLogRoot() != root {
		t.Errorf("payload = %+v root=%q", payload, payload.IterLogRoot())
	}
}

// A bare iter-N.yaml record change (no sidecar yet) publishes
// iteration.scored with the record's session id and no band.
func TestFSEventBareIterRecordPublishes(t *testing.T) {
	root := t.TempDir()
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	writeIterRecord(t, root, 7, "sess-r")
	fake.events <- fsEvent(root, "iter-7.yaml", fsnotify.Create)

	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicIterationScored })
	if p := ev.payload.(IterationScored); p.SessionID != "sess-r" || p.Iteration != 7 || p.Band != "" {
		t.Errorf("payload = %+v", p)
	}
}

// Unwatched filenames (atomic-write temp files), chmod-only ops, and events
// for files deleted before stat are all dropped without publishing.
func TestFSEventIgnoresIrrelevantChanges(t *testing.T) {
	root := t.TempDir()
	writeIterRecord(t, root, 1, "sess-i")
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	writeFile(t, root, ".iter-9.yaml.tmp", "temp")
	fake.events <- fsEvent(root, ".iter-9.yaml.tmp", fsnotify.Create)        // unwatched name
	fake.events <- fsEvent(root, "iter-1.yaml", fsnotify.Chmod)              // irrelevant op
	fake.events <- fsEvent(root, "iter-8.yaml", fsnotify.Create)             // vanished before stat
	fake.events <- fsEvent(root, "session-x.score.yaml", fsnotify.Remove)    // removal: forget only
	writeFile(t, root, "session-real.score.yaml", "session_id: real\n")      // sentinel
	fake.events <- fsEvent(root, "session-real.score.yaml", fsnotify.Create) // published

	pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
	for _, ev := range pub.snapshot() {
		if ev.topic != events.TopicSessionUpdated {
			t.Errorf("unexpected event published: %+v", ev)
		}
	}
}

// Removal forgets the dedupe entry so a re-created file (same mtime edge
// aside) publishes again.
func TestFSEventRemoveThenRecreatePublishesAgain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "session-s.score.yaml", "session_id: s\n") // baselined
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	fake.events <- fsEvent(root, "session-s.score.yaml", fsnotify.Remove)
	fake.events <- fsEvent(root, "session-s.score.yaml", fsnotify.Create)

	pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
}

// An unchanged mtime is the "whichever fires first wins" dedupe: the second
// source's notification for the same change publishes nothing.
func TestDuplicateNotificationIsDeduped(t *testing.T) {
	root := t.TempDir()
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub)

	writeFile(t, root, "session-d.score.yaml", "session_id: d\n")
	fake.events <- fsEvent(root, "session-d.score.yaml", fsnotify.Create)
	pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })

	fake.events <- fsEvent(root, "session-d.score.yaml", fsnotify.Write) // same mtime
	fake.events <- fsEvent(root, "session-d.score.yaml", fsnotify.Create)
	// A sentinel event is processed strictly after the duplicates (single
	// event channel), so once it lands the dedupe verdict is final.
	writeFile(t, root, "iter-1.yaml", "iteration: 1\n")
	fake.events <- fsEvent(root, "iter-1.yaml", fsnotify.Create)
	pub.waitFor(t, func(e publishedEvent) bool {
		p, ok := e.payload.(IterationScored)
		return ok && p.Iteration == 1
	})
	if got := len(pub.snapshot()); got != 2 {
		t.Errorf("events published = %d, want 2 (session + sentinel; duplicates deduped)", got)
	}
}

// Watcher errors are logged and the loop keeps serving events.
func TestFSErrorIsLoggedAndLoopContinues(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub, WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	fake.errs <- errors.New("transient watch error")
	writeFile(t, root, "session-e.score.yaml", "session_id: e\n")
	fake.events <- fsEvent(root, "session-e.score.yaml", fsnotify.Create)

	pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
	if !strings.Contains(buf.String(), "fsnotify error") {
		t.Errorf("watcher error should be logged, got: %s", buf.String())
	}
}

// Corrupt neighbours degrade the payload, never suppress the event: an
// unparseable record yields an empty session_id (logged), a corrupt sidecar
// an empty band.
func TestPublishBestEffortPayloadOnCorruptNeighbours(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "iter-4.yaml", "not: [valid")
	var buf bytes.Buffer
	pub := newRecordingPublisher()
	_, fake := startWatcher(t, root, pub, WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))

	writeFile(t, root, "iter-4.score.yaml", "also: [corrupt")
	fake.events <- fsEvent(root, "iter-4.score.yaml", fsnotify.Create)

	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicIterationScored })
	if p := ev.payload.(IterationScored); p.SessionID != "" || p.Band != "" || p.Iteration != 4 {
		t.Errorf("payload = %+v", p)
	}
	if !strings.Contains(buf.String(), "unparseable iter record") {
		t.Errorf("unparseable record should be logged, got: %s", buf.String())
	}
}

// --- poll fallback (OQ3) -------------------------------------------------------------

// With the fsnotify source silent, the 1-second-class mtime poll alone
// detects a new file — the OQ3 belt-and-suspenders fallback.
func TestPollFallbackDetectsNewFile(t *testing.T) {
	root := t.TempDir()
	writeIterRecord(t, root, 1, "sess-p") // baselined
	fake := newFakeFSWatcher()            // never emits
	stubWatcherFactory(t, func() (fsWatcher, error) { return fake, nil })
	pub := newRecordingPublisher()
	w := New([]string{root}, pub, WithPollInterval(5*time.Millisecond))
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Close)

	writeIterSidecar(t, root, 1, "good")
	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicIterationScored })
	if p := ev.payload.(IterationScored); p.SessionID != "sess-p" || p.Band != "good" {
		t.Errorf("payload = %+v", p)
	}

	// Deletion is forgotten by the poll so a re-create publishes again.
	if err := os.Remove(filepath.Join(root, "iter-1.score.yaml")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond) // let a tick observe the deletion
	writeIterSidecar(t, root, 1, "fair")
	pub.waitFor(t, func(e publishedEvent) bool {
		p, ok := e.payload.(IterationScored)
		return ok && p.Band == "fair"
	})
}

// A failed fsnotify constructor degrades to poll-only (warning, nil error)
// when the poll is enabled, and fails Start when it is not.
func TestStartFSNotifyFailureDegradesToPollOnly(t *testing.T) {
	stubWatcherFactory(t, func() (fsWatcher, error) { return nil, errors.New("no kqueue") })
	root := t.TempDir()
	var buf bytes.Buffer
	pub := newRecordingPublisher()

	w := New([]string{root}, pub,
		WithPollInterval(5*time.Millisecond),
		WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	if err := w.Start(); err != nil {
		t.Fatalf("Start should degrade to poll-only, got %v", err)
	}
	t.Cleanup(w.Close)
	if !strings.Contains(buf.String(), "fsnotify unavailable") {
		t.Errorf("degradation should be logged, got: %s", buf.String())
	}
	writeFile(t, root, "session-poll.score.yaml", "session_id: poll\n")
	pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })

	dead := New([]string{root}, pub, WithPollInterval(-1))
	if err := dead.Start(); err == nil {
		t.Error("Start with no working source must error")
	}
}

// An unregistrable root is a warning, not a Start failure — the poll covers
// the directory once it exists.
func TestStartUnregistrableRootWarnsAndContinues(t *testing.T) {
	fake := newFakeFSWatcher()
	fake.addErr = errors.New("no such directory")
	okRoot := t.TempDir()
	fake.addOKPrefix = okRoot // second root registers fine
	stubWatcherFactory(t, func() (fsWatcher, error) { return fake, nil })
	var buf bytes.Buffer

	// One root unregistrable, one registered: fsnotify is still an active
	// source, so Start succeeds even with the poll disabled.
	w := New([]string{filepath.Join(t.TempDir(), "missing"), okRoot}, newRecordingPublisher(),
		WithPollInterval(-1),
		WithLogger(slog.New(slog.NewTextHandler(&buf, nil))))
	if err := w.Start(); err != nil {
		t.Fatalf("Start with one registered root must succeed: %v", err)
	}
	w.Close()
	if !strings.Contains(buf.String(), "cannot watch root") {
		t.Errorf("Add failure should be logged, got: %s", buf.String())
	}
}

// A watcher with ZERO active sources — every fsnotify Add failed AND the
// poll fallback disabled — must error out of Start, not report a healthy
// dead bridge. The wrapped Add error stays inspectable.
func TestStartAllRootsUnregistrablePollDisabledErrors(t *testing.T) {
	fake := newFakeFSWatcher()
	addErr := errors.New("no such directory")
	fake.addErr = addErr
	stubWatcherFactory(t, func() (fsWatcher, error) { return fake, nil })

	w := New([]string{filepath.Join(t.TempDir(), "missing")}, newRecordingPublisher(),
		WithPollInterval(-1))
	err := w.Start()
	if err == nil {
		t.Fatal("Start with all Adds failed + poll disabled must error")
	}
	if !errors.Is(err, addErr) {
		t.Errorf("underlying Add error must be wrapped, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no active watch source") {
		t.Errorf("error should name the dead-source condition, got: %v", err)
	}
	w.Close()
}

// Every Add failing is survivable when the poll fallback is enabled: Start
// succeeds and the poll alone carries changes to the broker.
func TestStartAllRootsUnregistrablePollEnabledSucceeds(t *testing.T) {
	root := t.TempDir()
	fake := newFakeFSWatcher()
	fake.addErr = errors.New("no such directory")
	stubWatcherFactory(t, func() (fsWatcher, error) { return fake, nil })
	pub := newRecordingPublisher()

	w := New([]string{root}, pub, WithPollInterval(5*time.Millisecond))
	if err := w.Start(); err != nil {
		t.Fatalf("Start must degrade to poll-only when the poll is enabled: %v", err)
	}
	t.Cleanup(w.Close)

	writeFile(t, root, "session-carried.score.yaml", "session_id: carried\n")
	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
	if p := ev.payload.(SessionUpdated); p.SessionID != "carried" {
		t.Errorf("payload = %+v", p)
	}
}

// Close is idempotent and drains both sources.
func TestCloseIsIdempotent(t *testing.T) {
	root := t.TempDir()
	w, _ := startWatcher(t, root, newRecordingPublisher())
	w.Close()
	w.Close()
}

// --- real fsnotify integration --------------------------------------------------------

// One end-to-end pass over the real fsnotify implementation: a session
// sidecar written after Start reaches the publisher via kqueue/inotify (or,
// belt-and-suspenders, the enabled poll — exactly the OQ3 production shape).
func TestRealFSNotifyIntegration(t *testing.T) {
	root := t.TempDir()
	pub := newRecordingPublisher()
	w := New([]string{root}, pub, WithPollInterval(500*time.Millisecond))
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Close)

	writeFile(t, root, "session-live.score.yaml", "session_id: live\n")
	ev := pub.waitFor(t, func(e publishedEvent) bool { return e.topic == events.TopicSessionUpdated })
	if p := ev.payload.(SessionUpdated); p.SessionID != "live" {
		t.Errorf("payload = %+v", p)
	}
}
