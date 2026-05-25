package rules

import (
	"testing"
)

func TestNewRulesCmd_Metadata(t *testing.T) {
	cmd := NewRulesCmd(testDeps(false, false, false))
	if cmd.Use != "rules" {
		t.Errorf("Use = %q", cmd.Use)
	}
	wantSubs := map[string]bool{"list": false, "show": false, "remove": false}
	for _, c := range cmd.Commands() {
		if _, ok := wantSubs[c.Name()]; ok {
			wantSubs[c.Name()] = true
		}
	}
	for name, found := range wantSubs {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestNewRulesCmd_MetadataExhaustive(t *testing.T) {
	cmd := NewRulesCmd(testDeps(false, false, false))
	for _, sub := range cmd.Commands() {
		if sub.Use == "" {
			t.Errorf("subcommand %q: Use is empty", sub.Name())
		}
		if sub.Short == "" {
			t.Errorf("subcommand %q: Short is empty", sub.Name())
		}
		if sub.Args == nil {
			t.Errorf("subcommand %q: Args validator nil", sub.Name())
		}
		if sub.RunE == nil {
			t.Errorf("subcommand %q: RunE nil", sub.Name())
		}
	}
}

// TestNewListCmd_RejectsExtraArgs covers the MaximumNArgs(1) validator
// rejecting the >1-arg case.
func TestNewListCmd_RejectsExtraArgs(t *testing.T) {
	cmd := NewListCmd(testDeps(false, false, false))
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("list should reject 2 args")
	}
}

// TestNewShowCmd_RejectsBadArity covers ExactArgs(2) edges.
func TestNewShowCmd_RejectsBadArity(t *testing.T) {
	cmd := NewShowCmd(testDeps(false, false, false))
	if err := cmd.Args(cmd, []string{"only-one"}); err == nil {
		t.Error("show should reject 1 arg")
	}
	if err := cmd.Args(cmd, []string{"a", "b", "c"}); err == nil {
		t.Error("show should reject 3 args")
	}
	if err := cmd.Args(cmd, []string{"scope", "name"}); err != nil {
		t.Errorf("show(2 args) should be valid: %v", err)
	}
}

// TestNewRemoveCmd_RejectsBadArity covers ExactArgs(2) edges.
func TestNewRemoveCmd_RejectsBadArity(t *testing.T) {
	cmd := NewRemoveCmd(testDeps(false, false, false))
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("remove should reject 0 args")
	}
	if err := cmd.Args(cmd, []string{"scope", "name"}); err != nil {
		t.Errorf("remove(2 args) should be valid: %v", err)
	}
}
