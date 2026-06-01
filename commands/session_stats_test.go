package commands

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// captureStatsStdout runs fn while os.Stdout is redirected; returns the
// captured bytes. Local helper to keep this file self-contained.
func captureStatsStdout(t *testing.T, fn func()) string {
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

func TestCommaInt(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}
	for _, tc := range tests {
		got := commaInt(tc.in)
		if got != tc.want {
			t.Errorf("commaInt(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exact-five", 10, "exact-five"},
		{"truncate-me-please", 8, "truncate"},
		{"", 5, ""},
	}
	for _, tc := range tests {
		got := truncate(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestFormatTimestamp_RFC3339(t *testing.T) {
	got := formatTimestamp("2025-06-15T12:34:56Z")
	if got != "2025-06-15T12:34Z" {
		t.Errorf("formatTimestamp = %q", got)
	}
}

func TestFormatTimestamp_RFC3339Nano(t *testing.T) {
	got := formatTimestamp("2025-06-15T12:34:56.789Z")
	if got != "2025-06-15T12:34Z" {
		t.Errorf("formatTimestamp nano = %q", got)
	}
}

func TestFormatTimestamp_InvalidPassthrough(t *testing.T) {
	got := formatTimestamp("not-a-timestamp")
	if got != "not-a-timestamp" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestFormatUnixMs(t *testing.T) {
	ms := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli()
	got := formatUnixMs(strconv.FormatInt(ms, 10))
	if got != "2026-01-02T03:04Z" {
		t.Errorf("formatUnixMs = %q", got)
	}
}

func TestFormatUnixMs_InvalidPassthrough(t *testing.T) {
	if got := formatUnixMs("not-numeric"); got != "not-numeric" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestRenderTokensByModel_EmitsHeader(t *testing.T) {
	tokens := map[string]platform.ModelTokenUsage{
		"sonnet-4.7": {InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 50, CacheCreationInputTokens: 10},
	}
	out := captureStatsStdout(t, func() { renderTokensByModel(tokens) })
	if !strings.Contains(out, "token usage by model:") {
		t.Errorf("missing header in output:\n%s", out)
	}
	if !strings.Contains(out, "sonnet-4.7") {
		t.Errorf("missing model name in output:\n%s", out)
	}
}

func TestRenderTokensByModel_EmptyNoOutput(t *testing.T) {
	out := captureStatsStdout(t, func() { renderTokensByModel(nil) })
	if out != "" {
		t.Errorf("expected no output for empty map, got %q", out)
	}
}

func TestRenderRecentSessions_EmitsHeader(t *testing.T) {
	sessions := []platform.SessionSummary{
		{ID: "abcdef12", Name: "feature branch", UpdatedAt: "2025-01-01T00:00:00Z"},
	}
	out := captureStatsStdout(t, func() { renderRecentSessions(sessions) })
	if !strings.Contains(out, "recent sessions:") {
		t.Errorf("missing header, got %q", out)
	}
	if !strings.Contains(out, "feature branch") {
		t.Errorf("missing session name, got %q", out)
	}
}

func TestRenderRecentSessions_EmptyNoOutput(t *testing.T) {
	out := captureStatsStdout(t, func() { renderRecentSessions(nil) })
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestRenderCommitAttribution_EmitsHeader(t *testing.T) {
	ms := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli()
	commits := []platform.CommitAttribution{
		{CommitHash: "abc12345", BranchName: "main", ScoredAt: strconv.FormatInt(ms, 10), LinesAdded: 10, LinesDeleted: 2, V2AIPercentage: 65.5},
	}
	out := captureStatsStdout(t, func() { renderCommitAttribution(commits) })
	if !strings.Contains(out, "recent commits") {
		t.Errorf("missing header, got %q", out)
	}
	if !strings.Contains(out, "main") {
		t.Errorf("missing branch, got %q", out)
	}
}

func TestRenderCommitAttribution_EmptyNoOutput(t *testing.T) {
	out := captureStatsStdout(t, func() { renderCommitAttribution(nil) })
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestRenderPlatformStats_FullStats(t *testing.T) {
	stats := &platform.PlatformUsageStats{
		PlatformID:    "claude-code",
		TotalSessions: 12,
		TotalMessages: 345,
		TokensByModel: map[string]platform.ModelTokenUsage{
			"opus-4.7": {InputTokens: 100},
		},
		DailyActivity: []platform.DailyUsage{
			{Date: "2026-05-01", MessageCount: 9, SessionCount: 3},
		},
		RecentSessions: []platform.SessionSummary{
			{ID: "s1", Name: "loop", UpdatedAt: "2025-01-01T00:00:00Z"},
		},
	}
	out := captureStatsStdout(t, func() { renderPlatformStats(stats) })
	for _, want := range []string{"sessions: 12", "messages: 345", "token usage by model:", "recent daily activity:", "recent sessions:", "2026-05-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderPlatformStats_SessionsOnly(t *testing.T) {
	stats := &platform.PlatformUsageStats{TotalSessions: 4}
	out := captureStatsStdout(t, func() { renderPlatformStats(stats) })
	if !strings.Contains(out, "sessions: 4") {
		t.Errorf("missing sessions count, got %q", out)
	}
	if strings.Contains(out, "messages:") {
		t.Errorf("did not expect messages label, got %q", out)
	}
}

func TestRunSessionStats_NoPanic(t *testing.T) {
	// Smoke test: should terminate cleanly even with no platform data on
	// disk. Output is platform-dependent; assert at least one section
	// header is printed.
	out := captureStatsStdout(t, func() {
		if err := runSessionStats(&cobra.Command{}, nil); err != nil {
			t.Fatalf("runSessionStats: %v", err)
		}
	})
	if !strings.Contains(out, "##") {
		t.Errorf("expected at least one platform header in output:\n%s", out)
	}
}

func TestNewSessionStatsCmd_Metadata(t *testing.T) {
	cmd := newSessionStatsCmd()
	if cmd.Use != "stats" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("stats subcommand should have RunE")
	}
}

// TestRunSessionStats_WithSeededStatsCacheCoversRenderBranch covers
// session_stats.go:55 (`renderPlatformStats(stats)` when stats != nil). On CI
// bare runners no ~/.claude/stats-cache.json exists, so claudeReadUsageStats
// returns nil and the function takes the `(no data available)` branch.
func TestRunSessionStats_WithSeededStatsCacheCoversRenderBranch(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)

	cache := `{
		"totalSessions": 1,
		"totalMessages": 2,
		"modelUsage": {
			"claude-sonnet-4-6": {
				"inputTokens": 100,
				"outputTokens": 200,
				"cacheReadInputTokens": 50,
				"cacheCreationInputTokens": 25
			}
		},
		"dailyActivity": [
			{"date": "2026-05-15", "messageCount": 2, "sessionCount": 1, "toolCallCount": 4}
		]
	}`
	if err := os.WriteFile(filepath.Join(tmp, ".claude", "stats-cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runSessionStats(nil, nil); err != nil {
		t.Errorf("runSessionStats with seeded stats cache: %v", err)
	}
}
