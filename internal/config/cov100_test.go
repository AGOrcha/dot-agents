package config

// Coverage-directed tests that close the last per-file gaps to 100% line
// coverage on bundle_safety.go, materialize.go, and local_source.go for the
// package-artifact-install 100%-ratchet CI gate. They reach fail-closed
// "cannot happen" guards two ways: real fault injection (read-only dirs,
// directory-at-target, malformed config) where the leg is deterministically
// triggerable, and a minimal production seam override where the derived input
// can never itself violate the guard (a BundleDigest, a re-derived store path,
// a WalkDir child path, an agentslock-mapped missing lock).
//
// Every seam-overriding test is DELIBERATELY non-parallel: Go resumes paused
// t.Parallel tests only after all sequential tests finish, so a global-seam
// mutation (always restored via defer) is confined to the sequential phase and
// never races the parallel readers of that seam. Helpers use the mat100 prefix.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mat100SkipWithoutPosixPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission-based fault injection is POSIX-specific; the branch runs on unix legs")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission-denied branches are not enforced for root")
	}
}

// ============================ bundle_safety.go ============================

// TestMat100CanonicalBundlePathAbsoluteAfterClean covers the post-clean
// absolute guard: path.Clean never yields an absolute result from a relative
// input, so the guard is only reachable by overriding the cleaner.
func TestMat100CanonicalBundlePathAbsoluteAfterClean(t *testing.T) {
	orig := pathCleanFn
	pathCleanFn = func(string) string { return "/abs-after-clean" }
	defer func() { pathCleanFn = orig }()
	if _, err := canonicalBundlePath("x"); err == nil {
		t.Fatal("expected rejection when the cleaner yields an absolute path")
	}
}

// TestMat100CanonicalBundlePathBadSegmentAfterClean covers the post-clean
// per-segment guard (an empty/"."/".." segment), unreachable from a real clean.
func TestMat100CanonicalBundlePathBadSegmentAfterClean(t *testing.T) {
	orig := pathCleanFn
	pathCleanFn = func(string) string { return "a/../b" }
	defer func() { pathCleanFn = orig }()
	if _, err := canonicalBundlePath("x"); err == nil {
		t.Fatal("expected rejection when a cleaned segment is a traversal component")
	}
}

// TestMat100ValidateArtifactSubpathBadSegmentAfterClean covers the post-clean
// ".." segment guard in validateArtifactSubpath.
func TestMat100ValidateArtifactSubpathBadSegmentAfterClean(t *testing.T) {
	orig := pathCleanFn
	pathCleanFn = func(string) string { return "a/../b" }
	defer func() { pathCleanFn = orig }()
	if _, err := validateArtifactSubpath("x"); err == nil {
		t.Fatal("expected rejection when a cleaned artifact segment is a traversal component")
	}
}

// ============================ materialize.go ============================

// TestMat100ValidateStoreSegmentAbsolute covers the filepath.IsAbs leg of
// ValidateStoreSegment. No separator-free string is absolute on any real OS
// (drive letters/UNC/rooted paths all carry a `:` or a separator caught by the
// earlier guards), so this fail-closed leg is exercised via the seam here; on
// the Windows CI leg the same guard also stands for a genuine rooted segment.
func TestMat100ValidateStoreSegmentAbsolute(t *testing.T) {
	orig := filepathIsAbsFn
	filepathIsAbsFn = func(string) bool { return true }
	defer func() { filepathIsAbsFn = orig }()
	if err := ValidateStoreSegment("skills"); err == nil {
		t.Fatal("expected an absolute-segment rejection")
	}
}

// TestMat100MaterializeUnexpectedDigestShape covers MaterializeToStore's
// digest-shape guard: BundleDigest always emits a canonical sha256 digest, so
// the guard is only reachable by overriding the digest source.
func TestMat100MaterializeUnexpectedDigestShape(t *testing.T) {
	orig := bundleDigestFn
	bundleDigestFn = func(Bundle) string { return "sha256:not-hex" }
	defer func() { bundleDigestFn = orig }()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatal("expected a rejection for a non-canonical bundle digest shape")
	}
}

// TestMat100MaterializeContainmentGuard covers MaterializeToStore's
// assertUnderCASRoot call-site guard: the store path is re-derived from a
// validated segment and always resolves under the CAS root, so the guard is
// only reachable by overriding the containment check.
func TestMat100MaterializeContainmentGuard(t *testing.T) {
	orig := assertUnderCASRootFn
	assertUnderCASRootFn = func(string, string, string) error { return errors.New("mat100 boom") }
	defer func() { assertUnderCASRootFn = orig }()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatal("expected the CAS-root containment guard to fail closed")
	}
}

// TestMat100GCContainmentGuard covers GCOrphanedArtifactStore's symmetric
// pre-delete assertUnderCASRoot guard: a real orphan is materialized first
// (guard passing), then the seam is flipped so the delete-time re-assert fails.
func TestMat100GCContainmentGuard(t *testing.T) {
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "orphan\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err != nil {
		t.Fatalf("materialize orphan: %v", err)
	}
	orig := assertUnderCASRootFn
	assertUnderCASRootFn = func(string, string, string) error { return errors.New("mat100 boom") }
	defer func() { assertUnderCASRootFn = orig }()
	if _, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{}); err == nil {
		t.Fatal("expected GC's pre-delete containment guard to fail closed")
	}
}

// TestMat100MaterializeMkdirTempFails covers the staging-dir creation failure:
// the store root pre-exists read-only, so MkdirAll(root) is a no-op but the
// MkdirTemp inside it is refused. (Real fault injection; unix-only.)
func TestMat100MaterializeMkdirTempFails(t *testing.T) {
	t.Parallel()
	mat100SkipWithoutPosixPerms(t)
	home := t.TempDir()
	root := ArtifactStoreRoot(home, "skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatal("expected MkdirTemp of a staging dir under a read-only store root to fail")
	}
}

// TestMat100MaterializePublishError covers MaterializeToStore surfacing a
// publishStagedEntry error. verifyOrQuarantineExisting clears the target before
// the publish, so a real rename-time failure through the top-level entrypoint
// is non-deterministic — the seam forces the error leg deterministically.
func TestMat100MaterializePublishError(t *testing.T) {
	orig := publishStagedEntryFn
	publishStagedEntryFn = func(string, string, string) (bool, error) { return false, errors.New("mat100 publish boom") }
	defer func() { publishStagedEntryFn = orig }()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatal("expected the publish-stage error to be surfaced")
	}
}

// TestMat100MaterializePublishConcurrentHit covers MaterializeToStore's
// concurrent-hit no-op return (installed=false) when a same-digest materializer
// wins the publish race. The race is timing-dependent through the real path, so
// the seam reports the hit deterministically.
func TestMat100MaterializePublishConcurrentHit(t *testing.T) {
	orig := publishStagedEntryFn
	publishStagedEntryFn = func(string, string, string) (bool, error) { return true, nil }
	defer func() { publishStagedEntryFn = orig }()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	_, _, installed, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("concurrent-hit materialize: %v", err)
	}
	if installed {
		t.Fatal("expected installed=false on a concurrent publish hit")
	}
}

// TestMat100PublishStagedEntryConcurrentVerifiedHit covers publishStagedEntry's
// verified-concurrent-entry leg deterministically: the target already holds
// byte-identical content, so the rename onto the non-empty dir fails and the
// re-verify reports a trusted no-op (hit=true) — the same-digest convergence
// the concurrent stress test only reaches probabilistically.
func TestMat100PublishStagedEntryConcurrentVerifiedHit(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	storePath := filepath.Join(home, "store")
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storePath, "f"), []byte("right"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := StoreContentDigest(storePath)
	if err != nil {
		t.Fatalf("compute expected digest: %v", err)
	}
	staging := filepath.Join(home, "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "f"), []byte("right"), 0o644); err != nil {
		t.Fatal(err)
	}
	hit, err := publishStagedEntry(staging, storePath, expected)
	if err != nil || !hit {
		t.Fatalf("expected a trusted concurrent hit, got hit=%v err=%v", hit, err)
	}
}

// TestMat100StoreContentDigestRelError covers storeContentDigest's filepath.Rel
// guard: a WalkDir child is always under the walk root, so Rel never errors in
// production; the seam makes the guard reachable.
func TestMat100StoreContentDigestRelError(t *testing.T) {
	orig := storeWalkRelFn
	storeWalkRelFn = func(string, string) (string, error) { return "", errors.New("mat100 rel boom") }
	defer func() { storeWalkRelFn = orig }()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreContentDigest(dir); err == nil {
		t.Fatal("expected a relativize error to abort the store walk")
	}
}

// TestMat100LiveArtifactDigestsLoadError covers LiveArtifactDigests' registry
// load-error leg via a malformed config.json. (Real fault injection.)
func TestMat100LiveArtifactDigestsLoadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{ not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LiveArtifactDigests(); err == nil {
		t.Fatal("expected LiveArtifactDigests to surface a registry load error")
	}
}

// TestMat100LiveArtifactDigestsMissingLockSkips covers the belt-and-suspenders
// os.IsNotExist continue: agentslock.Open maps a missing lock to an empty
// error-free lockfile today, so ReadUnits never returns IsNotExist — the seam
// stands in for a future Open that propagates it. A bound project whose read
// reports IsNotExist must be skipped (contributes nothing), not fail closed.
func TestMat100LiveArtifactDigestsMissingLockSkips(t *testing.T) {
	orig := readUnitsFn
	readUnitsFn = func(string) (UnitsLock, error) { return UnitsLock{}, os.ErrNotExist }
	defer func() { readUnitsFn = orig }()
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	projPath := filepath.Join(t.TempDir(), "proj")
	if err := os.MkdirAll(projPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.AddProject("proj", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	live, err := LiveArtifactDigests()
	if err != nil {
		t.Fatalf("expected an IsNotExist read to be a clean skip, got %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("expected an empty live set, got %v", live)
	}
}

// ============================ local_source.go ============================

// TestMat100EnsureProvenanceGitignoreLockError covers the file-lock acquisition
// error: rooting the source under a regular file makes the lock sidecar's
// parent MkdirAll fail. (Real fault injection.)
func TestMat100EnsureProvenanceGitignoreLockError(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewLocalSource(filepath.Join(blocker, "sub"), &fakeGit{isRepo: true})
	if err := s.EnsureProvenanceGitignore([]string{"cache/"}); err == nil {
		t.Fatal("expected a lock-acquisition error when the source root is under a file")
	}
}

// TestMat100CASPathIgnoredEmptyRoot covers CASPathIgnored's empty-root guard.
func TestMat100CASPathIgnoredEmptyRoot(t *testing.T) {
	t.Parallel()
	s := NewLocalSource("", &fakeGit{})
	if _, err := s.CASPathIgnored("cache/x"); err == nil {
		t.Fatal("expected an empty-root error")
	}
}

// TestMat100CASPathIgnoredReadError covers CASPathIgnored's readGitignore error
// leg: a directory at the .gitignore path makes the read fail (non-NotExist).
func TestMat100CASPathIgnoredReadError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, gitignoreFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewLocalSource(root, &fakeGit{isRepo: true})
	if _, err := s.CASPathIgnored("cache/x"); err == nil {
		t.Fatal("expected a read error when .gitignore is a directory")
	}
}

// TestMat100EnsureAndVerifyCASIgnoreVerifyReadError covers the post-install
// verify read-error leg: the install step is seamed to a no-op so the on-disk
// .gitignore (a directory here) drives CASPathIgnored's read failure.
func TestMat100EnsureAndVerifyCASIgnoreVerifyReadError(t *testing.T) {
	orig := provenanceGitignoreInstallFn
	provenanceGitignoreInstallFn = func(*LocalSource, []string) error { return nil }
	defer func() { provenanceGitignoreInstallFn = orig }()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, gitignoreFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAndVerifyCASIgnore(home, "skills", "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected a verify read error when .gitignore cannot be read")
	}
}

// TestMat100EnsureAndVerifyCASIgnoreNotIgnored covers the "still not ignored
// after install" refusal: the install is seamed to a no-op and the on-disk
// .gitignore carries no covering pattern, so the CAS path verifies as tracked.
func TestMat100EnsureAndVerifyCASIgnoreNotIgnored(t *testing.T) {
	orig := provenanceGitignoreInstallFn
	provenanceGitignoreInstallFn = func(*LocalSource, []string) error { return nil }
	defer func() { provenanceGitignoreInstallFn = orig }()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, gitignoreFileName), []byte("logs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureAndVerifyCASIgnore(home, "skills", "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected a refusal when the CAS path is not gitignored after install")
	}
}

// TestMat100ManagedBlockEmpty covers managedBlock's empty-result guard: it is
// unreachable while alwaysIgnoredCAS carries the permanent "cache/" pattern, so
// that package var is emptied here to exercise the len==0 return.
func TestMat100ManagedBlockEmpty(t *testing.T) {
	orig := alwaysIgnoredCAS
	alwaysIgnoredCAS = []string{}
	defer func() { alwaysIgnoredCAS = orig }()
	if got := managedBlock(nil); got != "" {
		t.Fatalf("expected an empty managed block, got %q", got)
	}
}

// TestMat100JoinGitignoreEmpty covers joinGitignore's both-empty return.
func TestMat100JoinGitignoreEmpty(t *testing.T) {
	t.Parallel()
	if got := joinGitignore("", ""); got != "" {
		t.Fatalf("expected an empty join, got %q", got)
	}
}
