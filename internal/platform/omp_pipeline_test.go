package platform

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type swarmAgent struct {
	Role     string   `yaml:"role"`
	Model    string   `yaml:"model"`
	WaitsFor []string `yaml:"waits_for"`
	Task     string   `yaml:"task"`
}

type swarmDoc struct {
	Swarm struct {
		Name        string                `yaml:"name"`
		Workspace   string                `yaml:"workspace"`
		Mode        string                `yaml:"mode"`
		TargetCount int                   `yaml:"target_count"`
		Model       string                `yaml:"model"`
		Agents      map[string]swarmAgent `yaml:"agents"`
	} `yaml:"swarm"`
}

func mustParseSwarm(t *testing.T, content string) swarmDoc {
	t.Helper()
	var doc swarmDoc
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("emitted YAML does not parse: %v", err)
	}
	return doc
}

func skeletonSpec(workspace string) PipelineSpec {
	spec, err := BuildPipelineSpec(workspace, "", fixtureStageProfiles(), nil)
	if err != nil {
		panic(err)
	}
	return spec
}

func emitOMP(t *testing.T, spec PipelineSpec) []PipelineArtifact {
	t.Helper()
	arts, err := ompPipelineProjector{}.Emit(spec)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return arts
}

// TestOMPEmitParsesAndMergeKeysResolve confirms the emitted YAML is well-formed
// and its `<<: *verify_slot` / `*routine_review` merge keys resolve (the parser
// validation the acceptance calls for; no swarm-extension parser is vendored in
// this repo, so a real YAML parse + merge-key resolution stands in for it, backed
// by the omp-full-loop driver integration test).
func TestOMPEmitParsesAndMergeKeysResolve(t *testing.T) {
	arts := emitOMP(t, skeletonSpec("/repo"))
	if len(arts) != 2 {
		t.Fatalf("want 2 artifacts, got %d", len(arts))
	}
	if arts[0].Name != OMPInnerArtifactName || arts[1].Name != OMPReconcileArtifactName {
		t.Fatalf("artifact names = %q,%q", arts[0].Name, arts[1].Name)
	}
	doc := mustParseSwarm(t, arts[0].Content)
	if len(doc.Swarm.Agents) != 15 {
		t.Fatalf("skeleton agent count = %d, want 15", len(doc.Swarm.Agents))
	}
	// Merge-key resolution: a later verify/routine slot inherits the anchor model.
	if doc.Swarm.Agents["verify_7"].Model != "claude-opus-4-8" {
		t.Fatalf("verify_7 model (via merge) = %q", doc.Swarm.Agents["verify_7"].Model)
	}
	if doc.Swarm.Agents["review_routine_4"].Model != "claude-opus-4-8" {
		t.Fatalf("review_routine_4 model (via merge) = %q", doc.Swarm.Agents["review_routine_4"].Model)
	}
	if doc.Swarm.Agents["review_cross_family"].Model != "gpt-5.4" {
		t.Fatalf("cross-family model = %q", doc.Swarm.Agents["review_cross_family"].Model)
	}
}

// TestOMPEmitMatchesCheckedInSemantics is the round-trip semantic-equality test:
// the emitted skeleton must agree with the checked-in runtime YAMLs on name,
// mode, target_count, models, agent set, and waits_for topology (the acceptance
// contract). The workspace path and the generated header are the documented
// deltas and are excluded.
func TestOMPEmitMatchesCheckedInSemantics(t *testing.T) {
	arts := emitOMP(t, skeletonSpec("/repo"))
	emitted := mustParseSwarm(t, arts[0].Content)

	raw, err := os.ReadFile("../../.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml")
	if err != nil {
		t.Fatalf("read checked-in inner YAML: %v", err)
	}
	checked := mustParseSwarm(t, string(raw))

	assertInnerSwarmSemantics(t, emitted, checked)

	// Reconcile YAML semantics.
	rawR, err := os.ReadFile("../../.agents/workflow/runtime/full-loop/reconcile.swarm.yaml")
	if err != nil {
		t.Fatalf("read checked-in reconcile YAML: %v", err)
	}
	emittedR := mustParseSwarm(t, arts[1].Content)
	checkedR := mustParseSwarm(t, string(rawR))
	if emittedR.Swarm.Mode != checkedR.Swarm.Mode || emittedR.Swarm.Model != checkedR.Swarm.Model {
		t.Fatalf("reconcile semantics differ: emit mode=%q model=%q checked mode=%q model=%q",
			emittedR.Swarm.Mode, emittedR.Swarm.Model, checkedR.Swarm.Mode, checkedR.Swarm.Model)
	}
	if len(emittedR.Swarm.Agents) != 1 || emittedR.Swarm.Agents["reconcile"].Model == "" {
		t.Fatalf("reconcile agents = %v", emittedR.Swarm.Agents)
	}
}

// assertInnerSwarmSemantics checks the emitted inner swarm agrees with the
// checked-in YAML on name, mode, target_count, model, and per-agent model +
// waits_for topology (the workspace path and generated header are documented
// deltas and are not compared here).
func assertInnerSwarmSemantics(t *testing.T, emitted, checked swarmDoc) {
	t.Helper()
	if emitted.Swarm.Name != checked.Swarm.Name {
		t.Fatalf("name: emit=%q checked=%q", emitted.Swarm.Name, checked.Swarm.Name)
	}
	if emitted.Swarm.Mode != checked.Swarm.Mode {
		t.Fatalf("mode: emit=%q checked=%q", emitted.Swarm.Mode, checked.Swarm.Mode)
	}
	if emitted.Swarm.TargetCount != checked.Swarm.TargetCount {
		t.Fatalf("target_count: emit=%d checked=%d", emitted.Swarm.TargetCount, checked.Swarm.TargetCount)
	}
	if emitted.Swarm.Model != checked.Swarm.Model {
		t.Fatalf("model: emit=%q checked=%q", emitted.Swarm.Model, checked.Swarm.Model)
	}
	if len(emitted.Swarm.Agents) != len(checked.Swarm.Agents) {
		t.Fatalf("agent count: emit=%d checked=%d", len(emitted.Swarm.Agents), len(checked.Swarm.Agents))
	}
	for name, want := range checked.Swarm.Agents {
		got, ok := emitted.Swarm.Agents[name]
		if !ok {
			t.Fatalf("emitted YAML is missing agent %q", name)
		}
		if got.Model != want.Model {
			t.Fatalf("agent %q model: emit=%q checked=%q", name, got.Model, want.Model)
		}
		if strings.Join(got.WaitsFor, ",") != strings.Join(want.WaitsFor, ",") {
			t.Fatalf("agent %q waits_for: emit=%v checked=%v", name, got.WaitsFor, want.WaitsFor)
		}
	}
}

// TestOMPEmitByteIdempotent proves the same spec re-emits byte-identical content.
func TestOMPEmitByteIdempotent(t *testing.T) {
	a := emitOMP(t, skeletonSpec("/repo"))
	b := emitOMP(t, skeletonSpec("/repo"))
	for i := range a {
		if a[i].Content != b[i].Content {
			t.Fatalf("artifact %q is not byte-idempotent", a[i].Name)
		}
	}
	// Header carries the do-not-hand-edit marker and a digest, no wall-clock.
	if !strings.HasPrefix(a[0].Content, generatedHeaderLine+"\n# config-digest: ") {
		t.Fatalf("missing generation header: %q", strings.SplitN(a[0].Content, "\n", 2)[0])
	}
}

// TestOMPEmitSpecializedNaming checks app_type-qualified filenames.
func TestOMPEmitSpecializedNaming(t *testing.T) {
	spec, err := BuildPipelineSpec("/repo", "go-cli", fixtureStageProfiles(), fixtureExecProfile())
	if err != nil {
		t.Fatal(err)
	}
	arts := emitOMP(t, spec)
	if arts[0].Name != "profile-driven.go-cli.swarm.yaml" {
		t.Fatalf("specialized inner name = %q", arts[0].Name)
	}
	if arts[1].Name != "reconcile.go-cli.swarm.yaml" {
		t.Fatalf("specialized reconcile name = %q", arts[1].Name)
	}
	doc := mustParseSwarm(t, arts[0].Content)
	verifies := 0
	for name := range doc.Swarm.Agents {
		if strings.HasPrefix(name, "verify_") {
			verifies++
		}
	}
	if verifies != 2 {
		t.Fatalf("go-cli verify slots = %d, want 2", verifies)
	}
}

func TestOMPArtifactName(t *testing.T) {
	cases := map[string]struct{ base, appType, want string }{
		"skeleton": {OMPInnerArtifactName, "", "profile-driven.swarm.yaml"},
		"apptype":  {OMPInnerArtifactName, "daemon", "profile-driven.daemon.swarm.yaml"},
		"slash":    {OMPInnerArtifactName, "docs/web", "profile-driven.docs-web.swarm.yaml"},
		"trim":     {OMPReconcileArtifactName, "  meta  ", "reconcile.meta.swarm.yaml"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ompArtifactName(tc.base, tc.appType); got != tc.want {
				t.Fatalf("ompArtifactName(%q,%q) = %q, want %q", tc.base, tc.appType, got, tc.want)
			}
		})
	}
}

// TestOMPEmitNoRoutineNoCrossFamily exercises the emit branches where the routine
// lens group and/or the cross-family gate are absent (gate then chains to the
// last present stage).
func TestOMPEmitNoRoutineNoCrossFamily(t *testing.T) {
	exec := StageRoute{Slug: "default", Model: "claude-opus-4-8", ModelFamily: "claude"}
	// No routine lenses, no cross-family: gate waits on the last verify slot.
	spec := PipelineSpec{
		Workspace:   "/repo",
		AppType:     "bare",
		TargetCount: 3,
		Executor:    exec,
		Verifiers:   []StageRoute{exec, exec},
	}
	doc := mustParseSwarm(t, emitOMP(t, spec)[0].Content)
	if _, ok := doc.Swarm.Agents["review_routine_1"]; ok {
		t.Fatal("no routine lenses expected")
	}
	if _, ok := doc.Swarm.Agents["review_cross_family"]; ok {
		t.Fatal("no cross-family expected")
	}
	if got := doc.Swarm.Agents["gate"].WaitsFor; strings.Join(got, ",") != "verify_2" {
		t.Fatalf("gate waits_for = %v, want [verify_2]", got)
	}

	// Cross-family present but no routine lenses: cross waits on the last verify,
	// gate waits on cross.
	cross := StageRoute{Slug: "cross-harness-adversarial", Model: "gpt-5.4", ModelFamily: "gpt"}
	spec.CrossFamily = &cross
	doc = mustParseSwarm(t, emitOMP(t, spec)[0].Content)
	if got := doc.Swarm.Agents["review_cross_family"].WaitsFor; strings.Join(got, ",") != "verify_2" {
		t.Fatalf("cross-family waits_for = %v, want [verify_2]", got)
	}
	if got := doc.Swarm.Agents["gate"].WaitsFor; strings.Join(got, ",") != "review_cross_family" {
		t.Fatalf("gate waits_for = %v, want [review_cross_family]", got)
	}
}

// TestOMPEmitModelOverride confirms a per-slot model that differs from the anchor
// route emits an explicit model override line (and parses to that model).
func TestOMPEmitModelOverride(t *testing.T) {
	exec := StageRoute{Slug: "default", Model: "claude-opus-4-8", ModelFamily: "claude"}
	fast := StageRoute{Slug: "fast", Model: "gpt-5.4-mini", ModelFamily: "gpt"}
	arch := StageRoute{Slug: "architecture-standards", Model: "claude-opus-4-8", ModelFamily: "claude"}
	cheapLens := StageRoute{Slug: "adversarial", Model: "gpt-5.4-mini", ModelFamily: "gpt"}
	spec := PipelineSpec{
		Workspace:     "/repo",
		AppType:       "over",
		TargetCount:   3,
		Executor:      exec,
		Verifiers:     []StageRoute{exec, fast},
		RoutineLenses: []StageRoute{arch, cheapLens},
	}
	content := emitOMP(t, spec)[0].Content
	if !strings.Contains(content, "      model: gpt-5.4-mini\n") {
		t.Fatal("expected an explicit model override line for the differing slot")
	}
	doc := mustParseSwarm(t, content)
	if doc.Swarm.Agents["verify_2"].Model != "gpt-5.4-mini" {
		t.Fatalf("verify_2 override model = %q", doc.Swarm.Agents["verify_2"].Model)
	}
	if doc.Swarm.Agents["review_routine_2"].Model != "gpt-5.4-mini" {
		t.Fatalf("review_routine_2 override model = %q", doc.Swarm.Agents["review_routine_2"].Model)
	}
}

func TestOMPEmitValidateError(t *testing.T) {
	_, err := ompPipelineProjector{}.Emit(PipelineSpec{Workspace: ""})
	if err == nil {
		t.Fatal("emit should reject an invalid spec")
	}
}

func TestOMPPlatform(t *testing.T) {
	if (ompPipelineProjector{}).Platform() != "omp" {
		t.Fatal("platform id")
	}
}
