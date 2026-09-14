// Command crgbehaviorgate runs the CRG behavior-preservation gate
// (graph-backend-adapter-contract §11.4 criterion 2) over a corpus of REAL
// review tasks pinned from this repository's history, and maintains that
// corpus plus its release-pinned upstream baseline.
//
// The gate certifies against ONE release — code-review-graph 2.3.8, graph
// schema v9 — and it materializes every pinned commit in its own isolated
// linked worktree (natively, via internal/gitwt over go-git) before comparing,
// so a historical review task is replayed against the repository as it
// actually was. Every oracle is exact.
//
// Usage:
//
//	go run ./tools/crgbehaviorgate [-repo DIR] [-tasks N] [-depth N] [-json PATH]
//	go run ./tools/crgbehaviorgate -record [-tasks N]
//	go run ./tools/crgbehaviorgate -regen [-ref REF] [-commits N] [-window N]
//
// Regeneration and fixture recording are explicit: a gate run never rewrites
// the pinned corpus and never re-records its own baseline.
//
// Exit codes:
//
//	0  PASS          — every required surface exercised, every oracle agreed
//	1  FAIL          — a behavior diverged, a comparison failed, or a required
//	                   surface went unexercised without a ratified exception
//	2  ERROR         — usage, plumbing, off-release bridge, or incompatible schema
//	3  INCONCLUSIVE  — the pinned bridge could not be driven, so the run produced
//	                   no evidence. This is NOT a pass: an absent bridge cannot
//	                   demonstrate preserved behavior.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/crgbehavior"
)

const (
	exitPass         = 0
	exitFail         = 1
	exitError        = 2
	exitInconclusive = 3
)

func main() {
	os.Exit(mainRun(os.Args[1:], os.Stdout, os.Stderr))
}

// options are the parsed command-line knobs.
type options struct {
	repo       string
	workDir    string
	manifest   string
	release    string
	contract   string
	fixtures   string
	jsonReport string
	ref        string
	commits    int
	window     int
	tasks      int
	depth      int
	maxResults int
	regen      bool
	record     bool
}

// mainRun is the testable entry point: it never calls os.Exit.
func mainRun(args []string, stdout, stderr io.Writer) int {
	opts, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}
	rel, err := crgbehavior.LoadRelease(opts.release)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if opts.regen {
		return regenerate(opts, rel, stdout, stderr)
	}
	if opts.record {
		return record(opts, rel, stdout, stderr)
	}
	return runGate(opts, rel, stdout, stderr)
}

// parseArgs parses the flag set. ok is false when the caller should exit with
// the returned code.
func parseArgs(args []string, stderr io.Writer) (options, int, bool) {
	var o options
	fs := flag.NewFlagSet("crgbehaviorgate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.repo, "repo", ".", "repository whose history the corpus is pinned from")
	fs.StringVar(&o.workDir, "work-dir", "", "parent directory for the per-commit worktrees (default: a private directory under the user cache)")
	fs.StringVar(&o.manifest, "manifest", crgbehavior.DefaultManifestPath, "pinned corpus manifest path")
	fs.StringVar(&o.release, "release", crgbehavior.DefaultReleasePath, "pinned release capability fixture path")
	fs.StringVar(&o.contract, "contract", crgbehavior.DefaultContractPath, "corpus contract path")
	fs.StringVar(&o.fixtures, "fixtures", crgbehavior.DefaultFixtureDir, "release-pinned upstream behavior fixture directory")
	fs.StringVar(&o.jsonReport, "json", "", "also write the machine-readable run artifact to this path")
	fs.StringVar(&o.ref, "ref", crgbehavior.DefaultRef, "git ref the corpus window is taken from (-regen)")
	fs.IntVar(&o.commits, "commits", crgbehavior.DefaultCommitCount, "review tasks to pin (-regen)")
	fs.IntVar(&o.window, "window", 0, "commits to scan when pinning (-regen; 0 = release default)")
	fs.IntVar(&o.tasks, "tasks", 0, "run only the first N corpus tasks (0 = all)")
	fs.IntVar(&o.depth, "depth", crgbehavior.DefaultDepth, "impact-radius hop budget")
	fs.IntVar(&o.maxResults, "max-results", crgbehavior.DefaultMaxResults, "impact-radius result cap")
	fs.BoolVar(&o.regen, "regen", false, "regenerate the pinned corpus manifest and exit")
	fs.BoolVar(&o.record, "record", false, "record the release-pinned upstream behavior fixtures and exit")
	if err := fs.Parse(args); err != nil {
		return o, exitError, false
	}
	if o.regen && o.record {
		fmt.Fprintln(stderr, "crgbehaviorgate: -regen and -record are separate commands; run one at a time")
		return o, exitError, false
	}
	if o.workDir == "" {
		dir, ok := defaultWorkDir()
		if !ok {
			fmt.Fprintln(stderr, "crgbehaviorgate: no user cache directory to materialize worktrees in; pass -work-dir")
			return o, exitError, false
		}
		o.workDir = dir
	}
	return o, exitPass, true
}

// defaultWorkDir is where per-commit worktrees are materialized when -work-dir
// is not given. It is outside the repository so a materialized checkout can
// never be mistaken for working state or picked up by a build.
//
// It lives in the user's own cache directory rather than the shared temp root.
// The path is deliberately STABLE — addWorktree clears whatever an interrupted
// run left at it — and a stable, guessable path under a world-writable
// directory is a path another user can pre-create or point at a symlink. The
// cache directory is the user's own, so stability costs nothing.
//
// ok is false when the platform reports no cache directory (no HOME, no
// XDG_CACHE_HOME). The gate then refuses to guess and asks for -work-dir:
// falling back to the shared temp root would reintroduce exactly the exposure
// this avoids.
func defaultWorkDir() (string, bool) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(cache, "crg-behavior-worktrees"), true
}

// regenerate rewrites the pinned corpus manifest from real history, using the
// pinned release's own indexed-language set.
func regenerate(o options, rel crgbehavior.Release, stdout, stderr io.Writer) int {
	m, err := crgbehavior.BuildManifest(o.repo, o.ref, o.commits, o.window, rel)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if err := m.Save(o.manifest); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	covered, uncovered := crgbehavior.ExtractorCoverage(rel)
	fmt.Fprintf(stdout, "wrote %s: %d review task(s) pinned from %s at %s for %s %s\n",
		o.manifest, len(m.Tasks), m.GeneratedFrom, m.Head, crgbehavior.PackageName, rel.Version)
	fmt.Fprintf(stdout, "declaration extractors: %d release language(s) covered, %d without one (%v)\n",
		len(covered), len(uncovered), uncovered)
	return exitPass
}

// record drives the pinned release over every corpus task and writes the
// release-pinned upstream behavior fixtures the gate conforms against.
func record(o options, rel crgbehavior.Release, stdout, stderr io.Writer) int {
	cfg, code, ok := gateConfig(o, rel, stderr)
	if !ok {
		return code
	}
	mat, err := materializer(o, rel, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	fixtures, err := crgbehavior.RecordFixtures(cfg, mat)
	if err != nil {
		return reportRunError(err, stdout, stderr)
	}
	for _, f := range fixtures {
		fmt.Fprintf(stdout, "recorded %s from %s\n", crgbehavior.FixturePath(o.fixtures, f.Commit), f.Release)
	}
	return exitPass
}

// runGate executes the gate against the pinned release.
func runGate(o options, rel crgbehavior.Release, stdout, stderr io.Writer) int {
	cfg, code, ok := gateConfig(o, rel, stderr)
	if !ok {
		return code
	}
	mat, err := materializer(o, rel, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	report, err := crgbehavior.Run(cfg, mat)
	if err != nil {
		return reportRunError(err, stdout, stderr)
	}
	report.Render(stdout)
	if o.jsonReport != "" {
		if err := report.WriteJSON(o.jsonReport); err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
		fmt.Fprintf(stdout, "artifact: %s\n", o.jsonReport)
	}
	switch report.Verdict() {
	case crgbehavior.VerdictPass:
		return exitPass
	case crgbehavior.VerdictInconclusive:
		return exitInconclusive
	default:
		return exitFail
	}
}

// gateConfig loads the corpus, the contract and the release into a run config.
func gateConfig(o options, rel crgbehavior.Release, stderr io.Writer) (crgbehavior.Config, int, bool) {
	manifest, err := crgbehavior.LoadManifest(o.manifest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return crgbehavior.Config{}, exitError, false
	}
	contract, err := crgbehavior.LoadContract(o.contract)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return crgbehavior.Config{}, exitError, false
	}
	return crgbehavior.Config{
		RepoRoot:   o.repo,
		Manifest:   manifest,
		Release:    rel,
		Contract:   contract,
		FixtureDir: o.fixtures,
		Depth:      o.depth,
		MaxResults: o.maxResults,
		MaxTasks:   o.tasks,
	}, exitPass, true
}

// materializer builds the same-SHA materializer, echoing progress because one
// full pinned-release build per commit takes minutes.
func materializer(o options, rel crgbehavior.Release, stdout io.Writer) (crgbehavior.Materializer, error) {
	return crgbehavior.NewWorktreeMaterializer(o.repo, o.workDir, rel, o.depth, o.maxResults,
		func(line string) { fmt.Fprintf(stdout, "... %s\n", line) })
}

// reportRunError maps a run failure onto an exit code. An unavailable bridge is
// INCONCLUSIVE and still exits non-zero: the gate could not be driven, so it
// has demonstrated nothing, and a silent success here is exactly how a missing
// legacy side previously read as preserved behavior.
func reportRunError(err error, stdout, stderr io.Writer) int {
	if errors.Is(err, crgbehavior.ErrBridgeUnavailable) {
		fmt.Fprintf(stdout, "GATE: %s\n", crgbehavior.VerdictInconclusive)
		fmt.Fprintf(stdout, "SIGN-OFF: criterion 2 NOT ESTABLISHED — %v\n", err)
		fmt.Fprintf(stdout, "the dual-read comparison needs %s %s on PATH or in .venv; "+
			"see testdata/crg-behavior/BEHAVIOR.md\n", crgbehavior.PackageName, crgbehavior.PinnedVersion)
		return exitInconclusive
	}
	fmt.Fprintln(stderr, err)
	return exitError
}
