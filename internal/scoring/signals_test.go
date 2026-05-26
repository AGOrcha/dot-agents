package scoring

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCorrectionPressure(t *testing.T) {
	tests := []struct {
		name             string
		retries          int
		userCorrections  int
		errorRate        float64
		errorRatePresent bool
		want             float64
	}{
		{"clean run", 0, 0, 0, false, 1.0},
		{"two retries", 2, 0, 0, false, 1.0 / 3.0},
		{"retries and corrections", 1, 3, 0, false, 0.2},
		{"error rate only", 0, 0, 0.5, true, 0.5},  // 1/(1+2*0.5)
		{"all three", 1, 1, 0.25, true, 1.0 / 3.5}, // 1/(1+1+1+0.5)
		{"error rate ignored when absent", 5, 0, 0.9, false, 1.0 / 6.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := correctionPressure(tt.retries, tt.userCorrections, tt.errorRate, tt.errorRatePresent)
			if !got.Present {
				t.Fatal("correctionPressure is never absent, got Present=false")
			}
			if !approxEq(got.SubScore, tt.want) {
				t.Errorf("SubScore = %g, want %g", got.SubScore, tt.want)
			}
			if got.Detail == "" {
				t.Error("Detail is empty")
			}
		})
	}
}

func TestCoalesce(t *testing.T) {
	primary := PresentSignal(0.8, "primary")
	fallback := PresentSignal(0.2, "fallback")

	if got := coalesce(primary, fallback); got.Detail != "primary" {
		t.Errorf("coalesce(present, _) = %q, want primary", got.Detail)
	}
	if got := coalesce(AbsentSignal("none"), fallback); got.Detail != "fallback" {
		t.Errorf("coalesce(absent, present) = %q, want fallback", got.Detail)
	}
}

func TestIntegrityObservation(t *testing.T) {
	tests := []struct {
		name           string
		claimed        SignalValue
		observed       SignalValue
		wantComparable bool
		wantDelta      float64
	}{
		{"under-claim", PresentSignal(0.5, "c"), PresentSignal(1.0, "o"), true, 0.5},
		{"over-claim", PresentSignal(1.0, "c"), PresentSignal(0.0, "o"), true, -1.0},
		{"honest", PresentSignal(0.7, "c"), PresentSignal(0.7, "o"), true, 0.0},
		{"observed absent", PresentSignal(0.5, "c"), AbsentSignal("o"), false, 0.0},
		{"claimed absent", AbsentSignal("c"), PresentSignal(0.5, "o"), false, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := IntegrityObservation{Claimed: tt.claimed, Observed: tt.observed}
			if o.Comparable() != tt.wantComparable {
				t.Errorf("Comparable() = %v, want %v", o.Comparable(), tt.wantComparable)
			}
			if !approxEq(o.Delta(), tt.wantDelta) {
				t.Errorf("Delta() = %g, want %g", o.Delta(), tt.wantDelta)
			}
		})
	}
}

func TestIntegrityRole(t *testing.T) {
	if got := integrityRole(RoleVerifier, 2); got != RoleVerifier {
		t.Errorf("integrityRole(verifier, v2) = %q, want verifier", got)
	}
	if got := integrityRole(RoleVerifier, 1); got != RoleImpl {
		t.Errorf("integrityRole(verifier, v1) = %q, want impl — v1 entries are flat", got)
	}
}

// fullPartials returns one populated set of extractor partials for assembly tests.
func fullPartials() (IterlogSignals, GitSignals, BackfillSignals) {
	il := IterlogSignals{
		ScopeClaimed:    PresentSignal(1.0, "on-target"),
		TestsClaimed:    PresentSignal(1.0, "2/2 flags true"),
		Verifier:        PresentSignal(1.0, "1/1 verifier passed"),
		VerifierClaimed: PresentSignal(0.8, "ran cli, committed after tests"),
		LandedClaimed:   PresentSignal(1.0, "persisted via workflow"),
		Retries:         1,
		UserCorrections: 0,
	}
	gs := GitSignals{
		LandedObserved: PresentSignal(1.0, "reachable from master"),
		ScopeObserved:  AbsentSignal("no write_scope for task"),
	}
	bf := BackfillSignals{
		Iteration:            5,
		TokenEfficiency:      PresentSignal(0.9, "cache hit rate 0.90"),
		ToolErrorRate:        0,
		ToolErrorRatePresent: false,
	}
	return il, gs, bf
}

func TestAssembleSignalSet(t *testing.T) {
	rec := IterationRecord{Iteration: 5, SchemaVersion: 2}
	il, gs, bf := fullPartials()

	set := AssembleSignalSet(rec, il, gs, bf, IterationObjectives{}, AbsentSignal("no sidecar"))

	if set.Iteration != 5 {
		t.Errorf("Iteration = %d, want 5", set.Iteration)
	}
	// landed and token_efficiency are objective-only, taken straight through.
	if set.Landed.Detail != "reachable from master" {
		t.Errorf("Landed = %+v, want the git-observed value", set.Landed)
	}
	if set.TokenEfficiency.SubScore != 0.9 {
		t.Errorf("TokenEfficiency.SubScore = %g, want 0.9", set.TokenEfficiency.SubScore)
	}
	// verifier and tests come from the iteration log.
	if set.Verifier.SubScore != 1.0 || set.Tests.SubScore != 1.0 {
		t.Errorf("Verifier/Tests = %g/%g, want 1.0/1.0", set.Verifier.SubScore, set.Tests.SubScore)
	}
	// scope falls back to the self-reported value when the git measure is absent.
	if set.Scope.Detail != "on-target" {
		t.Errorf("Scope = %+v, want the self-reported fallback", set.Scope)
	}
	// correction_pressure = 1/(1+1 retry) = 0.5.
	if !approxEq(set.CorrectionPressure.SubScore, 0.5) {
		t.Errorf("CorrectionPressure.SubScore = %g, want 0.5", set.CorrectionPressure.SubScore)
	}
	// Every two-way signal has at least one side present -> four observations.
	if len(set.Integrity) != 4 {
		t.Fatalf("Integrity has %d observations, want 4", len(set.Integrity))
	}
}

func TestAssembleScopePrefersObjective(t *testing.T) {
	rec := IterationRecord{Iteration: 1, SchemaVersion: 2}
	il, gs, bf := fullPartials()
	gs.ScopeObserved = PresentSignal(0.6, "3/5 files in scope")

	set := AssembleSignalSet(rec, il, gs, bf, IterationObjectives{}, AbsentSignal("no sidecar"))
	if set.Scope.Detail != "3/5 files in scope" {
		t.Errorf("Scope = %+v, want the objective git measurement", set.Scope)
	}
}

func TestAssembleIntegrityRolesV2(t *testing.T) {
	rec := IterationRecord{Iteration: 5, SchemaVersion: 2}
	il, gs, bf := fullPartials()

	set := AssembleSignalSet(rec, il, gs, bf, IterationObjectives{}, AbsentSignal("no sidecar"))
	roles := make(map[SignalID]AgentRole)
	for _, o := range set.Integrity {
		roles[o.Signal] = o.Role
	}
	if roles[SignalVerifier] != RoleVerifier {
		t.Errorf("v2 verifier observation role = %q, want verifier", roles[SignalVerifier])
	}
	if roles[SignalLanded] != RoleImpl || roles[SignalScope] != RoleImpl {
		t.Error("landed and scope observations should attribute to impl")
	}
}

func TestAssembleIntegrityRolesV1(t *testing.T) {
	rec := IterationRecord{Iteration: 5, SchemaVersion: 1}
	il, gs, bf := fullPartials()

	set := AssembleSignalSet(rec, il, gs, bf, IterationObjectives{}, AbsentSignal("no sidecar"))
	for _, o := range set.Integrity {
		if o.Role != RoleImpl {
			t.Errorf("v1 observation for %q has role %q, want impl — v1 entries are flat", o.Signal, o.Role)
		}
	}
}

func TestAssembleIntegritySkipsEmptyPairs(t *testing.T) {
	rec := IterationRecord{Iteration: 9, SchemaVersion: 2}
	// Every signal absent on both sides -> no observations at all.
	il := IterlogSignals{
		ScopeClaimed:    AbsentSignal("x"),
		TestsClaimed:    AbsentSignal("x"),
		Verifier:        AbsentSignal("x"),
		VerifierClaimed: AbsentSignal("x"),
		LandedClaimed:   AbsentSignal("x"),
	}
	gs := GitSignals{LandedObserved: AbsentSignal("x"), ScopeObserved: AbsentSignal("x")}
	bf := BackfillSignals{Iteration: 9, TokenEfficiency: AbsentSignal("x")}

	set := AssembleSignalSet(rec, il, gs, bf, IterationObjectives{}, AbsentSignal("no sidecar"))
	if len(set.Integrity) != 0 {
		t.Errorf("Integrity has %d observations, want 0 when every pair is empty", len(set.Integrity))
	}
	// correction_pressure is still present — a clean run with no recorded friction.
	if !set.CorrectionPressure.Present || !approxEq(set.CorrectionPressure.SubScore, 1.0) {
		t.Errorf("CorrectionPressure = %+v, want present 1.0", set.CorrectionPressure)
	}
}

func TestSignalSetValue(t *testing.T) {
	set := SignalSet{
		Landed:             PresentSignal(0.1, "landed"),
		Verifier:           PresentSignal(0.2, "verifier"),
		Tests:              PresentSignal(0.3, "tests"),
		CorrectionPressure: PresentSignal(0.4, "correction"),
		Scope:              PresentSignal(0.5, "scope"),
		HookOutcomes:       PresentSignal(0.7, "hooks"),
		TokenEfficiency:    PresentSignal(0.6, "token"),
	}
	for id, want := range map[SignalID]float64{
		SignalLanded:             0.1,
		SignalVerifier:           0.2,
		SignalTests:              0.3,
		SignalCorrectionPressure: 0.4,
		SignalScope:              0.5,
		SignalHookOutcomes:       0.7,
		SignalTokenEfficiency:    0.6,
	} {
		if got := set.Value(id); got.SubScore != want {
			t.Errorf("Value(%q).SubScore = %g, want %g", id, got.SubScore, want)
		}
	}
	if got := set.Value(SignalID("nonexistent")); got.Present {
		t.Errorf("Value(unknown) = %+v, want absent", got)
	}
}

func TestBuildSignalSetsReportsBadIterLog(t *testing.T) {
	_, err := BuildSignalSets(filepath.Join("testdata", "iterlog_badhist"), ".")
	if err == nil {
		t.Fatal("BuildSignalSets with a malformed iteration log = nil error, want error")
	}
}

func TestBuildSignalSetsReportsBackfillError(t *testing.T) {
	// The iteration log loads, but the repo dir is not a git repo, so the
	// backfill's commit-time resolution fails.
	_, err := BuildSignalSets(filepath.Join("testdata", "iterlog"), t.TempDir())
	if err == nil {
		t.Fatal("BuildSignalSets with a non-git repoDir = nil error, want error")
	}
}

// TestBuildSignalSets is the slice's integration assertion: the whole pipeline
// — load, backfill, extract, assemble — runs over the real iteration log.
func TestBuildSignalSets(t *testing.T) {
	iterLogDir := filepath.Join("..", "..", ".agents", "active", "iteration-log")
	repoDir := filepath.Join("..", "..")
	if _, err := os.Stat(iterLogDir); err != nil {
		t.Skipf("iteration-log dir not present: %v", err)
	}

	sets, err := BuildSignalSets(iterLogDir, repoDir)
	if err != nil {
		t.Fatalf("BuildSignalSets() = %v, want nil", err)
	}
	if len(sets) == 0 {
		t.Fatal("BuildSignalSets() produced no signal sets")
	}
	for _, s := range sets {
		if s.Iteration == 0 {
			t.Error("a SignalSet has Iteration 0 — assembly lost the iteration number")
		}
		// correction_pressure is always computable.
		if !s.CorrectionPressure.Present {
			t.Errorf("iteration %d: CorrectionPressure is absent, want always present", s.Iteration)
		}
	}
	t.Logf("assembled %d signal sets from the real iteration log", len(sets))
}
