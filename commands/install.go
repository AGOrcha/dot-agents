package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
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

	rc, err := loadInstallManifest(projectPath)
	if err != nil {
		return err
	}
	if err := ensureAgentsHomeInitialized(); err != nil {
		return err
	}

	projectName := installProjectName(rc.Project, projectPath)
	fmt.Fprintf(os.Stdout, "Project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path:    %s\n", ui.DimText(config.DisplayPath(projectPath)))

	resolvedSources, err := resolveInstallSources(rc.Sources, strict)
	if err != nil {
		return err
	}
	if err := linkInstallResources(projectName, rc, resolvedSources, strict); err != nil {
		return err
	}
	if err := ensureInstallProjectDirs(projectName); err != nil {
		return err
	}
	if err := registerInstallProject(projectName, projectPath); err != nil {
		return err
	}

	createInstallPlatformLinks(projectName, projectPath)
	finalizeInstall(projectPath)

	ui.SuccessBox(
		fmt.Sprintf("Project '%s' installed successfully!", projectName),
		"Check links: dot-agents status --audit",
		"Update manifest: dot-agents install --generate",
	)
	return nil
}

func loadInstallManifest(projectPath string) (*config.AgentsRC, error) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err == nil {
		return rc, nil
	}
	if os.IsNotExist(err) {
		ui.Error(config.AgentsRCFile + " not found in current directory")
		fmt.Fprintln(os.Stdout, "  Run 'dot-agents install --generate' to create one, or")
		fmt.Fprintln(os.Stdout, "  run 'dot-agents add .' to register this project first.")
		return nil, fmt.Errorf("manifest not found")
	}
	return nil, fmt.Errorf("reading %s: %w", config.AgentsRCFile, err)
}

func ensureAgentsHomeInitialized() error {
	if _, err := os.Stat(filepath.Join(config.AgentsHome(), "config.json")); err != nil {
		return fmt.Errorf("~/.agents/ not initialized — run 'dot-agents init' first")
	}
	return nil
}

func installProjectName(manifestProject, projectPath string) string {
	if manifestProject != "" {
		return manifestProject
	}
	return filepath.Base(projectPath)
}

func resolveInstallSources(sources []config.Source, strict bool) ([]string, error) {
	ui.Section("Resolving sources")
	resolvedSources, err := resolveSources(sources)
	if err != nil && strict {
		return nil, err
	}
	return resolvedSources, nil
}

func linkInstallResources(projectName string, rc *config.AgentsRC, resolvedSources []string, strict bool) error {
	if len(resolvedSources) == 0 {
		return nil
	}
	if err := linkInstallResourceList("skills", "skill", rc.Skills, projectName, resolvedSources, strict); err != nil {
		return err
	}
	return linkInstallResourceList("agents", "agent", rc.Agents, projectName, resolvedSources, strict)
}

func linkInstallResourceList(resourceType, label string, names []string, projectName string, sources []string, strict bool) error {
	for _, name := range names {
		if err := linkResourceFromSources(resourceType, name, projectName, sources); err != nil {
			msg := fmt.Sprintf("%s '%s' not found in any source", label, name)
			if strict {
				return fmt.Errorf("%s (--strict mode)", msg)
			}
			ui.Bullet("warn", msg+" — skipping")
		}
	}
	return nil
}

func ensureInstallProjectDirs(projectName string) error {
	if Flags.DryRun {
		ui.DryRun("create ~/.agents/ directories for '" + projectName + "'")
		return nil
	}
	if err := projectsync.CreateProjectDirs(projectName); err != nil {
		return err
	}
	ui.Bullet("ok", "Ensured ~/.agents/ project directories")
	return nil
}

func registerInstallProject(projectName, projectPath string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.GetProjectPath(projectName) != "" {
		ui.Bullet("skip", "Already registered in config.json")
		return nil
	}
	if Flags.DryRun {
		ui.DryRun("register '" + projectName + "' in config.json")
		return nil
	}
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Registered '"+projectName+"' in config.json")
	return nil
}

func createInstallPlatformLinks(projectName, projectPath string) {
	ui.Section("Creating platform links")
	config.SetWindowsMirrorContext(projectPath)

	if !Flags.DryRun {
		var installed []platform.Platform
		for _, p := range platform.All() {
			if p.IsInstalled() {
				installed = append(installed, p)
			}
		}
		if err := platform.CollectAndExecuteSharedTargetPlan(projectName, projectPath, installed); err != nil {
			ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		}
	}

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
}

func finalizeInstall(projectPath string) {
	if Flags.DryRun {
		return
	}
	projectsync.WriteRefreshMarker(projectPath, Version, Commit, Describe)
	ui.Bullet("ok", "Wrote .agents-refresh marker")
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
		root, err := resolveSourceRoot(src)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if root == "" {
			continue
		}
		resolved = append(resolved, root)
	}
	return resolved, firstErr
}

func resolveSourceRoot(src config.Source) (string, error) {
	switch src.Type {
	case "local":
		root := config.AgentsHome()
		if src.Path != "" {
			root = config.ExpandPath(src.Path)
		}
		ui.Bullet("ok", "Local source: "+config.DisplayPath(root))
		return root, nil
	case "git":
		if src.URL == "" {
			ui.Bullet("warn", "Git source missing 'url' — skipping")
			return "", nil
		}
		cacheDir, err := fetchGitSource(src.URL, src.Ref)
		if err != nil {
			ui.Bullet("warn", fmt.Sprintf("Failed to fetch %s — skipping", src.URL))
			return "", err
		}
		ui.Bullet("ok", "Git source: "+src.URL)
		return cacheDir, nil
	default:
		ui.Bullet("warn", fmt.Sprintf("Unknown source type '%s' — skipping", src.Type))
		return "", nil
	}
}

// fetchGitSource clones or updates a git repository to the cache.
func fetchGitSource(url, ref string) (string, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git not installed")
	}

	cacheDir := config.GitSourceCacheDir(url)
	if hasCachedGitSource(cacheDir) {
		if shouldUseCachedGitSource(cacheDir, url) {
			return cacheDir, nil
		}
		if Flags.DryRun {
			ui.DryRun("git -C " + cacheDir + " pull")
			return cacheDir, nil
		}
		updateCachedGitSource(gitBin, cacheDir, url)
		return cacheDir, nil
	}

	if Flags.DryRun {
		ui.DryRun(gitCloneDryRunCommand(url, ref, cacheDir))
		return cacheDir, nil
	}
	return cloneGitSource(gitBin, url, ref, cacheDir)
}

func hasCachedGitSource(cacheDir string) bool {
	_, err := os.Stat(filepath.Join(cacheDir, ".git"))
	return err == nil
}

func shouldUseCachedGitSource(cacheDir, url string) bool {
	if Flags.Force {
		return false
	}
	lastFetch := filepath.Join(cacheDir, ".last-fetch")
	info, err := os.Stat(lastFetch)
	if err != nil || time.Since(info.ModTime()) >= time.Hour {
		return false
	}
	if Flags.Verbose {
		ui.Info("Using cached source (< 1h old): " + url)
	}
	return true
}

func updateCachedGitSource(gitBin, cacheDir, url string) {
	if Flags.Verbose {
		ui.Info("Updating cached source: " + url)
	}
	cmd := exec.Command(gitBin, "-C", cacheDir, "pull", "-q")
	if err := cmd.Run(); err != nil {
		ui.Bullet("warn", "Could not update cached source — using existing copy")
		return
	}
	touchLastFetch(cacheDir)
}

func gitCloneDryRunCommand(url, ref, cacheDir string) string {
	args := "git clone --depth 1"
	if ref != "" {
		args += " --branch " + ref
	}
	return args + " " + url + " " + cacheDir
}

func cloneGitSource(gitBin, url, ref, cacheDir string) (string, error) {
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
	cmd := exec.Command(gitBin, args...)
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
	destDir := filepath.Join(config.AgentsHome(), resourceType, project, name)
	markerFile := resourceMarkerFile(resourceType)
	candidate, srcRoot, found := firstResourceCandidate(resourceType, name, markerFile, sources)
	if !found {
		return fmt.Errorf("not found in any source")
	}

	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("link %s/%s → %s", resourceType, name, config.DisplayPath(candidate)))
		return nil
	}
	if shouldSkipLinkDestination(destDir) {
		return nil
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

func resourceMarkerFile(resourceType string) string {
	switch resourceType {
	case "skills":
		return "SKILL.md"
	case "agents":
		return "AGENT.md"
	default:
		return ""
	}
}

func firstResourceCandidate(resourceType, name, markerFile string, sources []string) (string, string, bool) {
	for _, srcRoot := range sources {
		candidate := filepath.Join(srcRoot, resourceType, "global", name)
		if !resourceCandidateIsValid(candidate, markerFile) {
			continue
		}
		return candidate, srcRoot, true
	}
	return "", "", false
}

func resourceCandidateIsValid(candidate, markerFile string) bool {
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return false
	}
	if markerFile == "" {
		return true
	}
	_, err = os.Stat(filepath.Join(candidate, markerFile))
	return err == nil
}

func shouldSkipLinkDestination(destDir string) bool {
	if _, err := os.Lstat(destDir); err != nil {
		return false
	}
	if !Flags.Force {
		return true
	}
	_ = os.RemoveAll(destDir)
	return false
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
