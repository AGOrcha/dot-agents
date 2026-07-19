package workflow

import (
	"fmt"
	"os"

	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitRefWT / writeToWT are the work_tracking blocks the mirror tests seed.
// reviewAuditRel is the repo-relative ref key for the review audit log.
const (
	gitRefWT       = `{"backend":"git-ref"}`
	writeToWT      = `{"write_to":"state-ref"}`
	reviewAuditRel = ".agents/active/review/audit.log.jsonl"
	auditLogFile   = "audit.log.jsonl"
)

// managedArtifactRelPaths are the repo-relative state-ref keys the five managed
// workflow artifacts mirror to, matching what writeAllManagedArtifacts writes.
var managedArtifactRelPaths = []string{
	".agents/active/delegation/t1.yaml",
	".agents/active/delegation-bundles/del-1.yaml",
	".agents/active/merge-back/t1.md",
	".agents/active/verification/task-1/merge-back.result.yaml",
	".agents/active/verification/task-1/review-decision.yaml",
}

// writeAllManagedArtifacts drives the five in-package managed-artifact writers
// with valid inputs, each of which mirrors to refs/agents/state when the
// backend is active. Task/plan/delegation ids match managedArtifactRelPaths.
func writeAllManagedArtifacts(t *testing.T, repo string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveDelegationContract(repo, &DelegationContract{
		SchemaVersion: 1, ParentTaskID: "t1", ParentPlanID: "p1",
		Title: "x", Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("saveDelegationContract: %v", err)
	}
	if err := saveDelegationBundle(repo, &delegationBundleYAML{
		SchemaVersion: 1, DelegationID: "del-1", PlanID: "p1", TaskID: "t1",
	}); err != nil {
		t.Fatalf("saveDelegationBundle: %v", err)
	}
	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion: 1, TaskID: "t1", ParentPlanID: "p1",
		Title: "x", Summary: "s", IntegrationNotes: "n", CreatedAt: now,
	}); err != nil {
		t.Fatalf("saveMergeBack: %v", err)
	}
	if err := writeVerificationResultYAML(repo, newValidVerificationResultDoc()); err != nil {
		t.Fatalf("writeVerificationResultYAML: %v", err)
	}
	if err := writeReviewDecisionYAML(repo, newValidReviewDecisionDoc()); err != nil {
		t.Fatalf("writeReviewDecisionYAML: %v", err)
	}
}

// refBlobExists reports whether rel is present in refs/agents/state's tree.
func refBlobExists(t *testing.T, repo, rel string) bool {
	t.Helper()
	return stateRefPathExists(repo, stateRefHead(repo), rel)
}

// TestManagedArtifactMirror_GitRefBackendMirrorsEachType proves the contract:
// under backend=git-ref every managed artifact type lands on refs/agents/state
// at its repo-relative working-copy path, AND the working copy is still written
// (additive).
func TestManagedArtifactMirror_GitRefBackendMirrorsEachType(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	writeAllManagedArtifacts(t, repo)

	if stateRefHead(repo) == "" {
		t.Fatal("backend=git-ref must create refs/agents/state after a managed-artifact write")
	}
	for _, rel := range managedArtifactRelPaths {
		if !refBlobExists(t, repo, rel) {
			t.Errorf("managed artifact %q not mirrored to %s", rel, stateRefName)
		}
		// Additive: the working copy is still present.
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			t.Errorf("working copy %q missing (mirror must be additive): %v", rel, err)
		}
	}
	// Spot-check content fidelity on the ref (not just presence).
	blob := gitOut(t, repo, "show", stateRefName+":.agents/active/delegation/t1.yaml")
	if !strings.Contains(blob, "parent_task_id: t1") {
		t.Fatalf("ref blob content not faithful:\n%s", blob)
	}
}

// TestManagedArtifactMirror_WriteToStateRefAlsoMirrors proves the gate matches
// plan-state: the additive write_to=state-ref mode (no git-ref backend) also
// mirrors managed artifacts.
func TestManagedArtifactMirror_WriteToStateRefAlsoMirrors(t *testing.T) {
	repo := seedGitRefBackendRepo(t, writeToWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	writeAllManagedArtifacts(t, repo)

	if stateRefHead(repo) == "" {
		t.Fatal("write_to=state-ref must mirror managed artifacts to the ref")
	}
	for _, rel := range managedArtifactRelPaths {
		if !refBlobExists(t, repo, rel) {
			t.Errorf("managed artifact %q not mirrored under write_to=state-ref", rel)
		}
	}
}

// TestManagedArtifactMirror_DefaultBackendNoRefWrite is the HARD GATE: under
// the default/local backend NO managed artifact touches the ref — the
// working-copy-only path stays byte-identical.
func TestManagedArtifactMirror_DefaultBackendNoRefWrite(t *testing.T) {
	repo := seedGitRefBackendRepo(t, "", tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	saveDelegationBundleWithBaseArtifact(t, repo)
	writeAllManagedArtifacts(t, repo)

	if head := stateRefHead(repo); head != "" {
		t.Fatalf("default backend must NOT create the state ref, got %q", head)
	}
	// Working copies are still written under the default backend.
	for _, rel := range managedArtifactRelPaths {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); err != nil {
			t.Errorf("default backend must still write working copy %q: %v", rel, err)
		}
	}
}

// saveDelegationBundleWithBaseArtifact exercises the SECOND bundle writer
// (saveDelegationBundleWithBase) so its mirror line is covered too.
func saveDelegationBundleWithBaseArtifact(t *testing.T, repo string) {
	t.Helper()
	if err := saveDelegationBundleWithBase(repo, &delegationBundleYAML{
		SchemaVersion: 1, DelegationID: "del-2", PlanID: "p1", TaskID: "t1",
	}, nil); err != nil {
		t.Fatalf("saveDelegationBundleWithBase: %v", err)
	}
}

// TestManagedArtifactMirror_WithBaseMirrorsUnderGitRef proves the second bundle
// writer also mirrors under git-ref.
func TestManagedArtifactMirror_WithBaseMirrorsUnderGitRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	saveDelegationBundleWithBaseArtifact(t, repo)
	if !refBlobExists(t, repo, ".agents/active/delegation-bundles/del-2.yaml") {
		t.Fatal("saveDelegationBundleWithBase must mirror to the ref under git-ref")
	}
}

// TestManagedArtifactMirror_FailurePropagatesUnderGitRef proves the mode-aware
// policy: under backend=git-ref a mirror failure PROPAGATES from the writer.
func TestManagedArtifactMirror_FailurePropagatesUnderGitRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	prev := casSwapFn
	t.Cleanup(func() { casSwapFn = prev })
	casSwapFn = func(string, string, string) error { return fmt.Errorf("simulated ref move") }

	err := saveDelegationContract(repo, &DelegationContract{ParentTaskID: "t1"})
	if err == nil || !strings.Contains(err.Error(), "failed to mirror to "+stateRefName) {
		t.Fatalf("git-ref mirror failure must propagate, got %v", err)
	}
	// The working copy was still written before the mirror ran.
	if _, statErr := os.Stat(filepath.Join(repo, ".agents", "active", "delegation", "t1.yaml")); statErr != nil {
		t.Fatalf("working copy must be written before the mirror: %v", statErr)
	}
}

// TestManagedArtifactMirror_FailureWarnsUnderWriteTo proves the other half of
// the mode-aware policy: under the additive write_to=state-ref mode a mirror
// failure is WARNED, not propagated (the writer still returns nil).
func TestManagedArtifactMirror_FailureWarnsUnderWriteTo(t *testing.T) {
	repo := seedGitRefBackendRepo(t, writeToWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	prev := casSwapFn
	t.Cleanup(func() { casSwapFn = prev })
	casSwapFn = func(string, string, string) error { return fmt.Errorf("simulated ref move") }

	if err := saveDelegationContract(repo, &DelegationContract{ParentTaskID: "t1"}); err != nil {
		t.Fatalf("write_to=state-ref mirror failure must be warn-only, got %v", err)
	}
}

// TestManagedArtifactMirror_PreservesUnrelatedRefPaths proves the CAS overlay
// keeps a co-located session's coordination/plan ref blobs intact while adding
// the artifact blob.
func TestManagedArtifactMirror_PreservesUnrelatedRefPaths(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	// Seed an unrelated coordination blob on the ref (mirrors what a plan-state
	// transition would leave behind).
	const coordRel = ".agents/workflow/plans/srw/tasks/t1.yaml"
	if err := writeStateRefCAS(repo, []stateRefFile{{relPath: coordRel, content: []byte("id: t1\nstatus: in_progress\n")}}); err != nil {
		t.Fatalf("seed coordination blob: %v", err)
	}
	if err := saveMergeBack(repo, &MergeBackSummary{SchemaVersion: 1, TaskID: "t1", ParentPlanID: "p1", Title: "x", Summary: "s", IntegrationNotes: "n"}); err != nil {
		t.Fatalf("saveMergeBack: %v", err)
	}
	if !refBlobExists(t, repo, coordRel) {
		t.Error("unrelated coordination blob was clobbered by the artifact mirror")
	}
	if !refBlobExists(t, repo, ".agents/active/merge-back/t1.md") {
		t.Error("artifact blob missing after mirror")
	}
}

// TestManagedArtifactMirror_Idempotent proves the tree-equality guard: mirroring
// identical content twice produces NO new ref commit.
func TestManagedArtifactMirror_Idempotent(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	rel := ".agents/active/merge-back/idem.md"
	if err := mirrorArtifactToStateRef(repo, rel, []byte("same\n")); err != nil {
		t.Fatalf("first mirror: %v", err)
	}
	first := stateRefHead(repo)
	if err := mirrorArtifactToStateRef(repo, rel, []byte("same\n")); err != nil {
		t.Fatalf("second mirror: %v", err)
	}
	if second := stateRefHead(repo); second != first {
		t.Fatalf("identical re-mirror must not advance the ref: %q -> %q", first, second)
	}
}

// TestMirrorArtifactFilesToStateRef_EmptyNoop proves an empty file set under an
// active backend is a no-op (no ref commit) and covers the empty-what branch.
func TestMirrorArtifactFilesToStateRef_EmptyNoop(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	if err := mirrorArtifactFilesToStateRef(repo, nil); err != nil {
		t.Fatalf("empty mirror must be a no-op, got %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("empty mirror must not create the ref, got %q", head)
	}
}

// TestDeriveManagedArtifactRef covers the relative, absolute, and not-under-
// agents branches of the ref-key deriver.
func TestDeriveManagedArtifactRef(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantProject string
		wantRel     string
		wantOK      bool
	}{
		{"relative", reviewAuditRel, ".", reviewAuditRel, true},
		{"absolute", "/repo/wt/" + reviewAuditRel, filepath.FromSlash("/repo/wt"), reviewAuditRel, true},
		{"not-under-agents", "/tmp/other/audit.log.jsonl", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project, rel, ok := deriveManagedArtifactRef(tc.in)
			if project != tc.wantProject || rel != tc.wantRel || ok != tc.wantOK {
				t.Fatalf("derive(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, project, rel, ok, tc.wantProject, tc.wantRel, tc.wantOK)
			}
		})
	}
}

// TestCollectAuditLogStateRefFiles covers the log+head, log-only, missing-log,
// and head-error branches.
func TestCollectAuditLogStateRefFiles(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, auditLogFile)

	// missing log → error.
	if _, err := collectAuditLogStateRefFiles(logPath, reviewAuditRel); err == nil {
		t.Fatal("missing log must error")
	}

	// log only (no head) → one file.
	if err := os.WriteFile(logPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files, err := collectAuditLogStateRefFiles(logPath, reviewAuditRel)
	if err != nil {
		t.Fatalf("log-only collect: %v", err)
	}
	if len(files) != 1 || files[0].relPath != reviewAuditRel {
		t.Fatalf("log-only must yield one file, got %+v", files)
	}

	// log + head → two files.
	if err := os.WriteFile(logPath+auditHeadSuffix, []byte("head\n"), 0644); err != nil {
		t.Fatal(err)
	}
	files, err = collectAuditLogStateRefFiles(logPath, reviewAuditRel)
	if err != nil {
		t.Fatalf("log+head collect: %v", err)
	}
	if len(files) != 2 || files[1].relPath != reviewAuditRel+auditHeadSuffix {
		t.Fatalf("log+head must yield two files, got %+v", files)
	}

	// head unreadable (a directory) → error.
	dir2 := t.TempDir()
	logPath2 := filepath.Join(dir2, auditLogFile)
	if err := os.WriteFile(logPath2, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath2+auditHeadSuffix, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := collectAuditLogStateRefFiles(logPath2, reviewAuditRel); err == nil {
		t.Fatal("head read error must propagate")
	}
}

// TestMirrorReviewAuditLogToStateRef_GitRef proves the exported audit-log entry
// point mirrors the log AND its .head anchor to the ref under backend=git-ref.
func TestMirrorReviewAuditLogToStateRef_GitRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	logPath := writeAuditLogFixture(t, repo)

	if err := MirrorReviewAuditLogToStateRef(logPath); err != nil {
		t.Fatalf("mirror audit log: %v", err)
	}
	if !refBlobExists(t, repo, reviewAuditRel) {
		t.Error("audit log not mirrored to the ref")
	}
	if !refBlobExists(t, repo, reviewAuditRel+auditHeadSuffix) {
		t.Error("audit .head anchor not mirrored to the ref")
	}
}

// TestMirrorReviewAuditLogToStateRef_DefaultNoRef proves the hard gate for the
// audit log: default backend writes no ref.
func TestMirrorReviewAuditLogToStateRef_DefaultNoRef(t *testing.T) {
	repo := seedGitRefBackendRepo(t, "", tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	logPath := writeAuditLogFixture(t, repo)

	if err := MirrorReviewAuditLogToStateRef(logPath); err != nil {
		t.Fatalf("default audit mirror must be a no-op, got %v", err)
	}
	if head := stateRefHead(repo); head != "" {
		t.Fatalf("default backend must not mirror the audit log, got %q", head)
	}
}

// TestMirrorReviewAuditLogToStateRef_NotUnderAgents proves a path outside an
// .agents tree is a no-op.
func TestMirrorReviewAuditLogToStateRef_NotUnderAgents(t *testing.T) {
	if err := MirrorReviewAuditLogToStateRef(filepath.Join(t.TempDir(), auditLogFile)); err != nil {
		t.Fatalf("path not under .agents must be a no-op, got %v", err)
	}
}

// TestMirrorReviewAuditLogToStateRef_CollectErrorPropagates proves a read
// failure after the gate propagates under backend=git-ref.
func TestMirrorReviewAuditLogToStateRef_CollectErrorPropagates(t *testing.T) {
	repo := seedGitRefBackendRepo(t, gitRefWT, tasksYAMLWithStatus(stateRefTestPlanID, "pending"))
	// The log file does not exist, so collect fails after the gate passes.
	logPath := filepath.Join(repo, filepath.FromSlash(reviewAuditRel))
	err := MirrorReviewAuditLogToStateRef(logPath)
	if err == nil || !strings.Contains(err.Error(), "failed to mirror to "+stateRefName) {
		t.Fatalf("collect error under git-ref must propagate, got %v", err)
	}
}

// writeAuditLogFixture writes a minimal audit log + head anchor under repo's
// review state dir and returns the log path.
func writeAuditLogFixture(t *testing.T, repo string) string {
	t.Helper()
	logPath := filepath.Join(repo, filepath.FromSlash(reviewAuditRel))
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(`{"seq":1}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath+auditHeadSuffix, []byte(`{"count":1}`), 0644); err != nil {
		t.Fatal(err)
	}
	return logPath
}
