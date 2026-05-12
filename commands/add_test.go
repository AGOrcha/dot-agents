package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// ---------- isBackupArtifact ----------

func TestIsBackupArtifact(t *testing.T) {
	cases := []struct {
		name   string
		expect bool
	}{
		{"rules.mdc", false},
		{"AGENTS.md", false},
		{"rules.mdc.dot-agents-backup", true},
		{"rules.mdc.dot-agents-backup.dot-agents-backup", true},
		{"sonarqube_mcp_instructions.mdc.dot-agents-backup", true},
		{".dot-agents-backup", true},
	}
	for _, c := range cases {
		got := isBackupArtifact(c.name)
		if got != c.expect {
			t.Errorf("isBackupArtifact(%q) = %v, want %v", c.name, got, c.expect)
		}
	}
}

// ---------- checkExistingConfigFiles ----------

func TestCheckExistingConfigFiles_SkipsBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Write a backup artifact named AGENTS.md.dot-agents-backup
	artifact := filepath.Join(tmp, "AGENTS.md.dot-agents-backup")
	os.WriteFile(artifact, []byte("old"), 0644)

	// No actual AGENTS.md — only the artifact
	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	for _, f := range found {
		if strings.Contains(f, ".dot-agents-backup") {
			t.Errorf("checkExistingConfigFiles returned backup artifact: %s", f)
		}
	}
}

func TestCheckExistingConfigFiles_SkipsAlreadyManagedSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755)

	// Create a symlink that points into agentsHome (already managed)
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.WriteFile(target, []byte("rules"), 0644)
	linkPath := filepath.Join(tmp, "AGENTS.md")
	os.Symlink(target, linkPath)

	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	for _, f := range found {
		if f == linkPath {
			t.Errorf("checkExistingConfigFiles should have skipped already-managed symlink %s", f)
		}
	}
}

func TestCheckExistingConfigFiles_IncludesUnmanagedFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Write a real AGENTS.md that is not managed
	agentsMD := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("# instructions"), 0644)

	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	if len(found) != 1 || found[0] != agentsMD {
		t.Errorf("expected [%s], got %v", agentsMD, found)
	}
}

func TestCheckExistingConfigFiles_SkipsManagedCursorHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	project := "proj"
	source := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	rulePath := filepath.Join(tmp, ".cursor", "rules", "global--rules.mdc")
	if err := os.MkdirAll(filepath.Dir(rulePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, rulePath); err != nil {
		t.Fatal(err)
	}

	found := checkExistingConfigFiles(project, tmp, agentsHome)
	for _, f := range found {
		if f == rulePath {
			t.Fatalf("checkExistingConfigFiles should have skipped managed hardlink %s", f)
		}
	}
}

// ---------- scanExistingAIConfigs ----------

func TestScanExistingAIConfigs_ExcludesBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()

	// Create a real .mcp.json and a backup artifact alongside it
	os.WriteFile(filepath.Join(tmp, ".mcp.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmp, ".mcp.json.dot-agents-backup"), []byte("{}"), 0644)

	// Create a .cursor/rules/ dir with a rule and a backup artifact
	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)
	os.WriteFile(filepath.Join(rulesDir, "global--rules.mdc"), []byte("rule"), 0644)
	os.WriteFile(filepath.Join(rulesDir, "global--rules.mdc.dot-agents-backup"), []byte("rule"), 0644)

	results := scanExistingAIConfigs(tmp)
	for _, r := range results {
		if strings.Contains(r, ".dot-agents-backup") {
			t.Errorf("scanExistingAIConfigs returned backup artifact: %s", r)
		}
	}
}

// ---------- backupExistingConfigsList ----------

func TestBackupExistingConfigsList_CopyDeleteNoArtifactInProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create a regular file to be backed up
	agentsMD := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("# instructions"), 0644)

	count := backupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "myproject", "20260101-120000")

	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// Original should be gone from the project
	if _, err := os.Lstat(agentsMD); !os.IsNotExist(err) {
		t.Error("original file should have been deleted from the project tree")
	}

	// No *.dot-agents-backup should exist in the project
	backupPath := agentsMD + ".dot-agents-backup"
	if _, err := os.Lstat(backupPath); !os.IsNotExist(err) {
		t.Error("*.dot-agents-backup should NOT exist in the project tree")
	}

	// Canonical copy should exist in ~/.agents/resources/<project>/AGENTS.md
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	if _, err := os.Stat(activeTarget); err != nil {
		t.Errorf("active backup not found in resources: %v", err)
	}

	// Timestamped copy should exist
	tsTarget := filepath.Join(agentsHome, "resources", "myproject", "backups", "20260101-120000", "AGENTS.md")
	if _, err := os.Stat(tsTarget); err != nil {
		t.Errorf("timestamped backup not found in resources: %v", err)
	}
}

func TestBackupExistingConfigsList_SkipsBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create only a backup artifact (no real file)
	artifact := filepath.Join(tmp, "AGENTS.md.dot-agents-backup")
	os.WriteFile(artifact, []byte("old"), 0644)

	count := backupExistingConfigsList([]string{artifact}, tmp, agentsHome, "myproject", "20260101-120000")

	// Artifact should be skipped — count stays 0
	if count != 0 {
		t.Errorf("expected count=0 for artifact input, got %d", count)
	}

	// The artifact itself should still exist (we didn't touch it)
	if _, err := os.Lstat(artifact); err != nil {
		t.Error("backup artifact should not have been removed by the backup function")
	}

	// Nothing should appear in resources for this
	resourcesDir := filepath.Join(agentsHome, "resources", "myproject")
	if _, err := os.Stat(resourcesDir); !os.IsNotExist(err) {
		t.Error("resources dir should not have been created for backup artifact input")
	}
}

func TestBackupExistingConfigsList_RemovesSymlinkNoBackup(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create an unmanaged symlink (pointing somewhere outside agentsHome)
	target := filepath.Join(tmp, "external.md")
	os.WriteFile(target, []byte("x"), 0644)
	linkPath := filepath.Join(tmp, "AGENTS.md")
	os.Symlink(target, linkPath)

	count := backupExistingConfigsList([]string{linkPath}, tmp, agentsHome, "myproject", "ts")

	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// Symlink should be removed
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Error("unmanaged symlink should have been removed")
	}

	// No resources entry for symlinks (no content to preserve)
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	if _, err := os.Stat(activeTarget); !os.IsNotExist(err) {
		t.Error("symlinks should not produce a resources backup entry")
	}
}

// ---------- idempotence: second add sees no files to backup ----------

func TestCheckExistingConfigFiles_IdempotentAfterAdd(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Simulate post-add state: AGENTS.md is a symlink into agentsHome
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.WriteFile(target, []byte("# rules"), 0644)
	os.Symlink(target, filepath.Join(tmp, "AGENTS.md"))

	// No stale backup artifacts either
	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	if len(found) != 0 {
		t.Errorf("second add should find nothing to back up, got: %v", found)
	}
}

// ---------- NewAddCmd metadata ----------

func TestNewAddCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewAddCmd()
	if cmd.Use != "add <path>" {
		t.Errorf("unexpected Use=%q", cmd.Use)
	}
	if cmd.Flags().Lookup("name") == nil {
		t.Error("missing --name flag")
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected error when path arg missing")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected error when too many args supplied")
	}
}

// ---------- runAdd basic guards ----------

func TestRunAdd_MissingDirectoryErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(filepath.Join(tmp, "nonexistent"), "")
	if err == nil || !strings.Contains(err.Error(), "directory not found") {
		t.Errorf("expected directory-not-found error, got: %v", err)
	}
}

func TestRunAdd_InvalidProjectNameRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "valid-path")
	os.MkdirAll(projectPath, 0755)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "bad name with spaces")
	if err == nil || !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("expected invalid project name error, got: %v", err)
	}
}

func TestRunAdd_AlreadyRegisteredWithoutForceErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "already")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("already", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "already")
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected already-registered error, got: %v", err)
	}
}

func TestRunAdd_DryRunSkipsRegistration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "drytest")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.Save()

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, ""); err != nil {
		t.Fatalf("runAdd dry-run: %v", err)
	}

	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("drytest") != "" {
		t.Error("dry-run should not register project")
	}
}

// ---------- KG MCP config writers ----------

func TestKGConfigPath_UsesKGHomeEnv(t *testing.T) {
	t.Setenv("KG_HOME", "/custom/kg")
	got := kgConfigPath()
	want := "/custom/kg/self/config.yaml"
	if got != want {
		t.Errorf("kgConfigPath() = %q, want %q", got, want)
	}
}

func TestKGConfigPath_FallsBackToHome(t *testing.T) {
	t.Setenv("KG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := kgConfigPath()
	if !strings.HasSuffix(got, filepath.Join("knowledge-graph", "self", "config.yaml")) {
		t.Errorf("unexpected fallback path: %q", got)
	}
}

func TestWriteKGMCPConfigs_WritesThreeFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := writeKGMCPConfigs(tmp); err != nil {
		t.Fatalf("writeKGMCPConfigs: %v", err)
	}
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		servers, _ := parsed["servers"].(map[string]any)
		if servers == nil || servers["dot-agents-kg"] == nil {
			t.Errorf("%s missing dot-agents-kg server entry: %+v", name, parsed)
		}
	}
}

func TestWriteKGMCPConfigFile_MergesExistingServers(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	// Pre-existing file with another server
	pre := map[string]any{
		"servers": map[string]any{"other": map[string]any{"command": "foo"}},
	}
	data, _ := json.Marshal(pre)
	os.WriteFile(target, data, 0644)

	server := map[string]any{"command": "exe", "args": []string{"kg", "serve"}, "type": "stdio"}
	if err := writeKGMCPConfigFile(target, server); err != nil {
		t.Fatalf("writeKGMCPConfigFile: %v", err)
	}

	out, _ := os.ReadFile(target)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	servers, _ := parsed["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("expected to preserve existing servers.other entry")
	}
	if _, ok := servers["dot-agents-kg"]; !ok {
		t.Error("expected dot-agents-kg server entry")
	}
}

func TestEnsureGlobalKGMCPConfigs_NoopWhenKGAbsent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KG_HOME", filepath.Join(tmp, "no-kg-here"))

	agentsHome := filepath.Join(tmp, ".agents")
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Errorf("ensureGlobalKGMCPConfigs should be no-op when KG missing: %v", err)
	}
	// No mcp/global dir should have been created
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err == nil {
		t.Error("expected no MCP config to be written")
	}
}

func TestEnsureGlobalKGMCPConfigs_WritesWhenKGPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	kgHome := filepath.Join(tmp, "kgroot")
	os.MkdirAll(filepath.Join(kgHome, "self"), 0755)
	os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("k: v\n"), 0644)
	t.Setenv("KG_HOME", kgHome)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("ensureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err != nil {
		t.Errorf("expected claude.json: %v", err)
	}
}

func TestEnsureProjectKGMCPConfigs_NoopWithoutManifest(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	agentsHome := filepath.Join(tmp, ".agents")

	if err := ensureProjectKGMCPConfigs("p", projectPath, agentsHome); err != nil {
		t.Errorf("expected no-op when manifest missing, got: %v", err)
	}
}

func TestEnsureProjectKGMCPConfigs_WritesWhenKGDeclared(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	rc := &config.AgentsRC{
		Version: 1,
		Project: "p",
		KG:      &config.AgentsRCKG{Backend: "sqlite"},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	if err := ensureProjectKGMCPConfigs("p", projectPath, agentsHome); err != nil {
		t.Fatalf("ensureProjectKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "p", "claude.json")); err != nil {
		t.Errorf("expected project KG MCP config: %v", err)
	}
}

// ---------- isManagedCursorRuleRel / isManagedProjectOutput ----------

func TestIsManagedCursorRuleRel(t *testing.T) {
	cases := []struct {
		project, rel string
		want         bool
	}{
		{"proj", ".cursor/rules/global--foo.mdc", true},
		{"proj", ".cursor/rules/proj--bar.mdc", true},
		{"proj", ".cursor/rules/other--baz.mdc", false},
		{"proj", ".cursor/rules/loose.mdc", false},
		{"proj", "other/path.md", false},
	}
	for _, c := range cases {
		got := isManagedCursorRuleRel(c.project, c.rel)
		if got != c.want {
			t.Errorf("isManagedCursorRuleRel(%q,%q)=%v, want %v", c.project, c.rel, got, c.want)
		}
	}
}

func TestIsManagedProjectOutput_ManagedHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	project := "proj"
	source := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	os.MkdirAll(filepath.Dir(source), 0755)
	os.WriteFile(source, []byte("rule"), 0644)

	// A cursor-rules-managed hardlink should be detected as managed.
	ruleLink := filepath.Join(tmp, ".cursor", "rules", "global--rules.mdc")
	os.MkdirAll(filepath.Dir(ruleLink), 0755)
	if err := os.Link(source, ruleLink); err != nil {
		t.Fatal(err)
	}
	if !isManagedProjectOutput(project, tmp, ruleLink, agentsHome) {
		t.Error("expected managed cursor hardlink to be detected as managed output")
	}

	// A loose unmanaged file should NOT be detected
	loose := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(loose, []byte("hi"), 0644)
	if isManagedProjectOutput(project, tmp, loose, agentsHome) {
		t.Error("loose AGENTS.md should not be detected as managed")
	}
}

// ---------- mirrorBackup ----------

func TestMirrorBackup_NoTimestampStillWritesActive(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(src, []byte("hello"), 0644)

	mirrorBackup("proj", tmp, src, "")

	active := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if data, err := os.ReadFile(active); err != nil || string(data) != "hello" {
		t.Errorf("expected active backup with content 'hello', got data=%q err=%v", string(data), err)
	}
	// No backups dir should be created when timestamp is empty
	if _, err := os.Stat(filepath.Join(agentsHome, "resources", "proj", "backups")); err == nil {
		t.Error("expected no backups/ subdir when timestamp empty")
	}
}

func TestMirrorBackup_NestedRelativePath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	srcDir := filepath.Join(tmp, ".github")
	os.MkdirAll(srcDir, 0755)
	src := filepath.Join(srcDir, "copilot-instructions.md")
	os.WriteFile(src, []byte("instr"), 0644)

	mirrorBackup("proj", tmp, src, "20260101-000000")

	want := filepath.Join(agentsHome, "resources", "proj", ".github", "copilot-instructions.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected nested mirror: %v", err)
	}
	ts := filepath.Join(agentsHome, "resources", "proj", "backups", "20260101-000000", ".github", "copilot-instructions.md")
	if _, err := os.Stat(ts); err != nil {
		t.Errorf("expected timestamped mirror: %v", err)
	}
}

// ---------- restoreFromResourcesCounted ----------

func TestRestoreFromResourcesCounted_NoResourcesDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if n := restoreFromResourcesCounted("ghost", tmp); n != 0 {
		t.Errorf("expected 0 restores for missing resources, got %d", n)
	}
}

func TestRestoreFromResourcesCounted_RestoresAGENTSMD(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// Stage a backed-up AGENTS.md in resources
	resourceFile := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	os.MkdirAll(filepath.Dir(resourceFile), 0755)
	if err := os.WriteFile(resourceFile, []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}

	n := restoreFromResourcesCounted("proj", tmp)
	if n != 1 {
		t.Errorf("expected 1 restore, got %d", n)
	}
	// Should have produced rules/proj/agents.md in agentsHome
	want := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected restored agents.md at %s: %v", want, err)
	}
}

func TestRestoreFromResourcesCounted_SkipsBackupsDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	backupFile := filepath.Join(agentsHome, "resources", "proj", "backups", "20260101", "AGENTS.md")
	os.MkdirAll(filepath.Dir(backupFile), 0755)
	os.WriteFile(backupFile, []byte("old"), 0644)

	if n := restoreFromResourcesCounted("proj", tmp); n != 0 {
		t.Errorf("expected 0 restores for backups-only resources dir, got %d", n)
	}
}
