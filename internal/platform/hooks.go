package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/links"
	"go.yaml.in/yaml/v3"
)

type HookSourceKind string

const (
	HookSourceLegacyFile      HookSourceKind = "legacy_file"
	HookSourceCanonicalBundle HookSourceKind = "canonical_bundle"
)

type HookShape string

const (
	HookShapeDirect       HookShape = "direct"
	HookShapeRenderSingle HookShape = "render_single"
	HookShapeRenderFanout HookShape = "render_fanout"
)

type HookTransport string

const (
	HookTransportSymlink  HookTransport = "symlink"
	HookTransportHardlink HookTransport = "hardlink"
	HookTransportWrite    HookTransport = "write"
)

type HookEmissionMode struct {
	Shape     HookShape
	Transport HookTransport
}

var (
	directSymlinkHookMode  = HookEmissionMode{Shape: HookShapeDirect, Transport: HookTransportSymlink}
	directHardlinkHookMode = HookEmissionMode{Shape: HookShapeDirect, Transport: HookTransportHardlink}
)

type HookPlatformOverride struct {
	Event   string `yaml:"event"`
	Matcher string `yaml:"matcher"`
	File    string `yaml:"file"`
}

type HookSpec struct {
	Name         string
	Scope        string
	SourcePath   string
	SourceBucket string
	SourceKind   HookSourceKind
	Description  string
	When         string
	// WhenEvents, when non-empty, declares a multi-event hook. The
	// loader rejects manifests that set both `when` and `when_events`
	// (mutual exclusion) and manifests with duplicate or unknown
	// canonical events (P1c contract `when_events` rules). At render
	// time the spec is expanded into one rendered action per canonical
	// event the target platform documents.
	WhenEvents        []string
	MatchTools        []string
	MatchExpression   string
	Command           string
	TimeoutMS         int
	EnabledOn         []string
	RequiredOn        []string
	PlatformOverrides map[string]HookPlatformOverride
}

type hookManifest struct {
	Name              string                          `yaml:"name"`
	Description       string                          `yaml:"description"`
	When              string                          `yaml:"when"`
	WhenEvents        []string                        `yaml:"when_events"`
	Match             hookMatchManifest               `yaml:"match"`
	Run               hookRunManifest                 `yaml:"run"`
	EnabledOn         []string                        `yaml:"enabled_on"`
	RequiredOn        []string                        `yaml:"required_on"`
	PlatformOverrides map[string]HookPlatformOverride `yaml:"platform_overrides"`
}

type hookMatchManifest struct {
	Tools      []string `yaml:"tools"`
	Expression string   `yaml:"expression"`
}

type hookRunManifest struct {
	Command   string `yaml:"command"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

type claudeRenderedHooks struct {
	Schema string                           `json:"$schema,omitempty"`
	Hooks  map[string][]claudeRenderedEntry `json:"hooks"`
}

type claudeRenderedEntry struct {
	Matcher string                 `json:"matcher"`
	Hooks   []claudeRenderedAction `json:"hooks"`
}

type claudeRenderedAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type codexRenderedHooks struct {
	Hooks map[string][]claudeRenderedEntry `json:"hooks"`
}

type cursorRenderedHooks struct {
	Version int                              `json:"version"`
	Hooks   map[string][]cursorRenderedEntry `json:"hooks"`
}

type cursorRenderedEntry struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type copilotRenderedHooks struct {
	Version int                                `json:"version"`
	Hooks   map[string][]copilotRenderedAction `json:"hooks"`
}

type copilotRenderedAction struct {
	Type       string `json:"type"`
	Bash       string `json:"bash"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

func resolveHookSpec(agentsHome string, buckets []string, project string, names ...string) (*HookSpec, error) {
	return resolveHookSpecInScopes(agentsHome, buckets, scopedNames(project), names...)
}

func resolveHookSpecInScope(agentsHome string, buckets []string, scope string, names ...string) (*HookSpec, error) {
	return resolveHookSpecInScopes(agentsHome, buckets, []string{scope}, names...)
}

// resolveHookSpecInScopes searches scope × bucket × name candidates in order
// and returns the first that exists. A confirmed-absent candidate
// (fsops.StatAllowMissing found=false, err=nil) is skipped and the search
// continues; a real Stat error (permission denied, I/O failure, ...) aborts
// the whole search immediately and propagates — it must never be treated as
// "this candidate doesn't exist" and silently skipped, since that is exactly
// the swallow that let a source-read error masquerade as "nothing to render"
// upstream in emitPreferredHookFile.
func resolveHookSpecInScopes(agentsHome string, buckets []string, scopes []string, names ...string) (*HookSpec, error) {
	for _, scope := range scopes {
		for _, bucket := range buckets {
			for _, name := range names {
				src := filepath.Join(agentsHome, bucket, scope, name)
				_, found, err := fsops.StatAllowMissing(src)
				if err != nil {
					return nil, err
				}
				if found {
					return &HookSpec{
						Name:         strings.TrimSuffix(name, filepath.Ext(name)),
						Scope:        scope,
						SourcePath:   src,
						SourceBucket: bucket,
						SourceKind:   HookSourceLegacyFile,
					}, nil
				}
			}
		}
	}
	return nil, nil
}

// ListHookSpecs returns hook entries under ~/.agents/hooks/<scope>/: canonical bundles
// (…/<name>/HOOK.yaml) and legacy single-file JSON hooks. The hooks directory must exist;
// if it is missing, ReadDir fails with an error satisfying os.IsNotExist.
func ListHookSpecs(agentsHome, scope string) ([]HookSpec, error) {
	root := filepath.Join(agentsHome, "hooks", scope)
	entries, err := os.ReadDir(root)
	if err != nil {
		// Distinguish "bucket absent" (legitimate no-op; callers swallow via
		// os.IsNotExist) from "bucket exists but is not a listable directory"
		// (a regular file masquerading as the bucket, EACCES, EIO — real
		// corruption that must surface). This must be OS-independent: Windows
		// os.ReadDir on a regular-file path maps to a NotExist-class error,
		// which would otherwise silently hide the corruption. The %v (not %w)
		// deliberately breaks the fs.ErrNotExist chain so the fault propagates
		// on every OS.
		if _, statErr := os.Lstat(root); statErr == nil {
			return nil, fmt.Errorf("hooks bucket %s exists but is not a listable directory (%v)", root, err)
		}
		return nil, err
	}

	byName := map[string]HookSpec{}
	for _, entry := range entries {
		spec, ok, loadErr := loadHookSpecEntry(root, scope, entry)
		if loadErr != nil {
			return nil, loadErr
		}
		if !ok {
			continue
		}
		if _, exists := byName[spec.Name]; exists {
			continue
		}
		byName[spec.Name] = spec
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]HookSpec, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func loadHookSpecEntry(root, scope string, entry os.DirEntry) (HookSpec, bool, error) {
	if entry.IsDir() {
		spec, ok, err := loadHookBundleSpec(root, scope, entry.Name())
		if err != nil {
			return HookSpec{}, false, err
		}
		return spec, ok, nil
	}
	if !strings.HasSuffix(entry.Name(), ".json") {
		return HookSpec{}, false, nil
	}
	name := strings.TrimSuffix(entry.Name(), ".json")
	return HookSpec{
		Name:         name,
		Scope:        scope,
		SourcePath:   filepath.Join(root, entry.Name()),
		SourceBucket: "hooks",
		SourceKind:   HookSourceLegacyFile,
	}, true, nil
}

func listCanonicalHookSpecs(agentsHome, scope string) ([]HookSpec, error) {
	specs, err := ListHookSpecs(agentsHome, scope)
	if err != nil {
		return nil, err
	}
	out := make([]HookSpec, 0, len(specs))
	for _, spec := range specs {
		if spec.SourceKind == HookSourceCanonicalBundle {
			out = append(out, spec)
		}
	}
	return out, nil
}

func collectCanonicalHookSpecsForPlatform(agentsHome, project, platformID string, scopes ...string) ([]HookSpec, error) {
	merged := map[string]HookSpec{}
	for _, scope := range scopes {
		specs, err := listCanonicalHookSpecs(agentsHome, scope)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, spec := range specs {
			if !hookEnabledOnPlatform(spec, platformID) {
				continue
			}
			merged[spec.Name] = spec
		}
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]HookSpec, 0, len(names))
	for _, name := range names {
		out = append(out, merged[name])
	}
	return out, nil
}

func emitHookSpec(io platformIO, spec *HookSpec, dst string, mode HookEmissionMode) error {
	if spec == nil {
		return nil
	}
	switch mode.Shape {
	case HookShapeDirect:
		return emitHookFile(io, spec.SourcePath, dst, mode.Transport)
	case HookShapeRenderSingle, HookShapeRenderFanout:
		return fmt.Errorf("hook emission shape %q is not supported for single direct emission", mode.Shape)
	default:
		return fmt.Errorf("unknown hook emission shape %q", mode.Shape)
	}
}

func emitHookSpecToUserHomes(io platformIO, spec *HookSpec, relativePath string, mode HookEmissionMode) error {
	if spec == nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := emitHookSpec(io, spec, filepath.Join(homeRoot, relativePath), mode); err != nil {
			return err
		}
	}
	return nil
}

func emitHookFanout(io platformIO, specs []HookSpec, dstRoot string, mode HookEmissionMode, mapName func(HookSpec) (string, bool)) error {
	if mode.Shape != HookShapeRenderFanout {
		return fmt.Errorf("hook fanout requires %q shape, got %q", HookShapeRenderFanout, mode.Shape)
	}
	if err := io.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, spec := range specs {
		name, ok := mapName(spec)
		if !ok {
			continue
		}
		if err := emitHookFile(io, spec.SourcePath, filepath.Join(dstRoot, name), mode.Transport); err != nil {
			return err
		}
	}
	return nil
}

func emitRenderedHookFile(io platformIO, specs []HookSpec, dst string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	content, err := render(specs)
	if err != nil {
		return err
	}
	return writeManagedFile(io, dst, content)
}

func emitRenderedHookFileToUserHomes(io platformIO, specs []HookSpec, relativePath string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := emitRenderedHookFile(io, specs, filepath.Join(homeRoot, relativePath), render); err != nil {
			return err
		}
	}
	return nil
}

func emitPreferredHookFile(
	io platformIO,
	dst string,
	render func([]HookSpec) ([]byte, error),
	legacy *HookSpec,
	mode HookEmissionMode,
	removeRendered func(string) error,
	canonicalSets ...[]HookSpec,
) error {
	for _, specs := range canonicalSets {
		if len(specs) == 0 {
			continue
		}
		return emitRenderedHookFile(io, specs, dst, render)
	}
	if legacy != nil {
		return emitHookSpec(io, legacy, dst, mode)
	}
	if removeRendered != nil {
		return removeRendered(dst)
	}
	return nil
}

func emitPreferredHookFileToUserHomes(
	io platformIO,
	relativePath string,
	render func([]HookSpec) ([]byte, error),
	legacy *HookSpec,
	mode HookEmissionMode,
	removeRendered func(string) error,
	canonicalSets ...[]HookSpec,
) error {
	for _, specs := range canonicalSets {
		if len(specs) == 0 {
			continue
		}
		return emitRenderedHookFileToUserHomes(io, specs, relativePath, render)
	}
	if legacy != nil {
		return emitHookSpecToUserHomes(io, legacy, relativePath, mode)
	}
	if removeRendered == nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := removeRendered(filepath.Join(homeRoot, relativePath)); err != nil {
			return err
		}
	}
	return nil
}

func removeRenderedClaudeHookSettings(io platformIO, path string) error {
	return removeManagedFileIf(io, path, isLikelyRenderedClaudeHookSettings)
}

func removeRenderedCodexHookConfig(io platformIO, path string) error {
	return removeManagedFileIf(io, path, isLikelyRenderedCodexHookConfig)
}

func removeRenderedCursorHookConfig(io platformIO, path string) error {
	return removeManagedFileIf(io, path, isLikelyRenderedCursorHookConfig)
}

func removeManagedRenderedHookFile(io platformIO, specs []HookSpec, dst string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	content, err := render(specs)
	if err != nil {
		return err
	}
	return removeManagedFile(io, dst, content)
}

func removeManagedRenderedHookFileToUserHomes(io platformIO, specs []HookSpec, relativePath string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := removeManagedRenderedHookFile(io, specs, filepath.Join(homeRoot, relativePath), render); err != nil {
			return err
		}
	}
	return nil
}

func emitRenderedHookFanout(io platformIO, specs []HookSpec, dstRoot string, render func(HookSpec) (string, []byte, bool, error)) error {
	if len(specs) == 0 {
		return nil
	}
	if err := io.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, parent := range specs {
		for _, spec := range expandHookSpecForFanout(parent) {
			name, content, ok, err := render(spec)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := writeManagedFile(io, filepath.Join(dstRoot, name), content); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeManagedRenderedHookFanout(io platformIO, specs []HookSpec, dstRoot string, render func(HookSpec) (string, []byte, bool, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, parent := range specs {
		for _, spec := range expandHookSpecForFanout(parent) {
			name, content, ok, err := render(spec)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if err := removeManagedFile(io, filepath.Join(dstRoot, name), content); err != nil {
				return err
			}
		}
	}
	return removeDirIfEmpty(dstRoot)
}

// expandHookSpecForFanout is the per-file flavor of expandHookSpecEvents:
// when a spec declares multiple `when_events`, each rendered file must
// land at a distinct path. We disambiguate by appending the canonical
// event to the spec Name so the per-spec render functions produce one
// file per event (e.g. `gate-pre_tool_use.json`, `gate-stop.json`).
// Single-event and scalar `when` hooks are returned untouched so their
// existing filename layout is preserved.
func expandHookSpecForFanout(spec HookSpec) []HookSpec {
	views := expandHookSpecEvents(spec)
	if len(views) <= 1 {
		return views
	}
	for i := range views {
		views[i].Name = spec.Name + "-" + views[i].When
	}
	return views
}

func emitHookFile(io platformIO, src, dst string, transport HookTransport) error {
	switch transport {
	case HookTransportSymlink:
		// Managed-replace: the hook file is a dot-agents output at a fixed
		// platform-owned path, re-emitted every refresh. Use the Replacing
		// variant so a stale managed link is relinked idempotently and a
		// genuine user file is preserved as <dst>.dot-agents-backup.
		return links.SymlinkReplacing(src, dst, backupSidecar)
	case HookTransportHardlink:
		// Managed-replace; the prior managed hard link has no reparse point so
		// plain Hardlink would refuse it — same rationale as the symlink case.
		return links.HardlinkReplacing(src, dst, backupSidecar)
	case HookTransportWrite:
		content, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return writeManagedFile(io, dst, content)
	default:
		return fmt.Errorf("unknown hook transport %q", transport)
	}
}

func loadHookBundleSpec(root, scope, dirName string) (HookSpec, bool, error) {
	manifestPath := filepath.Join(root, dirName, "HOOK.yaml")
	content, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return HookSpec{}, false, nil
	}
	if err != nil {
		return HookSpec{}, false, err
	}

	var manifest hookManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return HookSpec{}, false, fmt.Errorf("parse %s: %w", manifestPath, err)
	}

	whenEvents, err := validateHookWhenEvents(manifestPath, manifest)
	if err != nil {
		return HookSpec{}, false, err
	}

	spec := HookSpec{
		Name:              strings.TrimSpace(manifest.Name),
		Scope:             scope,
		SourcePath:        manifestPath,
		SourceBucket:      "hooks",
		SourceKind:        HookSourceCanonicalBundle,
		Description:       strings.TrimSpace(manifest.Description),
		When:              strings.TrimSpace(manifest.When),
		WhenEvents:        whenEvents,
		MatchTools:        append([]string{}, manifest.Match.Tools...),
		MatchExpression:   strings.TrimSpace(manifest.Match.Expression),
		Command:           strings.TrimSpace(manifest.Run.Command),
		TimeoutMS:         manifest.Run.TimeoutMS,
		EnabledOn:         append([]string{}, manifest.EnabledOn...),
		RequiredOn:        append([]string{}, manifest.RequiredOn...),
		PlatformOverrides: manifest.PlatformOverrides,
	}
	if spec.Name == "" {
		spec.Name = dirName
	}
	return spec, true, nil
}

// validateHookWhenEvents enforces the P1c when_events contract:
//   - exactly one of `when` (scalar) or `when_events` (non-empty array)
//     must be present;
//   - duplicate entries inside `when_events` are rejected;
//   - unknown canonical events (not in any per-platform mapper table) are
//     rejected so typos do not silently become no-ops on every platform.
//
// Returns the normalized whenEvents slice (trimmed, may be nil/empty when
// the manifest uses scalar `when`).
func validateHookWhenEvents(manifestPath string, manifest hookManifest) ([]string, error) {
	whenScalar := strings.TrimSpace(manifest.When)

	// Trim and drop blank strings from when_events for validation.
	trimmedEvents := make([]string, 0, len(manifest.WhenEvents))
	for _, raw := range manifest.WhenEvents {
		if v := strings.TrimSpace(raw); v != "" {
			trimmedEvents = append(trimmedEvents, v)
		}
	}

	hasScalar := whenScalar != ""
	hasEvents := len(trimmedEvents) > 0

	if hasScalar && hasEvents {
		return nil, fmt.Errorf("parse %s: hook may not set both `when` and `when_events`", manifestPath)
	}
	if !hasScalar && !hasEvents {
		// Allow a hook with neither when nor when_events to remain
		// supported for backward compatibility — many existing hooks
		// rely on platform_overrides.event instead. Only enforce the
		// mutual-exclusion + duplicate / unknown rules when when_events
		// is explicitly used.
		return nil, nil
	}
	if !hasEvents {
		return nil, nil
	}

	seen := make(map[string]bool, len(trimmedEvents))
	for _, event := range trimmedEvents {
		if seen[event] {
			return nil, fmt.Errorf("parse %s: hook `when_events` contains duplicate canonical event %q", manifestPath, event)
		}
		seen[event] = true
		if !isKnownCanonicalEvent(event) {
			return nil, fmt.Errorf("parse %s: hook `when_events` lists unknown canonical event %q (not documented on any platform mapper)", manifestPath, event)
		}
	}
	return trimmedEvents, nil
}

// isKnownCanonicalEvent reports whether name is registered in at least one
// per-platform mapper table. The mapper tables are the documentation
// authority for which canonical When values exist; if no platform can
// render the event, the load must fail loudly rather than silently emit
// nothing on every platform.
func isKnownCanonicalEvent(name string) bool {
	for _, table := range []map[string]string{claudeEventTable, codexEventTable, cursorEventTable, copilotEventTable, antigravityEventTable} {
		if _, ok := table[name]; ok {
			return true
		}
	}
	return false
}

// expandHookSpecEvents returns the per-event HookSpec views the renderers
// iterate. For a scalar `when` hook (or one with neither when nor
// when_events) the result is a single-element slice containing spec
// itself. For a `when_events` hook, each entry yields a copy of spec with
// `When` set to that event and `WhenEvents` cleared so downstream code
// sees the same shape it did pre-P1c.
func expandHookSpecEvents(spec HookSpec) []HookSpec {
	if len(spec.WhenEvents) == 0 {
		return []HookSpec{spec}
	}
	out := make([]HookSpec, 0, len(spec.WhenEvents))
	for _, event := range spec.WhenEvents {
		view := spec
		view.When = event
		view.WhenEvents = nil
		out = append(out, view)
	}
	return out
}

func hookEnabledOnPlatform(spec HookSpec, platformID string) bool {
	if len(spec.EnabledOn) == 0 {
		return true
	}
	for _, id := range spec.EnabledOn {
		if id == platformID {
			return true
		}
	}
	return false
}

func hookRequiredOnPlatform(spec HookSpec, platformID string) bool {
	for _, id := range spec.RequiredOn {
		if id == platformID {
			return true
		}
	}
	return false
}

// ResolveHookCommand returns the hook command with relative paths resolved against the HOOK.yaml location.
// A first token written as ./x or ../x is always resolved. A bare relative
// token (e.g. "x.sh") is resolved only when it names an existing regular file
// beside the hook's manifest — bare binaries expected on PATH (e.g. "echo")
// never match a bundle file and pass through untouched, so rendered configs
// cannot end up with repo-cwd-relative script paths that break when the
// harness runs the hook from a subdirectory or a linked git worktree.
func ResolveHookCommand(spec HookSpec) string {
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return ""
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return command
	}
	first := parts[0]
	explicitRelative := strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../")
	if explicitRelative || bundleRelativeHookCommandFile(spec, first) {
		parts[0] = filepath.Clean(filepath.Join(filepath.Dir(spec.SourcePath), first))
		return strings.Join(parts, " ")
	}
	return command
}

// bundleRelativeHookCommandFile reports whether first is a non-absolute
// command token that resolves to an existing regular file beside the hook's
// HOOK.yaml. Only such tokens are safe to absolutize; anything else (bare
// binaries on PATH, absolute paths, specs without a source path) is left
// alone.
func bundleRelativeHookCommandFile(spec HookSpec, first string) bool {
	if first == "" || filepath.IsAbs(first) || strings.TrimSpace(spec.SourcePath) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(spec.SourcePath), first))
	return err == nil && info.Mode().IsRegular()
}

func platformOverride(spec HookSpec, platformID string) HookPlatformOverride {
	if spec.PlatformOverrides == nil {
		return HookPlatformOverride{}
	}
	return spec.PlatformOverrides[platformID]
}

func matcherForSpec(spec HookSpec, platformID string, fallback string) string {
	if override := strings.TrimSpace(platformOverride(spec, platformID).Matcher); override != "" {
		return override
	}
	if expression := strings.TrimSpace(spec.MatchExpression); expression != "" {
		return expression
	}
	if len(spec.MatchTools) == 0 {
		return fallback
	}
	return strings.Join(spec.MatchTools, "|")
}

func specHasMatcher(spec HookSpec) bool {
	if strings.TrimSpace(spec.MatchExpression) != "" {
		return true
	}
	return len(spec.MatchTools) > 0
}

// mapEventName is the shared canonical→vendor lookup that all per-platform
// *EventName functions delegate to. Per-platform tables live in package
// scope (claudeEventTable, codexEventTable, ...) and document which
// canonical HookSpec.When values that vendor supports plus the exact
// vendor-side event name string. Honors operator-supplied per-platform
// event overrides before consulting the table (D2 explicit-opt-in
// posture).
func mapEventName(spec HookSpec, platform string, table map[string]string) (string, bool) {
	if override := strings.TrimSpace(platformOverride(spec, platform).Event); override != "" {
		return override, true
	}
	v, ok := table[spec.When]
	return v, ok
}

// claudeEventTable encodes the canonical→Claude event mapping. Entries
// added in p1b (R6.4 post_compact) sit alongside the pre-existing surface.
// The "P1d wider-surface" block below brings the documented Claude Code
// lifecycle surface (verified against <https://code.claude.com/docs/en/hooks>
// on 2026-05-26 per the p1d-claude-lifecycle-parity contract) into canonical
// coverage so operator HookSpec entries can target these events without
// redefining per-platform overrides. Adding an entry here does NOT attach
// any gate: matcher narrowing, sentinel writes, and gate scripts remain a
// downstream concern (p2-hook-scripts and r1-5-* plans). The mapper exists
// only so a hook authored against the canonical name renders correctly.
var claudeEventTable = map[string]string{
	"pre_tool_use":          "PreToolUse",
	"post_tool_use":         "PostToolUse",
	"post_tool_use_failure": "PostToolUseFailure",
	"notification":          "Notification",
	"message_display":       "MessageDisplay",
	"user_prompt_submit":    "UserPromptSubmit",
	"session_start":         "SessionStart",
	"session_end":           "SessionEnd",
	"stop":                  "Stop",
	"subagent_start":        "SubagentStart",
	"subagent_stop":         "SubagentStop",
	"pre_compact":           "PreCompact",
	// P1b R6.4: post_compact is a new canonical When value introduced
	// alongside pre_compact; Claude documents PostCompact as the matching
	// terminal-of-compaction event.
	"post_compact":       "PostCompact",
	"permission_request": "PermissionRequest",
	// P1d wider-surface (R6.6 + DC9 + D2): documented Claude Code hooks
	// brought into canonical coverage without attaching new gates. Each
	// canonical snake_case key maps 1:1 to the vendor PascalCase event
	// name. Other platforms' tables omit these values today and fall
	// through (ok=false) per D2 — no semantic-equivalence inference.
	"setup":                 "Setup",
	"user_prompt_expansion": "UserPromptExpansion",
	"post_tool_batch":       "PostToolBatch",
	"permission_denied":     "PermissionDenied",
	"stop_failure":          "StopFailure",
	"teammate_idle":         "TeammateIdle",
	"task_created":          "TaskCreated",
	"task_completed":        "TaskCompleted",
	"worktree_create":       "WorktreeCreate",
	"worktree_remove":       "WorktreeRemove",
	"file_changed":          "FileChanged",
	"config_change":         "ConfigChange",
	"cwd_changed":           "CwdChanged",
	"instructions_loaded":   "InstructionsLoaded",
	"elicitation":           "Elicitation",
	"elicitation_result":    "ElicitationResult",
}

// codexEventTable encodes the canonical→Codex event mapping. Entries
// without comments are documented vendor events; commented entries note
// the spec rule that pulled them in.
var codexEventTable = map[string]string{
	"session_start":      "SessionStart",
	"pre_tool_use":       "PreToolUse",
	"post_tool_use":      "PostToolUse",
	"user_prompt_submit": "UserPromptSubmit",
	"stop":               "Stop",
	// P1a gate-critical: Codex documents SubagentStop as a distinct
	// terminal event for subagent runs.
	"subagent_stop": "SubagentStop",
	// P1b R6.1: Codex parity additions paired with the existing surface.
	"subagent_start":     "SubagentStart",
	"pre_compact":        "PreCompact",
	"post_compact":       "PostCompact",
	"permission_request": "PermissionRequest",
}

// cursorEventTable encodes the canonical→Cursor mapping. Cursor exposes
// the widest surface (D3 + R6.3); the "wider-surface" entries are
// canonical When values that only Cursor implements today — other
// platforms' tables omit them and fall through to ok=false.
var cursorEventTable = map[string]string{
	"pre_tool_use": "preToolUse",
	// P1b R6.3: Cursor exposes postToolUse / postToolUseFailure as
	// observation inputs per D9, not implicit gates.
	"post_tool_use":         "postToolUse",
	"post_tool_use_failure": "postToolUseFailure",
	"user_prompt_submit":    "beforeSubmitPrompt",
	"stop":                  "stop",
	"session_start":         "sessionStart",
	// P1b R6.3: Cursor exposes sessionEnd as a terminal session event.
	"session_end": "sessionEnd",
	// P1a gate-critical: Cursor exposes subagentStop as a sibling
	// terminal event to `stop`.
	"subagent_stop": "subagentStop",
	// P1b R6.3: subagentStart / preCompact added for delegated-worker
	// bootstrap and continuity-advice parity.
	"subagent_start": "subagentStart",
	"pre_compact":    "preCompact",
	// Cursor-wider surface (D3 + R6.3): fine-grained events promoted to
	// canonical HookSpec.When values even though only Cursor implements
	// them today. Other platform mappers no-op for these values until
	// vendors document equivalents.
	"before_shell_execution": "beforeShellExecution",
	"after_shell_execution":  "afterShellExecution",
	"before_mcp_execution":   "beforeMCPExecution",
	"after_mcp_execution":    "afterMCPExecution",
	"before_read_file":       "beforeReadFile",
	"after_file_edit":        "afterFileEdit",
	"after_agent_response":   "afterAgentResponse",
	"after_agent_thought":    "afterAgentThought",
	"workspace_open":         "workspaceOpen",
	"before_tab_file_read":   "beforeTabFileRead",
	"after_tab_file_edit":    "afterTabFileEdit",
}

// copilotEventTable encodes the canonical→Copilot mapping. The `stop`
// entry maps to `agentStop` per Copilot's vendor docs — see comment on
// the entry for the gate-critical note.
var copilotEventTable = map[string]string{
	"session_start": "sessionStart",
	// P1b R6.2: Copilot exposes sessionEnd as the terminal session event.
	"session_end":        "sessionEnd",
	"user_prompt_submit": "userPromptSubmitted",
	"pre_tool_use":       "preToolUse",
	// P1b R6.2: Copilot exposes postToolUse / postToolUseFailure as
	// observation inputs per D9.
	"post_tool_use":         "postToolUse",
	"post_tool_use_failure": "postToolUseFailure",
	// P1b R6.2: Copilot surface additions.
	"notification":       "notification",
	"permission_request": "permissionRequest",
	"pre_compact":        "preCompact",
	// P1a gate-critical: GitHub Copilot's terminal event for the
	// top-level agent is `agentStop`, NOT `stop`. The Claude/Cursor
	// `stop` name does not exist in Copilot's event surface.
	"stop":           "agentStop",
	"subagent_stop":  "subagentStop",
	"subagent_start": "subagentStart",
	// P1b R6.2 + R6.4: Copilot exposes errorOccurred as a runtime error
	// notification event; error_occurred is a new canonical When value
	// introduced in this task.
	"error_occurred": "errorOccurred",
}

func claudeEventName(spec HookSpec) (string, bool) {
	return mapEventName(spec, "claude", claudeEventTable)
}

func codexEventName(spec HookSpec) (string, bool) {
	return mapEventName(spec, "codex", codexEventTable)
}

func cursorEventName(spec HookSpec) (string, bool) {
	return mapEventName(spec, "cursor", cursorEventTable)
}

func copilotEventName(spec HookSpec) (string, bool) {
	return mapEventName(spec, "copilot", copilotEventTable)
}

func renderClaudeHookSettings(specs []HookSpec) ([]byte, error) {
	out := claudeRenderedHooks{
		Schema: "https://json.schemastore.org/claude-code-settings.json",
		Hooks:  map[string][]claudeRenderedEntry{},
	}
	for _, parent := range specs {
		for _, spec := range expandHookSpecEvents(parent) {
			event, entry, include, err := renderClaudeHookEntry(spec)
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

// renderClaudeHookEntry resolves the per-spec event mapping + command
// resolution for Claude. Returns include=false when the spec falls
// through (not RequiredOn claude); returns a non-nil error only when
// the spec IS RequiredOn claude but cannot be represented or has no
// command. Extracted from renderClaudeHookSettings to keep the outer
// fanout loop under Sonar's cog-complexity gate.
func renderClaudeHookEntry(spec HookSpec) (string, claudeRenderedEntry, bool, error) {
	event, ok := claudeEventName(spec)
	if !ok {
		if hookRequiredOnPlatform(spec, "claude") {
			return "", claudeRenderedEntry{}, false, fmt.Errorf("hook %q is not representable for claude event %q", spec.Name, spec.When)
		}
		return "", claudeRenderedEntry{}, false, nil
	}
	command := ResolveHookCommand(spec)
	if command == "" {
		if hookRequiredOnPlatform(spec, "claude") {
			return "", claudeRenderedEntry{}, false, fmt.Errorf("hook %q has no command for claude", spec.Name)
		}
		return "", claudeRenderedEntry{}, false, nil
	}
	return event, claudeRenderedEntry{
		Matcher: matcherForSpec(spec, "claude", "*"),
		Hooks: []claudeRenderedAction{{
			Type:    "command",
			Command: command,
		}},
	}, true, nil
}

// codexMatcherWhitelist enumerates the Codex events whose official
// documentation establishes matcher narrowing. P1c verification reviewed
// the Codex hooks reference (https://developers.openai.com/codex/hooks);
// the surface is broader than the original P1b-deferred trio per the docs:
//
//	Event             What matcher filters       Notes
//	PermissionRequest tool name                   Bash, apply_patch, MCP tool names
//	PostToolUse       tool name                   Bash, apply_patch, MCP tool names
//	PostCompact       compaction trigger          manual | auto
//	PreCompact        compaction trigger          manual | auto
//	PreToolUse        tool name                   Bash, apply_patch, MCP tool names
//	SessionStart      start source                startup | resume | clear | compact
//	SubagentStart     subagent type               depends on subagent
//	SubagentStop      subagent type               depends on subagent
//
// UserPromptSubmit and Stop explicitly do NOT support matcher — any value
// is ignored by Codex. Events outside this map render with matcher="" per
// the contract's "Matcher Boundary" rule so gate scripts parse the
// vendor-provided input rather than rely on matcher narrowing.
var codexMatcherWhitelist = map[string]bool{
	"PermissionRequest": true,
	"PostCompact":       true,
	"PostToolUse":       true,
	"PreCompact":        true,
	"PreToolUse":        true,
	"SessionStart":      true,
	"SubagentStart":     true,
	"SubagentStop":      true,
}

func renderCodexHookConfig(specs []HookSpec) ([]byte, error) {
	out := codexRenderedHooks{Hooks: map[string][]claudeRenderedEntry{}}
	for _, parent := range specs {
		for _, spec := range expandHookSpecEvents(parent) {
			event, entry, include, err := renderCodexHookEntry(spec)
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

// renderCodexHookEntry mirrors renderClaudeHookEntry for Codex, with one
// extra step: matcher narrowing is only emitted for events in
// codexMatcherWhitelist (per P1c verification of the Codex hooks
// reference). Other events render with matcher="" so gate scripts parse
// the vendor-provided input instead.
func renderCodexHookEntry(spec HookSpec) (string, claudeRenderedEntry, bool, error) {
	event, ok := codexEventName(spec)
	if !ok {
		if hookRequiredOnPlatform(spec, "codex") {
			return "", claudeRenderedEntry{}, false, fmt.Errorf("hook %q is not representable for codex event %q", spec.Name, spec.When)
		}
		return "", claudeRenderedEntry{}, false, nil
	}
	command := ResolveHookCommand(spec)
	if command == "" {
		if hookRequiredOnPlatform(spec, "codex") {
			return "", claudeRenderedEntry{}, false, fmt.Errorf("hook %q has no command for codex", spec.Name)
		}
		return "", claudeRenderedEntry{}, false, nil
	}
	matcher := ""
	if codexMatcherWhitelist[event] {
		matcher = matcherForSpec(spec, "codex", "*")
	}
	return event, claudeRenderedEntry{
		Matcher: matcher,
		Hooks: []claudeRenderedAction{{
			Type:    "command",
			Command: command,
		}},
	}, true, nil
}

func renderCursorHookConfig(specs []HookSpec) ([]byte, error) {
	out := cursorRenderedHooks{
		Version: 1,
		Hooks:   map[string][]cursorRenderedEntry{},
	}
	for _, parent := range specs {
		for _, spec := range expandHookSpecEvents(parent) {
			event, entry, include, err := renderCursorHookEntry(spec)
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

func renderCursorHookEntry(spec HookSpec) (string, cursorRenderedEntry, bool, error) {
	event, ok := cursorEventName(spec)
	if !ok {
		if hookRequiredOnPlatform(spec, "cursor") {
			return "", cursorRenderedEntry{}, false, fmt.Errorf("hook %q is not representable for cursor event %q", spec.Name, spec.When)
		}
		return "", cursorRenderedEntry{}, false, nil
	}
	command := ResolveHookCommand(spec)
	if command == "" {
		if hookRequiredOnPlatform(spec, "cursor") {
			return "", cursorRenderedEntry{}, false, fmt.Errorf("hook %q has no command for cursor", spec.Name)
		}
		return "", cursorRenderedEntry{}, false, nil
	}
	entry := cursorRenderedEntry{
		Command: command,
		Matcher: matcherForSpec(spec, "cursor", ""),
	}
	if spec.TimeoutMS > 0 {
		entry.Timeout = spec.TimeoutMS / 1000
		if entry.Timeout == 0 {
			entry.Timeout = 1
		}
	}
	return event, entry, true, nil
}

func renderCopilotHookFile(spec HookSpec) (string, []byte, bool, error) {
	event, ok := copilotEventName(spec)
	if !ok {
		if hookRequiredOnPlatform(spec, "copilot") {
			return "", nil, false, fmt.Errorf("hook %q is not representable for copilot event %q", spec.Name, spec.When)
		}
		return "", nil, false, nil
	}
	if specHasMatcher(spec) {
		if hookRequiredOnPlatform(spec, "copilot") {
			return "", nil, false, fmt.Errorf("hook %q uses tool matchers unsupported by copilot", spec.Name)
		}
		return "", nil, false, nil
	}
	command := ResolveHookCommand(spec)
	if command == "" {
		if hookRequiredOnPlatform(spec, "copilot") {
			return "", nil, false, fmt.Errorf("hook %q has no command for copilot", spec.Name)
		}
		return "", nil, false, nil
	}
	fileName := strings.TrimSpace(platformOverride(spec, "copilot").File)
	if fileName == "" {
		fileName = spec.Name + ".json"
	}
	out := copilotRenderedHooks{
		Version: 1,
		Hooks: map[string][]copilotRenderedAction{
			event: {{
				Type: "command",
				Bash: command,
			}},
		},
	}
	if spec.TimeoutMS > 0 {
		out.Hooks[event][0].TimeoutSec = spec.TimeoutMS / 1000
		if out.Hooks[event][0].TimeoutSec == 0 {
			out.Hooks[event][0].TimeoutSec = 1
		}
	}
	content, err := marshalJSON(out)
	if err != nil {
		return "", nil, false, err
	}
	return fileName, content, true, nil
}

func marshalJSON(v any) ([]byte, error) {
	content, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(content, '\n'), nil
}

func writeManagedFile(io platformIO, dst string, content []byte) error {
	newHash := renderContentHash(content)
	existing, readErr := os.ReadFile(dst)
	switch {
	case readErr == nil:
		if bytes.Equal(existing, content) {
			recordRenderHash(io, dst, newHash) // heal/ensure provenance
			return nil
		}
		// Divergent existing file. It is ONLY safe to clobber if it is
		// provably what we last rendered (hash matches the manifest).
		// Otherwise it is a user edit (or unknown provenance) and must
		// be preserved before we overwrite — never silently lost.
		if renderManifestHash(dst) != renderContentHash(existing) {
			if bErr := BackupBeforeOverwrite(dst); bErr != nil {
				return fmt.Errorf("preserving user-modified managed file %s before refresh: %w", dst, bErr)
			}
		}
	case os.IsNotExist(readErr):
		// No existing destination: safe to render fresh.
	default:
		// The destination exists but is unreadable (e.g. perms). Its
		// bytes could be an unsaved user edit we cannot compare or back
		// up. Removing/overwriting now would destroy it silently while
		// reporting success, so this MUST block instead of falling
		// through to the remove/write path.
		return fmt.Errorf("reading existing managed file %s before refresh: %w", dst, readErr)
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := io.Remove(dst); err != nil {
			return err
		}
	}
	if err := io.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := io.WriteFile(dst, content, 0644); err != nil {
		return err
	}
	recordRenderHash(io, dst, newHash)
	return nil
}

func removeManagedFile(io platformIO, dst string, content []byte) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// Never delete a managed link (POSIX symlink / Windows junction or
	// hard link) or a non-regular entry — only a plain file we rendered.
	// A Windows managed file link is a hard link with no reparse point, so
	// a raw ModeSymlink check would miss it and wrongly delete the link.
	if links.IsManagedFileLink(dst) || !info.Mode().IsRegular() {
		return nil
	}
	existing, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, content) {
		return nil
	}
	if err := io.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeDirIfEmpty(filepath.Dir(dst))
}

func removeDirIfEmpty(path string) error {
	if path == "" {
		return nil
	}
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeManagedFileIf(io platformIO, dst string, matches func([]byte) bool) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// See removeManagedFile: preserve any managed link (incl. a Windows
	// hard-linked managed file with no reparse point); only a plain
	// rendered file is eligible for removal.
	if links.IsManagedFileLink(dst) || !info.Mode().IsRegular() {
		return nil
	}
	content, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	if !matches(content) {
		return nil
	}
	if err := io.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeDirIfEmpty(filepath.Dir(dst))
}

func pruneManagedRenderedFanoutExtras(io platformIO, dstRoot string, wanted map[string]bool, matches func([]byte) bool) error {
	entries, err := os.ReadDir(dstRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || wanted[entry.Name()] {
			continue
		}
		if err := removeManagedFileIf(io, filepath.Join(dstRoot, entry.Name()), matches); err != nil {
			return err
		}
	}
	return removeDirIfEmpty(dstRoot)
}

func isLikelyRenderedClaudeHookSettings(content []byte) bool {
	var payload claudeRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		return false
	}
	return len(payload.Hooks) > 0
}

func isLikelyRenderedCodexHookConfig(content []byte) bool {
	var payload codexRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		return false
	}
	return len(payload.Hooks) > 0
}

func isLikelyRenderedCursorHookConfig(content []byte) bool {
	var payload cursorRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		return false
	}
	return payload.Version == 1 && len(payload.Hooks) > 0
}

func isLikelyRenderedCopilotHookFile(content []byte) bool {
	var payload copilotRenderedHooks
	if err := json.Unmarshal(content, &payload); err != nil {
		return false
	}
	return payload.Version == 1 && len(payload.Hooks) > 0
}
