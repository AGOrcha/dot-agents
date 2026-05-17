package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

const uxUsageHintFmt = "Usage: %s"

// CLIError carries actionable hints for user-facing command failures.
type CLIError struct {
	Message   string
	Hints     []string
	ShowUsage bool
	Cause     error
}

func (e *CLIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *CLIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// errNoWorkflowProject is returned when workflow commands cannot resolve a project context.
// Kept in package commands so ux enrichment can use errors.Is with a stable sentinel.
// The full workflow wiring (PR3b) will use this sentinel in workflowBridgeDeps.
var errNoWorkflowProject = &CLIError{
	Message: "workflow commands must run inside a project directory",
	Hints: []string{
		"Run workflow commands from a repository that already contains `.agents/` or `.agentsrc.json`.",
		"If this repo is not managed yet, start with `da add .` or `da install --generate`.",
	},
}

func ErrorWithHints(message string, hints ...string) error {
	return &CLIError{Message: strings.TrimSpace(message), Hints: compactHints(hints)}
}

func UsageError(message string, hints ...string) error {
	return &CLIError{Message: strings.TrimSpace(message), Hints: compactHints(hints), ShowUsage: true}
}

func ConfigureRootCommandUX(root *cobra.Command) {
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &CLIError{
			Message:   strings.TrimSpace(err.Error()),
			Hints:     compactHints([]string{helpHintForCommand(cmd)}),
			ShowUsage: true,
			Cause:     err,
		}
	})
}

func RenderCommandError(w io.Writer, root *cobra.Command, argv []string, err error) {
	cmd := resolveRequestedCommand(root, argv)
	details := classifyCLIError(err, cmd)

	fmt.Fprintf(w, "%s%s\n", ui.ColorText(ui.Red, "✗ Error: "), details.Message)

	if len(details.Hints) > 0 {
		label := "Hint"
		if len(details.Hints) > 1 {
			label = "Hints"
		}
		fmt.Fprintf(w, "\n%s:\n", label)
		for _, hint := range details.Hints {
			fmt.Fprintf(w, "  - %s\n", hint)
		}
	}

	if details.ShowUsage && cmd != nil {
		usage := cmd.UsageString()
		if strings.TrimSpace(usage) != "" {
			fmt.Fprintf(w, "\n%s", usage)
		}
	}
}

func ExactArgsWithHints(n int, hints ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == n {
			return nil
		}
		noun := "arguments"
		if n == 1 {
			noun = "argument"
		}
		return UsageError(
			fmt.Sprintf("%s expects %d %s, got %d", cmd.CommandPath(), n, noun, len(args)),
			append([]string{
				fmt.Sprintf(uxUsageHintFmt, cmd.UseLine()),
				helpHintForCommand(cmd),
			}, hints...)...,
		)
	}
}

func NoArgsWithHints(hints ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return UsageError(
			fmt.Sprintf("%s does not accept positional arguments (got %d)", cmd.CommandPath(), len(args)),
			append([]string{
				fmt.Sprintf(uxUsageHintFmt, cmd.UseLine()),
				helpHintForCommand(cmd),
			}, hints...)...,
		)
	}
}

func MaximumNArgsWithHints(n int, hints ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= n {
			return nil
		}
		return UsageError(
			fmt.Sprintf("%s accepts at most %d argument(s), got %d", cmd.CommandPath(), n, len(args)),
			append([]string{
				fmt.Sprintf(uxUsageHintFmt, cmd.UseLine()),
				helpHintForCommand(cmd),
			}, hints...)...,
		)
	}
}

func RangeArgsWithHints(min, max int, hints ...string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		return UsageError(
			fmt.Sprintf("%s expects between %d and %d arguments, got %d", cmd.CommandPath(), min, max, len(args)),
			append([]string{
				fmt.Sprintf(uxUsageHintFmt, cmd.UseLine()),
				helpHintForCommand(cmd),
			}, hints...)...,
		)
	}
}

func ExampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}

func helpHintForCommand(cmd *cobra.Command) string {
	if cmd == nil {
		return "Run `da --help` to see available commands."
	}
	path := cmd.CommandPath()
	if root := cmd.Root(); root == cmd && cmd.Name() != "da" {
		path = "da " + path
	}
	return fmt.Sprintf("Run `%s --help` to see examples and supported flags.", path)
}

func compactHints(hints []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(hints))
	for _, hint := range hints {
		hint = strings.TrimSpace(hint)
		if hint == "" || seen[hint] {
			continue
		}
		seen[hint] = true
		out = append(out, hint)
	}
	return out
}

func resolveRequestedCommand(root *cobra.Command, argv []string) *cobra.Command {
	if root == nil {
		return nil
	}
	if len(argv) == 0 {
		return root
	}

	current := root
	for _, arg := range argv {
		if strings.HasPrefix(arg, "-") {
			break
		}
		next := findChildCommand(current, arg)
		if next == nil {
			break
		}
		current = next
	}
	return current
}

func findChildCommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name || child.HasAlias(name) {
			return child
		}
	}
	return nil
}

func classifyCLIError(err error, cmd *cobra.Command) *CLIError {
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return enrichCLIError(cliErr, cmd)
	}

	derived := &CLIError{Message: strings.TrimSpace(err.Error())}
	if strings.Contains(derived.Message, "unknown command") {
		derived.ShowUsage = true
	}
	return enrichCLIError(derived, cmd)
}

type cliHintRule struct {
	contains string
	hints    []string
}

var cliHintRules = []cliHintRule{
	{"manifest not found", []string{
		"Run `da install --generate` to create `.agentsrc.json` from the current shared state.",
		"If the project is not managed yet, run `da add .` first.",
	}},
	{"~/.agents/ not initialized", []string{
		"Run `da init` once on this machine before using install, add, or refresh.",
	}},
	{"project not found:", []string{
		"Use the registered project name from `da status`, not the filesystem path.",
		"Run `da status` to list managed projects.",
	}},
	{"invalid scope", []string{
		"Supported scopes are `project`, `global`, and `all`.",
	}},
	{"unknown preference key", []string{
		"Check the preference key against the documented configuration keys for your installed `da` version.",
	}},
	{"invalid task status", []string{
		"Valid task statuses are `pending`, `in_progress`, `blocked`, `completed`, and `cancelled`.",
	}},
	{"invalid plan status", []string{
		"Valid plan statuses are `draft`, `active`, `paused`, `completed`, and `archived`.",
	}},
	{"invalid verification status", []string{
		"Valid verification statuses are `pass`, `fail`, `partial`, and `unknown`.",
	}},
	{"not found in any source", []string{
		"Check that the resource name exists in one of the `.agentsrc.json` sources.",
		"Use `da install --strict` when you want missing resources to fail fast.",
	}},
	{"unknown command", []string{
		"Run `da --help` to see available command families.",
	}},
}

func enrichCLIError(cliErr *CLIError, cmd *cobra.Command) *CLIError {
	if cliErr == nil {
		return &CLIError{Message: "command failed"}
	}

	enriched := &CLIError{
		Message:   strings.TrimSpace(cliErr.Message),
		Hints:     append([]string{}, cliErr.Hints...),
		ShowUsage: cliErr.ShowUsage,
		Cause:     cliErr.Cause,
	}

	msg := enriched.Message
	if errors.Is(cliErr, errNoWorkflowProject) {
		enriched.Hints = append(enriched.Hints,
			"Run workflow commands from a repository that already contains `.agents/` or `.agentsrc.json`.",
			"If this repo is not registered yet, start with `da add .` or `da install --generate`.",
		)
	} else {
		for _, rule := range cliHintRules {
			if strings.Contains(msg, rule.contains) {
				enriched.Hints = append(enriched.Hints, rule.hints...)
				break
			}
		}
	}

	if cmd != nil {
		enriched.Hints = append(enriched.Hints, helpHintForCommand(cmd))
	}
	enriched.Hints = compactHints(enriched.Hints)
	return enriched
}
