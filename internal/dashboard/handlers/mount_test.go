// mount_test.go proves the §2A edge placement with the real collaborators:
// the mount stitches onto the actual R3 HTTP/SSE edge via RegisterMount
// (full-path routing, no prefix stripping, coexisting with the built-in
// routes) and answers over a real TCP socket — mirroring the review mount's
// integration test.
package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/dashboard/store"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// stubState satisfies the R3 server's StateProvider.
type stubState struct{}

// State returns no scheduler tasks; the built-in route just needs a provider.
func (stubState) State() []scheduler.TaskState { return nil }

// TestRegisterMountIntegration mounts the dashboard on the real R3 server
// exactly as t13 will: RegisterMount(m.Prefix(), m).
func TestRegisterMountIntegration(t *testing.T) {
	m := newFixtureMount(t)
	srv := servicehttp.New("127.0.0.1:0", stubState{}, nil)
	if err := srv.RegisterMount(m.Prefix(), m); err != nil {
		t.Fatalf("RegisterMount(%q): %v", m.Prefix(), err)
	}

	// Dashboard route through the R3 mux (full path, nothing stripped).
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, basePath+"/runs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard route through R3 mux = %d; body: %s", rr.Code, rr.Body)
	}
	assertEnvelope(t, rr, true)

	// Built-in R3 route still served (most-specific-wins coexistence).
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("built-in /healthz = %d, want 200", rr.Code)
	}
}

// TestRealSocketRoundTrip serves the mount over a real TCP listener and
// drives it with a plain HTTP client.
func TestRealSocketRoundTrip(t *testing.T) {
	m := newFixtureMount(t)
	ts := httptest.NewServer(m)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + basePath + "/runs")
	if err != nil {
		t.Fatalf("GET over socket: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data []store.RunSummary `json:"data"`
		Meta struct {
			Count *int `json:"count"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v; body: %s", err, body)
	}
	if len(env.Data) != 2 || env.Meta.Count == nil || *env.Meta.Count != 2 {
		t.Fatalf("runs over socket = %d items (count %v), want 2", len(env.Data), env.Meta.Count)
	}
}
