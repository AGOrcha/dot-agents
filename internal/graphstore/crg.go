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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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
//  1. VENV_PATH/.venv/bin/code-review-graph relative to repoRoot
//  2. .venv/bin/code-review-graph relative to repoRoot
//  3. code-review-graph on PATH
func DiscoverCRGBin(repoRoot string) (string, error) {
	candidates := []string{
		filepath.Join(repoRoot, ".venv", "bin", "code-review-graph"),
	}
	// also check parent dirs up to 2 levels for .venv
	parent := filepath.Dir(repoRoot)
	candidates = append(candidates,
		filepath.Join(parent, ".venv", "bin", "code-review-graph"),
	)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	// fall back to PATH
	if p, err := exec.LookPath("code-review-graph"); err == nil {
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
	// b.Bin is e.g. /path/to/.venv/bin/code-review-graph
	// Python is in the same bin/ directory.
	binDir := filepath.Dir(b.Bin)
	candidates := []string{
		filepath.Join(binDir, "python3"),
		filepath.Join(binDir, "python"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "python3"
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

	py := b.pythonBin()
	cmd := exec.Command(py, "-c", script)
	cmd.Dir = b.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
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
}

// Build triggers a full graph rebuild via `code-review-graph build`.
// Output is streamed to stdout/stderr.
func (b *CRGBridge) Build(opts BuildOptions) error {
	args := []string{"build", "--repo", b.RepoRoot}
	if opts.SkipFlows {
		args = append(args, "--skip-flows")
	}
	if opts.SkipPostprocess {
		args = append(args, "--skip-postprocess")
	}
	return b.runStreamed(args...)
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

// Update triggers an incremental graph update via `code-review-graph update`.
func (b *CRGBridge) Update(opts UpdateOptions) error {
	args := []string{"update", "--repo", b.RepoRoot}
	if opts.Base != "" {
		args = append(args, "--base", opts.Base)
	}
	if opts.SkipFlows {
		args = append(args, "--skip-flows")
	}
	if opts.SkipPostprocess {
		args = append(args, "--skip-postprocess")
	}
	return b.runStreamed(args...)
}

// ── Status ────────────────────────────────────────────────────────────────────

// CRGStatus is the parsed output of `code-review-graph status`.
type CRGStatus struct {
	Nodes       int    `json:"nodes"`
	Edges       int    `json:"edges"`
	Files       int    `json:"files"`
	Languages   string `json:"languages"`
	LastUpdated string `json:"last_updated"`
}

// Status returns the current graph stats from `code-review-graph status`.
// The CRG CLI outputs human-readable lines; we parse them to a struct.
func (b *CRGBridge) Status() (*CRGStatus, error) {
	out, err := b.run("status", "--repo", b.RepoRoot)
	if err != nil {
		return nil, err
	}
	return parseCRGStatusOutput(out), nil
}

// parseCRGStatusOutput parses the human-readable output of `crg status`.
// Expected lines (may include INFO: log lines which we skip):
//
//	Nodes: 923
//	Edges: 6281
//	Files: 50
//	Languages: go, ruby
//	Last updated: 2026-04-11T00:49:52
func parseCRGStatusOutput(out []byte) *CRGStatus {
	s := &CRGStatus{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "INFO:") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Nodes":
			s.Nodes, _ = strconv.Atoi(val)
		case "Edges":
			s.Edges, _ = strconv.Atoi(val)
		case "Files":
			s.Files, _ = strconv.Atoi(val)
		case "Languages":
			s.Languages = val
		case "Last updated":
			s.LastUpdated = val
		}
	}
	return s
}

// ── Change detection ──────────────────────────────────────────────────────────

// CRGChangeReport is the JSON output of `code-review-graph detect-changes`.
type CRGChangeReport struct {
	Summary         string             `json:"summary"`
	RiskScore       float64            `json:"risk_score"`
	ChangedFunctions []CRGChangedNode  `json:"changed_functions"`
	AffectedFlows   []CRGFlow          `json:"affected_flows"`
	TestGaps        []CRGTestGap       `json:"test_gaps"`
	ReviewPriorities []CRGPriority     `json:"review_priorities"`
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
	maxDepth := opts.MaxDepth
	if maxDepth == 0 {
		maxDepth = 2
	}
	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 50
	}

	filesJSON := "None"
	if len(opts.ChangedFiles) > 0 {
		parts := make([]string, len(opts.ChangedFiles))
		for i, f := range opts.ChangedFiles {
			parts[i] = fmt.Sprintf("%q", f)
		}
		filesJSON = "[" + strings.Join(parts, ", ") + "]"
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
	args := []string{"postprocess", "--repo", b.RepoRoot}
	if opts.NoFlows {
		args = append(args, "--no-flows")
	}
	if opts.NoCommunities {
		args = append(args, "--no-communities")
	}
	if opts.NoFTS {
		args = append(args, "--no-fts")
	}
	return b.runStreamed(args...)
}

// DetectChanges returns the change-impact report for the current diff.
//
// When opts.Brief is true the CRG CLI emits human-readable text rather than
// JSON.  In that case we populate only CRGChangeReport.Summary with the raw
// text and leave structured fields empty.
func (b *CRGBridge) DetectChanges(opts DetectChangesOptions) (*CRGChangeReport, error) {
	args := []string{"detect-changes", "--repo", b.RepoRoot}
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
	trimmed := bytes.TrimSpace(out)
	var report CRGChangeReport
	if err := json.Unmarshal(trimmed, &report); err != nil {
		// strip leading INFO/WARNING lines and retry
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
		if err2 := json.Unmarshal([]byte(strings.Join(jsonLines, "\n")), &report); err2 != nil {
			return nil, fmt.Errorf("parse detect-changes output: %w (raw: %s)", err, string(out))
		}
	}
	return &report, nil
}
