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

func listHookSpecs(agentsHome, scope string) ([]HookSpec, error) {
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
	specs, err := listHookSpecs(agentsHome, scope)
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

func emitHookSpec(spec *HookSpec, dst string, mode HookEmissionMode) error {
	if spec == nil {
		return nil
	}
	switch mode.Shape {
	case HookShapeDirect:
		return emitHookFile(spec.SourcePath, dst, mode.Transport)
	case HookShapeRenderSingle, HookShapeRenderFanout:
		return fmt.Errorf("hook emission shape %q is not supported for single direct emission", mode.Shape)
	default:
		return fmt.Errorf("unknown hook emission shape %q", mode.Shape)
	}
}

func emitHookSpecToUserHomes(spec *HookSpec, relativePath string, mode HookEmissionMode) error {
	if spec == nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := emitHookSpec(spec, filepath.Join(homeRoot, relativePath), mode); err != nil {
			return err
		}
	}
	return nil
}

func emitHookFanout(specs []HookSpec, dstRoot string, mode HookEmissionMode, mapName func(HookSpec) (string, bool)) error {
	if mode.Shape != HookShapeRenderFanout {
		return fmt.Errorf("hook fanout requires %q shape, got %q", HookShapeRenderFanout, mode.Shape)
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, spec := range specs {
		name, ok := mapName(spec)
		if !ok {
			continue
		}
		if err := emitHookFile(spec.SourcePath, filepath.Join(dstRoot, name), mode.Transport); err != nil {
			return err
		}
	}
	return nil
}

func emitRenderedHookFile(specs []HookSpec, dst string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	content, err := render(specs)
	if err != nil {
		return err
	}
	return writeManagedFile(dst, content)
}

func emitRenderedHookFileToUserHomes(specs []HookSpec, relativePath string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := emitRenderedHookFile(specs, filepath.Join(homeRoot, relativePath), render); err != nil {
			return err
		}
	}
	return nil
}

func emitPreferredHookFile(
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
		return emitRenderedHookFile(specs, dst, render)
	}
	if legacy != nil {
		return emitHookSpec(legacy, dst, mode)
	}
	if removeRendered != nil {
		return removeRendered(dst)
	}
	return nil
}

func emitPreferredHookFileToUserHomes(
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
		return emitRenderedHookFileToUserHomes(specs, relativePath, render)
	}
	if legacy != nil {
		return emitHookSpecToUserHomes(legacy, relativePath, mode)
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

func removeManagedRenderedHookFile(specs []HookSpec, dst string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	content, err := render(specs)
	if err != nil {
		return err
	}
	return removeManagedFile(dst, content)
}

func removeManagedRenderedHookFileToUserHomes(specs []HookSpec, relativePath string, render func([]HookSpec) ([]byte, error)) error {
	if len(specs) == 0 {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		if err := removeManagedRenderedHookFile(specs, filepath.Join(homeRoot, relativePath), render); err != nil {
			return err
		}
	}
	return nil
}

func emitRenderedHookFanout(specs []HookSpec, dstRoot string, render func(HookSpec) (string, []byte, bool, error)) error {
	if len(specs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
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
		if err := writeManagedFile(filepath.Join(dstRoot, name), content); err != nil {
			return err
		}
	}
	return nil
}

func removeManagedRenderedHookFanout(specs []HookSpec, dstRoot string, render func(HookSpec) (string, []byte, bool, error)) error {
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
		if err := removeManagedFile(filepath.Join(dstRoot, name), content); err != nil {
			return err
		}
	}
	return removeDirIfEmpty(dstRoot)
}

func emitHookFile(src, dst string, transport HookTransport) error {
	switch transport {
	case HookTransportSymlink:
		return links.Symlink(src, dst)
	case HookTransportHardlink:
		return links.Hardlink(src, dst)
	case HookTransportWrite:
		content, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return writeManagedFile(dst, content)
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

func resolveHookCommand(spec HookSpec) string {
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
	case "user_prompt_submit":
		return "beforeSubmitPrompt", true
	case "stop":
		return "stop", true
	case "session_start":
		return "sessionStart", true
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
	case "user_prompt_submit":
		return "userPromptSubmitted", true
	case "pre_tool_use":
		return "preToolUse", true
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
		command := resolveHookCommand(spec)
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
		command := resolveHookCommand(spec)
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
	command := resolveHookCommand(spec)
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
	command := resolveHookCommand(spec)
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

func writeManagedFile(dst string, content []byte) error {
	if existing, err := os.ReadFile(dst); err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Remove(dst); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, content, 0644)
}

func removeManagedFile(dst string, content []byte) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	existing, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, content) {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
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

func removeManagedFileIf(dst string, matches func([]byte) bool) error {
	info, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	content, err := os.ReadFile(dst)
	if err != nil {
		return err
	}
	if !matches(content) {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return removeDirIfEmpty(filepath.Dir(dst))
}

func pruneManagedRenderedFanoutExtras(dstRoot string, wanted map[string]bool, matches func([]byte) bool) error {
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
		if err := removeManagedFileIf(filepath.Join(dstRoot, entry.Name()), matches); err != nil {
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
