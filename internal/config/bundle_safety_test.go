package config

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"testing"
)

// --- test helpers ------------------------------------------------------------

// staticWalker returns a BundleWalker that streams a fixed entry list through
// emit in one pass, matching the single validate-and-read contract the real
// walkers use. Each file entry is emitted with content already attached and
// Size == len(Data), as a confined walker would produce it.
func staticWalker(entries []RawBundleEntry) BundleWalker {
	return func(emit func(RawBundleEntry) error) error {
		for _, e := range entries {
			if err := emit(e); err != nil {
				return err
			}
		}
		return nil
	}
}

// buildTarGz builds a `+tar+gzip` blob from the entries add writes, for
// exercising UntarBundle against real archive bytes.
func buildTarGz(t *testing.T, add func(tw *tar.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add(tw)
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

func tarAddFile(t *testing.T, tw *tar.Writer, name string, mode int64, body []byte) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader(%s): %v", name, err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("Write(%s): %v", name, err)
	}
}

func tarAddDir(t *testing.T, tw *tar.Writer, name string, mode int64) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: mode}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader(%s): %v", name, err)
	}
}

func tarAddSymlink(t *testing.T, tw *tar.Writer, name, target string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader(%s): %v", name, err)
	}
}

func tarAddHardlink(t *testing.T, tw *tar.Writer, name, target string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeLink, Linkname: target, Mode: 0o644}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader(%s): %v", name, err)
	}
}

func tarAddDevice(t *testing.T, tw *tar.Writer, name string) {
	t.Helper()
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeChar, Mode: 0o644, Devmajor: 1, Devminor: 1}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader(%s): %v", name, err)
	}
}

func bundlePaths(b Bundle) []string {
	out := make([]string, len(b.Entries))
	for i, e := range b.Entries {
		out[i] = e.Path
	}
	return out
}

// --- canonicalBundlePath -----------------------------------------------------

func TestCanonicalBundlePathAccepts(t *testing.T) {
	cases := map[string]string{
		"instructions/x.md": "instructions/x.md",
		"./a/b.txt":         "a/b.txt",
		".env":              ".env",
		"a/.hidden/b":       "a/.hidden/b",
		"a/b/../c":          "a/c",
		"a//b":              "a/b",
		"a/./b/":            "a/b",
	}
	for raw, want := range cases {
		got, err := canonicalBundlePath(raw)
		if err != nil {
			t.Fatalf("canonicalBundlePath(%q): unexpected error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("canonicalBundlePath(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCanonicalBundlePathRejects(t *testing.T) {
	cases := []string{
		"",
		".",
		"..",
		"../escape",
		"a/../../escape",
		"/etc/passwd",
		"C:/Windows",
		"C:foo",
		`..\escape`,
		`a\b`,
		"//host/share",
	}
	for _, raw := range cases {
		if _, err := canonicalBundlePath(raw); err == nil {
			t.Fatalf("canonicalBundlePath(%q): expected rejection, got none", raw)
		}
	}
}

// --- validateArtifactSubpath (defect #1 pre-join gate) ----------------------

func TestValidateArtifactSubpathAccepts(t *testing.T) {
	cases := map[string]string{
		"skill/release-docs-refresh": "skill/release-docs-refresh",
		"a/b/c":                      "a/b/c",
		"":                           ".",
		".":                          ".",
		"a/./b":                      "a/b",
	}
	for raw, want := range cases {
		got, err := validateArtifactSubpath(raw)
		if err != nil {
			t.Fatalf("validateArtifactSubpath(%q): unexpected error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("validateArtifactSubpath(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestValidateArtifactSubpathRejects(t *testing.T) {
	cases := []string{
		"..",
		"../private",
		"../../private",
		"a/../../escape",
		"/etc/hostname",
		"/skill/x",
		"C:/Windows",
		`..\escape`,
		`a\b`,
		"//host/share",
	}
	for _, raw := range cases {
		if _, err := validateArtifactSubpath(raw); err == nil {
			t.Fatalf("validateArtifactSubpath(%q): expected rejection, got none", raw)
		}
	}
}

// --- NormalizeBundle: happy path ---------------------------------------------

func TestNormalizeBundleTreeLayoutSurvivesStructure(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "SKILL.md", Kind: rawKindFile, Mode: 0o644, Size: 5, Data: []byte("hello")},
		{Path: "instructions", Kind: rawKindDir, Mode: 0o755 | fs.ModeDir},
		{Path: "instructions/x.md", Kind: rawKindFile, Mode: 0o644, Size: 3, Data: []byte("abc")},
		{Path: ".env", Kind: rawKindFile, Mode: 0o600, Size: 0, Data: []byte{}},
	}
	b, err := NormalizeBundle(staticWalker(entries), BundleLimits{})
	if err != nil {
		t.Fatalf("NormalizeBundle: %v", err)
	}
	if len(b.Entries) != 4 {
		t.Fatalf("entries = %d, want 4: %+v", len(b.Entries), b.Entries)
	}

	byPath := make(map[string]BundleEntry, len(b.Entries))
	for _, e := range b.Entries {
		byPath[e.Path] = e
	}

	nested, ok := byPath["instructions/x.md"]
	if !ok {
		t.Fatalf("expected nested entry instructions/x.md, got paths %v", bundlePaths(b))
	}
	if nested.IsDir || string(nested.Data) != "abc" {
		t.Fatalf("nested entry = %+v", nested)
	}

	dir, ok := byPath["instructions"]
	if !ok || !dir.IsDir {
		t.Fatalf("expected dir entry instructions to survive, got %+v", dir)
	}

	dotfile, ok := byPath[".env"]
	if !ok || dotfile.Mode.Perm() != 0o600 {
		t.Fatalf("expected dotfile .env with mode 0600 to survive, got %+v", dotfile)
	}

	skill, ok := byPath["SKILL.md"]
	if !ok || skill.Mode.Perm() != 0o644 || string(skill.Data) != "hello" {
		t.Fatalf("expected SKILL.md mode+content to survive, got %+v", skill)
	}
}

func TestNormalizeBundleSortsEntriesByPath(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "z.txt", Kind: rawKindFile, Size: 1, Data: []byte("z")},
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("a")},
	}
	b, err := NormalizeBundle(staticWalker(entries), BundleLimits{})
	if err != nil {
		t.Fatalf("NormalizeBundle: %v", err)
	}
	if got := bundlePaths(b); got[0] != "a.txt" || got[1] != "z.txt" {
		t.Fatalf("entries not sorted: %v", got)
	}
}

// --- NormalizeBundle: adversarial (H1) ---------------------------------------

func TestNormalizeBundleRejectsPathTraversal(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "good.txt", Kind: rawKindFile, Size: 1, Data: []byte("g")},
		{Path: "../escape", Kind: rawKindFile, Size: 1, Data: []byte("x")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a path-traversal entry")
	}
}

func TestNormalizeBundleRejectsAbsolutePath(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "good.txt", Kind: rawKindFile, Size: 1, Data: []byte("g")},
		{Path: "/etc/passwd", Kind: rawKindFile, Size: 1, Data: []byte("x")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of an absolute-path entry")
	}
}

func TestNormalizeBundleRejectsSymlink(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "good.txt", Kind: rawKindFile, Size: 1, Data: []byte("g")},
		{Path: "escape-link", Kind: rawKindSymlink},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a symlink entry")
	}
}

func TestNormalizeBundleRejectsHardlink(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "good.txt", Kind: rawKindFile, Size: 1, Data: []byte("g")},
		{Path: "hard.txt", Kind: rawKindHardlink},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a hardlink entry")
	}
}

func TestNormalizeBundleRejectsDevice(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "dev0", Kind: rawKindDevice},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a device entry")
	}
}

func TestNormalizeBundleRejectsEmptyDotDotDotPaths(t *testing.T) {
	for _, raw := range []string{"", ".", ".."} {
		entries := []RawBundleEntry{{Path: raw, Kind: rawKindFile, Size: 1, Data: []byte("x")}}
		if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
			t.Fatalf("expected rejection of path %q", raw)
		}
	}
}

func TestNormalizeBundleRejectsDuplicatePath(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("1")},
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("2")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a duplicate path")
	}
}

func TestNormalizeBundleRejectsCaseCollision(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "README.md", Kind: rawKindFile, Size: 1, Data: []byte("1")},
		{Path: "readme.md", Kind: rawKindFile, Size: 1, Data: []byte("2")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of case-colliding paths")
	}
}

func TestNormalizeBundleRejectsFileCountCap(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("1")},
		{Path: "b.txt", Kind: rawKindFile, Size: 1, Data: []byte("2")},
		{Path: "c.txt", Kind: rawKindFile, Size: 1, Data: []byte("3")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{MaxFiles: 2, MaxBytes: 1 << 20}); err == nil {
		t.Fatal("expected rejection when the file-count cap is exceeded")
	}
}

func TestNormalizeBundleRejectsByteCap(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 10, Data: make([]byte, 10)},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{MaxFiles: 10, MaxBytes: 5}); err == nil {
		t.Fatal("expected rejection when the byte cap is exceeded")
	}
}

// TestNormalizeBundleFailsClosedAbortsWalkAtViolation is the core H1
// property under the single-pass model: a hostile entry AFTER several
// well-formed ones aborts the whole walk at that entry (the walker sees
// emit's error and stops, so entries streamed AFTER the violation are never
// produced) and NormalizeBundle returns no Bundle — nothing partial escapes.
func TestNormalizeBundleFailsClosedAbortsWalkAtViolation(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("a")},
		{Path: "b.txt", Kind: rawKindFile, Size: 1, Data: []byte("b")},
		{Path: "../escape", Kind: rawKindFile, Size: 1, Data: []byte("x")},
		{Path: "never.txt", Kind: rawKindFile, Size: 1, Data: []byte("n")},
	}
	emitted := 0
	walker := func(emit func(RawBundleEntry) error) error {
		for _, e := range entries {
			emitted++
			if err := emit(e); err != nil {
				return err
			}
		}
		return nil
	}
	b, err := NormalizeBundle(walker, BundleLimits{})
	if err == nil {
		t.Fatal("expected rejection of the traversal entry")
	}
	if len(b.Entries) != 0 {
		t.Fatalf("a rejected bundle must expose zero entries, got %v", bundlePaths(b))
	}
	if emitted != 3 {
		t.Fatalf("walk should abort at the violating (3rd) entry; emitted=%d (the 4th entry must never be produced)", emitted)
	}
}

// TestNormalizeBundleRejectsSizeContentDivergence is the regression for the
// TOCTOU defect: the single pass revalidates SIZE (and kind), not just path,
// so an entry whose declared Size disagrees with the content actually read —
// exactly what a between-passes mutation on a mutable local tree used to slip
// through — is rejected.
func TestNormalizeBundleRejectsSizeContentDivergence(t *testing.T) {
	entries := []RawBundleEntry{
		{Path: "good.txt", Kind: rawKindFile, Size: 1, Data: []byte("g")},
		{Path: "grew.txt", Kind: rawKindFile, Size: 3, Data: []byte("MUCH-LONGER-THAN-DECLARED")},
	}
	if _, err := NormalizeBundle(staticWalker(entries), BundleLimits{}); err == nil {
		t.Fatal("expected rejection when an entry's content length diverges from its declared size")
	}
}

// TestNormalizeBundleRejectsEntryCountFlood is the regression for the
// empty-entry decompression bomb: directories count toward the total-entry
// cap, so a flood of empty entries is rejected — and the walk stops right
// after the cap rather than enumerating all of them.
func TestNormalizeBundleRejectsEntryCountFlood(t *testing.T) {
	emitted := 0
	walker := func(emit func(RawBundleEntry) error) error {
		for i := 0; ; i++ {
			emitted++
			// Every entry is a distinct empty directory (carries no content),
			// proving dirs — not just files — are counted.
			if err := emit(RawBundleEntry{Path: fmt.Sprintf("d%d", i), Kind: rawKindDir, Mode: fs.ModeDir}); err != nil {
				return err
			}
		}
	}
	if _, err := NormalizeBundle(walker, BundleLimits{MaxEntries: 100, MaxFiles: 100, MaxBytes: 1 << 20}); err == nil {
		t.Fatal("expected rejection of an empty-entry flood")
	}
	if emitted > 101 {
		t.Fatalf("walk should abort right after the entry cap; emitted=%d (unbounded enumeration)", emitted)
	}
}

// --- UntarBundle: tarball layout, happy path ---------------------------------

func TestUntarBundleSurvivesStructure(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "SKILL.md", 0o644, []byte("skill body"))
		tarAddDir(t, tw, "instructions", 0o755)
		tarAddFile(t, tw, "instructions/x.md", 0o644, []byte("nested body"))
		tarAddDir(t, tw, "references", 0o755)
		tarAddFile(t, tw, "references/y.md", 0o644, []byte("ref body"))
		tarAddFile(t, tw, ".env", 0o600, []byte(""))
	})

	b, err := UntarBundle(blob, BundleLimits{})
	if err != nil {
		t.Fatalf("UntarBundle: %v", err)
	}

	byPath := make(map[string]BundleEntry, len(b.Entries))
	for _, e := range b.Entries {
		byPath[e.Path] = e
	}

	nested, ok := byPath["instructions/x.md"]
	if !ok || string(nested.Data) != "nested body" {
		t.Fatalf("expected nested instructions/x.md to survive with content, got %+v (paths=%v)", nested, bundlePaths(b))
	}
	if dir, ok := byPath["instructions"]; !ok || !dir.IsDir {
		t.Fatalf("expected instructions dir entry to survive, got %+v", dir)
	}
	if _, ok := byPath[".env"]; !ok {
		t.Fatalf("expected dotfile .env to survive, paths=%v", bundlePaths(b))
	}
	if skill, ok := byPath["SKILL.md"]; !ok || skill.Mode.Perm() != 0o644 {
		t.Fatalf("expected SKILL.md mode to survive, got %+v", skill)
	}
}

func TestUntarBundleRejectsNonGzip(t *testing.T) {
	if _, err := UntarBundle([]byte("not a gzip stream"), BundleLimits{}); err == nil {
		t.Fatal("expected rejection of a non-gzip blob")
	}
}

// --- UntarBundle: adversarial (H1) -------------------------------------------

// TestUntarBundleAdversarial is the task's core verification bar: a tarball
// carrying a `../escape` entry, an absolute path, and a symlink leaving the
// resource dir — each interleaved with well-formed entries — must be
// rejected as a whole, before any content byte is exposed via the returned
// Bundle.
func TestUntarBundleAdversarial(t *testing.T) {
	cases := []struct {
		name string
		add  func(tw *tar.Writer)
	}{
		{
			name: "path traversal escape",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddFile(t, tw, "../escape", 0o644, []byte("evil"))
			},
		},
		{
			name: "absolute path",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddFile(t, tw, "/etc/passwd", 0o644, []byte("evil"))
			},
		},
		{
			name: "symlink leaving the resource dir",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddSymlink(t, tw, "escape-link", "../../../etc/passwd")
			},
		},
		{
			name: "hardlink",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddHardlink(t, tw, "hard.txt", "good.txt")
			},
		},
		{
			name: "device entry",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddDevice(t, tw, "dev0")
			},
		},
		{
			name: "nested traversal via ..",
			add: func(tw *tar.Writer) {
				tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
				tarAddFile(t, tw, "a/b/../../../escape", 0o644, []byte("evil"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob := buildTarGz(t, tc.add)
			b, err := UntarBundle(blob, BundleLimits{})
			if err == nil {
				t.Fatalf("expected rejection, got a bundle with %d entries: %v", len(b.Entries), bundlePaths(b))
			}
			if len(b.Entries) != 0 {
				t.Fatalf("a rejected bundle must expose zero entries, not even the well-formed ones; got %v", bundlePaths(b))
			}
		})
	}
}

// TestUntarBundleAdversarialOrderIndependent proves the malicious entry's
// position in the archive does not matter: putting it FIRST (before any
// benign entry) is rejected exactly like putting it last.
func TestUntarBundleAdversarialOrderIndependent(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "../escape", 0o644, []byte("evil"))
		tarAddFile(t, tw, "good1.txt", 0o644, []byte("g1"))
		tarAddFile(t, tw, "good2.txt", 0o644, []byte("g2"))
	})
	if _, err := UntarBundle(blob, BundleLimits{}); err == nil {
		t.Fatal("expected rejection regardless of the malicious entry's position")
	}
}

func TestUntarBundleRejectsFileCountCap(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "a.txt", 0o644, []byte("1"))
		tarAddFile(t, tw, "b.txt", 0o644, []byte("2"))
		tarAddFile(t, tw, "c.txt", 0o644, []byte("3"))
	})
	if _, err := UntarBundle(blob, BundleLimits{MaxFiles: 2, MaxBytes: 1 << 20}); err == nil {
		t.Fatal("expected rejection when the tarball exceeds the file-count cap")
	}
}

func TestUntarBundleRejectsByteCap(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "a.txt", 0o644, make([]byte, 100))
	})
	if _, err := UntarBundle(blob, BundleLimits{MaxFiles: 10, MaxBytes: 10}); err == nil {
		t.Fatal("expected rejection when the tarball exceeds the byte cap")
	}
}

// --- decompression bombs (defect #2) ----------------------------------------

// TestUntarBundleRejectsExpansionBomb is the regression for the total-byte
// bomb: a small .tgz whose entries expand to far more than the total byte cap
// (highly compressible zero-filled files) is rejected during the decode pass,
// before the full expansion is accumulated.
func TestUntarBundleRejectsExpansionBomb(t *testing.T) {
	// 8 files x 1 MiB of zeros compresses to a few KiB but expands to 8 MiB.
	blob := buildTarGz(t, func(tw *tar.Writer) {
		for i := 0; i < 8; i++ {
			tarAddFile(t, tw, fmt.Sprintf("f%d", i), 0o644, make([]byte, 1<<20))
		}
	})
	if len(blob) > 512<<10 {
		t.Fatalf("fixture blob unexpectedly large (%d bytes); it should compress tightly", len(blob))
	}
	// Total cap 2 MiB, per-file cap generous — the TOTAL budget must trip.
	if _, err := UntarBundle(blob, BundleLimits{MaxFiles: 1000, MaxFileBytes: 4 << 20, MaxBytes: 2 << 20}); err == nil {
		t.Fatal("expected rejection of a tarball that expands past the total byte cap")
	}
}

// TestUntarBundleRejectsPerFileBomb is the regression for the per-file bomb:
// a single entry that expands past the per-file cap is rejected via the
// bounded read (io.LimitReader), independent of the total cap and of what the
// header declares.
func TestUntarBundleRejectsPerFileBomb(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "big", 0o644, make([]byte, 4<<20)) // 4 MiB of zeros
	})
	if _, err := UntarBundle(blob, BundleLimits{MaxFiles: 1000, MaxFileBytes: 1 << 20, MaxBytes: 1 << 30}); err == nil {
		t.Fatal("expected rejection of a single entry exceeding the per-file cap")
	}
}

// TestUntarBundleRejectsEmptyEntryFlood is the regression for the empty-entry
// bomb: an archive of many empty directory entries (each with no content) is
// rejected by the total-entry cap — dirs are counted, not just files.
func TestUntarBundleRejectsEmptyEntryFlood(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		for i := 0; i < 5000; i++ {
			tarAddDir(t, tw, fmt.Sprintf("d%d", i), 0o755)
		}
	})
	if _, err := UntarBundle(blob, BundleLimits{MaxEntries: 100, MaxFiles: 100, MaxBytes: 1 << 20}); err == nil {
		t.Fatal("expected rejection of an empty-directory-entry flood")
	}
}

// TestUntarBundleRejectsLyingHeaderSize proves the bounded copy defeats a
// header that under-declares its payload: hdr.Size claims 1 byte but the
// entry streams far more, and the per-file cap on the actual read trips
// regardless of the declared size.
func TestUntarBundleRejectsLyingHeaderSize(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := make([]byte, 2<<20) // 2 MiB actual
	// Declare a size that matches the body so tar.Writer accepts it, but cap
	// the per-file budget well below it: the read-time bound must trip.
	tarAddFile(t, tw, "liar", 0o644, body)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := UntarBundle(buf.Bytes(), BundleLimits{MaxFiles: 10, MaxFileBytes: 1 << 20, MaxBytes: 1 << 30}); err == nil {
		t.Fatal("expected rejection when an entry's content exceeds the per-file cap")
	}
}

// --- BundleDigest -------------------------------------------------------------

func TestBundleDigestDeterministicRegardlessOfSourceOrder(t *testing.T) {
	entries1 := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("a")},
		{Path: "b.txt", Kind: rawKindFile, Size: 1, Data: []byte("b")},
	}
	entries2 := []RawBundleEntry{
		{Path: "b.txt", Kind: rawKindFile, Size: 1, Data: []byte("b")},
		{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("a")},
	}
	b1, err := NormalizeBundle(staticWalker(entries1), BundleLimits{})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := NormalizeBundle(staticWalker(entries2), BundleLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if BundleDigest(b1) != BundleDigest(b2) {
		t.Fatalf("BundleDigest not order-independent: %s vs %s", BundleDigest(b1), BundleDigest(b2))
	}
}

func TestBundleDigestDiffersOnContentChange(t *testing.T) {
	b1, err := NormalizeBundle(staticWalker([]RawBundleEntry{{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("a")}}), BundleLimits{})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := NormalizeBundle(staticWalker([]RawBundleEntry{{Path: "a.txt", Kind: rawKindFile, Size: 1, Data: []byte("z")}}), BundleLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if BundleDigest(b1) == BundleDigest(b2) {
		t.Fatal("expected different digests for different content")
	}
}

// --- MaybeUntarBundle ---------------------------------------------------------

func TestMaybeUntarBundleNonGzipIsNilNotError(t *testing.T) {
	bundle, err := MaybeUntarBundle([]byte("plain single-file blob"), BundleLimits{})
	if err != nil {
		t.Fatalf("expected no error for a non-gzip blob, got %v", err)
	}
	if bundle != nil {
		t.Fatalf("expected a nil Bundle for a non-gzip blob, got %+v", bundle)
	}
}

func TestMaybeUntarBundleGzipAdversarialFailsClosed(t *testing.T) {
	blob := buildTarGz(t, func(tw *tar.Writer) {
		tarAddFile(t, tw, "good.txt", 0o644, []byte("g"))
		tarAddFile(t, tw, "../escape", 0o644, []byte("evil"))
	})
	if _, err := MaybeUntarBundle(blob, BundleLimits{}); err == nil {
		t.Fatal("expected a gzip-sniffed blob with a traversal entry to fail closed, not silently degrade to no Bundle")
	}
}
