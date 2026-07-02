package http

import (
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

const validLabelBody = `{"correctness":2,"scope_judgement":"on-target","hallucination":"none","free_text":"solid"}`

// TestSubmitLabelHappyPath covers spec R1: a reviewer POST persists a sidecar
// label, returns 201 with the DTO, and lands exactly one chained audit record.
func TestSubmitLabelHappyPath(t *testing.T) {
	env := newTestEnv(t)
	rr := env.do(nethttp.MethodPost, labelsPath(7), tokReviewer, validLabelBody)
	wantStatus(t, rr, nethttp.StatusCreated)

	var got labelJSON
	decodeBody(t, rr, &got)
	if got.ID == "" || got.Iteration != 7 || got.Actor != idReviewer.Email {
		t.Fatalf("unexpected label DTO: %+v", got)
	}
	if got.LabelSchemaVersion != labels.LabelSchemaVersion {
		t.Fatalf("label_schema_version = %q, want %q (OQ3)", got.LabelSchemaVersion, labels.LabelSchemaVersion)
	}
	if len(got.History) != 1 || got.History[0].Correctness != 2 {
		t.Fatalf("unexpected history: %+v", got.History)
	}

	// Sidecar persisted.
	stored, err := labels.Get(env.iterDir, 7, got.ID)
	if err != nil {
		t.Fatalf("sidecar read-back: %v", err)
	}
	if stored.Actor != idReviewer.Email {
		t.Fatalf("stored actor = %q", stored.Actor)
	}

	// One audit record, chained clean, request id echoed.
	recs := env.auditRecords()
	if len(recs) != 1 || recs[0].Action != audit.ActionLabelSubmit {
		t.Fatalf("audit records: %+v", recs)
	}
	if recs[0].AfterHash == "" || recs[0].Actor != idReviewer.Email {
		t.Fatalf("audit record incomplete: %+v", recs[0])
	}
	reqID := rr.Header().Get(HeaderRequestID)
	if reqID == "" || recs[0].RequestID != reqID {
		t.Fatalf("request id mismatch: header %q vs record %q", reqID, recs[0].RequestID)
	}
	if res, err := env.auditLog.Verify(); err != nil || !res.OK {
		t.Fatalf("audit verify: %+v, %v", res, err)
	}
}

// TestSubmitLabelValidation walks the client-error branches of the POST route.
func TestSubmitLabelValidation(t *testing.T) {
	env := newTestEnv(t)
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"non-integer iteration", DefaultPrefix + "/runs/abc/labels", validLabelBody, nethttp.StatusBadRequest},
		{"negative iteration", DefaultPrefix + "/runs/-1/labels", validLabelBody, nethttp.StatusBadRequest},
		{"malformed json", labelsPath(1), `{"correctness":`, nethttp.StatusBadRequest},
		{"unknown field", labelsPath(1), `{"correctness":1,"scope_judgement":"partial","hallucination":"none","surprise":true}`, nethttp.StatusBadRequest},
		{"missing correctness", labelsPath(1), `{"scope_judgement":"partial","hallucination":"none"}`, nethttp.StatusBadRequest},
		{"correctness out of range", labelsPath(1), `{"correctness":9,"scope_judgement":"partial","hallucination":"none"}`, nethttp.StatusBadRequest},
		{"invalid scope enum", labelsPath(1), `{"correctness":1,"scope_judgement":"sideways","hallucination":"none"}`, nethttp.StatusBadRequest},
		{"invalid hallucination enum", labelsPath(1), `{"correctness":1,"scope_judgement":"partial","hallucination":"lots"}`, nethttp.StatusBadRequest},
		{"free text too long", labelsPath(1), `{"correctness":1,"scope_judgement":"partial","hallucination":"none","free_text":"` + strings.Repeat("x", labels.FreeTextMaxLen+1) + `"}`, nethttp.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := env.do(nethttp.MethodPost, tc.path, tokReviewer, tc.body)
			wantStatus(t, rr, tc.want)
		})
	}
	if recs := env.auditRecords(); len(recs) != 0 {
		t.Fatalf("rejected requests must not write audit records, got %d", len(recs))
	}
}

// TestSubmitLabelAdminOverride covers OQ2: only an admin may mark their own
// label as the override that supersedes the reviewer mean.
func TestSubmitLabelAdminOverride(t *testing.T) {
	env := newTestEnv(t)
	body := `{"correctness":1,"scope_judgement":"breach","hallucination":"major","admin_override":true}`

	rr := env.do(nethttp.MethodPost, labelsPath(3), tokReviewer, body)
	wantStatus(t, rr, nethttp.StatusForbidden)

	rr = env.do(nethttp.MethodPost, labelsPath(3), tokAdmin, body)
	wantStatus(t, rr, nethttp.StatusCreated)
	var got labelJSON
	decodeBody(t, rr, &got)
	if !got.AdminOverride {
		t.Fatal("admin_override not set on admin's own label")
	}
}

// TestEditLabelOwnAndAdmin covers spec R2 and OQ2: the author may edit their
// own label; an admin edit of a reviewer's label stays the reviewer's label
// with the admin attributed in history and audit.
func TestEditLabelOwnAndAdmin(t *testing.T) {
	env := newTestEnv(t)
	id := env.submitLabel(tokReviewer, 4)

	edit := `{"correctness":3,"scope_judgement":"partial","hallucination":"minor","free_text":"revised"}`
	rr := env.do(nethttp.MethodPatch, labelsPath(4)+"/"+id, tokReviewer, edit)
	wantStatus(t, rr, nethttp.StatusOK)
	var got labelJSON
	decodeBody(t, rr, &got)
	if len(got.History) != 2 || got.History[1].Correctness != 3 {
		t.Fatalf("history after own edit: %+v", got.History)
	}

	adminEdit := `{"correctness":0,"scope_judgement":"breach","hallucination":"major","free_text":"admin correction"}`
	rr = env.do(nethttp.MethodPatch, labelsPath(4)+"/"+id, tokAdmin, adminEdit)
	wantStatus(t, rr, nethttp.StatusOK)
	decodeBody(t, rr, &got)
	if got.Actor != idReviewer.Email {
		t.Fatalf("admin edit reassigned label ownership to %q (OQ2 violation)", got.Actor)
	}
	if latest := got.History[len(got.History)-1]; latest.Actor != idAdmin.Email {
		t.Fatalf("admin edit not attributed in history: %+v", latest)
	}

	recs := env.auditRecords()
	if len(recs) != 3 {
		t.Fatalf("expected 3 audit records (submit + 2 edits), got %d", len(recs))
	}
	last := recs[2]
	if last.Action != audit.ActionLabelEdit || last.Actor != idAdmin.Email {
		t.Fatalf("audit attribution for admin edit: %+v", last)
	}
	if last.BeforeHash == "" || last.AfterHash == "" || last.BeforeHash == last.AfterHash {
		t.Fatalf("edit record hashes: %+v", last)
	}
}

// TestEditLabelErrors walks the PATCH error branches.
func TestEditLabelErrors(t *testing.T) {
	env := newTestEnv(t)
	id := env.submitLabel(tokReviewer, 5)
	edit := `{"correctness":1,"scope_judgement":"partial","hallucination":"none"}`

	cases := []struct {
		name  string
		path  string
		token string
		body  string
		want  int
	}{
		{"bad iteration", DefaultPrefix + "/runs/x/labels/" + id, tokReviewer, edit, nethttp.StatusBadRequest},
		{"malformed json", labelsPath(5) + "/" + id, tokReviewer, `{`, nethttp.StatusBadRequest},
		{"missing correctness", labelsPath(5) + "/" + id, tokReviewer, `{"scope_judgement":"partial","hallucination":"none"}`, nethttp.StatusBadRequest},
		{"unknown label id", labelsPath(5) + "/deadbeef", tokReviewer, edit, nethttp.StatusNotFound},
		{"cross-reviewer edit", labelsPath(5) + "/" + id, tokReviewer2, edit, nethttp.StatusForbidden},
		{"invalid enum on own label", labelsPath(5) + "/" + id, tokReviewer, `{"correctness":1,"scope_judgement":"nope","hallucination":"none"}`, nethttp.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := env.do(nethttp.MethodPatch, tc.path, tc.token, tc.body)
			wantStatus(t, rr, tc.want)
		})
	}
}

// TestListLabels covers the read route: empty iteration, populated iteration,
// and read access for every role (spec R3: readonly can fetch).
func TestListLabels(t *testing.T) {
	env := newTestEnv(t)

	rr := env.do(nethttp.MethodGet, labelsPath(9), tokReadonly, "")
	wantStatus(t, rr, nethttp.StatusOK)
	var got labelListJSON
	decodeBody(t, rr, &got)
	if got.Iteration != 9 || len(got.Labels) != 0 || got.LabelSchemaVersion != labels.LabelSchemaVersion {
		t.Fatalf("empty list response: %+v", got)
	}

	env.submitLabel(tokReviewer, 9)
	env.submitLabel(tokReviewer2, 9)
	rr = env.do(nethttp.MethodGet, labelsPath(9), tokReadonly, "")
	wantStatus(t, rr, nethttp.StatusOK)
	decodeBody(t, rr, &got)
	if len(got.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(got.Labels))
	}

	rr = env.do(nethttp.MethodGet, DefaultPrefix+"/runs/zz/labels", tokReadonly, "")
	wantStatus(t, rr, nethttp.StatusBadRequest)
}

// TestListLabelsCorruptSidecar maps a backing-store parse failure to a 500.
func TestListLabelsCorruptSidecar(t *testing.T) {
	env := newTestEnv(t)
	path := labels.IterationLabelsPath(env.iterDir, 2)
	if err := os.WriteFile(path, []byte("labels: [::"), 0o600); err != nil {
		t.Fatalf("plant corrupt sidecar: %v", err)
	}
	rr := env.do(nethttp.MethodGet, labelsPath(2), tokReviewer, "")
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}

// TestWriteLabelErrorMapping pins the sentinel→status table directly.
func TestWriteLabelErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{labels.ErrLabelNotFound, nethttp.StatusNotFound},
		{labels.ErrUnauthorizedEdit, nethttp.StatusForbidden},
		{labels.ErrCorrectnessRange, nethttp.StatusBadRequest},
		{labels.ErrInvalidScope, nethttp.StatusBadRequest},
		{labels.ErrInvalidHalluc, nethttp.StatusBadRequest},
		{labels.ErrFreeTextTooLong, nethttp.StatusBadRequest},
		{labels.ErrInvalidRole, nethttp.StatusBadRequest},
		{labels.ErrNegativeIteration, nethttp.StatusBadRequest},
		{errors.New("disk exploded"), nethttp.StatusInternalServerError},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		writeLabelError(rr, tc.err)
		if rr.Code != tc.want {
			t.Fatalf("writeLabelError(%v) = %d, want %d", tc.err, rr.Code, tc.want)
		}
	}
}

// TestSubmitLabelStoreFailure maps an opaque store failure to a 500 and leaves
// no audit record behind.
func TestSubmitLabelStoreFailure(t *testing.T) {
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: failLabelStore{err: errors.New("store down")},
		Users:  failUserStore{},
		Audit:  failAudit{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}
