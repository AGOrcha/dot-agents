package workflow

import (
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

type DelegationContract struct {
	SchemaVersion            int                `json:"schema_version" yaml:"schema_version"`
	ID                       string             `json:"id" yaml:"id"`
	ParentPlanID             string             `json:"parent_plan_id" yaml:"parent_plan_id"`
	ParentTaskID             string             `json:"parent_task_id" yaml:"parent_task_id"`
	Title                    string             `json:"title" yaml:"title"`
	Summary                  string             `json:"summary" yaml:"summary"`
	WriteScope               []string           `json:"write_scope" yaml:"write_scope"`
	SuccessCriteria          string             `json:"success_criteria" yaml:"success_criteria"`
	VerificationExpectations string             `json:"verification_expectations" yaml:"verification_expectations"`
	MayMutateWorkflowState   bool               `json:"may_mutate_workflow_state" yaml:"may_mutate_workflow_state"`
	Owner                    string             `json:"owner" yaml:"owner"`
	Status                   string             `json:"status" yaml:"status"`
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
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

	if _, err := loadDelegationContract(project.Path, taskID); err == nil {
		return fmt.Errorf("task %s already has an active delegation contract", taskID)
	}

	if writeScopeExplicit {
		writeScope = writeScope[:0]
		for _, p := range strings.Split(writeScopeCSV, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				writeScope = append(writeScope, p)
			}
		}
	}
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

	var filesChanged []string
	gitOut, err := exec.Command("git", "-C", project.Path, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(gitOut)), "\n") {
			if f != "" {
				filesChanged = append(filesChanged, f)
			}
		}
	}

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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, b.DelegationID+".yaml"), data, 0644)
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

	// workflow merge-back marks the delegation completed when merge-back is written; workers that drop
	// merge-back/*.md without invoking that command can leave status as active/pending/failed.
	if contract.Status != "completed" && contract.Status != "cancelled" {
		contract.Status = "completed"
		if err := saveDelegationContract(project.Path, contract); err != nil {
			return fmt.Errorf("reconcile delegation status before closeout: %w", err)
		}
		contract, err = loadDelegationContract(project.Path, taskID)
		if err != nil {
			return fmt.Errorf("reload delegation contract: %w", err)
		}
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
