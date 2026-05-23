package scoring

import (
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// IterlogSignals is the bundle of signal values an iteration's own
// iteration-log entry yields — the self-reported claims plus the objective
// verifier evidence reachable from the entry's recorded artifact paths.
//
// It is one input to the assemble slice, not a finished score. The
// SignalValue fields are sub-scores in [0,1] (or absent); Retries and
// UserCorrections are raw counts the assemble slice folds into the
// correction_pressure signal. This slice does no signal combination and no
// claimed-vs-observed integrity pairing — see docs/OUTCOME_SCORING_RUBRIC.md.
type IterlogSignals struct {
	// ScopeClaimed is the self-reported scope adherence, read from the
	// impl block's scope_note: on-target → 1.0, partial → 0.5,
	// scope-breach → 0.0. A free-text note whose lowercased form begins
	// "on-target" also maps to 1.0. Empty or unrecognized → absent.
	ScopeClaimed SignalValue

	// TestsClaimed is the self-reported test outcome: the fraction of the
	// *set* tri-state pass flags (impl.focused_tests_pass,
	// impl.tests_total_pass, and each verifiers[].tests_total_pass) whose
	// value is true. Absent when no such flag is set anywhere in the entry.
	TestsClaimed SignalValue

	// Verifier is the OBJECTIVE verifier result. Over the entry's verifier
	// records whose status is pass/fail/partial (unknown excluded) it is the
	// mean of pass→1.0, partial→0.5, fail→0.0; a verifier's recorded
	// result_artifact, when readable under repoRoot, overrides its inline
	// status. For v1 entries (no verifiers array) it falls back to
	// impl.tests_total_pass as a proxy. Absent when no verifier evidence
	// exists.
	Verifier SignalValue

	// VerifierClaimed is the self-reported verification diligence — at v2.0.0
	// it is read from linked_traces_to_outcomes alone, after the
	// boolean-effectiveness analysis showed ran_cli_command and
	// committed_after_tests were rubber-stamped (~98% true, no information).
	// The objective replacements live in IterationObjectives; this field will
	// be retired once structured-claims expands linked_traces_to_outcomes into
	// a named-trace list.
	VerifierClaimed SignalValue

	// LandedClaimed is the self-reported landing outcome: the
	// self_assessment.persisted_via_workflow_commands note combined with
	// review.overall_decision (accept→1.0, reject→0.0, escalate→0.5).
	// Absent when neither source says anything.
	LandedClaimed SignalValue

	// Retries is the raw retry count: impl.retries plus the sum of every
	// verifiers[].retries. A raw input to correction_pressure.
	Retries int

	// UserCorrections is the raw count of post-invocation corrections read
	// from the review-decision.yaml artifact (review.decision_artifact
	// resolved under repoRoot): the length of post_invocation.user_corrections
	// plus post_invocation.retries_in_loop. 0 when the artifact is
	// unavailable. A raw input to correction_pressure.
	UserCorrections int
}

// ExtractIterlogSignals derives the iteration-log-native signal values for one
// iteration record. repoRoot resolves the repo-relative verification-artifact
// paths the record carries (verifier result_artifact, review decision_artifact);
// when repoRoot is "" or an artifact is missing or unreadable the affected
// signal degrades gracefully — it becomes absent, or contributes 0. This
// function never panics and never returns an error.
func ExtractIterlogSignals(rec IterationRecord, repoRoot string) IterlogSignals {
	return IterlogSignals{
		ScopeClaimed:    extractScopeClaimed(rec),
		TestsClaimed:    extractTestsClaimed(rec),
		Verifier:        extractVerifier(rec, repoRoot),
		VerifierClaimed: extractVerifierClaimed(rec),
		LandedClaimed:   extractLandedClaimed(rec),
		Retries:         extractRetries(rec),
		UserCorrections: extractUserCorrections(rec, repoRoot),
	}
}

// extractScopeClaimed maps the impl block's scope_note to a self-reported
// scope sub-score.
func extractScopeClaimed(rec IterationRecord) SignalValue {
	note := strings.TrimSpace(rec.Impl.ScopeNote)
	if note == "" {
		return AbsentSignal("no scope_note in iteration log")
	}
	lower := strings.ToLower(note)
	switch lower {
	case "on-target":
		return PresentSignal(1.0, "scope_note: on-target")
	case "partial":
		return PresentSignal(0.5, "scope_note: partial")
	case "scope-breach":
		return PresentSignal(0.0, "scope_note: scope-breach")
	}
	// Historical entries predate the schema enum and carry free-text notes;
	// a leading "on-target" prefix normalizes, anything else is unusable.
	if strings.HasPrefix(lower, "on-target") {
		return PresentSignal(1.0, "scope_note: free-text with on-target prefix")
	}
	return AbsentSignal("unrecognized free-text scope_note")
}

// extractTestsClaimed computes the fraction of set tri-state test-pass flags
// that are true, across the impl block and every verifier.
func extractTestsClaimed(rec IterationRecord) SignalValue {
	flags := []OptionalBool{rec.Impl.FocusedTestsPass, rec.Impl.TestsTotalPass}
	for _, v := range rec.Verifiers {
		flags = append(flags, v.TestsTotalPass)
	}

	var set, pass int
	for _, f := range flags {
		if !f.Set {
			continue
		}
		set++
		if f.Value {
			pass++
		}
	}
	if set == 0 {
		return AbsentSignal("no test-pass flags set in iteration log")
	}
	return PresentSignal(float64(pass)/float64(set),
		formatFraction("test-pass flags true", pass, set))
}

// extractVerifier computes the objective verifier sub-score from the entry's
// verifier records — preferring a readable result_artifact's status over the
// inline status — and falls back to the v1 tests_total_pass proxy.
func extractVerifier(rec IterationRecord, repoRoot string) SignalValue {
	if len(rec.Verifiers) == 0 {
		// v1 entries have no verifiers array; tests_total_pass is the proxy.
		if rec.Impl.TestsTotalPass.Set {
			if rec.Impl.TestsTotalPass.Value {
				return PresentSignal(1.0, "no verifiers; tests_total_pass proxy: pass")
			}
			return PresentSignal(0.0, "no verifiers; tests_total_pass proxy: fail")
		}
		return AbsentSignal("no verifier records and no tests_total_pass proxy")
	}

	var sum float64
	var counted int
	for _, v := range rec.Verifiers {
		status := v.Status
		if artifactStatus, ok := readArtifactStatus(repoRoot, v.ResultArtifact); ok {
			status = artifactStatus
		}
		score, scored := verifierStatusScore(status)
		if !scored {
			continue // status unknown (or unrecognized) — excluded.
		}
		sum += score
		counted++
	}
	if counted == 0 {
		return AbsentSignal("verifier records carry no pass/fail/partial status")
	}
	return PresentSignal(sum/float64(counted),
		formatFraction("verifiers scored, mean status", counted, len(rec.Verifiers)))
}

// verifierStatusScore maps a verifier status string to a sub-score. The bool
// is false for "unknown" or any unrecognized value, which are excluded from
// the mean.
func verifierStatusScore(status string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass":
		return 1.0, true
	case "partial":
		return 0.5, true
	case "fail":
		return 0.0, true
	default:
		return 0, false
	}
}

// extractVerifierClaimed reads the verification-diligence claim from the
// remaining self-reported sources: the structured verifier.linked_traces
// list (preferred, populated in current entries) and the legacy
// linked_traces_to_outcomes boolean (kept for backward compat with the 66
// salvaged entries). The boolean is derivable from the list — a non-empty
// list is positive evidence; the boolean stands in for entries that predate
// the structured field.
func extractVerifierClaimed(rec IterationRecord) SignalValue {
	sa := rec.Impl.SelfAssessment
	// v2 carries the legacy flag on the verifier-block self_assessment too;
	// either side, plus any verifier's structured LinkedTraces list, counts.
	linked := sa.LinkedTracesToOutcomes
	for _, v := range rec.Verifiers {
		if v.SelfAssessment.LinkedTracesToOutcomes || len(v.LinkedTraces) > 0 {
			linked = true
		}
	}
	if !linked && isZeroSelfAssessment(sa) && !anyVerifierStructuredClaims(rec) {
		return AbsentSignal("no verification self-report")
	}
	if linked {
		return PresentSignal(1.0, "linked traces recorded")
	}
	return PresentSignal(0.0, "no linked traces recorded")
}

// anyVerifierStructuredClaims reports whether any verifier carries the new
// structured-claim lists — used so the signal stays present when an entry has
// e.g. tests_added_by_kind but no self_assessment block.
func anyVerifierStructuredClaims(rec IterationRecord) bool {
	for _, v := range rec.Verifiers {
		if len(v.TestsAddedByKind) > 0 || len(v.LinkedTraces) > 0 {
			return true
		}
	}
	return false
}

// extractLandedClaimed combines the self-reported persistence note with the
// review's overall decision into a self-reported landing sub-score.
func extractLandedClaimed(rec IterationRecord) SignalValue {
	persisted := strings.ToLower(strings.TrimSpace(
		rec.Impl.SelfAssessment.PersistedViaWorkflowCommands))
	decision := strings.ToLower(strings.TrimSpace(rec.Review.OverallDecision))

	var scores []float64
	var details []string

	if persisted != "" {
		if strings.Contains(persisted, "yes") {
			scores = append(scores, 1.0)
			details = append(details, "persisted_via_workflow_commands: yes")
		} else {
			scores = append(scores, 0.0)
			details = append(details, "persisted_via_workflow_commands: no")
		}
	}

	switch decision {
	case "accept":
		scores = append(scores, 1.0)
		details = append(details, "review: accept")
	case "reject":
		scores = append(scores, 0.0)
		details = append(details, "review: reject")
	case "escalate":
		scores = append(scores, 0.5)
		details = append(details, "review: escalate")
	}

	if len(scores) == 0 {
		return AbsentSignal("no persisted-via-workflow note and no review decision")
	}
	var sum float64
	for _, s := range scores {
		sum += s
	}
	return PresentSignal(sum/float64(len(scores)), strings.Join(details, "; "))
}

// extractRetries sums the impl retries with every verifier's retries.
func extractRetries(rec IterationRecord) int {
	total := rec.Impl.Retries
	for _, v := range rec.Verifiers {
		total += v.Retries
	}
	return total
}

// rawReviewDecision is the subset of review-decision.yaml this slice reads:
// the post-invocation correction counts.
type rawReviewDecision struct {
	PostInvocation struct {
		UserCorrections []string `yaml:"user_corrections"`
		RetriesInLoop   int      `yaml:"retries_in_loop"`
	} `yaml:"post_invocation"`
}

// extractUserCorrections reads the post-invocation correction counts from the
// review's decision artifact. It returns 0 whenever the artifact is missing,
// unreadable, or unparseable — the signal degrades to "no correction pressure
// observed" rather than failing.
func extractUserCorrections(rec IterationRecord, repoRoot string) int {
	data, ok := readArtifact(repoRoot, rec.Review.DecisionArtifact)
	if !ok {
		return 0
	}
	var decision rawReviewDecision
	if err := yaml.Unmarshal(data, &decision); err != nil {
		return 0
	}
	return len(decision.PostInvocation.UserCorrections) + decision.PostInvocation.RetriesInLoop
}

// readArtifactStatus reads a verifier result artifact and returns its recorded
// status. The bool is false when the path is empty, unresolvable, unreadable,
// or the file carries no status field.
func readArtifactStatus(repoRoot, relPath string) (string, bool) {
	data, ok := readArtifact(repoRoot, relPath)
	if !ok {
		return "", false
	}
	var result struct {
		Status string `yaml:"status"`
	}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return "", false
	}
	if strings.TrimSpace(result.Status) == "" {
		return "", false
	}
	return result.Status, true
}

// readArtifact resolves a repo-relative artifact path under repoRoot and reads
// it. The bool is false when repoRoot or relPath is empty, the resolved path
// escapes repoRoot, or the file is missing or unreadable — every degradation
// the spec requires be handled silently.
func readArtifact(repoRoot, relPath string) ([]byte, bool) {
	relPath = strings.TrimSpace(relPath)
	if repoRoot == "" || relPath == "" {
		return nil, false
	}
	if filepath.IsAbs(relPath) {
		// Artifact paths in the iteration log are repo-relative by contract;
		// an absolute path is not a path under repoRoot to trust.
		return nil, false
	}
	full := filepath.Join(repoRoot, filepath.Clean(relPath))
	cleanRoot := filepath.Clean(repoRoot)
	if full != cleanRoot && !strings.HasPrefix(full, cleanRoot+string(os.PathSeparator)) {
		// Defends against "../" escaping the repo root.
		return nil, false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, false
	}
	return data, true
}

// isZeroSelfAssessment reports whether a SelfAssessment carries no
// agent-reported information at all.
func isZeroSelfAssessment(sa SelfAssessment) bool {
	return sa == SelfAssessment{}
}

// formatFraction renders a "n/d label" detail string.
func formatFraction(label string, n, d int) string {
	return itoa(n) + "/" + itoa(d) + " " + label
}

// itoa is a tiny base-10 int formatter, kept local to avoid an strconv import
// for two call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
