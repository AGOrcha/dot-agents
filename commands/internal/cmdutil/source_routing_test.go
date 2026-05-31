package cmdutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeAuthorizer is a table-driven stand-in for config.WriteAuthorizer. It
// records the call and returns a canned verdict/error so the routing tests can
// exercise allow/deny/prompt and backend-error paths without a real backend.
type fakeAuthorizer struct {
	verdict config.Verdict
	err     error

	called    bool
	gotPrin   config.Principal
	gotTarget config.WriteTarget
}

func (f *fakeAuthorizer) Authorize(p config.Principal, s config.WriteTarget) (config.Verdict, error) {
	f.called = true
	f.gotPrin = p
	f.gotTarget = s
	return f.verdict, f.err
}

func TestBindScopeSourceFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	var f ScopeSourceFlags
	BindScopeSourceFlags(cmd, &f)

	scopeFlag := cmd.Flags().Lookup(FlagScope)
	if scopeFlag == nil {
		t.Fatalf("--%s not registered", FlagScope)
	}
	if scopeFlag.DefValue != string(config.ScopeLocal) {
		t.Errorf("--%s default = %q, want %q", FlagScope, scopeFlag.DefValue, config.ScopeLocal)
	}
	if cmd.Flags().Lookup(FlagSource) == nil {
		t.Fatalf("--%s not registered", FlagSource)
	}

	// Parsing writes back into the bound struct.
	if err := cmd.Flags().Parse([]string{"--scope", "team", "--source", "acme"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Scope != "team" || f.Source != "acme" {
		t.Fatalf("parsed flags = %+v, want scope=team source=acme", f)
	}
}

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name      string
		flags     ScopeSourceFlags
		owner     string
		wantScope config.EditScope
		wantErr   bool
	}{
		{
			name:      "empty scope defaults to local",
			flags:     ScopeSourceFlags{Scope: "", Source: "personal"},
			wantScope: config.ScopeLocal,
		},
		{
			name:      "explicit local",
			flags:     ScopeSourceFlags{Scope: "local", Source: "personal"},
			wantScope: config.ScopeLocal,
		},
		{
			name:      "team scope with owner",
			flags:     ScopeSourceFlags{Scope: "team", Source: "acme"},
			owner:     "acme-team",
			wantScope: config.ScopeTeam,
		},
		{
			name:      "org scope",
			flags:     ScopeSourceFlags{Scope: "org", Source: "acme"},
			wantScope: config.ScopeOrg,
		},
		{
			name:      "project scope",
			flags:     ScopeSourceFlags{Scope: "project", Source: "repo"},
			wantScope: config.ScopeProject,
		},
		{
			name:    "invalid scope errors",
			flags:   ScopeSourceFlags{Scope: "runtime", Source: "x"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTarget(tt.flags, tt.owner)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got target %+v", got)
				}
				if !strings.Contains(err.Error(), "--"+FlagScope) {
					t.Errorf("error %q should name the --%s flag", err, FlagScope)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Scope != tt.wantScope {
				t.Errorf("scope = %q, want %q", got.Scope, tt.wantScope)
			}
			if got.SourceID != tt.flags.Source {
				t.Errorf("source id = %q, want %q", got.SourceID, tt.flags.Source)
			}
			if got.Owner != tt.owner {
				t.Errorf("owner = %q, want %q", got.Owner, tt.owner)
			}
		})
	}
}

func TestNewRouterNilCheckerSafeDefault(t *testing.T) {
	// A nil checker must not panic and must fail closed (prompt) for governed
	// scopes.
	r := NewRouter(nil)
	verdict, err := r.CheckWrite(config.Principal{ID: "u"}, RoutedTarget{
		Scope: config.ScopeOrg, SourceID: "acme",
	})
	if err == nil {
		t.Fatalf("governed scope with nil checker should not be allowed")
	}
	if verdict.Decision != config.DecisionPrompt {
		t.Errorf("decision = %q, want prompt", verdict.Decision)
	}
}

func TestCheckWrite(t *testing.T) {
	prin := config.Principal{ID: "alice", Groups: []string{"acme-team"}}

	tests := []struct {
		name         string
		authorizer   *fakeAuthorizer // nil → no backend wired
		target       RoutedTarget
		wantDecision config.Decision
		wantErr      bool
		errContains  string
		wantBackend  bool // whether the authorizer should have been consulted
	}{
		{
			name:         "local always allowed without backend",
			target:       RoutedTarget{Scope: config.ScopeLocal, SourceID: "personal"},
			wantDecision: config.DecisionAllow,
		},
		{
			name:         "personal project derives to local allow",
			target:       RoutedTarget{Scope: config.ScopeProject, SourceID: "repo"},
			wantDecision: config.DecisionAllow,
		},
		{
			name:         "team allow via backend",
			authorizer:   &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionAllow, Reason: "member"}},
			target:       RoutedTarget{Scope: config.ScopeTeam, SourceID: "acme", Owner: "acme-team"},
			wantDecision: config.DecisionAllow,
			wantBackend:  true,
		},
		{
			name:         "org deny via backend surfaces reason",
			authorizer:   &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionDeny, Reason: "not a member"}},
			target:       RoutedTarget{Scope: config.ScopeOrg, SourceID: "acme", Owner: "acme-org"},
			wantDecision: config.DecisionDeny,
			wantErr:      true,
			errContains:  "denied: not a member",
			wantBackend:  true,
		},
		{
			name:         "team prompt via backend surfaces reason",
			authorizer:   &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionPrompt, Reason: "needs lead approval"}},
			target:       RoutedTarget{Scope: config.ScopeTeam, SourceID: "acme", Owner: "acme-team"},
			wantDecision: config.DecisionPrompt,
			wantErr:      true,
			errContains:  "needs confirmation: needs lead approval",
			wantBackend:  true,
		},
		{
			name:         "backend error fails closed to prompt",
			authorizer:   &fakeAuthorizer{err: errors.New("policy service unreachable")},
			target:       RoutedTarget{Scope: config.ScopeOrg, SourceID: "acme", Owner: "acme-org"},
			wantDecision: config.DecisionPrompt,
			wantErr:      true,
			errContains:  "needs confirmation",
			wantBackend:  true,
		},
		{
			name:         "owned project derives to governed backend",
			authorizer:   &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionAllow, Reason: "ok"}},
			target:       RoutedTarget{Scope: config.ScopeProject, SourceID: "repo", Owner: "acme-team"},
			wantDecision: config.DecisionAllow,
			wantBackend:  true,
		},
		{
			name:        "empty scope is rejected",
			target:      RoutedTarget{SourceID: "x"},
			wantErr:     true,
			errContains: "no write target resolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var checker *config.Checker
			if tt.authorizer != nil {
				checker = config.NewChecker(tt.authorizer)
			}
			r := NewRouter(checker)

			verdict, err := r.CheckWrite(prin, tt.target)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got verdict %+v", verdict)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err, tt.errContains)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Empty-scope rejection never produces a decision.
			if tt.errContains != "no write target resolved" && verdict.Decision != tt.wantDecision {
				t.Errorf("decision = %q, want %q", verdict.Decision, tt.wantDecision)
			}

			if tt.authorizer != nil {
				if tt.authorizer.called != tt.wantBackend {
					t.Errorf("backend called = %v, want %v", tt.authorizer.called, tt.wantBackend)
				}
				if tt.wantBackend && tt.authorizer.gotPrin.ID != prin.ID {
					t.Errorf("backend principal = %+v, want id %q", tt.authorizer.gotPrin, prin.ID)
				}
			}
		})
	}
}

func TestRoute(t *testing.T) {
	prin := config.Principal{ID: "alice"}

	t.Run("resolve error short-circuits before check", func(t *testing.T) {
		auth := &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionAllow}}
		r := NewRouter(config.NewChecker(auth))

		target, verdict, err := r.Route(ScopeSourceFlags{Scope: "bogus"}, prin, "")
		if err == nil {
			t.Fatalf("expected resolve error")
		}
		if auth.called {
			t.Errorf("backend must not be consulted when resolution fails")
		}
		if target.Scope != "" || verdict.Decision != "" {
			t.Errorf("zero target/verdict expected on error, got %+v / %+v", target, verdict)
		}
	})

	t.Run("happy path resolves and allows local", func(t *testing.T) {
		r := NewRouter(nil)
		target, verdict, err := r.Route(ScopeSourceFlags{Source: "personal"}, prin, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.Scope != config.ScopeLocal || target.SourceID != "personal" {
			t.Errorf("target = %+v, want local/personal", target)
		}
		if !verdict.Allowed() {
			t.Errorf("verdict should be allowed, got %+v", verdict)
		}
	})

	t.Run("governed path surfaces verdict and error together", func(t *testing.T) {
		auth := &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionDeny, Reason: "blocked"}}
		r := NewRouter(config.NewChecker(auth))

		target, verdict, err := r.Route(ScopeSourceFlags{Scope: "team", Source: "acme"}, prin, "acme-team")
		if err == nil {
			t.Fatalf("expected deny error")
		}
		if target.SourceID != "acme" {
			t.Errorf("target should still carry the source id, got %+v", target)
		}
		if verdict.Decision != config.DecisionDeny {
			t.Errorf("verdict decision = %q, want deny", verdict.Decision)
		}
	})
}
