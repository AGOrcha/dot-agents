package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// NewSessionCmd builds the `da session` command group.
func NewSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Inspect platform session data",
	}
	cmd.AddCommand(newSessionStatsCmd())
	return cmd
}

func newSessionStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show usage statistics from each installed AI platform",
		RunE:  runSessionStats,
	}
}

func runSessionStats(_ *cobra.Command, _ []string) error {
	home, err := config.UserHomeDir()
	if err != nil {
		return err
	}

	first := true
	for _, p := range platform.All() {
		sr, ok := p.(platform.StatsReader)
		if !ok {
			continue
		}
		stats := sr.ReadUsageStats(home)
		if !first {
			fmt.Println("---")
		}
		first = false

		fmt.Printf("## %s\n", p.DisplayName())
		if stats == nil {
			fmt.Println("(no data available)")
			continue
		}
		renderPlatformStats(stats)
	}
	return nil
}

func renderTokensByModel(tokensByModel map[string]platform.ModelTokenUsage) {
	if len(tokensByModel) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("token usage by model:")
	models := make([]string, 0, len(tokensByModel))
	for m := range tokensByModel {
		models = append(models, m)
	}
	sort.Strings(models)
	for _, model := range models {
		u := tokensByModel[model]
		fmt.Printf("  %-45s  in: %-14s  out: %-14s  cache-read: %-14s  cache-write: %s\n",
			model,
			commaInt(u.InputTokens),
			commaInt(u.OutputTokens),
			commaInt(u.CacheReadInputTokens),
			commaInt(u.CacheCreationInputTokens))
	}
}

func renderRecentSessions(sessions []platform.SessionSummary) {
	if len(sessions) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("recent sessions:")
	for _, sess := range sessions {
		ts := formatTimestamp(sess.UpdatedAt)
		fmt.Printf("  %-38s  %s  %s\n", truncate(sess.Name, 38), ts, truncate(sess.ID, 8))
	}
}

func renderCommitAttribution(commits []platform.CommitAttribution) {
	if len(commits) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("recent commits (AI attribution):")
	for _, c := range commits {
		ts := formatUnixMs(c.ScoredAt)
		fmt.Printf("  %s  %s  %-38s  AI: %5.1f%%  +%d/-%d\n",
			truncate(c.CommitHash, 8), ts,
			truncate(c.BranchName, 38),
			c.V2AIPercentage,
			c.LinesAdded, c.LinesDeleted)
	}
}

func renderPlatformStats(s *platform.PlatformUsageStats) {
	if s.TotalSessions > 0 || s.TotalMessages > 0 {
		if s.TotalMessages > 0 {
			fmt.Printf("sessions: %s    messages: %s\n",
				commaInt(s.TotalSessions), commaInt(s.TotalMessages))
		} else {
			fmt.Printf("sessions: %s\n", commaInt(s.TotalSessions))
		}
	}

	renderTokensByModel(s.TokensByModel)

	if len(s.DailyActivity) > 0 {
		fmt.Println()
		fmt.Println("recent daily activity:")
		for _, d := range s.DailyActivity {
			fmt.Printf("  %s  msgs: %-6s  sessions: %s\n",
				d.Date, commaInt(d.MessageCount), commaInt(d.SessionCount))
		}
	}

	renderRecentSessions(s.RecentSessions)
	renderCommitAttribution(s.CommitAttribution)
}

func commaInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		s = s[1:]
	}
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	if n < 0 {
		return "-" + b.String()
	}
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func formatTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		return ts
	}
	return t.UTC().Format("2006-01-02T15:04Z")
}

func formatUnixMs(ms string) string {
	// ms is stored as decimal string
	var v int64
	if _, err := fmt.Sscanf(ms, "%d", &v); err != nil {
		return ms
	}
	return time.UnixMilli(v).UTC().Format("2006-01-02T15:04Z")
}
