package config

// H2 — Content-addressed immutable store (package-artifact-install spec
// §3A). MaterializeToStore is the ONE choke point every tier-2 artifact
// fetch (tree/tarball/registry content layout, D3) routes an H1-normalized
// Bundle through before its content ever lands anywhere a project can read
// it from.
//
// This file owns ONLY the digest-keyed backing store — never the H3
// reserved-namespace projection link. That link needs the OS-native managed
// -link primitives (POSIX symlink / Windows junction) owned by
// internal/links, and internal/links imports internal/config for
// config.AgentsHome(); importing internal/links back from here would be an
// import cycle. internal/platform already depends on both packages, so the
// H3 projection (and the H3 "refuse to replace a non-sourced path" guard)
// lives there — see platform.MaterializeArtifact. This split is intentional
// and mirrors the existing config/platform boundary: config fetches and
// normalizes content, platform owns every filesystem link it exposes.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// SourcedScopeSegment is the reserved namespace segment (H3) no local scope
// may ever equal: every materialized artifact projects into
// "<family>/_sourced/<source-id>/<name>/", strictly disjoint from
// "<family>/global/" and "<family>/<project>/". It is exported so
// internal/platform (the projection layer) and this package's own
// permanent-ignore pattern (local_source.go) share one literal rather than
// two copies drifting apart.
const SourcedScopeSegment = "_sourced"

// ArtifactStoreRoot returns the H2 content-addressed, immutable backing
// store root for family: "<agentsHome>/cache/artifacts/<family>/". Every
// entry under it is named by its bundle digest and, once published, is
// never mutated in place — a changed upstream digest lands at a NEW path,
// never overwrites the old one. Store-level garbage collection of
// no-longer-referenced digests is deferred (spec §6); this file never
// deletes a store entry.
func ArtifactStoreRoot(agentsHome, family string) string {
	return filepath.Join(agentsHome, "cache", "artifacts", family)
}

// ArtifactStorePath returns the immutable backing path for one bundle
// digest under family.
func ArtifactStorePath(agentsHome, family, digest string) string {
	return filepath.Join(ArtifactStoreRoot(agentsHome, family), digestDir(digest))
}

// MaterializeToStore installs bundle's H1-validated content into the H2
// content-addressed store, keyed by BundleDigest(bundle) under family. It
// is idempotent and safe under concurrent callers: a digest already present
// at its store path is trusted as-is (installed=false — the byte-identical
// no-op path, R4) rather than re-extracted, and losing a race to a
// concurrent materializer of the SAME digest is likewise a no-op (H2
// content-addressing guarantees the winner's bytes are identical to what
// this call would have written — the digest IS the content).
//
// The write itself is staged in a sibling temp directory under the store
// root and published with a single atomic rename (H1 "stage → atomic
// rename"), so a reader can never observe a partially-extracted entry, and
// a crash mid-write leaves NOTHING at the final digest path at all —
// existence of the store path is therefore proof of a complete extraction,
// with no separate completion marker needed.
func MaterializeToStore(agentsHome, family string, bundle Bundle) (storePath, digest string, installed bool, err error) {
	if family == "" {
		return "", "", false, fmt.Errorf("materialize: empty family")
	}
	digest = BundleDigest(bundle)
	if !looksLikeSha256Digest(digest) {
		// Cannot happen — BundleDigest always emits "sha256:<64 hex>" — but
		// this is the same fail-closed discipline H1 applies to every other
		// digest that becomes a filesystem path component.
		return "", "", false, fmt.Errorf("materialize: unexpected bundle digest shape %q", digest)
	}
	storePath = ArtifactStorePath(agentsHome, family, digest)

	if _, statErr := os.Stat(storePath); statErr == nil {
		return storePath, digest, false, nil
	} else if !os.IsNotExist(statErr) {
		return "", "", false, fmt.Errorf("materialize: stat store path %s: %w", storePath, statErr)
	}

	root := ArtifactStoreRoot(agentsHome, family)
	if err := fsops.MkdirAll(root, 0o755); err != nil {
		return "", "", false, fmt.Errorf("materialize: create store root %s: %w", root, err)
	}
	staging, err := os.MkdirTemp(root, ".materialize-staging-*")
	if err != nil {
		return "", "", false, fmt.Errorf("materialize: create staging dir: %w", err)
	}
	defer func() { _ = fsops.RemoveAll(staging) }()
	if err := writeBundleTree(staging, bundle); err != nil {
		return "", "", false, fmt.Errorf("materialize: stage bundle: %w", err)
	}
	if err := fsops.Rename(staging, storePath); err != nil {
		if _, statErr := os.Stat(storePath); statErr == nil {
			// Lost a race to a concurrent materializer of the SAME digest;
			// its content is byte-identical by construction (H2), so this is
			// still the idempotent no-op path, not a failure.
			return storePath, digest, false, nil
		}
		return "", "", false, fmt.Errorf("materialize: publish store path %s: %w", storePath, err)
	}
	return storePath, digest, true, nil
}

// writeBundleTree extracts every entry of bundle under root. It does not
// depend on Bundle.Entries' sort order for correctness: every file's parent
// directory is created (if absent) before the file is written, regardless
// of whether the bundle carried an explicit directory entry for it — a
// tarball layout commonly omits directory entries entirely.
func writeBundleTree(root string, bundle Bundle) error {
	for _, e := range bundle.Entries {
		dst := filepath.Join(root, filepath.FromSlash(e.Path))
		if e.IsDir {
			if err := fsops.MkdirAll(dst, dirPerm(e.Mode)); err != nil {
				return fmt.Errorf("mkdir %s: %w", e.Path, err)
			}
			continue
		}
		if err := fsops.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir parent for %s: %w", e.Path, err)
		}
		if err := fsops.WriteFile(dst, e.Data, filePerm(e.Mode)); err != nil {
			return fmt.Errorf("write %s: %w", e.Path, err)
		}
	}
	return nil
}

// dirPerm / filePerm mask an entry's observed mode down to permission bits
// only (H1 already rejects every non-file/non-dir entry kind; this is
// defense in depth against setuid/setgid/sticky bits surviving into the
// store) and fill in a sane default when the source reported none.
func dirPerm(m fs.FileMode) os.FileMode {
	if perm := m.Perm(); perm != 0 {
		return perm
	}
	return 0o755
}

func filePerm(m fs.FileMode) os.FileMode {
	if perm := m.Perm(); perm != 0 {
		return perm
	}
	return 0o644
}
