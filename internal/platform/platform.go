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
