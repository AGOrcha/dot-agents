package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// statOpenFile fstats an already-open file descriptor. It is a test seam
// (renameLockDirFn style) over (*os.File).Stat, whose error leg — an fstat
// failure on a descriptor that opened cleanly — is not reproducible with a real
// file, so it is exercised by overriding this var. Shared by openConfinedDir
// and readRootFile.
var statOpenFile = func(fh *os.File) (fs.FileInfo, error) { return fh.Stat() }

// readDirBatch reads up to n directory entries from an open directory handle. It
// is a test seam over (*os.File).ReadDir so streamConfinedDir's non-EOF error
// leg and its defensive len(batch)==0 termination (which a real *os.File never
// takes — it always signals exhaustion via io.EOF) can be exercised.
var readDirBatch = func(fh *os.File, n int) ([]os.DirEntry, error) { return fh.ReadDir(n) }

// readCappedFile reads rel's already-open descriptor bounded to limit bytes. It
// is a test seam over io.ReadAll(io.LimitReader(...)) so readRootFile's
// read-error leg and its post-read size-divergence legs (content grown past the
// cap, or a byte count that no longer matches the fstat size) can be exercised
// deterministically without racing a concurrent writer.
var readCappedFile = func(fh *os.File, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(fh, limit))
}

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
	if fa, done, err := pinnedArtifactCacheHit(pinned, isPinned, posture); done {
		return fa, err
	}

	base := src.Path
	if base == "" {
		base = src.URL
	}

	root, rel, fi, err := f.openLocalArtifact(base, parts)
	if err != nil {
		return FetchedArtifact{}, err
	}
	defer func() { _ = root.Close() }()

	if fi.IsDir() {
		// Tree layout (spec D3, mirrors the git subtree walk — a local
		// source is the dev/test-fixture equivalent of a git tree): the ref
		// names a resource directory, not a single file.
		return f.fetchTreeBundle(root, rel, fi, parts, posture, isPinned, pinned)
	}
	return f.readSingleFileArtifact(root, rel, fi, parts, posture, isPinned, pinned)
}

// pinnedArtifactCacheHit serves a digest-pinned artifact from the shared
// packages cache without touching the filesystem (offline fast path, spec §8).
// done is true when the caller should return (fa, err) immediately: on a cache
// hit (fa populated) or when the cached blob fails signature verification.
func pinnedArtifactCacheHit(pinned string, isPinned bool, posture SigningPosture) (FetchedArtifact, bool, error) {
	if !isPinned {
		return FetchedArtifact{}, false, nil
	}
	cached, ok := readCachedArtifact(pinned)
	if !ok {
		return FetchedArtifact{}, false, nil
	}
	if err := verifySignature(posture, pinned, false); err != nil {
		return FetchedArtifact{}, true, err
	}
	return FetchedArtifact{Data: cached, Digest: pinned, CacheHit: true, Posture: posture, KeyInputs: CacheKeyInputs{ContentDigest: pinned}}, true, nil
}

// openLocalArtifact validates the artifact subpath, opens an os.Root confined to
// the source base, and Lstats the entry — rejecting a symlink root outright (H1
// admits no symlink entry). os.Root refuses any path — or symlink component —
// resolving outside the root, so `Source.Path=/safe/root` + `../../private`, or
// an intermediate/root symlink out of the tree, cannot escape. On success the
// returned root is the caller's to Close; on a post-open error it is closed here.
func (f *localArtifactFetcher) openLocalArtifact(base string, parts PackageRefParts) (*os.Root, string, fs.FileInfo, error) {
	rel, err := validateArtifactSubpath(parts.ArtifactPath)
	if err != nil {
		return nil, "", nil, newArtifactImportError(parts, ReasonSchema, fmt.Errorf("local artifact path: %w", err))
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local source root %s not found: %w", base, err))
		}
		return nil, "", nil, newArtifactImportError(parts, ReasonContent, fmt.Errorf("opening local source root %s: %w", base, err))
	}
	relOS := filepath.FromSlash(rel)
	fi, statErr := root.Lstat(relOS)
	if statErr != nil {
		_ = root.Close()
		if os.IsNotExist(statErr) {
			return nil, "", nil, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", rel, statErr))
		}
		return nil, "", nil, newArtifactImportError(parts, ReasonContent, fmt.Errorf("stat local artifact %s: %w", rel, statErr))
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		_ = root.Close()
		return nil, "", nil, newArtifactImportError(parts, ReasonContent, fmt.Errorf("local artifact %s is a symlink; symlinks are not permitted", rel))
	}
	return root, rel, fi, nil
}

// readSingleFileArtifact reads a single-file local artifact through the same
// confined + identity-checked + capped path as the tree files (so an in-root
// symlink swapped in after the Lstat cannot redirect the read and an oversized
// file cannot force an unbounded allocation), enforces a digest pin, verifies
// the signing posture, and caches the content-addressed blob. A local source
// has no committed SHA, so its working-tree content IS the content
// (config-distribution-model §7A.4 / D6): the tree is marked dirty and the
// content digest supplied as the precise worktree key.
func (f *localArtifactFetcher) readSingleFileArtifact(root *os.Root, rel string, fi fs.FileInfo, parts PackageRefParts, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	_, _, data, err := readRootFile(root, rel, fi, DefaultBundleLimits())
	if err != nil {
		if os.IsNotExist(err) {
			return FetchedArtifact{}, newArtifactImportError(parts, ReasonNotFound, fmt.Errorf("local artifact %s not found: %w", rel, err))
		}
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("reading local artifact %s: %w", rel, err))
	}
	digest := artifactDigest(data)
	if isPinned && digest != pinned {
		return FetchedArtifact{}, newArtifactImportError(parts, ReasonContent, fmt.Errorf("digest mismatch: pinned %s but local served %s", pinned, digest))
	}
	if err := verifySignature(posture, digest, false); err != nil {
		return FetchedArtifact{}, err
	}
	if err := writeCachedArtifact(digest, data); err != nil {
		return FetchedArtifact{}, err
	}
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
func (f *localArtifactFetcher) fetchTreeBundle(root *os.Root, artifactRel string, rootInfo fs.FileInfo, parts PackageRefParts, posture SigningPosture, isPinned bool, pinned string) (FetchedArtifact, error) {
	bundle, err := NormalizeBundle(localRootWalker(root, artifactRel, rootInfo, DefaultBundleLimits()), DefaultBundleLimits())
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
// each regular file's content inline from the SAME confined open it fstats.
//
// Because os.Root PERMITS in-root symlinks, a pre-open Lstat is not enough: a
// path classified as a file/dir can be swapped for an in-root symlink before
// the open, and os.Root would then follow it. So every directory and file
// open verifies the OPENED object's identity against the pre-open Lstat via
// os.SameFile (device+inode) — a mismatch means the entry was swapped between
// classify and open, and the walk fails closed. Combined with the Lstat-based
// symlink rejection and os.Root's no-escape guarantee, the symlink ban and the
// subtree boundary hold even under a concurrent mutation.
func localRootWalker(root *os.Root, artifactRel string, rootInfo fs.FileInfo, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		w := &localWalk{root: root, artifactRel: artifactRel, limits: limits, emit: emit}
		return w.walkDir(artifactRel, rootInfo)
	}
}

// localWalk carries the confined-walk state so the recursive directory walk and
// its per-entry handler are flat methods rather than deeply nested closures.
type localWalk struct {
	root        *os.Root
	artifactRel string
	limits      BundleLimits
	emit        func(RawBundleEntry) error
}

// walkDir streams the confined directory at rel and dispatches each entry to
// handleEntry (recursing into subdirectories).
func (w *localWalk) walkDir(rel string, expected fs.FileInfo) error {
	return streamConfinedDir(w.root, rel, expected, func(item os.DirEntry) error {
		return w.handleEntry(rel, item)
	})
}

// handleEntry emits one directory entry. A symlink is emitted as a symlink kind
// (never followed); a directory is emitted then recursed into; a regular file is
// read through the confined, capped path; anything else is emitted as "other".
func (w *localWalk) handleEntry(rel string, item os.DirEntry) error {
	childRel := path.Join(rel, item.Name())
	bundleRel := bundleRelPath(w.artifactRel, childRel)

	info, err := w.root.Lstat(filepath.FromSlash(childRel))
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return w.emit(RawBundleEntry{Path: bundleRel, Kind: rawKindSymlink})
	case info.IsDir():
		if err := w.emit(RawBundleEntry{Path: bundleRel, Kind: rawKindDir, Mode: info.Mode()}); err != nil {
			return err
		}
		return w.walkDir(childRel, info)
	case info.Mode().IsRegular():
		mode, size, data, err := readRootFile(w.root, childRel, info, w.limits)
		if err != nil {
			return err
		}
		return w.emit(RawBundleEntry{Path: bundleRel, Kind: rawKindFile, Mode: mode, Size: size, Data: data})
	default:
		return w.emit(RawBundleEntry{Path: bundleRel, Kind: rawKindOther})
	}
}

// dirReadBatchSize bounds how many directory entries streamConfinedDir
// requests from the filesystem in a single ReadDir call. os.ReadDir(-1) (and
// (*os.File).ReadDir(-1)) materializes a directory's ENTIRE listing in one
// allocation before a caller ever sees an entry — for a directory flooded
// with millions of flat entries, that single call is the unbounded
// allocation MaxEntries is supposed to prevent (t1b). Reading in small,
// fixed-size batches instead means a flood is rejected within one batch's
// worth of over-read, not after the whole directory has been buffered.
const dirReadBatchSize = 1024

// streamConfinedDir opens rel under root, verifies the opened directory's
// identity against the pre-open Lstat (expected) via os.SameFile — defeating
// an in-root symlink swapped in between classify and open — and streams its
// entries to visit in dirReadBatchSize batches (never one unbounded
// ReadDir(-1)). It stops the moment visit returns an error, so once the
// accumulator's MaxEntries cap trips (via emit, inside visit) the read halts
// immediately rather than continuing to enumerate the rest of a flooded
// directory.
func streamConfinedDir(root *os.Root, rel string, expected fs.FileInfo, visit func(os.DirEntry) error) error {
	fh, err := openConfinedDir(root, rel, expected)
	if err != nil {
		return err
	}
	defer func() { _ = fh.Close() }()
	for {
		batch, err := readDirBatch(fh, dirReadBatchSize)
		if verr := visitDirBatch(batch, visit); verr != nil {
			return verr
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(batch) == 0 {
			return nil
		}
	}
}

// openConfinedDir opens rel under root and verifies the opened object is a
// directory whose identity matches the pre-open Lstat (expected) via
// os.SameFile — defeating an in-root symlink swapped in between classify and
// open. The returned file is the caller's to Close; on error it is closed here.
func openConfinedDir(root *os.Root, rel string, expected fs.FileInfo) (*os.File, error) {
	fh, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	info, err := statOpenFile(fh)
	if err != nil {
		_ = fh.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = fh.Close()
		return nil, fmt.Errorf("local tree dir %q: not a directory at open time (mode %v)", rel, info.Mode())
	}
	if !os.SameFile(expected, info) {
		_ = fh.Close()
		return nil, fmt.Errorf("local tree dir %q: identity changed between classify and open (possible in-root symlink swap)", rel)
	}
	return fh, nil
}

// visitDirBatch passes each entry in one ReadDir batch to visit, stopping at the
// first error.
func visitDirBatch(batch []os.DirEntry, visit func(os.DirEntry) error) error {
	for _, item := range batch {
		if err := visit(item); err != nil {
			return err
		}
	}
	return nil
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
// descriptor and (a) rejects a non-regular kind, (b) verifies the opened
// object's identity against the pre-open Lstat (expected) via os.SameFile so an
// in-root symlink swapped in between classify and open cannot redirect the read,
// and (c) bounds the copy to the per-file cap+1 independently of the fstat size
// so a file that grows mid-read cannot expand past the cap. The size the
// accumulator validates is the fstat size, matched against the bytes read.
func readRootFile(root *os.Root, rel string, expected fs.FileInfo, limits BundleLimits) (fs.FileMode, int64, []byte, error) {
	fh, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		return 0, 0, nil, err
	}
	defer func() { _ = fh.Close() }()
	info, err := statOpenFile(fh)
	if err != nil {
		return 0, 0, nil, err
	}
	if !info.Mode().IsRegular() {
		return 0, 0, nil, fmt.Errorf("local tree file %q: not a regular file at read time (mode %v)", rel, info.Mode())
	}
	if !os.SameFile(expected, info) {
		return 0, 0, nil, fmt.Errorf("local tree file %q: identity changed between classify and open (possible in-root symlink swap)", rel)
	}
	size := info.Size()
	if size > limits.MaxFileBytes {
		return 0, 0, nil, fmt.Errorf("local tree file %q: size %d exceeds per-file cap of %d bytes", rel, size, limits.MaxFileBytes)
	}
	data, err := readCappedFile(fh, limits.MaxFileBytes+1)
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
