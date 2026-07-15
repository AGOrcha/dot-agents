package config

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io/fs"
	"testing"
)

// --- test helpers ------------------------------------------------------------

// staticWalker returns a BundleWalker over a fixed entry list. It mimics the
// walker contract precisely: the metadata-only pass (readContent=false)
// never carries content, exercising NormalizeBundle's two-call contract even
// for a hand-built raw listing.
func staticWalker(entries []RawBundleEntry) BundleWalker {
	return func(readContent bool) ([]RawBundleEntry, error) {
		out := make([]RawBundleEntry, len(entries))
		copy(out, entries)
		if !readContent {
			for i := range out {
				out[i].Data = nil
			}
		}
		return out, nil
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

// TestNormalizeBundleFailsClosedBeforeContentRead is the core H1 property:
// a hostile entry AFTER several well-formed ones must reject the WHOLE
// bundle, and the content-reading pass must never even run — asserted here
// by a walker whose content-pass panics, proving NormalizeBundle never calls
// it once the metadata-only pass has already failed.
func TestNormalizeBundleFailsClosedBeforeContentRead(t *testing.T) {
	meta := []RawBundleEntry{
		{Path: "a.txt", Kind: rawKindFile, Size: 1},
		{Path: "b.txt", Kind: rawKindFile, Size: 1},
		{Path: "../escape", Kind: rawKindFile, Size: 1},
	}
	contentPassCalled := false
	walker := func(readContent bool) ([]RawBundleEntry, error) {
		if readContent {
			contentPassCalled = true
			t.Fatal("content-reading pass must not run when the metadata pass rejects the bundle")
		}
		out := make([]RawBundleEntry, len(meta))
		copy(out, meta)
		return out, nil
	}
	if _, err := NormalizeBundle(walker, BundleLimits{}); err == nil {
		t.Fatal("expected rejection")
	}
	if contentPassCalled {
		t.Fatal("content-reading pass ran despite a metadata-pass rejection")
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
