package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// antigravity is the Platform implementation for Google's Antigravity coding
// harness (the successor to the Gemini CLI). It was hand-added as the F4/DC0
// "real harness" probe for the multi-harness-extensibility spec; the friction
// findings live in
// .agents/workflow/specs/multi-harness-extensibility/evidence/dc0-antigravity-handadd.md.
//
// OWNER-TODO (research assumptions, see the DC0 evidence file): Antigravity's
// authoritative on-disk layout is still sparsely documented. The native roots
// reported by vendor-adjacent sources are the shared `~/.gemini/` home tree and
// a project-local `.agents/` umbrella — but `.agents/` is ALSO dot-agents'
// canonical source root, so projecting into it verbatim would collide with the
// source of truth. To keep the probe safe and reversible this harness projects
// into a dedicated `.antigravity/` repo-local root instead; the collision and
// the `~/.gemini/` home reuse are recorded as the headline descriptor-schema
// finding (a harness that reads the canonical store directly needs a
// "zero-projection / identity read-path" the descriptor must be able to model).
type antigravity struct {
	io platformIO
}

const (
	antigravityDir          = ".antigravity"
	antigravitySettingsFile = "settings.json"
	antigravityMCPFile      = "mcp_config.json"
	antigravityHooksFile    = "hooks.json"
	// antigravityJSON is the canonical scoped source filename under
	// ~/.agents/<bucket>/<scope>/ that feeds the settings/mcp/hooks links,
	// matching the per-platform "<id>.json" convention (cursor.json,
	// opencode.json).
	antigravityJSON        = "antigravity.json"
	antigravityDisplayName = "Antigravity"
)

// NewAntigravity constructs the Antigravity platform with the real filesystem IO.
func NewAntigravity() Platform { return &antigravity{io: stdPlatformIO{}} }

func (a *antigravity) ID() string          { return "antigravity" }
func (a *antigravity) DisplayName() string { return antigravityDisplayName }

// SessionReader — Antigravity's session env-var contract is not yet confirmed
// (OWNER-TODO). Stubs are valid per the SessionReader doc until the contract is
// known; ANTIGRAVITY_SESSION_ID is the inferred analog of the other harnesses'
// <HARNESS>_SESSION_ID convention.
func (a *antigravity) AIAgentPrefix() string              { return "antigravity" }
func (a *antigravity) SessionEnvs() []string              { return []string{"ANTIGRAVITY_SESSION_ID"} }
func (a *antigravity) EntrypointEnvs() []string           { return nil }
func (a *antigravity) ResolveModel(_, _, _ string) string { return "" }

func (a *antigravity) IsInstalled() bool {
	return probeInstalled("antigravity")
}

func (a *antigravity) Version() string {
	return probeVersionLine("antigravity")
}

func (a *antigravity) HasDeprecatedFormat(_ string) bool { return false }
func (a *antigravity) DeprecatedDetails(_ string) string { return "" }

func (a *antigravity) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()
	if err := a.createScopedJSONLink(project, repoPath, agentsHome, "settings", antigravitySettingsFile); err != nil {
		return err
	}
	if err := a.createScopedJSONLink(project, repoPath, agentsHome, "mcp", antigravityMCPFile); err != nil {
		return err
	}
	if err := a.createHooksLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	// .antigravity/skills/ and .antigravity/agents/ are emitted by
	// CollectAndExecuteSharedTargetPlan via SharedTargetIntents; no direct
	// action is needed here.
	return nil
}

// createScopedJSONLink managed-replaces a single repo-local JSON config file
// (.antigravity/<destName>) from the canonical scoped source under
// ~/.agents/<bucket>/<scope>/antigravity.json. Mirrors cursor.createSettingsLinks
// / createMCPLinks: a hard link at a fixed owned path, routed through the
// Replacing variant so a stale managed link is relinked idempotently and a
// genuine user file is preserved as a sidecar backup.
func (a *antigravity) createScopedJSONLink(project, repoPath, agentsHome, bucket, destName string) error {
	if err := a.io.MkdirAll(filepath.Join(repoPath, antigravityDir), 0755); err != nil {
		return err
	}
	src := resolveScopedFile(agentsHome, bucket, project, antigravityJSON)
	if src == "" {
		return nil
	}
	return links.HardlinkReplacing(src, filepath.Join(repoPath, antigravityDir, destName), backupSidecar)
}

func (a *antigravity) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := a.writeRepoHooks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return a.writeUserHomeHooks(project, agentsHome)
}

func (a *antigravity) writeRepoHooks(project, repoPath, agentsHome string) error {
	repoTarget := filepath.Join(repoPath, antigravityDir, antigravityHooksFile)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, a.ID(), "global", project)
	if err != nil {
		return err
	}
	if err := a.io.MkdirAll(filepath.Join(repoPath, antigravityDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		a.io,
		repoTarget,
		renderAntigravityHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, antigravityJSON),
		directHardlinkHookMode,
		func(p string) error { return removeRenderedAntigravityHookConfig(a.io, p) },
		repoBundles,
	)
}

func (a *antigravity) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, a.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		a.io,
		filepath.Join(antigravityDir, antigravityHooksFile),
		renderAntigravityHookConfig,
		resolveHookSpecInScope(agentsHome, []string{"hooks"}, "global", antigravityJSON),
		directHardlinkHookMode,
		func(p string) error { return removeRenderedAntigravityHookConfig(a.io, p) },
		globalBundles,
	)
}

func (a *antigravity) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()
	return errors.Join(
		a.removeScopedJSONLink(repoPath, agentsHome, project, "settings", antigravitySettingsFile),
		a.removeScopedJSONLink(repoPath, agentsHome, project, "mcp", antigravityMCPFile),
		a.removeHooksLink(project, repoPath, agentsHome),
		a.removeSharedMirrorDir(repoPath, agentsHome, "skills"),
		a.removeSharedMirrorDir(repoPath, agentsHome, "agents"),
	)
}

func (a *antigravity) removeScopedJSONLink(repoPath, agentsHome, project, bucket, destName string) error {
	dst := filepath.Join(repoPath, antigravityDir, destName)
	return errors.Join(
		links.RemoveIfSymlinkUnder(dst, agentsHome),
		removeHardlinkedManaged(dst, scopedBucketFileSources(agentsHome, bucket, project, antigravityJSON)),
	)
}

func (a *antigravity) removeHooksLink(project, repoPath, agentsHome string) error {
	hooksFilePath := filepath.Join(repoPath, antigravityDir, antigravityHooksFile)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, a.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(a.io, repoBundles, hooksFilePath, renderAntigravityHookConfig)
	}
	return removeHardlinkedManaged(hooksFilePath, scopedBucketFileSources(agentsHome, "hooks", project, antigravityJSON))
}

// removeSharedMirrorDir tears down the managed symlink entries under
// .antigravity/<bucket>/ (skills or agents) that point into agentsHome. Mirrors
// opencode.RemoveLinks' skill-dir teardown so CreateLinks/RemoveLinks stay
// symmetric (R7).
func (a *antigravity) removeSharedMirrorDir(repoPath, agentsHome, bucket string) error {
	dir := filepath.Join(repoPath, antigravityDir, bucket)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(dir, e.Name()), agentsHome))
	}
	return errors.Join(errs...)
}

func (a *antigravity) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(antigravityDir, "skills"))
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentMirrorIntents(project, filepath.Join(antigravityDir, "agents"))
	if err != nil {
		return nil, err
	}
	return append(skills, agents...), nil
}

// antigravityManagedFiles returns the three repo-local single-file managed
// targets antigravity owns, shared by the diagnostics readers.
func antigravityManagedFiles(repoPath string) []string {
	return []string{
		filepath.Join(repoPath, antigravityDir, antigravitySettingsFile),
		filepath.Join(repoPath, antigravityDir, antigravityMCPFile),
		filepath.Join(repoPath, antigravityDir, antigravityHooksFile),
	}
}

func antigravityManagedDirs(repoPath string) []string {
	return []string{
		filepath.Join(repoPath, antigravityDir, "skills"),
		filepath.Join(repoPath, antigravityDir, "agents"),
	}
}

// BrokenLinks implements BrokenLinkReporter: a managed single-file link whose
// target is missing is reported broken; plain files and healthy links are
// skipped (the shared classifyManagedLink contract used by opencode/cursor).
func (a *antigravity) BrokenLinks(_, repoPath, _ string) []BrokenLink {
	var broken []BrokenLink
	for _, linkPath := range antigravityManagedFiles(repoPath) {
		state, raw := classifyManagedLink(linkPath)
		if state != linkStateBroken {
			continue
		}
		broken = append(broken, BrokenLink{
			PlatformID:  "antigravity",
			LinkPath:    linkPath,
			Dest:        raw,
			DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
		})
	}
	return broken
}

// CountLinks implements LinkCounter over antigravity's repo-local managed
// surface: the three config files plus the skills/agents mirror dirs.
func (a *antigravity) CountLinks(_, repoPath, _ string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, antigravityManagedFiles(repoPath))
	addManagedDirCounts(&ok, &broken, antigravityManagedDirs(repoPath))
	return ok, broken
}

// Badge implements StatusBadger.
func (a *antigravity) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := a.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: a.DisplayName(), Present: ok > 0, Broken: broken > 0}
}

// antigravityUserConfigFiles returns the managed single-file references
// antigravity maintains under the user's home: ~/.antigravity/hooks.json (the
// only user-scope target writeUserHomeHooks emits). The vendor's broader
// documented user-home layout (~/.gemini/...) is NOT wired by dot-agents yet
// (OWNER-TODO), so only the hooks file is reported today — paralleling cursor's
// ~/.cursor/hooks.json-only user surface.
func antigravityUserConfigFiles(home string) []string {
	return []string{filepath.Join(home, antigravityDir, antigravityHooksFile)}
}

// UserBrokenLinks implements UserConfigReporter.
func (a *antigravity) UserBrokenLinks(home string) []BrokenLink {
	return scanUserBrokenLinks("antigravity", antigravityUserConfigFiles(home), nil)
}

// UserBadge implements UserConfigReporter.
func (a *antigravity) UserBadge(home string) PlatformBadge {
	ok, broken := scanUserConfigCounts(antigravityUserConfigFiles(home), nil)
	return PlatformBadge{Name: a.DisplayName(), Present: ok > 0, Broken: broken > 0}
}

// PrintAudit implements AuditPrinter: renders the three config-file links and
// the skills/agents mirror dirs under `da status --audit`.
func (a *antigravity) PrintAudit(w io.Writer, _, repoPath, _ string) {
	fmt.Fprintf(w, "    %s%s%s\n", ui.Cyan, antigravityDisplayName, ui.Reset)
	for _, name := range []string{antigravitySettingsFile, antigravityMCPFile, antigravityHooksFile} {
		printSymlinkAudit(w, filepath.Join(repoPath, antigravityDir, name), antigravityDir+"/"+name)
	}
	for _, bucket := range []string{"skills", "agents"} {
		dir := filepath.Join(repoPath, antigravityDir, bucket)
		label := antigravityDir + "/" + bucket + "/"
		printSymlinkDirAudit(w, dir, label, label+"%s")
	}
	fmt.Fprintln(w)
}

// ---- Hook rendering (touchpoint #6) ----------------------------------------
//
// Antigravity's hooks.json is reported to follow the Claude-shaped per-event
// array of {matcher, hooks:[{type:command, command, timeout}]} entries (the I/O
// contract differs — stdin/stdout JSON with a decision field — but that is the
// hook SCRIPT's concern, not the projector's). The struct is kept harness-local
// (rather than reusing claudeRenderedHooks) to match the per-harness pattern and
// because the timeout field diverges from Claude's action shape.
//
// OWNER-TODO: one source reports antigravity nests events under a top-level
// hook-name key (`{"<name>":{"PreToolUse":[...]}}`). If confirmed, only this
// render struct + detector change — the event table and dispatch are unaffected.

type antigravityRenderedHooks struct {
	Hooks map[string][]antigravityRenderedEntry `json:"hooks"`
}

type antigravityRenderedEntry struct {
	Matcher string                      `json:"matcher,omitempty"`
	Hooks   []antigravityRenderedAction `json:"hooks"`
}

type antigravityRenderedAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// antigravityEventTable is the canonical→native event-name map (touchpoint #3,
// pure data). Only the events with a confirmed canonical analog are mapped;
// antigravity's PreInvocation/PostInvocation lifecycle events have no canonical
// equivalent today and are intentionally omitted (recorded as a descriptor
// finding: a new harness can introduce event vocabulary the canonical set does
// not yet name).
var antigravityEventTable = map[string]string{
	"pre_tool_use":  "PreToolUse",
	"post_tool_use": "PostToolUse",
	"stop":          "Stop",
}

func antigravityEventName(spec HookSpec) (string, bool) {
	return mapEventName(spec, "antigravity", antigravityEventTable)
}

func renderAntigravityHookConfig(specs []HookSpec) ([]byte, error) {
	out := antigravityRenderedHooks{Hooks: map[string][]antigravityRenderedEntry{}}
	for _, parent := range specs {
		for _, spec := range expandHookSpecEvents(parent) {
			event, entry, include, err := renderAntigravityHookEntry(spec)
			if err != nil {
				return nil, err
			}
			if !include {
				continue
			}
			out.Hooks[event] = append(out.Hooks[event], entry)
		}
	}
	return marshalJSON(out)
}

func renderAntigravityHookEntry(spec HookSpec) (string, antigravityRenderedEntry, bool, error) {
	event, ok := antigravityEventName(spec)
	if !ok {
		if hookRequiredOnPlatform(spec, "antigravity") {
			return "", antigravityRenderedEntry{}, false, fmt.Errorf("hook %q is not representable for antigravity event %q", spec.Name, spec.When)
		}
		return "", antigravityRenderedEntry{}, false, nil
	}
	command := ResolveHookCommand(spec)
	if command == "" {
		if hookRequiredOnPlatform(spec, "antigravity") {
			return "", antigravityRenderedEntry{}, false, fmt.Errorf("hook %q has no command for antigravity", spec.Name)
		}
		return "", antigravityRenderedEntry{}, false, nil
	}
	entry := antigravityRenderedEntry{
		Matcher: matcherForSpec(spec, "antigravity", ""),
		Hooks:   []antigravityRenderedAction{{Type: "command", Command: command}},
	}
	if spec.TimeoutMS > 0 {
		entry.Hooks[0].Timeout = spec.TimeoutMS / 1000
		if entry.Hooks[0].Timeout == 0 {
			entry.Hooks[0].Timeout = 1
		}
	}
	return event, entry, true, nil
}

func isLikelyRenderedAntigravityHookConfig(content []byte) bool {
	var payload antigravityRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		return false
	}
	return len(payload.Hooks) > 0
}

func removeRenderedAntigravityHookConfig(io platformIO, path string) error {
	return removeManagedFileIf(io, path, isLikelyRenderedAntigravityHookConfig)
}
