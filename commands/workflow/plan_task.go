package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

const (
	workflowTasksFileName      = "TASKS.yaml"
	workflowPlanFileName       = "PLAN.yaml"
	workflowDesignFileName     = "design.md"
	errPlanNotFoundFmt         = "plan %q not found"
	errPlanNotFoundWithCause   = "plan %q not found: %w"
	errTaskNotFoundInPlanFmt   = "task %q not found in plan %q"
	errTasksForPlanNotFoundFmt = "tasks for plan %q not found: %w"
	errParseFileFmt            = "parse %s: %w"
	planTaskSliceIDPrefix      = "slice:"
	gitFlagNameOnly            = "--name-only"
	gitSubcmdRevParse          = "rev-parse"
)

// ScopeEvidence is the Go representation of a .scope.yaml sidecar file located at
// .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml.
// All slice fields use []string{} (not nil) so JSON marshals to [] not null.
type ScopeEvidence struct {
	SchemaVersion       int                 `json:"schema_version"          yaml:"schema_version"`
	PlanID              string              `json:"plan_id"                 yaml:"plan_id"`
	TaskID              string              `json:"task_id"                 yaml:"task_id"`
	Status              string              `json:"status"                  yaml:"status"`
	Mode                string              `json:"mode,omitempty"          yaml:"mode,omitempty"`
	Goal                string              `json:"goal,omitempty"          yaml:"goal,omitempty"`
	Confidence          string              `json:"confidence"              yaml:"confidence"`
	DecisionLocks       []string            `json:"decision_locks"          yaml:"decision_locks"`
	RequiredReads       []ScopeRequiredRead `json:"required_reads"          yaml:"required_reads"`
	Seeds               *ScopeSeeds         `json:"seeds,omitempty"         yaml:"seeds,omitempty"`
	Queries             []ScopeQuery        `json:"queries"                 yaml:"queries"`
	RequiredPaths       []ScopePath         `json:"required_paths"          yaml:"required_paths"`
	OptionalPaths       []ScopePath         `json:"optional_paths"          yaml:"optional_paths"`
	ExcludedPaths       []ScopeExcludedPath `json:"excluded_paths"          yaml:"excluded_paths"`
	Provides            []string            `json:"provides"                yaml:"provides"`
	Consumes            []string            `json:"consumes"                yaml:"consumes"`
	FinalWriteScope     []string            `json:"final_write_scope"       yaml:"final_write_scope"`
	VerificationFocus   []string            `json:"verification_focus"      yaml:"verification_focus"`
	AllowedLocalChoices []string            `json:"allowed_local_choices"   yaml:"allowed_local_choices"`
	StopConditions      []string            `json:"stop_conditions"         yaml:"stop_conditions"`
	OpenGaps            []string            `json:"open_gaps"               yaml:"open_gaps"`
}

// ScopeRequiredRead is an entry in ScopeEvidence.RequiredReads.
type ScopeRequiredRead struct {
	Path string `json:"path" yaml:"path"`
	Why  string `json:"why"  yaml:"why"`
}

// ScopeSeeds captures the starting symbols or paths the planner identified.
type ScopeSeeds struct {
	Symbols   []string `json:"symbols,omitempty"   yaml:"symbols,omitempty"`
	Paths     []string `json:"paths,omitempty"     yaml:"paths,omitempty"`
	Rationale []string `json:"rationale,omitempty" yaml:"rationale,omitempty"`
}

// ScopeQuerySummary holds the result files returned by a graph query.
type ScopeQuerySummary struct {
	Files []string `json:"files" yaml:"files"`
}

// ScopeQuery represents a single graph query run during scope derivation.
type ScopeQuery struct {
	Tool    string             `json:"tool"              yaml:"tool"`
	Kind    string             `json:"kind"              yaml:"kind"`
	Intent  string             `json:"intent"            yaml:"intent"`
	Subject string             `json:"subject"           yaml:"subject"`
	Summary *ScopeQuerySummary `json:"summary,omitempty" yaml:"summary,omitempty"`
}

// ScopePath is a required or optional path entry with explanatory reasons.
type ScopePath struct {
	Path    string   `json:"path"    yaml:"path"`
	Because []string `json:"because" yaml:"because"`
}

// ScopeExcludedPath is a path intentionally excluded from write_scope.
type ScopeExcludedPath struct {
	Path      string   `json:"path"      yaml:"path"`
	Rationale []string `json:"rationale" yaml:"rationale"`
}

// NewScopeEvidence returns a ScopeEvidence with all slice fields initialized to
// empty slices so they marshal to [] rather than null.
func NewScopeEvidence(planID, taskID string) *ScopeEvidence {
	return &ScopeEvidence{
		SchemaVersion:       1,
		PlanID:              planID,
		TaskID:              taskID,
		Status:              "draft",
		Confidence:          "low",
		DecisionLocks:       []string{},
		RequiredReads:       []ScopeRequiredRead{},
		Queries:             []ScopeQuery{},
		RequiredPaths:       []ScopePath{},
		OptionalPaths:       []ScopePath{},
		ExcludedPaths:       []ScopeExcludedPath{},
		Provides:            []string{},
		Consumes:            []string{},
		FinalWriteScope:     []string{},
		VerificationFocus:   []string{},
		AllowedLocalChoices: []string{},
		StopConditions:      []string{},
		OpenGaps:            []string{},
	}
}

// deriveScopeEvidencePath returns the canonical sidecar output path for a task.
func deriveScopeEvidencePath(projectPath, planID, taskID string) string {
	return filepath.Join(plansBaseDir(projectPath), planID, "evidence", taskID+".scope.yaml")
}

// runWorkflowPlanDeriveScope implements `workflow plan derive-scope <plan_id> <task_id>`.
// It runs scope-lane and context-lane query bundles against the KG/CRG graph and
// writes a candidate .scope.yaml sidecar. Degrades gracefully to confidence:low
// when the graph is not ready. Does NOT auto-edit TASKS.yaml.
func loadCanonicalTaskByID(projectPath, planID, taskID string) (*CanonicalTask, error) {
	tf, err := loadCanonicalTasks(projectPath, planID)
	if err != nil {
		return nil, fmt.Errorf(errTasksForPlanNotFoundFmt, planID, err)
	}
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			return &tf.Tasks[i], nil
		}
	}
	return nil, fmt.Errorf(errTaskNotFoundInPlanFmt, taskID, planID)
}

// findCanonicalTaskAnyPlan searches every canonical plan's TASKS.yaml for
// taskID and returns the owning plan id and task on the first match. Used by
// `workflow verify record` to validate --task against the workflow store
// when no delegation contract exists for the task — i.e. direct
// (non-delegated) work — so a typo'd --task still fails loudly instead of
// silently recording an unscoped entry.
func findCanonicalTaskAnyPlan(projectPath, taskID string) (string, *CanonicalTask, error) {
	planIDs, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return "", nil, fmt.Errorf("list plans: %w", err)
	}
	for _, planID := range planIDs {
		tf, err := loadCanonicalTasks(projectPath, planID)
		if err != nil {
			continue
		}
		for i := range tf.Tasks {
			if tf.Tasks[i].ID == taskID {
				return planID, &tf.Tasks[i], nil
			}
		}
	}
	return "", nil, fmt.Errorf("unknown task %q: not found in any workflow plan", taskID)
}

func graphAdapterForProject(projectPath string) *LocalGraphAdapter {
	cfg, _ := loadGraphBridgeConfig(projectPath)
	if cfg == nil {
		cfg = &GraphBridgeConfig{Enabled: false}
	}
	graphHome := cfg.GraphHome
	if graphHome == "" {
		graphHome = defaultGraphHome(projectPath)
	}
	return NewLocalGraphAdapter(graphHome)
}

// deriveScopeWarningsForMode returns the scope-lane skip reason (if any) for
// the given mode/readiness/seeds combination. Empty string means scope-lane
// queries should run.
func deriveScopeWarningsForMode(mode string, codeReady, hasScopeInputs bool) string {
	if mode == "code" && codeReady && hasScopeInputs {
		return ""
	}
	if mode != "code" {
		return "scope-lane graph queries skipped (mode: " + mode + ")"
	}
	if !codeReady {
		return "scope-lane graph queries skipped (code-lane not ready; run 'kg build' then 'kg warm --include-code')"
	}
	return "scope-lane graph queries skipped (no --seed-symbol or --seed-path provided)"
}

func persistScopeEvidenceSidecar(projectPath, planID, taskID string, ev *ScopeEvidence) (string, error) {
	outPath := deriveScopeEvidencePath(projectPath, planID, taskID)
	if err := osMkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return "", fmt.Errorf("create evidence dir: %w", err)
	}
	data, err := yamlMarshal(ev)
	if err != nil {
		return "", fmt.Errorf("marshal sidecar: %w", err)
	}
	if err := osWriteFile(outPath, data, 0644); err != nil {
		return "", fmt.Errorf("write sidecar: %w", err)
	}
	return outPath, nil
}

func runWorkflowPlanDeriveScope(planID, taskID string, seedSymbols, seedPaths []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	projectPath := project.Path

	input := &journal.DeriveScopeInput{Plan: planID, Task: taskID, SeedSymbols: seedSymbols, SeedPaths: seedPaths}
	observed := &journal.DeriveScopeObserved{}
	ok := false
	defer func() { journalTier1(projectPath, journal.CmdPlanDeriveScope, input, observed, ok) }()

	task, err := loadCanonicalTaskByID(projectPath, planID, taskID)
	if err != nil {
		return err
	}
	mode := deriveScopeMode(task)

	adapter := graphAdapterForProject(projectPath)
	health, _ := adapter.Health()

	ev := NewScopeEvidence(planID, taskID)
	ev.Mode = mode
	ev.Goal = strings.TrimSpace(task.Notes)
	if len(ev.Goal) > 120 {
		ev.Goal = ev.Goal[:120] + "…"
	}

	hasScopeInputs := len(seedSymbols) > 0 || len(seedPaths) > 0
	if hasScopeInputs {
		ev.Seeds = &ScopeSeeds{
			Symbols: append([]string{}, seedSymbols...),
			Paths:   append([]string{}, seedPaths...),
		}
	}

	for _, p := range task.WriteScope {
		ev.RequiredPaths = append(ev.RequiredPaths, ScopePath{
			Path:    p,
			Because: []string{"listed in TASKS.yaml write_scope"},
		})
	}

	codeReady := health.CodeLaneReady
	contextReady := health.ContextLaneReady

	var scopeWarnings []string
	if w := deriveScopeWarningsForMode(mode, codeReady, hasScopeInputs); w != "" {
		scopeWarnings = append(scopeWarnings, w)
	} else {
		_ = deriveScopeRunScopeLane(projectPath, seedSymbols, seedPaths, ev)
	}

	if contextReady {
		deriveScopeRunContextLane(planID, taskID, adapter, ev)
	} else {
		scopeWarnings = append(scopeWarnings, "context-lane queries skipped (context-lane not ready; run 'kg warm' after authoring notes)")
	}

	ev.Confidence = deriveScopeConfidence(mode, codeReady, contextReady, hasScopeInputs, len(ev.Queries))
	if len(scopeWarnings) > 0 {
		ev.OpenGaps = append(ev.OpenGaps, scopeWarnings...)
	}

	outPath, err := persistScopeEvidenceSidecar(projectPath, planID, taskID, ev)
	if err != nil {
		return err
	}
	observed.SidecarPath = config.DisplayPath(outPath)
	observed.Mode = mode
	observed.Confidence = ev.Confidence
	observed.RequiredPaths = len(ev.RequiredPaths)
	observed.Queries = len(ev.Queries)
	ok = true

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ev)
	}
	ui.Success(fmt.Sprintf("Wrote scope evidence sidecar: %s", config.DisplayPath(outPath)))
	fmt.Fprintf(os.Stdout, "  confidence: %s\n", ev.Confidence)
	fmt.Fprintf(os.Stdout, "  required_paths: %d  queries: %d\n", len(ev.RequiredPaths), len(ev.Queries))
	for _, w := range scopeWarnings {
		ui.Warn(w)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// deriveScopeMode derives the task mode from app_type and notes heuristics.
func deriveScopeMode(task *CanonicalTask) string {
	if task.AppType != "" {
		// Any declared app_type implies code mode.
		return "code"
	}
	notes := strings.ToLower(task.Notes)
	// Research/doc markers in notes.
	if strings.Contains(notes, "research task") || strings.Contains(notes, "no go code") ||
		strings.Contains(notes, "doc only") || strings.Contains(notes, "docs only") ||
		strings.Contains(notes, "skill instruction") {
		return "research"
	}
	// Check write_scope: if it only contains non-Go paths it's likely doc/research.
	allDocs := len(task.WriteScope) > 0
	for _, p := range task.WriteScope {
		if strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "/") ||
			strings.HasPrefix(p, "commands/") || strings.HasPrefix(p, "internal/") {
			allDocs = false
			break
		}
	}
	if allDocs && len(task.WriteScope) > 0 {
		return "doc"
	}
	return "code"
}

// deriveScopeConfidence calculates a confidence level string based on lane readiness.
func deriveScopeConfidence(mode string, codeReady, contextReady, hasScopeInputs bool, queryCount int) string {
	if mode != "code" {
		if contextReady {
			return "medium"
		}
		return "low"
	}
	// code mode
	switch {
	case codeReady && hasScopeInputs && queryCount > 0:
		return "medium"
	case codeReady && hasScopeInputs:
		return "medium"
	case codeReady || contextReady:
		return "low"
	default:
		return "low"
	}
}

// deriveScopeRunScopeLane runs symbol_lookup, callers_of, and impact_radius queries
// for all provided seed symbols and seed paths, populating ev.Queries and ev.RequiredPaths.
func deriveScopeRunScopeLane(projectPath string, seedSymbols, seedPaths []string, ev *ScopeEvidence) []string {
	var allFiles []string
	seen := make(map[string]bool)
	addFiles := func(files []string) {
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				allFiles = append(allFiles, f)
			}
		}
	}

	for _, sym := range seedSymbols {
		for _, intent := range []string{"symbol_lookup", "callers_of"} {
			if files := appendScopeBridgeQuery(projectPath, intent, sym, ev); len(files) > 0 {
				addFiles(files)
			}
		}
	}

	for _, p := range seedPaths {
		if files := appendScopeBridgeQuery(projectPath, "impact_radius", p, ev); len(files) > 0 {
			addFiles(files)
		}
	}

	appendScopeRequiredPaths(ev, allFiles)
	return allFiles
}

func appendScopeBridgeQuery(projectPath, intent, subject string, ev *ScopeEvidence) []string {
	files := deriveScopeKGBridgeQuery(projectPath, intent, subject)
	q := ScopeQuery{
		Tool:    "kg",
		Kind:    "bridge_query",
		Intent:  intent,
		Subject: subject,
	}
	if len(files) > 0 {
		q.Summary = &ScopeQuerySummary{Files: files}
	}
	ev.Queries = append(ev.Queries, q)
	return files
}

func appendScopeRequiredPaths(ev *ScopeEvidence, files []string) {
	existingPaths := make(map[string]bool)
	for _, rp := range ev.RequiredPaths {
		existingPaths[rp.Path] = true
	}
	for _, f := range files {
		if existingPaths[f] {
			continue
		}
		ev.RequiredPaths = append(ev.RequiredPaths, ScopePath{
			Path:    f,
			Because: []string{"discovered via scope-lane graph query"},
		})
		existingPaths[f] = true
	}
}

// deriveScopeKGBridgeQuery runs one kg bridge query subcommand and returns the
// list of file paths extracted from the JSON response. Returns nil on any error
// (graceful degradation).
func deriveScopeKGBridgeQuery(projectPath, intent, subject string) []string {
	exe, err := workflowDotAgentsExe()
	if err != nil {
		return nil
	}
	argv := []string{"--json", "kg", "bridge", "query", "--intent", intent, subject}
	cmd := exec.Command(exe, argv...)
	cmd.Dir = projectPath
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	// Parse the JSON response to extract file paths from results.
	var resp struct {
		Results []struct {
			Path     string `json:"path"`
			FilePath string `json:"file_path"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var files []string
	for _, r := range resp.Results {
		p := r.FilePath
		if p == "" {
			p = r.Path
		}
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	return files
}

// deriveScopeRunContextLane runs plan_context and decision_lookup queries against
// the context-lane and populates ev.RequiredReads with the results.
func deriveScopeRunContextLane(planID, taskID string, adapter *LocalGraphAdapter, ev *ScopeEvidence) {
	for _, intent := range []string{"plan_context", "decision_lookup"} {
		q := strings.TrimSpace(planID + " " + taskID)
		resp, err := adapter.Query(GraphBridgeQuery{
			Intent: intent,
			Query:  q,
		})
		if err != nil {
			continue
		}
		sq := ScopeQuery{
			Tool:    "kg",
			Kind:    "bridge_query",
			Intent:  intent,
			Subject: q,
		}
		files := scopeResponseFiles(resp.Results)
		if len(files) > 0 {
			sq.Summary = &ScopeQuerySummary{Files: files}
		}
		appendScopeRequiredReads(ev, resp.Results)
		ev.Queries = append(ev.Queries, sq)
	}
}

func scopeResponseFiles(results []GraphBridgeResult) []string {
	var files []string
	for _, r := range results {
		if r.Path != "" {
			files = append(files, r.Path)
		}
	}
	return files
}

func appendScopeRequiredReads(ev *ScopeEvidence, results []GraphBridgeResult) {
	for _, r := range results {
		if r.Path == "" {
			continue
		}
		ev.RequiredReads = append(ev.RequiredReads, ScopeRequiredRead{
			Path: r.Path,
			Why:  r.Title + " — " + r.Summary,
		})
	}
}

// readCanonicalStateFile reads a coordination-state file (TASKS.yaml /
// PLAN.yaml) for the project, honoring work_tracking.read_from
// (work-tracking-storage-abstraction spec §9 read-from-master shim).
//
// Default / "worktree" mode: this is EXACTLY os.ReadFile(absPath) — the read
// path is byte-for-byte unchanged (same content, same *os.PathError so
// os.IsNotExist callers keep working), so the running loop is untouched unless
// read_from is explicitly set to "master".
//
// "master" mode: the file is resolved from the canonical ref
// (origin/<default-branch>) via `git show <ref>:<repo-rel-path>` so worktree
// isolation cannot make the orchestrator/scout read a stale status and
// re-dispatch in-flight work. The read gracefully falls back to the worktree
// copy when the ref (no origin remote / origin/HEAD unset) or the file within
// it (a plan not yet on the default branch) cannot be resolved — a
// misconfigured ref is therefore never worse than today. Writes are unaffected.
func readCanonicalStateFile(projectPath, absPath string) ([]byte, error) {
	if canonicalReadFromMaster(projectPath) {
		if content, ok := readFileFromCanonicalRef(projectPath, absPath); ok {
			return content, nil
		}
	}
	return os.ReadFile(absPath)
}

// canonicalReadFromMaster reports whether coordination-state reads for
// projectPath resolve from the canonical ref rather than the worktree copy —
// work_tracking.read_from == "master". Any config-load failure or absent
// config yields false so the default (worktree) read path is unchanged.
func canonicalReadFromMaster(projectPath string) bool {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return false
	}
	return rc.ReadFromMaster()
}

// originRefPrefix is the remote-tracking ref prefix for the origin remote.
const originRefPrefix = "origin/"

// canonicalStateRef returns the ref coordination-state is read from in master
// mode: origin/<default-branch>. Empty when the default branch cannot be
// resolved (no origin remote / origin/HEAD unset), signalling the caller to
// fall back to the worktree copy.
func canonicalStateRef(projectPath string) string {
	branch := originDefaultBranch(projectPath)
	if branch == "" {
		return ""
	}
	return originRefPrefix + branch
}

// originDefaultBranch resolves the remote default branch name (e.g. "main")
// from origin/HEAD — never hardcoding "master". Empty when origin/HEAD is
// unset (bare `git remote add` without a fetch, or no origin at all).
func originDefaultBranch(projectPath string) string {
	if out := strings.TrimSpace(gitOutput(projectPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")); out != "" {
		return strings.TrimPrefix(out, originRefPrefix)
	}
	if out := strings.TrimSpace(gitOutput(projectPath, gitSubcmdRevParse, "--abbrev-ref", "origin/HEAD")); out != "" && out != "origin/HEAD" {
		return strings.TrimPrefix(out, originRefPrefix)
	}
	return ""
}

// readFileFromCanonicalRef returns the contents of absPath as recorded on the
// canonical ref (origin/<default-branch>), via `git show <ref>:<repo-rel-path>`.
// The second result is false — signalling the caller to fall back to the
// worktree copy — when the ref cannot be resolved, absPath is not under
// projectPath, or the file does not exist on that ref.
func readFileFromCanonicalRef(projectPath, absPath string) ([]byte, bool) {
	ref := canonicalStateRef(projectPath)
	if ref == "" {
		return nil, false
	}
	rel, err := filepath.Rel(projectPath, absPath)
	if err != nil {
		return nil, false
	}
	trackGitSpawn()
	out, err := execabs.Command("git", "-C", projectPath, "show", ref+":"+filepath.ToSlash(rel)).Output()
	if err != nil {
		return nil, false
	}
	return out, true
}

// ── git-ref state backend: CAS write path (work-tracking-storage-abstraction §9 D9/D10) ──

// stateRefName is the dedicated git ref coordination-state transitions are
// mirrored to under the git-ref backend (design §9 D9). It is ORTHOGONAL to the
// code branch — worktrees on any feature branch or detached HEAD resolve status
// against this one ref — and is NEVER merged into the default branch (D10): a
// parallel lineage like refs/notes/*.
const stateRefName = "refs/agents/state"

// zeroOID is git's all-zeros object id. As `git update-ref <ref> <new> <old>`'s
// <old> argument it asserts the ref does NOT yet exist, making the very first
// write race-safe (create-only) under the same compare-and-swap as every update.
const zeroOID = "0000000000000000000000000000000000000000"

// stateRefCASAttempts bounds the compare-and-swap retry loop: when a concurrent
// process advances refs/agents/state between our read of <old> and our
// update-ref, git rejects the swap and we re-read, rebuild, and retry up to this
// many times before surfacing the contention as an error.
const stateRefCASAttempts = 16

// stateRefCommitMessage is the fixed message on each state-ref commit. The ref
// is machine-owned coordination state, not human-authored history.
const stateRefCommitMessage = "da: coordination-state transition"

// mirrorTransitionToStateRef additionally writes planID's changed
// coordination-state to refs/agents/state via atomic compare-and-swap, but ONLY
// when the git-ref write backend is active. Status is split into per-task state
// blobs: taskID's blob and PLAN.yaml are overwritten authoritatively while
// sibling tasks are seeded only when absent, preserving disjoint transitions.
func mirrorTransitionToStateRef(projectPath, planID, taskID string) error {
	if !canonicalWriteToStateRef(projectPath) {
		return nil
	}
	overwrite, seed, err := collectPlanTaskStateRefWrite(projectPath, planID, taskID)
	if err != nil {
		return fmt.Errorf("collect state files for %s: %w", stateRefName, err)
	}
	return writePlanStateRefCAS(projectPath, overwrite, seed)
}

// resolveRepointRefTasks reconciles the repoint mutation against the latest ref
// snapshot: with no prior per-task records it returns tf unchanged; otherwise it
// projects the records, applies the repoint when the old task still exists, or
// (when only the new id is present) repoints dependencies. Pure in-memory — it
// issues no git calls, so it preserves the mirror path's git-spawn behavior.
func resolveRepointRefTasks(tf *CanonicalTaskFile, current []stateRefTaskRecord, in taskRepointInputs) (*CanonicalTaskFile, error) {
	if len(current) == 0 {
		return tf, nil
	}
	refTasks := projectCanonicalTaskFile(current)
	oldIdx, newIdx := taskIndexes(refTasks, in.OldID, in.NewID)
	if oldIdx >= 0 {
		if _, applyErr := applyTaskRepoint(refTasks, in, oldIdx, newIdx); applyErr != nil {
			return nil, applyErr
		}
		return refTasks, nil
	}
	if newIdx < 0 {
		return nil, fmt.Errorf(errTaskNotFoundInPlanFmt, in.OldID, in.PlanID)
	}
	repointTaskDependencies(refTasks, in)
	return refTasks, nil
}

// mirrorTaskRepointToStateRef applies a structural task-ID mutation to the
// latest ref snapshot on every CAS attempt. This preserves concurrent status
// transitions and unrelated task additions while deleting the old task blob in
// the same ref commit as the rewritten survivors.
func mirrorTaskRepointToStateRef(projectPath string, tf *CanonicalTaskFile, in taskRepointInputs) error {
	if !canonicalWriteToStateRef(projectPath) {
		return nil
	}
	planPath := filepath.Join(plansBaseDir(projectPath), tf.PlanID, workflowPlanFileName)
	planContent, err := os.ReadFile(planPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return casWriteStateRef(projectPath, func(head string) ([]stateRefFile, error) {
		current, loadErr := readPlanTaskRecordsFromCommit(projectPath, head, tf.PlanID)
		if loadErr != nil {
			return nil, loadErr
		}
		refTasks, resolveErr := resolveRepointRefTasks(tf, current, in)
		if resolveErr != nil {
			return nil, resolveErr
		}
		return taskRepointStateRefFiles(projectPath, refTasks, in.OldID, planContent)
	})
}

func taskRepointStateRefFiles(projectPath string, tf *CanonicalTaskFile, oldID string, planContent []byte) ([]stateRefFile, error) {
	files := make([]stateRefFile, 0, len(tf.Tasks)+2)
	for _, rec := range splitCanonicalTaskFile(tf) {
		content, err := yamlMarshal(rec)
		if err != nil {
			return nil, err
		}
		rel, err := planTaskStateRefRelPath(projectPath, tf.PlanID, rec.Task.ID)
		if err != nil {
			return nil, err
		}
		files = append(files, stateRefFile{relPath: rel, content: content})
	}
	oldRel, err := planTaskStateRefRelPath(projectPath, tf.PlanID, oldID)
	if err != nil {
		return nil, err
	}
	files = append(files, stateRefFile{relPath: oldRel, remove: true})
	if len(planContent) > 0 {
		planRel, err := planStateRefRelPath(projectPath, tf.PlanID)
		if err != nil {
			return nil, err
		}
		files = append(files, stateRefFile{relPath: planRel, content: planContent})
	}
	return files, nil
}

// canonicalWriteToStateRef reports whether transitions for projectPath ALSO
// mirror to the state ref. True when either work_tracking.write_to ==
// "state-ref" (the focused write gate) OR work_tracking.backend == "git-ref"
// (the git-ref backend IMPLIES the mirror even with write_to unset). Any
// config-load failure or absent config yields false so the default
// (working-copy-only) write path is byte-for-byte unchanged.
func canonicalWriteToStateRef(projectPath string) bool {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return false
	}
	return rc.WriteToStateRef() || rc.UseGitRefBackend()
}

// useGitRefBackend reports whether the git-ref WorkStore backend is active for
// projectPath — work_tracking.backend == "git-ref". Any config-load failure or
// absent config yields false so the default (working-copy) read path is
// byte-for-byte unchanged.
func useGitRefBackend(projectPath string) bool {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return false
	}
	return rc.UseGitRefBackend()
}

// stateRefFile is one coordination-state path staged into a state-ref commit.
// A remove entry deletes relPath from the parent tree; otherwise content is
// written as a regular file.
type stateRefFile struct {
	relPath string
	content []byte
	remove  bool
}

// stateRefTasksDir is the ref-only subdirectory under a plan that holds the
// per-task state blobs (one file per task id). Splitting status out of the
// monolithic TASKS.yaml into these files (§9 D5) lets two workers transition
// DIFFERENT tasks without touching the same ref blob — eliminating the
// line-level TASKS.yaml conflict; only the benign shared-PLAN.yaml CAS-retry
// remains.
const stateRefTasksDir = "tasks"

// stateRefTaskRecord is the on-ref serialization of one task's coordination
// state: the CanonicalTask plus the plan-scoping and ordering metadata needed
// to regenerate the monolithic TASKS.yaml projection with byte fidelity
// (§3B / D1'). Each record is self-contained so per-task writes stay disjoint —
// no shared order-manifest to contend on.
type stateRefTaskRecord struct {
	SchemaVersion int           `yaml:"schema_version"`
	PlanID        string        `yaml:"plan_id"`
	Order         int           `yaml:"order"`
	Task          CanonicalTask `yaml:"task"`
}

// splitCanonicalTaskFile decomposes a monolithic CanonicalTaskFile into one
// self-contained per-task record each, preserving task order via the Order
// field so projectCanonicalTaskFile can reassemble the exact original ordering.
func splitCanonicalTaskFile(tf *CanonicalTaskFile) []stateRefTaskRecord {
	records := make([]stateRefTaskRecord, 0, len(tf.Tasks))
	for i := range tf.Tasks {
		records = append(records, stateRefTaskRecord{
			SchemaVersion: tf.SchemaVersion,
			PlanID:        tf.PlanID,
			Order:         i,
			Task:          tf.Tasks[i],
		})
	}
	return records
}

// projectCanonicalTaskFile regenerates the canonical TASKS.yaml view from the
// per-task records: tasks are ordered by their Order field so the result is
// byte-identical to what saveCanonicalTasks wrote (the §3B / D1' projection).
// Plan scoping (schema_version, plan_id) comes from the lowest-ordered record.
func projectCanonicalTaskFile(records []stateRefTaskRecord) *CanonicalTaskFile {
	sorted := append([]stateRefTaskRecord{}, records...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	tf := &CanonicalTaskFile{}
	if len(sorted) > 0 {
		tf.SchemaVersion = sorted[0].SchemaVersion
		tf.PlanID = sorted[0].PlanID
	}
	tf.Tasks = make([]CanonicalTask, 0, len(sorted))
	for i := range sorted {
		tf.Tasks = append(tf.Tasks, sorted[i].Task)
	}
	return tf
}

// planTaskStateRefRelPath returns the repo-relative (slash-separated) ref path
// for taskID's per-task state blob under planID.
func planTaskStateRefRelPath(projectPath, planID, taskID string) (string, error) {
	abs := filepath.Join(plansBaseDir(projectPath), planID, stateRefTasksDir, taskID+".yaml")
	rel, err := filepath.Rel(projectPath, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// planStateRefRelPath returns the repo-relative (slash-separated) ref path for
// planID's PLAN.yaml blob.
func planStateRefRelPath(projectPath, planID string) (string, error) {
	abs := filepath.Join(plansBaseDir(projectPath), planID, workflowPlanFileName)
	rel, err := filepath.Rel(projectPath, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// buildPlanTaskStateRefWrite computes the per-task state-ref write for a
// transition of changedTaskID: the changed task's blob and PLAN.yaml are
// overwritten authoritatively, while every OTHER canonical task's blob is
// returned as a seed (written only when absent on the ref) so the projection
// stays complete without clobbering a concurrent writer's authoritative update
// to a different task. The task set comes from the canonical (working-copy) tf,
// so a newly-added task is seeded on the next transition (§3B projection
// completeness).
func buildPlanTaskStateRefWrite(projectPath, planID, changedTaskID string, tf *CanonicalTaskFile, planContent []byte) (overwrite, seed []stateRefFile, err error) {
	for _, rec := range splitCanonicalTaskFile(tf) {
		content, marshalErr := yamlMarshal(rec)
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		rel, relErr := planTaskStateRefRelPath(projectPath, planID, rec.Task.ID)
		if relErr != nil {
			return nil, nil, relErr
		}
		f := stateRefFile{relPath: rel, content: content}
		if rec.Task.ID == changedTaskID {
			overwrite = append(overwrite, f)
		} else {
			seed = append(seed, f)
		}
	}
	if len(planContent) > 0 {
		rel, relErr := planStateRefRelPath(projectPath, planID)
		if relErr != nil {
			return nil, nil, relErr
		}
		overwrite = append(overwrite, stateRefFile{relPath: rel, content: planContent})
	}
	return overwrite, seed, nil
}

// collectPlanTaskStateRefWrite reads planID's canonical TASKS.yaml and PLAN.yaml
// back from the working copy (NOT the read_from=master shim — the ref mirrors
// exactly what the §3B projection just wrote) and builds the per-task state-ref
// write for a transition of taskID. A missing TASKS.yaml yields an empty write.
func collectPlanTaskStateRefWrite(projectPath, planID, taskID string) (overwrite, seed []stateRefFile, err error) {
	dir := filepath.Join(plansBaseDir(projectPath), planID)
	raw, err := os.ReadFile(filepath.Join(dir, workflowTasksFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var tf CanonicalTaskFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return nil, nil, err
	}
	planContent, err := os.ReadFile(filepath.Join(dir, workflowPlanFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, err
		}
		planContent = nil
	}
	return buildPlanTaskStateRefWrite(projectPath, planID, taskID, &tf, planContent)
}

// casWriteStateRef runs the atomic compare-and-swap retry loop against
// refs/agents/state. Each attempt re-reads the ref's current commit (<old>),
// asks resolve for the files to overlay on <old>'s tree (letting a per-task
// writer skip seed files already present at that <old>), builds a new commit,
// then swaps only if the ref still points at <old>. A concurrent writer that
// moved the ref makes the swap fail; we re-read <old> and rebuild — the
// interprocess-safe read-modify-write the file lock alone cannot provide across
// worktrees. Bails after stateRefCASAttempts with the last contention error.
func casWriteStateRef(projectPath string, resolve func(old string) ([]stateRefFile, error)) error {
	var lastErr error
	for range stateRefCASAttempts {
		old := stateRefHead(projectPath)
		files, err := resolve(old)
		if err != nil {
			return err
		}
		if len(files) == 0 {
			return nil
		}
		commit, err := buildStateRefCommit(projectPath, old, files)
		if err != nil {
			return err
		}
		// Idempotency guard: a rebuilt tree identical to the current ref's tree
		// changes nothing, so skip the swap — a redundant mirror (e.g. the
		// choke-point save-mirror followed by a caller's explicit seed-mirror
		// of the same state) then produces NO new commit. Defense-in-depth for
		// the canonical-write choke point (#433): exactly one commit per write.
		if old != "" {
			if newTree := stateRefCommitTree(projectPath, commit); newTree != "" && newTree == stateRefCommitTree(projectPath, old) {
				return nil
			}
		}
		if err := casSwapFn(projectPath, commit, old); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("update %s: compare-and-swap lost after %d attempts: %w", stateRefName, stateRefCASAttempts, lastErr)
}

// writeStateRefCAS commits files to refs/agents/state, overlaying every file
// unconditionally on the current tree. It is the generic primitive;
// writePlanStateRefCAS layers the per-task seed-if-absent policy on top.
func writeStateRefCAS(projectPath string, files []stateRefFile) error {
	if len(files) == 0 {
		return nil
	}
	return casWriteStateRef(projectPath, func(string) ([]stateRefFile, error) {
		return files, nil
	})
}

// writePlanStateRefCAS commits a per-task transition to refs/agents/state: the
// overwrite files (the changed task's blob + PLAN.yaml) are always written,
// while each seed file (a sibling task's blob) is written only when it is not
// already present on the ref. Because "absent" is re-checked against the
// current ref on every CAS attempt, two workers transitioning DIFFERENT tasks
// each overwrite only their own blob and never seed over the other's
// authoritative update — no lost update on disjoint tasks.
func writePlanStateRefCAS(projectPath string, overwrite, seed []stateRefFile) error {
	if len(overwrite) == 0 && len(seed) == 0 {
		return nil
	}
	return casWriteStateRef(projectPath, func(old string) ([]stateRefFile, error) {
		files := append([]stateRefFile{}, overwrite...)
		seedPaths := make([]string, len(seed))
		for i := range seed {
			seedPaths[i] = seed[i].relPath
		}
		present := stateRefTreeContains(projectPath, old, seedPaths)
		for i := range seed {
			if !present[seed[i].relPath] {
				files = append(files, seed[i])
			}
		}
		return files, nil
	})
}

// stateRefPathExists reports whether relPath is present in commit's tree on the
// state ref. An empty commit (ref absent — the first write) is always false;
// any resolution error is treated as absent, matching stateRefHead's ""
// tolerance. Resolved IN-PROCESS via go-git (no git subprocess).
func stateRefPathExists(projectPath, commit, relPath string) bool {
	return stateRefTreeContains(projectPath, commit, []string{relPath})[relPath]
}

// stateRefTreeContains resolves, for every relPath, whether it is present in
// commit's tree on the state ref, in a SINGLE in-process go-git repo open — the
// batched seed-presence check that replaces the prior O(seeds) `cat-file -e`
// subprocess fan-out. An empty commit, an unopenable repo, or any tree-resolution
// error maps every path to absent (present==false), exactly matching the old
// per-path "git error ⇒ absent" tolerance so the seed-if-absent policy is
// behavior-preserving. Membership mirrors `cat-file -e commit:relPath`: a path
// resolving to any object (blob or tree) counts as present.
func stateRefTreeContains(projectPath, commit string, relPaths []string) map[string]bool {
	present := make(map[string]bool, len(relPaths))
	if commit == "" || len(relPaths) == 0 {
		return present
	}
	repo, err := openStateRefRepo(projectPath)
	if err != nil {
		return present
	}
	tree, err := stateRefCommitTreeObject(repo, commit)
	if err != nil {
		return present
	}
	for _, rel := range relPaths {
		if _, err := tree.FindEntry(rel); err == nil {
			present[rel] = true
		}
	}
	return present
}

// readPlanTaskRecordsFromStateRef reads planID's per-task state blobs from
// refs/agents/state and decodes them into records. A missing ref or plan tasks/
// directory yields no records (the projection of an empty state).
func readPlanTaskRecordsFromStateRef(projectPath, planID string) ([]stateRefTaskRecord, error) {
	return readPlanTaskRecordsFromCommit(projectPath, stateRefHead(projectPath), planID)
}

func readPlanTaskRecordsFromCommit(projectPath, commit, planID string) ([]stateRefTaskRecord, error) {
	if commit == "" {
		return nil, nil
	}
	dirRel, err := planTaskStateRefDirRel(projectPath, planID)
	if err != nil {
		return nil, err
	}
	treeish := commit + ":" + dirRel
	out, lsErr := gitStateExec(projectPath, nil, nil, "ls-tree", gitFlagNameOnly, treeish)
	if lsErr != nil {
		return nil, nil
	}
	var names []string
	for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, nil
	}
	blobs, err := catFileBatchBlobs(projectPath, treeish, names)
	if err != nil {
		return nil, err
	}
	records := make([]stateRefTaskRecord, 0, len(names))
	for _, blob := range blobs {
		var rec stateRefTaskRecord
		if err := yaml.Unmarshal(blob, &rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// catFileBatchBlobs resolves every treeish/name to its blob content in ONE
// `git cat-file --batch` invocation, returning contents in the SAME order as
// names. This collapses the prior O(len(names)) `git show` fan-out to a single
// spawn. cat-file --batch echoes a name it cannot resolve as "<object> missing"
// (exit 0), so a missing entry is surfaced as an error — mirroring the per-blob
// `git show` failure the old read path returned. Output is size-framed and
// binary-safe; gitStateExec returns stdout verbatim, so []byte(out) is exact.
func catFileBatchBlobs(projectPath, treeish string, names []string) ([][]byte, error) {
	var stdin bytes.Buffer
	for _, name := range names {
		stdin.WriteString(treeish + "/" + name + "\n")
	}
	out, err := gitStateExec(projectPath, nil, stdin.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	return parseCatFileBatch([]byte(out), names)
}

// parseCatFileBatch decodes `git cat-file --batch` stdout into one blob per
// name, in order. Each found entry is `<oid> SP <type> SP <size> LF` followed
// by exactly <size> content bytes and a trailing LF; an unresolved name is
// `<object> SP missing LF`. A missing entry or any framing corruption is an
// error, matching the old per-`git show` path's error behavior.
func parseCatFileBatch(data []byte, names []string) ([][]byte, error) {
	blobs := make([][]byte, 0, len(names))
	pos := 0
	for _, name := range names {
		nl := bytes.IndexByte(data[pos:], '\n')
		if nl < 0 {
			return nil, fmt.Errorf("git cat-file --batch: truncated header for %q", name)
		}
		header := string(data[pos : pos+nl])
		pos += nl + 1
		if strings.HasSuffix(header, " missing") {
			return nil, fmt.Errorf("git cat-file --batch: object missing for %q", name)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file --batch: malformed header %q", header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("git cat-file --batch: bad size in header %q: %w", header, err)
		}
		if pos+size > len(data) {
			return nil, fmt.Errorf("git cat-file --batch: truncated content for %q", name)
		}
		blobs = append(blobs, data[pos:pos+size])
		pos += size
		if pos < len(data) && data[pos] == '\n' {
			pos++
		}
	}
	return blobs, nil
}

// planTaskStateRefDirRel returns the repo-relative (slash-separated) ref path of
// planID's per-task blob directory.
func planTaskStateRefDirRel(projectPath, planID string) (string, error) {
	abs := filepath.Join(plansBaseDir(projectPath), planID, stateRefTasksDir)
	rel, err := filepath.Rel(projectPath, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// projectPlanTasksFromStateRef regenerates planID's canonical TASKS.yaml view
// from the per-task state blobs on refs/agents/state — the §3B / D1' projection
// that reconstructs the monolithic view from the split files with byte fidelity.
func projectPlanTasksFromStateRef(projectPath, planID string) (*CanonicalTaskFile, error) {
	records, err := readPlanTaskRecordsFromStateRef(projectPath, planID)
	if err != nil {
		return nil, err
	}
	return projectCanonicalTaskFile(records), nil
}

// loadCanonicalTasksFromStateRef projects planID's canonical TASKS.yaml view
// from the per-task state blobs on refs/agents/state (git-ref backend read
// path). ok is false — signalling the caller to fall back to the per-worktree
// working copy — when the plan has no task blobs on the ref (a ref never
// written, or a plan not yet mirrored). A malformed blob surfaces as an error.
func loadCanonicalTasksFromStateRef(projectPath, planID string) (tf *CanonicalTaskFile, ok bool, err error) {
	records, err := readPlanTaskRecordsFromStateRef(projectPath, planID)
	if err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	return projectCanonicalTaskFile(records), true, nil
}

// readPlanFileFromStateRef returns planID's PLAN.yaml bytes as recorded on
// refs/agents/state (git-ref backend read path). ok is false — signalling the
// caller to fall back to the per-worktree working copy — when the ref is absent
// or PLAN.yaml has not been mirrored to it.
func readPlanFileFromStateRef(projectPath, planID string) ([]byte, bool) {
	head := stateRefHead(projectPath)
	if head == "" {
		return nil, false
	}
	rel, err := planStateRefRelPath(projectPath, planID)
	if err != nil {
		return nil, false
	}
	out, err := gitStateExec(projectPath, nil, nil, "show", head+":"+rel)
	if err != nil {
		return nil, false
	}
	return []byte(out), true
}

// stateRefHead returns the commit refs/agents/state currently points at, or ""
// when the ref does not yet exist (the first write). Uses gitOutput, which
// yields "" on the expected non-zero exit for an absent ref.
func stateRefHead(projectPath string) string {
	return strings.TrimSpace(gitOutput(projectPath, gitSubcmdRevParse, "--verify", "--quiet", stateRefName+"^{commit}"))
}

// stateRefCommitTree returns the tree OID of commit, or "" when commit is absent
// or invalid (gitOutput yields "" on the expected non-zero exit). Used by the
// CAS idempotency guard to detect a no-op write whose tree already matches the
// ref, so a redundant mirror produces no new commit.
func stateRefCommitTree(projectPath, commit string) string {
	return strings.TrimSpace(gitOutput(projectPath, gitSubcmdRevParse, "--verify", "--quiet", commit+"^{tree}"))
}

// buildStateRefCommit builds (but does not install) a commit that carries files
// on top of parent's tree, entirely IN-PROCESS via go-git — creating the blobs,
// assembling the tree, and writing the commit object into the repo's shared
// object store without spawning a single `git` child. parent "" produces a root
// commit (first write). Returns the new commit's object id.
//
// CAS-SAFETY: the objects are byte-canonical git objects. Blobs hash as
// SHA1("blob <len>\0<content>"), trees encode as sorted `<octal-mode> <name>\0
// <rawhash>` entries (go-git's TreeEntrySorter applies git's trailing-slash
// directory sort rule), and directory removal prunes now-empty subtrees just as
// `write-tree` omits empty directories. So the resulting TREE OID is IDENTICAL
// to what the old read-tree+hash-object+update-index+write-tree plumbing
// produced for the same parent and file set — the idempotency-tree guard
// (stateRefCommitTree) and the compare-and-swap therefore behave unchanged, even
// across a ref whose earlier commits were written by the plumbing path. The
// commit reuses the same deterministic dot-agents author/committer identity
// (stateRefIdentityName/Email) and the fixed stateRefCommitMessage; only the
// commit timestamp varies, exactly as commit-tree already varied it.
func buildStateRefCommit(projectPath, parent string, files []stateRefFile) (string, error) {
	repo, err := openStateRefRepo(projectPath)
	if err != nil {
		return "", err
	}
	root := newStateRefTreeNode()
	if parent != "" {
		parentTree, err := stateRefCommitTreeObject(repo, parent)
		if err != nil {
			return "", err
		}
		if err := root.loadFrom(repo, parentTree); err != nil {
			return "", err
		}
	}
	store := repo.Storer
	for _, f := range files {
		parts := strings.Split(f.relPath, "/")
		if f.remove {
			root.remove(parts)
			continue
		}
		blobHash, err := writeStateRefBlob(store, f.content)
		if err != nil {
			return "", err
		}
		root.set(parts, stateRefTreeEntry{hash: blobHash, mode: filemode.Regular})
	}
	treeHash, err := root.encode(store)
	if err != nil {
		return "", err
	}
	now := time.Now()
	sig := object.Signature{Name: stateRefIdentityName, Email: stateRefIdentityEmail, When: now}
	commit := &object.Commit{
		Author:    sig,
		Committer: sig,
		Message:   stateRefCommitMessage + "\n",
		TreeHash:  treeHash,
	}
	if parent != "" {
		commit.ParentHashes = []plumbing.Hash{plumbing.NewHash(parent)}
	}
	obj := store.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return "", err
	}
	commitHash, err := store.SetEncodedObject(obj)
	if err != nil {
		return "", err
	}
	return commitHash.String(), nil
}

// stateRefIdentityName / stateRefIdentityEmail are the deterministic, hermetic
// author/committer identity carried on every state-ref commit — the in-process
// counterpart of stateRefCommitEnv's GIT_AUTHOR_*/GIT_COMMITTER_* so the commit
// never depends on ambient user.name/user.email config (the ref is
// machine-owned).
const (
	stateRefIdentityName  = "dot-agents"
	stateRefIdentityEmail = "dot-agents@localhost"
)

// openStateRefRepo opens the go-git repository at (or above) projectPath. Objects
// SetEncodedObject writes land in the repo's shared object store — the same store
// `git update-ref` (the untouched CAS-swap primitive) resolves the new commit
// against — so a linked worktree writes to the common objects dir, matching the
// plumbing path's `git -C projectPath` behavior.
func openStateRefRepo(projectPath string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(projectPath, &git.PlainOpenOptions{DetectDotGit: true})
}

// stateRefCommitTreeObject resolves commit's root tree object in repo.
func stateRefCommitTreeObject(repo *git.Repository, commit string) (*object.Tree, error) {
	c, err := repo.CommitObject(plumbing.NewHash(commit))
	if err != nil {
		return nil, err
	}
	return c.Tree()
}

// writeStateRefBlob writes content as a blob object and returns its hash. The
// blob hashes exactly as `git hash-object -w --stdin` would.
func writeStateRefBlob(store storer.EncodedObjectStorer, content []byte) (plumbing.Hash, error) {
	obj := store.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := w.Write(content); err != nil {
		_ = w.Close()
		return plumbing.ZeroHash, err
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return store.SetEncodedObject(obj)
}

// stateRefTreeEntry is a resolved blob entry (hash + file mode) in an in-process
// tree node.
type stateRefTreeEntry struct {
	hash plumbing.Hash
	mode filemode.FileMode
}

// stateRefTreeNode is a mutable in-process representation of a git tree: blob
// entries by name plus child subtrees by name. It mirrors the overlay semantics
// of read-tree (load parent) + update-index (set/remove) + write-tree (encode).
type stateRefTreeNode struct {
	files   map[string]stateRefTreeEntry
	subdirs map[string]*stateRefTreeNode
}

func newStateRefTreeNode() *stateRefTreeNode {
	return &stateRefTreeNode{
		files:   map[string]stateRefTreeEntry{},
		subdirs: map[string]*stateRefTreeNode{},
	}
}

func (n *stateRefTreeNode) empty() bool {
	return len(n.files) == 0 && len(n.subdirs) == 0
}

// loadFrom recursively populates n from an existing tree object, so subsequent
// set/remove calls overlay onto the parent's full content.
func (n *stateRefTreeNode) loadFrom(repo *git.Repository, tree *object.Tree) error {
	for i := range tree.Entries {
		e := tree.Entries[i]
		if e.Mode == filemode.Dir {
			sub, err := object.GetTree(repo.Storer, e.Hash)
			if err != nil {
				return err
			}
			child := newStateRefTreeNode()
			if err := child.loadFrom(repo, sub); err != nil {
				return err
			}
			n.subdirs[e.Name] = child
			continue
		}
		n.files[e.Name] = stateRefTreeEntry{hash: e.Hash, mode: e.Mode}
	}
	return nil
}

// set overlays a blob at the slash-split path, creating intermediate subtrees.
func (n *stateRefTreeNode) set(parts []string, entry stateRefTreeEntry) {
	if len(parts) == 1 {
		delete(n.subdirs, parts[0])
		n.files[parts[0]] = entry
		return
	}
	child := n.subdirs[parts[0]]
	if child == nil {
		child = newStateRefTreeNode()
		n.subdirs[parts[0]] = child
	}
	delete(n.files, parts[0])
	child.set(parts[1:], entry)
}

// remove deletes the blob (or subtree) at the slash-split path and prunes any
// subtree left empty, so the encoded tree omits empty directories just as
// write-tree does.
func (n *stateRefTreeNode) remove(parts []string) {
	if len(parts) == 1 {
		delete(n.files, parts[0])
		delete(n.subdirs, parts[0])
		return
	}
	child := n.subdirs[parts[0]]
	if child == nil {
		return
	}
	child.remove(parts[1:])
	if child.empty() {
		delete(n.subdirs, parts[0])
	}
}

// encode writes this node and every non-empty descendant as tree objects and
// returns this node's tree hash. Entries are sorted with git's directory
// trailing-slash rule so the object bytes — and thus the OID — are canonical.
func (n *stateRefTreeNode) encode(store storer.EncodedObjectStorer) (plumbing.Hash, error) {
	entries := make([]object.TreeEntry, 0, len(n.files)+len(n.subdirs))
	for name, e := range n.files {
		entries = append(entries, object.TreeEntry{Name: name, Mode: e.mode, Hash: e.hash})
	}
	for name, child := range n.subdirs {
		if child.empty() {
			continue
		}
		h, err := child.encode(store)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries = append(entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: h})
	}
	sort.Sort(object.TreeEntrySorter(entries))
	tree := &object.Tree{Entries: entries}
	obj := store.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return store.SetEncodedObject(obj)
}

// compareAndSwapStateRef performs the atomic swap: update refs/agents/state to
// newCommit only if it still points at old. An empty old is sent as zeroOID so
// the first write succeeds only if the ref is still absent (create-only). A
// non-nil error means the swap was rejected (the ref moved) — the caller's
// retry signal.
func compareAndSwapStateRef(projectPath, newCommit, old string) error {
	oldArg := old
	if oldArg == "" {
		oldArg = zeroOID
	}
	_, err := gitStateExec(projectPath, nil, nil, "update-ref", stateRefName, newCommit, oldArg)
	return err
}

// casSwapFn is the compare-and-swap seam. Production points it at
// compareAndSwapStateRef; the bounded-retry-exhaustion test overrides it with a
// perpetual-conflict stub to prove writeStateRefCAS gives up (with a wrapped
// error) after stateRefCASAttempts instead of spinning forever.
var casSwapFn = compareAndSwapStateRef

// gitStateExec runs a git command in projectPath for the state-ref CAS path,
// with optional extra environment (e.g. GIT_INDEX_FILE) and stdin. Unlike
// gitOutput it RETURNS the error and stderr, so the CAS loop can tell a rejected
// compare-and-swap (ref moved) apart from a successful update.
func gitStateExec(projectPath string, extraEnv []string, stdin []byte, args ...string) (string, error) {
	trackGitSpawn()
	cmd := execabs.Command("git", append([]string{"-C", projectPath}, args...)...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// loadCanonicalPlan resolves planID's PLAN.yaml, honoring the coordination-state
// backend. git-ref: read PLAN.yaml from refs/agents/state (a local,
// read-your-writes-safe ref), gracefully falling back to the per-worktree
// working copy when PLAN.yaml is absent on the ref. git-ref takes PRECEDENCE
// over the read_from=master shim — the fallback reads the working copy directly,
// never origin/<default-branch>. Default/local: the existing readCanonicalStateFile
// seam (worktree, or read_from=master).
func loadCanonicalPlan(projectPath, planID string) (*CanonicalPlan, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, workflowPlanFileName)
	var content []byte
	var err error
	if useGitRefBackend(projectPath) {
		if refContent, ok := readPlanFileFromStateRef(projectPath, planID); ok {
			content = refContent
		} else if content, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	} else if content, err = readCanonicalStateFile(projectPath, path); err != nil {
		return nil, err
	}
	var plan CanonicalPlan
	if err := yaml.Unmarshal(content, &plan); err != nil {
		return nil, fmt.Errorf(errParseFileFmt, path, err)
	}
	return &plan, nil
}

func saveCanonicalPlan(projectPath string, plan *CanonicalPlan) error {
	dir := filepath.Join(plansBaseDir(projectPath), plan.ID)
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yamlMarshal(plan)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, workflowPlanFileName), content, 0644)
}

// loadCanonicalTasks resolves planID's canonical TASKS.yaml view, honoring the
// coordination-state backend. git-ref: project the view from the per-task state
// blobs on refs/agents/state (a local, read-your-writes-safe ref), gracefully
// falling back to the per-worktree working copy when the plan has no task blobs
// on the ref. git-ref takes PRECEDENCE over the read_from=master shim — the
// fallback reads the working copy directly, never origin/<default-branch>.
// Default/local: the existing readCanonicalStateFile seam (worktree, or
// read_from=master).
func loadCanonicalTasks(projectPath, planID string) (*CanonicalTaskFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, workflowTasksFileName)
	if useGitRefBackend(projectPath) {
		tf, ok, err := loadCanonicalTasksFromStateRef(projectPath, planID)
		if err != nil {
			return nil, err
		}
		if ok {
			return tf, nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return unmarshalCanonicalTaskFile(path, content)
	}
	content, err := readCanonicalStateFile(projectPath, path)
	if err != nil {
		return nil, err
	}
	return unmarshalCanonicalTaskFile(path, content)
}

// unmarshalCanonicalTaskFile decodes TASKS.yaml bytes into a CanonicalTaskFile,
// wrapping a parse failure with the source path for diagnostics.
func unmarshalCanonicalTaskFile(path string, content []byte) (*CanonicalTaskFile, error) {
	var tf CanonicalTaskFile
	if err := yaml.Unmarshal(content, &tf); err != nil {
		return nil, fmt.Errorf(errParseFileFmt, path, err)
	}
	return &tf, nil
}

func loadCanonicalSlices(projectPath, planID string) (*CanonicalSliceFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "SLICES.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf CanonicalSliceFile
	if err := yaml.Unmarshal(content, &sf); err != nil {
		return nil, fmt.Errorf(errParseFileFmt, path, err)
	}
	return &sf, nil
}

func saveCanonicalTasks(projectPath string, tf *CanonicalTaskFile) error {
	dir := filepath.Join(plansBaseDir(projectPath), tf.PlanID)
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yamlMarshal(tf)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, workflowTasksFileName), content, 0644)
}

// saveCanonicalTasksMirrored is the canonical-write choke point for plan tasks:
// it writes the working-copy projection, then mirrors the changed task and plan
// through the configured state-ref failure policy.
func saveCanonicalTasksMirrored(projectPath string, tf *CanonicalTaskFile, changedTaskID string) error {
	if err := saveCanonicalTasks(projectPath, tf); err != nil {
		return err
	}
	return mirrorBestEffortOrPropagate(projectPath, "canonical tasks saved",
		mirrorTransitionToStateRef(projectPath, tf.PlanID, changedTaskID))
}

// saveCanonicalTaskRepointMirrored uses the same canonical save choke point as
// other task writers, then performs a structural CAS transform against the
// latest ref snapshot. JSON mode sends an additive-backend warning to stderr so
// stdout remains valid JSON.
func saveCanonicalTaskRepointMirrored(projectPath string, tf *CanonicalTaskFile, in taskRepointInputs) error {
	if err := saveCanonicalTasks(projectPath, tf); err != nil {
		return err
	}
	mirrorErr := mirrorTaskRepointToStateRef(projectPath, tf, in)
	if (in.JSON || deps.Flags.JSON()) && mirrorErr != nil && !useGitRefBackend(projectPath) {
		_, _ = fmt.Fprintf(os.Stderr, "warning: canonical tasks saved but failed to mirror to %s: %v\n", stateRefName, mirrorErr)
		return nil
	}
	return mirrorBestEffortOrPropagate(projectPath, "canonical tasks saved", mirrorErr)
}

// mirrorBestEffortOrPropagate applies the mode-aware choke-point mirror failure
// policy. Under backend=git-ref the ref is the read source, so failures
// propagate. Under additive write_to=state-ref mode, reads still hit the
// worktree, so the failure is warned and swallowed.
func mirrorBestEffortOrPropagate(projectPath, what string, err error) error {
	if err == nil {
		return nil
	}
	if useGitRefBackend(projectPath) {
		return fmt.Errorf("%s but failed to mirror to %s: %w", what, stateRefName, err)
	}
	ui.Warn(fmt.Sprintf("%s but failed to mirror to %s: %v", what, stateRefName, err))
	return nil
}

// saveCanonicalPlanMirrored is the canonical-write choke point for a plan-only
// write (plan update): it writes PLAN.yaml to the working copy and, when the
// backend mirrors canonical writes, overwrites PLAN.yaml on refs/agents/state
// while seeding the plan's existing task blobs only-if-absent (changedTaskID is
// empty — no task blob is authoritatively changed). The mirror failure policy is
// mode-aware, identical to saveCanonicalTasksMirrored (propagate under
// backend=git-ref, warn under the additive write_to=state-ref mode).
func saveCanonicalPlanMirrored(projectPath string, plan *CanonicalPlan) error {
	if err := saveCanonicalPlan(projectPath, plan); err != nil {
		return err
	}
	return mirrorBestEffortOrPropagate(projectPath, "canonical plan saved",
		mirrorTransitionToStateRef(projectPath, plan.ID, ""))
}

// tasksLockPath returns the sidecar-lock target for planID's TASKS.yaml — the
// same path loadCanonicalTasks/saveCanonicalTasks read and write, since
// agentslock.AcquireFileLock locks by the protected file's own path rather
// than a caller-supplied sidecar name.
func tasksLockPath(projectPath, planID string) string {
	return filepath.Join(plansBaseDir(projectPath), planID, workflowTasksFileName)
}

// withTasksLock runs fn while holding the cross-process advisory lock on
// planID's TASKS.yaml, serializing the load -> mutate -> save critical
// section against every other da process mutating the same plan's canonical
// task file. This closes the lost-update race between concurrent
// `da workflow` invocations (two orchestrators, a parallel worker batch, …)
// that would otherwise race loadCanonicalTasks/saveCanonicalTasks against
// each other.
//
// This is an INTERIM band-aid: the strategic fix is the WorkStore/backend
// storage abstraction (.agents/workflow/specs/work-tracking-storage-
// abstraction/design.md D2/D5); file-locking the existing YAML
// read-modify-write holds the line until that cutover lands.
//
// fn must perform the entire load, mutate, and save of planID's TASKS.yaml —
// acquiring the lock any later, or releasing it any earlier, reopens the
// race. A bounded acquisition timeout (agentslock's lockAcquireTimeout)
// surfaces as a wrapped error rather than silently proceeding unlocked.
func withTasksLock(projectPath, planID string, fn func() error) (err error) {
	path := tasksLockPath(projectPath, planID)
	release, lockErr := agentslock.AcquireFileLock(path)
	if lockErr != nil {
		return fmt.Errorf("TASKS.yaml locked by another process, timed out waiting: %w", lockErr)
	}
	defer func() {
		if relErr := release(); relErr != nil && err == nil {
			err = fmt.Errorf("release TASKS.yaml lock: %w", relErr)
		}
	}()
	return fn()
}

func collectCanonicalPlans(projectPath string) ([]workflowCanonicalPlanSummary, []string) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, []string{"canonical plans unreadable: " + err.Error()}
	}
	var summaries []workflowCanonicalPlanSummary
	var warnings []string
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plan %s unreadable: %v", id, err))
			continue
		}
		summary := workflowCanonicalPlanSummary{
			ID:               plan.ID,
			Title:            plan.Title,
			Status:           plan.Status,
			CurrentFocusTask: plan.CurrentFocusTask,
		}
		if tf, err := loadCanonicalTasks(projectPath, id); err == nil {
			summary.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
			for _, t := range tf.Tasks {
				switch t.Status {
				case "pending", "in_progress":
					summary.PendingCount++
				case "blocked":
					summary.BlockedCount++
				case "completed":
					summary.CompletedCount++
				}
			}
		}
		summaries = append(summaries, summary)
	}
	if summaries == nil {
		summaries = []workflowCanonicalPlanSummary{}
	}
	return summaries, warnings
}

// collectDraftPlanIDs returns the IDs of all canonical plans whose status is
// "draft". Draft plans are silently skipped by selectAllEligibleTasks /
// selectNextCanonicalTask; this helper lets eligible/next surface them so an
// operator is never told "no eligible tasks" while plans sit unactivated.
// Returns an empty (non-nil) slice when no drafts exist or on read errors.
func collectDraftPlanIDs(projectPath string) []string {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return []string{}
	}
	drafts := make([]string, 0, len(ids))
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			continue
		}
		if plan.Status == "draft" {
			drafts = append(drafts, plan.ID)
		}
	}
	return drafts
}

// renderDraftPlansHint writes the "draft plans not yet activated" guidance to
// out. It is emitted by `workflow eligible` and `workflow next` when no
// actionable task is found AND there are drafts the operator may have
// forgotten to promote. Kept in one place so the wording stays consistent
// across surfaces.
func renderDraftPlansHint(out io.Writer, drafts []string) {
	if len(drafts) == 0 {
		return
	}
	fmt.Fprintf(out, "Found %d draft plan(s) not yet activated: %s\n", len(drafts), strings.Join(drafts, ", "))
	fmt.Fprintln(out, "  Run `da workflow plan update --status active --plan <id>` to activate.")
}

func isValidPlanStatus(s string) bool {
	switch s {
	case "draft", "active", "paused", "completed", "archived":
		return true
	default:
		return false
	}
}

func runWorkflowPlanList() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	ids, err := listCanonicalPlanIDs(project.Path)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, "No canonical plans found.")
		fmt.Fprintf(os.Stdout, "  Create one at: %s\n", config.DisplayPath(filepath.Join(plansBaseDir(project.Path), "<plan-id>", workflowPlanFileName)))
		return nil
	}
	if deps.Flags.JSON() {
		summaries, _ := collectCanonicalPlans(project.Path)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}
	ui.Header("Canonical Plans")
	for _, id := range ids {
		plan, err := loadCanonicalPlan(project.Path, id)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  %s (unreadable: %v)\n", id, err)
			continue
		}
		focus := ""
		if plan.CurrentFocusTask != "" {
			focus = "  focus: " + plan.CurrentFocusTask
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s (%s)%s\n", plan.ID, plan.Title, plan.Status, focus)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPlanShow(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf(errPlanNotFoundWithCause, planID, err)
	}
	tf, tasksErr := loadCanonicalTasks(project.Path, planID)
	sf, slicesErr := loadCanonicalSlices(project.Path, planID)

	if deps.Flags.JSON() {
		out := map[string]interface{}{"plan": plan}
		if tasksErr == nil {
			out["tasks"] = tf
		}
		if slicesErr == nil {
			out["slices"] = sf
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	renderPlanShowHeader(plan)
	if tasksErr != nil {
		fmt.Fprintf(os.Stdout, "  (no %s found)\n", workflowTasksFileName)
		return nil
	}
	renderPlanShowTasks(tf)
	renderPlanShowSlices(sf, slicesErr)
	return nil
}

func renderPlanShowHeader(plan *CanonicalPlan) {
	ui.Header(plan.Title)
	ui.Section("Plan")
	fmt.Fprintf(os.Stdout, "  id: %s\n", plan.ID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", plan.Status)
	fmt.Fprintf(os.Stdout, "  created: %s\n", plan.CreatedAt)
	fmt.Fprintf(os.Stdout, "  updated: %s\n", plan.UpdatedAt)
	if plan.Owner != "" {
		fmt.Fprintf(os.Stdout, "  owner: %s\n", plan.Owner)
	}
	if plan.Summary != "" {
		fmt.Fprintf(os.Stdout, "  summary: %s\n", plan.Summary)
	}
	if plan.SuccessCriteria != "" {
		fmt.Fprintf(os.Stdout, "  success criteria: %s\n", plan.SuccessCriteria)
	}
	if plan.CurrentFocusTask != "" {
		fmt.Fprintf(os.Stdout, "  focus task: %s\n", plan.CurrentFocusTask)
	}
	fmt.Fprintln(os.Stdout)
}

func planShowTaskMarker(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "in_progress":
		return "▶"
	case "blocked":
		return "✗"
	default:
		return " "
	}
}

func summarizeTaskCounts(tasks []CanonicalTask) (pending, blocked, completed, total int) {
	for _, t := range tasks {
		total++
		switch t.Status {
		case "pending", "in_progress":
			pending++
		case "blocked":
			blocked++
		case "completed":
			completed++
		}
	}
	return
}

func renderPlanShowTasks(tf *CanonicalTaskFile) {
	pending, blocked, completed, total := summarizeTaskCounts(tf.Tasks)
	ui.Section("Tasks")
	fmt.Fprintf(os.Stdout, "  total: %d   pending: %d   blocked: %d   completed: %d\n\n", total, pending, blocked, completed)
	for _, t := range tf.Tasks {
		fmt.Fprintf(os.Stdout, "  [%s] %s  %s\n", planShowTaskMarker(t.Status), t.ID, t.Title)
	}
	fmt.Fprintln(os.Stdout)
}

func renderPlanShowSlices(sf *CanonicalSliceFile, slicesErr error) {
	if slicesErr == nil {
		ui.Section("Slices")
		fmt.Fprintf(os.Stdout, "  total: %d\n\n", len(sf.Slices))
		for _, slice := range sf.Slices {
			fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID)
		}
	}
	fmt.Fprintln(os.Stdout)
}

type workflowPlanGraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	PlanID string `json:"plan_id,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
}

type workflowPlanGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type workflowPlanGraph struct {
	PlanFilter string                  `json:"plan_filter,omitempty"`
	Nodes      []workflowPlanGraphNode `json:"nodes"`
	Edges      []workflowPlanGraphEdge `json:"edges"`
	Warnings   []string                `json:"warnings,omitempty"`
}

func renderPlanGraphSliceDeps(graph *workflowPlanGraph, nodeByID map[string]workflowPlanGraphNode, sliceNode workflowPlanGraphNode) {
	for _, sliceEdge := range graph.Edges {
		if sliceEdge.From != sliceNode.ID || sliceEdge.Type != "depends_on" {
			continue
		}
		targetLabel := sliceEdge.To
		if targetNode, ok := nodeByID[sliceEdge.To]; ok {
			targetLabel = targetNode.Label
		}
		fmt.Fprintf(os.Stdout, "            depends_on: %s\n", targetLabel)
	}
}

func renderPlanGraphTaskChildren(graph *workflowPlanGraph, nodeByID map[string]workflowPlanGraphNode, taskNode workflowPlanGraphNode) {
	for _, taskEdge := range graph.Edges {
		if taskEdge.From == taskNode.ID && taskEdge.Type == "contains" {
			sliceNode, ok := nodeByID[taskEdge.To]
			if ok && sliceNode.Kind == "slice" {
				fmt.Fprintf(os.Stdout, "         => [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(sliceNode.ID, planTaskSliceIDPrefix+sliceNode.PlanID+"/"), planTaskSliceIDPrefix), sliceNode.Label, sliceNode.Status)
				renderPlanGraphSliceDeps(graph, nodeByID, sliceNode)
			}
		}
		if taskEdge.From != taskNode.ID || (taskEdge.Type != "depends_on" && taskEdge.Type != "blocks") {
			continue
		}
		targetLabel := taskEdge.To
		if targetNode, ok := nodeByID[taskEdge.To]; ok {
			targetLabel = targetNode.Label
		}
		fmt.Fprintf(os.Stdout, "         %s: %s\n", taskEdge.Type, targetLabel)
	}
}

func renderPlanGraphPlanNode(graph *workflowPlanGraph, nodeByID map[string]workflowPlanGraphNode, node workflowPlanGraphNode) {
	fmt.Fprintf(os.Stdout, "  [%s] %s (%s)\n", strings.TrimPrefix(node.ID, "plan:"), node.Label, node.Status)
	for _, edge := range graph.Edges {
		if edge.Type != "contains" || edge.From != node.ID {
			continue
		}
		taskNode, ok := nodeByID[edge.To]
		if !ok {
			continue
		}
		fmt.Fprintf(os.Stdout, "      -> [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(taskNode.ID, "task:"+taskNode.PlanID+"/"), "task:"), taskNode.Label, taskNode.Status)
		renderPlanGraphTaskChildren(graph, nodeByID, taskNode)
	}
}

func runWorkflowPlanGraph(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	graph, err := buildWorkflowPlanGraph(project.Path, planID)
	if err != nil {
		return err
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(graph)
	}

	title := "Canonical Plan Graph"
	if planID != "" {
		title += ": " + planID
	}
	ui.Header(title)

	nodeByID := make(map[string]workflowPlanGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}

	for _, node := range graph.Nodes {
		if node.Kind != "plan" {
			continue
		}
		renderPlanGraphPlanNode(graph, nodeByID, node)
	}

	for _, warning := range graph.Warnings {
		ui.Warn(warning)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func resolvePlanGraphPlanIDs(projectPath, planID string) ([]string, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if planID == "" {
		return ids, nil
	}
	for _, id := range ids {
		if id == planID {
			return []string{planID}, nil
		}
	}
	return nil, fmt.Errorf(errPlanNotFoundFmt, planID)
}

func appendPlanGraphTaskNodes(graph *workflowPlanGraph, plan *CanonicalPlan, tasks []CanonicalTask, planNodeID string) map[string]string {
	taskIDs := make(map[string]string, len(tasks))
	for _, task := range tasks {
		taskNodeID := "task:" + plan.ID + "/" + task.ID
		taskIDs[task.ID] = taskNodeID
		graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
			ID:     taskNodeID,
			Kind:   "task",
			PlanID: plan.ID,
			Label:  task.Title,
			Status: task.Status,
		})
		graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
			From: planNodeID,
			To:   taskNodeID,
			Type: "contains",
		})
	}
	return taskIDs
}

func appendPlanGraphSliceNodes(graph *workflowPlanGraph, plan *CanonicalPlan, slices []CanonicalSlice, taskIDs map[string]string) map[string]string {
	sliceIDs := make(map[string]string, len(slices))
	for _, slice := range slices {
		parentTaskNodeID, ok := taskIDs[slice.ParentTaskID]
		if !ok {
			graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s references unknown parent task %s", plan.ID, slice.ID, slice.ParentTaskID))
			continue
		}
		sliceNodeID := planTaskSliceIDPrefix + plan.ID + "/" + slice.ID
		sliceIDs[slice.ID] = sliceNodeID
		graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
			ID:     sliceNodeID,
			Kind:   "slice",
			PlanID: plan.ID,
			TaskID: slice.ParentTaskID,
			Label:  slice.Title,
			Status: slice.Status,
		})
		graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
			From: parentTaskNodeID,
			To:   sliceNodeID,
			Type: "contains",
		})
	}
	return sliceIDs
}

func appendPlanGraphSliceDeps(graph *workflowPlanGraph, plan *CanonicalPlan, slices []CanonicalSlice, sliceIDs map[string]string) {
	for _, slice := range slices {
		fromID, ok := sliceIDs[slice.ID]
		if !ok {
			continue
		}
		for _, dep := range slice.DependsOn {
			toID, ok := sliceIDs[dep]
			if !ok {
				graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s depends on unknown slice %s", plan.ID, slice.ID, dep))
				continue
			}
			graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
				From: fromID,
				To:   toID,
				Type: "depends_on",
			})
		}
	}
}

func appendPlanGraphTaskRelationEdges(graph *workflowPlanGraph, plan *CanonicalPlan, tasks []CanonicalTask, taskIDs map[string]string) {
	for _, task := range tasks {
		fromID := taskIDs[task.ID]
		for _, dep := range task.DependsOn {
			toID, ok := taskIDs[dep]
			if !ok {
				graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s depends on unknown task %s", plan.ID, task.ID, dep))
				continue
			}
			graph.Edges = append(graph.Edges, workflowPlanGraphEdge{From: fromID, To: toID, Type: "depends_on"})
		}
		for _, blocked := range task.Blocks {
			toID, ok := taskIDs[blocked]
			if !ok {
				graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s blocks unknown task %s", plan.ID, task.ID, blocked))
				continue
			}
			graph.Edges = append(graph.Edges, workflowPlanGraphEdge{From: fromID, To: toID, Type: "blocks"})
		}
	}
}

func appendPlanToWorkflowGraph(graph *workflowPlanGraph, projectPath, id string) error {
	plan, err := loadCanonicalPlan(projectPath, id)
	if err != nil {
		return fmt.Errorf("load plan %q: %w", id, err)
	}
	tf, err := loadCanonicalTasks(projectPath, id)
	if err != nil {
		return fmt.Errorf("load tasks for plan %q: %w", id, err)
	}
	sf, slicesErr := loadCanonicalSlices(projectPath, id)
	if slicesErr != nil && !os.IsNotExist(slicesErr) {
		return fmt.Errorf("load slices for plan %q: %w", id, slicesErr)
	}

	planNodeID := "plan:" + plan.ID
	graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
		ID:     planNodeID,
		Kind:   "plan",
		Label:  plan.Title,
		Status: plan.Status,
	})

	taskIDs := appendPlanGraphTaskNodes(graph, plan, tf.Tasks, planNodeID)
	if slicesErr == nil {
		sliceIDs := appendPlanGraphSliceNodes(graph, plan, sf.Slices, taskIDs)
		appendPlanGraphSliceDeps(graph, plan, sf.Slices, sliceIDs)
	}
	appendPlanGraphTaskRelationEdges(graph, plan, tf.Tasks, taskIDs)
	return nil
}

func buildWorkflowPlanGraph(projectPath, planID string) (*workflowPlanGraph, error) {
	ids, err := resolvePlanGraphPlanIDs(projectPath, planID)
	if err != nil {
		return nil, err
	}

	graph := &workflowPlanGraph{
		PlanFilter: planID,
		Nodes:      []workflowPlanGraphNode{},
		Edges:      []workflowPlanGraphEdge{},
		Warnings:   []string{},
	}

	for _, id := range ids {
		if err := appendPlanToWorkflowGraph(graph, projectPath, id); err != nil {
			return nil, err
		}
	}
	return graph, nil
}

func runWorkflowTasks(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf(errPlanNotFoundWithCause, planID, err)
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf(errTasksForPlanNotFoundFmt, planID, err)
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tf)
	}
	ui.Header("Tasks: " + planID)
	for _, t := range tf.Tasks {
		depsLabel := ""
		if len(t.DependsOn) > 0 {
			depsLabel = "  depends: " + strings.Join(t.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)%s\n", t.ID, t.Title, t.Status, depsLabel)
		if t.Notes != "" {
			fmt.Fprintf(os.Stdout, "      note: %s\n", t.Notes)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowSlices(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf(errPlanNotFoundWithCause, planID, err)
	}
	sf, err := loadCanonicalSlices(project.Path, planID)
	if err != nil {
		return fmt.Errorf("slices for plan %q not found: %w", planID, err)
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sf)
	}
	ui.Header("Slices: " + planID)
	for _, slice := range sf.Slices {
		depsLabel := ""
		if len(slice.DependsOn) > 0 {
			depsLabel = "  depends: " + strings.Join(slice.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s%s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID, depsLabel)
		if slice.Summary != "" {
			fmt.Fprintf(os.Stdout, "      summary: %s\n", slice.Summary)
		}
		if len(slice.WriteScope) > 0 {
			fmt.Fprintf(os.Stdout, "      write scope: %s\n", strings.Join(slice.WriteScope, ", "))
		}
		if slice.VerificationFocus != "" {
			fmt.Fprintf(os.Stdout, "      verification: %s\n", slice.VerificationFocus)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type workflowNextTaskSuggestion struct {
	PlanID               string   `json:"plan_id"`
	PlanTitle            string   `json:"plan_title"`
	TaskID               string   `json:"task_id"`
	TaskTitle            string   `json:"task_title"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason"`
	WriteScope           []string `json:"write_scope,omitempty"`
	VerificationRequired bool     `json:"verification_required"`
	DependsOn            []string `json:"depends_on,omitempty"`
	AppType              string   `json:"app_type,omitempty"`
	ConflictsWith        []string `json:"conflicts_with"`
}

// AnnotatedTask enriches a workflowNextTaskSuggestion with conflict detection
// and evidence fields populated by computeWriteScopeConflicts and the eligible command.
// All slice fields are initialized to []string{} (not nil) so they marshal to [] not null.
type AnnotatedTask struct {
	workflowNextTaskSuggestion
	ConflictsWith      []string `json:"conflicts_with"`
	HasEvidence        bool     `json:"has_evidence"`
	EvidenceConfidence string   `json:"evidence_confidence"`
	WriteScopeDeclared bool     `json:"write_scope_declared"`
}

// eligibleOutput is the full JSON output of `workflow eligible`.
type eligibleOutput struct {
	EligibleTasks []AnnotatedTask     `json:"eligible_tasks"`
	MaxBatch      []string            `json:"max_batch"`
	ConflictGraph map[string][]string `json:"conflict_graph"`
	TotalEligible int                 `json:"total_eligible"`
	MaxParallel   int                 `json:"max_parallel"`
	DraftPlans    []string            `json:"draft_plans"`
}

// writeScopeConflictResult is the output of computeWriteScopeConflicts.
// All slice/map fields use non-nil zero values per the additive struct pattern.
type writeScopeConflictResult struct {
	EligibleTasks []workflowNextTaskSuggestion `json:"eligible_tasks"`
	MaxBatch      []string                     `json:"max_batch"`
	ConflictGraph map[string][]string          `json:"conflict_graph"`
}

// writeScopesConflict reports whether two write_scope lists overlap.
// Two scopes conflict when any path in one is a prefix of (or equal to) any path in the other.
// Prefix matching is directory-aware: "commands/workflow/" conflicts with
// "commands/workflow/plan_task.go", and exact matches also conflict.
// Uses the package-level scopePathsOverlap (defined in delegation.go).
func writeScopesConflict(a, b []string) bool {
	for _, pa := range a {
		for _, pb := range b {
			if scopePathsOverlap(pa, pb) {
				return true
			}
		}
	}
	return false
}

// computeWriteScopeConflicts annotates each task in the input slice with
// ConflictsWith (the IDs of other tasks whose write_scope overlaps its own),
// then computes the MaxNonConflictingBatch (the largest subset of tasks with
// zero pairwise conflicts, greedy by input order) and builds a ConflictGraph.
//
// Schema rule: ConflictsWith per task is []string{} not nil. MaxBatch is []string{} not nil.
// ConflictGraph values are []string{} not nil for every key.
func computeWriteScopeConflicts(tasks []workflowNextTaskSuggestion) writeScopeConflictResult {
	initializeTaskConflicts(tasks)
	populateTaskConflicts(tasks)
	conflictGraph := buildTaskConflictGraph(tasks)
	maxBatch := greedyNonConflictingBatch(tasks)

	return writeScopeConflictResult{
		EligibleTasks: tasks,
		MaxBatch:      maxBatch,
		ConflictGraph: conflictGraph,
	}
}

func initializeTaskConflicts(tasks []workflowNextTaskSuggestion) {
	for i := range tasks {
		tasks[i].ConflictsWith = []string{}
	}
}

func populateTaskConflicts(tasks []workflowNextTaskSuggestion) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if !writeScopesConflict(tasks[i].WriteScope, tasks[j].WriteScope) {
				continue
			}
			tasks[i].ConflictsWith = append(tasks[i].ConflictsWith, tasks[j].TaskID)
			tasks[j].ConflictsWith = append(tasks[j].ConflictsWith, tasks[i].TaskID)
		}
	}
}

func buildTaskConflictGraph(tasks []workflowNextTaskSuggestion) map[string][]string {
	conflictGraph := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		if t.ConflictsWith == nil {
			conflictGraph[t.TaskID] = []string{}
			continue
		}
		conflictGraph[t.TaskID] = t.ConflictsWith
	}
	return conflictGraph
}

func greedyNonConflictingBatch(tasks []workflowNextTaskSuggestion) []string {
	inBatch := make(map[string]bool, len(tasks))
	var maxBatch []string
	for _, t := range tasks {
		canAdd := true
		for _, conflictID := range t.ConflictsWith {
			if inBatch[conflictID] {
				canAdd = false
				break
			}
		}
		if canAdd {
			inBatch[t.TaskID] = true
			maxBatch = append(maxBatch, t.TaskID)
		}
	}
	return maxBatch
}

type workflowCompletionScopeState struct {
	Scope       []string                    `json:"scope"`
	State       string                      `json:"state"`
	Next        *workflowNextTaskSuggestion `json:"next,omitempty"`
	PausedPlans []string                    `json:"paused_plans,omitempty"`
	LockedPlans []string                    `json:"locked_plans,omitempty"`
}

func runWorkflowNext(explicitPlanID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	suggestion, err := selectNextCanonicalTask(project.Path, explicitPlanID)
	if err != nil {
		return err
	}
	if suggestion == nil {
		drafts := collectDraftPlanIDs(project.Path)
		if deps.Flags.JSON() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"suggestion":  nil,
				"draft_plans": drafts,
			})
		}
		fmt.Fprintln(os.Stdout, "No actionable canonical task found.")
		fmt.Fprintln(os.Stdout, "  Active plans are completed, blocked by dependencies, already delegated, or missing TASKS.yaml.")
		if len(drafts) > 0 {
			fmt.Fprintln(os.Stdout)
			renderDraftPlansHint(os.Stdout, drafts)
		}
		return nil
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestion)
	}

	ui.Header("Next Canonical Task")
	fmt.Fprintf(os.Stdout, "  plan: %s  [%s]\n", suggestion.PlanTitle, suggestion.PlanID)
	fmt.Fprintf(os.Stdout, "  task: %s  [%s]\n", suggestion.TaskTitle, suggestion.TaskID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", suggestion.Status)
	fmt.Fprintf(os.Stdout, "  reason: %s\n", suggestion.Reason)
	if len(suggestion.DependsOn) > 0 {
		fmt.Fprintf(os.Stdout, "  depends on: %s\n", strings.Join(suggestion.DependsOn, ", "))
	}
	if len(suggestion.WriteScope) > 0 {
		fmt.Fprintf(os.Stdout, "  write scope: %s\n", strings.Join(suggestion.WriteScope, ", "))
	}
	if suggestion.VerificationRequired {
		fmt.Fprintln(os.Stdout, "  verification: required")
	} else {
		fmt.Fprintln(os.Stdout, "  verification: optional")
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// annotateEligibleTasks enriches a slice of workflowNextTaskSuggestion with
// conflict detection (ConflictsWith), evidence sidecar data (HasEvidence,
// EvidenceConfidence), and write_scope_declared.
func annotateEligibleTasks(projectPath string, tasks []workflowNextTaskSuggestion) []AnnotatedTask {
	// Run conflict detection first (populates ConflictsWith on each task in-place).
	conflictResult := computeWriteScopeConflicts(tasks)

	annotated := make([]AnnotatedTask, len(conflictResult.EligibleTasks))
	for i, t := range conflictResult.EligibleTasks {
		at := AnnotatedTask{
			workflowNextTaskSuggestion: t,
			ConflictsWith:              t.ConflictsWith,
			WriteScopeDeclared:         len(t.WriteScope) > 0,
		}

		// Check for evidence sidecar. A missing sidecar (os.IsNotExist) is
		// legitimate absence — most tasks have none. A REAL read error
		// (permission denied, TOCTOU) is silently indistinguishable from
		// "no evidence" without this check, which would defeat eligible's
		// evidence-confidence signal without any indication why.
		sidecarPath := deriveScopeEvidencePath(projectPath, t.PlanID, t.TaskID)
		data, err := os.ReadFile(sidecarPath)
		if err == nil {
			at.HasEvidence = true
			// Parse confidence from sidecar.
			var ev struct {
				Confidence string `yaml:"confidence"`
			}
			if parseErr := yaml.Unmarshal(data, &ev); parseErr == nil && ev.Confidence != "" {
				at.EvidenceConfidence = ev.Confidence
			} else {
				at.EvidenceConfidence = "none"
			}
		} else {
			at.HasEvidence = false
			at.EvidenceConfidence = "none"
			if !os.IsNotExist(err) {
				ui.Warn(fmt.Sprintf("evidence sidecar unreadable for %s/%s, treating as no-evidence: %v", t.PlanID, t.TaskID, err))
			}
		}

		annotated[i] = at
	}
	return annotated
}

// runWorkflowEligible implements `workflow eligible`: lists all unblocked tasks
// across active plans, annotated with conflict detection and evidence data.
func resolveEligibleEffectiveLimit(prefs WorkflowPreferences, limit int) (effective, maxWorkers int) {
	maxWorkers = 1
	if prefs.Execution.MaxParallelWorkers != nil {
		maxWorkers = *prefs.Execution.MaxParallelWorkers
	}
	effective = maxWorkers
	if limit > 0 {
		effective = limit
	}
	return effective, maxWorkers
}

func eligibleAnnotatedWithConflicts(projectPath string, tasks []workflowNextTaskSuggestion) ([]AnnotatedTask, writeScopeConflictResult) {
	annotated := annotateEligibleTasks(projectPath, tasks)
	taskSuggestions := make([]workflowNextTaskSuggestion, len(annotated))
	for i, at := range annotated {
		taskSuggestions[i] = at.workflowNextTaskSuggestion
	}
	conflictResult := computeWriteScopeConflicts(taskSuggestions)
	for i := range annotated {
		if i < len(conflictResult.EligibleTasks) {
			annotated[i].ConflictsWith = conflictResult.EligibleTasks[i].ConflictsWith
		}
	}
	return annotated, conflictResult
}

func renderEligibleTask(at AnnotatedTask) {
	scopeStr := strings.Join(at.WriteScope, ", ")
	if !at.WriteScopeDeclared {
		scopeStr = "(none) [no write_scope declared]"
	}
	evidenceStr := fmt.Sprintf("  evidence: %s (confidence: %s)", fmt.Sprintf("%v", at.HasEvidence), at.EvidenceConfidence)
	fmt.Fprintf(os.Stdout, "  [%s/%s] %s  (%s)\n", at.PlanID, at.TaskID, at.TaskTitle, at.Status)
	fmt.Fprintf(os.Stdout, "      scope: %s\n", scopeStr)
	fmt.Fprintf(os.Stdout, "     %s\n", evidenceStr)
	if len(at.ConflictsWith) > 0 {
		fmt.Fprintf(os.Stdout, "     %s\n", "  conflicts: "+strings.Join(at.ConflictsWith, ", "))
	}
}

func renderEligibleOutput(out eligibleOutput, maxWorkers, limit int) {
	ui.Header("Eligible Tasks")
	for _, at := range out.EligibleTasks {
		renderEligibleTask(at)
	}
	fmt.Fprintln(os.Stdout)
	limitLabel := fmt.Sprintf("max_parallel_workers=%d", maxWorkers)
	if limit > 0 {
		limitLabel = fmt.Sprintf("--limit=%d", limit)
	}
	fmt.Fprintf(os.Stdout, "%d tasks eligible, %d can run in parallel (limited by %s)\n",
		out.TotalEligible, out.MaxParallel, limitLabel)
	if len(out.MaxBatch) > 0 {
		fmt.Fprintf(os.Stdout, "  max batch: %s\n", strings.Join(out.MaxBatch, ", "))
	}
	if out.TotalEligible == 0 && len(out.DraftPlans) > 0 {
		fmt.Fprintln(os.Stdout)
		renderDraftPlansHint(os.Stdout, out.DraftPlans)
	}
	fmt.Fprintln(os.Stdout)
}

func runWorkflowEligible(planFilter string, limit int) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	prefs, err := resolvePreferences(project.Path, project.Name)
	if err != nil {
		return err
	}
	effectiveLimit, maxWorkers := resolveEligibleEffectiveLimit(prefs, limit)

	planIDs := parsePlanIDFilter(planFilter)
	tasks, err := selectAllEligibleTasks(project.Path, planIDs)
	if err != nil {
		return err
	}

	if effectiveLimit > 0 && len(tasks) > effectiveLimit {
		tasks = tasks[:effectiveLimit]
	}

	annotated, conflictResult := eligibleAnnotatedWithConflicts(project.Path, tasks)

	out := eligibleOutput{
		EligibleTasks: annotated,
		MaxBatch:      conflictResult.MaxBatch,
		ConflictGraph: conflictResult.ConflictGraph,
		TotalEligible: len(annotated),
		MaxParallel:   len(conflictResult.MaxBatch),
		DraftPlans:    collectDraftPlanIDs(project.Path),
	}
	if out.EligibleTasks == nil {
		out.EligibleTasks = []AnnotatedTask{}
	}
	if out.DraftPlans == nil {
		out.DraftPlans = []string{}
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	renderEligibleOutput(out, maxWorkers, limit)
	return nil
}

func runWorkflowComplete(explicitPlanID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if strings.TrimSpace(explicitPlanID) == "" {
		return fmt.Errorf("--plan must not be empty")
	}
	completion, err := collectWorkflowCompletionState(project.Path, explicitPlanID)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(completion)
	}

	ui.Header("Scoped Plan Completion")
	fmt.Fprintf(os.Stdout, "  scope: %s\n", strings.Join(completion.Scope, ", "))
	fmt.Fprintf(os.Stdout, "  state: %s\n", completion.State)
	if completion.Next != nil {
		fmt.Fprintf(os.Stdout, "  next: %s  [%s]\n", completion.Next.TaskTitle, completion.Next.TaskID)
		fmt.Fprintf(os.Stdout, "  plan: %s  [%s]\n", completion.Next.PlanTitle, completion.Next.PlanID)
		fmt.Fprintf(os.Stdout, "  reason: %s\n", completion.Next.Reason)
	}
	if len(completion.PausedPlans) > 0 {
		fmt.Fprintf(os.Stdout, "  paused plans: %s\n", strings.Join(completion.PausedPlans, ", "))
	}
	if len(completion.LockedPlans) > 0 {
		fmt.Fprintf(os.Stdout, "  locked plans: %s\n", strings.Join(completion.LockedPlans, ", "))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func parsePlanIDFilter(planFilter string) []string {
	planFilter = strings.TrimSpace(planFilter)
	if planFilter == "" {
		return nil
	}

	seen := make(map[string]bool)
	ids := make([]string, 0, 4)
	for _, raw := range strings.Split(planFilter, ",") {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func filterPlanIDsUnlocked(ids []string, locked map[string]bool) []string {
	if len(locked) == 0 {
		return ids
	}
	var out []string
	for _, id := range ids {
		if !locked[id] {
			out = append(out, id)
		}
	}
	return out
}

// activeDelegationPlanIDs returns plan ids that currently have a pending or active delegation.
func activeDelegationPlanIDs(delegations []DelegationContract) map[string]bool {
	m := make(map[string]bool)
	for _, c := range delegations {
		if c.Status != "pending" && c.Status != "active" {
			continue
		}
		if c.ParentPlanID != "" {
			m[c.ParentPlanID] = true
		}
	}
	return m
}

func filterPlanIDsLocked(ids []string, locked map[string]bool) []string {
	if len(locked) == 0 {
		return ids
	}
	var out []string
	for _, id := range ids {
		if locked[id] {
			out = append(out, id)
		}
	}
	return out
}

func validateScopeIDsAgainstAvailable(scopeIDs, ids []string) ([]string, error) {
	if len(scopeIDs) == 0 {
		return scopeIDs, nil
	}
	available := make(map[string]bool, len(ids))
	for _, id := range ids {
		available[id] = true
	}
	filtered := make([]string, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		if !available[id] {
			return nil, fmt.Errorf(errPlanNotFoundFmt, id)
		}
		filtered = append(filtered, id)
	}
	return filtered, nil
}

func partitionScopePlansByStatus(projectPath string, scopeIDs []string, lockedPlans map[string]bool) (paused, locked []string, err error) {
	paused = make([]string, 0, len(scopeIDs))
	locked = make([]string, 0, len(scopeIDs))
	for _, id := range scopeIDs {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			return nil, nil, fmt.Errorf("load plan %q: %w", id, err)
		}
		switch plan.Status {
		case "paused":
			paused = append(paused, id)
		case "active":
			if lockedPlans[id] {
				locked = append(locked, id)
			}
		}
	}
	sort.Strings(paused)
	sort.Strings(locked)
	return paused, locked, nil
}

func deriveCompletionState(suggestion *workflowNextTaskSuggestion, paused, locked []string) string {
	switch {
	case suggestion != nil:
		return "actionable"
	case len(paused) > 0:
		return "paused"
	case len(locked) > 0:
		return "locked"
	default:
		return "drained"
	}
}

func collectWorkflowCompletionState(projectPath, explicitPlanID string) (*workflowCompletionScopeState, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &workflowCompletionScopeState{
			Scope: []string{},
			State: "drained",
		}, nil
	}

	scopeIDs, err := validateScopeIDsAgainstAvailable(parsePlanIDFilter(explicitPlanID), ids)
	if err != nil {
		return nil, err
	}

	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	lockedPlans := activeDelegationPlanIDs(delegations)
	pausedPlans, lockedScopePlans, err := partitionScopePlansByStatus(projectPath, scopeIDs, lockedPlans)
	if err != nil {
		return nil, err
	}

	suggestion, err := selectNextCanonicalTask(projectPath, explicitPlanID)
	if err != nil {
		return nil, err
	}

	state := deriveCompletionState(suggestion, pausedPlans, lockedScopePlans)

	return &workflowCompletionScopeState{
		Scope:       scopeIDs,
		State:       state,
		Next:        suggestion,
		PausedPlans: pausedPlans,
		LockedPlans: lockedScopePlans,
	}, nil
}

func resolveNextEffectivePlanIDs(projectPath, explicitPlanID string, ids []string) ([]string, error) {
	planFilter := parsePlanIDFilter(explicitPlanID)
	if explicitPlanID != "" && len(planFilter) == 0 {
		return nil, nil
	}
	if len(planFilter) > 0 {
		available := make(map[string]bool, len(ids))
		for _, id := range ids {
			available[id] = true
		}
		for _, id := range planFilter {
			if !available[id] {
				return nil, fmt.Errorf(errPlanNotFoundFmt, id)
			}
		}
	}

	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	lockedPlans := activeDelegationPlanIDs(delegations)
	if len(planFilter) > 0 {
		return filterPlanIDsUnlocked(planFilter, lockedPlans), nil
	}
	return filterPlanIDsLocked(ids, lockedPlans), nil
}

func rankNextTaskCandidate(projectPath string, sug workflowNextTaskSuggestion) (workflowNextTaskSuggestion, int) {
	plan, loadErr := loadCanonicalPlan(projectPath, sug.PlanID)
	priority := 3
	reason := "first pending unblocked task in an active canonical plan"
	if loadErr == nil {
		switch {
		case sug.Status == "in_progress" && plan.CurrentFocusTask == sug.TaskTitle:
			priority = 0
			reason = "current focus task is already in progress"
		case sug.Status == "in_progress":
			priority = 1
			reason = "task is already in progress and unblocked"
		case plan.CurrentFocusTask == sug.TaskTitle:
			priority = 2
			reason = "current focus task is pending and all dependencies are complete"
		}
	}
	sug.Reason = reason
	return sug, priority
}

func selectNextCanonicalTask(projectPath, explicitPlanID string) (*workflowNextTaskSuggestion, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	explicitPlanID = strings.TrimSpace(explicitPlanID)
	effectiveIDs, err := resolveNextEffectivePlanIDs(projectPath, explicitPlanID, ids)
	if err != nil {
		return nil, err
	}
	if len(effectiveIDs) == 0 {
		return nil, nil
	}

	candidates, err := selectAllEligibleTasks(projectPath, effectiveIDs)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	var best *workflowNextTaskSuggestion
	bestPriority := math.MaxInt
	for _, sug := range candidates {
		ranked, priority := rankNextTaskCandidate(projectPath, sug)
		if best == nil || priority < bestPriority {
			tmp := ranked
			best = &tmp
			bestPriority = priority
		}
	}
	return best, nil
}

func incompleteCanonicalDependencies(tasks []CanonicalTask, deps []string) []string {
	if len(deps) == 0 {
		return nil
	}

	statusByID := make(map[string]string, len(tasks))
	for _, task := range tasks {
		statusByID[task.ID] = task.Status
	}

	var incomplete []string
	for _, dep := range deps {
		// §3.4.6/§4: an upstream in {completed, awaiting_owner_review} satisfies
		// the dep; in_progress / awaiting_agent_review do NOT.
		if !depSatisfiesDownstream(statusByID[dep]) {
			incomplete = append(incomplete, dep)
		}
	}
	return incomplete
}

// incompleteCanonicalDependenciesCrossplan checks whether all deps are satisfied,
// resolving cross-plan references (entries containing "/") by loading the referenced
// plan's TASKS.yaml. Intra-plan deps are checked against localTasks.
// If a cross-plan reference cannot be loaded, it is treated as unsatisfied and a
// warning is appended to warnings.
// loadCrossPlanTasksCached returns the canonical tasks for refPlanID, loading
// from workflow/plans/ first and falling back to history/. Caches misses as nil.
func loadCrossPlanTasksCached(projectPath, refPlanID string, cache map[string]*CanonicalTaskFile) (*CanonicalTaskFile, error) {
	if tf, ok := cache[refPlanID]; ok {
		return tf, nil
	}
	tf, err := loadCanonicalTasks(projectPath, refPlanID)
	if err == nil {
		cache[refPlanID] = tf
		return tf, nil
	}
	histPath := filepath.Join(historyBaseDir(projectPath), refPlanID, workflowTasksFileName)
	if histContent, histErr := os.ReadFile(histPath); histErr == nil {
		var histTF CanonicalTaskFile
		if yaml.Unmarshal(histContent, &histTF) == nil {
			cache[refPlanID] = &histTF
			return &histTF, nil
		}
	}
	cache[refPlanID] = nil
	return nil, err
}

// crossPlanDepIncomplete returns true when the cross-plan dependency dep is
// not satisfied (plan missing, task missing, or task not completed). Warnings
// are appended when the plan or task cannot be located.
func crossPlanDepIncomplete(projectPath, dep string, cache map[string]*CanonicalTaskFile, warnings *[]string) bool {
	slashIdx := strings.Index(dep, "/")
	refPlanID := dep[:slashIdx]
	refTaskID := dep[slashIdx+1:]

	tf, loadErr := loadCrossPlanTasksCached(projectPath, refPlanID, cache)
	if tf == nil {
		if loadErr != nil && warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf("cross-plan dep %q: cannot load plan %q tasks: %v", dep, refPlanID, loadErr))
		}
		return true
	}

	for _, t := range tf.Tasks {
		if t.ID == refTaskID {
			// §3.4.6/§4: completed OR awaiting_owner_review satisfies the dep.
			return !depSatisfiesDownstream(t.Status)
		}
	}
	if warnings != nil {
		*warnings = append(*warnings, fmt.Sprintf("cross-plan dep %q: "+errTaskNotFoundInPlanFmt, dep, refTaskID, refPlanID))
	}
	return true
}

func incompleteCanonicalDependenciesCrossplan(projectPath string, localTasks []CanonicalTask, deps []string, warnings *[]string) []string {
	if len(deps) == 0 {
		return nil
	}

	statusByID := make(map[string]string, len(localTasks))
	for _, task := range localTasks {
		statusByID[task.ID] = task.Status
	}

	cache := make(map[string]*CanonicalTaskFile)
	var incomplete []string
	for _, dep := range deps {
		if !strings.Contains(dep, "/") {
			// §3.4.6/§4: completed OR awaiting_owner_review satisfies the dep.
			if !depSatisfiesDownstream(statusByID[dep]) {
				incomplete = append(incomplete, dep)
			}
			continue
		}
		if crossPlanDepIncomplete(projectPath, dep, cache, warnings) {
			incomplete = append(incomplete, dep)
		}
	}
	return incomplete
}

// selectAllEligibleTasks returns ALL unblocked pending/in_progress tasks across active
// plans, optionally filtered to the plans named in planFilter (comma-separated IDs or
// a pre-split []string passed directly). Tasks are excluded when:
//   - their plan has status != "active"
//   - the task has an active delegation lock (pending or active DelegationContract)
//   - any dependency is incomplete (intra-plan or cross-plan)
//
// The returned slice is ordered: in_progress before pending, then by plan order and
// task order within each plan. Each entry carries the same workflowNextTaskSuggestion
// shape that selectNextCanonicalTask uses. Returns an empty slice (not nil) when no
// eligible tasks exist.
func selectAllEligibleTasks(projectPath string, planFilter []string) ([]workflowNextTaskSuggestion, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return []workflowNextTaskSuggestion{}, err
	}
	if len(ids) == 0 {
		return []workflowNextTaskSuggestion{}, nil
	}

	ids, err = applyEligiblePlanFilter(ids, planFilter)
	if err != nil {
		return nil, err
	}

	activeDelegations, err := loadActiveDelegationTaskSet(projectPath)
	if err != nil {
		return []workflowNextTaskSuggestion{}, err
	}

	var eligible []workflowNextTaskSuggestion
	for _, id := range ids {
		eligible = append(eligible, collectEligibleTasksForPlan(projectPath, id, activeDelegations)...)
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		si, sj := eligible[i].Status, eligible[j].Status
		if si == sj {
			return false
		}
		return si == "in_progress"
	})

	if eligible == nil {
		eligible = []workflowNextTaskSuggestion{}
	}
	return eligible, nil
}

func applyEligiblePlanFilter(ids, planFilter []string) ([]string, error) {
	if len(planFilter) == 0 {
		return ids, nil
	}
	available := make(map[string]bool, len(ids))
	for _, id := range ids {
		available[id] = true
	}
	for _, want := range planFilter {
		if !available[want] {
			return nil, fmt.Errorf(errPlanNotFoundFmt, want)
		}
	}
	filterSet := make(map[string]bool, len(planFilter))
	for _, id := range planFilter {
		filterSet[id] = true
	}
	filtered := ids[:0]
	for _, id := range ids {
		if filterSet[id] {
			filtered = append(filtered, id)
		}
	}
	return filtered, nil
}

func loadActiveDelegationTaskSet(projectPath string) (map[string]bool, error) {
	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(delegations))
	for _, c := range delegations {
		if c.Status == "pending" || c.Status == "active" {
			active[c.ParentTaskID] = true
		}
	}
	return active, nil
}

func collectEligibleTasksForPlan(projectPath, id string, activeDelegations map[string]bool) []workflowNextTaskSuggestion {
	plan, err := loadCanonicalPlan(projectPath, id)
	if err != nil || plan.Status != "active" {
		return nil
	}
	tf, err := loadCanonicalTasks(projectPath, id)
	if err != nil {
		return nil
	}
	var out []workflowNextTaskSuggestion
	for _, task := range tf.Tasks {
		if task.Status != "pending" && task.Status != "in_progress" {
			continue
		}
		if activeDelegations[task.ID] {
			continue
		}
		var depWarnings []string
		if len(incompleteCanonicalDependenciesCrossplan(projectPath, tf.Tasks, task.DependsOn, &depWarnings)) > 0 {
			continue
		}
		ws := task.WriteScope
		if ws == nil {
			ws = []string{}
		}
		out = append(out, workflowNextTaskSuggestion{
			PlanID:               plan.ID,
			PlanTitle:            plan.Title,
			TaskID:               task.ID,
			TaskTitle:            task.Title,
			Status:               task.Status,
			Reason:               "eligible: active plan, unblocked, no active delegation",
			WriteScope:           append([]string(nil), ws...),
			VerificationRequired: task.VerificationRequired,
			DependsOn:            append([]string(nil), task.DependsOn...),
			AppType:              task.AppType,
		})
	}
	return out
}

// effectivePlanFocusTask returns the title that should represent plan focus for orient/status.
func effectivePlanFocusTask(tasks []CanonicalTask) string {
	var lastInProgress string
	for _, t := range tasks {
		if t.Status == "in_progress" {
			lastInProgress = strings.TrimSpace(t.Title)
		}
	}
	if lastInProgress != "" {
		return lastInProgress
	}
	for _, t := range tasks {
		if t.Status != "pending" {
			continue
		}
		if len(incompleteCanonicalDependencies(tasks, t.DependsOn)) > 0 {
			continue
		}
		return strings.TrimSpace(t.Title)
	}
	return ""
}

func runWorkflowAdvance(planID, taskID, newStatus string) error {
	if !isValidTaskStatus(newStatus) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid task status %q", newStatus),
			taskStatusVocabularyHint(),
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	input := &journal.AdvanceInput{Plan: planID, Task: taskID, Status: newStatus}
	observed := &journal.AdvanceObserved{ToStatus: newStatus}
	ok := false
	defer func() { journalTier1(project.Path, journal.CmdAdvance, input, observed, ok) }()

	var tf *CanonicalTaskFile
	var taskTitle string
	lockErr := withTasksLock(project.Path, planID, func() error {
		var loadErr error
		tf, loadErr = loadCanonicalTasks(project.Path, planID)
		if loadErr != nil {
			return fmt.Errorf(errTasksForPlanNotFoundFmt, planID, loadErr)
		}
		observed.FromStatus = canonicalTaskStatusByID(tf, taskID)
		var transErr error
		taskTitle, transErr = applyTaskStatusTransition(tf, planID, taskID, newStatus)
		if transErr != nil {
			return transErr
		}
		// Write PLAN.yaml first so the single choke-point mirror in
		// saveCanonicalTasksMirrored captures the fresh plan + tasks in ONE ref
		// commit (no double CAS). Both writes stay inside the TASKS lock.
		plan, planErr := loadCanonicalPlan(project.Path, planID)
		if planErr != nil {
			return planErr
		}
		plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if newStatus == "in_progress" {
			plan.CurrentFocusTask = strings.TrimSpace(taskTitle)
		} else {
			plan.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
		}
		if err := saveCanonicalPlan(project.Path, plan); err != nil {
			return err
		}
		return saveCanonicalTasksMirrored(project.Path, tf, taskID)
	})
	if lockErr != nil {
		return lockErr
	}
	ok = true
	ui.Success(fmt.Sprintf("Task %q advanced to %q", taskTitle, newStatus))
	return nil
}

// canonicalTaskStatusByID returns the current status of taskID in tf, or "" when
// the task is absent. Used to record a transition's from-status for the journal.
func canonicalTaskStatusByID(tf *CanonicalTaskFile, taskID string) string {
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			return tf.Tasks[i].Status
		}
	}
	return ""
}

func runWorkflowPlanCreate(planID, title, summary, owner, successCriteria, verificationStrategy string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	dir := filepath.Join(plansBaseDir(project.Path), planID)
	input := &journal.PlanCreateInput{Plan: planID, Title: title, Owner: owner}
	observed := &journal.PlanCreateObserved{PlanDir: config.DisplayPath(dir)}
	ok := false
	defer func() { journalTier1(project.Path, journal.CmdPlanCreate, input, observed, ok) }()
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("plan %q already exists at %s", planID, config.DisplayPath(dir))
	} else if !os.IsNotExist(err) {
		// A real Stat error (permission denied, TOCTOU) must not be treated
		// as "no collision" — that would let plan create silently write over
		// or beside a directory it couldn't actually verify was absent.
		return fmt.Errorf("check for existing plan %q at %s: %w", planID, config.DisplayPath(dir), err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	plan := &CanonicalPlan{
		SchemaVersion:        1,
		ID:                   planID,
		Title:                title,
		Status:               "draft",
		Summary:              summary,
		Owner:                owner,
		SuccessCriteria:      successCriteria,
		VerificationStrategy: verificationStrategy,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return err
	}
	tf := &CanonicalTaskFile{
		SchemaVersion: 1,
		PlanID:        planID,
		Tasks:         []CanonicalTask{},
	}
	if err := saveCanonicalTasksMirrored(project.Path, tf, ""); err != nil {
		return err
	}
	observed.FilesCreated = []string{workflowPlanFileName, workflowTasksFileName}
	ok = true
	ui.Success(fmt.Sprintf("Created plan %q at %s", planID, config.DisplayPath(dir)))
	return nil
}

// runWorkflowPlanArchive archives one or more plans (comma-separated planIDs) by
// merging each plan source directory into .agents/history/<planID>/ and stamping
// the PLAN.yaml status=archived before the move.
//
// Guard: plan status must be "completed" unless --force is set.
// Bulk: each plan is archived in sequence; a failure for one plan is logged and
// iteration continues.
//
// noCommit suppresses the per-plan workflow-state commit that otherwise persists
// each archive move (see archiveSinglePlan). Commit-by-default is the intent:
// the fresh-clone / worktree loop model discards an uncommitted move, so a
// half-archived working tree is the failure mode this command exists to avoid.
func runWorkflowPlanArchive(projectPath string, planIDs []string, force, dryRun, noCommit bool) error {
	var firstErr error
	var archivePaths, activeDirsRemoved []string
	for _, planID := range planIDs {
		if err := archiveSinglePlan(projectPath, planID, force, dryRun, noCommit); err != nil {
			fmt.Fprintf(os.Stderr, "archive plan %q: %v\n", planID, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !dryRun {
			archivePaths = append(archivePaths, config.DisplayPath(filepath.Join(historyBaseDir(projectPath), planID)))
			activeDirsRemoved = append(activeDirsRemoved, config.DisplayPath(filepath.Join(plansBaseDir(projectPath), planID)))
		}
	}
	// Journal only the plans that were actually moved (dry-run mutates nothing).
	// A failed plan within a bulk run is logged above; the success event records
	// the subset that landed, which is what a recovering session needs to see.
	if len(archivePaths) > 0 {
		emitWorkflowSuccess(projectPath, journal.CmdPlanArchive,
			&journal.PlanArchiveInput{Plans: planIDs, Force: force},
			&journal.PlanArchiveObserved{ArchivePaths: archivePaths, ActiveDirsRemoved: activeDirsRemoved})
	}
	return firstErr
}

func archiveSinglePlan(projectPath, planID string, force, dryRun, noCommit bool) error {
	plan, err := loadCanonicalPlan(projectPath, planID)
	if err != nil {
		return fmt.Errorf(errPlanNotFoundWithCause, planID, err)
	}

	// Guard: status must be completed (or --force).
	if plan.Status != "completed" && !force {
		return deps.ErrorWithHints(
			fmt.Sprintf("plan %q has status %q (expected completed)", planID, plan.Status),
			"Use --force to archive regardless of status.",
		)
	}

	srcDir := filepath.Join(plansBaseDir(projectPath), planID)
	dstDir := filepath.Join(historyBaseDir(projectPath), planID)

	// Stamp status=archived + updated_at BEFORE move.
	if err := stampPlanArchived(projectPath, planID, plan, dryRun); err != nil {
		return err
	}

	// Capture the plan-dir files BEFORE the merge relocates or removes them: the
	// merge fast-path RENAMES srcDir into history, so a capture taken afterwards
	// would see nothing. Each captured path becomes a tracked git deletion that
	// must be named as an explicit include so it stages even under the git-ref
	// backend, where DerivePathSet excludes .agents/workflow/ from the
	// auto-managed set. The matching history/<id> additions land under the
	// still-auto-managed .agents/history/ root. (In dry-run the capture is a
	// harmless read; the commit that consumes it never runs.)
	removedPaths := planDirIncludePaths(projectPath, srcDir)

	// Merge or rename into history.
	if err := mergeWorkflowPlanDir(planID, srcDir, dstDir, dryRun); err != nil {
		return fmt.Errorf("merge plan dir: %w", err)
	}

	// Archive the linked spec alongside the plan so the permanent record is complete
	// (workflow-artifact-model: history captures the spec too). A missing spec is a
	// no-op — not every plan has a 1:1 spec.
	if err := archiveLinkedSpec(projectPath, planID, dstDir, dryRun); err != nil {
		return fmt.Errorf("archive linked spec: %w", err)
	}

	// Relocate this plan's iteration-log records (iter-N.yaml + hook-outcomes +
	// score sidecars) into .agents/history/<plan>/iteration-log/, drop them from
	// the active index, and write the permanent per-plan history index. The
	// returned includes name the source deletions + active index (outside the
	// auto-managed roots) so they stage under BOTH backends; dry-run mutates
	// nothing and returns nil.
	iterIncludes, err := archivePlanIterations(projectPath, planID, dryRun)
	if err != nil {
		return fmt.Errorf("archive iteration log for %q: %w", planID, err)
	}

	// Remove the source directory after a successful merge.
	if !dryRun {
		if err := removeAllWithRetry(srcDir); err != nil {
			return fmt.Errorf("remove source dir %s: %w", srcDir, err)
		}
		ui.Success(fmt.Sprintf("Archived plan %q to %s", planID, config.DisplayPath(dstDir)))

		// Persist the archive move by default. At this point the working tree is
		// in its final archived state — plans/<id> is gone (tracked deletions),
		// history/<id> is populated (untracked additions), the iteration-log
		// records have moved, and PLAN.yaml is stamped archived.
		// iterationCloseCommitWithIncludes stages the workflow-managed path set
		// plus the named plan-dir deletions AND the named iter-log moves + index,
		// so the deletions AND the additions land as ONE commit — exactly like
		// advance --commit-state — under BOTH the local and git-ref backends.
		// Without this the fresh-clone / worktree loop model discards the
		// uncommitted move and the plan never actually archives. runWorkflowCommit
		// honors the commit.disable opt-out internally; --no-commit is the
		// explicit operator opt-out for batching several archives into one manual
		// commit.
		archiveIncludes := append(append([]string{}, removedPaths...), iterIncludes...)
		if noCommit {
			fmt.Printf("  archive: --no-commit set; skipping workflow-state commit for %q\n", planID)
		} else if err := iterationCloseCommitWithIncludes(os.Stdout, archiveIncludes); err != nil {
			return fmt.Errorf("commit archive move for %q: %w", planID, err)
		}
	} else {
		fmt.Printf("  [dry-run] remove source dir %s\n", srcDir)
	}

	return nil
}

// planDirIncludePaths returns the repo-relative (forward-slash) paths of every
// regular file currently under the plan source dir. archiveSinglePlan calls it
// before the merge relocates the dir into history: the archive turns each file
// into a
// tracked git deletion, and those deletions must be named as explicit --include
// so they stage even when the git-ref backend excludes .agents/workflow/ from
// the auto-managed set (planStateSkipped). Best-effort: a walk error yields the
// paths collected so far — the archive commit still stages the auto-managed set.
func planDirIncludePaths(projectPath, srcDir string) []string {
	var includes []string
	_ = filepath.WalkDir(srcDir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if rel, relErr := filepath.Rel(projectPath, p); relErr == nil {
			includes = append(includes, filepath.ToSlash(rel))
		}
		return nil
	})
	return includes
}

func stampPlanArchived(projectPath, planID string, plan *CanonicalPlan, dryRun bool) error {
	if dryRun {
		fmt.Printf("  [dry-run] stamp %s status=archived\n", planID)
		return nil
	}

	plan.Status = "archived"
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveCanonicalPlan(projectPath, plan); err != nil {
		return fmt.Errorf("stamp archived status: %w", err)
	}
	return nil
}

// archiveLinkedSpec copies the plan's linked spec (workflow/specs/<planID>/design.md,
// by the plan-id == spec-id convention) into the plan's history archive so the
// permanent record is complete. The spec is COPIED, not moved: other active specs
// cross-link it by relative path, so the editable copy stays under workflow/specs.
// A missing spec is a no-op (not every plan has a 1:1 spec).
func archiveLinkedSpec(projectPath, planID, dstDir string, dryRun bool) error {
	specPath := filepath.Join(specsBaseDir(projectPath), planID, workflowDesignFileName)
	info, err := os.Stat(specPath)
	if err != nil || info.IsDir() {
		return nil // no linked spec to archive
	}
	dst := filepath.Join(dstDir, workflowDesignFileName)
	if dryRun {
		fmt.Printf("  [dry-run] archive linked spec %s -> %s\n", config.DisplayPath(specPath), config.DisplayPath(dst))
		return nil
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read linked spec %s: %w", specPath, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("write archived spec %s: %w", dst, err)
	}
	ui.Success(fmt.Sprintf("Archived linked spec for %q to %s", planID, config.DisplayPath(dst)))
	return nil
}

func runWorkflowPlanUpdate(planID, status, title, summary, focus, successCriteria, verificationStrategy string) error {
	if status != "" && !isValidPlanStatus(status) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid plan status %q", status),
			"Valid values: `draft`, `active`, `paused`, `completed`, `archived`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf(errPlanNotFoundWithCause, planID, err)
	}
	changed := map[string]string{}
	applyPlanFieldUpdate(changed, "status", status, &plan.Status)
	applyPlanFieldUpdate(changed, "title", title, &plan.Title)
	applyPlanFieldUpdate(changed, "summary", summary, &plan.Summary)
	applyPlanFieldUpdate(changed, "success_criteria", successCriteria, &plan.SuccessCriteria)
	applyPlanFieldUpdate(changed, "verification_strategy", verificationStrategy, &plan.VerificationStrategy)
	applyPlanFieldUpdate(changed, "current_focus_task", focus, &plan.CurrentFocusTask)
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveCanonicalPlanMirrored(project.Path, plan); err != nil {
		return err
	}
	emitWorkflowDelta(project.Path, journal.CmdPlanUpdate, planID, "", changed)
	ui.Success(fmt.Sprintf("Updated plan %q", planID))
	return nil
}

// applyPlanFieldUpdate sets *dst to newVal when newVal is a non-empty override
// that differs from the current value, recording the replaced field's new value
// in changed so the Tier-2 journal delta carries only fields that actually moved.
func applyPlanFieldUpdate(changed map[string]string, field, newVal string, dst *string) {
	if newVal == "" || newVal == *dst {
		return
	}
	*dst = newVal
	changed[field] = newVal
}

func splitTrimmedCSV(csv string) []string {
	if csv == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// taskAddInputs bundles the inputs to runWorkflowTaskAdd so the call site stays
// under the function-parameter limit while keeping each field individually
// addressable from the caller.
type taskAddInputs struct {
	PlanID               string
	TaskID               string
	Title                string
	Notes                string
	Owner                string
	DependsOn            string
	Blocks               string
	WriteScope           string
	AppType              string
	VerificationRequired bool
}

func runWorkflowTaskAdd(in taskAddInputs) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	var task CanonicalTask
	lockErr := withTasksLock(project.Path, in.PlanID, func() error {
		tf, loadErr := loadCanonicalTasks(project.Path, in.PlanID)
		if loadErr != nil {
			return fmt.Errorf(errTasksForPlanNotFoundFmt, in.PlanID, loadErr)
		}
		for _, t := range tf.Tasks {
			if t.ID == in.TaskID {
				return fmt.Errorf("task %q already exists in plan %q", in.TaskID, in.PlanID)
			}
		}
		task = CanonicalTask{
			ID:                   in.TaskID,
			Title:                in.Title,
			Status:               "pending",
			Owner:                in.Owner,
			Notes:                in.Notes,
			AppType:              in.AppType,
			VerificationRequired: in.VerificationRequired,
			DependsOn:            splitTrimmedCSV(in.DependsOn),
			Blocks:               splitTrimmedCSV(in.Blocks),
			WriteScope:           splitTrimmedCSV(in.WriteScope),
		}
		tf.Tasks = append(tf.Tasks, task)
		// Bump PLAN.yaml first so the single choke-point mirror in
		// saveCanonicalTasksMirrored captures the fresh plan + the new task in
		// ONE ref commit. Plan-save error is non-fatal (as before).
		plan, planErr := loadCanonicalPlan(project.Path, in.PlanID)
		if planErr != nil {
			return planErr
		}
		plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = saveCanonicalPlan(project.Path, plan)
		return saveCanonicalTasksMirrored(project.Path, tf, in.TaskID)
	})
	if lockErr != nil {
		return lockErr
	}
	emitWorkflowSuccess(project.Path, journal.CmdTaskAdd,
		&journal.TaskAddInput{
			Plan:       in.PlanID,
			TaskID:     in.TaskID,
			Title:      in.Title,
			DependsOn:  task.DependsOn,
			WriteScope: task.WriteScope,
			AppType:    in.AppType,
		},
		&journal.TaskAddObserved{Appended: true})
	ui.Success(fmt.Sprintf("Added task %q to plan %q", in.TaskID, in.PlanID))
	return nil
}

// taskUpdateInputs bundles the inputs to runWorkflowTaskUpdate so the call site
// stays under the function-parameter limit while keeping each field
// individually addressable from the caller (mirrors taskAddInputs).
type taskUpdateInputs struct {
	PlanID     string
	TaskID     string
	Title      string
	Notes      string
	WriteScope string
	DependsOn  string
	Blocks     string
	AppType    string
}

// applyTaskFieldUpdates mutates task in place for any non-empty field value that
// differs from the current one, and returns the set of changed fields keyed by
// field name (the value being the new value, used for the journal delta).
//
// in.AppType is expected to have already passed validateTaskAppType — this
// function only applies it, matching the "replace when non-empty and
// different" semantics every other field here follows.
func applyTaskFieldUpdates(task *CanonicalTask, in taskUpdateInputs) map[string]string {
	changed := map[string]string{}
	if in.Title != "" && in.Title != task.Title {
		task.Title = in.Title
		changed["title"] = in.Title
	}
	if in.Notes != "" && in.Notes != task.Notes {
		task.Notes = in.Notes
		changed["notes"] = in.Notes
	}
	if ws := splitTrimmedCSV(in.WriteScope); in.WriteScope != "" && strings.Join(ws, ",") != strings.Join(task.WriteScope, ",") {
		task.WriteScope = ws
		changed["write_scope"] = in.WriteScope
	}
	if do := splitTrimmedCSV(in.DependsOn); in.DependsOn != "" && strings.Join(do, ",") != strings.Join(task.DependsOn, ",") {
		task.DependsOn = do
		changed["depends_on"] = in.DependsOn
	}
	if bl := splitTrimmedCSV(in.Blocks); in.Blocks != "" && strings.Join(bl, ",") != strings.Join(task.Blocks, ",") {
		task.Blocks = bl
		changed["blocks"] = in.Blocks
	}
	if in.AppType != "" && in.AppType != task.AppType {
		task.AppType = in.AppType
		changed["app_type"] = in.AppType
	}
	return changed
}

func runWorkflowTaskUpdate(in taskUpdateInputs) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	warning, err := validateTaskAppType(project.Path, in.AppType)
	if err != nil {
		return err
	}
	if warning != "" {
		ui.Warn(warning)
	}
	changed := map[string]string{}
	lockErr := withTasksLock(project.Path, in.PlanID, func() error {
		tf, loadErr := loadCanonicalTasks(project.Path, in.PlanID)
		if loadErr != nil {
			return fmt.Errorf(errTasksForPlanNotFoundFmt, in.PlanID, loadErr)
		}
		found := false
		for i := range tf.Tasks {
			if tf.Tasks[i].ID != in.TaskID {
				continue
			}
			changed = applyTaskFieldUpdates(&tf.Tasks[i], in)
			found = true
			break
		}
		if !found {
			return fmt.Errorf(errTaskNotFoundInPlanFmt, in.TaskID, in.PlanID)
		}
		// Bump PLAN.yaml first so the single choke-point mirror in
		// saveCanonicalTasksMirrored captures the fresh plan + the updated task
		// in ONE ref commit. Plan-save error is non-fatal (as before).
		plan, planErr := loadCanonicalPlan(project.Path, in.PlanID)
		if planErr != nil {
			return planErr
		}
		plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = saveCanonicalPlan(project.Path, plan)
		return saveCanonicalTasksMirrored(project.Path, tf, in.TaskID)
	})
	if lockErr != nil {
		return lockErr
	}
	emitWorkflowDelta(project.Path, journal.CmdTaskUpdate, in.PlanID, in.TaskID, changed)
	ui.Success(fmt.Sprintf("Updated task %q in plan %q", in.TaskID, in.PlanID))
	return nil
}

type taskRepointInputs struct {
	PlanID    string
	OldID     string
	NewID     string
	Operation string
	DryRun    bool
	JSON      bool
}

type taskRepointDetail struct {
	TaskID string   `json:"task_id"`
	Fields []string `json:"fields"`
}

type taskRepointResult struct {
	Operation string              `json:"operation"`
	PlanID    string              `json:"plan_id"`
	OldID     string              `json:"old_id"`
	NewID     string              `json:"new_id"`
	DryRun    bool                `json:"dry_run"`
	Repointed []taskRepointDetail `json:"repointed"`
}

func runWorkflowTaskRename(out io.Writer, planID, oldID, newID string, dryRun, asJSON bool) error {
	return runWorkflowTaskRepoint(out, taskRepointInputs{
		PlanID: planID, OldID: oldID, NewID: newID, Operation: "rename", DryRun: dryRun, JSON: asJSON,
	})
}

func runWorkflowTaskSupersede(out io.Writer, planID, oldID, newID string, dryRun, asJSON bool) error {
	return runWorkflowTaskRepoint(out, taskRepointInputs{
		PlanID: planID, OldID: oldID, NewID: newID, Operation: "supersede", DryRun: dryRun, JSON: asJSON,
	})
}

func runWorkflowTaskRepoint(out io.Writer, in taskRepointInputs) error {
	if strings.TrimSpace(in.OldID) == "" || strings.TrimSpace(in.NewID) == "" {
		return fmt.Errorf("old and new task IDs must not be empty")
	}
	if in.OldID == in.NewID {
		return fmt.Errorf("old and new task IDs must differ")
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	result := taskRepointResult{
		Operation: in.Operation,
		PlanID:    in.PlanID,
		OldID:     in.OldID,
		NewID:     in.NewID,
		DryRun:    in.DryRun,
		Repointed: []taskRepointDetail{},
	}
	lockErr := withTasksLock(project.Path, in.PlanID, func() error {
		tf, loadErr := loadCanonicalTasks(project.Path, in.PlanID)
		if loadErr != nil {
			return fmt.Errorf(errTasksForPlanNotFoundFmt, in.PlanID, loadErr)
		}
		oldIdx, newIdx := taskIndexes(tf, in.OldID, in.NewID)
		repointed, applyErr := applyTaskRepoint(tf, in, oldIdx, newIdx)
		if applyErr != nil {
			return applyErr
		}
		result.Repointed = repointed
		if in.DryRun {
			return nil
		}
		plan, planErr := loadCanonicalPlan(project.Path, in.PlanID)
		if planErr != nil {
			return planErr
		}
		plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = saveCanonicalPlan(project.Path, plan)
		return saveCanonicalTaskRepointMirrored(project.Path, tf, in)
	})
	if lockErr != nil {
		return lockErr
	}
	return emitTaskRepointResult(out, result, in.JSON)
}

func taskIndexes(tf *CanonicalTaskFile, oldID, newID string) (oldIdx, newIdx int) {
	oldIdx, newIdx = -1, -1
	for i := range tf.Tasks {
		switch tf.Tasks[i].ID {
		case oldID:
			oldIdx = i
		case newID:
			newIdx = i
		}
	}
	return oldIdx, newIdx
}

func applyTaskRepoint(tf *CanonicalTaskFile, in taskRepointInputs, oldIdx, newIdx int) ([]taskRepointDetail, error) {
	if oldIdx < 0 {
		return nil, fmt.Errorf(errTaskNotFoundInPlanFmt, in.OldID, in.PlanID)
	}
	switch in.Operation {
	case "rename":
		if newIdx >= 0 {
			return nil, fmt.Errorf("task %q already exists in plan %q", in.NewID, in.PlanID)
		}
		tf.Tasks[oldIdx].ID = in.NewID
		tf.Tasks[oldIdx].Notes = repointFoldBackNoteTags(tf.Tasks[oldIdx].Notes, in.OldID, in.NewID)
	case "supersede":
		if newIdx < 0 {
			return nil, fmt.Errorf(errTaskNotFoundInPlanFmt, in.NewID, in.PlanID)
		}
		tf.Tasks = append(tf.Tasks[:oldIdx], tf.Tasks[oldIdx+1:]...)
	default:
		return nil, fmt.Errorf("unsupported task repoint operation %q", in.Operation)
	}
	return repointTaskDependencies(tf, in), nil
}

func repointTaskDependencies(tf *CanonicalTaskFile, in taskRepointInputs) []taskRepointDetail {
	repointed := []taskRepointDetail{}
	for i := range tf.Tasks {
		if in.Operation == "rename" && tf.Tasks[i].ID == in.NewID {
			continue
		}
		fields := []string{}
		removeOld := in.Operation == "supersede" && tf.Tasks[i].ID == in.NewID
		if refs, changed := rewriteTaskReferences(tf.Tasks[i].DependsOn, in.PlanID, in.OldID, in.NewID, removeOld); changed {
			tf.Tasks[i].DependsOn = refs
			fields = append(fields, "depends_on")
		}
		if refs, changed := rewriteTaskReferences(tf.Tasks[i].Blocks, in.PlanID, in.OldID, in.NewID, removeOld); changed {
			tf.Tasks[i].Blocks = refs
			fields = append(fields, "blocks")
		}
		if len(fields) > 0 {
			repointed = append(repointed, taskRepointDetail{TaskID: tf.Tasks[i].ID, Fields: fields})
		}
	}
	return repointed
}

// repointRef maps a single reference to its rewritten form for a task repoint.
// It returns the (possibly rewritten) ref and whether it should be kept: a ref
// equal to the old (bare or plan-qualified) id is dropped when remove is set,
// otherwise rewritten to the new id; any other ref is returned unchanged.
func repointRef(ref, oldID, oldQualified, newID, newQualified string, remove bool) (string, bool) {
	switch ref {
	case oldID:
		if remove {
			return "", false
		}
		return newID, true
	case oldQualified:
		if remove {
			return "", false
		}
		return newQualified, true
	}
	return ref, true
}

func rewriteTaskReferences(refs []string, planID, oldID, newID string, remove bool) ([]string, bool) {
	oldQualified := planID + "/" + oldID
	newQualified := planID + "/" + newID
	found := false
	for _, ref := range refs {
		if ref == oldID || ref == oldQualified {
			found = true
			break
		}
	}
	if !found {
		return refs, false
	}
	out := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		mapped, keep := repointRef(ref, oldID, oldQualified, newID, newQualified, remove)
		if !keep {
			continue
		}
		if _, exists := seen[mapped]; exists {
			continue
		}
		seen[mapped] = struct{}{}
		out = append(out, mapped)
	}
	return out, true
}

func repointFoldBackNoteTags(notes, oldID, newID string) string {
	var out strings.Builder
	scan, written := 0, 0
	changed := false
	for {
		startOffset := strings.Index(notes[scan:], "(fb:")
		if startOffset < 0 {
			break
		}
		start := scan + startOffset
		slugStart := start + len("(fb:")
		endOffset := strings.IndexByte(notes[slugStart:], ')')
		if endOffset < 0 {
			break
		}
		end := slugStart + endOffset
		slug := notes[slugStart:end]
		repointedSlug := slug
		if slug == oldID || strings.HasSuffix(slug, "-"+oldID) || strings.HasSuffix(slug, "_"+oldID) {
			repointedSlug = strings.TrimSuffix(slug, oldID) + newID
		}
		if repointedSlug != slug {
			out.WriteString(notes[written:slugStart])
			out.WriteString(repointedSlug)
			written = end
			changed = true
		}
		scan = end + 1
	}
	if !changed {
		return notes
	}
	out.WriteString(notes[written:])
	return out.String()
}

func emitTaskRepointResult(out io.Writer, result taskRepointResult, asJSON bool) error {
	if asJSON || deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	verb := "Renamed"
	if result.Operation == "supersede" {
		verb = "Superseded"
	}
	if result.DryRun {
		if result.Operation == "supersede" {
			verb = "Would supersede"
		} else {
			verb = "Would rename"
		}
	}
	if _, err := fmt.Fprintf(out, "%s task %q with %q in plan %q\n", verb, result.OldID, result.NewID, result.PlanID); err != nil {
		return err
	}
	if len(result.Repointed) == 0 {
		_, err := fmt.Fprintln(out, "Dependents to repoint: none")
		return err
	}
	if _, err := fmt.Fprintln(out, "Dependents to repoint:"); err != nil {
		return err
	}
	for _, detail := range result.Repointed {
		if _, err := fmt.Fprintf(out, "- %s: %s\n", detail.TaskID, strings.Join(detail.Fields, ", ")); err != nil {
			return err
		}
	}
	return nil
}

// PlanScheduleTask is a task entry in the schedule output.
type PlanScheduleTask struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	WriteScope []string `json:"write_scope"`
}

// PlanScheduleWave is a single wave (parallel group) in the schedule.
type PlanScheduleWave struct {
	Wave  int                `json:"wave"`
	Tasks []PlanScheduleTask `json:"tasks"`
}

// PlanScheduleResult is the full schedule output.
type PlanScheduleResult struct {
	PlanID              string             `json:"plan_id"`
	Waves               []PlanScheduleWave `json:"waves"`
	CriticalPathLength  int                `json:"critical_path_length"`
	MaxIntraParallelism int                `json:"max_intra_plan_parallelism"`
}

// buildPlanScheduleGraph builds the in-degree array and adjacency list used
// by Kahn's BFS topological sort. Cross-plan deps (containing "/") are ignored.
func buildPlanScheduleGraph(tf *CanonicalTaskFile) (inDegree []int, adj [][]int) {
	idxByID := make(map[string]int, len(tf.Tasks))
	for i, t := range tf.Tasks {
		idxByID[t.ID] = i
	}
	inDegree = make([]int, len(tf.Tasks))
	adj = make([][]int, len(tf.Tasks))
	for i, t := range tf.Tasks {
		for _, dep := range t.DependsOn {
			if strings.Contains(dep, "/") {
				continue
			}
			j, ok := idxByID[dep]
			if !ok {
				continue
			}
			adj[j] = append(adj[j], i)
			inDegree[i]++
		}
	}
	return inDegree, adj
}

// runKahnBFSWaves assigns each task a wave index by Kahn's BFS topological sort.
// Returns the wave-grouped task indices and the total number of processed tasks.
func runKahnBFSWaves(inDegree []int, adj [][]int) (waveSlots map[int][]int, processed int) {
	waveIdx := make([]int, len(inDegree))
	queue := zeroDegreeQueue(inDegree)
	waveSlots = map[int][]int{}
	for len(queue) > 0 {
		queue, processed = processKahnWave(queue, adj, inDegree, waveIdx, waveSlots, processed)
	}
	return waveSlots, processed
}

func zeroDegreeQueue(inDegree []int) []int {
	queue := make([]int, 0, len(inDegree))
	for i, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, i)
		}
	}
	return queue
}

func processKahnWave(queue []int, adj [][]int, inDegree, waveIdx []int, waveSlots map[int][]int, processed int) ([]int, int) {
	var nextQueue []int
	for _, idx := range queue {
		w := waveIdx[idx]
		waveSlots[w] = append(waveSlots[w], idx)
		processed++
		for _, dep := range adj[idx] {
			inDegree[dep]--
			if waveIdx[dep] < w+1 {
				waveIdx[dep] = w + 1
			}
			if inDegree[dep] == 0 {
				nextQueue = append(nextQueue, dep)
			}
		}
	}
	return nextQueue, processed
}

func buildPlanScheduleWaves(tf *CanonicalTaskFile, waveSlots map[int][]int) ([]PlanScheduleWave, int) {
	waveNums := make([]int, 0, len(waveSlots))
	for w := range waveSlots {
		waveNums = append(waveNums, w)
	}
	sort.Ints(waveNums)

	waves := make([]PlanScheduleWave, 0, len(waveNums))
	maxPar := 0
	for _, w := range waveNums {
		indices := waveSlots[w]
		waveTasks := make([]PlanScheduleTask, 0, len(indices))
		for _, idx := range indices {
			t := tf.Tasks[idx]
			ws := t.WriteScope
			if ws == nil {
				ws = []string{}
			}
			waveTasks = append(waveTasks, PlanScheduleTask{
				ID:         t.ID,
				Title:      t.Title,
				Status:     t.Status,
				WriteScope: ws,
			})
		}
		sort.Slice(waveTasks, func(a, b int) bool { return waveTasks[a].ID < waveTasks[b].ID })
		waves = append(waves, PlanScheduleWave{Wave: w + 1, Tasks: waveTasks})
		if len(waveTasks) > maxPar {
			maxPar = len(waveTasks)
		}
	}
	return waves, maxPar
}

// computePlanSchedule runs Kahn's BFS topological sort on the tasks in tf,
// assigning each task a wave number. Cross-plan dep entries (containing "/")
// are ignored for intra-plan scheduling. Returns a PlanScheduleResult.
func computePlanSchedule(tf *CanonicalTaskFile) (*PlanScheduleResult, error) {
	inDegree, adj := buildPlanScheduleGraph(tf)
	waveSlots, processed := runKahnBFSWaves(inDegree, adj)
	if processed != len(tf.Tasks) {
		return nil, fmt.Errorf("plan %q has a dependency cycle (processed %d of %d tasks)", tf.PlanID, processed, len(tf.Tasks))
	}
	waves, maxPar := buildPlanScheduleWaves(tf, waveSlots)
	return &PlanScheduleResult{
		PlanID:              tf.PlanID,
		Waves:               waves,
		CriticalPathLength:  len(waves),
		MaxIntraParallelism: maxPar,
	}, nil
}

// errCheckScopeSidecarMissing is returned by loadCheckScopeSidecar when the
// .scope.yaml sidecar is absent. In production, the function calls
// osExit(2) before returning, so this error is observed only when tests
// stub osExit to a no-op.
var errCheckScopeSidecarMissing = fmt.Errorf("scope sidecar missing")

// checkScopeResult holds the output of a check-scope run.
type checkScopeResult struct {
	PlanID            string   `json:"plan_id"`
	TaskID            string   `json:"task_id"`
	SidecarPath       string   `json:"sidecar_path"`
	ChangedFiles      []string `json:"changed_files"`
	InsideScope       []string `json:"inside_scope"`
	OutsideScope      []string `json:"outside_scope"`
	UntouchedRequired []string `json:"untouched_required"`
	TouchedExcluded   []string `json:"touched_excluded"`
	Clean             bool     `json:"clean"`
}

// runWorkflowPlanCheckScope implements `workflow plan check-scope <plan_id> <task_id>`.
// It reads the .scope.yaml sidecar, collects changed files from flags or git diff, and
// reports which files are inside/outside final_write_scope, which required_paths were
// untouched, and which excluded_paths were touched.
// Exit code: 0=clean, 1=warning (outside-scope or excluded touched), 2=no-sidecar.
func loadCheckScopeSidecar(projectPath, planID, taskID string) (string, *ScopeEvidence, error) {
	sidecarPath := deriveScopeEvidencePath(projectPath, planID, taskID)
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "no scope sidecar found at %s\n", config.DisplayPath(sidecarPath))
			fmt.Fprintln(os.Stderr, "Run 'da workflow plan derive-scope' to generate one.")
			osExit(2)
			// Unreachable in production (osExit terminates). Tests swap osExit
			// to a no-op, so we return an explicit error to keep callers safe.
			return "", nil, errCheckScopeSidecarMissing
		}
		return "", nil, fmt.Errorf("read sidecar: %w", err)
	}
	var ev ScopeEvidence
	if err := yaml.Unmarshal(data, &ev); err != nil {
		return "", nil, fmt.Errorf("parse sidecar: %w", err)
	}
	return sidecarPath, &ev, nil
}

func collectCheckScopeChangedFiles(projectPath string, changedFiles []string, fromGitDiff bool) []string {
	if fromGitDiff {
		if gitFiles, err := checkScopeGitDiffFiles(projectPath); err != nil {
			ui.Warn("--from-git-diff: " + err.Error())
		} else {
			changedFiles = append(changedFiles, gitFiles...)
		}
	}
	seen := make(map[string]bool, len(changedFiles))
	deduped := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		if !seen[f] {
			seen[f] = true
			deduped = append(deduped, f)
		}
	}
	return deduped
}

func classifyCheckScopeFiles(ev *ScopeEvidence, changedFiles []string) (inside, outside, touchedExcluded, untouchedRequired []string) {
	finalScopeSet := make(map[string]bool, len(ev.FinalWriteScope))
	for _, p := range ev.FinalWriteScope {
		finalScopeSet[p] = true
	}
	requiredSet := make(map[string]bool, len(ev.RequiredPaths))
	for _, rp := range ev.RequiredPaths {
		requiredSet[rp.Path] = true
	}
	excludedSet := make(map[string]bool, len(ev.ExcludedPaths))
	for _, ep := range ev.ExcludedPaths {
		excludedSet[ep.Path] = true
	}

	touchedFiles := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		touchedFiles[f] = true
		switch {
		case finalScopeSet[f] || requiredSet[f]:
			inside = append(inside, f)
		case excludedSet[f]:
			touchedExcluded = append(touchedExcluded, f)
		default:
			outside = append(outside, f)
		}
	}
	for _, rp := range ev.RequiredPaths {
		if !touchedFiles[rp.Path] {
			untouchedRequired = append(untouchedRequired, rp.Path)
		}
	}
	sort.Strings(inside)
	sort.Strings(outside)
	sort.Strings(untouchedRequired)
	sort.Strings(touchedExcluded)
	return
}

func renderCheckScopeSection(out *os.File, title, prefix string, items []string) {
	if len(items) == 0 {
		return
	}
	ui.Section(title)
	for _, f := range items {
		fmt.Fprintf(out, "  %s %s\n", prefix, f)
	}
	fmt.Fprintln(out)
}

func renderCheckScopeResult(planID, taskID, sidecarPath string, result checkScopeResult) {
	ui.Header(fmt.Sprintf("Scope Check: %s / %s", planID, taskID))
	fmt.Fprintf(os.Stdout, "  sidecar: %s\n", sidecarPath)
	fmt.Fprintf(os.Stdout, "  changed files: %d\n\n", len(result.ChangedFiles))

	renderCheckScopeSection(os.Stdout, "Inside Scope", "+", result.InsideScope)
	renderCheckScopeSection(os.Stdout, "Outside Scope (warning)", "!", result.OutsideScope)
	renderCheckScopeSection(os.Stdout, "Untouched Required Paths", "-", result.UntouchedRequired)
	renderCheckScopeSection(os.Stdout, "Touched Excluded Paths (warning)", "x", result.TouchedExcluded)

	if result.Clean {
		ui.Success("clean — all changes are within scope, no excluded paths touched")
	} else {
		ui.Warn("scope warnings present (outside-scope or excluded paths touched)")
		fmt.Fprintln(os.Stdout)
		osExit(1)
	}
}

func runWorkflowPlanCheckScope(planID, taskID string, changedFiles []string, fromGitDiff bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	projectPath := project.Path

	sidecarPath, ev, err := loadCheckScopeSidecar(projectPath, planID, taskID)
	if err != nil {
		return err
	}

	changedFiles = collectCheckScopeChangedFiles(projectPath, changedFiles, fromGitDiff)
	insideScope, outsideScope, touchedExcluded, untouchedRequired := classifyCheckScopeFiles(ev, changedFiles)
	clean := len(outsideScope) == 0 && len(touchedExcluded) == 0

	result := checkScopeResult{
		PlanID:            planID,
		TaskID:            taskID,
		SidecarPath:       config.DisplayPath(sidecarPath),
		ChangedFiles:      changedFiles,
		InsideScope:       insideScope,
		OutsideScope:      outsideScope,
		UntouchedRequired: untouchedRequired,
		TouchedExcluded:   touchedExcluded,
		Clean:             clean,
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
		if !clean {
			osExit(1)
		}
		return nil
	}

	renderCheckScopeResult(planID, taskID, config.DisplayPath(sidecarPath), result)
	return nil
}

// checkScopeGitDiffFiles returns the list of files with uncommitted changes using
// `git diff --name-only HEAD`. Returns an error on failure (used for graceful degradation).
func checkScopeGitDiffFiles(projectPath string) ([]string, error) {
	trackGitSpawn()
	cmd := execabs.Command("git", "diff", gitFlagNameOnly, "HEAD")
	cmd.Dir = projectPath
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		// Also try index-only (staged but not committed).
		trackGitSpawn()
		cmd2 := execabs.Command("git", "diff", gitFlagNameOnly, "--cached")
		cmd2.Dir = projectPath
		cmd2.Env = os.Environ()
		out2, err2 := cmd2.Output()
		if err2 != nil {
			return nil, fmt.Errorf("git diff HEAD: %v; git diff --cached: %v", err, err2)
		}
		out = out2
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func runWorkflowPlanSchedule(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("load tasks for plan %q: %w", planID, err)
	}

	result, err := computePlanSchedule(tf)
	if err != nil {
		return err
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	ui.Header(fmt.Sprintf("Plan Schedule: %s", planID))
	for _, w := range result.Waves {
		fmt.Fprintf(os.Stdout, "\nWave %d (%d task(s)):\n", w.Wave, len(w.Tasks))
		for _, t := range w.Tasks {
			scope := strings.Join(t.WriteScope, ", ")
			if scope == "" {
				scope = "(none)"
			}
			fmt.Fprintf(os.Stdout, "  [%s] %s — %s\n    write_scope: %s\n", t.Status, t.ID, t.Title, scope)
		}
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Critical path length : %d wave(s)\n", result.CriticalPathLength)
	fmt.Fprintf(os.Stdout, "Max intra-plan parallelism: %d task(s)\n", result.MaxIntraParallelism)
	fmt.Fprintln(os.Stdout)
	return nil
}
