package http

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"time"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

// submitLabelPayload is the POST body for a new label (spec D5.7 dimensions).
// Correctness is a pointer so a missing field is distinguishable from a
// legitimate 0.
type submitLabelPayload struct {
	Correctness    *int   `json:"correctness"`
	ScopeJudgement string `json:"scope_judgement"`
	Hallucination  string `json:"hallucination"`
	FreeText       string `json:"free_text"`
	// AdminOverride marks an admin's own label as superseding the reviewer
	// mean (spec D5.8 / OQ2). Requires the labels:admin permission.
	AdminOverride bool `json:"admin_override"`
}

// editLabelPayload is the PATCH body for an existing label. There is no
// admin_override here: per OQ2 an admin edit of a reviewer's label stays the
// reviewer's label (with audit attribution); override status is set only at
// submission of the admin's own label.
type editLabelPayload struct {
	Correctness    *int   `json:"correctness"`
	ScopeJudgement string `json:"scope_judgement"`
	Hallucination  string `json:"hallucination"`
	FreeText       string `json:"free_text"`
}

// labelEditJSON is one edit-history entry in a label response.
type labelEditJSON struct {
	Actor          string    `json:"actor"`
	Role           string    `json:"role"`
	Timestamp      time.Time `json:"timestamp"`
	Correctness    int       `json:"correctness"`
	ScopeJudgement string    `json:"scope_judgement"`
	Hallucination  string    `json:"hallucination"`
	FreeText       string    `json:"free_text,omitempty"`
}

// labelJSON is the response shape for one label. label_schema_version is
// carried on every record per OQ3.
type labelJSON struct {
	ID                 string          `json:"id"`
	Iteration          int             `json:"iteration"`
	Actor              string          `json:"actor"`
	Role               string          `json:"role"`
	AdminOverride      bool            `json:"admin_override,omitempty"`
	LabelSchemaVersion string          `json:"label_schema_version"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	History            []labelEditJSON `json:"history"`
}

// labelListJSON is the GET response for an iteration's labels.
type labelListJSON struct {
	Iteration          int         `json:"iteration"`
	LabelSchemaVersion string      `json:"label_schema_version"`
	Labels             []labelJSON `json:"labels"`
}

// toLabelJSON converts a persisted label to its response DTO.
func toLabelJSON(l labels.Label) labelJSON {
	history := make([]labelEditJSON, 0, len(l.History))
	for _, e := range l.History {
		history = append(history, labelEditJSON{
			Actor:          e.Actor,
			Role:           string(e.Role),
			Timestamp:      e.Timestamp,
			Correctness:    e.Structured.Correctness,
			ScopeJudgement: string(e.Structured.ScopeJudgement),
			Hallucination:  string(e.Structured.Hallucination),
			FreeText:       e.FreeText,
		})
	}
	return labelJSON{
		ID:                 l.ID,
		Iteration:          l.Iteration,
		Actor:              l.Actor,
		Role:               string(l.Role),
		AdminOverride:      l.AdminOverride,
		LabelSchemaVersion: l.SchemaVersion,
		CreatedAt:          l.CreatedAt,
		UpdatedAt:          l.UpdatedAt,
		History:            history,
	}
}

// toLabelJSONs maps a label slice to DTOs (always non-nil for JSON []).
func toLabelJSONs(ls []labels.Label) []labelJSON {
	out := make([]labelJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, toLabelJSON(l))
	}
	return out
}

// labelBadRequestErrs are the labels-package validation sentinels that map to
// a 400 (the client sent an out-of-range or malformed dimension).
var labelBadRequestErrs = []error{
	labels.ErrCorrectnessRange,
	labels.ErrInvalidScope,
	labels.ErrInvalidHalluc,
	labels.ErrFreeTextTooLong,
	labels.ErrInvalidRole,
	labels.ErrNegativeIteration,
}

// isBadLabelRequest reports whether err is a client-side label validation
// failure.
func isBadLabelRequest(err error) bool {
	for _, target := range labelBadRequestErrs {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// writeLabelError maps a labels-package error to the right HTTP status.
func writeLabelError(w nethttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, labels.ErrLabelNotFound):
		writeError(w, nethttp.StatusNotFound, err.Error(), "")
	case errors.Is(err, labels.ErrUnauthorizedEdit):
		writeError(w, nethttp.StatusForbidden, err.Error(), "")
	case isBadLabelRequest(err):
		writeError(w, nethttp.StatusBadRequest, err.Error(), "")
	default:
		writeError(w, nethttp.StatusInternalServerError, err.Error(), "")
	}
}

// labelTarget is the audit-record target for a label mutation.
func labelTarget(iteration int, labelID string) string {
	return fmt.Sprintf("iteration/%d/label/%s", iteration, labelID)
}

// structuredFrom builds the labels-package structured judgement from payload
// fields; range/enum validation is owned by the labels package.
func structuredFrom(correctness int, scope, hallucination string) labels.Structured {
	return labels.Structured{
		Correctness:    correctness,
		ScopeJudgement: labels.ScopeJudgement(scope),
		Hallucination:  labels.Hallucination(hallucination),
	}
}

// handleListLabels serves GET {prefix}/runs/{iteration}/labels.
func (m *Mount) handleListLabels(w nethttp.ResponseWriter, r *nethttp.Request) {
	iter, ok := iterationParam(w, r)
	if !ok {
		return
	}
	ls, err := m.labels.List(iter)
	if err != nil {
		writeLabelError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, labelListJSON{
		Iteration:          iter,
		LabelSchemaVersion: labels.LabelSchemaVersion,
		Labels:             toLabelJSONs(ls),
	})
}

// handleSubmitLabel serves POST {prefix}/runs/{iteration}/labels (spec R1).
func (m *Mount) handleSubmitLabel(w nethttp.ResponseWriter, r *nethttp.Request) {
	iter, ok := iterationParam(w, r)
	if !ok {
		return
	}
	id, ok := mustIdentity(w, r)
	if !ok {
		return
	}
	var p submitLabelPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.Correctness == nil {
		writeError(w, nethttp.StatusBadRequest, "correctness is required", "")
		return
	}
	if p.AdminOverride && !id.Can(auth.PermAdminLabels) {
		writeError(w, nethttp.StatusForbidden, "admin_override requires the admin role", "")
		return
	}
	created, err := m.labels.Add(iter, labels.AddInput{
		Actor:         id.Email,
		Role:          labels.Role(id.Role),
		Structured:    structuredFrom(*p.Correctness, p.ScopeJudgement, p.Hallucination),
		FreeText:      p.FreeText,
		AdminOverride: p.AdminOverride,
	})
	if err != nil {
		writeLabelError(w, err)
		return
	}
	dto := toLabelJSON(created)
	stageAudit(r.Context(), audit.Event{
		Actor:     id.Email,
		Role:      string(id.Role),
		Action:    audit.ActionLabelSubmit,
		Target:    labelTarget(iter, created.ID),
		AfterHash: hashJSON(dto),
	})
	writeJSON(w, nethttp.StatusCreated, dto)
}

// handleEditLabel serves PATCH {prefix}/runs/{iteration}/labels/{label_id}
// (spec R2). Ownership enforcement (author-or-admin) is the labels package's
// EditLabel rule; per OQ2 an admin edit stays attributed to the admin in the
// label's history and in the audit record, while the label remains the
// original reviewer's.
func (m *Mount) handleEditLabel(w nethttp.ResponseWriter, r *nethttp.Request) {
	iter, ok := iterationParam(w, r)
	if !ok {
		return
	}
	id, ok := mustIdentity(w, r)
	if !ok {
		return
	}
	var p editLabelPayload
	if !decodeJSON(w, r, &p) {
		return
	}
	if p.Correctness == nil {
		writeError(w, nethttp.StatusBadRequest, "correctness is required", "")
		return
	}
	labelID := r.PathValue("label_id")
	before, err := m.labels.Get(iter, labelID)
	if err != nil {
		writeLabelError(w, err)
		return
	}
	updated, err := m.labels.Edit(iter, labelID, labels.EditInput{
		Actor:      id.Email,
		Role:       labels.Role(id.Role),
		Structured: structuredFrom(*p.Correctness, p.ScopeJudgement, p.Hallucination),
		FreeText:   p.FreeText,
	})
	if err != nil {
		writeLabelError(w, err)
		return
	}
	dto := toLabelJSON(updated)
	stageAudit(r.Context(), audit.Event{
		Actor:      id.Email,
		Role:       string(id.Role),
		Action:     audit.ActionLabelEdit,
		Target:     labelTarget(iter, labelID),
		BeforeHash: hashJSON(toLabelJSON(before)),
		AfterHash:  hashJSON(dto),
	})
	writeJSON(w, nethttp.StatusOK, dto)
}
