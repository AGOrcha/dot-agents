package config

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// This file is the H1 fail-closed bundle-safety normalizer
// (package-artifact-install spec §3A H1): the single choke point every
// tier-2 artifact fetch (tree/git-subtree walk, tarball untar) routes
// through before a caller ever sees a Bundle. It does not talk to disk — it
// operates entirely on an in-memory raw entry listing and returns either a
// fully-validated Bundle or an error; nothing partial is ever returned.
//
// H1's two hardening properties, both enforced here:
//   - Canonical relative slash paths only. Empty, ".", "..", absolute,
//     drive-letter, UNC, duplicate, and case-colliding entries are rejected.
//     Every symlink, hardlink, and device/fifo entry is rejected outright,
//     regardless of where it points.
//   - The whole bundle is validated before any entry's content is read: a
//     hostile entry anywhere in a large archive is caught in a metadata-only
//     pass (path + type + declared size, no content read) before a second
//     pass ever decodes bytes — so nothing from a bad bundle is ever
//     admitted into the returned Bundle, not even the well-formed entries
//     that preceded the bad one.

// BundleEntry is one file or directory in a normalized Bundle. A symlink,
// hardlink, or device entry never reaches this type: NormalizeBundle rejects
// the whole bundle before any raw entry of that kind is admitted.
type BundleEntry struct {
	// Path is the canonical, relative, forward-slash path from the resource
	// root (e.g. "instructions/x.md"). Never empty, ".", "..", absolute, a
	// drive-letter or UNC path; never differs from another entry's Path only
	// by case.
	Path string
	// IsDir reports whether the entry is a directory. Directory entries
	// carry no Data.
	IsDir bool
	// Mode is the entry's permission bits, as observed by the source walker
	// (materialize applies these; NormalizeBundle does not interpret them
	// beyond the type gate above).
	Mode fs.FileMode
	// Data is the entry's content. Empty (never nil) for a zero-byte
	// regular file; nil for directories.
	Data []byte
}

// Bundle is the normalized, in-memory result of a tier-2 artifact fetch's
// content layout — a "tree" (git/local subtree walk) or a "tarball" (archive
// untar), package-artifact-install spec D3/H1. Every Bundle returned by
// NormalizeBundle or UntarBundle has already passed the H1 fail-closed
// safety contract.
type Bundle struct {
	// Entries is the validated listing, sorted by Path so two structurally-
	// and byte-identical bundles compare equal regardless of the source's
	// native entry order (git tree order vs tar entry order), and so a
	// materializer can create parent directories before children
	// deterministically.
	Entries []BundleEntry
}

// BundleDigest returns the canonical "sha256:<hex>" content digest of a
// Bundle: a deterministic hash over every entry's path, dir/file kind, mode,
// and content, in Bundle.Entries' (always path-sorted) order. It is the
// digest a "pinned:sha256:..." packages ref pins for a tree-layout artifact,
// the tree-layout counterpart to artifactDigest for a single blob.
func BundleDigest(b Bundle) string {
	h := sha256.New()
	for _, e := range b.Entries {
		_, _ = fmt.Fprintf(h, "%s\x00%v\x00%o\x00%d\x00", e.Path, e.IsDir, e.Mode.Perm(), len(e.Data))
		h.Write(e.Data)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// rawEntryKind classifies a pre-validation bundle entry so NormalizeBundle
// can apply the H1 fail-closed type gate before looking at anything else
// about the entry.
type rawEntryKind int

const (
	rawKindFile rawEntryKind = iota
	rawKindDir
	rawKindSymlink
	rawKindHardlink
	rawKindDevice
	// rawKindOther covers any source-specific entry type NormalizeBundle does
	// not recognize (e.g. a tar fifo/char/block device already folds into
	// rawKindDevice; this is the catch-all for anything else). It is always
	// rejected.
	rawKindOther
)

// RawBundleEntry is one entry a bundle source walker (the git/local subtree
// walker or the tar reader) hands to NormalizeBundle's single validate-and-
// read pass. Path is the raw, as-observed path — it may be absolute, may use
// backslashes, may be "../escape"; canonicalBundlePath is exactly what makes
// it safe to accept or reject. For a regular file the walker has already read
// Data under confinement and Size is the authoritative size the content was
// read at (an fstat on the open fd, or a tar header validated against the
// bytes actually read) — the accumulator asserts len(Data) == Size so a
// walker cannot admit an entry whose content diverged from its declared
// size. Directory entries carry no Data.
type RawBundleEntry struct {
	Path string
	Kind rawEntryKind
	Mode fs.FileMode
	Size int64
	Data []byte
}

// BundleLimits caps a normalized Bundle so a hostile or runaway source
// cannot exhaust memory/disk via file-count or decompression-bomb
// amplification, independent of the per-entry path-traversal defenses. Every
// budget is enforced *while the source is being walked* (see bundleAccumulator
// and tarWalker), never only after a full listing is materialized, so a
// decompression bomb is rejected before its expansion is accumulated.
type BundleLimits struct {
	// MaxEntries caps the total number of entries (files AND directories),
	// bounding an archive that floods millions of empty entries.
	MaxEntries int
	// MaxFiles caps the number of regular-file entries.
	MaxFiles int
	// MaxFileBytes caps a single file's expanded content, bounding the
	// per-entry allocation the walker performs before an entry is admitted.
	MaxFileBytes int64
	// MaxBytes caps the sum of expanded file content bytes across the bundle.
	MaxBytes int64
	// MaxStreamBytes caps the TOTAL number of decompressed bytes a tar/gzip
	// source may pull from its stream across the whole archive — including the
	// bytes tar.Reader.Next consumes internally for PAX/GNU extension headers
	// (long names/link targets), which are otherwise outside MaxEntries and the
	// per-file/total content budgets. This is the hard ceiling that bounds a
	// long-name / PAX header bomb.
	MaxStreamBytes int64
	// MaxPathBytes caps a single entry's canonical path length, rejecting an
	// individual oversized (e.g. PAX long-name) path.
	MaxPathBytes int
	// MaxTotalPathBytes caps the sum of canonical path lengths across the
	// bundle, so a flood of individually-legal long names still fails closed.
	MaxTotalPathBytes int
}

// DefaultBundleLimits returns the caps applied when a caller passes a zero
// BundleLimits. They are generous for a skill/agent resource tree
// (skill-tiering-contract §5: SKILL.md + instructions/ + references/ + a
// handful of small files) while still bounding the worst case.
func DefaultBundleLimits() BundleLimits {
	return BundleLimits{
		MaxEntries:        20000,
		MaxFiles:          10000,
		MaxFileBytes:      16 << 20, // 16 MiB per file
		MaxBytes:          64 << 20, // 64 MiB total content
		MaxStreamBytes:    96 << 20, // 96 MiB total decompressed stream (content + headers)
		MaxPathBytes:      4096,     // one path, ~PATH_MAX
		MaxTotalPathBytes: 8 << 20,  // 8 MiB of path text across the bundle
	}
}

// orDefault fills any unset (<=0) field with DefaultBundleLimits' value.
func (l BundleLimits) orDefault() BundleLimits {
	d := DefaultBundleLimits()
	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = d.MaxFiles
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxStreamBytes <= 0 {
		l.MaxStreamBytes = d.MaxStreamBytes
	}
	if l.MaxPathBytes <= 0 {
		l.MaxPathBytes = d.MaxPathBytes
	}
	if l.MaxTotalPathBytes <= 0 {
		l.MaxTotalPathBytes = d.MaxTotalPathBytes
	}
	return l
}

// canonicalBundlePath validates and canonicalizes a raw bundle entry path per
// H1: relative, forward-slash, no empty/"."/".."/absolute/drive-letter/UNC
// component. It rejects (never silently strips or best-effort sanitizes)
// anything unsafe.
func canonicalBundlePath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}
	// A backslash is a Windows path separator; a bundle path must be
	// slash-only so a crafted "..\\escape" cannot smuggle a Windows-only
	// traversal past a slash-based check, and so a UNC path
	// ("\\\\host\\share") is caught here rather than slipping through as a
	// harmless-looking relative string.
	if strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("path %q uses a backslash path separator; only forward slashes are permitted", raw)
	}
	// A drive-letter path ("C:/x" or "C:foo").
	if len(raw) >= 2 && raw[1] == ':' {
		return "", fmt.Errorf("path %q looks like a drive-letter path", raw)
	}
	// A POSIX-style UNC/host-relative path ("//host/share").
	if strings.HasPrefix(raw, "//") {
		return "", fmt.Errorf("path %q looks like a UNC path", raw)
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("path %q is absolute", raw)
	}

	clean := path.Clean(raw)
	if clean == "." {
		return "", fmt.Errorf("path %q resolves to the bundle root", raw)
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes the bundle root", raw)
	}
	if path.IsAbs(clean) {
		return "", fmt.Errorf("path %q is absolute after cleaning", raw)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("path %q has an invalid segment %q", raw, seg)
		}
	}
	return clean, nil
}

// validateArtifactSubpath validates the artifact path from a packages ref
// (`source:<artifact-path>@version`) before it is joined onto a source root
// on disk. The artifact path addresses a resource dir/file WITHIN the source;
// it must stay inside the root — so absolute, drive-letter, UNC, backslash,
// and any `..` component are rejected here, BEFORE the join, closing the
// classic `Source.Path=/safe/root` + `ArtifactPath=../../private` local-root
// escape. It returns the cleaned, forward-slash relative path (an empty or
// "/" path canonicalizes to "." — the source root itself).
func validateArtifactSubpath(raw string) (string, error) {
	if raw == "" {
		return ".", nil
	}
	if strings.ContainsRune(raw, '\\') {
		return "", fmt.Errorf("artifact path %q uses a backslash path separator", raw)
	}
	if len(raw) >= 2 && raw[1] == ':' {
		return "", fmt.Errorf("artifact path %q looks like a drive-letter path", raw)
	}
	if strings.HasPrefix(raw, "//") {
		return "", fmt.Errorf("artifact path %q looks like a UNC path", raw)
	}
	if path.IsAbs(raw) {
		return "", fmt.Errorf("artifact path %q is absolute", raw)
	}
	clean := path.Clean(raw)
	if clean == "." {
		return ".", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("artifact path %q escapes the source root", raw)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return "", fmt.Errorf("artifact path %q escapes the source root", raw)
		}
	}
	return clean, nil
}

// checkDuplicate records cp's lowercase form in seen, rejecting an exact
// duplicate path and a case-only collision between two distinct paths (H1:
// "duplicate/case-colliding entries").
func checkDuplicate(seen map[string]string, cp string) error {
	lower := strings.ToLower(cp)
	if prior, dup := seen[lower]; dup {
		if prior == cp {
			return fmt.Errorf("path %q: duplicate entry", cp)
		}
		return fmt.Errorf("paths %q and %q: case-colliding entries", prior, cp)
	}
	seen[lower] = cp
	return nil
}

// bundleAccumulator is the single validation choke point every bundle source
// streams through. It replaces the former two-pass (validate-metadata, then
// re-read) design, whose gap between passes let a symlink, an oversized file,
// or an extra entry be swapped in on a mutable local tree after validation
// (a TOCTOU window). Here each entry is validated and admitted in one step,
// during the walk, from the SAME read that produced its content — kind, path,
// size, and every cap are checked together, and the first violation aborts
// the whole walk. Entries are appended as they pass, but NormalizeBundle only
// returns the assembled Bundle after the walk completes with no error, so a
// failure anywhere exposes nothing (the H1 "validate the whole bundle before
// admitting any of it" property, now without a re-read window).
type bundleAccumulator struct {
	limits         BundleLimits
	seen           map[string]string
	entries        []BundleEntry
	entryCount     int
	fileCount      int
	totalBytes     int64
	totalPathBytes int
}

func newBundleAccumulator(limits BundleLimits) *bundleAccumulator {
	return &bundleAccumulator{limits: limits, seen: map[string]string{}}
}

// add validates one raw entry — total-entry cap, kind gate, canonical path,
// duplicate/case-collision, and (for files) the size/count/byte caps plus the
// len(Data)==Size invariant — and, on success, appends the corresponding
// BundleEntry. A walker calls add for every entry it produces; the first
// error aborts the walk. Every cap is enforced here, incrementally, so a
// runaway source is rejected as it is walked rather than after it is fully
// buffered.
func (a *bundleAccumulator) add(e RawBundleEntry) error {
	a.entryCount++
	if a.entryCount > a.limits.MaxEntries {
		return fmt.Errorf("bundle exceeds entry-count cap of %d", a.limits.MaxEntries)
	}

	switch e.Kind {
	case rawKindFile, rawKindDir:
		// continues to path validation below
	case rawKindSymlink:
		return fmt.Errorf("bundle entry %q: symlinks are not permitted", e.Path)
	case rawKindHardlink:
		return fmt.Errorf("bundle entry %q: hardlinks are not permitted", e.Path)
	case rawKindDevice:
		return fmt.Errorf("bundle entry %q: device/fifo entries are not permitted", e.Path)
	default:
		return fmt.Errorf("bundle entry %q: unsupported entry type", e.Path)
	}

	cp, err := canonicalBundlePath(e.Path)
	if err != nil {
		return fmt.Errorf("bundle entry %q: %w", e.Path, err)
	}
	// Path/metadata-byte budget: an individual oversized path (a PAX long-name)
	// and a flood of individually-legal long names both fail closed here, even
	// though the raw PAX header bytes were already bounded by the stream ceiling.
	if len(cp) > a.limits.MaxPathBytes {
		return fmt.Errorf("bundle entry path exceeds per-path cap of %d bytes", a.limits.MaxPathBytes)
	}
	a.totalPathBytes += len(cp)
	if a.totalPathBytes > a.limits.MaxTotalPathBytes {
		return fmt.Errorf("bundle exceeds total path-byte cap of %d bytes", a.limits.MaxTotalPathBytes)
	}
	if err := checkDuplicate(a.seen, cp); err != nil {
		return err
	}

	if e.Kind == rawKindFile {
		if e.Size < 0 {
			return fmt.Errorf("bundle entry %q: negative declared size", cp)
		}
		// The walker must hand over content read at the SAME size it declares,
		// so a between-stat-and-read divergence (a file that grew, or an entry
		// whose header lied about its length) cannot be admitted.
		if int64(len(e.Data)) != e.Size {
			return fmt.Errorf("bundle entry %q: declared size %d does not match %d bytes read", cp, e.Size, len(e.Data))
		}
		if e.Size > a.limits.MaxFileBytes {
			return fmt.Errorf("bundle entry %q: exceeds per-file cap of %d bytes", cp, a.limits.MaxFileBytes)
		}
		a.fileCount++
		if a.fileCount > a.limits.MaxFiles {
			return fmt.Errorf("bundle exceeds file-count cap of %d", a.limits.MaxFiles)
		}
		a.totalBytes += e.Size
		if a.totalBytes > a.limits.MaxBytes {
			return fmt.Errorf("bundle exceeds expanded-size cap of %d bytes", a.limits.MaxBytes)
		}
	}

	a.entries = append(a.entries, BundleEntry{Path: cp, IsDir: e.Kind == rawKindDir, Mode: e.Mode, Data: e.Data})
	return nil
}

// BundleWalker drives a bundle source (git-subtree walk, local-subtree walk,
// or tar reader) in a single streaming pass, calling emit for every entry it
// produces. A regular-file entry MUST already carry its content, read under
// confinement and bounded to the per-file cap, with Size set to the length
// actually read. The walker MUST stop and propagate the error the moment emit
// returns one — that is how the accumulator's caps bound the walk (e.g. an
// entry-flood is halted after MaxEntries+1 iterations rather than fully
// enumerated).
type BundleWalker func(emit func(RawBundleEntry) error) error

// NormalizeBundle is the H1 entry point shared by every bundle-shaped fetch
// (git-subtree walk, local-subtree walk, tarball untar). It drives the walker
// once, streaming every entry through the accumulator's single validate-and-
// admit step. Any violation — bad kind, unsafe path, duplicate/case-collision,
// size divergence, or an exceeded cap — aborts the walk and returns an error;
// the assembled Bundle is returned only on a fully clean walk, so nothing from
// a bad bundle (not even a well-formed entry that preceded the bad one) ever
// reaches a caller.
func NormalizeBundle(walk BundleWalker, limits BundleLimits) (Bundle, error) {
	acc := newBundleAccumulator(limits.orDefault())
	if err := walk(acc.add); err != nil {
		return Bundle{}, err
	}
	sort.Slice(acc.entries, func(i, j int) bool { return acc.entries[i].Path < acc.entries[j].Path })
	return Bundle{Entries: acc.entries}, nil
}

// --- tarball layout ---------------------------------------------------------

// rawKindForTarType classifies a tar header's Typeflag. Only TypeReg/
// TypeRegA (regular file) and TypeDir survive; every symlink, hardlink, and
// device/char/block/fifo type is classified so validateRawEntries rejects it
// with a specific reason (H1: "reject ALL symlink/hardlink/device entries").
func rawKindForTarType(t byte) rawEntryKind {
	switch t {
	case tar.TypeReg, tar.TypeRegA:
		return rawKindFile
	case tar.TypeDir:
		return rawKindDir
	case tar.TypeSymlink:
		return rawKindSymlink
	case tar.TypeLink:
		return rawKindHardlink
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return rawKindDevice
	default:
		return rawKindOther
	}
}

// streamCeilingReader bounds the TOTAL number of bytes read from an
// underlying reader, returning errStreamCeiling once the ceiling is crossed.
// It wraps the gzip stream so every byte tar.Reader pulls — file content AND
// the PAX/GNU extension-header bytes Next consumes internally — counts against
// one hard budget, closing the long-name/PAX decompression bomb that the
// per-entry and per-file caps cannot see.
type streamCeilingReader struct {
	r         io.Reader
	remaining int64
}

// errStreamCeiling is returned when a tar/gzip source tries to pull more
// decompressed bytes than BundleLimits.MaxStreamBytes.
var errStreamCeiling = errors.New("bundle: decompressed stream exceeds ceiling")

func (s *streamCeilingReader) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, errStreamCeiling
	}
	if int64(len(p)) > s.remaining {
		p = p[:s.remaining]
	}
	n, err := s.r.Read(p)
	s.remaining -= int64(n)
	return n, err
}

// tarWalker returns a BundleWalker over a `+tar+gzip` blob. It advances the
// tar reader exactly once, streaming each entry to emit as it is decoded, so
// the accumulator's caps are enforced while the archive is being expanded —
// never after a full listing is buffered. Three decompression-bomb classes are
// bounded here: an entry flood halts because emit errors once MaxEntries is
// exceeded; each file's content is copied through an io.LimitReader capped at
// the per-file budget, so a header that under-declares a huge payload cannot
// expand memory past the cap; and the whole decompressed stream (content plus
// the PAX/GNU header bytes Next consumes out of band) is bounded by a hard
// streamCeilingReader ceiling.
func tarWalker(blob []byte, limits BundleLimits) BundleWalker {
	limits = limits.orDefault()
	return func(emit func(RawBundleEntry) error) error {
		gz, err := gzip.NewReader(bytes.NewReader(blob))
		if err != nil {
			return fmt.Errorf("not a valid gzip stream: %w", err)
		}
		defer func() { _ = gz.Close() }()
		tr := tar.NewReader(&streamCeilingReader{r: gz, remaining: limits.MaxStreamBytes})

		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("reading tar entry: %w", err)
			}
			kind := rawKindForTarType(hdr.Typeflag)
			entry := RawBundleEntry{Path: hdr.Name, Kind: kind, Mode: hdr.FileInfo().Mode(), Size: hdr.Size}
			if kind == rawKindFile {
				data, err := readTarFile(tr, hdr, limits)
				if err != nil {
					return err
				}
				entry.Data = data
				entry.Size = int64(len(data))
			}
			if err := emit(entry); err != nil {
				return err
			}
		}
	}
}

// readTarFile copies a tar file entry's content, bounded independently of the
// header's declared size so a lying header cannot expand past the per-file
// cap. It rejects a negative or over-cap declared size before reading and
// re-checks the bytes actually read, returning content whose length is the
// authoritative size the accumulator validates against.
func readTarFile(tr *tar.Reader, hdr *tar.Header, limits BundleLimits) ([]byte, error) {
	if hdr.Size < 0 {
		return nil, fmt.Errorf("tar entry %q: negative declared size", hdr.Name)
	}
	if hdr.Size > limits.MaxFileBytes {
		return nil, fmt.Errorf("tar entry %q: declared size %d exceeds per-file cap of %d bytes", hdr.Name, hdr.Size, limits.MaxFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(tr, limits.MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("tar entry %q: reading content: %w", hdr.Name, err)
	}
	if int64(len(data)) > limits.MaxFileBytes {
		return nil, fmt.Errorf("tar entry %q: content exceeds per-file cap of %d bytes", hdr.Name, limits.MaxFileBytes)
	}
	if int64(len(data)) != hdr.Size {
		return nil, fmt.Errorf("tar entry %q: declared size %d does not match %d bytes read", hdr.Name, hdr.Size, len(data))
	}
	return data, nil
}

// UntarBundle decodes a `+tar+gzip` artifact-bundle blob into a normalized
// Bundle, routed through the same H1 fail-closed contract as the tree-layout
// walkers (package-artifact-install spec D3/H1): a `../escape` entry, an
// absolute path, a symlink anywhere in the archive, or a decompression bomb
// (entry flood or over-cap expansion) is rejected during the single decode
// pass, before the returned Bundle admits anything.
func UntarBundle(blob []byte, limits BundleLimits) (Bundle, error) {
	limits = limits.orDefault()
	return NormalizeBundle(tarWalker(blob, limits), limits)
}

// looksLikeGzip reports whether data begins with the gzip magic bytes
// (RFC 1952 §2.3.1). It is a content sniff, not a trust decision: it only
// decides whether a fetched blob is treated as a tarball-layout bundle (and
// thus routed through UntarBundle, fail-closed) versus a legacy opaque
// single-file blob.
func looksLikeGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

// MaybeUntarBundle sniffs data for the gzip magic bytes and, only if
// present, decodes it as a `+tar+gzip` artifact bundle through the H1
// fail-closed normalizer. It returns (nil, nil) for content that does not
// look like gzip at all — a legacy opaque single-file blob is not an error,
// it simply has no Bundle. Once the sniff says "this claims to be a gzip
// stream", any decode/validation failure IS an error (fail closed): a caller
// that announces a bundle-shaped blob does not get silently treated as an
// opaque one.
func MaybeUntarBundle(data []byte, limits BundleLimits) (*Bundle, error) {
	if !looksLikeGzip(data) {
		return nil, nil
	}
	bundle, err := UntarBundle(data, limits)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}
