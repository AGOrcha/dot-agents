package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func TestNewStatusCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewStatusCmd()
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
	dangling := filepath.Join(tmp, "nope.txt")
	if err := os.Symlink(dangling, link); err != nil {
		t.Fatal(err)
	}
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
	os.Symlink(filepath.Join(tmp, "nope"), broken)

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
	os.Symlink(filepath.Join(tmp, "ghost"), filepath.Join(dir, "ghost.md"))

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

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runStatus(false, ""); err != nil {
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

	saved := Flags
	Flags = GlobalFlags{JSON: true}
	defer func() { Flags = saved }()

	if err := runStatus(false, ""); err != nil {
		t.Errorf("runStatus --json: %v", err)
	}
}

// ---------- collect{User,Project}PlatformsHelpers exist with empty home ----------

func TestCollectProjectPlatforms_StableLength(t *testing.T) {
	tmp := t.TempDir()
	got := collectProjectPlatforms(tmp)
	if len(got) != 5 {
		t.Errorf("expected 5 platforms (cursor/claude/codex/opencode/copilot), got %d", len(got))
	}
}

func TestCollectUserConfigPlatforms_FilterIsolation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// With no managed configs, collectUserConfigPlatforms returns nothing.
	if got := collectUserConfigPlatforms("claude"); got != nil && len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
	if got := collectUserConfigPlatforms("codex"); got != nil && len(got) != 0 {
		t.Errorf("expected empty list, got %+v", got)
	}
}

// ---------- printBadgeRow / cursorTextBadge integration ----------

func TestCursorTextBadge_NoConfig(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	badge := cursorTextBadge(tmp, agentsHome)
	if badge.present {
		t.Errorf("expected no present badge for empty project, got %+v", badge)
	}
}

func TestClaudeTextBadge_NoRules(t *testing.T) {
	tmp := t.TempDir()
	badge := claudeTextBadge(tmp)
	if badge.present {
		t.Errorf("expected no present badge for empty project, got %+v", badge)
	}
}

func TestCountClaudeRules_ReportsBrokenSymlinks(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, ".claude", "rules")
	os.MkdirAll(rulesDir, 0755)
	os.Symlink(filepath.Join(tmp, "ghost.md"), filepath.Join(rulesDir, "missing.md"))

	ok, warn := countClaudeRules(tmp)
	if ok != 0 || warn != 1 {
		t.Errorf("expected (0,1) for broken claude rules, got (%d,%d)", ok, warn)
	}
}

func TestCountCursorRules_GlobalHardlink(t *testing.T) {
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

	ok, warn := countCursorRules(tmp, agentsHome)
	if ok != 1 || warn != 0 {
		t.Errorf("expected (1,0) for healthy cursor hardlink, got (%d,%d)", ok, warn)
	}
}

// TestCountCursorRules_MDFallbackAndWarn covers the .md fallback success branch
// and the "no link found" warn branch.
func TestCountCursorRules_MDFallbackAndWarn(t *testing.T) {
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

	// Non-global prefix (continue branch)
	os.WriteFile(filepath.Join(rulesDir, "proj--ignored.mdc"), []byte("p"), 0644)
	// Non-mdc (continue)
	os.WriteFile(filepath.Join(rulesDir, "notrule.txt"), []byte("x"), 0644)
	// Backup artifact (continue)
	os.WriteFile(filepath.Join(rulesDir, "global--x.mdc.dot-agents-backup"), []byte("x"), 0644)

	ok, warn := countCursorRules(tmp, agentsHome)
	if ok != 1 {
		t.Errorf("expected ok=1 (md fallback), got %d", ok)
	}
	if warn != 1 {
		t.Errorf("expected warn=1 (orphan), got %d", warn)
	}
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
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	warn := 0
	if got := countManagedFileOK(link, &warn); got != 1 || warn != 0 {
		t.Errorf("healthy symlink: got=%d warn=%d", got, warn)
	}
}

func TestCountManagedFileOK_BrokenSymlink(t *testing.T) {
	tmp := t.TempDir()
	link := filepath.Join(tmp, "link")
	os.Symlink(filepath.Join(tmp, "ghost"), link)
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
	os.Symlink(target, link)
	printManagedAuditPath(link, rel)

	// broken symlink
	broken := filepath.Join(tmp, "b")
	os.Symlink(filepath.Join(tmp, "ghost"), broken)
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
	os.Symlink(target, filepath.Join(tmp, ".agents", "skills", "demo"))

	// OpenCode: ~/.opencode/agent/<symlink>.
	opAgent := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opAgent, 0755)
	opTarget := filepath.Join(agentsHome, "agents", "global", "demo")
	os.MkdirAll(opTarget, 0755)
	os.Symlink(opTarget, filepath.Join(opAgent, "demo"))

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
	os.Symlink(target, link)
	if !printLinkedStatusLine("label", link) {
		t.Error("expected healthy symlink to return true")
	}

	broken := filepath.Join(tmp, "b")
	os.Symlink(filepath.Join(tmp, "ghost"), broken)
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
	os.Symlink(plain, link)
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
	os.Symlink(target, link)
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
	os.Symlink(target, filepath.Join(d, "ok"))
	os.Symlink(filepath.Join(tmp, "ghost"), filepath.Join(d, "broken"))
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
	os.Symlink(filepath.Join(agentsHome, "ghost.json"), filepath.Join(tmp, ".cursor", "mcp.json"))

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
	os.Symlink(target, filepath.Join(rulesDir, "ok.md"))
	os.Symlink(filepath.Join(agentsHome, "ghost.md"), filepath.Join(rulesDir, "broken.md"))

	// .mcp.json healthy
	mcpTarget := filepath.Join(agentsHome, "mcp.json")
	os.WriteFile(mcpTarget, []byte("{}"), 0644)
	os.Symlink(mcpTarget, filepath.Join(tmp, ".mcp.json"))

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
	os.Symlink(target, filepath.Join(tmp, "opencode.json"))

	agentDir := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(agentDir, 0755)
	agentTarget := filepath.Join(agentsHome, "agents", "proj", "ok", "AGENT.md")
	os.MkdirAll(filepath.Dir(agentTarget), 0755)
	os.WriteFile(agentTarget, []byte("ok"), 0644)
	os.Symlink(agentTarget, filepath.Join(agentDir, "ok.md"))
	os.Symlink(filepath.Join(agentsHome, "ghost"), filepath.Join(agentDir, "broken.md"))

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
	os.Symlink(target, filepath.Join(tmp, ".github", "copilot-instructions.md"))

	mcpTarget := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	os.MkdirAll(filepath.Dir(mcpTarget), 0755)
	os.WriteFile(mcpTarget, []byte("{}"), 0644)
	os.MkdirAll(filepath.Join(tmp, ".vscode"), 0755)
	os.Symlink(mcpTarget, filepath.Join(tmp, ".vscode", "mcp.json"))

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

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runStatus(true, ""); err != nil {
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

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runStatus(false, ""); err != nil {
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

// codexTextBadge / opencodeTextBadge / copilotTextBadge basic smoke.
func TestPlatformTextBadges_Empty(t *testing.T) {
	tmp := t.TempDir()
	for _, badge := range []platformBadge{
		codexTextBadge(tmp),
		opencodeTextBadge(tmp),
		copilotTextBadge(tmp),
	} {
		if badge.present {
			t.Errorf("expected no present badge for empty project, got %+v", badge)
		}
	}
}

// TestPrintSharedTargetRegistry_WithInstalledClaude exercises the printer with
// a real installed platform — covers the lines-rendering branch (post early
// return).
func TestPrintSharedTargetRegistry_WithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)
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
	os.Symlink(filepath.Join(agentsHome, "nonexistent"), filepath.Join(cursorDirA, "mcp.json"))

	// Project B: regular file (not symlink) at .cursor/mcp.json → "hard link or local file".
	projB := filepath.Join(tmp, "projB")
	cursorDirB := filepath.Join(projB, ".cursor")
	os.MkdirAll(filepath.Join(cursorDirB, "rules"), 0755)
	os.WriteFile(filepath.Join(cursorDirB, "mcp.json"), []byte("{}"), 0644)

	// Project C: no .cursor at all → "not linked" early-return branch.
	projC := filepath.Join(tmp, "projC")
	os.MkdirAll(projC, 0755)

	printCursorAudit("projA", projA, agentsHome)
	printCursorAudit("projB", projB, agentsHome)
	printCursorAudit("projC", projC, agentsHome)
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
	os.Symlink(healthyTarget, filepath.Join(claudeRules, "p--ok.md"))

	// Broken symlink.
	os.Symlink(filepath.Join(agentsHome, "rules", "p", "missing.md"), filepath.Join(claudeRules, "p--broken.md"))
	// Non-symlink regular file in rules dir → triggers the Readlink-err continue branch.
	os.WriteFile(filepath.Join(claudeRules, "raw-file.md"), []byte("not-a-link"), 0644)

	// Broken .mcp.json symlink at project root.
	os.Symlink(filepath.Join(agentsHome, "mcp", "p", "missing.json"), filepath.Join(proj, ".mcp.json"))

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
	os.Symlink(target, filepath.Join(proj, "AGENTS.md"))

	// codex config.toml linked.
	cfgT := filepath.Join(agentsHome, "settings", "p1", "codex.toml")
	os.MkdirAll(filepath.Dir(cfgT), 0755)
	os.WriteFile(cfgT, []byte("# toml"), 0644)
	os.Symlink(cfgT, filepath.Join(proj, ".codex", "config.toml"))

	// Skill: one healthy symlink + one broken + one non-symlink file (skipped).
	skillTarget := filepath.Join(agentsHome, "skills", "p1", "x")
	os.MkdirAll(skillTarget, 0755)
	os.Symlink(skillTarget, filepath.Join(proj, ".agents", "skills", "x"))
	os.Symlink(filepath.Join(agentsHome, "skills", "p1", "missing"), filepath.Join(proj, ".agents", "skills", "broken"))
	os.WriteFile(filepath.Join(proj, ".agents", "skills", "regular.md"), []byte("not-a-symlink"), 0644)

	// Codex agent file (readable) + a broken symlink → printCodexAgentsAudit ✗.
	os.WriteFile(filepath.Join(proj, ".codex", "agents", "ok.toml"), []byte("name=ok"), 0644)
	os.Symlink(filepath.Join(agentsHome, "missing-agent.toml"), filepath.Join(proj, ".codex", "agents", "broken.toml"))

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
	os.Symlink(filepath.Join(agentsHome, "missing.json"), filepath.Join(projBroken, "opencode.json"))
	printOpenCodeAudit("broken", projBroken, agentsHome)
}

// TestPrintCopilotAudit_BrokenAndNotLinked covers .github/copilot-instructions.md
// broken-symlink and not-linked branches.
func TestPrintCopilotAudit_BrokenAndNotLinked(t *testing.T) {
	tmp := t.TempDir()

	// Broken symlink.
	projBroken := filepath.Join(tmp, "broken")
	os.MkdirAll(filepath.Join(projBroken, ".github"), 0755)
	os.Symlink(filepath.Join(tmp, "missing.md"), filepath.Join(projBroken, ".github", "copilot-instructions.md"))
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
	target := filepath.Join(tmp, "target.md")
	link := filepath.Join(tmp, "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
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
	os.Symlink(filepath.Join(tmp, "missing"), filepath.Join(dir, "bad"))
	warn := 0
	got := countManagedDirEntries(dir, &warn)
	if got < 1 {
		t.Errorf("expected at least one ok entry, got %d", got)
	}
	if warn < 1 {
		t.Errorf("expected at least one warn for broken symlink, got %d", warn)
	}
}

// TestPrintAgentsHomeGitStatusLine_RepoNoRemote covers the no-remote branch.
func TestPrintAgentsHomeGitStatusLine_RepoNoRemote(t *testing.T) {
	tmp := t.TempDir()
	// Fake .git directory so probeAgentsHomeGit treats this as a repo.
	os.MkdirAll(filepath.Join(tmp, ".git"), 0755)
	// `git remote get-url origin` will fail → no remote branch printed.
	printAgentsHomeGitStatusLine(tmp)
}

// TestRunStatus_AuditModeWithRegisteredProject covers runStatus audit-mode with
// a registered project and an installed claude platform — exercises the
// per-project printAudit + printSharedTargetRegistry full path.
func TestRunStatus_AuditModeWithRegisteredProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)
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
	os.Symlink(target, filepath.Join(projectPath, "AGENTS.md"))

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	os.Symlink(filepath.Join(agentsHome, "missing"), filepath.Join(claudeRules, "p--ghost.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	// runStatus is the cobra RunE; invoke via cmd to ensure flags route through.
	cmd := NewStatusCmd()
	cmd.SetArgs([]string{"--audit"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("runStatus audit: %v", err)
	}
}
