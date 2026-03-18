package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	found := checkExistingConfigFiles(tmp, agentsHome)
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

	found := checkExistingConfigFiles(tmp, agentsHome)
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

	found := checkExistingConfigFiles(tmp, agentsHome)
	if len(found) != 1 || found[0] != agentsMD {
		t.Errorf("expected [%s], got %v", agentsMD, found)
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
	found := checkExistingConfigFiles(tmp, agentsHome)
	if len(found) != 0 {
		t.Errorf("second add should find nothing to back up, got: %v", found)
	}
}
