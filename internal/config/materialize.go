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

// ArtifactsRoot returns the root of the whole content-addressed artifact
// store ("<agentsHome>/cache/artifacts"). Exported so the projection layer
// can assert that a symlink it is about to replace actually resolves INTO
// managed CAS storage (H17: only an identity-verified managed CAS link may be
// replaced — a foreign user symlink pointing elsewhere is left intact).
func ArtifactsRoot(agentsHome string) string {
	return filepath.Join(agentsHome, "cache", "artifacts")
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
	// H-hardening (package-artifact-install t3 review #2c): tighten the just-
	// published, content-addressed entry to read-only so a casual post-install
	// overwrite of store bytes needs an explicit privilege escalation (a chmod
	// +w first) rather than a bare write — turning silent tamper into a
	// deliberate, detectable act on top of the H7 integrity check. Best-effort:
	// an exotic filesystem that cannot chmod does not fail the materialize (the
	// H7/projection-boundary content check remains the authoritative guard).
	makeStoreEntryReadOnly(storePath)
	return storePath, digest, true, nil
}

// makeStoreEntryReadOnly walks a published store entry and drops the write
// bit on every FILE (→ 0o400, owner read-only) so the immutable, content-addressed tree
// resists a casual in-place overwrite — the dominant tamper vector (mutating
// existing bytes) now needs an explicit `chmod +w` first. Directories are
// LEFT writable on purpose: a read-only directory would block unlink of its
// own children (unlink needs write on the parent dir), breaking deferred
// store GC and every caller's cleanup, while buying little — an ADDED or
// DELETED file is already caught by the H7 content-digest check (the on-disk
// walk hashes exactly the files present), so read-only files close the one
// tamper vector that content-hashing alone cannot make expensive.
//
// It is defense-in-depth on top of the H7 integrity check and the
// projection-boundary re-verify. Errors are swallowed (best-effort
// hardening) — a filesystem that cannot chmod still gets a correct,
// verifiable store; only the extra privilege barrier is absent there.
func makeStoreEntryReadOnly(root string) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		_ = os.Chmod(p, 0o400)
		return nil
	})
}

// BundleContentDigest is the exported form of bundleContentDigest: the H16
// content-only integrity digest of a bundle (canonical hash over every
// regular-file entry's relative slash path + content, sorted, modes/dir
// entries excluded so it round-trips from a later on-disk walk). t3 records
// this as the git-tracked integrity anchor for a materialized artifact so the
// CAS entry can be re-verified offline against the lock, independent of the
// bundle-addressing digest (BundleDigest) that keys the store path but does
// NOT round-trip from disk (it embeds modes + explicit dir entries).
func BundleContentDigest(b Bundle) string {
	return bundleContentDigest(b)
}

// StoreContentDigest is the exported form of storeContentDigest: it recomputes
// the H16 content-only integrity digest from an on-disk store tree using the
// SAME canonicalization as BundleContentDigest, so the two are directly
// comparable. It is the primitive the offline H7 integrity resolver uses to
// detect a post-install CAS tamper without a network re-fetch or the original
// bundle.
func StoreContentDigest(dir string) (string, error) {
	return storeContentDigest(dir)
}

// VerifyStoreContentDigest reports whether the CAS entry at
// ArtifactStorePath(agentsHome, family, digest) is present and, when present,
// whether its on-disk content still matches expectedContentDigest (a value
// produced by BundleContentDigest at materialize time and recorded in the
// git-tracked lock). It is the bundle-free, network-free H7 primitive: unlike
// VerifyArtifactStoreDigest it needs no in-memory Bundle, so an offline caller
// (config verify / EnsureResolved staleness) that only has the committed
// integrity anchor — never the source bytes — can still catch a store tamper
// for ANY packages ref, regardless of source type or declared version syntax.
//
// present=false means the entry is not materialized on this machine (the
// caller's "not hydrated yet" state, not a tamper signal). present=true,
// matches=false is the tamper signal.
func VerifyStoreContentDigest(agentsHome, family, digest, expectedContentDigest string) (present, matches bool) {
	storePath := ArtifactStorePath(agentsHome, family, digest)
	fi, err := os.Stat(storePath)
	if err != nil || !fi.IsDir() {
		return false, false
	}
	got, err := storeContentDigest(storePath)
	if err != nil {
		return true, false
	}
	return true, got == expectedContentDigest
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

// VerifyArtifactStoreDigest is the READ-ONLY sibling of MaterializeToStore's
// H16 verify-on-hit check (package-artifact-install spec H7): it reports
// whether the CAS entry at ArtifactStorePath(agentsHome, family, digest) is
// present, and — only when present — whether its on-disk content still
// verifies against bundle's own content. Unlike MaterializeToStore it NEVER
// writes, quarantines, or re-extracts; it exists so a purely-diagnostic
// caller (a staleness/integrity resolver threaded into EnsureResolved or
// `config verify`) can detect a tampered CAS entry offline, using bundle
// bytes it already has locally (e.g. from a pinned-cache-hit fetch), without
// silently self-healing the tamper as a side effect of checking it.
//
// present=false means the CAS entry does not exist (or is not a directory) —
// the caller's usual "not hydrated yet" state, not evidence of tampering.
// present=true, matches=false means the entry exists but its content no
// longer matches bundle — the H7 tamper signal.
func VerifyArtifactStoreDigest(agentsHome, family, digest string, bundle Bundle) (present, matches bool) {
	return VerifyStoreContentDigest(agentsHome, family, digest, bundleContentDigest(bundle))
}

// --- CAS orphan GC (package-artifact-install spec §6, H11/H17) -------------
//
// Store-level GC of no-longer-referenced digests was explicitly deferred by
// MaterializeToStore's doc comment; t3b closes that gap. The store is never
// safe to sweep on identity/mtime heuristics — the ONLY correctness invariant
// (H11) is that a digest referenced by ANY project's lock is never removed,
// so GC is deliberately split into two steps a caller must run in order:
//
//  1. LiveArtifactDigests() unions the kind:artifact digests referenced by
//     EVERY known project's lock, computed FRESH, immediately before GC.
//  2. GCOrphanedArtifactStore(..., liveDigests) removes only a family's store
//     entries whose digest is ABSENT from that union.
//
// A lock added between step 1 and step 2 (a concurrent install/refresh in
// another project) is the residual race this two-step contract does not
// eliminate on its own — see GCOrphanedArtifactStore's doc for how a caller
// closes it (recency window, or serializing GC behind the same advisory lock
// installs use).

// LiveArtifactDigests unions the kind:artifact content digests referenced by
// every known, machine-bound project's lock (H11: this union MUST be
// computed — COMPLETELY — before any GC deletion, because a digest referenced
// by even one project is never an orphan). It reads the identity registry
// (Load) to discover every registered project, resolves each to its
// machine-local bound path (GetProjectPath — an unbound project has no local
// lock and contributes nothing), and reads that project's units lock
// (ReadUnits) for every kind:artifact entry's Digest.
//
// CRITICAL fail-closed contract (t3b round-2 — a data-loss BLOCKER fix): the
// union is either COMPLETE or an ERROR — never a silent partial. Under-
// collecting the LIVE set is not "conservative", it is the OVER-DELETE bug:
// GCOrphanedArtifactStore treats any digest ABSENT from this set as an orphan
// and RemoveAll's it, so a live digest omitted here is a live CAS entry
// deleted out from under a project that still references it. Therefore, for a
// BOUND project (path != ""), the two failure modes of ReadUnits are treated
// differently:
//
//   - lock file DOES NOT EXIST (os.IsNotExist): SAFE — the project simply has
//     no lock / no artifact units yet (never installed), so it provably
//     contributes zero digests. Continue. (agentslock.Open already maps a
//     missing file to an empty, error-free lockfile, so ReadUnits does not
//     even surface this as an error today; the explicit IsNotExist branch is
//     belt-and-suspenders against a future Open that propagates it.)
//   - lock EXISTS but WON'T READ/PARSE (any other error — a concurrent
//     agentslock.Update mid-write, an IO/NFS hiccup, a partial or corrupt
//     write): UNSAFE — we cannot prove what that project references, so we
//     MUST NOT skip it and let its digests look orphaned. Fail the whole
//     union closed, wrapped with the project name+path, so no caller ever
//     runs GC against a union that could be missing a live entry.
//
// The returned set is family-agnostic (a raw digest string, not scoped to
// skills/agents/plugins) — deliberately conservative in the SAFE direction:
// treating a digest referenced under a DIFFERENT family as "live" only makes
// GCOrphanedArtifactStore under-COLLECT (skip something it could have safely
// removed), which is a smaller reclaim, never a data-loss. That is the only
// direction over-inclusion is allowed; under-including the live set is not.
func LiveArtifactDigests() (map[string]bool, error) {
	cfg, err := Load()
	if err != nil {
		return nil, fmt.Errorf("gc: load project registry: %w", err)
	}
	live := make(map[string]bool)
	for _, name := range cfg.ListProjects() {
		path := cfg.GetProjectPath(name)
		if path == "" {
			continue // known but unbound on this machine — nothing local to read
		}
		units, err := ReadUnits(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // no lock yet — provably zero artifact units, safe to skip
			}
			// Present-but-unreadable lock: we cannot prove this project's live
			// set, so the union would be silently partial. Fail closed rather
			// than let GC over-delete a digest this project may still reference.
			return nil, fmt.Errorf("gc: project %q lock at %s is unreadable — refusing to compute a partial live-digest set (would risk deleting a live CAS entry): %w", name, path, err)
		}
		for _, u := range units.Units {
			if u.Kind == UnitKindArtifact && u.Digest != "" {
				live[u.Digest] = true
			}
		}
	}
	return live, nil
}

// looksLikeStoreDigestDirName reports whether name has EXACTLY the shape
// StoreDigestDir produces from a canonical "sha256:<hex>" digest (a bare
// 64-char lowercase-hex string). It is GC's allowlist gate: only a directory
// whose name has this exact shape is ever a delete candidate, so a
// quarantined ".corrupt-<nanos>" sibling (quarantineStoreEntry) or a stray
// ".materialize-staging-*" leftover from an interrupted materialize can never
// be mistaken for a digest entry and swept.
func looksLikeStoreDigestDirName(name string) bool {
	return looksLikeSha256Digest("sha256:" + name)
}

// GCOrphanedArtifactStore reclaims CAS entries under
// ArtifactStoreRoot(agentsHome, family) whose digest is NOT a member of
// liveDigests (see LiveArtifactDigests). It is the read-modify-delete half of
// t3b's GC: the caller computes liveDigests fresh, close to this call —
// GCOrphanedArtifactStore trusts the set it is given and never re-derives it,
// so a caller that wants the concurrent-install race closed must either
// recompute liveDigests immediately before calling (shrinking, not
// eliminating, the window) or serialize GC behind the same advisory lock
// installs already take on the project's .agentsrc.lock (agentslock.Update) —
// this function does not take that lock itself because it operates on the
// SHARED store root, not any one project's lock file.
//
// CONTRACT (t3b round-2, data-loss fail-closed): liveDigests MUST be a
// COMPLETE union — the value returned by a NON-error LiveArtifactDigests
// call. A caller that got an error from LiveArtifactDigests MUST abort and
// never reach here: this function deletes every digest-shaped entry absent
// from the set it is handed, so a partial set silently becomes a delete list
// of live entries. GCOrphanedArtifactStore cannot itself detect an under-
// complete set (it has no way to tell "no project references D" from "the
// project that references D failed to load"), which is exactly why the
// completeness guarantee lives in LiveArtifactDigests' error channel and the
// caller must honor it.
//
// H17 (no unsafe RemoveAll after an ownership check alone): every deletion
// candidate must satisfy ALL three gates before RemoveAll runs —
//  1. looksLikeStoreDigestDirName — the directory name is a well-formed
//     64-hex digest segment (never a quarantine/staging sibling or any other
//     stray entry, which are left untouched, not GC's concern);
//  2. its digest is absent from liveDigests (H11 — never remove a live
//     entry);
//  3. assertUnderCASRoot re-derives the exact delete path from
//     ArtifactStorePath and re-asserts it resolves to one clean segment
//     under the family's CAS root — the same containment check
//     MaterializeToStore itself applies before its first write, applied
//     symmetrically before GC's only delete.
//
// A missing store root is not an error (nothing to collect yet, H11's
// "never remove a live entry" is vacuously satisfied). A single entry's
// delete failure aborts the sweep and returns everything removed so far
// (fail-closed: a caller that gets a non-nil error must not assume the sweep
// completed) rather than silently skipping the failure and continuing.
func GCOrphanedArtifactStore(agentsHome, family string, liveDigests map[string]bool) (removed []string, err error) {
	root := ArtifactStoreRoot(agentsHome, family)
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("gc: read store root %s: %w", root, readErr)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !looksLikeStoreDigestDirName(name) {
			continue // not a digest-keyed entry — quarantine/staging sibling, never GC's concern
		}
		digest := "sha256:" + name
		if liveDigests[digest] {
			continue // H11 — provably still referenced by some project's lock
		}
		target := ArtifactStorePath(agentsHome, family, digest)
		if err := assertUnderCASRoot(agentsHome, family, target); err != nil {
			return removed, fmt.Errorf("gc: %w", err)
		}
		if err := fsops.RemoveAll(target); err != nil {
			return removed, fmt.Errorf("gc: remove orphan store entry %s: %w", target, err)
		}
		removed = append(removed, digest)
	}
	return removed, nil
}
