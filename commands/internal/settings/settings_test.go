package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// hintError is the local stub for tests that need to assert hint payloads
// or use errors.As against the deps-injected error type. Mirrors the same
// pattern used by commands/agents/agents_test.go's hintError.
type hintError struct {
	message string
	hints   []string
}

func (e *hintError) Error() string {
	return e.message
}

// stubDeps returns a Deps with hint helpers wired to construct hintError
// values, so findSettingsSpec / RunRemove error paths produce assertable
// shapes without depending on the parent commands package.
func stubDeps(dryRun, yes, force bool) Deps {
	return Deps{
		Flags: GlobalFlags{DryRun: dryRun, Yes: yes, Force: force},
		ErrorWithHints: func(message string, hints ...string) error {
			return &hintError{message: message, hints: hints}
		},
		UsageError: func(message string, hints ...string) error {
			return &hintError{message: message, hints: hints}
		},
	}
}

func TestRunSettingsList_ListsSettings(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte(`{"editor.fontSize": 14}`))

	if err := RunList("global"); err != nil {
		t.Fatalf("RunList: %v", err)
	}
}

func TestRunSettingsList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunList("global"); err != nil {
		t.Fatalf("RunList with empty scope: %v", err)
	}
}

func TestRunSettingsList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunList("nonexistent"); err != nil {
		t.Fatalf("RunList with missing scope: %v", err)
	}
}

func TestRunSettingsShow_ReadsSettingsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "claude-code.json", []byte(`{"theme": "dark"}`))

	if err := RunShow(stubDeps(false, false, false), "global", "claude-code.json"); err != nil {
		t.Fatalf("RunShow: %v", err)
	}
}

func TestRunSettingsShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	settingsDir := filepath.Join(agentsHome, "settings", "global")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := RunShow(stubDeps(false, false, false), "global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing settings file")
	}
}

func TestFindSettingsSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findSettingsSpec(stubDeps(false, false, false), agentsHome, "global", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFindSettingsSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	spec, err := findSettingsSpec(stubDeps(false, false, false), agentsHome, "global", "cursor.json")
	if err != nil {
		t.Fatalf("findSettingsSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "cursor.json" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestFindSettingsSpec_NotFoundHintsAtList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findSettingsSpec(stubDeps(false, false, false), agentsHome, "global", "absent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var hErr *hintError
	if !errors.As(err, &hErr) {
		t.Fatalf("expected *hintError, got %T", err)
	}
	if !strings.Contains(strings.Join(hErr.hints, " "), "da settings list") {
		t.Errorf("missing hint pointing at `da settings list`: %v", hErr.hints)
	}
}

func TestRunSettingsRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunRemove(stubDeps(true, false, false), "global", "cursor.json"); err != nil {
		t.Fatalf("RunRemove dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "settings", "global", "cursor.json")); err != nil {
		t.Fatalf("dry-run should preserve file: %v", err)
	}
}

func TestRunSettingsRemove_Force_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "settings", "global", "cursor.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunRemove(stubDeps(false, true, false), "global", "cursor.json"); err != nil {
		t.Fatalf("RunRemove force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "settings", "global", "cursor.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed; stat err = %v", err)
	}
}

// TestSettingsErrorWithHints_FallbackUnwired covers the no-Deps path through
// settingsErrorWithHints: when Deps.ErrorWithHints is nil, the helper must
// degrade to fmt.Errorf, with and without a hint payload. Both branches were
// uncovered by the existing tests (which always wire stubDeps) and pulled the
// file below the 95% per-file gate.
func TestSettingsErrorWithHints_FallbackUnwired(t *testing.T) {
	t.Run("no_hints", func(t *testing.T) {
		err := settingsErrorWithHints(Deps{}, "boom")
		if err == nil || err.Error() != "boom" {
			t.Fatalf("expected plain message, got %v", err)
		}
	})
	t.Run("with_hints", func(t *testing.T) {
		err := settingsErrorWithHints(Deps{}, "boom", "first hint", "second hint")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "first hint") {
			t.Fatalf("expected message+first hint, got %v", err)
		}
	})
}

// TestSettingsUsageError_FallbackUnwired mirrors the above for the usage-error
// helper: when Deps.UsageError is nil, returns a plain fmt.Errorf (with or
// without hint).
func TestSettingsUsageError_FallbackUnwired(t *testing.T) {
	t.Run("no_hints", func(t *testing.T) {
		err := settingsUsageError(Deps{}, "bad usage")
		if err == nil || err.Error() != "bad usage" {
			t.Fatalf("expected plain message, got %v", err)
		}
	})
	t.Run("with_hints", func(t *testing.T) {
		err := settingsUsageError(Deps{}, "bad usage", "try --help")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "bad usage") || !strings.Contains(err.Error(), "try --help") {
			t.Fatalf("expected message+hint, got %v", err)
		}
	})
}

// TestFindSettingsSpec_EmptyName_FallbackUnwired confirms the empty-name
// branch of findSettingsSpec degrades cleanly when Deps carries no helpers —
// this is the only caller of settingsUsageError today, so the fallback path
// must be reachable end-to-end.
func TestFindSettingsSpec_EmptyName_FallbackUnwired(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findSettingsSpec(Deps{}, agentsHome, "global", "   ")
	if err == nil {
		t.Fatal("expected error for empty/whitespace name")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected 'empty' in error, got %v", err)
	}
}
