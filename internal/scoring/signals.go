package scoring

import "fmt"

// SignalSet is the rubric's six typed input signals for one iteration — the
// objective values the scorer consumes — together with the integrity
// observations (claimed vs observed) for the two-way signals.
//
// It is the output of the signals task: the seam between raw telemetry and the
// scorer. AssembleSignalSet builds one from the three extractor partials;
// BuildSignalSets builds them for a whole iteration log.
type SignalSet struct {
	Iteration int

	Landed             SignalValue
	Verifier           SignalValue
	Tests              SignalValue
	CorrectionPressure SignalValue
	Scope              SignalValue
	TokenEfficiency    SignalValue

	// Integrity holds one observation per two-way signal that had at least one
	// of its claimed / observed sides present. It is a parallel output — it
	// never affects the numeric score.
	Integrity []IntegrityObservation

	// Objective carries the transcript-derived process-discipline checks that
	// replaced the rubber-stamped self_assessment booleans
	// (read_loop_state / committed_after_tests / ran_cli_command). These have
	// no self-reported counterpart in the current schema, so they surface as
	// observational facts alongside the score rather than as integrity pairs.
	Objective IterationObjectives
}

// Value returns the SignalValue for a rubric signal ID, so the scorer can walk
// the rubric's signal list. An unknown ID yields an absent value.
func (s SignalSet) Value(id SignalID) SignalValue {
	switch id {
	case SignalLanded:
		return s.Landed
	case SignalVerifier:
		return s.Verifier
	case SignalTests:
		return s.Tests
	case SignalCorrectionPressure:
		return s.CorrectionPressure
	case SignalScope:
		return s.Scope
	case SignalTokenEfficiency:
		return s.TokenEfficiency
	default:
		return AbsentSignal(fmt.Sprintf("unknown signal %q", id))
	}
}

// IntegrityObservation pairs an agent's self-reported claim with the objective
// observation for one two-way signal, attributed to the role that made the
// claim. The claimed-vs-observed gap is the integrity signal — it never feeds
// the numeric score (see docs/OUTCOME_SCORING_RUBRIC.md).
type IntegrityObservation struct {
	Signal   SignalID
	Role     AgentRole
	Claimed  SignalValue
	Observed SignalValue
}

// Comparable reports whether both sides are present, so Delta is meaningful.
func (o IntegrityObservation) Comparable() bool {
	return o.Claimed.Present && o.Observed.Present
}

// Delta is observed minus claimed when both sides are present: a negative delta
// is an over-claim, the agent having reported better than reality. It is 0 when
// the pair is not Comparable.
func (o IntegrityObservation) Delta() float64 {
	if !o.Comparable() {
		return 0
	}
	return o.Observed.SubScore - o.Claimed.SubScore
}

// correctionPressureErrorCoefficient weights the tool-call error rate in the
// correction_pressure formula — see docs/OUTCOME_SCORING_RUBRIC.md.
const correctionPressureErrorCoefficient = 2.0

// correctionPressure folds retries, user corrections, and the optional
// tool-call error rate into a sub-score: 1 / (1 + retries + corrections +
// 2·errorRate) — 1.0 for a clean run, decaying as friction accumulates. It is
// always present: an iteration with no recorded friction scores a clean 1.0.
func correctionPressure(retries, userCorrections int, errorRate float64, errorRatePresent bool) SignalValue {
	friction := float64(retries) + float64(userCorrections)
	detail := fmt.Sprintf("%d retries, %d user corrections", retries, userCorrections)
	if errorRatePresent {
		friction += correctionPressureErrorCoefficient * errorRate
		detail += fmt.Sprintf(", %.0f%% tool-error rate", errorRate*100)
	}
	return PresentSignal(1.0/(1.0+friction), detail)
}

// coalesce returns primary when it is present, otherwise the fallback. It backs
// the rubric's "objective source, self-reported fallback" rule for scope.
func coalesce(primary, fallback SignalValue) SignalValue {
	if primary.Present {
		return primary
	}
	return fallback
}

// integrityRole attributes an observation to a role. v1 iteration entries are
// flat — they have no role-owned blocks — so every v1 observation attributes to
// impl, per docs/OUTCOME_SCORING_RUBRIC.md.
func integrityRole(natural AgentRole, schemaVersion int) AgentRole {
	if schemaVersion == 1 {
		return RoleImpl
	}
	return natural
}

// integrityObservations pairs the claimed and observed sides of each two-way
// signal. A pair with neither side present is dropped — there is nothing to
// say about it.
func integrityObservations(rec IterationRecord, il IterlogSignals, gs GitSignals) []IntegrityObservation {
	pairs := []struct {
		signal            SignalID
		role              AgentRole
		claimed, observed SignalValue
	}{
		{SignalLanded, RoleImpl, il.LandedClaimed, gs.LandedObserved},
		{SignalVerifier, RoleVerifier, il.VerifierClaimed, il.Verifier},
		{SignalTests, RoleImpl, il.TestsClaimed, il.Verifier},
		{SignalScope, RoleImpl, il.ScopeClaimed, gs.ScopeObserved},
	}
	var obs []IntegrityObservation
	for _, p := range pairs {
		if !p.claimed.Present && !p.observed.Present {
			continue
		}
		obs = append(obs, IntegrityObservation{
			Signal:   p.signal,
			Role:     integrityRole(p.role, rec.SchemaVersion),
			Claimed:  p.claimed,
			Observed: p.observed,
		})
	}
	return obs
}

// AssembleSignalSet joins the extractor partials for one iteration into the
// rubric's typed input set. It is pure — the scorer task consumes its output.
//
// landed and token_efficiency are objective-only. verifier and tests come from
// the iteration log and its verification artifacts. scope prefers the objective
// git measurement and falls back to the self-reported scope_note.
// correction_pressure is composed from retries, user corrections, and the
// transcript error rate. The IterationObjectives are recorded as observational
// facts on the result; they do not enter the score directly.
func AssembleSignalSet(rec IterationRecord, il IterlogSignals, gs GitSignals, bf BackfillSignals, obj IterationObjectives) SignalSet {
	return SignalSet{
		Iteration:          rec.Iteration,
		Landed:             gs.LandedObserved,
		Verifier:           il.Verifier,
		Tests:              il.TestsClaimed,
		CorrectionPressure: correctionPressure(il.Retries, il.UserCorrections, bf.ToolErrorRate, bf.ToolErrorRatePresent),
		Scope:              coalesce(gs.ScopeObserved, il.ScopeClaimed),
		TokenEfficiency:    bf.TokenEfficiency,
		Integrity:          integrityObservations(rec, il, gs),
		Objective:          obj,
	}
}

// BuildSignalSets loads an iteration log and runs every extractor, returning one
// assembled SignalSet per iteration in iteration order.
//
// repoDir is the dot-agents repo root — used for git topology and for resolving
// the repo-relative verification-artifact paths. transcriptDirs are the agent
// session-log roots (~/.claude/projects/..., ~/.codex/sessions) for token
// backfill; pass none to score only natively-captured token telemetry.
func BuildSignalSets(iterLogDir, repoDir string, transcriptDirs ...string) ([]SignalSet, error) {
	records, err := LoadIterationLog(iterLogDir)
	if err != nil {
		return nil, err
	}

	backfills, err := BackfillIterations(records, repoDir, transcriptDirs...)
	if err != nil {
		return nil, fmt.Errorf("scoring: token backfill: %w", err)
	}
	bfByIter := make(map[int]BackfillSignals, len(backfills))
	for _, b := range backfills {
		bfByIter[b.Iteration] = b
	}

	// resolveWindows is the same per-iteration commit-windowing the backfill
	// uses; reusing it keeps the objective-check scans aligned with the token
	// scans (same window, same iteration boundaries).
	windows := resolveWindows(records, repoDir)

	sets := make([]SignalSet, 0, len(records))
	for _, rec := range records {
		il := ExtractIterlogSignals(rec, repoDir)
		gs, err := ExtractGitSignals(rec, repoDir)
		if err != nil {
			return nil, fmt.Errorf("scoring: git signals for iteration %d: %w", rec.Iteration, err)
		}
		obj := ExtractIterationObjectives(rec, windows[rec.Iteration], transcriptDirs...)
		sets = append(sets, AssembleSignalSet(rec, il, gs, bfByIter[rec.Iteration], obj))
	}
	return sets, nil
}
