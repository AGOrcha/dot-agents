package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"go.yaml.in/yaml/v3"
)

type iterLogEntry struct {
	SchemaVersion int                    `yaml:"schema_version" json:"schema_version"`
	Iteration     int                    `yaml:"iteration" json:"iteration"`
	Date          string                 `yaml:"date" json:"date"`
	Wave          string                 `yaml:"wave" json:"wave"`
	TaskID        string                 `yaml:"task_id" json:"task_id"`
	Commit        string                 `yaml:"commit" json:"commit"`
	FilesChanged  int                    `yaml:"files_changed" json:"files_changed"`
	LinesAdded    int                    `yaml:"lines_added" json:"lines_added"`
	LinesRemoved  int                    `yaml:"lines_removed" json:"lines_removed"`
	FirstCommit   bool                   `yaml:"first_commit,omitempty" json:"first_commit,omitempty"`
	Impl          iterLogImplBlock       `yaml:"impl" json:"impl"`
	Verifiers     []iterLogVerifierEntry `yaml:"verifiers" json:"verifiers"`
	Review        iterLogReviewBlock     `yaml:"review" json:"review"`
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
	Type           string                        `yaml:"type" json:"type"`
	Status         string                        `yaml:"status" json:"status"`
	GatePassed     bool                          `yaml:"gate_passed" json:"gate_passed"`
	TestsAdded     int                           `yaml:"tests_added" json:"tests_added"`
	TestsTotalPass interface{}                   `yaml:"tests_total_pass,omitempty" json:"tests_total_pass,omitempty"`
	ScenarioTags   []string                      `yaml:"scenario_tags,omitempty" json:"scenario_tags,omitempty"`
	Retries        int                           `yaml:"retries" json:"retries"`
	ResultArtifact string                        `yaml:"result_artifact" json:"result_artifact"`
	SelfAssessment iterLogVerifierSelfAssessment `yaml:"self_assessment,omitempty" json:"self_assessment,omitempty"`
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
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD~1")
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
	var probe struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse iteration log: %w", err)
	}
	if probe.SchemaVersion == 1 {
		var v1 iterLogV1Legacy
		if err := yaml.Unmarshal(data, &v1); err != nil {
			return nil, fmt.Errorf("parse iteration log v1: %w", err)
		}
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

func mergeIterLogTopLevelGit(dst *iterLogEntry, n int, wave, taskID, commit string, diff iterLogDiffStat) {
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
	return nil
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
	iterPath := filepath.Join(iterDir, fmt.Sprintf("iter-%d.yaml", n))

	var entry *iterLogEntry
	if data, err := os.ReadFile(iterPath); err == nil {
		entry, err = loadIterLogDocument(data)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing iteration log: %w", err)
	}

	if entry == nil {
		entry = &iterLogEntry{
			SchemaVersion: 2,
			Impl:          emptyIterLogImplBlock(feedbackGoal),
			Verifiers:     []iterLogVerifierEntry{},
			Review:        zeroIterLogReviewBlock(taskID),
		}
	} else if entry.SchemaVersion != 2 {
		return fmt.Errorf("iteration log %s has unsupported schema_version %d (expected 2 after migration)", config.DisplayPath(iterPath), entry.SchemaVersion)
	}

	mergeIterLogTopLevelGit(entry, n, wave, taskID, commit, diff)

	switch role {
	case "":
		entry.Impl.FeedbackGoal = feedbackGoal
	case "impl":
		mergeImplIterLog(entry, contract, project.Path)
	case "verifier":
		if err := upsertVerifierIterLog(entry, project.Path, taskID, verifierType); err != nil {
			return err
		}
	case "review":
		if err := mergeReviewIterLog(entry, project.Path, taskID); err != nil {
			return err
		}
	}

	if err := validateWorkflowIterLogEntry(entry); err != nil {
		return err
	}

	body, err := yaml.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal iter log: %w", err)
	}

	const header = "# yaml-language-server: $schema=../../../../schemas/workflow-iter-log.schema.json\n"
	content := []byte(header + string(body))

	if err := os.WriteFile(iterPath, content, 0644); err != nil {
		return fmt.Errorf("write iter log: %w", err)
	}

	fmt.Fprintf(os.Stdout, "%s\n", config.DisplayPath(iterPath))
	return nil
}
