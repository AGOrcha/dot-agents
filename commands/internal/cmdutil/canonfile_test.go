package cmdutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns the
// captured output. Tests use it to assert which branch of the canonical
// helpers fired (Info / Header / Success).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf strings.Builder
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	<-done
	os.Stdout = orig
	return buf.String()
}

// setAgentsHome points AgentsHome() at a temp dir for the test and reverts
// on cleanup. AgentsHome reads AGENTS_HOME first, so this is sufficient.
func setAgentsHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, hadPrev := os.LookupEnv("AGENTS_HOME")
	if err := os.Setenv("AGENTS_HOME", dir); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("AGENTS_HOME", prev)
		} else {
			_ = os.Unsetenv("AGENTS_HOME")
		}
	})
	return dir
}

// fakeSpec returns a CanonicalFileSpec wired against a writable filesystem
// rooted at agentsHome. It mimics how settings.go / mcp.go / rules.go shape
// their per-subcommand specs, but uses simple file conventions so the
// helper logic in canonfile.go is exercised directly.
func fakeSpec(agentsHome, segment string) CanonicalFileSpec {
	return CanonicalFileSpec{
		Kind:        "Fake",
		DirSegment:  segment,
		SingularRem: "fake file",
		EmptyHint: func(scope string) string {
			return "EMPTY:" + scope
		},
		List: func(home, scope string) ([]CanonicalFileEntry, error) {
			dir := filepath.Join(home, segment, scope)
			ents, err := os.ReadDir(dir)
			if err != nil {
				return nil, err
			}
			out := make([]CanonicalFileEntry, 0, len(ents))
			for _, e := range ents {
				if e.IsDir() {
					continue
				}
				out = append(out, CanonicalFileEntry{
					Scope:      scope,
					BaseName:   e.Name(),
					SourcePath: filepath.Join(dir, e.Name()),
				})
			}
			return out, nil
		},
		Resolve: func(home, scope, name string) (CanonicalFileEntry, error) {
			path := filepath.Join(home, segment, scope, name)
			info, err := os.Stat(path)
			if err != nil {
				return CanonicalFileEntry{}, err
			}
			return CanonicalFileEntry{
				Scope:      scope,
				BaseName:   info.Name(),
				SourcePath: path,
			}, nil
		},
		EnsureScope: func(home, scope, target string) error {
			prefix := filepath.Join(home, segment, scope) + string(os.PathSeparator)
			if !strings.HasPrefix(target, prefix) {
				return errors.New("outside scope")
			}
			return nil
		},
	}
}

func TestRunCanonicalList_MissingDir_DefaultHint(t *testing.T) {
	setAgentsHome(t)
	spec := fakeSpec(os.Getenv("AGENTS_HOME"), "settings")

	out := captureStdout(t, func() {
		if err := RunCanonicalList("user", spec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "No ~/.agents/settings/user/ directory yet") {
		t.Fatalf("expected default missing-dir hint, got: %q", out)
	}
}

func TestRunCanonicalList_MissingDir_CustomHint(t *testing.T) {
	setAgentsHome(t)
	spec := fakeSpec(os.Getenv("AGENTS_HOME"), "rules")
	spec.MissingDirHint = func(scope string) string { return "CUSTOM-MISSING:" + scope }

	out := captureStdout(t, func() {
		if err := RunCanonicalList("project", spec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "CUSTOM-MISSING:project") {
		t.Fatalf("expected custom hint, got: %q", out)
	}
}

func TestRunCanonicalList_EmptyDir(t *testing.T) {
	home := setAgentsHome(t)
	if err := os.MkdirAll(filepath.Join(home, "mcp", "user"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	spec := fakeSpec(home, "mcp")

	out := captureStdout(t, func() {
		if err := RunCanonicalList("user", spec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "EMPTY:user") {
		t.Fatalf("expected empty hint, got: %q", out)
	}
}

func TestRunCanonicalList_WithEntries(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "settings", "user")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "alpha.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "beta.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "settings")

	out := captureStdout(t, func() {
		if err := RunCanonicalList("user", spec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "alpha.json") || !strings.Contains(out, "beta.json") {
		t.Fatalf("expected entries listed, got: %q", out)
	}
	if !strings.Contains(out, "Fake (user)") {
		t.Fatalf("expected header, got: %q", out)
	}
}

func TestRunCanonicalList_PropagatesNonNotExistError(t *testing.T) {
	setAgentsHome(t)
	spec := fakeSpec(os.Getenv("AGENTS_HOME"), "settings")
	sentinel := errors.New("boom")
	spec.List = func(home, scope string) ([]CanonicalFileEntry, error) {
		return nil, sentinel
	}

	err := RunCanonicalList("user", spec)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}
}

func TestRunCanonicalShow_ResolveError(t *testing.T) {
	setAgentsHome(t)
	spec := fakeSpec(os.Getenv("AGENTS_HOME"), "settings")

	err := RunCanonicalShow("user", "missing.json", spec)
	if err == nil {
		t.Fatalf("expected error for missing entry")
	}
}

func TestRunCanonicalShow_PrintsMetadataAndExtras(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "rules", "user")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fpath := filepath.Join(scopeDir, "rule.md")
	if err := os.WriteFile(fpath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "rules")

	var gotPath string
	out := captureStdout(t, func() {
		err := RunCanonicalShow("user", "rule.md", spec, func(p string) {
			gotPath = p
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if gotPath != fpath {
		t.Fatalf("extra callback got %q, want %q", gotPath, fpath)
	}
	if !strings.Contains(out, "rule.md") {
		t.Fatalf("expected basename in output, got: %q", out)
	}
	if !strings.Contains(out, "size:") {
		t.Fatalf("expected size in output, got: %q", out)
	}
}

func TestRunCanonicalRemove_NotFound(t *testing.T) {
	setAgentsHome(t)
	spec := fakeSpec(os.Getenv("AGENTS_HOME"), "settings")

	err := RunCanonicalRemove(RemoveDeps{Yes: true}, "user", "ghost.json", spec)
	if err == nil {
		t.Fatalf("expected error for missing entry")
	}
}

func TestRunCanonicalRemove_OutsideScope(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "settings", "user")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fpath := filepath.Join(scopeDir, "config.json")
	if err := os.WriteFile(fpath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "settings")
	spec.EnsureScope = func(home, scope, target string) error {
		return errors.New("scope mismatch")
	}

	err := RunCanonicalRemove(RemoveDeps{Yes: true}, "user", "config.json", spec)
	if err == nil || !strings.Contains(err.Error(), "scope mismatch") {
		t.Fatalf("expected scope mismatch error, got: %v", err)
	}
	// File must NOT be removed.
	if _, statErr := os.Stat(fpath); statErr != nil {
		t.Fatalf("file should remain after scope error, got: %v", statErr)
	}
}

func TestRunCanonicalRemove_DryRun(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "settings", "user")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fpath := filepath.Join(scopeDir, "config.json")
	if err := os.WriteFile(fpath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "settings")

	out := captureStdout(t, func() {
		err := RunCanonicalRemove(RemoveDeps{DryRun: true}, "user", "config.json", spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "DRY RUN") {
		t.Fatalf("expected dry-run banner, got: %q", out)
	}
	if _, statErr := os.Stat(fpath); statErr != nil {
		t.Fatalf("file should remain after dry run, got: %v", statErr)
	}
}

func TestRunCanonicalRemove_Force(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "mcp", "user")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fpath := filepath.Join(scopeDir, "server.json")
	if err := os.WriteFile(fpath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "mcp")

	out := captureStdout(t, func() {
		err := RunCanonicalRemove(RemoveDeps{Force: true}, "user", "server.json", spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "Removed") {
		t.Fatalf("expected success message, got: %q", out)
	}
	if _, statErr := os.Stat(fpath); !os.IsNotExist(statErr) {
		t.Fatalf("file should be removed, stat err: %v", statErr)
	}
}

func TestRunCanonicalRemove_Yes(t *testing.T) {
	home := setAgentsHome(t)
	scopeDir := filepath.Join(home, "settings", "project")
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fpath := filepath.Join(scopeDir, "config.json")
	if err := os.WriteFile(fpath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	spec := fakeSpec(home, "settings")

	out := captureStdout(t, func() {
		err := RunCanonicalRemove(RemoveDeps{Yes: true}, "project", "config.json", spec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "Removed") {
		t.Fatalf("expected success message, got: %q", out)
	}
	if _, statErr := os.Stat(fpath); !os.IsNotExist(statErr) {
		t.Fatalf("file should be removed, stat err: %v", statErr)
	}
}

func TestMissingDirMessage_Defaults(t *testing.T) {
	spec := CanonicalFileSpec{DirSegment: "settings"}
	msg := missingDirMessage("user", spec)
	if !strings.Contains(msg, "~/.agents/settings/user/") {
		t.Fatalf("default missing-dir message wrong: %q", msg)
	}

	spec.MissingDirHint = func(scope string) string { return "X:" + scope }
	if got := missingDirMessage("project", spec); got != "X:project" {
		t.Fatalf("custom hint not used, got: %q", got)
	}
}
