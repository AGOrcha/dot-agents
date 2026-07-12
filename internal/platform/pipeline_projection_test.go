package platform

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
)

func claudeProfile() config.StageProfile {
	return config.StageProfile{Model: "claude-opus-4-8", ModelFamily: "claude"}
}

// fixtureStageProfiles mirrors the shape of the repo's real stage_profiles: a
// claude executor/default, a gpt cross-harness-adversarial reviewer, uniform
// claude verifiers/reviewers, plus a few deliberately-broken entries the error
// paths key on.
func fixtureStageProfiles() map[string]map[string]config.StageProfile {
	return map[string]map[string]config.StageProfile{
		stageExecutor:  {"default": claudeProfile()},
		"orchestrator": {"default": claudeProfile()},
		stageVerifier: {
			"unit":             claudeProfile(),
			"cli-runner":       claudeProfile(),
			"schema-check":     claudeProfile(),
			"citation-check":   claudeProfile(),
			"fast":             {Model: "gpt-5.4-mini", ModelFamily: "gpt"},
			"emptymodel-verif": {Model: "", ModelFamily: "claude"},
		},
		stageReviewer: {
			"architecture-standards":    claudeProfile(),
			"acceptance-invariants":     claudeProfile(),
			"adversarial":               claudeProfile(),
			"security":                  claudeProfile(),
			"web-a11y":                  claudeProfile(),
			"cross-harness-adversarial": {Model: "gpt-5.4", ModelFamily: "gpt"},
			"emptymodel-lens":           {Model: "", ModelFamily: "claude"},
		},
	}
}

func fixtureExecProfile() *config.ExecutionProfile {
	return &config.ExecutionProfile{
		ByAppType: map[string]config.AppTypeProfile{
			"go-cli": {
				Topology: config.Topology{VerifierSequence: []string{"unit", "cli-runner"}},
				Lenses:   config.Lenses{LensSet: []string{"architecture-standards", "acceptance-invariants", "adversarial", "cross-harness-adversarial"}},
			},
			"docs": {
				Topology: config.Topology{VerifierSequence: []string{"schema-check", "citation-check", "cli-runner"}},
				Lenses:   config.Lenses{LensSet: []string{"architecture-standards", "acceptance-invariants"}},
			},
			"mixed": {
				Topology: config.Topology{VerifierSequence: []string{"unit", "fast"}},
				Lenses:   config.Lenses{LensSet: []string{"architecture-standards"}},
			},
			"noseq": {},
			"toomany": {
				Topology: config.Topology{VerifierSequence: []string{"a", "b", "c", "d", "e", "f", "g", "h"}},
			},
			"toomanylens": {
				Topology: config.Topology{VerifierSequence: []string{"unit"}},
				Lenses:   config.Lenses{LensSet: []string{"architecture-standards", "acceptance-invariants", "adversarial", "security", "web-a11y"}},
			},
			"badverifier": {
				Topology: config.Topology{VerifierSequence: []string{"nonexistent"}},
			},
			"badverifmodel": {
				Topology: config.Topology{VerifierSequence: []string{"emptymodel-verif"}},
			},
			"badlens": {
				Topology: config.Topology{VerifierSequence: []string{"unit"}},
				Lenses:   config.Lenses{LensSet: []string{"nonexistent-lens"}},
			},
			"badlensmodel": {
				Topology: config.Topology{VerifierSequence: []string{"unit"}},
				Lenses:   config.Lenses{LensSet: []string{"emptymodel-lens"}},
			},
		},
	}
}

func TestStageRouteValidate(t *testing.T) {
	if err := (StageRoute{Model: "m", ModelFamily: "f"}).validate("x"); err != nil {
		t.Fatalf("valid route rejected: %v", err)
	}
	if err := (StageRoute{Model: "", ModelFamily: "f"}).validate("x"); err == nil || !strings.Contains(err.Error(), "empty model") {
		t.Fatalf("want empty model error, got %v", err)
	}
	if err := (StageRoute{Model: "m", ModelFamily: " "}).validate("x"); err == nil || !strings.Contains(err.Error(), "empty model_family") {
		t.Fatalf("want empty model_family error, got %v", err)
	}
}

func TestBuildPipelineSpecSkeleton(t *testing.T) {
	spec, err := BuildPipelineSpec("/repo", "", fixtureStageProfiles(), nil)
	if err != nil {
		t.Fatalf("skeleton build: %v", err)
	}
	if spec.AppType != "" {
		t.Fatalf("skeleton AppType = %q, want empty", spec.AppType)
	}
	if got := len(spec.Verifiers); got != maxPipelineVerifiers {
		t.Fatalf("skeleton verifiers = %d, want %d", got, maxPipelineVerifiers)
	}
	if got := len(spec.RoutineLenses); got != maxPipelineRoutineLenses {
		t.Fatalf("skeleton routine lenses = %d, want %d", got, maxPipelineRoutineLenses)
	}
	if spec.CrossFamily == nil {
		t.Fatal("skeleton must have a cross-family gate")
	}
	if spec.CrossFamily.Model != "gpt-5.4" || spec.CrossFamily.ModelFamily != "gpt" {
		t.Fatalf("cross-family route = %+v", spec.CrossFamily)
	}
	if spec.TargetCount != pipelineTargetCount {
		t.Fatalf("target_count = %d, want %d", spec.TargetCount, pipelineTargetCount)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("skeleton spec invalid: %v", err)
	}
}

func TestBuildPipelineSpecSpecialized(t *testing.T) {
	spec, err := BuildPipelineSpec("/repo", "go-cli", fixtureStageProfiles(), fixtureExecProfile())
	if err != nil {
		t.Fatalf("go-cli build: %v", err)
	}
	if len(spec.Verifiers) != 2 {
		t.Fatalf("go-cli verifiers = %d, want 2", len(spec.Verifiers))
	}
	if spec.Verifiers[0].Slug != "unit" || spec.Verifiers[1].Slug != "cli-runner" {
		t.Fatalf("go-cli verifier slugs = %q,%q", spec.Verifiers[0].Slug, spec.Verifiers[1].Slug)
	}
	if len(spec.RoutineLenses) != 3 {
		t.Fatalf("go-cli routine lenses = %d, want 3", len(spec.RoutineLenses))
	}
	if spec.CrossFamily == nil {
		t.Fatal("go-cli should have cross-family (lens_set includes it)")
	}
}

func TestBuildPipelineSpecNoCrossFamily(t *testing.T) {
	spec, err := BuildPipelineSpec("/repo", "docs", fixtureStageProfiles(), fixtureExecProfile())
	if err != nil {
		t.Fatalf("docs build: %v", err)
	}
	if len(spec.Verifiers) != 3 {
		t.Fatalf("docs verifiers = %d, want 3", len(spec.Verifiers))
	}
	if len(spec.RoutineLenses) != 2 {
		t.Fatalf("docs routine lenses = %d, want 2", len(spec.RoutineLenses))
	}
	if spec.CrossFamily != nil {
		t.Fatal("docs lens_set has no cross-harness-adversarial; cross-family must be nil")
	}
}

func TestBuildPipelineSpecMixedModels(t *testing.T) {
	spec, err := BuildPipelineSpec("/repo", "mixed", fixtureStageProfiles(), fixtureExecProfile())
	if err != nil {
		t.Fatalf("mixed build: %v", err)
	}
	if spec.Verifiers[1].Model != "gpt-5.4-mini" {
		t.Fatalf("mixed verifier[1] model = %q, want gpt-5.4-mini", spec.Verifiers[1].Model)
	}
}

func TestBuildPipelineSpecErrors(t *testing.T) {
	sp := fixtureStageProfiles()
	ep := fixtureExecProfile()
	cases := []struct {
		name    string
		appType string
		sp      map[string]map[string]config.StageProfile
		ep      *config.ExecutionProfile
		want    string
	}{
		{"no-executor", "", map[string]map[string]config.StageProfile{}, nil, "stage_profiles.executor.default"},
		{"executor-empty-model", "", map[string]map[string]config.StageProfile{stageExecutor: {"default": {Model: "", ModelFamily: "claude"}}}, nil, "empty model"},
		{"skeleton-no-cross", "", map[string]map[string]config.StageProfile{stageExecutor: {"default": claudeProfile()}}, nil, "cross-harness-adversarial is not defined"},
		{"skeleton-cross-same-family", "", map[string]map[string]config.StageProfile{stageExecutor: {"default": claudeProfile()}, stageReviewer: {"cross-harness-adversarial": claudeProfile()}}, nil, "must differ from executor family"},
		{"specialize-nil-ep", "go-cli", sp, nil, "no execution_profile.by_app_type"},
		{"specialize-missing-apptype", "nope", sp, ep, "no entry for app_type"},
		{"specialize-no-seq", "noseq", sp, ep, "no verifier_sequence"},
		{"specialize-too-many-verifiers", "toomany", sp, ep, "exceeds cap"},
		{"specialize-too-many-lenses", "toomanylens", sp, ep, "exceeds cap"},
		{"specialize-bad-verifier", "badverifier", sp, ep, "has no stage_profiles.verifier entry"},
		{"specialize-bad-verifier-model", "badverifmodel", sp, ep, "empty model"},
		{"specialize-bad-lens", "badlens", sp, ep, "has no stage_profiles.reviewer entry"},
		{"specialize-bad-lens-model", "badlensmodel", sp, ep, "empty model"},
	}
	// Custom-config cases for the cross-family resolution error branches.
	crossEmptySP := map[string]map[string]config.StageProfile{
		stageExecutor: {"default": claudeProfile()},
		stageReviewer: {"cross-harness-adversarial": {Model: "", ModelFamily: "gpt"}},
	}
	crossMissingSP := map[string]map[string]config.StageProfile{
		stageExecutor: {"default": claudeProfile()},
		stageVerifier: {"unit": claudeProfile()},
		stageReviewer: {"architecture-standards": claudeProfile()},
	}
	crossMissingEP := &config.ExecutionProfile{ByAppType: map[string]config.AppTypeProfile{
		"xc": {
			Topology: config.Topology{VerifierSequence: []string{"unit"}},
			Lenses:   config.Lenses{LensSet: []string{"architecture-standards", "cross-harness-adversarial"}},
		},
	}}
	cases = append(cases,
		struct {
			name    string
			appType string
			sp      map[string]map[string]config.StageProfile
			ep      *config.ExecutionProfile
			want    string
		}{"skeleton-cross-empty-model", "", crossEmptySP, nil, "empty model"},
		struct {
			name    string
			appType string
			sp      map[string]map[string]config.StageProfile
			ep      *config.ExecutionProfile
			want    string
		}{"specialize-cross-missing", "xc", crossMissingSP, crossMissingEP, "cross-harness-adversarial is not defined"},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildPipelineSpec("/repo", tc.appType, tc.sp, tc.ep)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestPipelineSpecValidate(t *testing.T) {
	good := func() PipelineSpec {
		exec := StageRoute{Model: "claude-opus-4-8", ModelFamily: "claude"}
		cross := StageRoute{Slug: "cross-harness-adversarial", Model: "gpt-5.4", ModelFamily: "gpt"}
		return PipelineSpec{
			Workspace:     "/repo",
			TargetCount:   3,
			Executor:      exec,
			Verifiers:     []StageRoute{exec},
			RoutineLenses: []StageRoute{exec},
			CrossFamily:   &cross,
		}
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("good spec rejected: %v", err)
	}

	mutate := func(f func(*PipelineSpec)) PipelineSpec {
		s := good()
		f(&s)
		return s
	}
	cases := []struct {
		name string
		spec PipelineSpec
		want string
	}{
		{"empty-workspace", mutate(func(s *PipelineSpec) { s.Workspace = " " }), "empty workspace"},
		{"bad-target", mutate(func(s *PipelineSpec) { s.TargetCount = 0 }), "target_count must be >= 1"},
		{"bad-executor", mutate(func(s *PipelineSpec) { s.Executor.Model = "" }), "empty model"},
		{"no-verifiers", mutate(func(s *PipelineSpec) { s.Verifiers = nil }), "at least one verifier"},
		{"too-many-verifiers", mutate(func(s *PipelineSpec) {
			s.Verifiers = make([]StageRoute, maxPipelineVerifiers+1)
			for i := range s.Verifiers {
				s.Verifiers[i] = StageRoute{Model: "m", ModelFamily: "f"}
			}
		}), "exceeds cap"},
		{"bad-verifier", mutate(func(s *PipelineSpec) { s.Verifiers = []StageRoute{{Model: "", ModelFamily: "f"}} }), "verifier slot 1"},
		{"too-many-lenses", mutate(func(s *PipelineSpec) {
			s.RoutineLenses = make([]StageRoute, maxPipelineRoutineLenses+1)
			for i := range s.RoutineLenses {
				s.RoutineLenses[i] = StageRoute{Model: "m", ModelFamily: "f"}
			}
		}), "exceeds cap"},
		{"bad-lens", mutate(func(s *PipelineSpec) { s.RoutineLenses = []StageRoute{{Model: "", ModelFamily: "f"}} }), "routine lens slot 1"},
		{"bad-cross", mutate(func(s *PipelineSpec) { s.CrossFamily = &StageRoute{Model: "", ModelFamily: "gpt"} }), "cross-family lens"},
		{"cross-same-family", mutate(func(s *PipelineSpec) { s.CrossFamily = &StageRoute{Model: "x", ModelFamily: "claude"} }), "must differ from executor family"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPipelineSpecDigestDeterministicAndSensitive(t *testing.T) {
	specA, _ := BuildPipelineSpec("/repo", "", fixtureStageProfiles(), nil)
	specB, _ := BuildPipelineSpec("/other-repo", "", fixtureStageProfiles(), nil)
	if specA.Digest() != specB.Digest() {
		t.Fatal("digest must ignore workspace (config digest only)")
	}
	if specA.Digest() == "" || len(specA.Digest()) != 12 {
		t.Fatalf("digest should be 12 hex chars, got %q", specA.Digest())
	}
	// A routing change must move the digest.
	specC := specA
	specC.Verifiers = append([]StageRoute{}, specA.Verifiers...)
	specC.Verifiers[0] = StageRoute{Model: "different-model", ModelFamily: "claude"}
	if specA.Digest() == specC.Digest() {
		t.Fatal("digest must change when a verifier route changes")
	}
	// Cross-family presence must move the digest.
	specD := specA
	specD.CrossFamily = nil
	if specA.Digest() == specD.Digest() {
		t.Fatal("digest must change when cross-family presence changes")
	}
}

func TestPipelineProjectorFor(t *testing.T) {
	p, err := PipelineProjectorFor("omp")
	if err != nil {
		t.Fatalf("omp projector: %v", err)
	}
	if p.Platform() != "omp" {
		t.Fatalf("platform = %q", p.Platform())
	}
	cc, err := PipelineProjectorFor("claude-code")
	if err != nil {
		t.Fatalf("claude-code projector: %v", err)
	}
	if cc.Platform() != "claude-code" {
		t.Fatalf("cc platform = %q", cc.Platform())
	}
	if _, err := PipelineProjectorFor("bogus"); err == nil || !strings.Contains(err.Error(), "unknown pipeline platform") {
		t.Fatalf("bogus platform should be unknown, got %v", err)
	}
}

func TestSupportedPipelinePlatforms(t *testing.T) {
	got := SupportedPipelinePlatforms()
	if len(got) != 2 || got[0] != "claude-code" || got[1] != "omp" {
		t.Fatalf("supported platforms = %v, want [claude-code omp]", got)
	}
}

func TestLookupStageRoute(t *testing.T) {
	sp := fixtureStageProfiles()
	if _, ok := lookupStageRoute(sp, "nostage", "x"); ok {
		t.Fatal("missing stage should not resolve")
	}
	if _, ok := lookupStageRoute(sp, stageExecutor, "missing"); ok {
		t.Fatal("missing slug should not resolve")
	}
	r, ok := lookupStageRoute(sp, stageExecutor, "default")
	if !ok || r.Model != "claude-opus-4-8" || r.Slug != "default" {
		t.Fatalf("executor/default resolved wrong: %+v ok=%v", r, ok)
	}
}
