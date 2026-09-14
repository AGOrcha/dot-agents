package crgbehavior

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Surface is one comparison for one task: which behavior was compared, what the
// exact oracle said, and — on divergence — the structural diff a decommission
// decision is made on.
type Surface struct {
	// Name is the compared behavior (changed_nodes, flows, risk_index, ...).
	Name string `json:"surface"`
	// Status is the oracle outcome.
	Status SurfaceStatus `json:"status"`
	// Metric is the headline number (set sizes, matched surfaces).
	Metric string `json:"metric,omitempty"`
	// Reason explains a not-exercised, failed, or unimplemented surface.
	Reason string `json:"reason,omitempty"`
	// Detail is the capped structural diff on divergence.
	Detail []string `json:"detail,omitempty"`
}

// Divergent reports whether this surface counts against the gate.
func (s Surface) Divergent() bool {
	return s.Status == StatusDiverge || s.Status == StatusFailed
}

// TaskReport is one pinned review task's outcome, materialized at its own SHA.
type TaskReport struct {
	Commit       string   `json:"commit"`
	Subject      string   `json:"subject"`
	ChangedFiles []string `json:"changed_files"`
	Identifiers  []string `json:"identifiers,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	// Release and Schema are the bridge facts observed at THIS commit.
	Release ReleaseInfo  `json:"release"`
	Schema  SchemaReport `json:"schema"`
	// Graph sizes describe the state both sides were compared over.
	GraphSymbols  int `json:"graph_symbols"`
	GraphEdges    int `json:"graph_edges"`
	GraphFiles    int `json:"graph_files"`
	NativeSymbols int `json:"native_symbols"`
	// Failure records a task whose materialization or comparison could not run.
	Failure  string    `json:"failure,omitempty"`
	Surfaces []Surface `json:"surfaces,omitempty"`
}

// Verdict is a gate run's overall answer.
type Verdict string

const (
	// VerdictPass — every executed comparison agreed and the corpus contract
	// was satisfied.
	VerdictPass Verdict = "PASS"
	// VerdictFail — at least one behavior diverged, one comparison failed, or
	// a required surface went unexercised without a ratified exception.
	VerdictFail Verdict = "FAIL"
	// VerdictInconclusive — the run produced no comparison at all. It is NOT a
	// pass: an absent bridge, an empty corpus or an aborted run is the absence
	// of evidence, and callers exit non-zero on it.
	VerdictInconclusive Verdict = "INCONCLUSIVE"
)

// Report is a full gate run: which repository, which release, which corpus, and
// every task's per-surface verdict.
type Report struct {
	RepoRoot string `json:"repo_root"`
	// Head is the corpus manifest's generation head.
	Head string `json:"corpus_head"`
	// GeneratedFrom is the ref the corpus window was taken from.
	GeneratedFrom string `json:"corpus_generated_from"`
	// PinnedVersion and PinnedSchemaVersion are the release this gate
	// certifies against; Release is what the run actually drove. Both are
	// persisted so a run's evidence names its own baseline.
	PinnedVersion       string      `json:"pinned_version"`
	PinnedSchemaVersion int         `json:"pinned_schema_version"`
	Release             ReleaseInfo `json:"observed_release"`
	// CorpusTasks is the manifest size; len(Tasks) is what this run executed.
	CorpusTasks int               `json:"corpus_tasks"`
	Tasks       []TaskReport      `json:"tasks"`
	Coverage    []SurfaceCoverage `json:"required_surface_coverage"`
}

// Verdict is the gate's sign-off answer.
func (r Report) Verdict() Verdict {
	if len(r.Tasks) == 0 {
		return VerdictInconclusive
	}
	for _, t := range r.Tasks {
		if t.Failure != "" {
			return VerdictFail
		}
		for _, s := range t.Surfaces {
			if s.Divergent() {
				return VerdictFail
			}
		}
	}
	for _, c := range r.Coverage {
		if !c.Satisfied {
			return VerdictFail
		}
	}
	return VerdictPass
}

// Pass reports whether the run is a criterion-2 sign-off.
func (r Report) Pass() bool { return r.Verdict() == VerdictPass }

// DivergentSurfaces counts divergent surfaces by name.
func (r Report) DivergentSurfaces() map[string]int {
	out := map[string]int{}
	for _, t := range r.Tasks {
		for _, s := range t.Surfaces {
			if s.Divergent() {
				out[s.Name]++
			}
		}
	}
	return out
}

// UnsatisfiedSurfaces names the required surfaces this run never exercised and
// which carry no ratified exception.
func (r Report) UnsatisfiedSurfaces() []string {
	var out []string
	for _, c := range r.Coverage {
		if !c.Satisfied {
			out = append(out, c.Surface)
		}
	}
	sort.Strings(out)
	return out
}

// SignOff is the one-line claim this run supports. It is the sentence a
// reviewer quotes in the §11.4 decision, so it states what was compared, at
// which release, and over how many tasks — never just "green".
func (r Report) SignOff() string {
	switch r.Verdict() {
	case VerdictPass:
		return fmt.Sprintf("criterion 2 SATISFIED: %d review task(s) replayed at their own commits against %s; "+
			"every required surface exercised and every exact oracle agreed",
			len(r.Tasks), r.Release)
	case VerdictInconclusive:
		return "criterion 2 NOT ESTABLISHED: the run produced no comparison — absence of evidence, not preserved behavior"
	default:
		return fmt.Sprintf("criterion 2 NOT SATISFIED against %s: %s",
			r.Release, strings.Join(r.failureSummary(), "; "))
	}
}

// failureSummary lists the reasons the run is not a sign-off.
func (r Report) failureSummary() []string {
	var out []string
	if names := sortedKeys(r.DivergentSurfaces()); len(names) > 0 {
		out = append(out, "divergent surface(s): "+strings.Join(names, ", "))
	}
	if names := r.UnsatisfiedSurfaces(); len(names) > 0 {
		out = append(out, "required surface(s) never exercised: "+strings.Join(names, ", "))
	}
	var failed []string
	for _, t := range r.Tasks {
		if t.Failure != "" {
			failed = append(failed, short(t.Commit))
		}
	}
	if len(failed) > 0 {
		out = append(out, "task(s) that could not run: "+strings.Join(failed, ", "))
	}
	return out
}

// Render writes the human-readable gate report.
func (r Report) Render(w io.Writer) {
	r.renderHeader(w)
	for _, t := range r.Tasks {
		renderTask(w, t)
	}
	r.renderCoverage(w)
	r.renderSummary(w)
}

// renderHeader states the release the evidence was produced against, up front.
func (r Report) renderHeader(w io.Writer) {
	fmt.Fprintln(w, "CRG behavior-preservation gate (graph-backend-adapter-contract §11.4 criterion 2)")
	fmt.Fprintf(w, "repo:     %s\n", r.RepoRoot)
	fmt.Fprintf(w, "pinned:   %s %s (graph schema v%d)\n", PackageName, r.PinnedVersion, r.PinnedSchemaVersion)
	fmt.Fprintf(w, "observed: %s\n", r.Release)
	fmt.Fprintf(w, "corpus:   %d of %d pinned review task(s) from %s at %s\n",
		len(r.Tasks), r.CorpusTasks, r.GeneratedFrom, short(r.Head))
	fmt.Fprintln(w, "model:    each task is replayed against a graph built at ITS OWN commit; every oracle is exact")
	fmt.Fprintln(w)
}

// renderTask prints one task's verdicts.
func renderTask(w io.Writer, t TaskReport) {
	fmt.Fprintf(w, "commit %s  %s\n", short(t.Commit), t.Subject)
	if t.Failure != "" {
		fmt.Fprintf(w, "  ERROR %s\n\n", t.Failure)
		return
	}
	fmt.Fprintf(w, "  files:  %s\n", strings.Join(t.ChangedFiles, ", "))
	if len(t.Languages) > 0 {
		fmt.Fprintf(w, "  langs:  %s\n", strings.Join(t.Languages, ", "))
	}
	fmt.Fprintf(w, "  graph:  %d symbols / %d edges / %d files; %d symbols ingested natively\n",
		t.GraphSymbols, t.GraphEdges, t.GraphFiles, t.NativeSymbols)
	for _, s := range t.Surfaces {
		renderSurface(w, s)
	}
	fmt.Fprintln(w)
}

// renderSurface prints one comparison.
func renderSurface(w io.Writer, s Surface) {
	fmt.Fprintf(w, "  %-6s %-21s %s\n", verdictWord(s.Status), s.Name, s.Metric)
	if s.Reason != "" {
		fmt.Fprintf(w, "         %s\n", s.Reason)
	}
	for _, d := range s.Detail {
		fmt.Fprintf(w, "         %s\n", d)
	}
}

// verdictWord is the per-surface status word.
func verdictWord(status SurfaceStatus) string {
	switch status {
	case StatusAgree:
		return "AGREE"
	case StatusDiverge:
		return "DIFFER"
	case StatusFailed:
		return "ERROR"
	default:
		return "NOTRUN"
	}
}

// renderCoverage prints the corpus contract's verdict per required surface.
func (r Report) renderCoverage(w io.Writer) {
	if len(r.Coverage) == 0 {
		return
	}
	fmt.Fprintln(w, "required surface coverage (corpus contract)")
	for _, c := range r.Coverage {
		fmt.Fprintf(w, "  %-8s %-21s exercised on %d task(s), divergent on %d\n",
			coverageWord(c), c.Surface, c.Exercised, c.Diverged)
		if c.Ratified != nil {
			fmt.Fprintf(w, "           ratified by %s: %s\n", c.Ratified.RatifiedBy, c.Ratified.Reason)
		}
		for _, reason := range c.Reasons {
			if c.Exercised == 0 && reason != "" {
				fmt.Fprintf(w, "           not exercised: %s\n", reason)
			}
		}
	}
	fmt.Fprintln(w)
}

// coverageWord labels a required surface's contract outcome.
func coverageWord(c SurfaceCoverage) string {
	switch {
	case !c.Satisfied:
		return "MISSING"
	case c.Ratified != nil:
		return "WAIVED"
	default:
		return "COVERED"
	}
}

// renderSummary prints the run's sign-off claim.
func (r Report) renderSummary(w io.Writer) {
	fmt.Fprintf(w, "%d of %d corpus task(s) executed\n", len(r.Tasks), r.CorpusTasks)
	divergent := r.DivergentSurfaces()
	for _, name := range sortedKeys(divergent) {
		fmt.Fprintf(w, "  divergent surface %-21s on %d task(s)\n", name, divergent[name])
	}
	fmt.Fprintf(w, "GATE: %s\n", r.Verdict())
	fmt.Fprintf(w, "SIGN-OFF: %s\n", r.SignOff())
}

// WriteJSON persists the machine-readable run artifact.
func (r Report) WriteJSON(path string) error {
	return writeJSON(path, r)
}

// sortedKeys returns map keys in deterministic order.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
