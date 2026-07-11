// Package handlers implements the R2 observability dashboard's REST API
// (plan task t03-handlers-rest) per the pinned contract in
// .agents/workflow/plans/r2-observability-dashboard/design/API.md.
//
// It is the browser-facing HTTP arm of the r3 §2A surface→transport map: the
// Mount below is a plain http.Handler whose routes carry their FULL paths
// (rooted at /api/v1/observability, the observability domain of
// [[api-conventions]]), so the R3 service stitches it onto its HTTP/SSE edge
// via internal/service/http.(*Server).RegisterMount(Prefix, m) with no prefix
// stripping — the same shape as internal/review/http. Hosting, lifecycle, and
// middleware live in the composing runtimes (t07 standalone / t13 R3 mount);
// route ownership lives here.
//
// Handlers depend only on the dashboard store.Store interface (production
// wiring passes the t06 RecomputeStore so a detail read can trigger
// score-recompute-on-miss transparently): each parses and validates its
// params (§1.3/§1.4 — invalid input is a 400 bad_request envelope), calls the
// store with the request context, wraps the DTO in the §1.2
// {data, meta:{etag, count}} envelope, honours If-None-Match with a 304
// (§1.5), and emits one structured slog line per served request.
//
// The SSE endpoint (§3.7, /api/v1/observability/events) is NOT registered
// here — t05 owns it (stream.go in this package) and attaches it to the same
// mount.
//
// Anti-scope (API.md §5): no write endpoints, no auth, no cursors, no replay.
package handlers

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
)

// Prefix is the mount point the composing runtimes (t07 standalone server,
// t13 R3 mount) register this handler under — RegisterMount(Prefix, m), the
// R3 server docs' "R2 mounts its dashboard at /api". Mounts do not strip
// prefixes, so every route pattern below carries its full path.
const Prefix = "/api"

// basePath is the version-first API root pinned by API.md §1.1: every
// dashboard resource lives under the literal /api/v1/observability prefix,
// and the contract test asserts that literal so a future /api/v2 move is a
// deliberate, testable break.
const basePath = Prefix + "/v1/observability"

// ErrNilStore is returned by New when Deps.Store is missing.
var ErrNilStore = errors.New("dashboard/handlers: Deps.Store is required")

// Deps carries the collaborators a Mount is built from.
type Deps struct {
	// Store is the read surface (API.md's authoritative Store interface).
	// Production wiring passes the recompute-decorated store
	// (store.NewRecompute) so GetIteration can recompute-on-miss; any
	// Store implementation works. Required.
	Store store.Store
	// Logger receives the per-request structured log lines and store
	// failure warnings. Optional; nil defaults to a discard logger.
	Logger *slog.Logger
	// Broker is the SSE fan-out seam the /events endpoint subscribes to
	// (t04). Optional: when nil the REST routes still serve and GET
	// {base}/events replies 503 (streaming unavailable in this
	// composition), so REST-only wirings need not construct a broker.
	Broker *events.Broker
}

// Mount is the dashboard REST API as a plain http.Handler. Construct it with
// New; the composing runtime registers it via RegisterMount(m.Prefix(), m).
// It is stateless apart from its injected collaborators and safe for
// concurrent use.
type Mount struct {
	mux    *http.ServeMux
	store  store.Store
	logger *slog.Logger
	broker *events.Broker
}

// New builds a Mount over deps with all six REST routes registered.
func New(deps Deps) (*Mount, error) {
	if deps.Store == nil {
		return nil, ErrNilStore
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	m := &Mount{
		mux:    http.NewServeMux(),
		store:  deps.Store,
		logger: logger,
		broker: deps.Broker,
	}
	m.routes()
	return m, nil
}

// Prefix returns the mount prefix the composing runtime passes to
// RegisterMount.
func (m *Mount) Prefix() string { return Prefix }

// ServeHTTP dispatches to the mount's internal mux. Patterns carry the full
// prefixed paths, so no prefix stripping happens (or is needed) on the host
// side.
func (m *Mount) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

// routes registers the API.md §2 endpoint catalogue (the REST rows; the SSE
// row is t05's).
func (m *Mount) routes() {
	m.handle("GET "+basePath+"/runs", m.handleListRuns)
	m.handle("GET "+basePath+"/runs/{session_id}", m.handleGetRun)
	m.handle("GET "+basePath+"/runs/{session_id}/iterations", m.handleListIterations)
	m.handle("GET "+basePath+"/iterations/{n}", m.handleGetIteration)
	m.handle("GET "+basePath+"/rubric", m.handleRubric)
	m.handle("GET "+basePath+"/health", m.handleHealth)
	m.handle("GET "+basePath+"/events", m.handleEvents)
}

// handle wires one route through the request-logging middleware.
func (m *Mount) handle(pattern string, h http.HandlerFunc) {
	m.mux.Handle(pattern, m.logged(h))
}

// logged emits one structured slog line per completed request.
func (m *Mount) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		m.logger.Info("dashboard/handlers: served",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder captures the response status for the request log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status before delegating.
func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the wrapped ResponseWriter when it is an http.Flusher,
// keeping the logged middleware transparent to the long-lived SSE stream
// (t05's /events handler asserts http.Flusher on the writer it receives).
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// handleListRuns serves GET {base}/runs (API.md §3.1).
func (m *Mount) handleListRuns(w http.ResponseWriter, r *http.Request) {
	f, err := parseRunFilter(r.URL.Query())
	if err != nil {
		m.writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	runs, err := m.store.ListRuns(r.Context(), f)
	if err != nil {
		m.storeError(w, r, err, "listing runs failed")
		return
	}
	if runs == nil {
		runs = []store.RunSummary{}
	}
	m.respond(w, r, payload{resource: "runs", data: runs, count: intPtr(len(runs))})
}

// handleGetRun serves GET {base}/runs/{session_id} (API.md §3.2).
func (m *Mount) handleGetRun(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("session_id")
	run, err := m.store.GetRun(r.Context(), sid)
	if err != nil {
		m.storeError(w, r, err, fmt.Sprintf("no run for session_id %q", sid))
		return
	}
	m.respond(w, r, payload{resource: "run", data: run})
}

// handleListIterations serves GET {base}/runs/{session_id}/iterations
// (API.md §3.3). The store returns the full ascending list for the session;
// pagination is a presentation concern, applied here.
func (m *Mount) handleListIterations(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePage(r.URL.Query())
	if err != nil {
		m.writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	sid := r.PathValue("session_id")
	its, err := m.store.ListIterations(r.Context(), sid)
	if err != nil {
		m.storeError(w, r, err, fmt.Sprintf("no run for session_id %q", sid))
		return
	}
	page := pageOf(its, limit, offset)
	m.respond(w, r, payload{resource: "iterations", data: page, count: intPtr(len(page))})
}

// handleGetIteration serves GET {base}/iterations/{n} (API.md §3.4). The
// optional iter_log_dir query param disambiguates n across resolved roots
// (§1.6); the store rejects a dir outside its configured roots, which maps to
// a 400 here.
func (m *Mount) handleGetIteration(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		m.writeError(w, http.StatusBadRequest, codeBadRequest,
			`invalid "n": must be an integer >= 1`)
		return
	}
	it, err := m.store.GetIteration(r.Context(), r.URL.Query().Get("iter_log_dir"), n)
	if err != nil {
		m.storeError(w, r, err, fmt.Sprintf("no iteration %d in the resolved root", n))
		return
	}
	m.respond(w, r, payload{resource: "iteration", data: it})
}

// handleRubric serves GET {base}/rubric (API.md §3.5). Per the contract the
// etag IS the rubric version — the rubric is immutable per process.
func (m *Mount) handleRubric(w http.ResponseWriter, r *http.Request) {
	doc, err := m.store.Rubric(r.Context())
	if err != nil {
		m.storeError(w, r, err, "rubric unavailable")
		return
	}
	m.respond(w, r, payload{resource: "rubric", data: doc, etag: doc.Version})
}

// handleHealth serves GET {base}/health (API.md §3.6). The contract pins
// "never returns 5xx while the process is up", so a store failure degrades to
// a bare liveness payload instead of an error envelope.
func (m *Mount) handleHealth(w http.ResponseWriter, r *http.Request) {
	h, err := m.store.Health(r.Context())
	if err != nil {
		m.logger.Warn("dashboard/handlers: health probe degraded to bare liveness",
			"error", err)
		h = store.Health{Status: "ok", Roots: []string{}}
	}
	m.respond(w, r, payload{resource: "health", data: h})
}

// storeError maps a Store failure onto the §1.3 error envelope: ErrNotFound →
// 404 with the caller's resource-specific message, ErrRootNotAllowed → 400
// (the request named a directory outside the resolved roots), anything else →
// a logged 500 internal.
func (m *Mount) storeError(w http.ResponseWriter, r *http.Request, err error, notFoundMsg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		m.writeError(w, http.StatusNotFound, codeNotFound, notFoundMsg)
	case errors.Is(err, store.ErrRootNotAllowed):
		m.writeError(w, http.StatusBadRequest, codeBadRequest,
			`invalid "iter_log_dir": not a configured iter-log root`)
	default:
		m.logger.Error("dashboard/handlers: store failure",
			"path", r.URL.Path, "error", err)
		m.writeError(w, http.StatusInternalServerError, codeInternal,
			"unexpected server error")
	}
}

// intPtr adapts a list length for the envelope's optional meta.count.
func intPtr(v int) *int { return &v }
