package lifecycle

// End-to-end coverage for the resource-source PLAN `da install` materializes
// project skills/agents from (config.ResourceSourcePlan).
//
// Two properties are pinned together on the real RunInstall path:
//
//  1. INHERITED roots are searched. A repo that declares only its team source
//     still resolves the org root the team layer declared, so a skill the org
//     layer contributes to the effective manifest actually has somewhere to
//     come from. Before the plan, install resolved roots from the raw repo
//     `sources` while selecting NAMES from the layered effective manifest —
//     so an inherited skill could be selected with no root that provides it.
//
//  2. USER-HOME roots are not project roots. Neither the synthesized
//     `{"type":"local"}` sentinel nor a source declared by the user-local
//     layer may supply project resource materialization. The canonical home
//     store is still searched, but LAST — as the fallback linkInstallResources
//     appends, not as a declared root that outranks the project's own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// writeSkill creates <root>/skills/<scope>/<name>/SKILL.md carrying body.
func writeSkill(t *testing.T, root, scope, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", scope, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// jsonQuoted renders a filesystem path as a JSON string literal so Windows
// separators survive embedding in a manifest fixture.
func jsonQuoted(p string) string {
	return `"` + strings.ReplaceAll(p, `\`, `\\`) + `"`
}

// linkedSkillBody reads the SKILL.md behind the canonical project link
// ~/.agents/skills/<project>/<name>, or "" when the link was never created.
func linkedSkillBody(t *testing.T, project, name string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("AGENTS_HOME"), "skills", project, name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read linked skill: %v", err)
	}
	return string(data)
}

// TestRunInstall_ResolvesTransitivelyInheritedSourceRoot is the positive half:
// repo → team layer → org layer, where only the TEAM source is named by the
// repo. The org root reaches install solely through the layer graph, and the
// org layer's skill links from it. The same assertion is repeated against the
// already-locked resolution path, which is what an offline install replays.
func TestRunInstall_ResolvesTransitivelyInheritedSourceRoot(t *testing.T) {
	orgRoot := t.TempDir()
	teamRoot := t.TempDir()
	writeJSONFile(t, filepath.Join(orgRoot, "org", "base.json"), `{"skills":["org-skill"]}`)
	writeSkill(t, orgRoot, "global", "org-skill", "ORG")
	writeJSONFile(t, filepath.Join(teamRoot, "team", "base.json"),
		`{"sources":[{"id":"org","type":"local","path":`+jsonQuoted(orgRoot)+`}],"extends":["org:org/base.json"]}`)

	installFixture(t, `{
		"version": 2,
		"project": "proj",
		"sources": [{"id": "team", "type": "local", "path": `+jsonQuoted(teamRoot)+`}],
		"extends": ["team:team/base.json"]
	}`)

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	if got := linkedSkillBody(t, "proj", "org-skill"); got != "ORG" {
		t.Fatalf("inherited skill body = %q, want %q — the org root reached install only through the team layer", got, "ORG")
	}

	// Second pass: the lock is now fresh, so resolution replays from the lock
	// (ResolveLocked) instead of re-fetching. The inherited root must survive
	// that path too — it is the one an offline install uses.
	if err := os.RemoveAll(filepath.Join(os.Getenv("AGENTS_HOME"), "skills", "proj")); err != nil {
		t.Fatal(err)
	}
	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("locked RunInstall: %v", err)
	}
	if got := linkedSkillBody(t, "proj", "org-skill"); got != "ORG" {
		t.Errorf("locked-resolution skill body = %q, want %q", got, "ORG")
	}
}

// TestRunInstall_DefaultHomeSourceDoesNotShadowProjectSource: a legacy manifest
// that literally carries the synthesized `{"type":"local"}` entry FIRST must
// not let a same-named user-home resource outrank the project's own copy. The
// sentinel names ~/.agents, which install already searches as the last-resort
// fallback; admitting it as a declared root is what inverted the precedence.
func TestRunInstall_DefaultHomeSourceDoesNotShadowProjectSource(t *testing.T) {
	vendor := t.TempDir()
	writeSkill(t, vendor, "global", "shared", "PROJECT")

	installFixture(t, `{
		"version": 2,
		"project": "proj",
		"sources": [{"type": "local"}, {"type": "local", "path": `+jsonQuoted(vendor)+`}],
		"skills": ["shared"]
	}`)
	writeSkill(t, os.Getenv("AGENTS_HOME"), "global", "shared", "HOME")

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	if got := linkedSkillBody(t, "proj", "shared"); got != "PROJECT" {
		t.Errorf("linked skill body = %q, want %q — the project's declared source must outrank the user-home fallback", got, "PROJECT")
	}
}

// TestRunInstall_UserLocalLayerSourceIsNotAProjectRoot is the invariant a blind
// `Effective.Sources` substitution would break. `sources` is ordered-replace,
// so a repo that declares NONE inherits the user-local layer's array outright;
// resolving those roots would materialize a machine-local resource into
// ~/.agents/skills/<project>. The plan classifies them ineligible instead.
func TestRunInstall_UserLocalLayerSourceIsNotAProjectRoot(t *testing.T) {
	userScratch := t.TempDir()
	writeSkill(t, userScratch, "global", "scratch-skill", "USER")

	projDir := installFixture(t, `{"version":2,"project":"proj","skills":["scratch-skill"]}`)
	agentsHome := os.Getenv("AGENTS_HOME")
	writeJSONFile(t, filepath.Join(agentsHome, ".agentsrc.json"),
		`{"version":2,"sources":[{"id":"mine","type":"local","path":`+jsonQuoted(userScratch)+`}]}`)

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	if got := linkedSkillBody(t, "proj", "scratch-skill"); got != "" {
		t.Errorf("user-local source root supplied a project resource (body %q); it must never be a project root", got)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "skills", "proj", "scratch-skill")); !os.IsNotExist(err) {
		t.Errorf("no project link may be created from a user-scope source root (stat err=%v)", err)
	}
	// The repo manifest must not have acquired the user's source either.
	manifest, err := os.ReadFile(filepath.Join(projDir, ".agentsrc.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), userScratch) {
		t.Errorf("install must not write a user-scope source into the project manifest:\n%s", manifest)
	}
}

// TestRunInstall_ManifestWithoutSourcesStillResolvesFromHome is the
// compatibility half: dropping the synthesized root from the PLAN must not
// stop a manifest-declared global resource from linking, because
// linkInstallResources appends the canonical home store unconditionally.
func TestRunInstall_ManifestWithoutSourcesStillResolvesFromHome(t *testing.T) {
	installFixture(t, `{"version":2,"project":"proj","skills":["home-skill"]}`)
	writeSkill(t, os.Getenv("AGENTS_HOME"), "global", "home-skill", "HOME")

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}
	if got := linkedSkillBody(t, "proj", "home-skill"); got != "HOME" {
		t.Errorf("linked skill body = %q, want %q — the canonical home store must stay searchable", got, "HOME")
	}
}

// TestInstallSnapshot_AccessModePerRun pins the access mode the whole install
// reads through. A real run reuses pass-1's resolution; a DRY-RUN resolves
// read-only instead, and when there is nothing to replay it must report "no
// layered view" rather than resolving (and thereby writing) one.
func TestInstallSnapshot_AccessModePerRun(t *testing.T) {
	saved := Flags
	t.Cleanup(func() { Flags = saved })
	// installFixture resets Flags, so every case sets its mode AFTER the fixture.

	t.Run("real run reuses the pass-1 resolution", func(t *testing.T) {
		Flags = GlobalFlags{}
		res := &config.EnsureResult{Snapshot: &config.Snapshot{}}
		if got := installSnapshot(t.TempDir(), res); got != res.Snapshot {
			t.Errorf("snapshot = %p, want pass-1's %p", got, res.Snapshot)
		}
	})

	t.Run("real run without a resolution has no snapshot", func(t *testing.T) {
		// Not a dry-run and pass-1 produced nothing: resolving here would be a
		// second, unsanctioned resolve, so the caller falls back to the raw rc.
		proj := installFixture(t, `{"version":2,"project":"proj"}`)
		Flags = GlobalFlags{}
		if got := installSnapshot(proj, nil); got != nil {
			t.Errorf("snapshot = %+v, want nil outside dry-run", got)
		}
	})

	t.Run("dry-run resolves the committed lock read-only", func(t *testing.T) {
		proj := installFixture(t, `{"version":2,"project":"proj","skills":["s"]}`)
		Flags = GlobalFlags{DryRun: true}
		snap := installSnapshot(proj, nil)
		if snap == nil {
			t.Fatal("a dry-run over a flat project must still preview the layered config")
		}
		if _, err := os.Stat(config.AgentsLockPath(proj)); !os.IsNotExist(err) {
			t.Errorf("the dry-run preview must not write a lock (stat err=%v)", err)
		}
	})

	t.Run("dry-run with an unreplayable layer stack has no snapshot", func(t *testing.T) {
		proj := installFixture(t, `{"version":2,"project":"proj",
			"sources":[{"id":"org","type":"local","path":"/nonexistent"}],
			"extends":["org:org/base.json"]}`)
		Flags = GlobalFlags{DryRun: true}
		if got := installSnapshot(proj, nil); got != nil {
			t.Errorf("snapshot = %+v, want nil: an extends project with no lock has nothing to preview", got)
		}
	})
}

// TestInstallSourcePlan_FallsBackToTheRepoDeclarations: with no snapshot the
// preview classifies the repo's OWN sources under the same eligibility rules,
// so it never lists a root the real install would refuse.
func TestInstallSourcePlan_FallsBackToTheRepoDeclarations(t *testing.T) {
	rc := &config.AgentsRC{Sources: []config.Source{
		{Type: "local"},
		{ID: "repo", Type: "local", Path: "./vendor/agents"},
	}}

	plan, err := installSourcePlan(rc, nil)
	if err != nil {
		t.Fatalf("installSourcePlan: %v", err)
	}
	got := plan.ProjectSources()
	if len(got) != 1 || got[0].ID != "repo" {
		t.Errorf("project sources = %+v, want only the authored repo root", got)
	}
}

// TestDescribeSourceIdentity names each fallback the skip line uses, so an
// unusable declaration is always identified by something the author wrote.
func TestDescribeSourceIdentity(t *testing.T) {
	home, err := config.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		src  config.Source
		want string
	}{
		{"id wins", config.Source{ID: "org", Type: "http", URL: "https://example/x.tgz"}, "'org'"},
		{"url when unnamed", config.Source{Type: "http", URL: "https://example/x.tgz"}, "https://example/x.tgz"},
		{"path when there is no url", config.Source{Type: "oci", Path: filepath.Join(home, "blobs")}, "~/blobs"},
		{"nothing identifying at all", config.Source{Type: "http"}, "(unnamed)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeSourceIdentity(tc.src); got != tc.want {
				t.Errorf("describeSourceIdentity(%+v) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
