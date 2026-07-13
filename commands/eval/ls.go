package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/eval/store"
	"github.com/spf13/cobra"
)

// evalRunFile is the per-run aggregate sidecar `da eval run` persists and
// `da eval ls` reads back. The name is the R2 dashboard contract the store
// pins; ls reads it by name to list runs without re-deriving every stage.
const evalRunFile = "eval-run.yaml"

// lsRecord is the read-back projection of eval-run.yaml `da eval ls` needs. It
// is intentionally a subset of the store's PersistedEvalRun (run identity, the
// scored outcome, and the verify pass flag) so ls stays decoupled from the
// store's internal, unexported summary sub-types.
type lsRecord struct {
	RunID      string     `yaml:"run_id"`
	Language   string     `yaml:"language"`
	Difficulty string     `yaml:"difficulty"`
	Score      lsScore    `yaml:"score"`
	Verify     lsVerify   `yaml:"verify"`
	Agent      lsAgentTag `yaml:"agent"`

	// modTime is the eval-run.yaml modification time — the run's persisted-at
	// wall clock. It backs the recency ordering (most-recent first) and is
	// intentionally untagged so it is neither read from nor written to the
	// sidecar YAML.
	modTime time.Time
}

type lsScore struct {
	Value  float64 `yaml:"value" json:"value"`
	Band   string  `yaml:"band" json:"band"`
	Scored bool    `yaml:"scored" json:"scored"`
}

type lsVerify struct {
	Passed bool `yaml:"passed" json:"passed"`
}

type lsAgentTag struct {
	Harness string `yaml:"harness" json:"harness,omitempty"`
}

// newLsCmd builds `da eval ls`. The RunE handler is injected by the root (see
// package doc); this constructor owns only the command shape + flag definitions.
func newLsCmd(runE handlerFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls",
		Short:   "List persisted eval runs",
		Example: "  da eval ls\n  da eval ls --repo-dir /path/to/repo",
		Args:    cobra.NoArgs,
		RunE:    runE,
	}
	cmd.Flags().String(repoDirFlagName, "", repoDirFlagHelp)
	return cmd
}

// RunLs is the `da eval ls` entry point the root wires as the subcommand's RunE.
// asJSON is the resolved global --json flag, passed by the root handler so the
// flag read stays statically traceable in package commands.
func RunLs(cmd *cobra.Command, asJSON bool) error {
	return runLs(cmd.OutOrStdout(), resolveRepoDir(flagString(cmd, repoDirFlagName)), asJSON)
}

// evalRunsRoot returns the eval runs root (<root>/.agents/eval/runs) by deriving
// the parent of the store's canonical run dir, so the on-disk layout stays
// single-sourced in the store package.
func evalRunsRoot(root string) string {
	return filepath.Dir(store.RunDir(root, "_"))
}

// runLs enumerates the eval runs root and renders each run's summary. A missing
// runs root is the first-use state, not an error — it renders the same friendly
// "no runs" notice an empty root does.
func runLs(out io.Writer, root string, asJSON bool) error {
	runsDir := evalRunsRoot(root)
	entries, err := os.ReadDir(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return emitRuns(out, runsDir, nil, asJSON)
	}
	if err != nil {
		return fmt.Errorf("eval ls: read runs dir: %w", err)
	}
	return emitRuns(out, runsDir, collectRuns(runsDir, entries), asJSON)
}

// collectRuns reads the eval-run.yaml of every run dir under runsDir, skipping
// non-directories and dirs without a readable/parseable sidecar (an in-flight
// or leaked run), and returns the records ordered most-recent-first.
func collectRuns(runsDir string, entries []os.DirEntry) []lsRecord {
	records := make([]lsRecord, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, ok := readRunRecord(filepath.Join(runsDir, e.Name()))
		if !ok {
			continue
		}
		records = append(records, rec)
	}
	sortByRecency(records)
	return records
}

// sortByRecency orders records most-recent-first by the sidecar's modification
// time. Ties (equal mtime — e.g. two runs persisted within the same clock tick)
// fall back to a stable descending run-id order so the listing is deterministic.
func sortByRecency(records []lsRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].modTime.Equal(records[j].modTime) {
			return records[i].RunID > records[j].RunID
		}
		return records[i].modTime.After(records[j].modTime)
	})
}

// readRunRecord loads and parses a run dir's eval-run.yaml, capturing its
// modification time for recency ordering. The boolean is false when the sidecar
// is absent or malformed — ls degrades past such a run rather than failing the
// whole listing.
func readRunRecord(runDir string) (lsRecord, bool) {
	path := filepath.Join(runDir, evalRunFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return lsRecord{}, false
	}
	var rec lsRecord
	if err := yaml.Unmarshal(data, &rec); err != nil {
		return lsRecord{}, false
	}
	if info, err := os.Stat(path); err == nil {
		rec.modTime = info.ModTime()
	}
	return rec, true
}

// emitRuns dispatches to the JSON or text renderer.
func emitRuns(out io.Writer, runsDir string, records []lsRecord, asJSON bool) error {
	if asJSON {
		return emitRunsJSON(out, records)
	}
	renderRunList(out, runsDir, records)
	return nil
}

// emitRunsJSON writes the run list as an indented JSON array (never null: an
// empty listing marshals to []).
func emitRunsJSON(out io.Writer, records []lsRecord) error {
	payload := make([]lsRecordJSON, 0, len(records))
	for _, r := range records {
		payload = append(payload, lsRecordJSON{
			RunID:      r.RunID,
			Language:   r.Language,
			Difficulty: r.Difficulty,
			Score:      r.Score,
			Verify:     r.Verify,
			Agent:      r.Agent,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// lsRecordJSON is the JSON shape of one listed run (snake_case top-level keys).
type lsRecordJSON struct {
	RunID      string     `json:"run_id"`
	Language   string     `json:"language"`
	Difficulty string     `json:"difficulty"`
	Score      lsScore    `json:"score"`
	Verify     lsVerify   `json:"verify"`
	Agent      lsAgentTag `json:"agent"`
}

// renderRunList prints the run table (or the empty-state notice). The RUN
// column width is derived from the widest run id present (floored at the header
// label) so a long run id never overflows a fixed column and shears the rest of
// the row out of alignment.
func renderRunList(out io.Writer, runsDir string, records []lsRecord) {
	if len(records) == 0 {
		fmt.Fprintf(out, "eval: no runs found in %s\n", runsDir)
		return
	}
	runW := runColWidth(records)
	rowFmt := fmt.Sprintf("%%-%ds  %%-10s  %%-8s  %%-9s  %%-10s  %%s\n", runW)
	fmt.Fprintf(out, "Eval runs — %s\n\n", runsDir)
	fmt.Fprintf(out, rowFmt, "RUN", "LANG", "DIFF", "SCORE", "BAND", "VERIFY")
	for _, r := range records {
		fmt.Fprintf(out, rowFmt,
			r.RunID, r.Language, r.Difficulty, lsScoreCol(r.Score), r.Score.Band, verifyCol(r.Verify))
	}
}

// runColWidth returns the RUN column width: the widest run id, floored at the
// "RUN" header label so the header never truncates.
func runColWidth(records []lsRecord) int {
	w := len("RUN")
	for _, r := range records {
		if len(r.RunID) > w {
			w = len(r.RunID)
		}
	}
	return w
}

// lsScoreCol formats the score column, showing a dash for an unscored run.
func lsScoreCol(s lsScore) string {
	if !s.Scored {
		return "-"
	}
	return fmt.Sprintf("%.3f", s.Value)
}

// verifyCol renders the verify column as pass/fail.
func verifyCol(v lsVerify) string {
	if v.Passed {
		return "pass"
	}
	return "fail"
}
