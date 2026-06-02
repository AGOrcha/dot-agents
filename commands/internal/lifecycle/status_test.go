package lifecycle

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/spf13/cobra"
)

// testStatusDeps returns a lifecycle.Deps suitable for status_test exercises:
// UsageError forwards to a fmt.Errorf so the statusNoArgsHint branch is
// reachable, and the *WithHints helpers return cobra-accepting validators
// that always succeed (we don't exercise positional-arg parsing in these
// tests beyond NewStatusCmd_FlagsAndArgs).
func testStatusDeps() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	usage := func(msg string, hints ...string) error {
		return fmt.Errorf("%s", msg)
	}
	return Deps{
		Flags:                 GlobalFlags{},
		ErrorWithHints:        func(msg string, hints ...string) error { return fmt.Errorf("%s", msg) },
		UsageError:            usage,
		MaximumNArgsWithHints: func(int, ...string) cobra.PositionalArgs { return accept },
		RangeArgsWithHints:    func(int, int, ...string) cobra.PositionalArgs { return accept },
		ExactArgsWithHints:    func(int, ...string) cobra.PositionalArgs { return accept },
	}
}

// jsonOff is the default jsonOutput closure: emit text mode.
func jsonOff() bool { return false }

// jsonOn forces JSON mode for the JSON-path tests.
func jsonOn() bool { return true }

// fakeStatusConfigLoader is the interface-DI test double for
// statusConfigLoader (per docs/TEST_SEAMS.md). A nil func field delegates
// to the real config.Load implementation.
type fakeStatusConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeStatusConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeStatusConfigLoader_NilDelegatesToReal pins the nil-delegates-to-real
// contract: a test that omits loadConfig must hit the real config.Load (not a
// silent no-op). Without this, future regressions in the fake's default
// branch could mask happy-path test failures.
func TestFakeStatusConfigLoader_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := (fakeStatusConfigLoader{}).LoadConfig()
	if err != nil {
		t.Fatalf("nil-loadConfig delegate: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected real config.Load result, got nil")
	}
}

// TestNewStatusCmd_RunEClosureWiresStdDeps drives status' RunE closure end
// to end so a regression that drops std deps wiring fails here rather than
// silently in production.
func TestNewStatusCmd_RunEClosureWiresStdDeps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := NewStatusCmd(testStatusDeps(), jsonOff)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE closure: %v", err)
	}
}

func TestNewStatusCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewStatusCmd(testStatusDeps(), jsonOff)
	if cmd.Use != "status" {
		t.Errorf("expected Use=status, got %q", cmd.Use)
	}
	if cmd.Flags().Lookup("audit") == nil {
		t.Error("missing --audit flag")
	}
	if cmd.Flags().Lookup("agent") == nil {
		t.Error("missing --agent flag")
	}
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("status takes no args, expected error")
	}
}

// ---------- probeAgentsHomeGit ----------

func TestProbeAgentsHomeGit_NonRepo(t *testing.T) {
	tmp := t.TempDir()
	g := probeAgentsHomeGit(tmp)
	if g.IsRepo {
		t.Error("expected IsRepo=false for non-git dir")
	}
}

func TestProbeAgentsHomeGit_BareGitDir(t *testing.T) {
	tmp := t.TempDir()
	// Create a fake .git dir (probe just checks for existence; we don't need a real repo
	// because the git CLI calls in this function ignore non-zero output gracefully).
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	g := probeAgentsHomeGit(tmp)
	if !g.IsRepo {
		t.Error("expected IsRepo=true when .git dir exists")
	}
}

// ---------- statusGitInfo ----------

func TestStatusGitInfo_EmptyWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	g := statusGitInfo(tmp)
	if g.Initialized {
		t.Error("expected Initialized=false on non-repo")
	}
}

// ---------- countPlatformHealth, platformStatus ----------

func TestCountPlatformHealth_NoneInputs(t *testing.T) {
	badge := countPlatformHealth(nil, nil)
	if badge.present || badge.broken {
		t.Errorf("expected zero-value badge, got %+v", badge)
	}
}

func TestCountPlatformHealth_ReportsHealthyFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "f.txt")
	os.WriteFile(f, []byte("x"), 0644)
	badge := countPlatformHealth([]string{f}, nil)
	if !badge.present || badge.broken {
		t.Errorf("expected present=true broken=false, got %+v", badge)
	}
}

func TestCountPlatformHealth_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "broken.txt")
	linktest.DanglingLink(t, link)
	badge := countPlatformHealth([]string{link}, nil)
	if !badge.broken {
		t.Errorf("expected broken=true, got %+v", badge)
	}
}

func TestPlatformStatusBuilder(t *testing.T) {
	got := platformStatus("X", platformBadge{present: true, broken: false})
	if got.Name != "X" || !got.Present || got.Broken {
		t.Errorf("unexpected platformStatus: %+v", got)
	}
}

func TestAppendPlatformIfPresent_SkipsAbsent(t *testing.T) {
	out := appendPlatformIfPresent(nil, "X", platformBadge{})
	if len(out) != 0 {
		t.Errorf("expected nothing appended for zero badge, got %+v", out)
	}
	out = appendPlatformIfPresent(nil, "Y", platformBadge{present: true})
	if len(out) != 1 || out[0].Name != "Y" {
		t.Errorf("expected single Y entry, got %+v", out)
	}
}

// ---------- pathExists ----------

func TestPathExists(t *testing.T) {
	tmp := t.TempDir()
	if !pathExists(tmp) {
		t.Error("expected pathExists=true for temp dir")
	}
	if pathExists(filepath.Join(tmp, "missing")) {
		t.Error("expected pathExists=false for missing path")
	}
}

// ---------- formatRefreshDisplay / readRefreshTimestamp ----------

func TestFormatRefreshDisplay(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"2026-03-12T05:18:11Z", "2026-03-12 05:18 UTC"},
		{"short", "short"},
	}
	for _, c := range cases {
		if got := formatRefreshDisplay(c.input); got != c.want {
			t.Errorf("formatRefreshDisplay(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestReadLegacyRefreshTimestamp(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, ".agents-refresh")
	os.WriteFile(marker, []byte("refreshed_at=2026-03-12T05:18:11Z\n"), 0644)
	got := readLegacyRefreshTimestamp(tmp)
	if got != "2026-03-12 05:18 UTC" {
		t.Errorf("got %q, want 2026-03-12 05:18 UTC", got)
	}
}

func TestReadLegacyRefreshTimestamp_NoFile(t *testing.T) {
	tmp := t.TempDir()
	if got := readLegacyRefreshTimestamp(tmp); got != "" {
		t.Errorf("expected empty for missing marker, got %q", got)
	}
}

// TestReadLegacyRefreshTimestamp_NoRefreshedAtLine covers the case where the
// marker file exists but contains no `refreshed_at=` prefix — the scanner
// loop falls through and returns "".
func TestReadLegacyRefreshTimestamp_NoRefreshedAtLine(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, ".agents-refresh")
	os.WriteFile(marker, []byte("# unrelated\nother=value\n"), 0644)
	if got := readLegacyRefreshTimestamp(tmp); got != "" {
		t.Errorf("expected empty when no refreshed_at line, got %q", got)
	}
}

// ---------- summarizeCanonicalBucket ----------

func TestSummarizeCanonicalBucket_Empty(t *testing.T) {
	tmp := t.TempDir()
	scopes, items := summarizeCanonicalBucket(filepath.Join(tmp, "missing"), false, "")
	if scopes != 0 || items != 0 {
		t.Errorf("expected (0,0) for missing root, got (%d,%d)", scopes, items)
	}
}

func TestSummarizeCanonicalBucket_CountsFiles(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "bucket")
	os.MkdirAll(filepath.Join(root, "scope1"), 0755)
	os.WriteFile(filepath.Join(root, "scope1", "a.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(root, "scope1", "b.json"), []byte("{}"), 0644)

	scopes, items := summarizeCanonicalBucket(root, false, "")
	if scopes != 1 || items != 2 {
		t.Errorf("expected (1,2), got (%d,%d)", scopes, items)
	}
}

func TestSummarizeCanonicalBucket_CountsMarkerDirs(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "bucket")
	skillA := filepath.Join(root, "scope1", "skill-a")
	skillB := filepath.Join(root, "scope1", "skill-b")
	os.MkdirAll(skillA, 0755)
	os.MkdirAll(skillB, 0755)
	os.WriteFile(filepath.Join(skillA, "SKILL.md"), []byte("ok"), 0644)
	// skillB intentionally missing marker

	scopes, items := summarizeCanonicalBucket(root, true, "SKILL.md")
	if scopes != 1 || items != 1 {
		t.Errorf("expected (1,1), got (%d,%d)", scopes, items)
	}
}

// ---------- addManagedCounts ----------

func TestAddManagedCounts_ReportsOKAndWarn(t *testing.T) {
	tmp := t.TempDir()
	regular := filepath.Join(tmp, "reg.txt")
	os.WriteFile(regular, []byte("x"), 0644)
	broken := filepath.Join(tmp, "broken.txt")
	linktest.DanglingLink(t, broken)

	ok, warn := 0, 0
	addManagedCounts(&ok, &warn, []string{regular, broken, filepath.Join(tmp, "missing")}, nil)
	if ok != 1 {
		t.Errorf("expected ok=1, got %d", ok)
	}
	if warn != 1 {
		t.Errorf("expected warn=1, got %d", warn)
	}
}

func TestCountManagedDirEntries_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "agents")
	os.MkdirAll(dir, 0755)
	linktest.DanglingLink(t, filepath.Join(dir, "ghost.md"))

	warn := 0
	got := countManagedDirEntries(dir, &warn)
	if got != 0 {
		t.Errorf("expected ok=0, got %d", got)
	}
	if warn != 1 {
		t.Errorf("expected warn=1, got %d", warn)
	}
}

func TestCountManagedDirEntries_MissingDir(t *testing.T) {
	warn := 0
	got := countManagedDirEntries("/no/such/path/xyz", &warn)
	if got != 0 || warn != 0 {
		t.Errorf("expected (0,0) for missing dir, got (%d, %d)", got, warn)
	}
}

// ---------- runStatus (text and JSON) ----------

func TestRunStatus_TextEmptyConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.Save()

	if err := runStatus(false, "", stdStatusConfigLoader{}, false); err != nil {
		t.Errorf("runStatus: %v", err)
	}
}

func TestRunStatus_JSONReportContainsProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	report, err := buildStatusJSONReport(cfg, agentsHome, "")
	if err != nil {
		t.Fatalf("buildStatusJSONReport: %v", err)
	}
	if report.AgentsHome != agentsHome {
		t.Errorf("expected AgentsHome=%q, got %q", agentsHome, report.AgentsHome)
	}
	if len(report.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(report.Projects))
	}
	if report.Projects[0].Name != "p" || report.Projects[0].Path != projectPath {
		t.Errorf("unexpected project entry: %+v", report.Projects[0])
	}
	// Ensure JSON marshaling round-trips
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"name":"p"`) {
		t.Errorf("expected JSON to mention project name, got: %s", string(data))
	}
}

func TestRunStatus_JSONFlagEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.Save()

	if err := runStatus(false, "", stdStatusConfigLoader{}, true); err != nil {
		t.Errorf("runStatus --json: %v", err)
	}
}

// ---------- collect{User,Project}PlatformsHelpers exist with empty home ----------

func TestCollectProjectPlatforms_StableLength(t *testing.T) {
	tmp := t.TempDir()
	got := collectProjectPlatforms("proj", tmp, t.TempDir())
	if len(got) != 5 {
		t.Errorf("expected 5 platforms (cursor/claude/codex/opencode/copilot), got %d", len(got))
	}
}

func TestCollectUserConfigPlatforms_FilterIsolation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// With no managed configs, collectUserConfigPlatforms returns nothing.
	if got := collectUserConfigPlatforms("claude"); len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
	if got := collectUserConfigPlatforms("codex"); len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
}

// ---------- printBadgeRow / per-platform Badge integration ----------
//
// After P3, the legacy cursorTextBadge / claudeTextBadge / countCursorRules /
// countClaudeRules helpers no longer live in this package — each platform
// owns its Badge + CountLinks via the StatusBadger / LinkCounter sister
// interfaces (see internal/platform/diagnostics.go). The lifecycle-side
// tests below preserve their original behavioral assertions by driving the
// same scenarios through collectProjectTextBadges (the iterator that
// replaced the per-platform inline helpers) and CountClaudeRules (the
// thin shim retained for the legacy seam).

// TestCollectProjectTextBadges_EmptyProject confirms the iterator returns one
// not-present, not-broken badge per platform when the project tree is empty.
// Replaces the prior TestCursorTextBadge_NoConfig / TestClaudeTextBadge_NoRules
// pair with one assertion that covers every platform's empty branch via the
// public iterator.
func TestCollectProjectTextBadges_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	got := collectProjectTextBadges("proj", tmp, agentsHome, nil)
	if len(got) != 5 {
		t.Fatalf("expected 5 badges, got %d (%+v)", len(got), got)
	}
	for _, b := range got {
		if b.present {
			t.Errorf("%s badge.present = true for empty project, want false", b.name)
		}
		if b.broken {
			t.Errorf("%s badge.broken = true for empty project, want false", b.name)
		}
	}
}

// TestCountClaudeRules_ReportsBrokenSymlinks exercises the lifecycle-side
// CountClaudeRules shim (kept exported for the legacy commands seams_test
// callers). The underlying classification logic now lives in the platform
// package; this test pins that the shim continues to surface
// (ok=0, warn=1) for a dangling .claude/rules symlink.
func TestCountClaudeRules_ReportsBrokenSymlinks(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, ".claude", "rules")
	os.MkdirAll(rulesDir, 0755)
	linktest.DanglingLink(t, filepath.Join(rulesDir, "missing.md"))

	ok, warn := CountClaudeRules(tmp)
	if ok != 0 || warn != 1 {
		t.Errorf("expected (0,1) for broken claude rules, got (%d,%d)", ok, warn)
	}
}

// TestCollectProjectTextBadges_CursorGlobalHardlink replaces
// TestCountCursorRules_GlobalHardlink: drives the cursor.CountLinks branch
// via the iterator and asserts the Cursor badge surfaces as
// present+not-broken when the project hosts a healthy global hardlink.
func TestCollectProjectTextBadges_CursorGlobalHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "rules", "global", "myrule.mdc")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("rule"), 0644)

	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)
	if err := os.Link(src, filepath.Join(rulesDir, "global--myrule.mdc")); err != nil {
		t.Fatal(err)
	}

	got := collectProjectTextBadges("proj", tmp, agentsHome, nil)
	cursor := findBadge(t, got, "Cursor")
	if !cursor.present || cursor.broken {
		t.Errorf("expected Cursor badge present+ok, got %+v", cursor)
	}
}

// TestCollectProjectTextBadges_CursorMDFallbackAndWarn covers the same .md
// fallback + warn-branch combination the prior TestCountCursorRules_*
// suite asserted: one healthy fallback link, one orphan (warn), plus
// background entries (non-global prefix, non-mdc, backup artifact) that
// must be ignored.
func TestCollectProjectTextBadges_CursorMDFallbackAndWarn(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")

	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)

	// Healthy via .md fallback: file on disk is global--foo.mdc but src is .md.
	srcMD := filepath.Join(agentsHome, "rules", "global", "foo.md")
	os.MkdirAll(filepath.Dir(srcMD), 0755)
	os.WriteFile(srcMD, []byte("md"), 0644)
	if err := os.Link(srcMD, filepath.Join(rulesDir, "global--foo.mdc")); err != nil {
		t.Fatal(err)
	}

	// Unlinked global rule → warn++ branch.
	os.WriteFile(filepath.Join(rulesDir, "global--orphan.mdc"), []byte("o"), 0644)

	// Non-global prefix (continue branch).
	os.WriteFile(filepath.Join(rulesDir, "proj--ignored.mdc"), []byte("p"), 0644)
	// Non-mdc (continue).
	os.WriteFile(filepath.Join(rulesDir, "notrule.txt"), []byte("x"), 0644)
	// Backup artifact (continue).
	os.WriteFile(filepath.Join(rulesDir, "global--x.mdc.dot-agents-backup"), []byte("x"), 0644)

	got := collectProjectTextBadges("proj", tmp, agentsHome, nil)
	cursor := findBadge(t, got, "Cursor")
	if !cursor.present {
		t.Errorf("expected Cursor.present=true (md fallback link), got %+v", cursor)
	}
	if !cursor.broken {
		t.Errorf("expected Cursor.broken=true (orphan global rule), got %+v", cursor)
	}
}

// findBadge fishes one badge out of the iterator result by name; fails the
// surrounding test when the badge is missing, since every platform.All()
// entry that implements StatusBadger is expected to appear in the slice.
func findBadge(t *testing.T, badges []platformBadge, name string) platformBadge {
	t.Helper()
	for _, b := range badges {
		if b.name == name {
			return b
		}
	}
	t.Fatalf("badge %q not found in %+v", name, badges)
	return platformBadge{}
}

// ---------- additional coverage ----------

// countCanonicalScopedFiles / countCanonicalScopedDirs
func TestCountCanonicalScopedFiles_IgnoresDirs(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.json"), []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(tmp, "subdir"), 0755)
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got := countCanonicalScopedFiles(entries); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestCountCanonicalScopedDirs_RequiresMarker(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "withmarker")
	b := filepath.Join(tmp, "nomarker")
	os.MkdirAll(a, 0755)
	os.MkdirAll(b, 0755)
	os.WriteFile(filepath.Join(a, "SKILL.md"), []byte("x"), 0644)
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got := countCanonicalScopedDirs(tmp, entries, "SKILL.md"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestSummarizeCanonicalScope_BothModes(t *testing.T) {
	tmp := t.TempDir()
	scope := filepath.Join(tmp, "s")
	os.MkdirAll(scope, 0755)
	os.WriteFile(filepath.Join(scope, "f1.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(scope, "f2.json"), []byte("{}"), 0644)
	if got := summarizeCanonicalScope(scope, false, ""); got != 2 {
		t.Errorf("file mode expected 2, got %d", got)
	}
	if got := summarizeCanonicalScope(filepath.Join(tmp, "missing"), false, ""); got != 0 {
		t.Errorf("missing path expected 0, got %d", got)
	}
}

// countManagedFileOK: healthy file, symlink-to-good, symlink-to-broken, missing.
func TestCountManagedFileOK_RegularFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "f")
	os.WriteFile(f, []byte("x"), 0644)
	warn := 0
	if got := countManagedFileOK(f, &warn); got != 1 || warn != 0 {
		t.Errorf("regular file: got=%d warn=%d", got, warn)
	}
}

func TestCountManagedFileOK_MissingFile(t *testing.T) {
	warn := 0
	if got := countManagedFileOK("/no/such/file/xyz123", &warn); got != 0 || warn != 0 {
		t.Errorf("missing: got=%d warn=%d", got, warn)
	}
}

func TestCountManagedFileOK_HealthySymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "link")
	linktest.Link(t, target, link)
	warn := 0
	if got := countManagedFileOK(link, &warn); got != 1 || warn != 0 {
		t.Errorf("healthy symlink: got=%d warn=%d", got, warn)
	}
}

func TestCountManagedFileOK_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "link")
	linktest.DanglingLink(t, link)
	warn := 0
	if got := countManagedFileOK(link, &warn); got != 0 || warn != 1 {
		t.Errorf("broken symlink: got=%d warn=%d", got, warn)
	}
}

// formatRefreshDisplay edge cases.
func TestFormatRefreshDisplay_ShortAndExact(t *testing.T) {
	if got := formatRefreshDisplay(""); got != "" {
		t.Errorf("empty: got %q", got)
	}
	// Exactly long enough boundary
	in := "2026-01-02T03:04"
	want := "2026-01-02 03:04 UTC"
	if got := formatRefreshDisplay(in); got != want {
		t.Errorf("len16: got %q want %q", got, want)
	}
}

// readRefreshTimestamp uses rc.Refresh if present.
func TestReadRefreshTimestamp_PrefersAgentsRC(t *testing.T) {
	tmp := t.TempDir()
	rc := &config.AgentsRC{
		Version: 1,
		Project: "p",
		Refresh: &config.RefreshMetadata{RefreshedAt: "2027-05-01T10:11:12Z"},
	}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	got := readRefreshTimestamp(tmp)
	if got != "2027-05-01 10:11 UTC" {
		t.Errorf("got %q", got)
	}
}

func TestReadRefreshTimestamp_FallsBackToLegacy(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, ".agents-refresh"), []byte("refreshed_at=2024-12-31T23:59:00Z"), 0644)
	got := readRefreshTimestamp(tmp)
	if got != "2024-12-31 23:59 UTC" {
		t.Errorf("got %q", got)
	}
}

// printBadgeRow / printAgentsHomeGitStatusLine smoke tests (just exercise without panic).
func TestPrintBadgeRow_VariousStates(t *testing.T) {
	printBadgeRow([]platformBadge{
		{name: "A", present: true},
		{name: "B", broken: true},
		{name: "C"},
	})
}

func TestPrintAgentsHomeGitStatusLine_NotRepo(t *testing.T) {
	tmp := t.TempDir()
	printAgentsHomeGitStatusLine(tmp)
}

func TestPrintAgentsHomeGitStatusLine_BareGit(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	printAgentsHomeGitStatusLine(tmp)
}

// printManagedAuditPath / printManagedAuditDir smoke tests.
func TestPrintManagedAuditPath_AllBranches(t *testing.T) {
	tmp := t.TempDir()
	rel := func(s string) string { return filepath.Base(s) }

	// missing path
	printManagedAuditPath(filepath.Join(tmp, "missing"), rel)

	// regular file
	f := filepath.Join(tmp, "f")
	os.WriteFile(f, []byte("x"), 0644)
	printManagedAuditPath(f, rel)

	// healthy symlink
	target := filepath.Join(tmp, "t")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "l")
	linktest.Link(t, target, link)
	printManagedAuditPath(link, rel)

	// broken symlink
	broken := filepath.Join(tmp, "b")
	linktest.DanglingLink(t, broken)
	printManagedAuditPath(broken, rel)
}

func TestPrintManagedAuditDir_Smoke(t *testing.T) {
	tmp := t.TempDir()
	d := filepath.Join(tmp, "d")
	os.MkdirAll(d, 0755)
	os.WriteFile(filepath.Join(d, "a"), []byte("x"), 0644)
	rel := func(s string) string { return s }
	printManagedAuditDir(d, rel)
	// Missing dir is a no-op.
	printManagedAuditDir(filepath.Join(tmp, "missing"), rel)
}

// printCanonicalStoreSection / printPluginsSection: smoke run.
func TestPrintCanonicalStoreSection_Smoke(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	printCanonicalStoreSection(agentsHome)
}

func TestPrintPluginsSection_NoPlugins(t *testing.T) {
	tmp := t.TempDir()
	printPluginsSection(tmp)
}

func TestPrintPluginsSection_WithPlugins(t *testing.T) {
	tmp := t.TempDir()
	pluginDir := filepath.Join(tmp, "plugins", "scope1", "demo")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [opencode]\n"), 0644)
	// Also a global plugin (no scope dir name; let's add another)
	globalDir := filepath.Join(tmp, "plugins", "global", "another")
	os.MkdirAll(globalDir, 0755)
	os.WriteFile(filepath.Join(globalDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: another\nplatforms: [opencode]\n"), 0644)
	printPluginsSection(tmp)
}

// printStatusProjectManifestSummary: covers manifest missing + manifest present.
func TestPrintStatusProjectManifestSummary_NoManifest(t *testing.T) {
	tmp := t.TempDir()
	printStatusProjectManifestSummary(tmp)
}

func TestPrintStatusProjectManifestSummary_PresentWithSkills(t *testing.T) {
	tmp := t.TempDir()
	rc := &config.AgentsRC{
		Version: 1,
		Project: "demo",
		Skills:  []string{"s1", "s2"},
		Agents:  []string{"a1"},
		Sources: []config.Source{{Type: "git", URL: "https://example.com/foo.git"}},
	}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	printStatusProjectManifestSummary(tmp)
}

// TestPrintStatusProjectManifestSummary_HooksAndMCPEnabled covers the
// rc.Hooks.IsEnabled() + rc.MCP.IsEnabled() append branches.
func TestPrintStatusProjectManifestSummary_HooksAndMCPEnabled(t *testing.T) {
	tmp := t.TempDir()
	rc := &config.AgentsRC{
		Version: 1,
		Project: "demo",
		Hooks:   config.StringsOrBool{All: true},
		MCP:     config.StringsOrBool{All: true},
	}
	if err := rc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	printStatusProjectManifestSummary(tmp)
}

// printUserConfigSection: empty home → exercises the "no managed user-level config" branch.
func TestPrintUserConfigSection_NoConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	printUserConfigSection(agentsHome, false, "")
}

func TestPrintUserConfigSection_WithClaudeMD(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("# claude"), 0644)
	// Audit mode prints managed audit details.
	printUserConfigSection(agentsHome, true, "")
}

// TestPrintUserConfigSection_AllPlatformsSeeded covers the codex and opencode
// badge-append branches (906-908, 925-927) plus opencode audit-mode dir walk.
func TestPrintUserConfigSection_AllPlatformsSeeded(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Claude
	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("# c"), 0644)

	// Codex: ~/.codex/hooks.json + ~/.codex/agents/ + ~/.agents/skills/.
	codexHome := filepath.Join(tmp, ".codex")
	os.MkdirAll(filepath.Join(codexHome, "agents"), 0755)
	os.WriteFile(filepath.Join(codexHome, "hooks.json"), []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(tmp, ".agents", "skills"), 0755)
	// One skill symlink so the dir count > 0.
	target := filepath.Join(agentsHome, "skills", "global", "demo")
	os.MkdirAll(target, 0755)
	linktest.Link(t, target, filepath.Join(tmp, ".agents", "skills", "demo"))

	// OpenCode: ~/.opencode/agent/<symlink>.
	opAgent := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opAgent, 0755)
	opTarget := filepath.Join(agentsHome, "agents", "global", "demo")
	os.MkdirAll(opTarget, 0755)
	linktest.Link(t, opTarget, filepath.Join(opAgent, "demo"))

	// Audit mode also exercises the audit-detail prints across all platforms.
	printUserConfigSection(agentsHome, true, "")
}

// printSharedTargetRegistry: empty platforms hits the early-return branch.
// All platforms explicitly disabled in cfg so the early-return fires regardless
// of host environment.
func TestPrintSharedTargetRegistry_NoPlatforms(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	for _, pid := range []string{"cursor", "claude", "codex", "opencode", "copilot"} {
		cfg.SetPlatformState(pid, false, "")
	}
	printSharedTargetRegistry("proj", tmp, cfg)
}

func TestSharedTargetRegistryPlanLines_EmptyPlatforms(t *testing.T) {
	lines, err := sharedTargetRegistryPlanLines("p", "/tmp/x", nil)
	if err != nil || lines != nil {
		t.Errorf("expected (nil, nil), got (%v, %v)", lines, err)
	}
}

// printLinkedStatusLine: healthy and broken.
func TestPrintLinkedStatusLine_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "t")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "l")
	linktest.Link(t, target, link)
	if !printLinkedStatusLine("label", link) {
		t.Error("expected healthy symlink to return true")
	}

	broken := filepath.Join(tmp, "b")
	linktest.DanglingLink(t, broken)
	if printLinkedStatusLine("label", broken) {
		t.Error("expected broken symlink to return false")
	}
}

// printCodexAgentsMD: symlink, regular file, missing.
func TestPrintCodexAgentsMD_Variants(t *testing.T) {
	tmp := t.TempDir()
	printCodexAgentsMD(filepath.Join(tmp, "AGENTS.md")) // missing

	plain := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(plain, []byte("rules"), 0644)
	printCodexAgentsMD(plain) // regular file

	link := filepath.Join(tmp, "AGENTS-link.md")
	linktest.Link(t, plain, link)
	printCodexAgentsMD(link) // symlink
}

func TestPrintCodexSymlinkAudit_Variants(t *testing.T) {
	tmp := t.TempDir()
	// not linked
	printCodexSymlinkAudit(filepath.Join(tmp, "missing"), "label")

	// linked
	target := filepath.Join(tmp, "target")
	os.WriteFile(target, []byte("x"), 0644)
	link := filepath.Join(tmp, "link")
	linktest.Link(t, target, link)
	printCodexSymlinkAudit(link, "label")
}

func TestPrintCodexSkillsAudit_EmptyAndPopulated(t *testing.T) {
	// no dir → no-op
	tmp := t.TempDir()
	printCodexSkillsAudit(filepath.Join(tmp, "missing"))

	// empty dir
	emptyDir := filepath.Join(tmp, "empty")
	os.MkdirAll(emptyDir, 0755)
	printCodexSkillsAudit(emptyDir)

	// dir with healthy + broken symlinks
	d := filepath.Join(tmp, "d")
	os.MkdirAll(d, 0755)
	target := filepath.Join(tmp, "skill-target")
	os.WriteFile(target, []byte("x"), 0644)
	linktest.Link(t, target, filepath.Join(d, "ok"))
	linktest.DanglingLink(t, filepath.Join(d, "broken"))
	printCodexSkillsAudit(d)
}

func TestPrintCodexAgentsAudit_Variants(t *testing.T) {
	tmp := t.TempDir()
	// missing dir
	printCodexAgentsAudit(filepath.Join(tmp, "missing"))
	// empty dir
	emptyDir := filepath.Join(tmp, "empty")
	os.MkdirAll(emptyDir, 0755)
	printCodexAgentsAudit(emptyDir)
	// populated
	d := filepath.Join(tmp, "d")
	os.MkdirAll(d, 0755)
	os.WriteFile(filepath.Join(d, "agent.toml"), []byte("x"), 0644)
	printCodexAgentsAudit(d)
}

// printCursorAudit / printClaudeAudit / printCodexAudit / printOpenCodeAudit / printCopilotAudit smoke tests.
func TestPrintAuditFunctions_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	printCursorAudit("proj", tmp, agentsHome)
	printClaudeAudit("proj", tmp, agentsHome)
	printCodexAudit("proj", tmp, agentsHome)
	printOpenCodeAudit("proj", tmp, agentsHome)
	printCopilotAudit("proj", tmp)
}

func TestPrintCursorAudit_HealthyAndUnlinkedRule(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	src := filepath.Join(agentsHome, "rules", "global", "rule.mdc")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("rule"), 0644)

	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)
	// healthy hardlink
	os.Link(src, filepath.Join(rulesDir, "global--rule.mdc"))
	// unlinked managed-namespace file (will report "not linked")
	os.WriteFile(filepath.Join(rulesDir, "proj--unlinked.mdc"), []byte("x"), 0644)
	// local-file rule
	os.WriteFile(filepath.Join(rulesDir, "local.mdc"), []byte("x"), 0644)
	// non-mdc skipped
	os.WriteFile(filepath.Join(rulesDir, "junk.txt"), []byte("x"), 0644)

	// cursor mcp.json broken symlink
	linktest.DanglingLink(t, filepath.Join(tmp, ".cursor", "mcp.json"))

	printCursorAudit("proj", tmp, agentsHome)
}

func TestPrintClaudeAudit_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# rules"), 0644)

	rulesDir := filepath.Join(tmp, ".claude", "rules")
	os.MkdirAll(rulesDir, 0755)
	linktest.Link(t, target, filepath.Join(rulesDir, "ok.md"))
	linktest.DanglingLink(t, filepath.Join(rulesDir, "broken.md"))

	// .mcp.json healthy
	mcpTarget := filepath.Join(agentsHome, "mcp.json")
	os.WriteFile(mcpTarget, []byte("{}"), 0644)
	linktest.Link(t, mcpTarget, filepath.Join(tmp, ".mcp.json"))

	printClaudeAudit("proj", tmp, agentsHome)
}

func TestPrintOpenCodeAudit_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	target := filepath.Join(agentsHome, "settings", "proj", "opencode.json")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("{}"), 0644)
	linktest.Link(t, target, filepath.Join(tmp, "opencode.json"))

	agentDir := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(agentDir, 0755)
	agentTarget := filepath.Join(agentsHome, "agents", "proj", "ok", "AGENT.md")
	os.MkdirAll(filepath.Dir(agentTarget), 0755)
	os.WriteFile(agentTarget, []byte("ok"), 0644)
	linktest.Link(t, agentTarget, filepath.Join(agentDir, "ok.md"))
	linktest.DanglingLink(t, filepath.Join(agentDir, "broken.md"))

	printOpenCodeAudit("proj", tmp, agentsHome)
}

func TestPrintCopilotAudit_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	target := filepath.Join(agentsHome, "rules", "proj", "copilot-instructions.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# instructions"), 0644)
	os.MkdirAll(filepath.Join(tmp, ".github"), 0755)
	linktest.Link(t, target, filepath.Join(tmp, ".github", "copilot-instructions.md"))

	mcpTarget := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpTarget), 0755)
	os.WriteFile(mcpTarget, []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(tmp, ".vscode"), 0755)
	linktest.Link(t, mcpTarget, filepath.Join(tmp, ".vscode", "mcp.json"))

	printCopilotAudit("proj", tmp)
}

// printAudit (top-level) with no platforms enabled — should just emit headers.
func TestPrintAudit_AllPlatformsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	printAudit("proj", tmp, agentsHome, "", cfg)
	printAudit("proj", tmp, agentsHome, "claude", cfg)
}

// statusGitInfo with a fake .git dir reaches the IsRepo=true branch.
func TestStatusGitInfo_WithGitDir(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	g := statusGitInfo(tmp)
	if !g.Initialized {
		t.Errorf("expected Initialized=true, got %+v", g)
	}
}

// runStatus --audit with a registered project and a manifest to exercise printAudit.
func TestRunStatus_AuditMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	rc.Save(projectPath)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runStatus(true, "", stdStatusConfigLoader{}, false); err != nil {
		t.Errorf("runStatus --audit: %v", err)
	}
}

// runStatus with a project whose directory was removed → error bullet branch.
func TestRunStatus_MissingProjectDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("gone", filepath.Join(tmp, "gone-dir"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runStatus(false, "", stdStatusConfigLoader{}, false); err != nil {
		t.Errorf("runStatus with missing project dir: %v", err)
	}
}

// collectUserConfigPlatforms with files present.
func TestCollectUserConfigPlatforms_Populated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	claudeDir := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("x"), 0644)
	got := collectUserConfigPlatforms("")
	if len(got) == 0 {
		t.Error("expected at least one platform reported")
	}
}

// TestPlatformTextBadges_Empty — codex/opencode/copilot basic smoke. After
// P3 each badge is produced by the platform.StatusBadger implementation
// for the named platform, surfaced through collectProjectTextBadges (the
// status.go iterator). The empty-project assertion is identical to the
// pre-P3 behavior: every badge reports not-present, not-broken.
func TestPlatformTextBadges_Empty(t *testing.T) {
	tmp := t.TempDir()
	got := collectProjectTextBadges("proj", tmp, filepath.Join(tmp, ".agents"), nil)
	for _, label := range []string{"Codex", "OpenCode", "Copilot"} {
		badge := findBadge(t, got, label)
		if badge.present {
			t.Errorf("expected %s badge.present=false for empty project, got %+v", label, badge)
		}
		if badge.broken {
			t.Errorf("expected %s badge.broken=false for empty project, got %+v", label, badge)
		}
	}
}

// TestPrintSharedTargetRegistry_WithInstalledClaude exercises the printer with
// a real installed platform — covers the lines-rendering branch (post early
// return).
func TestPrintSharedTargetRegistry_WithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	// Seed a small canonical resource so the plan has at least one line.
	os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755)
	os.WriteFile(filepath.Join(agentsHome, "rules", "proj", "agents.md"), []byte("# rules"), 0644)

	repo := filepath.Join(tmp, "repo")
	os.MkdirAll(repo, 0755)

	// Should not panic and should print the registry header with lines.
	printSharedTargetRegistry("proj", repo, cfg)
}

// TestBuildStatusJSONReport_WithPluginAndProjects exercises the buildStatusJSONReport
// branches that populate plugin entries and project entries — the existing
// JSON test only covers the empty-projects case.
func TestBuildStatusJSONReport_WithPluginAndProjects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Seed a plugin
	pluginDir := filepath.Join(agentsHome, "plugins", "global", "demo")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [opencode]\n"), 0644)

	// Seed a registered project
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	rc.Save(projectPath)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)

	report, err := buildStatusJSONReport(cfg, agentsHome, "")
	if err != nil {
		t.Fatalf("buildStatusJSONReport: %v", err)
	}
	if len(report.Plugins) == 0 {
		t.Error("expected at least one plugin entry")
	}
	if len(report.Projects) == 0 {
		t.Error("expected at least one project entry")
	}
}

// TestPrintCursorAudit_BrokenSymlinkAndLocalFile exercises the .cursor/mcp.json
// branches: broken-symlink, hard-link-or-local-file, and (not linked).
func TestPrintCursorAudit_BrokenSymlinkAndLocalFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Project A: broken .cursor/mcp.json symlink + a local .mdc rule with no prefix.
	projA := filepath.Join(tmp, "projA")
	cursorDirA := filepath.Join(projA, ".cursor")
	os.MkdirAll(filepath.Join(cursorDirA, "rules"), 0755)
	// Local-file rule (no prefix → "local" branch).
	os.WriteFile(filepath.Join(cursorDirA, "rules", "ad-hoc.mdc"), []byte("# local"), 0644)
	// Broken symlink for .cursor/mcp.json.
	linktest.DanglingLink(t, filepath.Join(cursorDirA, "mcp.json"))

	// Project B: regular file (not symlink) at .cursor/mcp.json → "hard link or local file".
	projB := filepath.Join(tmp, "projB")
	cursorDirB := filepath.Join(projB, ".cursor")
	os.MkdirAll(filepath.Join(cursorDirB, "rules"), 0755)
	os.WriteFile(filepath.Join(cursorDirB, "mcp.json"), []byte("{}"), 0644)

	// Project C: no .cursor at all → "not linked" early-return branch.
	projC := filepath.Join(tmp, "projC")
	os.MkdirAll(projC, 0755)

	// Project D: rules dir present, no mcp.json → not-linked branch (1005-1007).
	projD := filepath.Join(tmp, "projD")
	os.MkdirAll(filepath.Join(projD, ".cursor", "rules"), 0755)

	// Project E: rules dir present with global--*.mdc that's NOT hardlinked
	// (warning branch 983).
	projE := filepath.Join(tmp, "projE")
	rulesE := filepath.Join(projE, ".cursor", "rules")
	os.MkdirAll(rulesE, 0755)
	os.WriteFile(filepath.Join(rulesE, "global--orphan.mdc"), []byte("not linked"), 0644)
	os.WriteFile(filepath.Join(rulesE, "projE--also.mdc"), []byte("not linked"), 0644)

	printCursorAudit("projA", projA, agentsHome)
	printCursorAudit("projB", projB, agentsHome)
	printCursorAudit("projC", projC, agentsHome)
	printCursorAudit("projD", projD, agentsHome)
	printCursorAudit("projE", projE, agentsHome)
}

// TestPrintClaudeAudit_BrokenAndHealthy exercises printSymlinkDirAudit broken
// branch and printSymlinkAudit broken branch via printClaudeAudit.
func TestPrintClaudeAudit_BrokenAndHealthy(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	proj := filepath.Join(tmp, "p")
	claudeRules := filepath.Join(proj, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)

	// Healthy symlink.
	healthyTarget := filepath.Join(agentsHome, "rules", "p", "ok.md")
	os.MkdirAll(filepath.Dir(healthyTarget), 0755)
	os.WriteFile(healthyTarget, []byte("ok"), 0644)
	linktest.Link(t, healthyTarget, filepath.Join(claudeRules, "p--ok.md"))

	// Broken symlink.
	linktest.DanglingLink(t, filepath.Join(claudeRules, "p--broken.md"))
	// Non-symlink regular file in rules dir → triggers the Readlink-err continue branch.
	os.WriteFile(filepath.Join(claudeRules, "raw-file.md"), []byte("not-a-link"), 0644)

	// Broken .mcp.json symlink at project root.
	linktest.DanglingLink(t, filepath.Join(proj, ".mcp.json"))

	printClaudeAudit("p", proj, agentsHome)
}

// TestPrintCodexAudit_AllBranches covers printCodexAgentsMD (symlink, local, missing),
// printCodexSymlinkAudit (linked + not linked), printCodexSkillsAudit and
// printCodexAgentsAudit (healthy + broken).
func TestPrintCodexAudit_AllBranches(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Project 1: AGENTS.md is a healthy symlink, codex config.toml linked,
	// hooks.json not linked, skills dir has healthy+broken+non-symlink entries,
	// agents dir has readable + unreadable file.
	proj := filepath.Join(tmp, "p1")
	os.MkdirAll(filepath.Join(proj, ".codex", "agents"), 0755)
	os.MkdirAll(filepath.Join(proj, ".agents", "skills"), 0755)

	// AGENTS.md symlink → healthy target.
	target := filepath.Join(agentsHome, "rules", "p1", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# a"), 0644)
	linktest.Link(t, target, filepath.Join(proj, "AGENTS.md"))

	// codex config.toml linked.
	cfgT := filepath.Join(agentsHome, "settings", "p1", "codex.toml")
	os.MkdirAll(filepath.Dir(cfgT), 0755)
	os.WriteFile(cfgT, []byte("# toml"), 0644)
	linktest.Link(t, cfgT, filepath.Join(proj, ".codex", "config.toml"))

	// Skill: one healthy symlink + one broken + one non-symlink file (skipped).
	skillTarget := filepath.Join(agentsHome, "skills", "p1", "x")
	os.MkdirAll(skillTarget, 0755)
	linktest.Link(t, skillTarget, filepath.Join(proj, ".agents", "skills", "x"))
	linktest.DanglingLink(t, filepath.Join(proj, ".agents", "skills", "broken"))
	os.WriteFile(filepath.Join(proj, ".agents", "skills", "regular.md"), []byte("not-a-symlink"), 0644)

	// Codex agent file (readable) + a broken symlink → printCodexAgentsAudit ✗.
	os.WriteFile(filepath.Join(proj, ".codex", "agents", "ok.toml"), []byte("name=ok"), 0644)
	linktest.DanglingLink(t, filepath.Join(proj, ".codex", "agents", "broken.toml"))

	printCodexAudit("p1", proj, agentsHome)

	// Project 2: AGENTS.md is a regular file (local branch), no other dirs.
	proj2 := filepath.Join(tmp, "p2")
	os.MkdirAll(filepath.Join(proj2, ".codex"), 0755)
	os.MkdirAll(filepath.Join(proj2, ".agents", "skills"), 0755)
	os.WriteFile(filepath.Join(proj2, "AGENTS.md"), []byte("# local"), 0644)
	printCodexAudit("p2", proj2, agentsHome)

	// Project 3: no AGENTS.md at all → "(no AGENTS.md)" branch.
	proj3 := filepath.Join(tmp, "p3")
	os.MkdirAll(proj3, 0755)
	printCodexAudit("p3", proj3, agentsHome)
}

// TestPrintOpenCodeAudit_LocalAndBroken covers opencode.json local-file +
// broken-symlink branches and the missing-.opencode/ branch.
func TestPrintOpenCodeAudit_LocalAndBroken(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Local-file opencode.json (not symlink).
	projLocal := filepath.Join(tmp, "local")
	os.MkdirAll(projLocal, 0755)
	os.WriteFile(filepath.Join(projLocal, "opencode.json"), []byte("{}"), 0644)
	printOpenCodeAudit("local", projLocal, agentsHome)

	// Broken-symlink opencode.json + missing .opencode/agent dir.
	projBroken := filepath.Join(tmp, "broken")
	os.MkdirAll(projBroken, 0755)
	linktest.DanglingLink(t, filepath.Join(projBroken, "opencode.json"))
	printOpenCodeAudit("broken", projBroken, agentsHome)
}

// TestPrintCopilotAudit_BrokenAndNotLinked covers .github/copilot-instructions.md
// broken-symlink and not-linked branches.
func TestPrintCopilotAudit_BrokenAndNotLinked(t *testing.T) {
	tmp := t.TempDir()

	// Broken symlink.
	projBroken := filepath.Join(tmp, "broken")
	os.MkdirAll(filepath.Join(projBroken, ".github"), 0755)
	linktest.DanglingLink(t, filepath.Join(projBroken, ".github", "copilot-instructions.md"))
	printCopilotAudit("broken", projBroken)

	// Not linked at all.
	projEmpty := filepath.Join(tmp, "empty")
	os.MkdirAll(projEmpty, 0755)
	printCopilotAudit("empty", projEmpty)
}

// TestPrintSymlinkDirAudit_EmptyDir covers the empty-label branch.
func TestPrintSymlinkDirAudit_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "empty")
	os.MkdirAll(dir, 0755)
	ok, broken := printSymlinkDirAudit(dir, ".some/path/", "%s")
	if ok != 0 || broken != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", ok, broken)
	}
}

// TestPrintCanonicalStoreSection_PopulatedBuckets exercises printManagedAuditPath
// broken-symlink branch via canonical store and several countManagedDirEntries
// edge cases (symlink with broken Readlink dest).
func TestPrintManagedAuditPath_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "link.md")
	linktest.DanglingLink(t, link)
	// Target doesn't exist → broken-symlink output branch.
	printManagedAuditPath(link, func(p string) string { return p })

	// Regular file (not symlink) → final fmt.Fprintf branch.
	regular := filepath.Join(tmp, "reg.md")
	os.WriteFile(regular, []byte("x"), 0644)
	printManagedAuditPath(regular, func(p string) string { return p })

	// Non-existent path → early return.
	printManagedAuditPath(filepath.Join(tmp, "ghost"), func(p string) string { return p })
}

// TestCountManagedDirEntries_RegularFilePlusBroken covers regular-file ok++
// alongside a broken symlink warn++ in the same dir.
func TestCountManagedDirEntries_RegularFilePlusBroken(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "d")
	os.MkdirAll(dir, 0755)
	// Healthy regular file (non-symlink) → ok++.
	os.WriteFile(filepath.Join(dir, "a"), []byte("a"), 0644)
	// Broken symlink → warn++.
	linktest.DanglingLink(t, filepath.Join(dir, "bad"))
	warn := 0
	got := countManagedDirEntries(dir, &warn)
	if got < 1 {
		t.Errorf("expected at least one ok entry, got %d", got)
	}
	if warn < 1 {
		t.Errorf("expected at least one warn for broken symlink, got %d", warn)
	}
}

// Note: TestPrintAgentsHomeGitStatusLine_NotRepo and _BareGit upstream
// (lines 575, 580) already cover both no-.git and .git-but-no-remote
// branches. Duplicates removed per SonarCloud S4144.

// TestRunStatus_CorruptConfigErrors covers the config.Load err branch (326-328).
func TestRunStatus_CorruptConfigErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	// Corrupt config.json → Load returns parse error.
	os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte("not-json"), 0644)

	err := runStatus(false, "", stdStatusConfigLoader{}, false)
	if err == nil {
		t.Error("expected config.Load error from corrupt config.json")
	}
}

// TestRunStatus_JSONMode covers the JSON path (lines 332-341) which we haven't
// exercised much.
func TestRunStatus_JSONMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runStatus(false, "", stdStatusConfigLoader{}, true); err != nil {
		t.Errorf("runStatus JSON: %v", err)
	}
}

// TestRunStatus_LastRefreshedRender covers the "last refreshed" print branch
// (389-391) by registering a project whose .agentsrc.json has a refresh
// timestamp.
func TestRunStatus_LastRefreshedRender(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	rc := &config.AgentsRC{
		Version: 1,
		Project: "p",
		Refresh: &config.RefreshMetadata{RefreshedAt: "2026-05-01T12:30:00Z"},
	}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	cmd := NewStatusCmd(testStatusDeps(), jsonOff)
	if err := cmd.Execute(); err != nil {
		t.Errorf("runStatus with refresh ts: %v", err)
	}
}

// TestRunStatus_DirectoryMissing covers the "Directory not found" continue
// branch for a registered-but-missing project (line 380-382).
func TestRunStatus_DirectoryMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("ghost", filepath.Join(tmp, "ghost-path"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := runStatus(false, "", stdStatusConfigLoader{}, false); err != nil {
		t.Errorf("runStatus missing dir: %v", err)
	}
}

// TestRunStatus_AuditModeWithRegisteredProject covers runStatus audit-mode with
// a registered project and an installed claude platform — exercises the
// per-project printAudit + printSharedTargetRegistry full path.
func TestRunStatus_AuditModeWithRegisteredProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	// Healthy AGENTS.md link + broken claude rule symlink to exercise both
	// branches inside printAudit.
	target := filepath.Join(agentsHome, "rules", "p", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# rules"), 0644)
	linktest.Link(t, target, filepath.Join(projectPath, "AGENTS.md"))

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.DanglingLink(t, filepath.Join(claudeRules, "p--ghost.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// runStatus is the cobra RunE; invoke via cmd to ensure flags route through.
	cmd := NewStatusCmd(testStatusDeps(), jsonOff)
	cmd.SetArgs([]string{"--audit"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("runStatus audit: %v", err)
	}
}

// TestStatusNoArgsHint_RejectsPositionalArgs covers the lifecycle-local
// noArgs hint variant — non-empty args path returns the deps.UsageError-built
// error.
func TestStatusNoArgsHint_RejectsPositionalArgs(t *testing.T) {
	cmd := NewStatusCmd(testStatusDeps(), jsonOff)
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("expected error for positional arg")
	} else if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Errorf("unexpected error text: %v", err)
	}
}

// TestStatusJSONClosure_Toggle pins that the jsonOutput closure is invoked
// per-RunE call: switching the closure switches the output path. This
// guards the new closure-based JSON seam introduced when status moved into
// the lifecycle subpackage (the old pattern used commands.Flags.JSON
// directly, which is unavailable here without an import cycle).
func TestStatusJSONClosure_Toggle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	calls := 0
	closure := func() bool {
		calls++
		return calls > 1 // first call: text; second: JSON
	}
	cmd := NewStatusCmd(testStatusDeps(), closure)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("first RunE: %v", err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("second RunE: %v", err)
	}
	if calls < 2 {
		t.Errorf("expected jsonOutput closure to be invoked per RunE, got %d calls", calls)
	}
}

// ---------- .agentsrc.lock summary (config-v2 p2) ----------

// captureStatusStdout redirects stdout for the duration of fn and returns the
// captured bytes — used to assert the lock summary line content.
func captureStatusStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// seedLockProject creates a project dir with a manifest and (optionally) a
// committed .agentsrc.lock holding the given locked layers.
func seedLockProject(t *testing.T, manifest string, layers map[string]config.LockedLayer) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.AgentsRCFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if layers != nil {
		if err := config.WriteConfigLock(dir, layers); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPrintStatusProjectLockSummary_NoExtendsSilent(t *testing.T) {
	dir := seedLockProject(t, `{"version":2}`, nil)
	out := captureStatusStdout(t, func() { printStatusProjectLockSummary(dir) })
	if out != "" {
		t.Errorf("expected no output for manifest without extends, got %q", out)
	}
}

func TestPrintStatusProjectLockSummary_MissingManifestSilent(t *testing.T) {
	dir := t.TempDir() // no manifest at all
	out := captureStatusStdout(t, func() { printStatusProjectLockSummary(dir) })
	if out != "" {
		t.Errorf("expected no output for missing manifest, got %q", out)
	}
}

func TestPrintStatusProjectLockSummary_NoLock(t *testing.T) {
	dir := seedLockProject(t, `{"extends":["acme:org/base.json"]}`, nil)
	out := captureStatusStdout(t, func() { printStatusProjectLockSummary(dir) })
	if !strings.Contains(out, "no .agentsrc.lock") {
		t.Errorf("expected missing-lock notice, got %q", out)
	}
}

func TestPrintStatusProjectLockSummary_Locked(t *testing.T) {
	dir := seedLockProject(t, `{"extends":["acme:org/base.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	out := captureStatusStdout(t, func() { printStatusProjectLockSummary(dir) })
	if !strings.Contains(out, "lock") || !strings.Contains(out, "1 unit(s) locked") {
		t.Errorf("expected locked summary, got %q", out)
	}
}

func TestPrintStatusProjectLockSummary_Drifted(t *testing.T) {
	// Declared but not in lock → drift.
	dir := seedLockProject(t, `{"extends":["acme:org/base.json","acme:org/missing.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	out := captureStatusStdout(t, func() { printStatusProjectLockSummary(dir) })
	if !strings.Contains(out, "drifted") || !strings.Contains(out, "da config sync") {
		t.Errorf("expected drift summary with sync hint, got %q", out)
	}
}

func TestBuildStatusJSONLock_NotApplicable(t *testing.T) {
	dir := seedLockProject(t, `{"version":2}`, nil)
	if got := buildStatusJSONLock(dir); got != nil {
		t.Errorf("expected nil lock JSON for no-extends manifest, got %+v", got)
	}
}

func TestBuildStatusJSONLock_ReportsDrift(t *testing.T) {
	dir := seedLockProject(t, `{"extends":["acme:org/base.json","acme:org/missing.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	got := buildStatusJSONLock(dir)
	if got == nil {
		t.Fatal("expected lock JSON, got nil")
	}
	if !got.Present {
		t.Error("expected Present=true")
	}
	if got.TotalLayers != 2 {
		t.Errorf("expected 2 total layers, got %d", got.TotalLayers)
	}
	if len(got.DriftedLayers) != 1 || got.DriftedLayers[0] != "acme:org/missing.json" {
		t.Errorf("expected one drifted layer acme:org/missing.json, got %+v", got.DriftedLayers)
	}
}

// ---------- D4: platform header gated on config-enabled ∧ installed ----------

// TestCollectProjectTextBadges_GatedByEnabledAndInstalled proves the header
// badges are driven by config.json enabled flags ∧ the real install probe,
// not by stray managed artifacts on disk (D4 bug #2). The project tree seeds
// managed links for opencode and copilot so their raw Badge would report
// present=true; the cfg disables opencode and leaves copilot uninstalled, so
// both must render not-present. cursor is enabled+installed with a healthy
// rule, so it stays present.
func TestCollectProjectTextBadges_GatedByEnabledAndInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Hermetic PATH: only the seeded shims resolve, so copilot/codex are
	// guaranteed not-installed regardless of the host machine's real PATH.
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	binDir := filepath.Join(tmp, "fakebin")
	os.MkdirAll(binDir, 0o755)
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, n := range []string{"cursor", "opencode"} {
		if err := os.WriteFile(filepath.Join(binDir, n), []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	// Cursor: healthy global hardlink → raw Badge present.
	cursorSrc := filepath.Join(agentsHome, "rules", "global", "r.mdc")
	os.MkdirAll(filepath.Dir(cursorSrc), 0755)
	os.WriteFile(cursorSrc, []byte("rule"), 0644)
	cursorRules := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)
	if err := os.Link(cursorSrc, filepath.Join(cursorRules, "global--r.mdc")); err != nil {
		t.Fatal(err)
	}

	// OpenCode: opencode.json symlink → raw Badge present (but disabled in cfg).
	ocTarget := filepath.Join(agentsHome, "settings", "proj", "opencode.json")
	os.MkdirAll(filepath.Dir(ocTarget), 0755)
	os.WriteFile(ocTarget, []byte("{}"), 0644)
	linktest.Link(t, ocTarget, filepath.Join(projectPath, "opencode.json"))

	// Copilot: instructions symlink → raw Badge present (but not installed).
	cpTarget := filepath.Join(agentsHome, "rules", "proj", "copilot-instructions.md")
	os.MkdirAll(filepath.Dir(cpTarget), 0755)
	os.WriteFile(cpTarget, []byte("# c"), 0644)
	linktest.Link(t, cpTarget, filepath.Join(projectPath, ".github", "copilot-instructions.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.SetPlatformState("cursor", true, "")
	cfg.SetPlatformState("opencode", false, "") // disabled despite installed + present
	cfg.SetPlatformState("copilot", true, "")   // enabled but not installed

	badges := collectProjectTextBadges("proj", projectPath, agentsHome, cfg)

	cases := []struct {
		name        string
		wantPresent bool
	}{
		{"Cursor", true},    // enabled ∧ installed ∧ present
		{"OpenCode", false}, // disabled in cfg
		{"Copilot", false},  // not installed
	}
	for _, c := range cases {
		b := findBadge(t, badges, c.name)
		if b.present != c.wantPresent {
			t.Errorf("%s badge.present = %v, want %v (header must reflect enabled∧installed)", c.name, b.present, c.wantPresent)
		}
		if !c.wantPresent && b.broken {
			t.Errorf("%s badge.broken = true; a gated-off platform must not surface broken", c.name)
		}
	}
}

// TestCollectProjectTextBadges_NilCfgPreservesRawBadges pins the legacy
// behavior: with a nil cfg (no gating context) the raw per-platform Badge
// values pass through unchanged.
func TestCollectProjectTextBadges_NilCfgPreservesRawBadges(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	cursorSrc := filepath.Join(agentsHome, "rules", "global", "r.mdc")
	os.MkdirAll(filepath.Dir(cursorSrc), 0755)
	os.WriteFile(cursorSrc, []byte("rule"), 0644)
	cursorRules := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)
	if err := os.Link(cursorSrc, filepath.Join(cursorRules, "global--r.mdc")); err != nil {
		t.Fatal(err)
	}
	badges := collectProjectTextBadges("proj", tmp, agentsHome, nil)
	if !findBadge(t, badges, "Cursor").present {
		t.Error("nil cfg must preserve raw Cursor badge present=true")
	}
}

// TestInstalledEnabledPlatformIDs covers both the nil-cfg (empty set) branch
// and the populated branch via an enabled+installed claude.
func TestInstalledEnabledPlatformIDs(t *testing.T) {
	if got := installedEnabledPlatformIDs(nil); len(got) != 0 {
		t.Errorf("nil cfg expected empty set, got %+v", got)
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.SetPlatformState("claude", true, "")
	cfg.SetPlatformState("cursor", false, "")
	got := installedEnabledPlatformIDs(cfg)
	if !got["claude"] {
		t.Errorf("expected claude in enabled+installed set, got %+v", got)
	}
	if got["cursor"] {
		t.Error("disabled cursor must not appear in the set")
	}
}

// ---------- D4: present rendered files are not labeled absent ----------

// TestPrintCodexSymlinkAudit_PresentFileIsLocalNotAbsent proves a present-but-
// not-a-symlink rendered file (e.g. .codex/hooks.json) renders "(local file)"
// rather than "(not linked)" (D4 bug #3), while a truly absent path still
// renders "(not linked)".
func TestPrintCodexSymlinkAudit_PresentFileIsLocalNotAbsent(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, "hooks.json")
	os.WriteFile(present, []byte("{}"), 0644)

	out := captureStatusStdout(t, func() { printCodexSymlinkAudit(present, ".codex/hooks.json") })
	if !strings.Contains(out, "(local file)") {
		t.Errorf("present rendered file should be labeled (local file), got %q", out)
	}
	if strings.Contains(out, "(not linked)") {
		t.Errorf("present rendered file must not be labeled (not linked), got %q", out)
	}

	absent := captureStatusStdout(t, func() { printCodexSymlinkAudit(filepath.Join(tmp, "gone.json"), ".codex/gone.json") })
	if !strings.Contains(absent, "(not linked)") {
		t.Errorf("absent path should be labeled (not linked), got %q", absent)
	}
}

// TestPrintSymlinkAudit_PresentFileIsLocalNotAbsent covers the same
// distinction for the shared single-symlink auditor used by Claude (.mcp.json)
// and Copilot (.vscode/mcp.json).
func TestPrintSymlinkAudit_PresentFileIsLocalNotAbsent(t *testing.T) {
	tmp := t.TempDir()
	present := filepath.Join(tmp, ".mcp.json")
	os.WriteFile(present, []byte("{}"), 0644)

	out := captureStatusStdout(t, func() { printSymlinkAudit(present, ".mcp.json") })
	if !strings.Contains(out, "(local file)") {
		t.Errorf("present file should be (local file), got %q", out)
	}
	if strings.Contains(out, "(not linked)") {
		t.Errorf("present file must not be (not linked), got %q", out)
	}

	absent := captureStatusStdout(t, func() { printSymlinkAudit(filepath.Join(tmp, "missing.json"), ".vscode/mcp.json") })
	if !strings.Contains(absent, "(not linked)") {
		t.Errorf("absent path should be (not linked), got %q", absent)
	}

	// Healthy symlink still renders ✓ → dest.
	target := filepath.Join(tmp, "target.json")
	os.WriteFile(target, []byte("{}"), 0644)
	link := filepath.Join(tmp, "link.json")
	linktest.Link(t, target, link)
	healthy := captureStatusStdout(t, func() { printSymlinkAudit(link, ".mcp.json") })
	if runtime.GOOS == "windows" {
		// Windows managed file links are hard links (symlinks need privilege),
		// indistinguishable from a local file via Lstat, so the audit renders
		// "(local file)" rather than a target arrow.
		if !strings.Contains(healthy, "(local file)") {
			t.Errorf("windows hard link should render (local file), got %q", healthy)
		}
	} else if !strings.Contains(healthy, "→") {
		t.Errorf("healthy symlink should render target arrow, got %q", healthy)
	}
}
