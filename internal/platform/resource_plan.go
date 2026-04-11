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

func prepareIntentTargetForReplacement(target string, intent ResourceIntent) error {
	info, err := os.Lstat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		if intent.ReplacePolicy == ResourceReplaceNever {
			return fmt.Errorf("refusing to replace existing file %s", target)
		}
		return os.Remove(target)
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
		strings.HasPrefix(normalized, ".claude/agents/")
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

// CollectAndExecuteSharedTargetPlan aggregates SharedTargetIntents from all
// provided platforms, builds a single ResourcePlan that deduplicates compatible
// shared-target intents and fails fast on incompatible ones, then executes it.
// This is the command-layer entry point for centralized shared-target writes.
func CollectAndExecuteSharedTargetPlan(project, repoPath string, platforms []Platform) error {
	var all []ResourceIntent
	for _, p := range platforms {
		intents, err := p.SharedTargetIntents(project)
		if err != nil {
			return fmt.Errorf("%s shared intents: %w", p.ID(), err)
		}
		all = append(all, intents...)
	}
	if len(all) == 0 {
		return nil
	}
	plan, err := BuildResourcePlan(all)
	if err != nil {
		return err
	}
	return plan.Execute(repoPath, config.AgentsHome())
}

// DryRunSharedTargetPlanLines describes what CollectAndExecuteSharedTargetPlan would
// write (merged shared-target rows, duplicate-intent counts) without touching the filesystem.
func DryRunSharedTargetPlanLines(project, repoPath string, platforms []Platform) ([]string, error) {
	var all []ResourceIntent
	for _, p := range platforms {
		intents, err := p.SharedTargetIntents(project)
		if err != nil {
			return nil, fmt.Errorf("%s shared intents: %w", p.ID(), err)
		}
		all = append(all, intents...)
	}
	if len(all) == 0 {
		return []string{"shared targets: (none)"}, nil
	}
	plan, err := BuildResourcePlan(all)
	if err != nil {
		return nil, err
	}
	agentsHome := config.AgentsHome()
	var lines []string
	for _, res := range plan.Resources {
		intent := res.Intent
		src := intent.SourceRef.CanonicalPath(agentsHome)
		if src == "" {
			src = "(unknown source)"
		}
		dest := resolveIntentTargetPath(intent.TargetPath, repoPath)
		line := fmt.Sprintf("shared target: symlink %s -> %s", config.DisplayPath(dest), config.DisplayPath(src))
		if n := len(res.Duplicates); n > 0 {
			line += fmt.Sprintf(" (%d duplicate intent(s) merged)", n)
		}
		lines = append(lines, line)
	}
	return lines, nil
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
