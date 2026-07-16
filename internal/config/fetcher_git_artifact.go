package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/osfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/filesystem"

	"github.com/AGOrcha/dot-agents/internal/gitremote"
)

// This file adds the git source type to the tier-2 (packages/artifacts) path.
// Per config-distribution-model §4 (relaxed) / §7A.1-2 / §15 D3+D8, any source
// type may serve any artifact kind; the kind governs merge/trust, not which
// source is permitted. The git artifact fetcher mirrors the layer-tier
// gitFetcher (in fetcher.go): it resolves the ref→commit SHA via a shallow
// in-memory clone, reads the artifact file at that SHA, and caches it in the
// shared content-addressed packages cache (~/.agents/cache/packages/<digest>/).
// It returns a FetchedArtifact so it composes with the same pass-2 (p6)
// resolution and signing-posture stub as the oci/http artifact paths.

// gitArtifactFetcher pulls a package artifact from a git source. It reuses the
// gitFetcher clone plumbing (ref→SHA via a Depth:1 single-branch in-memory
// clone) and the artifact contract (content digest addressing, signing posture,
// pinned-digest verification). It satisfies PackageFetcher so
// SelectPackageFetcher can return it for `git` package sources.
type gitArtifactFetcher struct {
	// cloner is a test seam over the go-git clone, identical in shape to
	// gitFetcher.cloner. Nil uses gitCloneShallow. It returns the cloned
	// repository (for HEAD resolution) and the worktree filesystem (for reading
	// the artifact file).
	cloner func(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error)
}

func (f *gitArtifactFetcher) clone(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
	if f.cloner != nil {
		return f.cloner(ctx, url, ref)
	}
	return gitCloneShallowQuotaBounded(ctx, url, ref)
}

// gitArtifactRef resolves the git ref to clone for an artifact pull. A
// "pinned:sha256:..." version spec is a content-digest pin (verified after the
// read), not a git ref, so it falls back to the source's declared ref (then
// "main"); any other version spec is treated as a branch/tag ref so distinct
// versions clone distinct refs.
func gitArtifactRef(src Source, parts PackageRefParts) string {
	if _, pinned := digestFromVersionSpec(parts.VersionSpec); !pinned && parts.VersionSpec != "" {
		return parts.VersionSpec
	}
	if src.Ref != "" {
		return src.Ref
	}
	return "main"
}

func (f *gitArtifactFetcher) FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error) {
	posture := PostureFromSource(src)
	pinned, isPinned := digestFromVersionSpec(parts.VersionSpec)

	// A digest-pinned artifact is content-addressed, so the shared packages cache
	// is checked before any clone (offline fast path, spec §8). This fast path
	// only ever serves a single-file (blob) pull: a tree-layout pull's digest is
	// a whole-subtree BundleDigest, never written to the flat blob cache (see
	// fetchTreeBundle), so a pinned tree-layout ref always falls through to a
	// real clone+walk below.
	if isPinned {
		if art, ok, err := readCachedPinnedGitArtifact(posture, pinned); ok {
			return art, err
		}
	}

	// Canonicalize the artifact path ONCE (rejecting ../absolute/UNC), and use
	// the single canonical form for BOTH the worktree walk and the committed-
	// tree submodule lookup below — a noncanonical path ("./skill/vendored",
	// "skill/../skill/vendored") must not resolve to one thing in the worktree
	// and fail to resolve in the committed tree, which is how a gitlink bypass
	// slips through.
	rel, err := validateArtifactSubpath(parts.ArtifactPath)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonSchema, fmt.Errorf("git artifact path: %w", err))
	}

	wfs, tree, treeErr, commit, err := f.cloneAndResolve(src, parts)
	if err != nil {
		return FetchedArtifact{}, err
	}
	// The production clone path checks out into a fresh temp dir (t1b: a
	// quota-bounded disk clone, not go-git's default in-memory storer+memfs —
	// see gitCloneShallowQuotaBounded). That temp dir must outlive every read
	// below (both branches), so cleanup is deferred here rather than inside
	// clone() itself. A test-injected memfs cloner implements no Cleanup, so
	// this type assertion is a no-op for every existing test.
	if cleaner, ok := wfs.(interface{ Cleanup() }); ok {
		defer cleaner.Cleanup()
	}
	rootOS := filepath.FromSlash(rel)

	info, err := wfs.Lstat(rootOS)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git read %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	if info.IsDir() {
		// Tree layout (spec D3): the ref names a resource directory, not a
		// single file. The gitlink check REQUIRES the committed tree — without
		// it a submodule flattened into an empty worktree dir would slip past
		// rejectGitSubmodules and restore the pinning-defeat. So a directory
		// artifact whose committed tree could not be resolved fails closed.
		if tree == nil {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: cannot verify submodules — committed tree unavailable: %w", parts.ArtifactPath, commit, treeErr))
		}
		return f.fetchTreeBundle(tree, rel, parts, commit, posture, isPinned, pinned)
	}

	data, err := readArtifactFile(wfs, rootOS, parts, commit)
	if err != nil {
		return FetchedArtifact{}, err
	}

	digest := artifactDigest(data)
	// A digest pin must match the committed content, else the artifact is not
	// what was requested (tamper / mismatch -> content failure).
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but git served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
	// The git artifact key is content-addressed by the artifact digest, with the
	// resolved commit recorded so the package resolver can derive an effective key
	// sensitive to the source commit (config-distribution-model §7A.4).
	return FetchedArtifact{Data: data, Digest: digest, CacheHit: false, Posture: posture, KeyInputs: CacheKeyInputs{OCIDigest: digest, ResolvedCommit: commit}}, nil
}

// fetchTreeBundle walks the git subtree at canonicalRel (already confirmed to
// be a directory) into a normalized Bundle (H1) and wraps it in a
// FetchedArtifact. It first inspects the committed git tree object and
// rejects any gitlink (submodule) entry: go-git flattens a `160000` submodule
// into an empty directory in the worktree, so a worktree-only walk would
// silently drop the gitlink's commit OID and let two different referenced
// commits produce the same BundleDigest — a digest-defeat. Once the subtree
// is proven gitlink-free, its content is read directly from the COMMITTED
// tree/blob objects (gitCommittedTreeWalker) rather than the checked-out
// worktree — t1b: a worktree ReadDir materializes a directory's ENTIRE
// listing in one call before MaxEntries can reject a flood, where
// object.TreeWalker resolves and streams one entry at a time, so the
// accumulator's caps bound the walk as it happens rather than after the
// whole subtree is buffered. Digest is the whole-subtree content digest
// (BundleDigest), the tree-layout counterpart to a single blob's
// artifactDigest, so a "pinned:sha256:..." version spec pins the whole
// subtree. Tree-layout results are never written to the flat single-blob
// packages cache (writeCachedArtifact addresses one blob, not a multi-file
// tree); materialize (t2, spec H2) owns the tree's content-addressed store.
func (f *gitArtifactFetcher) fetchTreeBundle(tree *object.Tree, canonicalRel string, parts PackageRefParts, commit string, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	if err := rejectGitSubmodules(tree, canonicalRel); err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	sub, err := committedSubtreeAt(tree, canonicalRel)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	limits := DefaultBundleLimits()
	bundle, err := NormalizeBundle(gitCommittedTreeWalker(sub, limits), limits)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	digest := BundleDigest(bundle)
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but git subtree served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	return FetchedArtifact{Digest: digest, Bundle: &bundle, CacheHit: false, Posture: posture, KeyInputs: CacheKeyInputs{OCIDigest: digest, ResolvedCommit: commit}}, nil
}

// rejectGitSubmodules fails closed if the committed git tree at canonicalRel,
// or anything beneath it, is a gitlink (filemode.Submodule / `160000`). It
// inspects the tree object graph directly — not the flattened worktree — so a
// submodule that go-git checked out as an empty directory is still caught.
// canonicalRel MUST be the same canonical path used for the worktree walk (see
// FetchArtifact): when a directory is visible in the worktree but the committed
// tree cannot resolve that exact path, this refuses rather than returning
// success, closing the noncanonical-path bypass where the lookup silently
// missed a gitlink. A nil tree fails closed — the committed tree is REQUIRED to
// verify a directory artifact carries no gitlink; the caller guarantees a
// non-nil tree for any directory pull, and this guard is the defense in depth.
func rejectGitSubmodules(tree *object.Tree, canonicalRel string) error {
	if tree == nil {
		return fmt.Errorf("committed tree unavailable; cannot verify submodules for %q", canonicalRel)
	}
	rel := strings.Trim(filepath.ToSlash(canonicalRel), "/")
	if rel == "" || rel == "." {
		return walkTreeForSubmodules(tree, "")
	}

	entry, err := tree.FindEntry(rel)
	if err != nil {
		// The path resolved to a directory in the worktree (FetchArtifact's Lstat
		// confirmed it) but is absent from the committed tree — a canonicalization
		// mismatch that could hide a gitlink. Fail closed.
		return fmt.Errorf("path %q resolves in the worktree but not in the committed tree; refusing (possible gitlink/path bypass)", rel)
	}
	if entry.Mode == filemode.Submodule {
		return fmt.Errorf("path %q is a git submodule (gitlink); submodules are not permitted in an artifact bundle", rel)
	}
	sub, err := tree.Tree(rel)
	if err != nil {
		// The entry exists but is not a descendable tree while the worktree shows
		// a directory — refuse rather than silently accept.
		return fmt.Errorf("path %q is a directory in the worktree but not a tree in the committed object; refusing", rel)
	}
	return walkTreeForSubmodules(sub, rel)
}

// walkTreeForSubmodules recursively rejects any filemode.Submodule entry in
// tree, reporting the offending path prefixed by prefix.
func walkTreeForSubmodules(tree *object.Tree, prefix string) error {
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		name, entry, err := walker.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("walking git tree: %w", err)
		}
		if entry.Mode == filemode.Submodule {
			return fmt.Errorf("tree entry %q is a git submodule (gitlink); submodules are not permitted in an artifact bundle", path.Join(prefix, name))
		}
	}
}

// committedSubtreeAt resolves the subtree at canonicalRel within tree,
// returning tree itself unchanged when canonicalRel names the repository
// root ("" or "."). The caller MUST already have proven the path is
// gitlink-free (rejectGitSubmodules) before calling this: it performs no
// gitlink check of its own, it only locates the already-verified subtree for
// gitCommittedTreeWalker.
func committedSubtreeAt(tree *object.Tree, canonicalRel string) (*object.Tree, error) {
	rel := strings.Trim(filepath.ToSlash(canonicalRel), "/")
	if rel == "" || rel == "." {
		return tree, nil
	}
	sub, err := tree.Tree(rel)
	if err != nil {
		return nil, fmt.Errorf("resolve committed subtree %q: %w", rel, err)
	}
	return sub, nil
}

// gitCommittedTreeWalker returns a BundleWalker over the committed git tree
// sub (the artifact's subtree, already gitlink-verified by the caller).
// Unlike a worktree walk (billy.Filesystem.ReadDir has no bounded/batched
// form — it always materializes a directory's ENTIRE listing in one call),
// object.TreeWalker resolves and streams exactly one entry at a time as it
// descends, lazily decoding each subtree object only when reached — so a
// directory flooded with millions of entries is enumerated without ever
// buffering a full listing: the accumulator's MaxEntries cap (applied via
// emit, below) trips within one entry's worth of over-read, not after the
// whole subtree has been materialized (t1b). Reading a file's bytes straight
// from its committed blob — after checking the blob's declared Size against
// the per-file cap, mirroring the tar/local paths' bound-before-read
// discipline — also keeps the walk's source of truth for content identical
// to the object rejectGitSubmodules already trusted, rather than a second,
// possibly-diverging read through the checked-out worktree file.
func gitCommittedTreeWalker(sub *object.Tree, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		walker := object.NewTreeWalker(sub, true, nil)
		defer walker.Close()
		for {
			name, entry, err := walker.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("walking committed git tree: %w", err)
			}
			switch entry.Mode {
			case filemode.Dir:
				mode, _ := entry.Mode.ToOSFileMode()
				if err := emit(RawBundleEntry{Path: name, Kind: rawKindDir, Mode: mode}); err != nil {
					return err
				}
			case filemode.Symlink:
				if err := emit(RawBundleEntry{Path: name, Kind: rawKindSymlink}); err != nil {
					return err
				}
			case filemode.Submodule:
				// Defense in depth: the caller already walked this same subtree via
				// rejectGitSubmodules and would have failed closed before this
				// walker ever ran. A gitlink surfacing here means that guarantee
				// broke somehow, so this fails closed too rather than skipping it.
				return fmt.Errorf("git tree entry %q: submodule (gitlink) encountered mid-walk; refusing", name)
			case filemode.Regular, filemode.Executable, filemode.Deprecated:
				f, ferr := walker.Tree().TreeEntryFile(&entry)
				if ferr != nil {
					return fmt.Errorf("git tree file %q: resolving blob: %w", name, ferr)
				}
				data, derr := readCommittedBlobFile(name, f, limits)
				if derr != nil {
					return derr
				}
				mode, _ := entry.Mode.ToOSFileMode()
				if err := emit(RawBundleEntry{Path: name, Kind: rawKindFile, Mode: mode, Size: int64(len(data)), Data: data}); err != nil {
					return err
				}
			default:
				if err := emit(RawBundleEntry{Path: name, Kind: rawKindOther}); err != nil {
					return err
				}
			}
		}
	}
}

// readCommittedBlobFile reads f's content from the committed object graph.
// Like the tar and local paths, it rejects a declared size over the per-file
// cap BEFORE Reader() is ever called (f.Size is populated from the object's
// header when the blob was resolved, independent of reading its content) and
// bounds the actual copy to MaxFileBytes+1 (independently of the declared
// size), so an oversized committed blob cannot force a large allocation
// before the accumulator sees it (mirrors defect #4's fix on the tree-walker
// path).
func readCommittedBlobFile(name string, f *object.File, limits BundleLimits) ([]byte, error) {
	if f.Size > limits.MaxFileBytes {
		return nil, fmt.Errorf("git tree file %q: size %d exceeds per-file cap of %d bytes", name, f.Size, limits.MaxFileBytes)
	}
	r, err := f.Reader()
	if err != nil {
		return nil, fmt.Errorf("git tree file %q: opening blob reader: %w", name, err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, limits.MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("git tree file %q: reading content: %w", name, err)
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, fmt.Errorf("git tree file %q: content exceeds per-file cap of %d bytes", name, limits.MaxFileBytes)
	}
	if int64(len(data)) != f.Size {
		return nil, fmt.Errorf("git tree file %q: declared size %d does not match %d bytes read", name, f.Size, len(data))
	}
	return data, nil
}

// readCachedPinnedGitArtifact returns a cache-hit FetchedArtifact when a
// digest-pinned git artifact is already in the shared packages cache, applying
// the signing posture to the cached digest. ok is false when the caller must
// fall through to a clone.
func readCachedPinnedGitArtifact(posture SigningPosture, pinned string) (FetchedArtifact, bool, error) {
	cached, ok := readCachedArtifact(pinned)
	if !ok {
		return FetchedArtifact{}, false, nil
	}
	if err := verifySignature(posture, pinned, false); err != nil {
		return FetchedArtifact{}, true, err
	}
	return FetchedArtifact{Data: cached, Digest: pinned, CacheHit: true, Posture: posture, KeyInputs: CacheKeyInputs{OCIDigest: pinned}}, true, nil
}

// validateGitSourceURL classifies the source URL so a malformed remote fails
// before any network work. file:// (local fixture / on-disk repo) is a
// legitimate clone source, so an ErrNotRemote "file" classification is not
// itself an error — only a hard parse failure is. Splitting the parse and the
// not-remote check into separate guards keeps each condition flat.
func validateGitSourceURL(url string, parts PackageRefParts) error {
	_, err := gitremote.ParseRemoteURL(url)
	if err == nil {
		return nil
	}
	if errors.Is(err, gitremote.ErrNotRemote) {
		return nil
	}
	return newArtifactImportError(parts, ReasonSchema, fmt.Errorf("git source url %q: %w", url, err))
}

// readArtifactFile opens the single-file artifact at rootOS (the canonical
// artifact path in the worktree's OS join form — the SAME path used for the
// directory-vs-file classification) and returns its bytes. readAllLimited caps
// the read (maxLayerBytes), so an oversized committed blob cannot force an
// unbounded allocation.
func readArtifactFile(wfs billy.Filesystem, rootOS string, parts PackageRefParts, commit string) ([]byte, error) {
	fh, err := wfs.Open(rootOS)
	if err != nil {
		return nil, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git read %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	defer func() { _ = fh.Close() }()
	data, err := readAllLimited(fh)
	if err != nil {
		return nil, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git read %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	return data, nil
}

// cloneAndResolve validates the source URL, shallow-clones at the resolved
// ref, and resolves the cloned HEAD. It returns the worktree filesystem, the
// committed HEAD tree object (for the gitlink inspection in fetchTreeBundle),
// a treeErr describing why the committed tree could NOT be resolved (nil on
// success), and the resolved commit SHA, mapping each failure to its
// ImportError reason. treeErr is NOT fatal for a single-file artifact (which
// needs no tree), but a directory artifact fails closed when the tree is
// unresolvable — a nil tree must never let a worktree directory bypass the
// gitlink check (that would restore the pinning-defeat). The caller
// (FetchArtifact) owns canonicalizing the artifact path and deciding the
// content layout.
func (f *gitArtifactFetcher) cloneAndResolve(src Source, parts PackageRefParts) (wfs billy.Filesystem, tree *object.Tree, treeErr error, commit string, err error) {
	if err := validateGitSourceURL(src.URL, parts); err != nil {
		return nil, nil, nil, "", err
	}

	ref := gitArtifactRef(src, parts)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo, wfs, err := f.clone(ctx, src.URL, gitFullRef(ref))
	if err != nil {
		return nil, nil, nil, "", newArtifactImportError(parts, ReasonTransport, fmt.Errorf("git clone %s @ %s: %w", src.URL, ref, err))
	}

	head, err := repo.Head()
	if err != nil {
		return nil, nil, nil, "", newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git resolve HEAD for %s @ %s: %w", src.URL, ref, err))
	}
	commit = head.Hash().String()

	// The committed tree drives the gitlink check. Any resolution failure is
	// captured in treeErr and propagated so a directory artifact can fail closed
	// (a single-file artifact ignores it — it needs no tree).
	commitObj, cerr := repo.CommitObject(head.Hash())
	if cerr != nil {
		return wfs, nil, fmt.Errorf("resolve commit object %s: %w", commit, cerr), commit, nil
	}
	t, terr := commitObj.Tree()
	if terr != nil {
		return wfs, nil, fmt.Errorf("resolve tree for commit %s: %w", commit, terr), commit, nil
	}
	return wfs, t, nil, commit, nil
}

// gitArtifactCloneQuotaBytes bounds the ON-DISK footprint of BOTH the object
// store and the checked-out worktree for a single package-artifact git
// clone. gitCloneShallow (fetcher.go, shared with the layer-tier gitFetcher)
// backs both with go-git's default in-memory storer+memfs, which decodes an
// upstream repository's full object graph — and checks the WHOLE repo out,
// not just the requested artifact subtree — into process memory before
// BundleLimits.MaxFileBytes ever gets a chance to reject an oversized
// committed blob (spec §3A / t1b). gitCloneShallowQuotaBounded instead clones
// into a fresh temp dir behind a byte-quota-guarded filesystem, so a hostile
// or oversized upstream fails closed against a fixed ceiling instead of
// exhausting host memory. Each of the object store and the worktree gets its
// own budget (the combined worst case is twice this value — still a fixed
// ceiling, independent of upstream repository size).
const gitArtifactCloneQuotaBytes = 256 << 20 // 256 MiB

// gitCloneShallowQuotaBounded performs the same Depth:1, single-branch clone
// as gitCloneShallow, but backs the object store and the checkout with a
// disk-backed, quota-guarded filesystem in a fresh temp dir instead of an
// in-memory storer+memfs (t1b, see gitArtifactCloneQuotaBytes). The returned
// filesystem's Cleanup method — invoked by FetchArtifact once it is done
// reading from it — removes the temp dir; a test-injected cloner is
// unaffected (it returns a plain billy.Filesystem with no Cleanup method, so
// the caller's best-effort type assertion is simply a no-op for it).
func gitCloneShallowQuotaBounded(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
	auth, err := gitSSHAuth(url)
	if err != nil {
		return nil, nil, err
	}
	var clientOpts []client.Option
	if auth != nil {
		clientOpts = []client.Option{client.WithSSHAuth(auth)}
	}

	tmpDir, err := os.MkdirTemp("", "da-artifact-clone-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating git artifact clone workdir: %w", err)
	}

	storeFS := newQuotaFilesystem(osfs.New(filepath.Join(tmpDir, "objects")), gitArtifactCloneQuotaBytes)
	checkoutFS := newQuotaFilesystem(osfs.New(filepath.Join(tmpDir, "worktree")), gitArtifactCloneQuotaBytes)
	storer := filesystem.NewStorage(storeFS, cache.NewObjectLRUDefault())

	repo, err := gogit.CloneContext(ctx, storer, checkoutFS, &gogit.CloneOptions{
		URL:           url,
		ClientOptions: clientOpts,
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
		Depth:         1,
		Tags:          plumbing.NoTags,
	})
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, nil, err
	}
	return repo, &artifactCloneFS{Filesystem: checkoutFS, tmpDir: tmpDir}, nil
}

// artifactCloneFS wraps the checked-out billy.Filesystem for a quota-bounded
// clone (gitCloneShallowQuotaBounded). Cleanup removes the whole temp dir
// (both the object store and the worktree) once the caller is done reading
// from it — see FetchArtifact's deferred cleanup.
type artifactCloneFS struct {
	billy.Filesystem
	tmpDir string
}

// Cleanup removes the clone's temp dir. Safe to call once; a's Filesystem
// itself is not reused afterward.
func (a *artifactCloneFS) Cleanup() {
	if a.tmpDir != "" {
		_ = os.RemoveAll(a.tmpDir)
	}
}

// errCloneQuotaExceeded is returned by a quotaFile write once its shared
// clone budget (gitArtifactCloneQuotaBytes) is exhausted.
var errCloneQuotaExceeded = errors.New("git artifact clone exceeds its disk quota")

// quotaTracker is a shared, mutex-guarded byte budget. Every write through a
// quotaFile reserves against it before the underlying write happens, so the
// quota holds even if go-git ever writes through multiple open files
// concurrently.
type quotaTracker struct {
	mu        sync.Mutex
	remaining int64
}

func newQuotaTracker(limit int64) *quotaTracker {
	return &quotaTracker{remaining: limit}
}

// reserve deducts n bytes from the remaining budget, failing closed (leaving
// the budget untouched) once n would drive it negative.
func (q *quotaTracker) reserve(n int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n > q.remaining {
		return errCloneQuotaExceeded
	}
	q.remaining -= n
	return nil
}

// quotaFilesystem wraps a billy.Filesystem so every byte written through
// Create/OpenFile/TempFile is reserved against a shared quota BEFORE it
// reaches disk (via quotaFile), failing closed once the budget is exhausted
// rather than letting a hostile or oversized upstream write without bound.
// Every other billy.Filesystem method (Open, Stat, ReadDir, Lstat, Join, …)
// delegates to the embedded Filesystem unchanged.
type quotaFilesystem struct {
	billy.Filesystem
	quota *quotaTracker
}

func newQuotaFilesystem(underlying billy.Filesystem, limitBytes int64) *quotaFilesystem {
	return &quotaFilesystem{Filesystem: underlying, quota: newQuotaTracker(limitBytes)}
}

func (q *quotaFilesystem) wrapFile(f billy.File, err error) (billy.File, error) {
	if err != nil {
		return nil, err
	}
	return &quotaFile{File: f, quota: q.quota}, nil
}

func (q *quotaFilesystem) Create(filename string) (billy.File, error) {
	return q.wrapFile(q.Filesystem.Create(filename))
}

func (q *quotaFilesystem) OpenFile(filename string, flag int, perm fs.FileMode) (billy.File, error) {
	return q.wrapFile(q.Filesystem.OpenFile(filename, flag, perm))
}

func (q *quotaFilesystem) TempFile(dir, prefix string) (billy.File, error) {
	return q.wrapFile(q.Filesystem.TempFile(dir, prefix))
}

// Chroot propagates the SAME shared quota to the chroot'd sub-filesystem, so
// the combined budget still holds across whatever internal chroot go-git's
// dotgit storage layer performs (e.g. for alternates).
func (q *quotaFilesystem) Chroot(path string) (billy.Filesystem, error) {
	inner, err := q.Filesystem.Chroot(path)
	if err != nil {
		return nil, err
	}
	return &quotaFilesystem{Filesystem: inner, quota: q.quota}, nil
}

// quotaFile wraps a billy.File so every Write/WriteAt reserves against the
// shared quota before delegating to the underlying file. Every other
// billy.File method (Read, Seek, Stat, Close, Truncate, …) delegates to the
// embedded File unchanged.
type quotaFile struct {
	billy.File
	quota *quotaTracker
}

func (f *quotaFile) Write(p []byte) (int, error) {
	if err := f.quota.reserve(int64(len(p))); err != nil {
		return 0, err
	}
	return f.File.Write(p)
}

func (f *quotaFile) WriteAt(p []byte, off int64) (int, error) {
	if err := f.quota.reserve(int64(len(p))); err != nil {
		return 0, err
	}
	return f.File.WriteAt(p, off)
}
