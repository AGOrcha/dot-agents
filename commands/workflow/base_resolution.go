package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

// Base-resolution implements the layered-PR-fanout §4 algorithm: `da workflow
// fanout` computes the recommended base_branch for a new delegation bundle from
// the union of the task's depends_on PR branches (lineage-aware) instead of
// always branching off master. A downstream/fold task therefore branches off
// its dependency's open PR branch while that PR is still in review.
//
// Spec: .agents/workflow/specs/layered-pr-fanout/design.md §4.1 (algorithm),
// §4.2 (bundle schema additions), §4.3 (lineage-certificate).

// baseRefMaster is the default base when no in-flight dependency PR backs the
// new task (today's behavior; §4.2 "When base_branch is omitted, the worker
// defaults to master").
const baseRefMaster = "master"

// awaitingReviewStatuses are the dep statuses that make a dep's PR branch a
// candidate base. A dep in any awaiting_review sub-status (§2.2) has an open PR
// whose branch a downstream task can layer on top of (§4.1 step 3).
var awaitingReviewStatuses = map[string]bool{
	"awaiting_review":       true,
	"awaiting_agent_review": true,
	"awaiting_owner_review": true,
}

// inFlightTask is the per-dep view the algorithm consumes (§4.1 input
// `in_flight_tasks : { task_id → status, pr_branch, pr_number }`).
type inFlightTask struct {
	Status   string
	PRBranch string
	PRNumber int
}

// ghPR is one open pull request as reported by the gh seam.
type ghPR struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
}

// ghPRLister is the seam over `gh pr list`. It is an interface so tests stay
// hermetic — no network, no gh binary. The production implementation shells out
// to gh; tests inject a fake.
type ghPRLister interface {
	ListOpenPRs(projectPath string) ([]ghPR, error)
}

// execGHPRLister is the production ghPRLister: it runs
// `gh pr list --state open --json number,headRefName`.
type execGHPRLister struct{}

func (execGHPRLister) ListOpenPRs(projectPath string) ([]ghPR, error) {
	out, err := execabs.Command(
		"gh", "pr", "list",
		"--state", "open",
		"--json", "number,headRefName",
		"--limit", "200",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	return parseGHPRList(out)
}

// parseGHPRList decodes `gh pr list --json number,headRefName` output. Empty
// output (no open PRs) yields a nil slice, not an error.
func parseGHPRList(out []byte) ([]ghPR, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	var prs []ghPR
	if err := json.Unmarshal([]byte(trimmed), &prs); err != nil {
		return nil, fmt.Errorf("parse gh pr list output: %w", err)
	}
	return prs, nil
}

// lineageCertificate is the serialized justification for choosing a layered
// base over master (§4.3). In v1 it is optional observability metadata; v2
// makes it a hard precondition for auto-sequencing.
type lineageCertificate struct {
	// SourceTasks are the dep task ids whose PR branches were considered.
	SourceTasks []string `yaml:"source_tasks"`
	// SelectedTask is the dep whose PR branch was chosen as the base.
	SelectedTask string `yaml:"selected_task,omitempty"`
	// Rationale is a human-readable explanation of the selection (§4.1 step 5).
	Rationale string `yaml:"rationale"`
}

// baseResolution is the §4.1 step-5 emission:
// base_recommendation = { base_branch, base_pr, base_task, lineage_certificate? }.
type baseResolution struct {
	BaseBranch string
	BasePR     int
	BaseTask   string
	Lineage    *lineageCertificate
}

// baseResolutionInput carries the §4.1 inputs.
type baseResolutionInput struct {
	// TaskID is the new task being fanned out.
	TaskID string
	// PlanID is the new task's plan, used to qualify intra-plan dep ids so
	// base_task is always recorded as "<plan>/<task>".
	PlanID string
	// DependsOn is the dep id set (may include cross-plan "<plan>/<task>" ids).
	DependsOn []string
	// InFlight maps each qualified dep id to its status + PR metadata.
	InFlight map[string]inFlightTask
	// ExplicitBase, when non-empty, is the operator-supplied --base-branch that
	// short-circuits resolution (§2.5 v1 manual sequencing escape hatch).
	ExplicitBase string
}

// qualifyDepID normalizes a dep id to "<plan>/<task>" form. Intra-plan deps
// (no "/") are qualified with the new task's plan so base_task is unambiguous.
func qualifyDepID(dep, planID string) string {
	if strings.Contains(dep, "/") {
		return dep
	}
	return planID + "/" + dep
}

// multiDepConflict is returned by resolveBase when multiple deps are in
// awaiting_review on distinct branches and no --base-branch was given (§4.1
// step 4b: v1 refuses and surfaces the conflict set). Callers turn it into a
// non-zero-exit sequencing prompt.
type multiDepConflict struct {
	conflictTasks []string
}

func (e *multiDepConflict) Error() string {
	return fmt.Sprintf(
		"multiple in-flight deps on distinct PR branches (%s); "+
			"pass --base-branch to sequence them explicitly",
		strings.Join(e.conflictTasks, ", "),
	)
}

// awaitingDepRefs collects the qualified ids of deps currently in an
// awaiting_review status, paired with their distinct PR branches.
func awaitingDepRefs(in baseResolutionInput) (refs []string, branches map[string]inFlightTask) {
	branches = make(map[string]inFlightTask)
	for _, dep := range in.DependsOn {
		qid := qualifyDepID(dep, in.PlanID)
		f, ok := in.InFlight[qid]
		if !ok || !awaitingReviewStatuses[f.Status] || strings.TrimSpace(f.PRBranch) == "" {
			continue
		}
		refs = append(refs, qid)
		branches[qid] = f
	}
	sort.Strings(refs)
	return refs, branches
}

// distinctBranchCount returns how many distinct PR branches the given
// awaiting deps occupy.
func distinctBranchCount(refs []string, branches map[string]inFlightTask) int {
	seen := make(map[string]bool)
	for _, r := range refs {
		seen[branches[r].PRBranch] = true
	}
	return len(seen)
}

// resolveBase implements §4.1. It returns the recommended base, or a
// *multiDepConflict when v1 must refuse and require explicit sequencing.
func resolveBase(in baseResolutionInput) (baseResolution, error) {
	if b := strings.TrimSpace(in.ExplicitBase); b != "" {
		return explicitBaseResolution(in, b), nil
	}

	refs, branches := awaitingDepRefs(in)
	if len(refs) == 0 {
		// No in-flight dep PRs: either all deps merged (step 2) or there are no
		// deps at all. Either way, base off master.
		return masterResolution(), nil
	}
	if len(refs) == 1 {
		return singleDepResolution(refs[0], branches[refs[0]]), nil
	}
	if distinctBranchCount(refs, branches) == 1 {
		// All awaiting deps share one branch — unambiguous (step 3 degenerate).
		return singleDepResolution(refs[0], branches[refs[0]]), nil
	}
	// Step 4b: v1 refuses on multiple distinct branches.
	return baseResolution{}, &multiDepConflict{conflictTasks: refs}
}

func explicitBaseResolution(in baseResolutionInput, base string) baseResolution {
	res := baseResolution{
		BaseBranch: base,
		Lineage: &lineageCertificate{
			SourceTasks: append([]string(nil), in.DependsOn...),
			Rationale:   "operator supplied --base-branch; manual sequencing (spec §2.5 v1)",
		},
	}
	// If the explicit base matches a known dep PR branch, record its number/task.
	for _, dep := range in.DependsOn {
		qid := qualifyDepID(dep, in.PlanID)
		if f, ok := in.InFlight[qid]; ok && f.PRBranch == base {
			res.BasePR = f.PRNumber
			res.BaseTask = qid
			res.Lineage.SelectedTask = qid
			break
		}
	}
	return res
}

func masterResolution() baseResolution {
	return baseResolution{BaseBranch: baseRefMaster}
}

func singleDepResolution(depID string, f inFlightTask) baseResolution {
	return baseResolution{
		BaseBranch: f.PRBranch,
		BasePR:     f.PRNumber,
		BaseTask:   depID,
		Lineage: &lineageCertificate{
			SourceTasks:  []string{depID},
			SelectedTask: depID,
			Rationale: fmt.Sprintf(
				"single in-flight dep %s in review; branch off its open PR #%d (spec §4.1 step 3)",
				depID, f.PRNumber,
			),
		},
	}
}

// buildInFlightMap joins canonical dep statuses with open-PR metadata from the
// gh seam to produce the §4.1 in_flight_tasks map. depStatus maps a qualified
// dep id to its canonical status; depBranch maps it to the head branch the
// task pushed. A dep contributes a PR number only when an open PR exists for
// its branch.
func buildInFlightMap(depStatus, depBranch map[string]string, openPRs []ghPR) map[string]inFlightTask {
	prByBranch := make(map[string]int, len(openPRs))
	for _, pr := range openPRs {
		if pr.HeadRefName != "" {
			prByBranch[pr.HeadRefName] = pr.Number
		}
	}
	out := make(map[string]inFlightTask, len(depStatus))
	for id, status := range depStatus {
		f := inFlightTask{Status: status}
		if br := strings.TrimSpace(depBranch[id]); br != "" {
			if num, ok := prByBranch[br]; ok {
				f.PRBranch = br
				f.PRNumber = num
			}
		}
		out[id] = f
	}
	return out
}

// bundleScopeWithBase is the §4.2 scope block augmented with the base-resolution
// fields. It is marshaled into the bundle's `scope` mapping by
// marshalBundleWithBase so the canonical delegationBundleYAML.Scope struct does
// not need to grow these fields inline.
type bundleScopeWithBase struct {
	WriteScope  []string            `yaml:"write_scope"`
	Constraints []string            `yaml:"constraints,omitempty"`
	BaseBranch  string              `yaml:"base_branch,omitempty"`
	BasePR      int                 `yaml:"base_pr,omitempty"`
	BaseTask    string              `yaml:"base_task,omitempty"`
	Lineage     *lineageCertificate `yaml:"lineage,omitempty"`
}

// marshalBundleWithBase marshals the bundle and injects the §4.2 base-resolution
// fields under `scope`. When res is nil or resolves to plain master with no PR,
// the scope block is left unchanged (backward-compatible: older workers and the
// default-master path see today's shape).
func marshalBundleWithBase(b *delegationBundleYAML, res *baseResolution) ([]byte, error) {
	data, err := yamlMarshal(b)
	if err != nil {
		return nil, err
	}
	if !baseResolutionIsLayered(res) {
		return data, nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("re-parse bundle for base injection: %w", err)
	}
	scope := bundleScopeWithBase{
		WriteScope:  b.Scope.WriteScope,
		Constraints: b.Scope.Constraints,
		BaseBranch:  res.BaseBranch,
		BasePR:      res.BasePR,
		BaseTask:    res.BaseTask,
		Lineage:     res.Lineage,
	}
	if err := replaceMappingValue(&root, "scope", scope); err != nil {
		return nil, err
	}
	return yamlMarshal(&root)
}

// baseResolutionIsLayered reports whether res selects a non-master base (or a
// master base that still carries a PR/lineage worth recording). A nil res or a
// bare master selection is not layered and needs no scope augmentation.
func baseResolutionIsLayered(res *baseResolution) bool {
	if res == nil {
		return false
	}
	if res.BaseBranch == "" || res.BaseBranch == baseRefMaster {
		return res.BasePR != 0 || res.Lineage != nil
	}
	return true
}

// replaceMappingValue swaps the value node for key in the document's top-level
// mapping with a freshly encoded value. It errors when the document is not a
// mapping or the key is absent — both indicate a malformed bundle. The value is
// always a concrete bundleScopeWithBase, so encoding cannot fail.
func replaceMappingValue(root *yaml.Node, key string, value bundleScopeWithBase) error {
	mapping := documentMapping(root)
	if mapping == nil {
		return fmt.Errorf("bundle root is not a mapping")
	}
	var encoded yaml.Node
	_ = encoded.Encode(value)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = &encoded
			return nil
		}
	}
	return fmt.Errorf("bundle has no %q key", key)
}

// documentMapping unwraps a document node to its top-level mapping node, or
// returns nil when the shape is not a mapping.
func documentMapping(root *yaml.Node) *yaml.Node {
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}
