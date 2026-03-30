package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// aiScanPatterns lists file/dir names to look for when scanning for AI configs.
var aiScanPatterns = []string{
	// Cursor
	".cursorrules",
	".cursor/settings.json",
	".cursor/mcp.json",
	".cursorignore",
	// Claude Code
	"CLAUDE.md",
	".claude/settings.json",
	".claude/settings.local.json",
	".claude.json",
	".mcp.json",
	// Codex
	"AGENTS.md",
	".codex/instructions.md",
	".codex/config.json",
	"codex.md",
	// OpenCode
	".opencode/instructions.md",
	".opencode/config.json",
	"OPENCODE.md",
	// GitHub Copilot
	".github/copilot-instructions.md",
	".vscode/mcp.json",
	"copilot-instructions.md",
	// Windsurf / other
	".windsurfrules",
	".ai-rules",
	".ai-instructions",
}

// aiScanDirPatterns lists directories whose children are AI config files.
var aiScanDirPatterns = []string{
	".cursor/rules",
	".cursor/agents",
	".claude/agents",
	".claude/skills",
	".claude/rules",
	".codex/agents",
	".continue",
	".github/agents",
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "__pycache__": true,
	".venv": true, "venv": true,
}

// isBackupArtifact reports whether a filename is a dot-agents backup artifact.
func isBackupArtifact(name string) bool {
	return strings.Contains(name, ".dot-agents-backup")
}

// scanExistingAIConfigs walks projectPath and returns all AI config files found,
// excluding *.dot-agents-backup artifacts.
func scanExistingAIConfigs(projectPath string) []string {
	var results []string
	seen := map[string]bool{}

	add := func(p string) {
		if isBackupArtifact(filepath.Base(p)) {
			return
		}
		if !seen[p] {
			seen[p] = true
			results = append(results, p)
		}
	}

	for _, pattern := range aiScanPatterns {
		candidate := filepath.Join(projectPath, pattern)
		if info, err := os.Lstat(candidate); err == nil && !info.IsDir() {
			add(candidate)
		}
	}
	for _, dir := range aiScanDirPatterns {
		d := filepath.Join(projectPath, dir)
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				add(filepath.Join(d, e.Name()))
			}
		}
	}

	// Walk for .aider* and aider.conf* anywhere in the tree
	_ = filepath.WalkDir(projectPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		name := d.Name()
		if strings.HasPrefix(name, ".aider") || strings.HasPrefix(name, "aider.conf") {
			add(path)
		}
		return nil
	})

	return results
}

// checkExistingConfigFiles returns root-level AI config files/entries that dot-agents would replace.
// Excludes files already managed by dot-agents and backup artifacts.
func checkExistingConfigFiles(projectPath, agentsHome string) []string {
	candidates := []string{
		filepath.Join(projectPath, ".mcp.json"),
		filepath.Join(projectPath, "AGENTS.md"),
		filepath.Join(projectPath, "opencode.json"),
		filepath.Join(projectPath, ".github", "copilot-instructions.md"),
	}
	var found []string
	for _, f := range candidates {
		// Never consider backup artifacts as live configs
		if isBackupArtifact(filepath.Base(f)) {
			continue
		}
		info, err := os.Lstat(f)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(f)
			if strings.HasPrefix(dest, agentsHome) {
				continue // already managed
			}
		}
		found = append(found, f)
	}
	return found
}

func NewAddCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a project to dot-agents management",
		Long: `Registers a project with dot-agents and sets up configuration links.
Existing config files are backed up before being replaced.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(args[0], name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Override project name (default: directory name)")
	return cmd
}

func runAdd(pathArg, nameArg string) error {
	// Resolve path
	projectPath := config.ExpandPath(pathArg)
	if _, err := os.Stat(projectPath); err != nil {
		return fmt.Errorf("directory not found: %s", projectPath)
	}

	// Derive name
	projectName := nameArg
	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}

	// Validate name
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !validName.MatchString(projectName) {
		return fmt.Errorf("invalid project name: %s (use --name for alphanumeric/hyphens/underscores)", projectName)
	}

	agentsHome := config.AgentsHome()
	displayPath := config.DisplayPath(projectPath)
	displayAgentsHome := config.DisplayPath(agentsHome)

	ui.Header("dot-agents add")
	fmt.Fprintf(os.Stdout, "Adding project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path: %s\n", ui.DimText(displayPath))

	// Step 1: Scan
	ui.Step("Scanning project...")

	if _, err := os.Stat(filepath.Join(projectPath, ".git")); err == nil {
		ui.Bullet("ok", "Valid git repository")
	} else {
		ui.Bullet("none", "Not a git repository (optional)")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if existing := cfg.GetProjectPath(projectName); existing != "" {
		if !Flags.Force {
			ui.Bullet("warn", "Already registered at: "+existing)
			fmt.Fprintln(os.Stdout, "\n  Use --force to update, or --name to use a different name")
			return fmt.Errorf("project '%s' already registered", projectName)
		}
		ui.Bullet("warn", "Will update existing registration (--force)")
	} else {
		ui.Bullet("ok", "Not yet registered")
	}

	// Check deprecated formats
	hasDeprecated := false
	for _, p := range platform.All() {
		if p.HasDeprecatedFormat(projectPath) {
			ui.Bullet("warn", fmt.Sprintf("Found deprecated %s config", p.DisplayName()))
			hasDeprecated = true
		}
	}

	// Step 2: Preview (platform-aware)
	ui.Step("The following will be created:")

	ui.PreviewSection(displayAgentsHome+"/",
		"rules/"+projectName+"/              (project rules)",
		"settings/"+projectName+"/           (project settings)",
		"  └── claude-code.json            (hooks, permissions)",
		"mcp/"+projectName+"/                (project MCP configs)",
		"skills/"+projectName+"/             (project skills)",
		"agents/"+projectName+"/             (project subagents)",
	)

	// Per-platform link preview
	type platformPreview struct {
		name     string
		id       string
		items    []string
		linkNote string
	}
	platformPreviews := []platformPreview{
		{
			name:     "Cursor",
			id:       "cursor",
			linkNote: "hard links",
			items: []string{
				".cursor/rules/global--*.mdc",
				".cursor/rules/" + projectName + "--*.mdc",
				".cursor/settings.json",
				".cursor/mcp.json",
				".cursorignore",
			},
		},
		{
			name:     "Claude Code",
			id:       "claude",
			linkNote: "symlinks",
			items: []string{
				".claude/rules/" + projectName + "--*.md",
				".claude/agents/*.md",
				".claude/skills/*/",
				".claude/settings.local.json",
				".mcp.json",
			},
		},
		{
			name:     "Codex",
			id:       "codex",
			linkNote: "symlinks",
			items:    []string{"AGENTS.md", ".agents/skills/*/"},
		},
		{
			name:     "OpenCode",
			id:       "opencode",
			linkNote: "symlinks",
			items:    []string{"opencode.json", ".opencode/agent/"},
		},
		{
			name:     "GitHub Copilot",
			id:       "copilot",
			linkNote: "symlinks",
			items: []string{
				".github/copilot-instructions.md",
				".github/agents/*.agent.md",
				".vscode/mcp.json",
			},
		},
	}

	fmt.Fprintf(os.Stdout, "\n  %s%s/%s\n", ui.Bold, displayPath, ui.Reset)
	for _, pp := range platformPreviews {
		installed := false
		for _, p := range platform.All() {
			if p.ID() == pp.id && p.IsInstalled() {
				installed = true
				break
			}
		}
		if installed {
			fmt.Fprintf(os.Stdout, "    %s%s%s %s(%s)%s\n", ui.Cyan, pp.name, ui.Reset, ui.Dim, pp.linkNote, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "    %s%s %s(not installed — skipped)%s\n", ui.Dim, pp.name, ui.Dim, ui.Reset)
			continue
		}
		for _, item := range pp.items {
			fmt.Fprintf(os.Stdout, "      %s%s%s\n", ui.Dim, item, ui.Reset)
		}
	}

	ui.InfoBox("About Link Types",
		"Cursor uses HARD LINKS (required by IDE).",
		"Other agents use symlinks for flexibility.",
	)

	// Identify files that will be replaced
	existingFiles := checkExistingConfigFiles(projectPath, agentsHome)

	// Show files that will be replaced
	if len(existingFiles) > 0 {
		ui.Section("Files to Replace")
		fmt.Fprintf(os.Stdout, "  %sThese root-level files will be backed up and replaced with links:%s\n", ui.Yellow, ui.Reset)
		for _, f := range existingFiles {
			rel := strings.TrimPrefix(f, projectPath+"/")
			fileType := "file"
			if info, err := os.Lstat(f); err == nil && info.Mode()&os.ModeSymlink != 0 {
				fileType = "symlink"
			}
			fmt.Fprintf(os.Stdout, "  %s!%s %s %s(%s)%s\n", ui.Yellow, ui.Reset, rel, ui.Dim, fileType, ui.Reset)
		}
		fmt.Fprintf(os.Stdout, "\n  %sBackups stored in ~/.agents/resources/%s/backups/<timestamp>/%s\n", ui.Dim, projectName, ui.Reset)
	}

	// Scan for other AI configs in the repo (informational)
	allAIConfigs := scanExistingAIConfigs(projectPath)
	var discoveredElsewhere []string
	existingSet := map[string]bool{}
	for _, f := range existingFiles {
		existingSet[f] = true
	}
	for _, f := range allAIConfigs {
		if !existingSet[f] {
			discoveredElsewhere = append(discoveredElsewhere, f)
		}
	}
	if len(discoveredElsewhere) > 0 {
		ui.Section("Other AI Configs Discovered")
		fmt.Fprintf(os.Stdout, "  %sFound AI agent configs elsewhere in the repo (not replaced):%s\n", ui.Cyan, ui.Reset)
		shown := 0
		for _, f := range discoveredElsewhere {
			if shown >= 10 {
				break
			}
			rel := strings.TrimPrefix(f, projectPath+"/")
			kind := "file"
			if info, err := os.Lstat(f); err == nil {
				switch {
				case info.Mode()&os.ModeSymlink != 0:
					kind = "symlink"
				case info.IsDir():
					kind = "dir"
				}
			}
			fmt.Fprintf(os.Stdout, "  %s○%s %s %s(%s)%s\n", ui.Dim, ui.Reset, rel, ui.Dim, kind, ui.Reset)
			shown++
		}
		if len(discoveredElsewhere) > 10 {
			fmt.Fprintf(os.Stdout, "  %s... and %d more%s\n", ui.Dim, len(discoveredElsewhere)-10, ui.Reset)
		}
		fmt.Fprintf(os.Stdout, "\n  %sConsider migrating these to ~/.agents/ for centralized management.%s\n", ui.Dim, ui.Reset)
	}

	if Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}

	confirmMsg := "Proceed?"
	if len(existingFiles) > 0 {
		confirmMsg = fmt.Sprintf("Proceed? (%d file(s) will be backed up and replaced)", len(existingFiles))
	}
	if !Flags.Yes {
		if !ui.Confirm(confirmMsg, false) {
			ui.Info("Add cancelled.")
			return nil
		}
	}

	// Step 3: Backup existing configs
	if len(existingFiles) > 0 {
		ui.Step("Backing up existing configs...")
		timestamp := time.Now().Format("20060102-150405")
		backed := backupExistingConfigsList(existingFiles, projectPath, agentsHome, projectName, timestamp)
		ui.Bullet("ok", fmt.Sprintf("Backed up %d existing file(s)", backed))
		ui.Bullet("ok", fmt.Sprintf("Stored backups in ~/.agents/resources/%s/backups/%s/", projectName, timestamp))
	}

	// Step 4: Create project dirs
	ui.Step("Creating project structure...")
	if err := createProjectDirs(projectName); err != nil {
		return err
	}
	ui.Bullet("ok", "Created ~/.agents/ directories")

	// Restore from active resources
	restored := restoreFromResourcesCounted(projectName, projectPath)
	if restored > 0 {
		ui.Bullet("ok", fmt.Sprintf("Restored %d item(s) from ~/.agents/resources/%s/", restored, projectName))
	}

	// Step 5: Create links
	ui.Step("Creating links...")
	config.SetWindowsMirrorContext(projectPath)

	for _, p := range platform.All() {
		if !p.IsInstalled() {
			continue
		}
		if err := p.CreateLinks(projectName, projectPath); err != nil {
			ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
		} else {
			ui.Bullet("ok", p.DisplayName()+" links created")
		}
	}

	// Add .agents-refresh to .gitignore
	ensureGitignoreEntry(projectPath, ".agents-refresh")

	// Step 6: Register
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Registered in config.json")

	nextSteps := []string{
		"Add project rules: edit ~/.agents/rules/" + projectName + "/rules.md",
		"Check applied configs: dot-agents status --audit",
		"Make it git-portable: dot-agents install --generate",
	}
	if hasDeprecated {
		nextSteps = append(nextSteps, "Migrate deprecated formats: dot-agents migrate detect")
	}
	ui.SuccessBox(fmt.Sprintf("Project '%s' added successfully!", projectName), nextSteps...)
	return nil
}

func createProjectDirs(project string) error {
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

// backupExistingConfigsList backs up the given files into ~/.agents/resources/<project>/...
// and removes the originals from the project tree. No *.dot-agents-backup files are left
// in the project. Returns count of files processed.
func backupExistingConfigsList(files []string, projectPath, agentsHome, project, timestamp string) int {
	count := 0
	for _, f := range files {
		// Safety: never back up backup artifacts
		if isBackupArtifact(filepath.Base(f)) {
			continue
		}
		info, err := os.Lstat(f)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Unmanaged symlinks: remove without backup (no content to preserve)
			os.Remove(f)
			count++
			continue
		}
		// Regular file: copy into resources, then delete from project
		mirrorBackup(project, projectPath, f, timestamp)
		if err := os.Remove(f); err != nil {
			continue
		}
		count++
	}
	return count
}

// restoreFromResourcesCounted restores files from ~/.agents/resources/<project>/ and returns the count.
func restoreFromResourcesCounted(project, projectPath string) int {
	agentsHome := config.AgentsHome()
	resourcesDir := filepath.Join(agentsHome, "resources", project)
	if _, err := os.Stat(resourcesDir); err != nil {
		return 0
	}
	count := 0
	_ = filepath.WalkDir(resourcesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		relPath := strings.TrimPrefix(path, resourcesDir+"/")
		if strings.HasPrefix(relPath, "backups/") {
			return nil
		}
		destRel := mapResourceRelToDest(project, relPath)
		if destRel == "" {
			return nil
		}
		destPath := filepath.Join(agentsHome, destRel)
		os.MkdirAll(filepath.Dir(destPath), 0755)
		if err := copyFile(path, destPath); err == nil {
			count++
		}
		return nil
	})
	return count
}

// mirrorBackup copies srcFile (original path, before deletion) into the
// ~/.agents/resources/<project>/ tree using the file's original relative path.
// No *.dot-agents-backup suffix is added anywhere.
func mirrorBackup(project, projectPath, srcFile, timestamp string) {
	agentsHome := config.AgentsHome()
	relPath := strings.TrimPrefix(srcFile, projectPath+"/")
	if relPath == srcFile {
		relPath = filepath.Base(srcFile)
	}

	// Active (latest) copy — overwritten on each backup run
	activeTarget := filepath.Join(agentsHome, "resources", project, relPath)
	os.MkdirAll(filepath.Dir(activeTarget), 0755)
	copyFile(srcFile, activeTarget)

	// Timestamped immutable copy
	if timestamp != "" {
		tsTarget := filepath.Join(agentsHome, "resources", project, "backups", timestamp, relPath)
		os.MkdirAll(filepath.Dir(tsTarget), 0755)
		copyFile(srcFile, tsTarget)
	}
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func ensureGitignoreEntry(repoPath, entry string) {
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
