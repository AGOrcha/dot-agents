// Package audit implements the R5 append-only, hash-chained audit log (design
// D5.4). Every mutating review action — label submit, label edit, user create,
// user delete, role change — writes exactly one JSON-lines record to
// .agents/active/review/audit.log.jsonl. Each record carries the SHA-256 of the
// previous record in prev_hash, giving a tamper-evident chain without an
// external ledger or database dependency.
//
// The package is pure file IO: it has no HTTP surface and no CLI. Downstream
// callers (the R5 collection endpoint's audit middleware and the admin CLI's
// `da review audit verify` command) consume Append and Verify defined here.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is stamped on every record so a future record-shape change can
// be migrated without ambiguity. It is part of the hashed content, so it also
// binds each record to the schema it was written under.
const SchemaVersion = "1.0.0"

// GenesisPrevHash is the prev_hash of the first record in a log file. An empty
// string marks the start of a chain; each rotated file begins a fresh chain so
// pruning an older year cannot break the active file's verification (design
// D5.4: prune compacts older years).
const GenesisPrevHash = ""

// Action is the mutating action a record describes. The closed set mirrors the
// mutating operations enumerated by spec requirement R6.
type Action string

const (
	// ActionLabelSubmit records a reviewer submitting a new label.
	ActionLabelSubmit Action = "label.submit"
	// ActionLabelEdit records an edit appended to an existing label.
	ActionLabelEdit Action = "label.edit"
	// ActionUserCreate records an admin creating a user.
	ActionUserCreate Action = "user.create"
	// ActionUserDelete records an admin removing a user.
	ActionUserDelete Action = "user.delete"
	// ActionRoleChange records an admin changing a user's role.
	ActionRoleChange Action = "role.change"
)

// validActions is the closed set an Event's action must belong to.
var validActions = map[Action]struct{}{
	ActionLabelSubmit: {},
	ActionLabelEdit:   {},
	ActionUserCreate:  {},
	ActionUserDelete:  {},
	ActionRoleChange:  {},
}

// Valid reports whether a is a recognized action.
func (a Action) Valid() bool {
	_, ok := validActions[a]
	return ok
}

// Record is one line of the audit log, serialized as a single JSON object. The
// field order is fixed and json.Marshal is deterministic for a struct, so the
// canonical bytes used for hashing are reproducible on read.
//
// SchemaVersion, PrevHash and Ts are stamped by Append; the remaining fields
// come from the caller-supplied Event.
type Record struct {
	SchemaVersion string    `json:"schema_version"`
	Ts            time.Time `json:"ts"`
	Actor         string    `json:"actor"`
	Role          string    `json:"role"`
	Action        Action    `json:"action"`
	Target        string    `json:"target"`
	BeforeHash    string    `json:"before_hash,omitempty"`
	AfterHash     string    `json:"after_hash,omitempty"`
	PrevHash      string    `json:"prev_hash"`
	RequestID     string    `json:"request_id,omitempty"`
}

// Event is the caller-facing input to Append. The chain fields (SchemaVersion,
// Ts, PrevHash) are filled by Append, not the caller; before/after hashes and
// the request id are optional context the caller may supply.
type Event struct {
	Actor      string
	Role       string
	Action     Action
	Target     string
	BeforeHash string
	AfterHash  string
	RequestID  string
	// Now, if non-zero, fixes the record timestamp (tests use this). Zero means
	// timeNow() (time.Now().UTC()).
	Now time.Time
}

// Validation / operation sentinel errors. These are sentinels so callers and
// tests can match with errors.Is.
var (
	ErrEmptyActor    = errors.New("audit: event actor is empty")
	ErrEmptyTarget   = errors.New("audit: event target is empty")
	ErrInvalidAction = errors.New("audit: unknown action")
	ErrMarshal       = errors.New("audit: marshal record")
)

// validate checks an event before it is chained and written.
func (e Event) validate() error {
	if strings.TrimSpace(e.Actor) == "" {
		return ErrEmptyActor
	}
	if strings.TrimSpace(e.Target) == "" {
		return ErrEmptyTarget
	}
	if !e.Action.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAction, e.Action)
	}
	return nil
}

// marshal is the seam over json.Marshal so the otherwise-unreachable marshal
// error branch is coverable in tests.
var marshal = json.Marshal

// decode parses one JSON-lines record. Unknown fields are rejected so a
// silently reshaped log line is caught rather than dropped.
func decode(raw []byte, r *Record) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(r)
}

// canonicalBytes returns the deterministic JSON encoding of a record. Because
// json.Marshal encodes struct fields in declaration order and the record holds
// no maps, the bytes are stable across write and re-read — the property the
// hash chain depends on.
func canonicalBytes(r Record) ([]byte, error) {
	b, err := marshal(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMarshal, err)
	}
	return b, nil
}

// hashRecord returns the hex-encoded SHA-256 of a record's canonical bytes.
// This is the value the next record stores in prev_hash, linking the chain.
func hashRecord(r Record) (string, error) {
	b, err := canonicalBytes(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
