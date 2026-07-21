package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

type iterLogAgentBlock struct {
	SessionID      string `yaml:"session_id,omitempty" json:"session_id,omitempty"`
	Harness        string `yaml:"harness,omitempty" json:"harness,omitempty"`
	HarnessVersion string `yaml:"harness_version,omitempty" json:"harness_version,omitempty"`
	Model          string `yaml:"model,omitempty" json:"model,omitempty"`
	Entrypoint     string `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
}

type iterLogSessionTokensBlock struct {
	InputTokens         int     `yaml:"input_tokens" json:"input_tokens"`
	OutputTokens        int     `yaml:"output_tokens" json:"output_tokens"`
	CacheReadTokens     int     `yaml:"cache_read_tokens" json:"cache_read_tokens"`
	CacheCreationTokens int     `yaml:"cache_creation_tokens" json:"cache_creation_tokens"`
	ReasoningTokens     int     `yaml:"reasoning_tokens,omitempty" json:"reasoning_tokens,omitempty"`
	CacheHitRate        float64 `yaml:"cache_hit_rate,omitempty" json:"cache_hit_rate,omitempty"`
	MessageCount        int     `yaml:"message_count" json:"message_count"`
}

type iterLogEntry struct {
	SchemaVersion int                        `yaml:"schema_version" json:"schema_version"`
	Iteration     int                        `yaml:"iteration" json:"iteration"`
	Date          string                     `yaml:"date" json:"date"`
	Wave          string                     `yaml:"wave" json:"wave"`
	TaskID        string                     `yaml:"task_id" json:"task_id"`
	Commit        string                     `yaml:"commit" json:"commit"`
	FilesChanged  int                        `yaml:"files_changed" json:"files_changed"`
	LinesAdded    int                        `yaml:"lines_added" json:"lines_added"`
	LinesRemoved  int                        `yaml:"lines_removed" json:"lines_removed"`
	FirstCommit   bool                       `yaml:"first_commit,omitempty" json:"first_commit,omitempty"`
	Agent         *iterLogAgentBlock         `yaml:"agent,omitempty" json:"agent,omitempty"`
	CheckpointAt  string                     `yaml:"checkpoint_at,omitempty" json:"checkpoint_at,omitempty"`
	SessionTokens *iterLogSessionTokensBlock `yaml:"session_tokens,omitempty" json:"session_tokens,omitempty"`
	Impl          iterLogImplBlock           `yaml:"impl" json:"impl"`
	Verifiers     []iterLogVerifierEntry     `yaml:"verifiers" json:"verifiers"`
	Review        iterLogReviewBlock         `yaml:"review" json:"review"`
}

type iterLogImplBlock struct {
	Item              string                    `yaml:"item" json:"item"`
	Summary           string                    `yaml:"summary" json:"summary"`
	ScopeNote         string                    `yaml:"scope_note" json:"scope_note"`
	FeedbackGoal      string                    `yaml:"feedback_goal" json:"feedback_goal"`
	Retries           int                       `yaml:"retries" json:"retries"`
	FocusedTestsAdded int                       `yaml:"focused_tests_added" json:"focused_tests_added"`
	FocusedTestsPass  interface{}               `yaml:"focused_tests_pass,omitempty" json:"focused_tests_pass,omitempty"`
	SelfAssessment    iterLogImplSelfAssessment `yaml:"self_assessment,omitempty" json:"self_assessment,omitempty"`
}

type iterLogImplSelfAssessment struct {
	ReadLoopState                bool   `yaml:"read_loop_state" json:"read_loop_state"`
	OneItemOnly                  bool   `yaml:"one_item_only" json:"one_item_only"`
	CommittedAfterTests          bool   `yaml:"committed_after_tests" json:"committed_after_tests"`
	AlignedWithCanonicalTasks    bool   `yaml:"aligned_with_canonical_tasks" json:"aligned_with_canonical_tasks"`
	PersistedViaWorkflowCommands string `yaml:"persisted_via_workflow_commands" json:"persisted_via_workflow_commands"`
	StayedUnder10Files           bool   `yaml:"stayed_under_10_files" json:"stayed_under_10_files"`
	NoDestructiveCommands        bool   `yaml:"no_destructive_commands" json:"no_destructive_commands"`
	ScopedTestsToWriteScope      bool   `yaml:"scoped_tests_to_write_scope" json:"scoped_tests_to_write_scope"`
	TddRefreshPerformed          bool   `yaml:"tdd_refresh_performed" json:"tdd_refresh_performed"`
}

type iterLogVerifierEntry struct {
	Type             string                        `yaml:"type" json:"type"`
	Status           string                        `yaml:"status" json:"status"`
	GatePassed       bool                          `yaml:"gate_passed" json:"gate_passed"`
	TestsAdded       int                           `yaml:"tests_added" json:"tests_added"`
	TestsAddedByKind []iterLogTestAddedByKind      `yaml:"tests_added_by_kind,omitempty" json:"tests_added_by_kind,omitempty"`
	LinkedTraces     []iterLogLinkedTrace          `yaml:"linked_traces,omitempty" json:"linked_traces,omitempty"`
	TestsTotalPass   interface{}                   `yaml:"tests_total_pass,omitempty" json:"tests_total_pass,omitempty"`
	ScenarioTags     []string                      `yaml:"scenario_tags,omitempty" json:"scenario_tags,omitempty"`
	Retries          int                           `yaml:"retries" json:"retries"`
	ResultArtifact   string                        `yaml:"result_artifact" json:"result_artifact"`
	SelfAssessment   iterLogVerifierSelfAssessment `yaml:"self_assessment,omitempty" json:"self_assessment,omitempty"`
}

// iterLogTestAddedByKind is one entry of verifier.tests_added_by_kind — the
// structured replacement for the deprecated tests_positive_and_negative
// boolean. Naming the test makes the claim verifiable against the diff.
type iterLogTestAddedByKind struct {
	Name string `yaml:"name" json:"name"`
	// Kind matches the schema enum: positive | negative | edge | regression.
	Kind string `yaml:"kind" json:"kind"`
}

// iterLogLinkedTrace is one entry of verifier.linked_traces — the structured
// replacement for the deprecated linked_traces_to_outcomes boolean. Each pair
// names a concrete verification trace and the outcome it links to (commit
// SHA, iteration, review-decision path, etc.).
type iterLogLinkedTrace struct {
	TraceRef   string `yaml:"trace_ref" json:"trace_ref"`
	OutcomeRef string `yaml:"outcome_ref" json:"outcome_ref"`
}

type iterLogVerifierSelfAssessment struct {
	TestsPositiveAndNegative      bool   `yaml:"tests_positive_and_negative" json:"tests_positive_and_negative"`
	TestsUsedSandbox              bool   `yaml:"tests_used_sandbox" json:"tests_used_sandbox"`
	ExercisedNewScenario          bool   `yaml:"exercised_new_scenario" json:"exercised_new_scenario"`
	RanCliCommand                 bool   `yaml:"ran_cli_command" json:"ran_cli_command"`
	CliProducedActionableFeedback string `yaml:"cli_produced_actionable_feedback" json:"cli_produced_actionable_feedback"`
	LinkedTracesToOutcomes        bool   `yaml:"linked_traces_to_outcomes" json:"linked_traces_to_outcomes"`
}

type iterLogReviewBlock struct {
	Phase1Decision       string   `yaml:"phase_1_decision" json:"phase_1_decision"`
	Phase2Decision       string   `yaml:"phase_2_decision" json:"phase_2_decision"`
	OverallDecision      string   `yaml:"overall_decision" json:"overall_decision"`
	FailedGates          []string `yaml:"failed_gates,omitempty" json:"failed_gates,omitempty"`
	EscalationReason     string   `yaml:"escalation_reason" json:"escalation_reason"`
	ReviewerNotes        string   `yaml:"reviewer_notes" json:"reviewer_notes"`
	DecisionArtifact     string   `yaml:"decision_artifact" json:"decision_artifact"`
	VerifyRecordAppended bool     `yaml:"verify_record_appended" json:"verify_record_appended"`
}

type iterLogV1Legacy struct {
	SchemaVersion  int                     `yaml:"schema_version"`
	Iteration      int                     `yaml:"iteration"`
	Date           string                  `yaml:"date"`
	Wave           string                  `yaml:"wave"`
	TaskID         string                  `yaml:"task_id"`
	Commit         string                  `yaml:"commit"`
	FilesChanged   int                     `yaml:"files_changed"`
	LinesAdded     int                     `yaml:"lines_added"`
	LinesRemoved   int                     `yaml:"lines_removed"`
	FirstCommit    bool                    `yaml:"first_commit"`
	Item           string                  `yaml:"item"`
	ScenarioTags   []string                `yaml:"scenario_tags"`
	FeedbackGoal   string                  `yaml:"feedback_goal"`
	TestsAdded     int                     `yaml:"tests_added"`
	TestsTotalPass interface{}             `yaml:"tests_total_pass"`
	Retries        int                     `yaml:"retries"`
	ScopeNote      string                  `yaml:"scope_note"`
	Summary        string                  `yaml:"summary"`
	SelfAssessment iterLogV1SelfAssessment `yaml:"self_assessment"`
}

type iterLogV1SelfAssessment struct {
	ReadLoopState                 bool   `yaml:"read_loop_state"`
	OneItemOnly                   bool   `yaml:"one_item_only"`
	CommittedAfterTests           bool   `yaml:"committed_after_tests"`
	TestsPositiveAndNegative      bool   `yaml:"tests_positive_and_negative"`
	TestsUsedSandbox              bool   `yaml:"tests_used_sandbox"`
	AlignedWithCanonicalTasks     bool   `yaml:"aligned_with_canonical_tasks"`
	PersistedViaWorkflowCommands  string `yaml:"persisted_via_workflow_commands"`
	RanCliCommand                 bool   `yaml:"ran_cli_command"`
	ExercisedNewScenario          bool   `yaml:"exercised_new_scenario"`
	CliProducedActionableFeedback string `yaml:"cli_produced_actionable_feedback"`
	LinkedTracesToOutcomes        bool   `yaml:"linked_traces_to_outcomes"`
	StayedUnder10Files            bool   `yaml:"stayed_under_10_files"`
	NoDestructiveCommands         bool   `yaml:"no_destructive_commands"`
}

type iterLogDiffStat struct {
	FilesChanged int
	LinesAdded   int
	LinesRemoved int
	FirstCommit  bool
}

func gitIterDiffStat(projectPath string) iterLogDiffStat {
	cmd := execabs.Command("git", "-C", projectPath, "rev-parse", "HEAD~1")
	if err := cmd.Run(); err != nil {
		return iterLogDiffStat{FirstCommit: true}
	}

	out := strings.TrimSpace(gitOutput(projectPath, "diff", "--stat", "HEAD~1"))
	if out == "" {
		return iterLogDiffStat{}
	}

	lines := strings.Split(out, "\n")
	summary := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			summary = strings.TrimSpace(lines[i])
			break
		}
	}
	if summary == "" {
		return iterLogDiffStat{}
	}
	return parseGitDiffStatSummary(summary)
}

func parseGitDiffStatSummary(summary string) iterLogDiffStat {
	var result iterLogDiffStat
	if idx := strings.Index(summary, " file"); idx != -1 {
		fmt.Sscanf(strings.TrimSpace(summary[:idx]), "%d", &result.FilesChanged)
	}
	if idx := strings.Index(summary, " insertion"); idx != -1 {
		start := idx
		for start > 0 && summary[start-1] != ',' {
			start--
		}
		fmt.Sscanf(strings.TrimSpace(summary[start:idx]), "%d", &result.LinesAdded)
	}
	if idx := strings.Index(summary, " deletion"); idx != -1 {
		start := idx
		for start > 0 && summary[start-1] != ',' {
			start--
		}
		fmt.Sscanf(strings.TrimSpace(summary[start:idx]), "%d", &result.LinesRemoved)
	}
	return result
}

func firstReadableDelegationContract(projectPath string) *DelegationContract {
	dir := delegationDir(projectPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		contract, err := loadDelegationContract(projectPath, strings.TrimSuffix(e.Name(), ".yaml"))
		if err != nil {
			continue
		}
		if contract.Status != "pending" && contract.Status != "active" {
			continue
		}
		return contract
	}
	return nil
}

func scanActiveDelegationContract(projectPath string) (wave, taskID string) {
	c := firstReadableDelegationContract(projectPath)
	if c == nil {
		return "", ""
	}
	return c.ParentPlanID, c.ParentTaskID
}

func feedbackGoalFromDelegationBundle(projectPath string, c *DelegationContract) string {
	if c == nil || strings.TrimSpace(c.ID) == "" {
		return ""
	}
	p := filepath.Join(delegationBundlesDir(projectPath), c.ID+".yaml")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var b delegationBundleYAML
	if err := yaml.Unmarshal(data, &b); err != nil {
		return ""
	}
	return strings.TrimSpace(b.Verification.FeedbackGoal)
}

func iterLogReviewDecisionPath(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	return ".agents/active/verification/" + taskID + "/review-decision.yaml"
}

func zeroIterLogReviewBlock(taskID string) iterLogReviewBlock {
	return iterLogReviewBlock{
		DecisionArtifact: iterLogReviewDecisionPath(taskID),
	}
}

func emptyIterLogImplBlock(feedbackGoal string) iterLogImplBlock {
	return iterLogImplBlock{
		FeedbackGoal: strings.TrimSpace(feedbackGoal),
	}
}

func migrateIterLogV1Legacy(v1 *iterLogV1Legacy) iterLogEntry {
	e := iterLogEntry{
		SchemaVersion: 2,
		Iteration:     v1.Iteration,
		Date:          v1.Date,
		Wave:          v1.Wave,
		TaskID:        v1.TaskID,
		Commit:        v1.Commit,
		FilesChanged:  v1.FilesChanged,
		LinesAdded:    v1.LinesAdded,
		LinesRemoved:  v1.LinesRemoved,
		FirstCommit:   v1.FirstCommit,
		Impl: iterLogImplBlock{
			Item:              v1.Item,
			Summary:           v1.Summary,
			ScopeNote:         v1.ScopeNote,
			FeedbackGoal:      v1.FeedbackGoal,
			Retries:           v1.Retries,
			FocusedTestsAdded: v1.TestsAdded,
			FocusedTestsPass:  v1.TestsTotalPass,
			SelfAssessment: iterLogImplSelfAssessment{
				ReadLoopState:                v1.SelfAssessment.ReadLoopState,
				OneItemOnly:                  v1.SelfAssessment.OneItemOnly,
				CommittedAfterTests:          v1.SelfAssessment.CommittedAfterTests,
				AlignedWithCanonicalTasks:    v1.SelfAssessment.AlignedWithCanonicalTasks,
				PersistedViaWorkflowCommands: v1.SelfAssessment.PersistedViaWorkflowCommands,
				StayedUnder10Files:           v1.SelfAssessment.StayedUnder10Files,
				NoDestructiveCommands:        v1.SelfAssessment.NoDestructiveCommands,
			},
		},
		Verifiers: nil,
		Review:    zeroIterLogReviewBlock(v1.TaskID),
	}
	if e.Verifiers == nil {
		e.Verifiers = []iterLogVerifierEntry{}
	}
	return e
}

func loadIterLogDocument(data []byte) (*iterLogEntry, error) {
	// Decode once into the v1 legacy shape. It carries schema_version, so a v1
	// document is fully materialized by this single pass (no re-decode). A v2
	// document ignores the v1-only fields here and is decoded natively below.
	var v1 iterLogV1Legacy
	if err := yaml.Unmarshal(data, &v1); err != nil {
		return nil, fmt.Errorf("parse iteration log: %w", err)
	}
	if v1.SchemaVersion == 1 {
		out := migrateIterLogV1Legacy(&v1)
		return &out, nil
	}
	var e iterLogEntry
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("parse iteration log v2: %w", err)
	}
	if e.Verifiers == nil {
		e.Verifiers = []iterLogVerifierEntry{}
	}
	return &e, nil
}

func validateIterLogRoleFlags(role, verifierType string) error {
	role = strings.TrimSpace(strings.ToLower(role))
	verifierType = strings.TrimSpace(verifierType)
	switch role {
	case "", "impl", "verifier", "review":
	default:
		return fmt.Errorf("invalid --role %q (expected impl, verifier, review, or omit)", role)
	}
	if verifierType != "" && role != "verifier" {
		return fmt.Errorf("--verifier-type is only valid with --role verifier")
	}
	if role == "verifier" && verifierType == "" {
		return fmt.Errorf("--role verifier requires --verifier-type")
	}
	return nil
}

func mergeIterLogTopLevelGit(dst *iterLogEntry, n int, wave, taskID, commit, projectPath string, diff iterLogDiffStat) {
	dst.SchemaVersion = 2
	dst.Iteration = n
	dst.Date = time.Now().UTC().Format("2006-01-02")
	dst.Wave = wave
	dst.TaskID = taskID
	dst.Commit = commit
	dst.FilesChanged = diff.FilesChanged
	dst.LinesAdded = diff.LinesAdded
	dst.LinesRemoved = diff.LinesRemoved
	dst.FirstCommit = diff.FirstCommit
	dst.CheckpointAt = time.Now().UTC().Format(time.RFC3339)
	if a := resolveAgentBlock(projectPath); a != nil {
		dst.Agent = a
	}
}

// findReaderByHarness returns the SessionReader whose AIAgentPrefix matches harness.
func findReaderByHarness(harness string) platform.SessionReader {
	for _, p := range platform.All() {
		if sr, ok := p.(platform.SessionReader); ok && sr.AIAgentPrefix() == harness {
			return sr
		}
	}
	return nil
}

// probeSessionReader scans all platform SessionReaders for active session env vars.
// Returns the first reader that has a session ID set.
func probeSessionReader() (platform.SessionReader, string, string) {
	for _, p := range platform.All() {
		sr, ok := p.(platform.SessionReader)
		if !ok {
			continue
		}
		if id := firstEnv(sr.SessionEnvs()); id != "" {
			return sr, sr.AIAgentPrefix(), id
		}
	}
	return nil, "", ""
}

// resolveAgentBlock iterates platform.All(), type-asserts to platform.SessionReader,
// and produces an agent identity record. Returns nil when no platform signals are detectable.
// Platform session env vars and model resolvers are maintained in internal/platform/;
// no changes to this function are needed when adding a new platform.
func resolveAgentBlock(projectPath string) *iterLogAgentBlock {
	home, _ := config.UserHomeDir()

	// AI_AGENT=<harness>_<version>_agent is the cross-platform harness identifier.
	// When present it pins the platform and version without probing session env vars.
	aiAgent := os.Getenv("AI_AGENT")
	harness, harnessVersion := "", ""
	var reader platform.SessionReader
	if aiAgent != "" {
		harness, harnessVersion = parseAIAgentEnv(aiAgent)
		reader = findReaderByHarness(harness)
	}

	// Fallback: probe each SessionReader's env vars to detect the active platform.
	sessionID, entrypoint := "", ""
	if reader == nil {
		reader, harness, sessionID = probeSessionReader()
	} else {
		sessionID = firstEnv(reader.SessionEnvs())
	}

	if reader != nil {
		entrypoint = firstEnv(reader.EntrypointEnvs())
	}

	if harness == "" && sessionID == "" && entrypoint == "" {
		return nil
	}

	block := &iterLogAgentBlock{
		SessionID:      sessionID,
		Harness:        harness,
		HarnessVersion: harnessVersion,
		Entrypoint:     entrypoint,
	}
	if reader != nil && sessionID != "" && home != "" {
		block.Model = reader.ResolveModel(home, projectPath, sessionID)
	}
	return block
}

func firstEnv(names []string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// parseAIAgentEnv parses AI_AGENT=<harness>_<version-dashes>_agent into
// harness and semver version strings (e.g. "2-1-138" → "2.1.138").
func parseAIAgentEnv(raw string) (harness, version string) {
	s := strings.TrimSuffix(raw, "_agent")
	idx := strings.LastIndex(s, "_")
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.ReplaceAll(s[idx+1:], "-", ".")
}

func mergeImplIterLog(dst *iterLogEntry, contract *DelegationContract, projectPath string) {
	fg := feedbackGoalFromDelegationBundle(projectPath, contract)
	dst.Impl.FeedbackGoal = fg
}

func upsertVerifierIterLog(dst *iterLogEntry, projectPath, taskID, verifierType string) error {
	verifierType = strings.TrimSpace(verifierType)
	relPath, err := verificationResultFilePath(projectPath, taskID, verifierType)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(relPath)
	if err != nil {
		return fmt.Errorf("read verifier result %s: %w", relPath, err)
	}
	var doc VerificationResultDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse verifier result %s: %w", relPath, err)
	}
	if err := validateVerificationResultDoc(&doc); err != nil {
		return fmt.Errorf("verifier result %s invalid: %w", relPath, err)
	}
	artifact, err := filepath.Rel(projectPath, relPath)
	if err != nil {
		return fmt.Errorf("rel path for verifier artifact: %w", err)
	}
	artifact = filepath.ToSlash(artifact)
	entry := iterLogVerifierEntry{
		Type:           verifierType,
		Status:         doc.Status,
		GatePassed:     doc.Status == "pass",
		ResultArtifact: artifact,
	}
	replaced := false
	for i := range dst.Verifiers {
		if dst.Verifiers[i].Type == verifierType {
			prev := dst.Verifiers[i]
			entry.TestsAdded = prev.TestsAdded
			entry.TestsTotalPass = prev.TestsTotalPass
			entry.ScenarioTags = prev.ScenarioTags
			entry.Retries = prev.Retries
			entry.SelfAssessment = prev.SelfAssessment
			dst.Verifiers[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		dst.Verifiers = append(dst.Verifiers, entry)
	}
	return nil
}

type reviewDecisionLoose struct {
	Phase1Decision   string   `yaml:"phase_1_decision"`
	Phase2Decision   string   `yaml:"phase_2_decision"`
	OverallDecision  string   `yaml:"overall_decision"`
	FailedGates      []string `yaml:"failed_gates"`
	EscalationReason string   `yaml:"escalation_reason"`
	ReviewerNotes    string   `yaml:"reviewer_notes"`
}

func mergeReviewIterLog(dst *iterLogEntry, projectPath, taskID string) error {
	taskID = strings.TrimSpace(taskID)
	rel := iterLogReviewDecisionPath(taskID)
	if rel == "" {
		dst.Review = zeroIterLogReviewBlock(taskID)
		return nil
	}
	dst.Review.DecisionArtifact = rel
	full := filepath.Join(projectPath, filepath.FromSlash(rel))
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read review decision %s: %w", rel, err)
	}
	var doc reviewDecisionLoose
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse review decision %s: %w", rel, err)
	}
	dst.Review.Phase1Decision = doc.Phase1Decision
	dst.Review.Phase2Decision = doc.Phase2Decision
	dst.Review.OverallDecision = doc.OverallDecision
	if len(doc.FailedGates) > 0 {
		dst.Review.FailedGates = append([]string(nil), doc.FailedGates...)
	}
	dst.Review.EscalationReason = doc.EscalationReason
	dst.Review.ReviewerNotes = doc.ReviewerNotes
	dst.Review.VerifyRecordAppended = true
	return nil
}

func loadOrInitIterLogEntry(iterPath, feedbackGoal, taskID string) (*iterLogEntry, error) {
	data, err := os.ReadFile(iterPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &iterLogEntry{
				SchemaVersion: 2,
				Impl:          emptyIterLogImplBlock(feedbackGoal),
				Verifiers:     []iterLogVerifierEntry{},
				Review:        zeroIterLogReviewBlock(taskID),
			}, nil
		}
		return nil, fmt.Errorf("read existing iteration log: %w", err)
	}
	entry, err := loadIterLogDocument(data)
	if err != nil {
		return nil, err
	}
	if entry.SchemaVersion != 2 {
		return nil, fmt.Errorf("iteration log %s has unsupported schema_version %d (expected 2 after migration)", config.DisplayPath(iterPath), entry.SchemaVersion)
	}
	return entry, nil
}

func applyIterLogRole(entry *iterLogEntry, role, verifierType, feedbackGoal, projectPath, taskID string, contract *DelegationContract) error {
	switch role {
	case "":
		entry.Impl.FeedbackGoal = feedbackGoal
	case "impl":
		mergeImplIterLog(entry, contract, projectPath)
	case "verifier":
		return upsertVerifierIterLog(entry, projectPath, taskID, verifierType)
	case "review":
		return mergeReviewIterLog(entry, projectPath, taskID)
	}
	return nil
}

func writeIterLogEntry(iterPath string, entry *iterLogEntry) error {
	if err := validateWorkflowIterLogEntry(entry); err != nil {
		return err
	}
	body, err := yamlMarshal(entry)
	if err != nil {
		return fmt.Errorf("marshal iter log: %w", err)
	}
	const header = "# yaml-language-server: $schema=../../../../schemas/workflow-iter-log.schema.json\n"
	content := []byte(header + string(body))
	if err := osWriteFile(iterPath, content, 0644); err != nil {
		return fmt.Errorf("write iter log: %w", err)
	}
	return nil
}

// loadPrevCheckpointAt reads checkpoint_at from iter-(n-1).yaml for token
// time-windowing. Returns "" when n==1 or the previous file is absent.
func loadPrevCheckpointAt(iterDir string, n int) string {
	if n <= 1 {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(iterDir, fmt.Sprintf("iter-%d.yaml", n-1)))
	if err != nil {
		return ""
	}
	var e struct {
		CheckpointAt string `yaml:"checkpoint_at"`
	}
	if err := yaml.Unmarshal(data, &e); err != nil {
		return ""
	}
	return e.CheckpointAt
}

// populateIterLogSessionTokens scans the active session's JSONL and sets
// entry.SessionTokens when token metrics are available. Scanner errors are
// swallowed; on any failure the SessionTokens block is left nil.
func populateIterLogSessionTokens(entry *iterLogEntry, projectPath, iterDir string, n int) {
	if entry.Agent == nil || entry.Agent.SessionID == "" || entry.Agent.Harness == "" {
		return
	}
	prevAt := loadPrevCheckpointAt(iterDir, n)
	home, _ := config.UserHomeDir()
	for _, p := range platform.All() {
		sr, isReader := p.(platform.SessionReader)
		scanner, isScanner := p.(platform.SessionTokenScanner)
		if !isReader || !isScanner || sr.AIAgentPrefix() != entry.Agent.Harness {
			continue
		}
		metrics := scanner.ScanSessionTokens(home, projectPath, entry.Agent.SessionID, prevAt)
		if metrics.MessageCount > 0 {
			entry.SessionTokens = &iterLogSessionTokensBlock{
				InputTokens:         metrics.InputTokens,
				OutputTokens:        metrics.OutputTokens,
				CacheReadTokens:     metrics.CacheReadTokens,
				CacheCreationTokens: metrics.CacheCreationTokens,
				ReasoningTokens:     metrics.ReasoningTokens,
				CacheHitRate:        metrics.CacheHitRate,
				MessageCount:        metrics.MessageCount,
			}
		}
		break
	}
}

func runWorkflowCheckpointLogToIter(n int, role, verifierType string) error {
	if err := validateIterLogRoleFlags(role, verifierType); err != nil {
		return err
	}
	role = strings.TrimSpace(strings.ToLower(role))
	verifierType = strings.TrimSpace(verifierType)

	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	contract := firstReadableDelegationContract(project.Path)
	wave, taskID := "", ""
	if contract != nil {
		wave, taskID = contract.ParentPlanID, contract.ParentTaskID
	}

	commit := strings.TrimSpace(gitOutput(project.Path, "log", "-1", "--format=%H"))
	diff := gitIterDiffStat(project.Path)
	feedbackGoal := feedbackGoalFromDelegationBundle(project.Path, contract)

	iterDir := filepath.Join(project.Path, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterDir, 0755); err != nil {
		return fmt.Errorf("create iteration-log dir: %w", err)
	}
	iterPath := iterRecordPath(project.Path, n)

	entry, err := loadOrInitIterLogEntry(iterPath, feedbackGoal, taskID)
	if err != nil {
		return err
	}

	mergeIterLogTopLevelGit(entry, n, wave, taskID, commit, project.Path, diff)

	// Populate session_tokens by scanning the active session's JSONL.
	populateIterLogSessionTokens(entry, project.Path, iterDir, n)

	if err := applyIterLogRole(entry, role, verifierType, feedbackGoal, project.Path, taskID, contract); err != nil {
		return err
	}

	if err := writeIterLogEntry(iterPath, entry); err != nil {
		return err
	}

	// Register this iteration in the append-on-close index (fast-path for
	// per-plan traversal + archival enabler). Upsert-by-N because a checkpoint
	// re-runs for the same iteration across roles; the index is purely additive
	// so a write failure never fails the checkpoint (best-effort — max-scan
	// stays the authoritative source of next-N).
	_ = upsertIterationIndexEntry(project.Path, iterIndexEntry{
		N:         n,
		PlanID:    wave,
		TaskID:    taskID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	publishCheckpointBestEffort(project.Path, n)

	fmt.Fprintf(os.Stdout, "%s\n", config.DisplayPath(iterPath))
	return nil
}
