// stream.go owns the API.md §3.7 Server-Sent Events endpoint,
// GET /api/v1/observability/events — the browser push wire (r3 §2A
// surface→transport map). The route is registered by Mount.routes() in
// handlers.go; this file owns the long-lived handler.
//
// Each connection subscribes to the t04 broker (events.Broker.Subscribe),
// then relays every delivered Event as one SSE frame:
//
//	event: <topic>\n
//	id: <seq>\n
//	data: <json>\n
//	\n
//
// where <json> is the marshaled events.Event (the schema-mirrored struct).
// On connect the handler writes a ~2KB comment line so intermediary proxies
// flush their read buffers and the browser's EventSource fires `open`
// promptly. The stream ends when the request context is cancelled (client
// hung up / server shutdown) or the broker closes the subscriber channel
// (broker Close, or OQ5 disconnect-on-overflow — the client reconnects and
// refetches; no replay here, per spec D2.2 and this package's anti-scope).
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/dashboard/events"
)

// ssePadding is the connect-time comment payload. SSE comment lines start
// with ':' and are ignored by EventSource; ~2KB defeats proxy/read-buffer
// batching so the first real frame is not withheld behind a fill threshold.
const ssePadding = 2048

// handleEvents serves GET {base}/events as a text/event-stream (API.md §3.7).
// It requires a broker (503 otherwise) and a flushable writer (500
// otherwise), subscribes for the request's lifetime, and relays broker
// Events as SSE frames until the context is cancelled or the stream closes.
func (m *Mount) handleEvents(w http.ResponseWriter, r *http.Request) {
	if m.broker == nil {
		m.writeError(w, http.StatusServiceUnavailable, codeInternal,
			"event stream unavailable in this deployment")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		m.writeError(w, http.StatusInternalServerError, codeInternal,
			"streaming unsupported")
		return
	}

	// Subscribe before any bytes reach the client: once the connect-time
	// padding is flushed, the caller knows the subscription is live, so an
	// event published the instant the padding arrives cannot race ahead of
	// registration and be dropped (broker delivery is at-most-once).
	stream, cancel := m.broker.Subscribe(r.Context())
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Connect-time padding comment: flush the transport before the first
	// event so the EventSource open handshake never stalls behind a proxy
	// buffer fill threshold.
	if _, err := w.Write(paddingFrame()); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-stream:
			if !ok {
				// Broker closed this subscriber (Close or overflow
				// disconnect); the client reconnects and refetches.
				return
			}
			if err := writeEventFrame(w, ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// paddingFrame builds the connect-time SSE comment line (a ':'-prefixed
// line EventSource ignores).
func paddingFrame() []byte {
	return []byte(": " + strings.Repeat(" ", ssePadding) + "\n\n")
}

// writeEventFrame marshals ev and writes one SSE frame. Field order mirrors
// the wire contract: `event:` = topic, `id:` = per-connection seq, `data:` =
// the JSON-encoded Event; a blank line terminates the frame.
func writeEventFrame(w http.ResponseWriter, ev events.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", ev.Type, ev.Seq, data)
	return err
}
