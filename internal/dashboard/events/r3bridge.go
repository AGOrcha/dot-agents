package events

import (
	"path/filepath"
	"sync"
	"time"

	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
)

// r3bridge.go bridges the R3 background-worker service's publish primitive
// (internal/service/events, the D4.1 EventBus interface) into this dashboard
// broker (task t13-r3-mount-integration). Where AttachBus (bridge.go) is the
// generic verbatim forwarder — same topic, same payload — this bridge is the
// R3-SPECIFIC one: R3 and the dashboard own DIFFERENT event taxonomies, so
// AttachR3Bus TRANSLATES each R3 topic into the API.md §3.7 dashboard taxonomy
// (topic + schema payload) before fan-out.
//
// After t13 lands this is the dashboard's PRIMARY event source when hosted in
// R3: R3's in-process publish drives the SSE stream, and the fswatch watcher
// (t06) demotes to the fallback the standalone binary keeps for writers that
// bypass the service.
//
// Topic translation (R3 topic → dashboard topic):
//
//   - events.TopicIterationScored ("iteration.scored") → TopicIterationScored:
//     R3's IterationScored payload (iter + score + band + sidecar path) becomes
//     the schema's {session_id, iteration, band}. R3's payload does not carry
//     the session id, so it is resolved from the iteration's on-disk record via
//     the injected SessionResolver (best-effort — "" when unavailable, exactly
//     as the fswatch bridge degrades).
//   - events.TopicRescoreDone ("rescore.done") → TopicRubricChanged: the
//     RescoreDone payload's ToVersion becomes {rubric_version}, the spec-R7
//     full-reload broadcast.
//
// R3's task.error topic has no dashboard SSE surface, so it is never subscribed
// and never bridged.
//
// Like AttachBus, this binds ONLY to the D4.1 EventBus INTERFACE and assumes
// only the D4.2 G1–G4 delivery floor (a config-selected backend can replace the
// builtin without touching this bridge). Bridged events keep the source bus
// timestamp, normalized to the schema's whole-second UTC form.

// r3BridgedTopics is the set of R3 service-event topics with a dashboard SSE
// surface, subscribed by AttachR3Bus. task.error is deliberately absent.
var r3BridgedTopics = []string{
	svcevents.TopicIterationScored,
	svcevents.TopicRescoreDone,
}

// SessionResolver resolves an iteration's dashboard session id from its
// iter-log directory and iteration number. R3's iteration.scored payload does
// not carry the session id (the dashboard event schema keys query invalidation
// on it), so the composing runtime injects a resolver — see
// internal/dashboard/server.Mount, which resolves it from the iter-<n>.yaml
// record. Best-effort: return "" when the session id cannot be determined.
type SessionResolver func(iterLogDir string, iteration int) string

// R3Option configures AttachR3Bus.
type R3Option func(*r3Config)

// r3Config carries AttachR3Bus's resolved options.
type r3Config struct {
	resolve SessionResolver
}

// WithR3SessionResolver sets the resolver used to fill the dashboard
// iteration.scored payload's session_id from the iteration's on-disk record.
// A nil resolver is ignored (the zero-value resolver yields "").
func WithR3SessionResolver(r SessionResolver) R3Option {
	return func(c *r3Config) {
		if r != nil {
			c.resolve = r
		}
	}
}

// r3IterationScored is the dashboard iteration.scored / score.recomputed
// payload shape (dashboard-event.schema.json: session_id + iteration + optional
// band), carrying its iter-log root out-of-band for the broker's per-root cache
// eviction without leaking the root into the marshaled payload.
type r3IterationScored struct {
	SessionID string `json:"session_id"`
	Iteration int    `json:"iteration"`
	Band      string `json:"band,omitempty"`

	root string
}

// IterLogRoot implements IterLogRooter so the broker evicts only the affected
// root's cache snapshot.
func (p r3IterationScored) IterLogRoot() string { return p.root }

// r3RubricChanged is the dashboard rubric.changed payload shape
// (dashboard-event.schema.json: rubric_version). A rubric bump rescores every
// root, so it carries no root and drives whole-cache eviction.
type r3RubricChanged struct {
	RubricVersion string `json:"rubric_version"`
}

// AttachR3Bus subscribes the broker to the R3 EventBus for every topic with a
// dashboard SSE surface, translates each received event into the API.md §3.7
// taxonomy, evicts the affected store-cache scope, and fans the translated
// event out to SSE subscribers — the same eviction+fan-out path a local
// Publish takes.
//
// The returned detach func unsubscribes every topic and is idempotent; Close
// also runs it. Attaching to a closed broker returns ErrClosed. An error from
// bus.Subscribe aborts the attach and unwinds any subscriptions already made.
func (b *Broker) AttachR3Bus(bus svcevents.EventBus, opts ...R3Option) (func(), error) {
	cfg := r3Config{resolve: func(string, int) string { return "" }}
	for _, o := range opts {
		o(&cfg)
	}

	streams, unsubs, err := subscribeAll(bus, r3BridgedTopics)
	if err != nil {
		return nil, err
	}
	detach := func() {
		for _, unsub := range unsubs {
			unsub()
		}
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		detach()
		return nil, ErrClosed
	}
	b.detaches = append(b.detaches, detach)
	b.wg.Add(len(streams))
	b.mu.Unlock()

	for _, stream := range streams {
		go b.forwardR3(stream, cfg)
	}
	var once sync.Once
	return func() { once.Do(detach) }, nil
}

// forwardR3 drains one R3 bus subscription, translating each event into the
// dashboard taxonomy before eviction and fan-out. Events that do not translate
// (an unexpected payload shape on a bridged topic) are dropped rather than
// forwarded off-schema.
func (b *Broker) forwardR3(stream <-chan svcevents.Event, cfg r3Config) {
	defer b.wg.Done()
	for busEvent := range stream {
		topic, payload, ok := translateR3Event(busEvent, cfg)
		if !ok {
			continue
		}
		b.evict(payload)
		b.dispatch(topic, payload, busEvent.Timestamp.UTC().Truncate(time.Second))
	}
}

// translateR3Event maps one R3 bus event onto its dashboard topic + schema
// payload. The second return is false when the event carries no dashboard
// surface, so the caller drops it.
func translateR3Event(ev svcevents.Event, cfg r3Config) (string, any, bool) {
	switch ev.Topic {
	case svcevents.TopicIterationScored:
		p, ok := ev.Payload.(svcevents.IterationScored)
		if !ok {
			return "", nil, false
		}
		root := ""
		if p.SidecarPath != "" {
			// Logical event-stream root key: forward-slash and OS-independent
			// so it matches the store's normalized cache key on every OS.
			// filepath.Dir strips the filename for both slash and OS-sep input
			// (R3 may publish either); ToSlash then re-canonicalizes the
			// separator that filepath.Dir cleaned to the OS form on Windows.
			// The resolver's actual disk read re-derives the OS path via
			// filepath.Join, so keeping this key logical is safe.
			root = filepath.ToSlash(filepath.Dir(p.SidecarPath))
		}
		return TopicIterationScored, r3IterationScored{
			SessionID: cfg.resolve(root, p.Iteration),
			Iteration: p.Iteration,
			Band:      p.Band,
			root:      root,
		}, true
	case svcevents.TopicRescoreDone:
		p, ok := ev.Payload.(svcevents.RescoreDone)
		if !ok {
			return "", nil, false
		}
		return TopicRubricChanged, r3RubricChanged{RubricVersion: p.ToVersion}, true
	default:
		return "", nil, false
	}
}
