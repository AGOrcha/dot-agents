// Command crgbehaviorgate runs the CRG behavior-preservation gate
// (graph-backend-adapter-contract §11.4 criterion 2) over a corpus of REAL
// review tasks pinned from this repository's history, and regenerates that
// corpus on request.
//
// The hermetic §11.6 parity gate compares the kg-native CRG adapter against the
// crg-bridge mirror on a synthetic 10-commit corpus. This gate replays the same
// comparison discipline — the same structural oracles — against the LIVE legacy
// Python bridge for real commits: for each pinned commit it issues the queries
// a review of that commit issues (changed-file impact radius, flows touched,
// community membership of the changed symbols, FTS over the changed
// identifiers) on both sides and diffs the answers.
//
// Usage:
//
//	go run ./tools/crgbehaviorgate [-repo DIR] [-graph-repo DIR]
//	    [-manifest PATH] [-tasks N] [-depth N] [-strict]
//	go run ./tools/crgbehaviorgate -regen [-ref REF] [-commits N]
//
// Regeneration is explicit: a gate run never rewrites the pinned corpus.
// The gate exits 0 on PASS, 1 on a gating divergence, and 0 with a SKIP notice
// when the legacy Python bridge is not available on this machine (a missing
// legacy side is an environment fact, not a behavior divergence).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AGOrcha/dot-agents/internal/crgbehavior"
)

// exit codes: 0 pass or skip, 1 gating divergence, 2 usage/plumbing error.
const (
	exitPass  = 0
	exitFail  = 1
	exitError = 2
)

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stdout, os.Stderr))
}

// options are the parsed command-line knobs.
type options struct {
	repo      string
	graphRepo string
	manifest  string
	ref       string
	commits   int
	tasks     int
	depth     int
	strict    bool
	regen     bool
}

// mainRun is the testable entry point: it never calls os.Exit.
func mainRun(args []string, stdout, stderr io.Writer) int {
	opts, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}
	if opts.regen {
		return regenerate(opts, stdout, stderr)
	}
	return runGate(opts, stdout, stderr)
}

// parseArgs parses the flag set. ok is false when the caller should exit with
// the returned code.
func parseArgs(args []string, stderr io.Writer) (options, int, bool) {
	var o options
	fs := flag.NewFlagSet("crgbehaviorgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.repo, "repo", ".", "repository whose history the corpus is pinned from")
	fs.StringVar(&o.graphRepo, "graph-repo", "", "repository holding the legacy .code-review-graph graph (defaults to -repo)")
	fs.StringVar(&o.manifest, "manifest", crgbehavior.DefaultManifestPath, "pinned corpus manifest path")
	fs.StringVar(&o.ref, "ref", crgbehavior.DefaultRef, "git ref the corpus window is taken from (-regen)")
	fs.IntVar(&o.commits, "commits", crgbehavior.DefaultCommitCount, "commits to pin (-regen)")
	fs.IntVar(&o.tasks, "tasks", 0, "run only the first N corpus tasks (0 = all)")
	fs.IntVar(&o.depth, "depth", crgbehavior.DefaultDepth, "impact-radius hop budget")
	fs.BoolVar(&o.strict, "strict", false, "promote the advisory surfaces to gating (§11.4 sign-off)")
	fs.BoolVar(&o.regen, "regen", false, "regenerate the pinned corpus manifest and exit")
	if err := fs.Parse(args); err != nil {
		return o, exitError, false
	}
	if o.graphRepo == "" {
		o.graphRepo = o.repo
	}
	return o, exitPass, true
}

// regenerate rewrites the pinned corpus manifest from real history.
func regenerate(o options, stdout, stderr io.Writer) int {
	m, err := crgbehavior.BuildManifest(o.repo, o.ref, o.commits)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if err := m.Save(o.manifest); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	fmt.Fprintf(stdout, "wrote %s: %d review task(s) pinned from %s at %s\n",
		o.manifest, len(m.Tasks), m.GeneratedFrom, m.Head)
	return exitPass
}

// runGate executes the gate against the live legacy bridge.
func runGate(o options, stdout, stderr io.Writer) int {
	manifest, err := crgbehavior.LoadManifest(o.manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	report, err := crgbehavior.RunLive(crgbehavior.Config{
		RepoRoot: o.repo,
		Manifest: manifest,
		Depth:    o.depth,
		MaxTasks: o.tasks,
		Strict:   o.strict,
	}, o.graphRepo)
	if err != nil {
		return reportRunError(err, stdout, stderr)
	}
	report.Render(stdout)
	if !report.Pass() {
		return exitFail
	}
	return exitPass
}

// reportRunError maps a run failure onto an exit code: an unavailable legacy
// bridge SKIPS (the gate cannot run, but nothing diverged), anything else is a
// plumbing error.
func reportRunError(err error, stdout, stderr io.Writer) int {
	if errors.Is(err, crgbehavior.ErrBridgeUnavailable) {
		fmt.Fprintf(stdout, "SKIP: %v\n", err)
		fmt.Fprintln(stdout, "SKIP: the dual-read comparison needs the legacy Python code-review-graph "+
			"and a built .code-review-graph/graph.db; see testdata/crg-behavior/BEHAVIOR.md")
		return exitPass
	}
	fmt.Fprintln(stderr, err)
	return exitError
}
