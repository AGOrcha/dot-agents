package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/review/audit"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

const (
	auditStateRef = "refs/agents/state"
	auditLogRel   = ".agents/active/review/audit.log.jsonl"
)

func auditLogPathIn(repo string) string {
	return filepath.Join(repo, filepath.FromSlash(auditLogRel))
}

func auditMirrorEvent() audit.Event {
	return audit.Event{
		Actor:  "admin@example.com",
		Role:   "admin",
		Action: audit.ActionUserCreate,
		Target: "user/x",
	}
}

// stateRefExists reports whether refs/agents/state is present in repo, checking
// both the loose-ref file and packed-refs — an exec-free proxy for "the mirror
// created the coordination ref" (the state-ref blob contents are verified in
// commands/workflow's mirror tests).
func stateRefExists(t *testing.T, repo string) bool {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repo, ".git", "refs", "agents", "state")); err == nil {
		return true
	}
	packed, err := os.ReadFile(filepath.Join(repo, ".git", "packed-refs"))
	return err == nil && strings.Contains(string(packed), auditStateRef)
}

// TestStdAuditAppend_MirrorsUnderGitRef proves the CLI audit-log writer routes
// through workflow.MirrorReviewAuditLogToStateRef under backend=git-ref: the
// append succeeds and the coordination ref is created.
func TestStdAuditAppend_MirrorsUnderGitRef(t *testing.T) {
	repo := t.TempDir()
	testutil.InitGitRepo(t, repo, map[string]string{
		".agentsrc.json": `{"version":1,"project":"p","sources":[{"type":"local"}],"work_tracking":{"backend":"git-ref"}}`,
	})

	if _, err := (stdReviewAdminDeps{}).AuditAppend(auditLogPathIn(repo), auditMirrorEvent()); err != nil {
		t.Fatalf("AuditAppend: %v", err)
	}
	if !stateRefExists(t, repo) {
		t.Errorf("backend=git-ref audit append must mirror to %s", auditStateRef)
	}
}

// TestStdAuditAppend_DefaultBackendNoMirror proves the hard gate: without the
// git-ref backend the CLI audit writer touches no ref.
func TestStdAuditAppend_DefaultBackendNoMirror(t *testing.T) {
	repo := t.TempDir()
	testutil.InitGitRepo(t, repo, map[string]string{
		".agentsrc.json": `{"version":1,"project":"p","sources":[{"type":"local"}]}`,
	})
	if _, err := (stdReviewAdminDeps{}).AuditAppend(auditLogPathIn(repo), auditMirrorEvent()); err != nil {
		t.Fatalf("AuditAppend: %v", err)
	}
	if stateRefExists(t, repo) {
		t.Fatalf("default backend must not create %s for the audit log", auditStateRef)
	}
}

// TestStdAuditAppend_AppendErrorPropagates covers the append-failure branch of
// the mirror-wired AuditAppend: a log path whose parent cannot be created makes
// audit.Append fail before the mirror runs.
func TestStdAuditAppend_AppendErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(blocker, "audit.log.jsonl")
	if _, err := (stdReviewAdminDeps{}).AuditAppend(logPath, auditMirrorEvent()); err == nil {
		t.Fatal("expected audit append error when the log dir cannot be created")
	}
}
