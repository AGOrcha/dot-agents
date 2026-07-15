package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"

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
	return gitCloneShallow(ctx, url, ref)
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
		return f.fetchTreeBundle(wfs, tree, rootOS, rel, parts, commit, posture, isPinned, pinned)
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

// fetchTreeBundle walks the git subtree rooted at root (already confirmed to
// be a directory) into a normalized Bundle (H1) and wraps it in a
// FetchedArtifact. Before walking the checked-out worktree it inspects the
// committed git tree object and rejects any gitlink (submodule) entry: go-git
// flattens a `160000` submodule into an empty directory in the worktree, so a
// worktree-only walk would silently drop the gitlink's commit OID and let two
// different referenced commits produce the same BundleDigest — a digest-defeat.
// Digest is the whole-subtree content digest (BundleDigest), the tree-layout
// counterpart to a single blob's artifactDigest, so a "pinned:sha256:..."
// version spec pins the whole subtree. Tree-layout results are never written
// to the flat single-blob packages cache (writeCachedArtifact addresses one
// blob, not a multi-file tree); materialize (t2, spec H2) owns the tree's
// content-addressed store.
func (f *gitArtifactFetcher) fetchTreeBundle(wfs billy.Filesystem, tree *object.Tree, rootOS, canonicalRel string, parts PackageRefParts, commit string, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	if err := rejectGitSubmodules(tree, canonicalRel); err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("git subtree %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	limits := DefaultBundleLimits()
	bundle, err := NormalizeBundle(gitSubtreeWalker(wfs, rootOS, limits), limits)
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

// gitSubtreeWalker returns a BundleWalker over the git worktree subtree
// rooted at root. It streams every entry through emit in a single pass,
// classifying each ReadDir entry from its own type bits (a symlink is never
// followed to decide "is this a file or a dir" — H1 rejects a symlink entry
// outright, regardless of what it points to) and reading each regular file's
// content inline. The worktree is go-git's in-memory checkout (memfs), so
// there is no real-filesystem TOCTOU window here; the confinement that
// matters for git is the gitlink rejection in fetchTreeBundle.
func gitSubtreeWalker(wfs billy.Filesystem, root string, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		var walk func(dir string) error
		walk = func(dir string) error {
			items, err := wfs.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, item := range items {
				full := wfs.Join(dir, item.Name())
				rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(full, root), string(filepath.Separator)))

				switch {
				case item.Type()&fs.ModeSymlink != 0:
					if err := emit(RawBundleEntry{Path: rel, Kind: rawKindSymlink}); err != nil {
						return err
					}
				case item.IsDir():
					info, err := item.Info()
					if err != nil {
						return err
					}
					if err := emit(RawBundleEntry{Path: rel, Kind: rawKindDir, Mode: info.Mode()}); err != nil {
						return err
					}
					if err := walk(full); err != nil {
						return err
					}
				case item.Type().IsRegular():
					info, err := item.Info()
					if err != nil {
						return err
					}
					data, err := readGitTreeFile(wfs, full, info.Size(), limits)
					if err != nil {
						return err
					}
					if err := emit(RawBundleEntry{Path: rel, Kind: rawKindFile, Mode: info.Mode(), Size: int64(len(data)), Data: data}); err != nil {
						return err
					}
				default:
					if err := emit(RawBundleEntry{Path: rel, Kind: rawKindOther}); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return walk(root)
	}
}

// readGitTreeFile reads full's content from wfs. Like the tar and local paths,
// it rejects a declared size over the per-file cap BEFORE reading and bounds
// the copy to MaxFileBytes+1 (independently of the declared size), so an
// oversized committed file cannot force a large allocation before the
// accumulator sees it (defect #4: the cap must hold on the git path too).
func readGitTreeFile(wfs billy.Filesystem, full string, size int64, limits BundleLimits) ([]byte, error) {
	if size > limits.MaxFileBytes {
		return nil, fmt.Errorf("git tree file %q: size %d exceeds per-file cap of %d bytes", full, size, limits.MaxFileBytes)
	}
	fh, err := wfs.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, limits.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, fmt.Errorf("git tree file %q: content exceeds per-file cap of %d bytes", full, limits.MaxFileBytes)
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("git tree file %q: declared size %d does not match %d bytes read", full, size, len(data))
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
