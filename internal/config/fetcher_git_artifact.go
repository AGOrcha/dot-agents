package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6"
	gogit "github.com/go-git/go-git/v6"

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

	wfs, root, commit, err := f.cloneAndResolve(src, parts)
	if err != nil {
		return FetchedArtifact{}, err
	}

	info, err := wfs.Lstat(root)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git read %s@%s: %w", parts.ArtifactPath, commit, err))
	}
	if info.IsDir() {
		// Tree layout (spec D3): the ref names a resource directory, not a
		// single file. Fetch walks the subtree into a normalized Bundle
		// (H1) rather than treating it as an opaque blob.
		return f.fetchTreeBundle(wfs, root, parts, commit, posture, isPinned, pinned)
	}

	data, err := readArtifactFile(wfs, parts, commit)
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
// FetchedArtifact. Digest is the whole-subtree content digest (BundleDigest)
// — the tree-layout counterpart to a single blob's artifactDigest — so a
// "pinned:sha256:..." version spec pins the whole subtree. Tree-layout
// results are never written to the flat single-blob packages cache
// (writeCachedArtifact addresses one blob, not a multi-file tree);
// materialize (t2, spec H2) owns the tree's content-addressed store.
func (f *gitArtifactFetcher) fetchTreeBundle(wfs billy.Filesystem, root string, parts PackageRefParts, commit string, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	bundle, err := NormalizeBundle(gitSubtreeWalker(wfs, root), DefaultBundleLimits())
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

// gitSubtreeWalker returns a BundleWalker over the git worktree subtree
// rooted at root. It classifies each ReadDir entry from its own type bits
// (a symlink is never followed to decide "is this a file or a dir" — H1
// rejects a symlink entry outright, regardless of what it points to).
func gitSubtreeWalker(wfs billy.Filesystem, root string) BundleWalker {
	return func(readContent bool) ([]RawBundleEntry, error) {
		var out []RawBundleEntry
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
					out = append(out, RawBundleEntry{Path: rel, Kind: rawKindSymlink})
				case item.IsDir():
					info, err := item.Info()
					if err != nil {
						return err
					}
					out = append(out, RawBundleEntry{Path: rel, Kind: rawKindDir, Mode: info.Mode()})
					if err := walk(full); err != nil {
						return err
					}
				case item.Type().IsRegular():
					info, err := item.Info()
					if err != nil {
						return err
					}
					entry := RawBundleEntry{Path: rel, Kind: rawKindFile, Mode: info.Mode(), Size: info.Size()}
					if readContent {
						data, err := readGitTreeFile(wfs, full, info.Size())
						if err != nil {
							return err
						}
						entry.Data = data
					}
					out = append(out, entry)
				default:
					out = append(out, RawBundleEntry{Path: rel, Kind: rawKindOther})
				}
			}
			return nil
		}
		if err := walk(root); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// readGitTreeFile reads full's content from wfs, bounded to size+1 bytes so
// a filesystem entry that lies about its declared size cannot smuggle more
// content past the bundle's already-validated byte-count cap (H1).
func readGitTreeFile(wfs billy.Filesystem, full string, size int64) ([]byte, error) {
	fh, err := wfs.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, size+1))
	if err != nil {
		return nil, err
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

// readArtifactFile opens parts.ArtifactPath in the cloned worktree and returns
// its bytes, kept separate so the defer-bound file handle does not nest inside
// cloneAndRead's control flow.
func readArtifactFile(wfs billy.Filesystem, parts PackageRefParts, commit string) ([]byte, error) {
	fh, err := wfs.Open(filepath.FromSlash(strings.TrimLeft(parts.ArtifactPath, "/")))
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
// artifact path canonicalized to the worktree's join form (root — not yet
// known to be a file or a directory), and the resolved commit SHA, mapping
// each failure to its ImportError reason. The caller (FetchArtifact) decides
// the content layout (tree vs single file) from root's Stat result.
func (f *gitArtifactFetcher) cloneAndResolve(src Source, parts PackageRefParts) (wfs billy.Filesystem, root, commit string, err error) {
	if err := validateGitSourceURL(src.URL, parts); err != nil {
		return nil, "", "", err
	}

	ref := gitArtifactRef(src, parts)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	repo, wfs, err := f.clone(ctx, src.URL, gitFullRef(ref))
	if err != nil {
		return nil, "", "", newArtifactImportError(parts, ReasonTransport, fmt.Errorf("git clone %s @ %s: %w", src.URL, ref, err))
	}

	head, err := repo.Head()
	if err != nil {
		return nil, "", "", newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("git resolve HEAD for %s @ %s: %w", src.URL, ref, err))
	}
	commit = head.Hash().String()
	root = filepath.FromSlash(strings.TrimLeft(parts.ArtifactPath, "/"))
	return wfs, root, commit, nil
}
