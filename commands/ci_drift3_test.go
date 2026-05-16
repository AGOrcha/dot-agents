package commands

// Tests in this file cover pr3b-only branches in import.go, doctor.go, and
// session_stats.go that pass locally but skip on CI. The pattern is:
//   - import.go: needs at least one global or project import candidate present
//     on disk so collectImportCandidates returns a non-empty slice, exercising
//     the sortImportCandidates / foldImportCandidates / processImportCandidate
//     / relinkImportedProjects branches.
//   - doctor.go: needs a `claude` shim in PATH that returns a version string
//     on --version so p.Version() at doctor.go:62-65 returns non-empty.
//   - session_stats.go: needs ~/.claude/stats-cache.json so claudeReadUsageStats
//     returns a non-nil stats block and renderPlatformStats is called.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// TestRunImport_WithSeededGlobalCandidatesExercisesFoldAndRelink covers
// import.go:316-326 (sort/fold/Success), 332-336 (foldImportCandidates body),
// and 1434 (relinkImportedProjects → CreateLinks on installed platform). The
// HOME is seeded with at least one entry from globalImportSingles AND
// platform install signals so CreateLinks fires for each installed platform.
func TestRunImport_WithSeededGlobalCandidatesExercisesFoldAndRelink(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// At least one global single — settings.json — present so the import
	// pipeline has a candidate to sort + process + relink.
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "settings.json"), []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Managed project so relinkImportedProjects has a project to iterate.
	projectPath := filepath.Join(tmp, "importproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("importproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runImport("", "all"); err != nil {
		t.Fatalf("runImport: %v", err)
	}
}

// TestRunImport_ProjectScopeWithCandidateExercisesWalkPath covers
// walkedImportCandidate's success path (import.go:563-565, 584-589). It seeds
// a managed AGENTS.md inside the project root so filepath.WalkDir surfaces it
// as a candidate and walkedImportCandidate returns true.
func TestRunImport_ProjectScopeWithCandidateExercisesWalkPath(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "walkproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// AGENTS.md is a canonical project import target; placing it triggers
	// walkedImportCandidate's success branch.
	if err := os.WriteFile(filepath.Join(projectPath, "AGENTS.md"), []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("walkproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runImport("", "project"); err != nil {
		t.Fatalf("runImport project scope: %v", err)
	}
}

// TestRunDoctor_WithClaudeVersionShimCoversInstalledWithVersionBranch covers
// doctor.go:62-65 (`ver := p.Version(); if ver != ""`). On CI bare runners
// `claude --version` errors and the else-branch fires. The shim returns a
// real version string so the if-branch is exercised.
func TestRunDoctor_WithClaudeVersionShimCoversInstalledWithVersionBranch(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)

	// Overwrite the shim's response so `claude --version` returns a real
	// version line. seedAllPlatformInstallSignals seeds `agent`/`codex`/
	// `opencode` shims; we add a `claude` shim that mimics a real version.
	binDir := filepath.Join(tmp, "fakebin")
	claudeShim := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeShim, []byte("#!/bin/sh\necho 'claude 1.2.3 (ci-drift)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDoctor(nil, nil); err != nil {
		t.Errorf("runDoctor with claude version shim: %v", err)
	}
}

// TestRunSessionStats_WithSeededStatsCacheCoversRenderBranch covers
// session_stats.go:55 (`renderPlatformStats(stats)` when stats != nil). On CI
// bare runners no ~/.claude/stats-cache.json exists, so claudeReadUsageStats
// returns nil and the function takes the `(no data available)` branch.
func TestRunSessionStats_WithSeededStatsCacheCoversRenderBranch(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)

	// Seed claude stats-cache.json with one model + one daily activity entry.
	cache := `{
		"totalSessions": 1,
		"totalMessages": 2,
		"modelUsage": {
			"claude-sonnet-4-6": {
				"inputTokens": 100,
				"outputTokens": 200,
				"cacheReadInputTokens": 50,
				"cacheCreationInputTokens": 25
			}
		},
		"dailyActivity": [
			{"date": "2026-05-15", "messageCount": 2, "sessionCount": 1, "toolCallCount": 4}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "stats-cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSessionStats(nil, nil); err != nil {
		t.Errorf("runSessionStats with seeded stats cache: %v", err)
	}
}
