package platform

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	_ "modernc.org/sqlite" // register SQLite driver for database/sql
)

type opencode struct {
	io platformIO
}

const (
	opencodeJSON = "opencode.json"
	opencodeDir  = ".opencode"
)

func NewOpenCode() Platform { return &opencode{io: stdPlatformIO{}} }

func (o *opencode) ID() string          { return "opencode" }
func (o *opencode) DisplayName() string { return "OpenCode" }

// SessionReader — env var contract not yet confirmed.
func (o *opencode) AIAgentPrefix() string              { return "opencode" }
func (o *opencode) SessionEnvs() []string              { return []string{"OPENCODE_SESSION_ID"} }
func (o *opencode) EntrypointEnvs() []string           { return nil }
func (o *opencode) ResolveModel(_, _, _ string) string { return "" }

// StatsReader — OpenCode session schema not yet confirmed for aggregated stats.
func (o *opencode) ReadUsageStats(_ string) *PlatformUsageStats { return nil }

// SessionTokenScanner implementation.
// Queries ~/.local/share/opencode/opencode.db (part table, type='step-finish').
// Since OpenCode injects no session ID env var, filtering is by message
// created_at (Unix ms) > afterTimestamp. All OpenCode usage across projects
// in the time window is included.
func (o *opencode) ScanSessionTokens(home, _, _, afterTimestamp string) SessionTokenMetrics {
	return opencodeScanSessionTokens(home, afterTimestamp)
}

// opencodeScanSessionTokens queries the OpenCode SQLite database for
// step-finish token totals after afterTimestamp.
// Fields from the part.data JSON column: $.tokens.input, $.tokens.output,
// $.tokens.cache.read, $.tokens.cache.write (all floats in the DB).
func opencodeScanSessionTokens(home, afterTimestamp string) SessionTokenMetrics {
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return SessionTokenMetrics{}
	}
	defer db.Close()

	var afterMs int64
	if afterTimestamp != "" {
		if t, err := time.Parse(time.RFC3339, afterTimestamp); err == nil {
			afterMs = t.UnixMilli()
		}
	}

	const query = `
		SELECT
			COALESCE(SUM(CAST(json_extract(p.data, '$.tokens.input')       AS INTEGER)), 0),
			COALESCE(SUM(CAST(json_extract(p.data, '$.tokens.output')      AS INTEGER)), 0),
			COALESCE(SUM(CAST(json_extract(p.data, '$.tokens.cache.read')  AS INTEGER)), 0),
			COALESCE(SUM(CAST(json_extract(p.data, '$.tokens.cache.write') AS INTEGER)), 0),
			COALESCE(COUNT(*), 0)
		FROM part p
		JOIN message m ON p.message_id = m.id
		WHERE p.type = 'step-finish'
		  AND (? = 0 OR m.created_at > ?)`

	var m SessionTokenMetrics
	row := db.QueryRow(query, afterMs, afterMs)
	if err := row.Scan(
		&m.InputTokens,
		&m.OutputTokens,
		&m.CacheReadTokens,
		&m.CacheCreationTokens,
		&m.MessageCount,
	); err != nil {
		return SessionTokenMetrics{}
	}
	return m
}

func (o *opencode) IsInstalled() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func (o *opencode) Version() string {
	out, err := exec.Command("opencode", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func (o *opencode) HasDeprecatedFormat(repoPath string) bool { return false }
func (o *opencode) DeprecatedDetails(repoPath string) string { return "" }

func (o *opencode) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := o.ensureUserAgents(agentsHome); err != nil {
		return err
	}

	// opencode.json config
	if src := resolveScopedFile(agentsHome, "settings", project, opencodeJSON); src != "" {
		// Managed-replace at a fixed owned repo path (opencode.json).
		if err := links.SymlinkReplacing(src, filepath.Join(repoPath, opencodeJSON), backupSidecar); err != nil {
			return err
		}
	}

	// .opencode/agent/*.md and .agents/skills/ — emitted by CollectAndExecuteSharedTargetPlan
	// via SharedTargetIntents; no direct action needed here.

	return nil
}

// opencodeConfigSources mirrors CreateLinks' resolveScopedFile call so a
// Windows hard-linked managed opencode.json (no reparse point) is cleaned up
// the same way RemoveIfSymlinkUnder drops a POSIX symlink.
func opencodeConfigSources(agentsHome, project string) []string {
	var srcs []string
	for _, scope := range scopedNames(project) {
		srcs = append(srcs, filepath.Join(agentsHome, "settings", scope, opencodeJSON))
	}
	return srcs
}

func (o *opencode) ensureUserAgents(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, opencodeDir, "agent")
		if err := syncScopedFileSymlinks(o.io, agentsHome, "agents", "global", "AGENT.md", userAgentsDir, ".md"); err != nil {
			return err
		}
	}
	return nil
}

func (o *opencode) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	var errs []error

	cfg := filepath.Join(repoPath, opencodeJSON)
	errs = append(errs,
		links.RemoveIfSymlinkUnder(cfg, agentsHome),
		removeHardlinkedManaged(cfg, opencodeConfigSources(agentsHome, project)),
	)

	agentDir := filepath.Join(repoPath, opencodeDir, "agent")
	if entries, err := os.ReadDir(agentDir); err == nil {
		for _, e := range entries {
			dst := filepath.Join(agentDir, e.Name())
			name := strings.TrimSuffix(e.Name(), ".md")
			errs = append(errs,
				links.RemoveIfSymlinkUnder(dst, agentsHome),
				removeHardlinkedManaged(dst, scopedAgentFileSources(agentsHome, project, name, ".md")),
			)
		}
	}

	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome))
		}
	}

	return errors.Join(errs...)
}

// BrokenLinks implements BrokenLinkReporter for the opencode platform.
//
// Opencode's project-scope single-file managed surface is opencode.json at
// the repo root — a managed symlink to the canonical settings source under
// <agentsHome>/settings/<scope>/opencode.json (see CreateLinks). The
// diagnostic contract preserved from doctor's previous inline
// projectSingleFiles table is "report broken only when the entry is a
// resolvable managed link AND its target is missing" — a hard-linked or
// absent opencode.json is silently passed over. classifyManagedLink encodes
// that semantic exactly, matching the claude.brokenMCPLink shape.
//
// PlatformID is set on the returned BrokenLink so JSON consumers can
// self-describe per-entry.
func (o *opencode) BrokenLinks(_, repoPath, _ string) []BrokenLink {
	linkPath := filepath.Join(repoPath, opencodeJSON)
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		return nil
	}
	return []BrokenLink{{
		PlatformID:  "opencode",
		LinkPath:    linkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
	}}
}

func (o *opencode) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(".agents", "skills"))
	if err != nil {
		return nil, err
	}
	plugins, err := BuildSharedPluginBundleIntents(project, filepath.Join(opencodeDir, "plugins"))
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentFileSymlinkIntents(project, filepath.Join(opencodeDir, "agent"), ".md")
	if err != nil {
		return nil, err
	}
	return append(append(skills, plugins...), agents...), nil
}

// CountLinks implements LinkCounter for the opencode platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter that previously lived in status.go's
// opencodeTextBadge.
//
// Healthy: opencode.json when it is a resolvable managed link, plus any
// resolvable entry under .opencode/agent/ or .agents/skills/. Broken: a
// resolvable managed link whose target is missing.
func (o *opencode) CountLinks(_, repoPath, _ string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, opencodeJSON),
	})
	addManagedDirCounts(&ok, &broken, []string{
		filepath.Join(repoPath, opencodeDir, "agent"),
		filepath.Join(repoPath, ".agents", "skills"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the opencode platform.
func (o *opencode) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := o.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: "OpenCode", Present: ok > 0, Broken: broken > 0}
}
