package commands

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// registryTestPlatform is a minimal Platform stub for commands-layer registry tests.
type registryTestPlatform struct {
	id         string
	collectErr error
}

func (p registryTestPlatform) ID() string                      { return p.id }
func (p registryTestPlatform) DisplayName() string             { return p.id }
func (p registryTestPlatform) IsInstalled() bool               { return true }
func (p registryTestPlatform) Version() string                 { return "" }
func (p registryTestPlatform) CreateLinks(_, _ string) error   { return nil }
func (p registryTestPlatform) RemoveLinks(_, _ string) error   { return nil }
func (p registryTestPlatform) HasDeprecatedFormat(string) bool { return false }
func (p registryTestPlatform) DeprecatedDetails(string) string { return "" }
func (p registryTestPlatform) SharedTargetIntents(string) ([]platform.ResourceIntent, error) {
	return nil, p.collectErr
}

func TestSharedTargetRegistryPlanLines_EmptyPlatforms(t *testing.T) {
	lines, err := sharedTargetRegistryPlanLines("proj", "/tmp/repo", nil)
	if err != nil {
		t.Fatalf("sharedTargetRegistryPlanLines: %v", err)
	}
	if lines != nil {
		t.Fatalf("expected nil lines, got %#v", lines)
	}
}

func TestSharedTargetRegistryPlanLines_MatchesDryRunSharedTargetPlanLines(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	plats := []platform.Platform{platform.NewCodex()}
	want, err := platform.DryRunSharedTargetPlanLines("proj", repo, plats)
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	got, err := sharedTargetRegistryPlanLines("proj", repo, plats)
	if err != nil {
		t.Fatalf("sharedTargetRegistryPlanLines: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d (%v) want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestSharedTargetRegistryPlanLines_PropagatesSharedIntentError(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	wrapped := errors.New("boom")
	_, err := sharedTargetRegistryPlanLines("proj", repo, []platform.Platform{
		registryTestPlatform{id: "bad", collectErr: wrapped},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wrapped) {
		t.Fatalf("errors.Is: got %v want %v", err, wrapped)
	}
	if !strings.Contains(err.Error(), "bad shared intents") {
		t.Fatalf("error = %q, want platform wrap", err.Error())
	}
}

func TestStatusShowsPluginsSection(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(filepath.Join(agentsHome, "plugins", "global", "my-plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "plugins", "global", "my-plugin", platform.PluginManifestName), []byte(`schema_version: 1
kind: native
name: my-plugin
platforms: [opencode]
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runStatus(false, ""); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "Plugins") {
		t.Fatalf("status output missing Plugins section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "my-plugin") {
		t.Fatalf("status output missing plugin name:\n%s", rendered)
	}
}

func TestStatusJSONOutput(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}}
	cfg.AddProject("repo", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	prevJSON := Flags.JSON
	Flags.JSON = true
	defer func() { Flags.JSON = prevJSON }()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	if err := runStatus(false, ""); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdout = oldStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	var report statusJSONReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("status output is not valid json: %v\n%s", err, string(out))
	}
	if report.AgentsHome != agentsHome {
		t.Fatalf("agents_home = %q, want %q", report.AgentsHome, agentsHome)
	}
	if len(report.Projects) != 1 || report.Projects[0].Name != "repo" {
		t.Fatalf("unexpected projects payload: %#v", report.Projects)
	}
	if !report.Projects[0].PathExists {
		t.Fatalf("expected project path to exist in report: %#v", report.Projects[0])
	}
}

func TestReadRefreshTimestampPrefersAgentsRCMetadata(t *testing.T) {
	projectPath := t.TempDir()
	rc := &config.AgentsRC{
		Version: 1,
		Project: "proj",
		Sources: []config.Source{{Type: "local"}},
	}
	rc.SetRefreshMetadata("1.0.0", "abc123", "v1.0.0", time.Date(2026, 3, 31, 5, 18, 11, 0, time.UTC))
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".agents-refresh"), []byte("refreshed_at=2020-01-01T00:00:00Z\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := readRefreshTimestamp(projectPath); got != "2026-03-31 05:18 UTC" {
		t.Fatalf("readRefreshTimestamp() = %q, want %q", got, "2026-03-31 05:18 UTC")
	}
}

func TestReadRefreshTimestampFallsBackToLegacyMarker(t *testing.T) {
	projectPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectPath, ".agents-refresh"), []byte("refreshed_at=2026-03-31T07:45:00Z\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := readRefreshTimestamp(projectPath); got != "2026-03-31 07:45 UTC" {
		t.Fatalf("readRefreshTimestamp() = %q, want %q", got, "2026-03-31 07:45 UTC")
	}
}

func TestProbeAgentsHomeGit_NotARepo(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "no-git-here")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if g := probeAgentsHomeGit(agentsHome); g.IsRepo {
		t.Fatalf("expected no repo, got %#v", g)
	}
}

func TestProbeAgentsHomeGit_InitRepo(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "-C", agentsHome, "init")
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	g := probeAgentsHomeGit(agentsHome)
	if !g.IsRepo {
		t.Fatalf("expected repo after init, got %#v", g)
	}
	if strings.TrimSpace(g.Branch) == "" {
		t.Fatalf("expected non-empty branch, got %#v", g)
	}
	if g.Remote != "" {
		t.Fatalf("unexpected remote before remote add: %q", g.Remote)
	}
}

func TestCollectProjectTextBadges_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "proj")
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	badges := collectProjectTextBadges(projectPath, agentsHome)
	if len(badges) != 5 {
		t.Fatalf("len(badges)=%d want 5 (%#v)", len(badges), badges)
	}
	want := []string{"Cursor", "Claude", "Codex", "OpenCode", "Copilot"}
	for i := range want {
		if badges[i].name != want[i] {
			t.Fatalf("badges[%d].name=%q want %q", i, badges[i].name, want[i])
		}
		if badges[i].present || badges[i].broken {
			t.Fatalf("empty project: badge %q should be inactive, got %#v", want[i], badges[i])
		}
	}
}
