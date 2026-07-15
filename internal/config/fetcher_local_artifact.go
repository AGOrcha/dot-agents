package config

import (
	"fmt"
	"io"
	"io/fs"
	"os"
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
	path := filepath.Join(base, filepath.FromSlash(strings.TrimLeft(parts.ArtifactPath, "/")))

	fi, statErr := os.Lstat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", path, statErr))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("stat local artifact %s: %w", path, statErr))
	}
	if fi.IsDir() {
		// Tree layout (spec D3, mirrors the git subtree walk — a local
		// source is the dev/test-fixture equivalent of a git tree): the ref
		// names a resource directory, not a single file.
		return f.fetchTreeBundle(path, parts, posture, isPinned, pinned)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", path, err))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("reading local artifact %s: %w", path, err))
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

// fetchTreeBundle walks the local directory at path (already confirmed to be
// a directory) into a normalized Bundle (H1) and wraps it in a
// FetchedArtifact. Digest is the whole-subtree content digest (BundleDigest)
// — the tree-layout counterpart to a single file's artifactDigest — so a
// "pinned:sha256:..." version spec pins the whole subtree. Tree-layout
// results are never written to the flat single-blob packages cache
// (writeCachedArtifact addresses one blob, not a multi-file tree);
// materialize (t2, spec H2) owns the tree's content-addressed store.
func (f *localArtifactFetcher) fetchTreeBundle(path string, parts PackageRefParts, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	bundle, err := NormalizeBundle(localSubtreeWalker(path), DefaultBundleLimits())
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

// localSubtreeWalker returns a BundleWalker over the local directory rooted
// at root. It uses os.Lstat (not os.Stat) so a symlink is classified as
// rawKindSymlink without following it — H1 rejects a symlink entry outright,
// regardless of what it points to.
func localSubtreeWalker(root string) BundleWalker {
	return func(readContent bool) ([]RawBundleEntry, error) {
		var out []RawBundleEntry
		var walk func(dir string) error
		walk = func(dir string) error {
			items, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, item := range items {
				full := filepath.Join(dir, item.Name())
				rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(full, root), string(filepath.Separator)))

				info, err := os.Lstat(full)
				if err != nil {
					return err
				}
				switch {
				case info.Mode()&fs.ModeSymlink != 0:
					out = append(out, RawBundleEntry{Path: rel, Kind: rawKindSymlink})
				case info.IsDir():
					out = append(out, RawBundleEntry{Path: rel, Kind: rawKindDir, Mode: info.Mode()})
					if err := walk(full); err != nil {
						return err
					}
				case info.Mode().IsRegular():
					entry := RawBundleEntry{Path: rel, Kind: rawKindFile, Mode: info.Mode(), Size: info.Size()}
					if readContent {
						data, err := readLocalTreeFile(full, info.Size())
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

// readLocalTreeFile reads full's content, bounded to size+1 bytes so a file
// that grows between the metadata pass and the content pass cannot smuggle
// more content past the bundle's already-validated byte-count cap (H1).
func readLocalTreeFile(full string, size int64) ([]byte, error) {
	fh, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()
	data, err := io.ReadAll(io.LimitReader(fh, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("local tree file %q: declared size %d does not match %d bytes read", full, size, len(data))
	}
	return data, nil
}
