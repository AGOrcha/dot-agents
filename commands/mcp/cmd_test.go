package mcp

import (
	"testing"

	"github.com/spf13/cobra"
)

// stubMaxArgs / stubExactArgs are zero-checking PositionalArgs the
// in-package tests pass through Deps when they only need NewCmd /
// NewListCmd / NewShowCmd / NewRemoveCmd to assemble without nil
// dereferences. Production wiring uses cobra.MaximumNArgs / cobra.ExactArgs
// via the parent commands package's hint-aware wrappers.
func stubMaxArgs(_ int, _ ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, _ []string) error { return nil }
}

func stubExactArgs(_ int, _ ...string) cobra.PositionalArgs {
	return func(_ *cobra.Command, _ []string) error { return nil }
}

// TestNewCmd_Metadata mirrors the parent commands.TestNewMCPCmd_Metadata —
// the assembled tree exposes the documented Use string and the list/show/
// remove subcommand triplet. The hooks/* and agents/* packages keep an
// identically-shaped metadata test as the single anchor for the public
// cobra surface.
func TestNewCmd_Metadata(t *testing.T) {
	deps := Deps{
		MaxArgsWithHints:   stubMaxArgs,
		ExactArgsWithHints: stubExactArgs,
	}
	cmd := NewCmd(deps)
	if cmd.Use != "mcp" {
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
