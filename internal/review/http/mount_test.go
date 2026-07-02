package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	servicehttp "github.com/AGOrcha/dot-agents/internal/service/http"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
)

// TestNewValidation covers prefix normalization and dependency validation.
func TestNewValidation(t *testing.T) {
	valid := Deps{
		Auth:   newStubAuth(),
		Labels: failLabelStore{},
		Users:  failUserStore{},
		Audit:  failAudit{},
	}

	for _, bad := range []string{"", "api/reviews", "/", "///"} {
		if _, err := New(bad, valid); err == nil {
			t.Fatalf("New(%q) should reject the prefix", bad)
		} else {
			errIs(t, err, ErrInvalidPrefix)
		}
	}

	m, err := New("/api/reviews/", valid)
	if err != nil {
		t.Fatalf("New with trailing slash: %v", err)
	}
	if m.Prefix() != "/api/reviews" {
		t.Fatalf("Prefix() = %q, want normalized /api/reviews", m.Prefix())
	}

	nilCases := []Deps{
		{Labels: valid.Labels, Users: valid.Users, Audit: valid.Audit},
		{Auth: valid.Auth, Users: valid.Users, Audit: valid.Audit},
		{Auth: valid.Auth, Labels: valid.Labels, Audit: valid.Audit},
		{Auth: valid.Auth, Labels: valid.Labels, Users: valid.Users},
	}
	for i, deps := range nilCases {
		if _, err := New(DefaultPrefix, deps); err == nil {
			t.Fatalf("New with nil dep %d should fail", i)
		} else {
			errIs(t, err, ErrNilDependency)
		}
	}
}

// TestUnknownRoutes pins mux behavior for paths and methods outside the route
// table.
func TestUnknownRoutes(t *testing.T) {
	env := newTestEnv(t)

	rr := env.do(nethttp.MethodGet, DefaultPrefix+"/nope", tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusNotFound)

	rr = env.do(nethttp.MethodDelete, labelsPath(1), tokAdmin, "")
	wantStatus(t, rr, nethttp.StatusMethodNotAllowed)
}

// stubState satisfies the R3 server's StateProvider.
type stubState struct{}

func (stubState) State() []scheduler.TaskState { return nil }

// TestRegisterMountIntegration proves the mount stitches onto the real R3
// HTTP/SSE edge exactly as the §2A rescope prescribes: RegisterMount with the
// mount's own prefix, full-path routing (no prefix stripping), coexisting with
// the built-in routes, and no bus dependency.
func TestRegisterMountIntegration(t *testing.T) {
	env := newTestEnv(t)

	srv := servicehttp.New("127.0.0.1:0", stubState{}, nil)
	if err := srv.RegisterMount(env.m.Prefix(), env.m); err != nil {
		t.Fatalf("RegisterMount(%q): %v", env.m.Prefix(), err)
	}

	// Review route through the R3 mux.
	req := httptest.NewRequest(nethttp.MethodGet, labelsPath(0), nil)
	req.Header.Set("Authorization", bearerScheme+tokReviewer)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	wantStatus(t, rr, nethttp.StatusOK)

	// Built-in route still served by R3.
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(nethttp.MethodGet, "/healthz", nil))
	wantStatus(t, rr, nethttp.StatusOK)
}

// TestEndToEndWithLocalUsersAuthenticator is the no-stub slice of done
// criterion 1's flow: a users file seeded through the shipped auth package, a
// real LocalUsersAuthenticator, real sidecar and audit stores — admin mints a
// reviewer over HTTP, and the minted plaintext token immediately authenticates
// a label submission.
func TestEndToEndWithLocalUsersAuthenticator(t *testing.T) {
	dir := t.TempDir()
	usersPath := filepath.Join(dir, "users.yaml")

	seed := &auth.UsersFile{}
	adminToken, err := seed.AddUser("root@example.com", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	if err := seed.Save(usersPath); err != nil {
		t.Fatalf("save users: %v", err)
	}

	m, err := New(DefaultPrefix, Deps{
		Auth:   auth.NewLocalUsersAuthenticator(usersPath),
		Labels: SidecarLabelStore{Dir: dir},
		Users:  FileUserStore{Path: usersPath},
		Audit:  audit.Open(filepath.Join(dir, "audit.log.jsonl")),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env := &testEnv{t: t, m: m, iterDir: dir, usersPath: usersPath}

	rr := env.do(nethttp.MethodPost, DefaultPrefix+"/users", adminToken,
		`{"email":"labeler@example.com","role":"reviewer"}`)
	wantStatus(t, rr, nethttp.StatusCreated)
	var created createUserJSON
	decodeBody(t, rr, &created)

	rr = env.do(nethttp.MethodPost, labelsPath(1), created.Token, validLabelBody)
	wantStatus(t, rr, nethttp.StatusCreated)

	// A bogus (well-formed but unknown) token stays a 401 through the real
	// authenticator.
	rr = env.do(nethttp.MethodPost, labelsPath(1), "rvw_bogusbogusbogus", validLabelBody)
	wantStatus(t, rr, nethttp.StatusUnauthorized)
}

// TestDefaultStores exercises the production store adapters directly (Get is
// otherwise only reached through the PATCH flow).
func TestDefaultStores(t *testing.T) {
	dir := t.TempDir()
	store := SidecarLabelStore{Dir: dir}
	if _, err := store.Get(1, "missing"); err == nil {
		t.Fatal("Get on empty store should fail")
	}
	if ls, err := store.List(1); err != nil || len(ls) != 0 {
		t.Fatalf("List on empty store: %v %d", err, len(ls))
	}

	users := FileUserStore{Path: filepath.Join(dir, "users.yaml")}
	uf, err := users.Load()
	if err != nil {
		t.Fatalf("Load missing users file: %v", err)
	}
	if _, err := uf.AddUser("a@example.com", auth.RoleReviewer); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := users.Save(uf); err != nil {
		t.Fatalf("Save: %v", err)
	}
}
