// Package workflow's hook_outcome.go provides `da workflow hook-outcome write`,
// the CLI primitive R1.5 requires (per design D2/R2.1) so gates append outcome
// records to `.agents/active/iteration-log/iter-N.hook-outcomes.yaml` via this
// single bottleneck rather than writing the YAML by hand from gate.sh. Mirrors
// the shape of hook_sentinel.go (schema-validation, atomic temp+rename write,
// file-local seams for fault injection in tests).
package workflow

import (
	// _ "embed": pull in static/workflow-hook-outcome.schema.json via go:embed.
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// HookOutcomeSchemaVersion is the current schema version emitted by
// `da workflow hook-outcome write` and persisted at the top of each
// iter-N.hook-outcomes.yaml sidecar.
const HookOutcomeSchemaVersion = 1

// hookOutcomeAppendResult.Status enum values. Constants so the JSON output
// shape, the doc comments on hookOutcomeAppendResult, and the runner's
// switch arms all reference the same literal.
const (
	hookOutcomeStatusWritten           = "written"
	hookOutcomeStatusDuplicate         = "duplicate"
	hookOutcomeStatusNoActiveIteration = "no-active-iteration"
)

// hookOutcomeWriteTimeout bounds the entire write operation per R2.4 (the
// upstream R5.4 hook budget of 8000ms in loop-discipline-stop-hooks). The
// CLI exceeds this only as a sanity ceiling; real filesystem ops complete in
// single-digit milliseconds.
const hookOutcomeWriteTimeout = 8 * time.Second

// The five hook-outcome enums mirror the schema enums in
// workflow-hook-outcome.schema.json. Their vocabularies are declared once in
// enums.go as EnumSpecs, so the `--help` listing, the completion candidates,
// and these guards all read the same slice.

// hookOutcomeRuleIDPattern mirrors the schema regex for rule_id.
var hookOutcomeRuleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[A-Za-z0-9]+(?:\.[A-Za-z0-9]+)*)+$`)

// hookOutcomeDeps is the narrow collaborator the hook-outcome CLI needs
// (interface-DI per docs/TEST_SEAMS.md; mirrors commands/review.go's
// reviewDeps pattern). One interface covers the os-level fault injection
// points (ReadFile/ReadDir/Rename/Remove), the workflow-project resolver,
// and the clock so the runner has a single fault-injection surface.
// File-scoped — do not share with other commands/workflow files.
type hookOutcomeDeps interface {
	Now() time.Time
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]os.DirEntry, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	ResolveProject() (workflowProjectRef, error)
}

// stdHookOutcomeDeps is the production hookOutcomeDeps backed by the os
// package and currentWorkflowProject. Zero-value usable; tests construct
// fakeHookOutcomeDeps{} (see hook_outcome_test.go) where each nil-func
// field delegates to this default.
type stdHookOutcomeDeps struct{}

func (stdHookOutcomeDeps) Now() time.Time                       { return time.Now() }
func (stdHookOutcomeDeps) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
func (stdHookOutcomeDeps) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}
func (stdHookOutcomeDeps) Rename(o, n string) error { return os.Rename(o, n) }
func (stdHookOutcomeDeps) Remove(name string) error { return os.Remove(name) }
func (stdHookOutcomeDeps) ResolveProject() (workflowProjectRef, error) {
	return currentWorkflowProject()
}

// HookOutcomeRecord is one record in iter-N.hook-outcomes.yaml. Field
// names mirror the schema; tags are explicit so YAML and JSON shapes agree
// (the schema validates the JSON projection). No tag for the disallowed
// fields exists by construction — they are not modelled here.
type HookOutcomeRecord struct {
	SchemaVersion        int    `json:"schema_version" yaml:"schema_version"`
	SentinelID           string `json:"sentinel_id" yaml:"sentinel_id"`
	Skill                string `json:"skill" yaml:"skill"`
	LifecyclePoint       string `json:"lifecycle_point" yaml:"lifecycle_point"`
	InterventionClass    string `json:"intervention_class" yaml:"intervention_class"`
	Result               string `json:"result" yaml:"result"`
	RuleID               string `json:"rule_id" yaml:"rule_id"`
	Platform             string `json:"platform" yaml:"platform"`
	TS                   string `json:"ts" yaml:"ts"`
	ArchivedSentinelPath string `json:"archived_sentinel_path,omitempty" yaml:"archived_sentinel_path,omitempty"`
	CorrelationID        string `json:"correlation_id" yaml:"correlation_id"`
}

// HookOutcomeSidecar is the iter-N.hook-outcomes.yaml top-level shape.
type HookOutcomeSidecar struct {
	SchemaVersion int                 `json:"schema_version" yaml:"schema_version"`
	Records       []HookOutcomeRecord `json:"records" yaml:"records"`
}

// validateHookOutcomeSidecar checks the full sidecar against the embedded
// schema. The same path covers single-record validation indirectly because
// the schema's records[].items references the same hookOutcome $def.
func validateHookOutcomeSidecar(sc *HookOutcomeSidecar) error {
	if sc == nil {
		return fmt.Errorf("hook outcome: nil sidecar")
	}
	sch, err := compiledWorkflowHookOutcomeSchema(stdSchemaCompiler{})
	if err != nil {
		return err
	}
	b, err := jsonMarshal(sc)
	if err != nil {
		return fmt.Errorf("marshal hook outcome for schema validation: %w", err)
	}
	var payload any
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("remap hook outcome for schema validation: %w", err)
	}
	if err := sch.Validate(payload); err != nil {
		return fmt.Errorf("hook outcome does not satisfy workflow-hook-outcome.schema.json: %w", err)
	}
	return nil
}

// validHookOutcomeSkill / lifecycle / intervention / result / platform are
// enum-style guards that let the CLI fail fast before any disk touch with a
// flag-specific message (the schema also enforces, but its errors are less
// targeted than a "--skill must be one of …" message).
func validHookOutcomeSkill(s string) bool {
	return s != "" && hookOutcomeSkillEnum.Contains(s)
}

func validHookOutcomeLifecyclePoint(s string) bool {
	return s != "" && hookOutcomeLifecycleEnum.Contains(s)
}

func validHookOutcomeInterventionClass(s string) bool {
	return s != "" && hookOutcomeInterventionEnum.Contains(s)
}

func validHookOutcomeResult(s string) bool {
	return s != "" && hookOutcomeResultEnum.Contains(s)
}

func validHookOutcomePlatform(s string) bool {
	return s != "" && hookOutcomePlatformEnum.Contains(s)
}

// requireHookOutcomeEnum rejects both a missing and an out-of-set value, naming
// the vocabulary either way. Unlike EnumSpec.Validate — which lets an empty
// value through because absence is usually a separate `required` check — every
// hook-outcome enum is mandatory, so empty and wrong fail identically here.
func requireHookOutcomeEnum(spec cmdutil.EnumSpec, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--%s is required: pass one of %s", spec.Name, spec.ValueList())
	}
	return spec.Validate(value)
}

// validHookOutcomeRuleID enforces the schema regex up-front so the user sees
// a "--rule-id must match <pattern>" message rather than the raw schema
// rejection at validate time.
func validHookOutcomeRuleID(s string) bool {
	if s == "" {
		return false
	}
	return hookOutcomeRuleIDPattern.MatchString(s)
}

// hookOutcomeSidecarPath returns the canonical sidecar path for iteration N
// (joined under .agents/active/iteration-log/iter-N.hook-outcomes.yaml).
func hookOutcomeSidecarPath(projectPath string, n int) string {
	return filepath.Join(IterationLogDir(projectPath), fmt.Sprintf("iter-%d.hook-outcomes.yaml", n))
}

// resolveActiveIterationN inspects the iter-log directory and returns the
// highest existing iter-N.yaml number (the iteration the gate write should
// append to). Returns (0, false, nil) when no canonical iteration file
// exists yet — the caller treats that as "no active iteration, exit 0 silent
// with stderr advisory" per R2.2.
//
// Errors propagate only for filesystem failures other than "directory does
// not exist" (which is also treated as no active iteration).
func resolveActiveIterationN(hod hookOutcomeDeps, projectPath string) (int, bool, error) {
	iterDir := IterationLogDir(projectPath)
	entries, err := hod.ReadDir(iterDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read iteration-log dir: %w", err)
	}
	maxN := 0
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := iterLogFileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
			continue
		}
		if n > maxN {
			maxN = n
			found = true
		}
	}
	return maxN, found, nil
}

// loadHookOutcomeSidecar reads an existing sidecar or returns a fresh empty
// shell when the file does not exist. Returned sidecar is schema-valid only
// after a record is appended (an empty sidecar with records=[] is valid by
// schema construction; absent file is not an error).
func loadHookOutcomeSidecar(hod hookOutcomeDeps, path string) (*HookOutcomeSidecar, error) {
	data, err := hod.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &HookOutcomeSidecar{
				SchemaVersion: HookOutcomeSchemaVersion,
				Records:       []HookOutcomeRecord{},
			}, nil
		}
		return nil, fmt.Errorf("read hook outcome sidecar: %w", err)
	}
	var sc HookOutcomeSidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parse hook outcome sidecar %s: %w", path, err)
	}
	if sc.SchemaVersion == 0 {
		// Treat a zero schema_version as a corruption guard rather than a
		// silent default: the write path always sets v1, so a v0 file on
		// disk means something else wrote it (or it was hand-edited).
		return nil, fmt.Errorf("hook outcome sidecar %s missing schema_version (expected %d)", path, HookOutcomeSchemaVersion)
	}
	if sc.SchemaVersion != HookOutcomeSchemaVersion {
		return nil, fmt.Errorf("hook outcome sidecar %s has unsupported schema_version %d (expected %d)", path, sc.SchemaVersion, HookOutcomeSchemaVersion)
	}
	if sc.Records == nil {
		sc.Records = []HookOutcomeRecord{}
	}
	return &sc, nil
}

// hookOutcomeIdempotencyKeyMatches reports whether two records collide on
// the R2.3 idempotency key: (sentinel_id, rule_id, lifecycle_point, intervention_class).
// When two records collide, the second write is a no-op — the existing record
// is preserved (ts is not refreshed) so re-runs of a recoverable platform
// retry do not silently rewrite history.
func hookOutcomeIdempotencyKeyMatches(a, b HookOutcomeRecord) bool {
	return a.SentinelID == b.SentinelID &&
		a.RuleID == b.RuleID &&
		a.LifecyclePoint == b.LifecyclePoint &&
		a.InterventionClass == b.InterventionClass
}

// writeHookOutcomeSidecar persists sc to path via the temp-file-then-rename
// atomic write pattern (mirrors hook_sentinel.go). The schema is validated
// before any disk touch.
func writeHookOutcomeSidecar(hod hookOutcomeDeps, path string, sc *HookOutcomeSidecar) error {
	if err := validateHookOutcomeSidecar(sc); err != nil {
		return err
	}
	body, err := yamlMarshal(sc)
	if err != nil {
		return fmt.Errorf("marshal hook outcome sidecar: %w", err)
	}
	const header = "# yaml-language-server: $schema=../../../../schemas/workflow-hook-outcome.schema.json\n"
	content := []byte(header + string(body))
	dir := filepath.Dir(path)
	if err := osMkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("prepare hook outcome dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := osWriteFile(tmp, content, 0o644); err != nil {
		return fmt.Errorf("write hook outcome temp: %w", err)
	}
	if err := hod.Rename(tmp, path); err != nil {
		_ = hod.Remove(tmp)
		return fmt.Errorf("publish hook outcome sidecar: %w", err)
	}
	return nil
}

// hookOutcomeWriteInputs bundles flag values so the cobra RunE stays
// readable and the runner is unit-testable without rebuilding cobra state.
type hookOutcomeWriteInputs struct {
	SentinelID           string
	Skill                string
	LifecyclePoint       string
	InterventionClass    string
	Result               string
	RuleID               string
	Platform             string
	CorrelationID        string
	ArchivedSentinelPath string
	TS                   string
}

// buildHookOutcomeRecord constructs and validates a single record from
// CLI inputs. Defaults: ts → now, correlation_id → sentinel_id when the
// flag is omitted (per D2 record contract).
func buildHookOutcomeRecord(hod hookOutcomeDeps, in hookOutcomeWriteInputs) (HookOutcomeRecord, error) {
	in.SentinelID = strings.TrimSpace(in.SentinelID)
	if in.SentinelID == "" {
		return HookOutcomeRecord{}, fmt.Errorf("--sentinel-id is required")
	}
	if err := requireHookOutcomeEnum(hookOutcomeSkillEnum, in.Skill); err != nil {
		return HookOutcomeRecord{}, err
	}
	if err := requireHookOutcomeEnum(hookOutcomeLifecycleEnum, in.LifecyclePoint); err != nil {
		return HookOutcomeRecord{}, err
	}
	if err := requireHookOutcomeEnum(hookOutcomeInterventionEnum, in.InterventionClass); err != nil {
		return HookOutcomeRecord{}, err
	}
	if err := requireHookOutcomeEnum(hookOutcomeResultEnum, in.Result); err != nil {
		return HookOutcomeRecord{}, err
	}
	if !validHookOutcomeRuleID(in.RuleID) {
		return HookOutcomeRecord{}, fmt.Errorf("--rule-id %q does not match required pattern (e.g. iteration-close.R1.1)", in.RuleID)
	}
	if err := requireHookOutcomeEnum(hookOutcomePlatformEnum, in.Platform); err != nil {
		return HookOutcomeRecord{}, err
	}
	ts := strings.TrimSpace(in.TS)
	if ts == "" {
		ts = hod.Now().UTC().Format(time.RFC3339Nano)
	}
	corr := strings.TrimSpace(in.CorrelationID)
	if corr == "" {
		corr = in.SentinelID
	}
	return HookOutcomeRecord{
		SchemaVersion:        HookOutcomeSchemaVersion,
		SentinelID:           in.SentinelID,
		Skill:                in.Skill,
		LifecyclePoint:       in.LifecyclePoint,
		InterventionClass:    in.InterventionClass,
		Result:               in.Result,
		RuleID:               in.RuleID,
		Platform:             in.Platform,
		TS:                   ts,
		ArchivedSentinelPath: strings.TrimSpace(in.ArchivedSentinelPath),
		CorrelationID:        corr,
	}, nil
}

// hookOutcomeAppendResult describes the outcome of a single write call so
// the caller (CLI handler or test) can distinguish "appended", "duplicate
// idempotent no-op", and "no active iteration" without parsing stderr.
type hookOutcomeAppendResult struct {
	Status     string // one of hookOutcomeStatus{Written,Duplicate,NoActiveIteration}
	Path       string // sidecar path (empty when status == hookOutcomeStatusNoActiveIteration)
	Iteration  int    // active N (zero when status == hookOutcomeStatusNoActiveIteration)
	RecordHash string // (sentinel_id, rule_id, lifecycle_point, intervention_class) joined for readback
}

// appendHookOutcome resolves N from the iter-log dir, loads the existing
// sidecar (if any), checks the idempotency key, appends the record, and
// writes back atomically. Returns the structured result so the cobra
// handler can render it in --json mode without re-reading the file.
func appendHookOutcome(hod hookOutcomeDeps, projectPath string, rec HookOutcomeRecord) (hookOutcomeAppendResult, error) {
	n, active, err := resolveActiveIterationN(hod, projectPath)
	if err != nil {
		return hookOutcomeAppendResult{}, err
	}
	if !active {
		return hookOutcomeAppendResult{Status: hookOutcomeStatusNoActiveIteration}, nil
	}
	path := hookOutcomeSidecarPath(projectPath, n)
	sc, err := loadHookOutcomeSidecar(hod, path)
	if err != nil {
		return hookOutcomeAppendResult{}, err
	}
	for _, existing := range sc.Records {
		if hookOutcomeIdempotencyKeyMatches(existing, rec) {
			return hookOutcomeAppendResult{
				Status:     hookOutcomeStatusDuplicate,
				Path:       path,
				Iteration:  n,
				RecordHash: hookOutcomeRecordKey(rec),
			}, nil
		}
	}
	sc.Records = append(sc.Records, rec)
	if err := writeHookOutcomeSidecar(hod, path, sc); err != nil {
		return hookOutcomeAppendResult{}, err
	}
	return hookOutcomeAppendResult{
		Status:     hookOutcomeStatusWritten,
		Path:       path,
		Iteration:  n,
		RecordHash: hookOutcomeRecordKey(rec),
	}, nil
}

// hookOutcomeRecordKey is the canonical join of the four idempotency-key
// fields, used for --json readback so a caller can correlate a write with
// any prior duplicate.
func hookOutcomeRecordKey(rec HookOutcomeRecord) string {
	return strings.Join([]string{
		rec.SentinelID,
		rec.RuleID,
		rec.LifecyclePoint,
		rec.InterventionClass,
	}, "|")
}

// runHookOutcomeWrite is the cobra handler body for `write`. It enforces
// the R2.4 timeout via a deadlined goroutine, applies the no-iteration
// silent-exit behaviour, and renders JSON or human output.
func runHookOutcomeWrite(hod hookOutcomeDeps, in hookOutcomeWriteInputs) error {
	project, err := hod.ResolveProject()
	if err != nil {
		return err
	}
	rec, err := buildHookOutcomeRecord(hod, in)
	if err != nil {
		return err
	}

	// Bound the entire operation by R2.4. A real filesystem write completes
	// in <10ms; this ceiling exists to guarantee a stuck rename (e.g. a
	// frozen network mount) cannot break the upstream R5.4 hook budget.
	type appendOut struct {
		res hookOutcomeAppendResult
		err error
	}
	done := make(chan appendOut, 1)
	go func() {
		res, err := appendHookOutcome(hod, project.Path, rec)
		done <- appendOut{res: res, err: err}
	}()
	var out appendOut
	select {
	case out = <-done:
	case <-time.After(hookOutcomeWriteTimeout):
		return fmt.Errorf("hook outcome write exceeded %s (R2.4 hook budget)", hookOutcomeWriteTimeout)
	}
	if out.err != nil {
		return out.err
	}

	switch out.res.Status {
	case hookOutcomeStatusNoActiveIteration:
		// Per R2.2: silent exit 0 with stderr advisory; the sentinel was
		// active but no iteration log exists yet, so this outcome is
		// session-only and dropped from scoring.
		fmt.Fprintln(os.Stderr, "hook-outcome write: no active iteration (iter-log dir empty); record dropped per R2.2")
		if deps.Flags.JSON() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"status": hookOutcomeStatusNoActiveIteration,
			})
		}
		return nil
	case hookOutcomeStatusDuplicate:
		if deps.Flags.JSON() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"status":     hookOutcomeStatusDuplicate,
				"path":       out.res.Path,
				"iteration":  out.res.Iteration,
				"record_key": out.res.RecordHash,
			})
		}
		fmt.Fprintf(os.Stdout, "duplicate hook outcome (idempotent no-op): %s\n", out.res.Path)
		return nil
	case hookOutcomeStatusWritten:
		if deps.Flags.JSON() {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"status":     hookOutcomeStatusWritten,
				"path":       out.res.Path,
				"iteration":  out.res.Iteration,
				"record_key": out.res.RecordHash,
			})
		}
		fmt.Fprintf(os.Stdout, "wrote hook outcome: %s (iter-%d)\n", out.res.Path, out.res.Iteration)
		return nil
	default:
		return fmt.Errorf("hook outcome write: unexpected status %q", out.res.Status)
	}
}

// newWorkflowHookOutcomeCmd builds the `da workflow hook-outcome` subtree.
// Wire from newWorkflowCmd.
func newWorkflowHookOutcomeCmd() *cobra.Command {
	hookOutcomeCmd := &cobra.Command{
		Use:   "hook-outcome",
		Short: "Append hook gate outcomes to the active iteration's sidecar",
		Long: `Hook gates (iteration-close, isp, loop-worker) emit one outcome record per
evaluated sentinel-anchored invocation. ` + "`write`" + ` appends a record to
.agents/active/iteration-log/iter-N.hook-outcomes.yaml, where N is resolved
from the highest existing iter-N.yaml entry. Idempotent on
(sentinel_id, rule_id, lifecycle_point, intervention_class) per R2.3 so
recoverable platform retries do not inflate the record list. Silent exit 0
with stderr advisory when no iteration is active (R2.2). Schema:
schemas/workflow-hook-outcome.schema.json.`,
		Example: deps.ExampleBlock(
			"  da workflow hook-outcome write --sentinel-id iteration-close-r1 --skill iteration-close \\",
			"      --lifecycle-point stop --intervention-class remediate_at_stop --result remediate \\",
			"      --rule-id iteration-close.R1.1 --platform claude",
		),
	}
	hookOutcomeCmd.AddCommand(newWorkflowHookOutcomeWriteCmd())
	return hookOutcomeCmd
}

func newWorkflowHookOutcomeWriteCmd() *cobra.Command {
	var in hookOutcomeWriteInputs
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Append one outcome record to iter-N.hook-outcomes.yaml",
		Example: deps.ExampleBlock(
			"  da workflow hook-outcome write \\",
			"      --sentinel-id loop-worker-r1 --skill loop-worker \\",
			"      --lifecycle-point subagent_stop --intervention-class remediate_at_stop \\",
			"      --result remediate --rule-id loop-worker.R3.1 --platform claude",
		),
		Args: deps.NoArgsWithHints("hook-outcome write takes no positional arguments; use flags."),
		RunE: func(c *cobra.Command, args []string) error {
			return runHookOutcomeWrite(stdHookOutcomeDeps{}, in)
		},
	}
	cmd.Flags().StringVar(&in.SentinelID, "sentinel-id", "", "Stable <skill>-<run-id> identifier joining this record to the archived sentinel (required)")
	cmdutil.RegisterEnum(cmd, &in.Skill, hookOutcomeSkillEnum)
	cmdutil.RegisterEnum(cmd, &in.LifecyclePoint, hookOutcomeLifecycleEnum)
	cmdutil.RegisterEnum(cmd, &in.InterventionClass, hookOutcomeInterventionEnum)
	cmdutil.RegisterEnum(cmd, &in.Result, hookOutcomeResultEnum)
	cmd.Flags().StringVar(&in.RuleID, "rule-id", "", "Stable rule identifier such as iteration-close.R1.1 (required)")
	cmdutil.RegisterEnum(cmd, &in.Platform, hookOutcomePlatformEnum)
	cmd.Flags().StringVar(&in.CorrelationID, "correlation-id", "", "Groups pre+terminal records for the same intent (defaults to sentinel-id)")
	cmd.Flags().StringVar(&in.ArchivedSentinelPath, "archived-sentinel-path", "", "Repo-relative POSIX path to the archived sentinel (empty for pre_tool_use records written before archive)")
	cmd.Flags().StringVar(&in.TS, "ts", "", "RFC3339 timestamp; defaults to now (UTC, nanosecond precision)")
	_ = cmd.MarkFlagRequired("sentinel-id")
	_ = cmd.MarkFlagRequired("skill")
	_ = cmd.MarkFlagRequired("lifecycle-point")
	_ = cmd.MarkFlagRequired("intervention-class")
	_ = cmd.MarkFlagRequired("result")
	_ = cmd.MarkFlagRequired("rule-id")
	_ = cmd.MarkFlagRequired("platform")
	return cmd
}
