// Package projectsync provides shared helpers for creating project directory
// structures, restoring resource files, writing refresh markers, and managing
// gitignore entries. These were extracted from commands/add.go,
// commands/refresh.go, and commands/init.go so that multiple command
// implementations can share them without duplication.
package projectsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// CopyFile copies src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	return os.WriteFile(dst, data, 0644)
}

// EnsureGitignoreEntry appends entry to <repoPath>/.gitignore if it is not
// already present. Silent no-op if the file cannot be opened or read.
func EnsureGitignoreEntry(repoPath, entry string) {
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == entry {
				return
			}
		}
	}
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, entry)
}

// CreateProjectDirs creates the standard per-project bucket directories inside
// AgentsHome. It is safe to call repeatedly; MkdirAll is idempotent.
func CreateProjectDirs(project string) error {
	agentsHome := config.AgentsHome()
	dirs := []string{
		filepath.Join(agentsHome, "rules", project),
		filepath.Join(agentsHome, "settings", project),
		filepath.Join(agentsHome, "mcp", project),
		filepath.Join(agentsHome, "skills", project),
		filepath.Join(agentsHome, "agents", project),
		filepath.Join(agentsHome, "hooks", project),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return nil
}

// RefreshMarkerContent returns the byte content for a .agents-refresh marker
// file, suitable for os.WriteFile.
func RefreshMarkerContent(version, commit, describe string) []byte {
	now := time.Now().UTC().Format(time.RFC3339)
	content := "# da refresh marker — do not edit\n"
	content += "version=" + version + "\n"
	if commit != "" {
		content += "commit=" + commit + "\n"
	}
	if describe != "" {
		content += "describe=" + describe + "\n"
	}
	content += "refreshed_at=" + now + "\n"
	return []byte(content)
}

// WriteRefreshToLock ensures .agentsrc.json exists (generating one via a
// project scan when absent, mirroring the old manifest-bootstrap behavior)
// and re-saves it — which also strips any legacy top-level "refresh" key a
// pre-refresh-metadata-to-lock manifest may still carry, since AgentsRC no
// longer has a Refresh field to round-trip it through (config-distribution-
// model §7A / refresh-metadata-to-lock). It then stamps the refresh metadata
// (version, commit, describe, refreshedAt) into .agentsrc.lock's "refresh"
// section — refresh metadata is resolved state about the project, not
// manifest content, so it belongs in the lock, not the committed manifest —
// and removes a legacy .agents-refresh marker file if present.
func WriteRefreshToLock(projectName, projectPath, version, commit, describe string) error {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		rc, err = config.GenerateAgentsRC(projectName, projectPath)
		if err != nil {
			return err
		}
	}
	if err := rc.Save(projectPath); err != nil {
		return err
	}
	meta := config.RefreshMetadata{
		Version:     version,
		Commit:      commit,
		Describe:    describe,
		RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := config.WriteRefreshLock(projectPath, meta); err != nil {
		return err
	}
	legacy := filepath.Join(projectPath, ".agents-refresh")
	if err := os.Remove(legacy); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
