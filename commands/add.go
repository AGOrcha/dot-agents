package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// addDeps is the multi-method collaborator runAdd and its backup / restore /
// KG-MCP-config helpers need (interface-DI per docs/TEST_SEAMS.md). The
// canonical definition lives in commands/lifecycle as lifecycle.AddDeps
// after root-command-decomposition t02b lifted the shared helpers; the
// alias here keeps existing commands/* call sites unchanged.
type addDeps = lifecycle.AddDeps

// stdAddDeps is the production addDeps backed by direct os / projectsync /
// config calls. Re-aliased from lifecycle.StdAddDeps post-t02b so the
// production wiring (NewAddCmd, mirrorBackup, ensureGlobalKGMCPConfigs)
// and tests (stdAddDeps{} literals) keep compiling unchanged.
type stdAddDeps = lifecycle.StdAddDeps

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
	".codex/hooks.json",
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
	".opencode/agent",
	".continue",
	".github/agents",
	".github/hooks",
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

func isManagedCursorRuleRel(project, rel string) bool {
	if !strings.HasPrefix(rel, relCursorRulesDir) {
		return false
	}
	name := filepath.Base(rel)
	return strings.HasPrefix(name, "global--") || strings.HasPrefix(name, project+"--")
}

func isManagedProjectOutput(project, projectPath, filePath, agentsHome string) bool {
	if isManagedSymlink(filePath, agentsHome) {
		return true
	}

	rel, err := filepath.Rel(projectPath, filePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)

	// Managed Cursor rule names live in a reserved namespace and should never
	// be re-imported or backed up as user-authored files.
	if isManagedCursorRuleRel(project, rel) {
		return true
	}

	destRel := mapResourceRelToDest(project, rel)
	if destRel == "" {
		return false
	}
	linked, err := links.AreHardlinked(filePath, filepath.Join(agentsHome, destRel))
	return err == nil && linked
}

// isManagedHardlinkToCanonicalSource reports whether filePath is hard linked
// to the canonical source under agentsHome that this candidate's repo-relative
// path maps to for project. This is the target-identity proof
// backupExistingConfigsList needs before dropping a multi-link file without a
// mirror backup: a bare nlink>1 only means "shares an inode with something",
// not "managed by da".
func isManagedHardlinkToCanonicalSource(project, projectPath, filePath, agentsHome string) bool {
	rel, err := filepath.Rel(projectPath, filePath)
	if err != nil {
		return false
	}
	destRel := mapResourceRelToDest(project, filepath.ToSlash(rel))
	if destRel == "" {
		return false
	}
	canonical := filepath.Join(agentsHome, destRel)
	linked, err := links.AreHardlinked(filePath, canonical)
	return err == nil && linked
}

// checkExistingConfigFiles returns root-level AI config files/entries that dot-agents would replace.
// Excludes files already managed by dot-agents and backup artifacts.
func checkExistingConfigFiles(project, projectPath, agentsHome string) []string {
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
		if _, err := os.Lstat(f); err != nil {
			continue
		}
		if links.IsManagedLinkUnder(f, agentsHome) {
			continue // already managed (resolvable symlink/junction)
		}
		if isManagedProjectOutput(project, projectPath, f, agentsHome) {
			continue
		}
		found = append(found, f)
	}
	return found
}

func NewAddCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a project to da management",
		Long: `Registers a project with da and sets up configuration links.
Existing config files are backed up before being replaced.

Use this when a project should consume shared configuration from ~/.agents/
and stay refreshable by both human operators and AI agents.`,
		Example: ExampleBlock(
			"  da add .",
			"  da add ~/src/my-repo --name billing-api",
			"  da add . --dry-run",
		),
		Args: ExactArgsWithHints(1, "Pass a project directory such as `.` or `~/src/my-repo`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(args[0], name, stdAddDeps{})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Override project name (default: directory name)")
	return cmd
}

func runAdd(pathArg, nameArg string, deps addDeps) error {
	projectPath, projectName, err := resolveAddTarget(pathArg, nameArg)
	if err != nil {
		return err
	}
	agentsHome := config.AgentsHome()

	announceAddTarget(projectName, projectPath, agentsHome)

	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := checkAddNotAlreadyRegistered(cfg, projectName); err != nil {
		return err
	}

	hasDeprecated := reportDeprecatedFormats(projectPath)

	printAddPreview(projectName, projectPath, agentsHome)

	existingFiles := checkExistingConfigFiles(projectName, projectPath, agentsHome)
	reportAddExistingFiles(existingFiles, projectName, projectPath)
	reportDiscoveredAIConfigs(existingFiles, projectPath)

	if Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}

	if confirmAddProceed(existingFiles) {
		return nil
	}

	if err := backupAddExistingFiles(existingFiles, projectName, projectPath, agentsHome, deps); err != nil {
		return err
	}

	if err := scaffoldAddProjectDirs(projectName, projectPath, agentsHome, deps); err != nil {
		return err
	}

	if err := createAddLinks(projectName, projectPath); err != nil {
		return err
	}

	if err := registerAddedProject(cfg, projectName, projectPath); err != nil {
		return err
	}

	emitAddSuccessBox(projectName, projectPath, hasDeprecated)
	return nil
}

// resolveAddTarget validates pathArg, derives the project name (override or
// directory base), and validates that the name is a legal identifier. Returns
// (projectPath, projectName, err).
func resolveAddTarget(pathArg, nameArg string) (string, string, error) {
	projectPath := config.ExpandPath(pathArg)
	if _, err := os.Stat(projectPath); err != nil {
		return "", "", fmt.Errorf("directory not found: %s", projectPath)
	}
	projectName := nameArg
	if projectName == "" {
		projectName = filepath.Base(projectPath)
	}
	validName := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !validName.MatchString(projectName) {
		return "", "", fmt.Errorf("invalid project name: %s (use --name for alphanumeric/hyphens/underscores)", projectName)
	}
	return projectPath, projectName, nil
}

// announceAddTarget prints the header, the project/path lines, the optional
// manifest-found hint, and the "Scanning project..." git-repo bullet.
func announceAddTarget(projectName, projectPath, _ string) {
	ui.Header("da add")
	fmt.Fprintf(os.Stdout, "Adding project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path: %s\n", ui.DimText(config.DisplayPath(projectPath)))

	if _, err := config.LoadAgentsRC(projectPath); err == nil {
		ui.Info(".agentsrc.json found — you can also use 'da install' to apply the manifest directly")
	}

	ui.Step("Scanning project...")
	if _, err := os.Stat(filepath.Join(projectPath, ".git")); err == nil {
		ui.Bullet("ok", "Valid git repository")
	} else {
		ui.Bullet("none", "Not a git repository (optional)")
	}
}

// checkAddNotAlreadyRegistered enforces the "not already registered" guard.
// --force downgrades it to a warning. Returns a typed error when the project
// is registered and --force was NOT supplied.
func checkAddNotAlreadyRegistered(cfg *config.Config, projectName string) error {
	existing := cfg.GetProjectPath(projectName)
	if existing == "" {
		ui.Bullet("ok", "Not yet registered")
		return nil
	}
	if !Flags.Force {
		ui.Bullet("warn", "Already registered at: "+existing)
		fmt.Fprintln(os.Stdout, "\n  Use --force to update, or --name to use a different name")
		return fmt.Errorf("project '%s' already registered", projectName)
	}
	ui.Bullet("warn", "Will update existing registration (--force)")
	return nil
}

// reportDeprecatedFormats prints a warn bullet for each platform whose
// deprecated config format is detected in projectPath. Returns true when
// at least one was found (drives the SuccessBox migrate hint).
func reportDeprecatedFormats(projectPath string) bool {
	hasDeprecated := false
	for _, p := range platform.All() {
		if p.HasDeprecatedFormat(projectPath) {
			ui.Bullet("warn", fmt.Sprintf("Found deprecated %s config", p.DisplayName()))
			hasDeprecated = true
		}
	}
	return hasDeprecated
}

// addPlatformPreview captures the per-platform link-preview row.
type addPlatformPreview struct {
	name     string
	id       string
	items    []string
	linkNote string
}

// addPlatformPreviews returns the static preview table — order and contents
// must match the prior runAdd inline literal.
func addPlatformPreviews(projectName string) []addPlatformPreview {
	return []addPlatformPreview{
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
}

// printAddPreview prints the "Step 2: Preview" block — the canonical
// ~/.agents/ tree plus the per-platform table with installed/not-installed
// detection — and the "About Link Types" info box.
func printAddPreview(projectName, projectPath, agentsHome string) {
	displayPath := config.DisplayPath(projectPath)
	displayAgentsHome := config.DisplayPath(agentsHome)

	ui.Step("The following will be created:")
	ui.PreviewSection(displayAgentsHome+"/",
		"rules/"+projectName+"/              (project rules)",
		"settings/"+projectName+"/           (project settings)",
		"  └── claude-code.json            (hooks, permissions)",
		"mcp/"+projectName+"/                (project MCP configs)",
		"skills/"+projectName+"/             (project skills)",
		"agents/"+projectName+"/             (project subagents)",
	)

	fmt.Fprintf(os.Stdout, "\n  %s%s/%s\n", ui.Bold, displayPath, ui.Reset)
	for _, pp := range addPlatformPreviews(projectName) {
		printOnePlatformPreview(pp)
	}

	ui.InfoBox("About Link Types",
		"Cursor uses HARD LINKS (required by IDE).",
		"Other agents use symlinks for flexibility.",
	)
}

// printOnePlatformPreview prints one preview row + its child items, dimming
// the row and skipping the items when the platform is not installed.
func printOnePlatformPreview(pp addPlatformPreview) {
	installed := false
	for _, p := range platform.All() {
		if p.ID() == pp.id && p.IsInstalled() {
			installed = true
			break
		}
	}
	if !installed {
		fmt.Fprintf(os.Stdout, "    %s%s %s(not installed — skipped)%s\n", ui.Dim, pp.name, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(os.Stdout, "    %s%s%s %s(%s)%s\n", ui.Cyan, pp.name, ui.Reset, ui.Dim, pp.linkNote, ui.Reset)
	for _, item := range pp.items {
		fmt.Fprintf(os.Stdout, "      %s%s%s\n", ui.Dim, item, ui.Reset)
	}
}

// reportAddExistingFiles prints the "Files to Replace" section: for each
// root-level file that will be replaced by a managed link, one yellow
// bullet with file/symlink kind.
func reportAddExistingFiles(existingFiles []string, projectName, projectPath string) {
	if len(existingFiles) == 0 {
		return
	}
	ui.Section("Files to Replace")
	fmt.Fprintf(os.Stdout, "  %sThese root-level files will be backed up and replaced with links:%s\n", ui.Yellow, ui.Reset)
	for _, f := range existingFiles {
		rel := strings.TrimPrefix(f, projectPath+"/")
		fileType := "file"
		if _, isLink := links.ManagedLinkTarget(f); isLink {
			fileType = "symlink"
		}
		fmt.Fprintf(os.Stdout, "  %s!%s %s %s(%s)%s\n", ui.Yellow, ui.Reset, rel, ui.Dim, fileType, ui.Reset)
	}
	fmt.Fprintf(os.Stdout, "\n  %sBackups stored in ~/.agents/resources/%s/backups/<timestamp>/%s\n", ui.Dim, projectName, ui.Reset)
}

// reportDiscoveredAIConfigs prints the "Other AI Configs Discovered" section
// for any AI config files outside the to-be-replaced set, capped at 10 lines
// with an "... and N more" trailer.
func reportDiscoveredAIConfigs(existingFiles []string, projectPath string) {
	allAIConfigs := scanExistingAIConfigs(projectPath)
	existingSet := map[string]bool{}
	for _, f := range existingFiles {
		existingSet[f] = true
	}
	var discoveredElsewhere []string
	for _, f := range allAIConfigs {
		if !existingSet[f] {
			discoveredElsewhere = append(discoveredElsewhere, f)
		}
	}
	if len(discoveredElsewhere) == 0 {
		return
	}
	ui.Section("Other AI Configs Discovered")
	fmt.Fprintf(os.Stdout, "  %sFound AI agent configs elsewhere in the repo (not replaced):%s\n", ui.Cyan, ui.Reset)
	shown := 0
	for _, f := range discoveredElsewhere {
		if shown >= 10 {
			break
		}
		rel := strings.TrimPrefix(f, projectPath+"/")
		kind := "file"
		if _, isLink := links.ManagedLinkTarget(f); isLink {
			kind = "symlink"
		} else if info, err := os.Lstat(f); err == nil && info.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(os.Stdout, "  %s○%s %s %s(%s)%s\n", ui.Dim, ui.Reset, rel, ui.Dim, kind, ui.Reset)
		shown++
	}
	if len(discoveredElsewhere) > 10 {
		fmt.Fprintf(os.Stdout, "  %s... and %d more%s\n", ui.Dim, len(discoveredElsewhere)-10, ui.Reset)
	}
	fmt.Fprintf(os.Stdout, "\n  %sConsider migrating these to ~/.agents/ for centralized management.%s\n", ui.Dim, ui.Reset)
}

// confirmAddProceed prompts the user when --yes is not set. Returns true
// when the user declined (caller returns nil to skip the rest of the run).
func confirmAddProceed(existingFiles []string) bool {
	confirmMsg := "Proceed?"
	if len(existingFiles) > 0 {
		confirmMsg = fmt.Sprintf("Proceed? (%d file(s) will be backed up and replaced)", len(existingFiles))
	}
	if Flags.Yes {
		return false
	}
	if !ui.Confirm(confirmMsg, false) {
		ui.Info("Add cancelled.")
		return true
	}
	return false
}

// backupAddExistingFiles runs Step 3 (backup existing configs) when there
// are files to back up. A failed backup aborts add with a typed error that
// guarantees the user's only copy of unmanaged configs is preserved.
func backupAddExistingFiles(existingFiles []string, projectName, projectPath, agentsHome string, deps addDeps) error {
	if len(existingFiles) == 0 {
		return nil
	}
	ui.Step("Backing up existing configs...")
	timestamp := time.Now().Format("20060102-150405")
	backed, backupErr := backupExistingConfigsList(existingFiles, projectPath, agentsHome, projectName, timestamp, deps)
	if backupErr != nil {
		ui.Bullet("warn", fmt.Sprintf("backup failed: %v", backupErr))
		return ErrorWithHints(
			fmt.Sprintf("aborting add for '%s': could not back up existing configs", projectName),
			"No files were removed and the project was NOT registered. "+
				"Ensure ~/.agents/resources is writable and has free space, then re-run `da add`.",
		)
	}
	ui.Bullet("ok", fmt.Sprintf("Backed up %d existing file(s)", backed))
	ui.Bullet("ok", fmt.Sprintf("Stored backups in ~/.agents/resources/%s/backups/%s/", projectName, timestamp))
	return nil
}

// scaffoldAddProjectDirs runs Step 4: creates project dirs, restores from
// active resources, and writes KG MCP configs. Aborts on a partial restore
// per the no-false-success invariant.
func scaffoldAddProjectDirs(projectName, projectPath, agentsHome string, deps addDeps) error {
	ui.Step("Creating project structure...")
	if err := projectsync.CreateProjectDirs(projectName); err != nil {
		return err
	}
	ui.Bullet("ok", "Created ~/.agents/ directories")

	restored, restoreErr := restoreFromResourcesCountedWithDeps(projectName, projectPath, deps)
	if restored > 0 {
		ui.Bullet("ok", fmt.Sprintf("Restored %d item(s) from ~/.agents/resources/%s/", restored, projectName))
	}
	if restoreErr != nil {
		ui.Bullet("warn", fmt.Sprintf("restore from resources incomplete: %v", restoreErr))
		return ErrorWithHints(
			fmt.Sprintf("add incomplete for '%s': could not restore resources: %v", projectName, restoreErr),
			"The project was NOT registered (partial resource restore). "+
				"Resolve the errors above (permissions, free space under ~/.agents/resources), "+
				"then re-run `da add`.",
		)
	}
	if err := ensureProjectKGMCPConfigs(projectName, projectPath, agentsHome, deps); err != nil {
		return fmt.Errorf("writing KG MCP configs: %w", err)
	}
	return nil
}

// createAddLinks runs Step 5: shared-target projection followed by every
// installed platform's CreateLinks. Returns a typed error listing every
// link failure when any failed — the caller must NOT register the project
// or print the success box (false-success invariant).
func createAddLinks(projectName, projectPath string) error {
	ui.Step("Creating links...")
	config.SetWindowsMirrorContext(projectPath)

	var addInstalled []platform.Platform
	for _, p := range platform.All() {
		if p.IsInstalled() {
			addInstalled = append(addInstalled, p)
		}
	}
	var linkFailures []string
	if _, err := platform.RunSharedTargetProjection(projectName, projectPath, addInstalled, false); err != nil {
		ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		linkFailures = append(linkFailures, fmt.Sprintf("shared targets: %v", err))
	}
	for _, p := range addInstalled {
		if err := p.CreateLinks(projectName, projectPath); err != nil {
			ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
			linkFailures = append(linkFailures, fmt.Sprintf("%s: %v", p.DisplayName(), err))
			continue
		}
		ui.Bullet("ok", p.DisplayName()+" links created")
	}
	if len(linkFailures) > 0 {
		return ErrorWithHints(
			fmt.Sprintf("add incomplete for '%s': %s", projectName, strings.Join(linkFailures, "; ")),
			"The project was NOT registered (partial link application). "+
				"Resolve the warnings above — unmanaged files occupying managed targets "+
				"must be imported (da import), backed up, or removed — then re-run `da add`.",
		)
	}
	return nil
}

// registerAddedProject persists the project in config.json. Only call after
// every prior step succeeded — registration is the success-stamp moment.
func registerAddedProject(cfg *config.Config, projectName, projectPath string) error {
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Registered in config.json")
	return nil
}

// emitAddSuccessBox prints the final success box with project-specific next
// steps: rule-editing hint, audit hint, manifest hint (apply vs generate),
// and the migrate hint when deprecated formats were detected.
func emitAddSuccessBox(projectName, projectPath string, hasDeprecated bool) {
	nextSteps := []string{
		"Add project rules: edit ~/.agents/rules/" + projectName + "/rules.md",
		"Check applied configs: da status --audit",
	}
	if _, err := config.LoadAgentsRC(projectPath); err == nil {
		nextSteps = append(nextSteps, "Manifest found — apply it: da install")
	} else {
		nextSteps = append(nextSteps, "Make it git-portable: da install --generate")
	}
	if hasDeprecated {
		nextSteps = append(nextSteps, "Migrate deprecated formats: da migrate detect")
	}
	ui.SuccessBox(fmt.Sprintf("Project '%s' added successfully!", projectName), nextSteps...)
}

// backupExistingConfigsList backs up the given files into ~/.agents/resources/<project>/...
// and removes the originals from the project tree. No *.dot-agents-backup files are left
// in the project. Returns count of files processed and a non-nil error if any required
// backup copy failed. On backup failure the original is NOT removed (the user's only
// copy is preserved) and the error aborts runAdd before any destructive removal.
func backupExistingConfigsList(files []string, projectPath, agentsHome, project, timestamp string, deps addDeps) (int, error) {
	count := 0
	for _, f := range files {
		// Safety: never back up backup artifacts
		if isBackupArtifact(filepath.Base(f)) {
			continue
		}
		if _, err := os.Lstat(f); err != nil {
			continue
		}
		// A PROVEN managed link (resolvable POSIX symlink / Windows junction
		// whose target resolves under the canonical agents root) has no
		// standalone content to preserve — remove it without a backup.
		// A merely-resolvable link is NOT proof: a project-owned
		// symlink/junction pointing at a real user file OUTSIDE dot-agents
		// (the symlink twin of the unmanaged-hard-link case below) carries
		// the user's only copy of that config. Dropping it without mirroring
		// the resolved content destroys it while claiming a backup. Such an
		// unmanaged link falls through to the normal mirror/backup path,
		// which copies the resolved bytes before removal.
		if links.IsManagedLinkUnder(f, agentsHome) {
			os.Remove(f)
			count++
			continue
		}
		// A hard link is only safe to drop without a backup when it is PROVEN
		// managed: its inode is shared with the canonical source this candidate
		// maps to under agentsHome. A bare nlink>1 is NOT proof — an
		// UNMANAGED hard-linked AGENTS.md/.mcp.json (e.g. the project hard
		// links its real config elsewhere) also has nlink>1, and dropping it
		// without a mirror backup destroys the project's real config while
		// claiming it was backed up. Unknown/unmanaged hard links fall through
		// to the normal backup/mirror path below.
		if hasMultipleHardLinks(f) && isManagedHardlinkToCanonicalSource(project, projectPath, f, agentsHome) {
			os.Remove(f)
			count++
			continue
		}
		// Regular file: copy into resources, then delete from project.
		// The removal below is destructive — it deletes the user's only
		// copy of an unmanaged config. Only proceed once the required
		// backup copies have actually landed; otherwise abort so runAdd
		// returns an error WITHOUT removing the original.
		if err := mirrorBackupChecked(project, projectPath, f, timestamp, deps); err != nil {
			return count, fmt.Errorf("backing up %s: %w", f, err)
		}
		if err := deps.Remove(f); err != nil {
			continue
		}
		count++
	}
	return count, nil
}

// restoreFromResourcesCounted restores files from ~/.agents/resources/<project>/
// and returns the number of files restored plus a non-nil error if any
// directory walk, mkdir, write, or copy failed. Callers that stamp success
// (e.g. refresh metadata) MUST observe this error: a partially-applied
// restore that is reported as success makes retries and doctor/refresh
// recovery ambiguous.
// restoreFromResourcesCounted is the legacy entry point retained for
// refresh.go's restoreFromResources wrapper. Delegates to the deps-aware
// implementation with stdAddDeps. The atomic-delete commit can fold this into
// a single deps-aware function once refresh.go is converted.
func restoreFromResourcesCounted(project, projectPath string) (int, error) {
	return restoreFromResourcesCountedWithDeps(project, projectPath, stdAddDeps{})
}

func restoreFromResourcesCountedWithDeps(project, projectPath string, deps addDeps) (int, error) {
	agentsHome := config.AgentsHome()
	resourcesDir := filepath.Join(agentsHome, "resources", project)
	info, err := os.Stat(resourcesDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No resources to restore is not a failure.
			return 0, nil
		}
		// A permission-denied / broken-symlink / other non-ENOENT stat
		// error is NOT "nothing to restore": treating it as success makes
		// refresh stamp fresh metadata over backed-up resource data that
		// was never restored. Surface it so refresh.go's projectFailed
		// path fires.
		return 0, fmt.Errorf("stat resources dir %s: %w", resourcesDir, err)
	}
	if !info.IsDir() {
		// A non-directory squatting the resources path cannot be walked;
		// silently skipping it would also mask unrestored data.
		return 0, fmt.Errorf("resources path %s is not a directory", resourcesDir)
	}
	count := 0
	var restoreErr error
	walkErr := filepath.WalkDir(resourcesDir, func(path string, d os.DirEntry, err error) error {
		n, ferr := restoreResourceFileCount(project, resourcesDir, agentsHome, path, d, err, deps)
		count += n
		if ferr != nil && restoreErr == nil {
			restoreErr = ferr
		}
		return nil
	})
	if walkErr != nil && restoreErr == nil {
		restoreErr = fmt.Errorf("walking resources dir %s: %w", resourcesDir, walkErr)
	}
	return count, restoreErr
}

func restoreResourceFileCount(project, resourcesDir, agentsHome, path string, d os.DirEntry, walkErr error, deps addDeps) (int, error) {
	if walkErr != nil {
		return 0, fmt.Errorf("walking %s: %w", path, walkErr)
	}
	if d.IsDir() {
		return 0, nil
	}
	relPath, err := filepath.Rel(resourcesDir, path)
	if err != nil {
		return 0, fmt.Errorf("resolving relative path for %s: %w", path, err)
	}
	relPath = filepath.ToSlash(relPath)
	if strings.HasPrefix(relPath, "backups/") || isCanonicalResourceBackupRel(relPath) {
		return 0, nil
	}
	canonicalCount, handled, canonErr := restoreCanonicalResourceFile(project, resourcesDir, agentsHome, path, deps)
	if handled {
		return canonicalCount, canonErr
	}
	return restoreLegacyResourceFile(project, relPath, agentsHome, path, deps)
}

func isCanonicalResourceBackupRel(relPath string) bool {
	for _, prefix := range []string{"rules/", "settings/", "mcp/", "skills/", "agents/", agentsHooksPrefix} {
		if strings.HasPrefix(relPath, prefix) {
			return true
		}
	}
	return false
}

func restoreCanonicalResourceFile(project, resourcesDir, agentsHome, path string, deps addDeps) (int, bool, error) {
	candidate := importCandidate{
		project:    project,
		sourceRoot: resourcesDir,
		sourcePath: path,
	}
	outputs, ok, canonErr := canonicalImportOutputs(candidate)
	if !ok {
		return 0, false, nil
	}
	if canonErr != nil {
		return 0, true, fmt.Errorf("canonical import for %s: %w", path, canonErr)
	}
	count := 0
	for _, output := range outputs {
		destPath := filepath.Join(agentsHome, output.destRel)
		if err := deps.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return count, true, fmt.Errorf("creating dir for %s: %w", destPath, err)
		}
		if err := deps.WriteFile(destPath, output.content, 0644); err != nil {
			return count, true, fmt.Errorf("writing %s: %w", destPath, err)
		}
		count++
	}
	return count, true, nil
}

func restoreLegacyResourceFile(project, relPath, agentsHome, path string, deps addDeps) (int, error) {
	destRel := mapResourceRelToDest(project, relPath)
	if destRel == "" {
		return 0, nil
	}
	destPath := filepath.Join(agentsHome, destRel)
	if err := deps.CopyFile(path, destPath); err != nil {
		return 0, fmt.Errorf("restoring %s -> %s: %w", path, destPath, err)
	}
	return 1, nil
}

// mirrorBackup copies srcFile (original path, before deletion) into the
// ~/.agents/resources/<project>/ tree using the file's original relative path.
// No *.dot-agents-backup suffix is added anywhere.
//
// This is the errorless wrapper retained for import.go callers, whose own
// failure handling keys off the subsequent CopyFile into the destination
// (a mirror-backup failure there does not destroy the user's only copy
// because import.go never removes the source after mirrorBackup). Callers
// that delete the original after backing it up (backupExistingConfigsList)
// MUST use mirrorBackupChecked so a failed backup aborts before the
// destructive removal.
func mirrorBackup(project, projectPath, srcFile, timestamp string) {
	_ = mirrorBackupChecked(project, projectPath, srcFile, timestamp, stdAddDeps{})
}

// mirrorBackupChecked performs the same copy as mirrorBackup but propagates
// the CopyFile errors. backupExistingConfigsList relies on this: it removes
// the user's only copy of an unmanaged config after backing it up, so a
// silent backup failure (unwritable ~/.agents/resources, disk full,
// unreadable source through a symlink) would destroy that config while
// reporting a successful backup.
func mirrorBackupChecked(project, projectPath, srcFile, timestamp string, deps addDeps) error {
	agentsHome := config.AgentsHome()
	relPath, err := filepath.Rel(projectPath, srcFile)
	if err != nil || relPath == "." || strings.HasPrefix(relPath, "..") {
		relPath = filepath.Base(srcFile)
	}

	// Active (latest) copy — overwritten on each backup run. This is the
	// recoverable copy `da refresh` / restore reads back, so it is required.
	activeTarget := filepath.Join(agentsHome, "resources", project, relPath)
	if cpErr := deps.CopyFile(srcFile, activeTarget); cpErr != nil {
		return fmt.Errorf("backing up %s -> %s: %w", srcFile, activeTarget, cpErr)
	}

	// Timestamped immutable copy — also required when a timestamp is given:
	// it is the only point-in-time snapshot the user can recover from.
	if timestamp != "" {
		tsTarget := filepath.Join(agentsHome, "resources", project, "backups", timestamp, relPath)
		if cpErr := deps.CopyFile(srcFile, tsTarget); cpErr != nil {
			return fmt.Errorf("backing up %s -> %s: %w", srcFile, tsTarget, cpErr)
		}
	}
	return nil
}

// writeKGMCPConfigs / writeKGMCPConfigFile / ensureGlobalKGMCPConfigs
// are thin re-aliases over the canonical helpers in
// commands/lifecycle/kgmcp.go (lifted in root-command-decomposition
// t02b). Function-var aliases keep callers in init.go, seams_test.go,
// and add_test.go unchanged until the parent command itself moves into
// commands/lifecycle/ in t05 / t08, at which point these wrappers go
// away in favor of direct package-qualified references.
var (
	writeKGMCPConfigs        = lifecycle.WriteKGMCPConfigs
	writeKGMCPConfigFile     = lifecycle.WriteKGMCPConfigFile
	ensureGlobalKGMCPConfigs = lifecycle.EnsureGlobalKGMCPConfigs
	kgConfigPath             = lifecycle.KGConfigPath
)

// ensureProjectKGMCPConfigs writes per-project KG MCP configs when the
// project's .agentsrc.json declares a KG section. The shared KG MCP
// writers live in commands/lifecycle/kgmcp.go after t02b — this thin
// wrapper retains the manifest-aware gate.
func ensureProjectKGMCPConfigs(projectName, projectPath, agentsHome string, deps addDeps) error {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return nil
	}
	if rc.KG == nil {
		return nil
	}
	return lifecycle.WriteKGMCPConfigs(filepath.Join(agentsHome, "mcp", projectName), deps)
}
