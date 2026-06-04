package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// `da config relevance --recompute` is the explicit driver event from the design
// (.agents/proposals/skill-relevance-filter.md §5/§6): it reads the scored
// iteration corpus (iter-N.yaml + iter-N.score.yaml) and, per skill/agent/lens
// the resolved execution_profile already lists, computes a contribution signal
// — cited-in-passing vs cited-in-low-scoring vs never-cited — then emits a
// proposed class (core | situational | noise) plus a gaps list of corpus-cited
// units the profile does not yet classify.
//
// It is a flag on `da config relevance` (not a subcommand) so the public surface
// matches the design contract `da config relevance --recompute [--write]`. It is
// explicit-only and has no clock: nothing recomputes on a timer, and even
// --write only emits a PROPOSED profile-layer diff for a human to accept — it
// never auto-applies (config-v2 D4/D5: content-hash, offline, explicit-only).

// lowScoreThreshold is the iteration-score boundary (on the persisted [0,1]
// Value) below which a citation counts as cited-in-low-scoring rather than
// cited-in-passing. It is a self-contained cutoff so the signal does not couple
// to the rubric's band-ladder names, which can drift independently.
const lowScoreThreshold = 0.5

// minCitationsForCore is the floor of passing citations a unit needs before it
// is proposed for promotion to core. One lucky mention is not a pattern; the
// design wants a "repeatedly-useful" unit, so a single citation only ever
// holds a unit at situational.
const minCitationsForCore = 2

// recomputeIterationLogDir is the corpus location relative to the project root,
// matching where `workflow checkpoint` and `da score` write iter-N.yaml /
// iter-N.score.yaml.
var recomputeIterationLogDirParts = []string{".agents", "active", "iteration-log"}

// recomputeResult is the stable JSON shape emitted by
// `da config relevance --recompute --json`. It reports the resolution context,
// the per-unit proposals, the gaps, and the corpus digest that anchors
// freshness (the D4 nudge surface).
type recomputeResult struct {
	// AppType is the app_type whose profile was recomputed.
	AppType string `json:"app_type"`
	// Stage is the stage the relevance classes were recomputed for, or empty
	// when every declared stage was recomputed.
	Stage string `json:"stage,omitempty"`
	// Write echoes whether --write was set (a proposed diff was emitted).
	Write bool `json:"write"`
	// IterationsScanned is the number of iteration records the corpus yielded.
	IterationsScanned int `json:"iterations_scanned"`
	// InputsDigest is the content hash of the scored corpus that produced this
	// recompute — it is what folds into the layer's inputs_digest so
	// `config explain` can report "last recomputed at digest X".
	InputsDigest string `json:"inputs_digest"`
	// Proposals is one entry per unit the profile already classifies, with its
	// current class, the recomputed proposed class, and the contribution signal.
	Proposals []unitProposal `json:"proposals"`
	// Gaps are units cited in the corpus that the profile does not classify yet
	// — candidates the operator may want to add. Never silently dropped.
	Gaps []string `json:"gaps,omitempty"`
	// ProposedLayer is the proposed execution_profile layer diff, present only
	// when --write was set. It is a proposal a human accepts; never auto-applied.
	ProposedLayer *cfg.ExecutionProfile `json:"proposed_layer,omitempty"`
}

// unitProposal is the recompute verdict for one classified unit.
type unitProposal struct {
	// Unit is the skill/agent/lens id.
	Unit string `json:"unit"`
	// Stage is the app_type stage the unit is classified under.
	Stage string `json:"stage"`
	// CurrentClass is the unit's class in the resolved profile today.
	CurrentClass string `json:"current_class"`
	// ProposedClass is the recomputed class from the corpus signal.
	ProposedClass string `json:"proposed_class"`
	// Signal is the human-readable contribution signal: cited-in-passing,
	// cited-in-low-scoring, or never-cited.
	Signal string `json:"signal"`
	// PassingCitations / LowScoringCitations count the scored iterations that
	// cited the unit in each band; surfaced so the proposal is auditable.
	PassingCitations    int `json:"passing_citations"`
	LowScoringCitations int `json:"low_scoring_citations"`
	// Changed reports whether ProposedClass differs from CurrentClass.
	Changed bool `json:"changed"`
}

// contribution signal labels — the three the design names in §6.
const (
	signalCitedInPassing    = "cited-in-passing"
	signalCitedInLowScoring = "cited-in-low-scoring"
	signalNeverCited        = "never-cited"
)

// unitSignal is the accumulated corpus evidence for one unit across the scored
// iterations: how many passing vs low-scoring iterations cited it. An
// unscored iteration that cites a unit does not vote (no score to attribute),
// matching the rubric's "absent does not vote" rule.
type unitSignal struct {
	passing    int
	lowScoring int
}

// total is the count of scored iterations that cited the unit at all.
func (s unitSignal) total() int { return s.passing + s.lowScoring }

// label maps the accumulated counts to one of the three §6 signal names.
func (s unitSignal) label() string {
	if s.total() == 0 {
		return signalNeverCited
	}
	if s.lowScoring > s.passing {
		return signalCitedInLowScoring
	}
	return signalCitedInPassing
}

// runRecompute is the explicit driver-event path reached when
// `da config relevance --recompute` is set. It shares the runRelevanceOptions
// flag struct with the inspector path — recompute reads --app-type/--stage and
// (with --write) emits a proposed layer diff — so the public surface is the
// single `da config relevance` command the design (§5) specifies.
func runRecompute(opts *runRelevanceOptions, deps Deps) error {
	opts.appType = strings.TrimSpace(opts.appType)
	opts.stage = strings.TrimSpace(opts.stage)
	if opts.appType == "" {
		return deps.UsageError(
			"--recompute requires --app-type",
			"Name the app_type whose profile to recompute, e.g. --recompute --app-type go-cli.",
		)
	}

	snap, _, err := loadFlatSnapshot(opts.cwd)
	if err != nil {
		return deps.ErrorWithHints(err.Error(),
			"Run `da install --generate` to create .agentsrc.json from current state.",
		)
	}

	profile, err := resolveExecutionProfile(snap)
	if err != nil {
		return deps.ErrorWithHints(err.Error(),
			"The execution_profile layer in .agentsrc.json is not shaped as expected; see .agents/proposals/skill-relevance-filter.md §2.",
		)
	}

	corpus, err := loadScoredCorpus(opts.cwd)
	if err != nil {
		return deps.ErrorWithHints(err.Error(),
			"The scored iteration corpus could not be read; run `da score run --write` to populate iter-N.score.yaml.",
		)
	}

	result := buildRecomputeResult(opts, profile, corpus)
	return renderRecompute(opts, result)
}

// scoredCorpus is the joined view of the iteration corpus: each iteration's
// record paired with its persisted score (when one exists). Iterations without
// a score sidecar are kept but do not vote on any unit's signal.
type scoredCorpus struct {
	// entries is iteration-ordered; one per iter-N.yaml.
	entries []corpusEntry
	// digest is the content hash of the scored corpus, anchoring freshness.
	digest string
}

// corpusEntry is one iteration: the citation haystack drawn from its record
// plus the score that attributes the citation to a passing or low band.
type corpusEntry struct {
	iteration int
	// haystack is the lower-cased, space-joined text a known unit id is searched
	// for: verifier types, impl summary/scope, review decision artifact, etc.
	// Free text is fine here because the search is for an exact, already-known
	// unit id — never for mining new candidate ids.
	haystack string
	// unitTokens are the lower-cased structured unit ids the iteration names
	// directly: verifier types and review failed-gates. Gap mining draws only
	// from these — never from free-text summaries, which are too noisy to mine
	// candidate ids out of (task ids, file names, and prose slugs would pollute
	// the gaps list).
	unitTokens []string
	// scored reports whether a score sidecar was found for the iteration.
	scored bool
	// value is the persisted score in [0,1]; meaningful only when scored.
	value float64
}

// loadScoredCorpus reads the iteration-log directory and joins each record with
// its score sidecar. A missing directory yields an empty corpus (a fresh repo
// with no traces is not an error — there is simply nothing to recompute from).
func loadScoredCorpus(projectPath string) (scoredCorpus, error) {
	dir := filepath.Join(append([]string{projectPath}, recomputeIterationLogDirParts...)...)
	if !dirExists(dir) {
		return scoredCorpus{}, nil
	}
	records, err := scoring.LoadIterationLog(dir)
	if err != nil {
		return scoredCorpus{}, fmt.Errorf("reading iteration corpus: %w", err)
	}

	entries := make([]corpusEntry, 0, len(records))
	hasher := sha256.New()
	for _, rec := range records {
		entry := corpusEntry{
			iteration:  rec.Iteration,
			haystack:   citationHaystack(rec),
			unitTokens: structuredUnitTokens(rec),
		}
		score, ok, serr := loadIterationScore(dir, rec.Iteration)
		if serr != nil {
			return scoredCorpus{}, serr
		}
		if ok && score.Scored {
			entry.scored = true
			entry.value = score.Value
		}
		entries = append(entries, entry)
		fmt.Fprintf(hasher, "%d|%t|%g|%s\n", entry.iteration, entry.scored, entry.value, entry.haystack)
	}

	return scoredCorpus{entries: entries, digest: hex.EncodeToString(hasher.Sum(nil))}, nil
}

// loadIterationScore reads iter-N.score.yaml for the iteration. A missing
// sidecar is reported as (zero, false, nil) — the iteration simply has no
// score to vote with; a malformed sidecar is a hard error.
func loadIterationScore(dir string, iteration int) (scoring.PersistedScore, bool, error) {
	path := scoring.IterationScorePath(dir, iteration)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return scoring.PersistedScore{}, false, nil
		}
		return scoring.PersistedScore{}, false, fmt.Errorf("reading score sidecar %s: %w", filepath.Base(path), err)
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil {
		return scoring.PersistedScore{}, false, fmt.Errorf("parsing score sidecar %s: %w", filepath.Base(path), err)
	}
	return ps, true, nil
}

// citationHaystack builds the lower-cased text blob a unit name is searched in
// for one iteration record. It draws from every free-text and structured field
// that names a skill/agent/lens/verifier: the impl summary and scope note, the
// verifier types and their test names, and the review decision artifact path
// (which embeds the task and, for lens reviews, the lens id).
func citationHaystack(rec scoring.IterationRecord) string {
	parts := []string{rec.Impl.Summary, rec.Impl.ScopeNote}
	for _, v := range rec.Verifiers {
		parts = append(parts, v.Type)
		for _, t := range v.TestsAddedByKind {
			parts = append(parts, t.Name)
		}
		parts = append(parts, v.ResultArtifact)
	}
	parts = append(parts, rec.Review.DecisionArtifact)
	parts = append(parts, rec.Review.FailedGates...)
	return strings.ToLower(strings.Join(parts, " "))
}

// structuredUnitTokens returns the lower-cased unit ids an iteration names
// directly through its structured fields — the verifier types and review
// failed-gates. These are the only fields where a value is, by schema, a
// unit/lens id rather than free-form prose, so they are the trustworthy source
// for mining gap candidates (units cited but not yet classified).
func structuredUnitTokens(rec scoring.IterationRecord) []string {
	var tokens []string
	for _, v := range rec.Verifiers {
		if t := strings.ToLower(strings.TrimSpace(v.Type)); t != "" {
			tokens = append(tokens, t)
		}
	}
	for _, g := range rec.Review.FailedGates {
		if t := strings.ToLower(strings.TrimSpace(g)); t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// buildRecomputeResult is the pure core: it classifies every unit the profile
// lists for the app_type (× stage) from the corpus signal, collects gaps, and
// — when --write — assembles the proposed layer. No I/O happens here, so it is
// fully table-drivable.
func buildRecomputeResult(opts *runRelevanceOptions, profile *cfg.ExecutionProfile, corpus scoredCorpus) recomputeResult {
	result := recomputeResult{
		AppType:           opts.appType,
		Stage:             opts.stage,
		Write:             opts.write,
		IterationsScanned: len(corpus.entries),
		InputsDigest:      corpus.digest,
	}

	prof := appTypeProfile(profile, opts.appType)
	stages := recomputeStages(prof, opts.stage)
	listed := map[string]bool{}

	for _, stage := range stages {
		classes := prof.Relevance[stage]
		for _, unit := range stageUnits(classes) {
			listed[unit] = true
			current := profile.ClassOf(opts.appType, stage, unit)
			sig := unitCitationSignal(unit, corpus)
			proposed := proposedClass(current, sig)
			result.Proposals = append(result.Proposals, unitProposal{
				Unit:                unit,
				Stage:               stage,
				CurrentClass:        current,
				ProposedClass:       proposed,
				Signal:              sig.label(),
				PassingCitations:    sig.passing,
				LowScoringCitations: sig.lowScoring,
				Changed:             proposed != current,
			})
		}
	}

	sort.SliceStable(result.Proposals, func(i, j int) bool {
		if result.Proposals[i].Stage != result.Proposals[j].Stage {
			return result.Proposals[i].Stage < result.Proposals[j].Stage
		}
		return result.Proposals[i].Unit < result.Proposals[j].Unit
	})

	result.Gaps = corpusGaps(corpus, listed)

	if opts.write {
		result.ProposedLayer = proposedLayer(opts.appType, result.Proposals, profile.EffectiveDefaultClass())
	}
	return result
}

// recomputeStages returns the stages to recompute: just the requested one when
// --stage is set (even if the profile does not list it — an empty result is
// honest), otherwise every stage the app_type declares, sorted.
func recomputeStages(prof cfg.AppTypeProfile, stage string) []string {
	if stage != "" {
		return []string{stage}
	}
	stages := make([]string, 0, len(prof.Relevance))
	for s := range prof.Relevance {
		stages = append(stages, s)
	}
	sort.Strings(stages)
	return stages
}

// stageUnits returns every unit listed across a stage's classes, de-duplicated
// and sorted, so a unit (mis)listed in two classes is recomputed once.
func stageUnits(classes cfg.RelevanceClasses) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{classes.Core, classes.Situational, classes.Noise} {
		for _, u := range list {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	sort.Strings(out)
	return out
}

// unitCitationSignal accumulates the passing/low-scoring citation counts for a
// unit across the corpus. Only scored iterations vote; an iteration cites a
// unit when its name appears as a whole token in the iteration's haystack.
func unitCitationSignal(unit string, corpus scoredCorpus) unitSignal {
	needle := strings.ToLower(strings.TrimSpace(unit))
	var sig unitSignal
	if needle == "" {
		return sig
	}
	for _, e := range corpus.entries {
		if !e.scored || !citesUnit(e.haystack, needle) {
			continue
		}
		if e.value < lowScoreThreshold {
			sig.lowScoring++
			continue
		}
		sig.passing++
	}
	return sig
}

// citesUnit reports whether needle appears as a whole token in haystack. A
// substring match would let "review" match "review-pr"; bounding by non-word
// separators keeps a citation attributable to exactly one unit.
func citesUnit(haystack, needle string) bool {
	for {
		idx := strings.Index(haystack, needle)
		if idx < 0 {
			return false
		}
		if isTokenBoundary(haystack, idx, len(needle)) {
			return true
		}
		haystack = haystack[idx+1:]
	}
}

// isTokenBoundary reports whether the match of length n at idx is flanked by
// token boundaries (start/end of string or a non-identifier byte). Unit ids use
// letters, digits, and hyphens, so the boundary set is "anything else."
func isTokenBoundary(s string, idx, n int) bool {
	before := idx == 0 || !isUnitByte(s[idx-1])
	afterIdx := idx + n
	after := afterIdx >= len(s) || !isUnitByte(s[afterIdx])
	return before && after
}

// isUnitByte reports whether b can be part of a unit id token (letters, digits,
// hyphen, underscore). The hyphen is included so "review-pr" is one token, not
// "review" followed by "pr".
func isUnitByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_':
		return true
	default:
		return false
	}
}

// proposedClass maps the current class plus the corpus signal to a proposed
// class, conservatively:
//   - never-cited (NO evidence at all) -> KEEP the current class. Absence of
//     evidence is not evidence of irrelevance: an empty or zero-signal corpus
//     must never mass-demote a unit to noise. Only an active low-scoring signal
//     suppresses; silence holds the status quo.
//   - cited-in-low-scoring (active negative evidence) -> noise candidate (suppress)
//   - cited-in-passing with enough citations -> core (promote)
//   - cited-in-passing but thin evidence -> situational (hold)
//
// A unit already classed noise stays noise unless the corpus actively
// rehabilitates it (repeated passing citations), so an operator's explicit
// suppression is not casually overturned by a single new mention.
func proposedClass(current string, sig unitSignal) string {
	switch sig.label() {
	case signalNeverCited:
		// No corpus evidence for this unit. Hold the current class so a corpus
		// that says nothing about a unit (an empty/unscored corpus says nothing
		// about every unit) never demotes it. This is the silent-zero guard.
		return current
	case signalCitedInLowScoring:
		// Active negative evidence: the unit was cited but predominantly in
		// low-scoring iterations -> propose suppression.
		return "noise"
	default: // cited-in-passing
		if sig.passing >= minCitationsForCore {
			return "core"
		}
		if current == "noise" {
			// Thin rehabilitation evidence: lift out of noise to situational
			// rather than straight to core.
			return "situational"
		}
		return "situational"
	}
}

// corpusGaps returns units cited in the corpus that the profile does not
// classify (listed==false), so a genuinely-useful but unlisted unit is surfaced
// rather than silently ignored. Candidates are drawn only from the structured
// unit tokens (verifier types and review gates) — never from free-text
// summaries, which would pollute the list with task ids, file names, and prose
// slugs. Sorted and de-duplicated.
func corpusGaps(corpus scoredCorpus, listed map[string]bool) []string {
	seen := map[string]bool{}
	var gaps []string
	for _, e := range corpus.entries {
		for _, cand := range e.unitTokens {
			if cand == "" || listed[cand] || seen[cand] {
				continue
			}
			seen[cand] = true
			gaps = append(gaps, cand)
		}
	}
	sort.Strings(gaps)
	return gaps
}

// proposedLayer assembles the PROPOSED execution_profile layer from the
// recompute proposals: each unit is placed into its proposed class within its
// stage. The result is a real cfg.ExecutionProfile so it round-trips through the
// same layer schema the static profile uses — it is a diff a human accepts, not
// an applied change.
func proposedLayer(appType string, proposals []unitProposal, defaultClass string) *cfg.ExecutionProfile {
	relevance := map[string]cfg.RelevanceClasses{}
	for _, p := range proposals {
		classes := relevance[p.Stage]
		switch p.ProposedClass {
		case "core":
			classes.Core = append(classes.Core, p.Unit)
		case "noise":
			classes.Noise = append(classes.Noise, p.Unit)
		default:
			classes.Situational = append(classes.Situational, p.Unit)
		}
		relevance[p.Stage] = classes
	}
	return &cfg.ExecutionProfile{
		ByAppType: map[string]cfg.AppTypeProfile{
			appType: {Relevance: relevance},
		},
		DefaultClass: defaultClass,
	}
}

// renderRecompute emits the result as JSON (stable envelope) or as the
// human-readable proposal view.
func renderRecompute(opts *runRelevanceOptions, result recomputeResult) error {
	if opts.jsonOut {
		return writeJSON(opts.stdout, result)
	}
	return printRecomputeHuman(opts.stdout, result)
}

// printRecomputeHuman renders the resolution context, the per-unit proposals
// (flagging changes), the gaps list, and — when --write — the proposed layer.
func printRecomputeHuman(w io.Writer, result recomputeResult) error {
	fmt.Fprintf(w, "Relevance recompute (app_type: %s)\n", relevanceAppTypeLabel(result.AppType))
	if result.Stage != "" {
		fmt.Fprintf(w, "  stage           : %s\n", result.Stage)
	}
	fmt.Fprintf(w, "  iterations      : %d (scored corpus)\n", result.IterationsScanned)
	fmt.Fprintf(w, "  inputs_digest   : %s\n", recomputeDigestLabel(result.InputsDigest))
	fmt.Fprintln(w)

	if len(result.Proposals) == 0 {
		fmt.Fprintln(w, "proposals")
		fmt.Fprintln(w, "  (this app_type lists no units to recompute)")
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "proposals")
		for _, p := range result.Proposals {
			fmt.Fprintf(w, "  [%s] %s\n", p.Stage, p.Unit)
			fmt.Fprintf(w, "    signal   : %s (%d passing, %d low-scoring)\n", p.Signal, p.PassingCitations, p.LowScoringCitations)
			fmt.Fprintf(w, "    class    : %s%s\n", p.CurrentClass, proposedClassSuffix(p))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "gaps (cited but unclassified): %s\n", joinUnits(result.Gaps))
	fmt.Fprintln(w)

	if result.Write {
		fmt.Fprintln(w, "PROPOSED execution_profile layer (review before applying — never auto-applied):")
		if err := writeJSON(w, result.ProposedLayer); err != nil {
			return err
		}
	}
	return nil
}

// proposedClassSuffix renders the " -> proposed" arrow only when the class
// changed, so an unchanged unit stays visually quiet.
func proposedClassSuffix(p unitProposal) string {
	if !p.Changed {
		return ""
	}
	return " -> " + p.ProposedClass
}

// recomputeDigestLabel renders an empty digest (empty corpus) as "(none)" so
// the freshness line never shows a hanging blank.
func recomputeDigestLabel(digest string) string {
	if digest == "" {
		return "(none)"
	}
	return digest
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
