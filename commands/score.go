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
	"path/filepath"
	"sort"
	"strconv"

	"go.yaml.in/yaml/v3"

	wf "github.com/NikashPrakash/dot-agents/commands/workflow"
	"github.com/NikashPrakash/dot-agents/internal/scoring"
	"github.com/spf13/cobra"
)

// scoredHookInterventionClasses are the only intervention_class values that
// contribute to the v1 `hook_outcomes` sub-score (R1.5 design D3 / D6). The
// CLI readback honours the same filter as
// `internal/scoring/signal_hook_outcomes.go`'s extractor so the rendered
// rule-id / sentinel-id sources cannot suggest a record voted when it did
// not. Adding a future class to the score is a deliberate edit there AND
// here.
var scoredHookInterventionClasses = map[string]bool{
	"prevent_before_action": true,
	"remediate_at_stop":     true,
}

// hookOutcomeSource is the readback projection of one scored hook-outcome
// sidecar record: enough to attribute the contribution row to its source
// (sentinel + rule), and to show which lifecycle point + result drove it.
// Transcript content is excluded by construction — only the fields named
// here are surfaced (R1.5 spec D2 / docs/OUTCOME_SCORING_RUBRIC.md "expose
// outcome source and rule identifiers without printing transcript contents").
type hookOutcomeSource struct {
	SentinelID        string `json:"sentinel_id" yaml:"sentinel_id"`
	RuleID            string `json:"rule_id" yaml:"rule_id"`
	Result            string `json:"result" yaml:"result"`
	LifecyclePoint    string `json:"lifecycle_point" yaml:"lifecycle_point"`
	InterventionClass string `json:"intervention_class" yaml:"intervention_class"`
	CorrelationID     string `json:"correlation_id,omitempty" yaml:"correlation_id,omitempty"`
}

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
	var (
		iterLogDir     string
		recompute      bool
		repoDir        string
		transcriptDirs []string
	)
	cmd := &cobra.Command{
		Use:   "iteration <N>",
		Short: "Render a persisted per-iteration score (or recompute it via --recompute)",
		Long: "Default behavior renders the persisted iter-N.score.yaml sidecar — fast, no\n" +
			"git work, no transcript scan. Pass --recompute to score iteration N fresh\n" +
			"from the canonical iter-N.yaml + git topology + transcripts, write the new\n" +
			"sidecar, and render it. close-task uses --recompute for its default\n" +
			"score-recompute=current flow so only the just-closed iteration's sidecar\n" +
			"is rewritten — older sidecars are computed from immutable inputs and stay\n" +
			"valid until a RubricVersion bump.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			iter, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("iteration must be an integer: %w", err)
			}
			dir := resolveIterLogDir(iterLogDir)
			if recompute {
				return runScoreIterationRecompute(cmd.OutOrStdout(), dir, repoDir, iter, transcriptDirs)
			}
			return runScoreIteration(cmd.OutOrStdout(), dir, iter)
		},
	}
	cmd.Flags().StringVar(&iterLogDir, iterLogDirFlagName, "", iterLogDirFlagHelp)
	cmd.Flags().BoolVar(&recompute, "recompute", false, "Recompute the score from canonical inputs and rewrite the iter-N.score.yaml sidecar")
	cmd.Flags().StringVar(&repoDir, "repo-dir", "", "Repository root for git topology (default: current working directory; used only with --recompute)")
	cmd.Flags().StringSliceVar(&transcriptDirs, "transcript-dir", nil, "Agent transcript root for token backfill (repeatable; used only with --recompute)")
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
		// os.Getwd is effectively infallible on a working process — the
		// canonical guidance is to let downstream surface a more useful
		// error if the result is somehow empty (BuildSignalSets reports
		// non-git, etc.). Treating the unreachable error branch as
		// untestable noise was bloating the allowlist.
		cwd, _ := os.Getwd()
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

// runScoreIterationRecompute scores iteration N fresh from the canonical
// inputs, writes the iter-N.score.yaml sidecar, and renders the same view
// runScoreIteration produces. close-task --score-recompute=current is the
// primary caller; operators can also run it ad-hoc after a rubric bump.
//
// repoDir defaults to the cwd when empty (matching runScoreRun's contract)
// so headless invocations from the repo root do not need to set the flag.
func runScoreIterationRecompute(out io.Writer, iterLogDir, repoDir string, iter int, transcriptDirs []string) error {
	if repoDir == "" {
		// See runScoreRun above for why we treat Getwd as infallible
		// rather than guarding an unreachable error branch.
		cwd, _ := os.Getwd()
		repoDir = cwd
	}
	score, rec, err := scoring.ScoreIteration(iterLogDir, repoDir, iter, transcriptDirs...)
	if err != nil {
		return fmt.Errorf("score iteration: %w", err)
	}
	path, err := scoring.WriteIterationScoreWithRecord(iterLogDir, score, rec)
	if err != nil {
		return fmt.Errorf("score iteration: persist sidecar: %w", err)
	}
	ps := scoring.BuildPersistedScore(score, rec)
	if Flags.JSON {
		return emitIterationScoreJSON(out, ps, iterLogDir)
	}
	renderIterationScoreWithHooks(out, ps, path, iterLogDir)
	return nil
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
		return emitIterationScoreJSON(out, ps, iterLogDir)
	}
	renderIterationScoreWithHooks(out, ps, path, iterLogDir)
	return nil
}

// iterationScoreJSON is the `da score iteration` JSON envelope: the
// PersistedScore inline (so existing consumers keep the same top-level
// fields) plus an optional `hook_outcome_sources` array surfacing the
// scored-record attributions for the hook_outcomes signal. The array is
// omitted when the signal did not vote or no sidecar exists — matching
// the text renderer's gate (hookRowPresent + non-empty sources).
type iterationScoreJSON struct {
	scoring.PersistedScore
	HookOutcomeSources []hookOutcomeSource `json:"hook_outcome_sources,omitempty"`
}

// emitIterationScoreJSON renders ps as JSON with the hook-outcome attribution
// list attached when applicable. Splitting it out of the run paths keeps both
// callers (sidecar-read + recompute) emitting the same shape so a downstream
// consumer does not have to special-case which subcommand produced the file.
func emitIterationScoreJSON(out io.Writer, ps scoring.PersistedScore, iterLogDir string) error {
	payload := iterationScoreJSON{PersistedScore: ps}
	if hookRowPresent(ps) && iterLogDir != "" {
		payload.HookOutcomeSources = loadHookOutcomeSources(iterLogDir, ps.Iteration)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func renderIterationScore(out io.Writer, ps scoring.PersistedScore, source string) {
	renderIterationScoreWithHooks(out, ps, source, "")
}

// renderIterationScoreWithHooks is the iteration renderer with an explicit
// hook-outcome sidecar directory. When iterLogDir is non-empty and the
// hook_outcomes signal contributed to ps, the renderer appends a
// "Hook outcome sources:" block listing the scored records' sentinel_id and
// rule_id (and lifecycle_point / result / intervention_class) so an operator
// can attribute the sub-score to a concrete gate firing. Missing or
// unreadable sidecars degrade silently — the breakdown table is the
// authoritative score view; the source list is augmentation.
func renderIterationScoreWithHooks(out io.Writer, ps scoring.PersistedScore, source, iterLogDir string) {
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
	if hookRowPresent(ps) && iterLogDir != "" {
		if sources := loadHookOutcomeSources(iterLogDir, ps.Iteration); len(sources) > 0 {
			renderHookOutcomeSources(out, sources)
		}
	}
	fmt.Fprintf(out, "\nSource: %s\n", source)
}

// hookRowPresent reports whether the persisted score has a present
// hook_outcomes breakdown row — the gate that decides whether to read and
// render the sidecar source list. A row that is absent (sidecar missing,
// no scored records) does not vote and has no sources to surface.
func hookRowPresent(ps scoring.PersistedScore) bool {
	for _, row := range ps.Breakdown {
		if row.Signal == scoring.SignalHookOutcomes && row.Present {
			return true
		}
	}
	return false
}

// loadHookOutcomeSources reads iter-N.hook-outcomes.yaml and projects the
// scored records to the [hookOutcomeSource] readback shape. The filter
// matches `internal/scoring/signal_hook_outcomes.go`'s
// `filterScoredHookOutcomes` (only `prevent_before_action` and
// `remediate_at_stop` vote in v1) so the rendered list and the sub-score
// row agree on what was scored. A missing or malformed sidecar returns nil
// — readback is best-effort augmentation, not a hard contract.
//
// Output is sorted by (rule_id, sentinel_id) for deterministic rendering
// and stable test diffs; the on-disk record order is append-time and not
// meaningful for explainability.
func loadHookOutcomeSources(iterLogDir string, iter int) []hookOutcomeSource {
	path := filepath.Join(iterLogDir, fmt.Sprintf("iter-%d.hook-outcomes.yaml", iter))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var sc wf.HookOutcomeSidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil
	}
	out := make([]hookOutcomeSource, 0, len(sc.Records))
	for _, r := range sc.Records {
		if !scoredHookInterventionClasses[r.InterventionClass] {
			continue
		}
		out = append(out, hookOutcomeSource{
			SentinelID:        r.SentinelID,
			RuleID:            r.RuleID,
			Result:            r.Result,
			LifecyclePoint:    r.LifecyclePoint,
			InterventionClass: r.InterventionClass,
			CorrelationID:     r.CorrelationID,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		return out[i].SentinelID < out[j].SentinelID
	})
	return out
}

// renderHookOutcomeSources prints the per-record attribution block under the
// breakdown table. The columns are deliberately minimal — sentinel_id and
// rule_id are the readback contract (R1.5 spec D2 + OUTCOME_SCORING_RUBRIC.md);
// lifecycle_point + result are the immediate "why this row scored what it
// scored" context. No transcript content is loaded or printed.
func renderHookOutcomeSources(out io.Writer, sources []hookOutcomeSource) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Hook outcome sources:")
	fmt.Fprintf(out, "  %-40s  %-32s  %-22s  %-10s  %s\n",
		"RULE_ID", "SENTINEL_ID", "LIFECYCLE", "RESULT", "INTERVENTION")
	for _, s := range sources {
		fmt.Fprintf(out, "  %-40s  %-32s  %-22s  %-10s  %s\n",
			truncStr(s.RuleID, 40),
			truncStr(s.SentinelID, 32),
			truncStr(s.LifecyclePoint, 22),
			truncStr(s.Result, 10),
			s.InterventionClass)
	}
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
