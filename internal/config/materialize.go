package config

// H2/H13–H16 — Content-addressed immutable store (package-artifact-install
// spec §3A). MaterializeToStore is the ONE choke point every tier-2 artifact
// fetch (tree/tarball/registry content layout, D3) routes an H1-normalized
// Bundle through before its content ever lands anywhere a project can read
// it from.
//
// This file owns ONLY the digest-keyed backing store — never the projection
// into a project's repo. Under the corrected per-project model (H13) the
// store is the SOLE materialized artifact location: each project's repo
// output links DIRECTLY to the immutable digest path here, keyed by the
// digest THAT project resolved from its lock. There is no shared mutable
// alias between projects (the old "_sourced/<source-id>/<name>" path, which
// broke per-project isolation, is gone). The projection mechanism lives in
// internal/platform (which already depends on both config and links);
// config cannot import internal/links without a cycle.
//
// Two hardening properties are enforced here before any byte is written:
//   - H14: the whole CAS root (~/.agents/cache/) is verified gitignored,
//     with git's own ignore semantics on the CAS path itself, BEFORE the
//     first store byte is written — so a fetched artifact can never enter
//     the local source's git tracking.
//   - H16: a pre-existing digest path is trusted only after its on-disk
//     content is re-walked and re-verified against the expected content
//     digest (the H8 verify-on-hit discipline applied to the store); a
//     corrupt, wrong-type, partially-written, or tampered entry is
//     quarantined and re-extracted, never trusted on os.Stat alone.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// ArtifactStoreRoot returns the H2 content-addressed, immutable backing
// store root for family: "<agentsHome>/cache/artifacts/<family>/". Every
// entry under it is named by its bundle digest and, once published, is
// never mutated in place — a changed upstream digest lands at a NEW path,
// never overwrites the old one. Store-level garbage collection of
// no-longer-referenced digests is deferred (spec §6); this file never
// deletes a live store entry (it only quarantines a corrupt one, H16).
func ArtifactStoreRoot(agentsHome, family string) string {
	return filepath.Join(agentsHome, "cache", "artifacts", family)
}

// ArtifactStorePath returns the immutable backing path for one bundle
// digest under family.
func ArtifactStorePath(agentsHome, family, digest string) string {
	return filepath.Join(ArtifactStoreRoot(agentsHome, family), StoreDigestDir(digest))
}

// StoreDigestDir maps a canonical "sha256:<hex>" digest to its store
// subdirectory name (the bare hex). Exported so the platform projection
// layer can address the exact CAS path a resolved unit's digest names
// without re-implementing the prefix strip.
func StoreDigestDir(digest string) string {
	return digestDir(digest)
}

// ValidateStoreSegment rejects a path component that is not a single, safe
// segment (H15): empty, ".", "..", anything containing a slash or backslash,
// a drive-letter/volume, or a NUL. A family/name/source-id must each be
// exactly one canonical segment so it can never widen a store or projection
// path beyond its intended root. Exported so the platform projection layer
// applies the identical rule to a resolved unit's identity components.
func ValidateStoreSegment(seg string) error {
	if seg == "" {
		return fmt.Errorf("empty path segment")
	}
	if seg == "." || seg == ".." {
		return fmt.Errorf("path segment %q is a relative traversal component", seg)
	}
	if strings.ContainsAny(seg, `/\`) {
		return fmt.Errorf("path segment %q contains a path separator", seg)
	}
	if len(seg) >= 2 && seg[1] == ':' {
		return fmt.Errorf("path segment %q looks like a drive-letter path", seg)
	}
	if strings.ContainsRune(seg, 0) {
		return fmt.Errorf("path segment %q contains a NUL byte", seg)
	}
	if filepath.IsAbs(seg) {
		return fmt.Errorf("path segment %q is absolute", seg)
	}
	return nil
}

// assertUnderCASRoot re-derives the CAS family root and asserts, via
// filepath.Rel, that candidate resolves to exactly one clean segment
// beneath it (H15 containment): no "..", no absolute escape, no
// separator-smuggled parent. Belt-and-suspenders on top of the per-segment
// validation, so even a future refactor that builds the path differently
// still cannot escape the store root.
func assertUnderCASRoot(agentsHome, family, candidate string) error {
	root := ArtifactStoreRoot(agentsHome, family)
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("store path %q is not resolvable under the CAS root %q: %w", candidate, root, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/") {
		return fmt.Errorf("store path %q escapes or nests below the CAS root %q (rel %q)", candidate, root, rel)
	}
	return nil
}

// MaterializeToStore installs bundle's H1-validated content into the H2
// content-addressed store, keyed by BundleDigest(bundle) under family. It is
// idempotent and safe under concurrent callers: a digest already present at
// its store path is trusted ONLY after H16 verify-on-hit (installed=false —
// the byte-identical no-op path, R4); a hit that fails verification is
// quarantined and re-extracted. Losing a rename race to a concurrent
// materializer of the SAME digest is likewise a verified no-op (H2 content-
// addressing guarantees the winner's bytes are identical — the digest IS the
// content), and a concurrent entry that fails verification is a hard error
// rather than a silent trust.
//
// H14: before ANY store byte is written, the CAS root is confirmed
// gitignored (git's own ignore semantics, checked on the CAS path itself),
// failing closed if not. The write is staged in a sibling temp directory
// under the store root and published with a single atomic rename (H1 "stage
// → atomic rename"), so a reader never observes a partially-extracted entry
// and a crash mid-write leaves nothing at the final digest path.
func MaterializeToStore(agentsHome, family string, bundle Bundle) (storePath, digest string, installed bool, err error) {
	if err := ValidateStoreSegment(family); err != nil {
		return "", "", false, fmt.Errorf("materialize: family: %w", err)
	}
	digest = BundleDigest(bundle)
	if !looksLikeSha256Digest(digest) {
		// Cannot happen — BundleDigest always emits "sha256:<64 hex>" — but
		// this is the same fail-closed discipline H1 applies to every other
		// digest that becomes a filesystem path component.
		return "", "", false, fmt.Errorf("materialize: unexpected bundle digest shape %q", digest)
	}
	storePath = ArtifactStorePath(agentsHome, family, digest)
	if err := assertUnderCASRoot(agentsHome, family, storePath); err != nil {
		return "", "", false, fmt.Errorf("materialize: %w", err)
	}

	// H14 — the CAS root must be verified gitignored (on the CAS path
	// itself) BEFORE the first byte is written.
	if err := EnsureAndVerifyCASIgnore(agentsHome, family, digest); err != nil {
		return "", "", false, err
	}

	expected := bundleContentDigest(bundle)

	if fi, statErr := os.Stat(storePath); statErr == nil {
		// H16 — never trust existence alone. Re-walk and re-verify; only a
		// verified entry is the idempotent no-op. A mismatch (corruption,
		// wrong type, partial write, tamper) is quarantined and re-extracted.
		if fi.IsDir() {
			if ok, _ := verifyStoreContent(storePath, expected); ok {
				return storePath, digest, false, nil
			}
		}
		if qerr := quarantineStoreEntry(storePath); qerr != nil {
			return "", "", false, fmt.Errorf("materialize: quarantine corrupt store entry %s: %w", storePath, qerr)
		}
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
		// Lost a race to a concurrent materializer of the SAME digest. Its
		// content MUST verify (same digest ⇒ identical content, H2); trust it
		// only after H16 verification, never on the bare rename failure.
		if fi, statErr := os.Stat(storePath); statErr == nil && fi.IsDir() {
			if ok, _ := verifyStoreContent(storePath, expected); ok {
				return storePath, digest, false, nil
			}
			return "", "", false, fmt.Errorf("materialize: concurrent store entry at %s failed integrity verification", storePath)
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

// bundleContentDigest is the H16 integrity digest of a bundle's CONTENT: a
// canonical hash over every regular-file entry's (relative slash path,
// content), sorted by path, with directory entries and file modes
// deliberately excluded so the digest is exactly reproducible from a later
// on-disk walk (verifyStoreContent). It is distinct from BundleDigest (which
// keys the store PATH and includes dir entries + modes, and so does not
// round-trip from disk when a source omitted explicit directory entries or
// used mode 0). Two bundles with the same BundleDigest necessarily have the
// same files and therefore the same bundleContentDigest.
func bundleContentDigest(b Bundle) string {
	type fileEnt struct {
		path string
		data []byte
	}
	files := make([]fileEnt, 0, len(b.Entries))
	for _, e := range b.Entries {
		if e.IsDir {
			continue
		}
		files = append(files, fileEnt{path: e.Path, data: e.Data})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, f := range files {
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", f.path, len(f.data))
		h.Write(f.data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// storeContentDigest recomputes the H16 integrity digest from an on-disk
// store tree, using the SAME canonicalization as bundleContentDigest so the
// two are directly comparable. It walks regular files only; encountering a
// symlink or any non-regular entry (a tamperer's device node, a swapped
// symlink) makes the whole store entry fail verification, which is the
// intended "wrong shape ⇒ not trusted" behavior. Directories and modes are
// excluded, matching bundleContentDigest.
func storeContentDigest(dir string) (string, error) {
	type fileEnt struct {
		path string
		data []byte
	}
	var files []fileEnt
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// No-follow shape check: only plain regular files are part of a
		// store entry's content. A symlink/device/socket means the entry was
		// tampered or is not a materialized bundle — fail verification.
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return fmt.Errorf("store entry %s contains a non-regular file %s", dir, p)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, fileEnt{path: filepath.ToSlash(rel), data: data})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, f := range files {
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", f.path, len(f.data))
		h.Write(f.data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// verifyStoreContent reports whether the on-disk store entry at dir matches
// the expected content digest (H16). A walk error, a non-regular entry, or a
// digest mismatch all report (false, err|nil) — the caller treats any of
// them as "do not trust this entry".
func verifyStoreContent(dir, expected string) (bool, error) {
	got, err := storeContentDigest(dir)
	if err != nil {
		return false, err
	}
	return got == expected, nil
}

// quarantineStoreEntry renames a corrupt/unverifiable store entry aside to a
// unique ".corrupt-<nanos>" sibling so a subsequent materialize re-extracts
// a fresh copy at the canonical path rather than repeatedly hitting the bad
// one. The entry is never deleted (a forensic copy survives for inspection);
// store-level GC is deferred. A rename failure is surfaced so the caller
// fails closed rather than extracting on top of corrupt content.
func quarantineStoreEntry(storePath string) error {
	dst := fmt.Sprintf("%s.corrupt-%d", storePath, time.Now().UnixNano())
	return fsops.Rename(storePath, dst)
}
