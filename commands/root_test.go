package commands

import (
	"strings"
	"testing"
)

// TestNewRootCommand_Metadata checks the root command's surface and
// persistent flags are wired correctly.
func TestNewRootCommand_Metadata(t *testing.T) {
	root := NewRootCommand()
	if root == nil {
		t.Fatal("NewRootCommand returned nil")
	}
	if root.Use != "da" {
		t.Errorf("Use = %q, want %q", root.Use, "da")
	}
	if !root.SilenceUsage {
		t.Error("SilenceUsage should be true")
	}
	if !root.SilenceErrors {
		t.Error("SilenceErrors should be true")
	}
	if root.Example == "" {
		t.Error("Example block should be populated")
	}
	if !strings.Contains(root.Example, "da init") {
		t.Errorf("Example missing 'da init': %q", root.Example)
	}
	if root.Version == "" {
		t.Error("Version should be set")
	}

	for _, name := range []string{"dry-run", "force", "verbose", "yes", "json"} {
		if f := root.PersistentFlags().Lookup(name); f == nil {
			t.Errorf("persistent flag %q missing", name)
		}
	}
}

// TestNewRootCommand_RegistersAllSubcommands asserts every advertised
// subcommand is reachable via cobra's Find().
func TestNewRootCommand_RegistersAllSubcommands(t *testing.T) {
	root := NewRootCommand()

	expected := []string{
		"init", "add", "remove", "refresh", "import",
		"status", "doctor", "skills", "agents", "hooks",
		"rules", "mcp", "settings", "review", "sync",
		"explain", "install", "session",
	}

	for _, name := range expected {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Errorf("Find(%q) error: %v", name, err)
			continue
		}
		if cmd == nil || cmd.Name() != name {
			t.Errorf("Find(%q) returned %v", name, cmd)
		}
	}

	if len(root.Commands()) < len(expected) {
		t.Errorf("root has %d subcommands; expected at least %d", len(root.Commands()), len(expected))
	}
}

// TestNewRootCommand_PreRunNoop ensures the PersistentPreRunE hook does not
// reject empty invocations.
func TestNewRootCommand_PreRunNoop(t *testing.T) {
	root := NewRootCommand()
	if root.PersistentPreRunE == nil {
		t.Fatal("PersistentPreRunE not set")
	}
	if err := root.PersistentPreRunE(root, nil); err != nil {
		t.Errorf("PersistentPreRunE returned error: %v", err)
	}
}

// TestNewRootCommand_VersionTemplate verifies the version output uses the
// "da version X" format rather than the cobra default.
func TestNewRootCommand_VersionTemplate(t *testing.T) {
	root := NewRootCommand()
	tmpl := root.VersionTemplate()
	if !strings.Contains(tmpl, "da version") {
		t.Errorf("version template = %q, want 'da version' prefix", tmpl)
	}
}
