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

	_ "modernc.org/sqlite"
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

// CRGOperationReport captures a build/update outcome for CLI callers.
type CRGOperationReport struct {
	Operation    string     `json:"operation"`
	Outcome      string     `json:"outcome"`
	Summary      string     `json:"summary"`
	ChangedFiles []string   `json:"changed_files,omitempty"`
	Status       *CRGStatus `json:"status,omitempty"`
	RawOutput    string     `json:"raw_output,omitempty"`
}

// BuildReport triggers a full graph rebuild and returns a structured summary.
func (b *CRGBridge) BuildReport(opts BuildOptions) (*CRGOperationReport, error) {
	args := []string{"build", "--repo", b.RepoRoot}
	if opts.SkipFlows {
		args = append(args, "--skip-flows")
	}
	if opts.SkipPostprocess {
		args = append(args, "--skip-postprocess")
	}
	out, err := b.runCaptured(args...)
	if err != nil {
		return nil, classifyCRGRunError("build", err, out)
	}
	status, statusErr := b.Status()
	if statusErr != nil {
		return nil, statusErr
	}

	report := &CRGOperationReport{
		Operation: "build",
		Status:    status,
		RawOutput: strings.TrimSpace(string(out)),
	}
	switch {
	case status.Ready:
		report.Outcome = string(CRGReadinessReady)
		report.Summary = fmt.Sprintf("Build complete: %d nodes, %d edges, %d files", status.Nodes, status.Edges, status.Files)
	case status.State == string(CRGReadinessUnbuilt):
		report.Outcome = string(CRGReadinessUnbuilt)
		report.Summary = "Build completed but the code graph is still unbuilt."
	case status.State == string(CRGReadinessBusyOrLocked):
		report.Outcome = string(CRGReadinessBusyOrLocked)
		report.Summary = "Build completed, but the code graph is busy or locked."
	default:
		report.Outcome = string(CRGReadinessError)
		report.Summary = status.Message
		if report.Summary == "" {
			report.Summary = "Build completed, but code graph status could not be determined."
		}
	}
	if report.RawOutput != "" && report.Summary == "" {
		report.Summary = report.RawOutput
	}
	return report, nil
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

	db, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(true)")
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
		if isCRGBusyLockedError(err) {
			status.State = string(CRGReadinessBusyOrLocked)
			status.Message = err.Error()
			return status, nil
		}
		if isCRGUnbuiltError(err) {
			status.Message = err.Error()
			return status, nil
		}
		status.State = string(CRGReadinessError)
		status.Message = err.Error()
		return status, nil
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM edges`).Scan(&edges); err != nil {
		if isCRGBusyLockedError(err) {
			status.State = string(CRGReadinessBusyOrLocked)
			status.Message = err.Error()
			return status, nil
		}
		if isCRGUnbuiltError(err) {
			status.Message = err.Error()
			return status, nil
		}
		status.State = string(CRGReadinessError)
		status.Message = err.Error()
		return status, nil
	}

	languages, langErr := readCRGLanguages(db)
	if langErr != nil {
		if isCRGBusyLockedError(langErr) {
			status.State = string(CRGReadinessBusyOrLocked)
			status.Message = langErr.Error()
			return status, nil
		}
		if isCRGUnbuiltError(langErr) {
			status.Message = langErr.Error()
			return status, nil
		}
		status.State = string(CRGReadinessError)
		status.Message = langErr.Error()
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
		return status, nil
	}

	status.State = string(CRGReadinessUnbuilt)
	if status.Message == "" {
		status.Message = "code graph has not been built yet"
	}
	return status, nil
}

const (
	CRGReadinessUnbuilt      = "unbuilt"
	CRGReadinessReady        = "ready"
	CRGReadinessBusyOrLocked = "busy_or_locked"
	CRGReadinessError        = "error"
)

func (s *CRGStatus) readyOrDefault() bool {
	return s != nil && s.State == CRGReadinessReady
}

func (b *CRGBridge) runCaptured(args ...string) ([]byte, error) {
	cmd := exec.Command(b.Bin, args...)
	cmd.Dir = b.RepoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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

func (b *CRGBridge) gitChangedFiles(base string) ([]string, error) {
	if base == "" {
		base = "HEAD~1"
	}
	cmd := exec.Command("git", "-C", b.RepoRoot, "diff", "--name-only", "--diff-filter=ACMRTUXB", base+"...HEAD")
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

func parseCRGMutationSummary(out []byte) (filesUpdated, nodesChanged, edgesChanged int, ok bool) {
	text := string(out)
	re := strings.NewReplacer("\r", "\n")
	text = re.Replace(text)
	summaryRe := regexp.MustCompile(`(?i)(\d+)\s+files?(?:\s+updated)?[^0-9]+(\d+)\s+nodes?[^0-9]+(\d+)\s+edges?`)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "INFO:") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		if match := summaryRe.FindStringSubmatch(line); len(match) == 4 {
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

// ── Direct CRG database access ────────────────────────────────────────────────

// CRGDBPath returns the path to the CRG SQLite database for repoRoot.
func CRGDBPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".code-review-graph", "graph.db")
}

// ReadNodes reads up to limit nodes directly from the CRG SQLite database.
// If limit <= 0, all nodes are returned. Returns an empty slice if the
// database does not exist or has no nodes.
func (b *CRGBridge) ReadNodes(limit int) ([]GraphNode, error) {
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil // no CRG db — not an error
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("open CRG db: %w", err)
	}
	defer db.Close()

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
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query CRG nodes: %w", err)
	}
	defer rows.Close()

	var nodes []GraphNode
	for rows.Next() {
		var n GraphNode
		var extraStr string
		var isTest int
		if err := rows.Scan(&n.ID, &n.Kind, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.LineStart, &n.LineEnd, &n.Language, &n.ParentName,
			&n.Params, &n.ReturnType, &isTest, &n.FileHash,
			&extraStr, &n.UpdatedAt); err != nil {
			continue
		}
		n.IsTest = isTest != 0
		_ = json.Unmarshal([]byte(extraStr), &n.Extra)
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// ReadEdges reads up to limit edges directly from the CRG SQLite database.
// If limit <= 0, all edges are returned.
func (b *CRGBridge) ReadEdges(limit int) ([]GraphEdge, error) {
	dbPath := CRGDBPath(b.RepoRoot)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=query_only(true)")
	if err != nil {
		return nil, fmt.Errorf("open CRG db: %w", err)
	}
	defer db.Close()

	q := `SELECT id,kind,source_qualified,target_qualified,
	             COALESCE(file_path,''),COALESCE(line,0),
	             COALESCE(extra,'{}'),updated_at
	      FROM edges`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("query CRG edges: %w", err)
	}
	defer rows.Close()

	var edges []GraphEdge
	for rows.Next() {
		var e GraphEdge
		var extraStr string
		if err := rows.Scan(&e.ID, &e.Kind, &e.SourceQualified, &e.TargetQualified,
			&e.FilePath, &e.Line, &extraStr, &e.UpdatedAt); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(extraStr), &e.Extra)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}
