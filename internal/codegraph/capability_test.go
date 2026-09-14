package codegraph

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureLanguages is the generated release fixture the embedded copy shadows.
const fixtureLanguages = "../../testdata/crg-release/v2.3.8/languages.json"

// embeddedLanguages is the copy go:embed can actually reach.
const embeddedLanguages = "contract/v2.3.8/languages.json"

// TestEmbeddedLanguageContractMatchesFixture is the anti-drift gate. Routing is
// decided at runtime from the embedded copy while the release contract lives in
// testdata; if the two ever diverge the diagnostic would silently answer from a
// stale language inventory.
func TestEmbeddedLanguageContractMatchesFixture(t *testing.T) {
	generated, err := os.ReadFile(fixtureLanguages)
	if err != nil {
		t.Fatalf("read generated fixture: %v", err)
	}
	embedded, err := languageContractFS.ReadFile(embeddedLanguages)
	if err != nil {
		t.Fatalf("read embedded contract: %v", err)
	}
	if !bytes.Equal(generated, embedded) {
		t.Fatalf("embedded %s drifted from %s (%d vs %d bytes); re-copy the generated fixture",
			embeddedLanguages, fixtureLanguages, len(embedded), len(generated))
	}
}

// capabilityRepo materializes empty files at the given repo-relative paths.
// Only the file NAMES matter to extension classification, so no content is
// written; use capabilityRepoContent when a file's bytes are the point.
func capabilityRepo(t *testing.T, paths ...string) string {
	t.Helper()
	return capabilityRepoContent(t, nil, paths...)
}

// capabilityRepoContent materializes paths, writing content[rel] for any path
// that has an entry and an empty file otherwise.
func capabilityRepoContent(t *testing.T, content map[string]string, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content[rel]), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// languageRows indexes a report's per-language rows by language name.
func languageRows(report CapabilityReport) map[string]SourceCapability {
	out := make(map[string]SourceCapability, len(report.Languages))
	for _, lang := range report.Languages {
		out[lang.Language] = lang
	}
	return out
}

// assertMixedRepositoryLanguages checks the per-language rows ScanCapability
// reports for the mixed fixture: every expected row must be present with the
// expected counts and flags, no extra rows may appear, the rows must be sorted
// by language name, and the case-folded TypeScript extension must collapse to a
// single entry.
func assertMixedRepositoryLanguages(t *testing.T, report CapabilityReport) {
	t.Helper()

	rows := languageRows(report)
	// Declared in the order ScanCapability must return them: sorted by
	// language name, which puts the LanguageUnindexed bucket last.
	want := []SourceCapability{
		{Language: "go", Files: 2, Native: true, UpstreamSupported: true},
		{Language: "python", Files: 1, Native: false, UpstreamSupported: true},
		{Language: "rust", Files: 1, Native: false, UpstreamSupported: true},
		{Language: "typescript", Files: 2, Native: false, UpstreamSupported: true},
		{Language: LanguageUnindexed, Files: 1, Native: false, UpstreamSupported: false},
	}
	for _, w := range want {
		assertMixedRepositoryRow(t, rows, w, report.Languages)
	}
	if len(report.Languages) != len(want) {
		t.Fatalf("languages = %+v, want exactly %d rows", report.Languages, len(want))
	}
	for i, w := range want {
		if report.Languages[i].Language != w.Language {
			t.Fatalf("languages not sorted by name: %+v", report.Languages)
		}
	}
	if exts := rows["typescript"].Extensions; len(exts) != 1 || exts[0] != ".ts" {
		t.Fatalf("typescript extensions = %v, want the case-folded [.ts]", exts)
	}
}

// assertMixedRepositoryRow checks one expected per-language row: it must be
// present at all, and its file count, native flag and upstream-support flag
// must match. The full row set is passed only to report it on failure.
func assertMixedRepositoryRow(t *testing.T, rows map[string]SourceCapability, want SourceCapability, all []SourceCapability) {
	t.Helper()

	got, ok := rows[want.Language]
	if !ok {
		t.Fatalf("no %s row in %+v", want.Language, all)
	}
	if got.Files != want.Files || got.Native != want.Native || got.UpstreamSupported != want.UpstreamSupported {
		t.Fatalf("%s row = %+v, want files=%d native=%v upstream=%v",
			want.Language, got, want.Files, want.Native, want.UpstreamSupported)
	}
}

// assertMixedRepositoryRouting checks the repository-wide verdict for the mixed
// fixture: the native/bridge/unsupported totals, the FullyNative flag, the
// routing decision derived from it, and the bridged language list.
func assertMixedRepositoryRouting(t *testing.T, report CapabilityReport) {
	t.Helper()

	if report.NativeFiles != 2 || report.BridgeFiles != 4 || report.UnsupportedFiles != 1 {
		t.Fatalf("totals = native %d bridge %d unsupported %d, want 2/4/1",
			report.NativeFiles, report.BridgeFiles, report.UnsupportedFiles)
	}
	if report.FullyNative {
		t.Fatalf("FullyNative = true for a repository with %d bridge files", report.BridgeFiles)
	}
	if report.Routing() != RoutingBridge {
		t.Fatalf("Routing = %q, want %q", report.Routing(), RoutingBridge)
	}
	bridged := report.BridgeLanguages()
	if strings.Join(bridged, ",") != "python,rust,typescript" {
		t.Fatalf("BridgeLanguages = %v, want [python rust typescript]", bridged)
	}
}

func TestScanCapabilityClassifiesMixedRepository(t *testing.T) {
	root := capabilityRepo(t,
		"main.go",
		"internal/util.go",
		"scripts/deploy.py",
		"web/app.ts",
		"web/other.TS", // extension lookup is case-folded, as upstream's is
		"crates/lib.rs",
		"assets/payload.bin",
	)

	report, err := ScanCapability(root)
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if report.Root != root {
		t.Fatalf("Root = %q, want %q", report.Root, root)
	}

	assertMixedRepositoryLanguages(t, report)
	assertMixedRepositoryRouting(t, report)
}

func TestScanCapabilityGoOnlyRepositoryIsFullyNative(t *testing.T) {
	// README is indexed by neither side, so it must not affect routing.
	root := capabilityRepo(t, "main.go", "internal/x/x.go", "internal/x/x_test.go", "README")

	report, err := ScanCapability(root)
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if !report.FullyNative {
		t.Fatalf("FullyNative = false for a Go-only repository: %+v", report)
	}
	if report.NativeFiles != 3 || report.BridgeFiles != 0 || report.UnsupportedFiles != 1 {
		t.Fatalf("totals = native %d bridge %d unsupported %d, want 3/0/1",
			report.NativeFiles, report.BridgeFiles, report.UnsupportedFiles)
	}
	if report.Routing() != RoutingNative {
		t.Fatalf("Routing = %q, want %q", report.Routing(), RoutingNative)
	}
}

func TestScanCapabilityEmptyRepositoryIsNotFullyNative(t *testing.T) {
	report, err := ScanCapability(t.TempDir())
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if report.FullyNative {
		t.Fatal("FullyNative = true for an empty repository: there is nothing native to extract")
	}
	if len(report.Languages) != 0 || report.NativeFiles != 0 || report.BridgeFiles != 0 {
		t.Fatalf("empty repository report = %+v", report)
	}
	if report.Routing() != RoutingBridge {
		t.Fatalf("Routing = %q, want %q", report.Routing(), RoutingBridge)
	}
}

// TestScanCapabilitySkipsIngesterPrunedDirectories is the reason the diagnostic
// reuses skipDirEntry: a vendored, hidden or generated tree contributes nothing
// to the graph, so it must not route an otherwise-native repository to the
// Python bridge.
func TestScanCapabilitySkipsIngesterPrunedDirectories(t *testing.T) {
	root := capabilityRepo(t,
		"main.go",
		"vendor/dep/setup.py",
		"node_modules/pkg/index.ts",
		".venv/lib/site.py",
		"build/generated/gen.py",
		"dist/bundle.js",
		".git/hooks/pre-commit.sh",
		".hidden/tool.rb",
		".code-review-graph/probe.py",
	)

	report, err := ScanCapability(root)
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if report.BridgeFiles != 0 {
		t.Fatalf("pruned trees leaked into the report: %+v", report.Languages)
	}
	if !report.FullyNative {
		t.Fatalf("FullyNative = false despite every non-Go file being pruned: %+v", report)
	}
	if report.NativeFiles != 1 {
		t.Fatalf("NativeFiles = %d, want 1", report.NativeFiles)
	}
}

// TestScanCapabilityCountsShebangScriptsAsBridgeWork pins the one classification
// an extension cannot make. Verified against the pinned release: a repository
// of `main.go` plus an extension-less `#!/usr/bin/env python3` file is indexed
// by upstream as {go, python}, so calling it fully native would silently drop
// the script from the graph.
func TestScanCapabilityCountsShebangScriptsAsBridgeWork(t *testing.T) {
	root := capabilityRepoContent(t,
		map[string]string{
			"deploy":  "#!/usr/bin/env python3\ndef helper():\n    return 1\n",
			"LICENSE": "MIT\n",
		},
		"main.go", "deploy", "LICENSE",
	)

	report, err := ScanCapability(root)
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if report.FullyNative {
		t.Fatalf("an extension-less shebang script was treated as native: %+v", report)
	}
	if report.NativeFiles != 1 || report.BridgeFiles != 1 || report.UnsupportedFiles != 1 {
		t.Fatalf("totals = native %d bridge %d unsupported %d, want 1/1/1",
			report.NativeFiles, report.BridgeFiles, report.UnsupportedFiles)
	}
	rows := languageRows(report)
	script, ok := rows[LanguageShebangScript]
	if !ok {
		t.Fatalf("no %s row in %+v", LanguageShebangScript, report.Languages)
	}
	if script.Files != 1 || script.Native || !script.UpstreamSupported {
		t.Fatalf("%s row = %+v", LanguageShebangScript, script)
	}
	// LICENSE has no extension and no shebang: upstream's probe returns nil
	// for it too, so it must stay in the unindexed bucket.
	if unindexed := rows[LanguageUnindexed]; unindexed.Files != 1 {
		t.Fatalf("unindexed row = %+v, want the extension-less non-script file", unindexed)
	}
	if !strings.Contains(report.Reason(), LanguageShebangScript) {
		t.Fatalf("reason %q does not name the script bucket", report.Reason())
	}
}

func TestScanCapabilityReasonExplainsEachVerdict(t *testing.T) {
	mixed, err := ScanCapability(capabilityRepo(t, "main.go", "tool.py", "lib.rs"))
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	for _, want := range []string{"python", "rust"} {
		if !strings.Contains(mixed.Reason(), want) {
			t.Fatalf("reason %q does not name %q", mixed.Reason(), want)
		}
	}

	native, err := ScanCapability(capabilityRepo(t, "main.go"))
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	if !strings.Contains(native.Reason(), LanguageNative) {
		t.Fatalf("native reason %q does not name %q", native.Reason(), LanguageNative)
	}

	empty, err := ScanCapability(t.TempDir())
	if err != nil {
		t.Fatalf("ScanCapability: %v", err)
	}
	// Nothing was bridged, so the reason must blame the missing Go source
	// rather than invent an offending language.
	if strings.Contains(empty.Reason(), "code-review-graph") {
		t.Fatalf("empty repository blamed the bridge: %q", empty.Reason())
	}
	if !strings.Contains(empty.Reason(), LanguageNative) {
		t.Fatalf("empty reason %q does not name the missing native language", empty.Reason())
	}
}

func TestScanCapabilityRejectsMissingRoot(t *testing.T) {
	if _, err := ScanCapability(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("ScanCapability succeeded on a missing root")
	}
}

// TestScanCapabilityPropagatesWalkFailure covers the arm the per-entry error
// swallow cannot reach: a walk that fails outright must not be reported as an
// empty — and therefore silently bridge-routed — repository.
func TestScanCapabilityPropagatesWalkFailure(t *testing.T) {
	orig := walkDir
	t.Cleanup(func() { walkDir = orig })
	walkDir = func(string, fs.WalkDirFunc) error { return os.ErrPermission }

	if _, err := ScanCapability(t.TempDir()); err == nil {
		t.Fatal("ScanCapability swallowed a walk failure")
	}
}

// TestLanguageContractCoversTheNativeLanguage proves the native extension set
// is DERIVED from the pinned inventory rather than restated: ".go" resolves to
// LanguageNative through the same table every other extension goes through.
func TestLanguageContractCoversTheNativeLanguage(t *testing.T) {
	if got := upstream.Extensions[".go"]; got != LanguageNative {
		t.Fatalf("upstream maps .go to %q, want %q", got, LanguageNative)
	}
	if !nativeLanguages[LanguageNative] {
		t.Fatalf("%q is not in the native language set", LanguageNative)
	}
	if _, claimed := upstream.Extensions[""]; claimed {
		t.Fatal("the empty extension is claimed by a language; extension-less files would be misrouted")
	}
	if _, claimed := upstream.Languages[LanguageUnindexed]; claimed {
		t.Fatalf("%q collides with a real upstream language name", LanguageUnindexed)
	}
}
