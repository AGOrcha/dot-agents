package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// --- positive: each sub-score band fires on the right inputs ---------------

func TestScoreHookOutcomeBytesBandRemediate(t *testing.T) {
	// A single remediate record drives the sub-score to 0.0 regardless of
	// any allow/advise records that share the iteration. The band ordering
	// (remediate > advise > allow) is the load-bearing invariant here.
	yaml := []byte(`schema_version: 1
records:
  - sentinel_id: s1
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: remediate
    rule_id: iteration-close.R1.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
  - sentinel_id: s2
    skill: loop-worker
    lifecycle_point: pre_tool_use
    intervention_class: prevent_before_action
    result: allow
    rule_id: loop-worker.R3.1
    platform: claude
    ts: "2026-05-26T00:00:01Z"
    correlation_id: c2
`)
	got := scoreHookOutcomeBytes(yaml, 7)
	if !got.Present {
		t.Fatalf("Present = false, want true: %+v", got)
	}
	if got.SubScore != hookSubScoreAllRemediate {
		t.Errorf("SubScore = %g, want %g", got.SubScore, hookSubScoreAllRemediate)
	}
	if !strings.Contains(got.Detail, "remediate:") || !strings.Contains(got.Detail, "iteration-close.R1.1") {
		t.Errorf("Detail = %q, want it to surface the remediating rule", got.Detail)
	}
}

func TestScoreHookOutcomeBytesBandAdvise(t *testing.T) {
	// No remediate, one advise → 0.6. Allow records do not pull the band up.
	yaml := []byte(`schema_version: 1
records:
  - sentinel_id: s1
    skill: loop-worker
    lifecycle_point: subagent_stop
    intervention_class: remediate_at_stop
    result: advise
    rule_id: loop-worker.R3.4
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
  - sentinel_id: s2
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: allow
    rule_id: iteration-close.R1.2
    platform: claude
    ts: "2026-05-26T00:00:01Z"
    correlation_id: c2
`)
	got := scoreHookOutcomeBytes(yaml, 3)
	if !got.Present || got.SubScore != hookSubScoreAllAdvise {
		t.Errorf("SubScore = %g, want %g (present=%v)", got.SubScore, hookSubScoreAllAdvise, got.Present)
	}
	if !strings.Contains(got.Detail, "advise:") || !strings.Contains(got.Detail, "loop-worker.R3.4") {
		t.Errorf("Detail = %q, want advising rule named", got.Detail)
	}
}

func TestScoreHookOutcomeBytesBandAllow(t *testing.T) {
	// Pure allow → 1.0.
	yaml := []byte(`schema_version: 1
records:
  - sentinel_id: s1
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: allow
    rule_id: iteration-close.R1.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
`)
	got := scoreHookOutcomeBytes(yaml, 1)
	if !got.Present || got.SubScore != hookSubScoreAllAllow {
		t.Errorf("SubScore = %g, want %g (present=%v)", got.SubScore, hookSubScoreAllAllow, got.Present)
	}
}

// --- negative: t1b deferred classes never reach the score ------------------

func TestScoreHookOutcomeBytesDropsDeferredInterventionClasses(t *testing.T) {
	// continuity_advice and observe_tool_result are R1.5.1 work per t1b
	// (PR #97) and MUST NOT contribute to the v1 sub-score. A sidecar that
	// contains only those classes should resolve to absent — the renormalizer
	// then drops the signal so it does not vote.
	yaml := []byte(`schema_version: 1
records:
  - sentinel_id: s1
    skill: iteration-close
    lifecycle_point: pre_compact
    intervention_class: continuity_advice
    result: advise
    rule_id: iteration-close.R5.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
  - sentinel_id: s2
    skill: loop-worker
    lifecycle_point: post_tool_use
    intervention_class: observe_tool_result
    result: remediate
    rule_id: loop-worker.R9.1
    platform: claude
    ts: "2026-05-26T00:00:01Z"
    correlation_id: c2
`)
	got := scoreHookOutcomeBytes(yaml, 9)
	if got.Present {
		t.Errorf("Present = true, want false — continuity_advice & observe_tool_result must not score (t1b PR #97): %+v", got)
	}
	if !strings.Contains(got.Detail, "continuity_advice/observe_tool_result are deferred") {
		t.Errorf("Detail = %q, want it to explain the deferral", got.Detail)
	}
}

// --- D4 dedup ----------------------------------------------------------------

func TestDedupCollapsesPreventAndRemediatePair(t *testing.T) {
	// A pre_tool_use prevent + a stop remediate sharing the same
	// (correlation_id, rule_id) collapse to one record at the more severe
	// result (remediate). Without this, an iteration where one prevented
	// action is also remediated at stop would double-count the same intent.
	records := []hookOutcomeRecord{
		{InterventionClass: hookInterventionPreventBeforeAction, Result: hookResultAdvise, RuleID: "loop-worker.R3.1", CorrelationID: "c1"},
		{InterventionClass: hookInterventionRemediateAtStop, Result: hookResultRemediate, RuleID: "loop-worker.R3.1", CorrelationID: "c1"},
	}
	got := dedupHookOutcomesByCorrelation(records)
	if len(got) != 1 {
		t.Fatalf("dedup len = %d, want 1 (the two records share correlation+rule)", len(got))
	}
	if got[0].Result != hookResultRemediate {
		t.Errorf("collapsed result = %q, want remediate (more severe wins)", got[0].Result)
	}
}

func TestDedupKeepsDistinctCorrelations(t *testing.T) {
	// Same rule, different correlation_id → two records, both counted.
	records := []hookOutcomeRecord{
		{InterventionClass: hookInterventionRemediateAtStop, Result: hookResultRemediate, RuleID: "R", CorrelationID: "c1"},
		{InterventionClass: hookInterventionRemediateAtStop, Result: hookResultRemediate, RuleID: "R", CorrelationID: "c2"},
	}
	got := dedupHookOutcomesByCorrelation(records)
	if len(got) != 2 {
		t.Errorf("dedup len = %d, want 2 (different correlations)", len(got))
	}
}

func TestDedupKeepsEmptyCorrelationDistinct(t *testing.T) {
	// Records with empty correlation_id never collapse — they cannot be
	// proven to be the same intent. Both surface.
	records := []hookOutcomeRecord{
		{InterventionClass: hookInterventionRemediateAtStop, Result: hookResultRemediate, RuleID: "R", CorrelationID: ""},
		{InterventionClass: hookInterventionRemediateAtStop, Result: hookResultRemediate, RuleID: "R", CorrelationID: ""},
	}
	got := dedupHookOutcomesByCorrelation(records)
	if len(got) != 2 {
		t.Errorf("dedup len = %d, want 2 (empty correlation never collapses)", len(got))
	}
}

// --- absent-signal cases ----------------------------------------------------

func TestExtractHookOutcomesSignalAbsentWhenSidecarMissing(t *testing.T) {
	dir := t.TempDir()
	got := ExtractHookOutcomesSignal(dir, 5)
	if got.Present {
		t.Errorf("Present = true, want false when no sidecar exists: %+v", got)
	}
	if !strings.Contains(got.Detail, "iter-5") {
		t.Errorf("Detail = %q, want it to name the iteration", got.Detail)
	}
}

func TestExtractHookOutcomesSignalEmptyIterLogDir(t *testing.T) {
	got := ExtractHookOutcomesSignal("", 1)
	if got.Present {
		t.Errorf("Present = true, want false for empty iter-log dir: %+v", got)
	}
}

func TestExtractHookOutcomesSignalMalformedYAMLIsAbsentNotError(t *testing.T) {
	// A corrupt sidecar degrades to absent rather than erroring out the
	// whole score — the rest of the rubric must still produce a number.
	dir := t.TempDir()
	path := filepath.Join(dir, hookOutcomeFileName(2))
	if err := os.WriteFile(path, []byte("this: is: not: valid: yaml: ["), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := ExtractHookOutcomesSignal(dir, 2)
	if got.Present {
		t.Errorf("Present = true, want false on malformed YAML: %+v", got)
	}
}

// --- end-to-end via ExtractHookOutcomesSignal -------------------------------

func TestExtractHookOutcomesSignalReadsSidecarOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, hookOutcomeFileName(4))
	body := []byte(`schema_version: 1
records:
  - sentinel_id: s1
    skill: iteration-close
    lifecycle_point: stop
    intervention_class: remediate_at_stop
    result: remediate
    rule_id: iteration-close.R1.1
    platform: claude
    ts: "2026-05-26T00:00:00Z"
    correlation_id: c1
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got := ExtractHookOutcomesSignal(dir, 4)
	if !got.Present || got.SubScore != hookSubScoreAllRemediate {
		t.Errorf("got %+v, want present 0.0 (remediate)", got)
	}
}

// --- joinSortedUnique invariant: deterministic and dedup'd ------------------

func TestJoinSortedUniqueIsStableAndUnique(t *testing.T) {
	got := joinSortedUnique([]string{"b", "a", "b", "c", "a"})
	if got != "a, b, c" {
		t.Errorf("joinSortedUnique = %q, want %q", got, "a, b, c")
	}
	if got := joinSortedUnique(nil); got != "" {
		t.Errorf("joinSortedUnique(nil) = %q, want empty", got)
	}
}

// --- integration: a non-empty sidecar contributes a row through the scorer

func TestRubricScoreIncludesHookOutcomesContribution(t *testing.T) {
	// End-to-end at the rubric layer: when the hook_outcomes signal is
	// present in the SignalSet, the rubric's Score() returns a breakdown
	// row for it with the configured weight (0.10 nominal).
	r := DefaultRubric()
	set := SignalSet{
		Iteration:    1,
		HookOutcomes: PresentSignal(hookSubScoreAllAdvise, "advise: loop-worker.R3.4"),
	}
	got := r.Score(set)
	if !got.Scored {
		t.Fatalf("Scored = false, want true with one present signal: %+v", got)
	}
	if got.Value != hookSubScoreAllAdvise {
		t.Errorf("Value = %g, want %g (single-signal renormalizes to its own sub-score)", got.Value, hookSubScoreAllAdvise)
	}
	var found bool
	for _, row := range got.Breakdown {
		if row.Signal == SignalHookOutcomes {
			found = true
			if row.NominalWeight != 0.10 {
				t.Errorf("hook_outcomes NominalWeight = %g, want 0.10", row.NominalWeight)
			}
			if !row.Present {
				t.Errorf("hook_outcomes row Present = false, want true")
			}
		}
	}
	if !found {
		t.Errorf("breakdown is missing the hook_outcomes row")
	}
}

// TestExtractHookOutcomesSignal_NonIsNotExistReadError covers the L112
// "cannot read hook-outcomes sidecar" branch (non-IsNotExist os error).
// Drive it by pointing the extractor at an unreadable directory entry.
func TestExtractHookOutcomesSignal_NonIsNotExistReadError(t *testing.T) {
	dir := t.TempDir()
	// Create a *directory* at the sidecar path so ReadFile returns EISDIR,
	// which is not IsNotExist — exercises the wrapped-error AbsentSignal.
	sidecar := filepath.Join(dir, "iter-7.hook-outcomes.yaml")
	if err := os.MkdirAll(sidecar, 0o755); err != nil {
		t.Fatalf("mkdir sidecar-as-dir: %v", err)
	}
	v := ExtractHookOutcomesSignal(dir, 7)
	if v.Present {
		t.Fatalf("expected absent signal for non-IsNotExist read error, got %+v", v)
	}
	if !strings.Contains(v.Detail, "cannot read") {
		t.Errorf("expected detail to mention 'cannot read', got %q", v.Detail)
	}
}

// TestHookResultPriority_AllowAndDefault covers the L188-194 hookResultAllow
// case and the documented-unreachable default branch via a direct call to
// the unexported helper. Both branches are exercised by constructing the
// minimal record set that selects each.
func TestHookResultPriority_AllowAndDefault(t *testing.T) {
	if got := hookResultSeverity(hookResultAllow); got != 0 {
		t.Errorf("hookResultSeverity(allow) = %d, want 0", got)
	}
	if got := hookResultSeverity("totally-unknown"); got != -1 {
		t.Errorf("hookResultSeverity(unknown) = %d, want -1 (default)", got)
	}
}

// --- companion-gate compatibility (R1.5 t2d, cross-plan contract) ----------
//
// These tests load YAML sidecars contributed by the orchestration-companion
// plan and assert the R1.5 extractor + scorer handle them without any schema
// change. They are the contract the companion plan's implementation tasks
// will consume as a fixture test on their side: if either side drifts (the
// companion gates start emitting a new skill/lifecycle/rule_id shape that
// doesn't satisfy the schema, or the R1.5 extractor changes its dedup or
// banding semantics), these tests fail loudly and pin down the breaking edge.
//
// The fixtures live under tests/fixtures/r1-5-hook-outcomes/companion-gates/
// (the canonical cross-plan contract location) rather than inline byte
// literals so the companion plan can reuse the same files verbatim without
// copy-paste drift.

// companionFixturePath returns the repo-root-anchored path to a companion
// gate fixture. internal/scoring tests run with their package directory as
// cwd, so the fixtures live two directories up.
func companionFixturePath(name string) string {
	return filepath.Join("..", "..", "tests", "fixtures", "r1-5-hook-outcomes", "companion-gates", name)
}

// loadCompanionFixture reads a fixture and fails the test if it is missing —
// a missing fixture is a contract break, not a soft signal.
func loadCompanionFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(companionFixturePath(name))
	if err != nil {
		t.Fatalf("load companion fixture %q: %v", name, err)
	}
	return data
}

// TestCompanionGate_OrchestratorHandoffRemediateBandFires asserts that a
// pre_tool_use prevent + stop remediate pair emitted by
// orchestrator-handoff-gate (orchestration-companion design R3.7 → R3.1)
// collapses via D4 dedup to a single remediate record and drives the
// sub-score to 0.0. The fixture exercises the orchestrator-session-start
// skill, which the R1.5 schema already enumerates — proving no schema
// change is required for the companion plan's terminal-gate emissions.
func TestCompanionGate_OrchestratorHandoffRemediateBandFires(t *testing.T) {
	got := scoreHookOutcomeBytes(loadCompanionFixture(t, "orchestrator-handoff-remediate.yaml"), 11)
	if !got.Present {
		t.Fatalf("Present = false, want true: %+v", got)
	}
	if got.SubScore != hookSubScoreAllRemediate {
		t.Errorf("SubScore = %g, want %g (one collapsed remediate from R3.7 prevent+stop pair)", got.SubScore, hookSubScoreAllRemediate)
	}
	if !strings.Contains(got.Detail, "remediate:") || !strings.Contains(got.Detail, "orchestrator-handoff-gate.R3.7") {
		t.Errorf("Detail = %q, want it to surface orchestrator-handoff-gate.R3.7", got.Detail)
	}
}

// TestCompanionGate_OrchestratorHandoffDedupCollapsesPair drills the D4
// dedup invariant directly on the parsed records — the prevent+stop pair
// sharing (correlation_id, rule_id) must collapse to one record. This is
// the load-bearing property that prevents the companion gate from double-
// counting the same prevented-then-remediated intent.
func TestCompanionGate_OrchestratorHandoffDedupCollapsesPair(t *testing.T) {
	var sc hookOutcomeSidecar
	if err := yaml.Unmarshal(loadCompanionFixture(t, "orchestrator-handoff-remediate.yaml"), &sc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	scored := filterScoredHookOutcomes(sc.Records)
	if len(scored) != 2 {
		t.Fatalf("scored len = %d, want 2 (both prevent_before_action and remediate_at_stop are in-scope)", len(scored))
	}
	collapsed := dedupHookOutcomesByCorrelation(scored)
	if len(collapsed) != 1 {
		t.Fatalf("collapsed len = %d, want 1 (D4 dedup keys on (correlation_id, rule_id))", len(collapsed))
	}
	if collapsed[0].Result != hookResultRemediate {
		t.Errorf("collapsed result = %q, want remediate (more severe wins)", collapsed[0].Result)
	}
}

// TestCompanionGate_DelegationCloseoutMultiRuleRemediate asserts that
// delegation-closeout-gate emitting two distinct rule_ids under the same
// sentinel (R4.1 + R4.2) keeps them as two records — different rule_ids
// mean different intents and both must count. The band still resolves to
// remediate (0.0) and the Detail names both rules in sorted order so the
// readback (R5 follow-up) is deterministic.
func TestCompanionGate_DelegationCloseoutMultiRuleRemediate(t *testing.T) {
	data := loadCompanionFixture(t, "delegation-closeout-remediate.yaml")

	var sc hookOutcomeSidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	collapsed := dedupHookOutcomesByCorrelation(filterScoredHookOutcomes(sc.Records))
	if len(collapsed) != 2 {
		t.Fatalf("collapsed len = %d, want 2 (different rule_ids are distinct intents even under one sentinel)", len(collapsed))
	}

	got := scoreHookOutcomeBytes(data, 12)
	if !got.Present || got.SubScore != hookSubScoreAllRemediate {
		t.Errorf("got %+v, want present 0.0 (two remediating rules from delegation-closeout-gate)", got)
	}
	if !strings.Contains(got.Detail, "delegation-closeout-gate.R4.1") || !strings.Contains(got.Detail, "delegation-closeout-gate.R4.2") {
		t.Errorf("Detail = %q, want both R4.1 and R4.2 surfaced", got.Detail)
	}
	// Sorted-unique invariant: R4.1 must precede R4.2 lexicographically.
	if idx1, idx2 := strings.Index(got.Detail, "R4.1"), strings.Index(got.Detail, "R4.2"); idx1 < 0 || idx2 < 0 || idx1 > idx2 {
		t.Errorf("Detail rule ordering = %q, want R4.1 before R4.2 (joinSortedUnique invariant)", got.Detail)
	}
}

// TestCompanionGate_MixedAdviseDoesNotDoubleCount asserts that an iteration
// where both companion gates fire advisory-only (orchestrator R3.4 +
// delegation R4.4) resolves to the advise band (0.6), not to remediate, and
// the two distinct correlation_ids do not collide despite identical
// lifecycle_point/intervention_class. This protects against a future
// refactor that keys dedup on the wrong tuple.
func TestCompanionGate_MixedAdviseDoesNotDoubleCount(t *testing.T) {
	data := loadCompanionFixture(t, "companion-gates-mixed-advise.yaml")
	got := scoreHookOutcomeBytes(data, 13)
	if !got.Present {
		t.Fatalf("Present = false, want true: %+v", got)
	}
	if got.SubScore != hookSubScoreAllAdvise {
		t.Errorf("SubScore = %g, want %g (no remediate, two advise records)", got.SubScore, hookSubScoreAllAdvise)
	}
	if !strings.Contains(got.Detail, "advise:") {
		t.Errorf("Detail = %q, want 'advise:' prefix", got.Detail)
	}
	if !strings.Contains(got.Detail, "orchestrator-handoff-gate.R3.4") || !strings.Contains(got.Detail, "delegation-closeout-gate.R4.4") {
		t.Errorf("Detail = %q, want both advising companion rules surfaced", got.Detail)
	}

	var sc hookOutcomeSidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	collapsed := dedupHookOutcomesByCorrelation(filterScoredHookOutcomes(sc.Records))
	if len(collapsed) != 2 {
		t.Errorf("collapsed len = %d, want 2 (distinct correlation_ids never collapse)", len(collapsed))
	}
}

// TestCompanionGate_FixtureRoundTripsThroughOnDiskExtractor asserts the full
// path: copy a companion fixture into the iteration-log sidecar location the
// extractor scans, then call ExtractHookOutcomesSignal exactly as the score
// pipeline does. This is the strongest contract: the companion-emitted bytes
// survive the schema-version field, the filename shape, and the rubric's
// presence accounting unchanged.
func TestCompanionGate_FixtureRoundTripsThroughOnDiskExtractor(t *testing.T) {
	dir := t.TempDir()
	body := loadCompanionFixture(t, "orchestrator-handoff-remediate.yaml")
	if err := os.WriteFile(filepath.Join(dir, hookOutcomeFileName(14)), body, 0o644); err != nil {
		t.Fatalf("seed sidecar: %v", err)
	}
	got := ExtractHookOutcomesSignal(dir, 14)
	if !got.Present || got.SubScore != hookSubScoreAllRemediate {
		t.Fatalf("got %+v, want present 0.0 from companion-gate fixture on disk", got)
	}

	// And the fixture-driven SignalValue rolls into the rubric breakdown
	// without any schema-aware code in scoring noticing the companion origin.
	r := DefaultRubric()
	res := r.Score(SignalSet{Iteration: 14, HookOutcomes: got})
	if !res.Scored {
		t.Fatalf("Scored = false, want true: %+v", res)
	}
	var found bool
	for _, row := range res.Breakdown {
		if row.Signal == SignalHookOutcomes {
			found = true
			if !row.Present {
				t.Errorf("hook_outcomes row Present = false, want true for companion fixture")
			}
		}
	}
	if !found {
		t.Errorf("breakdown is missing the hook_outcomes row for the companion fixture")
	}
}

// TestCompanionGate_NegativeDeferredClassesStillNotScored is the negative
// guard: if either companion gate ever drifts and emits an
// intervention_class outside the t1b-approved set, the record must NOT
// contribute to the score. This is a defensive fixture proving the
// extractor's filter is fixture-driven, not just byte-literal-driven.
func TestCompanionGate_NegativeDeferredClassesStillNotScored(t *testing.T) {
	// Built inline (not a fixture) because the fixture set ships only
	// valid in-scope shapes; this case proves what would happen if the
	// companion gate drifted into post-tool observation territory.
	yamlBytes := []byte(`schema_version: 1
records:
  - schema_version: 1
    sentinel_id: orchestrator-session-start-2026-05-26-099
    skill: orchestrator-session-start
    lifecycle_point: post_tool_use
    intervention_class: observe_tool_result
    result: remediate
    rule_id: orchestrator-handoff-gate.R3.99
    platform: claude
    ts: "2026-05-26T13:00:00Z"
    correlation_id: drift-corr-099
`)
	got := scoreHookOutcomeBytes(yamlBytes, 15)
	if got.Present {
		t.Errorf("Present = true, want false — companion gate emitting observe_tool_result must not score: %+v", got)
	}
}
