package main

// This file is the single source of truth for sanctioned raw os.* fs-mutator
// call sites — the deliberate exceptions to the leverage-cross-platform-fs-helpers
// rule that fsguard enforces. There are two tiers:
//
//   1. preciseAllow — file:line entries for calls that are sanctioned FOREVER
//      because fsops has no equivalent primitive (e.g. the atomic mkdir-as-lock).
//      Each entry names the exact site and the reason it can never move to fsops.
//
//   2. grandfatheredPackages — packages that already used raw os.* mutators when
//      this guard landed. They are migration DEBT, not endorsed exceptions: the
//      guard grandfathers the whole package so the gate can ship without a giant
//      migration, then ratchets — a NEW raw mutator in a package NOT on this list
//      fails CI, and entries are deleted as packages migrate to fsops.
//
// Adding to grandfatheredPackages for new code is a review red flag: prefer
// fsops.* (or, for a genuine fsops-less primitive, a preciseAllow entry with a
// reason). The lesson is .agents/lessons/leverage-cross-platform-fs-helpers.

// allowEntry is one sanctioned site, keyed by module-relative slash path + line.
type allowEntry struct {
	relPath string
	line    int
	reason  string
}

// preciseAllow lists calls sanctioned forever: fsops offers no equivalent, so
// these stay on raw os.*. Line numbers must be kept current with the source.
var preciseAllow = []allowEntry{
	{
		relPath: "internal/agentslock/lockfile.go",
		line:    317,
		reason: "atomic mkdir-as-lock: os.Mkdir of the sidecar lock dir IS the " +
			"mutual-exclusion primitive (its EEXIST result is the contention " +
			"signal). fsops has no atomic-mkdir-lock equivalent. The PARENT " +
			"directory is created via fsops.MkdirAll just above — that was the " +
			"#148 Windows fix; only this single-component atomic create stays raw.",
	},
}

// reasonUnmigrated is the bare "predates the guard, not yet migrated" reason
// shared by packages with no further detail to record.
const reasonUnmigrated = "predates fsguard; not yet migrated"

// grandfatheredPackages enumerates packages with pre-existing raw os.* mutators
// at the time fsguard landed. The reason records WHY it is still on the list
// (almost always: predates the guard, not yet migrated). Migrating a package to
// fsops and deleting its entry here is the intended direction of travel.
var grandfatheredPackages = map[string]string{
	"github.com/AGOrcha/dot-agents/cmd/globalflag-coverage":     "predates fsguard; dev tool, not yet migrated",
	"github.com/AGOrcha/dot-agents/commands":                    "predates fsguard; import/review/remove deps wrappers, not yet migrated",
	"github.com/AGOrcha/dot-agents/commands/agents":             reasonUnmigrated,
	"github.com/AGOrcha/dot-agents/commands/hooks":              reasonUnmigrated,
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil":   reasonUnmigrated,
	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle": "predates fsguard; init/install/backup/kgmcp, not yet migrated",
	"github.com/AGOrcha/dot-agents/commands/kg":                 "predates fsguard; io/bridge/query deps wrappers, not yet migrated",
	"github.com/AGOrcha/dot-agents/commands/skills":             "predates fsguard; io deps wrapper, not yet migrated",
	"github.com/AGOrcha/dot-agents/commands/sync":               reasonUnmigrated,
	"github.com/AGOrcha/dot-agents/commands/workflow":           "predates fsguard; fs/delegation/sweep/prefs/hook seams, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/config":             "predates fsguard; config/proposals/agentsrc writers, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/credstore":          "predates fsguard; sys deps wrapper, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/gitwt":              "predates fsguard; worktree setup, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/graphstore":         "predates fsguard; sqlite db-dir create, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/links":              "predates fsguard; symlink setup, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/linktest":           "predates fsguard; symlink test helper, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/platform":           "predates fsguard; claude/codex/hooks/resource_plan, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/projectsync":        "predates fsguard; promote/journal/sync, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/review/auth":        "predates fsguard; users.go atomic write, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/scaffold/home":      "predates fsguard; embed extraction, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/scaffold/hooks":     "predates fsguard; embed extraction, not yet migrated",
	"github.com/AGOrcha/dot-agents/internal/testutil":           "predates fsguard; test-fixture helpers, not yet migrated",
}

// allowed reports whether a raw mutator at (pkgPath, relPath, line) is
// sanctioned. A precise file:line match wins; otherwise the package must be
// grandfathered. fsguard's own package is exempt so its tests can plant a
// synthetic os.Mkdir to prove the detector fires.
func allowed(pkgPath, relPath string, line int) bool {
	if pkgPath == modulePath+"/tools/fsguard" {
		return true
	}
	for _, e := range preciseAllow {
		if e.relPath == relPath && e.line == line {
			return true
		}
	}
	_, ok := grandfatheredPackages[pkgPath]
	return ok
}
