package scoring

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// OptionalBool is a tri-state boolean parsed leniently from the iteration log.
// The schema specifies boolean|null for the test-pass flags, but real entries
// also carry integers (a pass count, e.g. tests_total_pass: 3) and strings.
// Unset (the zero value) means the agent did not report — distinct from a
// reported false.
type OptionalBool struct {
	Set   bool
	Value bool
}

// UnmarshalYAML coerces a YAML scalar into a tri-state bool: null and the empty
// string stay unset; a real boolean maps directly; a non-zero integer is true;
// a parseable string is taken at face value.
func (o *OptionalBool) UnmarshalYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!null":
		return nil
	case "!!bool":
		o.Set, o.Value = true, node.Value == "true"
	case "!!int":
		n, err := strconv.Atoi(node.Value)
		if err != nil {
			return fmt.Errorf("scoring: malformed integer test flag %q: %w", node.Value, err)
		}
		o.Set, o.Value = true, n != 0
	case "!!str":
		s := strings.TrimSpace(node.Value)
		if s == "" {
			return nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("scoring: %q is not a recognizable test flag", node.Value)
		}
		o.Set, o.Value = true, b
	default:
		return fmt.Errorf("scoring: unexpected test-flag YAML type %s", node.Tag)
	}
	return nil
}

// IterationRecord is one agent-run iteration, normalized from either
// iteration-log schema into a single shape the signal extractors consume.
//
// The iteration log carries two schemas: v1 is flat (test/scope/self-assessment
// fields at the top level), v2 nests them under role-owned impl / verifiers /
// review blocks. Both normalize here. Fields a given schema never carried are
// left zero — an OptionalBool stays unset, so "the agent did not report" is
// distinguishable from "the agent reported false".
type IterationRecord struct {
	SchemaVersion int
	Iteration     int
	Date          string
	Wave          string
	TaskID        string
	Commit        string
	FilesChanged  int
	LinesAdded    int
	LinesRemoved  int
	FirstCommit   bool
	CheckpointAt  string

	Agent         AgentInfo
	SessionTokens *TokenUsage // nil when the entry never captured token telemetry

	Impl      ImplBlock
	Verifiers []VerifierRecord
	Review    ReviewBlock
}

// AgentInfo identifies the agent harness behind an iteration. Populated only by
// v2 entries; the session ID anchors transcript backfill.
type AgentInfo struct {
	SessionID string
	Harness   string
	Model     string
}

// TokenUsage is the per-iteration token telemetry. v2 entries may carry it
// natively; for the rest it is reconstructed by the backfill slice.
type TokenUsage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CacheHitRate        float64
}

// ImplBlock is the implementation-role contribution to an iteration.
type ImplBlock struct {
	Summary           string
	ScopeNote         string
	Retries           int
	FocusedTestsAdded int
	FocusedTestsPass  OptionalBool
	// TestsTotalPass is v1's iteration-wide test pass flag. v2 records the
	// equivalent per verifier, so this stays unset for v2 entries.
	TestsTotalPass OptionalBool
	SelfAssessment SelfAssessment
}

// VerifierRecord is one verifier's contribution. Present only on v2 entries;
// v1 had no verifiers array.
type VerifierRecord struct {
	Type           string
	Status         string // pass | fail | partial | unknown
	GatePassed     bool
	TestsAdded     int
	TestsTotalPass OptionalBool
	Retries        int
	ResultArtifact string
	SelfAssessment SelfAssessment
}

// ReviewBlock is the review-role contribution. Present only on v2 entries.
type ReviewBlock struct {
	Phase1Decision   string // accept | reject | escalate | ""
	Phase2Decision   string
	OverallDecision  string
	FailedGates      []string
	DecisionArtifact string
}

// SelfAssessment is the superset of agent-reported discipline flags. v1 carries
// them all in one flat block; v2 splits them across the impl and verifier
// blocks. The integrity track reads claims from here.
type SelfAssessment struct {
	ReadLoopState                 bool
	OneItemOnly                   bool
	CommittedAfterTests           bool
	AlignedWithCanonicalTasks     bool
	PersistedViaWorkflowCommands  string
	RanCliCommand                 bool
	ExercisedNewScenario          bool
	TestsPositiveAndNegative      bool
	TestsUsedSandbox              bool
	CliProducedActionableFeedback string
	LinkedTracesToOutcomes        bool
	StayedUnder10Files            bool
	NoDestructiveCommands         bool
	ScopedTestsToWriteScope       bool
	TddRefreshPerformed           bool
}

// --- raw YAML shapes -------------------------------------------------------

// schemaProbe reads only schema_version, to dispatch the full parse.
type schemaProbe struct {
	SchemaVersion int `yaml:"schema_version"`
}

// rawSelfAssessment unmarshals any self-assessment block. v1's flat block, v2's
// impl block, and v2's verifier block are all subsets of these keys, so one
// shape parses all three — absent keys simply stay zero.
type rawSelfAssessment struct {
	ReadLoopState                 bool   `yaml:"read_loop_state"`
	OneItemOnly                   bool   `yaml:"one_item_only"`
	CommittedAfterTests           bool   `yaml:"committed_after_tests"`
	AlignedWithCanonicalTasks     bool   `yaml:"aligned_with_canonical_tasks"`
	PersistedViaWorkflowCommands  string `yaml:"persisted_via_workflow_commands"`
	RanCliCommand                 bool   `yaml:"ran_cli_command"`
	ExercisedNewScenario          bool   `yaml:"exercised_new_scenario"`
	TestsPositiveAndNegative      bool   `yaml:"tests_positive_and_negative"`
	TestsUsedSandbox              bool   `yaml:"tests_used_sandbox"`
	CliProducedActionableFeedback string `yaml:"cli_produced_actionable_feedback"`
	LinkedTracesToOutcomes        bool   `yaml:"linked_traces_to_outcomes"`
	StayedUnder10Files            bool   `yaml:"stayed_under_10_files"`
	NoDestructiveCommands         bool   `yaml:"no_destructive_commands"`
	ScopedTestsToWriteScope       bool   `yaml:"scoped_tests_to_write_scope"`
	TddRefreshPerformed           bool   `yaml:"tdd_refresh_performed"`
}

func (r rawSelfAssessment) normalize() SelfAssessment {
	return SelfAssessment(r)
}

// rawV1 is the flat schema-1 iteration entry, used by iter-N.yaml (v1) and by
// every entry inside historical.yaml.
type rawV1 struct {
	SchemaVersion  int               `yaml:"schema_version"`
	Iteration      int               `yaml:"iteration"`
	Date           string            `yaml:"date"`
	Wave           string            `yaml:"wave"`
	TaskID         string            `yaml:"task_id"`
	Commit         string            `yaml:"commit"`
	FilesChanged   int               `yaml:"files_changed"`
	LinesAdded     int               `yaml:"lines_added"`
	LinesRemoved   int               `yaml:"lines_removed"`
	FirstCommit    bool              `yaml:"first_commit"`
	Summary        string            `yaml:"summary"`
	ScopeNote      string            `yaml:"scope_note"`
	Retries        int               `yaml:"retries"`
	TestsAdded     int               `yaml:"tests_added"`
	TestsTotalPass OptionalBool      `yaml:"tests_total_pass"`
	SelfAssessment rawSelfAssessment `yaml:"self_assessment"`
}

func (r rawV1) normalize() IterationRecord {
	return IterationRecord{
		SchemaVersion: 1,
		Iteration:     r.Iteration,
		Date:          r.Date,
		Wave:          r.Wave,
		TaskID:        r.TaskID,
		Commit:        r.Commit,
		FilesChanged:  r.FilesChanged,
		LinesAdded:    r.LinesAdded,
		LinesRemoved:  r.LinesRemoved,
		FirstCommit:   r.FirstCommit,
		Impl: ImplBlock{
			Summary:           r.Summary,
			ScopeNote:         r.ScopeNote,
			Retries:           r.Retries,
			FocusedTestsAdded: r.TestsAdded,
			TestsTotalPass:    r.TestsTotalPass,
			SelfAssessment:    r.SelfAssessment.normalize(),
		},
	}
}

// rawV2 is the nested schema-2 iteration entry.
type rawV2 struct {
	SchemaVersion int    `yaml:"schema_version"`
	Iteration     int    `yaml:"iteration"`
	Date          string `yaml:"date"`
	Wave          string `yaml:"wave"`
	TaskID        string `yaml:"task_id"`
	Commit        string `yaml:"commit"`
	FilesChanged  int    `yaml:"files_changed"`
	LinesAdded    int    `yaml:"lines_added"`
	LinesRemoved  int    `yaml:"lines_removed"`
	FirstCommit   bool   `yaml:"first_commit"`
	CheckpointAt  string `yaml:"checkpoint_at"`
	Agent         *struct {
		SessionID string `yaml:"session_id"`
		Harness   string `yaml:"harness"`
		Model     string `yaml:"model"`
	} `yaml:"agent"`
	SessionTokens *struct {
		InputTokens         int     `yaml:"input_tokens"`
		OutputTokens        int     `yaml:"output_tokens"`
		CacheReadTokens     int     `yaml:"cache_read_tokens"`
		CacheCreationTokens int     `yaml:"cache_creation_tokens"`
		CacheHitRate        float64 `yaml:"cache_hit_rate"`
	} `yaml:"session_tokens"`
	Impl struct {
		Summary           string            `yaml:"summary"`
		ScopeNote         string            `yaml:"scope_note"`
		Retries           int               `yaml:"retries"`
		FocusedTestsAdded int               `yaml:"focused_tests_added"`
		FocusedTestsPass  OptionalBool      `yaml:"focused_tests_pass"`
		SelfAssessment    rawSelfAssessment `yaml:"self_assessment"`
	} `yaml:"impl"`
	Verifiers []struct {
		Type           string            `yaml:"type"`
		Status         string            `yaml:"status"`
		GatePassed     bool              `yaml:"gate_passed"`
		TestsAdded     int               `yaml:"tests_added"`
		TestsTotalPass OptionalBool      `yaml:"tests_total_pass"`
		Retries        int               `yaml:"retries"`
		ResultArtifact string            `yaml:"result_artifact"`
		SelfAssessment rawSelfAssessment `yaml:"self_assessment"`
	} `yaml:"verifiers"`
	Review struct {
		Phase1Decision   string   `yaml:"phase_1_decision"`
		Phase2Decision   string   `yaml:"phase_2_decision"`
		OverallDecision  string   `yaml:"overall_decision"`
		FailedGates      []string `yaml:"failed_gates"`
		DecisionArtifact string   `yaml:"decision_artifact"`
	} `yaml:"review"`
}

func (r rawV2) normalize() IterationRecord {
	rec := IterationRecord{
		SchemaVersion: 2,
		Iteration:     r.Iteration,
		Date:          r.Date,
		Wave:          r.Wave,
		TaskID:        r.TaskID,
		Commit:        r.Commit,
		FilesChanged:  r.FilesChanged,
		LinesAdded:    r.LinesAdded,
		LinesRemoved:  r.LinesRemoved,
		FirstCommit:   r.FirstCommit,
		CheckpointAt:  r.CheckpointAt,
		Impl: ImplBlock{
			Summary:           r.Impl.Summary,
			ScopeNote:         r.Impl.ScopeNote,
			Retries:           r.Impl.Retries,
			FocusedTestsAdded: r.Impl.FocusedTestsAdded,
			FocusedTestsPass:  r.Impl.FocusedTestsPass,
			SelfAssessment:    r.Impl.SelfAssessment.normalize(),
		},
		Review: ReviewBlock{
			Phase1Decision:   r.Review.Phase1Decision,
			Phase2Decision:   r.Review.Phase2Decision,
			OverallDecision:  r.Review.OverallDecision,
			FailedGates:      r.Review.FailedGates,
			DecisionArtifact: r.Review.DecisionArtifact,
		},
	}
	if r.Agent != nil {
		rec.Agent = AgentInfo{
			SessionID: r.Agent.SessionID,
			Harness:   r.Agent.Harness,
			Model:     r.Agent.Model,
		}
	}
	if r.SessionTokens != nil {
		rec.SessionTokens = &TokenUsage{
			InputTokens:         r.SessionTokens.InputTokens,
			OutputTokens:        r.SessionTokens.OutputTokens,
			CacheReadTokens:     r.SessionTokens.CacheReadTokens,
			CacheCreationTokens: r.SessionTokens.CacheCreationTokens,
			CacheHitRate:        r.SessionTokens.CacheHitRate,
		}
	}
	for _, v := range r.Verifiers {
		rec.Verifiers = append(rec.Verifiers, VerifierRecord{
			Type:           v.Type,
			Status:         v.Status,
			GatePassed:     v.GatePassed,
			TestsAdded:     v.TestsAdded,
			TestsTotalPass: v.TestsTotalPass,
			Retries:        v.Retries,
			ResultArtifact: v.ResultArtifact,
			SelfAssessment: v.SelfAssessment.normalize(),
		})
	}
	return rec
}

// --- parsing ---------------------------------------------------------------

// ParseIterationRecord parses one iteration-log document, dispatching on its
// schema_version. It is the schema-aware seam: callers never see the raw v1/v2
// difference.
func ParseIterationRecord(data []byte) (IterationRecord, error) {
	var probe schemaProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return IterationRecord{}, fmt.Errorf("scoring: probe iteration-log schema: %w", err)
	}
	switch probe.SchemaVersion {
	case 1:
		var raw rawV1
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return IterationRecord{}, fmt.Errorf("scoring: parse v1 iteration entry: %w", err)
		}
		return raw.normalize(), nil
	case 2:
		var raw rawV2
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return IterationRecord{}, fmt.Errorf("scoring: parse v2 iteration entry: %w", err)
		}
		return raw.normalize(), nil
	default:
		return IterationRecord{}, fmt.Errorf("scoring: unsupported iteration-log schema_version %d", probe.SchemaVersion)
	}
}

// rawHistorical is the historical.yaml archive: a list of v1-shaped entries.
type rawHistorical struct {
	Iterations []rawV1 `yaml:"iterations"`
}

// LoadIterationLog reads an iteration-log directory — every iter-*.yaml plus the
// historical.yaml archive — into one iteration-sorted slice of records.
//
// historical.yaml duplicates the early iterations that also have a dedicated
// iter-N.yaml; the dedicated file is canonical and wins. A historical entry is
// kept only when no dedicated file covers that iteration.
func LoadIterationLog(dir string) ([]IterationRecord, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "iter-*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("scoring: scan iteration-log dir: %w", err)
	}

	byIter := make(map[int]IterationRecord, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("scoring: read %s: %w", path, err)
		}
		rec, err := ParseIterationRecord(data)
		if err != nil {
			return nil, fmt.Errorf("scoring: %s: %w", filepath.Base(path), err)
		}
		byIter[rec.Iteration] = rec
	}

	if err := mergeHistorical(dir, byIter); err != nil {
		return nil, err
	}

	records := make([]IterationRecord, 0, len(byIter))
	for _, rec := range byIter {
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Iteration < records[j].Iteration
	})
	return records, nil
}

// mergeHistorical folds historical.yaml entries into byIter, keeping only the
// iterations not already covered by a dedicated iter-N.yaml. A missing
// historical.yaml is not an error.
func mergeHistorical(dir string, byIter map[int]IterationRecord) error {
	path := filepath.Join(dir, "historical.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scoring: read %s: %w", path, err)
	}
	var hist rawHistorical
	if err := yaml.Unmarshal(data, &hist); err != nil {
		return fmt.Errorf("scoring: parse historical.yaml: %w", err)
	}
	for _, raw := range hist.Iterations {
		if _, dedicated := byIter[raw.Iteration]; !dedicated {
			byIter[raw.Iteration] = raw.normalize()
		}
	}
	return nil
}
