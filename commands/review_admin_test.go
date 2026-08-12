package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/review/auth"
	"github.com/spf13/cobra"
)

// fakeReviewAdminDeps is the interface-DI test double for reviewAdminDeps
// (mirrors fakeReviewDeps in review_test.go). A nil func field delegates to
// the real stdReviewAdminDeps implementation so a test overrides only the
// operation it wants to fault-inject.
type fakeReviewAdminDeps struct {
	std              stdReviewAdminDeps
	defaultUsersPath func() (string, error)
	loadUsers        func(string) (*auth.UsersFile, error)
	saveUsers        func(*auth.UsersFile, string) error
	authenticate     func(string, string) (auth.Identity, error)
	acquireFileLock  func(string) (func() error, error)
	readFile         func(string) ([]byte, error)
	writeFileAtomic  func(string, []byte) error
	remove           func(string) error
	auditAppend      func(string, audit.Event) (audit.Record, error)
	auditRecords     func(string) ([]audit.Record, error)
	auditVerify      func(string) (audit.VerifyResult, error)
	auditRepairHead  func(string) (audit.VerifyResult, error)
	auditPrune       func(string, int) ([]string, error)
}

func (f fakeReviewAdminDeps) DefaultUsersPath() (string, error) {
	if f.defaultUsersPath != nil {
		return f.defaultUsersPath()
	}
	return f.std.DefaultUsersPath()
}

func (f fakeReviewAdminDeps) LoadUsers(path string) (*auth.UsersFile, error) {
	if f.loadUsers != nil {
		return f.loadUsers(path)
	}
	return f.std.LoadUsers(path)
}

func (f fakeReviewAdminDeps) SaveUsers(uf *auth.UsersFile, path string) error {
	if f.saveUsers != nil {
		return f.saveUsers(uf, path)
	}
	return f.std.SaveUsers(uf, path)
}

func (f fakeReviewAdminDeps) Authenticate(usersPath, token string) (auth.Identity, error) {
	if f.authenticate != nil {
		return f.authenticate(usersPath, token)
	}
	return f.std.Authenticate(usersPath, token)
}

func (f fakeReviewAdminDeps) AcquireFileLock(path string) (func() error, error) {
	if f.acquireFileLock != nil {
		return f.acquireFileLock(path)
	}
	return f.std.AcquireFileLock(path)
}

func (f fakeReviewAdminDeps) ReadFile(path string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(path)
	}
	return f.std.ReadFile(path)
}

func (f fakeReviewAdminDeps) WriteFileAtomic(path string, data []byte) error {
	if f.writeFileAtomic != nil {
		return f.writeFileAtomic(path, data)
	}
	return f.std.WriteFileAtomic(path, data)
}

func (f fakeReviewAdminDeps) Remove(path string) error {
	if f.remove != nil {
		return f.remove(path)
	}
	return f.std.Remove(path)
}

func (f fakeReviewAdminDeps) AuditAppend(logPath string, e audit.Event) (audit.Record, error) {
	if f.auditAppend != nil {
		return f.auditAppend(logPath, e)
	}
	return f.std.AuditAppend(logPath, e)
}

func (f fakeReviewAdminDeps) AuditRecords(logPath string) ([]audit.Record, error) {
	if f.auditRecords != nil {
		return f.auditRecords(logPath)
	}
	return f.std.AuditRecords(logPath)
}

func (f fakeReviewAdminDeps) AuditVerify(logPath string) (audit.VerifyResult, error) {
	if f.auditVerify != nil {
		return f.auditVerify(logPath)
	}
	return f.std.AuditVerify(logPath)
}

func (f fakeReviewAdminDeps) AuditRepairHead(logPath string) (audit.VerifyResult, error) {
	if f.auditRepairHead != nil {
		return f.auditRepairHead(logPath)
	}
	return f.std.AuditRepairHead(logPath)
}

func (f fakeReviewAdminDeps) AuditPruneArchivesBefore(logPath string, year int) ([]string, error) {
	if f.auditPrune != nil {
		return f.auditPrune(logPath, year)
	}
	return f.std.AuditPruneArchivesBefore(logPath, year)
}

// ── harness ─────────────────────────────────────────────────────────────────

// execReviewAdminCmd runs one subcommand tree built by build with args and
// returns the combined output.
func execReviewAdminCmd(t *testing.T, build func(reviewAdminDeps) *cobra.Command, deps reviewAdminDeps, args ...string) (string, error) {
	t.Helper()
	cmd := build(deps)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func execUsersCmd(t *testing.T, deps reviewAdminDeps, args ...string) (string, error) {
	t.Helper()
	return execReviewAdminCmd(t, newReviewUsersCmd, deps, args...)
}

func execAuditCmd(t *testing.T, deps reviewAdminDeps, args ...string) (string, error) {
	t.Helper()
	return execReviewAdminCmd(t, newReviewAuditCmd, deps, args...)
}

// tempReviewPaths returns fresh users-file and audit-log paths in one temp dir.
func tempReviewPaths(t *testing.T) (usersPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "users.yaml"), filepath.Join(dir, "audit.log.jsonl")
}

// stubHash is a syntactically PHC-shaped stand-in token hash for seeded users
// that never authenticate through real argon2 in a test.
const stubHash = "$argon2id$v=19$m=65536,t=1,p=4$c3R1YnNhbHQ$c3R1YmtleQ"

// writeReviewUsers seeds a users file directly (bypassing the command under
// test) with stub hashes.
func writeReviewUsers(t *testing.T, path string, users ...auth.User) {
	t.Helper()
	uf := &auth.UsersFile{Users: users}
	if err := uf.Save(path); err != nil {
		t.Fatalf("seed users file: %v", err)
	}
}

// adminIdentityFake returns a fake that authenticates any token as the given
// identity, skipping real argon2 work.
func adminIdentityFake(id auth.Identity) fakeReviewAdminDeps {
	return fakeReviewAdminDeps{
		authenticate: func(string, string) (auth.Identity, error) { return id, nil },
	}
}

// seededAdmin is the stub admin row used across mutation tests.
func seededAdmin() auth.User {
	return auth.User{Email: "admin@example.com", Role: auth.RoleAdmin, TokenHash: stubHash, CreatedAt: "2026-01-01T00:00:00Z"}
}

// secondAdmin is a second stub admin row so tests can remove/demote the first
// admin without tripping the last-admin lockout guard.
func secondAdmin() auth.User {
	return auth.User{Email: "admin2@example.com", Role: auth.RoleAdmin, TokenHash: stubHash, CreatedAt: "2026-01-02T00:00:00Z"}
}

// adminID is the identity the adminIdentityFake resolves for seededAdmin.
func adminID() auth.Identity {
	return auth.Identity{Email: "admin@example.com", Role: auth.RoleAdmin}
}

// setJSONFlag flips the global --json flag for one test.
func setJSONFlag(t *testing.T) {
	t.Helper()
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = false })
}

// auditRecordsOf reads back all records the command wrote.
func auditRecordsOf(t *testing.T, logPath string) []audit.Record {
	t.Helper()
	recs, err := audit.Open(logPath).Records()
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	return recs
}

// mustContain asserts each want substring is present.
func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Fatalf("output missing %q:\n%s", w, got)
		}
	}
}

// mustErrContain asserts err is non-nil and mentions each want substring.
func mustErrContain(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %v, got nil", wants)
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error missing %q: %v", w, err)
		}
	}
}

var tokenRe = regexp.MustCompile(`rvw_[A-Za-z0-9_-]+`)

// ── users add ───────────────────────────────────────────────────────────────

func TestReviewUsersAddBootstrapAdmin(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	out, err := execUsersCmd(t, stdReviewAdminDeps{},
		"add", "root@example.com", "--role", "admin",
		"--users-file", usersPath, "--audit-log", logPath)
	if err != nil {
		t.Fatalf("bootstrap add: %v", err)
	}
	mustContain(t, out, "User added: root@example.com (admin)", "shown once", "argon2id hash")

	token := tokenRe.FindString(out)
	if token == "" {
		t.Fatalf("no plaintext token in output:\n%s", out)
	}
	if strings.Count(out, token) != 1 {
		t.Fatalf("token printed more than once:\n%s", out)
	}

	uf, err := auth.LoadUsersFile(usersPath)
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	if len(uf.Users) != 1 || uf.Users[0].Role != auth.RoleAdmin {
		t.Fatalf("unexpected users file: %+v", uf.Users)
	}
	if strings.Contains(uf.Users[0].TokenHash, token) {
		t.Fatal("plaintext token persisted in users file")
	}

	recs := auditRecordsOf(t, logPath)
	if len(recs) != 1 || recs[0].Action != audit.ActionUserCreate {
		t.Fatalf("unexpected audit records: %+v", recs)
	}
	if recs[0].Actor != reviewBootstrapActor || recs[0].AfterHash == "" || recs[0].RequestID == "" {
		t.Fatalf("bad audit record: %+v", recs[0])
	}
	res, err := audit.Open(logPath).Verify()
	if err != nil || !res.OK || res.TornAppend {
		t.Fatalf("audit chain not clean: %+v err=%v", res, err)
	}
}

func TestReviewUsersAddBootstrapRejectsNonAdmin(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	_, err := execUsersCmd(t, stdReviewAdminDeps{},
		"add", "rev@example.com", "--role", "reviewer",
		"--users-file", usersPath, "--audit-log", logPath)
	mustErrContain(t, err, "first user must be an admin")
	if _, statErr := os.Stat(usersPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("users file should not exist, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(logPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("audit log should not exist, stat err=%v", statErr)
	}
}

func TestReviewUsersAddWithRealAdminToken(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	setJSONFlag(t)
	out, err := execUsersCmd(t, stdReviewAdminDeps{},
		"add", "root@example.com", "--role", "admin",
		"--users-file", usersPath, "--audit-log", logPath)
	if err != nil {
		t.Fatalf("bootstrap add: %v", err)
	}
	var boot reviewUserAddJSON
	if err := json.Unmarshal([]byte(out), &boot); err != nil {
		t.Fatalf("parse bootstrap JSON: %v\n%s", err, out)
	}
	if boot.Token == "" || boot.Email != "root@example.com" || boot.Role != "admin" || boot.CreatedAt == "" {
		t.Fatalf("bad add JSON: %+v", boot)
	}
	Flags.JSON = false

	out, err = execUsersCmd(t, stdReviewAdminDeps{},
		"add", "rev@example.com", "--role", "reviewer", "--token", boot.Token,
		"--users-file", usersPath, "--audit-log", logPath)
	if err != nil {
		t.Fatalf("add reviewer with admin token: %v", err)
	}
	mustContain(t, out, "User added: rev@example.com (reviewer)")

	recs := auditRecordsOf(t, logPath)
	if len(recs) != 2 || recs[1].Actor != "root@example.com" || recs[1].Role != "admin" {
		t.Fatalf("unexpected audit records: %+v", recs)
	}
}

func TestReviewUsersAddAuthFailures(t *testing.T) {
	cases := []struct {
		name  string
		deps  fakeReviewAdminDeps
		token string
		wants []string
	}{
		{
			name:  "missing token",
			deps:  fakeReviewAdminDeps{},
			wants: []string{"review admin token is required"},
		},
		{
			name: "invalid token",
			deps: fakeReviewAdminDeps{authenticate: func(string, string) (auth.Identity, error) {
				return auth.Identity{}, auth.ErrUnauthenticated
			}},
			token: "rvw_bogus",
			wants: []string{"invalid review token"},
		},
		{
			name: "backend failure",
			deps: fakeReviewAdminDeps{authenticate: func(string, string) (auth.Identity, error) {
				return auth.Identity{}, errors.New("users file exploded")
			}},
			token: "rvw_any",
			wants: []string{"users file exploded"},
		},
		{
			name:  "non-admin role",
			deps:  adminIdentityFake(auth.Identity{Email: "rev@example.com", Role: auth.RoleReviewer}),
			token: "rvw_reviewer",
			wants: []string{"lacks permission", "admin-only"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usersPath, logPath := tempReviewPaths(t)
			writeReviewUsers(t, usersPath, seededAdmin())
			t.Setenv(reviewTokenEnv, "")
			args := []string{"add", "new@example.com", "--role", "reviewer",
				"--users-file", usersPath, "--audit-log", logPath}
			if tc.token != "" {
				args = append(args, "--token", tc.token)
			}
			_, err := execUsersCmd(t, tc.deps, args...)
			mustErrContain(t, err, tc.wants...)
		})
	}
}

func TestReviewUsersAddTokenFromEnv(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	var gotToken string
	deps := fakeReviewAdminDeps{authenticate: func(_, token string) (auth.Identity, error) {
		gotToken = token
		return adminID(), nil
	}}
	t.Setenv(reviewTokenEnv, "rvw_from_env")
	if _, err := execUsersCmd(t, deps,
		"add", "new@example.com", "--role", "readonly",
		"--users-file", usersPath, "--audit-log", logPath); err != nil {
		t.Fatalf("add with env token: %v", err)
	}
	if gotToken != "rvw_from_env" {
		t.Fatalf("authenticate saw token %q, want env value", gotToken)
	}
}

func TestReviewUsersAddRejectsInvalidRoleAndDuplicates(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	deps := adminIdentityFake(adminID())

	_, err := execUsersCmd(t, deps, "add", "x@example.com", "--role", "owner",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	// The enum PreRunE now rejects the value, naming the vocabulary itself.
	mustErrContain(t, err, "--role must be one of", "reviewer|admin|readonly")

	_, err = execUsersCmd(t, deps, "add", "admin@example.com", "--role", "reviewer",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "already exists")

	// Whitespace-only email survives cobra but is rejected by the store.
	_, err = execUsersCmd(t, deps, "add", "  ", "--role", "reviewer",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "empty email")
}

// ── users list ──────────────────────────────────────────────────────────────

func TestReviewUsersListEmpty(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	out, err := execUsersCmd(t, stdReviewAdminDeps{},
		"list", "--users-file", usersPath, "--audit-log", logPath)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	mustContain(t, out, "No review users in "+usersPath)

	setJSONFlag(t)
	out, err = execUsersCmd(t, stdReviewAdminDeps{},
		"list", "--users-file", usersPath, "--audit-log", logPath)
	if err != nil {
		t.Fatalf("list empty json: %v", err)
	}
	var payload reviewUsersListJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse list JSON: %v", err)
	}
	if len(payload.Users) != 0 {
		t.Fatalf("expected empty users, got %+v", payload.Users)
	}
}

func TestReviewUsersListTableShowsHashNotPlaintext(t *testing.T) {
	usersPath, _ := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin(),
		auth.User{Email: "rev@example.com", Role: auth.RoleReviewer, TokenHash: stubHash, CreatedAt: "2026-02-01T00:00:00Z"})
	deps := adminIdentityFake(adminID())

	out, err := execUsersCmd(t, deps, "list", "--users-file", usersPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	mustContain(t, out, "EMAIL", "TOKEN_HASH", "admin@example.com", "rev@example.com", "$argon2id$v=19$m=65536")
	if strings.Contains(out, stubHash) {
		t.Fatal("full token hash rendered; expected truncation")
	}
}

func TestReviewUsersListJSONOmitsHash(t *testing.T) {
	usersPath, _ := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	setJSONFlag(t)
	out, err := execUsersCmd(t, adminIdentityFake(adminID()),
		"list", "--users-file", usersPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("list json: %v", err)
	}
	if strings.Contains(out, "token_hash") || strings.Contains(out, "argon2id") {
		t.Fatalf("hash leaked into JSON:\n%s", out)
	}
	var payload reviewUsersListJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse list JSON: %v", err)
	}
	if len(payload.Users) != 1 || payload.Users[0].Email != "admin@example.com" {
		t.Fatalf("unexpected users: %+v", payload.Users)
	}
}

func TestReviewUsersListErrors(t *testing.T) {
	usersPath, _ := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())

	t.Setenv(reviewTokenEnv, "")
	_, err := execUsersCmd(t, fakeReviewAdminDeps{}, "list", "--users-file", usersPath)
	mustErrContain(t, err, "review admin token is required")

	loadErr := fakeReviewAdminDeps{loadUsers: func(string) (*auth.UsersFile, error) {
		return nil, errors.New("parse users file: boom")
	}}
	_, err = execUsersCmd(t, loadErr, "list", "--users-file", usersPath)
	mustErrContain(t, err, "parse users file: boom")

	pathErr := fakeReviewAdminDeps{defaultUsersPath: func() (string, error) {
		return "", errors.New("no home dir")
	}}
	_, err = execUsersCmd(t, pathErr, "list")
	mustErrContain(t, err, "no home dir")
}

// ── users remove ────────────────────────────────────────────────────────────

func TestReviewUsersRemove(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	rev := auth.User{Email: "rev@example.com", Role: auth.RoleReviewer, TokenHash: stubHash, CreatedAt: "2026-02-01T00:00:00Z"}
	writeReviewUsers(t, usersPath, seededAdmin(), rev)
	deps := adminIdentityFake(adminID())

	out, err := execUsersCmd(t, deps, "remove", "rev@example.com",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	mustContain(t, out, "User removed: rev@example.com (reviewer)")

	uf, err := auth.LoadUsersFile(usersPath)
	if err != nil || len(uf.Users) != 1 || uf.Users[0].Email != "admin@example.com" {
		t.Fatalf("users file after remove: %+v err=%v", uf, err)
	}
	recs := auditRecordsOf(t, logPath)
	if len(recs) != 1 || recs[0].Action != audit.ActionUserDelete {
		t.Fatalf("unexpected audit records: %+v", recs)
	}
	if recs[0].Target != "user/rev@example.com" || recs[0].BeforeHash == "" || recs[0].AfterHash != "" {
		t.Fatalf("bad delete record: %+v", recs[0])
	}
}

func TestReviewUsersRemoveJSONAndNotFound(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	// Two admins so removing the first does not trip the last-admin guard —
	// this case exercises the JSON output path, not the lockout guard.
	writeReviewUsers(t, usersPath, seededAdmin(), secondAdmin())
	deps := adminIdentityFake(adminID())

	_, err := execUsersCmd(t, deps, "remove", "ghost@example.com",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "user not found: ghost@example.com")
	if recs := auditRecordsOf(t, logPath); len(recs) != 0 {
		t.Fatalf("failed remove must not audit: %+v", recs)
	}

	setJSONFlag(t)
	out, err := execUsersCmd(t, deps, "remove", "admin@example.com",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("remove json: %v", err)
	}
	var payload reviewUserRemoveJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse remove JSON: %v", err)
	}
	if !payload.Removed || payload.Email != "admin@example.com" {
		t.Fatalf("bad remove JSON: %+v", payload)
	}
}

// ── users set-role ──────────────────────────────────────────────────────────

func TestReviewUsersSetRole(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	rev := auth.User{Email: "rev@example.com", Role: auth.RoleReviewer, TokenHash: stubHash, CreatedAt: "2026-02-01T00:00:00Z"}
	writeReviewUsers(t, usersPath, seededAdmin(), rev)
	deps := adminIdentityFake(adminID())

	out, err := execUsersCmd(t, deps, "set-role", "rev@example.com", "--role", "readonly",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("set-role: %v", err)
	}
	mustContain(t, out, "Role changed: rev@example.com reviewer -> readonly")

	uf, err := auth.LoadUsersFile(usersPath)
	if err != nil {
		t.Fatalf("load users: %v", err)
	}
	if got, ok := uf.Find("rev@example.com"); !ok || got.Role != auth.RoleReadonly {
		t.Fatalf("role not persisted: %+v ok=%v", got, ok)
	}

	recs := auditRecordsOf(t, logPath)
	if len(recs) != 1 || recs[0].Action != audit.ActionRoleChange {
		t.Fatalf("unexpected audit records: %+v", recs)
	}
	if recs[0].BeforeHash == "" || recs[0].AfterHash == "" || recs[0].BeforeHash == recs[0].AfterHash {
		t.Fatalf("role-change hashes must differ: %+v", recs[0])
	}
}

func TestReviewUsersSetRoleErrorsAndJSON(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	// Two admins so demoting the first exercises the JSON output path rather
	// than the last-admin lockout guard (which the dedicated guard test covers).
	writeReviewUsers(t, usersPath, seededAdmin(), secondAdmin())
	deps := adminIdentityFake(adminID())

	_, err := execUsersCmd(t, deps, "set-role", "rev@example.com", "--role", "supreme",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "--role must be one of", "reviewer|admin|readonly")

	_, err = execUsersCmd(t, deps, "set-role", "ghost@example.com", "--role", "admin",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "user not found")

	setJSONFlag(t)
	out, err := execUsersCmd(t, deps, "set-role", "admin@example.com", "--role", "readonly",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("set-role json: %v", err)
	}
	var payload reviewSetRoleJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse set-role JSON: %v", err)
	}
	if payload.Role != "readonly" || payload.PreviousRole != "admin" {
		t.Fatalf("bad set-role JSON: %+v", payload)
	}
}

// ── last-admin lockout guard ────────────────────────────────────────────────

// TestReviewUsersLastAdminGuard covers the guard that keeps the users file from
// ever reaching zero admins (a permanent CLI lockout, since bootstrap only fires
// on an EMPTY file): demoting or deleting the sole admin is refused with the file
// and audit log untouched, while the same operations succeed once a second admin
// exists.
func TestReviewUsersLastAdminGuard(t *testing.T) {
	deps := adminIdentityFake(adminID())

	t.Run("sole admin demote refused", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		writeReviewUsers(t, usersPath, seededAdmin())
		_, err := execUsersCmd(t, deps, "set-role", "admin@example.com", "--role", "readonly",
			"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
		mustErrContain(t, err, "refusing to demote the last admin", "no CLI recovery path")
		assertSoleAdminUntouched(t, usersPath, logPath)
	})

	t.Run("sole admin delete refused", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		writeReviewUsers(t, usersPath, seededAdmin())
		_, err := execUsersCmd(t, deps, "remove", "admin@example.com",
			"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
		mustErrContain(t, err, "refusing to remove the last admin", "no CLI recovery path")
		assertSoleAdminUntouched(t, usersPath, logPath)
	})

	t.Run("demote succeeds with another admin", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		writeReviewUsers(t, usersPath, seededAdmin(), secondAdmin())
		if _, err := execUsersCmd(t, deps, "set-role", "admin@example.com", "--role", "readonly",
			"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t"); err != nil {
			t.Fatalf("demote with spare admin should succeed: %v", err)
		}
		uf, _ := auth.LoadUsersFile(usersPath)
		if got, _ := uf.Find("admin@example.com"); got.Role != auth.RoleReadonly {
			t.Fatalf("role not demoted: %+v", got)
		}
	})

	t.Run("delete succeeds with another admin", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		writeReviewUsers(t, usersPath, seededAdmin(), secondAdmin())
		if _, err := execUsersCmd(t, deps, "remove", "admin@example.com",
			"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t"); err != nil {
			t.Fatalf("delete with spare admin should succeed: %v", err)
		}
		uf, _ := auth.LoadUsersFile(usersPath)
		if _, ok := uf.Find("admin@example.com"); ok {
			t.Fatalf("admin should have been removed")
		}
	})
}

// assertSoleAdminUntouched confirms a refused last-admin mutation left the users
// file (still one admin) and audit log unchanged — the guard fails before any
// write.
func assertSoleAdminUntouched(t *testing.T, usersPath, logPath string) {
	t.Helper()
	uf, err := auth.LoadUsersFile(usersPath)
	if err != nil {
		t.Fatalf("load users after refusal: %v", err)
	}
	if got, ok := uf.Find("admin@example.com"); !ok || got.Role != auth.RoleAdmin {
		t.Fatalf("sole admin must be unchanged after refusal: %+v ok=%v", got, ok)
	}
	if recs := auditRecordsOf(t, logPath); len(recs) != 0 {
		t.Fatalf("refused mutation must not audit: %+v", recs)
	}
}

// ── mutation pipeline failure modes ─────────────────────────────────────────

func TestReviewUsersMutationPipelineErrors(t *testing.T) {
	bang := errors.New("bang")
	cases := []struct {
		name  string
		deps  fakeReviewAdminDeps
		wants []string
	}{
		{
			name:  "default users path",
			deps:  fakeReviewAdminDeps{defaultUsersPath: func() (string, error) { return "", bang }},
			wants: []string{"bang"},
		},
		{
			name: "lock failure",
			deps: fakeReviewAdminDeps{acquireFileLock: func(string) (func() error, error) {
				return nil, bang
			}},
			wants: []string{"lock users file"},
		},
		{
			name:  "load failure",
			deps:  fakeReviewAdminDeps{loadUsers: func(string) (*auth.UsersFile, error) { return nil, bang }},
			wants: []string{"bang"},
		},
		{
			name:  "pre-image read failure",
			deps:  fakeReviewAdminDeps{readFile: func(string) ([]byte, error) { return nil, bang }},
			wants: []string{"capture users-file pre-image"},
		},
		{
			name:  "save failure",
			deps:  fakeReviewAdminDeps{saveUsers: func(*auth.UsersFile, string) error { return bang }},
			wants: []string{"bang"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usersPath, logPath := tempReviewPaths(t)
			args := []string{"add", "root@example.com", "--role", "admin", "--audit-log", logPath}
			if tc.deps.defaultUsersPath == nil {
				args = append(args, "--users-file", usersPath)
			}
			_, err := execUsersCmd(t, tc.deps, args...)
			mustErrContain(t, err, tc.wants...)
		})
	}
}

func TestReviewUsersAuditFailClosedRollsBackExistingFile(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	pre, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatalf("read pre-image: %v", err)
	}
	deps := adminIdentityFake(adminID())
	deps.auditAppend = func(string, audit.Event) (audit.Record, error) {
		return audit.Record{}, errors.New("disk full")
	}
	_, err = execUsersCmd(t, deps, "add", "rev@example.com", "--role", "reviewer",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "audit append failed", "rolled back", "request_id=cli-", "disk full")

	post, readErr := os.ReadFile(usersPath)
	if readErr != nil {
		t.Fatalf("read post-image: %v", readErr)
	}
	if !bytes.Equal(pre, post) {
		t.Fatalf("users file not rolled back:\npre:\n%s\npost:\n%s", pre, post)
	}
}

func TestReviewUsersAuditFailClosedRemovesCreatedFile(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	deps := fakeReviewAdminDeps{auditAppend: func(string, audit.Event) (audit.Record, error) {
		return audit.Record{}, errors.New("chain sealed")
	}}
	_, err := execUsersCmd(t, deps, "add", "root@example.com", "--role", "admin",
		"--users-file", usersPath, "--audit-log", logPath)
	mustErrContain(t, err, "rolled back")
	if _, statErr := os.Stat(usersPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("bootstrap users file should have been removed, stat err=%v", statErr)
	}
}

func TestReviewUsersAuditFailClosedRollbackVariants(t *testing.T) {
	appendFail := func(string, audit.Event) (audit.Record, error) {
		return audit.Record{}, errors.New("append down")
	}

	t.Run("restore write fails", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		writeReviewUsers(t, usersPath, seededAdmin())
		deps := adminIdentityFake(adminID())
		deps.auditAppend = appendFail
		deps.writeFileAtomic = func(string, []byte) error { return errors.New("readonly fs") }
		_, err := execUsersCmd(t, deps, "add", "rev@example.com", "--role", "reviewer",
			"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
		mustErrContain(t, err, "UNAUDITED", "readonly fs")
	})

	t.Run("remove fails", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		deps := fakeReviewAdminDeps{auditAppend: appendFail,
			remove: func(string) error { return errors.New("held open") }}
		_, err := execUsersCmd(t, deps, "add", "root@example.com", "--role", "admin",
			"--users-file", usersPath, "--audit-log", logPath)
		mustErrContain(t, err, "UNAUDITED", "held open")
	})

	t.Run("remove says not-exist", func(t *testing.T) {
		usersPath, logPath := tempReviewPaths(t)
		deps := fakeReviewAdminDeps{auditAppend: appendFail,
			remove: func(string) error { return os.ErrNotExist }}
		_, err := execUsersCmd(t, deps, "add", "root@example.com", "--role", "admin",
			"--users-file", usersPath, "--audit-log", logPath)
		mustErrContain(t, err, "rolled back")
	})
}

// ── audit tail ──────────────────────────────────────────────────────────────

// seedAuditLog writes n chained records through the real audit package.
func seedAuditLog(t *testing.T, logPath string, n int) {
	t.Helper()
	log := audit.Open(logPath)
	for i := 0; i < n; i++ {
		if _, err := log.Append(audit.Event{
			Actor:  "admin@example.com",
			Role:   "admin",
			Action: audit.ActionUserCreate,
			Target: "user/seed",
		}); err != nil {
			t.Fatalf("seed audit record %d: %v", i, err)
		}
	}
}

func TestReviewAuditTail(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	seedAuditLog(t, logPath, 3)
	deps := adminIdentityFake(adminID())

	out, err := execAuditCmd(t, deps, "tail",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	mustContain(t, out, "ACTOR", "user.create", "user/seed", "Showing 3 of 3 record(s)")

	// The deprecated `view` alias still resolves to the same command.
	out, err = execAuditCmd(t, deps, "view", "--limit", "1",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("view alias --limit: %v", err)
	}
	mustContain(t, out, "Showing 1 of 3 record(s)")

	setJSONFlag(t)
	out, err = execAuditCmd(t, deps, "tail", "--limit", "2",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("tail json: %v", err)
	}
	var payload reviewAuditTailJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse tail JSON: %v", err)
	}
	if payload.Total != 3 || len(payload.Records) != 2 {
		t.Fatalf("bad tail JSON: total=%d records=%d", payload.Total, len(payload.Records))
	}
}

func TestReviewAuditTailEmptyAndErrors(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	deps := adminIdentityFake(adminID())

	out, err := execAuditCmd(t, deps, "tail",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("tail empty: %v", err)
	}
	mustContain(t, out, "No audit records in "+logPath)

	_, err = execAuditCmd(t, deps, "tail", "--limit", "-2",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "--limit must be a non-negative integer")

	readonly := adminIdentityFake(auth.Identity{Email: "ro@example.com", Role: auth.RoleReadonly})
	_, err = execAuditCmd(t, readonly, "tail",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "lacks permission audit:read")

	pathErr := fakeReviewAdminDeps{defaultUsersPath: func() (string, error) { return "", errors.New("no home") }}
	_, err = execAuditCmd(t, pathErr, "tail", "--audit-log", logPath)
	mustErrContain(t, err, "no home")

	// A malformed line makes the real Records() fail.
	if err := os.WriteFile(logPath, []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("corrupt log: %v", err)
	}
	_, err = execAuditCmd(t, deps, "tail",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "parse log line 1")
}

// ── audit verify ────────────────────────────────────────────────────────────

func TestReviewAuditVerifyOK(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	seedAuditLog(t, logPath, 3)

	out, err := execAuditCmd(t, stdReviewAdminDeps{}, "verify", "--audit-log", logPath)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	mustContain(t, out, "audit chain OK — 3 record(s)")

	setJSONFlag(t)
	out, err = execAuditCmd(t, stdReviewAdminDeps{}, "verify", "--audit-log", logPath)
	if err != nil {
		t.Fatalf("verify json: %v", err)
	}
	var payload reviewAuditVerifyJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse verify JSON: %v", err)
	}
	if !payload.OK || payload.Count != 3 {
		t.Fatalf("bad verify JSON: %+v", payload)
	}
}

// tamperAuditRecord flips content inside record line n (1-based) in place.
func tamperAuditRecord(t *testing.T, logPath string, n int) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	lines[n-1] = strings.Replace(lines[n-1], "admin@example.com", "evil1@example.com", 1)
	if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write tampered log: %v", err)
	}
}

func TestReviewAuditVerifyDetectsTamper(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	seedAuditLog(t, logPath, 3)
	tamperAuditRecord(t, logPath, 2)

	_, err := execAuditCmd(t, stdReviewAdminDeps{}, "verify", "--audit-log", logPath)
	mustErrContain(t, err, "integrity break at record 3", "record 2 was altered or removed")
}

// TestReviewAuditVerifyFailsOnTornAppend pins the fail-closed security
// invariant: a torn-append state (head anchor one behind) is byte-for-byte
// indistinguishable from a single forged tail record, so verify must return
// a non-zero-exit error naming the forgery risk explicitly.
func TestReviewAuditVerifyFailsOnTornAppend(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	seedAuditLog(t, logPath, 1)
	if err := os.Remove(logPath + ".head"); err != nil {
		t.Fatalf("remove head anchor: %v", err)
	}
	_, err := execAuditCmd(t, stdReviewAdminDeps{}, "verify", "--audit-log", logPath)
	mustErrContain(t, err, "integrity failure", "torn or possibly-forged tail", "forged", "da review audit repair")
}

// TestReviewAuditVerifyDefaultLogPath pins the repo-relative default audit
// log path (design D5.4) when --audit-log is not passed.
func TestReviewAuditVerifyDefaultLogPath(t *testing.T) {
	t.Chdir(t.TempDir())
	out, err := execAuditCmd(t, stdReviewAdminDeps{}, "verify")
	if err != nil {
		t.Fatalf("verify default path: %v", err)
	}
	mustContain(t, out, "audit chain OK — 0 record(s) in "+defaultReviewAuditLog)
}

func TestReviewAuditVerifyDepsError(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	deps := fakeReviewAdminDeps{auditVerify: func(string) (audit.VerifyResult, error) {
		return audit.VerifyResult{}, errors.New("cannot read log")
	}}
	_, err := execAuditCmd(t, deps, "verify", "--audit-log", logPath)
	mustErrContain(t, err, "cannot read log")
}

// ── audit repair ────────────────────────────────────────────────────────────

func TestReviewAuditRepairHealsTornAppend(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	seedAuditLog(t, logPath, 2)
	if err := os.Remove(logPath + ".head"); err != nil {
		t.Fatalf("remove head anchor: %v", err)
	}
	// Two unanchored records are tamper; rebuild the torn shape (one ahead)
	// by re-anchoring at record 1 via a fresh single-record log instead.
	if err := os.Remove(logPath); err != nil {
		t.Fatalf("reset log: %v", err)
	}
	seedAuditLog(t, logPath, 1)
	if err := os.Remove(logPath + ".head"); err != nil {
		t.Fatalf("remove head anchor: %v", err)
	}

	deps := adminIdentityFake(adminID())
	out, err := execAuditCmd(t, deps, "repair",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	mustContain(t, out, "audit head anchor is consistent — 1 record(s)")

	res, err := audit.Open(logPath).Verify()
	if err != nil || !res.OK || res.TornAppend {
		t.Fatalf("post-repair verify: %+v err=%v", res, err)
	}
}

func TestReviewAuditRepairErrors(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	writeReviewUsers(t, usersPath, seededAdmin())
	deps := adminIdentityFake(adminID())

	seedAuditLog(t, logPath, 3)
	tamperAuditRecord(t, logPath, 2)
	_, err := execAuditCmd(t, deps, "repair",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "integrity break")

	t.Setenv(reviewTokenEnv, "")
	_, err = execAuditCmd(t, fakeReviewAdminDeps{}, "repair",
		"--users-file", usersPath, "--audit-log", logPath)
	mustErrContain(t, err, "review admin token is required")

	pathErr := fakeReviewAdminDeps{defaultUsersPath: func() (string, error) { return "", errors.New("no home") }}
	_, err = execAuditCmd(t, pathErr, "repair", "--audit-log", logPath)
	mustErrContain(t, err, "no home")

	repairErr := adminIdentityFake(adminID())
	repairErr.auditRepairHead = func(string) (audit.VerifyResult, error) {
		return audit.VerifyResult{}, errors.New("lock stuck")
	}
	_, err = execAuditCmd(t, repairErr, "repair",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "lock stuck")

	setJSONFlag(t)
	okDeps := adminIdentityFake(adminID())
	out, err := execAuditCmd(t, okDeps, "repair",
		"--users-file", usersPath, "--audit-log", filepath.Join(t.TempDir(), "fresh.jsonl"), "--token", "rvw_t")
	if err != nil {
		t.Fatalf("repair clean json: %v", err)
	}
	var payload reviewAuditVerifyJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse repair JSON: %v", err)
	}
	if !payload.OK || payload.Count != 0 {
		t.Fatalf("bad repair JSON: %+v", payload)
	}
}

// ── audit prune ─────────────────────────────────────────────────────────────

// seedAuditArchives appends one record per year through the real audit package,
// so each year boundary rotates the active log into a dated year-archive. After
// the call the earlier years are frozen archives and the final year is live.
func seedAuditArchives(t *testing.T, logPath string, years ...int) {
	t.Helper()
	log := audit.Open(logPath)
	for _, y := range years {
		if _, err := log.Append(audit.Event{
			Actor:  "admin@example.com",
			Role:   "admin",
			Action: audit.ActionUserCreate,
			Target: "user/seed",
			Now:    time.Date(y, 2, 3, 4, 5, 6, 0, time.UTC),
		}); err != nil {
			t.Fatalf("seed audit year %d: %v", y, err)
		}
	}
}

func TestReviewAuditPruneCompactsOldArchives(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	seedAuditArchives(t, logPath, 2022, 2023, 2024, 2026)
	base := strings.TrimSuffix(logPath, ".jsonl")
	deps := adminIdentityFake(adminID())

	out, err := execAuditCmd(t, deps, "prune", "--before-year", "2024",
		"--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	mustContain(t, out, "Compacted 2 audit archive(s) older than 2024",
		base+".2022.jsonl", base+".2023.jsonl")

	for _, gone := range []string{base + ".2022.jsonl", base + ".2022.jsonl.head", base + ".2023.jsonl"} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, stat err=%v", gone, err)
		}
	}
	for _, kept := range []string{base + ".2024.jsonl", logPath} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("expected %s kept: %v", kept, err)
		}
	}
}

func TestReviewAuditPruneJSONAndEmpty(t *testing.T) {
	_, logPath := tempReviewPaths(t)
	seedAuditArchives(t, logPath, 2024, 2026) // one archive (2024), active 2026
	deps := adminIdentityFake(adminID())

	out, err := execAuditCmd(t, deps, "prune", "--before-year", "2024",
		"--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("prune empty: %v", err)
	}
	mustContain(t, out, "No audit archives older than 2024 to compact")

	setJSONFlag(t)
	out, err = execAuditCmd(t, deps, "prune", "--before-year", "2025",
		"--audit-log", logPath, "--token", "rvw_t")
	if err != nil {
		t.Fatalf("prune json: %v", err)
	}
	var payload reviewAuditPruneJSON
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse prune JSON: %v", err)
	}
	if payload.Count != 1 || payload.BeforeYear != 2025 || len(payload.Removed) != 1 {
		t.Fatalf("bad prune JSON: %+v", payload)
	}
}

func TestReviewAuditPruneErrors(t *testing.T) {
	usersPath, logPath := tempReviewPaths(t)
	deps := adminIdentityFake(adminID())

	// Non-four-digit years are rejected at both bounds and at zero.
	for _, bad := range []string{"0", "999", "10000"} {
		_, err := execAuditCmd(t, deps, "prune", "--before-year", bad,
			"--audit-log", logPath, "--token", "rvw_t")
		mustErrContain(t, err, "--before-year must be a four-digit year (1000-9999)")
	}

	_, err := execAuditCmd(t, deps, "prune", "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "before-year", "not set")

	readonly := adminIdentityFake(auth.Identity{Email: "ro@example.com", Role: auth.RoleReadonly})
	_, err = execAuditCmd(t, readonly, "prune", "--before-year", "2025",
		"--users-file", usersPath, "--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "lacks permission audit:read")

	pathErr := fakeReviewAdminDeps{defaultUsersPath: func() (string, error) { return "", errors.New("no home") }}
	_, err = execAuditCmd(t, pathErr, "prune", "--before-year", "2025", "--audit-log", logPath)
	mustErrContain(t, err, "no home")

	unprunable := adminIdentityFake(adminID())
	unprunable.auditPrune = func(string, int) ([]string, error) {
		return []string{"/x/audit.log.2020.jsonl"}, fmt.Errorf("%w: /x/audit.log.2021.jsonl", audit.ErrUnprunableArchive)
	}
	_, err = execAuditCmd(t, unprunable, "prune", "--before-year", "2025",
		"--audit-log", logPath, "--token", "rvw_t")
	mustErrContain(t, err, "audit prune incomplete (1 archive(s) compacted)", "/x/audit.log.2021.jsonl")
}

// ── wiring + std deps ───────────────────────────────────────────────────────

// TestWithReviewAdminAttachesWithoutCollisions pins the plan's naming
// decision: users/audit nest under the existing `da review` group and the
// proposal subcommands survive intact.
func TestWithReviewAdminAttachesWithoutCollisions(t *testing.T) {
	review := withReviewAdmin(NewReviewCmd())
	names := map[string]bool{}
	for _, sub := range review.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"show", "approve", "reject", "users", "audit"} {
		if !names[want] {
			t.Fatalf("`da review` is missing subcommand %q: %v", want, names)
		}
	}
}

func TestStdReviewAdminDepsDefaultUsersPath(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	got, err := stdReviewAdminDeps{}.DefaultUsersPath()
	if err != nil {
		t.Fatalf("DefaultUsersPath: %v", err)
	}
	want := filepath.Join(cfg, "da", "review", "users.yaml")
	if got != want {
		t.Fatalf("DefaultUsersPath = %q, want %q", got, want)
	}
}

// TestStdReviewAdminDepsAuthenticate covers the production authenticator
// delegate end-to-end: mint through the real store, verify through argon2.
func TestStdReviewAdminDepsAuthenticate(t *testing.T) {
	usersPath, _ := tempReviewPaths(t)
	uf := &auth.UsersFile{}
	token, err := uf.AddUser("admin@example.com", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if err := uf.Save(usersPath); err != nil {
		t.Fatalf("save users: %v", err)
	}
	id, err := stdReviewAdminDeps{}.Authenticate(usersPath, token)
	if err != nil || id.Email != "admin@example.com" || id.Role != auth.RoleAdmin {
		t.Fatalf("Authenticate = %+v, %v", id, err)
	}
}
