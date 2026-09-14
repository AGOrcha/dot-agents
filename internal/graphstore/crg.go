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
	"strconv"
	"strings"
	"time"

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

// ── Build / Update ────────────────────────────────────────────────────────────

// Post-processing levels. These are upstream v2.3.8's three levels verbatim
// (code_review_graph/tools/build.py::_run_postprocess). The `build` and
// `update` CLI commands derive the level from --skip-postprocess (none) and
// --skip-flows (minimal); the MCP tool takes it directly.
const (
	// PostprocessFull resolves call targets, then computes signatures, the
	// FTS index, flows, communities and the pre-computed summary tables.
	// Upstream's default.
	PostprocessFull = "full"
	// PostprocessMinimal resolves call targets, then computes signatures
	// and the FTS index only — search keeps working, the expensive derived
	// views are skipped.
	PostprocessMinimal = "minimal"
	// PostprocessNone skips every post-build step (raw parse only).
	PostprocessNone = "none"
)

// NormalizePostprocessLevel maps an unset level onto upstream's default. The
// option structs stay plain data — a caller binding from the MCP release
// schema passes the level explicitly, and a Go caller that leaves it empty
// gets upstream's documented default instead of a silent "none".
func NormalizePostprocessLevel(level string) string {
	if level == "" {
		return PostprocessFull
	}
	return level
}

// The literal strings upstream's result dicts carry.
const (
	// statusOK is the `status` field of every successful result.
	statusOK = "ok"
	// crgBuildTypeFull / crgBuildTypeIncremental are the two `build_type`
	// values.
	crgBuildTypeFull        = "full"
	crgBuildTypeIncremental = "incremental"
	// crgNoChangesSummary is the summary of an incremental run that found
	// nothing to re-parse.
	crgNoChangesSummary = "No changes detected. Graph is up to date."
	// crgPostprocessSummary is the summary of a standalone post-process.
	crgPostprocessSummary = "Post-processing complete."
)

// FullBuildSummary is upstream's full-build summary sentence.
func FullBuildSummary(filesParsed, nodes, edges int) string {
	return fmt.Sprintf(
		"Full build complete: parsed %d files, created %d nodes and %d edges.",
		filesParsed, nodes, edges)
}

// IncrementalSummary is upstream's incremental-update summary sentence. The
// two file lists are rendered as Python list literals because that is
// literally what upstream interpolates, and the string is part of the
// contract rather than prose we are free to tidy.
func IncrementalSummary(filesUpdated, nodes, edges int, changed, dependents []string) string {
	return fmt.Sprintf(
		"Incremental update: %d files re-parsed, %d nodes and %d edges updated. Changed: %s. Dependents also updated: %s.",
		filesUpdated, nodes, edges, pythonListLiteral(changed), pythonListLiteral(dependents))
}

// NoChangesSummary is upstream's "nothing to do" summary sentence.
func NoChangesSummary() string { return crgNoChangesSummary }

// PostprocessSummary is upstream's standalone post-process summary sentence.
func PostprocessSummary() string { return crgPostprocessSummary }

// pythonListLiteral renders a string slice the way Python's repr does, which
// is what upstream's f-string interpolation produces.
func pythonListLiteral(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, "'"+strings.ReplaceAll(item, "'", `\'`)+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// BuildOptions configures a full graph build: upstream's
// build_or_update_graph(full_rebuild=True, ...) argument set.
type BuildOptions struct {
	// RepoRoot overrides the repository root. Empty means the root the
	// provider was constructed with (upstream auto-detects it from cwd).
	RepoRoot string
	// Postprocess is the post-build level: PostprocessFull (the default
	// for an empty value), PostprocessMinimal or PostprocessNone.
	Postprocess string
	// RecurseSubmodules is upstream's tri-state: nil defers to the
	// CRG_RECURSE_SUBMODULES environment variable, non-nil forces
	// `git ls-files --recurse-submodules` on or off.
	RecurseSubmodules *bool
	// EmbeddingProvider and EmbeddingModel request an explicit post-build
	// embedding refresh. Upstream requires BOTH: supplying one alone is a
	// warning on the report, not an error.
	EmbeddingProvider string
	EmbeddingModel    string
}

// UpdateOptions configures an incremental graph update: upstream's
// build_or_update_graph(full_rebuild=False, ...) argument set.
type UpdateOptions struct {
	// FullRebuild forces the full-rebuild path. Upstream also sets it
	// implicitly when the graph holds no nodes, or when no diff base
	// resolves, in which case the report's build_type reads "full".
	FullRebuild bool
	// RepoRoot overrides the repository root (see BuildOptions.RepoRoot).
	RepoRoot string
	// Base is the git ref to diff against. Empty means "resolve
	// automatically" — the commit the graph was last built at — which is
	// upstream's default. It is NOT HEAD~1: a fixed HEAD~1 silently misses
	// work that arrived through a multi-commit pull, rebase or branch
	// switch.
	Base string
	// BaseSet records that Base was supplied EXPLICITLY, even as the empty
	// string. Upstream distinguishes an absent base (resolve it) from an
	// empty one (a ref that matches nothing, so the diff is empty and
	// base_resolved echoes ""), and only the absent form resolves or falls
	// back to a full rebuild. Callers that never send an explicit empty
	// base leave this false.
	BaseSet bool
	// Postprocess is the post-update level (see BuildOptions.Postprocess).
	Postprocess string
	// RecurseSubmodules is upstream's tri-state (see BuildOptions).
	RecurseSubmodules *bool
	// EmbeddingProvider and EmbeddingModel (see BuildOptions).
	EmbeddingProvider string
	EmbeddingModel    string
}

// BuildErrorRow is one entry of a build or update result's `errors` list.
type BuildErrorRow struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// NullableString is a string with three JSON states: the key is absent (a nil
// *NullableString), the key is present and null, or the key carries a value.
// Upstream's `base_resolved` needs all three — a full build reports null, an
// incremental reports the resolved ref, and a standalone post-process omits
// the key entirely.
type NullableString struct {
	Value string
	Valid bool
}

// NullString is the present-and-null form.
func NullString() *NullableString { return &NullableString{} }

// SomeString is the present-with-a-value form.
func SomeString(v string) *NullableString { return &NullableString{Value: v, Valid: true} }

// MarshalJSON emits null for the invalid form.
func (n NullableString) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Value)
}

// UnmarshalJSON accepts null and a string.
func (n *NullableString) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		*n = NullableString{}
		return nil
	}
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*n = NullableString{Value: v, Valid: true}
	return nil
}

// PostprocessTiming is upstream's per-stage duration block, in seconds. The
// minimal level reports SignaturesS and FTSS only; the full level adds the
// three expensive stages. Every duration is non-negative.
type PostprocessTiming struct {
	SignaturesS  float64  `json:"signatures_s"`
	FTSS         float64  `json:"fts_s"`
	FlowsS       *float64 `json:"flows_s,omitempty"`
	CommunitiesS *float64 `json:"communities_s,omitempty"`
	SummariesS   *float64 `json:"summaries_s,omitempty"`
}

// The seven per-language resolver blocks upstream attaches to a build or
// update result. The kg-native backend parses Go only, so it reports the
// upstream-shaped zero for each rather than inventing a number; the blocks
// still matter because their PRESENCE is what distinguishes upstream's
// incremental early return from its normal path.
type (
	// PythonResolution is `python_resolution`.
	PythonResolution struct {
		FilesIndexed     int `json:"files_indexed"`
		ImportsAmbiguous int `json:"imports_ambiguous"`
		ImportsResolved  int `json:"imports_resolved"`
		ImportsUpdated   int `json:"imports_updated"`
	}
	// ReScriptResolution is `rescript_resolution`.
	ReScriptResolution struct {
		CallsResolved   int `json:"calls_resolved"`
		FilesIndexed    int `json:"files_indexed"`
		ImportsResolved int `json:"imports_resolved"`
	}
	// SpringResolution is `spring_resolution`.
	SpringResolution struct {
		CallsResolved int `json:"calls_resolved"`
		FilesIndexed  int `json:"files_indexed"`
	}
	// EventResolution is `event_resolution`.
	EventResolution struct {
		CallsEmitted      int `json:"calls_emitted"`
		EventsIndexed     int `json:"events_indexed"`
		StaleCallsRemoved int `json:"stale_calls_removed"`
	}
	// TemporalResolution is `temporal_resolution`.
	TemporalResolution struct {
		CallsResolved int `json:"calls_resolved"`
		FilesIndexed  int `json:"files_indexed"`
	}
	// HCLResolution is `hcl_resolution`.
	HCLResolution struct {
		FilesIndexed       int `json:"files_indexed"`
		ImportsResolved    int `json:"imports_resolved"`
		ReferencesResolved int `json:"references_resolved"`
	}
	// ScopedResolution is `scoped_resolution`.
	ScopedResolution struct {
		CallsResolved int `json:"calls_resolved"`
		FilesIndexed  int `json:"files_indexed"`
	}
)

// ResolverStats groups the seven blocks so their presence can be controlled
// as one unit. Embedded as a nil pointer on a report, encoding/json omits all
// seven keys — the shape of upstream's incremental early return, which never
// reaches the resolvers. A nil member emits JSON null: the key is present but
// that resolver did not run, which is what an incremental update reports for
// every language whose files did not change.
type ResolverStats struct {
	Python   *PythonResolution   `json:"python_resolution"`
	ReScript *ReScriptResolution `json:"rescript_resolution"`
	Spring   *SpringResolution   `json:"spring_resolution"`
	Event    *EventResolution    `json:"event_resolution"`
	Temporal *TemporalResolution `json:"temporal_resolution"`
	HCL      *HCLResolution      `json:"hcl_resolution"`
	Scoped   *ScopedResolution   `json:"scoped_resolution"`
}

// ZeroResolverStats is the all-present, all-zero block a full build reports:
// upstream runs every resolver unconditionally on a full build, and each one
// reports zeroes when the graph holds no file of its language.
func ZeroResolverStats() *ResolverStats {
	return &ResolverStats{
		Python:   &PythonResolution{},
		ReScript: &ReScriptResolution{},
		Spring:   &SpringResolution{},
		Event:    &EventResolution{},
		Temporal: &TemporalResolution{},
		HCL:      &HCLResolution{},
		Scoped:   &ScopedResolution{},
	}
}

// NullResolverStats is the all-present, all-null block an incremental update
// reports: upstream runs a language resolver only when a file of that
// language changed, and records None for every resolver it skipped.
func NullResolverStats() *ResolverStats { return &ResolverStats{} }

// CRGOperationReport is upstream v2.3.8's build / update / post-process
// result dict. Which keys are PRESENT is part of the contract, not an
// encoding detail, so every optional field is a pointer and "nil" means
// "upstream omits this key" rather than "zero":
//
//   - a full build carries files_parsed and base_resolved: null;
//   - an incremental update carries files_updated, changed_files and
//     dependent_files, and base_resolved is the ref that was resolved;
//   - the incremental early return ("No changes detected.") carries only the
//     incremental core plus postprocess_level — no post-processing counters,
//     no timing, and no resolver blocks when the diff itself was empty;
//   - postprocess="none" carries no signature/FTS/flow/community/summary
//     counter and no timing;
//   - a standalone post-process carries status, summary, signatures_updated,
//     bare_edges_resolved, cpp_scoped_edges_resolved and only the counters of
//     the steps that were enabled — no build_type, no base_resolved, no
//     postprocess_level and no timing.
type CRGOperationReport struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`

	BuildType    string          `json:"build_type,omitempty"`
	BaseResolved *NullableString `json:"base_resolved,omitempty"`

	FilesParsed       *int             `json:"files_parsed,omitempty"`
	FilesUpdated      *int             `json:"files_updated,omitempty"`
	TotalNodes        *int             `json:"total_nodes,omitempty"`
	TotalEdges        *int             `json:"total_edges,omitempty"`
	ChangedFiles      *[]string        `json:"changed_files,omitempty"`
	DependentFiles    *[]string        `json:"dependent_files,omitempty"`
	StaleFilesRemoved *int             `json:"stale_files_removed,omitempty"`
	Errors            *[]BuildErrorRow `json:"errors,omitempty"`

	PostprocessLevel  string             `json:"postprocess_level,omitempty"`
	PostprocessTiming *PostprocessTiming `json:"postprocess_timing,omitempty"`

	SignaturesUpdated      *bool `json:"signatures_updated,omitempty"`
	FTSIndexed             *int  `json:"fts_indexed,omitempty"`
	FTSRebuilt             *bool `json:"fts_rebuilt,omitempty"`
	FlowsDetected          *int  `json:"flows_detected,omitempty"`
	CommunitiesDetected    *int  `json:"communities_detected,omitempty"`
	SummariesComputed      *bool `json:"summaries_computed,omitempty"`
	BareEdgesResolved      *int  `json:"bare_edges_resolved,omitempty"`
	CPPScopedEdgesResolved *int  `json:"cpp_scoped_edges_resolved,omitempty"`

	EmbeddingsRefreshed *int     `json:"embeddings_refreshed,omitempty"`
	EmbeddingsPurged    *int     `json:"embeddings_purged,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`

	*ResolverStats
}

// EmbeddingRefreshWarning is the exact string upstream records when a caller
// supplies only one half of the provider/model pair. Upstream warns and
// carries on; it is not an error.
const EmbeddingRefreshWarning = "Embedding refresh requires both an explicit provider and model."

// EmbeddingRefreshWarnings returns the warnings an embedding-refresh request
// produces before any provider is contacted. Both halves unset is "no refresh
// requested"; exactly one set is upstream's warning. A fully specified pair
// yields no warning here — the refresh itself is what the caller cannot run.
func EmbeddingRefreshWarnings(provider, model string) []string {
	if provider == "" && model == "" {
		return nil
	}
	if provider == "" || model == "" {
		return []string{EmbeddingRefreshWarning}
	}
	return nil
}

// buildArgs assembles `code-review-graph build`'s argv from the option set,
// forwarding upstream's flags verbatim. The postprocess LEVEL is expressed
// the way the CLI expresses it: --skip-postprocess for "none", --skip-flows
// for "minimal", nothing for "full".
func (b *CRGBridge) buildArgs(command, repoRoot, level, provider, model string) []string {
	if repoRoot == "" {
		repoRoot = b.RepoRoot
	}
	args := []string{command, crgFlagRepo, repoRoot}
	switch NormalizePostprocessLevel(level) {
	case PostprocessNone:
		args = append(args, "--skip-postprocess")
	case PostprocessMinimal:
		args = append(args, "--skip-flows")
	}
	if provider != "" {
		args = append(args, "--embedding-provider", provider)
	}
	if model != "" {
		args = append(args, "--embedding-model", model)
	}
	return args
}

// BuildReport runs `code-review-graph build` and returns upstream's result
// shape. The CLI prints one machine-recognisable line per build, so the
// counters come from that line rather than from a loose scrape of the whole
// transcript; everything the CLI does not print (the derived-view counters,
// the resolver blocks) is left ABSENT rather than guessed, which is exactly
// what a rollback path should report.
func (b *CRGBridge) BuildReport(opts BuildOptions) (*CRGOperationReport, error) {
	args := b.buildArgs("build", opts.RepoRoot, opts.Postprocess, opts.EmbeddingProvider, opts.EmbeddingModel)
	out, err := b.runCaptured(args...)
	if err != nil {
		return nil, classifyCRGRunError("build", err, out)
	}
	report := parseCRGBuildOutput(out)
	report.Warnings = append(report.Warnings, EmbeddingRefreshWarnings(opts.EmbeddingProvider, opts.EmbeddingModel)...)
	return report, nil
}

// Build triggers a full graph rebuild via `code-review-graph build`.
// The structured report is intentionally discarded for legacy callers.
func (b *CRGBridge) Build(opts BuildOptions) error {
	_, err := b.BuildReport(opts)
	return err
}

// UpdateReport runs `code-review-graph update` and returns upstream's result
// shape. Unlike the previous implementation it does NOT pre-empt the CLI with
// its own git diff: upstream resolves the base itself (to the commit the
// graph was last built at) and may fall back to a full rebuild, and a caller
// that short-circuits on an empty `HEAD~1` diff would report "no changes" for
// a graph that is actually stale.
func (b *CRGBridge) UpdateReport(opts UpdateOptions) (*CRGOperationReport, error) {
	if opts.FullRebuild {
		return b.BuildReport(BuildOptions{
			RepoRoot:          opts.RepoRoot,
			Postprocess:       opts.Postprocess,
			RecurseSubmodules: opts.RecurseSubmodules,
			EmbeddingProvider: opts.EmbeddingProvider,
			EmbeddingModel:    opts.EmbeddingModel,
		})
	}
	args := b.buildArgs("update", opts.RepoRoot, opts.Postprocess, opts.EmbeddingProvider, opts.EmbeddingModel)
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	out, err := b.runCaptured(args...)
	if err != nil {
		return nil, classifyCRGRunError("update", err, out)
	}
	report := parseCRGBuildOutput(out)
	report.Warnings = append(report.Warnings, EmbeddingRefreshWarnings(opts.EmbeddingProvider, opts.EmbeddingModel)...)
	return report, nil
}

// Update triggers an incremental graph update via `code-review-graph update`.
// The structured report is intentionally discarded for legacy callers.
func (b *CRGBridge) Update(opts UpdateOptions) error {
	_, err := b.UpdateReport(opts)
	return err
}

// ── Status ────────────────────────────────────────────────────────────────────

// CRGStatus is upstream v2.3.8's `status --json` object plus dot-agents'
// readiness overlay.
//
// The upstream half is the CLI's contract verbatim, including the nullable
// fields: `built_on_branch` / `built_at_commit` are the branch and commit the
// graph was built at (metadata), `current_branch` / `current_sha` are where
// the working copy is now, and the two `svn_*` fields are populated only for
// an SVN working copy. A nil pointer is JSON null, exactly as upstream emits
// it for a value it does not have.
//
// State / Ready / Message are dot-agents' readiness overlay. They are not
// upstream fields; they exist because `da kg code-status` is a diagnostic
// that must answer for an absent, locked or corrupt graph instead of failing.
type CRGStatus struct {
	Nodes         int      `json:"nodes"`
	Edges         int      `json:"edges"`
	Files         int      `json:"files"`
	Languages     []string `json:"languages"`
	LastUpdated   *string  `json:"last_updated"`
	VCS           string   `json:"vcs"`
	BuiltOnBranch *string  `json:"built_on_branch"`
	BuiltAtCommit *string  `json:"built_at_commit"`
	CurrentBranch *string  `json:"current_branch"`
	CurrentSHA    *string  `json:"current_sha"`
	SVNBranch     *string  `json:"svn_branch"`
	SVNRevision   *string  `json:"svn_revision"`

	State   string `json:"state"`
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

// LastUpdatedOrNever renders the nullable timestamp for human output.
func (s *CRGStatus) LastUpdatedOrNever() string {
	if s == nil || s.LastUpdated == nil || strings.TrimSpace(*s.LastUpdated) == "" {
		return "never"
	}
	return *s.LastUpdated
}

// applyVCSStatus fills the VCS half of `status --json` for root: the branch
// and revision the graph was built at come from the graph's own metadata, the
// current ones from the working copy.
func applyVCSStatus(status *CRGStatus, root string, metadata func(key string) string) {
	status.VCS = DetectVCS(root)
	status.BuiltOnBranch = nonEmptyPtr(metadata("git_branch"))
	status.BuiltAtCommit = nonEmptyPtr(metadata("git_head_sha"))
	status.SVNBranch = nonEmptyPtr(metadata("svn_branch"))
	status.SVNRevision = nonEmptyPtr(metadata("svn_revision"))
	if status.VCS == VCSGit {
		branch, sha := GitBranchInfo(root)
		status.CurrentBranch, status.CurrentSHA = new(branch), new(sha)
	}
}

// nonEmptyPtr maps "" onto a JSON null, which is how upstream reports a
// metadata key that was never written.
func nonEmptyPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// Status returns the current graph stats in upstream's `status --json` shape.
// The bridge reads the SQLite database directly rather than shelling out, so
// code-status keeps working when the CRG binary is unavailable — which is the
// exact situation the rollback path has to diagnose.
func (b *CRGBridge) Status() (*CRGStatus, error) {
	status := &CRGStatus{State: CRGReadinessUnbuilt}
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		status.Message = "code-review-graph database missing"
		applyVCSStatus(status, b.RepoRoot, func(string) string { return "" })
		return status, nil
	}

	db, err := sql.Open("sqlite", dbPath+crgReadOnlyPragma)
	if err != nil {
		status.State = CRGReadinessError
		status.Message = fmt.Sprintf("open CRG db: %v", err)
		return status, nil
	}
	defer db.Close()

	// upstream's get_stats: files and languages come from the File-node
	// inventory, never from every node row, so a virtual or leftover node
	// cannot keep a language alive after its last real file left.
	var nodes, files, edges int
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE kind = 'File') FROM nodes`).Scan(&nodes, &files); err != nil {
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

	status.Nodes, status.Edges, status.Files = nodes, edges, files
	status.Languages = languages
	metadata := func(key string) string {
		var value sql.NullString
		if err := db.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&value); err != nil {
			return ""
		}
		return strings.TrimSpace(value.String)
	}
	if raw := metadata("last_updated"); raw != "" {
		status.LastUpdated = new(normalizeCRGUpdatedAt(raw))
	}
	applyVCSStatus(status, b.RepoRoot, metadata)
	applyCRGReadiness(status)
	return status, nil
}

// applyCRGReadiness derives the readiness overlay from the counts. A graph
// with nodes, files and a build timestamp is ready; anything else is unbuilt.
func applyCRGReadiness(status *CRGStatus) {
	if status.Nodes > 0 && status.Files > 0 && status.LastUpdated != nil {
		status.State = CRGReadinessReady
		status.Ready = true
		return
	}
	status.State = CRGReadinessUnbuilt
	if status.Message == "" {
		status.Message = "code graph has not been built yet"
	}
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

// The three lines `code-review-graph build` / `update` prints on success.
// They are fixed format strings in upstream's cli.py, so matching them
// exactly recovers the counters the CLI does expose without guessing at
// prose, and a format change in a future release fails loudly (the report
// loses its counters) instead of silently yielding a wrong number.
var (
	crgFullBuildLineRE   = regexp.MustCompile(`^Full build: (\d+) files, (\d+) nodes, (\d+) edges \(postprocess=(\w+)\)$`)
	crgFullRebuildLineRE = regexp.MustCompile(`^Full rebuild \(no usable incremental base\): (\d+) files, (\d+) nodes, (\d+) edges \(postprocess=(\w+)\)$`)
	crgIncrementalLineRE = regexp.MustCompile(`^Incremental: (\d+) files updated, (\d+) nodes, (\d+) edges \(postprocess=(\w+)\)$`)
	crgErrorsLineRE      = regexp.MustCompile(`^Errors: (\d+)$`)
)

// parseCRGBuildOutput turns a build/update transcript into upstream's result
// shape. Only the fields the CLI actually prints are populated; the derived
// view counters and resolver blocks stay ABSENT, because the CLI never emits
// them and a rollback path must not invent them.
func parseCRGBuildOutput(out []byte) *CRGOperationReport {
	report := &CRGOperationReport{Status: statusOK}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case crgFullBuildLineRE.MatchString(line):
			applyCRGBuildLine(report, crgFullBuildLineRE.FindStringSubmatch(line), true)
		case crgFullRebuildLineRE.MatchString(line):
			applyCRGBuildLine(report, crgFullRebuildLineRE.FindStringSubmatch(line), true)
		case crgIncrementalLineRE.MatchString(line):
			applyCRGBuildLine(report, crgIncrementalLineRE.FindStringSubmatch(line), false)
		case crgErrorsLineRE.MatchString(line):
			report.Errors = new(make([]BuildErrorRow, atoiOrZero(crgErrorsLineRE.FindStringSubmatch(line)[1])))
		}
	}
	if report.Summary == "" {
		report.Summary = strings.TrimSpace(string(out))
	}
	return report
}

// applyCRGBuildLine records one parsed CLI line. The summary is upstream's
// own sentence whenever the parsed counters fully determine it: a full build
// always does, and an incremental update does when nothing changed. An
// incremental update that DID change something needs the changed and
// dependent file lists the CLI does not print, so the CLI's own line stands
// in rather than a fabricated one.
func applyCRGBuildLine(report *CRGOperationReport, match []string, full bool) {
	files, nodes, edges := atoiOrZero(match[1]), atoiOrZero(match[2]), atoiOrZero(match[3])
	report.TotalNodes, report.TotalEdges = new(nodes), new(edges)
	report.PostprocessLevel = match[4]
	if full {
		report.BuildType, report.BaseResolved = crgBuildTypeFull, NullString()
		report.FilesParsed = new(files)
		report.Summary = FullBuildSummary(files, nodes, edges)
		return
	}
	report.BuildType = crgBuildTypeIncremental
	report.FilesUpdated = new(files)
	if files == 0 {
		report.Summary = crgNoChangesSummary
		return
	}
	report.Summary = match[0]
}

// atoiOrZero parses a capture group that the regexp already proved is digits.
func atoiOrZero(s string) int {
	n, _ := strconv.Atoi(s)
	return n
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

// readCRGLanguages returns the language inventory `status --json` reports:
// upstream derives it from the live File-node rows, never from every node,
// so a virtual or leftover row cannot keep a language alive after its last
// real file left the graph. The result is never nil — upstream emits an
// empty JSON list, not null.
func readCRGLanguages(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT language FROM nodes WHERE kind = 'File' AND language IS NOT NULL AND language != '' ORDER BY language`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	languages := []string{}
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

// normalizeCRGUpdatedAt renders a stored `last_updated` value. Upstream
// writes an ISO-8601 local timestamp; older databases carry a Unix epoch
// float, which is converted rather than surfaced raw.
func normalizeCRGUpdatedAt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
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

// DetectChangesOptions configures a change-detection run. It is the union of
// upstream's `detect-changes` CLI surface (--base, --brief, --repo, --churn,
// --verify) and the one argument only the MCP tool accepts (changed_files):
// the CLI auto-detects its file set, the tool lets a caller supply it.
type DetectChangesOptions struct {
	// RepoRoot overrides the repository root (--repo).
	RepoRoot string
	// Base is the git diff base (--base). Empty means upstream's default,
	// which for detect-changes IS HEAD~1 (unlike `update`).
	Base string
	// Brief prints the risk summary + Token Savings panel instead of the
	// full JSON (--brief). Read-only against the existing graph.
	Brief bool
	// Churn adds the opt-in change-frequency term to risk scores
	// (--churn), counting commits per file over CRG_CHURN_WINDOW_DAYS.
	Churn bool
	// Verify calibrates the estimated savings against tiktoken's
	// cl100k_base tokenizer (--verify).
	Verify bool
	// Files restricts change detection to these paths. The CLI has no
	// equivalent flag — it is the MCP tool's `changed_files` argument —
	// so the bridge honours it by resolving the report itself rather than
	// passing it through.
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

// PostprocessOptions configures a standalone post-processing run: upstream's
// run_postprocess(...) argument set.
//
// The three step flags are tri-states because every one of them defaults to
// True upstream, which a Go bool cannot express: nil means "upstream's
// default", and a caller that binds from the MCP release schema sets them
// explicitly. Read them through the accessors, never directly.
type PostprocessOptions struct {
	// RepoRoot overrides the repository root (--repo).
	RepoRoot string
	// Flows runs flow detection (--no-flows disables it). Default: true.
	Flows *bool
	// Communities runs community detection (--no-communities). Default: true.
	Communities *bool
	// FTS rebuilds the full-text index (--no-fts). Default: true.
	FTS *bool
	// EmbeddingProvider and EmbeddingModel request an explicit embedding
	// refresh; upstream requires both or warns.
	EmbeddingProvider string
	EmbeddingModel    string
}

// FlowsEnabled reports whether flow detection runs.
func (o PostprocessOptions) FlowsEnabled() bool { return stepEnabled(o.Flows) }

// CommunitiesEnabled reports whether community detection runs.
func (o PostprocessOptions) CommunitiesEnabled() bool { return stepEnabled(o.Communities) }

// FTSEnabled reports whether the FTS index is rebuilt.
func (o PostprocessOptions) FTSEnabled() bool { return stepEnabled(o.FTS) }

// stepEnabled applies upstream's default (True) to an unset step flag.
func stepEnabled(flag *bool) bool { return flag == nil || *flag }

// PostprocessReport runs `code-review-graph postprocess` and returns
// upstream's result shape. The CLI prints a prose line rather than the
// result dict, so the report carries the summary and the step selection the
// caller asked for; the per-step counters stay ABSENT rather than guessed.
func (b *CRGBridge) PostprocessReport(opts PostprocessOptions) (*CRGOperationReport, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = b.RepoRoot
	}
	args := []string{"postprocess", crgFlagRepo, repoRoot}
	if !opts.FlowsEnabled() {
		args = append(args, "--no-flows")
	}
	if !opts.CommunitiesEnabled() {
		args = append(args, "--no-communities")
	}
	if !opts.FTSEnabled() {
		args = append(args, "--no-fts")
	}
	if opts.EmbeddingProvider != "" {
		args = append(args, "--embedding-provider", opts.EmbeddingProvider)
	}
	if opts.EmbeddingModel != "" {
		args = append(args, "--embedding-model", opts.EmbeddingModel)
	}
	out, err := b.runCaptured(args...)
	if err != nil {
		return nil, classifyCRGRunError("postprocess", err, out)
	}
	return &CRGOperationReport{
		Status:   statusOK,
		Summary:  PostprocessSummary(),
		Warnings: EmbeddingRefreshWarnings(opts.EmbeddingProvider, opts.EmbeddingModel),
	}, nil
}

// Postprocess runs post-processing, discarding the report.
func (b *CRGBridge) Postprocess(opts PostprocessOptions) error {
	_, err := b.PostprocessReport(opts)
	return err
}

// DetectChanges returns the change-impact report for the current diff,
// forwarding upstream's full `detect-changes` option surface.
//
// When opts.Brief is true the CRG CLI emits human-readable text rather than
// JSON. In that case we populate only CRGChangeReport.Summary with the raw
// text and leave the structured fields empty.
func (b *CRGBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = b.RepoRoot
	}
	args := []string{"detect-changes", crgFlagRepo, repoRoot}
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	if opts.Brief {
		args = append(args, "--brief")
	}
	if opts.Churn {
		args = append(args, "--churn")
	}
	if opts.Verify {
		args = append(args, "--verify")
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
