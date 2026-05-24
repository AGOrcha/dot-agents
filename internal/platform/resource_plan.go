package platform

import (
	"errors"
	"fmt"
	"io/fs"
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

const (
	agentManifestName          = "AGENT.md"
	codexAgentTomlMaterializer = "codex-agent-toml"
	emptySourcePathErr         = "empty source path"
	skillManifestName          = "SKILL.md"
)

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
		src, err := canonicalIntentSourcePath(intent, agentsHome)
		if err != nil {
			return err
		}
		target := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return ensureDirSymlinkIntent(src, target, intent)
	case intent.Shape == ResourceShapeDirectFile && intent.Transport == ResourceTransportSymlink:
		src, err := canonicalIntentSourcePath(intent, agentsHome)
		if err != nil {
			return err
		}
		target := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return ensureFileSymlinkIntent(src, target, intent)
	case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
		return executeRenderSingleWrite(intent, repoPath, agentsHome)
	default:
		return fmt.Errorf("unsupported intent shape/transport %s/%s", intent.Shape, intent.Transport)
	}
}

func canonicalIntentSourcePath(intent ResourceIntent, agentsHome string) (string, error) {
	src := intent.SourceRef.CanonicalPath(agentsHome)
	if src == "" {
		return "", fmt.Errorf(emptySourcePathErr)
	}
	return src, nil
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
	return ensureDirSymlinkIntent(src, target, intent)
}

func executeRenderSingleWrite(intent ResourceIntent, repoPath, agentsHome string) error {
	switch intent.Materializer {
	case codexAgentTomlMaterializer:
		src, err := canonicalIntentSourcePath(intent, agentsHome)
		if err != nil {
			return err
		}
		dst := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return writeCodexAgentTomlFile(stdPlatformIO{}, dst, src)
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
			// This policy authorizes replacing only a proven imported/managed
			// DIRECTORY (handled in the directory branch below). A regular
			// file at an allowlisted DirectFile target (OpenCode
			// .opencode/agent/*.md, Copilot .github/agents/*.agent.md) must
			// NOT be pre-removed here: doing so bypassed the ownership
			// contract and silently deleted a user-authored file. Leave it in
			// place and let links.Symlink apply the contract — a managed
			// symlink is re-pointed idempotently, a genuine user file is
			// refused (ErrUnmanagedTarget) and preserved.
			return nil
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
		rootIntents, err := buildSharedSkillMirrorIntentsForRoot(project, root)
		if err != nil {
			return nil, err
		}
		intents = append(intents, rootIntents...)
	}
	return intents, nil
}

// sharedMirrorIntentSpec parameterizes the per-bucket symlink-mirror
// intent shape used by buildShared{Skill,Plugin,Agent}MirrorIntentsForRoot.
type sharedMirrorIntentSpec struct {
	Bucket       string             // "skills" | "plugins" | "agents"
	ManifestName string             // marker file inside each entry
	SourceKind   ResourceSourceKind // CanonicalDir | CanonicalBundle
	Origin       string             // SourceRef.Origin
	Materializer string             // ResourceIntent.Materializer
}

// buildSharedMirrorIntentsForRoot returns ResourceIntents for every
// bucket entry under ~/.agents/<spec.Bucket>/<project>/ that owns
// spec.ManifestName, projecting them into targetRoot via symlink.
// All three per-bucket helpers (skill / plugin / agent) delegate
// here.
//
// A missing canonical bucket dir (ENOENT) is treated as an empty
// resource set — projects without any skills/plugins/agents yet are
// legitimate and should yield no intents, not a hard failure. Other
// errors (permission denied, IO) propagate so callers can surface
// them instead of silently producing an empty plan.
func buildSharedMirrorIntentsForRoot(project, targetRoot string, spec sharedMirrorIntentSpec) ([]ResourceIntent, error) {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, spec.Bucket, project, spec.ManifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing canonical %s for project %q under %s: %w", spec.Bucket, project, targetRoot, err)
	}

	intents := make([]ResourceIntent, 0, len(entries))
	for _, entry := range entries {
		targetPath := filepath.Join(targetRoot, entry.Name)
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("%s.%s.%s.%s", spec.Bucket, project, entry.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      spec.Bucket,
			LogicalName: entry.Name,
			TargetPath:  targetPath,
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        project,
				Bucket:       spec.Bucket,
				RelativePath: entry.Name,
				Kind:         spec.SourceKind,
				Origin:       spec.Origin,
			},
			Shape:         ResourceShapeDirectDir,
			Transport:     ResourceTransportSymlink,
			Materializer:  spec.Materializer,
			ReplacePolicy: ResourceReplaceAllowlistedImportedDirOnly,
			PrunePolicy:   ResourcePruneTarget,
			MarkerFiles:   []string{spec.ManifestName},
		})
	}
	return intents, nil
}

func buildSharedSkillMirrorIntentsForRoot(project, targetRoot string) ([]ResourceIntent, error) {
	return buildSharedMirrorIntentsForRoot(project, targetRoot, sharedMirrorIntentSpec{
		Bucket:       "skills",
		ManifestName: skillManifestName,
		SourceKind:   ResourceSourceCanonicalDir,
		Origin:       "shared-skill-mirror",
		Materializer: "shared-skill-dir-symlink",
	})
}

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
		rootIntents, err := buildSharedPluginBundleIntentsForRoot(project, root)
		if err != nil {
			return nil, err
		}
		intents = append(intents, rootIntents...)
	}
	return intents, nil
}

func buildSharedPluginBundleIntentsForRoot(project, targetRoot string) ([]ResourceIntent, error) {
	return buildSharedMirrorIntentsForRoot(project, targetRoot, sharedMirrorIntentSpec{
		Bucket:       "plugins",
		ManifestName: PluginManifestName,
		SourceKind:   ResourceSourceCanonicalBundle,
		Origin:       "shared-plugin-bundle",
		Materializer: "shared-plugin-dir-symlink",
	})
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
		rootIntents, err := buildSharedAgentMirrorIntentsForRoot(project, root)
		if err != nil {
			return nil, err
		}
		intents = append(intents, rootIntents...)
	}
	return intents, nil
}

// BuildSharedAgentFileSymlinkIntents builds symlink intents from each canonical
// AGENT.md file to a repo-local file path (OpenCode `.md`, Copilot `.agent.md`).
//
// A missing canonical agents bucket (ENOENT) is treated as an empty resource
// set — projects without any agents yet are legitimate and should yield no
// intents, not a hard failure. Other errors (permission denied, IO) propagate
// so callers can surface them instead of silently producing an empty plan.
func BuildSharedAgentFileSymlinkIntents(project, targetRoot, destFileSuffix string) ([]ResourceIntent, error) {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, agentManifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing canonical agents for project %q under %s: %w", project, targetRoot, err)
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
				RelativePath: filepath.Join(entry.Name, agentManifestName),
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
//
// A missing canonical agents bucket (ENOENT) is treated as an empty resource
// set — projects without any agents yet are legitimate and should yield no
// intents, not a hard failure. Other errors (permission denied, IO) propagate
// so callers can surface them instead of silently producing an empty plan.
func BuildSharedCodexAgentTomlIntents(project string) ([]ResourceIntent, error) {
	agentsHome := config.AgentsHome()
	entries, err := listScopedResourceDirs(agentsHome, "agents", project, agentManifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing canonical agents for project %q (codex toml intents): %w", project, err)
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
				RelativePath: filepath.Join(entry.Name, agentManifestName),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       "shared-codex-agent-toml",
			},
			Shape:         ResourceShapeRenderSingle,
			Transport:     ResourceTransportWrite,
			Materializer:  codexAgentTomlMaterializer,
			ReplacePolicy: ResourceReplaceIfManaged,
			PrunePolicy:   ResourcePruneNone,
		})
	}
	return intents, nil
}

func buildSharedAgentMirrorIntentsForRoot(project, targetRoot string) ([]ResourceIntent, error) {
	return buildSharedMirrorIntentsForRoot(project, targetRoot, sharedMirrorIntentSpec{
		Bucket:       "agents",
		ManifestName: agentManifestName,
		SourceKind:   ResourceSourceCanonicalDir,
		Origin:       "shared-agent-mirror",
		Materializer: "shared-agent-dir-symlink",
	})
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
// Per-resource removal failures are aggregated (errors.Join) rather than
// short-circuiting so that one stuck target cannot hide the removal status of
// the rest, and so the caller (da remove) never reports a clean unlink while a
// managed output is still live on disk.
func (p ResourcePlan) RemoveSharedTargets(repoPath, agentsHome string) error {
	var errs []error
	for _, res := range p.Resources {
		if err := removeManagedIntentTarget(res.Intent, repoPath, agentsHome); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", res.Intent.IntentID, err))
		}
	}
	return errors.Join(errs...)
}

// removeDirectSymlinkTarget removes the managed output for a DirectDir or
// DirectFile + symlink-transport intent. A missing entry is a successful
// no-op; any other failure means a managed link is still live and MUST be
// surfaced (aggregated, not short-circuited). DirectFile intents also
// materialize as hard links on Windows (links.createLink has no reparse
// point for files), so the symlink/junction removal is a no-op there and
// the canonical-source hard link must be removed too or the managed file
// is orphaned while remove reports success. Dir intents are always real
// symlinks/junctions, so the hard-link path only applies to the file shape.
func removeDirectSymlinkTarget(intent ResourceIntent, target, agentsHome string) error {
	var errs []error
	if err := links.RemoveIfSymlinkUnder(target, agentsHome); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("remove managed symlink %s: %w", target, err))
	}
	if intent.Shape == ResourceShapeDirectFile {
		src, err := canonicalIntentSourcePath(intent, agentsHome)
		if err != nil {
			errs = append(errs, err)
		} else if _, err := links.RemoveIfHardlinkedToAny(target, []string{src}); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove managed hard link %s: %w", target, err))
		}
	}
	return errors.Join(errs...)
}

func removeManagedIntentTarget(intent ResourceIntent, repoPath, agentsHome string) error {
	target := resolveIntentTargetPath(intent.TargetPath, repoPath)
	switch {
	case (intent.Shape == ResourceShapeDirectDir || intent.Shape == ResourceShapeDirectFile) && intent.Transport == ResourceTransportSymlink:
		return removeDirectSymlinkTarget(intent, target, agentsHome)
	case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
		switch intent.Materializer {
		case codexAgentTomlMaterializer:
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove rendered file %s: %w", target, err)
			}
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
		// Normalize to forward slashes so dry-run output is byte-identical
		// across OSes (Windows filepath.Join yields backslashes, which would
		// otherwise make this preview non-reproducible and break exact-line
		// assertions / cross-platform dedup display).
		srcDisp := filepath.ToSlash(config.DisplayPath(src))
		destDisp := filepath.ToSlash(config.DisplayPath(dest))
		var line string
		switch {
		case intent.Shape == ResourceShapeDirectDir && intent.Transport == ResourceTransportSymlink:
			line = fmt.Sprintf("shared target: symlink %s -> %s", destDisp, srcDisp)
		case intent.Shape == ResourceShapeDirectFile && intent.Transport == ResourceTransportSymlink:
			line = fmt.Sprintf("shared target: symlink file %s -> %s", destDisp, srcDisp)
		case intent.Shape == ResourceShapeRenderSingle && intent.Transport == ResourceTransportWrite:
			line = fmt.Sprintf("shared target: write %s <- %s (%s)", destDisp, srcDisp, intent.Materializer)
		default:
			line = fmt.Sprintf("shared target: preview %s/%s %s", intent.Shape, intent.Transport, destDisp)
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
