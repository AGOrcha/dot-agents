package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// pipeline_projection.go is the O1 Layer-2 seam: it turns the resolved profile IR
// (stage_profiles + execution_profile topology, full-loop-craft §1/§2/§6) into a
// materialized per-task pipeline for a target harness. The swarm YAML under
// .agents/workflow/runtime/full-loop/ is hand-written today; this makes it a
// GENERATED build artifact so the running pipeline never drifts from
// stage_profiles (craft §7 anti-pattern: hand-editing emitted swarm YAML).
//
// The projection is per-harness by construction (craft §6): OMP and Claude-Code
// drive `da workflow` directly, codex reads the artifacts, cursor runs the
// contract with no CLI — one emitted shape cannot serve all four. This slice
// ships two implementations: the OMP swarm-YAML projector (the beachhead) and
// the Claude-Code dynamic-workflow projector (cc_pipeline.go). Codex/cursor
// projectors remain declared follow-ons.

const (
	// maxPipelineVerifiers caps the verifier fan-out (craft §4: verifiers ≤7;
	// over-cardinality is a BLOCK refusal, never a silent truncation).
	maxPipelineVerifiers = 7
	// maxPipelineRoutineLenses caps the routine review lens fan-out (craft §4:
	// routine lenses ≤4).
	maxPipelineRoutineLenses = 4
	// crossFamilyLensSlug is the NAMED slug the blocking cross-family gate binds
	// to (craft §2 RULE-7 / §7 anti-pattern: never bind diversity to a numeric
	// slot index). Its presence in an app_type's lens_set toggles the cross-family
	// review stage.
	crossFamilyLensSlug = "cross-harness-adversarial"
	// pipelineTargetCount is the bounded fold-back re-entry ceiling for the inner
	// pipeline (craft §3: target_count is a hard iteration ceiling, = 3).
	pipelineTargetCount = 3
	// executorDefaultSlug is the stage_profiles.executor slug the swarm default /
	// executor / gate / reconcile routes resolve from.
	executorDefaultSlug = "default"

	stageExecutor = "executor"
	stageVerifier = "verifier"
	stageReviewer = "reviewer"
)

// StageRoute is one resolved stage's explicit model route (craft §2: every stage
// is a typed StageProfile with an explicit model AND model_family; a matched
// stage whose model or model_family is empty is refused, never emitted). Slug is
// empty for the app_type-agnostic skeleton slots (runtime profile_resolve fills
// the concrete route per task) and set for an app_type-specialized projection.
type StageRoute struct {
	Slug        string
	Model       string
	ModelFamily string
}

// validate refuses a route with an empty model or model_family (craft §2 rule).
func (r StageRoute) validate(what string) error {
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("%s: empty model", what)
	}
	if strings.TrimSpace(r.ModelFamily) == "" {
		return fmt.Errorf("%s: empty model_family", what)
	}
	return nil
}

// PipelineSpec is the resolved, platform-agnostic IR a PipelineProjector renders.
// It is the single input from which any harness projection is generated, so two
// harness projectors reading the same spec agree on models, cardinality, and
// topology by construction.
type PipelineSpec struct {
	// Workspace is the absolute repo root the emitted pipeline runs in.
	Workspace string
	// AppType is the app_type this pipeline was specialized for, or "" for the
	// app_type-agnostic maximal skeleton (7 verify + 4 routine slots) that the
	// runtime profile_resolve stage narrows per task.
	AppType string
	// TargetCount is the bounded fold-back re-entry ceiling (craft §3).
	TargetCount int
	// Executor is the executor/default route; it also drives the swarm default
	// model and the gate/reconcile/profile_resolve routes.
	Executor StageRoute
	// Verifiers is the ordered verifier slot routes (1..7). For the skeleton every
	// slot carries the executor route; a specialized projection binds each slot to
	// its verifier_sequence slug's stage_profile.
	Verifiers []StageRoute
	// RoutineLenses is the ordered routine review lens routes (0..4), excluding the
	// cross-family lens.
	RoutineLenses []StageRoute
	// CrossFamily is the blocking cross-family adversarial route, or nil when the
	// app_type's lens_set does not include the cross-harness-adversarial slug.
	CrossFamily *StageRoute
}

// Validate enforces the cardinality caps and the non-empty-route rule before any
// projector renders the spec (craft §2/§4).
func (s PipelineSpec) Validate() error {
	if strings.TrimSpace(s.Workspace) == "" {
		return fmt.Errorf("pipeline spec: empty workspace")
	}
	if s.TargetCount < 1 {
		return fmt.Errorf("pipeline spec: target_count must be >= 1, got %d", s.TargetCount)
	}
	if err := s.Executor.validate("executor"); err != nil {
		return err
	}
	if len(s.Verifiers) == 0 {
		return fmt.Errorf("pipeline spec: at least one verifier slot is required")
	}
	if len(s.Verifiers) > maxPipelineVerifiers {
		return fmt.Errorf("pipeline spec: %d verifiers exceeds cap %d (craft §4: BLOCK, never truncate)", len(s.Verifiers), maxPipelineVerifiers)
	}
	if err := validateStageRoutes(s.Verifiers, "verifier"); err != nil {
		return err
	}
	if len(s.RoutineLenses) > maxPipelineRoutineLenses {
		return fmt.Errorf("pipeline spec: %d routine lenses exceeds cap %d (craft §4: BLOCK, never truncate)", len(s.RoutineLenses), maxPipelineRoutineLenses)
	}
	if err := validateStageRoutes(s.RoutineLenses, "routine lens"); err != nil {
		return err
	}
	if s.CrossFamily != nil {
		if err := s.CrossFamily.validate("cross-family lens"); err != nil {
			return err
		}
		if s.CrossFamily.ModelFamily == s.Executor.ModelFamily {
			return fmt.Errorf("pipeline spec: cross-family lens family %q must differ from executor family %q (craft §2 RULE-7)", s.CrossFamily.ModelFamily, s.Executor.ModelFamily)
		}
	}
	return nil
}

// validateStageRoutes refuses any route in the slot group whose model or
// model_family is empty, labelling the offending slot 1-indexed within the group.
func validateStageRoutes(routes []StageRoute, label string) error {
	for i, r := range routes {
		if err := r.validate(fmt.Sprintf("%s slot %d", label, i+1)); err != nil {
			return err
		}
	}
	return nil
}

// Digest is the stable config digest stamped into the generated header comment.
// It is computed over the routing/topology inputs ONLY (never the workspace path
// or any wall-clock value), so re-emitting the same profile IR yields a
// byte-identical artifact (idempotence) while a stage_profiles/topology change
// registers as drift.
func (s PipelineSpec) Digest() string {
	type routeDigest struct {
		Slug   string `json:"slug"`
		Model  string `json:"model"`
		Family string `json:"family"`
	}
	toDigest := func(r StageRoute) routeDigest {
		return routeDigest{Slug: r.Slug, Model: r.Model, Family: r.ModelFamily}
	}
	payload := struct {
		AppType       string        `json:"app_type"`
		TargetCount   int           `json:"target_count"`
		Executor      routeDigest   `json:"executor"`
		Verifiers     []routeDigest `json:"verifiers"`
		RoutineLenses []routeDigest `json:"routine_lenses"`
		CrossFamily   *routeDigest  `json:"cross_family"`
	}{
		AppType:     s.AppType,
		TargetCount: s.TargetCount,
		Executor:    toDigest(s.Executor),
	}
	for _, v := range s.Verifiers {
		payload.Verifiers = append(payload.Verifiers, toDigest(v))
	}
	for _, l := range s.RoutineLenses {
		payload.RoutineLenses = append(payload.RoutineLenses, toDigest(l))
	}
	if s.CrossFamily != nil {
		cf := toDigest(*s.CrossFamily)
		payload.CrossFamily = &cf
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

// PipelineArtifact is one emitted file: a repo-relative filename and its full
// content. A projector returns the complete set for one emission.
type PipelineArtifact struct {
	// Name is the artifact filename relative to the platform's runtime dir
	// (e.g. "profile-driven.swarm.yaml").
	Name string
	// Content is the full file body, terminated by a single trailing newline.
	Content string
}

// PipelineProjector renders a PipelineSpec into a target harness's materialized
// per-task pipeline. It is platform-agnostic: the OMP implementation emits swarm
// YAML and the Claude-Code implementation emits dynamic-workflow `.mjs`.
type PipelineProjector interface {
	// Platform is the projector's target platform id (e.g. "omp").
	Platform() string
	// RuntimeRelDir is the repo-relative directory the projector's artifacts are
	// emitted into (each harness owns its own runtime location).
	RuntimeRelDir() string
	// Emit renders the pipeline artifacts from the spec, or an error when the
	// spec is invalid for this platform.
	Emit(spec PipelineSpec) ([]PipelineArtifact, error)
}

// PipelineProjectorFor returns the projector for a target platform id. Both the
// OMP beachhead and the Claude-Code dynamic-workflow projector are implemented;
// any other id is unknown.
func PipelineProjectorFor(platform string) (PipelineProjector, error) {
	switch strings.TrimSpace(platform) {
	case "omp":
		return ompPipelineProjector{}, nil
	case "claude-code":
		return ccPipelineProjector{}, nil
	default:
		return nil, fmt.Errorf("unknown pipeline platform %q (supported: %s)", platform, strings.Join(SupportedPipelinePlatforms(), ", "))
	}
}

// BuildPipelineSpec resolves stage_profiles + execution_profile topology into a
// PipelineSpec for the given app_type. An empty appType yields the maximal
// app_type-agnostic skeleton (7 verify + 4 routine slots + cross-family), which
// the runtime profile_resolve stage narrows per task; a named appType yields the
// specialized projection whose slot count equals that app_type's
// verifier_sequence / lens_set (still capped ≤7 / ≤4).
func BuildPipelineSpec(workspace, appType string, stageProfiles map[string]map[string]config.StageProfile, ep *config.ExecutionProfile) (PipelineSpec, error) {
	executor, ok := lookupStageRoute(stageProfiles, stageExecutor, executorDefaultSlug)
	if !ok {
		return PipelineSpec{}, fmt.Errorf("stage_profiles.executor.%s is not defined", executorDefaultSlug)
	}
	if err := executor.validate("executor/default"); err != nil {
		return PipelineSpec{}, err
	}
	spec := PipelineSpec{
		Workspace:   workspace,
		AppType:     strings.TrimSpace(appType),
		TargetCount: pipelineTargetCount,
		Executor:    executor,
	}

	if spec.AppType == "" {
		return buildSkeletonSpec(spec, stageProfiles)
	}

	if ep == nil || len(ep.ByAppType) == 0 {
		return PipelineSpec{}, fmt.Errorf("no execution_profile.by_app_type entries; cannot specialize for app_type %q", spec.AppType)
	}
	prof, ok := ep.ByAppType[spec.AppType]
	if !ok {
		return PipelineSpec{}, fmt.Errorf("execution_profile.by_app_type has no entry for app_type %q", spec.AppType)
	}
	verifiers, err := resolveVerifierRoutes(stageProfiles, prof.Topology.VerifierSequence, spec.AppType)
	if err != nil {
		return PipelineSpec{}, err
	}
	spec.Verifiers = verifiers

	routine, cross, err := resolveLensRoutes(stageProfiles, prof.Lenses.LensSet, executor, spec.AppType)
	if err != nil {
		return PipelineSpec{}, err
	}
	spec.RoutineLenses = routine
	spec.CrossFamily = cross
	return spec, nil
}

// buildSkeletonSpec fills the maximal app_type-agnostic skeleton: every verify
// and routine slot carries the executor route (runtime profile_resolve binds the
// concrete per-task routes) and the cross-family gate is always present.
func buildSkeletonSpec(spec PipelineSpec, stageProfiles map[string]map[string]config.StageProfile) (PipelineSpec, error) {
	for range maxPipelineVerifiers {
		spec.Verifiers = append(spec.Verifiers, spec.Executor)
	}
	for range maxPipelineRoutineLenses {
		spec.RoutineLenses = append(spec.RoutineLenses, spec.Executor)
	}
	cross, err := resolveCrossFamily(stageProfiles, spec.Executor)
	if err != nil {
		return PipelineSpec{}, err
	}
	spec.CrossFamily = cross
	return spec, nil
}

// resolveVerifierRoutes resolves an app_type's verifier_sequence into explicit
// verifier routes in declared order, enforcing the ≤7 cap (craft §4: BLOCK,
// never truncate) and refusing an unmapped or empty-route slug.
func resolveVerifierRoutes(stageProfiles map[string]map[string]config.StageProfile, sequence []string, appType string) ([]StageRoute, error) {
	if len(sequence) == 0 {
		return nil, fmt.Errorf("app_type %q topology declares no verifier_sequence", appType)
	}
	if len(sequence) > maxPipelineVerifiers {
		return nil, fmt.Errorf("app_type %q verifier_sequence has %d entries, exceeds cap %d (craft §4: BLOCK)", appType, len(sequence), maxPipelineVerifiers)
	}
	var verifiers []StageRoute
	for _, slug := range sequence {
		route, ok := lookupStageRoute(stageProfiles, stageVerifier, slug)
		if !ok {
			return nil, fmt.Errorf("app_type %q verifier %q has no stage_profiles.verifier entry", appType, slug)
		}
		if err := route.validate(fmt.Sprintf("verifier %q", slug)); err != nil {
			return nil, err
		}
		verifiers = append(verifiers, route)
	}
	return verifiers, nil
}

// resolveLensRoutes partitions an app_type's lens_set into routine lens routes
// (declared order, capped ≤4) and the optional cross-family route, resolving each
// against stage_profiles.reviewer. The cross-family lens binds by NAMED slug
// (craft §2 RULE-7 / §7): its presence in lens_set toggles the cross-family gate.
func resolveLensRoutes(stageProfiles map[string]map[string]config.StageProfile, lensSet []string, executor StageRoute, appType string) ([]StageRoute, *StageRoute, error) {
	var routine []StageRoute
	var cross *StageRoute
	for _, slug := range lensSet {
		if slug == crossFamilyLensSlug {
			c, err := resolveCrossFamily(stageProfiles, executor)
			if err != nil {
				return nil, nil, err
			}
			cross = c
			continue
		}
		route, ok := lookupStageRoute(stageProfiles, stageReviewer, slug)
		if !ok {
			return nil, nil, fmt.Errorf("app_type %q review lens %q has no stage_profiles.reviewer entry", appType, slug)
		}
		if err := route.validate(fmt.Sprintf("review lens %q", slug)); err != nil {
			return nil, nil, err
		}
		routine = append(routine, route)
	}
	if len(routine) > maxPipelineRoutineLenses {
		return nil, nil, fmt.Errorf("app_type %q has %d routine review lenses, exceeds cap %d (craft §4: BLOCK)", appType, len(routine), maxPipelineRoutineLenses)
	}
	return routine, cross, nil
}

// resolveCrossFamily resolves the blocking cross-family adversarial route from
// stage_profiles.reviewer and asserts family inequality against the executor
// (craft §2 RULE-7: same family both sides ⇒ review invalid).
func resolveCrossFamily(stageProfiles map[string]map[string]config.StageProfile, executor StageRoute) (*StageRoute, error) {
	route, ok := lookupStageRoute(stageProfiles, stageReviewer, crossFamilyLensSlug)
	if !ok {
		return nil, fmt.Errorf("stage_profiles.reviewer.%s is not defined but the pipeline requires a cross-family gate", crossFamilyLensSlug)
	}
	if err := route.validate("cross-family lens"); err != nil {
		return nil, err
	}
	if route.ModelFamily == executor.ModelFamily {
		return nil, fmt.Errorf("cross-family lens family %q must differ from executor family %q (craft §2 RULE-7)", route.ModelFamily, executor.ModelFamily)
	}
	return &route, nil
}

// lookupStageRoute reads one stage_profiles.<stage>.<slug> entry into a
// StageRoute, reporting whether the profile exists at all.
func lookupStageRoute(stageProfiles map[string]map[string]config.StageProfile, stage, slug string) (StageRoute, bool) {
	profiles, ok := stageProfiles[stage]
	if !ok {
		return StageRoute{}, false
	}
	prof, ok := profiles[slug]
	if !ok {
		return StageRoute{}, false
	}
	return StageRoute{
		Slug:        slug,
		Model:       strings.TrimSpace(prof.Model),
		ModelFamily: strings.TrimSpace(prof.ModelFamily),
	}, true
}

// SupportedPipelinePlatforms is the sorted list of platform ids with a
// PipelineProjector implementation.
func SupportedPipelinePlatforms() []string {
	out := []string{"claude-code", "omp"}
	sort.Strings(out)
	return out
}
