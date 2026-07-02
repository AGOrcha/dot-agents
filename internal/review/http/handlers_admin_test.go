package http

import (
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
)

// TestUsersCRUDHappyPath drives create → list → role change → delete as the
// admin, asserting the print-once token contract and one audit record per
// mutation (spec R6, OQ1). Each stage lives in its own helper.
func TestUsersCRUDHappyPath(t *testing.T) {
	env := newTestEnv(t)
	created := createUserStep(t, env)
	assertIssuedTokenPersisted(t, env, created)
	assertUsersListSafe(t, env)
	changeRoleStep(t, env)
	deleteUserStep(t, env)
	assertUsersCRUDAudit(t, env)
}

// createUserStep creates a reviewer over HTTP and checks the 201 payload,
// including the rvw_-prefixed print-once plaintext token.
func createUserStep(t *testing.T, env *testEnv) createUserJSON {
	t.Helper()
	rr := env.do(nethttp.MethodPost, DefaultPrefix+"/users", tokAdmin,
		`{"email":"new@example.com","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusCreated)
	var created createUserJSON
	decodeBody(t, rr, &created)
	if created.Email != "new@example.com" || created.Role != "reviewer" {
		t.Fatalf("create response: %+v", created)
	}
	if !strings.HasPrefix(created.Token, "rvw_") {
		t.Fatalf("issued token %q lacks the rvw_ prefix", created.Token)
	}
	return created
}

// assertIssuedTokenPersisted checks the stored argon2id hash verifies against
// the issued plaintext.
func assertIssuedTokenPersisted(t *testing.T, env *testEnv, created createUserJSON) {
	t.Helper()
	uf, err := auth.LoadUsersFile(env.usersPath)
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	stored, found := uf.Find(created.Email)
	if !found {
		t.Fatal("created user not persisted")
	}
	if ok, err := auth.VerifyToken(created.Token, stored.TokenHash); err != nil || !ok {
		t.Fatalf("issued token does not verify against stored hash: %v %v", ok, err)
	}
}

// assertUsersListSafe checks the list route returns the user without leaking
// hash or plaintext material.
func assertUsersListSafe(t *testing.T, env *testEnv) {
	t.Helper()
	rr := env.do(nethttp.MethodGet, DefaultPrefix+"/users", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusOK)
	if body := rr.Body.String(); strings.Contains(body, "token") || strings.Contains(body, "argon2") {
		t.Fatalf("users list leaks secret material: %s", body)
	}
	var list usersListJSON
	decodeBody(t, rr, &list)
	if len(list.Users) != 1 || list.Users[0].Email != "new@example.com" {
		t.Fatalf("users list: %+v", list)
	}
}

// changeRoleStep changes the user's role via a case-insensitive email match.
func changeRoleStep(t *testing.T, env *testEnv) {
	t.Helper()
	rr := env.do(nethttp.MethodPatch, DefaultPrefix+"/users/NEW@example.com", tokAdmin,
		`{"role":"readonly"}`)
	wantStatus(t, rr, nethttp.StatusOK)
	var changed userJSON
	decodeBody(t, rr, &changed)
	if changed.Role != "readonly" {
		t.Fatalf("role change response: %+v", changed)
	}
}

// deleteUserStep removes the user and expects a 204.
func deleteUserStep(t *testing.T, env *testEnv) {
	t.Helper()
	rr := env.do(nethttp.MethodDelete, DefaultPrefix+"/users/new@example.com", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusNoContent)
}

// assertUsersCRUDAudit checks one record per mutation, correct actions and
// attribution, role-change before/after hashes, and a clean chain.
func assertUsersCRUDAudit(t *testing.T, env *testEnv) {
	t.Helper()
	recs := env.auditRecords()
	if len(recs) != 3 {
		t.Fatalf("expected 3 audit records, got %d", len(recs))
	}
	wantActions := []audit.Action{audit.ActionUserCreate, audit.ActionRoleChange, audit.ActionUserDelete}
	for i, want := range wantActions {
		if recs[i].Action != want || recs[i].Actor != idAdmin.Email {
			t.Fatalf("record %d = %+v, want action %s", i, recs[i], want)
		}
	}
	if recs[1].BeforeHash == "" || recs[1].AfterHash == "" {
		t.Fatalf("role change should carry before/after hashes: %+v", recs[1])
	}
	if res, err := env.auditLog.Verify(); err != nil || !res.OK {
		t.Fatalf("audit verify: %+v, %v", res, err)
	}
}

// TestUsersValidationAndConflicts walks the client-error branches of the user
// routes.
func TestUsersValidationAndConflicts(t *testing.T) {
	env := newTestEnv(t)
	rr := env.do(nethttp.MethodPost, DefaultPrefix+"/users", tokAdmin,
		`{"email":"dup@example.com","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusCreated)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"create malformed json", nethttp.MethodPost, DefaultPrefix + "/users", `{`, nethttp.StatusBadRequest},
		{"create empty email", nethttp.MethodPost, DefaultPrefix + "/users", `{"email":"  ","role":"reviewer"}`, nethttp.StatusBadRequest},
		{"create invalid role", nethttp.MethodPost, DefaultPrefix + "/users", `{"email":"a@b","role":"czar"}`, nethttp.StatusBadRequest},
		{"create duplicate", nethttp.MethodPost, DefaultPrefix + "/users", `{"email":"dup@example.com","role":"reviewer"}`, nethttp.StatusConflict},
		{"role change malformed json", nethttp.MethodPatch, DefaultPrefix + "/users/dup@example.com", `{`, nethttp.StatusBadRequest},
		{"role change invalid role", nethttp.MethodPatch, DefaultPrefix + "/users/dup@example.com", `{"role":"czar"}`, nethttp.StatusBadRequest},
		{"role change unknown user", nethttp.MethodPatch, DefaultPrefix + "/users/ghost@example.com", `{"role":"admin"}`, nethttp.StatusNotFound},
		{"delete unknown user", nethttp.MethodDelete, DefaultPrefix + "/users/ghost@example.com", ``, nethttp.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := env.do(tc.method, tc.path, tokAdmin, tc.body)
			wantStatus(t, rr, tc.want)
		})
	}
}

// TestUsersStoreFailures maps users-file load/save failures to 500s across
// the admin routes.
func TestUsersStoreFailures(t *testing.T) {
	// Load failure: corrupt on-disk users file behind the real FileUserStore.
	env := newTestEnv(t)
	if err := os.WriteFile(env.usersPath, []byte("users: [::"), 0o600); err != nil {
		t.Fatalf("plant corrupt users file: %v", err)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{nethttp.MethodGet, DefaultPrefix + "/users", ""},
		{nethttp.MethodPost, DefaultPrefix + "/users", `{"email":"a@b","role":"reviewer"}`},
		{nethttp.MethodPatch, DefaultPrefix + "/users/a@b", `{"role":"admin"}`},
		{nethttp.MethodDelete, DefaultPrefix + "/users/a@b", ""},
	} {
		rr := env.do(tc.method, tc.path, tokAdmin, tc.body)
		wantStatus(t, rr, nethttp.StatusInternalServerError)
	}

	// Save failure: seeded store whose Save always fails.
	seeded := &auth.UsersFile{}
	if _, err := seeded.AddUser("held@example.com", auth.RoleReviewer); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: failLabelStore{},
		Users:  failUserStore{uf: seeded, saveErr: errors.New("disk full")},
		Audit:  failAudit{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{nethttp.MethodPost, DefaultPrefix + "/users", `{"email":"b@c","role":"reviewer"}`},
		{nethttp.MethodPatch, DefaultPrefix + "/users/held@example.com", `{"role":"admin"}`},
		{nethttp.MethodDelete, DefaultPrefix + "/users/held@example.com", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", bearerScheme+tokAdmin)
		rr := httptest.NewRecorder()
		m.ServeHTTP(rr, req)
		wantStatus(t, rr, nethttp.StatusInternalServerError)
	}
}

// TestCreateUserTokenIssuanceFailure covers the entropy-failure branch of
// user creation via the addUser seam.
func TestCreateUserTokenIssuanceFailure(t *testing.T) {
	orig := addUser
	addUser = func(*auth.UsersFile, string, auth.Role) (string, error) {
		return "", errors.New("entropy exhausted")
	}
	defer func() { addUser = orig }()

	env := newTestEnv(t)
	rr := env.do(nethttp.MethodPost, DefaultPrefix+"/users", tokAdmin,
		`{"email":"a@b","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}

// TestAuditView covers the admin audit route: full listing, tail limiting,
// invalid limits, and a backend read failure.
func TestAuditView(t *testing.T) {
	env := newTestEnv(t)
	for i := 0; i < 3; i++ {
		env.submitLabel(tokReviewer, i)
	}

	rr := env.do(nethttp.MethodGet, DefaultPrefix+"/audit", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusOK)
	var got auditListJSON
	decodeBody(t, rr, &got)
	if got.Total != 3 || len(got.Records) != 3 {
		t.Fatalf("full audit view: total=%d records=%d", got.Total, len(got.Records))
	}

	rr = env.do(nethttp.MethodGet, DefaultPrefix+"/audit?limit=2", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusOK)
	decodeBody(t, rr, &got)
	if got.Total != 3 || len(got.Records) != 2 {
		t.Fatalf("limited audit view: total=%d records=%d", got.Total, len(got.Records))
	}
	if got.Records[1].Target != "iteration/2/label/"+lastLabelID(t, env, 2) {
		t.Fatalf("limit should keep the newest records: %+v", got.Records)
	}

	rr = env.do(nethttp.MethodGet, DefaultPrefix+"/audit?limit=99", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusOK)
	decodeBody(t, rr, &got)
	if len(got.Records) != 3 {
		t.Fatalf("oversized limit should return everything, got %d", len(got.Records))
	}

	for _, bad := range []string{"abc", "-1"} {
		rr = env.do(nethttp.MethodGet, DefaultPrefix+"/audit?limit="+bad, tokAdmin, "")
		wantStatus(t, rr, nethttp.StatusBadRequest)
	}
}

// TestAuditViewBackendFailure maps a Records() failure to a 500.
func TestAuditViewBackendFailure(t *testing.T) {
	m, err := New(DefaultPrefix, Deps{
		Auth:   newStubAuth(),
		Labels: failLabelStore{},
		Users:  failUserStore{},
		Audit:  failAudit{recordsErr: errors.New("log unreadable")},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(nethttp.MethodGet, DefaultPrefix+"/audit", nil)
	req.Header.Set("Authorization", bearerScheme+tokAdmin)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusInternalServerError)
}

// TestAdminHandlersMissingIdentity covers the defensive mustIdentity branch on
// each mutating admin handler invoked without the auth middleware.
func TestAdminHandlersMissingIdentity(t *testing.T) {
	env := newTestEnv(t)
	handlers := []nethttp.HandlerFunc{
		env.m.handleCreateUser,
		env.m.handleChangeRole,
		env.m.handleDeleteUser,
	}
	for i, h := range handlers {
		req := httptest.NewRequest(nethttp.MethodPost, DefaultPrefix+"/users", strings.NewReader("{}"))
		req.SetPathValue("email", "a@b")
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != nethttp.StatusInternalServerError {
			t.Fatalf("handler %d without identity: status %d", i, rr.Code)
		}
	}
}

// lastLabelID returns the id of the newest label on an iteration.
func lastLabelID(t *testing.T, env *testEnv, iteration int) string {
	t.Helper()
	ls, err := SidecarLabelStore{Dir: env.iterDir}.List(iteration)
	if err != nil || len(ls) == 0 {
		t.Fatalf("list labels for iter %d: %v", iteration, err)
	}
	return ls[len(ls)-1].ID
}
