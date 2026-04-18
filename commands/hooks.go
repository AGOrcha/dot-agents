package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Inspect and manage canonical ~/.agents/hooks bundles",
		Long: `Commands for hook resources stored under ~/.agents/hooks/.

Each scope directory is either global (~/.agents/hooks/global/) or a managed project
name (~/.agents/hooks/<project>/), matching names from dot-agents status.

Canonical hooks live in bundle directories: hooks/<scope>/<logical-name>/HOOK.yaml
(optionally with sidecar scripts). Legacy single-file JSON hooks
(hooks/<scope>/<name>.json) are still listed for visibility; prefer HOOK.yaml bundles
for new work — the same layout import and refresh use when canonicalizing hook content.`,
		Example: ExampleBlock(
			"  dot-agents hooks list",
			"  dot-agents hooks list my-app",
			"  dot-agents hooks show global session-orient",
			"  dot-agents hooks remove global old-hook-bundle",
		),
	}
	cmd.AddCommand(newHooksListCmd())
	cmd.AddCommand(newHooksShowCmd())
	cmd.AddCommand(newHooksRemoveCmd())
	return cmd
}

func newHooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List configured hooks for a scope",
		Example: ExampleBlock(
			"  dot-agents hooks list",
			"  dot-agents hooks list billing-api",
		),
		Args: MaximumNArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that hooks tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return runHooksList(scope)
		},
	}
}

func newHooksShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show one hook bundle or legacy hook file in ~/.agents/hooks/",
		Args:  ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the hook logical name."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksShow(args[0], args[1])
		},
	}
}

func newHooksRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a hook bundle directory or legacy hooks/*.json file from ~/.agents/hooks/",
		Long: `Deletes managed hook storage only (not project symlinks). After removal, run
dot-agents refresh or install for the relevant project so platform hook files stay
consistent.`,
		Args: ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksRemove(args[0], args[1])
		},
	}
}

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

func hookKindLabel(kind platform.HookSourceKind) string {
	switch kind {
	case platform.HookSourceCanonicalBundle:
		return "canonical bundle"
	case platform.HookSourceLegacyFile:
		return "legacy file"
	default:
		return string(kind)
	}
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

func runHooksShow(scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findHookSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	ui.Header("Hook " + spec.Name + " (" + scope + ")")
	fmt.Fprintf(os.Stdout, "  %skind:%s %s\n", ui.Dim, ui.Reset, hookKindLabel(spec.SourceKind))
	fmt.Fprintf(os.Stdout, "  %smanifest:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	if spec.SourceKind == platform.HookSourceCanonicalBundle {
		fmt.Fprintf(os.Stdout, "  %sbundle dir:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(filepath.Dir(spec.SourcePath)))
	}
	if spec.Description != "" {
		fmt.Fprintf(os.Stdout, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, spec.Description)
	}
	if spec.When != "" {
		fmt.Fprintf(os.Stdout, "  %swhen:%s %s\n", ui.Dim, ui.Reset, spec.When)
	}
	if len(spec.MatchTools) > 0 {
		fmt.Fprintf(os.Stdout, "  %smatch.tools:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.MatchTools, ", "))
	}
	if spec.MatchExpression != "" {
		fmt.Fprintf(os.Stdout, "  %smatch.expression:%s %s\n", ui.Dim, ui.Reset, spec.MatchExpression)
	}
	cmd := strings.TrimSpace(platform.ResolveHookCommand(*spec))
	if cmd != "" {
		fmt.Fprintf(os.Stdout, "  %scommand:%s %s\n", ui.Dim, ui.Reset, cmd)
	}
	if spec.TimeoutMS > 0 {
		fmt.Fprintf(os.Stdout, "  %stimeout_ms:%s %d\n", ui.Dim, ui.Reset, spec.TimeoutMS)
	}
	if len(spec.EnabledOn) > 0 {
		fmt.Fprintf(os.Stdout, "  %senabled_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.EnabledOn, ", "))
	}
	if len(spec.RequiredOn) > 0 {
		fmt.Fprintf(os.Stdout, "  %srequired_on:%s %s\n", ui.Dim, ui.Reset, strings.Join(spec.RequiredOn, ", "))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func findHookSpec(agentsHome, scope, name string) (*platform.HookSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, UsageError("hook name is empty", "Pass the logical name shown by `dot-agents hooks list`.")
	}
	specs, err := platform.ListHookSpecs(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrorWithHints(
				fmt.Sprintf("no hooks directory for scope %q", scope),
				"Create ~/.agents/hooks/"+scope+"/ or run `dot-agents import` to populate hooks.",
			)
		}
		return nil, err
	}
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i], nil
		}
	}
	return nil, ErrorWithHints(
		fmt.Sprintf("hook not found: %s / %s", scope, name),
		"Run `dot-agents hooks list "+scope+"` to see available names.",
	)
}

func runHooksRemove(scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findHookSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	target, err := hookRemovalTarget(spec)
	if err != nil {
		return err
	}
	if err := ensureUnderHooksScopeTree(agentsHome, scope, target); err != nil {
		return err
	}

	ui.Header("dot-agents hooks remove")
	fmt.Fprintf(os.Stdout, "Remove %s hook %q\n", ui.BoldText(scope), name)
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(target))

	if Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !Flags.Yes && !Flags.Force {
		if !ui.Confirm("Remove this hook from ~/.agents/hooks/?", false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if spec.SourceKind == platform.HookSourceCanonicalBundle {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("removing bundle: %w", err)
		}
	} else {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("removing file: %w", err)
		}
	}
	ui.Success(fmt.Sprintf("Removed hook %q from scope %s.", name, scope))
	return nil
}

func hookRemovalTarget(spec *platform.HookSpec) (string, error) {
	switch spec.SourceKind {
	case platform.HookSourceCanonicalBundle:
		return filepath.Dir(spec.SourcePath), nil
	case platform.HookSourceLegacyFile:
		return spec.SourcePath, nil
	default:
		return "", fmt.Errorf("unsupported hook source kind %q", spec.SourceKind)
	}
}

func ensureUnderHooksScopeTree(agentsHome, scope, target string) error {
	root := filepath.Join(agentsHome, "hooks", scope)
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove path outside %s", cleanRoot)
	}
	return nil
}

func listHooksLegacyClaudeSettings(scope string) error {
	agentsHome := config.AgentsHome()
	settingsPath := filepath.Join(agentsHome, "settings", scope, "claude-code.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No hooks under ~/.agents/hooks/" + scope + "/ and no " + scope + "/claude-code.json hook settings found.")
			return nil
		}
		return fmt.Errorf("reading claude-code.json: %w", err)
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

	ui.Header("Hooks (" + scope + ") — legacy settings projection")

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
