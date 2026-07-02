// Package watch is the t06 iter-log filesystem watcher → SSE-broker bridge
// (plan task t06-recompute-on-miss-and-fswatch): the v1 standalone push path
// that exists BEFORE R3's publish primitive lands.
//
// It watches the configured iter-log roots for iteration-log and score-sidecar
// changes and publishes the API.md §3.7 dashboard events to the t04 broker's
// Publisher seam. Every payload implements events.IterLogRooter (carrying the
// root in an unexported, non-marshaled field, since the event schema pins the
// payload key set) so the broker's per-root cache eviction fires instead of
// the coarser whole-cache fallback.
//
// Watch semantics resolve spec OQ3 as the plan pinned it: fsnotify is the
// primary source AND a 1-second mtime poll runs alongside it as a
// belt-and-suspenders fallback — whichever fires first wins. Both paths feed
// one per-file (path, mtime) dedupe table, so a change reported by fsnotify is
// not re-published by the next poll tick and vice versa.
//
// When R3's publisher lands (t13) this watcher demotes to fallback only: R3's
// in-process publish becomes the primary path, and the watcher remains for
// writers that do not go through R3 (e.g. a developer running `da score run`
// manually).
package watch

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// DefaultPollInterval is the OQ3 mtime-poll fallback cadence: 1 second, so
// the poll alone still lands a push well inside the spec's 2-second budget
// even when FSEvents latency eats the fsnotify margin.
const DefaultPollInterval = time.Second

// Filename patterns for the three watched artifact families. The record
// pattern deliberately excludes the *.score.yaml sidecars sharing the iter-
// prefix (same shapes as the t02 store's patterns).
var (
	iterRecordRE = regexp.MustCompile(`^iter-(\d+)\.yaml$`)
	iterScoreRE  = regexp.MustCompile(`^iter-(\d+)\.score\.yaml$`)
	sessionScrRE = regexp.MustCompile(`^session-(.+)\.score\.yaml$`)
)

// IterationScored is the iteration.scored / score.recomputed payload
// (dashboard-event schema: session_id + iteration + optional band). It
// reports its iter-log root via IterLogRoot for per-root cache eviction
// without leaking the root into the marshaled payload.
type IterationScored struct {
	SessionID string `json:"session_id"`
	Iteration int    `json:"iteration"`
	Band      string `json:"band,omitempty"`

	root string
}

// IterLogRoot implements events.IterLogRooter.
func (p IterationScored) IterLogRoot() string { return p.root }

// SessionUpdated is the session.updated payload (dashboard-event schema:
// session_id only), likewise root-scoped for per-root eviction.
type SessionUpdated struct {
	SessionID string `json:"session_id"`

	root string
}

// IterLogRoot implements events.IterLogRooter.
func (p SessionUpdated) IterLogRoot() string { return p.root }

// Compile-time proof the payloads drive the broker's per-root eviction.
var (
	_ events.IterLogRooter = IterationScored{}
	_ events.IterLogRooter = SessionUpdated{}
)

// fsWatcher is the subset of *fsnotify.Watcher the bridge depends on — an
// interface so tests can inject a deterministic fake event source (same seam
// shape as internal/service/scheduler's trigger).
type fsWatcher interface {
	Add(path string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
}

// realWatcher adapts *fsnotify.Watcher to fsWatcher (the concrete type
// exposes Events/Errors as struct fields, not methods).
type realWatcher struct{ w *fsnotify.Watcher }

func (r *realWatcher) Add(path string) error         { return r.w.Add(path) }
func (r *realWatcher) Close() error                  { return r.w.Close() }
func (r *realWatcher) Events() <-chan fsnotify.Event { return r.w.Events }
func (r *realWatcher) Errors() <-chan error          { return r.w.Errors }

// newWatcher is the constructor seam for the fsnotify source; tests
// substitute a fake. Returned with its error verbatim so the seam carries no
// untestable branch of its own.
var newWatcher = func() (fsWatcher, error) {
	w, err := fsnotify.NewWatcher()
	return &realWatcher{w: w}, err
}

// Option configures a Watcher.
type Option func(*Watcher)

// WithLogger sets the structured logger used for skipped-file and watch
// warnings.
func WithLogger(l *slog.Logger) Option {
	return func(w *Watcher) {
		if l != nil {
			w.logger = l
		}
	}
}

// WithPollInterval overrides the OQ3 poll-fallback cadence. Zero keeps
// DefaultPollInterval; negative disables the poll (fsnotify-only — tests and
// callers that want deterministic single-source behavior).
func WithPollInterval(d time.Duration) Option {
	return func(w *Watcher) {
		if d != 0 {
			w.poll = d
		}
	}
}

// Watcher bridges filesystem changes under the iter-log roots to broker
// events. Construct with New, start with Start, stop with Close. Safe for
// the concurrent fsnotify + poll sources it runs internally.
type Watcher struct {
	roots  []string
	pub    events.Publisher
	logger *slog.Logger
	poll   time.Duration

	// mu guards seen: absolute file path → last published mtime. Both event
	// sources consult it, which is what makes "whichever fires first wins"
	// dedupe instead of double-publish.
	mu   sync.Mutex
	seen map[string]time.Time

	fsw       fsWatcher
	stop      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// New builds a Watcher over the given iter-log roots, publishing to pub
// (the t04 broker's Publisher seam).
func New(roots []string, pub events.Publisher, opts ...Option) *Watcher {
	w := &Watcher{
		roots:  append([]string(nil), roots...),
		pub:    pub,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		poll:   DefaultPollInterval,
		seen:   map[string]time.Time{},
		stop:   make(chan struct{}),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Start seeds the baseline (pre-existing files publish nothing), registers
// the fsnotify watches, and launches the fsnotify and poll sources. An
// inactive fsnotify source — the constructor erroring OR every root failing
// to register — degrades to poll-only with a warning; it is an error only
// when the poll fallback is disabled too, because a watcher with zero
// active sources is dead and must not report healthy.
func (w *Watcher) Start() error {
	for _, root := range w.roots {
		w.seedBaseline(root)
	}
	if err := w.startFSNotify(); err != nil && w.poll < 0 {
		return fmt.Errorf("dashboard/watch: no active watch source (poll fallback disabled): %w", err)
	}
	if w.poll > 0 {
		w.wg.Add(1)
		go w.pollLoop()
	}
	return nil
}

// Close stops both sources and waits for their goroutines. Idempotent.
func (w *Watcher) Close() {
	w.closeOnce.Do(func() {
		close(w.stop)
		if w.fsw != nil {
			_ = w.fsw.Close()
		}
	})
	w.wg.Wait()
}

// seedBaseline records every already-present watched file's mtime so startup
// never replays history as fresh events.
func (w *Watcher) seedBaseline(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() || !watchedFile(e.Name()) {
			continue
		}
		w.seen[filepath.Join(root, e.Name())] = info.ModTime()
	}
}

// startFSNotify builds the fsnotify source and registers every root. A
// single unregistrable root (e.g. not yet created) is a warning, not a
// failure — the poll fallback picks the directory up once it exists. The
// returned error reports an INACTIVE source: the constructor failed, or not
// one root registered (all Add errors wrapped); Start decides whether the
// poll fallback makes that survivable.
func (w *Watcher) startFSNotify() error {
	fsw, err := newWatcher()
	if err != nil {
		w.logger.Warn("dashboard/watch: fsnotify unavailable, poll fallback only", "error", err)
		return err
	}
	w.fsw = fsw
	registered := 0
	addErrs := []error{errors.New("no iter-log root registered with fsnotify")}
	for _, root := range w.roots {
		if err := fsw.Add(root); err != nil {
			w.logger.Warn("dashboard/watch: cannot watch root, poll fallback covers it",
				"root", root, "error", err)
			addErrs = append(addErrs, fmt.Errorf("watch %s: %w", root, err))
			continue
		}
		registered++
	}
	w.wg.Add(1)
	go w.fsLoop()
	if registered == 0 {
		return errors.Join(addErrs...)
	}
	return nil
}

// fsLoop drains the fsnotify source until Close or the source's channel
// closes. Watcher errors are logged and the loop continues — a transient
// error does not mean we should stop watching (and the poll still runs).
func (w *Watcher) fsLoop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stop:
			return
		case ev, ok := <-w.fsw.Events():
			if !ok {
				return
			}
			w.handleFSEvent(ev)
		case err, ok := <-w.fsw.Errors():
			if !ok {
				return
			}
			w.logger.Warn("dashboard/watch: fsnotify error", "error", err)
		}
	}
}

// handleFSEvent maps one raw fsnotify event onto the dedupe-and-publish path.
// Removals only forget the file so a later re-create publishes again; chmods
// and unwatched filenames (temp files from atomic writes) are ignored.
func (w *Watcher) handleFSEvent(ev fsnotify.Event) {
	name := filepath.Base(ev.Name)
	if !watchedFile(name) {
		return
	}
	if ev.Op&fsnotify.Remove != 0 {
		w.mu.Lock()
		delete(w.seen, ev.Name)
		w.mu.Unlock()
		return
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return
	}
	w.checkAndPublish(filepath.Dir(ev.Name), name)
}

// pollLoop is the OQ3 1-second mtime-poll fallback, rescanning every root on
// each tick. It shares the dedupe table with the fsnotify path, so whichever
// source notices a change first is the only one that publishes it.
func (w *Watcher) pollLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			for _, root := range w.roots {
				w.pollRoot(root)
			}
		}
	}
}

// pollRoot diffs one root's current directory state against the dedupe
// table: changed or new watched files publish, vanished files are forgotten.
func (w *Watcher) pollRoot(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !watchedFile(e.Name()) {
			continue
		}
		present[filepath.Join(root, e.Name())] = true
		w.checkAndPublish(root, e.Name())
	}
	w.forgetMissing(root, present)
}

// forgetMissing drops dedupe entries for files deleted from root, so a
// re-created file publishes again on either source.
func (w *Watcher) forgetMissing(root string, present map[string]bool) {
	prefix := root + string(filepath.Separator)
	w.mu.Lock()
	defer w.mu.Unlock()
	for path := range w.seen {
		if len(path) > len(prefix) && path[:len(prefix)] == prefix && !present[path] {
			delete(w.seen, path)
		}
	}
}

// checkAndPublish is the shared dedupe-then-publish step for both sources: a
// file whose mtime matches the last published one is a no-op; otherwise the
// new mtime is recorded and exactly one event goes out.
func (w *Watcher) checkAndPublish(root, name string) {
	full := filepath.Join(root, name)
	info, err := os.Stat(full)
	if err != nil {
		return // deleted between notification and stat
	}
	mt := info.ModTime()
	w.mu.Lock()
	prev, existed := w.seen[full]
	if existed && prev.Equal(mt) {
		w.mu.Unlock()
		return
	}
	w.seen[full] = mt
	w.mu.Unlock()
	w.publish(root, name, existed)
}

// watchedFile reports whether name belongs to one of the three watched
// artifact families.
func watchedFile(name string) bool {
	return iterRecordRE.MatchString(name) || iterScoreRE.MatchString(name) || sessionScrRE.MatchString(name)
}

// publish classifies one changed file into its API.md §3.7 topic + payload.
// existed distinguishes a brand-new score sidecar (iteration.scored, spec R5)
// from an in-place rewrite (score.recomputed).
func (w *Watcher) publish(root, name string, existed bool) {
	if m := sessionScrRE.FindStringSubmatch(name); m != nil {
		w.pub.Publish(events.TopicSessionUpdated, SessionUpdated{SessionID: m[1], root: root})
		return
	}
	if m := iterScoreRE.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1]) // the \d+ capture cannot fail Atoi
		topic := events.TopicIterationScored
		if existed {
			topic = events.TopicScoreRecomputed
		}
		w.pub.Publish(topic, IterationScored{
			SessionID: w.readSessionID(root, n),
			Iteration: n,
			Band:      w.readSidecarBand(root, n),
			root:      root,
		})
		return
	}
	// Remaining family: the iter-N.yaml record itself changed (checkpoint
	// wrote or rewrote the entry). Per the task contract this pushes
	// iteration.scored — the client invalidates the run + iteration queries.
	m := iterRecordRE.FindStringSubmatch(name)
	n, _ := strconv.Atoi(m[1])
	w.pub.Publish(events.TopicIterationScored, IterationScored{
		SessionID: w.readSessionID(root, n),
		Iteration: n,
		Band:      w.readSidecarBand(root, n),
		root:      root,
	})
}

// readSessionID best-effort resolves iteration n's session id from its
// iter-N.yaml record (the event schema keys invalidation on session_id, and
// neither sidecar family carries it for an iteration).
func (w *Watcher) readSessionID(root string, n int) string {
	path := filepath.Join(root, "iter-"+strconv.Itoa(n)+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	rec, err := scoring.ParseIterationRecord(data)
	if err != nil {
		w.logger.Warn("dashboard/watch: unparseable iter record for event payload",
			"path", path, "error", err)
		return ""
	}
	return rec.Agent.SessionID
}

// readSidecarBand best-effort reads iter-N.score.yaml's band so the UI can
// tint a live tick without an immediate refetch; empty when unavailable.
func (w *Watcher) readSidecarBand(root string, n int) string {
	data, err := os.ReadFile(filepath.Join(root, "iter-"+strconv.Itoa(n)+".score.yaml"))
	if err != nil {
		return ""
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil {
		return ""
	}
	return ps.Band
}
