package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// writeBundleHook creates a minimal canonical hook bundle on disk under
// AGENTS_HOME so findHookSpec resolves successfully and runHooksRemove
// proceeds to the seam-guarded helper calls.
func writeBundleHook(t *testing.T, agentsHome, scope, name string) {
	t.Helper()
	dir := filepath.Join(agentsHome, "hooks", scope, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HOOK.yaml"), []byte("name: "+name+`
when: stop
run:
  command: true
`), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestRunHooksRemovePropagatesHookRemovalTargetError forces the
// hookRemovalTargetFn seam to fail and asserts runHooksRemove returns
// exactly that error (the guard right after hookRemovalTarget).
func TestRunHooksRemovePropagatesHookRemovalTargetError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	writeBundleHook(t, agentsHome, scope, "alpha")

	sentinel := errors.New("forced hookRemovalTarget failure")
	orig := hookRemovalTargetFn
	hookRemovalTargetFn = func(*platform.HookSpec) (string, error) {
		return "", sentinel
	}
	t.Cleanup(func() { hookRemovalTargetFn = orig })

	deps := testDeps()
	deps.Flags.Yes = true
	err := runHooksRemove(deps, scope, "alpha")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected runHooksRemove to propagate the hookRemovalTarget error verbatim, got: %v", err)
	}
	// The bundle must remain — removal never ran.
	if _, statErr := os.Stat(filepath.Join(agentsHome, "hooks", scope, "alpha")); statErr != nil {
		t.Fatalf("bundle must be intact when hookRemovalTarget fails, stat err=%v", statErr)
	}
}

// TestRunHooksRemovePropagatesScopeTreeGuardError forces the
// ensureUnderHooksScopeTreeFn seam to fail and asserts runHooksRemove
// returns exactly that error (the guard right after the scope-tree check).
func TestRunHooksRemovePropagatesScopeTreeGuardError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	writeBundleHook(t, agentsHome, scope, "beta")

	sentinel := errors.New("forced scope-tree guard failure")
	orig := ensureUnderHooksScopeTreeFn
	ensureUnderHooksScopeTreeFn = func(string, string, string) error {
		return sentinel
	}
	t.Cleanup(func() { ensureUnderHooksScopeTreeFn = orig })

	deps := testDeps()
	deps.Flags.Yes = true
	err := runHooksRemove(deps, scope, "beta")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected runHooksRemove to propagate the scope-tree guard error verbatim, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(agentsHome, "hooks", scope, "beta")); statErr != nil {
		t.Fatalf("bundle must be intact when the scope-tree guard fails, stat err=%v", statErr)
	}
}

// TestHookRemovalTargetUnsupportedKind covers the default branch of
// hookRemovalTarget: an unknown source kind must produce a descriptive
// error rather than a path.
func TestHookRemovalTargetUnsupportedKind(t *testing.T) {
	spec := &platform.HookSpec{
		Name:       "weird",
		SourcePath: "/tmp/whatever/HOOK.yaml",
		SourceKind: platform.HookSourceKind("mystery"),
	}
	got, err := hookRemovalTarget(spec)
	if err == nil {
		t.Fatalf("expected error for unsupported kind, got target %q", got)
	}
	if !strings.Contains(err.Error(), "unsupported hook source kind") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty target on error, got %q", got)
	}
}

// TestHookRemovalTargetLegacyFile pins the legacy-file branch: the target
// is the file path itself, unchanged.
func TestHookRemovalTargetLegacyFile(t *testing.T) {
	spec := &platform.HookSpec{
		Name:       "old",
		SourcePath: "/tmp/x/old.json",
		SourceKind: platform.HookSourceLegacyFile,
	}
	got, err := hookRemovalTarget(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp/x/old.json" {
		t.Fatalf("legacy target should be the file path, got %q", got)
	}
}

// TestEnsureUnderHooksScopeTreeRelError forces filepath.Rel to fail by
// mixing a relative root (relative agentsHome) with an absolute target,
// which Rel cannot relativize.
func TestEnsureUnderHooksScopeTreeRelError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relative/absolute Rel semantics differ on windows")
	}
	// agentsHome relative => root relative; target absolute => Rel errors.
	err := ensureUnderHooksScopeTree("rel-home", "global", "/abs/target/path")
	if err == nil {
		t.Fatal("expected filepath.Rel error for relative root vs absolute target")
	}
}

// TestRunHooksRemoveRejectsTargetOutsideScopeTree drives runHooksRemove
// against a legacy hook whose resolved removal target escapes the scope
// tree via a traversal name, hitting the ensureUnderHooksScopeTree guard.
func TestRunHooksRemoveRejectsEscapingTarget(t *testing.T) {
	// hookRemovalTarget for a legacy file returns spec.SourcePath as-is.
	// We can't easily craft an escaping legacy file through ListHookSpecs,
	// so assert the guard directly with a path that resolves outside.
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "g"
	outside := filepath.Join(tmp, "elsewhere", "evil.json")
	if err := ensureUnderHooksScopeTree(agentsHome, scope, outside); err == nil {
		t.Fatal("expected refusal for target outside scope tree")
	} else if !strings.Contains(err.Error(), "refusing to remove path outside") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunHooksRemoveBundleRemoveAllError makes the scope directory
// read-only so os.RemoveAll on the bundle fails, exercising the
// "removing bundle" error wrap in runHooksRemove.
func TestRunHooksRemoveBundleRemoveAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission model required to block RemoveAll")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	scopeDir := filepath.Join(agentsHome, "hooks", scope)
	hookDir := filepath.Join(scopeDir, "locked")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "HOOK.yaml"), []byte(`name: locked
when: stop
run:
  command: true
`), 0644); err != nil {
		t.Fatal(err)
	}
	// Make the parent (scope dir) read-only so the bundle dir entry can't
	// be unlinked; restore afterward so t.TempDir cleanup succeeds.
	if err := os.Chmod(scopeDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(scopeDir, 0755) })

	deps := testDeps()
	deps.Flags.Yes = true
	err := runHooksRemove(deps, scope, "locked")
	if err == nil {
		t.Fatal("expected error removing bundle under read-only parent")
	}
	if !strings.Contains(err.Error(), "removing bundle") {
		t.Fatalf("expected 'removing bundle' wrap, got: %v", err)
	}
}

// TestRunHooksRemoveLegacyRemoveError makes the scope directory read-only
// after creating a legacy json hook so os.Remove fails, exercising the
// "removing file" error wrap.
func TestRunHooksRemoveLegacyRemoveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission model required to block Remove")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	scopeDir := filepath.Join(agentsHome, "hooks", scope)
	if err := os.MkdirAll(scopeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "leg.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scopeDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(scopeDir, 0755) })

	deps := testDeps()
	deps.Flags.Yes = true
	err := runHooksRemove(deps, scope, "leg")
	if err == nil {
		t.Fatal("expected error removing legacy file under read-only parent")
	}
	if !strings.Contains(err.Error(), "removing file") {
		t.Fatalf("expected 'removing file' wrap, got: %v", err)
	}
}

// TestRunHooksRemoveDryRun confirms the dry-run path returns early without
// deleting anything.
func TestRunHooksRemoveDryRun(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	hookDir := filepath.Join(agentsHome, "hooks", scope, "keep")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "HOOK.yaml"), []byte(`name: keep
when: stop
run:
  command: true
`), 0644); err != nil {
		t.Fatal(err)
	}
	deps := testDeps()
	deps.Flags.DryRun = true
	if err := runHooksRemove(deps, scope, "keep"); err != nil {
		t.Fatalf("dry-run remove: %v", err)
	}
	if _, err := os.Stat(hookDir); err != nil {
		t.Fatalf("dry-run must not delete the bundle, stat err=%v", err)
	}
}
