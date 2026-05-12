package agents

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// noOpHints returns a deps stub whose hint helpers accept any arg shape.
func noOpHints() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	return Deps{
		ErrorWithHints: func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		UsageError:     func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
		RangeArgsWithHints: func(min, max int, hints ...string) cobra.PositionalArgs {
			return accept
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
	}
}

func TestNewAgentsCmd_RegistersAllSubcommands(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	if cmd == nil || cmd.Use != "agents" {
		t.Fatalf("unexpected root: %+v", cmd)
	}

	expected := map[string]bool{
		"list": false, "new": false, "promote": false,
		"import": false, "remove": false,
	}
	for _, c := range cmd.Commands() {
		expected[c.Name()] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing agents subcommand %q", name)
		}
	}
}

func TestNewAgentsCmd_ExampleBlockMentionsLifecycle(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	for _, needle := range []string{"agents list", "agents new", "agents promote", "agents import", "agents remove"} {
		if !strings.Contains(cmd.Example, needle) {
			t.Errorf("Example missing %q: %q", needle, cmd.Example)
		}
	}
}

func TestNewAgentsCmd_PromoteHasForceFlag(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	var promote *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "promote" {
			promote = c
		}
	}
	if promote == nil {
		t.Fatal("promote subcommand not found")
	}
	if promote.Flags().Lookup("force") == nil {
		t.Error("promote command missing --force flag")
	}
}

func TestNewAgentsCmd_RemoveHasPurgeFlag(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	var remove *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "remove" {
			remove = c
		}
	}
	if remove == nil {
		t.Fatal("remove subcommand not found")
	}
	if remove.Flags().Lookup("purge") == nil {
		t.Error("remove command missing --purge flag")
	}
}

func TestScopeFromArgs(t *testing.T) {
	if got := scopeFromArgs(nil); got != "global" {
		t.Errorf("scopeFromArgs(nil) = %q, want global", got)
	}
	if got := scopeFromArgs([]string{"billing"}); got != "billing" {
		t.Errorf("scopeFromArgs([billing]) = %q", got)
	}
}

func TestPathExists(t *testing.T) {
	tmp := t.TempDir()
	if pathExists(tmp + "/missing") {
		t.Error("pathExists should return false for missing path")
	}
	if !pathExists(tmp) {
		t.Error("pathExists should return true for existing path")
	}
}

func TestAgentManifestConstant(t *testing.T) {
	if agentManifestName != "AGENT.md" {
		t.Errorf("agentManifestName = %q, want AGENT.md", agentManifestName)
	}
}
