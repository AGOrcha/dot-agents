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
