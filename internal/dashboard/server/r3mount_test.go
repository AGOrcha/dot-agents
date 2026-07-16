// r3mount_test.go proves the t13 R3-mount integration with the REAL
// collaborators: Mount stitches the dashboard onto an actual R3 HTTP/SSE edge
// (RegisterMount — full-path routing, coexisting with the built-in routes) and
// bridges the R3 EventBus so an R3 publish surfaces on the dashboard SSE
// stream — i.e. R3's in-process publish, not fswatch, is the primary source.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	svcevents "github.com/AGOrcha/dot-agents/internal/service/events"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// dashboardBase is the version-first dashboard API root (handlers.Prefix +
// API.md §1.1 prefix); the handlers package pins the literal, this mirrors it.
const dashboardBase = "/api/v1/observability"

// stubState satisfies the R3 server's StateProvider.
type stubState struct{}

func (stubState) State() []scheduler.TaskState { return nil }

// newEdge builds a real R3 HTTP/SSE edge over an in-process bus.
func newEdge(t *testing.T) *servicehttp.Server {
	t.Helper()
	bus := svcevents.NewInProcBus()
	t.Cleanup(func() { _ = bus.Close() })
	return servicehttp.New("127.0.0.1:0", stubState{}, bus)
}

func TestMountRegistersDashboardOnR3Edge(t *testing.T) {
	edge := newEdge(t)
	closer, err := Mount(edge, MountConfig{})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer func() { _ = closer.Close() }()

	// Dashboard REST route through the R3 mux (full path, nothing stripped).
	rr := httptest.NewRecorder()
	edge.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, dashboardBase+"/runs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard /runs through R3 mux = %d; body: %s", rr.Code, rr.Body)
	}
	var env struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body: %s", err, rr.Body)
	}

	// Built-in R3 route still served (most-specific-wins coexistence).
	rr = httptest.NewRecorder()
	edge.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("built-in /healthz = %d, want 200", rr.Code)
	}
}

func TestMountBridgesR3PublishToSSE(t *testing.T) {
	edge := newEdge(t)
	closer, err := Mount(edge, MountConfig{})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	defer func() { _ = closer.Close() }()

	ts := httptest.NewServer(edge.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, reader := openSSE(t, ts, ctx)
	defer resp.Body.Close()

	// Subscription is live once the padding is read; an R3 publish now flows
	// bus → bridge → broker → SSE without racing registration.
	if err := edge.Bus().Publish(svcevents.TopicRescoreDone, svcevents.RescoreDone{
		FromVersion: "1.0.0",
		ToVersion:   "7.7.7",
		IterCount:   3,
	}); err != nil {
		t.Fatalf("bus.Publish: %v", err)
	}

	type result struct {
		fields map[string]string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		f, err := readSSEFrame(reader)
		got <- result{f, err}
	}()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("read frame: %v", r.err)
		}
		// R3 rescore.done is TRANSLATED to the dashboard rubric.changed topic.
		if r.fields["event"] != "rubric.changed" {
			t.Fatalf("SSE event = %q, want rubric.changed", r.fields["event"])
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				RubricVersion string `json:"rubric_version"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(r.fields["data"]), &ev); err != nil {
			t.Fatalf("decode data: %v; data: %s", err, r.fields["data"])
		}
		if ev.Type != "rubric.changed" || ev.Payload.RubricVersion != "7.7.7" {
			t.Fatalf("bridged event = %+v, want rubric.changed / 7.7.7", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no SSE frame within budget; R3 publish did not reach the stream")
	}
}

func TestMountResolvesSessionFromDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-3.yaml"),
		[]byte("schema_version: 2\niteration: 3\nagent:\n  session_id: sess-abc\n"), 0o644); err != nil {
		t.Fatalf("write iter record: %v", err)
	}
	resolve := diskSessionResolver(discardLogger())

	if got := resolve(dir, 3); got != "sess-abc" {
		t.Errorf("resolve(dir, 3) = %q, want sess-abc", got)
	}
	// Missing record degrades to empty (best-effort), never an error.
	if got := resolve(dir, 99); got != "" {
		t.Errorf("resolve(missing) = %q, want empty", got)
	}
	if got := resolve("", 1); got != "" {
		t.Errorf("resolve(no dir) = %q, want empty", got)
	}
}

func TestMountRejectsNilEdgeAndBus(t *testing.T) {
	if _, err := Mount(nil, MountConfig{}); err != ErrNilEdge {
		t.Errorf("Mount(nil edge) err = %v, want ErrNilEdge", err)
	}
	// A real edge constructed with no bus is a wiring error, not REST-only.
	edge := servicehttp.New("127.0.0.1:0", stubState{}, nil)
	if _, err := Mount(edge, MountConfig{}); err != ErrNilBus {
		t.Errorf("Mount(nil bus) err = %v, want ErrNilBus", err)
	}
}

func TestMountCloseDisconnectsAndIsClean(t *testing.T) {
	edge := newEdge(t)
	closer, err := Mount(edge, MountConfig{})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Errorf("Close: %v, want nil", err)
	}
}

// openSSE dials the dashboard events endpoint and returns the response plus a
// reader positioned just past the connect-time padding comment — the point at
// which the server's Subscribe has registered this client.
func openSSE(t *testing.T, ts *httptest.Server, ctx context.Context) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+dashboardBase+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil { // padding comment
		t.Fatalf("read padding comment: %v", err)
	}
	if _, err := reader.ReadString('\n'); err != nil { // frame-terminating blank line
		t.Fatalf("read padding terminator: %v", err)
	}
	return resp, reader
}

// readSSEFrame reads one complete SSE frame into a field→value map.
func readSSEFrame(reader *bufio.Reader) (map[string]string, error) {
	fields := map[string]string{}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return fields, err
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			return fields, nil
		}
		name, value, found := strings.Cut(line, ": ")
		if !found {
			continue
		}
		fields[name] = value
	}
}

// discardLogger is a no-op structured logger for tests that do not assert logs.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
