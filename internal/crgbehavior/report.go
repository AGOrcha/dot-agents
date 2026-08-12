package crgbehavior

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Surface is one review-relevant query comparison for one task: which oracle
// ran, whether it agreed, and — on divergence — the structural diff a
// decommission decision is made on.
type Surface struct {
	// Name is the query surface (changed_nodes, impact_radius, flows, ...).
	Name string
	// Advisory reports that this surface does not fail the gate today; the
	// measured reason is in AdvisoryReason.
	Advisory bool
	// AdvisoryReason states why the surface cannot be strict yet.
	AdvisoryReason string
	// Pass is the oracle verdict.
	Pass bool
	// Metric is the headline number (set sizes, agreement, rank correlation).
	Metric string
	// Detail is the capped structural diff on divergence.
	Detail []string
	// Skipped reports that this task cannot exercise the surface.
	Skipped    bool
	SkipReason string
}

// TaskReport is one pinned review task's outcome.
type TaskReport struct {
	Commit       string
	Subject      string
	ChangedFiles []string
	Skipped      bool
	SkipReason   string
	Surfaces     []Surface
}

// Report is a full gate run: which repository, which graph, and every task's
// per-surface verdict.
type Report struct {
	RepoRoot      string
	Head          string
	Strict        bool
	GraphSymbols  int
	GraphEdges    int
	GraphFiles    int
	NativeSymbols int
	Tasks         []TaskReport
}

// Pass is the gate verdict: at least one task actually ran, and every gating
// surface agreed on every executed task. In Strict mode (the §11.4 sign-off
// flip) the advisory surfaces gate too.
func (r Report) Pass() bool {
	executed := 0
	for _, t := range r.Tasks {
		if t.Skipped {
			continue
		}
		executed++
		for _, s := range t.Surfaces {
			if s.Skipped || s.Pass {
				continue
			}
			if !s.Advisory || r.Strict {
				return false
			}
		}
	}
	return executed > 0
}

// ExecutedTasks counts the tasks whose queries actually ran.
func (r Report) ExecutedTasks() int {
	n := 0
	for _, t := range r.Tasks {
		if !t.Skipped {
			n++
		}
	}
	return n
}

// FailingSurfaces counts divergent surfaces by name, split by gating/advisory.
func (r Report) FailingSurfaces() map[string]int {
	out := map[string]int{}
	for _, t := range r.Tasks {
		for _, s := range t.Surfaces {
			if !s.Skipped && !s.Pass {
				out[s.Name]++
			}
		}
	}
	return out
}

// Render writes the human-readable gate report. On divergence it names WHICH
// commit, WHICH query surface, and the structural diff — the decommission
// decision is made on this output.
func (r Report) Render(w io.Writer) {
	r.renderHeader(w)
	for _, t := range r.Tasks {
		renderTask(w, t)
	}
	r.renderSummary(w)
}

func (r Report) renderHeader(w io.Writer) {
	fmt.Fprintln(w, "CRG behavior-preservation gate (graph-backend-adapter-contract §11.4 criterion 2)")
	fmt.Fprintf(w, "repo:   %s\n", r.RepoRoot)
	fmt.Fprintf(w, "corpus: %d pinned review task(s) at %s\n", len(r.Tasks), short(r.Head))
	fmt.Fprintf(w, "graph:  %d bridge symbols / %d references / %d files; %d symbols ingested natively\n",
		r.GraphSymbols, r.GraphEdges, r.GraphFiles, r.NativeSymbols)
	fmt.Fprintf(w, "mode:   %s\n\n", modeLabel(r.Strict))
}

// modeLabel names the gating mode in the report header.
func modeLabel(strict bool) string {
	if strict {
		return "STRICT (advisory surfaces gate too — §11.4 sign-off)"
	}
	return "advisory surfaces reported but not gating"
}

func renderTask(w io.Writer, t TaskReport) {
	fmt.Fprintf(w, "commit %s  %s\n", short(t.Commit), t.Subject)
	if t.Skipped {
		fmt.Fprintf(w, "  SKIP  %s\n\n", t.SkipReason)
		return
	}
	fmt.Fprintf(w, "  files: %s\n", strings.Join(t.ChangedFiles, ", "))
	for _, s := range t.Surfaces {
		renderSurface(w, s)
	}
	fmt.Fprintln(w)
}

func renderSurface(w io.Writer, s Surface) {
	if s.Skipped {
		fmt.Fprintf(w, "  skip  %-14s %s\n", s.Name, s.SkipReason)
		return
	}
	fmt.Fprintf(w, "  %-5s %-14s %s\n", verdict(s), s.Name, s.Metric)
	if s.Pass {
		return
	}
	if s.Advisory {
		fmt.Fprintf(w, "        advisory: %s\n", s.AdvisoryReason)
	}
	for _, d := range s.Detail {
		fmt.Fprintf(w, "        %s\n", d)
	}
}

// verdict is the per-surface status word: an advisory divergence is WARN, a
// gating divergence is FAIL.
func verdict(s Surface) string {
	switch {
	case s.Pass:
		return "PASS"
	case s.Advisory:
		return "WARN"
	default:
		return "FAIL"
	}
}

func (r Report) renderSummary(w io.Writer) {
	fmt.Fprintf(w, "%d of %d task(s) executed\n", r.ExecutedTasks(), len(r.Tasks))
	failing := r.FailingSurfaces()
	for _, name := range sortedKeys(failing) {
		fmt.Fprintf(w, "  divergent surface %-14s on %d task(s)%s\n", name, failing[name], advisorySuffix(name))
	}
	fmt.Fprintf(w, "GATE: %s\n", passLabel(r.Pass()))
}

// advisorySuffix marks a divergent surface that is not gating today.
func advisorySuffix(name string) string {
	if _, advisory := advisoryReasons[name]; advisory {
		return " (advisory)"
	}
	return ""
}

// passLabel renders the overall verdict.
func passLabel(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
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
