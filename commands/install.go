package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewInstallCmd() *cobra.Command {
	var generate bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up project from .agentsrc.json manifest",
		Long: `Reads .agentsrc.json in the current directory and wires up all declared
resources (skills, rules, agents, hooks, MCP configs, settings) by creating
the appropriate platform-specific symlinks and hard links.

Commit .agentsrc.json to git so any contributor can run 'dot-agents install'
after cloning — no manual init or sync required.

Use --generate to create .agentsrc.json from the current ~/.agents/ state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if generate {
				return runInstallGenerate()
			}
			return runInstall(strict)
		},
	}
	cmd.Flags().BoolVar(&generate, "generate", false, "Create .agentsrc.json from current ~/.agents/ state")
	cmd.Flags().BoolVar(&strict, "strict", false, "Fail if any declared resource is not found")
	return cmd
}

// ─── runInstall ──────────────────────────────────────────────────────────────

func runInstall(strict bool) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ui.Header("dot-agents install")

	// 1. Read manifest
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Error(config.AgentsRCFile + " not found in current directory")
			fmt.Fprintln(os.Stdout, "  Run 'dot-agents install --generate' to create one, or")
			fmt.Fprintln(os.Stdout, "  run 'dot-agents add .' to register this project first.")
			return fmt.Errorf("manifest not found")
		}
		return fmt.Errorf("reading %s: %w", config.AgentsRCFile, err)
	}

	// 2. Verify ~/.agents/ initialized
	agentsHome := config.AgentsHome()
	if _, err := os.Stat(filepath.Join(agentsHome, "config.json")); err != nil {
		return fmt.Errorf("~/.agents/ not initialized — run 'dot-agents init' first")
	}

	// Resolve project name
	projectName := rc.Project
	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}

	fmt.Fprintf(os.Stdout, "Project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path:    %s\n", ui.DimText(config.DisplayPath(projectPath)))

	// 3. Resolve sources and populate ~/.agents/ with remote resources
	ui.Section("Resolving sources")
	resolvedSources, err := resolveSources(rc.Sources)
	if err != nil && strict {
		return err
	}

	// 4. Link skills and agents from sources into ~/.agents/{type}/{project}/
	if len(resolvedSources) > 0 {
		for _, skillName := range rc.Skills {
			if err := linkResourceFromSources("skills", skillName, projectName, resolvedSources); err != nil {
				msg := fmt.Sprintf("skill '%s' not found in any source", skillName)
				if strict {
					return fmt.Errorf("%s (--strict mode)", msg)
				}
				ui.Bullet("warn", msg+" — skipping")
			}
		}
		for _, agentName := range rc.Agents {
			if err := linkResourceFromSources("agents", agentName, projectName, resolvedSources); err != nil {
				msg := fmt.Sprintf("agent '%s' not found in any source", agentName)
				if strict {
					return fmt.Errorf("%s (--strict mode)", msg)
				}
				ui.Bullet("warn", msg+" — skipping")
			}
		}
	}

	// 5. Ensure project dirs in ~/.agents/
	if !Flags.DryRun {
		if err := createProjectDirs(projectName); err != nil {
			return err
		}
		ui.Bullet("ok", "Ensured ~/.agents/ project directories")
	} else {
		ui.DryRun("create ~/.agents/ directories for '" + projectName + "'")
	}

	// 6. Register project in config.json
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.GetProjectPath(projectName) == "" {
		if !Flags.DryRun {
			cfg.AddProject(projectName, projectPath)
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			ui.Bullet("ok", "Registered '"+projectName+"' in config.json")
		} else {
			ui.DryRun("register '" + projectName + "' in config.json")
		}
	} else {
		ui.Bullet("skip", "Already registered in config.json")
	}

	// 7. Create platform links (handles rules, hooks, mcp, settings, skills, agents)
	ui.Section("Creating platform links")
	config.SetWindowsMirrorContext(projectPath)

	for _, p := range platform.All() {
		if !p.IsInstalled() {
			if Flags.Verbose {
				ui.Skip(p.DisplayName() + " (not installed)")
			}
			continue
		}
		if Flags.DryRun {
			ui.DryRun("refresh " + p.DisplayName() + " links")
			continue
		}
		if err := p.CreateLinks(projectName, projectPath); err != nil {
			ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
		} else {
			ui.Bullet("ok", p.DisplayName()+" links created")
		}
	}

	// 8. Write .agents-refresh marker
	if !Flags.DryRun {
		writeRefreshMarker(projectPath, Commit, Describe)
		ensureGitignoreEntry(projectPath, ".agents-refresh")
		ui.Bullet("ok", "Wrote .agents-refresh marker")
	}

	ui.SuccessBox(
		fmt.Sprintf("Project '%s' installed successfully!", projectName),
		"Check links: dot-agents status --audit",
		"Update manifest: dot-agents install --generate",
	)
	return nil
}

// ─── runInstallGenerate ──────────────────────────────────────────────────────

func runInstallGenerate() error {
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ui.Header("dot-agents install --generate")

	// Derive project name from config.json or directory name
	projectName := findProjectByPath(projectPath)
	if projectName == "" {
		projectName = filepath.Base(projectPath)
		ui.Info("Project not registered — using directory name: " + projectName)
	}

	rc, err := config.GenerateAgentsRC(projectName, projectPath)
	if err != nil {
		return fmt.Errorf("generating manifest: %w", err)
	}

	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Would write %s with:", config.AgentsRCFile))
		ui.DryRun(fmt.Sprintf("  project:  %s", projectName))
		ui.DryRun(fmt.Sprintf("  skills:   %v", rc.Skills))
		ui.DryRun(fmt.Sprintf("  rules:    %v", rc.Rules))
		ui.DryRun(fmt.Sprintf("  agents:   %v", rc.Agents))
		ui.DryRun(fmt.Sprintf("  hooks:    %v", rc.Hooks))
		ui.DryRun(fmt.Sprintf("  mcp:      %v", rc.MCP))
		ui.DryRun(fmt.Sprintf("  settings: %v", rc.Settings))
		return nil
	}

	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("writing %s: %w", config.AgentsRCFile, err)
	}

	ui.Success("Generated " + config.AgentsRCFile)
	fmt.Fprintf(os.Stdout, "  %sSkills: %d, Rules: %d, Agents: %d%s\n",
		ui.Dim, len(rc.Skills), len(rc.Rules), len(rc.Agents), ui.Reset)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Next steps:")
	fmt.Fprintf(os.Stdout, "  1. Review:  cat %s\n", config.AgentsRCFile)
	fmt.Fprintf(os.Stdout, "  2. Commit:  git add %s && git commit -m 'Add dot-agents manifest'\n", config.AgentsRCFile)
	fmt.Fprintln(os.Stdout, "  3. Others:  dot-agents install   (after cloning)")
	return nil
}

// ─── source resolution ───────────────────────────────────────────────────────

// resolveSources resolves each source to a local root directory.
func resolveSources(sources []config.Source) ([]string, error) {
	var resolved []string
	var firstErr error

	for _, src := range sources {
		switch src.Type {
		case "local":
			root := config.AgentsHome()
			if src.Path != "" {
				root = config.ExpandPath(src.Path)
			}
			resolved = append(resolved, root)
			ui.Bullet("ok", "Local source: "+config.DisplayPath(root))

		case "git":
			if src.URL == "" {
				ui.Bullet("warn", "Git source missing 'url' — skipping")
				continue
			}
			cacheDir, err := fetchGitSource(src.URL, src.Ref)
			if err != nil {
				ui.Bullet("warn", fmt.Sprintf("Failed to fetch %s — skipping", src.URL))
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			resolved = append(resolved, cacheDir)
			ui.Bullet("ok", "Git source: "+src.URL)

		default:
			ui.Bullet("warn", fmt.Sprintf("Unknown source type '%s' — skipping", src.Type))
		}
	}
	return resolved, firstErr
}

// fetchGitSource clones or updates a git repository to the cache.
func fetchGitSource(url, ref string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git not installed")
	}

	cacheDir := config.GitSourceCacheDir(url)

	if _, err := os.Stat(filepath.Join(cacheDir, ".git")); err == nil {
		// Already cloned — check whether to update
		doUpdate := Flags.Force
		if !doUpdate {
			lastFetch := filepath.Join(cacheDir, ".last-fetch")
			if info, err := os.Stat(lastFetch); err == nil {
				if time.Since(info.ModTime()) < time.Hour {
					if Flags.Verbose {
						ui.Info("Using cached source (< 1h old): " + url)
					}
					return cacheDir, nil
				}
			}
			doUpdate = true
		}
		if doUpdate {
			if Flags.DryRun {
				ui.DryRun("git -C " + cacheDir + " pull")
				return cacheDir, nil
			}
			if Flags.Verbose {
				ui.Info("Updating cached source: " + url)
			}
			cmd := exec.Command("git", "-C", cacheDir, "pull", "-q")
			if err := cmd.Run(); err != nil {
				ui.Bullet("warn", "Could not update cached source — using existing copy")
			} else {
				touchLastFetch(cacheDir)
			}
		}
		return cacheDir, nil
	}

	// First clone
	if Flags.DryRun {
		args := "git clone --depth 1"
		if ref != "" {
			args += " --branch " + ref
		}
		ui.DryRun(args + " " + url + " " + cacheDir)
		return cacheDir, nil
	}

	if Flags.Verbose {
		ui.Info("Cloning source: " + url)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, cacheDir)
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(cacheDir)
		return "", fmt.Errorf("git clone failed: %s", string(out))
	}
	touchLastFetch(cacheDir)
	return cacheDir, nil
}

func touchLastFetch(cacheDir string) {
	f := filepath.Join(cacheDir, ".last-fetch")
	_ = os.WriteFile(f, []byte(time.Now().Format(time.RFC3339)), 0644)
}

// linkResourceFromSources symlinks a resource from the first matching source
// into ~/.agents/{resourceType}/{project}/{name}/.
func linkResourceFromSources(resourceType, name, project string, sources []string) error {
	var markerFile string
	switch resourceType {
	case "skills":
		markerFile = "SKILL.md"
	case "agents":
		markerFile = "AGENT.md"
	}

	destDir := filepath.Join(config.AgentsHome(), resourceType, project, name)

	for _, srcRoot := range sources {
		candidate := filepath.Join(srcRoot, resourceType, "global", name)
		if info, err := os.Stat(candidate); err != nil || !info.IsDir() {
			continue
		}
		if markerFile != "" {
			if _, err := os.Stat(filepath.Join(candidate, markerFile)); err != nil {
				continue
			}
		}

		if Flags.DryRun {
			ui.DryRun(fmt.Sprintf("link %s/%s → %s", resourceType, name, config.DisplayPath(candidate)))
			return nil
		}

		// Remove stale entry if --force
		if _, err := os.Lstat(destDir); err == nil {
			if !Flags.Force {
				return nil // already present
			}
			os.RemoveAll(destDir)
		}

		if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
			return err
		}
		if err := os.Symlink(candidate, destDir); err != nil {
			return fmt.Errorf("symlinking %s: %w", name, err)
		}
		if Flags.Verbose {
			ui.Bullet("ok", fmt.Sprintf("Linked %s/%s from %s", resourceType, name, config.DisplayPath(srcRoot)))
		}
		return nil
	}
	return fmt.Errorf("not found in any source")
}

// findProjectByPath looks up the registered project name for a given path.
func findProjectByPath(projectPath string) string {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	for _, name := range cfg.ListProjects() {
		if cfg.GetProjectPath(name) == projectPath {
			return name
		}
	}
	return ""
}
