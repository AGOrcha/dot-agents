package rules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/platform"
)

// withReadFileStub swaps osReadFile for the duration of the test.
func withReadFileStub(t *testing.T, stub func(string) ([]byte, error)) {
	t.Helper()
	orig := osReadFile
	osReadFile = stub
	t.Cleanup(func() { osReadFile = orig })
}

// withListCanonicalRuleFilesStub swaps the platform.ListCanonicalRuleFiles seam.
func withListCanonicalRuleFilesStub(t *testing.T, stub func(string, string) ([]platform.RuleFileSpec, error)) {
	t.Helper()
	orig := platformListCanonicalRuleFiles
	platformListCanonicalRuleFiles = stub
	t.Cleanup(func() { platformListCanonicalRuleFiles = orig })
}

// withResolveCanonicalRuleFileStub swaps the platform.ResolveCanonicalRuleFile
// seam.
func withResolveCanonicalRuleFileStub(t *testing.T, stub func(string, string, string) (*platform.RuleFileSpec, error)) {
	t.Helper()
	orig := platformResolveCanonicalRuleFile
	platformResolveCanonicalRuleFile = stub
	t.Cleanup(func() { platformResolveCanonicalRuleFile = orig })
}

// ─── ExtractRuleFrontmatterDescription read-error branch ─────────────────────

// TestExtractRuleFrontmatterDescription_ReadError exercises the err != nil
// branch in ExtractRuleFrontmatterDescription. A regular missing-file path
// only fires os.IsNotExist; this seam test injects an arbitrary read error to
// cover the swallowed-error guard.
func TestExtractRuleFrontmatterDescription_ReadError(t *testing.T) {
	sentinel := errors.New("read boom")
	withReadFileStub(t, func(string) ([]byte, error) { return nil, sentinel })

	if got := ExtractRuleFrontmatterDescription("/whatever"); got != "" {
		t.Errorf("read-error path should yield empty string, got %q", got)
	}
}

// ─── canonicalSpec.List error branch ─────────────────────────────────────────

// TestRunList_ListErrorPropagates injects a non-not-exist filesystem error
// into platform.ListCanonicalRuleFiles. RunCanonicalList must surface that
// error rather than swallow it as an empty-scope message.
func TestRunList_ListErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	sentinel := errors.New("list boom")
	withListCanonicalRuleFilesStub(t, func(string, string) ([]platform.RuleFileSpec, error) {
		return nil, sentinel
	})

	err := RunList(testDeps(false, false, false), "global")
	if err == nil {
		t.Fatal("expected error from list seam")
	}
	if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), "list boom") {
		t.Errorf("expected sentinel surfaced, got %v", err)
	}
}

// ─── findRuleSpec resolve-error path ────────────────────────────────────────

// TestFindRuleSpec_ResolveError covers the err != nil branch where
// platform.ResolveCanonicalRuleFile returns an error. The wrapper must
// translate that into a hint-aware *CLIError via deps.ErrorWithHints.
func TestFindRuleSpec_ResolveError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	withResolveCanonicalRuleFileStub(t, func(string, string, string) (*platform.RuleFileSpec, error) {
		return nil, errors.New("resolve boom")
	})

	_, err := FindRuleSpec(testDeps(false, false, false), agentsHome, "global", "anything.md")
	if err == nil {
		t.Fatal("expected wrapped resolve error")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !strings.Contains(strings.Join(cliErr.Hints, " "), "da rules list") {
		t.Errorf("expected hint, got %v", cliErr.Hints)
	}
}
