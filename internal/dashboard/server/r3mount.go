// r3mount.go mounts the R2 observability dashboard INSIDE the R3
// background-worker service (task t13-r3-mount-integration): it composes the
// dashboard's store → broker → handlers collaborators, stitches the REST/SSE
// handler mount onto R3's HTTP/SSE edge via RegisterMount (spec D5 — the
// browser-facing arm of the r3 §2A surface→transport map, NEVER the UDS
// control plane), and bridges R3's EventBus into the broker so R3's in-process
// publish is the dashboard's PRIMARY event source.
//
// This is the counterpart to the standalone Server (server.go): where the
// standalone binary owns hosting (bind address, static assets) and runs the
// fswatch watcher as its push source, the R3-mounted composition owns neither —
// R3 owns hosting, and the R3 bus bridge replaces fswatch as the primary
// source. fswatch (t06) therefore demotes to the fallback the standalone
// binary keeps for writers that bypass the service (a developer running
// `da score run` manually); no watcher runs here.
package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
	"github.com/AGOrcha/dot-agents/internal/dashboard/handlers"
	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
)

// ErrNilEdge is returned by Mount when the R3 edge is nil.
var ErrNilEdge = errors.New("dashboard/server: R3 edge is required")

// ErrNilBus is returned by Mount when the R3 edge exposes no EventBus: the
// dashboard mount's whole point in R3 is to bridge that bus into the SSE
// stream, so a nil bus is a wiring error, not a silently REST-only mode.
var ErrNilBus = errors.New("dashboard/server: R3 edge exposes no event bus")

// newHandlers builds the dashboard REST/SSE Mount. It is a package var so a
// test can drive Mount's build-handlers error arm: with a live recompute store
// and broker, handlers.New only fails on a nil Store, a combination Mount never
// constructs, so the failure path is otherwise unreachable.
var newHandlers = handlers.New

// R3Edge is the slice of the R3 HTTP/SSE edge (internal/service/http.Server)
// the dashboard mount needs: the RegisterMount reservation point and the D4.1
// EventBus the runtime's background tasks publish on. *service/http.Server
// satisfies it; depending on the interface keeps this composition testable and
// free of a direct import of the service runtime.
type R3Edge interface {
	// RegisterMount stitches handler under prefix on the HTTP/SSE edge.
	RegisterMount(prefix string, handler http.Handler) error
	// Bus returns the EventBus the runtime publishes background-task events on.
	Bus() svcevents.EventBus
}

// MountConfig is the dashboard-mount policy the R3 runtime supplies. The zero
// value is usable (no roots → REST returns empty collections; repoDir ".").
type MountConfig struct {
	// IterLogDirs are the iter-log roots the Store reads. Empty is legal.
	IterLogDirs []string
	// RepoDir is the repository root the recompute pipeline runs git topology
	// against; empty → ".".
	RepoDir string
	// TranscriptDirs are optional agent session-log roots the recompute
	// pipeline uses for token backfill and transcript-derived checks.
	TranscriptDirs []string
	// Logger receives structured handler, store, and bridge logs; nil → a
	// discard logger.
	Logger *slog.Logger
}

// Mount composes the dashboard (store t02 + recompute t06 → broker t04 →
// handlers t03/t05), registers the handler mount under handlers.Prefix on
// edge's HTTP/SSE edge (RegisterMount, full-path routing, no prefix stripping),
// and bridges edge's R3 EventBus into the broker as the dashboard's primary
// event source. It starts no filesystem watcher.
//
// The returned io.Closer detaches the bus bridge and closes the broker
// (disconnecting every SSE subscriber); the R3 runtime calls it on shutdown.
// Mount fails on a nil edge, a nil bus, or a handler/mount-registration error;
// on any failure it releases the broker it built so no goroutine leaks.
func Mount(edge R3Edge, cfg MountConfig) (io.Closer, error) {
	if edge == nil {
		return nil, ErrNilEdge
	}
	bus := edge.Bus()
	if bus == nil {
		return nil, ErrNilBus
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	repoDir := cfg.RepoDir
	if repoDir == "" {
		repoDir = "."
	}
	roots := append([]string(nil), cfg.IterLogDirs...)

	// The store↔broker cycle (store reports the live subscriber count; the
	// broker evicts the store's cache) is broken with a late-bound closure, as
	// in the standalone Server: the counter is read only when a health request
	// lands, by which point broker is assigned.
	var broker *events.Broker
	disk := store.New(roots,
		store.WithLogger(logger),
		store.WithSubscriberCounter(func() int {
			return broker.SubscriberCount()
		}),
	)
	broker = events.New(events.Options{Evictor: disk})
	recStore := store.NewRecompute(disk, repoDir, cfg.TranscriptDirs...)

	mount, err := newHandlers(handlers.Deps{
		Store:  recStore,
		Logger: logger,
		Broker: broker,
	})
	if err != nil {
		broker.Close()
		return nil, fmt.Errorf("dashboard/server: build handlers: %w", err)
	}

	if err := edge.RegisterMount(mount.Prefix(), mount); err != nil {
		broker.Close()
		return nil, fmt.Errorf("dashboard/server: register mount %q: %w", mount.Prefix(), err)
	}

	detach, err := broker.AttachR3Bus(bus, events.WithR3SessionResolver(diskSessionResolver(logger)))
	if err != nil {
		broker.Close()
		return nil, fmt.Errorf("dashboard/server: bridge R3 bus: %w", err)
	}

	return &mountHandle{broker: broker, detach: detach}, nil
}

// diskSessionResolver resolves an iteration's dashboard session id from its
// on-disk iter-<n>.yaml record. R3's iteration.scored payload does not carry
// the session id (the dashboard event schema keys query invalidation on it),
// so the bridge asks this resolver, mirroring the fswatch bridge's own
// best-effort derivation: an unreadable or unparseable record yields "".
func diskSessionResolver(logger *slog.Logger) events.SessionResolver {
	return func(iterLogDir string, n int) string {
		if iterLogDir == "" {
			return ""
		}
		data, err := os.ReadFile(filepath.Join(iterLogDir, "iter-"+strconv.Itoa(n)+".yaml"))
		if err != nil {
			return ""
		}
		rec, err := scoring.ParseIterationRecord(data)
		if err != nil {
			logger.Warn("dashboard/server: unparseable iter record for R3 event payload",
				"dir", iterLogDir, "iter", n, "error", err)
			return ""
		}
		return rec.Agent.SessionID
	}
}

// mountHandle is the teardown handle Mount returns: it detaches the R3 bus
// bridge and closes the broker (which also disconnects SSE subscribers and
// re-runs the detach, harmlessly, via the broker's own detach list).
type mountHandle struct {
	broker *events.Broker
	detach func()
}

// Close detaches the bridge and closes the broker. Safe to call once on
// runtime shutdown.
func (h *mountHandle) Close() error {
	h.detach()
	h.broker.Close()
	return nil
}
