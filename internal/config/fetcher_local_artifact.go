package config

import (
	"fmt"
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
