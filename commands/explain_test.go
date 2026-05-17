package commands

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// captureExplainStdout runs fn while os.Stdout is redirected; returns the
// captured bytes. Mirrors the kg package helper to keep this test file
// self-contained.
func captureExplainStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = oldStdout
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// runExplainWith invokes runExplain against an empty cobra command with the
// supplied args list.
func runExplainWith(t *testing.T, args ...string) string {
	t.Helper()
	return captureExplainStdout(t, func() {
		if err := runExplain(&cobra.Command{}, args); err != nil {
			t.Fatalf("runExplain %v: %v", args, err)
		}
	})
}

// TestNewExplainCmd_Metadata exercises the cobra command surface.
func TestNewExplainCmd_Metadata(t *testing.T) {
	cmd := NewExplainCmd()
	if cmd.Use != "explain [topic]" {
		t.Errorf("Use: got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short description should be set")
	}
	if cmd.Long == "" {
		t.Error("Long description should be set")
	}
	if cmd.Example == "" {
		t.Error("Example block should be populated")
	}
	if cmd.RunE == nil {
		t.Error("RunE should be wired up")
	}
	// Args validator: 2 positional args must error out.
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected Args validator to reject >1 positional arg")
	}
	// Single positional arg passes the validator.
	if err := cmd.Args(cmd, []string{"manifest"}); err != nil {
		t.Errorf("Args validator rejected single arg: %v", err)
	}
}

// TestExplain_OverviewByDefault asserts the no-arg path prints the overview.
func TestExplain_OverviewByDefault(t *testing.T) {
	out := runExplainWith(t)
	for _, want := range []string{"da overview", "Commands", "Workflow", "Topics"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in overview output:\n%s", want, out)
		}
	}
}

// TestExplain_UnknownTopicFallsBackToOverview exercises the default branch.
func TestExplain_UnknownTopicFallsBackToOverview(t *testing.T) {
	out := runExplainWith(t, "definitely-not-a-topic")
	if !strings.Contains(out, "da overview") {
		t.Errorf("unknown topic should fall back to overview, got:\n%s", out)
	}
}

// TestExplain_ManifestTopic verifies the manifest documentation.
func TestExplain_ManifestTopic(t *testing.T) {
	out := runExplainWith(t, "manifest")
	for _, want := range []string{"Manifest", ".agentsrc.json", "Schema", "Sources", "skills", "agents", "rules", "hooks", "mcp", "Flags"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in manifest output:\n%s", want, out)
		}
	}
}

// TestExplain_ManifestAliases verifies the agentsrc/install aliases route to
// the same printer.
func TestExplain_ManifestAliases(t *testing.T) {
	for _, alias := range []string{"agentsrc", "install"} {
		out := runExplainWith(t, alias)
		if !strings.Contains(out, ".agentsrc.json") {
			t.Errorf("alias %q should render manifest doc, got:\n%s", alias, out)
		}
	}
}

// TestExplain_StructureTopic checks both alias paths to printStructureExplanation.
func TestExplain_StructureTopic(t *testing.T) {
	for _, topic := range []string{"structure", "layout"} {
		out := runExplainWith(t, topic)
		for _, want := range []string{"~/.agents/", "config.json", "rules/", "skills/", "agents/", "hooks/", "plugins/", "Plugins", "PLUGIN.yaml"} {
			if !strings.Contains(out, want) {
				t.Errorf("topic=%q: expected %q in output:\n%s", topic, want, out)
			}
		}
	}
}

// TestExplain_LinksTopic exercises both the "links" and "link-types" entries.
func TestExplain_LinksTopic(t *testing.T) {
	for _, topic := range []string{"links", "link-types"} {
		out := runExplainWith(t, topic)
		for _, want := range []string{"Link Types", "HARD LINKS", "Cursor", "SYMLINKS", "CENTRALIZED SHARED TARGETS"} {
			if !strings.Contains(out, want) {
				t.Errorf("topic=%q: expected %q in output:\n%s", topic, want, out)
			}
		}
	}
}

// TestExplain_PlatformsTopic verifies the supported-platforms listing.
func TestExplain_PlatformsTopic(t *testing.T) {
	out := runExplainWith(t, "platforms")
	for _, want := range []string{"Supported Platforms", "Cursor", "Claude Code", "Codex CLI", "OpenCode", "GitHub Copilot"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in platforms output:\n%s", want, out)
		}
	}
}

// TestPrintExplanationFunctions_Direct exercises the printer functions
// directly so coverage attaches to each helper independently of the dispatcher.
func TestPrintExplanationFunctions_Direct(t *testing.T) {
	tests := []struct {
		name string
		fn   func()
		want string
	}{
		{"overview", printOverviewExplanation, "da overview"},
		{"manifest", printManifestExplanation, ".agentsrc.json"},
		{"links", printLinkTypesExplanation, "Link Types"},
		{"platforms", printPlatformsExplanation, "Supported Platforms"},
		{"structure", printStructureExplanation, "~/.agents/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := captureExplainStdout(t, tc.fn)
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected %q in %s output:\n%s", tc.want, tc.name, out)
			}
		})
	}
}
