package commands

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// This file pins the GROUND TRUTH for how `da refresh` treats the three
// manifest projection flags — `hooks`, `mcp`, `settings` — once config layers
// (`extends`) are in play.
//
// Background: PR #535 made the three keys POINTERS on AgentsRC so an ABSENT key
// stays distinguishable from an explicit `false` across a load→save round trip.
// That fixed the MARSHAL side (refresh no longer injects `"hooks": false` into a
// manifest that omitted it). A follow-up field report claimed the READ side was
// still broken — that refresh consulted the flat repo-local boolean instead of
// the layer-resolved effective value, so an org layer's `hooks: true` was
// ignored and hook projections were dropped.
//
// The audit these tests encode found something different, and it is the reason
// they are written as an invariant rather than a bug reproduction: refresh does
// not read hooks/mcp/settings AT ALL — neither flat nor effective. The three
// keys participate in the layer merge (so `da config explain hooks` correctly
// reports an org layer's `true`), but no projection path consumes the result.
// Hook/MCP/settings projection is driven purely by what exists under
// ~/.agents/{hooks,mcp,settings}/<scope>/ (internal/platform's resolveHookSpec /
// resolveScopedFile), which is why the reported symptom could not have this
// cause.
//
// Two invariants therefore matter and are asserted together on every case:
//
//  1. The MERGE side is correct — the effective value is what the layer stack
//     says it is (absent defers to the layer; an explicit repo-local value wins).
//     A regression here would silently re-break `da config explain`.
//
//  2. Projection is flag-blind — the same artifacts land regardless. The
//     payout-report shape (key absent, org layer says true) MUST project, which
//     is the row that protects against the reported failure mode. The remaining
//     rows document that an explicit `false` does not currently suppress
//     anything, so wiring the flags into projection later shows up here as a
//     deliberate, reviewed change rather than a silent one. That wiring is NOT a
//     free fix: manifests written before #535 carry an injected `"hooks": false`
//     they never authored, so honoring it would disable hooks fleet-wide.

// projectionFlagsHookCommand is the command the seeded canonical hook bundle
// runs. Asserting on it (rather than merely on file existence) proves the
// rendered settings file actually carries the projected hook.
const projectionFlagsHookCommand = "/bin/echo projected"

// seedProjectionFlagsHome prepares an isolated HOME/AGENTS_HOME with every
// platform detectable, plus one canonical hook bundle, one MCP config, and one
// cursor settings file in the GLOBAL scope — the three home-store inputs whose
// projection the hooks/mcp/settings manifest keys nominally govern. Returns the
// temp HOME.
func seedProjectionFlagsHome(t *testing.T) string {
	t.Helper()
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	writeProjectionFile(t, filepath.Join(agentsHome, "hooks", "global", "probe", "HOOK.yaml"),
		"name: probe\nwhen: post_tool_use\nrun:\n  command: "+projectionFlagsHookCommand+"\n")
	writeProjectionFile(t, filepath.Join(agentsHome, "mcp", "global", "mcp.json"),
		`{"mcpServers":{"probe":{"command":"/bin/echo","args":["mcp"]}}}`)
	writeProjectionFile(t, filepath.Join(agentsHome, "settings", "global", "cursor.json"),
		`{"projectionProbe":true}`)
	return tmp
}

// seedProjectionFlagsLayer writes an extends layer declaring the three keys with
// the given boolean and returns the local source root holding it.
func seedProjectionFlagsLayer(t *testing.T, value bool) string {
	t.Helper()
	src := t.TempDir()
	body := `{"hooks":` + strconv.FormatBool(value) +
		`,"mcp":` + strconv.FormatBool(value) +
		`,"settings":` + strconv.FormatBool(value) + `}`
	writeProjectionFile(t, filepath.Join(src, "org", "base.json"), body)
	return src
}

func writeProjectionFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectionFlagsManifest builds a manifest for a repo named "p". repoDecl is
// the raw JSON fragment for the three keys (empty ⇒ all three keys ABSENT);
// layerSrc, when non-empty, adds the local source + extends entry that pulls in
// the layer written by seedProjectionFlagsLayer.
func projectionFlagsManifest(repoDecl, layerSrc string) string {
	sources := `{"type":"local"}`
	extends := ""
	if layerSrc != "" {
		sources += `,{"id":"org","type":"local","path":` + strconv.Quote(layerSrc) + `}`
		extends = `,"extends":["org:org/base.json"]`
	}
	body := `{"version":2,"project":"p","sources":[` + sources + `]` + extends
	if repoDecl != "" {
		body += "," + repoDecl
	}
	return body + "}"
}

// refreshProjectionFlagsRepo registers a repo carrying manifest and runs the real
// `da refresh` over it. Returns the repo path.
func refreshProjectionFlagsRepo(t *testing.T, home, manifest string) string {
	t.Helper()
	repo := filepath.Join(home, "p")
	writeProjectionFile(t, filepath.Join(repo, config.AgentsRCFile), manifest)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", repo)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(refreshScope{Project: "p"}, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}
	return repo
}

// assertProjectionFlagsEffective checks the LAYER MERGE half: the effective
// hooks/mcp/settings the resolver reports, read back offline through the same
// lock-backed surface `da config explain` uses. want is "absent", "true", or
// "false".
func assertProjectionFlagsEffective(t *testing.T, repo, want string) {
	t.Helper()
	snap, err := config.NewLayeredResolver().ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}
	if got := describeEffectiveStringsOrBool(snap.Effective.Hooks); got != want {
		t.Errorf("effective hooks = %s, want %s", got, want)
	}
	if got := describeEffectiveStringsOrBool(snap.Effective.MCP); got != want {
		t.Errorf("effective mcp = %s, want %s", got, want)
	}
	if got := describeEffectiveBool(snap.Effective.Settings); got != want {
		t.Errorf("effective settings = %s, want %s", got, want)
	}
}

func describeEffectiveStringsOrBool(s *config.StringsOrBool) string {
	if s == nil {
		return "absent"
	}
	return strconv.FormatBool(s.All)
}

func describeEffectiveBool(b *bool) string {
	if b == nil {
		return "absent"
	}
	return strconv.FormatBool(*b)
}

// assertProjectionFlagsProjected checks the PROJECTION half: all three
// home-store inputs reached the repo. Projection is currently flag-blind, so
// every case expects the same artifacts.
func assertProjectionFlagsProjected(t *testing.T, repo string) {
	t.Helper()
	settingsLocal := filepath.Join(repo, ".claude", "settings.local.json")
	rendered, err := os.ReadFile(settingsLocal)
	if err != nil {
		t.Fatalf("hooks: reading %s: %v", settingsLocal, err)
	}
	if !strings.Contains(string(rendered), projectionFlagsHookCommand) {
		t.Errorf("hooks: %s missing projected hook command %q; got:\n%s",
			settingsLocal, projectionFlagsHookCommand, rendered)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".mcp.json")); err != nil {
		t.Errorf("mcp: .mcp.json not projected: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, ".cursor", "settings.json")); err != nil {
		t.Errorf("settings: .cursor/settings.json not projected: %v", err)
	}
}

// runProjectionFlagsCase is the whole body of one table row, kept out of the
// t.Run closure so the closure stays a one-liner (S3776).
func runProjectionFlagsCase(t *testing.T, repoDecl string, layer *bool, wantEffective string) {
	t.Helper()
	home := seedProjectionFlagsHome(t)
	layerSrc := ""
	if layer != nil {
		layerSrc = seedProjectionFlagsLayer(t, *layer)
	}
	repo := refreshProjectionFlagsRepo(t, home, projectionFlagsManifest(repoDecl, layerSrc))
	assertProjectionFlagsEffective(t, repo, wantEffective)
	assertProjectionFlagsProjected(t, repo)
}

func TestRunRefresh_ProjectionIgnoresHooksMCPSettingsFlags(t *testing.T) {
	yes, no := true, false
	const allTrue = `"hooks":true,"mcp":true,"settings":true`
	const allFalse = `"hooks":false,"mcp":false,"settings":false`

	cases := []struct {
		name          string
		repoDecl      string
		layer         *bool
		wantEffective string
	}{
		// Plain repo, no layers: absent stays absent and everything projects.
		// This row is the "zero change for plain repos" guard.
		{"absent-no-layer", "", nil, "absent"},
		// THE PAYOUT-REPORT SHAPE. Manifest omits all three; an org layer says
		// true. The effective value is true (what `da config explain hooks`
		// shows) and the artifacts must land.
		{"absent-layer-true", "", &yes, "true"},
		// Absent + layer false: the layer wins the merge. Projection happens
		// anyway today — the flags gate nothing.
		{"absent-layer-false", "", &no, "false"},
		// Explicit repo-local false beats an org layer's true in the merge
		// (this is the precedence #535 exists to protect). Projection is still
		// unaffected.
		{"repo-false-layer-true", allFalse, &yes, "false"},
		// Explicit repo-local true beats an org layer's false.
		{"repo-true-layer-false", allTrue, &no, "true"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runProjectionFlagsCase(t, tc.repoDecl, tc.layer, tc.wantEffective)
		})
	}
}
