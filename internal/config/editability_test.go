package config

import (
	"errors"
	"testing"
)

// stubAuthorizer is a test WriteAuthorizer that returns a fixed verdict and/or
// error, recording the call so tests can assert delegation happened.
type stubAuthorizer struct {
	verdict Verdict
	err     error
	calls   int
	gotP    Principal
	gotS    WriteTarget
}

func (b *stubAuthorizer) Authorize(p Principal, s WriteTarget) (Verdict, error) {
	b.calls++
	b.gotP = p
	b.gotS = s
	return b.verdict, b.err
}

// assertVerdict fails the test unless the verdict matches the wanted decision
// and scope and carries a non-empty reason. Centralizing these checks keeps the
// table-test loops below under the cognitive-complexity ceiling.
func assertVerdict(t *testing.T, got Verdict, wantDecision Decision, wantScope EditScope) {
	t.Helper()
	if got.Decision != wantDecision {
		t.Fatalf("decision = %q, want %q (reason: %s)", got.Decision, wantDecision, got.Reason)
	}
	if got.Scope != wantScope {
		t.Fatalf("scope = %q, want %q", got.Scope, wantScope)
	}
	if got.Reason == "" {
		t.Fatalf("verdict returned empty reason")
	}
	if want := wantDecision == DecisionAllow; got.Allowed() != want {
		t.Fatalf("Allowed() = %v, want %v", got.Allowed(), want)
	}
}

// assertDelegated fails the test unless the authorizer was invoked exactly once
// with the expected principal and target.
func assertDelegated(t *testing.T, b *stubAuthorizer, wantP, wantS string) {
	t.Helper()
	if b.calls != 1 {
		t.Fatalf("authorizer.calls = %d, want 1 (delegation expected)", b.calls)
	}
	if b.gotP.ID != wantP {
		t.Fatalf("authorizer got principal %q, want %q", b.gotP.ID, wantP)
	}
	if b.gotS.ID != wantS {
		t.Fatalf("authorizer got target %q, want %q", b.gotS.ID, wantS)
	}
}

// assertNotDelegated fails the test if the authorizer was invoked at all.
func assertNotDelegated(t *testing.T, b *stubAuthorizer) {
	t.Helper()
	if b.calls != 0 {
		t.Fatalf("authorizer.calls = %d, want 0 (no delegation expected)", b.calls)
	}
}

func TestEditScopeValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scope EditScope
		want  bool
	}{
		{"local", ScopeLocal, true},
		{"team", ScopeTeam, true},
		{"org", ScopeOrg, true},
		{"project", ScopeProject, true},
		{"empty", EditScope(""), false},
		{"unknown", EditScope("runtime"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.scope.Valid(); got != tt.want {
				t.Fatalf("EditScope(%q).Valid() = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestVerdictAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    Verdict
		want bool
	}{
		{"allow", Verdict{Decision: DecisionAllow}, true},
		{"deny", Verdict{Decision: DecisionDeny}, false},
		{"prompt", Verdict{Decision: DecisionPrompt}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.v.Allowed(); got != tt.want {
				t.Fatalf("Verdict{%v}.Allowed() = %v, want %v", tt.v.Decision, got, tt.want)
			}
		})
	}
}

// TestCheckerCanWrite_NoBackend covers the default Checker (nil authorizer): the
// scope-derivation rules with NO policy backend wired, which is the shipped
// state. Local + personal project allow; governed tiers fall back to the safe
// prompt; unknown scope denies.
func TestCheckerCanWrite_NoBackend(t *testing.T) {
	t.Parallel()
	principal := Principal{ID: "alice", Groups: []string{"payments"}}

	tests := []struct {
		name         string
		target       WriteTarget
		wantDecision Decision
		wantScope    EditScope
	}{
		{
			name:         "local is always writable",
			target:       WriteTarget{ID: "home", Scope: ScopeLocal},
			wantDecision: DecisionAllow,
			wantScope:    ScopeLocal,
		},
		{
			name:         "personal project derives to local-writable",
			target:       WriteTarget{ID: "manager-ui", Scope: ScopeProject, Owner: ""},
			wantDecision: DecisionAllow,
			wantScope:    ScopeLocal,
		},
		{
			name:         "team scope governed but no backend -> safe prompt",
			target:       WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "payments"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeTeam,
		},
		{
			name:         "org scope governed but no backend -> safe prompt",
			target:       WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeOrg,
		},
		{
			name:         "owned project derives to governance, no backend -> safe prompt",
			target:       WriteTarget{ID: "team-proj", Scope: ScopeProject, Owner: "payments"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeProject,
		},
		{
			name:         "unknown scope is denied",
			target:       WriteTarget{ID: "weird", Scope: EditScope("runtime")},
			wantDecision: DecisionDeny,
			wantScope:    EditScope("runtime"),
		},
		{
			name:         "empty scope is denied",
			target:       WriteTarget{ID: "blank", Scope: EditScope("")},
			wantDecision: DecisionDeny,
			wantScope:    EditScope(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NewChecker(nil).CanWrite(principal, tt.target)
			assertVerdict(t, got, tt.wantDecision, tt.wantScope)
		})
	}
}

// TestCheckerCanWrite_BackendDelegation covers the governed path WITH a backend
// wired: the Checker must delegate team/org and owned-project writes to the
// authorizer and surface its verdict, and must NOT delegate local or personal
// project writes.
func TestCheckerCanWrite_BackendDelegation(t *testing.T) {
	t.Parallel()
	principal := Principal{ID: "bob", Groups: []string{"acme"}}

	tests := []struct {
		name          string
		target        WriteTarget
		backendReturn Verdict
		wantDecision  Decision
		wantScope     EditScope
		wantDelegate  bool
	}{
		{
			name:          "team allow from backend",
			target:        WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionAllow, Reason: "member of acme", Scope: ScopeTeam},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeTeam,
			wantDelegate:  true,
		},
		{
			name:          "org deny from backend",
			target:        WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "not an org admin", Scope: ScopeOrg},
			wantDecision:  DecisionDeny,
			wantScope:     ScopeOrg,
			wantDelegate:  true,
		},
		{
			name:          "owned project delegates and reports project scope",
			target:        WriteTarget{ID: "team-proj", Scope: ScopeProject, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionAllow, Reason: "owner match", Scope: ScopeProject},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeProject,
			wantDelegate:  true,
		},
		{
			name:          "local never reaches the backend",
			target:        WriteTarget{ID: "home", Scope: ScopeLocal},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "should not be called"},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeLocal,
			wantDelegate:  false,
		},
		{
			name:          "personal project never reaches the backend",
			target:        WriteTarget{ID: "mine", Scope: ScopeProject, Owner: ""},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "should not be called"},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeLocal,
			wantDelegate:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend := &stubAuthorizer{verdict: tt.backendReturn}
			got := NewChecker(backend).CanWrite(principal, tt.target)

			assertVerdict(t, got, tt.wantDecision, tt.wantScope)
			if tt.wantDelegate {
				assertDelegated(t, backend, principal.ID, tt.target.ID)
			} else {
				assertNotDelegated(t, backend)
			}
		})
	}
}

// TestCheckerCanWrite_BackendError covers the fail-closed path: a backend that
// cannot decide (returns an error) must NOT allow the write — it degrades to
// the safe DecisionPrompt.
func TestCheckerCanWrite_BackendError(t *testing.T) {
	t.Parallel()
	backend := &stubAuthorizer{
		verdict: Verdict{Decision: DecisionAllow, Reason: "ignored on error"},
		err:     errors.New("policy service unreachable"),
	}

	got := NewChecker(backend).CanWrite(
		Principal{ID: "carol"},
		WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
	)

	assertVerdict(t, got, DecisionPrompt, ScopeOrg)
	assertDelegated(t, backend, "carol", "org-src")
}

// TestCheckerCanWrite_BackendVerdictNormalized covers the branch where the
// backend returns a verdict with an empty Scope and Reason: the Checker must
// fill in the derived scope and a default reason rather than emitting blanks.
func TestCheckerCanWrite_BackendVerdictNormalized(t *testing.T) {
	t.Parallel()
	backend := &stubAuthorizer{
		verdict: Verdict{Decision: DecisionAllow}, // no Scope, no Reason
	}

	got := NewChecker(backend).CanWrite(
		Principal{ID: "dave"},
		WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "acme"},
	)

	// assertVerdict checks the normalized scope is ScopeTeam and reason is
	// non-empty (the normalization branch in governed()).
	assertVerdict(t, got, DecisionAllow, ScopeTeam)
}
