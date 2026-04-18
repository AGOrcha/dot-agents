package workflow

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

func runWorkflowStatus() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}

	ui.Header("Workflow Status")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Bold, state.Project.Name, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, state.Project.Path, ui.Reset)
	fmt.Fprintln(os.Stdout)

	ui.Section("Project")
	fmt.Fprintf(os.Stdout, "  branch: %s\n", state.Git.Branch)
	fmt.Fprintf(os.Stdout, "  sha: %s\n", state.Git.SHA)
	fmt.Fprintf(os.Stdout, "  dirty files: %d\n", state.Git.DirtyFileCount)
	fmt.Fprintf(os.Stdout, "  canonical plans: %d\n", len(state.CanonicalPlans))
	fmt.Fprintf(os.Stdout, "  active plans: %d\n", len(state.ActivePlans))
	fmt.Fprintf(os.Stdout, "  pending handoffs: %d\n", len(state.Handoffs))
	fmt.Fprintf(os.Stdout, "  lessons: %d\n", len(state.Lessons))
	fmt.Fprintf(os.Stdout, "  pending proposals: %d\n", state.Proposals.PendingCount)
	fmt.Fprintf(os.Stdout, "  active delegations: %d\n", state.ActiveDelegations.ActiveCount)
	fmt.Fprintf(os.Stdout, "  pending merge-backs: %d\n", state.PendingMergeBacks)
	fmt.Fprintln(os.Stdout)

	ui.Section("Last Checkpoint")
	if state.Checkpoint == nil {
		fmt.Fprintln(os.Stdout, "  none")
	} else {
		fmt.Fprintf(os.Stdout, "  timestamp: %s\n", state.Checkpoint.Timestamp)
		fmt.Fprintf(os.Stdout, "  verification: %s\n", state.Checkpoint.Verification.Status)
		if state.Checkpoint.Verification.Summary != "" {
			fmt.Fprintf(os.Stdout, "  summary: %s\n", state.Checkpoint.Verification.Summary)
		}
		fmt.Fprintf(os.Stdout, "  next action: %s\n", state.Checkpoint.NextAction)
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Next Action")
	fmt.Fprintf(os.Stdout, "  recommended: %s\n", state.NextAction)
	fmt.Fprintf(os.Stdout, "  source: %s\n", state.NextActionSource)

	if len(state.Warnings) > 0 {
		fmt.Fprintln(os.Stdout)
		ui.Section("Warnings")
		for _, warning := range state.Warnings {
			fmt.Fprintf(os.Stdout, "  - %s\n", warning)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowOrient() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}
	renderWorkflowOrientMarkdown(state, os.Stdout)
	return nil
}

func runWorkflowCheckpoint(message, verificationStatus, verificationSummary string) error {
	if verificationStatus == "" {
		verificationStatus = workflowDefaultVerificationState
	}
	if !isValidVerificationStatus(verificationStatus) {
		return fmt.Errorf("invalid verification status %q", verificationStatus)
	}

	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}

	checkpoint := workflowCheckpoint{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Project:       project,
		Git: workflowGitSummary{
			Branch:         state.Git.Branch,
			SHA:            state.Git.SHA,
			DirtyFileCount: state.Git.DirtyFileCount,
		},
		Message:    message,
		NextAction: state.NextAction,
		Blockers:   []string{},
	}
	checkpoint.Files.Modified, err = gitModifiedFiles(project.Path)
	if err != nil {
		checkpoint.Files.Modified = []string{}
	}
	checkpoint.Verification.Status = verificationStatus
	checkpoint.Verification.Summary = verificationSummary

	contextDir := config.ProjectContextDir(project.Name)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return err
	}
	checkpointPath := filepath.Join(contextDir, "checkpoint.yaml")
	content, err := yaml.Marshal(checkpoint)
	if err != nil {
		return err
	}
	if err := os.WriteFile(checkpointPath, content, 0644); err != nil {
		return err
	}
	if err := appendWorkflowSessionLog(filepath.Join(contextDir, "session-log.md"), checkpoint); err != nil {
		return err
	}

	ui.Success("Checkpoint written")
	fmt.Fprintf(os.Stdout, "  %s\n\n", config.DisplayPath(checkpointPath))
	return nil
}

// iterLogEntry is schema_version 2 YAML for .agents/active/iteration-log/iter-N.yaml.
// Top-level git fields refresh on every checkpoint --log-to-iter call; impl / verifiers / review merge by role.
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

// iterLogV1Legacy unmarshals on-disk iteration logs written before schema_version 2.
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

// iterLogDiffStat holds parsed output from git diff --stat HEAD~1.
type iterLogDiffStat struct {
	FilesChanged int
	LinesAdded   int
	LinesRemoved int
	FirstCommit  bool
}

// gitIterDiffStat runs git diff --stat HEAD~1 and parses the summary line.
// If HEAD~1 doesn't exist (first commit), all counts are 0 and FirstCommit is true.
func gitIterDiffStat(projectPath string) iterLogDiffStat {
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "HEAD~1")
	if err := cmd.Run(); err != nil {
		// HEAD~1 does not exist: first commit
		return iterLogDiffStat{FirstCommit: true}
	}

	out := strings.TrimSpace(gitOutput(projectPath, "diff", "--stat", "HEAD~1"))
	if out == "" {
		return iterLogDiffStat{}
	}

	// The summary line is the last non-empty line, e.g.:
	//   3 files changed, 42 insertions(+), 5 deletions(-)
	//   1 file changed, 10 insertions(+)
	//   1 file changed, 3 deletions(-)
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

// parseGitDiffStatSummary parses a git diff --stat summary line into counts.
func parseGitDiffStatSummary(summary string) iterLogDiffStat {
	var result iterLogDiffStat
	// files changed
	if idx := strings.Index(summary, " file"); idx != -1 {
		fmt.Sscanf(strings.TrimSpace(summary[:idx]), "%d", &result.FilesChanged)
	}
	// insertions
	if idx := strings.Index(summary, " insertion"); idx != -1 {
		// walk back to the comma or start
		start := idx
		for start > 0 && summary[start-1] != ',' {
			start--
		}
		fmt.Sscanf(strings.TrimSpace(summary[start:idx]), "%d", &result.LinesAdded)
	}
	// deletions
	if idx := strings.Index(summary, " deletion"); idx != -1 {
		start := idx
		for start > 0 && summary[start-1] != ',' {
			start--
		}
		fmt.Sscanf(strings.TrimSpace(summary[start:idx]), "%d", &result.LinesRemoved)
	}
	return result
}

// firstReadableDelegationContract returns the first readable delegation contract in lexical dir order.
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

// scanActiveDelegationContract scans .agents/active/delegation/*.yaml and returns
// the first contract's (plan_id, task_id). Returns empty strings if none found.
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

func runWorkflowLog(showAll bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	logPath := filepath.Join(config.ProjectContextDir(project.Name), "session-log.md")
	content, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No session log found.")
			return nil
		}
		return err
	}

	entries := splitWorkflowLogEntries(string(content))
	if !showAll && len(entries) > 10 {
		entries = entries[len(entries)-10:]
	}

	ui.Header("Workflow Log")
	for _, entry := range entries {
		fmt.Fprintln(os.Stdout, entry)
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// collectDelegationSummary loads active delegations and counts pending intents and merge-backs.
func collectDelegationSummary(projectPath string) (workflowDelegationSummary, int) {
	contracts, err := listDelegationContracts(projectPath)
	if err != nil {
		return workflowDelegationSummary{}, 0
	}
	summary := workflowDelegationSummary{}
	for _, c := range contracts {
		if c.Status == "pending" || c.Status == "active" {
			summary.ActiveCount++
			if c.PendingIntent != CoordinationIntentNone {
				summary.PendingIntents++
			}
		}
	}
	// Count unprocessed merge-back artifacts
	mergeBackEntries, err := os.ReadDir(mergeBackDir(projectPath))
	pendingMergebacks := 0
	if err == nil {
		for _, e := range mergeBackEntries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				pendingMergebacks++
			}
		}
	}
	return summary, pendingMergebacks
}

func collectWorkflowState() (*workflowOrientState, error) {
	project, err := currentWorkflowProject()
	if err != nil {
		return nil, err
	}

	gitSummary, gitWarnings := collectWorkflowGitSummary(project.Path)
	activePlans, err := collectWorkflowPlans(project.Path)
	if err != nil {
		return nil, err
	}
	canonicalPlans, canonicalWarnings := collectCanonicalPlans(project.Path)
	handoffs, err := collectWorkflowHandoffs(project.Path)
	if err != nil {
		return nil, err
	}
	lessons, lessonWarnings := collectWorkflowLessons(project.Path)
	checkpoint, checkpointWarnings := loadWorkflowCheckpoint(project.Name)
	proposals, err := countPendingWorkflowProposals()
	if err != nil {
		return nil, err
	}
	delegationSummary, pendingMergebacks := collectDelegationSummary(project.Path)

	warnings := append([]string{}, gitWarnings...)
	warnings = append(warnings, canonicalWarnings...)
	warnings = append(warnings, lessonWarnings...)
	warnings = append(warnings, checkpointWarnings...)

	state := &workflowOrientState{
		Project:        project,
		Git:            gitSummary,
		ActivePlans:    activePlans,
		CanonicalPlans: canonicalPlans,
		Checkpoint:     checkpoint,
		Handoffs:       handoffs,
		Lessons:        lessons,
		Proposals: workflowProposalSummary{
			PendingCount: proposals,
		},
		Warnings:          warnings,
		ActiveDelegations: delegationSummary,
		PendingMergeBacks: pendingMergebacks,
	}
	state.NextAction, state.NextActionSource = deriveWorkflowNextAction(gitSummary, checkpoint, canonicalPlans, activePlans)
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" && !isCheckpointCurrent(gitSummary, checkpoint) && state.NextActionSource != "checkpoint" {
		warnings = append(warnings, fmt.Sprintf("checkpoint next action %q is stale relative to current git state; using %s", checkpoint.NextAction, state.NextActionSource))
		state.Warnings = warnings
	}

	// Wave 7 Step 7: local drift check for current project
	localDrift := detectRepoDrift(
		ManagedProject{Name: project.Name, Path: project.Path},
		defaultCheckpointStaleDays, defaultProposalStaleDays,
	)
	if localDrift.Status != "healthy" {
		state.LocalDrift = &localDrift
	}
	health := computeWorkflowHealth(state)
	state.Health = &health

	prefs, err := resolvePreferences(project.Path, project.Name)
	if err == nil {
		state.Preferences = &prefs
	}

	return state, nil
}

func currentWorkflowProject() (workflowProjectRef, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return workflowProjectRef{}, err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return workflowProjectRef{}, err
	}

	project := filepath.Base(cwd)
	if rc, err := config.LoadAgentsRC(cwd); err == nil && strings.TrimSpace(rc.Project) != "" {
		project = strings.TrimSpace(rc.Project)
	}
	return workflowProjectRef{Name: project, Path: cwd}, nil
}

func collectWorkflowGitSummary(projectPath string) (workflowGitSummary, []string) {
	summary := workflowGitSummary{
		Branch:         "unknown",
		SHA:            "unknown",
		DirtyFileCount: 0,
	}
	var warnings []string
	if !isGitRepo(projectPath) {
		warnings = append(warnings, "git repo not detected")
		return summary, warnings
	}

	summary.Branch = strings.TrimSpace(gitOutput(projectPath, "rev-parse", "--abbrev-ref", "HEAD"))
	if summary.Branch == "" {
		summary.Branch = "unknown"
	}
	summary.SHA = strings.TrimSpace(gitOutput(projectPath, "rev-parse", "--short", "HEAD"))
	if summary.SHA == "" {
		summary.SHA = "unknown"
	}
	statusLines := strings.TrimSpace(gitOutput(projectPath, "status", "--short"))
	if statusLines != "" {
		summary.DirtyFileCount = len(strings.Split(statusLines, "\n"))
	}
	commits := strings.TrimSpace(gitOutput(projectPath, "log", "--oneline", "-5"))
	if commits != "" {
		summary.RecentCommits = strings.Split(commits, "\n")
	}
	return summary, warnings
}

func collectWorkflowPlans(projectPath string) ([]workflowPlanSummary, error) {
	paths, err := filepath.Glob(filepath.Join(projectPath, ".agents", "active", "*.plan.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	plans := make([]workflowPlanSummary, 0, len(paths))
	for _, path := range paths {
		plan, err := readWorkflowPlan(path)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func collectWorkflowHandoffs(projectPath string) ([]workflowHandoffSummary, error) {
	paths, err := filepath.Glob(filepath.Join(projectPath, ".agents", "active", "handoffs", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	handoffs := make([]workflowHandoffSummary, 0, len(paths))
	for _, path := range paths {
		title, err := firstMarkdownTitle(path)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, workflowHandoffSummary{Path: path, Title: title})
	}
	return handoffs, nil
}

func collectWorkflowLessons(projectPath string) ([]string, []string) {
	candidates := []string{
		filepath.Join(projectPath, ".agents", "lessons", "index.md"),
		filepath.Join(projectPath, ".agents", "lessons.md"),
	}
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		lines := make([]string, 0)
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		return lines, nil
	}
	return []string{}, []string{"lessons index not found"}
}

func loadWorkflowCheckpoint(project string) (*workflowCheckpoint, []string) {
	checkpointPath := filepath.Join(config.ProjectContextDir(project), "checkpoint.yaml")
	content, err := os.ReadFile(checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{"checkpoint unreadable"}
	}
	var checkpoint workflowCheckpoint
	if err := yaml.Unmarshal(content, &checkpoint); err != nil {
		return nil, []string{"checkpoint unreadable"}
	}
	return &checkpoint, nil
}

func countPendingWorkflowProposals() (int, error) {
	dir := filepath.Join(config.AgentsHome(), "proposals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".yaml") {
			count++
		}
	}
	return count, nil
}

func deriveWorkflowNextAction(git workflowGitSummary, checkpoint *workflowCheckpoint, canonicalPlans []workflowCanonicalPlanSummary, plans []workflowPlanSummary) (string, string) {
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" && isCheckpointCurrent(git, checkpoint) {
		return strings.TrimSpace(checkpoint.NextAction), "checkpoint"
	}
	for _, cp := range canonicalPlans {
		if cp.Status == "active" && strings.TrimSpace(cp.CurrentFocusTask) != "" {
			return strings.TrimSpace(cp.CurrentFocusTask), "canonical_plan"
		}
	}
	for _, plan := range plans {
		if len(plan.PendingItems) > 0 {
			return plan.PendingItems[0], "active_plan"
		}
	}
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" {
		return strings.TrimSpace(checkpoint.NextAction), "checkpoint_stale"
	}
	return workflowDefaultNextAction, "default"
}

func isCheckpointCurrent(git workflowGitSummary, checkpoint *workflowCheckpoint) bool {
	if checkpoint == nil {
		return false
	}
	if strings.TrimSpace(checkpoint.Git.Branch) == "" || strings.TrimSpace(checkpoint.Git.SHA) == "" {
		return false
	}
	return checkpoint.Git.Branch == git.Branch && checkpoint.Git.SHA == git.SHA
}

func readWorkflowPlan(path string) (workflowPlanSummary, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return workflowPlanSummary{}, err
	}
	lines := strings.Split(string(content), "\n")
	title := filepath.Base(path)
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "#") {
			title = strings.TrimSpace(strings.TrimLeft(first, "# "))
		}
	}
	var pending []string
	var fallback []string
	completed := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Status:") {
			status := strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:"))
			if strings.HasPrefix(strings.ToLower(status), "completed") {
				completed = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- [ ] ") {
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(trimmed, "- [ ] ")))
			if len(pending) == 3 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(fallback) < 3 {
			fallback = append(fallback, trimmed)
		}
	}
	if completed {
		pending = nil
	} else if len(pending) == 0 {
		pending = fallback
	}
	return workflowPlanSummary{Path: path, Title: title, PendingItems: pending}, nil
}

func firstMarkdownTitle(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "# ")), nil
		}
	}
	return filepath.Base(path), nil
}

func renderWorkflowOrientMarkdown(state *workflowOrientState, out io.Writer) {
	fmt.Fprintln(out, "# Project")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- name: %s\n", state.Project.Name)
	fmt.Fprintf(out, "- path: %s\n", state.Project.Path)
	fmt.Fprintf(out, "- branch: %s\n", state.Git.Branch)
	fmt.Fprintf(out, "- sha: %s\n", state.Git.SHA)
	fmt.Fprintf(out, "- dirty files: %d\n", state.Git.DirtyFileCount)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Canonical Plans")
	fmt.Fprintln(out)
	if len(state.CanonicalPlans) == 0 {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		for _, cp := range state.CanonicalPlans {
			fmt.Fprintf(out, "## %s (%s)\n", cp.Title, cp.Status)
			fmt.Fprintf(out, "- id: %s\n", cp.ID)
			if cp.CurrentFocusTask != "" {
				fmt.Fprintf(out, "- focus: %s\n", cp.CurrentFocusTask)
			}
			fmt.Fprintf(out, "- tasks: %d pending, %d blocked, %d completed\n", cp.PendingCount, cp.BlockedCount, cp.CompletedCount)
			fmt.Fprintln(out)
		}
	}

	fmt.Fprintln(out, "# Active Plans")
	fmt.Fprintln(out)
	if len(state.ActivePlans) == 0 {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		for _, plan := range state.ActivePlans {
			fmt.Fprintf(out, "## %s\n", plan.Title)
			fmt.Fprintf(out, "- path: %s\n", plan.Path)
			if len(plan.PendingItems) == 0 {
				fmt.Fprintln(out, "- no pending items found")
			} else {
				for _, item := range plan.PendingItems {
					fmt.Fprintf(out, "- %s\n", item)
				}
			}
			fmt.Fprintln(out)
		}
	}

	fmt.Fprintln(out, "# Last Checkpoint")
	fmt.Fprintln(out)
	if state.Checkpoint == nil {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		fmt.Fprintf(out, "- timestamp: %s\n", state.Checkpoint.Timestamp)
		fmt.Fprintf(out, "- branch: %s\n", state.Checkpoint.Git.Branch)
		fmt.Fprintf(out, "- sha: %s\n", state.Checkpoint.Git.SHA)
		fmt.Fprintf(out, "- verification: %s\n", state.Checkpoint.Verification.Status)
		if state.Checkpoint.Verification.Summary != "" {
			fmt.Fprintf(out, "- summary: %s\n", state.Checkpoint.Verification.Summary)
		}
		fmt.Fprintf(out, "- next action: %s\n", state.Checkpoint.NextAction)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "# Pending Handoffs")
	fmt.Fprintln(out)
	if len(state.Handoffs) == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		for _, handoff := range state.Handoffs {
			fmt.Fprintf(out, "- %s (%s)\n", handoff.Title, handoff.Path)
		}
	}
	fmt.Fprintln(out)

	// Wave 6 Step 7: Delegations section
	fmt.Fprintln(out, "# Delegations")
	fmt.Fprintln(out)
	if state.ActiveDelegations.ActiveCount == 0 && state.PendingMergeBacks == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		fmt.Fprintf(out, "- active delegations: %d\n", state.ActiveDelegations.ActiveCount)
		if state.ActiveDelegations.PendingIntents > 0 {
			fmt.Fprintf(out, "- pending intents: %d (check delegation contracts)\n", state.ActiveDelegations.PendingIntents)
		}
		fmt.Fprintf(out, "- pending merge-backs: %d\n", state.PendingMergeBacks)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Recent Lessons")
	fmt.Fprintln(out)
	if len(state.Lessons) == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		for _, lesson := range state.Lessons {
			fmt.Fprintf(out, "- %s\n", lesson)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Pending Proposals")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- count: %d\n", state.Proposals.PendingCount)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Next Action")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- %s\n", state.NextAction)
	fmt.Fprintf(out, "- source: %s\n", state.NextActionSource)

	if len(state.Git.RecentCommits) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Recent Commits")
		fmt.Fprintln(out)
		for _, commit := range state.Git.RecentCommits {
			fmt.Fprintln(out, commit)
		}
	}
	if state.Health != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Health")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "- status: %s\n", state.Health.Status)
		for _, w := range state.Health.Warnings {
			fmt.Fprintf(out, "- warning: %s\n", w)
		}
	}
	if p := state.Preferences; p != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Preferences")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "- test_command: %s\n", strPtrVal(p.Verification.TestCommand))
		fmt.Fprintf(out, "- lint_command: %s\n", strPtrVal(p.Verification.LintCommand))
		fmt.Fprintf(out, "- plan_directory: %s\n", strPtrVal(p.Planning.PlanDirectory))
		fmt.Fprintf(out, "- package_manager: %s\n", strPtrVal(p.Execution.PackageManager))
		fmt.Fprintf(out, "- formatter: %s\n", strPtrVal(p.Execution.Formatter))
	}
	if state.LocalDrift != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Local Drift")
		fmt.Fprintln(out)
		for _, w := range state.LocalDrift.Warnings {
			fmt.Fprintf(out, "- warn: %s\n", w)
		}
		fmt.Fprintln(out, "  (run 'dot-agents workflow drift' for cross-repo view)")
	}
	if len(state.Warnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Warnings")
		fmt.Fprintln(out)
		for _, warning := range state.Warnings {
			fmt.Fprintf(out, "- %s\n", warning)
		}
	}
}

func appendWorkflowSessionLog(path string, checkpoint workflowCheckpoint) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "## %s\n", checkpoint.Timestamp); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "branch: %s\n", checkpoint.Git.Branch); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "sha: %s\n", checkpoint.Git.SHA); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "files: %d\n", len(checkpoint.Files.Modified)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "verification: %s\n", checkpoint.Verification.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "message: %s\n", checkpoint.Message); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "next_action: %s\n\n", checkpoint.NextAction); err != nil {
		return err
	}
	return nil
}

func splitWorkflowLogEntries(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	parts := strings.Split(content, "\n## ")
	entries := make([]string, 0, len(parts))
	for i, part := range parts {
		entry := part
		if i > 0 {
			entry = "## " + part
		}
		entry = strings.TrimSpace(entry)
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func isGitRepo(projectPath string) bool {
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

func gitOutput(projectPath string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", projectPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func gitModifiedFiles(projectPath string) ([]string, error) {
	if !isGitRepo(projectPath) {
		return []string{}, nil
	}
	output := strings.TrimSpace(gitOutput(projectPath, "status", "--short"))
	if output == "" {
		return []string{}, nil
	}
	lines := strings.Split(output, "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		files = append(files, strings.TrimSpace(line[3:]))
	}
	return files, nil
}

func isValidVerificationStatus(status string) bool {
	switch status {
	case "pass", "fail", "partial", "unknown":
		return true
	default:
		return false
	}
}

// ── Canonical plan I/O ───────────────────────────────────────────────────────

func plansBaseDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "workflow", "plans")
}

func listCanonicalPlanIDs(projectPath string) ([]string, error) {
	base := plansBaseDir(projectPath)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ── Wave 3: Verification log ──────────────────────────────────────────────────

func isValidVerificationKind(k string) bool {
	switch strings.TrimSpace(strings.ToLower(k)) {
	case "test", "lint", "build", "format", "custom", "review":
		return true
	default:
		return false
	}
}

func isValidVerificationScope(s string) bool {
	switch s {
	case "file", "package", "repo", "custom":
		return true
	default:
		return false
	}
}

func verificationLogPath(project string) string {
	return filepath.Join(config.ProjectContextDir(project), "verification-log.jsonl")
}

func appendVerificationLog(project string, rec VerificationRecord) error {
	if err := os.MkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(verificationLogPath(project), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

func readVerificationLog(project string, limit int) ([]VerificationRecord, error) {
	content, err := os.ReadFile(verificationLogPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return []VerificationRecord{}, nil
		}
		return nil, err
	}
	var records []VerificationRecord
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec VerificationRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

// ── Wave 3: Health snapshot ───────────────────────────────────────────────────

func healthSnapshotPath(project string) string {
	return filepath.Join(config.ProjectContextDir(project), "health.json")
}

func computeWorkflowHealth(state *workflowOrientState) WorkflowHealthSnapshot {
	h := WorkflowHealthSnapshot{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Status:        "healthy",
	}
	h.Git.InsideRepo = state.Git.Branch != "unknown"
	h.Git.Branch = state.Git.Branch
	h.Git.DirtyFileCount = state.Git.DirtyFileCount
	h.Workflow.HasActivePlan = len(state.ActivePlans) > 0 || len(state.CanonicalPlans) > 0
	h.Workflow.HasCheckpoint = state.Checkpoint != nil
	h.Workflow.PendingProposals = state.Proposals.PendingCount
	h.Workflow.CanonicalPlanCount = len(state.CanonicalPlans)
	h.Tooling.MCP = "unknown"
	h.Tooling.Auth = "unknown"
	h.Tooling.Formatter = "unknown"

	var warnings []string
	if state.Git.DirtyFileCount > 20 {
		warnings = append(warnings, fmt.Sprintf("%d dirty files — consider a checkpoint", state.Git.DirtyFileCount))
	}
	if state.Proposals.PendingCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d pending proposal(s) need review", state.Proposals.PendingCount))
	}
	if !h.Workflow.HasCheckpoint {
		warnings = append(warnings, "no checkpoint recorded")
	}
	if len(warnings) > 0 {
		h.Status = "warn"
		h.Warnings = warnings
	} else {
		h.Warnings = []string{}
	}
	return h
}

func writeHealthSnapshot(project string, h WorkflowHealthSnapshot) error {
	if err := os.MkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(healthSnapshotPath(project), content, 0644)
}

func readHealthSnapshot(project string) (*WorkflowHealthSnapshot, error) {
	content, err := os.ReadFile(healthSnapshotPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var h WorkflowHealthSnapshot
	if err := json.Unmarshal(content, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ── Wave 3: Run functions ─────────────────────────────────────────────────────

func runWorkflowHealth() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	health := computeWorkflowHealth(state)
	// Persist the snapshot
	_ = writeHealthSnapshot(state.Project.Name, health)

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(health)
	}

	ui.Header("Workflow Health")
	statusIcon := "✓"
	if health.Status == "warn" {
		statusIcon = "⚠"
	} else if health.Status == "error" {
		statusIcon = "✗"
	}
	fmt.Fprintf(os.Stdout, "  %s status: %s\n\n", statusIcon, health.Status)

	ui.Section("Git")
	fmt.Fprintf(os.Stdout, "  branch: %s\n", health.Git.Branch)
	fmt.Fprintf(os.Stdout, "  dirty files: %d\n", health.Git.DirtyFileCount)
	fmt.Fprintln(os.Stdout)

	ui.Section("Workflow")
	fmt.Fprintf(os.Stdout, "  has active plan: %v\n", health.Workflow.HasActivePlan)
	fmt.Fprintf(os.Stdout, "  canonical plans: %d\n", health.Workflow.CanonicalPlanCount)
	fmt.Fprintf(os.Stdout, "  has checkpoint: %v\n", health.Workflow.HasCheckpoint)
	fmt.Fprintf(os.Stdout, "  pending proposals: %d\n", health.Workflow.PendingProposals)
	fmt.Fprintln(os.Stdout)

	if len(health.Warnings) > 0 {
		ui.Section("Warnings")
		for _, w := range health.Warnings {
			fmt.Fprintf(os.Stdout, "  - %s\n", w)
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func runWorkflowVerifyRecordReview(command, scope, summary, phase1In, phase2In, overallIn, escalation, reviewerNotes, taskFlag string, failedGatesInput []string) error {
	if !isValidVerificationScope(scope) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid scope %q", scope),
			"Valid verification scopes: `file`, `package`, `repo`, `custom`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	phase1, err := parseReviewPhaseDecision("--phase1-decision", phase1In)
	if err != nil {
		return err
	}
	phase2, err := parseReviewPhaseDecision("--phase2-decision", phase2In)
	if err != nil {
		return err
	}
	derived := deriveOverallReviewDecision(phase1, phase2)
	overall := strings.TrimSpace(strings.ToLower(overallIn))
	if overall == "" {
		overall = derived
	} else if overall != derived {
		return deps.ErrorWithHints(
			fmt.Sprintf("overall decision %q disagrees with phases (derived %q from phase_1=%s phase_2=%s)", overall, derived, phase1, phase2),
			"Omit --overall-decision to use derived consolidation, or adjust phase flags so the derived value matches.",
		)
	}
	if overall == "escalate" && strings.TrimSpace(escalation) == "" {
		return deps.ErrorWithHints(
			"overall decision is escalate but --escalation-reason is empty",
			"Provide a non-empty --escalation-reason whenever the consolidated decision is escalate.",
		)
	}

	taskID := strings.TrimSpace(taskFlag)
	var contract *DelegationContract
	if taskID == "" {
		contract = firstReadableDelegationContract(project.Path)
		if contract == nil {
			return deps.ErrorWithHints(
				"review verify record needs a delegation task id",
				"Pass --task <task_id> matching `.agents/active/delegation/<task_id>.yaml`, or keep a single readable active delegation contract.",
			)
		}
		taskID = contract.ParentTaskID
	} else {
		contract, err = loadDelegationContract(project.Path, taskID)
		if err != nil {
			return fmt.Errorf("load delegation contract for task %q: %w", taskID, err)
		}
	}

	failedGates := trimStringSlice(failedGatesInput)
	if failedGates == nil {
		failedGates = []string{}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	doc := &ReviewDecisionDoc{
		SchemaVersion:    1,
		TaskID:           taskID,
		ParentPlanID:     contract.ParentPlanID,
		DelegationID:     contract.ID,
		Phase1Decision:   phase1,
		Phase2Decision:   phase2,
		OverallDecision:  overall,
		FailedGates:      failedGates,
		EscalationReason: strings.TrimSpace(escalation),
		ReviewerNotes:    strings.TrimSpace(reviewerNotes),
		RecordedAt:       now,
		RecordedBy:       "dot-agents workflow verify record",
	}
	if err := writeReviewDecisionYAML(project.Path, doc); err != nil {
		return err
	}

	artifactRel := iterLogReviewDecisionPath(taskID)
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     now,
		Kind:          "review",
		Status:        overallDecisionToVerificationStatus(overall),
		Command:       strings.TrimSpace(command),
		Scope:         scope,
		Summary:       strings.TrimSpace(summary),
		Artifacts:     []string{artifactRel},
		RecordedBy:    "dot-agents workflow verify record",
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Review decision recorded for task %s: overall=%s (%s)", taskID, overall, strings.TrimSpace(summary)))
	return nil
}

func runWorkflowVerifyRecord(kind, status, command, scope, summary string) error {
	if strings.TrimSpace(strings.ToLower(kind)) == "review" {
		return fmt.Errorf("internal error: use runWorkflowVerifyRecordReview for kind review")
	}
	if !isValidVerificationKind(kind) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid kind %q", kind),
			"Valid verification kinds: `test`, `lint`, `build`, `format`, `custom`, `review`.",
		)
	}
	if !isValidVerificationStatus(status) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid status %q", status),
			"Valid verification statuses: `pass`, `fail`, `partial`, `unknown`.",
		)
	}
	if !isValidVerificationScope(scope) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid scope %q", scope),
			"Valid verification scopes: `file`, `package`, `repo`, `custom`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Kind:          kind,
		Status:        status,
		Command:       command,
		Scope:         scope,
		Summary:       summary,
		Artifacts:     []string{},
		RecordedBy:    "dot-agents workflow verify record",
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Verification recorded: %s %s (%s)", kind, status, summary))
	return nil
}

func runWorkflowVerifyLog(all bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	limit := 10
	if all {
		limit = 0
	}
	records, err := readVerificationLog(project.Name, limit)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(os.Stdout, "No verification records found.")
		return nil
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	ui.Header("Verification Log")
	for _, r := range records {
		icon := "✓"
		if r.Status == "fail" {
			icon = "✗"
		} else if r.Status == "partial" {
			icon = "~"
		} else if r.Status == "unknown" {
			icon = "?"
		}
		fmt.Fprintf(os.Stdout, "  %s [%s] %s  %s\n", icon, r.Kind, r.Timestamp, r.Summary)
		if r.Command != "" {
			fmt.Fprintf(os.Stdout, "    cmd: %s\n", r.Command)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// ── Wave 4: Shared preferences ────────────────────────────────────────────────

// WorkflowPreferences holds all workflow preference fields. Every field is a
// pointer so that "not set" is distinguishable from the zero value during merge.
type WorkflowPreferences struct {
	Verification WorkflowVerificationPrefs `json:"verification" yaml:"verification"`
	Planning     WorkflowPlanningPrefs     `json:"planning"     yaml:"planning"`
	Review       WorkflowReviewPrefs       `json:"review"       yaml:"review"`
	Execution    WorkflowExecutionPrefs    `json:"execution"    yaml:"execution"`
}

type WorkflowVerificationPrefs struct {
	TestCommand                    *string `json:"test_command,omitempty"                      yaml:"test_command,omitempty"`
	LintCommand                    *string `json:"lint_command,omitempty"                      yaml:"lint_command,omitempty"`
	RequireRegressionBeforeHandoff *bool   `json:"require_regression_before_handoff,omitempty" yaml:"require_regression_before_handoff,omitempty"`
}

type WorkflowPlanningPrefs struct {
	PlanDirectory         *string `json:"plan_directory,omitempty"          yaml:"plan_directory,omitempty"`
	RequirePlanBeforeCode *bool   `json:"require_plan_before_code,omitempty" yaml:"require_plan_before_code,omitempty"`
}

type WorkflowReviewPrefs struct {
	ReviewOrder          *string `json:"review_order,omitempty"           yaml:"review_order,omitempty"`
	RequireFindingsFirst *bool   `json:"require_findings_first,omitempty" yaml:"require_findings_first,omitempty"`
}

type WorkflowExecutionPrefs struct {
	PackageManager *string `json:"package_manager,omitempty" yaml:"package_manager,omitempty"`
	Formatter      *string `json:"formatter,omitempty"       yaml:"formatter,omitempty"`
}

// WorkflowPreferencesFile is the on-disk wrapper for preferences.yaml.
type WorkflowPreferencesFile struct {
	SchemaVersion       int `json:"schema_version" yaml:"schema_version"`
	WorkflowPreferences `yaml:",inline"      json:",inline"`
}

// preferenceSource records where a resolved preference value came from.
type preferenceSource struct {
	Key    string
	Value  string
	Source string // "default" | "repo" | "local"
}

func defaultWorkflowPreferences() WorkflowPreferences {
	trueVal := true
	testCmd := "go test ./..."
	lintCmd := "go vet ./..."
	planDir := ".agents/active"
	reviewOrder := "findings-first"
	pkgMgr := "go"
	formatter := "gofmt"
	return WorkflowPreferences{
		Verification: WorkflowVerificationPrefs{
			TestCommand:                    &testCmd,
			LintCommand:                    &lintCmd,
			RequireRegressionBeforeHandoff: &trueVal,
		},
		Planning: WorkflowPlanningPrefs{
			PlanDirectory:         &planDir,
			RequirePlanBeforeCode: &trueVal,
		},
		Review: WorkflowReviewPrefs{
			ReviewOrder:          &reviewOrder,
			RequireFindingsFirst: &trueVal,
		},
		Execution: WorkflowExecutionPrefs{
			PackageManager: &pkgMgr,
			Formatter:      &formatter,
		},
	}
}

func loadRepoPreferences(projectPath string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(projectPath, ".agents", "workflow", "preferences.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse repo preferences: %w", err)
	}
	return &f, nil
}

func loadLocalPreferences(project string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse local preferences: %w", err)
	}
	return &f, nil
}

// mergePreferences applies precedence: local > repo > defaults.
// Only non-nil pointer fields override.
func mergePreferences(defaults, repo, local WorkflowPreferences) WorkflowPreferences {
	out := defaults
	mergeVerificationPrefs(&out.Verification, repo.Verification)
	mergePlanningPrefs(&out.Planning, repo.Planning)
	mergeReviewPrefs(&out.Review, repo.Review)
	mergeExecutionPrefs(&out.Execution, repo.Execution)
	mergeVerificationPrefs(&out.Verification, local.Verification)
	mergePlanningPrefs(&out.Planning, local.Planning)
	mergeReviewPrefs(&out.Review, local.Review)
	mergeExecutionPrefs(&out.Execution, local.Execution)
	return out
}

func mergeVerificationPrefs(dst *WorkflowVerificationPrefs, src WorkflowVerificationPrefs) {
	if src.TestCommand != nil {
		dst.TestCommand = src.TestCommand
	}
	if src.LintCommand != nil {
		dst.LintCommand = src.LintCommand
	}
	if src.RequireRegressionBeforeHandoff != nil {
		dst.RequireRegressionBeforeHandoff = src.RequireRegressionBeforeHandoff
	}
}

func mergePlanningPrefs(dst *WorkflowPlanningPrefs, src WorkflowPlanningPrefs) {
	if src.PlanDirectory != nil {
		dst.PlanDirectory = src.PlanDirectory
	}
	if src.RequirePlanBeforeCode != nil {
		dst.RequirePlanBeforeCode = src.RequirePlanBeforeCode
	}
}

func mergeReviewPrefs(dst *WorkflowReviewPrefs, src WorkflowReviewPrefs) {
	if src.ReviewOrder != nil {
		dst.ReviewOrder = src.ReviewOrder
	}
	if src.RequireFindingsFirst != nil {
		dst.RequireFindingsFirst = src.RequireFindingsFirst
	}
}

func mergeExecutionPrefs(dst *WorkflowExecutionPrefs, src WorkflowExecutionPrefs) {
	if src.PackageManager != nil {
		dst.PackageManager = src.PackageManager
	}
	if src.Formatter != nil {
		dst.Formatter = src.Formatter
	}
}

func resolvePreferences(projectPath, project string) (WorkflowPreferences, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return defaults, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return defaults, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	return mergePreferences(defaults, repo, local), nil
}

var knownPreferenceKeys = map[string]struct{}{
	"verification.test_command":                      {},
	"verification.lint_command":                      {},
	"verification.require_regression_before_handoff": {},
	"planning.plan_directory":                        {},
	"planning.require_plan_before_code":              {},
	"review.review_order":                            {},
	"review.require_findings_first":                  {},
	"execution.package_manager":                      {},
	"execution.formatter":                            {},
}

func isValidPreferenceKey(key string) bool {
	_, ok := knownPreferenceKeys[key]
	return ok
}

func setLocalPreference(project, key, value string) error {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	var f WorkflowPreferencesFile
	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse local preferences: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f.SchemaVersion = 1
	if err := applyPreferenceKey(&f.WorkflowPreferences, key, value); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(&f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func applyPreferenceKey(p *WorkflowPreferences, key, value string) error {
	switch key {
	case "verification.test_command":
		p.Verification.TestCommand = &value
	case "verification.lint_command":
		p.Verification.LintCommand = &value
	case "verification.require_regression_before_handoff":
		b := value == "true"
		p.Verification.RequireRegressionBeforeHandoff = &b
	case "planning.plan_directory":
		p.Planning.PlanDirectory = &value
	case "planning.require_plan_before_code":
		b := value == "true"
		p.Planning.RequirePlanBeforeCode = &b
	case "review.review_order":
		p.Review.ReviewOrder = &value
	case "review.require_findings_first":
		b := value == "true"
		p.Review.RequireFindingsFirst = &b
	case "execution.package_manager":
		p.Execution.PackageManager = &value
	case "execution.formatter":
		p.Execution.Formatter = &value
	default:
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to list valid preference keys.",
		)
	}
	return nil
}

func resolvePreferencesWithSources(projectPath, project string) ([]preferenceSource, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return nil, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return nil, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	resolved := mergePreferences(defaults, repo, local)

	strSrc := func(_, r, l *string) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}
	boolSrc := func(_, r, l *bool) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}

	return []preferenceSource{
		{"verification.test_command", strPtrVal(resolved.Verification.TestCommand), strSrc(defaults.Verification.TestCommand, repo.Verification.TestCommand, local.Verification.TestCommand)},
		{"verification.lint_command", strPtrVal(resolved.Verification.LintCommand), strSrc(defaults.Verification.LintCommand, repo.Verification.LintCommand, local.Verification.LintCommand)},
		{"verification.require_regression_before_handoff", boolPtrStr(resolved.Verification.RequireRegressionBeforeHandoff), boolSrc(defaults.Verification.RequireRegressionBeforeHandoff, repo.Verification.RequireRegressionBeforeHandoff, local.Verification.RequireRegressionBeforeHandoff)},
		{"planning.plan_directory", strPtrVal(resolved.Planning.PlanDirectory), strSrc(defaults.Planning.PlanDirectory, repo.Planning.PlanDirectory, local.Planning.PlanDirectory)},
		{"planning.require_plan_before_code", boolPtrStr(resolved.Planning.RequirePlanBeforeCode), boolSrc(defaults.Planning.RequirePlanBeforeCode, repo.Planning.RequirePlanBeforeCode, local.Planning.RequirePlanBeforeCode)},
		{"review.review_order", strPtrVal(resolved.Review.ReviewOrder), strSrc(defaults.Review.ReviewOrder, repo.Review.ReviewOrder, local.Review.ReviewOrder)},
		{"review.require_findings_first", boolPtrStr(resolved.Review.RequireFindingsFirst), boolSrc(defaults.Review.RequireFindingsFirst, repo.Review.RequireFindingsFirst, local.Review.RequireFindingsFirst)},
		{"execution.package_manager", strPtrVal(resolved.Execution.PackageManager), strSrc(defaults.Execution.PackageManager, repo.Execution.PackageManager, local.Execution.PackageManager)},
		{"execution.formatter", strPtrVal(resolved.Execution.Formatter), strSrc(defaults.Execution.Formatter, repo.Execution.Formatter, local.Execution.Formatter)},
	}, nil
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolPtrStr(p *bool) string {
	if p == nil {
		return ""
	}
	if *p {
		return "true"
	}
	return "false"
}

func runWorkflowPrefs() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
		prefs, err := resolvePreferences(project.Path, project.Name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prefs)
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	ui.Header("Workflow Preferences")
	currentCategory := ""
	for _, s := range sources {
		parts := strings.SplitN(s.Key, ".", 2)
		if len(parts) == 2 && parts[0] != currentCategory {
			currentCategory = parts[0]
			fmt.Fprintf(os.Stdout, "\n[%s]\n", currentCategory)
		}
		fmt.Fprintf(os.Stdout, "  %-48s %s  (%s)\n", s.Key, s.Value, s.Source)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPrefsSetLocal(key, value string) error {
	if !isValidPreferenceKey(key) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to see valid preference keys.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if err := setLocalPreference(project.Name, key, value); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Set %s = %s  (local)", key, value))
	return nil
}

func runWorkflowPrefsSetShared(key, value string) error {
	if !isValidPreferenceKey(key) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown preference key %q", key),
			"Run `dot-agents workflow prefs` to see valid preference keys.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	currentVal := ""
	for _, s := range sources {
		if s.Key == key {
			currentVal = s.Value
			break
		}
	}
	id := fmt.Sprintf("pref-%s-%s", strings.ReplaceAll(key, ".", "-"), time.Now().UTC().Format("20060102T150405Z"))
	targetPath := filepath.Join(".agents", "workflow", "preferences.yaml")
	proposal := &config.Proposal{
		SchemaVersion: 1,
		ID:            id,
		Status:        "pending",
		Type:          "setting",
		Action:        "modify",
		Target:        targetPath,
		Rationale:     fmt.Sprintf("Set shared workflow preference %s to %q (was %q)", key, value, currentVal),
		Content:       fmt.Sprintf("%s: %s\n", key, value),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:     "workflow prefs set-shared",
	}
	if err := config.SaveProposal(proposal, config.ProposalPath(id)); err != nil {
		return fmt.Errorf("save proposal: %w", err)
	}
	ui.Info(fmt.Sprintf("Proposal %s created for shared preference change.", id))
	ui.Info("Run 'dot-agents review' to approve and apply.")
	return nil
}

// ── Wave 5: Graph bridge types ─────────────────────────────────────────────────

// ContextMapping maps a repo concept to a graph query scope.
type ContextMapping struct {
	RepoScope  string `json:"repo_scope" yaml:"repo_scope"`
	GraphScope string `json:"graph_scope" yaml:"graph_scope"`
	Intent     string `json:"intent" yaml:"intent"`
}

// GraphBridgeConfig is the schema for .agents/workflow/graph-bridge.yaml.
type GraphBridgeConfig struct {
	SchemaVersion   int              `json:"schema_version" yaml:"schema_version"`
	Enabled         bool             `json:"enabled" yaml:"enabled"`
	GraphHome       string           `json:"graph_home" yaml:"graph_home"`
	AllowedIntents  []string         `json:"allowed_intents" yaml:"allowed_intents"`
	ContextMappings []ContextMapping `json:"context_mappings" yaml:"context_mappings"`
}

var validWorkflowBridgeIntents = map[string]bool{
	"plan_context":    true,
	"decision_lookup": true,
	"entity_context":  true,
	"workflow_memory": true,
	"contradictions":  true,
}

func isValidWorkflowBridgeIntent(intent string) bool { return validWorkflowBridgeIntents[intent] }

var workflowGraphCodeBridgeIntents = map[string]bool{
	"symbol_lookup":     true,
	"impact_radius":     true,
	"change_analysis":   true,
	"tests_for":         true,
	"callers_of":        true,
	"callees_of":        true,
	"community_context": true,
	"symbol_decisions":  true,
	"decision_symbols":  true,
}

func isWorkflowGraphCodeBridgeIntent(intent string) bool {
	return workflowGraphCodeBridgeIntents[intent]
}

// workflowDotAgentsExe resolves the path to the dot-agents binary for nested CLI invocations
// (e.g. workflow graph query forwarding to kg bridge). Tests replace this with a freshly built
// binary because os.Executable() in `go test` points at the test harness, not the real CLI.
var workflowDotAgentsExe = func() (string, error) {
	return os.Executable()
}

func runWorkflowGraphQueryViaKGBridge(projectPath, intent string, queryArgs []string) error {
	exe, err := workflowDotAgentsExe()
	if err != nil {
		return fmt.Errorf("resolve dot-agents executable: %w", err)
	}
	argv := []string{"kg", "bridge", "query", "--intent", intent}
	argv = append(argv, queryArgs...)
	if deps.Flags.JSON() {
		argv = append([]string{"--json"}, argv...)
	}
	cmd := exec.Command(exe, argv...)
	cmd.Dir = projectPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kg bridge query (via workflow graph query): %w", err)
	}
	return nil
}

// loadGraphBridgeConfig reads .agents/workflow/graph-bridge.yaml. If absent, bridge is disabled.
func loadGraphBridgeConfig(projectPath string) (*GraphBridgeConfig, error) {
	p := filepath.Join(projectPath, ".agents", "workflow", "graph-bridge.yaml")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &GraphBridgeConfig{Enabled: false}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg GraphBridgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse graph-bridge.yaml: %w", err)
	}
	return &cfg, nil
}

// ── Wave 5: Bridge query contract ────────────────────────────────────────────

// GraphBridgeQuery is the input to a bridge query.
type GraphBridgeQuery struct {
	Intent  string `json:"intent"`
	Project string `json:"project"`
	Scope   string `json:"scope,omitempty"`
	Query   string `json:"query"`
}

// GraphBridgeResult is one result item.
type GraphBridgeResult struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Path       string   `json:"path"`
	SourceRefs []string `json:"source_refs,omitempty"`
}

// GraphBridgeResponse is the normalized response envelope.
type GraphBridgeResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	Intent        string              `json:"intent"`
	Query         string              `json:"query"`
	Results       []GraphBridgeResult `json:"results"`
	Warnings      []string            `json:"warnings"`
	Provider      string              `json:"provider"`
	Timestamp     string              `json:"timestamp"`
}

// ── Wave 5: Graph bridge adapter ─────────────────────────────────────────────

// GraphBridgeAdapter is the interface for bridge backends.
type GraphBridgeAdapter interface {
	Query(query GraphBridgeQuery) (GraphBridgeResponse, error)
	Health() (GraphBridgeHealth, error)
}

// GraphBridgeHealth is the adapter availability and last-query status.
type GraphBridgeHealth struct {
	SchemaVersion    int      `json:"schema_version"`
	Timestamp        string   `json:"timestamp"`
	AdapterAvailable bool     `json:"adapter_available"`
	GraphHomeExists  bool     `json:"graph_home_exists"`
	NoteCount        int      `json:"note_count"`
	LastQueryTime    string   `json:"last_query_time,omitempty"`
	LastQueryStatus  string   `json:"last_query_status,omitempty"`
	Status           string   `json:"status"` // healthy|warn|error
	Warnings         []string `json:"warnings,omitempty"`
}

// writeGraphBridgeHealth writes health to ~/.agents/context/<project>/graph-bridge-health.json.
func writeGraphBridgeHealth(project string, health GraphBridgeHealth) error {
	dir := config.ProjectContextDir(project)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "graph-bridge-health.json"), data, 0644)
}

// readGraphBridgeHealth reads the cached health snapshot.
func readGraphBridgeHealth(project string) (*GraphBridgeHealth, error) {
	p := filepath.Join(config.ProjectContextDir(project), "graph-bridge-health.json")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h GraphBridgeHealth
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// LocalGraphAdapter scans KG_HOME filesystem using simple string matching.
// This is intentionally independent of the kg package — agents use it without
// needing the kg subcommand installed.
type LocalGraphAdapter struct {
	graphHome  string
	lastQuery  string
	lastStatus string
}

func NewLocalGraphAdapter(graphHome string) *LocalGraphAdapter {
	return &LocalGraphAdapter{graphHome: graphHome}
}

func (a *LocalGraphAdapter) Health() (GraphBridgeHealth, error) {
	h := GraphBridgeHealth{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	info, err := os.Stat(a.graphHome)
	h.GraphHomeExists = err == nil && info.IsDir()
	configExists := false
	if _, err := os.Stat(filepath.Join(a.graphHome, "self", "config.yaml")); err == nil {
		configExists = true
	}
	h.AdapterAvailable = h.GraphHomeExists && configExists
	if !h.AdapterAvailable {
		h.Status = "warn"
		h.Warnings = append(h.Warnings, fmt.Sprintf("graph not initialized at %s", a.graphHome))
		return h, nil
	}
	// Count notes
	noteDirs := []string{"sources", "entities", "concepts", "synthesis", "decisions", "repos", "sessions"}
	for _, sub := range noteDirs {
		entries, err := os.ReadDir(filepath.Join(a.graphHome, "notes", sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				h.NoteCount++
			}
		}
	}
	h.LastQueryTime = a.lastQuery
	h.LastQueryStatus = a.lastStatus
	h.Status = "healthy"
	return h, nil
}

func (a *LocalGraphAdapter) Query(query GraphBridgeQuery) (GraphBridgeResponse, error) {
	resp := GraphBridgeResponse{
		SchemaVersion: 1,
		Intent:        query.Intent,
		Query:         query.Query,
		Provider:      "local-graph",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Results:       []GraphBridgeResult{},
	}

	// Map bridge intents to note types
	noteTypes := map[string][]string{
		"plan_context":    {"decisions", "synthesis"},
		"decision_lookup": {"decisions"},
		"entity_context":  {"entities"},
		"workflow_memory": {"sources", "sessions"},
		"contradictions":  {"decisions"},
	}
	subdirs, ok := noteTypes[query.Intent]
	if !ok {
		return resp, fmt.Errorf("unsupported bridge intent: %s", query.Intent)
	}

	seen := make(map[string]bool)
	q := strings.ToLower(query.Query)
	for _, sub := range subdirs {
		dir := filepath.Join(a.graphHome, "notes", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			content := strings.ToLower(string(data))
			if q == "" || strings.Contains(content, q) {
				// Parse frontmatter for id/title/summary
				id, title, summary, srcRefs := parseNoteMetadata(string(data))
				if id == "" {
					id = strings.TrimSuffix(e.Name(), ".md")
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				resp.Results = append(resp.Results, GraphBridgeResult{
					ID:         id,
					Type:       strings.TrimSuffix(sub, "s"),
					Title:      title,
					Summary:    summary,
					Path:       filepath.Join("notes", sub, e.Name()),
					SourceRefs: srcRefs,
				})
				if len(resp.Results) >= 10 {
					break
				}
			}
		}
	}

	a.lastQuery = time.Now().UTC().Format(time.RFC3339)
	a.lastStatus = "ok"
	return resp, nil
}

// parseNoteMetadata extracts id/title/summary/source_refs from YAML frontmatter.
func parseNoteMetadata(content string) (id, title, summary string, sourceRefs []string) {
	if !strings.HasPrefix(content, "---") {
		return
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return
	}
	fm := rest[:idx]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "id: "); ok {
			id = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "title: "); ok {
			title = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "summary: "); ok {
			summary = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "- "); ok && strings.Contains(fm, "source_refs:") {
			sourceRefs = append(sourceRefs, strings.Trim(after, "\"'"))
		}
	}
	return
}

// ── Wave 5: workflow graph subcommands ────────────────────────────────────────

func runWorkflowGraphQuery(cmd *cobra.Command, args []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return deps.UsageError(
			"`--intent` is required",
			"Workflow graph queries require a bridge intent such as `plan_context` or `decision_lookup`.",
		)
	}
	scope, _ := cmd.Flags().GetString("scope")
	if isWorkflowGraphCodeBridgeIntent(intent) {
		return runWorkflowGraphQueryViaKGBridge(projectPath, intent, args)
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}
	if !cfg.Enabled {
		return deps.ErrorWithHints(
			"graph bridge not configured",
			"Create `.agents/workflow/graph-bridge.yaml` with `enabled: true` to enable workflow graph queries.",
		)
	}

	if !isValidWorkflowBridgeIntent(intent) {
		return deps.ErrorWithHints(
			fmt.Sprintf("unknown intent %q", intent),
			"Valid workflow bridge intents: `plan_context`, `decision_lookup`, `entity_context`, `workflow_memory`, `contradictions`.",
		)
	}
	// Validate against allowed intents
	allowed := cfg.AllowedIntents
	if len(allowed) > 0 {
		ok := false
		for _, a := range allowed {
			if a == intent {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("intent %q not in allowed_intents for this repo", intent)
		}
	}

	query := strings.Join(args, " ")
	graphHome := cfg.GraphHome
	if graphHome == "" {
		home, _ := os.UserHomeDir()
		graphHome = filepath.Join(home, "knowledge-graph")
	}
	adapter := NewLocalGraphAdapter(graphHome)
	resp, err := adapter.Query(GraphBridgeQuery{
		Intent:  intent,
		Project: filepath.Base(projectPath),
		Scope:   scope,
		Query:   query,
	})
	if err != nil {
		return err
	}

	// Update health
	health, _ := adapter.Health()
	health.LastQueryTime = time.Now().UTC().Format(time.RFC3339)
	health.LastQueryStatus = "ok"
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if deps.Flags.JSON() {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Graph Query: %s  [%s]", intent, query))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, r.Summary))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

func runWorkflowGraphHealth(_ *cobra.Command, _ []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}

	graphHome := cfg.GraphHome
	if graphHome == "" {
		home, _ := os.UserHomeDir()
		graphHome = filepath.Join(home, "knowledge-graph")
	}
	adapter := NewLocalGraphAdapter(graphHome)
	health, err := adapter.Health()
	if err != nil {
		return err
	}
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if deps.Flags.JSON() {
		data, _ := json.MarshalIndent(health, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	badge := ui.ColorText(ui.Green, health.Status)
	if health.Status != "healthy" {
		badge = ui.ColorText(ui.Yellow, health.Status)
	}
	ui.Header(fmt.Sprintf("Graph Bridge Health  [%s]", badge))
	ui.Info(fmt.Sprintf("  Graph home: %s", graphHome))
	ui.Info(fmt.Sprintf("  Adapter available: %v", health.AdapterAvailable))
	ui.Info(fmt.Sprintf("  Notes: %d", health.NoteCount))
	ui.Info(fmt.Sprintf("  Bridge enabled: %v", cfg.Enabled))
	if !cfg.Enabled {
		ui.Warn("Bridge not enabled — create .agents/workflow/graph-bridge.yaml to enable")
	}
	for _, w := range health.Warnings {
		ui.Warn(w)
	}
	return nil
}

// ── Wave 6: Delegation & Merge-back ──────────────────────────────────────────

// CoordinationIntent is transport-neutral coordination between parent and delegate.
// Stored as enum field in DelegationContract, never as chat syntax or @mentions.
type CoordinationIntent string

const (
	CoordinationIntentNone             CoordinationIntent = ""
	CoordinationIntentStatusRequest    CoordinationIntent = "status_request"
	CoordinationIntentReviewRequest    CoordinationIntent = "review_request"
	CoordinationIntentEscalationNotice CoordinationIntent = "escalation_notice"
	CoordinationIntentAck              CoordinationIntent = "ack"
)

var validCoordinationIntents = map[CoordinationIntent]bool{
	CoordinationIntentNone:             true,
	CoordinationIntentStatusRequest:    true,
	CoordinationIntentReviewRequest:    true,
	CoordinationIntentEscalationNotice: true,
	CoordinationIntentAck:              true,
}

// DelegationContract declares a bounded task delegation from parent to sub-agent.
// Stored at .agents/active/delegation/<task-id>.yaml
type DelegationContract struct {
	SchemaVersion            int                `json:"schema_version" yaml:"schema_version"`
	ID                       string             `json:"id" yaml:"id"`
	ParentPlanID             string             `json:"parent_plan_id" yaml:"parent_plan_id"`
	ParentTaskID             string             `json:"parent_task_id" yaml:"parent_task_id"`
	Title                    string             `json:"title" yaml:"title"`
	Summary                  string             `json:"summary" yaml:"summary"`
	WriteScope               []string           `json:"write_scope" yaml:"write_scope"` // immutable after creation
	SuccessCriteria          string             `json:"success_criteria" yaml:"success_criteria"`
	VerificationExpectations string             `json:"verification_expectations" yaml:"verification_expectations"`
	MayMutateWorkflowState   bool               `json:"may_mutate_workflow_state" yaml:"may_mutate_workflow_state"`
	Owner                    string             `json:"owner" yaml:"owner"`   // delegate agent identity
	Status                   string             `json:"status" yaml:"status"` // pending|active|completed|failed|cancelled
	PendingIntent            CoordinationIntent `json:"pending_intent,omitempty" yaml:"pending_intent,omitempty"`
	CreatedAt                string             `json:"created_at" yaml:"created_at"`
	UpdatedAt                string             `json:"updated_at" yaml:"updated_at"`
}

var validDelegationStatuses = map[string]bool{
	"pending": true, "active": true, "completed": true, "failed": true, "cancelled": true,
}

func isValidDelegationStatus(s string) bool { return validDelegationStatuses[s] }

func delegationDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "delegation")
}

func mergeBackDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "merge-back")
}

func loadDelegationContract(projectPath, taskID string) (*DelegationContract, error) {
	path := filepath.Join(delegationDir(projectPath), taskID+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c DelegationContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse delegation contract %s: %w", taskID, err)
	}
	return &c, nil
}

func saveDelegationContract(projectPath string, c *DelegationContract) error {
	dir := delegationDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, c.ParentTaskID+".yaml"), data, 0644)
}

func listDelegationContracts(projectPath string) ([]DelegationContract, error) {
	dir := delegationDir(projectPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var contracts []DelegationContract
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		taskID := strings.TrimSuffix(e.Name(), ".yaml")
		c, err := loadDelegationContract(projectPath, taskID)
		if err != nil {
			continue // skip unreadable contracts
		}
		contracts = append(contracts, *c)
	}
	return contracts, nil
}

// ── Write-scope overlap detection (Wave 6 Step 2) ────────────────────────────

// writeScopeOverlaps returns conflict descriptions for any overlapping write scopes
// between active delegations and the proposed new scope.
// Detection strategy: prefix containment covers 90%+ of real cases (per RFC).
// Full glob intersection is deferred.
func writeScopeOverlaps(existing []DelegationContract, newScope []string, excludeTaskID string) []string {
	var conflicts []string
	for _, c := range existing {
		if c.Status != "pending" && c.Status != "active" {
			continue // only check live delegations
		}
		if c.ParentTaskID == excludeTaskID {
			continue
		}
		for _, np := range newScope {
			for _, ep := range c.WriteScope {
				if scopePathsOverlap(np, ep) {
					conflicts = append(conflicts, fmt.Sprintf(
						"task %s has overlapping write scope: %q overlaps %q (existing delegation for task %s)",
						excludeTaskID, np, ep, c.ParentTaskID,
					))
				}
			}
		}
	}
	return conflicts
}

// scopePathsOverlap returns true if path a and path b overlap.
// Two paths overlap if one is a prefix of the other, or they are identical.
// This handles the 90%+ case of disjoint directory trees.
func scopePathsOverlap(a, b string) bool {
	// Normalize: ensure directory paths end with /
	na := filepath.ToSlash(filepath.Clean(a))
	nb := filepath.ToSlash(filepath.Clean(b))
	// Identical
	if na == nb {
		return true
	}
	// a is prefix of b: commands/ vs commands/workflow.go
	if strings.HasPrefix(nb, na+"/") || strings.HasPrefix(na, nb+"/") {
		return true
	}
	return false
}

// ── MergeBackSummary (Wave 6 Step 3) ─────────────────────────────────────────

// MergeBackSummary is produced by the delegate and consumed by the parent.
// Stored at .agents/active/merge-back/<task-id>.md with YAML frontmatter.
type MergeBackSummary struct {
	SchemaVersion       int                   `json:"schema_version" yaml:"schema_version"`
	TaskID              string                `json:"task_id" yaml:"task_id"`
	ParentPlanID        string                `json:"parent_plan_id" yaml:"parent_plan_id"`
	Title               string                `json:"title" yaml:"title"`
	Summary             string                `json:"summary" yaml:"summary"`
	FilesChanged        []string              `json:"files_changed" yaml:"files_changed"`
	VerificationResult  MergeBackVerification `json:"verification_result" yaml:"verification_result"`
	IntegrationNotes    string                `json:"integration_notes" yaml:"integration_notes"`
	BlockersEncountered []string              `json:"blockers_encountered,omitempty" yaml:"blockers_encountered,omitempty"`
	CreatedAt           string                `json:"created_at" yaml:"created_at"`
}

// MergeBackVerification captures the delegate's self-reported verification.
type MergeBackVerification struct {
	Status  string `json:"status" yaml:"status"` // pass|fail|partial|unknown
	Summary string `json:"summary" yaml:"summary"`
}

func saveMergeBack(projectPath string, s *MergeBackSummary) error {
	dir := mergeBackDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Render as markdown with YAML frontmatter
	frontmatter, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("---\n%s---\n\n## Summary\n\n%s\n\n## Integration Notes\n\n%s\n",
		string(frontmatter), s.Summary, s.IntegrationNotes)
	return os.WriteFile(filepath.Join(dir, s.TaskID+".md"), []byte(content), 0644)
}

func loadMergeBack(projectPath, taskID string) (*MergeBackSummary, error) {
	path := filepath.Join(mergeBackDir(projectPath), taskID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Extract YAML frontmatter
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("merge-back %s: missing frontmatter", taskID)
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("merge-back %s: unterminated frontmatter", taskID)
	}
	var s MergeBackSummary
	if err := yaml.Unmarshal([]byte(rest[:end]), &s); err != nil {
		return nil, fmt.Errorf("parse merge-back %s: %w", taskID, err)
	}
	return &s, nil
}

// ── Fold-back (Phase 6) ───────────────────────────────────────────────────────

func foldBackDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "fold-back")
}

func appendFoldBackBullet(notes, observation string) string {
	notes = strings.TrimRight(notes, "\n")
	line := "- " + observation
	if notes == "" {
		return line
	}
	return notes + "\n" + line
}

// setFoldBackTaggedNote replaces or inserts a single markdown line tagged with (fb:<slug>)
// so repeated create/update with the same slug does not duplicate bullets in TASKS or plan summary.
func setFoldBackTaggedNote(notes, slug, observation string) string {
	tag := "- (fb:" + slug + ") "
	obs := strings.TrimSpace(observation)
	raw := strings.TrimRight(notes, "\n")
	if raw == "" {
		return tag + obs
	}
	lines := strings.Split(raw, "\n")
	var kept []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, tag) {
			continue
		}
		kept = append(kept, ln)
	}
	out := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	newLine := tag + obs
	if out == "" {
		return newLine
	}
	return out + "\n" + newLine
}

func validateFoldBackSlug(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("slug must not be empty")
	}
	if len(s) > 200 {
		return fmt.Errorf("slug exceeds maximum length (200)")
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return fmt.Errorf("slug contains invalid character %q", r)
		}
	}
	if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return fmt.Errorf("slug must not start or end with '-'")
	}
	return nil
}

func foldBackArtifactFile(projectPath, id string) string {
	return filepath.Join(foldBackDir(projectPath), id+".yaml")
}

func loadFoldBackArtifactByID(projectPath, id string) (foldBackArtifact, error) {
	data, err := os.ReadFile(foldBackArtifactFile(projectPath, id))
	if err != nil {
		return foldBackArtifact{}, err
	}
	var a foldBackArtifact
	if err := yaml.Unmarshal(data, &a); err != nil {
		return foldBackArtifact{}, err
	}
	return a, nil
}

func proposalAbsPathFromRoutedTo(routed string) (string, error) {
	if !strings.HasPrefix(routed, "proposal:") {
		return "", fmt.Errorf("not a proposal route: %q", routed)
	}
	name := strings.TrimPrefix(routed, "proposal:")
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid proposal name in route %q", routed)
	}
	return filepath.Join(config.AgentsHome(), "proposals", name), nil
}

func readFoldBackProposalFile(path string) (foldBackProposalFrontmatter, string, error) {
	var zero foldBackProposalFrontmatter
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, "", err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return zero, "", fmt.Errorf("proposal %s: missing frontmatter", path)
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return zero, "", fmt.Errorf("proposal %s: unterminated frontmatter", path)
	}
	var fm foldBackProposalFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return zero, "", err
	}
	body := strings.TrimSpace(rest[end+5:])
	return fm, body, nil
}

func writeFoldBackArtifact(projectPath string, artifact foldBackArtifact) error {
	dir := foldBackDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&artifact)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, artifact.ID+".yaml"), data, 0644)
}

func writeFoldBackProposalFile(path string, fm foldBackProposalFrontmatter, body string) error {
	header, err := yaml.Marshal(fm)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("---\n%s---\n\n%s\n", string(header), body)
	return os.WriteFile(path, []byte(content), 0644)
}

func runWorkflowFoldBackCreate(cmd *cobra.Command, _ []string) error {
	return runWorkflowFoldBackUpsert(cmd, false)
}

func runWorkflowFoldBackUpdate(cmd *cobra.Command, _ []string) error {
	return runWorkflowFoldBackUpsert(cmd, true)
}

func runWorkflowFoldBackUpsert(cmd *cobra.Command, updateOnly bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	observation, _ := cmd.Flags().GetString("observation")
	propose, _ := cmd.Flags().GetBool("propose")
	slug, _ := cmd.Flags().GetString("slug")
	slug = strings.TrimSpace(slug)

	if strings.TrimSpace(observation) == "" {
		return fmt.Errorf("observation text is required")
	}
	if updateOnly && slug == "" {
		return fmt.Errorf("--slug is required for fold-back update")
	}
	if slug != "" {
		if err := validateFoldBackSlug(slug); err != nil {
			return err
		}
	}

	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %s not found: %w", planID, err)
	}

	now := time.Now().UTC()
	createdAt := now.Format(time.RFC3339)
	ts := now.UnixNano()
	foldID := fmt.Sprintf("fold-%d", ts)

	var prior *foldBackArtifact
	priorExists := false
	if slug != "" {
		st, statErr := os.Stat(foldBackArtifactFile(project.Path, slug))
		if statErr == nil && !st.IsDir() {
			a, loadErr := loadFoldBackArtifactByID(project.Path, slug)
			if loadErr != nil {
				return fmt.Errorf("load fold-back %q: %w", slug, loadErr)
			}
			prior = &a
			priorExists = true
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
	}

	if updateOnly && !priorExists {
		return fmt.Errorf("no fold-back artifact with slug %q", slug)
	}

	if priorExists {
		if prior.PlanID != planID {
			return fmt.Errorf("fold-back %q belongs to plan %q, not %q", slug, prior.PlanID, planID)
		}
		if propose {
			return fmt.Errorf("--propose is not valid when updating an existing slug-scoped fold-back")
		}
		if prior.Classification == "small" {
			if prior.TaskID != "" {
				if strings.TrimSpace(taskID) == "" {
					return fmt.Errorf("fold-back %q is task-scoped (%s); pass --task %s", slug, prior.TaskID, prior.TaskID)
				}
				if taskID != prior.TaskID {
					return fmt.Errorf("--task %q does not match fold-back scope (expected %q)", taskID, prior.TaskID)
				}
			} else if strings.TrimSpace(taskID) != "" {
				return fmt.Errorf("fold-back %q is plan-scoped; omit --task", slug)
			}
		}
	}

	if priorExists && prior.Classification == "small" && propose {
		return fmt.Errorf("cannot use --propose for slug %q: existing artifact is inline (small)", slug)
	}

	artifact := foldBackArtifact{
		SchemaVersion: 1,
		PlanID:        planID,
		Observation:   observation,
		CreatedAt:     createdAt,
	}
	if priorExists {
		artifact.ID = prior.ID
		artifact.CreatedAt = prior.CreatedAt
	} else if slug != "" {
		artifact.ID = slug
	} else {
		artifact.ID = foldID
	}

	updated := priorExists

	switch {
	case priorExists && prior.Classification == "proposal":
		artifact.Classification = "proposal"
		artifact.TaskID = prior.TaskID
		artifact.RoutedTo = prior.RoutedTo
		propPath, err := proposalAbsPathFromRoutedTo(prior.RoutedTo)
		if err != nil {
			return err
		}
		fm, _, err := readFoldBackProposalFile(propPath)
		if err != nil {
			return fmt.Errorf("read proposal %s: %w", propPath, err)
		}
		fm.Observation = observation
		if err := writeFoldBackProposalFile(propPath, fm, observation); err != nil {
			return err
		}

	case priorExists && prior.Classification == "small":
		artifact.Classification = "small"
		artifact.TaskID = prior.TaskID
		if prior.TaskID != "" {
			tf, err := loadCanonicalTasks(project.Path, planID)
			if err != nil {
				return fmt.Errorf("load tasks for plan %s: %w", planID, err)
			}
			var found bool
			for i := range tf.Tasks {
				if tf.Tasks[i].ID == prior.TaskID {
					tf.Tasks[i].Notes = setFoldBackTaggedNote(tf.Tasks[i].Notes, slug, observation)
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("task %s not found in plan %s", prior.TaskID, planID)
			}
			if err := saveCanonicalTasks(project.Path, tf); err != nil {
				return err
			}
			artifact.RoutedTo = fmt.Sprintf("task_note:%s/%s", planID, prior.TaskID)
		} else {
			plan, err := loadCanonicalPlan(project.Path, planID)
			if err != nil {
				return err
			}
			plan.Summary = setFoldBackTaggedNote(plan.Summary, slug, observation)
			plan.UpdatedAt = createdAt
			if err := saveCanonicalPlan(project.Path, plan); err != nil {
				return err
			}
			artifact.TaskID = ""
			artifact.RoutedTo = fmt.Sprintf("plan_summary:%s", planID)
		}

	case !priorExists && propose:
		artifact.Classification = "proposal"
		artifact.TaskID = strings.TrimSpace(taskID)
		proposalName := fmt.Sprintf("obs-%d.md", ts)
		if slug != "" {
			proposalName = fmt.Sprintf("obs-%s.md", slug)
		}
		proposalsDir := filepath.Join(config.AgentsHome(), "proposals")
		if err := os.MkdirAll(proposalsDir, 0755); err != nil {
			return err
		}
		proposalPath := filepath.Join(proposalsDir, proposalName)
		fm := foldBackProposalFrontmatter{
			Title:       fmt.Sprintf("Fold-back: %s", planID),
			Observation: observation,
			PlanID:      planID,
			CreatedAt:   createdAt,
		}
		if artifact.TaskID != "" {
			fm.TaskID = artifact.TaskID
		}
		if err := writeFoldBackProposalFile(proposalPath, fm, observation); err != nil {
			return err
		}
		artifact.RoutedTo = "proposal:" + proposalName

	case !priorExists && slug != "" && strings.TrimSpace(taskID) != "":
		artifact.Classification = "small"
		artifact.TaskID = taskID
		tf, err := loadCanonicalTasks(project.Path, planID)
		if err != nil {
			return fmt.Errorf("load tasks for plan %s: %w", planID, err)
		}
		var found bool
		for i := range tf.Tasks {
			if tf.Tasks[i].ID == taskID {
				tf.Tasks[i].Notes = setFoldBackTaggedNote(tf.Tasks[i].Notes, slug, observation)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("task %s not found in plan %s", taskID, planID)
		}
		if err := saveCanonicalTasks(project.Path, tf); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf("task_note:%s/%s", planID, taskID)

	case !priorExists && slug != "" && strings.TrimSpace(taskID) == "":
		artifact.Classification = "small"
		artifact.TaskID = ""
		plan, err := loadCanonicalPlan(project.Path, planID)
		if err != nil {
			return err
		}
		plan.Summary = setFoldBackTaggedNote(plan.Summary, slug, observation)
		plan.UpdatedAt = createdAt
		if err := saveCanonicalPlan(project.Path, plan); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf("plan_summary:%s", planID)

	case !priorExists && !propose && strings.TrimSpace(taskID) != "":
		artifact.Classification = "small"
		artifact.TaskID = taskID
		tf, err := loadCanonicalTasks(project.Path, planID)
		if err != nil {
			return fmt.Errorf("load tasks for plan %s: %w", planID, err)
		}
		var found bool
		for i := range tf.Tasks {
			if tf.Tasks[i].ID == taskID {
				tf.Tasks[i].Notes = appendFoldBackBullet(tf.Tasks[i].Notes, observation)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("task %s not found in plan %s", taskID, planID)
		}
		if err := saveCanonicalTasks(project.Path, tf); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf("task_note:%s/%s", planID, taskID)

	case !priorExists && !propose && strings.TrimSpace(taskID) == "":
		artifact.Classification = "small"
		artifact.TaskID = ""
		plan, err := loadCanonicalPlan(project.Path, planID)
		if err != nil {
			return err
		}
		plan.Summary = appendFoldBackBullet(plan.Summary, observation)
		plan.UpdatedAt = createdAt
		if err := saveCanonicalPlan(project.Path, plan); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf("plan_summary:%s", planID)

	default:
		return fmt.Errorf("internal fold-back routing error (slug=%q propose=%v priorExists=%v)", slug, propose, priorExists)
	}

	if err := writeFoldBackArtifact(project.Path, artifact); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(artifact)
	}

	verb := "Recorded"
	if updated {
		verb = "Updated"
	}
	fmt.Fprintf(out, "  %s fold-back %s (%s) → %s\n", verb, artifact.ID, artifact.Classification, artifact.RoutedTo)
	return nil
}

func runWorkflowFoldBackList(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	planFilter, _ := cmd.Flags().GetString("plan")
	out := cmd.OutOrStdout()

	dir := foldBackDir(project.Path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if deps.Flags.JSON() {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode([]foldBackArtifact{})
			}
			fmt.Fprintf(out, "  %s\n", "No fold-back observations recorded.")
			return nil
		}
		return err
	}

	var artifacts []foldBackArtifact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		var a foldBackArtifact
		if err := yaml.Unmarshal(data, &a); err != nil {
			return fmt.Errorf("parse fold-back %s: %w", e.Name(), err)
		}
		if planFilter != "" && a.PlanID != planFilter {
			continue
		}
		artifacts = append(artifacts, a)
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].CreatedAt < artifacts[j].CreatedAt
	})

	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(artifacts)
	}

	if len(artifacts) == 0 {
		fmt.Fprintf(out, "  %s\n", "No fold-back observations recorded.")
		return nil
	}

	fmt.Fprintf(out, ui.ThreeStringPlaceHolder, ui.Bold, "Fold-back observations", ui.Reset)
	fmt.Fprintln(out, strings.Repeat("─", 40))
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPLAN\tTASK\tCLASSIFICATION\tROUTED-TO\tCREATED-AT")
	for _, a := range artifacts {
		taskCol := a.TaskID
		if taskCol == "" {
			taskCol = "—"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.PlanID, taskCol, a.Classification, a.RoutedTo, a.CreatedAt)
	}
	_ = w.Flush()
	fmt.Fprintln(out)
	return nil
}

// ensureTaskVerificationDir creates .agents/active/verification/<task_id>/ before dispatch
// so workers and verifiers have a stable per-task location for artifacts.
func ensureTaskVerificationDir(projectPath, taskID string) error {
	dir := filepath.Join(projectPath, ".agents", "active", "verification", taskID)
	return os.MkdirAll(dir, 0755)
}

func writeScopeImpliesNonTestGo(ws []string) bool {
	for _, rel := range ws {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			return true
		}
	}
	return false
}

func writeScopeHasAdjacentGoTests(projectPath string, ws []string) bool {
	dirs := make(map[string]bool)
	for _, rel := range ws {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if strings.HasSuffix(rel, ".go") {
			dirs[filepath.ToSlash(filepath.Dir(rel))] = true
			continue
		}
		abs := filepath.Join(projectPath, filepath.FromSlash(rel))
		st, err := os.Stat(abs)
		if err == nil && st.IsDir() {
			dirs[rel] = true
		}
	}
	for d := range dirs {
		abs := filepath.Join(projectPath, filepath.FromSlash(d))
		matches, err := filepath.Glob(filepath.Join(abs, "*_test.go"))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

func checkPreVerifierTDDGate(projectPath string, writeScope []string, verificationRequired, skip bool) error {
	if skip || !verificationRequired {
		return nil
	}
	if !writeScopeImpliesNonTestGo(writeScope) {
		return nil
	}
	if writeScopeHasAdjacentGoTests(projectPath, writeScope) {
		return nil
	}
	return fmt.Errorf("pre-verifier TDD gate: verification-required task with Go write_scope needs at least one *_test.go in the same directory (or list a *_test.go path); use --skip-tdd-gate for doc-only or non-Go work")
}

// ── workflow fanout subcommand (Wave 6 Step 5) ───────────────────────────────

func runWorkflowFanout(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	sliceID, _ := cmd.Flags().GetString("slice")
	owner, _ := cmd.Flags().GetString("owner")
	writeScopeCSV, _ := cmd.Flags().GetString("write-scope")
	writeScopeExplicit := cmd.Flags().Changed("write-scope")

	// Validate plan exists
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("plan %s not found: %w", planID, err)
	}

	if sliceID != "" && taskID != "" {
		return fmt.Errorf("provide --slice or --task, not both")
	}

	var writeScope []string
	if sliceID != "" {
		sf, err := loadCanonicalSlices(project.Path, planID)
		if err != nil {
			return fmt.Errorf("load slices for plan %s: %w", planID, err)
		}
		var found *CanonicalSlice
		for i := range sf.Slices {
			if sf.Slices[i].ID == sliceID {
				found = &sf.Slices[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("slice %q not found in plan %s", sliceID, planID)
		}
		if found.Status == "completed" {
			return fmt.Errorf("slice %q is already completed", sliceID)
		}
		taskID = found.ParentTaskID
		if !writeScopeExplicit {
			writeScope = append(writeScope, found.WriteScope...)
		}
	}
	if taskID == "" {
		return fmt.Errorf("provide --slice <slice-id> or --task <task-id>")
	}

	// Validate task exists in plan
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %s not found: %w", planID, err)
	}
	var targetTask *CanonicalTask
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			targetTask = &tf.Tasks[i]
			break
		}
	}
	if targetTask == nil {
		return fmt.Errorf("task %s not found in plan %s", taskID, planID)
	}
	if targetTask.Status != "pending" && targetTask.Status != "in_progress" {
		return fmt.Errorf("task %s has status %q — only pending or in_progress tasks can be delegated", taskID, targetTask.Status)
	}

	// Check for existing delegation for this task
	if _, err := loadDelegationContract(project.Path, taskID); err == nil {
		return fmt.Errorf("task %s already has an active delegation contract", taskID)
	}

	// Parse write scope
	if writeScopeExplicit {
		writeScope = writeScope[:0]
		for _, p := range strings.Split(writeScopeCSV, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				writeScope = append(writeScope, p)
			}
		}
	}
	// Fall back to task definition's write_scope when not explicitly provided
	if len(writeScope) == 0 && len(targetTask.WriteScope) > 0 {
		writeScope = append([]string(nil), targetTask.WriteScope...)
	}

	if err := ensureTaskVerificationDir(project.Path, taskID); err != nil {
		return fmt.Errorf("prepare verification directory: %w", err)
	}
	skipTDD, _ := cmd.Flags().GetBool("skip-tdd-gate")
	if err := checkPreVerifierTDDGate(project.Path, writeScope, targetTask.VerificationRequired, skipTDD); err != nil {
		return err
	}

	// Check write-scope overlap
	existing, err := listDelegationContracts(project.Path)
	if err != nil {
		return fmt.Errorf("list delegations: %w", err)
	}
	if conflicts := writeScopeOverlaps(existing, writeScope, taskID); len(conflicts) > 0 {
		for _, c := range conflicts {
			ui.Warn(c)
		}
		return fmt.Errorf("delegation rejected: write scope overlaps with existing active delegation(s)")
	}

	// Create delegation contract + Phase 8 bundle (schemas/workflow-delegation-bundle.schema.json)
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion:   1,
		ID:              fmt.Sprintf("del-%s-%d", taskID, time.Now().Unix()),
		ParentPlanID:    planID,
		ParentTaskID:    taskID,
		Title:           targetTask.Title,
		Summary:         fmt.Sprintf("Delegated from plan %s", plan.Title),
		WriteScope:      writeScope,
		SuccessCriteria: targetTask.Notes,
		Owner:           owner,
		Status:          "active",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	bundle, err := buildDelegationBundleForFanout(project.Path, cmd, planID, taskID, sliceID, plan, targetTask, contract, writeScope, now)
	if err != nil {
		return err
	}
	contractPath := filepath.Join(delegationDir(project.Path), taskID+".yaml")
	if err := saveDelegationContract(project.Path, contract); err != nil {
		return fmt.Errorf("save delegation contract: %w", err)
	}
	if err := saveDelegationBundle(project.Path, bundle); err != nil {
		_ = os.Remove(contractPath)
		return fmt.Errorf("save delegation bundle: %w", err)
	}

	// Advance task to in_progress
	if targetTask.Status == "pending" {
		targetTask.Status = "in_progress"
		if err := saveCanonicalTasks(project.Path, tf); err != nil {
			ui.Warn(fmt.Sprintf("delegation created but failed to advance task status: %v", err))
		}
	}

	ui.SuccessBox(
		fmt.Sprintf("Delegation created for task %s", taskID),
		fmt.Sprintf("Contract: .agents/active/delegation/%s.yaml", taskID),
		fmt.Sprintf("Bundle: .agents/active/delegation-bundles/%s.yaml", contract.ID),
		fmt.Sprintf("Write scope: %s", strings.Join(writeScope, ", ")),
	)
	return nil
}

// ── workflow merge-back subcommand (Wave 6 Step 6) ───────────────────────────

func runWorkflowMergeBack(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	summary, _ := cmd.Flags().GetString("summary")
	verificationStatus, _ := cmd.Flags().GetString("verification-status")
	integrationNotes, _ := cmd.Flags().GetString("integration-notes")

	if !isValidVerificationStatus(verificationStatus) {
		return fmt.Errorf("invalid verification status %q (expected pass, fail, partial, or unknown)", verificationStatus)
	}

	// Load delegation contract
	contract, err := loadDelegationContract(project.Path, taskID)
	if err != nil {
		return fmt.Errorf("delegation contract for task %s not found: %w", taskID, err)
	}
	if contract.Status == "completed" || contract.Status == "cancelled" {
		return fmt.Errorf("delegation for task %s is already %s", taskID, contract.Status)
	}

	// Collect changed files via git diff
	var filesChanged []string
	gitOut, err := exec.Command("git", "-C", project.Path, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(gitOut)), "\n") {
			if f != "" {
				filesChanged = append(filesChanged, f)
			}
		}
	}

	// Create merge-back summary
	now := time.Now().UTC().Format(time.RFC3339)
	mergeBack := &MergeBackSummary{
		SchemaVersion: 1,
		TaskID:        taskID,
		ParentPlanID:  contract.ParentPlanID,
		Title:         contract.Title,
		Summary:       summary,
		FilesChanged:  filesChanged,
		VerificationResult: MergeBackVerification{
			Status:  verificationStatus,
			Summary: integrationNotes,
		},
		IntegrationNotes: integrationNotes,
		CreatedAt:        now,
	}
	if err := saveMergeBack(project.Path, mergeBack); err != nil {
		return fmt.Errorf("save merge-back: %w", err)
	}

	verifSummary := strings.TrimSpace(integrationNotes)
	if verifSummary == "" {
		verifSummary = summary
	}
	vrDoc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        taskID,
		ParentPlanID:  contract.ParentPlanID,
		VerifierType:  VerifierTypeMergeBack,
		Status:        verificationStatus,
		Summary:       verifSummary,
		RecordedAt:    now,
		DelegationID:  contract.ID,
		ArtifactPaths: append([]string(nil), filesChanged...),
	}
	if err := writeVerificationResultYAML(project.Path, vrDoc); err != nil {
		return fmt.Errorf("write verification result: %w", err)
	}

	// Update delegation status to completed
	contract.Status = "completed"
	if err := saveDelegationContract(project.Path, contract); err != nil {
		ui.Warn(fmt.Sprintf("merge-back created but failed to update delegation status: %v", err))
	}

	ui.SuccessBox(
		fmt.Sprintf("Merge-back created for task %s", taskID),
		fmt.Sprintf("Artifact: .agents/active/merge-back/%s.md", taskID),
		fmt.Sprintf("Verification result: .agents/active/verification/%s/%s.result.yaml", taskID, VerifierTypeMergeBack),
		"Parent agent should review this artifact before advancing task to completed",
	)
	return nil
}

func delegationBundlesDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "delegation-bundles")
}

func trimStringSlice(in []string) []string {
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// validateInsideProjectPath ensures rel stays within projectPath (no Stat).
func validateInsideProjectPath(projectPath, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	abs := filepath.Join(projectPath, filepath.FromSlash(rel))
	base := filepath.Clean(projectPath)
	cleanAbs := filepath.Clean(abs)
	if cleanAbs != base && !strings.HasPrefix(cleanAbs+string(filepath.Separator), base+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project: %s", rel)
	}
	return rel, nil
}

// validateProjectFileRef ensures rel is inside projectPath and points to an existing regular file.
func validateProjectFileRef(projectPath, rel string) (string, error) {
	rel, err := validateInsideProjectPath(projectPath, rel)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(projectPath, filepath.FromSlash(rel))
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("cannot access %s: %w", rel, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("not a regular file: %s", rel)
	}
	return rel, nil
}

func saveDelegationBundle(projectPath string, b *delegationBundleYAML) error {
	if strings.TrimSpace(b.DelegationID) == "" {
		return fmt.Errorf("delegation bundle: empty delegation_id")
	}
	dir := delegationBundlesDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, b.DelegationID+".yaml"), data, 0644)
}

// agentsrcFanoutDispatch holds verifier dispatch fields read from .agentsrc.json
// (see schemas/agentsrc.schema.json). Parsed separately from config.AgentsRC so
// workflow fanout does not require expanding the typed manifest model.
type agentsrcFanoutDispatch struct {
	VerifierProfiles   map[string]json.RawMessage `json:"verifier_profiles"`
	AppTypeVerifierMap map[string][]string        `json:"app_type_verifier_map"`
}

func loadAgentsrcFanoutDispatch(projectPath string) (*agentsrcFanoutDispatch, error) {
	path := filepath.Join(projectPath, config.AgentsRCFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var d agentsrcFanoutDispatch
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", config.AgentsRCFile, err)
	}
	return &d, nil
}

func splitCommaVerifierList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func validateVerifierProfileRefs(sequence []string, profiles map[string]json.RawMessage) error {
	if len(profiles) == 0 || len(sequence) == 0 {
		return nil
	}
	for _, id := range sequence {
		if _, ok := profiles[id]; !ok {
			return fmt.Errorf("verifier profile %q is not defined under verifier_profiles in .agentsrc.json", id)
		}
	}
	return nil
}

func resolveFanoutVerifierDispatch(projectPath string, cmd *cobra.Command, plan *CanonicalPlan, task *CanonicalTask) (appType string, sequence []string, err error) {
	appType = strings.TrimSpace(task.AppType)
	if appType == "" && plan != nil {
		appType = strings.TrimSpace(plan.DefaultAppType)
	}

	verifierSeqFlag, _ := cmd.Flags().GetString("verifier-sequence")
	verifierSeqFlag = strings.TrimSpace(verifierSeqFlag)
	if verifierSeqFlag != "" {
		sequence = splitCommaVerifierList(verifierSeqFlag)
		if len(sequence) == 0 {
			return "", nil, fmt.Errorf("--verifier-sequence is non-empty but yielded no verifier profile ids")
		}
		d, err := loadAgentsrcFanoutDispatch(projectPath)
		if err != nil {
			return "", nil, err
		}
		var profiles map[string]json.RawMessage
		if d != nil {
			profiles = d.VerifierProfiles
		}
		if err := validateVerifierProfileRefs(sequence, profiles); err != nil {
			return "", nil, err
		}
		return appType, sequence, nil
	}

	d, err := loadAgentsrcFanoutDispatch(projectPath)
	if err != nil {
		return "", nil, err
	}
	if d == nil || len(d.AppTypeVerifierMap) == 0 {
		return appType, nil, nil
	}
	if appType == "" {
		return "", nil, nil
	}
	seq := d.AppTypeVerifierMap[appType]
	if len(seq) == 0 {
		return appType, nil, nil
	}
	sequence = append([]string(nil), seq...)
	if err := validateVerifierProfileRefs(sequence, d.VerifierProfiles); err != nil {
		return "", nil, err
	}
	return appType, sequence, nil
}

func buildDelegationBundleForFanout(
	projectPath string,
	cmd *cobra.Command,
	planID, taskID, sliceID string,
	plan *CanonicalPlan,
	targetTask *CanonicalTask,
	contract *DelegationContract,
	writeScope []string,
	createdAtRFC3339 string,
) (*delegationBundleYAML, error) {
	profile, _ := cmd.Flags().GetString("delegate-profile")
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = defaultDelegateProfile
	}
	feedbackGoal, _ := cmd.Flags().GetString("feedback-goal")
	feedbackGoal = strings.TrimSpace(feedbackGoal)
	if feedbackGoal == "" {
		feedbackGoal = defaultDelegationFeedbackGoal
	}
	validationQueue, _ := cmd.Flags().GetString("validation-queue")
	validationQueue = strings.TrimSpace(validationQueue)
	selReason, _ := cmd.Flags().GetString("selection-reason")

	overlays := trimStringSlice(mustGetStringSlice(cmd, "project-overlay"))
	promptLines := trimStringSlice(mustGetStringSlice(cmd, "prompt"))
	promptFiles := trimStringSlice(mustGetStringSlice(cmd, "prompt-file"))
	contextFiles := trimStringSlice(mustGetStringSlice(cmd, "context-file"))
	scenarioTags := trimStringSlice(mustGetStringSlice(cmd, "scenario-tag"))
	regressionArts := trimStringSlice(mustGetStringSlice(cmd, "regression-artifact"))

	for _, p := range overlays {
		if _, err := validateProjectFileRef(projectPath, p); err != nil {
			return nil, fmt.Errorf("--project-overlay %w", err)
		}
	}
	for _, p := range promptFiles {
		if _, err := validateProjectFileRef(projectPath, p); err != nil {
			return nil, fmt.Errorf("--prompt-file %w", err)
		}
	}
	for _, p := range contextFiles {
		if _, err := validateProjectFileRef(projectPath, p); err != nil {
			return nil, fmt.Errorf("--context-file %w", err)
		}
	}
	if validationQueue != "" {
		if _, err := validateProjectFileRef(projectPath, validationQueue); err != nil {
			return nil, fmt.Errorf("--validation-queue %w", err)
		}
	}
	for _, p := range regressionArts {
		if _, err := validateInsideProjectPath(projectPath, p); err != nil {
			return nil, fmt.Errorf("--regression-artifact %w", err)
		}
	}

	owner := strings.TrimSpace(contract.Owner)
	if owner == "" {
		owner = "unspecified"
	}

	var b delegationBundleYAML
	b.SchemaVersion = 1
	b.DelegationID = contract.ID
	b.PlanID = planID
	b.TaskID = taskID
	if sliceID != "" {
		b.SliceID = sliceID
	}
	b.Owner = owner

	b.Worker.Profile = profile
	if len(overlays) > 0 {
		b.Worker.ProjectOverlayFiles = overlays
	}

	b.Selection = &struct {
		SelectedBy string `yaml:"selected_by"`
		SelectedAt string `yaml:"selected_at"`
		Reason     string `yaml:"reason,omitempty"`
	}{
		SelectedBy: "workflow fanout",
		SelectedAt: createdAtRFC3339,
		Reason:     strings.TrimSpace(selReason),
	}

	b.Scope.WriteScope = append([]string(nil), writeScope...)

	if len(promptLines) > 0 {
		b.Prompt.Inline = promptLines
	}
	if len(promptFiles) > 0 {
		b.Prompt.PromptFiles = promptFiles
	}
	if len(contextFiles) > 0 {
		b.Context.RequiredFiles = contextFiles
	}

	b.Verification.FeedbackGoal = feedbackGoal
	if len(scenarioTags) > 0 {
		b.Verification.ScenarioTags = scenarioTags
	}
	if len(regressionArts) > 0 {
		b.Verification.RegressionArtifacts = regressionArts
	}
	if validationQueue != "" {
		b.Verification.HigherLayerValidationQueue = validationQueue
	}

	appType, verifierSeq, err := resolveFanoutVerifierDispatch(projectPath, cmd, plan, targetTask)
	if err != nil {
		return nil, err
	}
	if appType != "" {
		b.Verification.AppType = appType
	}
	if len(verifierSeq) > 0 {
		b.Verification.VerifierSequence = verifierSeq
	}

	reqNeg, _ := cmd.Flags().GetBool("require-negative-coverage")
	sandbox, _ := cmd.Flags().GetBool("sandbox-mutations")
	if reqNeg || sandbox {
		b.Verification.EvidencePolicy = &struct {
			RequireNegativeCoverage *bool `yaml:"require_negative_coverage,omitempty"`
			ClassificationRequired  *bool `yaml:"classification_required,omitempty"`
			SandboxMutations        *bool `yaml:"sandbox_mutations,omitempty"`
			PrimaryChainMax         *int  `yaml:"primary_chain_max,omitempty"`
		}{}
		if reqNeg {
			v := true
			b.Verification.EvidencePolicy.RequireNegativeCoverage = &v
		}
		if sandbox {
			v := true
			b.Verification.EvidencePolicy.SandboxMutations = &v
		}
	}

	retryMax, _ := cmd.Flags().GetInt("verifier-retry-max")
	if retryMax > 0 {
		if b.Verification.EvidencePolicy == nil {
			b.Verification.EvidencePolicy = &struct {
				RequireNegativeCoverage *bool `yaml:"require_negative_coverage,omitempty"`
				ClassificationRequired  *bool `yaml:"classification_required,omitempty"`
				SandboxMutations        *bool `yaml:"sandbox_mutations,omitempty"`
				PrimaryChainMax         *int  `yaml:"primary_chain_max,omitempty"`
			}{}
		}
		rm := retryMax
		b.Verification.EvidencePolicy.PrimaryChainMax = &rm
	}

	b.Closeout.WorkerMust = []string{"workflow_verify_record", "workflow_checkpoint", "workflow_merge_back"}
	b.Closeout.ParentMust = []string{"workflow_advance", "workflow_delegation_closeout"}

	return &b, nil
}

// mustGetStringSlice reads a StringSlice flag, tolerating missing definitions.
func mustGetStringSlice(cmd *cobra.Command, name string) []string {
	if f := cmd.Flags().Lookup(name); f == nil {
		return nil
	}
	s, err := cmd.Flags().GetStringSlice(name)
	if err != nil {
		return nil
	}
	return s
}

func copyWorkflowArtifact(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func allCanonicalTasksTerminal(tasks []CanonicalTask) bool {
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		switch t.Status {
		case "completed", "cancelled":
			continue
		default:
			return false
		}
	}
	return true
}

// ── workflow delegation closeout (Phase 7) ───────────────────────────────────

func runWorkflowDelegationCloseout(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	decision, _ := cmd.Flags().GetString("decision")
	note, _ := cmd.Flags().GetString("note")

	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "accept" && decision != "reject" {
		return fmt.Errorf(`--decision must be "accept" or "reject"`)
	}

	if _, err := loadMergeBack(project.Path, taskID); err != nil {
		return fmt.Errorf("merge-back for task %s is required before closeout: %w", taskID, err)
	}

	contract, err := loadDelegationContract(project.Path, taskID)
	if err != nil {
		return fmt.Errorf("delegation contract for task %s not found: %w", taskID, err)
	}
	if contract.ParentPlanID != planID {
		return fmt.Errorf("delegation plan_id %q does not match --plan %q", contract.ParentPlanID, planID)
	}
	if contract.Status != "completed" {
		return fmt.Errorf("delegation for task %s must be completed (run merge-back first); status is %q", taskID, contract.Status)
	}

	dateStr := time.Now().UTC().Format("2006-01-02")
	archiveDir := filepath.Join(project.Path, ".agents", "history", planID, "delegate-merge-back-archive", dateStr, taskID)
	mergeBackSrc := filepath.Join(mergeBackDir(project.Path), taskID+".md")
	delegationSrc := filepath.Join(delegationDir(project.Path), taskID+".yaml")

	if err := copyWorkflowArtifact(mergeBackSrc, filepath.Join(archiveDir, "merge-back.md")); err != nil {
		return fmt.Errorf("archive merge-back: %w", err)
	}
	if err := copyWorkflowArtifact(delegationSrc, filepath.Join(archiveDir, "delegation.yaml")); err != nil {
		return fmt.Errorf("archive delegation contract: %w", err)
	}

	closeout := workflowDelegationCloseoutRecord{
		SchemaVersion: 1,
		PlanID:        planID,
		TaskID:        taskID,
		DelegationID:  contract.ID,
		Decision:      decision,
		Note:          strings.TrimSpace(note),
		ClosedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	closeoutData, err := yaml.Marshal(closeout)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(archiveDir, "closeout.yaml"), closeoutData, 0644); err != nil {
		return fmt.Errorf("write closeout record: %w", err)
	}

	_ = os.Remove(mergeBackSrc)
	_ = os.Remove(delegationSrc)
	bundlePath := filepath.Join(delegationBundlesDir(project.Path), contract.ID+".yaml")
	if _, err := os.Stat(bundlePath); err == nil {
		_ = os.Remove(bundlePath)
	}

	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("load canonical tasks: %w", err)
	}
	found := false
	for i := range tf.Tasks {
		if tf.Tasks[i].ID != taskID {
			continue
		}
		found = true
		switch decision {
		case "accept":
			tf.Tasks[i].Status = "completed"
		case "reject":
			tf.Tasks[i].Status = "blocked"
			if closeout.Note != "" {
				tf.Tasks[i].Notes = appendFoldBackBullet(tf.Tasks[i].Notes, fmt.Sprintf("delegation closeout reject: %s", closeout.Note))
			}
		}
		break
	}
	if !found {
		return fmt.Errorf("task %q not found in plan %q", taskID, planID)
	}
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}

	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	plan.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
	if allCanonicalTasksTerminal(tf.Tasks) {
		plan.Status = "completed"
	}
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(closeout)
	}

	ui.SuccessBox(
		fmt.Sprintf("Delegation closeout %s for task %s", decision, taskID),
		fmt.Sprintf("Archived under .agents/history/%s/delegate-merge-back-archive/%s/%s/", planID, dateStr, taskID),
	)
	return nil
}

// ── Wave 7: Cross-Repo Sweep and Drift ───────────────────────────────────────

const (
	defaultCheckpointStaleDays = 7
	defaultProposalStaleDays   = 30
)

// ManagedProject is one entry from ~./agents/config.json loaded for drift checks.
type ManagedProject struct {
	Name string
	Path string
}

// loadManagedProjects returns all registered projects from the global config.
func loadManagedProjects() ([]ManagedProject, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	names := cfg.ListProjects()
	sort.Strings(names)
	projects := make([]ManagedProject, 0, len(names))
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if path == "" {
			continue
		}
		projects = append(projects, ManagedProject{Name: name, Path: path})
	}
	return projects, nil
}

// RepoDriftReport captures drift conditions for one managed project.
type RepoDriftReport struct {
	Project              ManagedProject `json:"project"`
	Reachable            bool           `json:"reachable"`              // false if path doesn't exist
	MissingCheckpoint    bool           `json:"missing_checkpoint"`     // no checkpoint file
	StaleCheckpoint      bool           `json:"stale_checkpoint"`       // checkpoint older than threshold
	CheckpointAgeDays    int            `json:"checkpoint_age_days"`    // -1 if no checkpoint
	StaleProposalCount   int            `json:"stale_proposal_count"`   // proposals older than threshold
	MissingWorkflowDir   bool           `json:"missing_workflow_dir"`   // no .agents/workflow/
	MissingPlanStructure bool           `json:"missing_plan_structure"` // no .agents/workflow/plans/
	Warnings             []string       `json:"warnings"`
	Status               string         `json:"status"` // healthy|warn|unreachable
}

// detectRepoDrift inspects one managed project for workflow drift.
// All checks are read-only.
func detectRepoDrift(project ManagedProject, checkpointStaleDays, proposalStaleDays int) RepoDriftReport {
	report := RepoDriftReport{Project: project, CheckpointAgeDays: -1}

	// 1. Reachability
	if _, err := os.Stat(project.Path); err != nil {
		report.Reachable = false
		report.Status = "unreachable"
		report.Warnings = append(report.Warnings, fmt.Sprintf("project path %q does not exist or is not accessible", project.Path))
		return report
	}
	report.Reachable = true

	// 2. Checkpoint existence and age
	checkpointPath := filepath.Join(config.ProjectContextDir(project.Name), "checkpoint.yaml")
	checkpointData, err := os.ReadFile(checkpointPath)
	if err != nil {
		report.MissingCheckpoint = true
		report.Warnings = append(report.Warnings, "no checkpoint found")
	} else {
		var cp workflowCheckpoint
		if err := yaml.Unmarshal(checkpointData, &cp); err == nil && cp.Timestamp != "" {
			t, err := time.Parse(time.RFC3339, cp.Timestamp)
			if err == nil {
				ageDays := int(time.Since(t).Hours() / 24)
				report.CheckpointAgeDays = ageDays
				if ageDays > checkpointStaleDays {
					report.StaleCheckpoint = true
					report.Warnings = append(report.Warnings, fmt.Sprintf("checkpoint is %d days old (threshold: %d)", ageDays, checkpointStaleDays))
				}
			}
		}
	}

	// 3. Stale proposals
	proposals, err := config.ListPendingProposals()
	if err == nil {
		cutoff := time.Now().UTC().AddDate(0, 0, -proposalStaleDays)
		for _, p := range proposals {
			t, err := time.Parse(time.RFC3339, p.CreatedAt)
			if err == nil && t.Before(cutoff) {
				report.StaleProposalCount++
			}
		}
		if report.StaleProposalCount > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%d stale proposals (older than %d days)", report.StaleProposalCount, proposalStaleDays))
		}
	}

	// 4. Workflow directory presence
	workflowDir := filepath.Join(project.Path, ".agents", "workflow")
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		report.MissingWorkflowDir = true
		report.Warnings = append(report.Warnings, "no .agents/workflow/ directory — workflow not initialized")
	}

	// 5. Canonical plan structure
	plansDir := filepath.Join(project.Path, ".agents", "workflow", "plans")
	if _, err := os.Stat(plansDir); os.IsNotExist(err) {
		report.MissingPlanStructure = true
		// Only warn if workflow dir exists (otherwise workflow dir warning is enough)
		if !report.MissingWorkflowDir {
			report.Warnings = append(report.Warnings, "no .agents/workflow/plans/ directory — no canonical plans")
		}
	}

	if len(report.Warnings) == 0 {
		report.Status = "healthy"
	} else {
		report.Status = "warn"
	}
	return report
}

// AggregateDriftReport summarizes drift across all managed projects.
type AggregateDriftReport struct {
	Timestamp        string            `json:"timestamp"`
	TotalProjects    int               `json:"total_projects"`
	ProjectsChecked  int               `json:"projects_checked"`
	Reports          []RepoDriftReport `json:"reports"`
	HealthyCount     int               `json:"healthy_count"`
	WarnCount        int               `json:"warn_count"`
	UnreachableCount int               `json:"unreachable_count"`
	TopWarnings      []string          `json:"top_warnings"`
}

// aggregateDrift combines per-repo reports into a summary.
func aggregateDrift(reports []RepoDriftReport) AggregateDriftReport {
	agg := AggregateDriftReport{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		TotalProjects: len(reports),
		Reports:       reports,
	}
	seen := make(map[string]bool)
	for _, r := range reports {
		agg.ProjectsChecked++
		switch r.Status {
		case "healthy":
			agg.HealthyCount++
		case "unreachable":
			agg.UnreachableCount++
		default:
			agg.WarnCount++
		}
		for _, w := range r.Warnings {
			if !seen[w] {
				seen[w] = true
				agg.TopWarnings = append(agg.TopWarnings, fmt.Sprintf("[%s] %s", r.Project.Name, w))
			}
		}
	}
	return agg
}

// sweepLogPath returns the path for the sweep operation log.
func sweepLogPath() string {
	return filepath.Join(config.AgentsContextDir(), "sweep-log.jsonl")
}

// driftReportPath returns the path for the persisted drift report.
func driftReportPath() string {
	return filepath.Join(config.AgentsContextDir(), "drift-report.json")
}

// saveDriftReport writes the aggregate drift report to disk.
func saveDriftReport(agg AggregateDriftReport) error {
	if err := os.MkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(agg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(driftReportPath(), data, 0644)
}

// runWorkflowDrift is the read-only cross-repo drift detection command.
func runWorkflowDrift(cmd *cobra.Command, _ []string) error {
	checkpointDays, _ := cmd.Flags().GetInt("stale-days")
	proposalDays, _ := cmd.Flags().GetInt("proposal-days")
	projectFilter, _ := cmd.Flags().GetString("project")

	projects, err := loadManagedProjects()
	if err != nil {
		return fmt.Errorf("load managed projects: %w", err)
	}
	if len(projects) == 0 {
		ui.Info("No managed projects registered. Add one with: dot-agents add <path>")
		return nil
	}

	// Filter to single project if requested
	if projectFilter != "" {
		var filtered []ManagedProject
		for _, p := range projects {
			if p.Name == projectFilter {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("project %q not found in managed projects", projectFilter)
		}
		projects = filtered
	}

	// Run drift detection
	reports := make([]RepoDriftReport, 0, len(projects))
	for _, p := range projects {
		reports = append(reports, detectRepoDrift(p, checkpointDays, proposalDays))
	}
	agg := aggregateDrift(reports)

	// Save to disk
	_ = saveDriftReport(agg)

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agg)
	}

	// Human-readable output
	ui.Header("Workflow Drift Report")
	fmt.Fprintf(os.Stdout, "  %s projects checked%s\n\n", ui.Bold, ui.Reset)

	for _, r := range reports {
		statusBadge := ui.ColorText(ui.Green, "healthy")
		if r.Status == "warn" {
			statusBadge = ui.ColorText(ui.Yellow, "warn")
		} else if r.Status == "unreachable" {
			statusBadge = ui.ColorText(ui.Red, "unreachable")
		}
		fmt.Fprintf(os.Stdout, "  %-20s [%s]\n", r.Project.Name, statusBadge)
		for _, w := range r.Warnings {
			fmt.Fprintf(os.Stdout, "    %s↳ %s%s\n", ui.Dim, ui.Reset, w)
		}
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Summary")
	fmt.Fprintf(os.Stdout, "  healthy: %d  warnings: %d  unreachable: %d\n",
		agg.HealthyCount, agg.WarnCount, agg.UnreachableCount)
	fmt.Fprintf(os.Stdout, "  report saved: %s\n", config.DisplayPath(driftReportPath()))
	return nil
}

// ── Wave 7: Sweep types ───────────────────────────────────────────────────────

// SweepActionType enumerates the kinds of fixes the sweep can apply.
type SweepActionType string

const (
	SweepActionScaffoldWorkflowDir      SweepActionType = "scaffold_workflow_dir"
	SweepActionCreatePlanStructure      SweepActionType = "create_plan_structure"
	SweepActionCreateCheckpointReminder SweepActionType = "create_checkpoint_reminder"
	SweepActionFlagStaleProposals       SweepActionType = "flag_stale_proposals"
)

// SweepActionItem is one actionable fix in a sweep plan.
type SweepActionItem struct {
	Project              ManagedProject  `json:"project"`
	Action               SweepActionType `json:"action"`
	Description          string          `json:"description"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
}

// SweepPlan is the collection of planned actions for a sweep run.
type SweepPlan struct {
	CreatedAt string            `json:"created_at"`
	Actions   []SweepActionItem `json:"actions"`
}

// planSweep generates a sweep plan from drift reports.
func planSweep(reports []RepoDriftReport) SweepPlan {
	plan := SweepPlan{CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, r := range reports {
		if !r.Reachable {
			continue // can't fix unreachable projects
		}
		if r.MissingWorkflowDir {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionScaffoldWorkflowDir,
				Description:          fmt.Sprintf("Create .agents/workflow/ directory in %s", r.Project.Name),
				RequiresConfirmation: true,
			})
		}
		if r.MissingPlanStructure && !r.MissingWorkflowDir {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionCreatePlanStructure,
				Description:          fmt.Sprintf("Create .agents/workflow/plans/ directory in %s", r.Project.Name),
				RequiresConfirmation: true,
			})
		}
		if r.MissingCheckpoint || r.StaleCheckpoint {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionCreateCheckpointReminder,
				Description:          fmt.Sprintf("Add checkpoint reminder annotation for %s", r.Project.Name),
				RequiresConfirmation: false, // read-only annotation, no mutation
			})
		}
		if r.StaleProposalCount > 0 {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionFlagStaleProposals,
				Description:          fmt.Sprintf("Flag %d stale proposal(s) in %s for review", r.StaleProposalCount, r.Project.Name),
				RequiresConfirmation: false, // flagging only, not deleting
			})
		}
	}
	return plan
}

// SweepLogEntry is one record in sweep-log.jsonl.
type SweepLogEntry struct {
	Timestamp   string          `json:"timestamp"`
	Project     string          `json:"project"`
	Action      SweepActionType `json:"action"`
	Description string          `json:"description"`
	Applied     bool            `json:"applied"`
	DryRun      bool            `json:"dry_run"`
}

// appendSweepLog appends one entry to the sweep log.
func appendSweepLog(entry SweepLogEntry) {
	_ = os.MkdirAll(filepath.Dir(sweepLogPath()), 0755)
	f, err := os.OpenFile(sweepLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, _ := json.Marshal(entry)
	_, _ = f.Write(append(data, '\n'))
}

// applySweepAction executes one sweep action.
func applySweepAction(item SweepActionItem) error {
	switch item.Action {
	case SweepActionScaffoldWorkflowDir:
		return os.MkdirAll(filepath.Join(item.Project.Path, ".agents", "workflow"), 0755)
	case SweepActionCreatePlanStructure:
		return os.MkdirAll(filepath.Join(item.Project.Path, ".agents", "workflow", "plans"), 0755)
	case SweepActionCreateCheckpointReminder, SweepActionFlagStaleProposals:
		// These are informational; logged but no filesystem mutation
		return nil
	default:
		return fmt.Errorf("unknown sweep action %q", item.Action)
	}
}

// runWorkflowSweep runs drift detection and optionally applies fixes.
func runWorkflowSweep(cmd *cobra.Command, _ []string) error {
	checkpointDays, _ := cmd.Flags().GetInt("stale-days")
	proposalDays, _ := cmd.Flags().GetInt("proposal-days")
	applyFlag, _ := cmd.Flags().GetBool("apply")
	dryRun := !applyFlag

	projects, err := loadManagedProjects()
	if err != nil {
		return fmt.Errorf("load managed projects: %w", err)
	}
	if len(projects) == 0 {
		ui.Info("No managed projects registered.")
		return nil
	}

	// Run drift detection
	reports := make([]RepoDriftReport, 0, len(projects))
	for _, p := range projects {
		reports = append(reports, detectRepoDrift(p, checkpointDays, proposalDays))
	}

	plan := planSweep(reports)
	if len(plan.Actions) == 0 {
		ui.Success("No sweep actions needed — all projects look healthy.")
		return nil
	}

	modeLabel := "dry-run"
	if !dryRun {
		modeLabel = "apply"
	}
	ui.Header(fmt.Sprintf("Sweep Plan [%s]", modeLabel))
	fmt.Fprintln(os.Stdout)

	for i, action := range plan.Actions {
		marker := "○"
		if action.RequiresConfirmation && !dryRun {
			marker = "⚡"
		}
		fmt.Fprintf(os.Stdout, "  %s %d. [%s] %s\n", marker, i+1, action.Project.Name, action.Description)
	}
	fmt.Fprintln(os.Stdout)

	if dryRun {
		ui.Info("Run with --apply to execute these actions.")
		for _, action := range plan.Actions {
			appendSweepLog(SweepLogEntry{
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
				Project:     action.Project.Name,
				Action:      action.Action,
				Description: action.Description,
				Applied:     false,
				DryRun:      true,
			})
		}
		return nil
	}

	// Apply with per-action confirmation for destructive actions
	applied := 0
	for _, action := range plan.Actions {
		if action.RequiresConfirmation && !deps.Flags.Yes() {
			fmt.Fprintf(os.Stdout, "  Apply: %s? [y/N] ", action.Description)
			var resp string
			fmt.Scanln(&resp)
			if strings.ToLower(strings.TrimSpace(resp)) != "y" {
				ui.Info(fmt.Sprintf("  Skipped: %s", action.Description))
				appendSweepLog(SweepLogEntry{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					Project:     action.Project.Name,
					Action:      action.Action,
					Description: action.Description,
					Applied:     false,
					DryRun:      false,
				})
				continue
			}
		}
		if err := applySweepAction(action); err != nil {
			ui.Warn(fmt.Sprintf("Failed: %s — %v", action.Description, err))
		} else {
			applied++
			ui.Success(fmt.Sprintf("Applied: %s", action.Description))
		}
		appendSweepLog(SweepLogEntry{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Project:     action.Project.Name,
			Action:      action.Action,
			Description: action.Description,
			Applied:     true,
			DryRun:      false,
		})
	}
	fmt.Fprintln(os.Stdout)
	ui.Success(fmt.Sprintf("Sweep complete: %d/%d actions applied.", applied, len(plan.Actions)))
	return nil
}
