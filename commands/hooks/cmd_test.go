package hooks

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewHooksCmd_HasExpectedSubcommands(t *testing.T) {
	cmd := NewHooksCmd(testDeps())
	if cmd == nil || cmd.Use != "hooks" {
		t.Fatalf("unexpected root: %+v", cmd)
	}
	want := map[string]bool{"list": false, "show": false, "remove": false}
	for _, c := range cmd.Commands() {
		want[c.Name()] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing hooks subcommand %q", name)
		}
	}
}

func TestNewHooksCmd_ExampleBlockReferencesCommands(t *testing.T) {
	cmd := NewHooksCmd(testDeps())
	for _, needle := range []string{"hooks list", "hooks show", "hooks remove"} {
		if !strings.Contains(cmd.Example, needle) {
			t.Errorf("Example missing %q: %q", needle, cmd.Example)
		}
	}
}

func TestNewHooksCmd_ListCmdRunsWithoutScopeArg(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var list *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "list" {
			list = c
		}
	}
	if list == nil {
		t.Fatal("list subcommand missing")
	}
	// Default scope "global" — runHooksList tolerates an absent tree (uses fallback).
	t.Setenv("AGENTS_HOME", t.TempDir())
	if err := list.RunE(list, nil); err != nil {
		t.Errorf("list with no args: %v", err)
	}
}

func TestNewHooksCmd_ListCmdAcceptsScopeArg(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var list *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "list" {
			list = c
		}
	}
	if list == nil {
		t.Fatal("list subcommand missing")
	}
	t.Setenv("AGENTS_HOME", t.TempDir())
	if err := list.RunE(list, []string{"some-project"}); err != nil {
		t.Errorf("list with scope arg: %v", err)
	}
}

func TestNewHooksCmd_ShowCmdMetadata(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var show *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "show" {
			show = c
		}
	}
	if show == nil {
		t.Fatal("show subcommand missing")
	}
	if !strings.Contains(show.Use, "scope") || !strings.Contains(show.Use, "name") {
		t.Errorf("show.Use should reference scope+name: %q", show.Use)
	}
}

func TestNewHooksCmd_RemoveCmdMetadata(t *testing.T) {
	root := NewHooksCmd(testDeps())
	var rm *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "remove" {
			rm = c
		}
	}
	if rm == nil {
		t.Fatal("remove subcommand missing")
	}
	if rm.Long == "" {
		t.Error("remove subcommand should have Long help text")
	}
}
