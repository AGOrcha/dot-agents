package journal

import "encoding/json"

// EventType marks how much of the command's effect a record carries (spec
// "Common envelope" → event_type).
type EventType string

const (
	// EventDurableDelta is a command that produced a durable, replayable state
	// transition; observed carries the full typed delta (Tier-1).
	EventDurableDelta EventType = "durable_delta"
	// EventInputOnly is a command whose durable effect is recoverable from the
	// snapshot or an underlying file, so only the invoked input is recorded
	// (Tier-2 records observed:{changed_fields}; some commands record input only).
	EventInputOnly EventType = "input_only"
	// EventFailed records that an attempted command did NOT succeed. Per R1 the
	// journal never fabricates an observed delta for a failure; a failed event
	// carries the input and the reason, never a false observed.
	EventFailed EventType = "failed"
)

// Actor identifies which agent role appended the event (spec envelope → actor).
type Actor string

const (
	// ActorMain is the primary interactive session.
	ActorMain Actor = "main"
	// ActorLoopWorker is a bounded delegation worker.
	ActorLoopWorker Actor = "loop-worker"
	// ActorOrchestrator is the loop orchestrator above the workers.
	ActorOrchestrator Actor = "orchestrator"
)

// Envelope is one journal record: a single JSON object serialized to one NDJSON
// line in events.log. It is the common shape every journaled command shares
// (spec "Common envelope"). The per-command typed Input/Observed schemas land in
// a later task; here Input and Observed stay json.RawMessage so callers can carry
// any payload and the later task can specialize without reshaping the envelope.
//
// Schema/Version/TS/Seq are stamped by Emit; callers set Actor, Command,
// EventType, and the payloads. Marshaling is plain encoding/json — no custom
// MarshalJSON — so a round-trip through json is lossless and order-stable enough
// for NDJSON.
type Envelope struct {
	// Schema/Version namespace and version the record (stamped by Emit).
	Schema  string `json:"schema"`
	Version int    `json:"version"`

	// TS is the RFC3339 UTC nanosecond timestamp — the primary replay ordering
	// key. Stamped by Emit when empty.
	TS string `json:"ts"`
	// Seq is a process-monotonic counter, the tiebreaker for records sharing a
	// TS. Stamped by Emit.
	Seq int64 `json:"seq"`

	// Actor is the agent role that produced the event.
	Actor Actor `json:"actor"`
	// Command is the canonical command name, e.g. "workflow advance".
	Command string `json:"command"`
	// CwdRepo is the repo identity/path the command ran against. The journal may
	// span repos (sweep/drift). Stamped by Emit from the repo fingerprint when
	// the caller leaves it empty.
	CwdRepo string `json:"cwd_repo"`
	// EventType selects durable_delta / input_only / failed.
	EventType EventType `json:"event_type"`

	// Input is the typed flags the command was invoked with (opaque here).
	Input json.RawMessage `json:"input,omitempty"`
	// Observed is the typed durable delta the command produced (opaque here).
	// Always absent on an EventFailed record (R1: never a false observed).
	Observed json.RawMessage `json:"observed,omitempty"`
}

// MarshalLine serializes the envelope to a single NDJSON line: the compact JSON
// object followed by exactly one '\n', returned as one buffer so the appender
// can write it in a single os.Write (no torn lines). json.Marshal already emits
// no interior newlines for these field types, keeping the record one line.
func (e Envelope) MarshalLine() ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
