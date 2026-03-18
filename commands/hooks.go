package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dot-agents/dot-agents/internal/config"
	"github.com/dot-agents/dot-agents/internal/ui"
	"github.com/spf13/cobra"
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
