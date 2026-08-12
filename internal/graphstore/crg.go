// Package graphstore — CRG bridge.
//
// CRGBridge delegates code-graph build, update, and query operations to the
// Python code-review-graph CLI installed at crgBin. It does not require Go
// tree-sitter bindings; instead it shells out to the CRG executable and
// marshals its output back to Go types compatible with the graphstore.Store
// interface contracts.
package graphstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/execabs"
	// _ "modernc.org/sqlite": side-effect registers SQLite driver in database/sql
	_ "modernc.org/sqlite"
)

const crgReadOnlyPragma = "?_pragma=query_only(true)"
const crgBinName = "code-review-graph"
const crgFlagRepo = "--repo"

// venvBinSubdirs and venvExeCandidates are OS-specific and live in
// crg_venv_unix.go / crg_venv_windows.go so coverage is measured only against
// the implementation actually compiled for the host (no dead runtime.GOOS
// branch dragging the package below the per-package coverage gate). The shared
// discovery orchestration below calls them unconditionally.

// CRGBridge shells out to the code-review-graph Python CLI.
type CRGBridge struct {
	// RepoRoot is the directory that code-review-graph treats as the project root.
	RepoRoot string
	// Bin is the path to the code-review-graph executable. If empty,
	// DiscoverCRGBin() is called to auto-detect it.
	Bin string
	// runner overrides how per-root builds and postprocess passes are
	// executed. Production leaves it nil (the bridge shells out to Bin);
	// tests inject a runner to drive the multi-root build without the Python
	// CRG installed.
	runner crgRunner
}

// NewCRGBridge returns a CRGBridge rooted at repoRoot, auto-detecting the CRG
// binary from standard locations (workspace .venv, PATH).
func NewCRGBridge(repoRoot string) (*CRGBridge, error) {
	b := &CRGBridge{RepoRoot: repoRoot}
	bin, err := DiscoverCRGBin(repoRoot)
	if err != nil {
		return nil, err
	}
	b.Bin = bin
	return b, nil
}

// DiscoverCRGBin looks for the code-review-graph executable in this order:
//  1. .venv/{bin,Scripts}/code-review-graph[.exe] relative to repoRoot
//  2. the same under repoRoot's parent .venv
//  3. code-review-graph on PATH (exec.LookPath applies PATHEXT on Windows)
func DiscoverCRGBin(repoRoot string) (string, error) {
	candidates := venvExeCandidates(filepath.Join(repoRoot, ".venv"), crgBinName)
	// also check parent dir for .venv
	parent := filepath.Dir(repoRoot)
	candidates = append(candidates,
		venvExeCandidates(filepath.Join(parent, ".venv"), crgBinName)...,
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	// fall back to PATH
	if p, err := exec.LookPath(crgBinName); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("code-review-graph not found in .venv or PATH; install with: uv pip install code-review-graph")
}

// Available returns true if the CRG binary exists and is executable.
func (b *CRGBridge) Available() bool {
	if b.Bin == "" {
		return false
	}
	_, err := os.Stat(b.Bin)
	return err == nil
}

// run executes b.Bin with the given args, returning combined stdout+stderr.
// stderr is forwarded verbatim to the caller if exitErr is non-nil.
func (b *CRGBridge) run(args ...string) ([]byte, error) {
	cmd := exec.Command(b.Bin, args...)
	cmd.Dir = b.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("crg %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// pythonBin returns the path to the Python interpreter in the same .venv as
// the CRG binary.
func (b *CRGBridge) pythonBin() string {
	// b.Bin is e.g. /path/to/.venv/bin/code-review-graph (POSIX) or
	// ...\.venv\Scripts\code-review-graph.exe (Windows). Python lives in
	// the same executable directory.
	binDir := filepath.Dir(b.Bin)
	for _, n := range venvPythonNames() {
		c := filepath.Join(binDir, n)
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return venvPythonFallback()
}

// runPyQuery executes a Python expression via the venv interpreter and returns
// the JSON output. The expression must print exactly one JSON document to stdout.
// The expression receives a pre-imported `repo_root` variable set to b.RepoRoot.
func (b *CRGBridge) runPyQuery(pyExpr string) ([]byte, error) {
	// Wrap in a small script: set repo_root, exec the expression, print result.
	script := fmt.Sprintf(`
import json, sys
sys.path.insert(0, %q)
repo_root = %q
%s
`, b.RepoRoot, b.RepoRoot, pyExpr)

	// Provider-owned request timeout (CONTRACT.md guarantee #2): a CRG
	// query is a Python subprocess that can run a long graph traversal.
	// exec.CommandContext kills it when the provider's deadline elapses
	// so callers never wrap their own deadline around Store/CRG calls.
	ctx, cancel := requestContext(nil)
	defer cancel()

	py := b.pythonBin()
	cmd := exec.CommandContext(ctx, py, "-c", script)
	cmd.Dir = b.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("crg-py: request timed out after %s", requestTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("crg-py: %s", msg)
	}
	return stdout.Bytes(), nil
}

// runStreamed executes the command with stdout/stderr forwarded directly to the
// caller's stdout/stderr — suitable for long-running commands like build.
func (b *CRGBridge) runStreamed(args ...string) error {
	cmd := exec.Command(b.Bin, args...)
	cmd.Dir = b.RepoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ── Build / Update ────────────────────────────────────────────────────────────

// BuildOptions configures a full-graph build.
type BuildOptions struct {
	// SkipFlows skips community/flow detection (faster, code signatures only).
	SkipFlows bool
	// SkipPostprocess skips all post-processing (raw parse only).
	SkipPostprocess bool
	// NoRecurseSubmodules opts out of indexing the repository's submodules.
	// The default (false) recurses: a superproject build that ignored its
	// submodules would index a fraction of the workspace and still report
	// success. Opting out does not hide the submodules — they are reported as
	// explicitly excluded in the build summary and the readiness status.
	NoRecurseSubmodules bool
}

// CRGOperationReport captures a build/update outcome for CLI callers.
type CRGOperationReport struct {
	Operation    string     `json:"operation"`
	Outcome      string     `json:"outcome"`
	Summary      string     `json:"summary"`
	ChangedFiles []string   `json:"changed_files,omitempty"`
	Status       *CRGStatus `json:"status,omitempty"`
	RawOutput    string     `json:"raw_output,omitempty"`
	// Workspace is the resolved set of indexed roots and skipped submodules.
	// It is present whenever the repository could be enumerated, so a caller
	// can always see what the reported counts actually cover.
	Workspace *WorkspacePlan `json:"workspace,omitempty"`
	// Merged reports the rows folded in from submodule graphs.
	Merged *MergeStats `json:"merged,omitempty"`
	// EnumerationError is set when the repository could not be enumerated at
	// all (not a git checkout, git unavailable). The build still ran over the
	// given root, but nothing can be said about submodules — which is a fact
	// the caller has to see rather than infer from a missing Workspace.
	EnumerationError string `json:"enumeration_error,omitempty"`
}

// crgRunner is the subprocess seam the workspace build drives: one CRG build
// per root, then one postprocess over the merged superproject graph. Tests
// substitute a runner that seeds graph databases directly so the multi-root
// orchestration is exercised without the Python CRG installed.
type crgRunner interface {
	buildRoot(root string, opts BuildOptions) ([]byte, error)
	postprocessRoot(root string, opts PostprocessOptions) error
}

// activeRunner returns the injected runner, defaulting to the bridge itself
// (which shells out to the CRG binary).
func (b *CRGBridge) activeRunner() crgRunner {
	if b.runner != nil {
		return b.runner
	}
	return b
}

// buildRoot runs `code-review-graph build` against one repository root.
func (b *CRGBridge) buildRoot(root string, opts BuildOptions) ([]byte, error) {
	args := []string{"build", crgFlagRepo, root}
	if opts.SkipFlows {
		args = append(args, "--skip-flows")
	}
	if opts.SkipPostprocess {
		args = append(args, "--skip-postprocess")
	}
	return b.runCaptured(args...)
}

// postprocessRoot rebuilds derived state (flows, communities, FTS) for root as
// part of a build.
//
// It runs through runCaptured — the same execution path the build itself uses
// — rather than the streaming path of the standalone Postprocess command. That
// matters twice over: the wrapper it applies to a Python entrypoint forces
// SQLite autocommit (this pass writes derived tables), and it invokes the
// interpreter directly, so a console script with a stale shebang cannot fail a
// build that would otherwise have succeeded.
func (b *CRGBridge) postprocessRoot(root string, opts PostprocessOptions) error {
	out, err := b.runCaptured(postprocessArgs(root, opts)...)
	if err != nil {
		return classifyCRGRunError("postprocess", err, out)
	}
	return nil
}

// postprocessArgs builds the CRG postprocess argv for root.
func postprocessArgs(root string, opts PostprocessOptions) []string {
	args := []string{"postprocess", crgFlagRepo, root}
	if opts.NoFlows {
		args = append(args, "--no-flows")
	}
	if opts.NoCommunities {
		args = append(args, "--no-communities")
	}
	if opts.NoFTS {
		args = append(args, "--no-fts")
	}
	return args
}

// BuildReport triggers a full graph rebuild and returns a structured summary.
//
// In a superproject the build is workspace-wide: the root and every
// initialized submodule are built, the submodule graphs are merged into the
// root graph under a per-repository scope, and postprocess runs once over the
// merged result. Postprocess is deliberately deferred to the end — merging
// base rows after a postprocess pass would leave the FTS index, flows, and
// communities describing only the root repository while the graph "looked"
// complete.
func (b *CRGBridge) BuildReport(opts BuildOptions) (*CRGOperationReport, error) {
	plan, planErr := PlanWorkspace(b.RepoRoot, !opts.NoRecurseSubmodules)
	subRoots := plan.Submodules()

	// Root build. When submodule graphs still have to be merged in, defer
	// postprocess until after the merge rather than running it twice.
	rootOpts := opts
	if len(subRoots) > 0 {
		rootOpts.SkipPostprocess = true
	}
	out, err := b.activeRunner().buildRoot(b.RepoRoot, rootOpts)
	if err != nil {
		return nil, classifyCRGRunError("build", err, out)
	}

	merged, mergeErr := b.buildAndMergeSubmodules(subRoots, opts)
	if mergeErr != nil {
		return nil, mergeErr
	}
	if planErr == nil {
		// Record what this build covered BEFORE reading status back: readiness
		// distinguishes "never looked at" from "looked and found nothing" (and
		// from "deliberately excluded") using this record.
		if err := writeCoverage(b.RepoRoot, plan); err != nil {
			return nil, fmt.Errorf("record workspace coverage: %w", err)
		}
	}

	status, statusErr := b.Status()
	if statusErr != nil {
		return nil, statusErr
	}

	report := &CRGOperationReport{
		Operation: "build",
		Status:    status,
		RawOutput: strings.TrimSpace(string(out)),
		Merged:    merged,
	}
	report.applyWorkspace(plan, planErr)
	report.applyBuildOutcome(status)
	if report.RawOutput != "" && report.Summary == "" {
		report.Summary = report.RawOutput
	}
	return report, nil
}

// mergeWriteDSN carries the write pragmas every merge connection needs.
// busy_timeout matches the rest of this package's writers (see sqlite.go): a
// concurrent MCP server or a parallel `da kg` holding the database must make
// the merge wait, not fail — the per-root builds that precede it are minutes
// of work to redo.
const mergeWriteDSN = "?_pragma=busy_timeout(5000)"

// mergeSubmoduleGraph folds one submodule's graph into the superproject graph
// on a connection of its own. A fresh handle per submodule keeps a failed
// merge (a rolled-back transaction, an undone ATTACH) from reaching the next
// one, so a failure is always attributed to the submodule that caused it.
func (b *CRGBridge) mergeSubmoduleGraph(sub WorkspaceRoot) (MergeStats, error) {
	db, err := sql.Open("sqlite", CRGDBPath(b.RepoRoot)+mergeWriteDSN)
	if err != nil {
		return MergeStats{}, fmt.Errorf("open superproject graph: %w", err)
	}
	defer db.Close()
	return MergeGraphDB(db, CRGDBPath(sub.AbsPath), sub.Scope)
}

// applyWorkspace attaches the resolved root plan, or — when the repository
// could not be enumerated at all — records why, so a build that silently
// covered the root alone cannot pass for a normal single-repo build.
func (r *CRGOperationReport) applyWorkspace(plan WorkspacePlan, planErr error) {
	if planErr != nil {
		r.EnumerationError = planErr.Error()
		return
	}
	planCopy := plan
	r.Workspace = &planCopy
}

// applyBuildOutcome maps the post-build readiness state onto the report's
// outcome and summary, then appends the per-root coverage line.
func (r *CRGOperationReport) applyBuildOutcome(status *CRGStatus) {
	switch {
	case status.Ready:
		r.Outcome = string(CRGReadinessReady)
		r.Summary = fmt.Sprintf("Build complete: %d nodes, %d edges, %d files", status.Nodes, status.Edges, status.Files)
	case status.State == CRGReadinessIncomplete:
		r.Outcome = CRGReadinessIncomplete
		r.Summary = fmt.Sprintf("Build incomplete: %d nodes, %d edges, %d files — %s",
			status.Nodes, status.Edges, status.Files, status.Message)
	case status.State == string(CRGReadinessUnbuilt):
		r.Outcome = string(CRGReadinessUnbuilt)
		r.Summary = "Build completed but the code graph is still unbuilt."
	case status.State == string(CRGReadinessBusyOrLocked):
		r.Outcome = string(CRGReadinessBusyOrLocked)
		r.Summary = "Build completed, but the code graph is busy or locked."
	default:
		r.Outcome = string(CRGReadinessError)
		r.Summary = status.Message
		if r.Summary == "" {
			r.Summary = "Build completed, but code graph status could not be determined."
		}
	}
	r.appendRootsLine()
}

// appendRootsLine adds the per-root enumeration to a non-empty summary.
func (r *CRGOperationReport) appendRootsLine() {
	if r.Summary == "" {
		return
	}
	if r.Workspace != nil {
		r.Summary += "\nRoots: " + r.Workspace.Summary()
		return
	}
	if r.EnumerationError != "" {
		r.Summary += "\nRoots: not enumerated (" + r.EnumerationError +
			") — this build covered the given root only."
	}
}

// buildAndMergeSubmodules builds each submodule root, merges its graph into
// the superproject graph under the submodule's scope, and then runs the single
// postprocess pass that rebuilds derived state over the merged rows. It
// returns nil stats when there is nothing to merge.
func (b *CRGBridge) buildAndMergeSubmodules(subRoots []WorkspaceRoot, opts BuildOptions) (*MergeStats, error) {
	if len(subRoots) == 0 {
		return nil, nil
	}
	// Submodule graphs are merged before any derived state is computed, so
	// each submodule build skips its own postprocess: the only postprocess
	// that matters runs over the merged superproject graph below.
	subOpts := BuildOptions{SkipFlows: opts.SkipFlows, SkipPostprocess: true}
	merged := &MergeStats{}
	for _, sub := range subRoots {
		out, buildErr := b.activeRunner().buildRoot(sub.AbsPath, subOpts)
		if buildErr != nil {
			return nil, classifyCRGRunError("build "+sub.Path, buildErr, out)
		}
		stats, mergeErr := b.mergeSubmoduleGraph(sub)
		if mergeErr != nil {
			return nil, fmt.Errorf("merge submodule %s: %w", sub.Path, mergeErr)
		}
		merged.Nodes += stats.Nodes
		merged.Edges += stats.Edges
	}
	if opts.SkipPostprocess {
		return merged, nil
	}
	if err := b.activeRunner().postprocessRoot(b.RepoRoot, PostprocessOptions{
		NoFlows:       opts.SkipFlows,
		NoCommunities: opts.SkipFlows,
	}); err != nil {
		return nil, fmt.Errorf("postprocess merged graph: %w", err)
	}
	return merged, nil
}

// Build triggers a full graph rebuild via `code-review-graph build`.
// The structured report is intentionally discarded for legacy callers.
func (b *CRGBridge) Build(opts BuildOptions) error {
	_, err := b.BuildReport(opts)
	return err
}

// UpdateOptions configures an incremental graph update.
type UpdateOptions struct {
	// Base is the git ref to diff against (default: HEAD~1).
	Base string
	// SkipFlows skips community/flow detection.
	SkipFlows bool
	// SkipPostprocess skips all post-processing.
	SkipPostprocess bool
}

// UpdateReport triggers an incremental graph update and returns a structured summary.
func (b *CRGBridge) UpdateReport(opts UpdateOptions) (*CRGOperationReport, error) {
	args := []string{"update", crgFlagRepo, b.RepoRoot}
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	if opts.SkipFlows {
		args = append(args, "--skip-flows")
	}
	if opts.SkipPostprocess {
		args = append(args, "--skip-postprocess")
	}
	changedFiles, diffErr := b.gitChangedFiles(opts.Base)
	if diffErr != nil {
		return nil, diffErr
	}
	if len(changedFiles) == 0 {
		status, statusErr := b.Status()
		if statusErr != nil {
			return nil, statusErr
		}
		return &CRGOperationReport{
			Operation:    "update",
			Outcome:      "no_diff",
			Summary:      "No diff to apply; code graph left unchanged.",
			ChangedFiles: nil,
			Status:       status,
		}, nil
	}

	out, err := b.runCaptured(args...)
	if err != nil {
		return nil, classifyCRGRunError("update", err, out)
	}

	status, statusErr := b.Status()
	if statusErr != nil {
		return nil, statusErr
	}

	filesUpdated, nodesChanged, edgesChanged, parsed := parseCRGMutationSummary(out)
	outcome := "updated"
	summary := fmt.Sprintf("Update complete: %d nodes, %d edges, %d files", status.Nodes, status.Edges, status.Files)
	if parsed && nodesChanged == 0 && edgesChanged == 0 {
		outcome = "no_mutation"
		if filesUpdated > 0 {
			summary = fmt.Sprintf("Changed %d files with no graph mutations.", filesUpdated)
		} else {
			summary = "Update completed with no graph mutations."
		}
	}

	return &CRGOperationReport{
		Operation:    "update",
		Outcome:      outcome,
		Summary:      summary,
		ChangedFiles: changedFiles,
		Status:       status,
		RawOutput:    strings.TrimSpace(string(out)),
	}, nil
}

// Update triggers an incremental graph update via `code-review-graph update`.
// The structured report is intentionally discarded for legacy callers.
func (b *CRGBridge) Update(opts UpdateOptions) error {
	_, err := b.UpdateReport(opts)
	return err
}

// ── Status ────────────────────────────────────────────────────────────────────

// CRGStatus is the parsed output of `code-review-graph status`.
type CRGStatus struct {
	Nodes       int    `json:"nodes"`
	Edges       int    `json:"edges"`
	Files       int    `json:"files"`
	Languages   string `json:"languages"`
	LastUpdated string `json:"last_updated"`
	State       string `json:"state"`
	Ready       bool   `json:"ready"`
	Message     string `json:"message,omitempty"`
	// Roots breaks the counts down per indexed repository — the superproject
	// first, then every submodule detected in the checkout. It is populated
	// only for superprojects; a plain repository has a single implicit root
	// and omits it.
	Roots []RootStatus `json:"roots,omitempty"`
}

// RootStatus is one repository's contribution to the graph.
type RootStatus struct {
	// Path is the root's path relative to the superproject ("." for the
	// superproject itself).
	Path string `json:"path"`
	// Nodes and Files are the rows in the graph attributed to this root.
	Nodes int `json:"nodes"`
	Files int `json:"files"`
	// Indexed reports whether this root contributed anything to the graph.
	Indexed bool `json:"indexed"`
	// Note explains a root that is present in the checkout but absent from
	// the graph.
	Note string `json:"note,omitempty"`
}

// applyWorkspaceReadiness attributes the graph's rows to the superproject and
// each submodule, and downgrades readiness when a submodule exists in the
// checkout but contributed nothing.
//
// This is the honest-readiness rule: a build that indexed 2 of 885 files
// because it never descended into the submodules must not report READY. When
// git cannot enumerate (not a repo, git unavailable) or the repo has no
// submodules, the status is left exactly as the single-repo path produced it.
func (b *CRGBridge) applyWorkspaceReadiness(status *CRGStatus, db *sql.DB) {
	subs, err := DiscoverSubmodules(b.RepoRoot)
	if err != nil || len(subs) == 0 {
		return
	}
	counts, err := countRootRows(db, b.RepoRoot, subs)
	if err != nil {
		return
	}
	coverage := readCoverage(b.RepoRoot)
	roots := []RootStatus{{
		Path:    ".",
		Nodes:   counts[superprojectRoot].Nodes,
		Files:   counts[superprojectRoot].Files,
		Indexed: counts[superprojectRoot].Nodes > 0,
	}}
	var missing []string
	for _, sub := range subs {
		root := rootStatusFor(sub, counts[sub.Path], coverage)
		if !root.Indexed && !coverage.excluded(sub.Path) {
			missing = append(missing, sub.Path)
		}
		roots = append(roots, root)
	}
	status.Roots = roots
	if len(missing) == 0 {
		return
	}
	status.Ready = false
	status.State = CRGReadinessIncomplete
	status.Message = fmt.Sprintf("submodules detected but not indexed: %s — initialize them and rerun 'da kg build', "+
		"or rebuild with --no-recurse-submodules to declare them out of scope; until then, queries against these "+
		"paths return nothing", strings.Join(missing, ", "))
}

// rootStatusFor renders one submodule's row attribution, explaining an empty
// root in terms the operator can act on.
//
// A root the last build deliberately covered is reported as indexed even at
// zero rows: the build looked and found no symbols (a docs-only or
// unsupported-language submodule), which is a complete answer, not a missing
// one. A root the last build was told to exclude is reported as excluded. Only
// a root that is genuinely absent counts as missing.
func rootStatusFor(sub Submodule, count rootCount, coverage buildCoverage) RootStatus {
	root := RootStatus{Path: sub.Path, Nodes: count.Nodes, Files: count.Files, Indexed: count.Nodes > 0}
	switch {
	case root.Indexed:
		return root
	case coverage.excluded(sub.Path):
		root.Note = "excluded from the last build (--no-recurse-submodules)"
	case coverage.indexed(sub.Path):
		root.Indexed = true
		root.Note = "indexed; no symbols found in this submodule"
	case !sub.Initialized:
		root.Note = SkipReasonUninitialized
	default:
		root.Note = "present in the checkout but absent from the graph"
	}
	return root
}

// superprojectRoot is the key rootCount uses for rows belonging to no
// submodule — the superproject's own code.
const superprojectRoot = "."

// rootCount is one root's share of the graph.
type rootCount struct {
	Nodes int
	Files int
}

// countRootRows attributes every node row to exactly one root in a single
// pass.
//
// Exclusivity is what makes nested submodules add up: `a/b`'s rows also sit
// under `a`'s path prefix, so counting each root independently would count
// them twice and could drive the superproject's share negative. The CASE arms
// are emitted deepest-path-first and SQLite takes the first that matches, so
// each row lands in its innermost root and the superproject keeps exactly what
// no submodule claimed.
//
// Rows are matched by the scope prefix the merge stamps onto qualified names,
// and by file path (absolute or superproject-relative). Path comparison is
// done on a slash-normalized copy so a graph written with either separator
// style — CRG on Windows, or an external aggregator — attributes the same.
func countRootRows(db *sql.DB, repoRoot string, subs []Submodule) (map[string]rootCount, error) {
	ordered := make([]Submodule, len(subs))
	copy(ordered, subs)
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i].Path) > len(ordered[j].Path) })

	counts, err := queryRootCounts(db, repoRoot, ordered, true)
	if err == nil {
		return counts, nil
	}
	// A graph whose nodes table has no qualified_name column (an older or
	// reduced schema) can still be attributed by file path alone. Falling back
	// keeps the readiness check working instead of dropping it on a schema
	// difference.
	return queryRootCounts(db, repoRoot, ordered, false)
}

// queryRootCounts runs the single-pass attribution. withQualifiedName selects
// whether the merge's scope prefix participates in the match.
func queryRootCounts(db *sql.DB, repoRoot string, ordered []Submodule, withQualifiedName bool) (map[string]rootCount, error) {
	var arms strings.Builder
	var args []any
	for _, sub := range ordered {
		if withQualifiedName {
			arms.WriteString(" WHEN instr(qualified_name, ?) = 1 OR instr(norm_path, ?) = 1 OR instr(norm_path, ?) = 1 THEN ?")
			args = append(args, sub.Path+scopeSeparator)
		} else {
			arms.WriteString(" WHEN instr(norm_path, ?) = 1 OR instr(norm_path, ?) = 1 THEN ?")
		}
		args = append(args, filepath.ToSlash(sub.AbsPath(repoRoot))+"/", sub.Path+"/", sub.Path)
	}
	selected := "file_path, replace(file_path, '\\', '/') AS norm_path"
	if withQualifiedName {
		selected += ", qualified_name"
	}
	query := `WITH attributed AS (SELECT ` + selected + ` FROM nodes)
	SELECT CASE` + arms.String() + ` ELSE ? END AS root, COUNT(*), COUNT(DISTINCT file_path)
	FROM attributed GROUP BY root`
	args = append(args, superprojectRoot)

	rows, err := db.Query(query, args...) //nolint:gosec // the query is assembled from bound placeholders only
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]rootCount{}
	for rows.Next() {
		var root string
		var c rootCount
		if err := rows.Scan(&root, &c.Nodes, &c.Files); err != nil {
			return nil, err
		}
		counts[root] = c
	}
	return counts, rows.Err()
}

// Status returns the current graph stats from `code-review-graph status`.
// The bridge reads the SQLite database directly so code-status can work even
// when the CRG binary is unavailable.
func (b *CRGBridge) Status() (*CRGStatus, error) {
	status := &CRGStatus{
		LastUpdated: "never",
		State:       string(CRGReadinessUnbuilt),
	}
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		status.Message = "code-review-graph database missing"
		return status, nil
	}

	db, err := sql.Open("sqlite", dbPath+crgReadOnlyPragma)
	if err != nil {
		status.State = string(CRGReadinessError)
		status.Message = fmt.Sprintf("open CRG db: %v", err)
		return status, nil
	}
	defer db.Close()

	var nodes, files, edges int
	var lastUpdated sql.NullString
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT file_path), COALESCE(MAX(updated_at), '')
		FROM nodes`).Scan(&nodes, &files, &lastUpdated); err != nil {
		applyCRGStatusError(status, err)
		return status, nil
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edges); err != nil {
		applyCRGStatusError(status, err)
		return status, nil
	}

	languages, langErr := readCRGLanguages(db)
	if langErr != nil {
		applyCRGStatusError(status, langErr)
		return status, nil
	}

	status.Nodes = nodes
	status.Edges = edges
	status.Files = files
	status.Languages = strings.Join(languages, ", ")
	if lastUpdated.Valid && strings.TrimSpace(lastUpdated.String) != "" {
		status.LastUpdated = normalizeCRGUpdatedAt(strings.TrimSpace(lastUpdated.String))
	}
	if status.Nodes > 0 && status.Files > 0 && status.LastUpdated != "never" {
		status.State = string(CRGReadinessReady)
		status.Ready = true
		b.applyWorkspaceReadiness(status, db)
		return status, nil
	}

	status.State = string(CRGReadinessUnbuilt)
	if status.Message == "" {
		status.Message = "code graph has not been built yet"
	}
	return status, nil
}

// applyCRGStatusError classifies a SQLite error against the busy/locked,
// unbuilt, and generic-error tiers and records the matching state and
// message on status. Behavior mirrors the previous inlined branches.
func applyCRGStatusError(status *CRGStatus, err error) {
	switch {
	case isCRGBusyLockedError(err):
		status.State = string(CRGReadinessBusyOrLocked)
	case isCRGUnbuiltError(err):
		// Leave status.State at its prior value (unbuilt by default).
	default:
		status.State = string(CRGReadinessError)
	}
	status.Message = err.Error()
}

const (
	CRGReadinessUnbuilt      = "unbuilt"
	CRGReadinessReady        = "ready"
	CRGReadinessBusyOrLocked = "busy_or_locked"
	CRGReadinessError        = "error"
	// CRGReadinessIncomplete means the graph was built and has rows, but a
	// repository root that exists in the checkout (a submodule) is not in it.
	// Queries still work; they silently under-report, which is why this is not
	// `ready`.
	CRGReadinessIncomplete = "incomplete"
)

func (b *CRGBridge) runCaptured(args ...string) ([]byte, error) {
	cmd, err := b.commandWithSQLiteAutocommit(args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = b.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	out := append(stdout.Bytes(), stderr.Bytes()...)
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("crg %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func (b *CRGBridge) commandWithSQLiteAutocommit(args ...string) (*exec.Cmd, error) {
	if !isPythonEntrypoint(b.Bin) {
		return exec.Command(b.Bin, args...), nil
	}
	py := b.pythonBin()
	script := fmt.Sprintf(`
import runpy
import sqlite3
import sys

target = %q
orig_connect = sqlite3.connect

def patched_connect(*args, **kwargs):
    kwargs.setdefault("isolation_level", None)
    return orig_connect(*args, **kwargs)

sqlite3.connect = patched_connect
sys.argv = [target] + sys.argv[1:]
runpy.run_path(target, run_name="__main__")
`, b.Bin)
	return exec.Command(py, append([]string{"-c", script}, args...)...), nil
}

func isPythonEntrypoint(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	firstLine := string(data)
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	firstLine = strings.ToLower(strings.TrimSpace(firstLine))
	return strings.HasPrefix(firstLine, "#!") && strings.Contains(firstLine, "python")
}

func (b *CRGBridge) gitChangedFiles(base string) ([]string, error) {
	if base == "" {
		base = "HEAD~1"
	}
	cmd := execabs.Command("git", "-C", b.RepoRoot, "diff", "--name-only", "--diff-filter=ACMRTUXB", base+"...HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return nil, fmt.Errorf("git diff %s...HEAD: %s", base, msg)
		}
		return nil, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// crgMutationSummaryRE matches the "N files updated … N nodes … N edges" line
// the CRG import tool prints. Compiled once at package init rather than per
// parseCRGMutationSummary call.
var crgMutationSummaryRE = regexp.MustCompile(`(?i)(\d+)\s+files?(?:\s+updated)?[^0-9]+(\d+)\s+nodes?[^0-9]+(\d+)\s+edges?`)

func parseCRGMutationSummary(out []byte) (filesUpdated, nodesChanged, edgesChanged int, ok bool) {
	text := string(out)
	text = strings.NewReplacer("\r", "\n").Replace(text)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		if match := crgMutationSummaryRE.FindStringSubmatch(line); len(match) == 4 {
			var file, node, edge int
			if _, err := fmt.Sscanf(match[1], "%d", &file); err == nil {
				_, _ = fmt.Sscanf(match[2], "%d", &node)
				_, _ = fmt.Sscanf(match[3], "%d", &edge)
				return file, node, edge, true
			}
		}
	}
	return 0, 0, 0, false
}

func isCRGBusyLockedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database locked") ||
		strings.Contains(msg, "sql: database is locked") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "locked")
}

func isCRGUnbuiltError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "missing") ||
		strings.Contains(msg, "not found")
}

func classifyCRGRunError(op string, err error, out []byte) error {
	if isCRGBusyLockedError(err) {
		return fmt.Errorf("%s blocked: code graph database is busy or locked: %w", op, err)
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("crg %s failed: %s", op, msg)
}

func readCRGLanguages(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT COALESCE(language, '') FROM nodes WHERE COALESCE(language, '') != '' ORDER BY COALESCE(language, '')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var languages []string
	for rows.Next() {
		var lang string
		if err := rows.Scan(&lang); err != nil {
			return nil, err
		}
		if lang != "" {
			languages = append(languages, lang)
		}
	}
	return languages, rows.Err()
}

func normalizeCRGUpdatedAt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "never"
	}
	if strings.Contains(raw, "T") {
		return raw
	}
	if sec, err := strconv.ParseFloat(raw, 64); err == nil && sec > 0 {
		whole, frac := math.Modf(sec)
		nanos := int64(frac * 1e9)
		return time.Unix(int64(whole), nanos).UTC().Format(time.RFC3339)
	}
	return raw
}

// ── Change detection ──────────────────────────────────────────────────────────

// CRGChangeReport is the JSON output of `code-review-graph detect-changes`.
type CRGChangeReport struct {
	Summary          string           `json:"summary"`
	RiskScore        float64          `json:"risk_score"`
	ChangedFunctions []CRGChangedNode `json:"changed_functions"`
	AffectedFlows    []CRGFlow        `json:"affected_flows"`
	TestGaps         []CRGTestGap     `json:"test_gaps"`
	ReviewPriorities []CRGPriority    `json:"review_priorities"`
}

// CRGChangedNode represents a function or class that changed.
type CRGChangedNode struct {
	Name          string  `json:"name"`
	QualifiedName string  `json:"qualified_name"`
	FilePath      string  `json:"file_path"`
	RiskScore     float64 `json:"risk_score"`
	Callers       int     `json:"callers"`
}

// CRGFlow is a data-flow path affected by the change.
type CRGFlow struct {
	ID          int64  `json:"id"`
	EntryPoint  string `json:"entry_point"`
	Description string `json:"description"`
}

// CRGTestGap is a changed symbol lacking test coverage.
type CRGTestGap struct {
	QualifiedName string `json:"qualified_name"`
	FilePath      string `json:"file_path"`
}

// CRGPriority is a review priority item.
type CRGPriority struct {
	QualifiedName string  `json:"qualified_name"`
	Reason        string  `json:"reason"`
	RiskScore     float64 `json:"risk_score"`
}

// DetectChangesOptions configures a change-detection run.
type DetectChangesOptions struct {
	Base  string
	Brief bool
	// Files is an optional list of repo-relative file paths to restrict change
	// detection to. NOTE: the CRG CLI detect-changes subcommand does not accept
	// a --files argument in v1.x; this field is reserved for a future CRG
	// version that supports per-file scoping. When set, the caller must fall
	// back to using Files only for impact-radius queries (warm store) and accept
	// that changed_functions will reflect the default HEAD~1 diff.
	Files []string
}

// ── Impact radius ─────────────────────────────────────────────────────────────

// ImpactOptions configures a blast-radius query.
type ImpactOptions struct {
	// ChangedFiles is the list of repo-relative or absolute file paths to analyze.
	// If empty, the current git diff (HEAD~1) is used.
	ChangedFiles []string
	MaxDepth     int
	MaxResults   int
	Base         string
}

// CRGImpactResult is the structured output of an impact-radius query via CRG.
type CRGImpactResult struct {
	Status        string       `json:"status"`
	Summary       string       `json:"summary"`
	ChangedFiles  []string     `json:"changed_files"`
	ChangedNodes  []ImpactNode `json:"changed_nodes"`
	ImpactedNodes []ImpactNode `json:"impacted_nodes"`
	ImpactedFiles []string     `json:"impacted_files"`
	Truncated     bool         `json:"truncated"`
	TotalImpacted int          `json:"total_impacted"`
}

// ImpactNode is one node in an impact result.
type ImpactNode struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	FilePath      string `json:"file_path"`
	LineStart     int    `json:"line_start"`
	LineEnd       int    `json:"line_end"`
	Language      string `json:"language"`
	IsTest        bool   `json:"is_test"`
}

// GetImpactRadius returns the blast-radius for the given files (or current diff).
func (b *CRGBridge) GetImpactRadius(opts ImpactOptions) (*CRGImpactResult, error) {
	// Uniform hard bounds (Path A): the CRG bridge clamps MaxDepth /
	// MaxResults through the SAME provider caps the native BFS uses
	// (bounds.go) so a blast-radius query has identical ceilings whether
	// it ran in-process or in the Python subprocess. Caller values are a
	// requested ceiling only.
	maxDepth, maxResults := normalizeTraversalBounds(opts.MaxDepth, opts.MaxResults)

	// A JSON array of strings is also a valid Python list literal, so
	// marshalling is a single correct escaper for both grammars. Hand-rolling
	// fmt.Sprintf("%q") relied on Go and Python string-escape rules agreeing.
	filesJSON := "None"
	if len(opts.ChangedFiles) > 0 {
		encoded, err := json.Marshal(opts.ChangedFiles)
		if err != nil {
			return nil, fmt.Errorf("graphstore: encode changed files: %w", err)
		}
		filesJSON = string(encoded)
	}

	base := opts.Base
	if base == "" {
		base = "HEAD~1"
	}

	pyExpr := fmt.Sprintf(`
from code_review_graph.tools.query import get_impact_radius
result = get_impact_radius(
    changed_files=%s,
    max_depth=%d,
    max_results=%d,
    repo_root=repo_root,
    base=%q,
)
print(json.dumps(result))
`, filesJSON, maxDepth, maxResults, base)

	out, err := b.runPyQuery(pyExpr)
	if err != nil {
		return nil, err
	}
	var result CRGImpactResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		return nil, fmt.Errorf("parse impact result: %w", err)
	}
	return &result, nil
}

// ── Flows ─────────────────────────────────────────────────────────────────────

// FlowsResult is the output of list_flows.
type FlowsResult struct {
	Status  string     `json:"status"`
	Summary string     `json:"summary"`
	Flows   []FlowInfo `json:"flows"`
}

// FlowInfo is one execution flow entry.
type FlowInfo struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	EntryPoint  string  `json:"entry_point"`
	StepCount   int     `json:"step_count"`
	Criticality float64 `json:"criticality"`
	Kind        string  `json:"kind"`
}

// ListFlows returns the top execution flows detected in the graph.
func (b *CRGBridge) ListFlows(limit int, sortBy string) (*FlowsResult, error) {
	if limit == 0 {
		limit = 20
	}
	if sortBy == "" {
		sortBy = "criticality"
	}
	pyExpr := fmt.Sprintf(`
from code_review_graph.tools.flows_tools import list_flows
result = list_flows(repo_root=repo_root, sort_by=%q, limit=%d)
print(json.dumps(result))
`, sortBy, limit)

	out, err := b.runPyQuery(pyExpr)
	if err != nil {
		return nil, err
	}
	var result FlowsResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		return nil, fmt.Errorf("parse flows result: %w", err)
	}
	return &result, nil
}

// ── Communities ───────────────────────────────────────────────────────────────

// CommunitiesResult is the output of list_communities.
type CommunitiesResult struct {
	Status      string          `json:"status"`
	Summary     string          `json:"summary"`
	Communities []CommunityInfo `json:"communities"`
}

// CommunityInfo is one code community.
type CommunityInfo struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Size             int      `json:"size"`
	Cohesion         float64  `json:"cohesion"`
	DominantLanguage string   `json:"dominant_language"`
	Description      string   `json:"description"`
	Members          []string `json:"members"`
}

// ListCommunities returns detected code communities.
func (b *CRGBridge) ListCommunities(minSize int, sortBy string) (*CommunitiesResult, error) {
	if sortBy == "" {
		sortBy = "size"
	}
	pyExpr := fmt.Sprintf(`
from code_review_graph.tools.community_tools import list_communities_func
result = list_communities_func(repo_root=repo_root, sort_by=%q, min_size=%d)
print(json.dumps(result))
`, sortBy, minSize)

	out, err := b.runPyQuery(pyExpr)
	if err != nil {
		return nil, err
	}
	var result CommunitiesResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &result); err != nil {
		return nil, fmt.Errorf("parse communities result: %w", err)
	}
	return &result, nil
}

// ── Postprocess ───────────────────────────────────────────────────────────────

// PostprocessOptions controls which post-processing steps to run.
type PostprocessOptions struct {
	NoFlows       bool
	NoCommunities bool
	NoFTS         bool
}

// Postprocess runs flows/communities/FTS rebuilding via `code-review-graph postprocess`.
func (b *CRGBridge) Postprocess(opts PostprocessOptions) error {
	return b.runStreamed(postprocessArgs(b.RepoRoot, opts)...)
}

// DetectChanges returns the change-impact report for the current diff.
//
// When opts.Brief is true the CRG CLI emits human-readable text rather than
// JSON.  In that case we populate only CRGChangeReport.Summary with the raw
// text and leave structured fields empty.
func (b *CRGBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	args := []string{"detect-changes", crgFlagRepo, b.RepoRoot}
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	if opts.Brief {
		args = append(args, "--brief")
	}
	out, err := b.run(args...)
	if err != nil {
		return nil, err
	}

	// brief mode → plain text, not JSON
	if opts.Brief {
		return &CRGChangeReport{Summary: strings.TrimSpace(string(out))}, nil
	}

	// full mode → JSON, possibly prefixed with INFO: log lines
	var report CRGChangeReport
	if err := unmarshalSkippingLogPrefix(out, &report); err != nil {
		return nil, fmt.Errorf("parse detect-changes output: %w (raw: %s)", err, string(out))
	}
	return &report, nil
}

func unmarshalSkippingLogPrefix(out []byte, v any) error {
	trimmed := bytes.TrimSpace(out)
	if err := json.Unmarshal(trimmed, v); err == nil {
		return nil
	}
	lines := strings.Split(string(trimmed), "\n")
	var jsonLines []string
	inJSON := false
	for _, l := range lines {
		if !inJSON && strings.HasPrefix(strings.TrimSpace(l), "{") {
			inJSON = true
		}
		if inJSON {
			jsonLines = append(jsonLines, l)
		}
	}
	return json.Unmarshal([]byte(strings.Join(jsonLines, "\n")), v)
}

// ── Direct CRG database access ────────────────────────────────────────────────

// CRGDBPath returns the path to the CRG SQLite database for repoRoot.
func CRGDBPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".code-review-graph", "graph.db")
}

// ReadNodes reads up to limit nodes directly from the CRG SQLite database.
// If limit <= 0, ALL nodes are returned.
//
// CONTRACT-PRESSURE (Path A, gcc2): ReadNodes/ReadEdges are bulk *export*
// operations — the warm-link sync (commands/kg/sync_code_warm_link.go)
// calls ReadNodes(0)/ReadEdges(0) to mirror the ENTIRE CRG graph into the
// warm store. The published contract's "hard uniform cap, 0 = default"
// bound model fits user-facing bounded queries (SearchNodes,
// GetImpactRadius) but NOT a full-graph mirror: clamping 0 -> a default
// limit would silently truncate the sync on any repo with more rows than
// the cap. So Path A intentionally does NOT apply the search-limit clamp
// here; it only adds the provider-owned request timeout (still a valid,
// uniform guarantee). The contract is left UNCHANGED and this divergence
// is flagged for the spec/gcc3 to resolve (e.g. exempt bulk export, or
// give it a streaming/paged contract) rather than silently bent.
func (b *CRGBridge) ReadNodes(limit int) ([]GraphNode, error) {
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no CRG db — not an error
		}
		return nil, fmt.Errorf("stat CRG db: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+crgReadOnlyPragma)
	if err != nil {
		return nil, fmt.Errorf("open CRG db: %w", err)
	}
	defer db.Close()

	// Provider-owned request timeout (CONTRACT.md guarantee #2) — applies
	// even to the export path so a wedged read cannot hang the process.
	ctx, cancel := requestContext(nil)
	defer cancel()

	q := `SELECT id,kind,name,qualified_name,file_path,
	             COALESCE(line_start,0),COALESCE(line_end,0),
	             COALESCE(language,''),COALESCE(parent_name,''),
	             COALESCE(params,''),COALESCE(return_type,''),
	             COALESCE(is_test,0),COALESCE(file_hash,''),
	             COALESCE(extra,'{}'),updated_at
	      FROM nodes`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query CRG nodes: %w", err)
	}
	defer rows.Close()

	var nodes []GraphNode
	var scanErrs int
	for rows.Next() {
		var n GraphNode
		var extraStr string
		var isTest int
		if err := rows.Scan(&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
			&n.Params, &n.ReturnType, &isTest, &n.FileHash,
			&extraStr, &n.UpdatedAt); err != nil {
			scanErrs++
			continue
		}
		n.IsTest = isTest != 0
		_ = json.Unmarshal([]byte(extraStr), &n.Extra)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nodes, fmt.Errorf("iterate CRG nodes: %w", err)
	}
	if scanErrs > 0 {
		return nodes, fmt.Errorf("crg: %d node row(s) failed to scan", scanErrs)
	}
	return nodes, nil
}

// ReadEdges reads up to limit edges directly from the CRG SQLite database.
// If limit <= 0, ALL edges are returned. Like ReadNodes this is a bulk
// export path: the search-limit clamp is intentionally NOT applied (see
// the CONTRACT-PRESSURE note on ReadNodes); only the provider request
// timeout is added. The contract is left unchanged and the divergence is
// flagged for the spec/gcc3.
func (b *CRGBridge) ReadEdges(limit int) ([]GraphEdge, error) {
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no CRG db — not an error
		}
		return nil, fmt.Errorf("stat CRG db: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+crgReadOnlyPragma)
	if err != nil {
		return nil, fmt.Errorf("open CRG db: %w", err)
	}
	defer db.Close()

	ctx, cancel := requestContext(nil)
	defer cancel()

	q := `SELECT id,kind,source_qualified,target_qualified,
	             COALESCE(file_path,''),COALESCE(line,0),
	             COALESCE(extra,'{}'),updated_at
	      FROM edges`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query CRG edges: %w", err)
	}
	defer rows.Close()

	var edges []GraphEdge
	var scanErrs int
	for rows.Next() {
		var e GraphEdge
		var extraStr string
		if err := rows.Scan(&e.ID, &e.Kind, &e.SourceQualified, &e.TargetQualified,
			&e.FilePath, &e.Line, &extraStr, &e.UpdatedAt); err != nil {
			scanErrs++
			continue
		}
		_ = json.Unmarshal([]byte(extraStr), &e.Extra)
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return edges, fmt.Errorf("iterate CRG edges: %w", err)
	}
	if scanErrs > 0 {
		return edges, fmt.Errorf("crg: %d edge row(s) failed to scan", scanErrs)
	}
	return edges, nil
}
