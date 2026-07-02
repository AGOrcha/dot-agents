package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// fakeState is a deterministic StateProvider for shape assertions.
type fakeState struct {
	states []scheduler.TaskState
}

func (f fakeState) State() []scheduler.TaskState { return f.states }

func newTestServer(sched StateProvider) *Server {
	return New("127.0.0.1:0", sched, events.NewInProcBus())
}

func TestHealthzReturns200(t *testing.T) {
	srv := newTestServer(fakeState{})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want %q", body, "ok")
	}
}

func TestTasksJSONShapeMatchesSchedulerState(t *testing.T) {
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	want := []scheduler.TaskState{
		{Name: "ingest", LastRunAt: &now, ConsecutiveFailures: 0, Runs: 3},
		{Name: "rescore", LastError: "boom", ConsecutiveFailures: 2, Runs: 1, Dropped: 4},
	}
	srv := newTestServer(fakeState{states: want})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got []scheduler.TaskState
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tasks = %+v, want %+v", got, want)
	}
}

// TestTasksReflectsRealScheduler proves the endpoint projects a real
// *scheduler.Scheduler's State() (the concrete type satisfies StateProvider).
func TestTasksReflectsRealScheduler(t *testing.T) {
	sched := scheduler.New()
	if err := sched.Register(scheduler.Task{
		Name:    "real",
		Trigger: scheduler.Interval(time.Hour),
		RunFn:   func(context.Context) error { return nil },
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv := newTestServer(sched)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	defer resp.Body.Close()

	var got []scheduler.TaskState
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "real" {
		t.Fatalf("tasks = %+v, want single task named real", got)
	}
}

func TestRegisterMountRoutesExactAndSubtree(t *testing.T) {
	srv := newTestServer(fakeState{})
	handler := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		_, _ = io.WriteString(w, "mounted:"+r.URL.Path)
	})
	if err := srv.RegisterMount("/api/test", handler); err != nil {
		t.Fatalf("RegisterMount: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, path := range []string{"/api/test", "/api/test/nested"} {
		resp, err := nethttp.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
		if want := "mounted:" + path; string(body) != want {
			t.Fatalf("GET %s body = %q, want %q", path, body, want)
		}
	}
}

// TestRegisterMountTrailingSlashNormalized confirms a trailing slash on the
// prefix is stripped and still routes.
func TestRegisterMountTrailingSlashNormalized(t *testing.T) {
	srv := newTestServer(fakeState{})
	hit := false
	if err := srv.RegisterMount("/api/reviews/", nethttp.HandlerFunc(
		func(w nethttp.ResponseWriter, _ *nethttp.Request) { hit = true }),
	); err != nil {
		t.Fatalf("RegisterMount: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := nethttp.Get(ts.URL + "/api/reviews")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if !hit {
		t.Fatal("handler for normalized prefix was not reached")
	}
}

func TestRegisterMountOverlapErrors(t *testing.T) {
	h := nethttp.HandlerFunc(func(nethttp.ResponseWriter, *nethttp.Request) {})

	cases := []struct {
		name    string
		setup   []string // mounts to register first (must succeed)
		prefix  string
		wantErr error
	}{
		// Duplicating a built-in exact path (which the mux itself would reject).
		{name: "duplicates built-in tasks", prefix: "/api/tasks", wantErr: ErrOverlappingMount},
		{name: "duplicates built-in healthz", prefix: "/healthz", wantErr: ErrOverlappingMount},
		// Duplicating a prior mount's exact path.
		{name: "duplicate mount", setup: []string{"/dash"}, prefix: "/dash", wantErr: ErrOverlappingMount},
		{name: "duplicate mount trailing slash", setup: []string{"/dash"}, prefix: "/dash/", wantErr: ErrOverlappingMount},
		// Invalid prefixes.
		{name: "empty prefix", prefix: "", wantErr: ErrInvalidMountPrefix},
		{name: "unrooted prefix", prefix: "api", wantErr: ErrInvalidMountPrefix},
		// Coexisting prefixes (the contract R2/R5 depend on): a parent mount, a
		// nested mount, siblings, and lookalikes all succeed.
		{name: "parent of built-in tasks (R2 /api)", prefix: "/api", wantErr: nil},
		{name: "nested under a mount (R5 /api/reviews)", setup: []string{"/api"}, prefix: "/api/reviews", wantErr: nil},
		{name: "parent of existing mount", setup: []string{"/dash/sub"}, prefix: "/dash", wantErr: nil},
		{name: "sibling prefix ok", setup: []string{"/dash"}, prefix: "/reviews", wantErr: nil},
		{name: "non-segment lookalike ok", setup: []string{"/api/test"}, prefix: "/api/testing", wantErr: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(fakeState{})
			mountAll(t, srv, h, tc.setup)
			assertMountErr(t, srv.RegisterMount(tc.prefix, h), tc.prefix, tc.wantErr)
		})
	}
}

// mountAll registers each setup prefix, failing the test if any does not
// succeed (setup mounts are preconditions, never the assertion under test).
func mountAll(t *testing.T, srv *Server, h nethttp.Handler, prefixes []string) {
	t.Helper()
	for _, p := range prefixes {
		if err := srv.RegisterMount(p, h); err != nil {
			t.Fatalf("setup RegisterMount(%q): %v", p, err)
		}
	}
}

// assertMountErr checks a RegisterMount result against the expected sentinel
// (nil meaning the mount was expected to succeed).
func assertMountErr(t *testing.T, err error, prefix string, want error) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatalf("RegisterMount(%q) = %v, want nil", prefix, err)
		}
		return
	}
	if !errors.Is(err, want) {
		t.Fatalf("RegisterMount(%q) = %v, want %v", prefix, err, want)
	}
}

func TestRegisterMountNilHandler(t *testing.T) {
	srv := newTestServer(fakeState{})
	if err := srv.RegisterMount("/x", nil); !errors.Is(err, ErrNilMountHandler) {
		t.Fatalf("err = %v, want ErrNilMountHandler", err)
	}
}

// TestApiMountCoexistsWithBuiltinTasks is the R2 contract: a dashboard mounted
// at "/api" must succeed and coexist with the built-in "/api/tasks" — the
// built-in exact route wins for its own path, the mount serves the rest of the
// subtree.
func TestApiMountCoexistsWithBuiltinTasks(t *testing.T) {
	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	sched := fakeState{states: []scheduler.TaskState{{Name: "ingest", LastRunAt: &now, Runs: 1}}}
	srv := newTestServer(sched)

	dashboard := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		_, _ = io.WriteString(w, "dashboard:"+r.URL.Path)
	})
	if err := srv.RegisterMount("/api", dashboard); err != nil {
		t.Fatalf(`RegisterMount("/api") = %v, want nil (R2 contract)`, err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// /api/tasks still resolves to the built-in scheduler projection.
	resp, err := nethttp.Get(ts.URL + "/api/tasks")
	if err != nil {
		t.Fatalf("GET /api/tasks: %v", err)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		resp.Body.Close()
		t.Fatalf("/api/tasks Content-Type = %q, want application/json (built-in shadowed by mount)", got)
	}
	var tasks []scheduler.TaskState
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		resp.Body.Close()
		t.Fatalf("decode /api/tasks: %v", err)
	}
	resp.Body.Close()
	if len(tasks) != 1 || tasks[0].Name != "ingest" {
		t.Fatalf("/api/tasks = %+v, want the built-in scheduler state", tasks)
	}

	// Any other path under /api routes to the mounted dashboard.
	resp2, err := nethttp.Get(ts.URL + "/api/foo")
	if err != nil {
		t.Fatalf("GET /api/foo: %v", err)
	}
	body, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if want := "dashboard:/api/foo"; string(body) != want {
		t.Fatalf("GET /api/foo body = %q, want %q (mount not reached)", body, want)
	}
}

func TestBusAccessor(t *testing.T) {
	bus := events.NewInProcBus()
	srv := New("127.0.0.1:0", fakeState{}, bus)
	if srv.Bus() != bus {
		t.Fatal("Bus() did not return the constructed bus")
	}
}

func TestServeGracefulShutdownWithinDeadline(t *testing.T) {
	srv := newTestServer(fakeState{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	// Wait for the listener to bind, then confirm the server is live.
	addr := waitForAddr(t, srv)
	resp, err := nethttp.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz on live server: %v", err)
	}
	resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil after graceful shutdown", err)
		}
	case <-time.After(defaultShutdownTimeout + 2*time.Second):
		t.Fatal("Serve did not return within the shutdown deadline")
	}
}

// TestServeHonorsShortCallerDeadline asserts that when the caller's ctx carries
// a deadline shorter than the default shutdown timeout, Serve returns within
// that shorter deadline rather than waiting the full 5s — the drain is bounded
// by min(remaining deadline, shutdownTimeout).
func TestServeHonorsShortCallerDeadline(t *testing.T) {
	srv := newTestServer(fakeState{}) // defaultShutdownTimeout (5s)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- srv.Serve(ctx) }()
	waitForAddr(t, srv)

	select {
	case <-done:
		if elapsed := time.Since(start); elapsed >= defaultShutdownTimeout {
			t.Fatalf("Serve took %v; the caller's 300ms deadline was ignored for the %v default",
				elapsed, defaultShutdownTimeout)
		}
	case <-time.After(defaultShutdownTimeout - time.Second):
		t.Fatal("Serve did not return well within the default shutdown timeout")
	}
}

// TestServeReturnsNilOnDirectShutdown covers the path where Serve exits because
// Shutdown was called directly (not via ctx cancellation): httpSrv.Serve then
// returns ErrServerClosed, which Serve maps to a nil (clean) result.
func TestServeReturnsNilOnDirectShutdown(t *testing.T) {
	srv := newTestServer(fakeState{})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background()) }()

	waitForAddr(t, srv)
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil after direct shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after direct shutdown")
	}
}

func TestServeListenError(t *testing.T) {
	// Occupy a port, then point a server at it to force a listen failure.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("prep listen: %v", err)
	}
	defer ln.Close()

	srv := New(ln.Addr().String(), fakeState{}, events.NewInProcBus())
	if err := srv.Serve(context.Background()); err == nil {
		t.Fatal("Serve on an occupied port returned nil, want listen error")
	}
}

func TestAddrBeforeAndAfterServe(t *testing.T) {
	srv := New("127.0.0.1:0", fakeState{}, events.NewInProcBus())
	if srv.Addr() != "127.0.0.1:0" {
		t.Fatalf("Addr() before Serve = %q, want configured addr", srv.Addr())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	addr := waitForAddr(t, srv)
	if addr == "127.0.0.1:0" {
		t.Fatal("Addr() after Serve still reports the wildcard port")
	}
	cancel()
	<-done
}

// waitForAddr polls until Serve has bound a concrete address.
func waitForAddr(t *testing.T, srv *Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := srv.Addr(); a != "" && a != "127.0.0.1:0" {
			return a
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not bind an address in time")
	return ""
}
