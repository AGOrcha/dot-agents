// stream_test.go proves the §3.7 SSE endpoint end to end over a real socket:
// an EventSource-like client connects, the connect-time padding comment
// arrives, one broker publish surfaces as a well-formed SSE frame within the
// latency budget, and the request context governs the connection lifetime.
package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
)

const frameBudget = 100 * time.Millisecond

// newStreamMount builds a Mount wired to broker over a canned store.
func newStreamMount(t *testing.T, broker *events.Broker) *Mount {
	t.Helper()
	m, err := New(Deps{Store: &stubStore{}, Broker: broker})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// openStream dials the events endpoint and returns the response plus a reader
// positioned just past the connect-time padding comment — i.e. at the point
// where the server's Subscribe has already registered this client.
func openStream(t *testing.T, ts *httptest.Server, ctx context.Context) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+basePath+"/events", nil)
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
	comment, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read padding comment: %v", err)
	}
	if !strings.HasPrefix(comment, ": ") {
		t.Fatalf("first line = %q, want a ':'-prefixed comment", comment)
	}
	if _, err := reader.ReadString('\n'); err != nil { // frame-terminating blank line
		t.Fatalf("read padding terminator: %v", err)
	}
	return resp, reader
}

// readFrame reads one complete SSE frame (fields up to the blank terminator)
// into a field→value map, returning what it managed to read on error.
func readFrame(reader *bufio.Reader) (map[string]string, error) {
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

// TestEventsStreamDeliversFrame is the acceptance proof: over an httptest
// server, an EventSource-like reader receives a single broker publish as a
// correctly framed SSE event (event/id/data) within frameBudget.
func TestEventsStreamDeliversFrame(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1}) // no heartbeat noise
	defer broker.Close()

	ts := httptest.NewServer(newStreamMount(t, broker))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, reader := openStream(t, ts, ctx)
	defer resp.Body.Close()

	// Subscription is live once the padding was read; publishing now cannot
	// race registration.
	broker.Publish(events.TopicIterationScored, map[string]any{
		"session_id": "sess-a",
		"iteration":  1,
	})

	type result struct {
		fields map[string]string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		f, err := readFrame(reader)
		got <- result{f, err}
	}()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("read frame: %v", r.err)
		}
		if r.fields["event"] != events.TopicIterationScored {
			t.Fatalf("event field = %q, want %q", r.fields["event"], events.TopicIterationScored)
		}
		if r.fields["id"] != "0" {
			t.Fatalf("id field = %q, want %q (first per-connection seq)", r.fields["id"], "0")
		}
		var ev events.Event
		if err := json.Unmarshal([]byte(r.fields["data"]), &ev); err != nil {
			t.Fatalf("data is not a JSON Event: %v; raw: %q", err, r.fields["data"])
		}
		if ev.Type != events.TopicIterationScored || ev.Seq != 0 {
			t.Fatalf("data Event = {Type:%q Seq:%d}, want {%q 0}", ev.Type, ev.Seq, events.TopicIterationScored)
		}
		payload, ok := ev.Payload.(map[string]any)
		if !ok || payload["session_id"] != "sess-a" {
			t.Fatalf("data payload = %#v, want session_id=sess-a", ev.Payload)
		}
	case <-time.After(frameBudget):
		t.Fatalf("no SSE frame within %s", frameBudget)
	}
}

// TestEventsStreamClosesOnContextCancel pins the lifetime contract: cancelling
// the request context ends the handler and closes the response body, so the
// client's read unblocks with EOF-class error rather than hanging.
func TestEventsStreamClosesOnContextCancel(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1})
	defer broker.Close()

	ts := httptest.NewServer(newStreamMount(t, broker))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	resp, reader := openStream(t, ts, ctx)
	defer resp.Body.Close()

	done := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\n')
		done <- err
	}()

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read returned nil error after context cancel, want a read failure")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not unblock within 1s of context cancel")
	}
}

// TestEventsWithoutBrokerIsUnavailable pins the optional-broker composition:
// a Mount built without a broker still answers /events, with 503.
func TestEventsWithoutBrokerIsUnavailable(t *testing.T) {
	m, err := New(Deps{Store: &stubStore{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, basePath+"/events", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no-broker /events = %d, want 503; body: %s", rr.Code, rr.Body)
	}
}

// TestHandleEventsRequiresFlusher pins the streaming-capability guard: some
// ResponseWriter implementations do not implement http.Flusher. handleEvents
// must refuse those with 500 rather than silently buffering (and never
// flushing) an SSE stream. This calls handleEvents directly rather than
// through Mount.ServeHTTP: every routed request passes through the logged
// middleware's statusRecorder (see handlers.go), which implements Flush
// unconditionally (a no-op when the underlying writer can't flush) to stay
// transparent to this very handler — so the public route can never actually
// observe a non-Flusher writer. The guard is real defense for any writer
// that reaches handleEvents outside that middleware chain.
func TestHandleEventsRequiresFlusher(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1})
	defer broker.Close()
	m := newStreamMount(t, broker)

	rec := httptest.NewRecorder()
	w := struct{ http.ResponseWriter }{rec} // strips the promoted Flush method
	req := httptest.NewRequest(http.MethodGet, basePath+"/events", nil)

	m.handleEvents(w, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "streaming unsupported") {
		t.Fatalf("body = %q, want it to mention streaming unsupported", rec.Body.String())
	}
}

// failingWriteRecorder wraps httptest.ResponseRecorder but fails every
// Write, simulating a client that has already hung up (broken pipe) before
// the connect-time padding frame reaches the wire. Header/WriteHeader/Flush
// are promoted from the embedded recorder so the handler's Flusher check
// still passes and the write failure is the only thing under test.
type failingWriteRecorder struct {
	*httptest.ResponseRecorder
}

func (w *failingWriteRecorder) Write([]byte) (int, error) {
	return 0, errors.New("simulated broken pipe")
}

// TestHandleEventsReturnsWhenPaddingWriteFails pins the connect-time write
// error path: if the padding frame write fails, the handler must return
// immediately (never enter the event loop) and release the broker
// subscription it had already registered — no goroutine or subscriber-slot
// leak on a client that disconnects mid-handshake.
func TestHandleEventsReturnsWhenPaddingWriteFails(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1})
	defer broker.Close()
	m := newStreamMount(t, broker)

	w := &failingWriteRecorder{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, basePath+"/events", nil)

	done := make(chan struct{})
	go func() {
		m.ServeHTTP(w, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleEvents did not return after a padding-frame write failure")
	}

	if got := broker.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count after failed write = %d, want 0 (subscription released via defer cancel)", got)
	}
}

// TestEventsStreamEndsWhenBrokerCloses pins the other half of the lifetime
// contract (see TestEventsStreamClosesOnContextCancel for the context-cancel
// half): closing the broker itself — independent of the request context —
// must end every live stream (broker Close, or the OQ5 disconnect-on-overflow
// path this mirrors), so the client's read unblocks rather than hanging.
func TestEventsStreamEndsWhenBrokerCloses(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1})
	ts := httptest.NewServer(newStreamMount(t, broker))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, reader := openStream(t, ts, ctx)
	defer resp.Body.Close()

	done := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\n')
		done <- err
	}()

	broker.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read returned nil error after broker.Close(), want a read failure (stream end)")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not unblock within 1s of broker.Close()")
	}
}

// TestWriteEventFrameReturnsMarshalError pins writeEventFrame's contract on
// a payload json.Marshal cannot encode (e.g. a producer bug that stuffs a
// non-finite float into the topic-specific payload): it must return the
// marshal error without writing any bytes to the response, so callers can
// treat "wrote nothing, got an error" as safe to abandon the frame.
func TestWriteEventFrameReturnsMarshalError(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := events.Event{Type: "test.topic", Seq: 0, Payload: math.Inf(1)}

	if err := writeEventFrame(rec, ev); err == nil {
		t.Fatal("writeEventFrame with an unmarshalable (+Inf) payload returned nil error, want a json marshal error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("writeEventFrame wrote %d bytes before failing marshal, want 0 (marshal happens before any write)", rec.Body.Len())
	}
}

// TestEventsStreamEndsOnUnmarshalablePayload proves the end-to-end
// consequence of TestWriteEventFrameReturnsMarshalError through the real
// broker→handler pipeline: a delivered event whose payload cannot be
// JSON-marshaled ends the stream (handleEvents bails out of writeEventFrame's
// error) rather than hanging or emitting a malformed frame.
func TestEventsStreamEndsOnUnmarshalablePayload(t *testing.T) {
	broker := events.New(events.Options{Heartbeat: -1})
	defer broker.Close()
	ts := httptest.NewServer(newStreamMount(t, broker))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, reader := openStream(t, ts, ctx)
	defer resp.Body.Close()

	broker.Publish(events.TopicIterationScored, math.Inf(1))

	done := make(chan error, 1)
	go func() {
		_, err := reader.ReadString('\n')
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("read returned nil error after an unmarshalable event payload, want the stream to end rather than deliver a broken frame")
		}
	case <-time.After(frameBudget):
		t.Fatal("stream did not end within frameBudget after an unmarshalable event payload")
	}
}
