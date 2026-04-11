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

	"github.com/NikashPrakash/dot-agents/internal/config"
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
	content := "# dot-agents refresh marker — do not edit\n"
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

// WriteRefreshMarker writes a .agents-refresh marker into projectPath and
// ensures the path is listed in .gitignore.
func WriteRefreshMarker(projectPath, version, commit, describe string) {
	markerPath := filepath.Join(projectPath, ".agents-refresh")
	content := RefreshMarkerContent(version, commit, describe)
	os.WriteFile(markerPath, content, 0644) //nolint:errcheck
	EnsureGitignoreEntry(projectPath, ".agents-refresh")
}
