package lifecycle

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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
