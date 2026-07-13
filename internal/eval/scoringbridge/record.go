// record.go is the iteration-record emitter: it renders an EvalRun as a
// schema_version-2 workflow iter-log entry (schemas/workflow-iter-log.
// schema.json) and provides the matching in-memory translation to
// scoring.IterationRecord.
//
// The emit shapes below are the write-side twin of scoring's read-side rawV2
// shape: field names and YAML tags must stay aligned with the schema so that
// scoring.LoadIterationLog — and therefore `da score iteration` (spec R5) —
// parses the eval space byte-for-byte the way production entries parse. The
// round-trip is pinned by tests: emitRecord.toIterationRecord() must equal
// what scoring.ParseIterationRecord yields from the marshaled bytes.
package scoringbridge

import (
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// Verifier slugs and statuses recorded for the sandbox verification commands.
// The values come from the iter-log schema's verifier status enum
// (pass|fail|partial|unknown); "unknown" marks a command that never ran and
// is excluded from scoring's verifier mean.
const (
	verifierTypeBuild = "build"
	verifierTypeTest  = "test"

	statusPass    = "pass"
	statusFail    = "fail"
	statusUnknown = "unknown"
)

// Timestamp layouts matching the iter-log schema's date / checkpoint_at
// patterns.
const (
	dateLayout       = "2006-01-02"
	checkpointLayout = "2006-01-02T15:04:05Z"
)

// emitRecord is the on-disk YAML shape of the eval iteration entry — a
// schema_version-2 iter-log document restricted to the fields an eval run
// populates. Every tag is a schema-defined key (additionalProperties: false).
type emitRecord struct {
	SchemaVersion int            `yaml:"schema_version"`
	Iteration     int            `yaml:"iteration"`
	Date          string         `yaml:"date"`
	Wave          string         `yaml:"wave"`
	TaskID        string         `yaml:"task_id"`
	Commit        string         `yaml:"commit"`
	CheckpointAt  string         `yaml:"checkpoint_at"`
	Agent         *emitAgent     `yaml:"agent,omitempty"`
	SessionTokens *emitTokens    `yaml:"session_tokens,omitempty"`
	Impl          emitImpl       `yaml:"impl"`
	Verifiers     []emitVerifier `yaml:"verifiers"`
	Review        emitReview     `yaml:"review"`
}

// emitAgent is the agent-identity block (spec R9).
type emitAgent struct {
	SessionID string `yaml:"session_id,omitempty"`
	Harness   string `yaml:"harness,omitempty"`
	Model     string `yaml:"model,omitempty"`
}

// emitTokens is the session_tokens block, mirroring scoring.TokenUsage.
type emitTokens struct {
	InputTokens         int     `yaml:"input_tokens"`
	OutputTokens        int     `yaml:"output_tokens"`
	CacheReadTokens     int     `yaml:"cache_read_tokens"`
	CacheCreationTokens int     `yaml:"cache_creation_tokens"`
	CacheHitRate        float64 `yaml:"cache_hit_rate"`
}

// emitImpl is the impl-role block. Eval agents produce no self-assessment;
// the block carries only the run summary and the runner retry count (a
// correction-pressure input).
type emitImpl struct {
	Summary string `yaml:"summary"`
	Retries int    `yaml:"retries"`
}

// emitVerifier is one verifiers[] entry. TestsTotalPass is a pointer so the
// tri-state schema field (boolean|null) can stay unset — distinct from a
// reported false — when the command never ran.
type emitVerifier struct {
	Type           string `yaml:"type"`
	Status         string `yaml:"status"`
	GatePassed     bool   `yaml:"gate_passed"`
	TestsTotalPass *bool  `yaml:"tests_total_pass,omitempty"`
}

// emitReview is the review-role block. Eval runs have no review stage; the
// block is emitted empty because the schema requires the key, and empty enum
// strings are the schema's own "no decision" value.
type emitReview struct {
	Phase1Decision  string `yaml:"phase_1_decision"`
	Phase2Decision  string `yaml:"phase_2_decision"`
	OverallDecision string `yaml:"overall_decision"`
}

// buildEmitRecord translates one EvalRun into the iter-log entry shape. The
// wave field carries the run ID — an eval run belongs to no plan, and the run
// ID is what R2's dashboard joins the score back to its eval-run sidecar by.
func buildEmitRecord(run EvalRun) emitRecord {
	finished := run.FinishedAt
	if finished.IsZero() {
		finished = time.Now()
	}
	finished = finished.UTC()

	return emitRecord{
		SchemaVersion: 2,
		Iteration:     evalIteration,
		Date:          finished.Format(dateLayout),
		Wave:          run.RunID,
		TaskID:        run.Spec.TaskID,
		Commit:        run.BaseCommit,
		CheckpointAt:  finished.Format(checkpointLayout),
		Agent:         agentBlock(run.Agent),
		SessionTokens: tokensBlock(run.Agent.Tokens),
		Impl: emitImpl{
			Summary: runSummary(run),
			Retries: run.Agent.Retries,
		},
		Verifiers: verifierBlocks(run.Verify),
		Review:    emitReview{},
	}
}

// runSummary renders the impl summary line from the task metadata.
func runSummary(run EvalRun) string {
	return fmt.Sprintf("eval run %s: %s/%s task %s scored via the R1 bridge",
		run.RunID, run.Spec.Language, run.Spec.Difficulty, run.Spec.TaskID)
}

// agentBlock maps the runner identity to the agent block, or nil when the
// telemetry carried no identity at all (the block is optional in the schema).
func agentBlock(a AgentTelemetry) *emitAgent {
	if a.SessionID == "" && a.Harness == "" && a.Model == "" {
		return nil
	}
	return &emitAgent{SessionID: a.SessionID, Harness: a.Harness, Model: a.Model}
}

// tokensBlock maps captured token telemetry to the session_tokens block, or
// nil when none was captured.
func tokensBlock(t *scoring.TokenUsage) *emitTokens {
	if t == nil {
		return nil
	}
	return &emitTokens{
		InputTokens:         t.InputTokens,
		OutputTokens:        t.OutputTokens,
		CacheReadTokens:     t.CacheReadTokens,
		CacheCreationTokens: t.CacheCreationTokens,
		CacheHitRate:        t.CacheHitRate,
	}
}

// verifierBlocks maps the sandbox verification outcome to verifiers[]
// entries: an optional "build" entry when the build command ran, and always a
// "test" entry — TaskSpec requires test_cmd, so its absence from the log
// would misreport the run. A test command that never ran is recorded with
// status "unknown" and an unset tests_total_pass, which scoring excludes
// rather than counting as a failure.
func verifierBlocks(v VerifyResult) []emitVerifier {
	var out []emitVerifier
	if v.BuildRan {
		out = append(out, emitVerifier{
			Type:       verifierTypeBuild,
			Status:     passFailStatus(v.BuildPassed),
			GatePassed: v.BuildPassed,
		})
	}

	test := emitVerifier{Type: verifierTypeTest, Status: statusUnknown}
	if v.TestRan {
		passed := v.TestPassed
		test.Status = passFailStatus(passed)
		test.GatePassed = passed
		test.TestsTotalPass = &passed
	}
	return append(out, test)
}

// passFailStatus maps a command outcome to the schema's verifier status enum.
func passFailStatus(passed bool) string {
	if passed {
		return statusPass
	}
	return statusFail
}

// toIterationRecord is the in-memory twin of parsing the marshaled record
// through scoring.ParseIterationRecord — the translation the round-trip test
// pins. Building the scored record from the same emit struct that was
// persisted guarantees the score sidecar and the on-disk iteration record can
// never describe different runs.
func (e emitRecord) toIterationRecord() scoring.IterationRecord {
	rec := scoring.IterationRecord{
		SchemaVersion: e.SchemaVersion,
		Iteration:     e.Iteration,
		Date:          e.Date,
		Wave:          e.Wave,
		TaskID:        e.TaskID,
		Commit:        e.Commit,
		CheckpointAt:  e.CheckpointAt,
		Impl: scoring.ImplBlock{
			Summary: e.Impl.Summary,
			Retries: e.Impl.Retries,
		},
	}
	if e.Agent != nil {
		rec.Agent = scoring.AgentInfo{
			SessionID: e.Agent.SessionID,
			Harness:   e.Agent.Harness,
			Model:     e.Agent.Model,
		}
	}
	if e.SessionTokens != nil {
		rec.SessionTokens = &scoring.TokenUsage{
			InputTokens:         e.SessionTokens.InputTokens,
			OutputTokens:        e.SessionTokens.OutputTokens,
			CacheReadTokens:     e.SessionTokens.CacheReadTokens,
			CacheCreationTokens: e.SessionTokens.CacheCreationTokens,
			CacheHitRate:        e.SessionTokens.CacheHitRate,
		}
	}
	for _, v := range e.Verifiers {
		rec.Verifiers = append(rec.Verifiers, scoring.VerifierRecord{
			Type:           v.Type,
			Status:         v.Status,
			GatePassed:     v.GatePassed,
			TestsTotalPass: optionalBool(v.TestsTotalPass),
		})
	}
	return rec
}

// optionalBool lifts a *bool into scoring's tri-state OptionalBool: nil stays
// unset ("not reported"), non-nil carries the reported value.
func optionalBool(v *bool) scoring.OptionalBool {
	if v == nil {
		return scoring.OptionalBool{}
	}
	return scoring.OptionalBool{Set: true, Value: *v}
}
