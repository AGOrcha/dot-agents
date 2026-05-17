package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

func runHooksList(scope string) error {
	agentsHome := config.AgentsHome()
	specs, err := platform.ListHookSpecs(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			return listHooksLegacyClaudeSettings(scope)
		}
		return err
	}
	if len(specs) == 0 {
		return listHooksLegacyClaudeSettings(scope)
	}
	return printHookSpecsList(specs, scope)
}

func printHookSpecsList(specs []platform.HookSpec, scope string) error {
	ui.Header("Hooks (" + scope + ")")
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(os.Stdout, "\n  %s%s%s  %s(%s)%s\n", ui.Cyan, name, ui.Reset, ui.Dim, hookKindLabel(spec.SourceKind), ui.Reset)
		if spec.Description != "" {
			fmt.Fprintf(os.Stdout, "    %sdescription:%s %s\n", ui.Dim, ui.Reset, spec.Description)
		}
		if spec.When != "" {
			fmt.Fprintf(os.Stdout, "    %swhen:%s %s\n", ui.Dim, ui.Reset, spec.When)
		}
		if len(spec.EnabledOn) > 0 {
			fmt.Fprintf(os.Stdout, "    %senabled_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.EnabledOn, ", "))
		}
		cmd := strings.TrimSpace(platform.ResolveHookCommand(spec))
		if cmd != "" {
			fmt.Fprintf(os.Stdout, "    %scommand:%s %s\n", ui.Dim, ui.Reset, cmd)
		}
		showPath := spec.SourcePath
		if spec.SourceKind == platform.HookSourceCanonicalBundle {
			showPath = filepath.Dir(spec.SourcePath)
		}
		fmt.Fprintf(os.Stdout, "    %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(showPath))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func listHooksLegacyClaudeSettings(scope string) error {
	settings, ok, err := loadLegacyClaudeSettings(scope)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	hooksVal, ok := settings["hooks"]
	if !ok {
		ui.Info("No hooks configured in " + scope + "/claude-code.json")
		return nil
	}

	ui.Header("Hooks (" + scope + ") — legacy settings projection")

	hooksMap, ok := hooksVal.(map[string]any)
	if !ok {
		hooksJSON, _ := json.MarshalIndent(hooksVal, "  ", "  ")
		fmt.Fprintf(os.Stdout, "  %s\n\n", string(hooksJSON))
		return nil
	}

	if len(hooksMap) == 0 {
		ui.Info("No hook events defined.")
	}
	for event, val := range hooksMap {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, event, ui.Reset)
		printLegacyHookList(asAnySlice(val))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// loadLegacyClaudeSettings reads ~/.agents/settings/<scope>/claude-code.json,
// returning (settings, true, nil) when present, (nil, false, nil) when the
// file does not exist (with an info message printed), or (nil, false, err)
// when read/parse fails.
func loadLegacyClaudeSettings(scope string) (map[string]any, bool, error) {
	settingsPath := filepath.Join(config.AgentsHome(), "settings", scope, "claude-code.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No hooks under ~/.agents/hooks/" + scope + "/ and no " + scope + "/claude-code.json hook settings found.")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading claude-code.json: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, false, fmt.Errorf("parsing claude-code.json: %w", err)
	}
	return settings, true, nil
}

// asAnySlice returns v as []any, wrapping a single value when v is not
// already a slice. Used to normalize hook entries that may be either a
// single object or an array.
func asAnySlice(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return []any{v}
}

// printLegacyHookList renders the hook entries under a single event name.
func printLegacyHookList(hookList []any) {
	for _, h := range hookList {
		hookObj, ok := h.(map[string]any)
		if !ok {
			fmt.Fprintf(os.Stdout, "    %s%v%s\n", ui.Dim, h, ui.Reset)
			continue
		}
		printLegacyHookObject(hookObj)
	}
}

// printLegacyHookObject renders a single legacy hook object, including its
// matcher line and either the nested hook commands array or the inline
// command/raw fallback.
func printLegacyHookObject(hookObj map[string]any) {
	if matcher, _ := hookObj["matcher"].(string); matcher != "" {
		fmt.Fprintf(os.Stdout, "    matcher: %s%s%s\n", ui.Bold, matcher, ui.Reset)
	}
	if cmds, ok := hookObj["hooks"].([]any); ok {
		for _, c := range cmds {
			printLegacyHookCommand(c)
		}
		return
	}
	if cmd, ok := hookObj["command"].(string); ok {
		fmt.Fprintf(os.Stdout, "    %scommand:%s %s%s%s\n", ui.Dim, ui.Reset, ui.Dim, cmd, ui.Reset)
		return
	}
	raw, _ := json.MarshalIndent(hookObj, "    ", "  ")
	fmt.Fprintf(os.Stdout, "    %s%s%s\n", ui.Dim, string(raw), ui.Reset)
}

// printLegacyHookCommand renders a single command entry inside a legacy hook
// object's `hooks` array.
func printLegacyHookCommand(c any) {
	cmdObj, ok := c.(map[string]any)
	if !ok {
		fmt.Fprintf(os.Stdout, "    %s%v%s\n", ui.Dim, c, ui.Reset)
		return
	}
	cmdType, _ := cmdObj["type"].(string)
	cmdVal, _ := cmdObj["command"].(string)
	if cmdVal == "" {
		cmdVal, _ = cmdObj["cmd"].(string)
	}
	label := cmdType
	if label == "" {
		label = "command"
	}
	fmt.Fprintf(os.Stdout, "    %s%s:%s %s%s%s\n", ui.Dim, label, ui.Reset, ui.Dim, cmdVal, ui.Reset)
}
