package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// scopeFixtureProjects are the three registered projects every scope test
// works against. Three is the minimum that can distinguish "refreshed the
// current one" from "refreshed the first one" from "refreshed all of them".
var scopeFixtureProjects = []string{"alpha", "beta", "gamma"}

const scopeFixtureManifest = `{
  "version": 2,
  "repo_id": "github.com/acme/%s",
  "sources": [
    { "type": "local" }
  ]
}
`

// seedScopeFixture registers scopeFixtureProjects under a sandboxed
// HOME/AGENTS_HOME and returns the sandbox root. Every project directory sits
// directly under the root, so the root itself belongs to no project and is a
// valid "outside any managed project" working directory.
func seedScopeFixture(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignal(t, tmp)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	for _, name := range scopeFixtureProjects {
		projectPath := filepath.Join(tmp, name)
		seedLinkedProject(t, projectPath, fmt.Sprintf(scopeFixtureManifest, name))
		cfg.AddProject(name, projectPath)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })
	return tmp
}

// refreshedProjects returns the fixture projects refresh actually touched,
// detected by the lock stamp finalizeProjectRefresh writes. A project refresh
// never visited has no lock at all.
func refreshedProjects(t *testing.T, tmp string) []string {
	t.Helper()
	touched := []string{}
	for _, name := range scopeFixtureProjects {
		if _, err := os.Stat(config.AgentsLockPath(filepath.Join(tmp, name))); err == nil {
			touched = append(touched, name)
		}
	}
	return touched
}

func assertRefreshedExactly(t *testing.T, tmp string, want ...string) {
	t.Helper()
	got := refreshedProjects(t, tmp)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("refreshed projects = %v, want %v", got, want)
	}
}

// TestRunRefresh_CurrentProjectDefaultLeavesSiblingsUntouched is the polarity
// guard: the default scope must reach exactly the project holding the working
// directory. The other two registered projects are the blast-radius witnesses —
// under the old default (unfiltered = every registered project) all three would
// be stamped, which is precisely what multiplied the manifest-injection bug
// across a workspace from a single invocation.
func TestRunRefresh_CurrentProjectDefaultLeavesSiblingsUntouched(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(filepath.Join(tmp, "beta"))

	if err := runRefresh(refreshScope{}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}
	assertRefreshedExactly(t, tmp, "beta")
}

// TestRunRefresh_CurrentProjectDefaultFromSubdirectory pins that the cwd need
// only be INSIDE the project, not its root — agents routinely run from a
// nested package dir or a git worktree beneath the repo.
func TestRunRefresh_CurrentProjectDefaultFromSubdirectory(t *testing.T) {
	tmp := seedScopeFixture(t)
	nested := filepath.Join(tmp, "gamma", "internal", "deep")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	if err := runRefresh(refreshScope{}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}
	assertRefreshedExactly(t, tmp, "gamma")
}

// TestRunRefresh_AllProjectsReproducesMachineWideSweep pins that --all still
// performs the old behavior — the escape hatch has to be a real one.
func TestRunRefresh_AllProjectsReproducesMachineWideSweep(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(filepath.Join(tmp, "beta"))

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh --all: %v", err)
	}
	assertRefreshedExactly(t, tmp, scopeFixtureProjects...)
}

// TestRunRefresh_AllProjectsWorksOutsideAnyProject: --all is the explicit
// machine-wide request, so it must NOT require a current project.
func TestRunRefresh_AllProjectsWorksOutsideAnyProject(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(tmp)

	if err := runRefresh(refreshScope{AllProjects: true}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh --all: %v", err)
	}
	assertRefreshedExactly(t, tmp, scopeFixtureProjects...)
}

// TestRunRefresh_OutsideManagedProjectRefuses pins the third leg of the
// contract: with no current project and no --all, refresh must refuse rather
// than fall back to the machine-wide sweep it used to do.
func TestRunRefresh_OutsideManagedProjectRefuses(t *testing.T) {
	tmp := seedScopeFixture(t)
	outside := filepath.Join(tmp, "elsewhere")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(outside)

	err := runRefresh(refreshScope{}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
	if err == nil {
		t.Fatal("expected refusal outside any managed project, got nil")
	}
	if !errors.Is(err, errNotInManagedProject) {
		t.Errorf("error = %v, want errNotInManagedProject", err)
	}
	if got := refreshedProjects(t, tmp); len(got) != 0 {
		t.Errorf("refusal still refreshed %v, want nothing touched", got)
	}
}

// TestRunRefresh_OutsideManagedProjectHintsRecovery keeps the refusal
// actionable: the operator must learn both escape routes from the message.
func TestRunRefresh_OutsideManagedProjectHintsRecovery(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(tmp)

	err := runRefresh(refreshScope{}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error = %v, want *CLIError", err)
	}
	hints := strings.Join(cliErr.Hints, "\n")
	for _, want := range []string{"cd into a managed project", "da refresh --all", "da status"} {
		if !strings.Contains(hints, want) {
			t.Errorf("hints missing %q:\n%s", want, hints)
		}
	}
}

// TestRefreshCurrentProjectOrSkip_SkipsOutsideProject pins the internal-caller
// contract: `da sync pull` / `da review approve` already did their real work by
// the time they ask for a projection, so "nowhere to project" is a skip, not a
// failure (a failure would roll the approval back).
func TestRefreshCurrentProjectOrSkip_SkipsOutsideProject(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(tmp)

	if err := refreshCurrentProjectOrSkip(); err != nil {
		t.Fatalf("expected a soft skip outside any project, got: %v", err)
	}
	if got := refreshedProjects(t, tmp); len(got) != 0 {
		t.Errorf("skip still refreshed %v, want nothing touched", got)
	}
}

// TestRefreshCurrentProjectOrSkip_RefreshesOnlyCurrentProject pins the other
// half: inside a project the internal callers refresh that one and no other.
func TestRefreshCurrentProjectOrSkip_RefreshesOnlyCurrentProject(t *testing.T) {
	tmp := seedScopeFixture(t)
	t.Chdir(filepath.Join(tmp, "alpha"))

	if err := refreshCurrentProjectOrSkip(); err != nil {
		t.Fatalf("refreshCurrentProjectOrSkip: %v", err)
	}
	assertRefreshedExactly(t, tmp, "alpha")
}

// TestSyncAndReviewDepsScopeToCurrentProject is the wiring guard for the two
// internal fan-out inheritors. Both seams are now shaped so they CANNOT ask for
// a machine-wide sweep — there is no argument to pass one through.
func TestSyncAndReviewDepsScopeToCurrentProject(t *testing.T) {
	if syncDeps().RunRefreshCurrentProject == nil {
		t.Error("sync deps must wire RunRefreshCurrentProject")
	}
	var _ interface{ RunRefreshCurrentProject() error } = stdReviewDeps{}
}

func TestResolveRefreshProjects_AllProjectsIsSorted(t *testing.T) {
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	for _, name := range []string{"zeta", "alpha", "mu"} {
		cfg.AddProject(name, filepath.Join(t.TempDir(), name))
	}

	// Repeated because map iteration order is randomized per range: a single
	// pass could pass by luck against an unsorted implementation.
	want := []string{"alpha", "mu", "zeta"}
	for i := 0; i < 8; i++ {
		got, err := resolveRefreshProjects(cfg, refreshScope{AllProjects: true})
		if err != nil {
			t.Fatalf("resolveRefreshProjects: %v", err)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("pass %d: order = %v, want %v", i, got, want)
		}
	}
}

func TestResolveRefreshProjects_ExplicitNameWins(t *testing.T) {
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("alpha", filepath.Join(t.TempDir(), "alpha"))
	cfg.AddProject("beta", filepath.Join(t.TempDir(), "beta"))

	got, err := resolveRefreshProjects(cfg, refreshScope{Project: "beta"})
	if err != nil {
		t.Fatalf("resolveRefreshProjects: %v", err)
	}
	if len(got) != 1 || got[0] != "beta" {
		t.Errorf("projects = %v, want [beta]", got)
	}
}

func TestResolveRefreshProjects_UnknownNameErrors(t *testing.T) {
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("alpha", filepath.Join(t.TempDir(), "alpha"))

	_, err := resolveRefreshProjects(cfg, refreshScope{Project: "ghost"})
	if err == nil {
		t.Fatal("expected an error for an unknown project name")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error = %v, want it to name the missing project", err)
	}
}

// TestMatchProjectForDir_DeepestProjectWins covers nested registrations: a
// workspace root registered alongside one of its members must not swallow the
// member's refreshes.
func TestMatchProjectForDir_DeepestProjectWins(t *testing.T) {
	root := t.TempDir()
	member := filepath.Join(root, "services", "billing")
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("workspace", root)
	cfg.AddProject("billing", member)

	if got := matchProjectForDir(cfg, member); got != "billing" {
		t.Errorf("match at member root = %q, want %q", got, "billing")
	}
	if got := matchProjectForDir(cfg, filepath.Join(member, "internal")); got != "billing" {
		t.Errorf("match below member = %q, want %q", got, "billing")
	}
	if got := matchProjectForDir(cfg, filepath.Join(root, "docs")); got != "workspace" {
		t.Errorf("match outside member = %q, want %q", got, "workspace")
	}
}

// TestMatchProjectForDir_SiblingPrefixIsNotAMatch guards the separator check:
// /tmp/repo-sandbox is not inside /tmp/repo.
func TestMatchProjectForDir_SiblingPrefixIsNotAMatch(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("repo", filepath.Join(tmp, "repo"))

	if got := matchProjectForDir(cfg, filepath.Join(tmp, "repo-sandbox")); got != "" {
		t.Errorf("match = %q, want no match for a sibling sharing a name prefix", got)
	}
}

// TestMatchProjectForDir_SkipsUnboundAndPlaceholderPaths: a project known in
// the synced registry but unbound on this machine has no path to compare, and
// "." is the placeholder refresh already refuses to visit. Neither may match —
// least of all match everything.
func TestMatchProjectForDir_SkipsUnboundAndPlaceholderPaths(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{"unbound": {}},
		Agents:   map[string]config.Agent{},
	}
	cfg.AddProject("placeholder", ".")

	if got := matchProjectForDir(cfg, tmp); got != "" {
		t.Errorf("match = %q, want no match from unbound/placeholder projects", got)
	}
}

// TestImportFilterForScope pins that the pre-refresh import pass inherits the
// refresh scope instead of always sweeping the registry.
func TestImportFilterForScope(t *testing.T) {
	cases := []struct {
		name     string
		scope    refreshScope
		projects []string
		want     string
	}{
		{"current project", refreshScope{}, []string{"beta"}, "beta"},
		{"named project", refreshScope{Project: "alpha"}, []string{"alpha"}, "alpha"},
		{"all projects", refreshScope{AllProjects: true}, []string{"a", "b"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := importFilterForScope(tc.scope, tc.projects); got != tc.want {
				t.Errorf("importFilterForScope = %q, want %q", got, tc.want)
			}
		})
	}
}
