package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/credstore"
	"github.com/AGOrcha/dot-agents/internal/events"
	"go.yaml.in/yaml/v3"
)

// Base-resolution implements the layered-PR-fanout §4 algorithm: `da workflow
// fanout` computes the recommended base_branch for a new delegation bundle from
// the union of the task's depends_on PR branches (lineage-aware) instead of
// always branching off master. A downstream/fold task therefore branches off
// its dependency's open PR branch while that PR is still in review.
//
// Spec: .agents/workflow/specs/layered-pr-fanout/design.md §4.1 (algorithm),
// §4.2 (bundle schema additions), §4.3 (lineage-certificate).
//
// Open PRs are sourced through the unified internal/events PR producer
// (event.pr.* on the pr_source engine, pr-event-source spec) — NOT a bespoke
// `gh` seam. Base-resolution therefore stays platform-agnostic: a new PR host
// is a pr_source config block, zero Go here.

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

// openPR is the minimal open-pull-request projection base-resolution needs:
// the head branch a downstream task layers on and the PR number recorded in
// the lineage certificate. It is derived from a canonical events.PR payload so
// the resolver never depends on a platform-specific shape.
type openPR struct {
	Number int
	Branch string
}

// prSourceLister yields the currently open PRs for a project. It is the seam
// over the internal/events PR producer; the production implementation runs the
// configured pr_source producer for one cycle and projects its event.pr.*
// envelopes onto openPR. Tests inject a fake so no producer, gh binary, or
// network is required.
type prSourceLister interface {
	ListOpenPRs(projectPath string) ([]openPR, error)
}

// producerPRSourceLister is the production prSourceLister. It runs the default
// `gh` pr_source producer (pr-event-source D4) for a single cycle and maps the
// emitted event.pr.opened envelopes onto openPR. A nil fetcher uses the real
// exec/http fetcher; tests inject a fake fetcher to stay hermetic.
type producerPRSourceLister struct {
	source  events.PRSourceConfig
	fetcher events.Fetcher
}

// daServiceProxyEnv names the env var carrying the `da service` credential
// injector base (e.g. "http://localhost:8765"). When set, the auth seam routes
// fetches through the proxy so the service owns credentials/refresh; when unset
// the seam uses the direct-load fallback (pr-event-source D5; the proxy server
// side is deferred in §5.1 — the fallback is the live path today).
const daServiceProxyEnv = "DA_SERVICE_PROXY"

// newProducerPRSourceLister builds the production lister from the default `gh`
// pr_source config. The fetcher is an auth-aware DefaultFetcher whose HTTP client
// carries the pr-event-source D5 AuthRoundTripper, so an http-based pr_source
// (a non-gh platform) authenticates through the seam instead of issuing
// unauthenticated requests. The default `gh` source is exec-driven and ignores
// the HTTP client, but the seam is wired on the live path either way — so adding
// an http pr_source block is config, not Go, and it is authenticated.
func newProducerPRSourceLister() producerPRSourceLister {
	return producerPRSourceLister{
		source:  events.DefaultGHPRSource(),
		fetcher: newAuthAwareFetcher(),
	}
}

// newAuthAwareFetcher returns the production events.Fetcher with the D5 auth seam
// wired into its HTTP transport. Exec fetches (the `gh` default) bypass the
// transport; http fetches (any non-gh pr_source) flow through the
// AuthRoundTripper, which either routes to the da service injector (proxy path,
// when DA_SERVICE_PROXY is set) or attaches a credential inline via the
// external-agent-sources credential loader (direct-load fallback).
func newAuthAwareFetcher() events.Fetcher {
	return events.DefaultFetcher{
		Client: &http.Client{Transport: newPRAuthRoundTripper()},
	}
}

// newPRAuthRoundTripper builds the D5 AuthRoundTripper for PR fetches. ProxyBase
// comes from DA_SERVICE_PROXY (empty selects the direct-load fallback). The
// fallback Loader resolves a per-host credential through the external-agent-
// sources credstore chain; a missing credential is a clean miss (the request
// proceeds unauthenticated), never a hard error, so an unauthenticated public
// fetch still works.
func newPRAuthRoundTripper() *events.AuthRoundTripper {
	return &events.AuthRoundTripper{
		ProxyBase: strings.TrimSpace(os.Getenv(daServiceProxyEnv)),
		Loader:    credstoreHostLoader(credstore.NewLoader()),
	}
}

// credstoreHostLoader adapts the credstore.Loader (keyed by credential id) to the
// AuthRoundTripper's host-keyed CredentialLoader: the request host is the
// credential id. A not-found credential is reported as an empty token with no
// error, so the round tripper leaves the request unauthenticated rather than
// failing the whole base-resolution cycle. Only a hard resolver error
// (mis-permissioned secrets file, decrypt failure) surfaces.
func credstoreHostLoader(loader *credstore.Loader) events.CredentialLoader {
	return func(host string) (string, error) {
		if strings.TrimSpace(host) == "" {
			return "", nil
		}
		token, err := loader.Resolve(host)
		if err != nil {
			if errors.Is(err, credstore.ErrCredentialNotFound) {
				return "", nil
			}
			return "", err
		}
		return token, nil
	}
}

// ListOpenPRs runs one producer cycle and projects the emitted PR envelopes.
// Because a fresh producer treats every item as new, one cycle surfaces the
// full open-PR set. The first cycle of a fresh producer enumerates every open
// PR, which is exactly what base-resolution needs.
func (l producerPRSourceLister) ListOpenPRs(string) ([]openPR, error) {
	producer, err := l.source.NewListProducer(events.KindPROpened, l.source.Producer, l.fetcher)
	if err != nil {
		return nil, fmt.Errorf("pr_source producer: %w", err)
	}
	envs, err := producer.Cycle(context.Background())
	if err != nil {
		return nil, fmt.Errorf("pr_source cycle: %w", err)
	}
	return openPRsFromEnvelopes(envs)
}

// envelopePR is the slice of the canonical events.PR payload base-resolution
// consumes. The producer maps each source PR onto these fields via the
// pr_source field map, so this decode is platform-independent.
type envelopePR struct {
	Number int    `json:"number"`
	Branch string `json:"branch"`
}

// openPRsFromEnvelopes projects event.pr.* envelopes onto openPR. Envelopes for
// other kinds are ignored, and an entry with neither a number nor a branch is
// dropped (it cannot back a base). Decode errors surface so a malformed
// producer payload fails loud rather than silently resolving to master.
func openPRsFromEnvelopes(envs []events.Envelope) ([]openPR, error) {
	out := make([]openPR, 0, len(envs))
	for _, env := range envs {
		if env.Namespace() != events.PRNamespace {
			continue
		}
		var p envelopePR
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return nil, fmt.Errorf("decode pr envelope %q: %w", env.IdempotencyKey, err)
		}
		if p.Number == 0 && strings.TrimSpace(p.Branch) == "" {
			continue
		}
		out = append(out, openPR{Number: p.Number, Branch: p.Branch})
	}
	return out, nil
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
// PR producer to produce the §4.1 in_flight_tasks map. depStatus maps a
// qualified dep id to its canonical status; depBranch maps it to the head
// branch the task pushed. A dep contributes a PR number only when an open PR
// exists for its branch.
func buildInFlightMap(depStatus, depBranch map[string]string, openPRs []openPR) map[string]inFlightTask {
	prByBranch := make(map[string]int, len(openPRs))
	for _, pr := range openPRs {
		if pr.Branch != "" {
			prByBranch[pr.Branch] = pr.Number
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
