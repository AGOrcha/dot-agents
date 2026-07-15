package config

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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

// RawBundleEntry is one unvalidated entry collected by a bundle source
// walker (the git/local subtree walker or the tar reader) before
// NormalizeBundle runs. Path is the raw, as-observed path — it may be
// absolute, may use backslashes, may be "../escape"; canonicalBundlePath is
// exactly what makes it safe to accept or reject. Size is the entry's
// declared size, known cheaply (a stat or a tar header) without reading
// content; Data is populated only by the walker's content-reading pass, and
// only after every entry in the bundle has already passed metadata
// validation.
type RawBundleEntry struct {
	Path string
	Kind rawEntryKind
	Mode fs.FileMode
	Size int64
	Data []byte
}

// BundleLimits caps a normalized Bundle so a hostile or runaway source
// cannot exhaust memory/disk via file-count or decompression-bomb
// amplification, independent of the per-entry path-traversal defenses.
type BundleLimits struct {
	// MaxFiles caps the number of regular-file entries.
	MaxFiles int
	// MaxBytes caps the sum of declared/expanded file content bytes.
	MaxBytes int64
}

// DefaultBundleLimits returns the caps applied when a caller passes a zero
// BundleLimits. They are generous for a skill/agent resource tree
// (skill-tiering-contract §5: SKILL.md + instructions/ + references/ + a
// handful of small files) while still bounding the worst case.
func DefaultBundleLimits() BundleLimits {
	return BundleLimits{MaxFiles: 10000, MaxBytes: 64 << 20} // 64 MiB
}

// orDefault fills any unset (<=0) field with DefaultBundleLimits' value.
func (l BundleLimits) orDefault() BundleLimits {
	d := DefaultBundleLimits()
	if l.MaxFiles <= 0 {
		l.MaxFiles = d.MaxFiles
	}
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
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

// validateRawEntries runs the H1 metadata-only pass over meta: entry type,
// canonical path, duplicate/case-collision, and the running file-count/
// expanded-byte caps (checked against each entry's declared Size). No
// content is read here — meta is expected to have been collected without
// reading file bytes, so this pass can reject the whole bundle before a
// single content byte is ever touched.
func validateRawEntries(meta []RawBundleEntry, limits BundleLimits) error {
	seen := make(map[string]string, len(meta))
	fileCount := 0
	var totalBytes int64

	for _, e := range meta {
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
		if err := checkDuplicate(seen, cp); err != nil {
			return err
		}

		if e.Kind == rawKindFile {
			if e.Size < 0 {
				return fmt.Errorf("bundle entry %q: negative declared size", cp)
			}
			fileCount++
			if fileCount > limits.MaxFiles {
				return fmt.Errorf("bundle exceeds file-count cap of %d", limits.MaxFiles)
			}
			totalBytes += e.Size
			if totalBytes > limits.MaxBytes {
				return fmt.Errorf("bundle exceeds expanded-size cap of %d bytes", limits.MaxBytes)
			}
		}
	}
	return nil
}

// buildBundleEntries converts an already-validated full (content-populated)
// raw listing into the sorted Bundle.Entries form. It re-derives the
// canonical path per entry (a pure, deterministic function of e.Path,
// already proven valid by validateRawEntries) rather than threading state
// between the two passes.
func buildBundleEntries(full []RawBundleEntry) ([]BundleEntry, error) {
	entries := make([]BundleEntry, len(full))
	for i, e := range full {
		cp, err := canonicalBundlePath(e.Path)
		if err != nil {
			// Unreachable if full mirrors an already-validated meta listing;
			// guarded rather than assumed so a caller mismatch fails loudly.
			return nil, fmt.Errorf("bundle entry %q: %w", e.Path, err)
		}
		entries[i] = BundleEntry{Path: cp, IsDir: e.Kind == rawKindDir, Mode: e.Mode, Data: e.Data}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// BundleWalker collects a bundle source's raw entry listing. When
// readContent is false the walker returns only cheap metadata (path, type,
// mode, declared size) and MUST NOT read any entry's content. When true it
// additionally populates each regular-file entry's Data. NormalizeBundle
// calls a BundleWalker twice — once for validation, once (only on success)
// to materialize content — so a single walker implementation drives both the
// safety check and the real read.
type BundleWalker func(readContent bool) ([]RawBundleEntry, error)

// NormalizeBundle is the H1 entry point shared by every bundle-shaped fetch
// (git-subtree walk, local-subtree walk, tarball untar): it calls walker
// twice. The first call must be metadata-only (readContent=false) and is
// validated in full — type, canonical path, duplicate/case-collision, and
// the running file-count/expanded-byte caps — before the second,
// content-reading call ever happens. Any violation returns an error and the
// second call never runs, so nothing from a bad bundle, not even a
// well-formed entry that preceded the bad one, ever reaches the returned
// Bundle.
func NormalizeBundle(walker BundleWalker, limits BundleLimits) (Bundle, error) {
	limits = limits.orDefault()

	meta, err := walker(false)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle: listing entries: %w", err)
	}
	if err := validateRawEntries(meta, limits); err != nil {
		return Bundle{}, err
	}

	full, err := walker(true)
	if err != nil {
		return Bundle{}, fmt.Errorf("bundle: reading entries: %w", err)
	}
	entries, err := buildBundleEntries(full)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Entries: entries}, nil
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

// tarWalker returns a BundleWalker over a `+tar+gzip` blob. Because
// tar.Reader is forward-only, the metadata-only and content-reading passes
// each open an independent gzip+tar reader over the same in-memory blob
// bytes rather than trying to replay a single stream — the blob is already
// fully buffered (the fetch transport already capped its size), so
// re-decoding it is cheap and keeps the two passes fully independent: the
// content pass can never run for an entry the metadata pass has not already
// cleared.
func tarWalker(blob []byte) BundleWalker {
	return func(readContent bool) ([]RawBundleEntry, error) {
		gz, err := gzip.NewReader(bytes.NewReader(blob))
		if err != nil {
			return nil, fmt.Errorf("not a valid gzip stream: %w", err)
		}
		defer func() { _ = gz.Close() }()
		tr := tar.NewReader(gz)

		var out []RawBundleEntry
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return out, nil
			}
			if err != nil {
				return nil, fmt.Errorf("reading tar entry: %w", err)
			}
			kind := rawKindForTarType(hdr.Typeflag)
			entry := RawBundleEntry{Path: hdr.Name, Kind: kind, Mode: hdr.FileInfo().Mode(), Size: hdr.Size}
			if readContent && kind == rawKindFile {
				if hdr.Size < 0 {
					return nil, fmt.Errorf("tar entry %q: negative declared size", hdr.Name)
				}
				data, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
				if err != nil {
					return nil, fmt.Errorf("tar entry %q: reading content: %w", hdr.Name, err)
				}
				if int64(len(data)) != hdr.Size {
					return nil, fmt.Errorf("tar entry %q: declared size %d does not match %d bytes read", hdr.Name, hdr.Size, len(data))
				}
				entry.Data = data
			}
			out = append(out, entry)
		}
	}
}

// UntarBundle decodes a `+tar+gzip` artifact-bundle blob into a normalized
// Bundle, routed through the same H1 fail-closed contract as the tree-layout
// walkers (package-artifact-install spec D3/H1): a `../escape` entry, an
// absolute path, or a symlink anywhere in the archive is rejected before any
// entry's content is read.
func UntarBundle(blob []byte, limits BundleLimits) (Bundle, error) {
	return NormalizeBundle(tarWalker(blob), limits)
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
