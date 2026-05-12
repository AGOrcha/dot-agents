package platform

// SessionTokenMetrics holds per-iteration token usage aggregated from a
// session's JSONL, time-windowed by checkpoint_at. Used by SessionTokenScanner.
type SessionTokenMetrics struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int     // Codex o-series reasoning tokens; 0 for Claude Code
	CacheHitRate        float64 // CacheReadTokens/(CacheReadTokens+CacheCreationTokens); 0 = unavailable
	MessageCount        int
}

// PlatformUsageStats holds pre-aggregated usage data from a platform's native
// store. Used by StatsReader.ReadUsageStats.
type PlatformUsageStats struct {
	PlatformID        string
	TotalSessions     int
	TotalMessages     int
	TokensByModel     map[string]ModelTokenUsage
	DailyActivity     []DailyUsage
	RecentSessions    []SessionSummary
	CommitAttribution []CommitAttribution
}

// ModelTokenUsage holds cumulative token counts for a single model.
type ModelTokenUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// DailyUsage holds activity counts for a single day.
type DailyUsage struct {
	Date          string
	MessageCount  int
	SessionCount  int
	ToolCallCount int
}

// SessionSummary holds a brief description of a session.
type SessionSummary struct {
	ID        string
	Name      string
	UpdatedAt string
}

// CommitAttribution holds AI vs human code attribution for a single commit.
// Sourced from Cursor's scored_commits table.
type CommitAttribution struct {
	CommitHash           string
	BranchName           string
	ScoredAt             string
	LinesAdded           int
	LinesDeleted         int
	ComposerLinesAdded   int
	ComposerLinesDeleted int
	HumanLinesAdded      int
	V2AIPercentage       float64
}

// BranchSessionInfo holds brief info about a session that was active on a
// specific git branch. Used by BranchSessionFinder.
type BranchSessionInfo struct {
	SessionID    string
	Timestamp    string
	MessageCount int
}
