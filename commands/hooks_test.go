package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/platform"
)

func TestEnsureUnderHooksScopeTree(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "global"
	root := filepath.Join(agentsHome, "hooks", scope)
	bundle := filepath.Join(root, "my-hook")
	if err := os.MkdirAll(bundle, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensureUnderHooksScopeTree(agentsHome, scope, bundle); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := ensureUnderHooksScopeTree(agentsHome, scope, outside); err == nil {
		t.Fatal("expected refusal for path outside hooks tree")
	}
}

func TestFindHookSpec(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "proj-a"
	hookDir := filepath.Join(agentsHome, "hooks", scope, "alpha")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(hookDir, "HOOK.yaml")
	if err := os.WriteFile(manifest, []byte(`name: alpha
when: pre_tool_use
run:
  command: ./x.sh
`), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := findHookSpec(agentsHome, scope, "alpha")
	if err != nil {
		t.Fatalf("findHookSpec: %v", err)
	}
	if got.Name != "alpha" || got.SourceKind != platform.HookSourceCanonicalBundle {
		t.Fatalf("unexpected spec: %#v", got)
	}
	if _, err := findHookSpec(agentsHome, scope, "missing"); err == nil {
		t.Fatal("expected error for missing hook")
	}
}

func TestRunHooksListCanonicalAndLegacy(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	root := filepath.Join(agentsHome, "hooks", scope)
	if err := os.MkdirAll(filepath.Join(root, "bundle-a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundle-a", "HOOK.yaml"), []byte(`name: bundle-a
when: session_start
run:
  command: echo hi
enabled_on: [claude]
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "legacy.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	oldYes := Flags.Yes
	Flags.Yes = true
	defer func() { Flags.Yes = oldYes }()

	if err := runHooksList(scope); err != nil {
		t.Fatalf("runHooksList: %v", err)
	}
}

func TestRunHooksRemoveCanonicalBundle(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	hookDir := filepath.Join(agentsHome, "hooks", scope, "to-drop")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "HOOK.yaml"), []byte(`name: to-drop
when: stop
run:
  command: true
`), 0644); err != nil {
		t.Fatal(err)
	}
	oldYes := Flags.Yes
	Flags.Yes = true
	defer func() { Flags.Yes = oldYes }()

	if err := runHooksRemove(scope, "to-drop"); err != nil {
		t.Fatalf("runHooksRemove: %v", err)
	}
	if _, err := os.Stat(hookDir); !os.IsNotExist(err) {
		t.Fatalf("expected bundle dir removed, stat err=%v", err)
	}
}

func TestRunHooksRemoveLegacyJSON(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	p := filepath.Join(agentsHome, "hooks", scope, "x.json")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	oldYes := Flags.Yes
	Flags.Yes = true
	defer func() { Flags.Yes = oldYes }()

	if err := runHooksRemove(scope, "x"); err != nil {
		t.Fatalf("runHooksRemove: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("expected legacy json removed")
	}
}

func TestRunHooksShow(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	hookDir := filepath.Join(agentsHome, "hooks", scope, "show-me")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "HOOK.yaml"), []byte(`name: show-me
description: test
when: pre_tool_use
run:
  command: ./run.sh
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runHooksShow(scope, "show-me"); err != nil {
		t.Fatalf("runHooksShow: %v", err)
	}
}

func TestRunHooksListFallsBackToSettingsJSON(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "global"
	settingsDir := filepath.Join(agentsHome, "settings", scope)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(settingsDir, "claude-code.json")
	payload := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"echo legacy"}]}]}}`
	if err := os.WriteFile(claude, []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runHooksList(scope); err != nil {
		t.Fatalf("runHooksList: %v", err)
	}
}

func TestFindHookSpecNotFoundMessage(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "g"
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks", scope), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := findHookSpec(agentsHome, scope, "nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hook not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
