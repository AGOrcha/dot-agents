package workflow

import (
	// _ "embed": pull in static/workflow-hook-sentinel.schema.json via go:embed.
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"
)

// HookSentinelSchemaVersion is the current schema version emitted by `da workflow hook-sentinel write`.
const HookSentinelSchemaVersion = 1

// hookSentinelLifecyclePointSkillEntry is the only `lifecycle_point` value
// allowed in v1; later versions may introduce additional points alongside a
// schema_version bump.
const hookSentinelLifecyclePointSkillEntry = "skill_entry"

// hookSentinelAllowedSkills mirrors the schema enum for `skill`.
var hookSentinelAllowedSkills = map[string]struct{}{
	"iteration-close": {},
	"isp":             {},
	"loop-worker":     {},
}

// hookSentinelAllowedAgentTypes mirrors the schema enum for `agent_type`.
var hookSentinelAllowedAgentTypes = map[string]struct{}{
	"main":        {},
	"loop-worker": {},
}

//go:embed static/workflow-hook-sentinel.schema.json
var workflowHookSentinelSchemaJSON []byte

// File-local seams for the hook-sentinel CLI. These mirror existing
// commands/workflow seams in style and exist here (rather than in seams.go)
// because the p0-sentinel-cli write scope excludes seams.go. Tests in
// hook_sentinel_test.go swap them via t.Cleanup-scoped helpers to drive
// the otherwise unreachable error branches (rename failure mid-publish,
// stat collision races, malformed time fields read from disk, etc.).
var (
	osStat          = os.Stat
	osReadFile      = os.ReadFile
	osReadDir       = os.ReadDir
	osRename        = os.Rename
	osRemove        = os.Remove
	hookSentinelNow = func() time.Time { return time.Now() }
)

var (
	workflowHookSentinelCompiled     *jsonschema.Schema
	workflowHookSentinelCompiledOnce sync.Once
	workflowHookSentinelCompiledErr  error
)

func compiledWorkflowHookSentinelSchema(sc schemaCompiler) (*jsonschema.Schema, error) {
	workflowHookSentinelCompiledOnce.Do(func() {
		const schemaURL = "./schemas/workflow-hook-sentinel.schema.json"
		workflowHookSentinelCompiled, workflowHookSentinelCompiledErr = compileEmbeddedSchema(
			sc, workflowHookSentinelSchemaJSON, schemaURL, "workflow-hook-sentinel")
	})
	return workflowHookSentinelCompiled, workflowHookSentinelCompiledErr
}

// HookSentinelContext carries the skill-specific signals declared at sentinel
// write time. Fields are pointer-shaped where the zero value collides with a
// meaningful value (e.g. `eligible_snapshot_loaded: false` is a real ISP
// signal and must be distinguishable from "not declared"). Use `omitempty`
// on absent fields so the rendered JSON matches the schema's
// `additionalProperties: false` contract.
type HookSentinelContext struct {
	GitHeadAtStart         string   `json:"git_head_at_start,omitempty"`
	WriteScope             []string `json:"write_scope,omitempty"`
	EligibleSnapshotLoaded *bool    `json:"eligible_snapshot_loaded,omitempty"`
	MaxBatch               *int     `json:"max_batch,omitempty"`
	TracePathHint          string   `json:"trace_path_hint,omitempty"`
}

// HookSentinelDoc is the typed payload persisted at
// `.agents/active/hook-sentinels/<skill>-<run-id>.json`.
type HookSentinelDoc struct {
	SchemaVersion     int                  `json:"schema_version"`
	Skill             string               `json:"skill"`
	RunID             string               `json:"run_id"`
	StartedAt         string               `json:"started_at"`
	PlanID            string               `json:"plan_id"`
	TaskID            string               `json:"task_id"`
	AgentType         string               `json:"agent_type"`
	LifecyclePoint    string               `json:"lifecycle_point,omitempty"`
	ExpectedArtifacts []string             `json:"expected_artifacts,omitempty"`
	Context           *HookSentinelContext `json:"context,omitempty"`
}

// validateHookSentinelDoc checks doc against the embedded
// schemas/workflow-hook-sentinel.schema.json.
func validateHookSentinelDoc(doc *HookSentinelDoc) error {
	if doc == nil {
		return fmt.Errorf("hook sentinel: nil document")
	}
	sch, err := compiledWorkflowHookSentinelSchema(stdSchemaCompiler{})
	if err != nil {
		return err
	}
	b, err := jsonMarshal(doc)
	if err != nil {
		return fmt.Errorf("marshal hook sentinel for schema validation: %w", err)
	}
	var payload any
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("remap hook sentinel for schema validation: %w", err)
	}
	if err := sch.Validate(payload); err != nil {
		return fmt.Errorf("hook sentinel does not satisfy workflow-hook-sentinel.schema.json: %w", err)
	}
	return nil
}

// validHookSentinelSkill reports whether s is one of the three enforced
// skill names. Used as a filename pre-flight check before touching disk.
func validHookSentinelSkill(s string) bool {
	_, ok := hookSentinelAllowedSkills[s]
	return ok
}

// validHookSentinelAgentType reports whether s matches the schema enum.
func validHookSentinelAgentType(s string) bool {
	_, ok := hookSentinelAllowedAgentTypes[s]
	return ok
}

// validHookSentinelRunID enforces filename-safe characters before any
// filename is constructed. Mirrors the schema's `run_id` pattern.
func validHookSentinelRunID(s string) bool {
	if len(s) == 0 {
		return false
	}
	first := s[0]
	switch {
	case first >= 'A' && first <= 'Z':
	case first >= 'a' && first <= 'z':
	case first >= '0' && first <= '9':
	default:
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// hookSentinelActiveDir returns `.agents/active/hook-sentinels` rooted at
// projectPath. Callers MUST already have validated skill/run-id before
// composing a filename underneath.
func hookSentinelActiveDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "hook-sentinels")
}

// hookSentinelActivePath returns the full active file path for skill/run-id.
// Returns an error when either component is invalid so callers fail before
// any filesystem operation.
func hookSentinelActivePath(projectPath, skill, runID string) (string, error) {
	skill = strings.TrimSpace(skill)
	if !validHookSentinelSkill(skill) {
		return "", fmt.Errorf("invalid skill %q (allowed: iteration-close, isp, loop-worker)", skill)
	}
	runID = strings.TrimSpace(runID)
	if !validHookSentinelRunID(runID) {
		return "", fmt.Errorf("invalid run_id %q (must be filename-safe: [A-Za-z0-9][A-Za-z0-9._-]*)", runID)
	}
	return filepath.Join(hookSentinelActiveDir(projectPath), skill+"-"+runID+".json"), nil
}

// hookSentinelArchiveDir returns the durable archive directory for planID on
// the supplied date (UTC YYYY-MM-DD). Per D5/Q2 the destination lives under
// `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/`.
func hookSentinelArchiveDir(projectPath, planID string, dateUTC string) string {
	return filepath.Join(projectPath, ".agents", "history", planID, "hook-sentinels", dateUTC)
}

// writeHookSentinelAtomically validates doc and persists it via the
// temp-file-then-rename Unix atomic-write pattern so a concurrent stop hook
// can never read a partial JSON document. Returns an error on collision
// (v1 has no overwrite flag).
func writeHookSentinelAtomically(projectPath string, doc *HookSentinelDoc) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("hook sentinel: nil document")
	}
	target, err := hookSentinelActivePath(projectPath, doc.Skill, doc.RunID)
	if err != nil {
		return "", err
	}
	if err := validateHookSentinelDoc(doc); err != nil {
		return "", err
	}
	body, err := jsonMarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal hook sentinel: %w", err)
	}
	body = append(body, '\n')

	dir := filepath.Dir(target)
	if err := osMkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prepare hook sentinel dir: %w", err)
	}
	// Collision guard: v1 has no overwrite flag. Check before touching disk
	// so a race that does materialize the file later is still caught by the
	// atomic rename below (os.Rename on POSIX replaces the target, so the
	// stat-then-rename pattern is the explicit reject point — not a TOCTOU
	// guarantee, but the documented v1 behaviour).
	if _, statErr := osStat(target); statErr == nil {
		return "", fmt.Errorf("hook sentinel already exists at %s (v1 has no overwrite flag; call `clear` to archive it first)", target)
	}

	tmp := target + ".tmp"
	if err := osWriteFile(tmp, body, 0o644); err != nil {
		return "", fmt.Errorf("write hook sentinel temp: %w", err)
	}
	if err := osRename(tmp, target); err != nil {
		_ = osRemove(tmp)
		return "", fmt.Errorf("publish hook sentinel: %w", err)
	}
	return target, nil
}

// readHookSentinel loads and schema-validates the sentinel at path.
func readHookSentinel(path string) (*HookSentinelDoc, error) {
	data, err := osReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hook sentinel: %w", err)
	}
	var doc HookSentinelDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse hook sentinel: %w", err)
	}
	if err := validateHookSentinelDoc(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// readLatestHookSentinel returns the sentinel with the most recent
// `started_at` for skill. Filename is the deterministic tie-breaker when
// timestamps are equal. Returns an error when no sentinel exists for skill.
func readLatestHookSentinel(projectPath, skill string) (*HookSentinelDoc, string, error) {
	skill = strings.TrimSpace(skill)
	if !validHookSentinelSkill(skill) {
		return nil, "", fmt.Errorf("invalid skill %q (allowed: iteration-close, isp, loop-worker)", skill)
	}
	dir := hookSentinelActiveDir(projectPath)
	entries, err := osReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("no hook sentinels for skill %q (directory missing)", skill)
		}
		return nil, "", fmt.Errorf("list hook sentinel dir: %w", err)
	}
	prefix := skill + "-"
	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no hook sentinels for skill %q", skill)
	}
	// Stable filename sort breaks ties when started_at compares equal.
	sort.Strings(candidates)
	var best *HookSentinelDoc
	var bestPath string
	var bestStarted string
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		doc, err := readHookSentinel(path)
		if err != nil {
			return nil, "", err
		}
		if best == nil || doc.StartedAt > bestStarted ||
			(doc.StartedAt == bestStarted && filepath.Base(path) > filepath.Base(bestPath)) {
			best = doc
			bestPath = path
			bestStarted = doc.StartedAt
		}
	}
	return best, bestPath, nil
}

// clearHookSentinel archives the active record under
// `.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/` and removes it
// from the active tier. The destination date is derived from the sentinel's
// own `started_at` (UTC) so the same record always lands in the same
// archive bucket regardless of when `clear` runs.
func clearHookSentinel(projectPath, skill, runID string) (active, archive string, err error) {
	active, err = hookSentinelActivePath(projectPath, skill, runID)
	if err != nil {
		return "", "", err
	}
	doc, err := readHookSentinel(active)
	if err != nil {
		return "", "", err
	}
	startedAt, parseErr := time.Parse(time.RFC3339Nano, doc.StartedAt)
	if parseErr != nil {
		// Fall back to RFC3339 (no fractional seconds) before giving up.
		startedAt, parseErr = time.Parse(time.RFC3339, doc.StartedAt)
		if parseErr != nil {
			return "", "", fmt.Errorf("hook sentinel started_at %q is not RFC3339: %w", doc.StartedAt, parseErr)
		}
	}
	dateUTC := startedAt.UTC().Format("2006-01-02")
	archiveDir := hookSentinelArchiveDir(projectPath, doc.PlanID, dateUTC)
	if err := osMkdirAll(archiveDir, 0o755); err != nil {
		return "", "", fmt.Errorf("prepare hook sentinel archive dir: %w", err)
	}
	archive = filepath.Join(archiveDir, filepath.Base(active))
	if _, statErr := osStat(archive); statErr == nil {
		return "", "", fmt.Errorf("archive collision: %s already exists (v1 does not overwrite history)", archive)
	}
	if err := osRename(active, archive); err != nil {
		return "", "", fmt.Errorf("archive hook sentinel: %w", err)
	}
	return active, archive, nil
}

// hookSentinelWriteInputs bundles `write` flag values so the cobra RunE stays
// readable and the runner can be unit-tested without rebuilding cobra state.
type hookSentinelWriteInputs struct {
	Skill                  string
	RunID                  string
	PlanID                 string
	TaskID                 string
	AgentType              string
	ExpectedArtifacts      []string
	WriteScope             []string
	EligibleSnapshotLoaded *bool
	MaxBatch               *int
	TracePathHint          string
}

// buildHookSentinelDoc constructs a *HookSentinelDoc from validated input.
// The CLI captures git HEAD itself per the contract; callers cannot supply
// it. started_at is set to now (UTC, RFC3339Nano) so latest-selection ties
// are rare in practice.
func buildHookSentinelDoc(projectPath string, in hookSentinelWriteInputs) (*HookSentinelDoc, error) {
	if !validHookSentinelSkill(in.Skill) {
		return nil, fmt.Errorf("--skill must be one of iteration-close, isp, loop-worker (got %q)", in.Skill)
	}
	if !validHookSentinelRunID(in.RunID) {
		return nil, fmt.Errorf("--run-id must be filename-safe ([A-Za-z0-9][A-Za-z0-9._-]*)")
	}
	if strings.TrimSpace(in.PlanID) == "" {
		return nil, fmt.Errorf("--plan is required")
	}
	if strings.TrimSpace(in.TaskID) == "" {
		return nil, fmt.Errorf("--task is required")
	}
	if !validHookSentinelAgentType(in.AgentType) {
		return nil, fmt.Errorf("--agent-type must be main or loop-worker (got %q)", in.AgentType)
	}
	doc := &HookSentinelDoc{
		SchemaVersion:  HookSentinelSchemaVersion,
		Skill:          in.Skill,
		RunID:          in.RunID,
		StartedAt:      hookSentinelNow().UTC().Format(time.RFC3339Nano),
		PlanID:         in.PlanID,
		TaskID:         in.TaskID,
		AgentType:      in.AgentType,
		LifecyclePoint: hookSentinelLifecyclePointSkillEntry,
	}
	if len(in.ExpectedArtifacts) > 0 {
		doc.ExpectedArtifacts = append([]string{}, in.ExpectedArtifacts...)
	}
	head := strings.TrimSpace(gitOutput(projectPath, "rev-parse", "HEAD"))
	ctx := &HookSentinelContext{}
	hasCtx := false
	if head != "" {
		ctx.GitHeadAtStart = head
		hasCtx = true
	}
	if len(in.WriteScope) > 0 {
		ctx.WriteScope = append([]string{}, in.WriteScope...)
		hasCtx = true
	}
	if in.EligibleSnapshotLoaded != nil {
		v := *in.EligibleSnapshotLoaded
		ctx.EligibleSnapshotLoaded = &v
		hasCtx = true
	}
	if in.MaxBatch != nil {
		v := *in.MaxBatch
		ctx.MaxBatch = &v
		hasCtx = true
	}
	if strings.TrimSpace(in.TracePathHint) != "" {
		ctx.TracePathHint = strings.TrimSpace(in.TracePathHint)
		hasCtx = true
	}
	if hasCtx {
		doc.Context = ctx
	}
	return doc, nil
}

// runHookSentinelWrite is the cobra handler body for `write`.
func runHookSentinelWrite(in hookSentinelWriteInputs) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	doc, err := buildHookSentinelDoc(project.Path, in)
	if err != nil {
		return err
	}
	path, err := writeHookSentinelAtomically(project.Path, doc)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"status": "written",
			"path":   path,
			"skill":  doc.Skill,
			"run_id": doc.RunID,
		})
	}
	fmt.Fprintf(os.Stdout, "wrote hook sentinel: %s\n", path)
	return nil
}

// runHookSentinelRead is the cobra handler body for `read`.
func runHookSentinelRead(skill, runID string, latest, asJSON bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if !validHookSentinelSkill(skill) {
		return fmt.Errorf("invalid skill %q (allowed: iteration-close, isp, loop-worker)", skill)
	}
	if latest && strings.TrimSpace(runID) != "" {
		return fmt.Errorf("--latest and --run-id are mutually exclusive")
	}
	if !latest && strings.TrimSpace(runID) == "" {
		return fmt.Errorf("read requires either --run-id or --latest")
	}
	var doc *HookSentinelDoc
	var path string
	if latest {
		doc, path, err = readLatestHookSentinel(project.Path, skill)
		if err != nil {
			return err
		}
	} else {
		path, err = hookSentinelActivePath(project.Path, skill, runID)
		if err != nil {
			return err
		}
		doc, err = readHookSentinel(path)
		if err != nil {
			return err
		}
	}
	if asJSON || deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	fmt.Fprintf(os.Stdout, "%s\n", path)
	fmt.Fprintf(os.Stdout, "  skill=%s run_id=%s started_at=%s\n", doc.Skill, doc.RunID, doc.StartedAt)
	fmt.Fprintf(os.Stdout, "  plan=%s task=%s agent_type=%s\n", doc.PlanID, doc.TaskID, doc.AgentType)
	if len(doc.ExpectedArtifacts) > 0 {
		fmt.Fprintf(os.Stdout, "  expected_artifacts: %s\n", strings.Join(doc.ExpectedArtifacts, ", "))
	}
	return nil
}

// runHookSentinelClear is the cobra handler body for `clear`.
func runHookSentinelClear(skill, runID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	active, archive, err := clearHookSentinel(project.Path, skill, runID)
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"status":  "archived",
			"active":  active,
			"archive": archive,
		})
	}
	fmt.Fprintf(os.Stdout, "archived hook sentinel:\n  from: %s\n  to:   %s\n", active, archive)
	return nil
}

// newWorkflowHookSentinelCmd builds the `da workflow hook-sentinel` subtree.
// Wire from newWorkflowCmd.
func newWorkflowHookSentinelCmd() *cobra.Command {
	hookSentinelCmd := &cobra.Command{
		Use:   "hook-sentinel",
		Short: "Write/read/clear hook sentinels declaring per-skill stop-gate context",
		Long: `Sentinels are the contract between an enforced skill (iteration-close, isp,
loop-worker) and its Stop/SubagentStop gate. ` + "`write`" + ` records plan/task/agent
context at skill entry; ` + "`read`" + ` returns the latest or an exact record for the
gate; ` + "`clear`" + ` archives a successful record under
.agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/ (no record is silently
deleted in v1).`,
		Example: deps.ExampleBlock(
			"  da workflow hook-sentinel write loop-worker --run-id r1 --plan my-plan --task t1 --agent-type loop-worker",
			"  da workflow hook-sentinel read loop-worker --latest",
			"  da workflow hook-sentinel clear loop-worker --run-id r1",
		),
	}
	hookSentinelCmd.AddCommand(
		newWorkflowHookSentinelWriteCmd(),
		newWorkflowHookSentinelReadCmd(),
		newWorkflowHookSentinelClearCmd(),
	)
	return hookSentinelCmd
}

func newWorkflowHookSentinelWriteCmd() *cobra.Command {
	var (
		runID                      string
		planID                     string
		taskID                     string
		agentType                  string
		expectedArtifacts          []string
		writeScope                 []string
		eligibleSnapshotLoadedFlag bool
		eligibleSnapshotLoadedSet  bool
		maxBatch                   int
	)
	cmd := &cobra.Command{
		Use:   "write <skill>",
		Short: "Write a hook sentinel at skill entry",
		Example: deps.ExampleBlock(
			"  da workflow hook-sentinel write loop-worker --run-id r1 --plan my-plan --task t1 --agent-type loop-worker --write-scope commands/workflow/",
			"  da workflow hook-sentinel write isp --run-id r2 --plan my-plan --task t1 --agent-type main --eligible-snapshot-loaded --max-batch 3",
		),
		Args: deps.ExactArgsWithHints(1, "Pass one of: iteration-close, isp, loop-worker."),
		RunE: func(c *cobra.Command, args []string) error {
			in := hookSentinelWriteInputs{
				Skill:             args[0],
				RunID:             runID,
				PlanID:            planID,
				TaskID:            taskID,
				AgentType:         agentType,
				ExpectedArtifacts: expectedArtifacts,
				WriteScope:        writeScope,
				TracePathHint:     "", // intentionally unsupported as a flag in v1; reserved for hook-stdin authority
			}
			if eligibleSnapshotLoadedSet {
				v := eligibleSnapshotLoadedFlag
				in.EligibleSnapshotLoaded = &v
			}
			if c.Flags().Changed("max-batch") {
				v := maxBatch
				in.MaxBatch = &v
			}
			return runHookSentinelWrite(in)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Caller-supplied run identifier (required, filename-safe)")
	cmd.Flags().StringVar(&planID, "plan", "", "Canonical plan ID (required)")
	cmd.Flags().StringVar(&taskID, "task", "", "Task ID within the plan (required)")
	cmd.Flags().StringVar(&agentType, "agent-type", "", "Caller agent role: main or loop-worker (required)")
	cmd.Flags().StringArrayVar(&expectedArtifacts, "expect", nil, "Repo-relative artifact path the terminal gate must find (repeatable)")
	cmd.Flags().StringArrayVar(&writeScope, "write-scope", nil, "Allowed repo-relative path or glob (repeatable; loop-worker gate diffs edits against this list)")
	cmd.Flags().BoolVar(&eligibleSnapshotLoadedFlag, "eligible-snapshot-loaded", false, "ISP gate signal: orchestrator loaded the eligible-task snapshot at session-start")
	cmd.Flags().IntVar(&maxBatch, "max-batch", 0, "ISP gate signal: declared maximum bundles to materialize this turn")
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		eligibleSnapshotLoadedSet = c.Flags().Changed("eligible-snapshot-loaded")
		return nil
	}
	_ = cmd.MarkFlagRequired("run-id")
	_ = cmd.MarkFlagRequired("plan")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("agent-type")
	return cmd
}

func newWorkflowHookSentinelReadCmd() *cobra.Command {
	var (
		runID  string
		latest bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "read <skill>",
		Short: "Read a hook sentinel by --run-id or --latest",
		Example: deps.ExampleBlock(
			"  da workflow hook-sentinel read loop-worker --latest",
			"  da workflow hook-sentinel read isp --run-id r2 --json",
		),
		Args: deps.ExactArgsWithHints(1, "Pass one of: iteration-close, isp, loop-worker."),
		RunE: func(c *cobra.Command, args []string) error {
			return runHookSentinelRead(args[0], runID, latest, asJSON)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Exact run identifier to read")
	cmd.Flags().BoolVar(&latest, "latest", false, "Read the most recent sentinel for this skill (filename tie-breaker)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the sentinel as JSON (also honours --json global flag)")
	return cmd
}

func newWorkflowHookSentinelClearCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "clear <skill>",
		Short: "Archive a hook sentinel to .agents/history/<plan-id>/hook-sentinels/<YYYY-MM-DD>/",
		Example: deps.ExampleBlock(
			"  da workflow hook-sentinel clear loop-worker --run-id r1",
		),
		Args: deps.ExactArgsWithHints(1, "Pass one of: iteration-close, isp, loop-worker."),
		RunE: func(c *cobra.Command, args []string) error {
			return runHookSentinelClear(args[0], runID)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run identifier of the sentinel to archive (required)")
	_ = cmd.MarkFlagRequired("run-id")
	return cmd
}
