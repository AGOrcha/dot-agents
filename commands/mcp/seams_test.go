package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// hintBearingError mirrors the parent commands.CLIError shape for the
// findMCPSpec hint-propagation test below. The seam under test is
// Deps.ErrorWithHints — production passes commands.ErrorWithHints (which
// returns *commands.CLIError); this test injects a local stub so the
// subpackage can assert on the propagated hint list without importing
// the parent commands package (which would re-introduce the very
// import cycle this subpackage exists to break).
type hintBearingError struct {
	Message string
	Hints   []string
}

func (e *hintBearingError) Error() string { return e.Message }

func TestFindMCPSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findMCPSpec(Deps{}, agentsHome, "global", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFindMCPSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "found.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	spec, err := findMCPSpec(Deps{}, agentsHome, "global", "found.json")
	if err != nil {
		t.Fatalf("findMCPSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "found.json" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

// TestFindMCPSpec_NotFoundHintsAtList covers the deps.ErrorWithHints seam:
// when the parent supplies an ErrorWithHints constructor, findMCPSpec
// must route the lookup-failure through it (carrying a hint that points
// the user at `da mcp list`). The parent test asserted on *CLIError; the
// subpackage equivalent asserts on the locally-stubbed error type.
func TestFindMCPSpec_NotFoundHintsAtList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "mcp", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := Deps{
		ErrorWithHints: func(message string, hints ...string) error {
			return &hintBearingError{Message: message, Hints: hints}
		},
	}
	_, err := findMCPSpec(deps, agentsHome, "global", "absent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var hintErr *hintBearingError
	if !errors.As(err, &hintErr) {
		t.Fatalf("expected *hintBearingError, got %T", err)
	}
	if !strings.Contains(strings.Join(hintErr.Hints, " "), "da mcp list") {
		t.Errorf("missing hint pointing at `da mcp list`: %v", hintErr.Hints)
	}
}

// TestFindMCPSpec_NotFoundFallbackWithoutDeps covers the fmt.Errorf
// fallback path inside hintErr — exercised when the parent has not
// supplied an ErrorWithHints (e.g. the bare-call RunShow paths). Without
// this assertion the fallback branch would only be exercised
// transitively via TestRunShow_NotFound and would not document the
// expected error shape.
func TestFindMCPSpec_NotFoundFallbackWithoutDeps(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "mcp", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findMCPSpec(Deps{}, agentsHome, "global", "absent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "MCP file not found") {
		t.Errorf("error = %q; want the not-found message", err.Error())
	}
}
