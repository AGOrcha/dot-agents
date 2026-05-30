// Package labels defines the human-review label data model and its YAML
// sidecar persistence. Labels live next to the scores they comment on,
// mirroring R1's iter-N.score.yaml sidecar pattern (internal/scoring/persist.go):
// the sidecar path is iter-N.labels.yaml in the same iteration-log directory.
//
// Per spec D5.1 the file is multi-label and append-on-edit: editing a label
// never destroys prior content, it appends an entry to that label's edit
// history. This package is pure (data model + file IO + CRUD + validation);
// it has no HTTP surface and is independent of internal/review/auth.
package labels

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// LabelSchemaVersion is the version of the structured-label schema, recorded
// on every label record so a future schema change (severity, reproducibility,
// …) can be migrated without ambiguity. Per spec OQ3 this is present from v1.
const LabelSchemaVersion = "1.0.0"

// FreeTextMaxLen bounds the human-readable comment per spec D5.7.
const FreeTextMaxLen = 4000

// errFmtWrapQuoted is the error format string for wrapping a value as a quoted string.
const errFmtWrapQuoted = "%w: %q"

// Role is the actor role recorded on a label. It mirrors the RBAC roles owned
// by internal/review/auth but is duplicated here as a plain string enum so
// this package stays independent of the auth package.
type Role string

const (
	RoleReviewer Role = "reviewer"
	RoleAdmin    Role = "admin"
	RoleReadonly Role = "readonly"
)

// ScopeJudgement is the enum-bounded scope dimension (spec D5.7).
type ScopeJudgement string

const (
	ScopeOnTarget ScopeJudgement = "on-target"
	ScopePartial  ScopeJudgement = "partial"
	ScopeBreach   ScopeJudgement = "breach"
)

// Hallucination is the enum-bounded hallucination dimension (spec D5.7).
type Hallucination string

const (
	HallucinationNone  Hallucination = "none"
	HallucinationMinor Hallucination = "minor"
	HallucinationMajor Hallucination = "major"
)

// CorrectnessMin / CorrectnessMax bound the correctness dimension (0–3, D5.7).
const (
	CorrectnessMin = 0
	CorrectnessMax = 3
)

// Structured is the enum-bounded structured judgement that feeds the
// human_label signal sub-score (spec D5.7). The free text is carried on the
// Label, not here, because only the structured dimensions are numeric.
type Structured struct {
	Correctness    int            `yaml:"correctness"`
	ScopeJudgement ScopeJudgement `yaml:"scope_judgement"`
	Hallucination  Hallucination  `yaml:"hallucination"`
}

// Edit is one entry in a label's append-only edit history. The first edit is
// the original submission; each subsequent edit records who changed the
// structured judgement / free text and when. The latest edit is the effective
// state of the label.
type Edit struct {
	Actor      string     `yaml:"actor"`
	Role       Role       `yaml:"role"`
	Timestamp  time.Time  `yaml:"timestamp"`
	Structured Structured `yaml:"structured"`
	FreeText   string     `yaml:"free_text,omitempty"`
}

// Label is one reviewer's judgement on one iteration. A label is multi-edit:
// History holds the original submission plus every later edit, oldest first.
// The effective structured judgement and free text are those of the latest
// edit; the convenience accessors Latest, EffectiveStructured and
// EffectiveFreeText return them.
type Label struct {
	// ID is a stable opaque identifier (hex, 32 chars) assigned at creation.
	ID string `yaml:"id"`
	// Iteration is the iteration this label comments on.
	Iteration int `yaml:"iteration"`
	// Actor is the original author's identity (e.g. email).
	Actor string `yaml:"actor"`
	// Role is the original author's role.
	Role Role `yaml:"role"`
	// AdminOverride marks a label that supersedes reviewer labels in
	// aggregation (spec D5.8). Set on an admin's own label, not on an admin
	// edit of a reviewer's label.
	AdminOverride bool `yaml:"admin_override,omitempty"`
	// SchemaVersion is the structured-label schema version (OQ3).
	SchemaVersion string `yaml:"schema_version"`
	// CreatedAt is the original submission time.
	CreatedAt time.Time `yaml:"created_at"`
	// UpdatedAt is the time of the latest edit (== CreatedAt for an unedited
	// label).
	UpdatedAt time.Time `yaml:"updated_at"`
	// History is the append-only edit history, oldest first. It always holds
	// at least the original submission.
	History []Edit `yaml:"history"`
}

// Latest returns the most recent edit. It panics only if History is empty,
// which a validated label never is; callers that may hold an unvalidated
// label should check len(History) first.
func (l Label) Latest() Edit {
	return l.History[len(l.History)-1]
}

// EffectiveStructured returns the structured judgement of the latest edit.
func (l Label) EffectiveStructured() Structured {
	return l.Latest().Structured
}

// EffectiveFreeText returns the free text of the latest edit.
func (l Label) EffectiveFreeText() string {
	return l.Latest().FreeText
}

// Sidecar is the on-disk YAML shape for one iteration's labels. It is the
// durable record consumed by the R5 collection endpoint and the R1
// human_label signal extractor, so the shape is explicit and stable.
type Sidecar struct {
	Iteration     int     `yaml:"iteration"`
	SchemaVersion string  `yaml:"schema_version"`
	Labels        []Label `yaml:"labels"`
}

// Validation errors. These are sentinel errors so callers (and tests) can
// match on them with errors.Is.
var (
	ErrEmptyID            = errors.New("labels: label id is empty")
	ErrEmptyActor         = errors.New("labels: label actor is empty")
	ErrIterationMismatch  = errors.New("labels: label iteration does not match sidecar")
	ErrNegativeIteration  = errors.New("labels: iteration is negative")
	ErrEmptyHistory       = errors.New("labels: label has no edit history")
	ErrCorrectnessRange   = errors.New("labels: correctness out of range")
	ErrInvalidRole        = errors.New("labels: invalid role")
	ErrInvalidScope       = errors.New("labels: invalid scope_judgement")
	ErrInvalidHalluc      = errors.New("labels: invalid hallucination")
	ErrFreeTextTooLong    = errors.New("labels: free_text exceeds max length")
	ErrDuplicateID        = errors.New("labels: duplicate label id")
	ErrLabelNotFound      = errors.New("labels: label id not found")
	ErrUnauthorizedEdit   = errors.New("labels: actor may not edit this label")
	ErrSchemaVersionEmpty = errors.New("labels: schema_version is empty")
)

func validRole(r Role) bool {
	switch r {
	case RoleReviewer, RoleAdmin, RoleReadonly:
		return true
	default:
		return false
	}
}

func validScope(s ScopeJudgement) bool {
	switch s {
	case ScopeOnTarget, ScopePartial, ScopeBreach:
		return true
	default:
		return false
	}
}

func validHallucination(h Hallucination) bool {
	switch h {
	case HallucinationNone, HallucinationMinor, HallucinationMajor:
		return true
	default:
		return false
	}
}

// validateStructured checks an enum-bounded structured judgement.
func validateStructured(s Structured) error {
	if s.Correctness < CorrectnessMin || s.Correctness > CorrectnessMax {
		return fmt.Errorf("%w: %d (want %d..%d)", ErrCorrectnessRange, s.Correctness, CorrectnessMin, CorrectnessMax)
	}
	if !validScope(s.ScopeJudgement) {
		return fmt.Errorf(errFmtWrapQuoted, ErrInvalidScope, s.ScopeJudgement)
	}
	if !validHallucination(s.Hallucination) {
		return fmt.Errorf(errFmtWrapQuoted, ErrInvalidHalluc, s.Hallucination)
	}
	return nil
}

// validateEdit checks one edit-history entry.
func validateEdit(e Edit) error {
	if strings.TrimSpace(e.Actor) == "" {
		return ErrEmptyActor
	}
	if !validRole(e.Role) {
		return fmt.Errorf(errFmtWrapQuoted, ErrInvalidRole, e.Role)
	}
	if len(e.FreeText) > FreeTextMaxLen {
		return fmt.Errorf("%w: %d > %d", ErrFreeTextTooLong, len(e.FreeText), FreeTextMaxLen)
	}
	return validateStructured(e.Structured)
}

// Validate checks a single label for structural and enum integrity. It does
// not check iteration alignment with a sidecar — that is Sidecar.Validate's
// job.
func (l Label) Validate() error {
	if strings.TrimSpace(l.ID) == "" {
		return ErrEmptyID
	}
	if strings.TrimSpace(l.Actor) == "" {
		return ErrEmptyActor
	}
	if !validRole(l.Role) {
		return fmt.Errorf(errFmtWrapQuoted, ErrInvalidRole, l.Role)
	}
	if l.Iteration < 0 {
		return ErrNegativeIteration
	}
	if strings.TrimSpace(l.SchemaVersion) == "" {
		return ErrSchemaVersionEmpty
	}
	if len(l.History) == 0 {
		return ErrEmptyHistory
	}
	for _, e := range l.History {
		if err := validateEdit(e); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the whole sidecar: each label is individually valid, every
// label's iteration matches the sidecar, and ids are unique.
func (s Sidecar) Validate() error {
	if s.Iteration < 0 {
		return ErrNegativeIteration
	}
	if strings.TrimSpace(s.SchemaVersion) == "" {
		return ErrSchemaVersionEmpty
	}
	seen := make(map[string]struct{}, len(s.Labels))
	for _, l := range s.Labels {
		if err := l.Validate(); err != nil {
			return err
		}
		if l.Iteration != s.Iteration {
			return fmt.Errorf("%w: label %s iteration %d != sidecar %d", ErrIterationMismatch, l.ID, l.Iteration, s.Iteration)
		}
		if _, dup := seen[l.ID]; dup {
			return fmt.Errorf("%w: %s", ErrDuplicateID, l.ID)
		}
		seen[l.ID] = struct{}{}
	}
	return nil
}

// randReader is the entropy source for newID. It is a package var (defaulting
// to crypto/rand) so tests can inject a failing reader to exercise the error
// path without a real entropy failure.
var randReader io.Reader = rand.Reader

// newID returns a 32-char hex identifier from crypto/rand. It uses the
// standard library only (no new module dependency this wave).
func newID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(randReader, b[:]); err != nil {
		return "", fmt.Errorf("labels: generate id: %w", err)
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0f]
	}
	return string(out), nil
}
