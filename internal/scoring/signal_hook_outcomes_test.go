package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
