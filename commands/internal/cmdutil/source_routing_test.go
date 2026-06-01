package cmdutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/AGOrcha/dot-agents/internal/config"
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

// testPrincipal is the actor reused across the routing tests.
var testPrincipal = config.Principal{ID: "alice", Groups: []string{"acme-team"}}

func TestBindScopeSourceFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	var f ScopeSourceFlags
	BindScopeSourceFlags(cmd, &f)

	assertFlagRegistered(t, cmd, FlagSource)
	scopeFlag := assertFlagRegistered(t, cmd, FlagScope)
	if scopeFlag.DefValue != string(config.ScopeLocal) {
		t.Errorf("--%s default = %q, want %q", FlagScope, scopeFlag.DefValue, config.ScopeLocal)
	}

	// Parsing writes back into the bound struct.
	if err := cmd.Flags().Parse([]string{"--scope", "team", "--source", "acme"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Scope != "team" || f.Source != "acme" {
		t.Fatalf("parsed flags = %+v, want scope=team source=acme", f)
	}
}

// assertFlagRegistered fails the test when name is not registered on cmd and
// returns the looked-up flag for further assertions.
func assertFlagRegistered(t *testing.T, cmd *cobra.Command, name string) *pflag.Flag {
	t.Helper()
	f := cmd.Flags().Lookup(name)
	if f == nil {
		t.Fatalf("--%s not registered", name)
	}
	return f
}

type resolveCase struct {
	name      string
	flags     ScopeSourceFlags
	owner     string
	wantScope config.EditScope
	wantErr   bool
}

func resolveCases() []resolveCase {
	return []resolveCase{
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
}

func TestResolveTarget(t *testing.T) {
	for _, tt := range resolveCases() {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveTarget(tt.flags, tt.owner)
			checkResolveResult(t, tt, got, err)
		})
	}
}

// checkResolveResult asserts the ResolveTarget outcome for one case, keeping
// the table loop body flat.
func checkResolveResult(t *testing.T, tt resolveCase, got RoutedTarget, err error) {
	t.Helper()
	if tt.wantErr {
		assertResolveErr(t, err)
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
}

// assertResolveErr asserts an invalid-scope error names the --scope flag.
func assertResolveErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--"+FlagScope) {
		t.Errorf("error %q should name the --%s flag", err, FlagScope)
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

type checkWriteCase struct {
	name         string
	authorizer   *fakeAuthorizer // nil → no backend wired
	target       RoutedTarget
	wantDecision config.Decision
	wantErr      bool
	errContains  string
	wantBackend  bool // whether the authorizer should have been consulted
}

func checkWriteCases() []checkWriteCase {
	return []checkWriteCase{
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
}

func TestCheckWrite(t *testing.T) {
	for _, tt := range checkWriteCases() {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(checkerFor(tt.authorizer))
			verdict, err := r.CheckWrite(testPrincipal, tt.target)

			assertCheckErr(t, tt, verdict, err)
			assertCheckDecision(t, tt, verdict)
			assertBackendConsulted(t, tt)
		})
	}
}

// checkerFor builds the Checker a case needs: nil authorizer → no backend
// wired (nil checker), otherwise a Checker bound to the fake.
func checkerFor(auth *fakeAuthorizer) *config.Checker {
	if auth == nil {
		return nil
	}
	return config.NewChecker(auth)
}

// assertCheckErr asserts the error expectation (presence + substring) for a
// CheckWrite case.
func assertCheckErr(t *testing.T, tt checkWriteCase, verdict config.Verdict, err error) {
	t.Helper()
	if !tt.wantErr {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error, got verdict %+v", verdict)
	}
	if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
		t.Errorf("error %q should contain %q", err, tt.errContains)
	}
}

// assertCheckDecision asserts the verdict decision, skipping the empty-target
// rejection which never produces a decision.
func assertCheckDecision(t *testing.T, tt checkWriteCase, verdict config.Verdict) {
	t.Helper()
	if tt.errContains == "no write target resolved" {
		return
	}
	if verdict.Decision != tt.wantDecision {
		t.Errorf("decision = %q, want %q", verdict.Decision, tt.wantDecision)
	}
}

// assertBackendConsulted verifies whether the fake authorizer was called and,
// when it was, that it saw the test principal.
func assertBackendConsulted(t *testing.T, tt checkWriteCase) {
	t.Helper()
	if tt.authorizer == nil {
		return
	}
	if tt.authorizer.called != tt.wantBackend {
		t.Errorf("backend called = %v, want %v", tt.authorizer.called, tt.wantBackend)
	}
	if tt.wantBackend && tt.authorizer.gotPrin.ID != testPrincipal.ID {
		t.Errorf("backend principal = %+v, want id %q", tt.authorizer.gotPrin, testPrincipal.ID)
	}
}

func TestRouteResolveErrorShortCircuits(t *testing.T) {
	auth := &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionAllow}}
	r := NewRouter(config.NewChecker(auth))

	target, verdict, err := r.Route(ScopeSourceFlags{Scope: "bogus"}, testPrincipal, "")
	if err == nil {
		t.Fatalf("expected resolve error")
	}
	if auth.called {
		t.Errorf("backend must not be consulted when resolution fails")
	}
	if target.Scope != "" || verdict.Decision != "" {
		t.Errorf("zero target/verdict expected on error, got %+v / %+v", target, verdict)
	}
}

func TestRouteHappyPathLocalAllow(t *testing.T) {
	r := NewRouter(nil)
	target, verdict, err := r.Route(ScopeSourceFlags{Source: "personal"}, testPrincipal, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target.Scope != config.ScopeLocal || target.SourceID != "personal" {
		t.Errorf("target = %+v, want local/personal", target)
	}
	if !verdict.Allowed() {
		t.Errorf("verdict should be allowed, got %+v", verdict)
	}
}

func TestRouteGovernedSurfacesVerdictAndError(t *testing.T) {
	auth := &fakeAuthorizer{verdict: config.Verdict{Decision: config.DecisionDeny, Reason: "blocked"}}
	r := NewRouter(config.NewChecker(auth))

	target, verdict, err := r.Route(ScopeSourceFlags{Scope: "team", Source: "acme"}, testPrincipal, "acme-team")
	if err == nil {
		t.Fatalf("expected deny error")
	}
	if target.SourceID != "acme" {
		t.Errorf("target should still carry the source id, got %+v", target)
	}
	if verdict.Decision != config.DecisionDeny {
		t.Errorf("verdict decision = %q, want deny", verdict.Decision)
	}
}
