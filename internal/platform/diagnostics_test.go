package platform

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// TestDiagnosticsTypes exercises the zero-value and populated forms of every
// type in diagnostics.go so the type definitions themselves are covered and
// regressions to field order/tags break the build, not silently.
func TestDiagnosticsTypes(t *testing.T) {
	t.Run("BrokenLink", func(t *testing.T) {
		var zero BrokenLink
		if zero.PlatformID != "" || zero.LinkPath != "" || zero.Dest != "" || zero.DisplayDest != "" {
			t.Fatalf("zero BrokenLink has non-empty fields: %+v", zero)
		}
		full := BrokenLink{PlatformID: "cursor", LinkPath: "/r/.cursor/rules/a.mdc", Dest: "/x/a.md", DisplayDest: "~/x/a.md"}
		if full.PlatformID != "cursor" || full.LinkPath == "" || full.Dest == "" || full.DisplayDest == "" {
			t.Fatalf("populated BrokenLink lost fields: %+v", full)
		}
	})

	t.Run("PlatformBadge", func(t *testing.T) {
		var zero PlatformBadge
		if zero.Name != "" || zero.Present || zero.Broken {
			t.Fatalf("zero PlatformBadge has non-empty fields: %+v", zero)
		}
		full := PlatformBadge{Name: "Cursor", Present: true, Broken: true}
		if full.Name != "Cursor" || !full.Present || !full.Broken {
			t.Fatalf("populated PlatformBadge lost fields: %+v", full)
		}
	})

	t.Run("OrphanCanonical", func(t *testing.T) {
		var zero OrphanCanonical
		if zero.Name != "" || zero.DisplayNote != "" {
			t.Fatalf("zero OrphanCanonical has non-empty fields: %+v", zero)
		}
		full := OrphanCanonical{Name: "reviewer", DisplayNote: "  (mis-pointed: /x)"}
		if full.Name == "" || full.DisplayNote == "" {
			t.Fatalf("populated OrphanCanonical lost fields: %+v", full)
		}
	})

	t.Run("SingleFileLinkSpec", func(t *testing.T) {
		var zero SingleFileLinkSpec
		if zero.LinkPath != "" || zero.CanonicalPaths != nil {
			t.Fatalf("zero SingleFileLinkSpec has non-empty fields: %+v", zero)
		}
		full := SingleFileLinkSpec{LinkPath: "/r/x", CanonicalPaths: []string{"/y", "/z"}}
		if full.LinkPath == "" || len(full.CanonicalPaths) != 2 {
			t.Fatalf("populated SingleFileLinkSpec lost fields: %+v", full)
		}
	})
}

// TestDiagnosticsInterfaces compile-time-checks every sister interface is a
// valid Go interface and that a single stub can implement the union. This
// guards against accidental method-signature drift (e.g. dropping a return).
func TestDiagnosticsInterfaces(t *testing.T) {
	var (
		_ BrokenLinkReporter      = stubDiagnosable{}
		_ LinkCounter             = stubDiagnosable{}
		_ StatusBadger            = stubDiagnosable{}
		_ UserConfigReporter      = stubDiagnosable{}
		_ OrphanCanonicalReporter = stubDiagnosable{}
		_ AuditPrinter            = stubDiagnosable{}
	)
	// Exercise each method so the stub itself counts toward coverage of the
	// interface shapes (Go's type system handles the rest at compile time).
	s := stubDiagnosable{}
	_ = s.BrokenLinks("", "", "")
	_, _ = s.CountLinks("", "", "")
	_ = s.Badge("", "", "")
	_ = s.UserBrokenLinks("")
	_ = s.UserBadge("")
	_ = s.OrphanCanonicals("", "", "", "")
	s.PrintAudit(io.Discard, "", "", "")
}

// stubDiagnosable is a no-op union implementor used only for the
// interface-conformance smoke check above.
type stubDiagnosable struct{}

func (stubDiagnosable) BrokenLinks(string, string, string) []BrokenLink { return nil }
func (stubDiagnosable) CountLinks(string, string, string) (int, int)    { return 0, 0 }
func (stubDiagnosable) Badge(string, string, string) PlatformBadge      { return PlatformBadge{} }
func (stubDiagnosable) UserBrokenLinks(string) []BrokenLink             { return nil }
func (stubDiagnosable) UserBadge(string) PlatformBadge                  { return PlatformBadge{} }
func (stubDiagnosable) OrphanCanonicals(string, string, string, string) []OrphanCanonical {
	return nil
}
func (stubDiagnosable) PrintAudit(io.Writer, string, string, string) {}

// TestScanSingleFileLinks_HardlinkedHealthy verifies that a link
// hard-linked to one of the canonical sources is reported healthy
// (nothing returned).
func TestScanSingleFileLinks_HardlinkedHealthy(t *testing.T) {
	dir := t.TempDir()
	canonical := filepath.Join(dir, "src.json")
	if err := os.WriteFile(canonical, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.json")
	if err := os.Link(canonical, link); err != nil {
		t.Fatal(err)
	}

	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: link, CanonicalPaths: []string{canonical}},
	})
	if len(got) != 0 {
		t.Fatalf("expected no broken, got %+v", got)
	}
}

// TestScanSingleFileLinks_FallbackCanonicalHealthy proves that the .mdc → .md
// fallback contract is honored: a link hard-linked to the SECOND canonical
// candidate also counts as healthy.
func TestScanSingleFileLinks_FallbackCanonicalHealthy(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary.mdc")
	fallback := filepath.Join(dir, "fallback.md")
	if err := os.WriteFile(fallback, []byte("# rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "rules", "a.mdc")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(fallback, link); err != nil {
		t.Fatal(err)
	}

	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: link, CanonicalPaths: []string{primary, fallback}},
	})
	if len(got) != 0 {
		t.Fatalf("fallback canonical should be healthy, got %+v", got)
	}
}

// TestScanSingleFileLinks_AbsentLinkSkipped verifies that a missing link
// path is silently skipped — absent != broken.
func TestScanSingleFileLinks_AbsentLinkSkipped(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: missing, CanonicalPaths: []string{filepath.Join(dir, "y")}},
	})
	if len(got) != 0 {
		t.Fatalf("absent link must produce no diagnostic, got %+v", got)
	}
}

// TestScanSingleFileLinks_BrokenSymlinkReported verifies that a resolvable
// managed link whose target is gone surfaces as a broken entry with the
// raw target preserved.
func TestScanSingleFileLinks_BrokenSymlinkReported(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	dir := t.TempDir()
	missingTarget := filepath.Join(dir, "vanished.json")
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(missingTarget, link); err != nil {
		t.Fatal(err)
	}

	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: link, CanonicalPaths: []string{filepath.Join(dir, "expected.json")}},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 broken entry, got %d (%+v)", len(got), got)
	}
	if got[0].LinkPath != link {
		t.Errorf("LinkPath = %q, want %q", got[0].LinkPath, link)
	}
	if got[0].Dest != missingTarget {
		t.Errorf("Dest = %q, want raw target %q", got[0].Dest, missingTarget)
	}
	if got[0].DisplayDest == "" {
		t.Error("DisplayDest should be populated even for broken symlinks")
	}
}

// TestScanSingleFileLinks_MispointedSymlinkReported covers the rule "link
// resolvable AND target exists but matches no canonical → broken".
func TestScanSingleFileLinks_MispointedSymlinkReported(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	dir := t.TempDir()
	wrongTarget := filepath.Join(dir, "wrong.json")
	if err := os.WriteFile(wrongTarget, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(wrongTarget, link); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(dir, "right.json")
	if err := os.WriteFile(canonical, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: link, CanonicalPaths: []string{canonical}},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 broken (mis-pointed) entry, got %d (%+v)", len(got), got)
	}
	if got[0].Dest != wrongTarget {
		t.Errorf("Dest = %q, want %q", got[0].Dest, wrongTarget)
	}
}

// TestScanSingleFileLinks_PlainFileUnreported guards the contract that a
// plain regular file at LinkPath (not hard-linked to any canonical, not a
// symlink) is silently skipped — it is "unmanaged user content", not the
// helper's concern.
func TestScanSingleFileLinks_PlainFileUnreported(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "user.json")
	if err := os.WriteFile(link, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(dir, "src.json")
	if err := os.WriteFile(canonical, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := ScanSingleFileLinks([]SingleFileLinkSpec{
		{LinkPath: link, CanonicalPaths: []string{canonical}},
	})
	if len(got) != 0 {
		t.Fatalf("plain file should be unreported by helper, got %+v", got)
	}
}

// TestScanSingleFileLinks_EmptyInput sanity-checks the nil-output contract.
func TestScanSingleFileLinks_EmptyInput(t *testing.T) {
	if got := ScanSingleFileLinks(nil); got != nil {
		t.Errorf("nil input should return nil, got %+v", got)
	}
	if got := ScanSingleFileLinks([]SingleFileLinkSpec{}); got != nil {
		t.Errorf("empty input should return nil, got %+v", got)
	}
}

// TestScanSymlinkDir_AbsentDirIsZero confirms a missing dir is "not
// present" rather than "error" — matches doctor/status behavior today.
func TestScanSymlinkDir_AbsentDirIsZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	ok, broken, brokenLinks := ScanSymlinkDir(dir)
	if ok != 0 || broken != 0 || brokenLinks != nil {
		t.Errorf("absent dir should return (0,0,nil), got (%d,%d,%+v)", ok, broken, brokenLinks)
	}
}

// TestScanSymlinkDir_EmptyDir confirms an empty existing dir is reported
// with all-zero counts and no broken entries.
func TestScanSymlinkDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	ok, broken, brokenLinks := ScanSymlinkDir(dir)
	if ok != 0 || broken != 0 || brokenLinks != nil {
		t.Errorf("empty dir should return (0,0,nil), got (%d,%d,%+v)", ok, broken, brokenLinks)
	}
}

// TestScanSymlinkDir_HealthyAndBrokenClassified is the central positive
// case: one healthy symlink, one broken symlink, one plain file (ignored
// — not a managed link). Expect ok=1, broken=1, brokenLinks has one entry.
func TestScanSymlinkDir_HealthyAndBrokenClassified(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	root := t.TempDir()
	target := filepath.Join(root, "real-target.md")
	if err := os.WriteFile(target, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "scan-me")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	healthy := filepath.Join(dir, "healthy.md")
	if err := os.Symlink(target, healthy); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.md")
	if err := os.Symlink(filepath.Join(root, "vanished.md"), broken); err != nil {
		t.Fatal(err)
	}
	// Plain file — not a managed link, must be ignored by the helper.
	plain := filepath.Join(dir, "plain.md")
	if err := os.WriteFile(plain, []byte("plain\n"), 0644); err != nil {
		t.Fatal(err)
	}

	gotOK, gotBroken, brokenLinks := ScanSymlinkDir(dir)
	if gotOK != 1 {
		t.Errorf("ok = %d, want 1", gotOK)
	}
	if gotBroken != 1 {
		t.Errorf("broken = %d, want 1", gotBroken)
	}
	if len(brokenLinks) != 1 {
		t.Fatalf("brokenLinks = %+v, want one entry", brokenLinks)
	}
	if brokenLinks[0].LinkPath != broken {
		t.Errorf("brokenLinks[0].LinkPath = %q, want %q", brokenLinks[0].LinkPath, broken)
	}
	if brokenLinks[0].Dest == "" {
		t.Error("brokenLinks[0].Dest should be populated with raw target")
	}
	if !strings.Contains(brokenLinks[0].DisplayDest, "vanished") {
		t.Errorf("brokenLinks[0].DisplayDest = %q, want it to contain 'vanished'", brokenLinks[0].DisplayDest)
	}
}

// TestAbsolutizeDest covers all three branches — empty pass-through,
// already-absolute pass-through, and relative-to-link-dir resolution.
// Uses t.TempDir() so the absolute branch is exercised with a platform-
// correct absolute path (Windows treats "/abs/target" as relative because
// it lacks a drive letter).
func TestAbsolutizeDest(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	absTarget := filepath.Join(dir, "abs", "target")

	if got := absolutizeDest(linkPath, ""); got != "" {
		t.Errorf("empty dest should pass through, got %q", got)
	}
	if got := absolutizeDest(linkPath, absTarget); got != absTarget {
		t.Errorf("absolute dest should pass through, got %q want %q", got, absTarget)
	}
	got := absolutizeDest(linkPath, filepath.Join("rel", "target"))
	want := filepath.Clean(filepath.Join(dir, "rel", "target"))
	if got != want {
		t.Errorf("relative dest = %q, want %q", got, want)
	}
}

// TestClassifyManagedLink_NotALink confirms the classifier returns the
// not-a-link state for a plain regular file.
func TestClassifyManagedLink_NotALink(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("plain\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, raw := classifyManagedLink(plain)
	if state != linkStateNotALink || raw != "" {
		t.Errorf("plain file = (%v, %q), want (linkStateNotALink, '')", state, raw)
	}
}

// TestClassifyManagedLink_Healthy confirms a resolvable symlink with an
// existing target is reported as linkStateHealthy with the raw target
// returned for round-trip use.
func TestClassifyManagedLink_Healthy(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("# x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	state, raw := classifyManagedLink(link)
	if state != linkStateHealthy || raw != target {
		t.Errorf("got (%v, %q), want (linkStateHealthy, %q)", state, raw, target)
	}
}

// TestClassifyManagedLink_Broken confirms a resolvable symlink whose
// target is missing is reported as linkStateBroken with the raw target
// preserved.
func TestClassifyManagedLink_Broken(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	dir := t.TempDir()
	link := filepath.Join(dir, "broken.md")
	missing := filepath.Join(dir, "gone.md")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	state, raw := classifyManagedLink(link)
	if state != linkStateBroken || raw != missing {
		t.Errorf("got (%v, %q), want (linkStateBroken, %q)", state, raw, missing)
	}
}
