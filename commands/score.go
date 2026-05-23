// Package commands hosts the `da` subcommands. score.go provides the
// outcome-scoring command tree: `da score run` to score every iteration in
// the active log dir and write sidecars, and `da score iteration|session` to
// render a persisted score back. The scoring rubric and persistence live in
// internal/scoring; this file is presentation and CLI wiring only.
package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"go.yaml.in/yaml/v3"

	"github.com/NikashPrakash/dot-agents/internal/scoring"
	"github.com/spf13/cobra"
)

// defaultIterLogDir is the in-repo iter-log root the CLI assumes when --iter-log-dir
// is not passed. The path is intentionally repo-relative so commands invoked
// from any subdirectory still resolve the right log directory.
const defaultIterLogDir = ".agents/active/iteration-log"

// iterLogDirFlagName / iterLogDirFlagHelp are shared by every score subcommand
// that takes an --iter-log-dir override (run / iteration / session). Pulled
// to constants so the flag surface stays identical across subcommands and a
// single edit reaches all three.
const (
	iterLogDirFlagName = "iter-log-dir"
	iterLogDirFlagHelp = "Iteration-log directory (default: .agents/active/iteration-log)"
)

type scoreRunOpts struct {
	iterLogDir     string
	repoDir        string
	transcriptDirs []string
	noWrite        bool
}

// NewScoreCmd builds the `da score` command group.
func NewScoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "score",
		Short: "Compute and query agent-run outcome scores",
		Long: "Scores every iteration of an agent run against the versioned outcome-scoring rubric\n" +
			"(docs/OUTCOME_SCORING_RUBRIC.md) and renders the result.\n\n" +
			"`score run` recomputes per-iteration and per-session scores from the active iteration\n" +
			"log and writes them as sidecars (iter-N.score.yaml, session-<id>.score.yaml).\n" +
			"`score iteration <N>` and `score session <id>` render a persisted sidecar.",
	}
	cmd.AddCommand(newScoreRunCmd())
	cmd.AddCommand(newScoreIterationCmd())
	cmd.AddCommand(newScoreSessionCmd())
	return cmd
}

func newScoreRunCmd() *cobra.Command {
	var opts scoreRunOpts
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Score every iteration in the active log and write score sidecars",
		Example: "  da score run\n" +
			"  da score run --iter-log-dir .agents/active/iteration-log\n" +
			"  da score run --transcript-dir ~/.claude/projects --transcript-dir ~/.codex/sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScoreRun(cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.iterLogDir, iterLogDirFlagName, "", iterLogDirFlagHelp)
	cmd.Flags().StringVar(&opts.repoDir, "repo-dir", "", "Repository root (default: current working directory)")
	cmd.Flags().StringSliceVar(&opts.transcriptDirs, "transcript-dir", nil, "Agent transcript root for token backfill (repeatable)")
	cmd.Flags().BoolVar(&opts.noWrite, "no-write", false, "Render the summary without writing sidecars")
	return cmd
}

func newScoreIterationCmd() *cobra.Command {
	var iterLogDir string
	cmd := &cobra.Command{
		Use:   "iteration <N>",
		Short: "Render a persisted per-iteration score",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			iter, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("iteration must be an integer: %w", err)
			}
			return runScoreIteration(cmd.OutOrStdout(), resolveIterLogDir(iterLogDir), iter)
		},
	}
	cmd.Flags().StringVar(&iterLogDir, iterLogDirFlagName, "", iterLogDirFlagHelp)
	return cmd
}

func newScoreSessionCmd() *cobra.Command {
	var iterLogDir string
	cmd := &cobra.Command{
		Use:   "session <session-id>",
		Short: "Render a persisted per-session score",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScoreSession(cmd.OutOrStdout(), resolveIterLogDir(iterLogDir), args[0])
		},
	}
	cmd.Flags().StringVar(&iterLogDir, iterLogDirFlagName, "", iterLogDirFlagHelp)
	return cmd
}

func resolveIterLogDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return defaultIterLogDir
}

// runScoreRun loads the iteration log, runs every extractor + the scorer, and
// (unless --no-write) persists per-iteration + per-session sidecars. The
// summary printed to out is intentionally compact — `da score iteration` is
// the drill-down for the full breakdown.
func runScoreRun(out io.Writer, opts scoreRunOpts) error {
	iterLogDir := resolveIterLogDir(opts.iterLogDir)
	repoDir := opts.repoDir
	if repoDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("score run: resolve cwd: %w", err)
		}
		repoDir = cwd
	}

	rubric := scoring.DefaultRubric()
	records, err := scoring.LoadIterationLog(iterLogDir)
	if err != nil {
		return fmt.Errorf("score run: load iteration log: %w", err)
	}
	if len(records) == 0 {
		fmt.Fprintf(out, "score: no iterations found in %s\n", iterLogDir)
		return nil
	}

	sets, err := scoring.BuildSignalSets(iterLogDir, repoDir, opts.transcriptDirs...)
	if err != nil {
		return fmt.Errorf("score run: build signals: %w", err)
	}
	scores := rubric.ScoreAll(sets)
	sessions := scoring.AggregateSessions(rubric, records, scores)

	if Flags.JSON {
		return emitScoreRunJSON(out, rubric, records, scores, sessions)
	}

	if !opts.noWrite {
		for i, s := range scores {
			if _, err := scoring.WriteIterationScoreWithRecord(iterLogDir, s, records[i]); err != nil {
				return fmt.Errorf("score run: persist iter-%d: %w", s.Iteration, err)
			}
		}
		if _, err := scoring.WriteSessionScores(iterLogDir, sessions); err != nil {
			return fmt.Errorf("score run: persist sessions: %w", err)
		}
	}

	renderRunSummary(out, rubric, records, scores, sessions, !opts.noWrite, iterLogDir)
	return nil
}

func emitScoreRunJSON(out io.Writer, rubric scoring.Rubric, records []scoring.IterationRecord, scores []scoring.Score, sessions []scoring.SessionScore) error {
	perIter := make([]scoring.PersistedScore, len(scores))
	for i, s := range scores {
		perIter[i] = scoring.BuildPersistedScore(s, records[i])
	}
	payload := struct {
		RubricVersion string                   `json:"rubric_version"`
		Iterations    []scoring.PersistedScore `json:"iterations"`
		Sessions      []scoring.SessionScore   `json:"sessions"`
	}{
		RubricVersion: rubric.Version,
		Iterations:    perIter,
		Sessions:      sessions,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func renderRunSummary(out io.Writer, rubric scoring.Rubric, records []scoring.IterationRecord, scores []scoring.Score, sessions []scoring.SessionScore, wrote bool, iterLogDir string) {
	fmt.Fprintf(out, "Outcome scoring — rubric %s\n", rubric.Version)
	fmt.Fprintf(out, "Iterations: %d   Sessions: %d   Source: %s\n\n", len(records), len(sessions), iterLogDir)
	fmt.Fprintf(out, "%-6s  %-12s  %-9s  %-26s  %s\n", "ITER", "TASK", "SCORE", "BAND", "DATE")
	for i, s := range scores {
		rec := records[i]
		scoreCol := "-"
		if s.Scored {
			scoreCol = fmt.Sprintf("%.3f", s.Value)
		}
		fmt.Fprintf(out, "%-6d  %-12s  %-9s  %-26s  %s\n",
			s.Iteration, truncStr(rec.TaskID, 12), scoreCol, s.Band, rec.Date)
	}

	if len(sessions) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%-40s  %-9s  %-11s  %s\n", "SESSION", "SCORE", "ITERS", "BAND")
		for _, ss := range sessions {
			scoreCol := "-"
			if ss.Scored {
				scoreCol = fmt.Sprintf("%.3f", ss.Value)
			}
			fmt.Fprintf(out, "%-40s  %-9s  %-11d  %s\n",
				truncStr(ss.SessionID, 40), scoreCol, len(ss.Iterations), ss.Band)
		}
	}
	if wrote {
		fmt.Fprintf(out, "\nWrote %d iter sidecars + %d session sidecars to %s\n", len(scores), len(sessions), iterLogDir)
	}
}

// runScoreIteration reads the iter-N.score.yaml sidecar and renders it. A
// missing sidecar is the most common failure mode — point the user at the
// fix (`da score run`) rather than a stack trace.
func runScoreIteration(out io.Writer, iterLogDir string, iter int) error {
	path := scoring.IterationScorePath(iterLogDir, iter)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no score sidecar for iteration %d at %s — run `da score run` first", iter, path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if Flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(ps)
	}
	renderIterationScore(out, ps, path)
	return nil
}

func renderIterationScore(out io.Writer, ps scoring.PersistedScore, source string) {
	scoreCol := "-"
	if ps.Scored {
		scoreCol = fmt.Sprintf("%.3f", ps.Value)
	}
	fmt.Fprintf(out, "Iteration %d   rubric %s   score %s   band %s\n",
		ps.Iteration, ps.RubricVersion, scoreCol, ps.Band)
	if ps.LinkedTracesToOutcomes {
		fmt.Fprintln(out, "linked_traces_to_outcomes: yes (derived)")
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-22s  %-7s  %-8s  %-8s  %-8s  %s\n",
		"SIGNAL", "PRESENT", "SUBSCORE", "WEIGHT", "CONTRIB", "DETAIL")
	for _, row := range ps.Breakdown {
		present := "no"
		sub := "-"
		weight := fmt.Sprintf("%.3f", row.NominalWeight)
		contrib := "-"
		if row.Present {
			present = "yes"
			sub = fmt.Sprintf("%.3f", row.SubScore)
			weight = fmt.Sprintf("%.3f", row.EffectiveWeight)
			contrib = fmt.Sprintf("%.3f", row.Contribution)
		}
		fmt.Fprintf(out, "%-22s  %-7s  %-8s  %-8s  %-8s  %s\n",
			string(row.Signal), present, sub, weight, contrib, truncStr(row.Detail, 60))
	}
	fmt.Fprintf(out, "\nSource: %s\n", source)
}

func runScoreSession(out io.Writer, iterLogDir, sessionID string) error {
	path, err := scoring.SessionScorePath(iterLogDir, sessionID)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no session sidecar for %s at %s — run `da score run` first", sessionID, path)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	var ss scoring.SessionScore
	if err := yaml.Unmarshal(data, &ss); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if Flags.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(ss)
	}
	renderSessionScore(out, ss, path)
	return nil
}

func renderSessionScore(out io.Writer, ss scoring.SessionScore, source string) {
	scoreCol := "-"
	if ss.Scored {
		scoreCol = fmt.Sprintf("%.3f", ss.Value)
	}
	fmt.Fprintf(out, "Session %s   rubric %s   score %s   band %s\n",
		ss.SessionID, ss.RubricVersion, scoreCol, ss.Band)
	fmt.Fprintf(out, "Iterations: %d\n\n", len(ss.PerIteration))
	fmt.Fprintf(out, "%-6s  %-9s  %s\n", "ITER", "SCORE", "BAND")
	for _, r := range ss.PerIteration {
		v := "-"
		if r.Scored {
			v = fmt.Sprintf("%.3f", r.Value)
		}
		fmt.Fprintf(out, "%-6d  %-9s  %s\n", r.Iteration, v, r.Band)
	}
	fmt.Fprintf(out, "\nSource: %s\n", source)
}

// truncStr crops s to width, appending an ellipsis when it had to truncate.
// Padding is the caller's job (fmt verbs handle it).
func truncStr(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	if width <= 3 {
		return s[:width]
	}
	return s[:width-3] + "..."
}
