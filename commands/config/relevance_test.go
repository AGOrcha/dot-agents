package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// relevanceRepoBody is a fixture .agentsrc.json carrying a fully-populated
// execution_profile layer with two app_types so the resolver, facet builders,
// and renderers all have non-empty data to exercise.
const relevanceRepoBody = `{
  "repo_id": "fixture",
  "execution_profile": {
    "by_app_type": {
      "go-cli": {
        "relevance": {
          "orchestrate": {
            "core": ["orchestrator-session-start", "loop-worker"],
            "noise": ["article-extract", "playwright"]
          },
          "review": {
            "core": ["review-pr"],
            "situational": ["self-review"]
          }
        },
        "topology": {
          "executors": 1,
          "verifiers_per_executor": 2,
          "reviewers": "per_verifier",
          "verifier_sequence": ["unit", "cli-runner"]
        },
        "lenses": {
          "lens_set": ["architecture-standards", "adversarial"],
          "lens_concurrency": "gated"
        },
        "graph_backend": "dotagents-builtin:graph/none@^1.0"
      },
      "ideation": {
        "topology": {"executors": 3, "verifiers_per_executor": 0, "reviewers": "0"},
        "lenses": {"lens_set": ["acceptance-invariants"], "lens_concurrency": "parallel"}
      }
    },
    "default_class": "situational"
  }
}`

func mustRelevanceOptions(project string) *runRelevanceOptions {
	return &runRelevanceOptions{
		filter: filterAll,
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		cwd:    project,
	}
}

func relevanceOut(opts *runRelevanceOptions) string {
	return opts.stdout.(*bytes.Buffer).String()
}

// seedPlan writes a PLAN.yaml + TASKS.yaml under the project's plans dir so the
// --task selector has something to resolve against.
func seedPlan(t *testing.T, project, planID, planBody, tasksBody string) {
	t.Helper()
	dir := filepath.Join(project, ".agents", "workflow", "plans", planID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plan dir: %v", err)
	}
	if planBody != "" {
		if err := os.WriteFile(filepath.Join(dir, "PLAN.yaml"), []byte(planBody), 0o644); err != nil {
			t.Fatalf("write PLAN.yaml: %v", err)
		}
	}
	if tasksBody != "" {
		if err := os.WriteFile(filepath.Join(dir, "TASKS.yaml"), []byte(tasksBody), 0o644); err != nil {
			t.Fatalf("write TASKS.yaml: %v", err)
		}
	}
}

// ---------- normalizeFilter ----------

func TestNormalizeFilter(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to all", "", filterAll, false},
		{"units lower", "units", filterUnits, false},
		{"topology trims+cases", "  Topology ", filterTopology, false},
		{"lenses", "LENSES", filterLenses, false},
		{"all", "all", filterAll, false},
		{"unknown is error", "bogus", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNormalizeFilterCase(t, tc.in, tc.want, tc.wantErr)
		})
	}
}

// assertNormalizeFilterCase runs one normalizeFilter case and asserts the
// outcome. Extracted from the table loop so the per-case branching does not
// inflate TestNormalizeFilter's cognitive complexity (SonarCloud go:S3776).
func assertNormalizeFilterCase(t *testing.T, in, want string, wantErr bool) {
	t.Helper()
	opts := &runRelevanceOptions{filter: in}
	err := normalizeFilter(opts, testDeps())
	if wantErr {
		assertUnknownFilterError(t, in, err)
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.filter != want {
		t.Fatalf("got filter %q want %q", opts.filter, want)
	}
}

func assertUnknownFilterError(t *testing.T, in string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error for %q", in)
	}
	he, ok := err.(*hintError)
	if !ok {
		t.Fatalf("expected hintError, got %T", err)
	}
	if !strings.Contains(he.message, "unknown --filter") {
		t.Fatalf("unexpected message: %q", he.message)
	}
}

// ---------- resolveExecutionProfile ----------

func TestResolveExecutionProfile_Present(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	snap, err := resolveLayered(project)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	profile := resolveExecutionProfile(snap)
	if profile == nil || profile.ByAppType == nil {
		t.Fatalf("expected populated profile, got %+v", profile)
	}
	if _, ok := profile.ByAppType["go-cli"]; !ok {
		t.Fatalf("expected go-cli entry")
	}
	if profile.DefaultClass != "situational" {
		t.Fatalf("got default_class %q", profile.DefaultClass)
	}
}

func TestResolveExecutionProfile_Absent(t *testing.T) {
	project := withRepoLayer(t, `{"repo_id":"x"}`, "")
	snap, err := resolveLayered(project)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	profile := resolveExecutionProfile(snap)
	if profile == nil {
		t.Fatalf("expected non-nil empty profile")
	}
	if profile.ByAppType != nil {
		t.Fatalf("expected nil ByAppType for absent layer, got %+v", profile.ByAppType)
	}
}

func TestResolveExecutionProfile_NullLayer(t *testing.T) {
	project := withRepoLayer(t, `{"execution_profile": null}`, "")
	snap, err := resolveLayered(project)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	profile := resolveExecutionProfile(snap)
	if profile == nil || profile.ByAppType != nil {
		t.Fatalf("expected empty profile for null layer, got %+v", profile)
	}
}

// TestResolveExecutionProfile_MalformedShapeFailsSnapshotLoad covers what was
// previously an isolated resolveExecutionProfile decode-error branch:
// execution_profile is a string, not an object. Snapshot resolution now
// decodes the WHOLE manifest through the typed AgentsRC schema up front
// (resolveLayered), so a malformed execution_profile shape fails snapshot
// resolution itself, before resolveExecutionProfile is ever reached.
func TestResolveExecutionProfile_MalformedShapeFailsSnapshotLoad(t *testing.T) {
	project := withRepoLayer(t, `{"execution_profile": "oops"}`, "")
	if _, err := resolveLayered(project); err == nil {
		t.Fatalf("expected decode error for malformed execution_profile")
	}
}

// ---------- splitTaskSelector ----------

func TestSplitTaskSelector(t *testing.T) {
	cases := []struct {
		in       string
		wantPlan string
		wantTask string
		wantErr  bool
	}{
		{"plan/task", "plan", "task", false},
		{"  config-relevance-profiles/t2 ", "config-relevance-profiles", "t2", false},
		{"a/b/c", "a", "b/c", false}, // first slash splits; remainder is the task id
		{"notaselector", "", "", true},
		{"/leading", "", "", true},
		{"trailing/", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			plan, task, err := splitTaskSelector(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan != tc.wantPlan || task != tc.wantTask {
				t.Fatalf("got (%q,%q) want (%q,%q)", plan, task, tc.wantPlan, tc.wantTask)
			}
		})
	}
}

// ---------- lookupTaskAppType ----------

func TestLookupTaskAppType(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedPlan(t, project, "p1",
		"schema_version: 1\nid: p1\ndefault_app_type: ideation\n",
		"schema_version: 1\nplan_id: p1\ntasks:\n  - id: t-cli\n    app_type: go-cli\n  - id: t-bare\n",
	)

	t.Run("task app_type wins", func(t *testing.T) {
		assertLookupAppType(t, project, "p1/t-cli", "go-cli", "ideation")
	})
	t.Run("bare task falls to plan default", func(t *testing.T) {
		assertLookupAppType(t, project, "p1/t-bare", "", "ideation")
	})
	t.Run("missing task errors", func(t *testing.T) {
		assertLookupAppTypeErr(t, project, "p1/nope")
	})
	t.Run("missing plan errors", func(t *testing.T) {
		assertLookupAppTypeErr(t, project, "ghost/t")
	})
	t.Run("bad selector errors", func(t *testing.T) {
		assertLookupAppTypeErr(t, project, "noselector")
	})
}

// assertLookupAppType / assertLookupAppTypeErr keep the per-case branching out of
// TestLookupTaskAppType so it stays under the cognitive-complexity gate (go:S3776).
func assertLookupAppType(t *testing.T, project, sel, wantTA, wantDef string) {
	t.Helper()
	ta, def, err := lookupTaskAppType(project, sel)
	if err != nil {
		t.Fatalf("lookup %q: %v", sel, err)
	}
	if ta != wantTA || def != wantDef {
		t.Fatalf("%q: got (%q,%q) want (%q,%q)", sel, ta, def, wantTA, wantDef)
	}
}

func assertLookupAppTypeErr(t *testing.T, project, sel string) {
	t.Helper()
	if _, _, err := lookupTaskAppType(project, sel); err == nil {
		t.Fatalf("%q: expected error", sel)
	}
}

func TestLookupTaskAppType_BadTasksYAML(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedPlan(t, project, "p2",
		"schema_version: 1\nid: p2\n",
		":\n  not valid yaml mapping\n\t- broken",
	)
	if _, _, err := lookupTaskAppType(project, "p2/anything"); err == nil {
		t.Fatalf("expected tasks parse error")
	}
}

func TestReadYAMLFile_Missing(t *testing.T) {
	var out struct{}
	if err := readYAMLFile(filepath.Join(t.TempDir(), "absent.yaml"), &out); err == nil {
		t.Fatalf("expected error for missing file")
	}
}

// ---------- resolveAppType ----------

func TestResolveAppType(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedPlan(t, project, "p1",
		"schema_version: 1\nid: p1\ndefault_app_type: ideation\n",
		"schema_version: 1\nplan_id: p1\ntasks:\n  - id: t-cli\n    app_type: go-cli\n  - id: t-bare\n",
	)

	cases := []struct {
		name       string
		opts       runRelevanceOptions
		wantApp    string
		wantSource string
		wantErr    bool
	}{
		{"task override", runRelevanceOptions{cwd: project, task: "p1/t-cli", appType: "ignored"}, "go-cli", "task", false},
		{"plan default", runRelevanceOptions{cwd: project, task: "p1/t-bare"}, "ideation", "plan-default", false},
		{"bare task falls to flag", runRelevanceOptions{cwd: project, task: "p1/t-bare-missing-default", appType: "flagval"}, "", "", true},
		{"flag only", runRelevanceOptions{cwd: project, appType: "go-cli"}, "go-cli", "flag", false},
		{"none", runRelevanceOptions{cwd: project}, "", "none", false},
		{"bad task selector", runRelevanceOptions{cwd: project, task: "nope"}, "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, source, err := resolveAppType(&tc.opts, testDeps())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if app != tc.wantApp || source != tc.wantSource {
				t.Fatalf("got (%q,%q) want (%q,%q)", app, source, tc.wantApp, tc.wantSource)
			}
		})
	}
}

// TestResolveAppType_BareTaskNoDefaultFallsToFlag covers the path where a task
// resolves but declares no app_type and its plan has no default, so the flag
// is used.
func TestResolveAppType_BareTaskNoDefaultFallsToFlag(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedPlan(t, project, "p3",
		"schema_version: 1\nid: p3\n",
		"schema_version: 1\nplan_id: p3\ntasks:\n  - id: only\n",
	)
	opts := &runRelevanceOptions{cwd: project, task: "p3/only", appType: "go-cli"}
	app, source, err := resolveAppType(opts, testDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != "go-cli" || source != "flag" {
		t.Fatalf("got (%q,%q), want (go-cli, flag)", app, source)
	}
}

// ---------- facet builders ----------

func loadFixtureProfile(t *testing.T) (project string) {
	t.Helper()
	return withRepoLayer(t, relevanceRepoBody, "")
}

func TestBuildUnitsFacet_AllStages(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)
	prof := appTypeProfile(profile, "go-cli")
	units := buildUnitsFacet(profile, prof, "go-cli", "")
	if units.DefaultClass != "situational" {
		t.Fatalf("default class %q", units.DefaultClass)
	}
	if len(units.ByStage) != 2 {
		t.Fatalf("expected 2 stages, got %d", len(units.ByStage))
	}
	if got := units.ByStage["orchestrate"].Core; len(got) != 2 {
		t.Fatalf("orchestrate core %v", got)
	}
}

func TestBuildUnitsFacet_OneStage(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)
	prof := appTypeProfile(profile, "go-cli")

	units := buildUnitsFacet(profile, prof, "go-cli", "review")
	if len(units.ByStage) != 1 {
		t.Fatalf("expected single stage, got %d", len(units.ByStage))
	}
	if _, ok := units.ByStage["review"]; !ok {
		t.Fatalf("expected review stage")
	}

	// A stage with no declared classes yields an empty ByStage.
	missing := buildUnitsFacet(profile, prof, "go-cli", "verify")
	if len(missing.ByStage) != 0 {
		t.Fatalf("expected no stages for undeclared stage, got %d", len(missing.ByStage))
	}
}

func TestBuildUnitsFacet_NoRelevance(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)
	prof := appTypeProfile(profile, "ideation") // ideation has no relevance map
	units := buildUnitsFacet(profile, prof, "ideation", "")
	if len(units.ByStage) != 0 {
		t.Fatalf("expected empty by_stage for app_type without relevance")
	}
}

func TestBuildTopologyAndLensesFacet(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)
	prof := appTypeProfile(profile, "go-cli")

	topo := buildTopologyFacet(prof)
	if topo.Executors != 1 || topo.VerifiersPerExecutor != 2 || topo.Reviewers != "per_verifier" {
		t.Fatalf("topology %+v", topo)
	}
	if len(topo.VerifierSequence) != 2 {
		t.Fatalf("verifier sequence %v", topo.VerifierSequence)
	}

	lenses := buildLensesFacet(prof)
	if lenses.LensConcurrency != "gated" || len(lenses.LensSet) != 2 {
		t.Fatalf("lenses %+v", lenses)
	}

	// Empty profile (unmatched app_type) yields zero-value facets, not nil.
	empty := appTypeProfile(profile, "no-such-app")
	if buildTopologyFacet(empty).Executors != 0 {
		t.Fatalf("expected zero executors for unmatched app_type")
	}
	if l := buildLensesFacet(empty); l.LensConcurrency != "" || len(l.LensSet) != 0 {
		t.Fatalf("expected empty lenses for unmatched app_type, got %+v", l)
	}
}

// ---------- appTypeMatched / appTypeProfile ----------

func TestAppTypeMatchedAndProfile(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)

	if !appTypeMatched(profile, "go-cli") {
		t.Fatalf("expected go-cli matched")
	}
	if appTypeMatched(profile, "ghost") {
		t.Fatalf("ghost should not match")
	}
	if appTypeMatched(profile, "") {
		t.Fatalf("empty app_type should not match")
	}
	if appTypeMatched(nil, "go-cli") {
		t.Fatalf("nil profile should not match")
	}

	// appTypeProfile on a nil profile and on an empty app_type both return the
	// zero AppTypeProfile so facet builders always have a value to read.
	if got := appTypeProfile(nil, "go-cli"); got.Topology.Executors != 0 {
		t.Fatalf("nil profile should give zero AppTypeProfile, got %+v", got)
	}
	if p := appTypeProfile(profile, ""); p.Topology.Executors != 0 {
		t.Fatalf("empty app_type should give zero profile")
	}
}

// ---------- buildRelevanceResult ----------

func TestBuildRelevanceResult_Filters(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)

	cases := []struct {
		filter      string
		wantUnits   bool
		wantTopo    bool
		wantLens    bool
		wantGraph   bool
		wantLessons bool
	}{
		{filterUnits, true, false, false, false, false},
		{filterTopology, false, true, false, false, false},
		{filterLenses, false, false, true, false, false},
		{filterGraph, false, false, false, true, false},
		{filterLessons, false, false, false, false, true},
		{filterAll, true, true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.filter, func(t *testing.T) {
			opts := &runRelevanceOptions{filter: tc.filter, stage: "review", cwd: project}
			res, err := buildRelevanceResult(opts, profile, "go-cli", "flag")
			if err != nil {
				t.Fatalf("buildRelevanceResult: %v", err)
			}
			assertFacetPresence(t, "units", res.Units != nil, tc.wantUnits)
			assertFacetPresence(t, "topology", res.Topology != nil, tc.wantTopo)
			assertFacetPresence(t, "lenses", res.Lenses != nil, tc.wantLens)
			assertFacetPresence(t, "graph", res.Graph != nil, tc.wantGraph)
			assertFacetPresence(t, "lessons", res.Lessons != nil, tc.wantLessons)
			if !res.Matched {
				t.Fatalf("expected matched=true for go-cli")
			}
		})
	}
}

// assertFacetPresence checks one facet's presence against the table expectation,
// keeping the per-case loop body flat (one call per facet rather than an inline
// if per facet).
func assertFacetPresence(t *testing.T, facet string, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("%s presence %v want %v", facet, got, want)
	}
}

func TestBuildRelevanceResult_UnmatchedAppType(t *testing.T) {
	project := loadFixtureProfile(t)
	snap, _ := resolveLayered(project)
	profile := resolveExecutionProfile(snap)

	opts := &runRelevanceOptions{filter: filterAll, cwd: project}
	res, err := buildRelevanceResult(opts, profile, "unknown-app", "flag")
	if err != nil {
		t.Fatalf("buildRelevanceResult: %v", err)
	}
	if res.Matched {
		t.Fatalf("unknown app should not match")
	}
	// Defaults still render: empty facets, default_class surfaced.
	if res.Units == nil || res.Units.DefaultClass != "situational" {
		t.Fatalf("expected default_class on unmatched units, got %+v", res.Units)
	}
	if res.Topology == nil || res.Topology.Executors != 0 {
		t.Fatalf("expected zero topology, got %+v", res.Topology)
	}
}

// ---------- runRelevance (end to end through the run path) ----------

func TestRunRelevance_JSON(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.filter = filterUnits
	opts.appType = "go-cli"
	opts.stage = "orchestrate"
	opts.jsonOut = true

	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	var got relevanceResult
	if err := json.Unmarshal([]byte(relevanceOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, relevanceOut(opts))
	}
	if got.AppType != "go-cli" || got.AppTypeSource != "flag" || got.Filter != filterUnits {
		t.Fatalf("envelope mismatch: %+v", got)
	}
	if got.Units == nil || len(got.Units.ByStage["orchestrate"].Core) != 2 {
		t.Fatalf("units payload mismatch: %+v", got.Units)
	}
	if got.Topology != nil || got.Lenses != nil {
		t.Fatalf("non-requested facets should be omitted: %+v", got)
	}
}

// TestRunRelevance_GraphJSON is the t7 hard test through the run path: a profile
// declaring graph_backend: dotagents-builtin:graph/none@^1.0 resolves end-to-end
// through config resolution to the registry's ref resolver — not just the direct
// adapter path. The no-op none adapter resolves, so the facet reports it.
func TestRunRelevance_GraphJSON(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.filter = filterGraph
	opts.appType = "go-cli"
	opts.jsonOut = true

	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	var got relevanceResult
	if err := json.Unmarshal([]byte(relevanceOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, relevanceOut(opts))
	}
	if got.Filter != filterGraph || got.Graph == nil {
		t.Fatalf("graph facet missing: %+v", got)
	}
	if !got.Graph.Resolved || got.Graph.Adapter != "none" || got.Graph.Version != "1.0.0" {
		t.Fatalf("graph facet did not resolve the none adapter: %+v", got.Graph)
	}
	if got.Units != nil || got.Topology != nil || got.Lenses != nil {
		t.Fatalf("non-requested facets should be omitted: %+v", got)
	}
}

func TestRunRelevance_HumanAll(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.appType = "go-cli"

	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	out := relevanceOut(opts)
	for _, want := range []string{"Execution profile (filter: all)", "app_type : go-cli", "units", "topology", "lenses", "verifier_sequence", "lens_concurrency"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunRelevance_HumanNoneAppType(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	// no app-type, no task — resolves to (none); facets render empty/default.
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	out := relevanceOut(opts)
	if !strings.Contains(out, "(none)") {
		t.Fatalf("expected (none) label:\n%s", out)
	}
	if !strings.Contains(out, "no relevance classes declared") {
		t.Fatalf("expected empty-units note:\n%s", out)
	}
}

func TestRunRelevance_UnitsStageScoped(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.filter = filterUnits
	opts.appType = "go-cli"
	opts.stage = "review"
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	out := relevanceOut(opts)
	if !strings.Contains(out, "stage: review") {
		t.Fatalf("expected stage-scoped header:\n%s", out)
	}
	if strings.Contains(out, "[orchestrate]") {
		t.Fatalf("orchestrate stage should be filtered out:\n%s", out)
	}
}

func TestRunRelevance_TaskSelector(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	seedPlan(t, project, "pl",
		"schema_version: 1\nid: pl\ndefault_app_type: ideation\n",
		"schema_version: 1\nplan_id: pl\ntasks:\n  - id: cli-task\n    app_type: go-cli\n",
	)
	opts := mustRelevanceOptions(project)
	opts.filter = filterTopology
	opts.task = "pl/cli-task"
	opts.jsonOut = true
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	var got relevanceResult
	if err := json.Unmarshal([]byte(relevanceOut(opts)), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.AppType != "go-cli" || got.AppTypeSource != "task" {
		t.Fatalf("task selector mismatch: %+v", got)
	}
	if got.Topology == nil || got.Topology.Executors != 1 {
		t.Fatalf("topology mismatch: %+v", got.Topology)
	}
}

func TestRunRelevance_BadFilter(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.filter = "nonsense"
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatalf("expected usage error")
	}
}

func TestRunRelevance_MissingManifest(t *testing.T) {
	project := withRepoLayer(t, "", "") // no repo-local .agentsrc.json
	opts := mustRelevanceOptions(project)
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatalf("expected error for missing manifest")
	}
	he, ok := err.(*hintError)
	if !ok {
		t.Fatalf("expected hintError, got %T", err)
	}
	if !strings.Contains(he.Error(), "install --generate") {
		t.Fatalf("expected install hint, got %q", he.Error())
	}
}

func TestRunRelevance_MalformedProfile(t *testing.T) {
	project := withRepoLayer(t, `{"execution_profile": "scalar-not-object"}`, "")
	opts := mustRelevanceOptions(project)
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatalf("expected decode error surfaced")
	}
}

// TestRunRelevance_RealLockedExtendsLayer is the end-to-end proof that
// `da config relevance` now resolves through the SAME layered path
// `da config explain` / `workflow app-types` use: a project that `extends` a
// layer carrying execution_profile.by_app_type (a team-source profile), with a
// populated .agentsrc.lock + on-disk layer cache, must surface that imported
// app_type's topology/verifier_sequence. Under the retired loadFlatSnapshot
// (product-defaults -> user-local -> repo-local only) this app_type would
// never have matched — extends layers were invisible to relevance.
func TestRunRelevance_RealLockedExtendsLayer(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())

	// A local source dir is a pure-disk extends source (no network): the
	// online Resolve below reads it straight off disk while populating the
	// cache + lock, after which the real (unstubbed) resolveLayered seam
	// (ResolveLocked) replays it offline — the same production path
	// runRelevance drives.
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "org"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "org", "base.json"), []byte(`{
  "execution_profile": {
    "by_app_type": {
      "org-shared-svc": {
        "topology": {
          "executors": 2,
          "verifiers_per_executor": 1,
          "verifier_sequence": ["org-unit", "org-api"]
        }
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, cfg.AgentsRCFile), []byte(`{
  "project":"svc","version":2,
  "sources":[{"id":"acme","type":"local","path":`+strconv.Quote(src)+`}],
  "extends":["acme:org/base.json"]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed .agentsrc.lock + the layer cache via one online (local-disk)
	// resolve — the same seeding pattern
	// TestWorkflowAppTypesRealLockedExtendsLayer uses.
	if _, err := cfg.NewLayeredResolver().Resolve(repo); err != nil {
		t.Fatalf("seed online resolve: %v", err)
	}

	opts := mustRelevanceOptions(repo)
	opts.filter = filterTopology
	opts.appType = "org-shared-svc"
	opts.jsonOut = true
	if err := runRelevance(opts, testDeps()); err != nil {
		t.Fatalf("runRelevance: %v", err)
	}
	var got relevanceResult
	if err := json.Unmarshal([]byte(relevanceOut(opts)), &got); err != nil {
		t.Fatalf("decode json: %v\n%s", err, relevanceOut(opts))
	}
	if !got.Matched {
		t.Fatalf("expected the imported app_type to match, got %+v", got)
	}
	if got.Topology == nil || got.Topology.Executors != 2 || got.Topology.VerifiersPerExecutor != 1 {
		t.Fatalf("imported topology facet missing/mismatched: %+v", got.Topology)
	}
	if strings.Join(got.Topology.VerifierSequence, ",") != "org-unit,org-api" {
		t.Fatalf("imported verifier_sequence mismatch: %+v", got.Topology)
	}
}

func TestRunRelevance_BadTaskSelector(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	opts := mustRelevanceOptions(project)
	opts.task = "no-slash"
	err := runRelevance(opts, testDeps())
	if err == nil {
		t.Fatalf("expected selector error")
	}
}

// ---------- rendering helpers ----------

func TestJoinUnitsAndAppTypeLabel(t *testing.T) {
	if joinUnits(nil) != "-" {
		t.Fatalf("empty list should render dash")
	}
	if joinUnits([]string{"a", "b"}) != "a, b" {
		t.Fatalf("join mismatch")
	}
	if relevanceAppTypeLabel("") != "(none)" {
		t.Fatalf("empty app_type label")
	}
	if relevanceAppTypeLabel("go-cli") != "go-cli" {
		t.Fatalf("app_type label passthrough")
	}
}

// ---------- command wiring ----------

func TestNewRelevanceCmd_Registered(t *testing.T) {
	root := NewConfigCmd(testDeps())
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "relevance" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("relevance subcommand not registered")
	}
}

// TestRelevanceCmd_RunE_Cobra drives the cobra RunE closure end to end (cwd
// auto-resolution, flag wiring, jsonFlag) by executing the config root with the
// relevance args from inside a temp project the harness chdirs into.
func TestRelevanceCmd_RunE_Cobra(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	chdir(t, project)

	root := NewConfigCmd(testDeps())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"relevance", "--filter", "lenses", "--app-type", "ideation"})

	if err := root.Execute(); err != nil {
		t.Fatalf("cobra execute: %v", err)
	}
	if !strings.Contains(out.String(), "lens_concurrency : parallel") {
		t.Fatalf("expected lenses output, got:\n%s", out.String())
	}
}

// TestRelevanceCmd_RunE_BadFilterCobra exercises the RunE error return through
// cobra so the closure's error wiring is covered.
func TestRelevanceCmd_RunE_BadFilterCobra(t *testing.T) {
	project := withRepoLayer(t, relevanceRepoBody, "")
	chdir(t, project)

	root := NewConfigCmd(testDeps())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"relevance", "--filter", "garbage"})

	if err := root.Execute(); err == nil {
		t.Fatalf("expected error from bad --filter through cobra")
	}
}

// chdir switches into dir for the duration of the test, restoring the prior cwd
// on cleanup. Used to exercise the cobra RunE cwd auto-resolution branch.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
