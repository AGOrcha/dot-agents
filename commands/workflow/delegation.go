package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

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

// DelegationContractMode discriminates contracts written by `workflow fanout`
// (delegated to a sub-agent worker) from contracts materialized for direct
// orchestrator-owned work via `workflow contract create`.
//
// Both modes share the same on-disk shape, advance through the same closeout
// pipeline (merge-back → delegation closeout → auto-advance), and contribute
// equally to the workflow audit trail. The distinction is documentation +
// downstream tooling (skills, lens routing) deciding which artifacts to expect.
type DelegationContractMode string

const (
	// DelegationContractModeDelegated is the legacy mode written by `workflow fanout`.
	// A sub-agent (loop-worker, codex helper, etc.) owns the write scope until merge-back.
	DelegationContractModeDelegated DelegationContractMode = "delegated"
	// DelegationContractModeDirect is the new mode written by `workflow contract create`.
	// The orchestrator itself owns the write scope; the contract pins guardrails
	// (write scope, success criteria, closeout discipline) so direct work flows
	// through the same audit trail as delegated work.
	DelegationContractModeDirect DelegationContractMode = "direct"
)

var validDelegationContractModes = map[DelegationContractMode]bool{
	DelegationContractModeDelegated: true,
	DelegationContractModeDirect:    true,
}

func isValidDelegationContractMode(m DelegationContractMode) bool {
	return validDelegationContractModes[m]
}

// normalizeDelegationContractMode defaults absent values to "delegated" so v1
// contracts on disk (written before this field existed) load with their
// historical semantics intact.
func normalizeDelegationContractMode(m DelegationContractMode) DelegationContractMode {
	if m == "" {
		return DelegationContractModeDelegated
	}
	return m
}

type DelegationContract struct {
	SchemaVersion            int                    `json:"schema_version" yaml:"schema_version"`
	ID                       string                 `json:"id" yaml:"id"`
	Mode                     DelegationContractMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	ParentPlanID             string                 `json:"parent_plan_id" yaml:"parent_plan_id"`
	ParentTaskID             string                 `json:"parent_task_id" yaml:"parent_task_id"`
	Title                    string                 `json:"title" yaml:"title"`
	Summary                  string                 `json:"summary" yaml:"summary"`
	WriteScope               []string               `json:"write_scope" yaml:"write_scope"`
	SuccessCriteria          string                 `json:"success_criteria" yaml:"success_criteria"`
	VerificationExpectations string                 `json:"verification_expectations" yaml:"verification_expectations"`
	MayMutateWorkflowState   bool                   `json:"may_mutate_workflow_state" yaml:"may_mutate_workflow_state"`
	Owner                    string                 `json:"owner" yaml:"owner"`
	Status                   string                 `json:"status" yaml:"status"`
	PendingIntent            CoordinationIntent     `json:"pending_intent,omitempty" yaml:"pending_intent,omitempty"`
	CreatedAt                string                 `json:"created_at" yaml:"created_at"`
	UpdatedAt                string                 `json:"updated_at" yaml:"updated_at"`
}

var validDelegationStatuses = map[string]bool{
	"pending": true, "active": true, "completed": true, "failed": true, "cancelled": true,
}

const (
	delegationAgentsDir         = ".agents"
	delegationProposalRoutePfx  = "proposal:"
	errLoadTasksForPlanFmt      = "load tasks for plan %s: %w"
	errTaskNotFoundInPlanShort  = "task %s not found in plan %s"
	delegationTaskNoteRouteFmt  = "task_note:%s/%s"
	delegationPlanSummaryRteFmt = "plan_summary:%s"
)

func isValidDelegationStatus(s string) bool { return validDelegationStatuses[s] }

func delegationDir(projectPath string) string {
	return filepath.Join(projectPath, delegationAgentsDir, "active", "delegation")
}

func mergeBackDir(projectPath string) string {
	return filepath.Join(projectPath, delegationAgentsDir, "active", "merge-back")
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
	// v1 contracts on disk predate the Mode field — default them to
	// "delegated" so historical semantics (fanout-style worker ownership)
	// are preserved without rewriting the file.
	c.Mode = normalizeDelegationContractMode(c.Mode)
	return &c, nil
}

func saveDelegationContract(projectPath string, c *DelegationContract) error {
	dir := delegationDir(projectPath)
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := yamlMarshal(c)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, c.ParentTaskID+".yaml"), data, 0644)
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
			continue
		}
		contracts = append(contracts, *c)
	}
	return contracts, nil
}

func writeScopeOverlaps(existing []DelegationContract, newScope []string, excludeTaskID string) []string {
	var conflicts []string
	for _, c := range existing {
		if c.Status != "pending" && c.Status != "active" {
			continue
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

func scopePathsOverlap(a, b string) bool {
	na := filepath.ToSlash(filepath.Clean(a))
	nb := filepath.ToSlash(filepath.Clean(b))
	if na == nb {
		return true
	}
	if strings.HasPrefix(nb, na+"/") || strings.HasPrefix(na, nb+"/") {
		return true
	}
	return false
}

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

type MergeBackVerification struct {
	Status  string `json:"status" yaml:"status"`
	Summary string `json:"summary" yaml:"summary"`
}

func saveMergeBack(projectPath string, s *MergeBackSummary) error {
	dir := mergeBackDir(projectPath)
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	frontmatter, err := yamlMarshal(s)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("---\n%s---\n\n## Summary\n\n%s\n\n## Integration Notes\n\n%s\n",
		string(frontmatter), s.Summary, s.IntegrationNotes)
	return osWriteFile(filepath.Join(dir, s.TaskID+".md"), []byte(content), 0644)
}

func loadMergeBack(projectPath, taskID string) (*MergeBackSummary, error) {
	path := filepath.Join(mergeBackDir(projectPath), taskID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
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

func foldBackDir(projectPath string) string {
	return filepath.Join(projectPath, delegationAgentsDir, "active", "fold-back")
}

func appendFoldBackBullet(notes, observation string) string {
	notes = strings.TrimRight(notes, "\n")
	line := "- " + observation
	if notes == "" {
		return line
	}
	return notes + "\n" + line
}

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
	if !strings.HasPrefix(routed, delegationProposalRoutePfx) {
		return "", fmt.Errorf("not a proposal route: %q", routed)
	}
	name := strings.TrimPrefix(routed, delegationProposalRoutePfx)
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
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yamlMarshal(&artifact)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, artifact.ID+".yaml"), data, 0644)
}

func writeFoldBackProposalFile(path string, fm foldBackProposalFrontmatter, body string) error {
	header, err := yamlMarshal(fm)
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

type foldBackUpsertInputs struct {
	planID      string
	taskID      string
	observation string
	propose     bool
	slug        string
}

func parseFoldBackUpsertInputs(cmd *cobra.Command, updateOnly bool) (*foldBackUpsertInputs, error) {
	in := &foldBackUpsertInputs{}
	in.planID, _ = cmd.Flags().GetString("plan")
	in.taskID, _ = cmd.Flags().GetString("task")
	in.observation, _ = cmd.Flags().GetString("observation")
	in.propose, _ = cmd.Flags().GetBool("propose")
	in.slug, _ = cmd.Flags().GetString("slug")
	in.slug = strings.TrimSpace(in.slug)

	if strings.TrimSpace(in.observation) == "" {
		return nil, fmt.Errorf("observation text is required")
	}
	if updateOnly && in.slug == "" {
		return nil, fmt.Errorf("--slug is required for fold-back update")
	}
	if in.slug != "" {
		if err := validateFoldBackSlug(in.slug); err != nil {
			return nil, err
		}
	}
	return in, nil
}

func loadPriorFoldBackArtifact(projectPath, slug string) (*foldBackArtifact, bool, error) {
	if slug == "" {
		return nil, false, nil
	}
	st, statErr := os.Stat(foldBackArtifactFile(projectPath, slug))
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, false, nil
		}
		return nil, false, statErr
	}
	if st.IsDir() {
		return nil, false, nil
	}
	a, loadErr := loadFoldBackArtifactByID(projectPath, slug)
	if loadErr != nil {
		return nil, false, fmt.Errorf("load fold-back %q: %w", slug, loadErr)
	}
	return &a, true, nil
}

func validateFoldBackPriorAgreement(prior *foldBackArtifact, in *foldBackUpsertInputs) error {
	if prior.PlanID != in.planID {
		return fmt.Errorf("fold-back %q belongs to plan %q, not %q", in.slug, prior.PlanID, in.planID)
	}
	if in.propose {
		return fmt.Errorf("--propose is not valid when updating an existing slug-scoped fold-back")
	}
	if prior.Classification != "small" {
		return nil
	}
	if prior.TaskID != "" {
		if strings.TrimSpace(in.taskID) == "" {
			return fmt.Errorf("fold-back %q is task-scoped (%s); pass --task %s", in.slug, prior.TaskID, prior.TaskID)
		}
		if in.taskID != prior.TaskID {
			return fmt.Errorf("--task %q does not match fold-back scope (expected %q)", in.taskID, prior.TaskID)
		}
		return nil
	}
	if strings.TrimSpace(in.taskID) != "" {
		return fmt.Errorf("fold-back %q is plan-scoped; omit --task", in.slug)
	}
	return nil
}

func updateTaskFoldBackNote(projectPath, planID, taskID string, mutate func(notes string) string) error {
	tf, err := loadCanonicalTasks(projectPath, planID)
	if err != nil {
		return fmt.Errorf(errLoadTasksForPlanFmt, planID, err)
	}
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			tf.Tasks[i].Notes = mutate(tf.Tasks[i].Notes)
			return saveCanonicalTasks(projectPath, tf)
		}
	}
	return fmt.Errorf(errTaskNotFoundInPlanShort, taskID, planID)
}

func updatePlanFoldBackSummary(projectPath, planID, createdAt string, mutate func(summary string) string) error {
	plan, err := loadCanonicalPlan(projectPath, planID)
	if err != nil {
		return err
	}
	plan.Summary = mutate(plan.Summary)
	plan.UpdatedAt = createdAt
	return saveCanonicalPlan(projectPath, plan)
}

func updateExistingProposalFoldBack(prior *foldBackArtifact, observation string, artifact *foldBackArtifact) error {
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
	return writeFoldBackProposalFile(propPath, fm, observation)
}

func updateExistingSmallFoldBack(projectPath string, in *foldBackUpsertInputs, prior *foldBackArtifact, createdAt string, artifact *foldBackArtifact) error {
	artifact.Classification = "small"
	artifact.TaskID = prior.TaskID
	mutate := func(notes string) string {
		return setFoldBackTaggedNote(notes, in.slug, in.observation)
	}
	if prior.TaskID != "" {
		if err := updateTaskFoldBackNote(projectPath, in.planID, prior.TaskID, mutate); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf(delegationTaskNoteRouteFmt, in.planID, prior.TaskID)
		return nil
	}
	if err := updatePlanFoldBackSummary(projectPath, in.planID, createdAt, mutate); err != nil {
		return err
	}
	artifact.TaskID = ""
	artifact.RoutedTo = fmt.Sprintf(delegationPlanSummaryRteFmt, in.planID)
	return nil
}

func createProposalFoldBack(in *foldBackUpsertInputs, ts int64, createdAt string, artifact *foldBackArtifact) error {
	artifact.Classification = "proposal"
	artifact.TaskID = strings.TrimSpace(in.taskID)
	proposalName := fmt.Sprintf("obs-%d.md", ts)
	if in.slug != "" {
		proposalName = fmt.Sprintf("obs-%s.md", in.slug)
	}
	proposalsDir := filepath.Join(config.AgentsHome(), "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		return err
	}
	proposalPath := filepath.Join(proposalsDir, proposalName)
	fm := foldBackProposalFrontmatter{
		Title:       fmt.Sprintf("Fold-back: %s", in.planID),
		Observation: in.observation,
		PlanID:      in.planID,
		CreatedAt:   createdAt,
	}
	if artifact.TaskID != "" {
		fm.TaskID = artifact.TaskID
	}
	if err := writeFoldBackProposalFile(proposalPath, fm, in.observation); err != nil {
		return err
	}
	artifact.RoutedTo = delegationProposalRoutePfx + proposalName
	return nil
}

func createSmallFoldBack(projectPath string, in *foldBackUpsertInputs, createdAt string, artifact *foldBackArtifact) error {
	artifact.Classification = "small"
	taskID := strings.TrimSpace(in.taskID)
	artifact.TaskID = taskID
	useTagged := in.slug != ""
	mutate := func(text string) string {
		if useTagged {
			return setFoldBackTaggedNote(text, in.slug, in.observation)
		}
		return appendFoldBackBullet(text, in.observation)
	}
	if taskID != "" {
		if err := updateTaskFoldBackNote(projectPath, in.planID, taskID, mutate); err != nil {
			return err
		}
		artifact.RoutedTo = fmt.Sprintf(delegationTaskNoteRouteFmt, in.planID, taskID)
		return nil
	}
	if err := updatePlanFoldBackSummary(projectPath, in.planID, createdAt, mutate); err != nil {
		return err
	}
	artifact.RoutedTo = fmt.Sprintf(delegationPlanSummaryRteFmt, in.planID)
	return nil
}

func dispatchFoldBackUpsert(projectPath string, in *foldBackUpsertInputs, prior *foldBackArtifact, priorExists bool, ts int64, createdAt string, artifact *foldBackArtifact) error {
	switch {
	case priorExists && prior.Classification == "proposal":
		return updateExistingProposalFoldBack(prior, in.observation, artifact)
	case priorExists && prior.Classification == "small":
		return updateExistingSmallFoldBack(projectPath, in, prior, createdAt, artifact)
	case !priorExists && in.propose:
		return createProposalFoldBack(in, ts, createdAt, artifact)
	case !priorExists:
		return createSmallFoldBack(projectPath, in, createdAt, artifact)
	default:
		return fmt.Errorf("internal fold-back routing error (slug=%q propose=%v priorExists=%v)", in.slug, in.propose, priorExists)
	}
}

func validatePriorFoldBack(prior *foldBackArtifact, in *foldBackUpsertInputs) error {
	if err := validateFoldBackPriorAgreement(prior, in); err != nil {
		return err
	}
	if prior.Classification == "small" && in.propose {
		return fmt.Errorf("cannot use --propose for slug %q: existing artifact is inline (small)", in.slug)
	}
	return nil
}

func assignFoldBackArtifactIdentity(artifact *foldBackArtifact, prior *foldBackArtifact, priorExists bool, slug string, ts int64) {
	switch {
	case priorExists:
		artifact.ID = prior.ID
		artifact.CreatedAt = prior.CreatedAt
	case slug != "":
		artifact.ID = slug
	default:
		artifact.ID = fmt.Sprintf("fold-%d", ts)
	}
}

func readFoldBackArtifacts(dir, planFilter string) ([]foldBackArtifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var artifacts []foldBackArtifact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var a foldBackArtifact
		if err := yaml.Unmarshal(data, &a); err != nil {
			return nil, fmt.Errorf("parse fold-back %s: %w", e.Name(), err)
		}
		if planFilter != "" && a.PlanID != planFilter {
			continue
		}
		artifacts = append(artifacts, a)
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].CreatedAt < artifacts[j].CreatedAt
	})
	return artifacts, nil
}

func renderFoldBackList(out io.Writer, artifacts []foldBackArtifact) error {
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

func runWorkflowFoldBackUpsert(cmd *cobra.Command, updateOnly bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	in, err := parseFoldBackUpsertInputs(cmd, updateOnly)
	if err != nil {
		return err
	}

	if _, err := loadCanonicalPlan(project.Path, in.planID); err != nil {
		return fmt.Errorf("plan %s not found: %w", in.planID, err)
	}

	now := time.Now().UTC()
	createdAt := now.Format(time.RFC3339)
	ts := now.UnixNano()

	prior, priorExists, err := loadPriorFoldBackArtifact(project.Path, in.slug)
	if err != nil {
		return err
	}
	if updateOnly && !priorExists {
		return fmt.Errorf("no fold-back artifact with slug %q", in.slug)
	}
	if priorExists {
		if err := validatePriorFoldBack(prior, in); err != nil {
			return err
		}
	}

	artifact := foldBackArtifact{
		SchemaVersion: 1,
		PlanID:        in.planID,
		Observation:   in.observation,
		CreatedAt:     createdAt,
	}
	assignFoldBackArtifactIdentity(&artifact, prior, priorExists, in.slug, ts)

	if err := dispatchFoldBackUpsert(project.Path, in, prior, priorExists, ts, createdAt, &artifact); err != nil {
		return err
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
	if priorExists {
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
	artifacts, err := readFoldBackArtifacts(dir, planFilter)
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

	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(artifacts)
	}
	return renderFoldBackList(out, artifacts)
}

func ensureTaskVerificationDir(projectPath, taskID string) error {
	dir := filepath.Join(projectPath, delegationAgentsDir, "active", "verification", taskID)
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

// checkFanoutScopeEvidenceWarnings emits non-blocking warnings to stderr when:
// (1) no scope-evidence sidecar exists for the task and the KG graph adapter is available, or
// (2) a sidecar exists but its confidence is "low".
// Both warnings are suppressed when skipCheck is true.
func checkFanoutScopeEvidenceWarnings(projectPath, planID, taskID string, skipCheck bool) {
	if skipCheck {
		return
	}
	sidecarPath := deriveScopeEvidencePath(projectPath, planID, taskID)
	_, statErr := os.Stat(sidecarPath)
	if os.IsNotExist(statErr) {
		// No sidecar — warn only when graph is available so the message is actionable.
		cfg, _ := loadGraphBridgeConfig(projectPath)
		if cfg == nil {
			cfg = &GraphBridgeConfig{Enabled: false}
		}
		graphHome := cfg.GraphHome
		if graphHome == "" {
			graphHome = defaultGraphHome(projectPath)
		}
		adapter := NewLocalGraphAdapter(graphHome)
		health, _ := adapter.Health()
		if health.AdapterAvailable {
			fmt.Fprintf(os.Stderr, "warning: no scope-evidence sidecar for %s; run workflow plan derive-scope first\n", taskID)
		}
		return
	}
	if statErr != nil {
		// Unexpected stat error — skip silently; do not block fanout.
		return
	}
	// Sidecar exists — check confidence.
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		return
	}
	var ev ScopeEvidence
	if err := yaml.Unmarshal(data, &ev); err != nil {
		return
	}
	if ev.Confidence == "low" {
		fmt.Fprintf(os.Stderr, "warning: scope-evidence confidence is low for %s; consider re-running derive-scope\n", taskID)
	}
}

// resolveFanoutSliceTask resolves taskID and seed write scope from an optional
// slice ID. Returns the (possibly mutated) taskID and the seed write scope.
func resolveFanoutSliceTask(projectPath, planID, sliceID, taskID string, writeScopeExplicit bool) (string, []string, error) {
	if sliceID == "" {
		return taskID, nil, nil
	}
	sf, err := loadCanonicalSlices(projectPath, planID)
	if err != nil {
		return "", nil, fmt.Errorf("load slices for plan %s: %w", planID, err)
	}
	var found *CanonicalSlice
	for i := range sf.Slices {
		if sf.Slices[i].ID == sliceID {
			found = &sf.Slices[i]
			break
		}
	}
	if found == nil {
		return "", nil, fmt.Errorf("slice %q not found in plan %s", sliceID, planID)
	}
	if found.Status == "completed" {
		return "", nil, fmt.Errorf("slice %q is already completed", sliceID)
	}
	taskID = found.ParentTaskID
	var ws []string
	if !writeScopeExplicit {
		ws = append(ws, found.WriteScope...)
	}
	return taskID, ws, nil
}

func resolveFanoutTargetTask(tasks *CanonicalTaskFile, taskID, planID string) (*CanonicalTask, error) {
	var targetTask *CanonicalTask
	for i := range tasks.Tasks {
		if tasks.Tasks[i].ID == taskID {
			targetTask = &tasks.Tasks[i]
			break
		}
	}
	if targetTask == nil {
		return nil, fmt.Errorf("task %s not found in plan %s", taskID, planID)
	}
	if targetTask.Status != "pending" && targetTask.Status != "in_progress" {
		return nil, fmt.Errorf("task %s has status %q — only pending or in_progress tasks can be delegated", taskID, targetTask.Status)
	}
	return targetTask, nil
}

func resolveFanoutWriteScope(seed []string, csv string, explicit bool, fallback []string) []string {
	if explicit {
		ws := seed[:0]
		for _, p := range strings.Split(csv, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				ws = append(ws, p)
			}
		}
		return ws
	}
	if len(seed) == 0 && len(fallback) > 0 {
		return append([]string(nil), fallback...)
	}
	return seed
}

func checkFanoutWriteScopeConflicts(projectPath string, writeScope []string, taskID string) error {
	existing, err := listDelegationContracts(projectPath)
	if err != nil {
		return fmt.Errorf("list delegations: %w", err)
	}
	conflicts := writeScopeOverlaps(existing, writeScope, taskID)
	if len(conflicts) == 0 {
		return nil
	}
	for _, c := range conflicts {
		ui.Warn(c)
	}
	return fmt.Errorf("delegation rejected: write scope overlaps with existing active delegation(s)")
}

// persistFanoutBundle writes the bundle that accompanies a fanout contract.
// The contract itself is already persisted by materializeDelegationContract;
// this helper only handles the bundle write and cleans up the contract file
// if the bundle write fails, preserving the original all-or-nothing semantics.
func persistFanoutBundle(projectPath string, contract *DelegationContract, bundle *delegationBundleYAML) error {
	return persistFanoutBundleWithBase(projectPath, contract, bundle, nil)
}

// persistFanoutBundleWithBase is persistFanoutBundle with the §4.2
// base-resolution fields injected under scope. persistFanoutBundle delegates
// here with a nil resolution to preserve the legacy default-master shape.
func persistFanoutBundleWithBase(projectPath string, contract *DelegationContract, bundle *delegationBundleYAML, res *baseResolution) error {
	if err := saveDelegationBundleWithBase(projectPath, bundle, res); err != nil {
		contractPath := filepath.Join(delegationDir(projectPath), contract.ParentTaskID+".yaml")
		_ = os.Remove(contractPath)
		return fmt.Errorf("save delegation bundle: %w", err)
	}
	return nil
}

// saveDelegationBundleWithBase writes the fanout bundle with the §4.2
// base-resolution fields injected under scope. It mirrors saveDelegationBundle
// but uses marshalBundleWithBase so the canonical struct stays unchanged.
func saveDelegationBundleWithBase(projectPath string, b *delegationBundleYAML, res *baseResolution) error {
	if strings.TrimSpace(b.DelegationID) == "" {
		return fmt.Errorf("delegation bundle: empty delegation_id")
	}
	dir := delegationBundlesDir(projectPath)
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := marshalBundleWithBase(b, res)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, b.DelegationID+".yaml"), data, 0644)
}

type fanoutInputs struct {
	planID             string
	taskID             string
	sliceID            string
	owner              string
	writeScopeCSV      string
	writeScopeExplicit bool
}

func parseFanoutInputs(cmd *cobra.Command) fanoutInputs {
	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	sliceID, _ := cmd.Flags().GetString("slice")
	owner, _ := cmd.Flags().GetString("owner")
	writeScopeCSV, _ := cmd.Flags().GetString("write-scope")
	return fanoutInputs{
		planID:             planID,
		taskID:             taskID,
		sliceID:            sliceID,
		owner:              owner,
		writeScopeCSV:      writeScopeCSV,
		writeScopeExplicit: cmd.Flags().Changed("write-scope"),
	}
}

func resolveFanoutTaskSelection(projectPath string, in fanoutInputs) (string, []string, error) {
	if in.sliceID != "" && in.taskID != "" {
		return "", nil, fmt.Errorf("provide --slice or --task, not both")
	}
	taskID, writeScope, err := resolveFanoutSliceTask(projectPath, in.planID, in.sliceID, in.taskID, in.writeScopeExplicit)
	if err != nil {
		return "", nil, err
	}
	if taskID == "" {
		return "", nil, fmt.Errorf("provide --slice <slice-id> or --task <task-id>")
	}
	return taskID, writeScope, nil
}

func runWorkflowFanout(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	in := parseFanoutInputs(cmd)

	plan, err := loadCanonicalPlan(project.Path, in.planID)
	if err != nil {
		return fmt.Errorf("plan %s not found: %w", in.planID, err)
	}

	taskID, writeScope, err := resolveFanoutTaskSelection(project.Path, in)
	if err != nil {
		return err
	}

	tf, err := loadCanonicalTasks(project.Path, in.planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %s not found: %w", in.planID, err)
	}
	targetTask, err := resolveFanoutTargetTask(tf, taskID, in.planID)
	if err != nil {
		return err
	}

	if _, err := loadDelegationContract(project.Path, taskID); err == nil {
		return fmt.Errorf("task %s already has an active delegation contract", taskID)
	}

	writeScope = resolveFanoutWriteScope(writeScope, in.writeScopeCSV, in.writeScopeExplicit, targetTask.WriteScope)

	if err := ensureTaskVerificationDir(project.Path, taskID); err != nil {
		return fmt.Errorf("prepare verification directory: %w", err)
	}
	skipTDD, _ := cmd.Flags().GetBool("skip-tdd-gate")
	if err := checkPreVerifierTDDGate(project.Path, writeScope, targetTask.VerificationRequired, skipTDD); err != nil {
		return err
	}

	skipEvidenceCheck, _ := cmd.Flags().GetBool("skip-evidence-check")
	checkFanoutScopeEvidenceWarnings(project.Path, in.planID, taskID, skipEvidenceCheck)

	if err := checkFanoutWriteScopeConflicts(project.Path, writeScope, taskID); err != nil {
		return err
	}

	baseRes, err := fanoutResolveBase(cmd, project.Path, in.planID, taskID, targetTask.DependsOn)
	if err != nil {
		return err
	}

	contract, err := materializeFanoutContractAndBundle(cmd, fanoutMaterializeRequest{
		Project:    project,
		In:         in,
		Plan:       plan,
		TargetTask: targetTask,
		TaskID:     taskID,
		WriteScope: writeScope,
		BaseRes:    &baseRes,
	})
	if err != nil {
		return err
	}

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
		fmt.Sprintf("Base branch: %s", fanoutBaseSummary(baseRes)),
	)
	return nil
}

// fanoutBaseSummary renders the resolved base for the fanout success box.
func fanoutBaseSummary(res baseResolution) string {
	if res.BaseTask != "" && res.BasePR != 0 {
		return fmt.Sprintf("%s (PR #%d, from %s)", res.BaseBranch, res.BasePR, res.BaseTask)
	}
	return res.BaseBranch
}

func gitDiffChangedFiles(projectPath string) []string {
	gitOut, err := execabs.Command("git", "-C", projectPath, "diff", "--name-only", "HEAD").Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(string(gitOut)), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

func buildMergeBackSummary(taskID, summary, verificationStatus, integrationNotes, now string, contract *DelegationContract, filesChanged []string) *MergeBackSummary {
	return &MergeBackSummary{
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
}

func buildMergeBackVerificationDoc(taskID, summary, verificationStatus, integrationNotes, now string, contract *DelegationContract, filesChanged []string) *VerificationResultDoc {
	verifSummary := strings.TrimSpace(integrationNotes)
	if verifSummary == "" {
		verifSummary = summary
	}
	return &VerificationResultDoc{
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
}

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

	contract, err := loadDelegationContract(project.Path, taskID)
	if err != nil {
		return fmt.Errorf("delegation contract for task %s not found: %w", taskID, err)
	}
	if contract.Status == "completed" || contract.Status == "cancelled" {
		return fmt.Errorf("delegation for task %s is already %s", taskID, contract.Status)
	}

	filesChanged := gitDiffChangedFiles(project.Path)

	now := time.Now().UTC().Format(time.RFC3339)
	mergeBack := buildMergeBackSummary(taskID, summary, verificationStatus, integrationNotes, now, contract, filesChanged)
	if err := saveMergeBack(project.Path, mergeBack); err != nil {
		return fmt.Errorf("save merge-back: %w", err)
	}

	vrDoc := buildMergeBackVerificationDoc(taskID, summary, verificationStatus, integrationNotes, now, contract, filesChanged)
	if err := writeVerificationResultYAML(project.Path, vrDoc); err != nil {
		return fmt.Errorf("write verification result: %w", err)
	}

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
	return filepath.Join(projectPath, delegationAgentsDir, "active", "delegation-bundles")
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
	if err := osMkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yamlMarshal(b)
	if err != nil {
		return err
	}
	return osWriteFile(filepath.Join(dir, b.DelegationID+".yaml"), data, 0644)
}

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

func explicitVerifierSequence(projectPath, verifierSeqFlag string) ([]string, error) {
	sequence := splitCommaVerifierList(verifierSeqFlag)
	if len(sequence) == 0 {
		return nil, fmt.Errorf("--verifier-sequence is non-empty but yielded no verifier profile ids")
	}
	d, err := loadAgentsrcFanoutDispatch(projectPath)
	if err != nil {
		return nil, err
	}
	var profiles map[string]json.RawMessage
	if d != nil {
		profiles = d.VerifierProfiles
	}
	if err := validateVerifierProfileRefs(sequence, profiles); err != nil {
		return nil, err
	}
	return sequence, nil
}

func mappedVerifierSequence(projectPath, appType string) ([]string, error) {
	d, err := loadAgentsrcFanoutDispatch(projectPath)
	if err != nil {
		return nil, err
	}
	if d == nil || len(d.AppTypeVerifierMap) == 0 || appType == "" {
		return nil, nil
	}
	sequence := append([]string(nil), d.AppTypeVerifierMap[appType]...)
	if len(sequence) == 0 {
		return nil, nil
	}
	if err := validateVerifierProfileRefs(sequence, d.VerifierProfiles); err != nil {
		return nil, err
	}
	return sequence, nil
}

func resolveFanoutVerifierDispatch(projectPath string, cmd *cobra.Command, plan *CanonicalPlan, task *CanonicalTask) (appType string, sequence []string, err error) {
	appType = strings.TrimSpace(task.AppType)
	if appType == "" && plan != nil {
		appType = strings.TrimSpace(plan.DefaultAppType)
	}

	verifierSeqFlag, _ := cmd.Flags().GetString("verifier-sequence")
	verifierSeqFlag = strings.TrimSpace(verifierSeqFlag)
	if verifierSeqFlag != "" {
		sequence, err = explicitVerifierSequence(projectPath, verifierSeqFlag)
		if err != nil {
			return "", nil, err
		}
		return appType, sequence, nil
	}
	sequence, err = mappedVerifierSequence(projectPath, appType)
	if err != nil {
		return "", nil, err
	}
	return appType, sequence, nil
}

type fanoutBundleFlags struct {
	profile          string
	feedbackGoal     string
	validationQueue  string
	selReason        string
	overlays         []string
	promptLines      []string
	promptFiles      []string
	contextFiles     []string
	scenarioTags     []string
	regressionArts   []string
	reqNeg           bool
	sandbox          bool
	verifierRetryMax int
	lensRetryMax     int
}

func collectFanoutBundleFlags(cmd *cobra.Command) *fanoutBundleFlags {
	f := &fanoutBundleFlags{}
	f.profile, _ = cmd.Flags().GetString("delegate-profile")
	f.profile = strings.TrimSpace(f.profile)
	if f.profile == "" {
		f.profile = defaultDelegateProfile
	}
	f.feedbackGoal, _ = cmd.Flags().GetString("feedback-goal")
	f.feedbackGoal = strings.TrimSpace(f.feedbackGoal)
	if f.feedbackGoal == "" {
		f.feedbackGoal = defaultDelegationFeedbackGoal
	}
	f.validationQueue, _ = cmd.Flags().GetString("validation-queue")
	f.validationQueue = strings.TrimSpace(f.validationQueue)
	f.selReason, _ = cmd.Flags().GetString("selection-reason")

	f.overlays = trimStringSlice(mustGetStringSlice(cmd, "project-overlay"))
	f.promptLines = trimStringSlice(mustGetStringSlice(cmd, "prompt"))
	f.promptFiles = trimStringSlice(mustGetStringSlice(cmd, "prompt-file"))
	f.contextFiles = trimStringSlice(mustGetStringSlice(cmd, "context-file"))
	f.scenarioTags = trimStringSlice(mustGetStringSlice(cmd, "scenario-tag"))
	f.regressionArts = trimStringSlice(mustGetStringSlice(cmd, "regression-artifact"))

	f.reqNeg, _ = cmd.Flags().GetBool("require-negative-coverage")
	f.sandbox, _ = cmd.Flags().GetBool("sandbox-mutations")
	f.verifierRetryMax, _ = cmd.Flags().GetInt("verifier-retry-max")
	f.lensRetryMax, _ = cmd.Flags().GetInt("lens-retry-max")
	return f
}

func validateFanoutBundleFlagPaths(projectPath string, f *fanoutBundleFlags) error {
	checks := []struct {
		paths []string
		flag  string
	}{
		{f.overlays, "--project-overlay"},
		{f.promptFiles, "--prompt-file"},
		{f.contextFiles, "--context-file"},
	}
	for _, c := range checks {
		for _, p := range c.paths {
			if _, err := validateProjectFileRef(projectPath, p); err != nil {
				return fmt.Errorf("%s %w", c.flag, err)
			}
		}
	}
	if f.validationQueue != "" {
		if _, err := validateProjectFileRef(projectPath, f.validationQueue); err != nil {
			return fmt.Errorf("--validation-queue %w", err)
		}
	}
	for _, p := range f.regressionArts {
		if _, err := validateInsideProjectPath(projectPath, p); err != nil {
			return fmt.Errorf("--regression-artifact %w", err)
		}
	}
	return nil
}

func newDelegationEvidencePolicy() *delegationEvidencePolicy {
	return &delegationEvidencePolicy{}
}

func applyFanoutEvidencePolicy(b *delegationBundleYAML, f *fanoutBundleFlags) {
	if f.reqNeg || f.sandbox {
		b.Verification.EvidencePolicy = newDelegationEvidencePolicy()
		if f.reqNeg {
			v := true
			b.Verification.EvidencePolicy.RequireNegativeCoverage = &v
		}
		if f.sandbox {
			v := true
			b.Verification.EvidencePolicy.SandboxMutations = &v
		}
	}
	if f.verifierRetryMax > 0 {
		if b.Verification.EvidencePolicy == nil {
			b.Verification.EvidencePolicy = newDelegationEvidencePolicy()
		}
		rm := f.verifierRetryMax
		b.Verification.EvidencePolicy.VerifierChainMax = &rm
	}
	if f.lensRetryMax > 0 {
		if b.Verification.EvidencePolicy == nil {
			b.Verification.EvidencePolicy = newDelegationEvidencePolicy()
		}
		rm := f.lensRetryMax
		b.Verification.EvidencePolicy.LensChainMax = &rm
	}
}

// fanoutBundleRequest bundles the inputs to buildDelegationBundleForFanout so
// the function stays under the parameter limit while keeping each value
// individually addressable from the caller.
type fanoutBundleRequest struct {
	ProjectPath      string
	Cmd              *cobra.Command
	PlanID           string
	TaskID           string
	SliceID          string
	Plan             *CanonicalPlan
	TargetTask       *CanonicalTask
	Contract         *DelegationContract
	WriteScope       []string
	CreatedAtRFC3339 string
}

func buildDelegationBundleForFanout(req fanoutBundleRequest) (*delegationBundleYAML, error) {
	f := collectFanoutBundleFlags(req.Cmd)
	if err := validateFanoutBundleFlagPaths(req.ProjectPath, f); err != nil {
		return nil, err
	}

	owner := strings.TrimSpace(req.Contract.Owner)
	if owner == "" {
		owner = "unspecified"
	}

	var b delegationBundleYAML
	b.SchemaVersion = 1
	b.DelegationID = req.Contract.ID
	b.PlanID = req.PlanID
	b.TaskID = req.TaskID
	if req.SliceID != "" {
		b.SliceID = req.SliceID
	}
	b.Owner = owner

	b.Worker.Profile = f.profile
	if len(f.overlays) > 0 {
		b.Worker.ProjectOverlayFiles = f.overlays
	}

	b.Selection = &struct {
		SelectedBy string `yaml:"selected_by"`
		SelectedAt string `yaml:"selected_at"`
		Reason     string `yaml:"reason,omitempty"`
	}{
		SelectedBy: "workflow fanout",
		SelectedAt: req.CreatedAtRFC3339,
		Reason:     strings.TrimSpace(f.selReason),
	}

	b.Scope.WriteScope = append([]string(nil), req.WriteScope...)

	if len(f.promptLines) > 0 {
		b.Prompt.Inline = f.promptLines
	}
	if len(f.promptFiles) > 0 {
		b.Prompt.PromptFiles = f.promptFiles
	}
	if len(f.contextFiles) > 0 {
		b.Context.RequiredFiles = f.contextFiles
	}

	b.Verification.FeedbackGoal = f.feedbackGoal
	if len(f.scenarioTags) > 0 {
		b.Verification.ScenarioTags = f.scenarioTags
	}
	if len(f.regressionArts) > 0 {
		b.Verification.RegressionArtifacts = f.regressionArts
	}
	if f.validationQueue != "" {
		b.Verification.HigherLayerValidationQueue = f.validationQueue
	}

	appType, verifierSeq, err := resolveFanoutVerifierDispatch(req.ProjectPath, req.Cmd, req.Plan, req.TargetTask)
	if err != nil {
		return nil, err
	}
	if appType != "" {
		b.Verification.AppType = appType
	}
	if len(verifierSeq) > 0 {
		b.Verification.VerifierSequence = verifierSeq
	}

	applyFanoutEvidencePolicy(&b, f)

	b.Closeout.WorkerMust = []string{"workflow_verify_record", "workflow_checkpoint", "workflow_merge_back"}
	b.Closeout.ParentMust = []string{"workflow_delegation_closeout"}

	return &b, nil
}

// defaultGHPRLister is the gh seam used by fanout base-resolution. Tests swap it
// for a hermetic fake so no gh binary or network is required.
var defaultGHPRLister ghPRLister = execGHPRLister{}

// depBranchForTask returns the open-PR head branch that backs a dep task, or ""
// when no open PR matches. Matching is by branch name containing the bare task
// id — the worktree/feature branch convention used across the workflow. This is
// the §4.1 "resolve a dep's PR branch via task metadata (and gh as a seam)"
// step: the task status comes from TASKS.yaml; the branch+number come from gh.
func depBranchForTask(taskID string, openPRs []ghPR) string {
	for _, pr := range openPRs {
		if pr.HeadRefName != "" && strings.Contains(pr.HeadRefName, taskID) {
			return pr.HeadRefName
		}
	}
	return ""
}

// collectDepStatusAndBranch builds the per-dep status and branch maps the §4.1
// in_flight_tasks join needs. Keys are qualified "<plan>/<task>" ids. Deps that
// cannot be resolved (missing plan/task) are recorded with an empty status so
// the algorithm treats them as not-in-flight (base off master).
func collectDepStatusAndBranch(projectPath, planID string, deps []string, openPRs []ghPR) (status, branch map[string]string) {
	status = make(map[string]string, len(deps))
	branch = make(map[string]string, len(deps))
	cache := make(map[string]*CanonicalTaskFile)
	for _, dep := range deps {
		qid := qualifyDepID(dep, planID)
		refPlanID, refTaskID := splitQualifiedDep(qid)
		tf, err := loadCrossPlanTasksCached(projectPath, refPlanID, cache)
		if err != nil || tf == nil {
			continue
		}
		for _, t := range tf.Tasks {
			if t.ID == refTaskID {
				status[qid] = t.Status
				branch[qid] = depBranchForTask(refTaskID, openPRs)
				break
			}
		}
	}
	return status, branch
}

// splitQualifiedDep splits a "<plan>/<task>" id into its parts.
func splitQualifiedDep(qid string) (planID, taskID string) {
	idx := strings.Index(qid, "/")
	if idx < 0 {
		return "", qid
	}
	return qid[:idx], qid[idx+1:]
}

// fanoutMaterializeRequest carries the inputs to materializeFanoutContractAndBundle.
type fanoutMaterializeRequest struct {
	Project    workflowProjectRef
	In         fanoutInputs
	Plan       *CanonicalPlan
	TargetTask *CanonicalTask
	TaskID     string
	WriteScope []string
	BaseRes    *baseResolution
}

// materializeFanoutContractAndBundle creates the delegation contract, builds the
// bundle, and persists the bundle with the §4.2 base-resolution fields. On
// bundle-build failure it removes the orphan contract so retries are not blocked
// by the duplicate-contract guard. Extracted from runWorkflowFanout to keep that
// function's cognitive complexity under the project ceiling.
func materializeFanoutContractAndBundle(cmd *cobra.Command, req fanoutMaterializeRequest) (*DelegationContract, error) {
	now := time.Now().UTC()
	createdAtRFC3339 := now.Format(time.RFC3339)
	contract, err := materializeDelegationContract(materializeContractRequest{
		ProjectPath:     req.Project.Path,
		Mode:            DelegationContractModeDelegated,
		PlanID:          req.In.planID,
		TaskID:          req.TaskID,
		Title:           req.TargetTask.Title,
		Summary:         fmt.Sprintf("Delegated from plan %s", req.Plan.Title),
		WriteScope:      req.WriteScope,
		SuccessCriteria: req.TargetTask.Notes,
		Owner:           req.In.owner,
		Now:             now,
	})
	if err != nil {
		return nil, err
	}
	bundle, err := buildDelegationBundleForFanout(fanoutBundleRequest{
		ProjectPath:      req.Project.Path,
		Cmd:              cmd,
		PlanID:           req.In.planID,
		TaskID:           req.TaskID,
		SliceID:          req.In.sliceID,
		Plan:             req.Plan,
		TargetTask:       req.TargetTask,
		Contract:         contract,
		WriteScope:       req.WriteScope,
		CreatedAtRFC3339: createdAtRFC3339,
	})
	if err != nil {
		contractPath := filepath.Join(delegationDir(req.Project.Path), req.TaskID+".yaml")
		_ = os.Remove(contractPath)
		return nil, err
	}
	if err := persistFanoutBundleWithBase(req.Project.Path, contract, bundle, req.BaseRes); err != nil {
		return nil, err
	}
	return contract, nil
}

// fanoutResolveBase reads the optional --base-branch flag and computes the §4.1
// base recommendation, translating a multi-dep conflict into a refusal error
// with a non-zero exit (done-criterion 4). It keeps the conflict-handling
// branches out of runWorkflowFanout to bound that function's complexity.
func fanoutResolveBase(cmd *cobra.Command, projectPath, planID, taskID string, deps []string) (baseResolution, error) {
	explicitBase, _ := cmd.Flags().GetString("base-branch")
	baseRes, err := resolveFanoutBase(projectPath, planID, taskID, deps, explicitBase, defaultGHPRLister)
	if err != nil {
		var conflict *multiDepConflict
		if errors.As(err, &conflict) {
			return baseResolution{}, fmt.Errorf("base-branch resolution refused: %w", err)
		}
		return baseResolution{}, err
	}
	return baseRes, nil
}

// resolveFanoutBase computes the §4.1 base recommendation for the target task.
// It queries the gh seam once for open PRs, joins them with canonical dep
// statuses, and runs resolveBase. A *multiDepConflict surfaces as a non-zero
// exit at the call site (§4.1 step 4b / done-criterion 4).
func resolveFanoutBase(projectPath, planID, taskID string, deps []string, explicitBase string, lister ghPRLister) (baseResolution, error) {
	openPRs, err := lister.ListOpenPRs(projectPath)
	if err != nil {
		// gh unavailable is non-fatal for the default-master path: if an
		// explicit base was given, honor it; otherwise fall back to master.
		if strings.TrimSpace(explicitBase) != "" {
			return resolveBase(baseResolutionInput{
				TaskID: taskID, PlanID: planID, DependsOn: deps, ExplicitBase: explicitBase,
			})
		}
		return masterResolution(), nil
	}
	status, branch := collectDepStatusAndBranch(projectPath, planID, deps, openPRs)
	return resolveBase(baseResolutionInput{
		TaskID:       taskID,
		PlanID:       planID,
		DependsOn:    deps,
		InFlight:     buildInFlightMap(status, branch, openPRs),
		ExplicitBase: explicitBase,
	})
}

func mustGetStringSlice(cmd *cobra.Command, name string) []string {
	if cmd.Flags().Lookup(name) == nil {
		return nil
	}
	s, err := cmd.Flags().GetStringSlice(name)
	if err != nil {
		return nil
	}
	return s
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

func reconcileDelegationContractForCloseout(projectPath, taskID, planID string) (*DelegationContract, error) {
	contract, err := loadDelegationContract(projectPath, taskID)
	if err != nil {
		return nil, fmt.Errorf("delegation contract for task %s not found: %w", taskID, err)
	}
	if contract.ParentPlanID != planID {
		return nil, fmt.Errorf("delegation plan_id %q does not match --plan %q", contract.ParentPlanID, planID)
	}
	if contract.Status != "completed" && contract.Status != "cancelled" {
		contract.Status = "completed"
		if err := saveDelegationContract(projectPath, contract); err != nil {
			return nil, fmt.Errorf("reconcile delegation status before closeout: %w", err)
		}
		contract, err = loadDelegationContract(projectPath, taskID)
		if err != nil {
			return nil, fmt.Errorf("reload delegation contract: %w", err)
		}
	}
	if contract.Status != "completed" {
		return nil, fmt.Errorf("delegation for task %s must be completed (run merge-back first); status is %q", taskID, contract.Status)
	}
	return contract, nil
}

func archiveCloseoutArtifacts(projectPath, taskID, planID, decision string, contract *DelegationContract, closeout workflowDelegationCloseoutRecord) (archiveDir, dateStr string, err error) {
	dateStr = time.Now().UTC().Format("2006-01-02")
	archiveDir = filepath.Join(projectPath, delegationAgentsDir, "history", planID, "delegate-merge-back-archive", dateStr, taskID)
	mergeBackSrc := filepath.Join(mergeBackDir(projectPath), taskID+".md")
	delegationSrc := filepath.Join(delegationDir(projectPath), taskID+".yaml")
	verificationSrcDir := filepath.Join(projectPath, delegationAgentsDir, "active", "verification", taskID)

	if err = copyWorkflowArtifact(mergeBackSrc, filepath.Join(archiveDir, "merge-back.md")); err != nil {
		return "", "", fmt.Errorf("archive merge-back: %w", err)
	}
	if err = copyWorkflowArtifact(delegationSrc, filepath.Join(archiveDir, "delegation.yaml")); err != nil {
		return "", "", fmt.Errorf("archive delegation contract: %w", err)
	}

	closeoutData, err := yamlMarshal(closeout)
	if err != nil {
		return "", "", err
	}
	if err = osWriteFile(filepath.Join(archiveDir, "closeout.yaml"), closeoutData, 0644); err != nil {
		return "", "", fmt.Errorf("write closeout record: %w", err)
	}

	if decision == "accept" {
		if st, err := os.Stat(verificationSrcDir); err == nil && st.IsDir() {
			if err := copyWorkflowDir(verificationSrcDir, filepath.Join(archiveDir, "verification")); err != nil {
				return "", "", fmt.Errorf("archive verification dir: %w", err)
			}
			if err := os.RemoveAll(verificationSrcDir); err != nil {
				return "", "", fmt.Errorf("remove active verification dir: %w", err)
			}
		}
	}

	_ = os.Remove(mergeBackSrc)
	_ = os.Remove(delegationSrc)
	bundlePath := filepath.Join(delegationBundlesDir(projectPath), contract.ID+".yaml")
	if _, err := os.Stat(bundlePath); err == nil {
		_ = os.Remove(bundlePath)
	}
	return archiveDir, dateStr, nil
}

func applyCloseoutDecisionToTasks(projectPath, planID, taskID string, closeout workflowDelegationCloseoutRecord) error {
	tf, err := loadCanonicalTasks(projectPath, planID)
	if err != nil {
		return fmt.Errorf("load canonical tasks: %w", err)
	}
	found := false
	for i := range tf.Tasks {
		if tf.Tasks[i].ID != taskID {
			continue
		}
		found = true
		switch closeout.Decision {
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
		return fmt.Errorf(errTaskNotFoundInPlanFmt, taskID, planID)
	}
	if err := saveCanonicalTasks(projectPath, tf); err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}

	plan, err := loadCanonicalPlan(projectPath, planID)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	plan.CurrentFocusTask = effectivePlanFocusTask(tf.Tasks)
	if allCanonicalTasksTerminal(tf.Tasks) {
		plan.Status = "completed"
	}
	if err := saveCanonicalPlan(projectPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	return nil
}

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

	contract, err := reconcileDelegationContractForCloseout(project.Path, taskID, planID)
	if err != nil {
		return err
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

	_, dateStr, err := archiveCloseoutArtifacts(project.Path, taskID, planID, decision, contract, closeout)
	if err != nil {
		return err
	}

	if err := applyCloseoutDecisionToTasks(project.Path, planID, taskID, closeout); err != nil {
		return err
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
