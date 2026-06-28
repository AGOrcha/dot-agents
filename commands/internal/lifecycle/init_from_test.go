package lifecycle

import (
	"errors"
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

// makeHomeSourceRepo builds a committed home-source git repo modelling a machine-A
// synced home: a v2 config.json (portable identity registry, NO paths) and a
// user-local .agentsrc.json declaring a manifest. When trackLocal is false the
// machine-local local/ is gitignored (the correct synced shape); when true the
// foreign local/bindings.json is COMMITTED (the BUG-2 misconfigured-source case).
// Returns the repo path to clone from.
func makeHomeSourceRepo(t *testing.T, projects string, trackLocal bool) string {
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
	if !trackLocal {
		write(".gitignore", "local/\ncache/\n")
	}
	// Machine-local state holding a SOURCE-machine absolute path. Gitignored ⇒ it
	// never travels; tracked ⇒ it travels and BUG-2's repair must drop it.
	write("local/bindings.json", `{"version":2,"bindings":{"svc":{"path":"/machine-a/svc","added":"2024-01-01T00:00:00Z"}}}`)

	gitLF(t, src, "add", "-A")
	gitLF(t, src, "commit", "-m", "machine A home")
	return src
}

// seedStagedClone stands in for a real clone: it git-inits dest (so the
// post-clone untrack repair has a repo to operate on) and writes a v2 config.json
// + the given .agentsrc.json. Used by cloneHomeSourceFn seam overrides.
func seedStagedClone(dest, agentsrc string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	if err := execabs.Command("git", "init", dest).Run(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dest, ".agentsrc.json"), []byte(agentsrc), 0644)
}

// freshAgentsHome points AGENTS_HOME at a not-yet-created path under a temp dir
// (so adoption materializes it) and HOME at an isolated dir.
func freshAgentsHome(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("HOME", base)
	home := filepath.Join(base, "dot-agents")
	t.Setenv("AGENTS_HOME", home)
	return home
}

// TestRunInitFrom_AdoptsHome is the headline cross-machine case: a fresh machine
// adopts a remote home, ends with the user scope present, and reports the synced
// project known-but-unbound. The gitignored machine-A binding does not travel.
func TestRunInitFrom_AdoptsHome(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`, false)
	home := freshAgentsHome(t)

	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom: %v", err)
	}
	for _, rel := range []string{"config.json", ".agentsrc.json"} {
		if _, err := os.Stat(filepath.Join(home, rel)); err != nil {
			t.Errorf("expected %s in adopted home: %v", rel, err)
		}
	}
	if data, _ := os.ReadFile(filepath.Join(home, "local", "bindings.json")); strings.Contains(string(data), "/machine-a/svc") {
		t.Errorf("machine-A binding leaked into adopted home:\n%s", data)
	}
}

// TestRunInitFrom_ProjectKnownButUnbound asserts the synced identity survives the
// trip and resolves as known-but-unbound (R4/R4a) with no fabricated path.
func TestRunInitFrom_ProjectKnownButUnbound(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`, false)
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
	if strings.Contains(string(cfgRaw), "/machine-a/") {
		t.Errorf("synced config.json leaked an absolute path:\n%s", cfgRaw)
	}
}

// TestRunInitFrom_V2TrackedBindingsDropped is the BUG-2 regression: a v2 source
// that mistakenly TRACKED local/bindings.json (foreign paths) must not import
// those paths — post-adopt the bindings are empty, every project is unbound, and
// local/ is untracked + gitignored so it stops re-syncing.
func TestRunInitFrom_V2TrackedBindingsDropped(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`, true)
	home := freshAgentsHome(t)
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInitFrom: %v", err)
	}

	bind, _ := os.ReadFile(filepath.Join(home, "local", "bindings.json"))
	if strings.Contains(string(bind), "/machine-a/svc") {
		t.Errorf("foreign source-machine path was imported (BUG-2):\n%s", bind)
	}
	tracked, err := execabs.Command("git", "-C", home, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tracked), "local/") {
		t.Errorf("local/ still tracked after adopt:\n%s", tracked)
	}
	gi, _ := os.ReadFile(filepath.Join(home, ".gitignore"))
	if !strings.Contains(string(gi), "local/") {
		t.Errorf(".gitignore should exclude local/ after the repair:\n%s", gi)
	}
}

// TestRunInitFrom_PostCloneFailureNoPartialHome is the BUG-1 regression: a failure
// AFTER the clone (here a malformed .agentsrc.json) must leave NO partial
// ~/.agents, so the retry is not refused as non-empty. A second, good adoption
// then succeeds.
func TestRunInitFrom_PostCloneFailureNoPartialHome(t *testing.T) {
	home := freshAgentsHome(t)
	orig := cloneHomeSourceFn
	defer func() { cloneHomeSourceFn = orig }()

	cloneHomeSourceFn = func(ref, dest string) error {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, ".agentsrc.json"), []byte("{not json"), 0644)
	}
	if err := runInitFrom(&cobra.Command{}, "fixture://bad", stdInitDirMaker{}); err == nil {
		t.Fatal("expected a post-clone resolve failure")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("a failed adoption must leave NO partial ~/.agents (err=%v)", err)
	}

	// The retry must NOT be refused — a good clone now adopts cleanly.
	requireGitLF(t)
	cloneHomeSourceFn = func(ref, dest string) error { return seedStagedClone(dest, `{}`) }
	if err := runInitFrom(&cobra.Command{}, "fixture://good", stdInitDirMaker{}); err != nil {
		t.Fatalf("retry after a failed adoption must not be refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); err != nil {
		t.Errorf("retry did not materialize the home: %v", err)
	}
}

// TestRunInitFrom_RefusesCredentialURL is the BUG-3 regression: a --from URL with
// embedded userinfo is refused before any clone, and nothing lands in ~/.agents.
func TestRunInitFrom_RefusesCredentialURL(t *testing.T) {
	home := freshAgentsHome(t)
	called := false
	orig := cloneHomeSourceFn
	cloneHomeSourceFn = func(ref, dest string) error { called = true; return nil }
	defer func() { cloneHomeSourceFn = orig }()

	err := runInitFrom(&cobra.Command{}, "https://user:s3cr3t@example.com/home.git", stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("expected a credential-bearing URL refusal, got %v", err)
	}
	if called {
		t.Error("clone must not run for a credential-bearing --from URL")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("a refused credential URL must not create ~/.agents (err=%v)", err)
	}
}

// TestRunInitFrom_RefusesNonEmptyHome covers the FORK-2 reconcile.
func TestRunInitFrom_RefusesNonEmptyHome(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`, false)
	home := freshAgentsHome(t)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "occupant"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected non-empty refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "occupant")); err != nil {
		t.Errorf("init --from must not clobber the existing home: %v", err)
	}
}

// TestRunInitFrom_AllowsEmptyDir: an empty placeholder ~/.agents is adoptable.
func TestRunInitFrom_AllowsEmptyDir(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`, false)
	home := freshAgentsHome(t)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("empty dir should be adoptable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); err != nil {
		t.Errorf("adoption did not materialize into the empty dir: %v", err)
	}
}

// TestRunInitFrom_AmbientAuthCredsNotSynced asserts no credential material is
// written into the adopted tree (NEW-FORK-B / R7).
func TestRunInitFrom_AmbientAuthCredsNotSynced(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`, false)
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

// TestRunInitFrom_CloneError surfaces a clone failure and leaves no partial home.
func TestRunInitFrom_CloneError(t *testing.T) {
	requireGitLF(t)
	home := freshAgentsHome(t)
	err := runInitFrom(&cobra.Command{}, filepath.Join(t.TempDir(), "does-not-exist"), stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "cloning home source") {
		t.Errorf("expected clone error, got %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("a clone failure must leave no partial ~/.agents (err=%v)", err)
	}
}

// TestRunInitFrom_DryRun prints the plan and adopts nothing.
func TestRunInitFrom_DryRun(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`, false)
	home := freshAgentsHome(t)
	InitDryRunFn = func() bool { return true }
	defer func() { InitDryRunFn = func() bool { return initDryRun } }()

	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("dry-run must not adopt: err=%v", err)
	}
}

// TestRunInitFrom_ResolveError surfaces a user-scope resolution failure (a
// malformed cloned .agentsrc.json) and leaves no partial home.
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

	home := freshAgentsHome(t)
	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err == nil {
		t.Error("expected user-scope resolve error from malformed .agentsrc.json")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("a resolve failure must leave no partial ~/.agents (err=%v)", err)
	}
}

// TestRunInit_DispatchesToFrom proves runInit routes to the --from path.
func TestRunInit_DispatchesToFrom(t *testing.T) {
	src := makeHomeSourceRepo(t, `{}`, false)
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
		t.Errorf("dispatch did not adopt the home: %v", err)
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

// TestValidateAmbientAuthRef covers the BUG-3 contract: credential-bearing URLs
// refused, ambient ssh / clean refs allowed.
func TestValidateAmbientAuthRef(t *testing.T) {
	tests := []struct {
		ref     string
		refused bool
	}{
		{"git@github.com:acme/repo.git", false},
		{"ssh://git@github.com/acme/repo.git", false},
		{"https://github.com/acme/repo.git", false},
		{"/local/path/to/home", false},
		{"file:///local/home", false},
		{"https://user:token@github.com/acme/repo.git", true},
		{"https://ghp_tokenonly@github.com/acme/repo.git", true},
		{"http://u:p@host/repo.git", true},
		{"ssh://git:secret@github.com/acme/repo.git", true},
	}
	for _, tt := range tests {
		err := validateAmbientAuthRef(tt.ref)
		if tt.refused && err == nil {
			t.Errorf("%q should be refused", tt.ref)
		}
		if !tt.refused && err != nil {
			t.Errorf("%q should be allowed, got %v", tt.ref, err)
		}
	}
}

// TestRedactRef masks an embedded password and passes clean refs through —
// including an ssh ref whose userinfo is a login user, not a credential.
func TestRedactRef(t *testing.T) {
	if got := redactRef("https://user:token@host/repo.git"); strings.Contains(got, "token") {
		t.Errorf("redactRef leaked the password: %q", got)
	}
	for _, clean := range []string{"git@github.com:acme/repo.git", "https://github.com/acme/repo.git", "/path", "ssh://git@github.com/acme/repo.git"} {
		if got := redactRef(clean); got != clean {
			t.Errorf("redactRef(%q) = %q, want unchanged", clean, got)
		}
	}
}

// TestRunInitFrom_StagingMkdirError covers stageAndAdoptHome's MkdirTemp-error
// branch: a target whose parent dir does not exist fails staging before any clone.
func TestRunInitFrom_StagingMkdirError(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("AGENTS_HOME", filepath.Join(base, "missing-parent", "dot-agents"))
	called := false
	orig := cloneHomeSourceFn
	cloneHomeSourceFn = func(ref, dest string) error { called = true; return nil }
	defer func() { cloneHomeSourceFn = orig }()

	if err := runInitFrom(&cobra.Command{}, "fixture://home", stdInitDirMaker{}); err == nil {
		t.Error("expected a staging-dir creation error")
	}
	if called {
		t.Error("clone must not run when staging cannot be created")
	}
}

// TestResolveAndRebindStaged_GitignoreError covers the ensureGitignore-error
// branch: a staged .gitignore that is a directory fails the machine-local repair.
func TestResolveAndRebindStaged_GitignoreError(t *testing.T) {
	requireGitLF(t)
	staging := t.TempDir()
	// git-init so the untrack repair succeeds and the gitignore branch is reached.
	gitLF(t, staging, "init")
	if err := os.WriteFile(filepath.Join(staging, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, ".agentsrc.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveAndRebindStaged(staging); err == nil {
		t.Error("expected a .gitignore write error")
	}
}

// TestRunInitFrom_LinkFailureIsNonFatal is the post-adopt-link-failure regression:
// a harness link failure AFTER the staged home is renamed into place must be
// NON-FATAL — runInitFrom returns success, ~/.agents EXISTS as the adopted home,
// and `da refresh` (not a re-run of init --from) owns completing the linking.
func TestRunInitFrom_LinkFailureIsNonFatal(t *testing.T) {
	src := makeHomeSourceRepo(t, `{"svc":{"repo_id":"github.com/acme/svc"}}`, false)
	home := freshAgentsHome(t)

	orig := linkClaudeGlobalFn
	linkClaudeGlobalFn = func(string, initDirMaker) error { return errors.New("simulated link failure") }
	defer func() { linkClaudeGlobalFn = orig }()

	if err := runInitFrom(&cobra.Command{}, src, stdInitDirMaker{}); err != nil {
		t.Fatalf("a post-adopt link failure must be non-fatal, got: %v", err)
	}
	// Adoption succeeded: the home exists and carries the adopted content.
	if _, err := os.Stat(filepath.Join(home, "config.json")); err != nil {
		t.Errorf("adopted home must exist after a link hiccup: %v", err)
	}
	cfgRaw, _ := os.ReadFile(filepath.Join(home, "config.json"))
	if !strings.Contains(string(cfgRaw), "github.com/acme/svc") {
		t.Errorf("home should be the adopted one despite the link hiccup:\n%s", cfgRaw)
	}
}

// TestRunInitFrom_UntrackErrorNoPartialHome is the NIT regression: a propagated
// untrack failure (here a clone that is not a git repo) must abort inside the
// staging window, leaving NO partial ~/.agents.
func TestRunInitFrom_UntrackErrorNoPartialHome(t *testing.T) {
	requireGitLF(t)
	home := freshAgentsHome(t)
	orig := cloneHomeSourceFn
	cloneHomeSourceFn = func(ref, dest string) error {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"), []byte(`{"version":2,"projects":{}}`), 0644); err != nil {
			return err
		}
		// No git init → the untrack `git rm --cached` fails (not a git repo).
		return os.WriteFile(filepath.Join(dest, ".agentsrc.json"), []byte(`{}`), 0644)
	}
	defer func() { cloneHomeSourceFn = orig }()

	err := runInitFrom(&cobra.Command{}, "fixture://nogit", stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "untracking") {
		t.Fatalf("expected an untrack error to propagate, got %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("an untrack failure must leave no partial ~/.agents (err=%v)", err)
	}
}

// TestUntrackStagedMachineLocal covers the no-op (clean repo) and error
// (non-git dir) branches directly.
func TestUntrackStagedMachineLocal(t *testing.T) {
	requireGitLF(t)
	t.Run("clean repo is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		gitLF(t, dir, "init")
		if err := untrackStagedMachineLocal(dir); err != nil {
			t.Errorf("untrack on a clean repo should succeed: %v", err)
		}
	})
	t.Run("non-git dir errors", func(t *testing.T) {
		if err := untrackStagedMachineLocal(t.TempDir()); err == nil {
			t.Error("expected an error untracking in a non-git dir")
		}
	})
}

// TestWarnIfLinkFailed covers both render branches (nil + error).
func TestWarnIfLinkFailed(t *testing.T) {
	warnIfLinkFailed("Claude Code", nil)
	warnIfLinkFailed("Cursor", errors.New("boom"))
}

// TestMoveStagedHome_NonEmptyTargetError covers the clear-target error branch: a
// non-empty target cannot be removed for the rename.
func TestMoveStagedHome_NonEmptyTargetError(t *testing.T) {
	base := t.TempDir()
	staging := filepath.Join(base, "staging")
	if err := os.MkdirAll(staging, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "home")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "occupant"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := moveStagedHome(staging, target); err == nil {
		t.Error("expected an error clearing a non-empty target")
	}
}

// TestReportRebind covers the render loop.
func TestReportRebind(t *testing.T) {
	reportRebind([]string{"unbound-a", "unbound-b"})
}

// TestRebindProjectSet_AllUnbound: every known project starts unbound, with the
// path-free split persisted.
func TestRebindProjectSet_AllUnbound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"),
		[]byte(`{"version":2,"projects":{"a":{"repo_id":"github.com/x/a"},"b":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	known, unbound, err := rebindProjectSet()
	if err != nil {
		t.Fatalf("rebindProjectSet: %v", err)
	}
	if known != 2 || len(unbound) != 2 {
		t.Errorf("known=%d unbound=%v; want 2/[a b]", known, unbound)
	}
}

// TestRebindProjectSet_DropsTrackedBindings: a binding table present in the home
// (foreign path) is dropped — every project unbound, no foreign path persisted
// (BUG-2 at the unit level).
func TestRebindProjectSet_DropsTrackedBindings(t *testing.T) {
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
		[]byte(`{"version":2,"bindings":{"svc":{"path":"/machine-a/svc","added":"2024-01-01T00:00:00Z"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	known, unbound, err := rebindProjectSet()
	if err != nil {
		t.Fatalf("rebindProjectSet: %v", err)
	}
	if known != 1 || len(unbound) != 1 {
		t.Errorf("known=%d unbound=%v; want 1/[svc]", known, unbound)
	}
	bind, _ := os.ReadFile(filepath.Join(home, "local", "bindings.json"))
	if strings.Contains(string(bind), "/machine-a/svc") {
		t.Errorf("foreign binding not dropped:\n%s", bind)
	}
}

// TestRebindProjectSet_LoadError covers the Load-error branch.
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
	if _, _, err := rebindProjectSet(); err == nil {
		t.Error("expected load error from malformed binding table")
	}
}

// TestRebindProjectSet_SaveError covers the persist-error branch: a read-only home
// lets Load read config.json but fails Save's MkdirAll(local).
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
		[]byte(`{"version":2,"projects":{"svc":{"repo_id":"github.com/x/svc"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(home, 0755) }()

	if _, _, err := rebindProjectSet(); err == nil {
		t.Error("expected Save error persisting the adopted registry")
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

// TestReconcileExistingAgentsHome covers the FORK-2 branches.
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

// The TestEnsureStagedMachineLocalGitignored_* tests cover the create,
// append-missing, already-present, and read-error branches. Split per-branch to
// keep each test's cognitive complexity low (S3776).

func TestEnsureStagedMachineLocalGitignored_CreatesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := ensureStagedMachineLocalGitignored(dir); err != nil {
		t.Fatal(err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, w := range []string{"local/", "cache/"} {
		if !strings.Contains(string(gi), w) {
			t.Errorf("missing %q:\n%s", w, gi)
		}
	}
}

func TestEnsureStagedMachineLocalGitignored_AppendsOnlyMissing(t *testing.T) {
	dir := t.TempDir()
	// No trailing newline — exercises the newline-normalizing append branch.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("local/\nfoo"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureStagedMachineLocalGitignored(dir); err != nil {
		t.Fatal(err)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi), "local/") != 1 {
		t.Errorf("local/ duplicated or missing:\n%s", gi)
	}
	if !strings.Contains(string(gi), "cache/") {
		t.Errorf("cache/ not appended:\n%s", gi)
	}
}

func TestEnsureStagedMachineLocalGitignored_NoopWhenPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("local/\ncache/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureStagedMachineLocalGitignored(dir); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureStagedMachineLocalGitignored_ReadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gitignore"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensureStagedMachineLocalGitignored(dir); err == nil {
		t.Error("a directory .gitignore should be a read error")
	}
}

// TestMoveStagedHome_RenamesAndClearsEmpty covers the empty-target clear + rename.
func TestMoveStagedHome_RenamesAndClearsEmpty(t *testing.T) {
	base := t.TempDir()
	staging := filepath.Join(base, "staging")
	if err := os.MkdirAll(staging, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "home")
	if err := os.MkdirAll(target, 0755); err != nil { // empty placeholder
		t.Fatal(err)
	}
	if err := moveStagedHome(staging, target); err != nil {
		t.Fatalf("moveStagedHome: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "f")); err != nil {
		t.Errorf("staged content not moved into target: %v", err)
	}
}

// TestWithAgentsHome_RestoresUnset covers the previously-unset restore branch.
func TestWithAgentsHome_RestoresUnset(t *testing.T) {
	if err := os.Unsetenv("AGENTS_HOME"); err != nil {
		t.Fatal(err)
	}
	restore := withAgentsHome("/tmp/x")
	if got := os.Getenv("AGENTS_HOME"); got != "/tmp/x" {
		t.Errorf("AGENTS_HOME = %q, want /tmp/x", got)
	}
	restore()
	if _, ok := os.LookupEnv("AGENTS_HOME"); ok {
		t.Error("AGENTS_HOME should be unset again after restore")
	}
}
