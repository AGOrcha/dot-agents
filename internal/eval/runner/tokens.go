package runner

import (
	"strings"

	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// scanFn is the token-scanner seam. It matches
// platform.SessionTokenScanner.ScanSessionTokens so an adapter can drive the
// real platform scanner against the sandbox scratch HOME in production and a
// deterministic fake in tests. Adapters whose CLI populates a native session
// store (copilot's ~/.copilot/session-state, claude's ~/.claude/projects) wire
// this after the run to recover token telemetry the CLI did not print inline.
type scanFn func(home, projectPath, sessionID, afterTimestamp string) platform.SessionTokenMetrics

// scratchHomeFromEnv extracts the sandbox scratch HOME from a sandbox
// Instance's Env slice. sandbox.Instance pins HOME (and USERPROFILE on
// Windows) to the scratch home; the platform token scanners read a platform's
// native session store rooted there. Later entries win because the sandbox
// appends its overrides last, so the slice is scanned in reverse.
func scratchHomeFromEnv(env []string) string {
	if h := lastValueFor(env, "HOME="); h != "" {
		return h
	}
	return lastValueFor(env, "USERPROFILE=")
}

// lastValueFor returns the value of the last KEY=VALUE entry in env whose key
// matches prefix (including the trailing '='), or "" when none match.
func lastValueFor(env []string, prefix string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], prefix); ok {
			return v
		}
	}
	return ""
}

// tokensFromMetrics maps a platform SessionTokenMetrics into the canonical
// scoring.TokenUsage, returning nil when the scan found nothing (all-zero) so
// an absent scan stays a first-class absent signal rather than a misleading
// zero score. CacheHitRate is derived from the read/creation split when the
// scanner did not report one directly.
func tokensFromMetrics(m platform.SessionTokenMetrics) *scoring.TokenUsage {
	if m.InputTokens == 0 && m.OutputTokens == 0 &&
		m.CacheReadTokens == 0 && m.CacheCreationTokens == 0 {
		return nil
	}
	hit := m.CacheHitRate
	if hit == 0 {
		if total := m.CacheReadTokens + m.CacheCreationTokens; total > 0 {
			hit = float64(m.CacheReadTokens) / float64(total)
		}
	}
	return &scoring.TokenUsage{
		InputTokens:         m.InputTokens,
		OutputTokens:        m.OutputTokens,
		CacheReadTokens:     m.CacheReadTokens,
		CacheCreationTokens: m.CacheCreationTokens,
		CacheHitRate:        hit,
	}
}

// asScanner adapts a platform constructor's Platform to the SessionTokenScanner
// seam. Every v1 adapter's platform type implements SessionTokenScanner, so the
// assertion is total; a future platform that does not would panic loudly at
// construction rather than silently dropping telemetry.
func asScanner(p platform.Platform) scanFn {
	return p.(platform.SessionTokenScanner).ScanSessionTokens
}
