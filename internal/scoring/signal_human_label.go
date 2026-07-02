// signal_human_label.go is the R5 human-label extractor: it reads the
// per-iteration `iter-N.labels.yaml` sidecar written by internal/review/labels
// and folds the labels into a single `human_label` SignalValue feeding the
// rubric. Added at RubricVersion 3.0.0 per R5 spec D5.2
// (.agents/workflow/specs/r5-review-labeling-access/design.md).
//
// Sub-score per label (spec D5.7): the mean of three normalized structured
// dimensions — correctness (0–3 → 0..1), scope_judgement (on-target 1.0,
// partial 0.5, breach 0.0), and hallucination (none 1.0, minor 0.5,
// major 0.0). The free text never affects the score.
//
// Aggregation (spec D5.8 + OQ2 recommendation):
//   - latest-per-reviewer: when one actor holds several labels, only the most
//     recently updated one counts; within a label, the effective judgement is
//     the latest edit (append-on-edit history).
//   - admin override: labels explicitly marked admin_override supersede the
//     reviewer pool entirely. Admin *edits* to a reviewer's label stay
//     attributed to the reviewer (the label keeps its original actor) and
//     therefore stay in the reviewer pool — the audit log captures who edited.
//   - the aggregate is the mean over the surviving pool.
//
// Schema versioning (spec OQ3): every label carries label_schema_version;
// this extractor scores major version 1 only. A sidecar or label at a
// different major degrades to absent rather than silently misreading fields.
//
// Absent-safety (spec D5.2 / R4-R5): an iteration with no sidecar, no labels,
// an unreadable sidecar, or an unsupported schema yields an absent signal.
// The renormalizing combination drops absent signals from the vote, so
// existing iterations without labels see no human_label contribution.
package scoring

import (
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

// humanLabelSchemaMajor is the label_schema_version major this extractor
// understands (labels.LabelSchemaVersion is 1.x.y). A different major means
// the structured dimensions may have changed shape, so the signal degrades to
// absent instead of guessing.
const humanLabelSchemaMajor = "1"

// humanLabelDimensionCount is the number of structured dimensions averaged
// into one label's sub-score (correctness, scope_judgement, hallucination).
const humanLabelDimensionCount = 3.0

// Normalized values for the enum-bounded label dimensions: the good end of
// each enum scores full, the middle half, the bad end zero — mirroring the
// scope_note mapping the rubric already uses (on-target 1.0 / partial 0.5 /
// breach 0.0).
const (
	humanLabelDimFull = 1.0
	humanLabelDimHalf = 0.5
	humanLabelDimZero = 0.0
)

// ExtractHumanLabelSignals computes the `human_label` sub-score for one
// iteration. iterLogDir is the same directory BuildSignalSets walks; n is the
// iteration number (matching the iter-N.yaml entry).
//
// Returns an absent SignalValue when no labels exist for the iteration, the
// sidecar cannot be read or fails validation, or the label schema major is
// unsupported. A missing sidecar is not an error — it is the universal case
// for iterations recorded before R5 shipped and for unreviewed iterations.
func ExtractHumanLabelSignals(iterLogDir string, n int) SignalValue {
	if strings.TrimSpace(iterLogDir) == "" {
		return AbsentSignal("no iteration-log directory for human-label signal")
	}
	sc, err := labels.ReadSidecar(iterLogDir, n)
	if err != nil {
		return AbsentSignal(fmt.Sprintf("cannot read labels sidecar for iter-%d: %v", n, err))
	}
	if len(sc.Labels) == 0 {
		return AbsentSignal(fmt.Sprintf("no human labels for iter-%d", n))
	}
	if bad, ok := labelSchemaSupported(sc); !ok {
		return AbsentSignal(fmt.Sprintf("unsupported label schema %q for iter-%d (this rubric scores major %s only)", bad, n, humanLabelSchemaMajor))
	}
	return foldHumanLabelSubScore(sc.Labels)
}

// labelSchemaSupported checks the sidecar and every label against the schema
// major this extractor understands (OQ3). It returns the first offending
// version string and false, or "" and true when everything is scorable.
func labelSchemaSupported(sc labels.Sidecar) (string, bool) {
	if schemaMajor(sc.SchemaVersion) != humanLabelSchemaMajor {
		return sc.SchemaVersion, false
	}
	for _, l := range sc.Labels {
		if schemaMajor(l.SchemaVersion) != humanLabelSchemaMajor {
			return l.SchemaVersion, false
		}
	}
	return "", true
}

// schemaMajor returns the major component of a semantic version string ("1"
// from "1.0.0"). A version with no dot is treated as its own major.
func schemaMajor(v string) string {
	if i := strings.IndexByte(v, '.'); i >= 0 {
		return v[:i]
	}
	return v
}

// foldHumanLabelSubScore aggregates a non-empty label set into the signal
// value: admin-override precedence, then latest-per-actor, then the mean of
// the per-label sub-scores. The Detail names the actors (sorted, so the
// explainable breakdown is reproducible across re-scores).
func foldHumanLabelSubScore(all []labels.Label) SignalValue {
	pool, override := humanLabelScoringPool(all)
	latest := latestLabelPerActor(pool)

	var sum float64
	actors := make([]string, 0, len(latest))
	for actor, l := range latest {
		sum += humanLabelSubScore(l.EffectiveStructured())
		actors = append(actors, actor)
	}
	mean := sum / float64(len(latest))
	who := joinSortedUnique(actors)
	if override {
		return PresentSignal(mean, fmt.Sprintf("admin override by %s", who))
	}
	return PresentSignal(mean, fmt.Sprintf("mean of %d latest label(s) by %s", len(latest), who))
}

// humanLabelScoringPool applies the OQ2 precedence rule: labels explicitly
// marked as an admin override supersede the reviewer pool entirely. When no
// override exists the whole label set is the pool. The second return reports
// whether the override branch was taken (for the Detail string).
func humanLabelScoringPool(all []labels.Label) ([]labels.Label, bool) {
	var overrides []labels.Label
	for _, l := range all {
		if l.AdminOverride {
			overrides = append(overrides, l)
		}
	}
	if len(overrides) > 0 {
		return overrides, true
	}
	return all, false
}

// latestLabelPerActor keeps the most recently updated label per original
// author, implementing the latest-per-reviewer half of spec D5.8. A
// same-timestamp tie keeps the later sidecar entry so the fold is
// deterministic for any input ordering the persist layer produces.
func latestLabelPerActor(pool []labels.Label) map[string]labels.Label {
	latest := make(map[string]labels.Label, len(pool))
	for _, l := range pool {
		prev, ok := latest[l.Actor]
		if !ok || !prev.UpdatedAt.After(l.UpdatedAt) {
			latest[l.Actor] = l
		}
	}
	return latest
}

// humanLabelSubScore maps one structured judgement to [0, 1]: the mean of the
// normalized correctness, scope_judgement, and hallucination dimensions
// (spec D5.7). Free text is deliberately not an input.
func humanLabelSubScore(s labels.Structured) float64 {
	c := float64(s.Correctness) / float64(labels.CorrectnessMax)
	return (c + scopeJudgementSubScore(s.ScopeJudgement) + hallucinationSubScore(s.Hallucination)) / humanLabelDimensionCount
}

// scopeJudgementSubScore normalizes the scope_judgement enum. Unknown values
// cannot reach here — labels.ReadSidecar validates the enum on load — so the
// default arm carries the bad end (breach).
func scopeJudgementSubScore(v labels.ScopeJudgement) float64 {
	switch v {
	case labels.ScopeOnTarget:
		return humanLabelDimFull
	case labels.ScopePartial:
		return humanLabelDimHalf
	default:
		return humanLabelDimZero
	}
}

// hallucinationSubScore normalizes the hallucination enum. Unknown values
// cannot reach here — labels.ReadSidecar validates the enum on load — so the
// default arm carries the bad end (major).
func hallucinationSubScore(v labels.Hallucination) float64 {
	switch v {
	case labels.HallucinationNone:
		return humanLabelDimFull
	case labels.HallucinationMinor:
		return humanLabelDimHalf
	default:
		return humanLabelDimZero
	}
}
