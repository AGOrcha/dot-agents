package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
)

// TestAuthMiddleware covers spec R8: missing/invalid token → 401, wrong role →
// 403, operational authenticator failure → 500 (never conflated with 401).
func TestAuthMiddleware(t *testing.T) {
	env := newTestEnv(t)
	path := labelsPath(1)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"missing header", "", nethttp.StatusUnauthorized},
		{"wrong scheme", "Basic abc", nethttp.StatusUnauthorized},
		{"empty bearer token", "Bearer   ", nethttp.StatusUnauthorized},
		{"unknown token", "Bearer nope", nethttp.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodGet, path, nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rr := httptest.NewRecorder()
			env.m.ServeHTTP(rr, req)
			wantStatus(t, rr, tc.want)
		})
	}

	// Role gates: readonly cannot write labels; reviewer cannot reach admin
	// routes (spec done-criterion 2).
	rr := env.do(nethttp.MethodPost, path, tokReadonly, validLabelBody)
	wantStatus(t, rr, nethttp.StatusForbidden)
	rr = env.do(nethttp.MethodGet, DefaultPrefix+"/audit", tokReviewer, "")
	wantStatus(t, rr, nethttp.StatusForbidden)
	rr = env.do(nethttp.MethodPost, DefaultPrefix+"/users", tokReviewer, `{"email":"x@x","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusForbidden)
	rr = env.do(nethttp.MethodGet, DefaultPrefix+"/users", tokReadonly, "")
	wantStatus(t, rr, nethttp.StatusForbidden)
}

// TestAuthMiddlewareBackendFailure maps a non-ErrUnauthenticated authenticator
// error to a 500.
func TestAuthMiddlewareBackendFailure(t *testing.T) {
	m, err := New(DefaultPrefix, Deps{
		Auth:   stubAuth{err: errors.New("users file unreadable")},
		Labels: failLabelStore{},
		Users:  failUserStore{},
		Audit:  failAudit{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(nethttp.MethodGet, labelsPath(1), nil)
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}

// TestAuditMiddlewareRequestID covers request-id handling: a sane
// client-supplied X-Request-Id is honored and lands on the audit record; an
// oversized one is replaced; absent means generated (32-hex).
func TestAuditMiddlewareRequestID(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	req.Header.Set(HeaderRequestID, "retry-key-42")
	rr := httptest.NewRecorder()
	env.m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusCreated)
	if got := rr.Header().Get(HeaderRequestID); got != "retry-key-42" {
		t.Fatalf("client request id not honored: %q", got)
	}

	req = httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	req.Header.Set(HeaderRequestID, strings.Repeat("x", maxRequestIDLen+1))
	rr = httptest.NewRecorder()
	env.m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusCreated)
	generated := rr.Header().Get(HeaderRequestID)
	if len(generated) != 32 || strings.Contains(generated, "x") {
		t.Fatalf("oversized request id should be replaced with generated hex, got %q", generated)
	}

	recs := env.auditRecords()
	if len(recs) != 2 || recs[0].RequestID != "retry-key-42" || recs[1].RequestID != generated {
		t.Fatalf("audit request ids: %+v", recs)
	}
}

// TestAuditMiddlewareAppendFailure covers the at-least-once contract: when the
// audit append fails after the mutation persisted, the client gets a 500
// carrying the request id and a do-not-blind-retry warning — and the label IS
// on disk.
func TestAuditMiddlewareAppendFailure(t *testing.T) {
	dir := t.TempDir()
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: SidecarLabelStore{Dir: dir},
		Users:  failUserStore{},
		Audit:  failAudit{appendErr: errors.New("audit disk full")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(nethttp.MethodPost, labelsPath(6), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)

	wantStatus(t, rr, nethttp.StatusInternalServerError)
	var body errorBody
	decodeBody(t, rr, &body)
	if body.RequestID == "" || !strings.Contains(body.Error, "may have persisted") {
		t.Fatalf("append-failure response must carry request id + warning: %+v", body)
	}
	// The mutation persisted (at-least-once semantics, documented).
	ls, err := SidecarLabelStore{Dir: dir}.List(6)
	if err != nil || len(ls) != 1 {
		t.Fatalf("label should have persisted despite audit failure: %v, %d", err, len(ls))
	}
}

// TestAuditMiddlewareContractViolations covers the defensive branches: a
// mutating handler that succeeds without staging an event, and a request-id
// generation failure.
func TestAuditMiddlewareContractViolations(t *testing.T) {
	env := newTestEnv(t)

	// 2xx with no staged event → 500.
	h := env.m.withAudit(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(nethttp.MethodPost, "/x", nil))
	wantStatus(t, rr, nethttp.StatusInternalServerError)

	// Error statuses pass through unaudited.
	h = env.m.withAudit(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		writeError(w, nethttp.StatusTeapot, "nope", "")
	}))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(nethttp.MethodPost, "/x", nil))
	wantStatus(t, rr, nethttp.StatusTeapot)
	if recs := env.auditRecords(); len(recs) != 0 {
		t.Fatalf("error responses must not append audit records, got %d", len(recs))
	}

	// Request-id entropy failure → 500 before the handler runs.
	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = orig }()
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(nethttp.MethodPost, "/x", nil))
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}

// TestStageAuditWithoutMiddleware confirms stageAudit is a safe no-op when the
// context carries no holder.
func TestStageAuditWithoutMiddleware(t *testing.T) {
	stageAudit(context.Background(), audit.Event{Actor: "x", Target: "y", Action: audit.ActionLabelSubmit})
}

// TestMustIdentityMissing covers the defensive 500 when a handler runs without
// the auth middleware having attached an identity.
func TestMustIdentityMissing(t *testing.T) {
	env := newTestEnv(t)
	req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.SetPathValue("iteration", "1")
	rr := httptest.NewRecorder()
	env.m.handleSubmitLabel(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)

	req = httptest.NewRequest(nethttp.MethodPatch, labelsPath(1)+"/abc", strings.NewReader(validLabelBody))
	req.SetPathValue("iteration", "1")
	rr = httptest.NewRecorder()
	env.m.handleEditLabel(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)

	// identityFrom on a bare context.
	if _, ok := identityFrom(context.Background()); ok {
		t.Fatal("identityFrom should miss on a bare context")
	}
}

// TestBufferedResponse pins the recorder semantics the audit middleware relies
// on: implicit 200, first-status-wins, and faithful header/body replay.
func TestBufferedResponse(t *testing.T) {
	b := newBufferedResponse()
	if b.status() != nethttp.StatusOK {
		t.Fatalf("default status = %d", b.status())
	}
	b.Header().Set("X-Custom", "v")
	if _, err := b.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	b.WriteHeader(nethttp.StatusTeapot) // after Write — ignored
	if b.status() != nethttp.StatusOK {
		t.Fatalf("status after late WriteHeader = %d", b.status())
	}
	rr := httptest.NewRecorder()
	b.flushTo(rr)
	if rr.Code != nethttp.StatusOK || rr.Body.String() != "hello" || rr.Header().Get("X-Custom") != "v" {
		t.Fatalf("flushTo mismatch: %d %q", rr.Code, rr.Body.String())
	}

	b2 := newBufferedResponse()
	b2.WriteHeader(nethttp.StatusCreated)
	b2.WriteHeader(nethttp.StatusTeapot)
	if b2.status() != nethttp.StatusCreated {
		t.Fatalf("first-status-wins violated: %d", b2.status())
	}
}

// TestPermissionMatrix sweeps every route against every role, pinning the
// permission each route was wired with.
func TestPermissionMatrix(t *testing.T) {
	env := newTestEnv(t)
	// Routes that must be denied (403) per role. Reads use GET so the request
	// bodies don't matter.
	denied := []struct {
		name   string
		method string
		path   string
		token  string
	}{
		{"readonly cannot POST labels", nethttp.MethodPost, labelsPath(1), tokReadonly},
		{"readonly cannot PATCH labels", nethttp.MethodPatch, labelsPath(1) + "/id1", tokReadonly},
		{"readonly cannot view audit", nethttp.MethodGet, DefaultPrefix + "/audit", tokReadonly},
		{"reviewer cannot view audit", nethttp.MethodGet, DefaultPrefix + "/audit", tokReviewer},
		{"reviewer cannot list users", nethttp.MethodGet, DefaultPrefix + "/users", tokReviewer},
		{"reviewer cannot create users", nethttp.MethodPost, DefaultPrefix + "/users", tokReviewer},
		{"reviewer cannot change roles", nethttp.MethodPatch, DefaultPrefix + "/users/a@b", tokReviewer},
		{"reviewer cannot delete users", nethttp.MethodDelete, DefaultPrefix + "/users/a@b", tokReviewer},
	}
	for _, tc := range denied {
		t.Run(tc.name, func(t *testing.T) {
			rr := env.do(tc.method, tc.path, tc.token, "{}")
			wantStatus(t, rr, nethttp.StatusForbidden)
		})
	}
	// auth.Identity.Can consistency spot-check.
	if !(auth.Identity{Role: auth.RoleAdmin}).Can(auth.PermManageUsers) {
		t.Fatal("admin should manage users")
	}
}
