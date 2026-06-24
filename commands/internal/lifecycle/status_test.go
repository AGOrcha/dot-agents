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
	"golang.org/x/sys/execabs"
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

// auditNameFmt is the default name format passed to printSymlinkDirAudit in
// tests (the entry name is printed verbatim). Shared const so the literal is
// not duplicated across cases (SonarCloud go:S1192).
const auditNameFmt = "%s"

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

// TestPrintAgentsHomeGitStatusLine_WithRemote drives the git-remote rendering
// branch (status.go 254-259 canonicalize + 270-273 "with remote" line). It
// initializes a real repo via `git init` and configures an origin URL so
// gitremote.ReadOriginURL/CanonicalRepoID resolve to a non-empty remote.
func TestPrintAgentsHomeGitStatusLine_WithRemote(t *testing.T) {
	if _, err := execabs.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmp := t.TempDir()
	run := func(args ...string) {
		cmd := execabs.Command("git", append([]string{"-C", tmp}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+tmp)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("remote", "add", "origin", "git@github.com:AGOrcha/dot-agents.git")

	probe := probeAgentsHomeGit(tmp)
	if !probe.IsRepo {
		t.Fatal("expected IsRepo=true for initialized repo")
	}
	if probe.Remote == "" {
		t.Errorf("expected non-empty canonical remote, got %q", probe.Remote)
	}
	// Renders the "with remote" status line without panic.
	printAgentsHomeGitStatusLine(tmp)
	// statusGitInfo mirrors the same probe into JSON form.
	if g := statusGitInfo(tmp); !g.Initialized || g.Remote == "" {
		t.Errorf("expected initialized git info with remote, got %+v", g)
	}
}

// TestCountClaudeRulesDir_HardlinkedRule covers the Windows-style hard-linked
// managed rule branch (status.go 363-365: HasMultipleHardLinks → ok++). The
// HasMultipleHardLinks seam is overridden so the branch is exercised on every
// OS without relying on host hard-link semantics.
func TestCountClaudeRulesDir_HardlinkedRule(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, ".claude", "rules")
	os.MkdirAll(rulesDir, 0755)
	// A plain regular file (not a managed symlink) so managedLinkBroken
	// reports isLink=false and control falls through to the hard-link check.
	os.WriteFile(filepath.Join(rulesDir, "hardrule.md"), []byte("x"), 0644)

	prev := HasMultipleHardLinks
	HasMultipleHardLinks = func(string) bool { return true }
	defer func() { HasMultipleHardLinks = prev }()

	ok, warn := countClaudeRulesDir(rulesDir)
	if ok != 1 || warn != 0 {
		t.Errorf("expected (1,0) for hard-linked managed rule, got (%d,%d)", ok, warn)
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
// Per §7A.6 status no longer renders any manifest summary line — that config
// inspection moved to `da config explain`. The former
// TestPrintStatusProjectManifestSummary_* cases were removed with the
// printStatusProjectManifestSummary helper they exercised. A text-mode
// assertion that status emits NO manifest/lock/last-refreshed output lives in
// TestRunStatus_TextOmitsConfigInspection below.

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

	// §7A.6: status JSON sheds all config inspection. The manifest_found,
	// last_refreshed, and lock keys must no longer appear in the marshaled
	// project entry — `da config explain` owns effective-config detail now.
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, gone := range []string{"manifest_found", "last_refreshed", `"lock"`} {
		if strings.Contains(string(data), gone) {
			t.Errorf("status JSON must not contain %s after §7A.6 reshape; got: %s", gone, string(data))
		}
	}
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

// TestPrintSymlinkDirAudit_MissingDir covers the os.ReadDir error early-return
// (a directory that does not exist returns (0,0) without printing).
func TestPrintSymlinkDirAudit_MissingDir(t *testing.T) {
	ok, broken := printSymlinkDirAudit(filepath.Join(t.TempDir(), "nope"), "empty", auditNameFmt)
	if ok != 0 || broken != 0 {
		t.Errorf("expected (0,0) for missing dir, got (%d,%d)", ok, broken)
	}
}

// TestPrintSymlinkDirAudit_HealthyAndBroken drives the loop body: a healthy
// managed symlink (ok++ / statusAuditLinkOkFormat branch), a broken managed
// symlink (broken++ / statusAuditLinkBrokenFormat branch), and a plain
// non-symlink file (isLink=false continue branch). This is the dispatch the
// AuditPrinter refactor left under-covered.
func TestPrintSymlinkDirAudit_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "agent")
	os.MkdirAll(dir, 0755)

	// Healthy managed symlink → ok++.
	target := filepath.Join(tmp, "target")
	os.WriteFile(target, []byte("x"), 0644)
	linktest.Link(t, target, filepath.Join(dir, "good"))

	// Broken managed symlink → broken++.
	linktest.DanglingLink(t, filepath.Join(dir, "bad"))

	// Plain file (not a managed link) → continue branch, neither counted.
	os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0644)

	out := captureStatusStdout(t, func() {
		ok, broken := printSymlinkDirAudit(dir, "empty", auditNameFmt)
		if ok != 1 {
			t.Errorf("expected ok=1, got %d", ok)
		}
		if broken != 1 {
			t.Errorf("expected broken=1, got %d", broken)
		}
	})
	if !strings.Contains(out, "good") || !strings.Contains(out, "bad") {
		t.Errorf("expected both link names in audit output, got:\n%s", out)
	}
	if !strings.Contains(out, "(broken)") {
		t.Errorf("expected broken marker in output, got:\n%s", out)
	}
}

// TestPrintSymlinkDirAudit_Exported pins the exported PrintSymlinkDirAudit
// wrapper used by the legacy commands/seams_test callers — it must delegate to
// the unexported impl and return the same counts.
func TestPrintSymlinkDirAudit_Exported(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "agent")
	os.MkdirAll(dir, 0755)
	target := filepath.Join(tmp, "t")
	os.WriteFile(target, []byte("x"), 0644)
	linktest.Link(t, target, filepath.Join(dir, "good"))

	ok, broken := PrintSymlinkDirAudit(dir, "empty", auditNameFmt)
	if ok != 1 || broken != 0 {
		t.Errorf("expected (1,0) from exported wrapper, got (%d,%d)", ok, broken)
	}
}

// TestResolveLinkDest_Branches covers all three return paths of
// resolveLinkDest: empty dest, already-absolute dest, and the relative-dest
// branch (line 119) that joins against the link's directory. The relative
// branch was previously only reachable on platforms whose symlinks resolve
// relative, so this pins it directly.
func TestResolveLinkDest_Branches(t *testing.T) {
	if got := resolveLinkDest("/links/a", ""); got != "" {
		t.Errorf("empty dest: expected \"\", got %q", got)
	}
	abs := filepath.Join(string(filepath.Separator), "abs", "target")
	if got := resolveLinkDest("/links/a", abs); got != abs {
		t.Errorf("abs dest: expected %q, got %q", abs, got)
	}
	linkPath := filepath.Join("links", "sub", "a")
	want := filepath.Clean(filepath.Join("links", "sub", "rel", "target"))
	if got := resolveLinkDest(linkPath, filepath.Join("rel", "target")); got != want {
		t.Errorf("relative dest: expected %q, got %q", want, got)
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

// TestRunStatus_TextOmitsConfigInspection is the positive coverage for §7A.6:
// even when a project's manifest carries a refresh timestamp and declared
// skills, text-mode status renders fleet/link-health only — it must NOT emit
// any "last refreshed", "manifest", or "lock" config-inspection line (that
// detail now belongs to `da config explain`).
func TestRunStatus_TextOmitsConfigInspection(t *testing.T) {
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
		Skills:  []string{"s1"},
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

	out := captureStatusStdout(t, func() {
		if err := runStatus(false, "", stdStatusConfigLoader{}, false); err != nil {
			t.Errorf("runStatus: %v", err)
		}
	})
	for _, gone := range []string{"last refreshed", "manifest", "lock"} {
		if strings.Contains(out, gone) {
			t.Errorf("status text must not contain %q after §7A.6 reshape; output:\n%s", gone, out)
		}
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

// Per §7A.6 status sheds lockfile inspection (printStatusProjectLockSummary /
// buildStatusJSONLock removed). Lockfile drift is now surfaced by `da doctor`
// (read-only) and reconciled by `da config sync`; the underlying drift logic is
// covered by internal/config's LockDrift tests. The former
// TestPrintStatusProjectLockSummary_* / TestBuildStatusJSONLock_* cases and
// their seedLockProject helper were removed with the helpers they exercised.

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
