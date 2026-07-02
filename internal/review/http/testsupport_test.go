package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

// Stub bearer tokens resolved by stubAuth. Shapes are irrelevant to the stub;
// the real token-shape rules are exercised by the LocalUsersAuthenticator
// integration test.
const (
	tokReviewer  = "tok-reviewer"
	tokReviewer2 = "tok-reviewer-2"
	tokAdmin     = "tok-admin"
	tokReadonly  = "tok-readonly"
)

// Stub identities behind the tokens above.
var (
	idReviewer  = auth.Identity{Email: "rev@example.com", Role: auth.RoleReviewer}
	idReviewer2 = auth.Identity{Email: "rev2@example.com", Role: auth.RoleReviewer}
	idAdmin     = auth.Identity{Email: "admin@example.com", Role: auth.RoleAdmin}
	idReadonly  = auth.Identity{Email: "ro@example.com", Role: auth.RoleReadonly}
)

// stubAuth is a deterministic Authenticator for handler tests.
type stubAuth struct {
	ids map[string]auth.Identity
	err error
}

func (s stubAuth) Authenticate(token string) (auth.Identity, error) {
	if s.err != nil {
		return auth.Identity{}, s.err
	}
	id, ok := s.ids[token]
	if !ok {
		return auth.Identity{}, auth.ErrUnauthenticated
	}
	return id, nil
}

// newStubAuth returns the four-identity stub used by most tests.
func newStubAuth() stubAuth {
	return stubAuth{ids: map[string]auth.Identity{
		tokReviewer:  idReviewer,
		tokReviewer2: idReviewer2,
		tokAdmin:     idAdmin,
		tokReadonly:  idReadonly,
	}}
}

// failLabelStore fails every operation with err.
type failLabelStore struct{ err error }

func (f failLabelStore) List(int) ([]labels.Label, error)      { return nil, f.err }
func (f failLabelStore) Get(int, string) (labels.Label, error) { return labels.Label{}, f.err }
func (f failLabelStore) Add(int, labels.AddInput) (labels.Label, error) {
	return labels.Label{}, f.err
}
func (f failLabelStore) Edit(int, string, labels.EditInput) (labels.Label, error) {
	return labels.Label{}, f.err
}
func (f failLabelStore) SidecarPath(int) string { return "" }

// failUserStore serves a fixed users file but fails Save.
type failUserStore struct {
	uf      *auth.UsersFile
	loadErr error
	saveErr error
}

func (f failUserStore) Load() (*auth.UsersFile, error) { return f.uf, f.loadErr }
func (f failUserStore) Save(*auth.UsersFile) error     { return f.saveErr }
func (f failUserStore) FilePath() string               { return "" }

// failAudit fails Append and/or Records.
type failAudit struct {
	appendErr  error
	records    []audit.Record
	recordsErr error
}

func (f failAudit) Append(audit.Event) (audit.Record, error) {
	return audit.Record{}, f.appendErr
}
func (f failAudit) Records() ([]audit.Record, error) { return f.records, f.recordsErr }

// testEnv bundles a Mount over real sidecar/users/audit stores in a temp dir.
type testEnv struct {
	t         *testing.T
	m         *Mount
	iterDir   string
	usersPath string
	auditLog  *audit.Log
}

// newTestEnv builds the default environment: stub auth, real stores.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	env := &testEnv{
		t:         t,
		iterDir:   dir,
		usersPath: filepath.Join(dir, "users.yaml"),
	}
	env.auditLog = audit.Open(filepath.Join(dir, "audit.log.jsonl"))
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: SidecarLabelStore{Dir: dir},
		Users:  FileUserStore{Path: env.usersPath},
		Audit:  env.auditLog,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env.m = m
	return env
}

// do performs one request against the mount. An empty token omits the
// Authorization header.
func (e *testEnv) do(method, path, token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", bearerScheme+token)
	}
	rr := httptest.NewRecorder()
	e.m.ServeHTTP(rr, req)
	return rr
}

// decodeBody unmarshals a recorded JSON response body.
func decodeBody(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("unmarshal response %q: %v", rr.Body.String(), err)
	}
}

// wantStatus fails the test when the recorded status differs.
func wantStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rr.Code, want, rr.Body.String())
	}
}

// submitLabel POSTs a valid label as token and returns the created label id.
func (e *testEnv) submitLabel(token string, iteration int) string {
	e.t.Helper()
	rr := e.do(nethttp.MethodPost, labelsPath(iteration), token,
		`{"correctness":2,"scope_judgement":"on-target","hallucination":"none","free_text":"solid"}`)
	wantStatus(e.t, rr, nethttp.StatusCreated)
	var got labelJSON
	decodeBody(e.t, rr, &got)
	return got.ID
}

// labelsPath builds the labels collection path for an iteration.
func labelsPath(iteration int) string {
	return DefaultPrefix + "/runs/" + strconv.Itoa(iteration) + "/labels"
}

// auditRecords reads back the environment's audit log.
func (e *testEnv) auditRecords() []audit.Record {
	e.t.Helper()
	recs, err := e.auditLog.Records()
	if err != nil {
		e.t.Fatalf("audit records: %v", err)
	}
	return recs
}

// errIs asserts err wraps target (test-side convenience for sentinel checks).
func errIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}
