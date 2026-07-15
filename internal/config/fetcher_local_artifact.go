package config

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// This file adds the local source type to the tier-2 (packages/artifacts) path.
// Per config-distribution-model §4 (relaxed) / §7A.1-2 / §15 D3+D8, any source
// type may serve any artifact kind; the kind governs merge/trust, not which
// source is permitted. The local artifact fetcher mirrors the layer-tier
// localFetcher (in fetcher.go): it reads the artifact file directly from the
// filesystem (dev/test sources), content-addresses it by digest, and caches it
// in the shared packages cache. It returns a FetchedArtifact so it composes with
// the same pass-2 (p6) resolution and signing-posture stub as the other paths.

// localArtifactFetcher resolves a package artifact from a local source path. The
// digest is the content digest of the file's bytes, so the lockfile is still
// well-formed and the cache stays content-addressed. It satisfies PackageFetcher
// so SelectPackageFetcher can return it for `local` package sources.
type localArtifactFetcher struct{}

func (f *localArtifactFetcher) FetchArtifact(src Source, parts PackageRefParts) (FetchedArtifact, error) {
	posture := PostureFromSource(src)
	pinned, isPinned := digestFromVersionSpec(parts.VersionSpec)

	// A digest-pinned artifact is content-addressed, so the shared packages cache
	// is checked before touching the filesystem (offline fast path, spec §8).
	if isPinned {
		if cached, ok := readCachedArtifact(pinned); ok {
			if err := verifySignature(posture, pinned, false); err != nil {
				return FetchedArtifact{}, err
			}
			return FetchedArtifact{Data: cached, Digest: pinned, CacheHit: true, Posture: posture, KeyInputs: CacheKeyInputs{ContentDigest: pinned}}, nil
		}
	}

	base := src.Path
	if base == "" {
		base = src.URL
	}

	// The artifact path is validated BEFORE it is joined (rejecting `..`,
	// absolute, drive-letter, UNC), and ALL traversal + reads are confined to
	// an os.Root opened on the source base. os.Root refuses any path — or any
	// symlink component — that resolves outside the root, so a
	// `Source.Path=/safe/root` + `ArtifactPath=../../private` reference, or an
	// intermediate/root symlink pointing out of the tree, cannot escape.
	rel, err := validateArtifactSubpath(parts.ArtifactPath)
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonSchema, fmt.Errorf("local artifact path: %w", err))
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		if os.IsNotExist(err) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local source root %s not found: %w", base, err))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("opening local source root %s: %w", base, err))
	}
	defer func() { _ = root.Close() }()

	relOS := filepath.FromSlash(rel)
	fi, statErr := root.Lstat(relOS)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", rel, statErr))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("stat local artifact %s: %w", rel, statErr))
	}
	// A symlink AT the artifact path (a "symlink root") is rejected outright —
	// H1 admits no symlink entry, and confining the walk is only meaningful if
	// the entry point itself is not a link out of the tree.
	if fi.Mode()&fs.ModeSymlink != 0 {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("local artifact %s is a symlink; symlinks are not permitted", rel))
	}
	if fi.IsDir() {
		// Tree layout (spec D3, mirrors the git subtree walk — a local
		// source is the dev/test-fixture equivalent of a git tree): the ref
		// names a resource directory, not a single file.
		return f.fetchTreeBundle(root, rel, parts, posture, isPinned, pinned)
	}

	data, err := root.ReadFile(relOS)
	if err != nil {
		if os.IsNotExist(err) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", rel, err))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("reading local artifact %s: %w", rel, err))
	}

	digest := artifactDigest(data)
	// A digest pin must match the on-disk content, else the artifact is not what
	// was requested (tamper / mismatch -> content failure).
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but local served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
	// A local source has no committed SHA to pin against, so its working-tree
	// content IS the content (config-distribution-model §7A.4 / D6): mark the tree
	// dirty and supply the content digest as the precise worktree key, so
	// authoring before a commit still derives a distinct effective cache key.
	return FetchedArtifact{Data: data, Digest: digest, CacheHit: false, Posture: posture, KeyInputs: CacheKeyInputs{WorktreeDirty: true, WorktreeContentHash: digest, ContentDigest: digest}}, nil
}

// fetchTreeBundle walks the local subtree at artifactRel (already confirmed a
// directory) — confined under root (an os.Root on the source base) — into a
// normalized Bundle (H1) and wraps it in a FetchedArtifact. Digest is the
// whole-subtree content digest (BundleDigest), the tree-layout counterpart to
// a single file's artifactDigest, so a "pinned:sha256:..." version spec pins
// the whole subtree. Tree-layout results are never written to the flat
// single-blob packages cache (writeCachedArtifact addresses one blob, not a
// multi-file tree); materialize (t2, spec H2) owns the tree's content-addressed
// store.
func (f *localArtifactFetcher) fetchTreeBundle(root *os.Root, artifactRel string, parts PackageRefParts, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	bundle, err := NormalizeBundle(localRootWalker(root, artifactRel, DefaultBundleLimits()), DefaultBundleLimits())
	if err != nil {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("local subtree %s: %w", parts.ArtifactPath, err))
	}
	digest := BundleDigest(bundle)
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but local subtree served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	// A local tree has no committed SHA, so the same worktree-dirty framing as
	// the single-file path applies: its on-disk content is the content.
	return FetchedArtifact{Digest: digest, Bundle: &bundle, CacheHit: false, Posture: posture, KeyInputs: CacheKeyInputs{WorktreeDirty: true, WorktreeContentHash: digest, ContentDigest: digest}}, nil
}

// localRootWalker returns a BundleWalker over the local directory subtree
// rooted at artifactRel, with every operation confined to root (an os.Root on
// the source base). It streams entries through emit in a single pass, reading
// each regular file's content inline from the SAME confined open it fstats, so
// there is no window between "stat" and "read" for a mutable local tree to be
// swapped (the TOCTOU the former two-pass design left open). It Lstats each
// entry (no-follow) and rejects symlinks outright; os.Root additionally makes
// any escape past the source root impossible, and readRootFile's fstat
// re-validates kind and size at read time.
func localRootWalker(root *os.Root, artifactRel string, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		var walk func(rel string) error
		walk = func(rel string) error {
			dir, err := root.Open(filepath.FromSlash(rel))
			if err != nil {
				return err
			}
			items, readErr := dir.ReadDir(-1)
			_ = dir.Close()
			if readErr != nil {
				return readErr
			}
			for _, item := range items {
				childRel := path.Join(rel, item.Name())
				bundleRel := bundleRelPath(artifactRel, childRel)

				info, err := root.Lstat(filepath.FromSlash(childRel))
				if err != nil {
					return err
				}
				switch {
				case info.Mode()&fs.ModeSymlink != 0:
					if err := emit(RawBundleEntry{Path: bundleRel, Kind: rawKindSymlink}); err != nil {
						return err
					}
				case info.IsDir():
					if err := emit(RawBundleEntry{Path: bundleRel, Kind: rawKindDir, Mode: info.Mode()}); err != nil {
						return err
					}
					if err := walk(childRel); err != nil {
						return err
					}
				case info.Mode().IsRegular():
					mode, size, data, err := readRootFile(root, childRel, limits)
					if err != nil {
						return err
					}
					if err := emit(RawBundleEntry{Path: bundleRel, Kind: rawKindFile, Mode: mode, Size: size, Data: data}); err != nil {
						return err
					}
				default:
					if err := emit(RawBundleEntry{Path: bundleRel, Kind: rawKindOther}); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return walk(artifactRel)
	}
}

// bundleRelPath re-bases a source-root-relative path onto the artifact root so
// the bundle entry path is relative to the requested resource dir (nested
// `<artifact>/instructions/x.md` becomes `instructions/x.md`).
func bundleRelPath(artifactRel, childRel string) string {
	if artifactRel == "." {
		return childRel
	}
	return strings.TrimPrefix(childRel, artifactRel+"/")
}

// readRootFile reads rel's content confined to root. It fstats the OPEN file
// descriptor (not the pre-open Lstat) to re-validate kind and size at read
// time, so a regular file swapped for a directory/other kind between the
// walker's Lstat and this read is rejected, and the size the accumulator
// checks is the size actually read. The copy is bounded to the per-file cap+1
// independently of the fstat size, so a file that grows mid-read cannot expand
// past the cap.
func readRootFile(root *os.Root, rel string, limits BundleLimits) (fs.FileMode, int64, []byte, error) {
	fh, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return 0, 0, nil, err
	}
	defer func() { _ = fh.Close() }()
	info, err := fh.Stat()
	if err != nil {
		return 0, 0, nil, err
	}
	if !info.Mode().IsRegular() {
		return 0, 0, nil, fmt.Errorf("local tree file %q: not a regular file at read time (mode %v)", rel, info.Mode())
	}
	size := info.Size()
	if size > limits.MaxFileBytes {
		return 0, 0, nil, fmt.Errorf("local tree file %q: size %d exceeds per-file cap of %d bytes", rel, size, limits.MaxFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(fh, limits.MaxFileBytes+1))
	if err != nil {
		return 0, 0, nil, err
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return 0, 0, nil, fmt.Errorf("local tree file %q: content exceeds per-file cap of %d bytes", rel, limits.MaxFileBytes)
	}
	if int64(len(data)) != size {
		return 0, 0, nil, fmt.Errorf("local tree file %q: fstat size %d does not match %d bytes read", rel, size, len(data))
	}
	return info.Mode(), size, data, nil
}
