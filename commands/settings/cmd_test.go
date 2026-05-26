package settings

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// noOpDeps returns a Deps stub whose positional-arg helpers accept any
// arg shape. Mirrors agents.noOpHints / skills's equivalent.
func noOpDeps() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	return Deps{
		ErrorWithHints: func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		UsageError:     func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
	}
}

func TestNewCmd_Metadata(t *testing.T) {
	cmd := NewCmd(noOpDeps())
	if cmd.Use != "settings" {
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

func TestNewCmd_ExampleBlockMentionsAllVerbs(t *testing.T) {
	cmd := NewCmd(noOpDeps())
	for _, needle := range []string{"settings list", "settings show", "settings remove"} {
		if !strings.Contains(cmd.Example, needle) {
			t.Errorf("Example missing %q: %q", needle, cmd.Example)
		}
	}
}

func TestNewCmd_ShortAndLongPopulated(t *testing.T) {
	cmd := NewCmd(noOpDeps())
	if cmd.Short == "" {
		t.Error("Short should be set")
	}
	if !strings.Contains(cmd.Long, "~/.agents/settings/") {
		t.Errorf("Long should describe canonical settings path: %q", cmd.Long)
	}
}
