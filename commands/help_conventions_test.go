package commands

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The CLI is read by AI agents through `--help` before it is read by anyone
// else, so the help text is a contract rather than a courtesy. These tests
// enforce the two rules in docs/CLI_HELP_CONVENTIONS.md that are mechanically
// checkable across the whole command tree: a closed-set flag lists its values,
// and the commands agents actually drive carry worked examples.

// agentDrivenCommands are the command paths an agent operating this repo
// invokes on a normal loop iteration. Each must ship an Example block, because
// these are the commands where guessing the flag combination is expensive.
// Add to this list when a new command joins the agent's critical path.
var agentDrivenCommands = []string{
	"da workflow orient",
	"da workflow eligible",
	"da workflow next",
	"da workflow start-task",
	"da workflow advance",
	"da workflow checkpoint",
	"da workflow verify record",
	"da workflow merge-back",
	"da workflow close-task",
	"da workflow fanout",
	"da workflow delegation closeout",
	"da workflow fold-back create",
	"da workflow task add",
	"da workflow task update",
	"da workflow plan create",
	"da workflow app-types",
	"da workflow resolve-prompt",
	"da config explain",
	"da config sync",
	"da config verify",
	"da kg query",
	"da kg bridge query",
	"da status",
	"da refresh",
}

func TestAgentDrivenCommandsShipExamples(t *testing.T) {
	byPath := indexCommandTree(NewRootCommand())
	for _, path := range agentDrivenCommands {
		t.Run(path, func(t *testing.T) {
			cmd, ok := byPath[path]
			if !ok {
				t.Fatalf("%q is listed as agent-driven but is not in the command tree", path)
			}
			if strings.TrimSpace(cmd.Example) == "" {
				t.Errorf("%q has no Example block; agents read --help before docs", path)
			}
		})
	}
}

func TestEnumFlagsListTheirValuesInHelp(t *testing.T) {
	forEachEnumFlag(NewRootCommand(), func(path string, flag *pflag.Flag, values []string) {
		t.Run(path+" --"+flag.Name, func(t *testing.T) {
			for _, v := range values {
				if !strings.Contains(flag.Usage, v) {
					t.Errorf("usage omits allowed value %q: %s", v, flag.Usage)
				}
			}
			if !strings.Contains(flag.Usage, "one of:") {
				t.Errorf("usage does not render the value listing: %s", flag.Usage)
			}
		})
	})
}

func TestEnumFlagDefaultsAreInTheirOwnValueSet(t *testing.T) {
	forEachEnumFlag(NewRootCommand(), func(path string, flag *pflag.Flag, values []string) {
		if flag.DefValue == "" {
			return
		}
		t.Run(path+" --"+flag.Name, func(t *testing.T) {
			if !containsValue(values, flag.DefValue) {
				t.Errorf("default %q is not in the declared set %v", flag.DefValue, values)
			}
		})
	})
}

// TestDynamicEnumFlagsPointAtRealCommands enforces the no-dead-ends rule: a
// flag whose vocabulary is config-derived must name a command that actually
// exists and prints the live set, not a doc page or a stale command path.
func TestDynamicEnumFlagsPointAtRealCommands(t *testing.T) {
	root := NewRootCommand()
	byPath := indexCommandTree(root)
	found := 0
	for path, cmd := range byPath {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			src := cmdutil.EnumDynamicSource(cmd, f.Name)
			if src == "" {
				return
			}
			found++
			t.Run(path+" --"+f.Name, func(t *testing.T) {
				if !namesExistingCommand(byPath, src) {
					t.Errorf("--%s points at %q, which is not a command in the tree", f.Name, src)
				}
				if !strings.Contains(f.Usage, src) {
					t.Errorf("usage does not surface the live-vocabulary command: %s", f.Usage)
				}
			})
		})
	}
	if found == 0 {
		t.Fatal("no dynamic enum flags found; the annotation wiring has regressed")
	}
}

// namesExistingCommand reports whether src (a full invocation such as
// "da workflow app-types --json") starts with a real command path.
func namesExistingCommand(byPath map[string]*cobra.Command, src string) bool {
	fields := strings.Fields(src)
	for n := len(fields); n > 0; n-- {
		if _, ok := byPath[strings.Join(fields[:n], " ")]; ok {
			return true
		}
	}
	return false
}

// TestKnownEnumFlagsUseTheHelper pins the flags whose vocabularies previously
// lived as prose in one place and as a literal list in another. Regressing any
// of them to a plain StringVar drops the value listing from `--help` silently,
// which is exactly the failure this pass was written to end.
func TestKnownEnumFlagsUseTheHelper(t *testing.T) {
	want := map[string][]string{
		"da workflow advance --status":                 {"pending", "in_progress", "completed", "cancelled", "blocked"},
		"da workflow verify record --kind":             {"test", "lint", "build", "format", "custom", "review"},
		"da workflow verify record --status":           {"pass", "fail", "partial", "unknown"},
		"da workflow verify record --scope":            {"file", "package", "repo", "custom"},
		"da workflow merge-back --verification-status": {"pass", "fail", "partial", "unknown"},
		"da workflow checkpoint --verification-status": {"pass", "fail", "partial", "unknown"},
		"da workflow checkpoint --role":                {"impl", "verifier", "review"},
		"da workflow plan update --status":             {"draft", "active", "paused", "completed", "archived"},
		"da workflow delegation closeout --decision":   {"accept", "reject"},
		"da workflow resolve-prompt --kind":            {"executor", "verifier", "reviewer", "orchestrator"},
		"da workflow pipeline emit --platform":         {"claude-code", "omp"},
		"da config relevance --filter":                 {"units", "topology", "lenses", "graph", "lessons", "all"},
		"da kg query --intent":                         {"decision_lookup", "repo_context", "contradictions"},
		"da kg link add --kind":                        {"mentions", "implements", "documents", "decides", "references"},
		"da import --scope":                            {"project", "global", "all"},
	}
	byPath := indexCommandTree(NewRootCommand())
	for spec, values := range want {
		t.Run(spec, func(t *testing.T) {
			path, flagName := splitFlagSpec(t, spec)
			cmd, ok := byPath[path]
			if !ok {
				t.Fatalf("command %q not found", path)
			}
			declared := cmdutil.EnumValues(cmd, flagName)
			if declared == nil {
				t.Fatalf("--%s is not registered through cmdutil.RegisterEnum", flagName)
			}
			for _, v := range values {
				if !containsValue(declared, v) {
					t.Errorf("declared set %v is missing %q", declared, v)
				}
			}
		})
	}
}

// TestEnumValidationRejectsOutOfSetValueNamingTheSet proves the third consumer
// of the shared declaration: the value the help lists is the value validation
// accepts, and the rejection names the vocabulary.
func TestEnumValidationRejectsOutOfSetValueNamingTheSet(t *testing.T) {
	root := NewRootCommand()
	cmd, ok := indexCommandTree(root)["da workflow advance"]
	if !ok {
		t.Fatal("workflow advance not found")
	}
	err := cmd.PreRunE(cmd, nil)
	if err != nil {
		t.Fatalf("unset --status must not fail validation (MarkFlagRequired owns that): %v", err)
	}
	if err := cmd.Flags().Set("status", "done"); err != nil {
		t.Fatal(err)
	}
	err = cmd.PreRunE(cmd, nil)
	if err == nil {
		t.Fatal("want rejection for an out-of-set status")
	}
	for _, v := range []string{"pending", "in_progress", "completed"} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("rejection omits allowed value %q: %s", v, err.Error())
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// indexCommandTree maps every command path in the tree to its command.
func indexCommandTree(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		out[c.CommandPath()] = c
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// forEachEnumFlag visits every flag in the tree that was registered through
// cmdutil.RegisterEnum with a compiled-in vocabulary.
func forEachEnumFlag(root *cobra.Command, visit func(path string, flag *pflag.Flag, values []string)) {
	for path, cmd := range indexCommandTree(root) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			values := cmdutil.EnumValues(cmd, f.Name)
			if len(values) == 0 {
				return
			}
			visit(path, f, values)
		})
	}
}

func containsValue(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// splitFlagSpec splits "da workflow advance --status" into path and flag name.
func splitFlagSpec(t *testing.T, spec string) (string, string) {
	t.Helper()
	idx := strings.LastIndex(spec, " --")
	if idx < 0 {
		t.Fatalf("malformed flag spec %q, want \"<command path> --<flag>\"", spec)
	}
	return spec[:idx], spec[idx+3:]
}
