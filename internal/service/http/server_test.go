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
	return New("127.0.0.1:0", sched, events.NewBus())
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
		{name: "shadows built-in tasks", prefix: "/api/tasks", wantErr: ErrOverlappingMount},
		{name: "parent of built-in tasks", prefix: "/api", wantErr: ErrOverlappingMount},
		{name: "equals built-in healthz", prefix: "/healthz", wantErr: ErrOverlappingMount},
		{name: "duplicate mount", setup: []string{"/dash"}, prefix: "/dash", wantErr: ErrOverlappingMount},
		{name: "child of existing mount", setup: []string{"/dash"}, prefix: "/dash/sub", wantErr: ErrOverlappingMount},
		{name: "parent of existing mount", setup: []string{"/dash/sub"}, prefix: "/dash", wantErr: ErrOverlappingMount},
		{name: "empty prefix", prefix: "", wantErr: ErrInvalidMountPrefix},
		{name: "unrooted prefix", prefix: "api", wantErr: ErrInvalidMountPrefix},
		{name: "sibling prefix ok", setup: []string{"/dash"}, prefix: "/reviews", wantErr: nil},
		{name: "non-segment lookalike ok", setup: []string{"/api/test"}, prefix: "/api/testing", wantErr: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(fakeState{})
			for _, p := range tc.setup {
				if err := srv.RegisterMount(p, h); err != nil {
					t.Fatalf("setup RegisterMount(%q): %v", p, err)
				}
			}
			err := srv.RegisterMount(tc.prefix, h)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("RegisterMount(%q) = %v, want nil", tc.prefix, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("RegisterMount(%q) = %v, want %v", tc.prefix, err, tc.wantErr)
			}
		})
	}
}

func TestRegisterMountNilHandler(t *testing.T) {
	srv := newTestServer(fakeState{})
	if err := srv.RegisterMount("/x", nil); !errors.Is(err, ErrNilMountHandler) {
		t.Fatalf("err = %v, want ErrNilMountHandler", err)
	}
}

func TestBusAccessor(t *testing.T) {
	bus := events.NewBus()
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

	srv := New(ln.Addr().String(), fakeState{}, events.NewBus())
	if err := srv.Serve(context.Background()); err == nil {
		t.Fatal("Serve on an occupied port returned nil, want listen error")
	}
}

func TestAddrBeforeAndAfterServe(t *testing.T) {
	srv := New("127.0.0.1:0", fakeState{}, events.NewBus())
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
