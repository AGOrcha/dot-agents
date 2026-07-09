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
		if root.PersistentFlags().Lookup(name) == nil {
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

// TestNewRootCommand_PreRun_AgentsHomeOverrideBypassesHomeCheck covers the
// top-risk #2 remediation: an explicit AGENTS_HOME override short-circuits
// the preflight even when $HOME is unresolvable.
func TestNewRootCommand_PreRun_AgentsHomeOverrideBypassesHomeCheck(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/explicit-agents-home")
	t.Setenv("HOME", "")
	root := NewRootCommand()
	if err := root.PersistentPreRunE(root, nil); err != nil {
		t.Errorf("expected no error with AGENTS_HOME override even though $HOME is unset: %v", err)
	}
}

// TestNewRootCommand_PreRun_HomeUnresolvableHardFails covers the actual
// remediation: no AGENTS_HOME override and an unresolvable $HOME must
// hard-fail with an actionable message rather than let every downstream
// AgentsHome() call silently degrade to a relative "./.agents" path.
func TestNewRootCommand_PreRun_HomeUnresolvableHardFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", "")
	t.Setenv("HOME", "")
	// os.UserHomeDir resolves via USERPROFILE (then HOMEDRIVE+HOMEPATH) on
	// Windows, so clear those too to force the failure cross-platform.
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	root := NewRootCommand()
	err := root.PersistentPreRunE(root, nil)
	if err == nil {
		t.Fatal("expected a hard error when home is unresolvable and AGENTS_HOME is unset")
	}
	if !strings.Contains(err.Error(), "AGENTS_HOME") {
		t.Errorf("expected actionable error mentioning AGENTS_HOME, got: %v", err)
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

// TestRootConfigDeps_FlagGetters verifies rootConfigDeps wires the global
// --json and --dry-run flags into config.Deps as live getters, so the mutating
// `da config sync` honors --dry-run the same way it honors --json. Both getters
// must read back through the package-global Flags.
func TestRootConfigDeps_FlagGetters(t *testing.T) {
	prev := Flags
	t.Cleanup(func() { Flags = prev })

	deps := rootConfigDeps()
	if deps.JSON == nil || deps.DryRun == nil {
		t.Fatalf("rootConfigDeps must wire both JSON and DryRun getters, got JSON=%v DryRun=%v",
			deps.JSON != nil, deps.DryRun != nil)
	}

	Flags.JSON = false
	Flags.DryRun = false
	if deps.JSON() || deps.DryRun() {
		t.Errorf("getters should read false when flags are unset: json=%v dryRun=%v", deps.JSON(), deps.DryRun())
	}

	Flags.JSON = true
	Flags.DryRun = true
	if !deps.JSON() || !deps.DryRun() {
		t.Errorf("getters should read true when flags are set: json=%v dryRun=%v", deps.JSON(), deps.DryRun())
	}
}
