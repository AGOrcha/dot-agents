package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/links"
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
	sha256DigestPrefix         = "sha256:"
	sharedTargetsNoneLine      = "shared targets: (none)"
	skillManifestName          = "SKILL.md"
)

// Filesystem seams for the prune/projection paths. They default to the real
// operations and exist so the error-propagation branches can be forced
// deterministically on every OS in tests, without relying on OS-specific tricks
// (a file-where-a-dir-is-expected ReadDir failure, or chmod-denied unlinks) that
// are no-ops on Windows. Production behavior is identical to calling the wrapped
// functions directly.
var (
	osReadDir            = os.ReadDir
	removeIfSymlinkUnder = links.RemoveIfSymlinkUnder
	executeResourcePlan  = func(p ResourcePlan, repoPath, agentsHome string) error {
		return p.Execute(repoPath, agentsHome)
	}
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
	// H13/H17: a CAS-direct intent links a project's repo output straight to
	// the immutable store digest path via the atomic symlink swap — never the
	// shared RemoveAll-after-check primitive. Routed here (not via a new
	// ReplacePolicy, which lives out of this file's scope) by the intent's
	// casDirectOrigin marker.
	if isCASDirectIntent(intent) {
		src, err := canonicalIntentSourcePath(intent, agentsHome)
		if err != nil {
			return err
		}
		target := resolveIntentTargetPath(intent.TargetPath, repoPath)
		return atomicManagedSymlinkSwap(src, target)
	}
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
		strings.HasPrefix(normalized, ".github/agents/") ||
		strings.HasPrefix(normalized, ".antigravity/skills/") ||
		strings.HasPrefix(normalized, ".antigravity/agents/")
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

// buildSharedMirrorIntentsForRoot returns ResourceIntents for every bucket
// entry under ~/.agents/<spec.Bucket>/<project>/ that owns spec.ManifestName
// (LOCAL-authored resources only). All three per-bucket dir-mirror helpers
// (skill / plugin / agent) delegate here.
//
// It deliberately does NOT know about packages/CAS units: caller-resolved
// packages units are projected by ProjectResolvedUnits as INVOCATION-LOCAL
// data (never a package global, never a filesystem scan), so this builder —
// which runs inside every platform's SharedTargetIntents, including plain
// `da refresh`/`install` with no caller units — can never observe another
// projection's units.
//
// A missing canonical bucket dir (ENOENT) is treated as an empty local
// resource set — projects without any skills/plugins/agents yet are
// legitimate and should yield no local intents, not a hard failure. Other
// errors (permission denied, IO) propagate so callers can surface them
// instead of silently producing an incomplete plan.
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

// listCanonicalAgentEntries lists the canonical agents-bucket entries for
// project (each per-entry directory that owns AGENT.md), centralizing the
// preamble shared by BuildSharedAgentFileSymlinkIntents and
// BuildSharedCodexAgentTomlIntents.
//
// A missing canonical agents bucket (ENOENT) is reported via ok=false with a
// nil error, so callers can return an empty intent set — projects without any
// agents yet are legitimate and must not be a hard failure. Any other error
// (permission denied, IO, bucket-is-a-file) is wrapped with the caller's
// errContext fragment and surfaced so the fault is not silently swallowed.
// errContext is interpolated as: listing canonical agents for project %q
// <errContext>: %w (e.g. "under .opencode" or "(codex toml intents)").
func listCanonicalAgentEntries(project, errContext string) (entries []resourceDir, ok bool, err error) {
	agentsHome := config.AgentsHome()
	entries, err = listScopedResourceDirs(agentsHome, "agents", project, agentManifestName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("listing canonical agents for project %q %s: %w", project, errContext, err)
	}
	return entries, true, nil
}

// BuildSharedAgentFileSymlinkIntents builds symlink intents from each canonical
// AGENT.md file to a repo-local file path (OpenCode `.md`, Copilot `.agent.md`).
//
// A missing canonical agents bucket (ENOENT) is treated as an empty resource
// set — projects without any agents yet are legitimate and should yield no
// intents, not a hard failure. Other errors (permission denied, IO) propagate
// so callers can surface them instead of silently producing an empty plan.
func BuildSharedAgentFileSymlinkIntents(project, targetRoot, destFileSuffix string) ([]ResourceIntent, error) {
	entries, ok, err := listCanonicalAgentEntries(project, fmt.Sprintf("under %s", targetRoot))
	if err != nil {
		return nil, err
	}
	if !ok {
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
	entries, ok, err := listCanonicalAgentEntries(project, "(codex toml intents)")
	if err != nil {
		return nil, err
	}
	if !ok {
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

// RunSharedTargetProjectionExact is the EXACT/PRUNE command-layer entry point
// (config-v2-coherence §7A.5 / D10 "outputs half"): it projects the resolved
// asset-store union AND prunes managed outputs that are no longer in the
// resolved set, so the repo tree converges to exactly what the plan declares.
// It is the projection refresh/install drive by default; pass exact=false
// (`--inexact`) to keep the additive RunSharedTargetProjection behavior (write
// the wanted set, leave stale managed outputs in place).
//
// Dry-run returns the same preview lines as RunSharedTargetProjection plus a
// "prune" line for every managed output the apply path would delete, so the
// preview is a faithful diff of the exact projection. Apply executes the plan
// then prunes; prune is best-effort relative to the write — a prune failure is
// returned so the caller can withhold a clean-success stamp.
//
// Callers must set config.SetWindowsMirrorContext(repoPath) before calling when
// the repo needs Windows-specific path behavior for intent resolution.
func RunSharedTargetProjectionExact(project, repoPath string, platforms []Platform, dryRun, exact bool) ([]string, error) {
	if !exact {
		return RunSharedTargetProjection(project, repoPath, platforms, dryRun)
	}
	plan, err := BuildSharedTargetPlan(project, platforms)
	if err != nil {
		return nil, err
	}
	if dryRun {
		lines := dryRunExactProjectionLines(plan, platforms, repoPath)
		if len(lines) == 0 {
			return []string{sharedTargetsNoneLine}, nil
		}
		return lines, nil
	}
	if len(plan.Resources) > 0 {
		if err := executeResourcePlan(plan, repoPath, config.AgentsHome()); err != nil {
			return nil, err
		}
	}
	if _, err := plan.PruneStaleSharedTargets(repoPath, config.AgentsHome()); err != nil {
		return nil, err
	}
	// Managed-RENDER prune (codex .toml): the generic PruneStaleSharedTargets
	// scan reaps only managed symlinks, so a stale codex render — a plain file —
	// is reaped here, driven by the SAME plan wanted-set. This is what lets the
	// exact projection (not a separate CreateLinks pass) own codex toml pruning
	// uniformly for local-authored AND sourced agents (defect 3).
	wanted, _ := plan.prunableTargets(repoPath)
	if _, err := pruneManagedRenders(platforms, repoPath, wanted); err != nil {
		return nil, err
	}
	return nil, nil
}

// dryRunExactProjectionLines is the dry-run preview for the exact projection:
// the additive write lines (formatSharedTargetPlanForDryRun) followed by a
// "prune managed" line for every managed output the exact prune would delete —
// both the managed SYMLINKS PruneStaleSharedTargets scans AND the managed
// RENDERS (codex .toml) staleManagedRenderTargets scans (defect 3: exact +
// dry-run are uniform across all file-shaped platforms). It never mutates the
// filesystem — both scans only read.
func dryRunExactProjectionLines(plan ResourcePlan, platforms []Platform, repoPath string) []string {
	var lines []string
	if len(plan.Resources) > 0 {
		lines = append(lines, formatSharedTargetPlanForDryRun(plan, repoPath)...)
	}
	for _, target := range plan.staleManagedTargets(repoPath, config.AgentsHome()) {
		lines = append(lines, fmt.Sprintf("shared target: prune managed %s", filepath.ToSlash(config.DisplayPath(target))))
	}
	for _, target := range staleManagedRenderTargets(plan, platforms, repoPath) {
		lines = append(lines, fmt.Sprintf("shared target: prune managed %s", filepath.ToSlash(config.DisplayPath(target))))
	}
	return lines
}

// ManagedRenderProjector is implemented by a platform whose shared agents
// surface is a MANAGED RENDERED FILE (codex `.toml`) rather than a symlink.
// Such a file is a plain regular file — never a managed symlink — so the
// generic exact/prune scan (pruneManagedEntries, which only reaps symlinks via
// links.IsManagedLinkUnder) cannot see it. These methods let the exact
// projection reap a stale managed render uniformly with the symlink shapes
// (defect 3), driven by the SAME wanted-target set the plan already carries.
type ManagedRenderProjector interface {
	// ManagedRenderDir is the repo-relative directory the platform renders its
	// managed files into (e.g. .codex/agents). Forced into the exact scan even
	// on a one-to-zero pass, so a render dropped to zero is still pruned.
	ManagedRenderDir() string
	// IsManagedRender reports whether path is one of THIS platform's managed
	// rendered files — identity-verified provenance, never a bare name/suffix
	// match — so a user-authored file in the same directory is never pruned.
	IsManagedRender(path string) (bool, error)
}

// pruneManagedRenders removes each ManagedRenderProjector platform's managed
// rendered files that the exact plan no longer wants. It is the render-shape
// analogue of pruneManagedEntries: ownership is proven per-file via
// IsManagedRender (never a blind name/extension match), so a user-authored
// file in the same directory is never deleted. wanted is the plan's absolute
// wanted-target set (from prunableTargets). Returns the pruned absolute paths
// and an aggregated error.
func pruneManagedRenders(platforms []Platform, repoPath string, wanted map[string]bool) ([]string, error) {
	var pruned []string
	var errs []error
	for _, plat := range platforms {
		rp, ok := plat.(ManagedRenderProjector)
		if !ok {
			continue
		}
		gotPruned, gotErrs := pruneManagedRendersForPlatform(rp, repoPath, wanted)
		pruned = append(pruned, gotPruned...)
		errs = append(errs, gotErrs...)
	}
	sort.Strings(pruned)
	return pruned, errors.Join(errs...)
}

// pruneManagedRendersForPlatform reaps one ManagedRenderProjector platform's
// stale managed renders under its ManagedRenderDir. A missing dir is an empty
// set (no error); any other listing failure is surfaced.
func pruneManagedRendersForPlatform(rp ManagedRenderProjector, repoPath string, wanted map[string]bool) (pruned []string, errs []error) {
	dir := resolveIntentTargetPath(rp.ManagedRenderDir(), repoPath)
	entries, err := osReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("listing managed render dir %s: %w", dir, err))
		}
		return nil, errs
	}
	for _, e := range entries {
		candidate := filepath.Join(dir, e.Name())
		if wanted[candidate] {
			continue
		}
		removed, err := pruneManagedRenderEntry(rp, candidate)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if removed {
			pruned = append(pruned, candidate)
		}
	}
	return pruned, errs
}

// pruneManagedRenderEntry removes a single candidate iff it is provably one of
// rp's managed renders. A user/foreign file (isRender false) is left untouched
// and reported as not removed; a missing entry counts as removed.
func pruneManagedRenderEntry(rp ManagedRenderProjector, candidate string) (bool, error) {
	isRender, provErr := rp.IsManagedRender(candidate)
	if provErr != nil {
		return false, fmt.Errorf("managed render provenance %s: %w", candidate, provErr)
	}
	if !isRender {
		return false, nil // user/foreign file — never touch
	}
	if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("prune managed render %s: %w", candidate, err)
	}
	return true, nil
}

// staleManagedRenderTargets is the read-only scan pruneManagedRenders and the
// dry-run preview share: it returns the managed renders the exact prune would
// delete, without removing anything.
func staleManagedRenderTargets(plan ResourcePlan, platforms []Platform, repoPath string) []string {
	wanted, _ := plan.prunableTargets(repoPath)
	var stale []string
	for _, p := range platforms {
		rp, ok := p.(ManagedRenderProjector)
		if !ok {
			continue
		}
		dir := resolveIntentTargetPath(rp.ManagedRenderDir(), repoPath)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			candidate := filepath.Join(dir, e.Name())
			if wanted[candidate] {
				continue
			}
			if isRender, err := rp.IsManagedRender(candidate); err == nil && isRender {
				stale = append(stale, candidate)
			}
		}
	}
	sort.Strings(stale)
	return stale
}

// PruneStaleSharedTargets deletes managed outputs that are no longer in the
// resolved plan (the EXACT/PRUNE projection, config-v2-coherence §7A.5). It
// scans only the parent directories that own at least one ResourcePruneTarget
// intent — the directories da actively projects into — and removes any entry
// there that (a) is not a wanted plan target and (b) is a managed link under
// agentsHome. User-authored files and links pointing outside agentsHome are
// never touched, so the prune cannot delete content da does not own.
//
// Returns the list of pruned absolute paths and an aggregated error: a single
// stuck removal is reported (errors.Join) rather than short-circuiting, so one
// failure cannot hide the prune status of the rest and the caller never reports
// a converged tree while a stale managed output is still live.
func (p ResourcePlan) PruneStaleSharedTargets(repoPath, agentsHome string) ([]string, error) {
	return p.pruneStaleSharedTargetsExtraDirs(repoPath, agentsHome, nil)
}

// pruneStaleSharedTargetsExtraDirs is PruneStaleSharedTargets with an
// additional set of directories to scan beyond those a wanted ResourcePruneTarget
// intent contributes. It exists for the one-to-zero case (H17/fix-3): when a
// caller reduces a bucket to zero units, the plan carries no intent pointing at
// that bucket's directory, so the default scan would never visit it and the
// last stale managed link would survive forever. The caller (ProjectResolvedUnits)
// passes the deterministic per-platform dir-mirror roots so the directory is
// scanned regardless of the desired set. Prune semantics are unchanged: only a
// managed link under agentsHome that is NOT a wanted target is removed, so a
// plain user file or a link pointing outside agentsHome is never touched.
func (p ResourcePlan) pruneStaleSharedTargetsExtraDirs(repoPath, agentsHome string, extraDirs []string) ([]string, error) {
	wanted, dirs := p.prunableTargets(repoPath)
	dirs = mergeSortedDirs(dirs, extraDirs)
	var pruned []string
	var errs []error
	for _, dir := range dirs {
		entries, err := osReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("listing managed dir %s: %w", dir, err))
			}
			continue
		}
		dirPruned, dirErrs := pruneManagedEntries(dir, entries, wanted, agentsHome)
		pruned = append(pruned, dirPruned...)
		errs = append(errs, dirErrs...)
	}
	sort.Strings(pruned)
	return pruned, errors.Join(errs...)
}

// mergeSortedDirs unions two directory lists, de-duplicated and sorted.
func mergeSortedDirs(a, b []string) []string {
	set := map[string]bool{}
	for _, d := range a {
		set[d] = true
	}
	for _, d := range b {
		set[d] = true
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// pruneManagedEntries removes the managed outputs in a single scanned directory
// that are no longer wanted by the plan. It is the per-directory body lifted out
// of PruneStaleSharedTargets so the prune loop carries no nested entry logic:
// entries that are wanted, or not managed links under agentsHome, are skipped;
// the rest are removed (best-effort, accumulating errors).
func pruneManagedEntries(dir string, entries []fs.DirEntry, wanted map[string]bool, agentsHome string) (pruned []string, errs []error) {
	for _, entry := range entries {
		candidate := filepath.Join(dir, entry.Name())
		if wanted[candidate] || !links.IsManagedLinkUnder(candidate, agentsHome) {
			continue
		}
		if err := removeIfSymlinkUnder(candidate, agentsHome); err != nil {
			errs = append(errs, fmt.Errorf("prune managed target %s: %w", candidate, err))
			continue
		}
		pruned = append(pruned, candidate)
	}
	return pruned, errs
}

// staleManagedTargets is the read-only scan PruneStaleSharedTargets and the
// dry-run preview share: it returns the managed outputs the prune would delete,
// without removing anything.
func (p ResourcePlan) staleManagedTargets(repoPath, agentsHome string) []string {
	wanted, dirs := p.prunableTargets(repoPath)
	var stale []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			candidate := filepath.Join(dir, entry.Name())
			if wanted[candidate] {
				continue
			}
			if links.IsManagedLinkUnder(candidate, agentsHome) {
				stale = append(stale, candidate)
			}
		}
	}
	sort.Strings(stale)
	return stale
}

// prunableTargets returns (wanted, dirs): the set of absolute target paths the
// plan declares, and the sorted, de-duplicated set of parent directories that
// own at least one ResourcePruneTarget intent (the only directories eligible
// for sibling-pruning). Intents with ResourcePruneNone never contribute a
// scan directory — their siblings are out of scope for the exact projection.
func (p ResourcePlan) prunableTargets(repoPath string) (wanted map[string]bool, dirs []string) {
	wanted = map[string]bool{}
	dirSet := map[string]bool{}
	for _, res := range p.Resources {
		target := resolveIntentTargetPath(res.Intent.TargetPath, repoPath)
		wanted[target] = true
		if res.Intent.PrunePolicy == ResourcePruneTarget {
			dirSet[filepath.Dir(target)] = true
		}
	}
	dirs = make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return wanted, dirs
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
		return []string{sharedTargetsNoneLine}, nil
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

// ResolvedUnit is one packages artifact a PROJECT resolved from its lock —
// the caller-supplied projection input (H13). It is the boundary between t2
// (mechanism) and t3 (driver): t3 reads the project's lock, materializes each
// ref, and hands the resulting units to ProjectResolvedUnits. t2 NEVER
// discovers this set by scanning the global store; authority flows in, not
// out.
type ResolvedUnit struct {
	// Family is the resource bucket ("skills" | "agents" | "plugins").
	Family string
	// Name is the resource directory name (one canonical segment).
	Name string
	// SourceID is the provenance source id (one canonical segment); used for a
	// stable, source-namespaced intent id so two sources shipping a same-named
	// resource never collide.
	SourceID string
	// Digest is the "sha256:<hex>" content digest THIS project resolved.
	Digest string
	// CASPath is the absolute immutable store path for Digest
	// (config.ArtifactStorePath). Recomputed and asserted by
	// ValidateResolvedUnit, never trusted as passed.
	CASPath string
	// ContentDigest is the OPTIONAL H16 content-integrity anchor
	// (config.BundleContentDigest / the git-tracked "artifact-content" lock
	// value) the caller resolved this unit against. When non-empty,
	// ProjectResolvedUnits re-verifies it — via config.VerifyStoreContentDigest
	// — immediately before the unit's CAS content is actually linked into the
	// project (t3b, closing the verify→link TOCTOU: an earlier batch verify a
	// driver ran before calling ProjectResolvedUnits does not, by itself, catch
	// a store mutation that lands during this call). Left empty, a unit gets
	// no invocation-time re-check — identical to pre-t3b behavior — so an
	// older caller that has not yet threaded its lock's content digest through
	// keeps working unchanged; the anchor is defense-in-depth on top of H13's
	// existing identity/path binding, not a new hard requirement on every
	// caller.
	ContentDigest string
}

// MaterializeArtifact installs an H1-normalized bundle into the H2 content-
// addressed store and returns the immutable CAS path plus the resolved
// digest. Under the corrected per-project model (H13) it does NOT project:
// there is no shared mutable alias, and the projection target is a
// per-PROJECT decision driven by that project's resolved lock
// (ProjectResolvedUnits), not a global side effect of materialize. Identity
// components are validated (H15) and the source id is checked against
// reserved local scopes (H3) before any store write; H14 (CAS gitignored
// before first byte) and H16 (verify-on-hit) are enforced inside
// config.MaterializeToStore.
//
// localScopes are the local scope names the source id must not collide with
// (H3) — typically the consuming project's own scope name; "global" is
// always rejected.
func MaterializeArtifact(agentsHome, family, sourceID, name string, bundle config.Bundle, localScopes ...string) (casPath, digest string, err error) {
	if err := ValidateResolvedUnitIdentity(family, sourceID, name, localScopes...); err != nil {
		return "", "", err
	}
	storePath, digest, _, err := config.MaterializeToStore(agentsHome, family, bundle)
	if err != nil {
		return "", "", err
	}
	return storePath, digest, nil
}

// ValidateResolvedUnit checks a caller-supplied resolved unit before it is
// trusted as a projection input: identity-component containment (H15), a
// well-formed digest, and — critically — that CASPath is EXACTLY the store
// path the unit's (family, digest) names under agentsHome (never trusted as
// passed, H13/H16). A unit whose CASPath does not equal
// config.ArtifactStorePath(agentsHome, family, digest) is rejected, so a
// caller cannot smuggle a link target outside the immutable store.
func ValidateResolvedUnit(agentsHome string, u ResolvedUnit, localScopes ...string) error {
	if err := ValidateResolvedUnitIdentity(u.Family, u.SourceID, u.Name, localScopes...); err != nil {
		return err
	}
	if !strings.HasPrefix(u.Digest, sha256DigestPrefix) || len(u.Digest) != len(sha256DigestPrefix)+64 {
		return fmt.Errorf("platform: resolved unit %s/%s: malformed digest %q", u.Family, u.Name, u.Digest)
	}
	want := config.ArtifactStorePath(agentsHome, u.Family, u.Digest)
	if filepath.Clean(u.CASPath) != filepath.Clean(want) {
		return fmt.Errorf("platform: resolved unit %s/%s: CAS path %q is not the store path for its digest (%q)", u.Family, u.Name, u.CASPath, want)
	}
	if u.ContentDigest != "" && !strings.HasPrefix(u.ContentDigest, sha256DigestPrefix) {
		return fmt.Errorf("platform: resolved unit %s/%s: malformed content digest %q", u.Family, u.Name, u.ContentDigest)
	}
	return nil
}

// verifyResolvedUnitsAtUse is t3b's invocation-time re-check (package-
// artifact-install spec H7/H16 defense-in-depth): immediately before
// ProjectResolvedUnits performs any actual link/render mutation, every unit
// carrying a ContentDigest has its CAS entry re-walked and re-hashed —
// config.VerifyStoreContentDigest, the SAME offline H16 primitive
// verifyProjectionInputs uses — and compared against that anchor. A unit
// whose CAS content was mutated or replaced AFTER an earlier caller-side
// verify but BEFORE this call reaches the symlink swap is caught here,
// closing the specific gap the t3 round-2 review named: "verifyProjectionInputs
// re-hashes... but the symlink then references the live (dir-writable) CAS
// tree and projection never re-hashes."
//
// A unit with an empty ContentDigest is skipped (no anchor to check against —
// identical to pre-t3b behavior for a caller that has not threaded one
// through). A present-but-mismatched or vanished CAS entry fails the WHOLE
// call closed — the same all-or-nothing discipline ValidateResolvedUnit
// already applies to identity/path validation — so a caller never links a
// subset of units while silently skipping a tampered one.
//
// This does not, and cannot, close the window between this check and an
// agent HOST later reading the file through the symlink (the "use" step
// proper): that consumption happens in a wholly separate process this
// binary does not control and returns from before. Narrowing that residual
// further needs either holding a lock across consumption (not attempted:
// the host, not dot-agents, owns that read) or truly-immutable storage (a
// read-only bind mount), both deferred per the t3 review notes — this
// function closes the verify→link half of the gap, which is the half a
// materialize/projection caller can actually own.
func verifyResolvedUnitsAtUse(agentsHome string, units []ResolvedUnit) error {
	for _, u := range units {
		if u.ContentDigest == "" {
			continue
		}
		present, matches := config.VerifyStoreContentDigest(agentsHome, u.Family, u.Digest, u.ContentDigest)
		if !present {
			return fmt.Errorf("platform: resolved unit %s/%s: CAS entry vanished before use", u.Family, u.Name)
		}
		if !matches {
			return fmt.Errorf("platform: resolved unit %s/%s: CAS content failed integrity re-check at moment of use (possible post-verify store tamper)", u.Family, u.Name)
		}
	}
	return nil
}

// ProjectResolvedUnits projects a caller-supplied set of resolved packages
// units into project's repo, each linking DIRECTLY to its immutable CAS
// digest path (H13 per-project CAS-direct). The units are pure INVOCATION-
// LOCAL data — a function parameter threaded straight into the CAS-intent
// builder and never stored in any package-level variable — so two concurrent
// projections with different resolved-unit sets cannot cross-contaminate
// (the previous package-global projection context, which a concurrent plain
// RunSharedTargetProjectionExact could read, is gone).
//
// It reuses the exact/prune projection machinery (D4: same BuildResourcePlan
// / Execute / PruneStaleSharedTargets, not a parallel linker): local-authored
// intents (from the platforms) and the caller's CAS-direct intents are merged
// into ONE plan, executed, and pruned together. The per-platform dir-mirror
// roots are resolved deterministically (sharedDirMirrorRoots) so a bucket
// projects — and, critically, is PRUNE-SCANNED — even when the caller's set
// for it is empty (H17/one-to-zero: dropping a bucket to zero units still
// removes its stale link) and even when the project has no local-authored
// sibling.
//
// Every unit is validated (H15 identity + H13/H16 CAS-path binding) before
// projection; a single invalid unit fails the whole call closed. localScopes
// are the reserved local scope names each unit's source id must not equal
// (H3). CAS projection covers BOTH shapes: the DIR-MIRROR buckets (skills +
// agents-dir + plugins, via buildCASIntents) AND the FILE-shaped agents
// surfaces (Codex .toml render, OpenCode/Copilot agent-file symlink, via
// collectSourcedAgentFileIntents) — t2b closed the gap where a sourced agent
// reached only the dir-mirror platforms.
func ProjectResolvedUnits(project, repoPath string, units []ResolvedUnit, platforms []Platform, dryRun, exact bool, localScopes ...string) ([]string, error) {
	agentsHome := config.AgentsHome()
	if err := validateResolvedUnits(agentsHome, units, localScopes); err != nil {
		return nil, err
	}
	local, err := collectSharedTargetIntents(project, platforms)
	if err != nil {
		return nil, err
	}
	roots := unionDirMirrorRoots(platforms)
	cas := buildCASIntents(project, units, roots)
	fileIntents := collectSourcedAgentFileIntents(project, units, platforms)

	merged := make([]ResourceIntent, 0, len(local)+len(cas)+len(fileIntents))
	merged = append(merged, local...)
	merged = append(merged, cas...)
	merged = append(merged, fileIntents...)
	plan, err := BuildResourcePlan(merged)
	if err != nil {
		return nil, err
	}
	// Force the dir-mirror bucket roots — AND the file-shaped SYMLINK agents
	// roots (t2b: OpenCode/Copilot) — into the SYMLINK prune scan even when a
	// bucket has zero wanted targets this pass, so a bucket reduced to zero
	// units still has its stale managed link removed (one-to-zero prune).
	// Codex's rendered `.toml` is not a symlink, so it is reaped instead by the
	// managed-RENDER prune (pruneManagedRenders), which scans each codex
	// platform's ManagedRenderDir unconditionally for the same one-to-zero
	// guarantee (defect 3).
	pruneDirs := mergeSortedDirs(resolvedPruneDirs(roots, repoPath), resolvedFileShapedAgentPruneDirs(platforms, repoPath))

	if !exact {
		return projectResolvedUnitsInexact(plan, repoPath, agentsHome, units, dryRun)
	}
	return projectResolvedUnitsExact(plan, platforms, repoPath, agentsHome, units, pruneDirs, dryRun)
}

// validateResolvedUnits validates every caller-supplied unit before it is
// trusted as a projection input; a single invalid unit fails the whole call
// closed (H15 identity + H13/H16 CAS-path binding).
func validateResolvedUnits(agentsHome string, units []ResolvedUnit, localScopes []string) error {
	for _, u := range units {
		if err := ValidateResolvedUnit(agentsHome, u, localScopes...); err != nil {
			return err
		}
	}
	return nil
}

// projectResolvedUnitsInexact is the additive (non-exact) branch of
// ProjectResolvedUnits: dry-run preview or write-the-wanted-set with no prune.
func projectResolvedUnitsInexact(plan ResourcePlan, repoPath, agentsHome string, units []ResolvedUnit, dryRun bool) ([]string, error) {
	if dryRun {
		if len(plan.Resources) == 0 {
			return []string{sharedTargetsNoneLine}, nil
		}
		return formatSharedTargetPlanForDryRun(plan, repoPath), nil
	}
	if len(plan.Resources) > 0 {
		// t3b — invocation-time re-verify, immediately before the ONLY
		// mutating call in this branch, closing the verify→link gap down to
		// this call's own window.
		if err := verifyResolvedUnitsAtUse(agentsHome, units); err != nil {
			return nil, err
		}
		return nil, plan.Execute(repoPath, agentsHome)
	}
	return nil, nil
}

// projectResolvedUnitsExact is the EXACT/PRUNE branch of ProjectResolvedUnits:
// dry-run diff, or execute-then-prune (symlink prune + managed-render prune).
func projectResolvedUnitsExact(plan ResourcePlan, platforms []Platform, repoPath, agentsHome string, units []ResolvedUnit, pruneDirs []string, dryRun bool) ([]string, error) {
	if dryRun {
		lines := dryRunExactProjectionLines(plan, platforms, repoPath)
		if len(lines) == 0 {
			return []string{sharedTargetsNoneLine}, nil
		}
		return lines, nil
	}
	if len(plan.Resources) > 0 {
		// t3b — same invocation-time re-verify as the !exact branch, run
		// immediately before the exact-projection execute (the actual
		// link/render mutation), not at dry-run/plan-build time.
		if err := verifyResolvedUnitsAtUse(agentsHome, units); err != nil {
			return nil, err
		}
		if err := executeResourcePlan(plan, repoPath, agentsHome); err != nil {
			return nil, err
		}
	}
	if _, err := plan.pruneStaleSharedTargetsExtraDirs(repoPath, agentsHome, pruneDirs); err != nil {
		return nil, err
	}
	wanted, _ := plan.prunableTargets(repoPath)
	if _, err := pruneManagedRenders(platforms, repoPath, wanted); err != nil {
		return nil, err
	}
	return nil, nil
}

// DirMirrorRootsProvider is implemented by platforms with DIR-MIRROR shaped
// shared-target buckets (skills/agents/plugins dirs symlinked wholesale,
// rather than rendered or symlinked per entry). DirMirrorRoots returns a
// platform's bucket roots (bucket -> repo-relative target roots) INDEPENDENT
// of any local content, so CAS projection (buildCASIntents) and one-to-zero
// pruning work even when a project has no local-authored sibling and even
// when the caller's unit set for the bucket is empty. It mirrors exactly the
// roots each platform passes to BuildSharedSkillMirrorIntents /
// BuildSharedAgentMirrorIntents / BuildSharedPluginBundleIntents inside its
// SharedTargetIntents. Platforms whose agents bucket is FILE-shaped instead
// (Codex .toml render, OpenCode/Copilot agent-file symlink) omit "agents"
// here and implement SourcedAgentFileProjector instead (t2b).
//
// codex/opencode/copilot implement this in their own files; claude/cursor/
// antigravity implement it below — t2b's write scope does not include
// claude.go/cursor.go/antigravity.go, and Go permits a method on a
// same-package type to live in a different file, so this is the narrowest
// way to consolidate the former type-switch into first-class methods without
// touching files outside this task's scope.
type DirMirrorRootsProvider interface {
	DirMirrorRoots() map[string][]string
}

func (c *claude) DirMirrorRoots() map[string][]string {
	return map[string][]string{
		"skills": {filepath.Join(claudeDir, "skills"), filepath.Join(claudeAgentsBucketDir, "skills")},
		"agents": {filepath.Join(claudeDir, "agents")},
	}
}

func (c *cursor) DirMirrorRoots() map[string][]string {
	return map[string][]string{"agents": {filepath.Join(".claude", "agents")}}
}

func (a *antigravity) DirMirrorRoots() map[string][]string {
	return map[string][]string{
		"skills": {filepath.Join(antigravityDir, "skills")},
		"agents": {filepath.Join(antigravityDir, "agents")},
	}
}

// sharedDirMirrorRoots looks up a platform's DIR-MIRROR bucket roots via
// DirMirrorRootsProvider. A platform without any dir-mirror bucket (none
// currently) yields nil.
func sharedDirMirrorRoots(p Platform) map[string][]string {
	provider, ok := p.(DirMirrorRootsProvider)
	if !ok {
		return nil
	}
	return provider.DirMirrorRoots()
}

// unionDirMirrorRoots unions sharedDirMirrorRoots across platforms, de-duped
// and sorted, so a target root shared by two platforms (e.g. Claude and
// Cursor both use .claude/agents) is projected/scanned once.
func unionDirMirrorRoots(platforms []Platform) map[string][]string {
	set := map[string]map[string]bool{}
	for _, p := range platforms {
		for bucket, roots := range sharedDirMirrorRoots(p) {
			if set[bucket] == nil {
				set[bucket] = map[string]bool{}
			}
			for _, r := range roots {
				set[bucket][r] = true
			}
		}
	}
	out := make(map[string][]string, len(set))
	for bucket, roots := range set {
		list := make([]string, 0, len(roots))
		for r := range roots {
			list = append(list, r)
		}
		sort.Strings(list)
		out[bucket] = list
	}
	return out
}

// resolvedPruneDirs flattens roots (bucket → repo-relative target roots) into
// the absolute repo directories that must be prune-scanned regardless of
// whether the current plan has any wanted target there (one-to-zero prune).
func resolvedPruneDirs(roots map[string][]string, repoPath string) []string {
	var dirs []string
	for _, list := range roots {
		for _, r := range list {
			dirs = append(dirs, resolveIntentTargetPath(r, repoPath))
		}
	}
	sort.Strings(dirs)
	return dirs
}

// SourcedAgentFilePruneRootProvider is implemented by platforms whose
// SourcedAgentFileIntents materializes a SYMLINK (OpenCode/Copilot) rather
// than a render (Codex): it names the repo-relative directory that must be
// forced into the exact/prune scan even when the current call's unit set is
// empty (H17 one-to-zero, extended from the dir-mirror buckets to t2b's
// file-shaped CAS intents) — otherwise a unit projected on a prior call and
// then fully removed leaves an orphaned managed symlink behind forever,
// since no intent this call touches that directory at all. Codex does NOT
// implement this: its rendered `.toml` is never a symlink, so forcing its
// directory into the generic symlink prune scan would not help — that shape's
// removal is the managed-RENDER prune (pruneManagedRenders via
// ManagedRenderProjector), which scans the codex render dir separately.
type SourcedAgentFilePruneRootProvider interface {
	SourcedAgentFilePruneRoot() string
}

// resolvedFileShapedAgentPruneDirs is resolvedPruneDirs' counterpart for the
// file-shaped SYMLINK agents roots: the absolute repo directories every
// SourcedAgentFilePruneRoot-implementing platform in platforms must have
// prune-scanned regardless of this call's unit set.
func resolvedFileShapedAgentPruneDirs(platforms []Platform, repoPath string) []string {
	var dirs []string
	for _, p := range platforms {
		provider, ok := p.(SourcedAgentFilePruneRootProvider)
		if !ok {
			continue
		}
		if root := provider.SourcedAgentFilePruneRoot(); root != "" {
			dirs = append(dirs, resolveIntentTargetPath(root, repoPath))
		}
	}
	sort.Strings(dirs)
	return dirs
}

// casDirMirrorSpec maps a dir-mirror bucket to the marker + materializer a
// CAS-direct intent for it carries. Buckets absent here are NOT dir-mirror
// shapes in t2 (file-shaped agent projection is t2b) and are skipped.
var casDirMirrorSpec = map[string]struct{ marker, materializer string }{
	"skills":  {skillManifestName, "shared-skill-dir-symlink"},
	"agents":  {agentManifestName, "shared-agent-dir-symlink"},
	"plugins": {PluginManifestName, "shared-plugin-dir-symlink"},
}

// buildCASIntents builds the CAS-direct ResourceIntents for the caller's
// resolved units (H13) — pure invocation-local data. Each intent's SourceRef
// addresses the immutable store path "<agentsHome>/cache/artifacts/<family>/<hex>"
// directly (Scope "artifacts", Bucket "cache") — no shared mutable alias —
// one per (unit, platform dir-mirror root) for the unit's bucket. ReplacePolicy
// is ResourceReplaceNever and the casDirectOrigin marker routes the link step
// through the H17 atomic managed-symlink swap.
func buildCASIntents(project string, units []ResolvedUnit, roots map[string][]string) []ResourceIntent {
	var intents []ResourceIntent
	for _, u := range units {
		spec, ok := casDirMirrorSpec[u.Family]
		if !ok {
			continue // non-dir-mirror bucket (file-shaped agents: collectSourcedAgentFileIntents)
		}
		for _, targetRoot := range roots[u.Family] {
			intents = append(intents, ResourceIntent{
				IntentID:    fmt.Sprintf("%s.sourced.%s.%s.%s", u.Family, sanitizeIntentRoot(u.SourceID), u.Name, sanitizeIntentRoot(targetRoot)),
				Project:     project,
				Bucket:      u.Family,
				LogicalName: u.Name,
				TargetPath:  filepath.Join(targetRoot, u.Name),
				Ownership:   ResourceOwnershipSharedRepo,
				SourceRef: ResourceSourceRef{
					Scope:        "artifacts",
					Bucket:       "cache",
					RelativePath: filepath.Join(u.Family, config.StoreDigestDir(u.Digest)),
					Kind:         ResourceSourceCanonicalDir,
					Origin:       casDirectOrigin,
				},
				Shape:         ResourceShapeDirectDir,
				Transport:     ResourceTransportSymlink,
				Materializer:  spec.materializer,
				ReplacePolicy: ResourceReplaceNever,
				PrunePolicy:   ResourcePruneTarget,
				MarkerFiles:   []string{spec.marker},
			})
		}
	}
	return intents
}

// SourcedAgentFileProjector is implemented by platforms whose agents bucket
// is FILE-shaped (a rendered or symlinked per-entry file, e.g. Codex's
// rendered `.toml`, OpenCode/Copilot's symlinked `.md`/`.agent.md`) rather
// than dir-mirror-shaped (buildCASIntents / casDirMirrorSpec). t2b closes the
// gap where a sourced agent unit reached only the dir-mirror platforms:
// ProjectResolvedUnits drives SourcedAgentFileIntents from the SAME
// caller-supplied resolved-unit set (H13) as every other CAS intent. A
// platform without a file-shaped agents bucket simply omits this method.
type SourcedAgentFileProjector interface {
	// SourcedAgentFileIntents returns the CAS-direct ResourceIntents this
	// platform's file-shaped agents surface needs for units — implementations
	// filter to Family == "agents" themselves (units may carry other families).
	SourcedAgentFileIntents(project string, units []ResolvedUnit) []ResourceIntent
}

// collectSourcedAgentFileIntents aggregates SourcedAgentFileIntents across
// platforms that implement it, mirroring collectSharedTargetIntents' shape
// for the file-shaped CAS-direct half of projection.
func collectSourcedAgentFileIntents(project string, units []ResolvedUnit, platforms []Platform) []ResourceIntent {
	var intents []ResourceIntent
	for _, p := range platforms {
		if provider, ok := p.(SourcedAgentFileProjector); ok {
			intents = append(intents, provider.SourcedAgentFileIntents(project, units)...)
		}
	}
	return intents
}

// buildCASAgentFileIntents builds CAS-direct symlink ResourceIntents for one
// platform's FILE-shaped agents surface whose materialization is a plain
// symlink to the unit's canonical AGENT.md (OpenCode `.md`, Copilot
// `.agent.md`): each sourced "agents"-family unit gets one intent symlinking
// targetRoot/<name><destSuffix> straight to the unit's CAS AGENT.md file
// (H13). The casDirectOrigin marker routes execution through the identical
// H17 atomic managed-symlink swap the dir-mirror CAS intents use — only the
// shape differs (DirectFile vs DirectDir). A render-shaped surface (Codex
// .toml) cannot reuse this helper: casDirectOrigin intents are routed
// straight to the symlink swap in executeResourceIntent, before the
// shape/transport switch, so a render intent must NOT carry that marker (see
// codex.go's SourcedAgentFileIntents).
func buildCASAgentFileIntents(project string, units []ResolvedUnit, targetRoot, destSuffix, materializer string) []ResourceIntent {
	intents := make([]ResourceIntent, 0, len(units))
	for _, u := range units {
		if u.Family != "agents" {
			continue
		}
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("agents.sourced.file.%s.%s.%s", sanitizeIntentRoot(u.SourceID), u.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      "agents",
			LogicalName: u.Name,
			TargetPath:  filepath.Join(targetRoot, u.Name+destSuffix),
			Ownership:   ResourceOwnershipSharedRepo,
			SourceRef: ResourceSourceRef{
				Scope:        "artifacts",
				Bucket:       "cache",
				RelativePath: filepath.Join(u.Family, config.StoreDigestDir(u.Digest), agentManifestName),
				Kind:         ResourceSourceCanonicalFile,
				Origin:       casDirectOrigin,
			},
			Shape:         ResourceShapeDirectFile,
			Transport:     ResourceTransportSymlink,
			Materializer:  materializer,
			ReplacePolicy: ResourceReplaceNever,
			PrunePolicy:   ResourcePruneTarget,
		})
	}
	return intents
}

// casDirectOrigin marks a ResourceIntent whose source is a resolved
// packages unit's immutable CAS path (H13). executeResourceIntent routes
// such intents through the H17 atomic symlink swap instead of the shared
// RemoveAll-after-check link primitive.
const casDirectOrigin = "sourced-cas-direct"

// isCASDirectIntent reports whether intent projects a caller-resolved
// packages unit directly from the CAS (H13/H17 routing key).
func isCASDirectIntent(intent ResourceIntent) bool {
	return intent.SourceRef.Origin == casDirectOrigin
}

// errSwapUnsupported is returned by atomicSwapRename on an OS that has no
// atomic path-exchange primitive (see resource_plan_swap_other.go). It is the
// signal that a managed-link REPOINT cannot be done safely, so
// atomicManagedSymlinkSwap fails closed rather than reaching any
// unlink-by-pathname fallback.
var errSwapUnsupported = errors.New("atomic path-exchange unsupported on this OS")

// atomicManagedSymlinkSwap creates or repoints a CAS-direct managed symlink at
// target → src with NO unlink-by-pathname and NO RemoveAll fallback (H17 +
// defect 2). It NEVER deletes or overwrites anything that is not, at the
// moment of the mutating syscall, provably one of our own managed CAS links.
//
// Two disjoint cases, each closed against the verify→mutate TOCTOU:
//
//   - CREATE (target absent): os.Symlink(src, target) is itself an atomic
//     no-clobber create — the kernel fails with EEXIST if anything occupies
//     target, so a racer that lands a file there is never clobbered. When the
//     symlink cannot be made at all (e.g. Windows without the privilege) the
//     call fails closed.
//
//   - REPOINT (target already our managed CAS link, pointing at a different
//     digest): handled by atomicSwapReplaceManagedLink, which uses an OS-atomic
//     path EXCHANGE (Linux RENAME_EXCHANGE / Darwin RENAME_SWAP) and re-checks
//     provenance on the post-exchange occupant BEFORE unlinking it — so a racer
//     that replaced target with a user file between our read and the exchange
//     has its file safely restored (swap-back) and never deleted. On an OS with
//     no exchange primitive the repoint fails closed.
//
// The former design unlinked target by pathname after a plain Lstat identity
// check; a racer replacing the link in that window had its file deleted. That
// unlink-by-pathname is gone.
func atomicManagedSymlinkSwap(src, target string) error {
	if cur, err := os.Readlink(target); err == nil && cur == src {
		return nil // already correct (idempotent)
	}
	if err := fsops.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", target, err)
	}
	// CREATE path: os.Symlink is an atomic no-clobber create (EEXIST if
	// occupied). Success means target was absent and now points at src.
	if err := os.Symlink(src, target); err == nil {
		return nil
	} else if !os.IsExist(err) {
		// Not "already occupied" — e.g. symlinks unsupported/unprivileged.
		// Fail closed; never reach an unlink-based fallback (H17).
		return fmt.Errorf("H17 atomic swap: cannot create managed link %s (no unsafe fallback): %w", target, err)
	}
	// Occupied. Re-read to classify WITHOUT mutating anything yet.
	cur, rlErr := os.Readlink(target)
	if rlErr != nil {
		// Not a symlink at all (a real file/dir = user content) → refuse.
		return fmt.Errorf("refusing to replace %s: occupied by non-symlink content (user-authored) — leaving it intact", target)
	}
	if cur == src {
		return nil // a concurrent projector already set exactly what we wanted
	}
	artifactsRoot := config.ArtifactsRoot(config.AgentsHome())
	if !links.IsManagedLinkUnder(target, artifactsRoot) {
		// A FOREIGN symlink (points outside managed CAS) → refuse, untouched.
		return fmt.Errorf("refusing to replace %s: not a managed CAS link (foreign symlink or user content) — leaving it intact", target)
	}
	// REPOINT: target is our managed CAS link pointing at a different digest.
	return atomicSwapReplaceManagedLink(src, target, artifactsRoot)
}

// atomicSwapReplaceManagedLink repoints an EXISTING managed CAS symlink at
// target to src without ever unlinking target by pathname after a non-atomic
// identity check (defect 2). It stages a temp symlink → src, then atomically
// EXCHANGES it with target (RENAME_EXCHANGE / RENAME_SWAP): after the exchange
// target is our new symlink and the FORMER occupant sits at a private tmp name
// only this call knows — so it can be inspected free of any external race.
//
// If the former occupant is verifiably one of our managed CAS links, it is
// unlinked (the repoint is complete). If it is anything else — meaning a racer
// slipped a user file into target between our provenance check above and the
// exchange — the exchange is REVERSED (restoring the user file to target and
// our symlink to tmp), the tmp symlink is removed, and the call fails closed.
// The user file is never deleted.
//
// On an OS without an atomic exchange primitive (errSwapUnsupported) the
// repoint fails closed rather than falling back to an unsafe unlink+rename.
func atomicSwapReplaceManagedLink(src, target, artifactsRoot string) error {
	tmp := fmt.Sprintf("%s.casswap-%d", target, time.Now().UnixNano())
	if err := os.Symlink(src, tmp); err != nil {
		return fmt.Errorf("H17 atomic swap: cannot stage temp symlink for %s: %w", target, err)
	}
	if err := atomicSwapRename(tmp, target); err != nil {
		_ = fsops.Remove(tmp)
		if errors.Is(err, errSwapUnsupported) {
			return fmt.Errorf("refusing to repoint occupied managed link %s: atomic exchange unavailable on this OS (fail closed) — %w", target, err)
		}
		return fmt.Errorf("H17 atomic swap: exchange %s <-> %s: %w", tmp, target, err)
	}
	// Post-exchange: target = our new symlink; tmp = the former occupant, now
	// at a name only we hold (race-free to inspect).
	if links.IsManagedLinkUnder(tmp, artifactsRoot) {
		if err := fsops.Remove(tmp); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("H17 atomic swap: removing superseded managed link (now at %s): %w", tmp, err)
		}
		return nil
	}
	// The former occupant is NOT our managed link — a racer's user file landed
	// in target after our provenance check. Reverse the exchange to restore it,
	// then fail closed. The user file is returned to target untouched.
	if err := atomicSwapRename(tmp, target); err != nil {
		return fmt.Errorf("H17 atomic swap: CRITICAL — could not restore user content to %s after a racing write (it is currently at %s): %w", target, tmp, err)
	}
	_ = fsops.Remove(tmp)
	return fmt.Errorf("refusing to replace %s: a non-managed file raced into the target during projection — restored it and left it intact", target)
}
