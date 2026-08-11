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

// WriteRefreshToLock stamps the refresh metadata (version, commit, describe,
// refreshedAt) into .agentsrc.lock's "refresh" section — refresh metadata is
// resolved state about the project, not manifest content, so it belongs in the
// lock, not the committed manifest — and removes a legacy .agents-refresh
// marker file if present.
//
// It deliberately does NOT touch .agentsrc.json. The manifest is user-authored
// and only the lock is machine-written by `da refresh` / `da config sync`.
// This function previously load-and-re-saved the manifest on every refresh to
// opportunistically strip a legacy top-level "refresh" key; that side effect
// corrupted manifests, because a re-save round-trips the whole file through
// AgentsRC and re-emits keys the author never wrote. Concretely it injected
// `"hooks": false, "mcp": false, "settings": false` into any manifest that
// omitted them, and since the layer merge is key-presence driven those explicit
// falses then beat an org layer's `true` — silently disabling hooks/mcp/settings
// projection for every repo relying on org defaults.
//
// The legacy-"refresh"-key strip is still available, but only behind its
// explicit, backed-up, opt-in path: `da config migrate`. A resolve/projection
// command must not rewrite user-authored files as a side effect. The former
// projectName parameter went away with the manifest-generate path that was its
// only consumer.
func WriteRefreshToLock(projectPath, version, commit, describe string) error {
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
