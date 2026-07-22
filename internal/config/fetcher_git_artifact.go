package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
	"github.com/go-git/go-git/v6/plumbing/protocol/packp"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/plumbing/transport"
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

	wfs, store, tree, treeErr, commit, err := f.cloneAndResolve(src, parts)
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
		// the committed-tree walk and restore the pinning-defeat. So a directory
		// artifact whose committed tree could not be resolved fails closed.
		if tree == nil {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: cannot verify submodules — committed tree unavailable: %w", parts.ArtifactPath, commit, treeErr))
		}
		return f.fetchTreeBundle(store, tree, rel, parts, commit, posture)
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
// FetchedArtifact. All content — the directory structure, the gitlink
// (submodule) rejection, and every file's bytes — is read from the COMMITTED
// tree/blob object graph (gitCommittedTreeWalker) rather than the checked-out
// worktree, for two reasons: (1) go-git flattens a `160000` submodule into an
// empty worktree directory, so a worktree-only walk would silently drop the
// gitlink's commit OID and let two different referenced commits produce the
// same BundleDigest — a digest-defeat; reading the committed tree modes
// catches the gitlink. (2) The billy worktree filesystem has no bounded
// ReadDir, so a flat directory flood could not be halted mid-enumeration.
// Digest is the whole-subtree content digest (BundleDigest), so a
// "pinned:sha256:..." version spec pins the whole subtree. Tree-layout
// results are never written to the flat single-blob packages cache
// (writeCachedArtifact addresses one blob, not a multi-file tree); materialize
// (t2, spec H2) owns the tree's content-addressed store.
func (f *gitArtifactFetcher) fetchTreeBundle(store storer.EncodedObjectStorer, tree *object.Tree, canonicalRel string, parts PackageRefParts, commit string, posture SigningPosture) (FetchedArtifact, error) {
	// The pin is re-derived from parts here (the same parse FetchArtifact runs)
	// so the tree path needs no extra pin params. A tree pull's digest is the
	// whole-subtree BundleDigest, matched against the pinned digest below.
	pinned, isPinned := digestFromVersionSpec(parts.VersionSpec)
	subHash, err := committedSubtreeHashAt(tree, canonicalRel)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	limits := DefaultBundleLimits()
	bundle, err := NormalizeBundle(gitCommittedTreeWalker(store, subHash, limits), limits)
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

// committedSubtreeHashAt resolves the object hash of the artifact subtree at
// canonicalRel within the commit's root tree, returning the root tree's own
// hash when canonicalRel names the repository root ("" or "."). It resolves
// the hash via FindEntry, which decodes only the ANCESTOR trees along the
// path (shallow, normal-sized) and NOT the target subtree itself — so the
// target's decode stays behind gitCommittedTreeWalker's pre-decode size gate
// (t1b review: a flat target directory with millions of entries must not be
// decoded here). It fails closed on the front-door cases the worktree
// classification can hide: a path visible as a directory in the worktree but
// absent from the committed tree (canonicalization/gitlink bypass), the
// artifact path itself being a gitlink, or a non-tree object where the
// worktree showed a directory.
func committedSubtreeHashAt(root *object.Tree, canonicalRel string) (plumbing.Hash, error) {
	rel := strings.Trim(filepath.ToSlash(canonicalRel), "/")
	if rel == "" || rel == "." {
		return root.Hash, nil
	}
	entry, err := root.FindEntry(rel)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("path %q resolves in the worktree but not in the committed tree; refusing (possible gitlink/path bypass)", rel)
	}
	if entry.Mode == filemode.Submodule {
		return plumbing.ZeroHash, fmt.Errorf("path %q is a git submodule (gitlink); submodules are not permitted in an artifact bundle", rel)
	}
	if entry.Mode != filemode.Dir {
		return plumbing.ZeroHash, fmt.Errorf("path %q is a directory in the worktree but not a tree in the committed object; refusing", rel)
	}
	return entry.Hash, nil
}

// gitTreeObjectByteCap derives the per-tree-object encoded-size ceiling used
// by gitCommittedTreeWalker's pre-decode gate. A git tree object encodes each
// entry as `<mode> <name>\0<20-byte-hash>` — at least ~perTreeEntryOverhead
// bytes plus the name — so a legal tree bounded by MaxEntries + the
// whole-bundle path budget cannot exceed MaxEntries*overhead +
// MaxTotalPathBytes. Any single tree object larger than that is a flood and is
// rejected before object.GetTree materializes its entry slice.
func gitTreeObjectByteCap(limits BundleLimits) int64 {
	// mode ("100644 ") + NUL + 20-byte binary hash + slack per entry.
	const perTreeEntryOverhead = 32
	return int64(limits.MaxEntries)*perTreeEntryOverhead + int64(limits.MaxTotalPathBytes)
}

// gitCommittedTreeWalker returns a BundleWalker over the committed git tree
// rooted at subHash, read directly from the object store s.
//
// t1b review hardening — the flat-tree decode bound: object.GetTree
// materializes a WHOLE tree object's entry slice into memory, so a single
// directory with millions of entries would exhaust memory during decode
// before the accumulator's MaxEntries cap (which only sees entries AFTER
// decode) could act. So before decoding any tree object this checks its
// plaintext encoded size via s.EncodedObjectSize — which reads the object
// header without inflating the whole object — against gitTreeObjectByteCap and
// fails closed on an over-cap (flooded) tree. Each blob is likewise size-gated
// via EncodedObjectSize before object.GetBlob resolves it, so an over-cap file
// object is rejected before it is decoded/delta-resolved into memory. It
// recurses manually (rather than via object.TreeWalker, which offers no
// pre-decode hook) so the gate sits on every subtree; it also rejects any
// gitlink (submodule) entry inline. This reads from the same committed object
// graph the digest pins, not the checked-out worktree, so content cannot
// diverge from what was verified.
func gitCommittedTreeWalker(s storer.EncodedObjectStorer, subHash plumbing.Hash, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		w := &gitTreeWalk{store: s, limits: limits, treeCap: gitTreeObjectByteCap(limits), emit: emit}
		return w.walk("", subHash)
	}
}

// gitTreeWalk carries the committed-tree walk state so the recursive tree walk
// and its per-entry handler are flat methods rather than deeply nested closures.
type gitTreeWalk struct {
	store   storer.EncodedObjectStorer
	limits  BundleLimits
	treeCap int64
	emit    func(RawBundleEntry) error
}

// walk size-gates the tree object at h via EncodedObjectSize BEFORE decoding it
// (the t1b pre-decode flood bound: object.GetTree would otherwise materialize a
// million-entry tree's whole entry slice into memory), decodes it, and
// dispatches each entry through handleEntry.
func (w *gitTreeWalk) walk(prefix string, h plumbing.Hash) error {
	size, err := w.store.EncodedObjectSize(h)
	if err != nil {
		return fmt.Errorf("git tree object %q: sizing: %w", prefix, err)
	}
	if size > w.treeCap {
		return fmt.Errorf("git tree object %q: encoded size %d exceeds tree-object cap of %d bytes", prefix, size, w.treeCap)
	}
	t, err := object.GetTree(w.store, h)
	if err != nil {
		return fmt.Errorf("git tree object %q: decoding: %w", prefix, err)
	}
	for i := range t.Entries {
		if err := w.handleEntry(prefix, &t.Entries[i]); err != nil {
			return err
		}
	}
	return nil
}

// handleEntry emits one committed-tree entry, recursing into subtrees. A
// submodule (gitlink) is rejected — a worktree walk would flatten it to an
// empty dir and drop its commit OID, defeating the digest. A regular/executable
// file is read size-gated from the object graph; a symlink and any other object
// are emitted by kind. Returning nil lets walk's loop advance to the next entry;
// a non-nil error aborts the whole walk.
func (w *gitTreeWalk) handleEntry(prefix string, e *object.TreeEntry) error {
	name := e.Name
	if prefix != "" {
		name = prefix + "/" + e.Name
	}
	switch e.Mode {
	case filemode.Dir:
		mode, _ := e.Mode.ToOSFileMode()
		if err := w.emit(RawBundleEntry{Path: name, Kind: rawKindDir, Mode: mode}); err != nil {
			return err
		}
		return w.walk(name, e.Hash)
	case filemode.Symlink:
		return w.emit(RawBundleEntry{Path: name, Kind: rawKindSymlink})
	case filemode.Submodule:
		return fmt.Errorf("git tree entry %q: submodule (gitlink) not permitted in an artifact bundle", name)
	case filemode.Regular, filemode.Executable, filemode.Deprecated:
		data, derr := readCommittedBlob(w.store, name, e.Hash, w.limits)
		if derr != nil {
			return derr
		}
		mode, _ := e.Mode.ToOSFileMode()
		return w.emit(RawBundleEntry{Path: name, Kind: rawKindFile, Mode: mode, Size: int64(len(data)), Data: data})
	default:
		return w.emit(RawBundleEntry{Path: name, Kind: rawKindOther})
	}
}

// readCommittedBlob reads the blob at hash from the object store s, size-gated
// before decode. It checks the blob's plaintext size via EncodedObjectSize
// (which reads the object header, not the whole content) against the per-file
// cap BEFORE object.GetBlob resolves it — so an over-cap file object is
// rejected before it is decoded (and, for a pack-delta object, before its
// delta chain is applied) into memory. It then re-bounds the actual copy to
// MaxFileBytes+1 independently, mirroring the tar/local paths.
func readCommittedBlob(s storer.EncodedObjectStorer, name string, hash plumbing.Hash, limits BundleLimits) ([]byte, error) {
	size, err := s.EncodedObjectSize(hash)
	if err != nil {
		return nil, fmt.Errorf("git tree file %q: sizing blob: %w", name, err)
	}
	if size > limits.MaxFileBytes {
		return nil, fmt.Errorf("git tree file %q: size %d exceeds per-file cap of %d bytes", name, size, limits.MaxFileBytes)
	}
	blob, err := object.GetBlob(s, hash)
	if err != nil {
		return nil, fmt.Errorf("git tree file %q: resolving blob: %w", name, err)
	}
	return readCommittedBlobFile(name, object.NewFile(name, filemode.Regular, blob), limits)
}

// committedBlobReader opens the committed blob's content stream. It is a test
// seam (matching the renameLockDirFn style) over object.File.Reader, whose only
// error legs — the reader open and the subsequent content read — cannot be
// provoked from an in-memory blob, so they are exercised by overriding this var.
var committedBlobReader = func(f *object.File) (io.ReadCloser, error) { return f.Reader() }

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
	r, err := committedBlobReader(f)
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
// clone's object store (for the committed tree/blob reads in fetchTreeBundle),
// the committed HEAD tree object, a treeErr describing why the committed tree
// could NOT be resolved (nil on success), and the resolved commit SHA, mapping
// each failure to its ImportError reason. treeErr is NOT fatal for a
// single-file artifact (which needs no tree), but a directory artifact fails
// closed when the tree is unresolvable — a nil tree must never let a worktree
// directory bypass the gitlink check (that would restore the pinning-defeat).
// The caller (FetchArtifact) owns canonicalizing the artifact path and
// deciding the content layout.
func (f *gitArtifactFetcher) cloneAndResolve(src Source, parts PackageRefParts) (wfs billy.Filesystem, store storer.EncodedObjectStorer, tree *object.Tree, treeErr error, commit string, err error) {
	if err := validateGitSourceURL(src.URL, parts); err != nil {
		return nil, nil, nil, nil, "", err
	}

	ref := gitArtifactRef(src, parts)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo, wfs, err := f.clone(ctx, src.URL, gitFullRef(ref))
	if err != nil {
		return nil, nil, nil, nil, "", newArtifactImportError(parts, ReasonTransport, fmt.Errorf("git clone %s @ %s: %w", src.URL, ref, err))
	}
	store = repo.Storer

	head, err := repo.Head()
	if err != nil {
		return nil, nil, nil, nil, "", newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git resolve HEAD for %s @ %s: %w", src.URL, ref, err))
	}
	commit = head.Hash().String()

	// The committed tree drives the gitlink check. Any resolution failure is
	// captured in treeErr and propagated so a directory artifact can fail closed
	// (a single-file artifact ignores it — it needs no tree).
	commitObj, cerr := repo.CommitObject(head.Hash())
	if cerr != nil {
		return wfs, store, nil, fmt.Errorf("resolve commit object %s: %w", commit, cerr), commit, nil
	}
	t, terr := commitObj.Tree()
	if terr != nil {
		return wfs, store, nil, fmt.Errorf("resolve tree for commit %s: %w", commit, terr), commit, nil
	}
	return wfs, store, t, nil, commit, nil
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
// as gitCloneShallow, but adds two pre-materialization bounds against a
// hostile/oversized upstream (t1b review):
//
//  1. Disk + byte quota. The object store and the checkout are backed by a
//     disk-backed, quota-guarded filesystem in a fresh temp dir instead of an
//     in-memory storer+memfs, so an oversized upstream fails closed against a
//     fixed ceiling (gitArtifactCloneQuotaBytes) instead of exhausting host
//     memory during checkout/store writes.
//  2. Partial-clone blob-size filter. A `filter=blob:limit=<per-file-cap>` is
//     requested first, so a filter-capable server (the real untrusted-remote
//     case: GitHub/GitLab/self-hosted) never sends an over-budget blob into
//     the client's pack/delta resolution at all — the over-budget object is
//     never received or resolved into memory. Servers that do not advertise
//     the filter capability (notably go-git's own file:// transport used by
//     local fixtures) return transport.ErrFilterNotSupported; we then fall
//     back to an unfiltered — but still quota-bounded — clone.
//
// RESIDUAL (documented, availability-only): the blob-size filter is the only
// fetch-time object-size control go-git v6 exposes, and it applies to BLOBS
// against FILTER-CAPABLE servers only. go-git resolves the received packfile
// (including any in-memory delta-chain reconstruction and non-blob object
// decode) BEFORE the quota-guarded storer sees a write, and exposes no hook to
// bound that in-memory pack resolution. So a malicious pack built entirely of
// deep delta chains over non-blob objects, or served by a filter-incapable
// server, can still be resolved in memory before the quota fires. This is an
// availability concern for an untrusted remote, not an integrity break (the
// digest/pin still gate what is accepted); a harder bound would need a
// transport-byte cap or a shell-`git` partial-clone fallback, tracked as a
// follow-up.
//
// The returned filesystem's Cleanup method — invoked by FetchArtifact once it
// is done reading — removes the temp dir; a test-injected cloner is unaffected
// (it returns a plain billy.Filesystem with no Cleanup method, so the caller's
// best-effort type assertion is a no-op for it).
func gitCloneShallowQuotaBounded(ctx context.Context, url, ref string) (*gogit.Repository, billy.Filesystem, error) {
	primary, fallback, err := gitSSHAuth(url)
	if err != nil {
		return nil, nil, err
	}
	repo, wfs, err := quotaBoundedCloneWithAuth(ctx, url, ref, primary)
	if err != nil && fallback != nil {
		// The agent-path clone failed (unprobeable agent); retry with the on-disk
		// key fallback for ANY error, mirroring gitCloneShallow.
		repo, wfs, err = quotaBoundedCloneWithAuth(ctx, url, ref, fallback)
	}
	return repo, wfs, err
}

// quotaBoundedCloneWithAuth runs the filter-then-unfiltered quota-bounded clone
// with a single SSH auth method (nil for non-ssh/no auth).
func quotaBoundedCloneWithAuth(ctx context.Context, url, ref string, auth client.SSHAuth) (*gogit.Repository, billy.Filesystem, error) {
	var clientOpts []client.Option
	if auth != nil {
		clientOpts = []client.Option{client.WithSSHAuth(auth)}
	}

	// Request blobs strictly larger than the per-file cap be omitted server
	// side. An omitted (over-cap) blob later reads back as a missing object and
	// fails closed — which is the same outcome the per-file cap would enforce on
	// read, achieved before the blob is ever transferred or delta-resolved.
	blobLimit := uint64(DefaultBundleLimits().MaxFileBytes) + 1
	filter := packp.FilterBlobLimit(blobLimit, packp.BlobLimitPrefixNone)

	repo, wfs, err := cloneQuotaBoundedOnce(ctx, url, ref, clientOpts, filter)
	if errors.Is(err, transport.ErrFilterNotSupported) {
		// Server has no partial-clone support; fall back to an unfiltered but
		// still quota-bounded clone (the disk/byte ceiling remains in force).
		return cloneQuotaBoundedOnce(ctx, url, ref, clientOpts, "")
	}
	return repo, wfs, err
}

// cloneQuotaBoundedOnce performs one quota-bounded clone attempt with the given
// (possibly empty) object filter, cleaning up its temp dir on any failure. An
// empty filter means no partial-clone request (go-git treats "" as unset).
func cloneQuotaBoundedOnce(ctx context.Context, url, ref string, clientOpts []client.Option, filter packp.Filter) (*gogit.Repository, billy.Filesystem, error) {
	tmpDir, err := os.MkdirTemp("", "da-artifact-clone-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating git artifact clone workdir: %w", err)
	}

	storeFS := newQuotaFilesystem(osfs.New(filepath.Join(tmpDir, "objects")), gitArtifactCloneQuotaBytes)
	checkoutFS := newQuotaFilesystem(osfs.New(filepath.Join(tmpDir, "worktree")), gitArtifactCloneQuotaBytes)
	store := filesystem.NewStorage(storeFS, cache.NewObjectLRUDefault())

	repo, err := gogit.CloneContext(ctx, store, checkoutFS, &gogit.CloneOptions{
		URL:           url,
		ClientOptions: clientOpts,
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
		Depth:         1,
		Tags:          plumbing.NoTags,
		Filter:        filter,
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
