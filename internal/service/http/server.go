// Package http is the HTTP surface of the `da service` runtime. It hosts two
// built-in routes — GET /healthz (liveness) and GET /api/tasks (the scheduler
// State() projection as JSON) — and a RegisterMount reservation point that lets
// sibling plans (R2's dashboard, R5's review queue) stitch their own handlers
// under arbitrary path prefixes without modifying this package (see R3 design
// decision D5).
//
// Scope: this package ships only the mount machinery plus the two built-in
// routes. It deliberately owns no R2/R5 endpoints and no authn/authz — RBAC is
// R5's plan. The package name collides with the standard library, so net/http
// is imported here under the alias nethttp.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"sync"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// defaultShutdownTimeout bounds how long Serve waits for in-flight requests to
// finish once the run context is cancelled before forcing the listener closed.
const defaultShutdownTimeout = 5 * time.Second

// Built-in reserved route prefixes. They are seeded into the mount registry so
// a RegisterMount call that would shadow or be shadowed by them is rejected
// deterministically rather than silently colliding at the mux level.
const (
	routeHealthz = "/healthz"
	routeTasks   = "/api/tasks"
)

// Errors returned by RegisterMount.
var (
	// ErrInvalidMountPrefix is returned when a mount prefix is empty or does
	// not begin with a leading slash.
	ErrInvalidMountPrefix = errors.New("service/http: mount prefix must be a non-empty rooted path")
	// ErrNilMountHandler is returned when RegisterMount is given a nil handler.
	ErrNilMountHandler = errors.New("service/http: mount handler must not be nil")
	// ErrOverlappingMount is returned when a mount prefix overlaps a built-in
	// route or a previously registered mount. Two prefixes overlap when they
	// are equal or one is a path-segment prefix of the other.
	ErrOverlappingMount = errors.New("service/http: mount prefix overlaps an existing route")
)

// StateProvider is the read-only slice of the scheduler the HTTP surface needs:
// a snapshot of task health for /api/tasks. Depending on the interface rather
// than *scheduler.Scheduler keeps the server trivially testable and documents
// that the server never mutates the scheduler. *scheduler.Scheduler satisfies
// it.
type StateProvider interface {
	State() []scheduler.TaskState
}

// Server is the service HTTP surface. Construct it with New. It is safe for
// concurrent use: RegisterMount may be called from any goroutine before or
// after Serve, and Serve/Shutdown coordinate the underlying net/http server.
type Server struct {
	addr      string
	mux       *nethttp.ServeMux
	scheduler StateProvider
	bus       *events.Bus
	httpSrv   *nethttp.Server

	shutdownTimeout time.Duration

	mu         sync.Mutex
	mounts     []string // reserved prefixes (built-ins + registered mounts)
	actualAddr string   // bound address, resolved after Serve begins listening
}

// New builds a Server bound (on Serve) to addr, projecting sched.State() at
// /api/tasks and carrying bus for sibling plans that mount bus-backed handlers.
// The two built-in routes are registered immediately.
func New(addr string, sched StateProvider, bus *events.Bus) *Server {
	mux := nethttp.NewServeMux()
	s := &Server{
		addr:            addr,
		mux:             mux,
		scheduler:       sched,
		bus:             bus,
		shutdownTimeout: defaultShutdownTimeout,
		// Built-in routes are reserved so mounts can't shadow them.
		mounts: []string{routeHealthz, routeTasks},
	}
	s.httpSrv = &nethttp.Server{Handler: mux}
	mux.HandleFunc("GET "+routeHealthz, s.handleHealthz)
	mux.HandleFunc("GET "+routeTasks, s.handleTasks)
	return s
}

// Handler returns the underlying request multiplexer. It is exposed so tests
// (and embedders) can drive the routes via httptest without binding a socket.
func (s *Server) Handler() nethttp.Handler { return s.mux }

// Bus returns the event bus the server was constructed with, so a sibling
// plan's mount handler can be wired to it by the composing runtime.
func (s *Server) Bus() *events.Bus { return s.bus }

// Addr returns the bound listen address once Serve is listening; before that it
// returns the configured address (which may use a :0 wildcard port).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.actualAddr != "" {
		return s.actualAddr
	}
	return s.addr
}

// handleHealthz reports liveness with a bare 200.
func (s *Server) handleHealthz(w nethttp.ResponseWriter, _ *nethttp.Request) {
	w.WriteHeader(nethttp.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// handleTasks renders the scheduler task-health snapshot as JSON. The shape is
// exactly scheduler.State() ([]scheduler.TaskState), so status clients decode
// against the scheduler's own type.
func (s *Server) handleTasks(w nethttp.ResponseWriter, _ *nethttp.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.scheduler.State())
}

// RegisterMount stitches handler under prefix, serving both the exact prefix
// path and its subtree. R2 mounts its dashboard, R5 its review queue, through
// this call. The prefix must be rooted (start with "/"); a trailing slash is
// normalized away. Overlapping prefixes — equal to, or a path-segment prefix
// of, an existing route (built-in or previously mounted) — are rejected with
// ErrOverlappingMount so registration order never silently shadows a route.
func (s *Server) RegisterMount(prefix string, handler nethttp.Handler) error {
	if handler == nil {
		return ErrNilMountHandler
	}
	clean, err := normalizeMountPrefix(prefix)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.mounts {
		if prefixesOverlap(clean, existing) {
			return fmt.Errorf("%w: %q vs %q", ErrOverlappingMount, clean, existing)
		}
	}
	// Register the exact path and the subtree so both /api/x and /api/x/y route
	// to handler without net/http's trailing-slash redirect.
	s.mux.Handle(clean, handler)
	s.mux.Handle(clean+"/", handler)
	s.mounts = append(s.mounts, clean)
	return nil
}

// normalizeMountPrefix validates a rooted path and strips a trailing slash
// (except for the root "/" itself, which any caller-supplied prefix will still
// overlap the built-ins and thus be rejected downstream).
func normalizeMountPrefix(prefix string) (string, error) {
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return "", fmt.Errorf("%w: %q", ErrInvalidMountPrefix, prefix)
	}
	if prefix != "/" {
		prefix = strings.TrimRight(prefix, "/")
	}
	return prefix, nil
}

// prefixesOverlap reports whether a and b collide as route prefixes: equal, or
// one a path-segment prefix of the other ("/api" overlaps "/api/tasks" but not
// "/apiv2").
func prefixesOverlap(a, b string) bool {
	return segmentPrefix(a, b) || segmentPrefix(b, a)
}

// segmentPrefix reports whether a is a path-segment prefix of b. The root "/"
// is a prefix of everything.
func segmentPrefix(a, b string) bool {
	if a == b || a == "/" {
		return true
	}
	return strings.HasPrefix(b, a+"/")
}

// Shutdown gracefully drains the underlying net/http server, honouring ctx as
// the deadline. It is the mechanism the runtime wires to context cancellation.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// Serve binds the configured address and serves until ctx is cancelled, at
// which point it gracefully shuts the server down within the configured
// shutdown timeout. It returns nil on a clean shutdown, the listen error if the
// address cannot be bound, or a non-ErrServerClosed serve error otherwise.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("service/http: listen %q: %w", s.addr, err)
	}
	s.mu.Lock()
	s.actualAddr = ln.Addr().String()
	s.mu.Unlock()

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.httpSrv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		shutdownErr := s.Shutdown(shutdownCtx)
		// Serve returns ErrServerClosed once Shutdown completes; drain it so the
		// goroutine never leaks.
		<-serveErr
		return shutdownErr
	case err := <-serveErr:
		if errors.Is(err, nethttp.ErrServerClosed) {
			return nil
		}
		return err
	}
}
