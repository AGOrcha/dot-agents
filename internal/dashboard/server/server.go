// Package server wires the R2 observability dashboard's four in-process
// collaborators — the read Store (t02) decorated with recompute-on-miss (t06),
// the SSE Broker (t04), the REST+SSE Handlers mount (t03/t05), and the
// filesystem Watcher (t06) — into a single net/http server bound to a
// loopback address.
//
// The Server type is the reusable composition root: the standalone
// cmd/da-dashboard binary constructs one and calls Serve, and the future R3
// service (t13) can construct one and mount its Handler under its own
// HTTP/SSE edge. Hosting policy (bind address, dev-asset proxying, embedded
// vs. on-disk static assets) is Config; route ownership stays in the
// handlers package.
//
// Routing (net/http most-specific-wins):
//
//   - GET /api/health        → a bare liveness 200 for the standalone process.
//   - /api/ (subtree)        → the handlers mount (REST + SSE, full-path routes).
//   - / (everything else)    → the SPA static handler (or the Vite dev proxy).
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
	"github.com/AGOrcha/dot-agents/internal/dashboard/handlers"
	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
	"github.com/AGOrcha/dot-agents/internal/dashboard/watch"
)

// DefaultAddr is the loopback bind used when Config.Addr is empty — the same
// deliberate loopback-by-default posture as the R3 service edge (anything
// wider is an explicit opt-in via --addr).
const DefaultAddr = "127.0.0.1:7300"

// defaultShutdownTimeout bounds the graceful drain when Serve's context is
// cancelled before the listener is forced closed.
const defaultShutdownTimeout = 5 * time.Second

// Config is the standalone dashboard server's hosting policy. The zero value
// is usable (loopback bind, embedded assets, no watched roots).
type Config struct {
	// Addr is the TCP bind address; empty → DefaultAddr. Use a :0 port to
	// bind an ephemeral port (tests read the resolved port via Addr()).
	Addr string
	// IterLogDirs are the iter-log roots the Store reads and the Watcher
	// watches. Empty is legal — the server still serves (REST returns empty
	// collections; the watcher degrades to a no-op poll).
	IterLogDirs []string
	// RepoDir is the repository root the recompute pipeline runs git
	// topology against (store.NewRecompute's repoDir); empty → ".".
	RepoDir string
	// TranscriptDirs are optional agent session-log roots the recompute
	// pipeline uses for token backfill and transcript-derived checks.
	TranscriptDirs []string
	// DevAssetProxy, when set, is the base URL of a running Vite dev server;
	// every non-/api request is reverse-proxied there instead of served from
	// the embedded/static bundle (front-end hot-reload during development).
	DevAssetProxy string
	// StaticDir overrides the go:embed'd dist/ with an on-disk build
	// directory (e.g. a freshly built web/dashboard/dist). Ignored when
	// DevAssetProxy is set.
	StaticDir string
	// Logger receives structured server, handler, and watcher logs; nil →
	// a discard logger.
	Logger *slog.Logger
}

// Server is the composed dashboard runtime. Construct it with New, start it
// with Start (non-blocking) or Serve (blocking until ctx is cancelled), and
// release it with Stop.
type Server struct {
	cfg     Config
	logger  *slog.Logger
	store   *store.RecomputeStore
	broker  *events.Broker
	mount   *handlers.Mount
	watcher *watch.Watcher
	httpSrv *http.Server

	mu       sync.Mutex
	addr     string
	serveErr chan error
}

// mountBuilder constructs the API handlers mount. Production wires
// stdMountBuilder (handlers.New); newServer takes it as a parameter so tests
// can inject a builder that fails, exercising newServer's build-error branch.
// handlers.New itself only errors on a nil Store, which the real store wiring
// never produces, so this narrow seam is the only way to prove that branch.
type mountBuilder interface {
	build(handlers.Deps) (*handlers.Mount, error)
}

// stdMountBuilder is the production mountBuilder: it delegates to handlers.New.
type stdMountBuilder struct{}

func (stdMountBuilder) build(d handlers.Deps) (*handlers.Mount, error) {
	return handlers.New(d)
}

// New builds a Server over cfg, wiring the store → broker → handlers → watcher
// collaborators and composing the HTTP handler. It binds no socket; call Start
// or Serve for that. It fails only when the handlers mount or the static asset
// FS cannot be constructed.
func New(cfg Config) (*Server, error) {
	return newServer(cfg, stdMountBuilder{})
}

// newServer is New's testable core: the mountBuilder is injected so tests can
// drive the handlers-build failure path that the production wiring can't reach.
func newServer(cfg Config, mb mountBuilder) (*Server, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	addr := cfg.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	repoDir := cfg.RepoDir
	if repoDir == "" {
		repoDir = "."
	}
	roots := append([]string(nil), cfg.IterLogDirs...)

	broker, recStore, mount, err := dashboardCore(roots, repoDir, cfg.TranscriptDirs, logger, mb.build)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:     cfg,
		logger:  logger,
		store:   recStore,
		broker:  broker,
		mount:   mount,
		watcher: watch.New(roots, broker, watch.WithLogger(logger)),
		addr:    addr,
	}

	handler, err := s.composeHandler()
	if err != nil {
		return nil, err
	}
	s.httpSrv = &http.Server{Handler: handler}
	return s, nil
}

// dashboardCore composes the dashboard's shared read-side collaborators — the
// disk store decorated with recompute-on-miss, the SSE broker wired to evict
// that store, and the REST/SSE handlers mount built via build — for both the
// standalone Server (newServer) and the R3-mounted Mount. The store↔broker
// cycle (store reports the live subscriber count; the broker evicts the store's
// cache) is broken with a late-bound closure over the broker var assigned here,
// so the counter is read only when a health request lands, by which point
// broker is set. On a build failure it returns the broker it created WITHOUT
// closing it, leaving teardown policy to the caller (newServer returns the
// wrapped error as-is; Mount closes the broker first).
func dashboardCore(roots []string, repoDir string, transcriptDirs []string, logger *slog.Logger, build func(handlers.Deps) (*handlers.Mount, error)) (*events.Broker, *store.RecomputeStore, *handlers.Mount, error) {
	var broker *events.Broker
	disk := store.New(roots,
		store.WithLogger(logger),
		store.WithSubscriberCounter(func() int {
			return broker.SubscriberCount()
		}),
	)
	broker = events.New(events.Options{Evictor: disk})
	recStore := store.NewRecompute(disk, repoDir, transcriptDirs...)

	mount, err := build(handlers.Deps{
		Store:  recStore,
		Logger: logger,
		Broker: broker,
	})
	if err != nil {
		return broker, nil, nil, fmt.Errorf("dashboard/server: build handlers: %w", err)
	}
	return broker, recStore, mount, nil
}

// composeHandler builds the root mux: the liveness route, the handlers mount
// on the /api subtree (full-path routing, no prefix stripping), and the non-
// /api arm — the Vite dev proxy when configured, else the SPA static handler.
func (s *Server) composeHandler() (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.Handle(s.mount.Prefix()+"/", s.mount)

	var root http.Handler
	if s.cfg.DevAssetProxy != "" {
		target, err := url.Parse(s.cfg.DevAssetProxy)
		if err != nil {
			return nil, fmt.Errorf("dashboard/server: invalid --dev-asset-proxy %q: %w", s.cfg.DevAssetProxy, err)
		}
		root = httputil.NewSingleHostReverseProxy(target)
	} else {
		fsys, err := distFS(s.cfg.StaticDir)
		if err != nil {
			return nil, fmt.Errorf("dashboard/server: static assets: %w", err)
		}
		root = spaHandler(fsys)
	}
	mux.Handle("/", root)
	return mux, nil
}

// handleHealth is the standalone process's own liveness probe: a bare 200 that
// never depends on the store, distinct from the mount's richer
// /api/v1/observability/health.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

// Handler returns the composed request multiplexer so R3 can mount it and
// tests can drive routes without binding a socket.
func (s *Server) Handler() http.Handler { return s.httpSrv.Handler }

// Addr returns the bound listen address once Start has run; before that it is
// the configured address (which may carry a :0 wildcard port).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Start binds the listener, launches the filesystem watcher, and serves in the
// background. It returns once the socket is bound (so Addr is resolved),
// surfacing any bind or watcher-start error synchronously.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("dashboard/server: listen %s: %w", s.addr, err)
	}
	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.mu.Unlock()

	if err := s.watcher.Start(); err != nil {
		_ = ln.Close()
		return fmt.Errorf("dashboard/server: start watcher: %w", err)
	}

	s.serveErr = make(chan error, 1)
	go func() {
		serveErr := s.httpSrv.Serve(ln)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		s.serveErr <- serveErr
	}()
	return nil
}

// Stop tears the server down in dependency order: the watcher stops producing
// events, the broker disconnects every SSE client (so their long-lived
// handlers return and the HTTP drain can complete), the HTTP server drains
// in-flight requests within ctx's deadline, and finally pending best-effort
// recompute sidecar writes are flushed so a just-computed score is not lost
// with the process. It is safe to call once after Start.
func (s *Server) Stop(ctx context.Context) error {
	s.watcher.Close()
	s.broker.Close()
	shutdownErr := s.httpSrv.Shutdown(ctx)
	s.store.Flush()

	var serveErr error
	if s.serveErr != nil {
		select {
		case serveErr = <-s.serveErr:
		default:
		}
	}
	return errors.Join(shutdownErr, serveErr)
}

// Serve starts the server and blocks until ctx is cancelled (or the background
// serve fails), then gracefully stops within the shutdown timeout. It is the
// entry point the standalone binary wires to signal-driven cancellation.
func (s *Server) Serve(ctx context.Context) error {
	if err := s.Start(); err != nil {
		return err
	}
	s.logger.Info("dashboard server listening", "addr", s.Addr())

	select {
	case <-ctx.Done():
	case err := <-s.serveErr:
		if err != nil {
			// Serve died on its own; clean up the rest and report it.
			_ = s.Stop(context.Background())
			return err
		}
	}

	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultShutdownTimeout)
	defer cancel()
	return s.Stop(drainCtx)
}
