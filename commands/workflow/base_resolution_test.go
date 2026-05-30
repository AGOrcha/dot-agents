package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// fakeGHPRLister is a hermetic ghPRLister for tests — no gh binary, no network.
type fakeGHPRLister struct {
	prs []ghPR
	err error
}

func (f fakeGHPRLister) ListOpenPRs(string) ([]ghPR, error) { return f.prs, f.err }

func TestQualifyDepID(t *testing.T) {
	if got := qualifyDepID("t1", "planA"); got != "planA/t1" {
		t.Fatalf("intra-plan qualify = %q", got)
	}
	if got := qualifyDepID("planB/t2", "planA"); got != "planB/t2" {
		t.Fatalf("cross-plan qualify must be unchanged = %q", got)
	}
}

func TestSplitQualifiedDep(t *testing.T) {
	p, tk := splitQualifiedDep("planA/t1")
	if p != "planA" || tk != "t1" {
		t.Fatalf("split = %q/%q", p, tk)
	}
	p, tk = splitQualifiedDep("bareid")
	if p != "" || tk != "bareid" {
		t.Fatalf("split no-slash = %q/%q", p, tk)
	}
}

func TestResolveBaseNoDepsIsMaster(t *testing.T) {
	res, err := resolveBase(baseResolutionInput{TaskID: "t", PlanID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster || res.BasePR != 0 || res.Lineage != nil {
		t.Fatalf("expected bare master, got %+v", res)
	}
}

func TestResolveBaseAllDepsMergedIsMaster(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "completed"},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("merged dep should base off master, got %q", res.BaseBranch)
	}
}

func TestResolveBaseSingleAwaitingDep(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/d1" || res.BasePR != 205 || res.BaseTask != "p/d1" {
		t.Fatalf("single-dep base wrong: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "p/d1" {
		t.Fatalf("expected lineage certificate, got %+v", res.Lineage)
	}
}

func TestResolveBaseSingleAwaitingSubStatus(t *testing.T) {
	for _, status := range []string{"awaiting_agent_review", "awaiting_owner_review"} {
		in := baseResolutionInput{
			TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
			InFlight: map[string]inFlightTask{
				"p/d1": {Status: status, PRBranch: "feature/d1", PRNumber: 9},
			},
		}
		res, err := resolveBase(in)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if res.BaseBranch != "feature/d1" {
			t.Fatalf("%s: base = %q", status, res.BaseBranch)
		}
	}
}

func TestResolveBaseMultiDepSameBranch(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1", "d2"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/shared", PRNumber: 1},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/shared", PRNumber: 1},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("same-branch multi-dep should not conflict: %v", err)
	}
	if res.BaseBranch != "feature/shared" {
		t.Fatalf("base = %q", res.BaseBranch)
	}
}

func TestResolveBaseMultiDepDistinctBranchesRefuses(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d2", "d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/d2", PRNumber: 206},
		},
	}
	_, err := resolveBase(in)
	var conflict *multiDepConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected multiDepConflict, got %v", err)
	}
	// Conflict set must be sorted and name both deps.
	if got := conflict.conflictTasks; len(got) != 2 || got[0] != "p/d1" || got[1] != "p/d2" {
		t.Fatalf("conflict set = %+v", got)
	}
	if !strings.Contains(conflict.Error(), "--base-branch") {
		t.Fatalf("error should prompt --base-branch: %q", conflict.Error())
	}
}

func TestResolveBaseExplicitBaseShortCircuits(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1", "d2"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
			"p/d2": {Status: "awaiting_review", PRBranch: "feature/d2", PRNumber: 206},
		},
		ExplicitBase: "feature/d2",
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatalf("explicit base must override conflict: %v", err)
	}
	if res.BaseBranch != "feature/d2" || res.BasePR != 206 || res.BaseTask != "p/d2" {
		t.Fatalf("explicit base resolution wrong: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "p/d2" {
		t.Fatalf("explicit lineage = %+v", res.Lineage)
	}
}

func TestResolveBaseExplicitBaseUnknownBranch(t *testing.T) {
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "feature/d1", PRNumber: 205},
		},
		ExplicitBase: "feature/manual-merge",
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/manual-merge" || res.BasePR != 0 || res.BaseTask != "" {
		t.Fatalf("unknown explicit branch should carry no PR: %+v", res)
	}
	if res.Lineage == nil || res.Lineage.SelectedTask != "" {
		t.Fatalf("lineage should record manual rationale, got %+v", res.Lineage)
	}
}

func TestResolveBaseSkipsDepWithoutBranch(t *testing.T) {
	// Dep is awaiting_review but has no PR branch (gh found no open PR) — it
	// must not count as an in-flight base, so resolution falls back to master.
	in := baseResolutionInput{
		TaskID: "t", PlanID: "p", DependsOn: []string{"d1"},
		InFlight: map[string]inFlightTask{
			"p/d1": {Status: "awaiting_review", PRBranch: "", PRNumber: 0},
		},
	}
	res, err := resolveBase(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("branchless awaiting dep should fall back to master, got %q", res.BaseBranch)
	}
}

func TestBuildInFlightMapJoinsPRs(t *testing.T) {
	depStatus := map[string]string{"p/d1": "awaiting_review", "p/d2": "in_progress"}
	depBranch := map[string]string{"p/d1": "feature/d1", "p/d2": "feature/d2"}
	openPRs := []ghPR{{Number: 205, HeadRefName: "feature/d1"}}
	got := buildInFlightMap(depStatus, depBranch, openPRs)

	if f := got["p/d1"]; f.PRBranch != "feature/d1" || f.PRNumber != 205 {
		t.Fatalf("d1 join = %+v", f)
	}
	// d2 has a branch name but no open PR → no PR metadata attached.
	if f := got["p/d2"]; f.PRBranch != "" || f.PRNumber != 0 || f.Status != "in_progress" {
		t.Fatalf("d2 should have no PR metadata: %+v", f)
	}
}

func TestBuildInFlightMapIgnoresBlankPRHead(t *testing.T) {
	got := buildInFlightMap(
		map[string]string{"p/d1": "awaiting_review"},
		map[string]string{"p/d1": "feature/d1"},
		[]ghPR{{Number: 5, HeadRefName: ""}},
	)
	if f := got["p/d1"]; f.PRNumber != 0 {
		t.Fatalf("blank PR head must not match: %+v", f)
	}
}

func TestParseGHPRList(t *testing.T) {
	prs, err := parseGHPRList([]byte(`[{"number":205,"headRefName":"feature/d1"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 205 || prs[0].HeadRefName != "feature/d1" {
		t.Fatalf("parsed = %+v", prs)
	}

	empty, err := parseGHPRList([]byte("  \n"))
	if err != nil || empty != nil {
		t.Fatalf("empty output should yield nil,nil: %+v, %v", empty, err)
	}

	if _, err := parseGHPRList([]byte("{not json")); err == nil {
		t.Fatal("expected parse error on malformed JSON")
	}
}

func TestExecGHPRListerSatisfiesSeam(t *testing.T) {
	// Compile-time assurance the production lister satisfies the seam interface,
	// and that nil output is treated as empty (no open PRs) rather than erroring.
	var _ ghPRLister = execGHPRLister{}
	if _, err := parseGHPRList(nil); err != nil {
		t.Fatalf("nil output should be treated as empty: %v", err)
	}
}

func baseTestBundle() *delegationBundleYAML {
	b := &delegationBundleYAML{SchemaVersion: 1, DelegationID: "del-x", PlanID: "p", TaskID: "t"}
	b.Scope.WriteScope = []string{"a.go"}
	return b
}

func TestMarshalBundleWithBaseInjectsScopeFields(t *testing.T) {
	res := &baseResolution{
		BaseBranch: "feature/d1",
		BasePR:     205,
		BaseTask:   "p/d1",
		Lineage:    &lineageCertificate{SourceTasks: []string{"p/d1"}, SelectedTask: "p/d1", Rationale: "x"},
	}
	data, err := marshalBundleWithBase(baseTestBundle(), res)
	if err != nil {
		t.Fatal(err)
	}
	var rt map[string]any
	if err := yaml.Unmarshal(data, &rt); err != nil {
		t.Fatal(err)
	}
	scope, ok := rt["scope"].(map[string]any)
	if !ok {
		t.Fatalf("scope not a mapping: %T", rt["scope"])
	}
	if scope["base_branch"] != "feature/d1" || scope["base_pr"] != 205 || scope["base_task"] != "p/d1" {
		t.Fatalf("scope base fields = %+v", scope)
	}
	if scope["lineage"] == nil {
		t.Fatalf("expected lineage in scope: %+v", scope)
	}
	// write_scope must survive the injection.
	if ws, _ := scope["write_scope"].([]any); len(ws) != 1 || ws[0] != "a.go" {
		t.Fatalf("write_scope clobbered: %+v", scope["write_scope"])
	}
}

func TestMarshalBundleWithBasePlainMasterUnchanged(t *testing.T) {
	plain, err := yamlMarshal(baseTestBundle())
	if err != nil {
		t.Fatal(err)
	}
	got, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: baseRefMaster})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("bare master must not augment scope:\n%s", got)
	}
	// nil resolution is also unchanged.
	gotNil, err := marshalBundleWithBase(baseTestBundle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotNil) != string(plain) {
		t.Fatalf("nil resolution must not augment scope:\n%s", gotNil)
	}
}

func TestMarshalBundleWithBaseMasterWithPRIsLayered(t *testing.T) {
	// A master base that still carries a PR/lineage is recorded (observability).
	res := &baseResolution{BaseBranch: baseRefMaster, BasePR: 7}
	data, err := marshalBundleWithBase(baseTestBundle(), res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "base_pr: 7") {
		t.Fatalf("master+PR should record base_pr:\n%s", data)
	}
}

func TestMarshalBundleWithBaseMarshalError(t *testing.T) {
	prev := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected marshal error to propagate")
	}
}

func TestBaseResolutionIsLayered(t *testing.T) {
	cases := []struct {
		name string
		res  *baseResolution
		want bool
	}{
		{"nil", nil, false},
		{"empty-branch", &baseResolution{}, false},
		{"bare-master", &baseResolution{BaseBranch: baseRefMaster}, false},
		{"master-with-pr", &baseResolution{BaseBranch: baseRefMaster, BasePR: 3}, true},
		{"master-with-lineage", &baseResolution{BaseBranch: baseRefMaster, Lineage: &lineageCertificate{}}, true},
		{"feature-branch", &baseResolution{BaseBranch: "feature/x"}, true},
	}
	for _, c := range cases {
		if got := baseResolutionIsLayered(c.res); got != c.want {
			t.Fatalf("%s: layered = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestReplaceMappingValueErrors(t *testing.T) {
	scope := bundleScopeWithBase{WriteScope: []string{"a.go"}}
	// Non-mapping root.
	scalar := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	if err := replaceMappingValue(scalar, "scope", scope); err == nil {
		t.Fatal("expected non-mapping error")
	}
	// Missing key.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("foo: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if err := replaceMappingValue(&doc, "scope", scope); err == nil {
		t.Fatal("expected missing-key error")
	}
	// Happy path: existing key is replaced.
	var ok yaml.Node
	if err := yaml.Unmarshal([]byte("scope: {}\n"), &ok); err != nil {
		t.Fatal(err)
	}
	if err := replaceMappingValue(&ok, "scope", scope); err != nil {
		t.Fatalf("replace existing key: %v", err)
	}
}

func TestDocumentMappingShapes(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\n"), &doc); err != nil {
		t.Fatal(err)
	}
	if documentMapping(&doc) == nil {
		t.Fatal("expected mapping for document node")
	}
	if documentMapping(&yaml.Node{Kind: yaml.ScalarNode}) != nil {
		t.Fatal("scalar must not be a mapping")
	}
}

// ─── delegation.go fanout base-resolution integration ────────────────────────

func TestDepBranchForTask(t *testing.T) {
	prs := []ghPR{
		{Number: 205, HeadRefName: "feature/config-v2-d1"},
		{Number: 206, HeadRefName: ""},
	}
	if got := depBranchForTask("d1", prs); got != "feature/config-v2-d1" {
		t.Fatalf("matched branch = %q", got)
	}
	if got := depBranchForTask("nope", prs); got != "" {
		t.Fatalf("no-match should be empty, got %q", got)
	}
}

func TestCollectDepStatusAndBranch(t *testing.T) {
	repo := setupTestProject(t)
	// Mark task-002 awaiting_review so it is a candidate dep.
	tf, err := loadCanonicalTasks(repo, "plan-001")
	if err != nil {
		t.Fatal(err)
	}
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	openPRs := []ghPR{{Number: 206, HeadRefName: "feature/task-002"}}

	status, branch := collectDepStatusAndBranch(repo, "plan-001", []string{"task-002", "missing"}, openPRs)
	if status["plan-001/task-002"] != "awaiting_review" {
		t.Fatalf("dep status = %+v", status)
	}
	if branch["plan-001/task-002"] != "feature/task-002" {
		t.Fatalf("dep branch = %+v", branch)
	}
	// Unknown dep must be absent (no status entry → not in-flight).
	if _, ok := status["plan-001/missing"]; ok {
		t.Fatalf("missing dep should not be recorded: %+v", status)
	}
}

func TestResolveFanoutBaseSingleDep(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	lister := fakeGHPRLister{prs: []ghPR{{Number: 206, HeadRefName: "feature/task-002"}}}

	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/task-002" || res.BasePR != 206 || res.BaseTask != "plan-001/task-002" {
		t.Fatalf("resolved base = %+v", res)
	}
}

func TestResolveFanoutBaseGHErrorFallsBackToMaster(t *testing.T) {
	repo := setupTestProject(t)
	lister := fakeGHPRLister{err: errors.New("gh not installed")}
	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != baseRefMaster {
		t.Fatalf("gh error should fall back to master, got %q", res.BaseBranch)
	}
}

func TestResolveFanoutBaseGHErrorHonorsExplicitBase(t *testing.T) {
	repo := setupTestProject(t)
	lister := fakeGHPRLister{err: errors.New("gh down")}
	res, err := resolveFanoutBase(repo, "plan-001", "task-001", []string{"task-002"}, "feature/manual", lister)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/manual" {
		t.Fatalf("explicit base must survive gh error, got %q", res.BaseBranch)
	}
}

func TestResolveFanoutBaseMultiDepConflict(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	lister := fakeGHPRLister{prs: []ghPR{
		{Number: 205, HeadRefName: "feature/task-001"},
		{Number: 206, HeadRefName: "feature/task-002"},
	}}
	_, err := resolveFanoutBase(repo, "plan-001", "task-new", []string{"task-001", "task-002"}, "", lister)
	var conflict *multiDepConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("expected multiDepConflict, got %v", err)
	}
}

func newBaseFlagCmd(t *testing.T, base string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "fanout"}
	cmd.Flags().String("base-branch", "", "")
	if base != "" {
		if err := cmd.Flags().Set("base-branch", base); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

func TestFanoutResolveBaseSingleDep(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultGHPRLister
	defaultGHPRLister = fakeGHPRLister{prs: []ghPR{{Number: 206, HeadRefName: "feature/task-002"}}}
	t.Cleanup(func() { defaultGHPRLister = prev })

	res, err := fanoutResolveBase(newBaseFlagCmd(t, ""), repo, "plan-001", "task-001", []string{"task-002"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseBranch != "feature/task-002" {
		t.Fatalf("base = %q", res.BaseBranch)
	}
}

func TestFanoutResolveBaseConflictRefuses(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultGHPRLister
	defaultGHPRLister = fakeGHPRLister{prs: []ghPR{
		{Number: 205, HeadRefName: "feature/task-001"},
		{Number: 206, HeadRefName: "feature/task-002"},
	}}
	t.Cleanup(func() { defaultGHPRLister = prev })

	_, err := fanoutResolveBase(newBaseFlagCmd(t, ""), repo, "plan-001", "task-new", []string{"task-001", "task-002"})
	if err == nil || !strings.Contains(err.Error(), "base-branch resolution refused") {
		t.Fatalf("expected refusal error, got %v", err)
	}
}

func TestFanoutResolveBaseExplicitFlagOverridesConflict(t *testing.T) {
	repo := setupTestProject(t)
	tf, _ := loadCanonicalTasks(repo, "plan-001")
	tf.Tasks[0].Status = "awaiting_review"
	tf.Tasks[1].Status = "awaiting_review"
	if err := saveCanonicalTasks(repo, tf); err != nil {
		t.Fatal(err)
	}
	prev := defaultGHPRLister
	defaultGHPRLister = fakeGHPRLister{prs: []ghPR{
		{Number: 205, HeadRefName: "feature/task-001"},
		{Number: 206, HeadRefName: "feature/task-002"},
	}}
	t.Cleanup(func() { defaultGHPRLister = prev })

	res, err := fanoutResolveBase(newBaseFlagCmd(t, "feature/task-001"), repo, "plan-001", "task-new", []string{"task-001", "task-002"})
	if err != nil {
		t.Fatalf("explicit flag must override conflict: %v", err)
	}
	if res.BaseBranch != "feature/task-001" || res.BasePR != 205 {
		t.Fatalf("explicit resolution = %+v", res)
	}
}

func TestFanoutBaseSummary(t *testing.T) {
	withPR := baseResolution{BaseBranch: "feature/d1", BasePR: 205, BaseTask: "p/d1"}
	if got := fanoutBaseSummary(withPR); got != "feature/d1 (PR #205, from p/d1)" {
		t.Fatalf("summary = %q", got)
	}
	bare := baseResolution{BaseBranch: baseRefMaster}
	if got := fanoutBaseSummary(bare); got != baseRefMaster {
		t.Fatalf("bare summary = %q", got)
	}
}

func TestSaveDelegationBundleWithBase(t *testing.T) {
	repo := t.TempDir()
	res := &baseResolution{BaseBranch: "feature/d1", BasePR: 205, BaseTask: "p/d1"}
	if err := saveDelegationBundleWithBase(repo, baseTestBundle(), res); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".agents", "active", "delegation-bundles", "del-x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "base_branch: feature/d1") {
		t.Fatalf("persisted bundle missing base_branch:\n%s", data)
	}
}

func TestSaveDelegationBundleWithBaseEmptyID(t *testing.T) {
	b := &delegationBundleYAML{}
	if err := saveDelegationBundleWithBase(t.TempDir(), b, nil); err == nil {
		t.Fatal("expected empty delegation_id error")
	}
}

func TestSaveDelegationBundleWithBaseMkdirError(t *testing.T) {
	prev := osMkdirAll
	osMkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	t.Cleanup(func() { osMkdirAll = prev })
	if err := saveDelegationBundleWithBase(t.TempDir(), baseTestBundle(), nil); err == nil {
		t.Fatal("expected mkdir error to propagate")
	}
}

func TestMarshalBundleWithBaseReparseError(t *testing.T) {
	// First yamlMarshal call (struct → bytes) succeeds with bytes that are not
	// a valid YAML mapping, forcing the re-parse/injection path to fail.
	prev := yamlMarshal
	calls := 0
	yamlMarshal = func(v any) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("\tnot: [valid"), nil
		}
		return prev(v)
	}
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected re-parse error")
	}
}

func TestMarshalBundleWithBaseNonMappingRoot(t *testing.T) {
	prev := yamlMarshal
	yamlMarshal = func(any) ([]byte, error) { return []byte("- just\n- a\n- list\n"), nil }
	t.Cleanup(func() { yamlMarshal = prev })
	if _, err := marshalBundleWithBase(baseTestBundle(), &baseResolution{BaseBranch: "feature/d1"}); err == nil {
		t.Fatal("expected non-mapping root error")
	}
}

func TestCollectDepStatusAndBranchUnloadablePlan(t *testing.T) {
	repo := setupTestProject(t)
	// Cross-plan dep into a plan that does not exist → loadErr branch; the dep
	// is silently skipped (treated as not-in-flight).
	status, branch := collectDepStatusAndBranch(repo, "plan-001", []string{"ghost-plan/x"}, nil)
	if len(status) != 0 || len(branch) != 0 {
		t.Fatalf("unloadable plan dep must be skipped: %+v / %+v", status, branch)
	}
}
