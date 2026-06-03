package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// seedIsolatedCLIShims writes shims for the named CLIs into a fakebin under a
// fresh temp dir and sets PATH to ONLY that directory, so platforms whose CLI
// was not seeded reliably probe IsInstalled()==false (no leakage from the real
// host PATH). Each shim prints "<name> 0.0.0" so Version() resolves to 0.0.0.
// Returns the temp root. Skips on Windows where the shim contract differs.
func seedIsolatedCLIShims(t *testing.T, names ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	return tmp
}

// TestDetectAndEnableNewPlatforms covers the refresh-driven auto-enable: a
// platform installed after `da init` (probe true) but recorded enabled:false
// gets enabled with its live version; versions for already-enabled installed
// platforms are refreshed; an enabled-but-uninstalled platform is left enabled
// and not flipped; and a present-but-new platform clears the "nothing to
// refresh" dead-end by returning a non-empty newly-enabled set.
// detectPlatformCase is one row of the TestDetectAndEnableNewPlatforms table.
// Naming it (vs an anonymous struct) lets the per-case assertions live on a
// method, keeping the test function's cognitive complexity low.
type detectPlatformCase struct {
	name          string
	shims         []string // CLIs to put on PATH (controls IsInstalled)
	agents        map[string]config.Agent
	wantEnabledID string // platform that must end enabled with version set
	wantNewlyLen  int    // expected count of newly-enabled display names
	wantLeftID    string // platform that must remain enabled:false untouched
}

// check asserts the post-detect config + newly-enabled set match the case.
func (tc detectPlatformCase) check(t *testing.T, cfg *config.Config, newly []string) {
	t.Helper()
	if len(newly) != tc.wantNewlyLen {
		t.Fatalf("newly-enabled = %v (len %d), want len %d", newly, len(newly), tc.wantNewlyLen)
	}
	if tc.wantEnabledID != "" {
		a, ok := cfg.Agents[tc.wantEnabledID]
		if !ok || !a.Enabled {
			t.Fatalf("%s should be enabled after detect, got %+v (present=%v)", tc.wantEnabledID, a, ok)
		}
		if a.Version != "0.0.0" {
			t.Errorf("%s version = %q, want refreshed %q", tc.wantEnabledID, a.Version, "0.0.0")
		}
	}
	if tc.wantLeftID != "" {
		if a := cfg.Agents[tc.wantLeftID]; a.Enabled {
			t.Errorf("%s should be left disabled (refresh never auto-enables an absent tool), got enabled", tc.wantLeftID)
		}
	}
}

func TestDetectAndEnableNewPlatforms(t *testing.T) {
	tests := []detectPlatformCase{
		{
			name:          "installed-but-disabled platform gets enabled and versioned",
			shims:         []string{"codex"},
			agents:        map[string]config.Agent{"codex": {Enabled: false}},
			wantEnabledID: "codex",
			wantNewlyLen:  1,
		},
		{
			name:  "already-enabled installed platform refreshes version, no new enable",
			shims: []string{"codex"},
			// codex already enabled with a stale version string.
			agents:        map[string]config.Agent{"codex": {Enabled: true, Version: "stale"}},
			wantEnabledID: "codex",
			wantNewlyLen:  0,
		},
		{
			name:          "enabled-but-uninstalled platform is left enabled and not projected",
			shims:         nil, // nothing installed
			agents:        map[string]config.Agent{"codex": {Enabled: true, Version: "1.2.3"}},
			wantNewlyLen:  0,
			wantLeftID:    "",
			wantEnabledID: "",
		},
		{
			name:          "disabled-and-uninstalled platform stays disabled",
			shims:         nil,
			agents:        map[string]config.Agent{"opencode": {Enabled: false}},
			wantNewlyLen:  0,
			wantLeftID:    "opencode",
			wantEnabledID: "",
		},
		{
			name:          "newly-installed platform clears the nothing-to-refresh dead-end",
			shims:         []string{"opencode"},
			agents:        map[string]config.Agent{"opencode": {Enabled: false}},
			wantEnabledID: "opencode",
			wantNewlyLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedIsolatedCLIShims(t, tt.shims...)
			cfg := &config.Config{Agents: map[string]config.Agent{}}
			for id, a := range tt.agents {
				cfg.Agents[id] = a
			}

			newly := DetectAndEnableNewPlatforms(cfg)
			tt.check(t, cfg, newly)
		})
	}
}

// TestDetectAndEnableNewPlatforms_DefaultEnabledNotReEnabled pins that a
// platform absent from the Agents map (IsPlatformEnabled defaults to true) is
// treated as already-enabled and is never added to the newly-enabled set, even
// when installed. Guards against re-announcing default-enabled platforms on
// every refresh.
func TestDetectAndEnableNewPlatforms_DefaultEnabledNotReEnabled(t *testing.T) {
	seedIsolatedCLIShims(t, "codex")
	cfg := &config.Config{Agents: map[string]config.Agent{}} // codex absent → default enabled

	newly := DetectAndEnableNewPlatforms(cfg)

	for _, n := range newly {
		if strings.Contains(strings.ToLower(n), "codex") {
			t.Fatalf("default-enabled codex must not be reported newly-enabled, got %v", newly)
		}
	}
}

// stubExampleBlock is the lifecycle-package mirror of commands.ExampleBlock
// — joins lines with newlines. Sufficient for cobra Example metadata in
// these constructor-level tests.
func stubExampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}

// stubMaximumNArgsWithHints returns a permissive PositionalArgs for tests
// that only need to inspect cmd.Args presence / behavior, not the real
// commands.MaximumNArgsWithHints wording.
func stubMaximumNArgsWithHints(n int, _ ...string) cobra.PositionalArgs {
	return cobra.MaximumNArgs(n)
}

func depsWithRunRefresh(run func(string, bool) error) Deps {
	return Deps{
		ExampleBlock:          stubExampleBlock,
		MaximumNArgsWithHints: stubMaximumNArgsWithHints,
		RunRefresh:            run,
	}
}

// TestNewRefreshCmd_MetadataAndFlag covers the cobra metadata + the
// presence of the --import flag. Positive: a known-good Deps wires
// everything cleanly.
func TestNewRefreshCmd_MetadataAndFlag(t *testing.T) {
	cmd := NewRefreshCmd(depsWithRunRefresh(func(string, bool) error { return nil }))

	if cmd.Use != "refresh [project]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "refresh [project]")
	}
	if cmd.Short == "" {
		t.Error("Short must not be empty")
	}
	if cmd.Flags().Lookup("import") == nil {
		t.Error("expected --import flag wired on lifecycle refresh cmd")
	}
	if cmd.Args == nil {
		t.Error("expected Args validator wired on lifecycle refresh cmd")
	}
	if err := cmd.Args(cmd, []string{"one"}); err != nil {
		t.Errorf("Args should accept 1 arg, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("Args should reject 2 args")
	}
}

// TestNewRefreshCmd_RunEPassesFilterAndImportFlag is the positive end-to-end
// dispatch check: RunE forwards the positional arg as `filter` and the
// parsed --import boolean as `importAlso` to deps.RunRefresh.
func TestNewRefreshCmd_RunEPassesFilterAndImportFlag(t *testing.T) {
	var gotFilter string
	var gotImport bool
	called := false

	cmd := NewRefreshCmd(depsWithRunRefresh(func(filter string, importAlso bool) error {
		called = true
		gotFilter = filter
		gotImport = importAlso
		return nil
	}))

	// Parse the --import flag through cobra so the closure sees the real
	// flag-derived value, not the zero value of the closure variable.
	cmd.SetArgs([]string{"--import", "billing"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !called {
		t.Fatal("RunE never invoked deps.RunRefresh")
	}
	if gotFilter != "billing" {
		t.Errorf("filter = %q, want %q", gotFilter, "billing")
	}
	if !gotImport {
		t.Errorf("importAlso = false, want true (set via --import)")
	}
}

// TestNewRefreshCmd_RunEPropagatesError is the negative path: an error
// returned by deps.RunRefresh surfaces from cmd.Execute unchanged. Pins
// the no-swallowed-error contract for the lifecycle dispatch boundary.
func TestNewRefreshCmd_RunEPropagatesError(t *testing.T) {
	sentinel := errors.New("refresh deliberately failed")
	cmd := NewRefreshCmd(depsWithRunRefresh(func(string, bool) error {
		return sentinel
	}))
	cmd.SetArgs(nil)
	if err := cmd.Execute(); !errors.Is(err, sentinel) {
		t.Errorf("expected RunE to surface sentinel error, got: %v", err)
	}
}

// TestNewRefreshCmd_RunEEmptyArgsPassesEmptyFilter pins the
// "no positional arg → empty filter string" contract.
func TestNewRefreshCmd_RunEEmptyArgsPassesEmptyFilter(t *testing.T) {
	var seenFilter = "sentinel-not-overwritten"
	cmd := NewRefreshCmd(depsWithRunRefresh(func(filter string, _ bool) error {
		seenFilter = filter
		return nil
	}))
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if seenFilter != "" {
		t.Errorf("expected empty filter, got %q", seenFilter)
	}
}
