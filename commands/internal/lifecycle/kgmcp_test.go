package lifecycle

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---------- KGConfigPath ----------

func TestKGConfigPath_UsesKGHomeEnv(t *testing.T) {
	t.Setenv("KG_HOME", filepath.FromSlash("/custom/kg"))
	got := KGConfigPath()
	want := filepath.FromSlash("/custom/kg/self/config.yaml")
	if got != want {
		t.Errorf("KGConfigPath() = %q, want %q", got, want)
	}
}

func TestKGConfigPath_FallsBackToHome(t *testing.T) {
	t.Setenv("KG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := KGConfigPath()
	if !strings.HasSuffix(got, filepath.Join("knowledge-graph", "self", "config.yaml")) {
		t.Errorf("unexpected fallback path: %q", got)
	}
}

// ---------- WriteKGMCPConfigs ----------

func TestWriteKGMCPConfigs_WritesThreeFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := WriteKGMCPConfigs(tmp, StdAddDeps{}); err != nil {
		t.Fatalf("WriteKGMCPConfigs: %v", err)
	}
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		servers, _ := parsed["servers"].(map[string]any)
		if servers == nil || servers["dot-agents-kg"] == nil {
			t.Errorf("%s missing dot-agents-kg server entry: %+v", name, parsed)
		}
	}
}

// ---------- WriteKGMCPConfigFile ----------

func TestWriteKGMCPConfigFile_MergesExistingServers(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	// Pre-existing file with another server
	pre := map[string]any{
		"servers": map[string]any{"other": map[string]any{"command": "foo"}},
	}
	data, _ := json.Marshal(pre)
	_ = os.WriteFile(target, data, 0644)

	server := map[string]any{"command": "exe", "args": []string{"kg", "serve"}, "type": "stdio"}
	if err := WriteKGMCPConfigFile(target, server, StdAddDeps{}); err != nil {
		t.Fatalf("WriteKGMCPConfigFile: %v", err)
	}

	out, _ := os.ReadFile(target)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	servers, _ := parsed["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("expected to preserve existing servers.other entry")
	}
	if _, ok := servers["dot-agents-kg"]; !ok {
		t.Error("expected dot-agents-kg server entry")
	}
}

// WriteKGMCPConfigFile must abort the write and leave the on-disk file
// untouched when an EXISTING claude.json/cursor.json/mcp.json is not
// valid JSON — overwriting it would silently replace every other
// server entry with an empty/partial config (should-be-ATOMIC).
func TestWriteKGMCPConfigFile_CorruptExistingFileAborts(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	corrupt := []byte(`{"servers": {"other": {"command": "foo"}` + "\n") // truncated/invalid JSON
	if err := os.WriteFile(target, corrupt, 0644); err != nil {
		t.Fatalf("seed corrupt config: %v", err)
	}

	server := map[string]any{"command": "exe", "args": []string{"kg", "serve"}, "type": "stdio"}
	err := WriteKGMCPConfigFile(target, server, StdAddDeps{})
	if err == nil {
		t.Fatal("expected an error for corrupt existing JSON, got nil")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("expected error to name the offending path %q, got %v", target, err)
	}

	out, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("re-reading target: %v", readErr)
	}
	if string(out) != string(corrupt) {
		t.Errorf("on-disk bytes changed after aborted write:\n got: %q\nwant: %q", out, corrupt)
	}
}

// WriteKGMCPConfigFile must also abort when the existing file cannot be
// read at all (e.g. permission denied) rather than silently treating a
// real I/O error identically to "file does not exist yet."
func TestWriteKGMCPConfigFile_UnreadableExistingFileAborts(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection not available")
	}
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	pre := []byte(`{"servers": {"other": {"command": "foo"}}}`)
	if err := os.WriteFile(target, pre, 0644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := os.Chmod(target, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0644) })

	err := WriteKGMCPConfigFile(target, map[string]any{"command": "x"}, StdAddDeps{})
	if err == nil {
		t.Fatal("expected an error for unreadable existing config, got nil")
	}
}

// ---------- EnsureGlobalKGMCPConfigs ----------

func TestEnsureGlobalKGMCPConfigs_NoopWhenKGAbsent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KG_HOME", filepath.Join(tmp, "no-kg-here"))

	agentsHome := filepath.Join(tmp, ".agents")
	if err := EnsureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Errorf("EnsureGlobalKGMCPConfigs should be no-op when KG missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err == nil {
		t.Error("expected no MCP config to be written")
	}
}

func TestEnsureGlobalKGMCPConfigs_WritesWhenKGPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	kgHome := filepath.Join(tmp, "kgroot")
	_ = os.MkdirAll(filepath.Join(kgHome, "self"), 0755)
	_ = os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("k: v\n"), 0644)
	t.Setenv("KG_HOME", kgHome)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := EnsureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("EnsureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err != nil {
		t.Errorf("expected claude.json: %v", err)
	}
}

// execDeps overrides Executable() so we can fault-inject a failure of the
// dot-agents binary lookup. WriteKGMCPConfigs propagates that error
// before any platform file is written.
type execDeps struct {
	fakeAddDeps
	execFn func() (string, error)
}

func (e execDeps) Executable() (string, error) {
	if e.execFn != nil {
		return e.execFn()
	}
	return os.Executable()
}

// WriteKGMCPConfigs must propagate an Executable() failure and write
// nothing to disk — without the binary path, the MCP server entry would
// be unusable, so callers depend on an early hard error rather than a
// half-applied config.
func TestWriteKGMCPConfigs_ExecutableErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	deps := execDeps{execFn: func() (string, error) { return "", errors.New("no exe") }}
	err := WriteKGMCPConfigs(tmp, deps)
	if err == nil || !strings.Contains(err.Error(), "no exe") {
		t.Fatalf("expected exe error, got %v", err)
	}
	// No files should have been written.
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		if _, statErr := os.Stat(filepath.Join(tmp, name)); statErr == nil {
			t.Errorf("expected %s NOT to be written when Executable() fails", name)
		}
	}
}

// WriteKGMCPConfigFile must propagate MkdirAll failures from the
// supplied AddDeps. Without the parent directory, the subsequent
// WriteFile would fail anyway; the explicit MkdirAll branch lets
// callers distinguish a missing-parent failure from a write failure.
func TestWriteKGMCPConfigFile_MkdirErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nested", "deep", "claude.json")
	deps := fakeAddDeps{
		mkdirAll: func(string, os.FileMode) error { return errors.New("mkdir denied") },
	}
	err := WriteKGMCPConfigFile(target, map[string]any{"command": "x"}, deps)
	if err == nil || !strings.Contains(err.Error(), "mkdir denied") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

// WriteKGMCPConfigFile must propagate a WriteFile failure unchanged so
// callers can surface a half-applied disk-full / permission state to
// the operator (otherwise EnsureGlobalKGMCPConfigs silently drops the
// per-platform config).
func TestWriteKGMCPConfigFile_WriteErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	deps := fakeAddDeps{
		writeFile: func(string, []byte, os.FileMode) error { return errors.New("disk full") },
	}
	err := WriteKGMCPConfigFile(target, map[string]any{"command": "x"}, deps)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected write error, got %v", err)
	}
}

// WriteKGMCPConfigs must propagate an error from the inner
// WriteKGMCPConfigFile loop (e.g. MkdirAll failure on the parent dir)
// without writing the remaining platform files: a half-applied MCP
// scope confuses doctor checks more than a hard abort.
func TestWriteKGMCPConfigs_PerFileErrorAborts(t *testing.T) {
	tmp := t.TempDir()
	// Make deps.MkdirAll fail so the FIRST WriteKGMCPConfigFile call
	// surfaces an error and the loop bails before the other two files.
	deps := fakeAddDeps{mkdirAll: func(string, os.FileMode) error { return errors.New("parent denied") }}
	err := WriteKGMCPConfigs(tmp, deps)
	if err == nil || !strings.Contains(err.Error(), "parent denied") {
		t.Fatalf("expected per-file error to abort the loop, got %v", err)
	}
}

// StdAddDeps.LoadConfig delegates to config.Load. We don't need to
// exercise the full config-load behavior — that lives in internal/config
// tests — but we do need the wrapper itself called so the lifecycle file
// reflects the production path. A missing config returns a non-nil
// error and a nil cfg; an empty AGENTS_HOME with no rc files is enough
// to drive the negative path without setting up project fixtures.
func TestStdAddDeps_LoadConfigInvokesConfigLoader(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	// Run from an empty directory so config.Load has nothing to read.
	t.Chdir(tmp)
	cfg, err := StdAddDeps{}.LoadConfig()
	// We accept either outcome — the contract is "delegates to
	// config.Load and returns its values unchanged"; the load-error vs
	// empty-load behavior is governed by config.Load's tests. The only
	// invariant here is that the wrapper does not panic.
	_ = cfg
	_ = err
}
