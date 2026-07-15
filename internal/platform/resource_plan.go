package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
// (LOCAL-authored resources), plus — when a caller-driven projection is
// active (ProjectResolvedUnits) — one CAS-direct intent per caller-resolved
// packages unit of this bucket, linking straight to the immutable store
// digest path (H13). All three per-bucket helpers (skill / plugin / agent
// dir mirror) delegate here.
//
// Authority for the packages half is ENTIRELY the caller's resolved-unit set
// (casUnitsForBucket) — never a scan of the global store — so a package
// materialized for one project never leaks into another and two projects
// pinning different digests of the same source/name cannot collide (the old
// global "_sourced" scan that broke this is gone).
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
			// No local-authored resources; caller-resolved packages units may
			// still project below.
			return casMirrorIntents(project, targetRoot, spec.Bucket, spec.ManifestName, spec.Materializer), nil
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
	// H13: append caller-resolved packages units of this bucket, CAS-direct.
	intents = append(intents, casMirrorIntents(project, targetRoot, spec.Bucket, spec.ManifestName, spec.Materializer)...)
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
		lines := dryRunExactProjectionLines(plan, repoPath)
		if len(lines) == 0 {
			return []string{"shared targets: (none)"}, nil
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
	return nil, nil
}

// dryRunExactProjectionLines is the dry-run preview for the exact projection:
// the additive write lines (formatSharedTargetPlanForDryRun) followed by a
// "prune managed" line for every managed output PruneStaleSharedTargets would
// delete. It never mutates the filesystem — the prune scan only reads.
func dryRunExactProjectionLines(plan ResourcePlan, repoPath string) []string {
	var lines []string
	if len(plan.Resources) > 0 {
		lines = append(lines, formatSharedTargetPlanForDryRun(plan, repoPath)...)
	}
	for _, target := range plan.staleManagedTargets(repoPath, config.AgentsHome()) {
		lines = append(lines, fmt.Sprintf("shared target: prune managed %s", filepath.ToSlash(config.DisplayPath(target))))
	}
	return lines
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
	wanted, dirs := p.prunableTargets(repoPath)
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
	if !strings.HasPrefix(u.Digest, "sha256:") || len(u.Digest) != len("sha256:")+64 {
		return fmt.Errorf("platform: resolved unit %s/%s: malformed digest %q", u.Family, u.Name, u.Digest)
	}
	want := config.ArtifactStorePath(agentsHome, u.Family, u.Digest)
	if filepath.Clean(u.CASPath) != filepath.Clean(want) {
		return fmt.Errorf("platform: resolved unit %s/%s: CAS path %q is not the store path for its digest (%q)", u.Family, u.Name, u.CASPath, want)
	}
	return nil
}

// projectionUnitsState is the caller-supplied resolved-unit set for the
// CURRENT caller-driven projection (H13). It is a package-scoped projection
// context — the same established pattern as config.SetWindowsMirrorContext —
// so the shared mirror-intent builders (which every platform's
// SharedTargetIntents calls with ITS OWN per-bucket target roots) can emit
// CAS-direct intents for the caller's units WITHOUT any platform-file change
// and WITHOUT ever scanning ~/.agents for authority. projectionSerialize
// ensures only one caller-driven projection populates the context at a time;
// unitsMu guards the slice for race-detector cleanliness against a plain
// RunSharedTargetProjectionExact call (refresh/install) that reads an empty
// context concurrently.
var (
	projectionSerialize sync.Mutex
	unitsMu             sync.Mutex
	currentUnits        []ResolvedUnit
)

// casUnitsForBucket returns the caller-supplied resolved units whose Family
// matches bucket (H13). Empty when no caller-driven projection is active
// (the plain refresh/install path), so those callers project local-authored
// resources only.
func casUnitsForBucket(bucket string) []ResolvedUnit {
	unitsMu.Lock()
	defer unitsMu.Unlock()
	var out []ResolvedUnit
	for _, u := range currentUnits {
		if u.Family == bucket {
			out = append(out, u)
		}
	}
	return out
}

// ProjectResolvedUnits projects a caller-supplied set of resolved packages
// units into project's repo, each linking DIRECTLY to its immutable CAS
// digest path (H13 per-project CAS-direct). It reuses the exact/prune
// projection (D4: RunSharedTargetProjectionExact — not a parallel linker):
// the caller's units are set as the projection context, so the shared
// mirror-intent builders emit CAS-direct intents at each platform's own
// target roots, composed and pruned in ONE plan alongside the project's
// local-authored resources. Authority is entirely the caller's unit list —
// nothing is discovered by scanning the global store, so a package
// materialized for one project never leaks into another and two projects
// pinning different digests of the same source/name never collide.
//
// Every unit is validated (H15 identity + H13/H16 CAS-path binding) before
// projection; a single invalid unit fails the whole call closed. localScopes
// are the reserved local scope names each unit's source id must not equal
// (H3).
func ProjectResolvedUnits(project, repoPath string, units []ResolvedUnit, platforms []Platform, dryRun, exact bool, localScopes ...string) ([]string, error) {
	agentsHome := config.AgentsHome()
	for _, u := range units {
		if err := ValidateResolvedUnit(agentsHome, u, localScopes...); err != nil {
			return nil, err
		}
	}
	projectionSerialize.Lock()
	defer projectionSerialize.Unlock()
	unitsMu.Lock()
	currentUnits = units
	unitsMu.Unlock()
	defer func() {
		unitsMu.Lock()
		currentUnits = nil
		unitsMu.Unlock()
	}()
	return RunSharedTargetProjectionExact(project, repoPath, platforms, dryRun, exact)
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

// casMirrorIntents builds the CAS-direct ResourceIntents for the caller's
// resolved units of one bucket at one target root (H13). Each intent's
// SourceRef addresses the immutable store path
// "<agentsHome>/cache/artifacts/<family>/<hex>" directly (Scope "artifacts",
// Bucket "cache") — no shared mutable alias. ReplacePolicy is
// ResourceReplaceNever so a real (non-symlink) occupant is refused, and the
// casDirectOrigin marker routes the link step through the H17 atomic symlink
// swap (repoint-or-refuse) rather than a RemoveAll-after-check.
func casMirrorIntents(project, targetRoot, bucket, marker, materializer string) []ResourceIntent {
	units := casUnitsForBucket(bucket)
	intents := make([]ResourceIntent, 0, len(units))
	for _, u := range units {
		intents = append(intents, ResourceIntent{
			IntentID:    fmt.Sprintf("%s.sourced.%s.%s.%s", bucket, sanitizeIntentRoot(u.SourceID), u.Name, sanitizeIntentRoot(targetRoot)),
			Project:     project,
			Bucket:      bucket,
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
			Materializer:  materializer,
			ReplacePolicy: ResourceReplaceNever,
			PrunePolicy:   ResourcePruneTarget,
			MarkerFiles:   []string{marker},
		})
	}
	return intents
}

// atomicManagedSymlinkSwap creates or repoints a managed symlink at target
// pointing to src with NO RemoveAll-after-check (H17). It refuses up front to
// touch any occupant that is not itself a symlink (a real dir/file is
// caller/user content), then publishes via a same-dir temp symlink + atomic
// os.Rename — which replaces an existing SYMLINK in one step (no window where
// target is absent) and FAILS closed against a real directory/file (rename of
// a symlink onto a non-empty dir errors), so user content is never deleted
// even if the entry changed between the check and the rename. On a platform
// where os.Symlink is unavailable/again-privileged (Windows without symlink
// privilege), it falls back to links.SymlinkReplacing so behavior is no worse
// than the pre-existing shared path there.
func atomicManagedSymlinkSwap(src, target string) error {
	if cur, err := os.Readlink(target); err == nil && cur == src {
		return nil // already correct
	}
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to replace unmanaged entry %s (not a managed symlink)", target)
	}
	if err := fsops.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", target, err)
	}
	tmp := fmt.Sprintf("%s.casswap-%d", target, time.Now().UnixNano())
	if err := os.Symlink(src, tmp); err != nil {
		// os.Symlink unsupported/again-denied (e.g. Windows without the
		// privilege): fall back to the shared primitive rather than fail the
		// whole projection. links refuses unmanaged occupants too.
		return links.Symlink(src, target)
	}
	if err := fsops.Rename(tmp, target); err != nil {
		_ = fsops.Remove(tmp)
		return fmt.Errorf("atomic symlink swap %s -> %s: %w", target, src, err)
	}
	return nil
}
