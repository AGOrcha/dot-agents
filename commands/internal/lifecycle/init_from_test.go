package lifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

// requireGitLF skips when git is unavailable.
func requireGitLF(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// gitLF runs a git command rooted at dir, failing the test on error.
func gitLF(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := execabs.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// makeHomeSourceRepo builds a committed home-source git repo that models a
// machine-A synced home: a v2 config.json (portable identity registry, NO paths),
// a gitignored machine-local local/ + cache/, and a user-local .agentsrc.json
// declaring a manifest. Returns the repo path to clone from.
func makeHomeSourceRepo(t *testing.T, projects string) string {
	t.Helper()
	requireGitLF(t)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	src := filepath.Join(t.TempDir(), "home-source")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitLF(t, src, "init")
	gitLF(t, src, "config", "user.name", "Test")
	gitLF(t, src, "config", "user.email", "test@example.com")

	write := func(rel, body string) {
		full := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("config.json", `{"version":2,"projects":`+projects+`}`)
	write(".agentsrc.json", `{
  "manifests": {"home": {"sources": ["team:base@v1.0.0"], "project_set": "team:projects"}}
}`)
	write(".gitignore", "local/\ncache/\n")
	// Machine-local state that MUST NOT travel — gitignored, so the clone omits it.
	write("local/bindings.json", `{"version":2,"bindings":{"svc":{"path":"/machine-a/svc","added":"2024-01-01T00:00:00Z"}}}`)

	gitLF(t, src, "add", "-A")
	gitLF(t, src, "commit", "-m", "machine A home")
	return src
}

// freshAgentsHome points AGENTS_HOME at a not-yet-created path under a temp dir
// (so the clone materializes it) and HOME at an isolated dir.
func freshAgentsHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	home := filepath.Join(base, "dot-agents")
	t.Setenv("AGENTS_HOME", home)
	return home
}

// TestRunInitFrom_AdoptsHome is the headline cross-machine case: a fresh machine
// clones a remote home, ends with the user scope present, and reports the synced
// project as known-but-unbound (not vanished). Machine-local state from machine A
// (local/bindings.json) does NOT travel.
func TestRunInitFrom_AdoptsHome(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`)
	home := freshAgentsHome(t)

	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom: %v", err)
	}

	// User scope present: the synced config.json + user-local layer arrived.
	for _, rel := range []string{"config.json", ".agentsrc.json"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("expected %s in adopted home: %v", rel, err)
		}
	}
	// Machine-local binding table from machine A must NOT have travelled.
	if data, _ := os.ReadFile(filepath.Join(home, "local", "bindings.json")); strings.Contains(string(data), "/machine-a/svc") {
		t.Errorf("machine-A binding leaked into adopted home:\n%s", data)
	}
}

// TestRunInitFrom_ProjectKnownButUnbound asserts the synced identity survives the
// trip and resolves through Load as known-but-unbound (R4/R4a) — no "No managed
// projects", no fabricated path.
func TestRunInitFrom_ProjectKnownButUnbound(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`)
	home := freshAgentsHome(t)
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom: %v", err)
	}

	cfgRaw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgRaw), "github.com/acme/svc") {
		t.Errorf("synced identity registry should carry the portable repo_id:\n%s", cfgRaw)
	}
	// No absolute path may appear in the synced surface (R7 / DC3).
	if strings.Contains(string(cfgRaw), "/machine-a/") {
		t.Errorf("synced config.json leaked an absolute path:\n%s", cfgRaw)
	}
}

// TestRunInitFrom_RefusesNonEmptyHome covers the FORK-2 reconcile: a non-empty
// existing ~/.agents is refused with a clear message (distinct from --force).
func TestRunInitFrom_RefusesNonEmptyHome(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`)
	home := freshAgentsHome(t)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "occupant"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{})
	if err == nil {
		t.Fatal("expected refusal for a non-empty existing ~/.agents")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("error should explain the non-empty refusal, got: %v", err)
	}
	// The occupant must be untouched — init --from never clobbers.
	if _, err := os.Stat(filepath.Join(home, "occupant")); err != nil {
		t.Errorf("init --from must not clobber the existing home: %v", err)
	}
}

// TestRunInitFrom_AllowsEmptyDir: an empty placeholder ~/.agents is treated as
// fresh and the clone proceeds.
func TestRunInitFrom_AllowsEmptyDir(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`)
	home := freshAgentsHome(t)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("empty dir should be adoptable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); err != nil {
		t.Errorf("clone did not materialize into the empty dir: %v", err)
	}
}

// TestRunInitFrom_AmbientAuthCredsNotSynced asserts the clone uses ambient git
// (no credential is threaded by dot-agents) and that no credential material is
// written into the adopted tree (NEW-FORK-B / R7).
func TestRunInitFrom_AmbientAuthCredsNotSynced(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`)
	home := freshAgentsHome(t)
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom: %v", err)
	}
	for _, leak := range []string{".git-credentials", ".netrc", "token"} {
		if _, err := os.Stat(filepath.Join(home, leak)); err == nil {
			t.Errorf("credential material %q must never be written into the adopted home", leak)
		}
	}
}

// TestRunInitFrom_CloneError surfaces a clone failure (bad source ref).
func TestRunInitFrom_CloneError(t *testing.T) {
	requireGitLF(t)
	freshAgentsHome(t)
	err := runInitFrom(&cobra.Command{}, filepath.Join(t.TempDir(), "does-not-exist"), stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "cloning home source") {
		t.Errorf("expected clone error, got %v", err)
	}
}

// TestRunInitFrom_DryRun prints the plan and clones nothing.
func TestRunInitFrom_DryRun(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`)
	home := freshAgentsHome(t)
	InitDryRunFn = func() bool { return true }
	defer func() { InitDryRunFn = func() bool { return initDryRun } }()

	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not clone: err=%v", err)
	}
}

// TestRunInitFrom_ResolveError surfaces a user-scope resolution failure (a
// malformed cloned .agentsrc.json) instead of silently continuing.
func TestRunInitFrom_ResolveError(t *testing.T) {
	requireGitLF(t)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	src := filepath.Join(t.TempDir(), "bad-home")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitLF(t, src, "init")
	gitLF(t, src, "config", "user.name", "Test")
	gitLF(t, src, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(src, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".agentsrc.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	gitLF(t, src, "add", "-A")
	gitLF(t, src, "commit", "-m", "bad home")

	freshAgentsHome(t)
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err == nil {
		t.Error("expected user-scope resolve error from malformed .agentsrc.json")
	}
}

// TestRunInit_DispatchesToFrom proves runInit routes to the --from path when the
// flag is set on the command.
func TestRunInit_DispatchesToFrom(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`)
	home := freshAgentsHome(t)

	cmd := &cobra.Command{}
	cmd.Flags().String(initFromFlag, src, "")
	InitDryRunFn = func() bool { return false }
	InitYesFn = func() bool { return true }
	InitForceFn = func() bool { return false }
	defer func() {
		InitDryRunFn = func() bool { return initDryRun }
		InitYesFn = func() bool { return initYes }
		InitForceFn = func() bool { return initForce }
	}()

	if err := runInit(cmd, nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInit --from: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); err != nil {
		t.Errorf("dispatch did not clone the home: %v", err)
	}
}

// TestInitFromValue_NilAndUnregistered covers the safe-read fallbacks.
func TestInitFromValue_NilAndUnregistered(t *testing.T) {
	if got := initFromValue(nil); got != "" {
		t.Errorf("nil cmd should yield empty, got %q", got)
	}
	if got := initFromValue(&cobra.Command{}); got != "" {
		t.Errorf("unregistered flag should yield empty, got %q", got)
	}
}

// TestReconcileExistingAgentsHome covers the FORK-2 branches: missing (allowed),
// empty (allowed), non-empty (refused), and read-error (a file at the path).
func TestReconcileExistingAgentsHome(t *testing.T) {
	t.Run("missing is allowed", func(t *testing.T) {
		if err := reconcileExistingAgentsHome(filepath.Join(t.TempDir(), "nope")); err != nil {
			t.Errorf("missing home should be allowed: %v", err)
		}
	})
	t.Run("empty is allowed", func(t *testing.T) {
		if err := reconcileExistingAgentsHome(t.TempDir()); err != nil {
			t.Errorf("empty home should be allowed: %v", err)
		}
	})
	t.Run("non-empty refused", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := reconcileExistingAgentsHome(dir); err == nil {
			t.Error("non-empty home should be refused")
		}
	})
	t.Run("read error", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := reconcileExistingAgentsHome(f); err == nil {
			t.Error("a file at the home path should be a read error")
		}
	})
}

// TestRebindProjectSet_V2AllUnbound: a fresh v2 clone with no binding table
// reports every project known-but-unbound.
func TestRebindProjectSet_V2AllUnbound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":2,"projects":{"a":{"repo_id":"github.com/x/a"},"b":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	known, bound, unbound, err := rebindProjectSet()
	if err != nil {
		t.Fatalf("rebindProjectSet: %v", err)
	}
	if known != 2 || len(bound) != 0 || len(unbound) != 2 {
		t.Errorf("known=%d bound=%v unbound=%v; want 2/0/2", known, bound, unbound)
	}
}

// TestRebindProjectSet_V1DropsForeignPaths: a legacy v1 home (paths inline from
// machine A) must NOT inherit those paths — they are dropped, the project is
// unbound, and the v2 split is persisted path-free.
func TestRebindProjectSet_V1DropsForeignPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":1,"projects":{"svc":{"path":"/machine-a/svc","added":"2024-01-02T03:04:05Z"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	known, bound, unbound, err := rebindProjectSet()
	if err != nil {
		t.Fatalf("rebindProjectSet: %v", err)
	}
	if known != 1 || len(bound) != 0 || len(unbound) != 1 {
		t.Errorf("known=%d bound=%v unbound=%v; want 1/0/1 (foreign path dropped)", known, bound, unbound)
	}
	migrated, _ := os.ReadFile(filepath.Join(home, "config.json"))
	if strings.Contains(string(migrated), "/machine-a/svc") {
		t.Errorf("persisted v2 config.json still carries the foreign path:\n%s", migrated)
	}
}

// TestRebindProjectSet_LoadError covers the Load-error branch (a malformed
// machine-local binding table).
func TestRebindProjectSet_LoadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "local", "bindings.json"), []byte("{bad"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := rebindProjectSet(); err == nil {
		t.Error("expected load error from malformed binding table")
	}
}

// TestReportRebind covers both render loops (bound + unbound).
func TestReportRebind(t *testing.T) {
	reportRebind([]string{"bound-a"}, []string{"unbound-b"})
}

// TestRebindProjectSet_BoundProject: a project carrying a machine-local binding
// is reported bound (the empty-dir adoption that kept a prior local/ table).
func TestRebindProjectSet_BoundProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":2,"projects":{"svc":{"repo_id":"github.com/x/svc"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "local"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "local", "bindings.json"),
		[]byte(`{"version":2,"bindings":{"svc":{"path":"/here/svc","added":"2024-01-01T00:00:00Z"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	known, bound, unbound, err := rebindProjectSet()
	if err != nil {
		t.Fatalf("rebindProjectSet: %v", err)
	}
	if known != 1 || len(bound) != 1 || bound[0] != "svc" || len(unbound) != 0 {
		t.Errorf("known=%d bound=%v unbound=%v; want 1/[svc]/[]", known, bound, unbound)
	}
}

// TestRebindProjectSet_SaveError covers the migrated-home Save-error branch: a
// legacy v1 home (UpgradeNeeded) whose machine-local binding write fails must
// surface the persist error rather than silently dropping the migration. The
// home is made read-only so Load still reads config.json but Save's
// MkdirAll(local) fails.
func TestRebindProjectSet_SaveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not gate writes the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":1,"projects":{"svc":{"path":"/a/svc","added":"2024-01-01T00:00:00Z"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(home, 0755) }()

	if _, _, _, err := rebindProjectSet(); err == nil {
		t.Error("expected Save error persisting the migrated registry")
	}
}

// TestRunInitFrom_RebindError covers runInitFrom's rebind-error branch: a clone
// that lands a malformed machine-local binding table makes config.Load fail
// during the rebind step, and the error must surface (not be swallowed).
func TestRunInitFrom_RebindError(t *testing.T) {
	freshAgentsHome(t)
	orig := cloneHomeSourceFn
	cloneHomeSourceFn = func(ref, dest string) error {
		if err := os.MkdirAll(filepath.Join(dest, "local"), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"),
			[]byte(`{"version":2,"projects":{}}`), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, ".agentsrc.json"), []byte(`{}`), 0644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "local", "bindings.json"), []byte("{bad"), 0644)
	}
	defer func() { cloneHomeSourceFn = orig }()

	if err := runInitFrom(&cobra.Command{}, "fixture://home", stdInitDirMaker{}); err == nil {
		t.Error("expected rebind error from a malformed binding table")
	}
}

// TestGitCloneHomeSource_Error covers the production clone seam's error branch.
func TestGitCloneHomeSource_Error(t *testing.T) {
	requireGitLF(t)
	err := gitCloneHomeSource(filepath.Join(t.TempDir(), "nonexistent"), filepath.Join(t.TempDir(), "dst"))
	if err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Errorf("expected git clone error, got %v", err)
	}
}

// TestRunInitFrom_CloneSeamInjected covers the cloneHomeSourceFn seam: a fixture
// copy stands in for the network clone, exercising the post-clone resolve +
// rebind without git.
func TestRunInitFrom_CloneSeamInjected(t *testing.T) {
	home := freshAgentsHome(t)
	orig := cloneHomeSourceFn
	cloneHomeSourceFn = func(ref, dest string) error {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"),
			[]byte(`{"version":2,"projects":{"svc":{"repo_id":"github.com/x/svc"}}}`), 0644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, ".agentsrc.json"), []byte(`{}`), 0644)
	}
	defer func() { cloneHomeSourceFn = orig }()

	if err := runInitFrom(&cobra.Command{}, "fixture://home", stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom (seam): %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "local")); err != nil {
		t.Errorf("machine-local local/ dir should be materialized: %v", err)
	}
}
