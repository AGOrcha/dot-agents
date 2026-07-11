package platform

// SessionReader is implemented by platforms that expose structured runtime
// session data. It is the read-side complement to the write-side Platform
// interface and is kept separate to avoid burdening platforms that do not
// yet have confirmed session env var contracts.
//
// Implement on a platform struct to make it participate in agent identity
// detection at `da workflow checkpoint` time. Stubs that return "" / nil are
// valid until the env var contract for that platform is confirmed.
type SessionReader interface {
	// AIAgentPrefix returns the harness prefix used in the AI_AGENT env var
	// convention (e.g. "claude-code" for AI_AGENT=claude-code_2-1-138_agent).
	// Returns "" if this platform does not follow the AI_AGENT convention.
	AIAgentPrefix() string
	// SessionEnvs lists env var names (in preference order) that carry the
	// active session ID. First non-empty value wins.
	SessionEnvs() []string
	// EntrypointEnvs lists env var names for the session launch entrypoint.
	EntrypointEnvs() []string
	// ResolveModel scans the platform's session store for the model active in
	// the given session. Returns "" when unavailable or not yet implemented.
	ResolveModel(home, projectPath, sessionID string) string
}

// StatsReader is implemented by platforms that expose pre-aggregated usage
// statistics. ReadUsageStats returns nil when the platform's stats file is
// absent or not yet implemented.
type StatsReader interface {
	ReadUsageStats(home string) *PlatformUsageStats
}

// SessionTokenScanner is implemented by platforms that support per-iteration
// token usage scanning from their session JSONL. Returns a zero
// SessionTokenMetrics when no matching entries exist or the session file is
// absent.
type SessionTokenScanner interface {
	ScanSessionTokens(home, projectPath, sessionID, afterTimestamp string) SessionTokenMetrics
}

// BranchSessionFinder is implemented by platforms that embed git branch
// metadata in their session files, allowing orient to surface recent sessions
// on the current branch.
type BranchSessionFinder interface {
	FindSessionsOnBranch(home, projectPath, branch string, maxResults int) []BranchSessionInfo
}

// Platform defines the interface all AI agent platforms must implement.
type Platform interface {
	// ID returns the platform identifier (e.g. "cursor", "claude").
	ID() string
	// DisplayName returns the human-readable name.
	DisplayName() string
	// IsInstalled checks if this platform is installed on the system.
	IsInstalled() bool
	// Version returns the detected version string, or empty string.
	Version() string
	// CreateLinks creates all managed links for a project in repoPath.
	CreateLinks(project, repoPath string) error
	// RemoveLinks removes all managed links for a project from repoPath.
	RemoveLinks(project, repoPath string) error
	// HasDeprecatedFormat checks if the project has deprecated config files.
	HasDeprecatedFormat(repoPath string) bool
	// DeprecatedDetails returns a description of the deprecated format.
	DeprecatedDetails(repoPath string) string
	// SharedTargetIntents returns the ResourceIntents this platform would write
	// to shared (cross-platform) repo-local targets such as .agents/skills/*.
	// These intents are aggregated by the command layer into a single
	// ResourcePlan so compatible targets are deduped and conflicts are caught
	// before any filesystem writes occur.
	SharedTargetIntents(project string) ([]ResourceIntent, error)
}

// All returns the ordered list of all supported platforms.
func All() []Platform {
	return []Platform{
		NewCursor(),
		NewClaude(),
		NewCodex(),
		NewOpenCode(),
		NewCopilot(),
		NewAntigravity(),
	}
}

// ByID returns the platform with the given ID, or nil.
func ByID(id string) Platform {
	for _, p := range All() {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// ManagedOutputReporter is implemented by platforms whose generated repo-local
// outputs are dynamic enough (or divergent enough from the static table) to
// warrant per-platform enumeration — e.g. Copilot's rendered .github/hooks/*.json
// fanout, which is not a single owned directory like the other platforms'
// surfaces. A platform that does not implement it falls back to the
// staticManagedOutputs table in CollectManagedOutputs. The returned entries are
// repo-relative .gitignore patterns (forward-slash, trailing slash for
// directories) for the managed-.gitignore block (config-distribution-model §15
// / D14 / R8).
type ManagedOutputReporter interface {
	ManagedOutputs() []string
}

// staticManagedOutputs maps a platform ID to the repo-relative .gitignore
// patterns for the outputs `da refresh` projects/generates into a consuming
// project (config-distribution-model §15 / D14): projected platform links,
// generated platform configs, and the shared skills/agents mirrors. These are
// machine-materialized outputs that must stay out of git; the committed
// resolved-state contract (.agentsrc.json/.agentsrc.lock) is filtered back out
// by links.EnsureManagedGitignore, so it is never listed here even though
// refresh materializes the lock. Platforms with a dynamic surface implement
// ManagedOutputReporter instead of appearing here (see copilot).
var staticManagedOutputs = map[string][]string{
	"cursor": {cursorDir + "/", ".cursorrules", ".cursorignore"},
	"claude": {
		claudeDir + "/",
		claudeMCPFile,
		"CLAUDE.md",
		claudeAgentsBucketDir + "/agents/",
		claudeAgentsBucketDir + "/skills/",
	},
	"codex":       {codexDir + "/", codexAgentsMarkdown, codexAgentsDir + "/skills/"},
	"opencode":    {opencodeDir + "/", opencodeJSON, opencodeAgentsDir + "/skills/"},
	"antigravity": {antigravityDir + "/"},
}

// CollectManagedOutputs aggregates the repo-relative managed-output .gitignore
// patterns for the given platforms (typically the enabled set). A platform that
// implements ManagedOutputReporter supplies its own patterns; otherwise the
// static staticManagedOutputs table is consulted. The raw union is returned
// unsorted and possibly duplicated — links.EnsureManagedGitignore normalizes,
// de-duplicates, sorts, and filters the never-ignored contract files, so this
// collector never has to.
func CollectManagedOutputs(platforms []Platform) []string {
	var out []string
	for _, p := range platforms {
		if r, ok := p.(ManagedOutputReporter); ok {
			out = append(out, r.ManagedOutputs()...)
			continue
		}
		out = append(out, staticManagedOutputs[p.ID()]...)
	}
	return out
}
