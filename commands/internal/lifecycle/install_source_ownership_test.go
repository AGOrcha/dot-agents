package lifecycle

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// sourceOwnershipFixture stands up an isolated HOME + ~/.agents with a
// canonical project-scope skill and a user/global-scope skill, seeds the
// project manifest when manifest is non-empty, and chdirs into the project.
// Returns the agents home and the project directory.
func sourceOwnershipFixture(t *testing.T, manifest string) (agentsHome, projDir string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome = filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":2}`), 0644); err != nil {
		t.Fatal(err)
	}
	seedCanonicalSkill(t, agentsHome, ownershipProject, ownershipProjectSkill)
	seedCanonicalSkill(t, agentsHome, "global", ownershipUserSkill)

	projDir = filepath.Join(tmp, ownershipProject)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(projDir, config.AgentsRCFile), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })
	return agentsHome, projDir
}

// seedCanonicalSkill creates <root>/skills/<scope>/<name>/SKILL.md.
func seedCanonicalSkill(t *testing.T, root, scope, name string) string {
	t.Helper()
	dir := filepath.Join(root, "skills", scope, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const (
	ownershipProject      = "ownerproj"
	ownershipProjectSkill = "proj-skill"
	ownershipUserSkill    = "user-skill"
	ownershipExtSkill     = "ext-skill"
	localSourceLogPrefix  = "Local source:"
)

// decodeManifestJSON reads the on-disk manifest as a raw JSON object, which is
// the only way to tell an ABSENT `sources` key from one holding the synthesized
// in-memory default that LoadAgentsRC hands consumers.
func decodeManifestJSON(t *testing.T, projDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projDir, config.AgentsRCFile))
	if err != nil {
		t.Fatalf("reading generated manifest: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing generated manifest: %v\n%s", err, data)
	}
	return raw
}

// TestRunInstallGenerate_NeverSerializesDefaultHomeSource is the end-to-end
// contract for the committed artifact: `da install --generate` must never write
// the bare default-home `{"type":"local"}` source into .agentsrc.json, whether
// the project has no manifest at all or a legacy one that declares exactly that
// entry. A manifest is shared with teammates, and that entry only names the
// generating user's own ~/.agents home — install links canonical resources from
// there unconditionally, so declaring it is at best redundant and at worst lets
// a user-scope root pose as one the project owns.
//
// The counterweight is asserted in the same table: an authored root — a git
// remote, or a path-BEARING local layer — is a real declaration and survives
// regeneration untouched.
func TestRunInstallGenerate_NeverSerializesDefaultHomeSource(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		// wantSources is the expected decoded `sources` value; nil means the
		// key must be ABSENT from the written file.
		wantSources []any
	}{
		{
			name:        "no manifest at all",
			manifest:    "",
			wantSources: nil,
		},
		{
			name:        "legacy manifest declaring only the implicit local source",
			manifest:    `{"version":1,"project":"` + ownershipProject + `","sources":[{"type":"local"}]}`,
			wantSources: nil,
		},
		{
			name:     "explicit project-local source survives",
			manifest: `{"version":1,"project":"` + ownershipProject + `","sources":[{"id":"org","type":"local","path":"../orglayer"}]}`,
			wantSources: []any{
				map[string]any{"id": "org", "type": "local", "path": "../orglayer"},
			},
		},
		{
			name:     "external source survives while the implicit local beside it is dropped",
			manifest: `{"version":1,"project":"` + ownershipProject + `","sources":[{"type":"local"},{"type":"git","url":"https://example.test/org.git","ref":"main"}]}`,
			wantSources: []any{
				map[string]any{"type": "git", "url": "https://example.test/org.git", "ref": "main"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, projDir := sourceOwnershipFixture(t, tc.manifest)

			if err := RunInstallGenerate(StdInstallDeps{}); err != nil {
				t.Fatalf("RunInstallGenerate: %v", err)
			}

			raw := decodeManifestJSON(t, projDir)
			got, present := raw["sources"]
			if tc.wantSources == nil {
				if present {
					t.Errorf("`sources` must be ABSENT from the generated manifest, got %v", got)
				}
				return
			}
			if !present {
				t.Fatal("`sources` must be present: an authored declaration was dropped")
			}
			wantJSON, _ := json.Marshal(tc.wantSources)
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("sources: got %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// TestRunInstallGenerate_UserResourcesStayUserOnly proves the ownership
// invariant across a full generate→install lifecycle. The fixture home holds a
// user/global-scope skill beside the project-scope one. Neither the generated
// manifest nor the project's canonical resource directory may acquire the
// user-scope skill: global resources auto-resolve at the USER level for every
// project through each platform adapter's global scope pass, so re-declaring or
// re-linking one under the project both duplicates it and makes it go stale on
// the next machine.
func TestRunInstallGenerate_UserResourcesStayUserOnly(t *testing.T) {
	agentsHome, projDir := sourceOwnershipFixture(t, "")

	if err := RunInstallGenerate(StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstallGenerate: %v", err)
	}

	raw := decodeManifestJSON(t, projDir)
	if _, present := raw["sources"]; present {
		t.Errorf("`sources` must be absent, got %v", raw["sources"])
	}
	gotSkills, _ := json.Marshal(raw["skills"])
	if want := `["` + ownershipProjectSkill + `"]`; string(gotSkills) != want {
		t.Errorf("skills: got %s, want %s (the user/global skill must not be captured)", gotSkills, want)
	}

	if err := RunInstall(false, StdInstallDeps{}); err != nil {
		t.Fatalf("RunInstall: %v", err)
	}

	// The user-scope skill must not have been materialized under project scope.
	leaked := filepath.Join(agentsHome, "skills", ownershipProject, ownershipUserSkill)
	if _, err := os.Lstat(leaked); err == nil {
		t.Errorf("user-scope skill leaked into project scope at %s", leaked)
	}
	// It must still exist where it belongs — this fix removes a declaration,
	// not the resource.
	userScoped := filepath.Join(agentsHome, "skills", "global", ownershipUserSkill)
	if _, err := os.Stat(userScoped); err != nil {
		t.Errorf("user-scope skill must remain at %s: %v", userScoped, err)
	}
	// And the project's own canonical skill is untouched.
	projScoped := filepath.Join(agentsHome, "skills", ownershipProject, ownershipProjectSkill)
	if _, err := os.Stat(projScoped); err != nil {
		t.Errorf("project skill must remain at %s: %v", projScoped, err)
	}
}

// captureInstallStdout runs fn with os.Stdout redirected and returns what it
// printed. ui.Bullet/ui.Section resolve os.Stdout per call, so the swap is
// observed by the "Resolving sources" section.
func captureInstallStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestRunInstall_DefaultHomeSourceIsNotResolved pins the resolution half of the
// ownership contract. `da install` resolves the manifest's declared source roots
// before projection; the default-home entry must not reach that stage, because
// linkInstallResources appends ~/.agents unconditionally as the canonical store
// and resolving it again treats the user's own home as a project declaration.
//
// The oracle is the "Resolving sources" section: resolveSourceRoot logs one
// "Local source:" bullet per resolved local root. Both the absent-sources case
// (LoadAgentsRC synthesizes the default in memory) and the legacy
// explicitly-declared case must resolve NO local root, while canonical project
// resources still link from ~/.agents.
func TestRunInstall_DefaultHomeSourceIsNotResolved(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
	}{
		{
			name:     "absent sources (synthesized in memory by LoadAgentsRC)",
			manifest: `{"version":2,"project":"` + ownershipProject + `","skills":["` + ownershipProjectSkill + `"]}`,
		},
		{
			name:     "legacy manifest declaring the default-home local source",
			manifest: `{"version":2,"project":"` + ownershipProject + `","skills":["` + ownershipProjectSkill + `"],"sources":[{"type":"local"}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agentsHome, _ := sourceOwnershipFixture(t, tc.manifest)

			var runErr error
			out := captureInstallStdout(t, func() {
				runErr = RunInstall(false, StdInstallDeps{})
			})
			if runErr != nil {
				t.Fatalf("RunInstall: %v", runErr)
			}
			if strings.Contains(out, localSourceLogPrefix) {
				t.Errorf("install resolved the default-home source as a project root:\n%s", out)
			}
			// linkInstallResources still finds the canonical resource: dropping
			// the redundant source root must not cost the project its skill.
			projScoped := filepath.Join(agentsHome, "skills", ownershipProject, ownershipProjectSkill)
			if _, err := os.Stat(projScoped); err != nil {
				t.Errorf("canonical project skill unresolved at %s: %v", projScoped, err)
			}
		})
	}
}

// TestRunInstall_ExplicitProjectLocalSourceStillResolves is the counterweight:
// narrowing resolution to project-owned roots must not break a path-bearing
// local source. The fixture puts a skill ONLY inside that external root, so it
// can be materialized by nothing else.
func TestRunInstall_ExplicitProjectLocalSourceStillResolves(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":2}`), 0644); err != nil {
		t.Fatal(err)
	}

	extRoot := filepath.Join(tmp, "orglayer")
	seedCanonicalSkill(t, extRoot, ownershipProject, ownershipExtSkill)

	projDir := filepath.Join(tmp, ownershipProject)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":2,"project":"` + ownershipProject + `","skills":["` + ownershipExtSkill +
		`"],"sources":[{"id":"org","type":"local","path":` + strconv.Quote(extRoot) + `}]}`
	if err := os.WriteFile(filepath.Join(projDir, config.AgentsRCFile), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(projDir); err != nil {
		t.Fatal(err)
	}
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	t.Cleanup(func() { Flags = saved })

	var runErr error
	out := captureInstallStdout(t, func() {
		runErr = RunInstall(false, StdInstallDeps{})
	})
	if runErr != nil {
		t.Fatalf("RunInstall: %v", runErr)
	}
	if !strings.Contains(out, localSourceLogPrefix) {
		t.Errorf("authored path-bearing local source was not resolved:\n%s", out)
	}
	dest := filepath.Join(agentsHome, "skills", ownershipProject, ownershipExtSkill)
	info, err := os.Lstat(dest)
	if err != nil {
		t.Fatalf("skill from the authored local root not linked at %s: %v", dest, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to be a symlink into the authored local root", dest)
	}
}
