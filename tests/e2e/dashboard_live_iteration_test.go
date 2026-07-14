// Package e2e holds cross-package end-to-end smokes that boot a real
// dashboard runtime over a loopback socket and drive it the way a browser
// would, rather than exercising a single package in isolation.
//
// dashboard_live_iteration_test.go is the t12 live-iteration proof: a
// filesystem iteration write under a watched iter-log root must surface on a
// connected Server-Sent-Events client — the exact transport the dashboard
// SPA's EventSource consumes (web/dashboard/src/api/eventStream.ts) — within
// the spec's 2-second propagation budget (API.md §3.7 / OQ3). It wires the
// whole standalone composition (store → SSE broker → REST/SSE handlers →
// iter-log filesystem watcher) via internal/dashboard/server, so the assertion
// covers the real push path end to end and not a hand-built stub.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/server"
)

// propagationBudget is the live-update latency ceiling this smoke asserts: a
// live iteration written to a watched iter-log root must reach a connected SSE
// client within 2 seconds. The watcher's OQ3 fallback poll runs at 1s and
// fsnotify is faster still, so the budget holds even when FSEvents latency
// eats the fsnotify margin — but the test measures and fails if it does not.
const propagationBudget = 2 * time.Second

// scoredTopic is the SSE event topic (and frame data type) a scored iteration
// publishes on the observability stream.
const scoredTopic = "iteration.scored"

// liveIterationRecord is a minimal but schema-valid iter-N.yaml record (the
// same shape as internal/dashboard/handlers/testdata/iterlog/iter-1.yaml) so
// the watcher can resolve the session id into the published event payload,
// letting the assertion below check both the iteration number and session id.
const liveIterationRecord = `schema_version: 2
iteration: 1
date: "2026-07-10"
wave: "w1"
task_id: "t12-e2e"
commit: "deadbeef"
files_changed: 1
lines_added: 3
lines_removed: 0
agent:
  session_id: "sess-live-e2e"
  harness: "claude-code"
  model: "m-claude"
impl:
  summary: "live iteration smoke"
  retries: 0
`

// TestLiveIterationPropagatesToSSEClientWithinBudget boots the standalone
// dashboard server over an ephemeral loopback port, connects one SSE client,
// writes a fresh iteration record into the watched root, and asserts the
// change arrives as an iteration.scored frame within the 2s propagation
// budget. This is the runnable, browser-free half of the t12 live-iteration
// smoke; the Playwright leg (web/dashboard/e2e/live-iteration.spec.ts) proves
// the same budget from inside a real browser's EventSource when the built
// front-end and a browser are available.
func TestLiveIterationPropagatesToSSEClientWithinBudget(t *testing.T) {
	iterLogDir := t.TempDir()

	srv, err := server.New(server.Config{
		// :0 binds an ephemeral port; Addr() resolves it after Start.
		Addr:        "127.0.0.1:0",
		IterLogDirs: []string{iterLogDir},
		// Recompute-on-miss (which runs git topology against RepoDir) is not on
		// the SSE push path, so point it at the temp dir to stay off the real
		// repo. The watcher only best-effort reads the record for its payload.
		RepoDir: iterLogDir,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	})

	baseURL := "http://" + srv.Addr()

	// Connect and read past the connect-time padding comment. Once the padding
	// arrives the server's broker.Subscribe has registered this client, so an
	// event published immediately after cannot race ahead of registration and
	// be dropped (broker delivery is at-most-once, no replay).
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	resp, reader := openSSEStream(t, streamCtx, baseURL)
	defer resp.Body.Close()

	// Emit the live iteration: a brand-new iter-1.yaml under the watched root,
	// written AFTER the subscription is live so the fan-out has a subscriber.
	// It did not exist at the watcher's startup baseline, so it publishes.
	start := time.Now()
	if err := os.WriteFile(filepath.Join(iterLogDir, "iter-1.yaml"), []byte(liveIterationRecord), 0o644); err != nil {
		t.Fatalf("write iteration record: %v", err)
	}

	// Read frames off the socket in the background so the propagation budget is
	// enforced by a select against a timer rather than a blocking read. The
	// channel is buffered so the reader never leaks if the budget expires: the
	// deferred cancel/close unblocks its read and it exits.
	type frameResult struct {
		fields map[string]string
		err    error
	}
	frames := make(chan frameResult, 1)
	go func() {
		for {
			fields, err := readSSEFrame(reader)
			if err != nil {
				frames <- frameResult{err: err}
				return
			}
			// A default-config broker heartbeats every 15s, well outside the
			// budget, but skip it defensively so a stray keepalive can never be
			// mistaken for the iteration event.
			if fields["event"] == "heartbeat" {
				continue
			}
			frames <- frameResult{fields: fields}
			return
		}
	}()

	select {
	case <-time.After(propagationBudget):
		t.Fatalf("live iteration did not reach the SSE client within the %s propagation budget", propagationBudget)
	case fr := <-frames:
		if fr.err != nil {
			t.Fatalf("read SSE frame: %v", fr.err)
		}
		elapsed := time.Since(start)

		if got := fr.fields["event"]; got != scoredTopic {
			t.Fatalf("SSE event topic = %q, want %q", got, scoredTopic)
		}

		var frame struct {
			Type    string `json:"type"`
			Seq     uint64 `json:"seq"`
			Payload struct {
				SessionID string `json:"session_id"`
				Iteration int    `json:"iteration"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(fr.fields["data"]), &frame); err != nil {
			t.Fatalf("unmarshal SSE data %q: %v", fr.fields["data"], err)
		}
		if frame.Type != scoredTopic {
			t.Errorf("frame data type = %q, want %q", frame.Type, scoredTopic)
		}
		if frame.Payload.Iteration != 1 {
			t.Errorf("payload.iteration = %d, want 1", frame.Payload.Iteration)
		}
		if frame.Payload.SessionID != "sess-live-e2e" {
			t.Errorf("payload.session_id = %q, want %q", frame.Payload.SessionID, "sess-live-e2e")
		}
		if elapsed > propagationBudget {
			t.Errorf("propagation took %s, want <= %s", elapsed, propagationBudget)
		}
		t.Logf("live iteration propagated to SSE client in %s (budget %s)", elapsed, propagationBudget)
	}
}

// openSSEStream dials {baseURL}/api/v1/observability/events, verifies the
// text/event-stream handshake, and returns the response plus a reader
// positioned just past the connect-time padding comment — i.e. at the point
// where the server has registered this subscriber.
func openSSEStream(t *testing.T, ctx context.Context, baseURL string) (*http.Response, *bufio.Reader) {
	t.Helper()
	url := baseURL + "/api/v1/observability/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		_ = resp.Body.Close()
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	reader := bufio.NewReader(resp.Body)
	// The padding frame is one ':'-prefixed comment line followed by the
	// frame-terminating blank line (see handlers.paddingFrame).
	comment, err := reader.ReadString('\n')
	if err != nil {
		_ = resp.Body.Close()
		t.Fatalf("read padding comment: %v", err)
	}
	if !strings.HasPrefix(comment, ": ") {
		_ = resp.Body.Close()
		t.Fatalf("first line = %q, want a ':'-prefixed comment", comment)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("read padding terminator: %v", err)
	}
	return resp, reader
}

// readSSEFrame reads one complete SSE frame (fields up to the blank
// terminator) into a field->value map, matching the wire shape t05 writes:
// `event: <topic>`, `id: <seq>`, `data: <json>`.
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
