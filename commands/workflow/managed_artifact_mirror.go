package workflow

import (
	"os"
	"path/filepath"
	"strings"
)

// ── git-ref state backend: managed .agents/active artifact mirror ──
//
// The plan-state mirror (mirrorTransitionToStateRef) makes coordination status
// ref-visible; these helpers extend the SAME machinery (writeStateRefCAS,
// canonicalWriteToStateRef, mirrorBestEffortOrPropagate) to the other MANAGED
// artifacts an active session writes under .agents/active — delegation
// contracts/bundles, merge-back markdown, verification-result / review-decision
// YAML, and the review audit log. Under the git-ref backend (or the additive
// write_to=state-ref mode) each is ADDITIVELY mirrored to refs/agents/state at
// its exact repo-relative working-copy path, so it is durable and visible
// across worktrees. The working copy is ALWAYS written first; the mirror never
// replaces it.

// Managed-artifact sub-dir names under .agents/active. They match the segments
// the on-disk dir helpers (delegationDir, mergeBackDir, delegationBundlesDir)
// join, so a ref key built here mirrors the working-copy path exactly.
const (
	managedActiveDir        = "active"
	delegationSubDir        = "delegation"
	delegationBundlesSubDir = "delegation-bundles"
	mergeBackSubDir         = "merge-back"
	verificationSubDir      = "verification"
)

// auditHeadSuffix mirrors internal/review/audit's head-anchor sidecar suffix.
// The audit log and its ".head" attestation mirror together (one CAS commit)
// so the ref snapshot stays a faithful, verifiable copy.
const auditHeadSuffix = ".head"

// managedActiveRel joins segments into a repo-relative (slash-separated) ref
// key under ".agents/active" — the shared root of every managed session
// artifact.
func managedActiveRel(segments ...string) string {
	return strings.Join(append([]string{delegationAgentsDir, managedActiveDir}, segments...), "/")
}

// delegationContractRel is the repo-relative (slash) ref key for a delegation
// contract (.agents/active/delegation/<parentTaskID>.yaml).
func delegationContractRel(parentTaskID string) string {
	return managedActiveRel(delegationSubDir, parentTaskID+".yaml")
}

// delegationBundleRel is the repo-relative (slash) ref key for a delegation
// bundle (.agents/active/delegation-bundles/<delegationID>.yaml).
func delegationBundleRel(delegationID string) string {
	return managedActiveRel(delegationBundlesSubDir, delegationID+".yaml")
}

// mergeBackRel is the repo-relative (slash) ref key for a merge-back summary
// (.agents/active/merge-back/<taskID>.md).
func mergeBackRel(taskID string) string {
	return managedActiveRel(mergeBackSubDir, taskID+".md")
}

// verificationResultRel is the repo-relative (slash) ref key for a
// verification-result document
// (.agents/active/verification/<taskID>/<verifierType>.result.yaml).
func verificationResultRel(taskID, verifierType string) string {
	return managedActiveRel(verificationSubDir, taskID, verifierType+".result.yaml")
}

// mirrorArtifactToStateRef additively mirrors relPath's just-written content to
// refs/agents/state, but ONLY when the coordination-state backend mirrors
// canonical writes (canonicalWriteToStateRef — write_to=state-ref OR
// backend=git-ref). Under the default/local backend it is a no-op, so those
// writers stay byte-for-byte unchanged. It overlays a SINGLE blob on the
// current ref tree, so unrelated ref paths (the co-located session's
// coordination/* and plan blobs) are preserved, and writeStateRefCAS's
// tree-equality guard makes re-mirroring identical content a no-op. Failure is
// mode-aware (mirrorBestEffortOrPropagate): propagated under backend=git-ref
// (the ref IS the read source), warned under the additive write_to=state-ref
// mode.
func mirrorArtifactToStateRef(projectPath, relPath string, content []byte) error {
	return mirrorArtifactFilesToStateRef(projectPath, []stateRefFile{{relPath: relPath, content: content}})
}

// mirrorArtifactFilesToStateRef mirrors one or more managed-artifact blobs in a
// SINGLE compare-and-swap commit — the review audit log mirrors its log file
// and .head anchor together so the ref never records a torn chain. It shares
// mirrorArtifactToStateRef's gate, idempotency, unrelated-path preservation,
// and mode-aware failure policy.
func mirrorArtifactFilesToStateRef(projectPath string, files []stateRefFile) error {
	if !canonicalWriteToStateRef(projectPath) {
		return nil
	}
	what := "managed artifact"
	if len(files) > 0 {
		what = files[0].relPath
	}
	return mirrorBestEffortOrPropagate(projectPath, what+" saved",
		writeStateRefCAS(projectPath, files))
}

// MirrorReviewAuditLogToStateRef additively mirrors the review audit log at
// logPath and its ".head" anchor sidecar to refs/agents/state in one CAS
// commit. internal/review/audit is a leaf package that cannot reach this mirror
// machinery, so its live CLI writer (commands.stdReviewAdminDeps.AuditAppend)
// routes here after each append. The project root and the repo-relative ref key
// are derived from the ".agents/" boundary in logPath, gated on
// canonicalWriteToStateRef (a no-op under default/local), and CAS-preserving of
// unrelated ref paths. A missing .head (never anchored) mirrors just the log; a
// path not under an .agents tree is a no-op. Failure is mode-aware (propagate
// under backend=git-ref, warn under write_to=state-ref).
func MirrorReviewAuditLogToStateRef(logPath string) error {
	projectPath, relPath, ok := deriveManagedArtifactRef(logPath)
	if !ok || !canonicalWriteToStateRef(projectPath) {
		return nil
	}
	files, err := collectAuditLogStateRefFiles(logPath, relPath)
	if err != nil {
		return mirrorBestEffortOrPropagate(projectPath, relPath+" saved", err)
	}
	return mirrorArtifactFilesToStateRef(projectPath, files)
}

// deriveManagedArtifactRef splits an on-disk managed-artifact path into its
// project root and repo-relative (slash-separated) ref key at the ".agents/"
// boundary. A path already rooted at ".agents/" is relative to the current
// working directory ("."); an absolute path splits at its last "/.agents/"
// segment. ok is false when the path is not under an .agents tree.
func deriveManagedArtifactRef(logPath string) (projectPath, relPath string, ok bool) {
	slash := filepath.ToSlash(logPath)
	prefix := delegationAgentsDir + "/"
	if strings.HasPrefix(slash, prefix) {
		return ".", slash, true
	}
	if i := strings.LastIndex(slash, "/"+prefix); i >= 0 {
		return filepath.FromSlash(slash[:i]), slash[i+1:], true
	}
	return "", "", false
}

// collectAuditLogStateRefFiles reads the just-appended audit log and its .head
// anchor back from disk and pairs each with its repo-relative ref key. The log
// must exist (Append just wrote it); a missing .head (never anchored) is
// skipped rather than treated as an error.
func collectAuditLogStateRefFiles(logPath, relLogPath string) ([]stateRefFile, error) {
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}
	files := []stateRefFile{{relPath: relLogPath, content: logContent}}
	headContent, headErr := os.ReadFile(logPath + auditHeadSuffix)
	switch {
	case headErr == nil:
		files = append(files, stateRefFile{relPath: relLogPath + auditHeadSuffix, content: headContent})
	case !os.IsNotExist(headErr):
		return nil, headErr
	}
	return files, nil
}
