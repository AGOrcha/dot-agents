package config

import (
	"errors"
	"testing"
)

// stubBackend is a test PolicyBackend that returns a fixed verdict and/or
// error, recording the call so tests can assert delegation happened.
type stubBackend struct {
	verdict Verdict
	err     error
	calls   int
	gotP    Principal
	gotS    WriteTarget
}

func (b *stubBackend) CanWrite(p Principal, s WriteTarget) (Verdict, error) {
	b.calls++
	b.gotP = p
	b.gotS = s
	return b.verdict, b.err
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

// TestCheckerCanWrite_NoBackend covers the default Checker (nil backend): the
// scope-derivation rules with NO policy backend wired, which is the shipped
// state. Local + personal project allow; governed tiers fall back to the safe
// prompt; unknown scope denies.
func TestCheckerCanWrite_NoBackend(t *testing.T) {
	t.Parallel()
	principal := Principal{ID: "alice", Groups: []string{"payments"}}

	tests := []struct {
		name         string
		source       WriteTarget
		wantDecision Decision
		wantScope    EditScope
	}{
		{
			name:         "local is always writable",
			source:       WriteTarget{ID: "home", Scope: ScopeLocal},
			wantDecision: DecisionAllow,
			wantScope:    ScopeLocal,
		},
		{
			name:         "personal project derives to local-writable",
			source:       WriteTarget{ID: "manager-ui", Scope: ScopeProject, Owner: ""},
			wantDecision: DecisionAllow,
			wantScope:    ScopeLocal,
		},
		{
			name:         "team scope governed but no backend -> safe prompt",
			source:       WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "payments"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeTeam,
		},
		{
			name:         "org scope governed but no backend -> safe prompt",
			source:       WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeOrg,
		},
		{
			name:         "owned project derives to governance, no backend -> safe prompt",
			source:       WriteTarget{ID: "team-proj", Scope: ScopeProject, Owner: "payments"},
			wantDecision: DecisionPrompt,
			wantScope:    ScopeProject,
		},
		{
			name:         "unknown scope is denied",
			source:       WriteTarget{ID: "weird", Scope: EditScope("runtime")},
			wantDecision: DecisionDeny,
			wantScope:    EditScope("runtime"),
		},
		{
			name:         "empty scope is denied",
			source:       WriteTarget{ID: "blank", Scope: EditScope("")},
			wantDecision: DecisionDeny,
			wantScope:    EditScope(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewChecker(nil)
			got := c.CanWrite(principal, tt.source)
			if got.Decision != tt.wantDecision {
				t.Fatalf("CanWrite() decision = %q, want %q (reason: %s)",
					got.Decision, tt.wantDecision, got.Reason)
			}
			if got.Scope != tt.wantScope {
				t.Fatalf("CanWrite() scope = %q, want %q", got.Scope, tt.wantScope)
			}
			if got.Reason == "" {
				t.Fatalf("CanWrite() returned empty reason for %q", tt.name)
			}
			// Allowed() must agree with the decision.
			if want := tt.wantDecision == DecisionAllow; got.Allowed() != want {
				t.Fatalf("Allowed() = %v, want %v", got.Allowed(), want)
			}
		})
	}
}

// TestCheckerCanWrite_BackendDelegation covers the governed path WITH a backend
// wired: the Checker must delegate team/org and owned-project writes to the
// backend and surface its verdict.
func TestCheckerCanWrite_BackendDelegation(t *testing.T) {
	t.Parallel()
	principal := Principal{ID: "bob", Groups: []string{"acme"}}

	tests := []struct {
		name          string
		source        WriteTarget
		backendReturn Verdict
		wantDecision  Decision
		wantScope     EditScope
		wantDelegate  bool
	}{
		{
			name:          "team allow from backend",
			source:        WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionAllow, Reason: "member of acme", Scope: ScopeTeam},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeTeam,
			wantDelegate:  true,
		},
		{
			name:          "org deny from backend",
			source:        WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "not an org admin", Scope: ScopeOrg},
			wantDecision:  DecisionDeny,
			wantScope:     ScopeOrg,
			wantDelegate:  true,
		},
		{
			name:          "owned project delegates and reports project scope",
			source:        WriteTarget{ID: "team-proj", Scope: ScopeProject, Owner: "acme"},
			backendReturn: Verdict{Decision: DecisionAllow, Reason: "owner match", Scope: ScopeProject},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeProject,
			wantDelegate:  true,
		},
		{
			name:          "local never reaches the backend",
			source:        WriteTarget{ID: "home", Scope: ScopeLocal},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "should not be called"},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeLocal,
			wantDelegate:  false,
		},
		{
			name:          "personal project never reaches the backend",
			source:        WriteTarget{ID: "mine", Scope: ScopeProject, Owner: ""},
			backendReturn: Verdict{Decision: DecisionDeny, Reason: "should not be called"},
			wantDecision:  DecisionAllow,
			wantScope:     ScopeLocal,
			wantDelegate:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend := &stubBackend{verdict: tt.backendReturn}
			c := NewChecker(backend)
			got := c.CanWrite(principal, tt.source)

			if got.Decision != tt.wantDecision {
				t.Fatalf("decision = %q, want %q", got.Decision, tt.wantDecision)
			}
			if got.Scope != tt.wantScope {
				t.Fatalf("scope = %q, want %q", got.Scope, tt.wantScope)
			}
			if tt.wantDelegate {
				if backend.calls != 1 {
					t.Fatalf("backend.calls = %d, want 1 (delegation expected)", backend.calls)
				}
				if backend.gotP.ID != principal.ID {
					t.Fatalf("backend got principal %q, want %q", backend.gotP.ID, principal.ID)
				}
				if backend.gotS.ID != tt.source.ID {
					t.Fatalf("backend got source %q, want %q", backend.gotS.ID, tt.source.ID)
				}
			} else if backend.calls != 0 {
				t.Fatalf("backend.calls = %d, want 0 (no delegation expected)", backend.calls)
			}
		})
	}
}

// TestCheckerCanWrite_BackendError covers the fail-closed path: a backend that
// cannot decide (returns an error) must NOT allow the write — it degrades to
// the safe DecisionPrompt.
func TestCheckerCanWrite_BackendError(t *testing.T) {
	t.Parallel()
	backend := &stubBackend{
		verdict: Verdict{Decision: DecisionAllow, Reason: "ignored on error"},
		err:     errors.New("policy service unreachable"),
	}
	c := NewChecker(backend)

	got := c.CanWrite(
		Principal{ID: "carol"},
		WriteTarget{ID: "org-src", Scope: ScopeOrg, Owner: "acme"},
	)

	if got.Decision != DecisionPrompt {
		t.Fatalf("decision = %q, want %q (fail-closed)", got.Decision, DecisionPrompt)
	}
	if got.Allowed() {
		t.Fatal("Allowed() = true on backend error, want false (must fail closed)")
	}
	if got.Scope != ScopeOrg {
		t.Fatalf("scope = %q, want %q", got.Scope, ScopeOrg)
	}
	if backend.calls != 1 {
		t.Fatalf("backend.calls = %d, want 1", backend.calls)
	}
}

// TestCheckerCanWrite_BackendVerdictNormalized covers the branch where the
// backend returns a verdict with an empty Scope and Reason: the Checker must
// fill in the derived scope and a default reason rather than emitting blanks.
func TestCheckerCanWrite_BackendVerdictNormalized(t *testing.T) {
	t.Parallel()
	backend := &stubBackend{
		verdict: Verdict{Decision: DecisionAllow}, // no Scope, no Reason
	}
	c := NewChecker(backend)

	got := c.CanWrite(
		Principal{ID: "dave"},
		WriteTarget{ID: "team-src", Scope: ScopeTeam, Owner: "acme"},
	)

	if got.Decision != DecisionAllow {
		t.Fatalf("decision = %q, want %q", got.Decision, DecisionAllow)
	}
	if got.Scope != ScopeTeam {
		t.Fatalf("scope = %q, want %q (should normalize to derived tier)", got.Scope, ScopeTeam)
	}
	if got.Reason == "" {
		t.Fatal("reason was not normalized; want a non-empty default reason")
	}
}
