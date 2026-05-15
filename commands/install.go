package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

var gitCommitRefPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
var errInstallResourceNotFound = errors.New("resource not found in any source")

type gitSourceRefKind int

const (
	gitSourceRefBranch gitSourceRefKind = iota
	gitSourceRefTag
	gitSourceRefCommit
)

func NewInstallCmd() *cobra.Command {
	var generate bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up project from .agentsrc.json manifest",
		Long: `Reads .agentsrc.json in the current directory, materializes declared skills and
agents into ~/.agents/ from configured sources, then applies the manifest to each
installed platform (rules, hooks, MCP configs, settings) with the same link pass
as da refresh.

Commit .agentsrc.json to git so any contributor can run 'da install'
after cloning — no manual init or sync required.

Use --generate to create or refresh .agentsrc.json from the current ~/.agents/ state.
If a manifest already exists, generated skill and platform lists replace stale values,
but existing source entries (for example git remotes), a non-empty project name, and
unknown JSON keys are preserved.`,
		Example: ExampleBlock(
			"  da install",
			"  da install --strict",
			"  da install --generate",
			"  da install --generate --force",
		),
		Args: NoArgsWithHints("Run install from the target repository directory instead of passing a path."),
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

	ui.Header("da install")

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
	finalizeInstall(projectName, projectPath)

	ui.SuccessBox(
		fmt.Sprintf("Project '%s' installed successfully!", projectName),
		"Check links: da status --audit",
		"Update manifest: da install --generate",
	)
	return nil
}

func loadInstallManifest(projectPath string) (*config.AgentsRC, error) {
	rc, err := config.LoadAgentsRC(projectPath)
	if err == nil {
		return rc, nil
	}
	if os.IsNotExist(err) {
		return nil, ErrorWithHints(
			config.AgentsRCFile+" not found in current directory",
			"Run `da install --generate` to create one from the current shared state.",
			"If this project is not managed yet, run `da add .` first.",
		)
	}
	return nil, fmt.Errorf("reading %s: %w", config.AgentsRCFile, err)
}

func ensureAgentsHomeInitialized() error {
	if _, err := os.Stat(filepath.Join(config.AgentsHome(), "config.json")); err != nil {
		return ErrorWithHints(
			"~/.agents/ not initialized",
			"Run `da init` once on this machine before using install.",
		)
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
	sources := resolvedSources
	if len(sources) == 0 {
		// Manifest may omit explicit sources while listing skills/agents that already exist
		// under ~/.agents/<bucket>/<project>/ (e.g. after promote). Resolve from canonical home.
		sources = []string{config.AgentsHome()}
	}
	if err := linkInstallResourceList("skills", "skill", rc.Skills, projectName, sources, strict); err != nil {
		return err
	}
	return linkInstallResourceList("agents", "agent", rc.Agents, projectName, sources, strict)
}

func linkInstallResourceList(resourceType, label string, names []string, projectName string, sources []string, strict bool) error {
	for _, name := range names {
		if err := linkResourceFromSources(resourceType, name, projectName, sources); err != nil {
			if strict {
				if errors.Is(err, errInstallResourceNotFound) {
					return fmt.Errorf("%s '%s' not found in any source (--strict mode)", label, name)
				}
				return fmt.Errorf("%s '%s' install failed (--strict mode): %w", label, name, err)
			}
			if errors.Is(err, errInstallResourceNotFound) {
				ui.Bullet("warn", fmt.Sprintf("%s '%s' not found in any source — skipping", label, name))
				continue
			}
			ui.Bullet("warn", fmt.Sprintf("%s '%s' install failed: %v — skipping", label, name, err))
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

	runInstallSharedTargets(projectName, projectPath)

	for _, p := range platform.All() {
		createInstallPlatformLink(p, projectName, projectPath)
	}
}

// runInstallSharedTargets runs the shared-target projection across all
// installed platforms and surfaces the resulting plan or warning lines.
func runInstallSharedTargets(projectName, projectPath string) {
	var installed []platform.Platform
	for _, p := range platform.All() {
		if p.IsInstalled() {
			installed = append(installed, p)
		}
	}
	lines, err := platform.RunSharedTargetProjection(projectName, projectPath, installed, Flags.DryRun)
	if err != nil {
		if Flags.DryRun {
			ui.Bullet("warn", fmt.Sprintf("shared targets plan: %v", err))
		} else {
			ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		}
		return
	}
	for _, line := range lines {
		ui.DryRun(line)
	}
}

// createInstallPlatformLink refreshes (or skips) the link bundle for a
// single platform during install, honoring verbose / dry-run flags.
func createInstallPlatformLink(p platform.Platform, projectName, projectPath string) {
	if !p.IsInstalled() {
		if Flags.Verbose {
			ui.Skip(p.DisplayName() + " (not installed)")
		}
		return
	}
	if Flags.DryRun {
		ui.DryRun("refresh " + p.DisplayName() + " links")
		return
	}
	if err := p.CreateLinks(projectName, projectPath); err != nil {
		ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
		return
	}
	ui.Bullet("ok", p.DisplayName()+" links created")
}

func finalizeInstall(projectName, projectPath string) {
	if Flags.DryRun {
		return
	}
	if err := projectsync.WriteRefreshToAgentsRC(projectName, projectPath, Version, Commit, Describe); err != nil {
		ui.Bullet("warn", fmt.Sprintf("manifest refresh metadata: %v", err))
		return
	}
	ui.Bullet("ok", "Updated .agentsrc.json refresh details")
}

// ─── runInstallGenerate ──────────────────────────────────────────────────────

func runInstallGenerate() error {
	projectPath, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	ui.Header("da install --generate")

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

	manifestPath := filepath.Join(projectPath, config.AgentsRCFile)
	if _, statErr := os.Stat(manifestPath); statErr == nil {
		existing, loadErr := config.LoadAgentsRC(projectPath)
		if loadErr != nil {
			return fmt.Errorf("loading existing %s: %w", config.AgentsRCFile, loadErr)
		}
		rc = config.MergeGenerateAgentsRC(existing, rc)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("accessing %s: %w", config.AgentsRCFile, statErr)
	}

	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Would write %s with:", config.AgentsRCFile))
		ui.DryRun(fmt.Sprintf("  project:  %s", rc.Project))
		ui.DryRun(fmt.Sprintf("  sources:  %d entries", len(rc.Sources)))
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
	fmt.Fprintf(os.Stdout, "  2. Commit:  git add %s && git commit -m 'Add da manifest'\n", config.AgentsRCFile)
	fmt.Fprintln(os.Stdout, "  3. Others:  da install   (after cloning)")
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

	cacheDir := config.GitSourceCacheDir(url, ref)
	if hasCachedGitSource(cacheDir) {
		if Flags.Force {
			if Flags.DryRun {
				ui.DryRun("refresh git source " + gitSourceLabel(url, ref))
				return cacheDir, nil
			}
			return refreshCachedGitSource(gitBin, url, ref, cacheDir)
		}

		refKind, branchRef, err := resolveGitSourceRefKind(gitBin, url, ref)
		if err != nil {
			ui.Bullet("warn", "Could not verify cached source against remote — using existing copy")
			return cacheDir, nil
		}
		if refKind != gitSourceRefBranch {
			if Flags.Verbose {
				ui.Info("Using pinned cached source: " + gitSourceLabel(url, ref))
			}
			return cacheDir, nil
		}

		upToDate, err := cachedGitSourceMatchesRemoteTip(gitBin, cacheDir, url, branchRef)
		if err != nil {
			ui.Bullet("warn", "Could not compare cached source to remote branch tip — using existing copy")
			return cacheDir, nil
		}
		if upToDate {
			if Flags.Verbose {
				ui.Info("Using cached source at branch tip: " + gitSourceLabel(url, branchRef))
			}
			return cacheDir, nil
		}
		if Flags.DryRun {
			ui.DryRun("refresh git source " + gitSourceLabel(url, branchRef))
			return cacheDir, nil
		}
		return refreshCachedGitSource(gitBin, url, ref, cacheDir)
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

func refreshCachedGitSource(gitBin, url, ref, cacheDir string) (string, error) {
	if Flags.Verbose {
		ui.Info("Refreshing cached source: " + gitSourceLabel(url, ref))
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return "", fmt.Errorf("clearing cached source for %s: %w", gitSourceLabel(url, ref), err)
	}
	return cloneGitSource(gitBin, url, ref, cacheDir)
}

func resolveGitSourceRefKind(gitBin, url, ref string) (gitSourceRefKind, string, error) {
	if ref == "" {
		branch, err := resolveGitDefaultBranch(gitBin, url)
		if err != nil {
			return gitSourceRefBranch, "", err
		}
		return gitSourceRefBranch, branch, nil
	}
	if gitCommitRefPattern.MatchString(ref) {
		return gitSourceRefCommit, ref, nil
	}
	branchExists, err := gitRemoteRefExists(gitBin, url, "heads", ref)
	if err != nil {
		return gitSourceRefBranch, "", err
	}
	if branchExists {
		return gitSourceRefBranch, ref, nil
	}
	tagExists, err := gitRemoteRefExists(gitBin, url, "tags", ref)
	if err != nil {
		return gitSourceRefBranch, "", err
	}
	if tagExists {
		return gitSourceRefTag, ref, nil
	}
	return gitSourceRefBranch, "", fmt.Errorf("git ref %q not found in %s", ref, url)
}

func gitRemoteRefExists(gitBin, url, scope, ref string) (bool, error) {
	args := []string{"ls-remote", "--quiet"}
	switch scope {
	case "heads":
		args = append(args, "--heads")
	case "tags":
		args = append(args, "--tags", "--refs")
	default:
		return false, fmt.Errorf("unknown git ref scope %q", scope)
	}
	args = append(args, url, ref)
	cmd := exec.Command(gitBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git ls-remote failed: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func resolveGitDefaultBranch(gitBin, url string) (string, error) {
	cmd := exec.Command(gitBin, "ls-remote", "--symref", url, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-remote --symref failed: %s", strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(line, "ref: refs/heads/")
		branch = strings.TrimSuffix(branch, "\tHEAD")
		branch = strings.TrimSpace(branch)
		if branch != "" {
			return branch, nil
		}
	}
	return "", fmt.Errorf("could not resolve default branch for %s", url)
}

func cachedGitSourceMatchesRemoteTip(gitBin, cacheDir, url, branch string) (bool, error) {
	localSHA, err := gitHEADSHA(gitBin, cacheDir)
	if err != nil {
		return false, err
	}
	remoteSHA, err := gitRemoteBranchSHA(gitBin, url, branch)
	if err != nil {
		return false, err
	}
	return localSHA == remoteSHA, nil
}

func gitHEADSHA(gitBin, repo string) (string, error) {
	cmd := exec.Command(gitBin, "-C", repo, "rev-parse", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRemoteBranchSHA(gitBin, url, branch string) (string, error) {
	cmd := exec.Command(gitBin, "ls-remote", "--heads", url, branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git ls-remote branch failed: %s", strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("remote branch %q not found in %s", branch, url)
	}
	return fields[0], nil
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
	candidate, srcRoot, found := firstResourceCandidate(resourceType, name, markerFile, project, sources)
	if !found {
		return errInstallResourceNotFound
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
	if err := links.Symlink(candidate, destDir); err != nil {
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

func firstResourceCandidate(resourceType, name, markerFile, project string, sources []string) (string, string, bool) {
	for _, srcRoot := range sources {
		// Prefer project-scoped canonical dirs (~/.agents/skills/<project>/…), then global/.
		candidates := []string{
			filepath.Join(srcRoot, resourceType, project, name),
			filepath.Join(srcRoot, resourceType, "global", name),
		}
		for _, candidate := range candidates {
			if resourceCandidateIsValid(candidate, markerFile) {
				return candidate, srcRoot, true
			}
		}
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
