package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type plannedResource struct {
	Intent     ResourceIntent
	Duplicates []ResourceIntent
}

type ResourcePlan struct {
	Resources []plannedResource
}

func BuildResourcePlan(intents []ResourceIntent) (ResourcePlan, error) {
	byConflict := map[string][]ResourceIntent{}
	for _, intent := range intents {
		if err := intent.Validate(); err != nil {
			return ResourcePlan{}, fmt.Errorf("validate %s: %w", intent.IntentID, err)
		}
		byConflict[intent.EffectiveConflictKey()] = append(byConflict[intent.EffectiveConflictKey()], intent)
	}

	keys := make([]string, 0, len(byConflict))
	for key := range byConflict {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	plan := ResourcePlan{Resources: make([]plannedResource, 0, len(keys))}
	for _, key := range keys {
		group := byConflict[key]
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].TargetPath == group[j].TargetPath {
				return group[i].IntentID < group[j].IntentID
			}
			return group[i].TargetPath < group[j].TargetPath
		})

		base := group[0]
		resource := plannedResource{Intent: base}
		for _, candidate := range group[1:] {
			if !resourceIntentCompatible(base, candidate) {
				return ResourcePlan{}, fmt.Errorf(
					"conflicting intents for %s: %s (%s) vs %s (%s)",
					key,
					base.IntentID,
					base.SourceRef.CanonicalPath(".agents"),
					candidate.IntentID,
					candidate.SourceRef.CanonicalPath(".agents"),
				)
			}
			resource.Duplicates = append(resource.Duplicates, candidate)
		}
		plan.Resources = append(plan.Resources, resource)
	}

	sort.SliceStable(plan.Resources, func(i, j int) bool {
		return plan.Resources[i].Intent.TargetPath < plan.Resources[j].Intent.TargetPath
	})
	return plan, nil
}

// resourceIntentCompatible reports whether two intents with the same conflict key are
// identical in every field that affects execution. All struct fields are compared
// explicitly; if ResourceIntent gains a new field, this function must be updated to
// include it — otherwise two semantically different intents could be silently merged.
func resourceIntentCompatible(left, right ResourceIntent) bool {
	if left.TargetPath != right.TargetPath ||
		left.Ownership != right.Ownership ||
		left.SourceRef != right.SourceRef ||
		left.Shape != right.Shape ||
		left.Transport != right.Transport ||
		left.Materializer != right.Materializer ||
		left.ReplacePolicy != right.ReplacePolicy ||
		left.PrunePolicy != right.PrunePolicy {
		return false
	}
	return sameStrings(left.MarkerFiles, right.MarkerFiles)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]string(nil), left...)
	rightCopy := append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	for i := range leftCopy {
		if leftCopy[i] != rightCopy[i] {
			return false
		}
	}
	return true
}

func (p ResourcePlan) Execute(repoPath, agentsHome string) error {
	for _, resource := range p.Resources {
		if err := executeResourceIntent(resource.Intent, repoPath, agentsHome); err != nil {
			return fmt.Errorf("%s: %w", resource.Intent.IntentID, err)
		}
	}
	return nil
}

func executeResourceIntent(intent ResourceIntent, repoPath, agentsHome string) error {
	switch {
	case intent.Shape == ResourceShapeDirectDir && intent.Transport == ResourceTransportSymlink:
		src := intent.SourceRef.CanonicalPath(agentsHome)
		if src == "" {
			return fmt.Errorf("empty source path")
		}
		target := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return ensureDirSymlinkIntent(src, target, intent)
	case intent.Shape == ResourceShapeDirectFile && intent.Transport == ResourceTransportSymlink:
		src := intent.SourceRef.CanonicalPath(agentsHome)
		if src == "" {
			return fmt.Errorf("empty source path")
		}
		target := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return ensureFileSymlinkIntent(src, target, intent)
	case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
		return executeRenderSingleWrite(intent, repoPath, agentsHome)
	default:
		return fmt.Errorf("unsupported intent shape/transport %s/%s", intent.Shape, intent.Transport)
	}
}

func resolveIntentTargetPath(targetPath, repoPath string) string {
	if filepath.IsAbs(targetPath) {
		return targetPath
	}
	return filepath.Join(repoPath, targetPath)
}

func ensureDirSymlinkIntent(src, target string, intent ResourceIntent) error {
	info, err := os.Lstat(target)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return links.Symlink(src, target)
		}
		if err := prepareIntentTargetForReplacement(target, intent); err != nil {
			return err
		}
	case os.IsNotExist(err):
	default:
		return err
	}
	return links.Symlink(src, target)
}

func ensureFileSymlinkIntent(src, target string, intent ResourceIntent) error {
	info, err := os.Lstat(target)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return links.Symlink(src, target)
		}
		if err := prepareIntentTargetForReplacement(target, intent); err != nil {
			return err
		}
	case os.IsNotExist(err):
	default:
		return err
	}
	return links.Symlink(src, target)
}

func executeRenderSingleWrite(intent ResourceIntent, repoPath, agentsHome string) error {
	switch intent.Materializer {
	case "codex-agent-toml":
		src := intent.SourceRef.CanonicalPath(agentsHome)
		if src == "" {
			return fmt.Errorf("empty source path")
		}
		dst := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return writeCodexAgentTomlFile(dst, src)
	default:
		return fmt.Errorf("unsupported materializer %q for render intent", intent.Materializer)
	}
}

func prepareIntentTargetForReplacement(target string, intent ResourceIntent) error {
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		switch intent.ReplacePolicy {
		case ResourceReplaceNever:
			return fmt.Errorf("refusing to replace existing file %s", target)
		case ResourceReplaceAllowlistedImportedDirOnly:
			if !isAllowlistedSharedMirrorTarget(intent.TargetPath) {
				return fmt.Errorf("refusing to replace unmanaged file %s", target)
			}
			return os.Remove(target)
		default:
			return os.Remove(target)
		}
	}

	switch intent.ReplacePolicy {
	case ResourceReplaceAllowlistedImportedDirOnly:
		return removeImportedDirIfAllowlisted(target, intent)
	case ResourceReplaceIfManaged:
		return fmt.Errorf("refusing to replace unmanaged directory %s", target)
	case ResourceReplaceNever:
		return fmt.Errorf("refusing to replace existing directory %s", target)
	default:
		return fmt.Errorf("unsupported replace policy %s for directory target", intent.ReplacePolicy)
	}
}

func removeImportedDirIfAllowlisted(target string, intent ResourceIntent) error {
	if !isAllowlistedSharedMirrorTarget(intent.TargetPath) {
		return fmt.Errorf("target %s is not allowlisted for imported directory replacement", intent.TargetPath)
	}
	for _, marker := range intent.MarkerFiles {
		if marker == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(target, marker)); err == nil {
			return os.RemoveAll(target)
		}
	}
	return fmt.Errorf("refusing to replace unmanaged directory %s without imported markers", target)
}

func isAllowlistedSharedMirrorTarget(targetPath string) bool {
	normalized := filepath.ToSlash(targetPath)
	return strings.HasPrefix(normalized, ".agents/skills/") ||
		strings.HasPrefix(normalized, ".claude/skills/") ||
		strings.HasPrefix(normalized, ".claude/agents/") ||
		strings.HasPrefix(normalized, ".codex/agents/") ||
		strings.HasPrefix(normalized, ".opencode/plugins/") ||
		strings.HasPrefix(normalized, ".opencode/agent/") ||
		strings.HasPrefix(normalized, ".github/agents/")
}

func BuildSharedSkillMirrorIntents(project string, targetRoots ...string) ([]ResourceIntent, error) {
	intents := make([]ResourceIntent, 0)
	for _, root := range targetRoots {
		root = filepath.Clean(root)
		if root == "." {
			continue
		}
		intents = append(intents, buildSharedSkillMirrorIntentsForRoot(project, root)...)
	}
	return intents, nil
}

func buildSharedSkillMirrorIntentsForRoot(project, targetRoot string) []ResourceIntent {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "skills", project, "SKILL.md")
	if err != nil {
		return nil
	}

	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(targetRoot, entry.Name)
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("skills.%s.%s.%s", project, entry.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      "skills",
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       "skills",
				RelativePath: entry.Name,
				Kind:         ResourceSourceCanonicalDir,
				Origin:       "shared-skill-mirror",
			},
			Shape:         ResourceShapeDirectDir,
			Transport:     ResourceTransportSymlink,
			Materializer:  "shared-skill-dir-symlink",
			ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
			PrunePolicy:   ResourcePruneTarget,
			MarkerFiles:   []string{"SKILL.md"},
		})
	}
	return intents
}

const pluginManifestName = "PLUGIN.yaml"

// BuildSharedPluginBundleIntents returns ResourceIntents for each canonical plugin bundle
// under ~/.agents/plugins/{scope}/ pointing at the given target roots. Each platform's
// SharedTargetIntents calls this with its own native plugin target path (e.g. OpenCode uses
// .opencode/plugins/, Cursor uses .cursor-plugin/, Claude uses .claude-plugin/, etc.).
// Platforms that do not yet have an emitter for their native plugin format simply omit this
// call from their SharedTargetIntents implementation — add it there when the emitter lands.
func BuildSharedPluginBundleIntents(project string, targetRoots ...string) ([]ResourceIntent, error) {
	intents := make([]ResourceIntent, 0)
	for _, root := range targetRoots {
		root = filepath.Clean(root)
		if root == "." {
			continue
		}
		intents = append(intents, buildSharedPluginBundleIntentsForRoot(project, root)...)
	}
	return intents, nil
}

func buildSharedPluginBundleIntentsForRoot(project, targetRoot string) []ResourceIntent {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "plugins", project, pluginManifestName)
	if err != nil {
		return nil
	}

	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(targetRoot, entry.Name)
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("plugins.%s.%s.%s", project, entry.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      "plugins",
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       "plugins",
				RelativePath: entry.Name,
				Kind:         ResourceSourceCanonicalBundle,
				Origin:       "shared-plugin-bundle",
			},
			Shape:         ResourceShapeDirectDir,
			Transport:     ResourceTransportSymlink,
			Materializer:  "shared-plugin-dir-symlink",
			ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
			PrunePolicy:   ResourcePruneTarget,
			MarkerFiles:   []string{pluginManifestName},
		})
	}
	return intents
}

func sanitizeIntentRoot(root string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ".", "")
	return replacer.Replace(root)
}

// BuildSharedAgentMirrorIntents builds symlink intents for canonical agents/ buckets
// (per-entry directories with AGENT.md) into the given repo-relative target roots.
func BuildSharedAgentMirrorIntents(project string, targetRoots ...string) ([]ResourceIntent, error) {
	intents := make([]ResourceIntent, 0)
	for _, root := range targetRoots {
		root = filepath.Clean(root)
		if root == "." {
			continue
		}
		intents = append(intents, buildSharedAgentMirrorIntentsForRoot(project, root)...)
	}
	return intents, nil
}

// BuildSharedAgentFileSymlinkIntents builds symlink intents from each canonical
// AGENT.md file to a repo-local file path (OpenCode `.md`, Copilot `.agent.md`).
func BuildSharedAgentFileSymlinkIntents(project, targetRoot, destFileSuffix string) ([]ResourceIntent, error) {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, "AGENT.md")
	if err != nil {
		return nil, nil
	}
	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(targetRoot, entry.Name+destFileSuffix)
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.file.%s.%s.%s", project, entry.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      "agents",
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       "agents",
				RelativePath: filepath.Join(entry.Name, "AGENT.md"),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       "shared-agent-file-symlink",
			},
			Shape:         ResourceShapeDirectFile,
			Transport:     ResourceTransportSymlink,
			Materializer:  "shared-agent-file-symlink",
			ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
			PrunePolicy:   ResourcePruneTarget,
		})
	}
	return intents, nil
}

// BuildSharedCodexAgentTomlIntents builds render intents for `.codex/agents/*.toml`
// from canonical project agent directories.
func BuildSharedCodexAgentTomlIntents(project string) ([]ResourceIntent, error) {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, "AGENT.md")
	if err != nil {
		return nil, nil
	}
	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(".codex", "agents", entry.Name+".toml")
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.codex-toml.%s.%s", project, entry.Name),
			Project:     project,
			Bucket:      "agents",
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       "agents",
				RelativePath: filepath.Join(entry.Name, "AGENT.md"),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       "shared-codex-agent-toml",
			},
			Shape:         ResourceShapeRenderSingle,
			Transport:     ResourceTransportWrite,
			Materializer:  "codex-agent-toml",
			ReplacePolicy: ResourceReplaceIfManaged,
			PrunePolicy:   ResourcePruneNone,
		})
	}
	return intents, nil
}

func buildSharedAgentMirrorIntentsForRoot(project, targetRoot string) []ResourceIntent {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, "AGENT.md")
	if err != nil {
		return nil
	}

	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(targetRoot, entry.Name)
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.%s.%s.%s", project, entry.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      "agents",
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       "agents",
				RelativePath: entry.Name,
				Kind:         ResourceSourceCanonicalDir,
				Origin:       "shared-agent-mirror",
			},
			Shape:         ResourceShapeDirectDir,
			Transport:     ResourceTransportSymlink,
			Materializer:  "shared-agent-dir-symlink",
			ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
			PrunePolicy:   ResourcePruneTarget,
			MarkerFiles:   []string{"AGENT.md"},
		})
	}
	return intents
}

func collectSharedTargetIntents(project string, platforms []Platform) ([]ResourceIntent, error) {
	var all []ResourceIntent
	for _, p := range platforms {
		intents, err := p.SharedTargetIntents(project)
		if err != nil {
			return nil, fmt.Errorf("%s shared intents: %w", p.ID(), err)
		}
		all = append(all, intents...)
	}
	return all, nil
}

// BuildSharedTargetPlan aggregates SharedTargetIntents from all provided platforms and
// builds a single merged ResourcePlan (dedupe, conflict detection). Dry-run and execute
// paths both use this so intent collection and planning happen once per operation.
func BuildSharedTargetPlan(project string, platforms []Platform) (ResourcePlan, error) {
	all, err := collectSharedTargetIntents(project, platforms)
	if err != nil {
		return ResourcePlan{}, err
	}
	return BuildResourcePlan(all)
}

// RunSharedTargetProjection is the command-layer entry point for shared-target
// projection: it builds the merged ResourcePlan (BuildSharedTargetPlan) and either
// returns dry-run preview lines or executes writes. This keeps refresh/install/add on
// one code path for "build intents → plan → dry-run or apply".
//
// Callers must set config.SetWindowsMirrorContext(repoPath) before calling when the
// repo needs Windows-specific path behavior for intent resolution.
func RunSharedTargetProjection(project, repoPath string, platforms []Platform, dryRun bool) ([]string, error) {
	if dryRun {
		return DryRunSharedTargetPlanLines(project, repoPath, platforms)
	}
	return nil, CollectAndExecuteSharedTargetPlan(project, repoPath, platforms)
}

// CollectAndExecuteSharedTargetPlan runs BuildSharedTargetPlan then executes it against
// the repo and agents home. This is the command-layer entry point for centralized
// shared-target writes.
func CollectAndExecuteSharedTargetPlan(project, repoPath string, platforms []Platform) error {
	plan, err := BuildSharedTargetPlan(project, platforms)
	if err != nil {
		return err
	}
	if len(plan.Resources) == 0 {
		return nil
	}
	return plan.Execute(repoPath, config.AgentsHome())
}

// RemoveSharedTargetPlan removes repo-local shared targets implied by the merged plan for
// the given platforms (same aggregation as CollectAndExecuteSharedTargetPlan). Symlinks
// are removed only when they point into agentsHome; rendered files are removed for known
// materializers (e.g. codex-agent-toml).
func RemoveSharedTargetPlan(project, repoPath string, platforms []Platform) error {
	plan, err := BuildSharedTargetPlan(project, platforms)
	if err != nil {
		return err
	}
	return plan.RemoveSharedTargets(repoPath, config.AgentsHome())
}

// RemoveSharedTargets deletes managed outputs for each resource in the plan.
func (p ResourcePlan) RemoveSharedTargets(repoPath, agentsHome string) error {
	for _, res := range p.Resources {
		if err := removeManagedIntentTarget(res.Intent, repoPath, agentsHome); err != nil {
			return fmt.Errorf("%s: %w", res.Intent.IntentID, err)
		}
	}
	return nil
}

func removeManagedIntentTarget(intent ResourceIntent, repoPath, agentsHome string) error {
	target := resolveIntentTargetPath(intent.TargetPath, repoPath)
	switch {
	case intent.Shape == ResourceShapeDirectDir && intent.Transport == ResourceTransportSymlink:
		_ = links.RemoveIfSymlinkUnder(target, agentsHome)
		return nil
	case intent.Shape == ResourceShapeDirectFile && intent.Transport == ResourceTransportSymlink:
		_ = links.RemoveIfSymlinkUnder(target, agentsHome)
		return nil
	case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
		switch intent.Materializer {
		case "codex-agent-toml":
			_ = os.Remove(target)
			return nil
		default:
			return fmt.Errorf("unsupported materializer %q for remove", intent.Materializer)
		}
	default:
		// Unknown shape/transport combos are intentionally a no-op during removal (unlike
		// Execute, which errors). The planner prevents unknown combos from being created;
		// if one somehow reaches here the safest outcome is to leave the target in place
		// rather than error-loop on every refresh.
		return nil
	}
}

// DryRunSharedTargetPlanLines describes what CollectAndExecuteSharedTargetPlan would
// write (merged shared-target rows, duplicate-intent counts) without touching the filesystem.
func DryRunSharedTargetPlanLines(project, repoPath string, platforms []Platform) ([]string, error) {
	plan, err := BuildSharedTargetPlan(project, platforms)
	if err != nil {
		return nil, err
	}
	if len(plan.Resources) == 0 {
		return []string{"shared targets: (none)"}, nil
	}
	return formatSharedTargetPlanForDryRun(plan, repoPath), nil
}

func formatSharedTargetPlanForDryRun(plan ResourcePlan, repoPath string) []string {
	agentsHome := config.AgentsHome()
	var lines []string
	for _, res := range plan.Resources {
		intent := res.Intent
		src := intent.SourceRef.CanonicalPath(agentsHome)
		if src == "" {
			src = "(unknown source)"
		}
		dest := resolveIntentTargetPath(intent.TargetPath, repoPath)
		var line string
		switch {
		case intent.Shape == ResourceShapeDirectDir && intent.Transport == ResourceTransportSymlink:
			line = fmt.Sprintf("shared target: symlink %s -> %s", config.DisplayPath(dest), config.DisplayPath(src))
		case intent.Shape == ResourceShapeDirectFile && intent.Transport == ResourceTransportSymlink:
			line = fmt.Sprintf("shared target: symlink file %s -> %s", config.DisplayPath(dest), config.DisplayPath(src))
		case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
			line = fmt.Sprintf("shared target: write %s <- %s (%s)", config.DisplayPath(dest), config.DisplayPath(src), intent.Materializer)
		default:
			line = fmt.Sprintf("shared target: preview %s/%s %s", intent.Shape, intent.Transport, config.DisplayPath(dest))
		}
		if n := len(res.Duplicates); n > 0 {
			line += fmt.Sprintf(" (%d duplicate intent(s) merged)", n)
		}
		lines = append(lines, line)
	}
	return lines
}

func ExecuteSharedSkillMirrorPlan(project, repoPath string, targetRoots ...string) error {
	intents, err := BuildSharedSkillMirrorIntents(project, targetRoots...)
	if err != nil {
		return err
	}
	plan, err := BuildResourcePlan(intents)
	if err != nil {
		return err
	}
	return plan.Execute(repoPath, config.AgentsHome())
}
