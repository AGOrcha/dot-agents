// signal_hook_outcomes.go is the R1.5 hook-outcome objective extractor: it
// reads the per-iteration `.agents/active/iteration-log/iter-N.hook-outcomes.yaml`
// sidecar written by `da workflow hook-outcome write`
// (commands/workflow/hook_outcome.go, R1.5 t1) and folds the records into a
// single `hook_outcomes` SignalValue feeding the rubric.
//
// Scope per R1.5 t1b (PR #97) and R1.5 design D3/D4:
//   - Only `intervention_class` in {`prevent_before_action`,
//     `remediate_at_stop`} contributes to scoring.
//   - `continuity_advice` (pre_compact) and `observe_tool_result` (post-tool)
//     are deferred to R1.5.1 and never reach the sub-score under RubricVersion
//     2.1.0 — they remain in the sidecar as audit-only observations.
//   - A pre-action and a terminal-remediation record sharing the same
//     `correlation_id` + `rule_id` collapse to one record (the more severe,
//     remediate) per D4 dedup, so a prevented-then-stop record does not
//     double-count.
//
// Sub-score (per design D3):
//   - At least one collapsed record at `remediate` ⇒ 0.0.
//   - All collapsed records at `advise`, no `remediate`        ⇒ 0.6.
//   - All collapsed records at `allow`, no `advise`/`remediate` ⇒ 1.0.
//   - No in-scope records (or sidecar missing/unreadable)       ⇒ absent
//     (preserves the "absent does not vote" invariant from scorer.go).
//
// The extractor is read-only and pure-ish: it does file I/O bounded to the
// sidecar path, never mutates it, and never escalates a parse failure to an
// error — every recoverable failure mode degrades to AbsentSignal so a
// malformed sidecar cannot break the rest of the score.
package scoring

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Hook-outcome intervention-class enum values the scorer recognises. Mirrors
// the schema enum in commands/workflow/static/workflow-hook-outcome.schema.json
// but only enumerates the two classes this signal scores; the deferred ones
// are intentionally absent so any addition is a deliberate code edit.
const (
	hookInterventionPreventBeforeAction = "prevent_before_action"
	hookInterventionRemediateAtStop     = "remediate_at_stop"
)

// Hook-outcome result enum values. Mirrors the schema enum; the sub-score
// folding fixes their relative ordering (remediate dominates advise dominates
// allow) in one place.
const (
	hookResultAllow     = "allow"
	hookResultAdvise    = "advise"
	hookResultRemediate = "remediate"
)

// Hook-outcomes sub-score bands (per R1.5 design D3). Named so the extractor
// reports the exact value the doc promises rather than re-deriving it from a
// formula the doc does not state.
const (
	hookSubScoreAllRemediate = 0.0
	hookSubScoreAllAdvise    = 0.6
	hookSubScoreAllAllow     = 1.0
)

// hookOutcomeFileName is the canonical sidecar filename for iteration N. Kept
// next to the extractor so a path-shape change in the writer
// (commands/workflow/hook_outcome.go) and the reader stay together.
func hookOutcomeFileName(n int) string {
	return fmt.Sprintf("iter-%d.hook-outcomes.yaml", n)
}

// hookOutcomeRecord is the minimal projection of one record the scorer needs.
// The writer's HookOutcomeRecord lives in package workflow and carries
// audit-only fields (ts, archived_sentinel_path, etc.); duplicating the four
// scored fields here keeps internal/scoring independent of the commands tree.
type hookOutcomeRecord struct {
	InterventionClass string `yaml:"intervention_class"`
	Result            string `yaml:"result"`
	RuleID            string `yaml:"rule_id"`
	CorrelationID     string `yaml:"correlation_id"`
}

// hookOutcomeSidecar is the file shape: the scorer ignores schema_version
// because a future bump would change field names, not the four it reads (a
// breaking change there warrants a separate extractor revision, not a silent
// promotion).
type hookOutcomeSidecar struct {
	Records []hookOutcomeRecord `yaml:"records"`
}

// ExtractHookOutcomesSignal computes the `hook_outcomes` sub-score for one
// iteration. iterLogDir is the same directory BuildSignalSets walks; n is the
// iteration number (matching the iter-N.yaml entry).
//
// Returns an absent SignalValue when the sidecar does not exist, cannot be
// parsed, or contains no scored records. A non-existent sidecar is not an
// error — it is the common case for iterations recorded before R1.5 shipped
// and for iterations whose sentinel was inactive throughout.
func ExtractHookOutcomesSignal(iterLogDir string, n int) SignalValue {
	if strings.TrimSpace(iterLogDir) == "" {
		return AbsentSignal("no iteration-log directory for hook-outcomes signal")
	}
	path := filepath.Join(iterLogDir, hookOutcomeFileName(n))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AbsentSignal(fmt.Sprintf("no hook-outcomes sidecar for iter-%d", n))
		}
		return AbsentSignal(fmt.Sprintf("cannot read hook-outcomes sidecar for iter-%d: %v", n, err))
	}
	return scoreHookOutcomeBytes(data, n)
}

// scoreHookOutcomeBytes parses sidecar YAML and folds it to a SignalValue. It
// is split from ExtractHookOutcomesSignal so tests can drive it without
// staging a temp directory of YAML files.
func scoreHookOutcomeBytes(data []byte, n int) SignalValue {
	var sc hookOutcomeSidecar
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return AbsentSignal(fmt.Sprintf("cannot parse hook-outcomes sidecar for iter-%d: %v", n, err))
	}
	scored := filterScoredHookOutcomes(sc.Records)
	if len(scored) == 0 {
		return AbsentSignal(fmt.Sprintf("no scored hook-outcome records for iter-%d (continuity_advice/observe_tool_result are deferred)", n))
	}
	collapsed := dedupHookOutcomesByCorrelation(scored)
	return foldHookOutcomeSubScore(collapsed)
}

// filterScoredHookOutcomes drops records that R1.5 v1 does not score:
// continuity_advice (deferred to R1.5.1 per t1b PR #97) and
// observe_tool_result (deferred to R1.5.1 per t1b PR #97). Any unrecognised
// intervention_class is also dropped — a future schema addition must opt into
// scoring through a code change, not by being silently included here.
func filterScoredHookOutcomes(in []hookOutcomeRecord) []hookOutcomeRecord {
	out := make([]hookOutcomeRecord, 0, len(in))
	for _, r := range in {
		switch r.InterventionClass {
		case hookInterventionPreventBeforeAction, hookInterventionRemediateAtStop:
			out = append(out, r)
		}
	}
	return out
}

// dedupHookOutcomesByCorrelation collapses prevent_before_action +
// remediate_at_stop pairs that share (correlation_id, rule_id) into a single
// record at the more severe result (remediate > advise > allow), per R1.5
// design D4. Records with empty correlation_id never collapse — they are
// counted independently.
//
// Output ordering is stable: collapsed pairs surface at the position of their
// first occurrence so downstream readback (R5 follow-up) sees a deterministic
// rule-id sequence.
func dedupHookOutcomesByCorrelation(in []hookOutcomeRecord) []hookOutcomeRecord {
	type key struct{ corr, rule string }
	indexByKey := make(map[key]int, len(in))
	out := make([]hookOutcomeRecord, 0, len(in))
	for _, r := range in {
		if r.CorrelationID == "" {
			out = append(out, r)
			continue
		}
		k := key{corr: r.CorrelationID, rule: r.RuleID}
		if idx, ok := indexByKey[k]; ok {
			if hookResultSeverity(r.Result) > hookResultSeverity(out[idx].Result) {
				out[idx].Result = r.Result
			}
			continue
		}
		indexByKey[k] = len(out)
		out = append(out, r)
	}
	return out
}

// hookResultSeverity orders the three result values so the dedup pass can
// compare them. Higher = more severe (more pressure on the score).
func hookResultSeverity(result string) int {
	switch result {
	case hookResultRemediate:
		return 2
	case hookResultAdvise:
		return 1
	case hookResultAllow:
		return 0
	default:
		// Unknown result is dropped from the comparison — neither side wins,
		// so the existing record stays. The schema validator at write time
		// guarantees this branch is unreachable in practice.
		return -1
	}
}

// foldHookOutcomeSubScore maps the collapsed record set to the rubric's
// three-band sub-score and assembles a human-readable detail string that
// names which rules drove the band.
func foldHookOutcomeSubScore(collapsed []hookOutcomeRecord) SignalValue {
	var remediate, advise, allow []string
	for _, r := range collapsed {
		switch r.Result {
		case hookResultRemediate:
			remediate = append(remediate, r.RuleID)
		case hookResultAdvise:
			advise = append(advise, r.RuleID)
		case hookResultAllow:
			allow = append(allow, r.RuleID)
		}
	}
	if len(remediate) > 0 {
		return PresentSignal(hookSubScoreAllRemediate, fmt.Sprintf("remediate: %s", joinSortedUnique(remediate)))
	}
	if len(advise) > 0 {
		return PresentSignal(hookSubScoreAllAdvise, fmt.Sprintf("advise: %s", joinSortedUnique(advise)))
	}
	if len(allow) > 0 {
		return PresentSignal(hookSubScoreAllAllow, fmt.Sprintf("allow: %s (%d records)", joinSortedUnique(allow), len(allow)))
	}
	// Unreachable in practice: filterScoredHookOutcomes guarantees at least
	// one in-scope record reaches this point, and the schema validator at
	// write time guarantees each in-scope record has a known result. Return
	// absent rather than a fabricated sub-score so a future schema addition
	// that slipped through the filter does not vote silently.
	return AbsentSignal("hook-outcome records had no recognised result value")
}

// joinSortedUnique renders a deterministic, comma-separated list of rule IDs
// for the Detail string. The score itself is rule-set-invariant so it does
// not need ordering, but a stable Detail keeps the explainable breakdown
// reproducible across re-scores.
func joinSortedUnique(ids []string) string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
