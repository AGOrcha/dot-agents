package scoring

import (
	"path/filepath"
	"testing"
)

// fixtureRepo is the prefixed testdata repo root holding the verification
// artifacts the iteration-log signals resolve against.
func fixtureRepo() string {
	return filepath.Join("testdata", "iterlog_signals_repo")
}

func wantPresent(t *testing.T, label string, got SignalValue, want float64) {
	t.Helper()
	if !got.Present {
		t.Fatalf("%s: SignalValue.Present = false, want true (detail %q)", label, got.Detail)
	}
	if got.SubScore != want {
		t.Errorf("%s: SubScore = %g, want %g (detail %q)", label, got.SubScore, want, got.Detail)
	}
	if got.Detail == "" {
		t.Errorf("%s: present signal has empty Detail", label)
	}
}

func wantAbsent(t *testing.T, label string, got SignalValue) {
	t.Helper()
	if got.Present {
		t.Fatalf("%s: SignalValue.Present = true, want false (subscore %g)", label, got.SubScore)
	}
	if got.Detail == "" {
		t.Errorf("%s: absent signal has empty Detail", label)
	}
}

func optBool(v bool) OptionalBool { return OptionalBool{Set: true, Value: v} }

// --- ScopeClaimed ----------------------------------------------------------

func TestScopeClaimed(t *testing.T) {
	tests := []struct {
		name      string
		scopeNote string
		present   bool
		want      float64
	}{
		{"on-target enum", "on-target", true, 1.0},
		{"partial enum", "partial", true, 0.5},
		{"scope-breach enum", "scope-breach", true, 0.0},
		{"uppercase normalizes", "On-Target", true, 1.0},
		{"whitespace trimmed", "  partial  ", true, 0.5},
		{"free-text on-target prefix", "on-target — all files inside write_scope", true, 1.0},
		{"empty", "", false, 0},
		{"unrecognized free-text", "did some stuff", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := IterationRecord{Impl: ImplBlock{ScopeNote: tt.scopeNote}}
			got := ExtractIterlogSignals(rec, "").ScopeClaimed
			if tt.present {
				wantPresent(t, "ScopeClaimed", got, tt.want)
			} else {
				wantAbsent(t, "ScopeClaimed", got)
			}
		})
	}
}

// --- TestsClaimed ----------------------------------------------------------

func TestTestsClaimed(t *testing.T) {
	t.Run("no flags set is absent", func(t *testing.T) {
		got := ExtractIterlogSignals(IterationRecord{}, "").TestsClaimed
		wantAbsent(t, "TestsClaimed", got)
	})

	t.Run("all set flags true", func(t *testing.T) {
		rec := IterationRecord{
			Impl: ImplBlock{
				FocusedTestsPass: optBool(true),
				TestsTotalPass:   optBool(true),
			},
		}
		got := ExtractIterlogSignals(rec, "").TestsClaimed
		wantPresent(t, "TestsClaimed", got, 1.0)
	})

	t.Run("fraction across impl and verifiers", func(t *testing.T) {
		// 4 flags set: impl.focused true, impl.total false,
		// verifier#1 true, verifier#2 false -> 2/4 = 0.5.
		rec := IterationRecord{
			Impl: ImplBlock{
				FocusedTestsPass: optBool(true),
				TestsTotalPass:   optBool(false),
			},
			Verifiers: []VerifierRecord{
				{TestsTotalPass: optBool(true)},
				{TestsTotalPass: optBool(false)},
			},
		}
		got := ExtractIterlogSignals(rec, "").TestsClaimed
		wantPresent(t, "TestsClaimed", got, 0.5)
	})

	t.Run("unset flags ignored", func(t *testing.T) {
		// Only the one set flag counts; the unset ones do not dilute.
		rec := IterationRecord{
			Impl:      ImplBlock{TestsTotalPass: optBool(true)},
			Verifiers: []VerifierRecord{{}, {}},
		}
		got := ExtractIterlogSignals(rec, "").TestsClaimed
		wantPresent(t, "TestsClaimed", got, 1.0)
	})
}

// --- Verifier --------------------------------------------------------------

func TestVerifierObjective(t *testing.T) {
	t.Run("mean of pass/fail/partial", func(t *testing.T) {
		// pass(1.0) + partial(0.5) + fail(0.0) over 3 = 0.5.
		rec := IterationRecord{Verifiers: []VerifierRecord{
			{Status: "pass"},
			{Status: "partial"},
			{Status: "fail"},
		}}
		got := ExtractIterlogSignals(rec, "").Verifier
		wantPresent(t, "Verifier", got, 0.5)
	})

	t.Run("unknown excluded from mean", func(t *testing.T) {
		// pass + unknown -> only pass counts -> 1.0.
		rec := IterationRecord{Verifiers: []VerifierRecord{
			{Status: "pass"},
			{Status: "unknown"},
		}}
		got := ExtractIterlogSignals(rec, "").Verifier
		wantPresent(t, "Verifier", got, 1.0)
	})

	t.Run("all unknown is absent", func(t *testing.T) {
		rec := IterationRecord{Verifiers: []VerifierRecord{
			{Status: "unknown"},
			{Status: ""},
		}}
		got := ExtractIterlogSignals(rec, "").Verifier
		wantAbsent(t, "Verifier", got)
	})

	t.Run("v1 fallback to tests_total_pass pass", func(t *testing.T) {
		rec := IterationRecord{Impl: ImplBlock{TestsTotalPass: optBool(true)}}
		got := ExtractIterlogSignals(rec, "").Verifier
		wantPresent(t, "Verifier", got, 1.0)
	})

	t.Run("v1 fallback to tests_total_pass fail", func(t *testing.T) {
		rec := IterationRecord{Impl: ImplBlock{TestsTotalPass: optBool(false)}}
		got := ExtractIterlogSignals(rec, "").Verifier
		wantPresent(t, "Verifier", got, 0.0)
	})

	t.Run("no verifiers and no proxy is absent", func(t *testing.T) {
		got := ExtractIterlogSignals(IterationRecord{}, "").Verifier
		wantAbsent(t, "Verifier", got)
	})
}

func TestVerifierArtifactOverride(t *testing.T) {
	// The inline status is pass, but the resolved result_artifact records
	// fail; the artifact wins.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "pass",
		ResultArtifact: ".agents/active/verification/c3/test.result.yaml",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (artifact override)", got, 0.0)
}

func TestVerifierArtifactMissingFallsBackToInline(t *testing.T) {
	// Artifact path does not resolve; inline status is used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "pass",
		ResultArtifact: ".agents/active/verification/c3/does-not-exist.yaml",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (missing artifact)", got, 1.0)
}

func TestVerifierArtifactUnreadableWithoutRepoRoot(t *testing.T) {
	// repoRoot is empty: artifact cannot be resolved, inline status used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "partial",
		ResultArtifact: ".agents/active/verification/c3/test.result.yaml",
	}}}
	got := ExtractIterlogSignals(rec, "").Verifier
	wantPresent(t, "Verifier (no repoRoot)", got, 0.5)
}

func TestVerifierArtifactMalformedFallsBackToInline(t *testing.T) {
	// The artifact exists but is not parseable; inline status is used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "pass",
		ResultArtifact: ".agents/active/verification/badart/test.result.yaml",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (malformed artifact)", got, 1.0)
}

func TestVerifierArtifactNoStatusFallsBackToInline(t *testing.T) {
	// The artifact parses cleanly but records no status field; the inline
	// status is used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "partial",
		ResultArtifact: ".agents/active/verification/c3/nostatus.result.yaml",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (no-status artifact)", got, 0.5)
}

func TestVerifierArtifactPathEscapeRejected(t *testing.T) {
	// A traversal path that escapes the repo root is refused; inline used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "fail",
		ResultArtifact: "../../../etc/passwd",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (path escape)", got, 0.0)
}

func TestVerifierArtifactAbsolutePathRejected(t *testing.T) {
	// An absolute artifact path is not trusted; inline status is used.
	rec := IterationRecord{Verifiers: []VerifierRecord{{
		Status:         "pass",
		ResultArtifact: "/etc/hosts",
	}}}
	got := ExtractIterlogSignals(rec, fixtureRepo()).Verifier
	wantPresent(t, "Verifier (absolute path)", got, 1.0)
}

// --- VerifierClaimed -------------------------------------------------------
//
// After the objective-checks rework VerifierClaimed reads only
// linked_traces_to_outcomes — ran_cli_command and committed_after_tests were
// dropped from the schema because they were rubber-stamped at ~98% true.

func TestVerifierClaimed(t *testing.T) {
	t.Run("zero self-assessment is absent", func(t *testing.T) {
		got := ExtractIterlogSignals(IterationRecord{}, "").VerifierClaimed
		wantAbsent(t, "VerifierClaimed", got)
	})

	t.Run("impl linked_traces_to_outcomes true", func(t *testing.T) {
		rec := IterationRecord{Impl: ImplBlock{SelfAssessment: SelfAssessment{
			LinkedTracesToOutcomes: true,
		}}}
		got := ExtractIterlogSignals(rec, "").VerifierClaimed
		wantPresent(t, "VerifierClaimed", got, 1.0)
	})

	t.Run("verifier-block linked_traces_to_outcomes true", func(t *testing.T) {
		// v2 carries the flag on the verifier block, not impl.
		rec := IterationRecord{Verifiers: []VerifierRecord{{
			SelfAssessment: SelfAssessment{LinkedTracesToOutcomes: true},
		}}}
		got := ExtractIterlogSignals(rec, "").VerifierClaimed
		wantPresent(t, "VerifierClaimed", got, 1.0)
	})

	t.Run("self-assessment present but no linked_traces", func(t *testing.T) {
		// self_assessment carries info but linked_traces_to_outcomes is false
		// — the claim is recorded as not-linked: present 0.0, not absent.
		rec := IterationRecord{Impl: ImplBlock{SelfAssessment: SelfAssessment{
			OneItemOnly: OptionalBool{Set: true, Value: true},
		}}}
		got := ExtractIterlogSignals(rec, "").VerifierClaimed
		wantPresent(t, "VerifierClaimed", got, 0.0)
	})
}

// --- LandedClaimed ---------------------------------------------------------

func TestLandedClaimed(t *testing.T) {
	tests := []struct {
		name      string
		persisted string
		decision  string
		present   bool
		want      float64
	}{
		{"persisted yes + accept", "yes", "accept", true, 1.0},
		{"persisted yes + reject", "yes", "reject", true, 0.5},
		{"persisted yes + escalate", "yes", "escalate", true, 0.75},
		{"persisted yes only", "yes — verify record, checkpoint", "", true, 1.0},
		{"persisted no only", "no", "", true, 0.0},
		{"accept only", "", "accept", true, 1.0},
		{"reject only", "", "reject", true, 0.0},
		{"escalate only", "", "escalate", true, 0.5},
		{"neither", "", "", false, 0},
		{"unrecognized decision ignored", "yes", "deferred", true, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := IterationRecord{
				Impl: ImplBlock{SelfAssessment: SelfAssessment{
					PersistedViaWorkflowCommands: tt.persisted,
				}},
				Review: ReviewBlock{OverallDecision: tt.decision},
			}
			got := ExtractIterlogSignals(rec, "").LandedClaimed
			if tt.present {
				wantPresent(t, "LandedClaimed", got, tt.want)
			} else {
				wantAbsent(t, "LandedClaimed", got)
			}
		})
	}
}

// --- Retries ---------------------------------------------------------------

func TestRetries(t *testing.T) {
	rec := IterationRecord{
		Impl: ImplBlock{Retries: 2},
		Verifiers: []VerifierRecord{
			{Retries: 1},
			{Retries: 3},
		},
	}
	if got := ExtractIterlogSignals(rec, "").Retries; got != 6 {
		t.Errorf("Retries = %d, want 6", got)
	}
	if got := ExtractIterlogSignals(IterationRecord{}, "").Retries; got != 0 {
		t.Errorf("Retries (empty) = %d, want 0", got)
	}
}

// --- UserCorrections -------------------------------------------------------

func TestUserCorrections(t *testing.T) {
	t.Run("reads list length plus retries_in_loop", func(t *testing.T) {
		// fixture: 2 user_corrections + retries_in_loop 1 = 3.
		rec := IterationRecord{Review: ReviewBlock{
			DecisionArtifact: ".agents/active/verification/c3/review-decision.yaml",
		}}
		if got := ExtractIterlogSignals(rec, fixtureRepo()).UserCorrections; got != 3 {
			t.Errorf("UserCorrections = %d, want 3", got)
		}
	})

	t.Run("missing artifact yields zero", func(t *testing.T) {
		rec := IterationRecord{Review: ReviewBlock{
			DecisionArtifact: ".agents/active/verification/c3/nope.yaml",
		}}
		if got := ExtractIterlogSignals(rec, fixtureRepo()).UserCorrections; got != 0 {
			t.Errorf("UserCorrections (missing) = %d, want 0", got)
		}
	})

	t.Run("no repoRoot yields zero", func(t *testing.T) {
		rec := IterationRecord{Review: ReviewBlock{
			DecisionArtifact: ".agents/active/verification/c3/review-decision.yaml",
		}}
		if got := ExtractIterlogSignals(rec, "").UserCorrections; got != 0 {
			t.Errorf("UserCorrections (no repoRoot) = %d, want 0", got)
		}
	})

	t.Run("empty artifact path yields zero", func(t *testing.T) {
		if got := ExtractIterlogSignals(IterationRecord{}, fixtureRepo()).UserCorrections; got != 0 {
			t.Errorf("UserCorrections (empty path) = %d, want 0", got)
		}
	})

	t.Run("malformed artifact yields zero", func(t *testing.T) {
		rec := IterationRecord{Review: ReviewBlock{
			DecisionArtifact: ".agents/active/verification/badart/review-decision.yaml",
		}}
		if got := ExtractIterlogSignals(rec, fixtureRepo()).UserCorrections; got != 0 {
			t.Errorf("UserCorrections (malformed) = %d, want 0", got)
		}
	})
}

// --- end-to-end on real iteration-log data ---------------------------------

// TestExtractIterlogSignalsRealData parses every real iteration-log entry and
// extracts signals from each, asserting only that the call never panics and
// produces internally consistent values. It is the broad smoke test.
func TestExtractIterlogSignalsRealData(t *testing.T) {
	dir := filepath.Join("..", "..", ".agents", "active", "iteration-log")
	records, err := LoadIterationLog(dir)
	if err != nil {
		t.Skipf("real iteration-log not available: %v", err)
	}
	if len(records) == 0 {
		t.Skip("no real iteration-log records")
	}
	repoRoot := filepath.Join("..", "..")
	for _, rec := range records {
		sig := ExtractIterlogSignals(rec, repoRoot)
		for label, sv := range map[string]SignalValue{
			"ScopeClaimed":    sig.ScopeClaimed,
			"TestsClaimed":    sig.TestsClaimed,
			"Verifier":        sig.Verifier,
			"VerifierClaimed": sig.VerifierClaimed,
			"LandedClaimed":   sig.LandedClaimed,
		} {
			if sv.Present && (sv.SubScore < 0 || sv.SubScore > 1) {
				t.Errorf("iter %d %s: SubScore %g out of [0,1]", rec.Iteration, label, sv.SubScore)
			}
			if sv.Detail == "" {
				t.Errorf("iter %d %s: empty Detail", rec.Iteration, label)
			}
		}
		if sig.Retries < 0 {
			t.Errorf("iter %d: negative Retries %d", rec.Iteration, sig.Retries)
		}
		if sig.UserCorrections < 0 {
			t.Errorf("iter %d: negative UserCorrections %d", rec.Iteration, sig.UserCorrections)
		}
	}
}

// TestExtractIterlogSignalsRealV2WithVerifier exercises a real v2 entry that
// carries a populated verifiers array (iter-52).
func TestExtractIterlogSignalsRealV2WithVerifier(t *testing.T) {
	rec := mustParse(t, "v2_iter.yaml")
	sig := ExtractIterlogSignals(rec, "")
	wantPresent(t, "real v2 ScopeClaimed", sig.ScopeClaimed, 1.0)
	wantPresent(t, "real v2 Verifier", sig.Verifier, 1.0)
	wantPresent(t, "real v2 TestsClaimed", sig.TestsClaimed, 1.0)
	// v2_iter.yaml verifier self_assessment sets linked_traces_to_outcomes,
	// which is now the sole input to VerifierClaimed -> 1.0.
	wantPresent(t, "real v2 VerifierClaimed", sig.VerifierClaimed, 1.0)
	wantPresent(t, "real v2 LandedClaimed", sig.LandedClaimed, 1.0)
	if sig.Retries != 1 {
		t.Errorf("real v2 Retries = %d, want 1", sig.Retries)
	}
}

// itoa is exercised indirectly through Detail strings; this pins its edge
// cases (zero and a negative value) directly.
func TestItoa(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", 42: "42", -5: "-5", 1000: "1000"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
