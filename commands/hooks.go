package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func NewHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage ~/.agents/settings/*/claude-code.json hooks",
	}
	cmd.AddCommand(newHooksListCmd())
	return cmd
}

func newHooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List configured hooks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return listHooks(scope)
		},
	}
}

func listHooks(scope string) error {
	agentsHome := config.AgentsHome()
	if listed, err := listCanonicalHooks(filepath.Join(agentsHome, "hooks", scope), scope); err != nil {
		return err
	} else if listed {
		return nil
	}
	settingsPath := filepath.Join(agentsHome, "settings", scope, "claude-code.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		ui.Info("No claude-code.json found for scope: " + scope)
		return nil
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing claude-code.json: %w", err)
	}

	hooks, ok := settings["hooks"]
	if !ok {
		ui.Info("No hooks configured in " + scope + "/claude-code.json")
		return nil
	}

	ui.Header("Hooks (" + scope + ")")

	// hooks is expected to be map[string][]map[string]any (event → list of hook objects)
	hooksMap, ok := hooks.(map[string]any)
	if !ok {
		// fallback: raw JSON
		hooksJSON, _ := json.MarshalIndent(hooks, "  ", "  ")
		fmt.Fprintf(os.Stdout, "  %s\n\n", string(hooksJSON))
		return nil
	}

	count := 0
	for event, val := range hooksMap {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, event, ui.Reset)
		hookList, isList := val.([]any)
		if !isList {
			// single object
			hookList = []any{val}
		}
		for _, h := range hookList {
			hookObj, isMap := h.(map[string]any)
			if !isMap {
				fmt.Fprintf(os.Stdout, "    %s%v%s\n", ui.Dim, h, ui.Reset)
				continue
			}
			// Extract matcher and commands
			matcher, _ := hookObj["matcher"].(string)
			if matcher != "" {
				fmt.Fprintf(os.Stdout, "    matcher: %s%s%s\n", ui.Bold, matcher, ui.Reset)
			}
			if cmds, ok := hookObj["hooks"].([]any); ok {
				for _, c := range cmds {
					cmdObj, isMap := c.(map[string]any)
					if !isMap {
						fmt.Fprintf(os.Stdout, "    %s%v%s\n", ui.Dim, c, ui.Reset)
						continue
					}
					cmdType, _ := cmdObj["type"].(string)
					cmdVal, _ := cmdObj["command"].(string)
					if cmdVal == "" {
						// try "cmd"
						cmdVal, _ = cmdObj["cmd"].(string)
					}
					label := cmdType
					if label == "" {
						label = "command"
					}
					fmt.Fprintf(os.Stdout, "    %s%s:%s %s%s%s\n", ui.Dim, label, ui.Reset, ui.Dim, cmdVal, ui.Reset)
				}
			} else if cmd, ok := hookObj["command"].(string); ok {
				fmt.Fprintf(os.Stdout, "    %scommand:%s %s%s%s\n", ui.Dim, ui.Reset, ui.Dim, cmd, ui.Reset)
			} else {
				// fallback for unknown structure
				raw, _ := json.MarshalIndent(hookObj, "    ", "  ")
				fmt.Fprintf(os.Stdout, "    %s%s%s\n", ui.Dim, string(raw), ui.Reset)
			}
		}
		count++
	}
	if count == 0 {
		ui.Info("No hook events defined.")
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type listedCanonicalHook struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	When        string   `yaml:"when"`
	EnabledOn   []string `yaml:"enabled_on"`
	Run         struct {
		Command string `yaml:"command"`
	} `yaml:"run"`
}

func listCanonicalHooks(root, scope string) (bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	type renderedHook struct {
		dir string
		h   listedCanonicalHook
	}
	var hooks []renderedHook
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(root, entry.Name(), "HOOK.yaml")
		content, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("reading %s: %w", manifestPath, err)
		}
		var hook listedCanonicalHook
		if err := yaml.Unmarshal(content, &hook); err != nil {
			return false, fmt.Errorf("parsing %s: %w", manifestPath, err)
		}
		hooks = append(hooks, renderedHook{dir: entry.Name(), h: hook})
	}
	if len(hooks) == 0 {
		return false, nil
	}

	sort.Slice(hooks, func(i, j int) bool {
		return hooks[i].dir < hooks[j].dir
	})

	ui.Header("Hooks (" + scope + ")")
	for _, item := range hooks {
		name := strings.TrimSpace(item.h.Name)
		if name == "" {
			name = item.dir
		}
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, name, ui.Reset)
		if item.h.Description != "" {
			fmt.Fprintf(os.Stdout, "    %sdescription:%s %s\n", ui.Dim, ui.Reset, item.h.Description)
		}
		if item.h.When != "" {
			fmt.Fprintf(os.Stdout, "    %swhen:%s %s\n", ui.Dim, ui.Reset, item.h.When)
		}
		if len(item.h.EnabledOn) > 0 {
			fmt.Fprintf(os.Stdout, "    %senabled_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(item.h.EnabledOn, ", "))
		}
		if item.h.Run.Command != "" {
			fmt.Fprintf(os.Stdout, "    %scommand:%s %s\n", ui.Dim, ui.Reset, item.h.Run.Command)
		}
	}
	fmt.Fprintln(os.Stdout)
	return true, nil
}
