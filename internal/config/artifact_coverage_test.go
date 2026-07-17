package config

// Coverage-directed tests for the tier-2 artifact path: materialize.go,
// bundle_safety.go, fetcher_git_artifact.go, and fetcher_local_artifact.go.
// These exercise the error/edge branches (corrupt/tampered CAS, quarantine,
// non-directory store paths, gzip/tar edge cases, git subtree hashing over
// missing/oversized/desynced blobs, quota-file writes, and local-source path
// errors) that the behaviour-driven tests in the sibling *_test.go files do
// not reach. Helper names are prefixed artCov to avoid collisions with the
// package's existing test helpers.

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-billy/v6/osfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

var artCovBoom = errors.New("artCov: emit boom")

// ============================ materialize.go ============================

func TestArtCovArtifactsRootAndSegmentValidation(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if got, want := ArtifactsRoot(home), filepath.Join(home, "cache", "artifacts"); got != want {
		t.Fatalf("ArtifactsRoot = %q, want %q", got, want)
	}
	// Drive-letter, NUL, and absolute-path segments are each rejected. (The
	// absolute-path branch only fires on Windows, where a rooted path has no
	// separator caught earlier; the call still documents the contract.)
	for _, bad := range []string{"C:", "a\x00b", `\\host`} {
		if err := ValidateStoreSegment(bad); err == nil {
			t.Fatalf("ValidateStoreSegment(%q) = nil, want error", bad)
		}
	}
	if err := ValidateStoreSegment("skills"); err != nil {
		t.Fatalf("ValidateStoreSegment(skills) = %v, want nil", err)
	}
}

func TestArtCovAssertUnderCASRoot(t *testing.T) {
	t.Parallel()
	// A relative home makes the derived root relative; an absolute candidate
	// then cannot be made relative to it -> filepath.Rel errors.
	if err := assertUnderCASRoot("relative-home", "skills", "/absolute/candidate"); err == nil {
		t.Fatal("expected a Rel error for an absolute candidate under a relative root")
	}
	// A candidate two segments below the CAS root escapes the single-segment rule.
	home := t.TempDir()
	nested := filepath.Join(ArtifactStoreRoot(home, "skills"), "a", "b")
	if err := assertUnderCASRoot(home, "skills", nested); err == nil {
		t.Fatal("expected an escape error for a candidate nested below the CAS root")
	}
}

// TestArtCovMaterializeStatErrorOnNonDirParent covers verifyOrQuarantineExisting's
// stat-error return (and MaterializeToStore surfacing it) when a store-path
// ancestor is a regular file, so os.Stat fails with ENOTDIR (not IsNotExist).
func TestArtCovMaterializeStatErrorOnNonDirParent(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "cache", "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Make the family root a FILE: storePath = .../artifacts/skills/<digest>
	// then stats through a non-directory.
	if err := os.WriteFile(filepath.Join(home, "cache", "artifacts", "skills"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
	if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
		t.Fatal("expected a stat error when a store-path ancestor is a regular file")
	}
}

// TestArtCovVerifyOrQuarantineQuarantineFailure covers the quarantine-rename
// failure branch: a non-directory store entry in a read-only parent cannot be
// renamed aside, so the function surfaces the failure (fail-closed).
func TestArtCovVerifyOrQuarantineQuarantineFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only-parent rename failure is not enforced for root")
	}
	parent := filepath.Join(t.TempDir(), "p")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(parent, "entry")
	if err := os.WriteFile(entry, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if _, err := verifyOrQuarantineExisting(entry, "sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected a quarantine-rename failure to be surfaced")
	}
}

// TestArtCovWriteBundleTreeErrors covers writeBundleTree's mkdir failures (and
// MaterializeToStore surfacing a staging error) when a bundle file collides
// with a directory path: "a" is a file, so writing "a/b" must fail.
func TestArtCovWriteBundleTreeErrors(t *testing.T) {
	t.Parallel()
	t.Run("file-parent-of-file", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		bundle := artCovRawBundle(t,
			RawBundleEntry{Path: "a", Kind: rawKindFile, Mode: 0o644, Size: 1, Data: []byte("x")},
			RawBundleEntry{Path: "a/b", Kind: rawKindFile, Mode: 0o644, Size: 1, Data: []byte("y")},
		)
		if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
			t.Fatal("expected a stage error when a file path is also used as a directory")
		}
	})
	t.Run("file-parent-of-dir", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		bundle := artCovRawBundle(t,
			RawBundleEntry{Path: "a", Kind: rawKindFile, Mode: 0o644, Size: 1, Data: []byte("x")},
			RawBundleEntry{Path: "a/b", Kind: rawKindDir, Mode: 0o755},
		)
		if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
			t.Fatal("expected a stage error when a dir entry nests under a file entry")
		}
	})
}

// TestArtCovPublishStagedEntry drives publishStagedEntry directly for the two
// rename-failure branches: a concurrent store entry that fails verification is
// a hard error, and any other rename failure is surfaced.
func TestArtCovPublishStagedEntry(t *testing.T) {
	t.Parallel()
	t.Run("concurrent-entry-fails-verification", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		storePath := filepath.Join(home, "store")
		if err := os.MkdirAll(storePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(storePath, "f"), []byte("wrong"), 0o644); err != nil {
			t.Fatal(err)
		}
		staging := filepath.Join(home, "staging")
		if err := os.MkdirAll(staging, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, "f"), []byte("right"), 0o644); err != nil {
			t.Fatal(err)
		}
		hit, err := publishStagedEntry(staging, storePath, "sha256:"+strings.Repeat("0", 64))
		if err == nil {
			t.Fatal("expected a hard error when a concurrent entry fails verification")
		}
		if hit {
			t.Fatal("expected hit=false on a verification failure")
		}
	})
	t.Run("rename-failure-no-existing-entry", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		target := filepath.Join(home, "missing-parent", "target")
		hit, err := publishStagedEntry(filepath.Join(home, "no-such-staging"), target, "sha256:x")
		if err == nil {
			t.Fatal("expected a publish error when staging rename fails and no entry exists")
		}
		if hit {
			t.Fatal("expected hit=false on a plain rename failure")
		}
	})
}

// TestArtCovStoreContentDigestWalkError covers the exported StoreContentDigest
// and its walk-error path against a directory that does not exist.
func TestArtCovStoreContentDigestWalkError(t *testing.T) {
	t.Parallel()
	if _, err := StoreContentDigest(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected a walk error for a nonexistent store dir")
	}
	// The happy path over a real tree keeps the exported wrapper honest.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := StoreContentDigest(dir); err != nil {
		t.Fatalf("StoreContentDigest over a real tree: %v", err)
	}
}

// TestArtCovVerifyStoreContentDigestWalkError covers VerifyStoreContentDigest's
// present=true, matches=false path when the on-disk walk itself errors (a
// non-regular entry was injected into the store dir).
func TestArtCovVerifyStoreContentDigestWalkError(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	bundle := testBundle(t, map[string]string{"SKILL.md": "authentic\n"})
	storePath, digest, _, err := MaterializeToStore(home, "skills", bundle)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(storePath, "evil")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	present, matches := VerifyStoreContentDigest(home, "skills", digest, BundleContentDigest(bundle))
	if !present || matches {
		t.Fatalf("expected (present=true, matches=false) when the store walk errors, got (%v, %v)", present, matches)
	}
}

func TestArtCovDirFilePermDefaults(t *testing.T) {
	t.Parallel()
	if got := dirPerm(fs.FileMode(0)); got != 0o755 {
		t.Fatalf("dirPerm(0) = %o, want 0755", got)
	}
	if got := filePerm(fs.FileMode(0)); got != 0o644 {
		t.Fatalf("filePerm(0) = %o, want 0644", got)
	}
}

// TestArtCovLiveArtifactDigestsUnboundProject covers the unbound-project skip:
// a registered-but-unbound project has no local lock, so it contributes
// nothing without erroring.
func TestArtCovLiveArtifactDigestsUnboundProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Projects["unbound-proj"] = Project{}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}
	live, err := LiveArtifactDigests()
	if err != nil {
		t.Fatalf("LiveArtifactDigests with an unbound project: %v", err)
	}
	if len(live) != 0 {
		t.Fatalf("expected an empty live set from a single unbound project, got %v", live)
	}
}

// TestArtCovGCReadDirAndNonDirEntry covers GCOrphanedArtifactStore's read error
// (store root is a regular file) and its non-directory-entry skip.
func TestArtCovGCReadDirAndNonDirEntry(t *testing.T) {
	t.Parallel()
	t.Run("read-error-root-is-file", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		root := ArtifactStoreRoot(home, "skills")
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{}); err == nil {
			t.Fatal("expected a read error when the store root is a regular file")
		}
	})
	t.Run("non-directory-entry-skipped", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		root := ArtifactStoreRoot(home, "skills")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "stray-file"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		removed, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{})
		if err != nil {
			t.Fatalf("GC: %v", err)
		}
		if len(removed) != 0 {
			t.Fatalf("expected nothing removed (only a stray file present), got %v", removed)
		}
		if _, err := os.Stat(filepath.Join(root, "stray-file")); err != nil {
			t.Fatalf("expected the stray file to survive GC, got %v", err)
		}
	})
}

// artCovRawBundle assembles a Bundle from explicit raw entries (bypassing
// testBundle's file-only helper) so a test can craft path collisions.
func artCovRawBundle(t *testing.T, entries ...RawBundleEntry) Bundle {
	t.Helper()
	b, err := NormalizeBundle(func(emit func(RawBundleEntry) error) error {
		for _, e := range entries {
			if err := emit(e); err != nil {
				return err
			}
		}
		return nil
	}, BundleLimits{})
	if err != nil {
		t.Fatalf("build raw bundle: %v", err)
	}
	return b
}

// ============================ bundle_safety.go ============================

func TestArtCovCheckEntryKindAndTarType(t *testing.T) {
	t.Parallel()
	if err := checkEntryKind(RawBundleEntry{Path: "x", Kind: rawKindOther}); err == nil {
		t.Fatal("expected checkEntryKind to reject an unknown entry kind")
	}
	for _, tf := range []byte{tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
		if got := rawKindForTarType(tf); got != rawKindDevice {
			t.Fatalf("rawKindForTarType(%d) = %d, want rawKindDevice", tf, got)
		}
	}
	// An unrecognized typeflag classifies as the catch-all "other" kind.
	if got := rawKindForTarType(tar.TypeXGlobalHeader); got != rawKindOther {
		t.Fatalf("rawKindForTarType(unknown) = %d, want rawKindOther", got)
	}
}

// TestArtCovMaterializePermissionErrors covers the write-failure branches that
// only fire when a directory refuses a write: MkdirAll of the store root, a
// WriteFile into a read-only staged dir, storeContentDigest's ReadFile of an
// unreadable store file, and GC's RemoveAll of an orphan under a read-only
// root. These are skipped for root, which bypasses DAC permission checks.
func TestArtCovMaterializePermissionErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-denied branches are not enforced for root")
	}

	t.Run("mkdir-store-root", func(t *testing.T) {
		home := t.TempDir()
		artifacts := filepath.Join(home, "cache", "artifacts")
		if err := os.MkdirAll(artifacts, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(artifacts, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(artifacts, 0o755) })
		bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
		if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
			t.Fatal("expected MkdirAll of the store root to fail under a read-only parent")
		}
	})

	t.Run("writefile-into-readonly-staged-dir", func(t *testing.T) {
		home := t.TempDir()
		// A dir entry with no write bit, then a file nested under it: staging the
		// file's WriteFile is refused by the read-only staged directory.
		bundle := artCovRawBundle(t,
			RawBundleEntry{Path: "d", Kind: rawKindDir, Mode: 0o500},
			RawBundleEntry{Path: "d/f", Kind: rawKindFile, Mode: 0o644, Size: 1, Data: []byte("x")},
		)
		if _, _, _, err := MaterializeToStore(home, "skills", bundle); err == nil {
			t.Fatal("expected WriteFile into a read-only staged dir to fail")
		}
	})

	t.Run("storecontentdigest-unreadable-file", func(t *testing.T) {
		home := t.TempDir()
		bundle := testBundle(t, map[string]string{"SKILL.md": "x\n"})
		storePath, _, _, err := MaterializeToStore(home, "skills", bundle)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		unreadable := filepath.Join(storePath, "SKILL.md")
		if err := os.Chmod(unreadable, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })
		if _, err := StoreContentDigest(storePath); err == nil {
			t.Fatal("expected a ReadFile error walking an unreadable store file")
		}
	})

	t.Run("gc-removeall-under-readonly-root", func(t *testing.T) {
		home := t.TempDir()
		bundle := testBundle(t, map[string]string{"SKILL.md": "orphan\n"})
		if _, _, _, err := MaterializeToStore(home, "skills", bundle); err != nil {
			t.Fatalf("materialize orphan: %v", err)
		}
		root := ArtifactStoreRoot(home, "skills")
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		if _, err := GCOrphanedArtifactStore(home, "skills", map[string]bool{}); err == nil {
			t.Fatal("expected RemoveAll of an orphan under a read-only root to fail")
		}
	})
}

// TestArtCovLocalTreeRequiredPostureFails covers fetchTreeBundle's signing-
// posture failure branch for a local tree-layout pull.
func TestArtCovLocalTreeRequiredPostureFails(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &localArtifactFetcher{}
	src := Source{Type: "local", Path: srcDir, Auth: json.RawMessage(`{"signing":"required"}`)}
	if _, err := f.FetchArtifact(src, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"}); err == nil {
		t.Fatal("required posture must fail an unsigned local tree-layout pull")
	}
}

func TestArtCovAddFileCapsErrors(t *testing.T) {
	t.Parallel()
	t.Run("negative-size", func(t *testing.T) {
		t.Parallel()
		_, err := NormalizeBundle(staticWalker([]RawBundleEntry{
			{Path: "a", Kind: rawKindFile, Size: -1},
		}), BundleLimits{})
		if err == nil {
			t.Fatal("expected rejection of a negative declared size")
		}
	})
	t.Run("over-per-file-cap", func(t *testing.T) {
		t.Parallel()
		big := make([]byte, 100)
		_, err := NormalizeBundle(staticWalker([]RawBundleEntry{
			{Path: "a", Kind: rawKindFile, Size: 100, Data: big},
		}), BundleLimits{MaxFileBytes: 10})
		if err == nil || !strings.Contains(err.Error(), "per-file cap") {
			t.Fatalf("expected a per-file-cap rejection, got %v", err)
		}
	})
}

// TestArtCovReadTarFile drives readTarFile directly with forged headers so the
// negative-size, read-error, over-cap, and size-mismatch guards each fire —
// invariants a well-formed archive cannot itself violate.
func TestArtCovReadTarFile(t *testing.T) {
	t.Parallel()
	limits := DefaultBundleLimits()

	t.Run("negative-declared-size", func(t *testing.T) {
		t.Parallel()
		tr := tar.NewReader(bytes.NewReader(nil))
		if _, err := readTarFile(tr, &tar.Header{Name: "f", Size: -1}, limits); err == nil {
			t.Fatal("expected rejection of a negative declared size")
		}
	})
	t.Run("truncated-read-error", func(t *testing.T) {
		t.Parallel()
		full := artCovRawTar(t, "f", make([]byte, 20))
		truncated := full[:512+10] // header block + partial content
		tr := tar.NewReader(bytes.NewReader(truncated))
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("tar Next: %v", err)
		}
		if _, err := readTarFile(tr, hdr, limits); err == nil {
			t.Fatal("expected a read error on truncated content")
		}
	})
	t.Run("content-exceeds-cap", func(t *testing.T) {
		t.Parallel()
		tr := artCovTarReaderAtContent(t, "f", []byte("hello"))
		// Forge a declared size within the cap while the real content overruns it.
		if _, err := readTarFile(tr, &tar.Header{Name: "f", Size: 4}, BundleLimits{MaxFileBytes: 4}); err == nil {
			t.Fatal("expected a content-over-cap rejection")
		}
	})
	t.Run("declared-size-mismatch", func(t *testing.T) {
		t.Parallel()
		tr := artCovTarReaderAtContent(t, "f", []byte("hello"))
		if _, err := readTarFile(tr, &tar.Header{Name: "f", Size: 3}, BundleLimits{MaxFileBytes: 1 << 20}); err == nil {
			t.Fatal("expected a declared-size-mismatch rejection")
		}
	})
}

// artCovRawTar builds a single-file uncompressed tar archive (no gzip) so a
// test can slice its bytes to simulate truncation.
func artCovRawTar(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	return buf.Bytes()
}

// artCovTarReaderAtContent returns a tar.Reader advanced past the header of a
// single-file archive so the next read yields body.
func artCovTarReaderAtContent(t *testing.T, name string, body []byte) *tar.Reader {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(artCovRawTar(t, name, body)))
	if _, err := tr.Next(); err != nil {
		t.Fatalf("tar Next: %v", err)
	}
	return tr
}

// ============================ fetcher_git_artifact.go ============================

func TestArtCovGitFetchArtifactBadSubpath(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		t.Fatal("clone must not run for an invalid artifact subpath")
		return nil, nil, nil
	}}
	_, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r"}, PackageRefParts{SourceID: "s", ArtifactPath: "../escape", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("want schema error for a traversal artifact path, got %v", err)
	}
}

func TestArtCovCommittedSubtreeHashAt(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	rootHash := buildCommittedTree(t, st, map[string][]byte{
		"dir/a.txt": []byte("a"),
		"file.txt":  []byte("f"),
	}, nil)
	root, err := object.GetTree(st, rootHash)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	// Repo root ("." / "") resolves to the root tree's own hash.
	if h, err := committedSubtreeHashAt(root, "."); err != nil || h != root.Hash {
		t.Fatalf("committedSubtreeHashAt(.) = (%v, %v), want (%v, nil)", h, err, root.Hash)
	}
	// A path absent from the committed tree fails closed.
	if _, err := committedSubtreeHashAt(root, "nope"); err == nil {
		t.Fatal("expected a not-in-committed-tree error")
	}
	// A path that is a file (not a tree) fails closed.
	if _, err := committedSubtreeHashAt(root, "file.txt"); err == nil {
		t.Fatal("expected a not-a-tree error for a file path")
	}
	// A real subtree resolves.
	if _, err := committedSubtreeHashAt(root, "dir"); err != nil {
		t.Fatalf("committedSubtreeHashAt(dir): %v", err)
	}
}

func TestArtCovGitTreeWalkSizingAndDecodeErrors(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	limits := DefaultBundleLimits()
	// A hash absent from the store fails the pre-decode size gate.
	bogus := plumbing.NewHash("4444444444444444444444444444444444444444")
	if _, err := NormalizeBundle(gitCommittedTreeWalker(st, bogus, limits), limits); err == nil {
		t.Fatal("expected a sizing error for a missing tree object")
	}
	// A blob hash sizes fine but fails to decode as a tree.
	blobHash := writeFixtureBlob(t, st, []byte("not a tree"))
	if _, err := NormalizeBundle(gitCommittedTreeWalker(st, blobHash, limits), limits); err == nil {
		t.Fatal("expected a decode error when a non-tree object is walked as a tree")
	}
}

func TestArtCovGitHandleEntryDirect(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	limits := DefaultBundleLimits().orDefault()

	// A directory entry whose emit errors aborts before recursion.
	wErr := &gitTreeWalk{store: st, limits: limits, treeCap: gitTreeObjectByteCap(limits), emit: func(RawBundleEntry) error { return artCovBoom }}
	if err := wErr.handleEntry("", &object.TreeEntry{Name: "d", Mode: filemode.Dir, Hash: plumbing.ZeroHash}); !errors.Is(err, artCovBoom) {
		t.Fatalf("expected the emit error to propagate from a dir entry, got %v", err)
	}

	// An unrecognized mode is emitted as the catch-all "other" kind.
	var got RawBundleEntry
	wOK := &gitTreeWalk{store: st, limits: limits, treeCap: gitTreeObjectByteCap(limits), emit: func(e RawBundleEntry) error { got = e; return nil }}
	if err := wOK.handleEntry("", &object.TreeEntry{Name: "weird", Mode: filemode.Empty}); err != nil {
		t.Fatalf("handleEntry(other): %v", err)
	}
	if got.Kind != rawKindOther {
		t.Fatalf("expected rawKindOther for an unrecognized mode, got %v", got.Kind)
	}
}

func TestArtCovReadCommittedBlobErrors(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	limits := DefaultBundleLimits()
	// Missing object -> sizing error.
	bogus := plumbing.NewHash("5555555555555555555555555555555555555555")
	if _, err := readCommittedBlob(st, "x", bogus, limits); err == nil {
		t.Fatal("expected a sizing error for a missing blob object")
	}
	// A tree hash sizes fine but fails to resolve as a blob.
	treeHash := buildCommittedTree(t, st, map[string][]byte{"a.txt": []byte("a")}, nil)
	if _, err := readCommittedBlob(st, "x", treeHash, limits); err == nil {
		t.Fatal("expected a resolve error when a tree object is read as a blob")
	}
}

// TestArtCovReadCommittedBlobFile covers the per-file-cap gate and, where
// go-git preserves a lied object size, the content-over-cap and size-mismatch
// guards against a header/content-desynced blob.
func TestArtCovReadCommittedBlobFile(t *testing.T) {
	t.Parallel()
	st := memory.NewStorage()
	// Declared blob size over the cap is rejected before the reader is opened.
	blobHash := writeFixtureBlob(t, st, make([]byte, 100))
	blob, err := object.GetBlob(st, blobHash)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	f := object.NewFile("big", filemode.Regular, blob)
	if _, err := readCommittedBlobFile("big", f, BundleLimits{MaxFileBytes: 10}); err == nil {
		t.Fatal("expected a per-file-cap rejection from readCommittedBlobFile")
	}

	// Header/content desync: a blob whose recorded size disagrees with the
	// bytes its reader yields must fail. With a generous cap the size-mismatch
	// guard fires; with a tight cap the content-over-cap guard fires first.
	if fDesync, real := artCovDesyncedBlobFile(t, st, 5, 40); fDesync != nil {
		if _, err := readCommittedBlobFile("desync", fDesync, BundleLimits{MaxFileBytes: 1 << 20}); err == nil {
			t.Fatalf("expected a size-mismatch rejection (recorded=%d real=%d)", fDesync.Size, real)
		}
	}
	if fCap, real := artCovDesyncedBlobFile(t, st, 5, 40); fCap != nil {
		if _, err := readCommittedBlobFile("desync", fCap, BundleLimits{MaxFileBytes: 10}); err == nil {
			t.Fatalf("expected a content-over-cap rejection (recorded=%d real=%d cap=10)", fCap.Size, real)
		}
	}
}

// artCovDesyncedBlobFile stores a blob whose recorded object size is set to
// recordedSize while realSize bytes of content are written, then returns an
// *object.File over it. Returns (nil, 0) if go-git normalizes the size away
// (making the desync guard unreachable on this build).
func artCovDesyncedBlobFile(t *testing.T, st *memory.Storage, recordedSize, realSize int) (*object.File, int) {
	t.Helper()
	obj := st.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, realSize)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	obj.SetSize(int64(recordedSize)) // lie AFTER the writer set the real size
	h, err := st.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := object.GetBlob(st, h)
	if err != nil {
		t.Fatal(err)
	}
	if blob.Size == int64(realSize) {
		return nil, 0 // size was normalized to the content length; nothing to test
	}
	return object.NewFile("desync", filemode.Regular, blob), realSize
}

func TestArtCovReadArtifactFileOpenError(t *testing.T) {
	t.Parallel()
	_, err := readArtifactFile(memfs.New(), "missing.json", PackageRefParts{SourceID: "s", ArtifactPath: "missing.json"}, "deadbeef")
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
		t.Fatalf("want not_found open error, got %v", err)
	}
}

// TestArtCovCloneAndResolveTreeUnresolvable covers cloneAndResolve's tree-error
// branch: HEAD's commit object exists but its tree object is missing. A
// single-file pull ignores the unresolvable tree and still succeeds.
func TestArtCovCloneAndResolveTreeUnresolvable(t *testing.T) {
	withPackagesCache(t)
	f := &gitArtifactFetcher{cloner: artCovMissingTreeCloner(t)}
	got, err := f.FetchArtifact(Source{Type: "git", URL: "file:///r", Ref: "main"}, PackageRefParts{SourceID: "s", ArtifactPath: "x.json", VersionSpec: "1"})
	if err != nil {
		t.Fatalf("single-file pull over an unresolvable committed tree should succeed: %v", err)
	}
	if string(got.Data) != "hi" {
		t.Fatalf("data = %q, want %q", got.Data, "hi")
	}
}

// artCovMissingTreeCloner builds a repo whose HEAD commit object is present but
// references a tree hash that has no object in storage; the worktree memfs
// carries a single file so a single-file pull can still read it.
func artCovMissingTreeCloner(t *testing.T) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	st := memory.NewStorage()
	sig := object.Signature{Name: "t", Email: "t@example"}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "no tree", TreeHash: plumbing.NewHash("6666666666666666666666666666666666666666")}
	commitObj := st.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := st.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commitHash)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}
	wfs := memfs.New()
	memfsWriteFile(t, wfs, "x.json", []byte("hi"))
	return func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
		repo, err := gogit.Open(st, wfs)
		if err != nil {
			return nil, nil, err
		}
		return repo, wfs, nil
	}
}

// TestArtCovQuotaFilesystem covers the quota filesystem's wrapFile error
// passthrough, Chroot quota propagation, and quotaFile.WriteAt (both the
// under-budget delegate and the over-budget fail-closed).
func TestArtCovQuotaFilesystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	qfs := newQuotaFilesystem(osfs.New(dir), 10)

	// wrapFile passes a nil file + error straight through.
	if _, err := qfs.wrapFile(nil, errors.New("open boom")); err == nil {
		t.Fatal("expected wrapFile to pass an open error through")
	}

	// Chroot returns a sub-filesystem sharing the same quota.
	if _, err := qfs.Chroot("sub"); err != nil {
		t.Fatalf("Chroot: %v", err)
	}

	// WriteAt reserves against the shared budget before delegating.
	fh, err := qfs.Create("x")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = fh.Close() }()
	if _, err := fh.WriteAt([]byte("hello"), 0); err != nil {
		t.Fatalf("first WriteAt within budget: %v", err)
	}
	if _, err := fh.WriteAt([]byte("worldworld"), 5); err == nil {
		t.Fatal("expected the over-budget WriteAt to fail closed")
	}
}

// ============================ fetcher_local_artifact.go ============================

func TestArtCovOpenLocalArtifactRootErrors(t *testing.T) {
	t.Parallel()
	f := &localArtifactFetcher{}
	t.Run("nonexistent-base", func(t *testing.T) {
		t.Parallel()
		_, err := f.FetchArtifact(Source{Type: "local", Path: filepath.Join(t.TempDir(), "no-such-root")}, PackageRefParts{SourceID: "s", ArtifactPath: "x", VersionSpec: "1"})
		var ie *ImportError
		if !errors.As(err, &ie) || ie.Reason != ReasonNotFound {
			t.Fatalf("want not_found for a missing source root, got %v", err)
		}
	})
	t.Run("base-is-a-file", func(t *testing.T) {
		t.Parallel()
		fileBase := filepath.Join(t.TempDir(), "afile")
		if err := os.WriteFile(fileBase, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := f.FetchArtifact(Source{Type: "local", Path: fileBase}, PackageRefParts{SourceID: "s", ArtifactPath: "x", VersionSpec: "1"})
		var ie *ImportError
		if !errors.As(err, &ie) || ie.Reason != ReasonContent {
			t.Fatalf("want content error when the source root is a regular file, got %v", err)
		}
	})
}

// TestArtCovLocalTreeNormalizeError covers fetchTreeBundle's normalize-error
// wrap when a subtree contains a symlink (rejected by H1).
func TestArtCovLocalTreeNormalizeError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	skillDir := filepath.Join(srcDir, "skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(skillDir, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	f := &localArtifactFetcher{}
	_, err := f.FetchArtifact(Source{Type: "local", Path: srcDir}, PackageRefParts{SourceID: "s", ArtifactPath: "skill", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting a symlink in the subtree, got %v", err)
	}
}

// TestArtCovLocalWalkHandleEntry covers localWalk.handleEntry's Lstat-error
// branch (a phantom dir entry) and its dir-emit-error branch.
func TestArtCovLocalWalkHandleEntry(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	limits := DefaultBundleLimits().orDefault()

	// A directory entry whose name is not on disk -> Lstat error.
	wLstat := &localWalk{root: root, artifactRel: ".", limits: limits, emit: func(RawBundleEntry) error { return nil }}
	if err := wLstat.handleEntry(".", artCovGhostDirEntry{}); err == nil {
		t.Fatal("expected an Lstat error for a phantom directory entry")
	}

	// A real directory entry whose emit errors propagates that error.
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var dirEntry os.DirEntry
	for _, e := range entries {
		if e.Name() == "d" {
			dirEntry = e
		}
	}
	if dirEntry == nil {
		t.Fatal("fixture directory entry not found")
	}
	wEmit := &localWalk{root: root, artifactRel: ".", limits: limits, emit: func(RawBundleEntry) error { return artCovBoom }}
	if err := wEmit.handleEntry(".", dirEntry); !errors.Is(err, artCovBoom) {
		t.Fatalf("expected the dir emit error to propagate, got %v", err)
	}
}

// TestArtCovLocalTreeRejectsSocket covers handleEntry's "other" kind: a unix
// socket file is neither a regular file, dir, nor symlink, so it is emitted as
// rawKindOther and rejected by the accumulator.
func TestArtCovLocalTreeRejectsSocket(t *testing.T) {
	t.Parallel()
	base, err := os.MkdirTemp("", "artcov")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(base) }()
	skillDir := filepath.Join(base, "s")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(skillDir, "sock"))
	if err != nil {
		t.Skipf("unix socket unsupported here: %v", err)
	}
	defer func() { _ = l.Close() }()

	f := &localArtifactFetcher{}
	_, err = f.FetchArtifact(Source{Type: "local", Path: base}, PackageRefParts{SourceID: "s", ArtifactPath: "s", VersionSpec: "1"})
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonContent {
		t.Fatalf("want content error rejecting a non-regular (socket) entry, got %v", err)
	}
}

func TestArtCovOpenConfinedDirErrors(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()

	// Open of a missing path fails.
	if _, err := openConfinedDir(root, "no-such-dir", nil); err == nil {
		t.Fatal("expected an open error for a missing directory")
	}
	// Opening a regular file where a directory was expected fails the IsDir check.
	info, err := os.Lstat(filepath.Join(base, "f"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openConfinedDir(root, "f", info); err == nil {
		t.Fatal("expected a not-a-directory error when opening a regular file as a dir")
	}
}

func TestArtCovReadRootFileNotRegular(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = root.Close() }()
	info, err := os.Lstat(filepath.Join(base, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readRootFile(root, "sub", info, DefaultBundleLimits().orDefault()); err == nil {
		t.Fatal("expected a not-a-regular-file error when reading a directory as a file")
	}
}

// artCovGhostDirEntry is a fake os.DirEntry naming a path that does not exist,
// used to drive the Lstat-error branch of localWalk.handleEntry.
type artCovGhostDirEntry struct{}

func (artCovGhostDirEntry) Name() string               { return "ghost-missing-entry" }
func (artCovGhostDirEntry) IsDir() bool                { return false }
func (artCovGhostDirEntry) Type() fs.FileMode          { return 0 }
func (artCovGhostDirEntry) Info() (fs.FileInfo, error) { return nil, os.ErrNotExist }
