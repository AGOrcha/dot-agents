package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
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

// TestAuditFailClosedRollback pins the fail-closed contract: when the audit
// append fails, the mutation is rolled back to the target's byte-exact
// pre-image and the client gets a 500 carrying the request id — no
// persisted-but-unaudited mutation survives.
func TestAuditFailClosedRollback(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.yaml")
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: SidecarLabelStore{Dir: dir},
		Users:  FileUserStore{Path: usersPath},
		Audit:  failAudit{appendErr: errors.New("audit disk full")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	post := func(path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(nethttp.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", bearerScheme+token)
		rr := httptest.NewRecorder()
		m.ServeHTTP(rr, req)
		return rr
	}

	// Case 1: no pre-existing sidecar — rollback restores absence.
	rr := post(labelsPath(6), tokReviewer, validLabelBody)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	var body errorBody
	decodeBody(t, rr, &body)
	if body.RequestID == "" || !strings.Contains(body.Error, "rolled back") {
		t.Fatalf("fail-closed response must carry request id + rollback note: %+v", body)
	}
	if _, err := os.Stat(labels.IterationLabelsPath(dir, 6)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sidecar must not survive a failed audit append: %v", err)
	}

	// Case 2: pre-existing sidecar — rollback restores the exact prior bytes.
	if _, err := labels.Add(dir, 7, labels.AddInput{
		Actor: "seed@example.com", Role: labels.RoleReviewer,
		Structured: labels.Structured{Correctness: 1, ScopeJudgement: labels.ScopePartial, Hallucination: labels.HallucinationNone},
	}); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	preBytes, err := os.ReadFile(labels.IterationLabelsPath(dir, 7))
	if err != nil {
		t.Fatalf("read pre-image: %v", err)
	}
	rr = post(labelsPath(7), tokReviewer, validLabelBody)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	postBytes, err := os.ReadFile(labels.IterationLabelsPath(dir, 7))
	if err != nil || !bytes.Equal(preBytes, postBytes) {
		t.Fatalf("sidecar must be byte-identical to its pre-image after rollback (err=%v)", err)
	}

	// Case 3: users file — a failed user creation leaves no users file behind.
	rr = post(DefaultPrefix+"/users", tokAdmin, `{"email":"ghost@example.com","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	if _, err := os.Stat(usersPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("users file must not survive a failed audit append: %v", err)
	}
}

// TestAuditRollbackResidual covers the doubly-degraded environment: the audit
// append fails AND the rollback fails — the mutation survives unaudited, the
// client is told so, and the incident is reported loudly with the request id.
func TestAuditRollbackResidual(t *testing.T) {
	origRemove, origRestore, origLog := removeTargetFile, restoreTargetFile, criticalLog
	defer func() {
		removeTargetFile, restoreTargetFile, criticalLog = origRemove, origRestore, origLog
	}()
	removeTargetFile = func(string) error { return errors.New("rm blocked") }
	restoreTargetFile = func(string, []byte) error { return errors.New("write blocked") }
	var logBuf bytes.Buffer
	criticalLog = &logBuf

	dir := t.TempDir()
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: SidecarLabelStore{Dir: dir},
		Users:  FileUserStore{Path: filepath.Join(dir, "users.yaml")},
		Audit:  failAudit{appendErr: errors.New("audit disk full")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// No pre-image → remove path fails.
	req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	req.Header.Set(HeaderRequestID, "residual-req-1")
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	var body errorBody
	decodeBody(t, rr, &body)
	if !strings.Contains(body.Error, "UNAUDITED") || body.RequestID != "residual-req-1" {
		t.Fatalf("residual response: %+v", body)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "CRITICAL") || !strings.Contains(logged, "residual-req-1") {
		t.Fatalf("critical log must scream with the request id: %q", logged)
	}
	// The residual: the mutation survived (documented and loud, not silent).
	if ls, err := (SidecarLabelStore{Dir: dir}).List(1); err != nil || len(ls) != 1 {
		t.Fatalf("residual mutation state: %v, %d", err, len(ls))
	}

	// Pre-existing file → restore path fails too.
	logBuf.Reset()
	req = httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	rr = httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	if !strings.Contains(logBuf.String(), "CRITICAL") {
		t.Fatalf("restore-path failure must also scream: %q", logBuf.String())
	}
}

// TestRollbackTolerantOfMissingFile covers the remove-path tolerance: when the
// handler never materialized the target file, absence already IS the
// pre-image and the rollback counts as success.
func TestRollbackTolerantOfMissingFile(t *testing.T) {
	orig := removeTargetFile
	removeTargetFile = func(string) error { return os.ErrNotExist }
	defer func() { removeTargetFile = orig }()
	if !rollback("/nonexistent/target", preImage{}, "req-x") {
		t.Fatal("missing target file should count as a successful rollback")
	}
}

// TestMutationGuardLockAndPreImageFailures covers the guard's own error
// branches: target lock acquisition failure and pre-image read failure.
func TestMutationGuardLockAndPreImageFailures(t *testing.T) {
	env := newTestEnv(t)
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(validLabelBody))
		req.Header.Set("Authorization", bearerScheme+tokReviewer)
		rr := httptest.NewRecorder()
		env.m.ServeHTTP(rr, req)
		return rr
	}

	origLock := acquireFileLock
	acquireFileLock = func(string) (func() error, error) { return nil, errors.New("lock held") }
	rr := post()
	acquireFileLock = origLock
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	var body errorBody
	decodeBody(t, rr, &body)
	if !strings.Contains(body.Error, "lock mutation target") {
		t.Fatalf("lock failure response: %+v", body)
	}

	origRead := readTargetFile
	readTargetFile = func(string) ([]byte, error) { return nil, errors.New("io fault") }
	rr = post()
	readTargetFile = origRead
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	decodeBody(t, rr, &body)
	if !strings.Contains(body.Error, "pre-image") {
		t.Fatalf("pre-image failure response: %+v", body)
	}
}

// TestConcurrentLabelSubmissionsNoLostUpdates is the lost-update regression
// test: N parallel POSTs of distinct labels against the same iteration must
// ALL survive in the sidecar (the guard serializes the read-modify-write
// sections), with N audit records and a clean chain.
func TestConcurrentLabelSubmissionsNoLostUpdates(t *testing.T) {
	env := newTestEnv(t)
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(
				`{"correctness":%d,"scope_judgement":"on-target","hallucination":"none","free_text":"writer %d"}`,
				i%4, i)
			req := httptest.NewRequest(nethttp.MethodPost, labelsPath(1), strings.NewReader(body))
			req.Header.Set("Authorization", bearerScheme+tokReviewer)
			rr := httptest.NewRecorder()
			env.m.ServeHTTP(rr, req)
			codes[i] = rr.Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != nethttp.StatusCreated {
			t.Fatalf("writer %d: status %d", i, c)
		}
	}

	ls, err := SidecarLabelStore{Dir: env.iterDir}.List(1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ls) != n {
		t.Fatalf("lost update: %d of %d labels survived", len(ls), n)
	}
	distinct := make(map[string]struct{}, n)
	for _, l := range ls {
		distinct[l.EffectiveFreeText()] = struct{}{}
	}
	if len(distinct) != n {
		t.Fatalf("expected %d distinct labels, got %d", n, len(distinct))
	}
	recs := env.auditRecords()
	if len(recs) != n {
		t.Fatalf("expected %d audit records, got %d", n, len(recs))
	}
	if res, err := env.auditLog.Verify(); err != nil || !res.OK {
		t.Fatalf("audit verify after concurrent writes: %+v, %v", res, err)
	}
}

// TestAuditMiddlewareContractViolations covers the defensive branches: a
// mutating handler that succeeds without staging an event (rolled back like a
// failed audit), and a request-id generation failure.
func TestAuditMiddlewareContractViolations(t *testing.T) {
	env := newTestEnv(t)
	noTarget := targetFunc(func(*nethttp.Request) string { return "" })

	// 2xx with no staged event → 500.
	h := env.m.withAudit(noTarget, nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(nethttp.MethodPost, "/x", nil))
	wantStatus(t, rr, nethttp.StatusInternalServerError)

	// The no-staged-event violation rolls the mutation back when a target is
	// known: the file written by the buggy handler must not survive.
	buggyPath := filepath.Join(t.TempDir(), "buggy.yaml")
	h = env.m.withAudit(func(*nethttp.Request) string { return buggyPath },
		nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
			if err := fsops.WriteFileAtomic(buggyPath, []byte("mutated")); err != nil {
				t.Errorf("buggy handler write: %v", err)
			}
			w.WriteHeader(nethttp.StatusOK)
		}))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(nethttp.MethodPost, "/x", nil))
	wantStatus(t, rr, nethttp.StatusInternalServerError)
	if _, err := os.Stat(buggyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unstaged mutation must be rolled back: %v", err)
	}

	// Error statuses pass through unaudited.
	h = env.m.withAudit(noTarget, nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
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
