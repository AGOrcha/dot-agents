package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
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
	Name              string
	Scope             string
	SourcePath        string
	SourceBucket      string
	SourceKind        HookSourceKind
	Description       string
	When              string
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

func resolveHookSpec(agentsHome string, buckets []string, project string, names ...string) *HookSpec {
	return resolveHookSpecInScopes(agentsHome, buckets, scopedNames(project), names...)
}

func resolveHookSpecInScope(agentsHome string, buckets []string, scope string, names ...string) *HookSpec {
	return resolveHookSpecInScopes(agentsHome, buckets, []string{scope}, names...)
}

func resolveHookSpecInScopes(agentsHome string, buckets []string, scopes []string, names ...string) *HookSpec {
	for _, scope := range scopes {
		for _, bucket := range buckets {
			for _, name := range names {
				src := filepath.Join(agentsHome, bucket, scope, name)
				if _, err := os.Stat(src); err == nil {
					return &HookSpec{
						Name:         strings.TrimSuffix(name, filepath.Ext(name)),
						Scope:        scope,
						SourcePath:   src,
						SourceBucket: bucket,
						SourceKind:   HookSourceLegacyFile,
					}
				}
			}
		}
	}
	return nil
}

// ListHookSpecs returns hook entries under ~/.agents/hooks/<scope>/: canonical bundles
// (…/<name>/HOOK.yaml) and legacy single-file JSON hooks. The hooks directory must exist;
// if it is missing, ReadDir fails with an error satisfying os.IsNotExist.
func ListHookSpecs(agentsHome, scope string) ([]HookSpec, error) {
	root := filepath.Join(agentsHome, "hooks", scope)
	entries, err := os.ReadDir(root)
	if err != nil {
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
	for _, spec := range specs {
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
	return nil
}

func removeManagedRenderedHookFanout(io platformIO, specs []HookSpec, dstRoot string, render func(HookSpec) (string, []byte, bool, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, spec := range specs {
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
	return removeDirIfEmpty(dstRoot)
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

	spec := HookSpec{
		Name:              strings.TrimSpace(manifest.Name),
		Scope:             scope,
		SourcePath:        manifestPath,
		SourceBucket:      "hooks",
		SourceKind:        HookSourceCanonicalBundle,
		Description:       strings.TrimSpace(manifest.Description),
		When:              strings.TrimSpace(manifest.When),
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
	if strings.HasPrefix(first, "./") || strings.HasPrefix(first, "../") {
		parts[0] = filepath.Clean(filepath.Join(filepath.Dir(spec.SourcePath), first))
		return strings.Join(parts, " ")
	}
	return command
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

func claudeEventName(spec HookSpec) (string, bool) {
	if override := strings.TrimSpace(platformOverride(spec, "claude").Event); override != "" {
		return override, true
	}
	switch spec.When {
	case "pre_tool_use":
		return "PreToolUse", true
	case "post_tool_use":
		return "PostToolUse", true
	case "post_tool_use_failure":
		return "PostToolUseFailure", true
	case "notification":
		return "Notification", true
	case "user_prompt_submit":
		return "UserPromptSubmit", true
	case "session_start":
		return "SessionStart", true
	case "session_end":
		return "SessionEnd", true
	case "stop":
		return "Stop", true
	case "subagent_start":
		return "SubagentStart", true
	case "subagent_stop":
		return "SubagentStop", true
	case "pre_compact":
		return "PreCompact", true
	case "post_compact":
		// P1b R6.4: post_compact is a new canonical When value introduced
		// alongside pre_compact; Claude documents PostCompact as the
		// matching terminal-of-compaction event.
		return "PostCompact", true
	case "permission_request":
		return "PermissionRequest", true
	default:
		return "", false
	}
}

func codexEventName(spec HookSpec) (string, bool) {
	if override := strings.TrimSpace(platformOverride(spec, "codex").Event); override != "" {
		return override, true
	}
	switch spec.When {
	case "session_start":
		return "SessionStart", true
	case "pre_tool_use":
		return "PreToolUse", true
	case "post_tool_use":
		return "PostToolUse", true
	case "user_prompt_submit":
		return "UserPromptSubmit", true
	case "stop":
		return "Stop", true
	case "subagent_stop":
		// P1a gate-critical: Codex documents SubagentStop as a distinct
		// terminal event for subagent runs.
		return "SubagentStop", true
	case "subagent_start":
		// P1b R6.1: Codex documents SubagentStart for forward parity with
		// the SubagentStop terminal event.
		return "SubagentStart", true
	case "pre_compact":
		// P1b R6.1: Codex documents PreCompact for compaction lifecycle.
		return "PreCompact", true
	case "post_compact":
		// P1b R6.1: Codex documents PostCompact paired with PreCompact.
		return "PostCompact", true
	case "permission_request":
		// P1b R6.1: Codex documents PermissionRequest for permission
		// elicitation events.
		return "PermissionRequest", true
	default:
		return "", false
	}
}

func cursorEventName(spec HookSpec) (string, bool) {
	if override := strings.TrimSpace(platformOverride(spec, "cursor").Event); override != "" {
		return override, true
	}
	switch spec.When {
	case "pre_tool_use":
		return "preToolUse", true
	case "post_tool_use":
		// P1b R6.3: Cursor exposes postToolUse; treated as observation
		// input per D9, not an implicit gate.
		return "postToolUse", true
	case "post_tool_use_failure":
		// P1b R6.3: Cursor exposes postToolUseFailure; observation input
		// per D9.
		return "postToolUseFailure", true
	case "user_prompt_submit":
		return "beforeSubmitPrompt", true
	case "stop":
		return "stop", true
	case "session_start":
		return "sessionStart", true
	case "session_end":
		// P1b R6.3: Cursor exposes sessionEnd as a terminal session event.
		return "sessionEnd", true
	case "subagent_stop":
		// P1a gate-critical: Cursor exposes subagentStop as a sibling
		// terminal event to `stop`.
		return "subagentStop", true
	case "subagent_start":
		// P1b R6.3: Cursor exposes subagentStart for delegated worker
		// lifecycle bootstrap.
		return "subagentStart", true
	case "pre_compact":
		// P1b R6.3: Cursor exposes preCompact for continuity advice
		// before context compaction.
		return "preCompact", true
	// Cursor-wider surface (D3 + R6.3): fine-grained events promoted to
	// canonical HookSpec.When values even though only Cursor implements
	// them today. Other platform mappers no-op for these values until
	// vendors document equivalents.
	case "before_shell_execution":
		return "beforeShellExecution", true
	case "after_shell_execution":
		return "afterShellExecution", true
	case "before_mcp_execution":
		return "beforeMCPExecution", true
	case "after_mcp_execution":
		return "afterMCPExecution", true
	case "before_read_file":
		return "beforeReadFile", true
	case "after_file_edit":
		return "afterFileEdit", true
	case "after_agent_response":
		return "afterAgentResponse", true
	case "after_agent_thought":
		return "afterAgentThought", true
	case "workspace_open":
		return "workspaceOpen", true
	case "before_tab_file_read":
		return "beforeTabFileRead", true
	case "after_tab_file_edit":
		return "afterTabFileEdit", true
	default:
		return "", false
	}
}

func copilotEventName(spec HookSpec) (string, bool) {
	if override := strings.TrimSpace(platformOverride(spec, "copilot").Event); override != "" {
		return override, true
	}
	switch spec.When {
	case "session_start":
		return "sessionStart", true
	case "session_end":
		// P1b R6.2: Copilot exposes sessionEnd as the terminal session
		// event.
		return "sessionEnd", true
	case "user_prompt_submit":
		return "userPromptSubmitted", true
	case "pre_tool_use":
		return "preToolUse", true
	case "post_tool_use":
		// P1b R6.2: Copilot exposes postToolUse; observation input per D9.
		return "postToolUse", true
	case "post_tool_use_failure":
		// P1b R6.2: Copilot exposes postToolUseFailure; observation input
		// per D9.
		return "postToolUseFailure", true
	case "notification":
		// P1b R6.2: Copilot exposes notification events.
		return "notification", true
	case "permission_request":
		// P1b R6.2: Copilot exposes permissionRequest for permission
		// elicitation.
		return "permissionRequest", true
	case "pre_compact":
		// P1b R6.2: Copilot exposes preCompact for continuity advice
		// before context compaction.
		return "preCompact", true
	case "stop":
		// P1a gate-critical: GitHub Copilot's terminal event for the
		// top-level agent is `agentStop`, NOT `stop`. The Claude/Cursor
		// `stop` name does not exist in Copilot's event surface.
		return "agentStop", true
	case "subagent_stop":
		return "subagentStop", true
	case "subagent_start":
		// P1b R6.2: Copilot exposes subagentStart for delegated worker
		// lifecycle bootstrap.
		return "subagentStart", true
	case "error_occurred":
		// P1b R6.2 + R6.4: Copilot exposes errorOccurred as a runtime
		// error notification event; error_occurred is a new canonical
		// When value introduced in this task.
		return "errorOccurred", true
	default:
		return "", false
	}
}

func renderClaudeHookSettings(specs []HookSpec) ([]byte, error) {
	out := claudeRenderedHooks{
		Schema: "https://json.schemastore.org/claude-code-settings.json",
		Hooks:  map[string][]claudeRenderedEntry{},
	}
	for _, spec := range specs {
		event, ok := claudeEventName(spec)
		if !ok {
			if hookRequiredOnPlatform(spec, "claude") {
				return nil, fmt.Errorf("hook %q is not representable for claude event %q", spec.Name, spec.When)
			}
			continue
		}
		command := ResolveHookCommand(spec)
		if command == "" {
			if hookRequiredOnPlatform(spec, "claude") {
				return nil, fmt.Errorf("hook %q has no command for claude", spec.Name)
			}
			continue
		}
		out.Hooks[event] = append(out.Hooks[event], claudeRenderedEntry{
			Matcher: matcherForSpec(spec, "claude", "*"),
			Hooks: []claudeRenderedAction{{
				Type:    "command",
				Command: command,
			}},
		})
	}
	return marshalJSON(out)
}

func renderCodexHookConfig(specs []HookSpec) ([]byte, error) {
	out := codexRenderedHooks{Hooks: map[string][]claudeRenderedEntry{}}
	for _, spec := range specs {
		event, ok := codexEventName(spec)
		if !ok {
			if hookRequiredOnPlatform(spec, "codex") {
				return nil, fmt.Errorf("hook %q is not representable for codex event %q", spec.Name, spec.When)
			}
			continue
		}
		command := ResolveHookCommand(spec)
		if command == "" {
			if hookRequiredOnPlatform(spec, "codex") {
				return nil, fmt.Errorf("hook %q has no command for codex", spec.Name)
			}
			continue
		}
		matcher := ""
		switch event {
		case "SessionStart", "PreToolUse", "PostToolUse":
			matcher = matcherForSpec(spec, "codex", "*")
		}
		out.Hooks[event] = append(out.Hooks[event], claudeRenderedEntry{
			Matcher: matcher,
			Hooks: []claudeRenderedAction{{
				Type:    "command",
				Command: command,
			}},
		})
	}
	return marshalJSON(out)
}

func renderCursorHookConfig(specs []HookSpec) ([]byte, error) {
	out := cursorRenderedHooks{
		Version: 1,
		Hooks:   map[string][]cursorRenderedEntry{},
	}
	for _, spec := range specs {
		event, entry, include, err := renderCursorHookEntry(spec)
		if err != nil {
			return nil, err
		}
		if !include {
			continue
		}
		out.Hooks[event] = append(out.Hooks[event], entry)
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
