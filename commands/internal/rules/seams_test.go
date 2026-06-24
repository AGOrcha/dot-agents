package rules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/platform"
)

// fakeRuleIO is the test ruleIO. Each nil func-field delegates to the real
// stdRuleIO implementation, so a test overrides only the operation it wants
// to fault-inject. Per docs/TEST_SEAMS.md this replaces the legacy
// withReadFileStub / withListCanonicalRuleFilesStub / withResolveCanonicalRuleFileStub
// package func-var swaps.
type fakeRuleIO struct {
	readFile func(string) ([]byte, error)
	list     func(string, string) ([]platform.RuleFileSpec, error)
	resolve  func(string, string, string) (*platform.RuleFileSpec, error)
}

func (f fakeRuleIO) ReadFile(name string) ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(name)
	}
	return stdRuleIO{}.ReadFile(name)
}

func (f fakeRuleIO) ListCanonicalRuleFiles(agentsHome, scope string) ([]platform.RuleFileSpec, error) {
	if f.list != nil {
		return f.list(agentsHome, scope)
	}
	return stdRuleIO{}.ListCanonicalRuleFiles(agentsHome, scope)
}

func (f fakeRuleIO) ResolveCanonicalRuleFile(agentsHome, scope, name string) (*platform.RuleFileSpec, error) {
	if f.resolve != nil {
		return f.resolve(agentsHome, scope, name)
	}
	return stdRuleIO{}.ResolveCanonicalRuleFile(agentsHome, scope, name)
}

// ─── ExtractRuleFrontmatterDescription read-error branch ─────────────────────

// TestExtractRuleFrontmatterDescription_ReadError exercises the err != nil
// branch in ExtractRuleFrontmatterDescription. A regular missing-file path
// only fires os.IsNotExist; this seam test injects an arbitrary read error to
// cover the swallowed-error guard.
func TestExtractRuleFrontmatterDescription_ReadError(t *testing.T) {
	sentinel := errors.New("read boom")
	deps := testDeps(false, false, false)
	deps.IO = fakeRuleIO{readFile: func(string) ([]byte, error) { return nil, sentinel }}

	if got := ExtractRuleFrontmatterDescription(deps, "/whatever"); got != "" {
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
	deps := testDeps(false, false, false)
	deps.IO = fakeRuleIO{list: func(string, string) ([]platform.RuleFileSpec, error) {
		return nil, sentinel
	}}

	err := RunList(deps, "global")
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

	deps := testDeps(false, false, false)
	deps.IO = fakeRuleIO{resolve: func(string, string, string) (*platform.RuleFileSpec, error) {
		return nil, errors.New("resolve boom")
	}}

	_, err := FindRuleSpec(deps, agentsHome, "global", "anything.md")
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
